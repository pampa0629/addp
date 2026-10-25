import { expect } from '@playwright/test'

// Test-only observation of actual Canvas drawing, without application/G6 objects.
export function installGraphRelationshipPaint() {
  const frames = new WeakMap(), paths = new WeakMap()
  window.__graphRelationshipPaint = frames
  const prototype = CanvasRenderingContext2D.prototype
  for (const name of ['clearRect', 'beginPath', 'moveTo', 'lineTo', 'quadraticCurveTo', 'bezierCurveTo', 'arc', 'ellipse', 'rect', 'stroke']) {
    const original = prototype[name]
    prototype[name] = function (...args) {
      if (this.canvas.closest('.graph-canvas')) {
        if (name === 'clearRect') frames.set(this.canvas, [])
        if (name === 'beginPath') paths.set(this, [])
        const path = paths.get(this)
        const command = { moveTo: 'M', lineTo: 'L', quadraticCurveTo: 'Q', bezierCurveTo: 'C' }[name]
        if (path && command) {
          const matrix = this.getTransform(), ratio = this.canvas.width / this.canvas.getBoundingClientRect().width
          const points = []
          for (let index = 0; index < args.length; index += 2) points.push(
            (matrix.a * args[index] + matrix.c * args[index + 1] + matrix.e) / ratio,
            (matrix.b * args[index] + matrix.d * args[index + 1] + matrix.f) / ratio)
          path.push([command, ...points])
        } else if (path && ['arc', 'ellipse', 'rect'].includes(name)) paths.delete(this)
        if (name === 'stroke' && path?.length >= 2 && path[0][0] === 'M') {
          const frame = frames.get(this.canvas)
          if (frame && frame.length < 1000) frame.push({ path: structuredClone(path), color: this.strokeStyle, alpha: this.globalAlpha })
        }
      }
      return original.apply(this, args)
    }
  }
}

export async function stableRelationshipPaths(element, { nodes, timeout = 10000 }) {
  const canvas = element.querySelector('canvas'), rect = canvas?.getBoundingClientRect()
  if (!canvas || !rect.width || !rect.height || getComputedStyle(canvas).visibility !== 'visible' || getComputedStyle(canvas).display === 'none' || !canvas.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })) throw new Error('Relationship Canvas is not visible')
  let previous = '', stable = 0, strokes = []
  const deadline = performance.now() + timeout
  while (performance.now() < deadline) {
    await new Promise(requestAnimationFrame)
    if (element.querySelector('.el-loading-mask') || !canvas.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })) { previous = ''; stable = 0; continue }
    strokes = window.__graphRelationshipPaint?.get(canvas) || []
    strokes = [...new Map(strokes.filter(stroke => stroke.alpha > 0 && stroke.path.every(segment => segment.slice(1).every(Number.isFinite)))
      .map(stroke => [JSON.stringify(stroke.path) + stroke.color, stroke])).values()]
    const signature = JSON.stringify(strokes)
    stable = strokes.length && signature === previous ? stable + 1 : 0
    previous = signature
    if (stable >= 10) break
  }
  if (stable < 10 || !strokes.length) throw new Error('Relationship drawing did not reach ten stable frames')
  const point = (path, t) => {
    const [x, y] = path[0].slice(1), segment = path[1], q = 1 - t
    if (segment[0] === 'Q') return { x: q*q*x + 2*q*t*segment[1] + t*t*segment[3], y: q*q*y + 2*q*t*segment[2] + t*t*segment[4] }
    if (segment[0] === 'C') return { x: q*q*q*x + 3*q*q*t*segment[1] + 3*q*t*t*segment[3] + t*t*t*segment[5], y: q*q*q*y + 3*q*q*t*segment[2] + 3*q*t*t*segment[4] + t*t*t*segment[6] }
    return { x: q*x + t*segment[1], y: q*y + t*segment[2] }
  }
  const visible = nodes.flatMap(group => group.hits.map((hit, index) => ({ ...hit, box: group.frame.nodes[index].box })))
  // G6 paths terminate at the circle outline. A nearby node's center may be
  // closer to that anchor than its owner; bind against the observed outline.
  const boundaryDistance = (node, x, y) => {
    const rx = (node.box[2] - node.box[0]) / 2, ry = (node.box[3] - node.box[1]) / 2
    return Math.abs(Math.hypot((x - node.x) / rx, (y - node.y) / ry) - 1) * Math.min(rx, ry)
  }
  const nearest = (x, y) => [...visible].sort((a, b) => boundaryDistance(a, x, y) - boundaryDistance(b, x, y))[0]
  const copy = document.createElement('canvas')
  copy.width = canvas.width; copy.height = canvas.height
  const context = copy.getContext('2d', { willReadFrequently: true }), ratio = canvas.width / rect.width
  context.drawImage(canvas, 0, 0)
  const all = []
  for (const [index, stroke] of strokes.entries()) {
    if (stroke.path.length !== 2 || !['L', 'Q', 'C'].includes(stroke.path[1][0])) continue
    const start = nearest(...stroke.path[0].slice(1)), end = nearest(...stroke.path[1].slice(-2))
    if (!start || !end) continue
    for (let sample = 1; sample < 100; sample++) {
      const location = point(stroke.path, sample / 100), { x, y } = location
      if (x <= 2 || y <= 2 || x >= rect.width-2 || y >= rect.height-2 || visible.some(node => x >= node.box[0]-3 && x <= node.box[2]+3 && y >= node.box[1]-3 && y <= node.box[3]+3)) continue
      const pixel = [...context.getImageData(Math.floor(x*ratio), Math.floor(y*ratio), 1, 1).data]
      const background = [...context.getImageData(0, 0, 1, 1).data]
      const rgb = stroke.color.match(/^#([0-9a-f]{6})$/i)?.[1]?.match(/../g).map(value => parseInt(value, 16))
      if (!rgb || pixel.slice(0, 3).some((value, channel) => Math.abs(value-rgb[channel]) > 16)) continue
      if (pixel[3] === 0 || pixel.slice(0, 3).every((value, channel) => Math.abs(value-background[channel]) < 10)) continue
      all.push({ ...stroke, index, t: sample/100, x, y, pixel, source: start.native_id, target: end.native_id })
    }
  }
  for (const candidate of all) {
    candidate.clearance = Math.min(100, ...all.filter(other => other.index !== candidate.index).map(other => Math.hypot(other.x-candidate.x, other.y-candidate.y)))
  }
  all.sort((a, b) => b.clearance-a.clearance)
  return { width: rect.width, height: rect.height, stable_frames: stable, candidates: all, paths: strokes }
}

