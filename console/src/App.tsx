import { Activity, Bot, CheckCircle2, DatabaseZap, FileClock, GitPullRequestArrow, KeyRound, Layers3, ListChecks, Play, Power, RefreshCw, ShieldCheck, Sparkles, UsersRound } from 'lucide-react'
import type { ReactNode } from 'react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { ActionType, Approval, AxisSession, BusinessModel, CapabilityRecord, CompanionAgent, CompanionJob, CompanionTask, CostSummary, Delegation, MemoryConflict, MemoryReview, MemorySummary, NexusRequest, ObservabilityEvent, Policy, RunTrace, RuntimePolicy, SecurityEvalReport, ServiceHealth, axisFetch, getHealth, getSession } from './api'

type LoadState<T> = {
  data: T
  error: string
  loading: boolean
}

type RouteArea = 'overview' | 'nexus' | 'companion' | 'operations' | 'access'

type Route = {
  area: RouteArea
  screen: string
}

const empty = <T,>(data: T): LoadState<T> => ({ data, error: '', loading: false })

export function App() {
  const [orgId, setOrgId] = useState(localStorage.getItem('axis.org_id') || 'local-dev-org')
  const [route, setRoute] = useState<Route>(() => parseRoute(window.location.hash))
  const [session, setSession] = useState<LoadState<AxisSession | null>>(empty(null))
  const [health, setHealth] = useState<LoadState<ServiceHealth | null>>(empty(null))
  const [approvals, setApprovals] = useState<LoadState<Approval[]>>(empty([]))
  const [requests, setRequests] = useState<LoadState<NexusRequest[]>>(empty([]))
  const [policies, setPolicies] = useState<LoadState<Policy[]>>(empty([]))
  const [actionTypes, setActionTypes] = useState<LoadState<ActionType[]>>(empty([]))
  const [delegations, setDelegations] = useState<LoadState<Delegation[]>>(empty([]))
  const [tasks, setTasks] = useState<LoadState<CompanionTask[]>>(empty([]))
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

  const refresh = useCallback(async () => {
    localStorage.setItem('axis.org_id', orgId)
    await Promise.all([
      load(setSession, () => getSession(orgId), null),
      load(setHealth, () => getHealth(), null),
      load(setApprovals, async () => (await axisFetch<{ data: Approval[] }>('/api/nexus/v1/approvals/pending', orgId)).data ?? [], []),
      load(setRequests, async () => (await axisFetch<{ data: NexusRequest[] }>('/api/nexus/v1/requests?limit=12', orgId)).data ?? [], []),
      load(setPolicies, async () => (await axisFetch<{ data: Policy[] }>('/api/nexus/v1/policies', orgId)).data ?? [], []),
      load(setActionTypes, async () => (await axisFetch<{ data: ActionType[] }>('/api/nexus/v1/action-types', orgId)).data ?? [], []),
      load(setDelegations, async () => (await axisFetch<{ data: Delegation[] }>('/api/nexus/v1/delegations', orgId)).data ?? [], []),
      load(setTasks, async () => (await axisFetch<{ data: CompanionTask[] }>('/api/companion/v1/tasks?limit=12', orgId)).data ?? [], []),
      load(setTraces, async () => (await axisFetch<{ traces: RunTrace[] }>('/api/companion/v1/run-traces?limit=12', orgId)).traces ?? [], []),
      load(setRuntimePolicy, () => axisFetch<RuntimePolicy>('/api/companion/v1/runtime/policy', orgId), null),
      load(setAgents, async () => (await axisFetch<{ data: CompanionAgent[] }>('/api/companion/v1/agents', orgId)).data ?? [], []),
      load(setCapabilities, async () => (await axisFetch<{ capabilities: CapabilityRecord[] }>('/api/companion/v1/capabilities?limit=12', orgId)).capabilities ?? [], []),
      load(setMemoryConflicts, async () => (await axisFetch<{ conflicts: MemoryConflict[] }>('/api/companion/v1/memory/conflicts?limit=12', orgId)).conflicts ?? [], []),
      load(setMemoryReviews, async () => (await axisFetch<{ reviews: MemoryReview[] }>('/api/companion/v1/memory/reviews?limit=12', orgId)).reviews ?? [], []),
      load(setMemorySummaries, async () => (await axisFetch<{ summaries: MemorySummary[] }>('/api/companion/v1/memory/summaries?limit=12', orgId)).summaries ?? [], []),
      load(setJobs, async () => (await axisFetch<{ jobs: CompanionJob[] }>('/api/companion/v1/jobs?limit=12', orgId)).jobs ?? [], []),
      load(setEvents, async () => (await axisFetch<{ events: ObservabilityEvent[] }>('/api/companion/v1/observability/events?limit=12', orgId)).events ?? [], []),
      load(setCosts, () => axisFetch<CostSummary>('/api/companion/v1/runtime/costs', orgId), null),
      load(setSecurityReports, async () => (await axisFetch<{ reports: SecurityEvalReport[] }>('/api/companion/v1/security-evals/reports?limit=12', orgId)).reports ?? [], []),
      load(setBusinessModel, () => axisFetch<BusinessModel>('/api/companion/v1/business-model', orgId), null)
    ])
  }, [orgId])

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

  useEffect(() => {
    const syncRoute = () => setRoute(parseRoute(window.location.hash))
    window.addEventListener('hashchange', syncRoute)
    window.addEventListener('popstate', syncRoute)
    return () => {
      window.removeEventListener('hashchange', syncRoute)
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

  const title = pageTitle(route)
  const navigate = useCallback((next: Route) => {
    window.history.pushState(null, '', routeHash(next))
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
          <button type="button" className={route.area === 'overview' ? 'active' : ''} onClick={() => navigate({ area: 'overview', screen: 'summary' })}><Activity aria-hidden="true" />Overview</button>
          <button type="button" className={route.area === 'nexus' ? 'active' : ''} onClick={() => navigate({ area: 'nexus', screen: 'approvals' })}><GitPullRequestArrow aria-hidden="true" />Nexus</button>
          <button type="button" className={route.area === 'companion' ? 'active' : ''} onClick={() => navigate({ area: 'companion', screen: 'tasks' })}><Bot aria-hidden="true" />Companion</button>
          <button type="button" className={route.area === 'operations' ? 'active' : ''} onClick={() => navigate({ area: 'operations', screen: 'memory' })}><Activity aria-hidden="true" />Ops</button>
          <button type="button" className={route.area === 'access' ? 'active' : ''} onClick={() => navigate({ area: 'access', screen: 'action-types' })}><KeyRound aria-hidden="true" />Access</button>
        </nav>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div>
            <h1>{title}</h1>
            <p>{session.data?.actor_id ?? 'local-dev-admin'}</p>
          </div>
          <div className="toolbar">
            <label>
              <span>Org</span>
              <input value={orgId} onChange={(event) => setOrgId(event.target.value)} />
            </label>
            <button type="button" onClick={() => void refresh()} aria-label="Refresh">
              <RefreshCw aria-hidden="true" />
            </button>
          </div>
        </header>

        <section className="health-row">
          <HealthPill label="BFF" value="ok" />
          <HealthPill label="Companion" value={health.data?.companion ?? health.error ?? 'loading'} />
          <HealthPill label="Nexus" value={health.data?.nexus ?? health.error ?? 'loading'} />
          <span className="scope-pill">{session.data?.auth_method ?? 'dev'}</span>
          {actionMessage && <span className="scope-pill">{actionMessage}</span>}
        </section>

        {route.area === 'overview' && (
          <section className="page-section">
            <div className="metrics-grid">
              <Metric icon={<CheckCircle2 />} label="Approvals" value={approvals.data.length} tone="green" />
              <Metric icon={<FileClock />} label="Requests" value={requests.data.length} tone="blue" />
              <Metric icon={<Sparkles />} label="Agents" value={agents.data.length} tone="violet" />
              <Metric icon={<DatabaseZap />} label="Capabilities" value={capabilities.data.length} tone="amber" />
            </div>
          </section>
        )}

        {route.area === 'nexus' && (
          <section className="page-section">
            <ScreenNav items={[
              ['approvals', 'Approvals'],
              ['requests', 'Requests'],
              ['policies', 'Policies'],
              ['risk', 'Risk']
            ]} base="nexus" active={route.screen} onNavigate={navigate} />

            {route.screen === 'approvals' && (
              <div className="screen-grid">
                <Panel title="Approvals" icon={<ListChecks />} state={approvals}>
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
                <Panel title="Risk" icon={<Activity />} state={requests}>
                  <div className="risk-list">
                    {Object.entries(riskCounts).map(([risk, count]) => (
                      <span key={risk}><b>{risk}</b>{count}</span>
                    ))}
                  </div>
                </Panel>
              </div>
            )}
          </section>
        )}

        {route.area === 'access' && (
          <section className="page-section">
            <ScreenNav items={[
              ['action-types', 'Action Types'],
              ['delegations', 'Delegations']
            ]} base="access" active={route.screen} onNavigate={navigate} />
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

        {route.area === 'companion' && (
          <section className="page-section">
            <ScreenNav items={[
              ['tasks', 'Tasks'],
              ['control', 'Control Plane'],
              ['agents', 'Agents'],
              ['capabilities', 'Capabilities'],
              ['traces', 'Run Traces'],
              ['business', 'Business Model']
            ]} base="companion" active={route.screen} onNavigate={navigate} />
            {route.screen === 'tasks' && (
              <div className="screen-grid">
                <Panel title="Tasks" icon={<Bot />} state={tasks}>
                  <Table columns={['title', 'status', 'channel']} rows={tasks.data.map((item) => [item.title, item.status, item.channel ?? 'api'])} />
                </Panel>
              </div>
            )}
            {route.screen === 'control' && (
              <div className="screen-grid">
                <Panel title="Control Plane" icon={<ShieldCheck />} state={runtimePolicy}>
                  <div className="panel-actions">
                    <button type="button" onClick={() => void runAction('runtime kill switch', () => axisFetch('/api/companion/v1/runtime/policy', orgId, { method: 'PUT', body: JSON.stringify({ kill_switch: !runtimePolicy.data?.kill_switch }) }))}>
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
            {route.screen === 'agents' && (
              <div className="screen-grid">
                <Panel title="Agents" icon={<UsersRound />} state={agents}>
                  <div className="panel-actions">
                    <button type="button" disabled={!agents.data.some((item) => item.status === 'active')} onClick={() => {
                      const agent = agents.data.find((item) => item.status === 'active')
                      if (agent) void runAction('disable agent', () => axisFetch(`/api/companion/v1/agents/${encodeURIComponent(agent.agent_id)}/disable`, orgId, { method: 'POST', body: '{}' }))
                    }}>
                      <Power aria-hidden="true" />Disable active
                    </button>
                  </div>
                  <Table columns={['agent', 'role', 'status', 'autonomy']} rows={agents.data.map((item) => [item.display_name || item.agent_id, item.role ?? item.profile_id ?? '-', item.status, item.max_autonomy])} />
                </Panel>
              </div>
            )}
            {route.screen === 'capabilities' && (
              <div className="screen-grid">
                <Panel title="Capabilities" icon={<Layers3 />} state={capabilities}>
                  <div className="panel-actions">
                    <button type="button" disabled={!capabilities.data.some((item) => item.status === 'draft')} onClick={() => {
                      const cap = capabilities.data.find((item) => item.status === 'draft')
                      if (cap) void runAction('promote manifest', () => axisFetch(`/api/companion/v1/capabilities/${encodeURIComponent(cap.manifest.capability_id)}/versions/${encodeURIComponent(cap.manifest.version)}/promote`, orgId, { method: 'POST', body: '{}' }))
                    }}>
                      <CheckCircle2 aria-hidden="true" />Promote draft
                    </button>
                  </div>
                  <Table columns={['capability', 'version', 'risk', 'approval']} rows={capabilities.data.map((item) => [item.manifest.display_name || item.manifest.capability_id, item.manifest.version, item.manifest.risk_level, item.manifest.approval_required ? 'yes' : 'no'])} />
                </Panel>
              </div>
            )}
            {route.screen === 'traces' && (
              <div className="screen-grid">
                <Panel title="Run Traces" icon={<Sparkles />} state={traces}>
                  <Table columns={['surface', 'intent', 'status', 'started']} rows={traces.data.map((item) => [item.product_surface ?? '-', item.intent ?? '-', item.status ?? '-', date(item.started_at)])} />
                </Panel>
              </div>
            )}
            {route.screen === 'business' && (
              <div className="screen-grid">
                <Panel title="Business Model" icon={<DatabaseZap />} state={businessModel}>
                  <Table columns={['area', 'count']} rows={[
                    ['areas', countOf(businessModel.data?.areas)],
                    ['roles', countOf(businessModel.data?.roles)],
                    ['workflows', countOf(businessModel.data?.workflows)],
                    ['rules', countOf(businessModel.data?.rules)]
                  ]} />
                </Panel>
              </div>
            )}
          </section>
        )}

        {route.area === 'operations' && (
          <section className="page-section">
            <ScreenNav items={[
              ['memory', 'Memory'],
              ['jobs', 'Jobs'],
              ['observability', 'Observability'],
              ['cost', 'Cost'],
              ['security', 'Security Evals']
            ]} base="operations" active={route.screen} onNavigate={navigate} />
            {route.screen === 'memory' && (
              <div className="screen-grid two">
                <Panel title="Memory Review" icon={<DatabaseZap />} state={memoryConflicts}>
                  <div className="panel-actions">
                    <button type="button" disabled={!memoryReviews.data.length} onClick={() => {
                      const review = memoryReviews.data[0]
                      if (review) void runAction('apply memory review', () => axisFetch(`/api/companion/v1/memory/reviews/${encodeURIComponent(review.id)}/apply`, orgId, { method: 'POST', body: '{}' }))
                    }}>
                      <CheckCircle2 aria-hidden="true" />Apply review
                    </button>
                  </div>
                  <Table columns={['key', 'kind', 'confidence', 'updated']} rows={memoryConflicts.data.map((item) => [item.key, item.kind, item.confidence.toFixed(2), date(item.updated_at)])} />
                </Panel>
                <Panel title="Memory Summaries" icon={<FileClock />} state={memorySummaries}>
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
                <Panel title="Observability" icon={<ListChecks />} state={events}>
                  <Table columns={['type', 'name', 'severity', 'time']} rows={events.data.map((item) => [item.event_type, item.event_name, item.severity, date(item.occurred_at)])} />
                </Panel>
              </div>
            )}
            {route.screen === 'cost' && (
              <div className="screen-grid">
                <Panel title="Cost" icon={<DatabaseZap />} state={costs}>
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
                <Panel title="Security Evals" icon={<ShieldCheck />} state={securityReports}>
                  <div className="panel-actions">
                    <button type="button" onClick={() => void runAction('security eval', () => axisFetch('/api/companion/v1/security-evals/runs', orgId, { method: 'POST', body: JSON.stringify({ suite: 'security-adversarial' }) }))}>
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

function parseRoute(hash: string): Route {
  const raw = hash.replace(/^#/, '').trim()
  const [areaRaw, screenRaw] = raw.split('/')
  const area = isRouteArea(areaRaw) ? areaRaw : 'overview'
  const screens: Record<RouteArea, string[]> = {
    overview: ['summary'],
    nexus: ['approvals', 'requests', 'policies', 'risk'],
    companion: ['tasks', 'control', 'agents', 'capabilities', 'traces', 'business'],
    operations: ['memory', 'jobs', 'observability', 'cost', 'security'],
    access: ['action-types', 'delegations']
  }
  const screen = screens[area].includes(screenRaw) ? screenRaw : screens[area][0]
  return { area, screen }
}

function isRouteArea(value: string): value is RouteArea {
  return value === 'overview' || value === 'nexus' || value === 'companion' || value === 'operations' || value === 'access'
}

function routeHash(route: Route) {
  if (route.area === 'overview') {
    return '#overview'
  }
  return `#${route.area}/${route.screen}`
}

function pageTitle(route: Route) {
  switch (route.area) {
    case 'nexus':
      return 'Nexus'
    case 'companion':
      return 'Companion'
    case 'operations':
      return 'Operations'
    case 'access':
      return 'Access'
    default:
      return 'Axis Overview'
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

async function load<T>(setState: (value: LoadState<T>) => void, fn: () => Promise<T>, fallback: T) {
  setState({ data: fallback, error: '', loading: true })
  try {
    const data = await fn()
    setState({ data, error: '', loading: false })
  } catch (error) {
    setState({ data: fallback, error: error instanceof Error ? error.message : 'error', loading: false })
  }
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
    return <div className="empty">No data</div>
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
