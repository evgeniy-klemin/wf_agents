package config

import (
	"fmt"
	"path"
	"strings"
)

// knownWhenTokens are the valid identifiers in when expressions.
// Operators (and, or, not, ==, <, >) are also allowed.
var knownWhenVariables = map[string]bool{
	"working_tree_clean": true,
	"branch_pushed":      true,
	"ci_passed":          true,
	"review_approved":    true,
	"merged":             true,
	"active_agents":      true,
	"iteration":          true,
	"max_iterations":     true,
}

// whenOperators are tokens in when expressions that are not variables.
var whenOperators = map[string]bool{
	"and": true,
	"or":  true,
	"not": true,
	"==":  true,
	"<":   true,
	">":   true,
	"<=":  true,
	">=":  true,
	"!=":  true,
	"0":   true,
}

// ValidateWorkflowConfig validates the workflow configuration.
// Returns all validation errors (not just the first).
// If cfg.Phases is nil, validation is skipped (not yet configured).
func ValidateWorkflowConfig(cfg *Config) []error {
	if cfg.Phases == nil {
		return nil
	}

	var errs []error
	add := func(format string, args ...interface{}) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	phases := cfg.Phases.Phases
	if phases == nil {
		phases = map[string]PhaseConfig{}
	}

	// 1. Structural: BLOCKED must not appear in YAML phases
	if _, ok := phases["BLOCKED"]; ok {
		add("phase 'BLOCKED' must not be defined in YAML — it is an infrastructure meta-phase")
	}

	// 1. Structural: start references a defined phase
	if cfg.Phases.Start == "" {
		add("phases.start is required")
	} else if _, ok := phases[cfg.Phases.Start]; !ok {
		add("phases.start %q references undefined phase", cfg.Phases.Start)
	}

	// 1. Structural: stop references defined phases
	stopSet := make(map[string]bool)
	for _, s := range cfg.Phases.Stop {
		stopSet[s] = true
		if _, ok := phases[s]; !ok {
			add("phases.stop %q references undefined phase", s)
		}
	}

	// 2. Graph: all transition 'to' targets reference defined phases
	for from, transitions := range cfg.Transitions {
		// stop phases must not have outgoing transitions
		if stopSet[from] {
			add("stop phase %q must not have outgoing transitions", from)
			continue
		}
		for _, t := range transitions {
			if _, ok := phases[t.To]; !ok {
				add("transition %s→%s: target phase %q is not defined", from, t.To, t.To)
			}
		}
	}

	// 2. Graph: BFS reachability from start
	if cfg.Phases.Start != "" {
		if _, startExists := phases[cfg.Phases.Start]; startExists {
			reachable := bfsReachable(cfg.Phases.Start, cfg.Transitions)
			for name := range phases {
				if name == cfg.Phases.Start {
					continue
				}
				if !reachable[name] {
					add("phase %q is not reachable from start phase %q", name, cfg.Phases.Start)
				}
			}
		}
	}

	// 3. Transitions: when expressions + message validation
	for from, transitions := range cfg.Transitions {
		for i, t := range transitions {
			if t.When != "" {
				// Check for unknown variables in when expression
				if unknowns := unknownWhenVars(t.When); len(unknowns) > 0 {
					add("transition %s→%s [%d]: unknown when variable(s): %s",
						from, t.To, i, strings.Join(unknowns, ", "))
				}
				// Message must be non-empty when 'when' is present
				if t.Message == "" {
					add("transition %s→%s [%d]: message is required when 'when' is set", from, t.To, i)
				}
			}
		}
	}

	// 4. Side-effects: known types only
	knownSideEffects := map[string]bool{"increment_iteration": true}
	for name, pc := range phases {
		for i, se := range pc.OnEnter {
			if !knownSideEffects[se.Type] {
				add("phase %q on_enter[%d]: unknown side-effect type %q", name, i, se.Type)
			}
		}
	}

	// 6. Permissions: validate agent glob patterns
	for name, pc := range phases {
		for i, ap := range pc.Permissions.Teammate {
			if err := validateGlob(ap.Agent); err != nil {
				add("phase %q permissions.teammate[%d]: invalid agent glob pattern %q: %v",
					name, i, ap.Agent, err)
			}
		}
	}
	// Validate defaults teammate globs
	for i, ap := range cfg.Phases.Defaults.Permissions.Teammate {
		if err := validateGlob(ap.Agent); err != nil {
			add("phases.defaults.permissions.teammate[%d]: invalid agent glob pattern %q: %v",
				i, ap.Agent, err)
		}
	}

	return errs
}

// bfsReachable returns the set of phases reachable from start via transitions.
func bfsReachable(start string, transitions map[string][]TransitionConfig) map[string]bool {
	visited := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, t := range transitions[cur] {
			if !visited[t.To] {
				visited[t.To] = true
				queue = append(queue, t.To)
			}
		}
	}
	return visited
}

// unknownWhenVars parses a when expression and returns any unknown variable names.
// Tokens are split by whitespace; numbers and operators are excluded.
func unknownWhenVars(when string) []string {
	var unknowns []string
	tokens := strings.Fields(when)
	for _, tok := range tokens {
		// Skip operators and numeric literals
		if whenOperators[tok] {
			continue
		}
		// Skip pure numeric literals
		if isNumeric(tok) {
			continue
		}
		// Must be a known variable
		if !knownWhenVariables[tok] {
			unknowns = append(unknowns, tok)
		}
	}
	return unknowns
}

// isNumeric returns true if s is an integer literal.
func isNumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// validateGlob checks that a glob pattern is syntactically valid.
func validateGlob(pattern string) error {
	_, err := path.Match(pattern, "")
	return err
}
