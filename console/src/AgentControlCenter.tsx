import {
  Archive,
  Bot,
  CheckCircle2,
  Copy,
  FileClock,
  Filter,
  Layers3,
  Pencil,
  Play,
  Plus,
  Power,
  Search,
  ShieldCheck,
  UsersRound,
} from 'lucide-react'
import type { ReactNode } from 'react'
import { useMemo, useState } from 'react'

type AgentStatus = 'active' | 'disabled' | 'archived'
type RunStatus = 'completed' | 'requires_approval' | 'failed'
type AuditAction = 'created' | 'updated' | 'disabled' | 'archived' | 'duplicated' | 'test_run'

export type AgentFamily = {
  id: string
  name: string
  role: string
  defaultProfileId: string
  maxAutonomy: string
  description: string
}

export type AgentProfileView = {
  profileId: string
  familyId: string
  versionLabel: string
  status: AgentStatus
  maxAutonomy: string
  installedAgents: number
}

export type InstalledAgentView = {
  id: string
  orgId: string
  productSurface: string
  agentId: string
  displayName: string
  familyId: string
  profileId: string
  status: AgentStatus
  maxAutonomy: string
  allowedCapabilities: string[]
  allowedTools: string[]
  allowedConnectors: string[]
  memoryScopeId: string
  lastRunAt: string
  errorCount: number
}

export type EphemeralAgentView = {
  id: string
  route: string
  productSurface: string
  autonomy: string
  tools: string[]
}

export type AgentAuditEvent = {
  id: string
  agentId: string
  orgId: string
  productSurface: string
  action: AuditAction
  actor: string
  occurredAt: string
  summary: string
}

export type AgentRunPreview = {
  id: string
  agentId: string
  orgId: string
  productSurface: string
  runType: string
  status: RunStatus
  toolCalls: number
  nexusRequestId?: string
  createdAt: string
}

type AgentFilters = {
  search: string
  orgId: string
  productSurface: string
  familyId: string
  profileId: string
  status: string
  autonomy: string
  capability: string
}

type AgentFormState = {
  orgId: string
  productSurface: string
  agentId: string
  displayName: string
  familyId: string
  profileId: string
  status: AgentStatus
  maxAutonomy: string
  allowedCapabilities: string
  allowedTools: string
  allowedConnectors: string
  memoryScopeId: string
}

const emptyFilters: AgentFilters = {
  search: '',
  orgId: '',
  productSurface: '',
  familyId: '',
  profileId: '',
  status: '',
  autonomy: '',
  capability: '',
}

const familiesSeed: AgentFamily[] = [
  {
    id: 'billing',
    name: 'Billing',
    role: 'billing',
    defaultProfileId: 'axis.ops.billing.v1',
    maxAutonomy: 'A1',
    description: 'Planes, cuotas, pagos, límites, upgrades y propuestas comerciales.',
  },
  {
    id: 'support',
    name: 'Support',
    role: 'support',
    defaultProfileId: 'axis.ops.support.v1',
    maxAutonomy: 'A1',
    description: 'Estado de cuenta, soporte operativo y escalaciones sin tocar datos sensibles.',
  },
  {
    id: 'quality',
    name: 'Quality',
    role: 'quality',
    defaultProfileId: 'axis.ops.quality.v1',
    maxAutonomy: 'A1',
    description: 'Revisión de outputs, extracción, prompts, evals y señales de calidad.',
  },
  {
    id: 'incident',
    name: 'Incident',
    role: 'incident_triage',
    defaultProfileId: 'axis.ops.incident_triage.v1',
    maxAutonomy: 'A1',
    description: 'Triage de fallas, DLQ, salud de plataforma y propuestas de remediación.',
  },
  {
    id: 'clinical',
    name: 'Clinical',
    role: 'clinical_assist',
    defaultProfileId: 'medmory.clinical.assist.v1',
    maxAutonomy: 'A2',
    description: 'Asistencia clínica acotada al producto con evidencia y límites de seguridad.',
  },
  {
    id: 'coordinator',
    name: 'Coordinator',
    role: 'coordinator',
    defaultProfileId: 'axis.ops.coordinator.v1',
    maxAutonomy: 'A2',
    description: 'Coordinación, handoffs, seguimiento y next actions entre agentes.',
  },
]

const profilesSeed: AgentProfileView[] = [
  { profileId: 'axis.ops.billing.v1', familyId: 'billing', versionLabel: 'v1', status: 'active', maxAutonomy: 'A1', installedAgents: 3 },
  { profileId: 'axis.ops.support.v1', familyId: 'support', versionLabel: 'v1', status: 'active', maxAutonomy: 'A1', installedAgents: 2 },
  { profileId: 'axis.ops.quality.v1', familyId: 'quality', versionLabel: 'v1', status: 'active', maxAutonomy: 'A1', installedAgents: 3 },
  { profileId: 'axis.ops.incident_triage.v1', familyId: 'incident', versionLabel: 'v1', status: 'active', maxAutonomy: 'A1', installedAgents: 2 },
  { profileId: 'medmory.clinical.assist.v1', familyId: 'clinical', versionLabel: 'v1', status: 'active', maxAutonomy: 'A2', installedAgents: 5 },
  { profileId: 'axis.ops.coordinator.v1', familyId: 'coordinator', versionLabel: 'v1', status: 'active', maxAutonomy: 'A2', installedAgents: 2 },
]

