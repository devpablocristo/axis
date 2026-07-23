const { chromium } = require('@playwright/test')
const fs = require('fs/promises')

const baseURL = process.env.PLAYWRIGHT_BASE_URL || 'http://console-v2:19173'
const reportPath = process.env.REAL_UI_CHECK_REPORT || '/app/test-results/real-ui-check.txt'
const sections = ['Virployees', 'Capabilities', 'Job Roles', 'Profile Templates']

async function main() {
  const browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: 1366, height: 768 } })
  const lines = []
  const failures = []
  const diagnostics = []

  page.on('console', (message) => {
    diagnostics.push(`console.${message.type()}: ${message.text()}`)
  })
  page.on('pageerror', (error) => {
    diagnostics.push(`pageerror: ${error.message}`)
  })

  await page.goto(baseURL, { waitUntil: 'networkidle' })
  const nav = page.locator('.nav')
  try {
    await nav.waitFor({ state: 'visible', timeout: 5000 })
  } catch (error) {
    await page.screenshot({ path: '/app/test-results/real-ui-check-entry.png', fullPage: true })
    const bodyText = await page.locator('body').innerText().catch(() => '')
    const report = [
      'Navigation not visible on real Console.',
      `body: ${bodyText.slice(0, 1000) || '(empty)'}`,
      ...diagnostics,
    ].join('\n')
    await fs.mkdir('/app/test-results', { recursive: true })
    await fs.writeFile(reportPath, `${report}\n`, 'utf8')
    console.error(report)
    throw error
  }

  for (const section of sections) {
    await nav.getByRole('button', { name: section }).click()
    await page.waitForTimeout(300)
    await auditCurrentTable(page, section, lines, failures)
  }

  await nav.getByRole('button', { name: 'Organization' }).click()
  await page.waitForTimeout(300)
  const tabs = page.locator('.organization-admin-section__tabs')
  for (const tab of ['Users', 'Organizations', 'Orgs', 'Products']) {
    await tabs.getByRole('tab', { name: tab }).click()
    await page.waitForTimeout(300)
    await auditCurrentTable(page, `Admin/${tab}`, lines, failures)
  }

  await browser.close()
  const report = lines.join('\n')
  await fs.mkdir('/app/test-results', { recursive: true })
  await fs.writeFile(reportPath, `${report}\n`, 'utf8')
  console.log(report)
  if (failures.length > 0) {
    console.error(failures.join('\n'))
    process.exit(1)
  }
}

async function auditCurrentTable(page, label, lines, failures) {
  const createdCount = await page.locator('th').filter({ hasText: 'Created' }).count()
  const rows = await page.locator('.axis-crud-host tbody tr').count()
  let sticky = 'not-tested-no-rows'

  if (rows > 0) {
    const wrap = page.locator('.axis-crud-host .table-wrap').first()
    const row = page.locator('.axis-crud-host tbody tr').first()
    const checkboxCell = row.locator('td').nth(0)
    const primaryCell = row.locator('td').nth(1)
    const beforeCheckbox = await checkboxCell.boundingBox()
    const beforePrimary = await primaryCell.boundingBox()

    await wrap.evaluate((element) => {
      element.scrollLeft = 900
    })
    await page.waitForTimeout(100)

    const afterCheckbox = await checkboxCell.boundingBox()
    const afterPrimary = await primaryCell.boundingBox()
    sticky = beforeCheckbox && afterCheckbox && beforePrimary && afterPrimary
      && Math.abs(beforeCheckbox.x - afterCheckbox.x) <= 1
      && Math.abs(beforePrimary.x - afterPrimary.x) <= 1
      ? 'ok'
      : 'failed'
  }

  lines.push(`${label}: Created=${createdCount}, rows=${rows}, sticky=${sticky}`)
  if (createdCount === 0) failures.push(`${label}: missing Created column`)
  if (sticky === 'failed') failures.push(`${label}: selection/primary columns are not sticky`)
}

main().catch((error) => {
  console.error(error)
  process.exit(1)
})
