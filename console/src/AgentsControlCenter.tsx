import {
  CrudPage as PlatformCrudPage,
  crudStringsEs,
  type CrudFormValues,
  type CrudHttpClient,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import type { ReactElement } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  axisCrudHttpClient,
  listAgentProfiles,
  listIAMTenants,
  type AxisAgentProfileView,
  type AxisAgentView,
  type AxisTenantView,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'
type ReviewFilter = 'all' | 'approved' | 'needs_review' | 'ignored'

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

const AUTONOMY_OPTIONS = [
  { label: 'A1', value: 'A1' },
  { label: 'A2', value: 'A2' },
  { label: 'A3', value: 'A3' },
]

export function AgentsControlCenter({ orgId }: { orgId: string }) {
  const rootRef = useRef<HTMLElement | null>(null)
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [createRequested, setCreateRequested] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [axisOrgs, setAxisOrgs] = useState<AxisTenantView[]>([])
  const [selectedOrgId, setSelectedOrgId] = useState('')
  const [reviewFilter, setReviewFilter] = useState<ReviewFilter>('all')
  const [reviewBusyId, setReviewBusyId] = useState('')
  const [agentProfiles, setAgentProfiles] = useState<AxisAgentProfileView[]>([])
  const [profilesError, setProfilesError] = useState('')

  const activeOrgs = useMemo(() => axisOrgs.filter((org) => lifecycleBucket(org.status) === 'active'), [axisOrgs])
  const orgNameById = useMemo(() => new Map(activeOrgs.map((org) => [org.id, org.name])), [activeOrgs])
  const activeProfiles = useMemo(() => agentProfiles.filter((profile) => profile.enabled && !profile.archived_at), [agentProfiles])
  const profileOptions = useMemo(() => activeProfiles.map((profile) => ({
    label: `${profile.name} · ${profile.profile_id}`,
    value: profile.profile_id,
  })), [activeProfiles])
  const crudClient = useMemo(() => axisCrudHttpClient(orgId), [orgId])
  const isActive = selectedOrgId.length > 0 && profileOptions.length > 0

  useEffect(() => {
    void loadOrgOptions(orgId, setAxisOrgs)
  }, [orgId, reloadVersion])

  useEffect(() => {
    void loadProfileOptions(orgId, setAgentProfiles, setProfilesError)
  }, [orgId, reloadVersion])

  useEffect(() => {
    if (activeOrgs.length === 0) {
      setSelectedOrgId('')
      return
    }
    if (selectedOrgId && activeOrgs.some((org) => org.id === selectedOrgId)) return
    const preferred = activeOrgs.find((org) => org.id === orgId)
    setSelectedOrgId(preferred?.id ?? activeOrgs[0].id)
  }, [activeOrgs, orgId, selectedOrgId])

  useEffect(() => {
    setSelectedIds([])
  }, [selectedOrgId])

  useEffect(() => {
    if (!createRequested) return
    const handle = window.setTimeout(() => {
      const buttons = Array.from(rootRef.current?.querySelectorAll<HTMLButtonElement>('.crud-page-shell__header-actions > .actions-row > .actions-row > button') ?? [])
      buttons.find((button) => button.textContent?.trim() === 'Nuevo')?.click()
      setCreateRequested(false)
    }, 0)
    return () => window.clearTimeout(handle)
  }, [createRequested, reloadVersion])

  const toggleSelected = (id: string, checked: boolean) => {
    setSelectedIds((current) => checked ? Array.from(new Set([...current, id])) : current.filter((item) => item !== id))
  }

  const clearSelected = () => setSelectedIds([])

  const applyReviewAction = async (agent: AxisAgentView, action: 'approve' | 'ignore') => {
    if (!isActive || reviewBusyId) return
    setReviewBusyId(agent.id)
    try {
      await crudClient.json(`/api/agents/${agent.id}/${action}`, { method: 'POST', body: {} })
      setReloadVersion((current) => current + 1)
    } finally {
      setReviewBusyId('')
    }
  }

  const applyBulkAction = async (action: BulkAction) => {
    if (!isActive || selectedIds.length === 0 || bulkBusy) return
    setBulkBusy(true)
    try {
      for (const id of selectedIds) {
        const method = action === 'purge' ? 'DELETE' : 'POST'
        await crudClient.json(`/api/agents/${id}/${action}`, { method, body: {} })
      }
      clearSelected()
      setReloadVersion((current) => current + 1)
    } finally {
      setBulkBusy(false)
    }
  }

  const orgSelector = (
    <div className="iam-control__below-actions">
      <label>
        <span>Org</span>
        <select value={selectedOrgId} onChange={(event) => setSelectedOrgId(event.target.value)}>
          {activeOrgs.map((org) => (
            <option key={org.id} value={org.id}>{org.name}</option>
          ))}
        </select>
      </label>
      <label>
        <span>Revisión</span>
        <select value={reviewFilter} onChange={(event) => setReviewFilter(event.target.value as ReviewFilter)}>
          <option value="all">Todas</option>
          <option value="approved">Aprobadas</option>
          <option value="needs_review">En revisión</option>
          <option value="ignored">Ignoradas</option>
        </select>
      </label>
    </div>
  )

  return (
    <section ref={rootRef} className="page-section iam-control iam-control--external-lifecycle axis-crud-host">
      <CrudPage<AxisAgentView>
        key={`agents-${selectedOrgId}-${lifecycleView}-${reloadVersion}`}
        basePath="/api/agents"
        listQuery={selectedOrgId ? `org_id=${encodeURIComponent(selectedOrgId)}` : undefined}
        httpClient={crudClient}
        stringsBase={crudStringsEs}
        strings={{ actionTrash: 'Papelera' }}
        initialView={lifecycleView}
        supportsArchived
        supportsTrash
        allowCreate={isActive}
        allowEdit={isActive}
        allowArchive={isActive}
        allowTrash={isActive}
        allowUnarchive={isActive}
        allowRestore={isActive}
        allowPurge={isActive}
        label="agente"
        labelPlural="agentes"
        labelPluralCap="Agentes"
        createLabel="Nuevo"
        columns={agentColumns(selectedIds, toggleSelected, orgNameById, reviewBusyId, (agent, action) => void applyReviewAction(agent, action))}
        formFields={agentFormFields(profileOptions)}
        preSearchFilter={(items) => reviewFilter === 'all' ? items : items.filter((item) => normalizeReviewStatus(item.review_status) === reviewFilter)}
        searchText={(row) => [
          orgNameById.get(row.org_id) ?? row.org_id,
          row.name,
          row.profile,
          row.autonomy,
          row.description,
          row.origin_kind,
          row.review_status,
          row.source_product_surface,
          row.source_org_id,
          row.source_agent_id,
          ...stringList(row.capabilities),
          ...stringList(row.tools),
        ].join(' ')}
        toFormValues={(row) => ({
          name: row.name,
          profile: row.profile,
          autonomy: row.autonomy,
          memory_enabled: row.memory_enabled,
          description: row.description,
          capabilities: stringList(row.capabilities).join(', '),
          tools: stringList(row.tools).join(', '),
        })}
        toBody={(values) => ({
          org_id: selectedOrgId,
          name: stringValue(values.name),
          profile: stringValue(values.profile),
          autonomy: stringValue(values.autonomy),
          memory_enabled: booleanValue(values.memory_enabled),
          description: stringValue(values.description),
          capabilities: splitList(values.capabilities),
          tools: splitList(values.tools),
        })}
        isValid={(values) => isActive && stringValue(values.name).length > 0 && stringValue(values.profile).length > 0}
        emptyState={profileOptions.length > 0 ? 'Sin agentes' : 'Sin perfiles disponibles'}
        archivedEmptyState="Sin agentes archivados"
        trashEmptyState="Sin agentes en papelera"
        searchPlaceholder="Buscar agentes"
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
            {orgSelector}
            {profilesError && <p className="iam-control__inline-error">{profilesError}</p>}
          </div>
        )}
        toolbarActions={lifecycleToolbarActions(lifecycleView, (view) => {
          setLifecycleView(view)
          clearSelected()
        })}
        featureFlags={{ csvToolbar: false }}
      />
    </section>
  )
}

