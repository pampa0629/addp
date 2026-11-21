import { cleanupOwners } from './cleanupObservation.mjs'

const hasValue = value => value !== null && value !== undefined && String(value).trim() !== ''

export function buildMonitorExecutionRoute(executionId) {
  if (!hasValue(executionId)) return ''
  return `/monitor/executions?execution_id=${encodeURIComponent(String(executionId))}`
}

// Types served by each Owner's human execution detail endpoint.
const resultTaskTypes = {
  develop: ['query', 'workflow', 'script'],
  transfer: ['sync'],
  security: ['sensitive_data_discovery'],
  graph: ['kg_build'],
  quality: ['quality_plan'],
  meta: ['scan'],
  orchestrator: ['orchestration'],
  manager: [
    'embedding', 'image_segmentation', 'vector_tile_cache_generation', 'vector_tile_set_generation',
    'vector_materialized_view_generation', 'raster_cog_generation', 'raster_mosaic_generation',
    'model_3d_glb_generation', 'model3d_tiles_generation', 'gaussian_splat_ksplat_generation',
    'point_cloud_copc_generation', 'pptx_pdf_generation'
  ]
}

export function buildExecutionResultRoute(execution, returnTo) {
  if (!execution?.execution_id) return ''
  const { module, task_type: taskType, execution_id: executionId } = execution
  let route
  if (taskType === 'cleanup_executor' && execution.source === 'system' && cleanupOwners.includes(module) && hasValue(execution.parent_execution_id)) {
    route = `/system/executions/${encodeURIComponent(execution.parent_execution_id)}/children/${encodeURIComponent(executionId)}`
  } else if ((module === 'system' && taskType === 'cleanup' && execution.source === 'system') ||
    (Object.hasOwn(resultTaskTypes, module) && resultTaskTypes[module].includes(taskType))) {
    route = `/${module}/executions/${encodeURIComponent(executionId)}`
  } else {
    return ''
  }
  if (returnTo === undefined) return route
  const back = resolveMonitorExecutionReturnRoute(executionId, returnTo)
  return back ? route + '?' + new URLSearchParams({ return_to: back }) : ''
}

// A result page can return only to this execution's canonical Monitor context.
export function resolveMonitorExecutionReturnRoute(executionId, returnTo) {
  if (!hasValue(executionId)) return ''
  if (returnTo === undefined) return buildMonitorExecutionRoute(executionId)
  if (typeof returnTo !== 'string' || !returnTo.startsWith('/monitor/executions?')) return ''
  const url = new URL(returnTo, 'http://console.invalid')
  const allowed = new Set(['module', 'task_type', 'source_task_id', 'source', 'status', 'trigger_type', 'page', 'page_size', 'execution_id'])
  const keys = [...url.searchParams.keys()]
  if (url.pathname !== '/monitor/executions' || url.hash || new Set(keys).size !== keys.length || keys.some(key => !allowed.has(key)) || url.searchParams.get('execution_id') !== String(executionId)) return ''
  return url.pathname + url.search
}
