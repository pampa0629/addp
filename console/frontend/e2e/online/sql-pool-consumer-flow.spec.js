import { expect, request, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, identity, json, login, selectSearchResult } from './transfer-browser-support.js'

const root = resolve(process.cwd(), '../..')
const frameOf = page => page.frameLocator('iframe[data-testid="module-iframe"]')
const previewRoute = sample => `/manager/data-explorer?locator=${encodeURIComponent(sample.locator)}`
const openKey = 'pool_max_open_conns'
const idleKey = 'pool_max_idle_conns'
const responseTimeout = 30_000

function observe(engine) {
  return JSON.parse(execFileSync('python3', ['-m', 'scripts.test.sql-pool-consumer-flow-online', 'observe', '--engine', engine],
    { cwd: root, encoding: 'utf8', timeout: 30_000 }))
}

async function checkIdentity(token, tenantID, permissions, role) {
  const api = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  try {
    const context = await json(await api.get('/api/v1/system/auth/context'), 'pool browser identity')
    const actual = identity(context)
    expect(actual.principalType).toBe('user')
    expect(actual.contextType).toBe('tenant')
    expect(actual.tenantID).toBe(tenantID)
    expect([...actual.permissions].sort()).toEqual(permissions)
    if (role) expect(context.authorization.role_assignments.map(assignment => assignment.role_key)).toEqual([role])
    return actual
  } finally { await api.dispose() }
}

async function managerPreview(page, sample, navigate) {
  const loaded = page.waitForResponse(response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === sample.locator
  }, { timeout: responseTimeout })
  const token = await navigate()
  const data = await json(await loaded, 'Manager pool preview')
  expect(data.preview_type).toBe('table')
  expect(data.data.rows).toEqual([{ id: 7, label: sample.sample }])
  const frame = frameOf(page)
  await expect(frame.getByRole('cell', { name: sample.sample, exact: true })).toBeVisible()
  await expect(frame.getByText('共 1 行', { exact: true })).toBeVisible()
  return token
}

async function developQuery(page, sample, phase, consumer) {
  const engines = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/develop/engines',
    { timeout: responseTimeout })
  await page.goto('/develop/sql?action=create')
  const api = await browserAPIFromResponse(await engines, process.env.GATEWAY_URL, consumer)
  await api.dispose()
  const frame = frameOf(page)
  const selector = frame.locator('.engine-select')
  await expect(selector).not.toBeEmpty()
  if (!(await selector.innerText()).includes(sample.name)) {
    await selector.click()
    await frame.getByRole('option').filter({ hasText: sample.name }).click()
  }
  await expect(selector).toContainText(sample.name)
  await selectSearchResult(frame.locator('.catalog-panel .resource-tree-picker'), sample.table, sample.table)
  const query = `SELECT id, label FROM ${sample.namespace}.${sample.table} WHERE id = 7 /* pool ${phase} */`
  await page.evaluate(value => navigator.clipboard.writeText(value), query)
  await frame.locator('.monaco-editor').click({ position: { x: 70, y: 20 } })
  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.press('ControlOrMeta+V')
  const created = page.waitForResponse(response =>
    new URL(response.url()).pathname === '/api/v1/develop/executions' && response.request().method() === 'POST',
  { timeout: responseTimeout })
  const finished = page.waitForResponse(async response => {
    if (!/^\/api\/v1\/develop\/executions\/[^/]+\/result$/.test(new URL(response.url()).pathname) || !response.ok()) return false
    return ['success', 'failed', 'timeout', 'cancelled'].includes((await response.json()).status)
  }, { timeout: 120_000 })
  await frame.locator('.toolbar-actions').getByRole('button', { name: /^(执行|Execute)$/ }).click()
  const response = await created
  const body = response.request().postDataJSON()
  expect(body.content.query).toBe(query)
  expect(body.execution_config.engine_id).toBe(sample.engine_id)
  const execution = await json(response, 'Develop pool execution submission')
  const terminal = await finished
  expect(new URL(terminal.url()).pathname).toBe(`/api/v1/develop/executions/${execution.execution_id}/result`)
  const result = await json(terminal, 'Develop pool execution result')
  expect(result.status).toBe('success')
  expect(result.metadata.result.summary.preview_rows).toEqual([{ id: 7, label: sample.sample }])
  const table = frame.locator('.query-result .result-table.el-table')
  await expect(table.locator('.el-table__body-wrapper tr')).toHaveCount(1)
  await expect(table.getByRole('cell', { name: sample.sample, exact: true })).toBeVisible()
  return execution.execution_id
}

