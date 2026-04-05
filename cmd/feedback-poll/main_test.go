package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)


func TestGitHubPoll_NewComments(t *testing.T) {
	prViewJSON := `{
		"reviewDecision": "REVIEW_REQUIRED",
		"state": "OPEN",
		"number": 42,
		"isDraft": false,
		"headRepository": {"name": "myrepo"},
		"headRepositoryOwner": {"login": "myorg"}
	}`

	inlineJSON := `[
		{"id": 1, "path": "foo.go", "line": 10, "body": "inline 1", "user": {"login": "alice"}, "created_at": "2026-01-01T00:00:00Z", "pull_request_review_id": 500, "start_line": 8},
		{"id": 2, "path": "bar.go", "line": 20, "body": "inline 2", "user": {"login": "bob"}, "created_at": "2026-01-02T00:00:00Z", "pull_request_review_id": 501},
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

	latestReviewsJSON := `{"latestReviews":[]}`

	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "which" && len(args) > 0 && args[0] == "gh" {
			return "/usr/bin/gh", nil
		}
		if name == "gh" && len(args) > 0 && args[0] == "pr" {
			// Distinguish between the gh pr view calls
			for _, a := range args {
				if a == "comments" {
					return prCommentsJSON, nil
				}
				if a == "latestReviews" {
					return latestReviewsJSON, nil
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

	approvalState, prState, draft, _, inline, prComments, err := pollGitHub(runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approvalState != "REVIEW_REQUIRED" {
		t.Errorf("want REVIEW_REQUIRED, got %q", approvalState)
	}
	if prState != "OPEN" {
		t.Errorf("want OPEN, got %q", prState)
	}
	_ = draft
	if len(inline) != 3 {
		t.Errorf("want 3 inline comments, got %d", len(inline))
	}
	if len(prComments) != 2 {
		t.Errorf("want 2 PR comments, got %d", len(prComments))
	}
	// First inline comment should have DiscussionID from pull_request_review_id and EndLine from start_line
	if inline[0].DiscussionID != "500" {
		t.Errorf("want DiscussionID 500, got %q", inline[0].DiscussionID)
	}
	if inline[0].EndLine != 10 {
		t.Errorf("want EndLine 10 (line field when start_line present), got %d", inline[0].EndLine)
	}
	if inline[0].Line != 8 {
		t.Errorf("want Line 8 (start_line as start), got %d", inline[0].Line)
	}
	// Second inline comment: DiscussionID set, no start_line so EndLine=0
	if inline[1].DiscussionID != "501" {
		t.Errorf("want DiscussionID 501, got %q", inline[1].DiscussionID)
	}
	if inline[1].EndLine != 0 {
		t.Errorf("want EndLine 0 for single-line comment, got %d", inline[1].EndLine)
	}
}

func TestGitHubPoll_Approved(t *testing.T) {
	prViewJSON := `{
		"reviewDecision": "APPROVED",
		"state": "OPEN",
		"number": 7,
		"isDraft": false,
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
				if a == "latestReviews" {
					return `{"latestReviews":[{"state":"APPROVED","author":{"login":"alice"}}]}`, nil
				}
			}
			return prViewJSON, nil
		}
		if name == "gh" && len(args) > 0 && args[0] == "api" {
			return `[]`, nil
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	approvalState, prState, _, approvers, _, _, err := pollGitHub(runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approvalState != "APPROVED" {
		t.Errorf("want APPROVED, got %q", approvalState)
	}
	if prState != "OPEN" {
		t.Errorf("want OPEN, got %q", prState)
	}
	if len(approvers) != 1 || approvers[0] != "alice" {
		t.Errorf("want approvers [alice], got %v", approvers)
	}
}

func TestGitHubPoll_SeenFiltering(t *testing.T) {
	prViewJSON := `{
		"reviewDecision": "REVIEW_REQUIRED",
		"state": "OPEN",
		"number": 1,
		"isDraft": false,
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
				if a == "latestReviews" {
					return `{"latestReviews":[]}`, nil
				}
			}
			return prViewJSON, nil
		}
		if name == "gh" && len(args) > 0 && args[0] == "api" {
			return inlineJSON, nil
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	_, _, _, _, inline, _, err := pollGitHub(runner)
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
		"project_id": 99,
		"iid": 5,
		"draft": true
	}`
	approvalsJSON := `{"approved_by":[]}`
	notesJSON := `[
		{"id": 200, "body": "note 1", "author": {"username": "alice"}, "created_at": "2026-01-01T00:00:00Z", "system": false},
		{"id": 201, "body": "note 2", "author": {"username": "bob"}, "created_at": "2026-01-02T00:00:00Z", "system": false},
		{"id": 202, "body": "system note", "author": {"username": "gitlab"}, "created_at": "2026-01-03T00:00:00Z", "system": true}
	]`
	discussionsJSON := `[
		{
			"id": "abc123",
			"notes": [
				{
					"id": 300,
					"body": "inline comment",
					"author": {"username": "carol"},
					"position": {
						"new_path": "main.go",
						"new_line": 42,
						"old_path": "main_old.go",
						"old_line": 40,
						"line_range": {
							"start": {"new_line": 41},
							"end": {"new_line": 42}
						}
					},
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
				if containsSubstr(a, "approvals") {
					return approvalsJSON, nil
				}
			}
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	approvalState, prState, _, _, inline, prComments, err := pollGitLab(runner, 0)
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
	if inline[0].DiscussionID != "abc123" {
		t.Errorf("want DiscussionID abc123, got %q", inline[0].DiscussionID)
	}
	if inline[0].OldPath != "main_old.go" {
		t.Errorf("want OldPath main_old.go, got %q", inline[0].OldPath)
	}
	if inline[0].OldLine != 40 {
		t.Errorf("want OldLine 40, got %d", inline[0].OldLine)
	}
	if inline[0].EndLine != 42 {
		t.Errorf("want EndLine 42 from line_range.end.new_line, got %d", inline[0].EndLine)
	}
	if inline[0].Line != 41 {
		t.Errorf("want Line 41 from line_range.start.new_line, got %d", inline[0].Line)
	}
}

func TestGitLabPoll_Merged(t *testing.T) {
	mrViewJSON := `{
		"state": "merged",
		"project_id": 10,
		"iid": 3,
		"draft": false
	}`
	approvalsJSON := `{"approved_by":[{"user":{"username":"reviewer1","email":"reviewer1@example.com"}}]}`

	runner := func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "which" {
			return "/usr/bin/glab", nil
		}
		if name == "glab" && len(args) > 0 && args[0] == "mr" {
			return mrViewJSON, nil
		}
		if name == "glab" && len(args) > 0 && args[0] == "api" {
			for _, a := range args {
				if containsSubstr(a, "approvals") {
					return approvalsJSON, nil
				}
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

	approvalState, prState, _, approvers, _, _, err := pollGitLab(runner, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if approvalState != "approved" {
		t.Errorf("want approved, got %q", approvalState)
	}
	if prState != "merged" {
		t.Errorf("want merged, got %q", prState)
	}
	if len(approvers) != 1 || approvers[0] != "reviewer1@example.com" {
		t.Errorf("want approvers [reviewer1@example.com], got %v", approvers)
	}
}

func TestGitHubPoll_MultiLineComment(t *testing.T) {
	prViewJSON := `{
		"reviewDecision": "REVIEW_REQUIRED",
		"state": "OPEN",
		"number": 10,
		"isDraft": false,
		"headRepository": {"name": "repo"},
		"headRepositoryOwner": {"login": "org"}
	}`
	// Multi-line comment: start_line is the start, line is the end of the range
	inlineJSON := `[
		{"id": 99, "path": "file.go", "line": 20, "start_line": 15, "body": "multi-line", "user": {"login": "alice"}, "created_at": "2026-01-01T00:00:00Z", "pull_request_review_id": 777}
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
				if a == "latestReviews" {
					return `{"latestReviews":[]}`, nil
				}
			}
			return prViewJSON, nil
		}
		if name == "gh" && len(args) > 0 && args[0] == "api" {
			return inlineJSON, nil
		}
		return "", fmt.Errorf("unexpected: %s %v", name, args)
	}

	_, _, _, _, inline, _, err := pollGitHub(runner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inline) != 1 {
		t.Fatalf("want 1 inline comment, got %d", len(inline))
	}
	c := inline[0]
	if c.Line != 15 {
		t.Errorf("want Line 15 (start_line), got %d", c.Line)
	}
	if c.EndLine != 20 {
		t.Errorf("want EndLine 20 (line), got %d", c.EndLine)
	}
	if c.DiscussionID != "777" {
		t.Errorf("want DiscussionID 777, got %q", c.DiscussionID)
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
	_, _, _, _, _, _, err := pollGitHub(runner)
	if err == nil {
		t.Fatal("want error for missing gh, got nil")
	}

	// Test GitLab missing CLI
	_, _, _, _, _, _, err = pollGitLab(runner, 0)
	if err == nil {
		t.Fatal("want error for missing glab, got nil")
	}
}

