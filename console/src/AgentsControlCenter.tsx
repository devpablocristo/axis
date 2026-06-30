import {
  CrudPage as PlatformCrudPage,
  crudStringsEs,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import type { ReactElement } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  archiveJobRole,
  archiveAgentProfile,
  axisCrudHttpClient,
  listAgentProfiles,
  listIAMTenants,
  listJobRoles,
  purgeAgentProfile,
  restoreJobRole,
  restoreAgentProfile,
  trashAgentProfile,
  upsertAgentProfile,
  upsertJobRole,
  type AxisAgentProfileView,
  type AxisAgentView,
  type AxisJobRoleView,
  type AxisTenantView,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'
type ValidationFilter = 'all' | 'approved' | 'needs_review' | 'ignored'
type AgentSection = 'agents' | 'profiles' | 'job_roles'
type ProfileCrudRow = AxisAgentProfileView & { id: string }
type JobRoleCrudRow = AxisJobRoleView & { id: string }
type AgentSelection = { orgId: string; ids: string[] }
type AgentActionError = { orgId: string; message: string }

export const VIRTUAL_EMPLOYEES_BASE_PATH = '/api/virtual-employees'

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

const AUTONOMY_OPTIONS = [
  { label: 'A1', value: 'A1' },
  { label: 'A2', value: 'A2' },
  { label: 'A3', value: 'A3' },
]

const PROFILE_AUTONOMY_OPTIONS = [
  { label: 'A0', value: 'A0' },
  { label: 'A1', value: 'A1' },
  { label: 'A2', value: 'A2' },
  { label: 'A3', value: 'A3' },
  { label: 'A4', value: 'A4' },
  { label: 'A5', value: 'A5' },
]

export function AgentsControlCenter({ orgId, tenantId }: { orgId: string; tenantId: string }) {
  const rootRef = useRef<HTMLElement | null>(null)
  const [activeSection, setActiveSection] = useState<AgentSection>('agents')
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selection, setSelection] = useState<AgentSelection>({ orgId, ids: [] })
  const [createRequested, setCreateRequested] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [axisOrgs, setAxisOrgs] = useState<AxisTenantView[]>([])
  const selectedOrgId = orgId
  const [validationFilter, setValidationFilter] = useState<ValidationFilter>('all')
  const [validationBusyId, setValidationBusyId] = useState('')
  const [agentActionError, setAgentActionError] = useState<AgentActionError>({ orgId, message: '' })
  const [agentProfiles, setAgentProfiles] = useState<AxisAgentProfileView[]>([])
  const [jobRoles, setJobRoles] = useState<AxisJobRoleView[]>([])
  const [profilesError, setProfilesError] = useState('')
  const [jobRolesError, setJobRolesError] = useState('')

  const activeOrgs = useMemo(() => axisOrgs.filter((org) => lifecycleBucket(org.status) === 'active'), [axisOrgs])
  const orgNameById = useMemo(() => new Map(activeOrgs.map((org) => [org.id, org.name])), [activeOrgs])
  const activeProfiles = useMemo(() => agentProfiles.filter((profile) => profile.enabled && !profile.archived_at), [agentProfiles])
  const profileOptions = useMemo(() => activeProfiles.map((profile) => ({
    label: `${profile.name} · ${profile.profile_id}`,
    value: profile.profile_id,
  })), [activeProfiles])
  const activeJobRoles = useMemo(() => jobRoles.filter((role) => role.status === 'active' && !role.archived_at), [jobRoles])
  const jobRoleById = useMemo(() => new Map(activeJobRoles.map((role) => [role.job_role_id, role])), [activeJobRoles])
  const jobRoleOptions = useMemo(() => [
    { label: 'Sin Job Role', value: '' },
    ...activeJobRoles.map((role) => ({
      label: `${role.name} · ${role.job_role_id}`,
      value: role.job_role_id,
    })),
  ], [activeJobRoles])
  const crudClient = useMemo(() => axisCrudHttpClient(orgId, tenantId), [orgId, tenantId])
  const isActive = selectedOrgId.length > 0 && profileOptions.length > 0
  const selectedIds = selection.orgId === selectedOrgId ? selection.ids : []
  const agentActionErrorMessage = agentActionError.orgId === selectedOrgId ? agentActionError.message : ''
  const refreshProfiles = () => setReloadVersion((current) => current + 1)

  useEffect(() => {
    void loadOrgOptions(orgId, tenantId, setAxisOrgs)
  }, [orgId, tenantId, reloadVersion])

  useEffect(() => {
    void loadProfileOptions(orgId, tenantId, setAgentProfiles, setProfilesError)
  }, [orgId, tenantId, reloadVersion, activeSection])

  useEffect(() => {
    void loadJobRoleOptions(orgId, tenantId, setJobRoles, setJobRolesError)
  }, [orgId, tenantId, reloadVersion, activeSection])

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
    setSelection((current) => {
      const currentIds = current.orgId === selectedOrgId ? current.ids : []
      const nextIds = checked ? Array.from(new Set([...currentIds, id])) : currentIds.filter((item) => item !== id)
      return { orgId: selectedOrgId, ids: nextIds }
    })
  }

  const clearSelected = () => setSelection({ orgId: selectedOrgId, ids: [] })

  const applyReviewAction = async (agent: AxisAgentView, action: 'approve' | 'ignore') => {
    if (!isActive || validationBusyId) return
    setValidationBusyId(agent.id)
    setAgentActionError({ orgId: selectedOrgId, message: '' })
    try {
      await crudClient.json(`${VIRTUAL_EMPLOYEES_BASE_PATH}/${agent.id}/${action}`, { method: 'POST', body: {} })
      setReloadVersion((current) => current + 1)
    } catch (err) {
      setAgentActionError({ orgId: selectedOrgId, message: errorMessage(err) })
    } finally {
      setValidationBusyId('')
    }
  }

  const applyBulkAction = async (action: BulkAction) => {
    if (!isActive || selectedIds.length === 0 || bulkBusy) return
    setBulkBusy(true)
    setAgentActionError({ orgId: selectedOrgId, message: '' })
    try {
      for (const id of selectedIds) {
        const method = action === 'purge' ? 'DELETE' : 'POST'
        await crudClient.json(`${VIRTUAL_EMPLOYEES_BASE_PATH}/${id}/${action}`, { method, body: {} })
      }
      clearSelected()
      setReloadVersion((current) => current + 1)
    } catch (err) {
      setAgentActionError({ orgId: selectedOrgId, message: errorMessage(err) })
    } finally {
      setBulkBusy(false)
    }
  }

  const setAgentLifecycleView = (view: CrudLifecycleView) => {
    setLifecycleView(view)
    clearSelected()
    setAgentActionError({ orgId: selectedOrgId, message: '' })
  }

  const orgSelector = (
    <div className="iam-control__below-actions">
      <label>
        <span>Validación</span>
        <select value={validationFilter} onChange={(event) => setValidationFilter(event.target.value as ValidationFilter)}>
          <option value="all">Todas</option>
          <option value="approved">Aprobadas</option>
          <option value="needs_review">Pendientes</option>
          <option value="ignored">Ignoradas</option>
        </select>
      </label>
    </div>
  )

  return (
    <section
      ref={rootRef}
      className={`page-section iam-control axis-crud-host${activeSection === 'agents' ? ' iam-control--external-lifecycle' : ''}`}
    >
      <div className="screen-nav agents-section-tabs">
        <button type="button" className={activeSection === 'agents' ? 'active' : ''} onClick={() => setActiveSection('agents')}>Virtual Employees</button>
        <button type="button" className={activeSection === 'profiles' ? 'active' : ''} onClick={() => setActiveSection('profiles')}>Perfiles</button>
        <button type="button" className={activeSection === 'job_roles' ? 'active' : ''} onClick={() => setActiveSection('job_roles')}>Job Roles</button>
      </div>
      {activeSection === 'agents' ? (
        <CrudPage<AxisAgentView>
          key={`agents-${selectedOrgId}-${lifecycleView}-${reloadVersion}`}
          basePath={VIRTUAL_EMPLOYEES_BASE_PATH}
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
          label="virtual employee"
          labelPlural="virtual employees"
          labelPluralCap="Virtual Employees"
          createLabel="Nuevo"
          columns={agentColumns(selectedIds, toggleSelected, orgNameById, validationBusyId, (agent, action) => void applyReviewAction(agent, action))}
          formFields={agentFormFields(profileOptions, jobRoleOptions)}
          preSearchFilter={(items) => validationFilter === 'all' ? items : items.filter((item) => normalizeValidationStatus(item) === validationFilter)}
          searchText={(row) => [
            orgNameById.get(row.org_id) ?? row.org_id,
            row.name,
            semanticString(row.metadata, 'job_title'),
            semanticString(row.metadata, 'job_role_id'),
            semanticString(row.metadata, 'mission'),
            semanticString(row.metadata, 'owner_user_id'),
            ...semanticList(row.metadata, 'responsibilities'),
            ...semanticList(row.metadata, 'contact_channels'),
            ...semanticList(row.metadata, 'escalation_rules'),
            row.profile,
            row.autonomy,
            row.description,
            row.origin_kind,
            row.validation_status,
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
            _metadata_json: jsonText(row.metadata) || '{}',
            job_role_id: semanticString(row.metadata, 'job_role_id'),
            job_title: semanticString(row.metadata, 'job_title'),
            mission: semanticString(row.metadata, 'mission'),
            responsibilities: semanticList(row.metadata, 'responsibilities').join('\n'),
            owner_user_id: semanticString(row.metadata, 'owner_user_id'),
            contact_channels: semanticList(row.metadata, 'contact_channels').join('\n'),
            escalation_rules: semanticList(row.metadata, 'escalation_rules').join('\n'),
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
            metadata: virtualEmployeeMetadata(values, jobRoleById.get(stringValue(values.job_role_id)), shouldApplyJobRoleDefaults(values)),
            capabilities: splitList(values.capabilities).length > 0 || !shouldApplyJobRoleDefaults(values)
              ? splitList(values.capabilities)
              : stringList(jobRoleById.get(stringValue(values.job_role_id))?.recommended_capabilities),
            tools: splitList(values.tools),
          })}
          isValid={(values) => isActive && stringValue(values.name).length > 0 && stringValue(values.profile).length > 0}
          emptyState={profileOptions.length > 0 ? 'Sin virtual employees' : 'Sin perfiles disponibles'}
          archivedEmptyState="Sin virtual employees archivados"
          trashEmptyState="Sin virtual employees en papelera"
          searchPlaceholder="Buscar virtual employees"
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
              {agentActionErrorMessage && <p role="alert" className="iam-control__inline-error">{agentActionErrorMessage}</p>}
              {profilesError && <p className="iam-control__inline-error">{profilesError}</p>}
              {jobRolesError && <p className="iam-control__inline-error">{jobRolesError}</p>}
            </div>
          )}
          toolbarActions={lifecycleToolbarActions(lifecycleView, setAgentLifecycleView)}
          featureFlags={{ csvToolbar: false }}
        />
      ) : activeSection === 'profiles' ? (
        <AgentProfilesCrud orgId={orgId} tenantId={tenantId} onChanged={refreshProfiles} />
      ) : (
        <JobRolesCrud orgId={orgId} tenantId={tenantId} onChanged={refreshProfiles} />
      )}
    </section>
  )
}

