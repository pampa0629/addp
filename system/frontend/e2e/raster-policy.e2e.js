import { expect, test } from '@playwright/test'

async function mockRaster(page, { scope = 'platform', writable = true, unavailable = false, conflict = false, empty = false, shrunk = false } = {}) {
  const writes = []
  let value = {
    policy: { engine_id: 1, version: 1, running: 2, waiting: 2, cache_mib: 256, default_tenant_running: 2, default_tenant_waiting: 2 },
    quota: { engine_id: 1, tenant_id: 3, version: 1, running: null, waiting: null }, effective_running: 2, effective_waiting: 2,
    runtime: { running: 0, waiting: 0, enabled: true, applied_version: 1, cache_mib: 256, effective_cpu: 2, memory_limit_bytes: 1073741824,
      observed_at: '2026-10-05T00:00:00Z', advice: { running: 1, waiting: 1, cache_mib: 32 } }
  }
  if (shrunk) { value.policy.running = 1; value.policy.default_tenant_running = 1; value.quota.running = 2; value.effective_running = 1 }
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || 'http://127.0.0.1:4173', 'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,PUT,OPTIONS' }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'raster-policy-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Raster administrator' })
    if (path.endsWith('/auth/context')) return reply({
      principal: { id: '1', principal_type: 'user' }, context: { type: scope, ...(scope === 'tenant' ? { tenant_id: '3', tenant_membership_id: '4' } : {}) },
      authorization: { role_assignments: [{ scope: { type: scope, ...(scope === 'tenant' ? { tenant_id: '3' } : {}) }, permissions:
        ['system.engine_raster_policy.read', ...(writable ? ['system.engine_raster_policy.update'] : [])] }] }
    })
    if (path.endsWith(`/system/${scope}/engine-raster-policies/engines`)) return reply(empty ? [] : [{ id: 1, name: 'GeoPython' }])
    if (path.endsWith(`/system/${scope}/engine-raster-policies/1`)) {
      if (request.method() === 'PUT') {
        const body = request.postDataJSON(); writes.push(body)
        if (conflict) return reply({ error: '配置版本冲突' }, 409)
        if (scope === 'platform') value.policy = { ...value.policy, ...body, version: body.version + 1 }
        else value.quota = { ...value.quota, ...body, version: body.version + 1 }
        return reply(value)
      }
      return unavailable ? reply({ error: '不可用' }, 503) : reply(value)
    }
    throw new Error(`Unexpected raster request: ${request.method()} ${path}`)
  })
  return writes
}

test('platform adopts visible resource advice and saves a versioned policy with restart status', async ({ page }) => {
  const writes = await mockRaster(page)
  await page.goto('/engine-raster-policies')
  const card = page.getByTestId('raster-policy')
  await expect(card).toHaveAttribute('data-state', 'loaded')
  await expect(page.getByTestId('raster-advice-values')).toContainText('缓存 32 MiB')
  await page.getByTestId('raster-advice').click()
  await page.getByTestId('raster-save').click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toEqual({ version: 1, running: 1, waiting: 1, cache_mib: 32, default_tenant_running: 1, default_tenant_waiting: 1 })
  await expect(page.getByTestId('raster-restart')).toBeVisible()
})

test('tenant only writes its quota and can reset to inherited limits', async ({ page }) => {
  const writes = await mockRaster(page, { scope: 'tenant' })
  await page.goto('/engine-raster-policies')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await expect(page.getByTestId('cache_mib')).toHaveCount(0)
  await expect(page.getByTestId('raster-advice')).toHaveCount(0)
  await expect(page.getByTestId('raster-constraints')).toContainText('256')
  await page.getByTestId('inherit-running').click()
  await page.getByTestId('running').getByRole('spinbutton').fill('1')
  await page.getByTestId('raster-save').click()
  await expect.poll(() => writes.length).toBe(1)
  expect(writes[0]).toEqual({ version: 1, running: 1, waiting: null })
  await page.getByTestId('inherit-running').click()
  await page.getByTestId('raster-save').click()
  await expect.poll(() => writes.length).toBe(2)
  expect(writes[1]).toEqual({ version: 2, running: null, waiting: null })
})

test('read-only role has no save action and stale writes keep the administrator draft', async ({ page }) => {
  await mockRaster(page, { writable: false })
  await page.goto('/engine-raster-policies')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await expect(page.getByTestId('raster-save')).toHaveCount(0)
  await expect(page.getByTestId('running').getByRole('spinbutton')).toBeDisabled()
})

test('version conflict keeps the draft', async ({ page }) => {
  await mockRaster(page, { conflict: true })
  await page.goto('/engine-raster-policies')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await page.getByTestId('cache_mib').getByRole('spinbutton').fill('128')
  await page.getByTestId('raster-save').click()
  await expect(page.getByText(/配置已被修改，请刷新后重新编辑/)).toBeVisible()
  await expect(page.getByTestId('cache_mib').getByRole('spinbutton')).toHaveValue('128')
})

for (const empty of [false, true]) test(`unavailable policy or empty engine list exposes no fabricated defaults (${empty})`, async ({ page }) => {
  await mockRaster(page, { unavailable: true, empty })
  await page.goto('/engine-raster-policies')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'unavailable')
  await expect(page.getByTestId('raster-save')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '刷新', exact: true })).not.toHaveClass(/is-loading/)
})


test('platform shrink displays the saved tenant quota without silently overwriting it', async ({ page }) => {
  await mockRaster(page, { scope: 'tenant', shrunk: true })
  await page.goto('/engine-raster-policies')
  await expect(page.getByTestId('raster-policy')).toHaveAttribute('data-state', 'loaded')
  await expect(page.getByTestId('running').getByRole('spinbutton')).toHaveValue('2')
  await expect(page.getByTestId('raster-quota-exceeds')).toBeVisible()
  await page.getByTestId('running').getByRole('spinbutton').fill('1')
  await expect(page.getByTestId('running').getByRole('spinbutton')).toHaveValue('1')
})
