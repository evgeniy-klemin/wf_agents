package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eklemin/wf-agents/internal/config"
	"github.com/eklemin/wf-agents/internal/model"
	"github.com/eklemin/wf-agents/internal/noplog"
	"github.com/eklemin/wf-agents/internal/platform"
	"github.com/eklemin/wf-agents/internal/session"
	internaltemporal "github.com/eklemin/wf-agents/internal/temporal"
	wf "github.com/eklemin/wf-agents/internal/workflow"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

const taskQueue = "coding-session"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	c, err := client.Dial(client.Options{
		HostPort: internaltemporal.Host(),
		Logger:   noplog.New(),
	})
	if err != nil {
		log.Fatalf("Failed to connect to Temporal: %v", err)
	}
	defer c.Close()

	ctx := context.Background()

	switch os.Args[1] {
	case "start":
		cmdStart(ctx, c, os.Args[2:])
	case "status":
		cmdStatus(ctx, c, os.Args[2:])
	case "timeline":
		cmdTimeline(ctx, c, os.Args[2:])
	case "transition":
		cmdTransition(ctx, c, os.Args[2:])
	case "journal":
		cmdJournal(ctx, c, os.Args[2:])
	case "complete":
		cmdComplete(ctx, c, os.Args[2:])
	case "reset-iterations":
		cmdResetIterations(ctx, c, os.Args[2:])
	case "deregister-agent", "shut-down":
		cmdShutDown(ctx, c, os.Args[2:])
	case "deregister-all-agents":
		cmdDeregisterAllAgents(ctx, c, os.Args[2:])
	case "list":
		cmdList(ctx, c)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Usage: wf-client <command> [args]

Commands:
  start                 --session <id> --task <desc> [--repo <path>] [--max-iter <n>]
  status                <workflow-id>
  timeline              <workflow-id>
  transition            <workflow-id> --to <PHASE> [--reason <text>]
  journal               <workflow-id> --message <text>
  complete              <workflow-id>
  reset-iterations      <workflow-id>
  shut-down             <workflow-id> --agent <agent-type>
  deregister-all-agents <workflow-id>
  list`)
}

// buildStartOptions constructs StartWorkflowOptions with an explicit reuse policy
// that allows the same workflow ID to be reused after the previous run completes.
func buildStartOptions(workflowID, queue string) client.StartWorkflowOptions {
	return client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: queue,
		// ALLOW_DUPLICATE permits reusing a workflow ID only after the previous
		// execution has closed (Completed/Failed/Terminated). A running workflow
		// with the same ID will cause ExecuteWorkflow to return an error.
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}
}

// isAlreadyStartedError returns true when the error indicates the workflow ID is
// already in use by a running execution.
func isAlreadyStartedError(errMsg string) bool {
	return strings.Contains(errMsg, "already started") || strings.Contains(errMsg, "AlreadyStarted")
}

// alreadyStartedMessage returns a human-friendly error message for the given session.
func alreadyStartedMessage(sessionID string) string {
	return fmt.Sprintf("A workflow is already running for session %s. Complete or terminate it first, then retry.", sessionID)
}

func cmdStart(ctx context.Context, c client.Client, args []string) {
	input := model.WorkflowInput{MaxIterations: 5}
	for i := 0; i < len(args)-1; i += 2 {
		switch args[i] {
		case "--session":
			input.SessionID = args[i+1]
		case "--task":
			input.TaskDescription = args[i+1]
		case "--repo":
			input.RepoPath = args[i+1]
		case "--max-iter":
			fmt.Sscanf(args[i+1], "%d", &input.MaxIterations)
		}
	}

	if input.SessionID == "" {
		log.Fatal("--session is required")
	}
	if input.TaskDescription == "" {
		log.Fatal("--task is required")
	}

	// Snapshot the flow topology (phases + transitions) at session start.
	// Uses LoadConfig to merge embedded defaults with project-level .wf-agents.yaml.
	projectDir := input.RepoPath
	if projectDir == "" {
		projectDir, _ = os.Getwd()
	}
	cfg, err := config.LoadConfig(projectDir)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	input.Flow = config.ExtractFlowSnapshot(cfg)

	workflowID := "coding-session-" + input.SessionID
	opts := buildStartOptions(workflowID, taskQueue)
	opts.Memo = map[string]interface{}{
		"task": input.TaskDescription,
	}

	run, err := c.ExecuteWorkflow(ctx, opts, wf.CodingSessionWorkflow, input)
	if err != nil {
		if isAlreadyStartedError(err.Error()) {
			log.Fatal(alreadyStartedMessage(input.SessionID))
		}
		log.Fatalf("Failed to start workflow: %v", err)
	}

	// Determine CWD: use --repo flag if provided, otherwise current directory.
	cwd := input.RepoPath
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}

	// Create marker file so hook-handler knows this session is active
	createSessionMarker(input.SessionID, cwd)

	fmt.Printf("Workflow started:\n  ID:    %s\n  RunID: %s\n  UI:    http://localhost:8080/namespaces/default/workflows/%s\n",
		run.GetID(), run.GetRunID(), workflowID)
}

func cmdStatus(ctx context.Context, c client.Client, args []string) {
	if len(args) < 1 {
		log.Fatal("workflow-id required")
	}
	workflowID := resolveWorkflowID(args[0])

	resp, err := c.QueryWorkflow(ctx, workflowID, "", wf.QueryStatus)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}

	var status model.WorkflowStatus
	if err := resp.Get(&status); err != nil {
		log.Fatalf("Failed to decode: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "Phase:\t%s\n", status.Phase)
	fmt.Fprintf(w, "Iteration:\t%d\n", status.Iteration)
	fmt.Fprintf(w, "Events:\t%d\n", status.EventCount)
	fmt.Fprintf(w, "Active Agents:\t%v\n", status.ActiveAgents)
	fmt.Fprintf(w, "Started:\t%s\n", status.StartedAt)
	fmt.Fprintf(w, "Updated:\t%s\n", status.LastUpdatedAt)
	fmt.Fprintf(w, "Task:\t%s\n", status.Task)
	if len(status.CommandsRan) > 0 {
		fmt.Fprintf(w, "Commands Ran:\t\n")
		for agent, cats := range status.CommandsRan {
			for cat, ran := range cats {
				if ran {
					fmt.Fprintf(w, "  %s/%s:\ttrue\n", agent, cat)
				}
			}
		}
	}
	w.Flush()
}

func cmdTimeline(ctx context.Context, c client.Client, args []string) {
	if len(args) < 1 {
		log.Fatal("workflow-id required")
	}
	workflowID := resolveWorkflowID(args[0])

	resp, err := c.QueryWorkflow(ctx, workflowID, "", wf.QueryTimeline)
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}

	var timeline model.WorkflowTimeline
	if err := resp.Get(&timeline); err != nil {
		log.Fatalf("Failed to decode: %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	for _, evt := range timeline.Events {
		enc.Encode(evt)
	}
}

func cmdTransition(ctx context.Context, c client.Client, args []string) {
	if len(args) < 1 {
		log.Fatal("workflow-id required")
	}
	workflowID := resolveWorkflowID(args[0])

	req := model.SignalTransition{SessionID: "cli"}
	for i := 1; i < len(args)-1; i += 2 {
		switch args[i] {
		case "--to":
			req.To = model.Phase(args[i+1])
		case "--reason":
			req.Reason = args[i+1]
		case "--session":
			req.SessionID = args[i+1]
		}
	}
	if req.To == "" {
		log.Fatal("--to is required")
	}
	req.To = model.Phase(strings.ToUpper(string(req.To)))

	// Collect evidence for transition guards
	req.Guards = collectEvidence()

	// Use UpdateWorkflow for synchronous allow/deny response
	handle, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   workflowID,
		UpdateName:   wf.UpdateTransition,
		Args:         []interface{}{req},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		log.Fatalf("Transition request failed: %v", err)
	}

	var result model.TransitionResult
	if err := handle.Get(ctx, &result); err != nil {
		log.Fatalf("Failed to get transition result: %v", err)
	}

	if result.Allowed {
		fmt.Printf("TRANSITION ALLOWED: %s → %s\n", result.From, result.To)
		if result.To == model.PhaseComplete {
			sessionID := strings.TrimPrefix(workflowID, "coding-session-")
			removeSessionMarker(sessionID)
		}
	} else {
		fmt.Fprintf(os.Stderr, "TRANSITION DENIED: %s → %s\nReason: %s\n", result.From, result.To, result.Reason)
		os.Exit(1)
	}
}

func cmdJournal(ctx context.Context, c client.Client, args []string) {
	if len(args) < 1 {
		log.Fatal("workflow-id required")
	}
	workflowID := resolveWorkflowID(args[0])

	sig := model.SignalJournal{SessionID: "cli"}
	for i := 1; i < len(args)-1; i += 2 {
		switch args[i] {
		case "--message":
			sig.Message = args[i+1]
		}
	}
	if sig.Message == "" {
		log.Fatal("--message is required")
	}

	err := c.SignalWorkflow(ctx, workflowID, "", wf.SignalJournal, sig)
	if err != nil {
		log.Fatalf("Signal failed: %v", err)
	}
	fmt.Println("Journal entry sent")
}

func cmdComplete(ctx context.Context, c client.Client, args []string) {
	if len(args) < 1 {
		log.Fatal("workflow-id required")
	}
	workflowID := resolveWorkflowID(args[0])

	// Use the same UpdateWorkflow path as transition — goes through state machine validation
	req := model.SignalTransition{
		To:        model.PhaseComplete,
		SessionID: "cli",
		Reason:    "manual complete",
		Guards:    collectEvidence(),
	}
	handle, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   workflowID,
		UpdateName:   wf.UpdateTransition,
		Args:         []interface{}{req},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		log.Fatalf("Complete request failed: %v", err)
	}

	var result model.TransitionResult
	if err := handle.Get(ctx, &result); err != nil {
		log.Fatalf("Failed to get result: %v", err)
	}

	if result.Allowed {
		fmt.Printf("COMPLETE: %s → %s\n", result.From, result.To)
		sessionID := strings.TrimPrefix(workflowID, "coding-session-")
		removeSessionMarker(sessionID)
	} else {
		fmt.Fprintf(os.Stderr, "COMPLETE DENIED: %s → %s\nReason: %s\n", result.From, result.To, result.Reason)
		os.Exit(1)
	}
}

func cmdResetIterations(ctx context.Context, c client.Client, args []string) {
	if len(args) < 1 {
		log.Fatal("workflow-id required")
	}
	workflowID := resolveWorkflowID(args[0])

	sessionID := strings.TrimPrefix(workflowID, "coding-session-")
	err := c.SignalWorkflow(ctx, workflowID, "", wf.SignalResetIterations, sessionID)
	if err != nil {
		log.Fatalf("Signal failed: %v", err)
	}
	fmt.Printf("Iteration counter reset signal sent to %s\n", workflowID)
	fmt.Println("The resettable iteration counter will be set to 1 on the next workflow task.")
	fmt.Println("Total iterations counter is unchanged. You may now retry the RESPAWN transition.")
}

// cmdShutDown removes a single named agent from activeAgents by sending the
// agent-shut-down signal. This is the explicit, intent-clear way to deregister
// a teammate (developer-N, reviewer-N) after they finish work.
//
// Also accepts the legacy "deregister-agent" command alias for backwards compatibility.
func cmdShutDown(ctx context.Context, c client.Client, args []string) {
	if len(args) < 1 {
		log.Fatal("workflow-id required")
	}
	workflowID := resolveWorkflowID(args[0])

	var agentName string
	for i := 1; i < len(args)-1; i += 2 {
		switch args[i] {
		case "--agent":
			agentName = args[i+1]
		}
	}
	if agentName == "" {
		log.Fatal("--agent <agent-type> is required")
	}

	err := c.SignalWorkflow(ctx, workflowID, "", wf.SignalAgentShutDown, struct{ AgentName string }{agentName})
	if err != nil {
		log.Fatalf("Signal failed: %v", err)
	}
	fmt.Printf("Shut-down signal sent for agent %q in workflow %s\n", agentName, workflowID)
}

// cmdDeregisterAllAgents clears ALL activeAgents by sending a clear-active-agents signal.
func cmdDeregisterAllAgents(ctx context.Context, c client.Client, args []string) {
	if len(args) < 1 {
		log.Fatal("workflow-id required")
	}
	workflowID := resolveWorkflowID(args[0])

	err := c.SignalWorkflow(ctx, workflowID, "", wf.SignalClearActiveAgents, "cli")
	if err != nil {
		log.Fatalf("Signal failed: %v", err)
	}
	fmt.Printf("All active agents cleared in workflow %s\n", workflowID)
}

func cmdList(ctx context.Context, c client.Client) {
	resp, err := c.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
		Namespace: "default",
		Query:     `WorkflowType = "CodingSessionWorkflow"`,
	})
	if err != nil {
		log.Fatalf("List failed: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "WORKFLOW ID\tSTATUS\tSTART TIME\n")
	for _, wfe := range resp.Executions {
		startTime := ""
		if wfe.StartTime != nil {
			startTime = wfe.StartTime.AsTime().Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			wfe.Execution.WorkflowId,
			wfe.Status.String(),
			startTime,
		)
	}
	w.Flush()
}

func resolveWorkflowID(id string) string {
	return session.ResolveWorkflowID(id)
}

// collectGitHubEvidence populates evidence map using GitHub's gh CLI.
func collectGitHubEvidence(evidence map[string]string, runner platform.CmdRunner) {
	// PR checks status via JSON — reliable parsing of check states.
	// Empty array or no PR = no CI configured → pass (don't block).
	if out, err := runner(15*time.Second, "gh", "pr", "checks", "--json", "name,state"); err == nil {
		var checks []struct {
			State string `json:"state"`
		}
		if json.Unmarshal([]byte(out), &checks) == nil {
			if len(checks) == 0 {
				evidence["ci_passed"] = "true"
				evidence["pr_checks_detail"] = "no CI checks configured"
			} else {
				allPass := true
				for _, ch := range checks {
					if ch.State != "SUCCESS" && ch.State != "NEUTRAL" && ch.State != "SKIPPED" {
						allPass = false
						break
					}
				}
				if allPass {
					evidence["ci_passed"] = "true"
				} else {
					evidence["ci_passed"] = "false"
					evidence["pr_checks_detail"] = fmt.Sprintf("%d checks, some not passed", len(checks))
				}
			}
		} else {
			evidence["ci_passed"] = "true"
			evidence["pr_checks_detail"] = "could not parse checks"
		}
	} else {
		// gh pr checks failed entirely (no PR, no git remote, etc.) → don't block
		evidence["ci_passed"] = "true"
		evidence["pr_checks_detail"] = "no PR found or gh unavailable"
	}

	// PR review approval and merge status — for FEEDBACK → COMPLETE.
	// Allows completion if PR is approved OR already merged.
	if out, err := runner(10*time.Second, "gh", "pr", "view", "--json", "reviewDecision,state"); err == nil {
		var pr struct {
			ReviewDecision string `json:"reviewDecision"`
			State          string `json:"state"`
		}
		if json.Unmarshal([]byte(out), &pr) == nil {
			if pr.ReviewDecision == "APPROVED" {
				evidence["review_approved"] = "true"
			} else {
				evidence["review_approved"] = "false"
				if pr.ReviewDecision != "" {
					evidence["pr_approved_detail"] = pr.ReviewDecision
				} else {
					evidence["pr_approved_detail"] = "no reviews yet"
				}
			}
			if pr.State == "MERGED" {
				evidence["merged"] = "true"
			} else {
				evidence["merged"] = "false"
			}
		}
	} else {
		evidence["review_approved"] = "false"
		evidence["pr_approved_detail"] = "no PR found"
		evidence["merged"] = "false"
	}
}

// collectGitLabEvidence populates evidence map using GitLab's glab CLI.
func collectGitLabEvidence(evidence map[string]string, runner platform.CmdRunner) {
	out, err := runner(15*time.Second, "glab", "mr", "view", "-F", "json")
	if err != nil {
		// No MR or glab unavailable — use permissive defaults
		evidence["ci_passed"] = "true"
		evidence["review_approved"] = "false"
		evidence["merged"] = "false"
		return
	}

	var mr struct {
		HeadPipeline *struct {
			Status string `json:"status"`
		} `json:"head_pipeline"`
		ApprovedBy []interface{} `json:"approved_by"`
		State      string        `json:"state"`
	}

	if json.Unmarshal([]byte(out), &mr) != nil {
		// Malformed JSON — use permissive defaults
		evidence["ci_passed"] = "true"
		evidence["review_approved"] = "false"
		evidence["merged"] = "false"
		return
	}

	// CI pipeline status
	if mr.HeadPipeline == nil {
		evidence["ci_passed"] = "true"
	} else {
		switch mr.HeadPipeline.Status {
		case "success", "skipped", "":
			evidence["ci_passed"] = "true"
		default:
			evidence["ci_passed"] = "false"
		}
	}

	// Review approval
	if len(mr.ApprovedBy) > 0 {
		evidence["review_approved"] = "true"
	} else {
		evidence["review_approved"] = "false"
	}

	// Merge status
	if mr.State == "merged" {
		evidence["merged"] = "true"
	} else {
		evidence["merged"] = "false"
	}
}

// collectBranchPushedEvidence checks whether the local HEAD matches the upstream
// tracking branch HEAD, setting evidence["branch_pushed"] to "true" or "false".
func collectBranchPushedEvidence(evidence map[string]string, runner platform.CmdRunner) {
	localHead, err := runner(5*time.Second, "git", "rev-parse", "HEAD")
	if err != nil {
		return
	}
	remoteHead, err := runner(5*time.Second, "git", "rev-parse", "@{u}")
	if err != nil {
		// No upstream tracking branch — not pushed
		evidence["branch_pushed"] = "false"
		return
	}
	if strings.TrimSpace(localHead) == strings.TrimSpace(remoteHead) {
		evidence["branch_pushed"] = "true"
	} else {
		evidence["branch_pushed"] = "false"
	}
}

// collectEvidence gathers local git/PR state to send with the transition request.
// The Temporal workflow uses this evidence for guard validation.
// Evidence is best-effort: failures result in missing keys, not errors.
func collectEvidence() map[string]string {
	evidence := make(map[string]string)

	// git working tree status — platform-agnostic
	if out, err := platform.RunCmd(10*time.Second, "git", "status", "--porcelain"); err == nil {
		if strings.TrimSpace(out) == "" {
			evidence["working_tree_clean"] = "true"
		} else {
			evidence["working_tree_clean"] = "false"
		}
	}

	collectBranchPushedEvidence(evidence, platform.RunCmd)

	switch platform.DetectPlatform() {
	case "github":
		collectGitHubEvidence(evidence, platform.RunCmd)
	case "gitlab":
		collectGitLabEvidence(evidence, platform.RunCmd)
	default:
		// Unknown platform — use permissive defaults so we don't block
		evidence["ci_passed"] = "true"
		evidence["review_approved"] = "false"
		evidence["merged"] = "false"
	}

	return evidence
}

// createSessionMarker writes a JSON marker file so the hook-handler knows this session
// has an active workflow and can resolve teammates by CWD. Without the marker, hooks are no-ops.
func createSessionMarker(sessionID, cwd string) {
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	os.MkdirAll(dir, 0o755)
	marker := filepath.Join(dir, sessionID)
	data, _ := json.Marshal(map[string]string{
		"session_id":  sessionID,
		"workflow_id": "coding-session-" + sessionID,
		"cwd":         cwd,
	})
	if err := os.WriteFile(marker, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create session marker: %v\n", err)
	}
}

// removeSessionMarker deletes the marker file for the given session so that
// hook-handler becomes a no-op after the workflow reaches COMPLETE.
// It also removes any teammate markers whose "parent" field matches sessionID.
func removeSessionMarker(sessionID string) {
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	marker := filepath.Join(dir, sessionID)
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: could not remove session marker: %v\n", err)
	}

	// Clean up teammate markers that belong to this parent session.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Name() == sessionID {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var m map[string]string
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		if m["parent"] == sessionID {
			os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