function AgentProfilesCrud({ orgId, tenantId, onChanged }: { orgId: string; tenantId: string; onChanged: () => void }) {
  const rootRef = useRef<HTMLDivElement | null>(null)
  const [profileView, setProfileView] = useState<CrudLifecycleView>('active')
  const [selectedProfileIds, setSelectedProfileIds] = useState<string[]>([])
  const [createProfileRequested, setCreateProfileRequested] = useState(false)
  const [profileBulkBusy, setProfileBulkBusy] = useState(false)
  const dataSource: NonNullable<CrudPageProps<ProfileCrudRow>['dataSource']> = {
    async list({ view }) {
      const profiles = await listAgentProfiles(orgId, view, tenantId)
      return profiles.map(profileToRow)
    },
    async create(values) {
      const profileId = stringValue(values.profile_id)
      await upsertAgentProfile(orgId, profileId, profilePayload(values, true), tenantId)
      onChanged()
    },
    async update(row, values) {
      await upsertAgentProfile(orgId, row.profile_id, profilePayload(values, row.enabled), tenantId)
      onChanged()
    },
    async archive(row) {
      await archiveAgentProfile(orgId, row.profile_id, tenantId)
      onChanged()
    },
    async trash(row) {
      await trashAgentProfile(orgId, row.profile_id, tenantId)
      onChanged()
    },
    async unarchive(row) {
      await restoreAgentProfile(orgId, row.profile_id, tenantId)
      onChanged()
    },
    async restore(row) {
      await restoreAgentProfile(orgId, row.profile_id, tenantId)
      onChanged()
    },
    async purge(row) {
      await purgeAgentProfile(orgId, row.profile_id, tenantId)
      onChanged()
    },
  }

  useEffect(() => {
    if (!createProfileRequested) return
    const handle = window.setTimeout(() => {
      const buttons = Array.from(rootRef.current?.querySelectorAll<HTMLButtonElement>('.crud-page-shell__header-actions > .actions-row > .actions-row > button') ?? [])
      buttons.find((button) => button.textContent?.trim() === 'Nuevo')?.click()
      setCreateProfileRequested(false)
    }, 0)
    return () => window.clearTimeout(handle)
  }, [createProfileRequested, profileView])

  const toggleSelectedProfile = (id: string, checked: boolean) => {
    setSelectedProfileIds((current) => checked ? Array.from(new Set([...current, id])) : current.filter((item) => item !== id))
  }

  const clearSelectedProfiles = () => setSelectedProfileIds([])

  const setProfileLifecycleView = (view: CrudLifecycleView) => {
    setProfileView(view)
    clearSelectedProfiles()
  }

  const applyProfileBulkAction = async (action: BulkAction) => {
    if (selectedProfileIds.length === 0 || profileBulkBusy) return
    setProfileBulkBusy(true)
    try {
      for (const profileId of selectedProfileIds) {
        if (action === 'archive') {
          await archiveAgentProfile(orgId, profileId, tenantId)
        } else if (action === 'trash') {
          await trashAgentProfile(orgId, profileId, tenantId)
        } else if (action === 'restore') {
          await restoreAgentProfile(orgId, profileId, tenantId)
        } else {
          await purgeAgentProfile(orgId, profileId, tenantId)
        }
      }
      clearSelectedProfiles()
      onChanged()
      setProfileView(action === 'archive' ? 'archived' : action === 'trash' || action === 'purge' ? 'trash' : 'active')
    } finally {
      setProfileBulkBusy(false)
    }
  }

  return (
    <div ref={rootRef} className="agent-profiles-crud">
      <CrudPage<ProfileCrudRow>
        key={`profiles-${profileView}`}
        dataSource={dataSource}
        stringsBase={crudStringsEs}
        strings={{ actionUnarchive: 'Restaurar' }}
        initialView={profileView}
        supportsArchived
        supportsTrash
        allowCreate
        allowEdit
        allowArchive
        allowUnarchive
        allowTrash
        allowRestore
        allowPurge
        label="perfil"
        labelPlural="perfiles"
        labelPluralCap="Perfiles"
        createLabel="Nuevo"
        columns={profileColumns(selectedProfileIds, toggleSelectedProfile)}
        formFields={profileFormFields()}
        searchText={(row) => [
          row.name,
          row.profile_id,
          row.family_id,
          row.version_label,
          row.description,
          row.system_prompt,
          row.max_autonomy,
          ...stringList(row.allowed_tools),
          ...stringList(row.allowed_capabilities),
        ].join(' ')}
        toFormValues={profileToFormValues}
        isValid={(values) => (
          stringValue(values.profile_id).length > 0
          && stringValue(values.family_id).length > 0
          && stringValue(values.version_label).length > 0
          && stringValue(values.name).length > 0
          && stringValue(values.system_prompt).length > 0
          && stringValue(values.max_autonomy).length > 0
        )}
        emptyState="Sin perfiles"
        archivedEmptyState="Sin perfiles archivados"
        trashEmptyState="Sin perfiles en papelera"
        searchPlaceholder="Buscar perfiles"
        listHeaderInlineSlot={() => (
          <div className="iam-control__lead-stack">
            <ProfileCreateAndBulkActions
              selectedCount={selectedProfileIds.length}
              view={profileView}
              busy={profileBulkBusy}
              onCreate={() => setCreateProfileRequested(true)}
              onClear={clearSelectedProfiles}
              onBulkAction={(action) => void applyProfileBulkAction(action)}
            />
          </div>
        )}
        toolbarActions={[
          { id: 'active', label: 'Activos', kind: profileView === 'active' ? 'primary' as const : 'secondary' as const, onClick: () => setProfileLifecycleView('active') },
          { id: 'archived', label: 'Archivados', kind: profileView === 'archived' ? 'primary' as const : 'secondary' as const, onClick: () => setProfileLifecycleView('archived') },
          { id: 'trash', label: 'Papelera', kind: profileView === 'trash' ? 'primary' as const : 'secondary' as const, onClick: () => setProfileLifecycleView('trash') },
        ]}
        featureFlags={{ csvToolbar: false, archivedToggle: false, trashToggle: false }}
      />
    </div>
  )
}

