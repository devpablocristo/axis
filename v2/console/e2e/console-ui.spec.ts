import { expect, test, type Page, type Route } from '@playwright/test'

const tenantID = 'tenant-axis-e2e'
const principalID = 'dev-user'

const now = '2026-07-09T13:45:00Z'

const session = {
  principal_id: principalID,
  actor_id: principalID,
  org_id: 'dev-org',
  auth_method: 'dev',
  user: { id: principalID, email: 'dev@example.local', status: 'active' },
  tenants: [{
    id: tenantID,
    org_id: 'dev-org',
    org_name: 'dev-org',
    product_surface: 'axis',
    product_name: 'Axis',
    status: 'active',
    state: 'active',
    created_at: now,
    updated_at: now,
    archived_at: null,
    trashed_at: null,
    purge_after: null,
  }],
}

const jobRoles = [{
  id: 'job-calendar',
  slug: 'calendar-assistant',
  name: 'Calendar Assistant',
  mission: 'Manage calendar operations',
  state: 'active',
  created_at: now,
  updated_at: now,
  archived_at: null,
  trashed_at: null,
  purge_after: null,
}]

const capabilities = [
  {
    id: 'cap-events-create',
    capability_key: 'calendar.events.create',
    name: 'Create calendar events',
    description: 'Prepare calendar event drafts',
    required_autonomy: 'A2',
    state: 'active',
    created_at: now,
    updated_at: now,
    archived_at: null,
    trashed_at: null,
    purge_after: null,
  },
  {
    id: 'cap-events-read',
    capability_key: 'calendar.events.read',
    name: 'Read calendar events',
    description: 'Read calendar events',
    required_autonomy: 'A1',
    state: 'active',
    created_at: now,
    updated_at: now,
    archived_at: null,
    trashed_at: null,
    purge_after: null,
  },
]

const profileTemplates = [{
  id: 'profile-calendar',
  name: 'Safe Calendar Operator',
  description: 'Calendar-focused profile',
  system_prompt: 'You are a safe calendar assistant.',
  max_autonomy: 'A3',
  state: 'active',
  created_at: now,
  updated_at: now,
  archived_at: null,
  trashed_at: null,
  purge_after: null,
}]

const users = [{
  id: 'dev-user',
  kind: 'human',
  email: 'dev@example.local',
  role: 'owner',
  status: 'active',
  state: 'active',
  created_at: now,
  updated_at: now,
  archived_at: null,
  trashed_at: null,
  purge_after: null,
}]

const virployees = [
  {
    id: 'virployee-sofia',
    name: 'Sofia Nexus E2E',
    job_role_id: 'job-calendar',
    profile_template_id: 'profile-calendar',
    capability_ids: ['cap-events-create', 'cap-events-read'],
    description: 'Smoke approval flow virployee',
    supervisor_user_id: 'dev-user',
    autonomy: 'A3',
    state: 'active',
    created_at: now,
    updated_at: now,
    archived_at: null,
    trashed_at: null,
    purge_after: null,
  },
  {
    id: 'virployee-long',
    name: 'Smoke Approval Virployee 20260708202631',
    job_role_id: 'job-calendar',
    profile_template_id: 'profile-calendar',
    capability_ids: ['cap-events-create', 'cap-events-read'],
    description: 'Long row to force table width',
    supervisor_user_id: 'dev-user',
    autonomy: 'A3',
    state: 'active',
    created_at: now,
    updated_at: now,
    archived_at: null,
    trashed_at: null,
    purge_after: null,
  },
]

const orgs = [{
  id: 'dev-org',
  name: 'dev-org',
  provider: 'dev',
  provider_org_id: 'dev-org',
  status: 'active',
  state: 'active',
  tenant_count: 1,
  has_tenants: true,
  created_at: now,
  updated_at: now,
  archived_at: null,
  trashed_at: null,
  purge_after: null,
}]

const products = [{
  id: 'product-axis',
  product_surface: 'axis',
  name: 'Axis',
  status: 'active',
  state: 'active',
  created_at: now,
  updated_at: now,
  archived_at: null,
  trashed_at: null,
  purge_after: null,
}]

type ApprovalFixture = {
  id: string
  requester_id: string
  action_type: string
  target_system: string
  target_resource: string
  risk_level: string
  reason: string
  binding_hash: string
  status: 'pending' | 'approved' | 'rejected'
  decided_by: string
  decision_note: string
  decided_at: string | null
  created_at: string
  updated_at: string
}

