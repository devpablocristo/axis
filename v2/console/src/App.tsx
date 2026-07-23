import { Activity, ArchiveRestore, Beaker, BookOpen, Bot, BriefcaseBusiness, ClipboardCheck, FileCode2, GraduationCap, LayoutDashboard, Network, Radar, RefreshCw, Scale, ScrollText, ServerCog, Settings, Siren, SlidersHorizontal, UsersRound, Wrench } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { ApprovalsPage } from './ApprovalsPage'
import { CapabilitiesPage } from './CapabilitiesPage'
import { CoordinationPage } from './CoordinationPage'
import { CompanionGovernancePage } from './CompanionGovernancePage'
import { GovernancePage } from './GovernancePage'
import { LearningProposalsPage } from './LearningProposalsPage'
import { JobRolesPage } from './JobRolesPage'
import { KnowledgeBasesPage } from './KnowledgeBasesPage'
import { MCPGovernancePage } from './MCPGovernancePage'
import { NexusOverviewPage, type NexusDestination } from './NexusOverviewPage'
import { OperationsPage } from './OperationsPage'
import { ProfileTemplatesPage } from './ProfileTemplatesPage'
import { ProfessionalPoliciesPage } from './ProfessionalPoliciesPage'
import { OrganizationAdminPage } from './OrganizationAdminPage'
import { VirployeesPage } from './VirployeesPage'
import { WorkforcePage } from './WorkforcePage'
import { getSession, setAxisProductSurface, type Session } from './api'

type LoadState<T> = {
  data: T | null
  loading: boolean
  error: string
}

