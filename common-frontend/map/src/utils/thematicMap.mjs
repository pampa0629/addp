const MAX_CATEGORIES = 8
const CONTINUOUS_STEPS = 5

export const THEMATIC_MODES = new Set(['uniform', 'categorical', 'continuous'])
export const THEMATIC_PALETTES = new Set(['primary', 'success', 'warning', 'danger'])

export function buildThematicContext(features, style = {}) {
  const mode = style.mode || 'uniform'
  if (!THEMATIC_MODES.has(mode) || !THEMATIC_PALETTES.has(style.palette || 'primary')) {
    return { valid: false, reason: 'invalid_config' }
  }
  const palette = style.palette || 'primary'
  if (mode === 'uniform') return { valid: true, reason: '', mode, palette, entries: [] }
  if (!style.field) return { valid: false, reason: 'invalid_config' }
  if (!Array.isArray(features) || features.length === 0) return { valid: true, reason: '', mode, field: style.field, palette, entries: [] }

  const values = features.map((feature) => feature?.properties?.[style.field])
  if (mode === 'categorical') {
    if (values.some((value) => value === null || value === undefined || typeof value === 'object')) {
      return { valid: false, reason: 'invalid_result' }
    }
    const categories = [...new Set(values.map(String))].sort((left, right) => left.localeCompare(right))
    if (categories.length > MAX_CATEGORIES) return { valid: false, reason: 'category_limit' }
    return {
      valid: true,
      reason: '',
      mode,
      field: style.field,
      palette,
      categories,
      entries: categories.map((label, index) => ({ label, index, count: categories.length })),
    }
  }

  if (values.some((value) => value === null || value === undefined || value === '')) return { valid: false, reason: 'invalid_measure' }
  const numbers = values.map(Number)
  if (numbers.some((value) => !Number.isFinite(value))) return { valid: false, reason: 'invalid_measure' }
  const minimum = Math.min(...numbers)
  const maximum = Math.max(...numbers)
  const step = maximum === minimum ? 0 : (maximum - minimum) / CONTINUOUS_STEPS
  const entries = Array.from({ length: step === 0 ? 1 : CONTINUOUS_STEPS }, (_, index) => {
    const lower = minimum + step * index
    const upper = index === CONTINUOUS_STEPS - 1 ? maximum : minimum + step * (index + 1)
    return { label: formatRange(lower, upper), index, count: step === 0 ? 1 : CONTINUOUS_STEPS }
  })
  return { valid: true, reason: '', mode, field: style.field, palette, minimum, maximum, step, entries }
}

export function thematicIndexForValue(value, context) {
  if (!context?.valid || context.mode === 'uniform') return 0
  if (context.mode === 'categorical') return Math.max(0, context.categories.indexOf(String(value)))
  if (context.step === 0) return 0
  return Math.min(CONTINUOUS_STEPS - 1, Math.max(0, Math.floor((Number(value) - context.minimum) / context.step)))
}

export function thematicColorVariable(index, count, palette = 'primary') {
  const normalizedCount = Math.max(1, count)
  const lightness = normalizedCount === 1 ? 3 : Math.round(8 - (Math.min(index, normalizedCount - 1) * 6) / (normalizedCount - 1))
  return `--el-color-${THEMATIC_PALETTES.has(palette) ? palette : 'primary'}-light-${lightness}`
}

function formatRange(lower, upper) {
  const format = (value) => Number(value.toPrecision(4)).toString()
  return lower === upper ? format(lower) : `${format(lower)} – ${format(upper)}`
}
