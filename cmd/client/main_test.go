package main

import (
	"encoding/json"
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

	// Create via createSessionMarker with cwd
	createSessionMarker(sessionID, "/some/repo/path")

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

// TestCreateSessionMarker_WritesJSONWithCWD verifies the marker file contains JSON
// with session_id, workflow_id, and cwd fields.
func TestCreateSessionMarker_WritesJSONWithCWD(t *testing.T) {
	sessionID := "json-marker-session-001"
	cwd := "/home/user/myproject"
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	os.MkdirAll(dir, 0o755)
	marker := filepath.Join(dir, sessionID)
	// Clean up after test
	defer os.Remove(marker)

	createSessionMarker(sessionID, cwd)

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("could not read marker file: %v", err)
	}

	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("marker file is not valid JSON: %v\ncontent: %s", err, data)
	}

	if m["session_id"] != sessionID {
		t.Errorf("session_id = %q, want %q", m["session_id"], sessionID)
	}
	if m["workflow_id"] != "coding-session-"+sessionID {
		t.Errorf("workflow_id = %q, want %q", m["workflow_id"], "coding-session-"+sessionID)
	}
	if m["cwd"] != cwd {
		t.Errorf("cwd = %q, want %q", m["cwd"], cwd)
	}
}

// TestRemoveSessionMarker_AlsoCleansTeammateMarkers verifies that when removing a parent
// session marker, child teammate markers (with matching parent field) are also removed.
func TestRemoveSessionMarker_AlsoCleansTeammateMarkers(t *testing.T) {
	parentSessionID := "parent-session-cleanup-test"
	teammateSessionID := "teammate-session-cleanup-test"
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	os.MkdirAll(dir, 0o755)

	// Create parent marker
	createSessionMarker(parentSessionID, "/some/repo")

	// Create a teammate marker with parent field pointing to parentSessionID
	teammateMarker := filepath.Join(dir, teammateSessionID)
	teammateData, _ := json.Marshal(map[string]string{
		"session_id":  teammateSessionID,
		"workflow_id": "coding-session-" + parentSessionID,
		"cwd":         "/some/repo",
		"parent":      parentSessionID,
	})
	if err := os.WriteFile(teammateMarker, teammateData, 0o644); err != nil {
		t.Fatalf("could not create teammate marker: %v", err)
	}

	// Remove parent
	removeSessionMarker(parentSessionID)

	// Parent marker should be gone
	if _, err := os.Stat(filepath.Join(dir, parentSessionID)); !os.IsNotExist(err) {
		t.Errorf("parent marker should have been removed")
	}

	// Teammate marker should also be gone
	if _, err := os.Stat(teammateMarker); !os.IsNotExist(err) {
		t.Errorf("teammate marker with parent=%s should have been removed by removeSessionMarker", parentSessionID)
	}
}
