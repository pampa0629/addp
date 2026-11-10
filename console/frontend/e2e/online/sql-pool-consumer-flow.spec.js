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
const readers = ['pool_reader', 'pool_reader_next']
const phases = ['address', 'account', 'password', 'pool']

function observe(engine, reader) {
  return JSON.parse(execFileSync('python3', ['-m', 'scripts.test.sql-pool-consumer-flow-online', 'observe', '--engine', engine, '--reader', reader],
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

function poolQuery(sample, phase) {
  return `SELECT id, label FROM ${sample.namespace}.${sample.table} WHERE id = 7 /* pool ${phase} */`
}

async function developQuery(page, sample, phase, consumer, observeFailure) {
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
  const query = poolQuery(sample, phase)
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
  }, { timeout: observeFailure ? 155_000 : 120_000 })
  await frame.locator('.toolbar-actions').getByRole('button', { name: /^(执行|Execute)$/ }).click()
  const response = await created
  const body = response.request().postDataJSON()
  expect(body.content.query).toBe(query)
  expect(body.execution_config.engine_id).toBe(sample.engine_id)
  const execution = await json(response, 'Develop pool execution submission')
  if (observeFailure) {
    expect(body.timeout).toBe(120)
    return observeFailure(execution.execution_id, finished, frame)
  }
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

async function saveChange(page, sample, operator, phase, credentials) {
  const frame = frameOf(page)
  const path = `/api/v1/system/engines/${sample.engine_id}`
  const row = frame.getByRole('row').filter({ hasText: sample.name })
  const api = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${operator}` } })
  const patch = {
    address: { host: 'localhost' },
    account: { user: readers[1], password: credentials.account },
    password: { password: credentials.rotated },
    pool: { [idleKey]: 0 },
    concurrent: { [idleKey]: 1 },
    saturation: { [openKey]: 1 },
  }[phase]
  try {
    const before = await json(await api.get(path), 'System config before edit')
    expect(before.id).toBe(sample.engine_id)
    expect(before.connection_info).toMatchObject({ database: 'pool_fixture', [openKey]: 2, [idleKey]: phase === 'concurrent' ? 0 : 1 })
    await row.getByRole('button', { name: '编辑', exact: true }).click({ timeout: responseTimeout })
    const dialog = frame.getByRole('dialog', { name: '编辑引擎', exact: true })
    for (const [key, value] of Object.entries(patch)) {
      const label = { host: '主机地址', user: '用户名', password: '密码', [idleKey]: '最大空闲连接数', [openKey]: '最大连接数' }[key]
      const item = dialog.locator('.el-form-item').filter({ hasText: label })
      const input = key === idleKey || key === openKey ? item.getByRole('spinbutton') : item.locator('input')
      if (key === 'password') {
        try { await input.fill(value) } catch { throw new Error('System sensitive input was unavailable') }
      } else {
        await input.fill(String(value))
      }
      await input.press('Tab')
    }
    const saved = page.waitForResponse(response => new URL(response.url()).pathname === path && response.request().method() === 'PUT',
      { timeout: responseTimeout })
    await dialog.getByRole('button', { name: '保存', exact: true }).click()
    if (phase === 'address') {
      const confirmation = frame.locator('.el-message-box').filter({ hasText: '确认引擎地址变更' })
      await expect(confirmation).toBeVisible()
      await confirmation.getByRole('button', { name: '保存', exact: true }).click()
    }
    const response = await saved
    const body = response.request().postDataJSON()
    expect(body.version).toBe(before.version)
    expect(Boolean(body.confirm_same_engine)).toBe(phase === 'address')
    for (const [key, value] of Object.entries(patch)) {
      // A failing assertion must never print a database password.
      expect(body.connection_info[key] === value, `System submitted ${phase} field ${key}`).toBe(true)
    }
    const receipt = await json(response, 'System config save')
    expect(receipt.version).toBeGreaterThan(before.version)
    await expect(dialog).toBeHidden()
    const after = await json(await api.get(path), 'System config readback')
    expect(after.id).toBe(sample.engine_id)
    expect(after.version).toBe(receipt.version)
    expect(after.connection_info.database).toBe('pool_fixture')
    expect(after.connection_info.port).toBe(before.connection_info.port)
    expect(after.connection_info[openKey]).toBe(phase === 'saturation' ? 1 : 2)
    for (const [key, value] of Object.entries(patch)) {
      expect(key === 'password' ? after.connection_info[key] !== value : after.connection_info[key] === value,
        `System readback ${phase} field ${key}`).toBe(true)
    }
    const online = await connectionObservation(page, sample, operator, true, credentials)
    expect(online.engine_version).toBe(after.version)
    return {
      before_version: before.version, after_version: after.version, phase,
      changed_fields: Object.keys(patch).sort(), same_engine_confirmed: phase === 'address',
      host: after.connection_info.host, database: after.connection_info.database,
      reader: after.connection_info.user, max_open: after.connection_info[openKey],
      max_idle: after.connection_info[idleKey], connection_online: true,
    }
  } finally { await api.dispose() }
}

function baseline(engine, reader, start, source) {
  const state = observe(engine, reader)
  expect(state.source_identity).toEqual(source)
  const ids = state.statement_ids.filter(id => !start.statement_ids.includes(id))
  expect(ids.some(id => state.active_ids.includes(id)), JSON.stringify({ engine, reader, start, state })).toBe(true)
  return { state, ids: ids.filter(id => state.active_ids.includes(id)) }
}

async function replacement(engine, reader, start, oldReader, oldIDs, idle, source) {
  let proof
  await expect.poll(() => {
    const state = observe(engine, reader)
    const old = oldReader === reader ? state : observe(engine, oldReader)
    expect(state.source_identity).toEqual(source)
    expect(old.source_identity).toEqual(source)
    const newIDs = state.statement_ids.filter(id => !start.statement_ids.includes(id))
    proof = { old_ids: oldIDs, new_ids: newIDs, old_active_after: old.active_ids,
      active_after: state.active_ids, old_reader: oldReader, reader }
    return newIDs.length > 0 && !oldIDs.some(id => old.active_ids.includes(id))
      && (idle === 0 ? !newIDs.some(id => state.active_ids.includes(id)) : newIDs.every(id => state.active_ids.includes(id)))
  }, { timeout: 15_000 }).toBe(true)
  return proof
}

async function connectionObservation(page, sample, operator, available, credentials) {
  const frame = frameOf(page)
  const path = `/api/v1/system/engines/${sample.engine_id}`
  const api = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${operator}` } })
  try {
    const before = await json(await api.get(path), 'System observation baseline')
    const tested = page.waitForResponse(response => new URL(response.url()).pathname === `${path}/test`
      && response.request().method() === 'POST', { timeout: responseTimeout })
    await frame.getByRole('row').filter({ hasText: sample.name }).getByRole('button', { name: '测试', exact: true }).click()
    const result = await json(await tested, 'System current connection observation')
    expect(result.success).toBe(available)
    for (const secret of Object.values(credentials)) {
      expect(JSON.stringify(result).includes(secret), 'connection observation must not expose credentials').toBe(false)
    }
    const after = await json(await api.get(path), 'System observation readback')
    expect(after.connection_status).toBe(available ? 'online' : 'offline')
    expect(after.version).toBe(before.version)
    return { engine_version: after.version, connection_status: after.connection_status, probe_success: result.success }
  } finally { await api.dispose() }
}