function agentColumns(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
  orgNameById: Map<string, string>,
  reviewBusyId: string,
  onReviewAction: (agent: AxisAgentView, action: 'approve' | 'ignore') => void,
): CrudPageProps<AxisAgentView>['columns'] {
  return [
    selectionColumn<AxisAgentView>(selectedIds, onToggle),
    { key: 'org_id', header: 'Org', render: (value) => orgNameById.get(String(value ?? '')) ?? String(value ?? '-') },
    { key: 'name', header: 'Nombre' },
    { key: 'profile', header: 'Perfil', render: (value) => formatProfile(String(value ?? '')) },
    { key: 'autonomy', header: 'Autonomía' },
    { key: 'source_org_id', header: 'Origen', render: (_value, row) => formatOrigin(row) },
    { key: 'origin_kind', header: 'Tipo origen', render: (value) => formatOriginKind(String(value ?? '')) },
    { key: 'review_status', header: 'Revisión', render: (_value, row) => (
      <ReviewCell agent={row} busy={reviewBusyId === row.id} onAction={onReviewAction} />
    ) },
    { key: 'status', header: 'Estado', render: (value) => formatStatus(String(value ?? '')) },
  ]
}

function ReviewCell(props: {
  agent: AxisAgentView
  busy: boolean
  onAction: (agent: AxisAgentView, action: 'approve' | 'ignore') => void
}) {
  const status = normalizeReviewStatus(props.agent.review_status)
  return (
    <div className="agent-review-cell">
      <span>{formatReviewStatus(status)}</span>
      {status === 'needs_review' && (
        <div className="agent-review-cell__actions">
          <button type="button" disabled={props.busy} onClick={() => props.onAction(props.agent, 'approve')}>Aprobar</button>
          <button type="button" disabled={props.busy} onClick={() => props.onAction(props.agent, 'ignore')}>Ignorar</button>
        </div>
      )}
      {status === 'ignored' && (
        <button type="button" disabled={props.busy} onClick={() => props.onAction(props.agent, 'approve')}>Aprobar</button>
      )}
    </div>
  )
}

