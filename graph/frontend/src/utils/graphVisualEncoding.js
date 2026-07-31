export const GRAPH_CATEGORY_VARIABLES = Array.from(
  { length: 12 },
  (_, index) => `--addp-graph-category-${index + 1}`
)

export const RELATION_LINE_DASHES = [
  [],
  [8, 4],
  [2, 4],
  [10, 3, 2, 3]
]

const GRAPH_THEME_VARIABLES = {
  labelLight: '--addp-graph-label-light',
  labelDark: '--addp-graph-label-dark',
  nodeStroke: '--addp-graph-node-stroke',
  edgeDefault: '--addp-graph-edge-default',
  edgeLabel: '--addp-graph-edge-label',
  edgeLabelStroke: '--addp-graph-edge-label-stroke',
  selection: '--addp-graph-selection',
  related: '--addp-graph-related',
  searchMatch: '--addp-graph-search-match',
  path: '--addp-graph-path',
  analysisLow: '--addp-graph-analysis-low',
  analysisHigh: '--addp-graph-analysis-high'
}

export function readGraphTheme(root = document.documentElement) {
  const style = getComputedStyle(root)
  const read = variable => style.getPropertyValue(variable).trim()
  return {
    categoryColors: GRAPH_CATEGORY_VARIABLES.map(read).filter(Boolean),
    ...Object.fromEntries(Object.entries(GRAPH_THEME_VARIABLES).map(([key, variable]) => [key, read(variable)]))
  }
}

export function graphNodeTypeKey(node) {
  if (node?.entity_type) return String(node.entity_type)
  if (Array.isArray(node?.labels) && node.labels.length > 0) {
    return [...node.labels].map(String).sort().join('+')
  }
  return ''
}

export function createGraphVisualEncoding({ nodeShapes = [], relationshipShapes = [], nodes = [], edges = [], palette = [] }) {
  const nodeDefinitions = collectDefinitions(
    nodeShapes.map(shape => ({ key: shape.name, color: shape.color })),
    nodes.map(node => ({ key: graphNodeTypeKey(node), color: node.color }))
  )
  const relationshipDefinitions = collectDefinitions(
    relationshipShapes.map(shape => ({ key: shape.type, color: shape.color, directed: shape.directed })),
    edges.map(edge => ({ key: edge.relation_type || edge.type, color: edge.color, directed: edge.directed }))
  )

  const nodeColors = assignDistinctColors(nodeDefinitions, palette)
  const relationshipColors = assignDistinctColors(relationshipDefinitions, palette)
  const nodeTypes = new Map()
  const relationshipTypes = new Map()

  nodeDefinitions.forEach(definition => {
    nodeTypes.set(definition.key, { color: nodeColors.get(definition.key) })
  })
  relationshipDefinitions.forEach((definition, index) => {
    const dashIndex = index % RELATION_LINE_DASHES.length
    relationshipTypes.set(definition.key, {
      color: relationshipColors.get(definition.key),
      lineDash: RELATION_LINE_DASHES[dashIndex],
      dashIndex,
      directed: definition.directed !== false
    })
  })

  return { nodeTypes, relationshipTypes }
}

export function getContrastingTextColor(backgroundColor, lightColor, darkColor) {
  const rgb = parseHexColor(backgroundColor)
  if (!rgb) return lightColor
  const luminance = rgb
    .map(value => {
      const channel = value / 255
      return channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
    })
    .reduce((sum, value, index) => sum + value * [0.2126, 0.7152, 0.0722][index], 0)
  return luminance > 0.45 ? darkColor : lightColor
}

function collectDefinitions(primary, secondary) {
  const definitions = new Map()
  for (const candidate of [...primary, ...secondary]) {
    const key = String(candidate.key || '').trim()
    if (!key) continue
    const current = definitions.get(key) || { key, color: '', directed: undefined }
    if (!current.color && candidate.color) current.color = candidate.color
    if (current.directed === undefined && typeof candidate.directed === 'boolean') current.directed = candidate.directed
    definitions.set(key, current)
  }
  return [...definitions.values()].sort((left, right) => left.key.localeCompare(right.key))
}

function assignDistinctColors(definitions, palette) {
  const normalizedCounts = new Map()
  definitions.forEach(({ color }) => {
    const normalized = normalizeColor(color)
    if (normalized) normalizedCounts.set(normalized, (normalizedCounts.get(normalized) || 0) + 1)
  })

  const assignments = new Map()
  const used = new Set()
  definitions.forEach(definition => {
    const normalized = normalizeColor(definition.color)
    if (normalized && normalizedCounts.get(normalized) === 1) {
      assignments.set(definition.key, definition.color)
      used.add(normalized)
    }
  })

  definitions.forEach(definition => {
    if (assignments.has(definition.key)) return
    const color = choosePaletteColor(definition.key, palette, used)
    assignments.set(definition.key, color || definition.color || palette[0] || '')
    if (color) used.add(normalizeColor(color))
  })
  return assignments
}

function choosePaletteColor(key, palette, used) {
  if (palette.length === 0) return ''
  const start = stableHash(key) % palette.length
  for (let offset = 0; offset < palette.length; offset += 1) {
    const color = palette[(start + offset) % palette.length]
    if (!used.has(normalizeColor(color))) return color
  }
  return palette[start]
}

function stableHash(value) {
  let hash = 2166136261
  for (const char of String(value)) {
    hash ^= char.codePointAt(0)
    hash = Math.imul(hash, 16777619)
  }
  return hash >>> 0
}

function normalizeColor(color) {
  return String(color || '').trim().toLowerCase()
}

function parseHexColor(color) {
  const match = String(color || '').trim().match(/^#([\da-f]{3}|[\da-f]{6})$/i)
  if (!match) return null
  const value = match[1].length === 3
    ? match[1].split('').map(char => char + char).join('')
    : match[1]
  return [0, 2, 4].map(index => Number.parseInt(value.slice(index, index + 2), 16))
}
