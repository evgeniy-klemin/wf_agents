import WorkflowList from './WorkflowList'
import type { WorkflowListItem } from '@/types/api'
import type { PhaseColor } from '@/lib/phases'
import { statusColor } from '@/lib/formatting'
import { isStuck } from '@/lib/stuck'

interface Props {
  workflows: WorkflowListItem[]
  selectedWorkflowId: string | null
  phaseColors: Record<string, PhaseColor>
  phaseLabels: Record<string, string>
  onSelect: (workflowId: string, runId: string) => void
  onRefresh: () => void
  onHome: () => void
  collapsed: boolean
  onToggleCollapse: () => void
}

export default function Sidebar({
  workflows,
  selectedWorkflowId,
  phaseColors,
  phaseLabels,
  onSelect,
  onRefresh,
  onHome,
  collapsed,
  onToggleCollapse,
}: Props) {
  return (
    <aside className={`${collapsed ? 'w-12' : 'w-72'} bg-surface-light border-r border-gray-700/50 flex flex-col flex-shrink-0 transition-all duration-200`}>
      {/* Header row */}
      <div className="px-2 py-3 border-b border-gray-700/50 flex items-center justify-between flex-shrink-0">
        {!collapsed && (
          <div className="flex items-center gap-1 px-2">
            <h2 className="text-sm font-medium text-gray-400 uppercase tracking-wider">Workflows</h2>
            {selectedWorkflowId && (
              <button
                onClick={onHome}
                title="Back to overview (h)"
                className="p-1 rounded hover:bg-gray-700/50 text-gray-500 hover:text-gray-200 transition-colors"
              >
                <svg className="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6" />
                </svg>
              </button>
            )}
          </div>
        )}
        <button
          onClick={onToggleCollapse}
          className="ml-auto p-1 rounded hover:bg-gray-700/50 text-gray-400 hover:text-gray-200 transition-colors flex-shrink-0"
          title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            {collapsed ? (
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M9 5l7 7-7 7" />
            ) : (
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M15 19l-7-7 7-7" />
            )}
          </svg>
        </button>
      </div>

      {/* Content */}
      {collapsed ? (
        <div className="flex-1 overflow-y-auto py-2 flex flex-col items-center gap-2">
          {workflows.map(w => {
            const stuck = isStuck(w)
            const dotColor = statusColor(w.status === 'RUNNING' ? w.phase : null, stuck)
            const isSelected = selectedWorkflowId === w.workflow_id
            return (
              <button
                key={w.workflow_id}
                onClick={() => onSelect(w.workflow_id, w.run_id)}
                title={`${w.task || w.workflow_id} (${phaseLabels[w.phase] || w.phase})`}
                className={`w-6 h-6 rounded-full flex-shrink-0 border-2 transition-all ${isSelected ? 'scale-110' : 'opacity-70 hover:opacity-100'}`}
                style={{
                  backgroundColor: dotColor,
                  borderColor: isSelected ? dotColor : 'transparent',
                }}
              />
            )
          })}
        </div>
      ) : (
        <div className="flex-1 overflow-y-auto p-2">
          <WorkflowList
            workflows={workflows}
            selectedWorkflowId={selectedWorkflowId}
            phaseColors={phaseColors}
            phaseLabels={phaseLabels}
            onSelect={onSelect}
            onRefresh={onRefresh}
          />
        </div>
      )}
    </aside>
  )
}
