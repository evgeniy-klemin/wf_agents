package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eklemin/wf-agents/internal/config"
	"github.com/eklemin/wf-agents/internal/model"
	"github.com/eklemin/wf-agents/internal/noplog"
	"github.com/eklemin/wf-agents/internal/session"
	internaltemporal "github.com/eklemin/wf-agents/internal/temporal"
	wf "github.com/eklemin/wf-agents/internal/workflow"
	enums "go.temporal.io/api/enums/v1"
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
	TeammateName   string          `json:"teammate_name,omitempty"`
	TeamName       string          `json:"team_name,omitempty"`
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

// logResponse appends a response entry to the session JSONL log.
func logResponse(sessionID string, event string, exitCode int, response interface{}) {
	logDir := filepath.Join(os.TempDir(), "wf-agents-hook-logs")
	logFile := filepath.Join(logDir, sessionID+".jsonl")
	entry := map[string]interface{}{
		"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		"event":     event,
		"direction": "response",
		"exit_code": exitCode,
		"response":  response,
	}
	line, _ := json.Marshal(entry)
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.Write(line)
		f.Write([]byte("\n"))
		f.Close()
	}
}

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	// Read raw stdin for diagnostics, then decode
	rawInput, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Failed to read stdin: %v", err)
	}

	var input claudeHookInput
	if err := json.Unmarshal(rawInput, &input); err != nil {
		log.Fatalf("Failed to parse hook input: %v", err)
	}

	// Append to session log file (JSONL format — one JSON object per line)
	logDir := filepath.Join(os.TempDir(), "wf-agents-hook-logs")
	_ = os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, input.SessionID+".jsonl")

	// Add timestamp and write as one line
	logEntry := map[string]interface{}{
		"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		"event":     input.HookEventName,
		"direction": "request",
		"raw":       json.RawMessage(rawInput),
	}
	logLine, _ := json.Marshal(logEntry)
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.Write(logLine)
		f.Write([]byte("\n"))
		f.Close()
	}

	// Capture any fields not in our struct
	var rawFields map[string]json.RawMessage
	_ = json.Unmarshal(rawInput, &rawFields)
	// Log unknown fields to stderr for discovery
	knownFields := map[string]bool{
		"session_id": true, "hook_event_name": true, "cwd": true,
		"permission_mode": true, "transcript_path": true,
		"tool_name": true, "tool_input": true, "tool_response": true,
		"tool_use_id": true, "agent_id": true, "agent_type": true,
		"prompt": true, "source": true, "model": true,
		"last_assistant_message": true, "error": true,
		"teammate_name": true, "team_name": true,
		"stop_hook_active": true,
	}
	for k := range rawFields {
		if !knownFields[k] {
			fmt.Fprintf(os.Stderr, "UNKNOWN FIELD in %s: %s = %s\n", input.HookEventName, k, string(rawFields[k]))
		}
	}

	if input.SessionID == "" {
		fmt.Fprintln(os.Stderr, "Warning: no session_id in hook input, skipping")
		os.Exit(0)
	}

	// No active workflow for this session → hooks are no-ops
	workflowID := resolveWorkflowID(input.SessionID, input.CWD)
	if workflowID == "" {
		os.Exit(0)
	}

	// Detect teammate sessions: their session_id differs from the workflow's.
	// Set a synthetic agent_id so IsTeammate() returns true and teammates get auto-approve.
	workflowSessionID := strings.TrimPrefix(workflowID, "coding-session-")
	if input.SessionID != workflowSessionID {
		if input.AgentID == "" {
			input.AgentID = "teammate-" + input.SessionID
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := client.Dial(client.Options{
		HostPort: temporalHost(),
		Logger:   noplog.New(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot connect to Temporal: %v\n", err)
		os.Exit(0)
	}
	defer c.Close()

	// Workflow execution finished (completed/terminated/cancelled) — skip all hook enforcement.
	if desc, err := c.DescribeWorkflowExecution(ctx, workflowID, ""); err == nil {
		if desc.WorkflowExecutionInfo.Status != enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
			os.Exit(0)
		}
	}

	detail := buildDetail(input)

	switch input.HookEventName {
	case "PreToolUse":
		status := queryStatus(ctx, c, workflowID)
		phase := status.Phase

		// Per-agent command tracking: run before permission check so that tracking
		// signals are sent regardless of which code path handles the allow/deny.
		// In PreToolUse, TeammateName is often empty; AgentType (e.g. "developer-1") is the fallback.
		if input.TeammateName != "" || input.AgentType != "" {
			trackPreToolUse(ctx, c, workflowID, input)
		}

		// Check if tool is allowed in this phase.
		// Use agent name (TeammateName || AgentType, e.g. "developer-1") for permission
		// matching — same approach as trackPreToolUse. Fallback to AgentID (UUID) so
		// IsTeammate() still returns true for teammates with no name/type.
		agentName := resolveAgentName(input)
		if agentName == "" {
			agentName = input.AgentID
		}
		decision := wf.CheckToolPermission(phase, input.ToolName, input.ToolInput, agentName, status.ActiveAgents)

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
			logResponse(input.SessionID, "PreToolUse", 2, map[string]string{
				"decision": "deny",
				"reason":   decision.Reason,
			})
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
			instructions := phaseInstructions(currentPhase, input.CWD)
			out := hookOutput{
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:            "PreToolUse",
					PermissionDecision:       "allow",
					PermissionDecisionReason: "Safe command auto-approved by workflow",
					AdditionalContext:        fmt.Sprintf("[Workflow Phase: %s] %s", currentPhase, instructions),
				},
			}
			json.NewEncoder(os.Stdout).Encode(out)
			logResponse(input.SessionID, "PreToolUse", 0, map[string]string{
				"decision": "allow",
				"phase":    string(currentPhase),
			})
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
		instructions := phaseInstructions(currentPhase, input.CWD)
		if instructions != "" {
			out := hookOutput{
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:     "PreToolUse",
					AdditionalContext: fmt.Sprintf("[Workflow Phase: %s] %s", currentPhase, instructions),
				},
			}
			json.NewEncoder(os.Stdout).Encode(out)
		}
		logResponse(input.SessionID, "PreToolUse", 0, map[string]string{
			"decision": "pass",
			"phase":    string(currentPhase),
		})

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
		detail["teammate_name"] = input.TeammateName
		detail["team_name"] = input.TeamName
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "TeammateIdle",
			SessionID: input.SessionID,
			Detail:    detail,
		})

		// Determine who is idle and enforce appropriate constraints.
		// If teammate_name is non-empty (or agent_id is non-empty), this is a teammate going idle.
		// If both are empty, assume it is the Team Lead.
		phase := queryPhase(ctx, c, workflowID)
		isTeammate := input.TeammateName != "" || input.AgentID != ""

		if !isTeammate {
			// This is the Team Lead going idle — use config-driven deny rules.
			if msg := evalLeadStopConfig(input.CWD, string(phase)); msg != "" {
				reason := fmt.Sprintf("DENIED: %s", msg)
				fmt.Fprintf(os.Stderr, "%s\n", reason)
				logResponse(input.SessionID, "TeammateIdle", 2, map[string]string{
					"action": "keep_working",
					"reason": reason,
				})
				os.Exit(2)
			}
		} else {
			// Teammate going idle — query per-agent command tracking then evaluate config-driven idle rules.
			agentName := resolveAgentName(input)
			commandsRan := queryAgentCommands(ctx, c, workflowID, agentName)
			// Skip command_ran idle checks if the agent hasn't changed any files yet.
			// This prevents denying idle immediately after spawn (before the agent starts working).
			if !commandsRan["_file_changed"] {
				break
			}
			if reason := evalTeammateIdleConfig(input.CWD, string(phase), agentName, commandsRan); reason != "" {
				fmt.Fprintf(os.Stderr, "%s\n", reason)
				logResponse(input.SessionID, "TeammateIdle", 2, map[string]string{
					"action": "keep_working",
					"reason": reason,
				})
				os.Exit(2)
			}
		}

	case "Stop":
		sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
			HookType:  "Stop",
			SessionID: input.SessionID,
			Detail:    detail,
		})

		// Deny Stop from Team Lead if config says so for this phase.
		isTeammate := input.TeammateName != "" || input.AgentID != ""
		if !isTeammate {
			phase := queryPhase(ctx, c, workflowID)
			if msg := evalLeadStopConfig(input.CWD, string(phase)); msg != "" {
				reason := fmt.Sprintf("DENIED: %s", msg)
				fmt.Fprintf(os.Stderr, "%s\n", reason)
				logResponse(input.SessionID, "Stop", 2, map[string]string{
					"action": "keep_working",
					"reason": reason,
				})
				os.Exit(2)
			}
		}

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
		logResponse(input.SessionID, "SessionStart", 0, map[string]string{
			"action":      "context_injected",
			"workflow_id": workflowID,
		})

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

	logResponse(input.SessionID, input.HookEventName, 0, map[string]string{
		"action": "logged",
	})
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
// Content is loaded from workflow/<PHASE>.md under CLAUDE_PLUGIN_ROOT, with a project-level
// override checked first at <cwd>/.wf-agents/<PHASE>.md. Placeholders
// ({{WF_CLIENT}}, {{PLUGIN_ROOT}}, {{AGENT_FILE}}) are substituted.
// The .md filename defaults to PHASE.md (uppercase); it is no longer read from config.
func phaseInstructions(phase model.Phase, cwd string) string {
	wfc := wfClientBin()

	pluginRoot := os.Getenv("CLAUDE_PLUGIN_ROOT")
	if pluginRoot == "" {
		pluginRoot = "${CLAUDE_PLUGIN_ROOT}"
	}
	agentFile := pluginRoot + "/agents/iriski-team-lead.md"

	// Preamble for Team Lead phases — where the main agent is acting
	teamLeadPreamble := "You are the Team Lead. You NEVER write code or review code. You plan, delegate, and coordinate.\n" +
		"If a tool call is denied, DO NOT retry — follow the denial reason.\n" +
		"CONTEXT RECOVERY: If context was compressed or you lost your role instructions, " +
		"re-read your full protocol: " + agentFile + "\n\n"

	// Enforcement-only preamble for phases where teammates act
	enforcementPreamble := "If a tool call is denied, DO NOT retry — follow the denial reason.\n\n"

	// Phases where teammates act (not the Team Lead).
	teammatePhases := map[model.Phase]bool{
		model.PhaseDeveloping: true,
		model.PhaseReviewing:  true,
	}

	preamble := teamLeadPreamble
	if teammatePhases[phase] {
		preamble = enforcementPreamble
	}

	// Filename is PHASE.md (uppercase). No longer read from config.
	filename := string(phase) + ".md"

	// Check for project-level override first: <cwd>/.wf-agents/<PHASE>.md
	var raw []byte
	var err error
	if cwd != "" {
		projectFile := filepath.Join(cwd, ".wf-agents", filename)
		raw, err = os.ReadFile(projectFile)
	}

	// Fall back to plugin's workflow/ directory
	if err != nil || len(raw) == 0 {
		stateFile := filepath.Join(pluginRoot, "workflow", filename)
		raw, err = os.ReadFile(stateFile)
		if err != nil {
			return fmt.Sprintf("PHASE: %s", phase)
		}
	}

	content := strings.NewReplacer(
		"{{WF_CLIENT}}", wfc,
		"{{PLUGIN_ROOT}}", pluginRoot,
		"{{AGENT_FILE}}", agentFile,
	).Replace(string(raw))

	return preamble + content
}

