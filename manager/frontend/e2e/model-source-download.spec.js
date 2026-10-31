import { expect, test } from '@playwright/test'
import { readFileSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { installModelDownloadAttributeObserver, modelDownloadAttributes } from './model-preview-support.js'
import { managerAuthContext } from './managerAuthContext.js'

const locator = 'addp://engine/2/path/models/model.stl?type=file&item_id=8'
const otherLocator = 'addp://engine/2/path/models/other.stl?type=file&item_id=9'
const bytes = Buffer.from('exact original model bytes')

async function install(page, respond, locale = 'zh-cn', options = {}) {
  const selectedLocator = options.locator || locator
  const label = options.label || 'model.stl'
  const attributes = options.attributes || { data_type: 'model_3d', format: 'stl', layout: 'single' }
  const node = (id, label) => ({ id, locator: id, label, type: 'file', hasChildren: false })
  const item = node(selectedLocator, label), other = node(otherLocator, 'other.stl')
  const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
  const root = { id: rootLocator, locator: rootLocator, label: 'Model engine', type: 'server', hasChildren: true, children: [item, other] }
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), locale)
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'addp_at_model-download-test', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'Model reader', local_account: { username: 'model-reader' } })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: options.engineType || 'ftp', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: selectedLocator, ancestors: [{ ...root, children: [] }, item] })
    if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(route, root)
    if (path.endsWith('/resource-actions')) return json(route, { actions: { download: { supported: options.supported !== false, requires_current_user: true } } })
    if (path.endsWith('/manager/preview')) return json(route, { preview_type: 'object', data: {
      mode: 'object', object: { attributes: { item: attributes }, content: { kind: 'text', text: 'model source' } }
    } })
    if (path.endsWith('/manager/downloads/file')) return respond(route)
    return json(route, {})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(selectedLocator)}`)
  await page.getByRole('treeitem', { name: label, exact: true }).click()
  if (options.supported === false) await expect(page.getByRole('button', { name: /^(下载|Download)$/i })).toHaveCount(0)
  else await expect(page.getByRole('button', { name: /^(下载|Download)$/i })).toBeEnabled()
}

for (const locale of ['zh-cn', 'en']) for (const [header, name, suggested] of [
  ['attachment; filename=model.stl', 'model.stl', 'model.stl'],
  ["attachment; filename*=utf-8''%20%E5%8E%9F%E5%90%8D%20%25.GLB", ' 原名 %.GLB', '_原名 %.GLB'],
  ["attachment; filename*=utf-8''%E6%A8%A1%E5%9E%8B%20%25.stl.zip", '模型 %.stl.zip', '模型 %.stl.zip']
]) {
  test(`the real model download button saves authenticated bytes as ${name} in ${locale}`, async ({ page }, testInfo) => {
    const sourceBytes = name.endsWith('.GLB') ? readFileSync(new URL('../../../business/fixtures/manager/addp_online_model_fixture.glb', import.meta.url)) : bytes
    await installModelDownloadAttributeObserver(page)
    let calls = 0
    await install(page, async route => {
      calls++
      const headers = route.request().headers()
      expect(headers.authorization).toBe('Bearer addp_at_model-download-test')
      expect(headers.cookie).toBeUndefined()
      expect([...new URL(route.request().url()).searchParams.keys()]).toEqual(['locator'])
      return route.fulfill({ status: 200, headers: { 'Content-Type': 'application/octet-stream', 'Content-Disposition': header }, body: sourceBytes })
    }, locale)
    const [download, response] = await Promise.all([
      page.waitForEvent('download'),
      page.waitForResponse(value => new URL(value.url()).pathname.endsWith('/manager/downloads/file')),
      page.getByRole('button', { name: /^(下载|Download)$/i }).click()
    ])
    expect(await download.failure()).toBeNull()
    const saved = readFileSync(await download.path())
    const anchors = await modelDownloadAttributes(page)
    const proof = { expected_name: name, response_disposition: response.headers()['content-disposition'],
      anchors, suggested_filename: download.suggestedFilename(), size: saved.length,
      sha256: createHash('sha256').update(saved).digest('hex'), exact_source_bytes: saved.equals(sourceBytes) }
    writeFileSync(testInfo.outputPath('filename-boundaries.json'), JSON.stringify(proof, null, 2))
    await testInfo.attach('model-download-filename-boundaries', { body: JSON.stringify(proof, null, 2), contentType: 'application/json' })
    expect(response.headers()['content-disposition']).toBe(header)
    expect(anchors).toEqual([{ name, blob: true }])
    expect(saved).toEqual(sourceBytes)
    expect(download.suggestedFilename()).toBe(suggested)
    expect(calls).toBe(1)
    await expect(page.getByText(/^(下载已开始|Download started)$/i)).toBeVisible()
  })
}

for (const status of [401, 403]) {
  test(`HTTP ${status} from the real model button never saves error bytes or claims success`, async ({ page }) => {
    let downloads = 0, calls = 0
    page.on('download', () => downloads++)
    await install(page, route => { calls++; return json(route, { error: '源模型读取已拒绝' }, status) })
    await page.getByRole('button', { name: /^(下载|Download)$/i }).click()
    await expect(page.locator('.el-message--error').filter({ hasText: '源模型读取已拒绝' })).toBeVisible()
    await expect(page.getByText(/^(下载已开始|Download started)$/i)).toHaveCount(0)
    expect(downloads).toBe(0)
    expect(calls).toBe(status === 401 ? 2 : 1)
  })
}

test('an empty successful response cannot save an empty model or claim success', async ({ page }) => {
  let downloads = 0
  page.on('download', () => downloads++)
  await install(page, route => route.fulfill({ status: 200, headers: { 'Content-Disposition': 'attachment; filename=model.stl' }, body: '' }))
  await page.getByRole('button', { name: /^(下载|Download)$/i }).click()
  await expect(page.locator('.el-message--error').filter({ hasText: '下载响应无效或文件名缺失' })).toBeVisible()
  await expect(page.getByText(/^(下载已开始|Download started)$/i)).toHaveCount(0)
  expect(downloads).toBe(0)
})

test('switching models cancels an in-flight source download without saving or a success toast', async ({ page }) => {
  let release, seen
  const pending = new Promise(resolve => { release = resolve })
  const requested = new Promise(resolve => { seen = resolve })
  let downloads = 0
  page.on('download', () => downloads++)
  await install(page, async route => {
    seen()
    await pending
    await route.fulfill({ status: 200, headers: { 'Content-Disposition': 'attachment; filename=model.stl' }, body: bytes }).catch(() => {})
  })
  await page.getByRole('button', { name: /^(下载|Download)$/i }).click()
  await requested
  await page.getByRole('treeitem', { name: 'other.stl', exact: true }).click()
  await expect(page).toHaveURL(/other.stl/)
  release()
  await expect(page.getByRole('button', { name: /^(下载|Download)$/i })).toBeEnabled()
  await expect(page.getByText(/^(下载已开始|Download started)$/i)).toHaveCount(0)
  expect(downloads).toBe(0)
})


const gdbLocator = 'addp://engine/2/path/exports/public_test.gdb?type=file&item_id=8'
const gdbOptions = { locator: gdbLocator, label: 'public_test.gdb', engineType: 'nfs',
  attributes: { data_type: 'container', format: 'filegdb', layout: 'whole' } }
const gdbZip = Buffer.from('UEsDBBQAAAAAANabX12bREkXFQAAABUAAAAiAAAAcHVibGljX3Rlc3QuZ2RiL2EwMDAwMDAwMS5nZGJ0YWJsZWZpeHR1cmUgc291cmNlIG1lbWJlclBLAQIUAxQAAAAAANabX12bREkXFQAAABUAAAAiAAAAAAAAAAAAAACAAQAAAABwdWJsaWNfdGVzdC5nZGIvYTAwMDAwMDAxLmdkYnRhYmxlUEsFBgAAAAABAAEAUAAAAFUAAAAAAA==', 'base64')
for (const locale of ['zh-cn', 'en']) test(`FileGDB downloads its complete server-named ZIP using the current User (${locale})`, async ({ page }) => {
  let requests = 0
  await install(page, route => {
    requests++
    expect(route.request().headers().authorization).toBe('Bearer addp_at_model-download-test')
    expect(route.request().headers().cookie).toBeUndefined()
    const url = new URL(route.request().url())
    expect([...url.searchParams.keys()]).toEqual(['locator'])
    expect(url.searchParams.get('locator')).toBe(gdbLocator)
    return route.fulfill({ status: 200, headers: { 'Content-Type': 'application/zip',
      'Content-Disposition': 'attachment; filename=public_test.gdb.zip', 'Content-Length': String(gdbZip.length) }, body: gdbZip })
  }, locale, gdbOptions)
  const [download] = await Promise.all([page.waitForEvent('download'), page.getByRole('button', {name:/^(下载|Download)$/i}).click()])
  expect(download.suggestedFilename()).toBe('public_test.gdb.zip')
  expect(await download.failure()).toBeNull()
  expect(readFileSync(await download.path())).toEqual(gdbZip)
  expect(requests).toBe(1)
})
for (const status of [403, 409, 500]) test(`FileGDB rejection ${status} saves nothing and shows no download success`, async ({ page }) => {
  let downloads = 0, requests = 0
  page.on('download',()=>downloads++)
  await install(page,route=>{ requests++; return json(route,{error:'源数据稳定后重试'},status) },'zh-cn',gdbOptions)
  await page.getByRole('button', {name:/^(下载|Download)$/i}).click()
  await expect(page.locator('.el-message--error')).toContainText('源数据稳定后重试')
  await expect(page.getByText(/^(下载已开始|Download started)$/i)).toHaveCount(0)
  expect(downloads).toBe(0); expect(requests).toBe(1)
})
test('unsupported whole-container download capability does not offer a download button or read source', async ({ page }) => {
  let reads=0
  await install(page,route=>{reads++;return route.abort()},'zh-cn',{...gdbOptions,supported:false,
    attributes:{data_type:'model_3d',format:'osgb',layout:'whole'}})
  expect(reads).toBe(0)
})

function json(route, body, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
