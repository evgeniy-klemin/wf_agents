package config

// MergeConfigs merges override on top of base, returning a new Config.
// Neither base nor override is mutated.
//
// Merge rules:
//   - tracking.lint/test: append then deduplicate (base entries first)
//   - guards: same from+to → append override checks to base checks;
//     disabled:true in override → remove all rules for that from+to pair
//   - teammate_idle: same match → replace; new match → append
func MergeConfigs(base, override *Config) *Config {
	result := &Config{}

	// Merge tracking
	result.Tracking = TrackingConfig{
		Lint: dedupStrings(base.Tracking.Lint, override.Tracking.Lint),
		Test: dedupStrings(base.Tracking.Test, override.Tracking.Test),
	}

	// Merge guards
	result.Guards = mergeGuards(base.Guards, override.Guards)

	// Merge teammate_idle
	result.TeammateIdle = mergeIdleRules(base.TeammateIdle, override.TeammateIdle)

	return result
}

func dedupStrings(base, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	result := make([]string, 0, len(base)+len(extra))
	for _, s := range append(base, extra...) {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
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
				// Append checks to existing rule
				existing := result[idx]
				existing.Checks = append(existing.Checks, r.Checks...)
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

func mergeIdleRules(base, override []IdleRule) []IdleRule {
	index := make(map[string]int)
	result := make([]IdleRule, 0, len(base)+len(override))

	for _, r := range base {
		index[r.Match] = len(result)
		result = append(result, r)
	}

	for _, r := range override {
		if idx, exists := index[r.Match]; exists {
			result[idx] = r // replace
		} else {
			index[r.Match] = len(result)
			result = append(result, r)
		}
	}

	return result
}
