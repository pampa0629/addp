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
  'ADDP_ONLINE_TEST_ENGINE_NAME',
  'ADDP_ONLINE_TEST_ENGINE_DATABASE',
  'ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_NAME',
  'ADDP_ONLINE_TRANSFER_MYSQL_DATABASE',
  'ADDP_ONLINE_TRANSFER_TASK_NAME',
  'ADDP_ONLINE_TRANSFER_SOURCE_TABLE',
  'ADDP_ONLINE_TRANSFER_TARGET_TABLE',
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

function identity(payload) {
  const assignments = payload?.authorization?.role_assignments
  if (!Array.isArray(assignments)) throw new Error('AuthContext role_assignments must be an array')
  return {
    principalID: String(payload?.principal?.id || ''),
    principalType: payload?.principal?.type,
    tenantID: String(payload?.context?.tenant_id || ''),
    contextType: payload?.context?.type,
    permissions: new Set(assignments.flatMap(assignment => assignment.permissions || []))
  }
}

async function login(page, username, password) {
  let browserAccessToken = ''
  page.on('request', requestEvent => {
    if (!requestEvent.url().endsWith('/api/v1/system/auth/context')) return
    browserAccessToken = (requestEvent.headers().authorization || '').replace(/^Bearer\s+/i, '')
  })

  await page.goto('/login?redirect=/transfer/tasks/create')
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
  await page.waitForURL(url => url.pathname === '/transfer/tasks/create')
  await expect.poll(() => browserAccessToken, { timeout: 20_000 }).not.toBe('')
  return browserAccessToken
}

function controlFixture(repository, action) {
  execFileSync(
    'bash',
    ['business/scripts/online-transfer-insert-only-fixture.sh', action],
    { cwd: repository, env: process.env, stdio: 'inherit' }
  )
}

function executionItems(payload) {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.data)) return payload.data
  if (Array.isArray(payload?.items)) return payload.items
  throw new Error('Transfer task executions must be an array')
}

async function waitForExecution(api, taskID, expectedCount, expectedWritten) {
  let matched = null
  await expect.poll(async () => {
    const payload = await json(
      await api.get(`/api/v1/transfer/task-definitions/${taskID}/executions?page=1&page_size=10`),
      'list Transfer task executions'
    )
    const items = executionItems(payload)
    if (items.length < expectedCount) return `count:${items.length}`
    const latest = items[0]
    if (['failed', 'cancelled', 'timeout'].includes(latest?.status)) {
      throw new Error(`Transfer execution ended with status ${latest.status}`)
    }
    if (latest?.status !== 'success') return `status:${latest?.status || 'missing'}`
    matched = latest
    return Number(latest.records_written)
  }, { timeout: 120_000, intervals: [500, 1000, 2000, 5000] }).toBe(expectedWritten)
  return matched
}

async function selectSearchResult(picker, keyword, expectedText) {
  await picker.locator('.picker-search input').fill(keyword)
  const result = picker.locator('button.search-result').filter({ hasText: expectedText }).first()
  await expect(result).toBeVisible()
  await result.click()
}

