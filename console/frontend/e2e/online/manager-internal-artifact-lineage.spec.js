import { expect, request, test } from '@playwright/test'
import { createBrowserGpuDiagnostics } from '../../../../common-frontend/basic/src/utils/browserGpuDiagnostics.mjs'
import { readFileSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { resolve } from 'node:path'
import { isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

const requiredNames = [
  'ADDP_ONLINE_ARTIFACT_DIR',
  'ADDP_ONLINE_TEST_RUN_ID',
  'ADDP_ONLINE_TEST_TENANT_ID',
  'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
  'ADDP_ONLINE_TEST_USER_USERNAME',
  'ADDP_ONLINE_TEST_USER_PASSWORD',
  'ADDP_ONLINE_MANAGER_LINEAGE_EXECUTION_ID',
  'ADDP_ONLINE_MANAGER_LINEAGE_ITEM_ID',
  'ADDP_ONLINE_MANAGER_LINEAGE_SOURCE_NAME',
  'ADDP_ONLINE_MANAGER_LINEAGE_OUTPUT_NAME',
  'ADDP_ONLINE_MANAGER_PPTX_ITEM_LOCATOR',
  'ADDP_ONLINE_MANAGER_PPTX_ITEM_ID',
  'ADDP_ONLINE_MANAGER_PPTX_PAGE_COUNT',
  'ADDP_ONLINE_MANAGER_MODELS_JSON',
  'ADDP_ONLINE_MANAGER_BROWSER_PHASE',
  'ADDP_ONLINE_MANAGER_RASTER_JSON',
  'ADDP_ONLINE_MANAGER_DELIMITED_JSON',
  'ADDP_ONLINE_MANAGER_WORKBOOK_JSON',
  'ADDP_ONLINE_MANAGER_STRUCTURED_JSON',
  'GATEWAY_URL'
]

function environment() {
  const missing = requiredNames.filter(name => !process.env[name])
  if (missing.length > 0) throw new Error(`missing Online environment: ${missing.join(', ')}`)
  return Object.fromEntries(requiredNames.map(name => [name, process.env[name]]))
}

async function verifyOriginalDownload(page, frame, source) {
  const downloaded = page.waitForEvent('download')
  await frame.getByRole('button', { name: /下载|Download/, exact: true }).click()
  const download = await downloaded
  const path = await download.path()
  expect(path).not.toBeNull()
  const bytes = readFileSync(path)
  expect(bytes.length).toBe(source.size_bytes)
  const checksum = createHash('sha256').update(bytes).digest('hex')
  expect(checksum).toBe(source.sha256)
  return { bytes, checksum }
}

async function verifySourceModel(page, frame, model) {
  const preview = frame.locator('.model-preview')
  await expect(preview).toBeVisible({ timeout: 60_000 })
  await expect(preview.locator('canvas')).toBeVisible()
  await expect(preview.locator('.three-status')).toHaveCount(0, { timeout: 60_000 })
  const downloaded = await verifyOriginalDownload(page, frame, model)
  return { source_model_loaded: true, download_sha256: downloaded.checksum }
}

async function verifyDelimitedTable(page, api, source, artifactDir) {
  const route = `/manager/data-explorer?locator=${encodeURIComponent(source.locator)}`
  await page.goto(route)
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const selector = frame.locator('.csv-encoding-select')
  await expect(selector).toContainText('UTF-8')
  await selector.click()
  await frame.getByRole('option', { name: 'GBK', exact: true }).click()
  const refresh = () => page.waitForResponse(response => {
    const url = new URL(response.url())
    return response.request().method() === 'POST'
      && /\/api\/v1\/manager\/engines\/[1-9][0-9]*\/items\/refresh$/.test(url.pathname)
      && url.searchParams.get('locator') === source.locator
  })
  const applied = refresh()
  await frame.getByRole('button', { name: /应用编码并刷新|Apply encoding and refresh/ }).click()
  const applyResponse = await applied
  expect(applyResponse.status()).toBe(200)
  expect(applyResponse.request().postDataJSON()).toEqual({ encoding: 'gbk' })
  await expect(frame.getByRole('columnheader', { name: '姓名', exact: true })).toBeVisible()
  await expect(selector).toContainText('GBK')

  const verifyPage = async (offset, count) => {
    await expect(frame.getByRole('cell', { name: `张三${offset}`, exact: true })).toBeVisible()
    await expect(frame.getByRole('columnheader')).toHaveText(['id', '姓名', '备注'])
    await expect(frame.getByRole('cell')).toHaveCount(count * 3)
    const expected = Array.from({ length: count }, (_, index) => {
      const id = offset + index
      return [String(id), `张三${id}`, `北京,第${id}项\n第二行`]
    }).flat()
    expect((await frame.getByRole('cell').allTextContents()).map(text => text.trim())).toEqual(expected)
  }
  const verifyMetadata = async () => {
    const item = await json(await api.get(`/api/v1/meta/items/${source.item_id}`), 'read delimited item facts')
    expect(item.id).toBe(source.item_id)
    expect(item.fingerprint).toBe(source.fingerprint)
    expect(item.row_count).toBe(25)
    expect(item.attributes.item).toMatchObject({ format: source.format, data_type: 'table', layout: 'single' })
    expect(item.attributes.format_info[source.format].encoding).toBe('gbk')
    expect(item.attributes.type_info.table.fields.map(field => field.name)).toEqual(['id', '姓名', '备注'])
    expect(item.attributes.storage.content_hash).toBe(source.sha256)
    expect(item.attributes.access_index?.table).toBeUndefined()
  }
  await verifyPage(0, 20)
  const next = frame.getByRole('button', { name: /下一页|Go to next page/, exact: true })
  await expect(next).toBeEnabled()
  await next.click()
  await verifyPage(20, 5)
  await expect(next).toBeDisabled()
  await verifyMetadata()
  await frame.locator('.preview-panel').screenshot({ path: resolve(artifactDir, `gbk-${source.format}-preview.png`) })
  const { checksum } = await verifyOriginalDownload(page, frame, source)

  const ordinary = refresh()
  await frame.getByRole('treeitem', { name: `gbk-quoted.${source.format}`, exact: true }).locator('.node-action').click()
  const ordinaryResponse = await ordinary
  expect(ordinaryResponse.status()).toBe(200)
  expect(ordinaryResponse.request().postData()).toBeNull()
  await verifyMetadata()
  await page.reload()
  await expect(selector).toContainText('GBK')
  await verifyPage(0, 20)
  return { ...source, encoding: 'gbk', first_page_rows: 20, last_page_rows: 5,
    last_page_disabled: true, metadata_verified: true, ordinary_refresh: true,
    reopened: true, download_sha256: checksum, encoding_request_verified: true }
}

async function verifyWorkbook(page, api, source, artifactDir) {
  const previews = []
  const pending = []
  const readErrors = []
  const receive = response => {
    const url = new URL(response.url())
    if (url.pathname !== '/api/v1/manager/preview' || url.searchParams.get('locator') !== source.locator) return
    pending.push(response.json().then(body => previews.push({
      child: url.searchParams.get('child_name') || '', page: Number(url.searchParams.get('page') || 1),
      status: response.status(), data: body.data
    })).catch(error => readErrors.push(error.message)))
  }
  page.on('response', receive)
  try {
    await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(source.locator)}`)
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const container = frame.locator('.container-preview')
    const content = container.locator('.child-preview')
    const next = container.getByRole('button', { name: /下一页|Go to next page/, exact: true })
    const sheets = [
      { name: 'Cities', columns: ['city_id', 'city_name'], verified_rows: 25 },
      { name: 'Readings', columns: ['sensor', 'temperature', 'status'], verified_rows: 1 },
      { name: 'Empty', columns: [], verified_rows: 0 },
      { name: 'HeaderOnly', columns: ['code', 'label'], verified_rows: 0 }
    ]
    const verifyRows = async (columns, rows) => {
      await expect(content.getByRole('columnheader')).toHaveText(columns)
      await expect(content.getByRole('cell')).toHaveCount(rows.length * columns.length)
      expect((await content.getByRole('cell').allTextContents()).map(value => value.trim())).toEqual(rows.flat())
    }
    const cities = (first, count) => Array.from({ length: count }, (_, index) => [String(first + index), `杭州${first + index}`])
    const select = async name => {
      await container.locator('.child-select').click()
      await frame.getByRole('option', { name: new RegExp(`^${name}(?: \\(|$)`) }).click()
      await expect(container.locator('.child-select')).toContainText(name)
    }
    const verifyPreview = async (child, pageNumber, hasMore) => {
      await expect.poll(() => {
        const preview = previews.filter(value => value.child === child && value.page === pageNumber).at(-1)
        return preview && { status: preview.status, total: preview.data?.total, has_more: preview.data?.has_more }
      }).toEqual({ status: 200, total: null, has_more: hasMore })
    }
    const verifyMetadata = async () => {
      const item = await json(await api.get(`/api/v1/meta/items/${source.item_id}`), 'read workbook facts')
      expect(item.id).toBe(source.item_id)
      expect(item.fingerprint).toBe(source.fingerprint)
      expect(item.size_bytes).toBe(source.size_bytes)
      expect(item.attributes.item).toMatchObject({ format: 'excel', data_type: 'container', layout: 'single' })
      expect(item.attributes.storage.content_hash).toBe(source.sha256)
      expect(item.attributes.format_info.excel).toMatchObject({ sheet_count: 4, default_sheet: 'Cities' })
      const facts = item.attributes.type_info.container
      expect(facts.child_count).toBe(4)
      expect(facts.default_child).toBe('Cities')
      expect(facts.children.map(child => child.name)).toEqual(source.sheet_names)
      expect(facts.children.map(child => child.column_count)).toEqual([2, 3, 0, 2])
      for (const child of facts.children) {
        expect(child).toMatchObject({ data_type: 'table', child_kind: 'sheet' })
        expect(child.row_count).toBeUndefined()
      }
    }
    const refreshSelection = async (child, pageNumber) => {
      const before = previews.length
      const refreshed = page.waitForResponse(response => {
        const url = new URL(response.url())
        return response.request().method() === 'POST'
          && /\/api\/v1\/manager\/engines\/[1-9][0-9]*\/items\/refresh$/.test(url.pathname)
          && url.searchParams.get('locator') === source.locator
      })
      await frame.getByRole('treeitem', { name: 'format-validation.xlsx', exact: true }).locator('.node-action').click()
      const response = await refreshed
      expect(response.status()).toBe(200)
      expect(response.request().postData()).toBeNull()
      for (const name of ['', child]) {
        await expect.poll(() => previews.slice(before).some(value => value.child === name && value.page === pageNumber && value.status === 200)).toBe(true)
      }
      await expect(container.locator('.child-select')).toContainText(child)
      await verifyMetadata()
    }

    await verifyRows(sheets[0].columns, cities(1, 20))
    await verifyPreview('Cities', 1, true)
    await expect(container.locator('.el-pagination__total')).toHaveCount(0)
    await expect(container.locator('.el-pager')).toHaveCount(0)
    await expect(next).toBeEnabled()
    await next.click()
    await verifyRows(sheets[0].columns, cities(21, 5))
    await verifyPreview('Cities', 2, false)
    await expect(next).toBeDisabled()
    await refreshSelection('Cities', 2)
    await verifyRows(sheets[0].columns, cities(21, 5))
    await expect(next).toBeDisabled()
    await frame.locator('.preview-panel').screenshot({ path: resolve(artifactDir, 'xlsx-cities-preview.png') })

    await select('Readings')
    await verifyRows(sheets[1].columns, [['Sensor-A', '18.5', '正常']])
    await verifyPreview('Readings', 1, false)
    await expect(next).toBeDisabled()
    await frame.locator('.preview-panel').screenshot({ path: resolve(artifactDir, 'xlsx-readings-preview.png') })

    await select('Empty')
    await verifyRows([], [])
    await expect(content.locator('.el-table__empty-block')).toBeVisible()
    await expect(container.locator('.data-pagination')).toHaveCount(0)
    await refreshSelection('Empty', 1)
    await verifyRows([], [])
    await expect(content.locator('.el-table__empty-block')).toBeVisible()
    await expect(container.locator('.data-pagination')).toHaveCount(0)
    await frame.locator('.preview-panel').screenshot({ path: resolve(artifactDir, 'xlsx-empty-preview.png') })

    await select('HeaderOnly')
    await verifyRows(sheets[3].columns, [])
    await expect(content.locator('.el-table__empty-block')).toBeVisible()
    await expect(container.locator('.data-pagination')).toHaveCount(0)
    await frame.locator('.preview-panel').screenshot({ path: resolve(artifactDir, 'xlsx-headeronly-preview.png') })
    await select('Cities')
    await verifyRows(sheets[0].columns, cities(1, 20))
    await expect(next).toBeEnabled()

    const { bytes, checksum } = await verifyOriginalDownload(page, frame, source)
    expect(bytes.subarray(0, 4)).toEqual(Buffer.from([0x50, 0x4b, 0x03, 0x04]))
    await page.reload()
    await expect(container.locator('.child-select')).toContainText('Cities')
    await verifyRows(sheets[0].columns, cities(1, 20))
    await verifyMetadata()
    await Promise.all(pending)
    expect(readErrors).toEqual([])
    return { ...source, sheets, first_page_rows: 20, last_page_rows: 5, last_page_disabled: true,
      unknown_total_verified: true, sheet_switch_reset: true, refresh_preserved_page: true,
      refresh_preserved_empty_sheet: true, metadata_verified: true, reopened: true, download_sha256: checksum }
  } finally {
    page.off('response', receive)
  }
}

async function verifyStructuredJSON(page, api, source, artifactDir) {
  const document = { title: '格式验证', settings: { enabled: true, threshold: null }, values: [1, '中文', false] }
  const features = { type: 'FeatureCollection', features: [
    { type: 'Feature', properties: { id: 1, label: '采样点' }, geometry: { type: 'Point', coordinates: [120, 30] } },
    { type: 'Feature', properties: { id: 2, label: '采样线' }, geometry: { type: 'LineString', coordinates: [[120, 30], [120.01, 30.02], [120.02, 30]] } }
  ] }
  const received = []
  const pending = []
  const readErrors = []
  const receive = response => {
    const url = new URL(response.url())
    if (url.pathname !== '/api/v1/manager/preview' || url.searchParams.get('locator') !== source.locator) return
    pending.push(response.json().then(body => received.push({ status: response.status(), page: Number(url.searchParams.get('page') || 1), ...body })).catch(error => readErrors.push(error.message)))
  }
  page.on('response', receive)
  try {
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const verifyMetadata = async () => {
      const item = await json(await api.get(`/api/v1/meta/items/${source.item_id}`), 'read structured JSON facts')
      expect(item.id).toBe(source.item_id)
      expect(item.fingerprint).toBe(source.fingerprint)
      expect(item.size_bytes).toBe(source.size_bytes)
      expect(item.attributes.item).toMatchObject({ format: source.format, data_type: source.data_type, layout: 'single' })
      expect(item.attributes.storage.content_hash).toBe(source.sha256)
      if (source.case === 'records') {
        expect(item.row_count).toBe(25)
        expect(item.attributes.type_info.table.fields.map(field => field.name)).toEqual(['id', 'label'])
        expect(item.attributes.capabilities?.spatial).toBeUndefined()
      } else if (source.format === 'geojson') {
        expect(item.row_count).toBe(2)
        expect(item.attributes.format_info.geojson.structure).toBe('geojson_feature_collection')
        expect(item.attributes.format_info.json).toBeUndefined()
        const geometry = item.attributes.capabilities.spatial.geometry_columns
        expect(geometry).toHaveLength(1)
        expect(geometry[0]).toMatchObject({ name: 'geometry', srid: 4326, crs_ref: 'EPSG:4326' })
      } else {
        expect(item.attributes.capabilities?.spatial).toBeUndefined()
      }
    }
    const verifyContent = async (offset = 0, count = 20) => {
      await expect.poll(() => received.length).toBeGreaterThan(0)
      if (source.case === 'records') {
        const table = frame.locator('.table-preview')
        await expect(table.getByRole('columnheader')).toHaveText(['id', 'label'])
        await expect(table.getByRole('cell', { name: `记录${offset}`, exact: true })).toBeVisible()
        await expect(table.getByRole('cell')).toHaveCount(count * 2)
        expect((await table.getByRole('cell').allTextContents()).map(value => value.trim())).toEqual(
          Array.from({ length: count }, (_, i) => [String(offset + i), `记录${offset + i}`]).flat())
        await expect(frame.locator('.geojson-preview')).toHaveCount(0)
        const expectedRows = Array.from({ length: count }, (_, i) => ({ id: offset + i, label: `记录${offset + i}` }))
        await expect.poll(() => {
          const preview = received.filter(value => value.page === (offset === 0 ? 1 : 2)).at(-1)
          return preview && { type: preview.preview_type, total: preview.data?.total, rows: preview.data?.rows }
        }).toEqual({ type: 'table', total: 25, rows: expectedRows })
      } else if (source.format === 'geojson') {
        const expectedRows = features.features.map(feature => ({ ...feature.properties, geometry: feature.geometry }))
        await expect.poll(() => {
          const preview = received.at(-1)
          const data = preview?.data
          return preview && { status: preview.status, type: preview.preview_type, total: data?.total,
            columns: [...(data?.columns || [])].sort(), rows: data?.rows,
            geometry_columns: data?.geometry_columns, geometry_column: data?.geometry_column,
            source_srid: data?.source_srid, source_crs: data?.source_crs,
            transform_status: data?.transform_status, preview_hint: data?.preview_hint }
        }).toEqual({ status: 200, type: 'table', total: 2, columns: ['geometry', 'id', 'label'], rows: expectedRows,
          geometry_columns: ['geometry'], geometry_column: 'geometry', source_srid: 4326, source_crs: 'EPSG:4326',
          transform_status: 'not_transformed', preview_hint: 'direct_renderable' })
        const table = frame.locator('.table-preview')
        await expect(table.getByRole('columnheader')).toHaveText(['id', 'label'])
        expect((await table.getByRole('cell').allTextContents()).map(value => value.trim())).toEqual(['1', '采样点', '2', '采样线'])
        await expect(table.getByRole('button', { name: /下一页|Go to next page/, exact: true })).toBeDisabled()
        await expect(table.locator('.map-container canvas').first()).toBeVisible()
        await expect(table.locator('.map-placeholder')).toHaveCount(0)
        const map = table.locator('.map-container')
        await expect.poll(async () => {
          const popup = map.locator('.map-popup').last()
          if (await popup.isVisible() && (await popup.innerText()).includes('采样点')) return true
          // Read OpenLayers' current projected pixel, then use real pointer input.
          // A visible canvas can precede feature insertion and the fit animation.
          const pixel = await map.locator('.openlayers-map-renderer').evaluate(element => {
            const value = element.__vueParentComponent?.setupState?.mapInstance
            const instance = value?.value || value
            if (!instance || instance.getView().getAnimating()) return null
            const point = instance.getLayers().getArray()
              .flatMap(layer => layer.getSource?.()?.getFeatures?.() || [])
              .find(feature => feature.getGeometry()?.getType() === 'Point')
            return point ? instance.getPixelFromCoordinate(point.getGeometry().getCoordinates()) : null
          })
          const bounds = await map.boundingBox()
          if (!pixel || !bounds || pixel[0] < 0 || pixel[1] < 0 || pixel[0] >= bounds.width || pixel[1] >= bounds.height) return false
          await map.click({ position: { x: pixel[0], y: pixel[1] } })
          await expect(popup).toBeVisible()
          return (await popup.innerText()).includes('采样点')
        }).toBe(true)
        await expect(frame.locator('.geojson-preview, .json-preview')).toHaveCount(0)
      } else {
        await expect.poll(() => received.at(-1)?.data?.object?.content?.frontend_renderer).toBe('json')
        const content = received.at(-1)?.data?.object?.content
        const preview = frame.locator('.json-preview')
        await expect(preview).toBeVisible()
        expect(JSON.parse(await preview.locator('.json-content').innerText())).toEqual(document)
        expect(content).toMatchObject({ kind: 'json', preview_material: 'json', json: document })
        await expect(frame.locator('.table-preview, .geojson-preview')).toHaveCount(0)
      }
    }
    await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(source.locator)}`)
    await verifyContent()
    if (source.case === 'records') {
      const next = frame.getByRole('button', { name: /下一页|Go to next page/, exact: true })
      await expect(next).toBeEnabled()
      await next.click()
      await verifyContent(20, 5)
      await expect(next).toBeDisabled()
    }
    await verifyMetadata()
    await frame.locator('.preview-panel').screenshot({ path: resolve(artifactDir, `json-${source.case}-preview.png`) })
    const { bytes, checksum } = await verifyOriginalDownload(page, frame, source)
    expect(JSON.parse(bytes.toString('utf8'))).toEqual(source.case === 'document' ? document : source.case === 'records'
      ? Array.from({ length: 25 }, (_, i) => ({ id: i, label: `记录${i}` })) : features)
    const beforeRefresh = received.length
    const refreshed = page.waitForResponse(response => {
      const url = new URL(response.url())
      return response.request().method() === 'POST'
        && /\/api\/v1\/manager\/engines\/[1-9][0-9]*\/items\/refresh$/.test(url.pathname)
        && url.searchParams.get('locator') === source.locator
    })
    await frame.getByRole('treeitem', { name: source.object.split('/').at(-1), exact: true }).locator('.node-action').click()
    const response = await refreshed
    expect(response.status()).toBe(200)
    expect(response.request().postData()).toBeNull()
    await expect.poll(() => received.length).toBeGreaterThan(beforeRefresh)
    await verifyContent(source.case === 'records' ? 20 : 0, source.case === 'records' ? 5 : 20)
    await verifyMetadata()
    await Promise.all(pending)
    expect(readErrors).toEqual([])
    received.length = 0
    await page.reload()
    await verifyContent()
    await verifyMetadata()
    await Promise.all(pending)
    expect(readErrors).toEqual([])
    return { ...source, content_verified: true, metadata_verified: true, ordinary_refresh: true,
      reopened: true, download_sha256: checksum,
      ...(source.case === 'records' ? { first_page_rows: 20, last_page_rows: 5, last_page_disabled: true } : {}),
      ...(source.format === 'geojson' ? { map_loaded: true, table_verified: true, geometry_verified: true, feature_count: 2, geometry_types: ['Point', 'LineString'], source_srid: 4326, source_crs: 'EPSG:4326' } : {}) }
  } finally {
    page.off('response', receive)
  }
}

test('Manager lineage, cached previews, GBK tables, XLSX sheets and JSON formats through Console', async ({ page }) => {
  const env = environment()
  const itemID = Number(env.ADDP_ONLINE_MANAGER_LINEAGE_ITEM_ID)
  if (!Number.isInteger(itemID) || itemID <= 0) throw new Error('Manager lineage item ID must be positive')
  const pptxItemID = Number(env.ADDP_ONLINE_MANAGER_PPTX_ITEM_ID)
  const pptxPageCount = Number(env.ADDP_ONLINE_MANAGER_PPTX_PAGE_COUNT)
  if (!Number.isInteger(pptxItemID) || pptxItemID <= 0) throw new Error('Manager PPTX item ID must be positive')
  if (pptxPageCount !== 3) throw new Error('Manager PPTX fixture must have exactly 3 pages')
  test.setTimeout(240_000)
  const raster = JSON.parse(env.ADDP_ONLINE_MANAGER_RASTER_JSON)
  expect(Number.isInteger(raster.item_id) && raster.item_id > 0).toBe(true)
  expect(raster.preview_url).toMatch(/^\/api\/v1\/manager\/raster_cog\/[1-9][0-9]*\/content$/)
  const delimited = JSON.parse(env.ADDP_ONLINE_MANAGER_DELIMITED_JSON)
  expect(delimited.map(source => source.format)).toEqual(['csv', 'tsv'])
  const workbook = JSON.parse(env.ADDP_ONLINE_MANAGER_WORKBOOK_JSON)
  expect(workbook.format).toBe('excel')
  expect(workbook.sheet_names).toEqual(['Cities', 'Readings', 'Empty', 'HeaderOnly'])
  const structuredJSON = JSON.parse(env.ADDP_ONLINE_MANAGER_STRUCTURED_JSON)
  expect(structuredJSON.map(source => source.case)).toEqual(['document', 'records', 'geojson', 'geojson-json'])
  const phase = env.ADDP_ONLINE_MANAGER_BROWSER_PHASE
  expect(['generation-entry', 'cached-preview']).toContain(phase)
  const models = JSON.parse(env.ADDP_ONLINE_MANAGER_MODELS_JSON)
  expect(models.map(model => model.format)).toEqual(['dae', '3ds', 'ifc', 'osgb', 'skp', 'max', 'fbx', 'obj'])
  for (const model of models) {
    expect(Number.isInteger(model.item_id) && model.item_id > 0).toBe(true)
    if (phase === 'generation-entry') continue
    expect(Number.isInteger(model.result_id) && model.result_id > 0).toBe(true)
    expect(model.preview_url).toBe(`/api/v1/manager/model_3d_glb/${model.result_id}/content`)
  }
  const executionPath = `/monitor/executions?execution_id=${encodeURIComponent(env.ADDP_ONLINE_MANAGER_LINEAGE_EXECUTION_ID)}`
  const api = await request.newContext({
    baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN}` }
  })
  const browserMessages = []
  const gpuDiagnostics = createBrowserGpuDiagnostics('navigation')
  const failedBusinessResponses = []
  let businessStarted = false
  let anonymousRefresh401 = 0
  let pptxGenerationRequests = 0
  let modelGenerationRequests = 0
  let rasterGenerationRequests = 0
  let managerEngineRequests = 0
  page.on('request', requestEvent => {
    const pathname = new URL(requestEvent.url()).pathname
    if (pathname === '/api/v1/system/auth/context' && requestEvent.headers().authorization) {
      businessStarted = true
    }
    if (requestEvent.method() === 'POST' && pathname === '/api/v1/manager/quick-view/actions') {
      const action = requestEvent.postDataJSON()?.action
      if (action === 'generate_pptx_pdf') pptxGenerationRequests += 1
      if (action === 'generate_raster_cog') rasterGenerationRequests += 1
      if (action === 'generate_model_3d_glb') modelGenerationRequests += 1
    }
    if (requestEvent.method() === 'GET' && pathname === '/api/v1/manager/engines') {
      managerEngineRequests += 1
    }
  })
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, businessStarted)) return
    if (gpuDiagnostics.record(message)) return
    if (['warning', 'error'].includes(message.type())) {
      browserMessages.push({ type: message.type(), text: message.text() })
    }
  })
  page.on('pageerror', error => browserMessages.push({ type: 'pageerror', text: error.message }))
  page.on('response', response => {
    const pathname = new URL(response.url()).pathname
    if (isAnonymousRefreshResponse(response, businessStarted)) {
      anonymousRefresh401 += 1
      return
    }
    if (pathname.startsWith('/api/v1/') && response.status() >= 400) {
      failedBusinessResponses.push({ pathname, status: response.status() })
    }
  })

  try {
    const apiIdentity = await json(await api.get('/api/v1/system/auth/context'), 'read API AuthContext')
    expect(apiIdentity?.principal?.type).toBe('user')
    expect(String(apiIdentity?.context?.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)

    const browserAccessToken = await login(
      page,
      env.ADDP_ONLINE_TEST_USER_USERNAME,
      env.ADDP_ONLINE_TEST_USER_PASSWORD,
      executionPath
    )
    businessStarted = true
    const browserAPI = await request.newContext({
      baseURL: env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${browserAccessToken}` }
    })
    const browserIdentity = await json(await browserAPI.get('/api/v1/system/auth/context'), 'read browser AuthContext')
    expect(String(browserIdentity?.principal?.id)).toBe(String(apiIdentity?.principal?.id))
    expect(String(browserIdentity?.context?.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    await browserAPI.dispose()

    if (phase === 'generation-entry') {
      const modelEvidence = []
      for (const model of models) {
        gpuDiagnostics.setStage(`generation-${model.format}:preview`)
        await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(model.locator)}`)
        const modelFrame = page.frameLocator('iframe[data-testid="module-iframe"]')
        const generate = modelFrame.getByRole('button', { name: /生成 GLB 快显|Generate GLB Quick View/ })
        await expect(generate).toBeVisible()
        await expect(generate).toBeEnabled()
        const sourceProof = model.format === 'obj'
          ? await verifySourceModel(page, modelFrame, model)
          : model.format === 'fbx' ? { download_sha256: (await verifyOriginalDownload(page, modelFrame, model)).checksum } : {}
        if (!['fbx', 'obj'].includes(model.format)) {
          await expect(modelFrame.locator('.model-preview')).toHaveCount(0)
        }
        if (model.format === 'max') {
          expect(model.conversion_options.source_unit).toBe('mm')
          const options = modelFrame.locator('.max-options')
          await expect(options).toBeVisible()
          const defaultUnit = options.getByText(/未选择时默认米|Defaults to metres/)
          await expect(defaultUnit).toBeVisible()
          await defaultUnit.click()
          await modelFrame.getByRole('option', { name: /毫米（mm）|Millimetres \(mm\)/ }).click()
          await expect(options.getByText(/毫米（mm）|Millimetres \(mm\)/)).toBeVisible()
          await options.getByRole('button', { name: /外部贴图（0）|External textures \(0\)/ }).click()
          const dialog = modelFrame.getByRole('dialog', { name: /MAX 外部贴图|MAX external textures/ })
          await dialog.getByRole('button', { name: /添加贴图|Add texture/ }).click()
          const entries = Object.entries(model.conversion_options.texture_files)
          expect(entries).toHaveLength(1)
          await dialog.getByRole('textbox', { name: /第 1 项贴图引用|Texture reference 1/ }).fill(entries[0][0])
          await dialog.getByRole('textbox', { name: /第 1 项图片路径|Image path 1/ }).fill(entries[0][1])
          await dialog.getByRole('button', { name: /保存|Save/, exact: true }).click()
          await expect(dialog).not.toBeVisible()
          await expect(options.getByRole('button', { name: /外部贴图（1）|External textures \(1\)/ })).toBeVisible()
          // Reopen to verify the exact declaration survived component submission.
          await options.getByRole('button', { name: /外部贴图（1）|External textures \(1\)/ }).click()
          await expect(dialog.getByRole('textbox', { name: /第 1 项贴图引用|Texture reference 1/ })).toHaveValue(entries[0][0])
          await expect(dialog.getByRole('textbox', { name: /第 1 项图片路径|Image path 1/ })).toHaveValue(entries[0][1])
          await dialog.getByRole('button', { name: /取消|Cancel/, exact: true }).click()
        }
        gpuDiagnostics.setStage(`generation-${model.format}:screenshot`)
        await modelFrame.locator('.preview-panel').screenshot({
          path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `model-${model.format}-generation-entry.png`)
        })
        gpuDiagnostics.setStage('navigation')
        modelEvidence.push({ ...model, generation_entry_visible: true, ...sourceProof, ...(model.format === 'max' ? { conversion_options_verified: true } : {}) })
      }
      expect(modelGenerationRequests).toBe(0)
      expect(failedBusinessResponses).toEqual([])
      expect(browserMessages).toEqual([])
      expect(anonymousRefresh401).toBeLessThanOrEqual(1)
      writeFileSync(resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `manager-internal-artifact-lineage-browser-${phase}.json`),
        `${JSON.stringify({
          schema_version: 'addp.manager-internal-artifact-lineage-browser/v10', phase,
          suite: 'manager-internal-artifact-lineage', run_id: env.ADDP_ONLINE_TEST_RUN_ID,
          result: 'passed', models: modelEvidence, model_generation_requests: modelGenerationRequests,
          browser_warning_errors: 0, failed_business_responses: 0,
          anonymous_refresh_401: anonymousRefresh401, gpu_performance_diagnostics: gpuDiagnostics.snapshot()
        })}\n`, 'utf8')
      return
    }

    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const lineage = frame.locator('.execution-lineage')
    await expect(lineage).toBeVisible()
    const groups = lineage.locator('.execution-lineage__group')
    await expect(groups).toHaveCount(2)
    const inputCards = groups.nth(0).locator('.execution-lineage__card')
    const outputCards = groups.nth(1).locator('.execution-lineage__card')
    await expect(inputCards).toHaveCount(1)
    await expect(outputCards).toHaveCount(1)
    await expect(inputCards.first()).toContainText(env.ADDP_ONLINE_MANAGER_LINEAGE_SOURCE_NAME)
    await expect(inputCards.first()).toContainText(String(itemID))
    await expect(inputCards.first().locator('.execution-lineage__resource-action')).toHaveCount(1)
    await expect(outputCards.first()).toContainText(env.ADDP_ONLINE_MANAGER_LINEAGE_OUTPUT_NAME)
    await expect(outputCards.first()).toContainText(/平台内部产物|Platform-internal artifact/)
    await expect(outputCards.first().locator('.execution-lineage__resource-action')).toHaveCount(0)

    gpuDiagnostics.setStage('pptx-preview')
    const dataExplorerPath = `/manager/data-explorer?locator=${encodeURIComponent(env.ADDP_ONLINE_MANAGER_PPTX_ITEM_LOCATOR)}`
    await page.goto(dataExplorerPath)
    const explorerFrame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const pdfPreview = explorerFrame.locator('.pptx-preview .pdf-preview')
    await expect(pdfPreview).toBeVisible({ timeout: 60_000 })
    await expect(pdfPreview.locator('.page-info')).toContainText(`/ ${pptxPageCount}`)
    await pdfPreview.locator('.toolbar-left .el-button-group .el-button').nth(1).click()
    const currentPageInput = pdfPreview.locator('.page-info input')
    await expect(currentPageInput).toHaveValue('2')
    const engineRequestsAfterPreviewReady = managerEngineRequests
    await expect.poll(() => managerEngineRequests, { timeout: 25_000 }).toBeGreaterThan(engineRequestsAfterPreviewReady)
    await expect(currentPageInput).toHaveValue('2')
    expect(pptxGenerationRequests).toBe(0)

    gpuDiagnostics.setStage('raster-cog:preview')
    const cogResponse = page.waitForResponse(response =>
      new URL(response.url()).pathname === raster.preview_url && response.status() === 206
      && Boolean(response.request().headers().range)
    )
    await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(raster.locator)}`)
    const rasterFrame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const rasterPreview = rasterFrame.locator('.raster-tiff-quick-view')
    await expect(rasterPreview).toBeVisible({ timeout: 60_000 })
    const cogRange = await cogResponse
    expect((await cogRange.body()).length).toBeGreaterThan(0)
    expect(cogRange.headers()['content-range']).toMatch(/^bytes [0-9]+-[0-9]+\/[0-9]+$/)
    await expect(rasterPreview.locator('.loading-overlay')).toHaveCount(0, { timeout: 60_000 })
    await expect(rasterPreview.locator('.map-empty')).toHaveCount(0)
    // OpenLayers puts the layer class on the WebGL canvas, but on the
    // containing div for the base map's 2D canvas. Check the COG layer itself.
    await expect(rasterPreview.locator('canvas.ol-layer')).toBeVisible()
    gpuDiagnostics.setStage('raster-cog:screenshot')
    await rasterPreview.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'raster-cog-preview.png') })
    expect(rasterGenerationRequests).toBe(0)

    const modelEvidence = []
    for (const model of models) {
      gpuDiagnostics.setStage(`model-${model.format}:preview`)
      const contentResponse = page.waitForResponse(response =>
        new URL(response.url()).pathname === model.preview_url && response.status() === 200
      )
      await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(model.locator)}`)
      const modelFrame = page.frameLocator('iframe[data-testid="module-iframe"]')
      const preview = modelFrame.locator('.model-preview')
      await expect(preview).toBeVisible({ timeout: 60_000 })
      await expect(preview.locator('canvas')).toBeVisible()
      const response = await contentResponse
      expect((await response.body()).subarray(0, 4).toString('ascii')).toBe('glTF')
      await expect(preview.locator('.three-status')).toHaveCount(0, { timeout: 60_000 })
      await expect(modelFrame.getByRole('button', { name: /生成 GLB 快显|Generate GLB Quick View/ })).toHaveCount(0)
      gpuDiagnostics.setStage(`model-${model.format}:screenshot`)
      await preview.screenshot({ path: resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `model-${model.format}-preview.png`) })
      gpuDiagnostics.setStage('navigation')
      const sourceProof = ['fbx', 'obj'].includes(model.format)
        ? { download_sha256: (await verifyOriginalDownload(page, modelFrame, model)).checksum } : {}
      modelEvidence.push({ ...model, model_loaded: true, content_loaded: true, ...sourceProof })
    }
    const delimitedEvidence = []
    gpuDiagnostics.setStage('navigation')
    for (const source of delimited) {
      delimitedEvidence.push(await verifyDelimitedTable(page, api, source, env.ADDP_ONLINE_ARTIFACT_DIR))
    }
    const workbookEvidence = await verifyWorkbook(page, api, workbook, env.ADDP_ONLINE_ARTIFACT_DIR)
    const structuredEvidence = []
    for (const source of structuredJSON) {
      structuredEvidence.push(await verifyStructuredJSON(page, api, source, env.ADDP_ONLINE_ARTIFACT_DIR))
    }
    expect(modelGenerationRequests).toBe(0)
    expect(failedBusinessResponses).toEqual([])
    expect(browserMessages).toEqual([])
    expect(anonymousRefresh401).toBeLessThanOrEqual(1)

    const report = {
      schema_version: 'addp.manager-internal-artifact-lineage-browser/v10',
      phase,
      suite: 'manager-internal-artifact-lineage',
      run_id: env.ADDP_ONLINE_TEST_RUN_ID,
      result: 'passed',
      execution_id: env.ADDP_ONLINE_MANAGER_LINEAGE_EXECUTION_ID,
      item_id: itemID,
      output_name: env.ADDP_ONLINE_MANAGER_LINEAGE_OUTPUT_NAME,
      input_resources: 1,
      output_resources: 1,
      platform_internal_outputs: 1,
      pptx_item_id: pptxItemID,
      pptx_page_count: pptxPageCount,
      pptx_page_after_engine_refresh: 2,
      pptx_generation_requests: pptxGenerationRequests,
      model_generation_requests: modelGenerationRequests,
      raster_generation_requests: rasterGenerationRequests,
      raster: { ...raster, range_loaded: true, map_loaded: true },
      models: modelEvidence,
      delimited_tables: delimitedEvidence,
      workbook: workbookEvidence,
      structured_json: structuredEvidence,
      gpu_performance_diagnostics: gpuDiagnostics.snapshot(),
      browser_warning_errors: 0,
      failed_business_responses: 0,
      anonymous_refresh_401: anonymousRefresh401
    }
    writeFileSync(
      resolve(env.ADDP_ONLINE_ARTIFACT_DIR, `manager-internal-artifact-lineage-browser-${phase}.json`),
      `${JSON.stringify(report)}\n`,
      'utf8'
    )
  } finally {
    await api.dispose()
  }
})
