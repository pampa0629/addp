import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { randomBytes, randomUUID } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Every error attachment remains in the ephemeral private output directory.
test.use({ screenshot: 'off', trace: 'off', video: 'off' })
const route = '/system/iam/accounts?tab=administrator-changes'
const changes = '/api/v1/system/platform/administrator_changes'
const contextPath = '/api/v1/system/auth/context'
const emergencyRoute = '/system/iam/accounts?tab=emergency-sessions'
const emergencyBase = scope => `/api/v1/system/${scope}/emergency_session_requests`
const fixture = (role, key) => process.env[`ADDP_ONLINE_GOV_${role.toUpperCase()}_${key}`]
const bearer = token => ({ Authorization: `Bearer ${token}` })
const systemModule = page => page.frameLocator('iframe[data-testid="module-iframe"]')
const freshStep = () => new Promise(done => setTimeout(done, 30_000 - Date.now() % 30_000 + 200))
function code(secret) {
  return execFileSync('python3', ['-c', 'import importlib,os,time; print(importlib.import_module("scripts.test.platform-administrator-governance-online").totp(os.environ["ADDP_BROWSER_TOTP"],time.time()))'],
    { cwd: resolve(process.cwd(), '../..'), env: { ...process.env, ADDP_BROWSER_TOTP: secret }, encoding: 'utf8' }).trim()
}
async function json(response) { expect(response.ok()).toBe(true); return response.json() }

async function login(page, role, password, secret, destination = route) {
  await page.goto('/login?redirect=' + encodeURIComponent(destination))
  await page.locator('input[autocomplete="username"]').fill(fixture(role, 'USERNAME'))
  await page.locator('input[autocomplete="current-password"]').fill(password || fixture(role, 'PASSWORD'))
  const response = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/system/login')
  await page.locator('.auth-login-primary').click()
  expect((await json(await response)).next_action).toBe('verify_mfa')
  await expect(page.locator('input[autocomplete="one-time-code"]')).toBeVisible()
  const mfa = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/system/auth/mfa-verifications')
  await page.locator('input[autocomplete="one-time-code"]').fill(code(secret || fixture(role, 'TOTP')))
  await page.locator('.auth-login-primary').click()
  const result = await json(await mfa)
  expect(result.next_action).toBe('session_issued')
  await expect(page).toHaveURL(destination === emergencyRoute
    ? /\/system\/iam\/accounts(?:\?tab=emergency-sessions)?$/
    : /\/system\/iam\/accounts\?tab=administrator-changes$/)
  const token = result.session.access_token
  const identity = await authContext(page, token)
  expect(identity.principal).toEqual({ id: fixture(role, 'ID'), type: 'user' })
  expect(identity.authentication.assurance_level).toMatch(/^aal[23]$/)
  if (role === 'successor') {
    expect(identity.context).toMatchObject({ type: 'tenant', tenant_id: process.env.ADDP_ONLINE_TEST_TENANT_ID })
  } else {
    expect(identity.context).toEqual({ type: 'platform' })
    expect(identity.authorization.role_assignments.map(a => a.role_key)).toEqual([`platform.${role}_administrator`])
  }
  if (destination === emergencyRoute) {
    expect(identity.token.type).toBe('first_party_access_token')
    expect(Date.parse(identity.authentication.step_up_expires_at)).toBeGreaterThan(Date.now())
  }
  return token
}

