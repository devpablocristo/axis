import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useEffect, useMemo, useRef, useState, type ReactElement } from 'react'
import { formatDateTime24 } from './formatters'
import {
  type Approval,
  type Capability,
  type JobRole,
  type TenantUser,
  type VirployeeAutonomy,
  type VirployeeAutonomyLevel,
  type Virployee,
  type VirployeeConfirmedDraft,
  type VirployeeDryRun,
  type VirployeeExecutionGate,
  type VirployeeRunTrace,
  type VirployeeRuntimeContext,
  type ProfileTemplate,
  archiveVirployee,
  checkVirployeeExecutionGate,
  createVirployee,
  dryRunVirployee,
  getApproval,
  getVirployeeRuntimeContext,
  listCapabilities,
  listJobRoles,
  listProfileTemplates,
  listVirployeeRuns,
  listUsers,
  listVirployeeAutonomyLevels,
  listVirployees,
  purgeVirployee,
  restoreVirployee,
  simulateApprovedVirployeeExecution,
  trashVirployee,
  unarchiveVirployee,
  updateVirployee,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'
type CalendarCreateDraftValues = {
  title: string
  date_hint: string
  time: string
  attendees: string
}
type CalendarCreateDraftKey = keyof CalendarCreateDraftValues

type VirployeesPageProps = {
  tenantId: string
  principalId: string
  focusDryRunVirployeeId?: string
  onFocusDryRunConsumed?: () => void
  onReviewApproval?: (context: { approvalId: string; virployeeId: string }) => void
}

type VirployeeEditValues = {
  name: string
  job_role_id: string
  profile_template_id: string
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

const LIFECYCLE_VIEWS: CrudLifecycleView[] = ['active', 'archived', 'trash']

async function listAllLifecycle<T extends { id: string }>(
  load: (view: CrudLifecycleView) => Promise<T[]>,
): Promise<T[]> {
  const groups = await Promise.all(LIFECYCLE_VIEWS.map((view) => load(view)))
  const rowsByID = new Map<string, T>()
  for (const group of groups) {
    for (const row of group) {
      rowsByID.set(row.id, row)
    }
  }
  return [...rowsByID.values()]
}

export function VirployeesPage({
  tenantId,
  principalId,
  focusDryRunVirployeeId = '',
  onFocusDryRunConsumed,
  onReviewApproval,
}: VirployeesPageProps) {
  const rootRef = useRef<HTMLElement | null>(null)
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [createOpen, setCreateOpen] = useState(false)
  const [createValues, setCreateValues] = useState<VirployeeEditValues | null>(null)
  const [createSaving, setCreateSaving] = useState(false)
  const [createError, setCreateError] = useState('')
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const [virployeeRows, setVirployeeRows] = useState<Virployee[]>([])
  const [autonomyLevels, setAutonomyLevels] = useState<VirployeeAutonomyLevel[]>(FALLBACK_AUTONOMY_LEVELS)
  const [jobRoles, setJobRoles] = useState<JobRole[]>([])
  const [jobRolesError, setJobRolesError] = useState('')
  const [users, setUsers] = useState<TenantUser[]>([])
  const [usersError, setUsersError] = useState('')
  const [capabilities, setCapabilities] = useState<Capability[]>([])
  const [capabilitiesError, setCapabilitiesError] = useState('')
  const [profileTemplates, setProfileTemplates] = useState<ProfileTemplate[]>([])
  const [profileTemplatesError, setProfileTemplatesError] = useState('')
  const [previewRow, setPreviewRow] = useState<Virployee | null>(null)
  const [previewContext, setPreviewContext] = useState<VirployeeRuntimeContext | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState('')
  const previewRequestRef = useRef(0)
  const [dryRunRow, setDryRunRow] = useState<Virployee | null>(null)
  const [dryRunInput, setDryRunInput] = useState('')
  const [dryRunResult, setDryRunResult] = useState<VirployeeDryRun | null>(null)
  const [dryRunLoading, setDryRunLoading] = useState(false)
  const [dryRunError, setDryRunError] = useState('')
  const dryRunRequestRef = useRef(0)
  const [runTraces, setRunTraces] = useState<VirployeeRunTrace[]>([])
  const [runTracesLoading, setRunTracesLoading] = useState(false)
  const [runTracesError, setRunTracesError] = useState('')
  const runTraceRequestRef = useRef(0)
  const [executionGateResult, setExecutionGateResult] = useState<VirployeeExecutionGate | null>(null)
  const [executionGateLoading, setExecutionGateLoading] = useState(false)
  const [executionGateError, setExecutionGateError] = useState('')
  const executionGateRequestRef = useRef(0)
  const [simulationLoading, setSimulationLoading] = useState(false)
  const [simulationError, setSimulationError] = useState('')
  const simulationRequestRef = useRef(0)
  const [calendarDraftValues, setCalendarDraftValues] = useState<CalendarCreateDraftValues | null>(null)
  const [confirmedDraft, setConfirmedDraft] = useState<VirployeeConfirmedDraft | null>(null)
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
  const profileTemplateByID = useMemo(() => {
    return new Map(profileTemplates.map((profile) => [profile.id, profile]))
  }, [profileTemplates])
  const activeSupervisorUsers = useMemo(() => {
    return users.filter((user) => user.kind !== 'invitation' && user.state === 'active')
  }, [users])
  const activeJobRoles = useMemo(() => {
    return jobRoles.filter((jobRole) => jobRole.state === 'active')
  }, [jobRoles])
  const activeCapabilities = useMemo(() => {
    return capabilities.filter((capability) => capability.state === 'active')
  }, [capabilities])
  const activeProfileTemplates = useMemo(() => {
    return profileTemplates.filter((profile) => profile.state === 'active')
  }, [profileTemplates])
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
    return activeJobRoles.map((jobRole) => ({
      label: jobRole.name,
      value: jobRole.id,
    }))
  }, [activeJobRoles])
  const supervisorOptions = useMemo(() => {
    return activeSupervisorUsers.map((user) => ({
      label: userLabel(user),
      value: user.id,
    }))
  }, [activeSupervisorUsers])
  const profileTemplateOptions = useMemo(() => {
    return activeProfileTemplates.map((profile) => ({
      label: profile.name,
      value: profile.id,
    }))
  }, [activeProfileTemplates])
  const virployeeByID = useMemo(() => {
    return new Map(virployeeRows.map((virployee) => [virployee.id, virployee]))
  }, [virployeeRows])
  const selectedVirployee = selectedIds.length === 1 ? virployeeByID.get(selectedIds[0]) ?? null : null
  const inlinePanelOpen = createOpen || previewRow != null || dryRunRow != null || editRow != null

  const dataSource: NonNullable<CrudPageProps<Virployee>['dataSource']> = useMemo(() => ({
    list: async () => {
      if (!isActive) {
        setVirployeeRows([])
        return []
      }
      const rows = await listVirployees(lifecycleView, tenantId, principalId)
      setVirployeeRows(rows)
      return rows
    },
  }), [isActive, lifecycleView, principalId, tenantId])

  useEffect(() => {
    setSelectedIds([])
    setVirployeeRows([])
    closeCreate()
    setActionError('')
    setJobRoles([])
    setJobRolesError('')
    setUsers([])
    setUsersError('')
    setCapabilities([])
    setCapabilitiesError('')
    setProfileTemplates([])
    setProfileTemplatesError('')
    closePreview()
    closeDryRun()
    closeEdit()
  }, [lifecycleView, tenantId])

  useEffect(() => {
    if (!isActive) {
      setJobRoles([])
      setJobRolesError('')
      return
    }
    let cancelled = false
    listAllLifecycle((view) => listJobRoles(view, tenantId, principalId))
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
    listAllLifecycle((view) => listCapabilities(view, tenantId, principalId))
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
      setProfileTemplates([])
      setProfileTemplatesError('')
      return
    }
    let cancelled = false
    listAllLifecycle((view) => listProfileTemplates(view, tenantId, principalId))
      .then((items) => {
        if (cancelled) return
        setProfileTemplates(items)
        setProfileTemplatesError('')
      })
      .catch((error) => {
        if (cancelled) return
        setProfileTemplates([])
        setProfileTemplatesError(error instanceof Error ? error.message : 'Could not load Profile Templates')
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
    listAllLifecycle((view) => listUsers(view, tenantId, principalId))
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

  const openCreate = () => {
    closePreview()
    closeDryRun()
    closeEdit()
    setCreateValues(initialVirployeeCreateValues(jobRoleOptions, profileTemplateOptions, supervisorOptions))
    setCreateSaving(false)
    setCreateError('')
    setActionError('')
    setCreateOpen(true)
  }

  const closeCreate = () => {
    setCreateValues(null)
    setCreateSaving(false)
    setCreateError('')
    setCreateOpen(false)
  }

  const updateCreateValue = (key: keyof VirployeeEditValues, value: string) => {
    setCreateValues((current) => current ? { ...current, [key]: value } : current)
  }

  const toggleCreateCapability = (id: string) => {
    setCreateValues((current) => {
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

  const saveCreate = async () => {
    if (!createValues || createSaving || !isValidEditValues(createValues)) return
    setCreateSaving(true)
    setCreateError('')
    try {
      await createVirployee(editPayload(createValues), tenantId, principalId)
      closeCreate()
      setReloadVersion((current) => current + 1)
    } catch (error) {
      setCreateError(error instanceof Error ? error.message : 'Could not create Virployee')
    } finally {
      setCreateSaving(false)
    }
  }

  const openEdit = (row: Virployee) => {
    closeCreate()
    closePreview()
    closeDryRun()
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

  const openPreview = (row: Virployee) => {
    closeCreate()
    closeEdit()
    closeDryRun()
    const requestID = previewRequestRef.current + 1
    previewRequestRef.current = requestID
    setPreviewRow(row)
    setPreviewContext(null)
    setPreviewError('')
    setPreviewLoading(true)
    setActionError('')
    getVirployeeRuntimeContext(row.id, tenantId, principalId)
      .then((context) => {
        if (previewRequestRef.current !== requestID) return
        setPreviewContext(context)
      })
      .catch((error) => {
        if (previewRequestRef.current !== requestID) return
        setPreviewError(error instanceof Error ? error.message : 'Could not load Runtime Context')
      })
      .finally(() => {
        if (previewRequestRef.current === requestID) setPreviewLoading(false)
      })
  }

  const closePreview = () => {
    previewRequestRef.current += 1
    setPreviewRow(null)
    setPreviewContext(null)
    setPreviewLoading(false)
    setPreviewError('')
  }

  const openDryRun = (row: Virployee) => {
    closeCreate()
    closePreview()
    closeEdit()
    dryRunRequestRef.current += 1
    executionGateRequestRef.current += 1
    simulationRequestRef.current += 1
    setDryRunRow(row)
    setDryRunInput('')
    setDryRunResult(null)
    setDryRunError('')
    setDryRunLoading(false)
    setExecutionGateResult(null)
    setExecutionGateError('')
    setExecutionGateLoading(false)
    setSimulationError('')
    setSimulationLoading(false)
    setCalendarDraftValues(null)
    setConfirmedDraft(null)
    setActionError('')
    void loadRunTraces(row)
  }

  const closeDryRun = () => {
    dryRunRequestRef.current += 1
    executionGateRequestRef.current += 1
    runTraceRequestRef.current += 1
    simulationRequestRef.current += 1
    setDryRunRow(null)
    setDryRunInput('')
    setDryRunResult(null)
    setDryRunLoading(false)
    setDryRunError('')
    setRunTraces([])
    setRunTracesLoading(false)
    setRunTracesError('')
    setExecutionGateResult(null)
    setExecutionGateLoading(false)
    setExecutionGateError('')
    setSimulationLoading(false)
    setSimulationError('')
    setCalendarDraftValues(null)
    setConfirmedDraft(null)
  }

  useEffect(() => {
    if (!focusDryRunVirployeeId || !isActive) return
    if (lifecycleView !== 'active') {
      setLifecycleView('active')
      return
    }
    const row = virployeeRows.find((item) => item.id === focusDryRunVirployeeId)
    if (!row) return
    if (dryRunRow?.id !== row.id) {
      openDryRun(row)
    } else {
      void loadRunTraces(row)
    }
    onFocusDryRunConsumed?.()
  }, [dryRunRow?.id, focusDryRunVirployeeId, isActive, lifecycleView, onFocusDryRunConsumed, virployeeRows])

  const updateDryRunInput = (value: string) => {
    setDryRunInput(value)
    setDryRunResult(null)
    setExecutionGateResult(null)
    setExecutionGateError('')
    setSimulationError('')
    setCalendarDraftValues(null)
    setConfirmedDraft(null)
  }

  async function loadRunTraces(row: Virployee) {
    const requestID = runTraceRequestRef.current + 1
    runTraceRequestRef.current = requestID
    setRunTracesLoading(true)
    setRunTracesError('')
    try {
      const runs = await listVirployeeRuns(row.id, tenantId, principalId, 20)
      if (runTraceRequestRef.current !== requestID) return
      setRunTraces(runs)
    } catch (error) {
      if (runTraceRequestRef.current !== requestID) return
      setRunTraces([])
      setRunTracesError(error instanceof Error ? error.message : 'Could not load run history')
    } finally {
      if (runTraceRequestRef.current === requestID) setRunTracesLoading(false)
    }
  }

  const runDryRun = async () => {
    if (!dryRunRow || dryRunLoading || stringValue(dryRunInput).length === 0) return
    const requestID = dryRunRequestRef.current + 1
    dryRunRequestRef.current = requestID
    setDryRunLoading(true)
    setDryRunError('')
    setDryRunResult(null)
    setExecutionGateResult(null)
    setExecutionGateError('')
    setSimulationError('')
    setCalendarDraftValues(null)
    setConfirmedDraft(null)
    try {
      const result = await dryRunVirployee(dryRunRow.id, dryRunInput, tenantId, principalId)
      if (dryRunRequestRef.current !== requestID) return
      setDryRunResult(result)
      setCalendarDraftValues(calendarCreateDraftValuesFromDryRun(result))
      void loadRunTraces(dryRunRow)
    } catch (error) {
      if (dryRunRequestRef.current !== requestID) return
      setDryRunError(error instanceof Error ? error.message : 'Could not run dry run')
    } finally {
      if (dryRunRequestRef.current === requestID) setDryRunLoading(false)
    }
  }

  const checkExecutionGate = async () => {
    if (!dryRunRow || !dryRunResult || executionGateLoading || stringValue(dryRunInput).length === 0) return
    const confirmedDraftForGate = confirmedDraft ?? (
      requiresConfirmedCalendarDraft(dryRunResult) &&
      calendarDraftValues &&
      isCalendarCreateDraftComplete(calendarDraftValues)
        ? calendarConfirmedDraftFromValues(calendarDraftValues)
        : null
    )
    if (requiresConfirmedCalendarDraft(dryRunResult) && !confirmedDraftForGate) return
    const requestID = executionGateRequestRef.current + 1
    executionGateRequestRef.current = requestID
    setExecutionGateLoading(true)
    setExecutionGateError('')
    setExecutionGateResult(null)
    if (confirmedDraftForGate) setConfirmedDraft(confirmedDraftForGate)
    try {
      const result = await checkVirployeeExecutionGate(dryRunRow.id, dryRunInput, tenantId, principalId, confirmedDraftForGate ?? undefined)
      if (executionGateRequestRef.current !== requestID) return
      setExecutionGateResult(result)
      setDryRunResult(result.dry_run)
      setCalendarDraftValues(calendarCreateDraftValuesFromDryRun(result.dry_run))
      void loadRunTraces(dryRunRow)
    } catch (error) {
      if (executionGateRequestRef.current !== requestID) return
      setExecutionGateError(error instanceof Error ? error.message : 'Could not check execution gate')
    } finally {
      if (executionGateRequestRef.current === requestID) setExecutionGateLoading(false)
    }
  }

  const updateCalendarDraftValue = (key: CalendarCreateDraftKey, value: string) => {
    setCalendarDraftValues((current) => current ? { ...current, [key]: value } : current)
    setConfirmedDraft(null)
    setExecutionGateResult(null)
    setExecutionGateError('')
  }

  const confirmCalendarDraft = () => {
    if (!calendarDraftValues || !isCalendarCreateDraftComplete(calendarDraftValues)) return
    setConfirmedDraft(calendarConfirmedDraftFromValues(calendarDraftValues))
    setExecutionGateResult(null)
    setExecutionGateError('')
    setSimulationError('')
  }

  const simulateApprovedExecution = async (approvalId: string) => {
    if (!dryRunRow || simulationLoading || stringValue(approvalId).length === 0) return
    const requestID = simulationRequestRef.current + 1
    simulationRequestRef.current = requestID
    setSimulationLoading(true)
    setSimulationError('')
    try {
      await simulateApprovedVirployeeExecution(dryRunRow.id, approvalId, tenantId, principalId)
      if (simulationRequestRef.current !== requestID) return
      await loadRunTraces(dryRunRow)
    } catch (error) {
      if (simulationRequestRef.current !== requestID) return
      setSimulationError(error instanceof Error ? error.message : 'Could not simulate execution')
    } finally {
      if (simulationRequestRef.current === requestID) setSimulationLoading(false)
    }
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
    <section ref={rootRef} className="page-section iam-control axis-crud-host virployees-control">
      <CrudPage<Virployee>
        key={`virployees-${tenantId}-${lifecycleView}-${reloadVersion}`}
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
        label="virployee"
        labelPlural="virployees"
        labelPluralCap="Virployees"
        createLabel="New"
        columns={virployeeColumns(selectedIds, toggleSelected, autonomyByLevel, jobRoleByID, userByID, capabilityByID)}
        formFields={virployeeFormFields(autonomyOptions, jobRoleOptions, supervisorOptions, profileTemplateOptions)}
        searchText={(row) => virployeeSearchText(row, autonomyByLevel, jobRoleByID, userByID, capabilityByID, profileTemplateByID)}
        toFormValues={virployeeToFormValues}
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
              selectedRow={selectedVirployee}
              onCreate={openCreate}
              onEdit={openEdit}
              onPreview={openPreview}
              onDryRun={openDryRun}
              onClear={clearSelected}
              onBulkAction={(action) => void applyBulkAction(action)}
            />
            {actionError ? <p role="alert" className="iam-control__inline-error">{actionError}</p> : null}
            {jobRolesError ? <p role="alert" className="iam-control__inline-error">{jobRolesError}</p> : null}
            {usersError ? <p role="alert" className="iam-control__inline-error">{usersError}</p> : null}
            {capabilitiesError ? <p role="alert" className="iam-control__inline-error">{capabilitiesError}</p> : null}
            {profileTemplatesError ? <p role="alert" className="iam-control__inline-error">{profileTemplatesError}</p> : null}
            {createValues ? (
              <VirployeeEditInline
                title="New virployee"
                primaryLabel="Create"
                values={createValues}
                saving={createSaving}
                error={createError}
                autonomyOptions={autonomyOptions}
                jobRoleOptions={jobRoleOptions}
                profileTemplateOptions={profileTemplateOptions}
                supervisorOptions={supervisorOptions}
                capabilities={activeCapabilities}
                capabilityByID={capabilityByID}
                onValueChange={updateCreateValue}
                onToggleCapability={toggleCreateCapability}
                onClose={closeCreate}
                onSave={() => void saveCreate()}
              />
            ) : null}
            {previewRow ? (
              <VirployeePreviewInline
                row={previewRow}
                context={previewContext}
                loading={previewLoading}
                error={previewError}
                autonomyByLevel={autonomyByLevel}
                supervisor={userByID.get((previewContext?.virployee.supervisor_user_id ?? previewRow.supervisor_user_id))}
                onClose={closePreview}
              />
            ) : null}
            {dryRunRow ? (
              <VirployeeDryRunInline
                tenantId={tenantId}
                principalId={principalId}
                row={dryRunRow}
                input={dryRunInput}
                result={dryRunResult}
                loading={dryRunLoading}
                error={dryRunError}
                executionGate={executionGateResult}
                executionGateLoading={executionGateLoading}
                executionGateError={executionGateError}
                simulationLoading={simulationLoading}
                simulationError={simulationError}
                runTraces={runTraces}
                runTracesLoading={runTracesLoading}
                runTracesError={runTracesError}
                calendarDraftValues={calendarDraftValues}
                confirmedDraft={confirmedDraft}
                autonomyByLevel={autonomyByLevel}
                supervisor={userByID.get(dryRunResult?.runtime_context.virployee.supervisor_user_id ?? dryRunRow.supervisor_user_id)}
                onInputChange={updateDryRunInput}
                onRun={() => void runDryRun()}
                onCheckExecutionGate={() => void checkExecutionGate()}
                onSimulateApprovedExecution={(approvalId) => void simulateApprovedExecution(approvalId)}
                onRefreshRuns={() => void loadRunTraces(dryRunRow)}
                onReviewApproval={onReviewApproval ? (approvalId) => onReviewApproval({ approvalId, virployeeId: dryRunRow.id }) : undefined}
                onCalendarDraftValueChange={updateCalendarDraftValue}
                onConfirmCalendarDraft={confirmCalendarDraft}
                onClose={closeDryRun}
              />
            ) : null}
            {editRow && editValues ? (
              <VirployeeEditInline
                title="Edit virployee"
                primaryLabel="Save"
                values={editValues}
                saving={editSaving}
                error={editError}
                autonomyOptions={autonomyOptions}
                jobRoleOptions={jobRoleOptions}
                profileTemplateOptions={profileTemplateOptions}
                supervisorOptions={supervisorOptions}
                capabilities={activeCapabilities}
                capabilityByID={capabilityByID}
                onValueChange={updateEditValue}
                onToggleCapability={toggleEditCapability}
                onClose={closeEdit}
                onSave={() => void saveEdit()}
              />
            ) : null}
          </div>
        )}
        toolbarActions={lifecycleToolbarActions(lifecycleView, inlinePanelOpen, setExternalLifecycleView)}
        featureFlags={{ csvToolbar: false }}
      />
    </section>
  )
}

function VirployeeEditInline(props: {
  title: string
  primaryLabel: string
  values: VirployeeEditValues
  saving: boolean
  error: string
  autonomyOptions: Array<{ label: string; value: string }>
  jobRoleOptions: Array<{ label: string; value: string }>
  profileTemplateOptions: Array<{ label: string; value: string }>
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
  const prerequisiteNotes = [
    props.jobRoleOptions.length === 0 ? 'Create an active Job Role before saving Virployees.' : '',
    props.profileTemplateOptions.length === 0 ? 'Create an active Profile Template before saving Virployees.' : '',
    props.supervisorOptions.length === 0 ? 'Create an active User before assigning a supervisor.' : '',
  ].filter(Boolean)

  return (
    <div className="card crud-form-card virployee-edit-inline">
      <div className="card-header">
        <h2>{props.title}</h2>
      </div>
      <form
        className="virployee-edit-form"
        onSubmit={(event) => {
            event.preventDefault()
            props.onSave()
          }}
        >
          <div className="virployee-form-actions virployee-form-actions--top">
            <button type="submit" className="btn-primary" disabled={props.saving || !isValidEditValues(props.values)}>
              {props.saving ? 'Saving...' : props.primaryLabel}
            </button>
            <button type="button" className="btn-secondary" disabled={props.saving} onClick={props.onClose}>
              Cancel
            </button>
          </div>
          {props.error ? <p role="alert" className="iam-control__inline-error">{props.error}</p> : null}
          {prerequisiteNotes.map((note) => (
            <p key={note} className="iam-control__inline-note">{note}</p>
          ))}
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
              Profile template
              <select
                value={props.values.profile_template_id}
                onChange={(event) => props.onValueChange('profile_template_id', event.currentTarget.value)}
              >
                <option value="">{props.profileTemplateOptions.length > 0 ? 'Select profile template...' : 'Create a Profile Template first'}</option>
                {props.profileTemplateOptions.map((option) => (
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
              {props.saving ? 'Saving...' : props.primaryLabel}
            </button>
            <button type="button" className="btn-secondary" disabled={props.saving} onClick={props.onClose}>
              Cancel
            </button>
          </footer>
      </form>
    </div>
  )
}

function VirployeePreviewInline(props: {
  row: Virployee
  context: VirployeeRuntimeContext | null
  loading: boolean
  error: string
  autonomyByLevel: ReadonlyMap<VirployeeAutonomy, VirployeeAutonomyLevel>
  supervisor?: TenantUser
  onClose: () => void
}) {
  const virployee = props.context?.virployee ?? props.row
  const description = stringValue(virployee.description)
  const jobRoleNameValue = props.context?.job_role.name ?? 'Unknown Job Role'
  const jobRoleMission = stringValue(props.context?.job_role.mission ?? '')
  const profileNameValue = props.context?.profile_template.name ?? 'Unknown Profile Template'
  const profilePrompt = stringValue(props.context?.profile_template.system_prompt ?? '')
  const capabilities = props.context?.capabilities ?? []
  const supervisorValue = props.supervisor ? userLabel(props.supervisor) : 'Unknown Supervisor'

  return (
    <div className="card crud-form-card virployee-preview-inline">
      <div className="card-header">
        <h2>Virployee preview</h2>
      </div>
      <div className="virployee-panel-actions virployee-panel-actions--top">
        <button type="button" className="btn-secondary" onClick={props.onClose}>
          Close
        </button>
      </div>
      <div className="virployee-preview">
        {props.loading ? <p className="iam-control__inline-note">Loading Runtime Context...</p> : null}
        {props.error ? <p role="alert" className="iam-control__inline-error">{props.error}</p> : null}
        <section className="virployee-preview__section" aria-label="Virployee">
          <h3>{virployee.name}</h3>
          <div className="virployee-preview__grid">
            <PreviewField label="Autonomy" value={formatAutonomy(virployee.autonomy, props.autonomyByLevel)} />
            <PreviewField label="State" value={formatState(virployee.state)} />
            <PreviewField label="Supervisor" value={supervisorValue} />
            <PreviewField label="Description" value={description || '-'} />
          </div>
        </section>

        <section className="virployee-preview__section" aria-label="Job Role">
          <h3>Job Role</h3>
          <div className="virployee-preview__grid">
            <PreviewField label="Name" value={jobRoleNameValue} />
            <PreviewField label="Mission" value={jobRoleMission || '-'} />
          </div>
        </section>

        <section className="virployee-preview__section" aria-label="Profile Template">
          <h3>Profile Template</h3>
          <div className="virployee-preview__grid">
            <PreviewField label="Name" value={profileNameValue} />
            <PreviewField
              label="Max autonomy"
              value={props.context ? formatAutonomy(props.context.profile_template.max_autonomy, props.autonomyByLevel) : '-'}
            />
          </div>
          <div className="virployee-preview__prompt">
            <span>System prompt</span>
            <pre>{profilePrompt || '-'}</pre>
          </div>
        </section>

        <section className="virployee-preview__section" aria-label="Capabilities">
          <h3>Capabilities</h3>
          {capabilities.length === 0 ? (
            <p className="virployee-preview__empty">No capabilities assigned</p>
          ) : (
            <div className="virployee-preview__capabilities">
              {capabilities.map((capability) => (
                <div key={capability.id} className="virployee-preview__capability">
                  <strong>{capability.name}</strong>
                  <span>{capability.capability_key}</span>
                  <span>Requires {formatAutonomy(capability.required_autonomy, props.autonomyByLevel)}</span>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>
      <footer className="virployee-panel-footer">
        <button type="button" className="btn-secondary" onClick={props.onClose}>
          Close
        </button>
      </footer>
    </div>
  )
}

function VirployeeDryRunInline(props: {
  tenantId: string
  principalId: string
  row: Virployee
  input: string
  result: VirployeeDryRun | null
  loading: boolean
  error: string
  executionGate: VirployeeExecutionGate | null
  executionGateLoading: boolean
  executionGateError: string
  simulationLoading: boolean
  simulationError: string
  runTraces: VirployeeRunTrace[]
  runTracesLoading: boolean
  runTracesError: string
  calendarDraftValues: CalendarCreateDraftValues | null
  confirmedDraft: VirployeeConfirmedDraft | null
  autonomyByLevel: ReadonlyMap<VirployeeAutonomy, VirployeeAutonomyLevel>
  supervisor?: TenantUser
  onInputChange: (value: string) => void
  onRun: () => void
  onCheckExecutionGate: () => void
  onSimulateApprovedExecution: (approvalId: string) => void
  onRefreshRuns: () => void
  onReviewApproval?: (approvalId: string) => void
  onCalendarDraftValueChange: (key: CalendarCreateDraftKey, value: string) => void
  onConfirmCalendarDraft: () => void
  onClose: () => void
}) {
  const context = props.result?.runtime_context
  const virployee = context?.virployee ?? props.row
  const requiredCapability = props.result?.required_capability
  const capabilities = context?.capabilities ?? []
  const canRun = stringValue(props.input).length > 0 && !props.loading
  const needsConfirmedDraft = props.result ? requiresConfirmedCalendarDraft(props.result) : false
  const draftComplete = props.calendarDraftValues ? isCalendarCreateDraftComplete(props.calendarDraftValues) : false
  const canCheckGate = Boolean(props.result) &&
    canRun &&
    !props.executionGateLoading &&
    (!needsConfirmedDraft || Boolean(props.confirmedDraft) || draftComplete)
  const supervisorValue = props.supervisor ? userLabel(props.supervisor) : 'Unknown Supervisor'
  const latestGateRun = latestExecutionGateRun(props.runTraces)
  const latestApprovalID = latestGateRun?.nexus_result?.approval_id ?? ''
  const latestSimulatedRun = latestSimulatedExecutionRun(props.runTraces, latestApprovalID)
  const [latestApproval, setLatestApproval] = useState<Approval | null>(null)
  const [latestApprovalLoading, setLatestApprovalLoading] = useState(false)
  const [latestApprovalError, setLatestApprovalError] = useState('')
  const latestApprovalRequestRef = useRef(0)
  const runButtonLabel = props.loading ? 'Running...' : props.result ? 'Run again' : 'Run Dry Run'
  const gateButtonLabel = props.executionGateLoading
    ? 'Checking...'
    : props.executionGate
      ? 'Re-check execution gate'
      : 'Check execution gate'

  useEffect(() => {
    if (!latestApprovalID) {
      latestApprovalRequestRef.current += 1
      setLatestApproval(null)
      setLatestApprovalLoading(false)
      setLatestApprovalError('')
      return
    }
    void loadLatestApproval(latestApprovalID)
  }, [latestApprovalID, props.principalId, props.tenantId])

  async function loadLatestApproval(approvalId = latestApprovalID) {
    if (!approvalId) return
    const requestID = latestApprovalRequestRef.current + 1
    latestApprovalRequestRef.current = requestID
    setLatestApprovalLoading(true)
    setLatestApprovalError('')
    try {
      const approval = await getApproval(approvalId, props.tenantId, props.principalId)
      if (latestApprovalRequestRef.current !== requestID) return
      setLatestApproval(approval)
    } catch (error) {
      if (latestApprovalRequestRef.current !== requestID) return
      setLatestApproval(null)
      setLatestApprovalError(error instanceof Error ? error.message : 'Could not load approval')
    } finally {
      if (latestApprovalRequestRef.current === requestID) setLatestApprovalLoading(false)
    }
  }

  return (
    <div className="card crud-form-card virployee-dry-run-inline">
      <div className="card-header">
        <h2>Dry Run</h2>
      </div>
      <form
        className="virployee-dry-run"
        onSubmit={(event) => {
          event.preventDefault()
          props.onRun()
        }}
      >
        <div className="virployee-form-actions virployee-form-actions--top">
          <button type="submit" className="btn-primary" disabled={!canRun}>
            {runButtonLabel}
          </button>
          <button
            type="button"
            className="btn-secondary"
            disabled={!canCheckGate}
            onClick={props.onCheckExecutionGate}
          >
            {gateButtonLabel}
          </button>
          <button type="button" className="btn-secondary" disabled={props.loading || props.executionGateLoading} onClick={props.onClose}>
            Close
          </button>
        </div>
        {props.error ? <p role="alert" className="iam-control__inline-error">{props.error}</p> : null}
        {props.executionGateError ? <p role="alert" className="iam-control__inline-error">{props.executionGateError}</p> : null}
        {props.simulationError ? <p role="alert" className="iam-control__inline-error">{props.simulationError}</p> : null}
        {props.runTracesError ? <p role="alert" className="iam-control__inline-error">{props.runTracesError}</p> : null}

        <div className="virployee-run-target" aria-label="Selected virployee">
          <PreviewField label="Virployee" value={virployee.name} />
          <PreviewField label="Supervisor" value={supervisorValue} />
          <PreviewField label="Autonomy" value={formatAutonomy(virployee.autonomy, props.autonomyByLevel)} />
        </div>

        <label className="form-group full-width">
          Action input
          <textarea
            rows={3}
            value={props.input}
            placeholder="Agendá una reunión para mañana"
            onChange={(event) => props.onInputChange(event.currentTarget.value)}
          />
        </label>

        {props.result ? (
          <section className="virployee-dry-run__result" aria-label="Dry Run result">
            <section className="virployee-preview__section virployee-flow-section" aria-label="Flow status">
              <SectionHeading title="Flow status" eyebrow="Checkpoint" />
              <div className="virployee-flow-summary" aria-label="Flow summary">
                <FlowSummaryItem
                  label="Dry Run"
                  value={props.result.decision === 'allowed' ? 'Allowed' : 'Blocked'}
                  tone={props.result.decision === 'allowed' ? 'success' : 'danger'}
                />
                <FlowSummaryItem
                  label="Gate"
                  value={props.executionGate ? formatExecutionGateDecision(props.executionGate) : 'Not checked'}
                  tone={executionGateTone(props.executionGate)}
                />
                <FlowSummaryItem
                  label="Nexus"
                  value={latestGateRun ? formatNexusTrace(latestGateRun, latestApproval) : 'Not called'}
                  tone={nexusTraceTone(latestGateRun, latestApproval)}
                />
                <FlowSummaryItem
                  label="Approval"
                  value={latestGateRun && latestApprovalID ? formatApprovalTrace(latestGateRun, latestApproval) : 'None'}
                  tone={approvalTraceTone(latestGateRun, latestApproval)}
                />
                <FlowSummaryItem
                  label="Execution"
                  value={latestSimulatedRun ? 'Simulated' : latestApproval?.status === 'approved' ? 'Ready' : 'Not ready'}
                  tone={latestSimulatedRun ? 'success' : latestApproval?.status === 'approved' ? 'warning' : 'muted'}
                />
              </div>
            </section>

            <section className="virployee-preview__section" aria-label="Dry Run decision">
              <SectionHeading title="Dry Run result" eyebrow="Capability and autonomy" />
              <div className={`virployee-dry-run__decision virployee-dry-run__decision--${props.result.decision}`}>
                <strong>{props.result.decision === 'allowed' ? 'Allowed' : 'Blocked'}</strong>
                <span>{props.result.reason}</span>
              </div>
              <div className="virployee-preview__grid">
                <PreviewField
                  label="Required capability"
                  value={requiredCapability
                    ? `${requiredCapability.name || requiredCapability.capability_key}${requiredCapability.matched ? '' : ' (not assigned)'}`
                    : 'None inferred'}
                />
                <PreviewField
                  label="Required autonomy"
                  value={formatAutonomy(props.result.required_autonomy, props.autonomyByLevel)}
                />
                <PreviewField
                  label="Virployee autonomy"
                  value={formatAutonomy(props.result.virployee_autonomy, props.autonomyByLevel)}
                />
                <PreviewField label="Next step" value={props.result.next_step} />
              </div>
            </section>

            {requiresConfirmedCalendarDraft(props.result) && props.calendarDraftValues ? (
              <ConfirmableCalendarDraftView
                draft={props.result.draft}
                values={props.calendarDraftValues}
                confirmed={Boolean(props.confirmedDraft)}
                onValueChange={props.onCalendarDraftValueChange}
                onConfirm={props.onConfirmCalendarDraft}
              />
            ) : (
              <DryRunDraftView draft={props.result.draft} />
            )}
            <DryRunIntentView intent={props.result.intent} />
            {props.executionGate ? (
              <ExecutionGateView gate={props.executionGate} autonomyByLevel={props.autonomyByLevel} />
            ) : null}

            <ApprovalCheckpointView
              run={latestGateRun}
              approval={latestApproval}
              simulatedRun={latestSimulatedRun}
              loading={latestApprovalLoading}
              simulationLoading={props.simulationLoading}
              error={latestApprovalError}
              simulationError={props.simulationError}
              onRefresh={() => void loadLatestApproval()}
              onReviewApproval={props.onReviewApproval}
              onSimulateApprovedExecution={props.onSimulateApprovedExecution}
            />

            <RunTraceHistory
              tenantId={props.tenantId}
              principalId={props.principalId}
              runs={props.runTraces}
              loading={props.runTracesLoading}
              simulationLoading={props.simulationLoading}
              onReviewApproval={props.onReviewApproval}
              onSimulateApprovedExecution={props.onSimulateApprovedExecution}
              onRefresh={props.onRefreshRuns}
            />

            <details className="virployee-preview__section virployee-runtime-details">
              <summary>Runtime Context used</summary>
              <div className="virployee-preview__grid">
                <PreviewField label="Virployee" value={virployee.name} />
                <PreviewField label="Supervisor" value={supervisorValue} />
                <PreviewField label="Job Role" value={context?.job_role.name ?? 'Unknown Job Role'} />
                <PreviewField label="Profile Template" value={context?.profile_template.name ?? 'Unknown Profile Template'} />
              </div>
              <div className="virployee-preview__prompt">
                <span>System prompt</span>
                <pre>{context?.profile_template.system_prompt || '-'}</pre>
              </div>
              {capabilities.length === 0 ? (
                <p className="virployee-preview__empty">No capabilities assigned</p>
              ) : (
                <div className="virployee-preview__capabilities">
                  {capabilities.map((capability) => (
                    <div key={capability.id} className="virployee-preview__capability">
                      <strong>{capability.name}</strong>
                      <span>{capability.capability_key}</span>
                      <span>Requires {formatAutonomy(capability.required_autonomy, props.autonomyByLevel)}</span>
                    </div>
                  ))}
                </div>
              )}
            </details>
          </section>
        ) : (
          <>
            <p className="iam-control__inline-note">Dry Run checks the Runtime Context, required Capability and autonomy decision without executing anything.</p>
            <RunTraceHistory
              tenantId={props.tenantId}
              principalId={props.principalId}
              runs={props.runTraces}
              loading={props.runTracesLoading}
              simulationLoading={props.simulationLoading}
              onReviewApproval={props.onReviewApproval}
              onSimulateApprovedExecution={props.onSimulateApprovedExecution}
              onRefresh={props.onRefreshRuns}
            />
          </>
        )}

        <footer className="virployee-edit-form__footer">
          <button type="submit" className="btn-primary" disabled={!canRun}>
            {runButtonLabel}
          </button>
          <button
            type="button"
            className="btn-secondary"
            disabled={!canCheckGate}
            onClick={props.onCheckExecutionGate}
          >
            {gateButtonLabel}
          </button>
          <button type="button" className="btn-secondary" disabled={props.loading || props.executionGateLoading} onClick={props.onClose}>
            Close
          </button>
        </footer>
      </form>
    </div>
  )
}

function DryRunIntentView(props: { intent: VirployeeDryRun['intent'] }) {
  const intent = props.intent
  return (
    <section className="virployee-preview__section" aria-label="Intent">
      <SectionHeading title="Intent match" eyebrow="Parser" />
      <div className="virployee-preview__grid">
        <PreviewField label="Matched" value={intent.matched ? 'Yes' : 'No'} />
        <PreviewField label="Capability key" value={intent.capability_key || '-'} />
        <PreviewField label="Action" value={intent.action || '-'} />
        <PreviewField label="Confidence" value={formatConfidence(intent.confidence)} />
      </div>
      {intent.matched_by.length > 0 ? (
        <div className="virployee-dry-run__draft-list" aria-label="Intent matched by">
          <span>Matched by</span>
          {intent.matched_by.map((item) => (
            <div key={item} className="virployee-dry-run__draft-row">
              <span>{item}</span>
            </div>
          ))}
        </div>
      ) : null}
      {intent.rules.length > 0 ? (
        <div className="virployee-dry-run__draft-list" aria-label="Intent rules">
          <span>Rules</span>
          {intent.rules.map((rule) => (
            <div key={`${rule.type}-${rule.target}-${rule.value}`} className="virployee-dry-run__draft-row">
              <strong>{rule.type}</strong>
              <span>{rule.target}: {rule.value}</span>
            </div>
          ))}
        </div>
      ) : null}
    </section>
  )
}

function ConfirmableCalendarDraftView(props: {
  draft: VirployeeDryRun['draft']
  values: CalendarCreateDraftValues
  confirmed: boolean
  onValueChange: (key: CalendarCreateDraftKey, value: string) => void
  onConfirm: () => void
}) {
  const complete = isCalendarCreateDraftComplete(props.values)
  const reviewMessage = props.confirmed
    ? 'Draft confirmed'
    : complete
      ? 'Ready to check the gate.'
      : 'Complete the draft before checking the gate.'
  const clarifications = props.draft.missing_fields.filter((field) => {
    return isCalendarCreateDraftKey(field.key) && stringValue(props.values[field.key]).length === 0
  })
  return (
    <section className="virployee-preview__section" aria-label="Draft">
      <SectionHeading title="Draft" eyebrow="Human review" />
      {clarifications.length > 0 ? (
        <div className="virployee-dry-run__clarifications" aria-label="Needs clarification">
          <strong>Needs clarification</strong>
          {clarifications.map((field) => (
            <span key={field.key}>{clarificationQuestion(field.key)}</span>
          ))}
        </div>
      ) : null}
      <div className="virployee-preview__grid">
        <label className="form-group">
          Action
          <input value="Create calendar event" readOnly />
        </label>
        <label className="form-group">
          Title
          <input
            value={props.values.title}
            placeholder="Reunión"
            onChange={(event) => props.onValueChange('title', event.currentTarget.value)}
            required
          />
          {stringValue(props.values.title).length === 0 ? <span className="form-field-required">Required</span> : null}
        </label>
        <label className="form-group">
          Date
          <input
            value={props.values.date_hint}
            placeholder="mañana"
            onChange={(event) => props.onValueChange('date_hint', event.currentTarget.value)}
            required
          />
          {stringValue(props.values.date_hint).length === 0 ? <span className="form-field-required">Required</span> : null}
        </label>
        <label className="form-group">
          Time
          <input
            value={props.values.time}
            placeholder="15:00"
            onChange={(event) => props.onValueChange('time', event.currentTarget.value)}
            required
          />
          {stringValue(props.values.time).length === 0 ? <span className="form-field-required">Required</span> : null}
        </label>
        <label className="form-group full-width">
          Attendees
          <input
            value={props.values.attendees}
            placeholder="ana@example.com"
            onChange={(event) => props.onValueChange('attendees', event.currentTarget.value)}
            required
          />
          {stringValue(props.values.attendees).length === 0 ? <span className="form-field-required">Required</span> : null}
        </label>
      </div>
      <div className="virployee-dry-run__draft-actions">
        <button type="button" className="btn-secondary" disabled={!complete || props.confirmed} onClick={props.onConfirm}>
          Confirm draft
        </button>
        <span className={complete || props.confirmed ? 'iam-control__inline-note' : 'iam-control__inline-error'}>
          {reviewMessage}
        </span>
      </div>
    </section>
  )
}

function DryRunDraftView(props: { draft: VirployeeDryRun['draft'] }) {
  const draft = props.draft
  return (
    <section className="virployee-preview__section" aria-label="Draft">
      <SectionHeading title="Draft" eyebrow="Prepared action" />
      <div className="virployee-preview__grid">
        <PreviewField label="Status" value={formatDraftStatus(draft.status)} />
        <PreviewField label="Action" value={draft.action || '-'} />
        <PreviewField label="Kind" value={draft.kind || '-'} />
        <PreviewField label="Summary" value={draft.summary || '-'} />
      </div>
      {draft.fields.length > 0 ? (
        <div className="virployee-dry-run__draft-list" aria-label="Detected fields">
          <span>Detected fields</span>
          {draft.fields.map((field) => (
            <div key={`${field.key}-${field.value}`} className="virployee-dry-run__draft-row">
              <strong>{field.label}</strong>
              <span>{field.value}</span>
              <small>{field.source}</small>
            </div>
          ))}
        </div>
      ) : null}
      {draft.missing_fields.length > 0 ? (
        <div className="virployee-dry-run__draft-list" aria-label="Missing fields">
          <span>Missing fields</span>
          {draft.missing_fields.map((field) => (
            <div key={field.key} className="virployee-dry-run__draft-row">
              <strong>{field.label}</strong>
              <span>{field.reason}</span>
            </div>
          ))}
        </div>
      ) : null}
      {draft.notes.length > 0 ? (
        <div className="virployee-dry-run__draft-list" aria-label="Notes">
          <span>Notes</span>
          {draft.notes.map((note) => (
            <div key={note} className="virployee-dry-run__draft-row">
              <span>{note}</span>
            </div>
          ))}
        </div>
      ) : null}
    </section>
  )
}

function ExecutionGateView(props: {
  gate: VirployeeExecutionGate
  autonomyByLevel: ReadonlyMap<VirployeeAutonomy, VirployeeAutonomyLevel>
}) {
  const gate = props.gate.execution_gate
  const decisionClass = gate.decision === 'pass' ? 'allowed' : 'blocked'
  return (
    <section className="virployee-preview__section" aria-label="Execution gate">
      <SectionHeading title="Execution gate" eyebrow="Local checks" />
      <div className={`virployee-dry-run__decision virployee-dry-run__decision--${decisionClass}`}>
        <strong>{gate.decision === 'pass' ? 'Pass' : 'Blocked'}</strong>
        <span>{gate.next_step}</span>
      </div>
      <div className="virployee-preview__grid">
        <PreviewField label="Mode" value={gate.mode} />
        <PreviewField label="Will execute" value={gate.will_execute ? 'Yes' : 'No'} />
        <PreviewField
          label="Required execution autonomy"
          value={formatAutonomy(gate.required_execution_autonomy, props.autonomyByLevel)}
        />
        <PreviewField
          label="Virployee autonomy"
          value={formatAutonomy(gate.virployee_autonomy, props.autonomyByLevel)}
        />
      </div>
      <div className="virployee-dry-run__draft-list" aria-label="Execution gate checks">
        <span>Checks</span>
        {gate.checks.map((check) => (
          <div key={check.key} className="virployee-dry-run__draft-row">
            <strong>{formatGateCheckKey(check.key)}: {check.status === 'pass' ? 'Pass' : 'Blocked'}</strong>
            <span>{check.reason}</span>
          </div>
        ))}
      </div>
    </section>
  )
}

function ApprovalCheckpointView(props: {
  run: VirployeeRunTrace | null
  approval: Approval | null
  simulatedRun: VirployeeRunTrace | null
  loading: boolean
  simulationLoading: boolean
  error: string
  simulationError: string
  onRefresh: () => void
  onReviewApproval?: (approvalId: string) => void
  onSimulateApprovedExecution: (approvalId: string) => void
}) {
  const approvalID = props.run?.nexus_result?.approval_id ?? ''
  if (!approvalID) return null
  const status = props.approval?.status || props.run?.nexus_result?.approval_status || 'pending'
  const bindingHash = props.approval?.binding_hash || props.run?.nexus_result?.binding_hash || props.run?.binding_hash || ''
  const decision = props.approval?.decided_by ? formatApprovalDecision(props.approval) : 'Not decided yet'
  return (
    <section className="virployee-preview__section virployee-approval-checkpoint" aria-label="Approval checkpoint">
      <div className="virployee-approval-checkpoint__header">
        <SectionHeading title="Approval" eyebrow="Human gate" />
        <StatusBadge value={formatApprovalStatus(status)} tone={approvalStatusTone(status)} />
      </div>
      <div className="virployee-preview__grid">
        <PreviewField label="Approval" value={shortHash(approvalID)} />
        <PreviewField label="Status" value={formatApprovalStatus(status)} />
        <PreviewField label="Risk" value={props.approval?.risk_level || props.run?.nexus_result?.risk_level || '-'} />
        <PreviewField label="Binding hash" value={shortHash(bindingHash)} />
        <PreviewField label="Reason" value={props.approval?.reason || props.run?.nexus_result?.decision_reason || '-'} />
        <PreviewField label="Decision" value={decision} />
      </div>
      {props.error ? <p role="alert" className="iam-control__inline-error">{props.error}</p> : null}
      {props.simulationError ? <p role="alert" className="iam-control__inline-error">{props.simulationError}</p> : null}
      {props.simulatedRun ? (
        <p className="iam-control__inline-note">
          {props.simulatedRun.execution_result?.message || 'Simulated execution completed; no external effects were performed.'}
        </p>
      ) : null}
      <div className="virployee-approval-checkpoint__actions">
        <button type="button" className="btn-secondary" disabled={props.loading} onClick={props.onRefresh}>
          {props.loading ? 'Refreshing...' : 'Refresh approval'}
        </button>
        {props.onReviewApproval ? (
          <button type="button" className={status === 'pending' ? 'btn-primary' : 'btn-secondary'} onClick={() => props.onReviewApproval?.(approvalID)}>
            {status === 'pending' ? 'Review approval' : 'View approval'}
          </button>
        ) : null}
        {status === 'approved' && !props.simulatedRun ? (
          <button type="button" className="btn-primary" disabled={props.simulationLoading} onClick={() => props.onSimulateApprovedExecution(approvalID)}>
            {props.simulationLoading ? 'Simulating...' : 'Simulate execution'}
          </button>
        ) : null}
      </div>
    </section>
  )
}

function RunTraceHistory(props: {
  tenantId: string
  principalId: string
  runs: VirployeeRunTrace[]
  loading: boolean
  simulationLoading: boolean
  onReviewApproval?: (approvalId: string) => void
  onSimulateApprovedExecution: (approvalId: string) => void
  onRefresh: () => void
}) {
  const approvalIDs = useMemo(
    () => Array.from(new Set(props.runs.map((run) => run.nexus_result?.approval_id || run.execution_result?.approval_id).filter(Boolean) as string[])).sort(),
    [props.runs],
  )
  const approvalKey = approvalIDs.join('|')
  const [approvalByID, setApprovalByID] = useState<Record<string, Approval | null>>({})

  useEffect(() => {
    if (approvalIDs.length === 0 || !props.tenantId || !props.principalId) {
      setApprovalByID({})
      return undefined
    }
    let cancelled = false
    setApprovalByID((current) => {
      const next: Record<string, Approval | null> = {}
      for (const id of approvalIDs) {
        next[id] = current[id] ?? null
      }
      return next
    })
    void Promise.all(
      approvalIDs.map(async (id): Promise<[string, Approval | null]> => {
        try {
          return [id, await getApproval(id, props.tenantId, props.principalId)]
        } catch {
          return [id, null]
        }
      }),
    ).then((entries) => {
      if (cancelled) return
      const next: Record<string, Approval | null> = {}
      for (const [id, approval] of entries) {
        next[id] = approval
      }
      setApprovalByID(next)
    })
    return () => {
      cancelled = true
    }
  }, [approvalKey, props.tenantId, props.principalId])

  return (
    <section className="virployee-preview__section" aria-label="Run history">
      <div className="virployee-run-history__header">
        <SectionHeading title="Run history" eyebrow="Audit trail" />
        <button type="button" className="btn-secondary" disabled={props.loading} onClick={props.onRefresh}>
          {props.loading ? 'Refreshing...' : 'Refresh'}
        </button>
      </div>
      {props.loading && props.runs.length === 0 ? (
        <p className="virployee-preview__empty">Loading runs</p>
      ) : props.runs.length === 0 ? (
        <p className="virployee-preview__empty">No runs recorded</p>
      ) : (
        <div className="virployee-run-history">
          {props.runs.map((run) => {
            const approvalID = run.nexus_result?.approval_id || run.execution_result?.approval_id || ''
            const approval = approvalID ? approvalByID[approvalID] : null
            const approvalStatus = approval?.status || run.nexus_result?.approval_status || ''
            const simulatedRun = latestSimulatedExecutionRun(props.runs, approvalID)
            const canSimulateExecution = run.operation === 'execution_gate' &&
              approvalID.length > 0 &&
              approvalStatus === 'approved' &&
              !simulatedRun
            return (
              <div key={run.id} className="virployee-run-history__row">
                <div className="virployee-run-history__main">
                  <div className="virployee-run-history__title">
                    <strong>{formatRunOperation(run.operation)}</strong>
                    <StatusBadge value={formatRunDecision(run, approval)} tone={runDecisionTone(run, approval)} />
                  </div>
                  <span>{formatDate(run.created_at)}</span>
                  <small>{run.capability_key || run.intent.capability_key || 'No capability'}</small>
                  <small>{run.input_preview || shortHash(run.input_hash)}</small>
                </div>
                <div className="virployee-run-history__nexus">
                  <StatusBadge value={formatNexusTrace(run, approval)} tone={nexusTraceTone(run, approval)} />
                  {formatNexusReason(run) ? <small>{formatNexusReason(run)}</small> : null}
                  {approvalID ? <small>{formatApprovalTrace(run, approval)}</small> : null}
                  {approval?.decided_by ? <small>{formatApprovalDecision(approval)}</small> : null}
                  <small>Binding {run.binding_hash ? shortHash(run.binding_hash) : shortHash(run.input_hash)}</small>
                  {approvalID && (props.onReviewApproval || canSimulateExecution) ? (
                    <div className="virployee-run-history__actions">
                      {props.onReviewApproval ? (
                        <button
                          type="button"
                          className="btn-sm btn-secondary"
                          onClick={() => props.onReviewApproval?.(approvalID)}
                        >
                          {(approval?.status || run.nexus_result?.approval_status) === 'pending' ? 'Review approval' : 'View approval'}
                        </button>
                      ) : null}
                      {canSimulateExecution ? (
                        <button
                          type="button"
                          className="btn-sm btn-primary"
                          disabled={props.simulationLoading}
                          onClick={() => props.onSimulateApprovedExecution(approvalID)}
                        >
                          {props.simulationLoading ? 'Simulating...' : 'Simulate execution'}
                        </button>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}

type StatusTone = 'success' | 'danger' | 'warning' | 'muted'

function FlowSummaryItem(props: { label: string; value: string; tone: StatusTone }) {
  return (
    <div className="virployee-flow-summary__item">
      <span>{props.label}</span>
      <StatusBadge value={props.value} tone={props.tone} />
    </div>
  )
}

function SectionHeading(props: { title: string; eyebrow: string }) {
  return (
    <div className="virployee-section-heading">
      <span>{props.eyebrow}</span>
      <h3>{props.title}</h3>
    </div>
  )
}

function StatusBadge(props: { value: string; tone: StatusTone }) {
  return <span className={`axis-status-badge axis-status-badge--${props.tone}`}>{props.value}</span>
}

function PreviewField(props: { label: string; value: string }) {
  return (
    <div className="virployee-preview__field">
      <span>{props.label}</span>
      <strong>{props.value}</strong>
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
    { key: 'name', header: 'Name', className: 'iam-control__primary-col virployee-name-col' },
    { key: 'created_at', header: 'Created', className: 'iam-control__created-col', render: (value) => formatDateTime24(String(value ?? '')) },
    { key: 'job_role_id', header: 'Job Role', render: (value) => jobRoleName(String(value ?? ''), jobRoleByID) },
    { key: 'autonomy', header: 'Autonomy', render: (value) => formatAutonomy(String(value ?? ''), autonomyByLevel) },
    { key: 'capability_ids', header: 'Capabilities', className: 'virployee-capabilities-col', render: (_value, row) => capabilitySummary(row.capability_ids ?? [], capabilityByID) },
    { key: 'supervisor_user_id', header: 'Supervisor', render: (value) => supervisorName(String(value ?? ''), userByID) },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
  ]
}

function virployeeFormFields(
  autonomyOptions: Array<{ label: string; value: string }>,
  jobRoleOptions: Array<{ label: string; value: string }>,
  supervisorOptions: Array<{ label: string; value: string }>,
  profileTemplateOptions: Array<{ label: string; value: string }>,
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
      key: 'profile_template_id',
      label: 'Profile template',
      type: 'select' as const,
      placeholder: profileTemplateOptions.length > 0 ? 'Select profile template...' : 'Create a Profile Template first',
      options: profileTemplateOptions,
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
    profile_template_id: row.profile_template_id,
    autonomy: row.autonomy ?? 'A1',
    description: row.description ?? '',
    supervisor_user_id: row.supervisor_user_id,
  }
}

function virployeePayload(values: CrudFormValues, capabilityIds: string[] = []) {
  return {
    name: stringValue(values.name),
    job_role_id: stringValue(values.job_role_id),
    profile_template_id: stringValue(values.profile_template_id),
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
    profile_template_id: row.profile_template_id,
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
    profile_template_id: values.profile_template_id,
    capability_ids: values.capability_ids,
    description: stringValue(values.description),
    supervisor_user_id: stringValue(values.supervisor_user_id),
    autonomy: values.autonomy,
  }
}

function initialVirployeeCreateValues(
  jobRoleOptions: Array<{ label: string; value: string }>,
  profileTemplateOptions: Array<{ label: string; value: string }>,
  supervisorOptions: Array<{ label: string; value: string }>,
): VirployeeEditValues {
  return {
    name: '',
    job_role_id: jobRoleOptions.length === 1 ? jobRoleOptions[0].value : '',
    profile_template_id: profileTemplateOptions.length === 1 ? profileTemplateOptions[0].value : '',
    autonomy: 'A1',
    supervisor_user_id: supervisorOptions.length === 1 ? supervisorOptions[0].value : '',
    description: '',
    capability_ids: [],
  }
}

function isValidEditValues(values: VirployeeEditValues): boolean {
  return (
    stringValue(values.name).length > 0 &&
    stringValue(values.job_role_id).length > 0 &&
    stringValue(values.profile_template_id).length > 0 &&
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
    stringValue(values.profile_template_id).length > 0 &&
    stringValue(values.supervisor_user_id).length > 0
  )
}

function virployeeSearchText(
  row: Virployee,
  autonomyByLevel: ReadonlyMap<VirployeeAutonomy, VirployeeAutonomyLevel>,
  jobRoleByID: ReadonlyMap<string, JobRole>,
  userByID: ReadonlyMap<string, TenantUser>,
  capabilityByID: ReadonlyMap<string, Capability>,
  profileTemplateByID: ReadonlyMap<string, ProfileTemplate>,
): string {
  const jobRole = jobRoleByID.get(row.job_role_id)
  const profile = row.profile_template_id ? profileTemplateByID.get(row.profile_template_id) : undefined
  const supervisor = userByID.get(row.supervisor_user_id)
  return [
    row.id,
    row.name,
    row.job_role_id,
    jobRole?.name,
    jobRole?.slug,
    row.profile_template_id,
    profile?.name,
    profile?.description,
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
  selectedRow: Virployee | null
  onCreate: () => void
  onEdit: (row: Virployee) => void
  onPreview: (row: Virployee) => void
  onDryRun: (row: Virployee) => void
  onClear: () => void
  onBulkAction: (action: BulkAction) => void
}) {
  const actionsDisabled = props.busy || props.selectedCount === 0
  const singleActionDisabled = props.busy || props.selectedCount !== 1 || props.selectedRow == null
  return (
    <div className="iam-control__create-inline">
      <div className="iam-control__bulk-buttons">
        <div className="iam-control__button-group">
          <button
            type="button"
            className={`btn-sm ${props.createOpen ? 'btn-primary' : 'btn-secondary'} iam-control__new-button`}
            onClick={props.onCreate}
          >
            New
          </button>
        </div>
        {props.view === 'active' ? (
          <div className="iam-control__button-group">
            <button
              type="button"
              className="btn-sm btn-secondary"
              disabled={singleActionDisabled}
              onClick={() => {
                if (props.selectedRow) props.onEdit(props.selectedRow)
              }}
            >
              Edit
            </button>
            <button
              type="button"
              className="btn-sm btn-secondary"
              disabled={singleActionDisabled}
              onClick={() => {
                if (props.selectedRow) props.onPreview(props.selectedRow)
              }}
            >
              Preview
            </button>
            <button
              type="button"
              className="btn-sm btn-secondary"
              disabled={singleActionDisabled}
              onClick={() => {
                if (props.selectedRow) props.onDryRun(props.selectedRow)
              }}
            >
              Dry Run
            </button>
            <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={props.onClear}>Clear</button>
          </div>
        ) : null}
        {props.view === 'active' ? (
          <div className="iam-control__button-group iam-control__button-group--lifecycle">
            <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={() => props.onBulkAction('archive')}>Archive</button>
            <button type="button" className="btn-sm btn-danger" disabled={actionsDisabled} onClick={() => props.onBulkAction('trash')}>Trash</button>
          </div>
        ) : null}
        {props.view === 'archived' ? (
          <>
            <div className="iam-control__button-group">
              <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={props.onClear}>Clear</button>
            </div>
            <div className="iam-control__button-group iam-control__button-group--lifecycle">
              <button type="button" className="btn-sm btn-primary" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restore</button>
            </div>
          </>
        ) : null}
        {props.view === 'trash' ? (
          <>
            <div className="iam-control__button-group">
              <button type="button" className="btn-sm btn-secondary" disabled={actionsDisabled} onClick={props.onClear}>Clear</button>
            </div>
            <div className="iam-control__button-group iam-control__button-group--lifecycle">
              <button type="button" className="btn-sm btn-primary" disabled={actionsDisabled} onClick={() => props.onBulkAction('restore')}>Restore</button>
              <button
                type="button"
                className="btn-sm btn-danger iam-control__danger-button"
                disabled={actionsDisabled}
                onClick={() => props.onBulkAction('purge')}
              >
                Delete
              </button>
            </div>
          </>
        ) : null}
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

function requiresConfirmedCalendarDraft(result: VirployeeDryRun): boolean {
  return result.intent.matched && result.draft.action === 'calendar.events.create'
}

function isCalendarCreateDraftKey(value: string): value is CalendarCreateDraftKey {
  return value === 'title' || value === 'date_hint' || value === 'time' || value === 'attendees'
}

function clarificationQuestion(value: string): string {
  if (value === 'title') return 'What is the event title?'
  if (value === 'date_hint') return 'What date should I use?'
  if (value === 'time') return 'What time should I use?'
  if (value === 'attendees') return 'Who should be invited?'
  return 'Please complete the missing field.'
}

function calendarCreateDraftValuesFromDryRun(result: VirployeeDryRun): CalendarCreateDraftValues | null {
  if (!requiresConfirmedCalendarDraft(result)) return null
  const values: CalendarCreateDraftValues = {
    title: '',
    date_hint: '',
    time: '',
    attendees: '',
  }
  for (const field of result.draft.fields) {
    if (field.key === 'title' || field.key === 'date_hint' || field.key === 'time' || field.key === 'attendees') {
      values[field.key] = field.value
    }
  }
  return values
}

function isCalendarCreateDraftComplete(values: CalendarCreateDraftValues): boolean {
  return stringValue(values.title).length > 0 &&
    stringValue(values.date_hint).length > 0 &&
    stringValue(values.time).length > 0 &&
    stringValue(values.attendees).length > 0
}

function calendarConfirmedDraftFromValues(values: CalendarCreateDraftValues): VirployeeConfirmedDraft {
  return {
    action: 'calendar.events.create',
    kind: 'calendar_event',
    fields: [
      { key: 'title', value: stringValue(values.title) },
      { key: 'date_hint', value: stringValue(values.date_hint) },
      { key: 'time', value: stringValue(values.time) },
      { key: 'attendees', value: stringValue(values.attendees) },
    ],
  }
}

function formatDraftStatus(value: VirployeeDryRun['draft']['status']): string {
  if (value === 'ready') return 'Ready'
  if (value === 'needs_input') return 'Needs input'
  if (value === 'blocked') return 'Blocked'
  if (value === 'not_applicable') return 'Not applicable'
  return value || '-'
}

function formatGateCheckKey(value: string): string {
  return value
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}

function formatConfidence(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0%'
  return `${Math.round(value * 100)}%`
}

function formatDate(value: string | null): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('en-US', { dateStyle: 'short', timeStyle: 'short', hour12: false })
}

function formatRunOperation(value: VirployeeRunTrace['operation']): string {
  if (value === 'execution_gate') return 'Execution gate'
  if (value === 'simulated_execution') return 'Simulated execution'
  return 'Dry Run'
}

function latestExecutionGateRun(runs: VirployeeRunTrace[]): VirployeeRunTrace | null {
  return runs.find((run) => run.operation === 'execution_gate') ?? null
}

function latestSimulatedExecutionRun(runs: VirployeeRunTrace[], approvalID: string): VirployeeRunTrace | null {
  if (!approvalID) return null
  return runs.find((run) => run.operation === 'simulated_execution' && run.execution_result?.approval_id === approvalID) ?? null
}

function formatRunDecision(run: VirployeeRunTrace, approval?: Approval | null): string {
  if (run.operation === 'simulated_execution') {
    return run.execution_result?.status === 'simulated_executed' ? 'Simulated' : 'Simulation'
  }
  if (run.operation === 'execution_gate') {
    if (run.nexus_result?.decision === 'require_approval') {
      const status = approval?.status || run.nexus_result.approval_status || 'pending'
      if (status === 'approved') return 'Approved'
      if (status === 'rejected') return 'Rejected'
      return 'Needs approval'
    }
    return run.gate_decision === 'pass' ? 'Pass' : 'Blocked'
  }
  return run.dry_run_decision === 'allowed' ? 'Allowed' : 'Blocked'
}

function runDecisionTone(run: VirployeeRunTrace, approval?: Approval | null): StatusTone {
  if (run.operation === 'simulated_execution') return 'success'
  if (run.operation === 'execution_gate' && run.nexus_result?.decision === 'require_approval') {
    return approvalStatusTone(approval?.status || run.nexus_result.approval_status || 'pending')
  }
  const decision = formatRunDecision(run, approval)
  return decision === 'Allowed' || decision === 'Pass' ? 'success' : 'danger'
}

function formatExecutionGateDecision(gate: VirployeeExecutionGate): string {
  return gate.execution_gate.decision === 'pass' ? 'Pass' : 'Blocked'
}

function executionGateTone(gate: VirployeeExecutionGate | null): StatusTone {
  if (!gate) return 'muted'
  return gate.execution_gate.decision === 'pass' ? 'success' : 'danger'
}

function formatNexusTrace(run: VirployeeRunTrace, approval?: Approval | null): string {
  if (run.operation === 'simulated_execution') {
    return run.execution_result?.external_effects ? 'External effects recorded' : 'No external effects'
  }
  if (!run.nexus_result) return 'Nexus not called'
  if (!run.nexus_result.available) return 'Nexus unavailable'
  if (run.nexus_result.decision === 'allow') return 'Allowed by Nexus'
  if (run.nexus_result.decision === 'deny') return 'Denied by Nexus'
  if (run.nexus_result.decision === 'require_approval') {
    const status = approval?.status || run.nexus_result.approval_status
    return status ? `Requires human approval · ${formatApprovalStatus(status)}` : 'Requires human approval'
  }
  return run.nexus_result.decision ? `Nexus ${run.nexus_result.decision}` : 'Nexus checked'
}

function nexusTraceTone(run?: VirployeeRunTrace | null, approval?: Approval | null): StatusTone {
  if (!run?.nexus_result) return 'muted'
  if (run.operation === 'simulated_execution') return run.execution_result?.external_effects ? 'warning' : 'success'
  if (!run.nexus_result.available || run.nexus_result.decision === 'deny') return 'danger'
  if (run.nexus_result.decision === 'require_approval') {
    return approvalStatusTone(approval?.status || run.nexus_result.approval_status || 'pending')
  }
  if (run.nexus_result.decision === 'allow') return 'success'
  return 'muted'
}

function formatNexusReason(run: VirployeeRunTrace): string {
  if (run.operation === 'simulated_execution') return run.execution_result?.message || ''
  if (!run.nexus_result) return ''
  return run.nexus_result.error || run.nexus_result.decision_reason || run.nexus_result.status || ''
}

function formatApprovalTrace(run: VirployeeRunTrace, approval?: Approval | null): string {
  const approvalID = run.nexus_result?.approval_id || run.execution_result?.approval_id
  if (!approvalID) return ''
  const status = approval?.status || run.nexus_result?.approval_status || run.execution_result?.approval_status || 'pending'
  return `Approval ${shortHash(approvalID)} · ${formatApprovalStatus(status)}`
}

function approvalTraceTone(run?: VirployeeRunTrace | null, approval?: Approval | null): StatusTone {
  return approvalStatusTone(approval?.status || run?.nexus_result?.approval_status || '')
}

function formatApprovalDecision(approval: Approval): string {
  const decidedAt = approval.decided_at ? ` · ${formatDate(approval.decided_at)}` : ''
  return `${formatApprovalStatus(approval.status)} by ${shortHash(approval.decided_by)}${decidedAt}`
}

function formatApprovalStatus(status: string): string {
  if (status === 'approved') return 'Approved'
  if (status === 'rejected') return 'Rejected'
  if (status === 'pending') return 'Pending'
  return status
}

function approvalStatusTone(status: string): StatusTone {
  if (status === 'approved') return 'success'
  if (status === 'rejected') return 'danger'
  if (status === 'pending') return 'warning'
  return 'muted'
}

function shortHash(value: string | undefined): string {
  if (!value) return '-'
  return value.length <= 12 ? value : value.slice(0, 12)
}
