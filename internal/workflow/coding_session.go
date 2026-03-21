package workflow

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eklemin/wf-agents/internal/model"
	"go.temporal.io/sdk/workflow"
)

// Signal and query name constants used by CodingSessionWorkflow.
const (
	SignalTransition         = "transition"
	SignalHookEvent          = "hook-event"
	SignalJournal            = "journal"
	SignalComplete           = "complete"
	SignalResetIterations    = "reset-iterations"
	SignalClearActiveAgents  = "clear-active-agents"
	SignalCommandRan         = "command-ran"
	SignalInvalidateCommands = "invalidate-commands"
	SignalAgentShutDown      = "agent-shut-down"

	QueryStatus         = "status"
	QueryTimeline       = "timeline"
	QueryTimelineRecent = "timeline-recent"
	QueryPhase          = "phase"
	QueryCommandsRan    = "query-commands-ran"
	QueryWorkflowConfig = "workflow-config"

	UpdateTransition = "request-transition"
	SignalSetTask    = "set-task"
)

// CodingSessionWorkflow is a long-running workflow that acts as an event store
// and observer for a Claude Code coding session. It does NOT launch Claude Code —
// instead it receives signals from Claude Code hooks and tracks state.
func CodingSessionWorkflow(ctx workflow.Context, input model.WorkflowInput) (model.WorkflowTimeline, error) {
	logger := workflow.GetLogger(ctx)

	// Initialize terminal phases — prefer flow snapshot, fall back to guardConfig.
	if input.Flow != nil && len(input.Flow.Stop) > 0 {
		model.SetTerminalPhases(input.Flow.Stop)
	} else if stops := guardConfig.StopPhases(); len(stops) > 0 {
		model.SetTerminalPhases(stops)
	}

	// Determine initial phase — prefer flow snapshot, fall back to guardConfig.
	initialPhase := model.PhasePlanning
	if input.Flow != nil && input.Flow.Start != "" {
		initialPhase = model.Phase(input.Flow.Start)
	} else if start := guardConfig.StartPhase(); start != "" {
		initialPhase = model.Phase(start)
	}

	state := &sessionState{
		phase:           initialPhase,
		iteration:       0,
		totalIterations: 0,
		events:          make([]model.WorkflowEvent, 0, 100),
		activeAgents:    make(map[string]string),
		commandsRan:     make(map[string]map[string]bool),
		startedAt:       workflow.Now(ctx),
		lastUpdated:     workflow.Now(ctx),
		phaseEnteredAt:  workflow.Now(ctx),
		task:            input.TaskDescription,
		maxIter:         input.MaxIterations,
		flow:            input.Flow,
	}
	if state.maxIter == 0 {
		state.maxIter = 5
	}
	state.doneCh = workflow.NewChannel(ctx)

	state.addEvent(ctx, model.EventTransition, input.SessionID, map[string]string{
		"to":     string(initialPhase),
		"reason": "workflow started",
	})

	// Register queries
	if err := workflow.SetQueryHandler(ctx, QueryStatus, func() (model.WorkflowStatus, error) {
		return state.status(workflow.Now(ctx)), nil
	}); err != nil {
		return model.WorkflowTimeline{}, fmt.Errorf("set status query: %w", err)
	}

	if err := workflow.SetQueryHandler(ctx, QueryTimeline, func() (model.WorkflowTimeline, error) {
		return model.WorkflowTimeline{Events: state.events}, nil
	}); err != nil {
		return model.WorkflowTimeline{}, fmt.Errorf("set timeline query: %w", err)
	}

	// QueryTimelineRecent returns the last N events (default 500) to avoid exceeding
	// Temporal's query result size limit (2MB) for long-running sessions.
	if err := workflow.SetQueryHandler(ctx, QueryTimelineRecent, func(limit int) (model.WorkflowTimeline, error) {
		if limit <= 0 {
			limit = 500
		}
		events := state.events
		if len(events) > limit {
			events = events[len(events)-limit:]
		}
		return model.WorkflowTimeline{Events: events, TotalEvents: len(state.events)}, nil
	}); err != nil {
		return model.WorkflowTimeline{}, fmt.Errorf("set timeline-recent query: %w", err)
	}

	if err := workflow.SetQueryHandler(ctx, QueryPhase, func() (model.Phase, error) {
		return state.phase, nil
	}); err != nil {
		return model.WorkflowTimeline{}, fmt.Errorf("set phase query: %w", err)
	}

	if err := workflow.SetQueryHandler(ctx, QueryCommandsRan, func(agentName string) (map[string]bool, error) {
		if state.commandsRan == nil {
			return nil, nil
		}
		return state.commandsRan[agentName], nil
	}); err != nil {
		return model.WorkflowTimeline{}, fmt.Errorf("set commands-ran query: %w", err)
	}

	if err := workflow.SetQueryHandler(ctx, QueryWorkflowConfig, func() (*model.FlowSnapshot, error) {
		return state.flow, nil
	}); err != nil {
		return model.WorkflowTimeline{}, fmt.Errorf("set workflow-config query: %w", err)
	}

	// Register update handler for synchronous transition requests (allow/deny)
	if err := workflow.SetUpdateHandler(ctx, UpdateTransition, func(ctx workflow.Context, req model.SignalTransition) (model.TransitionResult, error) {
		return state.handleTransition(ctx, req), nil
	}); err != nil {
		return model.WorkflowTimeline{}, fmt.Errorf("set update handler: %w", err)
	}

	// Signal channels (hook events and journal only — transitions go through Update handler)
	hookEventCh := workflow.GetSignalChannel(ctx, SignalHookEvent)
	journalCh := workflow.GetSignalChannel(ctx, SignalJournal)
	setTaskCh := workflow.GetSignalChannel(ctx, SignalSetTask)
	resetIterationsCh := workflow.GetSignalChannel(ctx, SignalResetIterations)
	clearActiveAgentsCh := workflow.GetSignalChannel(ctx, SignalClearActiveAgents)
	agentShutDownCh := workflow.GetSignalChannel(ctx, SignalAgentShutDown)
	commandRanCh := workflow.GetSignalChannel(ctx, SignalCommandRan)
	invalidateCommandsCh := workflow.GetSignalChannel(ctx, SignalInvalidateCommands)

	// Drain legacy signal channels to prevent workflow stuck on unprocessed signals
	legacyTransitionCh := workflow.GetSignalChannel(ctx, SignalTransition)
	legacyCompleteCh := workflow.GetSignalChannel(ctx, SignalComplete)

	// Main event loop
	for !state.phase.IsTerminal() {
		sel := workflow.NewSelector(ctx)

		sel.AddReceive(hookEventCh, func(ch workflow.ReceiveChannel, more bool) {
			var evt model.SignalHookEvent
			ch.Receive(ctx, &evt)
			state.handleHookEvent(ctx, evt)
			logger.Info("Hook event recorded",
				"hook_type", evt.HookType,
				"session", evt.SessionID,
				"tool", evt.Tool,
			)
		})

		sel.AddReceive(journalCh, func(ch workflow.ReceiveChannel, more bool) {
			var j model.SignalJournal
			ch.Receive(ctx, &j)
			state.addEvent(ctx, model.EventJournal, j.SessionID, map[string]string{
				"message": j.Message,
			})
		})

		sel.AddReceive(setTaskCh, func(ch workflow.ReceiveChannel, more bool) {
			var task string
			ch.Receive(ctx, &task)
			if task != "" && state.task == "" {
				state.task = task
				_ = workflow.UpsertMemo(ctx, map[string]interface{}{
					"task": task,
				})
				logger.Info("Task set", "task", task)
			}
		})

		sel.AddReceive(resetIterationsCh, func(ch workflow.ReceiveChannel, more bool) {
			var sessionID string
			ch.Receive(ctx, &sessionID)
			old := state.iteration
			state.iteration = 1
			state.addEvent(ctx, model.EventJournal, sessionID, map[string]string{
				"message": fmt.Sprintf("iteration counter reset from %d to 1 (totalIterations=%d)", old, state.totalIterations),
			})
			logger.Info("Iteration counter reset", "old_iteration", old, "total_iterations", state.totalIterations)
		})

		sel.AddReceive(clearActiveAgentsCh, func(ch workflow.ReceiveChannel, more bool) {
			var sessionID string
			ch.Receive(ctx, &sessionID)
			count := len(state.activeAgents)
			state.activeAgents = make(map[string]string)
			state.commandsRan = make(map[string]map[string]bool)
			state.addEvent(ctx, model.EventJournal, sessionID, map[string]string{
				"message": fmt.Sprintf("cleared %d active agent(s) via deregister-all-agents", count),
			})
			logger.Info("Active agents cleared", "count", count, "session", sessionID)
		})

		sel.AddReceive(agentShutDownCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig struct{ AgentName string }
			ch.Receive(ctx, &sig)
			if sig.AgentName == "" {
				return
			}
			if _, ok := state.activeAgents[sig.AgentName]; ok {
				delete(state.activeAgents, sig.AgentName)
				delete(state.commandsRan, sig.AgentName)
				state.addEvent(ctx, model.EventJournal, "", map[string]string{
					"message": fmt.Sprintf("agent shut down: %s", sig.AgentName),
				})
				logger.Info("Agent shut down", "agent", sig.AgentName)
			}
		})

		sel.AddReceive(commandRanCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig model.SignalCommandRan
			ch.Receive(ctx, &sig)
			if sig.AgentName == "" || sig.Category == "" {
				return
			}
			if state.commandsRan == nil {
				state.commandsRan = make(map[string]map[string]bool)
			}
			if state.commandsRan[sig.AgentName] == nil {
				state.commandsRan[sig.AgentName] = make(map[string]bool)
			}
			state.commandsRan[sig.AgentName][sig.Category] = true
			logger.Info("Command ran recorded", "agent", sig.AgentName, "category", sig.Category, "command", sig.Command)
		})

		sel.AddReceive(invalidateCommandsCh, func(ch workflow.ReceiveChannel, more bool) {
			var sig model.SignalInvalidateCommands
			ch.Receive(ctx, &sig)
			if sig.AgentName == "" || state.commandsRan == nil {
				return
			}
			agentCmds := state.commandsRan[sig.AgentName]
			if agentCmds == nil {
				return
			}
			for _, cat := range sig.Categories {
				delete(agentCmds, cat)
			}
			logger.Info("Commands invalidated", "agent", sig.AgentName, "categories", sig.Categories, "tool", sig.Tool)
		})

		// doneCh unblocks the selector when a terminal phase is reached via Update handler
		sel.AddReceive(state.doneCh, func(ch workflow.ReceiveChannel, more bool) {
			var v bool
			ch.Receive(ctx, &v)
			// Loop condition will now re-evaluate and exit
		})

		// Drain legacy signals (ignore them — transitions must use UpdateWorkflow)
		sel.AddReceive(legacyTransitionCh, func(ch workflow.ReceiveChannel, more bool) {
			var req model.SignalTransition
			ch.Receive(ctx, &req)
			logger.Warn("Ignoring legacy transition signal — use UpdateWorkflow instead",
				"to", req.To,
			)
		})

		sel.AddReceive(legacyCompleteCh, func(ch workflow.ReceiveChannel, more bool) {
			var sessionID string
			ch.Receive(ctx, &sessionID)
			logger.Warn("Ignoring legacy complete signal — use UpdateWorkflow with --to COMPLETE instead",
				"session", sessionID,
			)
		})

		sel.Select(ctx)
	}

	logger.Info("Workflow completed", "phase", state.phase, "events", len(state.events))
	return model.WorkflowTimeline{Events: state.events}, nil
}

