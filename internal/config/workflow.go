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

// AllowedTransitions returns all valid target phases for a given from phase.
func (c *Config) AllowedTransitions(from string) []string {
	if c.Transitions == nil {
		return nil
	}
	var result []string
	for _, t := range c.Transitions[from] {
		result = append(result, t.To)
	}
	return result
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

// PhaseBashPolicy returns the bash_policy for a given phase ("whitelist" or "").
func (c *Config) PhaseBashPolicy(phase string) string {
	if c.Phases == nil {
		return ""
	}
	if pc, ok := c.Phases.Phases[phase]; ok {
		return pc.BashPolicy
	}
	return ""
}

// PhaseAllowsFileWritingCommands returns true if any teammate agent has file_writes==allow
// in the given phase, meaning file-modifying commands (like gofmt -w) are permitted.
func (c *Config) PhaseAllowsFileWritingCommands(phase string) bool {
	if c.Phases == nil {
		return false
	}
	if pc, ok := c.Phases.Phases[phase]; ok {
		for _, ap := range pc.Permissions.Teammate {
			if ap.FileWrites == "allow" {
				return true
			}
		}
	}
	return false
}

// IterationIncrementPhases returns the names of phases that have an
// on_enter action of type "increment_iteration".
func (c *Config) IterationIncrementPhases() []string {
	if c.Phases == nil {
		return nil
	}
	var phases []string
	for name, pc := range c.Phases.Phases {
		for _, effect := range pc.OnEnter {
			if effect.Type == "increment_iteration" {
				phases = append(phases, name)
				break
			}
		}
	}
	return phases
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

// LeadFileWritePermission returns whether the Team Lead is allowed to write files
// in a given phase, and optional path restrictions.
// Checks phase-specific lead override first, then falls back to default.
func (c *Config) LeadFileWritePermission(phase string) (allowed bool, paths []string) {
	if c.Phases == nil {
		return true, nil
	}
	// Phase-specific lead override
	if pc, ok := c.Phases.Phases[phase]; ok {
		if pc.Permissions.Lead != nil {
			if pc.Permissions.Lead.FileWrites == "allow" {
				return true, pc.Permissions.Lead.FileWritesPaths
			}
			if pc.Permissions.Lead.FileWrites == "deny" {
				return false, nil
			}
		}
	}
	// Default
	if c.Phases.Defaults.Permissions.Lead.FileWrites == "deny" {
		return false, nil
	}
	return true, nil
}

// IsTeammate returns true if agentName matches any teammate glob pattern defined in
// Phases.Defaults.Permissions.Teammate or any per-phase Permissions.Teammate entry.
func (c *Config) IsTeammate(agentName string) bool {
	if c.Phases == nil {
		return false
	}
	for _, ap := range c.Phases.Defaults.Permissions.Teammate {
		if agentGlobMatch(ap.Agent, agentName) {
			return true
		}
	}
	for _, pc := range c.Phases.Phases {
		for _, ap := range pc.Permissions.Teammate {
			if agentGlobMatch(ap.Agent, agentName) {
				return true
			}
		}
	}
	return false
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
	BashPolicy   string           `yaml:"bash_policy,omitempty" json:"bash_policy,omitempty"` // "whitelist" or "" (default blacklist)
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
	// Lead is a pointer to distinguish "not configured" (nil) from "configured with empty string".
	// DefaultPermissions.Lead uses a value type because it is always present.
	Lead      *RolePermission   `yaml:"lead,omitempty" json:"lead,omitempty"`
	Teammate  []AgentPermission `yaml:"teammate,omitempty" json:"teammate,omitempty"`
}

// RolePermission defines file-write permissions for a role (lead or teammate).
type RolePermission struct {
	FileWrites      string   `yaml:"file_writes,omitempty" json:"file_writes,omitempty"`           // "allow" or "deny"
	FileWritesPaths []string `yaml:"file_writes_paths,omitempty" json:"file_writes_paths,omitempty"` // restrict writes to these dirs
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
//
// Supported forms:
//
//	active_agents == 0          → no_active_agents (structural)
//	iteration < max_iterations  → max_iterations (structural)
//	not <identifier>            → evidence key=identifier value=false
//	<key> == "<value>"          → evidence key=key value=value (quoted string)
//	<id1> or <id2> [or ...]     → evidence key=id1 value=true, alternatives=[{id2,true},...]
//	<identifier>                → evidence key=identifier value=true
func parseSingleWhenExpr(expr, message string) Check {
	// Structural special cases — not evidence-based.
	if expr == "active_agents == 0" {
		return Check{Type: "no_active_agents"}
	}
	if expr == "iteration < max_iterations" {
		return Check{Type: "max_iterations"}
	}
	if expr == "mr_url_saved" {
		return Check{Type: "mr_url_saved", Message: message}
	}

	// "not <identifier>" → evidence value=false
	if strings.HasPrefix(expr, "not ") {
		key := strings.TrimSpace(expr[4:])
		return Check{Type: "evidence", Key: key, Value: "false", Message: message}
	}

	// `<key> == "<value>"` with a quoted string value
	if idx := strings.Index(expr, ` == "`); idx >= 0 {
		key := strings.TrimSpace(expr[:idx])
		rest := expr[idx+5:] // skip ' == "'
		// strip trailing quote
		value := strings.TrimSuffix(rest, `"`)
		return Check{Type: "evidence", Key: key, Value: value, Message: message}
	}

	// "<id1> or <id2> [or ...]" → primary + alternatives
	if strings.Contains(expr, " or ") {
		parts := strings.Split(expr, " or ")
		primary := strings.TrimSpace(parts[0])
		var alts []KV
		for _, p := range parts[1:] {
			alts = append(alts, KV{Key: strings.TrimSpace(p), Value: "true"})
		}
		return Check{
			Type:         "evidence",
			Key:          primary,
			Value:        "true",
			Alternatives: alts,
			Message:      message,
		}
	}

	// Bare identifier → evidence key=identifier value=true
	return Check{Type: "evidence", Key: expr, Value: "true", Message: message}
}
