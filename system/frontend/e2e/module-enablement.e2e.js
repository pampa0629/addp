import { expect, test } from '@playwright/test'

async function fulfillJSON(route, status, body) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    headers: {
      'access-control-allow-origin': 'http://127.0.0.1:4173',
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,PUT,POST,OPTIONS'
    },
    body: JSON.stringify(body)
  })
}

test('System is always enabled while a business module can be disabled', async ({ page }) => {
  const modules = [
    { id: 1, module_name: 'system', route_prefix: '/system', enabled: true, version: 1, instances: [] },
    { id: 2, module_name: 'manager', route_prefix: '/manager', enabled: true, version: 1, instances: [] }
  ]
  const writes = []
  await page.route('**/api/v1/system/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    if (method === 'OPTIONS') return fulfillJSON(route, 204, {})
    if (path.endsWith('/refresh')) return fulfillJSON(route, 401, { error: 'authentication_required' })
    if (path.endsWith('/login') && method === 'POST') {
      return fulfillJSON(route, 200, {
        next_action: 'session_issued',
        session: { access_token: 'e2e-module-token', expires_in: 300 }
      })
    }
    if (path.endsWith('/users/me')) {
      return fulfillJSON(route, 200, { id: '1', display_name: 'E2E Administrator' })
    }
    if (path.endsWith('/auth/context')) {
      return fulfillJSON(route, 200, {
        principal: { id: '1', principal_type: 'user' },
        context: { type: 'platform' },
        authentication: { assurance_level: 'aal2' },
        authorization: {
          role_assignments: [{
            role_key: 'platform.system_administrator', scope_type: 'platform',
            permissions: ['platform.module.read', 'platform.module.update']
          }]
        }
      })
    }
    if (path.endsWith('/platform/modules') && method === 'GET') {
      return fulfillJSON(route, 200, { modules })
    }
    if (path.endsWith('/platform/modules/manager') && method === 'PUT') {
      writes.push(request.postDataJSON())
      modules[1] = { ...modules[1], enabled: false, version: 2 }
      return fulfillJSON(route, 200, modules[1])
    }
    throw new Error(`unexpected module management E2E request: ${method} ${path}`)
  })

  await page.goto('/login?redirect=%2Fmodules')
  await page.locator('input[autocomplete="username"]').fill('e2e-admin')
  await page.locator('input[autocomplete="current-password"]').fill('e2e-password')
  await page.locator('button.auth-login-primary').click()

  await expect(page).toHaveURL(/\/modules$/)
  const systemRow = page.getByRole('row').filter({ has: page.getByRole('cell', { name: 'system', exact: true }) })
  const managerRow = page.getByRole('row').filter({ has: page.getByRole('cell', { name: 'manager', exact: true }) })
  await expect(systemRow.getByText('始终启用')).toBeVisible()
  await expect(systemRow.getByRole('switch')).toHaveCount(0)
  await managerRow.locator('.el-switch').click()
  await expect.poll(() => writes).toEqual([{ enabled: false, version: 1 }])
})
