import { expect, request, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

const requiredNames = [
  'ADDP_ONLINE_REPOSITORY',
  'ADDP_ONLINE_ARTIFACT_DIR',
  'ADDP_ONLINE_TEST_RUN_ID',
  'ADDP_ONLINE_TEST_TENANT_ID',
  'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
  'ADDP_ONLINE_TEST_USER_USERNAME',
  'ADDP_ONLINE_TEST_USER_PASSWORD',
  'ADDP_ONLINE_TEST_ENGINE_ID',
  'ADDP_ONLINE_TEST_ENGINE_NAME',
  'GATEWAY_URL'
]

function environment() {
  const missing = requiredNames.filter(name => !process.env[name])
  if (missing.length > 0) throw new Error(`missing Online environment: ${missing.join(', ')}`)
  return Object.fromEntries(requiredNames.map(name => [name, process.env[name]]))
}

function identity(payload) {
  const assignments = payload?.authorization?.role_assignments
  if (!Array.isArray(assignments)) throw new Error('AuthContext role_assignments must be an array')
  return {
    principalType: payload?.principal?.type,
    principalID: String(payload?.principal?.id || ''),
    contextType: payload?.context?.type,
    tenantID: String(payload?.context?.tenant_id || ''),
    permissions: new Set(assignments.flatMap(assignment => assignment.permissions || []))
  }
}

async function json(response, operation) {
  const payload = await response.json()
  if (!response.ok()) {
    throw new Error(`${operation} returned HTTP ${response.status()} (${payload?.error_code || 'unknown'})`)
  }
  return payload
}

async function engineStatus(api, engineID) {
  const response = await api.get(`/api/v1/system/engines/${engineID}`)
  return json(response, 'read Engine Instance')
}

async function testEngineConnection(api, engineID, expectedStatus) {
  await json(
    await api.post(`/api/v1/system/engines/${engineID}/test`),
    'test Engine Instance connection'
  )
  await expect.poll(async () => (await engineStatus(api, engineID)).connection_status, {
    timeout: 60_000,
    intervals: [500, 1000, 2000]
  }).toBe(expectedStatus)
}

function controlFixture(repository, action) {
  execFileSync(
    'bash',
    ['business/scripts/online-engine-fixture.sh', action],
    { cwd: repository, env: process.env, stdio: 'inherit' }
  )
}

async function login(page, username, password) {
  let browserAccessToken = ''
  page.on('request', requestEvent => {
    if (!requestEvent.url().endsWith('/api/v1/system/auth/context')) return
    browserAccessToken = (requestEvent.headers().authorization || '').replace(/^Bearer\s+/i, '')
  })

  await page.goto('/login?redirect=/configuration')
  await page.locator('input[autocomplete="username"]').fill(username)
  await page.locator('input[autocomplete="current-password"]').fill(password)
  await page.locator('button.auth-login-primary').click()

  const contextStep = page.locator('.auth-login-contexts')
  const needsContext = await contextStep.waitFor({ state: 'visible', timeout: 5000 })
    .then(() => true)
    .catch(() => false)
  if (needsContext) {
    await contextStep.locator('button.auth-login-primary').click()
  }
  if (await page.locator('input[autocomplete="one-time-code"]').isVisible().catch(() => false)) {
    throw new Error('the dedicated Online browser user must not require MFA')
  }

  await page.waitForURL(url => url.pathname === '/configuration')
  await expect.poll(() => browserAccessToken, { timeout: 20_000 }).not.toBe('')
  return browserAccessToken
}

test('consumer pages and Engine status recover without consumer restart or browser refresh', async ({ page }) => {
  const env = environment()
  const repository = env.ADDP_ONLINE_REPOSITORY
  const engineID = Number(env.ADDP_ONLINE_TEST_ENGINE_ID)
  if (!Number.isInteger(engineID) || engineID <= 0) throw new Error('ADDP_ONLINE_TEST_ENGINE_ID must be positive')

  const api = await request.newContext({
    baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN}` }
  })
  const failedBusinessResponses = []
  page.on('response', response => {
    const pathname = new URL(response.url()).pathname
    if (pathname.startsWith('/api/v1/') && response.status() >= 400) {
      failedBusinessResponses.push({ pathname, status: response.status() })
    }
  })

  let fixtureStopped = false
  try {
    const apiIdentity = identity(await json(
      await api.get('/api/v1/system/auth/context'),
      'read API AuthContext'
    ))
    expect(apiIdentity.principalType).toBe('user')
    expect(apiIdentity.contextType).toBe('tenant')
    expect(apiIdentity.tenantID).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    for (const permission of [
      'system.engine.read',
      'system.engine.execute',
      'manager.data_item.read',
      'service.definition.read'
    ]) {
      expect(apiIdentity.permissions.has(permission), `missing permission ${permission}`).toBe(true)
    }

    const fixtureEngine = await engineStatus(api, engineID)
    expect(fixtureEngine.name).toBe(env.ADDP_ONLINE_TEST_ENGINE_NAME)
    expect(fixtureEngine.engine_type).toBe('postgresql')
    expect(fixtureEngine.lifecycle_state).toBe('active')
    await testEngineConnection(api, engineID, 'online')

    const browserAccessToken = await login(
      page,
      env.ADDP_ONLINE_TEST_USER_USERNAME,
      env.ADDP_ONLINE_TEST_USER_PASSWORD
    )
    const browserAPI = await request.newContext({
      baseURL: env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${browserAccessToken}` }
    })
    const browserIdentity = identity(await json(
      await browserAPI.get('/api/v1/system/auth/context'),
      'read browser AuthContext'
    ))
    expect(browserIdentity.principalID).toBe(apiIdentity.principalID)
    expect(browserIdentity.tenantID).toBe(apiIdentity.tenantID)
    await browserAPI.dispose()

    const configuration = page.getByTestId('configuration-management')
    await expect(configuration).toHaveAttribute('data-load-state', 'loaded')

    await page.goto('/manager/data-explorer')
    const managerFrameLocator = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(managerFrameLocator.getByTestId('data-explorer')).toHaveAttribute(
      'data-engine-load-state',
      'loaded'
    )
    const engineNode = managerFrameLocator.locator(
      `[data-testid="engine-node"][data-engine-id="${engineID}"]`
    )
    await expect(engineNode).toHaveAttribute('data-connection-status', 'online')
    const managerFrame = page.frames().find(frame => frame.url().includes('/data-explorer'))
    expect(managerFrame).toBeTruthy()

    await page.goto('/service/query-services')
    const serviceFrameLocator = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(serviceFrameLocator.getByTestId('query-service-list')).toHaveAttribute(
      'data-load-state',
      'loaded'
    )

    await page.goto('/manager/data-explorer')
    const recoveryFrameLocator = page.frameLocator('iframe[data-testid="module-iframe"]')
    const recoveryNode = recoveryFrameLocator.locator(
      `[data-testid="engine-node"][data-engine-id="${engineID}"]`
    )
    await expect(recoveryNode).toHaveAttribute('data-connection-status', 'online')
    const recoveryFrame = page.frames().find(frame => frame.url().includes('/data-explorer'))
    expect(recoveryFrame).toBeTruthy()

    controlFixture(repository, 'stop')
    fixtureStopped = true
    await testEngineConnection(api, engineID, 'offline')
    await expect(recoveryNode).toHaveAttribute('data-connection-status', 'offline', { timeout: 45_000 })
    expect(page.frames()).toContain(recoveryFrame)

    controlFixture(repository, 'start')
    fixtureStopped = false
    await testEngineConnection(api, engineID, 'online')
    await expect(recoveryNode).toHaveAttribute('data-connection-status', 'online', { timeout: 45_000 })
    expect(page.frames()).toContain(recoveryFrame)

    const unexpectedResponses = failedBusinessResponses.filter(response => response.status !== 401)
    expect(unexpectedResponses).toEqual([])

    const report = {
      schema_version: 'addp.consumer-engine-recovery/v1',
      suite: 'consumer-engine-recovery',
      run_id: env.ADDP_ONLINE_TEST_RUN_ID,
      result: 'passed',
      tenant_id: env.ADDP_ONLINE_TEST_TENANT_ID,
      engine_id: engineID,
      engine_name: env.ADDP_ONLINE_TEST_ENGINE_NAME,
      first_access_pages: ['configuration', 'manager/data-explorer', 'service/query-services'],
      observed_connection_states: ['online', 'offline', 'online'],
      same_manager_frame_recovered: true,
      consumer_processes_restarted: 0,
      final_connection_status: 'online'
    }
    writeFileSync(
      resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'consumer-engine-recovery-browser.json'),
      `${JSON.stringify(report)}\n`,
      'utf8'
    )
  } finally {
    if (fixtureStopped) controlFixture(repository, 'start')
    await testEngineConnection(api, engineID, 'online')
    await api.dispose()
  }
})
