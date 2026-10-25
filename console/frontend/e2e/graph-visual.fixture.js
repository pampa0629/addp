import { graphRelationshipHits, stableRelationshipPaths, graphOverlapPaint } from './graph-relationship.fixture.js'
import { expect } from '@playwright/test'

// Shared browser observations for synthetic T3 and native Hosted production builds.
// Uses only Canvas pixels and DOM; never Vue/G6 debug objects or product test hooks.
export async function stableGraphPixels(element, { groups, timeout = 10_000 }) {
  const canvas = element.querySelector('canvas')
  if (!canvas || !Array.isArray(groups) || !groups.length || groups.length > 16 || groups.some(group => !Number.isInteger(group.count) || group.count <= 0)) throw new Error('Invalid search canvas/count')
  const copy = document.createElement('canvas')
  const context = copy.getContext('2d', { willReadFrequently: true })
  const colorProbe = document.createElement('canvas').getContext('2d', { willReadFrequently: true })
  const resolveColor = value => {
    if (!value || !CSS.supports('color', value)) throw new Error('Missing Graph observation color')
    colorProbe.clearRect(0, 0, 1, 1)
    colorProbe.fillStyle = value
    colorProbe.fillRect(0, 0, 1, 1)
    return [...colorProbe.getImageData(0, 0, 1, 1).data].slice(0, 3)
  }
  const palette = groups.map(({ color, fillColor = color }) => ({ color: resolveColor(color), fillColor: resolveColor(fillColor) }))
  const separateOutline = palette.map(({ color, fillColor }) => color.some((channel, index) => channel !== fillColor[index]))
  let previous = '', stable = 0, last = []
  const deadline = performance.now() + timeout
  while (performance.now() < deadline) {
    await new Promise(requestAnimationFrame)
    const rect = canvas.getBoundingClientRect()
    if (element.querySelector('.el-loading-mask') || !canvas.width || !canvas.height || rect.width <= 0 || rect.height <= 0 ||
      !canvas.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })) {
      previous = ''; stable = 0; continue
    }
    copy.width = canvas.width; copy.height = canvas.height
    context.drawImage(canvas, 0, 0)
    const width = copy.width, height = copy.height
    const pixels = context.getImageData(0, 0, width, height).data
    const channels = new Uint16Array(width * height), mask = new Uint8Array(width * height), queue = new Int32Array(width * height)
    for (let i = 0; i < mask.length; i++) {
      const offset = i * 4
      if (pixels[offset + 3] > 200) {
        palette.forEach(({ color, fillColor }, group) => {
          if (color.every((value, channel) => Math.abs(pixels[offset + channel] - value) <= 12)) channels[i] |= 1 << group
          if (fillColor.every((value, channel) => Math.abs(pixels[offset + channel] - value) <= 12)) mask[i] = group + 1
        })
      }
    }
    // Labels can split the outline, and shadow blending can separate it from the fill.
    // Ordinary node bounds are the connected fill, excluding disconnected same-color edges.
    // Only a distinct search outline is attributed to its nearest fill core.
    const cores = []
    for (let i = 0; i < mask.length; i++) {
      if (!mask[i]) continue
      let head = 0, tail = 1, minX = width, minY = height, maxX = 0, maxY = 0
      const group = mask[i] - 1
      queue[0] = i; mask[i] = 0
      while (head < tail) {
        const index = queue[head++], x = index % width, y = Math.floor(index / width)
        minX = Math.min(minX, x); maxX = Math.max(maxX, x); minY = Math.min(minY, y); maxY = Math.max(maxY, y)
        for (let dy = -1; dy <= 1; dy++) for (let dx = -1; dx <= 1; dx++) {
          const nx = x + dx, ny = y + dy, next = ny * width + nx
          if (nx >= 0 && nx < width && ny >= 0 && ny < height && mask[next] === group + 1) { mask[next] = 0; queue[tail++] = next }
        }
      }
      const w = (maxX - minX + 1) * rect.width / width, h = (maxY - minY + 1) * rect.height / height
      if (tail >= 40 && w >= 10 && h >= 10 && w / h > 0.7 && w / h < 1.4) {
        cores.push({ group, x: (minX + maxX) / 2, y: (minY + maxY) / 2,
          minX, minY, maxX, maxY, stroke: separateOutline[group] ? [width, height, 0, 0] : [minX, minY, maxX, maxY],
          pixelCount: separateOutline[group] ? 0 : tail, fillPixelCount: tail })
      }
    }
    if (groups.some((group, index) => cores.filter(core => core.group === index).length !== group.count)) { previous = ''; stable = 0; last = []; continue }
    for (let i = 0; i < channels.length; i++) {
      if (!channels[i]) continue
      const x = i % width, y = Math.floor(i / width)
      let closest = cores[0], distance = Infinity
      for (const core of cores) {
        const d = (x - core.x) ** 2 + (y - core.y) ** 2
        if (d < distance) { closest = core; distance = d }
      }
      if (separateOutline[closest.group] && (channels[i] & (1 << closest.group))) {
        closest.pixelCount++
        const box = closest.stroke
        box[0] = Math.min(box[0], x); box[1] = Math.min(box[1], y)
        box[2] = Math.max(box[2], x); box[3] = Math.max(box[3], y)
      }
    }
    const nodes = []
    for (const core of cores) {
      const [minX, minY, maxX, maxY] = core.stroke
      const sx = rect.width / width, sy = rect.height / height
      const w = (maxX - minX + 1) * sx, h = (maxY - minY + 1) * sy
      if (core.pixelCount >= 40 && core.fillPixelCount >= 40 && minX <= core.minX && minY <= core.minY &&
        maxX >= core.maxX && maxY >= core.maxY && w >= 10 && h >= 10 && w / h > 0.7 && w / h < 1.4) {
        nodes.push({ group: core.group, x: (minX + maxX) * sx / 2, y: (minY + maxY) * sy / 2,
          box: [minX * sx, minY * sy, maxX * sx, maxY * sy], fillBox: [core.minX * sx, core.minY * sy, core.maxX * sx, core.maxY * sy], pixelCount: core.pixelCount, fillPixelCount: core.fillPixelCount })
      }
    }
    nodes.sort((a, b) => a.x - b.x || a.y - b.y)
    const valid = nodes.length === groups.reduce((sum, group) => sum + group.count, 0) && nodes.every(node => node.box[0] > 1 && node.box[1] > 1 &&
      node.box[2] < rect.width - 1 && node.box[3] < rect.height - 1)
    const signature = JSON.stringify(nodes)
    stable = valid && signature === previous ? stable + 1 : 0
    previous = signature; last = nodes
    if (stable >= 10) return groups.map((_, index) => ({ width: rect.width, height: rect.height, ...palette[index],
      nodes: nodes.filter(node => node.group === index).map(({ group, ...node }) => node), stableFrames: stable }))
  }
  throw new Error(`Graph pixels did not reach a visible stable frame: ${JSON.stringify(last)}`)
}

