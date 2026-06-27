import {
  CrudPage as PlatformCrudPage,
  crudStringsEs,
  type CrudHttpClient,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import type { ReactElement } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { axisCrudHttpClient } from './api'

type IAMCrudResource = 'tenants' | 'users'
type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'
type IAMUserScope = 'axis' | 'tenant'

type AxisTenantView = {
  id: string
  name: string
  status: string
}

type IAMUserView = {
  id: string
  email: string
  role: string
  status: string
  scope: IAMUserScope
  org_id?: string
  tenant_id?: string
}

type SelectedRows = Record<IAMCrudResource, string[]>
type LifecycleViews = Record<IAMCrudResource, CrudLifecycleView>
type TenantRow = AxisTenantView

type IAMControlCenterProps = {
  orgId: string
  tenantId: string
  productSurface: string
  orgs: Array<{ id: string; name?: string; status?: string }>
  onOrgChange: (orgId: string) => void
  onRefreshShell: () => Promise<void>
}

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

const ROLE_OPTIONS = [
  { label: 'Owner', value: 'owner' },
  { label: 'Admin', value: 'admin' },
  { label: 'Member', value: 'member' },
]

export function IAMControlCenter(props: IAMControlCenterProps) {
  const rootRef = useRef<HTMLElement | null>(null)
  // Persist the active tab so changing org/product never bounces it back to Orgs.
  const [activeCrud, setActiveCrud] = useState<IAMCrudResource>(
    () => (localStorage.getItem('axis.iam.tab') as IAMCrudResource) || 'tenants',
  )
  const [lifecycleViews, setLifecycleViews] = useState<LifecycleViews>({ tenants: 'active', users: 'active' })
  const [selected, setSelected] = useState<SelectedRows>({ tenants: [], users: [] })
  const [createRequested, setCreateRequested] = useState<IAMCrudResource | null>(null)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [selectedUserOrgId, setSelectedUserOrgId] = useState('axis')

  useEffect(() => {
    localStorage.setItem('axis.iam.tab', activeCrud)
  }, [activeCrud])

  // El org lo gobierna el selector global (izquierda del avatar): el CRUD de
  // usuarios sigue al orgId global en vez de tener su propio selector.
  useEffect(() => {
    setSelectedUserOrgId(props.orgId)
  }, [props.orgId])

  // El tenant activo no entra en la query ni en el orgId, pero sí en el
  // X-Tenant-ID que manda axisFetch. Al cambiar, forzar refetch para re-listar
  // los items del nuevo tenant (sin tocar la tab activa).
  useEffect(() => {
    setReloadVersion((current) => current + 1)
  }, [props.productSurface, props.tenantId])

  useEffect(() => {
    setSelected((current) => ({ ...current, users: [] }))
  }, [selectedUserOrgId])

  useEffect(() => {
    if (!createRequested) return
    const handle = window.setTimeout(() => {
      const section = rootRef.current?.querySelector<HTMLElement>(`[data-iam-crud-section="${createRequested}"]`)
      const buttons = Array.from(section?.querySelectorAll<HTMLButtonElement>('.crud-page-shell__header-actions > .actions-row > .actions-row > button') ?? [])
      buttons.find((button) => button.textContent?.trim() === 'Nuevo')?.click()
      setCreateRequested(null)
    }, 0)
    return () => window.clearTimeout(handle)
  }, [createRequested, reloadVersion])

  const tenantsActive = true
  const usersActive = true

  const tenantsTitle = 'Orgs'
  const usersTitle = 'Usuarios'

  const crudClient = useMemo(() => axisCrudHttpClient(props.orgId, props.tenantId), [props.orgId, props.tenantId])

  const toggleSelected = (resource: IAMCrudResource, id: string, checked: boolean) => {
    setSelected((current) => {
      const currentIds = current[resource]
      const nextIds = checked ? Array.from(new Set([...currentIds, id])) : currentIds.filter((item) => item !== id)
      return { ...current, [resource]: nextIds }
    })
  }

  const clearSelected = (resource: IAMCrudResource) => {
    setSelected((current) => ({ ...current, [resource]: [] }))
  }

  const setResourceLifecycleView = (resource: IAMCrudResource, view: CrudLifecycleView) => {
    setLifecycleViews((current) => ({ ...current, [resource]: view }))
    clearSelected(resource)
  }

  const applyBulkAction = async (resource: IAMCrudResource, action: BulkAction, active: boolean) => {
    const ids = selected[resource]
    if (!active || ids.length === 0 || bulkBusy) return
    setBulkBusy(true)
    try {
      await applyLocalBulkAction({
        resource,
        action,
        ids,
        orgId: props.orgId,
        tenantId: props.tenantId,
      })
      clearSelected(resource)
      setReloadVersion((current) => current + 1)
    } finally {
      setBulkBusy(false)
    }
  }

  return (
    <section ref={rootRef} className="page-section iam-control iam-control--external-lifecycle axis-crud-host">

      <nav className="screen-nav" aria-label="IAM CRUDs">
        {[
          ['tenants', 'Orgs'],
          ['users', 'Usuarios'],
        ].map(([id, label]) => (
          <button key={id} type="button" className={activeCrud === id ? 'active' : ''} onClick={() => setActiveCrud(id as IAMCrudResource)}>
            {label}
          </button>
        ))}
      </nav>

      <div className="iam-control__sections">
        {activeCrud === 'tenants' && (
          <ContextCrudSection<TenantRow>
            resource="tenants"
            title={tenantsTitle}
            active={tenantsActive}
            lifecycleView={lifecycleViews.tenants}
            selectedIds={selected.tenants}
            bulkBusy={bulkBusy}
            reloadVersion={reloadVersion}
            httpClient={crudClient}
            label="org"
            labelPlural="orgs"
            createLabel="Nueva"
            columns={[
              selectionColumn<TenantRow>(selected.tenants, (id, checked) => toggleSelected('tenants', id, checked)),
              { key: 'name', header: 'Nombre' },
              { key: 'status', header: 'Estado', render: (value) => formatStatus(String(value ?? '')) },
            ]}
            formFields={[{ key: 'name', label: 'Nombre', required: true }]}
            searchText={(row) => [row.name, row.id].join(' ')}
            toFormValues={(row) => ({ name: row.name })}
            toBody={(values) => ({ name: stringValue(values.name) })}
            isValid={(values) => stringValue(values.name).length > 0}
            emptyState="Sin orgs"
            archivedEmptyState="Sin orgs archivadas"
            trashEmptyState="Sin orgs en papelera"
            searchPlaceholder="Buscar orgs"
            onCreate={() => setCreateRequested('tenants')}
            onClear={() => clearSelected('tenants')}
            onBulkAction={(action) => void applyBulkAction('tenants', action, tenantsActive)}
            onLifecycleChange={(view) => setResourceLifecycleView('tenants', view)}
          />
        )}

        {activeCrud === 'users' && (
          <ContextCrudSection<IAMUserView>
            resource="users"
            title={usersTitle}
            active={usersActive}
            lifecycleView={lifecycleViews.users}
            selectedIds={selected.users}
            bulkBusy={bulkBusy}
            reloadVersion={reloadVersion}
            httpClient={crudClient}
            listQuery={`org_id=${encodeURIComponent(selectedUserOrgId)}`}
            label="usuario"
            labelPlural="usuarios"
            createLabel="Nuevo"
            columns={userColumns(selected.users, (id, checked) => toggleSelected('users', id, checked))}
            formFields={userFormFields(selectedUserOrgId)}
            searchText={(row) => [row.email, row.role, row.id].join(' ')}
            toFormValues={(row) => ({ email: row.email, role: row.role || 'member' })}
            toBody={(values) => ({ email: stringValue(values.email), role: roleValue(values.role), org_id: selectedUserOrgId })}
            isValid={(values) => {
              return stringValue(values.email).length > 0 && roleValue(values.role).length > 0
            }}
            emptyState="Sin usuarios"
            archivedEmptyState="Sin usuarios archivados"
            trashEmptyState="Sin usuarios en papelera"
            searchPlaceholder="Buscar usuarios"
            onCreate={() => setCreateRequested('users')}
            onClear={() => clearSelected('users')}
            onBulkAction={(action) => void applyBulkAction('users', action, usersActive)}
            onLifecycleChange={(view) => setResourceLifecycleView('users', view)}
          />
        )}
      </div>
    </section>
  )
}

function ContextCrudSection<T extends { id: string; status: string }>(props: {
  resource: IAMCrudResource
  title: string
  active: boolean
  lifecycleView: CrudLifecycleView
  selectedIds: string[]
  bulkBusy: boolean
  reloadVersion: number
  httpClient: CrudHttpClient
  listQuery?: string
  label: string
  labelPlural: string
  createLabel: string
  columns: CrudPageProps<T>['columns']
  formFields: CrudPageProps<T>['formFields']
  searchText: (row: T) => string
  toFormValues: (row: T) => CrudFormValues
  toBody: (values: CrudFormValues) => Record<string, unknown>
  isValid: (values: CrudFormValues) => boolean
  emptyState: string
  archivedEmptyState: string
  trashEmptyState: string
  searchPlaceholder: string
  headerControlSlot?: ReactElement
  belowActionsSlot?: ReactElement
  externalSearch?: string
  onCreate: () => void
  onClear: () => void
  onBulkAction: (action: BulkAction) => void
  onLifecycleChange: (view: CrudLifecycleView) => void
}) {
  return (
    <div className="iam-control__crud-section" data-iam-crud-section={props.resource}>
      <CrudPage<T>
        key={`${props.resource}-${props.title}-${props.lifecycleView}-${props.reloadVersion}-${props.listQuery ?? ''}`}
        basePath={`/api/iam/${props.resource}`}
        listQuery={props.listQuery}
        httpClient={props.httpClient}
        stringsBase={crudStringsEs}
        strings={{ actionTrash: 'Papelera' }}
        initialView={props.lifecycleView}
        supportsArchived
        supportsTrash
        allowCreate={props.active}
        allowEdit={props.active}
        allowArchive={props.active}
        allowTrash={props.active}
        allowUnarchive={props.active}
        allowRestore={props.active}
        allowPurge={props.active}
        label={props.label}
        labelPlural={props.labelPlural}
        labelPluralCap={props.title}
        createLabel={props.createLabel}
        columns={props.columns}
        formFields={props.formFields}
        searchText={props.searchText}
        toFormValues={props.toFormValues}
        toBody={props.toBody}
        isValid={(values) => props.active && props.isValid(values)}
        emptyState={props.emptyState}
        archivedEmptyState={props.archivedEmptyState}
        trashEmptyState={props.trashEmptyState}
        searchPlaceholder={props.searchPlaceholder}
        listHeaderInlineSlot={() => (
          <div className={props.headerControlSlot ? 'iam-control__lead-stack iam-control__lead-stack--with-controls' : 'iam-control__lead-stack'}>
            {props.headerControlSlot}
            <CreateAndBulkActions
              createLabel={props.createLabel}
              selectedCount={props.selectedIds.length}
              view={props.lifecycleView}
              busy={props.bulkBusy || !props.active}
              onCreate={props.onCreate}
              onClear={props.onClear}
              onBulkAction={props.onBulkAction}
            />
            {props.belowActionsSlot}
          </div>
        )}
        toolbarActions={iamLifecycleToolbarActions(props.lifecycleView, props.onLifecycleChange)}
        externalSearch={props.externalSearch}
        featureFlags={{ csvToolbar: false }}
      />
    </div>
  )
}

function selectionColumn<T extends { id: string }>(selectedIds: string[], onToggle: (id: string, checked: boolean) => void) {
  return {
    key: 'id' as keyof T & string,
    header: '',
    sortable: false,
    className: 'iam-control__select-col',
    render: (_value: unknown, row: T) => (
      <input
        type="checkbox"
        aria-label={`Seleccionar ${row.id}`}
        checked={selectedIds.includes(row.id)}
        onClick={(event) => event.stopPropagation()}
        onChange={(event) => onToggle(row.id, event.currentTarget.checked)}
      />
    ),
  }
}

function userColumns(selectedIds: string[], onToggle: (id: string, checked: boolean) => void): CrudPageProps<IAMUserView>['columns'] {
  const columns: CrudPageProps<IAMUserView>['columns'] = [
    selectionColumn<IAMUserView>(selectedIds, onToggle),
    { key: 'email', header: 'Email' },
    { key: 'role', header: 'Rol', render: (value) => formatRole(String(value ?? '')) },
  ]
  columns.push({ key: 'status', header: 'Estado', render: (value) => formatStatus(String(value ?? '')) })
  return columns
}

function userFormFields(selectedUserOrgId: string): CrudPageProps<IAMUserView>['formFields'] {
  return [
    { key: 'email', label: 'Email', type: 'email' as const, required: true },
    {
      key: 'role',
      label: 'Rol',
      type: 'select' as const,
      required: true,
      // 'owner' is a GLOBAL platform role (access to everything), not a per-tenant
      // role — assign it from the axis/global scope, never from a company tenant.
      // Per tenant: admin/member. Global (axis): owner/admin.
      options:
        selectedUserOrgId === 'axis'
          ? ROLE_OPTIONS.filter((option) => option.value !== 'member')
          : ROLE_OPTIONS.filter((option) => option.value !== 'owner'),
    },
  ]
}

function CreateAndBulkActions(props: {
  createLabel: string
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
        <button type="button" className="iam-control__new-button" disabled={props.busy && props.selectedCount === 0} onClick={props.onCreate}>
          {props.createLabel}
        </button>
        {props.view === 'active' && (
          <>
            <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('archive')}>Archivar</button>
            <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('trash')}>Papelera</button>
          </>
        )}
        {props.view === 'archived' && (
          <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restaurar</button>
        )}
        {props.view === 'trash' && (
          <>
            <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restaurar</button>
            <button type="button" className="iam-control__danger-button" disabled={actionsDisabled} onClick={() => props.onBulkAction('purge')}>Eliminar</button>
          </>
        )}
        <button type="button" disabled={actionsDisabled} onClick={props.onClear}>Limpiar</button>
      </div>
      <span className="iam-control__selected-count">{props.selectedCount} seleccionados</span>
    </div>
  )
}

