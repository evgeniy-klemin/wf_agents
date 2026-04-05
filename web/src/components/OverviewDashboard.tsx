import { useState } from 'react'
import { Clock, Users, ExternalLink, AlertTriangle } from 'lucide-react'
import { apiPost } from '@/lib/api'
import { isStuck } from '@/lib/stuck'
import { timeAgo, mrLabel, statusColor } from '@/lib/formatting'
import type { WorkflowListItem } from '@/types/api'
import type { PhaseColor } from '@/lib/phases'

interface Props {
  workflows: WorkflowListItem[]
  phaseColors: Record<string, PhaseColor>
  phaseLabels: Record<string, string>
  phaseIcons: Record<string, string>
  onSelect: (workflowId: string, runId: string) => void
  onRefresh: () => void
}

interface WorkflowCardProps {
  w: WorkflowListItem
  phaseColors: Record<string, PhaseColor>
  phaseLabels: Record<string, string>
  phaseIcons: Record<string, string>
  onSelect: (workflowId: string, runId: string) => void
  onRefresh: () => void
}

function WorkflowCard({ w, phaseColors, phaseLabels, phaseIcons, onSelect, onRefresh }: WorkflowCardProps) {
  const stuck = isStuck(w)
  const isBlocked = w.phase === 'BLOCKED'
  const isComplete = w.status !== 'RUNNING'

  const dotHex = statusColor(w.status === 'RUNNING' ? w.phase : null, stuck)

  const phaseColor = isBlocked
    ? '#ef4444'
    : stuck
    ? '#fb923c'
    : phaseColors[w.phase]?.ring ?? '#6b7280'

  const phaseLabel = phaseLabels[w.phase] ?? w.phase
  const phaseIconPath = phaseIcons[w.phase] ?? null

  // Left accent stripe color based on status
  const accentColor = stuck
    ? '#f97316'
    : isBlocked
    ? '#ef4444'
    : isComplete
    ? '#374151'
    : phaseColors[w.phase]?.ring ?? '#22c55e'

  async function handleTerminate(e: React.MouseEvent) {
    e.stopPropagation()
    try {
      await apiPost(`/api/terminate/${encodeURIComponent(w.workflow_id)}`)
      onRefresh()
    } catch {
      // ignore
    }
  }

  return (
    <div
      className="group rounded-xl border border-gray-700/50 bg-surface-light hover:bg-surface-lighter hover:border-gray-600/60 hover:shadow-lg hover:shadow-black/20 hover:scale-[1.02] transition-all duration-150 cursor-pointer flex flex-col min-h-[160px] overflow-hidden relative"
      style={{ borderLeft: `3px solid ${accentColor}` }}
      onClick={() => onSelect(w.workflow_id, w.run_id)}
    >
      {/* Phase icon watermark */}
      {phaseIconPath && (
        <div className="absolute bottom-2 right-2 pointer-events-none" style={{ opacity: 0.06 }}>
          <svg className="w-16 h-16" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d={phaseIconPath} />
          </svg>
        </div>
      )}

      <div className="px-4 pt-4 pb-3 flex flex-col flex-1 gap-3">
        {/* Header: status dot + task name */}
        <div className="flex items-start gap-2.5">
          <span
            className={`flex-shrink-0 w-2 h-2 rounded-full mt-1.5${stuck || isBlocked ? ' animate-pulse' : ''}`}
            style={{ backgroundColor: dotHex }}
          />
          <span className="text-base font-semibold text-gray-100 leading-snug line-clamp-3 flex-1">
            {w.task ? w.task : w.session_id}
          </span>
        </div>

        {/* Phase badge + stuck indicator */}
        {w.phase && (
          <div className="flex items-center gap-2 flex-wrap">
            <span
              className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium"
              style={{ color: phaseColor, backgroundColor: phaseColor + '22' }}
            >
              {phaseIconPath && (
                <svg className="w-3 h-3 flex-shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d={phaseIconPath} />
                </svg>
              )}
              {phaseLabel}
            </span>
            {stuck && (
              <span className="inline-flex items-center gap-1 text-[11px] font-medium text-orange-400">
                <AlertTriangle className="w-3 h-3" />
                idle {timeAgo(w.last_updated_at)}
              </span>
            )}
          </div>
        )}

        {/* Stats row at bottom */}
        <div className="mt-auto flex items-center text-[11px] text-gray-400">
          {w.project_name && (
            <>
              {w.repo_url ? (
                <a
                  href={w.repo_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-accent-light hover:text-white hover:underline font-mono transition-colors"
                  onClick={e => e.stopPropagation()}
                >
                  {w.project_name}
                </a>
              ) : (
                <span className="font-mono">{w.project_name}</span>
              )}
              <span className="mx-1.5 text-gray-600">·</span>
            </>
          )}
          {w.start_time && (
            <span className="flex items-center gap-1">
              <Clock className="w-3 h-3 text-gray-500" />
              {timeAgo(w.start_time)}
            </span>
          )}
          {w.active_agents_count != null && w.active_agents_count > 0 && (
            <>
              <span className="mx-1.5 text-gray-600">·</span>
              <span className="flex items-center gap-1 text-green-400">
                <Users className="w-3 h-3" />
                {w.active_agents_count}
              </span>
            </>
          )}
          {w.mr_url && (
            <>
              <span className="mx-1.5 text-gray-600">·</span>
              <a
                href={w.mr_url}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-1 text-accent-light hover:text-white hover:underline font-mono transition-colors"
                onClick={e => e.stopPropagation()}
              >
                <ExternalLink className="w-3 h-3" />
                {mrLabel(w.mr_url)}
              </a>
            </>
          )}
          {w.task && (
            <span className="ml-auto text-[9px] text-gray-600 truncate max-w-[80px]" title={w.session_id}>
              {w.session_id}
            </span>
          )}
        </div>
      </div>

      {stuck && w.status === 'RUNNING' && (
        <div className="px-4 pb-3">
          <button
            onClick={handleTerminate}
            className="w-full px-2 py-1.5 text-xs font-medium text-orange-300 bg-orange-900/30 hover:bg-orange-900/50 border border-orange-700/40 rounded-lg transition-colors"
          >
            Terminate stuck session
          </button>
        </div>
      )}
    </div>
  )
}

interface SectionProps {
  title: string
  count: number
  accentColor: string
  children: React.ReactNode
  defaultCollapsed?: boolean
}

function Section({ title, count, accentColor, children, defaultCollapsed = false }: SectionProps) {
  const [collapsed, setCollapsed] = useState(defaultCollapsed)

  return (
    <div className="mb-8">
      <button
        className="flex items-center gap-3 mb-4 w-full text-left group"
        onClick={() => setCollapsed(v => !v)}
      >
        <span className="w-2.5 h-2.5 rounded-full flex-shrink-0" style={{ backgroundColor: accentColor }} />
        <span className="text-sm font-semibold text-gray-200 tracking-wide">{title}</span>
        <span
          className="text-[10px] font-semibold px-2 py-0.5 rounded-full"
          style={{ color: accentColor, backgroundColor: accentColor + '22' }}
        >
          {count}
        </span>
        <div className="flex-1 h-px bg-gray-700/50 ml-1" />
        <span className="text-gray-600 text-xs group-hover:text-gray-400 transition-colors ml-1">
          {collapsed ? '▸' : '▾'}
        </span>
      </button>
      {!collapsed && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {children}
        </div>
      )}
    </div>
  )
}