type ApiFixtureState = {
  approvals: ApprovalFixture[]
  runs: Array<Record<string, unknown>>
  sequence: number
  nextApproval: number
}

const approvals: ApprovalFixture[] = [{
  id: 'approval-1',
  requester_id: principalID,
  action_type: 'calendar.events.create',
  target_system: 'calendar',
  target_resource: 'event',
  risk_level: 'high',
  reason: 'No policy matched; default for risk high',
  binding_hash: 'binding-approval-e2e',
  status: 'pending',
  decided_by: '',
  decision_note: '',
  decided_at: null,
  created_at: now,
  updated_at: now,
}]

const approvedApprovalVolume: ApprovalFixture[] = Array.from({ length: 22 }, (_, index) => {
  const minute = String(44 - index).padStart(2, '0')
  return {
    id: `approval-approved-${String(index + 1).padStart(2, '0')}`,
    requester_id: principalID,
    action_type: 'calendar.events.create',
    target_system: 'calendar',
    target_resource: 'event',
    risk_level: 'high',
    reason: 'No policy matched; default for risk high',
    binding_hash: `binding-approved-${index + 1}`,
    status: 'approved' as const,
    decided_by: principalID,
    decision_note: 'approved fixture',
    decided_at: `2026-07-09T13:${minute}:00Z`,
    created_at: `2026-07-09T13:${minute}:00Z`,
    updated_at: `2026-07-09T13:${minute}:00Z`,
  }
})

test.beforeEach(async ({ page }) => {
  await installApiFixtures(page)
})

test('all main sections render with coherent action buttons', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.topbar h1')).toHaveText('Virployees')

  const nav = page.locator('.nav')
  for (const section of ['Virployees', 'Approvals', 'Capabilities', 'Job Roles', 'Profile Templates', 'Admin']) {
    await nav.getByRole('button', { name: section }).click()
    await expect(page.locator('.topbar h1')).toHaveText(section)
    await assertButtonSystem(page)
  }

  await nav.getByRole('button', { name: 'Admin' }).click()
  const tenancyTabs = page.locator('.tenancy-section__tabs')
  for (const tab of ['Users', 'Tenants', 'Orgs', 'Products']) {
    await tenancyTabs.getByRole('tab', { name: tab }).click()
    await assertButtonSystem(page)
  }
})

test('crud lists use one toolbar and do not render row action columns', async ({ page }) => {
  await page.goto('/')

  const nav = page.locator('.nav')
  for (const section of ['Virployees', 'Capabilities', 'Job Roles', 'Profile Templates']) {
    await nav.getByRole('button', { name: section }).click()
    await expect(page.locator('th.col-actions')).toHaveCount(0)
    await expect(page.locator('.iam-control__bulk-buttons')).toHaveCount(1)
  }

  await nav.getByRole('button', { name: 'Admin' }).click()
  const tenancyTabs = page.locator('.tenancy-section__tabs')
  for (const tab of ['Users', 'Tenants', 'Orgs', 'Products']) {
    await tenancyTabs.getByRole('tab', { name: tab }).click()
    await expect(page.locator('th.col-actions')).toHaveCount(0)
    await expect(page.locator('.iam-control__bulk-buttons')).toHaveCount(1)
  }
})

test('crud lists keep selection and primary columns fixed and expose created time', async ({ page }) => {
  await page.goto('/')

  const nav = page.locator('.nav')
  for (const section of ['Virployees', 'Capabilities', 'Job Roles', 'Profile Templates']) {
    await nav.getByRole('button', { name: section }).click()
    await expect(page.getByRole('columnheader', { name: 'Created' })).toBeVisible()
    await expectStickySelectionAndPrimary(page)
  }

  await nav.getByRole('button', { name: 'Admin' }).click()
  const tenancyTabs = page.locator('.tenancy-section__tabs')
  for (const tab of ['Users', 'Tenants', 'Orgs', 'Products']) {
    await tenancyTabs.getByRole('tab', { name: tab }).click()
    await expect(page.getByRole('columnheader', { name: 'Created' })).toBeVisible()
    await expectStickySelectionAndPrimary(page)
  }
})

