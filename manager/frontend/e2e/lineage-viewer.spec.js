import { expect, test } from '@playwright/test'
import { observeLineageCanvas, lineageCanvasText, lineageCanvasSnapshot, dragLineageTable, toggleLineageTableFields } from '../../../common-frontend/basic/tests/fixtures/lineageCanvas.js'
import { managerAuthContext } from './managerAuthContext.js'
import { FIELD_CARD_WIDTH, FIELD_FONT_SIZE, FIELD_HEADER_HEIGHT, FIELD_ROW_HEIGHT } from '../../../common-frontend/graph/src/lineageFields.js'

const locator = 'addp://engine/9/path/public/current?type=table&item_id=3'
const node = id => ({ kind: 'data_item', item_id: id, name: id === 3 ? 'current' : `source_${id}`, full_name: `public.table_${id}`, engine_id: 9, engine_name: 'Lineage PostgreSQL', item_type: 'table' })
const edge = (source, target) => ({ source: node(source), target: node(target), relation_kind: 'derive', granularity: 'item' })

test('canvas observations wait for queued text and path repaint', async ({ page }) => {
  await observeLineageCanvas(page)
  await page.goto('about:blank')
  await page.setContent('<div class="lineage-canvas"><canvas width="400" height="200"></canvas></div>')
  const canvas = page.locator('canvas')
  for (const read of [lineageCanvasText, lineageCanvasSnapshot]) {
    await canvas.evaluate(element => {
      const context = element.getContext('2d')
      const paint = x => {
        context.clearRect(0, 0, element.width, element.height)
        context.font = '18px sans-serif'
        context.fillText('current', x, 80)
        context.beginPath()
        context.moveTo(10, 80)
        context.bezierCurveTo(50, 80, 80, 80, x, 80)
        context.stroke()
      }
      paint(20)
      // G6 queues Canvas repaint; a DOM click can finish before pixels update.
      requestAnimationFrame(() => requestAnimationFrame(() => paint(120)))
    })
    const observation = await read(canvas)
    const rows = Array.isArray(observation) ? observation : observation.rows
    expect(rows.find(row => row.text === 'current').x).toBe(120)
    if (observation.paths) expect(observation.paths.at(-1).at(-1).x).toBe(120)
  }
})

