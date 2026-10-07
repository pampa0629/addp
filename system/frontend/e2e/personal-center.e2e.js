import { expect, test } from '@playwright/test'

const organization = {
  tenant: { id: '1', name: '户外研究租户', code: 'outdoor' },
  departments: [
    { id: '3', name: '户外部', code: 'outdoor_dept', membership_type: 'primary', relation_role: 'leader', path: [{ id: '2', name: '研究中心' }, { id: '3', name: '户外部' }] },
    { id: '4', name: '数据部', code: 'data_dept', membership_type: 'additional', relation_role: 'member', path: [{ id: '4', name: '数据部' }] }
  ],
  project_groups: [
    { id: '5', name: '山地调查项目', code: 'mountain_research', relation_role: 'coordinator' },
    { id: '6', name: '水系调查项目', code: 'water_research', relation_role: 'member' }
  ]
}
async function fulfillJSON(route, status, body) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
async function setup(page, handleOrganization = route => fulfillJSON(route, 200, organization), handlePassword, handleMFA) {
  let signedIn = false
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path.endsWith('/refresh')) return signedIn ? fulfillJSON(route, 200, { access_token: 'e2e-personal-token', expires_in: 300 }) : fulfillJSON(route, 401, { error: 'authentication_required' })
    if (path.endsWith('/login')) { signedIn = true; return fulfillJSON(route, 200, { next_action: 'session_issued', session: { access_token: 'e2e-personal-token', expires_in: 300 } }) }
    if (path.endsWith('/users/me/organization')) return handleOrganization(route)
    if (path.endsWith('/users/me/password') && handlePassword) return handlePassword(route)
    if (path.endsWith('/users/me')) return fulfillJSON(route, 200, { id: '1', display_name: 'outdoor开发者', primary_email: 'outdoor@example.com', local_account: { username: 'outdoor-develop' } })
    if (path.endsWith('/auth/context')) return fulfillJSON(route, 200, {
      principal: { id: '1', type: 'user' }, context: { type: 'tenant', tenant_id: '1', tenant_membership_id: '11' },
      authentication: { assurance_level: 'aal2' }, authorization: { authorization_version: '1', role_assignments: [] }
    })
    if (handleMFA && path.includes('/auth/mfa')) return handleMFA(route)
    if (path.endsWith('/auth/mfa')) return fulfillJSON(route, 200, { totp_enrolled: true })
    if (path.endsWith('/logout')) { signedIn = false; return fulfillJSON(route, 204, {}) }
    throw new Error(`unexpected personal center E2E request: ${request.method()} ${path}`)
  })
  await page.goto('/login?redirect=%2Faccount')
  await page.locator('input[autocomplete="username"]').fill('outdoor-develop')
  await page.locator('input[autocomplete="current-password"]').fill('not-transmitted')
  await page.locator('button.auth-login-primary').click()
  await expect(page).toHaveURL(/\/account$/)
}

test('one personal center shows organization without management permission and restores categories', async ({ page }) => {
  await setup(page)
  await expect(page.getByRole('heading', { name: '个人中心', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '基本信息', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('户外研究租户', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /户外部.*已加入 2 个项目组/ })).toBeVisible()
  await page.locator('.personal-center__summary').click()
  await expect(page).toHaveURL(/\/account\?tab=organization$/)
  await expect(page.getByText('研究中心 / 户外部', { exact: true })).toBeVisible()
  await expect(page.getByText('附加部门', { exact: true })).toBeVisible()
  await expect(page.getByText('山地调查项目', { exact: true })).toBeVisible()
  await expect(page.getByText('协调人', { exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: '组织归属', exact: true })).toHaveAttribute('aria-selected', 'true')
  await page.screenshot({ path: '/tmp/addp-personal-center.png', fullPage: true, animations: 'disabled' })
  await page.reload()
  await expect(page.getByRole('tab', { name: '组织归属', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByText('水系调查项目', { exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '账号安全', exact: true }).click()
  await expect(page.getByText('已启用', { exact: true })).toBeVisible()
  await expect(page).toHaveURL(/tab=security$/)
  await page.goBack()
  await expect(page.getByText('研究中心 / 户外部', { exact: true })).toBeVisible()
  await page.goForward()
  await expect(page.getByText('已启用', { exact: true })).toBeVisible()
  await page.getByRole('tab', { name: '基本信息', exact: true }).click()
  await expect(page).toHaveURL(/\/account$/)
  await page.goto('/account?tab=basic')
  await expect(page).toHaveURL(/\/account$/)
  await page.goto('/account?tab=organization')
  await page.evaluate(() => localStorage.setItem('addp-lang', 'en'))
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Personal Center', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Organization', exact: true })).toHaveAttribute('aria-selected', 'true')
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByText('山地调查项目', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
})

test('organization errors have retry and never become empty memberships', async ({ page }) => {
  let failed = true
  await setup(page, route => fulfillJSON(route, failed ? 503 : 200, failed ? { error: 'unavailable' } : { tenant: organization.tenant, departments: [], project_groups: [] }))
  await page.getByRole('tab', { name: '组织归属', exact: true }).click()
  await expect(page.getByText('组织归属加载失败，请重试', { exact: true })).toBeVisible()
  await expect(page.getByText('暂未加入部门', { exact: true })).toHaveCount(0)
  failed = false
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.getByText('暂未加入部门', { exact: true })).toBeVisible()
  await expect(page.getByText('暂未加入项目组', { exact: true })).toBeVisible()
})

test('late organization response cannot cross an account or tenant change', async ({ page }) => {
  let pendingRoute
  let requests = 0
  await setup(page, route => {
    requests++
    if (requests === 1) { pendingRoute = route; return }
    return fulfillJSON(route, 200, { tenant: { id: '2', name: '新租户', code: 'new' }, departments: [], project_groups: [] })
  })
  await expect.poll(() => requests).toBe(1)
  await page.evaluate(async () => {
    const { useAuthStore } = await import('/src/store/auth.js')
    const store = useAuthStore()
    store.user = { id: '2', display_name: '新账号', local_account: { username: 'new-user' } }
    store.authContext = { principal: { id: '2', type: 'user' }, context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '22' }, authorization: { authorization_version: '2', role_assignments: [] } }
  })
  await expect(page.getByText('新租户', { exact: true })).toBeVisible()
  await fulfillJSON(pendingRoute, 200, organization)
  await page.getByRole('tab', { name: '组织归属', exact: true }).click()
  await expect(page.getByText('暂未加入部门', { exact: true })).toBeVisible()
  await expect(page.getByText('户外部', { exact: true })).toHaveCount(0)
})

