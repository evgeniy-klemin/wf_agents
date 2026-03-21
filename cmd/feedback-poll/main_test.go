package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eklemin/wf-agents/internal/platform"
)

// mockRunner builds a CmdRunner that returns canned responses keyed by
// the first argument after the command name (or the command itself).
func mockRunner(responses map[string]string, errors map[string]error) platform.CmdRunner {
	return func(timeout time.Duration, name string, args ...string) (string, error) {
		// Build a lookup key from command + first arg
		key := name
		if len(args) > 0 {
			key = name + " " + args[0]
		}
		if err, ok := errors[key]; ok {
			return "", err
		}
		if resp, ok := responses[key]; ok {
			return resp, nil
		}
		// Fallback: try full command
		full := name
		for _, a := range args {
			full += " " + a
		}
		if err, ok := errors[full]; ok {
			return "", err
		}
		if resp, ok := responses[full]; ok {
			return resp, nil
		}
		return "", fmt.Errorf("unexpected command: %s %v", name, args)
	}
}

func TestGitHubPoll_NewComments(t *testing.T) {
	prViewJSON := `{
		"reviewDecision": "REVIEW_REQUIRED",
		"state": "OPEN",
		"number": 42,
		"headRepository": {"name": "myrepo"},
		"headRepositoryOwner": {"login": "myorg"}
	}`

	inlineJSON := `[
		{"id": 1, "path": "foo.go", "line": 10, "body": "inline 1", "user": {"login": "alice"}, "created_at": "2026-01-01T00:00:00Z"},
		{"id": 2, "path": "bar.go", "line": 20, "body": "inline 2", "user": {"login": "bob"}, "created_at": "2026-01-02T00:00:00Z"},
		{"id": 3, "path": "baz.go", "line": 30, "body": "inline 3", "user": {"login": "carol"}, "created_at": "2026-01-03T00:00:00Z"}
	]`

	prCommentsJSON := `{
		"comments": [
			{"id": 100, "body": "pr comment 1", "author": {"login": "dave"}, "createdAt": "2026-01-04T00:00:00Z"},
			{"id": 101, "body": "pr comment 2", "author": {"login": "eve"}, "createdAt": "2026-01-05T00:00:00Z"}
		]
	}`

	responses := map[string]string{
		"which gh":                   "/usr/bin/gh",
		"gh pr":                      prViewJSON,
		"gh api":                     inlineJSON,
		"gh pr view --json comments": prCommentsJSON,
	}

	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "which" && len(args) > 0 && args[0] == "gh" {
			return "/usr/bin/gh", nil
		}
		if name == "gh" && len(args) > 0 && args[0] == "pr" {
			// Distinguish between the two gh pr view calls
			for _, a := range args {
				if a == "comments" {
					return prCommentsJSON, nil
				}
			}
			return prViewJSON, nil
		}
		if name == "gh" && len(args) > 0 && args[0] == "api" {
			return inlineJSON, nil
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}
	_ = responses

	approvalState, prState, inline, prComments, err := pollGitHub(runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approvalState != "REVIEW_REQUIRED" {
		t.Errorf("want REVIEW_REQUIRED, got %q", approvalState)
	}
	if prState != "OPEN" {
		t.Errorf("want OPEN, got %q", prState)
	}
	if len(inline) != 3 {
		t.Errorf("want 3 inline comments, got %d", len(inline))
	}
	if len(prComments) != 2 {
		t.Errorf("want 2 PR comments, got %d", len(prComments))
	}
}

func TestGitHubPoll_Approved(t *testing.T) {
	prViewJSON := `{
		"reviewDecision": "APPROVED",
		"state": "OPEN",
		"number": 7,
		"headRepository": {"name": "repo"},
		"headRepositoryOwner": {"login": "org"}
	}`

	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "which" {
			return "/usr/bin/gh", nil
		}
		if name == "gh" && len(args) > 0 && args[0] == "pr" {
			for _, a := range args {
				if a == "comments" {
					return `{"comments":[]}`, nil
				}
			}
			return prViewJSON, nil
		}
		if name == "gh" && len(args) > 0 && args[0] == "api" {
			return `[]`, nil
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	approvalState, prState, _, _, err := pollGitHub(runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approvalState != "APPROVED" {
		t.Errorf("want APPROVED, got %q", approvalState)
	}
	if prState != "OPEN" {
		t.Errorf("want OPEN, got %q", prState)
	}
}

func TestGitHubPoll_SeenFiltering(t *testing.T) {
	prViewJSON := `{
		"reviewDecision": "REVIEW_REQUIRED",
		"state": "OPEN",
		"number": 1,
		"headRepository": {"name": "r"},
		"headRepositoryOwner": {"login": "o"}
	}`
	inlineJSON := `[
		{"id": 10, "path": "a.go", "line": 1, "body": "old", "user": {"login": "x"}, "created_at": "2026-01-01T00:00:00Z"},
		{"id": 11, "path": "b.go", "line": 2, "body": "new", "user": {"login": "y"}, "created_at": "2026-01-02T00:00:00Z"}
	]`
	prCommentsJSON := `{"comments":[]}`

	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "which" {
			return "/usr/bin/gh", nil
		}
		if name == "gh" && len(args) > 0 && args[0] == "pr" {
			for _, a := range args {
				if a == "comments" {
					return prCommentsJSON, nil
				}
			}
			return prViewJSON, nil
		}
		if name == "gh" && len(args) > 0 && args[0] == "api" {
			return inlineJSON, nil
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	_, _, inline, _, err := pollGitHub(runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Pre-populate seen with ID 10
	seen := map[string]bool{"10": true}
	newInline := filterNew(inline, seen)

	if len(newInline) != 1 {
		t.Fatalf("want 1 new inline comment, got %d", len(newInline))
	}
	if newInline[0].ID != "11" {
		t.Errorf("want ID 11, got %q", newInline[0].ID)
	}
}

func TestGitLabPoll_NewComments(t *testing.T) {
	mrViewJSON := `{
		"state": "opened",
		"approved_by": [],
		"project_id": 99,
		"iid": 5
	}`
	notesJSON := `[
		{"id": 200, "body": "note 1", "author": {"username": "alice"}, "created_at": "2026-01-01T00:00:00Z", "system": false},
		{"id": 201, "body": "note 2", "author": {"username": "bob"}, "created_at": "2026-01-02T00:00:00Z", "system": false},
		{"id": 202, "body": "system note", "author": {"username": "gitlab"}, "created_at": "2026-01-03T00:00:00Z", "system": true}
	]`
	discussionsJSON := `[
		{
			"notes": [
				{
					"id": 300,
					"body": "inline comment",
					"author": {"username": "carol"},
					"position": {"new_path": "main.go", "new_line": 42},
					"created_at": "2026-01-04T00:00:00Z",
					"system": false
				}
			]
		}
	]`

	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "which" {
			return "/usr/bin/glab", nil
		}
		if name == "glab" && len(args) > 0 && args[0] == "mr" {
			return mrViewJSON, nil
		}
		if name == "glab" && len(args) > 0 && args[0] == "api" {
			for _, a := range args {
				if containsSubstr(a, "discussions") {
					return discussionsJSON, nil
				}
				if containsSubstr(a, "notes") {
					return notesJSON, nil
				}
			}
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	approvalState, prState, inline, prComments, err := pollGitLab(runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approvalState != "pending" {
		t.Errorf("want pending, got %q", approvalState)
	}
	if prState != "opened" {
		t.Errorf("want opened, got %q", prState)
	}
	// system note should be filtered
	if len(prComments) != 2 {
		t.Errorf("want 2 MR notes (system filtered), got %d", len(prComments))
	}
	if len(inline) != 1 {
		t.Errorf("want 1 inline comment, got %d", len(inline))
	}
}

func TestGitLabPoll_Merged(t *testing.T) {
	mrViewJSON := `{
		"state": "merged",
		"approved_by": [{"username": "reviewer1"}],
		"project_id": 10,
		"iid": 3
	}`
	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "which" {
			return "/usr/bin/glab", nil
		}
		if name == "glab" && len(args) > 0 && args[0] == "mr" {
			return mrViewJSON, nil
		}
		if name == "glab" && len(args) > 0 && args[0] == "api" {
			for _, a := range args {
				if containsSubstr(a, "discussions") {
					return `[]`, nil
				}
				if containsSubstr(a, "notes") {
					return `[]`, nil
				}
			}
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	approvalState, prState, _, _, err := pollGitLab(runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approvalState != "approved" {
		t.Errorf("want approved, got %q", approvalState)
	}
	if prState != "merged" {
		t.Errorf("want merged, got %q", prState)
	}
}

func TestSeenIDsPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seen.json")

	// Write some IDs
	initial := map[string]bool{"1": true, "2": true, "3": true}
	saveSeenIDs(path, initial)

	// Reload
	loaded := loadSeenIDs(path)
	if len(loaded) != 3 {
		t.Fatalf("want 3 seen IDs, got %d", len(loaded))
	}
	if !loaded["1"] || !loaded["2"] || !loaded["3"] {
		t.Error("missing expected IDs after reload")
	}

	// Filter a comment list
	comments := []Comment{
		{ID: "1", Body: "old"},
		{ID: "4", Body: "new"},
	}
	newComments := filterNew(comments, loaded)
	if len(newComments) != 1 {
		t.Fatalf("want 1 new comment, got %d", len(newComments))
	}
	if newComments[0].ID != "4" {
		t.Errorf("want ID 4, got %q", newComments[0].ID)
	}
}

func TestMissingCLI(t *testing.T) {
	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "which" {
			return "", fmt.Errorf("not found")
		}
		return "", fmt.Errorf("unexpected call")
	}

	// Test GitHub missing CLI
	_, _, _, _, err := pollGitHub(runner)
	if err == nil {
		t.Fatal("want error for missing gh, got nil")
	}

	// Test GitLab missing CLI
	_, _, _, _, err = pollGitLab(runner)
	if err == nil {
		t.Fatal("want error for missing glab, got nil")
	}
}

func TestStatusLogic_Approved(t *testing.T) {
	// Simulate the status determination logic from main()
	approvalState := "APPROVED"
	prState := "OPEN"
	plt := "github"

	status := computeStatus(plt, approvalState, prState)
	if status != "approved" {
		t.Errorf("want approved, got %q", status)
	}
}

func TestStatusLogic_Merged(t *testing.T) {
	status := computeStatus("github", "REVIEW_REQUIRED", "MERGED")
	if status != "merged" {
		t.Errorf("want merged, got %q", status)
	}
}

func TestLoadSeenIDs_MissingFile(t *testing.T) {
	seen := loadSeenIDs("/nonexistent/path/seen.json")
	if seen == nil {
		t.Fatal("want empty map, got nil")
	}
	if len(seen) != 0 {
		t.Errorf("want empty map, got %d entries", len(seen))
	}
}

func TestLoadSeenIDs_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	_ = os.WriteFile(path, []byte("not json"), 0o644)

	seen := loadSeenIDs(path)
	if seen == nil {
		t.Fatal("want empty map, got nil")
	}
	if len(seen) != 0 {
		t.Errorf("want empty map from corrupt file, got %d entries", len(seen))
	}
}

func TestOutput_JSONSchema(t *testing.T) {
	out := Output{
		Platform:          "github",
		Status:            "ok",
		ApprovalState:     "REVIEW_REQUIRED",
		PRState:           "OPEN",
		NewInlineComments: []Comment{{ID: "1", Path: "f.go", Line: 1, Body: "hi", Author: "u", CreatedAt: "2026-01-01T00:00:00Z"}},
		NewPRComments:     []Comment{},
		TotalSeen:         5,
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	for _, field := range []string{"platform", "status", "approval_state", "pr_state", "new_inline_comments", "new_pr_comments", "total_seen"} {
		if _, ok := m[field]; !ok {
			t.Errorf("missing field %q in output JSON", field)
		}
	}
}

// computeStatus mirrors the status logic in main() for testing.
func computeStatus(plt, approvalState, prState string) string {
	switch {
	case approvalState == "APPROVED" || (plt == "gitlab" && approvalState == "approved"):
		return "approved"
	case prState == "MERGED" || prState == "merged":
		return "merged"
	}
	return "ok"
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstrHelper(s, sub))
}

func containsSubstrHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
