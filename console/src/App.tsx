import { Activity, Bot, CheckCircle2, DatabaseZap, FileClock, FileText, GitPullRequestArrow, KeyRound, Layers3, ListChecks, MessageSquareText, Play, Power, RefreshCw, ShieldCheck, Sparkles, UsersRound } from 'lucide-react'
import { ChatWorkspace, type ChatAdapter, type ChatConversationDetail, type ChatConversationSummary, type ChatRequest } from '@devpablocristo/platform-chat-ui'
import '@devpablocristo/platform-chat-ui/styles.css'
import type { ReactNode } from 'react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { ActionType, AgentRun, Approval, AxisSession, AxisTenantView, BusinessModel, CapabilityRecord, CompanionAgent, CompanionJob, CompanionTask, CostSummary, Delegation, MemoryConflict, MemoryReview, MemorySummary, NexusRequest, ObservabilityEvent, Policy, Product, ProductInstallation, RunTrace, RuntimePolicy, SecurityEvalReport, ServiceHealth, axisFetch, getHealth, getSession, listIAMTenants } from './api'
import { AgentsControlCenter } from './AgentsControlCenter'
import { ControlPlane } from './ControlPlane'
import { IAMControlCenter } from './IAMControlCenter'
import { PromptsControlCenter } from './PromptScreens'
import { type LoadState, empty, load } from './lib/load'
import { deriveTenantId, workspaceOrgs as deriveWorkspaceOrgs, workspaceProducts as deriveWorkspaceProducts } from './lib/tenant'

type RouteArea = 'home' | 'chat' | 'prompts' | 'agents' | 'iam' | 'operations' | 'nexus' | 'platform' | 'control'

type Route = {
  area: RouteArea
  screen: string
}


const knownProducts = [
  { productSurface: 'companion', label: 'Companion' },
  { productSurface: 'medmory', label: 'Medmory' },
  { productSurface: 'ponti', label: 'Ponti' },
  { productSurface: 'pymes', label: 'Pymes' },
  { productSurface: 'reference', label: 'Reference' },
  { productSurface: 'shadow', label: 'Shadow' },
  { productSurface: 'argos', label: 'Argos' },
  { productSurface: 'agro', label: 'Agro' },
]

const knownProductTenants = [
  {
    accountName: 'Acme',
    productSurface: 'pymes',
    productLabel: 'Pymes',
    tenantName: 'Bikeman',
    externalTenantId: 'bikeman',
    status: 'Activo',
  },
]

type ProductOption = {
  productSurface: string
  label: string
  status: string
}

