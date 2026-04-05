import { useState, useRef, useCallback } from 'react'
import { apiGet } from '@/lib/api'
import type { WorkflowEvent, WorkflowTimeline } from '@/types/api'

const BACKFILL_CHUNK = 500

const emptyState = () => ({ events: [] as WorkflowEvent[], totalEvents: 0 })

interface TimelineCacheResult {
  events: WorkflowEvent[]
  totalEvents: number
  fetchIncremental: () => Promise<void>
}

export function useTimelineCache(workflowId: string | null): TimelineCacheResult {
  const cacheRef = useRef<WorkflowEvent[]>([])
  const headOffset = useRef<number>(0)
  const totalRef = useRef<number>(0)
  const fetchInFlightRef = useRef<boolean>(false)
  const currentWorkflowId = useRef<string | null>(null)

  const [state, setState] = useState(emptyState())

  const fetchIncremental = useCallback(async () => {
    if (!workflowId) return

    // Reset on workflowId change
    if (currentWorkflowId.current !== workflowId) {
      currentWorkflowId.current = workflowId
      cacheRef.current = []
      headOffset.current = 0
      totalRef.current = 0
      fetchInFlightRef.current = false
      setState(emptyState())
    }

    // Dedup guard
    if (fetchInFlightRef.current) return
    fetchInFlightRef.current = true

    try {
      const id = encodeURIComponent(workflowId)
      const cache = cacheRef.current

      if (cache.length === 0) {
        // First poll — no after param, gets last 500
        const data = await apiGet<WorkflowTimeline>(`/api/workflows/${id}/timeline`)
        const newEvents = data.events ?? []
        const total = data.total_events ?? newEvents.length

        cacheRef.current = newEvents
        headOffset.current = Math.max(0, total - newEvents.length)
        totalRef.current = total
        setState({ events: newEvents, totalEvents: total })
      } else {
        // Subsequent poll — fetch only new events
        const afterIndex = headOffset.current + cache.length
        const data = await apiGet<WorkflowTimeline>(
          `/api/workflows/${id}/timeline?after=${afterIndex}`,
        )
        const newEvents = data.events ?? []
        const total = data.total_events ?? totalRef.current

        // Detect workflow restart
        if (total < headOffset.current + cache.length) {
          cacheRef.current = []
          headOffset.current = 0
          totalRef.current = 0
          setState(emptyState())
          return
        }

        const prevTotal = totalRef.current
        totalRef.current = total

        if (newEvents.length === 0 && total === prevTotal) {
          // No new events — skip setState
          return
        }

        const merged = [...cache, ...newEvents]
        cacheRef.current = merged
        setState({ events: merged, totalEvents: total })

        // Backfill: fetch missing earlier events one chunk at a time
        if (headOffset.current > 0) {
          const backfillFrom = Math.max(0, headOffset.current - BACKFILL_CHUNK)
          const backfillData = await apiGet<WorkflowTimeline>(
            `/api/workflows/${id}/timeline?after=${backfillFrom}`,
          )
          const backfillEvents = backfillData.events ?? []
          // Take only the slice we're missing
          const needed = backfillEvents.slice(0, headOffset.current - backfillFrom)
          if (needed.length > 0) {
            headOffset.current = backfillFrom
            const withBackfill = [...needed, ...cacheRef.current]
            cacheRef.current = withBackfill
            setState({ events: withBackfill, totalEvents: totalRef.current })
          }
        }
      }
    } finally {
      fetchInFlightRef.current = false
    }
  }, [workflowId])

  return { events: state.events, totalEvents: state.totalEvents, fetchIncremental }
}
