import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useEffect, useMemo, useRef, useState, type ReactElement } from 'react'
import { EntityFormPanel, emptyFormValues } from './EntityFormPanel'
import { LifecycleBulkActions } from './LifecycleBulkActions'
import { crudPrimaryStickyColumn, crudSelectionStickyColumn } from './crudTableColumns'
import { formatDateTime24 } from './formatters'
import {
  type Capability,
  type CapabilityInput,
  type CapabilityRiskClass,
  type CapabilitySideEffectClass,
  type CapabilityStats,
  type VirployeeAutonomy,
  archiveCapability,
  createCapability,
  listCapabilities,
  listCapabilityStats,
  purgeCapability,
  restoreCapability,
  trashCapability,
  unarchiveCapability,
  updateCapability,
  updateCapabilityManifest,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type CapabilitiesPageProps = {
  orgId: string
  principalId: string
}

const REQUIRED_AUTONOMY_OPTIONS: Array<{ label: string; value: VirployeeAutonomy }> = [
  { label: 'A0 - Conversation', value: 'A0' },
  { label: 'A1 - Recommendation', value: 'A1' },
  { label: 'A2 - Draft', value: 'A2' },
  { label: 'A3 - Limited execution', value: 'A3' },
  { label: 'A4 - Governed execution', value: 'A4' },
]

// Governance contract options (Fase 1). Empty selection falls back to the
// fail-safe default enforced by the backend (high risk, write, approval required).
const RISK_CLASS_OPTIONS: Array<{ label: string; value: CapabilityRiskClass }> = [
  { label: 'Low', value: 'low' },
  { label: 'Medium', value: 'medium' },
  { label: 'High', value: 'high' },
  { label: 'Critical / break-glass', value: 'critical' },
]

const SIDE_EFFECT_OPTIONS: Array<{ label: string; value: CapabilitySideEffectClass }> = [
  { label: 'Read-only', value: 'read' },
  { label: 'Write', value: 'write' },
]

const APPROVAL_OPTIONS: Array<{ label: string; value: string }> = [
  { label: 'Requires governance approval', value: 'true' },
  { label: 'No approval needed', value: 'false' },
]

const EVIDENCE_OPTIONS: Array<{ label: string; value: string }> = [
  { label: 'Not required', value: 'false' },
  { label: 'Required', value: 'true' },
]

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

// Capability row extended with pre-formatted stat cells (Fase 3). Computed at
// fetch time so CrudColumn keys stay real row fields (keyof T constraint).
type CapabilityRow = Capability & {
  capability_uuid: string
  executor_binding: string
  executor_operation: string
  stats_activity: string
  stats_executions: string
  stats_success: string
}

function toCapabilityRows(capabilities: Capability[], stats: CapabilityStats[]): CapabilityRow[] {
  const byKey = new Map(stats.map((item) => [item.capability_key, item]))
  return capabilities.map((capability) => {
    const item = byKey.get(capability.capability_key)
    return {
      ...capability,
      capability_uuid: capability.id,
      executor_binding: capability.manifest?.executor_binding_id || '—',
      executor_operation: capability.manifest?.operation || '—',
      stats_activity: item ? `${item.dry_runs} dry · ${item.gates} gate` : '—',
      stats_executions: formatStatsExecutions(item),
      stats_success: formatStatsSuccess(item),
    }
  })
}

function formatStatsExecutions(item?: CapabilityStats): string {
  if (!item || (item.executions_succeeded === 0 && item.executions_failed === 0)) return '—'
  return `${item.executions_succeeded} ok · ${item.executions_failed} fail`
}

function formatStatsSuccess(item?: CapabilityStats): string {
  // success_rate -1 is the backend's "no finished executions" sentinel.
  if (!item || item.success_rate < 0) return '—'
  return `${Math.round(item.success_rate * 100)}%`
}

export function CapabilitiesPage({ orgId, principalId }: CapabilitiesPageProps) {
  const rootRef = useRef<HTMLElement | null>(null)
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [selectedRowsById, setSelectedRowsById] = useState<Record<string, Capability>>({})
  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [formValues, setFormValues] = useState<CrudFormValues>({})
  const [formSaving, setFormSaving] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const isActive = Boolean(orgId && principalId)
  const formFields = useMemo(() => capabilityFormFields(), [])
  const selectedRow = selectedIds.length === 1 ? selectedRowsById[selectedIds[0]] ?? null : null

  const dataSource: NonNullable<CrudPageProps<CapabilityRow>['dataSource']> = useMemo(() => ({
    list: () => {
      if (!isActive) return Promise.resolve([])
      // Stats degrade gracefully: a stats hiccup must not break the CRUD list.
      return Promise.all([
        listCapabilities(lifecycleView, orgId, principalId),
        listCapabilityStats(orgId, principalId).catch(() => [] as CapabilityStats[]),
      ]).then(([capabilities, stats]) => toCapabilityRows(capabilities, stats))
    },
  }), [isActive, lifecycleView, principalId, orgId])

  useEffect(() => {
    setSelectedIds([])
    setSelectedRowsById({})
    closeForm()
    setActionError('')
  }, [lifecycleView, orgId])

  useEffect(() => {
    const root = rootRef.current
    if (!root) return
    let visibleHelpField = ''

    const syncFieldHelp = () => {
      ensureFieldHelpTrigger(root, 'required_autonomy', 'Required autonomy help')
      if (visibleHelpField) {
        renderFieldHelp(root, visibleHelpField)
      }
    }

    const showFieldHelp = (fieldKey: string) => {
      visibleHelpField = fieldKey
      renderFieldHelp(root, fieldKey)
    }

    const hideFieldHelp = () => {
      visibleHelpField = ''
      const host = document.querySelector<HTMLElement>('#capability-field-help-host')
      if (host) host.style.display = 'none'
    }

    const handlePointerOver = (event: Event) => {
      const trigger = helpTriggerFromEvent(event, root)
      if (trigger?.dataset.helpField) {
        showFieldHelp(trigger.dataset.helpField)
      }
    }

    const handlePointerOut = (event: Event) => {
      const trigger = helpTriggerFromEvent(event, root)
      if (!trigger) return
      const relatedTarget = event instanceof MouseEvent ? event.relatedTarget : null
      if (!(relatedTarget instanceof Node) || !trigger.contains(relatedTarget)) {
        hideFieldHelp()
      }
    }

    const handleChange = (event: Event) => {
      const target = event.target
      if (visibleHelpField === 'required_autonomy' && target instanceof HTMLSelectElement && target.id === 'crud-field-required_autonomy') {
        renderFieldHelp(root, visibleHelpField)
      }
    }

    const observer = new MutationObserver(syncFieldHelp)
    observer.observe(root, { childList: true, subtree: true })
    root.addEventListener('change', handleChange)
    root.addEventListener('mouseover', handlePointerOver)
    root.addEventListener('mouseout', handlePointerOut)
    syncFieldHelp()

    return () => {
      observer.disconnect()
      root.removeEventListener('change', handleChange)
      root.removeEventListener('mouseover', handlePointerOver)
      root.removeEventListener('mouseout', handlePointerOut)
      hideFieldHelp()
    }
  }, [orgId, lifecycleView, reloadVersion])

  const toggleSelected = (row: Capability, checked: boolean) => {
    setSelectedRowsById((current) => {
      const next = { ...current }
      if (checked) next[row.id] = row
      else delete next[row.id]
      return next
    })
    setSelectedIds((current) => (
      checked ? Array.from(new Set([...current, row.id])) : current.filter((item) => item !== row.id)
    ))
  }

  const clearSelected = () => {
    setSelectedIds([])
    setSelectedRowsById({})
  }

  const setExternalLifecycleView = (view: CrudLifecycleView) => {
    setLifecycleView(view)
    closeForm()
    clearSelected()
    setActionError('')
  }

  const openCreate = () => {
    setFormMode('create')
    setFormValues(emptyFormValues<Capability>(formFields))
    setActionError('')
  }

  const openEdit = () => {
    if (!selectedRow) return
    setFormMode('edit')
    setFormValues(capabilityToFormValues(selectedRow))
    setActionError('')
  }

  function closeForm() {
    setFormMode(null)
    setFormValues({})
    setFormSaving(false)
  }

  const submitForm = async () => {
    if (!isActive || !formMode || !isValidCapabilityForm(formValues) || formSaving) return
    setFormSaving(true)
    setActionError('')
    try {
      let saved: Capability
      if (formMode === 'create') {
        saved = await createCapability(capabilityPayload(formValues), orgId, principalId)
      } else if (selectedRow) {
        saved = await updateCapability(selectedRow.id, capabilityPayload(formValues), orgId, principalId)
      } else {
        return
      }
      await updateExecutorBinding(saved, formValues, orgId, principalId)
      closeForm()
      clearSelected()
      setReloadVersion((current) => current + 1)
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Could not save the capability')
    } finally {
      setFormSaving(false)
    }
  }

  const applyBulkAction = async (action: BulkAction) => {
    if (!isActive || selectedIds.length === 0 || bulkBusy) return
    setBulkBusy(true)
    setActionError('')
    try {
      for (const id of selectedIds) {
        if (action === 'archive') {
          await archiveCapability(id, orgId, principalId)
        } else if (action === 'trash') {
          await trashCapability(id, orgId, principalId)
        } else if (action === 'restore') {
          if (lifecycleView === 'archived') {
            await unarchiveCapability(id, orgId, principalId)
          } else {
            await restoreCapability(id, orgId, principalId)
          }
        } else {
          await purgeCapability(id, orgId, principalId)
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
        <div className="empty-state">Select an active organization to manage Capabilities.</div>
      </section>
    )
  }

  return (
    <section ref={rootRef} className="page-section iam-control axis-crud-host capabilities-control">
      <CrudPage<CapabilityRow>
        key={`capabilities-${orgId}-${lifecycleView}-${reloadVersion}`}
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
        label="capability"
        labelPlural="capabilities"
        labelPluralCap="Capabilities"
        createLabel="New"
        columns={capabilityColumns(selectedIds, toggleSelected)}
        formFields={formFields}
        searchText={capabilitySearchText}
        toFormValues={capabilityToFormValues}
        isValid={isValidCapabilityForm}
        emptyState="No capabilities"
        archivedEmptyState="No archived capabilities"
        trashEmptyState="No capabilities in trash"
        searchPlaceholder="Search capabilities"
        listHeaderInlineSlot={() => (
          <div className="iam-control__lead-stack">
            <CreateAndBulkActions
              selectedCount={selectedIds.length}
              view={lifecycleView}
              createOpen={formMode === 'create'}
              editOpen={formMode === 'edit'}
              busy={bulkBusy || formSaving || !isActive}
              onCreate={openCreate}
              onEdit={openEdit}
              onClear={clearSelected}
              onBulkAction={(action) => void applyBulkAction(action)}
            />
            {actionError ? <p role="alert" className="iam-control__inline-error">{actionError}</p> : null}
            {formMode ? (
              <EntityFormPanel<Capability>
                title={formMode === 'create' ? 'New capability' : 'Edit capability'}
                mode={formMode}
                fields={formFields}
                values={formValues}
                saving={formSaving}
                primaryLabel={formMode === 'create' ? 'Create' : 'Save'}
                valid={isValidCapabilityForm(formValues)}
                onChange={setFormValues}
                onSubmit={() => void submitForm()}
                onCancel={closeForm}
              />
            ) : null}
          </div>
        )}
        toolbarActions={lifecycleToolbarActions(lifecycleView, formMode != null, setExternalLifecycleView)}
        featureFlags={{ csvToolbar: false }}
      />
    </section>
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

function helpTriggerFromEvent(event: Event, root: HTMLElement): HTMLElement | null {
  const target = event.target
  if (!(target instanceof Element)) return null
  const trigger = target.closest<HTMLElement>('.axis-field-help-trigger[data-help-field]')
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
  const width = Math.min(560, window.innerWidth - viewportPadding * 2)
  const left = Math.min(Math.max(rect.left - 26, viewportPadding), window.innerWidth - width - viewportPadding)
  const top = Math.max(rect.top, viewportPadding)
  host.style.left = `${left}px`
  host.style.top = `${top}px`
  host.style.width = `${width}px`
}

function renderFieldHelp(root: HTMLElement, fieldKey: string) {
  const trigger = root.querySelector<HTMLElement>(`.axis-field-help-trigger[data-help-field="${fieldKey}"]`)
  if (!trigger) return
  const host = ensureHelpHost('capability-field-help-host')
  host.innerHTML = requiredAutonomyBubbleMarkup(root)
  positionHelpBubble(trigger, host)
  host.style.display = 'block'
}

function requiredAutonomyBubbleMarkup(root: HTMLElement): string {
  const select = root.querySelector<HTMLSelectElement>('#crud-field-required_autonomy')
  const selected = requiredAutonomyDefinition(select?.value ?? '')
  return `
    <div class="axis-field-help-bubble">
      <strong>Required autonomy</strong>
      <p><span>Status</span>Required.</p>
      <p><span>Selected</span>${selected.label}</p>
      <p><span>Purpose</span>Defines the minimum autonomy a Virployee needs to receive this Capability.</p>
      <p><span>Meaning</span>${selected.description}</p>
      <p><span>Effect</span>${selected.effect}</p>
    </div>
  `
}

function requiredAutonomyDefinition(value: string): {
  label: string
  description: string
  effect: string
} {
  if (value === 'A0') {
    return {
      label: 'A0 - Conversation',
      description: 'Can hold conversation and read contextual information.',
      effect: 'Any Virployee autonomy can receive it.',
    }
  }
  if (value === 'A1') {
    return {
      label: 'A1 - Recommendation',
      description: 'Can read, analyze and recommend actions.',
      effect: 'Virployees below A1 cannot receive it.',
    }
  }
  if (value === 'A2') {
    return {
      label: 'A2 - Draft',
      description: 'Can prepare plans or executable drafts, without external side effects.',
      effect: 'Virployees below A2 cannot receive it.',
    }
  }
  if (value === 'A3') {
    return {
      label: 'A3 - Limited execution',
      description: 'Can execute low-risk writes that are reversible, idempotent and scoped to the organization.',
      effect: 'Virployees below A3 cannot receive it.',
    }
  }
  if (value === 'A4') {
    return {
      label: 'A4 - Governed execution',
      description: 'Can attempt medium-risk actions only with prior approval or a controlled playbook.',
      effect: 'Virployees below A4 cannot receive it.',
    }
  }
  if (value === 'A5') {
    return {
      label: 'A5 - Broad autonomy',
      description: 'Reserved for broad multi-product autonomy; not enabled by default.',
      effect: 'Virployees below A5 cannot receive it.',
    }
  }
  return {
    label: 'Not selected',
    description: 'Choose the minimum autonomy for this Capability.',
    effect: 'Save stays disabled until a value is selected.',
  }
}

function capabilityColumns(
  selectedIds: string[],
  onToggle: (row: Capability, checked: boolean) => void,
): CrudPageProps<CapabilityRow>['columns'] {
  return [
    selectionColumn<CapabilityRow>(selectedIds, onToggle),
    { key: 'name', header: 'Capability', className: 'iam-control__primary-col', ...crudPrimaryStickyColumn },
    { key: 'capability_uuid', header: 'Capability UUID', render: (value) => shortCapabilityID(String(value ?? '')) },
    { key: 'description', header: 'Description' },
    { key: 'executor_binding', header: 'Connector binding' },
    { key: 'executor_operation', header: 'Operation' },
    { key: 'created_at', header: 'Created', className: 'iam-control__created-col', render: (value) => formatDateTime24(String(value ?? '')) },
    { key: 'required_autonomy', header: 'Required autonomy', render: (value) => formatRequiredAutonomy(String(value ?? '')) },
    { key: 'promotion_state', header: 'Promotion', render: (value) => formatPromotionState(String(value ?? '')) },
    { key: 'stats_activity', header: 'Activity' },
    { key: 'stats_executions', header: 'Executions' },
    { key: 'stats_success', header: 'Success' },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
  ]
}

function capabilityFormFields(): CrudPageProps<Capability>['formFields'] {
  return [
    {
      key: 'name',
      label: 'Capability',
      placeholder: 'Prepare a weekly operations summary',
      fullWidth: true,
    },
    {
      key: 'executor_binding_id',
      label: 'Connector binding (optional)',
      placeholder: 'connector-main',
    },
    {
      key: 'operation',
      label: 'Connector operation (optional)',
      placeholder: 'summary.prepare',
    },
    {
      key: 'required_autonomy',
      label: 'Required autonomy',
      type: 'select' as const,
      placeholder: 'Select...',
      options: REQUIRED_AUTONOMY_OPTIONS,
    },
    {
      key: 'risk_class',
      label: 'Risk class',
      type: 'select' as const,
      placeholder: 'High (default)',
      options: RISK_CLASS_OPTIONS,
    },
    {
      key: 'side_effect_class',
      label: 'Side effect',
      type: 'select' as const,
      placeholder: 'Write (default)',
      options: SIDE_EFFECT_OPTIONS,
    },
    {
      key: 'requires_governance_approval',
      label: 'Governance approval',
      type: 'select' as const,
      placeholder: 'Requires approval (default)',
      options: APPROVAL_OPTIONS,
    },
    {
      key: 'evidence_required',
      label: 'Evidence',
      type: 'select' as const,
      placeholder: 'Not required (default)',
      options: EVIDENCE_OPTIONS,
    },
    { key: 'description', label: 'Description (optional)', type: 'textarea' as const, rows: 3, fullWidth: true },
  ]
}

function capabilityToFormValues(row: Capability): CrudFormValues {
  return {
    capability_key: row.capability_key,
    name: row.name,
    executor_binding_id: row.manifest?.executor_binding_id ?? '',
    operation: row.manifest?.operation ?? '',
    required_autonomy: row.required_autonomy,
    risk_class: row.risk_class,
    side_effect_class: row.side_effect_class,
    requires_governance_approval: (row.requires_governance_approval ?? row.requires_nexus_approval) ? 'true' : 'false',
    evidence_required: row.evidence_required ? 'true' : 'false',
    rollback_capability_key: row.rollback_capability_key ?? '',
    description: row.description ?? '',
  }
}

function capabilityPayload(values: CrudFormValues): CapabilityInput {
  const name = stringValue(values.name)
  const compatibilityAlias = capabilityKeyValue(values.capability_key)
  return {
    capability_key: compatibilityAlias || undefined,
    name,
    description: stringValue(values.description),
    required_autonomy: requiredAutonomyValue(values.required_autonomy),
    risk_class: riskClassValue(values.risk_class),
    side_effect_class: sideEffectValue(values.side_effect_class),
    requires_governance_approval: booleanSelectValue(values.requires_governance_approval),
    evidence_required: stringValue(values.evidence_required) === 'true' ? true : undefined,
    rollback_capability_key: capabilityKeyValue(values.rollback_capability_key) || undefined,
  }
}

async function updateExecutorBinding(
  capability: Capability,
  values: CrudFormValues,
  orgId: string,
  principalId: string,
): Promise<void> {
  const executorBindingID = stringValue(values.executor_binding_id)
  const operation = stringValue(values.operation)
  const currentBindingID = capability.manifest?.executor_binding_id ?? ''
  const currentOperation = capability.manifest?.operation ?? ''
  if (executorBindingID === currentBindingID && operation === currentOperation) return
  await updateCapabilityManifest(
    capability.id,
    {
      ...capability.manifest,
      executor_binding_id: executorBindingID || undefined,
      operation: operation || undefined,
    },
    orgId,
    principalId,
  )
}

function riskClassValue(value: CrudFormValues[string]): CapabilityRiskClass | undefined {
  const raw = stringValue(value).toLowerCase()
  return RISK_CLASS_OPTIONS.some((option) => option.value === raw) ? (raw as CapabilityRiskClass) : undefined
}

function sideEffectValue(value: CrudFormValues[string]): CapabilitySideEffectClass | undefined {
  const raw = stringValue(value).toLowerCase()
  return SIDE_EFFECT_OPTIONS.some((option) => option.value === raw) ? (raw as CapabilitySideEffectClass) : undefined
}

// Returns undefined when unset so the backend applies its fail-safe default
// (approval required) rather than coercing to false.
function booleanSelectValue(value: CrudFormValues[string]): boolean | undefined {
  const raw = stringValue(value)
  if (raw === 'true') return true
  if (raw === 'false') return false
  return undefined
}

function isValidCapabilityForm(values: CrudFormValues): boolean {
  return (
    stringValue(values.name).length > 0 &&
    requiredAutonomyValue(values.required_autonomy).length > 0
  )
}

function capabilitySearchText(row: Capability): string {
  return [
    row.id,
    row.capability_key,
    row.name,
    row.description,
    row.manifest?.executor_binding_id,
    row.manifest?.operation,
    row.required_autonomy,
    formatRequiredAutonomy(row.required_autonomy),
    row.state,
  ].join(' ')
}

function shortCapabilityID(value: string): string {
  if (!value) return '—'
  return value.length > 18 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value
}

function selectionColumn<T extends Capability>(
  selectedIds: string[],
  onToggle: (row: T, checked: boolean) => void,
): NonNullable<CrudPageProps<T>['columns']>[number] {
  return {
    key: 'id' as keyof T & string,
    header: '',
    sortable: false,
    className: 'iam-control__select-col',
    ...crudSelectionStickyColumn,
    render: (_value: unknown, row: T) => (
      <input
        type="checkbox"
        aria-label={`Select ${row.id}`}
        checked={selectedIds.includes(row.id)}
        onClick={(event) => event.stopPropagation()}
        onChange={(event) => onToggle(row, event.currentTarget.checked)}
      />
    ),
  }
}

function CreateAndBulkActions(props: {
  selectedCount: number
  view: CrudLifecycleView
  createOpen: boolean
  editOpen: boolean
  busy: boolean
  onCreate: () => void
  onEdit: () => void
  onClear: () => void
  onBulkAction: (action: BulkAction) => void
}) {
  return (
    <LifecycleBulkActions
      selectedCount={props.selectedCount}
      view={props.view}
      createOpen={props.createOpen}
      editOpen={props.editOpen}
      busy={props.busy}
      onCreate={props.onCreate}
      onEdit={props.onEdit}
      onClear={props.onClear}
      onBulkAction={props.onBulkAction}
    />
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

function capabilityKeyValue(value: CrudFormValues[string]): string {
  return stringValue(value).toLowerCase()
}

function requiredAutonomyValue(value: CrudFormValues[string]): VirployeeAutonomy | '' {
  const raw = stringValue(value)
  return REQUIRED_AUTONOMY_OPTIONS.some((option) => option.value === raw) ? raw as VirployeeAutonomy : ''
}

function formatRequiredAutonomy(value: string): string {
  return REQUIRED_AUTONOMY_OPTIONS.find((option) => option.value === value)?.label ?? (value || '-')
}

function formatState(value: string): string {
  if (value === 'active') return 'Active'
  if (value === 'archived') return 'Archived'
  if (value === 'trashed') return 'Trash'
  return value || '-'
}

function formatPromotionState(value: string): string {
  if (value === 'draft') return 'Draft'
  if (value === 'conformant') return 'Conformant'
  if (value === 'active') return 'Active'
  return value || '-'
}
