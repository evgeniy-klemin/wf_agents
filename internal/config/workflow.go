package config

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// IsValidTransition checks if from→to is a valid transition in config.
func (c *Config) IsValidTransition(from, to string) bool {
	if c.Transitions == nil {
		return false
	}
	for _, t := range c.Transitions[from] {
		if t.To == to {
			return true
		}
	}
	return false
}

// PhaseHint returns the hint for a phase from config.
func (c *Config) PhaseHint(phase string) string {
	if c.Phases == nil {
		return ""
	}
	if pc, ok := c.Phases.Phases[phase]; ok {
		return pc.Hint
	}
	return ""
}

// StartPhase returns the configured start phase.
func (c *Config) StartPhase() string {
	if c.Phases == nil {
		return ""
	}
	return c.Phases.Start
}

// StopPhases returns the configured stop phases.
func (c *Config) StopPhases() []string {
	if c.Phases == nil {
		return nil
	}
	return c.Phases.Stop
}

// SafeCommands returns the default safe bash commands from config.
func (c *Config) SafeCommands() []string {
	if c.Phases == nil {
		return nil
	}
	return c.Phases.Defaults.Permissions.SafeCommands
}

// PhaseWhitelist returns additional whitelisted commands for a specific phase.
func (c *Config) PhaseWhitelist(phase string) []string {
	if c.Phases == nil {
		return nil
	}
	if pc, ok := c.Phases.Phases[phase]; ok {
		return pc.Permissions.Whitelist
	}
	return nil
}

// ReadOnlyTools returns tools that are auto-approved (read-only).
func (c *Config) ReadOnlyTools() []string {
	if c.Phases == nil {
		return nil
	}
	return c.Phases.Defaults.Permissions.ReadOnlyTools
}

// FileWritingTools returns tools considered file-writing (Edit, Write, NotebookEdit).
func (c *Config) FileWritingTools() []string {
	if c.Phases == nil {
		return nil
	}
	return c.Phases.Defaults.Permissions.FileWritingTools
}

// LeadFileWritesDenied returns true if team lead file writes are denied by default.
func (c *Config) LeadFileWritesDenied() bool {
	if c.Phases == nil {
		return false
	}
	return c.Phases.Defaults.Permissions.Lead.FileWrites == "deny"
}

// TeammateFileWritePermission returns the file_writes permission for a matching agent in a given phase.
// Checks phase-specific teammate rules first, then falls back to default teammate rules.
// Returns: "allow", "deny", or "" (not specified, use default).
func (c *Config) TeammateFileWritePermission(phase, agentName string) string {
	if c.Phases == nil {
		return ""
	}
	// Check phase-specific teammate rules first
	if pc, ok := c.Phases.Phases[phase]; ok {
		for _, ap := range pc.Permissions.Teammate {
			if agentGlobMatch(ap.Agent, agentName) && ap.FileWrites != "" {
				return ap.FileWrites
			}
		}
	}
	// Fall back to default teammate rules
	for _, ap := range c.Phases.Defaults.Permissions.Teammate {
		if agentGlobMatch(ap.Agent, agentName) && ap.FileWrites != "" {
			return ap.FileWrites
		}
	}
	return ""
}

// reservedPhaseKeys are keys in the phases section that are not phase names.
var reservedPhaseKeys = map[string]bool{
	"start":    true,
	"stop":     true,
	"defaults": true,
}

