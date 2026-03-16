package config

// MergeConfigs merges override on top of base, returning a new Config.
// Neither base nor override is mutated.
//
// Merge rules:
//   - tracking: same category key → override replaces base (patterns + flag); new keys append
//   - guards: same from+to → append override checks to base checks;
//     disabled:true in override → remove all rules for that from+to pair
//   - teammate_idle: same match → replace; new match → append
func MergeConfigs(base, override *Config) *Config {
	result := &Config{}

	// Merge tracking — override replaces by key, new keys append
	result.Tracking = mergeTracking(base.Tracking, override.Tracking)

	// Merge guards
	result.Guards = mergeGuards(base.Guards, override.Guards)

	// Merge teammate_idle
	result.TeammateIdle = mergeIdleRules(base.TeammateIdle, override.TeammateIdle)

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
			if !r.Disabled {
				index[k] = len(result)
				result = append(result, r)
			}
		}
	}

	// Filter out disabled placeholder entries
	filtered := result[:0]
	for _, r := range result {
		if !r.Disabled {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func idleRuleKey(r IdleRule) string {
	return r.Match + "|" + r.Agent
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
