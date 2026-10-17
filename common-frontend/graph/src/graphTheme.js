export const GRAPH_CATEGORY_VARIABLES = Array.from(
  { length: 12 },
  (_, index) => `--addp-graph-category-${index + 1}`
)

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

export function getContrastingTextColor(backgroundColor, lightColor, darkColor) {
  const rgb = parseHexColor(backgroundColor)
  if (!rgb) return lightColor
  const light = parseHexColor(lightColor)
  const dark = parseHexColor(darkColor)
  if (!light || !dark) return lightColor
  const background = relativeLuminance(rgb)
  const contrast = color => {
    const foreground = relativeLuminance(color)
    return (Math.max(background, foreground) + 0.05) / (Math.min(background, foreground) + 0.05)
  }
  return contrast(dark) > contrast(light) ? darkColor : lightColor
}

function relativeLuminance(rgb) {
  return rgb
    .map(value => {
      const channel = value / 255
      return channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
    })
    .reduce((sum, value, index) => sum + value * [0.2126, 0.7152, 0.0722][index], 0)
}

function parseHexColor(color) {
  const match = String(color || '').trim().match(/^#([\da-f]{3}|[\da-f]{6})$/i)
  if (!match) return null
  const value = match[1].length === 3
    ? match[1].split('').map(char => char + char).join('')
    : match[1]
  return [0, 2, 4].map(index => Number.parseInt(value.slice(index, index + 2), 16))
}