// A separate visibility contract for deliberately occluded circles: safe exposed
// fill cores, actual draw order and mouse identity; not a relaxed full-node frame.
export async function stableExposedGraphPixels(element, { groups, timeout = 10_000 }) {
  const canvas = element.querySelector('canvas'), probe = document.createElement('canvas').getContext('2d',{willReadFrequently:true}),copy=document.createElement('canvas')
  const rgb = color => { probe.clearRect(0,0,1,1);probe.fillStyle=color;probe.fillRect(0,0,1,1);return [...probe.getImageData(0,0,1,1).data].slice(0,3) }
  const palette=groups.map(group => rgb(group.fillColor)), context=copy.getContext('2d', { willReadFrequently:true })
  if (!context || !groups.length || groups.some(group => !Number.isInteger(group.count) || group.count<=0)) throw new Error('Invalid exposed circle observation')
  const margin=3,deadline=performance.now()+timeout
  let previous='',stable=0,last=[]
  while (performance.now()<deadline) {
    await new Promise(requestAnimationFrame)
    const rect=canvas.getBoundingClientRect()
    if (element.querySelector('.el-loading-mask') || !canvas.checkVisibility({checkOpacity:true,checkVisibilityCSS:true}) || !rect.width || !rect.height) { stable=0;previous='';continue }
    const drawn=new Map()
    for (const circle of window.__graphNodePaint?.get(canvas)||[]) {
      const color=rgb(circle.color),group=palette.findIndex(value => value.every((channel,index) => channel===color[index]))
      if (group<0 || circle.alpha!==1 || !['x','y','rx','ry'].every(key => Number.isFinite(circle[key])) || circle.rx<=margin || circle.ry<=margin) continue
      const key=JSON.stringify([circle.x,circle.y,circle.rx,circle.ry,color]);drawn.delete(key);drawn.set(key,{...circle,color,group})
    }
    const circles=[...drawn.values()].map((circle,order)=>({...circle,order}))
    if (groups.some((group,index)=>circles.filter(circle=>circle.group===index).length!==group.count)) { last={circles,palette,counts:groups.map(g=>g.count)};stable=0;previous='';continue }
    copy.width=canvas.width;copy.height=canvas.height;context.drawImage(canvas,0,0)
    const sx=rect.width/canvas.width,sy=rect.height/canvas.height,pixels=context.getImageData(0,0,canvas.width,canvas.height).data
    const nodes=[]
    for (const circle of circles) {
      const points=[]
      let best=null,bestClearance=-Infinity,minX=Infinity,minY=Infinity,maxX=-Infinity,maxY=-Infinity
      for (let y=Math.max(0,Math.floor((circle.y-circle.ry)/sy));y<Math.min(canvas.height,Math.ceil((circle.y+circle.ry)/sy));y++) {
        for (let x=Math.max(0,Math.floor((circle.x-circle.rx)/sx));x<Math.min(canvas.width,Math.ceil((circle.x+circle.rx)/sx));x++) {
          const px=(x+.5)*sx,py=(y+.5)*sy,offset=(y*canvas.width+x)*4
          const distance=other=>Math.hypot((px-other.x)/other.rx,(py-other.y)/other.ry)
          const clearance=Math.min((1-distance(circle))*Math.min(circle.rx,circle.ry)-margin,
            ...circles.filter(other=>other!==circle).map(other=>(distance(other)-1)*Math.min(other.rx,other.ry)-margin))
          if (clearance<=0 || pixels[offset+3]<=200 || !circle.color.every((value,index)=>Math.abs(pixels[offset+index]-value)<=12)) continue
          const point={x:px,y:py,pixel:[...pixels.slice(offset,offset+4)]}
          points.push(point);minX=Math.min(minX,px);maxX=Math.max(maxX,px);minY=Math.min(minY,py);maxY=Math.max(maxY,py)
          if (clearance>bestClearance) { bestClearance=clearance;best=point }
        }
      }
      if (points.length<40 || !best || !(circle.x-circle.rx>1 && circle.y-circle.ry>1 && circle.x+circle.rx<rect.width-1 && circle.y+circle.ry<rect.height-1)) continue
      nodes.push({group:circle.group,x:best.x,y:best.y,box:[minX,minY,maxX,maxY],pixelCount:points.length,
        geometry:{x:circle.x,y:circle.y,rx:circle.rx,ry:circle.ry,color:circle.color,alpha:circle.alpha,order:circle.order},
        samples:Array.from({length:5},(_,index)=>points[Math.floor(index*(points.length-1)/4)]),pixel:best.pixel})
    }
    const signature=JSON.stringify(nodes),valid=nodes.length===circles.length
    stable=valid && signature===previous?stable+1:0;previous=signature;last={nodes,circles,palette}
    if (stable>=10) return groups.map((_,group)=>({schema_version:'addp.graph-exposed-pixels/v1',width:rect.width,height:rect.height,color:palette[group],fillColor:palette[group],margin,
      nodes:nodes.filter(node=>node.group===group).map(({group,...node})=>node).sort((a,b)=>a.geometry.x-b.geometry.x||a.geometry.y-b.geometry.y),stableFrames:stable}))
  }
  throw new Error('Exposed circle pixels did not settle: '+JSON.stringify(last))
}

