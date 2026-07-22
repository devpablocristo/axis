import { useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'
import { LoadMoreControl } from '@devpablocristo/platform-ui-data-display'
import {
	type Approval,
	approveApproval,
	getApproval,
	listApprovalsPage,
	rejectApproval,
	reviewApproval,
} from './api'

type ApprovalsPageProps = {
  tenantId: string
  principalId: string
  focusApprovalId?: string
  onReturnToVirployee?: () => void
}

type ApprovalStatus = Approval['status']
type ApprovalColumnState = {
	items: Approval[]
	hasMore: boolean
	nextCursor: string
	loadingMore: boolean
}
type ApprovalsByStatus = Record<ApprovalStatus, ApprovalColumnState>

const APPROVAL_STATUSES: ApprovalStatus[] = ['pending', 'approved', 'rejected', 'expired']
const APPROVAL_PAGE_LIMITS: Record<ApprovalStatus, number> = {
	pending: 25,
	approved: 10,
	rejected: 10,
	expired: 10,
}

export function ApprovalsPage({ tenantId, principalId, focusApprovalId = '', onReturnToVirployee }: ApprovalsPageProps) {
  const [approvalsByStatus, setApprovalsByStatus] = useState<ApprovalsByStatus>(() => emptyApprovalColumns())
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [busyID, setBusyID] = useState('')
  const [searchQuery, setSearchQuery] = useState('')
  const focusedCardRef = useRef<HTMLElement | null>(null)
  const isActive = Boolean(tenantId && principalId)
  const normalizedSearchQuery = searchQuery.trim().toLowerCase()
  const totalCount = useMemo(
    () => APPROVAL_STATUSES.reduce((count, status) => count + approvalsByStatus[status].items.length, 0),
    [approvalsByStatus],
  )
  const visibleApprovalsByStatus = useMemo(
    () => filterApprovalsByStatus(approvalsByStatus, normalizedSearchQuery),
    [approvalsByStatus, normalizedSearchQuery],
  )
  const loadingMore = APPROVAL_STATUSES.some((status) => approvalsByStatus[status].loadingMore)
  const focusedApproval = useMemo(() => {
    if (!focusApprovalId) return null
    for (const status of APPROVAL_STATUSES) {
      const approval = approvalsByStatus[status].items.find((item) => item.id === focusApprovalId)
      if (approval) return approval
    }
    return null
  }, [approvalsByStatus, focusApprovalId])

  useEffect(() => {
    if (!isActive) {
      setApprovalsByStatus(emptyApprovalColumns())
      setError('')
      setLoading(false)
      return
    }
    void load()
  }, [isActive, tenantId, principalId])

  useEffect(() => {
    if (!focusApprovalId || !focusedApproval || loading) return
    focusedCardRef.current?.scrollIntoView({ block: 'center', behavior: 'smooth' })
  }, [focusedApproval, focusApprovalId, loading])

  async function load() {
    setLoading(true)
    setError('')
    try {
      const entries = await Promise.all(
        APPROVAL_STATUSES.map(async (status): Promise<[ApprovalStatus, ApprovalColumnState]> => {
          const page = await listApprovalsPage(tenantId, principalId, status, { limit: APPROVAL_PAGE_LIMITS[status] })
          return [status, {
            items: sortApprovals(page.items),
            hasMore: page.hasMore,
            nextCursor: page.nextCursor,
            loadingMore: false,
          }]
        }),
      )
      const next = await withFocusedApproval(Object.fromEntries(entries) as ApprovalsByStatus)
      setApprovalsByStatus(next)
    } catch (loadError) {
      setApprovalsByStatus(emptyApprovalColumns())
      setError(loadError instanceof Error ? loadError.message : 'Could not load approvals')
    } finally {
      setLoading(false)
    }
  }

  async function loadMore(status: ApprovalStatus) {
    const column = approvalsByStatus[status]
    if (column.loadingMore || !column.hasMore || !column.nextCursor) return
    setApprovalsByStatus((current) => ({
      ...current,
      [status]: { ...current[status], loadingMore: true },
    }))
    setError('')
    try {
      const page = await listApprovalsPage(tenantId, principalId, status, {
        limit: APPROVAL_PAGE_LIMITS[status],
        cursor: column.nextCursor,
      })
      setApprovalsByStatus((current) => ({
        ...current,
        [status]: {
          items: sortApprovals(mergeApprovals(current[status].items, page.items)),
          hasMore: page.hasMore,
          nextCursor: page.nextCursor,
          loadingMore: false,
        },
      }))
    } catch (loadError) {
      setApprovalsByStatus((current) => ({
        ...current,
        [status]: { ...current[status], loadingMore: false },
      }))
      setError(loadError instanceof Error ? loadError.message : 'Could not load more approvals')
    }
  }

  async function withFocusedApproval(columns: ApprovalsByStatus): Promise<ApprovalsByStatus> {
    if (!focusApprovalId) return columns
    const loaded = APPROVAL_STATUSES.some((status) => columns[status].items.some((approval) => approval.id === focusApprovalId))
    if (loaded) return columns
    try {
      const approval = await getApproval(focusApprovalId, tenantId, principalId)
      return insertApproval(columns, approval)
    } catch {
      return columns
    }
  }

  async function decide(approval: Approval, decision: 'approve' | 'reject' | 'review') {
    if (busyID) return
    const note = approval.approval_kind === 'break_glass' || decision === 'review'
      ? window.prompt(decision === 'review' ? 'Post-action review note' : 'Break-glass justification')?.trim()
      : ''
    if ((approval.approval_kind === 'break_glass' || decision === 'review') && !note) return
    setBusyID(approval.id)
    setError('')
    try {
      if (decision === 'approve') {
        await approveApproval(approval.id, tenantId, principalId, note)
      } else if (decision === 'reject') {
        await rejectApproval(approval.id, tenantId, principalId, note)
	  } else {
		await reviewApproval(approval.id, tenantId, principalId, note ?? '')
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
      <div className="approvals-header-actions">
        <label className="approvals-search">
          <span className="axis-visually-hidden">Search approvals</span>
          <input
            type="search"
            className="axis-search-input"
            aria-label="Search approvals"
            value={searchQuery}
            placeholder="Search by action, system, requester or binding"
            onChange={(event) => setSearchQuery(event.target.value)}
          />
        </label>
        {searchQuery ? (
          <button type="button" className="btn-secondary" onClick={() => setSearchQuery('')}>
            Clear
          </button>
        ) : null}
        <button type="button" className="btn-secondary" disabled={loading || loadingMore || Boolean(busyID)} onClick={() => void load()}>
          {loading ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>

      {error ? <p role="alert" className="iam-control__inline-error">{error}</p> : null}

      {focusApprovalId ? (
        <div className={`approval-focus-banner ${focusedApproval ? '' : 'approval-focus-banner--missing'}`}>
          <div>
            <strong>{focusedApproval ? 'Reviewing approval' : 'Approval not found'}</strong>
            <span>
              {focusedApproval
                ? `${focusedApproval.action_type} · ${approvalStatusLabel(focusedApproval.status)} · ${shortHash(focusedApproval.id)}`
                : `${shortHash(focusApprovalId)} is not in the loaded approvals.`}
            </span>
          </div>
          <div className="approval-focus-banner__actions">
            <button type="button" className="btn-secondary" disabled={loading || loadingMore || Boolean(busyID)} onClick={() => void load()}>
              Refresh
            </button>
            {onReturnToVirployee ? (
              <button type="button" className="btn-primary" onClick={onReturnToVirployee}>
                Back to Virployee
              </button>
            ) : null}
          </div>
        </div>
      ) : null}

      {loading && totalCount === 0 ? (
        <div className="spinner" />
      ) : (
        <div className="approvals-board" aria-label="Approvals board">
          {APPROVAL_STATUSES.map((status) => (
            <ApprovalColumn
              key={status}
              status={status}
              approvals={visibleApprovalsByStatus[status].items}
              loadedCount={approvalsByStatus[status].items.length}
              searchActive={Boolean(normalizedSearchQuery)}
              hasMore={approvalsByStatus[status].hasMore}
              loadingMore={approvalsByStatus[status].loadingMore}
              busyID={busyID}
              focusApprovalId={focusApprovalId}
              focusedCardRef={focusedCardRef}
              onDecide={decide}
              onLoadMore={() => void loadMore(status)}
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
  loadedCount: number
  searchActive: boolean
  hasMore: boolean
  loadingMore: boolean
  busyID: string
  focusApprovalId: string
  focusedCardRef: MutableRefObject<HTMLElement | null>
  onDecide: (approval: Approval, decision: 'approve' | 'reject' | 'review') => void
  onLoadMore: () => void
}) {
  return (
    <section className={`approvals-board__column approvals-board__column--${props.status}`} aria-label={approvalColumnTitle(props.status)}>
      <div className="approvals-board__column-header">
        <div>
          <h3>{approvalColumnTitle(props.status)}</h3>
          <p>{approvalColumnCopy(props.status)}</p>
        </div>
        <span className={`axis-status-badge axis-status-badge--${approvalStatusTone(props.status)}`}>
          {props.searchActive ? `${props.approvals.length}/${props.loadedCount}` : props.loadedCount}
        </span>
      </div>

      {props.approvals.length === 0 ? (
        <div className="approvals-board__empty">{emptyStateFor(props.status, props.searchActive)}</div>
      ) : (
        <div className="approvals-board__cards">
          {props.approvals.map((approval, index) => (
            <ApprovalCard
              key={approval.id}
              approval={approval}
              index={index + 1}
              busy={props.busyID === approval.id}
              disabled={Boolean(props.busyID)}
              focused={props.focusApprovalId === approval.id}
              cardRef={props.focusApprovalId === approval.id ? (node) => {
                props.focusedCardRef.current = node
              } : undefined}
              onDecide={props.onDecide}
            />
          ))}
        </div>
      )}
      {props.hasMore ? (
        <LoadMoreControl
          className="approvals-board__pager"
          hasMore={props.hasMore}
          loading={props.loadingMore}
          disabled={props.loadingMore || Boolean(props.busyID)}
          onLoadMore={props.onLoadMore}
          loadMoreLabel="Load more"
          loadingLabel="Loading..."
          endLabel=""
        />
      ) : null}
    </section>
  )
}

function ApprovalCard(props: {
  approval: Approval
  index: number
  busy: boolean
  disabled: boolean
  focused: boolean
  cardRef?: (node: HTMLElement | null) => void
  onDecide: (approval: Approval, decision: 'approve' | 'reject' | 'review') => void
}) {
  const approval = props.approval
  return (
    <article
      ref={props.cardRef}
      className={`approvals-board__card ${props.focused ? 'approvals-board__card--focused' : ''}`}
      aria-busy={props.busy}
    >
      <div className="approvals-board__card-title">
        <div>
          <span className="approvals-list__eyebrow">{approval.target_system || 'Unknown system'}</span>
          <strong>{approval.action_type}</strong>
        </div>
        <div className="approvals-board__card-markers">
          <span className="approvals-board__card-index">#{props.index}</span>
          <span className={`axis-status-badge axis-status-badge--${approvalStatusTone(approval.status)}`}>
            {approvalStatusLabel(approval.status)}
          </span>
        </div>
      </div>

      <p className="approvals-board__reason">{approval.reason || 'No reason provided'}</p>

      <div className="approvals-board__facts">
        <MetaValue label="Requester" value={shortHash(approval.requester_id)} />
        <MetaValue label="Risk" value={approval.risk_level || 'unknown'} />
		<MetaValue label="Quorum" value={`${approval.approval_count}/${approval.quorum_required}`} />
        <MetaValue label="Resource" value={`${approval.target_system || '-'} / ${approval.target_resource || '-'}`} />
        <MetaValue label="Created" value={formatDate(approval.created_at)} />
        <MetaValue label="Expires" value={formatDate(approval.expires_at)} />
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
            onClick={() => props.onDecide(approval, 'reject')}
          >
            {props.busy ? 'Working...' : 'Reject'}
          </button>
          <button
            type="button"
            className="btn-success"
            disabled={props.disabled}
            onClick={() => props.onDecide(approval, 'approve')}
          >
            {props.busy ? 'Working...' : 'Approve'}
          </button>
        </div>
      ) : approval.status === 'approved' && approval.post_review_required && !approval.reviewed_at ? (
		<div className="approvals-board__actions">
		  <button type="button" className="btn-secondary" disabled={props.disabled} onClick={() => props.onDecide(approval, 'review')}>
			{props.busy ? 'Working...' : 'Record post-action review'}
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

function approvalColumnTitle(status: ApprovalStatus): string {
	if (status === 'expired') return 'Expired'
  if (status === 'approved') return 'Approved'
  if (status === 'rejected') return 'Rejected'
  return 'Pending'
}

function approvalColumnCopy(status: ApprovalStatus): string {
	if (status === 'expired') return 'Closed after its decision window elapsed'
  if (status === 'approved') return 'Resolved and allowed'
  if (status === 'rejected') return 'Resolved and denied'
  return 'Waiting for a human decision'
}

function approvalStatusLabel(status: ApprovalStatus): string {
	if (status === 'expired') return 'Expired'
  if (status === 'approved') return 'Approved'
  if (status === 'rejected') return 'Rejected'
  return 'Pending'
}

function approvalStatusTone(status: ApprovalStatus): 'success' | 'danger' | 'warning' {
  if (status === 'approved') return 'success'
	if (status === 'rejected') return 'danger'
	if (status === 'expired') return 'danger'
  return 'warning'
}

function emptyStateFor(status: ApprovalStatus, searchActive = false): string {
  if (searchActive) return 'No matching approvals loaded'
  if (status === 'approved') return 'No approved approvals'
	if (status === 'rejected') return 'No rejected approvals'
	if (status === 'expired') return 'No expired approvals'
  return 'No pending approvals'
}

function sortApprovals(approvals: Approval[]): Approval[] {
  return [...approvals].sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))
}

function mergeApprovals(current: Approval[], incoming: Approval[]): Approval[] {
  const byID = new Map<string, Approval>()
  for (const approval of current) {
    byID.set(approval.id, approval)
  }
  for (const approval of incoming) {
    byID.set(approval.id, approval)
  }
  return Array.from(byID.values())
}

function filterApprovalsByStatus(columns: ApprovalsByStatus, query: string): ApprovalsByStatus {
  if (!query) return columns
  return {
    pending: { ...columns.pending, items: columns.pending.items.filter((approval) => approvalMatchesQuery(approval, query)) },
    approved: { ...columns.approved, items: columns.approved.items.filter((approval) => approvalMatchesQuery(approval, query)) },
    rejected: { ...columns.rejected, items: columns.rejected.items.filter((approval) => approvalMatchesQuery(approval, query)) },
		expired: { ...columns.expired, items: columns.expired.items.filter((approval) => approvalMatchesQuery(approval, query)) },
  }
}

function approvalMatchesQuery(approval: Approval, query: string): boolean {
  return [
    approval.id,
    approval.requester_id,
    approval.action_type,
    approval.target_system,
    approval.target_resource,
    approval.risk_level,
    approval.reason,
    approval.binding_hash,
    approval.status,
    approval.decided_by,
    approval.decision_note,
    approval.created_at,
    approval.decided_at,
		approval.expires_at,
  ].some((value) => String(value ?? '').toLowerCase().includes(query))
}

function insertApproval(columns: ApprovalsByStatus, approval: Approval): ApprovalsByStatus {
  const next = { ...columns }
  for (const status of APPROVAL_STATUSES) {
    next[status] = {
      ...next[status],
      items: next[status].items.filter((item) => item.id !== approval.id),
    }
  }
  next[approval.status] = {
    ...next[approval.status],
    items: sortApprovals([approval, ...next[approval.status].items]),
  }
  return next
}

function emptyApprovalColumns(): ApprovalsByStatus {
  return {
    pending: emptyApprovalColumn(),
    approved: emptyApprovalColumn(),
    rejected: emptyApprovalColumn(),
		expired: emptyApprovalColumn(),
  }
}

function emptyApprovalColumn(): ApprovalColumnState {
  return {
    items: [],
    hasMore: false,
    nextCursor: '',
    loadingMore: false,
  }
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
