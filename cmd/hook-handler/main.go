package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/eklemin/wf-agents/internal/model"
	wf "github.com/eklemin/wf-agents/internal/workflow"
	"go.temporal.io/sdk/client"
)

// claudeHookInput matches the exact JSON schema that Claude Code sends via stdin to hooks.
type claudeHookInput struct {
	SessionID      string          `json:"session_id"`
	HookEventName  string          `json:"hook_event_name"`
	CWD            string          `json:"cwd"`
	PermissionMode string          `json:"permission_mode"`
	TranscriptPath string          `json:"transcript_path"`
	ToolName       string          `json:"tool_name,omitempty"`
	ToolInput      json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse   json.RawMessage `json:"tool_response,omitempty"`
	ToolUseID      string          `json:"tool_use_id,omitempty"`
	AgentID        string          `json:"agent_id,omitempty"`
	AgentType      string          `json:"agent_type,omitempty"`
	Prompt         string          `json:"prompt,omitempty"`
	Source         string          `json:"source,omitempty"`
	Model          string          `json:"model,omitempty"`
	LastMessage    string          `json:"last_assistant_message,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// hookOutput is the JSON structure that Claude Code expects on stdout (exit 0).
// Continue is a pointer so it is omitted from JSON when nil — deny responses must
// NOT include "continue" at all, otherwise Claude Code ignores the deny decision.
type hookOutput struct {
	Continue           *bool               `json:"continue,omitempty"`
	HookSpecificOutput *hookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool { return &b }

type hookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	var input claudeHookInput
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		log.Fatalf("Failed to parse hook input: %v", err)
	}

	if input.SessionID == "" {
		fmt.Fprintln(os.Stderr, "Warning: no session_id in hook input, skipping")
		os.Exit(0)
	}

	// No active workflow for this session → hooks are no-ops
	if !sessionMarkerExists(input.SessionID) {
		os.Exit(0)
	}

	workflowID := "coding-session-" + input.SessionID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := client.Dial(client.Options{
		HostPort: temporalHost(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot connect to Temporal: %v\n", err)
		os.Exit(0)
	}
	defer c.Close()

	detail := buildDetail(input)

	// --- Auto-BLOCKED / auto-unblock logic (before per-event handling) ---
	//
	// "Blocking" events: Notification, TeammateIdle → transition TO BLOCKED
	// All other events from Claude Code → transition BACK from BLOCKED
	// "Blocking" events: agent stopped working, waiting for input
	// Stop = Claude finished turn, waiting for user input
	// Notification = system notification (e.g., teammate needs attention)
	// TeammateIdle = subagent waiting
	isBlockingEvent := input.HookEventName == "Stop" || input.HookEventName == "Notification" || input.HookEventName == "TeammateIdle"

	if isBlockingEvent {
		phase := queryPhase(ctx, c, workflowID)
		autoBlockPhases := map[model.Phase]bool{
			model.PhasePlanning:   true,
			model.PhaseDeveloping: true,
			model.PhaseReviewing:  true,
			model.PhaseCommitting: true,
			model.PhaseRespawn:    true,
			model.PhasePRCreation: true,
			model.PhaseFeedback:   true,
		}
		if autoBlockPhases[phase] {
			autoTransition(ctx, c, workflowID, input.SessionID, model.PhaseBlocked,
				fmt.Sprintf("auto: %s in %s", input.HookEventName, phase))
		}
	} else if input.HookEventName != "SessionStart" {
		// Any active event (tool use, user prompt, subagent, etc.) → auto-unblock
		status := queryStatus(ctx, c, workflowID)
		if status.Phase == model.PhaseBlocked && status.PreBlockedPhase != "" {
			autoTransition(ctx, c, workflowID, input.SessionID, status.PreBlockedPhase,
				fmt.Sprintf("auto: %s received, returning to %s", input.HookEventName, status.PreBlockedPhase))
		}
	}

	switch input.HookEventName {
	case "PreToolUse":
		status := queryStatus(ctx, c, workflowID)
		phase := status.Phase

		// Check if tool is allowed in this phase
		decision := wf.CheckToolPermission(phase, input.ToolName, input.ToolInput, input.AgentID, status.ActiveAgents)

		if decision.Denied {
			// Record denial in Temporal
			detail["denied"] = "true"
			detail["reason"] = decision.Reason
			sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
				HookType:  "PreToolUse",
				SessionID: input.SessionID,
				Tool:      input.ToolName,
				Detail:    detail,
			})

			// Block the tool call.
			// Exit code 2 signals a denial to Claude Code.
			// Write reason to stderr (logged) and stdout (shown to Claude).
			fmt.Fprintf(os.Stderr, "DENIED: %s\n", decision.Reason)
			fmt.Fprintf(os.Stdout, "%s\n", decision.Reason)
			os.Exit(2)
		}

		if decision.Allowed {
			// Auto-approve: bypass Claude Code permission prompt
			sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
				HookType:  "PreToolUse",
				SessionID: input.SessionID,
				Tool:      input.ToolName,
				Detail:    detail,
			})
			currentStatus := queryStatus(ctx, c, workflowID)
			currentPhase := currentStatus.Phase
			instructions := phaseInstructions(currentPhase)
			out := hookOutput{
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:            "PreToolUse",
					PermissionDecision:       "allow",
					PermissionDecisionReason: "Safe command auto-approved by workflow",
					AdditionalContext:        fmt.Sprintf("[Workflow Phase: %s] %s", currentPhase, instructions),
				},
			}
			json.NewEncoder(os.Stdout).Encode(out)
			os.Exit(0)
		}

		// Tool allowed — send event + inject context
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "PreToolUse",
			SessionID: input.SessionID,
			Tool:      input.ToolName,
			Detail:    detail,
		})
		// Re-query status after possible auto-transition (e.g., AskUserQuestion → BLOCKED)
		currentStatus := queryStatus(ctx, c, workflowID)
		currentPhase := currentStatus.Phase
		instructions := phaseInstructions(currentPhase)
		if instructions != "" {
			out := hookOutput{
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:     "PreToolUse",
					AdditionalContext: fmt.Sprintf("[Workflow Phase: %s] %s", currentPhase, instructions),
				},
			}
			json.NewEncoder(os.Stdout).Encode(out)
		}

	case "PostToolUse":
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "PostToolUse",
			SessionID: input.SessionID,
			Tool:      input.ToolName,
			Detail:    detail,
		})

	case "SubagentStart":
		detail["agent_id"] = input.AgentID
		detail["agent_type"] = input.AgentType
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "SubagentStart",
			SessionID: input.SessionID,
			Detail:    detail,
		})

	case "SubagentStop":
		detail["agent_id"] = input.AgentID
		detail["agent_type"] = input.AgentType
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "SubagentStop",
			SessionID: input.SessionID,
			Detail:    detail,
		})

	case "PostToolUseFailure":
		detail["error"] = input.Error
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "PostToolUseFailure",
			SessionID: input.SessionID,
			Tool:      input.ToolName,
			Detail:    detail,
		})

	case "Notification":
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "Notification",
			SessionID: input.SessionID,
			Detail:    detail,
		})

	case "TeammateIdle":
		detail["agent_id"] = input.AgentID
		detail["agent_type"] = input.AgentType
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "TeammateIdle",
			SessionID: input.SessionID,
			Detail:    detail,
		})

	case "Stop":
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "Stop",
			SessionID: input.SessionID,
			Detail:    detail,
		})

	case "SessionStart":
		detail["source"] = input.Source
		detail["model"] = input.Model
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "SessionStart",
			SessionID: input.SessionID,
			Detail:    detail,
		})
		// Inject session context so Claude knows its workflow ID and wf-client path
		wfClientPath := wfClientBin()
		out := hookOutput{
			Continue: boolPtr(true),
			HookSpecificOutput: &hookSpecificOutput{
				HookEventName: "SessionStart",
				AdditionalContext: fmt.Sprintf(
					"WORKFLOW SESSION STARTED.\n"+
						"Session ID: %s\n"+
						"Workflow ID: %s\n"+
						"wf-client path: %s\n"+
						"Current phase: PLANNING.\n"+
						"To transition phases: %s transition %s --to <PHASE> --reason \"<why>\"\n"+
						"Read CLAUDE.md for your full autonomous workflow protocol. You are the Team Lead.",
					input.SessionID, workflowID, wfClientPath, wfClientPath, workflowID),
			},
		}
		json.NewEncoder(os.Stdout).Encode(out)

	case "UserPromptSubmit":
		detail["prompt"] = input.Prompt
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "UserPromptSubmit",
			SessionID: input.SessionID,
			Detail:    detail,
		})
		// First user prompt is the task description — set it in the workflow
		if input.Prompt != "" {
			setTask(ctx, c, workflowID, input.Prompt)
		}

	case "PermissionRequest":
		// Auto-allow safe commands when Claude Code would prompt the user
		prStatus := queryStatus(ctx, c, workflowID)
		prDecision := wf.CheckToolPermission(prStatus.Phase, input.ToolName, input.ToolInput, input.AgentID, prStatus.ActiveAgents)

		if prDecision.Allowed {
			detail["auto_allowed"] = "true"
			sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
				HookType:  "PermissionRequest",
				SessionID: input.SessionID,
				Tool:      input.ToolName,
				Detail:    detail,
			})
			out := map[string]interface{}{
				"hookSpecificOutput": map[string]interface{}{
					"hookEventName": "PermissionRequest",
					"decision": map[string]interface{}{
						"behavior": "allow",
					},
				},
			}
			json.NewEncoder(os.Stdout).Encode(out)
			os.Exit(0)
		}

		// Not auto-allowed — log to Temporal and let Claude Code show the permission prompt
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "PermissionRequest",
			SessionID: input.SessionID,
			Tool:      input.ToolName,
			Detail:    detail,
		})

	default:
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  input.HookEventName,
			SessionID: input.SessionID,
			Detail:    detail,
		})
	}

	os.Exit(0)
}

// queryPhase fetches current workflow phase via Temporal query.
func queryPhase(ctx context.Context, c client.Client, workflowID string) model.Phase {
	resp, err := c.QueryWorkflow(ctx, workflowID, "", wf.QueryPhase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot query phase for %s: %v\n", workflowID, err)
		return model.PhasePlanning // default
	}
	var phase model.Phase
	if err := resp.Get(&phase); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot decode phase: %v\n", err)
		return model.PhasePlanning
	}
	return phase
}

// phaseInstructions returns comprehensive enforcement instructions for the current phase.
// Injected as additionalContext on every PreToolUse — this is the PRIMARY mechanism
// that keeps Claude on track (since plugin CLAUDE.md is project docs, not workflow rules).
func phaseInstructions(phase model.Phase) string {
	wfc := wfClientBin()

	pluginRoot := os.Getenv("CLAUDE_PLUGIN_ROOT")
	if pluginRoot == "" {
		pluginRoot = "${CLAUDE_PLUGIN_ROOT}"
	}
	agentFile := pluginRoot + "/agents/feature-team-lead.md"

	// Preamble for Team Lead phases — where the main agent is acting
	teamLeadPreamble := "You are the Team Lead. You NEVER write code or review code. You plan, delegate, and coordinate.\n" +
		"If a tool call is denied, DO NOT retry — follow the denial reason.\n" +
		"CONTEXT RECOVERY: If context was compressed or you lost your role instructions, " +
		"re-read your full protocol: " + agentFile + "\n\n"

	// Enforcement-only preamble for phases where subagents act
	enforcementPreamble := "If a tool call is denied, DO NOT retry — follow the denial reason.\n\n"

	switch phase {
	case model.PhasePlanning:
		return teamLeadPreamble + fmt.Sprintf(`PHASE: PLANNING — Read-only exploration and planning.

CHECKLIST (in order — do NOT skip steps):
- [ ] Run git branch --show-current → record as BASE_BRANCH
- [ ] Create feature branch: git checkout -b <feature-branch-name> (MANDATORY — never commit to BASE_BRANCH)
- [ ] Read relevant files, explore codebase structure
- [ ] Identify files to create or modify
- [ ] Break task into ordered iteration subtasks
- [ ] Define testing strategy
- [ ] Get user approval for the plan
- [ ] Transition: %s transition <id> --to RESPAWN --reason "Plan: <summary>"

BLOCKED ACTIONS: Edit, Write, NotebookEdit, unsafe Bash commands. Only read-only tools allowed.
If transition DENIED (exit 1): read error, adjust plan.
DO NOT offer to clear context or auto-accept edits. Transition to RESPAWN — that is the designed context reset.`, wfc)

	case model.PhaseRespawn:
		return teamLeadPreamble + fmt.Sprintf(`PHASE: RESPAWN — Spawn fresh agents with clean context.

CHECKLIST:
- [ ] Kill existing Developer/Reviewer subagents
- [ ] Prepare iteration context (plan + current iteration info)
- [ ] Spawn fresh agents — DO NOT pass stale context from prior iterations
- [ ] Transition: %s transition <id> --to DEVELOPING --reason "Iteration N: <task>"

BLOCKED ACTIONS: Edit, Write, NotebookEdit. Only agent management and reads.`, wfc)

	case model.PhaseDeveloping:
		return enforcementPreamble + fmt.Sprintf(`PHASE: DEVELOPING — Developer subagent implements via TDD.

IF YOU ARE THE TEAM LEAD: Do NOT write code yourself. Spawn a Developer subagent.
  Agent instructions: use .claude/agents/developer.md if it exists, otherwise %s/agents/developer.md.
IF YOU ARE THE DEVELOPER: Implement via TDD — tests first, then code, then refactor.
  Use simple, single-purpose Bash commands (go test ./..., npm test, make test).
  For complex commands — create a helper script in scripts/ and run ./scripts/<name>.sh.
  Do NOT use pipes, subshells, or multi-command chains — they block auto-approve.
  Do NOT run git add, git commit, or git push — leave changes uncommitted on disk.
  The REVIEWING guard requires a dirty working tree (uncommitted changes).

CHECKLIST:
- [ ] Load developer agent: .claude/agents/developer.md (project) or %s/agents/developer.md (plugin default)
- [ ] Spawn Developer subagent with: agent instructions, plan, iteration number, prior rejection feedback
- [ ] Developer writes failing tests
- [ ] Developer implements to pass tests
- [ ] Developer runs all tests (simple commands only)
- [ ] Leave all changes UNCOMMITTED — do not git add or git commit
- [ ] Transition: %s transition <id> --to REVIEWING --reason "Development done, iteration N"

BLOCKED ACTIONS: git add, git commit, git push (only in COMMITTING phase).`, pluginRoot, pluginRoot, wfc)

	case model.PhaseReviewing:
		return enforcementPreamble + fmt.Sprintf(`PHASE: REVIEWING — Reviewer subagent validates code quality.

IF YOU ARE THE TEAM LEAD: Do NOT review code yourself. Spawn a Reviewer subagent.
  Agent instructions: use .claude/agents/reviewer.md if it exists, otherwise %s/agents/reviewer.md.
IF YOU ARE THE REVIEWER: Read-only. DO NOT modify files. Report verdict.

CHECKLIST:
- [ ] Load reviewer agent: .claude/agents/reviewer.md (project) or %s/agents/reviewer.md (plugin default)
- [ ] Spawn Reviewer subagent with: agent instructions, scope of changes, plan context
- [ ] Reviewer runs git diff, tests, linting
- [ ] Reviewer outputs VERDICT: APPROVED or VERDICT: REJECTED — <issues>
- [ ] If APPROVED → %s transition <id> --to COMMITTING --reason "Review approved"
- [ ] If REJECTED → %s transition <id> --to DEVELOPING --reason "Review rejected: <issues>"

BLOCKED ACTIONS: git commit, git push, Edit/Write (for Reviewer).`, pluginRoot, pluginRoot, wfc, wfc)

	case model.PhaseCommitting:
		return teamLeadPreamble + fmt.Sprintf(`PHASE: COMMITTING — Git commit and push are ALLOWED.

CHECKLIST:
- [ ] git add <specific files>
- [ ] git commit -m "<clear message>"
- [ ] git push
- [ ] Verify: git status (working tree must be clean)
- [ ] Decide: more iterations or all done?
  - More iterations → %s transition <id> --to RESPAWN --reason "Iteration N+1: <task>"
  - All done → %s transition <id> --to PR_CREATION --reason "All iterations complete"

VERIFY: You must be on the feature branch (not BASE_BRANCH). Run git branch --show-current to confirm.
If RESPAWN DENIED: max iterations reached, must go to PR_CREATION.`, wfc, wfc)

	case model.PhasePRCreation:
		return teamLeadPreamble + fmt.Sprintf(`PHASE: PR_CREATION — Create draft PR and wait for CI.

CHECKLIST:
- [ ] gh pr create --draft --base BASE_BRANCH --title "<title>" --body "<description>"
- [ ] Present PR URL to user
- [ ] Wait for CI checks to pass
- [ ] Transition: %s transition <id> --to FEEDBACK --reason "PR created: <url>, CI passing"

VERIFY: Current branch must NOT be BASE_BRANCH. If it is, you forgot to create a feature branch in PLANNING.
If BASE_BRANCH is not main/master, --base is REQUIRED.`, wfc)

	case model.PhaseFeedback:
		return teamLeadPreamble + fmt.Sprintf(`PHASE: FEEDBACK — Triage human PR review comments.

CHECKLIST:
- [ ] Check for comments: gh pr view --json reviewDecision,reviews,comments,state
- [ ] If NO comments yet — poll in a loop:
      Run "sleep 60" (Bash), then check again. Repeat until comments appear.
      Do NOT stop or go idle — keep polling.
- [ ] When comments found: gh api repos/{owner}/{repo}/pulls/{pr_number}/comments
- [ ] For each comment: Accept (implement) / Reject (reply with reasoning) / Escalate (BLOCKED)
- [ ] Reply to EVERY comment with a transparent, concise response:
      ACCEPTED: what was changed, which files, brief rationale
      REJECTED: technical reasoning why the change is not needed or harmful
      Keep replies short but with enough context for the reviewer to understand without checking the code
- [ ] Changes needed → %s transition <id> --to RESPAWN --reason "Implementing feedback: <summary>"
- [ ] All comments resolved but PR NOT approved/merged → continue polling loop:
      sleep 60, then gh pr view --json reviewDecision,reviews,comments,state
      Watch for: new comments, reviewDecision=APPROVED, or state=MERGED
      If new comments appear — triage them (repeat from checklist start)
- [ ] PR approved/merged → %s transition <id> --to COMPLETE --reason "All feedback resolved, PR approved/merged"
      GUARD: requires reviewDecision=APPROVED or state=MERGED. Will be DENIED otherwise.

IMPORTANT: Do NOT stop and wait passively. Poll actively using sleep + gh pr view loop.`, wfc, wfc)

	case model.PhaseComplete:
		return "PHASE: COMPLETE. Workflow finished. No further actions needed."

	case model.PhaseBlocked:
		return fmt.Sprintf(`PHASE: BLOCKED — Paused, waiting for human intervention.

DO NOT proceed. DO NOT attempt transitions except back to the pre-blocked phase.
Check: %s status <id> to see pre_blocked_phase.
When unblocked: transition ONLY to the pre-blocked phase.`, wfc)

	default:
		return ""
	}
}

// sessionMarkerExists checks if wf-client start has been run for this session.
// The marker file is created by wf-client start in $TMPDIR/wf-agents-sessions/<session-id>.
func sessionMarkerExists(sessionID string) bool {
	marker := filepath.Join(os.TempDir(), "wf-agents-sessions", sessionID)
	_, err := os.Stat(marker)
	return err == nil
}

func buildDetail(input claudeHookInput) map[string]string {
	d := map[string]string{
		"cwd": input.CWD,
	}
	if input.ToolName != "" {
		d["tool_name"] = input.ToolName
	}
	if input.ToolUseID != "" {
		d["tool_use_id"] = input.ToolUseID
	}
	if input.PermissionMode != "" {
		d["permission_mode"] = input.PermissionMode
	}
	if len(input.ToolInput) > 0 {
		d["tool_input"] = string(input.ToolInput)
	}
	if input.Error != "" {
		d["error"] = input.Error
	}
	return d
}

// autoTransition performs a workflow transition via UpdateWorkflow (same path as wf-client).
// Errors are logged but not fatal — the workflow continues even if auto-transition fails.
func autoTransition(ctx context.Context, c client.Client, workflowID, sessionID string, to model.Phase, reason string) {
	req := model.SignalTransition{
		To:        to,
		SessionID: sessionID,
		Reason:    reason,
	}
	handle, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   workflowID,
		UpdateName:   wf.UpdateTransition,
		Args:         []interface{}{req},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-transition to %s failed: %v\n", to, err)
		return
	}
	var result model.TransitionResult
	if err := handle.Get(ctx, &result); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-transition result error: %v\n", err)
		return
	}
	if result.Allowed {
		fmt.Fprintf(os.Stderr, "Auto-transition: %s → %s (%s)\n", result.From, result.To, reason)
	} else {
		fmt.Fprintf(os.Stderr, "Warning: auto-transition denied: %s → %s: %s\n", result.From, result.To, result.Reason)
	}
}

// queryStatus fetches full workflow status (phase + pre_blocked_phase) via Temporal query.
func queryStatus(ctx context.Context, c client.Client, workflowID string) model.WorkflowStatus {
	resp, err := c.QueryWorkflow(ctx, workflowID, "", wf.QueryStatus)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot query status for %s: %v\n", workflowID, err)
		return model.WorkflowStatus{Phase: model.PhasePlanning}
	}
	var status model.WorkflowStatus
	if err := resp.Get(&status); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot decode status: %v\n", err)
		return model.WorkflowStatus{Phase: model.PhasePlanning}
	}
	return status
}

func setTask(ctx context.Context, c client.Client, workflowID string, task string) {
	err := c.SignalWorkflow(ctx, workflowID, "", wf.SignalSetTask, task)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to set task for %s: %v\n", workflowID, err)
	}
}

func sendHookEvent(ctx context.Context, c client.Client, workflowID string, evt model.SignalHookEvent) {
	err := c.SignalWorkflow(ctx, workflowID, "", wf.SignalHookEvent, evt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to signal workflow %s: %v\n", workflowID, err)
	}
}

// wfClientBin returns the absolute path to wf-client binary.
// Prefers $CLAUDE_PLUGIN_ROOT/bin/wf-client, falls back to sibling of hook-handler.
func wfClientBin() string {
	if root := os.Getenv("CLAUDE_PLUGIN_ROOT"); root != "" {
		return filepath.Join(root, "bin", "wf-client")
	}
	exe, err := os.Executable()
	if err != nil {
		return "wf-client"
	}
	return filepath.Join(filepath.Dir(exe), "wf-client")
}

func temporalHost() string {
	if h := os.Getenv("TEMPORAL_HOST"); h != "" {
		return h
	}
	return "localhost:7233"
}

