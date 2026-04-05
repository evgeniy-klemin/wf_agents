import { useState, useEffect, useCallback } from 'react'
import { apiGet } from '@/lib/api'
import type { WorkflowListItem } from '@/types/api'

interface UseWorkflowsResult {
  workflows: WorkflowListItem[]
  isConnected: boolean
  refresh: () => void
}

export function useWorkflows(): UseWorkflowsResult {
  const [workflows, setWorkflows] = useState<WorkflowListItem[]>([])
  const [isConnected, setIsConnected] = useState(false)
  const [tick, setTick] = useState(0)

  const refresh = useCallback(() => {
    setTick(t => t + 1)
  }, [])

  useEffect(() => {
    let cancelled = false

    const fetch = () => {
      apiGet<WorkflowListItem[]>('/api/workflows')
        .then(data => {
          if (!cancelled) {
            setWorkflows(data ?? [])
            setIsConnected(true)
          }
        })
        .catch(() => {
          if (!cancelled) setIsConnected(false)
        })
    }

    fetch()
    const id = setInterval(fetch, 3000)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [tick])

  return { workflows, isConnected, refresh }
}