function JobRolesCrud({ orgId, tenantId, onChanged }: { orgId: string; tenantId: string; onChanged: () => void }) {
  const rootRef = useRef<HTMLDivElement | null>(null)
  const [roleView, setRoleView] = useState<CrudLifecycleView>('active')
  const [selectedRoleIds, setSelectedRoleIds] = useState<string[]>([])
  const [createRoleRequested, setCreateRoleRequested] = useState(false)
  const [roleBulkBusy, setRoleBulkBusy] = useState(false)
  const dataSource: NonNullable<CrudPageProps<JobRoleCrudRow>['dataSource']> = {
    async list({ view }) {
      const roles = await listJobRoles(orgId, view === 'archived' ? 'archived' : 'active', tenantId)
      return roles.map(jobRoleToRow)
    },
    async create(values) {
      const jobRoleId = stringValue(values.job_role_id)
      await upsertJobRole(orgId, jobRoleId, jobRolePayload(values), tenantId)
      onChanged()
    },
    async update(row, values) {
      await upsertJobRole(orgId, row.job_role_id, jobRolePayload(values), tenantId)
      onChanged()
    },
    async archive(row) {
      await archiveJobRole(orgId, row.job_role_id, tenantId)
      onChanged()
    },
    async unarchive(row) {
      await restoreJobRole(orgId, row.job_role_id, tenantId)
      onChanged()
    },
    async restore(row) {
      await restoreJobRole(orgId, row.job_role_id, tenantId)
      onChanged()
    },
  }

  useEffect(() => {
    if (!createRoleRequested) return
    const handle = window.setTimeout(() => {
      const buttons = Array.from(rootRef.current?.querySelectorAll<HTMLButtonElement>('.crud-page-shell__header-actions > .actions-row > .actions-row > button') ?? [])
      buttons.find((button) => button.textContent?.trim() === 'Nuevo')?.click()
      setCreateRoleRequested(false)
    }, 0)
    return () => window.clearTimeout(handle)
  }, [createRoleRequested, roleView])

  const toggleSelectedRole = (id: string, checked: boolean) => {
    setSelectedRoleIds((current) => checked ? Array.from(new Set([...current, id])) : current.filter((item) => item !== id))
  }

  const clearSelectedRoles = () => setSelectedRoleIds([])

  const setRoleLifecycleView = (view: CrudLifecycleView) => {
    setRoleView(view)
    clearSelectedRoles()
  }

  const applyRoleBulkAction = async (action: 'archive' | 'restore') => {
    if (selectedRoleIds.length === 0 || roleBulkBusy) return
    setRoleBulkBusy(true)
    try {
      for (const roleId of selectedRoleIds) {
        if (action === 'archive') {
          await archiveJobRole(orgId, roleId, tenantId)
        } else {
          await restoreJobRole(orgId, roleId, tenantId)
        }
      }
      clearSelectedRoles()
      onChanged()
      setRoleView(action === 'archive' ? 'archived' : 'active')
    } finally {
      setRoleBulkBusy(false)
    }
  }

  return (
    <div ref={rootRef} className="job-roles-crud">
      <CrudPage<JobRoleCrudRow>
        key={`job-roles-${roleView}`}
        dataSource={dataSource}
        stringsBase={crudStringsEs}
        strings={{ actionUnarchive: 'Restaurar' }}
        initialView={roleView}
        supportsArchived
        allowCreate
        allowEdit
        allowArchive
        allowUnarchive
        allowRestore
        label="job role"
        labelPlural="job roles"
        labelPluralCap="Job Roles"
        createLabel="Nuevo"
        columns={jobRoleColumns(selectedRoleIds, toggleSelectedRole)}
        formFields={jobRoleFormFields()}
        searchText={(row) => [
          row.name,
          row.job_role_id,
          row.slug,
          row.description,
          row.mission,
          row.default_autonomy_level,
          row.default_permission_bundle_id,
          ...stringList(row.recommended_capabilities),
          ...stringList(row.success_criteria),
          ...responsibilitySearchText(row.responsibilities),
        ].join(' ')}
        toFormValues={jobRoleToFormValues}
        isValid={(values) => (
          stringValue(values.job_role_id).length > 0
          && stringValue(values.name).length > 0
          && stringValue(values.default_autonomy_level).length > 0
        )}
        emptyState="Sin job roles"
        archivedEmptyState="Sin job roles archivados"
        searchPlaceholder="Buscar job roles"
        listHeaderInlineSlot={() => (
          <div className="iam-control__lead-stack">
            <JobRoleCreateAndBulkActions
              selectedCount={selectedRoleIds.length}
              view={roleView}
              busy={roleBulkBusy}
              onCreate={() => setCreateRoleRequested(true)}
              onClear={clearSelectedRoles}
              onBulkAction={(action) => void applyRoleBulkAction(action)}
            />
          </div>
        )}
        toolbarActions={[
          { id: 'active', label: 'Activos', kind: roleView === 'active' ? 'primary' as const : 'secondary' as const, onClick: () => setRoleLifecycleView('active') },
          { id: 'archived', label: 'Archivados', kind: roleView === 'archived' ? 'primary' as const : 'secondary' as const, onClick: () => setRoleLifecycleView('archived') },
        ]}
        featureFlags={{ csvToolbar: false, archivedToggle: false, trashToggle: false }}
      />
    </div>
  )
}

