import { expect, test } from '@playwright/test'

async function openAsTenant(page, path, permissions, { configurationEntries } = {}) {
  const businessRequests = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const pathname = new URL(route.request().url()).pathname
    if (pathname === '/api/v1/system/refresh') {
      return route.fulfill({ json: { access_token: 'minimal-permissions-token', expires_in: 300 } })
    }
    if (pathname === '/api/v1/system/users/me') {
      return route.fulfill({ json: { id: 32, username: 'minimal-permissions-user' } })
    }
    if (pathname === '/api/v1/system/auth/context') {
      return route.fulfill({ json: {
        context: { type: 'tenant', tenant_id: '3' },
        authorization: { role_assignments: [{
          scope: { type: 'tenant', tenant_id: '3' }, permissions
        }] }
      } })
    }
    if (pathname === '/api/v1/system/configuration-management/entries' && configurationEntries) {
      return route.fulfill({ json: configurationEntries })
    }
    businessRequests.push(pathname)
    return route.fulfill({ status: 403, json: { error: 'unexpected_business_request' } })
  })
  await page.route(/^http:\/\/127\.0\.0\.1:(?!4170)\d+\//, route =>
    route.fulfill({ contentType: 'text/html', body: '<title>Module fixture</title>' }))
  await page.goto(path)
  return businessRequests
}

async function expectDenied(page, path) {
  await page.goto(path)
  await expect(page.locator('.el-result')).toContainText('无权访问')
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
}

const independentlyReadablePages = [
  ['/ontology/ontologies', ['ontology.revision.read']],
  ['/transfer/tasks', ['transfer.task.read']],
  ['/security/classification-grading', ['security.classification.read', 'security.grade.read']],
  ['/standard/domains', ['standard.domain.read']],
  ['/modeling/entities', ['model.entity.read', 'standard.domain.read']],
  ['/quality/plans', ['quality.plan.read']],
  ['/develop/sql', ['develop.task.read', 'meta.catalog.read']],
  ['/manager/data-retrieval', ['manager.search.execute', 'manager.data_item.read']],
  ['/service/query-services', ['service.definition.read']],
  ['/workbench/applications', ['workbench.data_application.read']],
  ['/orchestrator/orchestrations', ['orchestrator.workflow.read']],
  ['/catalog/entries', ['catalog.entry.read']],
  ['/asset/assets', ['asset.management.read', 'asset.entry.read', 'asset.category.read']],
  ['/agent', ['agent.session.read']],
  ['/graph/ontologies', ['graph.ontology.read']],
  ['/inference/settings/models', ['inference.provider.read', 'inference.deployment.read', 'inference.profile.read']]
]

for (const [path, permissions] of independentlyReadablePages) {
  test(`${path} opens with its minimal page permissions`, async ({ page }) => {
    const businessRequests = await openAsTenant(page, path, permissions)
    await expect(page.locator('iframe.module-iframe'), path).toHaveCount(1)
    await expect(page.locator('.el-result'), path).toHaveCount(0)
    if (path === '/workbench/applications') {
      const moduleUrl = new URL(await page.locator('iframe.module-iframe').getAttribute('src'))
      expect(moduleUrl.origin).not.toBe(new URL(page.url()).origin)
      expect(moduleUrl.pathname).toBe('/module-ui/workbench/applications')
    }
    expect(businessRequests).toEqual([])
  })
}

test('retrieval-only Manager account opens its permitted page from the Console card', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/', ['manager.search.execute', 'manager.data_item.read'])
  await page.getByText('所有模块', { exact: true }).click()
  const managerCard = page.locator('.module-card').filter({ has: page.getByRole('heading', { name: '数据管理' }) })
  await expect(managerCard).toBeVisible()
  await managerCard.click()
  await expect(page).toHaveURL(/\/manager\/data-retrieval$/)
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/data-retrieval$/)
  expect(businessRequests).toEqual([])
})

test('create-only Transfer account opens the wizard from the Console module root', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/transfer', ['transfer.task.create', 'meta.catalog.read'])
  await expect(page).toHaveURL(/\/transfer\/tasks\/create$/)
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/tasks\/create$/)
  expect(businessRequests).toEqual([])
})

