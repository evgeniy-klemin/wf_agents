import WorkflowListItem from './WorkflowListItem'
import type { WorkflowListItem as WorkflowListItemType } from '@/types/api'
import type { PhaseColor } from '@/lib/phases'

interface Props {
  workflows: WorkflowListItemType[]
  selectedWorkflowId: string | null
  phaseColors: Record<string, PhaseColor>
  phaseLabels: Record<string, string>
  onSelect: (workflowId: string, runId: string) => void
  onRefresh: () => void
}

export default function WorkflowList({
  workflows,
  selectedWorkflowId,
  phaseColors,
  phaseLabels,
  onSelect,
  onRefresh,
}: Props) {
  if (workflows.length === 0) {
    return (
      <div className="text-sm text-gray-500 p-4 text-center">
        No workflows found.
        <br />
        <span className="text-xs">Start one with wf-client.</span>
      </div>
    )
  }

  return (
    <div className="space-y-1">
      {workflows.map(w => (
        <WorkflowListItem
          key={w.workflow_id}
          workflow={w}
          isSelected={selectedWorkflowId === w.workflow_id}
          phaseColors={phaseColors}
          phaseLabels={phaseLabels}
          onSelect={onSelect}
          onTerminated={onRefresh}
        />
      ))}
    </div>
  )
}