function faultAction(engine, restore) {
  return JSON.parse(execFileSync('python3', ['-m', 'scripts.test.sql-pool-consumer-flow-online',
    restore ? 'restore-password' : 'invalidate-password', '--engine', engine],
  { cwd: root, encoding: 'utf8', timeout: 30_000 }))
}

async function credentialRecovery(page, operatorPage, sample, operator, consumer, credentials, lastChange) {
  const engine = sample.engine_type
  const reader = readers[1]
  const source = sample.source_identity
  const before = observe(engine, reader)
  await managerPreview(page, sample, () => page.goto(previewRoute(sample)))
  const warm = await replacement(engine, reader, before, reader, lastChange.consumers.manager.new_ids, 0, source)
  const beforeFailure = observe(engine, reader)
  const requests = { preview: 0, execution: 0 }
  const watchRequest = outgoing => {
    const path = new URL(outgoing.url()).pathname
    if (path === '/api/v1/manager/preview') requests.preview++
    if (path === '/api/v1/develop/executions' && outgoing.method() === 'POST') requests.execution++
  }
  page.on('request', watchRequest)
  let api
  try {
    const invalidated = faultAction(engine, false)
    expect(invalidated.source_identity).toEqual(source)
    expect(invalidated.configured_password_accepted).toBe(false)
    const offline = await connectionObservation(operatorPage, sample, operator, false, credentials)
    expect(offline.engine_version).toBe(lastChange.after_version)
    const frame = frameOf(page)
    const notice = frame.locator('.engine-unavailable-panel')
    await expect(notice).toBeVisible({ timeout: 45_000 })
    await expect(notice).toContainText('引擎当前不可用')
    await expect(notice).toContainText('离线')
    await expect(notice).toContainText(sample.name)
    await expect(frame.getByRole('cell', { name: sample.sample, exact: true })).toHaveCount(0)
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `${engine}-credentials-manager-offline.png`) })
    const managerUnavailable = { notice_visible: true, stale_rows_visible: false, preview_requests: requests.preview }
    expect(managerUnavailable.preview_requests).toBe(0)

    const listed = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/develop/engines',
      { timeout: responseTimeout })
    await page.goto('/develop/sql?action=create')
    const listing = await listed
    const descriptors = await json(listing, 'Develop offline selection')
    expect(descriptors.find(item => item.id === sample.engine_id)?.connection_status).toBe('offline')
    api = await browserAPIFromResponse(listing, process.env.GATEWAY_URL, consumer)
    const readCheck = await api.post('/api/v1/system/engine-access/read-checks/manager-preview', { data: { targets: [sample.read_target] } })
    expect(readCheck.status()).toBe(200)
    const selector = frameOf(page).locator('.engine-select')
    await selector.click()
    const option = frameOf(page).getByRole('option').filter({ hasText: sample.name })
    await expect(option).toContainText('离线')
    await expect(option).toHaveAttribute('aria-disabled', 'true')
    expect(requests.execution).toBe(0)
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `${engine}-credentials-develop-offline.png`) })
    const developUnavailable = { engine_id: sample.engine_id, connection_status: 'offline', disabled: true,
      submitted_executions: requests.execution, source_read_status: readCheck.status() }
    const failed = observe(engine, reader)
    expect(failed.source_identity).toEqual(source)
    const failedReads = failed.statement_ids.filter(id => !beforeFailure.statement_ids.includes(id))
    expect(failedReads).toEqual([])
    page.off('request', watchRequest)

    const restored = faultAction(engine, true)
    expect(restored.source_identity).toEqual(source)
    expect(restored.configured_password_accepted).toBe(true)
    const online = await connectionObservation(operatorPage, sample, operator, true, credentials)
    expect(online.engine_version).toBe(offline.engine_version)
    const restoredRead = await api.post('/api/v1/system/engine-access/read-checks/manager-preview', { data: { targets: [sample.read_target] } })
    expect(restoredRead.status()).toBe(200)
    const restoredStart = observe(engine, reader)
    await managerPreview(page, sample, () => page.goto(previewRoute(sample)))
    const manager = await replacement(engine, reader, restoredStart, reader, warm.new_ids, 0, source)
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `${engine}-credentials-manager-restored.png`) })
    const beforeQuery = observe(engine, reader)
    const execution = await developQuery(page, sample, 'restored', consumer)
    const develop = await replacement(engine, reader, beforeQuery, reader, lastChange.consumers.develop.new_ids, 0, source)
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `${engine}-credentials-develop-restored.png`) })
    return { source_identity: source, invalidated, offline, manager_unavailable: managerUnavailable,
      develop_unavailable: developUnavailable, failed_read_ids: failedReads, restored, online,
      restored_read_status: restoredRead.status(), consumers: {
        manager: { before_ids: warm.new_ids, new_ids: manager.new_ids, active_after: manager.active_after, rows: 1 },
        develop: { new_ids: develop.new_ids, active_after: develop.active_after, rows: 1,
          previous_execution_id: lastChange.consumers.develop.execution_ids[1], execution_id: execution },
      } }
  } finally {
    page.off('request', watchRequest)
    if (api) await api.dispose()
  }
}

