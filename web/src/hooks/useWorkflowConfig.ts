import { useState, useEffect } from 'react'
import { apiGet } from '@/lib/api'
import { applyWorkflowConfig, type WorkflowConfigDerived } from '@/lib/phases'
import type { WorkflowConfigResponse } from '@/types/api'

interface UseWorkflowConfigResult extends WorkflowConfigDerived {
  isLoaded: boolean
  error: string | null
}

export function useWorkflowConfig(): UseWorkflowConfigResult {
  const [result, setResult] = useState<UseWorkflowConfigResult>({
    phases: [],
    phaseColors: {},
    phaseLabels: {},
    transitions: [],
    startPhase: '',
    iterResetPhase: '',
    requiredCategories: {},
    isLoaded: false,
    error: null,
  })

  useEffect(() => {
    apiGet<WorkflowConfigResponse>('/api/workflow-config')
      .then(cfg => {
        const derived = applyWorkflowConfig(cfg)
        setResult({ ...derived, isLoaded: true, error: null })
      })
      .catch(err => {
        setResult(prev => ({ ...prev, isLoaded: true, error: String(err) }))
      })
  }, [])

  return result
}
