export type AxisProductSurface = string
export type AxisProductStatus = 'active' | 'disabled'
export type AxisProductInstallationAuthMode =
  | 'none'
  | 'api_key_ref'
  | 'oauth2'
  | 'internal_jwt'
  | 'custom'

export type AxisProductWorkspace = Record<string, unknown>

export type AxisProductIdentityContextV1 = {
  org_id: string
  product_surface: AxisProductSurface
  actor_id: string
  actor_type?: 'human' | 'service' | string
  on_behalf_of?: string
  service_principal?: boolean
  external_tenant_id?: string
  scopes?: string[]
  workspace?: AxisProductWorkspace
}

export type AxisProduct = {
  product_surface: AxisProductSurface
  display_name: string
  status: AxisProductStatus
  metadata?: Record<string, unknown>
  created_by?: string
  created_at?: string
  updated_at?: string
}

export type AxisProductInstallation = {
  id?: string
  org_id: string
  product_surface: AxisProductSurface
  external_tenant_id?: string
  base_url?: string
  auth_mode: AxisProductInstallationAuthMode
  secret_ref?: string
  enabled: boolean
  config?: Record<string, unknown>
  created_by?: string
  created_at?: string
  updated_at?: string
}

export type AxisProductInstallationInput = {
  external_tenant_id?: string
  base_url?: string
  auth_mode?: AxisProductInstallationAuthMode
  secret_ref?: string
  enabled?: boolean
  config?: Record<string, unknown>
}

export type AxisCapabilityManifestRef = {
  capability_id: string
  version: string
  product_surface: AxisProductSurface
}

export type AxisProductIntegrationContractV1 = {
  identity: AxisProductIdentityContextV1
  installation?: AxisProductInstallation
  capabilities?: AxisCapabilityManifestRef[]
}
