import { expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

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

export async function login(page, username, password) {
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