function agentColumns(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
  orgNameById: Map<string, string>,
  validationBusyId: string,
  onReviewAction: (agent: AxisAgentView, action: 'approve' | 'ignore') => void,
): CrudPageProps<AxisAgentView>['columns'] {
  return [
    selectionColumn<AxisAgentView>(selectedIds, onToggle),
    { key: 'org_id', header: 'Org', render: (value) => orgNameById.get(String(value ?? '')) ?? String(value ?? '-') },
    { key: 'name', header: 'Nombre' },
    { key: 'metadata', header: 'Puesto', render: (_value, row) => semanticString(row.metadata, 'job_title') || '-' },
    { key: 'profile', header: 'Perfil', render: (value) => formatProfile(String(value ?? '')) },
    { key: 'autonomy', header: 'Autonomía' },
    { key: 'source_org_id', header: 'Contexto', render: (_value, row) => formatOrigin(row) },
    { key: 'origin_kind', header: 'Origen', render: (value) => formatOriginKind(String(value ?? '')) },
    { key: 'validation_status', header: 'Validación', render: (_value, row) => (
      <ValidationCell agent={row} busy={validationBusyId === row.id} onAction={onReviewAction} />
    ) },
    { key: 'status', header: 'Estado', render: (value) => formatStatus(String(value ?? '')) },
  ]
}

