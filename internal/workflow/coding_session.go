package workflow

import (
	"fmt"
	"time"

	"github.com/eklemin/wf-agents/internal/model"
	"go.temporal.io/sdk/workflow"
)

const (
	SignalTransition = "transition"
	SignalHookEvent  = "hook-event"
	SignalJournal    = "journal"
	SignalComplete   = "complete"

	QueryStatus   = "status"
	QueryTimeline = "timeline"
	QueryPhase    = "phase"

	UpdateTransition = "request-transition"
	SignalSetTask    = "set-task"
)

// CodingSessionWorkflow is a long-running workflow that acts as an event store
// and observer for a Claude Code coding session. It does NOT launch Claude Code —
// instead it receives signals from Claude Code hooks and tracks state.
func CodingSessionWorkflow(ctx workflow.Context, input model.WorkflowInput) (model.WorkflowTimeline, error) {
	logger := workflow.GetLogger(ctx)

	state := &sessionState{
		phase:        model.PhasePlanning,
		iteration:    1,
		events:       make([]model.WorkflowEvent, 0, 100),
		activeAgents: make([]string, 0),
		startedAt:    workflow.Now(ctx),
		lastUpdated:  workflow.Now(ctx),
		task:         input.TaskDescription,
		maxIter:      input.MaxIterations,
	}
	if state.maxIter == 0 {
		state.maxIter = 5
	}

	state.addEvent(ctx, model.EventTransition, input.SessionID, map[string]string{
		"to":     string(model.PhasePlanning),
		"reason": "workflow started",
	})

	// Register queries
	if err := workflow.SetQueryHandler(ctx, QueryStatus, func() (model.WorkflowStatus, error) {
		return state.status(), nil
	}); err != nil {
		return model.WorkflowTimeline{}, fmt.Errorf("set status query: %w", err)
	}

	if err := workflow.SetQueryHandler(ctx, QueryTimeline, func() (model.WorkflowTimeline, error) {
		return model.WorkflowTimeline{Events: state.events}, nil
	}); err != nil {
		return model.WorkflowTimeline{}, fmt.Errorf("set timeline query: %w", err)
	}

	if err := workflow.SetQueryHandler(ctx, QueryPhase, func() (model.Phase, error) {
		return state.phase, nil
	}); err != nil {
		return model.WorkflowTimeline{}, fmt.Errorf("set phase query: %w", err)
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
	phase          model.Phase
	preBlockedPhase model.Phase // remembers state before BLOCKED, for returning
	iteration      int
	events         []model.WorkflowEvent
	activeAgents   []string
	startedAt      time.Time
	lastUpdated    time.Time
	task           string
	maxIter        int
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

	// BLOCKED → can only return to the phase that was active before entering BLOCKED
	if s.phase == model.PhaseBlocked {
		if s.preBlockedPhase == "" || req.To != s.preBlockedPhase {
			result.Allowed = false
			result.Reason = fmt.Sprintf("BLOCKED can only return to %s (the pre-blocked phase)", s.preBlockedPhase)
			s.addEvent(ctx, model.EventHookDenial, req.SessionID, map[string]string{
				"from":   string(s.phase),
				"to":     string(req.To),
				"reason": result.Reason,
			})
			return result
		}
	} else if !s.phase.CanTransitionTo(req.To) {
		result.Allowed = false
		result.Reason = fmt.Sprintf("transition %s → %s is not allowed", s.phase, req.To)
		s.addEvent(ctx, model.EventHookDenial, req.SessionID, map[string]string{
			"from":   string(s.phase),
			"to":     string(req.To),
			"reason": result.Reason,
		})
		return result
	}

	// Transition guards — validate evidence from client
	if reason := checkGuards(s.phase, req.To, req.Guards); reason != "" {
		result.Allowed = false
		result.Reason = reason
		s.addEvent(ctx, model.EventHookDenial, req.SessionID, map[string]string{
			"from":   string(s.phase),
			"to":     string(req.To),
			"reason": result.Reason,
		})
		return result
	}

	// RESPAWN → DEVELOPING: all old subagents must be stopped (context cleared)
	if s.phase == model.PhaseRespawn && req.To == model.PhaseDeveloping && len(s.activeAgents) > 0 {
		result.Allowed = false
		result.Reason = fmt.Sprintf("cannot leave RESPAWN with %d active subagent(s) — kill old agents before spawning new ones", len(s.activeAgents))
		s.addEvent(ctx, model.EventHookDenial, req.SessionID, map[string]string{
			"from":   string(s.phase),
			"to":     string(req.To),
			"reason": result.Reason,
		})
		return result
	}

	// Entering BLOCKED — remember where we came from
	if req.To == model.PhaseBlocked {
		s.preBlockedPhase = s.phase
	}

	// Track iteration on RESPAWN (iteration boundary), except first entry from PLANNING
	if req.To == model.PhaseRespawn && s.phase != model.PhasePlanning {
		s.iteration++
		if s.iteration > s.maxIter {
			s.iteration-- // rollback
			result.Allowed = false
			result.Reason = fmt.Sprintf("max iterations (%d) exceeded — transition denied", s.maxIter)
			s.addEvent(ctx, model.EventHookDenial, req.SessionID, map[string]string{
				"from":   string(s.phase),
				"to":     string(req.To),
				"reason": result.Reason,
			})
			return result
		}
	}

	// Apply transition
	s.phase = req.To
	s.lastUpdated = workflow.Now(ctx)
	result.Allowed = true

	s.addEvent(ctx, model.EventTransition, req.SessionID, map[string]string{
		"from":      string(result.From),
		"to":        string(req.To),
		"reason":    req.Reason,
		"iteration": fmt.Sprintf("%d", s.iteration),
	})

	return result
}

func (s *sessionState) handleHookEvent(ctx workflow.Context, evt model.SignalHookEvent) {
	evtType := model.EventToolUse
	switch evt.HookType {
	case "SubagentStart":
		evtType = model.EventAgentSpawn
		if agentID, ok := evt.Detail["agent_id"]; ok {
			s.activeAgents = append(s.activeAgents, agentID)
		}
	case "SubagentStop", "Stop":
		evtType = model.EventAgentStop
		if agentID, ok := evt.Detail["agent_id"]; ok {
			filtered := s.activeAgents[:0]
			for _, a := range s.activeAgents {
				if a != agentID {
					filtered = append(filtered, a)
				}
			}
			s.activeAgents = filtered
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

func (s *sessionState) status() model.WorkflowStatus {
	return model.WorkflowStatus{
		Phase:           s.phase,
		Iteration:       s.iteration,
		ActiveAgents:    s.activeAgents,
		EventCount:      len(s.events),
		StartedAt:       s.startedAt.Format(time.RFC3339),
		LastUpdatedAt:   s.lastUpdated.Format(time.RFC3339),
		Task:            s.task,
		PreBlockedPhase: s.preBlockedPhase,
	}
}
