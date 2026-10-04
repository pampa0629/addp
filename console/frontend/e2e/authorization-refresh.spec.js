import { expect, test } from '@playwright/test'

async function setup(page) {
  let generation = 0
  let hold = false
  let release
  let moduleLoads = 0
  let failure = false
  let identity = { principal: '17', tenant: '3', membership: '19' }
  let permissions = ['transfer.task.read', 'monitor.statistics.read']
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/refresh')) return route.fulfill({ json: { access_token: `token-${++generation}`, expires_in: 900 } })
    if (path.endsWith('/users/me')) return route.fulfill({ json: { id: '17', display_name: 'Refresh reader', local_account: { username: 'reader' } } })
    if (path.endsWith('/auth/context')) {
      if (hold) await new Promise(resolve => { release = resolve })
      if (failure) return route.fulfill({ status: 503, json: { error: 'service_unavailable' } })
      return route.fulfill({ json: {
        principal: { id: identity.principal, type: 'user' },
        context: { type: 'tenant', tenant_id: identity.tenant, tenant_membership_id: identity.membership },
        authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: identity.tenant }, permissions }] }
      } })
    }
    return route.fulfill({ status: 403, json: { error: 'unexpected_request' } })
  })
  await page.route('**/module-ui/**', route => {
    moduleLoads++
    return route.fulfill({ contentType: 'text/html', body: '<title>Unsaved module fixture</title><input aria-label="draft">' })
  })
  await page.goto('/transfer/tasks')
  const iframe = page.locator('iframe.module-iframe')
  const frame = () => page.frames().find(value => value.url().includes('/module-ui/'))
  await expect(page.frameLocator('iframe.module-iframe').getByLabel('draft')).toBeVisible()
  await page.frameLocator('iframe.module-iframe').getByLabel('draft').fill('unsaved content')
  const original = frame()
  return {
    iframe, frame, original, loads: () => moduleLoads,
    async refresh() {
      hold = true
      await page.evaluate(async () => {
        const { useAuthStore } = await import('/src/store/auth.js')
        await useAuthStore().refreshAccessToken({ force: true })
      })
      await expect.poll(() => Boolean(release)).toBe(true)
    },
    finish(changes = {}) {
      identity = { ...identity, ...changes.identity }
      permissions = changes.permissions || permissions
      failure = changes.failure || false
      hold = false
      release()
    }
  }
}

test('token refresh hides the iframe without retaining grants or losing the draft', async ({ page }) => {
  const fixture = await setup(page)
  await fixture.refresh()
  await expect(fixture.iframe).toHaveCount(1)
  await expect(fixture.iframe).toBeHidden()
  expect(await page.evaluate(async () => {
    const { useAuthStore } = await import('/src/store/auth.js')
    return useAuthStore().permissions
  })).toEqual([])
  fixture.finish()
  await expect(fixture.iframe).toBeVisible()
  expect(fixture.frame()).toBe(fixture.original)
  await expect(page.frameLocator('iframe.module-iframe').getByLabel('draft')).toHaveValue('unsaved content')
  expect(fixture.loads()).toBe(1)
})

test('an authorization outage keeps the draft hidden until a successful retry', async ({ page }) => {
  const fixture = await setup(page)
  await fixture.refresh()
  fixture.finish({ failure: true })
  await expect(page.getByText('无法确认当前页面的访问权限')).toBeVisible()
  await expect(fixture.iframe).toHaveCount(1)
  await expect(fixture.iframe).toBeHidden()
  await expect(page.getByText('无权访问')).toHaveCount(0)
  fixture.finish()
  await page.getByRole('button', { name: '重试' }).click()
  await expect(fixture.iframe).toBeVisible()
  expect(fixture.frame()).toBe(fixture.original)
  await expect(page.frameLocator('iframe.module-iframe').getByLabel('draft')).toHaveValue('unsaved content')
})

test('revoked permissions unload the draft after authoritative confirmation', async ({ page }) => {
  const fixture = await setup(page)
  await fixture.refresh()
  fixture.finish({ permissions: ['monitor.statistics.read'] })
  await expect(page.getByText('无权访问此页面', { exact: true })).toBeVisible()
  await expect(fixture.iframe).toHaveCount(0)
  expect(fixture.original.isDetached()).toBe(true)
  expect(fixture.loads()).toBe(1)
})

for (const [name, identity] of [
  ['tenant', { tenant: '4', membership: '20' }],
  ['principal', { principal: '18' }],
  ['membership', { membership: '20' }]
]) {
  test(`${name} changes never restore the old draft even with the same page permission`, async ({ page }) => {
    const fixture = await setup(page)
    await fixture.refresh()
    fixture.finish({ identity })
    await expect(page.frameLocator('iframe.module-iframe').getByLabel('draft')).toBeVisible()
    await expect(page.frameLocator('iframe.module-iframe').getByLabel('draft')).toHaveValue('')
    expect(fixture.original.isDetached()).toBe(true)
    expect(fixture.loads()).toBe(2)
  })
}

test('navigation during authorization loading unloads the old page and waits before loading another', async ({ page }) => {
  const fixture = await setup(page)
  await fixture.refresh()
  await page.evaluate(async () => {
    const { default: router } = await import('/src/router/index.js')
    await router.push('/monitor/dashboard')
  })
  await expect(fixture.iframe).toHaveCount(0)
  expect(fixture.loads()).toBe(1)
  fixture.finish()
  await expect(fixture.iframe).toHaveAttribute('src', /\/monitor\/dashboard$/)
  await expect(page.frameLocator('iframe.module-iframe').getByLabel('draft')).toHaveValue('')
  expect(fixture.loads()).toBe(2)
})
