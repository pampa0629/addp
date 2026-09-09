import { expect, test } from '@playwright/test'

const authorizationPermissions = [
  'iam.tenant_role.create',
  'iam.tenant_role.delete',
  'iam.tenant_role.read',
  'iam.tenant_role.update'
]

const assignablePermissions = [
  {
    permission_key: 'agent.run.read',
    owner_module: 'agent',
    action: 'read',
    risk_level: 'low',
    allowed_scope_types: ['tenant', 'department'],
    name_i18n_key: '',
    description_i18n_key: ''
  },
  {
    permission_key: 'agent.run.execute',
    owner_module: 'agent',
    action: 'execute',
    risk_level: 'high',
    allowed_scope_types: ['tenant'],
    name_i18n_key: '',
    description_i18n_key: ''
  },
  {
    permission_key: 'manager.data_item.read',
    owner_module: 'manager',
    action: 'read',
    risk_level: 'low',
    allowed_scope_types: ['tenant', 'department'],
    name_i18n_key: '',
    description_i18n_key: ''
  },
  {
    permission_key: 'manager.data_item.update',
    owner_module: 'manager',
    action: 'update',
    risk_level: 'medium',
    allowed_scope_types: ['tenant'],
    name_i18n_key: '',
    description_i18n_key: ''
  }
]

function tenantAdministratorRole() {
  return {
    id: '1',
    role_key: 'tenant.administrator',
    name: null,
    description: null,
    name_i18n_key: 'system.iam.roleCatalog.tenant.administrator.name',
    description_i18n_key: 'system.iam.roleCatalog.tenant.administrator.description',
    role_type: 'tenant_builtin',
    allowed_scope_types: ['tenant'],
    allowed_principal_types: ['user'],
    immutable: true,
    permission_keys: authorizationPermissions
  }
}

function tenantRuntimeRole() {
  return {
    id: '2',
    role_key: 'tenant.agent_runtime',
    name: 'Agent 运行账号',
    description: '供 Agent 机器身份运行任务。',
    name_i18n_key: null,
    description_i18n_key: null,
    role_type: 'tenant_builtin',
    allowed_scope_types: ['tenant'],
    allowed_principal_types: ['service_principal'],
    immutable: true,
    permission_keys: ['agent.run.execute']
  }
}