test('crud tables stay usable across desktop, tablet, and mobile viewports', async ({ page }) => {
  const viewports = [
    { name: 'desktop', width: 1366, height: 768 },
    { name: 'tablet', width: 900, height: 900 },
    { name: 'mobile', width: 390, height: 844 },
  ]

  for (const viewport of viewports) {
    await page.setViewportSize({ width: viewport.width, height: viewport.height })
    await page.goto('/')

    const nav = page.locator('.nav')
    for (const section of ['Virployees', 'Capabilities', 'Job Roles', 'Profile Templates']) {
      await nav.getByRole('button', { name: section }).click()
      await expectResponsiveCrudTable(page, viewport.name)
    }

    await nav.getByRole('button', { name: 'Admin' }).click()
    const tenancyTabs = page.locator('.tenancy-section__tabs')
    for (const tab of ['Users', 'Tenants', 'Orgs', 'Products']) {
      await tenancyTabs.getByRole('tab', { name: tab }).click()
      await expectResponsiveCrudTable(page, `${viewport.name} ${tab}`)
    }
  }
})

test('virployees keeps selector and name columns fixed during horizontal scroll', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Virployees' }).click()
  await expect(page.getByText('Sofia Nexus E2E')).toBeVisible()

  const tableWrap = page.locator('.virployees-control .table-wrap')
  const checkboxCell = page.locator('.virployees-control tbody tr').first().locator('td').nth(0)
  const nameCell = page.locator('.virployees-control tbody tr').first().locator('td').nth(1)
  const before = await cellPositions(checkboxCell, nameCell)

  await tableWrap.evaluate((element) => {
    element.scrollLeft = 900
  })

  const after = await cellPositions(checkboxCell, nameCell)
  expect(Math.abs(after.checkboxX - before.checkboxX)).toBeLessThanOrEqual(1)
  expect(Math.abs(after.nameX - before.nameX)).toBeLessThanOrEqual(1)
  await expect(page.getByText('Sofia Nexus E2E')).toBeVisible()
})

test('virployee edit, preview, and dry run panels have matching top and bottom action spacing', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Virployees' }).click()
  await page.getByRole('checkbox', { name: 'Select virployee-sofia' }).check()

  await page.getByRole('button', { name: 'Edit' }).click()
  await expect(page.getByText('Edit virployee')).toBeVisible()
  await expectActionBars(page, '.virployee-form-actions--top', '.virployee-edit-form__footer')

  await page.getByRole('button', { name: 'Cancel' }).first().click()
  await page.getByRole('button', { name: 'Preview' }).click()
  await expect(page.getByText('Virployee preview')).toBeVisible()
  await expectActionBars(page, '.virployee-panel-actions--top', '.virployee-panel-footer')

  await page.getByRole('button', { name: 'Close' }).first().click()
  await page.getByRole('button', { name: 'Dry Run' }).click()
  await expect(page.getByRole('heading', { name: 'Dry Run' })).toBeVisible()
  await expectActionBars(page, '.virployee-form-actions--top', '.virployee-edit-form__footer')
})

test('approval flow can approve from Virployees and return with an approved run history state', async ({ page }) => {
  await openSofiaDryRun(page)

  await runDryRunInput(page, 'Agenda una reunion "Approval Test" manana a las 15 con ana@example.com')
  await expect(page.getByText('Ready to check the gate.')).toBeVisible()
  await page.getByRole('button', { name: 'Check execution gate' }).first().click()

  await expect(page.getByRole('button', { name: 'Review approval' }).first()).toBeVisible()
  await page.getByRole('button', { name: 'Review approval' }).first().click()
  await expect(page.locator('.topbar h1')).toHaveText('Approvals')
  await expect(page.getByText('Reviewing approval')).toBeVisible()

  const focusedApproval = page.locator('.approvals-board__card--focused')
  await expect(focusedApproval).toContainText('calendar.events.create')
  await focusedApproval.getByRole('button', { name: 'Approve' }).click()
  await expect(focusedApproval).toContainText('Approved')
  await expect(focusedApproval.getByRole('button', { name: 'Approve' })).toHaveCount(0)
  await expect(focusedApproval.getByRole('button', { name: 'Reject' })).toHaveCount(0)

  await page.getByRole('button', { name: 'Back to Virployee' }).click()
  await expect(page.locator('.topbar h1')).toHaveText('Virployees')
  await expect(page.getByRole('heading', { name: 'Dry Run' })).toBeVisible()

  const latestRun = page.locator('.virployee-run-history__row').first()
  await expect(latestRun).toContainText('Approved')
  await expect(latestRun).toContainText('Requires human approval')
  await expect(latestRun).not.toContainText('Blocked')
  await expect(latestRun.getByRole('button', { name: 'View approval' })).toBeVisible()
})

