package main

// Tool permission and subagent detection tests have moved to internal/workflow/tool_permissions_test.go.
// They now test the exported CheckToolPermission and IsSubagent functions directly.
//
// This file is intentionally left with only a package declaration so the package still compiles.
// Add hook-handler integration tests here if needed.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/eklemin/wf-agents/internal/model"
)

// The deny path in main.go calls os.Exit(2) after writing the reason to stdout/stderr.
// os.Exit(2) cannot be tested in-process without subprocess scaffolding, so there is no
// TestDenyOutputFormat here. The deny behavior (exit code 2, plain-text reason on stdout)
// was verified via live testing — the Edit tool was blocked with exit code 2 as expected.
//
// The logic that decides WHAT to deny (which tools are forbidden in which phase, subagent
// detection, etc.) is thoroughly tested in internal/workflow/tool_permissions_test.go via
// the exported CheckToolPermission and IsSubagent functions.

// TestSessionStartOutputHasContinueTrue verifies that the SessionStart hookOutput
// serialized to JSON DOES include "continue": true so Claude keeps running.
func TestSessionStartOutputHasContinueTrue(t *testing.T) {
	out := hookOutput{
		Continue: boolPtr(true),
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName:     "SessionStart",
			AdditionalContext: "session context",
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(out); err != nil {
		t.Fatalf("failed to encode hookOutput: %v", err)
	}

	raw := buf.String()
	if !strings.Contains(raw, `"continue":true`) {
		t.Errorf("SessionStart output must contain \"continue\":true, got: %s", raw)
	}
}

// TestAllowWithContextOutputNoContinueField verifies that an allow-with-additionalContext
// hookOutput does NOT include the "continue" field (it is not required for allow).
func TestAllowWithContextOutputNoContinueField(t *testing.T) {
	out := hookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName:     "PreToolUse",
			AdditionalContext: "some phase instructions",
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(out); err != nil {
		t.Fatalf("failed to encode hookOutput: %v", err)
	}

	raw := buf.String()
	if strings.Contains(raw, `"continue"`) {
		t.Errorf("allow-with-context output must NOT contain \"continue\" field, got: %s", raw)
	}
}

// TestPhaseInstructionsNonEmpty verifies that phaseInstructions returns non-empty content
// for all phases, and that no {{...}} placeholders remain unresolved.
func TestPhaseInstructionsNonEmpty(t *testing.T) {
	// Find the project root by locating the claude/states directory.
	// The test binary runs from the package directory, so we go up two levels.
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("cannot determine project root: %v", err)
	}
	// Verify the states directory exists at the expected location.
	statesDir := filepath.Join(projectRoot, "claude", "states")
	if _, err := os.Stat(statesDir); err != nil {
		t.Fatalf("claude/states directory not found at %s: %v", statesDir, err)
	}

	// Set CLAUDE_PLUGIN_ROOT so phaseInstructions can locate the state files.
	t.Setenv("CLAUDE_PLUGIN_ROOT", projectRoot)

	// phaseInstructions reads CLAUDE_PLUGIN_ROOT and uses wfClientBin() which reads env.
	// We also set WF_CLIENT_BIN to a known value so the placeholder resolves.
	t.Setenv("WF_CLIENT_BIN", "/usr/local/bin/wf-client")

	unresolvedPlaceholder := regexp.MustCompile(`\{\{[A-Z_]+\}\}`)

	phases := []model.Phase{
		model.PhasePlanning,
		model.PhaseRespawn,
		model.PhaseDeveloping,
		model.PhaseReviewing,
		model.PhaseCommitting,
		model.PhasePRCreation,
		model.PhaseFeedback,
		model.PhaseComplete,
		model.PhaseBlocked,
	}

	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			result := phaseInstructions(phase)

			if strings.TrimSpace(result) == "" {
				t.Errorf("phaseInstructions(%s) returned empty string", phase)
			}

			if matches := unresolvedPlaceholder.FindAllString(result, -1); len(matches) > 0 {
				t.Errorf("phaseInstructions(%s) has unresolved placeholders: %v\nContent:\n%s",
					phase, matches, result)
			}
		})
	}
}

// TestPhaseInstructionsFallbackOnMissingFile verifies that phaseInstructions falls back
// gracefully when CLAUDE_PLUGIN_ROOT points to a directory without state files.
func TestPhaseInstructionsFallbackOnMissingFile(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "/nonexistent/path")

	// Should not panic, should return a non-empty fallback string.
	result := phaseInstructions(model.PhasePlanning)
	if strings.TrimSpace(result) == "" {
		t.Error("phaseInstructions fallback must return non-empty string")
	}
}

// TestAutoAllowOutputHasPermissionDecisionAllow verifies that when Allowed is true, the JSON output
// contains permissionDecision: "allow" so Claude Code bypasses the permission prompt.
func TestAutoAllowOutputHasPermissionDecisionAllow(t *testing.T) {
	out := hookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "allow",
			PermissionDecisionReason: "Safe command auto-approved by workflow",
			AdditionalContext:        "[Workflow Phase: DEVELOPING] phase instructions",
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(out); err != nil {
		t.Fatalf("failed to encode hookOutput: %v", err)
	}

	raw := buf.String()
	if !strings.Contains(raw, `"permissionDecision":"allow"`) {
		t.Errorf("auto-allow output must contain permissionDecision:\"allow\", got: %s", raw)
	}
	if !strings.Contains(raw, `"permissionDecisionReason"`) {
		t.Errorf("auto-allow output must contain permissionDecisionReason, got: %s", raw)
	}
	if strings.Contains(raw, `"continue"`) {
		t.Errorf("auto-allow output must NOT contain \"continue\" field, got: %s", raw)
	}
}
