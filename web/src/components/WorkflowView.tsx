import OverviewDashboard from './OverviewDashboard'
import StatusBar from './StatusBar'
import WorkflowDiagram from './WorkflowDiagram/WorkflowDiagram'
import PhaseHistoryBar from './PhaseHistoryBar'
import EventTimeline from './EventTimeline/EventTimeline'
import ActiveAgentsPanel from './ActiveAgentsPanel'
import type { WorkflowStatus, WorkflowTimeline, WorkflowListItem } from '@/types/api'
import type { WorkflowConfigDerived, PhaseColor } from '@/lib/phases'

interface Props {
  selectedWorkflowId: string | null
  selectedRunId: string | null
  status: WorkflowStatus | null
  timeline: WorkflowTimeline | null
  config: WorkflowConfigDerived | null
  now: number
  showAgents?: boolean
  workflows: WorkflowListItem[]
  phaseColors: Record<string, PhaseColor>
  phaseLabels: Record<string, string>
  onSelect: (workflowId: string, runId: string) => void
  onRefresh: () => void
  phaseIcons?: Record<string, string>
}

export default function WorkflowView({ selectedWorkflowId, selectedRunId, status, timeline, config, now, showAgents = true, workflows, phaseColors, phaseLabels, onSelect, onRefresh, phaseIcons }: Props) {
  const activeAgents = status?.active_agents || []
  const agentsPanelVisible = showAgents && activeAgents.length > 0
  const selectedWorkflow = workflows.find(w => w.workflow_id === selectedWorkflowId)

  if (!selectedWorkflowId) {
    return <OverviewDashboard workflows={workflows} phaseColors={phaseColors} phaseLabels={phaseLabels} phaseIcons={phaseIcons ?? {}} onSelect={onSelect} onRefresh={onRefresh} />
  }

  const isRunning = status?.phase !== 'COMPLETE' && status?.phase !== 'BLOCKED'

  const isFinished = status?.phase === 'COMPLETE'
  const sessionEndTime = isFinished && status?.last_updated_at
    ? new Date(status.last_updated_at).getTime()
    : now

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      {status && timeline && config ? (
        <>
          {/* StatusBar — full width */}
          <div className="flex-shrink-0">
            <StatusBar
              workflowId={selectedWorkflowId}
              runId={selectedRunId}
              status={status}
              timeline={timeline}
              config={config}
              isRunning={!!isRunning}
              now={now}
              projectName={selectedWorkflow?.project_name}
              repoUrl={selectedWorkflow?.repo_url}
            />
          </div>

          {/* Diagram + agents panel side-by-side */}
          <div className="flex flex-row flex-1 min-h-0 overflow-hidden">
            {/* Left: diagram + phase history */}
            <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
              <div className="flex-shrink-0 px-6 py-0 overflow-hidden border-b border-gray-700/50 bg-surface">
                <WorkflowDiagram
                  status={status}
                  timeline={timeline}
                  config={config}
                  now={now}
                />
              </div>
              <div className="flex-shrink-0">
                <PhaseHistoryBar
                  timeline={timeline}
                  config={config}
                  now={sessionEndTime}
                />
              </div>
              <EventTimeline timeline={timeline} workflowId={selectedWorkflowId} channelAvailable={status?.channel_available ?? false} />
            </div>

            {/* Right: active agents panel */}
            {agentsPanelVisible && (
              <div className="w-72 flex-shrink-0 border-l border-gray-700/50 overflow-hidden flex flex-col">
                <ActiveAgentsPanel
                  activeAgents={activeAgents}
                  commandsRan={status.commands_ran || {}}
                  requiredCategories={config.requiredCategories[status.phase] || []}
                  timeline={timeline}
                  now={now}
                />
              </div>
            )}
          </div>
        </>
      ) : (
        <div className="px-6 py-3 bg-surface border-b border-gray-700/50 flex items-center justify-between flex-shrink-0">
          <span className="text-sm text-gray-400">Loading workflow details...</span>
        </div>
      )}
    </div>
  )
}
