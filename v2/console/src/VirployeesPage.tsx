import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useEffect, useMemo, useRef, useState, type ReactElement } from 'react'
import {
  type Capability,
  type JobRole,
  type TenantUser,
  type VirployeeAutonomy,
  type VirployeeAutonomyLevel,
  type Virployee,
  archiveVirployee,
  createVirployee,
  listCapabilities,
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

type VirployeeEditValues = {
  name: string
  job_role_id: string
  autonomy: VirployeeAutonomy | ''
  supervisor_user_id: string
  description: string
  capability_ids: string[]
}

const VISIBLE_AUTONOMY_LEVELS: VirployeeAutonomy[] = ['A0', 'A1', 'A2', 'A3']

const FALLBACK_AUTONOMY_LEVELS: VirployeeAutonomyLevel[] = [
  {
    level: 'A0',
    name: 'Conversation',
    description: 'Can hold conversation and read contextual information.',
    allows_required_autonomies: ['A0'],
  },
  {
    level: 'A1',
    name: 'Recommendation',
    description: 'Can read, analyze and recommend actions.',
    allows_required_autonomies: ['A0', 'A1'],
  },
  {
    level: 'A2',
    name: 'Draft',
    description: 'Can prepare plans or executable drafts.',
    allows_required_autonomies: ['A0', 'A1', 'A2'],
  },
  {
    level: 'A3',
    name: 'Limited execution',
    description: 'Can execute low-risk reversible writes.',
    allows_required_autonomies: ['A0', 'A1', 'A2', 'A3'],
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
  const [capabilities, setCapabilities] = useState<Capability[]>([])
  const [capabilitiesError, setCapabilitiesError] = useState('')
  const [editRow, setEditRow] = useState<Virployee | null>(null)
  const [editValues, setEditValues] = useState<VirployeeEditValues | null>(null)
  const [editSaving, setEditSaving] = useState(false)
  const [editError, setEditError] = useState('')
  const isActive = Boolean(tenantId && principalId)
  const jobRoleByID = useMemo(() => {
    return new Map(jobRoles.map((jobRole) => [jobRole.id, jobRole]))
  }, [jobRoles])
  const userByID = useMemo(() => {
    return new Map(users.map((user) => [user.id, user]))
  }, [users])
  const capabilityByID = useMemo(() => {
    return new Map(capabilities.map((capability) => [capability.id, capability]))
  }, [capabilities])
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
      await updateVirployee(row.id, virployeePayload(values, row.capability_ids ?? []), tenantId, principalId)
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
    setCapabilities([])
    setCapabilitiesError('')
    closeEdit()
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
      setCapabilities([])
      setCapabilitiesError('')
      return
    }
    let cancelled = false
    listCapabilities('active', tenantId, principalId)
      .then((items) => {
        if (cancelled) return
        setCapabilities(items)
        setCapabilitiesError('')
      })
      .catch((error) => {
        if (cancelled) return
        setCapabilities([])
        setCapabilitiesError(error instanceof Error ? error.message : 'Could not load Capabilities')
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
      const trigger = ensureFieldHelpTrigger(root, 'autonomy', 'Autonomy help')
      if (!select || !trigger) {
        hideAutonomyBubble()
        return
      }
      const raw = select.value.trim()
      const host = ensureHelpHost('virployee-autonomy-help-host')
      const selectedAutonomy = isAutonomy(raw) ? raw : 'A1'
      const definition = autonomyByLevel.get(selectedAutonomy) ?? FALLBACK_AUTONOMY_LEVELS[1]
      host.innerHTML = autonomyBubbleMarkup(definition, raw === '')
      positionHelpBubble(trigger, host)
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

    const handlePointerOver = (event: Event) => {
      if (helpTriggerFromEvent(event, root, 'autonomy')) {
        showAutonomyBubble()
      }
    }

    const handlePointerOut = (event: Event) => {
      const trigger = helpTriggerFromEvent(event, root, 'autonomy')
      if (!trigger) return
      const relatedTarget = event instanceof MouseEvent ? event.relatedTarget : null
      if (!(relatedTarget instanceof Node) || !trigger.contains(relatedTarget)) {
        hideAutonomyBubble()
      }
    }

    const observer = new MutationObserver(syncAutonomyHelp)
    observer.observe(root, { childList: true, subtree: true })
    root.addEventListener('mouseover', handlePointerOver)
    root.addEventListener('mouseout', handlePointerOut)
    syncAutonomyHelp()

    return () => {
      observer.disconnect()
      root.removeEventListener('mouseover', handlePointerOver)
      root.removeEventListener('mouseout', handlePointerOut)
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

  const openEdit = (row: Virployee) => {
    setEditRow(row)
    setEditValues(virployeeToEditValues(row))
    setEditError('')
    setActionError('')
  }

  const closeEdit = () => {
    setEditRow(null)
    setEditValues(null)
    setEditError('')
    setEditSaving(false)
  }

  const updateEditValue = (key: keyof VirployeeEditValues, value: string) => {
    setEditValues((current) => current ? { ...current, [key]: value } : current)
  }

  const toggleEditCapability = (id: string) => {
    setEditValues((current) => {
      if (!current) return current
      const exists = current.capability_ids.includes(id)
      return {
        ...current,
        capability_ids: exists
          ? current.capability_ids.filter((item) => item !== id)
          : [...current.capability_ids, id],
      }
    })
  }

  const saveEdit = async () => {
    if (!editRow || !editValues || editSaving || !isValidEditValues(editValues)) return
    setEditSaving(true)
    setEditError('')
    try {
      await updateVirployee(editRow.id, editPayload(editValues), tenantId, principalId)
      closeEdit()
      setReloadVersion((current) => current + 1)
    } catch (error) {
      setEditError(error instanceof Error ? error.message : 'Could not save Virployee')
    } finally {
      setEditSaving(false)
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
    <section ref={rootRef} className="page-section iam-control axis-crud-host virployees-control iam-control--external-lifecycle">
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
        columns={virployeeColumns(selectedIds, toggleSelected, autonomyByLevel, jobRoleByID, userByID, capabilityByID)}
        formFields={virployeeFormFields(autonomyOptions, jobRoleOptions, supervisorOptions)}
        searchText={(row) => virployeeSearchText(row, autonomyByLevel, jobRoleByID, userByID, capabilityByID)}
        toFormValues={virployeeToFormValues}
        onExternalEdit={openEdit}
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
            {capabilitiesError ? <p role="alert" className="iam-control__inline-error">{capabilitiesError}</p> : null}
            {!jobRolesError && jobRoles.length === 0 ? (
              <p className="iam-control__inline-note">Create a Job Role before creating Virployees.</p>
            ) : null}
            {!usersError && activeSupervisorUsers.length === 0 ? (
              <p className="iam-control__inline-note">Create a User before assigning a supervisor.</p>
            ) : null}
            {editRow && editValues ? (
              <VirployeeEditInline
                row={editRow}
                values={editValues}
                saving={editSaving}
                error={editError}
                autonomyOptions={autonomyOptions}
                jobRoleOptions={jobRoleOptions}
                supervisorOptions={supervisorOptions}
                capabilities={capabilities}
                capabilityByID={capabilityByID}
                onValueChange={updateEditValue}
                onToggleCapability={toggleEditCapability}
                onClose={closeEdit}
                onSave={() => void saveEdit()}
              />
            ) : null}
          </div>
        )}
        toolbarActions={lifecycleToolbarActions(lifecycleView, createOpen, setExternalLifecycleView)}
        featureFlags={{ csvToolbar: false }}
      />
    </section>
  )
}

function VirployeeEditInline(props: {
  row: Virployee
  values: VirployeeEditValues
  saving: boolean
  error: string
  autonomyOptions: Array<{ label: string; value: string }>
  jobRoleOptions: Array<{ label: string; value: string }>
  supervisorOptions: Array<{ label: string; value: string }>
  capabilities: Capability[]
  capabilityByID: ReadonlyMap<string, Capability>
  onValueChange: (key: keyof VirployeeEditValues, value: string) => void
  onToggleCapability: (id: string) => void
  onClose: () => void
  onSave: () => void
}) {
  const selectedIDs = props.values.capability_ids
  const selectedSet = new Set(selectedIDs)
  const availableCapabilities = props.capabilities.filter((capability) => !selectedSet.has(capability.id))

  return (
    <div className="card crud-form-card virployee-edit-inline">
      <div className="card-header">
        <h2>Edit virployee</h2>
      </div>
      <form
        className="virployee-edit-form"
        onSubmit={(event) => {
            event.preventDefault()
            props.onSave()
          }}
        >
          {props.error ? <p role="alert" className="iam-control__inline-error">{props.error}</p> : null}
          <div className="crud-form-grid">
            <label className="form-group">
              Name
              <input
                value={props.values.name}
                onChange={(event) => props.onValueChange('name', event.currentTarget.value)}
              />
            </label>
            <label className="form-group">
              Job Role
              <select
                value={props.values.job_role_id}
                onChange={(event) => props.onValueChange('job_role_id', event.currentTarget.value)}
              >
                <option value="">{props.jobRoleOptions.length > 0 ? 'Select...' : 'Create a Job Role first'}</option>
                {props.jobRoleOptions.map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
              </select>
            </label>
            <label className="form-group">
              Autonomy (optional)
              <select
                value={props.values.autonomy}
                onChange={(event) => props.onValueChange('autonomy', event.currentTarget.value)}
              >
                <option value="">Default: A1 - Recommendation</option>
                {props.autonomyOptions.map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
              </select>
            </label>
            <label className="form-group full-width">
              Supervisor
              <select
                value={props.values.supervisor_user_id}
                onChange={(event) => props.onValueChange('supervisor_user_id', event.currentTarget.value)}
              >
                <option value="">{props.supervisorOptions.length > 0 ? 'Select...' : 'Create a User first'}</option>
                {props.supervisorOptions.map((option) => (
                  <option key={option.value} value={option.value}>{option.label}</option>
                ))}
              </select>
            </label>
            <label className="form-group full-width">
              Description (optional)
              <textarea
                rows={3}
                value={props.values.description}
                onChange={(event) => props.onValueChange('description', event.currentTarget.value)}
              />
            </label>
          </div>
          <section className="capability-selector" aria-label="Capabilities">
            <label className="form-group">
              Capabilities
              <select
                value=""
                disabled={props.capabilities.length === 0 || availableCapabilities.length === 0}
                onChange={(event) => {
                  const id = event.currentTarget.value
                  if (id) props.onToggleCapability(id)
                }}
              >
                <option value="" disabled hidden>
                  {props.capabilities.length === 0
                    ? 'Create a Capability first'
                    : availableCapabilities.length === 0
                      ? 'No capabilities available'
                      : 'Select capability...'}
                </option>
                {availableCapabilities.map((capability) => (
                  <option key={capability.id} value={capability.id}>
                    {capabilityOptionLabel(capability)}
                  </option>
                ))}
              </select>
            </label>
            <p className="capability-selector__count">{selectedIDs.length} selected</p>
            <div className="capability-selector__chips" aria-label="Selected capabilities">
              {selectedIDs.length === 0 ? (
                <span className="capability-selector__empty-chip">No capabilities selected</span>
              ) : selectedIDs.map((id) => {
                const capability = props.capabilityByID.get(id)
                return (
                  <button
                    key={id}
                    type="button"
                    className="capability-chip"
                    onClick={() => props.onToggleCapability(id)}
                    title="Remove capability"
                  >
                    {capability?.name ?? shortId(id)}
                    <span aria-hidden="true">x</span>
                  </button>
                )
              })}
            </div>
          </section>
        <footer className="virployee-edit-form__footer">
            <button type="submit" className="btn-primary" disabled={props.saving || !isValidEditValues(props.values)}>
              {props.saving ? 'Saving...' : 'Save'}
            </button>
          <button type="button" className="btn-secondary" disabled={props.saving} onClick={props.onClose}>
            Cancel
          </button>
        </footer>
      </form>
    </div>
  )
}

function ensureFieldHelpTrigger(root: HTMLElement, fieldKey: string, ariaLabel: string): HTMLElement | null {
  const field = root.querySelector<HTMLElement>(`#crud-field-${fieldKey}`)
  const group = field?.closest('.form-group')
  const label = group?.querySelector<HTMLLabelElement>(`label[for="crud-field-${fieldKey}"]`)
  if (!label) return null
  const existing = label.querySelector<HTMLElement>(`.axis-field-help-trigger[data-help-field="${fieldKey}"]`)
  if (existing) return existing
  const trigger = document.createElement('span')
  trigger.className = 'axis-field-help-trigger'
  trigger.dataset.helpField = fieldKey
  trigger.textContent = '?'
  trigger.setAttribute('aria-label', ariaLabel)
  trigger.setAttribute('role', 'img')
  label.appendChild(trigger)
  return trigger
}

function helpTriggerFromEvent(event: Event, root: HTMLElement, fieldKey: string): HTMLElement | null {
  const target = event.target
  if (!(target instanceof Element)) return null
  const trigger = target.closest<HTMLElement>(`.axis-field-help-trigger[data-help-field="${fieldKey}"]`)
  if (!trigger || !root.contains(trigger)) return null
  return trigger
}

function ensureHelpHost(id: string): HTMLElement {
  let host = document.querySelector<HTMLElement>(`#${id}`)
  if (!host) {
    host = document.createElement('div')
    host.id = id
    host.className = 'axis-field-help-host'
    document.body.appendChild(host)
  }
  return host
}

function positionHelpBubble(anchor: HTMLElement, host: HTMLElement) {
  const rect = anchor.getBoundingClientRect()
  const viewportPadding = 10
  const width = Math.min(420, window.innerWidth - viewportPadding * 2)
  const left = Math.min(Math.max(rect.left - 26, viewportPadding), window.innerWidth - width - viewportPadding)
  const top = Math.max(rect.top, viewportPadding)
  host.style.left = `${left}px`
  host.style.top = `${top}px`
  host.style.width = `${width}px`
}

function autonomyBubbleMarkup(definition: VirployeeAutonomyLevel, usesDefault: boolean): string {
  const allowedAutonomies = definition.allows_required_autonomies.join(', ') || 'None'
  return `
    <div class="axis-field-help-bubble">
      <strong>Autonomy</strong>
      <p><span>Status</span>Optional. Empty uses A1 - Recommendation.</p>
      <p><span>Selected</span>${escapeHTML(definition.level)} - ${escapeHTML(definition.name)}${usesDefault ? ' (default)' : ''}</p>
      <p><span>Purpose</span>Defines how far this Virployee may go when using assigned Capabilities.</p>
      <p><span>Meaning</span>${escapeHTML(definition.description)}</p>
      <p><span>Allows</span>Capabilities requiring ${escapeHTML(allowedAutonomies)}</p>
      <p><span>Effect</span>Capabilities requiring higher autonomy cannot be assigned.</p>
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
  capabilityByID?: ReadonlyMap<string, Capability>,
): CrudPageProps<Virployee>['columns'] {
  return [
    selectionColumn<Virployee>(selectedIds, onToggle),
    { key: 'name', header: 'Name' },
    { key: 'job_role_id', header: 'Job Role', render: (value) => jobRoleName(String(value ?? ''), jobRoleByID) },
    { key: 'autonomy', header: 'Autonomy', render: (value) => formatAutonomy(String(value ?? ''), autonomyByLevel) },
    { key: 'capability_ids', header: 'Capabilities', render: (_value, row) => capabilitySummary(row.capability_ids ?? [], capabilityByID) },
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

function virployeePayload(values: CrudFormValues, capabilityIds: string[] = []) {
  return {
    name: stringValue(values.name),
    job_role_id: stringValue(values.job_role_id),
    capability_ids: capabilityIds,
    description: stringValue(values.description),
    supervisor_user_id: stringValue(values.supervisor_user_id),
    autonomy: autonomyValue(values.autonomy),
  }
}

function virployeeToEditValues(row: Virployee): VirployeeEditValues {
  return {
    name: row.name,
    job_role_id: row.job_role_id,
    autonomy: row.autonomy ?? '',
    supervisor_user_id: row.supervisor_user_id,
    description: row.description ?? '',
    capability_ids: row.capability_ids ?? [],
  }
}

function editPayload(values: VirployeeEditValues) {
  return {
    name: stringValue(values.name),
    job_role_id: stringValue(values.job_role_id),
    capability_ids: values.capability_ids,
    description: stringValue(values.description),
    supervisor_user_id: stringValue(values.supervisor_user_id),
    autonomy: values.autonomy,
  }
}

function isValidEditValues(values: VirployeeEditValues): boolean {
  return (
    stringValue(values.name).length > 0 &&
    stringValue(values.job_role_id).length > 0 &&
    stringValue(values.supervisor_user_id).length > 0
  )
}

function capabilityOptionLabel(capability: Capability): string {
  return `${capability.name} - ${capability.capability_key} - Requires ${capability.required_autonomy}`
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
  capabilityByID: ReadonlyMap<string, Capability>,
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
    row.capability_ids?.join(' '),
    row.capability_ids?.map((id) => capabilityByID.get(id)?.name ?? '').join(' '),
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

function capabilitySummary(ids: string[], capabilityByID?: ReadonlyMap<string, Capability>): string {
  if (ids.length === 0) return '-'
  const labels = ids.map((id) => capabilityByID?.get(id)?.name ?? shortId(id))
  if (labels.length <= 2) return labels.join(', ')
  return `${labels.slice(0, 2).join(', ')} +${labels.length - 2}`
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
