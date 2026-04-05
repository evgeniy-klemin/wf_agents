package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/eklemin/wf-agents/internal/config"
	"github.com/eklemin/wf-agents/internal/model"
	"github.com/eklemin/wf-agents/internal/noplog"
	"github.com/eklemin/wf-agents/internal/phasedocs"
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
		_, _ = f.Write(line)
		_, _ = f.Write([]byte("\n"))
		_ = f.Close()
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

	// Load project-level config overrides so guardConfig reflects preset + project permissions.
	if input.CWD != "" {
		if err := wf.InitGuardConfig(input.CWD); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load project guard config from %s: %v\n", input.CWD, err)
		}
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
		_, _ = f.Write(logLine)
		_, _ = f.Write([]byte("\n"))
		_ = f.Close()
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
	if input.SessionID == workflowSessionID {
		// Lead session: patch marker CWD if it was recorded as repo root but we're in a worktree.
		session.UpdateMarkerCWD(input.SessionID, input.CWD)
	}
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
		status, err := queryStatus(ctx, c, workflowID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			os.Exit(0)
		}
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

		// Guard: deny git push to main/master in all phases.
		if input.ToolName == "Bash" {
			var bashInput struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(input.ToolInput, &bashInput); err == nil {
				for _, seg := range wf.SplitBashCommandsExported(strings.TrimSpace(bashInput.Command)) {
					seg = strings.TrimSpace(seg)
					if strings.HasPrefix(seg, "git ") && isPushToProtectedBranch(seg, input.CWD) {
						const reason = "Direct push to main/master is not allowed. Create a feature branch first."
						detail["denied"] = "true"
						detail["reason"] = reason
						sendHookEvent(ctx, c, workflowID, model.SignalHookEvent{
							HookType:  "PreToolUse",
							SessionID: input.SessionID,
							Tool:      input.ToolName,
							Detail:    detail,
						})
						fmt.Fprintf(os.Stderr, "DENIED: %s\n", reason)
						_, _ = fmt.Fprintf(os.Stdout, "%s\n", reason)
						logResponse(input.SessionID, "PreToolUse", 2, map[string]string{
							"decision": "deny",
							"reason":   reason,
						})
						os.Exit(2)
					}
				}
			}
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
			_, _ = fmt.Fprintf(os.Stdout, "%s\n", decision.Reason)
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
			preamble, err := phasedocs.Preamble(phase, input.CWD, wf.IsTeammate(agentName))
			if err != nil {
				log.Printf("phasedocs.Preamble error: %v", err)
				os.Exit(1)
			}
			out := hookOutput{
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:            "PreToolUse",
					PermissionDecision:       "allow",
					PermissionDecisionReason: "Safe command auto-approved by workflow",
					AdditionalContext:        fmt.Sprintf("[Workflow Phase: %s] %s", phase, preamble),
				},
			}
			_ = json.NewEncoder(os.Stdout).Encode(out)
			logResponse(input.SessionID, "PreToolUse", 0, map[string]string{
				"decision": "allow",
				"phase":    string(phase),
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
		preamble, err := phasedocs.Preamble(phase, input.CWD, wf.IsTeammate(agentName))
		if err != nil {
			log.Printf("phasedocs.Preamble error: %v", err)
			os.Exit(1)
		}
		if preamble != "" {
			out := hookOutput{
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:     "PreToolUse",
					AdditionalContext: fmt.Sprintf("[Workflow Phase: %s] %s", phase, preamble),
				},
			}
			_ = json.NewEncoder(os.Stdout).Encode(out)
		}
		logResponse(input.SessionID, "PreToolUse", 0, map[string]string{
			"decision": "pass",
			"phase":    string(phase),
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
		status, err := queryStatus(ctx, c, workflowID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			break
		}
		phase := status.Phase
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
			if reason := evalTeammateIdleConfig(input.CWD, string(phase), agentName, commandsRan, status.MRUrl); reason != "" {
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
			phase, err := queryPhase(ctx, c, workflowID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
				break
			}
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
		// Sync preset agents to ~/.claude/agents/ if preset defines agents/
		syncPresetAgents(input.CWD)
		// Inject session context so Claude knows its workflow ID and wf-client path
		wfClientPath := wfClientBin()
		var startCritical string
		cfg, cfgErr := config.LoadConfig(input.CWD)
		if cfgErr != nil {
			startCritical = "workflow config not found (searched: project .wf-agents/workflow.yaml, preset if configured, plugin default)"
		} else if cfg.Phases == nil || cfg.Phases.Start == "" {
			startCritical = "phases.start not configured in workflow config"
		}
		if startCritical != "" {
			out := hookOutput{
				Continue: boolPtr(false),
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:     "SessionStart",
					AdditionalContext: fmt.Sprintf("CRITICAL: %s\nSession cannot start. Fix the plugin configuration and retry.", startCritical),
				},
			}
			_ = json.NewEncoder(os.Stdout).Encode(out)
			return
		}
		startingPhase := model.Phase(cfg.Phases.Start)
		startingInstructions, err := phasedocs.FullInstructions(startingPhase, input.CWD, false)
		if err != nil {
			out := hookOutput{
				Continue: boolPtr(false),
				HookSpecificOutput: &hookSpecificOutput{
					HookEventName:     "SessionStart",
					AdditionalContext: fmt.Sprintf("CRITICAL: %v\nSession cannot start without phase instructions. Fix the plugin configuration and retry.", err),
				},
			}
			_ = json.NewEncoder(os.Stdout).Encode(out)
			return
		}
		out := hookOutput{
			Continue: boolPtr(true),
			HookSpecificOutput: &hookSpecificOutput{
				HookEventName: "SessionStart",
				AdditionalContext: fmt.Sprintf(
					"WORKFLOW SESSION STARTED.\n"+
						"Session ID: %s\n"+
						"Workflow ID: %s\n"+
						"wf-client path: %s\n"+
						"Current phase: %s.\n"+
						"To transition phases: %s transition %s --to <PHASE> --reason \"<why>\"\n\n"+
						"%s",
					input.SessionID, workflowID, wfClientPath, startingPhase, wfClientPath, workflowID, startingInstructions),
			},
		}
		_ = json.NewEncoder(os.Stdout).Encode(out)
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
func queryPhase(ctx context.Context, c client.Client, workflowID string) (model.Phase, error) {
	resp, err := c.QueryWorkflow(ctx, workflowID, "", wf.QueryPhase)
	if err != nil {
		return "", fmt.Errorf("cannot query phase for %s: %w", workflowID, err)
	}
	var phase model.Phase
	if err := resp.Get(&phase); err != nil {
		return "", fmt.Errorf("cannot decode phase: %w", err)
	}
	return phase, nil
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
func queryStatus(ctx context.Context, c client.Client, workflowID string) (model.WorkflowStatus, error) {
	resp, err := c.QueryWorkflow(ctx, workflowID, "", wf.QueryStatus)
	if err != nil {
		return model.WorkflowStatus{}, fmt.Errorf("cannot query status for %s: %w", workflowID, err)
	}
	var status model.WorkflowStatus
	if err := resp.Get(&status); err != nil {
		return model.WorkflowStatus{}, fmt.Errorf("cannot decode status: %w", err)
	}
	return status, nil
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
	mrUrl       string
}

func (c *idleCheckContext) Evidence() map[string]string  { return nil }
func (c *idleCheckContext) ActiveAgentCount() int        { return 0 }
func (c *idleCheckContext) Iteration() int               { return 0 }
func (c *idleCheckContext) MaxIterations() int           { return 0 }
func (c *idleCheckContext) OriginPhase() string          { return "" }
func (c *idleCheckContext) CommandsRan() map[string]bool { return c.commandsRan }
func (c *idleCheckContext) MrUrl() string                { return c.mrUrl }

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
		err := c.SignalWorkflow(ctx, workflowID, "", wf.SignalInvalidateCommands, model.SignalInvalidateCommands{
			SessionID: input.SessionID,
			AgentName: agentName,
			Tool:      input.ToolName,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to send invalidate-commands signal: %v\n", err)
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
func evalTeammateIdleConfig(projectDir, phase, teammateName string, commandsRan map[string]bool, mrUrl string) string {
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
	ctx := &idleCheckContext{commandsRan: commandsRan, mrUrl: mrUrl}
	return config.EvalChecks(rule.Checks, ctx)
}

// syncPresetAgents copies agent definition files from the preset's agents/ directory
// into ~/.claude/agents/ so Claude Code can discover them. A marker file
// ~/.claude/agents/.wf-agents-preset records the active preset identifier to track
// ownership. Files owned by a different preset or user are not overwritten.
func syncPresetAgents(cwd string) {
	if cwd == "" {
		return
	}
	cfgData, err := os.ReadFile(filepath.Join(cwd, ".wf-agents", "workflow.yaml"))
	if err != nil {
		return
	}
	presetDir, err := config.ResolvePresetDirFromYAML(cfgData)
	if err != nil || presetDir == "" {
		return
	}

	agentsDir := filepath.Join(presetDir, "agents")
	if _, err := os.Stat(agentsDir); err != nil {
		return // preset has no agents/ directory
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: wf-agents: cannot determine home dir for agent sync: %v\n", err)
		return
	}
	destDir := filepath.Join(home, ".claude", "agents")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: wf-agents: cannot create agents dir %s: %v\n", destDir, err)
		return
	}

	markerPath := filepath.Join(destDir, ".wf-agents-preset")
	markerData, _ := os.ReadFile(markerPath)
	activePreset := strings.TrimSpace(string(markerData))
	presetID := filepath.ToSlash(presetDir)

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: wf-agents: cannot read preset agents dir: %v\n", err)
		return
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		destFile := filepath.Join(destDir, e.Name())
		// Don't overwrite files owned by a different preset or user
		if _, statErr := os.Stat(destFile); statErr == nil && activePreset != presetID {
			continue
		}
		content, err := os.ReadFile(filepath.Join(agentsDir, e.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: wf-agents: cannot read preset agent %s: %v\n", e.Name(), err)
			continue
		}
		if err := os.WriteFile(destFile, content, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: wf-agents: cannot write agent %s: %v\n", destFile, err)
		}
	}

	_ = os.WriteFile(markerPath, []byte(presetID), 0644)
}

// isPushToProtectedBranch returns true if the given bash command is pushing to main or master.
// It handles:
//   - "git push origin main" / "git push origin master"
//   - "git push <remote> main" / "git push <remote> master"
//   - "git push origin HEAD:main" (refspec notation)
//   - "git push origin refs/heads/main" (full ref notation)
//   - Plain "git push" when the current branch (resolved via git symbolic-ref in cwd) is main/master
func isPushToProtectedBranch(cmd, cwd string) bool {
	parts := strings.Fields(cmd)
	if len(parts) < 2 {
		return false
	}
	// Skip flags before subcommand
	idx := 1
	for idx < len(parts) && strings.HasPrefix(parts[idx], "-") {
		idx++
	}
	if idx >= len(parts) || parts[idx] != "push" {
		return false
	}
	// Collect non-flag arguments after "push"
	var args []string
	for i := idx + 1; i < len(parts); i++ {
		if !strings.HasPrefix(parts[i], "-") {
			args = append(args, parts[i])
		}
	}
	// "git push <remote> <branch>" — check if branch is main or master
	if len(args) >= 2 {
		branch := args[1]
		// Strip refspec notation (e.g. "HEAD:main" → "main")
		if colon := strings.LastIndex(branch, ":"); colon >= 0 {
			branch = branch[colon+1:]
		}
		// Strip full ref prefix (e.g. "refs/heads/main" → "main")
		branch = strings.TrimPrefix(branch, "refs/heads/")
		return branch == "main" || branch == "master"
	}
	// Plain "git push" or "git push <remote>" — check current branch
	if cwd == "" {
		return false
	}
	out, err := exec.Command("git", "-C", cwd, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return false
	}
	currentBranch := strings.TrimSpace(string(out))
	return currentBranch == "main" || currentBranch == "master"
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
