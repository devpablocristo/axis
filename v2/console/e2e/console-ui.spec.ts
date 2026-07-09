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

const approvals = [{
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

test.beforeEach(async ({ page }) => {
  await installApiFixtures(page)
})

test('all main sections render with coherent action buttons', async ({ page }) => {
  await page.goto('/')
  await expect(page.locator('.topbar h1')).toHaveText('Virployees')

  const nav = page.locator('.nav')
  for (const section of ['Virployees', 'Job Roles', 'Capabilities', 'Profile Templates', 'Approvals', 'Admin']) {
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
  for (const section of ['Virployees', 'Job Roles', 'Capabilities', 'Profile Templates']) {
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
  for (const section of ['Virployees', 'Job Roles', 'Capabilities', 'Profile Templates']) {
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

async function installApiFixtures(page: Page) {
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/session') return json(route, session)
    if (path === '/api/virployees/autonomy-levels') return json(route, { data: autonomyLevels })
    if (path === '/api/virployees') return json(route, { data: virployees })
    if (path === '/api/virployees/archived' || path === '/api/virployees/trash') return json(route, { data: [] })
    if (path === '/api/virployees/virployee-sofia/runtime-context') return json(route, runtimeContext)
    if (path === '/api/virployees/virployee-sofia/runs') return json(route, { data: [] })
    if (path === '/api/job-roles') return json(route, { data: jobRoles })
    if (path === '/api/capabilities') return json(route, { data: capabilities })
    if (path === '/api/profile-templates') return json(route, { data: profileTemplates })
    if (path === '/api/users') return json(route, { data: users })
    if (path === '/api/tenants') return json(route, { data: session.tenants })
    if (path === '/api/orgs') return json(route, { data: orgs })
    if (path === '/api/products') return json(route, { data: products })
    if (path === '/api/approvals') {
      const status = url.searchParams.get('status')
      return json(route, { data: status === 'pending' ? approvals : [] })
    }
    return json(route, { data: [] })
  })
}

async function json(route: Route, body: unknown) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body),
  })
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
