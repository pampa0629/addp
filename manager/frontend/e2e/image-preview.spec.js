import { test, expect } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'
const locator = 'addp://engine/39/path/addp-demo/media/demo.png?type=object&item_id=1201'
const rootLocator = 'addp://engine/39/path/?type=service&node_id=200'
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aX1sAAAAASUVORK5CYII=', 'base64')
async function install(page, denied = false) {
  const requests = []
  const engine = { id: 39, name: 'Business RustFS', engine_type: 'rustfs', lifecycle_state: 'active', connection_status: 'online' }
  const item = { id: locator, locator, label: 'demo.png', type: 'object', path: 'addp-demo/media/demo.png', children: [], metadata: { item_id: 1201, data_type: 'media', format: 'png' } }
  const root = { id: rootLocator, locator: rootLocator, label: engine.name, type: 'service', hasChildren: true, loaded: true, children: [item] }
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    const json = body => route.fulfill({ json: body })
    if (path === '/api/v1/system/refresh') return json({ access_token: 'addp_at_image_user', expires_in: 3600 })
    if (path === '/api/v1/system/users/me') return json({ id: '1', display_name: 'image-user' })
    if (path === '/api/v1/system/auth/context') return json(managerAuthContext)
    if (path === '/api/v1/manager/engines') return json({ data: [engine] })
    if (path.endsWith('/ancestors')) return json({ target_locator: locator, ancestors: [root, item] })
    if (path.endsWith('/node')) return json({ parent_locator: rootLocator, children: [item] })
    if (path === '/api/v1/meta/resource-tree/39') return json(root)
    if (path === '/api/v1/meta/items/1201') return json({ id: 1201, item_type: 'object', full_name: item.path, attributes: { item: { data_type: 'media', format: 'png', layout: 'single' } } })
    if (path === '/api/v1/manager/preview') return json({ preview_type: 'object', data: { mode: 'object', object: { path: 'media/demo.png', content_type: 'image/png', content: { kind: 'image', frontend_renderer: 'image', preview_material: 'url', url: '/api/v1/manager/storage-stream?locator=fixture', metadata: { format: 'png' } } } } })
    if (path === '/api/v1/manager/storage-stream') {
      const auth = route.request().headers().authorization
      requests.push(auth || null)
      if (denied || auth !== 'Bearer addp_at_image_user') return route.fulfill({ status: 401, json: { error: 'Unauthorized' } })
      return route.fulfill({ contentType: 'image/png', body: png })
    }
    return json({})
  })
  return requests
}
test('PNG stream uses current User Bearer and releases its Blob on unmount', async ({ page }) => {
  const requests = await install(page)
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
  const image = page.getByRole('img', { name: 'media/demo.png', exact: true })
  await expect(image).toBeVisible()
  await expect.poll(() => image.evaluate(e => e.naturalWidth)).toBe(1)
  expect(requests).toEqual(['Bearer addp_at_image_user'])
  const blob = await image.getAttribute('src')
  expect(blob).toMatch(/^blob:/)
  await page.getByRole('treeitem', { name: 'Business RustFS', exact: true }).getByText('Business RustFS', { exact: true }).click()
  await expect(image).toHaveCount(0)
  await expect.poll(() => page.evaluate(async url => { try { await fetch(url); return false } catch { return true } }, blob)).toBe(true)
})
test('PNG denied stream shows failure without bare-image fallback or retry', async ({ page }) => {
  const requests = await install(page, true)
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
  await expect(page.locator('.image-preview .placeholder')).toContainText('解码失败')
  await expect(page.getByRole('img', { name: 'media/demo.png', exact: true })).toHaveCount(0)
  expect(requests).toEqual(['Bearer addp_at_image_user'])
})
