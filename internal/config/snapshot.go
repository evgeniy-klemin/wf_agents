package config

import "github.com/eklemin/wf-agents/internal/model"

// ExtractFlowSnapshot extracts the flow-relevant portion (phases + transitions)
// from a full Config. Permissions, guards, idle rules are excluded.
func ExtractFlowSnapshot(cfg *Config) *model.FlowSnapshot {
	if cfg == nil || cfg.Phases == nil {
		return nil
	}

	snap := &model.FlowSnapshot{
		Start:       cfg.Phases.Start,
		Stop:        cfg.Phases.Stop,
		PhaseOrder:  cfg.Phases.PhaseOrder,
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