export function App({ authSlot }: { authSlot?: ReactNode } = {}) {
  const [orgId, setOrgId] = useState(localStorage.getItem('axis.org_id') || 'cristo.tech')
  const [productSurface, setProductSurface] = useState(localStorage.getItem('axis.product_surface') || 'axis')
  const [externalTenantId, setExternalTenantId] = useState(localStorage.getItem('axis.external_tenant_id') || 'bikeman')
  const [route, setRoute] = useState<Route>(() => parseCurrentRoute())
  const [session, setSession] = useState<LoadState<AxisSession | null>>(empty(null))
  const [axisOrgs, setAxisOrgs] = useState<LoadState<AxisTenantView[]>>(empty([]))
  const [health, setHealth] = useState<LoadState<ServiceHealth | null>>(empty(null))
  const [products, setProducts] = useState<LoadState<Product[]>>(empty([]))
  const [installations, setInstallations] = useState<LoadState<ProductInstallation[]>>(empty([]))
  const [approvals, setApprovals] = useState<LoadState<Approval[]>>(empty([]))
  const [requests, setRequests] = useState<LoadState<NexusRequest[]>>(empty([]))
  const [policies, setPolicies] = useState<LoadState<Policy[]>>(empty([]))
  const [actionTypes, setActionTypes] = useState<LoadState<ActionType[]>>(empty([]))
  const [delegations, setDelegations] = useState<LoadState<Delegation[]>>(empty([]))
  const [tasks, setTasks] = useState<LoadState<CompanionTask[]>>(empty([]))
  const [billingAgentRuns, setBillingAgentRuns] = useState<LoadState<AgentRun[]>>(empty([]))
  const [traces, setTraces] = useState<LoadState<RunTrace[]>>(empty([]))
  const [runtimePolicy, setRuntimePolicy] = useState<LoadState<RuntimePolicy | null>>(empty(null))
  const [agents, setAgents] = useState<LoadState<CompanionAgent[]>>(empty([]))
  const [capabilities, setCapabilities] = useState<LoadState<CapabilityRecord[]>>(empty([]))
  const [memoryConflicts, setMemoryConflicts] = useState<LoadState<MemoryConflict[]>>(empty([]))
  const [memoryReviews, setMemoryReviews] = useState<LoadState<MemoryReview[]>>(empty([]))
  const [memorySummaries, setMemorySummaries] = useState<LoadState<MemorySummary[]>>(empty([]))
  const [jobs, setJobs] = useState<LoadState<CompanionJob[]>>(empty([]))
  const [events, setEvents] = useState<LoadState<ObservabilityEvent[]>>(empty([]))
  const [costs, setCosts] = useState<LoadState<CostSummary | null>>(empty(null))
  const [securityReports, setSecurityReports] = useState<LoadState<SecurityEvalReport[]>>(empty([]))
  const [businessModel, setBusinessModel] = useState<LoadState<BusinessModel | null>>(empty(null))
  const [actionMessage, setActionMessage] = useState('')

  // Active tenant = the (org, product) pair resolved against the user's tenants.
  // Derived synchronously (NOT useState) so it's always current in the same
  // render as an org/product change. It MUST NOT be a refresh() dep: refresh
  // transiently clears session.data (loading=true) which would recompute this
  // to '' and re-trigger refresh → infinite fetch loop (ERR_INSUFFICIENT_RESOURCES).
  const tenantId = useMemo(
    () => deriveTenantId(session.data?.tenants, orgId, productSurface),
    [session.data?.tenants, orgId, productSurface],
  )

  const refresh = useCallback(async () => {
    localStorage.setItem('axis.org_id', orgId)
    localStorage.setItem('axis.tenant_id', tenantId)
    localStorage.setItem('axis.product_surface', productSurface)
    localStorage.setItem('axis.external_tenant_id', externalTenantId)
    const productInit = productHeaders(productSurface)
    await Promise.all([
      load(setSession, () => getSession(), null),
      load(setAxisOrgs, () => listIAMTenants(orgId), []),
      load(setHealth, () => getHealth(), null),
      load(setProducts, async () => (await axisFetch<{ products: Product[] }>('/api/companion/v1/products', orgId, productInit)).products ?? [], []),
      load(setInstallations, async () => (await axisFetch<{ installations: ProductInstallation[] }>(`/api/companion/v1/product-installations?org_id=${encodeURIComponent(orgId)}`, orgId, productInit)).installations ?? [], []),
      load(setApprovals, async () => (await axisFetch<{ data: Approval[] }>('/api/nexus/v1/approvals/pending', orgId)).data ?? [], []),
      load(setRequests, async () => (await axisFetch<{ data: NexusRequest[] }>('/api/nexus/v1/requests?limit=12', orgId)).data ?? [], []),
      load(setPolicies, async () => (await axisFetch<{ data: Policy[] }>('/api/nexus/v1/policies', orgId)).data ?? [], []),
      load(setActionTypes, async () => (await axisFetch<{ data: ActionType[] }>('/api/nexus/v1/action-types', orgId)).data ?? [], []),
      load(setDelegations, async () => (await axisFetch<{ data: Delegation[] }>('/api/nexus/v1/delegations', orgId)).data ?? [], []),
      load(setTasks, async () => filterByProduct((await axisFetch<{ data: CompanionTask[] }>(withProduct('/api/companion/v1/tasks?limit=12', productSurface), orgId, productInit)).data ?? [], productSurface), []),
      load(setBillingAgentRuns, async () => filterByProduct((await axisFetch<{ data: AgentRun[] }>(withProduct('/api/companion/v1/agent-runs?agent_id=billing_agent&limit=20', productSurface), orgId, productInit)).data ?? [], productSurface), []),
      load(setTraces, async () => filterByProduct((await axisFetch<{ traces: RunTrace[] }>(withProduct('/api/companion/v1/run-traces?limit=12', productSurface), orgId, productInit)).traces ?? [], productSurface), []),
      load(setRuntimePolicy, () => axisFetch<RuntimePolicy>('/api/companion/v1/runtime/policy', orgId, productInit), null),
      load(setAgents, async () => filterByProduct((await axisFetch<{ data: CompanionAgent[] }>(withProduct('/api/companion/v1/agents', productSurface), orgId, productInit)).data ?? [], productSurface), []),
      load(setCapabilities, async () => filterCapabilities((await axisFetch<{ capabilities: CapabilityRecord[] }>(withProduct('/api/companion/v1/capabilities?limit=100', productSurface), orgId, productInit)).capabilities ?? [], productSurface).slice(0, 12), []),
      load(setMemoryConflicts, async () => filterByProduct((await axisFetch<{ conflicts: MemoryConflict[] }>(withProduct('/api/companion/v1/memory/conflicts?limit=12', productSurface), orgId, productInit)).conflicts ?? [], productSurface), []),
      load(setMemoryReviews, async () => filterByProduct((await axisFetch<{ reviews: MemoryReview[] }>(withProduct('/api/companion/v1/memory/reviews?limit=12', productSurface), orgId, productInit)).reviews ?? [], productSurface), []),
      load(setMemorySummaries, async () => filterByProduct((await axisFetch<{ summaries: MemorySummary[] }>(withProduct('/api/companion/v1/memory/summaries?limit=12', productSurface), orgId, productInit)).summaries ?? [], productSurface), []),
      load(setJobs, async () => filterByProduct((await axisFetch<{ jobs: CompanionJob[] }>(withProduct('/api/companion/v1/jobs?limit=12', productSurface), orgId, productInit)).jobs ?? [], productSurface), []),
      load(setEvents, async () => filterByProduct((await axisFetch<{ events: ObservabilityEvent[] }>(withProduct('/api/companion/v1/observability/events?limit=12', productSurface), orgId, productInit)).events ?? [], productSurface), []),
      load(setCosts, () => axisFetch<CostSummary>(withProduct('/api/companion/v1/runtime/costs', productSurface), orgId, productInit), null),
      load(setSecurityReports, async () => (await axisFetch<{ reports: SecurityEvalReport[] }>(withProduct('/api/companion/v1/security-evals/reports?limit=12', productSurface), orgId, productInit)).reports ?? [], []),
      load(setBusinessModel, () => axisFetch<BusinessModel>('/api/companion/v1/business-model', orgId, productInit), null)
    ])
    // eslint-disable-next-line react-hooks/exhaustive-deps -- tenantId is derived
    // from orgId+productSurface (a refresh dep); adding it loops (see useMemo above).
  }, [externalTenantId, orgId, productSurface])

  const runAction = useCallback(async (label: string, fn: () => Promise<unknown>) => {
    setActionMessage(`${label}: running`)
    try {
      await fn()
      setActionMessage(`${label}: done`)
      await refresh()
    } catch (error) {
      setActionMessage(`${label}: ${error instanceof Error ? error.message : 'failed'}`)
    }
  }, [refresh])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // Legacy org auto-select (pre-tenancy model). Only applies when the user has
  // NO tenants — otherwise the Workspace selectors below are the single source
  // of truth for orgId, and these would fight them into an infinite re-render.
  useEffect(() => {
    if ((session.data?.tenants?.length ?? 0) > 0) return
    const sessionOrgId = session.data?.org_id
    if (sessionOrgId && sessionOrgId !== orgId && !session.data?.scopes?.includes('axis:cross_org')) {
      setOrgId(sessionOrgId)
    }
  }, [orgId, session.data?.org_id, session.data?.scopes, session.data?.tenants])

  useEffect(() => {
    if ((session.data?.tenants?.length ?? 0) > 0) return
    const availableOrgs = axisOrgs.data.length > 0 ? axisOrgs.data : session.data?.orgs ?? []
    if (availableOrgs.length === 0) return
    if (availableOrgs.some((org) => org.id === orgId)) return
    setOrgId(availableOrgs[0].id)
  }, [axisOrgs.data, orgId, session.data?.orgs, session.data?.tenants])

  // Active workspace = tenant (org x product). Persist + auto-select the first
  // tenant the user belongs to. axisFetch sends it as X-Tenant-ID.
  useEffect(() => {
    localStorage.setItem('axis.tenant_id', tenantId)
  }, [tenantId])

  // Workspace = two cascading selectors: Org (company) + Producto. The Org
  // determines the available products; the (org, product) pair derives the
  // active tenant (X-Tenant-ID). Default preference: cristo.tech + axis.
  const workspaceOrgs = useMemo(() => deriveWorkspaceOrgs(session.data?.tenants), [session.data?.tenants])
  const workspaceProducts = useMemo(
    () => deriveWorkspaceProducts(session.data?.tenants, orgId),
    [session.data?.tenants, orgId],
  )

  useEffect(() => {
    if (workspaceOrgs.length === 0 || workspaceOrgs.includes(orgId)) return
    setOrgId(workspaceOrgs.includes('cristo.tech') ? 'cristo.tech' : workspaceOrgs[0])
  }, [workspaceOrgs, orgId])

  useEffect(() => {
    if (workspaceProducts.length === 0 || workspaceProducts.includes(productSurface)) return
    setProductSurface(workspaceProducts.includes('axis') ? 'axis' : workspaceProducts[0])
  }, [workspaceProducts, productSurface])

  const tenantOptions = useMemo(() => buildTenantOptions(productSurface), [productSurface])

  useEffect(() => {
    if (tenantOptions.some((tenant) => tenant.externalTenantId === externalTenantId)) return
    setExternalTenantId(tenantOptions[0]?.externalTenantId ?? '')
  }, [externalTenantId, tenantOptions])

  useEffect(() => {
    const syncRoute = () => setRoute(parseCurrentRoute())
    window.addEventListener('popstate', syncRoute)
    return () => {
      window.removeEventListener('popstate', syncRoute)
    }
  }, [])

  const riskCounts = useMemo(() => {
    return requests.data.reduce<Record<string, number>>((acc, item) => {
      const risk = item.risk_level || 'unknown'
      acc[risk] = (acc[risk] ?? 0) + 1
      return acc
    }, {})
  }, [requests.data])

  const productOptions = useMemo(() => buildProductOptions(products.data, installations.data), [products.data, installations.data])
  const orgOptions = useMemo<Array<{ id: string; name: string; status: string }>>(() => {
    if (axisOrgs.data.length > 0) return axisOrgs.data.map((org) => ({ id: org.id, name: org.name, status: org.status }))
    if (session.data?.orgs?.length) return session.data.orgs.map((org) => ({ id: org.id, name: org.name, status: org.status }))
    return [{ id: orgId, name: orgId, status: 'active' }]
  }, [axisOrgs.data, orgId, session.data?.orgs])
  const selectedOrgOption = orgOptions.find((item) => item.id === orgId)
  const selectedProductOption = productOptions.find((item) => item.productSurface === productSurface)
  const selectedTenantOption = tenantOptions.find((item) => item.externalTenantId === externalTenantId)
  const simpleControlHeader = route.area === 'agents' || route.area === 'chat' || route.area === 'prompts' || route.area === 'control'
  const showGlobalContext = route.area !== 'iam' && !simpleControlHeader
  const canViewIAM = Boolean(session.data?.scopes?.some((scope) => scope === 'axis:orgs:admin' || scope === 'axis:users:admin'))
  const canViewControl = Boolean(session.data?.platform_roles?.some((role) => role === 'platform_admin' || role === 'owner'))
  const title = pageTitle(route)
  const chatAdapter = useMemo<ChatAdapter>(() => axisChatAdapter(orgId, productSurface), [orgId, productSurface])
  const navigate = useCallback((next: Route) => {
    window.history.pushState(null, '', routePath(next))
    setRoute(next)
  }, [])

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <ShieldCheck aria-hidden="true" />
          <div>
            <strong>Axis</strong>
            <span>Console</span>
          </div>
        </div>
        <nav className="nav">
          <button type="button" className={route.area === 'home' ? 'active' : ''} onClick={() => navigate({ area: 'home', screen: 'summary' })}><Activity aria-hidden="true" />Inicio</button>
          <button type="button" className={route.area === 'chat' ? 'active' : ''} onClick={() => navigate({ area: 'chat', screen: 'workspace' })}><MessageSquareText aria-hidden="true" />Chat</button>
          <button type="button" className={route.area === 'prompts' ? 'active' : ''} onClick={() => navigate({ area: 'prompts', screen: 'product' })}><FileText aria-hidden="true" />Prompts</button>
          <button type="button" className={route.area === 'agents' ? 'active' : ''} onClick={() => navigate({ area: 'agents', screen: 'list' })}><Bot aria-hidden="true" />Agentes</button>
          {canViewIAM && <button type="button" className={route.area === 'iam' ? 'active' : ''} onClick={() => navigate({ area: 'iam', screen: 'internal' })}><KeyRound aria-hidden="true" />IAM</button>}
          <button type="button" className={route.area === 'operations' ? 'active' : ''} onClick={() => navigate({ area: 'operations', screen: 'runs' })}><Activity aria-hidden="true" />Operación</button>
          <button type="button" className={route.area === 'nexus' ? 'active' : ''} onClick={() => navigate({ area: 'nexus', screen: 'approvals' })}><GitPullRequestArrow aria-hidden="true" />Nexus</button>
          <button type="button" className={route.area === 'platform' ? 'active' : ''} onClick={() => navigate({ area: 'platform', screen: 'runtime' })}><KeyRound aria-hidden="true" />Plataforma</button>
          {canViewControl && <button type="button" className={route.area === 'control' ? 'active' : ''} onClick={() => navigate({ area: 'control', screen: 'home' })}><Layers3 aria-hidden="true" />Control Plane</button>}
        </nav>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div>
            <h1>{title}</h1>
            {route.area !== 'iam' && !simpleControlHeader && <p>{session.data?.actor_id ?? 'local-dev-admin'}</p>}
          </div>
          {(showGlobalContext || authSlot) && (
            <div className="toolbar">
              {showGlobalContext && (
                <>
                  <label>
                    <span>Tenant</span>
                    <select value={externalTenantId} onChange={(event) => setExternalTenantId(event.target.value)}>
                      {tenantOptions.map((tenant) => (
                        <option key={tenant.externalTenantId} value={tenant.externalTenantId}>
                          {tenant.tenantName} · {tenant.status}
                        </option>
                      ))}
                    </select>
                  </label>
                </>
              )}
              {showGlobalContext && (
                <button type="button" onClick={() => void refresh()} aria-label="Refresh">
                  <RefreshCw aria-hidden="true" />
                </button>
              )}
              {(session.data?.tenants?.length ?? 0) > 0 && (
                <>
                  <label className="topbar-org">
                    <span>Org</span>
                    <select value={orgId} onChange={(event) => setOrgId(event.target.value)}>
                      {workspaceOrgs.map((o) => (
                        <option key={o} value={o}>{o}</option>
                      ))}
                    </select>
                  </label>
                  <label className="topbar-org">
                    <span>Producto</span>
                    <select value={productSurface} onChange={(event) => setProductSurface(event.target.value)}>
                      {workspaceProducts.map((p) => (
                        <option key={p} value={p}>{p}</option>
                      ))}
                    </select>
                  </label>
                </>
              )}
              {authSlot && <div className="auth-slot">{authSlot}</div>}
            </div>
          )}
        </header>

        {showGlobalContext && (
          <section className="health-row">
            <HealthPill label="BFF" value="ok" />
            <HealthPill label="Plataforma IA" value={health.data?.companion ?? health.error ?? 'loading'} />
            <HealthPill label="Nexus" value={health.data?.nexus ?? health.error ?? 'loading'} />
            <span className="scope-pill"><b>Producto</b>{selectedProductOption ? `${selectedProductOption.label} · ${selectedProductOption.status}` : productSurface}</span>
            <span className="scope-pill">{session.data?.auth_method ?? 'dev'}</span>
            {actionMessage && <span className="scope-pill">{actionMessage}</span>}
          </section>
        )}

        {route.area === 'home' && (
          <section className="page-section">
            <div className="metrics-grid">
              <Metric icon={<CheckCircle2 />} label="Aprobaciones" value={approvals.data.length} tone="green" />
              <Metric icon={<FileClock />} label="Requests" value={requests.data.length} tone="blue" />
              <Metric icon={<Sparkles />} label="Agentes" value={agents.data.length} tone="violet" />
              <Metric icon={<DatabaseZap />} label="Capabilities" value={capabilities.data.length} tone="amber" />
            </div>
            <div className="screen-grid two">
              <Panel title="Producto seleccionado" icon={<Layers3 />} state={products}>
                <Table columns={['campo', 'valor']} rows={[
                  ['cuenta Axis', selectedOrgOption?.name ?? orgId],
                  ['axis_account_id', orgId],
                  ['producto', selectedProductOption?.label ?? productSurface],
                  ['product_surface', productSurface],
                  ['tenant', selectedTenantOption?.tenantName ?? externalTenantId],
                  ['external_tenant_id', selectedTenantOption?.externalTenantId ?? '-'],
                  ['estado', selectedProductOption?.status ?? '-'],
                ]} />
              </Panel>
              <Panel title="Últimas corridas" icon={<Activity />} state={tasks}>
                <Table columns={['tarea', 'estado', 'canal']} rows={tasks.data.slice(0, 6).map((item) => [item.title, item.status, item.channel ?? 'api'])} />
              </Panel>
            </div>
            <div className="screen-grid">
              <Panel title="Salud de plataforma" icon={<ShieldCheck />} state={health}>
                <Table columns={['servicio', 'estado']} rows={[
                  ['BFF', 'ok'],
                  ['Plataforma IA', health.data?.companion ?? health.error ?? 'loading'],
                  ['Nexus', health.data?.nexus ?? health.error ?? 'loading'],
                ]} />
              </Panel>
            </div>
          </section>
        )}

        {route.area === 'chat' && (
          <section className="page-section">
            <ChatWorkspace
              adapter={chatAdapter}
              baseRequest={{ productSurface, workspace: { org_id: orgId, product_surface: productSurface } }}
              labels={{
                title: '',
                lead: '',
                conversations: 'Conversaciones',
                emptyConversations: 'Sin conversaciones',
                emptyThread: 'Escribí una consulta para Axis.',
                inputPlaceholder: 'Mensaje para Axis',
                send: 'Enviar',
                sending: 'Enviando...',
                newConversation: 'Nueva',
                loadingHistory: 'Cargando historial...',
                confirmPending: 'Confirmar',
              }}
              nowLabel={() => new Date().toLocaleString()}
            />
          </section>
        )}

        {route.area === 'nexus' && (
          <section className="page-section">
            <ScreenNav items={[
              ['approvals', 'Aprobaciones'],
              ['requests', 'Requests'],
              ['policies', 'Policies'],
              ['action-types', 'Action Types'],
              ['delegations', 'Delegations'],
              ['risk', 'Riesgo']
            ]} base="nexus" active={route.screen} onNavigate={navigate} />

            {route.screen === 'approvals' && (
              <div className="screen-grid">
                <Panel title="Aprobaciones" icon={<ListChecks />} state={approvals}>
                  <Table columns={['status', 'request', 'expires']} rows={approvals.data.map((item) => [item.status, short(item.request_id), date(item.expires_at)])} />
                </Panel>
              </div>
            )}
            {route.screen === 'requests' && (
              <div className="screen-grid">
                <Panel title="Requests" icon={<GitPullRequestArrow />} state={requests}>
                  <Table columns={['action', 'decision', 'risk', 'status']} rows={requests.data.map((item) => [item.action_type, item.decision, item.risk_level, item.status])} />
                </Panel>
              </div>
            )}
            {route.screen === 'policies' && (
              <div className="screen-grid">
                <Panel title="Policies" icon={<ShieldCheck />} state={policies}>
                  <Table columns={['name', 'effect', 'mode', 'enabled']} rows={policies.data.map((item) => [item.name, item.effect, item.mode, item.enabled ? 'yes' : 'no'])} />
                </Panel>
              </div>
            )}
            {route.screen === 'risk' && (
              <div className="screen-grid">
                <Panel title="Riesgo" icon={<Activity />} state={requests}>
                  <div className="risk-list">
                    {Object.entries(riskCounts).map(([risk, count]) => (
                      <span key={risk}><b>{risk}</b>{count}</span>
                    ))}
                  </div>
                </Panel>
              </div>
            )}
            {route.screen === 'action-types' && (
              <div className="screen-grid">
                <Panel title="Action Types" icon={<Layers3 />} state={actionTypes}>
                  <Table columns={['name', 'category', 'risk', 'enabled']} rows={actionTypes.data.map((item) => [item.name, item.category, item.risk_class, item.enabled ? 'yes' : 'no'])} />
                </Panel>
              </div>
            )}
            {route.screen === 'delegations' && (
              <div className="screen-grid">
                <Panel title="Delegations" icon={<UsersRound />} state={delegations}>
                  <Table columns={['owner', 'agent', 'risk', 'enabled']} rows={delegations.data.map((item) => [item.owner_id, item.agent_id, item.max_risk_class ?? '-', item.enabled ? 'yes' : 'no'])} />
                </Panel>
              </div>
            )}
          </section>
        )}

        {route.area === 'platform' && (
          <section className="page-section">
            <ScreenNav items={[
              ['runtime', 'Política runtime'],
              ['capabilities', 'Capabilities'],
              ['business', 'Modelo del producto'],
              ['health', 'Health']
            ]} base="platform" active={route.screen} onNavigate={navigate} />
            {route.screen === 'runtime' && (
              <div className="screen-grid">
                <Panel title="Política runtime" icon={<ShieldCheck />} state={runtimePolicy}>
                  <div className="panel-actions">
                    <button type="button" onClick={() => void runAction('runtime kill switch', () => axisFetch('/api/companion/v1/runtime/policy', orgId, { method: 'PUT', headers: { 'X-Product-Surface': productSurface }, body: JSON.stringify({ kill_switch: !runtimePolicy.data?.kill_switch }) }))}>
                      <Power aria-hidden="true" />Kill switch
                    </button>
                  </div>
                  <Table columns={['setting', 'value']} rows={[
                    ['enabled', runtimePolicy.data?.enabled ? 'yes' : 'no'],
                    ['kill switch', runtimePolicy.data?.kill_switch ? 'on' : 'off'],
                    ['autonomy', runtimePolicy.data?.max_autonomy ?? '-'],
                    ['risk', runtimePolicy.data?.control_plane?.max_risk_class ?? '-'],
                    ['vector store', runtimePolicy.data?.control_plane?.embedding?.vector_store ?? '-']
                  ]} />
                </Panel>
              </div>
            )}
            {route.screen === 'capabilities' && (
              <div className="screen-grid">
                <Panel title="Capabilities" icon={<Layers3 />} state={capabilities}>
                  <div className="panel-actions">
                    <button type="button" disabled={!capabilities.data.some((item) => item.status === 'draft')} onClick={() => {
                      const cap = capabilities.data.find((item) => item.status === 'draft')
                      if (cap) void runAction('promote manifest', () => axisFetch(`/api/companion/v1/capabilities/${encodeURIComponent(cap.manifest.capability_id)}/versions/${encodeURIComponent(cap.manifest.version)}/promote`, orgId, { method: 'POST', headers: { 'X-Product-Surface': productSurface }, body: '{}' }))
                    }}>
                      <CheckCircle2 aria-hidden="true" />Promote draft
                    </button>
                  </div>
                  <Table columns={['capability', 'version', 'risk', 'approval']} rows={capabilities.data.map((item) => [item.manifest.display_name || item.manifest.capability_id, item.manifest.version, item.manifest.risk_level, item.manifest.approval_required ? 'yes' : 'no'])} />
                </Panel>
              </div>
            )}
            {route.screen === 'business' && (
              <div className="screen-grid">
                <Panel title="Modelo del producto" icon={<DatabaseZap />} state={businessModel}>
                  <Table columns={['area', 'count']} rows={[
                    ['areas', countOf(businessModel.data?.areas)],
                    ['roles', countOf(businessModel.data?.roles)],
                    ['workflows', countOf(businessModel.data?.workflows)],
                    ['rules', countOf(businessModel.data?.rules)]
                  ]} />
                </Panel>
              </div>
            )}
            {route.screen === 'health' && (
              <div className="screen-grid">
                <Panel title="Health" icon={<ShieldCheck />} state={health}>
                  <Table columns={['servicio', 'estado']} rows={[
                    ['BFF', 'ok'],
                    ['Plataforma IA', health.data?.companion ?? health.error ?? 'loading'],
                    ['Nexus', health.data?.nexus ?? health.error ?? 'loading'],
                  ]} />
                </Panel>
              </div>
            )}
          </section>
        )}

        {route.area === 'agents' && (
          <AgentsControlCenter orgId={orgId} />
        )}

        {route.area === 'control' && canViewControl && (
          <ControlPlane />
        )}

        {route.area === 'iam' && canViewIAM && (
          <IAMControlCenter
            orgId={orgId}
            productSurface={productSurface}
            orgs={orgOptions}
            onOrgChange={setOrgId}
            onRefreshShell={refresh}
          />
        )}

        {route.area === 'iam' && !canViewIAM && (
          <section className="empty-state">No tenés permisos para administrar IAM.</section>
        )}

        {route.area === 'prompts' && (
          <PromptsControlCenter
            orgId={orgId}
            productSurface={productSurface}
            agents={agents.data}
            initialSection={route.screen === 'agents' ? 'agents' : 'product'}
          />
        )}

        {route.area === 'operations' && (
          <section className="page-section">
            <ScreenNav items={[
              ['runs', 'Corridas'],
              ['traces', 'Trazas'],
              ['memory', 'Memoria'],
              ['jobs', 'Jobs'],
              ['observability', 'Observabilidad'],
              ['cost', 'Costos'],
              ['security', 'Evals de seguridad']
            ]} base="operations" active={route.screen} onNavigate={navigate} />
            {route.screen === 'runs' && (
              <div className="screen-grid two">
                <Panel title="Corridas / tareas" icon={<Bot />} state={tasks}>
                  <Table columns={['título', 'estado', 'agente', 'canal']} rows={tasks.data.map((item) => [item.title, item.status, item.agent_id ?? '-', item.channel ?? 'api'])} />
                </Panel>
                <Panel title="Corridas de Billing Agent" icon={<Bot />} state={billingAgentRuns}>
                  <Table columns={['recomendación', 'run type', 'task', 'tools', 'nexus']} rows={billingAgentRuns.data.map((item) => [
                    item.recommendation || '-',
                    item.run_type || item.task?.run_type || '-',
                    short(item.task_id || item.id),
                    item.tool_calls?.length ?? 0,
                    item.nexus_request_id ? short(item.nexus_request_id) : '-',
                  ])} />
                </Panel>
              </div>
            )}
            {route.screen === 'traces' && (
              <div className="screen-grid">
                <Panel title="Trazas" icon={<Sparkles />} state={traces}>
                  <Table columns={['producto', 'intent', 'estado', 'inicio']} rows={traces.data.map((item) => [item.product_surface ?? '-', item.intent ?? '-', item.status ?? '-', date(item.started_at)])} />
                </Panel>
              </div>
            )}
            {route.screen === 'memory' && (
              <div className="screen-grid two">
                <Panel title="Revisión de memoria" icon={<DatabaseZap />} state={memoryConflicts}>
                  <div className="panel-actions">
                    <button type="button" disabled={!memoryReviews.data.length} onClick={() => {
                      const review = memoryReviews.data[0]
                      if (review) void runAction('apply memory review', () => axisFetch(`/api/companion/v1/memory/reviews/${encodeURIComponent(review.id)}/apply`, orgId, { method: 'POST', headers: { 'X-Product-Surface': productSurface }, body: '{}' }))
                    }}>
                      <CheckCircle2 aria-hidden="true" />Apply review
                    </button>
                  </div>
                  <Table columns={['key', 'kind', 'confidence', 'updated']} rows={memoryConflicts.data.map((item) => [item.key, item.kind, item.confidence.toFixed(2), date(item.updated_at)])} />
                </Panel>
                <Panel title="Resúmenes de memoria" icon={<FileClock />} state={memorySummaries}>
                  <Table columns={['scope', 'type', 'version', 'sources']} rows={memorySummaries.data.map((item) => [`${item.scope_type}:${short(item.scope_id)}`, item.summary_type, item.version, item.source_count])} />
                </Panel>
              </div>
            )}
            {route.screen === 'jobs' && (
              <div className="screen-grid">
                <Panel title="Jobs" icon={<Activity />} state={jobs}>
                  <Table columns={['kind', 'status', 'attempts', 'created']} rows={jobs.data.map((item) => [item.kind, item.status, `${item.attempts}/${item.max_attempts}`, date(item.created_at)])} />
                </Panel>
              </div>
            )}
            {route.screen === 'observability' && (
              <div className="screen-grid">
                <Panel title="Observabilidad" icon={<ListChecks />} state={events}>
                  <Table columns={['type', 'name', 'severity', 'time']} rows={events.data.map((item) => [item.event_type, item.event_name, item.severity, date(item.occurred_at)])} />
                </Panel>
              </div>
            )}
            {route.screen === 'cost' && (
              <div className="screen-grid">
                <Panel title="Costos" icon={<DatabaseZap />} state={costs}>
                  <Table columns={['metric', 'value']} rows={[
                    ['period', costs.data?.period ?? '-'],
                    ['tokens', costs.data?.estimated_tokens ?? 0],
                    ['cents', costs.data?.estimated_cost_cents ?? 0],
                    ['llm calls', costs.data?.llm_calls ?? 0],
                    ['tool calls', costs.data?.tool_calls ?? 0]
                  ]} />
                </Panel>
              </div>
            )}
            {route.screen === 'security' && (
              <div className="screen-grid">
                <Panel title="Evals de seguridad" icon={<ShieldCheck />} state={securityReports}>
                  <div className="panel-actions">
                    <button type="button" onClick={() => void runAction('security eval', () => axisFetch('/api/companion/v1/security-evals/runs', orgId, { method: 'POST', headers: { 'X-Product-Surface': productSurface }, body: JSON.stringify({ suite: 'security-adversarial', product_surface: productSurface }) }))}>
                      <Play aria-hidden="true" />Run suite
                    </button>
                  </div>
                  <Table columns={['suite', 'status', 'score', 'created']} rows={securityReports.data.map((item) => [item.suite, item.status, `${Math.round(item.score * 100)}%`, date(item.created_at)])} />
                </Panel>
              </div>
            )}
          </section>
        )}
      </main>
    </div>
  )
}

