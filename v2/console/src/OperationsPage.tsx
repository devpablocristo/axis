import { AlertTriangle, Archive, CirclePause, CirclePlay, FileArchive, RefreshCw, RotateCcw, ShieldAlert, Stethoscope } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { axisDownload, axisFetch } from './api'
import { formatDateTime24 } from './formatters'

type Props = { orgId: string; principalId: string; productSurface: string; initialTab?: Tab }
type ServiceOverview = { status?: string; fleet?: Record<string, number>; jobs?: Record<string, number>; outbox?: Record<string, number>; incidents?: Record<string, number>; active_holds?: number; exports?: Record<string, number>; oldest_queued_job_age_seconds?: number; oldest_outbox_age_seconds?: number }
type OperationsOverview = { status: 'healthy' | 'partial' | 'unavailable'; services: Record<'companion' | 'nexus', ServiceOverview> }
type FleetMember = { virployee_id: string; name: string; status: string; job_role_name: string; autonomy: string; active_subjects: number; max_active_subjects: number; pending_jobs: number; recent_errors: number; authority_state: string; last_success_at?: string }
type Job = { service: 'companion' | 'nexus'; id: string; kind: string; status: string; effect_class: string; replay_policy: string; attempts: number; max_attempts: number; last_error_code?: string; created_at: string; dedupe_key_hash: string; payload_hash?: string }
type Incident = { id: string; incident_type: string; resource_type: string; resource_id: string; severity: string; status: string; occurrence_count: number; revision: number; last_seen: string }
type Reconciliation = { id: string; product_surface: string; mode: string; trigger: string; status: string; findings_count: number; repaired_count: number; report_hash: string; started_at: string }
type WorkerControl = { service?: 'companion' | 'nexus'; job_kind: string; state: string; version: number; failure_count: number; reason_code: string; opened_until?: string }
type OutboxItem = { id: string; kind: string; status: string; attempts: number; max_attempts: number; last_error_code?: string; created_at: string }
type LegalHold = { id: string; scope_type: string; scope_id: string; reason_code: string; status: string; revision: number; created_at: string }
type ExportItem = { id: string; scope_type: string; scope_id: string; categories: string[]; status: string; manifest_hash?: string; error_code?: string; requested_at: string; expires_at?: string }
type SLO = { metric_key: string; comparator: string; target: number; window_seconds: number; minimum_samples: number; severity: string; enabled: boolean; status: string; value?: number; sample_count: number }
type NotificationPolicy = { enabled: boolean; webhook_secret_ref: string; revision: number }
type DownloadToken = { token: string; export_id: string; manifest_hash: string; expires_at: string }
type ServedProduct = { service: 'companion' | 'nexus'; product_surface: string; area: string; status: string; configured: boolean; observed: boolean; access_mode?: string; requests: number; succeeded: number; denied: number; failed: number; latency_p95_ms?: number; last_seen_at?: string; last_error_code?: string }
type FinOpsSummary = { group: string; events: number; input_units: number; output_units: number; cost_micro_usd: number; unpriced: number }
type Tab = 'fleet' | 'products' | 'finops' | 'reconciliation' | 'delivery' | 'incidents' | 'retention'

