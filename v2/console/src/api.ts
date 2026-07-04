export type LifecycleTimestamp = string | null
export type TenantState = 'active' | 'archived' | 'trashed'

export type Tenant = {
  id: string
  org_id: string
  org_name: string
  product_surface: string
  product_name: string
  status: string
  state: TenantState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type AxisUser = {
  id: string
  email: string
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

export type AxisOrg = {
  id: string
  name: string
  provider: string
  provider_org_id: string
  status: string
  state: TenantState
  tenant_count: number
  has_tenants: boolean
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type OrgInput = {
  name: string
}

export type Product = {
  id: string
  product_surface: string
  name: string
  status: string
  state: TenantState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type ProductInput = {
  name: string
  product_surface?: string
}

export type TenantInput = {
  org_id?: string
  org_name?: string
  product_surface: string
}

export type TenantUpdateInput = {
  org_name: string
}

export type VirployeeState = 'active' | 'archived' | 'trashed'
export type VirployeeAutonomy = 'A0' | 'A1' | 'A2' | 'A3' | 'A4' | 'A5'

export type VirployeeAutonomyLevel = {
  level: VirployeeAutonomy
  name: string
  description: string
  allows_required_autonomies: VirployeeAutonomy[]
}

export type Virployee = {
  id: string
  name: string
  job_role_id: string
  profile_template_id: string
  capability_ids: string[]
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
  profile_template_id: string
  capability_ids?: string[]
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

export type CapabilityState = 'active' | 'archived' | 'trashed'

export type Capability = {
  id: string
  tenant_id: string
  capability_key: string
  name: string
  description: string
  required_autonomy: VirployeeAutonomy
  state: CapabilityState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type CapabilityInput = {
  capability_key?: string
  domain?: string
  resource?: string
  action?: string
  name: string
  description: string
  required_autonomy: VirployeeAutonomy | ''
}

export type ProfileTemplateState = 'active' | 'archived' | 'trashed'

export type ProfileTemplate = {
  id: string
  tenant_id: string
  name: string
  description: string
  system_prompt: string
  max_autonomy: VirployeeAutonomy
  state: ProfileTemplateState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type ProfileTemplateInput = {
  name: string
  description: string
  system_prompt: string
  max_autonomy: VirployeeAutonomy | ''
}

export type UserState = 'active' | 'archived' | 'trashed' | 'pending'
export type TenantUserRole = 'owner' | 'admin' | 'member'
export type TenantUserKind = 'user' | 'invitation'

export type TenantUser = {
  id: string
  kind: TenantUserKind
  email: string
  role: TenantUserRole
  tenant_id: string
  state: UserState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type TenantUserInput = {
  email: string
  role: TenantUserRole
}

type VirployeesListResponse = {
  data: Virployee[]
}

type JobRolesListResponse = {
  data: JobRole[]
}

type CapabilitiesListResponse = {
  data: Capability[]
}

type ProfileTemplatesListResponse = {
  data: ProfileTemplate[]
}

type UsersListResponse = {
  data: TenantUser[]
}

type TenantsListResponse = {
  data: Tenant[]
}

type OrgsListResponse = {
  data: AxisOrg[]
}

type ProductsListResponse = {
  data: Product[]
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

type AxisAuthTokenGetter = () => string | null | undefined | Promise<string | null | undefined>

let axisAuthTokenGetter: AxisAuthTokenGetter | null = null

export function setAxisAuthTokenGetter(getter: AxisAuthTokenGetter | null) {
  axisAuthTokenGetter = getter
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
  const token = await resolveAxisAuthToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
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

export function listTenants(
  lifecycle: 'active' | 'archived' | 'trash',
  principalId: string,
): Promise<Tenant[]> {
  const path =
    lifecycle === 'active'
      ? '/api/tenants'
      : `/api/tenants?lifecycle=${encodeURIComponent(lifecycle)}`
  return axisFetch<TenantsListResponse>(path, { principalId }).then((payload) => payload.data ?? [])
}

export function listOrgs(
  lifecycle: 'active' | 'archived' | 'trash',
  principalId: string,
): Promise<AxisOrg[]> {
  const path =
    lifecycle === 'active'
      ? '/api/orgs'
      : `/api/orgs?lifecycle=${encodeURIComponent(lifecycle)}`
  return axisFetch<OrgsListResponse>(path, { principalId }).then((payload) => payload.data ?? [])
}

export function createOrg(input: OrgInput, principalId: string): Promise<AxisOrg> {
  return axisFetch<AxisOrg>('/api/orgs', {
    method: 'POST',
    principalId,
    body: input,
  })
}

export function updateOrg(id: string, input: OrgInput, principalId: string): Promise<AxisOrg> {
  return axisFetch<AxisOrg>(`/api/orgs/${encodeURIComponent(id)}`, {
    method: 'PUT',
    principalId,
    body: input,
  })
}

export function archiveOrg(id: string, principalId: string): Promise<void> {
  return orgLifecycleAction(id, 'archive', principalId)
}

export function unarchiveOrg(id: string, principalId: string): Promise<void> {
  return orgLifecycleAction(id, 'unarchive', principalId)
}

export function trashOrg(id: string, principalId: string): Promise<void> {
  return orgLifecycleAction(id, 'trash', principalId)
}

export function restoreOrg(id: string, principalId: string): Promise<void> {
  return orgLifecycleAction(id, 'restore', principalId)
}

export function purgeOrg(id: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/orgs/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    principalId,
  })
}

export function listProducts(
  lifecycle: 'active' | 'archived' | 'trash',
  principalId: string,
): Promise<Product[]> {
  const path =
    lifecycle === 'active'
      ? '/api/products'
      : `/api/products?lifecycle=${encodeURIComponent(lifecycle)}`
  return axisFetch<ProductsListResponse>(path, { principalId }).then((payload) => payload.data ?? [])
}

export function createProduct(input: ProductInput, principalId: string): Promise<Product> {
  return axisFetch<Product>('/api/products', {
    method: 'POST',
    principalId,
    body: input,
  })
}

export function updateProduct(id: string, input: ProductInput, principalId: string): Promise<Product> {
  return axisFetch<Product>(`/api/products/${encodeURIComponent(id)}`, {
    method: 'PUT',
    principalId,
    body: { name: input.name },
  })
}

export function archiveProduct(id: string, principalId: string): Promise<void> {
  return productLifecycleAction(id, 'archive', principalId)
}

export function unarchiveProduct(id: string, principalId: string): Promise<void> {
  return productLifecycleAction(id, 'unarchive', principalId)
}

export function trashProduct(id: string, principalId: string): Promise<void> {
  return productLifecycleAction(id, 'trash', principalId)
}

export function restoreProduct(id: string, principalId: string): Promise<void> {
  return productLifecycleAction(id, 'restore', principalId)
}

export function purgeProduct(id: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/products/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    principalId,
  })
}

export function createTenant(input: TenantInput, principalId: string): Promise<Tenant> {
  return axisFetch<Tenant>('/api/tenants', {
    method: 'POST',
    principalId,
    body: input,
  })
}

export function updateTenant(id: string, input: TenantUpdateInput, principalId: string): Promise<Tenant> {
  return axisFetch<Tenant>(`/api/tenants/${encodeURIComponent(id)}`, {
    method: 'PUT',
    principalId,
    body: input,
  })
}

export function archiveTenant(id: string, principalId: string): Promise<void> {
  return tenantLifecycleAction(id, 'archive', principalId)
}

export function unarchiveTenant(id: string, principalId: string): Promise<void> {
  return tenantLifecycleAction(id, 'unarchive', principalId)
}

export function trashTenant(id: string, principalId: string): Promise<void> {
  return tenantLifecycleAction(id, 'trash', principalId)
}

export function restoreTenant(id: string, principalId: string): Promise<void> {
  return tenantLifecycleAction(id, 'restore', principalId)
}

export function purgeTenant(id: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/tenants/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    principalId,
  })
}

async function resolveAxisAuthToken(): Promise<string> {
  if (!axisAuthTokenGetter) return ''
  try {
    return (await axisAuthTokenGetter())?.trim() ?? ''
  } catch (error) {
    console.error('axis_console_v2_auth_token_failed', error)
    return ''
  }
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

export function listCapabilities(
  lifecycle: 'active' | 'archived' | 'trash',
  tenantId: string,
  principalId: string,
): Promise<Capability[]> {
  const path =
    lifecycle === 'active'
      ? '/api/capabilities'
      : lifecycle === 'archived'
        ? '/api/capabilities?lifecycle=archived'
        : '/api/capabilities?lifecycle=trash'
  return axisFetch<CapabilitiesListResponse>(path, { tenantId, principalId }).then((payload) => payload.data ?? [])
}

export function createCapability(input: CapabilityInput, tenantId: string, principalId: string): Promise<Capability> {
  return axisFetch<Capability>('/api/capabilities', {
    method: 'POST',
    tenantId,
    principalId,
    body: {
      capability_key: input.capability_key,
      name: input.name,
      description: input.description,
      required_autonomy: input.required_autonomy,
    },
  })
}

export function updateCapability(
  id: string,
  input: CapabilityInput,
  tenantId: string,
  principalId: string,
): Promise<Capability> {
  return axisFetch<Capability>(`/api/capabilities/${encodeURIComponent(id)}`, {
    method: 'PUT',
    tenantId,
    principalId,
    body: {
      name: input.name,
      description: input.description,
      required_autonomy: input.required_autonomy,
    },
  })
}

export function archiveCapability(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('capabilities', id, 'archive', tenantId, principalId)
}

export function unarchiveCapability(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('capabilities', id, 'unarchive', tenantId, principalId)
}

export function trashCapability(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('capabilities', id, 'trash', tenantId, principalId)
}

export function restoreCapability(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('capabilities', id, 'restore', tenantId, principalId)
}

export function purgeCapability(id: string, tenantId: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/capabilities/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    tenantId,
    principalId,
  })
}

export function listProfileTemplates(
  lifecycle: 'active' | 'archived' | 'trash',
  tenantId: string,
  principalId: string,
): Promise<ProfileTemplate[]> {
  const path =
    lifecycle === 'active'
      ? '/api/profile-templates'
      : lifecycle === 'archived'
        ? '/api/profile-templates?lifecycle=archived'
        : '/api/profile-templates?lifecycle=trash'
  return axisFetch<ProfileTemplatesListResponse>(path, { tenantId, principalId }).then((payload) => payload.data ?? [])
}

export function createProfileTemplate(
  input: ProfileTemplateInput,
  tenantId: string,
  principalId: string,
): Promise<ProfileTemplate> {
  return axisFetch<ProfileTemplate>('/api/profile-templates', {
    method: 'POST',
    tenantId,
    principalId,
    body: input,
  })
}

export function updateProfileTemplate(
  id: string,
  input: ProfileTemplateInput,
  tenantId: string,
  principalId: string,
): Promise<ProfileTemplate> {
  return axisFetch<ProfileTemplate>(`/api/profile-templates/${encodeURIComponent(id)}`, {
    method: 'PUT',
    tenantId,
    principalId,
    body: input,
  })
}

export function archiveProfileTemplate(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('profile-templates', id, 'archive', tenantId, principalId)
}

export function unarchiveProfileTemplate(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('profile-templates', id, 'unarchive', tenantId, principalId)
}

export function trashProfileTemplate(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('profile-templates', id, 'trash', tenantId, principalId)
}

export function restoreProfileTemplate(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('profile-templates', id, 'restore', tenantId, principalId)
}

export function purgeProfileTemplate(id: string, tenantId: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/profile-templates/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    tenantId,
    principalId,
  })
}

export function listUsers(
  lifecycle: 'active' | 'archived' | 'trash',
  tenantId: string,
  principalId: string,
): Promise<TenantUser[]> {
  const path =
    lifecycle === 'active'
      ? '/api/users'
      : lifecycle === 'archived'
        ? '/api/users?lifecycle=archived'
        : '/api/users?lifecycle=trash'
  return axisFetch<UsersListResponse>(path, { tenantId, principalId }).then((payload) => payload.data ?? [])
}

export function createUser(input: TenantUserInput, tenantId: string, principalId: string): Promise<TenantUser> {
  return axisFetch<TenantUser>('/api/users', {
    method: 'POST',
    tenantId,
    principalId,
    body: input,
  })
}

export function updateUser(
  id: string,
  input: TenantUserInput,
  tenantId: string,
  principalId: string,
): Promise<TenantUser> {
  return axisFetch<TenantUser>(`/api/users/${encodeURIComponent(id)}`, {
    method: 'PUT',
    tenantId,
    principalId,
    body: input,
  })
}

export function archiveUser(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('users', id, 'archive', tenantId, principalId)
}

export function unarchiveUser(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('users', id, 'unarchive', tenantId, principalId)
}

export function trashUser(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('users', id, 'trash', tenantId, principalId)
}

export function restoreUser(id: string, tenantId: string, principalId: string): Promise<void> {
  return lifecycleAction('users', id, 'restore', tenantId, principalId)
}

export function purgeUser(id: string, tenantId: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/users/${encodeURIComponent(id)}/purge`, {
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
  resource: 'virployees' | 'job-roles' | 'capabilities' | 'profile-templates' | 'users',
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

function tenantLifecycleAction(id: string, action: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/tenants/${encodeURIComponent(id)}/${action}`, {
    method: 'POST',
    principalId,
    body: { reason: 'console' },
  })
}

function orgLifecycleAction(id: string, action: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/orgs/${encodeURIComponent(id)}/${action}`, {
    method: 'POST',
    principalId,
    body: { reason: 'console' },
  })
}

function productLifecycleAction(id: string, action: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/products/${encodeURIComponent(id)}/${action}`, {
    method: 'POST',
    principalId,
    body: { reason: 'console' },
  })
}
