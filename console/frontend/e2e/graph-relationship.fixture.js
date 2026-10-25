import { expect } from '@playwright/test'

// Test-only observation of actual Canvas drawing, without application/G6 objects.
export function installGraphRelationshipPaint() {
  const frames = new WeakMap(), paths = new WeakMap(), circles = new WeakMap(), circlePaths = new WeakMap()
  const clips=new WeakMap(),clipStacks=new WeakMap(),rectPaths=new WeakMap(),events=new WeakMap()
  let sequence=0
  window.__graphRelationshipPaint = frames
  window.__graphNodePaint = circles
  window.__graphNodePaintEvents = events
  const prototype = CanvasRenderingContext2D.prototype
  for (const name of ['clearRect', 'beginPath', 'moveTo', 'lineTo', 'quadraticCurveTo', 'bezierCurveTo', 'arc', 'ellipse', 'rect', 'stroke', 'fill', 'save', 'restore', 'clip']) {
    const original = prototype[name]
    prototype[name] = function (...args) {
      if (this.canvas.closest('.graph-canvas')) {
        const matrix=this.getTransform(),ratio=this.canvas.width/this.canvas.getBoundingClientRect().width
        const rectangle=(x,y,width,height)=>{
          const points=[[x,y],[x+width,y],[x,y+height],[x+width,y+height]].map(([x,y])=>[(matrix.a*x+matrix.c*y+matrix.e)/ratio,(matrix.b*x+matrix.d*y+matrix.f)/ratio])
          return [Math.min(...points.map(p=>p[0])),Math.min(...points.map(p=>p[1])),Math.max(...points.map(p=>p[0])),Math.max(...points.map(p=>p[1]))]
        }
        if (name==='save') { const stack=clipStacks.get(this)||[];stack.push(clips.get(this)||null);clipStacks.set(this,stack) }
        if (name==='restore') clips.set(this,(clipStacks.get(this)||[]).pop()||null)
        if (name==='clearRect') {
          let box=rectangle(...args)
          const active=clips.get(this),rect=this.canvas.getBoundingClientRect()
          if (active==='unsupported') throw new Error('Unsupported Canvas clip in Graph paint observation')
          if (active) box=[Math.max(box[0],active[0]),Math.max(box[1],active[1]),Math.min(box[2],active[2]),Math.min(box[3],active[3])]
          const full=box[0]<=0 && box[1]<=0 && box[2]>=rect.width && box[3]>=rect.height
          // A dirty region leaves all pixels and circle geometry outside it intact.
          circles.set(this.canvas,(circles.get(this.canvas)||[]).filter(n=>n.x+n.rx<box[0]||n.x-n.rx>box[2]||n.y+n.ry<box[1]||n.y-n.ry>box[3]))
          frames.set(this.canvas,[])
          if (full) events.set(this.canvas,[])
          const history=events.get(this.canvas)||[];history.push({kind:'clear',box,sequence:++sequence});events.set(this.canvas,history)
        }
        if (name==='beginPath') { paths.set(this,[]);circlePaths.delete(this);rectPaths.delete(this) }
        if (name==='rect') rectPaths.set(this,rectangle(...args))
        if (name==='clip') {
          const box=rectPaths.get(this),previous=clips.get(this)
          if (!box) clips.set(this,'unsupported')
          else if (previous==='unsupported') clips.set(this,'unsupported')
          else clips.set(this,previous?[Math.max(previous[0],box[0]),Math.max(previous[1],box[1]),Math.min(previous[2],box[2]),Math.min(previous[3],box[3])]:box)
        }
        if (name === 'arc' && Math.abs(args[4] - args[3]) >= Math.PI * 2 - .0001) {
          const matrix = this.getTransform(), ratio = this.canvas.width / this.canvas.getBoundingClientRect().width
          circlePaths.set(this, { x: (matrix.a * args[0] + matrix.c * args[1] + matrix.e) / ratio,
            y: (matrix.b * args[0] + matrix.d * args[1] + matrix.f) / ratio,
            rx: Math.hypot(matrix.a, matrix.b) * args[2] / ratio, ry: Math.hypot(matrix.c, matrix.d) * args[2] / ratio })
        }
        if (name === 'fill' && circlePaths.has(this)) {
          const circle={...circlePaths.get(this),color:this.fillStyle,alpha:this.globalAlpha,clip:clips.get(this)||null,sequence:++sequence}
          const list=(circles.get(this.canvas)||[]).filter(n=>!['x','y','rx','ry','color'].every(key=>n[key]===circle[key]))
          if (list.length<1000) { list.push(circle);circles.set(this.canvas,list) }
          const history=events.get(this.canvas)||[]
          if (history.length>=100000) throw new Error('Graph paint event observation exceeded its bound')
          history.push({kind:'fill',...circle});events.set(this.canvas,history)
        }
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

// Reconstruct only paint operations that actually covered the sampled point.
// A later clipped repaint elsewhere must not change ownership of this overlap.
export function graphOverlapPaint(element, { geometry, owner, nearby }) {
  const canvas=element.querySelector('canvas'),rect=canvas.getBoundingClientRect(),copy=document.createElement('canvas'),context=copy.getContext('2d',{willReadFrequently:true})
  copy.width=canvas.width;copy.height=canvas.height;context.drawImage(canvas,0,0)
  const a=geometry.find(n=>n.native_id===owner),b=geometry.find(n=>n.native_id===nearby),ratio=canvas.width/rect.width
  if (!a || !b || !a.color.every((v,i)=>v===b.color[i])) throw new Error('Overlap requires two same-color native circles')
  const pixels=context.getImageData(0,0,canvas.width,canvas.height).data
  let selected=null,score=-Infinity
  for (let y=Math.floor(Math.max(a.y-a.ry,b.y-b.ry)*ratio);y<=Math.ceil(Math.min(a.y+a.ry,b.y+b.ry)*ratio);y++) {
    for (let x=Math.floor(Math.max(a.x-a.rx,b.x-b.rx)*ratio);x<=Math.ceil(Math.min(a.x+a.rx,b.x+b.rx)*ratio);x++) {
      const point={x:(x+.5)/ratio,y:(y+.5)/ratio}
      const clearance=Math.min(...[a,b].map(n=>(1-Math.hypot((point.x-n.x)/n.rx,(point.y-n.y)/n.ry))*Math.min(n.rx,n.ry)-3))
      if (clearance<=0 || x<0 || y<0 || x>=canvas.width || y>=canvas.height) continue
      const pixel=[...pixels.slice((y*canvas.width+x)*4,(y*canvas.width+x)*4+4)]
      if (pixel[3]<=200 || !a.color.every((v,i)=>Math.abs(v-pixel[i])<=12)) continue
      if (clearance>score) {score=clearance;selected={point,pixel}}
    }
  }
  if (!selected) throw new Error('No actual same-color fill pixel in circle intersection')
  const {point}=selected,contains=box=>box[0]<=point.x && point.x<box[2] && box[1]<=point.y && point.y<box[3]
  const history=window.__graphNodePaintEvents?.get(canvas)||[]
  const lastClear=history.findLastIndex(event=>event.kind==='clear' && contains(event.box))
  if (lastClear<0) throw new Error('Overlap paint has no observed clearing origin')
  const probe=document.createElement('canvas').getContext('2d',{willReadFrequently:true})
  const fills=[]
  for (const event of history.slice(lastClear+1)) {
    if (event.kind!=='fill') continue
    if (event.clip==='unsupported') throw new Error('Unsupported Graph paint clip')
    if (event.clip && !contains(event.clip)) continue
    if (Math.hypot((point.x-event.x)/event.rx,(point.y-event.y)/event.ry)>=1) continue
    probe.clearRect(0,0,1,1);probe.fillStyle=event.color;probe.fillRect(0,0,1,1)
    const color=[...probe.getImageData(0,0,1,1).data].slice(0,3)
    const matches=geometry.filter(n=>['x','y','rx','ry'].every(k=>n[k]===event[k]) && n.color.every((v,i)=>v===color[i]))
    if (matches.length!==1 || event.alpha!==1) throw new Error('Overlap paint cannot bind one current native circle')
    fills.push({native_id:matches[0].native_id,sequence:event.sequence,x:event.x,y:event.y,rx:event.rx,ry:event.ry,color,alpha:event.alpha,clip:event.clip})
  }
  if (!fills.length || ![owner,nearby].includes(fills.at(-1).native_id)) throw new Error('Overlap top paint is not either tested circle')
  return {...selected,cleared_at:history[lastClear].sequence,fills}
}

export async function stableRelationshipPaths(element, { nodes, timeout = 10000 }) {
  const canvas = element.querySelector('canvas'), rect = canvas?.getBoundingClientRect()
  if (!canvas || !rect.width || !rect.height || getComputedStyle(canvas).visibility !== 'visible' || getComputedStyle(canvas).display === 'none' || !canvas.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })) throw new Error('Relationship Canvas is not visible')
  let previous = '', stable = 0, strokes = [], paintedNodes = []
  const deadline = performance.now() + timeout
  while (performance.now() < deadline) {
    await new Promise(requestAnimationFrame)
    if (element.querySelector('.el-loading-mask') || !canvas.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })) { previous = ''; stable = 0; continue }
    strokes = window.__graphRelationshipPaint?.get(canvas) || []
    strokes = [...new Map(strokes.filter(stroke => stroke.alpha > 0 && stroke.path.every(segment => segment.slice(1).every(Number.isFinite)))
      .map(stroke => [JSON.stringify(stroke.path) + stroke.color, stroke])).values()]
    paintedNodes = [...new Map((window.__graphNodePaint?.get(canvas) || []).filter(node => node.alpha > 0).map(node => [JSON.stringify(node), node])).values()]
    const signature = JSON.stringify([strokes, paintedNodes])
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
  const visible = nodes.flatMap(group => group.hits.map((hit, index) => {
    const color = group.frame.fillColor
    const matches = paintedNodes.filter(node => {
      const rgb = node.color.match(/^#([0-9a-f]{6})$/i)?.[1]?.match(/../g).map(value => parseInt(value, 16))
      const box = group.frame.nodes[index].box
      return rgb && color && rgb.every((value, channel) => value === color[channel]) && Math.hypot((node.x - hit.x) / node.rx, (node.y - hit.y) / node.ry) < 1 &&
        node.x - node.rx <= box[0] + 1 && box[2] - 1 <= node.x + node.rx && node.y - node.ry <= box[1] + 1 && box[3] - 1 <= node.y + node.ry
    })
    if (matches.length !== 1) throw new Error('Visible node has no unique painted circle geometry')
    return { ...matches[0], color, native_id: hit.native_id, box: group.frame.nodes[index].box }
  }))
  // G6 paths terminate at the circle outline. A nearby node's center may be
  // closer to that anchor than its owner; bind against the observed outline.
  const boundaryDistance = (node, x, y) => {
    const { rx, ry } = node
    return Math.abs(Math.hypot((x - node.x) / rx, (y - node.y) / ry) - 1) * Math.min(rx, ry)
  }
  const bind = (x,y,tangent,leaving) => {
    const matches=visible.filter(node => boundaryDistance(node,x,y)<=2 &&
      (((x-node.x)*tangent[0]+(y-node.y)*tangent[1])*(leaving?1:-1))>0)
    return matches.length===1?matches[0]:null
  }
  const copy = document.createElement('canvas')
  copy.width = canvas.width; copy.height = canvas.height
  const context = copy.getContext('2d', { willReadFrequently: true }), ratio = canvas.width / rect.width
  context.drawImage(canvas, 0, 0)
  const all = []
  for (const [index, stroke] of strokes.entries()) {
    if (stroke.path.length !== 2 || !['L', 'Q', 'C'].includes(stroke.path[1][0])) continue
    const head=stroke.path[0].slice(1),tail=stroke.path[1].slice(-2),first=stroke.path[1].slice(1,3),last=stroke.path[1].length===3?head:stroke.path[1].slice(-4,-2)
    const start=bind(...head,[first[0]-head[0],first[1]-head[1]],true),end=bind(...tail,[tail[0]-last[0],tail[1]-last[1]],false)
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
  // Exact nearest point on another painted path, capped at the same 100px.
  // Spatial pruning avoids comparing every pair in a dense graph.
  const tree = (points, axis = 'x') => {
    if (!points.length) return null
    points.sort((a, b) => a[axis] - b[axis])
    const middle = Math.floor(points.length / 2), next = axis === 'x' ? 'y' : 'x'
    return { point: points[middle], axis, left: tree(points.slice(0, middle), next), right: tree(points.slice(middle + 1), next) }
  }
  const nearest = (node, candidate, distance = 100) => {
    if (!node) return distance
    if (node.point.index !== candidate.index) distance = Math.min(distance, Math.hypot(node.point.x-candidate.x, node.point.y-candidate.y))
    const delta = candidate[node.axis] - node.point[node.axis]
    distance = nearest(delta <= 0 ? node.left : node.right, candidate, distance)
    if (Math.abs(delta) < distance) distance = nearest(delta <= 0 ? node.right : node.left, candidate, distance)
    return distance
  }
  const spatial = tree([...all])
  for (const candidate of all) candidate.clearance = nearest(spatial, candidate)
  all.sort((a, b) => b.clearance-a.clearance)
  return { width: rect.width, height: rect.height, stable_frames: stable, candidates: all, paths: strokes, paintedNodes, node_geometry: visible.map(({ native_id, x, y, rx, ry, color, alpha }) => ({ native_id, x, y, rx, ry, color, alpha })) }
}

export async function graphRelationshipHits(page, frame, canvas, edges, stage) {
  const observed = await canvas.evaluate(stableRelationshipPaths, { nodes: stage.groups })
  const expected = new Map(edges.map(edge => [edge.properties.fixture_id, edge]))
  const hits = new Map(), panel = frame.locator('.node-panel'), box = await canvas.locator('canvas').boundingBox()
  let attempts = 0
  const unsuccessful = []
  // Try each painted curve's clearest point before retrying other points on
  // that curve. Keep every candidate for intersections and parallel edges.
  const first = [], remaining = [], seen = new Set()
  for (const candidate of observed.candidates) {
    if (seen.has(candidate.index)) remaining.push(candidate)
    else { seen.add(candidate.index); first.push(candidate) }
  }
  for (const candidate of [...first, ...remaining]) {
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
  if (hits.size !== edges.length) console.log('RELATIONSHIP_MISSES', JSON.stringify({ layout: stage.layout, missing: edges.filter(edge => !hits.has(edge.id)), node_bounds: stage.groups, observed_paths: observed.paths, painted_nodes: observed.paintedNodes, candidates: observed.candidates.filter(candidate => edges.some(edge => !hits.has(edge.id) && edge.source === candidate.source && edge.target === candidate.target)), unsuccessful }))
  expect([...hits.keys()].sort(), `${stage.layout} relationships not mouse-reachable after ${attempts} clicks`).toEqual(edges.map(edge => edge.id).sort())
  return { layout: stage.layout, width: observed.width, height: observed.height, stable_frames: observed.stable_frames, node_geometry: observed.node_geometry, attempts, hits: [...hits.values()].sort((a, b) => a.native_id.localeCompare(b.native_id)) }
}
