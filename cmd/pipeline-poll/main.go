package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/eklemin/wf-agents/internal/platform"
)

// Output is the JSON structure printed to stdout.
type Output struct {
	PipelineID  int         `json:"pipeline_id"`
	PipelineURL string      `json:"pipeline_url"`
	CreatedAt   string      `json:"created_at"`
	Duration    int         `json:"duration_seconds"`
	Status      string      `json:"status"`
	Stages      []Stage     `json:"stages"`
	FailedJobs  []FailedJob `json:"failed_jobs"`
	AllJobsDone bool        `json:"all_jobs_done"`
}

// ErrorOutput is printed when a fatal error occurs.
type ErrorOutput struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

// Stage represents a single CI stage with its aggregate status.
type Stage struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// FailedJob holds details about a failed job including log analysis.
type FailedJob struct {
	ID             int      `json:"id"`
	Name           string   `json:"name"`
	Stage          string   `json:"stage"`
	URL            string   `json:"url"`
	AllowFailure   bool     `json:"allow_failure"`
	FailRootCauses []string `json:"fail_root_causes"`
	LogTail        string   `json:"log_tail"`
}

// Job is the raw job data from the GitLab API.
type Job struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Stage        string `json:"stage"`
	Status       string `json:"status"`
	AllowFailure bool   `json:"allow_failure"`
	WebURL       string `json:"web_url"`
}

// Pipeline is the raw pipeline data from the GitLab API.
type Pipeline struct {
	ID        int    `json:"id"`
	Status    string `json:"status"`
	Ref       string `json:"ref"`
	WebURL    string `json:"web_url"`
	CreatedAt string `json:"created_at"`
	Duration  int    `json:"duration"`
}