// sessionState holds the internal mutable state of the workflow.
type sessionState struct {
	phase               model.Phase
	preBlockedPhase     model.Phase // remembers state before BLOCKED, for returning
	blockedByPermission bool        // true when BLOCKED was triggered by a PermissionRequest
	iteration           int         // resettable counter for guard checks
	totalIterations     int         // cumulative counter, never reset
	events              []model.WorkflowEvent
	activeAgents        map[string]string          // agent_type → agent_id; entries added on SubagentStart, cleared by clear-active-agents or COMPLETE
	commandsRan         map[string]map[string]bool // agent → category → ran
	startedAt           time.Time
	lastUpdated         time.Time
	phaseEnteredAt      time.Time
	task                string
	maxIter             int
	doneCh              workflow.Channel    // internal channel to unblock selector when terminal state is reached
	flow                *model.FlowSnapshot // snapshotted flow topology from session start
}

func (s *sessionState) addEvent(ctx workflow.Context, evtType model.EventType, sessionID string, detail map[string]string) {
	s.events = append(s.events, model.WorkflowEvent{
		Type:      evtType,
		Timestamp: workflow.Now(ctx),
		SessionID: sessionID,
		Detail:    detail,
	})
	s.lastUpdated = workflow.Now(ctx)
}

func (s *sessionState) handleTransition(ctx workflow.Context, req model.SignalTransition) model.TransitionResult {
	result := model.TransitionResult{
		From: s.phase,
		To:   req.To,
	}

	// Terminal state check
	if s.phase.IsTerminal() {
		result.Allowed = false
		result.Reason = fmt.Sprintf("workflow already in terminal state %s", s.phase)
		s.addEvent(ctx, model.EventHookDenial, req.SessionID, map[string]string{
			"from":   string(s.phase),
			"to":     string(req.To),
			"reason": result.Reason,
		})
		return result
	}

	// Validate transition + guards via unified transition table
	if reason := validateTransition(s, s.phase, req.To, req.Guards); reason != "" {
		result.Allowed = false
		result.Reason = reason
		s.addEvent(ctx, model.EventHookDenial, req.SessionID, map[string]string{
			"from":   string(s.phase),
			"to":     string(req.To),
			"reason": result.Reason,
		})
		return result
	}

	// Entering BLOCKED — remember where we came from
	fromPhase := s.phase
	if req.To == model.PhaseBlocked {
		s.preBlockedPhase = s.phase
	}

	// Apply transition
	s.phase = req.To

	// Execute on_enter side effects for the target phase.
	// Skip when unblocking from BLOCKED — the phase was already "entered" before BLOCKED.
	// Prefer flow snapshot; fall back to guardConfig for workflows started without a snapshot.
	if fromPhase != model.PhaseBlocked {
		if s.flow != nil {
			if fp, ok := s.flow.Phases[string(req.To)]; ok {
				for _, effect := range fp.OnEnter {
					if effect.Type == "increment_iteration" {
						s.iteration++
						s.totalIterations++
					}
				}
			}
		} else if guardConfig != nil && guardConfig.Phases != nil {
			if phaseConfig, ok := guardConfig.Phases.Phases[string(req.To)]; ok {
				for _, effect := range phaseConfig.OnEnter {
					if effect.Type == "increment_iteration" {
						s.iteration++
						s.totalIterations++
					}
				}
			}
		}
	}
	s.lastUpdated = workflow.Now(ctx)
	s.phaseEnteredAt = workflow.Now(ctx)
	result.Allowed = true

	// Clear command tracking on phase transitions — fresh phase = fresh state.
	// Skip when unblocking from BLOCKED — agent state was already set before BLOCKED.
	if fromPhase != model.PhaseBlocked {
		s.commandsRan = make(map[string]map[string]bool)
	}

	s.addEvent(ctx, model.EventTransition, req.SessionID, map[string]string{
		"from":             string(result.From),
		"to":               string(req.To),
		"reason":           req.Reason,
		"iteration":        fmt.Sprintf("%d", s.iteration),
		"total_iterations": fmt.Sprintf("%d", s.totalIterations),
	})

	if req.To.IsTerminal() {
		s.activeAgents = make(map[string]string)
		s.doneCh.Send(ctx, true)
	}

	return result
}

