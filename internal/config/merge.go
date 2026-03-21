package config

// MergeConfigs merges override on top of base, returning a new Config.
// Neither base nor override is mutated.
//
// Merge rules:
//   - tracking: same category key → override replaces base (patterns + flag); new keys append
//   - guards: same from+to → append override checks to base checks;
//     disabled:true in override → remove all rules for that from+to pair
//   - teammate_idle: same match → replace; new match → append
//   - phases: field-level merge per phase name; start/stop replaced if set in override
//   - transitions: per source-phase replacement (override completely replaces base transitions for a given from-phase)
func MergeConfigs(base, override *Config) *Config {
	result := &Config{}

	// Merge tracking — override replaces by key, new keys append
	result.Tracking = mergeTracking(base.Tracking, override.Tracking)

	// Merge guards
	result.Guards = mergeGuards(base.Guards, override.Guards)

	// Merge teammate_idle
	result.TeammateIdle = mergeIdleRules(base.TeammateIdle, override.TeammateIdle)

	// Merge lead_idle
	result.LeadIdle = mergeLeadIdleRules(base.LeadIdle, override.LeadIdle)

	// Merge teammate_permissions — override replaces base for same agent+tool key; new entries append
	result.TeammatePermissions = mergeTeammatePermissions(base.TeammatePermissions, override.TeammatePermissions)

	// Merge phases — field-level per phase name
	result.Phases = mergePhases(base.Phases, override.Phases)

	// Merge transitions — per source-phase replacement
	result.Transitions = mergeTransitions(base.Transitions, override.Transitions)

	return result
}

// mergePhases merges override PhasesConfig on top of base.
//   - start/stop from override replace base if set
//   - Individual phase configs are merged field-by-field: a phase in override updates
//     or adds to the base phase map; an override phase with a non-zero field replaces
//     that field in the base phase.
//   - defaults from override replace base if override.Defaults is non-zero.
func mergePhases(base, override *PhasesConfig) *PhasesConfig {
	if override == nil {
		return base
	}
	if base == nil {
		return override
	}

	result := &PhasesConfig{
		Start:    base.Start,
		Stop:     base.Stop,
		Defaults: base.Defaults,
	}

	if override.Start != "" {
		result.Start = override.Start
	}
	if len(override.Stop) > 0 {
		result.Stop = override.Stop
	}
	// Replace defaults entirely if override has any defaults set.
	if len(override.Defaults.Permissions.SafeCommands) > 0 ||
		len(override.Defaults.Permissions.ReadOnlyTools) > 0 ||
		len(override.Defaults.Permissions.FileWritingTools) > 0 ||
		override.Defaults.Permissions.Lead.FileWrites != "" ||
		len(override.Defaults.Permissions.Teammate) > 0 ||
		len(override.Defaults.Idle) > 0 {
		result.Defaults = override.Defaults
	}

	// Merge individual phases: start with base phases, then apply override per-phase.
	result.Phases = make(map[string]PhaseConfig, len(base.Phases))
	for k, v := range base.Phases {
		result.Phases[k] = v
	}
	for name, ov := range override.Phases {
		base, exists := result.Phases[name]
		if !exists {
			// New phase from override — add as-is.
			result.Phases[name] = ov
			continue
		}
		// Merge individual fields: override replaces only fields that are set.
		merged := base
		if ov.Display.Label != "" {
			merged.Display.Label = ov.Display.Label
		}
		if ov.Display.Icon != "" {
			merged.Display.Icon = ov.Display.Icon
		}
		if ov.Display.Color != "" {
			merged.Display.Color = ov.Display.Color
		}
		if ov.Instructions != "" {
			merged.Instructions = ov.Instructions
		}
		if ov.Hint != "" {
			merged.Hint = ov.Hint
		}
		if ov.Permissions.Lead != nil {
			merged.Permissions.Lead = ov.Permissions.Lead
		}
		if len(ov.Permissions.Teammate) > 0 {
			merged.Permissions.Teammate = ov.Permissions.Teammate
		}
		if len(ov.Permissions.Whitelist) > 0 {
			merged.Permissions.Whitelist = ov.Permissions.Whitelist
		}
		if len(ov.Idle) > 0 {
			merged.Idle = ov.Idle
		}
		if len(ov.OnEnter) > 0 {
			merged.OnEnter = ov.OnEnter
		}
		result.Phases[name] = merged
	}

	return result
}

