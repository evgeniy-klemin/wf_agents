import React, { useState } from 'react'
import type { WorkflowTimeline } from '@/types/api'
import { categoryLabel, truncate } from '@/lib/formatting'

function agentIcon(agentName: string, className: string): React.ReactElement {
  const name = agentName.toLowerCase()

  if (name.includes('lead') || name.includes('team-lead')) {
    // Crown icon
    return (
      <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2"
          d="M5 16L3 6l5.5 5L12 4l3.5 7L21 6l-2 10H5z" />
      </svg>
    )
  }

  if (name.includes('developer')) {
    // Code brackets </> icon
    return (
      <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2"
          d="M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4" />
      </svg>
    )
  }

  if (name.includes('reviewer')) {
    // Magnifying glass icon
    return (
      <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2"
          d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
      </svg>
    )
  }

  if (name.includes('explorer') || name.includes('explore')) {
    // Compass icon
    return (
      <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <circle cx="12" cy="12" r="10" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2"
          d="M16.24 7.76l-2.12 6.36-6.36 2.12 2.12-6.36 6.36-2.12z" />
      </svg>
    )
  }

  if (name.includes('planner') || name.includes('plan')) {
    // Clipboard icon
    return (
      <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2"
          d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01" />
      </svg>
    )
  }

  // Default: generic user/robot icon
  return (
    <svg className={className} fill="none" viewBox="0 0 24 24" stroke="currentColor">
      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2"
        d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />
    </svg>
  )
}

interface ActiveAgentsPanelProps {
  activeAgents: string[]
  commandsRan: Record<string, Record<string, boolean>>
  requiredCategories: string[]
  timeline: WorkflowTimeline
  now: number
}

function formatUptime(ms: number): string {
  if (ms < 0) ms = 0
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  const rs = s % 60
  if (m < 60) return `${m}m ${rs}s`
  const h = Math.floor(m / 60)
  const rm = m % 60
  return `${h}h ${rm}m`
}

