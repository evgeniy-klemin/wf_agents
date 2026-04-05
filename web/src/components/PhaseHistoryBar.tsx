import { useState } from 'react'
import type { WorkflowTimeline } from '@/types/api'
import type { WorkflowConfigDerived } from '@/lib/phases'
import { formatDuration } from '@/lib/formatting'
import PhaseTooltip, { type PhaseTooltipData } from './PhaseTooltip'

interface Segment {
  phase: string
  startMs: number
  durationMs: number
}

function computePhaseSegments(
  timeline: WorkflowTimeline,
  endTime: number | null,
  startPhase: string,
): { segments: Segment[]; sessionStart: number | null } {
  const events = timeline.events || []
  const segments: Segment[] = []
  let currentPhase: string | null = null
  let phaseStart: number | null = null
  let sessionStart: number | null = null
  const now = endTime || Date.now()

  for (const ev of events) {
    if (ev.type === 'transition' && ev.detail) {
      const ts = new Date(ev.timestamp).getTime()
      if (!sessionStart) sessionStart = ts

      if (currentPhase && phaseStart !== null) {
        segments.push({ phase: currentPhase, startMs: phaseStart - sessionStart, durationMs: ts - phaseStart })
      }
      currentPhase = ev.detail.to
      phaseStart = ts
    }
  }

  if (currentPhase && phaseStart !== null && sessionStart !== null) {
    segments.push({ phase: currentPhase, startMs: phaseStart - sessionStart, durationMs: now - phaseStart })
  }

  if (!currentPhase && events.length > 0) {
    sessionStart = new Date(events[0].timestamp).getTime()
    const phase = startPhase || 'PLANNING'
    segments.push({ phase, startMs: 0, durationMs: now - sessionStart })
  }

  return { segments, sessionStart }
}

interface Props {
  timeline: WorkflowTimeline
  config: WorkflowConfigDerived
  now: number
}

export default function PhaseHistoryBar({ timeline, config, now }: Props) {
  const { phaseColors, phaseLabels, startPhase, iterResetPhase } = config

  const [tooltip, setTooltip] = useState<PhaseTooltipData | null>(null)
  const [mousePos, setMousePos] = useState({ x: 0, y: 0 })

  const { segments } = computePhaseSegments(timeline, now, startPhase)

  if (segments.length === 0) return null

  const totalMs = segments.reduce((sum, s) => sum + s.durationMs, 0)
  if (totalMs === 0) return null

  const totalWork = segments.filter(s => s.phase !== 'BLOCKED').reduce((sum, s) => sum + s.durationMs, 0)
  const blockedSegments = segments.filter(s => s.phase === 'BLOCKED')
  const totalBlocked = blockedSegments.reduce((sum, s) => sum + s.durationMs, 0)

  // Iteration boundaries
  let iterNum = 0
  const iterBoundaries: { pct: number; label: string }[] = []
  let cumMs = 0
  for (const s of segments) {
    if (s.phase === iterResetPhase && iterResetPhase && cumMs > 0) {
      iterNum++
      iterBoundaries.push({ pct: (cumMs / totalMs) * 100, label: `Iter ${iterNum}` })
    }
    cumMs += s.durationMs
  }

  const segData = segments.map(s => {
    const colors = phaseColors[s.phase] || { bg: '#475569', text: '#e2e8f0' }
    return {
      phase: s.phase,
      name: phaseLabels[s.phase] || s.phase,
      dur: formatDuration(s.durationMs),
      pct: ((s.durationMs / totalMs) * 100).toFixed(1),
      color: colors.bg,
    }
  })

  return (
    <div className="px-6 py-2 border-t border-gray-700/50 flex-shrink-0">
      {/* Summary line */}
      <div className="flex items-center gap-3 mb-1">
        <h3 className="text-xs font-medium text-gray-500 uppercase tracking-wider">Phase History</h3>
        <span className="text-xs text-gray-600">Active: {formatDuration(totalWork)}</span>
        {totalBlocked > 0 && (
          <span className="flex items-center gap-1 text-xs text-red-400">
            <svg className="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            {formatDuration(totalBlocked)} blocked ({blockedSegments.length}x)
          </span>
        )}
      </div>

      {/* Bar */}
      <div className="relative mt-4">
        <div className="flex h-7 rounded-lg overflow-hidden border border-gray-700/50">
          {timeline.total_events !== undefined && timeline.events.length < timeline.total_events && (
            <div
              className="h-full flex-shrink-0 relative overflow-hidden cursor-default"
              style={{
                width: '12px',
                background: 'repeating-linear-gradient(135deg,#374151,#374151 3px,#1f2937 3px,#1f2937 6px)',
              }}
              title="Loading older events..."
            >
              <div className="absolute inset-0 animate-pulse bg-gray-500/20" />
            </div>
          )}
          {segments.map((s, idx) => {
            const pct = (s.durationMs / totalMs) * 100
            const isBlocked = s.phase === 'BLOCKED'
            const colors = phaseColors[s.phase] || { bg: '#475569', text: '#e2e8f0' }
            const bg = isBlocked
              ? `repeating-linear-gradient(135deg,${colors.bg},${colors.bg} 3px,${colors.bg}99 3px,${colors.bg}99 6px)`
              : colors.bg

            return (
              <div
                key={idx}
                className="h-full flex flex-col items-center justify-center overflow-hidden cursor-default relative"
                style={{
                  width: `${Math.max(pct, 0.3)}%`,
                  background: bg,
                }}
                onMouseEnter={() => setTooltip(segData[idx])}
                onMouseMove={e => setMousePos({ x: e.clientX, y: e.clientY })}
                onMouseLeave={() => setTooltip(null)}
              >
                {pct > 8 && (
                  <span
                    className="text-[10px] font-medium truncate px-1 pointer-events-none"
                    style={{ color: colors.text }}
                  >
                    {phaseLabels[s.phase] || s.phase}
                  </span>
                )}
                {pct > 5 && (
                  <span
                    className="text-[9px] opacity-70 px-1 pointer-events-none"
                    style={{ color: colors.text }}
                  >
                    {formatDuration(s.durationMs)}
                  </span>
                )}
              </div>
            )
          })}
        </div>

        {/* Iteration boundary markers */}
        {iterBoundaries.map((b, i) => (
          <div
            key={i}
            className="absolute top-0 bottom-0 border-l-2 border-yellow-500/60"
            style={{ left: `${b.pct}%` }}
          >
            <span className="absolute -top-4 left-1 text-[9px] text-yellow-500 font-medium whitespace-nowrap">
              {b.label}
            </span>
          </div>
        ))}
      </div>

      <PhaseTooltip data={tooltip} x={mousePos.x} y={mousePos.y} />
    </div>
  )
}
