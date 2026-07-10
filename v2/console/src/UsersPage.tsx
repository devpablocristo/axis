import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useEffect, useMemo, useState, type ReactElement } from 'react'
import { EntityFormPanel, emptyFormValues } from './EntityFormPanel'
import { LifecycleBulkActions } from './LifecycleBulkActions'
import { crudPrimaryStickyColumn, crudSelectionStickyColumn } from './crudTableColumns'
import { formatDateTime24 } from './formatters'
import {
  type TenantUser,
  type TenantUserInput,
  type TenantUserRole,
  archiveUser,
  createUser,
  listUsers,
  purgeUser,
  restoreUser,
  trashUser,
  unarchiveUser,
  updateUser,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type UsersPageProps = {
  tenantId: string
  principalId: string
}

const USER_ROLES: Array<{ label: string; value: TenantUserRole }> = [
  { label: 'Owner', value: 'owner' },
  { label: 'Admin', value: 'admin' },
  { label: 'Member', value: 'member' },
]

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function UsersPage({ tenantId, principalId }: UsersPageProps) {
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [selectedRowsById, setSelectedRowsById] = useState<Record<string, TenantUser>>({})
  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [formValues, setFormValues] = useState<CrudFormValues>({})
  const [formSaving, setFormSaving] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const isActive = Boolean(tenantId && principalId)
  const formFields = useMemo(() => userFormFields(), [])
  const selectedRow = selectedIds.length === 1 ? selectedRowsById[selectedIds[0]] ?? null : null

  const dataSource: NonNullable<CrudPageProps<TenantUser>['dataSource']> = useMemo(() => ({
    list: () => isActive ? listUsers(lifecycleView, tenantId, principalId) : Promise.resolve([]),
  }), [isActive, lifecycleView, principalId, tenantId])

  useEffect(() => {
    setSelectedIds([])
    setSelectedRowsById({})
    closeForm()
    setActionError('')
  }, [lifecycleView, tenantId])

  const toggleSelected = (row: TenantUser, checked: boolean) => {
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
    setFormValues({ ...emptyFormValues<TenantUser>(formFields), role: 'member' })
    setActionError('')
  }

  const openEdit = () => {
    if (!selectedRow) return
    setFormMode('edit')
    setFormValues(userToFormValues(selectedRow))
    setActionError('')
  }

  function closeForm() {
    setFormMode(null)
    setFormValues({})
    setFormSaving(false)
  }

  const submitForm = async () => {
    if (!isActive || !formMode || !isValidUserForm(formValues) || formSaving) return
    setFormSaving(true)
    setActionError('')
    try {
      if (formMode === 'create') {
        await createUser(userPayload(formValues), tenantId, principalId)
      } else if (selectedRow) {
        await updateUser(selectedRow.id, userPayload(formValues), tenantId, principalId)
      }
      closeForm()
      clearSelected()
      setReloadVersion((current) => current + 1)
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Could not save the user')
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
          await archiveUser(id, tenantId, principalId)
        } else if (action === 'trash') {
          await trashUser(id, tenantId, principalId)
        } else if (action === 'restore') {
          if (lifecycleView === 'archived') {
            await unarchiveUser(id, tenantId, principalId)
          } else {
            await restoreUser(id, tenantId, principalId)
          }
        } else {
          await purgeUser(id, tenantId, principalId)
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
        <div className="empty-state">Select an active tenant to manage Users.</div>
      </section>
    )
  }

  return (
    <section className="page-section iam-control axis-crud-host">
      <CrudPage<TenantUser>
        key={`users-${tenantId}-${lifecycleView}-${reloadVersion}`}
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
        label="user"
        labelPlural="users"
        labelPluralCap="Users"
        createLabel="New"
        columns={userColumns(selectedIds, toggleSelected)}
        formFields={formFields}
        searchText={userSearchText}
        toFormValues={userToFormValues}
        isValid={isValidUserForm}
        emptyState="No users"
        archivedEmptyState="No archived users"
        trashEmptyState="No users in trash"
        searchPlaceholder="Search users"
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
              <EntityFormPanel<TenantUser>
                title={formMode === 'create' ? 'New user' : 'Edit user'}
                mode={formMode}
                fields={formFields}
                values={formValues}
                saving={formSaving}
                primaryLabel={formMode === 'create' ? 'Create' : 'Save'}
                valid={isValidUserForm(formValues)}
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

function userColumns(
  selectedIds: string[],
  onToggle: (row: TenantUser, checked: boolean) => void,
): CrudPageProps<TenantUser>['columns'] {
  return [
    selectionColumn<TenantUser>(selectedIds, onToggle),
    { key: 'email', header: 'Email', className: 'iam-control__primary-col', ...crudPrimaryStickyColumn },
    { key: 'created_at', header: 'Created', className: 'iam-control__created-col', render: (value) => formatDateTime24(String(value ?? '')) },
    { key: 'role', header: 'Role', render: (value) => formatRole(String(value ?? '')) },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
  ]
}

function userFormFields(): CrudPageProps<TenantUser>['formFields'] {
  return [
    { key: 'email', label: 'Email' },
    {
      key: 'role',
      label: 'Role',
      type: 'select' as const,
      placeholder: 'Member',
      options: USER_ROLES,
    },
  ]
}

function userToFormValues(row: TenantUser): CrudFormValues {
  return {
    email: row.email,
    role: row.role || 'member',
  }
}

function userPayload(values: CrudFormValues): TenantUserInput {
  return {
    email: stringValue(values.email).toLowerCase(),
    role: roleValue(values.role),
  }
}

function isValidUserForm(values: CrudFormValues): boolean {
  return isEmail(stringValue(values.email)) && isUserRole(stringValue(values.role) || 'member')
}

function userSearchText(row: TenantUser): string {
  return [
    row.id,
    row.kind,
    row.email,
    row.role,
    row.state,
  ].join(' ')
}

function selectionColumn<T extends TenantUser>(
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

function roleValue(value: CrudFormValues[string]): TenantUserRole {
  const role = stringValue(value)
  return isUserRole(role) ? role : 'member'
}

function isUserRole(value: string): value is TenantUserRole {
  return value === 'owner' || value === 'admin' || value === 'member'
}

function isEmail(value: string): boolean {
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)
}

function formatRole(value: string): string {
  if (value === 'owner') return 'Owner'
  if (value === 'admin') return 'Admin'
  if (value === 'member') return 'Member'
  return value || '-'
}

function formatState(value: string): string {
  if (value === 'pending') return 'Pending'
  if (value === 'active') return 'Active'
  if (value === 'archived') return 'Archived'
  if (value === 'trashed') return 'Trash'
  return value || '-'
}
