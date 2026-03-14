package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eklemin/wf-agents/internal/model"
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
		HostPort: "localhost:7233",
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
  start             --session <id> --task <desc> [--repo <path>] [--max-iter <n>]
  status            <workflow-id>
  timeline          <workflow-id>
  transition        <workflow-id> --to <PHASE> [--reason <text>]
  journal           <workflow-id> --message <text>
  complete          <workflow-id>
  reset-iterations  <workflow-id>
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

	// Create marker file so hook-handler knows this session is active
	createSessionMarker(input.SessionID)

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
	// Allow short IDs without the prefix
	if len(id) > 0 && id[0] != 'c' {
		return "coding-session-" + id
	}
	return id
}

// collectEvidence gathers local git/PR state to send with the transition request.
// The Temporal workflow uses this evidence for guard validation.
// Evidence is best-effort: failures result in missing keys, not errors.
func collectEvidence() map[string]string {
	evidence := make(map[string]string)

	// git working tree status
	if out, err := runCmd(10*time.Second, "git", "status", "--porcelain"); err == nil {
		if strings.TrimSpace(out) == "" {
			evidence["working_tree_clean"] = "true"
		} else {
			evidence["working_tree_clean"] = "false"
		}
	}

	// PR checks status via JSON — reliable parsing of check states.
	// Empty array or no PR = no CI configured → pass (don't block).
	if out, err := runCmd(15*time.Second, "gh", "pr", "checks", "--json", "name,state"); err == nil {
		var checks []struct{ State string `json:"state"` }
		if json.Unmarshal([]byte(out), &checks) == nil {
			if len(checks) == 0 {
				evidence["pr_checks_pass"] = "true"
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
					evidence["pr_checks_pass"] = "true"
				} else {
					evidence["pr_checks_pass"] = "false"
					evidence["pr_checks_detail"] = fmt.Sprintf("%d checks, some not passed", len(checks))
				}
			}
		} else {
			evidence["pr_checks_pass"] = "true"
			evidence["pr_checks_detail"] = "could not parse checks"
		}
	} else {
		// gh pr checks failed entirely (no PR, no git remote, etc.) → don't block
		evidence["pr_checks_pass"] = "true"
		evidence["pr_checks_detail"] = "no PR found or gh unavailable"
	}

	// PR review approval and merge status — for FEEDBACK → COMPLETE.
	// Allows completion if PR is approved OR already merged.
	if out, err := runCmd(10*time.Second, "gh", "pr", "view", "--json", "reviewDecision,state"); err == nil {
		var pr struct {
			ReviewDecision string `json:"reviewDecision"`
			State          string `json:"state"`
		}
		if json.Unmarshal([]byte(out), &pr) == nil {
			if pr.ReviewDecision == "APPROVED" {
				evidence["pr_approved"] = "true"
			} else {
				evidence["pr_approved"] = "false"
				if pr.ReviewDecision != "" {
					evidence["pr_approved_detail"] = pr.ReviewDecision
				} else {
					evidence["pr_approved_detail"] = "no reviews yet"
				}
			}
			if pr.State == "MERGED" {
				evidence["pr_merged"] = "true"
			} else {
				evidence["pr_merged"] = "false"
			}
		}
	} else {
		evidence["pr_approved"] = "false"
		evidence["pr_approved_detail"] = "no PR found"
		evidence["pr_merged"] = "false"
	}

	return evidence
}

// createSessionMarker writes a marker file so the hook-handler knows this session
// has an active workflow. Without the marker, hooks are no-ops.
func createSessionMarker(sessionID string) {
	dir := filepath.Join(os.TempDir(), "wf-agents-sessions")
	os.MkdirAll(dir, 0o755)
	marker := filepath.Join(dir, sessionID)
	if err := os.WriteFile(marker, []byte(sessionID), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create session marker: %v\n", err)
	}
}

// removeSessionMarker deletes the marker file for the given session so that
// hook-handler becomes a no-op after the workflow reaches COMPLETE.
func removeSessionMarker(sessionID string) {
	marker := filepath.Join(os.TempDir(), "wf-agents-sessions", sessionID)
	if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Warning: could not remove session marker: %v\n", err)
	}
}

func runCmd(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
