export const DEFAULT_COPC_POINT_LIMIT = 420000
export const DEFAULT_COPC_NODE_LIMIT = 96
export const DEFAULT_COPC_HIERARCHY_PAGE_LIMIT = 256
export const DEFAULT_COPC_DETAIL_POINT_BUDGET = 2200000
export const DEFAULT_COPC_OVERVIEW_NODE_LIMIT = 32
export const DEFAULT_COPC_HIERARCHY_PAGE_LOAD_LIMIT = 32

export function hierarchyDepth(key) {
  const depth = Number(String(key || '').split('-')[0])
  return Number.isFinite(depth) ? depth : Number.MAX_SAFE_INTEGER
}

export function collectHierarchyNodeEntries(subtrees) {
  return subtrees.flatMap((subtree) => Object.entries(subtree?.nodes || {})
    .filter(([, node]) => Number(node?.pointCount || 0) > 0 && Number(node?.pointDataLength || 0) > 0)
    .map(([key, node]) => ({ key, node, depth: hierarchyDepth(key) })))
}

export function collectHierarchyPageEntries(subtrees) {
  return subtrees.flatMap((subtree) => Object.entries(subtree?.pages || {})
    .filter(([, page]) => Number(page?.pageLength || 0) > 0)
    .map(([key, page]) => ({ key, page, depth: hierarchyDepth(key) })))
}

export async function loadCOPCHierarchySubtrees(Copc, getter, rootHierarchyPage, pageLimit = DEFAULT_COPC_HIERARCHY_PAGE_LIMIT) {
  const subtrees = []
  const queue = [{ key: '__root__', page: rootHierarchyPage }]
  while (queue.length && subtrees.length < pageLimit) {
    const { key, page } = queue.shift()
    if (!page) continue
    const subtree = await Copc.loadHierarchyPage(getter, page)
    subtree.pageKey = key
    subtrees.push(subtree)
    Object.entries(subtree?.pages || {}).forEach(([childKey, childPage]) => {
      if (childPage && queue.length + subtrees.length < pageLimit) {
        queue.push({ key: childKey, page: childPage })
      }
    })
  }
  return subtrees
}

export function parseHierarchyKey(key) {
  const parts = String(key || '').split('-').map((part) => Number(part))
  if (parts.length !== 4 || parts.some((part) => !Number.isFinite(part))) return null
  return { depth: parts[0], x: parts[1], y: parts[2], z: parts[3] }
}

export function hierarchyKeyAncestorOf(parentKey, childKey) {
  const parent = parseHierarchyKey(parentKey)
  const child = parseHierarchyKey(childKey)
  if (!parent || !child || parent.depth >= child.depth) return false
  const shift = child.depth - parent.depth
  return (child.x >> shift) === parent.x && (child.y >> shift) === parent.y && (child.z >> shift) === parent.z
}

export function stepBounds(bounds, xBit, yBit, zBit) {
  const midX = bounds[0] + (bounds[3] - bounds[0]) / 2
  const midY = bounds[1] + (bounds[4] - bounds[1]) / 2
  const midZ = bounds[2] + (bounds[5] - bounds[2]) / 2
  return [
    xBit ? midX : bounds[0],
    yBit ? midY : bounds[1],
    zBit ? midZ : bounds[2],
    xBit ? bounds[3] : midX,
    yBit ? bounds[4] : midY,
    zBit ? bounds[5] : midZ
  ]
}

export function hierarchyNodeBounds(cube, key) {
  const parsed = parseHierarchyKey(key)
  if (!parsed || !Array.isArray(cube) || cube.length < 6) return null
  let bounds = cube.slice(0, 6).map((value) => Number(value))
  if (bounds.some((value) => !Number.isFinite(value))) return null
  for (let bitIndex = parsed.depth - 1; bitIndex >= 0; bitIndex -= 1) {
    bounds = stepBounds(
      bounds,
      (parsed.x >> bitIndex) & 1,
      (parsed.y >> bitIndex) & 1,
      (parsed.z >> bitIndex) & 1
    )
  }
  return bounds
}