const agentsSeed: InstalledAgentView[] = [
  {
    id: 'local-dev-org:medmory:billing_agent',
    orgId: 'local-dev-org',
    productSurface: 'medmory',
    agentId: 'billing_agent',
    displayName: 'Billing Agent',
    familyId: 'billing',
    profileId: 'axis.ops.billing.v1',
    status: 'active',
    maxAutonomy: 'A1',
    allowedCapabilities: ['medmory.ops.billing_status.read', 'medmory.ops.billing_cases.list', 'medmory.ops.plan_requests.read', 'medmory.ops.billing_adjustment.propose'],
    allowedTools: ['medmory_ops_billing_status_read', 'medmory_ops_billing_cases_list'],
    allowedConnectors: ['medmory'],
    memoryScopeId: 'billing:shared',
    lastRunAt: '2026-06-19T14:42:00Z',
    errorCount: 0,
  },
  {
    id: 'local-dev-org:medmory:support_agent',
    orgId: 'local-dev-org',
    productSurface: 'medmory',
    agentId: 'support_agent',
    displayName: 'Support Agent',
    familyId: 'support',
    profileId: 'axis.ops.support.v1',
    status: 'active',
    maxAutonomy: 'A1',
    allowedCapabilities: ['medmory.ops.account_status.read', 'medmory.ops.quota_status.read', 'medmory.ops.support_case_escalate'],
    allowedTools: ['medmory_ops_account_status_read'],
    allowedConnectors: ['medmory'],
    memoryScopeId: 'support:shared',
    lastRunAt: '2026-06-19T13:28:00Z',
    errorCount: 1,
  },
  {
    id: 'local-dev-org:medmory:clinical_archivist',
    orgId: 'local-dev-org',
    productSurface: 'medmory',
    agentId: 'clinical_archivist',
    displayName: 'Clinical Archivist',
    familyId: 'clinical',
    profileId: 'medmory.clinical.assist.v1',
    status: 'active',
    maxAutonomy: 'A2',
    allowedCapabilities: ['medmory.document.extract', 'medmory.document.summarize', 'medmory.memory.update', 'medmory.summary.read'],
    allowedTools: ['medmory_document_summarize', 'medmory_summary_read'],
    allowedConnectors: ['medmory'],
    memoryScopeId: 'clinical:archivist',
    lastRunAt: '2026-06-18T22:10:00Z',
    errorCount: 0,
  },
  {
    id: 'local-dev-org:medmory:care_coordinator',
    orgId: 'local-dev-org',
    productSurface: 'medmory',
    agentId: 'care_coordinator',
    displayName: 'Care Coordinator',
    familyId: 'coordinator',
    profileId: 'axis.ops.coordinator.v1',
    status: 'active',
    maxAutonomy: 'A2',
    allowedCapabilities: ['medmory.timeline.read', 'medmory.followup.plan', 'medmory.share.create'],
    allowedTools: ['medmory_timeline_read', 'medmory_followup_plan'],
    allowedConnectors: ['medmory'],
    memoryScopeId: 'care:coordination',
    lastRunAt: '2026-06-17T19:50:00Z',
    errorCount: 0,
  },
  {
    id: 'local-dev-org:medmory:prompt_quality_agent',
    orgId: 'local-dev-org',
    productSurface: 'medmory',
    agentId: 'prompt_quality_agent',
    displayName: 'Prompt Quality Agent',
    familyId: 'quality',
    profileId: 'axis.ops.quality.v1',
    status: 'active',
    maxAutonomy: 'A1',
    allowedCapabilities: ['medmory.ops.prompt_status.read', 'medmory.ops.eval_status.read', 'medmory.ops.prompt_rollout.propose'],
    allowedTools: ['medmory_ops_prompt_status_read'],
    allowedConnectors: ['medmory'],
    memoryScopeId: 'quality:prompts',
    lastRunAt: '2026-06-16T11:05:00Z',
    errorCount: 2,
  },
  {
    id: 'local-dev-org:medmory:billing_ops_agent',
    orgId: 'local-dev-org',
    productSurface: 'medmory',
    agentId: 'billing_ops_agent',
    displayName: 'Billing Ops Agent',
    familyId: 'billing',
    profileId: 'deprecated',
    status: 'disabled',
    maxAutonomy: 'A1',
    allowedCapabilities: [],
    allowedTools: [],
    allowedConnectors: ['medmory'],
    memoryScopeId: '',
    lastRunAt: '2026-06-03T08:00:00Z',
    errorCount: 0,
  },
  {
    id: 'local-dev-org:ponti:billing_agent',
    orgId: 'local-dev-org',
    productSurface: 'ponti',
    agentId: 'billing_agent',
    displayName: 'Billing Agent',
    familyId: 'billing',
    profileId: 'axis.ops.billing.v1',
    status: 'active',
    maxAutonomy: 'A1',
    allowedCapabilities: ['ponti.billing.status.read', 'ponti.plan_requests.read', 'ponti.billing_adjustment.propose'],
    allowedTools: ['ponti_billing_status_read'],
    allowedConnectors: ['ponti'],
    memoryScopeId: 'billing:ponti',
    lastRunAt: '2026-06-15T12:20:00Z',
    errorCount: 0,
  },
  {
    id: 'axis-stg-org:reference:incident_triage_agent',
    orgId: 'axis-stg-org',
    productSurface: 'reference',
    agentId: 'incident_triage_agent',
    displayName: 'Incident Triage Agent',
    familyId: 'incident',
    profileId: 'axis.ops.incident_triage.v1',
    status: 'active',
    maxAutonomy: 'A1',
    allowedCapabilities: ['reference.summary.read', 'reference.action.prepare'],
    allowedTools: ['reference_summary_read'],
    allowedConnectors: ['reference'],
    memoryScopeId: 'incident:reference',
    lastRunAt: '2026-06-14T16:10:00Z',
    errorCount: 1,
  },
  {
    id: 'shadow-org-a:shadow:quality_agent',
    orgId: 'shadow-org-a',
    productSurface: 'shadow',
    agentId: 'quality_agent',
    displayName: 'Quality Agent',
    familyId: 'quality',
    profileId: 'axis.ops.quality.v1',
    status: 'archived',
    maxAutonomy: 'A1',
    allowedCapabilities: ['shadow.summary.read'],
    allowedTools: ['shadow_summary_read'],
    allowedConnectors: ['shadow'],
    memoryScopeId: 'quality:shadow',
    lastRunAt: '2026-05-30T09:00:00Z',
    errorCount: 0,
  },
]

