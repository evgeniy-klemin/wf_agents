import { useState, useEffect } from 'react'
import { apiGet } from '@/lib/api'
import { applyWorkflowConfig, type WorkflowConfigDerived } from '@/lib/phases'
import type { WorkflowStatus, WorkflowTimeline, WorkflowConfigResponse } from '@/types/api'
import { useTimelineCache } from './useTimelineCache'

interface StatusConfigState {
  status: WorkflowStatus | null
  config: WorkflowConfigDerived | null
}

const EMPTY_SC: StatusConfigState = { status: null, config: null }

export interface UseWorkflowDetailResult {
  status: WorkflowStatus | null
  timeline: WorkflowTimeline | null
  config: WorkflowConfigDerived | null
}

export function useWorkflowDetail(
  workflowId: string | null,
  globalConfig: WorkflowConfigDerived | null,
): UseWorkflowDetailResult {
  const [sc, setSc] = useState<StatusConfigState>(EMPTY_SC)
  const { events: cachedEvents, totalEvents, fetchIncremental } = useTimelineCache(workflowId)

  useEffect(() => {
    if (!workflowId) return

    let cancelled = false

    const doFetch = () => {
      const id = encodeURIComponent(workflowId)
      Promise.all([
        apiGet<WorkflowStatus>(`/api/workflows/${id}/status`),
        fetchIncremental(),
        apiGet<WorkflowConfigResponse>(`/api/workflows/${id}/config`).catch(() => null),
      ]).then(([status, , cfg]) => {
        if (cancelled) return
        setSc(prev => ({
          ...prev,
          status,
          config: cfg ? applyWorkflowConfig(cfg) : globalConfig,
        }))
      }).catch(() => {
        // keep stale data on error
      })
    }

    doFetch()
    const intervalId = setInterval(doFetch, 3000)
    return () => {
      cancelled = true
      clearInterval(intervalId)
    }
  }, [workflowId, globalConfig, fetchIncremental])

  if (!workflowId) {
    return { status: null, timeline: null, config: null }
  }

  return {
    status: sc.status,
    timeline: { events: cachedEvents, total_events: totalEvents },
    config: sc.config,
  }
}
