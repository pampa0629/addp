import { test, expect } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const ENGINE = { id: 12, name: 'Business NFS', engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online' }
const ROOT = 'addp://engine/12/path/?type=root&node_id=200'
const LOCATOR = 'addp://engine/12/path/pages.tiff?type=file&item_id=1201'
const PAGES = [{ ifd_index: 0, width: 2, height: 1 }, { ifd_index: 3, width: 1, height: 2 }]

function fixture(farDirectory = false) {
  // Four top-level IFDs: two pages, one overview and one transparency mask.
  const images = [
    { width: 2, height: 1, bits: [8, 8, 8, 8], pi: 2, kind: 0, extras: [2], pixels: [255, 0, 0, 0, 0, 0, 255, 255] },
    { width: 1, height: 1, bits: [8], pi: 1, kind: 1, pixels: [10] },
    { width: 1, height: 1, bits: [8], pi: 1, kind: 4, pixels: [20] },
    { width: 1, height: 2, bits: [16], pi: 1, kind: 2, pixels: [65535, 0] }
  ]
  const firstIFD = 8 + (farDirectory ? 65536 : 0)
  const bytes = Buffer.alloc(firstIFD + images.length * 256 + 32)
  bytes.write('II'); bytes.writeUInt16LE(42, 2); bytes.writeUInt32LE(firstIFD, 4)
  let pixelOffset = firstIFD + images.length * 256
  images.forEach((image, index) => {
    const offset = firstIFD + index * 256
    const countBytes = image.pixels.length * image.bits[0] / 8
    const tags = [
      [254, 4, [image.kind]], [256, 4, [image.width]], [257, 4, [image.height]],
      [258, 3, image.bits], [259, 3, [1]], [262, 3, [image.pi]], [273, 4, [pixelOffset]],
      [277, 3, [image.bits.length]], [278, 4, [image.height]], [279, 4, [countBytes]], [284, 3, [1]]
    ]
    if (image.extras) tags.push([338, 3, image.extras])
    bytes.writeUInt16LE(tags.length, offset)
    tags.forEach(([tag, type, values], i) => {
      const entry = offset + 2 + i * 12
      bytes.writeUInt16LE(tag, entry); bytes.writeUInt16LE(type, entry + 2)
      bytes.writeUInt32LE(values.length, entry + 4)
      const inline = values.length * (type === 3 ? 2 : 4) <= 4
      const valueOffset = inline ? entry + 8 : offset + 200
      if (!inline) bytes.writeUInt32LE(valueOffset, entry + 8)
      values.forEach((value, j) => type === 3
        ? bytes.writeUInt16LE(value, valueOffset + j * 2)
        : bytes.writeUInt32LE(value, valueOffset + j * 4))
    })
    bytes.writeUInt32LE(index === images.length - 1 ? 0 : offset + 256, offset + 2 + tags.length * 12)
    image.pixels.forEach((value, i) => image.bits[0] === 16
      ? bytes.writeUInt16LE(value, pixelOffset + i * 2) : bytes.writeUInt8(value, pixelOffset + i))
    pixelOffset += countBytes
  })
  return bytes.subarray(0, pixelOffset)
}

async function installBackend(page, { summary = { page_summary_status: 'parsed', page_count: 2, pages: PAGES }, fullResponse = false, external = false, locale = 'zh-cn', firstRequestGate, farDirectory = false, gateRequest = 1 } = {}) {
  const binary = fixture(farDirectory)
  const requests = []
  const item = { id: LOCATOR, locator: LOCATOR, label: 'pages.tiff', type: 'file', path: 'pages.tiff', children: [], metadata: { item_id: 1201, data_type: 'media', format: 'tiff' } }
  const root = { id: ROOT, locator: ROOT, label: ENGINE.name, type: 'root', hasChildren: true, loaded: true, children: [item] }
  await page.addInitScript(locale => { localStorage.setItem('addp-lang', locale) }, locale)
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    const json = body => route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
    if (path === '/api/v1/system/refresh') return json({ access_token: 'manager-e2e-token', expires_in: 3600 })
    if (path === '/api/v1/system/users/me') return json({ id: '1', display_name: 'manager-e2e', local_account: { username: 'manager-e2e' } })
    if (path === '/api/v1/system/auth/context') return json(managerAuthContext)
    if (path === '/api/v1/manager/engines') return json({ data: [ENGINE] })
    if (path.endsWith('/ancestors')) return json({ target_locator: LOCATOR, ancestors: [root, item] })
    if (path.endsWith('/node')) return json({ parent_locator: ROOT, children: [item] })
    if (path === `/api/v1/meta/resource-tree/${ENGINE.id}`) return json(root)
    if (path === '/api/v1/meta/items/1201') return json({ id: 1201, item_type: 'file', full_name: 'pages.tiff', attributes: { item: { data_type: 'media', format: 'tiff', layout: 'single' }, format_info: { tiff: summary } } })
    if (path === '/api/v1/manager/preview') return json({ preview_type: 'object', data: {
      mode: 'object', object: { path: 'pages.tiff', size_bytes: binary.length, content_type: 'image/tiff',
        attributes: { item: { data_type: 'media', format: 'tiff', layout: 'single' }, type_info: { media: { width: 2, height: 1 } }, format_info: { tiff: summary } },
        content: { kind: 'image', frontend_renderer: 'image', preview_material: 'url', url: external ? 'https://external.invalid/pages.tiff' : '/api/v1/manager/storage-stream?locator=fixture', metadata: { format: 'tiff' } }
      }
    } })
    if (path === '/api/v1/manager/storage-stream') {
      const range = route.request().headers().range
      requests.push({ range, auth: route.request().headers().authorization })
      if (requests.length === gateRequest && firstRequestGate) await firstRequestGate
      if (fullResponse) return route.fulfill({ status: 200, body: binary })
      const match = /^bytes=(\d+)-(\d+)$/.exec(range || '')
      if (!match) return route.fulfill({ status: 400 })
      const start = Number(match[1]); const end = Math.min(binary.length - 1, Number(match[2]))
      return route.fulfill({ status: 206, headers: { 'Content-Type': 'image/tiff', 'Content-Range': `bytes ${start}-${end}/${binary.length}`, 'Accept-Ranges': 'bytes' }, body: binary.subarray(start, end + 1) })
    }
    return json({})
  })
  return requests
}