const ephemeralSeed: EphemeralAgentView[] = [
  { id: 'companion.default', route: 'general.assist', productSurface: 'companion', autonomy: 'A2', tools: ['get_overview', 'remember', 'recall', 'planner'] },
  { id: 'companion.nexus', route: 'nexus.*', productSurface: 'companion', autonomy: 'A2', tools: ['check_approvals', 'list_policies'] },
  { id: 'companion.operations', route: 'operations.*', productSurface: 'companion', autonomy: 'A2', tools: ['get_overview', 'list_watchers'] },
  { id: 'companion.memory', route: 'memory', productSurface: 'companion', autonomy: 'A1', tools: ['remember', 'recall'] },
  { id: 'product.<surface>.generic', route: 'general.assist', productSurface: '<surface>', autonomy: 'A2', tools: ['remember', 'recall', '<surface>_*'] },
]

const runsSeed: AgentRunPreview[] = [
  { id: 'run-1001', agentId: 'billing_agent', orgId: 'local-dev-org', productSurface: 'medmory', runType: 'billing.plan_requests.scan', status: 'completed', toolCalls: 3, createdAt: '2026-06-19T14:42:00Z' },
  { id: 'run-1002', agentId: 'billing_agent', orgId: 'local-dev-org', productSurface: 'medmory', runType: 'billing.adjustment.propose', status: 'requires_approval', toolCalls: 4, nexusRequestId: 'nx-1902', createdAt: '2026-06-19T14:20:00Z' },
  { id: 'run-1003', agentId: 'support_agent', orgId: 'local-dev-org', productSurface: 'medmory', runType: 'support.account.review', status: 'completed', toolCalls: 2, createdAt: '2026-06-19T13:28:00Z' },
  { id: 'run-1004', agentId: 'prompt_quality_agent', orgId: 'local-dev-org', productSurface: 'medmory', runType: 'quality.prompt.status', status: 'failed', toolCalls: 1, createdAt: '2026-06-16T11:05:00Z' },
]

const auditSeed: AgentAuditEvent[] = [
  { id: 'audit-1', agentId: 'billing_agent', orgId: 'local-dev-org', productSurface: 'medmory', action: 'updated', actor: 'local-dev-admin', occurredAt: '2026-06-19T12:15:00Z', summary: 'Reduced autonomy to A1 and refreshed billing capabilities.' },
  { id: 'audit-2', agentId: 'billing_ops_agent', orgId: 'local-dev-org', productSurface: 'medmory', action: 'disabled', actor: 'local-dev-admin', occurredAt: '2026-06-12T09:00:00Z', summary: 'Deprecated agent disabled after billing_agent rollout.' },
  { id: 'audit-3', agentId: 'incident_triage_agent', orgId: 'axis-stg-org', productSurface: 'reference', action: 'created', actor: 'platform-admin', occurredAt: '2026-06-08T16:30:00Z', summary: 'Reference incident triage fixture installed.' },
]

