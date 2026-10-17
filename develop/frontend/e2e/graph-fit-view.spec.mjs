import { expect, test } from '@playwright/test'

// Record the scale of text actually painted, including HiDPI canvas transforms.
async function observeGraphText(page) {
  await page.addInitScript(() => {
    window.graphTextFrames = new WeakMap()
    const clear = CanvasRenderingContext2D.prototype.clearRect
    CanvasRenderingContext2D.prototype.clearRect = function (...args) {
      window.graphTextFrames.delete(this.canvas)
      return clear.apply(this, args)
    }
    const paint = CanvasRenderingContext2D.prototype.fillText
    CanvasRenderingContext2D.prototype.fillText = function (text, x, y, ...args) {
      if (this.canvas.closest('.graph-container, .graph-canvas')) {
        const matrix = this.getTransform()
        const ratio = this.canvas.width / this.canvas.getBoundingClientRect().width
        const records = window.graphTextFrames.get(this.canvas) || new Map()
        records.set(String(text), {
          zoom: Math.hypot(matrix.a, matrix.b) / ratio,
          x: (matrix.a * x + matrix.c * y + matrix.e) / ratio,
          y: (matrix.b * x + matrix.d * y + matrix.f) / ratio
        })
        window.graphTextFrames.set(this.canvas, records)
      }
      return paint.call(this, text, x, y, ...args)
    }
  })
}

async function renderedText(canvas) {
  return canvas.evaluate(async element => {
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))
    return [...(window.graphTextFrames.get(element) || new Map()).values()]
  })
}

for (const view of ['schema', 'result']) {
  for (const count of [1, 2, 80]) {
    test(`${view} fits ${count} nodes without automatic enlargement and still allows manual zoom`, async ({ page }) => {
      await observeGraphText(page)
      await page.goto(`/e2e/fixtures/graph-schema-theme.html?view=${view}&count=${count}`)
      const canvas = page.locator(view === 'schema' ? '.graph-container canvas' : '.graph-canvas canvas')
      await expect(canvas).toBeVisible()
      const fits = async () => {
        const rows = await renderedText(canvas)
        expect(rows.length).toBeGreaterThan(0)
        expect(Math.max(...rows.map(row => row.zoom))).toBeLessThanOrEqual(1.001)
        expect(rows.every(row => row.zoom > 0)).toBe(true)
        const box = await canvas.boundingBox()
        expect(rows.every(row => row.x >= 0 && row.x <= box.width && row.y >= 0 && row.y <= box.height)).toBe(true)
        if (count === 80) expect(rows[0].zoom).toBeLessThan(1)
      }
      await expect(fits).toPass({ timeout: 5_000 })
      if (count === 1) {
        const rows = await renderedText(canvas)
        const box = await canvas.boundingBox()
        expect(Math.abs(rows[0].x - box.width / 2)).toBeLessThan(2)
        const bounds = await canvas.boundingBox()
        await page.mouse.move(bounds.x + bounds.width / 2, bounds.y + bounds.height / 2)
        await page.mouse.wheel(0, -600)
        await expect.poll(async () => Math.max(...(await renderedText(canvas)).map(row => row.zoom))).toBeGreaterThan(1.01)
        if (view === 'result') {
          await page.getByRole('button', { name: '适应窗口', exact: true }).click()
          await expect(fits).toPass({ timeout: 5_000 })
        }
      }
      await page.setViewportSize({ width: 620, height: 600 })
      await expect(fits).toPass({ timeout: 5_000 })
    })
  }
}