func (s *sessionState) handleHookEvent(ctx workflow.Context, evt model.SignalHookEvent) {
	evtType := model.EventToolUse
	switch evt.HookType {
	case "SubagentStart":
		evtType = model.EventAgentSpawn
		agentType := evt.Detail["agent_type"]
		agentID := evt.Detail["agent_id"]
		if agentType != "" {
			// Overwrite: a new spawn of the same agent_type replaces the old id.
			// This ensures that when a stale SubagentStop (old agent_id) arrives later,
			// it will not match the current id and will be ignored.
			s.activeAgents[agentType] = agentID
		}
	case "SubagentStop":
		evtType = model.EventAgentStop
		agentType := evt.Detail["agent_type"]
		agentID := evt.Detail["agent_id"]
		if agentType != "" && isNamedTeammate(agentType) {
			// Named teammates (developer-N, reviewer-N) persist across Agent tool invocations —
			// SubagentStop fires per Agent tool call but the teammate process is still alive.
			// They are removed only by clear-active-agents (RESPAWN) or COMPLETE.
		} else if agentType != "" {
			// One-shot/unnamed agents (e.g. Explore) are genuinely done when they stop.
			// Remove only if agent_id matches to guard against stale stops.
			if stored, ok := s.activeAgents[agentType]; ok && stored == agentID {
				delete(s.activeAgents, agentType)
			}
		}
	case "Stop":
		// Log only — a Stop event from Claude Code does not remove agents from the active map.
		// SubagentStop with the correct agent_id is the authoritative removal signal.
		evtType = model.EventAgentStop
	}

	// Auto-BLOCKED: PermissionRequest from any agent → terminal waiting for user approval
	if evt.HookType == "PermissionRequest" {
		if !s.phase.IsTerminal() && s.phase != model.PhaseBlocked {
			s.preBlockedPhase = s.phase
			s.phase = model.PhaseBlocked
			s.blockedByPermission = true
			s.lastUpdated = workflow.Now(ctx)
			s.phaseEnteredAt = workflow.Now(ctx)

			agent := evt.Detail["agent_type"]
			if agent == "" {
				agent = "lead"
			}
			tool := evt.Tool
			if tool == "" {
				tool = evt.Detail["tool_name"]
			}
			s.addEvent(ctx, model.EventTransition, evt.SessionID, map[string]string{
				"from":   string(s.preBlockedPhase),
				"to":     string(model.PhaseBlocked),
				"reason": fmt.Sprintf("auto: %s needs permission for %s", agent, tool),
			})
		}
	}

	// Auto-unblock: PostToolUse / PostToolUseFailure — only if BLOCKED was caused by PermissionRequest.
	// This prevents PostToolUse from normal tool calls from spuriously unblocking a manually-set BLOCKED.
	if s.phase == model.PhaseBlocked && s.preBlockedPhase != "" && s.blockedByPermission {
		if evt.HookType == "PostToolUse" || evt.HookType == "PostToolUseFailure" {
			from := s.phase
			s.phase = s.preBlockedPhase
			s.blockedByPermission = false
			s.lastUpdated = workflow.Now(ctx)
			s.phaseEnteredAt = workflow.Now(ctx)
			s.addEvent(ctx, model.EventTransition, evt.SessionID, map[string]string{
				"from":   string(from),
				"to":     string(s.preBlockedPhase),
				"reason": "auto: permission resolved",
			})
		}
	}

	detail := make(map[string]string)
	detail["hook_type"] = evt.HookType
	if evt.Tool != "" {
		detail["tool"] = evt.Tool
	}
	for k, v := range evt.Detail {
		detail[k] = v
	}
	s.addEvent(ctx, evtType, evt.SessionID, detail)
}

