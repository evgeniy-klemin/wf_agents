package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeMarker(t *testing.T, dir, sessionID string, fields map[string]string) {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("writeMarker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID), data, 0o644); err != nil {
		t.Fatalf("writeMarker write: %v", err)
	}
}

func readMarker(t *testing.T, dir, sessionID string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, sessionID))
	if err != nil {
		t.Fatalf("readMarker: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("readMarker unmarshal: %v", err)
	}
	return m
}

func setupDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setupDir: %v", err)
	}
	return dir
}

func TestUpdateMarkerCWD_SkipsTeammateMarkers(t *testing.T) {
	dir := setupDir(t)
	sessionID := "update-cwd-teammate-test"
	defer os.Remove(filepath.Join(dir, sessionID))

	writeMarker(t, dir, sessionID, map[string]string{
		"session_id":  sessionID,
		"workflow_id": "coding-session-" + sessionID,
		"cwd":         "/some/repo",
		"parent":      "lead-session-id",
	})

	worktreePath := "/some/repo/.claude/worktrees/my-branch"
	UpdateMarkerCWD(sessionID, worktreePath)

	m := readMarker(t, dir, sessionID)
	if m["cwd"] != "/some/repo" {
		t.Errorf("teammate marker cwd should not be patched, got %q", m["cwd"])
	}
}

func TestUpdateMarkerCWD_NoOpWhenAlreadyWorktree(t *testing.T) {
	dir := setupDir(t)
	sessionID := "update-cwd-already-worktree-test"
	defer os.Remove(filepath.Join(dir, sessionID))

	existing := "/some/repo/.claude/worktrees/existing-branch"
	writeMarker(t, dir, sessionID, map[string]string{
		"session_id":  sessionID,
		"workflow_id": "coding-session-" + sessionID,
		"cwd":         existing,
	})

	UpdateMarkerCWD(sessionID, "/some/repo/.claude/worktrees/different-branch")

	m := readMarker(t, dir, sessionID)
	if m["cwd"] != existing {
		t.Errorf("already-worktree cwd should not be changed, got %q", m["cwd"])
	}
}

func TestUpdateMarkerCWD_NoOpWhenNewCWDNotWorktree(t *testing.T) {
	dir := setupDir(t)
	sessionID := "update-cwd-not-worktree-test"
	defer os.Remove(filepath.Join(dir, sessionID))

	writeMarker(t, dir, sessionID, map[string]string{
		"session_id":  sessionID,
		"workflow_id": "coding-session-" + sessionID,
		"cwd":         "/some/repo",
	})

	UpdateMarkerCWD(sessionID, "/some/other/path")

	m := readMarker(t, dir, sessionID)
	if m["cwd"] != "/some/repo" {
		t.Errorf("non-worktree newCWD should not patch, got %q", m["cwd"])
	}
}

func TestUpdateMarkerCWD_PatchesRepoRootToWorktree(t *testing.T) {
	dir := setupDir(t)
	sessionID := "update-cwd-patch-test"
	defer os.Remove(filepath.Join(dir, sessionID))

	writeMarker(t, dir, sessionID, map[string]string{
		"session_id":  sessionID,
		"workflow_id": "coding-session-" + sessionID,
		"cwd":         "/some/repo",
	})

	worktreePath := "/some/repo/.claude/worktrees/feature-branch"
	UpdateMarkerCWD(sessionID, worktreePath)

	m := readMarker(t, dir, sessionID)
	if m["cwd"] != worktreePath {
		t.Errorf("cwd should be patched to %q, got %q", worktreePath, m["cwd"])
	}
}

func TestUpdateMarkerCWD_NoOpWhenMarkerMissing(t *testing.T) {
	// Should not panic or error on missing marker.
	UpdateMarkerCWD("nonexistent-session-xyz", "/some/repo/.claude/worktrees/branch")
}
