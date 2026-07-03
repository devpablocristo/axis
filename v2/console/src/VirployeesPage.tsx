import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useEffect, useMemo, useRef, useState, type ReactElement } from 'react'
import {
  type JobRole,
  type TenantUser,
  type VirployeeAutonomy,
  type VirployeeAutonomyLevel,
  type Virployee,
  archiveVirployee,
  createVirployee,
  listJobRoles,
  listUsers,
  listVirployeeAutonomyLevels,
  listVirployees,
  purgeVirployee,
  restoreVirployee,
  trashVirployee,
  unarchiveVirployee,
  updateVirployee,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type VirployeesPageProps = {
  tenantId: string
  principalId: string
}

const VISIBLE_AUTONOMY_LEVELS: VirployeeAutonomy[] = ['A0', 'A1', 'A2', 'A3']

const FALLBACK_AUTONOMY_LEVELS: VirployeeAutonomyLevel[] = [
  {
    level: 'A0',
    name: 'Conversation',
    description: 'Can hold conversation and read contextual information.',
    allowed_action_classes: [
      {
        class: 'observe',
        name: 'Observe',
        description: 'Read context and hold conversation.',
        requires_approval: false,
      },
    ],
  },
  {
    level: 'A1',
    name: 'Recommendation',
    description: 'Can read, analyze and recommend actions.',
    allowed_action_classes: [
      {
        class: 'observe',
        name: 'Observe',
        description: 'Read context and hold conversation.',
        requires_approval: false,
      },
      {
        class: 'recommend',
        name: 'Recommend',
        description: 'Analyze context and recommend actions.',
        requires_approval: false,
      },
    ],
  },
  {
    level: 'A2',
    name: 'Draft',
    description: 'Can prepare plans or executable drafts.',
    allowed_action_classes: [
      {
        class: 'observe',
        name: 'Observe',
        description: 'Read context and hold conversation.',
        requires_approval: false,
      },
      {
        class: 'recommend',
        name: 'Recommend',
        description: 'Analyze context and recommend actions.',
        requires_approval: false,
      },
      {
        class: 'draft',
        name: 'Draft',
        description: 'Prepare plans or executable drafts.',
        requires_approval: false,
      },
    ],
  },
  {
    level: 'A3',
    name: 'Limited execution',
    description: 'Can execute low-risk reversible writes.',
    allowed_action_classes: [
      {
        class: 'observe',
        name: 'Observe',
        description: 'Read context and hold conversation.',
        requires_approval: false,
      },
      {
        class: 'recommend',
        name: 'Recommend',
        description: 'Analyze context and recommend actions.',
        requires_approval: false,
      },
      {
        class: 'draft',
        name: 'Draft',
        description: 'Prepare plans or executable drafts.',
        requires_approval: false,
      },
      {
        class: 'write_low',
        name: 'Low-risk write',
        description: 'Execute low-risk reversible writes.',
        requires_approval: false,
      },
    ],
  },
]

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function VirployeesPage({ tenantId, principalId }: VirployeesPageProps) {
  const rootRef = useRef<HTMLElement | null>(null)
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [createRequested, setCreateRequested] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const [autonomyLevels, setAutonomyLevels] = useState<VirployeeAutonomyLevel[]>(FALLBACK_AUTONOMY_LEVELS)
  const [jobRoles, setJobRoles] = useState<JobRole[]>([])
  const [jobRolesError, setJobRolesError] = useState('')
  const [users, setUsers] = useState<TenantUser[]>([])
  const [usersError, setUsersError] = useState('')
  const isActive = Boolean(tenantId && principalId)
  const jobRoleByID = useMemo(() => {
    return new Map(jobRoles.map((jobRole) => [jobRole.id, jobRole]))
  }, [jobRoles])
  const userByID = useMemo(() => {
    return new Map(users.map((user) => [user.id, user]))
  }, [users])
  const activeSupervisorUsers = useMemo(() => {
    return users.filter((user) => user.kind !== 'invitation' && user.state === 'active')
  }, [users])
  const autonomyByLevel = useMemo(() => {
    return new Map(autonomyLevels.map((level) => [level.level, level]))
  }, [autonomyLevels])
  const autonomyOptions = useMemo(() => {
    return autonomyLevels.map((level) => ({
      label: `${level.level} - ${level.name}`,
      value: level.level,
    }))
  }, [autonomyLevels])
  const jobRoleOptions = useMemo(() => {
    return jobRoles.map((jobRole) => ({
      label: jobRole.name,
      value: jobRole.id,
    }))
  }, [jobRoles])
  const supervisorOptions = useMemo(() => {
    return activeSupervisorUsers.map((user) => ({
      label: userLabel(user),
      value: user.id,
    }))
  }, [activeSupervisorUsers])

  const dataSource: NonNullable<CrudPageProps<Virployee>['dataSource']> = useMemo(() => ({
    list: ({ view }) => isActive ? listVirployees(view, tenantId, principalId) : Promise.resolve([]),
    create: async (values) => {
      await createVirployee(virployeePayload(values), tenantId, principalId)
      setCreateOpen(false)
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
    setCreateOpen(false)
    setActionError('')
    setJobRoles([])
    setJobRolesError('')
    setUsers([])
    setUsersError('')
  }, [lifecycleView, tenantId])

  useEffect(() => {
    if (!isActive) {
      setJobRoles([])
      setJobRolesError('')
      return
    }
    let cancelled = false
    listJobRoles('active', tenantId, principalId)
      .then((items) => {
        if (cancelled) return
        setJobRoles(items)
        setJobRolesError('')
      })
      .catch((error) => {
        if (cancelled) return
        setJobRoles([])
        setJobRolesError(error instanceof Error ? error.message : 'Could not load Job Roles')
      })
    return () => {
      cancelled = true
    }
  }, [isActive, principalId, reloadVersion, tenantId])

  useEffect(() => {
    if (!isActive) {
      setUsers([])
      setUsersError('')
      return
    }
    let cancelled = false
    listUsers('active', tenantId, principalId)
      .then((items) => {
        if (cancelled) return
        setUsers(items)
        setUsersError('')
      })
      .catch((error) => {
        if (cancelled) return
        setUsers([])
        setUsersError(error instanceof Error ? error.message : 'Could not load Users')
      })
    return () => {
      cancelled = true
    }
  }, [isActive, principalId, reloadVersion, tenantId])

  useEffect(() => {
    if (!isActive) {
      setAutonomyLevels(FALLBACK_AUTONOMY_LEVELS)
      return
    }
    let cancelled = false
    listVirployeeAutonomyLevels(tenantId, principalId)
      .then((levels) => {
        if (cancelled) return
        const visible = levels.filter((level) => VISIBLE_AUTONOMY_LEVELS.includes(level.level))
        setAutonomyLevels(visible.length > 0 ? visible : FALLBACK_AUTONOMY_LEVELS)
      })
      .catch(() => {
        if (!cancelled) setAutonomyLevels(FALLBACK_AUTONOMY_LEVELS)
      })
    return () => {
      cancelled = true
    }
  }, [isActive, principalId, tenantId])

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
  }, [tenantId, lifecycleView, reloadVersion])

  useEffect(() => {
    const root = rootRef.current
    if (!root) return
    let bubbleVisible = false

    const syncAutonomyHelp = () => {
      const select = root.querySelector<HTMLSelectElement>('#crud-field-autonomy')
      if (!select) {
        hideAutonomyBubble()
        return
      }
      const raw = select.value.trim()
      let host = document.querySelector<HTMLElement>('#virployee-autonomy-help-host')
      if (!host) {
        host = document.createElement('div')
        host.id = 'virployee-autonomy-help-host'
        host.className = 'virployee-autonomy-help-host'
        document.body.appendChild(host)
      }
      const selectedAutonomy = isAutonomy(raw) ? raw : 'A1'
      const definition = autonomyByLevel.get(selectedAutonomy) ?? FALLBACK_AUTONOMY_LEVELS[1]
      host.innerHTML = autonomyBubbleMarkup(definition, raw === '')
      positionAutonomyBubble(select, host)
      host.style.display = bubbleVisible ? 'block' : 'none'
    }

    const showAutonomyBubble = () => {
      bubbleVisible = true
      syncAutonomyHelp()
    }

    const hideAutonomyBubble = () => {
      bubbleVisible = false
      const host = document.querySelector<HTMLElement>('#virployee-autonomy-help-host')
      if (host) host.style.display = 'none'
    }

    const isAutonomySelectEvent = (event: Event) => {
      const target = event.target
      return target instanceof HTMLSelectElement && target.id === 'crud-field-autonomy'
    }

    const handleChange = (event: Event) => {
      if (isAutonomySelectEvent(event)) {
        syncAutonomyHelp()
      }
    }

    const handleFocusIn = (event: Event) => {
      if (isAutonomySelectEvent(event)) {
        showAutonomyBubble()
      }
    }

    const handleFocusOut = (event: Event) => {
      if (!isAutonomySelectEvent(event)) return
      window.setTimeout(() => {
        if (document.activeElement?.id !== 'crud-field-autonomy') {
          hideAutonomyBubble()
        }
      }, 120)
    }

    const handlePointerOver = (event: Event) => {
      if (isAutonomySelectEvent(event)) {
        showAutonomyBubble()
      }
    }

    const handlePointerOut = (event: Event) => {
      if (!isAutonomySelectEvent(event)) return
      if (document.activeElement?.id !== 'crud-field-autonomy') {
        hideAutonomyBubble()
      }
    }

    const observer = new MutationObserver(syncAutonomyHelp)
    observer.observe(root, { childList: true, subtree: true })
    root.addEventListener('change', handleChange)
    root.addEventListener('input', handleChange)
    root.addEventListener('keyup', handleChange)
    root.addEventListener('mouseup', handleChange)
    root.addEventListener('focusin', handleFocusIn)
    root.addEventListener('focusout', handleFocusOut)
    root.addEventListener('mouseover', handlePointerOver)
    root.addEventListener('mouseout', handlePointerOut)
    const interval = window.setInterval(syncAutonomyHelp, 200)
    syncAutonomyHelp()

    return () => {
      observer.disconnect()
      root.removeEventListener('change', handleChange)
      root.removeEventListener('input', handleChange)
      root.removeEventListener('keyup', handleChange)
      root.removeEventListener('mouseup', handleChange)
      root.removeEventListener('focusin', handleFocusIn)
      root.removeEventListener('focusout', handleFocusOut)
      root.removeEventListener('mouseover', handlePointerOver)
      root.removeEventListener('mouseout', handlePointerOut)
      window.clearInterval(interval)
      hideAutonomyBubble()
    }
  }, [autonomyByLevel, lifecycleView, reloadVersion, tenantId])

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
      setActionError(error instanceof Error ? error.message : 'Could not run the action')
    } finally {
      setBulkBusy(false)
    }
  }

  if (!isActive) {
    return (
      <section className="page-section">
        <div className="empty-state">Select an active tenant to manage Virployees.</div>
      </section>
    )
  }

  return (
    <section ref={rootRef} className="page-section iam-control axis-crud-host iam-control--external-lifecycle">
      <CrudPage<Virployee>
        key={`virployees-${tenantId}-${lifecycleView}-${reloadVersion}`}
        dataSource={dataSource}
        stringsBase={defaultCrudStrings}
        strings={{
          actionTrash: 'Trash',
          actionPurge: 'Delete permanently',
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
        label="virployee"
        labelPlural="virployees"
        labelPluralCap="Virployees"
        createLabel="New"
        columns={virployeeColumns(selectedIds, toggleSelected, autonomyByLevel, jobRoleByID, userByID)}
        formFields={virployeeFormFields(autonomyOptions, jobRoleOptions, supervisorOptions)}
        searchText={(row) => virployeeSearchText(row, autonomyByLevel, jobRoleByID, userByID)}
        toFormValues={(row) => virployeeToFormValues(row)}
        isValid={isValidVirployeeForm}
        emptyState="No virployees"
        archivedEmptyState="No archived virployees"
        trashEmptyState="No virployees in trash"
        searchPlaceholder="Search virployees"
        listHeaderInlineSlot={() => (
          <div className="iam-control__lead-stack">
            <CreateAndBulkActions
              selectedCount={selectedIds.length}
              view={lifecycleView}
              createOpen={createOpen}
              busy={bulkBusy || !isActive}
              onCreate={() => {
                setCreateOpen(true)
                setCreateRequested(true)
              }}
              onClear={clearSelected}
              onBulkAction={(action) => void applyBulkAction(action)}
            />
            {actionError ? <p role="alert" className="iam-control__inline-error">{actionError}</p> : null}
            {jobRolesError ? <p role="alert" className="iam-control__inline-error">{jobRolesError}</p> : null}
            {usersError ? <p role="alert" className="iam-control__inline-error">{usersError}</p> : null}
            {!jobRolesError && jobRoles.length === 0 ? (
              <p className="iam-control__inline-note">Create a Job Role before creating Virployees.</p>
            ) : null}
            {!usersError && activeSupervisorUsers.length === 0 ? (
              <p className="iam-control__inline-note">Create a User before assigning a supervisor.</p>
            ) : null}
          </div>
        )}
        toolbarActions={lifecycleToolbarActions(lifecycleView, createOpen, setExternalLifecycleView)}
        featureFlags={{ csvToolbar: false }}
      />
    </section>
  )
}

function positionAutonomyBubble(select: HTMLSelectElement, host: HTMLElement) {
  const rect = select.getBoundingClientRect()
  const viewportPadding = 10
  const width = Math.min(Math.max(rect.width, 320), window.innerWidth - viewportPadding * 2)
  const left = Math.min(Math.max(rect.left, viewportPadding), window.innerWidth - width - viewportPadding)
  const top = Math.max(rect.top, viewportPadding)
  host.style.left = `${left}px`
  host.style.top = `${top}px`
  host.style.width = `${width}px`
}

function autonomyBubbleMarkup(definition: VirployeeAutonomyLevel, usesDefault: boolean): string {
  const allowedActions = definition.allowed_action_classes.map((action) => action.name).join(', ') || 'None'
  return `
    <div class="virployee-autonomy-bubble">
      <div class="virployee-autonomy-bubble__title">
        <span class="virployee-autonomy-bubble__level">${escapeHTML(definition.level)}</span>
        <strong>${escapeHTML(definition.name)}</strong>
        ${usesDefault ? '<span class="virployee-autonomy-bubble__default">Default</span>' : ''}
      </div>
      <p><span>Meaning</span>${escapeHTML(definition.description)}</p>
      <p><span>Allows</span>${escapeHTML(allowedActions)}</p>
    </div>
  `
}

function escapeHTML(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#039;')
}

function virployeeColumns(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
  autonomyByLevel: ReadonlyMap<VirployeeAutonomy, VirployeeAutonomyLevel>,
  jobRoleByID?: ReadonlyMap<string, JobRole>,
  userByID?: ReadonlyMap<string, TenantUser>,
): CrudPageProps<Virployee>['columns'] {
  return [
    selectionColumn<Virployee>(selectedIds, onToggle),
    { key: 'name', header: 'Name' },
    { key: 'job_role_id', header: 'Job Role', render: (value) => jobRoleName(String(value ?? ''), jobRoleByID) },
    { key: 'autonomy', header: 'Autonomy', render: (value) => formatAutonomy(String(value ?? ''), autonomyByLevel) },
    { key: 'supervisor_user_id', header: 'Supervisor', render: (value) => supervisorName(String(value ?? ''), userByID) },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
    { key: 'updated_at', header: 'Updated', render: (value) => formatDate(String(value ?? '')) },
  ]
}

function virployeeFormFields(
  autonomyOptions: Array<{ label: string; value: string }>,
  jobRoleOptions: Array<{ label: string; value: string }>,
  supervisorOptions: Array<{ label: string; value: string }>,
): CrudPageProps<Virployee>['formFields'] {
  return [
    { key: 'name', label: 'Name' },
    {
      key: 'job_role_id',
      label: 'Job Role',
      type: 'select' as const,
      placeholder: jobRoleOptions.length > 0 ? 'Select...' : 'Create a Job Role first',
      options: jobRoleOptions,
    },
    {
      key: 'autonomy',
      label: 'Autonomy (optional)',
      type: 'select' as const,
      placeholder: 'Default: A1 - Recommendation',
      options: autonomyOptions,
    },
    {
      key: 'supervisor_user_id',
      label: 'Supervisor',
      type: 'select' as const,
      placeholder: supervisorOptions.length > 0 ? 'Select...' : 'Create a User first',
      options: supervisorOptions,
      fullWidth: true,
    },
    { key: 'description', label: 'Description (optional)', type: 'textarea' as const, rows: 3, fullWidth: true },
  ]
}

function virployeeToFormValues(row: Virployee): CrudFormValues {
  return {
    name: row.name,
    job_role_id: row.job_role_id,
    autonomy: row.autonomy ?? 'A1',
    description: row.description ?? '',
    supervisor_user_id: row.supervisor_user_id,
  }
}

function virployeePayload(values: CrudFormValues) {
  return {
    name: stringValue(values.name),
    job_role_id: stringValue(values.job_role_id),
    description: stringValue(values.description),
    supervisor_user_id: stringValue(values.supervisor_user_id),
    autonomy: autonomyValue(values.autonomy),
  }
}

function isValidVirployeeForm(values: CrudFormValues): boolean {
  return (
    stringValue(values.name).length > 0 &&
    stringValue(values.job_role_id).length > 0 &&
    stringValue(values.supervisor_user_id).length > 0
  )
}

function virployeeSearchText(
  row: Virployee,
  autonomyByLevel: ReadonlyMap<VirployeeAutonomy, VirployeeAutonomyLevel>,
  jobRoleByID: ReadonlyMap<string, JobRole>,
  userByID: ReadonlyMap<string, TenantUser>,
): string {
  const jobRole = jobRoleByID.get(row.job_role_id)
  const supervisor = userByID.get(row.supervisor_user_id)
  return [
    row.id,
    row.name,
    row.job_role_id,
    jobRole?.name,
    jobRole?.slug,
    row.autonomy,
    formatAutonomy(row.autonomy, autonomyByLevel),
    row.description,
    row.supervisor_user_id,
    supervisor?.email,
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

function autonomyValue(value: CrudFormValues[string]): VirployeeAutonomy | '' {
  const autonomy = stringValue(value)
  return isAutonomy(autonomy) ? autonomy : ''
}

function jobRoleName(id: string, jobRoleByID?: ReadonlyMap<string, JobRole>): string {
  if (!id) return '-'
  const jobRole = jobRoleByID?.get(id)
  return jobRole?.name ?? shortId(id)
}

function supervisorName(id: string, userByID?: ReadonlyMap<string, TenantUser>): string {
  if (!id) return '-'
  const user = userByID?.get(id)
  return user ? userLabel(user) : shortId(id)
}

function userLabel(user: TenantUser): string {
  const email = stringValue(user.email)
  return email || user.id
}

function isAutonomy(value: string): value is VirployeeAutonomy {
  return ['A0', 'A1', 'A2', 'A3', 'A4', 'A5'].includes(value)
}

function formatAutonomy(
  value: string,
  autonomyByLevel: ReadonlyMap<VirployeeAutonomy, VirployeeAutonomyLevel>,
): string {
  if (!isAutonomy(value)) return value || '-'
  const definition = autonomyByLevel.get(value)
  return definition ? `${value} - ${definition.name}` : value
}

function shortId(value: string): string {
  if (!value) return '-'
  return value.length > 14 ? `${value.slice(0, 8)}...${value.slice(-4)}` : value
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
