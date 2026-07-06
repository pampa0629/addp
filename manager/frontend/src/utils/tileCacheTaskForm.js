export function createDefaultTileCacheTaskForm() {
  return {
    name: '',
    description: '',
    enabled: true,
    schedule: '',
    config: {
      target: { item_id: undefined, item_fingerprint: '', locator: '', source_engine_id: undefined, source_kind: '', full_name: '', schema: '', table: '' },
      tile: {
        format: 'mvt',
        tile_matrix_set: 'WebMercatorQuad',
        min_zoom: 0,
        max_zoom: 18,
        source_srid: 0,
        target_srid: 3857,
        extent_srid: 0,
        extent: []
      },
      storage: {},
      options: { geometry_column: '' }
    }
  }
}

export function createTileCacheTaskFormFromTask(task = null) {
  const next = createDefaultTileCacheTaskForm()
  if (!task) return next

  next.name = task.name || ''
  next.description = task.description || ''
  next.enabled = task.enabled !== false
  next.schedule = task.schedule || ''
  next.config = {
    ...next.config,
    ...(task.config || {}),
    target: { ...next.config.target, ...(task.config?.target || {}) },
    tile: { ...next.config.tile, ...(task.config?.tile || {}) },
    storage: { ...next.config.storage, ...(task.config?.storage || {}) },
    options: { ...next.config.options, ...(task.config?.options || {}) }
  }
  return next
}

export function createTileCacheTaskPayload(form) {
  const payload = JSON.parse(JSON.stringify(form))
  payload.name = String(payload.name || '').trim()
  payload.description = String(payload.description || '').trim()
  payload.schedule = String(payload.schedule || '').trim()
  payload.config.tile.format = 'mvt'
  payload.config.tile.target_srid = 3857
  if (!payload.config.target.item_id) {
    delete payload.config.target.item_id
  }
  if (!payload.config.target.item_fingerprint) {
    delete payload.config.target.item_fingerprint
  }
  if (!payload.config.target.locator) {
    delete payload.config.target.locator
  }
  delete payload.config.target.label
  delete payload.config.target.engine_name
  if (!payload.config.target.source_kind) {
    delete payload.config.target.source_kind
  }
  if (!payload.config.target.full_name) {
    delete payload.config.target.full_name
  }
  if (!payload.config.options.geometry_column) {
    delete payload.config.options.geometry_column
  }
  if (!payload.config.tile.source_srid) {
    delete payload.config.tile.source_srid
  }
  if (!payload.config.tile.extent_srid) {
    delete payload.config.tile.extent_srid
  }
  if (!Array.isArray(payload.config.tile.extent) || payload.config.tile.extent.length !== 4) {
    delete payload.config.tile.extent
  }
  return payload
}