test('execution-only Monitor account opens its permitted page from the Console module root', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/monitor', ['monitor.execution.read'])
  await expect(page).toHaveURL(/\/monitor\/executions$/)
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/executions$/)
  expect(businessRequests).toEqual([])
})

test('a module root without page permissions shows no business iframe', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/transfer', [])
  await expect(page.locator('.el-result')).toContainText('无权访问')
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
  expect(businessRequests).toEqual([])
})

test('statistics-only account sees Monitor dashboard without execution access', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/monitor/dashboard', ['monitor.statistics.read'])
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/dashboard$/)
  await expect(page.locator('.sidebar .el-menu-item')).toHaveCount(1)
  await expectDenied(page, '/monitor/executions')
  expect(businessRequests).toEqual([])
})

test('Meta scan needs the live engine catalog in addition to Meta reads', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/meta/scan', [
    'meta.catalog.read', 'meta.scan_task.read'
  ])
  await expect(page.locator('.el-result')).toContainText('无权访问')
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
  expect(businessRequests).toEqual([])
})

test('a completed Meta scan read grant opens only its permitted entry', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/meta/scan', [
    'meta.catalog.read', 'meta.scan_task.read', 'system.engine_catalog.read'
  ])
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/scan$/)
  await expect(page.locator('.sidebar .el-menu-item')).toHaveCount(1)
  await expectDenied(page, '/system/engines')
  expect(businessRequests).toEqual([])
})

test('same-tenant AuthContext refresh updates the Console home status snapshot', async ({ page }) => {
  let permissions = []
  let engineReads = 0
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const pathname = new URL(route.request().url()).pathname
    if (pathname === '/api/v1/system/refresh') {
      return route.fulfill({ json: { access_token: 'initial-token', expires_in: 300 } })
    }
    if (pathname === '/api/v1/system/users/me') {
      return route.fulfill({ json: { id: 32, username: 'minimal-permissions-user' } })
    }
    if (pathname === '/api/v1/system/auth/context') {
      return route.fulfill({ json: {
        context: { type: 'tenant', tenant_id: '3' },
        authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' }, permissions }] }
      } })
    }
    if (pathname === '/api/v1/system/engines') {
      engineReads += 1
      return route.fulfill({ json: [{ id: 1 }, { id: 2 }] })
    }
    return route.fulfill({ status: 403, json: { error: 'unexpected_business_request' } })
  })

  await page.goto('/')
  await expect(page.getByText('当前账号暂无可用业务模块')).toBeVisible()
  await expect(page.locator('.status-snapshot')).toHaveCount(0)
  expect(engineReads).toBe(0)

  const refreshAuthContext = () => page.evaluate(async () => {
    const { useAuthStore } = await import('/src/store/auth.js')
    await useAuthStore().fetchAuthContext({ force: true })
  })

  permissions = ['system.engine.read']
  await refreshAuthContext()
  await expect(page.locator('.status-snapshot .stat-value')).toContainText('2')
  expect(engineReads).toBe(1)

  permissions = []
  await refreshAuthContext()
  await expect(page.locator('.status-snapshot')).toHaveCount(0)
  expect(engineReads).toBe(1)
})

