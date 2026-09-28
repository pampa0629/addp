import { expect, test } from '@playwright/test'

async function openAsTenant(page, path, permissions) {
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