export async function graphRelationshipHits(page, frame, canvas, edges, stage) {
  const observed = await canvas.evaluate(stableRelationshipPaths, { nodes: stage.groups })
  const expected = new Map(edges.map(edge => [edge.properties.fixture_id, edge]))
  const hits = new Map(), panel = frame.locator('.node-panel'), box = await canvas.locator('canvas').boundingBox()
  let attempts = 0
  const unsuccessful = []
  for (const candidate of observed.candidates) {
    if (hits.size === edges.length) break
    const matching = edges.filter(edge => !hits.has(edge.id) && ((edge.source === candidate.source && edge.target === candidate.target) || (edge.source === candidate.target && edge.target === candidate.source)))
    if (!matching.length) continue
    if (await panel.count()) await panel.locator('.panel-header button').click()
    await page.mouse.click(box.x + candidate.x, box.y + candidate.y)
    attempts++
    await canvas.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    if (!await panel.count()) continue
    const raw = await panel.locator('.prop-row').evaluateAll(rows => Object.fromEntries(rows.map(row => [row.querySelector('.prop-key').textContent.trim(), row.querySelector('.prop-val').textContent.trim()])))
    const edge = expected.get(raw.fixture_id)
    if (!edge) continue // A node can cover a segment; this is not an accepted relationship hit.
    if (!matching.some(item => item.id === edge.id)) { unsuccessful.push({ x: candidate.x, y: candidate.y, source: candidate.source, target: candidate.target, actual: edge.id }); continue }
    const color = stage.relationship_pixels.find(sample => sample.type === edge.type).color
    if (candidate.pixel.slice(0, 3).some((value, channel) => Math.abs(value-color[channel]) > 16)) continue
    expect(raw).toEqual(Object.fromEntries(Object.entries(edge.properties).map(([key, value]) => [key, String(value)])))
    await expect(panel.locator('.panel-name')).toHaveText(edge.type)
    await expect(panel.locator('.panel-title .el-tag')).toHaveText('关系')
    await expect(panel.locator('.panel-actions')).toHaveCount(0)
    hits.set(edge.id, { fixture_id: raw.fixture_id, native_id: edge.id, source: edge.source, target: edge.target, type: edge.type,
      properties: raw, path: candidate.path, path_source: candidate.source, path_target: candidate.target,
      x: candidate.x, y: candidate.y, t: candidate.t, pixel: candidate.pixel })
  }
  if (hits.size !== edges.length) console.log('RELATIONSHIP_MISSES', JSON.stringify({ layout: stage.layout, missing: edges.filter(edge => !hits.has(edge.id)), node_bounds: stage.groups, observed_paths: observed.paths, candidates: observed.candidates.filter(candidate => edges.some(edge => !hits.has(edge.id) && edge.source === candidate.source && edge.target === candidate.target)), unsuccessful }))
  expect([...hits.keys()].sort(), `${stage.layout} relationships not mouse-reachable after ${attempts} clicks`).toEqual(edges.map(edge => edge.id).sort())
  return { layout: stage.layout, width: observed.width, height: observed.height, stable_frames: observed.stable_frames, attempts, hits: [...hits.values()].sort((a, b) => a.native_id.localeCompare(b.native_id)) }
}
