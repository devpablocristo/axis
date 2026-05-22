import { Activity, Bot, CheckCircle2, DatabaseZap, FileClock, GitPullRequestArrow, KeyRound, Layers3, ListChecks, RefreshCw, ShieldCheck, Sparkles, UsersRound } from 'lucide-react'
import type { ReactNode } from 'react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { ActionType, Approval, AxisSession, CompanionTask, Delegation, GovernanceRequest, Policy, RunTrace, ServiceHealth, axisFetch, getHealth, getSession } from './api'

type LoadState<T> = {
  data: T
  error: string
  loading: boolean
}

const empty = <T,>(data: T): LoadState<T> => ({ data, error: '', loading: false })

export function App() {
  const [orgId, setOrgId] = useState(localStorage.getItem('axis.org_id') || 'local-dev-org')
  const [session, setSession] = useState<LoadState<AxisSession | null>>(empty(null))
  const [health, setHealth] = useState<LoadState<ServiceHealth | null>>(empty(null))
  const [approvals, setApprovals] = useState<LoadState<Approval[]>>(empty([]))
  const [requests, setRequests] = useState<LoadState<GovernanceRequest[]>>(empty([]))
  const [policies, setPolicies] = useState<LoadState<Policy[]>>(empty([]))
  const [actionTypes, setActionTypes] = useState<LoadState<ActionType[]>>(empty([]))
  const [delegations, setDelegations] = useState<LoadState<Delegation[]>>(empty([]))
  const [tasks, setTasks] = useState<LoadState<CompanionTask[]>>(empty([]))
  const [traces, setTraces] = useState<LoadState<RunTrace[]>>(empty([]))

  const refresh = useCallback(async () => {
    localStorage.setItem('axis.org_id', orgId)
    await Promise.all([
      load(setSession, () => getSession(orgId), null),
      load(setHealth, () => getHealth(), null),
      load(setApprovals, async () => (await axisFetch<{ data: Approval[] }>('/api/nexus/v1/approvals/pending', orgId)).data ?? [], []),
      load(setRequests, async () => (await axisFetch<{ data: GovernanceRequest[] }>('/api/nexus/v1/requests?limit=12', orgId)).data ?? [], []),
      load(setPolicies, async () => (await axisFetch<{ data: Policy[] }>('/api/nexus/v1/policies', orgId)).data ?? [], []),
      load(setActionTypes, async () => (await axisFetch<{ data: ActionType[] }>('/api/nexus/v1/action-types', orgId)).data ?? [], []),
      load(setDelegations, async () => (await axisFetch<{ data: Delegation[] }>('/api/nexus/v1/delegations', orgId)).data ?? [], []),
      load(setTasks, async () => (await axisFetch<{ data: CompanionTask[] }>('/api/companion/v1/tasks?limit=12', orgId)).data ?? [], []),
      load(setTraces, async () => (await axisFetch<{ traces: RunTrace[] }>('/api/companion/v1/run-traces?limit=12', orgId)).traces ?? [], [])
    ])
  }, [orgId])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const riskCounts = useMemo(() => {
    return requests.data.reduce<Record<string, number>>((acc, item) => {
      const risk = item.risk_level || 'unknown'
      acc[risk] = (acc[risk] ?? 0) + 1
      return acc
    }, {})
  }, [requests.data])

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
          <a href="#overview"><Activity aria-hidden="true" />Overview</a>
          <a href="#nexus"><GitPullRequestArrow aria-hidden="true" />Nexus</a>
          <a href="#companion"><Bot aria-hidden="true" />Companion</a>
          <a href="#access"><KeyRound aria-hidden="true" />Access</a>
        </nav>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div>
            <h1>Axis Console</h1>
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

        <section id="overview" className="metrics-grid">
          <Metric icon={<CheckCircle2 />} label="Approvals" value={approvals.data.length} tone="green" />
          <Metric icon={<FileClock />} label="Requests" value={requests.data.length} tone="blue" />
          <Metric icon={<Sparkles />} label="Tasks" value={tasks.data.length} tone="violet" />
          <Metric icon={<DatabaseZap />} label="Traces" value={traces.data.length} tone="amber" />
        </section>

        <section className="health-row">
          <HealthPill label="BFF" value="ok" />
          <HealthPill label="Companion" value={health.data?.companion ?? health.error ?? 'loading'} />
          <HealthPill label="Nexus" value={health.data?.nexus ?? health.error ?? 'loading'} />
          <span className="scope-pill">{session.data?.auth_method ?? 'dev'}</span>
        </section>

        <section id="nexus" className="panel-grid">
          <Panel title="Approvals" icon={<ListChecks />} state={approvals}>
            <Table columns={['status', 'request', 'expires']} rows={approvals.data.map((item) => [item.status, short(item.request_id), date(item.expires_at)])} />
          </Panel>
          <Panel title="Requests" icon={<GitPullRequestArrow />} state={requests}>
            <Table columns={['action', 'decision', 'risk', 'status']} rows={requests.data.map((item) => [item.action_type, item.decision, item.risk_level, item.status])} />
          </Panel>
          <Panel title="Policies" icon={<ShieldCheck />} state={policies}>
            <Table columns={['name', 'effect', 'mode', 'enabled']} rows={policies.data.map((item) => [item.name, item.effect, item.mode, item.enabled ? 'yes' : 'no'])} />
          </Panel>
          <Panel title="Risk" icon={<Activity />} state={requests}>
            <div className="risk-list">
              {Object.entries(riskCounts).map(([risk, count]) => (
                <span key={risk}><b>{risk}</b>{count}</span>
              ))}
            </div>
          </Panel>
        </section>

        <section id="access" className="panel-grid">
          <Panel title="Action Types" icon={<Layers3 />} state={actionTypes}>
            <Table columns={['name', 'category', 'risk', 'enabled']} rows={actionTypes.data.map((item) => [item.name, item.category, item.risk_class, item.enabled ? 'yes' : 'no'])} />
          </Panel>
          <Panel title="Delegations" icon={<UsersRound />} state={delegations}>
            <Table columns={['owner', 'agent', 'risk', 'enabled']} rows={delegations.data.map((item) => [item.owner_id, item.agent_id, item.max_risk_class ?? '-', item.enabled ? 'yes' : 'no'])} />
          </Panel>
        </section>

        <section id="companion" className="panel-grid">
          <Panel title="Tasks" icon={<Bot />} state={tasks}>
            <Table columns={['title', 'status', 'channel']} rows={tasks.data.map((item) => [item.title, item.status, item.channel ?? 'api'])} />
          </Panel>
          <Panel title="Run Traces" icon={<Sparkles />} state={traces}>
            <Table columns={['surface', 'intent', 'status', 'started']} rows={traces.data.map((item) => [item.product_surface ?? '-', item.intent ?? '-', item.status ?? '-', date(item.started_at)])} />
          </Panel>
        </section>
      </main>
    </div>
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

function short(value: string) {
  return value ? value.slice(0, 8) : '-'
}

function date(value?: string) {
  if (!value) return '-'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return '-'
  return parsed.toLocaleString()
}