// PhasesConfig is the top-level phases section of the workflow config.
// It uses a custom UnmarshalYAML because start, stop, defaults, and phase
// names all appear at the same YAML level.
type PhasesConfig struct {
	Start      string                 `yaml:"start" json:"start"`
	Stop       []string               `yaml:"stop" json:"stop"`
	Defaults   PhaseDefaults          `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Phases     map[string]PhaseConfig `yaml:"-" json:"phases"`
	PhaseOrder []string               `yaml:"-" json:"phase_order"` // preserves YAML insertion order
}

// UnmarshalYAML implements yaml.Unmarshaler for PhasesConfig.
// It parses start/stop/defaults via the standard struct fields, then
// treats every remaining key as a phase name → PhaseConfig.
func (p *PhasesConfig) UnmarshalYAML(value *yaml.Node) error {
	// Use an alias to avoid infinite recursion.
	type phasesAlias struct {
		Start    string        `yaml:"start"`
		Stop     []string      `yaml:"stop"`
		Defaults PhaseDefaults `yaml:"defaults,omitempty"`
	}
	var alias phasesAlias
	if err := value.Decode(&alias); err != nil {
		return err
	}
	p.Start = alias.Start
	p.Stop = alias.Stop
	p.Defaults = alias.Defaults

	// Collect remaining keys as phase configs, preserving YAML insertion order.
	if value.Kind != yaml.MappingNode {
		return nil
	}
	p.Phases = make(map[string]PhaseConfig)
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode := value.Content[i]
		valNode := value.Content[i+1]
		key := keyNode.Value
		if reservedPhaseKeys[key] {
			continue
		}
		var pc PhaseConfig
		if err := valNode.Decode(&pc); err != nil {
			return err
		}
		p.Phases[key] = pc
		p.PhaseOrder = append(p.PhaseOrder, key)
	}
	return nil
}

// PhaseConfig defines the configuration for a single workflow phase.
type PhaseConfig struct {
	Display      PhaseDisplay     `yaml:"display,omitempty" json:"display,omitempty"`
	Instructions string           `yaml:"instructions,omitempty" json:"instructions,omitempty"`
	Hint         string           `yaml:"hint,omitempty" json:"hint,omitempty"`
	Permissions  PhasePermissions `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	Idle         []PhaseIdleRule  `yaml:"idle,omitempty" json:"idle,omitempty"`
	OnEnter      []SideEffect     `yaml:"on_enter,omitempty" json:"on_enter,omitempty"`
}

// PhaseDisplay holds the UI display properties for a phase.
type PhaseDisplay struct {
	Label string `yaml:"label,omitempty" json:"label,omitempty"`
	Icon  string `yaml:"icon,omitempty" json:"icon,omitempty"`
	Color string `yaml:"color,omitempty" json:"color,omitempty"`
}

// PhasePermissions defines the permission rules for a phase.
type PhasePermissions struct {
	Whitelist []string          `yaml:"whitelist,omitempty" json:"whitelist,omitempty"`
	Lead      *RolePermission   `yaml:"lead,omitempty" json:"lead,omitempty"`
	Teammate  []AgentPermission `yaml:"teammate,omitempty" json:"teammate,omitempty"`
}

// RolePermission defines file-write permissions for a role (lead or teammate).
type RolePermission struct {
	FileWrites string `yaml:"file_writes,omitempty" json:"file_writes,omitempty"` // "allow" or "deny"
}

// AgentPermission defines permissions for a specific agent (by glob pattern).
type AgentPermission struct {
	Agent      string   `yaml:"agent" json:"agent"`
	FileWrites string   `yaml:"file_writes,omitempty" json:"file_writes,omitempty"` // "allow" or "deny"
	Deny       []string `yaml:"deny,omitempty" json:"deny,omitempty"`
}

