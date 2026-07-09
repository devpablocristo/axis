type LifecycleView = 'active' | 'archived' | 'trash'
type LifecycleBulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type LifecycleBulkActionsProps = {
  selectedCount: number
  view: LifecycleView
  createOpen: boolean
  editOpen?: boolean
  busy: boolean
  blockedMessage?: string
  onCreate: () => void
  onEdit?: () => void
  onClear: () => void
  onBulkAction: (action: LifecycleBulkAction) => void
}

export function LifecycleBulkActions(props: LifecycleBulkActionsProps) {
  const actionsDisabled = props.busy || props.selectedCount === 0
  const editDisabled = props.busy || props.selectedCount !== 1
  return (
    <div className="iam-control__create-inline">
      <div className="iam-control__bulk-buttons">
        <div className="iam-control__button-group">
          <button
            type="button"
            className={`btn-sm ${props.createOpen ? 'btn-primary' : 'btn-secondary'} iam-control__new-button`}
            onClick={props.onCreate}
          >
            New
          </button>
        </div>
          {props.view === 'active' ? (
            <>
              <div className="iam-control__button-group">
                {props.onEdit ? (
                  <button
                    type="button"
                    className={`btn-sm ${props.editOpen ? 'btn-primary' : 'btn-secondary'}`}
                    disabled={editDisabled}
                    onClick={props.onEdit}
                  >
                    Edit
                  </button>
                ) : null}
                <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={props.onClear}>Clear</button>
              </div>
            <div className="iam-control__button-group iam-control__button-group--lifecycle">
              <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={() => props.onBulkAction('archive')}>Archive</button>
              <button type="button" className="btn-sm btn-danger" disabled={actionsDisabled} onClick={() => props.onBulkAction('trash')}>Trash</button>
            </div>
          </>
        ) : null}
        {props.view === 'archived' ? (
          <>
            <div className="iam-control__button-group">
              <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={props.onClear}>Clear</button>
            </div>
            <div className="iam-control__button-group iam-control__button-group--lifecycle">
              <button type="button" className="btn-sm btn-primary" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restore</button>
            </div>
          </>
        ) : null}
        {props.view === 'trash' ? (
          <>
            <div className="iam-control__button-group">
              <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={props.onClear}>Clear</button>
            </div>
            <div className="iam-control__button-group iam-control__button-group--lifecycle">
              <button type="button" className="btn-sm btn-primary" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restore</button>
              <button
                type="button"
                className="btn-sm btn-danger iam-control__danger-button"
                disabled={actionsDisabled}
                onClick={() => props.onBulkAction('purge')}
              >
                Delete
              </button>
            </div>
          </>
        ) : null}
      </div>
      <span className="iam-control__selected-count">{props.selectedCount} selected</span>
      {props.blockedMessage ? (
        <span className="iam-control__inline-note">{props.blockedMessage}</span>
      ) : null}
    </div>
  )
}
