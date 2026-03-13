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

// TestDenyOutputNoContinueField verifies that a deny hookOutput serialized to JSON
// does NOT include the "continue" field. Claude Code ignores the deny if "continue"
// is present, so it must be omitted entirely.
func TestDenyOutputNoContinueField(t *testing.T) {
	out := hookOutput{
		HookSpecificOutput: &hookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: "test reason",
		},
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(out); err != nil {
		t.Fatalf("failed to encode hookOutput: %v", err)
	}

	raw := buf.String()
	if strings.Contains(raw, `"continue"`) {
		t.Errorf("deny output must NOT contain \"continue\" field, got: %s", raw)
	}

	// Verify the deny fields are still present
	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}
	hso, ok := decoded["hookSpecificOutput"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing hookSpecificOutput in output: %s", raw)
	}
	if hso["permissionDecision"] != "deny" {
		t.Errorf("expected permissionDecision=deny, got %v", hso["permissionDecision"])
	}
	if hso["permissionDecisionReason"] != "test reason" {
		t.Errorf("expected permissionDecisionReason='test reason', got %v", hso["permissionDecisionReason"])
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
