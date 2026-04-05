// Package config provides declarative YAML-based configuration for workflow
// phase transition guards, tracking patterns, and teammate idle rules.
package config

// Config is the top-level configuration structure.
type Config struct {
	Extends             string                        `yaml:"extends,omitempty"`
	Tracking            TrackingConfig                `yaml:"tracking"`
	Guards              []GuardRule                   `yaml:"guards"`
	TeammateIdle        []IdleRule                    `yaml:"teammate_idle"`
	LeadIdle            []LeadIdleRule                `yaml:"lead_idle"`
	TeammatePermissions []TeammatePermission          `yaml:"teammate_permissions"`
	Phases              *PhasesConfig                 `yaml:"phases,omitempty"`
	Transitions         map[string][]TransitionConfig `yaml:"transitions,omitempty"`
}

// TeammatePermission restricts tool usage for teammates by phase and agent name.
// Each rule: agent glob + tools/bash patterns + allowed phases. First matching rule wins.
type TeammatePermission struct {
	Agent   string   `yaml:"agent,omitempty"` // glob pattern, e.g. "developer*"
	Tools   []string `yaml:"tools,omitempty"` // tool names: Edit, Write, NotebookEdit, ...
	Bash    []string `yaml:"bash,omitempty"`  // bash command prefixes: "git commit", ...
	Phases  []string `yaml:"phases"`          // allowed phases
	Message string   `yaml:"message,omitempty"`
}

// LeadIdleRule controls whether the Team Lead can idle/stop in a given phase.
// Phase is a phase name or "*" for wildcard matching.
type LeadIdleRule struct {
	Phase   string `yaml:"phase"`
	Deny    bool   `yaml:"deny"`
	Message string `yaml:"message,omitempty"`
}

// TrackingConfig maps category name to its tracking configuration.
type TrackingConfig map[string]TrackingCategory

// TrackingCategory defines the patterns and invalidation behavior for a tracking category.
type TrackingCategory struct {
	Patterns               []string `yaml:"patterns"`
	InvalidateOnFileChange *bool    `yaml:"invalidate_on_file_change,omitempty"`
}

// ShouldInvalidateOnFileChange returns true if this category should be reset when
// the agent modifies files. Defaults to true when not explicitly set.
func (tc TrackingCategory) ShouldInvalidateOnFileChange() bool {
	if tc.InvalidateOnFileChange == nil {
		return true
	}
	return *tc.InvalidateOnFileChange
}

// fileChangeTools are tool names that count as a file modification for invalidation purposes.
var fileChangeTools = map[string]bool{"Edit": true, "Write": true, "NotebookEdit": true}

// IsFileChangeTool returns true if the given tool name is a file-modification tool.
func IsFileChangeTool(toolName string) bool {
	return fileChangeTools[toolName]
}

// GuardRule defines guard checks for a phase transition.
// From and To are phase name strings or "*" for wildcard matching.
type GuardRule struct {
	From     string  `yaml:"from"`
	To       string  `yaml:"to"`
	Disabled bool    `yaml:"disabled,omitempty"`
	Checks   []Check `yaml:"checks"`
}

// Check is a single guard predicate.
type Check struct {
	// Type is one of: evidence, no_active_agents, max_iterations, command_ran
	Type string `yaml:"type"`

	// Key is the evidence map key (used by evidence checks).
	Key string `yaml:"key,omitempty"`

	// Value is the expected value (used by evidence check).
	Value string `yaml:"value,omitempty"`

	// Category is used by command_ran to match tracking categories (lint, test).
	Category string `yaml:"category,omitempty"`

	// Alternatives holds additional key/value pairs that also satisfy this check (OR semantics).
	Alternatives []KV `yaml:"alternatives,omitempty"`

	// Message is the denial message returned when this check fails.
	Message string `yaml:"message"`
}

// KV is a key/value pair used in Alternatives.
type KV struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}

// IdleRule defines checks that must pass when a teammate goes idle.
// Phase is a phase name or "*" for wildcard matching.
// Agent is an optional glob pattern matched against the teammate name (case-insensitive).
// If empty, the rule applies to all agents.
type IdleRule struct {
	Phase  string  `yaml:"phase"`
	Agent  string  `yaml:"agent,omitempty"`
	Checks []Check `yaml:"checks"`
}
