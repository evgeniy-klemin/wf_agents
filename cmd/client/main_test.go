package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
)

func TestRemoveSessionMarker_DeletesExistingFile(t *testing.T) {
	// Create a real marker file first
	sessionID := "test-session-abc123"
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("could not create session dir: %v", err)
	}
	marker := filepath.Join(dir, sessionID)
	if err := os.WriteFile(marker, []byte(sessionID), 0o644); err != nil {
		t.Fatalf("could not create marker file: %v", err)
	}

	// Verify it exists before removal
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected marker file to exist before removal: %v", err)
	}

	// Call the function under test
	removeSessionMarker(sessionID)

	// Verify it's gone
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("expected marker file to be deleted, but it still exists (err=%v)", err)
	}
}

func TestRemoveSessionMarker_NoErrorWhenFileAbsent(t *testing.T) {
	// Use a session ID that definitely has no marker file
	sessionID := "nonexistent-session-xyz"
	marker := filepath.Join(os.TempDir(), "wf-agents-sessions", sessionID)

	// Ensure the file doesn't exist
	os.Remove(marker)

	// Should not panic or log.Fatal — just succeed silently
	removeSessionMarker(sessionID)
}

// TestStartWorkflowOptions_ReusePolicy verifies that buildStartOptions
// allows workflow ID reuse only after the previous execution has closed,
// preventing parallel workflows with the same ID.
func TestStartWorkflowOptions_ReusePolicy(t *testing.T) {
	opts := buildStartOptions("coding-session-test", "coding-session")
	want := enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE
	if opts.WorkflowIDReusePolicy != want {
		t.Errorf("WorkflowIDReusePolicy = %v, want %v", opts.WorkflowIDReusePolicy, want)
	}
}

// TestStartWorkflowOptions_IDAndQueue verifies ID and task queue are set correctly.
func TestStartWorkflowOptions_IDAndQueue(t *testing.T) {
	opts := buildStartOptions("coding-session-abc", "my-queue")
	if opts.ID != "coding-session-abc" {
		t.Errorf("ID = %q, want %q", opts.ID, "coding-session-abc")
	}
	if opts.TaskQueue != "my-queue" {
		t.Errorf("TaskQueue = %q, want %q", opts.TaskQueue, "my-queue")
	}
}

// TestAlreadyStartedErrorMessage verifies the helper function that detects
// "already started" errors produces a clear message.
func TestAlreadyStartedErrorMessage(t *testing.T) {
	msg := alreadyStartedMessage("test-session-123")
	if !strings.Contains(msg, "test-session-123") {
		t.Errorf("error message should contain session ID, got: %s", msg)
	}
	if !strings.Contains(msg, "already running") {
		t.Errorf("error message should mention 'already running', got: %s", msg)
	}
}

// TestIsAlreadyStartedError verifies detection of AlreadyStarted errors.
func TestIsAlreadyStartedError(t *testing.T) {
	tests := []struct {
		name string
		err  string
		want bool
	}{
		{"already started lowercase", "workflow already started", true},
		{"AlreadyStarted camel", "AlreadyStarted error code", true},
		{"unrelated error", "connection refused", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isAlreadyStartedError(tc.err)
			if got != tc.want {
				t.Errorf("isAlreadyStartedError(%q) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// Compile-time check: buildStartOptions must return client.StartWorkflowOptions.
var _ client.StartWorkflowOptions = buildStartOptions("", "")

func TestRemoveSessionMarker_PathMatchesCreateMarker(t *testing.T) {
	// Ensure removeSessionMarker targets the same path as createSessionMarker
	sessionID := "roundtrip-session-999"
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	os.MkdirAll(dir, 0o755)
	expectedMarker := filepath.Join(dir, sessionID)

	// Create via createSessionMarker
	createSessionMarker(sessionID)

	// Verify it exists
	if _, err := os.Stat(expectedMarker); err != nil {
		t.Fatalf("createSessionMarker did not create marker at expected path: %v", err)
	}

	// Remove via removeSessionMarker
	removeSessionMarker(sessionID)

	// Verify it's gone
	if _, err := os.Stat(expectedMarker); !os.IsNotExist(err) {
		t.Errorf("removeSessionMarker did not delete marker at expected path")
	}
}