function barrierAction(engine, operation, purpose = 'concurrency') {
  return JSON.parse(execFileSync('python3', ['-m', 'scripts.test.sql-pool-consumer-flow-online',
    'barrier', '--engine', engine, '--barrier-operation', operation, '--barrier-purpose', purpose],
  { cwd: root, encoding: 'utf8', timeout: 30_000 }))
}

async function concurrentUpdate(page, operatorPage, sample, operator, consumer, credentials) {
  const engine = sample.engine_type
  const before = observe(engine, readers[1])
  const secondPage = await page.context().newPage()
  const pending = []
  let released, oldBlocked, bothBlocked
  const acquired = barrierAction(engine, 'acquire')
  try {
    pending.push(developQuery(page, sample, 'concurrent_old', consumer))
    // Retain failures for the final await while the native barrier is inspected.
    pending.at(-1).catch(() => {})
    await expect.poll(() => {
      oldBlocked = barrierAction(engine, 'observe')
      return oldBlocked.held && oldBlocked.waiters.length === 1 && oldBlocked.waiters[0].phase === 'old'
    }, { timeout: 15_000 }).toBe(true)
    const version = await saveChange(operatorPage, sample, operator, 'concurrent', credentials)
    pending.push(developQuery(secondPage, sample, 'concurrent_new', consumer))
    pending.at(-1).catch(() => {})
    await expect.poll(() => {
      bothBlocked = barrierAction(engine, 'observe')
      return bothBlocked.held && bothBlocked.waiters.length === 2
        && ['new', 'old'].every(phase => bothBlocked.waiters.some(waiter => waiter.phase === phase))
        && bothBlocked.waiters.some(waiter => waiter.id === oldBlocked.waiters[0].id && waiter.phase === 'old')
    }, { timeout: 15_000 }).toBe(true)
    released = barrierAction(engine, 'release')
    expect(released.held).toBe(false)
    const [oldExecution, newExecution] = await Promise.all(pending)
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `${engine}-concurrent-old-completed.png`) })
    await secondPage.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `${engine}-concurrent-new-completed.png`) })
    const oldID = oldBlocked.waiters[0].id
    const newID = bothBlocked.waiters.find(waiter => waiter.phase === 'new').id
    const queryState = observe(engine, readers[1])
    expect(queryState.active_ids).not.toContain(oldID)
    expect(queryState.active_ids).toContain(newID)
    expect(queryState.source_identity).toEqual(sample.source_identity)
    await managerPreview(page, sample, () => page.goto(previewRoute(sample)))
    const manager = await replacement(engine, readers[1], queryState, readers[1], before.statement_ids, 1, sample.source_identity)
    expect(manager.new_ids).not.toContain(oldID)
    expect(manager.new_ids).not.toContain(newID)
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `${engine}-concurrent-manager-completed.png`) })
    return { acquired, old_blocked: oldBlocked, both_blocked: bothBlocked, released, configuration: version,
      source_identity: sample.source_identity, old_execution_id: oldExecution, new_execution_id: newExecution,
      old_id: oldID, new_id: newID, query_active_after: queryState.active_ids,
      manager_new_ids: manager.new_ids, manager_active_after: manager.active_after, rows: { old: 1, new: 1, manager: 1 } }
  } finally {
    if (!released) barrierAction(engine, 'release')
    await Promise.allSettled(pending)
    await secondPage.close()
  }
}