export async function stableEmptyReviewSelection(element, { timeout = 10_000 } = {}) {
  const deadline = performance.now() + timeout
  let previous = '', stable = 0
  while (performance.now() < deadline) {
    await new Promise(requestAnimationFrame)
    const boxes = [...element.querySelectorAll('.el-checkbox__input')]
    const states = boxes.map(box => {
      const input = box.querySelector('input'), inner = box.querySelector('.el-checkbox__inner')
      const reference = document.createElement('span')
      reference.className = 'el-checkbox__inner'
      reference.style.position = 'absolute'
      reference.style.visibility = 'hidden'
      box.append(reference)
      const actual = getComputedStyle(inner), expected = getComputedStyle(reference)
      const value = { scope: box.closest('thead') ? 'header' : 'row',
        visible: inner.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true }), checked: input.checked, indeterminate: input.indeterminate,
        checkedClass: box.classList.contains('is-checked'), mixedClass: box.classList.contains('is-indeterminate'),
        background: actual.backgroundColor, emptyBackground: expected.backgroundColor,
        tick: getComputedStyle(inner, '::after').transform, emptyTick: getComputedStyle(reference, '::after').transform,
        animating: inner.getAnimations({ subtree: true }).some(animation => animation.pending || animation.playState === 'running') }
      reference.remove()
      return value
    })
    const valid = states.length >= 2 && states.every(state => state.visible && !state.checked && !state.indeterminate && !state.checkedClass &&
      !state.mixedClass && !state.animating && state.background === state.emptyBackground && state.tick === state.emptyTick)
    const signature = JSON.stringify(states)
    stable = valid && signature === previous ? stable + 1 : 0
    previous = signature
    if (stable >= 3) return { states, stableFrames: stable }
  }
  throw new Error('Row/header checkboxes did not settle to their unchecked visual state')
}

export async function clearGraphSelection(frame) {
  const panel = frame.locator('.node-panel')
  if (await panel.count()) {
    await panel.locator('.panel-header button').click()
    await expect(panel).toHaveCount(0)
  }
}

export async function clickGraphNode(frame, canvas, point, expected) {
  await clearGraphSelection(frame)
  await canvas.locator('canvas').click({ position: { x: point.x, y: point.y } })
  const property = frame.locator('.node-panel .prop-row').filter({
    has: frame.locator('.prop-key').filter({ hasText: /^id$/ })
  }).locator('.prop-val')
  await expect.poll(async () => {
    const id = await property.innerText().catch(() => '')
    return expected.some(node => String(node.properties.id) === id)
  }).toBe(true)
  const sourceID = await property.innerText()
  const node = expected.find(node => String(node.properties.id) === sourceID)
  await expect(frame.locator('.node-panel .panel-name')).toHaveText(node.display_name || node.id)
  return { source_id: sourceID, native_id: node.id, x: point.x, y: point.y }
}

export async function graphNodeHits(frame, canvas, stable, expected) {
  const hits = []
  for (const point of stable.nodes) {
    hits.push(await clickGraphNode(frame, canvas, point, expected))
  }
  expect(hits.map(hit => hit.native_id).sort()).toEqual(expected.map(node => node.id).sort())
  return hits
}

export async function graphObservationColor(frame, canvas, fill = false) {
  if (!fill) return canvas.evaluate(element => getComputedStyle(element).getPropertyValue('--addp-graph-search-match').trim())
  const marker = frame.locator('.filter-panel .label-dot')
  await expect(marker).toHaveCount(1) // This isolated fixture has one Person shape.
  return marker.evaluate(element => getComputedStyle(element).backgroundColor)
}

