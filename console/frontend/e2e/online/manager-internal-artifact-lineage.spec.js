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
  'ADDP_ONLINE_MANAGER_LINEAGE_EXECUTION_ID',
  'ADDP_ONLINE_MANAGER_LINEAGE_ITEM_ID',
  'ADDP_ONLINE_MANAGER_LINEAGE_SOURCE_NAME',
  'ADDP_ONLINE_MANAGER_LINEAGE_OUTPUT_NAME',
  'GATEWAY_URL'
]

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
  const expectedRedirect = new URL(redirect, 'http://addp.invalid')
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
  await page.waitForURL(url => (
    url.pathname === expectedRedirect.pathname &&
    url.searchParams.get('execution_id') === expectedRedirect.searchParams.get('execution_id')
  ))
  await expect.poll(() => browserAccessToken, { timeout: 20_000 }).not.toBe('')
  return browserAccessToken
}

test('Monitor renders one business input and one platform-internal output from Manager lineage facts', async ({ page }) => {
  const env = environment()
  const itemID = Number(env.ADDP_ONLINE_MANAGER_LINEAGE_ITEM_ID)
  if (!Number.isInteger(itemID) || itemID <= 0) throw new Error('Manager lineage item ID must be positive')
  const executionPath = `/monitor/executions?execution_id=${encodeURIComponent(env.ADDP_ONLINE_MANAGER_LINEAGE_EXECUTION_ID)}`
  const api = await request.newContext({
    baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN}` }
  })
  const browserMessages = []
  const failedBusinessResponses = []
  page.on('console', message => {
    if (['warning', 'error'].includes(message.type())) {
      browserMessages.push({ type: message.type(), text: message.text() })
    }
  })
  page.on('pageerror', error => browserMessages.push({ type: 'pageerror', text: error.message }))
  page.on('response', response => {
    const pathname = new URL(response.url()).pathname
    if (pathname.startsWith('/api/v1/') && response.status() >= 400) {
      failedBusinessResponses.push({ pathname, status: response.status() })
    }
  })

  try {
    const apiIdentity = await json(await api.get('/api/v1/system/auth/context'), 'read API AuthContext')
    expect(apiIdentity?.principal?.type).toBe('user')
    expect(String(apiIdentity?.context?.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)

    const browserAccessToken = await login(
      page,
      env.ADDP_ONLINE_TEST_USER_USERNAME,
      env.ADDP_ONLINE_TEST_USER_PASSWORD,
      executionPath
    )
    const browserAPI = await request.newContext({
      baseURL: env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${browserAccessToken}` }
    })
    const browserIdentity = await json(await browserAPI.get('/api/v1/system/auth/context'), 'read browser AuthContext')
    expect(String(browserIdentity?.principal?.id)).toBe(String(apiIdentity?.principal?.id))
    expect(String(browserIdentity?.context?.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    await browserAPI.dispose()

    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const lineage = frame.locator('.execution-lineage')
    await expect(lineage).toBeVisible()
    const groups = lineage.locator('.execution-lineage__group')
    await expect(groups).toHaveCount(2)
    const inputCards = groups.nth(0).locator('.execution-lineage__card')
    const outputCards = groups.nth(1).locator('.execution-lineage__card')
    await expect(inputCards).toHaveCount(1)
    await expect(outputCards).toHaveCount(1)
    await expect(inputCards.first()).toContainText(env.ADDP_ONLINE_MANAGER_LINEAGE_SOURCE_NAME)
    await expect(inputCards.first()).toContainText(String(itemID))
    await expect(inputCards.first().locator('.execution-lineage__resource-action')).toHaveCount(1)
    await expect(outputCards.first()).toContainText(env.ADDP_ONLINE_MANAGER_LINEAGE_OUTPUT_NAME)
    await expect(outputCards.first()).toContainText(/平台内部产物|Platform-internal artifact/)
    await expect(outputCards.first().locator('.execution-lineage__resource-action')).toHaveCount(0)
    expect(failedBusinessResponses).toEqual([])
    expect(browserMessages).toEqual([])

    const report = {
      schema_version: 'addp.manager-internal-artifact-lineage-browser/v1',
      suite: 'manager-internal-artifact-lineage',
      run_id: env.ADDP_ONLINE_TEST_RUN_ID,
      result: 'passed',
      execution_id: env.ADDP_ONLINE_MANAGER_LINEAGE_EXECUTION_ID,
      item_id: itemID,
      output_name: env.ADDP_ONLINE_MANAGER_LINEAGE_OUTPUT_NAME,
      input_resources: 1,
      output_resources: 1,
      platform_internal_outputs: 1,
      browser_warning_errors: 0
    }
    writeFileSync(
      resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'manager-internal-artifact-lineage-browser.json'),
      `${JSON.stringify(report)}\n`,
      'utf8'
    )
  } finally {
    await api.dispose()
  }
})
