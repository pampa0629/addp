import { test, expect } from '@playwright/test'

const LOCATOR = 'addp://engine/9/path/images/private.tif?type=file&item_id=1'
const ROOT = 'addp://engine/9/path/?type=root&node_id=2'
const NAME = 'private.tif'
const attrs = { schema_version: 1, item: { data_type: 'media', format: 'tiff', layout: 'single' } }
const itemMeta = { fingerprint: 'a'.repeat(64), item_type: 'file', full_name: 'images/private.tif',
  attributes: Object.entries(attrs).map(([key, value]) => ({ key, value })) }
function tinyTIFF() {
  const bytes = Buffer.alloc(202)
  bytes.write('II'); bytes.writeUInt16LE(42, 2); bytes.writeUInt32LE(8, 4)
  const entries = [[256, 4, 2], [257, 4, 1], [258, 3, 8], [259, 3, 1], [262, 3, 1],
    [273, 4, 200], [277, 3, 1], [278, 4, 1], [279, 4, 2]]
  bytes.writeUInt16LE(entries.length, 8)
  entries.forEach(([tag, type, value], index) => {
    const offset = 10 + index * 12
    bytes.writeUInt16LE(tag, offset); bytes.writeUInt16LE(type, offset + 2)
    bytes.writeUInt32LE(1, offset + 4); bytes.writeUInt32LE(value, offset + 8)
  })
  bytes[200] = 32; bytes[201] = 224
  return bytes
}
async function install(page, locale, { delayedCapability } = {}) {
  const state = { access: 'metadata', requests: [], writes: [], released: false }
  const raw = tinyTIFF()
  const context = { type: 'tenant', tenant_id: '1' }
  const node = { id: LOCATOR, locator: LOCATOR, label: NAME, type: 'file', path: 'images/private.tif',
    metadata: { item_id: 1, data_type: 'media', format: 'tiff' }, children: [] }
  const root = { id: ROOT, locator: ROOT, label: 'Metadata NFS', type: 'root', hasChildren: true, loaded: true, children: [node] }
  await page.addInitScript(locale => localStorage.setItem('addp-lang', locale), locale)
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const method = request.method()
    state.requests.push({ method, path, access: state.access })
    const json = (body, status = 200) => route.fulfill({ status, json: body, headers: { 'Cache-Control': 'no-store' } })
    if (path === '/api/v1/system/refresh') return json({ access_token: 'metadata-ui-fixture', expires_in: 3600 })
    if (path === '/api/v1/system/users/me') return json({ id: 33, username: 'metadata-ui-fixture' })
    if (path === '/api/v1/system/auth/context') return json({ principal: { type: 'user', id: '33' }, context,
      authorization: { role_assignments: [{ scope: context, permissions: ['manager.data_item.read', 'meta.catalog.read',
        'manager.derived_artifact.read', 'manager.derived_artifact.create'] }] } })
    if (method !== 'GET') { state.writes.push({ method, path }); return json({ error: 'unexpected mutation' }, 403) }
    if (path === '/api/v1/manager/engines') return json({ data: [{ id: 9, name: 'Metadata NFS', engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json({ target_locator: LOCATOR, ancestors: [root, node] })
    if (path.endsWith('/node')) return json({ parent_locator: ROOT, children: [node] })
    if (path === '/api/v1/meta/resource-tree/9') return json(root)
    if (path === '/api/v1/meta/items/1') return state.access === 'none' ? json({ error: 'hidden' }, 404) : json({ id: 1, ...itemMeta, attributes: attrs })
    if (path === '/api/v1/manager/resource-actions') {
      // A former read capability must not override the current metadata-only preview.
      return json({ actions: { download: { supported: true }, export: { supported: false } } })
    }
    if (path === '/api/v1/manager/quick-view/capability') {
      const access = state.access
      if (delayedCapability && access === 'read') { await delayedCapability; state.released = true }
      if (access === 'none') return json({ error: 'hidden' }, 404)
      if (delayedCapability && access === 'read') return json({ status: 'ready', can_use_quick_view: true,
        available_actions: ['generate_raster_cog'], preferred_mode: 'map_quick_view', render_source: 'client_cog_render',
        raster: { cog_url: '/api/v1/manager/raster_cog/9/content' }, quick_view: { raster_cog_url: '/api/v1/manager/raster_cog/9/content' } })
      return json(access === 'metadata'
        ? { status: 'metadata_only', can_use_quick_view: false, available_actions: [] }
        : { status: 'unavailable', can_use_quick_view: false, available_actions: [], preferred_mode: 'basic_preview' })
    }
    if (path === '/api/v1/manager/preview-state') return json({ preferred_mode: 'basic_preview' })
    if (path === '/api/v1/manager/preview') {
      if (state.access === 'none') return json({ error: 'hidden' }, 404)
      if (state.access === 'metadata') return json({ preview_type: 'metadata_only', data: {
        mode: 'metadata_only', rows: [], columns: [], item_meta: itemMeta }, metadata: { locator: LOCATOR } })
      return json({ preview_type: 'object', data: { mode: 'object', rows: [], columns: [], item_meta: { ...itemMeta, attributes: [
        ...itemMeta.attributes, { key: 'storage', value: { total_size: raw.length, content_versions: [{ sha256: 'private-content-digest' }] } }] },
        object: { path: 'images/private.tif', size_bytes: raw.length, content_type: 'image/tiff', attributes: {
          ...attrs, format_info: { tiff: { page_count: 1, page_summary_status: 'parsed', pages: [{ ifd_index: 0, width: 2, height: 1 }] } } },
        content: { kind: 'image', frontend_renderer: 'image', preview_material: 'url',
          url: '/api/v1/manager/storage-stream?locator=fixture', metadata: { format: 'tiff' } } } } })
    }
    if (path === '/api/v1/manager/storage-stream') {
      if (state.access !== 'read') return json({ error: 'denied' }, 403)
      const range = /^bytes=(\d+)-(\d+)$/.exec(request.headers().range || '')
      if (!range) return json({ error: 'range required' }, 400)
      const start = Number(range[1]), end = Math.min(Number(range[2]), raw.length - 1)
      return route.fulfill({ status: 206, headers: { 'Content-Type': 'image/tiff', 'Content-Range': `bytes ${start}-${end}/${raw.length}` }, body: raw.subarray(start, end + 1) })
    }
    return json({ error: 'unexpected business request' }, 403)
  })
  return state
}
function contentRequests(state) { return state.requests.filter(r => /storage-stream|downloads\/|profile|raster_cog\//.test(r.path)) }
async function metadataOnly(manager, title) {
  await expect(manager.getByTestId('metadata-only-preview')).toBeVisible()
  await expect(manager.getByTestId('metadata-only-preview')).toContainText(title)
  await expect(manager.locator('.panel-actions')).toHaveCount(0)
  await expect(manager.locator('.preview-content')).toHaveCount(0)
  await expect(manager.locator('.profile-tab-pane')).toHaveCount(0)
  await expect(manager.locator('.text-preview-container')).toHaveCount(0)
}
for (const [locale, title, properties] of [['zh-cn', '仅可查看元数据', '属性'], ['en', 'Metadata access only', 'Attributes']]) {
  test(`${locale}: Console metadata, read and withdrawn states keep content boundaries`, async ({ page }) => {
    const state = await install(page, locale)
    const initialPreview = page.waitForResponse(response => { const url = new URL(response.url()); return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === LOCATOR })
    await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(LOCATOR)}`)
    await initialPreview
    const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
    await metadataOnly(manager, title)
    expect(contentRequests(state)).toEqual([])
    await manager.getByRole('tab', { name: properties, exact: true }).click()
    await expect(manager.locator('.attributes-panel')).toContainText('single')
    await expect(manager.locator('.attributes-panel')).not.toContainText('1844')
    state.access = 'read'
    await manager.getByText(NAME, { exact: true }).click()
    await expect(manager.locator('.tiff-canvas')).toBeVisible()
    await expect(manager.locator('.tiff-canvas')).toHaveAttribute('width', '2')
    expect(contentRequests(state).some(r => r.path.endsWith('/storage-stream'))).toBe(true)
    state.access = 'metadata'
    await manager.getByText(NAME, { exact: true }).click()
    await metadataOnly(manager, title)
    expect(contentRequests(state).filter(r => r.access === 'metadata')).toEqual([])
    await manager.getByRole('tab', { name: properties, exact: true }).click()
    await expect(manager.locator('.attributes-panel')).not.toContainText('private-content-digest')
    state.access = 'none'
    await manager.getByText(NAME, { exact: true }).click()
    await expect(manager.getByTestId('preview-failure')).toBeVisible()
    await expect(manager.getByRole('tab', { name: properties, exact: true })).toHaveCount(0)
    await expect(manager.getByTestId('metadata-only-preview')).toHaveCount(0)
    await expect(manager.locator('.panel-actions')).toHaveCount(0)
    await expect(manager.locator('.preview-content')).toHaveCount(0)
    expect(contentRequests(state).filter(r => r.access === 'none')).toEqual([])
    expect(state.writes).toEqual([])
  })
}
test('late read capability cannot restore content after a metadata downgrade', async ({ page }) => {
  let release
  const state = await install(page, 'zh-cn', { delayedCapability: new Promise(resolve => { release = resolve }) })
  state.access = 'read'
  const initialPreview = page.waitForResponse(response => { const url = new URL(response.url()); return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === LOCATOR })
  await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(LOCATOR)}`)
  await initialPreview
  const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(manager.locator('.tiff-canvas')).toBeVisible()
  state.access = 'metadata'
  await manager.getByText(NAME, { exact: true }).click()
  await metadataOnly(manager, '仅可查看元数据')
  const delivered = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/manager/quick-view/capability')
  release()
  await (await delivered).finished()
  await manager.locator('body').evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
  await expect.poll(() => state.released).toBe(true)
  await metadataOnly(manager, '仅可查看元数据')
  expect(contentRequests(state).filter(r => r.access === 'metadata')).toEqual([])
  expect(state.writes).toEqual([])
})
