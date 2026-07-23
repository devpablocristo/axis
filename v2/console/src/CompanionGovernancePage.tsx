import { Beaker, Eye, Play, RefreshCw, ScrollText } from 'lucide-react'
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { axisFetch } from './api'
import { formatDateTime24 } from './formatters'

export type CompanionGovernanceTab = 'prompts' | 'watchers' | 'evaluations'

type Props = { orgId: string; principalId: string; productId: string; initialTab: CompanionGovernanceTab }
type Prompt = { id: string; name: string; description: string; created_by: string; created_at: string }
type Watcher = { id: string; product_id: string; name: string; lifecycle: string; mode: string; active_version_id?: string; updated_at: string }
type Suite = { id: string; name: string; description: string; artifact_type: string; created_at: string }
type Run = { id: string; artifact_type: string; artifact_ref: string; status: string; passed: boolean; report_hash: string; started_at: string }

export function CompanionGovernancePage({ orgId, principalId, productId, initialTab }: Props) {
  const [tab, setTab] = useState(initialTab)
  const [prompts, setPrompts] = useState<Prompt[]>([])
  const [watchers, setWatchers] = useState<Watcher[]>([])
  const [suites, setSuites] = useState<Suite[]>([])
  const [runs, setRuns] = useState<Run[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [promptDraft, setPromptDraft] = useState({ name: '', description: '', content: '' })
  const [watcherDraft, setWatcherDraft] = useState({ name: '', mode: 'propose' })
  const [suiteDraft, setSuiteDraft] = useState({ name: '', artifact_type: 'prompt_version' })

  const api = useCallback(<T,>(path: string, method = 'GET', body?: unknown) => axisFetch<T>(path, { method, body, orgId, principalId }), [orgId, principalId])
  const load = useCallback(async () => {
    setLoading(true); setError('')
    try {
      const [promptData, watcherData, suiteData, runData] = await Promise.all([
        api<{ items: Prompt[] }>('/api/prompts'),
        api<{ items: Watcher[] }>('/api/watchers'),
        api<{ items: Suite[] }>('/api/evaluation-suites'),
        api<{ items: Run[] }>('/api/evaluation-runs'),
      ])
      setPrompts(promptData.items ?? []); setWatchers(watcherData.items ?? []); setSuites(suiteData.items ?? []); setRuns(runData.items ?? [])
    } catch (cause) { setError(message(cause, 'Could not load Companion governance')) } finally { setLoading(false) }
  }, [api])

  useEffect(() => { setTab(initialTab) }, [initialTab])
  useEffect(() => { void load() }, [load])

  const submit = async (action: () => Promise<void>) => {
    if (busy) return
    setBusy(true); setError('')
    try { await action(); await load() } catch (cause) { setError(message(cause, 'The change could not be saved')) } finally { setBusy(false) }
  }

  return <section className="companion-governance">
    <header className="domain-banner domain-banner--companion">
      <div><ScrollText aria-hidden="true" /><span><strong>Companion behavior control</strong><small>Content stays here. Nexus receives only hashes and promotion evidence.</small></span></div>
      <button className="btn-secondary" onClick={() => void load()} disabled={loading}><RefreshCw aria-hidden="true" />Refresh</button>
    </header>
    <nav className="operations-tabs" aria-label="Companion governance sections">
      {([['prompts','Prompts'],['watchers','Watchers'],['evaluations','Evaluations']] as Array<[CompanionGovernanceTab,string]>).map(([key,label])=><button key={key} className={tab===key?'active':''} onClick={()=>setTab(key)}>{label}</button>)}
    </nav>
    {error ? <p className="iam-control__inline-error" role="alert">{error}</p> : null}

    {tab === 'prompts' ? <div className="operations-grid">
      <article className="card operations-card"><Heading icon={<ScrollText/>} title="New prompt asset" note="Versions are immutable. Promotion requires a fresh synthetic evaluation and an independent Nexus authorization."/>
        <form className="operations-form" onSubmit={event=>{event.preventDefault();void submit(async()=>{const prompt=await api<Prompt>('/api/prompts','POST',{name:promptDraft.name,description:promptDraft.description});await api(`/api/prompts/${prompt.id}/versions`,'POST',{content:promptDraft.content});setPromptDraft({name:'',description:'',content:''})})}}>
          <label className="form-group">Name<input required value={promptDraft.name} onChange={event=>setPromptDraft({...promptDraft,name:event.currentTarget.value})}/></label>
          <label className="form-group">Description<input value={promptDraft.description} onChange={event=>setPromptDraft({...promptDraft,description:event.currentTarget.value})}/></label>
          <label className="form-group">Initial content<textarea required rows={7} value={promptDraft.content} onChange={event=>setPromptDraft({...promptDraft,content:event.currentTarget.value})}/></label>
          <button className="btn-primary" disabled={busy}>Create immutable draft</button>
        </form>
      </article>
      <article className="card operations-card operations-card--wide"><Heading title="Prompt assets" note="Resolution order: Axis base → Job Role → Profile Template → Virployee, with product bindings taking precedence."/><Table headers={['Prompt','Description','Creator','Created']} empty="No prompt assets yet.">{prompts.map(item=><tr key={item.id}><td><strong>{item.name}</strong><code>{short(item.id)}</code></td><td>{item.description||'—'}</td><td>{item.created_by}</td><td>{formatDateTime24(item.created_at)}</td></tr>)}</Table></article>
    </div> : null}

    {tab === 'watchers' ? <div className="operations-grid">
      <article className="card operations-card"><Heading icon={<Eye/>} title="New watcher" note="New watchers begin paused. A detector must be an active, conformant read capability."/>
        <form className="operations-form" onSubmit={event=>{event.preventDefault();void submit(async()=>{await api('/api/watchers','POST',{product_id:productId,name:watcherDraft.name,mode:watcherDraft.mode});setWatcherDraft({name:'',mode:'propose'})})}}>
          <label className="form-group">Name<input required value={watcherDraft.name} onChange={event=>setWatcherDraft({...watcherDraft,name:event.currentTarget.value})}/></label>
          <label className="form-group">Mode<select value={watcherDraft.mode} onChange={event=>setWatcherDraft({...watcherDraft,mode:event.currentTarget.value})}><option value="observe">Observe</option><option value="propose">Propose</option><option value="execute_if_authorized">Execute if authorized</option></select></label>
          <button className="btn-primary" disabled={busy}>Create paused watcher</button>
        </form>
      </article>
      <article className="card operations-card operations-card--wide"><Heading title="Business automation" note="Each occurrence can create at most one governed invocation; there are no task plans or compensations."/><Table headers={['Watcher','Mode','State','Version','Updated']} empty="No watchers for this organization.">{watchers.map(item=><tr key={item.id}><td><strong>{item.name}</strong><code>{short(item.id)}</code></td><td>{item.mode}</td><td><Status value={item.lifecycle}/></td><td>{item.active_version_id?short(item.active_version_id):'No active version'}</td><td>{formatDateTime24(item.updated_at)}</td></tr>)}</Table></article>
    </div> : null}

    {tab === 'evaluations' ? <div className="operations-grid">
      <article className="card operations-card"><Heading icon={<Beaker/>} title="New evaluation suite" note="Fixtures are synthetic and executors have no external effects."/>
        <form className="operations-form" onSubmit={event=>{event.preventDefault();void submit(async()=>{await api('/api/evaluation-suites','POST',suiteDraft);setSuiteDraft({name:'',artifact_type:'prompt_version'})})}}>
          <label className="form-group">Name<input required value={suiteDraft.name} onChange={event=>setSuiteDraft({...suiteDraft,name:event.currentTarget.value})}/></label>
          <label className="form-group">Artifact<select value={suiteDraft.artifact_type} onChange={event=>setSuiteDraft({...suiteDraft,artifact_type:event.currentTarget.value})}><option value="prompt_version">Prompt version</option><option value="capability_manifest">Capability manifest</option><option value="virployee_snapshot">Virployee snapshot</option></select></label>
          <button className="btn-primary" disabled={busy}>Create suite</button>
        </form>
      </article>
      <article className="card operations-card"><Heading title="Suites"/><Table headers={['Suite','Artifact','Created']} empty="No evaluation suites.">{suites.map(item=><tr key={item.id}><td><strong>{item.name}</strong><code>{short(item.id)}</code></td><td>{item.artifact_type}</td><td>{formatDateTime24(item.created_at)}</td></tr>)}</Table></article>
      <article className="card operations-card operations-card--wide"><Heading icon={<Play/>} title="Recent evaluation runs" note="Isolation, leakage and approval bypass checks have zero tolerance."/><Table headers={['Artifact','Result','Report','Started']} empty="No evaluation runs.">{runs.map(item=><tr key={item.id}><td><strong>{item.artifact_type}</strong><code>{short(item.artifact_ref)}</code></td><td><Status value={item.status}/></td><td><code>{short(item.report_hash)}</code></td><td>{formatDateTime24(item.started_at)}</td></tr>)}</Table></article>
    </div> : null}
  </section>
}

function Heading({icon,title,note}:{icon?:ReactNode;title:string;note?:string}){return <div className="card-header operations-card__heading"><div>{icon}<span><h3>{title}</h3>{note?<p>{note}</p>:null}</span></div></div>}
function Table({headers,empty,children}:{headers:string[];empty:string;children:ReactNode}){const count=Array.isArray(children)?children.length:children?1:0;return <div className="table-wrap"><table><thead><tr>{headers.map(item=><th key={item}>{item}</th>)}</tr></thead><tbody>{count?children:<tr><td colSpan={headers.length} className="operations-empty">{empty}</td></tr>}</tbody></table></div>}
function Status({value}:{value:string}){return <span className={`axis-status-badge axis-status-badge--${value==='active'||value==='passed'?'success':value==='failed'||value==='blocked'?'danger':'warning'}`}>{value.replaceAll('_',' ')}</span>}
function short(value:string){return value.length>16?`${value.slice(0,8)}…${value.slice(-6)}`:value}
function message(cause:unknown,fallback:string){return cause instanceof Error?cause.message:fallback}
