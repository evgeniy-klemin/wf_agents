package config

import (
	"fmt"
	"path"
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
	// MrUrl returns the MR/PR URL saved on the workflow, or "" if not yet set.
	MrUrl() string
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
			return fmt.Sprintf("max iterations (%d) reached", ctx.MaxIterations())
		}
		return ""

	case "command_ran":
		cmds := ctx.CommandsRan()
		if cmds[check.Category] {
			return ""
		}
		return check.Message

	case "send_message":
		if !ctx.CommandsRan()["_sent_message"] {
			return check.Message
		}
		return ""

	case "mr_url_saved":
		if ctx.MrUrl() != "" {
			return ""
		}
		return check.Message

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

// agentGlobMatch returns true if the agent pattern matches the given name.
// Matching is case-insensitive glob using path.Match semantics.
func agentGlobMatch(pattern, name string) bool {
	matched, err := path.Match(strings.ToLower(pattern), strings.ToLower(name))
	return err == nil && matched
}

// phaseIdleRuleToIdleRule converts a PhaseIdleRule to an IdleRule for compatibility.
func phaseIdleRuleToIdleRule(phase string, r PhaseIdleRule) *IdleRule {
	checks := make([]Check, 0, len(r.Checks))
	for _, pc := range r.Checks {
		checks = append(checks, Check{
			Type:     pc.Type,
			Category: pc.Category,
			Message:  pc.Message,
		})
	}
	return &IdleRule{Phase: phase, Agent: r.Agent, Checks: checks}
}

// FindIdleRule returns the best-matching IdleRule from cfg for the given phase and agentName.
//
// Priority:
//  1. cfg.TeammateIdle (legacy/override entries from .wf-agents/workflow.yaml) — exact phase+agent,
//     exact phase+no-agent, wildcard+agent, wildcard+no-agent.
//  2. cfg.Phases idle rules (new format) — exact phase+agent, exact phase+no-agent,
//     defaults+agent, defaults+no-agent.
//
// This ordering allows project-level .wf-agents/workflow.yaml to override the phases idle rules
// using the legacy teammate_idle format, while the default config uses phases config.
//
// Returns nil if no rule matches.
func FindIdleRule(cfg *Config, phase, agentName string) *IdleRule {
	// Check cfg.TeammateIdle first — these are project-level overrides.
	if len(cfg.TeammateIdle) > 0 {
		var exactNoAgent, wildAgent, wildNoAgent *IdleRule
		for i := range cfg.TeammateIdle {
			r := &cfg.TeammateIdle[i]
			exactPhase := r.Phase == phase
			wildcardPhase := r.Phase == "*"
			if !exactPhase && !wildcardPhase {
				continue
			}
			hasAgent := r.Agent != ""
			agentMatches := hasAgent && agentGlobMatch(r.Agent, agentName)

			if exactPhase {
				if agentMatches {
					return r // highest priority: exact phase + agent match
				}
				if !hasAgent && exactNoAgent == nil {
					exactNoAgent = r
				}
			} else { // wildcard phase
				if agentMatches && wildAgent == nil {
					wildAgent = r
				}
				if !hasAgent && wildNoAgent == nil {
					wildNoAgent = r
				}
			}
		}
		if exactNoAgent != nil {
			return exactNoAgent
		}
		if wildAgent != nil {
			return wildAgent
		}
		if wildNoAgent != nil {
			return wildNoAgent
		}
		// Fall through to phases config if no match in TeammateIdle.
	}

	// New format: read from phases config idle rules (excluding "lead" agent).
	if cfg.Phases != nil {
		var exactAgentRule, exactNoAgentRule, defaultAgentRule, defaultNoAgentRule *IdleRule

		// Check exact phase match.
		if pc, ok := cfg.Phases.Phases[phase]; ok {
			for _, rule := range pc.Idle {
				if rule.Agent == "lead" {
					continue // lead rules are handled by FindLeadIdleRule
				}
				hasAgent := rule.Agent != "" && rule.Agent != "*"
				agentMatches := hasAgent && agentGlobMatch(rule.Agent, agentName)
				if agentMatches && exactAgentRule == nil {
					r := phaseIdleRuleToIdleRule(phase, rule)
					exactAgentRule = r
				} else if !hasAgent && exactNoAgentRule == nil {
					r := phaseIdleRuleToIdleRule(phase, rule)
					exactNoAgentRule = r
				}
			}
		}
		// Check defaults.
		for _, rule := range cfg.Phases.Defaults.Idle {
			if rule.Agent == "lead" {
				continue
			}
			hasAgent := rule.Agent != "" && rule.Agent != "*"
			agentMatches := hasAgent && agentGlobMatch(rule.Agent, agentName)
			if agentMatches && defaultAgentRule == nil {
				r := phaseIdleRuleToIdleRule("*", rule)
				defaultAgentRule = r
			} else if !hasAgent && defaultNoAgentRule == nil {
				r := phaseIdleRuleToIdleRule("*", rule)
				defaultNoAgentRule = r
			}
		}

		if exactAgentRule != nil {
			return exactAgentRule
		}
		if exactNoAgentRule != nil {
			return exactNoAgentRule
		}
		if defaultAgentRule != nil {
			return defaultAgentRule
		}
		return defaultNoAgentRule
	}

	return nil
}

