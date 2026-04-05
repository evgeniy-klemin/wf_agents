import type { WorkflowEvent } from '@/types/api'
import { timeAgo } from '@/lib/formatting'
import { formatEventDetail } from './formatEvent'

interface EventIconInfo {
  color: string
  symbol: string
}

const EVENT_ICONS: Record<string, EventIconInfo> = {
  transition:  { color: '#6366f1', symbol: '→' },
  tool_use:    { color: '#3b82f6', symbol: '⚙' },
  hook_denial: { color: '#ef4444', symbol: '⛔' },
  agent_spawn: { color: '#22c55e', symbol: '▶' },
  agent_stop:  { color: '#f59e0b', symbol: '■' },
  journal:     { color: '#8b5cf6', symbol: '✎' },
  error:       { color: '#ef4444', symbol: '✖' },
}

interface Props {
  event: WorkflowEvent
}

export default function TimelineEvent({ event }: Props) {
  const info = EVENT_ICONS[event.type] || { color: '#64748b', symbol: '•' }
  const ts = event.timestamp ? new Date(event.timestamp).getTime() : 0
  const relTime = ts ? timeAgo(event.timestamp) : ''
  const absTime = ts ? new Date(ts).toLocaleTimeString() : ''

  return (
    <div
      className="flex items-start gap-3 px-2 py-1.5 rounded"
      style={{ transition: 'background-color 0.2s' }}
      onMouseEnter={e => (e.currentTarget.style.backgroundColor = 'rgba(99,102,241,0.08)')}
      onMouseLeave={e => (e.currentTarget.style.backgroundColor = '')}
    >
      <span className="flex-shrink-0 w-5 text-center" style={{ color: info.color }}>
        {info.symbol}
      </span>
      <span
        className="flex-shrink-0 w-16 text-gray-500 text-xs tabular-nums pt-0.5"
        title={absTime}
      >
        {relTime}
      </span>
      <span
        className="px-1.5 py-0.5 rounded text-xs font-medium whitespace-nowrap"
        style={{ background: info.color + '22', color: info.color }}
      >
        {event.type}
      </span>
      <span className="text-gray-300 text-xs break-all">
        {formatEventDetail(event)}
      </span>
    </div>
  )
}
