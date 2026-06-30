export type AxisTenant = {
  id: string
  org_id: string
  product_surface: string
  name: string
  status: string
  plan?: string
}

export type AxisSession = {
  actor_id: string
  org_id: string
  orgs?: AxisOrg[]
  tenants?: AxisTenant[]
  platform_roles?: string[]
  role: string
  axis_role?: string
  org_role?: string
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

export type AxisTenantView = {
  id: string
  name: string
  status: string
  created_at?: string
  updated_at?: string
}

export type AxisProductView = {
  id: string
  tenant_id: string
  product_surface: string
  name: string
  status: string
  created_at?: string
  updated_at?: string
}

export type AxisUserView = {
  id: string
  user_id?: string
  email: string
  role: string
  org_id?: string
  tenant_id?: string
  scope: string
  status: string
  created_at?: string
  updated_at?: string
}

export type AxisAgentView = {
  id: string
  org_id: string
  name: string
  profile: string
  autonomy: 'A1' | 'A2' | 'A3'
  memory_enabled: boolean
  description: string
  capabilities: string[]
  tools: string[]
  status: string
  source_system?: string
  source_org_id?: string
  source_product_surface?: string
  source_agent_id?: string
  external_tenant_id?: string
  source_status?: string
  origin_kind?: string
  review_status?: string
  validation_status?: string
  metadata?: Record<string, unknown>
  last_synced_at?: string
  created_at?: string
  updated_at?: string
}

export type AxisAgentProfileView = {
  id?: string
  profile_id: string
  family_id: string
  version_label: string
  name: string
  description?: string
  system_prompt?: string
  max_autonomy: string
  allowed_tools?: string[]
  allowed_capabilities?: string[]
  memory_policy?: Record<string, unknown>
  llm_config?: Record<string, unknown>
  enabled: boolean
  archived_at?: string
  trashed_at?: string
  created_at?: string
  updated_at?: string
}

export type AxisJobRoleResponsibility = {
  title: string
  description?: string
  expected_outcome?: string
  priority?: number
}

export type AxisJobRoleView = {
  id?: string
  job_role_id: string
  org_id: string
  product_surface: string
  name: string
  slug: string
  description?: string
  mission?: string
  responsibilities?: AxisJobRoleResponsibility[]
  recommended_capabilities?: string[]
  default_autonomy_level: string
  default_permission_bundle_id?: string
  success_criteria?: string[]
  default_sla_policy?: Record<string, unknown>
  default_memory_policy?: Record<string, unknown>
  status: string
  metadata?: Record<string, unknown>
  created_by?: string
  created_at?: string
  updated_at?: string
  archived_at?: string
  version?: number
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
export type AxisFetchInit = RequestInit & { tenantId?: string | null }

let axisAuthTokenGetter: AxisAuthTokenGetter | null = null

export function setAxisAuthTokenGetter(getter: AxisAuthTokenGetter | null) {
  axisAuthTokenGetter = getter
}

export async function axisFetch<T>(path: string, orgId: string, init: AxisFetchInit = {}): Promise<T> {
  const { tenantId: explicitTenantId, ...fetchInit } = init
  const headers = new Headers(fetchInit.headers)
  headers.set('Accept', 'application/json')
  if (!(fetchInit.body instanceof FormData)) {
    headers.set('Content-Type', headers.get('Content-Type') ?? 'application/json')
  }
  if (orgId) {
    headers.set('X-Axis-Org-ID', orgId)
  }
  // Active workspace = tenant (org x product). When set, the BFF treats it as the
  // source of truth (resolves org_id + product_surface + scopes from the tenant).
  const tenantId = explicitTenantId !== undefined
    ? explicitTenantId
    : (typeof localStorage !== 'undefined' ? localStorage.getItem('axis.tenant_id') : '')
  if (tenantId) {
    headers.set('X-Tenant-ID', tenantId)
  }
  const token = await resolveAxisAuthToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  const response = await fetch(path, { ...fetchInit, headers })
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

export async function getSession(): Promise<AxisSession> {
  return axisFetch<AxisSession>('/api/session', '', { tenantId: null })
}

// --- Control Plane (platform-admin) ---

export type ControlOrg = { id: string; name: string; slug: string; status: string }
export type ControlProduct = { product_surface: string; name: string }

export async function listControlOrgs(): Promise<ControlOrg[]> {
  const payload = await axisFetch<{ data: ControlOrg[] }>('/api/control/organizations', '', { tenantId: null })
  return payload.data ?? []
}

export async function createControlOrg(name: string): Promise<ControlOrg> {
  return axisFetch<ControlOrg>('/api/control/organizations', '', { method: 'POST', tenantId: null, body: JSON.stringify({ name }) })
}

export async function listControlTenants(): Promise<AxisTenant[]> {
  const payload = await axisFetch<{ data: AxisTenant[] }>('/api/control/tenants', '', { tenantId: null })
  return payload.data ?? []
}

export async function listControlProducts(): Promise<ControlProduct[]> {
  const payload = await axisFetch<{ data: ControlProduct[] }>('/api/control/products', '', { tenantId: null })
  return payload.data ?? []
}

export async function provisionTenant(input: { org_id: string; product_surface: string; name?: string; owner_user_id?: string }): Promise<AxisTenant> {
  return axisFetch<AxisTenant>('/api/control/tenants', '', { method: 'POST', tenantId: null, body: JSON.stringify(input) })
}

export async function grantPlatformRole(userId: string, role = 'platform_admin'): Promise<void> {
  await axisFetch<unknown>('/api/control/platform-roles', '', { method: 'POST', tenantId: null, body: JSON.stringify({ user_id: userId, role }) })
}

export async function addTenantMember(tenantId: string, userId: string, role = 'member'): Promise<void> {
  await axisFetch<unknown>(`/api/control/tenants/${encodeURIComponent(tenantId)}/members`, '', { method: 'POST', tenantId: null, body: JSON.stringify({ user_id: userId, role }) })
}

export async function listIAMTenants(orgId: string, view = 'active', tenantId?: string): Promise<AxisTenantView[]> {
  const suffix = view === 'active' ? '' : `/${view}`
  const payload = await axisFetch<{ items: AxisTenantView[] }>(`/api/iam/tenants${suffix}`, orgId, { tenantId })
  return payload.items ?? []
}

export type AgentProfileLifecycle = 'active' | 'archived' | 'trash' | 'all'

export async function listAgentProfiles(
  orgId: string,
  lifecycleOrIncludeArchived: AgentProfileLifecycle | boolean = 'active',
  tenantId?: string,
): Promise<AxisAgentProfileView[]> {
  const suffix = typeof lifecycleOrIncludeArchived === 'boolean'
    ? (lifecycleOrIncludeArchived ? '?include_archived=true' : '')
    : `?lifecycle=${encodeURIComponent(lifecycleOrIncludeArchived)}`
  const payload = await axisFetch<{ profiles: AxisAgentProfileView[] }>(`/api/agent-profiles${suffix}`, orgId, { tenantId })
  return payload.profiles ?? []
}

export async function upsertAgentProfile(
  orgId: string,
  profileId: string,
  input: Partial<AxisAgentProfileView>,
  tenantId?: string,
): Promise<AxisAgentProfileView> {
  return axisFetch<AxisAgentProfileView>(`/api/agent-profiles/${encodeURIComponent(profileId)}`, orgId, {
    method: 'PUT',
    tenantId,
    body: JSON.stringify(input),
  })
}

export async function archiveAgentProfile(orgId: string, profileId: string, tenantId?: string): Promise<AxisAgentProfileView> {
  return axisFetch<AxisAgentProfileView>(`/api/agent-profiles/${encodeURIComponent(profileId)}/archive`, orgId, {
    method: 'POST',
    tenantId,
    body: '{}',
  })
}

export async function trashAgentProfile(orgId: string, profileId: string, tenantId?: string): Promise<AxisAgentProfileView> {
  return axisFetch<AxisAgentProfileView>(`/api/agent-profiles/${encodeURIComponent(profileId)}/trash`, orgId, {
    method: 'POST',
    tenantId,
    body: '{}',
  })
}

export async function restoreAgentProfile(orgId: string, profileId: string, tenantId?: string): Promise<AxisAgentProfileView> {
  return axisFetch<AxisAgentProfileView>(`/api/agent-profiles/${encodeURIComponent(profileId)}/restore`, orgId, {
    method: 'POST',
    tenantId,
    body: '{}',
  })
}

export async function purgeAgentProfile(orgId: string, profileId: string, tenantId?: string): Promise<void> {
  await axisFetch<void>(`/api/agent-profiles/${encodeURIComponent(profileId)}/purge`, orgId, {
    method: 'DELETE',
    tenantId,
  })
}

export type JobRoleLifecycle = 'active' | 'archived' | 'all'

export async function listJobRoles(orgId: string, lifecycle: JobRoleLifecycle = 'active', tenantId?: string): Promise<AxisJobRoleView[]> {
  const payload = await axisFetch<{ job_roles?: AxisJobRoleView[]; data?: AxisJobRoleView[] }>(
    `/api/job-roles?lifecycle=${encodeURIComponent(lifecycle)}`,
    orgId,
    { tenantId },
  )
  return payload.job_roles ?? payload.data ?? []
}

export async function upsertJobRole(
  orgId: string,
  jobRoleId: string,
  input: Partial<AxisJobRoleView>,
  tenantId?: string,
): Promise<AxisJobRoleView> {
  return axisFetch<AxisJobRoleView>(`/api/job-roles/${encodeURIComponent(jobRoleId)}`, orgId, {
    method: 'PUT',
    tenantId,
    body: JSON.stringify(input),
  })
}

export async function archiveJobRole(orgId: string, jobRoleId: string, tenantId?: string): Promise<AxisJobRoleView> {
  return axisFetch<AxisJobRoleView>(`/api/job-roles/${encodeURIComponent(jobRoleId)}/archive`, orgId, {
    method: 'POST',
    tenantId,
    body: '{}',
  })
}

export async function restoreJobRole(orgId: string, jobRoleId: string, tenantId?: string): Promise<AxisJobRoleView> {
  return axisFetch<AxisJobRoleView>(`/api/job-roles/${encodeURIComponent(jobRoleId)}/restore`, orgId, {
    method: 'POST',
    tenantId,
    body: '{}',
  })
}

export function axisCrudHttpClient(orgId: string, tenantId?: string) {
  return {
    async json<TResponse>(path: string, init?: { method?: string; body?: Record<string, unknown> }): Promise<TResponse> {
      return axisFetch<TResponse>(path, orgId, {
        method: init?.method ?? 'GET',
        tenantId,
        body: init?.body ? JSON.stringify(init.body) : undefined,
      })
    },
  }
}

export async function getHealth(): Promise<ServiceHealth> {
  const response = await fetch('/readyz')
  const payload = await response.json()
  if (!response.ok) {
    throw new Error('services unavailable')
  }
  return payload
}