test('approvals board loads resolved approvals incrementally', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Approvals' }).click()

  const approvedColumn = page.getByLabel('Approved')
  await expect(approvedColumn.locator('.approvals-board__card')).toHaveCount(10)
  await approvedColumn.getByRole('button', { name: 'Load more' }).click()
  await expect(approvedColumn.locator('.approvals-board__card')).toHaveCount(20)
  await expect(approvedColumn.getByRole('button', { name: 'Load more' })).toBeVisible()
})

test('approvals board searches loaded approvals and keeps card positions visible', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Approvals' }).click()

  const search = page.getByLabel('Search approvals')
  const approvedColumn = page.getByLabel('Approved')
  await expect(search).toBeVisible()
  await expect(page.locator('.approvals-toolbar__summary')).toHaveText('11 loaded')
  await expect(approvedColumn.locator('.approvals-board__card-index').first()).toHaveText('#1')

  await search.fill('binding-approved-9')

  await expect(page.locator('.approvals-toolbar__summary')).toHaveText('1 of 11 loaded')
  await expect(approvedColumn.locator('.approvals-board__card')).toHaveCount(1)
  await expect(approvedColumn.locator('.approvals-board__card-index')).toHaveText('#1')
  await expect(page.getByLabel('Pending').locator('.approvals-board__empty')).toContainText('No matching approvals loaded')

  await page.getByRole('button', { name: 'Clear' }).click()

  await expect(search).toHaveValue('')
  await expect(approvedColumn.locator('.approvals-board__card')).toHaveCount(10)
})

test('approval flow can reject and keeps rejected approvals read-only', async ({ page }) => {
  await openSofiaDryRun(page)

  await runDryRunInput(page, 'Agenda una reunion "Reject Test" manana a las 16 con ana@example.com')
  await page.getByRole('button', { name: 'Check execution gate' }).first().click()
  await page.getByRole('button', { name: 'Review approval' }).first().click()

  const focusedApproval = page.locator('.approvals-board__card--focused')
  await expect(focusedApproval).toContainText('Pending')
  await focusedApproval.getByRole('button', { name: 'Reject' }).click()
  await expect(focusedApproval).toContainText('Rejected')
  await expect(focusedApproval.getByRole('button', { name: 'Approve' })).toHaveCount(0)
  await expect(focusedApproval.getByRole('button', { name: 'Reject' })).toHaveCount(0)

  await page.getByRole('button', { name: 'Back to Virployee' }).click()
  const latestRun = page.locator('.virployee-run-history__row').first()
  await expect(latestRun).toContainText('Rejected')
  await expect(latestRun).not.toContainText('Blocked')
})

test('allow and deny gate results do not expose approval actions', async ({ page }) => {
  await openSofiaDryRun(page)

  await runDryRunInput(page, 'Que reuniones tengo manana')
  await page.getByRole('button', { name: 'Check execution gate' }).first().click()
  await expect(page.getByText('Allowed by Nexus').first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'Review approval' })).toHaveCount(0)

  await page.getByLabel('Action input').fill('Agenda una reunion "Smoke Deny" manana a las 16 con ana@example.com')
  await page.getByRole('button', { name: /Run (Dry Run|again)/ }).first().click()
  await page.getByRole('button', { name: 'Check execution gate' }).first().click()
  await expect(page.getByText('Denied by Nexus').first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'Review approval' })).toHaveCount(0)
})

