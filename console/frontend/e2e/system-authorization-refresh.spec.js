import { expect, test } from '@playwright/test'

const authorizationURL = '/system/engines/2?tab=data-authorization'
const permissions = ['system.engine.read', 'system.engine_access_approval_requirement.read',
  'system.engine_access_approval_requirement.initialize', 'system.engine_catalog.read']

async function setup(page) {
  let generation = 0, hold = false, allowed = [...permissions]
  const pending = [], unexpected = [], writes = [], errors = []
  page.on('pageerror', error => errors.push(error.stack || error.message))
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const context = () => ({
    principal: { id: '1', type: 'user' },
    context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
    authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' }, permissions: allowed }] }
  })
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (path === '/api/v1/system/refresh') return route.fulfill({ json: { access_token: `engine-token-${++generation}`, expires_in: 900 } })
    if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: '1', display_name: 'Engine administrator fixture' } })
    if (path === '/api/v1/system/auth/context') {
      if (hold) { pending.push(route); return }
      return route.fulfill({ json: context() })
    }
    if (path === '/api/v1/system/engines/2/catalog/children' && request.method() === 'POST') return route.fulfill({ json: { nodes: [] } })
    if (request.method() !== 'GET') writes.push(`${request.method()} ${path}`)
    const engine = { id: '2', name: '授权验证 PostgreSQL', engine_type: 'postgresql', engine_origin: 'general', lifecycle_state: 'active', connection_status: 'online', capability_groups: ['storage'] }
    const responses = {
      '/api/v1/system/engines': [engine], '/api/v1/system/engines/2': engine,
      '/api/v1/system/engine-types': [],
      '/api/v1/system/engines/2/access_approval_requirements': { data: [], total: 0, page: 1, page_size: 10, total_pages: 1 }
    }
    if (request.method() === 'GET' && Object.hasOwn(responses, path)) return route.fulfill({ json: responses[path] })
    unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({ status: 403, json: { error: 'unexpected_request' } })
  })
  await page.goto(authorizationURL)
  const iframe = page.locator('iframe.module-iframe')
  const frame = page.frameLocator('iframe.module-iframe')
  await expect(frame.getByTestId('engine-data-authorization')).toBeVisible()
  const original = page.frames().find(value => value.url().includes('/module-ui/system/'))
  return {
    iframe, frame, original,
    async beginRefresh() {
      hold = true
      await page.evaluate(async () => {
        const { useAuthStore } = await import('/src/store/auth.js')
        await useAuthStore().refreshAccessToken({ force: true })
      })
      await expect.poll(() => pending.length).toBe(2)
      await expect(iframe).toHaveCount(1)
      await expect(iframe).toBeHidden()
      expect(await original.evaluate(async () => {
        const { useAuthStore } = await import('/module-ui/system/src/store/auth.js')
        return useAuthStore().permissions
      })).toEqual([])
      // Let the real Engines watcher and route guard run while the authority is held.
      await original.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
      await expect(page).toHaveURL(new RegExp(`${authorizationURL.replace('?', '\\?')}$`))
    },
    async finishRefresh({ revoked = false, grantRevoked = false, failure = false } = {}) {
      if (revoked) allowed = []
      if (grantRevoked) allowed = ['system.engine.read']
      hold = false
      for (const route of pending.splice(0)) await route.fulfill(failure
        ? { status: 503, json: { error: 'fixture_authority_unavailable' } }
        : { json: context() })
    },
    async verifyRestored() {
      await expect(iframe).toBeVisible()
      await expect.poll(() => original.evaluate(async () => {
        const { useAuthStore } = await import('/module-ui/system/src/store/auth.js')
        return useAuthStore().permissions
      })).toEqual([...permissions].sort())
      await expect(frame.getByTestId('engine-data-authorization')).toBeVisible()
      await expect(page).toHaveURL(new RegExp(`${authorizationURL.replace('?', '\\?')}$`))
      expect(page.frames().find(value => value.url().includes('/module-ui/system/'))).toBe(original)
    },
    verifyNoWrites() {
      expect(writes).toEqual([])
      expect(unexpected).toEqual([])
      expect(errors).toEqual([])
    }
  }
}

test('keeps the real engine authorization tab and URL through repeated token rotations', async ({ page }) => {
  const fixture = await setup(page)
  for (let attempt = 0; attempt < 2; attempt++) {
    await fixture.beginRefresh()
    await fixture.finishRefresh()
    await fixture.verifyRestored()
  }
  fixture.verifyNoWrites()
})

test('unloads the engine page when the refreshed authority revokes engine access', async ({ page }) => {
  const fixture = await setup(page)
  await fixture.beginRefresh()
  await fixture.finishRefresh({ revoked: true })
  await expect(fixture.iframe).toHaveCount(0)
  fixture.verifyNoWrites()
})

test('retains the engine authorization URL through an authority outage and retry', async ({ page }) => {
  const fixture = await setup(page)
  await fixture.beginRefresh()
  await fixture.finishRefresh({ failure: true })
  await expect(page.getByText('无法确认当前页面的访问权限', { exact: true })).toBeVisible()
  await expect(page).toHaveURL(new RegExp(`${authorizationURL.replace('?', '\\?')}$`))
  await expect(fixture.iframe).toHaveCount(1)
  await expect(fixture.iframe).toBeHidden()
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await fixture.verifyRestored()
  fixture.verifyNoWrites()
})

test('normalizes the engine tab only after authorization permissions are really revoked', async ({ page }) => {
  const fixture = await setup(page)
  await fixture.beginRefresh()
  await fixture.finishRefresh({ grantRevoked: true })
  await expect(page).toHaveURL(/\/system\/engines\/2$/)
  await expect(fixture.frame.getByRole('tab', { name: '基本信息', exact: true })).toBeVisible()
  await expect(fixture.frame.getByTestId('engine-data-authorization')).toHaveCount(0)
  fixture.verifyNoWrites()
})
