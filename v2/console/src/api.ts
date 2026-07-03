export type LifecycleTimestamp = string | null

export type Tenant = {
  id: string
  org_id: string
  product_surface: string
  name: string
  status: string
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type AxisUser = {
  id: string
  email: string
  name: string
  status: string
}

export type Session = {
  principal_id: string
  actor_id: string
  org_id: string
  auth_method: string
  user: AxisUser
  tenants: Tenant[]
}

export type VirployeeState = 'active' | 'archived' | 'trashed'
export type VirployeeAutonomy = 'A0' | 'A1' | 'A2' | 'A3' | 'A4' | 'A5'

export type VirployeeAutonomyActionClass = {
  class: string
  name: string
  description: string
  requires_approval: boolean
}

export type VirployeeAutonomyLevel = {
  level: VirployeeAutonomy
  name: string
  description: string
  allowed_action_classes: VirployeeAutonomyActionClass[]
}

export type Virployee = {
  id: string
  name: string
  job_role_id: string
  description: string
  supervisor_user_id: string
  autonomy: VirployeeAutonomy
  state: VirployeeState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type VirployeeInput = {
  name: string
  job_role_id: string
  description: string
  supervisor_user_id: string
  autonomy: VirployeeAutonomy | ''
}

export type JobRoleState = 'active' | 'archived' | 'trashed'

export type JobRole = {
  id: string
  tenant_id: string
  name: string
  slug: string
  mission: string
  state: JobRoleState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type JobRoleInput = {
  name: string
  mission: string
}

type VirployeesListResponse = {
  data: Virployee[]
}

type JobRolesListResponse = {
  data: JobRole[]
}

type AutonomyLevelsResponse = {
  data: VirployeeAutonomyLevel[]
}

export type AxisFetchInit = {
  tenantId?: string
  principalId?: string
  method?: string
  body?: unknown
  headers?: Record<string, string>
}

export async function axisFetch<T>(path: string, init: AxisFetchInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body !== undefined) {
    headers.set('Content-Type', 'application/json')
  }
  if (init.tenantId) {
    headers.set('X-Tenant-ID', init.tenantId)
  }
  if (init.principalId) {
    headers.set('X-Actor-ID', init.principalId)
  }

  const response = await fetch(path, {
    method: init.method ?? 'GET',
    headers,
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
  })

  if (!response.ok) {
    throw new Error(await responseErrorMessage(response))
  }
  if (response.status === 204) {
    return undefined as T
  }
  return response.json() as Promise<T>
}

export function getSession(): Promise<Session> {
  return axisFetch<Session>('/api/session')
}

export function listVirployees(
  lifecycle: 'active' | 'archived' | 'trash',
  tenantId: string,
  principalId: string,
): Promise<Virployee[]> {
  const path =
    lifecycle === 'active'
      ? '/api/virployees'
      : lifecycle === 'archived'
        ? '/api/virployees/archived'
        : '/api/virployees/trash'
  return axisFetch<VirployeesListResponse>(path, { tenantId, principalId }).then((payload) => payload.data ?? [])
}

export function listVirployeeAutonomyLevels(
  tenantId: string,
  principalId: string,
): Promise<VirployeeAutonomyLevel[]> {
  return axisFetch<AutonomyLevelsResponse>('/api/virployees/autonomy-levels', { tenantId, principalId })
    .then((payload) => payload.data ?? [])
}

export function createVirployee(input: VirployeeInput, tenantId: string, principalId: string): Promise<Virployee> {
  return axisFetch<Virployee>('/api/virployees', {
    method: 'POST',
    tenantId,
    principalId,
    body: input,
  })
}

export function updateVirployee(
  id: string,
  input: VirployeeInput,
  tenantId: string,
  principalId: string,
): Promise<Virployee> {
  return axisFetch<Virployee>(`/api/virployees/${encodeURIComponent(id)}`, {
    method: 'PUT',
    tenantId,
    principalId,
    body: input,
  })
}

export function archiveVirployee(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('virployees', id, 'archive', tenantId, principalId)
}

export function unarchiveVirployee(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('virployees', id, 'unarchive', tenantId, principalId)
}

export function trashVirployee(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('virployees', id, 'trash', tenantId, principalId)
}

export function restoreVirployee(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('virployees', id, 'restore', tenantId, principalId)
}

export function purgeVirployee(id: string, tenantId: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/virployees/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    tenantId,
    principalId,
  })
}

export function listJobRoles(
  lifecycle: 'active' | 'archived' | 'trash',
  tenantId: string,
  principalId: string,
): Promise<JobRole[]> {
  const path =
    lifecycle === 'active'
      ? '/api/job-roles'
      : lifecycle === 'archived'
        ? '/api/job-roles?lifecycle=archived'
        : '/api/job-roles?lifecycle=trash'
  return axisFetch<JobRolesListResponse>(path, { tenantId, principalId }).then((payload) => payload.data ?? [])
}

export function createJobRole(input: JobRoleInput, tenantId: string, principalId: string): Promise<JobRole> {
  return axisFetch<JobRole>('/api/job-roles', {
    method: 'POST',
    tenantId,
    principalId,
    body: input,
  })
}

export function updateJobRole(
  id: string,
  input: JobRoleInput,
  tenantId: string,
  principalId: string,
): Promise<JobRole> {
  return axisFetch<JobRole>(`/api/job-roles/${encodeURIComponent(id)}`, {
    method: 'PUT',
    tenantId,
    principalId,
    body: input,
  })
}

export function archiveJobRole(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('job-roles', id, 'archive', tenantId, principalId)
}

export function unarchiveJobRole(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('job-roles', id, 'unarchive', tenantId, principalId)
}

export function trashJobRole(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('job-roles', id, 'trash', tenantId, principalId)
}

export function restoreJobRole(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('job-roles', id, 'restore', tenantId, principalId)
}

export function purgeJobRole(id: string, tenantId: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/job-roles/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    tenantId,
    principalId,
  })
}

async function responseErrorMessage(response: Response): Promise<string> {
  let fallback = `Request failed with ${response.status}`
  try {
    const payload = await response.json()
    fallback = payload?.message || payload?.error || fallback
  } catch {
    // Keep the status based fallback for empty/non-JSON responses.
  }
  return fallback
}

function lifecycleAction(
  resource: 'virployees' | 'job-roles',
  id: string,
  action: string,
  tenantId: string,
  principalId: string,
): Promise<void> {
  return axisFetch<void>(`/api/${resource}/${encodeURIComponent(id)}/${action}`, {
    method: 'POST',
    tenantId,
    principalId,
    body: { reason: 'console' },
  })
}