func resolveWorkflowID(sessionID, cwd string) string {
	return session.ResolveWorkflowIDByCWD(sessionID, cwd)
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
	if input.AgentID != "" {
		d["agent_id"] = input.AgentID
	}
	if input.AgentType != "" {
		d["agent_type"] = input.AgentType
	}
	if input.Source != "" {
		d["source"] = input.Source
	}
	if input.Model != "" {
		d["model"] = input.Model
	}
	if input.TeammateName != "" {
		d["teammate_name"] = input.TeammateName
	}
	if input.TeamName != "" {
		d["team_name"] = input.TeamName
	}
	return d
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
	return internaltemporal.Host()
}

// idleCheckContext implements config.CheckContext for teammate idle evaluation.
type idleCheckContext struct {
	commandsRan map[string]bool
}

func (c *idleCheckContext) Evidence() map[string]string  { return nil }
func (c *idleCheckContext) ActiveAgentCount() int        { return 0 }
func (c *idleCheckContext) Iteration() int               { return 0 }
func (c *idleCheckContext) MaxIterations() int           { return 0 }
func (c *idleCheckContext) OriginPhase() string          { return "" }
func (c *idleCheckContext) CommandsRan() map[string]bool { return c.commandsRan }

// queryAgentCommands fetches the command tracking state for a specific agent via Temporal query.
func queryAgentCommands(ctx context.Context, c client.Client, workflowID, agentName string) map[string]bool {
	resp, err := c.QueryWorkflow(ctx, workflowID, "", wf.QueryCommandsRan, agentName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot query commands-ran for %s: %v\n", agentName, err)
		return nil
	}
	var result map[string]bool
	if err := resp.Get(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot decode commands-ran: %v\n", err)
		return nil
	}
	return result
}