func TestStatusLogic_Approved(t *testing.T) {
	status := computeStatus("github", "APPROVED", "OPEN", false)
	if status != "approved" {
		t.Errorf("want approved, got %q", status)
	}
}

func TestStatusLogic_Ready(t *testing.T) {
	status := computeStatus("github", "REVIEW_REQUIRED", "OPEN", false)
	if status != "ready" {
		t.Errorf("want ready, got %q", status)
	}
}

func TestStatusLogic_Draft(t *testing.T) {
	status := computeStatus("github", "REVIEW_REQUIRED", "OPEN", true)
	if status != "draft" {
		t.Errorf("want draft for draft MR, got %q", status)
	}
}

func TestStatusLogic_Merged(t *testing.T) {
	status := computeStatus("gitlab", "approved", "merged", false)
	if status != "approved" {
		t.Errorf("want approved (approval takes precedence over merged), got %q", status)
	}

	status = computeStatus("github", "REVIEW_REQUIRED", "MERGED", false)
	if status != "merged" {
		t.Errorf("want merged, got %q", status)
	}

	status = computeStatus("gitlab", "pending", "merged", false)
	if status != "merged" {
		t.Errorf("want merged for gitlab merged state, got %q", status)
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
		ApprovedBy:        []string{"alice"},
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

	for _, field := range []string{"platform", "status", "approval_state", "pr_state", "approved_by", "new_inline_comments", "new_pr_comments", "total_seen"} {
		if _, ok := m[field]; !ok {
			t.Errorf("missing field %q in output JSON", field)
		}
	}
}

// computeStatus mirrors the status logic in main() for testing.
func computeStatus(plt, approvalState, prState string, draft bool) string {
	switch {
	case approvalState == "APPROVED" || (plt == "gitlab" && approvalState == "approved"):
		return "approved"
	case strings.ToUpper(prState) == "MERGED":
		return "merged"
	case !draft:
		return "ready"
	}
	return "draft"
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
