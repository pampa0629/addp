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
        paths.set(this.canvas, records)
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
          fontSize: Number(this.font.match(/([\d.]+)px/)?.[1]) * scale,
          opacity: this.globalAlpha }
        const current = records.filter(row => row.text !== record.text || Math.abs(row.x - record.x) > 0.01 || Math.abs(row.y - record.y) > 0.01)
        // G6 may issue draw calls wholly outside the bitmap after zooming.
        // Those calls produce no pixels and must not survive as painted labels.
        const bounds = this.canvas.getBoundingClientRect()
        if (record.x <= bounds.width && record.x + record.width >= 0 &&
          record.y - record.fontSize <= bounds.height && record.y + record.fontSize >= 0) current.push(record)
        frames.set(this.canvas, current)
      }
      return paint.call(this, text, x, y, ...args)
    }
  })
}

export async function lineageCanvasText(canvas) {
  return (await lineageCanvasSnapshot(canvas)).rows
}

// Deduplicate halo/key strokes; retain the actual field-port endpoints.
export async function lineageCanvasSnapshot(canvas) {
  return canvas.evaluate(async element => {
    // DOM actions and G6 layout completion precede its queued Canvas repaint.
    // Cross the next paint frame before reading labels and paths together.
    const view = element.ownerDocument.defaultView
    await new Promise(resolve => view.requestAnimationFrame(() => view.requestAnimationFrame(resolve)))
    return {
      rows: window.__lineageCanvasText.get(element) || [],
      paths: [...new Map((window.__lineageCanvasPaths.get(element) || [])
        .map(points => [JSON.stringify(points), points])).values()]
    }
  })
}

export async function lineageCanvasPaths(canvas) {
  return (await lineageCanvasSnapshot(canvas)).paths
}

// One lightweight probe for initial rendering, native layout and field focus.
// Keep detailed geometry recording on a separate verification page.
export async function profileLineagePaint(page, viewer, action, click) {
  await viewer.evaluate((element, action) => {
    const view = element.ownerDocument.defaultView
    if (view.__lineageCanvasText) throw new Error('Lineage profiling requires a page without the Canvas geometry observer')
    if (view.__lineagePaintProbe) throw new Error('A lineage paint profile is already active')
    const prototype = view.CanvasRenderingContext2D.prototype
    const paint = prototype.fillText
    const fields = new Set(action.fields || [action.field])
    const probe = { start: null, draws: 0, previousDraws: -1, fieldPaint: null, response: null }
    let finish
    probe.ready = new Promise(resolve => { finish = resolve })
    const frame = () => {
      const readable = action.mode !== 'focus' || (probe.fontSize >= 11 - 1e-6 && Math.abs(probe.y - probe.height / 2) < 3)
      const completed = action.mode !== 'layout' || !element.querySelector(`button[aria-label="${action.buttonLabel}"]`)?.disabled
      if (probe.fieldPaint !== null && probe.draws === probe.previousDraws && readable && completed &&
        (action.mode !== 'initial' || probe.response)) {
        const finished = view.performance.now()
        const timing = { eventToFieldPaintMs: probe.fieldPaint - probe.start,
          eventToStablePaintMs: finished - probe.start,
          fieldFontPx: probe.fontSize, drawCalls: probe.draws }
        if (probe.response) Object.assign(timing, {
          eventToResponseEndMs: probe.response.responseEnd - probe.start,
          responseEndToFieldPaintMs: probe.fieldPaint - probe.response.responseEnd,
          responseEndToStablePaintMs: finished - probe.response.responseEnd
        })
        finish(timing)
      } else {
        probe.previousDraws = probe.draws
        probe.frame = view.requestAnimationFrame(frame)
      }
    }
    const start = event => {
      if (probe.start !== null || !element.contains(event.target)) return
      const button = event.target.closest('button')
      const radio = event.target.closest('.el-radio-button')?.querySelector('input')
      const matches = action.mode === 'initial' ? radio?.value === 'field'
        : action.mode === 'focus' ? button?.closest('.lineage-field-options') && button.textContent.trim() === action.field
          : button?.getAttribute('aria-label') === action.buttonLabel
      if (!matches) return
      probe.start = view.performance.now()
      probe.frame = view.requestAnimationFrame(frame)
    }
    if (action.mode === 'initial') {
      probe.resources = new view.PerformanceObserver(list => {
        for (const entry of list.getEntries()) {
          const url = new URL(entry.name)
          if (probe.start !== null && entry.startTime >= probe.start &&
            url.pathname === '/api/v1/meta/lineage/graph' && url.searchParams.get('granularity') === 'field' &&
            url.searchParams.get('item_id') === String(action.itemID)) probe.response = entry
        }
      })
      probe.resources.observe({ type: 'resource' })
    }
    prototype.fillText = function (text, x, y, ...args) {
      const result = paint.call(this, text, x, y, ...args)
      // G6 can replace the canvas during initial rendering. Scope to the viewer.
      if (probe.start === null || !element.contains(this.canvas) || !this.canvas.parentElement.classList.contains('lineage-canvas')) return result
      probe.draws++
      if (fields.has(String(text))) {
        const matrix = this.getTransform()
        const ratio = this.canvas.width / this.canvas.clientWidth
        const px = (matrix.a * x + matrix.c * y + matrix.e) / ratio
        const py = (matrix.b * x + matrix.d * y + matrix.f) / ratio
        const font = Number(this.font.match(/([\d.]+)px/)?.[1]) * Math.hypot(matrix.a, matrix.b) / ratio
        // Exclude offscreen measureText fallback draws and pre-fit coordinates.
        if (px >= 0 && px < this.canvas.clientWidth && py >= 0 && py < this.canvas.clientHeight &&
          (action.mode !== 'initial' || font >= 11 - 1e-6)) {
          probe.fieldPaint = view.performance.now()
          probe.fontSize = font
          probe.y = py
          probe.height = this.canvas.clientHeight
        }
      }
      return result
    }
    element.ownerDocument.addEventListener('click', start, true)
    probe.timer = view.setTimeout(() => finish({ error: 'Lineage action did not reach a stable visible field paint frame' }), 10_000)
    probe.restore = () => {
      prototype.fillText = paint
      element.ownerDocument.removeEventListener('click', start, true)
      view.cancelAnimationFrame(probe.frame)
      view.clearTimeout(probe.timer)
      probe.resources?.disconnect()
      delete view.__lineagePaintProbe
    }
    view.__lineagePaintProbe = probe
  }, action)
  let profiler
  try {
    // Console and its module iframe use the same loopback site/renderer.
    profiler = await page.context().newCDPSession(page)
    await profiler.send('Profiler.enable')
    await profiler.send('Profiler.setSamplingInterval', { interval: 100 })
    await profiler.send('Profiler.start')
    await click()
    const timing = await viewer.evaluate(element => element.ownerDocument.defaultView.__lineagePaintProbe.ready)
    if (timing.error) throw new Error(timing.error)
    const { profile } = await profiler.send('Profiler.stop')
    return { timing, profile }
  } finally {
    await viewer.evaluate(element => element.ownerDocument.defaultView.__lineagePaintProbe?.restore())
    if (profiler) {
      try { await profiler.send('Profiler.disable') }
      finally { await profiler.detach() }
    }
  }
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