for (const theme of ['light', 'dark']) {
  test(`hundred-column branched lineage preserves every field port in ${theme} theme`, async ({ page }) => {
    await observeLineageCanvas(page)
    await page.setViewportSize({ width: 1600, height: 1000 })
    await page.addInitScript(theme => {
      localStorage.setItem('addp-lang', 'zh-cn')
      localStorage.setItem('theme-mode', theme)
    }, theme)
    const counts = [100, 18, 12, 20, 14, 24]
    const tables = counts.map((count, index) => Array.from({ length: count }, (_, column) => ({
      ...node(index + 1), kind: 'field_ref', field_name: `field_${index + 1}.${column}`,
      schema_snapshot_hash: `sha256:table-${index + 1}`, field_lineage_status: 'complete'
    })))
    const links = []
    for (const [source, target, count] of [[1, 4, 20], [1, 5, 14], [2, 6, 18], [4, 3, 12], [5, 3, 12], [6, 3, 12]]) {
      for (let index = 0; index < count; index++) links.push({
        source: tables[source - 1][index], target: tables[target - 1][index],
        relation_kind: 'derive', granularity: 'field', transformation: 'direct'
      })
    }
    expect(tables.flat()).toHaveLength(188)
    expect(links).toHaveLength(88)
    let graphRequests = 0
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
    await page.route('**/api/v1/**', route => {
      const url = new URL(route.request().url()), path = url.pathname
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'lineage-e2e-token', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'lineage-e2e', local_account: { username: 'lineage-e2e' } })
      if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
      if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 9, name: 'Lineage PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
      if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ id: locator, locator, label: 'current', type: 'table', metadata: { item_id: 3 } }] })
      if (path.endsWith('/meta/items/3')) return json(route, { ...node(3), attributes: { type_info: { table: { fields: tables[2].map(field => ({ name: field.field_name, type: 'string' })) } } } })
      if (path.endsWith('/meta/lineage/graph')) {
        if (url.searchParams.get('granularity') !== 'field') return json(route, { granularity: 'item', subject: node(3), nodes: [node(3)], edges: [] })
        graphRequests++
        return json(route, { granularity: 'field', subject: { ...node(3), schema_snapshot_hash: 'sha256:table-3' }, nodes: tables.flat(), edges: links })
      }
      return json(route, {})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
    await page.getByText('字段级', { exact: true }).click()
    const canvas = page.locator('.lineage-canvas canvas')
    await expect(canvas).toBeVisible()
    await expect(page.locator('.lineage-summary')).toContainText('188 个节点 · 88 条关系')
    // Entry focuses the root at a readable size, even when a source has 100 columns.
    // G6's Float32 viewport matrix introduces sub-millionth-pixel rounding.
    await expect.poll(async () => (await lineageCanvasText(canvas)).find(row => row.text === 'field_3.0')?.fontSize).toBeCloseTo(11, 5)
    await page.getByRole('button', { name: '适应窗口', exact: true }).click()
    const fieldRows = rows => rows.filter(row => /^field_\d+\.\d+$/.test(row.text))
    const headerName = id => id === 3 ? 'current' : `source_${id}`
    const verifyCards = async (collapsed = false) => {
      const rows = await lineageCanvasText(canvas)
      const zoom = rows.find(row => row.text === 'current').fontSize / 15
      const box = await canvas.boundingBox()
      const cards = counts.map((count, index) => {
        const title = rows.find(row => row.text === headerName(index + 1))
        return { x: title.x - 12 * zoom, y: title.y - 22 * zoom,
          width: FIELD_CARD_WIDTH * zoom,
          height: (FIELD_HEADER_HEIGHT + (collapsed ? 1 : count) * FIELD_ROW_HEIGHT + 8) * zoom }
      })
      for (const card of cards) {
        expect(card.x).toBeGreaterThanOrEqual(0)
        expect(card.y).toBeGreaterThanOrEqual(0)
        expect(card.x + card.width).toBeLessThanOrEqual(box.width)
        expect(card.y + card.height).toBeLessThanOrEqual(box.height)
      }
      for (let i = 0; i < cards.length; i++) for (let j = i + 1; j < cards.length; j++) {
        const a = cards[i], b = cards[j]
        expect(a.x + a.width <= b.x + 1 || b.x + b.width <= a.x + 1 ||
          a.y + a.height <= b.y + 1 || b.y + b.height <= a.y + 1).toBe(true)
      }
    }
    const verifyPorts = async (collapsedSource = false) => {
      await expect.poll(async () => {
        const { rows, paths } = await lineageCanvasSnapshot(canvas)
        const zoom = rows.find(row => row.text === 'current')?.fontSize / 15
        return links.every(link => {
          const source = rows.find(row => row.text === (collapsedSource && link.source.item_id === 1 ? '100 个字段' : link.source.field_name))
          const target = rows.find(row => row.text === link.target.field_name)
          return source && target && paths.some(points =>
            points.some(point => point.command === 'bezierCurveTo') &&
            Math.abs(points[0].x - source.x - (FIELD_CARD_WIDTH - 12) * zoom) < 2 &&
            Math.abs(points[0].y - source.y) < 2 &&
            Math.abs(points.at(-1).x - target.x + 12 * zoom) < 2 &&
            Math.abs(points.at(-1).y - target.y) < 2)
        })
      }).toBe(true)
    }
    await expect.poll(async () => fieldRows(await lineageCanvasText(canvas)).length).toBe(188)
    await verifyCards()
    await verifyPorts()
    const header = (await lineageCanvasText(canvas)).find(row => row.text === 'source_1')
    const stationary = (await lineageCanvasText(canvas)).find(row => row.text === 'current')
    await toggleLineageTableFields(page, canvas, 'source_1')
    await expect.poll(async () => fieldRows(await lineageCanvasText(canvas)).length).toBe(88)
    expect((await lineageCanvasText(canvas)).find(row => row.text === 'source_1').y).toBeCloseTo(header.y, 1)
    await dragLineageTable(page, canvas, 'source_1', 35, 30)
    await expect.poll(async () => (await lineageCanvasText(canvas)).find(row => row.text === 'source_1')?.y).toBeCloseTo(header.y + 30, 1)
    const unchanged = (await lineageCanvasText(canvas)).find(row => row.text === 'current')
    expect(unchanged.x).toBeCloseTo(stationary.x, 1)
    expect(unchanged.y).toBeCloseTo(stationary.y, 1)
    await verifyPorts(true)
    await toggleLineageTableFields(page, canvas, 'source_1')
    await expect.poll(async () => fieldRows(await lineageCanvasText(canvas)).length).toBe(188)
    await verifyPorts()
    await page.getByRole('button', { name: '收起字段', exact: true }).click()
    await page.getByRole('button', { name: '自动布局', exact: true }).click()
    await expect(page.getByRole('button', { name: '自动布局', exact: true })).toBeEnabled()
    await expect.poll(async () => fieldRows(await lineageCanvasText(canvas)).length).toBe(0)
    await verifyCards(true)
    await page.screenshot({ path: `/tmp/addp-field-hundred-collapsed-${theme}.png` })
    const compactFont = (await lineageCanvasText(canvas)).find(row => row.text === '12 个字段').fontSize
    const search = page.getByRole('textbox', { name: '搜索字段', exact: true })
    await search.fill('FIELD_3.11')
    await expect(page.locator('.lineage-field-options button')).toHaveCount(2)
    await page.getByRole('button', { name: 'field_3.11', exact: true }).click()
    await expect(page.locator('.lineage-inspector strong')).toHaveText('field_3.11')
    // All six tables belong to this reconverging field chain and must reopen.
    await expect(page.getByRole('button', { name: '展开字段', exact: true })).toBeDisabled()
    await expect.poll(async () => {
      const selected = (await lineageCanvasText(canvas)).find(row => row.text === 'field_3.11')
      return selected && Math.abs(selected.y - (await canvas.boundingBox()).height / 2)
    }).toBeLessThan(3)
    expect((await lineageCanvasText(canvas)).find(row => row.text === 'field_3.11').fontSize).toBeGreaterThanOrEqual(11)
    expect((await lineageCanvasText(canvas)).find(row => row.text === 'field_3.11').fontSize).toBeCloseTo(compactFont, 5)
    // Reopening tall branches from a compact layout must not overlap visible cards.
    // Recover each card's bounds from a painted field when its header is offscreen.
    const focusedRows = fieldRows(await lineageCanvasText(canvas))
    const focusedZoom = focusedRows.find(row => row.text === 'field_3.11').fontSize / FIELD_FONT_SIZE
    const visibleCards = counts.flatMap((count, index) => {
      const field = focusedRows.find(row => row.text.startsWith(`field_${index + 1}.`))
      if (!field) return []
      const column = Number(field.text.split('.').at(-1))
      return [{ id: index + 1, x: field.x - 12 * focusedZoom,
        y: field.y - (FIELD_HEADER_HEIGHT + (column + 0.5) * FIELD_ROW_HEIGHT) * focusedZoom,
        width: FIELD_CARD_WIDTH * focusedZoom,
        height: (FIELD_HEADER_HEIGHT + count * FIELD_ROW_HEIGHT + 8) * focusedZoom }]
    })
    expect(visibleCards.length).toBeGreaterThanOrEqual(2)
    for (let i = 0; i < visibleCards.length; i++) for (let j = i + 1; j < visibleCards.length; j++) {
      const a = visibleCards[i], b = visibleCards[j]
      expect(a.x + a.width <= b.x + 1 || b.x + b.width <= a.x + 1 ||
        a.y + a.height <= b.y + 1 || b.y + b.height <= a.y + 1,
      `expanded table ${a.id} overlaps table ${b.id}`).toBe(true)
    }
    await page.screenshot({ path: `/tmp/addp-field-hundred-focus-${theme}.png` })
    await search.fill('')
    await page.getByRole('button', { name: '全部字段', exact: true }).click()
    await page.getByRole('button', { name: '自动布局', exact: true }).click()
    await expect(page.getByRole('button', { name: '自动布局', exact: true })).toBeEnabled()
    await expect.poll(async () => fieldRows(await lineageCanvasText(canvas)).length).toBe(188)
    await verifyCards()
    await verifyPorts()
    expect(graphRequests).toBe(1)
    expect(errors).toEqual([])
  })
}

