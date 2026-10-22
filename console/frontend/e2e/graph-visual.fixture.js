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
    // Locate fill cores first, then attribute exact-color pixels to their nearest core.
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
          minX, minY, maxX, maxY, stroke: [width, height, 0, 0], pixelCount: 0, fillPixelCount: tail })
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
      if (channels[i] & (1 << closest.group)) {
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

// Four full label sets share the same pixel observer and settle in one rAF sequence.
export async function denseGraphStages(page, frame, canvas, expected, capture) {
  const root = `/api/v1/graph/graphs/${expected.graph_id}`
  const normalize = values => values.map(({ id, source, target, type }) => ({ id, source, target, type })).sort((a, b) => a.id.localeCompare(b.id))
  const nodeFacts = nodes => nodes.map(node => ({ id: node.id, labels: [...node.labels].sort(), properties: node.properties })).sort((a, b) => a.id.localeCompare(b.id))
  const reference = expected.nodes
  const shapes = expected.shapes
  const requests = [], stages = []
  const record = request => {
    if (new URL(request.url()).pathname.startsWith(root)) requests.push({ method: request.method(), path: new URL(request.url()).pathname, body: request.method() === 'POST' ? request.postDataJSON() : null })
  }
  await expect(canvas.locator('canvas')).toBeVisible()
  await expect(frame.locator('.filter-section').first().locator('.filter-item')).toHaveCount(shapes.length)
  await expect(canvas.locator('.el-loading-mask')).toHaveCount(0)
  page.on('request', record)
  async function observe(phase, nodes, edges, extra = {}, search = false) {
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
    const frames = await canvas.evaluate(stableGraphPixels, { groups: entries.map(({ count, color, fillColor }) => ({ count, color, fillColor })) })
    const groups = []
    for (const [index, entry] of entries.entries()) groups.push({ name: entry.shape.name, labels: [...entry.shape.labels].sort(),
      frame: frames[index], hits: await graphNodeHits(frame, canvas, frames[index], entry.members) })
    await clearGraphSelection(frame)
    const colors = []
    for (const type of [...new Set(edges.map(edge => edge.type))].sort()) {
      const item = frame.locator('.filter-section').nth(1).locator('.filter-item').filter({ has: frame.locator('input.el-checkbox__original[value=' + JSON.stringify(type) + ']') })
      colors.push({ type, color: await item.locator('.edge-swatch').evaluate(element => getComputedStyle(element).color) })
    }
    const relationship_pixels = colors.length ? await canvas.evaluate(stableRelationshipPixels, { colors, boxes: frames.flatMap(frame => frame.nodes.map(node => node.box)) }) : []
    stages.push({ phase, relationship_pixels, nodes: nodeFacts(nodes), edges: normalize(edges), groups, ...extra })
    await clearGraphSelection(frame)
    await capture(phase)
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
    for (const [layout, label] of [['dagre', '分层'], ['circular', '环形'], ['radial', '辐射'], ['force', '力导向']]) {
      const select = frame.locator('.layout-select')
      await select.click()
      const list = frame.locator('#' + await select.getByRole('combobox').getAttribute('aria-controls'))
      await list.getByRole('option', { name: label, exact: true }).click()
      await expect(select).toHaveText(label)
      await observe('dense-layout-' + layout, reference, expected.edges, { layout, selected_label: await select.innerText() })
    }
    expect(requests).toEqual([
      { method: 'POST', path: root + '/search', body: stages[0].request },
      { method: 'POST', path: root + '/expand', body: stages[2].request }
    ])
    return { stages, requests }
  } finally { page.off('request', record) }
}

export async function stableRelationshipPixels(element, { colors, boxes, timeout = 10_000 }) {
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
    const valid = canvas.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true }) && !element.querySelector('.el-loading-mask') && samples.every(sample => sample.pixelCount >= 40)
    stable = valid && signature === previous ? stable+1 : 0
    previous = signature
    if (stable >= 10) return samples.map(sample => ({ ...sample, stableFrames: stable }))
  }
  throw new Error('Visible relationship strokes did not settle: ' + JSON.stringify(samples))
}