export function OperationsPage({ orgId, principalId, productSurface, initialTab = 'fleet' }: Props) {
  const [overview, setOverview] = useState<OperationsOverview | null>(null)
  const [fleet, setFleet] = useState<FleetMember[]>([])
  const [jobs, setJobs] = useState<Job[]>([])
  const [incidents, setIncidents] = useState<Incident[]>([])
  const [reconciliations, setReconciliations] = useState<Reconciliation[]>([])
  const [controls, setControls] = useState<WorkerControl[]>([])
  const [outbox, setOutbox] = useState<OutboxItem[]>([])
  const [holds, setHolds] = useState<LegalHold[]>([])
  const [exports, setExports] = useState<ExportItem[]>([])
  const [slos, setSLOs] = useState<SLO[]>([])
  const [notification, setNotification] = useState<NotificationPolicy>({ enabled: false, webhook_secret_ref: '', revision: 0 })
  const [servedProducts, setServedProducts] = useState<ServedProduct[]>([])
  const [finops, setFinOps] = useState<FinOpsSummary[]>([])
  const [activeTab, setActiveTab] = useState<Tab>(initialTab)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [holdScope, setHoldScope] = useState({ scope_type: 'organization', scope_id: orgId, reason_code: 'legal_preservation' })

  const api = useCallback(<T,>(path: string, method = 'GET', body?: unknown, idempotencyKey?: string) => axisFetch<T>(path, {
    method, body, orgId, principalId, headers: idempotencyKey ? { 'Idempotency-Key': idempotencyKey } : undefined,
  }), [principalId, orgId])

  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const [nextOverview, nextFleet, companionJobs, nexusJobs, companionRuns, nexusRuns, companionControls, nexusControls, nextOutbox, nextIncidents, nextSLOs, nextHolds, nextExports, nextNotification, nextServed, nextFinOps] = await Promise.all([
        api<OperationsOverview>('/api/operations/overview'),
        api<{ items: FleetMember[] }>('/api/operations/fleet'),
        api<{ items: Job[] }>('/api/operations/jobs?service=companion&limit=100'),
        api<{ items: Job[] }>('/api/operations/jobs?service=nexus&limit=100'),
        api<{ items: Reconciliation[] }>('/api/operations/reconciliations?service=companion&limit=30'),
        api<{ items: Reconciliation[] }>('/api/operations/reconciliations?service=nexus&limit=30'),
        api<{ items: WorkerControl[] }>('/api/operations/worker-controls?service=companion'),
        api<{ items: WorkerControl[] }>('/api/operations/worker-controls?service=nexus'),
        api<{ items: OutboxItem[] }>('/api/operations/outbox?limit=100'),
        api<{ items: Incident[] }>('/api/operations/incidents?limit=100'),
        api<{ items: SLO[] }>('/api/operations/slos'),
        api<{ items: LegalHold[] }>('/api/operations/legal-holds'),
        api<{ items: ExportItem[] }>('/api/operations/exports'),
        api<NotificationPolicy>('/api/operations/notifications'),
        api<{ items: ServedProduct[] }>('/api/operations/product-service-map?service=all&window=24h'),
        api<{ items: FinOpsSummary[] }>('/api/finops/summary?group_by=product'),
      ])
      setOverview(nextOverview); setFleet(nextFleet.items ?? []); setJobs([...(companionJobs.items ?? []), ...(nexusJobs.items ?? [])]); setReconciliations([...(companionRuns.items ?? []), ...(nexusRuns.items ?? [])])
      setControls([...(companionControls.items ?? []).map(item=>({...item,service:'companion' as const})), ...(nexusControls.items ?? []).map(item=>({...item,service:'nexus' as const}))]); setOutbox(nextOutbox.items ?? []); setIncidents(nextIncidents.items ?? []); setSLOs(nextSLOs.items ?? []); setHolds(nextHolds.items ?? []); setExports(nextExports.items ?? []); setNotification(nextNotification)
      setServedProducts(nextServed.items ?? []); setFinOps(nextFinOps.items ?? [])
    } catch (cause) { setError(message(cause, 'Could not load operations')) } finally { setLoading(false) }
  }, [api])

  useEffect(() => { void load() }, [load])
  useEffect(() => { setActiveTab(initialTab) }, [initialTab])
  useEffect(() => { setHoldScope((current) => ({ ...current, scope_id: current.scope_type === 'organization' ? orgId : current.scope_id })) }, [orgId])

  const run = async (key: string, action: (idempotencyKey: string) => Promise<void>) => {
    if (busy) return
    setBusy(key); setError('')
    try { await action(crypto.randomUUID()); await load() } catch (cause) { setError(message(cause, 'Operation failed')) } finally { setBusy('') }
  }
  const downloadExport = async (item: ExportItem) => {
    const token = await api<DownloadToken>(`/api/operations/exports/${item.id}/download-token`, 'POST')
    const blob = await axisDownload(`/api/operations/exports/${item.id}/download?token=${encodeURIComponent(token.token)}`, { orgId, principalId })
    const objectURL = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = objectURL; link.download = `axis-export-${item.id}.zip`; link.click()
    URL.revokeObjectURL(objectURL)
  }
  const serviceState = (name: 'companion' | 'nexus') => overview?.services?.[name]?.status ?? 'unavailable'
  const criticalCount = useMemo(() => incidents.filter((item) => item.status !== 'resolved' && (item.severity === 'critical' || item.severity === 'high')).length, [incidents])

  if (loading && !overview) return <div className="spinner" />
  return <section className="operations-page">
    <header className="operations-rail operations-rail--compact">
      <div className="operations-rail__services" aria-label="Service health path">
        <ServiceNode name="Companion" state={serviceState('companion')} detail={`${fleet.length} Virployees`} />
        <i aria-hidden="true" />
        <ServiceNode name="Nexus" state={serviceState('nexus')} detail={`${criticalCount} high-priority incidents`} />
      </div>
      <button className="btn-secondary operations-refresh" onClick={() => void load()} disabled={loading}><RefreshCw aria-hidden="true" /> Refresh</button>
    </header>
    {overview?.status !== 'healthy' ? <div className="operations-degraded"><AlertTriangle aria-hidden="true" /><span>{overview?.status === 'partial' ? 'One service is unavailable. Missing data is not treated as healthy.' : 'Operational state is unavailable.'}</span></div> : null}
    {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}
    <nav className="operations-tabs" aria-label="Operations sections">
      {([['fleet','Fleet'],['products','Products served'],['finops','FinOps'],['reconciliation','Reconciliation'],['delivery','Jobs & delivery'],['incidents','Incidents & SLOs'],['retention','Holds & exports']] as Array<[Tab,string]>).map(([key,label]) => <button key={key} className={activeTab===key?'active':''} onClick={() => setActiveTab(key)}>{label}</button>)}
    </nav>

    {activeTab === 'fleet' ? <div className="operations-grid">
      <article className="card operations-card operations-card--wide"><CardHeading icon={<Stethoscope />} title="Fleet condition" note="Derived from current Virployees; no separate Agent records." />
        <div className="table-wrap"><table><thead><tr><th>Virployee</th><th>Role</th><th>State</th><th>Subjects</th><th>Queue</th><th>Authority</th><th>Last success</th></tr></thead><tbody>{fleet.length===0?<EmptyRow span={7} text="No Virployees are available in this organization."/>:fleet.map((item)=><tr key={item.virployee_id}><td><strong>{item.name}</strong><code>{short(item.virployee_id)}</code></td><td>{item.job_role_name}</td><td><Status value={item.status}/></td><td>{item.active_subjects} / {item.max_active_subjects || '—'}</td><td>{item.pending_jobs}</td><td>{item.authority_state}</td><td>{item.last_success_at?formatDateTime24(item.last_success_at):'Never'}</td></tr>)}</tbody></table></div>
      </article>
    </div> : null}

    {activeTab === 'products' ? <div className="operations-grid">
      <article className="card operations-card operations-card--wide"><CardHeading icon={<Stethoscope/>} title="Products served" note="Configured and observed areas are tracked independently. Idle and unknown are explicit states."/>
        <div className="served-product-matrix">{(['companion','nexus'] as const).map(service=><section key={service}><header><strong>{service}</strong><span>{servedProducts.filter(item=>item.service===service).length} area projections</span></header><div className="table-wrap"><table><thead><tr><th>Product / area</th><th>State</th><th>Contract</th><th>Traffic</th><th>Denied</th><th>Technical failures</th><th>p95</th><th>Last activity</th></tr></thead><tbody>{servedProducts.filter(item=>item.service===service).length===0?<EmptyRow span={8} text={`${service} is unavailable or has no configured products.`}/>:servedProducts.filter(item=>item.service===service).map((item,index)=><tr key={`${service}-${item.product_surface}-${item.area}-${index}`}><td><strong>{item.product_surface}</strong><span>{item.area}{item.access_mode?` · ${item.access_mode}`:''}</span></td><td><Status value={item.status}/></td><td>{item.configured?'configured':'observed only'}</td><td>{item.requests} / {item.succeeded}</td><td>{item.denied}</td><td>{item.failed}</td><td>{item.latency_p95_ms===undefined?'—':`${item.latency_p95_ms} ms`}</td><td>{item.last_seen_at?formatDateTime24(item.last_seen_at):'Never'}</td></tr>)}</tbody></table></div></section>)}</div>
      </article>
    </div> : null}

    {activeTab === 'finops' ? <div className="operations-grid">
      <article className="card operations-card operations-card--wide"><CardHeading title="Cost attribution" note="FinOps accounts for consumption. Quotas remain the only mechanism that controls or blocks work."/>
        <div className="finops-summary">{finops.map(item=><div key={item.group}><span>{item.group||'Unattributed product'}</span><strong>${(item.cost_micro_usd/1_000_000).toFixed(4)}</strong><small>{item.events} events · {(item.input_units+item.output_units).toLocaleString()} units</small>{item.unpriced?<em>{item.unpriced} unpriced</em>:null}</div>)}{finops.length===0?<p className="axis-muted">No recorded cost events for this period.</p>:null}</div>
      </article>
      <article className="card operations-card"><CardHeading title="Budget behavior" note="80% and 100% thresholds create informational incidents. Budget excess never blocks execution."/><p className="axis-muted">Budgets are scoped to the organization or one product and use UTC calendar months.</p></article>
    </div> : null}

    {activeTab === 'reconciliation' ? <div className="operations-grid">
      <article className="card operations-card"><CardHeading icon={<RotateCcw />} title="Run a reconciliation" note="Detect first. Safe repair only recovers deterministic runtime state." />
        <div className="operations-action-stack">{(['companion','nexus'] as const).map((service)=><div key={service}><strong>{service}</strong><span>Organization + {productSurface}</span><button className="btn-secondary" disabled={!!busy} onClick={() => void run(`detect-${service}`, async(key)=>{await api(`/api/operations/reconciliations?service=${service}`,'POST',{mode:'detect'},key)})}>Detect</button><button className="btn-primary" disabled={!!busy} onClick={() => void run(`repair-${service}`, async(key)=>{await api(`/api/operations/reconciliations?service=${service}`,'POST',{mode:'safe_repair'},key)})}>Safe repair</button></div>)}</div>
      </article>
      <article className="card operations-card operations-card--wide"><CardHeading title="Immutable reports" note="Report hashes bind the observed finding set."/><div className="table-wrap"><table><thead><tr><th>Service</th><th>Mode</th><th>Result</th><th>Findings</th><th>Repaired</th><th>Report</th><th>Started</th></tr></thead><tbody>{reconciliations.length===0?<EmptyRow span={7} text="No reconciliation has run yet."/>:reconciliations.map((item)=><tr key={item.id}><td>{item.product_surface}</td><td>{item.mode}</td><td><Status value={item.status}/></td><td>{item.findings_count}</td><td>{item.repaired_count}</td><td><code>{short(item.report_hash)}</code></td><td>{formatDateTime24(item.started_at)}</td></tr>)}</tbody></table></div></article>
    </div> : null}

    {activeTab === 'delivery' ? <div className="operations-grid">
      <article className="card operations-card operations-card--wide"><CardHeading icon={<Archive/>} title="Durable jobs" note="Payloads are represented by hashes. Replays preserve the original idempotency binding."/><div className="table-wrap"><table><thead><tr><th>Service / kind</th><th>State</th><th>Attempts</th><th>Effect</th><th>Error code</th><th>Created</th><th /></tr></thead><tbody>{jobs.length===0?<EmptyRow span={7} text="No durable jobs were found."/>:jobs.map((item)=><tr key={`${item.service}-${item.id}`}><td><strong>{item.service}</strong><span>{item.kind}</span></td><td><Status value={item.status}/></td><td>{item.attempts}/{item.max_attempts}</td><td>{item.effect_class||'internal_write'}</td><td><code>{item.last_error_code||'—'}</code></td><td>{formatDateTime24(item.created_at)}</td><td className="operations-row-actions">{item.status==='dead_letter'?<button className="btn-secondary" disabled={!!busy} onClick={()=>void run(`replay-${item.id}`,async key=>{await api(`/api/operations/jobs/${item.service}/${item.id}/replay`,'POST',{},key)})}><CirclePlay/>Replay</button>:null}{item.status==='queued'||item.status==='running'?<button className="btn-secondary" disabled={!!busy} onClick={()=>void run(`cancel-${item.id}`,async key=>{await api(`/api/operations/jobs/${item.service}/${item.id}/cancel`,'POST',{reason_code:'operator_cancelled'},key)})}><CirclePause/>Cancel</button>:null}</td></tr>)}</tbody></table></div></article>
      <article className="card operations-card"><CardHeading title="Worker controls" note="Protected recovery jobs cannot pause themselves."/><div className="operations-control-list">{controls.length===0?<p className="axis-muted">No manual worker controls.</p>:controls.map(item=><div key={`${item.service}-${item.job_kind}`}><span><strong>{item.job_kind}</strong><small>{item.service} · {item.failure_count} recent failures</small></span><Status value={item.state}/><button className="btn-secondary" disabled={!!busy} onClick={()=>void run(`control-${item.service}-${item.job_kind}`,async key=>{await api(`/api/operations/worker-controls?service=${item.service}`,'PUT',{job_kind:item.job_kind,state:item.state==='paused'?'closed':'paused',reason_code:item.state==='paused'?'operator_resumed':'operator_paused',expected_version:item.version},key)})}>{item.state==='paused'?'Resume':'Pause'}</button></div>)}</div></article>
      <article className="card operations-card"><CardHeading title="Nexus delivery outbox" note="A Nexus outage does not discard operational findings."/><div className="operations-mini-list">{outbox.slice(0,20).map(item=><div key={item.id}><span><strong>{item.kind}</strong><small>{formatDateTime24(item.created_at)}</small></span><Status value={item.status}/>{item.status==='dead'?<button className="btn-secondary" disabled={!!busy} onClick={()=>void run(`outbox-${item.id}`,async key=>{await api(`/api/operations/outbox/${item.id}/replay`,'POST',{reason_code:'operator_replay'},key)})}>Replay</button>:null}</div>)}{outbox.length===0?<p className="axis-muted">Outbox is empty.</p>:null}</div></article>
    </div> : null}

    {activeTab === 'incidents' ? <div className="operations-grid">
      <article className="card operations-card operations-card--wide"><CardHeading icon={<ShieldAlert/>} title="Incident ledger" note="Repeated findings increase occurrence count instead of opening duplicates."/><div className="table-wrap"><table><thead><tr><th>Severity</th><th>Finding</th><th>Resource</th><th>State</th><th>Seen</th><th>Last seen</th><th /></tr></thead><tbody>{incidents.length===0?<EmptyRow span={7} text="No operational incidents are open."/>:incidents.map(item=><tr key={item.id}><td><Status value={item.severity}/></td><td>{item.incident_type}</td><td>{item.resource_type}<code>{short(item.resource_id)}</code></td><td><Status value={item.status}/></td><td>{item.occurrence_count}</td><td>{formatDateTime24(item.last_seen)}</td><td className="operations-row-actions">{item.status==='open'?<button className="btn-secondary" disabled={!!busy} onClick={()=>void run(`ack-${item.id}`,async key=>{await api(`/api/operations/incidents/${item.id}/acknowledge`,'POST',{reason_code:'operator_acknowledged',expected_revision:item.revision},key)})}>Acknowledge</button>:null}{item.status!=='resolved'?<button className="btn-secondary" disabled={!!busy} onClick={()=>void run(`resolve-${item.id}`,async key=>{await api(`/api/operations/incidents/${item.id}/resolve`,'POST',{reason_code:'operator_resolved',expected_revision:item.revision},key)})}>Resolve</button>:null}</td></tr>)}</tbody></table></div></article>
      <article className="card operations-card operations-card--wide"><CardHeading title="Service level objectives" note="Without enough samples the answer is unknown, never healthy by assumption."/><div className="operations-slo-grid">{slos.length===0?<p className="axis-muted">No SLOs are configured. Monitoring remains informative and does not alert.</p>:slos.map(item=><div key={item.metric_key}><Status value={item.status}/><strong>{humanize(item.metric_key)}</strong><span>{item.value===undefined?'No measured value':`Measured ${item.value}`} · {item.comparator} {item.target} · {Math.round(item.window_seconds/60)} min · {item.sample_count} samples</span></div>)}</div></article>
      <article className="card operations-card"><CardHeading title="Incident notifications" note="The destination is a secret reference; webhook credentials never enter the console."/><form className="operations-form" onSubmit={event=>{event.preventDefault();void run('notifications',async key=>{await api('/api/operations/notifications','PUT',{enabled:notification.enabled,webhook_secret_ref:notification.webhook_secret_ref,expected_revision:notification.revision},key)})}}><label className="form-group"><span>Enabled</span><input type="checkbox" checked={notification.enabled} onChange={event=>setNotification({...notification,enabled:event.currentTarget.checked})}/></label><label className="form-group"><span>Webhook secret reference</span><input required={notification.enabled} value={notification.webhook_secret_ref} onChange={event=>setNotification({...notification,webhook_secret_ref:event.currentTarget.value})} placeholder="projects/…/secrets/ops-webhook"/></label><button className="btn-primary" disabled={!!busy}>Save notifications</button></form></article>
    </div> : null}

    {activeTab === 'retention' ? <div className="operations-grid">
      <article className="card operations-card"><CardHeading icon={<ShieldAlert/>} title="Create legal hold" note="Covered data cannot be purged until the hold is released."/><form className="operations-form" onSubmit={event=>{event.preventDefault();void run('hold',async key=>{await api('/api/operations/legal-holds','POST',holdScope,key)})}}><label className="form-group">Scope<select value={holdScope.scope_type} onChange={event=>setHoldScope({...holdScope,scope_type:event.currentTarget.value,scope_id:event.currentTarget.value==='organization'?orgId:''})}>{['organization','virployee','work_subject','case','audit_chain','export'].map(value=><option key={value}>{value}</option>)}</select></label><label className="form-group">Scope ID<input required value={holdScope.scope_id} onChange={event=>setHoldScope({...holdScope,scope_id:event.currentTarget.value})}/></label><label className="form-group">Reason code<input required value={holdScope.reason_code} onChange={event=>setHoldScope({...holdScope,reason_code:event.currentTarget.value})}/></label><button className="btn-primary" disabled={!!busy}>Create hold</button></form></article>
      <article className="card operations-card"><CardHeading icon={<FileArchive/>} title="Create export" note="The artifact publishes only when every selected section succeeds."/><div className="operations-export-create"><p>Organization governance metadata · audit, approvals, policies, grants, incidents and holds.</p><button className="btn-primary" disabled={!!busy} onClick={()=>void run('export',async key=>{await api('/api/operations/exports','POST',{scope_type:'organization',scope_id:orgId,categories:['audit','approvals','policies','role_grants','incidents','legal_holds']},key)})}>Create organization export</button></div></article>
      <article className="card operations-card operations-card--wide"><CardHeading title="Preservation ledger"/><div className="table-wrap"><table><thead><tr><th>Scope</th><th>Reason</th><th>Status</th><th>Created</th><th /></tr></thead><tbody>{holds.length===0?<EmptyRow span={5} text="No legal holds are active or released."/>:holds.map(item=><tr key={item.id}><td>{item.scope_type}<code>{short(item.scope_id)}</code></td><td>{item.reason_code}</td><td><Status value={item.status}/></td><td>{formatDateTime24(item.created_at)}</td><td>{item.status==='active'?<button className="btn-secondary" disabled={!!busy} onClick={()=>void run(`release-${item.id}`,async key=>{await api(`/api/operations/legal-holds/${item.id}/release`,'POST',{reason_code:'operator_released',expected_revision:item.revision},key)})}>Release</button>:null}</td></tr>)}</tbody></table></div></article>
      <article className="card operations-card operations-card--wide"><CardHeading title="Export artifacts" note="Manifest and per-file hashes make tampering visible."/><div className="table-wrap"><table><thead><tr><th>Requested</th><th>Scope</th><th>Categories</th><th>Status</th><th>Manifest</th><th>Expires</th><th /></tr></thead><tbody>{exports.length===0?<EmptyRow span={7} text="No enterprise exports have been requested."/>:exports.map(item=><tr key={item.id}><td>{formatDateTime24(item.requested_at)}</td><td>{item.scope_type}</td><td>{Array.isArray(item.categories)?item.categories.join(', '):'—'}</td><td><Status value={item.status}/></td><td><code>{short(item.manifest_hash||'')}</code></td><td>{item.expires_at?formatDateTime24(item.expires_at):'—'}</td><td>{item.status==='ready'?<button className="btn-secondary" disabled={!!busy} onClick={()=>void run(`download-${item.id}`,async()=>{await downloadExport(item)})}>Download</button>:null}</td></tr>)}</tbody></table></div></article>
    </div> : null}
  </section>
}

