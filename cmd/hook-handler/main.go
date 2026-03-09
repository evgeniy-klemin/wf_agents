package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eklemin/wf-agents/internal/model"
	wf "github.com/eklemin/wf-agents/internal/workflow"
	"go.temporal.io/sdk/client"
)

const taskQueue = "coding-session"

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
type hookOutput struct {
	Continue          bool                    `json:"continue"`
	HookSpecificOutput *hookSpecificOutput    `json:"hookSpecificOutput,omitempty"`
}

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

	// On SessionStart — auto-create workflow
	if input.HookEventName == "SessionStart" {
		ensureWorkflowExists(ctx, c, workflowID, input)
	}

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
		phase := queryPhase(ctx, c, workflowID)

		// Check if tool is allowed in this phase
		decision := checkToolPermission(phase, input.ToolName, input.ToolInput)

		if decision.denied {
			// Record denial in Temporal
			detail["denied"] = "true"
			detail["reason"] = decision.reason
			sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
				HookType:  "PreToolUse",
				SessionID: input.SessionID,
				Tool:      input.ToolName,
				Detail:    detail,
			})

			// Block the tool call
			out := hookOutput{
				Continue: true,
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:            "PreToolUse",
					PermissionDecision:       "deny",
					PermissionDecisionReason: decision.reason,
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
		// Re-query phase after possible auto-transition (e.g., AskUserQuestion → BLOCKED)
		currentPhase := queryPhase(ctx, c, workflowID)
		instructions := phaseInstructions(currentPhase)
		if instructions != "" {
			out := hookOutput{
				Continue: true,
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:    "PreToolUse",
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
			Continue: true,
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

// phaseInstructions returns context-specific enforcement instructions for the current phase.
// These are injected as additionalContext on every PreToolUse to keep Claude on track.
func phaseInstructions(phase model.Phase) string {
	wfc := wfClientBin()
	switch phase {
	case model.PhasePlanning:
		return fmt.Sprintf("PHASE: PLANNING. You are the Team Lead. Analyze the task, explore the codebase, create a plan. DO NOT write/edit code — file writes will be blocked by git restrictions. Only read-only tools allowed. When ready, run: %s transition <id> --to RESPAWN --reason \"<plan summary>\". If transition is DENIED (exit 1), read the error and adjust.", wfc)

	case model.PhaseRespawn:
		return fmt.Sprintf("PHASE: RESPAWN. Kill existing subagents and spawn fresh ones with clean context. File writes (Edit/Write) are BLOCKED in this phase. Only agent management and reads allowed. When agents are ready: %s transition <id> --to DEVELOPING. If DENIED, check status.", wfc)

	case model.PhaseDeveloping:
		return "PHASE: DEVELOPING. A Developer subagent should be doing the work. If you are the Team Lead, do not write code yourself — spawn a Developer subagent. If you are the Developer, implement via TDD. Git commit/push are BLOCKED — only in COMMITTING phase."

	case model.PhaseReviewing:
		return "PHASE: REVIEWING. A Reviewer subagent should be doing the work. If you are the Reviewer, DO NOT modify files — only read and report VERDICT: APPROVED or VERDICT: REJECTED. Git commit/push are BLOCKED."

	case model.PhaseCommitting:
		return fmt.Sprintf("PHASE: COMMITTING. Git commit and push are ALLOWED here. Commit and push approved changes. Then: more iterations → %s transition <id> --to RESPAWN (may be DENIED if max iterations reached), or all done → %s transition <id> --to PR_CREATION. Always check exit code.", wfc, wfc)

	case model.PhasePRCreation:
		return fmt.Sprintf("PHASE: PR_CREATION. Create a draft PR with gh pr create --draft --base BASE_BRANCH. After creating the PR, present the URL to the user and WAIT for CI checks to pass. Then: %s transition <id> --to FEEDBACK.", wfc)

	case model.PhaseFeedback:
		return fmt.Sprintf("PHASE: FEEDBACK. STOP and WAIT for human PR review. DO NOT transition to COMPLETE until the user explicitly approves. Present the PR URL and wait. When the user provides feedback: changes needed → %s transition <id> --to RESPAWN, user approves → %s transition <id> --to COMPLETE.", wfc, wfc)

	case model.PhaseComplete:
		return "PHASE: COMPLETE. Workflow finished. No further actions allowed. All tool calls except reads will be denied."

	case model.PhaseBlocked:
		return fmt.Sprintf("PHASE: BLOCKED. Waiting for human intervention. DO NOT proceed. DO NOT attempt any transitions except back to the pre-blocked phase. Check: %s status <id> to see pre_blocked_phase.", wfc)

	default:
		return ""
	}
}

func ensureWorkflowExists(ctx context.Context, c client.Client, workflowID string, input claudeHookInput) {
	taskDesc := fmt.Sprintf("Claude Code session in %s", filepath.Base(input.CWD))

	wfInput := model.WorkflowInput{
		SessionID:       input.SessionID,
		TaskDescription: taskDesc,
		RepoPath:        input.CWD,
		MaxIterations:   10,
	}

	startEvt := model.SignalHookEvent{
		HookType:  "SessionStart",
		SessionID: input.SessionID,
		Detail: map[string]string{
			"source": input.Source,
			"model":  input.Model,
			"cwd":    input.CWD,
		},
	}

	opts := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: taskQueue,
		Memo: map[string]interface{}{
			"task": taskDesc,
		},
	}

	_, err := c.SignalWithStartWorkflow(ctx, workflowID, wf.SignalHookEvent, startEvt, opts, wf.CodingSessionWorkflow, wfInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to ensure workflow %s: %v\n", workflowID, err)
	} else {
		fmt.Fprintf(os.Stderr, "Workflow %s ready (session: %s, project: %s)\n", workflowID, input.SessionID, filepath.Base(input.CWD))
	}
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

// wfClientBin returns the absolute path to wf-client binary (sibling of hook-handler).
func wfClientBin() string {
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

// --- Permission enforcement ---
//
// Matches the original NTCoding model:
// - RESPAWN: all file writes (Edit/Write/NotebookEdit) are forbidden
// - Global: git commit, git push, git checkout are forbidden in ALL phases
//   except per-phase exemptions:
//     PLANNING: git checkout allowed
//     COMMITTING: git commit, git push allowed
// - No other tool restrictions (BLOCKED, COMPLETE, etc. have no enforcement)

type permissionCheck struct {
	denied bool
	reason string
}

// fileWritingTools are tools that modify files.
var fileWritingTools = map[string]bool{
	"Edit": true, "Write": true, "NotebookEdit": true,
}

// forbiddenGitCommands are git subcommands forbidden globally by default.
var forbiddenGitCommands = []string{"git commit", "git push", "git checkout"}

// gitExemptions lists which git commands are allowed per phase.
var gitExemptions = map[model.Phase][]string{
	model.PhasePlanning:   {"git checkout"},
	model.PhaseCommitting: {"git commit", "git push"},
}

// checkToolPermission checks whether a tool is allowed in the given phase.
func checkToolPermission(phase model.Phase, toolName string, toolInput json.RawMessage) permissionCheck {
	// Read-only tools are always allowed
	readOnlyTools := map[string]bool{
		"Read": true, "Glob": true, "Grep": true,
		"WebFetch": true, "WebSearch": true,
		"ToolSearch": true, "LSP": true,
	}
	if readOnlyTools[toolName] {
		return permissionCheck{denied: false}
	}

	// PLANNING and RESPAWN: all file writes are forbidden (read-only phases)
	if (phase == model.PhasePlanning || phase == model.PhaseRespawn) && fileWritingTools[toolName] {
		return permissionCheck{
			denied: true,
			reason: fmt.Sprintf("File writes are forbidden in %s phase. %s", phase, phaseHint(phase)),
		}
	}

	// Bash: enforce global git command restrictions with per-phase exemptions
	if toolName == "Bash" {
		return checkBashPermission(phase, toolInput)
	}

	return permissionCheck{denied: false}
}

// safeGitSubcommands are read-only git subcommands allowed in PLANNING.
var safeGitSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true,
	"branch": true, "remote": true, "tag": true, "describe": true,
	"rev-parse": true, "ls-files": true, "ls-tree": true,
	"blame": true, "shortlog": true, "stash list": true,
	"config": true, "help": true, "version": true,
	"checkout": true, // allowed in PLANNING for branch creation
}

// safeBashPrefixes are read-only bash commands allowed in PLANNING.
var safeBashPrefixes = []string{
	"ls", "cat", "head", "tail", "less", "more", "wc", "file",
	"find", "grep", "rg", "ag", "awk", "sort", "uniq", "diff",
	"which", "where", "type", "command", "echo", "printf",
	"pwd", "cd", "tree", "stat", "du", "df",
	"gh pr view", "gh pr list", "gh pr checks", "gh pr diff",
	"gh issue view", "gh issue list",
	"gh api", "gh repo view",
	"go test", "go vet", "go build", "go list", "go mod",
	"npm test", "npm run lint", "npx", "yarn test",
	"make", "cargo test", "cargo check", "cargo clippy",
	"python -m pytest", "pytest", "python -c",
	"jq", "yq", "curl", "wget",
	"env", "printenv", "set", "export",
	"date", "uname", "whoami", "hostname",
	"true", "false", "test", "[",
}

// checkBashPermission enforces bash command restrictions per phase.
func checkBashPermission(phase model.Phase, toolInput json.RawMessage) permissionCheck {
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(toolInput, &input); err != nil {
		return permissionCheck{denied: false}
	}
	cmd := strings.TrimSpace(input.Command)

	// PLANNING: whitelist approach — only safe commands allowed
	if phase == model.PhasePlanning {
		return checkPlanningBash(cmd)
	}

	// Other phases: blacklist approach — block specific git commands
	exemptions := gitExemptions[phase]
	for _, forbidden := range forbiddenGitCommands {
		if matchesBashPrefix(cmd, forbidden) {
			exempted := false
			for _, ex := range exemptions {
				if ex == forbidden {
					exempted = true
					break
				}
			}
			if !exempted {
				return permissionCheck{
					denied: true,
					reason: fmt.Sprintf("%q is not allowed in %s phase. %s", forbidden, phase, phaseHint(phase)),
				}
			}
		}
	}

	return permissionCheck{denied: false}
}

// checkPlanningBash uses a whitelist: only safe read-only commands in PLANNING.
func checkPlanningBash(cmd string) permissionCheck {
	// Handle pipes/chains: check each sub-command
	for _, segment := range splitBashCommands(cmd) {
		seg := strings.TrimSpace(segment)
		if seg == "" {
			continue
		}

		if strings.HasPrefix(seg, "git ") || seg == "git" {
			if !isAllowedGitInPlanning(seg) {
				return permissionCheck{
					denied: true,
					reason: fmt.Sprintf("git command %q is not allowed in PLANNING phase — only read-only git operations permitted. Transition to RESPAWN first.", seg),
				}
			}
			continue
		}

		if isSafeBashCommand(seg) {
			continue
		}

		return permissionCheck{
			denied: true,
			reason: fmt.Sprintf("Command %q is not in the allowed list for PLANNING phase — no repository modifications allowed. Transition to RESPAWN to begin development.", truncateCmd(seg, 60)),
		}
	}

	return permissionCheck{denied: false}
}

// isAllowedGitInPlanning checks if a git command is safe (read-only) for PLANNING.
func isAllowedGitInPlanning(cmd string) bool {
	parts := strings.Fields(cmd)
	if len(parts) < 2 {
		return false
	}
	// Skip flags before subcommand (e.g., git -C /path status)
	idx := 1
	for idx < len(parts) && strings.HasPrefix(parts[idx], "-") {
		idx++
		// Skip flag value for flags that take arguments
		if idx < len(parts) && (parts[idx-1] == "-C" || parts[idx-1] == "-c" || parts[idx-1] == "--git-dir" || parts[idx-1] == "--work-tree") {
			idx++
		}
	}
	if idx >= len(parts) {
		return false
	}
	subCmd := parts[idx]
	return safeGitSubcommands[subCmd]
}

// isSafeBashCommand checks if a command matches any safe prefix for PLANNING.
func isSafeBashCommand(cmd string) bool {
	for _, prefix := range safeBashPrefixes {
		if matchesBashPrefix(cmd, prefix) {
			return true
		}
	}
	return false
}

// splitBashCommands splits a command line on pipes and command separators.
func splitBashCommands(cmd string) []string {
	var parts []string
	var current strings.Builder
	inSingle, inDouble := false, false

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
			current.WriteByte(ch)
		case ch == '"' && !inSingle:
			inDouble = !inDouble
			current.WriteByte(ch)
		case !inSingle && !inDouble && (ch == '|' || ch == ';' || ch == '\n'):
			parts = append(parts, current.String())
			current.Reset()
			// Skip && and ||
			if i+1 < len(cmd) && (cmd[i+1] == '|' || cmd[i+1] == '&') {
				i++
			}
		case !inSingle && !inDouble && ch == '&':
			parts = append(parts, current.String())
			current.Reset()
			if i+1 < len(cmd) && cmd[i+1] == '&' {
				i++
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func truncateCmd(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// phaseHint returns a short guidance message for denied actions in a phase.
func phaseHint(phase model.Phase) string {
	switch phase {
	case model.PhasePlanning:
		return "Transition to RESPAWN first."
	case model.PhaseRespawn:
		return "Only agent management allowed. Transition to DEVELOPING when agents are ready."
	case model.PhaseReviewing:
		return "If issues found, transition back to DEVELOPING."
	case model.PhaseCommitting:
		return "Only git operations are allowed."
	case model.PhasePRCreation:
		return "Only PR creation commands allowed."
	case model.PhaseComplete:
		return "Workflow is complete. No further actions needed."
	case model.PhaseBlocked:
		return "Waiting for human intervention. Transition back to the pre-blocked phase when unblocked."
	default:
		return ""
	}
}

// matchesBashPrefix checks if a bash command starts with the given prefix at a word boundary.
func matchesBashPrefix(cmd, prefix string) bool {
	if !strings.HasPrefix(cmd, prefix) {
		return false
	}
	if len(cmd) == len(prefix) {
		return true
	}
	c := cmd[len(prefix)]
	return c == ' ' || c == '\t' || c == '|' || c == ';' || c == '&' || c == '\n'
}
