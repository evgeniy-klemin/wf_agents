package model

import "time"

// EventType classifies workflow events.
type EventType string

const (
	EventTransition EventType = "transition"
	EventHookDenial EventType = "hook_denial"
	EventJournal    EventType = "journal"
	EventToolUse    EventType = "tool_use"
	EventAgentSpawn EventType = "agent_spawn"
	EventAgentStop  EventType = "agent_stop"
	EventError      EventType = "error"
)

// WorkflowEvent represents a single event in the workflow event log.
type WorkflowEvent struct {
	Type      EventType         `json:"type"`
	Timestamp time.Time         `json:"timestamp"`
	SessionID string            `json:"session_id"`
	Detail    map[string]string `json:"detail"`
}

// SignalTransition is the payload for a "transition" signal.
type SignalTransition struct {
	To        Phase             `json:"to"`
	SessionID string            `json:"session_id"`
	Reason    string            `json:"reason,omitempty"`
	Guards    map[string]string `json:"guards,omitempty"`
}

// SignalHookEvent is the payload for a "hook-event" signal.
type SignalHookEvent struct {
	HookType  string            `json:"hook_type"` // PreToolUse, SubagentStart, Stop, etc.
	SessionID string            `json:"session_id"`
	Tool      string            `json:"tool,omitempty"`
	Detail    map[string]string `json:"detail,omitempty"`
}

// SignalJournal is the payload for a "journal" signal.
type SignalJournal struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}
