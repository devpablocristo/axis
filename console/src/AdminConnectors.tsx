import {
  CrudPage as PlatformCrudPage,
  crudStringsEs,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import type { ReactElement } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  archiveConnector,
  createConnector,
  listConnectors,
  listConnectorTypes,
  refreshConnector,
  restoreConnector,
  testConnector,
  trashConnector,
  updateConnector,
  type AxisConnectorConfigField,
  type AxisConnectorTypeView,
  type AxisConnectorView,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type ConnectorCrudRow = AxisConnectorView & { id: string }
type ConnectorBulkAction = 'archive' | 'trash' | 'restore'

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function AdminConnectors({ orgId, tenantId }: { orgId: string; tenantId: string }) {
  const rootRef = useRef<HTMLDivElement | null>(null)
  const [view, setView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [createRequested, setCreateRequested] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [types, setTypes] = useState<AxisConnectorTypeView[]>([])
  const [typesError, setTypesError] = useState('')
  const typeByKind = useMemo(() => new Map(types.map((type) => [type.kind, type])), [types])
  const fields = useMemo(() => connectorConfigFields(types), [types])
  const typeOptions = useMemo(() => types.map((type) => ({
    label: `${type.name} · ${type.kind}`,
    value: type.kind,
  })), [types])

  useEffect(() => {
    if (!tenantId) return
    void loadConnectorTypes(orgId, tenantId, setTypes, setTypesError)
  }, [orgId, tenantId])

  useEffect(() => {
    if (!createRequested) return
    const handle = window.setTimeout(() => {
      const buttons = Array.from(rootRef.current?.querySelectorAll<HTMLButtonElement>('.crud-page-shell__header-actions > .actions-row > .actions-row > button') ?? [])
      buttons.find((button) => button.textContent?.trim() === 'Nuevo')?.click()
      setCreateRequested(false)
    }, 0)
    return () => window.clearTimeout(handle)
  }, [createRequested, view])

  if (!tenantId) {
    return <section className="empty-state">Seleccioná un tenant válido para operar Connectors.</section>
  }

  const dataSource: NonNullable<CrudPageProps<ConnectorCrudRow>['dataSource']> = {
    async list({ view }) {
      const rows = await listConnectors(orgId, view, tenantId)
      return rows.map(connectorToRow)
    },
    async create(values) {
      await createConnector(orgId, connectorPayload(values, typeByKind), tenantId)
    },
    async update(row, values) {
      await updateConnector(orgId, row.id, connectorPayload(values, typeByKind, row), tenantId)
    },
    async archive(row) {
      await archiveConnector(orgId, row.id, tenantId)
    },
    async trash(row) {
      await trashConnector(orgId, row.id, tenantId)
    },
    async unarchive(row) {
      await restoreConnector(orgId, row.id, tenantId)
    },
    async restore(row) {
      await restoreConnector(orgId, row.id, tenantId)
    },
  }

  const toggleSelected = (id: string, checked: boolean) => {
    setSelectedIds((current) => checked ? Array.from(new Set([...current, id])) : current.filter((item) => item !== id))
  }
  const clearSelected = () => setSelectedIds([])
  const setLifecycleView = (next: CrudLifecycleView) => {
    setView(next)
    clearSelected()
  }
  const applyBulkAction = async (action: ConnectorBulkAction) => {
    if (selectedIds.length === 0 || bulkBusy) return
    setBulkBusy(true)
    try {
      for (const connectorId of selectedIds) {
        if (action === 'archive') {
          await archiveConnector(orgId, connectorId, tenantId)
        } else if (action === 'trash') {
          await trashConnector(orgId, connectorId, tenantId)
        } else {
          await restoreConnector(orgId, connectorId, tenantId)
        }
      }
      clearSelected()
      setView(action === 'archive' ? 'archived' : action === 'trash' ? 'trash' : 'active')
    } finally {
      setBulkBusy(false)
    }
  }

  return (
    <section className="page-section iam-control axis-crud-host">
      <div ref={rootRef} className="connectors-crud">
        <CrudPage<ConnectorCrudRow>
          key={`connectors-${view}-${types.length}`}
          dataSource={dataSource}
          stringsBase={crudStringsEs}
          strings={{ actionUnarchive: 'Restaurar' }}
          initialView={view}
          supportsArchived
          supportsTrash
          allowCreate={types.length > 0}
          allowEdit
          allowArchive
          allowTrash
          allowUnarchive
          allowRestore
          label="connector"
          labelPlural="connectors"
          labelPluralCap="Connectors"
          createLabel="Nuevo"
          columns={connectorColumns(selectedIds, toggleSelected)}
          formFields={connectorFormFields(typeOptions, fields)}
          searchText={(row) => [
            row.name,
            row.kind,
            row.status,
            row.org_id,
            ...Object.values(row.config ?? {}).map((value) => String(value ?? '')),
          ].join(' ')}
          toFormValues={(row) => connectorToFormValues(row, fields)}
          isValid={(values) => connectorFormValid(values, typeByKind)}
          emptyState="Sin connectors"
          archivedEmptyState="Sin connectors archivados"
          trashEmptyState="Sin connectors en papelera"
          searchPlaceholder="Buscar connectors"
          listHeaderInlineSlot={() => (
            <div className="iam-control__lead-stack">
              <ConnectorCreateAndBulkActions
                selectedCount={selectedIds.length}
                view={view}
                busy={bulkBusy}
                onCreate={() => setCreateRequested(true)}
                onClear={clearSelected}
                onBulkAction={(action) => void applyBulkAction(action)}
              />
              {typesError && <p className="iam-control__inline-error">{typesError}</p>}
            </div>
          )}
          toolbarActions={lifecycleToolbarActions(view, setLifecycleView)}
          rowActions={[
            {
              id: 'test',
              label: 'Probar conexión',
              kind: 'secondary',
              isVisible: (row) => Boolean(typeByKind.get(row.kind)?.supports_test),
              onClick: async (row, helpers) => {
                await testConnector(orgId, row.id, tenantId)
                await helpers.reload()
              },
            },
            {
              id: 'refresh',
              label: 'Refrescar capabilities',
              kind: 'secondary',
              isVisible: (row) => Boolean(typeByKind.get(row.kind)?.supports_refresh),
              onClick: async (row, helpers) => {
                await refreshConnector(orgId, row.id, tenantId)
                await helpers.reload()
              },
            },
          ]}
          featureFlags={{ csvToolbar: false, archivedToggle: false, trashToggle: false }}
        />
      </div>
    </section>
  )
}

function connectorColumns(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
): CrudPageProps<ConnectorCrudRow>['columns'] {
  return [
    selectionColumn<ConnectorCrudRow>(selectedIds, onToggle),
    { key: 'name', header: 'Nombre' },
    { key: 'kind', header: 'Tipo' },
    { key: 'enabled', header: 'Enabled', render: (value) => value ? 'sí' : 'no' },
    { key: 'status', header: 'Estado', render: (value) => formatConnectorStatus(String(value ?? '')) },
    { key: 'updated_at', header: 'Actualizado', render: (value) => value ? new Date(String(value)).toLocaleString() : '-' },
  ]
}

function connectorFormFields(
  typeOptions: Array<{ label: string; value: string }>,
  fields: AxisConnectorConfigField[],
): CrudPageProps<ConnectorCrudRow>['formFields'] {
  return [
    { key: 'name', label: 'Nombre', required: true },
    { key: 'kind', label: 'Tipo', type: 'select' as const, required: true, createOnly: true, options: typeOptions },
    {
      key: 'enabled',
      label: 'Estado operativo',
      type: 'select' as const,
      options: [
        { label: 'Activo', value: 'true' },
        { label: 'Deshabilitado', value: 'false' },
      ],
    },
    ...fields.map((field) => ({
      key: configFieldKey(field.key),
      label: field.label,
      type: crudFieldType(field.type),
      required: field.required,
      options: field.options?.map((option) => ({ label: option, value: option })),
    })),
  ]
}

function connectorConfigFields(types: AxisConnectorTypeView[]): AxisConnectorConfigField[] {
  const byKey = new Map<string, AxisConnectorConfigField>()
  for (const type of types) {
    for (const field of type.config_schema?.fields ?? []) {
      if (!byKey.has(field.key)) byKey.set(field.key, field)
    }
  }
  return Array.from(byKey.values())
}

function connectorToRow(connector: AxisConnectorView): ConnectorCrudRow {
  return { ...connector, id: connector.connector_id || connector.id }
}

function connectorToFormValues(row: ConnectorCrudRow, fields: AxisConnectorConfigField[]): CrudFormValues {
  const values: CrudFormValues = {
    name: row.name,
    kind: row.kind,
    enabled: row.enabled ? 'true' : 'false',
  }
  for (const field of fields) {
    const value = row.config?.[field.key]
    values[configFieldKey(field.key)] = value === undefined || value === null ? field.default_value ?? '' : String(value)
  }
  return values
}

function connectorPayload(
  values: CrudFormValues,
  typeByKind: Map<string, AxisConnectorTypeView>,
  existing?: AxisConnectorView,
): Partial<AxisConnectorView> {
  const kind = stringValue(values.kind) || existing?.kind || ''
  const connectorType = typeByKind.get(kind)
  const config: Record<string, unknown> = { ...(existing?.config ?? {}) }
  for (const field of connectorType?.config_schema?.fields ?? []) {
    const raw = stringValue(values[configFieldKey(field.key)])
    if (raw === '' && field.default_value) {
      config[field.key] = field.default_value
    } else if (field.type === 'number') {
      config[field.key] = raw === '' ? undefined : Number(raw)
    } else if (field.type === 'checkbox') {
      config[field.key] = booleanValue(values[configFieldKey(field.key)])
    } else {
      config[field.key] = raw
    }
  }
  return {
    name: stringValue(values.name),
    kind,
    enabled: enabledValue(values.enabled),
    status: enabledValue(values.enabled) ? 'active' : 'disabled',
    config,
  }
}

function connectorFormValid(values: CrudFormValues, typeByKind: Map<string, AxisConnectorTypeView>): boolean {
  const kind = stringValue(values.kind)
  if (!stringValue(values.name) || !kind) return false
  const connectorType = typeByKind.get(kind)
  if (!connectorType) return false
  return (connectorType.config_schema?.fields ?? []).every((field) => {
    if (!field.required) return true
    return stringValue(values[configFieldKey(field.key)]).length > 0
  })
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

function ConnectorCreateAndBulkActions(props: {
  selectedCount: number
  view: CrudLifecycleView
  busy: boolean
  onCreate: () => void
  onClear: () => void
  onBulkAction: (action: ConnectorBulkAction) => void
}) {
  const actionsDisabled = props.busy || props.selectedCount === 0
  return (
    <div className="iam-control__create-inline">
      <div className="iam-control__bulk-buttons">
        <button type="button" className="iam-control__new-button" disabled={props.busy && props.selectedCount === 0} onClick={props.onCreate}>Nuevo</button>
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
          <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restaurar</button>
        )}
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

async function loadConnectorTypes(
  orgId: string,
  tenantId: string,
  setTypes: (rows: AxisConnectorTypeView[]) => void,
  setTypesError: (message: string) => void,
) {
  try {
    setTypes(await listConnectorTypes(orgId, tenantId))
    setTypesError('')
  } catch (err) {
    setTypes([])
    setTypesError(err instanceof Error ? err.message : 'No se pudieron cargar los connector types')
  }
}

function configFieldKey(key: string): string {
  return `config.${key}`
}

function crudFieldType(type: AxisConnectorConfigField['type']) {
  if (type === 'textarea' || type === 'select' || type === 'checkbox' || type === 'number') return type
  return 'text'
}

function stringValue(value: unknown): string {
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  return String(value ?? '').trim()
}

function booleanValue(value: unknown): boolean {
  if (typeof value === 'boolean') return value
  return ['true', '1', 'yes', 'si', 'sí'].includes(stringValue(value).toLowerCase())
}

function enabledValue(value: unknown): boolean {
  const raw = stringValue(value)
  if (raw === '') return true
  return booleanValue(raw)
}

function formatConnectorStatus(status: string): string {
  switch (status.trim().toLowerCase()) {
    case 'active':
      return 'activo'
    case 'disabled':
      return 'deshabilitado'
    case 'archived':
      return 'archivado'
    case 'trash':
      return 'papelera'
    default:
      return status || '-'
  }
}