export function boundsCenter(bounds) {
  return {
    x: bounds[0] + (bounds[3] - bounds[0]) / 2,
    y: bounds[1] + (bounds[4] - bounds[1]) / 2,
    z: bounds[2] + (bounds[5] - bounds[2]) / 2
  }
}

export function boundsRadius(bounds) {
  const width = Math.max(0, bounds[3] - bounds[0])
  const depth = Math.max(0, bounds[4] - bounds[1])
  const height = Math.max(0, bounds[5] - bounds[2])
  return Math.sqrt(width * width + depth * depth + height * height) / 2
}

export function enrichCOPCNodeEntries(nodeEntries, cube) {
  return nodeEntries
    .map((entry) => {
      const bounds = hierarchyNodeBounds(cube, entry.key)
      if (!bounds) return null
      return {
        ...entry,
        bounds,
        center: boundsCenter(bounds),
        radius: boundsRadius(bounds)
      }
    })
    .filter(Boolean)
}

export function selectCOPCOverviewNodes(nodeEntries, nodeLimit = DEFAULT_COPC_OVERVIEW_NODE_LIMIT) {
  return [...nodeEntries]
    .sort((left, right) => {
      const depthDelta = left.depth - right.depth
      if (depthDelta !== 0) return depthDelta
      const countDelta = Number(right.node?.pointCount || 0) - Number(left.node?.pointCount || 0)
      if (countDelta !== 0) return countDelta
      return String(left.key).localeCompare(String(right.key))
    })
    .slice(0, nodeLimit)
}

export function selectCOPCDetailNodes(nodeEntries, view, options = {}) {
  const nodeLimit = Number(options.nodeLimit || DEFAULT_COPC_NODE_LIMIT)
  const pointBudget = Number(options.pointBudget || DEFAULT_COPC_DETAIL_POINT_BUDGET)
  const nodePointLimit = Number(options.nodePointLimit || pointBudget)
  const minNodePointLimit = Math.min(nodePointLimit, Number(options.minNodePointLimit || 24000))
  const minProjectedPixels = Number(options.minProjectedPixels || 0.35)
  const viewportHeight = Math.max(Number(view?.viewportHeight || 1), 1)
  const fov = Math.max(Number(view?.fov || 45), 1)
  const camera = view?.camera || { x: 0, y: 0, z: 0 }
  const target = view?.target || camera
  const pixelsPerWorldAtUnit = viewportHeight / (2 * Math.tan((fov * Math.PI) / 360))

  let usedPoints = 0
  return [...nodeEntries]
    .map((entry) => {
      const center = entry.center || boundsCenter(entry.bounds)
      const cameraDistance = distance3(camera, center)
      const targetDistance = distance3(target, center)
      const distance = Math.max(Math.min(cameraDistance, targetDistance * 1.6), 1)
      const projectedPixels = (Math.max(entry.radius || 1, 1) / distance) * pixelsPerWorldAtUnit
      const cappedProjectedPixels = Math.min(projectedPixels, 260)
      const depthWeight = 1 + Math.min(Number(entry.depth || 0), 22) * 0.72
      const focusWeight = 1 + 2.4 / (1 + targetDistance / Math.max((entry.radius || 1) * 2, 1))
      const renderPointRatio = Math.max(0.3, Math.min(1, projectedPixels / 180))
      const renderPointLimit = Math.min(
        nodePointLimit,
        Math.max(minNodePointLimit, Math.round(nodePointLimit * renderPointRatio))
      )
      const score = cappedProjectedPixels * depthWeight * focusWeight
      return {
        ...entry,
        score,
        projectedPixels,
        renderPointLimit,
        estimatedRenderPoints: Math.min(Number(entry.node?.pointCount || 0), renderPointLimit)
      }
    })
    .filter((entry) => entry.projectedPixels >= minProjectedPixels)
    .sort((left, right) => {
      const scoreDelta = right.score - left.score
      if (scoreDelta !== 0) return scoreDelta
      return right.depth - left.depth
    })
    .filter((entry) => {
      if (usedPoints >= pointBudget) return false
      usedPoints += Number(entry.estimatedRenderPoints || entry.node?.pointCount || 0)
      return true
    })
    .slice(0, nodeLimit)
}

