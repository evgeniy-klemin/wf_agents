package main

import (
	"encoding/json"
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
	ID        string `json:"id"`
	Path      string `json:"path,omitempty"`
	Line      int    `json:"line,omitempty"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
}

// Output is the JSON structure printed to stdout.
type Output struct {
	Platform          string    `json:"platform"`
	Status            string    `json:"status"`
	Error             string    `json:"error,omitempty"`
	ApprovalState     string    `json:"approval_state"`
	PRState           string    `json:"pr_state"`
	NewInlineComments []Comment `json:"new_inline_comments"`
	NewPRComments     []Comment `json:"new_pr_comments"`
	TotalSeen         int       `json:"total_seen"`
}

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		printError("unknown", "failed to get working directory: "+err.Error())
		return
	}

	plt := platform.DetectPlatform()

	workflowID := session.ResolveWorkflowIDByCWD("", cwd)
	if workflowID == "" {
		printError(plt, "no active workflow session found")
		return
	}

	sessionID := strings.TrimPrefix(workflowID, "coding-session-")
	seenFile := filepath.Join(os.TempDir(), "wf-agents-feedback", sessionID+".json")

	seen := loadSeenIDs(seenFile)

	var (
		approvalState  string
		prState        string
		inlineComments []Comment
		prComments     []Comment
		pollErr        error
	)

	switch plt {
	case "github":
		approvalState, prState, inlineComments, prComments, pollErr = pollGitHub(platform.RunCmd)
	case "gitlab":
		approvalState, prState, inlineComments, prComments, pollErr = pollGitLab(platform.RunCmd)
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

	status := "ok"
	switch {
	case approvalState == "APPROVED" || (plt == "gitlab" && approvalState == "approved"):
		status = "approved"
	case strings.ToUpper(prState) == "MERGED":
		status = "merged"
	}

	out := Output{
		Platform:          plt,
		Status:            status,
		ApprovalState:     approvalState,
		PRState:           prState,
		NewInlineComments: newInline,
		NewPRComments:     newPR,
		TotalSeen:         len(seen),
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
func pollGitHub(runner platform.CmdRunner) (approvalState, prState string, inlineComments, prComments []Comment, err error) {
	// Check gh is available
	if _, e := runner(5*time.Second, "which", "gh"); e != nil {
		err = fmt.Errorf("gh CLI not found")
		return
	}

	// Get PR metadata
	prViewOut, e := runner(15*time.Second, "gh", "pr", "view", "--json", "reviewDecision,state,number,headRepository,headRepositoryOwner")
	if e != nil {
		err = fmt.Errorf("gh pr view failed: %s", prViewOut)
		return
	}

	var prView struct {
		ReviewDecision      string                 `json:"reviewDecision"`
		State               string                 `json:"state"`
		Number              int                    `json:"number"`
		HeadRepository      struct{ Name string }  `json:"headRepository"`
		HeadRepositoryOwner struct{ Login string } `json:"headRepositoryOwner"`
	}
	if e := json.Unmarshal([]byte(prViewOut), &prView); e != nil {
		err = fmt.Errorf("failed to parse gh pr view output: %v", e)
		return
	}

	approvalState = prView.ReviewDecision
	prState = prView.State
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
		ID        int                    `json:"id"`
		Path      string                 `json:"path"`
		Line      int                    `json:"line"`
		Body      string                 `json:"body"`
		User      struct{ Login string } `json:"user"`
		CreatedAt string                 `json:"created_at"`
	}
	if e := json.Unmarshal([]byte(inlineOut), &rawInline); e != nil {
		err = fmt.Errorf("failed to parse inline comments: %v", e)
		return
	}
	for _, c := range rawInline {
		inlineComments = append(inlineComments, Comment{
			ID:        fmt.Sprintf("%d", c.ID),
			Path:      c.Path,
			Line:      c.Line,
			Body:      c.Body,
			Author:    c.User.Login,
			CreatedAt: c.CreatedAt,
		})
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

	return
}

// pollGitLab fetches MR review state and comments using the glab CLI.
func pollGitLab(runner platform.CmdRunner) (approvalState, prState string, inlineComments, prComments []Comment, err error) {
	// Check glab is available
	if _, e := runner(5*time.Second, "which", "glab"); e != nil {
		err = fmt.Errorf("glab CLI not found")
		return
	}

	// Get MR metadata
	mrViewOut, e := runner(15*time.Second, "glab", "mr", "view", "-F", "json")
	if e != nil {
		err = fmt.Errorf("glab mr view failed: %s", mrViewOut)
		return
	}

	var mrView struct {
		State      string                      `json:"state"`
		ApprovedBy []struct{ Username string } `json:"approved_by"`
		ProjectID  int                         `json:"project_id"`
		IID        int                         `json:"iid"`
	}
	if e := json.Unmarshal([]byte(mrViewOut), &mrView); e != nil {
		err = fmt.Errorf("failed to parse glab mr view output: %v", e)
		return
	}

	prState = mrView.State
	if len(mrView.ApprovedBy) > 0 {
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
		Notes []struct {
			ID       int                       `json:"id"`
			Body     string                    `json:"body"`
			Author   struct{ Username string } `json:"author"`
			Position *struct {
				NewPath string `json:"new_path"`
				NewLine int    `json:"new_line"`
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
			inlineComments = append(inlineComments, Comment{
				ID:        fmt.Sprintf("%d", n.ID),
				Path:      n.Position.NewPath,
				Line:      n.Position.NewLine,
				Body:      n.Body,
				Author:    n.Author.Username,
				CreatedAt: n.CreatedAt,
			})
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
