package main

// Tool permission and teammate detection tests have moved to internal/workflow/tool_permissions_test.go.
// They now test the exported CheckToolPermission and IsTeammate functions directly.
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

// ---------------------------------------------------------------------------
// resolveWorkflowID tests
// ---------------------------------------------------------------------------

// makeMarker is a test helper that writes a JSON marker to $TMPDIR/wf-agents-sessions/<name>.
func makeMarker(t *testing.T, dir, filename string, fields map[string]string) {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("makeMarker: marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o644); err != nil {
		t.Fatalf("makeMarker: write: %v", err)
	}
}

// setupSessionsDir creates a temp directory and overrides os.TempDir to return it.
// Because os.TempDir is not mockable, we use a different approach: we write directly
// to the real $TMPDIR/wf-agents-sessions directory and clean up afterwards.
func setupMarkerDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("cannot create marker dir: %v", err)
	}
	return dir
}

// TestResolveWorkflowID_DirectMatch verifies that a lead session's marker is found directly.
func TestResolveWorkflowID_DirectMatch(t *testing.T) {
	dir := setupMarkerDir(t)
	sessionID := "direct-match-session-test"
	defer os.Remove(filepath.Join(dir, sessionID))

	makeMarker(t, dir, sessionID, map[string]string{
		"session_id":  sessionID,
		"workflow_id": "coding-session-" + sessionID,
		"cwd":         "/some/project",
	})

	got := resolveWorkflowID(sessionID, "/some/project")
	want := "coding-session-" + sessionID
	if got != want {
		t.Errorf("resolveWorkflowID direct match = %q, want %q", got, want)
	}
}

// TestResolveWorkflowID_LegacyPlainTextMarker verifies backward compatibility with
// old plain-text markers (session_id as plain text, not JSON).
func TestResolveWorkflowID_LegacyPlainTextMarker(t *testing.T) {
	dir := setupMarkerDir(t)
	sessionID := "legacy-plain-text-session"
	marker := filepath.Join(dir, sessionID)
	defer os.Remove(marker)

	// Write a legacy plain-text marker (old format)
	if err := os.WriteFile(marker, []byte(sessionID), 0o644); err != nil {
		t.Fatalf("could not write legacy marker: %v", err)
	}

	got := resolveWorkflowID(sessionID, "/any/cwd")
	want := "coding-session-" + sessionID
	if got != want {
		t.Errorf("resolveWorkflowID legacy marker = %q, want %q", got, want)
	}
}

// TestResolveWorkflowID_CWDMatch verifies that a teammate session (no direct marker)
// is resolved by scanning markers for a matching CWD.
func TestResolveWorkflowID_CWDMatch(t *testing.T) {
	dir := setupMarkerDir(t)
	leadSessionID := "lead-session-for-cwd-test"
	teammateSessionID := "teammate-cwd-match-session"
	cwd := "/unique/cwd/for/cwd-match-test"

	defer os.Remove(filepath.Join(dir, leadSessionID))
	defer os.Remove(filepath.Join(dir, teammateSessionID)) // auto-created teammate marker

	// Write lead session marker with a distinct CWD
	makeMarker(t, dir, leadSessionID, map[string]string{
		"session_id":  leadSessionID,
		"workflow_id": "coding-session-" + leadSessionID,
		"cwd":         cwd,
	})

	// Teammate has a different session_id but same CWD, no marker yet
	got := resolveWorkflowID(teammateSessionID, cwd)
	want := "coding-session-" + leadSessionID
	if got != want {
		t.Errorf("resolveWorkflowID CWD match = %q, want %q", got, want)
	}

	// A teammate marker should have been auto-created
	teammateMarker := filepath.Join(dir, teammateSessionID)
	data, err := os.ReadFile(teammateMarker)
	if err != nil {
		t.Fatalf("teammate marker was not auto-created: %v", err)
	}
	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		t.Fatalf("teammate marker is not valid JSON: %s", data)
	}
	if m["workflow_id"] != want {
		t.Errorf("teammate marker workflow_id = %q, want %q", m["workflow_id"], want)
	}
	if m["parent"] != leadSessionID {
		t.Errorf("teammate marker parent = %q, want %q", m["parent"], leadSessionID)
	}
	if m["cwd"] != cwd {
		t.Errorf("teammate marker cwd = %q, want %q", m["cwd"], cwd)
	}
}

// TestResolveWorkflowID_NoMatch verifies that an unrelated session returns empty string.
func TestResolveWorkflowID_NoMatch(t *testing.T) {
	// Use a session ID that will never have a marker
	got := resolveWorkflowID("no-marker-session-xyz-never-exists", "/completely/unique/cwd/xyz-never")
	if got != "" {
		t.Errorf("resolveWorkflowID no match = %q, want empty string", got)
	}
}

