import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

test('waits for the Owner checkpoint before automatically refreshing an approved preview', async ({ page }) => {
  const locator = 'addp://engine/2/path/public/contacts?type=table&item_id=8'
  const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
  const item = { id: locator, locator, label: 'contacts', type: 'table', hasChildren: false }
  const root = { id: rootLocator, locator: rootLocator, label: 'Security test engine', type: 'server', hasChildren: true, children: [item] }
  const auth = structuredClone(managerAuthContext)
  auth.authorization.role_assignments[0].permissions.push('security.protection_access_request.create', 'security.protection_access_request.read')
  let stage = 'pending'
  let previews = 0
  let targets = 0
  await page.clock.install()
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/table-preview.js'] }))
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'checkpoint-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'checkpoint-e2e', local_account: { username: 'checkpoint-e2e' } })
    if (path.endsWith('/system/auth/context')) return json(route, auth)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
    if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(route, root)
    if (path.endsWith('/manager/preview')) {
      previews += 1
      return json(route, { preview_type: 'table', data: {
        item_meta: { id: 8, fingerprint: 'contacts-fingerprint', full_name: 'public.contacts' },
        mode: 'table', columns: ['id', 'phone'], total: 1, page: 1, page_size: 20,
        rows: [{ id: 1, phone: stage === 'synchronized' ? '13812345678' : '138****5678' }]
      } })
    }
    if (path.endsWith('/security/protection-access-request-targets')) {
      targets += 1
      return json(route, { data: [{ assessment_id: 'phone-assessment', requestable: true,
        component: { key: 'phone', path: [{ name: 'phone', container: 'scalar' }], value_type: 'string' },
        access_request: { id: 'request-1', state: stage === 'pending' ? 'pending' : 'approved' },
        projection_acknowledged: stage === 'synchronized',
        ...(stage === 'pending' ? {} : { active_exemption_id: 'grant-1' })
      }] })
    }
    return json(route, {})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
  await expect(page.getByText('1 个申请待审批', { exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: '138****5678', exact: true })).toBeVisible()
  const initialPreviews = previews
  stage = 'approved'
  await page.clock.runFor(5000)
  await expect(page.getByText('1 个字段已批准，等待同步', { exact: true })).toBeVisible()
  expect(previews).toBe(initialPreviews)
  const waitingTargets = targets
  await page.clock.runFor(5000)
  await expect.poll(() => targets).toBeGreaterThan(waitingTargets)
  expect(previews).toBe(initialPreviews)
  stage = 'synchronized'
  await page.clock.runFor(5000)
  await expect(page.getByRole('cell', { name: '13812345678', exact: true })).toBeVisible()
  await expect(page.getByText('1 个字段原值授权已同步', { exact: true })).toBeVisible()
  expect(previews).toBe(initialPreviews + 1)
  await page.clock.runFor(10_000)
  expect(previews).toBe(initialPreviews + 1)
})

function json(route, body) {
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
}
