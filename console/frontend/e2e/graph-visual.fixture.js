import { expect } from '@playwright/test'

// Shared browser observations for synthetic T3 and native Hosted production builds.
// Uses only Canvas pixels and DOM; never Vue/G6 debug objects or product test hooks.
export async function stableGraphPixels(element, { count, color: colorCSS, timeout = 10_000 }) {
  const canvas = element.querySelector('canvas')
  if (!canvas || !Number.isInteger(count) || count <= 0) throw new Error('Invalid search canvas/count')
  const copy = document.createElement('canvas')
  const context = copy.getContext('2d', { willReadFrequently: true })
  const colorProbe = document.createElement('canvas').getContext('2d', { willReadFrequently: true })
  if (!colorCSS || !CSS.supports('color', colorCSS)) throw new Error('Missing Graph observation color')
  colorProbe.fillStyle = colorCSS
  colorProbe.fillRect(0, 0, 1, 1)
  const color = [...colorProbe.getImageData(0, 0, 1, 1).data].slice(0, 3)
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
    const mask = new Uint8Array(width * height), queue = new Int32Array(width * height)
    for (let i = 0; i < mask.length; i++) {
      const offset = i * 4
      mask[i] = pixels[offset + 3] > 200 && color.every((value, channel) => Math.abs(pixels[offset + channel] - value) <= 12) ? 1 : 0
    }
    const nodes = []
    for (let i = 0; i < mask.length; i++) {
      if (!mask[i]) continue
      let head = 0, tail = 1, minX = width, minY = height, maxX = 0, maxY = 0
      queue[0] = i; mask[i] = 0
      while (head < tail) {
        const index = queue[head++], x = index % width, y = Math.floor(index / width)
        minX = Math.min(minX, x); maxX = Math.max(maxX, x); minY = Math.min(minY, y); maxY = Math.max(maxY, y)
        for (let dy = -1; dy <= 1; dy++) for (let dx = -1; dx <= 1; dx++) {
          const nx = x + dx, ny = y + dy, next = ny * width + nx
          if (nx >= 0 && nx < width && ny >= 0 && ny < height && mask[next]) { mask[next] = 0; queue[tail++] = next }
        }
      }
      const sx = rect.width / width, sy = rect.height / height
      const w = (maxX - minX + 1) * sx, h = (maxY - minY + 1) * sy
      if (tail >= 40 && w >= 10 && h >= 10 && w / h > 0.7 && w / h < 1.4) {
        nodes.push({ x: (minX + maxX) * sx / 2, y: (minY + maxY) * sy / 2,
          box: [minX * sx, minY * sy, maxX * sx, maxY * sy], pixelCount: tail })
      }
    }
    nodes.sort((a, b) => a.x - b.x || a.y - b.y)
    const valid = nodes.length === count && nodes.every(node => node.box[0] > 1 && node.box[1] > 1 &&
      node.box[2] < rect.width - 1 && node.box[3] < rect.height - 1)
    const signature = JSON.stringify(nodes)
    stable = valid && signature === previous ? stable + 1 : 0
    previous = signature; last = nodes
    if (stable >= 10) return { width: rect.width, height: rect.height, color, nodes, stableFrames: stable }
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
    const stable = await canvas.evaluate(stableGraphPixels, { count: expectedNodes.length, color })
    const hits = await graphNodeHits(frame, canvas, stable, expectedNodes)
    stages.push({ phase, ...extra, native_ids: Object.fromEntries(expectedNodes.map(node => [node.properties.id, node.id])),
      edges: normalizeEdges(expectedEdges), frame: stable, hits })
    await capture(phase)
    return hits
  }
  try {
    let currentNodes = nodes, currentEdges = edges
    const stable = await canvas.evaluate(stableGraphPixels, { count: nodes.length, color })
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
