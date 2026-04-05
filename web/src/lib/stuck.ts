import type { WorkflowListItem } from '@/types/api'

export const STUCK_THRESHOLD_MS = 5 * 60 * 1000 // 5 minutes

export function isStuck(w: WorkflowListItem): boolean {
  if (w.status !== 'RUNNING') return false
  if (w.phase === 'BLOCKED' || w.phase === 'COMPLETE') return false
  if (!w.last_updated_at) return false
  const lastUpdate = new Date(w.last_updated_at).getTime()
  return (Date.now() - lastUpdate) > STUCK_THRESHOLD_MS
}

export function getGlobalPriorityStatus(workflows: WorkflowListItem[]): { phase: string | null, isStuck: boolean } {
  if (workflows.some(w => w.status === 'RUNNING' && w.phase === 'BLOCKED')) {
    return { phase: 'BLOCKED', isStuck: false }
  }
  if (workflows.some(w => isStuck(w))) {
    return { phase: 'ACTIVE', isStuck: true }
  }
  if (workflows.some(w => w.status === 'RUNNING' && w.phase !== 'COMPLETE' && w.phase !== 'BLOCKED')) {
    return { phase: 'ACTIVE', isStuck: false }
  }
  return { phase: null, isStuck: false }
}