async function installApiFixtures(page: Page) {
  const state = createApiFixtureState()
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/session') return json(route, session)
    if (path === '/api/virployees/autonomy-levels') return json(route, { data: autonomyLevels })
    if (path === '/api/virployees') return json(route, { data: virployees })
    if (path === '/api/virployees/archived' || path === '/api/virployees/trash') return json(route, { data: [] })
    if (path === '/api/virployees/virployee-sofia/runtime-context') return json(route, runtimeContext)
    if (path === '/api/virployees/virployee-sofia/dry-run' && route.request().method() === 'POST') {
      const input = requestInput(route)
      const result = dryRunForInput(input)
      state.runs.unshift(dryRunTrace(input, result, nextSequence(state)))
      return json(route, result)
    }
    if (path === '/api/virployees/virployee-sofia/execution-gate' && route.request().method() === 'POST') {
      const input = requestInput(route)
      const result = executionGateForInput(input, state)
      return json(route, result)
    }
    if (path === '/api/virployees/virployee-sofia/runs') return json(route, { data: state.runs })
    if (path === '/api/job-roles') return json(route, { data: jobRoles })
    if (path === '/api/capabilities') return json(route, { data: capabilities })
    if (path === '/api/profile-templates') return json(route, { data: profileTemplates })
    if (path === '/api/users') return json(route, { data: users })
    if (path === '/api/tenants') return json(route, { data: session.tenants })
    if (path === '/api/orgs') return json(route, { data: orgs })
    if (path === '/api/products') return json(route, { data: products })
    if (path === '/api/approvals') {
      const status = url.searchParams.get('status') ?? 'pending'
      return json(route, paginatedApprovals(state.approvals, status, url.searchParams))
    }
    const approvalDecisionMatch = path.match(/^\/api\/approvals\/([^/]+)\/(approve|reject)$/)
    if (approvalDecisionMatch && route.request().method() === 'POST') {
      const [, approvalID, decision] = approvalDecisionMatch
      const approval = state.approvals.find((item) => item.id === approvalID)
      if (!approval) return json(route, { error: 'approval not found' }, 404)
      if (approval.status !== 'pending') return json(route, { error: 'approval already decided' }, 409)
      approval.status = decision === 'approve' ? 'approved' : 'rejected'
      approval.decided_by = principalID
      approval.decision_note = decision === 'approve' ? 'approved in e2e' : 'rejected in e2e'
      approval.decided_at = '2026-07-09T15:06:00Z'
      approval.updated_at = approval.decided_at
      return json(route, approval)
    }
    const approvalMatch = path.match(/^\/api\/approvals\/([^/]+)$/)
    if (approvalMatch) {
      const approval = state.approvals.find((item) => item.id === approvalMatch[1])
      return approval ? json(route, approval) : json(route, { error: 'approval not found' }, 404)
    }
    return json(route, { data: [] })
  })
}

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })
}

async function openSofiaDryRun(page: Page) {
  await page.goto('/')
  await page.getByRole('button', { name: 'Virployees' }).click()
  await page.getByRole('checkbox', { name: 'Select virployee-sofia' }).check()
  await page.getByRole('button', { name: 'Dry Run' }).click()
  await expect(page.getByRole('heading', { name: 'Dry Run' })).toBeVisible()
}

async function runDryRunInput(page: Page, input: string) {
  await page.getByLabel('Action input').fill(input)
  await page.getByRole('button', { name: /Run (Dry Run|again)/ }).first().click()
  await expect(page.getByText('Dry Run result')).toBeVisible()
}

function createApiFixtureState(): ApiFixtureState {
  return {
    approvals: [...approvals, ...approvedApprovalVolume].map((approval) => ({ ...approval })),
    runs: [],
    sequence: 0,
    nextApproval: 2,
  }
}

function paginatedApprovals(approvals: ApprovalFixture[], status: string, searchParams: URLSearchParams) {
  const limit = Math.max(1, Number(searchParams.get('limit') ?? 50))
  const cursor = Math.max(0, Number(searchParams.get('cursor') ?? 0))
  const items = approvals
    .filter((approval) => approval.status === status)
    .sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))
  const pageItems = items.slice(cursor, cursor + limit)
  const nextCursor = cursor + limit < items.length ? String(cursor + limit) : ''
  return {
    items: pageItems,
    has_more: nextCursor !== '',
    next_cursor: nextCursor,
  }
}

function nextSequence(state: ApiFixtureState): number {
  state.sequence += 1
  return state.sequence
}

function requestInput(route: Route): string {
  const payload = route.request().postDataJSON() as { input?: string } | null
  return String(payload?.input ?? '')
}

function executionGateForInput(input: string, state: ApiFixtureState) {
  const sequence = nextSequence(state)
  const dryRun = dryRunForInput(input)
  if (isReadInput(input)) {
    const result = executionGateResponse(input, dryRun, 'pass', 'Allowed by Nexus')
    state.runs.unshift(executionGateTrace(input, dryRun, sequence, 'allow'))
    return result
  }
  if (isDenyInput(input)) {
    const result = executionGateResponse(input, dryRun, 'blocked', 'Action type is disabled')
    state.runs.unshift(executionGateTrace(input, dryRun, sequence, 'deny'))
    return result
  }

  const approvalID = `approval-${state.nextApproval}`
  state.nextApproval += 1
  const bindingHash = `binding-${sequence}-approval`
  state.approvals.unshift({
    id: approvalID,
    requester_id: principalID,
    action_type: 'calendar.events.create',
    target_system: 'calendar',
    target_resource: 'event',
    risk_level: 'high',
    reason: 'No policy matched; default for risk high',
    binding_hash: bindingHash,
    status: 'pending',
    decided_by: '',
    decision_note: '',
    decided_at: null,
    created_at: `2026-07-09T15:${String(sequence).padStart(2, '0')}:00Z`,
    updated_at: `2026-07-09T15:${String(sequence).padStart(2, '0')}:00Z`,
  })
  const result = executionGateResponse(input, dryRun, 'blocked', 'Requires human approval')
  state.runs.unshift(executionGateTrace(input, dryRun, sequence, 'require_approval', approvalID, bindingHash))
  return result
}

