import { expect, test } from '@playwright/test'

const requestID = '949f5710-1d2d-40a8-8fd2-dc291024b99e'
const root = '/iam/accounts?tab=emergency-sessions'
const targetID = '9007199254740993'
const familyID = '9007199254740997'
const permission = action => 'iam.emergency_session_revocation.' + action
const future = () => new Date(Date.now() + 600_000).toISOString()

async function mock(page, actor, { stepUp = true, responseLoss = false } = {}) {
  const tenant = actor === 'tenant'
  const principal = { system: '2', security: '3', audit: '4', tenant: '10' }[actor]
  const scope = tenant ? 'tenant' : 'platform'
  const actions = tenant ? ['create', 'cancel', 'execute'] : actor === 'audit' ? ['approve'] : ['approve', 'reject', 'revoke']
  const requests = []
  let refreshes = 0
  let row = {
    id: requestID, version: '2', incident_reference: 'IR-42', tenant_id: '8',
    tenant_name: 'Research', executor_name: 'Requester', executor_principal_id: '10', target_name: 'Member',
    target_principal_id: targetID, target_family_id: familyID, reason: 'Suspicious login',
    effective_state: tenant ? 'approved' : 'pending', approval_expires_at: future(),
    execute_before: tenant ? future() : null,
    approvals: [{ role_key: 'platform.system_administrator', principal_id: '2', display_name: 'Operator', reason: 'Verified', approved_at: new Date().toISOString() }]
  }
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    const reply = (json, status = 200) => route.fulfill({ json, status })
    if (path.endsWith('/refresh')) {
      refreshes++
      return reply({ access_token: 'first-token', expires_in: 3600 })
    }
    if (path.endsWith('/users/me')) return reply({ id: principal, display_name: actor })
    if (path.endsWith('/auth/context')) {
      const elevated = request.headers().authorization === 'Bearer mfa-token'
      return reply({
        principal: { id: principal, type: 'user' },
        context: { type: scope, ...(tenant ? { tenant_id: '8', tenant_membership_id: '12' } : {}) },
        authentication: { assurance_level: 'aal2', step_up_expires_at: elevated || stepUp ? future() : null },
        authorization: { role_assignments: [{ role_key: tenant ? 'tenant.tenant_administrator' : `platform.${actor}_administrator`, scope: { type: scope, ...(tenant ? { tenant_id: '8' } : {}) }, permissions: ['read', ...actions].map(permission) }] }
      })
    }
    if (path.endsWith('/mfa/step-up-challenges')) {
      requests.push({ method, kind: 'challenge' })
      return reply({ challenge_token: 'step-up-fixture' })
    }
    if (path.endsWith('/mfa/step-up-verifications')) {
      requests.push({ method, kind: 'verify' })
      return reply({ access_token: 'mfa-token', expires_in: 3600 })
    }
    const base = `/api/v1/system/${scope}/emergency_session_requests`
    if (path === base && method === 'GET') return reply({ data: [row], total: 1 })
    if (path === base && method === 'POST') {
      const payload = request.postDataJSON()
      requests.push({ method, kind: 'create', payload })
      return reply({ ...row, ...payload, effective_state: 'pending', execute_before: null, approvals: [] }, 201)
    }
    if (path === `${base}/${requestID}` && method === 'GET') {
      requests.push({ method, kind: 'lookup' })
      return reply(row)
    }
    if (path === `${base}/${requestID}/execute` && method === 'POST') {
      const payload = request.postDataJSON()
      requests.push({ method, kind: 'execute', payload })
      row = { ...row, version: '3', effective_state: 'executed', execution_id: payload.execution_id, outcome: 'already_revoked', executed_at: new Date().toISOString() }
      return responseLoss ? reply({ error: 'Connection lost after commit' }, 401) : reply(row)
    }
    if (path === `${base}/${requestID}/approve` && method === 'POST') {
      requests.push({ method, kind: 'approve', payload: request.postDataJSON() })
      row = { ...row, version: '3', effective_state: 'approved', execute_before: future(), approvals: [
        ...row.approvals, { role_key: 'platform.security_administrator', principal_id: '3', display_name: 'Reviewer', reason: 'Independently verified', approved_at: new Date().toISOString() }
      ] }
      return reply(row)
    }
    throw new Error(`Unexpected request: ${method} ${path}`)
  })
  return { requests, refreshCount: () => refreshes }
}

const button = (page, name) => page.getByRole('button', { name, exact: true })
const detail = page => button(page, '查看办理记录').click()

