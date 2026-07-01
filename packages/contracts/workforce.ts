export type UUID = string
export type ISODateTime = string

export type VirployeeStatus =
  | 'draft'
  | 'active'
  | 'disabled'
  | 'suspended'
  | 'archived'
  | 'trashed'
  | 'error'

export type AutonomyLevel = 'A0' | 'A1' | 'A2' | 'A3' | 'A4' | 'A5'

export type JobRoleStatus = 'active' | 'archived'
export type VirployeeProfileStatus = 'draft' | 'active' | 'disabled' | 'archived' | 'trashed'
export type MemoryStatus = 'active' | 'disabled' | 'archived'
export type TaskStatus = 'open' | 'assigned' | 'running' | 'blocked' | 'done' | 'cancelled'
export type WatcherStatus = 'active' | 'paused' | 'archived'
export type HandoffStatus = 'pending' | 'accepted' | 'rejected' | 'cancelled'
export type CapabilityMode = 'read' | 'write' | 'execute'
export type RiskClass = 'low' | 'medium' | 'high' | 'critical'

export type Virployee = {
  virployee_id: UUID
  tenant_id: UUID
  name: string
  supervisor_user_id: UUID
  status: VirployeeStatus
  job_role_id: UUID
  profile_id: UUID
  autonomy: AutonomyLevel
  capability_ids: UUID[]
  memory_id?: UUID | null
}

export type Responsibility = {
  title: string
  description?: string
  expected_outcome?: string
  priority: number
}

export type SuccessCriterion = {
  title: string
  description?: string
  target_value?: string
  priority: number
}

export type JobRole = {
  job_role_id: UUID
  tenant_id: UUID
  name: string
  slug: string
  mission: string
  responsibilities: Responsibility[]
  success_criteria: SuccessCriterion[]
  recommended_capability_ids: UUID[]
  default_autonomy: AutonomyLevel
  status: JobRoleStatus
}

export type LLMConfig = {
  provider: string
  model: string
  temperature: number
  max_tokens: number
}

export type MemoryPolicy = {
  enabled_by_default: boolean
  retention_days: number
  allow_user_memory: boolean
  allow_task_memory: boolean
  allow_tenant_memory: boolean
}

export type VirployeeProfile = {
  profile_id: UUID
  profile_key: string
  name: string
  system_prompt: string
  max_autonomy: AutonomyLevel
  default_capability_ids: UUID[]
  memory_policy: MemoryPolicy
  llm_config: LLMConfig
  status: VirployeeProfileStatus
}

export type Capability = {
  capability_id: UUID
  capability_key: string
  name: string
  description: string
  version: string
  product_id: UUID
  tool_id?: UUID | null
  mode: CapabilityMode
  risk_class: RiskClass
  status: string
}

export type Tool = {
  tool_id: UUID
  tool_key: string
  name: string
  description: string
  connector_id?: UUID | null
  operation: string
  side_effect: boolean
  status: string
}

export type ConnectorConfigField = {
  key: string
  label: string
  type: 'text' | 'number' | 'select' | 'checkbox' | 'textarea'
  required?: boolean
  secret?: boolean
  default_value?: string
  options?: string[]
}

export type ConnectorType = {
  kind: string
  name: string
  description: string
  config_schema: {
    fields: ConnectorConfigField[]
  }
  supports_test: boolean
  supports_refresh: boolean
  status: string
  capability_source?: string
}

export type Connector = {
  connector_id: UUID
  name: string
  kind: string
  enabled: boolean
  status: 'active' | 'disabled' | 'archived' | 'trash'
}

export type Memory = {
  memory_id: UUID
  tenant_id: UUID
  owner_virployee_id?: UUID | null
  policy: MemoryPolicy
  status: MemoryStatus
}

export type Task = {
  task_id: UUID
  tenant_id: UUID
  assignee_virployee_id?: UUID | null
  title: string
  description: string
  status: TaskStatus
}

export type Watcher = {
  watcher_id: UUID
  tenant_id: UUID
  assignee_virployee_id?: UUID | null
  name: string
  trigger_kind: string
  status: WatcherStatus
}

export type Handoff = {
  handoff_id: UUID
  tenant_id: UUID
  task_id?: UUID | null
  from_virployee_id?: UUID | null
  to_virployee_id: UUID
  reason: string
  status: HandoffStatus
}

export type AuditEvent = {
  audit_event_id: UUID
  tenant_id: UUID
  actor_user_id?: UUID | string | null
  resource_type: string
  resource_id: UUID
  action: string
  occurred_at: ISODateTime
}
