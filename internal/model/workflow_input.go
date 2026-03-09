package model

import "time"

// WorkflowInput is the input to start a CodingSessionWorkflow.
type WorkflowInput struct {
	SessionID       string `json:"session_id"`
	TaskDescription string `json:"task_description"`
	RepoPath        string `json:"repo_path"`
	BranchName      string `json:"branch_name,omitempty"`
	MaxIterations   int    `json:"max_iterations,omitempty"`
}

// WorkflowStatus is returned by the "status" query.
type WorkflowStatus struct {
	Phase          Phase    `json:"phase"`
	Iteration      int      `json:"iteration"`
	ActiveAgents   []string `json:"active_agents"`
	EventCount     int      `json:"event_count"`
	StartedAt      string   `json:"started_at"`
	LastUpdatedAt  string   `json:"last_updated_at"`
	Task           string   `json:"task"`
	PreBlockedPhase Phase   `json:"pre_blocked_phase,omitempty"`
}

// WorkflowTimeline is returned by the "timeline" query.
type WorkflowTimeline struct {
	Events []WorkflowEvent `json:"events"`
}

// TransitionResult is used for workflow updates (allow/deny).
type TransitionResult struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
	From    Phase  `json:"from"`
	To      Phase  `json:"to"`
}

// PhaseMetrics tracks time spent in each phase.
type PhaseMetrics struct {
	Phase     Phase         `json:"phase"`
	Duration  time.Duration `json:"duration"`
	EnteredAt time.Time     `json:"entered_at"`
}
