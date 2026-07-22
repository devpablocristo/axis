import { parsePaginatedResponse, type PaginatedList as PlatformPaginatedList } from '@devpablocristo/platform-browser/crud'

export type LifecycleTimestamp = string | null
export type OrgState = 'active' | 'archived' | 'trashed'

export type OrganizationProduct = {
  id: string
  product_surface: string
  name: string
  status: string
  state: OrgState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type SessionOrganization = {
  id: string
  name: string
  products: OrganizationProduct[]
}

export type AxisUser = {
  id: string
  email: string
  status: string
}

export type Session = {
  principal_id: string
  actor_id: string
  org_id: string
  auth_method: string
  user: AxisUser
  organizations: SessionOrganization[]
}

export type AxisOrg = {
  id: string
  name: string
  provider: string
  provider_org_id: string
  status: string
  state: OrgState
  product_count: number
  has_products: boolean
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type OrgInput = {
  name: string
}

export type Product = {
  id: string
  product_surface: string
  name: string
  status: string
  state: OrgState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type ProductInput = {
	name: string
	product_surface?: string
}

export type Approval = {
	id: string
	governance_check_id: string
	requester_id: string
	action_type: string
	target_system: string
	target_resource: string
	risk_level: string
	reason: string
	binding_hash: string
	status: 'pending' | 'approved' | 'rejected' | 'expired'
	approval_kind: 'normal' | 'break_glass'
	supervisor_user_id: string
	quorum_required: number
	approval_count: number
	decisions: Array<{
		id: string
		actor_id: string
		actor_role: string
		decision: 'approve' | 'reject'
		note: string
		decided_at: string
	}>
	post_review_required: boolean
	reviewed_by: string
	review_note: string
	reviewed_at: string | null
	decided_by: string
	decision_note: string
	decided_at: string | null
	expires_at: string
	created_at: string
	updated_at: string
}

export type AssistCase = {
	id: string
	org_id: string
	product_surface: string
	assist_type: string
	subject_id: string
	entrypoint_virployee_id: string
	owner_virployee_id: string
	status: 'open' | 'needs_human' | 'closed'
	version: number
	created_at: string
	updated_at: string
}

export type WorkSubjectKind = 'person' | 'organization' | 'team' | 'patient' | 'case'

export type WorkSubject = {
	id: string
	org_id: string
	kind: WorkSubjectKind
	display_name: string
	external_ref: string
	state: 'active' | 'archived'
	created_at: string
	updated_at: string
}

export type WorkSubjectInput = Pick<WorkSubject, 'kind' | 'display_name' | 'external_ref'>

export type RoutingPool = {
	id: string
	org_id: string
	job_role_id: string
	name: string
	state: 'active' | 'archived'
	created_at: string
	updated_at: string
}

export type RoutingPoolMember = {
	pool_id: string
	virployee_id: string
	max_active_subjects: number
	enabled: boolean
	active_subjects: number
	created_at: string
	updated_at: string
}

export type ContinuityAssignment = {
	id: string
	org_id: string
	pool_id: string
	subject_id: string
	virployee_id: string
	status: 'active' | 'reassigned'
	version: number
	assigned_at: string
	updated_at: string
}

export type MCPPolicyRule = {
	disabled: boolean
	allowed_capabilities: string[]
	denied_capabilities: string[]
}

export type MCPPolicy = {
	org_id: string
	enabled: boolean
	kill_switch: boolean
	allowed_capabilities: string[]
	denied_capabilities: string[]
	capability_kill_switches: Record<string, boolean>
	max_risk_class: 'low' | 'medium' | 'high' | 'critical'
	max_calls_per_minute: number
	max_concurrency: number
	product_rules: Record<string, MCPPolicyRule>
	job_role_rules: Record<string, MCPPolicyRule>
	version: number
	changed_by: string
	created_at: string
	updated_at: string
}

export type MCPTool = {
	name: string
	description?: string
	inputSchema: Record<string, unknown>
	outputSchema?: Record<string, unknown>
	annotations: {
		readOnlyHint: boolean
		destructiveHint: boolean
		idempotentHint: boolean
		openWorldHint: boolean
	}
	_meta: {
		'axis/capabilityVersion': string
		'axis/manifestHash': string
		'axis/riskClass': string
		'axis/requiresApproval': boolean
		'axis/rollbackMode': string
	}
}

export type MCPInvocationAudit = {
	id: string
	context: {
		org_id: string
		actor_id: string
		virployee_id: string
		subject_id: string
		case_id: string
		assignment_id: string
		assignment_version: number
	}
	method: string
	capability_key: string
	capability_version: string
	policy_version: number
	status: string
	blocked_by: string
	error_code: string
	duration_ms: number
	created_at: string
}

export type MCPPolicyAudit = {
	id: string
	actor_id: string
	previous_version: number
	new_version: number
	created_at: string
}

export type RoutingResolution = {
	status: 'assigned' | 'unavailable' | 'reassignment_required'
	created: boolean
	assignment?: ContinuityAssignment
}

export type WorkRelationshipType = 'works_for' | 'serves' | 'reports_to'

export type WorkRelationship = {
	id: string
	virployee_id: string
	subject_id: string
	type: WorkRelationshipType
	is_primary: boolean
	created_at: string
	updated_at: string
}

export type WorkRelationshipInput = Pick<WorkRelationship, 'subject_id' | 'type' | 'is_primary'>

export type KnowledgeBase = {
	id: string
	name: string
	description: string
	classification: 'professional' | 'private'
	state: 'active' | 'archived'
	version: number
	created_at: string
	updated_at: string
	archived_at?: string
}

export type KnowledgeArtifactScope = {
	virployee_id: string
	product_surface: string
	subject_id: string
	repository_generation: string
	document_id: string
}

export type KnowledgeDocument = {
	id: string
	knowledge_base_id: string
	title: string
	artifact_scope: KnowledgeArtifactScope
	source_version: string
	source_sha256: string
	state: 'active' | 'archived'
	version: number
	created_at: string
	updated_at: string
	archived_at?: string
}

export type KnowledgeIngestionTarget = {
	virployee_id: string
	subject_id: string
	document_id: string
}

export type KnowledgeConnectorIngestion = {
	title?: string
	target: KnowledgeIngestionTarget
	source: {
		connector: string
		external_id: string
		name: string
		read_url: string
		sha256: string
		mime_type: string
		size_bytes: number
	}
}

export type KnowledgeBindingScope = 'professional' | 'virployee' | 'subject' | 'case'

export type KnowledgeBindingInput = {
	scope_type: KnowledgeBindingScope
	job_role_id?: string
	virployee_id?: string
	subject_id?: string
	case_id?: string
}

export type KnowledgeBinding = KnowledgeBindingInput & {
	id: string
	knowledge_base_id: string
	version: number
	created_at: string
	updated_at: string
}

export type VirployeeKnowledgeBase = {
	knowledge_base: KnowledgeBase
	bindings: KnowledgeBinding[]
}

export type VirployeeScopePolicy = {
	virployee_id: string
	allowed_topics: string[]
	prohibited_topics: string[]
	out_of_scope: 'abstain' | 'escalate'
	revision: number
	created_at: string
	updated_at: string
}

export type ProfessionalPolicyRules = {
	allowed_topics: string[]
	prohibited_topics: string[]
	out_of_scope: 'abstain' | 'escalate'
	allowed_capabilities: string[]
	prohibited_capabilities: string[]
	delegation_required: boolean
}

export type ProfessionalPolicyPack = {
	id: string
	org_id: string
	policy_key: string
	name: string
	version: number
	job_role_id?: string
	rules: ProfessionalPolicyRules
	created_at: string
	updated_at: string
}

export type ProfessionalPolicyBinding = {
	virployee_id: string
	policy_pack_ids: string[]
	revision: number
	updated_at?: string
}

export type VirployeeDelegation = {
	id: string
	virployee_id: string
	principal_type: 'person' | 'organization' | 'team' | 'case' | 'project' | 'service'
	principal_id: string
	capability_scopes: string[]
	product_scopes: string[]
	resource_scopes: Array<{ resource_type: string; resource_id: string }>
	max_risk_class: 'low' | 'medium' | 'high' | 'critical'
	purpose: string
	granted_by: string
	valid_from: string
	valid_until: string
	status: 'active' | 'revoked' | 'expired'
	revision: number
	reviewed_at?: string | null
	reviewed_by?: string
	review_note?: string
	created_at: string
	updated_at: string
}

export type FunctionalRoleDefinition = { key: 'policy_admin' | 'approver' | 'auditor' | 'delegation_admin'; description: string; permissions: string[] }
export type FunctionalRoleGrant = {
	id: string; org_id: string; user_id: string; role_key: FunctionalRoleDefinition['key']; product_surface?: string
	action_type_pattern: string; resource_type?: string; resource_id?: string; max_risk_class: 'low' | 'medium' | 'high' | 'critical'
	valid_from: string; valid_until: string; revision: number; granted_by: string; revoked_at?: string; revoked_by?: string
	revocation_reason?: string; created_at: string; updated_at: string
}
export type GovernancePolicyVersion = {
	id: string; org_id: string; policy_id: string; version: number; state: 'draft' | 'shadow' | 'active' | 'retired'
	product_surface?: string; action_type_pattern: string; target_system?: string; requester_type?: string; expression: string
	effect: 'allow' | 'deny' | 'require_approval'; risk_override?: '' | 'low' | 'medium' | 'high' | 'critical'; priority: number
	content_hash: string; created_by: string; created_at: string; retired_at?: string
}
export type GovernancePolicy = {
	id: string; org_id: string; policy_key: string; name: string; description: string; created_by: string
	created_at: string; updated_at: string; versions?: GovernancePolicyVersion[]
}
export type GovernancePolicySimulation = {
	id: string; policy_version_id: string; requested_by: string; total_evaluated: number; would_match: number
	would_allow: number; would_deny: number; would_require_approval: number; report_hash: string; created_at: string
}
export type GovernancePolicyPromotion = {
	id: string; policy_version_id: string; simulation_id: string; target_state: 'shadow' | 'active'; status: 'pending' | 'approved' | 'rejected'
	requested_by: string; decided_by?: string; decision_reason?: string; created_at: string; decided_at?: string
}
export type GovernancePolicyEvaluation = {
	id: string; policy_version_id: string; mode: 'shadow' | 'enforced'; matched: boolean; effect: string; decision?: string
	error_code?: string; input_hash: string; created_at: string
}
export type GovernancePolicyChange = {
	id: string; policy_id: string; policy_version_id?: string; actor_id: string; action: string; summary: string
	data: Record<string, unknown>; created_at: string
}

export type Handoff = {
	id: string
	org_id: string
	case_id: string
	source_run_id?: string
	from_virployee_id: string
	to_virployee_id: string
	reason_code: string
	note_hash?: string
	status: 'pending' | 'accepted' | 'rejected' | 'cancelled' | 'expired'
	requested_by: string
	decided_by?: string
	version: number
	expires_at: string
	created_at: string
	updated_at: string
}

export type HumanReview = {
	id: string
	org_id: string
	case_id: string
	root_run_id: string
	handoff_id?: string
	reason_code: string
	urgency: 'routine' | 'urgent'
	status: 'pending' | 'claimed' | 'resolved'
	reviewer_user_id?: string
	outcome?: 'handled_externally' | 'handoff_requested' | 'dismissed'
	note_hash?: string
	created_at: string
	updated_at: string
}

export type OrchestrationPolicy = {
	id: string
	org_id: string
	product_surface: string
	assist_type: string
	entrypoint_virployee_id: string
	mode: 'disabled' | 'shadow' | 'active'
	selector_capability_id: string
	synthesis_capability_id: string
	output_schema: Record<string, unknown>
	max_specialists: number
	max_depth: number
	consultation_timeout_seconds: number
	orchestration_timeout_seconds: number
	version: number
	created_at: string
	updated_at: string
}

export type SpecialistRoute = {
	id: string
	org_id: string
	product_surface: string
	assist_type: string
	entrypoint_virployee_id: string
	specialty_code: string
	target_virployee_id: string
	capability_id: string
	requirement_mode: 'advisory_only' | 'selector_allowed' | 'required'
	enabled: boolean
	version: number
	created_at: string
	updated_at: string
}

export type VirployeeState = 'active' | 'archived' | 'trashed'
export type VirployeeAutonomy = 'A0' | 'A1' | 'A2' | 'A3' | 'A4' | 'A5'
export type GroundingMode = 'general' | 'sources_only'

export type VirployeeAutonomyLevel = {
  level: VirployeeAutonomy
  name: string
  description: string
  allows_required_autonomies: VirployeeAutonomy[]
}

export type Virployee = {
  id: string
  name: string
  job_role_id: string
  profile_template_id: string
  capability_ids: string[]
  description: string
  supervisor_user_id: string
  autonomy: VirployeeAutonomy
  grounding_mode: GroundingMode
  state: VirployeeState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type VirployeeRuntimeContext = {
  virployee: {
    id: string
    name: string
    description: string
    autonomy: VirployeeAutonomy
    state: VirployeeState
    supervisor_user_id: string
    grounding_mode: GroundingMode
  }
  job_role: {
    id: string
    name: string
    mission: string
    responsibilities: Array<{
      title: string
      description: string
      expected_outcome: string
      priority: number
    }>
    success_criteria: Array<{
      title: string
      description: string
      target_value: string
      priority: number
    }>
  }
  profile_template: {
    id: string
    name: string
    system_prompt: string
    max_autonomy: VirployeeAutonomy
  }
  capabilities: Array<{
    id: string
    capability_key: string
    name: string
    required_autonomy: VirployeeAutonomy
  }>
  memory_references: MemoryReference[]
  memory_context_hash: string
}

export type MemoryScope = { type:'virployee'|'subject'|'case'; subject_id?:string; case_id?:string }
export type MemoryReference = { id:string; title:string; type:'fact'|'preference'|'procedure'|'note'; version:number; hash:string; sensitivity:'normal'|'sensitive'; score:number; scope_type:'virployee'|'subject'|'case'; subject_id?:string; case_id?:string }
export type VirployeeMemory = { id:string; virployee_id:string; scope_type:'virployee'|'subject'|'case'; subject_id?:string; case_id?:string; title:string; type:MemoryReference['type']; content?:string; preview?:string; sensitivity:MemoryReference['sensitivity']; provenance:'human'|'system'; actor_id:string; source_reference?:string; content_hash:string; version:number; state:'active'|'archived'|'trash'; created_at:string; updated_at:string }
export type MemoryInput = { title:string; type:MemoryReference['type']; content:string; sensitivity:MemoryReference['sensitivity']; scope:MemoryScope }
export type MemoryPage = { items:VirployeeMemory[]; next_cursor?:string }
export type MemoryRecall = { items:MemoryReference[]; memory_context_hash:string }

export type VirployeeDryRun = {
  input: string
  runtime_context: VirployeeRuntimeContext
  intent: VirployeeDryRunIntent
  required_capability?: {
    id?: string
    capability_key: string
    name?: string
    required_autonomy: VirployeeAutonomy
    matched: boolean
  }
  required_autonomy: VirployeeAutonomy
  virployee_autonomy: VirployeeAutonomy
  decision: 'allowed' | 'blocked'
  reason: string
  next_step: string
  draft: VirployeeDryRunDraft
}

export type VirployeeDryRunIntent = {
  matched: boolean
  capability_key: string
  domain: string
  resource: string
  action: string
  confidence: number
  matched_by: string[]
  rules: Array<{
    type: string
    target: string
    value: string
  }>
  proposed_by: string
  model_id: string
}

export type VirployeeDryRunDraft = {
  status: 'ready' | 'needs_input' | 'blocked' | 'not_applicable'
  action: string
  kind: string
  summary: string
  fields: Array<{
    key: string
    label: string
    value: string
    source: string
  }>
  missing_fields: Array<{
    key: string
    label: string
    reason: string
  }>
  notes: string[]
}

export type VirployeeConfirmedDraft = {
  action: string
  kind: string
  fields: Array<{
    key: string
    value: string
  }>
}

export type VirployeeExecutionGate = {
  input: string
  dry_run: VirployeeDryRun
  execution_gate: {
    decision: 'pass' | 'blocked'
    mode: 'simulation'
    will_execute: boolean
    required_execution_autonomy: VirployeeAutonomy
    virployee_autonomy: VirployeeAutonomy
    checks: Array<{
      key: string
      status: 'pass' | 'blocked'
      reason: string
    }>
    next_step: string
  }
}

export type VirployeeRunTrace = {
  id: string
  virployee_id: string
  operation: 'dry_run' | 'execution_gate' | 'simulated_execution' | 'execution'
  input_hash: string
  input_preview: string
  intent: {
    matched?: boolean
    capability_key?: string
    action?: string
    [key: string]: unknown
  }
  capability_id?: string
  capability_key: string
  dry_run_decision: 'allowed' | 'blocked' | ''
  gate_decision?: 'pass' | 'blocked' | ''
  gate_checks: Array<{
    key: string
    status: 'pass' | 'blocked'
    reason: string
  }>
  nexus_result?: {
    check_id?: string
    available: boolean
    decision?: string
    risk_level?: string
    status?: string
    decision_reason?: string
    would_require_approval?: boolean
    binding_hash?: string
    approval_id?: string
    approval_status?: string
    error?: string
  }
  execution_result?: {
    status?: string
    mode?: string
    approval_id?: string
    approval_status?: string
    binding_hash?: string
    message?: string
    external_effects: boolean
    resource_id?: string
    duration_ms?: number
    nexus_report_status?: 'pending' | 'reported' | 'failed' | 'dead'
  }
  binding_hash?: string
  created_at: string
}

export type VirployeeInput = {
  name: string
  job_role_id: string
  profile_template_id: string
  capability_ids?: string[]
  description: string
  supervisor_user_id: string
  autonomy: VirployeeAutonomy | ''
  grounding_mode?: GroundingMode
  employer_subject_id?: string
}

export type JobRoleState = 'active' | 'archived' | 'trashed'

export type JobRoleResponsibility = {
	title: string
	description: string
	expected_outcome: string
	priority: number
}

export type JobRoleSuccessCriterion = {
	title: string
	description: string
	target_value: string
	priority: number
}

export type JobRole = {
  id: string
  org_id: string
  name: string
  slug: string
  mission: string
	responsibilities: JobRoleResponsibility[]
	success_criteria: JobRoleSuccessCriterion[]
  state: JobRoleState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type JobRoleInput = {
  name: string
  mission: string
	responsibilities: JobRoleResponsibility[]
	success_criteria: JobRoleSuccessCriterion[]
}

export type CapabilityState = 'active' | 'archived' | 'trashed'

export type CapabilityRiskClass = 'low' | 'medium' | 'high' | 'critical'
export type CapabilitySideEffectClass = 'read' | 'write'
export type CapabilityPromotionState = 'draft' | 'conformant' | 'active'

export type CapabilityManifest = {
  version: string
  product_surface: string
  input_schema: Record<string, unknown>
  output_schema: Record<string, unknown>
  required_scopes: string[]
  idempotency: { mode: string; key_fields: string[] }
  rollback_mode: string
  timeout_ms: number
  retry: { max_attempts: number; backoff_ms: number }
  postconditions: string[]
  quota_areas: string[]
  secret_refs: string[]
  attestation_required: boolean
  cost_class: string
}

export type CapabilityConformanceReport = {
  conformant: boolean
  manifest_hash: string
  checks: Array<{ key: string; passed: boolean; reason: string }>
}

export type Capability = {
  id: string
  org_id: string
  capability_key: string
  name: string
  description: string
  required_autonomy: VirployeeAutonomy
  risk_class: CapabilityRiskClass
  side_effect_class: CapabilitySideEffectClass
  requires_nexus_approval: boolean
  evidence_required: boolean
  rollback_capability_key: string
  promotion_state: CapabilityPromotionState
  manifest: CapabilityManifest
  manifest_hash: string
  conformed_hash: string
  conformance_report: CapabilityConformanceReport
  conformed_at: LifecycleTimestamp
  activated_at: LifecycleTimestamp
  state: CapabilityState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type CapabilityManifestInput = CapabilityManifest

export type CapabilityInput = {
  capability_key?: string
  domain?: string
  resource?: string
  action?: string
  name: string
  description: string
  required_autonomy: VirployeeAutonomy | ''
  risk_class?: CapabilityRiskClass
  side_effect_class?: CapabilitySideEffectClass
  requires_nexus_approval?: boolean
  evidence_required?: boolean
  rollback_capability_key?: string
}

export type ProfileTemplateState = 'active' | 'archived' | 'trashed'

export type ProfileTemplate = {
  id: string
  org_id: string
  name: string
  description: string
  system_prompt: string
  max_autonomy: VirployeeAutonomy
  state: ProfileTemplateState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type ProfileTemplateInput = {
  name: string
  description: string
  system_prompt: string
  max_autonomy: VirployeeAutonomy | ''
}

export type UserState = 'active' | 'archived' | 'trashed' | 'pending'
export type OrgUserRole = 'owner' | 'admin' | 'member'
export type OrgUserKind = 'user' | 'invitation'

export type OrgUser = {
  id: string
  kind: OrgUserKind
  email: string
  role: OrgUserRole
  org_id: string
  state: UserState
  created_at: string
  updated_at: string
  archived_at: LifecycleTimestamp
  trashed_at: LifecycleTimestamp
  purge_after: LifecycleTimestamp
}

export type OrgUserInput = {
  email: string
  role: OrgUserRole
}

type VirployeesListResponse = {
  data: Virployee[]
}

type VirployeeRunTracesListResponse = {
  data: VirployeeRunTrace[]
}

type JobRolesListResponse = {
  data: JobRole[]
}

type CapabilitiesListResponse = {
  data: Capability[]
}

type ProfileTemplatesListResponse = {
  data: ProfileTemplate[]
}

type UsersListResponse = {
  data: OrgUser[]
}

type OrgsListResponse = {
  data: AxisOrg[]
}

type ProductsListResponse = {
	data: Product[]
}

type ApprovalsListResponse = unknown

export type PaginatedList<T> = PlatformPaginatedList<T>

type AutonomyLevelsResponse = {
	data: VirployeeAutonomyLevel[]
}

export type AxisFetchInit = {
  orgId?: string
  principalId?: string
  method?: string
  body?: unknown
  rawBody?: BodyInit
  headers?: Record<string, string>
}

type AxisAuthTokenGetter = () => string | null | undefined | Promise<string | null | undefined>

let axisAuthTokenGetter: AxisAuthTokenGetter | null = null
let axisProductSurface = ''

export function setAxisAuthTokenGetter(getter: AxisAuthTokenGetter | null) {
  axisAuthTokenGetter = getter
}

export function setAxisProductSurface(productSurface: string) {
  axisProductSurface = productSurface.trim()
}

export async function axisFetch<T>(path: string, init: AxisFetchInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body !== undefined && init.rawBody === undefined) {
    headers.set('Content-Type', 'application/json')
  }
  if (init.orgId) {
    headers.set('X-Org-ID', init.orgId)
  }
  if (axisProductSurface) headers.set('X-Product-Surface', axisProductSurface)
  if (init.principalId) {
    headers.set('X-Actor-ID', init.principalId)
  }
  const token = await resolveAxisAuthToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  const response = await fetch(path, {
    method: init.method ?? 'GET',
    headers,
    body: init.rawBody ?? (init.body === undefined ? undefined : JSON.stringify(init.body)),
  })

  if (!response.ok) {
    throw new Error(await responseErrorMessage(response))
  }
  if (response.status === 204) {
    return undefined as T
  }
  return response.json() as Promise<T>
}

export async function axisDownload(path: string, init: AxisFetchInit = {}): Promise<Blob> {
  const headers = new Headers(init.headers)
  if (init.orgId) headers.set('X-Org-ID', init.orgId)
  if (axisProductSurface) headers.set('X-Product-Surface', axisProductSurface)
  if (init.principalId) headers.set('X-Actor-ID', init.principalId)
  const token = await resolveAxisAuthToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const response = await fetch(path, { method: init.method ?? 'GET', headers })
  if (!response.ok) throw new Error(await responseErrorMessage(response))
  return response.blob()
}

export function getSession(): Promise<Session> {
  return axisFetch<Session>('/api/session')
}

export function listWorkSubjects(orgId: string, principalId: string): Promise<WorkSubject[]> {
	return axisFetch<{ data?: WorkSubject[] } | WorkSubject[]>('/api/work-subjects', { orgId, principalId })
		.then((payload) => Array.isArray(payload) ? payload : payload.data ?? [])
}

export function getMCPPolicy(orgId: string, principalId: string): Promise<MCPPolicy> {
	return axisFetch<MCPPolicy>('/api/runtime/mcp-policy', { orgId, principalId })
}

export function putMCPPolicy(policy: MCPPolicy, orgId: string, principalId: string): Promise<MCPPolicy> {
	return axisFetch<MCPPolicy>('/api/runtime/mcp-policy', {
		method: 'PUT', orgId, principalId,
		body: {
			enabled: policy.enabled, kill_switch: policy.kill_switch,
			allowed_capabilities: policy.allowed_capabilities, denied_capabilities: policy.denied_capabilities,
			capability_kill_switches: policy.capability_kill_switches,
			max_risk_class: policy.max_risk_class, max_calls_per_minute: policy.max_calls_per_minute,
			max_concurrency: policy.max_concurrency, product_rules: policy.product_rules,
			job_role_rules: policy.job_role_rules, expected_version: policy.version,
		},
	})
}

export function listMCPInvocations(orgId: string, principalId: string, virployeeId = ''): Promise<MCPInvocationAudit[]> {
	const params = new URLSearchParams({ limit: '100' })
	if (virployeeId) params.set('virployee_id', virployeeId)
	return axisFetch<MCPInvocationAudit[]>(`/api/runtime/mcp-invocations?${params.toString()}`, { orgId, principalId })
}

export function listMCPPolicyAudit(orgId: string, principalId: string): Promise<MCPPolicyAudit[]> {
	return axisFetch<MCPPolicyAudit[]>('/api/runtime/mcp-policy/audit?limit=50', { orgId, principalId })
}

export function listMCPTools(virployeeId: string, subjectId: string, caseId: string, orgId: string, principalId: string): Promise<MCPTool[]> {
	return axisFetch<{
		jsonrpc: string
		result?: { tools?: MCPTool[] }
		error?: { message?: string }
	}>('/api/mcp', {
		method: 'POST', orgId, principalId,
		headers: {
			'X-Axis-Virployee-ID': virployeeId,
			'X-Axis-Subject-ID': subjectId,
			...(caseId ? { 'X-Axis-Case-ID': caseId } : {}),
		},
		body: { jsonrpc: '2.0', id: 'console-tools-list', method: 'tools/list', params: {} },
	}).then((payload) => {
		if (payload.error) throw new Error(payload.error.message || 'Could not resolve MCP tools')
		return payload.result?.tools ?? []
	})
}

export function createWorkSubject(input: WorkSubjectInput, orgId: string, principalId: string): Promise<WorkSubject> {
	return axisFetch<WorkSubject>('/api/work-subjects', { method: 'POST', orgId, principalId, body: input })
}

export function updateWorkSubject(id: string, input: WorkSubjectInput, orgId: string, principalId: string): Promise<WorkSubject> {
	return axisFetch<WorkSubject>(`/api/work-subjects/${encodeURIComponent(id)}`, { method: 'PUT', orgId, principalId, body: input })
}

export function setWorkSubjectArchived(id: string, archived: boolean, orgId: string, principalId: string): Promise<void> {
	return axisFetch<void>(`/api/work-subjects/${encodeURIComponent(id)}/${archived ? 'archive' : 'unarchive'}`, {
		method: 'POST', orgId, principalId, body: { reason: 'console' },
	})
}

export function listKnowledgeBases(orgId: string, principalId: string, state = 'active'): Promise<KnowledgeBase[]> {
	const params = new URLSearchParams({ state })
	return axisFetch<{ data?: KnowledgeBase[] } | KnowledgeBase[]>(`/api/knowledge-bases?${params.toString()}`, { orgId, principalId })
		.then((payload) => Array.isArray(payload) ? payload : payload.data ?? [])
}

export function createKnowledgeBase(input: Pick<KnowledgeBase, 'name' | 'description' | 'classification'>, orgId: string, principalId: string): Promise<KnowledgeBase> {
	return axisFetch<KnowledgeBase>('/api/knowledge-bases', { method: 'POST', orgId, principalId, body: input })
}

export function updateKnowledgeBase(id: string, input: Pick<KnowledgeBase, 'name' | 'description' | 'version'>, orgId: string, principalId: string): Promise<KnowledgeBase> {
	return axisFetch<KnowledgeBase>(`/api/knowledge-bases/${encodeURIComponent(id)}`, {
		method: 'PUT', orgId, principalId,
		body: { name: input.name, description: input.description, expected_version: input.version },
	})
}

export function setKnowledgeBaseArchived(base: KnowledgeBase, archived: boolean, orgId: string, principalId: string): Promise<KnowledgeBase> {
	return axisFetch<KnowledgeBase>(`/api/knowledge-bases/${encodeURIComponent(base.id)}/${archived ? 'archive' : 'activate'}`, {
		method: 'POST', orgId, principalId, body: { expected_version: base.version },
	})
}

export function listKnowledgeDocuments(baseId: string, orgId: string, principalId: string): Promise<KnowledgeDocument[]> {
	return axisFetch<{ data?: KnowledgeDocument[] } | KnowledgeDocument[]>(`/api/knowledge-bases/${encodeURIComponent(baseId)}/documents?state=active`, { orgId, principalId })
		.then((payload) => Array.isArray(payload) ? payload : payload.data ?? [])
}

export function registerKnowledgeDocument(baseId: string, input: Pick<KnowledgeDocument, 'title' | 'artifact_scope'>, orgId: string, principalId: string): Promise<KnowledgeDocument> {
	return axisFetch<KnowledgeDocument>(`/api/knowledge-bases/${encodeURIComponent(baseId)}/documents`, {
		method: 'POST', orgId, principalId, body: input,
	})
}

export function ingestKnowledgeConnector(baseId: string, input: KnowledgeConnectorIngestion, orgId: string, principalId: string): Promise<KnowledgeDocument> {
	return axisFetch<KnowledgeDocument>(`/api/knowledge-bases/${encodeURIComponent(baseId)}/ingestions/connector`, {
		method: 'POST', orgId, principalId, body: input,
	})
}

export function uploadKnowledgeFile(
	baseId: string,
	input: { title?: string; target: KnowledgeIngestionTarget; file: File },
	orgId: string,
	principalId: string,
): Promise<KnowledgeDocument> {
	const form = new FormData()
	form.set('title', input.title ?? '')
	form.set('virployee_id', input.target.virployee_id)
	form.set('subject_id', input.target.subject_id)
	form.set('document_id', input.target.document_id)
	form.set('file', input.file, input.file.name)
	return axisFetch<KnowledgeDocument>(`/api/knowledge-bases/${encodeURIComponent(baseId)}/ingestions/upload`, {
		method: 'POST', orgId, principalId, rawBody: form,
	})
}

export function archiveKnowledgeDocument(baseId: string, document: KnowledgeDocument, orgId: string, principalId: string): Promise<KnowledgeDocument> {
	return axisFetch<KnowledgeDocument>(`/api/knowledge-bases/${encodeURIComponent(baseId)}/documents/${encodeURIComponent(document.id)}/archive`, {
		method: 'POST', orgId, principalId, body: { expected_version: document.version },
	})
}

export function listKnowledgeBindings(baseId: string, orgId: string, principalId: string): Promise<KnowledgeBinding[]> {
	return axisFetch<{ data?: KnowledgeBinding[] } | KnowledgeBinding[]>(`/api/knowledge-bases/${encodeURIComponent(baseId)}/bindings`, { orgId, principalId })
		.then((payload) => Array.isArray(payload) ? payload : payload.data ?? [])
}

export function replaceKnowledgeBindings(base: KnowledgeBase, bindings: KnowledgeBindingInput[], orgId: string, principalId: string): Promise<KnowledgeBinding[]> {
	return axisFetch<{ data?: KnowledgeBinding[] }>(`/api/knowledge-bases/${encodeURIComponent(base.id)}/bindings`, {
		method: 'PUT', orgId, principalId, body: { expected_version: base.version, bindings },
	}).then((payload) => payload.data ?? [])
}

export function listVirployeeKnowledgeBases(
	virployeeId: string,
	orgId: string,
	principalId: string,
	preview?: { subjectId?: string; caseId?: string },
): Promise<VirployeeKnowledgeBase[]> {
	const params = new URLSearchParams()
	if (preview) {
		params.set('context_preview', '1')
		if (preview.subjectId) params.set('subject_id', preview.subjectId)
		if (preview.caseId) params.set('case_id', preview.caseId)
	}
	const query = params.size > 0 ? `?${params.toString()}` : ''
	return axisFetch<{ data?: VirployeeKnowledgeBase[] }>(`/api/virployees/${encodeURIComponent(virployeeId)}/knowledge-bases${query}`, {
		orgId,
		principalId,
	}).then((payload) => payload.data ?? [])
}

export function setVirployeeKnowledgeBase(
	virployeeId: string,
	base: KnowledgeBase,
	enabled: boolean,
	orgId: string,
	principalId: string,
): Promise<VirployeeKnowledgeBase[]> {
	return axisFetch<{ data?: VirployeeKnowledgeBase[] }>(`/api/virployees/${encodeURIComponent(virployeeId)}/knowledge-bases`, {
		method: 'PUT',
		orgId,
		principalId,
		body: { knowledge_base_id: base.id, expected_version: base.version, enabled },
	}).then((payload) => payload.data ?? [])
}

export function listRoutingPools(orgId: string, principalId: string): Promise<RoutingPool[]> {
	return axisFetch<{ data?: RoutingPool[] } | RoutingPool[]>('/api/routing-pools', { orgId, principalId })
		.then((payload) => Array.isArray(payload) ? payload : payload.data ?? [])
}

export function createRoutingPool(input: Pick<RoutingPool, 'job_role_id' | 'name'>, orgId: string, principalId: string): Promise<RoutingPool> {
	return axisFetch<RoutingPool>('/api/routing-pools', { method: 'POST', orgId, principalId, body: input })
}

export function updateRoutingPool(id: string, input: Pick<RoutingPool, 'job_role_id' | 'name'>, orgId: string, principalId: string): Promise<RoutingPool> {
	return axisFetch<RoutingPool>(`/api/routing-pools/${encodeURIComponent(id)}`, { method: 'PUT', orgId, principalId, body: input })
}

export function listRoutingPoolMembers(poolId: string, orgId: string, principalId: string): Promise<RoutingPoolMember[]> {
	return axisFetch<{ data?: RoutingPoolMember[] } | RoutingPoolMember[]>(`/api/routing-pools/${encodeURIComponent(poolId)}/members`, { orgId, principalId })
		.then((payload) => Array.isArray(payload) ? payload : payload.data ?? [])
}

export function putRoutingPoolMember(poolId: string, virployeeId: string, input: Pick<RoutingPoolMember, 'max_active_subjects' | 'enabled'>, orgId: string, principalId: string): Promise<RoutingPoolMember> {
	return axisFetch<RoutingPoolMember>(`/api/routing-pools/${encodeURIComponent(poolId)}/members/${encodeURIComponent(virployeeId)}`, {
		method: 'PUT', orgId, principalId, body: input,
	})
}

export function resolveVirployeeRouting(poolId: string, subjectId: string, orgId: string, principalId: string): Promise<RoutingResolution> {
	return axisFetch<RoutingResolution>('/api/virployee-routing/resolve', {
		method: 'POST', orgId, principalId, body: { pool_id: poolId, subject_id: subjectId },
	})
}

export function listContinuityAssignments(poolId: string, orgId: string, principalId: string): Promise<ContinuityAssignment[]> {
	const params = new URLSearchParams({ pool_id: poolId })
	return axisFetch<{ data?: ContinuityAssignment[] } | ContinuityAssignment[]>(`/api/virployee-routing/assignments?${params.toString()}`, { orgId, principalId })
		.then((payload) => Array.isArray(payload) ? payload : payload.data ?? [])
}

export function reassignContinuityAssignment(
	assignmentId: string,
	input: { virployee_id: string; expected_version: number; reason: string },
	orgId: string,
	principalId: string,
): Promise<ContinuityAssignment> {
	return axisFetch<ContinuityAssignment>(`/api/virployee-routing/assignments/${encodeURIComponent(assignmentId)}/reassign`, {
		method: 'POST', orgId, principalId, body: input,
	})
}

export function listVirployeeAssignments(virployeeId: string, orgId: string, principalId: string): Promise<ContinuityAssignment[]> {
	return axisFetch<{ data?: ContinuityAssignment[] } | ContinuityAssignment[]>(`/api/virployees/${encodeURIComponent(virployeeId)}/assignments`, { orgId, principalId })
		.then((payload) => Array.isArray(payload) ? payload : payload.data ?? [])
}

export function getVirployeeRelationships(virployeeId: string, orgId: string, principalId: string): Promise<WorkRelationship[]> {
	return axisFetch<{ data?: WorkRelationship[] }>(`/api/virployees/${encodeURIComponent(virployeeId)}/relationships`, { orgId, principalId })
		.then((payload) => payload.data ?? [])
}

export function putVirployeeRelationships(virployeeId: string, relationships: WorkRelationshipInput[], orgId: string, principalId: string): Promise<WorkRelationship[]> {
	return axisFetch<{ data?: WorkRelationship[] }>(`/api/virployees/${encodeURIComponent(virployeeId)}/relationships`, {
		method: 'PUT', orgId, principalId, body: { relationships },
	}).then((payload) => payload.data ?? [])
}

export function getVirployeeScopePolicy(virployeeId: string, orgId: string, principalId: string): Promise<VirployeeScopePolicy> {
	return axisFetch<VirployeeScopePolicy>(`/api/virployees/${encodeURIComponent(virployeeId)}/scope-policy`, { orgId, principalId })
}

export function putVirployeeScopePolicy(virployeeId: string, input: Pick<VirployeeScopePolicy, 'allowed_topics' | 'prohibited_topics' | 'out_of_scope' | 'revision'>, orgId: string, principalId: string): Promise<VirployeeScopePolicy> {
	return axisFetch<VirployeeScopePolicy>(`/api/virployees/${encodeURIComponent(virployeeId)}/scope-policy`, {
		method: 'PUT', orgId, principalId,
		body: { allowed_topics: input.allowed_topics, prohibited_topics: input.prohibited_topics, out_of_scope: input.out_of_scope, expected_revision: input.revision },
	})
}

export function listProfessionalPolicyPacks(orgId: string, principalId: string): Promise<ProfessionalPolicyPack[]> {
	return axisFetch<{ data?: ProfessionalPolicyPack[] } | ProfessionalPolicyPack[]>('/api/professional-policy-packs', { orgId, principalId })
		.then((payload) => Array.isArray(payload) ? payload : payload.data ?? [])
}

export function createProfessionalPolicyPack(input: Omit<ProfessionalPolicyPack, 'id' | 'org_id' | 'created_at' | 'updated_at'>, orgId: string, principalId: string): Promise<ProfessionalPolicyPack> {
	return axisFetch<ProfessionalPolicyPack>('/api/professional-policy-packs', { method: 'POST', orgId, principalId, body: input })
}

export function getVirployeeProfessionalPolicyBinding(virployeeId: string, orgId: string, principalId: string): Promise<ProfessionalPolicyBinding> {
	return axisFetch<ProfessionalPolicyBinding>(`/api/virployees/${encodeURIComponent(virployeeId)}/professional-policy-packs`, { orgId, principalId })
}

export function putVirployeeProfessionalPolicyBinding(virployeeId: string, binding: ProfessionalPolicyBinding, orgId: string, principalId: string): Promise<ProfessionalPolicyBinding> {
	return axisFetch<ProfessionalPolicyBinding>(`/api/virployees/${encodeURIComponent(virployeeId)}/professional-policy-packs`, {
		method: 'PUT', orgId, principalId, body: { policy_pack_ids: binding.policy_pack_ids, expected_revision: binding.revision },
	})
}

export function listVirployeeDelegations(virployeeId: string, orgId: string, principalId: string, principalIds: string[] = []): Promise<VirployeeDelegation[]> {
	const params = new URLSearchParams()
	for (const id of principalIds) if (id) params.append('principal_id', id)
	const query = params.size > 0 ? `?${params.toString()}` : ''
	return axisFetch<{ data?: VirployeeDelegation[] } | VirployeeDelegation[]>(`/api/virployees/${encodeURIComponent(virployeeId)}/delegations${query}`, { orgId, principalId })
		.then((payload) => Array.isArray(payload) ? payload : payload.data ?? [])
}

export function createVirployeeDelegation(virployeeId: string, input: Pick<VirployeeDelegation, 'principal_type' | 'principal_id' | 'capability_scopes' | 'product_scopes' | 'resource_scopes' | 'max_risk_class' | 'purpose' | 'valid_until'> & { valid_from?: string }, orgId: string, principalId: string): Promise<VirployeeDelegation> {
	return axisFetch<VirployeeDelegation>(`/api/virployees/${encodeURIComponent(virployeeId)}/delegations`, {
		method: 'POST', orgId, principalId, body: input,
	})
}

export function reviewVirployeeDelegation(virployeeId: string, delegationId: string, expectedRevision: number, note: string, orgId: string, principalId: string): Promise<VirployeeDelegation> {
	return axisFetch<VirployeeDelegation>(`/api/virployees/${encodeURIComponent(virployeeId)}/delegations/${encodeURIComponent(delegationId)}/review`, {
		method: 'POST', orgId, principalId, body: { expected_revision: expectedRevision, note },
	})
}

export function listFunctionalRoleDefinitions(orgId: string, principalId: string): Promise<FunctionalRoleDefinition[]> {
	return axisFetch<FunctionalRoleDefinition[]>('/api/role-definitions', { orgId, principalId })
}

export function listFunctionalRoleGrants(orgId: string, principalId: string): Promise<FunctionalRoleGrant[]> {
	return axisFetch<FunctionalRoleGrant[]>('/api/role-grants', { orgId, principalId })
}

export function createFunctionalRoleGrant(input: Omit<FunctionalRoleGrant, 'id' | 'org_id' | 'revision' | 'granted_by' | 'created_at' | 'updated_at' | 'revoked_at' | 'revoked_by' | 'revocation_reason'>, orgId: string, principalId: string): Promise<FunctionalRoleGrant> {
	return axisFetch<FunctionalRoleGrant>('/api/role-grants', { method: 'POST', orgId, principalId, body: input })
}

export function revokeFunctionalRoleGrant(id: string, revision: number, orgId: string, principalId: string): Promise<FunctionalRoleGrant> {
	return axisFetch<FunctionalRoleGrant>(`/api/role-grants/${encodeURIComponent(id)}/revoke`, { method: 'POST', orgId, principalId, body: { expected_revision: revision, reason: 'console' } })
}

export function listGovernancePolicies(orgId: string, principalId: string): Promise<GovernancePolicy[]> {
	return axisFetch<GovernancePolicy[]>('/api/governance-policies', { orgId, principalId })
}

export function getGovernancePolicy(id: string, orgId: string, principalId: string): Promise<GovernancePolicy> {
	return axisFetch<GovernancePolicy>(`/api/governance-policies/${encodeURIComponent(id)}`, { orgId, principalId })
}

export function createGovernancePolicy(input: Pick<GovernancePolicy, 'policy_key' | 'name' | 'description'>, orgId: string, principalId: string): Promise<GovernancePolicy> {
	return axisFetch<GovernancePolicy>('/api/governance-policies', { method: 'POST', orgId, principalId, body: input })
}

export function createGovernancePolicyVersion(policyId: string, input: Pick<GovernancePolicyVersion, 'product_surface' | 'action_type_pattern' | 'target_system' | 'requester_type' | 'expression' | 'effect' | 'risk_override' | 'priority'>, orgId: string, principalId: string): Promise<GovernancePolicyVersion> {
	return axisFetch<GovernancePolicyVersion>(`/api/governance-policies/${encodeURIComponent(policyId)}/versions`, { method: 'POST', orgId, principalId, body: input })
}

export function simulateGovernancePolicyVersion(id: string, orgId: string, principalId: string): Promise<GovernancePolicySimulation> {
	return axisFetch<GovernancePolicySimulation>(`/api/governance-policy-versions/${encodeURIComponent(id)}/simulate`, { method: 'POST', orgId, principalId, body: {} })
}

export function requestGovernancePolicyPromotion(id: string, simulationId: string, targetState: 'shadow' | 'active', orgId: string, principalId: string): Promise<GovernancePolicyPromotion> {
	return axisFetch<GovernancePolicyPromotion>(`/api/governance-policy-versions/${encodeURIComponent(id)}/promotions`, { method: 'POST', orgId, principalId, body: { simulation_id: simulationId, target_state: targetState } })
}

export function listGovernancePolicyPromotions(orgId: string, principalId: string): Promise<GovernancePolicyPromotion[]> {
	return axisFetch<GovernancePolicyPromotion[]>('/api/governance-policy-promotions?limit=200', { orgId, principalId })
}

export function decideGovernancePolicyPromotion(id: string, decision: 'approve' | 'reject', orgId: string, principalId: string): Promise<GovernancePolicyPromotion> {
	return axisFetch<GovernancePolicyPromotion>(`/api/governance-policy-promotions/${encodeURIComponent(id)}/${decision}`, { method: 'POST', orgId, principalId, body: { reason: 'console' } })
}

export function listGovernancePolicyEvaluations(orgId: string, principalId: string): Promise<GovernancePolicyEvaluation[]> {
	return axisFetch<GovernancePolicyEvaluation[]>('/api/governance-policy-evaluations?limit=200', { orgId, principalId })
}

export function listGovernancePolicyChanges(orgId: string, principalId: string): Promise<GovernancePolicyChange[]> {
	return axisFetch<GovernancePolicyChange[]>('/api/governance-policy-changelog?limit=200', { orgId, principalId })
}

export function revokeVirployeeDelegation(virployeeId: string, delegationId: string, expectedRevision: number, orgId: string, principalId: string): Promise<VirployeeDelegation> {
	return axisFetch<VirployeeDelegation>(`/api/virployees/${encodeURIComponent(virployeeId)}/delegations/${encodeURIComponent(delegationId)}/revoke`, {
		method: 'POST', orgId, principalId, body: { expected_revision: expectedRevision, reason: 'console' },
	})
}

export function listAssistCases(
	orgId: string,
	principalId: string,
	filters: { subjectId?: string; ownerVirployeeId?: string; caseId?: string } = {},
): Promise<AssistCase[]> {
	const params = new URLSearchParams({ limit: '200' })
	if (filters.subjectId) params.set('subject_id', filters.subjectId)
	if (filters.ownerVirployeeId) params.set('owner_virployee_id', filters.ownerVirployeeId)
	if (filters.caseId) params.set('case_id', filters.caseId)
	return axisFetch<AssistCase[]>(`/api/assist-cases?${params.toString()}`, { orgId, principalId })
}

export function listHandoffs(orgId: string, principalId: string): Promise<Handoff[]> {
	return axisFetch<Handoff[]>('/api/handoffs?limit=200', { orgId, principalId })
}

export function createHandoff(
	input: { case_id: string; source_run_id?: string; to_virployee_id: string; reason_code: string; note?: string },
	orgId: string,
	principalId: string,
): Promise<Handoff> {
	return axisFetch<Handoff>('/api/handoffs', { method: 'POST', orgId, principalId, body: input })
}

export function decideHandoff(
	id: string,
	decision: 'accept' | 'reject' | 'cancel',
	version: number,
	orgId: string,
	principalId: string,
	note = '',
): Promise<Handoff> {
	return axisFetch<Handoff>(`/api/handoffs/${encodeURIComponent(id)}/${decision}`, {
		method: 'POST', orgId, principalId, body: { version, note },
	})
}

export function listHumanReviews(orgId: string, principalId: string): Promise<HumanReview[]> {
	return axisFetch<HumanReview[]>('/api/human-reviews', { orgId, principalId })
}

export function claimHumanReview(id: string, orgId: string, principalId: string): Promise<HumanReview> {
	return axisFetch<HumanReview>(`/api/human-reviews/${encodeURIComponent(id)}/claim`, {
		method: 'POST', orgId, principalId, body: {},
	})
}

export function resolveHumanReview(
	id: string,
	outcome: 'handled_externally' | 'handoff_requested' | 'dismissed',
	orgId: string,
	principalId: string,
	note = '',
	handoffId = '',
): Promise<HumanReview> {
	return axisFetch<HumanReview>(`/api/human-reviews/${encodeURIComponent(id)}/resolve`, {
		method: 'POST', orgId, principalId, body: { outcome, note, handoff_id: handoffId },
	})
}

export function listOrchestrationPolicies(orgId: string, principalId: string): Promise<OrchestrationPolicy[]> {
	return axisFetch<OrchestrationPolicy[]>('/api/orchestration-policies', { orgId, principalId })
}

export function upsertOrchestrationPolicy(
	input: Omit<OrchestrationPolicy, 'id' | 'org_id' | 'version' | 'created_at' | 'updated_at' | 'max_depth'>,
	orgId: string,
	principalId: string,
): Promise<OrchestrationPolicy> {
	return axisFetch<OrchestrationPolicy>('/api/orchestration-policies', {
		method: 'PUT', orgId, principalId, body: input,
	})
}

export function listSpecialistRoutes(orgId: string, principalId: string): Promise<SpecialistRoute[]> {
	return axisFetch<SpecialistRoute[]>('/api/specialist-routes', { orgId, principalId })
}

export function upsertSpecialistRoute(
	input: Omit<SpecialistRoute, 'id' | 'org_id' | 'version' | 'created_at' | 'updated_at'>,
	orgId: string,
	principalId: string,
): Promise<SpecialistRoute> {
	return axisFetch<SpecialistRoute>('/api/specialist-routes', {
		method: 'PUT', orgId, principalId, body: input,
	})
}

export function listOrgs(
  lifecycle: 'active' | 'archived' | 'trash',
  principalId: string,
): Promise<AxisOrg[]> {
  const path =
    lifecycle === 'active'
      ? '/api/orgs'
      : `/api/orgs?lifecycle=${encodeURIComponent(lifecycle)}`
  return axisFetch<OrgsListResponse>(path, { principalId }).then((payload) => payload.data ?? [])
}

export function createOrg(input: OrgInput, principalId: string): Promise<AxisOrg> {
  return axisFetch<AxisOrg>('/api/orgs', {
    method: 'POST',
    principalId,
    body: input,
  })
}

export function updateOrg(id: string, input: OrgInput, principalId: string): Promise<AxisOrg> {
  return axisFetch<AxisOrg>(`/api/orgs/${encodeURIComponent(id)}`, {
    method: 'PUT',
    principalId,
    body: input,
  })
}

export function archiveOrg(id: string, principalId: string): Promise<void> {
  return orgLifecycleAction(id, 'archive', principalId)
}

export function unarchiveOrg(id: string, principalId: string): Promise<void> {
  return orgLifecycleAction(id, 'unarchive', principalId)
}

export function trashOrg(id: string, principalId: string): Promise<void> {
  return orgLifecycleAction(id, 'trash', principalId)
}

export function restoreOrg(id: string, principalId: string): Promise<void> {
  return orgLifecycleAction(id, 'restore', principalId)
}

export function purgeOrg(id: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/orgs/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    principalId,
  })
}

export function listProducts(
  lifecycle: 'active' | 'archived' | 'trash',
  organizationId: string,
  principalId: string,
): Promise<Product[]> {
  const base = `/api/organizations/${encodeURIComponent(organizationId)}/products`
  const path =
    lifecycle === 'active'
      ? base
      : `${base}?lifecycle=${encodeURIComponent(lifecycle)}`
  return axisFetch<ProductsListResponse>(path, { principalId }).then((payload) => payload.data ?? [])
}

export function createProduct(input: ProductInput, organizationId: string, principalId: string): Promise<Product> {
	return axisFetch<Product>(`/api/organizations/${encodeURIComponent(organizationId)}/products`, {
    method: 'POST',
    principalId,
    body: input,
  })
}

export function updateProduct(id: string, input: ProductInput, organizationId: string, principalId: string): Promise<Product> {
	return axisFetch<Product>(`/api/organizations/${encodeURIComponent(organizationId)}/products/${encodeURIComponent(id)}`, {
    method: 'PUT',
    principalId,
    body: { name: input.name },
  })
}

export function archiveProduct(id: string, organizationId: string, principalId: string): Promise<void> {
	return productLifecycleAction(id, 'archive', organizationId, principalId)
}

export function unarchiveProduct(id: string, organizationId: string, principalId: string): Promise<void> {
	return productLifecycleAction(id, 'unarchive', organizationId, principalId)
}

export function trashProduct(id: string, organizationId: string, principalId: string): Promise<void> {
	return productLifecycleAction(id, 'trash', organizationId, principalId)
}

export function restoreProduct(id: string, organizationId: string, principalId: string): Promise<void> {
	return productLifecycleAction(id, 'restore', organizationId, principalId)
}

export function purgeProduct(id: string, organizationId: string, principalId: string): Promise<void> {
	return axisFetch<void>(`/api/organizations/${encodeURIComponent(organizationId)}/products/${encodeURIComponent(id)}/purge`, {
		method: 'DELETE',
		principalId,
	})
}

export function listApprovals(
	orgId: string,
	principalId: string,
	status: Approval['status'] = 'pending',
	limit = 50,
): Promise<Approval[]> {
	return listApprovalsPage(orgId, principalId, status, { limit }).then((page) => page.items)
}

export function listApprovalsPage(
	orgId: string,
	principalId: string,
	status: Approval['status'] = 'pending',
	options: { limit?: number; cursor?: string } = {},
): Promise<PaginatedList<Approval>> {
	const params = new URLSearchParams()
	params.set('status', status)
	params.set('limit', String(options.limit ?? 50))
	if (options.cursor) {
		params.set('cursor', options.cursor)
	}
	return axisFetch<ApprovalsListResponse>(
		`/api/approvals?${params.toString()}`,
		{ orgId, principalId },
	).then((payload) => parsePaginatedResponse<Approval>(payload))
}

export function getApproval(id: string, orgId: string, principalId: string): Promise<Approval> {
	return axisFetch<Approval>(`/api/approvals/${encodeURIComponent(id)}`, {
		orgId,
		principalId,
	})
}

export function approveApproval(id: string, orgId: string, principalId: string, note = ''): Promise<Approval> {
	return axisFetch<Approval>(`/api/approvals/${encodeURIComponent(id)}/approve`, {
		method: 'POST',
		orgId,
		principalId,
		body: { note },
	})
}

export function rejectApproval(id: string, orgId: string, principalId: string, note = ''): Promise<Approval> {
	return axisFetch<Approval>(`/api/approvals/${encodeURIComponent(id)}/reject`, {
		method: 'POST',
		orgId,
		principalId,
		body: { note },
	})
}

export function reviewApproval(id: string, orgId: string, principalId: string, note: string): Promise<Approval> {
	return axisFetch<Approval>(`/api/approvals/${encodeURIComponent(id)}/review`, {
		method: 'POST',
		orgId,
		principalId,
		body: { note },
	})
}

async function resolveAxisAuthToken(): Promise<string> {
  if (!axisAuthTokenGetter) return ''
  try {
    return (await axisAuthTokenGetter())?.trim() ?? ''
  } catch (error) {
    console.error('axis_console_v2_auth_token_failed', error)
    return ''
  }
}

export function listVirployees(
  lifecycle: 'active' | 'archived' | 'trash',
  orgId: string,
  principalId: string,
): Promise<Virployee[]> {
  const path =
    lifecycle === 'active'
      ? '/api/virployees'
      : lifecycle === 'archived'
        ? '/api/virployees/archived'
        : '/api/virployees/trash'
  return axisFetch<VirployeesListResponse>(path, { orgId, principalId }).then((payload) => payload.data ?? [])
}

export function listVirployeeAutonomyLevels(
  orgId: string,
  principalId: string,
): Promise<VirployeeAutonomyLevel[]> {
  return axisFetch<AutonomyLevelsResponse>('/api/virployees/autonomy-levels', { orgId, principalId })
    .then((payload) => payload.data ?? [])
}

export function createVirployee(input: VirployeeInput, orgId: string, principalId: string): Promise<Virployee> {
  return axisFetch<Virployee>('/api/virployees', {
    method: 'POST',
    orgId,
    principalId,
    body: input,
  })
}

export function updateVirployee(
  id: string,
  input: VirployeeInput,
  orgId: string,
  principalId: string,
): Promise<Virployee> {
  return axisFetch<Virployee>(`/api/virployees/${encodeURIComponent(id)}`, {
    method: 'PUT',
    orgId,
    principalId,
    body: input,
  })
}

export function getVirployeeRuntimeContext(
  id: string,
  orgId: string,
  principalId: string,
): Promise<VirployeeRuntimeContext> {
  return axisFetch<VirployeeRuntimeContext>(
    `/api/virployees/${encodeURIComponent(id)}/runtime-context`,
    { orgId, principalId },
  )
}

export function listVirployeeMemories(id:string, state:string, query:string, cursor:string, scope:MemoryScope, orgId:string, principalId:string):Promise<MemoryPage> {
  const params = new URLSearchParams({state, q:query, limit:'50', scope_type:scope.type})
  if (cursor) params.set('cursor',cursor)
  if (scope.subject_id) params.set('subject_id', scope.subject_id)
  if (scope.case_id) params.set('case_id', scope.case_id)
  return axisFetch<MemoryPage>(`/api/virployees/${encodeURIComponent(id)}/memories?${params}`,{orgId,principalId})
}
export function createVirployeeMemory(id:string,input:MemoryInput,orgId:string,principalId:string):Promise<VirployeeMemory>{return axisFetch(`/api/virployees/${encodeURIComponent(id)}/memories`,{method:'POST',orgId,principalId,body:input})}
export function updateVirployeeMemory(virployeeId:string,id:string,input:MemoryInput,expectedVersion:number,orgId:string,principalId:string):Promise<VirployeeMemory>{return axisFetch(`/api/virployees/${encodeURIComponent(virployeeId)}/memories/${encodeURIComponent(id)}`,{method:'PUT',orgId,principalId,body:{...input,expected_version:expectedVersion}})}
export function recallVirployeeMemories(id:string,query:string,scope:MemoryScope,orgId:string,principalId:string):Promise<MemoryRecall>{return axisFetch(`/api/virployees/${encodeURIComponent(id)}/memories/recall`,{method:'POST',orgId,principalId,body:{query,scope}})}
export function lifecycleVirployeeMemory(virployeeId:string,id:string,action:'archive'|'unarchive'|'trash'|'restore'|'purge',orgId:string,principalId:string):Promise<void>{return axisFetch(`/api/virployees/${encodeURIComponent(virployeeId)}/memories/${encodeURIComponent(id)}/${action}`,{method:action==='purge'?'DELETE':'POST',orgId,principalId})}

export function dryRunVirployee(
  id: string,
  input: string,
  orgId: string,
  principalId: string,
): Promise<VirployeeDryRun> {
  return axisFetch<VirployeeDryRun>(`/api/virployees/${encodeURIComponent(id)}/dry-run`, {
    method: 'POST',
    orgId,
    principalId,
    body: { input },
  })
}

export function checkVirployeeExecutionGate(
  id: string,
  input: string,
  orgId: string,
  principalId: string,
  confirmedDraft?: VirployeeConfirmedDraft,
): Promise<VirployeeExecutionGate> {
  return axisFetch<VirployeeExecutionGate>(`/api/virployees/${encodeURIComponent(id)}/execution-gate`, {
    method: 'POST',
    orgId,
    principalId,
    body: confirmedDraft ? { input, confirmed_draft: confirmedDraft } : { input },
  })
}

export function listVirployeeRuns(
  id: string,
  orgId: string,
  principalId: string,
  limit = 20,
): Promise<VirployeeRunTrace[]> {
  return axisFetch<VirployeeRunTracesListResponse>(
    `/api/virployees/${encodeURIComponent(id)}/runs?limit=${encodeURIComponent(String(limit))}`,
    { orgId, principalId },
  ).then((payload) => payload.data ?? [])
}

export function simulateApprovedVirployeeExecution(
  id: string,
  approvalId: string,
  orgId: string,
  principalId: string,
): Promise<VirployeeRunTrace> {
  return axisFetch<VirployeeRunTrace>(`/api/virployees/${encodeURIComponent(id)}/simulated-executions`, {
    method: 'POST',
    orgId,
    principalId,
    body: { approval_id: approvalId },
  })
}

export function executeApprovedVirployeeAction(
  id: string,
  approvalId: string,
  orgId: string,
  principalId: string,
): Promise<VirployeeRunTrace> {
  return axisFetch<VirployeeRunTrace>(`/api/virployees/${encodeURIComponent(id)}/executions`, {
    method: 'POST',
    orgId,
    principalId,
    body: { approval_id: approvalId },
  })
}

export function archiveVirployee(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('virployees', id, 'archive', orgId, principalId)
}

export function unarchiveVirployee(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('virployees', id, 'unarchive', orgId, principalId)
}

export function trashVirployee(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('virployees', id, 'trash', orgId, principalId)
}

export function restoreVirployee(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('virployees', id, 'restore', orgId, principalId)
}

export function purgeVirployee(id: string, orgId: string, principalId: string): Promise<void> {
  // POST, not DELETE: browser extensions (ad blockers) silently block DELETE.
  return axisFetch<void>(`/api/virployees/${encodeURIComponent(id)}/purge`, {
    method: 'POST',
    orgId,
    principalId,
  })
}

export function listJobRoles(
  lifecycle: 'active' | 'archived' | 'trash',
  orgId: string,
  principalId: string,
): Promise<JobRole[]> {
  const path =
    lifecycle === 'active'
      ? '/api/job-roles'
      : lifecycle === 'archived'
        ? '/api/job-roles?lifecycle=archived'
        : '/api/job-roles?lifecycle=trash'
  return axisFetch<JobRolesListResponse>(path, { orgId, principalId }).then((payload) => payload.data ?? [])
}

export function createJobRole(input: JobRoleInput, orgId: string, principalId: string): Promise<JobRole> {
  return axisFetch<JobRole>('/api/job-roles', {
    method: 'POST',
    orgId,
    principalId,
    body: input,
  })
}

export function updateJobRole(
  id: string,
  input: JobRoleInput,
  orgId: string,
  principalId: string,
): Promise<JobRole> {
  return axisFetch<JobRole>(`/api/job-roles/${encodeURIComponent(id)}`, {
    method: 'PUT',
    orgId,
    principalId,
    body: input,
  })
}

export function archiveJobRole(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('job-roles', id, 'archive', orgId, principalId)
}

export function unarchiveJobRole(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('job-roles', id, 'unarchive', orgId, principalId)
}

export function trashJobRole(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('job-roles', id, 'trash', orgId, principalId)
}

export function restoreJobRole(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('job-roles', id, 'restore', orgId, principalId)
}

export function purgeJobRole(id: string, orgId: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/job-roles/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    orgId,
    principalId,
  })
}

export type CapabilityStats = {
  capability_key: string
  dry_runs: number
  dry_runs_allowed: number
  gates: number
  gates_passed: number
  executions_succeeded: number
  executions_failed: number
  // -1 is the "no data" sentinel: no finished executions to rate.
  success_rate: number
}

export function listCapabilityStats(orgId: string, principalId: string): Promise<CapabilityStats[]> {
  return axisFetch<{ data: CapabilityStats[] }>('/api/capability-stats', { orgId, principalId }).then(
    (payload) => payload.data ?? [],
  )
}

// --- Fase 4: procedural-learning proposals (the review queue) ---

export type LearningProposalStatus = 'pending' | 'accepted' | 'dismissed'

export type LearningProposal = {
  id: string
  org_id: string
  virployee_id: string
  capability_key: string
  title: string
  content: string
  content_hash: string
  evidence: Record<string, unknown>
  source_trace_ids: string[]
  status: LearningProposalStatus
  proposed_by: 'analyzer' | 'llm'
  succeeded_watermark: number
  decided_by?: string
  decided_at?: string | null
  memory_id?: string | null
  created_at: string
  updated_at: string
}

export type LearningEvalCheck = {
  key: string
  status: 'pass' | 'blocked'
  reason: string
}

export type LearningEvalReport = {
  passed: boolean
  checks: LearningEvalCheck[]
}

export type LearningAcceptResult = {
  proposal: LearningProposal
  eval: LearningEvalReport
}

export type LearningScanResult = {
  threshold: number
  candidates: number
  proposed: number
  skipped: number
  proposals: Array<{ id: string; virployee_id: string; capability_key: string; title: string }>
}

export function listLearningProposals(
  orgId: string,
  principalId: string,
  status: LearningProposalStatus = 'pending',
  virployeeId?: string,
): Promise<LearningProposal[]> {
  const params = new URLSearchParams()
  params.set('status', status)
  if (virployeeId) {
    params.set('virployee_id', virployeeId)
  }
  return axisFetch<{ data: LearningProposal[] }>(`/api/learning/proposals?${params.toString()}`, {
    orgId,
    principalId,
  }).then((payload) => payload.data ?? [])
}

export function getLearningProposal(id: string, orgId: string, principalId: string): Promise<LearningProposal> {
  return axisFetch<LearningProposal>(`/api/learning/proposals/${encodeURIComponent(id)}`, {
    orgId,
    principalId,
  })
}

export function acceptLearningProposal(
  id: string,
  orgId: string,
  principalId: string,
): Promise<LearningAcceptResult> {
  return axisFetch<LearningAcceptResult>(`/api/learning/proposals/${encodeURIComponent(id)}/accept`, {
    method: 'POST',
    orgId,
    principalId,
    body: {},
  })
}

export function dismissLearningProposal(
  id: string,
  orgId: string,
  principalId: string,
): Promise<LearningProposal> {
  return axisFetch<LearningProposal>(`/api/learning/proposals/${encodeURIComponent(id)}/dismiss`, {
    method: 'POST',
    orgId,
    principalId,
    body: {},
  })
}

export function scanLearning(orgId: string, principalId: string): Promise<LearningScanResult> {
  return axisFetch<LearningScanResult>('/api/learning/scan', {
    method: 'POST',
    orgId,
    principalId,
    body: {},
  })
}

export function listCapabilities(
  lifecycle: 'active' | 'archived' | 'trash',
  orgId: string,
  principalId: string,
): Promise<Capability[]> {
  const path =
    lifecycle === 'active'
      ? '/api/capabilities'
      : lifecycle === 'archived'
        ? '/api/capabilities?lifecycle=archived'
        : '/api/capabilities?lifecycle=trash'
  return axisFetch<CapabilitiesListResponse>(path, { orgId, principalId }).then((payload) => payload.data ?? [])
}

export function createCapability(input: CapabilityInput, orgId: string, principalId: string): Promise<Capability> {
  return axisFetch<Capability>('/api/capabilities', {
    method: 'POST',
    orgId,
    principalId,
    body: {
      capability_key: input.capability_key,
      name: input.name,
      description: input.description,
      required_autonomy: input.required_autonomy,
      risk_class: input.risk_class,
      side_effect_class: input.side_effect_class,
      requires_nexus_approval: input.requires_nexus_approval,
      evidence_required: input.evidence_required,
      rollback_capability_key: input.rollback_capability_key,
    },
  })
}

export function updateCapability(
  id: string,
  input: CapabilityInput,
  orgId: string,
  principalId: string,
): Promise<Capability> {
  return axisFetch<Capability>(`/api/capabilities/${encodeURIComponent(id)}`, {
    method: 'PUT',
    orgId,
    principalId,
    body: {
      name: input.name,
      description: input.description,
      required_autonomy: input.required_autonomy,
      risk_class: input.risk_class,
      side_effect_class: input.side_effect_class,
      requires_nexus_approval: input.requires_nexus_approval,
      evidence_required: input.evidence_required,
      rollback_capability_key: input.rollback_capability_key,
    },
  })
}

export function updateCapabilityManifest(
  id: string,
  manifest: CapabilityManifestInput,
  orgId: string,
  principalId: string,
): Promise<Capability> {
  return axisFetch<Capability>(`/api/capabilities/${encodeURIComponent(id)}/manifest`, {
    method: 'PUT', orgId, principalId, body: manifest,
  })
}

export function conformCapability(id: string, orgId: string, principalId: string): Promise<Capability> {
  return axisFetch<Capability>(`/api/capabilities/${encodeURIComponent(id)}/conform`, {
    method: 'POST', orgId, principalId, body: {},
  })
}

export function activateCapability(id: string, orgId: string, principalId: string): Promise<Capability> {
  return axisFetch<Capability>(`/api/capabilities/${encodeURIComponent(id)}/activate`, {
    method: 'POST', orgId, principalId, body: {},
  })
}

export function archiveCapability(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('capabilities', id, 'archive', orgId, principalId)
}

export function unarchiveCapability(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('capabilities', id, 'unarchive', orgId, principalId)
}

export function trashCapability(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('capabilities', id, 'trash', orgId, principalId)
}

export function restoreCapability(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('capabilities', id, 'restore', orgId, principalId)
}

export function purgeCapability(id: string, orgId: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/capabilities/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    orgId,
    principalId,
  })
}

