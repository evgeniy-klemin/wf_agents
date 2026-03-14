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
	// "Blocking" events: Notification → always transition TO BLOCKED
	// Stop → only blocking if it comes from the Team Lead (empty AgentID or not in activeAgents).
	//   Teammates (non-empty AgentID in activeAgents) finishing their turn is normal — they idle
	//   while Lead and other teammates continue working. Only Team Lead's Stop means "waiting for input".
	// TeammateIdle is handled separately (phase-aware) in its case below.
	// All other events from Claude Code → transition BACK from BLOCKED
	isBlockingEvent := input.HookEventName == "Notification"
	if input.HookEventName == "Stop" {
		// Stop is blocking only if it comes from the Team Lead (not a teammate).
		// Team Lead has empty AgentID; teammates have non-empty AgentID in activeAgents.
		if input.AgentID == "" {
			isBlockingEvent = true
		} else {
			// Check if this agent is a known teammate (present in activeAgents).
			status := queryStatus(ctx, c, workflowID)
			isTeammate := false
			for _, id := range status.ActiveAgents {
				if id == input.AgentID {
					isTeammate = true
					break
				}
			}
			if !isTeammate {
				// Unknown agent or unrecognized ID — treat as blocking (safe default).
				isBlockingEvent = true
			}
			// If teammate — isBlockingEvent stays false; no auto-BLOCKED on teammate Stop.
		}
	}

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
	} else if input.HookEventName != "SessionStart" && input.HookEventName != "TeammateIdle" {
		// Any active event (tool use, user prompt, teammate, etc.) → auto-unblock
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

		// In Agent Teams, teammates idle intentionally during active work phases
		// (e.g., Developer done, waiting for next instruction from Team Lead).
		// Only auto-BLOCKED if idle in a phase where no teammate should be idle.
		phase := queryPhase(ctx, c, workflowID)
		teammateIdleExpected := map[model.Phase]bool{
			model.PhaseDeveloping: true,
			model.PhaseReviewing:  true,
			model.PhaseCommitting: true,
			model.PhasePRCreation: true,
		}
		if !teammateIdleExpected[phase] {
			autoTransition(ctx, c, workflowID, input.SessionID, model.PhaseBlocked,
				fmt.Sprintf("auto: TeammateIdle in %s", phase))
		}

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

	case "TaskCompleted":
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "TaskCompleted",
			SessionID: input.SessionID,
			Detail:    detail,
		})

	case "PermissionRequest":
		// Log to Temporal for audit trail — PreToolUse already handled auto-approve/deny,
		// so if we reach here, the user needs to decide.
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
// For most phases, content is loaded from states/<phase>.md under CLAUDE_PLUGIN_ROOT
// and placeholders ({{WF_CLIENT}}, {{PLUGIN_ROOT}}, {{AGENT_FILE}}) are substituted.
// COMPLETE and BLOCKED are kept as hardcoded one-liners.
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

	// Enforcement-only preamble for phases where teammates act
	enforcementPreamble := "If a tool call is denied, DO NOT retry — follow the denial reason.\n\n"

	// Keep simple one-liners hardcoded — no file needed.
	switch phase {
	case model.PhaseComplete:
		return "PHASE: COMPLETE. Workflow finished. No further actions needed."
	case model.PhaseBlocked:
		return fmt.Sprintf(`PHASE: BLOCKED — Paused, waiting for human intervention.

DO NOT proceed. DO NOT attempt transitions except back to the pre-blocked phase.
Check: %s status <id> to see pre_blocked_phase.
When unblocked: transition ONLY to the pre-blocked phase.`, wfc)
	}

	// Map each phase to its state file name and the appropriate preamble.
	type phaseConfig struct {
		filename string
		preamble string
	}
	configs := map[model.Phase]phaseConfig{
		model.PhasePlanning:   {"planning.md", teamLeadPreamble},
		model.PhaseRespawn:    {"respawn.md", teamLeadPreamble},
		model.PhaseDeveloping: {"developing.md", enforcementPreamble},
		model.PhaseReviewing:  {"reviewing.md", enforcementPreamble},
		model.PhaseCommitting: {"committing.md", teamLeadPreamble},
		model.PhasePRCreation: {"pr_creation.md", teamLeadPreamble},
		model.PhaseFeedback:   {"feedback.md", teamLeadPreamble},
	}

	cfg, ok := configs[phase]
	if !ok {
		return fmt.Sprintf("PHASE: %s", phase)
	}

	stateFile := filepath.Join(pluginRoot, "states", cfg.filename)
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return fmt.Sprintf("PHASE: %s", phase)
	}

	content := strings.NewReplacer(
		"{{WF_CLIENT}}", wfc,
		"{{PLUGIN_ROOT}}", pluginRoot,
		"{{AGENT_FILE}}", agentFile,
	).Replace(string(raw))

	return cfg.preamble + content
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
