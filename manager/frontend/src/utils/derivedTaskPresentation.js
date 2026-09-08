const targetBackedSourceTypes = new Set([
  'vector_materialized_view_generation',
  'vector_tile_cache_generation',
  'raster_cog_generation'
])

const objectValue = value => value && typeof value === 'object' && !Array.isArray(value) ? value : {}

export function derivedTaskSource(task) {
  const config = objectValue(task?.config)
  if (targetBackedSourceTypes.has(task?.task_type)) return objectValue(config.target)
  return objectValue(config.source)
}

export function derivedTaskTarget(task) {
  const config = objectValue(task?.config)
  if (targetBackedSourceTypes.has(task?.task_type)) return {}
  return objectValue(config.target)
}

export function derivedTaskSourceLocator(task) {
  const source = derivedTaskSource(task)
  return String(source.item_locator || source.node_locator || source.locator || '').trim()
}

export function derivedTaskSourceEngineID(task) {
  const source = derivedTaskSource(task)
  return Number(source.source_engine_id || source.engine_id || 0)
}

export function derivedTaskTargetEngineID(task) {
  const target = derivedTaskTarget(task)
  return Number(target.target_engine_id || target.engine_id || 0)
}

export function derivedTaskSourceFormat(task) {
  const source = derivedTaskSource(task)
  return String(source.format || source.source_format || '').trim()
}

export function derivedTaskSourceSize(task) {
  const source = derivedTaskSource(task)
  const value = Number(source.source_size_bytes || source.size_bytes)
  return Number.isFinite(value) && value >= 0 ? value : null
}

export function derivedTaskResultName(task) {
  const config = objectValue(task?.config)
  const result = objectValue(config.result)
  const target = derivedTaskTarget(task)
  return String(result.file_name || target.file_name || target.name || target.target_name || '').trim()
}

export function derivedTaskLocatorLabel(locator, parseLocator) {
  if (!locator) return ''
  const parsed = parseLocator(locator)
  return Array.isArray(parsed?.path) ? parsed.path.filter(Boolean).join(' / ') : ''
}
