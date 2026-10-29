import { expect, test } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { managerAuthContext } from './managerAuthContext.js'

// Synthetic 21x21 XYZ grid, Z=(x*x+y*y)/200, RGB=(x/y ramp in 0..255,255).
// The tiny fixed COPC fixture keeps this frontend gate independent of native PDAL.
const copc = readFileSync(new URL('./fixtures/pointcloud/rgb8.copc.laz', import.meta.url))
const locator = 'addp://engine/2/path/rgb.copc.laz?type=file&item_id=8'
const otherLocator = 'addp://engine/2/path/other.copc.laz?type=file&item_id=9'
const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
const node = (identity, name) => ({ id: identity, locator: identity, label: name, type: 'file', hasChildren: false })
const item = node(locator, 'rgb.copc.laz'), other = node(otherLocator, 'other.copc.laz')
const root = { id: rootLocator, locator: rootLocator, label: 'Point source', type: 'server', hasChildren: true, children: [item, other] }

async function inspect(page) {
  return page.locator('.point-cloud-preview').evaluate(element => {
    const s = element.__vueParentComponent.setupState
    const cloud = s.giroPointCloud?.value || s.giroPointCloud
    const instance = s.giroInstance?.value || s.giroInstance
    let max = 0, count = 0
    cloud?.object3d.traverse(object => {
      const colors = object.geometry?.attributes?.color
      if (colors) { for (const value of colors.array) max = Math.max(value, max); count += colors.count }
    })
    return { max, count, position: instance?.view.camera.position.toArray(), attribute: cloud?.getActiveAttributes()[0]?.attribute.name }
  })
}

