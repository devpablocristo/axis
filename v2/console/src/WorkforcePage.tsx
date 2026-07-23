import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  createRoutingPool,
  createWorkSubject,
  listContinuityAssignments,
  listJobRoles,
  listRoutingPoolMembers,
  listRoutingPools,
  listVirployees,
  listWorkSubjects,
  putRoutingPoolMember,
  reassignContinuityAssignment,
  resolveVirployeeRouting,
  type ContinuityAssignment,
  type JobRole,
  type RoutingPool,
  type RoutingPoolMember,
  type Virployee,
  type WorkSubject,
  type WorkSubjectKind,
} from './api'

type WorkforcePageProps = {
  orgId: string
  principalId: string
  organizationName: string
}

type ReassignmentDraft = {
  virployee_id: string
  reason: string
}

export function WorkforcePage({ orgId, principalId }: WorkforcePageProps) {
  const [subjects, setSubjects] = useState<WorkSubject[]>([])
  const [pools, setPools] = useState<RoutingPool[]>([])
  const [jobRoles, setJobRoles] = useState<JobRole[]>([])
  const [virployees, setVirployees] = useState<Virployee[]>([])
  const [members, setMembers] = useState<RoutingPoolMember[]>([])
  const [assignments, setAssignments] = useState<ContinuityAssignment[]>([])
  const [selectedPoolId, setSelectedPoolId] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [subjectDraft, setSubjectDraft] = useState<{ kind: WorkSubjectKind; display_name: string; external_ref: string }>({
    kind: 'patient', display_name: '', external_ref: '',
  })
  const [poolDraft, setPoolDraft] = useState({ name: '', job_role_id: '' })
  const [memberDraft, setMemberDraft] = useState({ virployee_id: '', max_active_subjects: 20 })
  const [routingSubjectId, setRoutingSubjectId] = useState('')
  const [reassignmentDrafts, setReassignmentDrafts] = useState<Record<string, ReassignmentDraft>>({})

  const loadBase = useCallback(async () => {
    if (!orgId || !principalId) return
    setLoading(true)
    setError('')
    try {
      const [nextSubjects, nextPools, nextRoles, nextVirployees] = await Promise.all([
        listWorkSubjects(orgId, principalId),
        listRoutingPools(orgId, principalId),
        listJobRoles('active', orgId, principalId),
        listVirployees('active', orgId, principalId),
      ])
      setSubjects(nextSubjects)
      setPools(nextPools)
      setJobRoles(nextRoles)
      setVirployees(nextVirployees)
      setSelectedPoolId((current) => current && nextPools.some((pool) => pool.id === current) ? current : nextPools[0]?.id ?? '')
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setLoading(false)
    }
  }, [principalId, orgId])

  const loadPool = useCallback(async () => {
    if (!selectedPoolId) {
      setMembers([])
      setAssignments([])
      setReassignmentDrafts({})
      return
    }
    try {
      const [nextMembers, nextAssignments] = await Promise.all([
        listRoutingPoolMembers(selectedPoolId, orgId, principalId),
        listContinuityAssignments(selectedPoolId, orgId, principalId),
      ])
      setMembers(nextMembers)
      setAssignments(nextAssignments)
      setReassignmentDrafts((current) => Object.fromEntries(nextAssignments.map((assignment) => [
        assignment.id,
        current[assignment.id]?.virployee_id !== assignment.virployee_id
          ? current[assignment.id] ?? { virployee_id: '', reason: 'manual_reassignment' }
          : { virployee_id: '', reason: 'manual_reassignment' },
      ])))
    } catch (cause) {
      setError(errorMessage(cause))
    }
  }, [principalId, selectedPoolId, orgId])

  useEffect(() => { void loadBase() }, [loadBase])
  useEffect(() => { void loadPool() }, [loadPool])

  const roleById = useMemo(() => new Map(jobRoles.map((role) => [role.id, role.name])), [jobRoles])
  const virployeeById = useMemo(() => new Map(virployees.map((item) => [item.id, item.name])), [virployees])
  const subjectById = useMemo(() => new Map(subjects.map((subject) => [subject.id, subject.display_name])), [subjects])
  const selectedPool = pools.find((pool) => pool.id === selectedPoolId)

  const updateMember = (virployeeId: string, patch: Partial<Pick<RoutingPoolMember, 'max_active_subjects' | 'enabled'>>) => {
    setMembers((current) => current.map((member) => member.virployee_id === virployeeId ? { ...member, ...patch } : member))
  }

  const updateReassignment = (assignmentId: string, patch: Partial<ReassignmentDraft>) => {
    setReassignmentDrafts((current) => {
      const existing = current[assignmentId] ?? { virployee_id: '', reason: 'manual_reassignment' }
      return {
        ...current,
        [assignmentId]: { ...existing, ...patch },
      }
    })
  }

  const runMutation = async (operation: () => Promise<void>) => {
    if (busy) return
    setBusy(true)
    setError('')
    setNotice('')
    try {
      await operation()
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="page-section workforce-page">
      {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}
      {notice ? <p role="status" className="iam-control__inline-note">{notice}</p> : null}
      {loading ? <div className="spinner" /> : (
        <div className="workforce-grid">
          <section className="card workforce-card">
            <div className="card-header"><h2>Subjects</h2></div>
            <form className="workforce-form" onSubmit={(event) => {
              event.preventDefault()
              void runMutation(async () => {
                await createWorkSubject(subjectDraft, orgId, principalId)
                setSubjectDraft({ kind: 'patient', display_name: '', external_ref: '' })
                await loadBase()
              })
            }}>
              <label className="form-group">Kind
                <select value={subjectDraft.kind} onChange={(event) => setSubjectDraft({ ...subjectDraft, kind: event.currentTarget.value as WorkSubjectKind })}>
                  <option value="patient">Patient</option><option value="case">Case</option><option value="person">Person</option>
                  <option value="organization">Organization</option><option value="team">Team</option>
                </select>
              </label>
              <label className="form-group">Display name
                <input value={subjectDraft.display_name} onChange={(event) => setSubjectDraft({ ...subjectDraft, display_name: event.currentTarget.value })} />
              </label>
              <label className="form-group">External reference
                <input value={subjectDraft.external_ref} onChange={(event) => setSubjectDraft({ ...subjectDraft, external_ref: event.currentTarget.value })} />
              </label>
              <button className="btn-primary" disabled={busy || !subjectDraft.display_name.trim()}>Add subject</button>
            </form>
            <div className="workforce-list">
              {subjects.map((subject) => <div key={subject.id}><strong>{subject.display_name}</strong><span>{subject.kind} · {subject.external_ref || 'no external ref'}</span></div>)}
              {subjects.length === 0 ? <p className="axis-muted">No subjects configured.</p> : null}
            </div>
          </section>

          <section className="card workforce-card">
            <div className="card-header"><h2>Routing pools</h2></div>
            <form className="workforce-form" onSubmit={(event) => {
              event.preventDefault()
              void runMutation(async () => {
                const created = await createRoutingPool(poolDraft, orgId, principalId)
                setPoolDraft({ name: '', job_role_id: '' })
                await loadBase()
                setSelectedPoolId(created.id)
              })
            }}>
              <label className="form-group">Name
                <input value={poolDraft.name} onChange={(event) => setPoolDraft({ ...poolDraft, name: event.currentTarget.value })} />
              </label>
              <label className="form-group">Job Role
                <select value={poolDraft.job_role_id} onChange={(event) => setPoolDraft({ ...poolDraft, job_role_id: event.currentTarget.value })}>
                  <option value="">Select...</option>{jobRoles.map((role) => <option key={role.id} value={role.id}>{role.name}</option>)}
                </select>
              </label>
              <button className="btn-primary" disabled={busy || !poolDraft.name.trim() || !poolDraft.job_role_id}>Add pool</button>
            </form>
            <label className="form-group">Selected pool
              <select value={selectedPoolId} onChange={(event) => setSelectedPoolId(event.currentTarget.value)}>
                <option value="">Select...</option>{pools.map((pool) => <option key={pool.id} value={pool.id}>{pool.name} · {roleById.get(pool.job_role_id) ?? pool.job_role_id}</option>)}
              </select>
            </label>
          </section>

          <section className="card workforce-card workforce-card--wide">
            <div className="card-header"><h2>Capacity {selectedPool ? `· ${selectedPool.name}` : ''}</h2></div>
            <form className="workforce-form workforce-form--inline" onSubmit={(event) => {
              event.preventDefault()
              if (!selectedPoolId) return
              void runMutation(async () => {
                await putRoutingPoolMember(selectedPoolId, memberDraft.virployee_id, { max_active_subjects: memberDraft.max_active_subjects, enabled: true }, orgId, principalId)
                setMemberDraft({ virployee_id: '', max_active_subjects: 20 })
                await loadPool()
              })
            }}>
              <label className="form-group">Virployee
                <select value={memberDraft.virployee_id} onChange={(event) => setMemberDraft({ ...memberDraft, virployee_id: event.currentTarget.value })}>
                  <option value="">Select...</option>{virployees.filter((item) => !selectedPool || item.job_role_id === selectedPool.job_role_id).map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
                </select>
              </label>
              <label className="form-group">Maximum active subjects
                <input type="number" min={1} value={memberDraft.max_active_subjects} onChange={(event) => setMemberDraft({ ...memberDraft, max_active_subjects: Number(event.currentTarget.value) })} />
              </label>
              <button className="btn-primary" disabled={busy || !selectedPoolId || !memberDraft.virployee_id || memberDraft.max_active_subjects < 1}>Save member</button>
            </form>
            <div className="workforce-list">
              {members.map((member) => (
                <form key={member.virployee_id} className="workforce-member-row" onSubmit={(event) => {
                  event.preventDefault()
                  if (!selectedPoolId) return
                  void runMutation(async () => {
                    await putRoutingPoolMember(selectedPoolId, member.virployee_id, {
                      max_active_subjects: member.max_active_subjects,
                      enabled: member.enabled,
                    }, orgId, principalId)
                    setNotice(`${virployeeById.get(member.virployee_id) ?? 'Member'} capacity updated.`)
                    await loadPool()
                  })
                }}>
                  <div className="workforce-row-identity">
                    <strong>{virployeeById.get(member.virployee_id) ?? member.virployee_id}</strong>
                    <span>{member.active_subjects} active subject{member.active_subjects === 1 ? '' : 's'}</span>
                  </div>
                  <label className="form-group">Maximum active subjects
                    <input
                      type="number"
                      min={1}
                      value={member.max_active_subjects}
                      onChange={(event) => updateMember(member.virployee_id, { max_active_subjects: Number(event.currentTarget.value) })}
                    />
                  </label>
                  <label className="workforce-member-enabled">
                    <input
                      type="checkbox"
                      checked={member.enabled}
                      onChange={(event) => updateMember(member.virployee_id, { enabled: event.currentTarget.checked })}
                    />
                    Accept new assignments
                  </label>
                  <button className="btn-secondary" disabled={busy || member.max_active_subjects < 1}>Save member changes</button>
                </form>
              ))}
              {members.length === 0 ? <p className="axis-muted">No members in this pool.</p> : null}
            </div>
          </section>

          <section className="card workforce-card workforce-card--wide">
            <div className="card-header"><h2>Stable assignments</h2></div>
            <form className="workforce-form workforce-form--inline" onSubmit={(event) => {
              event.preventDefault()
              if (!selectedPoolId || !routingSubjectId) return
              void runMutation(async () => {
                const result = await resolveVirployeeRouting(selectedPoolId, routingSubjectId, orgId, principalId)
                setNotice(result.status === 'assigned' && result.assignment
                  ? `${subjectById.get(routingSubjectId) ?? 'Subject'} is assigned to ${virployeeById.get(result.assignment.virployee_id) ?? result.assignment.virployee_id}.`
                  : `Routing result: ${result.status}.`)
                await loadPool()
              })
            }}>
              <label className="form-group">Subject
                <select value={routingSubjectId} onChange={(event) => setRoutingSubjectId(event.currentTarget.value)}>
                  <option value="">Select...</option>{subjects.map((subject) => <option key={subject.id} value={subject.id}>{subject.display_name}</option>)}
                </select>
              </label>
              <button className="btn-primary" disabled={busy || !selectedPoolId || !routingSubjectId}>Resolve stable assignment</button>
            </form>
            <div className="workforce-list">
              {assignments.map((assignment) => {
                const draft = reassignmentDrafts[assignment.id] ?? { virployee_id: '', reason: 'manual_reassignment' }
                const eligibleTargets = members.filter((member) => member.enabled && member.virployee_id !== assignment.virployee_id)
                return (
                  <div key={assignment.id} className="workforce-assignment-row">
                    <div className="workforce-row-identity">
                      <strong>{subjectById.get(assignment.subject_id) ?? assignment.subject_id}</strong>
                      <span>{virployeeById.get(assignment.virployee_id) ?? assignment.virployee_id} · version {assignment.version}</span>
                    </div>
                    <form className="workforce-reassignment-form" onSubmit={(event) => {
                      event.preventDefault()
                      if (!draft.virployee_id || !draft.reason.trim()) return
                      void runMutation(async () => {
                        const updated = await reassignContinuityAssignment(assignment.id, {
                          virployee_id: draft.virployee_id,
                          expected_version: assignment.version,
                          reason: draft.reason.trim(),
                        }, orgId, principalId)
                        setNotice(`${subjectById.get(updated.subject_id) ?? 'Subject'} reassigned to ${virployeeById.get(updated.virployee_id) ?? updated.virployee_id}.`)
                        await loadPool()
                      })
                    }}>
                      <label className="form-group">New Virployee
                        <select value={draft.virployee_id} onChange={(event) => updateReassignment(assignment.id, { virployee_id: event.currentTarget.value })}>
                          <option value="">Select...</option>
                          {eligibleTargets.map((member) => (
                            <option key={member.virployee_id} value={member.virployee_id} disabled={member.active_subjects >= member.max_active_subjects}>
                              {virployeeById.get(member.virployee_id) ?? member.virployee_id} · {member.active_subjects}/{member.max_active_subjects}
                            </option>
                          ))}
                        </select>
                      </label>
                      <label className="form-group">Reason code
                        <input
                          value={draft.reason}
                          pattern="[a-z][a-z0-9_.-]{0,63}"
                          maxLength={64}
                          onChange={(event) => updateReassignment(assignment.id, { reason: event.currentTarget.value })}
                        />
                      </label>
                      <button className="btn-secondary" disabled={busy || !draft.virployee_id || !/^[a-z][a-z0-9_.-]{0,63}$/.test(draft.reason.trim())}>Reassign subject</button>
                    </form>
                  </div>
                )
              })}
              {assignments.length === 0 ? <p className="axis-muted">No assignments in this pool.</p> : null}
            </div>
          </section>
        </div>
      )}
    </section>
  )
}

function errorMessage(cause: unknown): string {
  return cause instanceof Error ? cause.message : 'Could not update workforce configuration'
}