function customRole(input, overrides = {}) {
  return {
    id: '700',
    role_key: input.role_key,
    name: input.name,
    description: input.description,
    name_i18n_key: null,
    description_i18n_key: null,
    role_type: 'tenant_custom',
    allowed_scope_types: input.scope_types,
    allowed_principal_types: ['user'],
    immutable: false,
    permission_keys: input.permission_keys,
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

test('tenant administrator manages a custom role with localized bulk permission selection', async ({ page }) => {
  let role = null
  const mutations = []

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
      await fulfillJSON(route, 200, {
        principal: { id: '1', principal_type: 'user' },
        context: { type: 'tenant', tenant_id: '1', tenant_membership_id: '11' },
        authentication: { assurance_level: 'aal2' },
        authorization: {
          role_assignments: [{ role_key: 'tenant.administrator', scope_type: 'tenant', permissions: authorizationPermissions }]
        }
      })
      return
    }
    if (path.endsWith('/tenant/roles') && method === 'GET') {
      await fulfillJSON(route, 200, [tenantAdministratorRole(), tenantRuntimeRole(), ...(role ? [role] : [])])
      return
    }
    if (path.endsWith('/tenant/role_permissions') && method === 'GET') {
      await fulfillJSON(route, 200, assignablePermissions)
      return
    }
    if (path.endsWith('/tenant/roles') && method === 'POST') {
      const input = request.postDataJSON()
      mutations.push({ action: 'create', input })
      role = customRole(input)
      await fulfillJSON(route, 201, role)
      return
    }
    if (path.endsWith('/tenant/roles/700') && method === 'PUT') {
      const input = request.postDataJSON()
      mutations.push({ action: 'update', input })
      role = customRole(input)
      await fulfillJSON(route, 200, role)
      return
    }
    if (path.endsWith('/tenant/roles/700') && method === 'DELETE') {
      const input = request.postDataJSON()
      mutations.push({ action: 'delete', input })
      role = null
      await route.fulfill({ status: 204 })
      return
    }

    throw new Error(`unexpected IAM E2E request: ${method} ${path}`)
  })

  await page.goto('/login?redirect=/iam/roles')
  await page.locator('input[autocomplete="username"]').fill('e2e-admin')
  await page.locator('input[autocomplete="current-password"]').fill('not-transmitted')
  await page.locator('button.auth-login-primary').click()

  await expect(page).toHaveURL(/\/iam\/roles$/)
  await expect(page.getByRole('heading', { name: '角色管理' })).toBeVisible()
  await expect(page.getByRole('tab', { name: '角色定义' })).toBeVisible()
  await expect(page.getByRole('radio', { name: '用户账号角色 1' })).toBeChecked()
  await expect(page.getByRole('row').filter({ hasText: 'tenant.administrator' })).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'tenant.agent_runtime' })).toHaveCount(0)

  await page.locator('.el-radio-button').filter({ hasText: '机器身份角色' }).click()
  await expect(page.getByText('角色分配统一从“应用接入 > 机器身份”进入。')).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'tenant.agent_runtime' })).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'tenant.administrator' })).toHaveCount(0)
  await page.locator('.el-radio-button').filter({ hasText: '用户账号角色' }).click()

  await page.getByRole('button', { name: '创建自定义角色', exact: true }).click()
  let dialog = page.getByRole('dialog', { name: '创建自定义角色' })
  await dialog.getByRole('textbox').nth(0).fill('tenant.e2e_researcher')
  await dialog.getByRole('textbox').nth(1).fill('E2E 研究员')
  await dialog.getByRole('textbox').nth(2).fill('端到端测试角色')

  const permissionSearch = dialog.getByPlaceholder('搜索权限名称、标识、模块或动作')
  await permissionSearch.fill('运行任务')
  await expect(dialog.getByText('运行任务', { exact: true }).first()).toBeVisible()
  await expect(dialog.getByText('数据项', { exact: true })).toHaveCount(0)
  await dialog.getByRole('button', { name: '选择当前结果', exact: true }).click()
  await expect(dialog.getByText('已选择 2 项权限', { exact: true })).toBeVisible()

  await dialog.locator('.el-checkbox').filter({ hasText: '部门' }).click()
  await expect(dialog.getByText('有 1 项已选权限不支持当前允许范围', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: '移除不兼容权限', exact: true }).click()
  await expect(dialog.getByText('已选择 1 项权限', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: '保存', exact: true }).click()

  let row = page.getByRole('row').filter({ hasText: 'E2E 研究员' })
  await expect(row).toContainText('tenant.e2e_researcher')
  await expect(row).toContainText('租户自定义角色')
  await expect(row).toContainText('租户')
  await expect(row).toContainText('部门')
  await expect(row).toContainText('用户账号')

  await row.getByRole('button', { name: /1 项/ }).click()
  const drawer = page.getByRole('dialog', { name: '角色权限明细' })
  await expect(drawer).toContainText('智能体 (1)')
  await expect(drawer).toContainText('运行任务')
  await expect(drawer).toContainText('agent.run.read')
  await drawer.locator('.el-drawer__close-btn').click()

  await row.getByRole('button', { name: '编辑', exact: true }).click()
  dialog = page.getByRole('dialog', { name: '编辑自定义角色' })
  await dialog.getByRole('textbox').nth(0).fill('E2E 高级研究员')
  await dialog.getByRole('textbox').nth(1).fill('可查看智能体任务和数据项')
  await dialog.getByPlaceholder('搜索权限名称、标识、模块或动作').fill('数据项')
  await expect(dialog.getByText('数据项', { exact: true }).first()).toBeVisible()
  await dialog.getByRole('button', { name: '选择当前结果', exact: true }).click()
  await expect(dialog.getByText('已选择 2 项权限', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: '保存', exact: true }).click()

  row = page.getByRole('row').filter({ hasText: 'E2E 高级研究员' })
  await expect(row).toContainText('可查看智能体任务和数据项')
  await row.getByRole('button', { name: /2 项/ }).click()
  const updatedDrawer = page.getByRole('dialog', { name: '角色权限明细' })
  await expect(updatedDrawer).toContainText('智能体 (1)')
  await expect(updatedDrawer).toContainText('数据管理 (1)')
  await expect(updatedDrawer).toContainText('manager.data_item.read')
  await updatedDrawer.locator('.el-drawer__close-btn').click()

  await row.getByRole('button', { name: '删除', exact: true }).click()
  const prompt = page.getByRole('dialog', { name: '删除' })
  await prompt.getByRole('textbox').fill('E2E cleanup')
  await prompt.getByRole('button', { name: '确认', exact: true }).click()
  await expect(page.getByRole('row').filter({ hasText: 'E2E 高级研究员' })).toHaveCount(0)

  expect(mutations).toEqual([
    {
      action: 'create',
      input: {
        role_key: 'tenant.e2e_researcher',
        name: 'E2E 研究员',
        description: '端到端测试角色',
        scope_types: ['tenant', 'department'],
        permission_keys: ['agent.run.read']
      }
    },
    {
      action: 'update',
      input: {
        role_key: 'tenant.e2e_researcher',
        name: 'E2E 高级研究员',
        description: '可查看智能体任务和数据项',
        scope_types: ['tenant', 'department'],
        permission_keys: ['agent.run.read', 'manager.data_item.read']
      }
    },
    { action: 'delete', input: { reason: 'E2E cleanup' } }
  ])
})