export function listProfileTemplates(
  lifecycle: 'active' | 'archived' | 'trash',
  orgId: string,
  principalId: string,
): Promise<ProfileTemplate[]> {
  const path =
    lifecycle === 'active'
      ? '/api/profile-templates'
      : lifecycle === 'archived'
        ? '/api/profile-templates?lifecycle=archived'
        : '/api/profile-templates?lifecycle=trash'
  return axisFetch<ProfileTemplatesListResponse>(path, { orgId, principalId }).then((payload) => payload.data ?? [])
}

export function createProfileTemplate(
  input: ProfileTemplateInput,
  orgId: string,
  principalId: string,
): Promise<ProfileTemplate> {
  return axisFetch<ProfileTemplate>('/api/profile-templates', {
    method: 'POST',
    orgId,
    principalId,
    body: input,
  })
}

export function updateProfileTemplate(
  id: string,
  input: ProfileTemplateInput,
  orgId: string,
  principalId: string,
): Promise<ProfileTemplate> {
  return axisFetch<ProfileTemplate>(`/api/profile-templates/${encodeURIComponent(id)}`, {
    method: 'PUT',
    orgId,
    principalId,
    body: input,
  })
}

export function archiveProfileTemplate(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('profile-templates', id, 'archive', orgId, principalId)
}

