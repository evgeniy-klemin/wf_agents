package config

import (
	"sort"

	"github.com/eklemin/wf-agents/internal/model"
)

func computePhaseOrder(start string, phases map[string]PhaseConfig, transitions map[string][]TransitionConfig) []string {
	visited := make(map[string]bool)
	var order []string
	queue := []string{start}
	visited[start] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		order = append(order, current)

		for _, t := range transitions[current] {
			if !visited[t.To] {
				visited[t.To] = true
				queue = append(queue, t.To)
			}
		}
	}

	var orphans []string
	for name := range phases {
		if !visited[name] {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	order = append(order, orphans...)

	return order
}

// ExtractFlowSnapshot extracts the flow-relevant portion (phases + transitions)
// from a full Config. Permissions, guards, idle rules are excluded.
func ExtractFlowSnapshot(cfg *Config) *model.FlowSnapshot {
	if cfg == nil || cfg.Phases == nil {
		return nil
	}

	var phaseOrder []string
	if len(cfg.Phases.PhaseOrder) >= len(cfg.Phases.Phases) {
		phaseOrder = cfg.Phases.PhaseOrder
	} else {
		phaseOrder = computePhaseOrder(cfg.Phases.Start, cfg.Phases.Phases, cfg.Transitions)
	}

	snap := &model.FlowSnapshot{
		Start:       cfg.Phases.Start,
		Stop:        cfg.Phases.Stop,
		PhaseOrder:  phaseOrder,
		Phases:      make(map[string]model.FlowPhase, len(cfg.Phases.Phases)),
		Transitions: make(map[string][]model.FlowTransition, len(cfg.Transitions)),
	}

	for name, pc := range cfg.Phases.Phases {
		fp := model.FlowPhase{
			Display: model.FlowPhaseDisplay{
				Label: pc.Display.Label,
				Icon:  pc.Display.Icon,
				Color: pc.Display.Color,
			},
			Instructions: pc.Instructions,
			Hint:         pc.Hint,
		}
		for _, se := range pc.OnEnter {
			fp.OnEnter = append(fp.OnEnter, model.FlowSideEffect{Type: se.Type})
		}
		snap.Phases[name] = fp
	}

	for from, transitions := range cfg.Transitions {
		for _, t := range transitions {
			snap.Transitions[from] = append(snap.Transitions[from], model.FlowTransition{
				To:      t.To,
				Label:   t.Label,
				When:    t.When,
				Message: t.Message,
			})
		}
	}

	return snap
}
