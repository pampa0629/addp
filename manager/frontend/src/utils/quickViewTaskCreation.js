const DEFINITIONS = [
  ['generate_vector_materialized_view', 'vector_materialized_view_generation', 'manager.quickViewCreator.actions.vectorMaterializedView'],
  ['generate_vector_tile_cache', 'vector_tile_cache_generation', 'manager.quickViewCreator.actions.vectorTileCache'],
  ['generate_raster_cog', 'raster_cog_generation', 'manager.quickViewCreator.actions.rasterCOG'],
  ['generate_model_3d_glb', 'model_3d_glb_generation', 'manager.quickViewCreator.actions.model3DGLB'],
  ['generate_model3d_3d_tiles', 'model3d_tiles_generation', 'manager.quickViewCreator.actions.model3D3DTiles'],
  ['generate_model3d_s3m', 'model3d_tiles_generation', 'manager.quickViewCreator.actions.model3DS3M'],
  ['generate_gaussian_splat_ksplat', 'gaussian_splat_ksplat_generation', 'manager.quickViewCreator.actions.gaussianSplat'],
  ['generate_point_cloud_copc', 'point_cloud_copc_generation', 'manager.quickViewCreator.actions.pointCloudCOPC']
].map(([action, taskType, labelKey]) => ({ action, taskType, labelKey }))

export const PPTX_PDF_GENERATION_ACTION = 'generate_pptx_pdf'

export function quickViewTaskTypeForAction(action) {
  if (action === PPTX_PDF_GENERATION_ACTION) return 'pptx_pdf_generation'
  return DEFINITIONS.find(definition => definition.action === action)?.taskType || ''
}

export function generationOptionsForCapability(capability = {}, taskType = '') {
  const available = new Set(Array.isArray(capability?.available_actions) ? capability.available_actions : [])
  return DEFINITIONS.filter(definition => (
    available.has(definition.action) && (!taskType || definition.taskType === taskType)
  ))
}

export function pptxGenerationOptions(taskType = '') {
  if (taskType && taskType !== 'pptx_pdf_generation') return []
  return [{
    action: PPTX_PDF_GENERATION_ACTION,
    taskType: 'pptx_pdf_generation',
    labelKey: 'manager.quickViewCreator.actions.pptxPDF'
  }]
}

export function isPPTXGenerationSource(selection) {
  const format = String(selection?.resource?.format || selection?.raw?.node?.metadata?.format || '').trim().toLowerCase()
  if (format === 'pptx') return true
  const locator = String(selection?.identity?.locator || '').split('?')[0].toLowerCase()
  return locator.endsWith('.pptx')
}