// FindLeadIdleRule returns the best-matching LeadIdleRule for the given phase.
// Reads from cfg.Phases (new format) when available, falling back to cfg.LeadIdle (legacy).
// Exact phase match takes priority over wildcard/defaults.
// Returns nil if no rule matches.
func FindLeadIdleRule(cfg *Config, phase string) *LeadIdleRule {
	// New format: read from phases config idle rules where agent == "lead".
	if cfg.Phases != nil {
		// Check exact phase match first.
		if pc, ok := cfg.Phases.Phases[phase]; ok {
			for _, rule := range pc.Idle {
				if rule.Agent == "lead" {
					return &LeadIdleRule{Phase: phase, Deny: rule.Deny, Message: rule.Message}
				}
			}
		}
		// Fall back to defaults idle rules.
		for _, rule := range cfg.Phases.Defaults.Idle {
			if rule.Agent == "lead" {
				return &LeadIdleRule{Phase: "*", Deny: rule.Deny, Message: rule.Message}
			}
		}
		return nil
	}

	// Legacy fallback: read from cfg.LeadIdle.
	var wildcard *LeadIdleRule
	for i := range cfg.LeadIdle {
		r := &cfg.LeadIdle[i]
		if r.Phase == phase {
			return r // exact match
		}
		if r.Phase == "*" && wildcard == nil {
			wildcard = r
		}
	}
	return wildcard
}

// FindTeammatePermission returns the first matching permission rule for the given
// phase, agent name, tool name, and bash command. Returns nil if no rule matches.
// Matching: agent glob (or empty=all), tool in Tools list OR bashCmd matches a Bash prefix.
//
// When cfg.Phases is set (new format), file-writing tool permissions are derived from
// per-phase AgentPermission.FileWrites settings. A synthesized TeammatePermission is
// returned with Phases set to the list of phases where file_writes==allow.
func FindTeammatePermission(cfg *Config, phase, agentName, toolName, bashCmd string) *TeammatePermission {
	// New format: derive from phases config when Phases is available.
	if cfg.Phases != nil && toolName != "" {
		// Only file-writing tools are controlled by the phases teammate permissions.
		isFileWrite := false
		for _, t := range cfg.Phases.Defaults.Permissions.FileWritingTools {
			if t == toolName {
				isFileWrite = true
				break
			}
		}
		if isFileWrite {
			return findTeammatePermissionFromPhases(cfg, agentName, toolName)
		}
		// Non-file-write tools are not restricted via phases teammate permissions.
		return nil
	}

	// Legacy fallback: read from cfg.TeammatePermissions.
	return findTeammatePermissionLegacy(cfg, agentName, toolName, bashCmd)
}

