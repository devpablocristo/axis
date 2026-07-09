import { useEffect, useMemo, useState } from 'react'
import {
  type Approval,
  approveApproval,
  listApprovals,
  rejectApproval,
} from './api'

type ApprovalsPageProps = {
  tenantId: string
  principalId: string
}

type ApprovalStatus = Approval['status']
type ApprovalsByStatus = Record<ApprovalStatus, Approval[]>

const APPROVAL_STATUSES: ApprovalStatus[] = ['pending', 'approved', 'rejected']
const EMPTY_APPROVALS: ApprovalsByStatus = {
  pending: [],
  approved: [],
  rejected: [],
}

export function ApprovalsPage({ tenantId, principalId }: ApprovalsPageProps) {
  const [approvalsByStatus, setApprovalsByStatus] = useState<ApprovalsByStatus>(EMPTY_APPROVALS)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [busyID, setBusyID] = useState('')
  const isActive = Boolean(tenantId && principalId)
  const pendingCount = approvalsByStatus.pending.length
  const totalCount = useMemo(
    () => APPROVAL_STATUSES.reduce((count, status) => count + approvalsByStatus[status].length, 0),
    [approvalsByStatus],
  )

  useEffect(() => {
    if (!isActive) {
      setApprovalsByStatus(EMPTY_APPROVALS)
      setError('')
      setLoading(false)
      return
    }
    void load()
  }, [isActive, tenantId, principalId])

  async function load() {
    setLoading(true)
    setError('')
    try {
      const entries = await Promise.all(
        APPROVAL_STATUSES.map(async (status): Promise<[ApprovalStatus, Approval[]]> => [
          status,
          sortApprovals(await listApprovals(tenantId, principalId, status, 50)),
        ]),
      )
      setApprovalsByStatus(Object.fromEntries(entries) as ApprovalsByStatus)
    } catch (loadError) {
      setApprovalsByStatus(EMPTY_APPROVALS)
      setError(loadError instanceof Error ? loadError.message : 'Could not load approvals')
    } finally {
      setLoading(false)
    }
  }

  async function decide(id: string, decision: 'approve' | 'reject') {
    if (busyID) return
    setBusyID(id)
    setError('')
    try {
      if (decision === 'approve') {
        await approveApproval(id, tenantId, principalId)
      } else {
        await rejectApproval(id, tenantId, principalId)
      }
      await load()
    } catch (decisionError) {
      setError(decisionError instanceof Error ? decisionError.message : 'Could not update approval')
    } finally {
      setBusyID('')
    }
  }

  if (!isActive) {
    return (
      <section className="page-section">
        <div className="empty-state">Select an active tenant to manage Approvals.</div>
      </section>
    )
  }

  return (
    <section className="page-section approvals-control">
      <div className="page-header">
        <div>
          <h2>Approvals board</h2>
          <p className="axis-muted">{approvalBoardSummary(pendingCount, totalCount, loading)}</p>
        </div>
        <button type="button" className="btn-secondary" disabled={loading || Boolean(busyID)} onClick={() => void load()}>
          {loading ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}

      {loading && totalCount === 0 ? (
        <div className="spinner" />
      ) : (
        <div className="approvals-board" aria-label="Approvals board">
          {APPROVAL_STATUSES.map((status) => (
            <ApprovalColumn
              key={status}
              status={status}
              approvals={approvalsByStatus[status]}
              busyID={busyID}
              onDecide={decide}
            />
          ))}
        </div>
      )}
    </section>
  )
}

function ApprovalColumn(props: {
  status: ApprovalStatus
  approvals: Approval[]
  busyID: string
  onDecide: (id: string, decision: 'approve' | 'reject') => void
}) {
  return (
    <section className={`approvals-board__column approvals-board__column--${props.status}`} aria-label={approvalColumnTitle(props.status)}>
      <div className="approvals-board__column-header">
        <div>
          <h3>{approvalColumnTitle(props.status)}</h3>
          <p>{approvalColumnCopy(props.status)}</p>
        </div>
        <span className={`axis-status-badge axis-status-badge--${approvalStatusTone(props.status)}`}>
          {props.approvals.length}
        </span>
      </div>

      {props.approvals.length === 0 ? (
        <div className="approvals-board__empty">{emptyStateFor(props.status)}</div>
      ) : (
        <div className="approvals-board__cards">
          {props.approvals.map((approval) => (
            <ApprovalCard
              key={approval.id}
              approval={approval}
              busy={props.busyID === approval.id}
              disabled={Boolean(props.busyID)}
              onDecide={props.onDecide}
            />
          ))}
        </div>
      )}
    </section>
  )
}

