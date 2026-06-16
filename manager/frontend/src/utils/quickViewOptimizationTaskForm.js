export function createDefaultQuickViewOptimizationTaskForm() {
  return {
    name: '',
    description: '',
    enabled: true,
    schedule: '',
    config: {
      target: {
        item_id: undefined,
        item_fingerprint: '',
        locator: '',
        source_engine_id: undefined,
        schema: '',
        table: ''
      },
      geometry: {
        geometry_column: '',
        source_srid: 0,
        target_srid: 3857
      },
      optimization: {
        target_kind: 'source_schema_materialized_view',
        include_source_key: true,
        attributes: [],
        analyze_after_build: true
      },
      storage: {
        target_schema: ''
      }
    }
  }
}

export function createQuickViewOptimizationTaskFormFromTask(task = null) {
  const next = createDefaultQuickViewOptimizationTaskForm()
  if (!task) return next

  const config = task.config || {}
  next.name = task.name || ''
  next.description = task.description || ''
  next.enabled = task.enabled !== false
  next.schedule = task.schedule || ''
  next.config = {
    ...next.config,
    ...config,
    target: { ...next.config.target, ...(config.target || {}) },
    geometry: { ...next.config.geometry, ...(config.geometry || {}) },
    optimization: { ...next.config.optimization, ...(config.optimization || {}) },
    storage: { ...next.config.storage, ...(config.storage || {}) }
  }
  return next
}

export function createQuickViewOptimizationTaskPayload(form) {
  const payload = JSON.parse(JSON.stringify(form))
  payload.name = String(payload.name || '').trim()
  payload.description = String(payload.description || '').trim()
  payload.schedule = ''
  payload.enabled = payload.enabled !== false
  payload.config.geometry.target_srid = 3857
  payload.config.optimization.target_kind = 'source_schema_materialized_view'
  payload.config.optimization.include_source_key = true
  payload.config.optimization.analyze_after_build = payload.config.optimization.analyze_after_build !== false
  if (!Array.isArray(payload.config.optimization.attributes)) {
    payload.config.optimization.attributes = []
  }
  payload.config.optimization.attributes = payload.config.optimization.attributes
    .map((item) => String(item || '').trim())
    .filter(Boolean)
  if (!payload.config.storage.target_schema) {
    payload.config.storage.target_schema = payload.config.target.schema
  }
  if (!payload.config.target.item_id) {
    delete payload.config.target.item_id
  }
  if (!payload.config.target.item_fingerprint) {
    delete payload.config.target.item_fingerprint
  }
  delete payload.config.target.label
  delete payload.config.target.engine_name
  return payload
}
