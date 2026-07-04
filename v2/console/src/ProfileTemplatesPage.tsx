import {
  CrudPage as PlatformCrudPage,
  defaultCrudStrings,
  type CrudFormValues,
  type CrudPageProps,
} from '@devpablocristo/platform-crud-ui'
import { useEffect, useMemo, useRef, useState, type ReactElement } from 'react'
import {
  type VirployeeAutonomy,
  type ProfileTemplate,
  type ProfileTemplateInput,
  archiveProfileTemplate,
  createProfileTemplate,
  listProfileTemplates,
  purgeProfileTemplate,
  restoreProfileTemplate,
  trashProfileTemplate,
  unarchiveProfileTemplate,
  updateProfileTemplate,
} from './api'

type CrudLifecycleView = 'active' | 'archived' | 'trash'
type BulkAction = 'archive' | 'trash' | 'restore' | 'purge'

type ProfileTemplatesPageProps = {
  tenantId: string
  principalId: string
}

const MAX_AUTONOMY_OPTIONS: Array<{ label: string; value: VirployeeAutonomy }> = [
  { label: 'A0 - Conversation', value: 'A0' },
  { label: 'A1 - Recommendation', value: 'A1' },
  { label: 'A2 - Draft', value: 'A2' },
  { label: 'A3 - Limited execution', value: 'A3' },
  { label: 'A4 - Governed execution', value: 'A4' },
  { label: 'A5 - Broad autonomy', value: 'A5' },
]

const CrudPage = PlatformCrudPage as unknown as <T extends { id: string }>(
  props: CrudPageProps<T>,
) => ReactElement

