import { useCallback, useEffect, useRef, useState } from 'react'
import {
  acceptLearningProposal,
  dismissLearningProposal,
  listLearningProposals,
  scanLearning,
  type LearningProposal,
  type LearningProposalStatus,
} from './api'
import { formatDateTime24 } from './formatters'

// The review queue for procedural learning (Fase 4). The analyzer distills
// successful executions into procedure proposals; a supervisor reviews and
// accepts (installs as a governed procedure memory) or dismisses. Nothing
// enters a virployee's memory without a human accept here.
const STATUSES: LearningProposalStatus[] = ['pending', 'accepted', 'dismissed']

type ColumnState = Record<LearningProposalStatus, LearningProposal[]>

const EMPTY_COLUMNS: ColumnState = { pending: [], accepted: [], dismissed: [] }

export function LearningProposalsPage(props: { orgId: string; principalId: string }) {
  const { orgId, principalId } = props
  const [columns, setColumns] = useState<ColumnState>(EMPTY_COLUMNS)
  const [loading, setLoading] = useState(true)
  const [scanning, setScanning] = useState(false)
  const [busyID, setBusyID] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  // Monotonic request id: only the newest load() may apply its result, so a
  // slow response from a previous organization can never overwrite the current one.
  const loadSeq = useRef(0)

  const load = useCallback(async () => {
    const seq = ++loadSeq.current
    setLoading(true)
    setError('')
    setNotice('')
    if (!orgId || !principalId) {
      setColumns(EMPTY_COLUMNS)
      setLoading(false)
      return
    }
    try {
      const [pending, accepted, dismissed] = await Promise.all([
        listLearningProposals(orgId, principalId, 'pending'),
        listLearningProposals(orgId, principalId, 'accepted'),
        listLearningProposals(orgId, principalId, 'dismissed'),
      ])
      if (seq !== loadSeq.current) return // a newer load superseded this one
      setColumns({ pending, accepted, dismissed })
    } catch (loadError) {
      if (seq !== loadSeq.current) return
      setError(loadError instanceof Error ? loadError.message : 'Could not load proposals')
    } finally {
      if (seq === loadSeq.current) setLoading(false)
    }
  }, [orgId, principalId])

  useEffect(() => {
    void load()
  }, [load])

  const busy = loading || scanning || Boolean(busyID)

  async function decide(id: string, decision: 'accept' | 'dismiss') {
    if (busy) return
    setBusyID(id)
    setError('')
    setNotice('')
    let message = ''
    try {
      if (decision === 'accept') {
        const result = await acceptLearningProposal(id, orgId, principalId)
        message = `Accepted — installed as a procedure memory for the virployee (${result.proposal.capability_key}).`
      } else {
        await dismissLearningProposal(id, orgId, principalId)
      }
      // Refresh first (load clears the notice), then surface the outcome.
      await load()
      if (message) setNotice(message)
    } catch (decisionError) {
      setError(decisionError instanceof Error ? decisionError.message : 'Could not update the proposal')
    } finally {
      setBusyID('')
    }
  }

  async function runScan() {
    if (busy) return
    setScanning(true)
    setError('')
    setNotice('')
    let message = ''
    try {
      const result = await scanLearning(orgId, principalId)
      message =
        result.proposed > 0
          ? `Scan proposed ${result.proposed} new procedure${result.proposed === 1 ? '' : 's'} (threshold ${result.threshold}).`
          : `Scan found nothing new to propose (threshold ${result.threshold}, ${result.candidates} candidate${result.candidates === 1 ? '' : 's'}).`
      await load()
      if (message) setNotice(message)
    } catch (scanError) {
      setError(scanError instanceof Error ? scanError.message : 'Could not run the scan')
    } finally {
      setScanning(false)
    }
  }

  const totalLoaded = columns.pending.length + columns.accepted.length + columns.dismissed.length

  return (
    <section className="page-section approvals-control">
      <div className="approvals-header-actions">
        <p className="learning-proposals__intro">
          Procedures the workforce learned from repeated successful work. Accept to teach the virployee; dismiss to discard.
        </p>
        <button type="button" className="btn-secondary" disabled={busy} onClick={() => void runScan()}>
          {scanning ? 'Scanning...' : 'Scan for new learnings'}
        </button>
        <button type="button" className="btn-secondary" disabled={busy} onClick={() => void load()}>
          {loading ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      {notice ? <p role="status" className="learning-proposals__notice">{notice}</p> : null}
      {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}

      {loading && totalLoaded === 0 ? (
        <div className="spinner" />
      ) : (
        <div className="approvals-board" aria-label="Learning proposals board">
          {STATUSES.map((status) => (
            <ProposalColumn key={status} status={status} proposals={columns[status]} busyID={busyID} actionsDisabled={busy} onDecide={decide} />
          ))}
        </div>
      )}
    </section>
  )
}

function ProposalColumn(props: {
  status: LearningProposalStatus
  proposals: LearningProposal[]
  busyID: string
  actionsDisabled: boolean
  onDecide: (id: string, decision: 'accept' | 'dismiss') => void
}) {
  return (
    <section className={`approvals-board__column approvals-board__column--${columnModifier(props.status)}`} aria-label={columnTitle(props.status)}>
      <div className="approvals-board__column-header">
        <div>
          <h3>{columnTitle(props.status)}</h3>
          <p>{columnCopy(props.status)}</p>
        </div>
        <span className={`axis-status-badge axis-status-badge--${statusTone(props.status)}`}>{props.proposals.length}</span>
      </div>

      {props.proposals.length === 0 ? (
        <div className="approvals-board__empty">{emptyCopy(props.status)}</div>
      ) : (
        <div className="approvals-board__cards">
          {props.proposals.map((proposal, index) => (
            <ProposalCard
              key={proposal.id}
              proposal={proposal}
              index={index + 1}
              busy={props.busyID === proposal.id}
              disabled={props.actionsDisabled}
              onDecide={props.onDecide}
            />
          ))}
        </div>
      )}
    </section>
  )
}

function ProposalCard(props: {
  proposal: LearningProposal
  index: number
  busy: boolean
  disabled: boolean
  onDecide: (id: string, decision: 'accept' | 'dismiss') => void
}) {
  const p = props.proposal
  const succeeded = numericEvidence(p.evidence, 'executions_succeeded')
  return (
    <article className="approvals-board__card" aria-busy={props.busy}>
      <div className="approvals-board__card-title">
        <div>
          <span className="approvals-list__eyebrow">{p.capability_key}</span>
          <strong>{p.title}</strong>
        </div>
        <div className="approvals-board__card-markers">
          <span className="approvals-board__card-index">#{props.index}</span>
          <span className={`axis-status-badge axis-status-badge--${p.proposed_by === 'llm' ? 'info' : 'muted'}`}>
            {p.proposed_by === 'llm' ? 'AI-drafted' : 'Analyzer'}
          </span>
        </div>
      </div>

      <p className="approvals-board__reason">{firstLine(p.content)}</p>

      <div className="approvals-board__facts">
        {succeeded != null ? <MetaValue label="Learned from" value={`${succeeded} successful run${succeeded === 1 ? '' : 's'}`} /> : null}
        <MetaValue label="Evidence" value={`${p.source_trace_ids.length} trace${p.source_trace_ids.length === 1 ? '' : 's'}`} />
        <MetaValue label="Virployee" value={shortHash(p.virployee_id)} />
        <MetaValue label="Created" value={formatDateTime24(p.created_at)} />
        {p.decided_by ? (
          <MetaValue label="Decision" value={`${statusLabel(p.status)} by ${shortHash(p.decided_by)} · ${formatDateTime24(p.decided_at)}`} />
        ) : null}
        {p.status === 'accepted' && p.memory_id ? <MetaValue label="Memory" value={shortHash(p.memory_id)} /> : null}
      </div>

      {p.status === 'pending' ? (
        <div className="approvals-board__actions">
          <button type="button" className="btn-danger" disabled={props.disabled} onClick={() => props.onDecide(p.id, 'dismiss')}>
            {props.busy ? 'Working...' : 'Dismiss'}
          </button>
          <button type="button" className="btn-success" disabled={props.disabled} onClick={() => props.onDecide(p.id, 'accept')}>
            {props.busy ? 'Working...' : 'Accept'}
          </button>
        </div>
      ) : (
        <div className="approvals-board__settled">
          {p.status === 'accepted' ? 'Installed as a procedure memory' : 'Dismissed'}
        </div>
      )}
    </article>
  )
}

function MetaValue(props: { label: string; value: string }) {
  return (
    <span className="axis-meta-value">
      <span>{props.label}</span>
      <strong>{props.value}</strong>
    </span>
  )
}

function columnTitle(status: LearningProposalStatus): string {
  if (status === 'pending') return 'Awaiting review'
  if (status === 'accepted') return 'Accepted'
  return 'Dismissed'
}

function columnCopy(status: LearningProposalStatus): string {
  if (status === 'pending') return 'Learned procedures a supervisor has not decided on yet.'
  if (status === 'accepted') return 'Installed into the virployee as governed procedure memory.'
  return 'Discarded — not taught to the virployee.'
}

function emptyCopy(status: LearningProposalStatus): string {
  if (status === 'pending') return 'No proposals waiting. Run a scan after the workforce logs successful runs.'
  if (status === 'accepted') return 'Nothing accepted yet.'
  return 'Nothing dismissed.'
}

function columnModifier(status: LearningProposalStatus): string {
  // Reuse the approvals column color modifiers: pending→pending, accepted→approved, dismissed→rejected.
  if (status === 'accepted') return 'approved'
  if (status === 'dismissed') return 'rejected'
  return 'pending'
}

function statusTone(status: LearningProposalStatus): string {
  if (status === 'accepted') return 'success'
  if (status === 'dismissed') return 'muted'
  return 'warning'
}

function statusLabel(status: LearningProposalStatus): string {
  if (status === 'accepted') return 'Accepted'
  if (status === 'dismissed') return 'Dismissed'
  return 'Pending'
}

function numericEvidence(evidence: Record<string, unknown>, key: string): number | null {
  const value = evidence?.[key]
  return typeof value === 'number' ? value : null
}

function firstLine(content: string): string {
  const line = content.split('\n').find((l) => l.trim().length > 0)
  return line ? line.trim() : 'Distilled procedure'
}

function shortHash(value: string): string {
  if (!value) return '-'
  return value.length <= 12 ? value : `${value.slice(0, 8)}…`
}
