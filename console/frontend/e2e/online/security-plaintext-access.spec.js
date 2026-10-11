import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

// Authorized previews contain fixture plaintext. Keep automatic failure captures off.
test.use({ screenshot: 'off', trace: 'off' })

const sourceName = 'addp_online_security.exemption_source'
const protectedValues = { 1: '138****5678', 2: '139****4321', 3: null }
const originalValues = { 1: '13812345678', 2: '13987654321', 3: null }
const frame = page => page.frameLocator('iframe[data-testid="module-iframe"]')
const endpoint = response => new URL(response.url()).pathname
const accessPath = '/api/v1/security/protection-access-requests'
const reviewPath = `${accessPath}/review-queue`

function assertPreview(payload, original) {
  expect(payload.preview_type).toBe('table')
  expect(payload.data.rows.length).toBe(3)
  const values = Object.fromEntries(payload.data.rows.map(row => [String(row.id), row.phone]))
  const expected = original ? originalValues : protectedValues
  expect(Object.keys(values).sort()).toEqual(['1', '2', '3'])
  expect(Object.keys(expected).every(key => values[key] === expected[key]), 'all phone values and null match the expected protection').toBe(true)
}

async function renderedPreview(page, original) {
  const rows = frame(page).locator('.table-preview .el-table__body tbody tr')
  await expect(rows).toHaveCount(3)
  const expected = original ? originalValues : protectedValues
  await expect.poll(async () => {
    const text = await rows.allTextContents()
    return Object.values(expected).filter(value => value !== null).every(value => text.some(row => row.includes(value)))
  }, { message: 'rendered preview matches the current subject protection' }).toBe(true)
}

async function reloadPreview(page, path, original) {
  let payload
  await expect.poll(async () => {
    const response = page.waitForResponse(r => endpoint(r) === '/api/v1/manager/preview' && r.request().method() === 'GET')
    await page.goto(path)
    payload = await json(await response, 'browser preview')
    expect(payload.preview_type).toBe('table')
    expect(payload.data.rows.length).toBe(3)
    const expected = original ? originalValues : protectedValues
    return payload.data.rows.every(row => Object.hasOwn(expected, String(row.id)) && row.phone === expected[String(row.id)])
  }, { intervals: [1000], message: 'fresh browser preview converges to the current protection' }).toBe(true)
  assertPreview(payload, original)
  await renderedPreview(page, original)
}

function observe(page) {
  const state = { started: false, errors: [], failures: [], anonymous: 0 }
  page.on('request', event => {
    if (new URL(event.url()).pathname === '/api/v1/system/auth/context' && event.headers().authorization) state.started = true
  })
  page.on('pageerror', error => state.errors.push(error.message))
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, state.started)) return
    if (['warning', 'error'].includes(message.type())) state.errors.push(message.text())
  })
  page.on('response', response => {
    if (isAnonymousRefreshResponse(response, state.started)) state.anonymous += 1
    if (state.started && endpoint(response).startsWith('/api/v1/') && response.status() >= 400) {
      state.failures.push({ path: endpoint(response), status: response.status() })
    }
  })
  return state
}

