import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

const requiredNames = [
  'ADDP_ONLINE_ARTIFACT_DIR',
  'ADDP_ONLINE_TEST_RUN_ID',
  'ADDP_ONLINE_TEST_TENANT_ID',
  'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
  'ADDP_ONLINE_TEST_USER_USERNAME',
  'ADDP_ONLINE_TEST_USER_PASSWORD',
  'ADDP_ONLINE_WORKBENCH_SERVICE_ID',
  'ADDP_ONLINE_WORKBENCH_APPLICATION_ID',
  'ADDP_ONLINE_WORKBENCH_ORIGINAL_FINGERPRINT',
  'GATEWAY_URL'
]

const fields = [
  'order_no', 'customer_code', 'city', 'membership_level', 'status',
  'total_amount', 'payment_method', 'ordered_at', 'shipped_at', 'active_customer'
]
const filterableFields = ['ordered_at', 'status', 'city', 'membership_level', 'active_customer']

function environment() {
  const missing = requiredNames.filter(name => !process.env[name])
  if (missing.length > 0) throw new Error(`missing Online environment: ${missing.join(', ')}`)
  return Object.fromEntries(requiredNames.map(name => [name, process.env[name]]))
}

async function json(response, operation) {
  const payload = await response.json()
  if (!response.ok()) {
    throw new Error(`${operation} returned HTTP ${response.status()} (${payload?.error_code || 'unknown'})`)
  }
  return payload
}

async function login(page, username, password, redirect) {
  let browserAccessToken = ''
  page.on('request', requestEvent => {
    if (!requestEvent.url().endsWith('/api/v1/system/auth/context')) return
    browserAccessToken = (requestEvent.headers().authorization || '').replace(/^Bearer\s+/i, '')
  })
  await page.goto(`/login?redirect=${encodeURIComponent(redirect)}`)
  await page.locator('input[autocomplete="username"]').fill(username)
  await page.locator('input[autocomplete="current-password"]').fill(password)
  await page.locator('button.auth-login-primary').click()
  const contextStep = page.locator('.auth-login-contexts')
  const needsContext = await contextStep.waitFor({ state: 'visible', timeout: 5000 })
    .then(() => true)
    .catch(() => false)
  if (needsContext) await contextStep.locator('button.auth-login-primary').click()
  if (await page.locator('input[autocomplete="one-time-code"]').isVisible().catch(() => false)) {
    throw new Error('the dedicated Online browser user must not require MFA')
  }
  await page.waitForURL(url => url.pathname === redirect)
  await expect.poll(() => browserAccessToken, { timeout: 20_000 }).not.toBe('')
  return browserAccessToken
}

test('Data Application authoring renders MySQL table and chart then blocks a changed contract', async ({ page }) => {
  const env = environment()
  const serviceID = Number(env.ADDP_ONLINE_WORKBENCH_SERVICE_ID)
  if (!Number.isInteger(serviceID) || serviceID <= 0) throw new Error('Workbench service ID must be positive')
  const applicationPath = `/workbench/applications/${env.ADDP_ONLINE_WORKBENCH_APPLICATION_ID}`
  const api = await request.newContext({
    baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN}` }
  })
  const failedBusinessResponses = []
  page.on('response', response => {
    const pathname = new URL(response.url()).pathname
    if ((pathname.startsWith('/api/v1/') || pathname.startsWith('/api/query/')) && response.status() >= 400) {
      failedBusinessResponses.push({ pathname, status: response.status() })
    }
  })

  try {
    const identity = await json(await api.get('/api/v1/system/auth/context'), 'read AuthContext')
    expect(identity?.principal?.type).toBe('user')
    expect(String(identity?.context?.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)

    const browserAccessToken = await login(
      page,
      env.ADDP_ONLINE_TEST_USER_USERNAME,
      env.ADDP_ONLINE_TEST_USER_PASSWORD,
      applicationPath
    )
    const browserAPI = await request.newContext({
      baseURL: env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${browserAccessToken}` }
    })
    const browserIdentity = await json(
      await browserAPI.get('/api/v1/system/auth/context'),
      'read browser AuthContext'
    )
    expect(String(browserIdentity?.principal?.id)).toBe(String(identity?.principal?.id))
    expect(String(browserIdentity?.context?.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    await browserAPI.dispose()
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(frame.getByTestId('data-application-editor')).toBeVisible()
    await expect(frame.getByTestId('contract-changed-alert')).toHaveCount(0)

    const applicationComponents = frame.getByTestId('application-component')
    await expect(applicationComponents).toHaveCount(2)
    await applicationComponents.nth(0).getByTestId('edit-component-action').click()
    const componentEditor = frame.getByTestId('application-component-editor')
    await expect(componentEditor).toBeVisible()
    await componentEditor.getByTestId('component-query-action').click()
    const tableRows = componentEditor.getByTestId('renderer-host').locator('.el-table__body-wrapper tbody tr')
    await expect(tableRows).toHaveCount(2)
    await expect(tableRows.first()).toContainText('ORD-20260420-001')
    await frame.locator('.el-dialog__headerbtn').click()

    await applicationComponents.nth(1).getByTestId('edit-component-action').click()
    await expect(componentEditor).toBeVisible()
    await componentEditor.getByTestId('component-query-action').click()
    await expect(componentEditor.getByTestId('renderer-host').locator('.chart-renderer canvas')).toBeVisible()
    await expect(componentEditor.getByTestId('renderer-host').locator('.map-container')).toHaveCount(0)
    await frame.locator('.el-dialog__headerbtn').click()

    await json(
      await api.put(`/api/v1/service/query/${serviceID}`, {
        data: { data_config: { default_fields: fields.slice(0, -1), filterable_fields: filterableFields } }
      }),
      'change Query Service public contract'
    )
    const changed = await json(
      await api.get(`/api/v1/service/consumer/services/query/${serviceID}`),
      'read changed Consumer Descriptor'
    )
    expect(changed.contract_fingerprint).not.toBe(env.ADDP_ONLINE_WORKBENCH_ORIGINAL_FINGERPRINT)

    await page.reload()
    await expect(frame.getByTestId('data-application-editor')).toBeVisible()
    await applicationComponents.nth(0).getByTestId('edit-component-action').click()
    await expect(componentEditor.getByTestId('contract-changed-alert')).toBeVisible()
    await expect(componentEditor.getByTestId('component-query-action')).toBeDisabled()
    expect(failedBusinessResponses).toEqual([])

    const report = {
      schema_version: 'addp.workbench-service-consumption-browser/v1',
      suite: 'workbench-service-consumption',
      run_id: env.ADDP_ONLINE_TEST_RUN_ID,
      result: 'passed',
      tenant_id: env.ADDP_ONLINE_TEST_TENANT_ID,
      service_id: serviceID,
      application_id: env.ADDP_ONLINE_WORKBENCH_APPLICATION_ID,
      table_rows: 2,
      chart_rendered: true,
      map_available: false,
      contract_change_blocked: true
    }
    writeFileSync(
      resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'workbench-service-consumption-browser.json'),
      `${JSON.stringify(report)}\n`,
      'utf8'
    )
  } finally {
    await api.dispose()
  }
})
