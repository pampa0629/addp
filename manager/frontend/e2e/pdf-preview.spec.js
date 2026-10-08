import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const locator = 'addp://engine/2/path/example/cache.pdf?type=file&item_id=8'

test('PDF page cache preserves rendered pages without Canvas readback warnings', async ({ page }) => {
  const diagnostics = []
  page.on('console', message => {
    if (['warning', 'error'].includes(message.type())) {
      diagnostics.push({ type: message.type(), text: message.text(), location: message.location() })
    }
  })
  page.on('pageerror', error => diagnostics.push({ type: 'pageerror', text: error.message }))

  await page.addInitScript(() => {
    localStorage.setItem('addp-lang', 'zh-cn')
    // Keep the real shared component and Chromium canvas; only PDF.js document IO is a fixture.
    window.pdfFixtureRenders = []
    window.pdfjsLib = {
      getDocument: () => ({
        destroy: async () => {},
        promise: Promise.resolve({
          numPages: 3,
          destroy: async () => {},
          getPage: async number => ({
            getViewport: ({ scale }) => ({ width: 240 * scale, height: 160 * scale }),
            render: ({ canvasContext }) => ({
              promise: Promise.resolve().then(() => {
                window.pdfFixtureRenders.push(number)
                canvasContext.fillStyle = ['#aa1100', '#00aa11', '#1100aa'][number - 1]
                canvasContext.fillRect(0, 0, canvasContext.canvas.width, canvasContext.canvas.height)
              })
            })
          })
        })
      })
    }
  })
  await installBackend(page)
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
  const preview = page.locator('.pdf-preview')
  const canvas = preview.locator('canvas')
  const input = preview.locator('.page-info input')
  const buttons = preview.locator('.toolbar-left .el-button-group .el-button')
  await expect(preview.locator('.page-info')).toContainText('/ 3')
  await expect(canvas).toBeVisible()
  await expect.poll(() => page.evaluate(() => window.pdfFixtureRenders)).toEqual([1])
  const first = await canvas.evaluate(element => element.toDataURL())

  await buttons.nth(1).click()
  await expect(input).toHaveValue('2')
  await expect.poll(() => page.evaluate(() => window.pdfFixtureRenders)).toEqual([1, 2])
  const second = await canvas.evaluate(element => element.toDataURL())
  expect(second).not.toBe(first)
  await buttons.nth(1).click()
  await expect(input).toHaveValue('3')
  await expect.poll(() => page.evaluate(() => window.pdfFixtureRenders)).toEqual([1, 2, 3])
  const third = await canvas.evaluate(element => element.toDataURL())
  expect(third).not.toBe(second)
  expect(third).not.toBe(first)

  await buttons.nth(0).click()
  await expect(input).toHaveValue('2')
  await expect.poll(() => canvas.evaluate(element => element.toDataURL())).toBe(second)
  await buttons.nth(0).click()
  await expect(input).toHaveValue('1')
  await expect.poll(() => canvas.evaluate(element => element.toDataURL())).toBe(first)
  expect(await page.evaluate(() => window.pdfFixtureRenders)).toEqual([1, 2, 3])
  expect(diagnostics).toEqual([])
})

async function installBackend(page) {
  const rootLocator = 'addp://engine/2/path/?type=root&node_id=20'
  const directoryLocator = 'addp://engine/2/path/example?type=directory&node_id=21'
  const root = { id: rootLocator, locator: rootLocator, label: 'PDF fixture', type: 'root', hasChildren: true, loaded: true }
  const item = { id: locator, locator, label: 'cache.pdf', type: 'file', path: 'example/cache.pdf', hasChildren: false,
    metadata: { item_id: 8, data_type: 'document', format: 'pdf' } }
  const directory = { id: directoryLocator, locator: directoryLocator, label: 'example', type: 'directory',
    path: 'example', hasChildren: true, loaded: true, children: [item] }
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/pdf-preview.js'] }))
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'pdf-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'pdf-e2e', local_account: { username: 'pdf-e2e' } })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [root, directory, item] })
    if (path.endsWith('/meta/resource-tree/2')) return json(route, { ...root, children: [directory] })
    if (path.endsWith('/meta/resource-tree/2/node')) return json(route, { parent_locator: directoryLocator, children: [item] })
    if (path.endsWith('/manager/preview')) return json(route, {
      preview_type: 'object', data: { mode: 'object', object: { name: 'cache.pdf', path: 'example/cache.pdf', extension: 'pdf',
        content: { kind: 'pdf', frontend_renderer: 'pdf', preview_material: 'url', url: '/fixture/cache.pdf' } } }
    })
    return json(route, {})
  })
}

function json(route, body) {
  return route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
}
