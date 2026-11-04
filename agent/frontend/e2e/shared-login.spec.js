import { expect, test } from '@playwright/test'

// Controlled HTTP responses exercise the real shared component, Auth Store,
// router and browser. Cryptographic TOTP and issuance are covered separately
// by the System PostgreSQL browser-login gate, without real user credentials.
for (const chosen of [0, 1]) {
  test(`MFA then tenant ${chosen + 1}: no early session, explicit choice and complete return URL`, async ({ page }) => {
    const contexts = ['A', 'B'].map((name, index) => ({
      type: 'tenant', tenant_id: String(11 + index), tenant_membership_id: String(21 + index),
      tenant_name: `验收租户 ${name}`, tenant_code: `fixture-${name.toLowerCase()}`,
    }))
    const calls = [], identityReads = [], unexpected = []
    let issued = false
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    await page.route(/^https?:\/\/[^/]+\/api\//, async route => {
      const request = route.request(), path = new URL(request.url()).pathname
      const reply = (json, status = 200) => route.fulfill({ status, json })
      if (path === '/api/v1/system/refresh') return issued
        ? reply({ access_token: 'fixture-access', expires_in: 3600 })
        : reply({ error: 'authentication_required' }, 401)
      if (path === '/api/v1/system/login') {
        calls.push('password')
        expect(request.postDataJSON()).toEqual({ username: 'fixture-user', password: 'fixture-password' })
        return reply({ next_action: 'verify_mfa', mfa: {
          challenge_token: 'fixture-challenge', method: 'totp', expires_at: '2099-01-01T00:00:00Z',
        } })
      }
      if (path === '/api/v1/system/auth/mfa-verifications') {
        calls.push('mfa')
        const body = request.postDataJSON()
        expect(body.challenge_token).toBe('fixture-challenge')
        if (body.code !== '123456') return reply({ error: 'invalid_mfa_verification' }, 401)
        return reply({ next_action: 'select_context', selection: {
          selection_ticket: 'fixture-selection', expires_at: '2099-01-01T00:00:00Z', contexts,
        } })
      }
      if (path === '/api/v1/system/auth/context-selections') {
        calls.push('context')
        expect(request.postDataJSON()).toEqual({
          selection_ticket: 'fixture-selection', context_type: 'tenant',
          tenant_membership_id: contexts[chosen].tenant_membership_id,
        })
        issued = true
        return reply({ access_token: 'fixture-access', token_type: 'Bearer', expires_in: 3600 })
      }
      if (['/api/v1/system/users/me', '/api/v1/system/auth/context'].includes(path)) {
        identityReads.push(path)
        expect(issued).toBe(true)
        expect(request.headers().authorization).toBe('Bearer fixture-access')
        return reply(path.endsWith('/users/me') ? { id: '31', username: 'fixture-user' } : {
          principal: { id: '31', type: 'user' }, context: contexts[chosen],
          authentication: { methods: ['password', 'totp'], assurance_level: 'aal2' },
          authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: contexts[chosen].tenant_id }, permissions: ['agent.session.read'] }] },
        })
      }
      if (path === '/api/v1/agent/skills') return reply([])
      if (path === '/api/v1/agent/sessions') return reply({ data: [], total: 0, page: 1, page_size: 20, total_pages: 0 })
      unexpected.push(`${request.method()} ${path}`)
      return reply({ error: 'Unexpected fixture request' }, 500)
    })
    const target = '/?archived=true&search=login-acceptance#history'
    await page.goto(`/module-ui/agent/login?redirect=${encodeURIComponent(target)}`)
    const loginURL = page.url()
    await page.getByPlaceholder('请输入用户名', { exact: true }).fill('fixture-user')
    await page.getByPlaceholder('请输入密码', { exact: true }).fill('fixture-password')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page.getByText('增强认证', { exact: true })).toBeVisible()
    await expect(page).toHaveURL(loginURL)
    await page.getByRole('button', { name: '验证', exact: true }).click()
    await expect(page.getByText('请输入 6 位动态验证码', { exact: true })).toBeVisible()
    expect(calls).toEqual(['password'])
    await page.getByPlaceholder('请输入 6 位验证码').fill('000000')
    await page.getByRole('button', { name: '验证', exact: true }).click()
    await expect(page.getByText('动态验证码错误或已失效', { exact: true })).toBeVisible()
    await expect(page).toHaveURL(loginURL)
    expect(identityReads).toEqual([])
    await page.getByPlaceholder('请输入 6 位验证码').fill('123456')
    await page.getByRole('button', { name: '验证', exact: true }).click()
    await expect(page.getByText('选择租户', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '继续', exact: true })).toBeDisabled()
    expect(identityReads).toEqual([])
    await expect(page).toHaveURL(loginURL)
    const storage = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))
    for (const secret of ['fixture-password', 'fixture-challenge', 'fixture-selection']) expect(storage).not.toContain(secret)
    await page.getByText(contexts[chosen].tenant_name, { exact: true }).click()
    await expect(page.getByRole('radio', { name: contexts[chosen].tenant_name, exact: true })).toBeChecked()
    await page.getByRole('button', { name: '继续', exact: true }).click()
    await expect(page).toHaveURL(new URL(`/module-ui/agent/${target.slice(1)}`, loginURL).href)
    expect(calls).toEqual(['password', 'mfa', 'mfa', 'context'])
    expect(identityReads).toContain('/api/v1/system/auth/context')
    expect(unexpected).toEqual([])
  })
}
