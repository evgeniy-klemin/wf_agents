package model

import "time"

// WorkflowInput is the input to start a CodingSessionWorkflow.
type WorkflowInput struct {
	SessionID       string        `json:"session_id"`
	TaskDescription string        `json:"task_description"`
	RepoPath        string        `json:"repo_path"`
	BranchName      string        `json:"branch_name,omitempty"`
	MaxIterations   int           `json:"max_iterations,omitempty"`
	Flow            *FlowSnapshot `json:"flow,omitempty"`
}

// FlowSnapshot captures the flow topology (phases + transitions) at session start.
// Permissions, guards, idle rules are NOT included — they come from the current config.
type FlowSnapshot struct {
	Start       string                      `json:"start"`
	Stop        []string                    `json:"stop"`
	PhaseOrder  []string                    `json:"phase_order"`
	Phases      map[string]FlowPhase        `json:"phases"`
	Transitions map[string][]FlowTransition `json:"transitions"`
}

// IsValidTransition checks whether the flow topology allows a transition from → to.
func (f *FlowSnapshot) IsValidTransition(from, to string) bool {
	if f == nil || f.Transitions == nil {
		return false
	}
	for _, t := range f.Transitions[from] {
		if t.To == to {
			return true
		}
	}
	return false
}

// AllowedTransitions returns all valid target phases for a given from phase.
func (f *FlowSnapshot) AllowedTransitions(from string) []string {
	if f == nil || f.Transitions == nil {
		return nil
	}
	var result []string
	for _, t := range f.Transitions[from] {
		result = append(result, t.To)
	}
	return result
}

// FlowPhase holds the flow-relevant properties of a single phase.
type FlowPhase struct {
	Display      FlowPhaseDisplay `json:"display,omitempty"`
	Instructions string           `json:"instructions,omitempty"`
	Hint         string           `json:"hint,omitempty"`
	OnEnter      []FlowSideEffect `json:"on_enter,omitempty"`
}

// FlowPhaseDisplay holds the UI display properties for a phase.
type FlowPhaseDisplay struct {
	Label string `json:"label,omitempty"`
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
}

// FlowSideEffect is an action executed when entering a phase.
type FlowSideEffect struct {
	Type string `json:"type"`
}

// FlowTransition defines a single phase transition in the flow snapshot.
type FlowTransition struct {
	To      string `json:"to"`
	Label   string `json:"label,omitempty"`
	When    string `json:"when,omitempty"`
	Message string `json:"message,omitempty"`
}

// WorkflowStatus is returned by the "status" query.
type WorkflowStatus struct {
	Phase                Phase                      `json:"phase"`
	Iteration            int                        `json:"iteration"`
	TotalIterations      int                        `json:"total_iterations"`
	ActiveAgents         []string                   `json:"active_agents"`
	EventCount           int                        `json:"event_count"`
	StartedAt            string                     `json:"started_at"`
	LastUpdatedAt        string                     `json:"last_updated_at"`
	Task                 string                     `json:"task"`
	MRUrl                string                     `json:"mr_url,omitempty"`
	PreBlockedPhase      Phase                      `json:"pre_blocked_phase,omitempty"`
	PhaseReason          string                     `json:"phase_reason,omitempty"`
	CurrentPhaseSecs     float64                    `json:"current_phase_secs"`
	PhaseDurationSecs    map[string]float64         `json:"phase_duration_secs,omitempty"`
	CurrentIterPhaseSecs map[string]float64         `json:"current_iter_phase_secs,omitempty"`
	CommandsRan          map[string]map[string]bool `json:"commands_ran,omitempty"`
}

// WorkflowTimeline is returned by the "timeline" query.
type WorkflowTimeline struct {
	Events      []WorkflowEvent `json:"events"`
	TotalEvents int             `json:"total_events,omitempty"`
}

// TransitionResult is used for workflow updates (allow/deny).
type TransitionResult struct {
	Allowed            bool     `json:"allowed"`
	NoOp               bool     `json:"no_op,omitempty"`
	Reason             string   `json:"reason,omitempty"`
	From               Phase    `json:"from"`
	To                 Phase    `json:"to"`
	AllowedTransitions []string `json:"allowed_transitions,omitempty"`
}

// PhaseMetrics tracks time spent in each phase.
type PhaseMetrics struct {
	Phase     Phase         `json:"phase"`
	Duration  time.Duration `json:"duration"`
	EnteredAt time.Time     `json:"entered_at"`
}