export function AgentControlCenter() {
  const [activeTab, setActiveTab] = useState<'inventory' | 'families' | 'ephemeral' | 'runs' | 'audit'>('inventory')
  const [agents, setAgents] = useState<InstalledAgentView[]>(agentsSeed)
  const [runs, setRuns] = useState<AgentRunPreview[]>(runsSeed)
  const [audit, setAudit] = useState<AgentAuditEvent[]>(auditSeed)
  const [filters, setFilters] = useState<AgentFilters>(emptyFilters)
  const [selectedAgentId, setSelectedAgentId] = useState(agentsSeed[0]?.id ?? '')
  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null)
  const [form, setForm] = useState<AgentFormState>(() => agentToForm(agentsSeed[0]))

  const orgs = useMemo(() => sortedUnique(agents.map((agent) => agent.orgId)), [agents])
  const products = useMemo(() => sortedUnique(agents.map((agent) => agent.productSurface)), [agents])
  const profiles = useMemo(() => sortedUnique([...profilesSeed.map((profile) => profile.profileId), ...agents.map((agent) => agent.profileId)]), [agents])
  const capabilities = useMemo(() => sortedUnique(agents.flatMap((agent) => agent.allowedCapabilities)), [agents])

  const filteredAgents = useMemo(() => {
    return agents.filter((agent) => matchesFilters(agent, filters))
  }, [agents, filters])

  const selectedAgent = agents.find((agent) => agent.id === selectedAgentId) ?? filteredAgents[0] ?? agents[0]
  const selectedRuns = selectedAgent ? runs.filter((run) => sameAgent(run, selectedAgent)) : []
  const selectedAudit = selectedAgent ? audit.filter((event) => sameAgent(event, selectedAgent)) : []
  const metrics = useMemo(() => ({
    total: agents.length,
    active: agents.filter((agent) => agent.status === 'active').length,
    disabled: agents.filter((agent) => agent.status === 'disabled').length,
    archived: agents.filter((agent) => agent.status === 'archived').length,
    errors: agents.reduce((count, agent) => count + agent.errorCount, 0),
    runs: runs.length,
  }), [agents, runs])

  const updateFilter = (key: keyof AgentFilters, value: string) => {
    setFilters((current) => ({ ...current, [key]: value }))
  }

  const startCreate = () => {
    const family = familiesSeed[0]
    setFormMode('create')
    setForm({
      orgId: filters.orgId || 'local-dev-org',
      productSurface: filters.productSurface || 'medmory',
      agentId: '',
      displayName: '',
      familyId: family.id,
      profileId: family.defaultProfileId,
      status: 'active',
      maxAutonomy: family.maxAutonomy,
      allowedCapabilities: '',
      allowedTools: '',
      allowedConnectors: '',
      memoryScopeId: '',
    })
  }

  const startEdit = (agent: InstalledAgentView) => {
    setSelectedAgentId(agent.id)
    setFormMode('edit')
    setForm(agentToForm(agent))
  }

  const saveForm = () => {
    const normalized = formToAgent(form, formMode === 'edit' ? selectedAgent?.id : undefined)
    if (!normalized) {
      return
    }
    setAgents((current) => {
      if (formMode === 'edit') {
        return current.map((agent) => agent.id === normalized.id ? normalized : agent)
      }
      return [normalized, ...current]
    })
    setSelectedAgentId(normalized.id)
    appendAudit(normalized, formMode === 'edit' ? 'updated' : 'created', formMode === 'edit' ? 'Agent configuration updated locally.' : 'Agent created locally.')
    setFormMode(null)
  }

  const changeStatus = (agent: InstalledAgentView, status: Extract<AgentStatus, 'disabled' | 'archived'>) => {
    const updated = { ...agent, status }
    setAgents((current) => current.map((item) => item.id === agent.id ? updated : item))
    appendAudit(updated, status === 'disabled' ? 'disabled' : 'archived', status === 'disabled' ? 'Agent disabled locally.' : 'Agent archived locally.')
    setSelectedAgentId(agent.id)
  }

  const duplicateAgent = (agent: InstalledAgentView) => {
    const copyId = nextCopyId(agents, agent)
    const copy: InstalledAgentView = {
      ...agent,
      id: `${agent.orgId}:${agent.productSurface}:${copyId}`,
      agentId: copyId,
      displayName: `${agent.displayName} Copy`,
      status: 'active',
      lastRunAt: '',
      errorCount: 0,
    }
    setAgents((current) => [copy, ...current])
    setSelectedAgentId(copy.id)
    appendAudit(copy, 'duplicated', `Duplicated from ${agent.agentId}.`)
  }

  const runAgent = (agent: InstalledAgentView) => {
    const needsApproval = agent.allowedCapabilities.some((capability) => capability.includes('propose') || capability.includes('share') || capability.includes('adjustment'))
    const createdAt = new Date().toISOString()
    const run: AgentRunPreview = {
      id: `run-${Math.random().toString(16).slice(2, 8)}`,
      agentId: agent.agentId,
      orgId: agent.orgId,
      productSurface: agent.productSurface,
      runType: `${agent.familyId}.test_run`,
      status: needsApproval ? 'requires_approval' : 'completed',
      toolCalls: Math.max(1, Math.min(5, agent.allowedCapabilities.length)),
      nexusRequestId: needsApproval ? `nx-${Math.random().toString(16).slice(2, 7)}` : undefined,
      createdAt,
    }
    setRuns((current) => [run, ...current])
    setAgents((current) => current.map((item) => item.id === agent.id ? { ...item, lastRunAt: createdAt } : item))
    appendAudit(agent, 'test_run', needsApproval ? 'Local test run produced a mock approval requirement.' : 'Local test run completed.')
    setActiveTab('runs')
  }

  const showAudit = (agent: InstalledAgentView) => {
    setSelectedAgentId(agent.id)
    setActiveTab('audit')
  }

  const appendAudit = (agent: InstalledAgentView, action: AuditAction, summary: string) => {
    setAudit((current) => [{
      id: `audit-${Math.random().toString(16).slice(2, 8)}`,
      agentId: agent.agentId,
      orgId: agent.orgId,
      productSurface: agent.productSurface,
      action,
      actor: 'local-ui',
      occurredAt: new Date().toISOString(),
      summary,
    }, ...current])
  }

  return (
    <section className="agent-control page-section">
      <div className="agent-control__topline">
        <div className="agent-control__title">
          <Bot aria-hidden="true" />
          <div>
            <strong>Agent Control Center</strong>
            <span>Familia/Profile = clase · Installed agent = objeto</span>
          </div>
        </div>
        <div className="agent-control__actions">
          <button type="button" onClick={startCreate}><Plus aria-hidden="true" />Crear agente</button>
          <button type="button" disabled={!selectedAgent} onClick={() => selectedAgent && runAgent(selectedAgent)}><Play aria-hidden="true" />Probar run</button>
        </div>
      </div>

      <nav className="agent-control__tabs" aria-label="Agent control screens">
        <TabButton active={activeTab === 'inventory'} label="Inventario" onClick={() => setActiveTab('inventory')} />
        <TabButton active={activeTab === 'families'} label="Familias / Perfiles" onClick={() => setActiveTab('families')} />
        <TabButton active={activeTab === 'ephemeral'} label="Efímeros" onClick={() => setActiveTab('ephemeral')} />
        <TabButton active={activeTab === 'runs'} label="Runs" onClick={() => setActiveTab('runs')} />
        <TabButton active={activeTab === 'audit'} label="Audit" onClick={() => setActiveTab('audit')} />
      </nav>

      <div className="agent-control__metrics">
        <MetricBox icon={<UsersRound />} label="total" value={metrics.total} />
        <MetricBox icon={<CheckCircle2 />} label="activos" value={metrics.active} />
        <MetricBox icon={<Power />} label="disabled" value={metrics.disabled} />
        <MetricBox icon={<Archive />} label="archivados" value={metrics.archived} />
        <MetricBox icon={<ShieldCheck />} label="errores" value={metrics.errors} />
        <MetricBox icon={<FileClock />} label="runs" value={metrics.runs} />
      </div>

      {formMode && (
        <AgentForm
          mode={formMode}
          form={form}
          families={familiesSeed}
          profiles={profilesSeed}
          onCancel={() => setFormMode(null)}
          onChange={setForm}
          onSave={saveForm}
        />
      )}

      {activeTab === 'inventory' && (
        <div className="agent-control__grid">
          <section className="agent-control__main">
            <FilterPanel
              filters={filters}
              orgs={orgs}
              products={products}
              families={familiesSeed}
              profiles={profiles}
              capabilities={capabilities}
              onChange={updateFilter}
              onClear={() => setFilters(emptyFilters)}
            />
            <AgentInventoryTable
              agents={filteredAgents}
              selectedId={selectedAgent?.id ?? ''}
              onSelect={setSelectedAgentId}
              onEdit={startEdit}
              onDisable={(agent) => changeStatus(agent, 'disabled')}
              onArchive={(agent) => changeStatus(agent, 'archived')}
              onDuplicate={duplicateAgent}
              onRun={runAgent}
              onAudit={showAudit}
            />
          </section>
          <AgentDetail agent={selectedAgent} runs={selectedRuns} audit={selectedAudit} />
        </div>
      )}

      {activeTab === 'families' && <FamiliesProfiles families={familiesSeed} profiles={profilesSeed} agents={agents} />}
      {activeTab === 'ephemeral' && <EphemeralAgents agents={ephemeralSeed} />}
      {activeTab === 'runs' && <RunsTable runs={runs} selectedAgent={selectedAgent} />}
      {activeTab === 'audit' && <AuditTable events={selectedAgent ? audit.filter((event) => sameAgent(event, selectedAgent)) : audit} selectedAgent={selectedAgent} />}
    </section>
  )
}

