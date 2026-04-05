export interface WorkflowListItem {
  workflow_id: string
  run_id: string
  session_id: string
  status: string
  phase: string
  task: string
  mr_url: string
  start_time: string
  last_updated_at: string
  active_agents_count?: number
  project_name?: string
  repo_url?: string
}

export interface WorkflowStatus {
  phase: string
  iteration: number
  total_iterations: number
  active_agents: string[]
  event_count: number
  started_at: string
  last_updated_at: string
  task: string
  mr_url: string
  pre_blocked_phase: string
  phase_reason: string
  current_phase_secs: number
  phase_duration_secs: Record<string, number>
  current_iter_phase_secs: Record<string, number>
  commands_ran: Record<string, Record<string, boolean>>
  channel_available?: boolean
}

export interface WorkflowTimeline {
  events: WorkflowEvent[]
  total_events?: number
}

export interface WorkflowEvent {
  type: string
  timestamp: string
  session_id: string
  detail: Record<string, string>
}

export interface WorkflowConfigResponse {
  phases: PhasesConfig
  transitions: Record<string, TransitionConfig[]>
  // required_categories maps phase name to the command categories required before transition.
  // Derived from idle check rules (type: command_ran) in the workflow config.
  required_categories?: Record<string, string[]>
}

export interface PhasesConfig {
  start: string
  stop: string[]
  phase_order: string[]
  phases: Record<string, PhaseConfig>
}

export interface PhaseConfig {
  display: PhaseDisplay
  instructions?: string
  hint?: string
}

export interface PhaseDisplay {
  label?: string
  icon?: string
  color?: string
}

export interface TransitionConfig {
  to: string
  label?: string
  when?: string
  message?: string
}
