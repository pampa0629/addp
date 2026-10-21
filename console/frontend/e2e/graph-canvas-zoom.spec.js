import { expect, test } from '@playwright/test'
import { mockGraph } from './graph.fixture.js'

for (const count of [1, 3]) {
  test(`Graph keeps text and strokes readable through wheel zoom (${count} nodes)`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1920, height: 1080 })
    const warnings = []
    page.on('console', message => { if (message.type() === 'warning') warnings.push(message.text()) })
    await page.addInitScript(() => {
      const frames = new WeakMap()
      window.__graphZoomFrames = frames
      window.__graphZoomViolations = []
      const clear = CanvasRenderingContext2D.prototype.clearRect
      CanvasRenderingContext2D.prototype.clearRect = function (...args) {
        if (this.canvas.closest('.graph-canvas')) frames.set(this.canvas, { labels: {}, strokes: [] })
        return clear.apply(this, args)
      }
      const text = CanvasRenderingContext2D.prototype.fillText
      CanvasRenderingContext2D.prototype.fillText = function (value, ...args) {
        const frame = frames.get(this.canvas)
        if (frame) {
          const matrix = this.getTransform()
          const ratio = this.canvas.width / this.canvas.getBoundingClientRect().width
          const zoom = Math.hypot(matrix.a, matrix.b) / ratio
          frame.zoom = zoom
          frame.labels[value] = { zoom, size: Number(this.font.match(/([\d.]+)px/)?.[1]) * zoom,
            x: (matrix.a * args[0] + matrix.c * args[1] + matrix.e) / ratio,
            y: (matrix.b * args[0] + matrix.d * args[1] + matrix.f) / ratio }
        }
        return text.call(this, value, ...args)
      }
      const stroke = CanvasRenderingContext2D.prototype.stroke
      CanvasRenderingContext2D.prototype.stroke = function (...args) {
        const frame = frames.get(this.canvas)
        if (frame) {
          const matrix = this.getTransform()
          const ratio = this.canvas.width / this.canvas.getBoundingClientRect().width
          const zoom = Math.hypot(matrix.a, matrix.b) / ratio
          frame.zoom = zoom
          frame.strokes.push({ width: this.lineWidth * zoom, dash: this.getLineDash().map(length => length * zoom) })
          if (zoom > 1 && this.lineWidth * zoom > 5.1) {
            window.__graphZoomViolations.push({ zoom, width: this.lineWidth * zoom })
          }
        }
        return stroke.apply(this, args)
      }
    })
    const nodes = ['甲', '乙', '丙'].slice(0, count).map((name, index) => ({
      id: String(index), labels: ['Person'], display_name: `缩放节点${name}`
    }))
    const edges = nodes.slice(1).map((node, index) => ({ id: `link-${index}`, source: nodes[index].id,
      target: node.id, type: 'Link', directed: true }))
    const unexpected = await mockGraph(page, { permissions: ['graph.graph.read'], responses: {
      '/api/v1/graph/graphs/7': { id: 7, name: '缩放合成图' },
      '/api/v1/graph/graphs/7/browse-snapshot': {
        schema: { node_shapes: [{ name: 'Person', labels: ['Person'] }], relationship_shapes: [
          { type: 'A-solid', directed: true }, { type: 'Link', directed: true }
        ] },
        stats: { node_count: nodes.length, relationship_count: edges.length }, overview: { nodes, edges }
      }
    } })
    await page.goto('/graph/graphs/7/browse')
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const canvas = frame.locator('.graph-canvas canvas')
    await expect(canvas).toBeVisible()
    await expect(frame.locator('.graph-canvas .el-loading-mask')).toHaveCount(0)
    const painted = () => canvas.evaluate(element => window.__graphZoomFrames.get(element))
    await expect.poll(async () => (await painted())?.labels['缩放节点甲']?.size).toBeGreaterThan(0)
    expect(warnings.filter(value => /ratio out of range|zoom failed|fitview failed/.test(value))).toEqual([])
    expect((await painted()).labels['缩放节点甲'].size).toBeLessThanOrEqual(14.1)

    // Use real wheel events at the node, without a product/G6 debug hook.
    const box = await canvas.boundingBox(), label = (await painted()).labels['缩放节点甲']
    await page.mouse.move(box.x + label.x, box.y + label.y)
    for (let step = 0; step < 5; step++) await page.mouse.wheel(0, -120)
    await expect.poll(async () => (await painted())?.labels['缩放节点甲']?.zoom).toBeGreaterThan(1.4)
    expect((await painted()).labels['缩放节点甲'].size).toBeLessThanOrEqual(14.1)
    for (let step = 0; step < 20; step++) await page.mouse.wheel(0, -120)
    await expect.poll(async () => (await painted())?.labels['缩放节点甲']?.zoom).toBeGreaterThan(4)
    const enlarged = await painted()
    expect(enlarged.labels['缩放节点甲'].zoom).toBeLessThanOrEqual(5.01)
    expect(enlarged.labels['缩放节点甲'].size).toBeLessThanOrEqual(14.1)
    expect(Math.max(...enlarged.strokes.map(stroke => stroke.width))).toBeLessThanOrEqual(5.1)
    await testInfo.attach('graph-enlarged.png', { body: await page.screenshot(), contentType: 'image/png' })
    await testInfo.attach('graph-enlarged.json', { body: JSON.stringify(enlarged), contentType: 'application/json' })
    if (edges.length) {
      const dashes = enlarged.strokes.flatMap(stroke => stroke.dash)
      expect(dashes.length).toBeGreaterThan(0)
      expect(Math.max(...dashes)).toBeLessThanOrEqual(8.1)
      const label = enlarged.labels['缩放节点甲']
      await page.mouse.move(box.x + label.x, box.y + label.y)
      await page.mouse.down()
      await page.mouse.move(box.x + label.x + 20, box.y + label.y + 15, { steps: 5 })
      await page.mouse.up()
      await expect.poll(async () => Math.max(...(await painted()).strokes.map(stroke => stroke.width))).toBeLessThanOrEqual(5.1)
    }
    expect(warnings.filter(value => /ratio out of range|zoom failed|fitview failed/.test(value))).toEqual([])
    for (let step = 0; step < 17; step++) await page.mouse.wheel(0, 120)
    await expect.poll(async () => (await painted())?.zoom).toBeLessThan(1)
    // Distant labels may be hidden by the existing semantic zoom policy.
    expect(Object.values((await painted()).labels).every(label => label.size <= 14.1)).toBe(true)
    expect(await canvas.evaluate(() => window.__graphZoomViolations)).toEqual([])
    expect(unexpected).toEqual([])
  })
}
