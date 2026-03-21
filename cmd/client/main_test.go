package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eklemin/wf-agents/internal/platform"
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

// TestParsePlatformFromURL verifies platform detection from git remote URLs.
func TestParsePlatformFromURL(t *testing.T) {
	tests := []struct {
		name      string
		remoteURL string
		want      string
	}{
		{"github SSH", "git@github.com:org/repo.git", "github"},
		{"github HTTPS", "https://github.com/org/repo.git", "github"},
		{"gitlab.diftech.org SSH", "git@gitlab.diftech.org:org/repo.git", "gitlab"},
		{"gitlab.diftech.org HTTPS", "https://gitlab.diftech.org/org/repo.git", "gitlab"},
		{"gitlab.com SSH", "git@gitlab.com:org/repo.git", "gitlab"},
		{"gitlab.com HTTPS", "https://gitlab.com/org/repo.git", "gitlab"},
		{"bitbucket", "https://bitbucket.org/org/repo.git", "unknown"},
		{"empty", "", "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := platform.ParsePlatformFromURL(tc.remoteURL)
			if got != tc.want {
				t.Errorf("parsePlatformFromURL(%q) = %q, want %q", tc.remoteURL, got, tc.want)
			}
		})
	}
}

// TestCollectBranchPushedEvidence verifies branch_pushed evidence using a mock CmdRunner.
func TestCollectBranchPushedEvidence(t *testing.T) {
	const sha1 = "abc123def456abc123def456abc123def456abc1"
	const sha2 = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	makeRunner := func(localHead, remoteHead string, localErr, remoteErr error) platform.CmdRunner {
		return func(_ time.Duration, name string, args ...string) (string, error) {
			if name == "git" && len(args) == 2 && args[0] == "rev-parse" && args[1] == "HEAD" {
				return localHead, localErr
			}
			if name == "git" && len(args) == 2 && args[0] == "rev-parse" && args[1] == "@{u}" {
				return remoteHead, remoteErr
			}
			return "", fmt.Errorf("unexpected command: %s %v", name, args)
		}
	}

	tests := []struct {
		name      string
		runner    platform.CmdRunner
		wantKey   bool
		wantValue string
	}{
		{
			name:      "HEAD matches upstream — pushed",
			runner:    makeRunner(sha1+"\n", sha1+"\n", nil, nil),
			wantKey:   true,
			wantValue: "true",
		},
		{
			name:      "HEAD differs from upstream — not pushed",
			runner:    makeRunner(sha1+"\n", sha2+"\n", nil, nil),
			wantKey:   true,
			wantValue: "false",
		},
		{
			name:      "no upstream tracking branch — not pushed",
			runner:    makeRunner(sha1+"\n", "", nil, fmt.Errorf("fatal: no upstream configured")),
			wantKey:   true,
			wantValue: "false",
		},
		{
			name:    "git rev-parse HEAD fails — key absent",
			runner:  makeRunner("", "", fmt.Errorf("not a git repo"), nil),
			wantKey: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evidence := make(map[string]string)
			collectBranchPushedEvidence(evidence, tc.runner)
			val, ok := evidence["branch_pushed"]
			if ok != tc.wantKey {
				t.Fatalf("branch_pushed present=%v, want present=%v", ok, tc.wantKey)
			}
			if tc.wantKey && val != tc.wantValue {
				t.Errorf("branch_pushed = %q, want %q", val, tc.wantValue)
			}
		})
	}
}