function ValidationCell(props: {
  agent: AxisAgentView
  busy: boolean
  onAction: (agent: AxisAgentView, action: 'approve' | 'ignore') => void
}) {
  const status = normalizeValidationStatus(props.agent)
  if (status === 'approved') {
    return <span className="agent-validation-cell agent-validation-cell--muted">Aprobado</span>
  }
  return (
    <div className="agent-validation-cell">
      <span>{formatValidationStatus(status)}</span>
      {status === 'needs_review' && (
        <div className="agent-validation-cell__actions">
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

function agentFormFields(
  profileOptions: Array<{ label: string; value: string }>,
  jobRoleOptions: Array<{ label: string; value: string }>,
): CrudPageProps<AxisAgentView>['formFields'] {
  return [
    { key: 'name', label: 'Nombre', required: true },
    { key: 'profile', label: 'Perfil', type: 'select' as const, required: true, options: profileOptions },
    { key: 'job_role_id', label: 'Job Role', type: 'select' as const, options: jobRoleOptions },
    { key: 'autonomy', label: 'Autonomía', type: 'select' as const, required: true, options: AUTONOMY_OPTIONS },
    { key: 'memory_enabled', label: 'Memoria', type: 'checkbox' as const },
    { key: 'description', label: 'Descripción', type: 'textarea' as const, rows: 3, fullWidth: true },
    { key: 'job_title', label: 'Puesto / Job title' },
    { key: 'mission', label: 'Misión', type: 'textarea' as const, rows: 2, fullWidth: true },
    { key: 'responsibilities', label: 'Responsabilidades', type: 'textarea' as const, rows: 3, fullWidth: true },
    { key: 'owner_user_id', label: 'Owner humano' },
    { key: 'contact_channels', label: 'Canales de contacto', type: 'textarea' as const, rows: 2, fullWidth: true },
    { key: 'escalation_rules', label: 'Reglas de escalamiento', type: 'textarea' as const, rows: 2, fullWidth: true },
    { key: 'capabilities', label: 'Capabilities', type: 'textarea' as const, rows: 2, fullWidth: true },
    { key: 'tools', label: 'Tools', type: 'textarea' as const, rows: 2, fullWidth: true },
  ]
}

function profileColumns(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
): CrudPageProps<ProfileCrudRow>['columns'] {
  return [
    selectionColumn<ProfileCrudRow>(selectedIds, onToggle),
    { key: 'name', header: 'Nombre' },
    { key: 'profile_id', header: 'Profile ID' },
    { key: 'family_id', header: 'Familia' },
    { key: 'version_label', header: 'Versión' },
    { key: 'max_autonomy', header: 'Autonomía' },
    { key: 'enabled', header: 'Estado', render: (_value, row) => formatProfileStatus(row) },
  ]
}

function profileFormFields(): CrudPageProps<ProfileCrudRow>['formFields'] {
  return [
    { key: 'profile_id', label: 'Profile ID', required: true, createOnly: true },
    { key: 'name', label: 'Nombre', required: true },
    { key: 'family_id', label: 'Familia', required: true },
    { key: 'version_label', label: 'Versión', required: true },
    { key: 'max_autonomy', label: 'Autonomía máxima', type: 'select' as const, required: true, options: PROFILE_AUTONOMY_OPTIONS },
    { key: 'description', label: 'Descripción', type: 'textarea' as const, rows: 3, fullWidth: true },
    { key: 'system_prompt', label: 'System prompt', type: 'textarea' as const, rows: 8, fullWidth: true, required: true },
    { key: 'allowed_tools', label: 'Tools', type: 'textarea' as const, rows: 2, fullWidth: true },
    { key: 'allowed_capabilities', label: 'Capabilities', type: 'textarea' as const, rows: 2, fullWidth: true },
    { key: 'memory_policy', label: 'Memoria/config JSON', type: 'textarea' as const, rows: 4, fullWidth: true },
    { key: 'llm_config', label: 'LLM config JSON', type: 'textarea' as const, rows: 4, fullWidth: true },
  ]
}

function profileToRow(profile: AxisAgentProfileView): ProfileCrudRow {
  return { ...profile, id: profile.profile_id }
}

function profileToFormValues(row: ProfileCrudRow): CrudFormValues {
  return {
    profile_id: row.profile_id,
    name: row.name,
    family_id: row.family_id,
    version_label: row.version_label,
    description: row.description ?? '',
    system_prompt: row.system_prompt ?? '',
    max_autonomy: row.max_autonomy,
    allowed_tools: stringList(row.allowed_tools).join(', '),
    allowed_capabilities: stringList(row.allowed_capabilities).join(', '),
    memory_policy: jsonText(row.memory_policy),
    llm_config: jsonText(row.llm_config),
  }
}

function profilePayload(values: CrudFormValues, enabled: boolean): Partial<AxisAgentProfileView> {
  return {
    family_id: stringValue(values.family_id),
    version_label: stringValue(values.version_label),
    name: stringValue(values.name),
    description: stringValue(values.description),
    system_prompt: stringValue(values.system_prompt),
    max_autonomy: stringValue(values.max_autonomy),
    allowed_tools: splitList(values.allowed_tools),
    allowed_capabilities: splitList(values.allowed_capabilities),
    memory_policy: parseOptionalJSON(values.memory_policy, 'Memoria/config JSON'),
    llm_config: parseOptionalJSON(values.llm_config, 'LLM config JSON'),
    enabled,
  }
}

function jobRoleColumns(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
): CrudPageProps<JobRoleCrudRow>['columns'] {
  return [
    selectionColumn<JobRoleCrudRow>(selectedIds, onToggle),
    { key: 'name', header: 'Nombre' },
    { key: 'job_role_id', header: 'Job Role ID' },
    { key: 'slug', header: 'Slug' },
    { key: 'default_autonomy_level', header: 'Autonomía' },
    { key: 'recommended_capabilities', header: 'Capabilities', render: (value) => stringList(value).join(', ') || '-' },
    { key: 'status', header: 'Estado', render: (value) => formatStatus(String(value ?? '')) },
  ]
}

function jobRoleFormFields(): CrudPageProps<JobRoleCrudRow>['formFields'] {
  return [
    { key: 'job_role_id', label: 'Job Role ID', required: true, createOnly: true },
    { key: 'name', label: 'Nombre', required: true },
    { key: 'slug', label: 'Slug' },
    { key: 'description', label: 'Descripción', type: 'textarea' as const, rows: 3, fullWidth: true },
    { key: 'mission', label: 'Misión', type: 'textarea' as const, rows: 3, fullWidth: true },
    { key: 'responsibilities', label: 'Responsibilities JSON', type: 'textarea' as const, rows: 6, fullWidth: true },
    { key: 'recommended_capabilities', label: 'Recommended capabilities', type: 'textarea' as const, rows: 2, fullWidth: true },
    { key: 'default_autonomy_level', label: 'Autonomía default', type: 'select' as const, required: true, options: PROFILE_AUTONOMY_OPTIONS },
    { key: 'default_permission_bundle_id', label: 'Default permission bundle ID' },
    { key: 'success_criteria', label: 'Success criteria', type: 'textarea' as const, rows: 3, fullWidth: true },
    { key: 'default_sla_policy', label: 'Default SLA policy JSON', type: 'textarea' as const, rows: 4, fullWidth: true },
    { key: 'default_memory_policy', label: 'Default memory policy JSON', type: 'textarea' as const, rows: 4, fullWidth: true },
    { key: 'metadata', label: 'Metadata JSON', type: 'textarea' as const, rows: 4, fullWidth: true },
  ]
}

function jobRoleToRow(role: AxisJobRoleView): JobRoleCrudRow {
  return { ...role, id: role.job_role_id }
}

function jobRoleToFormValues(row: JobRoleCrudRow): CrudFormValues {
  return {
    job_role_id: row.job_role_id,
    name: row.name,
    slug: row.slug,
    description: row.description ?? '',
    mission: row.mission ?? '',
    responsibilities: JSON.stringify(row.responsibilities ?? [], null, 2),
    recommended_capabilities: stringList(row.recommended_capabilities).join(', '),
    default_autonomy_level: row.default_autonomy_level,
    default_permission_bundle_id: row.default_permission_bundle_id ?? '',
    success_criteria: stringList(row.success_criteria).join('\n'),
    default_sla_policy: jsonText(row.default_sla_policy),
    default_memory_policy: jsonText(row.default_memory_policy),
    metadata: jsonText(row.metadata),
  }
}

function jobRolePayload(values: CrudFormValues): Partial<AxisJobRoleView> {
  return {
    name: stringValue(values.name),
    slug: stringValue(values.slug),
    description: stringValue(values.description),
    mission: stringValue(values.mission),
    responsibilities: parseResponsibilitiesJSON(values.responsibilities),
    recommended_capabilities: splitList(values.recommended_capabilities),
    default_autonomy_level: stringValue(values.default_autonomy_level) || 'A2',
    default_permission_bundle_id: stringValue(values.default_permission_bundle_id),
    success_criteria: splitSemanticList(values.success_criteria),
    default_sla_policy: parseOptionalJSON(values.default_sla_policy, 'Default SLA policy JSON'),
    default_memory_policy: parseOptionalJSON(values.default_memory_policy, 'Default memory policy JSON'),
    metadata: parseOptionalJSON(values.metadata, 'Metadata JSON'),
  }
}

function parseResponsibilitiesJSON(value: unknown): AxisJobRoleView['responsibilities'] {
  const raw = stringValue(value)
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) {
      throw new Error('expected array')
    }
    return parsed.map((item) => {
      if (!item || typeof item !== 'object' || Array.isArray(item)) {
        throw new Error('expected object')
      }
      const record = item as Record<string, unknown>
      return {
        title: stringValue(record.title),
        description: stringValue(record.description),
        expected_outcome: stringValue(record.expected_outcome),
        priority: numberValue(record.priority),
      }
    }).filter((item) => item.title || item.description || item.expected_outcome)
  } catch {
    throw new Error('Responsibilities JSON inválido')
  }
}

function responsibilitySearchText(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((item) => {
    if (!item || typeof item !== 'object' || Array.isArray(item)) return []
    const record = item as Record<string, unknown>
    return [record.title, record.description, record.expected_outcome].map((part) => String(part ?? '').trim()).filter(Boolean)
  })
}

function formatProfileStatus(profile: AxisAgentProfileView): string {
  if (profile.trashed_at) return 'papelera'
  if (profile.archived_at) return 'archivado'
  if (!profile.enabled) return 'deshabilitado'
  return 'activo'
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

function ProfileCreateAndBulkActions(props: {
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

function JobRoleCreateAndBulkActions(props: {
  selectedCount: number
  view: CrudLifecycleView
  busy: boolean
  onCreate: () => void
  onClear: () => void
  onBulkAction: (action: 'archive' | 'restore') => void
}) {
  const actionsDisabled = props.busy || props.selectedCount === 0
  return (
    <div className="iam-control__create-inline">
      <div className="iam-control__bulk-buttons">
        <button type="button" className="iam-control__new-button" disabled={props.busy && props.selectedCount === 0} onClick={props.onCreate}>Nuevo</button>
        {props.view === 'active' && (
          <button type="button" disabled={actionsDisabled} onClick={() => props.onBulkAction('archive')}>Archivar</button>
        )}
        {props.view === 'archived' && (
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

async function loadOrgOptions(orgId: string, tenantId: string, setAxisOrgs: (rows: AxisTenantView[]) => void) {
  try {
    setAxisOrgs(await listIAMTenants(orgId, 'active', tenantId))
  } catch {
    setAxisOrgs([])
  }
}

async function loadProfileOptions(
  orgId: string,
  tenantId: string,
  setAgentProfiles: (rows: AxisAgentProfileView[]) => void,
  setProfilesError: (message: string) => void,
) {
  try {
    const profiles = await listAgentProfiles(orgId, 'active', tenantId)
    setAgentProfiles(profiles)
    setProfilesError('')
  } catch (err) {
    setAgentProfiles([])
    setProfilesError(err instanceof Error ? err.message : 'No se pudieron cargar los perfiles')
  }
}

async function loadJobRoleOptions(
  orgId: string,
  tenantId: string,
  setJobRoles: (rows: AxisJobRoleView[]) => void,
  setJobRolesError: (message: string) => void,
) {
  try {
    const roles = await listJobRoles(orgId, 'active', tenantId)
    setJobRoles(roles)
    setJobRolesError('')
  } catch (err) {
    setJobRoles([])
    setJobRolesError(err instanceof Error ? err.message : 'No se pudieron cargar los job roles')
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
      return 'Importado'
    case 'runtime_inferred':
      return 'Inferido'
    case 'manual':
      return 'Manual'
    default:
      return kind || '-'
  }
}

function normalizeValidationStatus(agent: AxisAgentView): ValidationFilter {
  const normalized = String(agent.validation_status ?? agent.review_status ?? '').trim().toLowerCase()
  if (normalized === 'needs_review' || normalized === 'ignored' || normalized === 'approved') return normalized
  return 'approved'
}

function formatValidationStatus(status: ValidationFilter): string {
  switch (status) {
    case 'needs_review':
      return 'Pendiente'
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

function numberValue(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  const parsed = Number.parseInt(String(value ?? '').trim(), 10)
  return Number.isFinite(parsed) ? parsed : 0
}

function shouldApplyJobRoleDefaults(values: CrudFormValues): boolean {
  return !Object.prototype.hasOwnProperty.call(values, '_metadata_json')
}

function virtualEmployeeMetadata(values: CrudFormValues, jobRole?: AxisJobRoleView, applyDefaults = false): Record<string, unknown> {
  const rawMetadata = parseMetadataJSON(values._metadata_json)
  const base = { ...rawMetadata }
  setSemanticString(base, 'job_role_id', values.job_role_id)
  setSemanticString(base, 'job_title', stringValue(values.job_title) || (applyDefaults ? jobRole?.name : '') || '')
  setSemanticString(base, 'mission', stringValue(values.mission) || (applyDefaults ? jobRole?.mission : '') || '')
  setSemanticList(base, 'responsibilities', stringValue(values.responsibilities) || (applyDefaults ? jobRoleResponsibilitiesText(jobRole) : ''))
  setSemanticString(base, 'owner_user_id', values.owner_user_id)
  setSemanticList(base, 'contact_channels', values.contact_channels)
  setSemanticList(base, 'escalation_rules', values.escalation_rules)
  return base
}

function jobRoleResponsibilitiesText(jobRole?: AxisJobRoleView): string {
  if (!jobRole?.responsibilities) return ''
  return jobRole.responsibilities.map((item) => item.title || item.description || item.expected_outcome || '').filter(Boolean).join('\n')
}

function parseMetadataJSON(value: unknown): Record<string, unknown> {
  const text = stringValue(value)
  if (!text) return {}
  try {
    const parsed = JSON.parse(text) as unknown
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>
    }
  } catch {
    return {}
  }
  return {}
}

function setSemanticString(metadata: Record<string, unknown>, key: string, value: unknown) {
  const text = stringValue(value)
  if (text) {
    metadata[key] = text
  } else {
    delete metadata[key]
  }
}

function setSemanticList(metadata: Record<string, unknown>, key: string, value: unknown) {
  const items = splitSemanticList(value)
  if (items.length > 0) {
    metadata[key] = items
  } else {
    delete metadata[key]
  }
}

function semanticString(metadata: unknown, key: string): string {
  if (!metadata || typeof metadata !== 'object' || Array.isArray(metadata)) return ''
  const value = (metadata as Record<string, unknown>)[key]
  return typeof value === 'string' ? value.trim() : ''
}

function semanticList(metadata: unknown, key: string): string[] {
  if (!metadata || typeof metadata !== 'object' || Array.isArray(metadata)) return []
  return stringList((metadata as Record<string, unknown>)[key])
}

function errorMessage(err: unknown): string {
  if (err instanceof Error && err.message.trim()) return err.message
  return 'No se pudo completar la accion'
}

function splitSemanticList(value: unknown): string[] {
  return stringValue(value)
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function splitList(value: unknown): string[] {
  return stringValue(value)
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function jsonText(value: unknown): string {
  if (!value || typeof value !== 'object') return ''
  if (Object.keys(value).length === 0) return ''
  return JSON.stringify(value, null, 2)
}

function parseOptionalJSON(value: unknown, label: string): Record<string, unknown> {
  const raw = stringValue(value)
  if (!raw) return {}
  try {
    const parsed = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('expected object')
    }
    return parsed as Record<string, unknown>
  } catch {
    throw new Error(`${label} inválido`)
  }
}
