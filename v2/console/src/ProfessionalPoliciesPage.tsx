import { useCallback, useEffect, useState } from 'react'
import {
  createProfessionalPolicyPack,
  listJobRoles,
  listProfessionalPolicyPacks,
  type JobRole,
  type ProfessionalPolicyPack,
} from './api'

export function ProfessionalPoliciesPage({ orgId, principalId }: { orgId: string; principalId: string }) {
  const [packs, setPacks] = useState<ProfessionalPolicyPack[]>([])
  const [roles, setRoles] = useState<JobRole[]>([])
  const [draft, setDraft] = useState({
    policy_key: '', name: '', version: 1, job_role_id: '', allowed_topics: '', prohibited_topics: '',
    allowed_capabilities: '', prohibited_capabilities: '', out_of_scope: 'abstain' as 'abstain' | 'escalate', delegation_required: false,
  })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const [nextPacks, nextRoles] = await Promise.all([
        listProfessionalPolicyPacks(orgId, principalId), listJobRoles('active', orgId, principalId),
      ])
      setPacks(nextPacks)
      setRoles(nextRoles)
      setError('')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not load professional policies')
    }
  }, [principalId, orgId])

  useEffect(() => { void load() }, [load])

  return (
    <section className="page-section professional-policies-page">
      {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}
      <div className="card workforce-card">
        <div className="card-header"><h2>New professional policy pack</h2></div>
        <form className="policy-pack-form" onSubmit={(event) => {
          event.preventDefault()
          if (busy) return
          setBusy(true)
          setError('')
          void createProfessionalPolicyPack({
            policy_key: draft.policy_key.trim(), name: draft.name.trim(), version: draft.version,
            job_role_id: draft.job_role_id || undefined,
            rules: {
              allowed_topics: lines(draft.allowed_topics), prohibited_topics: lines(draft.prohibited_topics),
              out_of_scope: draft.out_of_scope, allowed_capabilities: lines(draft.allowed_capabilities),
              prohibited_capabilities: lines(draft.prohibited_capabilities), delegation_required: draft.delegation_required,
            },
          }, orgId, principalId)
            .then(async () => {
              setDraft({ policy_key: '', name: '', version: 1, job_role_id: '', allowed_topics: '', prohibited_topics: '', allowed_capabilities: '', prohibited_capabilities: '', out_of_scope: 'abstain', delegation_required: false })
              await load()
            })
            .catch((cause) => setError(cause instanceof Error ? cause.message : 'Could not create policy pack'))
            .finally(() => setBusy(false))
        }}>
          <label className="form-group">Policy key<input value={draft.policy_key} onChange={(event) => setDraft({ ...draft, policy_key: event.currentTarget.value })} /></label>
          <label className="form-group">Name<input value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.currentTarget.value })} /></label>
          <label className="form-group">Version<input type="number" min={1} value={draft.version} onChange={(event) => setDraft({ ...draft, version: Number(event.currentTarget.value) })} /></label>
          <label className="form-group">Job Role<select value={draft.job_role_id} onChange={(event) => setDraft({ ...draft, job_role_id: event.currentTarget.value })}><option value="">Reusable</option>{roles.map((role) => <option key={role.id} value={role.id}>{role.name}</option>)}</select></label>
          <label className="form-group">Allowed topics<textarea rows={4} value={draft.allowed_topics} onChange={(event) => setDraft({ ...draft, allowed_topics: event.currentTarget.value })} placeholder="One per line" /></label>
          <label className="form-group">Prohibited topics<textarea rows={4} value={draft.prohibited_topics} onChange={(event) => setDraft({ ...draft, prohibited_topics: event.currentTarget.value })} placeholder="One per line" /></label>
          <label className="form-group">Allowed capabilities<textarea rows={3} value={draft.allowed_capabilities} onChange={(event) => setDraft({ ...draft, allowed_capabilities: event.currentTarget.value })} placeholder="Capability keys" /></label>
          <label className="form-group">Prohibited capabilities<textarea rows={3} value={draft.prohibited_capabilities} onChange={(event) => setDraft({ ...draft, prohibited_capabilities: event.currentTarget.value })} placeholder="Capability keys" /></label>
          <label className="form-group">Outside scope<select value={draft.out_of_scope} onChange={(event) => setDraft({ ...draft, out_of_scope: event.currentTarget.value as 'abstain' | 'escalate' })}><option value="abstain">Abstain</option><option value="escalate">Escalate</option></select></label>
          <label className="form-group policy-pack-check"><input type="checkbox" checked={draft.delegation_required} onChange={(event) => setDraft({ ...draft, delegation_required: event.currentTarget.checked })} /> Delegation required</label>
          <button className="btn-primary" disabled={busy || !draft.policy_key.trim() || !draft.name.trim()}>{busy ? 'Creating...' : 'Create version'}</button>
        </form>
      </div>
      <div className="policy-pack-list">
        {packs.map((pack) => (
          <article key={pack.id} className="card workforce-card">
            <div className="card-header"><h2>{pack.name}</h2><span>v{pack.version}</span></div>
            <p><code>{pack.policy_key}</code></p>
            <p className="axis-muted">{pack.rules.allowed_topics.length} allowed · {pack.rules.prohibited_topics.length} prohibited · {pack.rules.out_of_scope}</p>
            <p className="axis-muted">Delegation {pack.rules.delegation_required ? 'required' : 'optional'}</p>
          </article>
        ))}
      </div>
    </section>
  )
}

function lines(value: string): string[] {
  return Array.from(new Set(value.split(/[,\n]/).map((item) => item.trim()).filter(Boolean)))
}
