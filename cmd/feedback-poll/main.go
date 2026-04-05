package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eklemin/wf-agents/internal/platform"
	"github.com/eklemin/wf-agents/internal/session"
)

// Comment represents a single comment (inline or PR-level).
type Comment struct {
	ID           string `json:"id"`
	Path         string `json:"path,omitempty"`
	Line         int    `json:"line,omitempty"`
	Body         string `json:"body"`
	Author       string `json:"author"`
	CreatedAt    string `json:"created_at"`
	DiscussionID string `json:"discussion_id,omitempty"`
	EndLine      int    `json:"end_line,omitempty"`
	OldPath      string `json:"old_path,omitempty"`
	OldLine      int    `json:"old_line,omitempty"`
}

// Output is the JSON structure printed to stdout.
type Output struct {
	Platform          string    `json:"platform"`
	Status            string    `json:"status"`
	Error             string    `json:"error,omitempty"`
	ApprovalState     string    `json:"approval_state"`
	PRState           string    `json:"pr_state"`
	MRDraft           bool      `json:"mr_draft"`
	ApprovedBy        []string  `json:"approved_by"`
	NewInlineComments []Comment `json:"new_inline_comments"`
	NewPRComments     []Comment `json:"new_pr_comments"`
	TotalSeen         int       `json:"total_seen"`
}

