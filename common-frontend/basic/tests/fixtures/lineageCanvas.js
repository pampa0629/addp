// Observe the text actually painted by G6, including device-pixel scaling.
export async function observeLineageCanvas(page) {
  await page.addInitScript(() => {
    const frames = new WeakMap()
    window.__lineageCanvasText = frames
    const clear = CanvasRenderingContext2D.prototype.clearRect
    CanvasRenderingContext2D.prototype.clearRect = function (...args) {
      frames.set(this.canvas, [])
      return clear.apply(this, args)
    }
    const paint = CanvasRenderingContext2D.prototype.fillText
    CanvasRenderingContext2D.prototype.fillText = function (text, x, y, ...args) {
      if (this.canvas.closest('.lineage-canvas')) {
        const matrix = this.getTransform()
        const ratio = this.canvas.width / this.canvas.getBoundingClientRect().width
        const scale = Math.hypot(matrix.a, matrix.b) / ratio
        const records = frames.get(this.canvas) || []
        records.push({ text: String(text), x: (matrix.a * x + matrix.c * y + matrix.e) / ratio,
          y: (matrix.b * x + matrix.d * y + matrix.f) / ratio,
          width: this.measureText(text).width * scale,
          fontSize: Number(this.font.match(/([\d.]+)px/)?.[1]) * scale })
        frames.set(this.canvas, records.slice(-1000))
      }
      return paint.call(this, text, x, y, ...args)
    }
  })
}

export async function lineageCanvasText(canvas) {
  return canvas.evaluate(element => window.__lineageCanvasText.get(element) || [])
}
