import { expect, test } from '@playwright/test'
import { mockGraph } from './graph.fixture'
import { stableGraphFrame } from './graph-visual.fixture.js'

for (const replace of [false, true]) {
  test(`Graph search settles to visible clickable nodes (${replace ? 'new' : 'retained'} identities)`, async ({ page }, testInfo) => {
    const nodes = ids => ids.map(id => ({ id, labels: ['Person'], display_name: `合成节点${id}`, properties: { name: `合成节点${id}` } }))
    const initial = nodes(['a', 'b', 'c'])
    const found = nodes(replace ? ['d', 'e', 'f'] : ['a', 'b', 'c'])
    const edges = values => values.slice(1).map((node, index) => ({ id: `edge-${node.id}`, source: values[index].id, target: node.id, type: 'Link', directed: true }))
    const unexpected = await mockGraph(page, { permissions: ['graph.graph.read'], responses: {
      '/api/v1/graph/graphs/7': { id: 7, name: '搜索稳定帧合成图' },
      '/api/v1/graph/graphs/7/browse-snapshot': {
        schema: { node_shapes: [{ name: 'Person', labels: ['Person'] }], relationship_shapes: [{ type: 'Link', directed: true }] },
        stats: { node_count: 3, relationship_count: 2 }, overview: { nodes: initial, edges: edges(initial) }
      }
    } })
    await page.route('**/api/v1/graph/graphs/7/search', route => {
      expect(route.request().method()).toBe('POST')
      return route.fulfill({ json: { nodes: found, edges: edges(found) } })
    })
    await page.goto('/graph/graphs/7/browse')
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const canvas = frame.locator('.graph-canvas')
    await expect(canvas.locator('canvas')).toBeVisible()
    await expect(canvas.locator('.el-loading-mask')).toHaveCount(0)
    const response = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/graph/graphs/7/search')
    await frame.locator('.search-input input').fill('合成节点')
    await frame.locator('.search-input input').press('Enter')
    await response
    const stable = await canvas.evaluate(stableGraphFrame, found.map(node => node.id))
    await testInfo.attach('stable-search-frame.json', { body: JSON.stringify(stable, null, 2), contentType: 'application/json' })
    for (const node of stable.nodes) {
      await canvas.locator('canvas').click({ position: { x: node.x, y: node.y } })
      await expect(frame.locator('.node-panel .panel-name')).toHaveText(`合成节点${node.id}`)
    }
    await testInfo.attach('stable-search.png', { body: await page.screenshot(), contentType: 'image/png' })
    expect(unexpected).toEqual([])
  })
}