func main() {
	mrFlag := flag.Int("mr", 0, "MR iid to poll; defaults to auto-detect from current branch")
	repoFlag := flag.String("repo", "", "Path to the git repository (worktree); used for platform detection and glab/gh commands")
	flag.Parse()

	// When --repo is set, run all external commands in that directory.
	runner := platform.RunCmd
	repoDir := *repoFlag
	if repoDir != "" {
		dir := repoDir
		runner = func(timeout time.Duration, name string, args ...string) (string, error) {
			return platform.RunCmdInDir(timeout, dir, name, args...)
		}
	}

	cwd := repoDir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			printError("unknown", "failed to get working directory: "+err.Error())
			return
		}
	}

	// Detect platform using the repo dir (or CWD if --repo not set).
	var plt string
	if repoDir != "" {
		out, err := runner(5*time.Second, "git", "remote", "get-url", "origin")
		if err != nil {
			plt = "unknown"
		} else {
			plt = platform.ParsePlatformFromURL(strings.TrimSpace(out))
		}
	} else {
		plt = platform.DetectPlatform()
	}

	workflowID := session.ResolveWorkflowIDByCWD("", cwd)
	if workflowID == "" {
		printError(plt, "no active workflow session found")
		return
	}

	sessionID := strings.TrimPrefix(workflowID, "coding-session-")
	seenFile := filepath.Join(os.TempDir(), "wf-agents-feedback", sessionID+".json")

	seen := loadSeenIDs(seenFile)

	var (
		draft          bool
		approvalState  string
		prState        string
		approvers      []string
		inlineComments []Comment
		prComments     []Comment
		pollErr        error
	)

	switch plt {
	case "github":
		approvalState, prState, draft, approvers, inlineComments, prComments, pollErr = pollGitHub(runner)
	case "gitlab":
		approvalState, prState, draft, approvers, inlineComments, prComments, pollErr = pollGitLab(runner, *mrFlag)
	default:
		printError(plt, "unsupported platform: "+plt)
		return
	}

	if pollErr != nil {
		printError(plt, pollErr.Error())
		return
	}

	newInline := filterNew(inlineComments, seen)
	newPR := filterNew(prComments, seen)

	for _, c := range newInline {
		seen[c.ID] = true
	}
	for _, c := range newPR {
		seen[c.ID] = true
	}
	saveSeenIDs(seenFile, seen)

	status := "draft"
	switch {
	case approvalState == "APPROVED" || (plt == "gitlab" && approvalState == "approved"):
		status = "approved"
	case strings.ToUpper(prState) == "MERGED":
		status = "merged"
	case !draft:
		status = "ready"
	}

	out := Output{
		Platform:          plt,
		Status:            status,
		ApprovalState:     approvalState,
		PRState:           prState,
		MRDraft:           draft,
		ApprovedBy:        approvers,
		NewInlineComments: newInline,
		NewPRComments:     newPR,
		TotalSeen:         len(seen),
	}
	if out.ApprovedBy == nil {
		out.ApprovedBy = []string{}
	}
	if out.NewInlineComments == nil {
		out.NewInlineComments = []Comment{}
	}
	if out.NewPRComments == nil {
		out.NewPRComments = []Comment{}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func printError(plt, msg string) {
	out := Output{
		Platform:          plt,
		Status:            "error",
		Error:             msg,
		NewInlineComments: []Comment{},
		NewPRComments:     []Comment{},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// pollGitHub fetches PR review state and comments using the gh CLI.
func pollGitHub(runner platform.CmdRunner) (approvalState, prState string, draft bool, approvers []string, inlineComments, prComments []Comment, err error) {
	// Check gh is available
	if _, e := runner(5*time.Second, "which", "gh"); e != nil {
		err = fmt.Errorf("gh CLI not found")
		return
	}

	// Get PR metadata
	prViewOut, e := runner(15*time.Second, "gh", "pr", "view", "--json", "reviewDecision,state,number,isDraft,headRepository,headRepositoryOwner")
	if e != nil {
		err = fmt.Errorf("gh pr view failed: %s", prViewOut)
		return
	}

	var prView struct {
		ReviewDecision      string                 `json:"reviewDecision"`
		State               string                 `json:"state"`
		Number              int                    `json:"number"`
		IsDraft             bool                   `json:"isDraft"`
		HeadRepository      struct{ Name string }  `json:"headRepository"`
		HeadRepositoryOwner struct{ Login string } `json:"headRepositoryOwner"`
	}
	if e := json.Unmarshal([]byte(prViewOut), &prView); e != nil {
		err = fmt.Errorf("failed to parse gh pr view output: %v", e)
		return
	}

	approvalState = prView.ReviewDecision
	prState = prView.State
	draft = prView.IsDraft
	owner := prView.HeadRepositoryOwner.Login
	repo := prView.HeadRepository.Name
	number := prView.Number

	// Get inline review comments
	inlineOut, e := runner(15*time.Second, "gh", "api",
		fmt.Sprintf("repos/%s/%s/pulls/%d/comments", owner, repo, number))
	if e != nil {
		err = fmt.Errorf("gh api inline comments failed: %s", inlineOut)
		return
	}

	var rawInline []struct {
		ID                  int                    `json:"id"`
		Path                string                 `json:"path"`
		Line                int                    `json:"line"`
		StartLine           int                    `json:"start_line"`
		Body                string                 `json:"body"`
		User                struct{ Login string } `json:"user"`
		CreatedAt           string                 `json:"created_at"`
		PullRequestReviewID int64                  `json:"pull_request_review_id"`
	}
	if e := json.Unmarshal([]byte(inlineOut), &rawInline); e != nil {
		err = fmt.Errorf("failed to parse inline comments: %v", e)
		return
	}
	for _, c := range rawInline {
		comment := Comment{
			ID:        fmt.Sprintf("%d", c.ID),
			Path:      c.Path,
			Line:      c.Line,
			Body:      c.Body,
			Author:    c.User.Login,
			CreatedAt: c.CreatedAt,
		}
		if c.PullRequestReviewID != 0 {
			comment.DiscussionID = fmt.Sprintf("%d", c.PullRequestReviewID)
		}
		// When start_line is set, it marks a multi-line range: start_line→line
		if c.StartLine != 0 {
			comment.Line = c.StartLine
			comment.EndLine = c.Line
		}
		inlineComments = append(inlineComments, comment)
	}

	// Get PR-level comments
	prCommentsOut, e := runner(15*time.Second, "gh", "pr", "view", "--json", "comments")
	if e != nil {
		err = fmt.Errorf("gh pr view comments failed: %s", prCommentsOut)
		return
	}

	var prCommentsWrapper struct {
		Comments []struct {
			ID        int                    `json:"id"`
			Body      string                 `json:"body"`
			Author    struct{ Login string } `json:"author"`
			CreatedAt string                 `json:"createdAt"`
		} `json:"comments"`
	}
	if e := json.Unmarshal([]byte(prCommentsOut), &prCommentsWrapper); e != nil {
		err = fmt.Errorf("failed to parse PR comments: %v", e)
		return
	}
	for _, c := range prCommentsWrapper.Comments {
		prComments = append(prComments, Comment{
			ID:        fmt.Sprintf("%d", c.ID),
			Body:      c.Body,
			Author:    c.Author.Login,
			CreatedAt: c.CreatedAt,
		})
	}

	// Get approvers from latest reviews
	reviewsOut, e := runner(15*time.Second, "gh", "pr", "view", "--json", "latestReviews")
	if e != nil {
		err = fmt.Errorf("gh pr view latestReviews failed: %s", reviewsOut)
		return
	}

	var reviewsWrapper struct {
		LatestReviews []struct {
			State  string                 `json:"state"`
			Author struct{ Login string } `json:"author"`
		} `json:"latestReviews"`
	}
	if e := json.Unmarshal([]byte(reviewsOut), &reviewsWrapper); e != nil {
		err = fmt.Errorf("failed to parse latestReviews: %v", e)
		return
	}
	for _, r := range reviewsWrapper.LatestReviews {
		if r.State == "APPROVED" {
			approvers = append(approvers, r.Author.Login)
		}
	}

	return
}

// pollGitLab fetches MR review state and comments using the glab CLI.
// mrIID, when non-zero, is passed explicitly to glab commands instead of auto-detecting from branch.
func pollGitLab(runner platform.CmdRunner, mrIID int) (approvalState, prState string, draft bool, approvers []string, inlineComments, prComments []Comment, err error) {
	// Check glab is available
	if _, e := runner(5*time.Second, "which", "glab"); e != nil {
		err = fmt.Errorf("glab CLI not found")
		return
	}

	// Get MR metadata
	var mrViewOut string
	var e error
	if mrIID != 0 {
		mrViewOut, e = runner(15*time.Second, "glab", "mr", "view", fmt.Sprintf("%d", mrIID), "-F", "json")
	} else {
		mrViewOut, e = runner(15*time.Second, "glab", "mr", "view", "-F", "json")
	}
	if e != nil {
		err = fmt.Errorf("glab mr view failed: %s", mrViewOut)
		return
	}

	var mrView struct {
		State     string `json:"state"`
		ProjectID int    `json:"project_id"`
		IID       int    `json:"iid"`
		Draft     bool   `json:"draft"`
	}
	if e := json.Unmarshal([]byte(mrViewOut), &mrView); e != nil {
		err = fmt.Errorf("failed to parse glab mr view output: %v", e)
		return
	}

	prState = mrView.State
	draft = mrView.Draft

	// Fetch approval status from dedicated endpoint
	approvalsOut, e := runner(15*time.Second, "glab", "api",
		fmt.Sprintf("projects/%d/merge_requests/%d/approvals", mrView.ProjectID, mrView.IID))
	if e != nil {
		err = fmt.Errorf("glab api approvals failed: %s", approvalsOut)
		return
	}

	var approvalsResp struct {
		ApprovedBy []struct {
			User struct {
				Username string `json:"username"`
				Email    string `json:"email"`
			} `json:"user"`
		} `json:"approved_by"`
	}
	if e := json.Unmarshal([]byte(approvalsOut), &approvalsResp); e != nil {
		err = fmt.Errorf("failed to parse approvals: %v", e)
		return
	}

	for _, a := range approvalsResp.ApprovedBy {
		if a.User.Email != "" {
			approvers = append(approvers, a.User.Email)
		} else {
			approvers = append(approvers, a.User.Username)
		}
	}

	if len(approvers) > 0 {
		approvalState = "approved"
	} else {
		approvalState = "pending"
	}

	// Get MR notes (PR-level comments)
	notesOut, e := runner(15*time.Second, "glab", "api",
		fmt.Sprintf("projects/%d/merge_requests/%d/notes?per_page=100", mrView.ProjectID, mrView.IID))
	if e != nil {
		err = fmt.Errorf("glab api notes failed: %s", notesOut)
		return
	}

	var notes []struct {
		ID        int                       `json:"id"`
		Body      string                    `json:"body"`
		Author    struct{ Username string } `json:"author"`
		CreatedAt string                    `json:"created_at"`
		System    bool                      `json:"system"`
	}
	if e := json.Unmarshal([]byte(notesOut), &notes); e != nil {
		err = fmt.Errorf("failed to parse notes: %v", e)
		return
	}
	for _, n := range notes {
		if n.System {
			continue
		}
		prComments = append(prComments, Comment{
			ID:        fmt.Sprintf("%d", n.ID),
			Body:      n.Body,
			Author:    n.Author.Username,
			CreatedAt: n.CreatedAt,
		})
	}

	// Get inline discussion comments
	discussionsOut, e := runner(15*time.Second, "glab", "api",
		fmt.Sprintf("projects/%d/merge_requests/%d/discussions?per_page=100", mrView.ProjectID, mrView.IID))
	if e != nil {
		err = fmt.Errorf("glab api discussions failed: %s", discussionsOut)
		return
	}

	var discussions []struct {
		ID    string `json:"id"`
		Notes []struct {
			ID       int                       `json:"id"`
			Body     string                    `json:"body"`
			Author   struct{ Username string } `json:"author"`
			Position *struct {
				NewPath string `json:"new_path"`
				NewLine int    `json:"new_line"`
				OldPath string `json:"old_path"`
				OldLine int    `json:"old_line"`
				LineRange *struct {
					Start struct {
						NewLine int `json:"new_line"`
					} `json:"start"`
					End struct {
						NewLine int `json:"new_line"`
					} `json:"end"`
				} `json:"line_range"`
			} `json:"position"`
			CreatedAt string `json:"created_at"`
			System    bool   `json:"system"`
		} `json:"notes"`
	}
	if e := json.Unmarshal([]byte(discussionsOut), &discussions); e != nil {
		err = fmt.Errorf("failed to parse discussions: %v", e)
		return
	}
	for _, d := range discussions {
		for _, n := range d.Notes {
			if n.System || n.Position == nil {
				continue
			}
			comment := Comment{
				ID:           fmt.Sprintf("%d", n.ID),
				Path:         n.Position.NewPath,
				Line:         n.Position.NewLine,
				Body:         n.Body,
				Author:       n.Author.Username,
				CreatedAt:    n.CreatedAt,
				DiscussionID: d.ID,
				OldPath:      n.Position.OldPath,
				OldLine:      n.Position.OldLine,
			}
			if n.Position.LineRange != nil {
				comment.Line = n.Position.LineRange.Start.NewLine
				comment.EndLine = n.Position.LineRange.End.NewLine
			}
			inlineComments = append(inlineComments, comment)
		}
	}

	return
}

// loadSeenIDs reads a JSON map of seen comment IDs from disk.
func loadSeenIDs(path string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return make(map[string]bool)
	}
	var m map[string]bool
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]bool)
	}
	return m
}

// saveSeenIDs persists the seen IDs map to disk.
func saveSeenIDs(path string, ids map[string]bool) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// filterNew returns only comments whose IDs are not in seen.
func filterNew(comments []Comment, seen map[string]bool) []Comment {
	var result []Comment
	for _, c := range comments {
		if !seen[c.ID] {
			result = append(result, c)
		}
	}
	return result
}