function TabButton(props: { active: boolean; label: string; onClick: () => void }) {
  return <button type="button" className={props.active ? 'active' : ''} onClick={props.onClick}>{props.label}</button>
}

function MetricBox(props: { icon: ReactNode; label: string; value: number }) {
  return (
    <article className="agent-control__metric">
      {props.icon}
      <span>{props.label}</span>
      <strong>{props.value}</strong>
    </article>
  )
}

function FilterPanel(props: {
  filters: AgentFilters
  orgs: string[]
  products: string[]
  families: AgentFamily[]
  profiles: string[]
  capabilities: string[]
  onChange: (key: keyof AgentFilters, value: string) => void
  onClear: () => void
}) {
  return (
    <section className="agent-control__filters" aria-label="Agent filters">
      <label className="agent-control__search">
        <Search aria-hidden="true" />
        <input value={props.filters.search} onChange={(event) => props.onChange('search', event.target.value)} placeholder="buscar agente, org, producto" />
      </label>
      <SelectFilter label="org" value={props.filters.orgId} values={props.orgs} onChange={(value) => props.onChange('orgId', value)} />
      <SelectFilter label="producto" value={props.filters.productSurface} values={props.products} onChange={(value) => props.onChange('productSurface', value)} />
      <SelectFilter label="familia" value={props.filters.familyId} values={props.families.map((family) => family.id)} onChange={(value) => props.onChange('familyId', value)} />
      <SelectFilter label="perfil" value={props.filters.profileId} values={props.profiles} onChange={(value) => props.onChange('profileId', value)} />
      <SelectFilter label="estado" value={props.filters.status} values={['active', 'disabled', 'archived']} onChange={(value) => props.onChange('status', value)} />
      <SelectFilter label="autonomía" value={props.filters.autonomy} values={['A0', 'A1', 'A2', 'A3', 'A4', 'A5']} onChange={(value) => props.onChange('autonomy', value)} />
      <SelectFilter label="capability" value={props.filters.capability} values={props.capabilities} onChange={(value) => props.onChange('capability', value)} />
      <button type="button" onClick={props.onClear}><Filter aria-hidden="true" />Limpiar</button>
    </section>
  )
}

