export type AxisSession = {
  actor_id: string
  org_id: string
  orgs?: AxisOrg[]
  role: string
  scopes: string[]
  auth_method: string
}

export type AxisOrg = {
  id: string
  external_id?: string
  provider?: string
  provider_org_id?: string
  name: string
  slug: string
  status: string
  created_at: string
  updated_at: string
}

export type ServiceHealth = {
  companion: string
  nexus: string
}

export type Product = {
  product_surface: string
  display_name: string
  status: string
  metadata?: Record<string, unknown>
}

export type ProductInstallation = {
  id?: string
  org_id: string
  product_surface: string
  external_tenant_id?: string
  base_url?: string
  auth_mode: string
  enabled: boolean
  config?: Record<string, unknown>
}

export type Approval = {
  id: string
  request_id: string
  org_id?: string
  status: string
  expires_at: string
  created_at: string
  current_approvals?: number
  required_approvals?: number
}

export type NexusRequest = {
  id: string
  org_id?: string
  action_type: string
  target_system?: string
  target_resource?: string
  decision: string
  risk_level: string
  status: string
  created_at: string
}

export type Policy = {
  id: string
  org_id?: string
  name: string
  effect: string
  mode: string
  enabled: boolean
}

export type ActionType = {
  id: string
  org_id?: string
  name: string
  category: string
  risk_class: string
  enabled: boolean
}

export type Delegation = {
  id: string
  org_id?: string
  owner_id: string
  agent_id: string
  enabled: boolean
  max_risk_class?: string
}

export type CompanionTask = {
  id: string
  org_id?: string
  product_surface?: string
  agent_id?: string
  run_type?: string
  title: string
  status: string
  priority?: string
  channel?: string
  updated_at?: string
}

export type AgentRun = {
  id: string
  task_id: string
  axis_run_id?: string
  agent_id: string
  product_surface?: string
  run_type?: string
  recommendation?: string
  summary?: string
  evidence?: Array<Record<string, unknown>>
  proposed_actions?: Array<Record<string, unknown>>
  nexus_request_id?: string
  reply?: string
  tool_calls?: Array<{ name: string; status?: string; allowed?: boolean; error?: string }>
  task?: CompanionTask
  created_at?: string
  updated_at?: string
}

export type RunTrace = {
  id: string
  org_id?: string
  task_id?: string
  product_surface?: string
  intent?: string
  status?: string
  started_at?: string
}

export type RuntimePolicy = {
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
    allowed_connectors?: string[]
    denied_connectors?: string[]
    embedding?: {
      provider?: string
      model?: string
      vector_store?: string
      namespace_mode?: string
    }
    observability?: {
      trace_level?: string
      redaction_mode?: string
      replay_enabled?: boolean
    }
  }
}

export type CompanionAgent = {
  org_id?: string
  product_surface?: string
  agent_id: string
  display_name?: string
  role?: string
  profile_id?: string
  status: string
  max_autonomy: string
  allowed_capabilities?: string[]
  allowed_connectors?: string[]
}

export type CapabilityRecord = {
  id: string
  status: string
  source: string
  manifest: {
    capability_id: string
    product_surface?: string
    version: string
    display_name: string
    connector: string
    risk_level: string
    side_effect_type: string
    approval_required: boolean
    cost_class: string
  }
  updated_at?: string
}

export type MemoryConflict = {
  id: string
  product_surface?: string
  kind: string
  memory_type: string
  key: string
  status: string
  confidence: number
  updated_at: string
}

export type MemorySummary = {
  id: string
  product_surface?: string
  scope_type: string
  scope_id: string
  summary_type: string
  version: number
  source_count: number
  created_at: string
}

export type MemoryReview = {
  id: string
  product_surface?: string
  memory_id?: string
  review_type: string
  status: string
  reason?: string
  created_at?: string
}

export type CompanionJob = {
  id: string
  product_surface?: string
  kind: string
  status: string
  attempts: number
  max_attempts: number
  created_at: string
}

export type ObservabilityEvent = {
  id: string
  product_surface?: string
  event_type: string
  event_name: string
  severity: string
  agent_id?: string
  capability_id?: string
  occurred_at: string
}

export type CostSummary = {
  org_id: string
  period: string
  estimated_tokens: number
  estimated_cost_cents: number
  llm_calls: number
  tool_calls: number
  job_events: number
  embedding_events: number
}

export type SecurityEvalReport = {
  id: string
  suite: string
  status: string
  score: number
  threshold: number
  created_at?: string
}

export type BusinessModel = {
  org_id: string
  product_surface: string
  version?: number
  status?: string
  areas?: unknown[]
  roles?: unknown[]
  workflows?: unknown[]
  rules?: unknown[]
  vocabulary?: Record<string, string>
}

type AxisAuthTokenGetter = () => string | null | undefined | Promise<string | null | undefined>

let axisAuthTokenGetter: AxisAuthTokenGetter | null = null

export function setAxisAuthTokenGetter(getter: AxisAuthTokenGetter | null) {
  axisAuthTokenGetter = getter
}

export async function axisFetch<T>(path: string, orgId: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (!(init.body instanceof FormData)) {
    headers.set('Content-Type', headers.get('Content-Type') ?? 'application/json')
  }
  if (orgId) {
    headers.set('X-Axis-Org-ID', orgId)
  }
  const token = await resolveAxisAuthToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  const response = await fetch(path, { ...init, headers })
  const text = await response.text()
  const payload = text ? JSON.parse(text) : null
  if (!response.ok) {
    const message = payload?.error?.message ?? `HTTP ${response.status}`
    throw new Error(message)
  }
  return payload as T
}

async function resolveAxisAuthToken(): Promise<string> {
  if (!axisAuthTokenGetter) {
    return ''
  }
  const token = await axisAuthTokenGetter()
  return typeof token === 'string' ? token : ''
}

export async function getSession(orgId: string): Promise<AxisSession> {
  return axisFetch<AxisSession>('/api/session', orgId)
}

export async function listAxisOrgs(orgId: string): Promise<AxisOrg[]> {
  const payload = await axisFetch<{ orgs: AxisOrg[] }>('/api/orgs', orgId)
  return payload.orgs ?? []
}

export async function getHealth(): Promise<ServiceHealth> {
  const response = await fetch('/readyz')
  const payload = await response.json()
  if (!response.ok) {
    throw new Error('services unavailable')
  }
  return payload
}
