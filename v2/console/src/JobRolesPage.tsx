import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useEffect, useMemo, useState, type ReactElement } from 'react'
import { LifecycleBulkActions } from './LifecycleBulkActions'
import { crudPrimaryStickyColumn, crudSelectionStickyColumn } from './crudTableColumns'
import { formatDateTime24 } from './formatters'
import {
  type JobRole,
  type JobRoleInput,
  type JobRoleResponsibility,
  type JobRoleSuccessCriterion,
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
  const [responsibilities, setResponsibilities] = useState<JobRoleResponsibility[]>([])
  const [successCriteria, setSuccessCriteria] = useState<JobRoleSuccessCriterion[]>([])
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
    setFormValues({ name: '', mission: '' })
    setResponsibilities([])
    setSuccessCriteria([])
    setActionError('')
  }

  const openEdit = () => {
    if (!selectedRow) return
    setFormMode('edit')
    setFormValues(jobRoleToFormValues(selectedRow))
    setResponsibilities(selectedRow.responsibilities ?? [])
    setSuccessCriteria(selectedRow.success_criteria ?? [])
    setActionError('')
  }

  function closeForm() {
    setFormMode(null)
    setFormValues({})
    setResponsibilities([])
    setSuccessCriteria([])
    setFormSaving(false)
  }

  const submitForm = async () => {
    if (!isActive || !formMode || !isValidJobRoleDefinition(formValues, responsibilities, successCriteria) || formSaving) return
    setFormSaving(true)
    setActionError('')
    try {
      if (formMode === 'create') {
        await createJobRole(jobRolePayload(formValues, responsibilities, successCriteria), tenantId, principalId)
      } else if (selectedRow) {
        await updateJobRole(selectedRow.id, jobRolePayload(formValues, responsibilities, successCriteria), tenantId, principalId)
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
              <JobRoleDefinitionPanel
                title={formMode === 'create' ? 'New job role' : 'Edit job role'}
                values={formValues}
                responsibilities={responsibilities}
                successCriteria={successCriteria}
                saving={formSaving}
                primaryLabel={formMode === 'create' ? 'Create' : 'Save'}
                valid={isValidJobRoleDefinition(formValues, responsibilities, successCriteria)}
                onChange={setFormValues}
                onResponsibilitiesChange={setResponsibilities}
                onSuccessCriteriaChange={setSuccessCriteria}
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
    { key: 'name', header: 'Name', className: 'iam-control__primary-col', ...crudPrimaryStickyColumn },
    { key: 'responsibilities', header: 'Responsibilities', render: (_value, row) => String(row.responsibilities?.length ?? 0) },
    { key: 'success_criteria', header: 'Success criteria', render: (_value, row) => String(row.success_criteria?.length ?? 0) },
    { key: 'created_at', header: 'Created', className: 'iam-control__created-col', render: (value) => formatDateTime24(String(value ?? '')) },
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

function jobRolePayload(
  values: CrudFormValues,
  responsibilities: JobRoleResponsibility[],
  successCriteria: JobRoleSuccessCriterion[],
): JobRoleInput {
  return {
    name: stringValue(values.name),
    mission: stringValue(values.mission),
    responsibilities: responsibilities.map((item, index) => ({
      ...item,
      title: item.title.trim(),
      description: item.description.trim(),
      expected_outcome: item.expected_outcome.trim(),
      priority: index + 1,
    })),
    success_criteria: successCriteria.map((item, index) => ({
      ...item,
      title: item.title.trim(),
      description: item.description.trim(),
      target_value: item.target_value.trim(),
      priority: index + 1,
    })),
  }
}

function isValidJobRoleForm(values: CrudFormValues): boolean {
  return stringValue(values.name).length > 0
}

function isValidJobRoleDefinition(
  values: CrudFormValues,
  responsibilities: JobRoleResponsibility[],
  successCriteria: JobRoleSuccessCriterion[],
): boolean {
  return isValidJobRoleForm(values)
    && responsibilities.every((item) => item.title.trim().length > 0)
    && successCriteria.every((item) => item.title.trim().length > 0)
}

function jobRoleSearchText(row: JobRole): string {
  return [
    row.id,
    row.name,
    row.slug,
    row.mission,
    ...(row.responsibilities ?? []).flatMap((item) => [item.title, item.description, item.expected_outcome]),
    ...(row.success_criteria ?? []).flatMap((item) => [item.title, item.description, item.target_value]),
    row.state,
  ].join(' ')
}

function JobRoleDefinitionPanel(props: {
  title: string
  values: CrudFormValues
  responsibilities: JobRoleResponsibility[]
  successCriteria: JobRoleSuccessCriterion[]
  saving: boolean
  primaryLabel: string
  valid: boolean
  onChange: (values: CrudFormValues) => void
  onResponsibilitiesChange: (items: JobRoleResponsibility[]) => void
  onSuccessCriteriaChange: (items: JobRoleSuccessCriterion[]) => void
  onSubmit: () => void
  onCancel: () => void
}) {
  const updateValue = (key: string, value: string) => props.onChange({ ...props.values, [key]: value })

  return (
    <div className="card crud-form-card job-role-definition-panel">
      <div className="card-header"><h2>{props.title}</h2></div>
      <form onSubmit={(event) => { event.preventDefault(); props.onSubmit() }}>
        <div className="crud-form-grid">
          <label className="form-group">
            Name
            <input value={String(props.values.name ?? '')} onChange={(event) => updateValue('name', event.currentTarget.value)} />
          </label>
          <label className="form-group full-width">
            Mission
            <textarea rows={3} value={String(props.values.mission ?? '')} onChange={(event) => updateValue('mission', event.currentTarget.value)} />
          </label>
        </div>

        <DefinitionRows
          title="Responsibilities"
          addLabel="Add responsibility"
          items={props.responsibilities}
          fields={[
            { key: 'title', label: 'Title' },
            { key: 'description', label: 'Description' },
            { key: 'expected_outcome', label: 'Expected outcome' },
          ]}
          onChange={props.onResponsibilitiesChange}
          createItem={() => ({ title: '', description: '', expected_outcome: '', priority: props.responsibilities.length + 1 })}
        />

        <DefinitionRows
          title="Success criteria"
          addLabel="Add criterion"
          items={props.successCriteria}
          fields={[
            { key: 'title', label: 'Title' },
            { key: 'description', label: 'Description' },
            { key: 'target_value', label: 'Target value' },
          ]}
          onChange={props.onSuccessCriteriaChange}
          createItem={() => ({ title: '', description: '', target_value: '', priority: props.successCriteria.length + 1 })}
        />

        <footer className="virployee-edit-form__footer">
          <button type="submit" className="btn-primary" disabled={props.saving || !props.valid}>
            {props.saving ? 'Saving...' : props.primaryLabel}
          </button>
          <button type="button" className="btn-secondary" disabled={props.saving} onClick={props.onCancel}>Cancel</button>
        </footer>
      </form>
    </div>
  )
}

function DefinitionRows<T extends { priority: number }>(props: {
  title: string
  addLabel: string
  items: T[]
  fields: Array<{ key: keyof T & string; label: string }>
  onChange: (items: T[]) => void
  createItem: () => T
}) {
  return (
    <section className="job-role-definition-list" aria-label={props.title}>
      <div className="card-header">
        <h3>{props.title}</h3>
        <button type="button" className="btn-secondary" onClick={() => props.onChange([...props.items, props.createItem()])}>
          {props.addLabel}
        </button>
      </div>
      {props.items.length === 0 ? <p className="axis-muted">None configured.</p> : null}
      {props.items.map((item, index) => (
        <div key={index} className="job-role-definition-row">
          <span className="job-role-definition-row__order">{index + 1}</span>
          {props.fields.map((field) => (
            <label key={field.key} className="form-group">
              {field.label}
              <input
                value={String(item[field.key] ?? '')}
                onChange={(event) => {
                  const next = [...props.items]
                  next[index] = { ...item, [field.key]: event.currentTarget.value, priority: index + 1 }
                  props.onChange(next)
                }}
              />
            </label>
          ))}
          <button
            type="button"
            className="btn-secondary"
            onClick={() => props.onChange(props.items.filter((_current, itemIndex) => itemIndex !== index))}
          >
            Remove
          </button>
        </div>
      ))}
    </section>
  )
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
    ...crudSelectionStickyColumn,
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
