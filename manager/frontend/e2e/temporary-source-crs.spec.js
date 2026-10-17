import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const transparentPNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==', 'base64')
async function fixture(page, language, container = true) {
  const name = container ? 'projected.topojson' : 'projected.gml'
  const locator = `addp://engine/2/path/${name}?type=file&item_id=8`
  const knownLocator = 'addp://engine/2/path/known.gml?type=file&item_id=9'
  const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
  const items = [[name, locator], ['known.gml', knownLocator]].map(([label, id]) => ({ id, locator: id, label, type: 'file', hasChildren: false }))
  const root = { id: rootLocator, locator: rootLocator, label: 'Vector exchange fixture', type: 'server', hasChildren: true, children: items }
  const requests = [], errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), language)
  await page.route('**/*openstreetmap.org/**', route => route.fulfill({ contentType: 'image/png', headers: { 'access-control-allow-origin': '*' }, body: transparentPNG }))
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/container-preview.js', '/plugins/table-preview.js'] }))
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url()), path = url.pathname
    requests.push({ path, method: route.request().method(), query: Object.fromEntries(url.searchParams) })
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'crs-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'crs-e2e', local_account: { username: 'crs-e2e' } })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) {
      const target = url.searchParams.get('locator') || locator
      return json(route, { target_locator: target, ancestors: [{ ...root, children: [] }, items.find(item => item.locator === target) || items[0]] })
    }
    if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(route, root)
    if (path.endsWith('/manager/preview')) {
      const selected = url.searchParams.get('locator') || locator
      const child = url.searchParams.get('child_name') || ''
      const known = selected === knownLocator
      const currentPage = Number(url.searchParams.get('page') || 1), size = Number(url.searchParams.get('page_size') || 20)
      if (container && !known && !child) return json(route, { preview_type: 'object', data: { object: { path: name, content: { kind: 'container', json: {
        format: 'topojson', default_child: 'sites', children: ['sites', 'roads'].map(name => ({ name, key: name, child_kind: 'feature_class', data_type: 'table', column_count: 3 })), summary: { child_count: 2 }
      } } } } })
      const count = Math.max(0, Math.min(size, 25 - (currentPage - 1) * size))
      return json(route, { preview_type: 'table', data: {
        mode: 'table', columns: ['id', 'name', 'source_shape'], geometry_columns: ['source_shape'], geometry_column: 'source_shape',
        source_srid: known ? 4326 : 0, source_crs: known ? 'EPSG:4326' : '', transform_status: known ? 'not_transformed' : 'unknown_crs',
        total: 25, page: currentPage, page_size: size,
        rows: Array.from({ length: count }, (_, index) => {
          const id = (currentPage - 1) * size + index + 1
          return { id, name: `${child || 'single'}-${id}`, source_shape: { type: 'Point', coordinates: known || child === 'roads' ? [10 + id / 100, id / 100] : [1113194.9079327357 + id * 1000, id * 1000] } }
        }), item_meta: { fingerprint: known ? 'known' : 'unknown', full_name: known ? 'known.gml' : name }
      } })
    }
    return json(route, {})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
  return { name, requests, errors }
}
async function chooseCRS(page, label, code) {
  const combo = page.getByRole('combobox', { name: label, exact: true })
  await combo.press('ArrowDown')
  const id = await combo.getAttribute('aria-controls')
  await page.locator(`[id="${id}"]`).getByRole('option', { name: code, exact: true }).click()
}
async function expectPaint(page) {
  const canvases = page.locator('.table-preview .ol-viewport canvas')
  await expect(canvases.first()).toBeVisible()
  await expect.poll(() => canvases.evaluateAll(elements => elements.reduce((count, element) => {
    const context = element.getContext('2d')
    if (!context || !element.width || !element.height) return count
    const pixels = context.getImageData(0, 0, element.width, element.height).data
    for (let offset = 0; offset < pixels.length; offset += 4) {
      if (pixels[offset + 3] > 0 && pixels[offset + 2] > pixels[offset] + 20) count++
    }
    return count
  }, 0))).toBeGreaterThan(10)
}
const managerWrites = state => state.requests.filter(request => request.path.includes('/manager/') && request.method !== 'GET')
for (const language of ['zh-cn', 'en']) {
  test(`temporary CRS is explicit, paginated, local and scoped to the selected layer (${language})`, async ({ page }, testInfo) => {
    const chinese = language === 'zh-cn', label = chinese ? '本次预览的源坐标系' : 'Source CRS for this preview'
    const state = await fixture(page, language)
    await expect(page.getByRole('cell', { name: 'sites-1', exact: true })).toBeVisible()
    await expect(page.getByRole('combobox', { name: label, exact: true })).toBeVisible()
    await expect(page.getByText(chinese ? '源坐标系未知，已跳过地图渲染' : 'Source CRS is unknown, map rendering has been skipped', { exact: true })).toBeVisible()
    await expect(page.locator('.table-preview .ol-viewport')).toHaveCount(0)
    const writes = managerWrites(state)
    await chooseCRS(page, label, 'EPSG:3857')
    await expectPaint(page)
    await page.getByRole('button', { name: chinese ? '下一页' : 'Go to next page', exact: true }).click()
    await expect(page.getByRole('cell', { name: 'sites-21', exact: true })).toBeVisible()
    await expect(page.locator('.temporary-crs-select')).toContainText('EPSG:3857')
    await expectPaint(page)
    expect(managerWrites(state)).toEqual(writes)
    expect(state.requests.filter(request => request.path.endsWith('/manager/preview')).every(request => !('temporary_crs' in request.query) && !('source_crs' in request.query))).toBe(true)
    await page.setViewportSize({ width: 900, height: 1000 })
    const control = page.locator('.temporary-crs-select')
    const box = await control.boundingBox()
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(901)
    await control.hover()
    await control.locator('.el-select__clear').click()
    await expect(page.locator('.table-preview .ol-viewport')).toHaveCount(0)
    await chooseCRS(page, label, 'EPSG:3857')
    await expect(control).toContainText('EPSG:3857')
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.locator('.container-preview .child-select').click()
    await page.getByRole('option', { name: /^roads/ }).click()
    await expect(page.getByRole('cell', { name: 'roads-1', exact: true })).toBeVisible()
    await expect(page.locator('.temporary-crs-select')).not.toContainText('EPSG:3857')
    await expect(page.locator('.table-preview .ol-viewport')).toHaveCount(0)
    await chooseCRS(page, label, 'EPSG:4326')
    await expectPaint(page)
    await page.getByRole('treeitem', { name: 'known.gml', exact: true }).click()
    await expect(page.locator('.temporary-crs-select')).toHaveCount(0)
    await expectPaint(page)
    await page.getByRole('treeitem', { name: state.name, exact: true }).click()
    await expect(page.getByRole('cell', { name: 'sites-1', exact: true })).toBeVisible()
    await expect(page.locator('.temporary-crs-select')).not.toContainText('EPSG:3857')
    await expect(page.locator('.table-preview .ol-viewport')).toHaveCount(0)
    await page.screenshot({ path: testInfo.outputPath('temporary-crs-reset.png'), fullPage: true })
    expect(state.errors).toEqual([])
  })
}
test('single-file table keeps its temporary CRS across response replacement and clears it on reload', async ({ page }) => {
  const state = await fixture(page, 'zh-cn', false)
  const label = '本次预览的源坐标系'
  await expect(page.getByRole('cell', { name: 'single-1', exact: true })).toBeVisible()
  await chooseCRS(page, label, 'EPSG:3857')
  await expectPaint(page)
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.getByRole('cell', { name: 'single-21', exact: true })).toBeVisible()
  await expect(page.locator('.temporary-crs-select')).toContainText('EPSG:3857')
  await expectPaint(page)
  await page.reload()
  await expect(page.locator('.temporary-crs-select')).not.toContainText('EPSG:3857')
  await expect(page.locator('.table-preview .ol-viewport')).toHaveCount(0)
  expect(state.errors).toEqual([])
})
function json(route, body) { return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) }) }
