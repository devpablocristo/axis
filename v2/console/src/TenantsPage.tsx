import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactElement } from 'react'
import {
  type Product,
  type Tenant,
  type TenantInput,
  archiveTenant,
  createTenant,
  listProducts,
  listTenants,
  purgeTenant,
  restoreTenant,
  trashTenant,
  unarchiveTenant,
  updateTenant,
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
  const rootRef = useRef<HTMLElement | null>(null)
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [createRequested, setCreateRequested] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const [products, setProducts] = useState<Product[]>([])
  const [productsError, setProductsError] = useState('')
  const isActive = Boolean(principalId)

  const orgLabels = useMemo(() => {
    const labels = new Map<string, string>()
    for (const tenant of sessionTenants) {
      if (!labels.has(tenant.org_id)) {
        labels.set(tenant.org_id, tenant.org_name || tenant.org_id)
      }
    }
    return labels
  }, [sessionTenants])
  const orgIDByLabel = useMemo(() => {
    const index = new Map<string, string>()
    for (const [id, label] of orgLabels.entries()) {
      index.set(label.trim().toLowerCase(), id)
    }
    return index
  }, [orgLabels])
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

  const refreshAfterMutation = useCallback(async () => {
    await onSessionChanged()
    setReloadVersion((current) => current + 1)
  }, [onSessionChanged])

  const dataSource: NonNullable<CrudPageProps<Tenant>['dataSource']> = useMemo(() => ({
    list: ({ view }) => isActive ? listTenants(view, principalId) : Promise.resolve([]),
    create: async (values) => {
      await createTenant(tenantPayload(values, orgIDByLabel), principalId)
      setCreateOpen(false)
      await refreshAfterMutation()
    },
    update: async (row, values) => {
      await updateTenant(row.id, { org_name: stringValue(values.org_name) }, principalId)
      await refreshAfterMutation()
    },
    archive: async (row) => {
      await archiveTenant(row.id, principalId)
      await refreshAfterMutation()
    },
    trash: async (row) => {
      await trashTenant(row.id, principalId)
      await refreshAfterMutation()
    },
    unarchive: async (row) => {
      await unarchiveTenant(row.id, principalId)
      await refreshAfterMutation()
    },
    restore: async (row) => {
      await restoreTenant(row.id, principalId)
      await refreshAfterMutation()
    },
    purge: async (row) => {
      await purgeTenant(row.id, principalId)
      await refreshAfterMutation()
    },
  }), [isActive, orgIDByLabel, principalId, refreshAfterMutation])

  useEffect(() => {
    if (!principalId) return
    let cancelled = false
    setProductsError('')
    listProducts(principalId)
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
  }, [principalId])

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
    <section ref={rootRef} className="page-section iam-control axis-crud-host iam-control--external-lifecycle tenants-control">
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
        label="tenant"
        labelPlural="tenants"
        labelPluralCap="Tenants"
        createLabel="New"
        columns={tenantColumns(selectedIds, toggleSelected, orgLabels, productLabels)}
        formFields={tenantFormFields(orgOptions, productOptions)}
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
              createOpen={createOpen}
              busy={bulkBusy || !isActive || products.length === 0}
              onCreate={() => {
                setCreateOpen(true)
                setCreateRequested(true)
              }}
              onClear={clearSelected}
              onBulkAction={(action) => void applyBulkAction(action)}
            />
            {productsError ? <p role="alert" className="iam-control__inline-error">{productsError}</p> : null}
            {actionError ? <p role="alert" className="iam-control__inline-error">{actionError}</p> : null}
          </div>
        )}
        toolbarActions={lifecycleToolbarActions(lifecycleView, createOpen, setExternalLifecycleView)}
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
    { key: 'updated_at', header: 'Updated', render: (value) => formatDate(String(value ?? '')) },
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
      key: 'org_name',
      label: 'New org',
      type: 'text' as const,
      createOnly: true,
      placeholder: 'Org name',
    },
    {
      key: 'org_name',
      label: 'Org',
      type: 'text' as const,
      editOnly: true,
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
    org_name: row.org_name,
    product_surface: row.product_surface,
  }
}

function tenantPayload(values: CrudFormValues, orgIDByLabel: Map<string, string>): TenantInput {
  const orgName = stringValue(values.org_name)
  const selectedOrgID = stringValue(values.org_id)
  const existingOrgID = orgIDByLabel.get(orgName.toLowerCase()) || ''
  if (selectedOrgID && selectedOrgID !== createOrgOptionValue) {
    return {
      org_id: selectedOrgID,
      product_surface: stringValue(values.product_surface),
    }
  }
  if (existingOrgID) {
    return {
      org_id: existingOrgID,
      product_surface: stringValue(values.product_surface),
    }
  }
  return {
    org_name: orgName,
    product_surface: stringValue(values.product_surface),
  }
}

function isValidTenantForm(values: CrudFormValues): boolean {
  const hasProduct = stringValue(values.product_surface).length > 0
  if (!hasProduct) return false
  const selectedOrgID = stringValue(values.org_id)
  if (selectedOrgID && selectedOrgID !== createOrgOptionValue) return true
  return stringValue(values.org_name).length > 0
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

const createOrgOptionValue = '__create_org__'

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
    </div>
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

function formatDate(value: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('en-US', { dateStyle: 'short', timeStyle: 'short' })
}
