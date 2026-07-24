const DEFAULT_HISTORY_LIMIT = 50
const DEFAULT_MERGE_WINDOW = 400
const DEFAULT_PASTE_OFFSET = 24
export const DAG_MIN_ZOOM = 0.5
export const DAG_MAX_ZOOM = 1.5

export function cloneDAGValue(value) {
  if (value === undefined) return undefined
  if (typeof structuredClone === 'function') {
    try {
      return structuredClone(value)
    } catch {
      // G6 模型可能包含函数等不可结构化克隆的绘制配置，历史快照只保留可序列化 DAG 数据。
    }
  }
  return JSON.parse(JSON.stringify(value))
}

export function createDAGHistoryStore({
  limit = DEFAULT_HISTORY_LIMIT,
  mergeWindow = DEFAULT_MERGE_WINDOW
} = {}) {
  let entries = []
  let index = -1
  let lastMergeKey = null
  let lastRecordedAt = 0

  function reset(snapshot) {
    entries = snapshot === undefined ? [] : [cloneDAGValue(snapshot)]
    index = entries.length - 1
    clearMergeState()
  }

  function record(snapshot, { mergeKey = null, now = Date.now() } = {}) {
    const next = cloneDAGValue(snapshot)
    if (index >= 0 && snapshotsEqual(entries[index], next)) return false

    entries = entries.slice(0, index + 1)
    const shouldMerge = Boolean(
      mergeKey &&
      mergeKey === lastMergeKey &&
      now - lastRecordedAt <= mergeWindow &&
      index > 0
    )

    if (shouldMerge) {
      entries[index] = next
    } else {
      entries.push(next)
      if (entries.length > limit) entries.shift()
      index = entries.length - 1
    }

    lastMergeKey = mergeKey
    lastRecordedAt = now
    return true
  }

  function undo() {
    if (!canUndo()) return null
    index -= 1
    clearMergeState()
    return cloneDAGValue(entries[index])
  }

  function redo() {
    if (!canRedo()) return null
    index += 1
    clearMergeState()
    return cloneDAGValue(entries[index])
  }

  function canUndo() {
    return index > 0
  }

  function canRedo() {
    return index >= 0 && index < entries.length - 1
  }

  function clearMergeState() {
    lastMergeKey = null
    lastRecordedAt = 0
  }

  return {
    reset,
    record,
    undo,
    redo,
    canUndo,
    canRedo,
    size: () => entries.length,
    current: () => index >= 0 ? cloneDAGValue(entries[index]) : null
  }
}

export function cloneDAGNodeForPaste(nodeModel, {
  id,
  offset = DEFAULT_PASTE_OFFSET,
  position = null
} = {}) {
  if (!nodeModel || !id) return null
  const node = cloneDAGValue(nodeModel)
  node.id = id
  node.x = finiteNumber(position?.x, finiteNumber(node.x, 0) + offset)
  node.y = finiteNumber(position?.y, finiteNumber(node.y, 0) + offset)
  return node
}

export function normalizeDAGEditorLayout(layout) {
  const normalized = {
    nodes: {},
    viewport: {
      zoom: 1,
      translate_x: 0,
      translate_y: 0
    }
  }

  for (const [nodeId, position] of Object.entries(layout?.nodes || {})) {
    if (!nodeId || !Number.isFinite(Number(position?.x)) || !Number.isFinite(Number(position?.y))) continue
    normalized.nodes[nodeId] = {
      x: Number(position.x),
      y: Number(position.y)
    }
  }

  const viewport = layout?.viewport || {}
  normalized.viewport.zoom = clampDAGZoom(viewport.zoom)
  normalized.viewport.translate_x = finiteNumber(viewport.translate_x, 0)
  normalized.viewport.translate_y = finiteNumber(viewport.translate_y, 0)
  return normalized
}

