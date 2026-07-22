import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import {
  archiveKnowledgeDocument,
  createKnowledgeBase,
  ingestKnowledgeConnector,
  listJobRoles,
  listKnowledgeBases,
  listKnowledgeBindings,
  listKnowledgeDocuments,
  listVirployees,
  listWorkSubjects,
  registerKnowledgeDocument,
  replaceKnowledgeBindings,
  setKnowledgeBaseArchived,
  uploadKnowledgeFile,
  type JobRole,
  type KnowledgeArtifactScope,
  type KnowledgeBase,
  type KnowledgeBindingInput,
  type KnowledgeBindingScope,
  type KnowledgeDocument,
  type KnowledgeConnectorIngestion,
  type Virployee,
  type WorkSubject,
} from './api'

type KnowledgeBasesPageProps = {
  orgId: string
  principalId: string
  productSurface: string
}

const EMPTY_BASE: Pick<KnowledgeBase, 'name' | 'description' | 'classification'> = { name: '', description: '', classification: 'private' }

export function KnowledgeBasesPage({ orgId, principalId, productSurface }: KnowledgeBasesPageProps) {
  const [bases, setBases] = useState<KnowledgeBase[]>([])
  const [documents, setDocuments] = useState<KnowledgeDocument[]>([])
  const [bindings, setBindings] = useState<KnowledgeBindingInput[]>([])
  const [roles, setRoles] = useState<JobRole[]>([])
  const [virployees, setVirployees] = useState<Virployee[]>([])
  const [subjects, setSubjects] = useState<WorkSubject[]>([])
  const [selectedBaseId, setSelectedBaseId] = useState('')
  const [baseDraft, setBaseDraft] = useState(EMPTY_BASE)
  const [documentDraft, setDocumentDraft] = useState<{ title: string; artifact_scope: KnowledgeArtifactScope }>({
    title: '',
    artifact_scope: { virployee_id: '', product_surface: productSurface, subject_id: '', repository_generation: 'current', document_id: '' },
  })
  const [uploadDraft, setUploadDraft] = useState({ title: '', virployee_id: '', subject_id: '', document_id: '' })
  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const [uploadInputKey, setUploadInputKey] = useState(0)
  const [connectorDraft, setConnectorDraft] = useState<KnowledgeConnectorIngestion>({
    title: '',
    target: { virployee_id: '', subject_id: '', document_id: '' },
    source: { connector: '', external_id: '', name: '', read_url: '', sha256: '', mime_type: '', size_bytes: 0 },
  })
  const [bindingDraft, setBindingDraft] = useState<KnowledgeBindingInput>({ scope_type: 'professional' })
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const loadBase = useCallback(async () => {
    if (!orgId || !principalId) return
    setLoading(true)
    setError('')
    try {
      const [active, archived, nextRoles, nextVirployees, nextSubjects] = await Promise.all([
        listKnowledgeBases(orgId, principalId, 'active'),
        listKnowledgeBases(orgId, principalId, 'archived'),
        listJobRoles('active', orgId, principalId),
        listVirployees('active', orgId, principalId),
        listWorkSubjects(orgId, principalId),
      ])
      const nextBases = [...active, ...archived]
      setBases(nextBases)
      setRoles(nextRoles)
      setVirployees(nextVirployees)
      setSubjects(nextSubjects)
      setSelectedBaseId((current) => current && nextBases.some((base) => base.id === current) ? current : nextBases[0]?.id ?? '')
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setLoading(false)
    }
  }, [principalId, orgId])

  const loadSelected = useCallback(async () => {
    if (!selectedBaseId) {
      setDocuments([])
      setBindings([])
      return
    }
    try {
      const [nextDocuments, nextBindings] = await Promise.all([
        listKnowledgeDocuments(selectedBaseId, orgId, principalId),
        listKnowledgeBindings(selectedBaseId, orgId, principalId),
      ])
      setDocuments(nextDocuments)
      const normalizedBindings = nextBindings.map(({ scope_type, job_role_id, virployee_id, subject_id, case_id }) => ({
        scope_type, job_role_id, virployee_id, subject_id, case_id,
      }))
      const classification = bases.find((base) => base.id === selectedBaseId)?.classification
      setBindings(classification === 'private' ? normalizedBindings.slice(0, 1) : normalizedBindings)
    } catch (cause) {
      setError(errorMessage(cause))
    }
  }, [bases, principalId, selectedBaseId, orgId])

  useEffect(() => { void loadBase() }, [loadBase])
  useEffect(() => { void loadSelected() }, [loadSelected])
  useEffect(() => {
    setDocumentDraft((current) => ({ ...current, artifact_scope: { ...current.artifact_scope, product_surface: productSurface } }))
  }, [productSurface])

  const selectedBase = bases.find((base) => base.id === selectedBaseId)

  useEffect(() => {
    if (!selectedBase) return
    setDocumentDraft((current) => ({
      ...current,
      artifact_scope: { ...current.artifact_scope, subject_id: selectedBase.classification === 'professional' ? 'professional' : '' },
    }))
    setBindingDraft({ scope_type: selectedBase.classification === 'professional' ? 'professional' : 'subject' })
	const subjectID = selectedBase.classification === 'professional' ? 'professional' : ''
	setUploadDraft((current) => ({ ...current, subject_id: subjectID }))
	setConnectorDraft((current) => ({ ...current, target: { ...current.target, subject_id: subjectID } }))
  }, [selectedBase?.classification, selectedBase?.id])
  const roleById = useMemo(() => new Map(roles.map((role) => [role.id, role.name])), [roles])
  const virployeeById = useMemo(() => new Map(virployees.map((item) => [item.id, item.name])), [virployees])
  const subjectById = useMemo(() => new Map(subjects.map((subject) => [subject.id, subject.display_name])), [subjects])

  async function mutate(operation: () => Promise<void>) {
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

  function addBinding(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!bindingIsComplete(bindingDraft)) return
    setBindings((current) => selectedBase?.classification === 'private' ? [bindingDraft] : [...current, bindingDraft])
    setBindingDraft({ scope_type: bindingDraft.scope_type })
  }

  return (
    <section className="page-section knowledge-bases-page">
      <div className="card workforce-summary">
        <div className="card-header"><h2>Grounded knowledge</h2></div>
        <p>Only authorized professional, Virployee, subject and case libraries are retrieved during Assist.</p>
        <p className="axis-muted">Direct uploads, connectors and pre-indexed artifacts all pass through the same organization and patient isolation contract.</p>
      </div>
      {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}
      {notice ? <p role="status" className="iam-control__inline-note">{notice}</p> : null}
      {loading ? <div className="spinner" /> : (
        <div className="workforce-grid">
          <section className="card workforce-card">
            <div className="card-header"><h2>Knowledge Bases</h2></div>
            <form className="workforce-form" onSubmit={(event) => {
              event.preventDefault()
              void mutate(async () => {
                const created = await createKnowledgeBase(baseDraft, orgId, principalId)
                setBaseDraft(EMPTY_BASE)
                await loadBase()
                setSelectedBaseId(created.id)
              })
            }}>
              <label className="form-group">Name
                <input value={baseDraft.name} maxLength={200} onChange={(event) => setBaseDraft({ ...baseDraft, name: event.currentTarget.value })} />
              </label>
              <label className="form-group">Description
                <textarea rows={3} value={baseDraft.description} maxLength={2000} onChange={(event) => setBaseDraft({ ...baseDraft, description: event.currentTarget.value })} />
              </label>
              <label className="form-group">Classification
                <select value={baseDraft.classification} onChange={(event) => setBaseDraft({ ...baseDraft, classification: event.currentTarget.value as KnowledgeBase['classification'] })}>
                  <option value="private">Private patient / case</option>
                  <option value="professional">Professional shared</option>
                </select>
              </label>
              <button className="btn-primary" disabled={busy || !baseDraft.name.trim()}>Create Knowledge Base</button>
            </form>
            <label className="form-group">Selected Knowledge Base
              <select value={selectedBaseId} onChange={(event) => setSelectedBaseId(event.currentTarget.value)}>
                <option value="">Select...</option>
                {bases.map((base) => <option key={base.id} value={base.id}>{base.name} · {base.state}</option>)}
              </select>
            </label>
            {selectedBase ? (
              <div className="knowledge-base-summary">
                <span>{selectedBase.classification} · {selectedBase.description || 'No description'}</span>
                <button type="button" className="btn-secondary" disabled={busy} onClick={() => void mutate(async () => {
                  const next = await setKnowledgeBaseArchived(selectedBase, selectedBase.state === 'active', orgId, principalId)
                  setNotice(`${next.name} is now ${next.state}.`)
                  await loadBase()
                })}>{selectedBase.state === 'active' ? 'Archive' : 'Activate'}</button>
              </div>
            ) : null}
          </section>

          <section className="card workforce-card">
            <div className="card-header"><h2>Upload document</h2></div>
            <form className="workforce-form" onSubmit={(event) => {
              event.preventDefault()
              if (!selectedBase || !uploadFile) return
              void mutate(async () => {
                await uploadKnowledgeFile(selectedBase.id, {
                  title: uploadDraft.title,
                  target: { virployee_id: uploadDraft.virployee_id, subject_id: uploadDraft.subject_id, document_id: uploadDraft.document_id },
                  file: uploadFile,
                }, orgId, principalId)
                setUploadDraft((current) => ({ ...current, title: '', document_id: '' }))
                setUploadFile(null)
                setUploadInputKey((current) => current + 1)
                setNotice('File scanned, indexed and registered with immutable provenance.')
                await loadBase()
                await loadSelected()
              })
            }}>
              <label className="form-group">Title
                <input value={uploadDraft.title} maxLength={300} placeholder="Defaults to file name" onChange={(event) => setUploadDraft({ ...uploadDraft, title: event.currentTarget.value })} />
              </label>
              <label className="form-group">Virployee
                <select value={uploadDraft.virployee_id} onChange={(event) => setUploadDraft({ ...uploadDraft, virployee_id: event.currentTarget.value })}>
                  <option value="">Select...</option>{virployees.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
                </select>
              </label>
              {selectedBase?.classification === 'professional' ? (
                <label className="form-group">Scope<input value="professional" readOnly /></label>
              ) : (
                <label className="form-group">Patient
                  <select value={uploadDraft.subject_id} onChange={(event) => setUploadDraft({ ...uploadDraft, subject_id: event.currentTarget.value })}>
                    <option value="">Select...</option>{subjects.map((subject) => <option key={subject.id} value={subject.id}>{subject.display_name}</option>)}
                  </select>
                </label>
              )}
              <label className="form-group">Stable document ID (optional)
                <input value={uploadDraft.document_id} pattern="[A-Za-z0-9][A-Za-z0-9._-]*" onChange={(event) => setUploadDraft({ ...uploadDraft, document_id: event.currentTarget.value })} />
              </label>
              <label className="form-group">File
                <input key={uploadInputKey} type="file" onChange={(event) => setUploadFile(event.currentTarget.files?.[0] ?? null)} />
              </label>
              <button className="btn-primary" disabled={busy || !selectedBase || !uploadFile || !uploadDraft.virployee_id || !uploadDraft.subject_id}>Upload and index</button>
            </form>
          </section>

          <section className="card workforce-card">
            <div className="card-header"><h2>Connector ingestion</h2></div>
            <form className="workforce-form" onSubmit={(event) => {
              event.preventDefault()
              if (!selectedBase) return
              void mutate(async () => {
                await ingestKnowledgeConnector(selectedBase.id, connectorDraft, orgId, principalId)
                setConnectorDraft((current) => ({
                  ...current,
                  title: '',
                  target: { ...current.target, document_id: '' },
                  source: { connector: '', external_id: '', name: '', read_url: '', sha256: '', mime_type: '', size_bytes: 0 },
                }))
                setNotice('Connector source fetched through the verified ingestion pipeline.')
                await loadBase()
                await loadSelected()
              })
            }}>
              <label className="form-group">Connector
                <input value={connectorDraft.source.connector} placeholder="google_drive" onChange={(event) => setConnectorDraft({ ...connectorDraft, source: { ...connectorDraft.source, connector: event.currentTarget.value } })} />
              </label>
              <label className="form-group">External ID
                <input value={connectorDraft.source.external_id} onChange={(event) => setConnectorDraft({ ...connectorDraft, source: { ...connectorDraft.source, external_id: event.currentTarget.value } })} />
              </label>
              <label className="form-group">Virployee
                <select value={connectorDraft.target.virployee_id} onChange={(event) => setConnectorDraft({ ...connectorDraft, target: { ...connectorDraft.target, virployee_id: event.currentTarget.value } })}>
                  <option value="">Select...</option>{virployees.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
                </select>
              </label>
              {selectedBase?.classification === 'professional' ? null : (
                <label className="form-group">Patient
                  <select value={connectorDraft.target.subject_id} onChange={(event) => setConnectorDraft({ ...connectorDraft, target: { ...connectorDraft.target, subject_id: event.currentTarget.value } })}>
                    <option value="">Select...</option>{subjects.map((subject) => <option key={subject.id} value={subject.id}>{subject.display_name}</option>)}
                  </select>
                </label>
              )}
              <label className="form-group">Document ID
                <input value={connectorDraft.target.document_id} onChange={(event) => setConnectorDraft({ ...connectorDraft, target: { ...connectorDraft.target, document_id: event.currentTarget.value } })} />
              </label>
              <label className="form-group">File name
                <input value={connectorDraft.source.name} onChange={(event) => setConnectorDraft({ ...connectorDraft, source: { ...connectorDraft.source, name: event.currentTarget.value } })} />
              </label>
              <label className="form-group">Authorized read URL
                <input type="url" value={connectorDraft.source.read_url} onChange={(event) => setConnectorDraft({ ...connectorDraft, source: { ...connectorDraft.source, read_url: event.currentTarget.value } })} />
              </label>
              <label className="form-group">SHA-256
                <input value={connectorDraft.source.sha256} minLength={64} maxLength={64} onChange={(event) => setConnectorDraft({ ...connectorDraft, source: { ...connectorDraft.source, sha256: event.currentTarget.value } })} />
              </label>
              <label className="form-group">MIME type
                <input value={connectorDraft.source.mime_type} placeholder="application/pdf" onChange={(event) => setConnectorDraft({ ...connectorDraft, source: { ...connectorDraft.source, mime_type: event.currentTarget.value } })} />
              </label>
              <label className="form-group">Size (bytes)
                <input type="number" min={1} value={connectorDraft.source.size_bytes || ''} onChange={(event) => setConnectorDraft({ ...connectorDraft, source: { ...connectorDraft.source, size_bytes: Number(event.currentTarget.value) } })} />
              </label>
              <button className="btn-primary" disabled={busy || !selectedBase || !connectorDraft.target.virployee_id || !connectorDraft.target.subject_id || !connectorDraft.target.document_id || !connectorDraft.source.connector || !connectorDraft.source.external_id || !connectorDraft.source.name || !connectorDraft.source.read_url || connectorDraft.source.sha256.length !== 64 || !connectorDraft.source.mime_type || connectorDraft.source.size_bytes <= 0}>Fetch, scan and index</button>
            </form>
          </section>

          <section className="card workforce-card">
            <div className="card-header"><h2>Register indexed artifact (advanced)</h2></div>
            <form className="workforce-form" onSubmit={(event) => {
              event.preventDefault()
              if (!selectedBase) return
              void mutate(async () => {
                await registerKnowledgeDocument(selectedBase.id, documentDraft, orgId, principalId)
                setDocumentDraft({ title: '', artifact_scope: { virployee_id: '', product_surface: productSurface, subject_id: selectedBase.classification === 'professional' ? 'professional' : '', repository_generation: 'current', document_id: '' } })
                setNotice('Document registered with its immutable source hash and version.')
				await loadBase()
                await loadSelected()
              })
            }}>
              <label className="form-group">Title
                <input value={documentDraft.title} maxLength={300} onChange={(event) => setDocumentDraft({ ...documentDraft, title: event.currentTarget.value })} />
              </label>
              <label className="form-group">Artifact owner Virployee
                <select value={documentDraft.artifact_scope.virployee_id} onChange={(event) => setDocumentDraft(withArtifact(documentDraft, 'virployee_id', event.currentTarget.value))}>
                  <option value="">Select...</option>{virployees.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
                </select>
              </label>
              {selectedBase?.classification === 'professional' ? (
                <label className="form-group">Artifact subject
                  <input value="professional" readOnly aria-describedby="professional-subject-help" />
                  <small id="professional-subject-help" className="axis-muted">Non-personal source namespace.</small>
                </label>
              ) : (
                <label className="form-group">Artifact subject
                  <select value={documentDraft.artifact_scope.subject_id} onChange={(event) => setDocumentDraft(withArtifact(documentDraft, 'subject_id', event.currentTarget.value))}>
                    <option value="">Select...</option>{subjects.map((subject) => <option key={subject.id} value={subject.id}>{subject.display_name}</option>)}
                  </select>
                </label>
              )}
              <label className="form-group">Repository generation
                <input value={documentDraft.artifact_scope.repository_generation} onChange={(event) => setDocumentDraft(withArtifact(documentDraft, 'repository_generation', event.currentTarget.value))} />
              </label>
              <label className="form-group">Artifact document ID
                <input value={documentDraft.artifact_scope.document_id} onChange={(event) => setDocumentDraft(withArtifact(documentDraft, 'document_id', event.currentTarget.value))} />
              </label>
              <button className="btn-primary" disabled={busy || !selectedBase || !documentIsComplete(documentDraft)}>Register document</button>
            </form>
          </section>

          <section className="card workforce-card workforce-card--wide">
            <div className="card-header"><h2>Authorized bindings</h2></div>
            {selectedBase?.classification === 'private' ? (
              <p className="axis-muted">A private Knowledge Base authorizes exactly one patient or case. Setting a new binding replaces the current one.</p>
            ) : null}
            <form className="knowledge-binding-form" onSubmit={addBinding}>
              <label className="form-group">Scope
                <select value={bindingDraft.scope_type} onChange={(event) => setBindingDraft({ scope_type: event.currentTarget.value as KnowledgeBindingScope })}>
                  {selectedBase?.classification === 'professional' ? <><option value="professional">Professional</option><option value="virployee">Virployee</option></> : null}
                  {selectedBase?.classification === 'private' ? <><option value="subject">Subject</option><option value="case">Case</option></> : null}
                </select>
              </label>
              {bindingDraft.scope_type === 'professional' ? (
                <label className="form-group">Job Role
                  <select value={bindingDraft.job_role_id ?? ''} onChange={(event) => setBindingDraft({ ...bindingDraft, job_role_id: event.currentTarget.value })}>
                    <option value="">Select...</option>{roles.map((role) => <option key={role.id} value={role.id}>{role.name}</option>)}
                  </select>
                </label>
              ) : (
                <label className="form-group">Virployee
                  <select value={bindingDraft.virployee_id ?? ''} onChange={(event) => setBindingDraft({ ...bindingDraft, virployee_id: event.currentTarget.value })}>
                    <option value="">Select...</option>{virployees.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
                  </select>
                </label>
              )}
              {bindingDraft.scope_type === 'subject' || bindingDraft.scope_type === 'case' ? (
                <label className="form-group">Subject
                  <select value={bindingDraft.subject_id ?? ''} onChange={(event) => setBindingDraft({ ...bindingDraft, subject_id: event.currentTarget.value })}>
                    <option value="">Select...</option>{subjects.map((subject) => <option key={subject.id} value={subject.id}>{subject.display_name}</option>)}
                  </select>
                </label>
              ) : null}
              {bindingDraft.scope_type === 'case' ? (
                <label className="form-group">Case ID
                  <input value={bindingDraft.case_id ?? ''} placeholder="UUID" onChange={(event) => setBindingDraft({ ...bindingDraft, case_id: event.currentTarget.value })} />
                </label>
              ) : null}
              <button className="btn-secondary" disabled={!bindingIsComplete(bindingDraft)}>{selectedBase?.classification === 'private' ? 'Set private binding' : 'Add binding'}</button>
            </form>
            <div className="workforce-list knowledge-binding-list">
              {bindings.map((binding, index) => (
                <div key={`${binding.scope_type}-${binding.job_role_id ?? binding.virployee_id}-${binding.subject_id ?? ''}-${binding.case_id ?? ''}-${index}`}>
                  <strong>{bindingLabel(binding, roleById, virployeeById, subjectById)}</strong>
                  {selectedBase?.classification === 'private' ? <span>Exclusive private scope</span> : (
                    <button type="button" className="btn-sm btn-danger" onClick={() => setBindings((current) => current.filter((_, candidate) => candidate !== index))}>Remove</button>
                  )}
                </div>
              ))}
              {bindings.length === 0 ? <p className="axis-muted">{selectedBase?.classification === 'private' ? 'Set one patient or case before saving.' : 'No scope is authorized yet.'}</p> : null}
            </div>
            <button type="button" className="btn-primary knowledge-save-bindings" disabled={busy || !selectedBase || (selectedBase.classification === 'private' && bindings.length !== 1)} onClick={() => void mutate(async () => {
              if (!selectedBase) return
              const bindingsToSave = selectedBase.classification === 'private' ? bindings.slice(0, 1) : bindings
              await replaceKnowledgeBindings(selectedBase, bindingsToSave, orgId, principalId)
              setNotice('Knowledge bindings saved. Prior approvals using the old context are now stale.')
              await loadBase()
              await loadSelected()
            })}>Save bindings</button>
          </section>

          <section className="card workforce-card workforce-card--wide">
            <div className="card-header"><h2>Registered documents</h2></div>
            <div className="workforce-list">
              {documents.map((document) => (
                <div key={document.id}>
                  <strong>{document.title || document.artifact_scope.document_id}</strong>
                  <span>{subjectById.get(document.artifact_scope.subject_id) ?? document.artifact_scope.subject_id} · {shortHash(document.source_sha256)}</span>
                  <button type="button" className="btn-sm btn-danger" disabled={busy} onClick={() => void mutate(async () => {
                    if (!selectedBase) return
                    await archiveKnowledgeDocument(selectedBase.id, document, orgId, principalId)
					await loadBase()
                    await loadSelected()
                  })}>Archive</button>
                </div>
              ))}
              {documents.length === 0 ? <p className="axis-muted">No active documents in this Knowledge Base.</p> : null}
            </div>
          </section>
        </div>
      )}
    </section>
  )
}

function withArtifact(
  draft: { title: string; artifact_scope: KnowledgeArtifactScope },
  key: keyof KnowledgeArtifactScope,
  value: string,
) {
  return { ...draft, artifact_scope: { ...draft.artifact_scope, [key]: value } }
}

function documentIsComplete(draft: { title: string; artifact_scope: KnowledgeArtifactScope }): boolean {
  return Boolean(draft.artifact_scope.virployee_id && draft.artifact_scope.product_surface.trim() && draft.artifact_scope.subject_id.trim() && draft.artifact_scope.repository_generation.trim() && draft.artifact_scope.document_id.trim())
}

function bindingIsComplete(binding: KnowledgeBindingInput): boolean {
  if (binding.scope_type === 'professional') return Boolean(binding.job_role_id)
  if (binding.scope_type === 'virployee') return Boolean(binding.virployee_id)
  if (binding.scope_type === 'subject') return Boolean(binding.virployee_id && binding.subject_id)
  return Boolean(binding.virployee_id && binding.subject_id && /^[0-9a-f-]{36}$/i.test(binding.case_id ?? ''))
}

function bindingLabel(
  binding: KnowledgeBindingInput,
  roles: Map<string, string>,
  virployees: Map<string, string>,
  subjects: Map<string, string>,
): string {
  if (binding.scope_type === 'professional') return `Professional · ${roles.get(binding.job_role_id ?? '') ?? binding.job_role_id}`
  const virployee = virployees.get(binding.virployee_id ?? '') ?? binding.virployee_id
  if (binding.scope_type === 'virployee') return `Virployee · ${virployee}`
  const subject = subjects.get(binding.subject_id ?? '') ?? binding.subject_id
  if (binding.scope_type === 'subject') return `Subject · ${subject} · ${virployee}`
  return `Case · ${subject} · ${binding.case_id} · ${virployee}`
}

function shortHash(value: string): string { return value ? `${value.slice(0, 12)}…` : 'pending hash' }
function errorMessage(cause: unknown): string { return cause instanceof Error ? cause.message : 'Could not update knowledge configuration' }
