import {
  CrudPage as PlatformCrudPage,
  crudStringsEs,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import {
  PromptEditorReview,
  ReadonlyContentViewer,
  downloadTextFile,
  safeFileName,
  useTextFileUpload,
} from '@devpablocristo/platform-crud-ui/prompt-files'
import type { ReactElement } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { CompanionAgent } from './api'
import { axisFetch } from './api'

type PromptLifecycleView = 'active' | 'archived' | 'trash'
type PromptSection = 'product' | 'agents'
type PromptBulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type AssistPack = {
  id: string
  owner_system: string
  product_surface: string
  assist_type: string
  name: string
  description?: string
  input_contract: string
  output_contract: string
  prompt_template: string
  model_policy?: Record<string, unknown>
  enabled: boolean
  archived_at?: string
  updated_at?: string
}

type AgentProfile = {
  id?: string
  profile_id: string
  family_id: string
  version_label: string
  name: string
  description?: string
  system_prompt: string
  max_autonomy: string
  allowed_tools?: string[]
  allowed_capabilities?: string[]
  memory_policy?: Record<string, unknown>
  llm_config?: Record<string, unknown>
  enabled: boolean
  archived_at?: string
  updated_at?: string
}

type AxisPromptRow = {
  id: string
  name: string
  promptKey: string
  kind: 'assist-pack' | 'agent-profile'
  description?: string
  version: string
  promptText: string
  productSurface: string
  scopeLabel?: string
  enabled: boolean
  updatedAt?: string
  archived: boolean
  original: AssistPack | AgentProfile
}

type PendingUpload = {
  row: AxisPromptRow
  fileName: string
  content: string
  nextVersion: string
}

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function PromptsControlCenter({
  orgId,
  productSurface,
  agents,
  initialSection = 'product',
}: {
  orgId: string
  productSurface: string
  agents: CompanionAgent[]
  initialSection?: PromptSection
}) {
  const [activeSection, setActiveSection] = useState<PromptSection>(initialSection)

  useEffect(() => {
    setActiveSection(initialSection)
  }, [initialSection])

  return (
    <section className="page-section iam-control axis-crud-host">
      <div className="screen-nav agents-section-tabs">
        <button type="button" className={activeSection === 'product' ? 'active' : ''} onClick={() => setActiveSection('product')}>Producto</button>
        <button type="button" className={activeSection === 'agents' ? 'active' : ''} onClick={() => setActiveSection('agents')}>Perfiles</button>
      </div>
      {activeSection === 'product' ? (
        <AssistPackPromptsScreen orgId={orgId} productSurface={productSurface} section={activeSection} />
      ) : (
        <AgentProfilePromptsScreen orgId={orgId} productSurface={productSurface} agents={agents} section={activeSection} />
      )}
    </section>
  )
}

function AssistPackPromptsScreen({
  orgId,
  productSurface,
  section,
}: {
  orgId: string
  productSurface: string
  section: PromptSection
}) {
  return (
    <PromptCrudScreen
      orgId={orgId}
      title="Producto"
      section={section}
      allowUpload={false}
      allowLifecycleManagement={false}
      supportsTrash={false}
      loadRows={async (view) => {
        const query = `product_surface=${encodeURIComponent(productSurface)}&lifecycle=${encodeURIComponent(view)}`
        const packs = await axisFetch<AssistPack[]>(
          `/api/prompts/assist-packs?${query}`,
          orgId,
          productHeaders(productSurface),
        )
        return packs.map((pack) => ({
          id: pack.id,
          name: pack.name,
          promptKey: pack.assist_type,
          kind: 'assist-pack',
          description: pack.description,
          version: stringValue(pack.model_policy?.prompt_version),
          promptText: pack.prompt_template,
          productSurface: pack.product_surface || productSurface,
          enabled: pack.enabled,
          updatedAt: pack.updated_at,
          archived: Boolean(pack.archived_at),
          original: pack,
        }))
      }}
      replacePrompt={async (row, content, nextVersion) => {
        const pack = row.original as AssistPack
        await axisFetch(`/api/prompts/assist-packs/${encodeURIComponent(pack.id)}/content`, orgId, {
          method: 'PUT',
          headers: { 'X-Product-Surface': productSurface },
          body: JSON.stringify({
            owner_system: pack.owner_system,
            product_surface: pack.product_surface,
            assist_type: pack.assist_type,
            name: pack.name,
            description: pack.description ?? '',
            input_contract: pack.input_contract,
            output_contract: pack.output_contract,
            prompt_template: content,
            model_policy: { ...(pack.model_policy ?? {}), prompt_version: nextVersion },
            enabled: pack.enabled,
          }),
        })
      }}
    />
  )
}

function AgentProfilePromptsScreen({
  orgId,
  productSurface,
  agents,
  section,
}: {
  orgId: string
  productSurface: string
  agents: CompanionAgent[]
  section: PromptSection
}) {
  return (
    <PromptCrudScreen
      orgId={orgId}
      title="Perfiles"
      section={section}
      allowUpload
      allowLifecycleManagement
      supportsTrash
      loadRows={async (view) => {
        const profileIDs = new Set(
          agents
            .filter((agent) => sameProduct(agent.product_surface, productSurface))
            .map((agent) => agent.profile_id?.trim())
            .filter((profileID): profileID is string => Boolean(profileID)),
        )
        if (profileIDs.size === 0) {
          return []
        }
        const response = await axisFetch<{ profiles: AgentProfile[] }>(
          `/api/prompts/agent-profiles?lifecycle=${encodeURIComponent(view)}`,
          orgId,
          productHeaders(productSurface),
        )
        return response.profiles
          .filter((profile) => profileIDs.has(profile.profile_id))
          .map((profile) => ({
            id: profile.profile_id,
            name: profile.name,
            promptKey: profile.profile_id,
            kind: 'agent-profile',
            description: profile.description,
            version: profile.version_label,
            promptText: profile.system_prompt,
            productSurface: 'global',
            scopeLabel: 'Global',
            enabled: profile.enabled,
            updatedAt: profile.updated_at,
            archived: Boolean(profile.archived_at),
            original: profile,
          }))
      }}
      replacePrompt={async (row, content, nextVersion) => {
        const profile = row.original as AgentProfile
        await axisFetch(`/api/prompts/agent-profiles/${encodeURIComponent(profile.profile_id)}/system-prompt`, orgId, {
          method: 'PUT',
          headers: { 'X-Product-Surface': productSurface },
          body: JSON.stringify({
            family_id: profile.family_id,
            version_label: nextVersion,
            name: profile.name,
            description: profile.description ?? '',
            system_prompt: content,
            max_autonomy: profile.max_autonomy,
            allowed_tools: profile.allowed_tools ?? [],
            allowed_capabilities: profile.allowed_capabilities ?? [],
            memory_policy: profile.memory_policy ?? {},
            llm_config: profile.llm_config ?? {},
            enabled: profile.enabled,
          }),
        })
      }}
      archivePrompt={(row) =>
        axisFetch(`/api/prompts/agent-profiles/${encodeURIComponent(row.id)}/archive`, orgId, {
          method: 'POST',
          headers: { 'X-Product-Surface': productSurface },
          body: '{}',
        })
      }
      restorePrompt={(row) =>
        axisFetch(`/api/prompts/agent-profiles/${encodeURIComponent(row.id)}/restore`, orgId, {
          method: 'POST',
          headers: { 'X-Product-Surface': productSurface },
          body: '{}',
        })
      }
      trashPrompt={(row) =>
        axisFetch(`/api/prompts/agent-profiles/${encodeURIComponent(row.id)}/trash`, orgId, {
          method: 'POST',
          headers: { 'X-Product-Surface': productSurface },
          body: '{}',
        })
      }
      purgePrompt={(row) =>
        axisFetch(`/api/prompts/agent-profiles/${encodeURIComponent(row.id)}/purge`, orgId, {
          method: 'DELETE',
          headers: { 'X-Product-Surface': productSurface },
        })
      }
    />
  )
}

function PromptCrudScreen({
  title,
  section,
  allowUpload,
  allowLifecycleManagement,
  supportsTrash,
  loadRows,
  replacePrompt,
  archivePrompt,
  restorePrompt,
  trashPrompt,
  purgePrompt,
}: {
  orgId: string
  title: string
  section: PromptSection
  allowUpload: boolean
  allowLifecycleManagement: boolean
  supportsTrash: boolean
  loadRows: (view: PromptLifecycleView) => Promise<AxisPromptRow[]>
  replacePrompt: (row: AxisPromptRow, content: string, nextVersion: string) => Promise<unknown>
  archivePrompt?: (row: AxisPromptRow) => Promise<unknown>
  restorePrompt?: (row: AxisPromptRow) => Promise<unknown>
  trashPrompt?: (row: AxisPromptRow) => Promise<unknown>
  purgePrompt?: (row: AxisPromptRow) => Promise<unknown>
}) {
  const [promptView, setPromptView] = useState<PromptLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [crudError, setCrudError] = useState('')
  const [viewedPrompt, setViewedPrompt] = useState<AxisPromptRow | null>(null)
  const [pendingUpload, setPendingUpload] = useState<PendingUpload | null>(null)
  const upload = useTextFileUpload<AxisPromptRow>({
    extensions: ['.md'],
    accept: '.md,text/markdown',
    emptyFileMessage: 'El archivo está vacío',
    invalidFileMessage: 'Solo se soportan archivos .md',
    onLoad: ({ context, fileName, content }) => {
      if (!context) return
      setPendingUpload({
        row: context,
        fileName,
        content,
        nextVersion: nextPromptVersion(context.version),
      })
    },
    onError: (error) => setCrudError(error.message),
  })

  const dataSource = useMemo(
    () => ({
      list: ({ view }: { view: 'active' | 'archived' | 'trash' }) => loadRows(view),
      ...(allowLifecycleManagement && archivePrompt ? { archive: archivePrompt } : {}),
      ...(allowLifecycleManagement && trashPrompt ? { trash: trashPrompt } : {}),
      ...(allowLifecycleManagement && restorePrompt ? { unarchive: restorePrompt, restore: restorePrompt } : {}),
      ...(allowLifecycleManagement && purgePrompt ? { purge: purgePrompt } : {}),
    }),
    [allowLifecycleManagement, archivePrompt, loadRows, purgePrompt, restorePrompt, trashPrompt],
  )

  useEffect(() => {
    setSelectedIds([])
  }, [section, promptView])

  useEffect(() => {
    if (!supportsTrash && promptView === 'trash') {
      setPromptView('active')
    }
  }, [promptView, supportsTrash])

  const toggleSelected = (id: string, checked: boolean) => {
    setSelectedIds((current) => checked ? Array.from(new Set([...current, id])) : current.filter((item) => item !== id))
  }

  const clearSelected = () => setSelectedIds([])

  const applyBulkAction = async (action: PromptBulkAction, items: AxisPromptRow[]) => {
    if (!allowLifecycleManagement || selectedIds.length === 0 || bulkBusy) return
    setBulkBusy(true)
    setCrudError('')
    try {
      for (const id of selectedIds) {
        const row = items.find((item) => item.id === id)
        if (!row) continue
        if (action === 'archive' && archivePrompt) {
          await archivePrompt(row)
        } else if (action === 'trash' && trashPrompt) {
          await trashPrompt(row)
        } else if (action === 'restore' && restorePrompt) {
          await restorePrompt(row)
        } else if (action === 'purge' && purgePrompt) {
          await purgePrompt(row)
        }
      }
      clearSelected()
      setReloadVersion((current) => current + 1)
    } catch (err) {
      setCrudError(err instanceof Error ? err.message : 'No se pudo aplicar la acción')
    } finally {
      setBulkBusy(false)
    }
  }

  async function confirmUpload() {
    if (!pendingUpload) return
    try {
      setCrudError('')
      await replacePrompt(pendingUpload.row, pendingUpload.content, pendingUpload.nextVersion)
      setPendingUpload(null)
      setReloadVersion((current) => current + 1)
    } catch (err) {
      setCrudError(err instanceof Error ? err.message : 'No se pudo cargar el prompt')
    }
  }

  return (
    <div className={`axis-prompts-crud${supportsTrash ? ' axis-prompts-crud--trash' : ''}`}>
      <input aria-label={`Upload ${title}`} ref={upload.inputRef} {...upload.inputProps} />
      {crudError ? <p className="alert-error">{crudError}</p> : null}
      {viewedPrompt ? (
        <ReadonlyContentViewer
          title={viewedPrompt.name}
          subtitle={viewedPrompt.promptKey}
          metadata={[
            { label: 'Tipo', value: viewedPrompt.kind },
            { label: 'Producto', value: viewedPrompt.scopeLabel || viewedPrompt.productSurface },
            { label: 'Versión', value: viewedPrompt.version || '-' },
            { label: 'Activo', value: viewedPrompt.enabled ? 'sí' : 'no' },
            { label: 'Actualizado', value: formatDate(viewedPrompt.updatedAt) },
          ]}
          content={viewedPrompt.promptText}
          emptyContent="Sin contenido de prompt"
          closeLabel="Cerrar"
          onClose={() => setViewedPrompt(null)}
        />
      ) : null}
      {pendingUpload ? (
        <PromptEditorReview
          title="Revisar carga de prompt"
          subtitle={`${pendingUpload.row.name} · ${pendingUpload.row.promptKey}`}
          metadata={[
            { label: 'Versión actual', value: pendingUpload.row.version || '-' },
            { label: 'Nueva versión', value: pendingUpload.nextVersion },
            { label: 'Archivo', value: pendingUpload.fileName },
          ]}
          content={pendingUpload.content}
          onContentChange={(content) => {
            setPendingUpload((current) => (current ? { ...current, content } : current))
          }}
          cancelLabel="Cancelar"
          confirmLabel="Cargar"
          readOnly
          onCancel={() => setPendingUpload(null)}
          onConfirm={() => void confirmUpload()}
        />
      ) : null}
      <CrudPage<AxisPromptRow>
        key={`${section}-${promptView}-${reloadVersion}`}
        label="prompt"
        labelPlural="prompts"
        labelPluralCap={title}
        stringsBase={crudStringsEs}
        strings={{ actionUnarchive: 'Restaurar' }}
        initialView={promptView}
        supportsArchived
        supportsTrash={supportsTrash}
        allowCreate={false}
        allowEdit={false}
        allowArchive={allowLifecycleManagement}
        allowUnarchive={allowLifecycleManagement}
        allowTrash={allowLifecycleManagement && supportsTrash}
        allowRestore={allowLifecycleManagement && supportsTrash}
        allowPurge={allowLifecycleManagement && supportsTrash}
        dataSource={dataSource}
        columns={[
          ...(allowLifecycleManagement ? [selectionColumn<AxisPromptRow>(selectedIds, toggleSelected)] : []),
          {
            key: 'name',
            header: 'Prompt',
            render: (_value, row) => (
              <div>
                <strong>{row.name}</strong>
                {row.description ? <p className="axis-muted">{row.description}</p> : null}
              </div>
            ),
          },
          { key: 'promptKey', header: 'Clave' },
          {
            key: 'productSurface',
            header: 'Producto',
            render: (_value, row) => row.scopeLabel || row.productSurface,
          },
          { key: 'version', header: 'Versión' },
          {
            key: 'enabled',
            header: 'Activo',
            render: (value) => (value ? 'sí' : 'no'),
          },
          {
            key: 'updatedAt',
            header: 'Actualizado',
            render: (_value, row) => formatDate(row.updatedAt),
          },
        ]}
        formFields={[]}
        searchText={(row) => [row.name, row.promptKey, row.version, row.description].join(' ')}
        toFormValues={() => ({})}
        isValid={() => true}
        emptyState="Sin prompts"
        archivedEmptyState="Sin prompts archivados"
        searchPlaceholder="Buscar prompts"
        listHeaderInlineSlot={({ items }) => (
          allowLifecycleManagement ? (
            <PromptBulkActions
              selectedCount={selectedIds.length}
              view={promptView}
              busy={bulkBusy}
              supportsTrash={supportsTrash}
              onCreate={() => setCrudError('Para crear un prompt de perfil, primero creá el perfil en Agentes > Perfiles.')}
              onClear={clearSelected}
              onBulkAction={(action) => void applyBulkAction(action, items)}
            />
          ) : null
        )}
        toolbarActions={[
          { id: 'active', label: 'Activos', kind: promptView === 'active' ? 'primary' as const : 'secondary' as const, onClick: () => setPromptView('active') },
          { id: 'archived', label: 'Archivados', kind: promptView === 'archived' ? 'primary' as const : 'secondary' as const, onClick: () => setPromptView('archived') },
          ...(supportsTrash ? [
            { id: 'trash', label: 'Papelera', kind: promptView === 'trash' ? 'primary' as const : 'secondary' as const, onClick: () => setPromptView('trash') },
          ] : []),
        ]}
        rowActions={[
          {
            id: 'view',
            label: 'Ver',
            kind: 'secondary',
            onClick: (row) => setViewedPrompt(row),
          },
          {
            id: 'download',
            label: 'Descargar',
            kind: 'secondary',
            isVisible: (row) => Boolean(row.promptText),
            onClick: (row) => downloadTextFile(`${safeFileName(row.promptKey)}.md`, row.promptText),
          },
          {
            id: 'upload',
            label: 'Cargar',
            kind: 'success',
            isVisible: (_row, ctx) => allowUpload && ctx.view === 'active',
            onClick: (row) => {
              upload.open(row)
            },
          },
        ]}
        featureFlags={{
          createAction: false,
          archivedToggle: false,
          trashToggle: false,
          csvToolbar: false,
        }}
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

function PromptBulkActions(props: {
  selectedCount: number
  view: PromptLifecycleView
  busy: boolean
  supportsTrash: boolean
  onCreate: () => void
  onClear: () => void
  onBulkAction: (action: PromptBulkAction) => void
}) {
  const actionsDisabled = props.busy || props.selectedCount === 0
  return (
    <div className="iam-control__create-inline">
      <div className="iam-control__bulk-buttons">
        <button type="button" className="iam-control__new-button" disabled={props.busy && props.selectedCount === 0} onClick={props.onCreate}>Nuevo</button>
        {props.view === 'active' && (
          <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('archive')}>Archivar</button>
        )}
        {props.view === 'active' && props.supportsTrash && (
          <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('trash')}>Papelera</button>
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

function stringValue(value: unknown): string {
  return typeof value === 'string' && value.trim() ? value.trim() : ''
}

function productHeaders(productSurface: string): RequestInit {
  return { headers: { 'X-Product-Surface': productSurface } }
}

function sameProduct(value: string | undefined, productSurface: string) {
  return (value || '').trim().toLowerCase() === productSurface.trim().toLowerCase()
}

function nextPromptVersion(current: string): string {
  const trimmed = current.trim()
  const vNumber = /^v(\d+)$/i.exec(trimmed)
  if (vNumber) {
    return `v${Number(vNumber[1]) + 1}`
  }
  const semver = /^(\d+)\.(\d+)\.(\d+)$/.exec(trimmed)
  if (semver) {
    return `${semver[1]}.${semver[2]}.${Number(semver[3]) + 1}`
  }
  return 'v1'
}

function formatDate(value?: string) {
  if (!value) return '-'
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? '-' : parsed.toLocaleString()
}
