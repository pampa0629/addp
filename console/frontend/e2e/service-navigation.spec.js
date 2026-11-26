import { expect, test } from '@playwright/test'

const hostURL = '/e2e/fixtures/leave-guard.html?editor=service-navigation'
const standaloneURL = '/e2e/service-fixture/e2e/navigation.html'
const entries = [
  { label: '查询服务', path: '/query-services', heading: '查询服务' },
  { label: '瓦片服务', path: '/tile', heading: '瓦片服务' },
  { label: '图查询服务', path: '/graph-services', heading: '图查询服务' },
  { label: '服务注册', path: '/services', heading: '注册服务' },
  { label: '服务目录', path: '/catalog', heading: '服务目录' }
]
const registered = {
  id: 49, service_name: 'fixture_registered', title: 'Fixture registered service',
  service_type: 'rest', endpoint_url: 'https://example.invalid/fixture',
  auth_type: 'none', status: 'active', created_at: '2026-09-01T00:00:00Z'
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  page.on('pageerror', error => { throw error })
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET') {
      if (path === '/api/v1/service/registered/49') return route.fulfill({ json: registered })
      if (['query', 'tile', 'graph', 'registered'].some(kind => path === `/api/v1/service/${kind}`)) {
        const data = path.endsWith('/registered') ? [registered] : []
        return route.fulfill({ json: { data, total: data.length, page: 1, limit: 20, offset: 0 } })
      }
    }
    await route.abort()
    throw new Error(`Unexpected API request: ${request.method()} ${path}`)
  })
})

async function expectEmbedded(page, entry, path = entry.path, heading = entry.heading) {
  await expect(page).toHaveURL(`${hostURL}#/service${path}`)
  const frame = page.frameLocator('iframe')
  await expect(frame.getByRole('heading', { name: heading, exact: true })).toBeVisible()
  await expect(page.getByRole('menubar')).toHaveCount(1)
  await expect(page.getByRole('menuitem', { name: entry.label, exact: true })).toHaveClass(/is-active/)
  // App and Layout both run here: an accidental second sidebar must fail this check.
  await expect(frame.getByRole('menubar')).toHaveCount(0)
  await expect(frame.locator('.sidebar, .layout > .header')).toHaveCount(0)
  await expect(page.getByRole('menuitem').filter({ hasNot: page.getByRole('menu') })).toHaveText(entries.map(item => item.label))
  return frame
}

for (const entry of entries) {
  test(`Console ${entry.label} menu keeps one navigation through reload, back and forward`, async ({ page }) => {
    const start = entries[entry.path === '/catalog' ? 0 : 4]
    await page.goto(`${hostURL}#/service${start.path}`)
    await expectEmbedded(page, start)
    await page.getByRole('menuitem', { name: entry.label, exact: true }).click()
    await expectEmbedded(page, entry)
    await page.reload()
    await expectEmbedded(page, entry)
    await page.goBack()
    await expectEmbedded(page, start)
    await page.goForward()
    await expectEmbedded(page, entry)
  })
}

for (const [button, entry] of [['查询服务管理', entries[0]], ['注册服务管理', entries[3]]]) {
  test(`catalog ${button} uses the canonical route without reloading the iframe or duplicating history`, async ({ page }) => {
    await page.goto(`${hostURL}#/service/catalog`)
    const frame = await expectEmbedded(page, entries[4])
    // A document marker survives router navigation but not a hidden iframe reload.
    await frame.locator('body').evaluate(element => element.dataset.navigationDocument = 'catalog')
    const historyLength = await page.evaluate(() => history.length)
    await frame.getByRole('button', { name: button, exact: true }).click()
    await expectEmbedded(page, entry)
    await expect(frame.locator('body')).toHaveAttribute('data-navigation-document', 'catalog')
    expect(await page.evaluate(() => history.length)).toBe(historyLength + 1)
    await page.goBack()
    await expectEmbedded(page, entries[4])
    await page.goForward()
    await expectEmbedded(page, entry)
  })
}

