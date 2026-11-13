import { expect, request } from '@playwright/test'
import { execFileSync } from 'node:child_process'

export async function checkLabeledCheckbox(checkbox) {
  if (!await checkbox.isChecked()) {
    await checkbox.locator('xpath=ancestor::label[1]').click()
  }
  await expect(checkbox).toBeChecked()
}

export async function json(response, operation) {
  const payload = await response.json()
  if (!response.ok()) {
    throw new Error(`${operation} returned HTTP ${response.status()} (${payload?.error_code || 'unknown'})`)
  }
  return payload
}

export function identity(payload) {
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

export async function browserAPIFromResponse(response, gateway, expected) {
  expect(response.ok()).toBe(true)
  const authorization = response.request().headers().authorization
  expect(typeof authorization === 'string' && /^Bearer\s+\S+$/.test(authorization)).toBe(true)
  const api = await request.newContext({ baseURL: gateway, extraHTTPHeaders: { Authorization: authorization } })
  try {
    let contextResponse
    try {
      contextResponse = await api.get('/api/v1/system/auth/context')
    } catch {
      throw new Error('current browser session identity request failed')
    }
    const actual = identity(await json(contextResponse, 'current browser session identity'))
    expect(actual.principalType).toBe('user')
    expect(actual.contextType).toBe('tenant')
    expect(actual.principalID).toBe(expected.principalID)
    expect(actual.tenantID).toBe(expected.tenantID)
    expect([...actual.permissions].sort()).toEqual([...expected.permissions].sort())
    return api
  } catch (error) {
    await api.dispose()
    throw error
  }
}

export async function browserAPIFromNavigation(page, gateway, expected, navigate, matchesResponse) {
  const observed = page.waitForResponse(matchesResponse)
  // A failed navigation must not leave a later response timeout unhandled.
  observed.catch(() => {})
  await navigate()
  return browserAPIFromResponse(await observed, gateway, expected)
}

export function matchesRedirectURL(actual, expected) {
  if (actual.pathname !== expected.pathname) return false
  const actualQuery = new URLSearchParams(actual.search)
  const expectedQuery = new URLSearchParams(expected.search)
  actualQuery.sort()
  expectedQuery.sort()
  return actualQuery.toString() === expectedQuery.toString()
}

export function isAnonymousRefreshConsoleError(message, businessStarted) {
  if (businessStarted || message.type() !== 'error' ||
      message.text() !== 'Failed to load resource: the server responded with a status of 401 (Unauthorized)') return false
  try {
    return new URL(message.location().url).pathname === '/api/v1/system/refresh'
  } catch {
    return false
  }
}

export function isAnonymousRefreshResponse(response, businessStarted) {
  return !businessStarted && response.status() === 401 &&
    response.request().method() === 'POST' &&
    new URL(response.url()).pathname === '/api/v1/system/refresh'
}

export async function login(page, username, password, redirect) {
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
  let landing = 'pending'
  await expect.poll(async () => {
    if (await page.locator('input[autocomplete="one-time-code"]').isVisible()) landing = 'mfa'
    else if (matchesRedirectURL(new URL(page.url()), expectedRedirect)) landing = 'redirect'
    else if (await contextStep.isVisible()) landing = 'context'
    return landing
  }, { timeout: 20_000 }).not.toBe('pending')
  if (landing === 'context') await contextStep.locator('button.auth-login-primary').click()
  if (await page.locator('input[autocomplete="one-time-code"]').isVisible().catch(() => false)) {
    throw new Error('the dedicated Online browser user must not require MFA')
  }
  await page.waitForURL(url => matchesRedirectURL(url, expectedRedirect))
  await expect.poll(() => browserAccessToken, { timeout: 20_000 }).not.toBe('')
  return browserAccessToken
}

export function controlFixture(repository, script, action) {
  execFileSync('bash', [script, action], { cwd: repository, env: process.env, stdio: 'inherit' })
}

export function executionItems(payload) {
  if (Array.isArray(payload)) return payload
  if (Array.isArray(payload?.data)) return payload.data
  if (Array.isArray(payload?.items)) return payload.items
  throw new Error('Transfer task executions must be an array')
}

export async function waitForExecution(api, taskID, expectedCount, expectedWritten) {
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

export async function selectSearchResult(picker, keyword, expectedText) {
  await picker.locator('.picker-search input').fill(keyword)
  const result = picker.locator('button.search-result').filter({ hasText: expectedText }).first()
  await expect(result).toBeVisible()
  await result.click()
}

export function isReadonlyTaskConfigCheckRequest(method, path) {
  if (method === 'POST') return ['/api/v1/transfer/task-config-checks', '/api/v1/system/refresh'].includes(path)
  if (method !== 'GET') return false
  return path.startsWith('/api/v1/system/') || path === '/api/v1/transfer/capabilities' ||
    /^\/api\/v1\/transfer\/task-definitions(?:\/statistics|\/\d+(?:\/executions)?)?$/.test(path) ||
    path === '/api/v1/meta/engines' || /^\/api\/v1\/meta\/resource-tree\/\d+(?:\/(?:node|ancestors|search))?$/.test(path) ||
    /^\/api\/v1\/meta\/items\/\d+(?:\/fields)?$/.test(path)
}

export async function assertCDCTaskMonitoring(page, frame, binding, phase, executionID) {
  const { task_id: taskID, engine_id: engineID } = binding
  const checks = []
  const response = page.waitForResponse(r => new URL(r.url()).pathname === `/api/v1/transfer/task-definitions/${taskID}` && r.request().method() === 'GET')
  await frame.getByRole('button', { name: '刷新', exact: true }).click()
  const task = await json(await response, 'CDC task response')
  expect(task.id).toBe(taskID)
  expect(task.config.source.locator).toBe(`addp://engine/${engineID}/path/public/continuous_source?type=table`)
  expect(task.config.target.parent_locator).toBe(`addp://engine/${engineID}/path/public?type=schema`)
  expect(task.config.target.name).toBe('continuous_cdc_target')
  expect(task.desired_state).toBe(binding.desired_state)
  expect(task.capture.generation).toBe(binding.generation)
  await expect(frame.getByTestId('cdc-task-source')).toContainText('public.continuous_source')
  await expect(frame.getByTestId('cdc-task-target')).toContainText('public.continuous_cdc_target')
  checks.push('task_identity')
  const source = task.capture.postgresql_observation
  expect(source.status).toBe('available')
  expect(Date.parse(source.expires_at)).toBeGreaterThan(Date.now())
  expect(source.slot_active).toBe(true)
  expect(source.risk_state).toBe('reserved')
  await expect(frame.getByTestId('cdc-source-active')).toHaveText('活跃')
  await expect(frame.getByTestId('cdc-source-risk')).toHaveText('WAL 仍在常规保留范围')
  for (const field of ['retained_wal_bytes', 'unacknowledged_wal_bytes', 'safe_wal_size_bytes']) {
    const metric = frame.getByTestId(`cdc-source-${field}`)
    if (source[field] === null) await expect(metric).toHaveText('—')
    else await expect(metric).toHaveAttribute('title', `${source[field]} B`)
  }
  checks.push('task_source_slot_wal')
  const delay = task.capture.delay_observation
  expect(delay.status).toBe('available')
  expect(Date.parse(delay.expires_at)).toBeGreaterThan(Date.now())
  expect(delay.connected).toBe(true)
  if (phase !== 'cdc_initial') expect(typeof delay.delay_seconds).toBe('number')
  await expect(frame.getByTestId('cdc-task-source-delay')).toHaveText(delay.delay_seconds === null ? '—' : delay.delay_seconds.toLocaleString('zh-CN', { maximumFractionDigits: 3 }) + ' s')
  checks.push('task_capture_delay')
  const buffer = task.capture.buffer_observation
  expect(buffer.status).toBe('available')
  expect(Date.parse(buffer.expires_at)).toBeGreaterThan(Date.now())
  expect(buffer.recovery_state).toBe('available')
  for (const field of ['committed_offset', 'latest_offset', 'lag_offsets']) expect(buffer[field]).toBe(binding[field])
  await expect(frame.getByTestId('cdc-buffer-lag_offsets')).toHaveText(binding.lag_offsets)
  await expect(frame.getByTestId('cdc-buffer-committed_offset')).toHaveText(binding.committed_offset)
  await expect(frame.getByTestId('cdc-buffer-latest_offset')).toHaveText(binding.latest_offset)
  checks.push('task_buffer_positions')
  expect(task.last_execution_id).toBe(executionID)
  await expect(frame.getByTestId('cdc-task-progress')).toBeVisible()
  checks.push('execution_binding')
  await expect(frame.getByTestId('cdc-task-chain')).not.toContainText('business-value-sentinel')
  checks.push('no_samples')
  return checks
}
