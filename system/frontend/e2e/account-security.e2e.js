import { expect, test } from '@playwright/test'

async function fulfillJSON(route, status, body) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    headers: {
      'access-control-allow-origin': 'http://127.0.0.1:4173',
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,POST,OPTIONS'
    },
    body: JSON.stringify(body)
  })
}

test('current user opens the single account security page outside IAM management tabs', async ({ page }) => {
  await page.route('**/api/v1/system/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()

    if (method === 'OPTIONS') {
      await fulfillJSON(route, 204, {})
      return
    }
    if (path.endsWith('/refresh')) {
      await fulfillJSON(route, 401, { error: 'authentication_required' })
      return
    }
    if (path.endsWith('/login') && method === 'POST') {
      await fulfillJSON(route, 200, {
        next_action: 'session_issued',
        session: { access_token: 'e2e-account-token', expires_in: 300 }
      })
      return
    }
    if (path.endsWith('/users/me')) {
      await fulfillJSON(route, 200, {
        id: '1', display_name: 'E2E Administrator', local_account: { username: 'e2e-admin' }
      })
      return
    }
    if (path.endsWith('/auth/context')) {
      await fulfillJSON(route, 200, {
        principal: { id: '1', principal_type: 'user' },
        context: { type: 'tenant', tenant_id: '1', tenant_membership_id: '11' },
        authentication: { assurance_level: 'aal2' },
        authorization: { role_assignments: [] }
      })
      return
    }
    if (path.endsWith('/auth/mfa')) {
      await fulfillJSON(route, 200, { totp_enrolled: true })
      return
    }

    throw new Error(`unexpected account security E2E request: ${method} ${path}`)
  })

  await page.goto('/login?redirect=%2Faccount%2Fsecurity')
  await page.locator('input[autocomplete="username"]').fill('e2e-admin')
  await page.locator('input[autocomplete="current-password"]').fill('not-transmitted')
  await page.locator('button.auth-login-primary').click()

  await expect(page).toHaveURL(/\/account\/security$/)
  await expect(page.getByRole('heading', { name: '我的账号', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '安全设置', exact: true })).toBeVisible()
  await expect(page.getByText('当前会话：多因素认证', { exact: true })).toBeVisible()
  await expect(page.locator('.iam-security-row__identity strong')).toHaveText('多因素认证')
  await expect(page.getByText('已启用', { exact: true })).toBeVisible()
})
