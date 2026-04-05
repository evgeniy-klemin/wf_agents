package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ResolveWorkflowID returns the workflow ID for the given session ID.
// Allows short IDs without the "coding-session-" prefix.
func ResolveWorkflowID(id string) string {
	if !strings.HasPrefix(id, "coding-session-") {
		return "coding-session-" + id
	}
	return id
}

// ResolveWorkflowIDByCWD returns the workflow ID for the given session.
// First checks if sessionID itself has a marker (lead session).
// If not, scans all markers to find one with matching CWD (teammate session).
// Returns empty string if no workflow found.
func ResolveWorkflowIDByCWD(sessionID, cwd string) string {
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")

	// Direct match — this is the lead session
	marker := filepath.Join(dir, sessionID)
	if data, err := os.ReadFile(marker); err == nil {
		var m map[string]string
		if json.Unmarshal(data, &m) == nil {
			return m["workflow_id"]
		}
		// Legacy marker (plain text) — assume workflow_id format
		return "coding-session-" + strings.TrimSpace(string(data))
	}

	// No direct match — scan for CWD match (teammate session).
	// Only consider lead markers (no "parent" field). Among multiple lead markers
	// with the same CWD, pick the one with the most recent modification time.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var bestWorkflowID string
	var bestSessionID string
	var bestModTime time.Time

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var m map[string]string
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		// Skip teammate markers — only lead markers can be the CWD match source.
		if m["parent"] != "" {
			continue
		}
		if m["cwd"] != cwd || cwd == "" {
			continue
		}
		// Pick the marker with the latest modification time.
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestModTime) {
			bestModTime = info.ModTime()
			bestWorkflowID = m["workflow_id"]
			bestSessionID = m["session_id"]
		}
	}

	if bestWorkflowID != "" && bestSessionID != sessionID {
		// Found a workflow with same CWD — this teammate belongs to it.
		// Create a marker for the teammate so future hooks resolve directly.
		teammateMarker := filepath.Join(dir, sessionID)
		teammateData, _ := json.Marshal(map[string]string{
			"session_id":  sessionID,
			"workflow_id": bestWorkflowID,
			"cwd":         cwd,
			"parent":      bestSessionID,
		})
		_ = os.WriteFile(teammateMarker, teammateData, 0o644)
		fmt.Fprintf(os.Stderr, "Teammate resolved: session=%s → workflow=%s (via CWD match with %s)\n",
			sessionID, bestWorkflowID, bestSessionID)
		return bestWorkflowID
	}

	return ""
}

// UpdateMarkerCWD patches the cwd field of a lead session marker to a worktree path.
// It is a no-op if:
//   - the marker does not exist
//   - the marker has a "parent" field (teammate marker)
//   - newCWD does not contain "/.claude/worktrees/" (not a worktree path)
//   - the current cwd already contains "/.claude/worktrees/" (already correct)
func UpdateMarkerCWD(sessionID, newCWD string) {
	if !strings.Contains(newCWD, "/.claude/worktrees/") {
		return
	}

	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	marker := filepath.Join(dir, sessionID)

	data, err := os.ReadFile(marker)
	if err != nil {
		return
	}

	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		return
	}

	// Skip teammate markers.
	if m["parent"] != "" {
		return
	}

	// Only patch if current cwd is a repo root (not already a worktree path).
	oldCWD := m["cwd"]
	if strings.Contains(oldCWD, "/.claude/worktrees/") {
		return
	}

	m["cwd"] = newCWD
	updated, err := json.Marshal(m)
	if err != nil {
		return
	}
	if err := os.WriteFile(marker, updated, 0o644); err != nil {
		return
	}
	fmt.Fprintf(os.Stderr, "Session marker CWD patched: %s → %s\n", oldCWD, newCWD)
}