async function open(page) {
  await page.goto(`/data-explorer?locator=${encodeURIComponent(LOCATOR)}`)
}

async function pixels(canvas) {
  return canvas.evaluate(canvas => ({ width: canvas.width, height: canvas.height, rgba: [...canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data] }))
}

test('TIFF pagination follows directory indices, preserves alpha and scales RGB/gray samples', async ({ page }) => {
  const requests = await installBackend(page)
  await open(page)
  const canvas = page.locator('.tiff-canvas')
  const previous = page.getByRole('button', { name: '上一页', exact: true })
  const next = page.getByRole('button', { name: '下一页', exact: true })
  await expect(canvas).toBeVisible()
  await expect(previous).toBeDisabled()
  await expect(page.getByRole('group', { name: 'TIFF 分页预览' })).toContainText('第 1 页 / 共 2 页')
  // Canvas stores transparent RGB as zero. Alpha=0 must never become opaque.
  expect(await pixels(canvas)).toEqual({ width: 2, height: 1, rgba: [0, 0, 0, 0, 0, 0, 255, 255] })
  await next.click()
  await expect(page.getByRole('group', { name: 'TIFF 分页预览' })).toContainText('第 2 页 / 共 2 页')
  await expect(canvas).toBeVisible()
  expect(await pixels(canvas)).toEqual({ width: 1, height: 2, rgba: [255, 255, 255, 255, 0, 0, 0, 255] })
  await expect(next).toBeDisabled()
  await previous.click()
  await expect(canvas).toBeVisible()
  expect((await pixels(canvas)).width).toBe(2)
  expect(requests.length).toBeGreaterThan(0)
  expect(requests.every(request => /^bytes=/.test(request.range) && request.auth === 'Bearer manager-e2e-token')).toBeTruthy()
})

