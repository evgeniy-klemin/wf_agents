import type { WorkflowStatus, WorkflowTimeline } from '@/types/api'
import type { WorkflowConfigDerived, PhaseColor } from '@/lib/phases'
import { computeLayout } from './layout'
import DiagramDefs from './DiagramDefs'
import DiagramEdge from './DiagramEdge'
import DiagramNode from './DiagramNode'

interface Props {
  status: WorkflowStatus
  timeline: WorkflowTimeline
  config: WorkflowConfigDerived
  now: number
}

function computePhaseDurationsFromTimeline(
  timeline: WorkflowTimeline,
  endTime: number,
): Record<string, number> {
  const events = timeline.events || []
  const durations: Record<string, number> = {}
  let currentPhase: string | null = null
  let phaseStart: number | null = null
  const now = endTime

  for (const ev of events) {
    if (ev.type === 'transition' && ev.detail) {
      const ts = new Date(ev.timestamp).getTime()
      if (currentPhase && phaseStart) {
        durations[currentPhase] = (durations[currentPhase] || 0) + (ts - phaseStart)
      }
      currentPhase = ev.detail.to || null
      phaseStart = ts
    }
  }
  if (currentPhase && phaseStart) {
    durations[currentPhase] = (durations[currentPhase] || 0) + (now - phaseStart)
  }
  return durations
}

const BLOCKED_COLORS: PhaseColor = { bg: '#dc2626', ring: '#ef4444', text: '#fee2e2' }