export function ProfileTemplatesPage({ tenantId, principalId }: ProfileTemplatesPageProps) {
  const rootRef = useRef<HTMLElement | null>(null)
  const [lifecycleView, setLifecycleView] = useState<CrudLifecycleView>('active')
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [createRequested, setCreateRequested] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [reloadVersion, setReloadVersion] = useState(0)
  const [actionError, setActionError] = useState('')
  const isActive = Boolean(tenantId && principalId)

  const dataSource: NonNullable<CrudPageProps<ProfileTemplate>['dataSource']> = useMemo(() => ({
    list: ({ view }) => isActive ? listProfileTemplates(view, tenantId, principalId) : Promise.resolve([]),
    create: async (values) => {
      await createProfileTemplate(profilePayload(values), tenantId, principalId)
      setCreateOpen(false)
      setReloadVersion((current) => current + 1)
    },
    update: async (row, values) => {
      await updateProfileTemplate(row.id, profilePayload(values), tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
    archive: async (row) => {
      await archiveProfileTemplate(row.id, tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
    trash: async (row) => {
      await trashProfileTemplate(row.id, tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
    unarchive: async (row) => {
      await unarchiveProfileTemplate(row.id, tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
    restore: async (row) => {
      await restoreProfileTemplate(row.id, tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
    purge: async (row) => {
      await purgeProfileTemplate(row.id, tenantId, principalId)
      setReloadVersion((current) => current + 1)
    },
  }), [isActive, principalId, tenantId])

  useEffect(() => {
    setSelectedIds([])
    setCreateOpen(false)
    setActionError('')
  }, [lifecycleView, tenantId])

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
    let visibleHelpField = ''

    const syncFieldHelp = () => {
      ensureFieldHelpTrigger(root, 'max_autonomy', 'Max autonomy help')
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
      const host = document.querySelector<HTMLElement>('#profile-template-field-help-host')
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
      if (visibleHelpField === 'max_autonomy' && target instanceof HTMLSelectElement && target.id === 'crud-field-max_autonomy') {
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
          await archiveProfileTemplate(id, tenantId, principalId)
        } else if (action === 'trash') {
          await trashProfileTemplate(id, tenantId, principalId)
        } else if (action === 'restore') {
          if (lifecycleView === 'archived') {
            await unarchiveProfileTemplate(id, tenantId, principalId)
          } else {
            await restoreProfileTemplate(id, tenantId, principalId)
          }
        } else {
          await purgeProfileTemplate(id, tenantId, principalId)
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
        <div className="empty-state">Select an active tenant to manage Profile Templates.</div>
      </section>
    )
  }

  return (
    <section ref={rootRef} className="page-section iam-control axis-crud-host iam-control--external-lifecycle">
      <CrudPage<ProfileTemplate>
        key={`profile-templates-${tenantId}-${lifecycleView}-${reloadVersion}`}
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
        label="profile template"
        labelPlural="profile templates"
        labelPluralCap="Profile Templates"
        createLabel="New"
        columns={profileColumns(selectedIds, toggleSelected)}
        formFields={profileFormFields()}
        searchText={profileSearchText}
        toFormValues={profileToFormValues}
        isValid={isValidProfileForm}
        emptyState="No Profile Templates"
        archivedEmptyState="No archived Profile Templates"
        trashEmptyState="No Profile Templates in trash"
        searchPlaceholder="Search Profile Templates"
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
          </div>
        )}
        toolbarActions={lifecycleToolbarActions(lifecycleView, createOpen, setExternalLifecycleView)}
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
  const host = ensureHelpHost('profile-template-field-help-host')
  host.innerHTML = maxAutonomyBubbleMarkup(root)
  positionHelpBubble(trigger, host)
  host.style.display = 'block'
}

function maxAutonomyBubbleMarkup(root: HTMLElement): string {
  const select = root.querySelector<HTMLSelectElement>('#crud-field-max_autonomy')
  const selected = maxAutonomyDefinition(select?.value ?? '')
  return `
    <div class="axis-field-help-bubble">
      <strong>Max autonomy</strong>
      <p><span>Status</span>Required.</p>
      <p><span>Selected</span>${selected.label}</p>
      <p><span>Purpose</span>Caps the highest autonomy a Virployee may have when this template is applied.</p>
      <p><span>Meaning</span>${selected.description}</p>
      <p><span>Effect</span>${selected.effect}</p>
    </div>
  `
}

function maxAutonomyDefinition(value: string): {
  label: string
  description: string
  effect: string
} {
  if (value === 'A0') {
    return {
      label: 'A0 - Conversation',
      description: 'Can hold conversation and read contextual information.',
      effect: 'Only Virployees with A0 can use this template.',
    }
  }
  if (value === 'A1') {
    return {
      label: 'A1 - Recommendation',
      description: 'Can read, analyze and recommend actions.',
      effect: 'Virployees above A1 cannot use this template.',
    }
  }
  if (value === 'A2') {
    return {
      label: 'A2 - Draft',
      description: 'Can prepare plans or executable drafts, without external side effects.',
      effect: 'Virployees above A2 cannot use this template.',
    }
  }
  if (value === 'A3') {
    return {
      label: 'A3 - Limited execution',
      description: 'Can execute low-risk writes that are reversible, idempotent and scoped to the tenant.',
      effect: 'Virployees above A3 cannot use this template.',
    }
  }
  if (value === 'A4') {
    return {
      label: 'A4 - Governed execution',
      description: 'Can attempt medium-risk actions only with prior approval or a controlled playbook.',
      effect: 'Virployees above A4 cannot use this template.',
    }
  }
  if (value === 'A5') {
    return {
      label: 'A5 - Broad autonomy',
      description: 'Reserved for broad multi-product autonomy; not enabled by default.',
      effect: 'Any valid Virployee autonomy can use this template.',
    }
  }
  return {
    label: 'Not selected',
    description: 'Choose the maximum autonomy this template allows.',
    effect: 'Save stays disabled until a value is selected.',
  }
}

function profileColumns(
  selectedIds: string[],
  onToggle: (id: string, checked: boolean) => void,
): CrudPageProps<ProfileTemplate>['columns'] {
  return [
    selectionColumn<ProfileTemplate>(selectedIds, onToggle),
    { key: 'name', header: 'Name' },
    { key: 'max_autonomy', header: 'Max autonomy', render: (value) => formatAutonomy(String(value ?? '')) },
    { key: 'state', header: 'State', render: (value) => formatState(String(value ?? '')) },
    { key: 'updated_at', header: 'Updated', render: (value) => formatDate(String(value ?? '')) },
  ]
}

function profileFormFields(): CrudPageProps<ProfileTemplate>['formFields'] {
  return [
    { key: 'name', label: 'Name' },
    {
      key: 'max_autonomy',
      label: 'Max autonomy',
      type: 'select' as const,
      placeholder: 'Select...',
      options: MAX_AUTONOMY_OPTIONS,
    },
    { key: 'system_prompt', label: 'System prompt', type: 'textarea' as const, rows: 5, fullWidth: true },
    { key: 'description', label: 'Description (optional)', type: 'textarea' as const, rows: 3, fullWidth: true },
  ]
}

function profileToFormValues(row: ProfileTemplate): CrudFormValues {
  return {
    name: row.name,
    max_autonomy: row.max_autonomy,
    system_prompt: row.system_prompt,
    description: row.description ?? '',
  }
}

function profilePayload(values: CrudFormValues): ProfileTemplateInput {
  return {
    name: stringValue(values.name),
    description: stringValue(values.description),
    system_prompt: stringValue(values.system_prompt),
    max_autonomy: autonomyValue(values.max_autonomy),
  }
}

function isValidProfileForm(values: CrudFormValues): boolean {
  return (
    stringValue(values.name).length > 0 &&
    stringValue(values.system_prompt).length > 0 &&
    autonomyValue(values.max_autonomy).length > 0
  )
}

function profileSearchText(row: ProfileTemplate): string {
  return [
    row.id,
    row.name,
    row.description,
    row.system_prompt,
    row.max_autonomy,
    formatAutonomy(row.max_autonomy),
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
  const raw = stringValue(value)
  return MAX_AUTONOMY_OPTIONS.some((option) => option.value === raw) ? raw as VirployeeAutonomy : ''
}

function formatAutonomy(value: string): string {
  return MAX_AUTONOMY_OPTIONS.find((option) => option.value === value)?.label ?? value
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