test('password confirmation, server error, and successful sign-out use the existing self-service API', async ({ page }) => {
  let requests = 0
  let fail = true
  await setup(page, undefined, async route => {
    requests++
    expect(route.request().postDataJSON()).toEqual({ current_password: 'old-secret', new_password: 'new-secret' })
    return fulfillJSON(route, fail ? 400 : 200, fail ? { error: '当前密码错误' } : { changed_at: '2026-10-07T00:00:00Z', revoked_family_count: 1 })
  })
  await page.getByRole('tab', { name: '账号安全', exact: true }).click()
  await page.getByRole('button', { name: '修改密码', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '修改密码', exact: true })
  await dialog.locator('input[autocomplete="current-password"]').fill('old-secret')
  await dialog.locator('input[autocomplete="new-password"]').nth(0).fill('new-secret')
  await dialog.locator('input[autocomplete="new-password"]').nth(1).fill('different-secret')
  await dialog.getByRole('button', { name: '修改密码', exact: true }).click()
  await expect(dialog.getByText('两次输入的密码不一致', { exact: true })).toBeVisible()
  expect(requests).toBe(0)
  await dialog.locator('input[autocomplete="new-password"]').nth(1).fill('new-secret')
  await dialog.getByRole('button', { name: '修改密码', exact: true }).click()
  await expect(page.getByText('当前密码错误', { exact: true })).toBeVisible()
  await expect(dialog).toBeVisible()
  fail = false
  await dialog.getByRole('button', { name: '修改密码', exact: true }).click()
  await expect(page).toHaveURL(/\/login$/)
  expect(requests).toBe(2)
})


test('leaving account security ignores a late MFA verification session', async ({ page }) => {
  let verificationRoute
  await setup(page, undefined, undefined, route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/totp-enrollments')) return fulfillJSON(route, 200, {
      enrollment_token: 'e2e-enrollment', secret: 'JBSWY3DPEHPK3PXP',
      otpauth_uri: 'otpauth://totp/ADDP:outdoor?secret=JBSWY3DPEHPK3PXP&issuer=ADDP'
    })
    if (path.endsWith('/totp-enrollment-verifications')) { verificationRoute = route; return }
    return fulfillJSON(route, 200, { totp_enrolled: false })
  })
  await page.getByRole('tab', { name: '账号安全', exact: true }).click()
  await page.getByRole('button', { name: '启用', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('input[autocomplete="current-password"]').fill('old-secret')
  await dialog.getByRole('button', { name: '继续', exact: true }).click()
  await dialog.locator('input[autocomplete="one-time-code"]').fill('123456')
  await dialog.getByRole('button', { name: '验证并启用', exact: true }).click()
  await expect.poll(() => Boolean(verificationRoute)).toBe(true)
  await dialog.locator('.el-dialog__headerbtn').click()
  await page.getByRole('tab', { name: '基本信息', exact: true }).click()
  await fulfillJSON(verificationRoute, 200, { access_token: 'late-mfa-token', expires_in: 300 })
  await expect(page.getByRole('tab', { name: '基本信息', exact: true })).toHaveAttribute('aria-selected', 'true')
  expect(await page.evaluate(async () => (await import('/src/store/auth.js')).useAuthStore().token)).toBe('e2e-personal-token')
})
