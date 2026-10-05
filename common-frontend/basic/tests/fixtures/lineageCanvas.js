import { FIELD_CARD_WIDTH } from '../../../graph/src/lineageFields.js'

// Observe the text actually painted by G6, including device-pixel scaling.
export async function observeLineageCanvas(page) {
  await page.addInitScript(() => {
    const paths = new WeakMap()
    const activePaths = new WeakMap()
    window.__lineageCanvasPaths = paths
    const frames = new WeakMap()
    window.__lineageCanvasText = frames
    // Assigning a Canvas dimension clears its bitmap without calling clearRect.
    // Fullscreen/ResizeObserver uses this path; discard all previous observations.
    for (const dimension of ['width', 'height']) {
      const descriptor = Object.getOwnPropertyDescriptor(HTMLCanvasElement.prototype, dimension)
      Object.defineProperty(HTMLCanvasElement.prototype, dimension, {
        ...descriptor,
        set(value) {
          frames.delete(this)
          paths.delete(this)
          descriptor.set.call(this, value)
        }
      })
    }
    const clear = CanvasRenderingContext2D.prototype.clearRect
    CanvasRenderingContext2D.prototype.clearRect = function (x, y, width, height) {
      // G6 can repaint only a dirty rectangle; retain unaffected painted shapes.
      const matrix = this.getTransform()
      const ratio = this.canvas.width / (this.canvas.getBoundingClientRect().width || this.canvas.width)
      const corners = [[x, y], [x + width, y], [x, y + height], [x + width, y + height]]
        .map(([x, y]) => ({ x: (matrix.a * x + matrix.c * y + matrix.e) / ratio,
          y: (matrix.b * x + matrix.d * y + matrix.f) / ratio }))
      const left = Math.min(...corners.map(point => point.x)), right = Math.max(...corners.map(point => point.x))
      const top = Math.min(...corners.map(point => point.y)), bottom = Math.max(...corners.map(point => point.y))
      const outside = (minX, minY, maxX, maxY) => maxX < left || minX > right || maxY < top || minY > bottom
      frames.set(this.canvas, (frames.get(this.canvas) || []).filter(row =>
        outside(row.x, row.y - row.fontSize, row.x + row.width, row.y + row.fontSize)))
      paths.set(this.canvas, (paths.get(this.canvas) || []).filter(points =>
        outside(Math.min(...points.map(point => point.x)), Math.min(...points.map(point => point.y)),
          Math.max(...points.map(point => point.x)), Math.max(...points.map(point => point.y)))))
      return clear.call(this, x, y, width, height)
    }
    const begin = CanvasRenderingContext2D.prototype.beginPath
    CanvasRenderingContext2D.prototype.beginPath = function (...args) {
      activePaths.set(this, [])
      return begin.apply(this, args)
    }
    for (const method of ['moveTo', 'lineTo', 'quadraticCurveTo', 'bezierCurveTo']) {
      const original = CanvasRenderingContext2D.prototype[method]
      CanvasRenderingContext2D.prototype[method] = function (...args) {
        if (this.canvas.closest('.lineage-canvas')) {
          const matrix = this.getTransform()
          const ratio = this.canvas.width / this.canvas.getBoundingClientRect().width
          const x = args.at(-2), y = args.at(-1)
          const points = activePaths.get(this) || []
          points.push({ x: (matrix.a * x + matrix.c * y + matrix.e) / ratio,
            y: (matrix.b * x + matrix.d * y + matrix.f) / ratio, command: method })
          activePaths.set(this, points)
        }
        return original.apply(this, args)
      }
    }
    const stroke = CanvasRenderingContext2D.prototype.stroke
    CanvasRenderingContext2D.prototype.stroke = function (...args) {
      const points = activePaths.get(this) || []
      if (points.length > 1 && this.canvas.closest('.lineage-canvas')) {
        const records = paths.get(this.canvas) || []
        records.push(points.slice())
        paths.set(this.canvas, records.slice(-1000))
      }
      return stroke.apply(this, args)
    }
    const paint = CanvasRenderingContext2D.prototype.fillText
    CanvasRenderingContext2D.prototype.fillText = function (text, x, y, ...args) {
      if (this.canvas.closest('.lineage-canvas')) {
        const matrix = this.getTransform()
        const ratio = this.canvas.width / this.canvas.getBoundingClientRect().width
        const scale = Math.hypot(matrix.a, matrix.b) / ratio
        const records = frames.get(this.canvas) || []
        const record = { text: String(text), x: (matrix.a * x + matrix.c * y + matrix.e) / ratio,
          y: (matrix.b * x + matrix.d * y + matrix.f) / ratio,
          width: this.measureText(text).width * scale,
          fontSize: Number(this.font.match(/([\d.]+)px/)?.[1]) * scale }
        const current = records.filter(row => row.text !== record.text || Math.abs(row.x - record.x) > 0.01 || Math.abs(row.y - record.y) > 0.01)
        // G6 may issue draw calls wholly outside the bitmap after zooming.
        // Those calls produce no pixels and must not survive as painted labels.
        const bounds = this.canvas.getBoundingClientRect()
        if (record.x <= bounds.width && record.x + record.width >= 0 &&
          record.y - record.fontSize <= bounds.height && record.y + record.fontSize >= 0) current.push(record)
        frames.set(this.canvas, current.slice(-1000))
      }
      return paint.call(this, text, x, y, ...args)
    }
  })
}

export async function lineageCanvasText(canvas) {
  return canvas.evaluate(element => window.__lineageCanvasText.get(element) || [])
}

// Deduplicate halo/key strokes; retain the actual field-port endpoints.
export async function lineageCanvasSnapshot(canvas) {
  return canvas.evaluate(element => ({
    rows: window.__lineageCanvasText.get(element) || [],
    paths: [...new Map((window.__lineageCanvasPaths.get(element) || [])
      .map(points => [JSON.stringify(points), points])).values()]
  }))
}

export async function lineageCanvasPaths(canvas) {
  return (await lineageCanvasSnapshot(canvas)).paths
}

export async function dragLineageTable(page, canvas, title, dx, dy) {
  const header = (await lineageCanvasText(canvas)).find(row => row.text === title)
  if (!header) throw new Error(`Missing lineage table header: ${title}`)
  // Keep the pointer inside the field-card header even in a large fit-view.
  const zoom = header.fontSize / 15
  const box = await canvas.boundingBox()
  const x = box.x + header.x + 10 * zoom, y = box.y + header.y - 3 * zoom
  await page.mouse.move(x, y)
  await page.mouse.down()
  await page.mouse.move(x + dx, y + dy, { steps: 12 })
  await page.mouse.up()
}

export async function toggleLineageTableFields(page, canvas, title) {
  const header = (await lineageCanvasText(canvas)).find(row => row.text === title)
  if (!header) throw new Error(`Missing lineage table header: ${title}`)
  const zoom = header.fontSize / 15
  const box = await canvas.boundingBox()
  await page.mouse.click(box.x + header.x + (FIELD_CARD_WIDTH - 32) * zoom, box.y + header.y - zoom)
}