for (const theme of ['light', 'dark']) {
  test(`hundred-column target locates last and generated fields in ${theme} theme`, async ({ page }) => {
    await observeLineageCanvas(page)
    await page.setViewportSize({ width: 1600, height: 1000 })
    await page.addInitScript(theme => {
      localStorage.setItem('addp-lang', 'zh-cn')
      localStorage.setItem('theme-mode', theme)
    }, theme)
    const fields = (id, count) => Array.from({ length: count }, (_, index) => ({
      ...node(id), kind: 'field_ref', field_name: `field_${id}.${index}`,
      schema_snapshot_hash: `sha256:table-${id}`, field_lineage_status: 'complete'
    }))
    const sources = fields(1, 3), targets = fields(3, 100)
    const links = [0, 49, 99].map((column, index) => ({
      source: sources[index], target: targets[column], relation_kind: 'derive', granularity: 'field',
      transformation: 'direct', evidence: { execution_id: `target-column-${column}` }
    }))
    let graphRequests = 0
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
    await page.route('**/api/v1/**', route => {
      const url = new URL(route.request().url()), path = url.pathname
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'lineage-e2e-token', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'lineage-e2e', local_account: { username: 'lineage-e2e' } })
      if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
      if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 9, name: 'Lineage PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
      if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ id: locator, locator, label: 'current', type: 'table', metadata: { item_id: 3 } }] })
      if (path.endsWith('/meta/items/3')) return json(route, { ...node(3), attributes: { type_info: { table: { fields: targets.map(field => ({ name: field.field_name, type: 'string' })) } } } })
      if (path.endsWith('/meta/lineage/graph')) {
        if (url.searchParams.get('granularity') !== 'field') return json(route, { granularity: 'item', subject: node(3), nodes: [node(3)], edges: [] })
        graphRequests++
        return json(route, { granularity: 'field', subject: { ...node(3), schema_snapshot_hash: 'sha256:table-3' }, nodes: [...sources, ...targets], edges: links })
      }
      return json(route, {})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
    await page.getByText('字段级', { exact: true }).click()
    const canvas = page.locator('.lineage-canvas canvas')
    await expect(canvas).toBeVisible()
    await expect(page.locator('.lineage-field-options button')).toHaveCount(101)
    await expect(page.locator('.lineage-summary')).toContainText('103 个节点 · 3 条关系')
    await expect.poll(async () => (await lineageCanvasText(canvas)).find(row => /^field_3\./.test(row.text))?.fontSize).toBeCloseTo(11, 5)
    const search = page.getByRole('textbox', { name: '搜索字段', exact: true })
    const locate = async column => {
      await search.fill(`FIELD_3.${column}`)
      await expect(page.locator('.lineage-field-options button')).toHaveCount(2)
      await page.getByRole('button', { name: `field_3.${column}`, exact: true }).click()
      await expect(page.locator('.lineage-inspector strong')).toHaveText(`field_3.${column}`)
      await expect.poll(async () => {
        const row = (await lineageCanvasText(canvas)).find(row => row.text === `field_3.${column}`)
        return row && Math.abs(row.y - (await canvas.boundingBox()).height / 2)
      }).toBeLessThan(3)
      expect((await lineageCanvasText(canvas)).find(row => row.text === `field_3.${column}`).fontSize).toBeCloseTo(11, 5)
    }
    await page.getByRole('button', { name: '收起字段', exact: true }).click()
    await locate(98)
    await expect(page.locator('.lineage-field-status')).toContainText('没有关联字段')
    // A generated field opens only its own table; its source stays folded.
    await expect(page.getByRole('button', { name: '展开字段', exact: true })).toBeEnabled()
    await locate(99)
    await expect(page.locator('.lineage-field-status')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '展开字段', exact: true })).toBeDisabled()
    await search.fill('')
    await page.getByRole('button', { name: '全部字段', exact: true }).click()
    await page.getByRole('button', { name: '适应窗口', exact: true }).click()
    const ports = async () => {
      await expect.poll(async () => {
        const { rows, paths } = await lineageCanvasSnapshot(canvas)
        const zoom = rows.find(row => row.text === 'current')?.fontSize / 15
        return links.every(link => {
          const source = rows.find(row => row.text === link.source.field_name)
          const target = rows.find(row => row.text === link.target.field_name)
          return source && target && paths.some(points => points.some(point => point.command === 'bezierCurveTo') &&
            Math.abs(points[0].x - source.x - 212 * zoom) < 2 && Math.abs(points[0].y - source.y) < 2 &&
            Math.abs(points.at(-1).x - target.x + 12 * zoom) < 2 && Math.abs(points.at(-1).y - target.y) < 2)
        })
      }).toBe(true)
    }
    await ports()
    const title = (await lineageCanvasText(canvas)).find(row => row.text === 'current')
    await dragLineageTable(page, canvas, 'current', 0, 25)
    await expect.poll(async () => (await lineageCanvasText(canvas)).find(row => row.text === 'current')?.y).toBeCloseTo(title.y + 25, 1)
    await ports()
    // The cubic midpoint remains clickable and exposes the exact evidence.
    const { rows, paths } = await lineageCanvasSnapshot(canvas)
    const target = rows.find(row => row.text === 'field_3.99')
    const zoom = target.fontSize / FIELD_FONT_SIZE
    const curve = paths.find(points => points.some(point => point.command === 'bezierCurveTo') &&
      Math.abs(points.at(-1).y - target.y) < 2 && Math.abs(points.at(-1).x - target.x + 12 * zoom) < 2)
    await canvas.click({ position: { x: (curve[0].x + curve.at(-1).x) / 2, y: (curve[0].y + curve.at(-1).y) / 2 } })
    await expect(page.locator('.lineage-inspector')).toContainText('target-column-99')
    await expect(page.locator('.lineage-inspector')).toContainText('field_3.99')
    await expect(page.locator('.lineage-summary')).toContainText('103 个节点 · 3 条关系')
    expect(graphRequests).toBe(1)
    expect(errors).toEqual([])
  })
}

