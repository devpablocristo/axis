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
  type VirployeeAutonomy,
  archiveCapability,
  createCapability,
  listCapabilities,
  purgeCapability,
  restoreCapability,
  trashCapability,
  unarchiveCapability,
  updateCapability,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type CapabilitiesPageProps = {
  tenantId: string
  principalId: string
}

const REQUIRED_AUTONOMY_OPTIONS: Array<{ label: string; value: VirployeeAutonomy }> = [
  { label: 'A0 - Conversation', value: 'A0' },
  { label: 'A1 - Recommendation', value: 'A1' },
  { label: 'A2 - Draft', value: 'A2' },
  { label: 'A3 - Limited execution', value: 'A3' },
  { label: 'A4 - Governed execution', value: 'A4' },
]

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function CapabilitiesPage({ tenantId, principalId }: CapabilitiesPageProps) {
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
  const isActive = Boolean(tenantId && principalId)
  const formFields = useMemo(() => capabilityFormFields(), [])
  const selectedRow = selectedIds.length === 1 ? selectedRowsById[selectedIds[0]] ?? null : null

  const dataSource: NonNullable<CrudPageProps<Capability>['dataSource']> = useMemo(() => ({
    list: () => isActive ? listCapabilities(lifecycleView, tenantId, principalId) : Promise.resolve([]),
  }), [isActive, lifecycleView, principalId, tenantId])

  useEffect(() => {
    setSelectedIds([])
    setSelectedRowsById({})
    closeForm()
    setActionError('')
  }, [lifecycleView, tenantId])

  useEffect(() => {
    const root = rootRef.current
    if (!root) return
    let visibleHelpField = ''

    const syncFieldHelp = () => {
      ensureFieldHelpTrigger(root, 'capability_key', 'Capability key help')
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
  }, [tenantId, lifecycleView, reloadVersion])

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
      if (formMode === 'create') {
        await createCapability(capabilityPayload(formValues), tenantId, principalId)
      } else if (selectedRow) {
        await updateCapability(selectedRow.id, capabilityPayload(formValues), tenantId, principalId)
      }
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
          await archiveCapability(id, tenantId, principalId)
        } else if (action === 'trash') {
          await trashCapability(id, tenantId, principalId)
        } else if (action === 'restore') {
          if (lifecycleView === 'archived') {
            await unarchiveCapability(id, tenantId, principalId)
          } else {
            await restoreCapability(id, tenantId, principalId)
          }
        } else {
          await purgeCapability(id, tenantId, principalId)
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
        <div className="empty-state">Select an active tenant to manage Capabilities.</div>
      </section>
    )
  }

  return (
    <section ref={rootRef} className="page-section iam-control axis-crud-host capabilities-control">
      <CrudPage<Capability>
        key={`capabilities-${tenantId}-${lifecycleView}-${reloadVersion}`}
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
  host.innerHTML = fieldKey === 'required_autonomy' ? requiredAutonomyBubbleMarkup(root) : capabilityKeyBubbleMarkup()
  positionHelpBubble(trigger, host)
  host.style.display = 'block'
}

function capabilityKeyBubbleMarkup(): string {
  return `
    <div class="axis-field-help-bubble">
      <strong>Capability key</strong>
      <p><span>Status</span>Required on create. It cannot be edited later.</p>
      <p><span>Format</span><code>domain.resource.action</code></p>
      <p><span>Example</span><code>calendar.events.read</code></p>
      <p><span>Rules</span>Lowercase letters and ñ only. No spaces, numbers, underscores or hyphens.</p>
    </div>
  `
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
      description: 'Can execute low-risk writes that are reversible, idempotent and scoped to the tenant.',
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
): CrudPageProps<Capability>['columns'] {
  return [
    selectionColumn<Capability>(selectedIds, onToggle),
    { key: 'capability_key', header: 'Key', className: 'iam-control__primary-col', ...crudPrimaryStickyColumn },
    { key: 'created_at', header: 'Created', className: 'iam-control__created-col', render: (value) => formatDateTime24(String(value ?? '')) },
    { key: 'name', header: 'Name' },
    { key: 'required_autonomy', header: 'Required autonomy', render: (value) => formatRequiredAutonomy(String(value ?? '')) },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
  ]
}

function capabilityFormFields(): CrudPageProps<Capability>['formFields'] {
  return [
    {
      key: 'capability_key',
      label: 'Capability key',
      placeholder: 'calendar.events.read',
      createOnly: true,
      fullWidth: true,
    },
    { key: 'name', label: 'Name' },
    {
      key: 'required_autonomy',
      label: 'Required autonomy',
      type: 'select' as const,
      placeholder: 'Select...',
      options: REQUIRED_AUTONOMY_OPTIONS,
    },
    { key: 'description', label: 'Description (optional)', type: 'textarea' as const, rows: 3, fullWidth: true },
  ]
}

function capabilityToFormValues(row: Capability): CrudFormValues {
  return {
    capability_key: row.capability_key,
    name: row.name,
    required_autonomy: row.required_autonomy,
    description: row.description ?? '',
  }
}

function capabilityPayload(values: CrudFormValues): CapabilityInput {
  return {
    capability_key: capabilityKeyValue(values.capability_key) || undefined,
    name: stringValue(values.name),
    description: stringValue(values.description),
    required_autonomy: requiredAutonomyValue(values.required_autonomy),
  }
}

function isValidCapabilityForm(values: CrudFormValues): boolean {
  const capabilityKey = capabilityKeyValue(values.capability_key)
  return (
    validCapabilityKey(capabilityKey) &&
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
    row.required_autonomy,
    formatRequiredAutonomy(row.required_autonomy),
    row.state,
  ].join(' ')
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

function validCapabilityKey(value: string): boolean {
  return /^[a-zñ]+\.[a-zñ]+\.[a-zñ]+$/.test(value)
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
