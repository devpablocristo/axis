import { useCallback, useEffect, useState } from 'react'
import {
  type AxisTenant,
  type ControlOrg,
  type ControlProduct,
  createControlOrg,
  listControlOrgs,
  listControlProducts,
  listControlTenants,
  provisionTenant,
} from './api'

// ControlPlane is the platform-admin surface: it manages GLOBAL resources
// (companies/organizations, products, and tenants = org x product) via the
// /api/control/* API. Orthogonal to the active tenant/workspace.
export function ControlPlane() {
  const [orgs, setOrgs] = useState<ControlOrg[]>([])
  const [tenants, setTenants] = useState<AxisTenant[]>([])
  const [products, setProducts] = useState<ControlProduct[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const [newOrgName, setNewOrgName] = useState('')
  const [provOrg, setProvOrg] = useState('')
  const [provProduct, setProvProduct] = useState('')

  const reload = useCallback(async () => {
    setError('')
    try {
      const [o, t, p] = await Promise.all([listControlOrgs(), listControlTenants(), listControlProducts()])
      setOrgs(o)
      setTenants(t)
      setProducts(p)
      if (!provOrg && o.length > 0) setProvOrg(o[0].id)
      if (!provProduct && p.length > 0) setProvProduct(p[0].product_surface)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'error')
    }
  }, [provOrg, provProduct])

  useEffect(() => {
    void reload()
  }, [reload])

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    setError('')
    try {
      await fn()
      await reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'error')
    } finally {
      setBusy(false)
    }
  }

  const orgName = (id: string) => orgs.find((o) => o.id === id)?.name ?? id

  return (
    <section className="page-section iam-control axis-crud-host">
      <h2>Control Plane</h2>
      <p className="m-chat-muted">Recursos globales: empresas, productos y tenants (empresa × producto). Solo platform-admin.</p>
      {error && <p role="alert" className="m-chat-error">{error}</p>}

      <h3>Empresas (orgs)</h3>
      <div className="iam-control__below-actions">
        <label>
          <span>Nueva empresa</span>
          <input value={newOrgName} onChange={(e) => setNewOrgName(e.target.value)} placeholder="cristo.tech" />
        </label>
        <button type="button" disabled={busy || !newOrgName.trim()} onClick={() => void run(async () => { await createControlOrg(newOrgName.trim()); setNewOrgName('') })}>
          Crear empresa
        </button>
      </div>
      <ul>
        {orgs.map((o) => (
          <li key={o.id}>{o.name} <span className="m-chat-muted">· {o.id} · {o.status}</span></li>
        ))}
      </ul>

      <h3>Provisionar tenant (empresa × producto)</h3>
      <div className="iam-control__below-actions">
        <label>
          <span>Empresa</span>
          <select value={provOrg} onChange={(e) => setProvOrg(e.target.value)}>
            {orgs.map((o) => (<option key={o.id} value={o.id}>{o.name}</option>))}
          </select>
        </label>
        <label>
          <span>Producto</span>
          <select value={provProduct} onChange={(e) => setProvProduct(e.target.value)}>
            {products.map((p) => (<option key={p.product_surface} value={p.product_surface}>{p.name}</option>))}
          </select>
        </label>
        <button type="button" disabled={busy || !provOrg || !provProduct} onClick={() => void run(() => provisionTenant({ org_id: provOrg, product_surface: provProduct, name: `${orgName(provOrg)} / ${provProduct}` }))}>
          Provisionar
        </button>
      </div>

      <h3>Tenants</h3>
      <table className="axis-table">
        <thead><tr><th>Tenant</th><th>Empresa</th><th>Producto</th><th>Estado</th></tr></thead>
        <tbody>
          {tenants.map((t) => (
            <tr key={t.id}>
              <td>{t.name || t.id}</td>
              <td>{orgName(t.org_id)}</td>
              <td>{t.product_surface}</td>
              <td>{t.status}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
