import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { expect, test } from '@playwright/test'

test.use({ trace: 'retain-on-failure' })

const permissions = [
  'iam.department.read',
  'iam.project_group.read',
  'iam.project_group_membership.read',
  'iam.project_group_membership.create',
  'iam.project_group_membership.update',
  'iam.project_group_membership.close',
  'iam.tenant_membership.read'
]

async function fulfillJSON(route, status, body) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    headers: {
      'access-control-allow-origin': browserTestOrigin('system'),
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,POST,PUT,OPTIONS'
    },
    body: JSON.stringify(body)
  })
}

for (const member of [
  { id: '11', principalId: '1', name: 'E2E Administrator', username: 'e2e-admin', self: true },
  { id: '12', principalId: '2', name: 'E2E Member', username: 'e2e-member', self: false },
  { id: '11', principalId: '1', name: 'E2E Administrator', username: 'e2e-admin', self: true, failRefresh: true }
]) {
  test(member.failRefresh ? 'a refresh failure after saving does not report a failed write' :
    `changing ${member.self ? 'the current user' : 'another user'} memberships avoids unauthorized reads`, async ({ page }) => {
    let authenticated = false
    let membership = null
    let createCount = 0
    let staleReadCount = 0
    let postSaveReadCount = 0
    let refreshCount = 0
    let updateCount = 0
    let closeCount = 0
    let currentToken = 'e2e-original-token'
    let authorizationInvalidated = false

    await page.route('**/api/v1/system/**', async (route) => {
      const request = route.request()
      const path = new URL(request.url()).pathname
      const method = request.method()

      if (method === 'OPTIONS') return fulfillJSON(route, 204, {})
      if (path.endsWith('/refresh')) {
        if (!authenticated) return fulfillJSON(route, 401, { error: 'authentication_required' })
        refreshCount += 1
        if (member.failRefresh) return fulfillJSON(route, 503, { error: 'Refresh unavailable' })
        currentToken = `e2e-refreshed-token-${refreshCount}`
        authorizationInvalidated = false
        return fulfillJSON(route, 200, { access_token: currentToken, expires_in: 300 })
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
          data: [{ id: member.id, principal_id: member.principalId, principal_type: 'user', display_name: member.name, username: member.username, status: 'active' }],
          total: 1, page: 1, page_size: 100, total_pages: 1
        })
      }
      if (path.endsWith('/tenant/project_groups/401/memberships') && method === 'POST') {
        expect(request.headers().authorization).toBe('Bearer e2e-original-token')
        expect(request.postDataJSON()).toEqual({ tenant_membership_id: member.id, relation_role: 'member' })
        createCount += 1
        membership = {
          id: '501', tenant_membership_id: member.id, principal_id: member.principalId,
          display_name: member.name, username: member.username,
          project_group_role: 'member', status: 'active', version: 1
        }
        authorizationInvalidated = member.self
        return fulfillJSON(route, 201, membership)
      }
      if (path.endsWith('/tenant/project_groups/401/memberships/501') && method === 'PUT') {
        expect(request.headers().authorization).toBe(`Bearer ${currentToken}`)
        expect(request.postDataJSON()).toEqual({ relation_role: 'leader', version: 1 })
        updateCount += 1
        membership = { ...membership, project_group_role: 'leader', version: 2 }
        authorizationInvalidated = member.self
        return fulfillJSON(route, 200, membership)
      }
      if (path.endsWith('/tenant/project_groups/401/memberships/501/close') && method === 'POST') {
        expect(request.headers().authorization).toBe(`Bearer ${currentToken}`)
        expect(request.postDataJSON()).toEqual({ version: 2, reason: 'E2E membership completed' })
        closeCount += 1
        membership = { ...membership, status: 'ended', version: 3 }
        authorizationInvalidated = member.self
        return fulfillJSON(route, 200, membership)
      }
      if (path.endsWith('/tenant/project_groups/401/memberships') && method === 'GET') {
        const token = request.headers().authorization
        if (authorizationInvalidated) {
          staleReadCount += 1
          return fulfillJSON(route, 401, { error: 'authentication_required' })
        }
        if (membership) {
          expect(token).toBe(`Bearer ${currentToken}`)
          postSaveReadCount += 1
        }
        return fulfillJSON(route, 200, {
          data: membership?.status === 'active' ? [membership] : [],
          total: membership?.status === 'active' ? 1 : 0,
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
    const memberCombobox = createDialog.getByRole('combobox', { name: '租户成员' })
    await memberCombobox.press('ArrowDown')
    await expect(memberCombobox).toHaveAttribute('aria-expanded', 'true')
    await page.getByRole('option', { name: new RegExp(member.name) }).click()
    await createDialog.getByRole('button', { name: '保存' }).click()

    await expect(createDialog).not.toBeVisible()
    await expect.poll(() => ({ createCount, staleReadCount, postSaveReadCount, refreshCount })).toEqual({
      createCount: 1, staleReadCount: 0, postSaveReadCount: member.failRefresh ? 0 : 1, refreshCount: member.self ? 1 : 0
    })
    await expect(page).toHaveURL(/\/iam\/organization\?tab=project-groups$/)
    await expect(page.getByText('保存失败')).toHaveCount(0)
    if (member.failRefresh) {
      await expect(page.getByText('Refresh unavailable', { exact: true })).toBeVisible()
      expect(membership?.status).toBe('active')
      return
    }
    await expect(membersDialog).toBeVisible()
    const memberRow = membersDialog.getByRole('row').filter({ hasText: member.name })
    await expect(memberRow).toBeVisible()
    await memberRow.getByRole('button', { name: '编辑' }).click()
    const editDialog = page.getByRole('dialog', { name: '编辑', exact: true })
    await expect(editDialog).toBeVisible()
    const relationRoleCombobox = editDialog.getByRole('combobox', { name: '组织角色' })
    await relationRoleCombobox.press('ArrowDown')
    await expect(relationRoleCombobox).toHaveAttribute('aria-expanded', 'true')
    await page.getByRole('option', { name: '负责人', exact: true }).click()
    await relationRoleCombobox.press('Escape')
    await editDialog.getByRole('button', { name: '保存' }).click()
    await expect.poll(() => ({ updateCount, staleReadCount, postSaveReadCount, refreshCount })).toEqual({
      updateCount: 1, staleReadCount: 0, postSaveReadCount: 2, refreshCount: member.self ? 2 : 0
    })
    await expect(memberRow.getByRole('cell', { name: '负责人', exact: true })).toBeVisible()
    await memberRow.getByRole('button', { name: '结束', exact: true }).click()
    const closeDialog = page.getByRole('dialog', { name: '结束成员关系', exact: true })
    await closeDialog.locator('input').fill('E2E membership completed')
    await closeDialog.getByRole('button', { name: '确认', exact: true }).click()
    await expect.poll(() => ({ closeCount, staleReadCount, postSaveReadCount, refreshCount })).toEqual({
      closeCount: 1, staleReadCount: 0, postSaveReadCount: 3, refreshCount: member.self ? 3 : 0
    })
    await expect(membersDialog).toBeVisible()
    await expect(memberRow).toHaveCount(0)
    await expect(page).toHaveURL(/\/iam\/organization\?tab=project-groups$/)
  })
}
