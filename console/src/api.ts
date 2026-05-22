export type AxisSession = {
  actor_id: string
  org_id: string
  role: string
  scopes: string[]
  auth_method: string
}

export type ServiceHealth = {
  companion: string
  nexus: string
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
  title: string
  status: string
  priority?: string
  channel?: string
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

export async function axisFetch<T>(path: string, orgId: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Accept', 'application/json')
  if (!(init.body instanceof FormData)) {
    headers.set('Content-Type', headers.get('Content-Type') ?? 'application/json')
  }
  if (orgId) {
    headers.set('X-Axis-Org-ID', orgId)
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

export async function getSession(orgId: string): Promise<AxisSession> {
  return axisFetch<AxisSession>('/api/session', orgId)
}

export async function getHealth(): Promise<ServiceHealth> {
  const response = await fetch('/readyz')
  const payload = await response.json()
  if (!response.ok) {
    throw new Error('services unavailable')
  }
  return payload
}