export function unarchiveProfileTemplate(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('profile-templates', id, 'unarchive', orgId, principalId)
}

export function trashProfileTemplate(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('profile-templates', id, 'trash', orgId, principalId)
}

export function restoreProfileTemplate(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('profile-templates', id, 'restore', orgId, principalId)
}

export function purgeProfileTemplate(id: string, orgId: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/profile-templates/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    orgId,
    principalId,
  })
}

export function listUsers(
  lifecycle: 'active' | 'archived' | 'trash',
  orgId: string,
  principalId: string,
): Promise<OrgUser[]> {
  const path =
    lifecycle === 'active'
      ? '/api/users'
      : lifecycle === 'archived'
        ? '/api/users?lifecycle=archived'
        : '/api/users?lifecycle=trash'
  return axisFetch<UsersListResponse>(path, { orgId, principalId }).then((payload) => payload.data ?? [])
}

export function createUser(input: OrgUserInput, orgId: string, principalId: string): Promise<OrgUser> {
  return axisFetch<OrgUser>('/api/users', {
    method: 'POST',
    orgId,
    principalId,
    body: input,
  })
}

export function updateUser(
  id: string,
  input: OrgUserInput,
  orgId: string,
  principalId: string,
): Promise<OrgUser> {
  return axisFetch<OrgUser>(`/api/users/${encodeURIComponent(id)}`, {
    method: 'PUT',
    orgId,
    principalId,
    body: input,
  })
}

