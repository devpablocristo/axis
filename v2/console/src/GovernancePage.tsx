import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  createFunctionalRoleGrant,
  createGovernancePolicy,
  createGovernancePolicyVersion,
  decideGovernancePolicyPromotion,
  getGovernancePolicy,
  listFunctionalRoleDefinitions,
  listFunctionalRoleGrants,
  listGovernancePolicies,
  listGovernancePolicyChanges,
  listGovernancePolicyEvaluations,
  listGovernancePolicyPromotions,
  requestGovernancePolicyPromotion,
  revokeFunctionalRoleGrant,
  simulateGovernancePolicyVersion,
  type FunctionalRoleDefinition,
  type FunctionalRoleGrant,
  type GovernancePolicy,
  type GovernancePolicyChange,
  type GovernancePolicyEvaluation,
  type GovernancePolicyPromotion,
  type GovernancePolicySimulation,
  type GovernancePolicyVersion,
} from './api'
import { formatDateTime24 } from './formatters'

type Props = { orgId: string; principalId: string; productSurface: string }
type Risk = 'low' | 'medium' | 'high' | 'critical'

export function GovernancePage({ orgId, principalId, productSurface }: Props) {
  const [definitions, setDefinitions] = useState<FunctionalRoleDefinition[]>([])
  const [grants, setGrants] = useState<FunctionalRoleGrant[]>([])
  const [policies, setPolicies] = useState<GovernancePolicy[]>([])
  const [promotions, setPromotions] = useState<GovernancePolicyPromotion[]>([])
  const [evaluations, setEvaluations] = useState<GovernancePolicyEvaluation[]>([])
  const [changes, setChanges] = useState<GovernancePolicyChange[]>([])
  const [simulations, setSimulations] = useState<Record<string, GovernancePolicySimulation>>({})
  const [selectedPolicyId, setSelectedPolicyId] = useState('')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [grantDraft, setGrantDraft] = useState({ user_id: '', role_key: 'auditor' as FunctionalRoleDefinition['key'], product_surface: '', action_type_pattern: '*', resource_type: '', resource_id: '', max_risk_class: 'critical' as Risk, valid_until: futureLocalDate(90) })
  const [policyDraft, setPolicyDraft] = useState({ policy_key: '', name: '', description: '' })
  const [versionDraft, setVersionDraft] = useState({ product_surface: productSurface, action_type_pattern: '*', target_system: '', requester_type: 'virployee', expression: 'true', effect: 'deny' as GovernancePolicyVersion['effect'], risk_override: '' as GovernancePolicyVersion['risk_override'], priority: 100 })

  const load = useCallback(async () => {
    setError('')
    try {
      const basePolicies = await listGovernancePolicies(orgId, principalId)
      const detailed = await Promise.all(basePolicies.map((policy) => getGovernancePolicy(policy.id, orgId, principalId)))
      const [nextPromotions, nextEvaluations, nextChanges] = await Promise.all([
        listGovernancePolicyPromotions(orgId, principalId),
        listGovernancePolicyEvaluations(orgId, principalId),
        listGovernancePolicyChanges(orgId, principalId),
      ])
      setPolicies(detailed)
      setPromotions(nextPromotions)
      setEvaluations(nextEvaluations)
      setChanges(nextChanges)
      setSelectedPolicyId((current) => current && detailed.some((item) => item.id === current) ? current : detailed[0]?.id ?? '')
      try {
        const [nextDefinitions, nextGrants] = await Promise.all([
          listFunctionalRoleDefinitions(orgId, principalId), listFunctionalRoleGrants(orgId, principalId),
        ])
        setDefinitions(nextDefinitions)
        setGrants(nextGrants)
      } catch {
        setDefinitions([])
        setGrants([])
      }
    } catch (cause) {
      setError(message(cause, 'Could not load governance authority'))
    }
  }, [principalId, orgId])

  useEffect(() => { void load() }, [load])
  useEffect(() => { setVersionDraft((current) => ({ ...current, product_surface: productSurface })) }, [productSurface])

  const selectedPolicy = policies.find((policy) => policy.id === selectedPolicyId) ?? null
  const versionById = useMemo(() => new Map(policies.flatMap((policy) => policy.versions ?? []).map((version) => [version.id, version])), [policies])

  const run = async (key: string, action: () => Promise<void>) => {
    if (busy) return
    setBusy(key)
    setError('')
    try { await action() } catch (cause) { setError(message(cause, 'Governance operation failed')) } finally { setBusy('') }
  }

  return (
    <section className="governance-page">
      <header className="governance-brief governance-brief--compact">
        <div className="decision-precedence" aria-label="Policy decision precedence"><strong>Decision order</strong><span className="precedence-deny">Deny</span><i>then</i><span className="precedence-approval">Approval</span><i>then</i><span className="precedence-allow">Allow</span></div>
      </header>
      {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}

      <div className="governance-layout">
        <article className="card governance-panel">
          <div className="card-header"><div><span className="governance-kicker">People</span><h2>Functional role grants</h2></div><small>Membership stays owner · admin · member</small></div>
          {definitions.length === 0 ? <p className="axis-muted">Role grants are hidden unless your scope includes RBAC read access.</p> : (
            <form className="governance-form" onSubmit={(event) => {
              event.preventDefault()
              void run('grant', async () => {
                await createFunctionalRoleGrant({ ...grantDraft, valid_from: new Date().toISOString(), valid_until: new Date(grantDraft.valid_until).toISOString() }, orgId, principalId)
                setGrantDraft((current) => ({ ...current, user_id: '' }))
                await load()
              })
            }}>
              <label className="form-group">User ID<input required value={grantDraft.user_id} onChange={(event) => setGrantDraft({ ...grantDraft, user_id: event.currentTarget.value })} /></label>
              <label className="form-group">Functional role<select value={grantDraft.role_key} onChange={(event) => setGrantDraft({ ...grantDraft, role_key: event.currentTarget.value as FunctionalRoleDefinition['key'] })}>{definitions.map((role) => <option key={role.key} value={role.key}>{role.key}</option>)}</select></label>
              <label className="form-group">Product scope<input value={grantDraft.product_surface} onChange={(event) => setGrantDraft({ ...grantDraft, product_surface: event.currentTarget.value })} placeholder="Empty = all" /></label>
              <label className="form-group">Action pattern<input value={grantDraft.action_type_pattern} onChange={(event) => setGrantDraft({ ...grantDraft, action_type_pattern: event.currentTarget.value })} /></label>
              <label className="form-group">Resource type<input value={grantDraft.resource_type} onChange={(event) => setGrantDraft({ ...grantDraft, resource_type: event.currentTarget.value })} /></label>
              <label className="form-group">Resource ID<input value={grantDraft.resource_id} onChange={(event) => setGrantDraft({ ...grantDraft, resource_id: event.currentTarget.value })} /></label>
              <label className="form-group">Maximum risk<select value={grantDraft.max_risk_class} onChange={(event) => setGrantDraft({ ...grantDraft, max_risk_class: event.currentTarget.value as Risk })}>{risks.map((risk) => <option key={risk}>{risk}</option>)}</select></label>
              <label className="form-group">Valid until<input type="datetime-local" required value={grantDraft.valid_until} onChange={(event) => setGrantDraft({ ...grantDraft, valid_until: event.currentTarget.value })} /></label>
              <button className="btn-primary" disabled={busy !== '' || !grantDraft.user_id.trim()}>{busy === 'grant' ? 'Granting…' : 'Grant role'}</button>
            </form>
          )}
          <div className="governance-ledger-list">{grants.map((grant) => <div key={grant.id} className={grant.revoked_at ? 'is-muted' : ''}><div><strong>{grant.role_key}</strong><span>{grant.user_id}</span></div><small>{grant.product_surface || 'all products'} · {grant.action_type_pattern} · risk ≤ {grant.max_risk_class} · until {formatDateTime24(grant.valid_until)}</small>{!grant.revoked_at ? <button className="btn-secondary" disabled={busy !== ''} onClick={() => void run(`revoke-${grant.id}`, async () => { await revokeFunctionalRoleGrant(grant.id, grant.revision, orgId, principalId); await load() })}>Revoke</button> : <span className="status-badge">Revoked</span>}</div>)}</div>
        </article>

        <article className="card governance-panel governance-policy-builder">
          <div className="card-header"><div><span className="governance-kicker">Rules</span><h2>Versioned CEL policies</h2></div><small>Draft → shadow → active → retired</small></div>
          <form className="governance-artifact-form" onSubmit={(event) => {
            event.preventDefault()
            void run('policy', async () => { const created = await createGovernancePolicy(policyDraft, orgId, principalId); setPolicyDraft({ policy_key: '', name: '', description: '' }); await load(); setSelectedPolicyId(created.id) })
          }}>
            <label className="form-group">Policy key<input required value={policyDraft.policy_key} onChange={(event) => setPolicyDraft({ ...policyDraft, policy_key: event.currentTarget.value })} /></label>
            <label className="form-group">Name<input required value={policyDraft.name} onChange={(event) => setPolicyDraft({ ...policyDraft, name: event.currentTarget.value })} /></label>
            <label className="form-group governance-wide">Description<input value={policyDraft.description} onChange={(event) => setPolicyDraft({ ...policyDraft, description: event.currentTarget.value })} /></label>
            <button className="btn-secondary" disabled={busy !== ''}>Create policy</button>
          </form>
          <label className="form-group">Policy<select value={selectedPolicyId} onChange={(event) => setSelectedPolicyId(event.currentTarget.value)}><option value="">Select…</option>{policies.map((policy) => <option key={policy.id} value={policy.id}>{policy.name} · {policy.policy_key}</option>)}</select></label>
          {selectedPolicy ? <form className="governance-version-form" onSubmit={(event) => {
            event.preventDefault()
            void run('version', async () => { await createGovernancePolicyVersion(selectedPolicy.id, versionDraft, orgId, principalId); await load() })
          }}>
            <label className="form-group">Product<input value={versionDraft.product_surface} onChange={(event) => setVersionDraft({ ...versionDraft, product_surface: event.currentTarget.value })} /></label>
            <label className="form-group">Action pattern<input value={versionDraft.action_type_pattern} onChange={(event) => setVersionDraft({ ...versionDraft, action_type_pattern: event.currentTarget.value })} /></label>
            <label className="form-group">Target system<input value={versionDraft.target_system} onChange={(event) => setVersionDraft({ ...versionDraft, target_system: event.currentTarget.value })} /></label>
            <label className="form-group">Requester<select value={versionDraft.requester_type} onChange={(event) => setVersionDraft({ ...versionDraft, requester_type: event.currentTarget.value })}><option value="">All</option><option value="virployee">Virployee</option><option value="human">Human</option></select></label>
            <label className="form-group">Effect<select value={versionDraft.effect} onChange={(event) => setVersionDraft({ ...versionDraft, effect: event.currentTarget.value as GovernancePolicyVersion['effect'] })}><option value="deny">Deny</option><option value="require_approval">Require approval</option><option value="allow">Allow</option></select></label>
            <label className="form-group">Raise risk to<select value={versionDraft.risk_override} onChange={(event) => setVersionDraft({ ...versionDraft, risk_override: event.currentTarget.value as GovernancePolicyVersion['risk_override'] })}><option value="">No override</option>{risks.map((risk) => <option key={risk}>{risk}</option>)}</select></label>
            <label className="form-group">Priority<input type="number" value={versionDraft.priority} onChange={(event) => setVersionDraft({ ...versionDraft, priority: Number(event.currentTarget.value) })} /></label>
            <label className="form-group governance-cel">CEL expression<textarea required rows={7} spellCheck={false} value={versionDraft.expression} onChange={(event) => setVersionDraft({ ...versionDraft, expression: event.currentTarget.value })} /><small>Available: action, resource, product, requester, authority, time.</small></label>
            <button className="btn-primary" disabled={busy !== ''}>{busy === 'version' ? 'Compiling…' : 'Compile draft version'}</button>
          </form> : <p className="axis-muted">Create or select a policy to add an immutable version.</p>}
        </article>
      </div>

      <section className="card governance-panel">
        <div className="card-header"><div><span className="governance-kicker">Lifecycle</span><h2>Versions and promotions</h2></div><button className="btn-secondary" onClick={() => void load()}>Refresh ledger</button></div>
        <div className="governance-version-list">{policies.flatMap((policy) => (policy.versions ?? []).map((version) => ({ policy, version }))).map(({ policy, version }) => {
          const simulation = simulations[version.id]
          const target: 'shadow' | 'active' = version.state === 'draft' ? 'shadow' : 'active'
          return <article key={version.id}><div><span className={`policy-state policy-state--${version.state}`}>{version.state}</span><strong>{policy.policy_key} · v{version.version}</strong><code>{version.effect}</code></div><p>{version.expression}</p><small>{version.action_type_pattern} · priority {version.priority} · {version.content_hash.slice(0, 12)}</small><div className="governance-actions"><button className="btn-secondary" disabled={busy !== ''} onClick={() => void run(`simulate-${version.id}`, async () => { const report = await simulateGovernancePolicyVersion(version.id, orgId, principalId); setSimulations((current) => ({ ...current, [version.id]: report })) })}>Simulate</button>{simulation ? <><span>{simulation.would_match}/{simulation.total_evaluated} matches</span>{version.state !== 'active' ? <button className="btn-primary" disabled={busy !== ''} onClick={() => void run(`promote-${version.id}`, async () => { await requestGovernancePolicyPromotion(version.id, simulation.id, target, orgId, principalId); await load() })}>{version.state === 'retired' ? 'Request rollback' : `Request ${target}`}</button> : null}</> : null}</div></article>
        })}</div>
        <div className="governance-promotion-list"><h3>Promotion decisions</h3>{promotions.filter((item) => item.status === 'pending').map((promotion) => <div key={promotion.id}><span><strong>{versionById.get(promotion.policy_version_id)?.state ?? 'version'} → {promotion.target_state}</strong><small>Requested by {promotion.requested_by}</small></span><button className="btn-secondary" disabled={busy !== ''} onClick={() => void run(`reject-${promotion.id}`, async () => { await decideGovernancePolicyPromotion(promotion.id, 'reject', orgId, principalId); await load() })}>Reject</button><button className="btn-primary" disabled={busy !== ''} onClick={() => void run(`approve-${promotion.id}`, async () => { await decideGovernancePolicyPromotion(promotion.id, 'approve', orgId, principalId); await load() })}>Approve</button></div>)}{promotions.every((item) => item.status !== 'pending') ? <p className="axis-muted">No promotion is waiting for an independent decision.</p> : null}</div>
      </section>

      <div className="governance-audit-grid">
        <section className="card governance-panel"><div className="card-header"><h2>Policy evaluations</h2><small>Metadata only</small></div><div className="governance-audit-list">{evaluations.slice(0, 30).map((item) => <div key={item.id}><span className={`policy-mode policy-mode--${item.mode}`}>{item.mode}</span><strong>{item.effect}</strong><span>{item.matched ? 'matched' : 'not matched'}</span><code>{item.input_hash.slice(0, 12)}</code><time>{formatDateTime24(item.created_at)}</time></div>)}</div></section>
        <section className="card governance-panel"><div className="card-header"><h2>Change ledger</h2><small>Append-only history</small></div><div className="governance-audit-list">{changes.slice(0, 30).map((item) => <div key={item.id}><strong>{item.action}</strong><span>{item.summary}</span><code>{item.actor_id}</code><time>{formatDateTime24(item.created_at)}</time></div>)}</div></section>
      </div>
    </section>
  )
}

const risks: Risk[] = ['low', 'medium', 'high', 'critical']
function futureLocalDate(days: number): string { const value = new Date(Date.now() + days * 86400000); const offset = value.getTimezoneOffset() * 60000; return new Date(value.getTime() - offset).toISOString().slice(0, 16) }
function message(cause: unknown, fallback: string): string { return cause instanceof Error ? cause.message : fallback }