export function clampDAGZoom(value, fallback = 1) {
  const zoom = positiveNumber(value, fallback)
  return Math.min(DAG_MAX_ZOOM, Math.max(DAG_MIN_ZOOM, zoom))
}

export function calculateDAGFitViewport({ width, height, bbox, padding = 0 } = {}) {
  const canvasWidth = finiteNumber(width, 0)
  const canvasHeight = finiteNumber(height, 0)
  const contentWidth = finiteNumber(bbox?.width, 0)
  const contentHeight = finiteNumber(bbox?.height, 0)
  if (canvasWidth <= 0 || canvasHeight <= 0 || contentWidth <= 0 || contentHeight <= 0) {
    return null
  }

  const [top, right, bottom, left] = normalizeDAGPadding(padding)
  const availableWidth = canvasWidth - left - right
  const availableHeight = canvasHeight - top - bottom
  if (availableWidth <= 0 || availableHeight <= 0) return null

  const center = {
    x: left + availableWidth / 2,
    y: top + availableHeight / 2
  }
  const contentCenter = {
    x: finiteNumber(bbox?.x, finiteNumber(bbox?.minX, 0)) + contentWidth / 2,
    y: finiteNumber(bbox?.y, finiteNumber(bbox?.minY, 0)) + contentHeight / 2
  }

  return {
    zoom: clampDAGZoom(Math.min(availableWidth / contentWidth, availableHeight / contentHeight)),
    center,
    translate: {
      x: center.x - contentCenter.x,
      y: center.y - contentCenter.y
    }
  }
}

export function captureDAGEditorLayout(graph) {
  const nodes = {}
  for (const node of graph?.save?.().nodes || []) {
    if (!node?.id || !Number.isFinite(Number(node.x)) || !Number.isFinite(Number(node.y))) continue
    nodes[node.id] = { x: Number(node.x), y: Number(node.y) }
  }

  const matrix = graph?.getGroup?.()?.getMatrix?.() || []
  return normalizeDAGEditorLayout({
    nodes,
    viewport: {
      zoom: graph?.getZoom?.() || 1,
      translate_x: matrix[6] || 0,
      translate_y: matrix[7] || 0
    }
  })
}

export function applyDAGNodePositions(nodes, layout) {
  const positions = normalizeDAGEditorLayout(layout).nodes
  return (nodes || []).map(node => {
    const position = positions[node.id]
    return position ? { ...node, ...position } : node
  })
}

export function restoreDAGViewport(graph, layout) {
  if (!graph) return
  const viewport = normalizeDAGEditorLayout(layout).viewport
  graph.zoomTo?.(viewport.zoom)

  const group = graph.getGroup?.()
  const currentMatrix = group?.getMatrix?.()
  const matrix = currentMatrix
    ? [...currentMatrix]
    : [viewport.zoom, 0, 0, 0, viewport.zoom, 0, 0, 0, 1]
  matrix[6] = viewport.translate_x
  matrix[7] = viewport.translate_y
  group?.setMatrix?.(matrix)
  graph.paint?.()
}

function snapshotsEqual(left, right) {
  return JSON.stringify(left) === JSON.stringify(right)
}

function finiteNumber(value, fallback) {
  const number = Number(value)
  return Number.isFinite(number) ? number : fallback
}

function positiveNumber(value, fallback) {
  const number = Number(value)
  return Number.isFinite(number) && number > 0 ? number : fallback
}

function normalizeDAGPadding(value) {
  if (!Array.isArray(value)) {
    const padding = Math.max(0, finiteNumber(value, 0))
    return [padding, padding, padding, padding]
  }

  const padding = value.map(item => Math.max(0, finiteNumber(item, 0)))
  if (padding.length === 1) return [padding[0], padding[0], padding[0], padding[0]]
  if (padding.length === 2) return [padding[0], padding[1], padding[0], padding[1]]
  if (padding.length === 3) return [padding[0], padding[1], padding[2], padding[1]]
  if (padding.length === 4) return padding
  return [0, 0, 0, 0]
}
