import { Activity, BookOpen, Bot, BriefcaseBusiness, ClipboardCheck, GraduationCap, Network, RefreshCw, Scale, ScrollText, ServerCog, Settings, ShieldCheck, SlidersHorizontal, UsersRound, Wrench } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { ApprovalsPage } from './ApprovalsPage'
import { CapabilitiesPage } from './CapabilitiesPage'
import { CoordinationPage } from './CoordinationPage'
import { GovernancePage } from './GovernancePage'
import { LearningProposalsPage } from './LearningProposalsPage'
import { JobRolesPage } from './JobRolesPage'
import { KnowledgeBasesPage } from './KnowledgeBasesPage'
import { MCPGovernancePage } from './MCPGovernancePage'
import { OperationsPage } from './OperationsPage'
import { ProfileTemplatesPage } from './ProfileTemplatesPage'
import { ProfessionalPoliciesPage } from './ProfessionalPoliciesPage'
import { TenancyPage } from './TenancyPage'
import { VirployeesPage } from './VirployeesPage'
import { WorkforcePage } from './WorkforcePage'
import { getSession, type Session, type Tenant } from './api'

type LoadState<T> = {
  data: T | null
  loading: boolean
  error: string
}

export function App({ authSlot }: { authSlot?: ReactNode } = {}) {
  const [session, setSession] = useState<LoadState<Session>>({ data: null, loading: true, error: '' })
  const [orgId, setOrgId] = useState(localStorage.getItem('axis.v2.org_id') || '')
  const [productSurface, setProductSurface] = useState(localStorage.getItem('axis.v2.product_surface') || '')
  const [activePage, setActivePage] = useState<Page>('virployees')
  const [approvalReviewContext, setApprovalReviewContext] = useState<ApprovalReviewContext | null>(null)
  const [focusDryRunVirployeeId, setFocusDryRunVirployeeId] = useState('')

  const refresh = useCallback(async () => {
    setSession((current) => ({ data: current.data, loading: true, error: '' }))
    try {
      const next = await getSession()
      setSession({ data: next, loading: false, error: '' })
    } catch (error) {
      setSession((current) => ({
        data: current.data,
        loading: false,
        error: error instanceof Error ? error.message : 'Could not load the session',
      }))
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const tenants = session.data?.tenants ?? []
  const workspaceOrgs = useMemo(() => unique(tenants.map((tenant) => tenant.org_id)), [tenants])
  const orgLabels = useMemo(() => {
    const labels = new Map<string, string>()
    for (const tenant of tenants) {
      if (!labels.has(tenant.org_id)) {
        labels.set(tenant.org_id, tenant.org_name || tenant.org_id)
      }
    }
    return labels
  }, [tenants])
  const workspaceProducts = useMemo(() => {
    const labels = new Map<string, string>()
    for (const tenant of tenants) {
      if (tenant.org_id !== orgId) continue
      if (!labels.has(tenant.product_surface)) {
        labels.set(tenant.product_surface, tenant.product_name || tenant.product_surface)
      }
    }
    return Array.from(labels.entries())
      .map(([value, label]) => ({ value, label }))
      .sort((left, right) => left.label.localeCompare(right.label))
  }, [orgId, tenants])
  const workspaceProductValues = useMemo(() => workspaceProducts.map((product) => product.value), [workspaceProducts])
  const selectedTenant = useMemo(
    () => tenants.find((tenant) => tenant.org_id === orgId && tenant.product_surface === productSurface) ?? null,
    [orgId, productSurface, tenants],
  )
  const principalId = session.data?.principal_id || session.data?.actor_id || ''
  const principalEmail = session.data?.user?.email || ''

  useEffect(() => {
    if (workspaceOrgs.length === 0) return
    if (!orgId || !workspaceOrgs.includes(orgId)) {
      setOrgId(workspaceOrgs[0])
    }
  }, [orgId, workspaceOrgs])

  useEffect(() => {
    if (workspaceProducts.length === 0) return
    if (!productSurface || !workspaceProductValues.includes(productSurface)) {
      setProductSurface(workspaceProductValues[0])
    }
  }, [productSurface, workspaceProductValues])

  useEffect(() => {
    if (orgId) localStorage.setItem('axis.v2.org_id', orgId)
  }, [orgId])

  useEffect(() => {
    if (productSurface) localStorage.setItem('axis.v2.product_surface', productSurface)
  }, [productSurface])

  useEffect(() => {
    if (selectedTenant?.id) localStorage.setItem('axis.v2.tenant_id', selectedTenant.id)
  }, [selectedTenant?.id])

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <ShieldCheck aria-hidden="true" />
          <div>
            <strong>Axis</strong>
            <span>Console v2</span>
          </div>
        </div>
        <nav className="nav">
          <span className="nav-section-label">Operate</span>
          <button
            type="button"
            className={activePage === 'virployees' ? 'active' : ''}
            onClick={() => {
              setFocusDryRunVirployeeId('')
              setActivePage('virployees')
            }}
          >
            <Bot aria-hidden="true" />
            Virployees
          </button>
          <button
            type="button"
            className={activePage === 'approvals' ? 'active' : ''}
            onClick={() => {
              setApprovalReviewContext(null)
              setActivePage('approvals')
            }}
          >
            <ClipboardCheck aria-hidden="true" />
            Approvals
          </button>
          <button
            type="button"
            className={activePage === 'coordination' ? 'active' : ''}
            onClick={() => setActivePage('coordination')}
          >
            <Network aria-hidden="true" />
            Coordination
          </button>
          <button
            type="button"
            className={activePage === 'workforce' ? 'active' : ''}
            onClick={() => setActivePage('workforce')}
          >
            <UsersRound aria-hidden="true" />
            Workforce
          </button>
          <button
            type="button"
            className={activePage === 'learning-proposals' ? 'active' : ''}
            onClick={() => setActivePage('learning-proposals')}
          >
            <GraduationCap aria-hidden="true" />
            Learning
          </button>
          <button
            type="button"
            className={activePage === 'operations' ? 'active' : ''}
            onClick={() => setActivePage('operations')}
          >
            <Activity aria-hidden="true" />
            Operations
          </button>
          <span className="nav-section-label nav-section-label--builder">Builder</span>
          <button
            type="button"
            className={activePage === 'capabilities' ? 'active' : ''}
            onClick={() => setActivePage('capabilities')}
          >
            <Wrench aria-hidden="true" />
            Capabilities
          </button>
          <button
            type="button"
            className={activePage === 'job-roles' ? 'active' : ''}
            onClick={() => setActivePage('job-roles')}
          >
            <BriefcaseBusiness aria-hidden="true" />
            Job Roles
          </button>
          <button
            type="button"
            className={activePage === 'profile-templates' ? 'active' : ''}
            onClick={() => setActivePage('profile-templates')}
          >
            <SlidersHorizontal aria-hidden="true" />
            Profile Templates
          </button>
          <button
            type="button"
            className={activePage === 'knowledge-bases' ? 'active' : ''}
            onClick={() => setActivePage('knowledge-bases')}
          >
            <BookOpen aria-hidden="true" />
            Knowledge Bases
          </button>
          <button
            type="button"
            className={activePage === 'professional-policies' ? 'active' : ''}
            onClick={() => setActivePage('professional-policies')}
          >
            <ScrollText aria-hidden="true" />
            Professional Policies
          </button>
          <span className="nav-section-label nav-section-label--admin">Admin</span>
          <button
            type="button"
            className={activePage === 'governance' ? 'active' : ''}
            onClick={() => setActivePage('governance')}
          >
            <Scale aria-hidden="true" />
            Governance
          </button>
          <button
            type="button"
            className={activePage === 'mcp-governance' ? 'active' : ''}
            onClick={() => setActivePage('mcp-governance')}
          >
            <ServerCog aria-hidden="true" />
            MCP Governance
          </button>
          <button
            type="button"
            className={activePage === 'admin' ? 'active' : ''}
            onClick={() => setActivePage('admin')}
          >
            <Settings aria-hidden="true" />
            Admin
          </button>
        </nav>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div>
            <h1>{pageTitle(activePage)}</h1>
            <p className="axis-muted">{principalEmail || 'loading'}</p>
          </div>
          <div className="toolbar">
            {tenants.length > 0 ? (
              <>
                <label className="topbar-org">
                  <span>Org</span>
                  <select value={orgId} onChange={(event) => setOrgId(event.target.value)}>
                    {workspaceOrgs.map((org) => (
                      <option key={org} value={org}>{orgLabels.get(org) ?? org}</option>
                    ))}
                  </select>
                </label>
                <label className="topbar-org">
                  <span>Product</span>
                  <select value={productSurface} onChange={(event) => setProductSurface(event.target.value)}>
                    {workspaceProducts.map((product) => (
                      <option key={product.value} value={product.value}>{product.label}</option>
                    ))}
                  </select>
                </label>
              </>
            ) : null}
            <button
              type="button"
              className="btn-secondary toolbar-icon-button"
              onClick={() => void refresh()}
              disabled={session.loading}
              title="Refresh session"
            >
              <RefreshCw aria-hidden="true" />
            </button>
            {authSlot ? <div className="auth-slot">{authSlot}</div> : null}
          </div>
        </header>

        {session.error ? <div className="alert alert-error">{session.error}</div> : null}

        {session.loading && !session.data ? (
          <div className="spinner" />
        ) : activePage === 'admin' ? (
          <TenancyPage
            tenantId={selectedTenant?.id ?? ''}
            principalId={principalId}
            sessionTenants={tenants}
            onSessionChanged={refresh}
          />
        ) : tenants.length === 0 ? (
          <section className="empty-state">No tenants are available for this user.</section>
        ) : selectedTenant == null ? (
          <section className="empty-state">No active tenant matches the selected combination.</section>
        ) : activePage === 'job-roles' ? (
          <JobRolesPage tenantId={selectedTenant.id} principalId={principalId} />
        ) : activePage === 'capabilities' ? (
          <CapabilitiesPage tenantId={selectedTenant.id} principalId={principalId} />
        ) : activePage === 'learning-proposals' ? (
          <LearningProposalsPage tenantId={selectedTenant.id} principalId={principalId} />
        ) : activePage === 'profile-templates' ? (
          <ProfileTemplatesPage tenantId={selectedTenant.id} principalId={principalId} />
        ) : activePage === 'knowledge-bases' ? (
          <KnowledgeBasesPage
            tenantId={selectedTenant.id}
            principalId={principalId}
            productSurface={selectedTenant.product_surface}
          />
        ) : activePage === 'professional-policies' ? (
          <ProfessionalPoliciesPage tenantId={selectedTenant.id} principalId={principalId} />
        ) : activePage === 'mcp-governance' ? (
          <MCPGovernancePage tenantId={selectedTenant.id} principalId={principalId} />
        ) : activePage === 'governance' ? (
          <GovernancePage tenantId={selectedTenant.id} principalId={principalId} productSurface={selectedTenant.product_surface} />
        ) : activePage === 'approvals' ? (
          <ApprovalsPage
            tenantId={selectedTenant.id}
            principalId={principalId}
            focusApprovalId={approvalReviewContext?.approvalId ?? ''}
            onReturnToVirployee={approvalReviewContext?.returnVirployeeId
              ? () => {
                  const returnVirployeeId = approvalReviewContext.returnVirployeeId ?? ''
                  setApprovalReviewContext(null)
                  setFocusDryRunVirployeeId(returnVirployeeId)
                  setActivePage('virployees')
                }
              : undefined}
          />
        ) : activePage === 'coordination' ? (
          <CoordinationPage
            tenantId={selectedTenant.id}
            principalId={principalId}
            productSurface={selectedTenant.product_surface}
          />
        ) : activePage === 'workforce' ? (
          <WorkforcePage
            tenantId={selectedTenant.id}
            principalId={principalId}
            organizationName={selectedTenant.org_name}
          />
        ) : activePage === 'operations' ? (
          <OperationsPage tenantId={selectedTenant.id} principalId={principalId} productSurface={selectedTenant.product_surface} />
        ) : (
          <VirployeesPage
            tenantId={selectedTenant.id}
            principalId={principalId}
            focusDryRunVirployeeId={focusDryRunVirployeeId}
            onFocusDryRunConsumed={() => setFocusDryRunVirployeeId('')}
            onReviewApproval={({ approvalId, virployeeId }) => {
              setApprovalReviewContext({ approvalId, returnVirployeeId: virployeeId })
              setActivePage('approvals')
            }}
          />
        )}
      </main>
    </div>
  )
}

function unique(values: string[]): string[] {
  return Array.from(new Set(values.filter(Boolean))).sort((left, right) => left.localeCompare(right))
}

type Page = 'virployees' | 'job-roles' | 'capabilities' | 'learning-proposals' | 'profile-templates' | 'knowledge-bases' | 'professional-policies' | 'governance' | 'mcp-governance' | 'approvals' | 'coordination' | 'workforce' | 'operations' | 'admin'

type ApprovalReviewContext = {
  approvalId: string
  returnVirployeeId?: string
}

function pageTitle(page: Page): string {
  if (page === 'job-roles') return 'Job Roles'
  if (page === 'capabilities') return 'Capabilities'
  if (page === 'learning-proposals') return 'Learning'
  if (page === 'profile-templates') return 'Profile Templates'
  if (page === 'knowledge-bases') return 'Knowledge Bases'
  if (page === 'professional-policies') return 'Professional Policies'
  if (page === 'governance') return 'Governance'
  if (page === 'mcp-governance') return 'MCP Governance'
  if (page === 'approvals') return 'Approvals'
  if (page === 'coordination') return 'Specialist coordination'
  if (page === 'workforce') return 'Workforce continuity'
  if (page === 'operations') return 'Operations'
  if (page === 'admin') return 'Admin'
  return 'Virployees'
}
