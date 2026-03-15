package config

import (
	"fmt"
	"strings"
)

// CheckContext provides the runtime values needed to evaluate guard checks.
type CheckContext interface {
	Evidence() map[string]string
	ActiveAgentCount() int
	Iteration() int
	MaxIterations() int
	// OriginPhase is the phase from which the transition originates.
	// Used by max_iterations to exempt transitions from PLANNING.
	OriginPhase() string
	// CommandsRan tracks which commands have been run (Phase 2 state).
	CommandsRan() map[string]bool
	// TeammateName returns the name of the teammate (used by role_check).
	// Returns "" when the context is not teammate-specific.
	TeammateName() string
}

// EvalCheck evaluates a single Check against the given context.
// Returns "" if the check passes, or a denial reason string if it fails.
func EvalCheck(check Check, ctx CheckContext) string {
	switch check.Type {
	case "evidence":
		ev := ctx.Evidence()
		// Primary key/value check
		if ev[check.Key] == check.Value {
			return ""
		}
		// Alternatives (OR semantics)
		for _, alt := range check.Alternatives {
			if ev[alt.Key] == alt.Value {
				return ""
			}
		}
		return check.Message

	case "no_active_agents":
		if ctx.ActiveAgentCount() == 0 {
			return ""
		}
		if check.Message != "" {
			return fmt.Sprintf("%s (%d active)", check.Message, ctx.ActiveAgentCount())
		}
		return fmt.Sprintf(
			"cannot leave RESPAWN with %d active teammate(s) — shut down old teammates before spawning new ones",
			ctx.ActiveAgentCount(),
		)

	case "max_iterations":
		// First entry from PLANNING doesn't count as an iteration.
		if ctx.OriginPhase() == "PLANNING" {
			return ""
		}
		if ctx.Iteration()+1 > ctx.MaxIterations() {
			if check.Message != "" {
				return fmt.Sprintf("%s (max: %d)", check.Message, ctx.MaxIterations())
			}
			return fmt.Sprintf(
				"max iterations (%d) reached. Ask the user whether to continue. If yes, run: wf-client reset-iterations <workflow-id>, then retry this transition.",
				ctx.MaxIterations(),
			)
		}
		return ""

	case "command_ran":
		cmds := ctx.CommandsRan()
		if cmds[check.Key] {
			return ""
		}
		return check.Message

	case "role_check":
		// Passes if the teammate name does NOT contain the role key (i.e. check only applies
		// to teammates whose name contains key). If it does contain the key, return the message.
		if strings.Contains(strings.ToLower(ctx.TeammateName()), strings.ToLower(check.Key)) {
			return check.Message
		}
		return ""

	default:
		return fmt.Sprintf("unknown check type %q", check.Type)
	}
}

// EvalChecks evaluates all checks with AND semantics, short-circuiting on first failure.
// Returns "" if all checks pass, or the first denial reason.
func EvalChecks(checks []Check, ctx CheckContext) string {
	for _, check := range checks {
		if reason := EvalCheck(check, ctx); reason != "" {
			return reason
		}
	}
	return ""
}

// FindIdleRule returns the first IdleRule from cfg that matches the given phase.
// Exact phase matches are preferred over wildcard ("*") matches.
// Returns nil if no rule matches.
func FindIdleRule(cfg *Config, phase string) *IdleRule {
	var wild *IdleRule
	for i := range cfg.TeammateIdle {
		r := &cfg.TeammateIdle[i]
		if r.Match == phase {
			return r
		}
		if r.Match == "*" && wild == nil {
			wild = r
		}
	}
	return wild
}

// FindGuards returns the GuardRules from cfg that match the given from→to transition.
// Exact matches (no wildcards) are returned before wildcard matches.
// Both from and to can be "*" in rules for wildcard matching.
func FindGuards(cfg *Config, from, to string) []GuardRule {
	var exact, wild []GuardRule
	for _, rule := range cfg.Guards {
		fromMatch := rule.From == from || rule.From == "*"
		toMatch := rule.To == to || rule.To == "*"
		if !fromMatch || !toMatch {
			continue
		}
		if rule.From == "*" || rule.To == "*" {
			wild = append(wild, rule)
		} else {
			exact = append(exact, rule)
		}
	}
	return append(exact, wild...)
}
