export type AxisSession = {
  actor_id: string
  org_id: string
  orgs?: AxisOrg[]
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

export type AxisUser = {
  id: string
  external_id?: string
  provider?: string
  provider_user_id?: string
  email: string
  name: string
  role?: string
  axis_role?: string
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

export type AxisMember = {
  org_id: string
  user_id: string
  role: string
  status: string
  created_at: string
  updated_at: string
  user?: AxisUser
}

export type AxisInvitation = {
  id: string
  org_id: string
  email: string
  role: string
  status: string
  provider: string
  provider_invitation_id?: string
  invited_by?: string
  accepted_by?: string
  expires_at: string
  created_at: string
  updated_at: string
}

export type AxisAuditEvent = {
  id: string
  org_id?: string
  actor?: string
  action: string
  target: string
  target_id?: string
  payload?: Record<string, unknown>
  created_at: string
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

export async function getSession(): Promise<AxisSession> {
  return axisFetch<AxisSession>('/api/session', '')
}

export async function listAxisOrgs(orgId: string): Promise<AxisOrg[]> {
  const payload = await axisFetch<{ orgs: AxisOrg[] }>('/api/orgs', orgId)
  return payload.orgs ?? []
}

export async function createAxisOrg(orgId: string, input: Partial<AxisOrg>): Promise<AxisOrg> {
  const payload = await axisFetch<{ org: AxisOrg }>('/api/orgs', orgId, {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return payload.org
}

export async function updateAxisOrg(orgId: string, targetOrgId: string, input: Partial<AxisOrg>): Promise<AxisOrg> {
  const payload = await axisFetch<{ org: AxisOrg }>(`/api/orgs/${encodeURIComponent(targetOrgId)}`, orgId, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })
  return payload.org
}

export async function deleteAxisOrg(orgId: string, targetOrgId: string): Promise<void> {
  await axisFetch<null>(`/api/orgs/${encodeURIComponent(targetOrgId)}`, orgId, {
    method: 'DELETE',
  })
}

export async function listAxisUsers(orgId: string): Promise<AxisUser[]> {
  const payload = await axisFetch<{ users: AxisUser[] }>('/api/users', orgId)
  return payload.users ?? []
}

export async function createAxisUser(orgId: string, input: Partial<AxisUser>): Promise<AxisUser> {
  const payload = await axisFetch<{ user: AxisUser }>('/api/users', orgId, {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return payload.user
}

export async function updateAxisUser(orgId: string, userId: string, input: Partial<AxisUser>): Promise<AxisUser> {
  const payload = await axisFetch<{ user: AxisUser }>(`/api/users/${encodeURIComponent(userId)}`, orgId, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })
  return payload.user
}

export async function deleteAxisUser(orgId: string, userId: string): Promise<void> {
  await axisFetch<null>(`/api/users/${encodeURIComponent(userId)}`, orgId, {
    method: 'DELETE',
  })
}

export async function listAxisMembers(orgId: string, targetOrgId: string): Promise<AxisMember[]> {
  const payload = await axisFetch<{ members: AxisMember[] }>(`/api/orgs/${encodeURIComponent(targetOrgId)}/members`, orgId)
  return payload.members ?? []
}

export async function upsertAxisMember(orgId: string, targetOrgId: string, input: Partial<AxisMember>): Promise<AxisMember> {
  const payload = await axisFetch<{ member: AxisMember }>(`/api/orgs/${encodeURIComponent(targetOrgId)}/members`, orgId, {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return payload.member
}

export async function updateAxisMember(orgId: string, targetOrgId: string, userId: string, input: Partial<AxisMember>): Promise<AxisMember> {
  const payload = await axisFetch<{ member: AxisMember }>(`/api/orgs/${encodeURIComponent(targetOrgId)}/members/${encodeURIComponent(userId)}`, orgId, {
    method: 'PATCH',
    body: JSON.stringify(input),
  })
  return payload.member
}

export async function deleteAxisMember(orgId: string, targetOrgId: string, userId: string): Promise<void> {
  await axisFetch<null>(`/api/orgs/${encodeURIComponent(targetOrgId)}/members/${encodeURIComponent(userId)}`, orgId, {
    method: 'DELETE',
  })
}

export async function listAxisInvitations(orgId: string, targetOrgId: string): Promise<AxisInvitation[]> {
  const payload = await axisFetch<{ invitations: AxisInvitation[] }>(`/api/orgs/${encodeURIComponent(targetOrgId)}/invitations`, orgId)
  return payload.invitations ?? []
}

export async function createAxisInvitation(orgId: string, targetOrgId: string, input: Partial<AxisInvitation>): Promise<AxisInvitation> {
  const payload = await axisFetch<{ invitation: AxisInvitation }>(`/api/orgs/${encodeURIComponent(targetOrgId)}/invitations`, orgId, {
    method: 'POST',
    body: JSON.stringify(input),
  })
  return payload.invitation
}

export async function updateAxisInvitationStatus(orgId: string, invitationId: string, action: 'accept' | 'revoke' | 'resend'): Promise<AxisInvitation> {
  const payload = await axisFetch<{ invitation: AxisInvitation }>(`/api/org-invitations/${encodeURIComponent(invitationId)}/${action}`, orgId, {
    method: 'POST',
    body: '{}',
  })
  return payload.invitation
}

export async function listAxisAuditEvents(orgId: string, targetOrgId?: string): Promise<AxisAuditEvent[]> {
  const query = targetOrgId ? `?org_id=${encodeURIComponent(targetOrgId)}` : ''
  const payload = await axisFetch<{ events: AxisAuditEvent[] }>(`/api/iam-audit${query}`, orgId)
  return payload.events ?? []
}

export async function listIAMTenants(orgId: string, view = 'active'): Promise<AxisTenantView[]> {
  const suffix = view === 'active' ? '' : `/${view}`
  const payload = await axisFetch<{ items: AxisTenantView[] }>(`/api/iam/tenants${suffix}`, orgId)
  return payload.items ?? []
}

export type AgentProfileLifecycle = 'active' | 'archived' | 'trash' | 'all'

export async function listAgentProfiles(
  orgId: string,
  lifecycleOrIncludeArchived: AgentProfileLifecycle | boolean = 'active',
): Promise<AxisAgentProfileView[]> {
  const suffix = typeof lifecycleOrIncludeArchived === 'boolean'
    ? (lifecycleOrIncludeArchived ? '?include_archived=true' : '')
    : `?lifecycle=${encodeURIComponent(lifecycleOrIncludeArchived)}`
  const payload = await axisFetch<{ profiles: AxisAgentProfileView[] }>(`/api/agent-profiles${suffix}`, orgId)
  return payload.profiles ?? []
}

export async function upsertAgentProfile(
  orgId: string,
  profileId: string,
  input: Partial<AxisAgentProfileView>,
): Promise<AxisAgentProfileView> {
  return axisFetch<AxisAgentProfileView>(`/api/agent-profiles/${encodeURIComponent(profileId)}`, orgId, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

export async function archiveAgentProfile(orgId: string, profileId: string): Promise<AxisAgentProfileView> {
  return axisFetch<AxisAgentProfileView>(`/api/agent-profiles/${encodeURIComponent(profileId)}/archive`, orgId, {
    method: 'POST',
    body: '{}',
  })
}

export async function trashAgentProfile(orgId: string, profileId: string): Promise<AxisAgentProfileView> {
  return axisFetch<AxisAgentProfileView>(`/api/agent-profiles/${encodeURIComponent(profileId)}/trash`, orgId, {
    method: 'POST',
    body: '{}',
  })
}

export async function restoreAgentProfile(orgId: string, profileId: string): Promise<AxisAgentProfileView> {
  return axisFetch<AxisAgentProfileView>(`/api/agent-profiles/${encodeURIComponent(profileId)}/restore`, orgId, {
    method: 'POST',
    body: '{}',
  })
}

export async function purgeAgentProfile(orgId: string, profileId: string): Promise<void> {
  await axisFetch<void>(`/api/agent-profiles/${encodeURIComponent(profileId)}/purge`, orgId, {
    method: 'DELETE',
  })
}

export function axisCrudHttpClient(orgId: string) {
  return {
    async json<TResponse>(path: string, init?: { method?: string; body?: Record<string, unknown> }): Promise<TResponse> {
      return axisFetch<TResponse>(path, orgId, {
        method: init?.method ?? 'GET',
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