for (const status of [undefined, 'invalid', 'budget_exceeded']) {
  test(`TIFF incomplete directory (${status}) prompts refresh without reading pixels`, async ({ page }) => {
    const requests = await installBackend(page, { summary: status ? { page_summary_status: status } : null })
    await open(page)
    await expect(page.locator('.image-preview')).toContainText('请深度刷新该文件')
    await expect(page.locator('.tiff-canvas')).not.toBeVisible()
    expect(requests).toEqual([])
  })
}

test('BigTIFF has an explicit unsupported message', async ({ page }) => {
  const requests = await installBackend(page, { summary: { page_summary_status: 'unsupported' } })
  await open(page)
  await expect(page.locator('.image-preview')).toContainText('暂不支持 BigTIFF')
  expect(requests).toEqual([])
})

test('a server ignoring Range cannot fall back to full-file TIFF decoding', async ({ page }) => {
  const requests = await installBackend(page, { fullResponse: true })
  await open(page)
  await expect(page.locator('.image-preview')).toContainText('TIFF 当前页读取或解码失败')
  await expect(page.locator('.tiff-canvas')).not.toBeVisible()
  expect(requests.every(request => /^bytes=/.test(request.range))).toBeTruthy()
})

test('the pixel budget is checked before the Range request', async ({ page }) => {
  const requests = await installBackend(page, { summary: { page_summary_status: 'parsed', page_count: 1, pages: [{ ifd_index: 0, width: 16000001, height: 1 }] } })
  await open(page)
  await expect(page.locator('.image-preview')).toContainText('超过 1600 万像素')
  expect(requests).toEqual([])
})

test('external TIFF URLs never receive platform credentials', async ({ page }) => {
  let externalRequests = 0
  await page.route('https://external.invalid/**', route => { externalRequests++; return route.abort() })
  await installBackend(page, { external: true, locale: 'en' })
  await open(page)
  await expect(page.locator('.image-preview')).toContainText('requires a platform Range URL')
  expect(externalRequests).toBe(0)
})


test('switching pages during a pending read cancels the old render', async ({ page }) => {
  let release
  const gate = new Promise(resolve => { release = resolve })
  const failures = []
  page.on('requestfailed', request => {
    if (request.url().includes('/manager/storage-stream')) failures.push(request.failure()?.errorText)
  })
  const requests = await installBackend(page, { firstRequestGate: gate, farDirectory: true, gateRequest: 2 })
  await open(page)
  // The header has arrived; the pending request is for an IFD outside block 0.
  await expect.poll(() => requests.length).toBe(2)
  await expect(page.locator('.image-preview')).toContainText('正在解析 TIFF 当前页')
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  const canvas = page.locator('.tiff-canvas')
  await expect(canvas).toBeVisible()
  expect((await pixels(canvas)).height).toBe(2)
  await expect.poll(() => failures.some(error => error?.includes('ERR_ABORTED'))).toBe(true)
  release()
  // The next interaction must also remain functional after the canceled response.
  await page.getByRole('button', { name: '上一页', exact: true }).click()
  await expect(canvas).toBeVisible()
  expect((await pixels(canvas)).width).toBe(2)
})

for (const locale of ['zh-cn', 'en']) {
  test(`TIFF page directory attributes are localized in ${locale}`, async ({ page }) => {
    await installBackend(page, { locale })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(LOCATOR)}&tab=attributes`)
    await expect(page.getByText(locale === 'zh-cn' ? 'TIFF 页目录状态' : 'TIFF page directory status', { exact: true })).toBeVisible()
    await expect(page.getByText(locale === 'zh-cn' ? '已完整解析' : 'Completely parsed', { exact: true })).toBeVisible()
    await expect(page.getByText(locale === 'zh-cn' ? 'TIFF 页面目录' : 'TIFF page directory', { exact: true })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: locale === 'zh-cn' ? '图像目录索引' : 'Image directory index', exact: true })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: locale === 'zh-cn' ? '页宽（像素）' : 'Page width (pixels)', exact: true })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: locale === 'zh-cn' ? '页高（像素）' : 'Page height (pixels)', exact: true })).toBeVisible()
  })
}
