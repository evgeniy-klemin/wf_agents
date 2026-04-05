import { useState } from 'react'
import type { WorkflowTimeline } from '@/types/api'
import TimelineFilters from './TimelineFilters'
import TimelineEvent from './TimelineEvent'
import { apiPostJSON } from '@/lib/api'

interface Props {
  timeline: WorkflowTimeline
  workflowId: string
  channelAvailable?: boolean
}

export default function EventTimeline({ timeline, workflowId, channelAvailable }: Props) {
  const [currentFilter, setCurrentFilter] = useState('all')
  const [message, setMessage] = useState('')
  const [sending, setSending] = useState(false)

  async function handleSend(e: React.FormEvent) {
    e.preventDefault()
    const text = message.trim()
    if (!text || sending) return
    setSending(true)
    try {
      await apiPostJSON(`/api/workflows/${encodeURIComponent(workflowId)}/message`, { message: text })
      setMessage('')
    } finally {
      setSending(false)
    }
  }

  const allEvents = timeline.events || []
  const events = allEvents.slice().reverse()
  const filtered = currentFilter === 'all'
    ? events
    : currentFilter === 'agent_spawn'
      ? events.filter(e => e.type === 'agent_spawn' || e.type === 'agent_stop')
      : events.filter(e => e.type === currentFilter)

  return (
    <div className="flex-1 border-t border-gray-700/50 flex flex-col overflow-hidden">
      <div className="px-6 py-2 flex items-center justify-between bg-surface-light flex-shrink-0">
        <h3 className="text-sm font-medium text-gray-400 uppercase tracking-wider">Event Timeline</h3>
        <TimelineFilters current={currentFilter} onChange={setCurrentFilter} />
      </div>
      <div className="flex-1 overflow-y-auto px-4 py-2 space-y-0.5 text-sm font-mono">
        {filtered.length === 0 ? (
          <div className="text-gray-500 text-center py-4">No events yet</div>
        ) : (
          filtered.map((ev, i) => (
            <TimelineEvent key={i} event={ev} />
          ))
        )}
        {timeline.total_events !== undefined && allEvents.length < timeline.total_events && (
          <div className="flex items-center gap-2 py-2 px-1 text-xs text-gray-500">
            <div className="w-3 h-3 rounded-full border-2 border-gray-600 border-t-gray-400 animate-spin flex-shrink-0" />
            Loading older events ({allEvents.length} of {timeline.total_events})...
          </div>
        )}
      </div>
      {channelAvailable && (
        <div className="flex-shrink-0 px-4 py-2 border-t border-gray-700/50 bg-surface">
          <form onSubmit={handleSend} className="flex gap-2">
            <input
              type="text"
              value={message}
              onChange={e => setMessage(e.target.value)}
              disabled={sending}
              placeholder="Send a message..."
              className="flex-1 bg-gray-800 border border-gray-700/50 rounded px-3 py-1.5 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-gray-500 disabled:opacity-50"
            />
            <button
              type="submit"
              disabled={sending || !message.trim()}
              className="px-3 py-1.5 text-sm bg-gray-700 hover:bg-gray-600 text-gray-200 rounded border border-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              Send
            </button>
          </form>
        </div>
      )}
    </div>
  )
}
