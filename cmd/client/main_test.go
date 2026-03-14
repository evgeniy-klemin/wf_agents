package main

import (
	"os"
	"path/filepath"
	"testing"
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
