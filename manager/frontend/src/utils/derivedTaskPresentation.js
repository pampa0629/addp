const targetBackedSourceTypes = new Set([
  'vector_materialized_view_generation',
  'vector_tile_cache_generation',
  'raster_cog_generation'
])

const spatialBusinessTypes = new Set([
  'vector_tile_set_generation',
  'raster_mosaic_generation'
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

export function derivedTaskTargetCatalogIdentity(task, parseLocator) {
  if (!spatialBusinessTypes.has(task?.task_type)) return null
  const target = derivedTaskTarget(task)
  const engineId = derivedTaskTargetEngineID(task)
  const storageLocator = String(target.storage_locator || '').trim()
  if (!engineId || !storageLocator || typeof parseLocator !== 'function') return null

  const parsed = parseLocator(storageLocator)
  const path = Array.isArray(parsed?.path) ? parsed.path.filter(Boolean).map(String) : []
  if (path.length === 0) return null

  if (task.task_type === 'vector_tile_set_generation') {
    const name = String(target.name || '').trim().replace(/^\/+|\/+$/g, '')
    if (!name) return null
    path.push(name)
  } else {
    const placement = objectValue(objectValue(task?.config).placement)
    if (String(placement.mode || '').trim() === 'detached') {
      const datasetName = String(target.dataset_name || '').trim().replace(/^\/+|\/+$/g, '')
      if (!datasetName) return null
      path.push(datasetName)
    }
  }

  return { engineId, catalogPath: path.join('/') }
}

export function metaItemLocator(item, buildLocator) {
  const engineId = Number(item?.engine_id || 0)
  const itemId = Number(item?.id || 0)
  const itemType = String(item?.item_type || '').trim()
  const path = String(item?.full_name || '').split('/').filter(Boolean)
  if (!engineId || !itemId || !itemType || path.length === 0 || typeof buildLocator !== 'function') return ''
  return buildLocator({ engineId, path, type: itemType, itemId })
}

export function derivedTaskLocatorLabel(locator, parseLocator) {
  if (!locator) return ''
  const parsed = parseLocator(locator)
  return Array.isArray(parsed?.path) ? parsed.path.filter(Boolean).join(' / ') : ''
}
