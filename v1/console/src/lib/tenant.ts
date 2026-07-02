// Pure derivations for the Org + Producto cascading workspace selectors.
// A tenant = org × product; the (org, product) pair derives the active tenant
// id (sent as X-Tenant-ID). These are pure so they can be unit-tested and so
// the active tenant is computed synchronously during render (never via an
// effect that would feed back into the data refresh → infinite loop).

export type WorkspaceTenant = { id: string; org_id: string; product_surface: string }

export function deriveTenantId(
  tenants: WorkspaceTenant[] | undefined,
  orgId: string,
  productSurface: string,
): string {
  const id = (tenants ?? []).find((t) => t.org_id === orgId && t.product_surface === productSurface)?.id ?? ''
  return isUuid(id) ? id : ''
}

export function workspaceOrgs(tenants: WorkspaceTenant[] | undefined): string[] {
  return Array.from(new Set((tenants ?? []).map((t) => t.org_id)))
}

export function workspaceProducts(tenants: WorkspaceTenant[] | undefined, orgId: string): string[] {
  return (tenants ?? []).filter((t) => t.org_id === orgId).map((t) => t.product_surface)
}

// preferred picks `prefer` when available, else the first option, else undefined.
export function preferred(options: string[], prefer: string): string | undefined {
  if (options.length === 0) return undefined
  return options.includes(prefer) ? prefer : options[0]
}

export function isUuid(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value.trim())
}