// isNamedTeammate returns true if the agent_type represents a persistent named teammate
// (e.g. "developer-1", "reviewer-2"). Named teammates persist across Agent tool invocations
// and are only removed via clear-active-agents or COMPLETE. One-shot agents (e.g. "Explore")
// are removed normally on SubagentStop.
func isNamedTeammate(agentType string) bool {
	return strings.HasPrefix(agentType, "developer-") || strings.HasPrefix(agentType, "reviewer-")
}

func (s *sessionState) status(now time.Time) model.WorkflowStatus {
	var commandsRan map[string]map[string]bool
	if len(s.commandsRan) > 0 {
		commandsRan = s.commandsRan
	}
	activeAgents := make([]string, 0, len(s.activeAgents))
	for agentType := range s.activeAgents {
		activeAgents = append(activeAgents, agentType)
	}
	sort.Strings(activeAgents)
	return model.WorkflowStatus{
		Phase:                s.phase,
		Iteration:            s.iteration,
		TotalIterations:      s.totalIterations,
		ActiveAgents:         activeAgents,
		EventCount:           len(s.events),
		StartedAt:            s.startedAt.Format(time.RFC3339),
		LastUpdatedAt:        s.lastUpdated.Format(time.RFC3339),
		Task:                 s.task,
		PreBlockedPhase:      s.preBlockedPhase,
		CurrentPhaseSecs:     now.Sub(s.phaseEnteredAt).Seconds(),
		PhaseDurationSecs:    s.computePhaseDurations(now),
		CurrentIterPhaseSecs: s.computeCurrentIterPhaseDurations(now),
		CommandsRan:          commandsRan,
	}
}

