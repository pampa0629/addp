import { installGraphRelationshipPaint, stableRelationshipPaths } from './graph-relationship.fixture.js'
import { expect, test } from '@playwright/test'
import { mockGraph } from './graph.fixture'
import { stableGraphPixels, graphNodeHits, graphObservationColor, exploreGraphLayouts, denseGraphStages } from './graph-visual.fixture.js'

test('Graph pixel observer excludes disconnected same-color relationship strokes from node bounds', async ({ page }) => {
  await page.setContent('<div id="graph"><canvas width="280" height="150"></canvas></div>')
  const canvas = page.locator('#graph')
  const color = 'rgb(80, 120, 180)'
  await canvas.locator('canvas').evaluate((element, color) => {
    const context = element.getContext('2d')
    context.fillStyle = context.strokeStyle = color
    for (const x of [60, 220]) {
      context.beginPath(); context.arc(x, 75, 16, 0, 2 * Math.PI); context.fill()
    }
    context.lineWidth = 2
    context.beginPath(); context.moveTo(82, 75); context.lineTo(198, 75); context.stroke()
  }, color)
  const [frame] = await canvas.evaluate(stableGraphPixels, { groups: [{ count: 2, color }], timeout: 1500 })
  expect(frame.stableFrames).toBeGreaterThanOrEqual(10)
  expect(frame.nodes).toHaveLength(2)
  for (const [index, node] of frame.nodes.entries()) {
    expect(node.x).toBeCloseTo([59.5, 219.5][index], 0)
    expect(node.y).toBeCloseTo(74.5, 0)
    expect(node.box).toEqual(node.fillBox)
    expect(node.box[2] - node.box[0]).toBeLessThanOrEqual(32)
  }
})

for (const replace of [false, true]) {
  test(`Graph search settles to visible clickable nodes (${replace ? 'new' : 'retained'} identities)`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    const nodes = ids => ids.map((id, index) => ({ id, labels: ['Person'], display_name: ['Fixture one', 'Fixture two', 'Fixture three'][index], properties: { id, name: ['Fixture one', 'Fixture two', 'Fixture three'][index] } }))
    const initial = nodes(['a', 'b', 'c'])
    const found = nodes(replace ? ['d', 'e', 'f'] : ['a', 'b', 'c'])
    const edges = values => values.slice(1).map((node, index) => ({ id: `edge-${node.id}`, source: values[index].id, target: node.id, type: 'Link', directed: true }))
    const unexpected = await mockGraph(page, { permissions: ['graph.graph.read'], responses: {
      '/api/v1/graph/graphs/7': { id: 7, name: '搜索稳定帧合成图' },
      '/api/v1/graph/graphs/7/browse-snapshot': {
        schema: { node_shapes: [{ name: 'Person', labels: ['Person'] }], relationship_shapes: [{ type: 'Link', directed: true }] },
        stats: { node_count: 3, relationship_count: 2 }, overview: { nodes: [{ id: 'aggregate-person', kind: 'aggregate', labels: ['Person'], display_name: 'Person', member_count: 3 }], edges: [] }
      }
    } })
    let searches = 0
    await page.route('**/api/v1/graph/graphs/7/search', route => {
      expect(route.request().method()).toBe('POST')
      return route.fulfill({ json: { nodes: ++searches === 1 ? initial : found, edges: [] } })
    })
    await page.route('**/api/v1/graph/graphs/7/expand', route => {
      expect(route.request().method()).toBe('POST')
      const count = route.request().postDataJSON().target.id === found[0].id ? 2 : 3
      return route.fulfill({ json: { nodes: found.slice(0, count), edges: edges(found).slice(0, count - 1) } })
    })
    await page.goto('/graph/graphs/7/browse')
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const canvas = frame.locator('.graph-canvas')
    await expect(canvas.locator('canvas')).toBeVisible()
    await expect(canvas.locator('.el-loading-mask')).toHaveCount(0)
    await page.getByRole('button', { name: /^当前主题:/ }).click()
    await page.getByRole('menuitem', { name: '紫色模式', exact: true }).click()
    await expect(frame.locator('html')).toHaveClass(/purple/)
    // Start from actual entity search results, independently of the aggregate overview.
    const fillColor = await graphObservationColor(frame, canvas, true)
    const first = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/graph/graphs/7/search')
    await frame.locator('.search-input input').fill('初始合成节点')
    await frame.locator('.search-input input').press('Enter')
    await first
    await canvas.evaluate(stableGraphPixels, { groups: [{ count: initial.length, fillColor, color: await graphObservationColor(frame, canvas) }] })
    const response = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/graph/graphs/7/search')
    await frame.locator('.search-input input').fill('合成节点')
    await frame.locator('.search-input input').press('Enter')
    await response
    const color = await graphObservationColor(frame, canvas)
    const [stable] = await canvas.evaluate(stableGraphPixels, { groups: [{ count: found.length, color, fillColor }] })
    await canvas.locator('canvas').evaluate(element => { element.style.visibility = 'hidden' })
    await expect(canvas.evaluate(stableGraphPixels, { groups: [{ count: found.length, color, fillColor }], timeout: 200 })).rejects.toThrow('visible stable frame')
    await canvas.locator('canvas').evaluate(element => { element.style.visibility = '' })
    const hits = await graphNodeHits(frame, canvas, stable, found)
    await testInfo.attach('stable-search-frame.json', { body: JSON.stringify({ ...stable, hits }, null, 2), contentType: 'application/json' })
    await testInfo.attach('stable-search.png', { body: await page.screenshot(), contentType: 'image/png' })
    const exploration = await exploreGraphLayouts(page, frame, canvas, '/api/v1/graph/graphs/7', found, edges(found), async phase => {
      await testInfo.attach(`stable-${phase}.png`, { body: await page.screenshot(), contentType: 'image/png' })
    })
    await testInfo.attach('graph-exploration-stable-frame.json', { body: JSON.stringify(exploration, null, 2), contentType: 'application/json' })
    // Clearing the query removes highlights; fill pixels alone must never satisfy search evidence.
    await expect(canvas.evaluate(stableGraphPixels, { groups: [{ count: found.length, color, fillColor }], timeout: 1000 })).rejects.toThrow('visible stable frame')
    expect(unexpected).toEqual([])
  })
}