function dryRunForInput(input: string) {
  if (isReadInput(input)) {
    return {
      input,
      runtime_context: runtimeContext,
      intent: {
        matched: true,
        capability_key: 'calendar.events.read',
        domain: 'calendar',
        resource: 'events',
        action: 'read',
        confidence: 0.92,
        matched_by: ['resource:reuniones'],
        rules: [{ type: 'keyword', target: 'resource', value: 'reuniones' }],
      },
      required_capability: {
        id: 'cap-events-read',
        capability_key: 'calendar.events.read',
        name: 'Read calendar events',
        required_autonomy: 'A1',
        matched: true,
      },
      required_autonomy: 'A1',
      virployee_autonomy: 'A3',
      decision: 'allowed',
      reason: 'virployee autonomy allows the required capability',
      next_step: 'would read calendar events without external side effects',
      draft: {
        status: 'not_applicable',
        action: 'calendar.events.read',
        kind: 'calendar_read',
        summary: 'Read calendar events',
        fields: [],
        missing_fields: [],
        notes: [],
      },
    }
  }

  return {
    input,
    runtime_context: runtimeContext,
    intent: {
      matched: true,
      capability_key: 'calendar.events.create',
      domain: 'calendar',
      resource: 'events',
      action: 'create',
      confidence: 0.9,
      matched_by: ['resource:reunion', 'action:agenda'],
      rules: [
        { type: 'keyword', target: 'resource', value: 'reunion' },
        { type: 'keyword', target: 'action', value: 'agenda' },
      ],
    },
    required_capability: {
      id: 'cap-events-create',
      capability_key: 'calendar.events.create',
      name: 'Create calendar events',
      required_autonomy: 'A2',
      matched: true,
    },
    required_autonomy: 'A2',
    virployee_autonomy: 'A3',
    decision: 'allowed',
    reason: 'virployee autonomy allows the required capability',
    next_step: 'would draft or prepare the action without external side effects',
    draft: {
      status: 'needs_input',
      action: 'calendar.events.create',
      kind: 'calendar_event',
      summary: 'Calendar event draft',
      fields: [
        { key: 'title', label: 'Title', value: isDenyInput(input) ? 'Smoke Deny' : titleFromInput(input), source: 'input' },
        { key: 'date_hint', label: 'Date', value: 'manana', source: 'input' },
        { key: 'time', label: 'Time', value: isDenyInput(input) ? '16:00' : '15:00', source: 'input' },
        { key: 'attendees', label: 'Attendees', value: 'ana@example.com', source: 'input' },
      ],
      missing_fields: [],
      notes: [],
    },
  }
}

function executionGateResponse(input: string, dryRun: ReturnType<typeof dryRunForInput>, decision: 'pass' | 'blocked', nextStep: string) {
  return {
    input,
    dry_run: dryRun,
    execution_gate: {
      decision,
      mode: 'simulation',
      will_execute: decision === 'pass',
      required_execution_autonomy: 'A3',
      virployee_autonomy: 'A3',
      checks: [{
        key: decision === 'pass' ? 'nexus_policy' : 'nexus_gate',
        status: decision === 'pass' ? 'pass' : 'blocked',
        reason: nextStep,
      }],
      next_step: nextStep,
    },
  }
}

function dryRunTrace(input: string, dryRun: ReturnType<typeof dryRunForInput>, sequence: number) {
  return {
    id: `run-dry-${sequence}`,
    virployee_id: 'virployee-sofia',
    operation: 'dry_run',
    input_hash: `hash-dry-${sequence}`,
    input_preview: input,
    intent: dryRun.intent,
    capability_id: dryRun.required_capability.id,
    capability_key: dryRun.required_capability.capability_key,
    dry_run_decision: dryRun.decision,
    gate_decision: '',
    gate_checks: [],
    binding_hash: `binding-dry-${sequence}`,
    created_at: `2026-07-09T15:${String(sequence).padStart(2, '0')}:00Z`,
  }
}