// PhaseDefaults holds default permissions and idle rules applied to all phases.
type PhaseDefaults struct {
	Permissions DefaultPermissions `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	Idle        []PhaseIdleRule    `yaml:"idle,omitempty" json:"idle,omitempty"`
}

// DefaultPermissions defines the default permission settings for all phases.
type DefaultPermissions struct {
	SafeCommands     []string          `yaml:"safe_commands,omitempty" json:"safe_commands,omitempty"`
	ReadOnlyTools    []string          `yaml:"read_only_tools,omitempty" json:"read_only_tools,omitempty"`
	FileWritingTools []string          `yaml:"file_writing_tools,omitempty" json:"file_writing_tools,omitempty"`
	Lead             RolePermission    `yaml:"lead,omitempty" json:"lead,omitempty"`
	Teammate         []AgentPermission `yaml:"teammate,omitempty" json:"teammate,omitempty"`
}

// PhaseIdleRule defines idle behaviour for an agent in a phase.
type PhaseIdleRule struct {
	Agent   string       `yaml:"agent,omitempty" json:"agent,omitempty"`
	Deny    bool         `yaml:"deny,omitempty" json:"deny,omitempty"`
	Message string       `yaml:"message,omitempty" json:"message,omitempty"`
	Checks  []PhaseCheck `yaml:"checks,omitempty" json:"checks,omitempty"`
}

// PhaseCheck is a check used in phase idle rules (e.g. command_ran).
type PhaseCheck struct {
	Type     string `yaml:"type" json:"type"`
	Category string `yaml:"category,omitempty" json:"category,omitempty"`
	Message  string `yaml:"message,omitempty" json:"message,omitempty"`
}

// TransitionConfig defines a single phase transition.
type TransitionConfig struct {
	To      string `yaml:"to" json:"to"`
	Label   string `yaml:"label,omitempty" json:"label,omitempty"`
	When    string `yaml:"when,omitempty" json:"when,omitempty"`
	Message string `yaml:"message,omitempty" json:"message,omitempty"`
}

// SideEffect is an action executed when entering a phase.
type SideEffect struct {
	Type string `yaml:"type" json:"type"`
}

// ParseWhenExpression converts a transition `when` string and its message into
// a slice of Check structs. Returns nil if when is empty (always-allowed transition).
//
// Supported expressions:
//
//	working_tree_clean          → evidence key=working_tree_clean value=true
//	not working_tree_clean      → evidence key=working_tree_clean value=false
//	active_agents == 0          → no_active_agents
//	iteration < max_iterations  → max_iterations
//	ci_passed                   → evidence key=ci_passed value=true
//	review_approved or merged   → evidence key=review_approved value=true, alternatives=[{merged,true}]
//	X and Y                     → two separate Check structs (AND semantics)
func ParseWhenExpression(when, message string) []Check {
	when = strings.TrimSpace(when)
	if when == "" {
		return nil
	}

	// Split on " and " for AND-semantics (multiple checks).
	parts := splitAnd(when)
	var checks []Check
	for _, part := range parts {
		part = strings.TrimSpace(part)
		c := parseSingleWhenExpr(part, message)
		checks = append(checks, c)
	}
	return checks
}

// splitAnd splits a when expression on " and " conjunctions.
func splitAnd(expr string) []string {
	var parts []string
	for {
		idx := strings.Index(expr, " and ")
		if idx < 0 {
			parts = append(parts, expr)
			break
		}
		parts = append(parts, expr[:idx])
		expr = expr[idx+5:]
	}
	return parts
}

// parseSingleWhenExpr parses a single (non-compound) when expression token.
// The message is used for evidence checks. For no_active_agents and max_iterations,
// the message is left empty so EvalCheck uses its built-in fallback messages.
func parseSingleWhenExpr(expr, message string) Check {
	switch expr {
	case "working_tree_clean":
		return Check{Type: "evidence", Key: "working_tree_clean", Value: "true", Message: message}
	case "not working_tree_clean":
		return Check{Type: "evidence", Key: "working_tree_clean", Value: "false", Message: message}
	case "active_agents == 0":
		// EvalCheck has a built-in message for no_active_agents; leave empty to use it.
		return Check{Type: "no_active_agents"}
	case "iteration < max_iterations":
		// EvalCheck has a built-in fallback message for max_iterations; leave empty to use it.
		return Check{Type: "max_iterations"}
	case "ci_passed":
		return Check{Type: "evidence", Key: "ci_passed", Value: "true", Message: message}
	case "branch_pushed":
		return Check{Type: "evidence", Key: "branch_pushed", Value: "true", Message: message}
	case "review_approved or merged":
		return Check{
			Type:         "evidence",
			Key:          "review_approved",
			Value:        "true",
			Alternatives: []KV{{Key: "merged", Value: "true"}},
			Message:      message,
		}
	default:
		// Unknown expression — return a check that always fails with a descriptive message.
		return Check{Type: "evidence", Key: "_unknown_when_expr_" + expr, Value: "true", Message: message}
	}
}
