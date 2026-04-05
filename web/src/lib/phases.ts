import type { WorkflowConfigResponse, TransitionConfig } from '@/types/api'
import { ICON_PATHS, DEFAULT_ICON } from './icons'

export interface PhaseInfo {
  id: string
  label: string
  icon: string
}

export interface PhaseColor {
  bg: string
  ring: string
  text: string
}

export interface WorkflowConfigDerived {
  phases: PhaseInfo[]
  phaseColors: Record<string, PhaseColor>
  phaseLabels: Record<string, string>
  transitions: [string, string, string][]
  startPhase: string
  iterResetPhase: string
  requiredCategories: Record<string, string[]>
}

export function applyWorkflowConfig(cfg: WorkflowConfigResponse): WorkflowConfigDerived {
  const phasesMap = cfg.phases.phases
  const order = cfg.phases.phase_order || Object.keys(phasesMap)

  const phases: PhaseInfo[] = order.map(id => {
    const pc = phasesMap[id] || {}
    const display = pc.display || {}
    const iconName = display.icon || ''
    return {
      id,
      label: display.label || id,
      icon: ICON_PATHS[iconName] || DEFAULT_ICON,
    }
  })

  const phaseLabels: Record<string, string> = {
    BLOCKED: 'Blocked',
  }
  for (const id of order) {
    const display = (phasesMap[id] || {}).display || {}
    phaseLabels[id] = display.label || id
  }

  const phaseColors: Record<string, PhaseColor> = {
    BLOCKED: { bg: '#dc2626', ring: '#ef4444', text: '#fee2e2' },
  }
  for (const id of order) {
    const display = (phasesMap[id] || {}).display || {}
    if (display.color) {
      const hex = display.color
      phaseColors[id] = { bg: hex, ring: hex, text: '#f1f5f9' }
    }
  }

  const transitions: [string, string, string][] = []
  const transMap = cfg.transitions || {}
  for (const from of Object.keys(transMap)) {
    for (const t of transMap[from] as TransitionConfig[]) {
      transitions.push([from, t.to, t.label || ''])
    }
  }

  const startPhase = cfg.phases.start || (order[0] || '')

  // Ensure phases starts with startPhase
  if (startPhase && phases.length > 0 && phases[0].id !== startPhase) {
    const idx = phases.findIndex(p => p.id === startPhase)
    if (idx > 0) {
      const [sp] = phases.splice(idx, 1)
      phases.unshift(sp)
    }
  }

  const iterResetPhase = (phases[1] && phases[1].id) || ''

  const requiredCategories: Record<string, string[]> = cfg.required_categories ?? {}

  return { phases, phaseColors, phaseLabels, transitions, startPhase, iterResetPhase, requiredCategories }
}