export default function OverviewDashboard({ workflows, phaseColors, phaseLabels, phaseIcons, onSelect, onRefresh }: Props) {
  const active = workflows.filter(w => w.status === 'RUNNING' && !isStuck(w) && w.phase !== 'BLOCKED')
  const needsAttention = workflows.filter(w => w.phase === 'BLOCKED' || isStuck(w))
  const completed = workflows.filter(w => w.status !== 'RUNNING')

  if (workflows.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center">
          <svg className="w-12 h-12 text-gray-600 mx-auto mb-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1} d="M9 17V7m0 10a2 2 0 01-2 2H5a2 2 0 01-2-2V7a2 2 0 012-2h2a2 2 0 012 2m0 10a2 2 0 002 2h2a2 2 0 002-2M9 7a2 2 0 012-2h2a2 2 0 012 2m0 10V7m0 10a2 2 0 002 2h2a2 2 0 002-2V7a2 2 0 00-2-2h-2a2 2 0 00-2 2" />
          </svg>
          <p className="text-gray-500 text-sm">No workflows found</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 overflow-auto">
      {/* Summary bar */}
      <div className="flex items-center gap-3 px-6 py-3 bg-surface border-b border-gray-700/50 flex-shrink-0">
        <span className="text-xs text-gray-500 font-medium uppercase tracking-wider">Overview</span>
        <div className="flex items-center gap-2 ml-1">
          {needsAttention.length > 0 && (
            <span className="flex items-center gap-1.5 text-xs font-medium text-orange-300 bg-orange-900/30 border border-orange-700/40 px-2.5 py-0.5 rounded-full">
              <AlertTriangle className="w-3 h-3" />
              {needsAttention.length} attention
            </span>
          )}
          <span className="flex items-center gap-1.5 text-xs font-medium text-green-300 bg-green-900/20 border border-green-700/30 px-2.5 py-0.5 rounded-full">
            <span className="w-1.5 h-1.5 rounded-full bg-green-400" />
            {active.length} active
          </span>
          <span className="flex items-center gap-1.5 text-xs font-medium text-gray-400 bg-gray-700/30 border border-gray-700/50 px-2.5 py-0.5 rounded-full">
            <span className="w-1.5 h-1.5 rounded-full bg-gray-500" />
            {completed.length} done
          </span>
        </div>
      </div>

      <div className="px-6 py-6">
        {needsAttention.length > 0 && (
          <Section title="Needs Attention" count={needsAttention.length} accentColor="#f97316">
            {needsAttention.map(w => (
              <WorkflowCard
                key={w.workflow_id}
                w={w}
                phaseColors={phaseColors}
                phaseLabels={phaseLabels}
                phaseIcons={phaseIcons}
                onSelect={onSelect}
                onRefresh={onRefresh}
              />
            ))}
          </Section>
        )}

        {active.length > 0 && (
          <Section title="Active" count={active.length} accentColor="#22c55e">
            {active.map(w => (
              <WorkflowCard
                key={w.workflow_id}
                w={w}
                phaseColors={phaseColors}
                phaseLabels={phaseLabels}
                phaseIcons={phaseIcons}
                onSelect={onSelect}
                onRefresh={onRefresh}
              />
            ))}
          </Section>
        )}

        {completed.length > 0 && (
          <Section title="Completed" count={completed.length} accentColor="#6b7280" defaultCollapsed>
            {completed.map(w => (
              <WorkflowCard
                key={w.workflow_id}
                w={w}
                phaseColors={phaseColors}
                phaseLabels={phaseLabels}
                phaseIcons={phaseIcons}
                onSelect={onSelect}
                onRefresh={onRefresh}
              />
            ))}
          </Section>
        )}
      </div>
    </div>
  )
}