function ApprovalCard(props: {
  approval: Approval
  busy: boolean
  disabled: boolean
  onDecide: (id: string, decision: 'approve' | 'reject') => void
}) {
  const approval = props.approval
  return (
    <article className="approvals-board__card" aria-busy={props.busy}>
      <div className="approvals-board__card-title">
        <div>
          <span className="approvals-list__eyebrow">{approval.target_system || 'Unknown system'}</span>
          <strong>{approval.action_type}</strong>
        </div>
        <span className={`axis-status-badge axis-status-badge--${approvalStatusTone(approval.status)}`}>
          {approvalStatusLabel(approval.status)}
        </span>
      </div>

      <p className="approvals-board__reason">{approval.reason || 'No reason provided'}</p>

      <div className="approvals-board__facts">
        <MetaValue label="Requester" value={shortHash(approval.requester_id)} />
        <MetaValue label="Risk" value={approval.risk_level || 'unknown'} />
        <MetaValue label="Resource" value={`${approval.target_system || '-'} / ${approval.target_resource || '-'}`} />
        <MetaValue label="Created" value={formatDate(approval.created_at)} />
        <MetaValue label="Approval" value={shortHash(approval.id)} />
        <MetaValue label="Binding" value={shortHash(approval.binding_hash)} />
        {approval.decided_by ? (
          <MetaValue
            label="Decision"
            value={`${approvalStatusLabel(approval.status)} by ${shortHash(approval.decided_by)} · ${formatDate(approval.decided_at)}`}
          />
        ) : null}
        {approval.decision_note ? <MetaValue label="Note" value={approval.decision_note} /> : null}
      </div>

      {approval.status === 'pending' ? (
        <div className="approvals-board__actions">
          <button
            type="button"
            className="btn-danger"
            disabled={props.disabled}
            onClick={() => props.onDecide(approval.id, 'reject')}
          >
            {props.busy ? 'Working...' : 'Reject'}
          </button>
          <button
            type="button"
            className="btn-success"
            disabled={props.disabled}
            onClick={() => props.onDecide(approval.id, 'approve')}
          >
            {props.busy ? 'Working...' : 'Approve'}
          </button>
        </div>
      ) : (
        <div className="approvals-board__settled">
          {approval.decided_at ? formatDate(approval.decided_at) : 'Decision recorded'}
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

function approvalBoardSummary(pendingCount: number, totalCount: number, loading: boolean): string {
  if (loading && totalCount === 0) return 'Loading approval requests...'
  const pendingNoun = pendingCount === 1 ? 'request' : 'requests'
  const totalNoun = totalCount === 1 ? 'approval' : 'approvals'
  return `${pendingCount} pending ${pendingNoun} · ${totalCount} total ${totalNoun}`
}

function approvalColumnTitle(status: ApprovalStatus): string {
  if (status === 'approved') return 'Approved'
  if (status === 'rejected') return 'Rejected'
  return 'Pending'
}

function approvalColumnCopy(status: ApprovalStatus): string {
  if (status === 'approved') return 'Resolved and allowed'
  if (status === 'rejected') return 'Resolved and denied'
  return 'Waiting for a human decision'
}

function approvalStatusLabel(status: ApprovalStatus): string {
  if (status === 'approved') return 'Approved'
  if (status === 'rejected') return 'Rejected'
  return 'Pending'
}

function approvalStatusTone(status: ApprovalStatus): 'success' | 'danger' | 'warning' {
  if (status === 'approved') return 'success'
  if (status === 'rejected') return 'danger'
  return 'warning'
}

function emptyStateFor(status: ApprovalStatus): string {
  if (status === 'approved') return 'No approved approvals'
  if (status === 'rejected') return 'No rejected approvals'
  return 'No pending approvals'
}

function sortApprovals(approvals: Approval[]): Approval[] {
  return [...approvals].sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))
}

function formatDate(value: string | null): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('en-US', { dateStyle: 'short', timeStyle: 'short' })
}

function shortHash(value: string): string {
  if (!value) return '-'
  return value.length <= 12 ? value : value.slice(0, 12)
}