export async function exploreGraphLayouts(page, frame, canvas, root, nodes, edges, capture) {
  const requests = []
  const record = request => {
    if (new URL(request.url()).pathname.startsWith(root)) requests.push({ method: request.method(), path: new URL(request.url()).pathname, body: request.postDataJSON() })
  }
  page.on('request', record)
  // Observe the ordinary node fill after clearing search/selection halos through real controls.
  await frame.locator('.search-input').hover()
  await frame.locator('.search-input .el-input__clear').click()
  await expect(frame.locator('.search-input input')).toHaveValue('')
  await clearGraphSelection(frame)
  const stages = [], color = await graphObservationColor(frame, canvas, true)
  const normalizeEdges = values => values.map(({ id, source, target, type }) => ({ id, source, target, type })).sort((a, b) => a.id.localeCompare(b.id))
  async function observe(phase, expectedNodes, expectedEdges, extra = {}) {
    await clearGraphSelection(frame)
    const [stable] = await canvas.evaluate(stableGraphPixels, { groups: [{ count: expectedNodes.length, color }] })
    const hits = await graphNodeHits(frame, canvas, stable, expectedNodes)
    stages.push({ phase, ...extra, native_ids: Object.fromEntries(expectedNodes.map(node => [node.properties.id, node.id])),
      edges: normalizeEdges(expectedEdges), frame: stable, hits })
    await capture(phase)
    return hits
  }
  try {
    let currentNodes = nodes, currentEdges = edges
    const [stable] = await canvas.evaluate(stableGraphPixels, { groups: [{ count: nodes.length, color }] })
    let hits = await graphNodeHits(frame, canvas, stable, nodes)
    for (const [index, anchor] of nodes.slice(0, 2).entries()) {
      await clickGraphNode(frame, canvas, hits.find(hit => hit.native_id === anchor.id), [anchor])
      const response = page.waitForResponse(value => value.request().method() === 'POST' && new URL(value.url()).pathname === root + '/expand')
      await frame.getByRole('button', { name: '展开邻居', exact: true }).click()
      const received = await response
      expect(received.status()).toBe(200)
      const body = received.request().postDataJSON()
      expect(body).toEqual({ target: { kind: 'entity', id: anchor.id }, depth: 1, node_limit: 200, relationship_limit: 400 })
      const result = await received.json()
      currentNodes = index === 0 ? nodes.slice(0, 2) : nodes
      currentEdges = index === 0 ? edges.filter(edge => [edge.source, edge.target].every(id => currentNodes.some(node => node.id === id))) : edges
      expect(result.nodes.map(node => ({ id: node.id, properties: node.properties })).sort((a, b) => a.id.localeCompare(b.id)))
        .toEqual(currentNodes.map(node => ({ id: node.id, properties: node.properties })).sort((a, b) => a.id.localeCompare(b.id)))
      expect(normalizeEdges(result.edges)).toEqual(normalizeEdges(currentEdges))
      expect(result.nodes).toHaveLength(index === 0 ? 2 : 3)
      expect(result.edges).toHaveLength(index === 0 ? 1 : 2)
      hits = await observe(`expand-${index + 1}`, result.nodes, result.edges, { request: body, http_status: received.status() })
    }
    for (const [layout, label] of [['dagre', '分层'], ['circular', '环形'], ['radial', '辐射'], ['force', '力导向']]) {
      const select = frame.locator('.layout-select')
      await select.click()
      const list = frame.locator('#' + await select.getByRole('combobox').getAttribute('aria-controls'))
      await list.getByRole('option', { name: label, exact: true }).click()
      await expect(select).toHaveText(label)
      await observe(`layout-${layout}`, currentNodes, currentEdges, { layout, selected_label: await select.innerText() })
      expect(stages.at(-1).hits.map(({ native_id, x, y }) => ({ native_id, x, y })).sort((a, b) => a.native_id.localeCompare(b.native_id)))
        .not.toEqual(stages.at(-2).hits.map(({ native_id, x, y }) => ({ native_id, x, y })).sort((a, b) => a.native_id.localeCompare(b.native_id)))
    }
    expect(requests).toEqual(stages.slice(0, 2).map(stage => ({ method: 'POST', path: root + '/expand', body: stage.request })))
    return { stages, requests, native_edges: normalizeEdges(edges) }
  } finally { page.off('request', record) }
}

export async function observeDenseGraph(frame, canvas, shapes, nodes, edges, search = false, visibility = 'full') {
  expect(['full','exposed']).toContain(visibility)
  expect(search && visibility === 'exposed').toBe(false)
  const nodeFacts = values => values.map(({ id, labels, properties }) => ({ id, labels: [...labels].sort(), properties })).sort((a, b) => a.id.localeCompare(b.id))
  const normalize = values => values.map(({ id, source, target, type, properties }) => ({ id, source, target, type, properties })).sort((a, b) => a.id.localeCompare(b.id))
  await clearGraphSelection(frame)
  const entries = []
  for (const shape of shapes) {
    const members = nodes.filter(node => JSON.stringify([...node.labels].sort()) === JSON.stringify([...shape.labels].sort()))
    if (!members.length) continue
    const item = frame.locator('.filter-section').first().locator('.filter-item').filter({ has: frame.getByRole('checkbox', { name: shape.name, exact: true }) })
    const fillColor = await item.locator('.label-dot').evaluate(element => getComputedStyle(element).backgroundColor)
    entries.push({ shape, members, count: members.length, fillColor, color: search ? await graphObservationColor(frame, canvas) : fillColor })
  }
  expect(entries.reduce((sum, entry) => sum + entry.count, 0)).toBe(nodes.length)
  const frames = await canvas.evaluate(visibility === 'exposed' ? stableExposedGraphPixels : stableGraphPixels, { groups: entries.map(({ count, color, fillColor }) => ({ count, color, fillColor })) })
  const groups = []
  for (const [index, entry] of entries.entries()) groups.push({ name: entry.shape.name, labels: [...entry.shape.labels].sort(),
    frame: frames[index], hits: await graphNodeHits(frame, canvas, frames[index], entry.members) })
  await clearGraphSelection(frame)
  const colors = []
  for (const type of [...new Set(edges.map(edge => edge.type))].sort()) {
    const item = frame.locator('.filter-section').nth(1).locator('.filter-item').filter({ has: frame.locator('input.el-checkbox__original[value=' + JSON.stringify(type) + ']') })
    colors.push({ type, color: await item.locator('.edge-swatch').evaluate(element => getComputedStyle(element).color) })
  }
  const relationship_pixels = colors.length ? await canvas.evaluate(stableGraphColorPixels, { colors, boxes: frames.flatMap(frame => frame.nodes.map(node => node.box)) }) : []
  return { relationship_pixels, nodes: nodeFacts(nodes), edges: normalize(edges), groups }
}