async function waitForNativeReaders(engine, purpose, phases) {
  let snapshot
  await expect.poll(() => {
    snapshot = barrierAction(engine, 'observe', purpose)
    return snapshot.held ? snapshot.waiters.map(waiter => waiter.phase).sort() : []
  }, { timeout: 15_000 }).toEqual(phases)
  return snapshot
}

async function developTimeout(page, sample, phase, consumer, observeNative) {
  return developQuery(page, sample, phase, consumer, async (id, terminal, frame) => {
    const started = Date.now()
    const during = await observeNative()
    const response = await terminal
    expect(new URL(response.url()).pathname).toBe(`/api/v1/develop/executions/${id}/result`)
    const result = await json(response, 'Develop timeout result')
    expect(['timeout', 'failed']).toContain(result.status)
    const diagnostic = result.error_details?.diagnostic
    expect(diagnostic?.stage).toBe('execution')
    expect(diagnostic.reason).toMatch(/^[a-z_]+$/)
    const panel = frame.locator('.query-result .execution-failure')
    await expect(panel).toBeVisible()
    await expect(panel).toContainText(id)
    await expect(frame.locator('.query-result .result-table')).toHaveCount(0)
    return { execution_id: id, status: result.status, reason: diagnostic.reason,
      elapsed_ms: Date.now() - started, during, heading: await panel.locator('h4').innerText() }
  })
}

