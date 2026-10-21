import { expect, test } from '@playwright/test'
import { mockGraph } from './graph.fixture'
import { stableGraphPixels, graphNodeHits, graphObservationColor, exploreGraphLayouts } from './graph-visual.fixture.js'

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
    await canvas.evaluate(stableGraphPixels, { count: initial.length, fillColor, color: await graphObservationColor(frame, canvas) })
    const response = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/graph/graphs/7/search')
    await frame.locator('.search-input input').fill('合成节点')
    await frame.locator('.search-input input').press('Enter')
    await response
    const color = await graphObservationColor(frame, canvas)
    const stable = await canvas.evaluate(stableGraphPixels, { count: found.length, color, fillColor })
    await canvas.locator('canvas').evaluate(element => { element.style.visibility = 'hidden' })
    await expect(canvas.evaluate(stableGraphPixels, { count: found.length, color, fillColor, timeout: 200 })).rejects.toThrow('visible stable frame')
    await canvas.locator('canvas').evaluate(element => { element.style.visibility = '' })
    const hits = await graphNodeHits(frame, canvas, stable, found)
    await testInfo.attach('stable-search-frame.json', { body: JSON.stringify({ ...stable, hits }, null, 2), contentType: 'application/json' })
    await testInfo.attach('stable-search.png', { body: await page.screenshot(), contentType: 'image/png' })
    const exploration = await exploreGraphLayouts(page, frame, canvas, '/api/v1/graph/graphs/7', found, edges(found), async phase => {
      await testInfo.attach(`stable-${phase}.png`, { body: await page.screenshot(), contentType: 'image/png' })
    })
    await testInfo.attach('graph-exploration-stable-frame.json', { body: JSON.stringify(exploration, null, 2), contentType: 'application/json' })
    // Clearing the query removes highlights; fill pixels alone must never satisfy search evidence.
    await expect(canvas.evaluate(stableGraphPixels, { count: found.length, color, fillColor, timeout: 1000 })).rejects.toThrow('visible stable frame')
    expect(unexpected).toEqual([])
  })
}