function executionGateTrace(
  input: string,
  dryRun: ReturnType<typeof dryRunForInput>,
  sequence: number,
  nexusDecision: 'allow' | 'deny' | 'require_approval',
  approvalID = '',
  bindingHash = `binding-${sequence}-${nexusDecision}`,
) {
  const approvalStatus = nexusDecision === 'require_approval' ? 'pending' : ''
  return {
    id: `run-gate-${sequence}`,
    virployee_id: 'virployee-sofia',
    operation: 'execution_gate',
    input_hash: `hash-gate-${sequence}`,
    input_preview: input,
    intent: dryRun.intent,
    capability_id: dryRun.required_capability.id,
    capability_key: dryRun.required_capability.capability_key,
    dry_run_decision: dryRun.decision,
    gate_decision: nexusDecision === 'allow' ? 'pass' : 'blocked',
    gate_checks: [{
      key: 'nexus_policy',
      status: nexusDecision === 'allow' ? 'pass' : 'blocked',
      reason: nexusDecision === 'allow'
        ? 'Allowed by Nexus'
        : nexusDecision === 'deny'
          ? 'Action type is disabled'
          : 'Requires human approval',
    }],
    nexus_result: {
      available: true,
      decision: nexusDecision,
      risk_level: nexusDecision === 'allow' ? 'low' : 'high',
      status: nexusDecision === 'allow' ? 'allowed' : nexusDecision === 'deny' ? 'denied' : 'pending_approval',
      decision_reason: nexusDecision === 'allow'
        ? 'No policy matched; default for risk low'
        : nexusDecision === 'deny'
          ? 'Action type is disabled'
          : 'No policy matched; default for risk high',
      would_require_approval: nexusDecision === 'require_approval',
      binding_hash: bindingHash,
      approval_id: approvalID,
      approval_status: approvalStatus,
    },
    binding_hash: bindingHash,
    created_at: `2026-07-09T15:${String(sequence).padStart(2, '0')}:00Z`,
  }
}

function isReadInput(input: string): boolean {
  return input.toLowerCase().includes('que reuniones') || input.toLowerCase().includes('qué reuniones')
}

function isDenyInput(input: string): boolean {
  return input.toLowerCase().includes('deny')
}

function titleFromInput(input: string): string {
  const quoted = input.match(/"([^"]+)"/)
  return quoted?.[1] ?? 'Approval Test'
}

async function assertButtonSystem(page: Page) {
  await page.mouse.move(4, 4)
  await page.waitForTimeout(180)
  const actionButtonSelector = [
    'main .iam-control__bulk-buttons button:visible',
    'main .crud-page-shell__header-actions button:visible',
    'main .axis-entity-form-actions button:visible',
    'main .virployee-form-actions button:visible',
    'main .virployee-edit-form__footer button:visible',
    'main .virployee-panel-actions button:visible',
    'main .virployee-panel-footer button:visible',
    'main .approvals-control .page-header button:visible',
    'main .approvals-board__actions button:visible',
    'main .tenancy-section__tabs button:visible',
  ].join(', ')
  const report = await page.locator(actionButtonSelector).evaluateAll((buttons) => {
    return buttons
      .filter((button) => !button.closest('.nav'))
      .map((button) => {
        const style = window.getComputedStyle(button)
        return {
          text: button.textContent?.trim() ?? '',
          radius: style.borderRadius,
          fontFamily: style.fontFamily,
          minHeight: Number.parseFloat(style.minHeight || '0'),
          background: style.backgroundColor,
          color: style.color,
          border: style.borderColor,
          className: button.className,
        }
      })
  })
  expect(report.length).toBeGreaterThan(0)
  for (const button of report) {
    expect(button.radius, button.text).toBe('6px')
    expect(button.minHeight, button.text).toBeGreaterThanOrEqual(32)
    expect(button.fontFamily, button.text).toContain('Inter')
    if (String(button.className).includes('btn-danger')) {
      expect(button.color, button.text).toBe('rgb(218, 30, 40)')
    }
    if (String(button.className).includes('btn-primary')) {
      expect(button.background, button.text).toBe('rgb(47, 95, 152)')
    }
  }
}

async function cellPositions(
  checkboxCell: ReturnType<Page['locator']>,
  nameCell: ReturnType<Page['locator']>,
) {
  const checkboxBox = await checkboxCell.boundingBox()
  const nameBox = await nameCell.boundingBox()
  if (!checkboxBox || !nameBox) throw new Error('Could not measure sticky cells')
  return { checkboxX: checkboxBox.x, nameX: nameBox.x }
}