test('role revocation refreshes an active session and removes the page from every open tab', async ({ context, page }) => {
  let revoked = false
  let refreshRequests = 0
  let moduleLoads = 0
  const taskTokens = []
  const unexpectedRequests = []
  await context.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await context.route('**/api/v1/**', async route => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname === '/api/v1/system/refresh') {
      refreshRequests += 1
      return route.fulfill({ json: {
        access_token: revoked ? 'revocation-fresh-token' : 'revocation-original-token', expires_in: 300
      } })
    }
    const token = await request.headerValue('authorization')
    if (pathname === '/api/v1/transfer/tasks') {
      taskTokens.push(token)
      return route.fulfill({ status: token === 'Bearer revocation-original-token' ? 401 : 403,
        json: { error: 'permission_denied' } })
    }
    if (revoked && token !== 'Bearer revocation-fresh-token') {
      return route.fulfill({ status: 401, json: { error: 'authentication_required' } })
    }
    if (pathname === '/api/v1/system/users/me') {
      return route.fulfill({ json: { id: '32', display_name: 'Role revocation fixture' } })
    }
    if (pathname === '/api/v1/system/auth/context') {
      const scope = { type: 'tenant', tenant_id: '3' }
      return route.fulfill({ json: {
        context: { type: 'tenant', tenant_id: '3', tenant_membership_id: '32' },
        authorization: { role_assignments: [
          { role_key: 'custom.monitor', scope, permissions: ['monitor.statistics.read'] },
          ...(!revoked ? [{ role_key: 'custom.transfer', scope, permissions: ['transfer.task.read'] }] : [])
        ] }
      } })
    }
    unexpectedRequests.push(pathname)
    return route.fulfill({ status: 403, json: { error: 'unexpected_business_request' } })
  })
  await context.route(/^http:\/\/127\.0\.0\.1:(?!4170)\d+\//, route => {
    moduleLoads += 1
    return route.fulfill({ contentType: 'text/html', body: '<title>Module fixture</title>' })
  })

  await page.goto('/transfer/tasks')
  await expect(page.locator('iframe.module-iframe')).toHaveCount(1)
  const peer = await context.newPage()
  await peer.goto('/transfer/tasks')
  await expect(peer.locator('iframe.module-iframe')).toHaveCount(1)
  await expect.poll(() => moduleLoads).toBe(2)
  const initialRefreshRequests = refreshRequests

  revoked = true
  const status = await page.evaluate(async () => {
    const { default: client } = await import('/src/api/client.js')
    try {
      await client.get('/transfer/tasks')
      return 200
    } catch (error) {
      return error.response.status
    }
  })
  expect(status).toBe(403)
  expect(taskTokens).toEqual(['Bearer revocation-original-token', 'Bearer revocation-fresh-token'])
  expect(refreshRequests).toBe(initialRefreshRequests + 1)
  for (const tab of [page, peer]) {
    await expect(tab).toHaveURL(/\/transfer\/tasks$/)
    await expect(tab.locator('.el-result')).toContainText('无权访问')
    await expect(tab.locator('iframe.module-iframe')).toHaveCount(0)
    await expect(tab.locator('.sidebar .el-menu-item').filter({ hasText: '传输任务' })).toHaveCount(0)
    expect(await tab.evaluate(async () => {
      const { useAuthStore } = await import('/src/store/auth.js')
      return useAuthStore().sessionStatus
    })).toBe('authenticated')
  }

  await expectDenied(page, '/transfer/tasks')
  expect(moduleLoads).toBe(2)
  await page.goto('/monitor/dashboard')
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/dashboard$/)
  await expect.poll(() => moduleLoads).toBe(3)
  expect(unexpectedRequests).toEqual([])
})

test('configuration owner direct URL does not inherit access from a different owner', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/configuration/monitor', ['develop.configuration.read'])
  await expect(page.locator('.el-result')).toContainText('无权访问')
  await expect(page.locator('[data-testid="configuration-management"]')).toHaveCount(0)
  expect(businessRequests).toEqual([])
})

test('configuration hub omits entries whose page needs an ungranted reference catalog', async ({ page }) => {
  const entries = ['agent', 'develop'].map(owner => ({
    id: `${owner}.configuration`,
    owner_module: owner,
    frontend_route: `/configuration/${owner}`,
    scope_types: ['platform_default_with_tenant_override'],
    available: true
  }))
  const businessRequests = await openAsTenant(page, '/configuration', [
    'agent.configuration.read', 'develop.configuration.read'
  ], { configurationEntries: entries })
  await expect(page.locator('.entries-table .el-table__row')).toHaveCount(1)
  await expect(page.locator('.entries-table .el-table__row')).toContainText('develop')
  expect(businessRequests).toEqual([])
})

