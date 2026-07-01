export type CompanionAdminRuntimePolicy = {
  org_id: string
  enabled: boolean
  kill_switch: boolean
  max_autonomy: string
  allowed_models?: string[]
  monthly_token_budget?: number
  monthly_tool_call_budget?: number
  control_plane?: {
    monthly_cost_budget_cents?: number
    max_risk_class?: string
    allowed_capabilities?: string[]
    denied_capabilities?: string[]
    embedding?: {
      provider?: string
      model?: string
      vector_store?: string
      dimensions?: number
      namespace_mode?: string
    }
    observability?: {
      trace_level?: string
      redaction_mode?: string
      replay_enabled?: boolean
    }
  }
}

export type CompanionAdminCapabilityRecord = {
  id: string
  status: 'draft' | 'active' | 'deprecated'
  source: 'generated' | 'imported'
  manifest: {
    capability_id: string
    version: string
    display_name: string
    risk_level: string
    side_effect_type: string
    approval_required: boolean
    cost_class: string
  }
}

export type CompanionAdminMemoryReview = {
  id: string
  memory_id?: string
  review_type: 'conflict' | 'correction' | 'invalidation' | 'deletion'
  status: 'open' | 'approved' | 'rejected' | 'applied' | 'cancelled'
  reason?: string
  created_at?: string
}

export type CompanionAdminSecurityEvalReport = {
  id: string
  suite: string
  status: 'passed' | 'failed'
  score: number
  threshold: number
  created_at?: string
}

export type CompanionAdminExecutionGraphEvent = {
  id: string
  org_id: string
  task_id: string
  step_id?: string
  event_type: string
  status?: string
  agent_id?: string
  capability_id?: string
  capability_version?: string
  job_id?: string
  nexus_decision_id?: string
  payload_json?: Record<string, unknown>
  created_at: string
}
