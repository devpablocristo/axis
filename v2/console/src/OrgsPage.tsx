import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useCallback, useEffect, useMemo, useState, type ReactElement } from 'react'
import { EntityFormPanel, emptyFormValues } from './EntityFormPanel'
import { LifecycleBulkActions } from './LifecycleBulkActions'
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
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [selectedRowsById, setSelectedRowsById] = useState<Record<string, AxisOrg>>({})
  const [lockedOrgIds, setLockedOrgIds] = useState<Set<string>>(() => new Set())
  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [formValues, setFormValues] = useState<CrudFormValues>({})
  const [formSaving, setFormSaving] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const isActive = Boolean(principalId)
  const formFields = useMemo(() => orgFormFields(), [])
  const selectedRow = selectedIds.length === 1 ? selectedRowsById[selectedIds[0]] ?? null : null

  const refreshAfterMutation = useCallback(async () => {
    setReloadVersion((current) => current + 1)
    await onSessionChanged()
  }, [onSessionChanged])

  const dataSource: NonNullable<CrudPageProps<AxisOrg>['dataSource']> = useMemo(() => ({
    list: async () => {
      if (!isActive) {
        setLockedOrgIds(new Set())
        return []
      }
      const rows = await listOrgs(lifecycleView, principalId)
      setLockedOrgIds(new Set(rows.filter(isOrgLifecycleLocked).map((row) => row.id)))
      setSelectedIds((current) => current.filter((id) => !rows.some((row) => row.id === id && isOrgLifecycleLocked(row))))
      setSelectedRowsById((current) => Object.fromEntries(
        Object.entries(current).filter(([id]) => rows.some((row) => row.id === id && !isOrgLifecycleLocked(row))),
      ))
      return rows
    },
  }), [isActive, lifecycleView, principalId])

  useEffect(() => {
    setSelectedIds([])
    setSelectedRowsById({})
    closeForm()
    setActionError('')
  }, [lifecycleView, principalId])

  const toggleSelected = (row: AxisOrg, checked: boolean) => {
    setSelectedRowsById((current) => {
      const next = { ...current }
      if (checked) next[row.id] = row
      else delete next[row.id]
      return next
    })
    setSelectedIds((current) => (
      checked ? Array.from(new Set([...current, row.id])) : current.filter((item) => item !== row.id)
    ))
  }

  const clearSelected = () => {
    setSelectedIds([])
    setSelectedRowsById({})
  }

  const setExternalLifecycleView = (view: CrudLifecycleView) => {
    setLifecycleView(view)
    closeForm()
    clearSelected()
    setActionError('')
  }

  const openCreate = () => {
    setFormMode('create')
    setFormValues(emptyFormValues<AxisOrg>(formFields))
    setActionError('')
  }

  const openEdit = () => {
    if (!selectedRow) return
    setFormMode('edit')
    setFormValues(orgToFormValues(selectedRow))
    setActionError('')
  }

  function closeForm() {
    setFormMode(null)
    setFormValues({})
    setFormSaving(false)
  }

  const submitForm = async () => {
    if (!isActive || !formMode || !isValidOrgForm(formValues) || formSaving) return
    setFormSaving(true)
    setActionError('')
    try {
      if (formMode === 'create') {
        await createOrg(orgPayload(formValues), principalId)
      } else if (selectedRow) {
        await updateOrg(selectedRow.id, orgPayload(formValues), principalId)
      }
      closeForm()
      clearSelected()
      await refreshAfterMutation()
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Could not save the org')
    } finally {
      setFormSaving(false)
    }
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
    <section className="page-section iam-control axis-crud-host">
      <CrudPage<AxisOrg>
        key={`orgs-${principalId}-${lifecycleView}-${reloadVersion}`}
        dataSource={dataSource}
        stringsBase={defaultCrudStrings}
        strings={{
          actionTrash: 'Trash',
          actionPurge: 'Delete permanently',
          confirmWord: 'delete',
        }}
        supportsArchived={false}
        supportsTrash={false}
        allowCreate={false}
        allowEdit={false}
        allowArchive={false}
        allowTrash={false}
        allowUnarchive={false}
        allowRestore={false}
        allowPurge={false}
        label="org"
        labelPlural="orgs"
        labelPluralCap="Orgs"
        createLabel="New"
        columns={orgColumns(selectedIds, toggleSelected)}
        formFields={formFields}
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
              createOpen={formMode === 'create'}
              editOpen={formMode === 'edit'}
              busy={bulkBusy || formSaving || !isActive}
              blockedCount={lockedOrgIds.size}
              onCreate={openCreate}
              onEdit={openEdit}
              onClear={clearSelected}
              onBulkAction={(action) => void applyBulkAction(action)}
            />
            {actionError ? <p role="alert" className="iam-control__inline-error">{actionError}</p> : null}
            {formMode ? (
              <EntityFormPanel<AxisOrg>
                title={formMode === 'create' ? 'New org' : 'Edit org'}
                mode={formMode}
                fields={formFields}
                values={formValues}
                saving={formSaving}
                primaryLabel={formMode === 'create' ? 'Create' : 'Save'}
                valid={isValidOrgForm(formValues)}
                onChange={setFormValues}
                onSubmit={() => void submitForm()}
                onCancel={closeForm}
              />
            ) : null}
          </div>
        )}
        toolbarActions={lifecycleToolbarActions(lifecycleView, formMode != null, setExternalLifecycleView)}
        featureFlags={{ csvToolbar: false }}
      />
    </section>
  )
}

function orgColumns(
  selectedIds: string[],
  onToggle: (row: AxisOrg, checked: boolean) => void,
): CrudPageProps<AxisOrg>['columns'] {
  return [
    selectionColumn<AxisOrg>(selectedIds, onToggle),
    { key: 'name', header: 'Org' },
    { key: 'tenant_count', header: 'Tenants', render: (value) => Number(value || 0) },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
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
  onToggle: (row: T, checked: boolean) => void,
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
        onChange={(event) => onToggle(row, event.currentTarget.checked)}
      />
    ),
  }
}

function CreateAndBulkActions(props: {
  selectedCount: number
  view: CrudLifecycleView
  createOpen: boolean
  editOpen: boolean
  busy: boolean
  blockedCount: number
  onCreate: () => void
  onEdit: () => void
  onClear: () => void
  onBulkAction: (action: BulkAction) => void
}) {
  return (
    <LifecycleBulkActions
      selectedCount={props.selectedCount}
      view={props.view}
      createOpen={props.createOpen}
      editOpen={props.editOpen}
      busy={props.busy}
      blockedMessage={props.view === 'active' && props.blockedCount > 0 ? 'Orgs with tenants cannot be archived or deleted.' : undefined}
      onCreate={props.onCreate}
      onEdit={props.onEdit}
      onClear={props.onClear}
      onBulkAction={props.onBulkAction}
    />
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

function formatState(value: string): string {
  if (value === 'trashed') return 'Trash'
  if (value === 'archived') return 'Archived'
  return 'Active'
}

function stringValue(value: unknown): string {
  return String(value ?? '').trim()
}
