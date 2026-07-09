import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useEffect, useMemo, useState, type ReactElement } from 'react'
import { EntityFormPanel, emptyFormValues } from './EntityFormPanel'
import { LifecycleBulkActions } from './LifecycleBulkActions'
import {
  type JobRole,
  type JobRoleInput,
  archiveJobRole,
  createJobRole,
  listJobRoles,
  purgeJobRole,
  restoreJobRole,
  trashJobRole,
  unarchiveJobRole,
  updateJobRole,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type JobRolesPageProps = {
  tenantId: string
  principalId: string
}

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function JobRolesPage({ tenantId, principalId }: JobRolesPageProps) {
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [selectedRowsById, setSelectedRowsById] = useState<Record<string, JobRole>>({})
  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [formValues, setFormValues] = useState<CrudFormValues>({})
  const [formSaving, setFormSaving] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const isActive = Boolean(tenantId && principalId)
  const formFields = useMemo(() => jobRoleFormFields(), [])
  const selectedRow = selectedIds.length === 1 ? selectedRowsById[selectedIds[0]] ?? null : null

  const dataSource: NonNullable<CrudPageProps<JobRole>['dataSource']> = useMemo(() => ({
    list: () => isActive ? listJobRoles(lifecycleView, tenantId, principalId) : Promise.resolve([]),
  }), [isActive, lifecycleView, principalId, tenantId])

  useEffect(() => {
    setSelectedIds([])
    setSelectedRowsById({})
    closeForm()
    setActionError('')
  }, [lifecycleView, tenantId])

  const toggleSelected = (row: JobRole, checked: boolean) => {
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
    setFormValues(emptyFormValues<JobRole>(formFields))
    setActionError('')
  }

  const openEdit = () => {
    if (!selectedRow) return
    setFormMode('edit')
    setFormValues(jobRoleToFormValues(selectedRow))
    setActionError('')
  }

  function closeForm() {
    setFormMode(null)
    setFormValues({})
    setFormSaving(false)
  }

  const submitForm = async () => {
    if (!isActive || !formMode || !isValidJobRoleForm(formValues) || formSaving) return
    setFormSaving(true)
    setActionError('')
    try {
      if (formMode === 'create') {
        await createJobRole(jobRolePayload(formValues), tenantId, principalId)
      } else if (selectedRow) {
        await updateJobRole(selectedRow.id, jobRolePayload(formValues), tenantId, principalId)
      }
      closeForm()
      clearSelected()
      setReloadVersion((current) => current + 1)
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Could not save the job role')
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
          await archiveJobRole(id, tenantId, principalId)
        } else if (action === 'trash') {
          await trashJobRole(id, tenantId, principalId)
        } else if (action === 'restore') {
          if (lifecycleView === 'archived') {
            await unarchiveJobRole(id, tenantId, principalId)
          } else {
            await restoreJobRole(id, tenantId, principalId)
          }
        } else {
          await purgeJobRole(id, tenantId, principalId)
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
        <div className="empty-state">Select an active tenant to manage Job Roles.</div>
      </section>
    )
  }

  return (
    <section className="page-section iam-control axis-crud-host">
      <CrudPage<JobRole>
        key={`job-roles-${tenantId}-${lifecycleView}-${reloadVersion}`}
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
        label="job role"
        labelPlural="job roles"
        labelPluralCap="Job Roles"
        createLabel="New"
        columns={jobRoleColumns(selectedIds, toggleSelected)}
        formFields={formFields}
        searchText={jobRoleSearchText}
        toFormValues={jobRoleToFormValues}
        isValid={isValidJobRoleForm}
        emptyState="No Job Roles"
        archivedEmptyState="No archived Job Roles"
        trashEmptyState="No Job Roles in trash"
        searchPlaceholder="Search Job Roles"
        listHeaderInlineSlot={() => (
          <div className="iam-control__lead-stack">
            <CreateAndBulkActions
              selectedCount={selectedIds.length}
              view={lifecycleView}
              createOpen={formMode === 'create'}
              editOpen={formMode === 'edit'}
              busy={bulkBusy || formSaving || !isActive}
              onCreate={openCreate}
              onEdit={openEdit}
              onClear={clearSelected}
              onBulkAction={(action) => void applyBulkAction(action)}
            />
            {actionError ? <p role="alert" className="iam-control__inline-error">{actionError}</p> : null}
            {formMode ? (
              <EntityFormPanel<JobRole>
                title={formMode === 'create' ? 'New job role' : 'Edit job role'}
                mode={formMode}
                fields={formFields}
                values={formValues}
                saving={formSaving}
                primaryLabel={formMode === 'create' ? 'Create' : 'Save'}
                valid={isValidJobRoleForm(formValues)}
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

function jobRoleColumns(
  selectedIds: string[],
  onToggle: (row: JobRole, checked: boolean) => void,
): CrudPageProps<JobRole>['columns'] {
  return [
    selectionColumn<JobRole>(selectedIds, onToggle),
    { key: 'name', header: 'Name' },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
  ]
}

function jobRoleFormFields(): CrudPageProps<JobRole>['formFields'] {
  return [
    { key: 'name', label: 'Name' },
    { key: 'mission', label: 'Mission (optional)', type: 'textarea' as const, rows: 3, fullWidth: true },
  ]
}

function jobRoleToFormValues(row: JobRole): CrudFormValues {
  return {
    name: row.name,
    mission: row.mission ?? '',
  }
}

function jobRolePayload(values: CrudFormValues): JobRoleInput {
  return {
    name: stringValue(values.name),
    mission: stringValue(values.mission),
  }
}

function isValidJobRoleForm(values: CrudFormValues): boolean {
  return stringValue(values.name).length > 0
}

function jobRoleSearchText(row: JobRole): string {
  return [
    row.id,
    row.name,
    row.slug,
    row.mission,
    row.state,
  ].join(' ')
}

function selectionColumn<T extends JobRole>(
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
      onCreate={props.onCreate}
      onEdit={props.onEdit}
      onClear={props.onClear}
      onBulkAction={props.onBulkAction}
    />
  )
}

function lifecycleToolbarActions(view: CrudLifecycleView, createOpen: boolean, onChange: (view: CrudLifecycleView) => void) {
  return [
    { id: 'active', label: 'Active', kind: !createOpen && view === 'active' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('active') },
    { id: 'archived', label: 'Archived', kind: !createOpen && view === 'archived' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('archived') },
    { id: 'trash', label: 'Trash', kind: !createOpen && view === 'trash' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('trash') },
  ]
}

function stringValue(value: CrudFormValues[string]): string {
  return String(value ?? '').trim()
}

function formatState(value: string): string {
  if (value === 'active') return 'Active'
  if (value === 'archived') return 'Archived'
  if (value === 'trashed') return 'Trash'
  return value || '-'
}