test('three officers request review and recover through real Console and System', async ({ browser }) => {
  test.setTimeout(240_000)
  const stage = value => writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'administrator-browser-stage.json'), JSON.stringify({ stage: value }))
  stage('authentication')
  const contexts = await Promise.all(['system', 'security', 'audit', 'anonymous'].map(() => browser.newContext({
    baseURL: process.env.CONSOLE_URL, locale: 'zh-CN', viewport: { width: 1440, height: 900 }
  })))
  for (const context of contexts) await context.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const [system, security, audit, anonymous] = await Promise.all(contexts.map(c => c.newPage()))
  try {
    await freshStep()
    const tokens = await Promise.all([login(system, 'system'), login(security, 'security'), login(audit, 'audit')])
    const securityFrame = security.frameLocator('iframe[data-testid="module-iframe"]')
    const auditFrame = audit.frameLocator('iframe[data-testid="module-iframe"]')
    const systemFrame = system.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(auditFrame.getByRole('button', { name: '创建管理员变更请求', exact: true })).toHaveCount(0)
    await expect(securityFrame.getByRole('button', { name: '创建管理员变更请求', exact: true })).toBeVisible()
    await expect(systemFrame.getByRole('button', { name: '创建管理员变更请求', exact: true })).toBeVisible()
    stage('request')
    await securityFrame.getByRole('button', { name: '创建管理员变更请求', exact: true }).click()
    const dialog = securityFrame.getByRole('dialog')
    await dialog.locator('.el-select').nth(0).click()
    await securityFrame.getByRole('option', { name: '恢复密码与 MFA', exact: true }).click()
    await dialog.locator('.el-select').nth(2).click()
    await securityFrame.getByRole('option').filter({ hasText: process.env.ADDP_ONLINE_GOV_SYSTEM_ID + ')' }).click()
    await dialog.locator('textarea').fill(process.env.ADDP_ONLINE_TEST_RUN_ID + ':browser-recovery')
    const created = security.waitForResponse(r => new URL(r.url()).pathname === changes && r.request().method() === 'POST')
    await dialog.getByRole('button', { name: '提交', exact: true }).click()
    const pending = await json(await created)
    expect(pending.status).toBe('pending')
    expect(pending.requested_by_principal_id).toBe(process.env.ADDP_ONLINE_GOV_SECURITY_ID)
    stage('review')
    await auditFrame.getByRole('button', { name: '刷新', exact: true }).click()
    const row = auditFrame.getByRole('row').filter({ hasText: process.env.ADDP_ONLINE_TEST_RUN_ID + ':browser-recovery' })
    await row.getByRole('button', { name: '批准', exact: true }).click()
    const review = auditFrame.getByRole('dialog')
    await review.locator('textarea').fill(process.env.ADDP_ONLINE_TEST_RUN_ID + ':browser-review')
    const approvedResponse = audit.waitForResponse(r => new URL(r.url()).pathname === `${changes}/${pending.id}/approve`)
    await review.getByRole('button', { name: '确认', exact: true }).click()
    const approved = await json(await approvedResponse)
    expect(approved.status).toBe('approved')
    expect(approved.review.principal_id).toBe(process.env.ADDP_ONLINE_GOV_AUDIT_ID)
    expect(approved.review.principal_id).not.toBe(pending.requested_by_principal_id)
    const proofDialog = auditFrame.getByRole('dialog', { name: '一次性恢复凭据', exact: true })
    const proof = await proofDialog.locator('textarea').inputValue()
    expect(proof).toBe(approved.recovery_token)
    stage('recovery-enrollment')
    await anonymous.goto('/administrator-recovery')
    const recovery = anonymous.frameLocator('iframe.administrator-recovery-frame')
    await recovery.locator('textarea').fill(proof)
    const enrollmentResponse = anonymous.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/system/auth/administrator-recovery/enrollments')
    await recovery.getByRole('button', { name: '继续', exact: true }).click()
    const enrollment = await json(await enrollmentResponse)
    await expect(recovery.locator('.totp-details code')).toHaveText(enrollment.secret)
    const password = 'Browser-recovered-' + randomBytes(24).toString('base64url')
    await recovery.locator('input[autocomplete="new-password"]').nth(0).fill(password)
    await recovery.locator('input[autocomplete="new-password"]').nth(1).fill(password)
    await recovery.locator('input[autocomplete="one-time-code"]').fill(code(enrollment.secret))
    const completed = anonymous.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/system/auth/administrator-recovery/completions')
    await recovery.getByRole('button', { name: '确认新凭据', exact: true }).click()
    expect((await completed).status()).toBe(204)
    await expect(recovery.locator('.totp-details')).toHaveCount(0)
    await expect(recovery.getByText('凭据已更新，请重新登录。账号原有暂停状态保持不变。', { exact: true })).toBeVisible()
    const reused = await anonymous.request.post('/api/v1/system/auth/administrator-recovery/enrollments', { data: { recovery_token: proof } })
    expect(reused.status()).toBe(401)
    const revoked = await system.request.get(contextPath, { headers: { Authorization: `Bearer ${tokens[0]}` } })
    expect(revoked.status()).toBe(401)
    const recoveryFrame = anonymous.frames().find(f => f.url().includes('/module-ui/system/administrator-recovery'))
    expect(recoveryFrame).toBeTruthy()
    const storage = await recoveryFrame.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }))
    for (const secret of [proof, enrollment.enrollment_token, enrollment.secret, password]) expect(storage).not.toContain(secret)
    expect(anonymous.url()).not.toContain(proof)
    stage('new-login')
    const newContext = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN' })
    try {
      await newContext.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
      await freshStep()
      await login(await newContext.newPage(), 'system', password, enrollment.secret)
    } finally { await newContext.close() }
    stage('passed')
    writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'administrator-browser.json'), JSON.stringify({
      real_mfa: true, role_matrix: true, request_created: true, independent_review: true,
      one_time_recovery: true, secret_memory_only: true, old_session_revoked: true, new_login: true
    }))
  } finally { for (const context of contexts) await context.close() }
})

