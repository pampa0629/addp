import { expect, test } from '@playwright/test'

const protectedModulePages = [
  '/ontology/ontologies',
  '/transfer/tasks',
  '/meta/scan',
  '/security/classification-grading',
  '/manager/data-explorer',
  '/standard/domains',
  '/modeling/entities',
  '/quality/plans',
  '/develop/sql',
  '/service/query-services',
  '/workbench/applications',
  '/orchestrator/orchestrations',
  '/monitor/dashboard',
  '/catalog/entries',
  '/asset/assets',
  '/system/iam/roles',
  '/agent',
  '/graph/ontologies',
  '/inference/settings/models'
]

test('zero-permission tenant cannot enter any module or start a business request', async ({ page }) => {
  const businessRequests = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/system/refresh') {
      return route.fulfill({ json: { access_token: 'zero-permission-token', expires_in: 300 } })
    }
    if (path === '/api/v1/system/users/me') {
      return route.fulfill({ json: { id: 32, username: 'zero-permission-user' } })
    }
    if (path === '/api/v1/system/auth/context') {
      return route.fulfill({ json: {
        context: { type: 'tenant', tenant_id: '3' },
        authorization: { role_assignments: [] }
      } })
    }
    businessRequests.push(path)
    return route.fulfill({ status: 403, json: { error: 'unexpected_business_request' } })
  })

  for (const route of protectedModulePages) {
    await page.goto(route)
    await expect(page.locator('.el-result'), route).toContainText('无权访问')
    await expect(page.locator('iframe.module-iframe'), route).toHaveCount(0)
  }
  expect(businessRequests).toEqual([])
})
