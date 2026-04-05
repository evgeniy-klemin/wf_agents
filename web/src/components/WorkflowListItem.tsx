import { apiPost } from '@/lib/api'
import { isStuck } from '@/lib/stuck'
import { timeAgo, truncate, mrLabel, statusColor } from '@/lib/formatting'
import type { WorkflowListItem as WorkflowListItemType } from '@/types/api'
import type { PhaseColor } from '@/lib/phases'

interface Props {
  workflow: WorkflowListItemType
  isSelected: boolean
  phaseColors: Record<string, PhaseColor>
  phaseLabels: Record<string, string>
  onSelect: (workflowId: string, runId: string) => void
  onTerminated: () => void
}

export default function WorkflowListItem({
  workflow: w,
  isSelected,
  phaseColors,
  phaseLabels,
  onSelect,
  onTerminated,
}: Props) {
  const stuck = isStuck(w)
  const isBlocked = w.phase === 'BLOCKED'

  const dotHex = statusColor(w.status === 'RUNNING' ? w.phase : null, stuck)
  const dotAnimate = stuck || isBlocked

  const ringClass = stuck
    ? 'ring-1 ring-orange-500/40'
    : isBlocked
    ? 'ring-1 ring-red-500/30'
    : ''

  const phaseColor = isBlocked
    ? '#ef4444'
    : stuck
    ? '#fb923c'
    : phaseColors[w.phase]?.ring ?? '#6b7280'

  const phaseLabel = phaseLabels[w.phase] ?? w.phase

  async function handleTerminate(e: React.MouseEvent) {
    e.stopPropagation()
    try {
      await apiPost(`/api/terminate/${encodeURIComponent(w.workflow_id)}`)
      onTerminated()
    } catch {
      // ignore
    }
  }

  return (
    <div
      className={`rounded-lg transition-colors ${ringClass} ${
        isSelected
          ? 'bg-accent/20 border border-accent/30'
          : 'hover:bg-surface-lighter border border-transparent'
      }`}
    >
      <button
        onClick={() => onSelect(w.workflow_id, w.run_id)}
        className="w-full text-left px-3 py-2.5"
      >
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium text-gray-200 truncate">
            {w.task ? truncate(w.task, 35) : w.session_id}
          </span>
          <span
            className={`flex-shrink-0 w-2 h-2 rounded-full${dotAnimate ? ' animate-pulse' : ''}`}
            style={{ backgroundColor: dotHex }}
          />
        </div>
        {w.task && (
          <div className="text-[10px] text-gray-500 truncate mt-0.5">{w.session_id}</div>
        )}
        <div className="flex items-center justify-between mt-0.5">
          <span className="text-xs text-gray-500">
            {w.start_time ? timeAgo(w.start_time) : 'Unknown'}
          </span>
          <div className="flex items-center gap-1.5">
            {stuck && (
              <span className="text-[10px] text-orange-400 font-medium">
                STUCK {timeAgo(w.last_updated_at)}
              </span>
            )}
            {w.phase && (
              <span className="text-xs" style={{ color: phaseColor }}>
                {phaseLabel}
              </span>
            )}
            {w.mr_url && (
              <a
                href={w.mr_url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-accent-light hover:underline font-mono text-[10px]"
                onClick={e => e.stopPropagation()}
              >
                {mrLabel(w.mr_url)}
              </a>
            )}
          </div>
        </div>
      </button>
      {stuck && w.status === 'RUNNING' && (
        <div className="px-3 pb-2">
          <button
            onClick={handleTerminate}
            className="w-full px-2 py-1 text-xs font-medium text-orange-300 bg-orange-900/30 hover:bg-orange-900/50 border border-orange-700/40 rounded transition-colors"
          >
            Terminate stuck session
          </button>
        </div>
      )}
    </div>
  )
}
