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
	"github.com/eklemin/wf-agents/internal/phasedocs"
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

	// lead-protocol does not need a Temporal connection — handle it early.
	if os.Args[1] == "lead-protocol" {
		cmdLeadProtocol(os.Args[2:])
		return
	}

	// Load default config so terminal phases are available to all subcommands.
	if defaultCfg, err := config.DefaultConfig(); err == nil {
		model.SetTerminalPhases(defaultCfg.StopPhases())
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
	case "set-mr-url":
		cmdSetMrUrl(ctx, c, os.Args[2:])
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
  transition            <workflow-id> --to <PHASE> [--reason <text>] [--evidence <key>=<value> ...]
  journal               <workflow-id> --message <text>
  complete              <workflow-id>
  reset-iterations      <workflow-id>
  shut-down             <workflow-id> --agent <agent-type>
  deregister-all-agents <workflow-id>
  set-mr-url            <workflow-id> --url <url>
  list
  lead-protocol`)
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

// denialReason returns the guard explanation for a denied transition, falling back to "not allowed".
func denialReason(reason string) string {
	if reason == "" {
		return "not allowed"
	}
	return reason
}

// alreadyStartedMessage returns a human-friendly error message for the given session.
func alreadyStartedMessage(sessionID string) string {
	return fmt.Sprintf(
		"A workflow is already running for session %s. Complete or terminate it first, then retry.",
		sessionID,
	)
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
			_, _ = fmt.Sscanf(args[i+1], "%d", &input.MaxIterations)
		}
	}

	if input.SessionID == "" {
		log.Fatal("--session is required")
	}
	if input.TaskDescription == "" {
		log.Fatal("--task is required")
	}
	if len(input.TaskDescription) > 60 {
		log.Fatalf("--task must be at most 60 characters (got %d); use a short English summary", len(input.TaskDescription))
	}
	if input.RepoPath == "" {
		log.Fatalf(
			"--repo is required for 'start'.\n\n" +
				"The marker file stores --repo as the session CWD; teammates use it for workflow resolution.\n\n" +
				"How to detect the correct path:\n" +
				"  - Check your CLAUDE.md path. If it contains .claude/worktrees/<name>/, use that worktree root.\n" +
				"  - Otherwise use the repository root.\n\n" +
				"Correct command:\n" +
				"  wf-client start --session <id> --repo $(pwd) --task \"<description>\"",
		)
	}

	// Snapshot the flow topology (phases + transitions) at session start.
	// Uses LoadConfig to merge embedded defaults with project-level .wf-agents.yaml.
	projectDir := input.RepoPath
	cfg, err := config.LoadConfig(projectDir)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	input.Flow = config.ExtractFlowSnapshot(cfg)

	workflowID := "coding-session-" + input.SessionID
	opts := buildStartOptions(workflowID, taskQueue)

	projectName := ""
	repoURL := ""
	if remoteOut, remoteErr := platform.RunCmdInDir(5*time.Second, input.RepoPath, "git", "remote", "get-url", "origin"); remoteErr == nil {
		remote := strings.TrimSpace(remoteOut)
		projectName = platform.ProjectNameFromURL(remote)
		repoURL = platform.GitRemoteToWebURL(remote)
	} else {
		projectName = filepath.Base(input.RepoPath)
	}

	opts.Memo = map[string]interface{}{
		"task":         input.TaskDescription,
		"project_name": projectName,
		"repo_url":     repoURL,
	}

	run, err := c.ExecuteWorkflow(ctx, opts, wf.CodingSessionWorkflow, input)
	if err != nil {
		if isAlreadyStartedError(err.Error()) {
			log.Fatal(alreadyStartedMessage(input.SessionID))
		}
		log.Fatalf("Failed to start workflow: %v", err)
	}

	// Create marker file so hook-handler knows this session is active
	createSessionMarker(input.SessionID, input.RepoPath)

	fmt.Printf(
		"Workflow started:\n  ID:    %s\n  RunID: %s\n",
		run.GetID(),
		run.GetRunID(),
	)

	startingPhase := model.Phase(cfg.Phases.Start)
	instructions, err := phasedocs.FullInstructions(startingPhase, input.RepoPath, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load phase instructions: %v\n", err)
	} else if instructions != "" {
		fmt.Println(instructions)
	}
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
	_, _ = fmt.Fprintf(w, "Phase:\t%s\n", status.Phase)
	_, _ = fmt.Fprintf(w, "Iteration:\t%d\n", status.Iteration)
	_, _ = fmt.Fprintf(w, "Events:\t%d\n", status.EventCount)
	_, _ = fmt.Fprintf(w, "Active Agents:\t%v\n", status.ActiveAgents)
	_, _ = fmt.Fprintf(w, "Started:\t%s\n", status.StartedAt)
	_, _ = fmt.Fprintf(w, "Updated:\t%s\n", status.LastUpdatedAt)
	_, _ = fmt.Fprintf(w, "Task:\t%s\n", status.Task)
	if status.MRUrl != "" {
		_, _ = fmt.Fprintf(w, "MR URL:\t%s\n", status.MRUrl)
	}
	if len(status.CommandsRan) > 0 {
		_, _ = fmt.Fprintf(w, "Commands Ran:\t\n")
		for agent, cats := range status.CommandsRan {
			for cat, ran := range cats {
				if ran {
					_, _ = fmt.Fprintf(w, "  %s/%s:\ttrue\n", agent, cat)
				}
			}
		}
	}
	_ = w.Flush()
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
		_ = enc.Encode(evt)
	}
}

func cmdTransition(ctx context.Context, c client.Client, args []string) {
	if len(args) < 1 {
		log.Fatal("workflow-id required")
	}
	workflowID := resolveWorkflowID(args[0])

	req := model.SignalTransition{SessionID: "cli"}
	var repoPath string
	for i := 1; i < len(args)-1; i += 2 {
		switch args[i] {
		case "--to":
			req.To = model.Phase(args[i+1])
		case "--reason":
			req.Reason = args[i+1]
		case "--session":
			req.SessionID = args[i+1]
		case "--repo":
			repoPath = args[i+1]
		case "--evidence":
			// consumed below via parseEvidenceFlags
		}
	}
	if req.To == "" {
		log.Fatal("--to is required")
	}
	req.To = model.Phase(strings.ToUpper(string(req.To)))

	// Collect evidence for transition guards
	req.Guards = collectEvidence(repoPath)

	// Record which keys were system-collected so CLI cannot override them.
	systemKeys := make(map[string]bool)
	for k := range req.Guards {
		systemKeys[k] = true
	}

	// Merge CLI-provided evidence, skipping system-collected keys.
	cliEvidence, err := parseEvidenceFlags(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	for k, v := range cliEvidence {
		if systemKeys[k] {
			fmt.Fprintf(os.Stderr, "Warning: ignoring --evidence %s (system-collected key)\n", k)
			continue
		}
		req.Guards[k] = v
	}

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
		if result.NoOp {
			fmt.Printf("TRANSITION NO-OP: already in %s\n", result.To)
		} else {
			fmt.Printf("TRANSITION ALLOWED: %s → %s\n", result.From, result.To)
		}
		cwd, err := os.Getwd()
		if err != nil {
			cwd = ""
		}
		instructions, err := phasedocs.FullInstructions(result.To, cwd, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "CRITICAL: %v\n", err)
			fmt.Fprintf(
				os.Stderr,
				"You MUST stop and ask the user to fix the plugin configuration. Do NOT proceed without phase instructions.\n",
			)
			os.Exit(1)
		}
		if instructions != "" {
			fmt.Println(instructions)
		}
		if result.To.IsTerminal() {
			sessionID := strings.TrimPrefix(workflowID, "coding-session-")
			removeSessionMarker(sessionID)
		}
	} else {
		fmt.Printf("TRANSITION DENIED: %s → %s (%s)\n", result.From, result.To, denialReason(result.Reason))
		if len(result.AllowedTransitions) > 0 {
			fmt.Printf(
				"Choose one of the allowed transitions: %s\n",
				strings.Join(result.AllowedTransitions, ", "),
			)
		}
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

	var repoPath string
	for i := 1; i < len(args)-1; i += 2 {
		if args[i] == "--repo" {
			repoPath = args[i+1]
		}
	}

	// Use the same UpdateWorkflow path as transition — goes through state machine validation.
	// Target the first configured stop phase (from defaults.yaml phases.stop).
	defaultCfg, err := config.DefaultConfig()
	if err != nil {
		log.Fatalf("cannot load workflow config: %v", err)
	}
	stops := defaultCfg.StopPhases()
	if len(stops) == 0 {
		log.Fatal("no stop phases configured")
	}
	stopPhase := model.Phase(stops[0])
	req := model.SignalTransition{
		To:        stopPhase,
		SessionID: "cli",
		Reason:    "manual complete",
		Guards:    collectEvidence(repoPath),
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
		if result.NoOp {
			fmt.Printf("COMPLETE NO-OP: already in %s\n", result.To)
		} else {
			fmt.Printf("COMPLETE: %s → %s\n", result.From, result.To)
			sessionID := strings.TrimPrefix(workflowID, "coding-session-")
			removeSessionMarker(sessionID)
		}
	} else {
		fmt.Printf("COMPLETE DENIED: %s → %s\nReason: %s\n", result.From, result.To, result.Reason)
		if len(result.AllowedTransitions) > 0 {
			fmt.Printf(
				"Allowed transitions from %s: %s\n",
				result.From,
				strings.Join(result.AllowedTransitions, ", "),
			)
		}
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

func cmdSetMrUrl(ctx context.Context, c client.Client, args []string) {
	if len(args) < 1 {
		log.Fatal("workflow-id required")
	}
	workflowID := resolveWorkflowID(args[0])

	var url string
	for i := 1; i < len(args)-1; i += 2 {
		switch args[i] {
		case "--url":
			url = args[i+1]
		}
	}
	if url == "" {
		log.Fatal("--url is required")
	}

	err := c.SignalWorkflow(ctx, workflowID, "", wf.SignalSetMrUrl, url)
	if err != nil {
		log.Fatalf("Signal failed: %v", err)
	}
	fmt.Printf("MR URL set to %s in workflow %s\n", url, workflowID)
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
	_, _ = fmt.Fprintf(w, "WORKFLOW ID\tSTATUS\tTASK\tSTART TIME\n")
	for _, wfe := range resp.Executions {
		startTime := ""
		if wfe.StartTime != nil {
			startTime = wfe.StartTime.AsTime().Format("2006-01-02 15:04:05")
		}
		task := ""
		if wfe.Memo != nil {
			if payload, ok := wfe.Memo.Fields["task"]; ok {
				var t string
				if json.Unmarshal(payload.Data, &t) == nil {
					task = t
				}
			}
		}
		if len([]rune(task)) > 40 {
			task = string([]rune(task)[:40]) + "…"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			wfe.Execution.WorkflowId,
			wfe.Status.String(),
			task,
			startTime,
		)
	}
	_ = w.Flush()
}

// cmdLeadProtocol resolves the team-lead.md protocol file using three-level resolution:
// project override (.wf-agents/team-lead.md) → preset → plugin default (workflow/team-lead.md).
// Prints the absolute path on success (exit 0) or exits 1 if not found.
func cmdLeadProtocol(args []string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot determine working directory: %v\n", err)
		os.Exit(1)
	}

	root, err := config.PluginRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot determine plugin root: %v\n", err)
		os.Exit(1)
	}

	workflowDir := filepath.Join(root, "workflow")
	path, err := config.ResolveFile("team-lead.md", cwd, workflowDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "team-lead.md not found: %v\n", err)
		os.Exit(1)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read team-lead.md: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Team Lead protocol file (re-read at: %s):\n\n%s\n", path, content)
}

func resolveWorkflowID(id string) string {
	return session.ResolveWorkflowID(id)
}

// collectGitHubEvidenceWithRunner populates evidence using GitHub's gh CLI via the provided runner.
func collectGitHubEvidenceWithRunner(evidence map[string]string, runner platform.CmdRunner) {
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
			evidence["ci_passed"] = "false"
			evidence["pr_checks_detail"] = "could not parse checks"
		}
	} else {
		// gh pr checks failed entirely (no PR, no git remote, etc.) → don't block
		evidence["ci_passed"] = "false"
		evidence["pr_checks_detail"] = "no PR found or gh unavailable"
	}

	// PR review approval and draft status — for FEEDBACK → COMPLETE.
	// Allows completion if PR is approved OR MR moved from draft to ready.
	if out, err := runner(10*time.Second, "gh", "pr", "view", "--json", "reviewDecision,state,isDraft"); err == nil {
		var pr struct {
			ReviewDecision string `json:"reviewDecision"`
			State          string `json:"state"`
			IsDraft        bool   `json:"isDraft"`
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
			if !pr.IsDraft {
				evidence["mr_ready"] = "true"
			} else {
				evidence["mr_ready"] = "false"
			}
		} else {
			evidence["review_approved"] = "false"
			evidence["pr_approved_detail"] = "could not parse PR"
			evidence["merged"] = "false"
			evidence["mr_ready"] = "false"
		}
	} else {
		evidence["review_approved"] = "false"
		evidence["pr_approved_detail"] = "no PR found"
		evidence["merged"] = "false"
		evidence["mr_ready"] = "false"
	}
}

// collectGitHubEvidence populates evidence map using GitHub's gh CLI, running in repoPath.
func collectGitHubEvidence(evidence map[string]string, repoPath string) {
	collectGitHubEvidenceWithRunner(evidence, cmdRunnerForDir(repoPath))
}

// collectGitLabEvidenceWithRunner populates evidence using GitLab's glab CLI via the provided runner.
func collectGitLabEvidenceWithRunner(evidence map[string]string, runner platform.CmdRunner) {
	out, err := runner(15*time.Second, "glab", "mr", "view", "-F", "json")
	if err != nil {
		// No MR or glab unavailable — use permissive defaults
		evidence["ci_passed"] = "false"
		evidence["review_approved"] = "false"
		evidence["merged"] = "false"
		evidence["mr_ready"] = "false"
		return
	}

	var mr struct {
		HeadPipeline *struct {
			Status string `json:"status"`
		} `json:"head_pipeline"`
		ApprovedBy []interface{} `json:"approved_by"`
		State      string        `json:"state"`
		Draft      bool          `json:"draft"`
	}

	if json.Unmarshal([]byte(out), &mr) != nil {
		// Malformed JSON — use permissive defaults
		evidence["ci_passed"] = "false"
		evidence["review_approved"] = "false"
		evidence["merged"] = "false"
		evidence["mr_ready"] = "false"
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

	// Merged status
	if mr.State == "merged" {
		evidence["merged"] = "true"
	} else {
		evidence["merged"] = "false"
	}

	// MR ready status (not a draft and not merged)
	if !mr.Draft && mr.State != "merged" {
		evidence["mr_ready"] = "true"
	} else {
		evidence["mr_ready"] = "false"
	}
}

// collectGitLabEvidence populates evidence map using GitLab's glab CLI, running in repoPath.
func collectGitLabEvidence(evidence map[string]string, repoPath string) {
	collectGitLabEvidenceWithRunner(evidence, cmdRunnerForDir(repoPath))
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

// parseEvidenceFlags extracts --evidence key=value pairs from args.
// Returns an error if any value does not contain '='.
func parseEvidenceFlags(args []string) (map[string]string, error) {
	evidence := make(map[string]string)
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--evidence" {
			val := args[i+1]
			idx := strings.Index(val, "=")
			if idx < 0 {
				return nil, fmt.Errorf("invalid --evidence value %q: must be key=value", val)
			}
			evidence[val[:idx]] = val[idx+1:]
			i++ // skip the value token
		}
	}
	return evidence, nil
}

// gitRunnerForRepo returns a CmdRunner that prepends "git -C repoPath" to all git
// invocations when repoPath is non-empty. This lets evidence collection run git in
// the correct worktree directory rather than the process CWD.
func gitRunnerForRepo(repoPath string) platform.CmdRunner {
	if repoPath == "" {
		return platform.RunCmd
	}
	return func(timeout time.Duration, name string, args ...string) (string, error) {
		if name == "git" {
			args = append([]string{"-C", repoPath}, args...)
		}
		return platform.RunCmd(timeout, name, args...)
	}
}

// cmdRunnerForDir returns a CmdRunner that runs commands with Dir set to repoPath
// when repoPath is non-empty. Used for CLI tools like glab/gh that don't support -C.
func cmdRunnerForDir(repoPath string) platform.CmdRunner {
	if repoPath == "" {
		return platform.RunCmd
	}
	return func(timeout time.Duration, name string, args ...string) (string, error) {
		return platform.RunCmdInDir(timeout, repoPath, name, args...)
	}
}

// collectEvidence gathers local git/PR state to send with the transition request.
// The Temporal workflow uses this evidence for guard validation.
// Evidence is best-effort: failures result in missing keys, not errors.
// repoPath, if non-empty, overrides the directory used for git commands (e.g. a worktree path).
func collectEvidence(repoPath string) map[string]string {
	evidence := make(map[string]string)
	gitRunner := gitRunnerForRepo(repoPath)

	// git working tree status — platform-agnostic
	if out, err := gitRunner(10*time.Second, "git", "status", "--porcelain"); err == nil {
		if strings.TrimSpace(out) == "" {
			evidence["working_tree_clean"] = "true"
		} else {
			evidence["working_tree_clean"] = "false"
		}
	}

	collectBranchPushedEvidence(evidence, gitRunner)

	switch platform.DetectPlatform() {
	case "github":
		collectGitHubEvidence(evidence, repoPath)
	case "gitlab":
		collectGitLabEvidence(evidence, repoPath)
	default:
		// Unknown platform — use conservative defaults
		evidence["ci_passed"] = "false"
		evidence["review_approved"] = "false"
		evidence["merged"] = "false"
		evidence["mr_ready"] = "false"
	}

	return evidence
}

// createSessionMarker writes a JSON marker file so the hook-handler knows this session
// has an active workflow and can resolve teammates by CWD. Without the marker, hooks are no-ops.
func createSessionMarker(sessionID, cwd string) {
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	_ = os.MkdirAll(dir, 0o755)
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
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}
