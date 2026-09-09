const DEFINITIONS = [
  ['generate_vector_materialized_view', 'vector_materialized_view_generation', 'manager.quickViewCreator.actions.vectorMaterializedView'],
  ['generate_vector_tile_cache', 'vector_tile_cache_generation', 'manager.quickViewCreator.actions.vectorTileCache'],
  ['generate_raster_cog', 'raster_cog_generation', 'manager.quickViewCreator.actions.rasterCOG'],
  ['generate_model_3d_glb', 'model_3d_glb_generation', 'manager.quickViewCreator.actions.model3DGLB'],
  ['generate_model3d_3d_tiles', 'model3d_tiles_generation', 'manager.quickViewCreator.actions.model3D3DTiles'],
  ['generate_model3d_s3m', 'model3d_tiles_generation', 'manager.quickViewCreator.actions.model3DS3M'],
  ['generate_gaussian_splat_ksplat', 'gaussian_splat_ksplat_generation', 'manager.quickViewCreator.actions.gaussianSplat'],
  ['generate_point_cloud_copc', 'point_cloud_copc_generation', 'manager.quickViewCreator.actions.pointCloudCOPC'],
  ['generate_pptx_pdf', 'pptx_pdf_generation', 'manager.quickViewCreator.actions.pptxPDF']
].map(([action, taskType, labelKey]) => ({ action, taskType, labelKey }))

export function quickViewTaskTypeForAction(action) {
  return DEFINITIONS.find(definition => definition.action === action)?.taskType || ''
}

export function generationOptionsForCapability(capability = {}, taskType = '') {
  const available = new Set(Array.isArray(capability?.available_actions) ? capability.available_actions : [])
  return DEFINITIONS.filter(definition => (
    available.has(definition.action) && (!taskType || definition.taskType === taskType)
  ))
}

function hasResultID(value) {
  return Number(value) > 0
}

const CURRENT_RESULT_CHECKS = {
  vector_materialized_view_generation: capability => (
    capability?.optimization?.available === true && hasResultID(capability?.optimization?.result_id)
  ),
  vector_tile_cache_generation: capability => hasResultID(capability?.default_vector_tile_cache_id),
  raster_cog_generation: capability => capability?.render_source === 'client_cog_render',
  model_3d_glb_generation: capability => hasResultID(capability?.model_3d?.result_id),
  model3d_tiles_generation: capability => (
    Array.isArray(capability?.model3d_tiles?.formats)
    && capability.model3d_tiles.formats.some(format => (
      format?.status === 'ready' && hasResultID(format?.result_id)
    ))
  ),
  gaussian_splat_ksplat_generation: capability => hasResultID(capability?.gaussian_splat?.result_id),
  point_cloud_copc_generation: capability => hasResultID(capability?.point_cloud?.result_id),
  pptx_pdf_generation: capability => (
    capability?.pptx_pdf?.status === 'ready' && hasResultID(capability?.pptx_pdf?.result_id)
  )
}

export function quickViewCreationEmptyReason(capability = {}, taskType = '') {
  return currentQuickViewResultTaskType(capability, taskType) ? 'currentResult' : 'unsupported'
}

export function currentQuickViewResultTaskType(capability = {}, taskType = '') {
  const candidates = taskType ? [taskType] : Object.keys(CURRENT_RESULT_CHECKS)
  return candidates.find(candidate => CURRENT_RESULT_CHECKS[candidate]?.(capability)) || ''
}