test('browser recommends MySQL decimal precision and preserves insert-only watermark semantics', async ({ page }) => {
  const env = environment()
  const repository = env.ADDP_ONLINE_REPOSITORY
  const api = await request.newContext({
    baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN}` }
  })
  const failedResponses = []
  const consoleErrors = []
  page.on('response', response => {
    const pathname = new URL(response.url()).pathname
    if (pathname.startsWith('/api/v1/') && response.status() >= 400) {
      failedResponses.push({ pathname, status: response.status() })
    }
  })
  page.on('console', message => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })

  let taskID = 0
  let taskDeleted = false
  try {
    const apiIdentity = identity(await json(await api.get('/api/v1/system/auth/context'), 'read API AuthContext'))
    expect(apiIdentity.principalType).toBe('user')
    expect(apiIdentity.contextType).toBe('tenant')
    expect(apiIdentity.tenantID).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    for (const permission of [
      'meta.catalog.read',
      'system.engine.execute',
      'system.engine.read',
      'transfer.task.create',
      'transfer.task.delete',
      'transfer.task.execute',
      'transfer.task.read'
    ]) {
      expect(apiIdentity.permissions.has(permission), `missing permission ${permission}`).toBe(true)
    }

    const browserAccessToken = await login(page, env.ADDP_ONLINE_TEST_USER_USERNAME, env.ADDP_ONLINE_TEST_USER_PASSWORD)
    const browserAPI = await request.newContext({
      baseURL: env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${browserAccessToken}` }
    })
    const browserIdentity = identity(await json(await browserAPI.get('/api/v1/system/auth/context'), 'read browser AuthContext'))
    expect(browserIdentity.principalID).toBe(apiIdentity.principalID)
    expect(browserIdentity.tenantID).toBe(apiIdentity.tenantID)
    await browserAPI.dispose()

    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const wizard = frame.getByTestId('transfer-task-wizard')
    await expect(wizard).toHaveAttribute('data-step', '0')
    await selectSearchResult(
      frame.getByTestId('task-source-picker'),
      env.ADDP_ONLINE_TRANSFER_SOURCE_TABLE,
      env.ADDP_ONLINE_TEST_ENGINE_NAME
    )
    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '1')

    const targetEngine = frame.getByTestId('task-target-engine')
    await targetEngine.click()
    const targetOption = frame.locator('.el-select-dropdown__item')
      .filter({ hasText: env.ADDP_ONLINE_TRANSFER_MYSQL_ENGINE_NAME })
      .first()
    await expect(targetOption).toBeVisible()
    await targetOption.click()
    await selectSearchResult(
      frame.getByTestId('task-target-parent'),
      env.ADDP_ONLINE_TRANSFER_MYSQL_DATABASE,
      env.ADDP_ONLINE_TRANSFER_MYSQL_DATABASE
    )
    await frame.getByTestId('task-target-table').locator('input').fill(env.ADDP_ONLINE_TRANSFER_TARGET_TABLE)
    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '2')

    const mappings = frame.getByTestId('task-field-mappings')
    const amountRow = mappings.locator('.el-table__row').filter({ hasText: 'amount' }).first()
    await expect(amountRow).toBeVisible()
    const amountNumbers = amountRow.locator('.decimal-number-input input')
    await expect(amountNumbers).toHaveCount(2)
    await expect(amountNumbers.nth(0)).toHaveValue('')
    await expect(amountNumbers.nth(1)).toHaveValue('')
    const recommendationResponse = page.waitForResponse(response => {
      const url = new URL(response.url())
      return response.request().method() === 'POST' &&
        url.pathname === '/api/v1/transfer/field-definition-recommendations'
    })
    await frame.getByTestId('task-recommend-decimal-definitions').click()
    expect((await recommendationResponse).status()).toBe(200)
    await expect(amountNumbers.nth(0)).toHaveValue('6')
    await expect(amountNumbers.nth(1)).toHaveValue('2')
    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '3')

    await frame.getByTestId('task-name').locator('input').fill(env.ADDP_ONLINE_TRANSFER_TASK_NAME)
    await frame.getByTestId('task-load-mode').locator('input[value="insert_only"]').check({ force: true })
    await expect(frame.getByTestId('task-watermark-field').locator('input')).toHaveValue(/id/)
    await expect(frame.getByTestId('task-watermark-target-keys')).toHaveText('id')
    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '4')
    await expect(frame.getByTestId('task-review-step')).toContainText(env.ADDP_ONLINE_TRANSFER_TASK_NAME)

    const createResponse = page.waitForResponse(response => {
      const url = new URL(response.url())
      return response.request().method() === 'POST' &&
        url.pathname === '/api/v1/transfer/task-definitions'
    })
    await frame.getByTestId('task-submit').click()
    const confirmDialog = frame.getByRole('dialog')
    await expect(confirmDialog).toBeVisible()
    await confirmDialog.locator('.el-button--primary').click()
    const createdResponse = await createResponse
    expect(createdResponse.status()).toBe(201)
    const created = await createdResponse.json()
    taskID = Number(created?.id)
    expect(Number.isInteger(taskID) && taskID > 0).toBe(true)

    const initial = await waitForExecution(api, taskID, 1, 6)
    expect(Number(initial.records_read)).toBe(6)
    await page.goto(`/transfer/tasks/${taskID}/detail`)
    const detailFrame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(detailFrame.getByTestId('transfer-task-detail')).toBeVisible()
    await expect(detailFrame.getByTestId('task-executions').locator('.el-table__row').first()).toContainText('6')

    controlFixture(repository, 'advance')
    const startResponse = page.waitForResponse(response => {
      const url = new URL(response.url())
      return response.request().method() === 'POST' &&
        url.pathname === `/api/v1/transfer/task-definitions/${taskID}/start`
    })
    await detailFrame.getByTestId('task-execute').click()
    expect((await startResponse).status()).toBe(200)
    const incremental = await waitForExecution(api, taskID, 2, 1)
    expect(Number(incremental.records_read)).toBe(1)
    await expect.poll(async () => {
      await page.reload()
      const row = page.frameLocator('iframe[data-testid="module-iframe"]')
        .getByTestId('task-executions').locator('.el-table__row').first()
      return (await row.innerText()).includes('1')
    }, { timeout: 30_000, intervals: [1000, 2000] }).toBe(true)

    const task = await json(await api.get(`/api/v1/transfer/task-definitions/${taskID}`), 'read Transfer task')
    expect(task?.config?.load?.mode).toBe('incremental')
    expect(task?.config?.load?.change_detection?.field).toBe('id')
    expect(task?.config?.load?.change_detection?.tie_breaker).toEqual([])
    expect(task?.config?.target?.policy?.apply_mode).toBe('upsert')
    expect(task?.config?.target?.policy?.keys).toEqual(['id'])
    const amountMapping = task?.config?.transforms
      ?.find(transform => transform?.type === 'field_mapping')
      ?.fields?.find(field => field?.source === 'amount')
    expect(amountMapping?.precision).toBe(6)
    expect(amountMapping?.scale).toBe(2)
    controlFixture(repository, 'verify')

    await json(await api.delete(`/api/v1/transfer/task-definitions/${taskID}`), 'delete Transfer task')
    const deleted = await api.get(`/api/v1/transfer/task-definitions/${taskID}`)
    expect(deleted.status()).toBe(404)
    taskDeleted = true

    const unexpectedResponses = failedResponses.filter(response => !(
      taskDeleted && response.status === 404 && response.pathname === `/api/v1/transfer/task-definitions/${taskID}`
    ))
    expect(unexpectedResponses).toEqual([])
    expect(consoleErrors).toEqual([])

    const report = {
      schema_version: 'addp.transfer-insert-only-mysql-browser/v1',
      suite: 'transfer-insert-only-mysql',
      run_id: env.ADDP_ONLINE_TEST_RUN_ID,
      result: 'passed',
      tenant_id: env.ADDP_ONLINE_TEST_TENANT_ID,
      task_name: env.ADDP_ONLINE_TRANSFER_TASK_NAME,
      initial_records_written: 6,
      incremental_records_written: 1,
      target_row_count: 7,
      old_update_ignored: true,
      decimal_precision: 6,
      decimal_scale: 2,
      task_deleted: true
    }
    writeFileSync(
      resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'transfer-insert-only-mysql-browser.json'),
      `${JSON.stringify(report)}\n`,
      'utf8'
    )
  } finally {
    if (taskID > 0 && !taskDeleted) {
      const response = await api.delete(`/api/v1/transfer/task-definitions/${taskID}`)
      if (![200, 404].includes(response.status())) {
        throw new Error(`cleanup Transfer task returned HTTP ${response.status()}`)
      }
    }
    await api.dispose()
  }
})