function SelectFilter(props: { label: string; value: string; values: string[]; onChange: (value: string) => void }) {
  return (
    <label>
      <span>{props.label}</span>
      <select value={props.value} onChange={(event) => props.onChange(event.target.value)}>
        <option value="">Todos</option>
        {props.values.map((value) => <option key={value} value={value}>{value}</option>)}
      </select>
    </label>
  )
}

function AgentInventoryTable(props: {
  agents: InstalledAgentView[]
  selectedId: string
  onSelect: (id: string) => void
  onEdit: (agent: InstalledAgentView) => void
  onDisable: (agent: InstalledAgentView) => void
  onArchive: (agent: InstalledAgentView) => void
  onDuplicate: (agent: InstalledAgentView) => void
  onRun: (agent: InstalledAgentView) => void
  onAudit: (agent: InstalledAgentView) => void
}) {
  if (props.agents.length === 0) {
    return <div className="agent-control__empty">Sin agentes para los filtros actuales</div>
  }
  return (
    <div className="agent-control__table-wrap">
      <table className="agent-control__table">
        <thead>
          <tr>
            <th>agente</th>
            <th>org</th>
            <th>producto</th>
            <th>familia</th>
            <th>perfil</th>
            <th>estado</th>
            <th>autonomía</th>
            <th>capabilities</th>
            <th>acciones</th>
          </tr>
        </thead>
        <tbody>
          {props.agents.map((agent) => (
            <tr key={agent.id} className={props.selectedId === agent.id ? 'selected' : ''}>
              <td>
                <button type="button" className="agent-control__link" onClick={() => props.onSelect(agent.id)}>
                  {agent.displayName}
                  <small>{agent.agentId}</small>
                </button>
              </td>
              <td>{agent.orgId}</td>
              <td>{agent.productSurface}</td>
              <td>{agent.familyId}</td>
              <td>{agent.profileId}</td>
              <td><StatusBadge status={agent.status} /></td>
              <td>{agent.maxAutonomy}</td>
              <td>{agent.allowedCapabilities.length}</td>
              <td>
                <div className="agent-control__row-actions">
                  <button type="button" onClick={() => props.onEdit(agent)} aria-label={`Editar ${agent.agentId}`}><Pencil aria-hidden="true" /></button>
                  <button type="button" disabled={agent.status !== 'active'} onClick={() => props.onDisable(agent)} aria-label={`Deshabilitar ${agent.agentId}`}><Power aria-hidden="true" /></button>
                  <button type="button" disabled={agent.status === 'archived'} onClick={() => props.onArchive(agent)} aria-label={`Archivar ${agent.agentId}`}><Archive aria-hidden="true" /></button>
                  <button type="button" onClick={() => props.onDuplicate(agent)} aria-label={`Duplicar ${agent.agentId}`}><Copy aria-hidden="true" /></button>
                  <button type="button" onClick={() => props.onRun(agent)} aria-label={`Probar ${agent.agentId}`}><Play aria-hidden="true" /></button>
                  <button type="button" onClick={() => props.onAudit(agent)} aria-label={`Audit ${agent.agentId}`}><FileClock aria-hidden="true" /></button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function AgentDetail(props: { agent?: InstalledAgentView; runs: AgentRunPreview[]; audit: AgentAuditEvent[] }) {
  if (!props.agent) {
    return <aside className="agent-control__detail"><div className="agent-control__empty">Seleccioná un agente</div></aside>
  }
  return (
    <aside className="agent-control__detail">
      <header>
        <span>Installed agent = objeto</span>
        <h2>{props.agent.displayName}</h2>
        <p>{props.agent.orgId} / {props.agent.productSurface} / {props.agent.agentId}</p>
      </header>
      <dl className="agent-control__facts">
        <div><dt>familia</dt><dd>{props.agent.familyId}</dd></div>
        <div><dt>profile</dt><dd>{props.agent.profileId}</dd></div>
        <div><dt>estado</dt><dd><StatusBadge status={props.agent.status} /></dd></div>
        <div><dt>autonomía</dt><dd>{props.agent.maxAutonomy}</dd></div>
        <div><dt>memoria</dt><dd>{props.agent.memoryScopeId || '-'}</dd></div>
        <div><dt>último run</dt><dd>{formatDate(props.agent.lastRunAt)}</dd></div>
      </dl>
      <DetailList title="capabilities" values={props.agent.allowedCapabilities} />
      <DetailList title="tools" values={props.agent.allowedTools} />
      <DetailList title="connectors" values={props.agent.allowedConnectors} />
      <DetailList title="runs recientes" values={props.runs.slice(0, 4).map((run) => `${run.runType} · ${run.status}`)} />
      <DetailList title="audit reciente" values={props.audit.slice(0, 4).map((event) => `${event.action} · ${formatDate(event.occurredAt)}`)} />
    </aside>
  )
}

function DetailList(props: { title: string; values: string[] }) {
  return (
    <section className="agent-control__detail-list">
      <h3>{props.title}</h3>
      {props.values.length === 0 ? <span>-</span> : props.values.map((value) => <span key={value}>{value}</span>)}
    </section>
  )
}

function FamiliesProfiles(props: { families: AgentFamily[]; profiles: AgentProfileView[]; agents: InstalledAgentView[] }) {
  return (
    <div className="agent-control__split">
      <section className="agent-control__surface">
        <h2><Layers3 aria-hidden="true" />Familias transversales</h2>
        <div className="agent-control__family-list">
          {props.families.map((family) => (
            <article key={family.id}>
              <strong>{family.name}</strong>
              <span>{family.defaultProfileId}</span>
              <p>{family.description}</p>
              <small>{props.agents.filter((agent) => agent.familyId === family.id).length} objetos instalados · max {family.maxAutonomy}</small>
            </article>
          ))}
        </div>
      </section>
      <section className="agent-control__surface">
        <h2><Bot aria-hidden="true" />Profiles versionados</h2>
        <SimpleTable columns={['profile', 'familia', 'status', 'max', 'instalados']} rows={props.profiles.map((profile) => [
          profile.profileId,
          profile.familyId,
          profile.status,
          profile.maxAutonomy,
          profile.installedAgents,
        ])} />
      </section>
    </div>
  )
}

function EphemeralAgents(props: { agents: EphemeralAgentView[] }) {
  return (
    <section className="agent-control__surface">
      <h2><ShieldCheck aria-hidden="true" />Defaults del runtime</h2>
      <SimpleTable columns={['id', 'ruta', 'producto', 'autonomía', 'tools']} rows={props.agents.map((agent) => [
        agent.id,
        agent.route,
        agent.productSurface,
        agent.autonomy,
        agent.tools.join(', '),
      ])} />
    </section>
  )
}

function RunsTable(props: { runs: AgentRunPreview[]; selectedAgent?: InstalledAgentView }) {
  const selectedAgent = props.selectedAgent
  const rows = selectedAgent ? props.runs.filter((run) => sameAgent(run, selectedAgent)) : props.runs
  return (
    <section className="agent-control__surface">
      <h2><Play aria-hidden="true" />Runs {selectedAgent ? selectedAgent.agentId : 'globales'}</h2>
      <SimpleTable columns={['run', 'agente', 'producto', 'tipo', 'status', 'tools', 'nexus', 'created']} rows={rows.map((run) => [
        run.id,
        run.agentId,
        run.productSurface,
        run.runType,
        run.status,
        run.toolCalls,
        run.nexusRequestId ?? '-',
        formatDate(run.createdAt),
      ])} />
    </section>
  )
}

function AuditTable(props: { events: AgentAuditEvent[]; selectedAgent?: InstalledAgentView }) {
  return (
    <section className="agent-control__surface">
      <h2><FileClock aria-hidden="true" />Audit {props.selectedAgent ? props.selectedAgent.agentId : 'global'}</h2>
      <SimpleTable columns={['fecha', 'agente', 'producto', 'acción', 'actor', 'resumen']} rows={props.events.map((event) => [
        formatDate(event.occurredAt),
        event.agentId,
        event.productSurface,
        event.action,
        event.actor,
        event.summary,
      ])} />
    </section>
  )
}

function AgentForm(props: {
  mode: 'create' | 'edit'
  form: AgentFormState
  families: AgentFamily[]
  profiles: AgentProfileView[]
  onChange: (form: AgentFormState) => void
  onCancel: () => void
  onSave: () => void
}) {
  const setValue = (key: keyof AgentFormState, value: string) => {
    if (key === 'familyId') {
      const family = props.families.find((item) => item.id === value)
      props.onChange({ ...props.form, familyId: value, profileId: family?.defaultProfileId ?? props.form.profileId, maxAutonomy: family?.maxAutonomy ?? props.form.maxAutonomy })
      return
    }
    props.onChange({ ...props.form, [key]: value } as AgentFormState)
  }
  return (
    <section className="agent-control__form">
      <header>
        <h2>{props.mode === 'create' ? 'Crear agente' : 'Editar agente'}</h2>
        <div>
          <button type="button" onClick={props.onCancel}>Cancelar</button>
          <button type="button" onClick={props.onSave}>Guardar</button>
        </div>
      </header>
      <div className="agent-control__form-grid">
        <TextInput label="org" value={props.form.orgId} onChange={(value) => setValue('orgId', value)} />
        <TextInput label="producto" value={props.form.productSurface} onChange={(value) => setValue('productSurface', value)} />
        <TextInput label="agent_id" value={props.form.agentId} onChange={(value) => setValue('agentId', value)} />
        <TextInput label="nombre" value={props.form.displayName} onChange={(value) => setValue('displayName', value)} />
        <label>
          <span>familia</span>
          <select value={props.form.familyId} onChange={(event) => setValue('familyId', event.target.value)}>
            {props.families.map((family) => <option key={family.id} value={family.id}>{family.id}</option>)}
          </select>
        </label>
        <label>
          <span>perfil</span>
          <select value={props.form.profileId} onChange={(event) => setValue('profileId', event.target.value)}>
            {props.profiles.map((profile) => <option key={profile.profileId} value={profile.profileId}>{profile.profileId}</option>)}
          </select>
        </label>
        <label>
          <span>estado</span>
          <select value={props.form.status} onChange={(event) => setValue('status', event.target.value)}>
            <option value="active">active</option>
            <option value="disabled">disabled</option>
            <option value="archived">archived</option>
          </select>
        </label>
        <label>
          <span>autonomía</span>
          <select value={props.form.maxAutonomy} onChange={(event) => setValue('maxAutonomy', event.target.value)}>
            {['A0', 'A1', 'A2', 'A3', 'A4', 'A5'].map((level) => <option key={level} value={level}>{level}</option>)}
          </select>
        </label>
        <TextInput label="memory scope" value={props.form.memoryScopeId} onChange={(value) => setValue('memoryScopeId', value)} />
        <TextInput label="capabilities" value={props.form.allowedCapabilities} onChange={(value) => setValue('allowedCapabilities', value)} />
        <TextInput label="tools" value={props.form.allowedTools} onChange={(value) => setValue('allowedTools', value)} />
        <TextInput label="connectors" value={props.form.allowedConnectors} onChange={(value) => setValue('allowedConnectors', value)} />
      </div>
    </section>
  )
}

function TextInput(props: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label>
      <span>{props.label}</span>
      <input value={props.value} onChange={(event) => props.onChange(event.target.value)} />
    </label>
  )
}

function SimpleTable(props: { columns: string[]; rows: Array<Array<string | number>> }) {
  if (props.rows.length === 0) {
    return <div className="agent-control__empty">Sin datos</div>
  }
  return (
    <div className="agent-control__table-wrap">
      <table className="agent-control__table">
        <thead>
          <tr>{props.columns.map((column) => <th key={column}>{column}</th>)}</tr>
        </thead>
        <tbody>
          {props.rows.map((row, index) => (
            <tr key={index}>{row.map((cell, cellIndex) => <td key={cellIndex}>{cell}</td>)}</tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function StatusBadge(props: { status: AgentStatus }) {
  return <span className={`agent-control__status ${props.status}`}>{props.status}</span>
}

function matchesFilters(agent: InstalledAgentView, filters: AgentFilters) {
  const search = filters.search.trim().toLowerCase()
  if (search) {
    const haystack = [agent.agentId, agent.displayName, agent.orgId, agent.productSurface, agent.familyId, agent.profileId, ...agent.allowedCapabilities].join(' ').toLowerCase()
    if (!haystack.includes(search)) return false
  }
  if (filters.orgId && agent.orgId !== filters.orgId) return false
  if (filters.productSurface && agent.productSurface !== filters.productSurface) return false
  if (filters.familyId && agent.familyId !== filters.familyId) return false
  if (filters.profileId && agent.profileId !== filters.profileId) return false
  if (filters.status && agent.status !== filters.status) return false
  if (filters.autonomy && agent.maxAutonomy !== filters.autonomy) return false
  if (filters.capability && !agent.allowedCapabilities.includes(filters.capability)) return false
  return true
}

function agentToForm(agent: InstalledAgentView): AgentFormState {
  return {
    orgId: agent.orgId,
    productSurface: agent.productSurface,
    agentId: agent.agentId,
    displayName: agent.displayName,
    familyId: agent.familyId,
    profileId: agent.profileId,
    status: agent.status,
    maxAutonomy: agent.maxAutonomy,
    allowedCapabilities: agent.allowedCapabilities.join(', '),
    allowedTools: agent.allowedTools.join(', '),
    allowedConnectors: agent.allowedConnectors.join(', '),
    memoryScopeId: agent.memoryScopeId,
  }
}

function formToAgent(form: AgentFormState, existingId?: string): InstalledAgentView | null {
  const orgId = form.orgId.trim()
  const productSurface = form.productSurface.trim()
  const agentId = form.agentId.trim()
  if (!orgId || !productSurface || !agentId) return null
  return {
    id: existingId ?? `${orgId}:${productSurface}:${agentId}`,
    orgId,
    productSurface,
    agentId,
    displayName: form.displayName.trim() || agentId,
    familyId: form.familyId,
    profileId: form.profileId,
    status: form.status,
    maxAutonomy: form.maxAutonomy,
    allowedCapabilities: splitList(form.allowedCapabilities),
    allowedTools: splitList(form.allowedTools),
    allowedConnectors: splitList(form.allowedConnectors),
    memoryScopeId: form.memoryScopeId.trim(),
    lastRunAt: '',
    errorCount: 0,
  }
}

function splitList(value: string) {
  return value.split(',').map((item) => item.trim()).filter(Boolean)
}

function sortedUnique(values: string[]) {
  return Array.from(new Set(values.filter(Boolean))).sort((a, b) => a.localeCompare(b))
}

function sameAgent(value: { agentId: string; orgId: string; productSurface: string }, agent: InstalledAgentView) {
  return value.agentId === agent.agentId && value.orgId === agent.orgId && value.productSurface === agent.productSurface
}

function nextCopyId(agents: InstalledAgentView[], agent: InstalledAgentView) {
  let index = 1
  let candidate = `${agent.agentId}_copy`
  const used = new Set(agents.map((item) => `${item.orgId}:${item.productSurface}:${item.agentId}`))
  while (used.has(`${agent.orgId}:${agent.productSurface}:${candidate}`)) {
    index += 1
    candidate = `${agent.agentId}_copy_${index}`
  }
  return candidate
}

function formatDate(value: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString()
}
