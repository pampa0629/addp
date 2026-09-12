import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  controlFixture,
  identity,
  json,
  login,
  selectSearchResult,
  waitForExecution
} from './transfer-browser-support.js'

const requiredNames = [
  'ADDP_ONLINE_REPOSITORY',
  'ADDP_ONLINE_ARTIFACT_DIR',
  'ADDP_ONLINE_TEST_RUN_ID',
  'ADDP_ONLINE_TEST_TENANT_ID',
  'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
  'ADDP_ONLINE_TEST_USER_USERNAME',
  'ADDP_ONLINE_TEST_USER_PASSWORD',
  'ADDP_ONLINE_TEST_ENGINE_NAME',
  'ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME',
  'ADDP_ONLINE_TRANSFER_SQL_ETL_SOURCE_TABLE',
  'ADDP_ONLINE_TRANSFER_SQL_ETL_TARGET_TABLE',
  'GATEWAY_URL'
]

function environment() {
  const missing = requiredNames.filter(name => !process.env[name])
  if (missing.length > 0) throw new Error(`missing Online environment: ${missing.join(', ')}`)
  return Object.fromEntries(requiredNames.map(name => [name, process.env[name]]))
}

async function chooseSelectOption(frame, select, optionText) {
  await select.click()
  const option = frame.locator('.el-select-dropdown__item:visible').filter({ hasText: optionText }).first()
  await expect(option).toBeVisible()
  await option.click()
}

async function selectOutputField(builder, fieldName) {
  const checkbox = builder.getByTestId(`sql-output-field-${fieldName}`).locator('input[type="checkbox"]')
  await checkbox.check({ force: true })
}

test('browser creates and executes PostgreSQL single-table SQL ETL', async ({ page }) => {
  const env = environment()
  const repository = env.ADDP_ONLINE_REPOSITORY
  const expectedStatement = `SELECT "id", "region", "amount" FROM "public"."${env.ADDP_ONLINE_TRANSFER_SQL_ETL_SOURCE_TABLE}" WHERE "status" = :p1 AND "amount" >= :p2`
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
      env.ADDP_ONLINE_TRANSFER_SQL_ETL_SOURCE_TABLE,
      env.ADDP_ONLINE_TEST_ENGINE_NAME
    )

    const queryToggle = frame.getByTestId('task-query-source-toggle').locator('input[type="checkbox"]')
    await queryToggle.check({ force: true })
    await expect(frame.getByTestId('task-query-language-fixed')).toHaveText('SQL')
    await expect(frame.getByTestId('task-query-language-select')).toHaveCount(0)
    const builder = frame.getByTestId('relational-sql-builder')
    await expect(builder).toBeVisible()
    await expect(builder.locator('.el-radio-group')).toHaveCount(0)
    await builder.getByTestId('sql-clear-selected-fields').click()
    await selectOutputField(builder, 'id')
    await selectOutputField(builder, 'region')
    await selectOutputField(builder, 'amount')

    await builder.getByTestId('sql-add-filter').click()
    const statusFilter = builder.getByTestId('sql-filter-0')
    await chooseSelectOption(frame, statusFilter.getByTestId('sql-filter-field'), 'status')
    await statusFilter.getByPlaceholder('输入值').fill('active')

    await builder.getByTestId('sql-add-filter').click()
    const amountFilter = builder.getByTestId('sql-filter-1')
    await chooseSelectOption(frame, amountFilter.getByTestId('sql-filter-field'), 'amount')
    await chooseSelectOption(frame, amountFilter.getByTestId('sql-filter-operator'), '大于等于')
    await amountFilter.getByPlaceholder('输入值').fill('100')

    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '1')
    await chooseSelectOption(frame, frame.getByTestId('task-target-engine'), env.ADDP_ONLINE_TEST_ENGINE_NAME)
    await selectSearchResult(frame.getByTestId('task-target-parent'), 'public', 'public')
    await frame.getByTestId('task-target-table').locator('input').fill(env.ADDP_ONLINE_TRANSFER_SQL_ETL_TARGET_TABLE)
    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '2')

    const mappings = frame.getByTestId('task-field-mappings')
    await expect(mappings.locator('.el-table__row')).toHaveCount(3)
    await expect(mappings).toContainText('id')
    await expect(mappings).toContainText('region')
    await expect(mappings).toContainText('amount')
    await expect(mappings).not.toContainText('status')
    await expect(mappings).not.toContainText('internal_note')
    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '3')

    await frame.getByTestId('task-name').locator('input').fill(env.ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME)
    await expect(frame.getByTestId('task-load-mode').locator('input[value="snapshot"]')).toBeChecked()
    await frame.getByTestId('task-wizard-next').click()
    await expect(wizard).toHaveAttribute('data-step', '4')
    await expect(frame.getByTestId('task-review-step')).toContainText(env.ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME)

    const createResponse = page.waitForResponse(response => {
      const url = new URL(response.url())
      return response.request().method() === 'POST' && url.pathname === '/api/v1/transfer/task-definitions'
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

    const execution = await waitForExecution(api, taskID, 1, 2)
    expect(Number(execution.records_read)).toBe(2)
    const taskPayload = await json(await api.get(`/api/v1/transfer/task-definitions/${taskID}`), 'read Transfer task')
    expect(taskPayload?.config?.source?.query).toEqual({
      language: 'sql',
      statement: expectedStatement,
      parameters: { p1: 'active', p2: '100' }
    })
    const mappingsPayload = taskPayload?.config?.transforms
      ?.find(transform => transform?.type === 'field_mapping')?.fields
    expect(mappingsPayload?.map(mapping => mapping.source)).toEqual(['id', 'region', 'amount'])
    expect(taskPayload?.config?.target?.policy?.apply_mode).toBe('replace')
    controlFixture(repository, 'business/scripts/online-transfer-relational-sql-etl-fixture.sh', 'verify')

    await json(await api.delete(`/api/v1/transfer/task-definitions/${taskID}`), 'delete Transfer task')
    const deleted = await api.get(`/api/v1/transfer/task-definitions/${taskID}`)
    expect(deleted.status()).toBe(404)
    taskDeleted = true

    const unexpectedResponses = failedResponses.filter(response => !(
      taskDeleted && response.status === 404 && response.pathname === `/api/v1/transfer/task-definitions/${taskID}`
    ))
    expect(unexpectedResponses).toEqual([])
    expect(consoleErrors).toEqual([])

    writeFileSync(
      resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'transfer-relational-sql-etl-browser.json'),
      `${JSON.stringify({
        schema_version: 'addp.transfer-relational-sql-etl-browser/v1',
        suite: 'transfer-relational-sql-etl',
        run_id: env.ADDP_ONLINE_TEST_RUN_ID,
        result: 'passed',
        tenant_id: env.ADDP_ONLINE_TEST_TENANT_ID,
        task_name: env.ADDP_ONLINE_TRANSFER_SQL_ETL_TASK_NAME,
        query_language: 'sql',
        language_selector_hidden: true,
        projected_fields: ['id', 'region', 'amount'],
        parameter_count: 2,
        records_read: 2,
        records_written: 2,
        target_row_count: 2,
        task_deleted: true
      })}\n`,
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
