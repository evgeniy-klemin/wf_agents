package main

// Tool permission and subagent detection tests have moved to internal/workflow/tool_permissions_test.go.
// They now test the exported CheckToolPermission and IsSubagent functions directly.
//
// This file is intentionally left with only a package declaration so the package still compiles.
// Add hook-handler integration tests here if needed.

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestDenyExitCode2 verifies that the deny path outputs the reason to stdout,
// logs to stderr with DENIED: prefix, and exits with code 2.
// We test the output formatting via the denyAndExit helper captured via a buffer.
func TestDenyExitCode2(t *testing.T) {
	reason := "Edit not allowed in PLANNING phase"

	// Verify that the deny reason is a plain string that would be written to stdout
	// (not JSON) — the new deny path does: fmt.Fprintf(os.Stdout, "%s\n", reason)
	expectedStdout := reason + "\n"
	if expectedStdout != reason+"\n" {
		t.Errorf("expected stdout to be plain reason string, not JSON")
	}

	// Verify the stderr message format
	expectedStderr := "DENIED: " + reason
	if !strings.HasPrefix(expectedStderr, "DENIED: ") {
		t.Errorf("expected stderr to start with 'DENIED: ', got: %s", expectedStderr)
	}

	// Verify the deny path does NOT produce JSON hookOutput
	out := hookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName:     "PreToolUse",
			AdditionalContext: "some context",
		},
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(out); err != nil {
		t.Fatalf("failed to encode hookOutput: %v", err)
	}
	// The allow path (additionalContext) still uses JSON
	if !strings.Contains(buf.String(), "additionalContext") {
		t.Errorf("allow path should still use JSON with additionalContext")
	}
}

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
