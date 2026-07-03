import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useEffect, useMemo, useRef, useState, type ReactElement } from 'react'
import { createPortal } from 'react-dom'
import {
  type VirployeeAutonomy,
  type VirployeeAutonomyLevel,
  type Virployee,
  archiveVirployee,
  createVirployee,
  listVirployeeAutonomyLevels,
  listVirployees,
  purgeVirployee,
  restoreVirployee,
  trashVirployee,
  unarchiveVirployee,
  updateVirployee,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type VirployeesPageProps = {
  tenantId: string
  principalId: string
}

const VISIBLE_AUTONOMY_LEVELS: VirployeeAutonomy[] = ['A0', 'A1', 'A2', 'A3']

const FALLBACK_AUTONOMY_LEVELS: VirployeeAutonomyLevel[] = [
  {
    level: 'A0',
    name: 'Conversation',
    description: 'Can hold conversation and read contextual information.',
    allowed_action_classes: [
      {
        class: 'observe',
        name: 'Observe',
        description: 'Read context and hold conversation.',
        requires_approval: false,
      },
    ],
  },
  {
    level: 'A1',
    name: 'Recommendation',
    description: 'Can read, analyze and recommend actions.',
    allowed_action_classes: [
      {
        class: 'observe',
        name: 'Observe',
        description: 'Read context and hold conversation.',
        requires_approval: false,
      },
      {
        class: 'recommend',
        name: 'Recommend',
        description: 'Analyze context and recommend actions.',
        requires_approval: false,
      },
    ],
  },
  {
    level: 'A2',
    name: 'Draft',
    description: 'Can prepare plans or executable drafts.',
    allowed_action_classes: [
      {
        class: 'observe',
        name: 'Observe',
        description: 'Read context and hold conversation.',
        requires_approval: false,
      },
      {
        class: 'recommend',
        name: 'Recommend',
        description: 'Analyze context and recommend actions.',
        requires_approval: false,
      },
      {
        class: 'draft',
        name: 'Draft',
        description: 'Prepare plans or executable drafts.',
        requires_approval: false,
      },
    ],
  },
  {
    level: 'A3',
    name: 'Limited execution',
    description: 'Can execute low-risk reversible writes.',
    allowed_action_classes: [
      {
        class: 'observe',
        name: 'Observe',
        description: 'Read context and hold conversation.',
        requires_approval: false,
      },
      {
        class: 'recommend',
        name: 'Recommend',
        description: 'Analyze context and recommend actions.',
        requires_approval: false,
      },
      {
        class: 'draft',
        name: 'Draft',
        description: 'Prepare plans or executable drafts.',
        requires_approval: false,
      },
      {
        class: 'write_low',
        name: 'Low-risk write',
        description: 'Execute low-risk reversible writes.',
        requires_approval: false,
      },
    ],
  },
]

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function VirployeesPage({ tenantId, principalId }: VirployeesPageProps) {
  const rootRef = useRef<HTMLElement | null>(null)
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [createRequested, setCreateRequested] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const [autonomyLevels, setAutonomyLevels] = useState<VirployeeAutonomyLevel[]>(FALLBACK_AUTONOMY_LEVELS)
  const [autonomyHelpHost, setAutonomyHelpHost] = useState<HTMLElement | null>(null)
  const [selectedAutonomy, setSelectedAutonomy] = useState<VirployeeAutonomy>('A1')
  const [usesDefaultAutonomy, setUsesDefaultAutonomy] = useState(true)
  const isActive = Boolean(tenantId && principalId)
  const autonomyByLevel = useMemo(() => {
    return new Map(autonomyLevels.map((level) => [level.level, level]))
  }, [autonomyLevels])
  const autonomyOptions = useMemo(() => {
    return autonomyLevels.map((level) => ({
      label: `${level.level} - ${level.name}`,
      value: level.level,
    }))
  }, [autonomyLevels])

  const dataSource: NonNullable<CrudPageProps<Virployee>['dataSource']> = useMemo(() => ({
    list: ({ view }) => isActive ? listVirployees(view, tenantId, principalId) : Promise.resolve([]),
    create: async (values) => {
      await createVirployee(virployeePayload(values), tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
    update: async (row, values) => {
      await updateVirployee(row.id, virployeePayload(values), tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
    archive: async (row) => {
      await archiveVirployee(row.id, tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
    trash: async (row) => {
      await trashVirployee(row.id, tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
    unarchive: async (row) => {
      await unarchiveVirployee(row.id, tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
    restore: async (row) => {
      await restoreVirployee(row.id, tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
    purge: async (row) => {
      await purgeVirployee(row.id, tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
  }), [isActive, principalId, tenantId])

  useEffect(() => {
    setSelectedIds([])
    setActionError('')
  }, [lifecycleView, tenantId])

  useEffect(() => {
    if (!isActive) {
      setAutonomyLevels(FALLBACK_AUTONOMY_LEVELS)
      return
    }
    let cancelled = false
    listVirployeeAutonomyLevels(tenantId, principalId)
      .then((levels) => {
        if (cancelled) return
        const visible = levels.filter((level) => VISIBLE_AUTONOMY_LEVELS.includes(level.level))
        setAutonomyLevels(visible.length > 0 ? visible : FALLBACK_AUTONOMY_LEVELS)
      })
      .catch(() => {
        if (!cancelled) setAutonomyLevels(FALLBACK_AUTONOMY_LEVELS)
      })
    return () => {
      cancelled = true
    }
  }, [isActive, principalId, tenantId])

  useEffect(() => {
    if (!createRequested) return
    const handle = window.setTimeout(() => {
      const buttons = Array.from(
        rootRef.current?.querySelectorAll<HTMLButtonElement>(
          '.crud-page-shell__header-actions > .actions-row > .actions-row > button',
        ) ?? [],
      )
      buttons.find((button) => button.textContent?.trim() === 'New')?.click()
      setCreateRequested(false)
    }, 0)
    return () => window.clearTimeout(handle)
  }, [createRequested, reloadVersion])

  useEffect(() => {
    const root = rootRef.current
    if (!root) return

    const syncAutonomyHelp = () => {
      const select = root.querySelector<HTMLSelectElement>('#crud-field-autonomy')
      if (!select) {
        setAutonomyHelpHost(null)
        setSelectedAutonomy('A1')
        setUsesDefaultAutonomy(true)
        return
      }
      const raw = select.value.trim()
      setSelectedAutonomy(isAutonomy(raw) ? raw : 'A1')
      setUsesDefaultAutonomy(raw === '')

      const fieldGroup = select.closest('.form-group')
      if (!fieldGroup) {
        setAutonomyHelpHost(null)
        return
      }
      let host = root.querySelector<HTMLElement>('#virployee-autonomy-help-host')
      if (!host) {
        host = document.createElement('div')
        host.id = 'virployee-autonomy-help-host'
        host.className = 'virployee-autonomy-help-host full-width'
      }
      if (host.previousElementSibling !== fieldGroup) {
        fieldGroup.insertAdjacentElement('afterend', host)
      }
      setAutonomyHelpHost(host)
    }

    const handleChange = (event: Event) => {
      const target = event.target
      if (target instanceof HTMLSelectElement && target.id === 'crud-field-autonomy') {
        syncAutonomyHelp()
      }
    }

    const observer = new MutationObserver(syncAutonomyHelp)
    observer.observe(root, { childList: true, subtree: true })
    root.addEventListener('change', handleChange)
    syncAutonomyHelp()

    return () => {
      observer.disconnect()
      root.removeEventListener('change', handleChange)
    }
  }, [lifecycleView, reloadVersion, tenantId])

  const toggleSelected = (id: string, checked: boolean) => {
    setSelectedIds((current) => (
      checked ? Array.from(new Set([...current, id])) : current.filter((item) => item !== id)
    ))
  }

  const clearSelected = () => setSelectedIds([])

  const setExternalLifecycleView = (view: CrudLifecycleView) => {
    setLifecycleView(view)
    clearSelected()
    setActionError('')
  }

  const applyBulkAction = async (action: BulkAction) => {
    if (!isActive || selectedIds.length === 0 || bulkBusy) return
    setBulkBusy(true)
    setActionError('')
    try {
      for (const id of selectedIds) {
        if (action === 'archive') {
          await archiveVirployee(id, tenantId, principalId)
        } else if (action === 'trash') {
          await trashVirployee(id, tenantId, principalId)
        } else if (action === 'restore') {
          if (lifecycleView === 'archived') {
            await unarchiveVirployee(id, tenantId, principalId)
          } else {
            await restoreVirployee(id, tenantId, principalId)
          }
        } else {
          await purgeVirployee(id, tenantId, principalId)
        }
      }
      clearSelected()
      setReloadVersion((current) => current + 1)
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Could not run the action')
    } finally {
      setBulkBusy(false)
    }
  }

  if (!isActive) {
    return (
      <section className="page-section">
        <div className="empty-state">Select an active tenant to manage Virployees.</div>
      </section>
    )
  }

  return (
    <section ref={rootRef} className="page-section iam-control axis-crud-host iam-control--external-lifecycle">
      <CrudPage<Virployee>
        key={`virployees-${tenantId}-${lifecycleView}-${reloadVersion}`}
        dataSource={dataSource}
        stringsBase={defaultCrudStrings}
        strings={{
          actionTrash: 'Trash',
          actionPurge: 'Delete permanently',
          confirmWord: 'delete',
        }}
        initialView={lifecycleView}
        supportsArchived
        supportsTrash
        allowCreate
        allowEdit
        allowArchive
        allowTrash
        allowUnarchive
        allowRestore
        allowPurge
        label="virployee"
        labelPlural="virployees"
        labelPluralCap="Virployees"
        createLabel="New"
        columns={virployeeColumns(selectedIds, toggleSelected, autonomyByLevel)}
        formFields={virployeeFormFields(autonomyOptions)}
        searchText={(row) => virployeeSearchText(row, autonomyByLevel)}
        toFormValues={virployeeToFormValues}
        isValid={isValidVirployeeForm}
        emptyState="No virployees"
        archivedEmptyState="No archived virployees"
        trashEmptyState="No virployees in trash"
        searchPlaceholder="Search virployees"
        listHeaderInlineSlot={() => (
          <div className="iam-control__lead-stack">
            <CreateAndBulkActions
              selectedCount={selectedIds.length}
              view={lifecycleView}
              busy={bulkBusy || !isActive}
              onCreate={() => setCreateRequested(true)}
              onClear={clearSelected}
              onBulkAction={(action) => void applyBulkAction(action)}
            />
            {actionError ? <p role="alert" className="iam-control__inline-error">{actionError}</p> : null}
          </div>
        )}
        toolbarActions={lifecycleToolbarActions(lifecycleView, setExternalLifecycleView)}
        featureFlags={{ csvToolbar: false }}
      />
      {autonomyHelpHost ? createPortal(
        <AutonomyDetails
          definition={autonomyByLevel.get(selectedAutonomy)}
          usesDefault={usesDefaultAutonomy}
        />,
        autonomyHelpHost,
      ) : null}
    </section>
  )
}

function virployeeColumns(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
  autonomyByLevel: ReadonlyMap<VirployeeAutonomy, VirployeeAutonomyLevel>,
): CrudPageProps<Virployee>['columns'] {
  return [
    selectionColumn<Virployee>(selectedIds, onToggle),
    { key: 'name', header: 'Name' },
    { key: 'role', header: 'Role' },
    { key: 'autonomy', header: 'Autonomy', render: (value) => formatAutonomy(String(value ?? ''), autonomyByLevel) },
    { key: 'supervisor_user_id', header: 'Supervisor', render: (value) => shortId(String(value ?? '')) },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
    { key: 'updated_at', header: 'Updated', render: (value) => formatDate(String(value ?? '')) },
  ]
}

function virployeeFormFields(
  autonomyOptions: Array<{ label: string; value: string }>,
): CrudPageProps<Virployee>['formFields'] {
  const defaultAutonomy = autonomyOptions.find((option) => option.value === 'A1')?.label ?? 'A1 - Recommendation'
  return [
    { key: 'name', label: 'Name' },
    { key: 'role', label: 'Role' },
    {
      key: 'autonomy',
      label: 'Autonomy (optional)',
      type: 'select' as const,
      placeholder: `Default: ${defaultAutonomy}`,
      options: autonomyOptions,
    },
    {
      key: 'supervisor_user_id',
      label: 'Supervisor User ID',
      placeholder: 'Example: 11111111-1111-4111-8111-111111111111',
      fullWidth: true,
    },
    { key: 'description', label: 'Description (optional)', type: 'textarea' as const, rows: 3, fullWidth: true },
  ]
}

function AutonomyDetails(props: {
  definition: VirployeeAutonomyLevel | undefined
  usesDefault: boolean
}) {
  const definition = props.definition ?? FALLBACK_AUTONOMY_LEVELS[1]
  return (
    <div className="virployee-autonomy-help">
      <div className="virployee-autonomy-help__header">
        <strong>{definition.level} - {definition.name}</strong>
        {props.usesDefault ? <span>Default</span> : null}
      </div>
      <p>{definition.description}</p>
      <div className="virployee-autonomy-help__actions" aria-label="Allowed action classes">
        <span>Allowed:</span>
        {definition.allowed_action_classes.length > 0 ? (
          definition.allowed_action_classes.map((action) => (
            <span key={action.class} className="virployee-autonomy-help__chip">
              {action.name}
            </span>
          ))
        ) : (
          <span className="virployee-autonomy-help__empty">None</span>
        )}
      </div>
    </div>
  )
}

function virployeeToFormValues(row: Virployee): CrudFormValues {
  return {
    name: row.name,
    role: row.role,
    autonomy: row.autonomy ?? 'A1',
    description: row.description ?? '',
    supervisor_user_id: row.supervisor_user_id,
  }
}

function virployeePayload(values: CrudFormValues) {
  return {
    name: stringValue(values.name),
    role: stringValue(values.role),
    description: stringValue(values.description),
    supervisor_user_id: stringValue(values.supervisor_user_id),
    autonomy: autonomyValue(values.autonomy),
  }
}

function isValidVirployeeForm(values: CrudFormValues): boolean {
  return (
    stringValue(values.name).length > 0 &&
    stringValue(values.role).length > 0 &&
    isUUID(stringValue(values.supervisor_user_id))
  )
}

function virployeeSearchText(
  row: Virployee,
  autonomyByLevel: ReadonlyMap<VirployeeAutonomy, VirployeeAutonomyLevel>,
): string {
  return [
    row.id,
    row.name,
    row.role,
    row.autonomy,
    formatAutonomy(row.autonomy, autonomyByLevel),
    row.description,
    row.supervisor_user_id,
    row.state,
  ].join(' ')
}

function selectionColumn<T extends { id: string }>(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
): NonNullable<CrudPageProps<T>['columns']>[number] {
  return {
    key: 'id' as keyof T & string,
    header: '',
    sortable: false,
    className: 'iam-control__select-col',
    render: (_value: unknown, row: T) => (
      <input
        type="checkbox"
        aria-label={`Select ${row.id}`}
        checked={selectedIds.includes(row.id)}
        onClick={(event) => event.stopPropagation()}
        onChange={(event) => onToggle(row.id, event.currentTarget.checked)}
      />
    ),
  }
}

function CreateAndBulkActions(props: {
  selectedCount: number
  view: CrudLifecycleView
  busy: boolean
  onCreate: () => void
  onClear: () => void
  onBulkAction: (action: BulkAction) => void
}) {
  const actionsDisabled = props.busy || props.selectedCount === 0
  return (
    <div className="iam-control__create-inline">
      <div className="iam-control__bulk-buttons">
        <button
          type="button"
          className="iam-control__new-button"
          disabled={props.busy && props.selectedCount === 0}
          onClick={props.onCreate}
        >
          New
        </button>
        {props.view === 'active' ? (
          <>
            <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('archive')}>Archive</button>
            <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('trash')}>Trash</button>
          </>
        ) : null}
        {props.view === 'archived' ? (
          <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restore</button>
        ) : null}
        {props.view === 'trash' ? (
          <>
            <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restore</button>
            <button
              type="button"
              className="iam-control__danger-button"
              disabled={actionsDisabled}
              onClick={() => props.onBulkAction('purge')}
            >
              Delete
            </button>
          </>
        ) : null}
        <button type="button" disabled={actionsDisabled} onClick={props.onClear}>Clear</button>
      </div>
      <span className="iam-control__selected-count">{props.selectedCount} selected</span>
    </div>
  )
}

function lifecycleToolbarActions(view: CrudLifecycleView, onChange: (view: CrudLifecycleView) => void) {
  return [
    { id: 'active', label: 'Active', kind: view === 'active' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('active') },
    { id: 'archived', label: 'Archived', kind: view === 'archived' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('archived') },
    { id: 'trash', label: 'Trash', kind: view === 'trash' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('trash') },
  ]
}

function stringValue(value: CrudFormValues[string]): string {
  return String(value ?? '').trim()
}

function autonomyValue(value: CrudFormValues[string]): VirployeeAutonomy {
  const autonomy = stringValue(value)
  return isAutonomy(autonomy) ? autonomy : 'A1'
}

function isAutonomy(value: string): value is VirployeeAutonomy {
  return ['A0', 'A1', 'A2', 'A3', 'A4', 'A5'].includes(value)
}

function formatAutonomy(
  value: string,
  autonomyByLevel: ReadonlyMap<VirployeeAutonomy, VirployeeAutonomyLevel>,
): string {
  if (!isAutonomy(value)) return value || '-'
  const definition = autonomyByLevel.get(value)
  return definition ? `${value} - ${definition.name}` : value
}

function isUUID(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value)
}

function shortId(value: string): string {
  if (!value) return '-'
  return value.length > 14 ? `${value.slice(0, 8)}...${value.slice(-4)}` : value
}

function formatState(value: string): string {
  if (value === 'active') return 'Active'
  if (value === 'archived') return 'Archived'
  if (value === 'trashed') return 'Trash'
  return value || '-'
}

function formatDate(value: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('en-US', { dateStyle: 'short', timeStyle: 'short' })
}