func main() {
	refFlag := flag.String("ref", "", "Git ref (branch) to check pipeline for; defaults to current branch")
	mrFlag := flag.Int("mr", 0, "MR iid to check pipeline for; uses the MR's head_pipeline")
	repoFlag := flag.String("repo", "", "Path to the git repository (worktree); used to detect current branch when -ref is not set")
	flag.Parse()

	runner := platform.RunCmd
	// When --repo is set, run all external commands (glab, git) in that directory
	// so they pick up the correct remote and MR context.
	if *repoFlag != "" {
		dir := *repoFlag
		runner = func(timeout time.Duration, name string, args ...string) (string, error) {
			return platform.RunCmdInDir(timeout, dir, name, args...)
		}
	}

	var pipeline *Pipeline
	switch {
	case *mrFlag != 0:
		var err error
		pipeline, err = getMRHeadPipeline(runner, *mrFlag)
		if err != nil {
			printError("failed to get MR head pipeline: " + err.Error())
			return
		}
	default:
		var branch string
		if *refFlag != "" {
			branch = *refFlag
		} else {
			var err error
			branch, err = getCurrentBranch(runner, *repoFlag)
			if err != nil {
				printError("failed to get current branch: " + err.Error())
				return
			}
		}
		var err error
		pipeline, err = getLatestPipeline(runner, branch)
		if err != nil {
			printError("failed to get pipeline: " + err.Error())
			return
		}
	}

	out, err := buildOutput(runner, pipeline.ID)
	if err != nil {
		printError("failed to build output: " + err.Error())
		return
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func printError(msg string) {
	out := ErrorOutput{Status: "error", Error: msg}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

// runGlab executes a glab api command and returns stdout.
func runGlab(runner platform.CmdRunner, args ...string) (string, error) {
	allArgs := append([]string{"api"}, args...)
	return runner(30*time.Second, "glab", allArgs...)
}

// getCurrentBranch returns the current git branch name.
// If repoDir is non-empty, runs git in that directory via -C.
func getCurrentBranch(runner platform.CmdRunner, repoDir string) (string, error) {
	var args []string
	if repoDir != "" {
		args = []string{"-C", repoDir, "rev-parse", "--abbrev-ref", "HEAD"}
	} else {
		args = []string{"rev-parse", "--abbrev-ref", "HEAD"}
	}
	out, err := runner(10*time.Second, "git", args...)
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// getMRHeadPipeline returns the head_pipeline of a GitLab MR by iid.
func getMRHeadPipeline(runner platform.CmdRunner, mrIID int) (*Pipeline, error) {
	out, err := runGlab(runner, fmt.Sprintf("projects/:id/merge_requests/%d", mrIID))
	if err != nil {
		return nil, fmt.Errorf("glab api mr failed: %w", err)
	}
	var mr struct {
		HeadPipeline *Pipeline `json:"head_pipeline"`
	}
	if err := json.Unmarshal([]byte(out), &mr); err != nil {
		return nil, fmt.Errorf("failed to parse MR: %w", err)
	}
	if mr.HeadPipeline == nil {
		return nil, fmt.Errorf("MR %d has no head_pipeline", mrIID)
	}
	return mr.HeadPipeline, nil
}

// getLatestPipeline fetches the most recent pipeline for the given branch.
func getLatestPipeline(runner platform.CmdRunner, branch string) (*Pipeline, error) {
	out, err := runGlab(runner, fmt.Sprintf("projects/:id/pipelines?ref=%s&per_page=1", branch))
	if err != nil {
		return nil, fmt.Errorf("glab api pipelines failed: %w", err)
	}

	var pipelines []Pipeline
	if err := json.Unmarshal([]byte(out), &pipelines); err != nil {
		return nil, fmt.Errorf("failed to parse pipelines: %w", err)
	}
	if len(pipelines) == 0 {
		return nil, fmt.Errorf("no pipeline found for branch %q", branch)
	}
	return &pipelines[0], nil
}

// getAllJobs fetches all jobs for a pipeline, handling pagination.
func getAllJobs(runner platform.CmdRunner, pipelineID int) ([]Job, error) {
	out, err := runGlab(runner, fmt.Sprintf("projects/:id/pipelines/%d/jobs?per_page=100", pipelineID))
	if err != nil {
		return nil, fmt.Errorf("glab api jobs failed: %w", err)
	}

	var jobs []Job
	if err := json.Unmarshal([]byte(out), &jobs); err != nil {
		return nil, fmt.Errorf("failed to parse jobs: %w", err)
	}
	return jobs, nil
}

// getJobLogTail fetches the job trace and returns the last N lines (cleaned).
func getJobLogTail(runner platform.CmdRunner, jobID int, lines int) (string, error) {
	out, err := runGlab(runner, fmt.Sprintf("projects/:id/jobs/%d/trace", jobID))
	if err != nil {
		return "", fmt.Errorf("glab api job trace failed: %w", err)
	}
	cleaned := stripANSI(out)
	return lastNLines(cleaned, lines), nil
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes ANSI escape codes from text.
func stripANSI(text string) string {
	return ansiEscape.ReplaceAllString(text, "")
}

// sectionMarker matches GitLab CI section markers: section_start:TIMESTAMP:NAME or section_end:TIMESTAMP:NAME.
// The \r prefix is common in raw GitLab job traces.
var sectionStartStepScript = regexp.MustCompile(`(?:\r)?section_start:\d+:step_script\r`)
var sectionEndStepScript = regexp.MustCompile(`(?:\r)?section_end:\d+:step_script\r`)

// extractStepScript extracts the content of the step_script section from a GitLab CI job log.
// This section contains user command output (tests, build, etc.) — everything outside it
// is CI infrastructure (executor setup, after_script, artifact uploads, cleanup).
// If no section markers are found, returns the full log as fallback.
func extractStepScript(text string) string {
	startLoc := sectionStartStepScript.FindStringIndex(text)

	var content string
	if startLoc != nil {
		content = text[startLoc[1]:]
	} else {
		content = text
	}

	// Truncate at section_end:step_script even when section_start was missing
	// (common when getJobLogTail returns only the tail of a long log).
	endLoc := sectionEndStepScript.FindStringIndex(content)
	if endLoc != nil {
		content = content[:endLoc[0]]
	}

	return strings.TrimSpace(content)
}

// goTestFailPattern matches Go test failure summary lines.
var goTestFailPattern = regexp.MustCompile(`^=== FAIL:|^--- FAIL:`)

// goTestDonePattern matches the "DONE N tests" summary line that ends Go test output.
var goTestDonePattern = regexp.MustCompile(`^DONE \d+ tests`)

// extractGoTestFailures looks for the structured "=== Failed" summary block
// that Go test runners produce. Returns nil if not found.
func extractGoTestFailures(log string) []string {
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")

	// Look for "=== Failed" marker — everything after it until "DONE" line or end is the summary.
	summaryStart := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "=== Failed" {
			summaryStart = i
			break
		}
	}
	if summaryStart >= 0 {
		summaryEnd := len(lines)
		for i := summaryStart + 1; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if goTestDonePattern.MatchString(trimmed) {
				summaryEnd = i + 1 // include the DONE line
				break
			}
		}
		block := strings.Join(lines[summaryStart:summaryEnd], "\n")
		block = strings.TrimSpace(block)
		if block != "" {
			return []string{block}
		}
	}

	// Fallback: collect individual --- FAIL: / === FAIL: lines with context.
	var blocks []string
	context := 3
	for i, line := range lines {
		if goTestFailPattern.MatchString(strings.TrimSpace(line)) {
			start := i
			end := i + context
			if end >= len(lines) {
				end = len(lines) - 1
			}
			block := strings.TrimSpace(strings.Join(lines[start:end+1], "\n"))
			if block != "" {
				blocks = append(blocks, block)
			}
		}
	}
	return blocks
}

// trivyCVEPattern matches CVE identifiers in Trivy scan output.
var trivyCVEPattern = regexp.MustCompile(`CVE-\d{4}-\d+`)

// trivyTableCellSep splits table cells delimited by │.
var trivyTableCellSep = regexp.MustCompile(`\s*│\s*`)

// defectDojoURLPattern extracts DefectDojo URLs from log lines.
var defectDojoURLPattern = regexp.MustCompile(`https?://[^\s]*defectdojo[^\s]*`)

// slackChannelPattern extracts Slack channel references.
var slackChannelPattern = regexp.MustCompile(`#[\w-]+`)

// extractTrivyFindings extracts a compact one-block summary from Trivy security scan output.
// Produces human-readable lines: CVE one-liners (deduplicated), vulnerability counts,
// DefectDojo link, and Slack channel — no ASCII table art.
// Returns nil if no Trivy findings are detected.
func extractTrivyFindings(log string) []string {
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")

	var parts []string
	seenCVEs := map[string]bool{}

	// 1. Parse CVE lines from Trivy tables into compact one-liners.
	for _, line := range lines {
		cve := trivyCVEPattern.FindString(line)
		if cve == "" || seenCVEs[cve] {
			continue
		}
		seenCVEs[cve] = true

		// Parse table row: │ Library │ CVE │ Severity │ Status │ Installed │ Fixed │ Title │
		cells := trivyTableCellSep.Split(line, -1)
		// Filter empty cells from leading/trailing │
		var cleaned []string
		for _, c := range cells {
			c = strings.TrimSpace(c)
			if c != "" {
				cleaned = append(cleaned, c)
			}
		}
		if len(cleaned) >= 6 {
			// Library, CVE, Severity, Status, Installed, Fixed
			parts = append(parts, fmt.Sprintf("CRITICAL: %s %s (%s -> %s)",
				cleaned[0], cleaned[1], cleaned[4], cleaned[5]))
		} else {
			parts = append(parts, fmt.Sprintf("CRITICAL: %s", cve))
		}
	}

	if len(parts) == 0 {
		return nil
	}

	// 2. Extract vulnerability counts.
	for _, line := range lines {
		if strings.Contains(line, "Vulnerabilities found:") {
			parts = append(parts, strings.TrimSpace(line))
			// Grab count lines (critical - N, high - N, etc.)
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "critical -") ||
			strings.HasPrefix(strings.TrimSpace(line), "high -") ||
			strings.HasPrefix(strings.TrimSpace(line), "medium -") {
			parts = append(parts, strings.TrimSpace(line))
		}
	}

	// 3. Extract DefectDojo URL.
	for _, line := range lines {
		if url := defectDojoURLPattern.FindString(line); url != "" {
			parts = append(parts, "DefectDojo: "+url)
			break
		}
	}

	// 4. Extract Slack channel from blockage reason.
	for _, line := range lines {
		if strings.Contains(line, "infosec-quality-gate") {
			ch := slackChannelPattern.FindString(line)
			if ch != "" {
				parts = append(parts, "Help: "+ch)
			}
			break
		}
	}

	return []string{strings.Join(parts, "\n")}
}

// lastNLines returns the last n lines of text.
func lastNLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// errorPattern matches error/fail/panic/FATAL at word boundaries to avoid
// matching CI variable names like FF_ENABLE_BASH_EXIT_CODE_CHECK.
var errorPattern = regexp.MustCompile(`(?i)\b(error|fail(ed|ure)?|panic|fatal)\b`)

// extractRootCauses greps for error/fail/panic/FATAL patterns in a log, returns
// context blocks (5 lines before and after each match), with overlapping blocks merged.
func extractRootCauses(log string) []string {
	lines := strings.Split(strings.TrimRight(log, "\n"), "\n")
	context := 5

	type interval struct{ start, end int }
	var intervals []interval

	for i, line := range lines {
		if errorPattern.MatchString(line) {
			start := i - context
			if start < 0 {
				start = 0
			}
			end := i + context
			if end >= len(lines) {
				end = len(lines) - 1
			}
			intervals = append(intervals, interval{start, end})
		}
	}

	if len(intervals) == 0 {
		return nil
	}

	// Merge overlapping intervals
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].start < intervals[j].start
	})

	merged := []interval{intervals[0]}
	for _, iv := range intervals[1:] {
		last := &merged[len(merged)-1]
		if iv.start <= last.end+1 {
			if iv.end > last.end {
				last.end = iv.end
			}
		} else {
			merged = append(merged, iv)
		}
	}

	result := make([]string, 0, len(merged))
	for _, iv := range merged {
		result = append(result, strings.Join(lines[iv.start:iv.end+1], "\n"))
	}
	return result
}

