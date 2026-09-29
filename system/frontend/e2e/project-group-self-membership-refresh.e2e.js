import { expect, test } from '@playwright/test'

const permissions = [
  'iam.department.read',
  'iam.project_group.read',
  'iam.project_group_membership.read',
  'iam.project_group_membership.create',
  'iam.tenant_membership.read'
]

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

test('adding the current user to a project group refreshes authorization and retries the member read', async ({ page }) => {
  let authenticated = false
  let membership = null
  let createCount = 0
  let staleReadCount = 0
  let retriedReadCount = 0
  let refreshCount = 0

  await page.route('**/api/v1/system/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()

    if (method === 'OPTIONS') return fulfillJSON(route, 204, {})
    if (path.endsWith('/refresh')) {
      if (!authenticated) return fulfillJSON(route, 401, { error: 'authentication_required' })
      refreshCount += 1
      return fulfillJSON(route, 200, { access_token: 'e2e-refreshed-token', expires_in: 300 })
    }
    if (path.endsWith('/login') && method === 'POST') {
      authenticated = true
      return fulfillJSON(route, 200, {
        next_action: 'session_issued',
        session: { access_token: 'e2e-original-token', expires_in: 300 }
      })
    }
    if (path.endsWith('/users/me')) {
      return fulfillJSON(route, 200, { id: '1', username: 'e2e-admin', display_name: 'E2E Administrator' })
    }
    if (path.endsWith('/auth/context-options')) {
      return fulfillJSON(route, 200, { contexts: [{
        type: 'tenant', tenant_id: '1', tenant_membership_id: '11',
        tenant_name: 'E2E Tenant', tenant_code: 'e2e', current: true, requires_step_up: false
      }] })
    }
    if (path.endsWith('/auth/context')) {
      return fulfillJSON(route, 200, {
        principal: { id: '1', principal_type: 'user' },
        context: { type: 'tenant', tenant_id: '1', tenant_membership_id: '11' },
        authentication: { assurance_level: 'aal2' },
        authorization: { role_assignments: [{
          role_key: 'tenant.administrator',
          scope: { type: 'tenant', tenant_id: '1' },
          permissions
        }] }
      })
    }
    if (path.endsWith('/tenant/project_groups') && method === 'GET') {
      return fulfillJSON(route, 200, {
        data: [{ id: '401', code: 'regression_group', name: '回归项目组', status: 'active', version: 1 }],
        total: 1, page: 1, page_size: 20, total_pages: 1
      })
    }
    if (path.endsWith('/tenant/departments') && method === 'GET') {
      return fulfillJSON(route, 200, { data: [], total: 0, page: 1, page_size: 20, total_pages: 1 })
    }
    if (path.endsWith('/tenant/memberships') && method === 'GET') {
      return fulfillJSON(route, 200, {
        data: [{ id: '11', principal_id: '1', principal_type: 'user', display_name: 'E2E Administrator', username: 'e2e-admin', status: 'active' }],
        total: 1, page: 1, page_size: 100, total_pages: 1
      })
    }
    if (path.endsWith('/tenant/project_groups/401/memberships') && method === 'POST') {
      expect(request.headers().authorization).toBe('Bearer e2e-original-token')
      expect(request.postDataJSON()).toEqual({ tenant_membership_id: '11', relation_role: 'member' })
      createCount += 1
      membership = {
        id: '501', tenant_membership_id: '11', principal_id: '1',
        display_name: 'E2E Administrator', username: 'e2e-admin',
        project_group_role: 'member', status: 'active', version: 1
      }
      return fulfillJSON(route, 201, membership)
    }
    if (path.endsWith('/tenant/project_groups/401/memberships') && method === 'GET') {
      const token = request.headers().authorization
      if (membership && token === 'Bearer e2e-original-token') {
        staleReadCount += 1
        return fulfillJSON(route, 401, { error: 'authentication_required' })
      }
      if (membership) {
        expect(token).toBe('Bearer e2e-refreshed-token')
        retriedReadCount += 1
      }
      return fulfillJSON(route, 200, {
        data: membership ? [membership] : [],
        total: membership ? 1 : 0,
        page: 1, page_size: 20, total_pages: 1
      })
    }

    throw new Error(`unexpected project group E2E request: ${method} ${path}`)
  })

  await page.goto('/login?redirect=%2Fiam%2Forganization%3Ftab%3Dproject-groups')
  await page.locator('input[autocomplete="username"]').fill('e2e-admin')
  await page.locator('input[autocomplete="current-password"]').fill('not-transmitted')
  await page.locator('button.auth-login-primary').click()

  await expect(page.getByRole('tab', { name: '项目组管理' })).toHaveAttribute('aria-selected', 'true')
  await expect(page).toHaveURL(/\/iam\/organization\?tab=project-groups$/)
  await page.getByRole('row').filter({ hasText: '回归项目组' }).getByRole('button', { name: '成员管理' }).click()
  const membersDialog = page.getByRole('dialog', { name: '成员管理 · 回归项目组' })
  await expect(membersDialog).toBeVisible()
  await membersDialog.getByRole('button', { name: '添加成员' }).click()

  const createDialog = page.getByRole('dialog', { name: '添加成员' })
  await createDialog.locator('.el-form-item').first().locator('.el-select').click()
  await page.getByRole('option', { name: /E2E Administrator/ }).click()
  await createDialog.getByRole('button', { name: '保存' }).click()

  await expect(createDialog).not.toBeVisible()
  await expect.poll(() => ({ createCount, staleReadCount, retriedReadCount, refreshCount })).toEqual({
    createCount: 1, staleReadCount: 1, retriedReadCount: 2, refreshCount: 1
  })
  await expect(page).toHaveURL(/\/iam\/organization\?tab=project-groups$/)
  await expect(page.getByText('保存失败')).toHaveCount(0)
  await expect(membersDialog).toBeVisible()
  await expect(membersDialog.getByRole('row').filter({ hasText: 'E2E Administrator' })).toBeVisible()
})