async function expectStickySelectionAndPrimary(page: Page) {
  const tableWrap = page.locator('.axis-crud-host .table-wrap').first()
  const firstRow = page.locator('.axis-crud-host tbody tr').first()
  await expect(firstRow).toBeVisible()
  const checkboxCell = firstRow.locator('td').nth(0)
  const primaryCell = firstRow.locator('td').nth(1)
  const before = await cellPositions(checkboxCell, primaryCell)
  await tableWrap.evaluate((element) => {
    element.scrollLeft = 900
  })
  await page.waitForTimeout(100)
  const after = await cellPositions(checkboxCell, primaryCell)
  expect(Math.abs(after.checkboxX - before.checkboxX)).toBeLessThanOrEqual(1)
  expect(Math.abs(after.nameX - before.nameX)).toBeLessThanOrEqual(1)
}

async function expectResponsiveCrudTable(page: Page, context: string) {
  const tableWrap = page.locator('.axis-crud-host .table-wrap').first()
  await expect(tableWrap, context).toBeVisible()
  await expect(page.getByRole('columnheader', { name: 'Created' }), context).toBeVisible()
  await expect(page.locator('th.col-actions'), context).toHaveCount(0)
  await expect(page.locator('.iam-control__bulk-buttons'), context).toHaveCount(1)
  await expectStickySelectionAndPrimary(page)

  const metrics = await tableWrap.evaluate((element) => {
    const rect = element.getBoundingClientRect()
    return {
      left: rect.left,
      right: rect.right,
      viewportWidth: window.innerWidth,
      scrollWidth: element.scrollWidth,
      clientWidth: element.clientWidth,
      overflowX: window.getComputedStyle(element).overflowX,
    }
  })

  expect(metrics.left, context).toBeGreaterThanOrEqual(0)
  expect(metrics.right, context).toBeLessThanOrEqual(metrics.viewportWidth + 1)
  expect(metrics.scrollWidth, context).toBeGreaterThanOrEqual(metrics.clientWidth)
  expect(metrics.overflowX, context).toBe('auto')
}

async function expectActionBars(page: Page, topSelector: string, bottomSelector: string) {
  const [top, bottom] = await Promise.all([
    page.locator(topSelector).first().evaluate(actionBarMetrics),
    page.locator(bottomSelector).first().evaluate(actionBarMetrics),
  ])
  expect(top.gap).toBe(bottom.gap)
  expect(top.background).toBe(bottom.background)
  expect(top.justify).toBe(bottom.justify)
}

function actionBarMetrics(element: Element) {
  const style = window.getComputedStyle(element)
  return {
    gap: style.gap,
    background: style.backgroundColor,
    justify: style.justifyContent,
  }
}

const autonomyLevels = [
  { level: 'A0', name: 'Conversation', description: 'Can converse.', allows_required_autonomies: ['A0'] },
  { level: 'A1', name: 'Recommendation', description: 'Can recommend.', allows_required_autonomies: ['A0', 'A1'] },
  { level: 'A2', name: 'Draft', description: 'Can draft.', allows_required_autonomies: ['A0', 'A1', 'A2'] },
  { level: 'A3', name: 'Limited execution', description: 'Can execute limited actions.', allows_required_autonomies: ['A0', 'A1', 'A2', 'A3'] },
  { level: 'A4', name: 'Governed execution', description: 'Can execute governed actions.', allows_required_autonomies: ['A0', 'A1', 'A2', 'A3', 'A4'] },
  { level: 'A5', name: 'Broad autonomy', description: 'Reserved.', allows_required_autonomies: ['A0', 'A1', 'A2', 'A3', 'A4', 'A5'] },
]

const runtimeContext = {
  virployee: {
    id: 'virployee-sofia',
    name: 'Sofia Nexus E2E',
    description: 'Smoke approval flow virployee',
    autonomy: 'A3',
    state: 'active',
    supervisor_user_id: 'dev-user',
  },
  job_role: {
    id: 'job-calendar',
    name: 'Calendar Assistant',
    mission: 'Manage calendar operations',
    responsibilities: [],
    success_criteria: [],
  },
  profile_template: {
    id: 'profile-calendar',
    name: 'Safe Calendar Operator',
    system_prompt: 'You are a safe calendar assistant.',
    max_autonomy: 'A3',
  },
  capabilities: capabilities.map((capability) => ({
    id: capability.id,
    capability_key: capability.capability_key,
    name: capability.name,
    required_autonomy: capability.required_autonomy,
  })),
}
