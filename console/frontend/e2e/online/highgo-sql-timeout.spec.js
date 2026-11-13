import { expect, request, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { appendFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, identity, json, login, selectSearchResult } from './transfer-browser-support.js'

const root = resolve(process.cwd(), '../..')
const frameOf = page => page.frameLocator('iframe[data-testid="module-iframe"]')
const rows = [{ id: 1, engine_name: 'HighGo V4.5 Business' }]
const terminal = new Set(['success', 'failed', 'timeout', 'cancelled'])
const query = 'SELECT id, engine_name FROM addp_business.addp_engine_probe WHERE id = 1'

function artifact(name) {
  return resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, name)
}

function barrier(operation, phase) {
  const result = JSON.parse(execFileSync('python3', ['business/scripts/online-highgo-consumer-fixture.py',
    'barrier', operation, phase], { cwd: root, encoding: 'utf8', timeout: 25_000 }))
  appendFileSync(artifact('highgo-timeout-native.jsonl'), JSON.stringify(result) + '\n')
  return result
}

async function preview(page, expected) {
  const response = page.waitForResponse(res => new URL(res.url()).pathname === '/api/v1/manager/preview'
    && new URL(res.url()).searchParams.get('locator') === expected.locator)
  response.catch(() => {})
  await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(expected.locator)}`)
  const payload = await json(await response, 'HighGo normal preview')
  expect(payload.preview_type).toBe('table')
  expect(payload.data.rows).toEqual(rows)
  expect(payload.data.total).toBe(1)
  const frame = frameOf(page)
  await expect(frame.getByRole('cell', { name: 'HighGo V4.5 Business', exact: true })).toBeVisible()
  await expect(frame.getByText('显示 2 / 2 个字段', { exact: true })).toBeVisible()
  await expect(frame.getByTestId('preview-failure')).toHaveCount(0)
  return payload.data.rows
}

async function openQuery(page, expected) {
  const engines = page.waitForResponse(res => new URL(res.url()).pathname === '/api/v1/develop/engines')
  engines.catch(() => {})
  await page.goto('/develop/sql?action=create')
  const api = await browserAPIFromResponse(await engines, process.env.GATEWAY_URL, {
    principalID: expected.principal_id, tenantID: expected.tenant_id, permissions: new Set(expected.permissions)
  })
  await api.dispose()
  const frame = frameOf(page)
  const selector = frame.locator('.engine-select')
  await expect(selector).not.toBeEmpty()
  if (!(await selector.innerText()).includes(expected.engine_name)) {
    await selector.click()
    await frame.getByRole('option').filter({ hasText: expected.engine_name }).click()
  }
  await selectSearchResult(frame.locator('.catalog-panel .resource-tree-picker'), 'addp_engine_probe', 'addp_engine_probe')
  await page.evaluate(value => navigator.clipboard.writeText(value), query)
  await frame.locator('.monaco-editor').click({ position: { x: 70, y: 20 } })
  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.press('ControlOrMeta+V')
  return frame
}

async function executeQuery(page, expected, during) {
  const frame = frameOf(page)
  const created = page.waitForResponse(res => new URL(res.url()).pathname === '/api/v1/develop/executions'
    && res.request().method() === 'POST')
  const finished = page.waitForResponse(async res => {
    if (!/^\/api\/v1\/develop\/executions\/[^/]+\/result$/.test(new URL(res.url()).pathname) || !res.ok()) return false
    return terminal.has((await res.json()).status)
  }, { timeout: during ? 155_000 : 45_000 })
  created.catch(() => {})
  finished.catch(() => {})
  const started = Date.now()
  await frame.locator('.toolbar-actions').getByRole('button', { name: /^(执行|Execute)$/ }).click()
  const submission = await created
  const body = submission.request().postDataJSON()
  expect(body.execution_config.engine_id).toBe(expected.engine_id)
  expect(body.content.query).toBe(query)
  expect(body.content.target_locator).toBe(expected.locator)
  expect(body.timeout).toBe(120)
  const execution = await json(submission, 'HighGo query submission')
  const native = during ? await during() : undefined
  const response = await finished
  expect(new URL(response.url()).pathname).toBe(`/api/v1/develop/executions/${execution.execution_id}/result`)
  return { frame, execution_id: execution.execution_id, native,
    elapsed_ms: Date.now() - started, result: await json(response, 'HighGo query result') }
}

async function timeoutPhase(page, expected, phase) {
  if (phase === 'develop') await openQuery(page, expected)
  const before = barrier('acquire', phase)
  let released
  try {
    let during
    const observeWaiter = async () => {
      await expect.poll(() => {
        during = barrier('observe', phase)
        return during.held && during.holder_id === before.holder_id && during.waiters.length === 1
      }, { timeout: 15_000 }).toBe(true)
      return during
    }
    let elapsed, code, heading, executionID = null
    if (phase === 'manager') {
      const response = page.waitForResponse(res => new URL(res.url()).pathname === '/api/v1/manager/preview'
        && new URL(res.url()).searchParams.get('locator') === expected.locator, { timeout: 75_000 })
      response.catch(() => {})
      const started = Date.now()
      await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(expected.locator)}`)
      await observeWaiter()
      const completed = await response
      elapsed = Date.now() - started
      expect(completed.status()).toBe(504)
      code = (await completed.json()).error_code
      expect(code).toBe('preview_sql_execution_timeout')
      const frame = frameOf(page)
      const panel = frame.getByTestId('preview-failure')
      await expect(panel).toBeVisible()
      heading = await panel.locator('.el-empty__description').innerText()
      await expect(frame.getByRole('cell', { name: 'HighGo V4.5 Business', exact: true })).toHaveCount(0)
      await expect(frame.locator('.el-message--error')).toHaveCount(0)
    } else {
      const completed = await executeQuery(page, expected, observeWaiter)
      expect(['failed', 'timeout']).toContain(completed.result.status)
      const diagnostic = completed.result.error_details?.diagnostic
      expect(diagnostic?.stage).toBe('execution')
      code = diagnostic?.reason
      expect(code).toBe('sql_execution_timeout')
      executionID = completed.execution_id
      elapsed = completed.elapsed_ms
      const panel = completed.frame.locator('.query-result .execution-failure')
      await expect(panel).toBeVisible()
      heading = await panel.locator('h4').innerText()
      await expect(panel).toContainText(executionID)
      await expect(completed.frame.locator('.query-result .result-table')).toHaveCount(0)
    }
    expect(heading).toBe('数据库 SQL 执行超时')
    let after
    await expect.poll(() => {
      after = barrier('observe', phase)
      return after.held && after.holder_id === before.holder_id && after.waiters.length === 0
    }, { timeout: 10_000 }).toBe(true)
    await page.screenshot({ path: artifact(`highgo-${phase}-sql-timeout.png`) })
    released = barrier('release', phase)
    expect(released.held).toBe(false)
    expect(released.waiters).toEqual([])
    let recoveryRows, recoveryExecutionID = null
    if (phase === 'manager') {
      recoveryRows = await preview(page, expected)
    } else {
      const recovered = await executeQuery(page, expected)
      expect(recovered.result.status).toBe('success')
      recoveryRows = recovered.result.metadata.result.summary.preview_rows
      expect(recoveryRows).toEqual(rows)
      recoveryExecutionID = recovered.execution_id
      await expect(recovered.frame.getByRole('cell', { name: 'HighGo V4.5 Business', exact: true })).toBeVisible()
      await expect(recovered.frame.locator('.query-result .execution-failure')).toHaveCount(0)
    }
    await page.screenshot({ path: artifact(`highgo-${phase}-sql-recovery.png`) })
    return { before, during, after_timeout: after, released, elapsed_ms: elapsed, code, heading,
      recovery_rows: recoveryRows, execution_id: executionID, recovery_execution_id: recoveryExecutionID }
  } finally {
    if (!released) barrier('release', phase)
  }
}