function iamLifecycleToolbarActions(view: CrudLifecycleView, onChange: (view: CrudLifecycleView) => void) {
  return [
    {
      id: 'active',
      label: 'Activos',
      kind: view === 'active' ? 'primary' as const : 'secondary' as const,
      onClick: () => onChange('active'),
    },
    {
      id: 'archived',
      label: 'Archivados',
      kind: view === 'archived' ? 'primary' as const : 'secondary' as const,
      onClick: () => onChange('archived'),
    },
    {
      id: 'trash',
      label: 'Papelera',
      kind: view === 'trash' ? 'primary' as const : 'secondary' as const,
      onClick: () => onChange('trash'),
    },
  ]
}

async function applyLocalBulkAction(args: {
  resource: IAMCrudResource
  action: BulkAction
  ids: string[]
  orgId: string
  tenantId: string
}) {
  const client = axisCrudHttpClient(args.orgId, args.tenantId)
  for (const id of args.ids) {
    const method = args.action === 'purge' ? 'DELETE' : 'POST'
    await client.json(`/api/iam/${args.resource}/${id}/${args.action}`, { method, body: {} })
  }
}


function formatStatus(status: string): string {
  switch (status.trim().toLowerCase()) {
    case 'active':
      return 'activo'
    case 'archived':
      return 'archivado'
    case 'trash':
    case 'disabled':
    case 'removed':
    case 'inactive':
      return 'papelera'
    default:
      return status || '-'
  }
}

function formatRole(role: string): string {
  switch (role.trim().toLowerCase()) {
    case 'owner':
      return 'Owner'
    case 'admin':
      return 'Admin'
    case 'member':
      return 'Member'
    default:
      return role || '-'
  }
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function roleValue(value: unknown): string {
  const role = stringValue(value)
  return role || 'member'
}