// The deny path in main.go calls os.Exit(2) after writing the reason to stdout/stderr.
// os.Exit(2) cannot be tested in-process without subprocess scaffolding, so there is no
// TestDenyOutputFormat here. The deny behavior (exit code 2, plain-text reason on stdout)
// was verified via live testing — the Edit tool was blocked with exit code 2 as expected.
//
// The logic that decides WHAT to deny (which tools are forbidden in which phase, teammate
// detection, etc.) is thoroughly tested in internal/workflow/tool_permissions_test.go via
// the exported CheckToolPermission and IsTeammate functions.

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
	// Find the project root by locating the states directory.
	// The test binary runs from the package directory, so we go up two levels.
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("cannot determine project root: %v", err)
	}
	// Verify the states directory exists at the expected location.
	statesDir := filepath.Join(projectRoot, "states")
	if _, err := os.Stat(statesDir); err != nil {
		t.Fatalf("states directory not found at %s: %v", statesDir, err)
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

// TestSyntheticAgentID_TeammateSessionGetsAgentID verifies that when a session_id differs from
// the workflow's session_id (teammate session), a synthetic agent_id is set.
func TestSyntheticAgentID_TeammateSessionGetsAgentID(t *testing.T) {
	dir := setupMarkerDir(t)
	leadSessionID := "lead-session-synthetic-agent-id-test"
	teammateSessionID := "teammate-synthetic-agent-id-test"
	cwd := "/unique/cwd/for/synthetic-agent-id-test"
	workflowID := "coding-session-" + leadSessionID

	defer os.Remove(filepath.Join(dir, leadSessionID))
	defer os.Remove(filepath.Join(dir, teammateSessionID))

	// Write lead session marker
	makeMarker(t, dir, leadSessionID, map[string]string{
		"session_id":  leadSessionID,
		"workflow_id": workflowID,
		"cwd":         cwd,
	})

	// Simulate a teammate hook input: same CWD, different session_id, empty agent_id
	input := claudeHookInput{
		SessionID: teammateSessionID,
		CWD:       cwd,
		AgentID:   "",
	}

	// Resolve the workflow ID (side-effect: creates teammate marker)
	resolvedWorkflowID := resolveWorkflowID(input.SessionID, input.CWD)
	if resolvedWorkflowID != workflowID {
		t.Fatalf("expected workflow_id %q, got %q", workflowID, resolvedWorkflowID)
	}

	// Apply synthetic agent_id logic (mirrors the code in main())
	workflowSessionID := strings.TrimPrefix(resolvedWorkflowID, "coding-session-")
	if input.SessionID != workflowSessionID && input.AgentID == "" {
		input.AgentID = "teammate-" + input.SessionID
	}

	expectedAgentID := "teammate-" + teammateSessionID
	if input.AgentID != expectedAgentID {
		t.Errorf("synthetic agent_id = %q, want %q", input.AgentID, expectedAgentID)
	}
}

// TestSyntheticAgentID_LeadSessionUnchanged verifies that the lead session's agent_id is NOT
// modified (session_id matches workflow's session_id).
func TestSyntheticAgentID_LeadSessionUnchanged(t *testing.T) {
	dir := setupMarkerDir(t)
	leadSessionID := "lead-session-no-change-test"
	workflowID := "coding-session-" + leadSessionID
	cwd := "/unique/cwd/for/no-change-test"

	defer os.Remove(filepath.Join(dir, leadSessionID))

	makeMarker(t, dir, leadSessionID, map[string]string{
		"session_id":  leadSessionID,
		"workflow_id": workflowID,
		"cwd":         cwd,
	})

	input := claudeHookInput{
		SessionID: leadSessionID,
		CWD:       cwd,
		AgentID:   "",
	}

	resolvedWorkflowID := resolveWorkflowID(input.SessionID, input.CWD)
	if resolvedWorkflowID != workflowID {
		t.Fatalf("expected workflow_id %q, got %q", workflowID, resolvedWorkflowID)
	}

	// Apply synthetic agent_id logic
	workflowSessionID := strings.TrimPrefix(resolvedWorkflowID, "coding-session-")
	if input.SessionID != workflowSessionID && input.AgentID == "" {
		input.AgentID = "teammate-" + input.SessionID
	}

	// Lead session should NOT get a synthetic agent_id
	if input.AgentID != "" {
		t.Errorf("lead session agent_id should remain empty, got %q", input.AgentID)
	}
}

// TestSyntheticAgentID_ExistingAgentIDPreserved verifies that a teammate with an existing
// agent_id (explicitly set by Claude Code) is not overwritten.
func TestSyntheticAgentID_ExistingAgentIDPreserved(t *testing.T) {
	dir := setupMarkerDir(t)
	leadSessionID := "lead-session-preserve-agent-id-test"
	teammateSessionID := "teammate-preserve-agent-id-test"
	cwd := "/unique/cwd/for/preserve-agent-id-test"
	workflowID := "coding-session-" + leadSessionID
	existingAgentID := "existing-agent-abc"

	defer os.Remove(filepath.Join(dir, leadSessionID))
	defer os.Remove(filepath.Join(dir, teammateSessionID))

	makeMarker(t, dir, leadSessionID, map[string]string{
		"session_id":  leadSessionID,
		"workflow_id": workflowID,
		"cwd":         cwd,
	})

	// Teammate already has an agent_id set
	input := claudeHookInput{
		SessionID: teammateSessionID,
		CWD:       cwd,
		AgentID:   existingAgentID,
	}

	resolvedWorkflowID := resolveWorkflowID(input.SessionID, input.CWD)
	if resolvedWorkflowID != workflowID {
		t.Fatalf("expected workflow_id %q, got %q", workflowID, resolvedWorkflowID)
	}

	// Apply synthetic agent_id logic
	workflowSessionID := strings.TrimPrefix(resolvedWorkflowID, "coding-session-")
	if input.SessionID != workflowSessionID && input.AgentID == "" {
		input.AgentID = "teammate-" + input.SessionID
	}

	// Existing agent_id should be preserved
	if input.AgentID != existingAgentID {
		t.Errorf("existing agent_id should be preserved, got %q, want %q", input.AgentID, existingAgentID)
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