test('registered catalog card opens the real detail under the registry menu and returns to its canonical list', async ({ page }) => {
  await page.goto(`${hostURL}#/service/catalog`)
  const frame = await expectEmbedded(page, entries[4])
  await frame.getByRole('heading', { name: registered.title, exact: true }).click()
  await expectEmbedded(page, entries[3], '/services/49', registered.title)
  await page.reload()
  await expectEmbedded(page, entries[3], '/services/49', registered.title)
  await frame.getByRole('button', { name: '← 返回', exact: true }).click()
  await expectEmbedded(page, entries[3])
})

test('standalone Service renders exactly the five Console entries and supports both catalog management buttons', async ({ page }) => {
  await page.goto(`${standaloneURL}#/catalog`)
  for (const entry of entries) {
    await expect(page.getByRole('menubar')).toHaveCount(1)
    await expect(page.getByRole('menuitem')).toHaveText(entries.map(item => item.label))
    await page.getByRole('menuitem', { name: entry.label, exact: true }).click()
    await expect(page).toHaveURL(`${standaloneURL}#${entry.path}`)
    await expect(page.getByRole('heading', { name: entry.heading, exact: true })).toBeVisible()
    await expect(page.getByRole('menuitem', { name: entry.label, exact: true })).toHaveClass(/is-active/)
  }
  for (const [button, entry] of [['查询服务管理', entries[0]], ['注册服务管理', entries[3]]]) {
    await page.getByRole('button', { name: button, exact: true }).click()
    await expect(page).toHaveURL(`${standaloneURL}#${entry.path}`)
    await expect(page.getByRole('heading', { name: entry.heading, exact: true })).toBeVisible()
    await page.goBack()
    await expect(page).toHaveURL(`${standaloneURL}#/catalog`)
    await expect(page.getByRole('heading', { name: '服务目录', exact: true })).toBeVisible()
  }
  await page.reload()
  await expect(page.getByRole('menubar')).toHaveCount(1)
  await expect(page.getByRole('menuitem', { name: '服务目录', exact: true })).toHaveClass(/is-active/)
})

const secondRegistered = { ...registered, id: 50, service_name: 'second_registered', title: 'Second registered service' }

async function switchStandaloneDetail(page, id) {
  await page.evaluate(id => { window.location.hash = `/services/${id}` }, id)
  await expect(page).toHaveURL(`${standaloneURL}#/services/${id}`)
}

test('reused registered detail switches identity and restores it through back and forward', async ({ page }) => {
  await page.route('**/api/v1/service/registered/50', route => route.fulfill({ json: secondRegistered }))
  await page.goto(`${standaloneURL}#/services/49`)
  const detail = page.locator('.registered-service-detail')
  await expect(detail.getByRole('heading', { name: registered.title, exact: true })).toBeVisible()
  await detail.evaluate(element => element.dataset.reusedDetail = 'original')
  await switchStandaloneDetail(page, 50)
  await expect(detail.getByRole('heading', { name: secondRegistered.title, exact: true })).toBeVisible()
  await expect(detail).toHaveAttribute('data-reused-detail', 'original')
  await page.goBack()
  await expect(page).toHaveURL(`${standaloneURL}#/services/49`)
  await expect(detail.getByRole('heading', { name: registered.title, exact: true })).toBeVisible()
  await page.goForward()
  await expect(page).toHaveURL(`${standaloneURL}#/services/50`)
  await expect(detail.getByRole('heading', { name: secondRegistered.title, exact: true })).toBeVisible()
  await expect(detail).toHaveAttribute('data-reused-detail', 'original')
})

