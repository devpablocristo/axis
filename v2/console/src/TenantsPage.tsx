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
  type Product,
  type Tenant,
  type TenantInput,
  archiveTenant,
  createTenant,
  listOrgs,
  listProducts,
  listTenants,
  purgeTenant,
  restoreTenant,
  trashTenant,
  unarchiveTenant,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type TenantsPageProps = {
  principalId: string
  sessionTenants: Tenant[]
  onSessionChanged: () => void | Promise<void>
}

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function TenantsPage({ principalId, sessionTenants, onSessionChanged }: TenantsPageProps) {
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [formMode, setFormMode] = useState<'create' | null>(null)
  const [formValues, setFormValues] = useState<CrudFormValues>({})
  const [formSaving, setFormSaving] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const [orgs, setOrgs] = useState<AxisOrg[]>([])
  const [orgsError, setOrgsError] = useState('')
  const [products, setProducts] = useState<Product[]>([])
  const [productsError, setProductsError] = useState('')
  const isActive = Boolean(principalId)

  const orgLabels = useMemo(() => {
    const labels = new Map<string, string>()
    for (const org of orgs) {
      labels.set(org.id, org.name || org.id)
    }
    for (const tenant of sessionTenants) {
      if (!labels.has(tenant.org_id)) {
        labels.set(tenant.org_id, tenant.org_name || tenant.org_id)
      }
    }
    return labels
  }, [orgs, sessionTenants])
  const orgOptions = useMemo(() => orgSelectOptions(orgLabels), [orgLabels])
  const productOptions = useMemo(() => products.map((product) => ({
    label: product.name || product.product_surface,
    value: product.product_surface,
  })), [products])
  const productLabels = useMemo(() => {
    const labels = new Map<string, string>()
    for (const product of products) {
      labels.set(product.product_surface, product.name || product.product_surface)
    }
    return labels
  }, [products])
  const formFields = useMemo(() => tenantFormFields(orgOptions, productOptions), [orgOptions, productOptions])

  const refreshAfterMutation = useCallback(async () => {
    await onSessionChanged()
    setReloadVersion((current) => current + 1)
  }, [onSessionChanged])

  const dataSource: NonNullable<CrudPageProps<Tenant>['dataSource']> = useMemo(() => ({
    list: () => isActive ? listTenants(lifecycleView, principalId) : Promise.resolve([]),
  }), [isActive, lifecycleView, principalId])

  useEffect(() => {
    if (!principalId) return
    let cancelled = false
    setOrgsError('')
    setProductsError('')
    listOrgs('active', principalId)
      .then((next) => {
        if (!cancelled) setOrgs(next)
      })
      .catch((error) => {
        if (!cancelled) {
          setOrgsError(error instanceof Error ? error.message : 'Could not load orgs')
          setOrgs([])
        }
      })
    listProducts('active', principalId)
      .then((next) => {
        if (!cancelled) setProducts(next)
      })
      .catch((error) => {
        if (!cancelled) {
          setProductsError(error instanceof Error ? error.message : 'Could not load products')
          setProducts([])
        }
      })
    return () => {
      cancelled = true
    }
  }, [principalId, reloadVersion])

  useEffect(() => {
    setSelectedIds([])
    closeForm()
    setActionError('')
  }, [lifecycleView, principalId])

  const toggleSelected = (id: string, checked: boolean) => {
    setSelectedIds((current) => (
      checked ? Array.from(new Set([...current, id])) : current.filter((item) => item !== id)
    ))
  }

  const clearSelected = () => setSelectedIds([])

  const setExternalLifecycleView = (view: CrudLifecycleView) => {
    setLifecycleView(view)
    closeForm()
    clearSelected()
    setActionError('')
  }

  const openCreate = () => {
    setFormMode('create')
    setFormValues(emptyFormValues<Tenant>(formFields))
    setActionError('')
  }

  function closeForm() {
    setFormMode(null)
    setFormValues({})
    setFormSaving(false)
  }

  const submitForm = async () => {
    if (!isActive || !formMode || !isValidTenantForm(formValues) || formSaving) return
    setFormSaving(true)
    setActionError('')
    try {
      await createTenant(tenantPayload(formValues), principalId)
      closeForm()
      clearSelected()
      await refreshAfterMutation()
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Could not create the tenant')
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
          await archiveTenant(id, principalId)
        } else if (action === 'trash') {
          await trashTenant(id, principalId)
        } else if (action === 'restore') {
          if (lifecycleView === 'archived') {
            await unarchiveTenant(id, principalId)
          } else {
            await restoreTenant(id, principalId)
          }
        } else {
          await purgeTenant(id, principalId)
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
        <div className="empty-state">Sign in to manage Tenants.</div>
      </section>
    )
  }

  return (
    <section className="page-section iam-control axis-crud-host tenants-control">
      <CrudPage<Tenant>
        key={`tenants-${principalId}-${lifecycleView}-${reloadVersion}`}
        dataSource={dataSource}
        stringsBase={defaultCrudStrings}
        strings={{
          actionSave: 'Create',
          actionTrash: 'Trash',
          actionPurge: 'Delete permanently',
          formCreate: 'Create {{label}}',
          confirmWord: 'delete',
        }}
        supportsArchived={false}
        supportsTrash={false}
        allowCreate={false}
        allowArchive={false}
        allowTrash={false}
        allowUnarchive={false}
        allowRestore={false}
        allowPurge={false}
        label="tenant"
        labelPlural="tenants"
        labelPluralCap="Tenants"
        createLabel="New"
        columns={tenantColumns(selectedIds, toggleSelected, orgLabels, productLabels)}
        formFields={formFields}
        searchText={tenantSearchText}
        toFormValues={tenantToFormValues}
        isValid={isValidTenantForm}
        emptyState="No tenants"
        archivedEmptyState="No archived tenants"
        trashEmptyState="No tenants in trash"
        searchPlaceholder="Search tenants"
        listHeaderInlineSlot={() => (
          <div className="iam-control__lead-stack">
            <CreateAndBulkActions
              selectedCount={selectedIds.length}
              view={lifecycleView}
              createOpen={formMode === 'create'}
              busy={bulkBusy || formSaving || !isActive || products.length === 0 || orgOptions.length === 0}
              onCreate={openCreate}
              onClear={clearSelected}
              onBulkAction={(action) => void applyBulkAction(action)}
            />
            {orgsError ? <p role="alert" className="iam-control__inline-error">{orgsError}</p> : null}
            {productsError ? <p role="alert" className="iam-control__inline-error">{productsError}</p> : null}
            {actionError ? <p role="alert" className="iam-control__inline-error">{actionError}</p> : null}
            {formMode ? (
              <EntityFormPanel<Tenant>
                title="New tenant"
                mode="create"
                fields={formFields}
                values={formValues}
                saving={formSaving}
                primaryLabel="Create"
                valid={isValidTenantForm(formValues)}
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

function tenantColumns(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
  orgLabels: Map<string, string>,
  productLabels: Map<string, string>,
): CrudPageProps<Tenant>['columns'] {
  return [
    selectionColumn<Tenant>(selectedIds, onToggle),
    { key: 'org_name', header: 'Org', render: (_value, row) => row.org_name || orgLabels.get(row.org_id) || row.org_id },
    { key: 'product_surface', header: 'Product', render: (value) => productLabels.get(String(value ?? '')) || String(value || '-') },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
  ]
}

function tenantFormFields(
  orgOptions: Array<{ label: string; value: string }>,
  productOptions: Array<{ label: string; value: string }>,
): CrudPageProps<Tenant>['formFields'] {
  return [
    {
      key: 'org_id',
      label: 'Org',
      type: 'select' as const,
      placeholder: 'Select...',
      createOnly: true,
      options: orgOptions,
    },
    {
      key: 'product_surface',
      label: 'Product',
      type: 'select' as const,
      placeholder: 'Select...',
      createOnly: true,
      options: productOptions,
    },
  ]
}

function tenantToFormValues(row: Tenant): CrudFormValues {
  return {
    org_id: row.org_id,
    product_surface: row.product_surface,
  }
}

function tenantPayload(values: CrudFormValues): TenantInput {
  const selectedOrgID = stringValue(values.org_id)
  return {
    org_id: selectedOrgID,
    product_surface: stringValue(values.product_surface),
  }
}

function isValidTenantForm(values: CrudFormValues): boolean {
  const hasOrg = stringValue(values.org_id).length > 0
  const hasProduct = stringValue(values.product_surface).length > 0
  return hasOrg && hasProduct
}

function tenantSearchText(row: Tenant): string {
  return [
    row.id,
    row.org_id,
    row.org_name,
    row.product_surface,
    row.status,
    row.state,
  ].join(' ')
}

function orgSelectOptions(labels: Map<string, string>): Array<{ label: string; value: string }> {
  return Array.from(labels.entries())
    .map(([value, label]) => ({ label, value }))
    .sort((left, right) => left.label.localeCompare(right.label))
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
  createOpen: boolean
  busy: boolean
  onCreate: () => void
  onClear: () => void
  onBulkAction: (action: BulkAction) => void
}) {
  return (
    <LifecycleBulkActions
      selectedCount={props.selectedCount}
      view={props.view}
      createOpen={props.createOpen}
      busy={props.busy}
      onCreate={props.onCreate}
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