// resolveAgentName returns the agent name to use for command tracking.
// TeammateName is preferred; AgentType is the fallback (populated in PreToolUse when
// TeammateName is empty, e.g. "developer-1").
func resolveAgentName(input claudeHookInput) string {
	if input.TeammateName != "" {
		return input.TeammateName
	}
	return input.AgentType
}

// workflowSignaler is a minimal interface for sending signals to a workflow.
// It is satisfied by client.Client and allows test mocks without implementing
// the full Temporal client interface.
type workflowSignaler interface {
	SignalWorkflow(ctx context.Context, workflowID string, runID string, signalName string, arg interface{}) error
}

// trackPreToolUse handles per-agent command tracking signals for PreToolUse events.
// For file-change tools (Edit/Write/NotebookEdit), it sends InvalidateCommands for categories
// with invalidate_on_file_change=true. For Bash tools, it matches the command against tracking
// patterns and sends CommandRan signals for each matched category.
func trackPreToolUse(ctx context.Context, c workflowSignaler, workflowID string, input claudeHookInput) {
	agentName := input.TeammateName
	if agentName == "" {
		agentName = input.AgentType
	}
	if agentName == "" {
		return
	}

	cfg, err := config.LoadConfig(input.CWD)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config for tracking: %v\n", err)
		return
	}

	// File-change tool: invalidate categories that have invalidate_on_file_change=true
	// and record _file_changed so idle checks know the agent has started making changes.
	if config.IsFileChangeTool(input.ToolName) {
		var toInvalidate []string
		for catName, cat := range cfg.Tracking {
			if cat.ShouldInvalidateOnFileChange() {
				toInvalidate = append(toInvalidate, catName)
			}
		}
		if len(toInvalidate) > 0 {
			err := c.SignalWorkflow(ctx, workflowID, "", wf.SignalInvalidateCommands, model.SignalInvalidateCommands{
				SessionID:  input.SessionID,
				AgentName:  agentName,
				Categories: toInvalidate,
				Tool:       input.ToolName,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to send invalidate-commands signal: %v\n", err)
			}
		}
		// Record that this agent has made at least one file change.
		_ = c.SignalWorkflow(ctx, workflowID, "", wf.SignalCommandRan, model.SignalCommandRan{
			SessionID: input.SessionID,
			AgentName: agentName,
			Category:  "_file_changed",
			Command:   input.ToolName,
		})
		return
	}

	// SendMessage tool: record that this agent sent a message to the team.
	if input.ToolName == "SendMessage" {
		_ = c.SignalWorkflow(ctx, workflowID, "", wf.SignalCommandRan, model.SignalCommandRan{
			SessionID: input.SessionID,
			AgentName: agentName,
			Category:  "_sent_message",
			Command:   "SendMessage",
		})
		return
	}

	// Bash tool: match command segments against tracking patterns
	if input.ToolName != "Bash" {
		return
	}
	var bashInput struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input.ToolInput, &bashInput); err != nil || bashInput.Command == "" {
		return
	}

	segments := wf.SplitBashCommandsExported(bashInput.Command)
	for catName, cat := range cfg.Tracking {
		for _, seg := range segments {
			seg = strings.TrimSpace(seg)
			if matchesAnyPattern(seg, cat.Patterns) {
				err := c.SignalWorkflow(ctx, workflowID, "", wf.SignalCommandRan, model.SignalCommandRan{
					SessionID: input.SessionID,
					AgentName: agentName,
					Category:  catName,
					Command:   seg,
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to send command-ran signal: %v\n", err)
				}
				break // one signal per category per tool call is enough
			}
		}
	}
}