export default function ActiveAgentsPanel({ activeAgents, commandsRan, requiredCategories, timeline, now }: ActiveAgentsPanelProps) {
  const [collapsed, setCollapsed] = useState(false)

  const events = timeline?.events || []

  // Find spawn time for an agent by scanning timeline in reverse
  function getSpawnTime(agentName: string): number | null {
    for (let i = events.length - 1; i >= 0; i--) {
      const ev = events[i]
      if (ev.type === 'agent_spawn' && ev.detail?.agent_type === agentName) {
        return new Date(ev.timestamp).getTime()
      }
    }
    return null
  }

  // Find last tool use for a session
  function getLastActivity(sessionId: string): { tool: string; input: string } | null {
    for (let i = events.length - 1; i >= 0; i--) {
      const ev = events[i]
      if (ev.type === 'tool_use' && ev.session_id === sessionId) {
        return {
          tool: ev.detail?.tool_name || ev.detail?.tool || '',
          input: ev.detail?.input || ev.detail?.command || '',
        }
      }
    }
    return null
  }

  // Recently stopped agents (agent_stop events in last 5 minutes)
  const fiveMinAgo = now - 5 * 60 * 1000
  const recentlyStopped: Array<{ name: string; stoppedAt: number }> = []
  const seenStopped = new Set<string>()
  for (let i = events.length - 1; i >= 0; i--) {
    const ev = events[i]
    const ts = new Date(ev.timestamp).getTime()
    if (ts < fiveMinAgo) break
    if (ev.type === 'agent_stop') {
      const name = ev.detail?.agent_type || ev.session_id
      if (!seenStopped.has(name) && !activeAgents.includes(name)) {
        seenStopped.add(name)
        recentlyStopped.push({ name, stoppedAt: ts })
      }
    }
  }

  const hasContent = activeAgents.length > 0 || recentlyStopped.length > 0

  return (
    <div className="flex flex-col h-full bg-surface border-gray-700/50">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-gray-700/50 flex-shrink-0">
        <span className="flex items-center gap-1.5 text-xs font-medium text-gray-300">
          <svg className="w-4 h-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <rect x="3" y="8" width="18" height="11" rx="2" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 12h.01M15 12h.01M9 16h6M12 8V5m-3 0h6" />
          </svg>
          Active Agents ({activeAgents.length})
        </span>
        <button
          onClick={() => setCollapsed(c => !c)}
          className="text-gray-500 hover:text-gray-300 transition-colors"
          aria-label={collapsed ? 'Expand agents panel' : 'Collapse agents panel'}
        >
          <svg
            className={`w-4 h-4 transition-transform ${collapsed ? '-rotate-90' : ''}`}
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 9l-7 7-7-7" />
          </svg>
        </button>
      </div>

      {/* Body */}
      {!collapsed && (
        <div className="flex-1 overflow-y-auto px-3 py-2 space-y-3">
          {!hasContent && (
            <p className="text-xs text-gray-500 italic">No active agents</p>
          )}

          {/* Active agents */}
          {activeAgents.map(agent => {
            const spawnTs = getSpawnTime(agent)
            const uptime = spawnTs != null ? formatUptime(now - spawnTs) : null
            const cmds = commandsRan[agent] || {}
            // Merge: required categories not yet done by any agent + categories this agent ran
            const allDoneAcrossAgents = new Set(
              Object.values(commandsRan).flatMap(m => Object.entries(m).filter(([, v]) => v).map(([k]) => k))
            )
            const missingRequired = requiredCategories.filter(cat => !allDoneAcrossAgents.has(cat) && !(cmds[cat]))
            const categories: [string, boolean][] = [
              ...missingRequired.map(cat => [cat, false] as [string, boolean]),
              ...Object.entries(cmds),
            ]
            // Deduplicate: if a category appears in both missing and cmds, cmds wins
            const seen = new Set<string>()
            const deduped = categories.filter(([cat]) => {
              if (seen.has(cat)) return false
              seen.add(cat)
              return true
            })
            const visible = deduped.filter(([cat]) => cat !== '_file_changed')
            const lastActivity = getLastActivity(agent)

            return (
              <div key={agent} className="space-y-1.5">
                {/* Agent name + uptime */}
                <div className="flex items-center gap-1.5">
                  {agentIcon(agent, 'w-4 h-4 flex-shrink-0 text-green-400')}
                  <span className="text-xs font-medium text-gray-200 truncate">{agent}</span>
                  {uptime && (
                    <span className="ml-auto text-xs text-gray-500 flex-shrink-0">{uptime}</span>
                  )}
                </div>

                {/* Command category chips */}
                {visible.length > 0 && (
                  <div className="flex flex-wrap gap-1 pl-3.5">
                    {visible.map(([cat, done]) => (
                      <span
                        key={cat}
                        className={`text-xs px-1.5 py-0.5 rounded ${
                          done
                            ? 'text-green-400'
                            : 'text-gray-500'
                        }`}
                      >
                        {done ? '✔' : '○'} {categoryLabel(cat)}
                      </span>
                    ))}
                  </div>
                )}

                {/* Last activity */}
                {lastActivity && (lastActivity.tool || lastActivity.input) && (
                  <div className="pl-3.5 text-xs text-gray-500 truncate">
                    {lastActivity.tool && (
                      <span className="text-gray-400 mr-1">{lastActivity.tool}:</span>
                    )}
                    {truncate(lastActivity.input, 40)}
                  </div>
                )}

                <div className="border-t border-gray-700/30" />
              </div>
            )
          })}

          {/* Recently stopped */}
          {recentlyStopped.length > 0 && (
            <div className="space-y-1.5">
              <p className="text-xs text-gray-600 uppercase tracking-wide">Recently stopped</p>
              {recentlyStopped.map(({ name, stoppedAt }) => {
                const minsAgo = Math.floor((now - stoppedAt) / 60000)
                return (
                  <div key={name} className="flex items-center gap-1.5">
                    {agentIcon(name, 'w-4 h-4 flex-shrink-0 text-gray-600')}
                    <span className="text-xs text-gray-400 truncate">{name}</span>
                    <span className="ml-auto text-xs text-gray-600 flex-shrink-0">stopped {minsAgo}m ago</span>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
