import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactElement } from 'react'
import {
  type AxisOrg,
  type OrgInput,
  archiveOrg,
  createOrg,
  listOrgs,
  purgeOrg,
  restoreOrg,
  trashOrg,
  unarchiveOrg,
  updateOrg,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type OrgsPageProps = {
  principalId: string
  onSessionChanged: () => void | Promise<void>
}

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function OrgsPage({ principalId, onSessionChanged }: OrgsPageProps) {
  const rootRef = useRef<HTMLElement | null>(null)
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [lockedOrgIds, setLockedOrgIds] = useState<Set<string>>(() => new Set())
  const [createRequested, setCreateRequested] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const isActive = Boolean(principalId)

  const refreshAfterMutation = useCallback(async () => {
    setReloadVersion((current) => current + 1)
    await onSessionChanged()
  }, [onSessionChanged])

  const dataSource: NonNullable<CrudPageProps<AxisOrg>['dataSource']> = useMemo(() => ({
    list: async ({ view }) => {
      if (!isActive) {
        setLockedOrgIds(new Set())
        return []
      }
      const rows = await listOrgs(view, principalId)
      setLockedOrgIds(new Set(rows.filter(isOrgLifecycleLocked).map((row) => row.id)))
      setSelectedIds((current) => current.filter((id) => !rows.some((row) => row.id === id && isOrgLifecycleLocked(row))))
      return rows
    },
    create: async (values) => {
      await createOrg(orgPayload(values), principalId)
      setCreateOpen(false)
      await refreshAfterMutation()
    },
    update: async (row, values) => {
      await updateOrg(row.id, orgPayload(values), principalId)
      await refreshAfterMutation()
    },
    archive: async (row) => {
      await archiveOrg(row.id, principalId)
      await refreshAfterMutation()
    },
    trash: async (row) => {
      await trashOrg(row.id, principalId)
      await refreshAfterMutation()
    },
    unarchive: async (row) => {
      await unarchiveOrg(row.id, principalId)
      await refreshAfterMutation()
    },
    restore: async (row) => {
      await restoreOrg(row.id, principalId)
      await refreshAfterMutation()
    },
    purge: async (row) => {
      await purgeOrg(row.id, principalId)
      await refreshAfterMutation()
    },
  }), [isActive, principalId, refreshAfterMutation])

  useEffect(() => {
    setSelectedIds([])
    setCreateOpen(false)
    setActionError('')
  }, [lifecycleView, principalId])

  useEffect(() => {
    if (!createRequested) return
    const handle = window.setTimeout(() => {
      const buttons = Array.from(
        rootRef.current?.querySelectorAll<HTMLButtonElement>(
          '.crud-page-shell__header-actions > .actions-row > .actions-row > button',
        ) ?? [],
      )
      const newButton = buttons.find((button) => button.textContent?.trim() === 'New')
      if (newButton) {
        newButton.click()
      } else {
        setCreateOpen(false)
      }
      setCreateRequested(false)
    }, 0)
    return () => window.clearTimeout(handle)
  }, [createRequested, reloadVersion])

  useEffect(() => {
    const root = rootRef.current
    if (!root) return
    const syncCreateOpen = () => {
      const title = root.querySelector<HTMLElement>('.crud-form-card .card-header h2')
      setCreateOpen(title?.textContent?.trim().toLowerCase().startsWith('new ') ?? false)
    }
    syncCreateOpen()
    const observer = new MutationObserver(syncCreateOpen)
    observer.observe(root, { childList: true, subtree: true })
    return () => observer.disconnect()
  }, [principalId, lifecycleView, reloadVersion])

  const toggleSelected = (id: string, checked: boolean) => {
    setSelectedIds((current) => (
      checked ? Array.from(new Set([...current, id])) : current.filter((item) => item !== id)
    ))
  }

  const clearSelected = () => setSelectedIds([])

  const setExternalLifecycleView = (view: CrudLifecycleView) => {
    setLifecycleView(view)
    setCreateOpen(false)
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
          await archiveOrg(id, principalId)
        } else if (action === 'trash') {
          await trashOrg(id, principalId)
        } else if (action === 'restore') {
          if (lifecycleView === 'archived') {
            await unarchiveOrg(id, principalId)
          } else {
            await restoreOrg(id, principalId)
          }
        } else {
          await purgeOrg(id, principalId)
        }
      }
      clearSelected()
      await refreshAfterMutation()
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Could not run the action')
    } finally {
      setBulkBusy(false)
    }
  }

  if (!isActive) {
    return (
      <section className="page-section">
        <div className="empty-state">Sign in to manage Orgs.</div>
      </section>
    )
  }

  return (
    <section ref={rootRef} className="page-section iam-control axis-crud-host iam-control--external-lifecycle">
      <CrudPage<AxisOrg>
        key={`orgs-${principalId}-${lifecycleView}-${reloadVersion}`}
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
        allowArchive={false}
        allowTrash={false}
        allowUnarchive
        allowRestore
        allowPurge
        label="org"
        labelPlural="orgs"
        labelPluralCap="Orgs"
        createLabel="New"
        columns={orgColumns(selectedIds, toggleSelected)}
        rowActions={orgRowActions(principalId, refreshAfterMutation)}
        formFields={orgFormFields()}
        searchText={orgSearchText}
        toFormValues={orgToFormValues}
        isValid={isValidOrgForm}
        emptyState="No orgs"
        archivedEmptyState="No archived orgs"
        trashEmptyState="No orgs in trash"
        searchPlaceholder="Search orgs"
        listHeaderInlineSlot={() => (
          <div className="iam-control__lead-stack">
            <CreateAndBulkActions
              selectedCount={selectedIds.length}
              view={lifecycleView}
              createOpen={createOpen}
              busy={bulkBusy || !isActive}
              blockedCount={lockedOrgIds.size}
              onCreate={() => {
                setCreateOpen(true)
                setCreateRequested(true)
              }}
              onClear={clearSelected}
              onBulkAction={(action) => void applyBulkAction(action)}
            />
            {actionError ? <p role="alert" className="iam-control__inline-error">{actionError}</p> : null}
          </div>
        )}
        toolbarActions={lifecycleToolbarActions(lifecycleView, createOpen, setExternalLifecycleView)}
        featureFlags={{ csvToolbar: false }}
      />
    </section>
  )
}

