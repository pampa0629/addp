import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const locator = 'addp://engine/11/path/Outdoor/Persons?type=collection&item_id=106'
const rootLocator = 'addp://engine/11/path/?type=server&node_id=1'

for (const permissions of [['security.enrollment.read'], ['security.enrollment.create'], []]) {
  test(`protection configuration entry follows read permission (${permissions[0] || 'none'})`, async ({ page }) => {
    const auth = structuredClone(managerAuthContext)
    auth.authorization.role_assignments[0].permissions.push(...permissions)
    const item = { id: locator, locator, label: 'Persons', type: 'collection', hasChildren: false }
    const root = { id: rootLocator, locator: rootLocator, label: 'Business MongoDB', type: 'server', hasChildren: true, children: [item] }
    const writes = []
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/table-preview.js'] }))
    await page.route('**/security/protection-enrollments?*', route => route.fulfill({ contentType: 'text/html', body: '<main>Security navigation destination</main>' }))
    await page.route('**/api/v1/**', route => {
      const request = route.request()
      const path = new URL(request.url()).pathname
      if (request.method() !== 'GET') writes.push(path)
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'navigation-e2e-token', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'navigation-e2e' })
      if (path.endsWith('/system/auth/context')) return json(route, auth)
      if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 11, name: root.label, engine_type: 'mongodb', lifecycle_state: 'active', connection_status: 'online' }] })
      if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
      if (path.endsWith('/meta/resource-tree/11') || path.endsWith('/meta/resource-tree/11/node')) return json(route, root)
      if (path.endsWith('/manager/preview')) return json(route, { preview_type: 'table', data: {
        item_meta: { id: 106, fingerprint: 'persons-fingerprint', full_name: 'Outdoor.Persons' },
        mode: 'table', columns: ['userInfo.phone'], total: 1, page: 1, page_size: 20,
        rows: [{ 'userInfo.phone': 'synthetic-protected-value' }]
      } })
      return json(route, {})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
    await expect(page.getByRole('cell', { name: 'synthetic-protected-value', exact: true })).toBeVisible()
    const entry = page.getByRole('button', { name: '查看/配置', exact: true })
    if (permissions.includes('security.enrollment.read')) {
      await expect(entry).toBeVisible()
      await entry.click()
      await expect(page).toHaveURL(/\/security\/protection-enrollments\?locator=/)
      const destination = new URL(page.url())
      expect(destination.searchParams.get('locator')).toBe(locator)
      expect(destination.searchParams.has('action')).toBe(false)
    } else await expect(entry).toHaveCount(0)
    // Authentication refresh may use POST; viewing configuration never writes Security facts.
    expect(writes.filter(path => path.startsWith('/api/v1/security/'))).toEqual([])
  })
}

function json(route, body) {
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
}
