import { expect, test } from '@playwright/test'

const initialSecret = 'e2e-initial-client-secret'
const rotatedSecret = 'e2e-rotated-client-secret'
const clientID = 'addp_svc_browser_e2e'
const permissions = [
  'iam.service_account.create',
  'iam.service_account.read',
  'iam.service_account.restore',
  'iam.service_account.suspend',
  'iam.service_account.update',
  'iam.service_credential.update',
  'iam.oauth_client.read'
]

function paginated(data) {
  return { data, total: data.length, page: 1, page_size: 20, total_pages: 1 }
}

function serviceAccount(overrides = {}) {
  return {
    id: '101',
    name: 'E2E Nightly Loader',
    description: 'Disposable browser test account',
    status: 'active',
    membership_id: '201',
    membership_status: 'active',
    client_id: clientID,
    credential_status: 'active',
    version: 1,
    created_by_principal_id: '1',
    created_at: '2026-09-08T12:00:00Z',
    updated_at: '2026-09-08T12:00:00Z',
    ...overrides
  }
}

async function fulfillJSON(route, status, body) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    headers: {
      'access-control-allow-origin': 'http://127.0.0.1:4173',
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,POST,PUT,DELETE,OPTIONS'
    },
    body: JSON.stringify(body)
  })
}

test('tenant administrator manages a service account lifecycle without leaking secrets', async ({ page }) => {
  let account = null
  const mutations = []

  await page.route('**/api/v1/system/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
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
        session: { access_token: 'e2e-access-token', expires_in: 300 }
      })
      return
    }
    if (path.endsWith('/users/me')) {
      await fulfillJSON(route, 200, { id: '1', username: 'e2e-admin', display_name: 'E2E Administrator' })
      return
    }
    if (path.endsWith('/auth/context')) {
      await fulfillJSON(route, 200, {
        principal: { id: '1', principal_type: 'user' },
        context: { type: 'tenant', tenant_id: '1', tenant_membership_id: '11' },
        authentication: { assurance_level: 'aal2' },
        authorization: {
          role_assignments: [{ role_key: 'tenant.administrator', scope_type: 'tenant', permissions }]
        }
      })
      return
    }
    if (path.endsWith('/tenant/service_accounts') && method === 'GET') {
      await fulfillJSON(route, 200, paginated(account ? [account] : []))
      return
    }
    if (path.endsWith('/tenant/service_accounts') && method === 'POST') {
      const input = request.postDataJSON()
      mutations.push({ action: 'create', input })
      account = serviceAccount({ name: input.name, description: input.description })
      await fulfillJSON(route, 201, { ...account, client_secret: initialSecret })
      return
    }
    if (path.endsWith('/tenant/service_accounts/101/suspend') && method === 'POST') {
      const input = request.postDataJSON()
      mutations.push({ action: 'suspend', input })
      account = serviceAccount({ ...account, status: 'suspended', membership_status: 'suspended', credential_status: 'disabled', version: 2 })
      await fulfillJSON(route, 200, account)
      return
    }
    if (path.endsWith('/tenant/service_accounts/101/restore') && method === 'POST') {
      const input = request.postDataJSON()
      mutations.push({ action: 'restore', input })
      account = serviceAccount({ ...account, status: 'active', membership_status: 'active', credential_status: 'active', version: 3 })
      await fulfillJSON(route, 200, account)
      return
    }
    if (path.endsWith('/tenant/service_accounts/101/rotate-secret') && method === 'POST') {
      const input = request.postDataJSON()
      mutations.push({ action: 'rotate-secret', input })
      account = serviceAccount({ ...account, version: 4 })
      await fulfillJSON(route, 200, { ...account, client_secret: rotatedSecret })
      return
    }

    throw new Error(`unexpected IAM E2E request: ${method} ${path}`)
  })

  await page.goto('/login?redirect=/iam/application-access')
  await page.locator('input[autocomplete="username"]').fill('e2e-admin')
  await page.locator('input[autocomplete="current-password"]').fill('not-transmitted')
  await page.locator('button.auth-login-primary').click()

  await expect(page).toHaveURL(/\/iam\/application-access$/)
  await expect(page.getByRole('heading', { name: '应用接入' })).toBeVisible()
  await expect(page.getByRole('tab', { name: '服务账号' })).toBeVisible()
  await expect(page.getByRole('tab', { name: '外部应用（OAuth）' })).toBeVisible()

  await page.getByRole('button', { name: '创建服务账号', exact: true }).click()
  const createDialog = page.getByRole('dialog', { name: '创建服务账号' })
  await createDialog.getByRole('textbox').nth(0).fill('E2E Nightly Loader')
  await createDialog.getByRole('textbox').nth(1).fill('Disposable browser test account')
  await createDialog.getByRole('button', { name: '确认', exact: true }).click()

  const credentialDialog = page.getByRole('dialog', { name: '保存服务账号凭据' })
  await expect(credentialDialog).toContainText(clientID)
  await expect(credentialDialog).toContainText(initialSecret)
  await credentialDialog.getByRole('button', { name: '完成', exact: true }).click()
  await expect(page.getByText(initialSecret, { exact: true })).toHaveCount(0)

  let row = page.getByRole('row').filter({ hasText: 'E2E Nightly Loader' })
  await expect(row).toContainText(clientID)
  await row.getByRole('button', { name: '暂停', exact: true }).click()
  let prompt = page.getByRole('dialog', { name: '暂停' })
  await prompt.getByRole('textbox').fill('scheduled pause')
  await prompt.getByRole('button', { name: '确认', exact: true }).click()
  await expect(row).toContainText('已暂停')
  await expect(row.getByRole('button', { name: '恢复', exact: true })).toBeVisible()

  await row.getByRole('button', { name: '恢复', exact: true }).click()
  prompt = page.getByRole('dialog', { name: '恢复' })
  await prompt.getByRole('textbox').fill('scheduled resume')
  await prompt.getByRole('button', { name: '确认', exact: true }).click()
  await expect(row).toContainText('有效')

  await row.getByRole('button', { name: '轮换密钥', exact: true }).click()
  prompt = page.getByRole('dialog', { name: '轮换密钥' })
  await prompt.getByRole('textbox').fill('scheduled rotation')
  await prompt.getByRole('button', { name: '确认', exact: true }).click()
  await expect(credentialDialog).toContainText(rotatedSecret)
  await credentialDialog.getByRole('button', { name: '完成', exact: true }).click()
  await expect(page.getByText(rotatedSecret, { exact: true })).toHaveCount(0)

  expect(mutations).toEqual([
    { action: 'create', input: { name: 'E2E Nightly Loader', description: 'Disposable browser test account' } },
    { action: 'suspend', input: { version: 1, reason: 'scheduled pause' } },
    { action: 'restore', input: { version: 2, reason: 'scheduled resume' } },
    { action: 'rotate-secret', input: { version: 3, reason: 'scheduled rotation' } }
  ])
})