test('lineage fills the viewport, controls query depth and survives resizing and empty results', async ({ page }) => {
  const requests = []
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'lineage-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'lineage-e2e', local_account: { username: 'lineage-e2e' } })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 9, name: 'Lineage PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ id: locator, locator, label: 'current', type: 'table', metadata: { item_id: 3 } }] })
    if (path.endsWith('/meta/lineage/graph')) {
      const depth = Number(url.searchParams.get('depth'))
      requests.push({ depth, item: url.searchParams.get('item_id'), direction: url.searchParams.get('direction') })
      if (depth === 20) return json(route, { subject: node(3), nodes: [node(3)], edges: [], truncated: true })
      return json(route, { subject: node(3), nodes: [node(1), node(2), node(3), node(4)], edges: [edge(1, 3), edge(2, 3), edge(3, 4)] })
    }
    return json(route, {})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
  await expect(page.locator('.lineage-canvas canvas')).toBeVisible()
  expect(requests.at(-1)).toEqual({ depth: 2, item: '3', direction: 'both' })
  const checkHeight = async () => {
    const box = await page.locator('.lineage-canvas').boundingBox()
    expect(box.height).toBeGreaterThan(page.viewportSize().height * 0.6)
    expect(page.viewportSize().height - box.y - box.height).toBeLessThan(60)
    const canvas = await page.locator('.lineage-canvas canvas').boundingBox()
    expect(Math.abs(canvas.height - box.height)).toBeLessThan(2)
  }
  await checkHeight()
  await page.setViewportSize({ width: 1600, height: 1100 })
  await expect.poll(async () => (await page.locator('.lineage-canvas canvas').boundingBox()).height).toBeGreaterThan(800)
  await checkHeight()
  await page.locator('.lineage-depth .el-select__wrapper').click()
  await page.getByRole('option', { name: '3 层', exact: true }).click()
  await expect.poll(() => requests.at(-1).depth).toBe(3)
  await page.getByRole('button', { name: '适应窗口' }).click()
  await page.getByRole('button', { name: '放大', exact: true }).click()
  await page.locator('.lineage-depth .el-select__wrapper').click()
  await page.getByRole('option', { name: '20 层', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已达到显示上限')
  await expect(page.locator('.lineage-summary')).toContainText('1 个节点 · 0 条关系')
  await page.locator('.lineage-depth .el-select__wrapper').click()
  await page.getByRole('option', { name: '1 层', exact: true }).click()
  await expect(page.locator('.lineage-summary')).toContainText('4 个节点 · 3 条关系')
  expect(errors).toEqual([])
})

function json(route, body) {
  return route.fulfill({ contentType: 'application/json', body: JSON.stringify(body) })
}

test('shows all table fields in one query and focuses fields without fetching again', async ({ page }) => {
  const requests = []
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
  const field = (id, name, status) => ({ ...node(id), kind: 'field_ref', field_name: name, schema_snapshot_hash: `sha256:table-${id}`, field_lineage_status: status })
  const roots = [field(3, 'client.id', 'complete'), field(3, 'generated', 'complete'), field(3, 'missing', 'unavailable')]
  const source = field(1, 'nested.id')
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path.endsWith('/system/refresh')) return json(route, { access_token: 'lineage-e2e-token', expires_in: 3600 })
    if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'lineage-e2e', local_account: { username: 'lineage-e2e' } })
    if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
    if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 9, name: 'Lineage PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
    if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ id: locator, locator, label: 'current', type: 'table', metadata: { item_id: 3 } }] })
    if (path.endsWith('/meta/items/3')) return json(route, { ...node(3), attributes: { type_info: { table: { fields: roots.map(field => ({ name: field.field_name, type: 'string' })) } } } })
    if (path.endsWith('/meta/lineage/graph')) {
      requests.push(Object.fromEntries(url.searchParams))
      if (url.searchParams.get('granularity') !== 'field') return json(route, { granularity: 'item', subject: node(3), nodes: [node(3)], edges: [] })
      return json(route, { granularity: 'field', subject: { ...node(3), schema_snapshot_hash: 'sha256:table-3' }, nodes: [...roots, source], edges: [{ source, target: roots[0], granularity: 'field', relation_kind: 'derive', transformation: 'direct' }], field_lineage_status: 'unavailable' })
    }
    return json(route, {})
  })
  await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
  await page.getByText('字段级', { exact: true }).click()
  await expect(page.locator('.lineage-fields')).toBeVisible()
  await expect(page.locator('.lineage-canvas canvas')).toBeVisible()
  expect(requests.at(-1)).toMatchObject({ subject_kind: 'data_item', granularity: 'field' })
  expect(requests.at(-1).field_name).toBeUndefined()
  const count = requests.length
  await page.getByRole('button', { name: 'client.id', exact: true }).click()
  await expect(page.locator('.lineage-inspector strong')).toHaveText('client.id')
  await expect(page.locator('.lineage-inspector')).toContainText('sha256:table-3')
  const heading = await page.locator('.lineage-inspector-heading').boundingBox()
  const details = await page.locator('.lineage-inspector-fields').boundingBox()
  expect(details.x).toBeGreaterThan(heading.x + heading.width)
  expect(details.y).toBeLessThan(heading.y + heading.height)
  await page.getByRole('button', { name: 'generated', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('已记录的字段血缘中没有关联字段')
  await page.getByRole('button', { name: 'missing', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('当前字段尚无可用血缘证据')
  await page.getByRole('button', { name: '全部字段', exact: true }).click()
  await expect(page.locator('.lineage-inspector')).toBeHidden()
  expect(requests.length).toBe(count)
  await page.screenshot({ path: '/tmp/addp-field-overview-e2e.png' })
  await page.getByText('数据项级', { exact: true }).click()
  await expect.poll(() => requests.at(-1).granularity).toBe('item')
  expect(errors).toEqual([])
})

test('expands a single direction from a node while keeping the current table and viewport', async ({ page }) => {
 const queries=[]
 await page.addInitScript(()=>localStorage.setItem('addp-lang','zh-cn'))
 await page.route('**/plugins/manifest.json',route=>json(route,{scripts:[]}))
 await page.route('**/api/v1/**',async route=>{
  const url=new URL(route.request().url()); const path=url.pathname
  if(path.endsWith('/system/refresh')) return json(route,{access_token:'lineage-e2e-token',expires_in:3600})
  if(path.endsWith('/system/users/me')) return json(route,{ id: '1', display_name: 'lineage-e2e', local_account: { username: 'lineage-e2e' } })
  if(path.endsWith('/system/auth/context')) return json(route,managerAuthContext)
  if(path.endsWith('/manager/engines')) return json(route,{data:[{id:9,name:'Lineage PostgreSQL',engine_type:'postgresql',lifecycle_state:'active',connection_status:'online'}]})
  if(path.endsWith('/ancestors')) return json(route,{target_locator:locator,ancestors:[{id:locator,locator,label:'current',type:'table',metadata:{item_id:3}}]})
  if(path.endsWith('/meta/lineage/graph')) {
   queries.push(Object.fromEntries(url.searchParams)); const expanded=url.searchParams.get('expand_upstream')==='3'
   const root={...node(3),hidden_upstream_count:expanded?0:2}
   return json(route,{subject:root,nodes:expanded?[node(1),node(2),root]:[root],edges:expanded?[edge(1,3),edge(2,3)]:[]})
  }
  return json(route,{})
 })
 await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
 const canvas=page.locator('.lineage-canvas canvas');await expect(canvas).toBeVisible()
 await expect(page.locator('.lineage-summary')).toContainText('1 个节点')
 const box=await canvas.boundingBox()
 await canvas.click({position:{x:box.width/2,y:box.height/2}})
 await expect(page.locator('.lineage-inspector strong')).toHaveText('current')
 await page.getByRole('button',{name:'上游 +2',exact:true}).click()
 await expect(page.locator('.lineage-summary')).toContainText('3 个节点 · 2 条关系')
 expect(queries.at(-1)).toMatchObject({item_id:'3',depth:'2',expand_upstream:'3'})
 expect(queries.at(-1).expand_downstream).toBeUndefined()
 await expect(page.locator('.lineage-inspector strong')).toHaveText('current')
 await expect(page.getByRole('button',{name:'上游 +2',exact:true})).toBeHidden()
 // A depth change deliberately resets local expansion.
 await page.locator('.lineage-depth .el-select__wrapper').click()
 await page.getByRole('option',{name:'3 层',exact:true}).click()
 await expect.poll(()=>queries.at(-1).depth).toBe('3')
 expect(queries.at(-1).expand_upstream).toBeUndefined()
 await expect(page.locator('.lineage-summary')).toContainText('1 个节点')
 const resetBox=await canvas.boundingBox()
 // Hit the drawn upstream badge; after expansion the root keeps its screen point.
 await canvas.click({position:{x:resetBox.width/2-142,y:resetBox.height/2}})
 await expect(page.locator('.lineage-summary')).toContainText('3 个节点')
 await canvas.click({position:{x:resetBox.width/2,y:resetBox.height/2}})
 await expect(page.locator('.lineage-inspector strong')).toHaveText('current')
})

test('edge evidence opens the source execution in Monitor', async ({ page }) => {
 await page.addInitScript(()=>localStorage.setItem('addp-lang','zh-cn'))
 await page.route('**/plugins/manifest.json',route=>json(route,{scripts:[]}))
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/system/refresh')) return json(route,{access_token:'lineage-e2e-token',expires_in:3600})
  if(path.endsWith('/system/users/me')) return json(route,{ id: '1', display_name: 'lineage-e2e', local_account: { username: 'lineage-e2e' } })
  if(path.endsWith('/system/auth/context')) return json(route,managerAuthContext)
  if(path.endsWith('/manager/engines')) return json(route,{data:[{id:9,name:'Lineage PostgreSQL',engine_type:'postgresql',lifecycle_state:'active',connection_status:'online'}]})
  if(path.endsWith('/ancestors')) return json(route,{target_locator:locator,ancestors:[{id:locator,locator,label:'current',type:'table',metadata:{item_id:3}}]})
  if(path.endsWith('/meta/lineage/graph')) return json(route,{subject:node(3),nodes:[node(1),node(3)],edges:[{...edge(1,3),evidence:{execution_id:'lineage-source-execution'}}]})
  return json(route,{})
 })
 await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
 const canvas=page.locator('.lineage-canvas canvas');await expect(canvas).toBeVisible()
 const box=await canvas.boundingBox()
 await canvas.click({position:{x:box.width/2,y:box.height/2}})
 await expect(page.locator('.lineage-inspector')).toContainText('lineage-source-execution')
 const popupPromise=page.waitForEvent('popup')
 await page.getByRole('button',{name:'查看来源执行'}).click()
 const popup=await popupPromise
 await expect(popup).toHaveURL(/\/monitor\/executions\?execution_id=lineage-source-execution/)
 await popup.close()
})

