import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const observations = [
  { unit: 'A', at: '2026-10-16T02:00:00Z', east: 117, north: 32 },
  { unit: 'B', at: '2026-10-16T01:00:00Z', east: 118, north: 33 },
  { unit: 'A', at: '2026-10-16T09:00:00+08:00', east: 116, north: 31 }
]
async function fixture(page, language, fullPage, format = 'csv', transform = data => data) {
  const locator = `addp://engine/2/path/observations.${format}?type=file&item_id=8`
  const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
  const item = { id: locator, locator, label: `observations.${format}`, type: 'file', hasChildren: false }
  const root = { id: rootLocator, locator: rootLocator, label: 'Trajectory source', type: 'server', hasChildren: true, children: [item] }
  const requests = [], errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), language)
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/table-preview.js'] }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'trajectory-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'trajectory-e2e', local_account: { username: 'trajectory-e2e' } })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
    if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(route, root)
    if (path.endsWith('/manager/preview')) {
      const size = Number(url.searchParams.get('page_size'))
      const selection = url.searchParams.has('trajectory_selection') ? JSON.parse(url.searchParams.get('trajectory_selection')) : null
      requests.push({ locator: url.searchParams.get('locator'), page: Number(url.searchParams.get('page')), size, ...(selection ? { selection } : {}) })
      const data = transform({
        mode: 'table', columns: ['unit', 'at', 'east', 'north'], rows: observations, total: 3, has_more: false, page: 1, page_size: size,
        object: { path: `observations.${format}`, content: { kind: format, metadata: { format } } },
        item_meta: { item_type: 'file', full_name: 'observations.csv', attributes: [{ key: 'item', value: { layout: 'single', data_type: 'table', format } }] }
      })
      if (size === 2000) return fullPage(route, data, selection)
      return json(route, { preview_type: 'table', data })
    }
    return json(route, {})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
  return { requests, errors, locator }
}
async function choose(page, name, value) {
  const combo = page.getByRole('combobox', { name, exact: true })
  await combo.press('ArrowDown')
  const listID = await combo.getAttribute('aria-controls')
  expect(listID).toBeTruthy()
  await page.locator(`[id="${listID}"]`).getByRole('option', { name: value, exact: true }).click()
}
async function expectMapPaint(dialog) {
  const canvas = dialog.locator('.ol-viewport canvas')
  await expect(canvas).toBeVisible()
  await expect.poll(() => canvas.evaluate(element => {
    const context = element.getContext('2d')
    if (!context || !element.width || !element.height) return 0
    const pixels = context.getImageData(0, 0, element.width, element.height).data
    let painted = 0
    for (let offset = 3; offset < pixels.length; offset += 4) {
      if (pixels[offset] > 0 && ++painted > 300) return painted
    }
    return painted
  })).toBeGreaterThan(300)
}
async function configure(page, language, encoding = null, draw = true) {
  const chinese = language === 'zh-cn'
  for (const [name, value] of [[chinese ? '设备字段' : 'Device field', 'unit'], [chinese ? '时间字段' : 'Time field', 'at'], [chinese ? 'X / 经度字段' : 'X / longitude field', 'east'], [chinese ? 'Y / 纬度字段' : 'Y / latitude field', 'north'], [chinese ? '源坐标系' : 'Source CRS', 'EPSG:4326 — WGS84']]) await choose(page, name, value)
  if (encoding) await choose(page, chinese ? '时间格式' : 'Time encoding', encoding)
  if (draw) await page.getByRole('button', { name: chinese ? '绘制轨迹' : 'Draw trajectories', exact: true }).click()
}
for (const language of ['zh-cn', 'en']) {
  test(`static trajectories use a complete protected preview and explicit fields (${language})`, async ({ page }, testInfo) => {
    const state = await fixture(page, language, (route, data) => json(route, { preview_type: 'table', data }))
    await page.getByRole('button', { name: language === 'zh-cn' ? '静态轨迹' : 'Static trajectory', exact: true }).click()
    await configure(page, language)
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('status')).toContainText(language === 'zh-cn' ? '2 台设备，3 个采样点' : '2 devices, 3 observations')
    await expectMapPaint(dialog)
    await expect(dialog.locator('.trajectory-map-controls')).toContainText(language === 'zh-cn' ? '无底图' : 'No base map')
    expect(state.requests.filter(request => request.size === 2000)).toEqual([{ locator: state.locator, page: 1, size: 2000 }])
    await page.setViewportSize({ width: 900, height: 900 })
    const box = await dialog.boundingBox()
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(901)
    await page.setViewportSize({ width: 1280, height: 1100 })
    await expectMapPaint(dialog)
    await page.screenshot({ path: testInfo.outputPath('static-trajectory.png'), fullPage: true })
    await dialog.getByRole('button', { name: language === 'zh-cn' ? '关闭此对话框' : 'Close this dialog' }).click()
    await page.getByRole('button', { name: language === 'zh-cn' ? '静态轨迹' : 'Static trajectory', exact: true }).click()
    await expect(page.getByRole('dialog').locator('.ol-viewport canvas')).toHaveCount(0)
    await expect(page.getByRole('dialog').locator('.el-select').filter({ has: page.getByRole('combobox', { name: language === 'zh-cn' ? '设备字段' : 'Device field', exact: true }) })).not.toContainText('unit')
    expect(state.errors).toEqual([])
  })
}
test('an incomplete response never becomes a trajectory of the first page', async ({ page }) => {
  await fixture(page, 'zh-cn', (route, data) => json(route, { preview_type: 'table', data: { ...data, total: null, has_more: true } }))
  await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
  await expect(page.getByRole('dialog').getByRole('alert')).toContainText('完整性未知')
  await expect(page.getByRole('dialog').locator('.el-switch').filter({ has: page.getByRole('switch', { name: '按设备和时间范围读取' }) })).toBeVisible()
  await expect(page.getByRole('dialog').locator('canvas')).toHaveCount(0)
})
test('a conflicting timestamp refuses all routes instead of silently skipping a record', async ({ page }) => {
  await fixture(page, 'zh-cn', (route, data) => json(route, { preview_type: 'table', data: { ...data, rows: [observations[0], { ...observations[0], east: 118 }], total: 2 } }))
  await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
  await configure(page, 'zh-cn')
  await expect(page.getByRole('dialog').getByRole('alert')).toContainText('同设备、同一时刻')
  await expect(page.getByRole('dialog').locator('canvas')).toHaveCount(0)
})
test('authorization failure does not use rows from the existing table', async ({ page }) => {
  await fixture(page, 'zh-cn', route => json(route, { error: 'protected', error_code: 'security_protection_required' }, 403))
  await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
  await expect(page.getByRole('dialog').getByRole('alert')).toContainText('数据保护')
  await expect(page.getByRole('dialog').locator('canvas')).toHaveCount(0)
})
test('records JSON uses the same complete-row trajectory interface', async ({ page }) => {
  await fixture(page, 'zh-cn', (route, data) => json(route, { preview_type: 'table', data }), 'json')
  await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
  await configure(page, 'zh-cn')
  await expect(page.getByRole('dialog').getByRole('status')).toContainText('2 台设备，3 个采样点')
  await expectMapPaint(page.getByRole('dialog'))
})
test('Unix seconds preserve exact milliseconds in the explicit time selector', async ({ page }) => {
  await fixture(page, 'zh-cn', (route, data) => json(route, { preview_type: 'table', data }), 'csv', data => ({
    ...data, rows: data.rows.map((row, index) => ({ ...row, at: ['1.001', '2', '0'][index] }))
  }))
  await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
  await configure(page, 'zh-cn', 'Unix 秒')
  await expect(page.getByRole('dialog').getByRole('status')).toContainText('2 台设备，3 个采样点')
  await expectMapPaint(page.getByRole('dialog'))
})
test('GeoJSON Point rows use the selected geometry and identified projected CRS', async ({ page }) => {
  await fixture(page, 'zh-cn', (route, data) => json(route, { preview_type: 'table', data }), 'geojson', data => ({
    ...data, columns: ['unit', 'at', 'shape'], geometry_columns: ['shape'], source_srid: 3857, source_crs: 'EPSG:3857',
    rows: observations.map((row, index) => ({ unit: row.unit, at: row.at, shape: { type: 'Point', coordinates: [index * 1000, index * 2000] } }))
  }))
  await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
  await choose(page, '设备字段', 'unit')
  await choose(page, '时间字段', 'at')
  await choose(page, '坐标来源', 'GeoJSON Point 字段')
  await choose(page, 'Point 几何字段', 'shape')
  await page.getByRole('button', { name: '绘制轨迹', exact: true }).click()
  await expect(page.getByRole('dialog').getByRole('status')).toContainText('2 台设备，3 个采样点')
  await expectMapPaint(page.getByRole('dialog'))
})
test('closing a pending read discards its late result before reopening', async ({ page }) => {
  let release, oldFinished, count = 0
  const pending = new Promise(resolve => { release = resolve })
  const oldResponse = new Promise(resolve => { oldFinished = resolve })
  const state = await fixture(page, 'zh-cn', async (route, data) => {
    count += 1
    if (count === 1) { await pending; return json(route, { preview_type: 'table', data }).catch(() => {}).finally(oldFinished) }
    return json(route, { preview_type: 'table', data: { ...data, columns: ['fresh_device', 'at', 'east', 'north'], rows: data.rows.map(row => ({ ...row, fresh_device: row.unit })) } })
  })
  await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
  await expect.poll(() => state.requests.filter(request => request.size === 2000).length).toBe(1)
  await page.getByRole('dialog').getByRole('button', { name: '关闭此对话框' }).click()
  await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
  await choose(page, '设备字段', 'fresh_device')
  release()
  await oldResponse
  await expect(page.getByRole('dialog').locator('.el-select').filter({ has: page.getByRole('combobox', { name: '设备字段', exact: true }) })).toContainText('fresh_device')
  expect(state.errors).toEqual([])
})
async function configureRange(page) {
  await configure(page, 'zh-cn', null, false)
  await page.getByRole('dialog').locator('.el-switch').filter({ has: page.getByRole('switch', { name: '按设备和时间范围读取', exact: true }) }).click()
  await page.getByRole('textbox', { name: '设备标识', exact: true }).fill('A')
  await page.getByRole('textbox', { name: '开始时间（包含，须带时区）', exact: true }).fill('2026-10-16T01:00:00Z')
  await page.getByRole('textbox', { name: '结束时间（不包含，须带时区）', exact: true }).fill('2026-10-16T03:00:00Z')
}
test('a large file reads one complete selected range and renders every matching device point', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1280, height: 1300 })
  const state = await fixture(page, 'zh-cn', (route, data, selection) => {
    if (!selection) return json(route, { preview_type: 'table', data: { ...data, total: 9000, has_more: true } })
    return json(route, { preview_type: 'table', data: { ...data, rows: observations.filter(row => row.unit === 'A'), total: 2, has_more: false, trajectory_scope: { selection, complete: true, scanned_rows: 9000 } } })
  })
  await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
  await expect(page.getByRole('dialog').getByRole('alert')).toContainText('完整性未知')
  await configureRange(page)
  await page.getByRole('button', { name: '绘制轨迹', exact: true }).click()
  await expect(page.getByRole('dialog').getByRole('status')).toContainText('1 台设备，2 个采样点')
  await expectMapPaint(page.getByRole('dialog'))
  await page.screenshot({ path: testInfo.outputPath('trajectory-range.png'), fullPage: true })
  expect(state.requests.filter(request => request.selection).map(request => request.selection)).toEqual([{ device_field: 'unit', time_field: 'at', time_encoding: 'iso', devices: ['A'], start: '2026-10-16T01:00:00Z', end: '2026-10-16T03:00:00Z' }])
  expect(state.errors).toEqual([])
})
for (const failure of ['ignored', 'budget', 'denied']) {
  test(`range selection refuses ${failure} without retaining an old route`, async ({ page }) => {
    await fixture(page, 'zh-cn', (route, data, selection) => {
      if (!selection || failure === 'ignored') return json(route, { preview_type: 'table', data })
      return json(route, { error: 'range unavailable', error_code: failure === 'budget' ? 'trajectory_scan_budget_exceeded' : 'security_protection_required' }, failure === 'budget' ? 422 : 403)
    })
    await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
    await configure(page, 'zh-cn')
    await expectMapPaint(page.getByRole('dialog'))
    await configureRange(page)
    await expect(page.getByRole('dialog').locator('canvas')).toHaveCount(0)
    await page.getByRole('button', { name: '绘制轨迹', exact: true }).click()
    await expect(page.getByRole('dialog').getByRole('alert')).toContainText(failure === 'ignored' ? '服务器未确认' : failure === 'budget' ? '文件扫描超过' : '数据保护')
    await expect(page.getByRole('dialog').locator('canvas')).toHaveCount(0)
  })
}
test('a late selected-range response cannot refill a reopened configuration', async ({ page }) => {
  let release, finished
  const pending = new Promise(resolve => { release = resolve })
  const completed = new Promise(resolve => { finished = resolve })
  const state = await fixture(page, 'zh-cn', async (route, data, selection) => {
    if (!selection) return json(route, { preview_type: 'table', data })
    await pending
    return json(route, { preview_type: 'table', data: { ...data, trajectory_scope: { selection, complete: true, scanned_rows: 3 } } }).catch(() => {}).finally(finished)
  })
  await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
  await configureRange(page)
  await page.getByRole('button', { name: '绘制轨迹', exact: true }).click()
  await expect.poll(() => state.requests.filter(request => request.selection).length).toBe(1)
  await page.getByRole('dialog').getByRole('button', { name: '关闭此对话框' }).click()
  await page.getByRole('button', { name: '静态轨迹', exact: true }).click()
  await choose(page, '设备字段', 'unit')
  release()
  await completed
  await expect(page.getByRole('dialog').locator('canvas')).toHaveCount(0)
  await expect(page.getByRole('dialog').locator('.el-switch').filter({ has: page.getByRole('switch', { name: '按设备和时间范围读取' }) })).not.toHaveClass(/is-checked/)
  expect(state.errors).toEqual([])
})
function json(route, body, status = 200) { return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) }) }
