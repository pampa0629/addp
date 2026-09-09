import { expect, test } from '@playwright/test'

const authorizationPermissions = [
  'iam.tenant_membership.read',
  'iam.tenant_role.read',
  'iam.tenant_role_assignment.create',
  'iam.tenant_role_assignment.read',
  'iam.tenant_role_assignment.revoke'
]

const members = [
  {
    id: '11',
    principal_id: '1',
    principal_type: 'user',
    display_name: 'E2E Administrator',
    username: 'e2e-admin',
    status: 'active'
  },
  {
    id: '12',
    principal_id: '2',
    principal_type: 'user',
    display_name: 'Alice Researcher',
    username: 'alice',
    status: 'active'
  }
]

const roles = [
  {
    id: '101',
    role_key: 'tenant.administrator',
    name: '租户管理员',
    description: '',
    role_type: 'tenant_builtin',
    allowed_scope_types: ['tenant'],
    allowed_principal_types: ['user'],
    immutable: true,
    permission_keys: ['iam.tenant_role_assignment.create']
  },
  {
    id: '102',
    role_key: 'tenant.data_viewer',
    name: '数据查看者',
    description: '',
    role_type: 'tenant_builtin',
    allowed_scope_types: ['tenant', 'department'],
    allowed_principal_types: ['user'],
    immutable: true,
    permission_keys: ['manager.data_item.read']
  },
  {
    id: '103',
    role_key: 'tenant.data_steward',
    name: '数据管理员',
    description: '',
    role_type: 'tenant_builtin',
    allowed_scope_types: ['tenant'],
    allowed_principal_types: ['user'],
    immutable: true,
    permission_keys: ['manager.data_item.update']
  },
  {
    id: '104',
    role_key: 'tenant.catalog_runtime',
    name: '目录运行账号',
    description: '',
    role_type: 'tenant_builtin',
    allowed_scope_types: ['tenant'],
    allowed_principal_types: ['service_principal'],
    immutable: true,
    permission_keys: ['catalog.entry.read']
  }
]

function assignment(overrides = {}) {
  return {
    id: '501',
    membership_id: '11',
    principal_id: '1',
    principal_type: 'user',
    display_name: 'E2E Administrator',
    username: 'e2e-admin',
    role_id: '101',
    role_key: 'tenant.administrator',
    role_name: '租户管理员',
    role_name_i18n_key: null,
    scope_type: 'tenant',
    department_id: null,
    project_group_id: null,
    status: 'active',
    valid_from: '2026-09-09T06:00:00Z',
    valid_until: null,
    reason: 'tenant bootstrap',
    created_by_principal_id: '1',
    revoked_by_principal_id: null,
    revoked_at: null,
    ...overrides
  }
}