// Four full label sets share the same pixel observer and settle in one rAF sequence.
export async function denseGraphStages(page, frame, canvas, expected, capture) {
  const root = `/api/v1/graph/graphs/${expected.graph_id}`
  const normalize = values => values.map(({ id, source, target, type, properties }) => ({ id, source, target, type, properties })).sort((a, b) => a.id.localeCompare(b.id))
  const nodeFacts = nodes => nodes.map(node => ({ id: node.id, labels: [...node.labels].sort(), properties: node.properties })).sort((a, b) => a.id.localeCompare(b.id))
  // This fixture's ontology explicitly declares name as its display property.
  // The independent KG reference intentionally contains no browser display DTO.
  const reference = expected.nodes.map(node => ({ ...node, display_name: node.properties.name }))
  const shapes = expected.shapes
  const requests = [], stages = [], relationships = [], relation_filters = []
  let node_filters
  const record = request => {
    if (new URL(request.url()).pathname.startsWith(root)) requests.push({ method: request.method(), path: new URL(request.url()).pathname, body: request.method() === 'POST' ? request.postDataJSON() : null })
  }
  await expect(canvas.locator('canvas')).toBeVisible()
  await expect(frame.locator('.filter-section').first().locator('.filter-item')).toHaveCount(shapes.length)
  await expect(canvas.locator('.el-loading-mask')).toHaveCount(0)
  page.on('request', record)
  async function observe(phase, nodes, edges, extra = {}, search = false, collection = stages) {
    const observed = await observeDenseGraph(frame, canvas, shapes, nodes, edges, search)
    const { groups } = observed
    collection.push({ phase, ...observed, ...extra })
    await clearGraphSelection(frame)
    if (collection === stages) await capture(phase)
    return groups.flatMap(group => group.hits)
  }
  try {
    const search = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === root + '/search')
    await frame.locator('.search-input input').fill('Dense')
    await frame.locator('.search-input input').press('Enter')
    const response = await search
    expect(response.status()).toBe(200)
    const result = await response.json()
    expect(nodeFacts(result.nodes)).toEqual(nodeFacts(reference))
    for (const node of result.nodes) expect(node.display_name).toBe(reference.find(item => item.id === node.id).display_name)
    expect(result.edges).toEqual([])
    let hits = await observe('dense-search', result.nodes, [], { request: response.request().postDataJSON(), http_status: 200 }, true)
    await frame.locator('.search-input').hover()
    await frame.locator('.search-input .el-input__clear').click()
    await clearGraphSelection(frame)
    hits = await observe('dense-search-cleared', reference, [])
    const anchor = reference.find(node => node.properties.id === 'n01')
    await clickGraphNode(frame, canvas, hits.find(hit => hit.native_id === anchor.id), [anchor])
    const expand = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === root + '/expand')
    const twoHops = frame.getByRole('radio', { name: '2 跳', exact: true })
    await twoHops.locator('..').click()
    await expect(twoHops).toBeChecked()
    const expanded = await expand
    expect(expanded.status()).toBe(200)
    const body = expanded.request().postDataJSON()
    expect(body).toEqual({ target: { kind: 'entity', id: anchor.id }, depth: 2, node_limit: 200, relationship_limit: 400 })
    const dense = await expanded.json()
    expect(nodeFacts(dense.nodes)).toEqual(nodeFacts(reference))
    expect(normalize(dense.edges)).toEqual(normalize(expected.edges))
    await observe('dense-expand', dense.nodes, dense.edges, { request: body, http_status: 200 })
    const selected = shapes.find(shape => [...shape.labels].sort().join('+') === 'Employee+Person')
    const checks = frame.locator('.filter-section').first()
    async function setShape(shape, checked) {
      const item = checks.locator('.filter-item').filter({ has: frame.getByRole('checkbox', { name: shape.name, exact: true }) })
      const input = item.getByRole('checkbox')
      if (await input.isChecked() !== checked) await item.locator('.el-checkbox').click()
      if (checked) await expect(input).toBeChecked()
      else await expect(input).not.toBeChecked()
    }
    for (const shape of shapes.filter(shape => shape.name !== selected.name)) await setShape(shape, false)
    const members = reference.filter(node => [...node.labels].sort().join('+') === 'Employee+Person')
    const visible = new Set(members.map(node => node.id))
    await observe('dense-filter', members, expected.edges.filter(edge => visible.has(edge.source) && visible.has(edge.target)), { selected_shapes: [selected.name] })
    for (const shape of shapes) await setShape(shape, true)
    await observe('dense-filter-restored', reference, expected.edges, { selected_shapes: shapes.map(shape => shape.name).sort() })
    await expect(frame.locator('.layout-select')).toHaveText('力导向')
    const baseline = await graphRelationshipHits(page, frame, canvas, expected.edges, { ...stages.at(-1), layout: 'force' })
    const types = [...new Set(expected.edges.map(edge => edge.type))].sort()
    for (const [index, selected_types] of [[types[1]], [], [types[0]], types].entries()) {
      for (const type of types) {
        const item = frame.locator('.filter-section').nth(1).locator('.filter-item').filter({ has: frame.locator('input.el-checkbox__original[value=' + JSON.stringify(type) + ']') })
        const input = item.getByRole('checkbox')
        if (await input.isChecked() !== selected_types.includes(type)) await item.locator('.el-checkbox').click()
        await expect(input).toBeChecked({ checked: selected_types.includes(type) })
      }
      const edges = expected.edges.filter(edge => selected_types.includes(edge.type))
      await expect(frame.locator('.toolbar-left .el-tag').nth(1)).toHaveText(`画布: 12 节点 / ${edges.length} 关系`)
      await observe('relation-filter-' + index, reference, edges, { layout: 'force', selected_types: await frame.locator('.filter-section').nth(1).locator('input:checked').evaluateAll(inputs => inputs.map(input => input.value).sort()) }, false, relation_filters)
      const stage = relation_filters.at(-1), colors = []
      for (const type of types.filter(type => !selected_types.includes(type))) {
        const item = frame.locator('.filter-section').nth(1).locator('.filter-item').filter({ has: frame.locator('input.el-checkbox__original[value=' + JSON.stringify(type) + ']') })
        colors.push({ type, color: await item.locator('.edge-swatch').evaluate(element => getComputedStyle(element).color) })
      }
      stage.absent_pixels = colors.length ? await canvas.evaluate(stableGraphColorPixels, { colors, absent: true, boxes: stage.groups.flatMap(group => group.frame.nodes.map(node => node.box)) }) : []
      stage.relationships = edges.length && index < 3 ? await graphRelationshipHits(page, frame, canvas, edges, stage) : null
      stage.probes = []
      if (!edges.length) {
        const box = await canvas.locator('canvas').boundingBox()
        for (const hit of baseline.hits) {
          await clearGraphSelection(frame)
          await page.mouse.click(box.x + hit.x, box.y + hit.y)
          await canvas.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
          await expect(frame.locator('.node-panel .panel-title .el-tag').filter({ hasText: /^关系$/ })).toHaveCount(0)
          stage.probes.push({ native_id: hit.native_id, x: hit.x, y: hit.y, relation_selected: false })
        }
      }
      await clearGraphSelection(frame)
      await capture(stage.phase)
    }
    const beforeNodes = relation_filters.at(-1)
    const nodeBaseline = await graphRelationshipHits(page, frame, canvas, expected.edges, beforeNodes)
    await clearGraphSelection(frame)
    for (const shape of shapes) await setShape(shape, false)
    await expect(frame.locator('.toolbar-left .el-tag').nth(1)).toHaveText('画布: 0 节点 / 0 关系')
    await expect(canvas.locator('canvas')).toBeVisible()
    await expect(canvas.locator('.empty-hint')).toBeVisible()
    await expect(canvas.locator('.el-loading-mask')).toHaveCount(0)
    const colors = [
      ...beforeNodes.groups.map(group => ({ type: 'node:' + group.name, color: `rgb(${group.frame.fillColor.join(',')})` })),
      ...beforeNodes.relationship_pixels.map(sample => ({ type: 'relationship:' + sample.type, color: `rgb(${sample.color.join(',')})` }))
    ]
    const absent_pixels = await canvas.evaluate(stableGraphColorPixels, { colors, boxes: [], absent: true })
    const nodeBox = await canvas.locator('canvas').boundingBox()
    const empty = { phase: 'node-filter-empty', selected_shapes: await checks.locator('input:checked').evaluateAll(inputs => inputs.map(input => input.value).sort()),
      selected_types: await frame.locator('.filter-section').nth(1).locator('input:checked').evaluateAll(inputs => inputs.map(input => input.value).sort()),
      width: nodeBox.width, height: nodeBox.height, nodes: [], edges: [], canvas_visible: true, hint_visible: true, loading_absent: true, absent_pixels }
    async function hiddenProbes(points) {
      const probes = []
      for (const hit of points) {
        await clearGraphSelection(frame)
        await page.mouse.click(nodeBox.x + hit.x, nodeBox.y + hit.y)
        await canvas.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
        await expect(frame.locator('.node-panel')).toHaveCount(0)
        probes.push({ native_id: hit.native_id, x: hit.x, y: hit.y, selection_absent: true })
      }
      return probes
    }
    empty.node_probes = await hiddenProbes(beforeNodes.groups.flatMap(group => group.hits))
    empty.relationship_probes = await hiddenProbes(nodeBaseline.hits)
    await capture(empty.phase)
    for (const shape of shapes) await setShape(shape, true)
    const restored = []
    await observe('node-filter-restored', reference, expected.edges, { layout: 'force', selected_shapes: await checks.locator('input:checked').evaluateAll(inputs => inputs.map(input => input.value).sort()) }, false, restored)
    await expect(frame.locator('.toolbar-left .el-tag').nth(1)).toHaveText('画布: 12 节点 / 72 关系')
    await expect(canvas.locator('.empty-hint')).toHaveCount(0)
    node_filters = { baseline: nodeBaseline, empty, restored: restored[0] }
    await capture('node-filter-restored')
    for (const [layout, label] of [['dagre', '分层'], ['circular', '环形'], ['radial', '辐射'], ['force', '力导向']]) {
      const select = frame.locator('.layout-select')
      await select.click()
      const list = frame.locator('#' + await select.getByRole('combobox').getAttribute('aria-controls'))
      await list.getByRole('option', { name: label, exact: true }).click()
      await expect(select).toHaveText(label)
      await observe('dense-layout-' + layout, reference, expected.edges, { layout, selected_label: await select.innerText() })
      relationships.push(await graphRelationshipHits(page, frame, canvas, expected.edges, stages.at(-1)))
      await capture('relationship-' + layout)
    }
    expect(requests).toEqual([
      { method: 'POST', path: root + '/search', body: stages[0].request },
      { method: 'POST', path: root + '/expand', body: stages[2].request }
    ])
    return { stages, requests, relationships, relation_filters: { baseline, stages: relation_filters }, node_filters }
  } finally { page.off('request', record) }
}

