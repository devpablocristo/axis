import { useEffect, useMemo, useState, type ReactNode } from 'react'
import {
  claimHumanReview,
  createHandoff,
  decideHandoff,
  listAssistCases,
  listCapabilities,
  listHandoffs,
  listHumanReviews,
  listOrchestrationPolicies,
  listSpecialistRoutes,
  listVirployees,
  resolveHumanReview,
  upsertOrchestrationPolicy,
  upsertSpecialistRoute,
  type AssistCase,
  type Capability,
  type Handoff,
  type HumanReview,
  type OrchestrationPolicy,
  type SpecialistRoute,
  type Virployee,
} from './api'

type Props = { orgId: string; principalId: string; productSurface: string }

const emptyPolicy = {
  assistType: '', entrypoint: '', mode: 'shadow' as OrchestrationPolicy['mode'],
  selector: '', synthesis: '', schema: '{\n  "type": "object"\n}', maxSpecialists: 3,
  consultationTimeout: 120, orchestrationTimeout: 300,
}
const emptyRoute = {
  assistType: '', entrypoint: '', code: '', target: '', capability: '',
  requirement: 'selector_allowed' as SpecialistRoute['requirement_mode'], enabled: true,
}

export function CoordinationPage({ orgId, principalId, productSurface }: Props) {
  const [cases, setCases] = useState<AssistCase[]>([])
  const [handoffs, setHandoffs] = useState<Handoff[]>([])
  const [reviews, setReviews] = useState<HumanReview[]>([])
  const [policies, setPolicies] = useState<OrchestrationPolicy[]>([])
  const [routes, setRoutes] = useState<SpecialistRoute[]>([])
  const [virployees, setVirployees] = useState<Virployee[]>([])
  const [capabilities, setCapabilities] = useState<Capability[]>([])
  const [loading, setLoading] = useState(false)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [policyForm, setPolicyForm] = useState(emptyPolicy)
  const [routeForm, setRouteForm] = useState(emptyRoute)

  const virployeeByID = useMemo(() => new Map(virployees.map((item) => [item.id, item.name])), [virployees])
  const capabilityByID = useMemo(() => new Map(capabilities.map((item) => [item.id, item.name])), [capabilities])
  const pendingHandoffs = handoffs.filter((item) => item.status === 'pending')
  const openReviews = reviews.filter((item) => item.status !== 'resolved')

  useEffect(() => {
    if (!orgId || !principalId) return
    void load()
  }, [orgId, principalId])

  async function load() {
    setLoading(true)
    setError('')
    try {
      const [nextCases, nextHandoffs, nextReviews, nextPolicies, nextRoutes, nextVirployees, nextCapabilities] = await Promise.all([
        listAssistCases(orgId, principalId), listHandoffs(orgId, principalId),
        listHumanReviews(orgId, principalId), listOrchestrationPolicies(orgId, principalId),
        listSpecialistRoutes(orgId, principalId), listVirployees('active', orgId, principalId),
        listCapabilities('active', orgId, principalId),
      ])
      setCases(nextCases); setHandoffs(nextHandoffs); setReviews(nextReviews)
      setPolicies(nextPolicies); setRoutes(nextRoutes); setVirployees(nextVirployees); setCapabilities(nextCapabilities)
    } catch (loadError) {
      setError(message(loadError, 'Could not load specialist coordination'))
    } finally {
      setLoading(false)
    }
  }

  async function run(key: string, operation: () => Promise<unknown>) {
    if (busy) return
    setBusy(key); setError('')
    try { await operation(); await load() } catch (operationError) {
      setError(message(operationError, 'Could not update specialist coordination'))
    } finally { setBusy('') }
  }

  function requestHandoff(item: AssistCase) {
    const target = window.prompt('Target Virployee ID')?.trim()
    if (!target) return
    const reason = window.prompt('Reason code', 'specialist_ownership_transfer')?.trim()
    if (!reason) return
    const note = window.prompt('Operational note (optional)')?.trim() ?? ''
    void run(`case:${item.id}`, () => createHandoff({ case_id: item.id, to_virployee_id: target, reason_code: reason, note }, orgId, principalId))
  }

  function handoffDecision(item: Handoff, decision: 'accept' | 'reject' | 'cancel') {
    const note = decision === 'cancel' ? '' : window.prompt(`${decision} note (optional)`)?.trim() ?? ''
    void run(`handoff:${item.id}`, () => decideHandoff(item.id, decision, item.version, orgId, principalId, note))
  }

  function resolveReview(item: HumanReview) {
    const raw = window.prompt('Outcome: handled_externally, handoff_requested or dismissed', 'handled_externally')?.trim()
    if (raw !== 'handled_externally' && raw !== 'handoff_requested' && raw !== 'dismissed') return
    const note = window.prompt('Review note (optional)')?.trim() ?? ''
    const handoffID = raw === 'handoff_requested' ? window.prompt('Handoff ID')?.trim() ?? '' : ''
    if (raw === 'handoff_requested' && !handoffID) return
    void run(`review:${item.id}`, () => resolveHumanReview(item.id, raw, orgId, principalId, note, handoffID))
  }

  function savePolicy() {
    let outputSchema: Record<string, unknown>
    try {
      const parsed = JSON.parse(policyForm.schema) as unknown
      if (parsed == null || Array.isArray(parsed) || typeof parsed !== 'object') throw new Error('schema must be an object')
      outputSchema = parsed as Record<string, unknown>
    } catch (schemaError) {
      setError(message(schemaError, 'Output schema must be valid JSON')); return
    }
    void run('policy', () => upsertOrchestrationPolicy({
      product_surface: productSurface, assist_type: policyForm.assistType,
      entrypoint_virployee_id: policyForm.entrypoint, mode: policyForm.mode,
      selector_capability_id: policyForm.selector, synthesis_capability_id: policyForm.synthesis,
      output_schema: outputSchema, max_specialists: policyForm.maxSpecialists,
      consultation_timeout_seconds: policyForm.consultationTimeout,
      orchestration_timeout_seconds: policyForm.orchestrationTimeout,
    }, orgId, principalId))
  }

  function saveRoute() {
    void run('route', () => upsertSpecialistRoute({
      product_surface: productSurface, assist_type: routeForm.assistType,
      entrypoint_virployee_id: routeForm.entrypoint, specialty_code: routeForm.code,
      target_virployee_id: routeForm.target, capability_id: routeForm.capability,
      requirement_mode: routeForm.requirement, enabled: routeForm.enabled,
    }, orgId, principalId))
  }

  return (
    <section className="page-section coordination-page">
      <div className="coordination-toolbar coordination-toolbar--actions">
        <button type="button" className="btn-secondary" onClick={() => void load()} disabled={loading || Boolean(busy)}>
          {loading ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>
      {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}

      <div className="coordination-metrics">
        <Metric label="Open cases" value={cases.filter((item) => item.status !== 'closed').length} />
        <Metric label="Pending handoffs" value={pendingHandoffs.length} />
        <Metric label="Human reviews" value={openReviews.length} />
        <Metric label="Active routes" value={routes.filter((item) => item.enabled).length} />
      </div>

      <div className="coordination-grid">
        <CoordinationSection title="Case ownership" empty="No assist cases yet.">
          {cases.map((item) => <article className="coordination-card" key={item.id}>
            <div><strong>{item.assist_type}</strong><span className={`coordination-status coordination-status--${item.status}`}>{item.status}</span></div>
            <p>{item.subject_id}</p>
            <small>Owner: {nameOf(item.owner_virployee_id, virployeeByID)} · v{item.version}</small>
            {item.status !== 'closed' ? <button type="button" className="btn-secondary" disabled={Boolean(busy)} onClick={() => requestHandoff(item)}>Request handoff</button> : null}
          </article>)}
        </CoordinationSection>

        <CoordinationSection title="Handoff inbox" empty="No handoffs.">
          {handoffs.map((item) => <article className="coordination-card" key={item.id}>
            <div><strong>{item.reason_code}</strong><span className={`coordination-status coordination-status--${item.status}`}>{item.status}</span></div>
            <p>{nameOf(item.from_virployee_id, virployeeByID)} → {nameOf(item.to_virployee_id, virployeeByID)}</p>
            <small>Expires {formatTime(item.expires_at)} · v{item.version}</small>
            {item.status === 'pending' ? <div className="coordination-actions">
              <button type="button" className="btn-primary" disabled={Boolean(busy)} onClick={() => handoffDecision(item, 'accept')}>Accept</button>
              <button type="button" className="btn-secondary" disabled={Boolean(busy)} onClick={() => handoffDecision(item, 'reject')}>Reject</button>
              <button type="button" className="btn-secondary" disabled={Boolean(busy)} onClick={() => handoffDecision(item, 'cancel')}>Cancel</button>
            </div> : null}
          </article>)}
        </CoordinationSection>

        <CoordinationSection title="Human review inbox" empty="No human reviews.">
          {reviews.map((item) => <article className="coordination-card" key={item.id}>
            <div><strong>{item.reason_code}</strong><span className={`coordination-status coordination-status--${item.urgency}`}>{item.urgency}</span></div>
            <p>Run {short(item.root_run_id)} · {item.status}</p>
            <small>{item.reviewer_user_id ? `Reviewer: ${item.reviewer_user_id}` : 'Unclaimed'}</small>
            {item.status === 'pending' ? <button type="button" className="btn-primary" disabled={Boolean(busy)} onClick={() => void run(`review:${item.id}`, () => claimHumanReview(item.id, orgId, principalId))}>Claim</button> : null}
            {item.status === 'claimed' ? <button type="button" className="btn-primary" disabled={Boolean(busy)} onClick={() => resolveReview(item)}>Resolve</button> : null}
          </article>)}
        </CoordinationSection>
      </div>

      <section className="coordination-builder">
        <h2>Orchestration policy</h2>
        <div className="coordination-form">
          <Field label="Assist type"><input value={policyForm.assistType} onChange={(e) => setPolicyForm({ ...policyForm, assistType: e.target.value })} /></Field>
          <Field label="Entrypoint"><EntitySelect value={policyForm.entrypoint} onChange={(entrypoint) => setPolicyForm({ ...policyForm, entrypoint })} items={virployees.map((item) => ({ id: item.id, name: item.name }))} /></Field>
          <Field label="Mode"><select value={policyForm.mode} onChange={(e) => setPolicyForm({ ...policyForm, mode: e.target.value as OrchestrationPolicy['mode'] })}><option value="disabled">disabled</option><option value="shadow">shadow</option><option value="active">active</option></select></Field>
          <Field label="Selector capability"><EntitySelect value={policyForm.selector} onChange={(selector) => setPolicyForm({ ...policyForm, selector })} items={capabilities.map((item) => ({ id: item.id, name: item.name }))} /></Field>
          <Field label="Synthesis capability"><EntitySelect value={policyForm.synthesis} onChange={(synthesis) => setPolicyForm({ ...policyForm, synthesis })} items={capabilities.map((item) => ({ id: item.id, name: item.name }))} /></Field>
          <Field label="Output schema"><textarea rows={4} value={policyForm.schema} onChange={(e) => setPolicyForm({ ...policyForm, schema: e.target.value })} /></Field>
          <button type="button" className="btn-primary" disabled={Boolean(busy)} onClick={savePolicy}>Save policy</button>
        </div>
        <div className="coordination-records">{policies.map((item) => <span key={item.id}>{item.assist_type} · {nameOf(item.entrypoint_virployee_id, virployeeByID)} · {item.mode} · v{item.version}</span>)}</div>
      </section>

      <section className="coordination-builder">
        <h2>Specialist route</h2>
        <div className="coordination-form">
          <Field label="Assist type"><input value={routeForm.assistType} onChange={(e) => setRouteForm({ ...routeForm, assistType: e.target.value })} /></Field>
          <Field label="Entrypoint"><EntitySelect value={routeForm.entrypoint} onChange={(entrypoint) => setRouteForm({ ...routeForm, entrypoint })} items={virployees.map((item) => ({ id: item.id, name: item.name }))} /></Field>
          <Field label="Specialty code"><input placeholder="domain.specialty" value={routeForm.code} onChange={(e) => setRouteForm({ ...routeForm, code: e.target.value })} /></Field>
          <Field label="Specialist"><EntitySelect value={routeForm.target} onChange={(target) => setRouteForm({ ...routeForm, target })} items={virployees.map((item) => ({ id: item.id, name: item.name }))} /></Field>
          <Field label="Capability"><EntitySelect value={routeForm.capability} onChange={(capability) => setRouteForm({ ...routeForm, capability })} items={capabilities.map((item) => ({ id: item.id, name: item.name }))} /></Field>
          <Field label="Requirement"><select value={routeForm.requirement} onChange={(e) => setRouteForm({ ...routeForm, requirement: e.target.value as SpecialistRoute['requirement_mode'] })}><option value="selector_allowed">selector allowed</option><option value="advisory_only">advisory only</option><option value="required">required</option></select></Field>
          <button type="button" className="btn-primary" disabled={Boolean(busy)} onClick={saveRoute}>Save route</button>
        </div>
        <div className="coordination-records">{routes.map((item) => <span key={item.id}>{item.specialty_code} → {nameOf(item.target_virployee_id, virployeeByID)} · {capabilityByID.get(item.capability_id) ?? short(item.capability_id)} · {item.requirement_mode}</span>)}</div>
      </section>
    </section>
  )
}

function Metric({ label, value }: { label: string; value: number }) { return <div><strong>{value}</strong><span>{label}</span></div> }
function CoordinationSection({ title, empty, children }: { title: string; empty: string; children: ReactNode }) {
  const count = Array.isArray(children) ? children.length : 1
  return <section className="coordination-section"><h2>{title}</h2>{count === 0 ? <p className="axis-muted">{empty}</p> : children}</section>
}
function Field({ label, children }: { label: string; children: ReactNode }) { return <label><span>{label}</span>{children}</label> }
function EntitySelect({ value, onChange, items }: { value: string; onChange: (value: string) => void; items: Array<{ id: string; name: string }> }) {
  return <select value={value} onChange={(e) => onChange(e.target.value)}><option value="">Select…</option>{items.map((item) => <option value={item.id} key={item.id}>{item.name} · {short(item.id)}</option>)}</select>
}
function short(value: string) { return value ? value.slice(0, 8) : '—' }
function nameOf(id: string, names: Map<string, string>) { return names.get(id) ?? short(id) }
function formatTime(value: string) { const date = new Date(value); return Number.isNaN(date.valueOf()) ? value : date.toLocaleString() }
function message(error: unknown, fallback: string) { return error instanceof Error ? error.message : fallback }
