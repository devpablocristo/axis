import { Bot, RefreshCw, ShieldCheck } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { AgentsControlCenter } from './AgentsControlCenter'
import { getSession, type Session, type Tenant } from './api'

type LoadState<T> = {
  data: T | null
  loading: boolean
  error: string
}

export function App({ authSlot }: { authSlot?: ReactNode } = {}) {
  const [session, setSession] = useState<LoadState<Session>>({ data: null, loading: true, error: '' })
  const [orgId, setOrgId] = useState(localStorage.getItem('axis.v2.org_id') || '')
  const [productSurface, setProductSurface] = useState(localStorage.getItem('axis.v2.product_surface') || '')

  const refresh = useCallback(async () => {
    setSession((current) => ({ data: current.data, loading: true, error: '' }))
    try {
      const next = await getSession()
      setSession({ data: next, loading: false, error: '' })
    } catch (error) {
      setSession((current) => ({
        data: current.data,
        loading: false,
        error: error instanceof Error ? error.message : 'Could not load the session',
      }))
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const tenants = session.data?.tenants ?? []
  const workspaceOrgs = useMemo(() => unique(tenants.map((tenant) => tenant.org_id)), [tenants])
  const workspaceProducts = useMemo(
    () => unique(tenants.filter((tenant) => tenant.org_id === orgId).map((tenant) => tenant.product_surface)),
    [orgId, tenants],
  )
  const selectedTenant = useMemo(
    () => tenants.find((tenant) => tenant.org_id === orgId && tenant.product_surface === productSurface) ?? null,
    [orgId, productSurface, tenants],
  )
  const principalId = session.data?.principal_id || session.data?.actor_id || ''

  useEffect(() => {
    if (workspaceOrgs.length === 0) return
    if (!orgId || !workspaceOrgs.includes(orgId)) {
      setOrgId(workspaceOrgs[0])
    }
  }, [orgId, workspaceOrgs])

  useEffect(() => {
    if (workspaceProducts.length === 0) return
    if (!productSurface || !workspaceProducts.includes(productSurface)) {
      setProductSurface(workspaceProducts[0])
    }
  }, [productSurface, workspaceProducts])

  useEffect(() => {
    if (orgId) localStorage.setItem('axis.v2.org_id', orgId)
  }, [orgId])

  useEffect(() => {
    if (productSurface) localStorage.setItem('axis.v2.product_surface', productSurface)
  }, [productSurface])

  useEffect(() => {
    if (selectedTenant?.id) localStorage.setItem('axis.v2.tenant_id', selectedTenant.id)
  }, [selectedTenant?.id])

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <ShieldCheck aria-hidden="true" />
          <div>
            <strong>Axis</strong>
            <span>Console v2</span>
          </div>
        </div>
        <nav className="nav">
          <button type="button" className="active">
            <Bot aria-hidden="true" />
            Virployees
          </button>
        </nav>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div>
            <h1>Virployees</h1>
            <p className="axis-muted">principal: {principalId || 'loading'}</p>
          </div>
          <div className="toolbar">
            {tenants.length > 0 ? (
              <>
                <label className="topbar-org">
                  <span>Org</span>
                  <select value={orgId} onChange={(event) => setOrgId(event.target.value)}>
                    {workspaceOrgs.map((org) => (
                      <option key={org} value={org}>{org}</option>
                    ))}
                  </select>
                </label>
                <label className="topbar-org">
                  <span>Product</span>
                  <select value={productSurface} onChange={(event) => setProductSurface(event.target.value)}>
                    {workspaceProducts.map((product) => (
                      <option key={product} value={product}>{product}</option>
                    ))}
                  </select>
                </label>
              </>
            ) : null}
            <button type="button" onClick={() => void refresh()} disabled={session.loading} title="Refresh session">
              <RefreshCw aria-hidden="true" />
            </button>
            {authSlot ? <div className="auth-slot">{authSlot}</div> : null}
          </div>
        </header>

        {session.error ? <div className="alert alert-error">{session.error}</div> : null}

        {session.loading && !session.data ? (
          <div className="spinner" />
        ) : tenants.length === 0 ? (
          <section className="empty-state">No tenants are available for this user.</section>
        ) : selectedTenant == null ? (
          <section className="empty-state">No active tenant matches the selected combination.</section>
        ) : (
          <AgentsControlCenter tenantId={selectedTenant.id} principalId={principalId} />
        )}
      </main>
    </div>
  )
}

function unique(values: string[]): string[] {
  return Array.from(new Set(values.filter(Boolean))).sort((left, right) => left.localeCompare(right))
}