export async function stableGraphColorPixels(element, { colors, boxes, absent = false, timeout = 10_000 }) {
  const canvas = element.querySelector('canvas'), copy = document.createElement('canvas')
  const context = copy.getContext('2d', { willReadFrequently: true })
  const probe = document.createElement('canvas').getContext('2d', { willReadFrequently: true })
  const palette = colors.map(({ type, color }) => {
    if (!CSS.supports('color', color)) throw new Error('Missing relationship color')
    probe.clearRect(0, 0, 1, 1); probe.fillStyle = color; probe.fillRect(0, 0, 1, 1)
    return { type, color: [...probe.getImageData(0, 0, 1, 1).data].slice(0, 3) }
  })
  let previous = '', stable = 0, samples = []
  const deadline = performance.now() + timeout
  while (performance.now() < deadline) {
    await new Promise(requestAnimationFrame)
    const rect = canvas.getBoundingClientRect()
    copy.width = canvas.width; copy.height = canvas.height
    context.drawImage(canvas, 0, 0)
    const pixels = context.getImageData(0, 0, copy.width, copy.height).data
    samples = palette.map(entry => ({ ...entry, pixelCount: 0 }))
    for (let index = 0; index < copy.width * copy.height; index++) {
      const x = (index % copy.width) * rect.width / copy.width, y = Math.floor(index / copy.width) * rect.height / copy.height
      if (boxes.some(([x0, y0, x1, y1]) => x >= x0-3 && x <= x1+3 && y >= y0-3 && y <= y1+3)) continue
      const offset = index*4
      if (pixels[offset+3] <= 100) continue
      for (const sample of samples) if (sample.color.every((value, channel) => Math.abs(pixels[offset+channel]-value) <= 12)) sample.pixelCount++
    }
    const signature = JSON.stringify(samples)
    const valid = canvas.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true }) && !element.querySelector('.el-loading-mask') && samples.every(sample => absent ? sample.pixelCount === 0 : sample.pixelCount >= 40)
    stable = valid && signature === previous ? stable+1 : 0
    previous = signature
    if (stable >= 10) return samples.map(sample => ({ ...sample, stableFrames: stable }))
  }
  throw new Error('Graph color pixels did not settle: ' + JSON.stringify(samples))
}

