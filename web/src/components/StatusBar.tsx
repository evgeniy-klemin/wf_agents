import { useState } from 'react'
import type { WorkflowStatus, WorkflowTimeline } from '@/types/api'
import type { WorkflowConfigDerived, PhaseColor } from '@/lib/phases'
import { formatDuration, mrLabel } from '@/lib/formatting'
import { STUCK_THRESHOLD_MS } from '@/lib/stuck'
import { apiPost } from '@/lib/api'

interface Props {
  workflowId: string
  runId: string | null
  status: WorkflowStatus
  timeline: WorkflowTimeline
  config: WorkflowConfigDerived
  isRunning: boolean
  now: number
  projectName?: string
  repoUrl?: string
}

export default function StatusBar({ workflowId, runId, status, timeline, config, isRunning, now, projectName, repoUrl }: Props) {
  const { phaseColors, phaseLabels } = config
  const [copied, setCopied] = useState(false)

  const defaultColor: PhaseColor = { bg: '#334155', ring: '#475569', text: '#f1f5f9' }
  const pc = phaseColors[status.phase] || defaultColor

  const isBlocked = status.phase === 'BLOCKED'
  const phaseDisplay = isBlocked && status.pre_blocked_phase
    ? `BLOCKED (${phaseLabels[status.pre_blocked_phase] || status.pre_blocked_phase})`
    : phaseLabels[status.phase] || status.phase

  // Session duration from first timeline event
  const events = timeline.events || []
  const sessionDur = events.length > 0
    ? formatDuration(now - new Date(events[0].timestamp).getTime())
    : ''

  // Phase duration with client-side interpolation
  const phaseElapsedSecs = status.phase !== 'COMPLETE' && status.last_updated_at
    ? Math.max(0, now - new Date(status.last_updated_at).getTime()) / 1000
    : 0
  const phaseDurMs = (status.current_phase_secs + phaseElapsedSecs) * 1000
  const phaseDur = phaseDurMs > 0
    ? formatDuration(phaseDurMs)
    : ''

  // Stuck detection
  const lastUpdate = status.last_updated_at ? new Date(status.last_updated_at).getTime() : 0
  const idleMs = lastUpdate ? now - lastUpdate : 0
  const stuck = isRunning &&
    status.phase !== 'BLOCKED' &&
    status.phase !== 'COMPLETE' &&
    idleMs > STUCK_THRESHOLD_MS

  async function handleTerminate() {
    const id = encodeURIComponent(workflowId)
    const rid = runId ? encodeURIComponent(runId) : ''
    await apiPost(`/api/workflows/${id}/terminate${rid ? `?run_id=${rid}` : ''}`)
  }

  function handleCopySessionId() {
    navigator.clipboard.writeText(workflowId).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1000)
    })
  }

  const activeAgents = status.active_agents || []
  const totalItersShown = status.total_iterations && status.total_iterations !== status.iteration

  return (
    <>
    <div className="px-4 py-2 bg-surface border-b border-gray-700/50 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-gray-400 flex-shrink-0">
      {/* Phase badge */}
      <span
        className="px-2.5 py-0.5 rounded-full text-xs font-medium border"
        title={status.phase_reason || undefined}
        style={{
          backgroundColor: pc.bg + '33',
          color: pc.text,
          borderColor: pc.ring,
        }}
      >
        {phaseDisplay}
      </span>

      {/* Session duration */}
      {sessionDur && (
        <span className="flex items-center gap-1">
          <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <strong className="text-gray-200">{sessionDur}</strong>
        </span>
      )}

      {/* Phase duration */}
      {phaseDur && (
        <span className="flex items-center gap-1">
          <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          Phase: <strong className="text-gray-200">{phaseDur}</strong>
        </span>
      )}

      {/* Iteration counter */}
      <span>
        Iter: <strong className="text-gray-200">{status.iteration}</strong>
        {totalItersShown && (
          <span className="text-gray-400 text-xs ml-1">({status.total_iterations} total)</span>
        )}
      </span>

      {/* Event count */}
      <span>Events: <strong className="text-gray-200">{status.event_count}</strong></span>

      {/* Active agents */}
      <span className="flex items-center gap-1">
        <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0z" />
        </svg>
        <strong className={activeAgents.length > 0 ? 'text-green-400' : 'text-gray-200'}>
          {activeAgents.length}
        </strong>
      </span>


      {/* Project name */}
      {projectName && (
        <>
          {repoUrl ? (
            <a
              href={repoUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="text-accent-light hover:underline font-mono text-xs"
            >
              {projectName}
            </a>
          ) : (
            <span className="font-mono text-xs">{projectName}</span>
          )}
          <span className="text-gray-600">·</span>
        </>
      )}

      {/* Task name with copy session ID button */}
      {status.task && (
        <span className="flex items-center gap-1 max-w-xs">
          <span className="truncate" title={status.task}>{status.task}</span>
          <button
            onClick={handleCopySessionId}
            title="Copy session ID"
            className="flex-shrink-0 p-0.5 rounded hover:bg-gray-700/50 text-gray-500 hover:text-gray-300 transition-colors"
          >
            {copied ? (
              <svg className="w-3.5 h-3.5 text-green-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M5 13l4 4L19 7" />
              </svg>
            ) : (
              <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
              </svg>
            )}
          </button>
        </span>
      )}

      {/* MR link */}
      {status.mr_url && (
        <a
          href={status.mr_url}
          target="_blank"
          rel="noopener noreferrer"
          className="text-accent-light hover:underline font-mono text-xs"
        >
          {mrLabel(status.mr_url)}
        </a>
      )}

      {/* Stuck warning */}
      {stuck && (
        <>
          <span className="flex items-center gap-1.5 text-orange-400">
            <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            <strong>Stuck</strong> (idle {formatDuration(idleMs)})
          </span>
          <button
            onClick={handleTerminate}
            className="px-2 py-0.5 text-xs font-medium text-orange-300 bg-orange-900/30 hover:bg-orange-900/50 border border-orange-700/40 rounded transition-colors"
          >
            Terminate
          </button>
        </>
      )}
    </div>
    {isBlocked && (
      <div className="px-4 py-2 bg-red-900/30 border border-red-700/40 text-xs text-red-300 flex items-center gap-2">
        <span>⏸</span>
        <span>
          Paused from {status.pre_blocked_phase || 'unknown'}
          {status.phase_reason ? ` — ${status.phase_reason}` : ''}
        </span>
      </div>
    )}
    </>
  )
}