// TestCollectGitLabEvidence verifies GitLab evidence collection with a mock platform.CmdRunner.
func TestCollectGitLabEvidence(t *testing.T) {
	makeRunner := func(out string, err error) platform.CmdRunner {
		return func(_ time.Duration, _ string, _ ...string) (string, error) {
			return out, err
		}
	}

	glabOutput := func(pipelineStatus *string, approvedBy int, state string) string {
		pipeline := "null"
		if pipelineStatus != nil {
			pipeline = fmt.Sprintf(`{"status":%q}`, *pipelineStatus)
		}
		approvers := "[]"
		if approvedBy > 0 {
			items := make([]string, approvedBy)
			for i := range items {
				items[i] = `{"username":"user"}`
			}
			approvers = "[" + strings.Join(items, ",") + "]"
		}
		return fmt.Sprintf(`{"head_pipeline":%s,"approved_by":%s,"state":%q}`, pipeline, approvers, state)
	}

	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name         string
		runner       platform.CmdRunner
		wantCI       string
		wantApproved string
		wantMerged   string
	}{
		{
			name:         "pipeline success, approved, merged",
			runner:       makeRunner(glabOutput(strPtr("success"), 1, "merged"), nil),
			wantCI:       "true",
			wantApproved: "true",
			wantMerged:   "true",
		},
		{
			name:         "pipeline failed, not approved, open",
			runner:       makeRunner(glabOutput(strPtr("failed"), 0, "opened"), nil),
			wantCI:       "false",
			wantApproved: "false",
			wantMerged:   "false",
		},
		{
			name:         "pipeline nil (no pipeline), not approved, open",
			runner:       makeRunner(glabOutput(nil, 0, "opened"), nil),
			wantCI:       "true",
			wantApproved: "false",
			wantMerged:   "false",
		},
		{
			name:         "pipeline skipped, approved, not merged",
			runner:       makeRunner(glabOutput(strPtr("skipped"), 2, "opened"), nil),
			wantCI:       "true",
			wantApproved: "true",
			wantMerged:   "false",
		},
		{
			name:         "glab error (no MR)",
			runner:       makeRunner("", fmt.Errorf("no MR found")),
			wantCI:       "true",
			wantApproved: "false",
			wantMerged:   "false",
		},
		{
			name:         "malformed JSON",
			runner:       makeRunner("not json at all", nil),
			wantCI:       "true",
			wantApproved: "false",
			wantMerged:   "false",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evidence := make(map[string]string)
			collectGitLabEvidence(evidence, tc.runner)
			if got := evidence["ci_passed"]; got != tc.wantCI {
				t.Errorf("ci_passed = %q, want %q", got, tc.wantCI)
			}
			if got := evidence["review_approved"]; got != tc.wantApproved {
				t.Errorf("review_approved = %q, want %q", got, tc.wantApproved)
			}
			if got := evidence["merged"]; got != tc.wantMerged {
				t.Errorf("merged = %q, want %q", got, tc.wantMerged)
			}
		})
	}
}

// TestCollectGitHubEvidence verifies GitHub evidence collection with a mock platform.CmdRunner.
func TestCollectGitHubEvidence(t *testing.T) {
	checksJSON := func(states ...string) string {
		items := make([]string, len(states))
		for i, s := range states {
			items[i] = fmt.Sprintf(`{"state":%q}`, s)
		}
		return "[" + strings.Join(items, ",") + "]"
	}

	prJSON := func(decision, state string) string {
		return fmt.Sprintf(`{"reviewDecision":%q,"state":%q}`, decision, state)
	}

	makeRunner := func(checksOut, prOut string, checksErr, prErr error) platform.CmdRunner {
		return func(_ time.Duration, name string, args ...string) (string, error) {
			if name == "gh" && len(args) > 0 && args[0] == "pr" && args[1] == "checks" {
				return checksOut, checksErr
			}
			if name == "gh" && len(args) > 0 && args[0] == "pr" && args[1] == "view" {
				return prOut, prErr
			}
			return "", fmt.Errorf("unexpected command: %s %v", name, args)
		}
	}

	tests := []struct {
		name         string
		runner       platform.CmdRunner
		wantCI       string
		wantApproved string
		wantMerged   string
	}{
		{
			name:         "all checks pass, approved, merged",
			runner:       makeRunner(checksJSON("SUCCESS", "NEUTRAL"), prJSON("APPROVED", "MERGED"), nil, nil),
			wantCI:       "true",
			wantApproved: "true",
			wantMerged:   "true",
		},
		{
			name:         "check failed, not approved, open",
			runner:       makeRunner(checksJSON("SUCCESS", "FAILURE"), prJSON("", "OPEN"), nil, nil),
			wantCI:       "false",
			wantApproved: "false",
			wantMerged:   "false",
		},
		{
			name:         "no checks (empty array), approved, open",
			runner:       makeRunner("[]", prJSON("APPROVED", "OPEN"), nil, nil),
			wantCI:       "true",
			wantApproved: "true",
			wantMerged:   "false",
		},
		{
			name:         "gh pr checks error, gh pr view error",
			runner:       makeRunner("", "", fmt.Errorf("no PR"), fmt.Errorf("no PR")),
			wantCI:       "true",
			wantApproved: "false",
			wantMerged:   "false",
		},
		{
			name:         "skipped check counts as pass",
			runner:       makeRunner(checksJSON("SKIPPED", "SUCCESS"), prJSON("APPROVED", "MERGED"), nil, nil),
			wantCI:       "true",
			wantApproved: "true",
			wantMerged:   "true",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evidence := make(map[string]string)
			collectGitHubEvidence(evidence, tc.runner)
			if got := evidence["ci_passed"]; got != tc.wantCI {
				t.Errorf("ci_passed = %q, want %q", got, tc.wantCI)
			}
			if got := evidence["review_approved"]; got != tc.wantApproved {
				t.Errorf("review_approved = %q, want %q", got, tc.wantApproved)
			}
			if got := evidence["merged"]; got != tc.wantMerged {
				t.Errorf("merged = %q, want %q", got, tc.wantMerged)
			}
		})
	}
}