// Fixed geometry is established only with real mouse dragging in the Graph page.
export async function graphEndpointGeometry(page, frame, canvas, expected, capture, scenario = 'different-color') {
  expect(['different-color','same-color']).toContain(scenario)
  const sameColor = scenario === 'same-color'
  const observe = (...args) => observeDenseGraph(...args, false, sameColor ? 'exposed' : 'full')
  const root = `/api/v1/graph/graphs/${expected.graph_id}`
  const reference = expected.nodes.map(node => ({ ...node, display_name: node.properties.name }))
  const source = reference.find(node => node.properties.id === 'n01')
  const target = reference.find(node => node.properties.id === 'n12')
  const differentNearby = reference.find(node => node.properties.id === 'n04')
  const edge = expected.edges.find(edge => edge.source === source.id && edge.target === target.id && edge.type === 'Parallel')
  expect(edge).toBeTruthy()
  const requests = [], drags = []
  const record = request => {
    if (new URL(request.url()).pathname.startsWith(root)) requests.push({ method: request.method(), path: new URL(request.url()).pathname, body: request.method() === 'POST' ? request.postDataJSON() : null })
  }
  await expect(canvas.locator('canvas')).toBeVisible()
  await expect(frame.locator('.filter-section').first().locator('.filter-item')).toHaveCount(expected.shapes.length)
  await expect(canvas.locator('.el-loading-mask')).toHaveCount(0)
  page.on('request', record)
  try {
  const search = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === root + '/search')
  await frame.locator('.search-input input').fill('Dense')
  await frame.locator('.search-input input').press('Enter')
  const found = await search
  expect(found.status()).toBe(200)
  expect(found.request().postDataJSON()).toEqual({ query: 'Dense', limit: 30 })
  await expect(frame.locator('.toolbar-left .el-tag').nth(1)).toHaveText('画布: 12 节点 / 0 关系')
  await frame.locator('.search-input').hover()
  await frame.locator('.search-input .el-input__clear').click()
  const initial = await observe(frame, canvas, expected.shapes, reference, [])
  await clickGraphNode(frame, canvas, initial.groups.flatMap(group => group.hits).find(hit => hit.native_id === source.id), [source])
  const expand = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === root + '/expand')
  await frame.getByRole('radio', { name: '2 跳', exact: true }).locator('..').click()
  const expanded = await expand
  expect(expanded.status()).toBe(200)
  expect(expanded.request().postDataJSON()).toEqual({ target: { kind: 'entity', id: source.id }, depth: 2, node_limit: 200, relationship_limit: 400 })
  const link = frame.locator('.filter-section').nth(1).locator('.filter-item').filter({ has: frame.locator('input.el-checkbox__original[value="Link"]') })
  await link.locator('.el-checkbox').click()
  await expect(link.getByRole('checkbox')).not.toBeChecked()
  await expect(frame.locator('.toolbar-left .el-tag').nth(1)).toHaveText('画布: 12 节点 / 4 关系')
  const edges = expected.edges.filter(edge => edge.type === 'Parallel')
  let observed = await observe(frame, canvas, expected.shapes, reference, edges)
  const before_drag = observed
  const positions = new Map(), centers = new Map()
  function refreshPositions(stage) {
    for (const group of stage.groups) for (const [index,hit] of group.hits.entries()) {
      positions.set(hit.native_id,{x:hit.x,y:hit.y})
      if (sameColor) centers.set(hit.native_id,group.frame.nodes[index].geometry)
    }
  }
  refreshPositions(observed)
  async function drag(node, to) {
    await clearGraphSelection(frame)
    const from = positions.get(node.id), center=centers.get(node.id), planned=to
    if (sameColor) to={x:from.x+planned.x-center.x,y:from.y+planned.y-center.y}
    const box = await canvas.locator('canvas').boundingBox()
    await page.mouse.move(box.x + from.x, box.y + from.y)
    await page.mouse.down()
    await page.mouse.move(box.x + to.x, box.y + to.y, { steps: 16 })
    await page.mouse.up()
    await canvas.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    let after
    if (sameColor) {
      const matches=await canvas.evaluate((element,point)=>(window.__graphNodePaint?.get(element.querySelector('canvas'))||[]).filter(circle=>Math.hypot(circle.x-point.x,circle.y-point.y)<2),planned)
      expect(matches).toHaveLength(1);after=matches[0];centers.set(node.id,after)
    }
    drags.push({ native_id: node.id, from, to, ...(sameColor ? {center_before:{x:center.x,y:center.y},center_after:{x:after.x,y:after.y},planned_center:planned} : {}) });positions.set(node.id, to)
  }
  const ordered = [...reference].sort((a, b) => a.properties.id.localeCompare(b.properties.id))
  const parking = ordered.map((node, index) => ({ native_id: node.id, x: 70 + index % 6 * 115, y: index < 6 ? 90 : 680 }))
  for (const [index, node] of ordered.entries()) await drag(node, { x: parking[index].x, y: parking[index].y })
  await drag(source, { x: 200, y: 330 });await drag(target, { x: 550, y: 330 })
  observed = await observe(frame, canvas, expected.shapes, reference, edges)
  if (sameColor) refreshPositions(observed)
  const baseline = { phase: sameColor ? 'same-color-baseline' : 'endpoint-baseline', layout: 'force', ...observed }
  await capture(baseline.phase, baseline)
  baseline.relationships = await graphRelationshipHits(page, frame, canvas, [edge], baseline)
  await capture(baseline.phase, baseline)
  const phases = []
  const steps = sameColor ? ['source','target'].flatMap(side => ['near','overlap','restored'].map(state => ({side,state}))) : ['source','target'].map(side => ({side}))
  for (const {side,state} of steps) {
    const owner = side === 'source' ? source : target
    const nearby = sameColor ? reference.find(node => node.properties.id === (side === 'source' ? 'n02' : 'n11')) : differentNearby
    const hit = baseline.relationships.hits[0]
    const anchor = hit.path_source === owner.id ? hit.path[0].slice(1) : hit.path[1].slice(-2)
    const center = baseline.relationships.node_geometry.find(node => node.native_id === owner.id)
    const dx = anchor[0] - center.x, dy = anchor[1] - center.y
    const parked=parking.find(node => node.native_id===nearby.id), nearRadius=baseline.relationships.node_geometry.find(node => node.native_id===nearby.id).rx
    const factor=state==='near'?(center.rx+nearRadius+2)/Math.hypot(dx,dy):.8
    const destination = sameColor ? (state==='restored' ? {x:parked.x,y:parked.y} : {x:center.x+dx*factor,y:center.y+dy*factor}) : {x:anchor[0]+dx*.8,y:anchor[1]+dy*.8}
    await drag(nearby, destination)
    observed = await observe(frame, canvas, expected.shapes, reference, edges)
    if (sameColor) refreshPositions(observed)
    const phase = { phase: sameColor ? `same-color-${side}-${state}` : 'endpoint-near-' + side, layout: 'force', side, ...(sameColor?{state}:{}), owner: owner.id, nearby: nearby.id, planned_center: destination, ...observed }
    if (state==='overlap') {
      const painted=await canvas.evaluate(stableRelationshipPaths,{nodes:phase.groups})
      const trace=await canvas.evaluate(graphOverlapPaint,{geometry:painted.node_geometry,owner:owner.id,nearby:nearby.id})
      const front=reference.find(node=>node.id===trace.fills.at(-1).native_id)
      phase.overlap={...trace,stable_frames:painted.stable_frames,hit:await clickGraphNode(frame,canvas,trace.point,[front])}
    }
    await capture(phase.phase, phase)
    await clearGraphSelection(frame)
    phase.relationships = await graphRelationshipHits(page, frame, canvas, [edge], phase)
    const current = phase.relationships.hits[0]
    const endpoint = current.path_source === owner.id ? current.path[0].slice(1) : current.path[1].slice(-2)
    const centers = phase.groups.flatMap(group => group.hits)
    const nearest = [...centers].sort((a, b) => Math.hypot(a.x - endpoint[0], a.y - endpoint[1]) - Math.hypot(b.x - endpoint[0], b.y - endpoint[1]))[0]
    if (!sameColor) expect(nearest.native_id, 'Fixed geometry must expose the old nearest-center error').toBe(nearby.id)
    if (!sameColor) phase.old_nearest = nearest.native_id
    const pixelNodes = phase.groups.flatMap(group => group.hits.map((hit, index) => ({ ...hit, box: group.frame.nodes[index].box })))
    const oldDistance = node => {
      const rx = (node.box[2]-node.box[0])/2, ry = (node.box[3]-node.box[1])/2
      return Math.abs(Math.hypot((endpoint[0]-node.x)/rx,(endpoint[1]-node.y)/ry)-1)*Math.min(rx,ry)
    }
    if (!sameColor) {
      phase.old_pixel_boundary = [...pixelNodes].sort((a,b) => oldDistance(a)-oldDistance(b))[0].native_id
      expect(phase.old_pixel_boundary, 'Fixed geometry must expose the clipped pixel-boundary error').toBe(nearby.id)
    }
    phases.push(phase)
    await capture(phase.phase, phase)
  }
  expect(requests).toEqual([{ method: 'POST', path: root + '/search', body: { query: 'Dense', limit: 30 } }, { method: 'POST', path: root + '/expand', body: { target: { kind: 'entity', id: source.id }, depth: 2, node_limit: 200, relationship_limit: 400 } }])
  return { ...(sameColor?{geometry_case:'same-color'}:{}), parking, drags, before_drag, baseline, phases, requests, target_edge_id: edge.id }
  } finally { page.off('request', record) }
}