export function selectCOPCCoverageNodes(nodeEntries, options = {}) {
  const nodeLimit = Number(options.nodeLimit || 48)
  const minDepth = Number(options.minDepth || 5)
  const maxDepth = Number(options.maxDepth || Number.MAX_SAFE_INTEGER)
  return [...(nodeEntries || [])]
    .filter((entry) => Number(entry?.depth || 0) >= minDepth && Number(entry?.depth || 0) <= maxDepth)
    .sort((left, right) => {
      const depthDelta = Number(right.depth || 0) - Number(left.depth || 0)
      if (depthDelta !== 0) return depthDelta
      const countDelta = Number(right.node?.pointCount || 0) - Number(left.node?.pointCount || 0)
      if (countDelta !== 0) return countDelta
      return String(left.key).localeCompare(String(right.key))
    })
    .slice(0, nodeLimit)
}

export function selectCOPCHierarchyPages(pageEntries, view, options = {}) {
  const pageLimit = Number(options.pageLimit || DEFAULT_COPC_HIERARCHY_PAGE_LOAD_LIMIT)
  const minProjectedPixels = Number(options.minProjectedPixels || 18)
  return scoreCOPCEntriesForView(pageEntries || [], view)
    .filter((entry) => entry.projectedPixels >= minProjectedPixels)
    .sort((left, right) => {
      const scoreDelta = right.score - left.score
      if (scoreDelta !== 0) return scoreDelta
      return right.depth - left.depth
    })
    .slice(0, pageLimit)
}

export function mergeCOPCNodeSelections(...groups) {
  const seen = new Set()
  return groups.flat().filter((entry) => {
    if (!entry || seen.has(entry.key)) return false
    seen.add(entry.key)
    return true
  })
}

export function suppressAncestorCOPCNodes(overviewEntries, detailEntries) {
  const details = detailEntries || []
  return (overviewEntries || []).filter((overview) => {
    return !details.some((detail) => hierarchyKeyAncestorOf(overview.key, detail.key))
  })
}

export function pointMaterialSize(boundsSize, pointCount) {
  const maxDim = Math.max(Number(boundsSize?.x || 0), Number(boundsSize?.y || 0), Number(boundsSize?.z || 0), 1)
  const count = Math.max(Number(pointCount || 0), 1)
  const spacingEstimate = maxDim / Math.sqrt(count)
  return Math.max(0.8, Math.min(2.2, spacingEstimate * 0.08))
}

function scoreCOPCEntriesForView(entries, view) {
  const viewportHeight = Math.max(Number(view?.viewportHeight || 1), 1)
  const fov = Math.max(Number(view?.fov || 45), 1)
  const camera = view?.camera || { x: 0, y: 0, z: 0 }
  const target = view?.target || camera
  const pixelsPerWorldAtUnit = viewportHeight / (2 * Math.tan((fov * Math.PI) / 360))
  return [...entries]
    .map((entry) => {
      const center = entry.center || boundsCenter(entry.bounds)
      const cameraDistance = distance3(camera, center)
      const targetDistance = distance3(target, center)
      const distance = Math.max(Math.min(cameraDistance, targetDistance * 1.6), 1)
      const projectedPixels = (Math.max(entry.radius || 1, 1) / distance) * pixelsPerWorldAtUnit
      const depthWeight = 1 + Math.min(Number(entry.depth || 0), 16) * 0.18
      return {
        ...entry,
        score: projectedPixels * depthWeight,
        projectedPixels
      }
    })
}

function distance3(left, right) {
  const dx = Number(left?.x || 0) - Number(right?.x || 0)
  const dy = Number(left?.y || 0) - Number(right?.y || 0)
  const dz = Number(left?.z || 0) - Number(right?.z || 0)
  return Math.sqrt(dx * dx + dy * dy + dz * dz)
}
