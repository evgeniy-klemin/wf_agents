// Package config provides declarative YAML-based configuration for workflow
// phase transition guards, tracking patterns, and teammate idle rules.
package config

// Config is the top-level configuration structure.
type Config struct {
	Tracking     TrackingConfig `yaml:"tracking"`
	Guards       []GuardRule    `yaml:"guards"`
	TeammateIdle []IdleRule     `yaml:"teammate_idle"`
}

// TrackingConfig holds command patterns used to detect when lint/test commands ran.
type TrackingConfig struct {
	Lint []string `yaml:"lint"`
	Test []string `yaml:"test"`
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

	// Key is the evidence map key (used by evidence and command_ran checks).
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
// Match is a phase name or "*" for wildcard matching.
type IdleRule struct {
	Match  string  `yaml:"match"`
	Checks []Check `yaml:"checks"`
}