async function managerTimeout(page, sample) {
  let started
  const request = page.waitForRequest(req => {
    const url = new URL(req.url())
    if (url.pathname !== '/api/v1/manager/preview' || url.searchParams.get('locator') !== sample.locator) return false
    started = Date.now()
    return true
  }, { timeout: responseTimeout })
  await page.goto(previewRoute(sample))
  const submitted = await request
  const frame = frameOf(page)
  const panel = frame.getByTestId('preview-failure')
  await expect(panel).toBeVisible({ timeout: 75_000 })
  await expect(frame.getByRole('cell', { name: sample.sample, exact: true })).toHaveCount(0)
  const proof = { elapsed_ms: Date.now() - started, http_status: (await submitted.response())?.status() ?? null,
    heading: await panel.locator('.el-empty__description').innerText() }
  await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `${sample.engine_type}-saturation-manager.png`) })
  return proof
}

async function saturationProbe(page, operatorPage, sample, operator, consumer, credentials) {
  const engine = sample.engine_type
  const configuration = await saveChange(operatorPage, sample, operator, 'saturation', credentials)
  const managerPage = await page.context().newPage()
  const pending = []
  let api, queue, holders, queueTerminal, queued, manager, native, released
  try {
    // The existing fixture API token belongs to the same ordinary user and
    // outlives the intentional waits. Page assertions use browser login.
    const fixtureToken = process.env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN
    const fixtureIdentity = await checkIdentity(fixtureToken, consumer.tenantID, [...consumer.permissions].sort())
    expect(fixtureIdentity.principalID).toBe(consumer.principalID)
    api = await request.newContext({ baseURL: process.env.GATEWAY_URL, extraHTTPHeaders: { Authorization: `Bearer ${fixtureToken}` } })
    queue = barrierAction(engine, 'acquire', 'queue')
    const previewHolder = api.get('/api/v1/manager/preview', {
      params: { locator: sample.locator, page: 1, page_size: 1 }, timeout: 200_000,
    })
    pending.push(previewHolder)
    previewHolder.catch(() => {})
    const queryHolder = await json(await api.post('/api/v1/develop/executions', { data: {
      dev_type: 'query', trigger_type: 'manual', timeout: 200,
      content: { query: poolQuery(sample, 'saturation_holder'), query_type: 'sql', target_locator: sample.locator },
      execution_config: { engine_id: sample.engine_id },
    } }), 'Develop saturation holder')
    holders = await waitForNativeReaders(engine, 'queue', ['holder', 'manager'])
    ;[manager, queued] = await Promise.all([
      managerTimeout(managerPage, sample),
      developTimeout(page, sample, 'saturation_queued', consumer, () => waitForNativeReaders(engine, 'queue', ['holder', 'manager'])),
    ])
    expect(queued.execution_id).not.toBe(queryHolder.execution_id)
    queueTerminal = await waitForNativeReaders(engine, 'queue', ['holder', 'manager'])
    expect(queueTerminal.waiters).toEqual(holders.waiters)
    await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `${engine}-saturation-develop.png`) })
    const descriptors = await json(await api.get('/api/v1/develop/engines'), 'saturated engine status')
    const engineStatus = descriptors.find(item => item.id === sample.engine_id)?.connection_status
    expect(engineStatus).toBe('online')
    released = barrierAction(engine, 'release', 'queue')
    const holderPreview = await json(await previewHolder, 'Manager saturation holder recovered')
    expect(holderPreview.data.rows).toEqual([{ id: 7, label: sample.sample }])
    await expect.poll(async () => {
      const result = await json(await api.get(`/api/v1/develop/executions/${queryHolder.execution_id}/result`), 'Develop saturation holder recovered')
      return result.status
    }, { timeout: 20_000 }).toBe('success')

    barrierAction(engine, 'acquire', 'native')
    try {
      native = await developTimeout(page, sample, 'saturation_native', consumer, () => waitForNativeReaders(engine, 'native', ['native']))
      await page.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `${engine}-saturation-native.png`) })
    } finally {
      barrierAction(engine, 'release', 'native')
    }
    const recoveredExecution = await developQuery(page, sample, 'saturation_recovered', consumer)
    await managerPreview(managerPage, sample, () => managerPage.goto(previewRoute(sample)))
    const proof = { configuration, queue, holders, queue_terminal: queueTerminal, released, queued, manager, native, engine_status: engineStatus,
      holder_execution_id: queryHolder.execution_id, recovered_execution_id: recoveredExecution, recovered_manager_rows: 1 }
    for (const secret of Object.values(credentials)) {
      expect(JSON.stringify(proof).includes(secret), 'pool diagnostic must not expose credentials').toBe(false)
    }
    return proof
  } finally {
    try {
      if (queue && !released) barrierAction(engine, 'release', 'queue')
    } finally {
      if (api) await api.dispose()
      await Promise.allSettled(pending)
      await managerPage.close()
    }
  }
}