function agentFormFields(profileOptions: Array<{ label: string; value: string }>): CrudPageProps<AxisAgentView>['formFields'] {
  return [
    { key: 'name', label: 'Nombre', required: true },
    { key: 'profile', label: 'Perfil', type: 'select' as const, required: true, options: profileOptions },
    { key: 'autonomy', label: 'Autonomía', type: 'select' as const, required: true, options: AUTONOMY_OPTIONS },
    { key: 'memory_enabled', label: 'Memoria', type: 'checkbox' as const },
    { key: 'description', label: 'Descripción', type: 'textarea' as const, rows: 3, fullWidth: true },
    { key: 'capabilities', label: 'Capabilities', type: 'textarea' as const, rows: 2, fullWidth: true },
    { key: 'tools', label: 'Tools', type: 'textarea' as const, rows: 2, fullWidth: true },
  ]
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

function lifecycleToolbarActions(view: CrudLifecycleView, onChange: (view: CrudLifecycleView) => void) {
  return [
    { id: 'active', label: 'Activos', kind: view === 'active' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('active') },
    { id: 'archived', label: 'Archivados', kind: view === 'archived' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('archived') },
    { id: 'trash', label: 'Papelera', kind: view === 'trash' ? 'primary' as const : 'secondary' as const, onClick: () => onChange('trash') },
  ]
}

async function loadOrgOptions(orgId: string, setAxisOrgs: (rows: AxisTenantView[]) => void) {
  try {
    setAxisOrgs(await listIAMTenants(orgId))
  } catch {
    setAxisOrgs([])
  }
}

async function loadProfileOptions(
  orgId: string,
  setAgentProfiles: (rows: AxisAgentProfileView[]) => void,
  setProfilesError: (message: string) => void,
) {
  try {
    const profiles = await listAgentProfiles(orgId)
    setAgentProfiles(profiles)
    setProfilesError('')
  } catch (err) {
    setAgentProfiles([])
    setProfilesError(err instanceof Error ? err.message : 'No se pudieron cargar los perfiles')
  }
}

function lifecycleBucket(status: string): CrudLifecycleView {
  const normalized = status.trim().toLowerCase()
  if (normalized === 'archived') return 'archived'
  if (normalized === 'trash' || normalized === 'disabled' || normalized === 'removed' || normalized === 'inactive') return 'trash'
  return 'active'
}

function formatStatus(status: string): string {
  switch (status.trim().toLowerCase()) {
    case 'active':
      return 'activo'
    case 'archived':
      return 'archivado'
    case 'trash':
      return 'papelera'
    default:
      return status || '-'
  }
}

function formatProfile(profile: string): string {
  if (profile.trim() === 'legacy.unprofiled') return 'Sin perfil'
  return profile || '-'
}

function formatOrigin(agent: AxisAgentView): string {
  const product = agent.source_product_surface?.trim()
  const org = agent.source_org_id?.trim()
  if (product && org) return `${product} / ${org}`
  return '-'
}

function formatOriginKind(kind: string): string {
  switch (kind.trim().toLowerCase()) {
    case 'companion_fleet':
      return 'Fleet'
    case 'runtime_inferred':
      return 'Inferido'
    case 'manual':
      return 'Manual'
    default:
      return kind || '-'
  }
}

function normalizeReviewStatus(status?: string): ReviewFilter {
  const normalized = String(status ?? '').trim().toLowerCase()
  if (normalized === 'needs_review' || normalized === 'ignored' || normalized === 'approved') return normalized
  return 'approved'
}

function formatReviewStatus(status: ReviewFilter): string {
  switch (status) {
    case 'needs_review':
      return 'En revisión'
    case 'ignored':
      return 'Ignorado'
    case 'approved':
      return 'Aprobado'
    default:
      return 'Todas'
  }
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function stringList(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.map((item) => String(item ?? '').trim()).filter(Boolean)
}

function booleanValue(value: unknown): boolean {
  return value === true
}

function splitList(value: unknown): string[] {
  return stringValue(value)
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}