for (const outcome of ['success', 'failure']) {
  test(`registered detail discards a previous identity's late ${outcome} while the next identity loads`, async ({ page }) => {
    let oldRequest, nextRequest
    await page.route('**/api/v1/service/registered/49', route => { oldRequest = route })
    await page.route('**/api/v1/service/registered/50', route => { nextRequest = route })
    await page.goto(`${standaloneURL}#/services/49`)
    await expect.poll(() => Boolean(oldRequest)).toBe(true)
    await switchStandaloneDetail(page, 50)
    await expect.poll(() => Boolean(nextRequest)).toBe(true)
    const oldFinished = page.waitForEvent('requestfinished', request => new URL(request.url()).pathname.endsWith('/registered/49'))
    await oldRequest.fulfill(outcome === 'success'
      ? { json: registered }
      : { status: 500, json: { error: 'Old identity failed' } })
    await oldFinished
    const detail = page.locator('.registered-service-detail')
    await expect(detail.locator('.loading')).toBeVisible()
    for (const label of ['刷新元数据', '健康检查', '编辑', '删除']) {
      await expect(detail.getByRole('button', { name: label, exact: true })).toBeDisabled()
    }
    await nextRequest.fulfill({ json: secondRegistered })
    await expect(detail.getByRole('heading', { name: secondRegistered.title, exact: true })).toBeVisible()
    await expect(page).toHaveURL(`${standaloneURL}#/services/50`)
    await expect(page.locator('.el-message--error')).toHaveCount(0)
    await expect(detail.getByRole('button', { name: '删除', exact: true })).toBeEnabled()
  })
}

