import {
  CrudPage as PlatformCrudPage,
  crudStringsEs,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import type { ReactElement } from 'react'
import { useMemo, useState } from 'react'
import { listTools, type ToolRecord } from './api'

type ToolStatusFilter = 'all' | 'active' | 'disabled' | 'deprecated'
type ToolCrudRow = ToolRecord & { id: string }

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

const TOOL_STATUS_FILTERS: Array<{ label: string; value: ToolStatusFilter }> = [
  { label: 'Todas', value: 'all' },
  { label: 'Activas', value: 'active' },
  { label: 'Disabled', value: 'disabled' },
  { label: 'Deprecated', value: 'deprecated' },
]

export function AdminControlCenter({ orgId, tenantId }: { orgId: string; tenantId: string }) {
  const [statusFilter, setStatusFilter] = useState<ToolStatusFilter>('all')

  const dataSource: NonNullable<CrudPageProps<ToolCrudRow>['dataSource']> = useMemo(() => ({
    async list() {
      const tools = await listTools(orgId, statusFilter === 'all' ? '' : statusFilter, tenantId)
      return tools.map(toolToRow)
    },
  }), [orgId, tenantId, statusFilter])

  if (!tenantId) {
    return (
      <section className="empty-state">
        Seleccioná un tenant válido para ver Tools.
      </section>
    )
  }

  return (
    <section className="page-section iam-control axis-crud-host admin-tools-crud">
      <div className="screen-nav agents-section-tabs">
        <button type="button" className="active">Tools</button>
      </div>

      <CrudPage<ToolCrudRow>
        key={`tools-${orgId}-${tenantId}-${statusFilter}`}
        dataSource={dataSource}
        stringsBase={crudStringsEs}
        label="tool"
        labelPlural="tools"
        labelPluralCap="Tools"
        columns={toolColumns()}
        formFields={[]}
        searchText={toolSearchText}
        toFormValues={() => ({})}
        isValid={() => false}
        emptyState="Sin tools"
        searchPlaceholder="Buscar tools"
        allowCreate={false}
        allowEdit={false}
        allowArchive={false}
        allowTrash={false}
        allowUnarchive={false}
        allowRestore={false}
        allowPurge={false}
        toolbarActions={TOOL_STATUS_FILTERS.map((filter) => ({
          id: filter.value,
          label: filter.label,
          kind: statusFilter === filter.value ? 'success' : 'secondary',
          onClick: () => setStatusFilter(filter.value),
        }))}
        featureFlags={{ csvToolbar: false, archivedToggle: false, trashToggle: false, createAction: false }}
      />
    </section>
  )
}

function toolColumns(): CrudPageProps<ToolCrudRow>['columns'] {
  return [
    { key: 'name', header: 'Tool', render: (value) => String(value ?? '-') },
    { key: 'tool_key', header: 'Key', render: (value) => String(value ?? '-') },
    { key: 'operation', header: 'Operación', render: (value) => String(value ?? '-') },
    { key: 'side_effect', header: 'Side effect', render: (value) => value ? 'sí' : 'no' },
    { key: 'status', header: 'Estado', render: (value) => formatStatus(String(value ?? '')) },
    { key: 'capability_key', header: 'Capability', render: (_value, row) => row.capability_key || row.capability_id || '-' },
  ]
}

function toolToRow(tool: ToolRecord): ToolCrudRow {
  return { ...tool, id: tool.tool_id || tool.tool_key }
}

function toolSearchText(tool: ToolCrudRow) {
  return [
    tool.tool_key,
    tool.name,
    tool.description,
    tool.operation,
    tool.status,
    tool.capability_key,
    tool.capability_id,
  ].filter(Boolean).join(' ')
}

function formatStatus(status: string) {
  if (!status) return '-'
  return status.replace(/_/g, ' ')
}