for (const catalogRead of [false, true]) {
  test(`lineage respects Catalog summary read permission (${catalogRead})`, async ({ page }) => {
    const catalogRequests = []
    const context = structuredClone(managerAuthContext)
    if (catalogRead) context.authorization.role_assignments[0].permissions.push('catalog.entry.read')
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
    await page.route('**/api/v1/**', route => {
      const url = new URL(route.request().url())
      const path = url.pathname
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'lineage-e2e-token', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'lineage-e2e', local_account: { username: 'lineage-e2e' } })
      if (path.endsWith('/system/auth/context')) return json(route, context)
      if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 9, name: 'Lineage PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
      if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ id: locator, locator, label: 'current', type: 'table', metadata: { item_id: 3 } }] })
      if (path.endsWith('/meta/items/3')) return json(route, { ...node(3), fingerprint: 'sha256:catalog-permission-item' })
      if (path.endsWith('/catalog/entries')) {
        catalogRequests.push(Object.fromEntries(url.searchParams))
        return json(route, { data: [{ id: 42, display_name: 'Current resource', governance_status: 'discovered', source_status: 'active' }] })
      }
      if (path.endsWith('/meta/lineage/graph')) return json(route, { subject: node(3), nodes: [node(1), node(3)], edges: [edge(1, 3)] })
      return json(route, {})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
    await expect(page.locator('.lineage-summary')).toContainText('2 个节点 · 1 条关系')
    await expect(page.locator('.lineage-canvas canvas')).toBeVisible()
    const catalog = page.locator('.resource-governance-summary__catalog')
    if (catalogRead) {
      await expect(catalog).toContainText('Current resource')
      expect(catalogRequests).toEqual([{ source_identity: 'sha256:catalog-permission-item', page: '1', page_size: '1' }])
    } else {
      await expect(catalog).toBeHidden()
      expect(catalogRequests).toEqual([])
    }
  })
}

