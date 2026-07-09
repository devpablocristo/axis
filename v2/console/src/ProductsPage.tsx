import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useCallback, useEffect, useMemo, useState, type ReactElement } from 'react'
import { EntityFormPanel, emptyFormValues } from './EntityFormPanel'
import { LifecycleBulkActions } from './LifecycleBulkActions'
import { formatDateTime24 } from './formatters'
import {
  type Product,
  type ProductInput,
  archiveProduct,
  createProduct,
  listProducts,
  purgeProduct,
  restoreProduct,
  trashProduct,
  unarchiveProduct,
  updateProduct,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type ProductsPageProps = {
  principalId: string
  onSessionChanged: () => void | Promise<void>
}

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function ProductsPage({ principalId, onSessionChanged }: ProductsPageProps) {
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [selectedRowsById, setSelectedRowsById] = useState<Record<string, Product>>({})
  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [formValues, setFormValues] = useState<CrudFormValues>({})
  const [formSaving, setFormSaving] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const isActive = Boolean(principalId)
  const formFields = useMemo(() => productFormFields(), [])
  const selectedRow = selectedIds.length === 1 ? selectedRowsById[selectedIds[0]] ?? null : null

  const refreshAfterMutation = useCallback(async () => {
    setReloadVersion((current) => current + 1)
    await onSessionChanged()
  }, [onSessionChanged])

  const dataSource: NonNullable<CrudPageProps<Product>['dataSource']> = useMemo(() => ({
    list: () => isActive ? listProducts(lifecycleView, principalId) : Promise.resolve([]),
  }), [isActive, lifecycleView, principalId])

  useEffect(() => {
    setSelectedIds([])
    setSelectedRowsById({})
    closeForm()
    setActionError('')
  }, [lifecycleView, principalId])

  const toggleSelected = (row: Product, checked: boolean) => {
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
    setFormValues(emptyFormValues<Product>(formFields))
    setActionError('')
  }

  const openEdit = () => {
    if (!selectedRow) return
    setFormMode('edit')
    setFormValues(productToFormValues(selectedRow))
    setActionError('')
  }

  function closeForm() {
    setFormMode(null)
    setFormValues({})
    setFormSaving(false)
  }

  const submitForm = async () => {
    if (!isActive || !formMode || !isValidProductForm(formValues) || formSaving) return
    setFormSaving(true)
    setActionError('')
    try {
      if (formMode === 'create') {
        await createProduct(productPayload(formValues, true), principalId)
      } else if (selectedRow) {
        await updateProduct(selectedRow.id, productPayload(formValues, false), principalId)
      }
      closeForm()
      clearSelected()
      await refreshAfterMutation()
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Could not save the product')
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
          await archiveProduct(id, principalId)
        } else if (action === 'trash') {
          await trashProduct(id, principalId)
        } else if (action === 'restore') {
          if (lifecycleView === 'archived') {
            await unarchiveProduct(id, principalId)
          } else {
            await restoreProduct(id, principalId)
          }
        } else {
          await purgeProduct(id, principalId)
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
        <div className="empty-state">Sign in to manage Products.</div>
      </section>
    )
  }

  return (
    <section className="page-section iam-control axis-crud-host">
      <CrudPage<Product>
        key={`products-${principalId}-${lifecycleView}-${reloadVersion}`}
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
        label="product"
        labelPlural="products"
        labelPluralCap="Products"
        createLabel="New"
        columns={productColumns(selectedIds, toggleSelected)}
        formFields={formFields}
        searchText={productSearchText}
        toFormValues={productToFormValues}
        isValid={isValidProductForm}
        emptyState="No products"
        archivedEmptyState="No archived products"
        trashEmptyState="No products in trash"
        searchPlaceholder="Search products"
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
              <EntityFormPanel<Product>
                title={formMode === 'create' ? 'New product' : 'Edit product'}
                mode={formMode}
                fields={formFields}
                values={formValues}
                saving={formSaving}
                primaryLabel={formMode === 'create' ? 'Create' : 'Save'}
                valid={isValidProductForm(formValues)}
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

function productColumns(
  selectedIds: string[],
  onToggle: (row: Product, checked: boolean) => void,
): CrudPageProps<Product>['columns'] {
  return [
    selectionColumn<Product>(selectedIds, onToggle),
    { key: 'name', header: 'Product', className: 'iam-control__primary-col' },
    { key: 'created_at', header: 'Created', className: 'iam-control__created-col', render: (value) => formatDateTime24(String(value ?? '')) },
    { key: 'product_surface', header: 'Slug' },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
  ]
}

function productFormFields(): CrudPageProps<Product>['formFields'] {
  return [
    { key: 'name', label: 'Product' },
    { key: 'product_surface', label: 'Slug (optional)', createOnly: true },
  ]
}

function productToFormValues(row: Product): CrudFormValues {
  return {
    name: row.name,
    product_surface: row.product_surface,
  }
}

function productPayload(values: CrudFormValues, includeSlug: boolean): ProductInput {
  const payload: ProductInput = { name: stringValue(values.name) }
  if (includeSlug) {
    const productSurface = stringValue(values.product_surface)
    if (productSurface) payload.product_surface = productSurface
  }
  return payload
}

function isValidProductForm(values: CrudFormValues): boolean {
  return stringValue(values.name).length > 0
}

function productSearchText(row: Product): string {
  return [
    row.id,
    row.name,
    row.product_surface,
    row.status,
    row.state,
  ].join(' ')
}

function selectionColumn<T extends Product>(
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
    {
      id: 'active',
      label: 'Active',
      kind: !createOpen && view === 'active' ? 'primary' as const : 'secondary' as const,
      onClick: () => onChange('active'),
    },
    {
      id: 'archived',
      label: 'Archived',
      kind: !createOpen && view === 'archived' ? 'primary' as const : 'secondary' as const,
      onClick: () => onChange('archived'),
    },
    {
      id: 'trash',
      label: 'Trash',
      kind: !createOpen && view === 'trash' ? 'primary' as const : 'secondary' as const,
      onClick: () => onChange('trash'),
    },
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