async function savePool(page, sample, operator) {
  const frame = frameOf(page)
  const path = `/api/v1/system/engines/${sample.engine_id}`
  const row = frame.getByRole('row').filter({ hasText: sample.name })
  const api = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${operator}` } })
  try {
    const before = await json(await api.get(path), 'System pool config before edit')
    expect(before.id).toBe(sample.engine_id)
    expect(before.connection_info).toMatchObject({ [openKey]: 2, [idleKey]: 1 })
    await row.getByRole('button', { name: '编辑', exact: true }).click({ timeout: responseTimeout })
    const dialog = frame.getByRole('dialog', { name: '编辑引擎', exact: true })
    const idle = dialog.locator('.el-form-item').filter({ hasText: '最大空闲连接数' }).getByRole('spinbutton')
    await expect(idle).toHaveValue('1')
    await idle.fill('0')
    await idle.press('Tab')
    const saved = page.waitForResponse(response => new URL(response.url()).pathname === path && response.request().method() === 'PUT',
      { timeout: responseTimeout })
    await dialog.getByRole('button', { name: '保存', exact: true }).click()
    const response = await saved
    const body = response.request().postDataJSON()
    expect(body.version).toBe(before.version)
    expect(body.connection_info).toMatchObject({ [openKey]: 2, [idleKey]: 0 })
    const receipt = await json(response, 'System pool config save')
    expect(receipt.version).toBeGreaterThan(before.version)
    await expect(dialog).toBeHidden()
    const after = await json(await api.get(path), 'System pool config readback')
    expect(after.id).toBe(sample.engine_id)
    expect(after.version).toBe(receipt.version)
    expect(after.connection_info).toMatchObject({ [openKey]: 2, [idleKey]: 0 })
    return {
      before_version: before.version, after_version: after.version,
      max_open: after.connection_info[openKey], idle_before: before.connection_info[idleKey], idle_after: after.connection_info[idleKey],
    }
  } finally { await api.dispose() }
}

function baseline(engine, start) {
  const state = observe(engine)
  const ids = state.statement_ids.filter(id => !start.statement_ids.includes(id))
  expect(ids.some(id => state.active_ids.includes(id))).toBe(true)
  return { state, ids: ids.filter(id => state.active_ids.includes(id)) }
}

async function replacement(engine, start, oldIDs) {
  let proof
  await expect.poll(() => {
    const state = observe(engine)
    const newIDs = state.statement_ids.filter(id => !start.statement_ids.includes(id))
    proof = { old_ids: oldIDs, new_ids: newIDs, active_after: state.active_ids }
    return newIDs.length > 0 && ![...oldIDs, ...newIDs].some(id => state.active_ids.includes(id))
  }, { timeout: 15_000 }).toBe(true)
  return proof
}

test('System pool edit replaces physical sessions in Manager and Develop for both engines', async ({ browser, page }) => {
  test.setTimeout(420_000)
  const expected = JSON.parse(readFileSync(process.env.ADDP_ONLINE_POOL_EXPECTATIONS, 'utf8'))
  const operatorContext = await browser.newContext()
  const operatorPage = await operatorContext.newPage()
  await operatorPage.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const [operator, engineListResponse] = await Promise.all([
    login(operatorPage, process.env.ADDP_ONLINE_POOL_OPERATOR_USERNAME,
      process.env.ADDP_ONLINE_POOL_OPERATOR_PASSWORD, '/system/engines'),
    operatorPage.waitForResponse(response =>
      new URL(response.url()).pathname === '/api/v1/system/engines' && response.request().method() === 'GET',
    { timeout: responseTimeout }),
  ])
  const engineList = await json(engineListResponse, 'System engine management page')
  for (const sample of expected.cases) {
    expect(engineList.some(engine => engine.id === sample.engine_id && engine.name === sample.name)).toBe(true)
    await expect(frameOf(operatorPage).getByRole('row').filter({ hasText: sample.name })).toBeVisible()
  }
  const configIdentity = await checkIdentity(operator, expected.tenant_id, expected.operator_permissions, expected.operator_role)
  expect(configIdentity.principalID).toBe(expected.operator_principal_id)
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])

  const cases = []
  let consumer
  try {
    for (const sample of expected.cases) {
      const engine = sample.engine_type
      const start = observe(engine)
      await test.step(`${engine} Manager baseline`, async () => {
        const token = await managerPreview(page, sample, () => consumer
          ? page.goto(previewRoute(sample))
          : login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME, process.env.ADDP_ONLINE_TEST_USER_PASSWORD, previewRoute(sample)))
        if (!consumer) {
          consumer = await checkIdentity(token, expected.tenant_id, expected.permissions)
          expect(consumer.principalID).toBe(expected.principal_id)
          expect(configIdentity.principalID).not.toBe(consumer.principalID)
        }
      })
      const managerBefore = baseline(engine, start)
      const originalExecution = await test.step(`${engine} Develop baseline`, () => developQuery(page, sample, 'before', consumer))
      const developBefore = baseline(engine, managerBefore.state)
      expect(managerBefore.ids.every(id => developBefore.state.active_ids.includes(id))).toBe(true)

      const version = await test.step(`${engine} System save`, () => savePool(operatorPage, sample, operator))
      const afterSave = observe(engine)
      await test.step(`${engine} Manager replacement`, () => managerPreview(page, sample, () => page.goto(previewRoute(sample))))
      const manager = await replacement(engine, afterSave, managerBefore.ids)
      const beforeSecondQuery = observe(engine)
      const nextExecution = await test.step(`${engine} Develop replacement`, () => developQuery(page, sample, 'after', consumer))
      const develop = await replacement(engine, beforeSecondQuery, developBefore.ids)
      expect(nextExecution).not.toBe(originalExecution)
      cases.push({ engine_id: sample.engine_id, engine_type: engine, ...version, consumers: {
        manager: { ...manager, before_rows: 1, after_rows: 1 },
        develop: { ...develop, before_rows: 1, after_rows: 1, execution_ids: [originalExecution, nextExecution] },
      } })
    }
    writeFileSync(process.env.ADDP_ONLINE_POOL_REPORT, JSON.stringify({
      schema_version: 'addp.sql-pool-console/v1', run_id: expected.run_id, consumer_principal_id: consumer.principalID, operator_principal_id: configIdentity.principalID, cases,
    }))
  } finally {
    if (!operatorPage.isClosed()) await operatorContext.close()
  }
})