function orgColumns(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
): CrudPageProps<AxisOrg>['columns'] {
  return [
    selectionColumn<AxisOrg>(selectedIds, onToggle),
    { key: 'name', header: 'Org' },
    { key: 'tenant_count', header: 'Tenants', render: (value) => Number(value || 0) },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
    { key: 'updated_at', header: 'Updated', render: (value) => formatDate(String(value ?? '')) },
  ]
}

function orgRowActions(
  principalId: string,
  refreshAfterMutation: () => Promise<void>,
): NonNullable<CrudPageProps<AxisOrg>['rowActions']> {
  return [
    {
      id: 'archive',
      label: 'Archive',
      kind: 'secondary',
      isVisible: (row, ctx) => ctx.view === 'active' && !isOrgLifecycleLocked(row),
      onClick: async (row) => {
        await archiveOrg(row.id, principalId)
        await refreshAfterMutation()
      },
    },
    {
      id: 'trash',
      label: 'Trash',
      kind: 'danger',
      isVisible: (row, ctx) => ctx.view === 'active' && !isOrgLifecycleLocked(row),
      onClick: async (row) => {
        await trashOrg(row.id, principalId)
        await refreshAfterMutation()
      },
    },
  ]
}

function orgFormFields(): CrudPageProps<AxisOrg>['formFields'] {
  return [
    { key: 'name', label: 'Org' },
  ]
}

function orgToFormValues(row: AxisOrg): CrudFormValues {
  return { name: row.name }
}

function orgPayload(values: CrudFormValues): OrgInput {
  return { name: stringValue(values.name) }
}

function isValidOrgForm(values: CrudFormValues): boolean {
  return stringValue(values.name).length > 0
}

function orgSearchText(row: AxisOrg): string {
  return [
    row.id,
    row.name,
    row.provider,
    row.provider_org_id,
    row.tenant_count,
    row.status,
    row.state,
  ].join(' ')
}

function selectionColumn<T extends AxisOrg>(
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
        title={isOrgLifecycleLocked(row) ? 'Remove all tenants before archiving or deleting this org.' : undefined}
        disabled={isOrgLifecycleLocked(row)}
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
  createOpen: boolean
  busy: boolean
  blockedCount: number
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
          className={`btn-sm ${props.createOpen ? 'btn-primary' : 'btn-secondary'} iam-control__new-button`}
          onClick={props.onCreate}
        >
          New
        </button>
        {props.view === 'active' ? (
          <>
            <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={() => props.onBulkAction('archive')}>Archive</button>
            <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={() => props.onBulkAction('trash')}>Trash</button>
          </>
        ) : null}
        {props.view === 'archived' ? (
          <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restore</button>
        ) : null}
        {props.view === 'trash' ? (
          <>
            <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restore</button>
            <button
              type="button"
              className="btn-sm btn-danger iam-control__danger-button"
              disabled={actionsDisabled}
              onClick={() => props.onBulkAction('purge')}
            >
              Delete
            </button>
          </>
        ) : null}
        <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={props.onClear}>Clear</button>
      </div>
      <span className="iam-control__selected-count">{props.selectedCount} selected</span>
      {props.view === 'active' && props.blockedCount > 0 ? (
        <span className="iam-control__inline-note">Orgs with tenants cannot be archived or deleted.</span>
      ) : null}
    </div>
  )
}

function isOrgLifecycleLocked(row: AxisOrg): boolean {
  return row.has_tenants || row.tenant_count > 0
}

function lifecycleToolbarActions(view: CrudLifecycleView, createOpen: boolean, onChange: (view: CrudLifecycleView) => void) {
  return [
    { id: 'active', label: 'Active', kind: !createOpen && view === 'active' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('active') },
    { id: 'archived', label: 'Archived', kind: !createOpen && view === 'archived' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('archived') },
    { id: 'trash', label: 'Trash', kind: !createOpen && view === 'trash' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('trash') },
  ]
}

function formatDate(value: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'short',
    timeStyle: 'short',
  }).format(date)
}

function formatState(value: string): string {
  if (value === 'trashed') return 'Trash'
  if (value === 'archived') return 'Archived'
  return 'Active'
}

function stringValue(value: unknown): string {
  return String(value ?? '').trim()
}
