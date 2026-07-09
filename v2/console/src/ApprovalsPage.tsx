import { CheckCircle2, RefreshCw, XCircle } from 'lucide-react'
import { useEffect, useState } from 'react'
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

export function ApprovalsPage({ tenantId, principalId }: ApprovalsPageProps) {
  const [status, setStatus] = useState<Approval['status']>('pending')
  const [approvals, setApprovals] = useState<Approval[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [busyID, setBusyID] = useState('')
  const isActive = Boolean(tenantId && principalId)

  useEffect(() => {
    if (!isActive) {
      setApprovals([])
      setError('')
      setLoading(false)
      return
    }
    void load()
  }, [isActive, tenantId, principalId, status])

  async function load() {
    setLoading(true)
    setError('')
    try {
      const items = await listApprovals(tenantId, principalId, status, 50)
      setApprovals(items)
    } catch (loadError) {
      setApprovals([])
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
          <h2>{approvalStatusTitle(status)}</h2>
          <p className="axis-muted">{approvalCountCopy(status, approvals.length, loading)}</p>
        </div>
        <button type="button" className="btn-secondary" disabled={loading || Boolean(busyID)} onClick={() => void load()}>
          <RefreshCw aria-hidden="true" />
          {loading ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      <div className="approval-status-tabs" aria-label="Approval status">
        {(['pending', 'approved', 'rejected'] as Approval['status'][]).map((nextStatus) => (
          <button
            key={nextStatus}
            type="button"
            className={status === nextStatus ? 'active' : ''}
            disabled={loading || Boolean(busyID)}
            onClick={() => setStatus(nextStatus)}
          >
            {approvalStatusLabel(nextStatus)}
          </button>
        ))}
      </div>

      {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}

      {loading && approvals.length === 0 ? (
        <div className="spinner" />
      ) : approvals.length === 0 ? (
        <div className="empty-state">{emptyStateFor(status)}</div>
      ) : (
        <div className="approvals-list">
          {approvals.map((approval) => (
            <article key={approval.id} className="approvals-list__item" aria-busy={busyID === approval.id}>
              <div className="approvals-list__main">
                <div className="approvals-list__title">
                  <div>
                    <span className="approvals-list__eyebrow">{approval.target_system || 'Unknown system'}</span>
                    <strong>{approval.action_type}</strong>
                  </div>
                  <span className={`axis-status-badge axis-status-badge--${approvalStatusTone(approval.status)}`}>
                    {approvalStatusLabel(approval.status)}
                  </span>
                </div>
                <small>{approval.reason || 'No reason provided'}</small>
                <div className="approvals-list__facts">
                  <MetaValue label="Requester" value={shortHash(approval.requester_id)} />
                  <MetaValue label="Risk" value={approval.risk_level || 'unknown'} />
                  <MetaValue label="Resource" value={`${approval.target_system || '-'} / ${approval.target_resource || '-'}`} />
                </div>
              </div>
              <div className="approvals-list__meta">
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
                <div className="approvals-list__actions">
                  <button
                    type="button"
                    className="btn-danger"
                    disabled={Boolean(busyID)}
                    onClick={() => void decide(approval.id, 'reject')}
                  >
                    <XCircle aria-hidden="true" />
                    {busyID === approval.id ? 'Working...' : 'Reject'}
                  </button>
                  <button
                    type="button"
                    className="btn-primary"
                    disabled={Boolean(busyID)}
                    onClick={() => void decide(approval.id, 'approve')}
                  >
                    <CheckCircle2 aria-hidden="true" />
                    {busyID === approval.id ? 'Working...' : 'Approve'}
                  </button>
                </div>
              ) : (
                <div className="approvals-list__actions approvals-list__actions--settled">
                  <span>{approval.decided_at ? formatDate(approval.decided_at) : 'Decision recorded'}</span>
                </div>
              )}
            </article>
          ))}
        </div>
      )}
    </section>
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

function approvalStatusTitle(status: Approval['status']): string {
  if (status === 'approved') return 'Approved'
  if (status === 'rejected') return 'Rejected'
  return 'Pending approvals'
}

function approvalCountCopy(status: Approval['status'], count: number, loading: boolean): string {
  if (loading && count === 0) return 'Loading requests...'
  const noun = count === 1 ? 'request' : 'requests'
  if (status === 'approved') return `${count} approved ${noun}`
  if (status === 'rejected') return `${count} rejected ${noun}`
  return `${count} pending ${noun} waiting for a human decision`
}

function approvalStatusLabel(status: Approval['status']): string {
  if (status === 'approved') return 'Approved'
  if (status === 'rejected') return 'Rejected'
  return 'Pending'
}

function approvalStatusTone(status: Approval['status']): 'success' | 'danger' | 'warning' {
  if (status === 'approved') return 'success'
  if (status === 'rejected') return 'danger'
  return 'warning'
}

function emptyStateFor(status: Approval['status']): string {
  if (status === 'approved') return 'No approved approvals'
  if (status === 'rejected') return 'No rejected approvals'
  return 'No pending approvals'
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