for (const theme of ['light', 'dark']) {
  test(`three-hop field overview remains readable and clickable in ${theme} theme`, async ({ page }) => {
    await observeLineageCanvas(page)
    await page.addInitScript(theme => {
      localStorage.setItem('addp-lang', 'zh-cn')
      localStorage.setItem('theme-mode', theme)
      if (theme === 'dark') document.documentElement.classList.add('dark')
    }, theme)
    const names = ['activity_id', 'activity_date', 'person_display_name', 'intensity']
    const fields = (id, columns) => columns.map(name => ({ ...node(id), kind: 'field_ref', field_name: name, schema_snapshot_hash: `sha256:table-${id}`, field_lineage_status: 'complete' }))
    const longName = 'source.profile.very_long_nested_field_name_that_requires_truncation'
    const source = fields(1, names.slice(0, 3)), ods = fields(2, names.slice(0, 3)), dim = fields(4, names.slice(0, 3)), root = fields(3, [...names, longName])
    const links = [source, ods, dim.slice(0, 2)].flatMap((columns, layer) => columns.map((source, index) => ({ source, target: [ods, dim, root][layer][index], relation_kind: 'derive', granularity: 'field', transformation: 'direct' })))
    links.push({ source: ods[2], target: root[2], relation_kind: 'derive', granularity: 'field', transformation: 'direct' })
    let graphRequests = 0
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
    await page.route('**/api/v1/**', route => {
      const url = new URL(route.request().url()), path = url.pathname
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'lineage-e2e-token', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'lineage-e2e', local_account: { username: 'lineage-e2e' } })
      if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
      if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 9, name: 'Lineage PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
      if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ id: locator, locator, label: 'current', type: 'table', metadata: { item_id: 3 } }] })
      if (path.endsWith('/meta/items/3')) return json(route, { ...node(3), attributes: { type_info: { table: { fields: names.map(name => ({ name, type: 'string' })) } } } })
      if (path.endsWith('/meta/lineage/graph')) {
        if (url.searchParams.get('granularity') !== 'field') return json(route, { granularity: 'item', subject: node(3), nodes: [node(3)], edges: [] })
        graphRequests++
        return json(route, { granularity: 'field', subject: { ...node(3), schema_snapshot_hash: 'sha256:table-3' }, nodes: [...source, ...ods, ...dim, ...root], edges: links })
      }
      return json(route, {})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
    await page.getByText('字段级', { exact: true }).click()
    expect(await page.locator('html').evaluate(element => element.classList.contains('dark'))).toBe(theme === 'dark')
    await page.locator('.lineage-viewer').evaluate(element => { element.style.maxWidth = '820px' })
    const canvas = page.locator('.lineage-canvas canvas')
    await expect(canvas).toBeVisible()
    await expect.poll(async () => (await lineageCanvasText(canvas)).filter(row => names.includes(row.text)).length).toBe(13)
    const initialBox = await canvas.boundingBox()
    for (const row of (await lineageCanvasText(canvas)).filter(row => names.includes(row.text))) {
      expect(row.x).toBeGreaterThanOrEqual(0)
      expect(row.x + row.width).toBeLessThanOrEqual(initialBox.width)
    }
    await page.getByRole('button', { name: '适应窗口', exact: true }).click()
    await expect.poll(async () => (await lineageCanvasText(canvas)).filter(row => names.includes(row.text)).length).toBe(13)
    const rows = (await lineageCanvasText(canvas)).filter(row => names.includes(row.text))
    const box = await canvas.boundingBox()
    for (const row of rows) {
      expect(row.fontSize).toBeGreaterThanOrEqual(11)
      expect(row.x).toBeGreaterThanOrEqual(0)
      expect(row.x + row.width).toBeLessThanOrEqual(box.width)
      expect(row.y).toBeGreaterThan(row.fontSize)
      expect(row.y).toBeLessThan(box.height)
    }
    const buttonLabel = page.getByRole('button', { name: longName, exact: true }).locator('span')
    expect(await buttonLabel.evaluate(element => element.scrollWidth > element.clientWidth && getComputedStyle(element).textOverflow === 'ellipsis')).toBe(true)
    const truncated = (await lineageCanvasText(canvas)).find(row => row.text.startsWith('source.profile.'))
    expect(truncated.text).toMatch(/…$/)
    await canvas.hover({ position: { x: truncated.x + 10, y: truncated.y } })
    await expect(page.locator('.lineage-tooltip')).toContainText(longName)
    await canvas.click({ position: { x: truncated.x + 10, y: truncated.y } })
    await expect(page.locator('.lineage-inspector strong')).toHaveText(longName)
    const chosen = rows.filter(row => row.text === 'person_display_name').at(-1)
    await canvas.click({ position: { x: chosen.x + 10, y: chosen.y } })
    await expect(page.locator('.lineage-inspector strong')).toHaveText('person_display_name')
    await page.getByRole('button', { name: '全部字段', exact: true }).click()
    await page.getByRole('button', { name: '适应窗口', exact: true }).click()
    await page.screenshot({ path: `/tmp/addp-field-compact-${theme}.png` })
    const previousWidth = (await canvas.boundingBox()).width
    const previousFont = (await lineageCanvasText(canvas)).find(row => row.text === 'activity_id').fontSize
    await page.setViewportSize({ width: 1600, height: 1000 })
    await expect.poll(async () => (await canvas.boundingBox()).width).toBeGreaterThan(previousWidth)
    await expect.poll(async () => (await lineageCanvasText(canvas)).find(row => row.text === 'activity_id')?.fontSize).toBeCloseTo(previousFont, 2)
    expect(graphRequests).toBe(1)
  })
}