test('HighGo native SQL timeouts render safely and both consumers recover after release', async ({ page }) => {
  test.setTimeout(330_000)
  const expected = JSON.parse(process.env.ADDP_ONLINE_HIGHGO_TIMEOUT_EXPECTATIONS)
  expect(process.env.ADDP_ONLINE_HIGHGO_CASE).toBe('sql-timeout')
  expect(process.env.ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN).toBeUndefined()
  expect(process.env.ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN).toBeUndefined()
  const api = await request.newContext({ baseURL: process.env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${process.env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN}` } })
  const current = identity(await json(await api.get('/api/v1/system/auth/context'), 'timeout consumer identity'))
  await api.dispose()
  expect(current.principalType).toBe('user')
  expect(current.principalID).toBe(expected.principal_id)
  expect(current.tenantID).toBe(expected.tenant_id)
  expect([...current.permissions].sort()).toEqual(expected.permissions)
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME, process.env.ADDP_ONLINE_TEST_USER_PASSWORD,
    `/manager/data-explorer?locator=${encodeURIComponent(expected.locator)}`)
  await preview(page, expected)
  const manager = await timeoutPhase(page, expected, 'manager')
  const develop = await timeoutPhase(page, expected, 'develop')
  writeFileSync(process.env.ADDP_ONLINE_HIGHGO_TIMEOUT_BROWSER_REPORT, JSON.stringify({
    schema_version: expected.schema_version, case: expected.case, run_id: expected.run_id,
    tenant_id: expected.tenant_id, principal_id: current.principalID, engine_id: expected.engine_id,
    locator: expected.locator, permissions: expected.permissions, manager, develop
  }))
})
