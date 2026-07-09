import { expect, test, type APIRequestContext, type Page } from '@playwright/test'

test.skip(process.env.AXIS_REAL_E2E !== '1', 'real approval flow runs only from make test-console-real-e2e')

const bffURL = process.env.AXIS_REAL_BFF_URL ?? 'http://bff-v2:8080'
const actorID = process.env.DEV_ACTOR_ID ?? 'dev-user'
const actorEmail = process.env.DEV_ACTOR_EMAIL ?? 'dev@example.local'
const orgID = process.env.DEV_ORG_ID ?? 'dev-org'

test('real UI approval flow can approve and return to an approved run history', async ({ page, request }) => {
  const context = await seedApprovalFlowFixture(request)

  await page.addInitScript(({ orgID, productSurface }) => {
    localStorage.setItem('axis.v2.org_id', orgID)
    localStorage.setItem('axis.v2.product_surface', productSurface)
  }, { orgID: context.orgID, productSurface: context.productSurface })

  await page.goto('/')
  await expect(page.getByLabel('Org')).toHaveValue(context.orgID)
  await expect(page.getByLabel('Product')).toHaveValue(context.productSurface)
  await page.getByRole('button', { name: 'Virployees' }).click()
  await page.getByPlaceholder('Search virployees').fill(context.virployeeName)
  await expect(page.getByText(context.virployeeName)).toBeVisible()
  await page.getByRole('checkbox', { name: `Select ${context.virployeeID}` }).check()
  await page.getByRole('button', { name: 'Dry Run' }).click()
  await expect(page.getByRole('heading', { name: 'Dry Run' })).toBeVisible()

  const title = `Real Approval ${context.runID}`
  await page.getByLabel('Action input').fill(`Agenda una reunion "${title}" manana a las 15 con ana@example.com`)
  await page.getByRole('button', { name: 'Run Dry Run' }).first().click()
  await expect(page.getByText('Dry Run result')).toBeVisible()

  await page.getByLabel('Title').fill(title)
  await page.getByLabel('Date').fill('manana')
  await page.getByLabel('Time').fill('15:00')
  await page.getByLabel('Attendees').fill('ana@example.com')
  await expect(page.getByText('Ready to check the gate.')).toBeVisible()
  await page.getByRole('button', { name: 'Check execution gate' }).first().click()

  const checkpointApprovalButton = page
    .getByLabel('Approval checkpoint')
    .getByRole('button', { name: 'Review approval' })
  await expect(checkpointApprovalButton).toBeVisible()
  await checkpointApprovalButton.click()
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

async function seedApprovalFlowFixture(request: APIRequestContext) {
  const session = await devSession(request)
  const tenantID = session.tenantID
  const principalID = session.principalID
  const runID = new Date().toISOString().replace(/\D/g, '').slice(0, 14)

  const readActionID = await ensureActionType(request, tenantID, principalID, {
    action_type_key: 'calendar.events.read',
    name: 'Read calendar events',
    description: 'Real UI approval flow action type',
    category: 'calendar',
    risk_class: 'low',
    enabled: true,
  })
  const createActionID = await ensureActionType(request, tenantID, principalID, {
    action_type_key: 'calendar.events.create',
    name: 'Create calendar events',
    description: 'Real UI approval flow action type',
    category: 'calendar',
    risk_class: 'high',
    enabled: true,
  })
  void readActionID
  void createActionID

  const readCapabilityID = await ensureCapability(request, tenantID, principalID, {
    capability_key: 'calendar.events.read',
    name: 'Read calendar events',
    description: 'Real UI approval flow capability',
    required_autonomy: 'A1',
  })
  const createCapabilityID = await ensureCapability(request, tenantID, principalID, {
    capability_key: 'calendar.events.create',
    name: 'Create calendar events',
    description: 'Real UI approval flow capability',
    required_autonomy: 'A2',
  })

  const jobRole = await api(request, 'POST', '/api/job-roles', tenantID, principalID, {
    name: `Real Approval Role ${runID}`,
    mission: 'Exercise the real approval UI flow',
  })
  const profile = await api(request, 'POST', '/api/profile-templates', tenantID, principalID, {
    name: `Real Approval Profile ${runID}`,
    description: 'Real approval UI flow profile',
    system_prompt: 'You are a real e2e assistant for calendar actions.',
    max_autonomy: 'A3',
  })
  const virployeeName = `Real Approval Virployee ${runID}`
  const virployee = await api(request, 'POST', '/api/virployees', tenantID, principalID, {
    name: virployeeName,
    job_role_id: jobRole.id,
    profile_template_id: profile.id,
    capability_ids: [readCapabilityID, createCapabilityID],
    description: 'Real approval UI flow virployee',
    supervisor_user_id: principalID,
    autonomy: 'A3',
  })

  return {
    runID,
    tenantID,
    orgID: session.orgID,
    productSurface: session.productSurface,
    principalID,
    virployeeID: String(virployee.id),
    virployeeName,
  }
}

async function devSession(request: APIRequestContext): Promise<{
  tenantID: string
  orgID: string
  productSurface: string
  principalID: string
}> {
  const response = await request.get(`${bffURL}/api/session`, {
    headers: {
      'X-Actor-ID': actorID,
      'X-Actor-Email': actorEmail,
      'X-Axis-Org-ID': orgID,
    },
  })
  if (!response.ok()) {
    throw new Error(`GET /api/session: ${await response.text()}`)
  }
  const payload = await response.json()
  const tenant = payload.tenants?.find((item: { org_id?: string }) => item.org_id === payload.org_id) ?? payload.tenants?.[0]
  if (!tenant?.id || !(payload.principal_id || payload.actor_id)) {
    throw new Error(`Could not resolve dev session: ${JSON.stringify(payload)}`)
  }
  return {
    tenantID: tenant.id,
    orgID: tenant.org_id,
    productSurface: tenant.product_surface,
    principalID: payload.principal_id || payload.actor_id,
  }
}

async function ensureActionType(
  request: APIRequestContext,
  tenantID: string,
  principalID: string,
  input: Record<string, unknown> & { action_type_key: string },
): Promise<string> {
  const list = await api(request, 'GET', '/api/action-types', tenantID, principalID)
  const existing = list.data?.find((item: { action_type_key?: string }) => item.action_type_key === input.action_type_key)
  if (existing?.id) {
    const updated = await api(request, 'PUT', `/api/action-types/${existing.id}`, tenantID, principalID, {
      name: input.name,
      description: input.description,
      category: input.category,
      risk_class: input.risk_class,
      enabled: input.enabled,
    })
    return String(updated.id)
  }
  const created = await api(request, 'POST', '/api/action-types', tenantID, principalID, input)
  return String(created.id)
}

async function ensureCapability(
  request: APIRequestContext,
  tenantID: string,
  principalID: string,
  input: Record<string, unknown> & { capability_key: string },
): Promise<string> {
  const list = await api(request, 'GET', '/api/capabilities', tenantID, principalID)
  const existing = list.data?.find((item: { capability_key?: string }) => item.capability_key === input.capability_key)
  if (existing?.id) return String(existing.id)
  const created = await api(request, 'POST', '/api/capabilities', tenantID, principalID, input)
  return String(created.id)
}

async function api(
  request: APIRequestContext,
  method: 'GET' | 'POST' | 'PUT',
  path: string,
  tenantID: string,
  principalID: string,
  body?: Record<string, unknown>,
) {
  const response = await request.fetch(`${bffURL}${path}`, {
    method,
    headers: {
      'X-Tenant-ID': tenantID,
      'X-Actor-ID': principalID,
    },
    data: body,
  })
  if (!response.ok()) {
    throw new Error(`${method} ${path}: ${await response.text()}`)
  }
  return response.json()
}