function paginated(data, pageSize = 20) {
  return { data, total: data.length, page: 1, page_size: pageSize, total_pages: 1 }
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

test('tenant administrator filters members and assigns multiple roles in one request', async ({ page }) => {
  let assignments = [
    assignment(),
    assignment({
      id: '502',
      membership_id: '12',
      principal_id: '2',
      display_name: 'Alice Researcher',
      username: 'alice',
      role_id: '101',
      role_key: 'tenant.administrator',
      role_name: '租户管理员',
      reason: 'research access'
    })
  ]
  const mutations = []
  const listQueries = []
  let authContextRequests = 0
  let authenticated = false

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
      if (!authenticated) await fulfillJSON(route, 401, { error: 'authentication_required' })
      else await fulfillJSON(route, 200, { access_token: 'e2e-refreshed-token', expires_in: 300 })
      return
    }
    if (path.endsWith('/login') && method === 'POST') {
      authenticated = true
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
    if (path.endsWith('/auth/context-options')) {
      await fulfillJSON(route, 200, { contexts: [{
        type: 'tenant', tenant_id: '1', tenant_membership_id: '11',
        tenant_name: 'E2E Tenant', tenant_code: 'e2e', current: true, requires_step_up: false
      }] })
      return
    }
    if (path.endsWith('/auth/context')) {
      authContextRequests += 1
      await fulfillJSON(route, 200, {
        principal: { id: '1', principal_type: 'user' },
        context: { type: 'tenant', tenant_id: '1', tenant_membership_id: '11' },
        authentication: { assurance_level: 'aal2' },
        authorization: {
          role_assignments: [{
            role_key: 'tenant.administrator',
            scope: { type: 'tenant' },
            permissions: authorizationPermissions
          }]
        }
      })
      return
    }
    if (path.endsWith('/tenant/memberships') && method === 'GET') {
      expect(url.searchParams.get('principal_type')).toBe('user')
      await fulfillJSON(route, 200, paginated(members, 100))
      return
    }
    if (path.endsWith('/tenant/roles') && method === 'GET') {
      await fulfillJSON(route, 200, roles)
      return
    }
    if (path.endsWith('/tenant/role_assignments') && method === 'GET') {
      const query = Object.fromEntries(url.searchParams.entries())
      listQueries.push(query)
      let rows = assignments
      if (query.membership_id) rows = rows.filter((item) => item.membership_id === query.membership_id)
      if (query.status) rows = rows.filter((item) => item.status === query.status)
      await fulfillJSON(route, 200, paginated(rows, Number(query.page_size || 20)))
      return
    }
    if (path.endsWith('/tenant/role_assignments') && method === 'POST') {
      const input = request.postDataJSON()
      mutations.push({ action: 'create', input })
      const selectedMember = members.find((member) => member.id === input.membership_id)
      const created = input.role_ids.map((roleID, index) => {
        const selectedRole = roles.find((role) => role.id === roleID)
        return assignment({
          id: String(601 + index),
          membership_id: selectedMember.id,
          principal_id: selectedMember.principal_id,
          display_name: selectedMember.display_name,
          username: selectedMember.username,
          role_id: selectedRole.id,
          role_key: selectedRole.role_key,
          role_name: selectedRole.name,
          reason: input.reason
        })
      })
      assignments = [...assignments, ...created]
      await fulfillJSON(route, 201, created)
      return
    }
    if (path.endsWith('/tenant/role_assignments/601/revoke') && method === 'POST') {
      const input = request.postDataJSON()
      mutations.push({ action: 'revoke', input })
      assignments = assignments.map((item) => item.id === '601'
        ? { ...item, status: 'revoked', revoked_by_principal_id: '1', revoked_at: '2026-09-09T07:00:00Z' }
        : item)
      await fulfillJSON(route, 200, assignments.find((item) => item.id === '601'))
      return
    }

    throw new Error(`unexpected IAM E2E request: ${method} ${path}`)
  })

  await page.goto('/login?redirect=%2Fiam%2Froles%3Ftab%3Drole-assignments')
  await page.locator('input[autocomplete="username"]').fill('e2e-admin')
  await page.locator('input[autocomplete="current-password"]').fill('not-transmitted')
  await page.locator('button.auth-login-primary').click()

  await expect(page).toHaveURL(/\/iam\/roles\?tab=role-assignments$/)
  await expect(page.getByRole('heading', { name: '角色管理' })).toBeVisible()
  await expect(page.getByRole('tab', { name: '角色分配' })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('row').filter({ hasText: 'E2E Administrator' })).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'Alice Researcher' })).toBeVisible()

  await page.locator('.iam-toolbar .iam-member-select__member').click()
  await page.locator('.el-select-dropdown__item:visible').filter({ hasText: 'Alice Researcher' }).click()
  await expect(page.getByRole('row').filter({ hasText: 'Alice Researcher' })).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'E2E Administrator' })).toHaveCount(0)

  await page.getByRole('button', { name: '当前账号', exact: true }).click()
  await expect(page.getByRole('row').filter({ hasText: 'E2E Administrator' })).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'Alice Researcher' })).toHaveCount(0)
  expect(listQueries.some((query) => query.membership_id === '11' && query.principal_type === 'user')).toBe(true)

  await page.getByRole('button', { name: '当前账号', exact: true }).click()
  await expect(page.getByRole('row').filter({ hasText: 'Alice Researcher' })).toBeVisible()

  await page.getByRole('button', { name: '分配角色', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '分配角色' })
  await expect(dialog).toContainText('E2E Administrator')
  await expect(dialog).toContainText('当前账号')

  const memberCombobox = dialog.getByRole('combobox', { name: /成员/ })
  const memberListboxID = await memberCombobox.getAttribute('aria-controls')
  await memberCombobox.click({ force: true })
  const memberListbox = page.locator(`[id="${memberListboxID}"]`)
  await memberListbox.getByRole('option', { name: /Alice Researcher/ }).click()
  await expect(dialog).toContainText('Alice Researcher')
  const roleCombobox = dialog.getByRole('combobox', { name: /角色/ })
  const roleListboxID = await roleCombobox.getAttribute('aria-controls')
  await expect(roleCombobox).toBeEnabled()
  await roleCombobox.press('ArrowDown')
  const roleListbox = page.locator(`[id="${roleListboxID}"]`)
  await expect(roleListbox.getByRole('option', { name: /目录运行账号/ })).toHaveCount(0)
  await roleListbox.getByRole('option', { name: /数据查看者/ }).click()
  await roleListbox.getByRole('option', { name: /数据管理员/ }).click()
  await expect(dialog.getByText('已选择 2 个角色', { exact: true })).toBeVisible()
  await dialog.locator('textarea').fill('E2E batch assignment')
  await dialog.getByRole('button', { name: '确认分配', exact: true }).click()

  const viewerRow = page.getByRole('row').filter({ hasText: '数据查看者' }).filter({ hasText: 'Alice Researcher' })
  const stewardRow = page.getByRole('row').filter({ hasText: '数据管理员' }).filter({ hasText: 'Alice Researcher' })
  await expect(viewerRow).toBeVisible()
  await expect(stewardRow).toBeVisible()
  expect(authContextRequests).toBeGreaterThanOrEqual(1)

  await viewerRow.getByRole('button', { name: '撤销', exact: true }).click()
  const prompt = page.getByRole('dialog', { name: '撤销' })
  await prompt.getByRole('textbox').fill('E2E revoke')
  await prompt.getByRole('button', { name: '确认', exact: true }).click()
  await expect(viewerRow).toContainText('已撤销')
  await expect(viewerRow.getByRole('button', { name: '撤销', exact: true })).toHaveCount(0)

  expect(mutations).toEqual([
    {
      action: 'create',
      input: {
        membership_id: '12',
        role_ids: ['102', '103'],
        scope_type: 'tenant',
        department_id: null,
        project_group_id: null,
        valid_until: null,
        reason: 'E2E batch assignment'
      }
    },
    { action: 'revoke', input: { reason: 'E2E revoke' } }
  ])
})