// computeCurrentIterPhaseDurations computes per-phase seconds spent only in the
// current iteration — i.e., since the last RESPAWN event (or from the beginning
// if this is iteration 1 / no RESPAWN has occurred yet).
func (s *sessionState) computeCurrentIterPhaseDurations(now time.Time) map[string]float64 {
	// Find index of the last RESPAWN transition that started a new iteration.
	// The very first RESPAWN (from PLANNING) is included; subsequent RESPAWNs
	// mark the boundary of a new iteration. We want to start accumulating from
	// the most recent RESPAWN that is NOT the very first one (iter > 1), OR
	// from the beginning when still in iteration 1.
	lastRespawnIdx := -1
	iterCount := 0
	for i, ev := range s.events {
		if ev.Type != model.EventTransition {
			continue
		}
		to, ok := ev.Detail["to"]
		if !ok || to != string(model.PhaseRespawn) {
			continue
		}
		// Skip BLOCKED→RESPAWN transitions — those are unblocks, not new iteration boundaries.
		if ev.Detail["from"] == string(model.PhaseBlocked) {
			continue
		}
		iterCount++
		lastRespawnIdx = i
	}

	// If we're in iteration 1 (at most one RESPAWN seen), start from beginning.
	if iterCount <= 1 {
		lastRespawnIdx = -1
	}

	// Accumulate durations from the last RESPAWN boundary (inclusive) onward.
	durations := make(map[string]float64)
	var currentPhase string
	var phaseStart time.Time

	startFrom := lastRespawnIdx // we begin at the RESPAWN event itself (inclusive)
	if startFrom < 0 {
		startFrom = -1 // all events
	}

	for i, ev := range s.events {
		if i <= startFrom && startFrom >= 0 && i != startFrom {
			continue
		}
		if ev.Type != model.EventTransition {
			continue
		}
		to, hasTo := ev.Detail["to"]
		if !hasTo {
			continue
		}
		// Close the previous phase
		if currentPhase != "" && !phaseStart.IsZero() {
			durations[currentPhase] += ev.Timestamp.Sub(phaseStart).Seconds()
		}
		currentPhase = to
		phaseStart = ev.Timestamp
	}

	// Close the current (open) phase using now
	if currentPhase != "" && !phaseStart.IsZero() {
		durations[currentPhase] += now.Sub(phaseStart).Seconds()
	}

	return durations
}

// computePhaseDurations iterates through transition events to compute cumulative
// seconds spent in each phase. The current phase is open-ended at now.
func (s *sessionState) computePhaseDurations(now time.Time) map[string]float64 {
	durations := make(map[string]float64)
	var currentPhase string
	var phaseStart time.Time

	for _, ev := range s.events {
		if ev.Type != model.EventTransition {
			continue
		}
		to, hasTo := ev.Detail["to"]
		if !hasTo {
			continue
		}
		// Close the previous phase
		if currentPhase != "" && !phaseStart.IsZero() {
			durations[currentPhase] += ev.Timestamp.Sub(phaseStart).Seconds()
		}
		currentPhase = to
		phaseStart = ev.Timestamp
	}

	// Close the current (open) phase using now
	if currentPhase != "" && !phaseStart.IsZero() {
		durations[currentPhase] += now.Sub(phaseStart).Seconds()
	}

	return durations
}