// findTeammatePermissionFromPhases synthesizes a TeammatePermission from the phases config.
// It collects all phases where the given agent has file_writes==allow and returns a rule
// whose Phases field contains those phases. The caller checks if the current phase is
// in rule.Phases to determine whether file writes are allowed.
//
// Only agents that have at least one phase with file_writes==allow are restricted.
// Agents with no allow rules anywhere return nil (default open, no restriction),
// preserving backward compatibility with agents like "reviewer*" that are not
// explicitly managed by the permissions system.
func findTeammatePermissionFromPhases(cfg *Config, agentName, toolName string) *TeammatePermission {
	// Collect phases where this agent explicitly has file_writes==allow.
	var allowedPhases []string
	for phaseName, pc := range cfg.Phases.Phases {
		for _, ap := range pc.Permissions.Teammate {
			if agentGlobMatch(ap.Agent, agentName) && ap.FileWrites == "allow" {
				allowedPhases = append(allowedPhases, phaseName)
				break
			}
		}
	}

	// If the agent has no phase-specific allow rules, return nil (no restriction).
	// This means the agent is not managed by the phases permissions system, and the
	// default-open behavior applies — consistent with how the legacy teammate_permissions
	// section only managed developer* agents.
	if len(allowedPhases) == 0 {
		return nil
	}

	// Agent has explicit phase-level allow rules — synthesize a permission rule.
	// Phases = allowed list; if current phase is not in this list, the caller denies.
	defaultMsg := "File editing only allowed in designated phases"
	// Try to get the message from the default deny rule for this agent.
	for _, ap := range cfg.Phases.Defaults.Permissions.Teammate {
		if agentGlobMatch(ap.Agent, agentName) && ap.FileWrites == "deny" {
			break // no message field on AgentPermission, use default
		}
	}
	return &TeammatePermission{
		Agent:   agentName,
		Tools:   []string{toolName},
		Phases:  allowedPhases,
		Message: defaultMsg,
	}
}

// findTeammatePermissionLegacy is the legacy implementation reading from cfg.TeammatePermissions.
func findTeammatePermissionLegacy(cfg *Config, agentName, toolName, bashCmd string) *TeammatePermission {
	for i := range cfg.TeammatePermissions {
		r := &cfg.TeammatePermissions[i]
		// Check agent glob
		if r.Agent != "" && !agentGlobMatch(r.Agent, agentName) {
			continue
		}
		// Check tool name match
		toolMatches := false
		for _, t := range r.Tools {
			if t == toolName {
				toolMatches = true
				break
			}
		}
		// Check bash command prefix match
		bashMatches := false
		if bashCmd != "" {
			for _, prefix := range r.Bash {
				if strings.HasPrefix(bashCmd, prefix) {
					rest := bashCmd[len(prefix):]
					if rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '|' || rest[0] == ';' || rest[0] == '&' || rest[0] == '\n' {
						bashMatches = true
						break
					}
				}
			}
		}
		if toolMatches || bashMatches {
			return r
		}
	}
	return nil
}

// FindGuards returns the GuardRules from cfg that match the given from→to transition.
// Reads from cfg.Transitions (new format) when available, falling back to cfg.Guards (legacy).
// Legacy cfg.Guards also supports wildcard "*" matching for from/to.
//
// When cfg.Transitions is set, cfg.Guards is still checked for disabled entries
// (disabled:true in an override YAML) which take precedence and suppress the guard.
func FindGuards(cfg *Config, from, to string) []GuardRule {
	// New format: derive checks from transitions[from] where t.To == to.
	if cfg.Transitions != nil {
		// Check if a guards override has explicitly disabled this transition's guard.
		for _, rule := range cfg.Guards {
			if rule.From == from && rule.To == to && rule.Disabled {
				return nil
			}
		}

		if transitions, ok := cfg.Transitions[from]; ok {
			for _, t := range transitions {
				if t.To != to {
					continue
				}
				checks := ParseWhenExpression(t.When, t.Message)
				if len(checks) > 0 {
					return []GuardRule{{From: from, To: to, Checks: checks}}
				}
				// Empty when = no guard checks (always allowed).
				return nil
			}
		}
		// Transition not found in new format → no guard (transition may be validated
		// separately by IsValidTransition; no checks means always allowed if valid).
		return nil
	}

	// Legacy fallback: read from cfg.Guards (supports wildcard "*" matching).
	// Skip disabled entries.
	var exact, wild []GuardRule
	for _, rule := range cfg.Guards {
		if rule.Disabled {
			continue
		}
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

// FindGuardsLegacy is the legacy implementation reading from cfg.Guards.
// Used by tests that exercise the old format directly.
// Skips disabled entries (Disabled:true).
func FindGuardsLegacy(cfg *Config, from, to string) []GuardRule {
	var exact, wild []GuardRule
	for _, rule := range cfg.Guards {
		if rule.Disabled {
			continue
		}
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
