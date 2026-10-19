import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Every error attachment remains in the ephemeral private output directory.
test.use({ screenshot: 'off', trace: 'off', video: 'off' })
const route = '/system/iam/accounts?tab=administrator-changes'
const changes = '/api/v1/system/platform/administrator_changes'
const contextPath = '/api/v1/system/auth/context'
const freshStep = () => new Promise(done => setTimeout(done, 30_000 - Date.now() % 30_000 + 200))
function code(secret) {
  return execFileSync('python3', ['-c', 'import importlib,os,time; print(importlib.import_module("scripts.test.platform-administrator-governance-online").totp(os.environ["ADDP_BROWSER_TOTP"],time.time()))'],
    { cwd: resolve(process.cwd(), '../..'), env: { ...process.env, ADDP_BROWSER_TOTP: secret }, encoding: 'utf8' }).trim()
}
async function json(response) { expect(response.ok()).toBe(true); return response.json() }

async function login(page, role, password, secret) {
  const prefix = `ADDP_ONLINE_GOV_${role.toUpperCase()}`
  await page.goto('/login?redirect=' + encodeURIComponent(route))
  await page.locator('input[autocomplete="username"]').fill(process.env[prefix + '_USERNAME'])
  await page.locator('input[autocomplete="current-password"]').fill(password || process.env[prefix + '_PASSWORD'])
  const response = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/system/login')
  await page.locator('.auth-login-primary').click()
  expect((await json(await response)).next_action).toBe('verify_mfa')
  await expect(page.locator('input[autocomplete="one-time-code"]')).toBeVisible()
  const mfa = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/system/auth/mfa-verifications')
  await page.locator('input[autocomplete="one-time-code"]').fill(code(secret || process.env[prefix + '_TOTP']))
  await page.locator('.auth-login-primary').click()
  const result = await json(await mfa)
  expect(result.next_action).toBe('session_issued')
  await expect(page).toHaveURL(/\/system\/iam\/accounts\?tab=administrator-changes$/)
  const token = result.session.access_token
  const identity = await json(await page.request.get(contextPath, { headers: { Authorization: `Bearer ${token}` } }))
  expect(identity.principal).toEqual({ id: process.env[prefix + '_ID'], type: 'user' })
  expect(identity.context).toEqual({ type: 'platform' })
  expect(identity.authentication.assurance_level).toMatch(/^aal[23]$/)
  expect(identity.authorization.role_assignments.map(a => a.role_key)).toEqual([`platform.${role}_administrator`])
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