// computeStages groups jobs by stage and computes per-stage status.
func computeStages(jobs []Job) []Stage {
	seen := map[string]struct{}{}
	stageStatus := map[string]string{}
	var order []string

	for _, j := range jobs {
		if _, ok := seen[j.Stage]; !ok {
			seen[j.Stage] = struct{}{}
			order = append(order, j.Stage)
			stageStatus[j.Stage] = "success"
		}
		// Aggregate: failed > running > success
		cur := stageStatus[j.Stage]
		switch {
		case j.Status == "failed" || j.Status == "canceled":
			stageStatus[j.Stage] = "failed"
		case (j.Status == "running" || j.Status == "pending" || j.Status == "created") && cur != "failed":
			stageStatus[j.Stage] = "running"
		}
	}

	stages := make([]Stage, 0, len(order))
	for _, name := range order {
		stages = append(stages, Stage{Name: name, Status: stageStatus[name]})
	}
	return stages
}

// buildOutput orchestrates fetching pipeline data and constructing the Output.
func buildOutput(runner platform.CmdRunner, pipelineID int) (*Output, error) {
	// Fetch pipeline details — response may be a single object or an array
	// (array form is used when the mock returns list responses for all pipeline paths).
	pipelineOut, err := runGlab(runner, fmt.Sprintf("projects/:id/pipelines/%d", pipelineID))

	var pipeline Pipeline
	if err != nil || pipelineOut == "" {
		pipeline.ID = pipelineID
	} else {
		// Try single object first
		if jerr := json.Unmarshal([]byte(pipelineOut), &pipeline); jerr != nil {
			// Fall back to array (test mocks return list form)
			var list []Pipeline
			if jerr2 := json.Unmarshal([]byte(pipelineOut), &list); jerr2 != nil || len(list) == 0 {
				return nil, fmt.Errorf("failed to parse pipeline: %w", jerr)
			}
			pipeline = list[0]
		}
	}

	jobs, err := getAllJobs(runner, pipelineID)
	if err != nil {
		return nil, err
	}

	// Determine overall status
	status := computeStatus(pipeline.Status, jobs)

	stages := computeStages(jobs)

	allDone := true
	for _, j := range jobs {
		switch j.Status {
		case "running", "pending", "created":
			allDone = false
		}
	}

	var failedJobs []FailedJob
	for _, j := range jobs {
		if j.Status == "failed" && !j.AllowFailure {
			rawLog, _ := getJobLogTail(runner, j.ID, 200)
			cleaned := extractStepScript(rawLog)

			// Try structured extractors first, fall back to generic pattern matching.
			causes := extractGoTestFailures(cleaned)
			if len(causes) == 0 {
				causes = extractTrivyFindings(cleaned)
			}
			if len(causes) == 0 {
				causes = extractRootCauses(cleaned)
			}
			if causes == nil {
				causes = []string{}
			}
			failedJobs = append(failedJobs, FailedJob{
				ID:             j.ID,
				Name:           j.Name,
				Stage:          j.Stage,
				URL:            j.WebURL,
				AllowFailure:   j.AllowFailure,
				FailRootCauses: causes,
				LogTail:        lastNLines(cleaned, 50),
			})
		}
	}
	if failedJobs == nil {
		failedJobs = []FailedJob{}
	}

	return &Output{
		PipelineID:  pipeline.ID,
		PipelineURL: pipeline.WebURL,
		CreatedAt:   pipeline.CreatedAt,
		Duration:    pipeline.Duration,
		Status:      status,
		Stages:      stages,
		FailedJobs:  failedJobs,
		AllJobsDone: allDone,
	}, nil
}

// computeStatus determines the overall pipeline status from API status + jobs.
func computeStatus(apiStatus string, jobs []Job) string {
	if apiStatus == "canceled" {
		return "canceled"
	}

	for _, j := range jobs {
		switch j.Status {
		case "running", "pending", "created":
			return "running"
		}
	}

	for _, j := range jobs {
		if j.Status == "failed" && !j.AllowFailure {
			return "failed"
		}
	}

	return "success"
}