test('switching items during COPC initialization settles the obsolete read without a page error', async ({ page }) => {
  const { unexpected } = await backend(page, 'en', 'quick_view')
  const errors = []
  page.on('pageerror', error => errors.push(error.stack || error.message))
  let release, started
  const held = new Promise(resolve => { release = resolve })
  const requested = new Promise(resolve => { started = resolve })
  let first = true
  await page.route('**/point_cloud_copc/1/content', async route => {
    if (first) {
      first = false
      started()
      await held
    }
    return route.fallback()
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
  await requested
  await page.locator('.point-cloud-preview').evaluate(element => {
    const state = element.__vueParentComponent.setupState
    const instance = state.giroInstance?.value || state.giroInstance
    const source = instance.getObjects()[0].source
    window.obsoleteCOPCRead = { settled: false }
    source.initialize().then(
      () => { window.obsoleteCOPCRead = { settled: true, ready: source.ready } },
      error => { window.obsoleteCOPCRead = { settled: true, ready: source.ready, error: error.message } }
    )
  })
  await page.getByRole('treeitem', { name: 'other.copc.laz', exact: true }).click()
  await expect.poll(async () => (await inspect(page)).count).toBeGreaterThan(100)
  release()
  await expect.poll(() => page.evaluate(() => window.obsoleteCOPCRead)).toEqual({ settled: true, ready: false, error: 'aborted' })
  expect(unexpected).toEqual([])
  expect(errors).toEqual([])
})

test('a current COPC permission failure stays visible and has no unhandled initialization rejection', async ({ page }) => {
  const { unexpected } = await backend(page, 'en', 'quick_view')
  const errors = [], reported = []
  page.on('pageerror', error => errors.push(error.message))
  page.on('console', message => {
    if (message.type() === 'error') reported.push(message.text())
  })
  await page.route('**/point_cloud_copc/1/content', route => route.fulfill({ status: 403, body: 'Forbidden' }))
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
  await expect(page.locator('.point-cloud-preview')).toContainText('COPC range request failed: HTTP 403')
  expect(reported.some(message => message.includes('COPC range request failed: HTTP 403'))).toBe(true)
  expect(unexpected).toEqual([])
  expect(errors).toEqual([])
})

async function backend(page, locale, domain) {
  const states = new Map(), writes = [], unexpected = []
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), locale)
  await page.route('**/plugins/manifest.json', route => route.fulfill({ json: { scripts: ['/plugins/point-cloud-preview.js'] } }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname
    const identity = url.searchParams.get('locator') || locator
    const state = states.get(identity) || {}
    const json = value => route.fulfill({ json: value })
    if (path.endsWith('/system/refresh')) return json({ access_token: 'point-color-fixture', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json({ id: 1, display_name: 'point-color-test', local_account: { username: 'point-color-test' } })
    if (path.endsWith('/system/auth/context')) return json(managerAuthContext)
    if (path.endsWith('/manager/engines')) return json({ data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json({ target_locator: identity, ancestors: [{ ...root, children: [] }, identity === otherLocator ? other : item] })
    if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(root)
    if (path.endsWith('/manager/preview-state/view-state')) {
      const body = route.request().postDataJSON()
      states.set(body.locator, body.view_state)
      writes.push(body)
      return json({ view_state: body.view_state })
    }
    if (path.endsWith('/manager/preview-state')) return json({ view_state: state, preferred_mode: domain === 'quick_view' ? 'map_quick_view' : 'basic_preview' })
    if (path.endsWith('/manager/quick-view/capability')) return json({
      can_use_quick_view: domain === 'quick_view', status: 'available', render_source: 'point_cloud_copc',
      preferred_mode: domain === 'quick_view' ? 'map_quick_view' : 'basic_preview', view_state: state,
      point_cloud: { result_id: 1, last_execution_id: 'fixture-v1', point_count: 441, preview_url: '/api/v1/manager/point_cloud_copc/1/content' }
    })
    if (path.endsWith('/manager/preview')) return json({ preview_type: 'object', data: {
      mode: 'object', object: { content: { kind: 'point_cloud', frontend_renderer: 'point_cloud', url: '/api/v1/manager/point_cloud_copc/1/content', metadata: { format: 'copc' } } }
    } })
    if (path.endsWith('/point_cloud_copc/1/content')) {
      const range = /bytes=(\d+)-(\d+)/.exec(route.request().headers().range || '')
      expect(range).not.toBeNull()
      const start = Number(range[1]), end = Math.min(Number(range[2]), copc.length - 1)
      return route.fulfill({ status: 206, headers: { 'Content-Type': 'application/vnd.laszip+copc', 'Content-Range': `bytes ${start}-${end}/${copc.length}`, 'Accept-Ranges': 'bytes' }, body: copc.subarray(start, end + 1) })
    }
    if (/quick-view\/actions|\/execute$/.test(path)) unexpected.push(path)
    return json({})
  })
  return { writes, states, unexpected }
}

for (const locale of ['zh-cn', 'en']) for (const domain of ['quick_view', 'basic_preview']) {
  test(`point color preserves RGB precision, camera and item preference in ${domain} ${locale}`, async ({ page }) => {
    const { writes, states, unexpected } = await backend(page, locale, domain)
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
    const select = page.getByRole('combobox', { name: locale === 'en' ? 'Point cloud coloring' : '点云着色模式' })
    await expect(select).toBeVisible()
    await expect.poll(async () => (await inspect(page)).count).toBeGreaterThan(100)
    const initial = await inspect(page)
    expect(initial.max).toBeGreaterThan(0)
    expect(initial.max).toBeLessThan(0.01)
    const choose = async name => { await select.press('ArrowDown'); await page.getByRole('option', { name, exact: true }).click() }
    await choose(locale === 'en' ? 'RGB 8-bit' : 'RGB 8 位')
    await expect.poll(async () => (await inspect(page)).max).toBeGreaterThan(0.8)
    for (const [axis, value] of (await inspect(page)).position.entries()) expect(value).toBeCloseTo(initial.position[axis], 10)
    await choose(locale === 'en' ? 'RGB 16-bit' : 'RGB 16 位')
    await expect.poll(async () => (await inspect(page)).max).toBeLessThan(0.01)
    await choose(locale === 'en' ? 'Elevation' : '按高程着色')
    await expect.poll(async () => (await inspect(page)).attribute).toBe('Z')
    for (const [axis, value] of (await inspect(page)).position.entries()) expect(value).toBeCloseTo(initial.position[axis], 10)
    await choose(locale === 'en' ? 'RGB 8-bit' : 'RGB 8 位')
    await expect.poll(() => states.get(locator)?.[domain]?.scene_3d?.color_mode).toBe('rgb8')
    await page.reload()
    await expect.poll(async () => (await inspect(page)).max).toBeGreaterThan(0.8)
    await page.getByRole('treeitem', { name: 'other.copc.laz', exact: true }).click()
    await expect.poll(async () => (await inspect(page)).count).toBeGreaterThan(100)
    await expect.poll(async () => (await inspect(page)).max).toBeLessThan(0.01)
    expect(states.get(locator)?.[domain]?.scene_3d?.color_mode).toBe('rgb8')
    expect(writes.length).toBeGreaterThan(0)
    expect(unexpected).toEqual([])
    expect(errors).toEqual([])
  })
}