// matchesAnyPattern returns true if cmd starts with any of the given patterns at a word boundary.
// It also tries matching the basename of the first token to handle path-prefixed commands
// like "/usr/local/bin/golangci-lint run ./..." matching pattern "golangci-lint".
func matchesAnyPattern(cmd string, patterns []string) bool {
	for _, p := range patterns {
		if matchesBashPatternPrefix(cmd, p) {
			return true
		}
	}
	// Try with the basename of the executable (first token may be an absolute path).
	if strings.HasPrefix(cmd, "/") {
		// Extract first space-delimited token and replace with its basename.
		firstSpace := strings.IndexByte(cmd, ' ')
		var exe, rest string
		if firstSpace == -1 {
			exe = cmd
			rest = ""
		} else {
			exe = cmd[:firstSpace]
			rest = cmd[firstSpace:] // includes the leading space
		}
		base := filepath.Base(exe)
		normalized := base + rest
		for _, p := range patterns {
			if matchesBashPatternPrefix(normalized, p) {
				return true
			}
		}
	}
	return false
}

// matchesBashPatternPrefix checks if cmd starts with pattern at a word boundary.
func matchesBashPatternPrefix(cmd, pattern string) bool {
	if !strings.HasPrefix(cmd, pattern) {
		return false
	}
	if len(cmd) == len(pattern) {
		return true
	}
	c := cmd[len(pattern)]
	return c == ' ' || c == '\t' || c == '|' || c == ';' || c == '&' || c == '\n'
}