async function authContext(page, token) {
  return json(await page.request.get(contextPath, { headers: bearer(token) }))
}

const requestDetails = frame => frame.getByRole('dialog', { name: '查看办理记录', exact: true })
const button = (scope, name) => scope.getByRole('button', { name, exact: true })
const writeResponse = (page, path) => page.waitForResponse(response =>
  new URL(response.url()).pathname === path && response.request().method() === 'POST')

async function lookupEmergency(frame, id) {
  await frame.getByRole('textbox', { name: '请求编号', exact: true }).fill(id)
  await button(frame, '按编号核清').click()
  await expect(requestDetails(frame)).toContainText(id)
  return requestDetails(frame)
}

test('single-session emergency revocation through real Console', async ({ browser }) => {
  test.setTimeout(180_000)
  const directory = process.env.ADDP_ONLINE_ARTIFACT_DIR
  const stage = value => writeFileSync(resolve(directory, 'emergency-browser-stage.json'), JSON.stringify({ stage: value }))
  const roles = ['successor', 'system', 'security', 'audit']
  const contexts = []
  const tenantPath = emergencyBase('tenant')
  const platformPath = emergencyBase('platform')
  try {
    stage('authentication')
    for (let index = 0; index < roles.length; index++) {
      const context = await browser.newContext({ baseURL: process.env.CONSOLE_URL, locale: 'zh-CN', viewport: { width: 1440, height: 900 } })
      contexts.push(context)
      await context.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    }
    const [tenant, system, security, audit] = await Promise.all(contexts.map(context => context.newPage()))
    await freshStep()
    const [executorToken, , , auditToken] = await Promise.all([tenant, system, security, audit].map((page, index) =>
      login(page, roles[index], undefined, undefined, emergencyRoute)))
    const tenantFrame = systemModule(tenant)
    const auditFrame = systemModule(audit)
    const targetToken = fixture('target', 'ACCESS_TOKEN')
    const otherToken = fixture('target', 'OTHER_ACCESS_TOKEN')
    const family = fixture('target', 'FAMILY_ID')
    expect(targetToken).toBeTruthy()
    expect(otherToken).toBeTruthy()
    expect(otherToken).not.toBe(targetToken)
    expect(family).toMatch(/^[1-9]\d*$/)
    const target = await authContext(tenant, targetToken)
    const otherBefore = await authContext(tenant, otherToken)
    expect(otherBefore.principal).toEqual(target.principal)
    expect(otherBefore.context).toEqual(target.context)
    expect(target.context).toMatchObject({ type: 'tenant', tenant_id: process.env.ADDP_ONLINE_TEST_TENANT_ID })
    expect(target.token.type).toBe('first_party_access_token')
    expect(otherBefore.token.type).toBe('first_party_access_token')

    stage('request')
    await button(tenantFrame, '申请应急撤销').click()
    const form = tenantFrame.getByRole('dialog', { name: '申请应急撤销', exact: true })
    const incident = process.env.ADDP_ONLINE_TEST_RUN_ID
    await form.getByLabel('目标用户主体编号').fill(target.principal.id)
    await form.getByLabel('目标会话族编号').fill(family)
    await form.getByLabel('事件编号', { exact: true }).fill(incident)
    await form.getByLabel('理由', { exact: true }).fill('Disposable suspicious Web session')
    const created = writeResponse(tenant, tenantPath)
    await button(form, '确认提交').click()
    const createResponse = await created
    expect(createResponse.status()).toBe(201)
    const pending = await createResponse.json()
    expect(pending).toMatchObject({
      tenant_id: process.env.ADDP_ONLINE_TEST_TENANT_ID, executor_principal_id: fixture('successor', 'ID'),
      target_principal_id: target.principal.id, target_family_id: family, incident_reference: incident, status: 'pending'
    })
    expect(pending.tenant_name).toBeTruthy()
    expect(pending.target_name).toBeTruthy()
    const tenantRequest = `${tenantPath}/${pending.id}`
    const platformRequest = `${platformPath}/${pending.id}`
    await expect(requestDetails(tenantFrame)).toContainText(pending.id)
    await expect(button(requestDetails(tenantFrame), '执行撤销')).toHaveCount(0)
    const prematureID = randomUUID()
    const executeBeforeApproval = version => tenant.request.post(`${tenantRequest}/execute`, {
      headers: bearer(executorToken), data: { version, execution_id: prematureID }
    })
    expect((await executeBeforeApproval(pending.version)).status()).toBe(409)
    expect((await audit.request.post(`${platformRequest}/approve`, {
      headers: bearer(auditToken), data: { version: pending.version, reason: 'audit must only observe' }
    })).status()).toBe(403)
    const auditPending = await lookupEmergency(auditFrame, pending.id)
    for (const action of ['批准', '拒绝', '撤回授权', '执行撤销', '取消申请']) await expect(button(auditPending, action)).toHaveCount(0)
    await auditPending.getByRole('button', { name: '关闭此对话框', exact: true }).click()

    stage('independent-approvals')
    let current = pending
    for (const [page, role] of [[system, 'system'], [security, 'security']]) {
      const frame = systemModule(page)
      const details = await lookupEmergency(frame, pending.id)
      await expect(details).toContainText(family)
      await button(details, '批准').click()
      const approval = frame.getByRole('dialog', { name: '批准', exact: true })
      await approval.getByLabel('理由', { exact: true }).fill(`Independent ${role} review`)
      const response = writeResponse(page, `${platformRequest}/approve`)
      await button(approval, '确认提交').click()
      current = await json(await response)
      expect(current.approvals.find(approval => approval.principal_id === fixture(role, 'ID'))).toMatchObject({ role_key: `platform.${role}_administrator`, principal_id: fixture(role, 'ID') })
      if (role === 'system') {
        expect(current.status).toBe('pending')
        expect(current.approvals).toHaveLength(1)
        expect((await executeBeforeApproval(current.version)).status()).toBe(409)
      }
    }
    expect(current.status).toBe('approved')
    expect(current.approvals).toHaveLength(2)
    expect(new Set(current.approvals.map(approval => approval.principal_id)).size).toBe(2)
    expect(Date.parse(current.approval_expires_at) - Date.parse(current.requested_at)).toBe(600_000)
    expect(Date.parse(current.execute_before) - Date.parse(current.approved_at)).toBe(900_000)

    stage('execute')
    await requestDetails(tenantFrame).getByRole('button', { name: '关闭此对话框', exact: true }).click()
    const ready = await lookupEmergency(tenantFrame, pending.id)
    await button(ready, '执行撤销').click()
    const execution = tenantFrame.getByRole('dialog', { name: '执行撤销', exact: true })
    await expect(execution).toContainText(family)
    const executePath = `${tenantRequest}/execute`
    const isExecution = request => new URL(request.url()).pathname === executePath && request.method() === 'POST'
    const browserOperations = []
    tenant.on('request', request => {
      if (isExecution(request)) browserOperations.push('execute')
      if (new URL(request.url()).pathname === tenantRequest && request.method() === 'GET') browserOperations.push('lookup')
    })
    let forwarded = false
    let upstreamResponse
    await tenant.route(new URL(executePath, process.env.CONSOLE_URL).href, async route => {
      if (!isExecution(route.request())) return route.fallback()
      try {
        // Forward the browser's real command once, retaining only the upstream response in memory.
        if (!forwarded) {
          forwarded = true
          upstreamResponse = await route.fetch({ maxRetries: 0, maxRedirects: 0 })
        }
      } finally {
        await route.abort('connectionreset')
      }
    })
    const disconnected = tenant.waitForEvent('requestfailed', isExecution)
    await button(execution, '确认提交').click()
    const failedRequest = await disconnected
    expect(failedRequest.failure()?.errorText).toContain('ERR_CONNECTION_RESET')
    const command = failedRequest.postDataJSON()
    expect(upstreamResponse?.status()).toBe(200)
    const receipt = await upstreamResponse.json()
    expect(receipt).toMatchObject({ id: pending.id, status: 'executed', outcome: 'revoked', execution_id: command.execution_id })

    stage('execution-receipt-withheld')
    const unresolved = tenantFrame.getByText(`请求 ${pending.id} 的结果待核清。`, { exact: false })
    await expect(unresolved).toBeVisible()
    await expect(requestDetails(tenantFrame)).not.toBeVisible()
    await expect(button(tenantFrame, '申请应急撤销')).toBeDisabled()
    await expect(tenantFrame.getByRole('textbox', { name: '请求编号', exact: true })).toHaveValue(pending.id)
    expect(browserOperations).toEqual(['execute'])

    stage('exact-session-effect-before-lookup')
    expect((await tenant.request.get(contextPath, { headers: bearer(targetToken) })).status()).toBe(401)
    const otherAfter = await authContext(tenant, otherToken)
    expect(otherAfter.principal).toEqual(otherBefore.principal)
    expect(otherAfter.context).toEqual(otherBefore.context)
    expect(otherAfter.authorization).toEqual(otherBefore.authorization)

    stage('read-only-reconciliation')
    const reconciled = tenant.waitForResponse(r => new URL(r.url()).pathname === tenantRequest && r.request().method() === 'GET')
    await button(tenantFrame, '按编号核清').click()
    expect(await json(await reconciled)).toEqual(receipt)
    const recovered = requestDetails(tenantFrame)
    await expect(recovered).toContainText(receipt.execution_id)
    await expect(recovered).toContainText('目标会话已撤销')
    await expect(unresolved).toHaveCount(0)
    await expect(button(recovered, '执行撤销')).toHaveCount(0)
    expect(browserOperations).toEqual(['execute', 'lookup'])

    // Explicit API probes run outside the page and must not count as browser writes.
    const replay = await tenant.request.post(`${tenantRequest}/execute`, { headers: bearer(executorToken), data: command })
    expect(await json(replay)).toEqual(receipt)
    const second = await tenant.request.post(`${tenantRequest}/execute`, {
      headers: bearer(executorToken), data: { version: receipt.version, execution_id: randomUUID() }
    })
    expect(second.status()).toBe(409)

    stage('independent-audit')
    const audited = audit.waitForResponse(r => new URL(r.url()).pathname === platformRequest && r.request().method() === 'GET')
    const record = await lookupEmergency(auditFrame, pending.id)
    expect(await json(await audited)).toEqual(receipt)
    await expect(record).toContainText(receipt.execution_id)
    await expect(record).toContainText('目标会话已撤销')
    for (const action of ['批准', '拒绝', '撤回授权', '执行撤销', '取消申请']) await expect(button(record, action)).toHaveCount(0)
    const events = await json(await audit.request.get('/api/v1/system/platform/audit/events?entity_type=emergency_session_request&page_size=100', { headers: bearer(auditToken) }))
    const rows = events.data.filter(row => row.entity_id === pending.id)
    expect(rows.map(row => [row.event_name, row.principal_id, row.result]).sort()).toEqual([
      ['iam.emergency_session_revocation.created', fixture('successor', 'ID'), 'succeeded'],
      ['iam.emergency_session_revocation.approve', fixture('system', 'ID'), 'succeeded'],
      ['iam.emergency_session_revocation.approve', fixture('security', 'ID'), 'succeeded'],
      ['iam.emergency_session_revocation.execute', fixture('successor', 'ID'), 'succeeded']
    ].sort())
    for (const row of rows) expect(row.details).toMatchObject({ target_family_id: family, target_principal_id: target.principal.id })
    const auditJSON = JSON.stringify(rows)
    for (const token of [executorToken, auditToken, targetToken, otherToken]) expect(auditJSON).not.toContain(token)
    expect(browserOperations).toEqual(['execute', 'lookup'])
  } finally {
    for (const context of contexts) await context.close()
  }
  stage('passed')
  writeFileSync(resolve(directory, 'emergency-browser.json'), JSON.stringify({
    real_console: true, exact_tenant_family: true, two_independent_approvals: true, ten_fifteen_minute_windows: true,
    single_consumption_receipt: true, target_session_revoked: true, unrelated_session_unchanged: true, independent_audit: true,
    committed_receipt_withheld: true, original_request_reconciled_read_only: true, no_browser_write_replay: true
  }))
})