export function App({ authSlot }: { authSlot?: ReactNode } = {}) {
  const [session, setSession] = useState<LoadState<Session>>({ data: null, loading: true, error: '' })
  const [orgId, setOrgId] = useState(localStorage.getItem('axis.v2.org_id') || '')
  const [productSurface, setProductSurface] = useState(localStorage.getItem('axis.v2.product_surface') || '')
  const [activePage, setActivePage] = useState<Page>(() => {
    const storedPage = localStorage.getItem('axis.v2.active_page')
    return isPage(storedPage) ? storedPage : 'virployees'
  })
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

  const organizations = session.data?.organizations ?? []
  const selectedOrganization = useMemo(
    () => organizations.find((organization) => organization.id === orgId) ?? null,
    [orgId, organizations],
  )
  const workspaceProducts = useMemo(
    () => (selectedOrganization?.products ?? [])
      .map((product) => ({ value: product.product_surface, label: product.name || product.product_surface }))
      .sort((left, right) => left.label.localeCompare(right.label)),
    [selectedOrganization],
  )
  const workspaceProductValues = useMemo(() => workspaceProducts.map((product) => product.value), [workspaceProducts])
  const selectedProduct = useMemo(
    () => selectedOrganization?.products.find((product) => product.product_surface === productSurface) ?? null,
    [productSurface, selectedOrganization],
  )
  const principalId = session.data?.principal_id || session.data?.actor_id || ''
  const principalEmail = session.data?.user?.email || ''

  useEffect(() => {
		if (organizations.length === 0) return
		if (!orgId || !organizations.some((organization) => organization.id === orgId)) {
			setOrgId(organizations[0].id)
		}
	}, [orgId, organizations])

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
    setAxisProductSurface(productSurface)
    if (productSurface) localStorage.setItem('axis.v2.product_surface', productSurface)
  }, [productSurface])

  useEffect(() => {
    localStorage.setItem('axis.v2.active_page', activePage)
  }, [activePage])

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <Bot aria-hidden="true" />
          <div>
            <strong>Axis</strong>
            <span>Console v2</span>
          </div>
        </div>
        <nav className="nav">
          <span className="nav-section-label nav-section-label--companion">Companion</span>
          <button
            type="button"
            data-domain="companion"
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
            data-domain="companion"
            className={activePage === 'workforce' ? 'active' : ''}
            onClick={() => setActivePage('workforce')}
          >
            <UsersRound aria-hidden="true" />
            Workforce
          </button>
          <button
            type="button"
            data-domain="companion"
            className={activePage === 'coordination' ? 'active' : ''}
            onClick={() => setActivePage('coordination')}
          >
            <Network aria-hidden="true" />
            Coordination
          </button>
          <button
            type="button"
            data-domain="companion"
            className={activePage === 'learning-proposals' ? 'active' : ''}
            onClick={() => setActivePage('learning-proposals')}
          >
            <GraduationCap aria-hidden="true" />
            Learning
          </button>
          <button
            type="button"
            data-domain="companion"
            className={activePage === 'prompts' ? 'active' : ''}
            onClick={() => setActivePage('prompts')}
          >
            <FileCode2 aria-hidden="true" />
            Prompts
          </button>
          <button
            type="button"
            data-domain="companion"
            className={activePage === 'watchers' ? 'active' : ''}
            onClick={() => setActivePage('watchers')}
          >
            <Radar aria-hidden="true" />
            Watchers
          </button>
          <button
            type="button"
            data-domain="companion"
            className={activePage === 'evaluations' ? 'active' : ''}
            onClick={() => setActivePage('evaluations')}
          >
            <Beaker aria-hidden="true" />
            Evaluations
          </button>
          <button
            type="button"
            data-domain="companion"
            className={activePage === 'capabilities' ? 'active' : ''}
            onClick={() => setActivePage('capabilities')}
          >
            <Wrench aria-hidden="true" />
            Capabilities
          </button>
          <button
            type="button"
            data-domain="companion"
            className={activePage === 'job-roles' ? 'active' : ''}
            onClick={() => setActivePage('job-roles')}
          >
            <BriefcaseBusiness aria-hidden="true" />
            Job Roles
          </button>
          <button
            type="button"
            data-domain="companion"
            className={activePage === 'profile-templates' ? 'active' : ''}
            onClick={() => setActivePage('profile-templates')}
          >
            <SlidersHorizontal aria-hidden="true" />
            Profile Templates
          </button>
          <button
            type="button"
            data-domain="companion"
            className={activePage === 'knowledge-bases' ? 'active' : ''}
            onClick={() => setActivePage('knowledge-bases')}
          >
            <BookOpen aria-hidden="true" />
            Knowledge Bases
          </button>
          <button
            type="button"
            data-domain="companion"
            className={activePage === 'professional-policies' ? 'active' : ''}
            onClick={() => setActivePage('professional-policies')}
          >
            <ScrollText aria-hidden="true" />
            Professional Policies
          </button>
          <button
            type="button"
            data-domain="companion"
            className={activePage === 'mcp-governance' ? 'active' : ''}
            onClick={() => setActivePage('mcp-governance')}
          >
            <ServerCog aria-hidden="true" />
            MCP Governance
          </button>
          <span className="nav-section-label nav-section-label--nexus">Nexus</span>
          <button
            type="button"
            data-domain="nexus"
            className={activePage === 'nexus-overview' ? 'active' : ''}
            onClick={() => setActivePage('nexus-overview')}
          >
            <LayoutDashboard aria-hidden="true" />
            Overview
          </button>
          <button
            type="button"
            data-domain="nexus"
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
            data-domain="nexus"
            className={activePage === 'governance' ? 'active' : ''}
            onClick={() => setActivePage('governance')}
          >
            <Scale aria-hidden="true" />
            Policies & access
          </button>
          <button
            type="button"
            data-domain="nexus"
            className={activePage === 'nexus-incidents' ? 'active' : ''}
            onClick={() => setActivePage('nexus-incidents')}
          >
            <Siren aria-hidden="true" />
            Incidents & SLOs
          </button>
          <button
            type="button"
            data-domain="nexus"
            className={activePage === 'nexus-retention' ? 'active' : ''}
            onClick={() => setActivePage('nexus-retention')}
          >
            <ArchiveRestore aria-hidden="true" />
            Holds & exports
          </button>
          <span className="nav-section-label nav-section-label--operations">Operations</span>
          <button
            type="button"
            data-domain="operations"
            className={activePage === 'operations' ? 'active' : ''}
            onClick={() => setActivePage('operations')}
          >
            <Activity aria-hidden="true" />
            Fleet & runtime
          </button>
          <span className="nav-section-label nav-section-label--administration">Administration</span>
          <button
            type="button"
            data-domain="administration"
            className={activePage === 'admin' ? 'active' : ''}
            onClick={() => setActivePage('admin')}
          >
            <Settings aria-hidden="true" />
            Organization
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
			{organizations.length > 0 ? (
              <>
                <label className="topbar-org">
                  <span>Org</span>
                  <select value={orgId} onChange={(event) => setOrgId(event.target.value)}>
					{organizations.map((organization) => (
						<option key={organization.id} value={organization.id}>{organization.name || organization.id}</option>
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
          <OrganizationAdminPage
            organizationId={selectedOrganization?.id ?? ''}
            principalId={principalId}
            productId={selectedProduct?.id ?? ''}
            productSurface={selectedProduct?.product_surface ?? ''}
            onSessionChanged={refresh}
          />
		) : organizations.length === 0 ? (
			<section className="empty-state">No organizations are available for this user.</section>
		) : selectedProduct == null ? (
			<section className="empty-state">No product belongs to the selected organization.</section>
        ) : activePage === 'job-roles' ? (
          <JobRolesPage orgId={(selectedOrganization?.id ?? "")} principalId={principalId} />
        ) : activePage === 'capabilities' ? (
          <CapabilitiesPage orgId={(selectedOrganization?.id ?? "")} principalId={principalId} />
        ) : activePage === 'learning-proposals' ? (
          <LearningProposalsPage orgId={(selectedOrganization?.id ?? "")} principalId={principalId} />
        ) : activePage === 'prompts' || activePage === 'watchers' || activePage === 'evaluations' ? (
          <CompanionGovernancePage
            orgId={selectedOrganization?.id ?? ''}
            principalId={principalId}
            productId={selectedProduct.id}
            initialTab={activePage}
          />
        ) : activePage === 'profile-templates' ? (
          <ProfileTemplatesPage orgId={(selectedOrganization?.id ?? "")} principalId={principalId} />
        ) : activePage === 'knowledge-bases' ? (
          <KnowledgeBasesPage
            orgId={(selectedOrganization?.id ?? "")}
            principalId={principalId}
            productSurface={selectedProduct.product_surface}
          />
        ) : activePage === 'professional-policies' ? (
          <ProfessionalPoliciesPage orgId={(selectedOrganization?.id ?? "")} principalId={principalId} />
        ) : activePage === 'mcp-governance' ? (
          <MCPGovernancePage orgId={(selectedOrganization?.id ?? "")} principalId={principalId} />
        ) : activePage === 'nexus-overview' ? (
          <NexusOverviewPage onNavigate={(destination: NexusDestination) => setActivePage(destination)} />
        ) : activePage === 'governance' ? (
          <GovernancePage orgId={(selectedOrganization?.id ?? "")} principalId={principalId} productSurface={selectedProduct.product_surface} />
        ) : activePage === 'nexus-incidents' ? (
          <OperationsPage orgId={(selectedOrganization?.id ?? "")} principalId={principalId} productSurface={selectedProduct.product_surface} initialTab="incidents" />
        ) : activePage === 'nexus-retention' ? (
          <OperationsPage orgId={(selectedOrganization?.id ?? "")} principalId={principalId} productSurface={selectedProduct.product_surface} initialTab="retention" />
        ) : activePage === 'approvals' ? (
          <ApprovalsPage
            orgId={(selectedOrganization?.id ?? "")}
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
            orgId={(selectedOrganization?.id ?? "")}
            principalId={principalId}
            productSurface={selectedProduct.product_surface}
          />
        ) : activePage === 'workforce' ? (
          <WorkforcePage
            orgId={(selectedOrganization?.id ?? "")}
            principalId={principalId}
            organizationName={selectedOrganization?.name ?? ''}
          />
        ) : activePage === 'operations' ? (
          <OperationsPage orgId={(selectedOrganization?.id ?? "")} principalId={principalId} productSurface={selectedProduct.product_surface} />
        ) : (
          <VirployeesPage
            orgId={(selectedOrganization?.id ?? "")}
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

type Page = 'virployees' | 'job-roles' | 'capabilities' | 'learning-proposals' | 'prompts' | 'watchers' | 'evaluations' | 'profile-templates' | 'knowledge-bases' | 'professional-policies' | 'nexus-overview' | 'governance' | 'mcp-governance' | 'nexus-incidents' | 'nexus-retention' | 'approvals' | 'coordination' | 'workforce' | 'operations' | 'admin'

function isPage(value: string | null): value is Page {
  return value !== null && [
    'virployees',
    'job-roles',
    'capabilities',
    'learning-proposals',
    'prompts',
    'watchers',
    'evaluations',
    'profile-templates',
    'knowledge-bases',
    'professional-policies',
    'nexus-overview',
    'governance',
    'mcp-governance',
    'nexus-incidents',
    'nexus-retention',
    'approvals',
    'coordination',
    'workforce',
    'operations',
    'admin',
  ].includes(value)
}

type ApprovalReviewContext = {
  approvalId: string
  returnVirployeeId?: string
}

function pageTitle(page: Page): string {
  if (page === 'job-roles') return 'Job Roles'
  if (page === 'capabilities') return 'Capabilities'
  if (page === 'learning-proposals') return 'Learning'
  if (page === 'prompts') return 'Prompt governance'
  if (page === 'watchers') return 'Watchers'
  if (page === 'evaluations') return 'Behavior evaluations'
  if (page === 'profile-templates') return 'Profile Templates'
  if (page === 'knowledge-bases') return 'Knowledge Bases'
  if (page === 'professional-policies') return 'Professional Policies'
  if (page === 'nexus-overview') return 'Nexus'
  if (page === 'governance') return 'Policies & access'
  if (page === 'mcp-governance') return 'MCP Governance'
  if (page === 'nexus-incidents') return 'Incidents & SLOs'
  if (page === 'nexus-retention') return 'Holds & exports'
  if (page === 'approvals') return 'Approvals'
  if (page === 'coordination') return 'Specialist coordination'
  if (page === 'workforce') return 'Workforce continuity'
  if (page === 'operations') return 'Operations'
  if (page === 'admin') return 'Admin'
  return 'Virployees'
}
