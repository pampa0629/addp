import { expect, test } from '@playwright/test'
import { assertDarkFirstHeading, observeFirstHeading } from '../../../common-frontend/basic/tests/fixtures/firstHeading.mjs'

const tenant = { type: 'tenant', tenant_id: '3' }
const pages = [
  ['/published-services', '服务发布'],
  ['/graph-services', '图查询服务'],
  ['/catalog', '服务目录']
]

for (const [path, title] of pages) {
  test(`Service first render uses the dark foreground on ${path}`, async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.setItem('addp-lang', 'zh-cn')
      localStorage.setItem('theme-mode', 'dark')
    })
    await observeFirstHeading(page, title)
    await page.route('**/api/v1/**', route => {
      const request = route.request()
      const pathname = new URL(request.url()).pathname
      if (pathname === '/api/v1/system/refresh') {
        return route.fulfill({ json: { access_token: 'service-heading-fixture', expires_in: 300 } })
      }
      if (pathname === '/api/v1/system/users/me') {
        return route.fulfill({ json: { id: 32, username: 'service-heading-reader' } })
      }
      if (pathname === '/api/v1/system/auth/context') {
        return route.fulfill({ json: {
          principal: { type: 'user', id: '32' },
          context: tenant,
          authorization: { role_assignments: [{ scope: tenant, permissions: ['service.definition.read'] }] }
        } })
      }
      if (request.method() === 'GET' && pathname.startsWith('/api/v1/service/')) {
        const data = path === '/published-services' && pathname === '/api/v1/service/query'
          ? [{ id: 49, title: 'Fixture published service', service_name: 'fixture_published', status: 'active', created_at: '2026-09-01T00:00:00Z' }]
          : []
        return route.fulfill({ json: { data, total: data.length, page: 1, limit: 20, offset: 0 } })
      }
      return route.abort()
    })
    await page.goto(`/e2e/service-fixture${path}`)
    const heading = page.getByRole('heading', { name: title, exact: true })
    await expect(heading).toBeInViewport({ ratio: 1 })
    await assertDarkFirstHeading(heading)
    if (path === '/published-services') {
      const table = page.getByRole('table')
      await expect(table.getByRole('cell', { name: 'Fixture published service', exact: true })).toHaveCSS('color', 'rgb(229, 234, 243)')
      await expect(table).toHaveCSS('background-color', 'rgb(29, 30, 31)')
      await expect(table.getByRole('columnheader').first()).toHaveCSS('background-color', 'rgb(20, 20, 20)')
      await expect(page.locator('.pagination')).toHaveCSS('background-color', 'rgb(29, 30, 31)')
    }
  })
}