test('same-tenant configuration grants refresh the visible owner entries immediately', async ({ page }) => {
  let permissions = ['develop.configuration.read']
  let entryReads = 0
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const pathname = new URL(route.request().url()).pathname
    if (pathname === '/api/v1/system/refresh') {
      return route.fulfill({ json: { access_token: 'configuration-refresh-token', expires_in: 300 } })
    }
    if (pathname === '/api/v1/system/users/me') {
      return route.fulfill({ json: { id: 34, username: 'configuration-refresh-user' } })
    }
    if (pathname === '/api/v1/system/auth/context') {
      return route.fulfill({ json: {
        context: { type: 'tenant', tenant_id: '3' },
        authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' }, permissions }] }
      } })
    }
    if (pathname === '/api/v1/system/configuration-management/entries') {
      entryReads += 1
      const owners = permissions.includes('agent.configuration.read') ? ['agent', 'develop'] : ['develop']
      return route.fulfill({ json: owners.map(owner => ({
        id: `${owner}.configuration`, owner_module: owner,
        frontend_route: `/configuration/${owner}`,
        scope_types: ['platform_default_with_tenant_override'], available: true
      })) })
    }
    return route.fulfill({ status: 403, json: { error: 'unexpected_business_request' } })
  })

  await page.goto('/configuration')
  await expect(page.locator('.entries-table .el-table__row')).toHaveCount(1)
  expect(entryReads).toBe(1)

  permissions = ['develop.configuration.read', 'agent.configuration.read', 'inference.profile.read']
  await page.evaluate(async () => {
    const { useAuthStore } = await import('/src/store/auth.js')
    await useAuthStore().fetchAuthContext({ force: true })
  })
  await expect(page.locator('.entries-table .el-table__row')).toHaveCount(2)
  expect(entryReads).toBe(2)
})

test('platform Inference configuration entry opens with all three management reads', async ({ page }) => {
  const businessRequests = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const pathname = new URL(route.request().url()).pathname
    if (pathname === '/api/v1/system/refresh') {
      return route.fulfill({ json: { access_token: 'platform-inference-token', expires_in: 300 } })
    }
    if (pathname === '/api/v1/system/users/me') {
      return route.fulfill({ json: { id: 33, username: 'platform-inference-user' } })
    }
    if (pathname === '/api/v1/system/auth/context') {
      return route.fulfill({ json: {
        context: { type: 'platform' },
        authorization: { role_assignments: [{
          scope: { type: 'platform' },
          permissions: ['inference.provider.read', 'inference.deployment.read', 'inference.profile.read']
        }] }
      } })
    }
    businessRequests.push(pathname)
    return route.fulfill({ status: 403, json: { error: 'unexpected_business_request' } })
  })
  await page.route(/^http:\/\/127\.0\.0\.1:(?!4170)\d+\//, route =>
    route.fulfill({ contentType: 'text/html', body: '<title>Module fixture</title>' }))
  await page.goto('/inference/settings/models')
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/settings\/models$/)
  await expect(page.locator('.el-result')).toHaveCount(0)
  expect(businessRequests).toEqual([])
})

test('Manager task reads cannot open embedding without the narrow model label grant', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/manager/tasks/quick-view', [
    'manager.derived_artifact.read', 'manager.data_item.read'
  ])
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/tasks\/quick-view$/)
  await expectDenied(page, '/manager/tasks/embedding')
  expect(businessRequests).toEqual([])
})

test('the narrow model label grant unlocks embedding without model administration', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/manager/tasks/embedding', [
    'manager.derived_artifact.read', 'manager.data_item.read', 'inference.model_label.read'
  ])
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/tasks\/embedding$/)
  await expectDenied(page, '/inference/settings/models')
  expect(businessRequests).toEqual([])
})

test('Tenant role reader sees IAM roles but no account administration', async ({ page }) => {
  const businessRequests = await openAsTenant(page, '/system/iam/roles', ['iam.tenant_role.read'])
  await expect(page.locator('iframe.module-iframe')).toHaveAttribute('src', /\/iam\/roles$/)
  await expect(page.locator('.sidebar .el-menu-item')).toHaveCount(1)
  await expectDenied(page, '/system/iam/accounts')
  expect(businessRequests).toEqual([])
})