export default function WorkflowDiagram({ status, timeline, config, now }: Props) {
  const { phases, phaseColors, transitions } = config

  const serverDurations = status.phase_duration_secs
  let phaseDurations: Record<string, number> = serverDurations ? { ...serverDurations } : (() => {
    const clientMs = computePhaseDurationsFromTimeline(timeline, now)
    const clientSecs: Record<string, number> = {}
    for (const [k, v] of Object.entries(clientMs)) { clientSecs[k] = v / 1000 }
    return clientSecs
  })()

  let iterDurations: Record<string, number> = { ...(status.current_iter_phase_secs || {}) }
  const hasMultipleIters = (status.iteration || 1) > 1

  const currentPhase = status.phase
  const isBlocked = currentPhase === 'BLOCKED'
  const preBlockedPhase = status.pre_blocked_phase

  // Client-side interpolation: add elapsed time since last server update for the current phase
  // Only when serverDurations is present — the timeline fallback already extends to `now`
  if (serverDurations && currentPhase !== 'COMPLETE' && status.last_updated_at) {
    const activePhase = isBlocked ? 'BLOCKED' : currentPhase
    const elapsedSecs = Math.max(0, now - new Date(status.last_updated_at).getTime()) / 1000
    phaseDurations = { ...phaseDurations, [activePhase]: (phaseDurations[activePhase] || 0) + elapsedSecs }
    iterDurations = { ...iterDurations, [activePhase]: (iterDurations[activePhase] || 0) + elapsedSecs }
  }

  const mainOrder = phases.map(p => p.id)
  let currentIdx = mainOrder.indexOf(currentPhase)
  if (isBlocked && preBlockedPhase) {
    currentIdx = mainOrder.indexOf(preBlockedPhase)
  }

  const layout = computeLayout(phases, transitions, hasMultipleIters)
  const { positions, nodeW, nodeH, perRow, viewBoxX, viewBoxWidth, viewBoxHeight, numRows } = layout

  // Pre-classify transitions for depth staggering
  const forwardSkipTransitions: { fromId: string; toId: string; label: string; fromIdx: number; toIdx: number }[] = []
  const backwardTransitions: { fromId: string; toId: string; label: string; fromIdx: number; toIdx: number }[] = []

  for (const [fromId, toId, label] of transitions) {
    if (!positions[fromId] || !positions[toId]) continue
    const fi = mainOrder.indexOf(fromId)
    const ti = mainOrder.indexOf(toId)
    if (fi < 0 || ti < 0) continue
    if (ti > fi + 1) {
      forwardSkipTransitions.push({ fromId, toId, label, fromIdx: fi, toIdx: ti })
    } else if (ti < fi) {
      backwardTransitions.push({ fromId, toId, label, fromIdx: fi, toIdx: ti })
    }
  }

  forwardSkipTransitions.sort((a, b) => (b.toIdx - b.fromIdx) - (a.toIdx - a.fromIdx))
  backwardTransitions.sort((a, b) => (a.fromIdx - a.toIdx) - (b.fromIdx - b.toIdx))

  const fwdSkipDepthMap = new Map<string, number>()
  const bwdDepthMap = new Map<string, number>()
  forwardSkipTransitions.forEach((t, i) => { fwdSkipDepthMap.set(`${t.fromId}->${t.toId}`, i) })
  backwardTransitions.forEach((t, i) => { bwdDepthMap.set(`${t.fromId}->${t.toId}`, i) })

  const BASE_DEPTH = 40
  const DEPTH_STEP = 20

  const defaultColor: PhaseColor = { bg: '#334155', ring: '#475569', text: '#f1f5f9' }

  return (
    <svg
      viewBox={`${viewBoxX} 0 ${viewBoxWidth} ${viewBoxHeight}`}
      className="w-full max-w-7xl mx-auto"
      style={{ minWidth: '1000px', maxHeight: numRows === 1 ? '200px' : '330px', display: 'block' }}
    >
      <DiagramDefs />

      {transitions.map(([fromId, toId, label]) => {
        if (!positions[fromId] || !positions[toId]) return null
        const fi = mainOrder.indexOf(fromId)
        const ti = mainOrder.indexOf(toId)
        if (fi < 0 || ti < 0) return null

        let edgeType: 'forward-adjacent' | 'forward-skip' | 'backward'
        let depthIdx = 0

        if (ti < fi) {
          edgeType = 'backward'
          depthIdx = bwdDepthMap.get(`${fromId}->${toId}`) || 0
        } else if (ti === fi + 1) {
          edgeType = 'forward-adjacent'
        } else {
          edgeType = 'forward-skip'
          depthIdx = fwdSkipDepthMap.get(`${fromId}->${toId}`) || 0
        }

        return (
          <DiagramEdge
            key={`${fromId}->${toId}`}
            label={label}
            from={positions[fromId]}
            to={positions[toId]}
            fromIdx={fi}
            toIdx={ti}
            currentIdx={currentIdx}
            nodeW={nodeW}
            nodeH={nodeH}
            perRow={perRow}
            depthIdx={depthIdx}
            baseDepth={BASE_DEPTH}
            depthStep={DEPTH_STEP}
            edgeType={edgeType}
          />
        )
      })}

      {phases.map((phase, i) => {
        const pos = positions[phase.id]
        if (!pos) return null
        const pc = phaseColors[phase.id] || defaultColor
        const isThisBlocked = isBlocked && preBlockedPhase === phase.id
        const isCurrent = isThisBlocked || phase.id === currentPhase
        const isPast = currentIdx > i && !isCurrent
        const isFuture = !isCurrent && !isPast

        const totalDurSecs =
          (phaseDurations[phase.id] || 0) +
          (isThisBlocked ? (phaseDurations['BLOCKED'] || 0) : 0) || undefined
        const iterDurSecs =
          (iterDurations[phase.id] || 0) +
          (isThisBlocked ? (iterDurations['BLOCKED'] || 0) : 0) || undefined

        return (
          <DiagramNode
            key={phase.id}
            phase={phase}
            colors={pc}
            x={pos.x}
            y={pos.y}
            w={nodeW}
            h={nodeH}
            isCurrent={isCurrent}
            isPast={isPast}
            isFuture={isFuture}
            isBlocked={isThisBlocked}
            totalDurSecs={totalDurSecs}
            iterDurSecs={iterDurSecs}
            hasMultipleIters={hasMultipleIters}
            blockedColors={BLOCKED_COLORS}
          />
        )
      })}
    </svg>
  )
}
