import { useCallback, useEffect, useState, type FormEvent } from 'react'
import {
  createVirployeeMemory,
  lifecycleVirployeeMemory,
  listVirployeeMemories,
  recallVirployeeMemories,
  type MemoryInput,
  type MemoryReference,
  type Virployee,
  type VirployeeMemory,
} from './api'
import { formatDateTime24 } from './formatters'

type MemoryView = 'active' | 'archived' | 'trash'
type MemoryAction = 'archive' | 'unarchive' | 'trash' | 'restore'

const EMPTY_FORM: MemoryInput = {
  title: '',
  type: 'note',
  content: '',
  sensitivity: 'normal',
}

export function VirployeeMemoryPanel(props: {
  row: Virployee
  tenantId: string
  principalId: string
  onClose: () => void
}) {
  const [view, setView] = useState<MemoryView>('active')
  const [query, setQuery] = useState('')
  const [appliedQuery, setAppliedQuery] = useState('')
  const [items, setItems] = useState<VirployeeMemory[]>([])
  const [nextCursor, setNextCursor] = useState('')
  const [form, setForm] = useState<MemoryInput>(EMPTY_FORM)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [busyID, setBusyID] = useState('')
  const [error, setError] = useState('')
  const [recallItems, setRecallItems] = useState<MemoryReference[]>([])
  const [recallHash, setRecallHash] = useState('')
  const [recallLoading, setRecallLoading] = useState(false)

  const load = useCallback(async (cursor = '', append = false, search = appliedQuery) => {
    setLoading(true)
    setError('')
    try {
      const result = await listVirployeeMemories(
        props.row.id,
        view,
        search,
        cursor,
        props.tenantId,
        props.principalId,
      )
      setItems((current) => append ? [...current, ...result.items] : result.items)
      setNextCursor(result.next_cursor ?? '')
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setLoading(false)
    }
  }, [appliedQuery, props.principalId, props.row.id, props.tenantId, view])

  useEffect(() => {
    setItems([])
    setNextCursor('')
    setRecallItems([])
    setRecallHash('')
    void load('', false)
  }, [load, view])

  async function submitCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!form.title.trim() || !form.content.trim()) return
    setSaving(true)
    setError('')
    try {
      await createVirployeeMemory(props.row.id, form, props.tenantId, props.principalId)
      setForm(EMPTY_FORM)
      if (view !== 'active') setView('active')
      else await load('', false)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setSaving(false)
    }
  }

  async function applySearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const normalized = query.trim()
    setAppliedQuery(normalized)
    await load('', false, normalized)
  }

  async function testRecall() {
    const normalized = query.trim()
    if (!normalized) return
    setRecallLoading(true)
    setError('')
    try {
      const result = await recallVirployeeMemories(
        props.row.id,
        normalized,
        props.tenantId,
        props.principalId,
      )
      setRecallItems(result.items)
      setRecallHash(result.memory_context_hash)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setRecallLoading(false)
    }
  }

  async function applyAction(item: VirployeeMemory, action: MemoryAction) {
    setBusyID(item.id)
    setError('')
    try {
      await lifecycleVirployeeMemory(
        props.row.id,
        item.id,
        action,
        props.tenantId,
        props.principalId,
      )
      await load('', false)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusyID('')
    }
  }

  return (
    <div className="card crud-form-card virployee-memory-inline">
      <div className="card-header">
        <h2>Memory</h2>
      </div>
      <div className="virployee-panel-actions virployee-panel-actions--top">
        <button type="button" className="btn-secondary" onClick={props.onClose}>Close</button>
      </div>

      <div className="virployee-memory">
        <header className="virployee-memory__intro">
          <div>
            <span>Virployee memory</span>
            <strong>{props.row.name}</strong>
          </div>
          <div className="virployee-memory__views" role="group" aria-label="Memory view">
            {(['active', 'archived', 'trash'] as MemoryView[]).map((candidate) => (
              <button
                key={candidate}
                type="button"
                className={`btn-sm ${view === candidate ? 'btn-primary' : 'btn-secondary'}`}
                aria-pressed={view === candidate}
                onClick={() => setView(candidate)}
              >
                {viewLabel(candidate)}
              </button>
            ))}
          </div>
        </header>

        {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}

        <section className="virployee-memory__section" aria-labelledby="memory-create-title">
          <h3 id="memory-create-title">Add memory</h3>
          <form className="virployee-memory__form" onSubmit={(event) => void submitCreate(event)}>
            <label className="form-group">
              Title
              <input value={form.title} maxLength={200} onChange={(event) => setForm({...form, title: event.currentTarget.value})} />
            </label>
            <label className="form-group">
              Type
              <select value={form.type} onChange={(event) => setForm({...form, type: event.currentTarget.value as MemoryInput['type']})}>
                <option value="fact">Fact</option>
                <option value="preference">Preference</option>
                <option value="procedure">Procedure</option>
                <option value="note">Note</option>
              </select>
            </label>
            <label className="form-group virployee-memory__content-field">
              Content
              <textarea rows={4} value={form.content} maxLength={20000} onChange={(event) => setForm({...form, content: event.currentTarget.value})} />
            </label>
            <label className="virployee-memory__sensitive-field">
              <input type="checkbox" checked={form.sensitivity === 'sensitive'} onChange={(event) => setForm({...form, sensitivity: event.currentTarget.checked ? 'sensitive' : 'normal'})} />
              <span><strong>Sensitive</strong><small>Hide content from lists, previews, logs and traces.</small></span>
            </label>
            <div className="virployee-memory__form-actions">
              <button type="submit" className="btn-primary" disabled={saving || !form.title.trim() || !form.content.trim()}>
                {saving ? 'Saving...' : 'Add memory'}
              </button>
            </div>
          </form>
        </section>

        <section className="virployee-memory__section" aria-labelledby="memory-list-title">
          <div className="virployee-memory__section-heading">
            <h3 id="memory-list-title">{viewLabel(view)} memories</h3>
            <span>{items.length} shown</span>
          </div>
          <form className="virployee-memory__search" onSubmit={(event) => void applySearch(event)}>
            <label className="form-group">
              Search and recall query
              <input placeholder="Search memories" value={query} onChange={(event) => setQuery(event.currentTarget.value)} />
            </label>
            <div className="virployee-memory__search-actions">
              <button type="submit" className="btn-secondary" disabled={loading}>Search</button>
              <button type="button" className="btn-secondary" disabled={recallLoading || !query.trim()} onClick={() => void testRecall()}>
                {recallLoading ? 'Testing...' : 'Test recall'}
              </button>
            </div>
          </form>

          {recallItems.length > 0 ? (
            <div className="virployee-memory__recall" aria-label="Recall results">
              <div><strong>Recall order</strong><code title={recallHash}>{shortHash(recallHash)}</code></div>
              <ol>{recallItems.map((item) => <li key={item.id}><span>{item.title}</span><small>{typeLabel(item.type)} · score {item.score.toFixed(3)}</small></li>)}</ol>
            </div>
          ) : null}

          <div className="virployee-memory__table-wrap">
            <table className="virployee-memory__table">
              <thead><tr><th>Title</th><th>Type</th><th>Sensitivity</th><th>Provenance</th><th>Version</th><th>Updated</th><th><span className="sr-only">Actions</span></th></tr></thead>
              <tbody>
                {items.map((item) => (
                  <tr key={item.id}>
                    <td><strong>{item.title}</strong>{item.preview ? <small>{item.preview}</small> : null}</td>
                    <td>{typeLabel(item.type)}</td>
                    <td><span className={`virployee-memory__badge virployee-memory__badge--${item.sensitivity}`}>{item.sensitivity === 'sensitive' ? 'Sensitive' : 'Normal'}</span></td>
                    <td><span className={`virployee-memory__badge virployee-memory__badge--${item.provenance === 'system' ? 'system' : 'human'}`}>{item.provenance === 'system' ? 'Learned' : 'Human'}</span></td>
                    <td>v{item.version}</td>
                    <td>{formatDateTime24(item.updated_at)}</td>
                    <td><MemoryRowActions view={view} item={item} busy={busyID === item.id} onAction={applyAction} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
            {!loading && items.length === 0 ? <p className="virployee-memory__empty">No {viewLabel(view).toLowerCase()} memories found.</p> : null}
            {loading ? <p className="virployee-memory__empty">Loading memories...</p> : null}
          </div>
          {nextCursor ? <button type="button" className="btn-secondary virployee-memory__load-more" disabled={loading} onClick={() => void load(nextCursor, true)}>Load more</button> : null}
        </section>
      </div>

      <footer className="virployee-panel-footer">
        <button type="button" className="btn-secondary" onClick={props.onClose}>Close</button>
      </footer>
    </div>
  )
}

function MemoryRowActions(props: {view: MemoryView; item: VirployeeMemory; busy: boolean; onAction: (item: VirployeeMemory, action: MemoryAction) => Promise<void>}) {
  if (props.view === 'active') return <div className="virployee-memory__row-actions"><button type="button" className="btn-sm btn-secondary" disabled={props.busy} onClick={() => void props.onAction(props.item, 'archive')}>Archive</button><button type="button" className="btn-sm btn-danger" disabled={props.busy} onClick={() => void props.onAction(props.item, 'trash')}>Trash</button></div>
  if (props.view === 'archived') return <button type="button" className="btn-sm btn-primary" disabled={props.busy} onClick={() => void props.onAction(props.item, 'unarchive')}>Unarchive</button>
  return <button type="button" className="btn-sm btn-primary" disabled={props.busy} onClick={() => void props.onAction(props.item, 'restore')}>Restore</button>
}

function viewLabel(view: MemoryView) { return view === 'active' ? 'Active' : view === 'archived' ? 'Archived' : 'Trash' }
function typeLabel(type: MemoryReference['type']) { return type.charAt(0).toUpperCase() + type.slice(1) }
function shortHash(value: string) { return value ? `${value.slice(0, 10)}…` : '' }
function errorMessage(cause: unknown) { return cause instanceof Error ? cause.message : 'Memory request failed.' }