test('tenant creates a request without losing 64-bit IDs or selecting another tenant', async ({ page }) => {
  const { requests } = await mock(page, 'tenant')
  await page.goto(root)
  await button(page, '申请应急撤销').click()
  await page.getByLabel('目标用户主体编号').fill(targetID)
  await page.getByLabel('目标会话族编号').fill(familyID)
  await page.getByLabel('事件编号', { exact: true }).fill('IR-43')
  await page.getByLabel('理由', { exact: true }).fill('Known session family')
  const id = await page.locator('code').textContent()
  await button(page, '确认提交').click()
  await expect(page.getByRole('dialog', { name: '查看办理记录' })).toContainText(id)
  expect(requests).toEqual([{ method: 'POST', kind: 'create', payload: {
    id, target_principal_id: targetID, target_family_id: familyID, incident_reference: 'IR-43', reason: 'Known session family'
  } }])
})

for (const actor of ['system', 'audit']) {
  test(`${actor} cannot give a second approval for the same role or write as an auditor`, async ({ page }) => {
    const { requests } = await mock(page, actor)
    await page.goto(root)
    await detail(page)
    await expect(page.getByRole('dialog', { name: '查看办理记录' })).toContainText('系统管理员 · Operator')
    await expect(button(page, '批准')).toHaveCount(0)
    await expect(button(page, '拒绝')).toHaveCount(actor === 'system' ? 1 : 0)
    await expect(button(page, '申请应急撤销')).toHaveCount(0)
    expect(requests).toEqual([{ method: 'GET', kind: 'lookup' }])
  })
}

test('MFA does not submit approval until security administrator explicitly confirms again', async ({ page }) => {
  const { requests } = await mock(page, 'security', { stepUp: false })
  await page.goto(root)
  await detail(page)
  await button(page, '批准').click()
  await button(page, '增强认证').click()
  await page.locator('input[autocomplete="one-time-code"]').fill('123456')
  await button(page, '验证并继续').click()
  await expect(button(page, '查看办理记录')).toBeVisible()
  expect(requests.map(r => r.kind)).toEqual(['lookup', 'challenge', 'verify'])
  await detail(page)
  await button(page, '批准').click()
  await page.getByLabel('理由', { exact: true }).fill('Independently verified')
  await button(page, '确认提交').click()
  await expect(page.getByRole('dialog', { name: '查看办理记录' })).toContainText('安全管理员 · Reviewer')
  expect(requests.at(-1)).toEqual({ method: 'POST', kind: 'approve', payload: { version: '2', reason: 'Independently verified' } })
})

test('lost execution response is resolved by original ID without refresh-and-replay', async ({ page }) => {
  const { requests, refreshCount } = await mock(page, 'tenant', { responseLoss: true })
  await page.goto(root)
  await detail(page)
  await button(page, '执行撤销').click()
  await expect(page.getByRole('dialog', { name: '执行撤销' })).toContainText(familyID)
  await button(page, '确认提交').click()
  await expect(page.getByText(`请求 ${requestID} 的结果待核清`, { exact: false })).toBeVisible()
  await expect(button(page, '申请应急撤销')).toBeDisabled()
  await button(page, '按编号核清').click()
  await expect(page.getByText('目标会话此前已撤销', { exact: false })).toBeVisible()
  expect(requests.map(r => r.kind)).toEqual(['lookup', 'execute', 'lookup'])
  expect(requests[1].payload).toEqual({ version: '2', execution_id: expect.stringMatching(/^[a-f0-9-]{36}$/) })
  expect(refreshCount()).toBe(1)
})

test('a late MFA response cannot restore a previous authorization context', async ({ page }) => {
  const { requests } = await mock(page, 'security', { stepUp: false })
  let received
  const verificationStarted = new Promise(resolve => { received = resolve })
  let release
  const releaseResponse = new Promise(resolve => { release = resolve })
  await page.route('**/api/v1/system/auth/mfa/step-up-verifications', async route => {
    received()
    await releaseResponse
    await route.fulfill({ json: { access_token: 'mfa-token', expires_in: 3600 } })
  })
  await page.goto(root)
  await detail(page)
  await button(page, '批准').click()
  await button(page, '增强认证').click()
  await page.locator('input[autocomplete="one-time-code"]').fill('123456')
  await button(page, '验证并继续').click()
  await verificationStarted
  try {
    await page.evaluate(async () => {
      const { useAuthStore } = await import('/src/store/auth.js')
      useAuthStore().setToken('new-context-token', 3600)
    })
    await expect(button(page, '查看办理记录')).toBeVisible()
  } finally {
    release()
  }
  await detail(page)
  await button(page, '批准').click()
  await expect(button(page, '增强认证')).toBeVisible()
  expect(await page.evaluate(async () => {
    const { useAuthStore } = await import('/src/store/auth.js')
    return useAuthStore().token
  })).toBe('new-context-token')
  expect(requests.every(request => request.kind !== 'approve')).toBe(true)
})