test('high-risk self assignment completes MFA step-up and retries the original request', async ({ page }) => {
  const currentAssignments = [assignment()]
  const assignmentAttempts = []
  const mfaRequests = []
  let authenticated = false
  let steppedUp = false
  let authContextRequests = 0

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
      if (!authenticated) await fulfillJSON(route, 401, { error: 'authentication_required' })
      else await fulfillJSON(route, 200, { access_token: 'e2e-refreshed-token', expires_in: 300 })
      return
    }
    if (path.endsWith('/login') && method === 'POST') {
      authenticated = true
      await fulfillJSON(route, 200, {
        next_action: 'session_issued',
        session: { access_token: 'e2e-aal1-token', expires_in: 300 }
      })
      return
    }
    if (path.endsWith('/users/me')) {
      await fulfillJSON(route, 200, { id: '1', username: 'e2e-admin', display_name: 'E2E Administrator' })
      return
    }
    if (path.endsWith('/auth/context-options')) {
      await fulfillJSON(route, 200, { contexts: [{
        type: 'tenant', tenant_id: '1', tenant_membership_id: '11',
        tenant_name: 'E2E Tenant', tenant_code: 'e2e', current: true, requires_step_up: false
      }] })
      return
    }
    if (path.endsWith('/auth/context')) {
      authContextRequests += 1
      await fulfillJSON(route, 200, {
        principal: { id: '1', principal_type: 'user' },
        context: { type: 'tenant', tenant_id: '1', tenant_membership_id: '11' },
        authentication: { assurance_level: steppedUp ? 'aal2' : 'aal1' },
        authorization: {
          role_assignments: [{
            role_key: 'tenant.administrator',
            scope: { type: 'tenant' },
            permissions: authorizationPermissions
          }]
        }
      })
      return
    }
    if (path.endsWith('/tenant/memberships') && method === 'GET') {
      await fulfillJSON(route, 200, paginated([members[0]], 100))
      return
    }
    if (path.endsWith('/tenant/roles') && method === 'GET') {
      await fulfillJSON(route, 200, roles)
      return
    }
    if (path.endsWith('/tenant/role_assignments') && method === 'GET') {
      const membershipID = url.searchParams.get('membership_id')
      const status = url.searchParams.get('status')
      const rows = currentAssignments.filter((item) =>
        (!membershipID || item.membership_id === membershipID) && (!status || item.status === status)
      )
      await fulfillJSON(route, 200, paginated(rows, Number(url.searchParams.get('page_size') || 20)))
      return
    }
    if (path.endsWith('/tenant/role_assignments') && method === 'POST') {
      const input = request.postDataJSON()
      assignmentAttempts.push(input)
      if (!steppedUp) {
        await fulfillJSON(route, 403, { error: '需要增强认证', error_code: 'step_up_required' })
        return
      }
      const created = assignment({
        id: '701',
        role_id: '103',
        role_key: 'tenant.data_steward',
        role_name: '数据管理员',
        reason: input.reason
      })
      currentAssignments.push(created)
      await fulfillJSON(route, 201, [created])
      return
    }
    if (path.endsWith('/auth/mfa/step-up-challenges') && method === 'POST') {
      mfaRequests.push({ action: 'challenge' })
      await fulfillJSON(route, 201, {
        challenge_token: 'addp_mfc_browser_e2e',
        method: 'totp',
        expires_at: '2026-09-09T08:00:00Z'
      })
      return
    }
    if (path.endsWith('/auth/mfa/step-up-verifications') && method === 'POST') {
      const input = request.postDataJSON()
      mfaRequests.push({ action: 'verify', input })
      steppedUp = true
      await fulfillJSON(route, 200, { access_token: 'e2e-aal2-token', expires_in: 300 })
      return
    }

    throw new Error(`unexpected IAM MFA E2E request: ${method} ${path}`)
  })

  await page.goto('/login?redirect=%2Fiam%2Froles%3Ftab%3Drole-assignments')
  await page.locator('input[autocomplete="username"]').fill('e2e-admin')
  await page.locator('input[autocomplete="current-password"]').fill('not-transmitted')
  await page.locator('button.auth-login-primary').click()

  await expect(page).toHaveURL(/\/iam\/roles\?tab=role-assignments$/)
  await expect(page.getByText('当前会话：基础认证', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '分配角色', exact: true }).click()

  const assignmentDialog = page.getByRole('dialog', { name: '分配角色' })
  const roleCombobox = assignmentDialog.getByRole('combobox', { name: /角色/ })
  const roleListboxID = await roleCombobox.getAttribute('aria-controls')
  await expect(roleCombobox).toBeEnabled()
  await roleCombobox.press('ArrowDown')
  await page.locator(`[id="${roleListboxID}"]`).getByRole('option', { name: /数据管理员/ }).click()
  await assignmentDialog.locator('textarea').fill('E2E high-risk self assignment')
  await assignmentDialog.getByRole('button', { name: '确认分配', exact: true }).click()

  const stepUpDialog = page.getByRole('dialog', { name: '确认高风险操作' })
  await expect(stepUpDialog).toBeVisible()
  await stepUpDialog.locator('input[autocomplete="one-time-code"]').fill('654321')
  await stepUpDialog.getByRole('button', { name: '验证并继续', exact: true }).click()

  await expect.poll(() => assignmentAttempts.length).toBe(2)
  await expect.poll(() => authContextRequests).toBeGreaterThanOrEqual(2)
  expect(assignmentAttempts[1]).toEqual(assignmentAttempts[0])
  expect(assignmentAttempts[0]).toEqual({
    membership_id: '11',
    role_ids: ['103'],
    scope_type: 'tenant',
    department_id: null,
    project_group_id: null,
    valid_until: null,
    reason: 'E2E high-risk self assignment'
  })
  expect(mfaRequests).toEqual([
    { action: 'challenge' },
    {
      action: 'verify',
      input: { challenge_token: 'addp_mfc_browser_e2e', code: '654321' }
    }
  ])
})
