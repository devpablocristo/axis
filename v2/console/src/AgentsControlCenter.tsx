import {
  CrudPage as PlatformCrudPage,
  crudStringsEs,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useEffect, useMemo, useRef, useState, type ReactElement } from 'react'
import {
  type Virployee,
  archiveVirployee,
  createVirployee,
  listVirployees,
  purgeVirployee,
  restoreVirployee,
  trashVirployee,
  unarchiveVirployee,
  updateVirployee,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type AgentsControlCenterProps = {
  tenantId: string
  principalId: string
}

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function AgentsControlCenter({ tenantId, principalId }: AgentsControlCenterProps) {
  const rootRef = useRef<HTMLElement | null>(null)
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [createRequested, setCreateRequested] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const isActive = Boolean(tenantId && principalId)

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
    if (!createRequested) return
    const handle = window.setTimeout(() => {
      const buttons = Array.from(
        rootRef.current?.querySelectorAll<HTMLButtonElement>(
          '.crud-page-shell__header-actions > .actions-row > .actions-row > button',
        ) ?? [],
      )
      buttons.find((button) => button.textContent?.trim() === 'Nuevo')?.click()
      setCreateRequested(false)
    }, 0)
    return () => window.clearTimeout(handle)
  }, [createRequested, reloadVersion])

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
      setActionError(error instanceof Error ? error.message : 'No se pudo ejecutar la acción')
    } finally {
      setBulkBusy(false)
    }
  }

  if (!isActive) {
    return (
      <section className="page-section">
        <div className="empty-state">Seleccioná un tenant activo para administrar Virployees.</div>
      </section>
    )
  }

  return (
    <section ref={rootRef} className="page-section iam-control axis-crud-host iam-control--external-lifecycle">
      <CrudPage<Virployee>
        key={`virployees-${tenantId}-${lifecycleView}-${reloadVersion}`}
        dataSource={dataSource}
        stringsBase={crudStringsEs}
        strings={{
          actionTrash: 'Papelera',
          actionPurge: 'Eliminar definitivo',
          confirmWord: 'eliminar',
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
        createLabel="Nuevo"
        columns={virployeeColumns(selectedIds, toggleSelected)}
        formFields={virployeeFormFields()}
        searchText={virployeeSearchText}
        toFormValues={virployeeToFormValues}
        isValid={isValidVirployeeForm}
        emptyState="Sin virployees"
        archivedEmptyState="Sin virployees archivados"
        trashEmptyState="Sin virployees en papelera"
        searchPlaceholder="Buscar virployees"
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
    </section>
  )
}

function virployeeColumns(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
): CrudPageProps<Virployee>['columns'] {
  return [
    selectionColumn<Virployee>(selectedIds, onToggle),
    { key: 'name', header: 'Nombre' },
    { key: 'role', header: 'Role' },
    { key: 'supervisor_user_id', header: 'Supervisor', render: (value) => shortId(String(value ?? '')) },
    { key: 'state', header: 'Estado', render: (value) => formatState(String(value ?? '')) },
    { key: 'updated_at', header: 'Actualizado', render: (value) => formatDate(String(value ?? '')) },
  ]
}

function virployeeFormFields(): CrudPageProps<Virployee>['formFields'] {
  return [
    { key: 'name', label: 'Nombre', required: true },
    { key: 'role', label: 'Role', required: true },
    {
      key: 'supervisor_user_id',
      label: 'Supervisor User ID',
      required: true,
      placeholder: 'Ej: 11111111-1111-4111-8111-111111111111',
      fullWidth: true,
    },
    { key: 'description', label: 'Descripción', type: 'textarea' as const, rows: 3, fullWidth: true },
  ]
}

function virployeeToFormValues(row: Virployee): CrudFormValues {
  return {
    name: row.name,
    role: row.role,
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
  }
}

function isValidVirployeeForm(values: CrudFormValues): boolean {
  return (
    stringValue(values.name).length > 0 &&
    stringValue(values.role).length > 0 &&
    isUUID(stringValue(values.supervisor_user_id))
  )
}

function virployeeSearchText(row: Virployee): string {
  return [
    row.id,
    row.name,
    row.role,
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
        aria-label={`Seleccionar ${row.id}`}
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
          Nuevo
        </button>
        {props.view === 'active' ? (
          <>
            <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('archive')}>Archivar</button>
            <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('trash')}>Papelera</button>
          </>
        ) : null}
        {props.view === 'archived' ? (
          <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restaurar</button>
        ) : null}
        {props.view === 'trash' ? (
          <>
            <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restaurar</button>
            <button
              type="button"
              className="iam-control__danger-button"
              disabled={actionsDisabled}
              onClick={() => props.onBulkAction('purge')}
            >
              Eliminar
            </button>
          </>
        ) : null}
        <button type="button" disabled={actionsDisabled} onClick={props.onClear}>Limpiar</button>
      </div>
      <span className="iam-control__selected-count">{props.selectedCount} seleccionados</span>
    </div>
  )
}

function lifecycleToolbarActions(view: CrudLifecycleView, onChange: (view: CrudLifecycleView) => void) {
  return [
    { id: 'active', label: 'Activos', kind: view === 'active' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('active') },
    { id: 'archived', label: 'Archivados', kind: view === 'archived' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('archived') },
    { id: 'trash', label: 'Papelera', kind: view === 'trash' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('trash') },
  ]
}

function stringValue(value: CrudFormValues[string]): string {
  return String(value ?? '').trim()
}

function isUUID(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value)
}

function shortId(value: string): string {
  if (!value) return '-'
  return value.length > 14 ? `${value.slice(0, 8)}...${value.slice(-4)}` : value
}

function formatState(value: string): string {
  if (value === 'active') return 'Activo'
  if (value === 'archived') return 'Archivado'
  if (value === 'trashed') return 'Papelera'
  return value || '-'
}

function formatDate(value: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('es-AR', { dateStyle: 'short', timeStyle: 'short' })
}