test('catalog follows the graph list pagination and distinguishes a failed source from an empty one', async ({ page }) => {
  const pages = []
  const graphs = Array.from({ length: 101 }, (_, index) => ({
    id: index + 1, title: 'Graph ' + (index + 1), config_type: 'shape'
  }))
  await page.route('**/api/v1/service/graph?*', route => {
    const params = new URL(route.request().url()).searchParams
    const number = Number(params.get('page'))
    const limit = Number(params.get('page_size'))
    expect(limit).toBeLessThanOrEqual(100)
    pages.push(number)
    return route.fulfill({ json: { data: graphs.slice((number - 1) * limit, number * limit), total: graphs.length } })
  })
  let fail = true
  await page.route('**/api/v1/service/registered?*', route => fail
    ? route.fulfill({ status: 503, json: { error: 'fixture unavailable' } })
    : route.fulfill({ json: { data: [registered], total: 1 } }))
  await page.goto(`${standaloneURL}#/catalog?tab=graph`)
  await expect(page.getByRole('heading', { name: 'Graph 101', exact: true })).toBeVisible()
  expect(pages).toEqual([1, 2])
  await expect(page.getByRole('alert')).toContainText('注册服务')
  await page.getByRole('tab', { name: 'REST API' }).click()
  await expect(page.getByText('暂无 REST API 服务')).toHaveCount(0)
  fail = false
  await page.getByRole('button', { name: '刷新' }).click()
  await expect(page.getByRole('heading', { name: registered.title })).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('tile edit loads the current service and only updates its editable metadata', async ({ page }) => {
  const tile = {
    id: 64, title: 'Original tiles', service_name: 'example_tiles', description: 'Example',
    keywords: ['one', 'two'], public_access: true, layers: [], protocols: {}, status: 'error'
  }
  const writes = []
  await page.route('**/api/v1/service/tile/64', async route => {
    if (route.request().method() === 'PUT') {
      const fields = route.request().postDataJSON()
      writes.push(fields)
      if (writes.length === 1) return route.fulfill({ status: 503, json: { error: 'fixture unavailable' } })
      Object.assign(tile, fields)
    }
    return route.fulfill({ json: tile })
  })
  await page.goto(`${standaloneURL}#/tile/64`)
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  const title = page.getByPlaceholder('输入服务显示标题')
  await expect(title).toHaveValue('Original tiles')
  await expect(page.getByPlaceholder('输入服务名称（英文、数字、下划线）')).toBeDisabled()
  await title.fill('Updated tiles')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.locator('.el-message--error')).toContainText('fixture unavailable')
  await expect(title).toHaveValue('Updated tiles')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect(page).toHaveURL(`${standaloneURL}#/tile/64`)
  await expect(page.getByRole('heading', { name: 'Updated tiles' })).toBeVisible()
  expect(writes).toEqual(Array(2).fill({ title: 'Updated tiles', description: 'Example', keywords: ['one', 'two'], public_access: true }))
})

test('tile edit persists publication switches independently and retains protocol settings', async ({ page }) => {
  const protocols = {
    xyz: { enabled: true, formats: ['mvt'] },
    ogc_tiles: { enabled: false, version: '1.0' },
    tms: { enabled: false }
  }
  const tile = {
    id: 64, title: 'Roads', service_name: 'roads', keywords: [], public_access: true,
    layers: [], protocols, status: 'active'
  }
  const writes = []
  let refuseFirstSave = true
  await page.route('**/api/v1/service/tile/64', route => {
    if (route.request().method() === 'PUT') {
      const fields = route.request().postDataJSON()
      writes.push(fields)
      if (refuseFirstSave) {
        refuseFirstSave = false
        return route.fulfill({ status: 503, json: { error: 'fixture unavailable' } })
      }
      Object.assign(tile, fields, { protocols: { ...tile.protocols, ...fields.protocols } })
    }
    return route.fulfill({ json: tile })
  })
  const edit = page.getByRole('button', { name: '编辑', exact: true })
  const save = page.getByRole('button', { name: '保存', exact: true })
  const active = page.getByRole('switch', { name: '启用瓦片服务' })
  const xyz = page.getByRole('checkbox', { name: 'XYZ 瓦片协议' })
  const ogc = page.getByRole('checkbox', { name: 'OGC Tiles API' })
  await page.goto(`${standaloneURL}#/tile/64`)
  await edit.click()
  await expect(active).toBeChecked()
  await expect(xyz).toBeChecked()
  await expect(ogc).not.toBeChecked()
  await page.locator('.el-switch').filter({ has: active }).click()
  await page.getByText('OGC Tiles API', { exact: true }).click()
  await save.click()
  await expect(page.locator('.el-message--error')).toContainText('fixture unavailable')
  await expect(active).not.toBeChecked()
  await expect(ogc).toBeChecked()
  await save.click()
  await edit.click()
  await expect(active).not.toBeChecked()
  await expect(ogc).toBeChecked()
  await page.locator('.el-switch').filter({ has: active }).click()
  await page.getByText('XYZ 瓦片协议', { exact: true }).click()
  await save.click()
  await edit.click()
  await expect(active).toBeChecked()
  await expect(xyz).not.toBeChecked()
  await page.getByText('XYZ 瓦片协议', { exact: true }).click()
  await page.getByText('OGC Tiles API', { exact: true }).click()
  await save.click()
  await expect(edit).toBeVisible()
  expect(writes[0]).toEqual(writes[1])
  expect(writes.map(w => [w.status, w.protocols])).toEqual([
    ['inactive', { ogc_tiles: { enabled: true, version: '1.0' } }],
    ['inactive', { ogc_tiles: { enabled: true, version: '1.0' } }],
    ['active', { xyz: { enabled: false, formats: ['mvt'] } }],
    [undefined, { xyz: { enabled: true, formats: ['mvt'] }, ogc_tiles: { enabled: false, version: '1.0' } }]
  ])
})

test('tile preview requests OGC tiles when XYZ is off and skips disabled layers', async ({ page }) => {
  const tile = {
    id: 64, service_name: 'fixture', title: 'Fixture tiles', status: 'active',
    protocols: { xyz: { enabled: false }, ogc_tiles: { enabled: true } },
    layers: [
      { id: 1, layer_name: 'hidden', title: 'Hidden layer', enabled: false },
      { id: 2, layer_name: 'roads', title: 'Roads', enabled: true }
    ]
  }
  const tileRequests = []
  await page.route('**/api/v1/service/tile/64', route => route.fulfill({ json: tile }))
  await page.route('https://tile.openstreetmap.org/**', route => route.fulfill({ status: 204 }))
  await page.route(url => /^\/(ogc\/tiles|tiles)\/fixture\//.test(url.pathname), route => {
    tileRequests.push(new URL(route.request().url()).pathname)
    return route.fulfill({ status: 200, contentType: 'application/vnd.mapbox-vector-tile', body: '' })
  })
  await page.goto(`${standaloneURL}#/tile/64`)
  await expect(page.locator('pre.code-example').first()).toContainText('/ogc/tiles/fixture/tiles/roads/WebMercatorQuad/{z}/{y}/{x}')
  await page.locator('.preview-controls').scrollIntoViewIfNeeded()
  await expect.poll(() => tileRequests.length).toBeGreaterThan(0)
  expect(tileRequests.every(path => path.startsWith('/ogc/tiles/fixture/tiles/roads/WebMercatorQuad/'))).toBe(true)
  tile.status = 'inactive'
  await page.reload()
  await expect(page.getByText('当前服务没有可预览的已发布图层')).toBeVisible()
  await expect(page.locator('pre.code-example')).toHaveCount(0)
})