test('System pool edit replaces physical sessions in Manager and Develop for both engines', async ({ browser, page }) => {
  test.setTimeout(810_000)
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
      const credentials = JSON.parse(readFileSync(resolve(process.env.ADDP_ONLINE_POOL_PRIVATE, `${engine}-credentials.json`), 'utf8'))
      let reader = readers[0]
      const source = sample.source_identity
      const start = observe(engine, reader)
      expect(start.source_identity).toEqual(source)
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
      let managerBefore = baseline(engine, reader, start, source)
      let originalExecution = await test.step(`${engine} Develop baseline`, () => developQuery(page, sample, 'initial', consumer))
      let developBefore = baseline(engine, reader, managerBefore.state, source)
      const changes = []
      for (const phase of phases) {
        const nextReader = phase === 'address' ? readers[0] : readers[1]
        let passwordProof
        if (phase === 'password') {
          passwordProof = JSON.parse(execFileSync('python3', ['-m', 'scripts.test.sql-pool-consumer-flow-online',
            'rotate-password', '--engine', engine], { cwd: root, encoding: 'utf8', timeout: 30_000 }))
          expect(passwordProof.old_password_rejected).toBe(true)
          expect(passwordProof.new_password_accepted).toBe(true)
          expect(passwordProof.source_identity).toEqual(source)
        }
        const version = await test.step(`${engine} System ${phase} save`, () => saveChange(operatorPage, sample, operator, phase, credentials))
        const afterSave = observe(engine, nextReader)
        await test.step(`${engine} Manager ${phase}`, () => managerPreview(page, sample, () => page.goto(previewRoute(sample))))
        const manager = await replacement(engine, nextReader, afterSave, reader, managerBefore.ids, version.max_idle, source)
        const beforeSecondQuery = observe(engine, nextReader)
        const nextExecution = await test.step(`${engine} Develop ${phase}`, () => developQuery(page, sample, phase, consumer))
        const develop = await replacement(engine, nextReader, beforeSecondQuery, reader, developBefore.ids, version.max_idle, source)
        expect(nextExecution).not.toBe(originalExecution)
        changes.push({ ...version, ...(passwordProof ? { password_proof: passwordProof } : {}), consumers: {
          manager: { ...manager, before_rows: 1, after_rows: 1 },
          develop: { ...develop, before_rows: 1, after_rows: 1, execution_ids: [originalExecution, nextExecution] },
        } })
        managerBefore = { ids: manager.new_ids }
        developBefore = { ids: develop.new_ids }
        reader = nextReader
        originalExecution = nextExecution
      }
      const recovery = await test.step(`${engine} credential failure and recovery`, () =>
        credentialRecovery(page, operatorPage, sample, operator, consumer, credentials, changes.at(-1)))
      const concurrency = await test.step(`${engine} active query during System pool update`, () =>
        concurrentUpdate(page, operatorPage, sample, operator, consumer, credentials))
      const saturation = await test.step(`${engine} pool wait and native SQL timeout`, () =>
        saturationProbe(page, operatorPage, sample, operator, consumer, credentials))
      cases.push({ engine_id: sample.engine_id, engine_type: engine, source_identity: source, changes, recovery, concurrency, saturation })
    }
    writeFileSync(process.env.ADDP_ONLINE_POOL_REPORT, JSON.stringify({
      schema_version: 'addp.sql-pool-console/v5', run_id: expected.run_id, consumer_principal_id: consumer.principalID, operator_principal_id: configIdentity.principalID, cases,
    }))
  } finally {
    if (!operatorPage.isClosed()) await operatorContext.close()
  }
})