test('Graph multi-label dense search, filter and four layouts preserve every native identity', async ({ page }, testInfo) => {
  test.setTimeout(240_000)
  await page.addInitScript(installGraphRelationshipPaint)
  const diagnostics = []
  page.on('pageerror', error => diagnostics.push(error.message))
  page.on('console', message => { if (['warning', 'error'].includes(message.type())) diagnostics.push(message.text()) })
  await page.setViewportSize({ width: 1440, height: 900 })
  const labelSets = [['Person'], ['Employee', 'Person'], ['Organization'], ['Organization', 'Partner']]
  const nodes = Array.from({ length: 12 }, (_, index) => {
    const key = 'n' + String(index + 1).padStart(2, '0')
    return { id: 'native-' + key, labels: labelSets[Math.floor(index / 3)], display_name: 'Dense ' + key.slice(1), properties: { id: key, name: 'Dense ' + key.slice(1) } }
  })
  const edges = nodes.flatMap((source, index) => nodes.slice(index + 1).map(target => ({ source: source.id, target: target.id, type: 'Link' })))
  edges.push({ source: nodes[1].id, target: nodes[0].id, type: 'Link' }, { source: nodes[0].id, target: nodes[1].id, type: 'Parallel' }, { source: nodes[11].id, target: nodes[11].id, type: 'Link' })
  edges.push({ source: nodes[0].id, target: nodes[11].id, type: 'Parallel' }, { source: nodes[3].id, target: nodes[8].id, type: 'Parallel' }, { source: nodes[4].id, target: nodes[7].id, type: 'Parallel' })
  edges.forEach((edge, index) => Object.assign(edge, { id: 'relationship-' + index, directed: true, properties: { fixture_id: 'r' + String(index + 1).padStart(2, '0'), source_key: edge.source.slice(7), target_key: edge.target.slice(7), ordinal: index, score: (index + 1) / 4, active: index % 2 === 0 } }))
  const shapes = labelSets.map(labels => ({ name: [...labels].sort().join('+'), labels })).sort((a, b) => a.name.localeCompare(b.name))
  const unexpected = await mockGraph(page, { permissions: ['graph.graph.read'], responses: {
    '/api/v1/graph/graphs/7': { id: 7, name: '多标签密集关系隔离图' },
    '/api/v1/graph/graphs/7/browse-snapshot': { schema: { node_shapes: shapes, relationship_shapes: [{ type: 'Link', directed: true }, { type: 'Parallel', directed: true }] },
      stats: { node_count: 12, relationship_count: 72 }, overview: { nodes: labelSets.map((labels, index) => ({ id: 'aggregate-' + index, labels, kind: 'aggregate', display_name: labels.join('+'), member_count: 3 })), edges: [] } }
  } })
  await page.route('**/api/v1/graph/graphs/7/search', route => route.fulfill({ json: { nodes, edges: [] } }))
  await page.route('**/api/v1/graph/graphs/7/expand', route => route.fulfill({ json: { nodes, edges } }))
  await page.goto('/graph/graphs/7/browse')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  // Native KG evidence contains identity/labels/properties, not the browser display DTO.
  const nativeNodes = nodes.map(({ id, labels, properties }) => ({ id, labels, properties }))
  const result = await denseGraphStages(page, frame, frame.locator('.graph-canvas'), { graph_id: 7, nodes: nativeNodes, edges, shapes }, async phase => {
    await testInfo.attach(phase + '.png', { body: await page.screenshot(), contentType: 'image/png' })
  })
  await testInfo.attach('graph-dense-stable-frame.json', { body: JSON.stringify(result, null, 2), contentType: 'application/json' })
  const nativeCanvas = frame.locator('.graph-canvas canvas')
  await nativeCanvas.evaluate(element => { element.style.opacity = '0' })
  await expect(frame.locator('.graph-canvas').evaluate(stableRelationshipPaths, { nodes: result.stages.at(-1).groups, timeout: 1000 })).rejects.toThrow('not visible')
  await nativeCanvas.evaluate(element => { element.style.opacity = '' })
  expect(unexpected).toEqual([])
  expect(diagnostics).toEqual([])
})