test('tile detail adds, edits and deletes a layer under its original parent', async ({ page }) => {
  const tile = { id: 64, service_name: 'fixture', title: 'Fixture tiles', layers: [], protocols: {}, status: 'active' }
  const root = 'addp://engine/9/path/?type=root&node_id=2'
  const locator = 'addp://engine/9/path/roads.pmtiles?type=file&item_id=71'
  const file = { id: locator, locator, label: 'roads.pmtiles', type: 'file',
    metadata: { item_id: 71, data_type: 'media', format: 'pmtiles', layout: 'single' } }
  const source = { locator }
  await page.route('**/api/v1/service/tile/64', route => route.fulfill({ json: tile }))
  await page.route('**/api/v1/meta/engines', route => route.fulfill({ json: [
    { id: 9, name: 'Business files', engine_type: 'nfs', lifecycle_state: 'active', connection_status: 'online',
      capabilities: { engine_family: 'file', storage: {} } }
  ] }))
  await page.route('**/api/v1/meta/resource-tree/9**', route => route.fulfill({ json: {
    id: root, locator: root, label: 'Business files', type: 'root', hasChildren: true, children: [file]
  } }))
  await page.route('**/api/v1/meta/items/71', route => route.fulfill({ json: {
    attributes: { format_info: { pmtiles: { tile_type: 'mvt', tile_compression: 'gzip', min_zoom: 0, max_zoom: 12 } } }
  } }))
  const methods = []
  await page.route('**/api/v1/service/tile-layers/64**', async route => {
    const method = route.request().method()
    const path = new URL(route.request().url()).pathname
    methods.push(method)
    if (method === 'POST') {
      expect(path).toBe('/api/v1/service/tile-layers/64')
      const layer = route.request().postDataJSON()
      expect(layer).toMatchObject({ layer_type: 'static', layer_config: { source } })
      tile.layers = [{ ...layer, id: 18, enabled: true }]
    } else {
      expect(path).toBe('/api/v1/service/tile-layers/64/18')
      if (method === 'PUT') {
        const update = route.request().postDataJSON()
        expect(update).not.toHaveProperty('layer_type')
        expect(update).not.toHaveProperty('layer_name')
        Object.assign(tile.layers[0], update)
      } else if (method === 'DELETE') tile.layers = []
      else throw new Error(method)
    }
    return route.fulfill({ json: {} })
  })
  await page.goto(`${standaloneURL}#/tile/64`)
  await page.getByRole('button', { name: '添加图层' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByText('静态图层', { exact: true }).click()
  await dialog.getByText(file.label, { exact: true }).click()
  await dialog.getByPlaceholder('输入图层显示标题').fill('Road layer')
  await dialog.getByRole('button', { name: '保存' }).click()
  const row = page.getByRole('row').filter({ hasText: 'Road layer' })
  await row.getByRole('button', { name: '编辑' }).click()
  await dialog.getByPlaceholder('输入图层显示标题').fill('Changed road layer')
  await dialog.getByRole('button', { name: '保存' }).click()
  const changed = page.getByRole('row').filter({ hasText: 'Changed road layer' })
  await changed.getByRole('button', { name: '删除' }).click()
  await page.locator('.el-message-box').getByRole('button', { name: '确定' }).click()
  await expect(changed).toHaveCount(0)
  expect(methods).toEqual(['POST', 'PUT', 'DELETE'])
})