for (const theme of ['light', 'dark']) {
  test(`branched field graph supports search, drag and automatic layout in ${theme} theme`, async ({ page }) => {
    await observeLineageCanvas(page)
    await page.setViewportSize({ width: 1600, height: 1000 })
    await page.addInitScript(theme => {
      localStorage.setItem('addp-lang', 'zh-cn')
      localStorage.setItem('theme-mode', theme)
    }, theme)
    const counts = [6, 2, 9, 4, 3, 2, 2, 2, 2]
    const tables = counts.map((count, index) => Array.from({ length: count }, (_, column) => ({
      ...node(index + 1), kind: 'field_ref', field_name: `field_${index + 1}.${column}`,
      schema_snapshot_hash: `sha256:table-${index + 1}`, field_lineage_status: 'complete'
    })))
    const links = []
    const connect = (source, target, count) => {
      for (let i = 0; i < count; i++) links.push({ source: tables[source - 1][i], target: tables[target - 1][i],
        relation_kind: 'derive', granularity: 'field', transformation: 'direct' })
    }
    // Two sources, reconverging ODS/DIM/DWD branches and a final nine-field table.
    connect(1, 4, 4); connect(1, 5, 3); connect(2, 6, 2); connect(4, 7, 2)
    connect(5, 8, 2); connect(6, 9, 2); connect(7, 8, 2); connect(8, 3, 2)
    connect(9, 3, 2); connect(4, 3, 4)
    expect(tables.flat()).toHaveLength(32); expect(links).toHaveLength(25)
    let graphRequests = 0
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
    await page.route('**/api/v1/**', route => {
      const url = new URL(route.request().url()), path = url.pathname
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'lineage-e2e-token', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'lineage-e2e', local_account: { username: 'lineage-e2e' } })
      if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
      if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 9, name: 'Lineage PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
      if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ id: locator, locator, label: 'current', type: 'table', metadata: { item_id: 3 } }] })
      if (path.endsWith('/meta/items/3')) return json(route, { ...node(3), attributes: { type_info: { table: { fields: tables[2].map(field => ({ name: field.field_name, type: 'string' })) } } } })
      if (path.endsWith('/meta/lineage/graph')) {
        if (url.searchParams.get('granularity') !== 'field') return json(route, { granularity: 'item', subject: node(3), nodes: [node(3)], edges: [] })
        graphRequests++
        return json(route, { granularity: 'field', subject: { ...node(3), schema_snapshot_hash: 'sha256:table-3' }, nodes: tables.flat(), edges: links })
      }
      return json(route, {})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}&tab=lineage`)
    await page.getByText('字段级', { exact: true }).click()
    const canvas = page.locator('.lineage-canvas canvas')
    await expect(canvas).toBeVisible()
    await page.getByRole('button', { name: '适应窗口', exact: true }).click()
    await expect.poll(async () => (await lineageCanvasText(canvas)).filter(row => /^field_/.test(row.text)).length).toBe(32)
    const initialRows = await lineageCanvasText(canvas)
    const header = initialRows.find(row => row.text === 'source_1')
    const scale = header.fontSize / 15
    const cards = tables.map((fields, index) => {
      const title = initialRows.find(row => row.text === (index === 2 ? 'current' : `source_${index + 1}`))
      return { x: title.x - 12 * scale, y: title.y - 22 * scale,
        width: 224 * scale, height: (72 + fields.length * 34) * scale }
    })
    for (let i = 0; i < cards.length; i++) for (let j = i + 1; j < cards.length; j++) {
      const a = cards[i], b = cards[j]
      expect(a.x + a.width <= b.x + 1 || b.x + b.width <= a.x + 1 ||
        a.y + a.height <= b.y + 1 || b.y + b.height <= a.y + 1).toBe(true)
    }
    const portsMatch = async () => {
      await expect.poll(async () => {
        // Read paths and labels from the same painted frame, including hover redraws.
        const { rows, paths } = await lineageCanvasSnapshot(canvas)
        const fields = rows.filter(row => row.text.startsWith('field_1.')).slice(0, 4)
        const header = rows.find(row => row.text === 'source_1')
        const zoom = header?.fontSize / 15
        const targets = rows.filter(row => row.text.startsWith('field_3.')).slice(0, 4)
        return fields.length === 4 && targets.length === 4 && fields.every(row => paths.some(points =>
          points.some(point => point.command === 'bezierCurveTo') &&
          Math.abs(points[0].x - row.x - 212 * zoom) < 2 && Math.abs(points[0].y - row.y) < 2)) &&
          targets.every(row => paths.some(points => Math.abs(points.at(-1).x - row.x + 12 * zoom) < 2 &&
            Math.abs(points.at(-1).y - row.y) < 2))
      }).toBe(true)
    }
    await portsMatch()
    await dragLineageTable(page, canvas, 'source_1', 0, -60)
    await expect.poll(async () => (await lineageCanvasText(canvas)).find(row => row.text === 'source_1')?.y).toBeCloseTo(header.y - 60, 1)
    await portsMatch()
    const draggedRows = await lineageCanvasText(canvas)
    await page.locator('html').evaluate(element => element.classList.toggle('dark'))
    await expect.poll(async () => (await lineageCanvasText(canvas)).find(row => row.text === 'source_1')?.y).toBeCloseTo(header.y - 60, 1)
    for (const row of draggedRows.filter(row => row.text.startsWith('field_'))) {
      const current = (await lineageCanvasText(canvas)).find(current => current.text === row.text)
      expect(current.x).toBeCloseTo(row.x, 1)
      expect(current.y).toBeCloseTo(row.y, 1)
    }
    await portsMatch()
    await page.locator('html').evaluate(element => element.classList.toggle('dark'))
    // Fit changes only the viewport; dragging remains reversible through auto layout.
    await page.getByRole('button', { name: '适应窗口', exact: true }).click()
    const search = page.getByRole('textbox', { name: '搜索字段', exact: true })
    await search.fill('FIELD_3.1')
    await expect(page.locator('.lineage-field-options button')).toHaveCount(2)
    await expect(page.getByRole('button', { name: 'field_3.1', exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'field_3.1', exact: true }).click()
    await expect(page.locator('.lineage-inspector strong')).toHaveText('field_3.1')
    const selectedRow = (await lineageCanvasText(canvas)).find(row => row.text === 'field_3.1')
    expect(Math.abs(selectedRow.y - (await canvas.boundingBox()).height / 2)).toBeLessThan(3)
    await page.getByRole('button', { name: '自动布局', exact: true }).click()
    await expect(page.getByRole('button', { name: '自动布局', exact: true })).toBeEnabled()
    await expect(page.locator('.lineage-inspector strong')).toHaveText('field_3.1')
    // Native fullscreen keeps the same graph/selection, zoom and dragged positions.
    await search.fill('FIELD_3.1')
    const fontBeforeZoom = (await lineageCanvasText(canvas)).find(row => row.text === 'field_1.0').fontSize
    await page.getByRole('button', { name: '放大', exact: true }).click()
    await expect.poll(async () => (await lineageCanvasText(canvas)).find(row => row.text === 'field_1.0')?.fontSize).toBeCloseTo(fontBeforeZoom * 1.25, 1)
    const beforeFullscreen = await lineageCanvasText(canvas)
    const originalBounds = await canvas.boundingBox()
    await page.getByRole('button', { name: '全屏查看', exact: true }).click()
    await expect.poll(() => page.evaluate(() => document.fullscreenElement?.className)).toBe('lineage-viewer')
    await expect.poll(async () => (await canvas.boundingBox()).width).toBeGreaterThan(originalBounds.width + 100)
    await expect(page.getByRole('button', { name: '退出全屏', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await expect(search).toHaveValue('FIELD_3.1')
    await expect(page.locator('.lineage-inspector strong')).toHaveText('field_3.1')
    const verifyViewport = async () => {
      const current = await lineageCanvasText(canvas)
      for (const row of beforeFullscreen.filter(row => row.text.startsWith('field_'))) {
        const rendered = current.find(value => value.text === row.text)
        expect(rendered.x).toBeCloseTo(row.x, 1)
        expect(rendered.y).toBeCloseTo(row.y, 1)
        expect(rendered.fontSize).toBeCloseTo(row.fontSize, 1)
      }
    }
    await verifyViewport()
    // Element Plus popups must remain inside the fullscreen element.
    await page.locator('.lineage-depth .el-select__wrapper').click()
    await expect(page.getByRole('option', { name: '10 层', exact: true })).toBeVisible()
    await page.locator('.lineage-depth .el-select__wrapper').click()
    await page.getByRole('button', { name: '退出全屏', exact: true }).click()
    await expect.poll(() => page.evaluate(() => document.fullscreenElement)).toBeNull()
    await expect.poll(async () => (await canvas.boundingBox()).width).toBeCloseTo(originalBounds.width, 1)
    await expect.poll(async () => (await canvas.boundingBox()).height).toBeCloseTo(originalBounds.height, 1)
    await verifyViewport()
    await page.getByRole('button', { name: '全屏查看', exact: true }).click()
    await expect.poll(() => page.evaluate(() => document.fullscreenElement?.className)).toBe('lineage-viewer')
    // Browser exit (the same fullscreenchange emitted by Esc) synchronizes the control.
    await page.evaluate(() => document.exitFullscreen())
    await expect.poll(() => page.evaluate(() => document.fullscreenElement)).toBeNull()
    await expect.poll(async () => (await canvas.boundingBox()).width).toBeCloseTo(originalBounds.width, 1)
    await expect.poll(async () => (await canvas.boundingBox()).height).toBeCloseTo(originalBounds.height, 1)
    await expect(page.getByRole('button', { name: '全屏查看', exact: true })).toHaveAttribute('aria-pressed', 'false')
    await verifyViewport()
    await search.fill('absent')
    await expect(page.getByRole('status')).toContainText('没有匹配的字段')
    await search.fill('')
    await expect(page.locator('.lineage-field-options button')).toHaveCount(10)
    await page.getByRole('button', { name: '全部字段', exact: true }).click()
    await page.getByRole('button', { name: '自动布局', exact: true }).click()
    await expect(page.getByRole('button', { name: '自动布局', exact: true })).toBeEnabled()
    // Layout completion precedes Canvas repaint. Wait for the actual drawn positions.
    // Hover/border strokes can shift the fitted bounds by a pixel.
    await expect.poll(async () => {
      const finalRows = await lineageCanvasText(canvas)
      return initialRows.filter(row => row.text.startsWith('field_')).every(initial => {
        const current = finalRows.find(row => row.text === initial.text)
        return current && Math.abs(current.x - initial.x) < 2 && Math.abs(current.y - initial.y) < 2
      })
    }).toBe(true)
    await portsMatch()
    const beforeCollapse = (await lineageCanvasText(canvas)).find(row => row.text === 'source_1')
    await toggleLineageTableFields(page, canvas, 'source_1')
    await expect.poll(async () => (await lineageCanvasText(canvas)).filter(row => /^field_/.test(row.text)).length).toBe(26)
    const collapsedHeader = (await lineageCanvasText(canvas)).find(row => row.text === 'source_1')
    expect(collapsedHeader.x).toBeCloseTo(beforeCollapse.x, 1)
    expect(collapsedHeader.y).toBeCloseTo(beforeCollapse.y, 1)
    expect(collapsedHeader.fontSize).toBeCloseTo(beforeCollapse.fontSize, 1)
    await dragLineageTable(page, canvas, 'source_1', 0, -30)
    await expect.poll(async () => (await lineageCanvasText(canvas)).find(row => row.text === 'source_1')?.y).toBeCloseTo(beforeCollapse.y - 30, 1)
    await expect.poll(async () => {
      const folded = await lineageCanvasSnapshot(canvas)
      const summary = folded.rows.find(row => row.text === '6 个字段')
      if (!summary) return false
      const collapsedZoom = summary.fontSize / 18
      return folded.paths.some(points => Math.abs(points[0].x - summary.x - 212 * collapsedZoom) < 2 && Math.abs(points[0].y - summary.y) < 2)
    }).toBe(true)
    await toggleLineageTableFields(page, canvas, 'source_1')
    await expect.poll(async () => (await lineageCanvasText(canvas)).filter(row => /^field_/.test(row.text)).length).toBe(32)
    await expect.poll(async () => (await lineageCanvasText(canvas)).find(row => row.text === 'source_1')?.y).toBeCloseTo(beforeCollapse.y - 30, 1)
    await portsMatch()
    await page.getByRole('button', { name: '收起字段', exact: true }).click()
    await expect.poll(async () => (await lineageCanvasText(canvas)).filter(row => /^field_/.test(row.text)).length).toBe(0)
    await expect(page.getByRole('button', { name: '收起字段', exact: true })).toBeDisabled()
    await expect(page.locator('.lineage-summary')).toContainText('32 个节点 · 25 条关系')
    await page.getByRole('button', { name: '自动布局', exact: true }).click()
    await expect(page.getByRole('button', { name: '自动布局', exact: true })).toBeEnabled()
    await page.screenshot({ path: `/tmp/addp-field-collapsed-${theme}.png` })
    // A generated field reveals only its own table, leaving unrelated tables folded.
    await page.getByRole('button', { name: 'field_3.8', exact: true }).click()
    await expect(page.locator('.lineage-inspector strong')).toHaveText('field_3.8')
    await expect.poll(async () => (await lineageCanvasText(canvas)).filter(row => /^field_/.test(row.text)).length).toBe(9)
    await page.locator('html').evaluate(element => element.classList.toggle('dark'))
    await expect.poll(async () => (await lineageCanvasText(canvas)).filter(row => /^field_/.test(row.text)).length).toBe(9)
    await page.locator('html').evaluate(element => element.classList.toggle('dark'))
    // A derived field automatically opens its complete upstream chain.
    await page.getByRole('button', { name: 'field_3.1', exact: true }).click()
    await expect(page.locator('.lineage-inspector strong')).toHaveText('field_3.1')
    await expect(page.getByRole('button', { name: '展开字段', exact: true })).toBeDisabled()
    await page.getByRole('button', { name: '全部字段', exact: true }).click()
    await page.getByRole('button', { name: '自动布局', exact: true }).click()
    await expect(page.getByRole('button', { name: '自动布局', exact: true })).toBeEnabled()
    await expect.poll(async () => (await lineageCanvasText(canvas)).filter(row => /^field_/.test(row.text)).length).toBe(32)
    await portsMatch()
    expect(graphRequests).toBe(1)
    expect(errors).toEqual([])
    await page.screenshot({ path: `/tmp/addp-field-interaction-${theme}.png` })
  })
}