function parseCurrentRoute(): Route {
  if (window.location.hash.startsWith('#')) {
    const fromHash = parseRoutePath(window.location.hash.replace(/^#\/?/, ''))
    window.history.replaceState(null, '', routePath(fromHash))
    return fromHash
  }
  return parseRoutePath(window.location.pathname)
}

function parseRoutePath(path: string): Route {
  const raw = path.replace(/^\/+/, '').replace(/\/+$/, '').trim()
  if (raw === '') {
    return { area: 'home', screen: 'summary' }
  }
  const [areaRaw, screenRaw] = raw.split('/')
  const area = normalizeRouteArea(areaRaw)
  const screens: Record<RouteArea, string[]> = {
    home: ['summary'],
    prompts: ['product', 'agents'],
    chat: ['workspace'],
    agents: ['list'],
    iam: ['internal', 'clients', 'users'],
    operations: ['runs', 'traces', 'memory', 'jobs', 'observability', 'cost', 'security'],
    nexus: ['approvals', 'requests', 'policies', 'action-types', 'delegations', 'risk'],
    platform: ['runtime', 'capabilities', 'business', 'health'],
    control: ['home']
  }
  const normalizedScreen = normalizeRouteScreen(area, screenRaw)
  const screen = screens[area].includes(normalizedScreen) ? normalizedScreen : screens[area][0]
  return { area, screen }
}

function normalizeRouteArea(value: string): RouteArea {
  if (value === 'overview') return 'home'
  if (value === 'companion') return 'platform'
  if (value === 'access') return 'nexus'
  if (value === 'home' || value === 'chat' || value === 'prompts' || value === 'agents' || value === 'iam' || value === 'operations' || value === 'nexus' || value === 'platform' || value === 'control') {
    return value
  }
  return 'home'
}

function normalizeRouteScreen(area: RouteArea, value: string | undefined) {
  if (!value) return ''
  if (area === 'prompts' && value === 'assist-packs') return 'product'
  if (area === 'prompts' && value === 'agent-profiles') return 'agents'
  if (area === 'platform' && value === 'control') return 'runtime'
  if (area === 'platform' && value === 'tasks') return 'runtime'
  if (area === 'operations' && value === 'billing-agent') return 'runs'
  if (area === 'agents' && value === 'billing_agent') return 'list'
  if (area === 'iam' && value === 'orgs') return 'internal'
  return value
}

function routePath(route: Route) {
  if (route.area === 'home') {
    return '/home'
  }
  if (route.area === 'chat') {
    return '/chat'
  }
  // URLs bare: estas secciones viven en /area pelado SIEMPRE; los tabs cambian
  // el contenido in-page sin ensuciar la URL.
  if (route.area === 'prompts') {
    return '/prompts'
  }
  if (route.area === 'agents') {
    return '/agents'
  }
  if (route.area === 'iam') {
    return '/iam'
  }
  if (route.area === 'control') {
    return '/control'
  }
  return `/${route.area}/${route.screen}`
}

function pageTitle(route: Route) {
  switch (route.area) {
    case 'home':
      return 'Inicio'
    case 'chat':
      return 'Chat'
    case 'nexus':
      return 'Nexus'
    case 'agents':
      return 'Agentes'
    case 'iam':
      return 'IAM'
    case 'platform':
      return 'Plataforma IA'
    case 'prompts':
      return 'Prompts'
    case 'operations':
      return 'Operación'
    default:
      return 'Inicio'
  }
}

function ScreenNav(props: { base: RouteArea; active: string; items: Array<[string, string]>; onNavigate: (route: Route) => void }) {
  return (
    <nav className="screen-nav" aria-label={`${props.base} screens`}>
      {props.items.map(([id, label]) => (
        <button key={id} type="button" className={props.active === id ? 'active' : ''} onClick={() => props.onNavigate({ area: props.base, screen: id })}>
          {label}
        </button>
      ))}
    </nav>
  )
}


function Metric(props: { icon: ReactNode; label: string; value: number; tone: string }) {
  return (
    <article className={`metric ${props.tone}`}>
      {props.icon}
      <span>{props.label}</span>
      <strong>{props.value}</strong>
    </article>
  )
}

function HealthPill(props: { label: string; value: string }) {
  const ok = props.value === 'ok'
  return <span className={`health ${ok ? 'ok' : 'warn'}`}><b>{props.label}</b>{props.value}</span>
}

function Panel<T>(props: { title: string; icon: ReactNode; state: LoadState<T>; children: ReactNode }) {
  return (
    <article className="panel">
      <header>
        <h2>{props.icon}{props.title}</h2>
        {props.state.loading && <span className="status">Loading</span>}
        {props.state.error && <span className="status error">{props.state.error}</span>}
      </header>
      {props.children}
    </article>
  )
}

function Table(props: { columns: string[]; rows: Array<Array<string | number>> }) {
  if (props.rows.length === 0) {
    return <div className="empty">Sin datos para este producto</div>
  }
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>{props.columns.map((column) => <th key={column}>{column}</th>)}</tr>
        </thead>
        <tbody>
          {props.rows.map((row, index) => (
            <tr key={index}>
              {row.map((cell, cellIndex) => <td key={cellIndex}>{cell}</td>)}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function countOf(value: unknown[] | undefined) {
  return Array.isArray(value) ? value.length : 0
}

function short(value: string) {
  return value ? value.slice(0, 8) : '-'
}

function date(value?: string) {
  if (!value) return '-'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return '-'
  return parsed.toLocaleString()
}

function productHeaders(productSurface: string): RequestInit {
  return { headers: { 'X-Product-Surface': productSurface } }
}

function withProduct(path: string, productSurface: string) {
  const separator = path.includes('?') ? '&' : '?'
  return `${path}${separator}product_surface=${encodeURIComponent(productSurface)}`
}

function filterByProduct<T extends { product_surface?: string }>(items: T[], productSurface: string) {
  return items.filter((item) => !item.product_surface || sameProduct(item.product_surface, productSurface))
}

function filterCapabilities(items: CapabilityRecord[], productSurface: string) {
  return items.filter((item) => !item.manifest.product_surface || sameProduct(item.manifest.product_surface, productSurface))
}

function sameProduct(value: string | undefined, productSurface: string) {
  return (value || '').trim().toLowerCase() === productSurface.trim().toLowerCase()
}

function buildProductOptions(products: Product[], installations: ProductInstallation[]): ProductOption[] {
  const registered = new Map(products.map((product) => [product.product_surface, product]))
  const installed = new Set(
    installations
      .filter((installation) => installation.enabled)
      .map((installation) => installation.product_surface),
  )
  return knownProducts.map((known) => {
    const product = registered.get(known.productSurface)
    const isInstalled = installed.has(known.productSurface)
    const status = isInstalled
      ? 'Instalado'
      : product
        ? product.status === 'active'
          ? 'Activo · no instalado'
          : 'No instalado'
        : 'No registrado'
    return {
      productSurface: known.productSurface,
      label: product?.display_name || known.label,
      status,
    }
  })
}

function buildTenantOptions(productSurface: string) {
  const tenants = knownProductTenants.filter((tenant) => tenant.productSurface === productSurface)
  if (tenants.length > 0) return tenants
  return [
    {
      accountName: '',
      productSurface,
      productLabel: productSurface,
      tenantName: 'Sin tenant',
      externalTenantId: 'none',
      status: '-',
    },
  ]
}

function axisChatAdapter(orgId: string, productSurface: string): ChatAdapter {
  return {
    sendMessage: async (input: ChatRequest) => {
      const payload = {
        message: input.message,
        chat_id: input.chatId ?? null,
        task_id: input.taskId ?? null,
        agent_id: input.agentId || undefined,
        product_surface: productSurface,
        route_hint: input.routeHint || undefined,
        confirmed_actions: input.confirmedActions ?? [],
        workspace: input.workspace ?? { org_id: orgId, product_surface: productSurface },
      }
      const response = await axisFetch<Record<string, unknown>>('/api/companion/v1/chat', orgId, {
        method: 'POST',
        headers: { 'X-Product-Surface': productSurface },
        body: JSON.stringify(payload),
      })
      return {
        chatId: stringField(response, 'chat_id') ?? stringField(response, 'chatId') ?? undefined,
        taskId: stringField(response, 'task_id') ?? stringField(response, 'taskId') ?? undefined,
        runId: stringField(response, 'run_id') ?? stringField(response, 'runId') ?? undefined,
        agentId: stringField(response, 'agent_id') ?? stringField(response, 'agentId') ?? undefined,
        reply: stringField(response, 'reply') ?? '',
        blocks: arrayField(response, 'blocks'),
        toolCalls: arrayField(response, 'tool_calls') ?? arrayField(response, 'toolCalls'),
        pendingConfirmations: arrayField(response, 'pending_confirmations') ?? arrayField(response, 'pendingConfirmations'),
      }
    },
    listConversations: async (limit = 30) => axisFetch<{ items: ChatConversationSummary[] }>(
      withProduct(`/api/companion/v1/chat/conversations?limit=${encodeURIComponent(String(limit))}`, productSurface),
      orgId,
      productHeaders(productSurface),
    ),
    getConversation: async (id: string) => axisFetch<ChatConversationDetail>(
      withProduct(`/api/companion/v1/chat/conversations/${encodeURIComponent(id)}`, productSurface),
      orgId,
      productHeaders(productSurface),
    ),
  }
}

function stringField(value: Record<string, unknown>, key: string) {
  return typeof value[key] === 'string' ? value[key] as string : null
}

function arrayField(value: Record<string, unknown>, key: string) {
  return Array.isArray(value[key]) ? value[key] as unknown[] : undefined
}