// mergeTransitions merges override transitions on top of base.
// Override completely replaces transitions for a given source phase.
// Source phases not in override keep base transitions.
func mergeTransitions(base, override map[string][]TransitionConfig) map[string][]TransitionConfig {
	if override == nil {
		return base
	}
	if base == nil {
		return override
	}

	result := make(map[string][]TransitionConfig, len(base))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v // complete replacement per source phase
	}
	return result
}

func mergeTracking(base, override TrackingConfig) TrackingConfig {
	result := make(TrackingConfig)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v // override replaces base for same key
	}
	return result
}

func mergeGuards(base, override []GuardRule) []GuardRule {
	// Build index of base rules by from+to key
	type key struct{ from, to string }
	index := make(map[key]int)
	result := make([]GuardRule, 0, len(base)+len(override))

	for _, r := range base {
		k := key{r.From, r.To}
		index[k] = len(result)
		result = append(result, r)
	}

	for _, r := range override {
		k := key{r.From, r.To}
		if idx, exists := index[k]; exists {
			if r.Disabled {
				// Remove all rules for this from+to pair
				result[idx] = GuardRule{From: r.From, To: r.To, Disabled: true}
			} else {
				// Deep copy base Checks before appending to avoid mutating the base slice.
				existing := result[idx]
				merged := make([]Check, len(existing.Checks), len(existing.Checks)+len(r.Checks))
				copy(merged, existing.Checks)
				existing.Checks = append(merged, r.Checks...)
				result[idx] = existing
			}
		} else {
			// New pair: add regardless of disabled state.
			// Disabled entries are kept as suppressors for transitions-based guard lookup.
			index[k] = len(result)
			result = append(result, r)
		}
	}

	// Keep disabled placeholder entries in the result. FindGuards checks for Disabled:true
	// entries to suppress guards derived from the transitions format. The legacy FindGuardsLegacy
	// path skips disabled entries when collecting guard rules.
	return result
}

func mergeLeadIdleRules(base, override []LeadIdleRule) []LeadIdleRule {
	index := make(map[string]int)
	result := make([]LeadIdleRule, 0, len(base)+len(override))

	for _, r := range base {
		index[r.Phase] = len(result)
		result = append(result, r)
	}

	for _, r := range override {
		if idx, exists := index[r.Phase]; exists {
			result[idx] = r // replace
		} else {
			index[r.Phase] = len(result)
			result = append(result, r)
		}
	}

	return result
}

func idleRuleKey(r IdleRule) string {
	return r.Phase + "|" + r.Agent
}

func mergeTeammatePermissions(base, override []TeammatePermission) []TeammatePermission {
	type key struct{ agent string }
	index := make(map[key]int)
	result := make([]TeammatePermission, 0, len(base)+len(override))

	for _, r := range base {
		k := key{r.Agent}
		index[k] = len(result)
		result = append(result, r)
	}

	for _, r := range override {
		k := key{r.Agent}
		if idx, exists := index[k]; exists {
			result[idx] = r // replace
		} else {
			index[k] = len(result)
			result = append(result, r)
		}
	}

	return result
}

func mergeIdleRules(base, override []IdleRule) []IdleRule {
	index := make(map[string]int)
	result := make([]IdleRule, 0, len(base)+len(override))

	for _, r := range base {
		index[idleRuleKey(r)] = len(result)
		result = append(result, r)
	}

	for _, r := range override {
		k := idleRuleKey(r)
		if idx, exists := index[k]; exists {
			result[idx] = r // replace
		} else {
			index[k] = len(result)
			result = append(result, r)
		}
	}

	return result
}