export function archiveUser(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('users', id, 'archive', orgId, principalId)
}

export function unarchiveUser(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('users', id, 'unarchive', orgId, principalId)
}

export function trashUser(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('users', id, 'trash', orgId, principalId)
}

export function restoreUser(id: string, orgId: string, principalId: string): Promise<void> {
  return lifecycleAction('users', id, 'restore', orgId, principalId)
}

export function purgeUser(id: string, orgId: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/users/${encodeURIComponent(id)}/purge`, {
    method: 'DELETE',
    orgId,
    principalId,
  })
}

async function responseErrorMessage(response: Response): Promise<string> {
  let fallback = `Request failed with ${response.status}`
  try {
    const payload = await response.json()
    fallback = payload?.message || payload?.error || fallback
  } catch {
    // Keep the status based fallback for empty/non-JSON responses.
  }
  return fallback
}

function lifecycleAction(
  resource: 'virployees' | 'job-roles' | 'capabilities' | 'profile-templates' | 'users',
  id: string,
  action: string,
  orgId: string,
  principalId: string,
): Promise<void> {
  return axisFetch<void>(`/api/${resource}/${encodeURIComponent(id)}/${action}`, {
    method: 'POST',
    orgId,
    principalId,
    body: { reason: 'console' },
  })
}

function orgLifecycleAction(id: string, action: string, principalId: string): Promise<void> {
  return axisFetch<void>(`/api/orgs/${encodeURIComponent(id)}/${action}`, {
    method: 'POST',
    principalId,
    body: { reason: 'console' },
  })
}

function productLifecycleAction(id: string, action: string, organizationId: string, principalId: string): Promise<void> {
	return axisFetch<void>(`/api/organizations/${encodeURIComponent(organizationId)}/products/${encodeURIComponent(id)}/${action}`, {
    method: 'POST',
    principalId,
    body: { reason: 'console' },
  })
}
