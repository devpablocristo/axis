import {
  LifecycleActionToolbar,
  type LifecycleBulkAction,
  type LifecycleView,
} from '@devpablocristo/platform-lifecycle'

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
  return (
    <LifecycleActionToolbar
      selectedCount={props.selectedCount}
      view={props.view}
      createOpen={props.createOpen}
      editOpen={props.editOpen}
      busy={props.busy}
      blockedMessage={props.blockedMessage}
      onCreate={props.onCreate}
      onEdit={props.onEdit}
      onClear={props.onClear}
      onBulkAction={props.onBulkAction}
      classNames={{
        root: 'iam-control__create-inline',
        buttons: 'iam-control__bulk-buttons',
        group: 'iam-control__button-group',
        lifecycleGroup: 'iam-control__button-group--lifecycle',
        buttonBase: 'btn-sm',
        primaryButton: 'btn-primary',
        secondaryButton: 'btn-secondary',
        dangerButton: 'btn-danger iam-control__danger-button',
        newButton: 'iam-control__new-button',
        selectedCount: 'iam-control__selected-count',
        inlineNote: 'iam-control__inline-note',
      }}
    />
  )
}