async function browserIdentity(page, prefix, redirect) {
  const env = process.env
  const api = await request.newContext({ baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${env[`${prefix}_ACCESS_TOKEN`]}` } })
  const token = await login(page, env[`${prefix}_USERNAME`], env[`${prefix}_PASSWORD`], redirect)
  const browserAPI = await request.newContext({ baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  try {
    const expected = identity(await json(await api.get('/api/v1/system/auth/context'), 'fixture identity'))
    const actual = identity(await json(await browserAPI.get('/api/v1/system/auth/context'), 'browser identity'))
    expect(actual.principalType).toBe('user')
    expect(actual.contextType).toBe('tenant')
    expect(actual.principalID).toBe(expected.principalID)
    expect(actual.tenantID).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    expect([...actual.permissions].sort()).toEqual([...expected.permissions].sort())
    return actual
  } finally {
    await api.dispose()
    await browserAPI.dispose()
  }
}

async function submitRequest(page, seconds, rationale) {
  await frame(page).getByRole('button', { name: /^(申请查看原值|Request plaintext access)$/ }).click()
  const dialog = frame(page).getByRole('dialog', { name: /申请查看字段原值|Request field plaintext access/ })
  await expect(dialog).toBeVisible()
  const expires = new Date(Date.now() + seconds * 1000)
  const pad = value => String(value).padStart(2, '0')
  const formatted = `${expires.getFullYear()}-${pad(expires.getMonth() + 1)}-${pad(expires.getDate())} ${pad(expires.getHours())}:${pad(expires.getMinutes())}:${pad(expires.getSeconds())}`
  const dateInput = dialog.locator('.el-date-editor input')
  await dateInput.fill(formatted)
  await dateInput.press('Enter')
  await dialog.locator('textarea').fill(rationale)
  const pending = page.waitForResponse(r => endpoint(r) === accessPath && r.request().method() === 'POST')
  await dialog.getByRole('button', { name: /^(提交申请|Submit request)$/ }).click()
  const response = await pending
  expect(response.status()).toBe(201)
  const body = response.request().postDataJSON()
  expect(body.assessment_id).toBe(process.env.ADDP_ONLINE_SECURITY_ASSESSMENT_ID)
  expect(body.consumer_owner).toBe('manager')
  expect(body.action).toBe('preview')
  expect(body).not.toHaveProperty('subject_id')
  expect(body.rationale).toBe(rationale)
  expect(Math.abs(Date.parse(body.requested_expires_at) - expires.getTime()) < 1000, 'UI submitted the chosen short deadline').toBe(true)
  const created = await json(response, 'request from Manager UI')
  expect(created.state).toBe('pending')
  expect(created.id).toBeTruthy()
  await expect(dialog).not.toBeVisible()
  return created
}

async function approve(page, securityPath, created, rationale, approverID) {
  await page.goto(securityPath)
  const card = frame(page).locator('.access-review-card')
  const row = card.getByRole('row').filter({ hasText: rationale })
  await expect(row).toContainText(sourceName)
  await expect(row).toContainText('phone')
  await row.getByRole('button', { name: /^(批准|Approve)$/ }).click()
  const dialog = frame(page).getByRole('dialog').filter({ hasText: rationale })
  await expect(dialog).toContainText(sourceName)
  await dialog.locator('textarea').fill('Hosted browser independent approval')
  const pending = page.waitForResponse(r => endpoint(r) === `${accessPath}/${created.id}/decisions` && r.request().method() === 'POST')
  await dialog.getByRole('button', { name: /^(确认批准|Confirm approval)$/ }).click()
  const response = await pending
  expect(response.status()).toBe(200)
  expect(response.request().postDataJSON().decision).toBe('approve')
  const approved = await json(response, 'approval from Security UI')
  expect(approved.state).toBe('approved')
  expect(approved.requester.id).toBe(created.requester.id)
  expect(approved.reviewer.id).toBe(approverID)
  expect(approved.reviewer.type).toBe('user')
  expect(approved.exemption_id).toBeTruthy()
  await expect(dialog).not.toBeVisible()
  return approved
}

async function history(page, securityPath, created, rationale, state) {
  await page.goto(securityPath)
  const card = frame(page).locator('.access-review-card')
  const pending = page.waitForResponse(r => endpoint(r) === reviewPath && new URL(r.url()).searchParams.get('scope') === 'history')
  await card.locator('.el-radio-button').filter({ hasText: /审批记录|Review history/ }).click()
  const payload = await json(await pending, 'approval history from browser')
  expect(payload.total).toBeGreaterThanOrEqual(1)
  const matches = payload.data.filter(row => row.id === created.id)
  expect(matches.length).toBe(1)
  expect(matches[0].state).toBe('approved')
  expect(matches[0].requester.id).toBe(created.requester.id)
  expect(matches[0].authorization_state).toBe(state)
  const row = card.getByRole('row').filter({ hasText: rationale })
  await expect(row).toContainText(state === 'expired' ? /已到期|Expired/ : state === 'revoked' ? /已撤销|Revoked/ : /生效中|Active/)
  return row
}

test('two users request, approve, expire and revoke plaintext access through Console pages', async ({ browser }) => {
  test.setTimeout(300_000)
  const env = process.env
  for (const name of ['ADDP_ONLINE_ARTIFACT_DIR', 'ADDP_ONLINE_TEST_RUN_ID', 'ADDP_ONLINE_TEST_TENANT_ID',
    'ADDP_ONLINE_TEST_USER_USERNAME', 'ADDP_ONLINE_TEST_USER_PASSWORD', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
    'ADDP_ONLINE_TEST_APPROVER_USERNAME', 'ADDP_ONLINE_TEST_APPROVER_PASSWORD', 'ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN',
    'ADDP_ONLINE_SECURITY_PLAINTEXT_LOCATOR', 'ADDP_ONLINE_SECURITY_ASSESSMENT_ID', 'GATEWAY_URL', 'CONSOLE_URL']) {
    if (!env[name]) throw new Error(`${name} is required`)
  }
  const contexts = []
  try {
    const applicantContext = await browser.newContext({ baseURL: env.CONSOLE_URL })
    contexts.push(applicantContext)
    const approverContext = await browser.newContext({ baseURL: env.CONSOLE_URL })
    contexts.push(approverContext)
    const applicant = await applicantContext.newPage()
    const approver = await approverContext.newPage()
    const applicantState = observe(applicant)
    const approverState = observe(approver)
    const managerPath = `/manager/data-explorer?locator=${encodeURIComponent(env.ADDP_ONLINE_SECURITY_PLAINTEXT_LOCATOR)}`
    const securityPath = '/security/protection-enrollments'
    const initialPreview = applicant.waitForResponse(r => endpoint(r) === '/api/v1/manager/preview')
    const applicantIdentity = await browserIdentity(applicant, 'ADDP_ONLINE_TEST_USER', managerPath)
    assertPreview(await json(await initialPreview, 'initial protected preview'), false)
    await renderedPreview(applicant, false)
    const approverIdentity = await browserIdentity(approver, 'ADDP_ONLINE_TEST_APPROVER', securityPath)
    expect(applicantIdentity.principalID === approverIdentity.principalID).toBe(false)
    expect(applicantIdentity.permissions.has('security.protection_access_request.update')).toBe(false)
    expect(approverIdentity.permissions.has('security.protection_access_request.create')).toBe(false)

    const firstRationale = `Hosted browser expiry ${env.ADDP_ONLINE_TEST_RUN_ID}`
    const first = await submitRequest(applicant, 90, firstRationale)
    expect(first.requester.type).toBe('user')
    expect(first.requester.id).toBe(applicantIdentity.principalID)
    await applicant.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-plaintext-pending.png') })
    const activated = applicant.waitForResponse(r => endpoint(r) === '/api/v1/manager/preview')
    await approve(approver, securityPath, first, firstRationale, approverIdentity.principalID)
    assertPreview(await json(await activated, 'automatically refreshed approved preview'), true)
    await renderedPreview(applicant, true)
    await reloadPreview(approver, managerPath, false)
    await expect(frame(approver).getByRole('button', { name: /^(申请查看原值|Request plaintext access)$/ })).toHaveCount(0)
    await expect.poll(() => Date.now() >= Date.parse(first.requested_expires_at) + 1000, { timeout: 110_000, intervals: [1000] }).toBe(true)
    await reloadPreview(applicant, managerPath, false)
    await history(approver, securityPath, first, firstRationale, 'expired')
    await applicant.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-plaintext-expired.png') })

    const secondRationale = `Hosted browser revocation ${env.ADDP_ONLINE_TEST_RUN_ID}`
    const second = await submitRequest(applicant, 120, secondRationale)
    expect(second.requester.id).toBe(applicantIdentity.principalID)
    expect(second.id === first.id).toBe(false)
    const secondActivated = applicant.waitForResponse(r => endpoint(r) === '/api/v1/manager/preview')
    const approved = await approve(approver, securityPath, second, secondRationale, approverIdentity.principalID)
    assertPreview(await json(await secondActivated, 'second approved preview'), true)
    await renderedPreview(applicant, true)
    await reloadPreview(approver, managerPath, false)
    const activeRow = await history(approver, securityPath, second, secondRationale, 'active')
    await activeRow.getByRole('button', { name: /^(查看授权|View authorization)$/ }).click()
    const grant = frame(approver).locator('.exemption-card.is-focused')
    await expect(grant).toContainText(applicantIdentity.principalID)
    await expect(grant).toContainText('phone')
    await grant.getByRole('button', { name: /^(提前撤销|Revoke early)$/ }).click()
    const revokeDialog = frame(approver).getByRole('dialog', { name: /提前撤销临时授权|Revoke temporary authorization/ })
    await revokeDialog.locator('textarea').fill('Hosted browser work completed')
    const revokedResponse = approver.waitForResponse(r => endpoint(r) === `/api/v1/security/protection-exemptions/${approved.exemption_id}` && r.request().method() === 'DELETE')
    await revokeDialog.getByRole('button', { name: /^(确认撤销|Confirm revocation)$/ }).click()
    const revoked = await revokedResponse
    expect(revoked.status()).toBe(200)
    expect(revoked.request().postDataJSON().rationale).toBe('Hosted browser work completed')
    await expect(revokeDialog).not.toBeVisible()
    // The viewer explicitly reloads the preview after the control-plane revocation.
    await reloadPreview(applicant, managerPath, false)
    await history(approver, securityPath, second, secondRationale, 'revoked')
    await approver.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-plaintext-revoked.png') })
    expect(applicantState.errors.concat(approverState.errors)).toEqual([])
    expect(applicantState.failures.concat(approverState.failures)).toEqual([])
    expect(applicantState.anonymous).toBe(1)
    expect(approverState.anonymous).toBe(1)
    writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'security-plaintext-access-browser.json'), `${JSON.stringify({
      schema_version: 'addp.security-plaintext-access-browser/v1', result: 'passed',
      run_id: env.ADDP_ONLINE_TEST_RUN_ID, tenant_id: applicantIdentity.tenantID,
      applicant_id: applicantIdentity.principalID, approver_id: approverIdentity.principalID,
      records: 3, request_ids: [first.id, second.id], isolated_browser_contexts: true, same_users_verified: true,
      request_from_manager_ui: true, approval_from_security_ui: true, authorized_applicant_verified: true,
      other_subject_protected: true, expired_preview_protected: true, expired_history_verified: true,
      revocation_from_security_ui: true, revoked_preview_protected: true, revoked_history_verified: true,
      null_preserved: true, browser_warning_errors: 0, failed_business_responses: 0, anonymous_refresh_401: 2
    })}\n`, 'utf8')
  } finally {
    for (const context of contexts) await context.close()
  }
})