// evalTeammateIdleConfig loads the project config (with optional .wf-agents/workflow.yaml override),
// finds the idle rule matching the current phase, and evaluates its checks.
// Returns a non-empty denial reason if the teammate should not idle, or "" if idle is allowed.
func evalTeammateIdleConfig(projectDir, phase, teammateName string, commandsRan map[string]bool) string {
	cfg, err := config.LoadConfig(projectDir)
	if err != nil {
		// Config load failure: log but allow idle to avoid blocking teammates unexpectedly.
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		return ""
	}
	rule := config.FindIdleRule(cfg, phase, teammateName)
	if rule == nil {
		return ""
	}
	ctx := &idleCheckContext{commandsRan: commandsRan}
	return config.EvalChecks(rule.Checks, ctx)
}

// evalLeadStopConfig loads the project config and checks whether the Team Lead is allowed
// to stop/idle in the given phase. Returns a non-empty denial message if stopping is denied.
func evalLeadStopConfig(projectDir, phase string) string {
	cfg, err := config.LoadConfig(projectDir)
	if err != nil {
		// Config load failure: allow stop to avoid blocking the lead unexpectedly.
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		return ""
	}
	rule := config.FindLeadIdleRule(cfg, phase)
	if rule == nil || !rule.Deny {
		return ""
	}
	if rule.Message != "" {
		return rule.Message
	}
	return fmt.Sprintf("Lead cannot stop in %s — transition to BLOCKED first", phase)
}
