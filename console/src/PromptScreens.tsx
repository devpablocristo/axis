import {
  CrudPage as PlatformCrudPage,
  type CrudHelpers,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import {
  FileUploadReview,
  ReadonlyContentViewer,
  downloadTextFile,
  downloadZipFile,
  safeFileName,
  useTextFileUpload,
} from '@devpablocristo/platform-crud-ui/prompt-files'
import type { ReactElement } from 'react'
import { useMemo, useRef, useState } from 'react'
import type { CompanionAgent } from './api'
import { axisFetch } from './api'

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

export function AssistPackPromptsScreen({ orgId, productSurface }: { orgId: string; productSurface: string }) {
  return (
    <PromptCrudScreen
      orgId={orgId}
      title="Assist Packs"
      description="Runtime prompts used by Companion assist-runs."
      downloadAllName="axis_assist_packs.zip"
      loadRows={async (view) => {
        const query = `product_surface=${encodeURIComponent(productSurface)}`
        const packs = await axisFetch<AssistPack[]>(
          `/api/companion/v1/assist-packs${view === 'archived' ? '/archived' : ''}?${query}`,
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
        await axisFetch(`/api/companion/v1/assist-packs/${encodeURIComponent(pack.id)}`, orgId, {
          method: 'PATCH',
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
      archivePrompt={(row) =>
        axisFetch(`/api/companion/v1/assist-packs/${encodeURIComponent(row.id)}/archive`, orgId, {
          method: 'POST',
          headers: { 'X-Product-Surface': productSurface },
          body: '{}',
        })
      }
      restorePrompt={(row) =>
        axisFetch(`/api/companion/v1/assist-packs/${encodeURIComponent(row.id)}/restore`, orgId, {
          method: 'POST',
          headers: { 'X-Product-Surface': productSurface },
          body: '{}',
        })
      }
    />
  )
}

export function AgentProfilePromptsScreen({
  orgId,
  productSurface,
  agents,
}: {
  orgId: string
  productSurface: string
  agents: CompanionAgent[]
}) {
  return (
    <PromptCrudScreen
      orgId={orgId}
      title="Agent Profiles"
      description="Global Axis agent prompts loaded by runtime through profile_id."
      downloadAllName="axis_agent_profiles.zip"
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
          '/api/companion/v1/agent-profiles?include_archived=true',
          orgId,
          productHeaders(productSurface),
        )
        return response.profiles
          .filter((profile) => profileIDs.has(profile.profile_id))
          .filter((profile) => (view === 'archived' ? Boolean(profile.archived_at) : !profile.archived_at))
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
        await axisFetch(`/api/companion/v1/agent-profiles/${encodeURIComponent(profile.profile_id)}`, orgId, {
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
        axisFetch(`/api/companion/v1/agent-profiles/${encodeURIComponent(row.id)}/archive`, orgId, {
          method: 'POST',
          headers: { 'X-Product-Surface': productSurface },
          body: '{}',
        })
      }
      restorePrompt={(row) =>
        axisFetch(`/api/companion/v1/agent-profiles/${encodeURIComponent(row.id)}/restore`, orgId, {
          method: 'POST',
          headers: { 'X-Product-Surface': productSurface },
          body: '{}',
        })
      }
    />
  )
}

function PromptCrudScreen({
  title,
  description,
  downloadAllName,
  loadRows,
  replacePrompt,
  archivePrompt,
  restorePrompt,
}: {
  orgId: string
  title: string
  description: string
  downloadAllName: string
  loadRows: (view: 'active' | 'archived' | 'trash') => Promise<AxisPromptRow[]>
  replacePrompt: (row: AxisPromptRow, content: string, nextVersion: string) => Promise<unknown>
  archivePrompt: (row: AxisPromptRow) => Promise<unknown>
  restorePrompt: (row: AxisPromptRow) => Promise<unknown>
}) {
  const helpersRef = useRef<CrudHelpers<AxisPromptRow> | null>(null)
  const [viewedPrompt, setViewedPrompt] = useState<AxisPromptRow | null>(null)
  const [pendingUpload, setPendingUpload] = useState<PendingUpload | null>(null)
  const upload = useTextFileUpload<AxisPromptRow>({
    extensions: ['.md'],
    accept: '.md,text/markdown',
    emptyFileMessage: 'File is empty',
    invalidFileMessage: 'Only .md files are supported',
    onLoad: ({ context, fileName, content }) => {
      if (!context) return
      setPendingUpload({
        row: context,
        fileName,
        content,
        nextVersion: nextPromptVersion(context.version),
      })
    },
    onError: (error) => helpersRef.current?.setError(error.message),
  })

  const dataSource = useMemo(
    () => ({
      list: ({ view }: { view: 'active' | 'archived' | 'trash' }) => loadRows(view),
      archive: archivePrompt,
      unarchive: restorePrompt,
    }),
    [archivePrompt, loadRows, restorePrompt],
  )

  async function confirmUpload() {
    if (!pendingUpload) return
    await replacePrompt(pendingUpload.row, pendingUpload.content, pendingUpload.nextVersion)
    setPendingUpload(null)
    await helpersRef.current?.reload()
  }

  return (
    <div className="axis-crud-host">
      <input aria-label={`Upload ${title}`} ref={upload.inputRef} {...upload.inputProps} />
      {viewedPrompt ? (
        <ReadonlyContentViewer
          title={viewedPrompt.name}
          subtitle={viewedPrompt.promptKey}
          metadata={[
            { label: 'Type', value: viewedPrompt.kind },
            { label: 'Product', value: viewedPrompt.scopeLabel || viewedPrompt.productSurface },
            { label: 'Version', value: viewedPrompt.version || '-' },
            { label: 'Enabled', value: viewedPrompt.enabled ? 'yes' : 'no' },
            { label: 'Updated', value: formatDate(viewedPrompt.updatedAt) },
          ]}
          content={viewedPrompt.promptText}
          emptyContent="No prompt content"
          closeLabel="Close"
          onClose={() => setViewedPrompt(null)}
        />
      ) : null}
      {pendingUpload ? (
        <FileUploadReview
          title="Review prompt upload"
          subtitle={`${pendingUpload.row.name} · ${pendingUpload.row.promptKey}`}
          metadata={[
            { label: 'Current version', value: pendingUpload.row.version || '-' },
            { label: 'New version', value: pendingUpload.nextVersion },
            { label: 'File', value: pendingUpload.fileName },
          ]}
          content={pendingUpload.content}
          cancelLabel="Cancel"
          confirmLabel="Upload"
          onCancel={() => setPendingUpload(null)}
          onConfirm={() => void confirmUpload()}
        />
      ) : null}
      <CrudPage<AxisPromptRow>
        label="prompt"
        labelPlural="prompts"
        labelPluralCap={title}
        supportsArchived
        supportsTrash={false}
        allowCreate={false}
        allowEdit={false}
        allowArchive
        allowUnarchive
        allowTrash={false}
        allowRestore={false}
        allowPurge={false}
        dataSource={dataSource}
        columns={[
          {
            key: 'name',
            header: 'Prompt',
            render: (_value, row) => (
              <div>
                <strong>{row.name}</strong>
                <p className="axis-muted">{row.description || description}</p>
              </div>
            ),
          },
          { key: 'promptKey', header: 'Key' },
          {
            key: 'productSurface',
            header: 'Product',
            render: (_value, row) => row.scopeLabel || row.productSurface,
          },
          { key: 'version', header: 'Version' },
          {
            key: 'enabled',
            header: 'Enabled',
            render: (value) => (value ? 'yes' : 'no'),
          },
          {
            key: 'updatedAt',
            header: 'Updated',
            render: (_value, row) => formatDate(row.updatedAt),
          },
        ]}
        formFields={[]}
        searchText={(row) => [row.name, row.promptKey, row.version, row.description].join(' ')}
        toFormValues={() => ({})}
        isValid={() => true}
        emptyState="No prompts"
        archivedEmptyState="No archived prompts"
        searchPlaceholder="Search prompts"
        toolbarActions={[
          {
            id: 'download-all',
            label: 'Download all',
            kind: 'secondary',
            onClick: (helpers) => {
              helpersRef.current = helpers
              downloadZipFile(
                downloadAllName,
                helpers.items
                  .filter((row) => row.promptText)
                  .map((row) => ({ fileName: `${safeFileName(row.promptKey)}.md`, content: row.promptText })),
              )
            },
          },
        ]}
        rowActions={[
          {
            id: 'view',
            label: 'View',
            kind: 'secondary',
            onClick: (row) => setViewedPrompt(row),
          },
          {
            id: 'download',
            label: 'Download',
            kind: 'secondary',
            isVisible: (row) => Boolean(row.promptText),
            onClick: (row) => downloadTextFile(`${safeFileName(row.promptKey)}.md`, row.promptText),
          },
          {
            id: 'upload',
            label: 'Upload',
            kind: 'success',
            onClick: (row, helpers) => {
              helpersRef.current = helpers
              upload.open(row)
            },
          },
        ]}
        featureFlags={{
          createAction: false,
          trashToggle: false,
          csvToolbar: false,
        }}
      />
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