function ServiceNode({name,state,detail}:{name:string;state:string;detail:string}){return <div className={`operations-service operations-service--${state}`}><span>{name}</span><strong>{state}</strong><small>{detail}</small></div>}
function CardHeading({icon,title,note}:{icon?:ReactNode;title:string;note?:string}){return <div className="card-header operations-card__heading"><div>{icon}<span><h3>{title}</h3>{note?<p>{note}</p>:null}</span></div></div>}
function Status({value}:{value:string}){const tone=['ready','healthy','succeeded','closed','resolved','active','info'].includes(value)?'success':['blocked','critical','dead_letter','failed','open'].includes(value)?'danger':['degraded','warning','paused','pending','cancel_requested','acknowledged','suppressed'].includes(value)?'warning':'muted';return <span className={`axis-status-badge axis-status-badge--${tone}`}>{humanize(value)}</span>}
function EmptyRow({span,text}:{span:number;text:string}){return <tr><td colSpan={span} className="operations-empty">{text}</td></tr>}
function short(value:string){if(!value)return '—';return value.length>16?`${value.slice(0,8)}…${value.slice(-6)}`:value}
function humanize(value:string){return value.replaceAll('_',' ')}
function message(cause:unknown,fallback:string){return cause instanceof Error?cause.message:fallback}
