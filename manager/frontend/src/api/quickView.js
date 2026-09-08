import request from './client'

export const quickViewAPI = {
  getPreviewStateByLocator(locator) {
    return request.get('/manager/preview-state', { params: { locator } })
  },

  getQuickViewCapabilityByLocator(locator) {
    return request.get('/manager/quick-view/capability', {
      params: { locator }
    })
  },

  executeQuickViewAction(locator, action, payload = {}) {
    return request.post('/manager/quick-view/actions', { locator, action, ...payload })
  },

  ensurePPTXPDFPreview(locator, payload = {}) {
    return request.post('/manager/pptx_pdf/preview', { locator, ...payload })
  },

  updatePreferredModeByLocator(locator, preferredMode) {
    return request.patch(
      '/manager/preview-state/preferred-mode',
      { locator, preferred_mode: preferredMode }
    )
  },

  updateViewStateByLocator(locator, viewState) {
    return request.patch(
      '/manager/preview-state/view-state',
      { locator, view_state: viewState || {} }
    )
  },

  listOptimizationTasks(params = {}) {
    return request.get('/manager/tasks', { params: { ...params, task_type: 'vector_materialized_view_generation' } })
  },

  getOptimizationTask(id) {
    return request.get(`/manager/tasks/vector_materialized_view_generation/${id}`)
  },

  createOptimizationTask(payload) {
    return request.post('/manager/tasks/vector_materialized_view_generation', payload)
  },

  updateOptimizationTask(id, payload) {
    return request.put(`/manager/tasks/vector_materialized_view_generation/${id}`, payload)
  },

  deleteOptimizationTask(id) {
    return request.delete(`/manager/tasks/vector_materialized_view_generation/${id}`)
  },

  executeOptimizationTask(id, payload = {}) {
    return request.post(`/manager/tasks/vector_materialized_view_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  listOptimizations(params = {}) {
    return request.get('/manager/vector_materialized_view', { params })
  },

  getOptimization(id) {
    return request.get(`/manager/vector_materialized_view/${id}`)
  },

  deleteOptimization(id) {
    return request.delete(`/manager/vector_materialized_view/${id}`)
  },

  listRasterCOGTasks(params = {}) {
    return request.get('/manager/tasks', { params: { ...params, task_type: 'raster_cog_generation' } })
  },

  getRasterCOGTask(id) {
    return request.get(`/manager/tasks/raster_cog_generation/${id}`)
  },

  deleteRasterCOGTask(id) {
    return request.delete(`/manager/tasks/raster_cog_generation/${id}`)
  },

  executeRasterCOGTask(id, payload = {}) {
    return request.post(`/manager/tasks/raster_cog_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  createRasterMosaicTask(payload) {
    return request.post('/manager/tasks/raster_mosaic_generation', payload)
  },

  listRasterMosaicTasks(params = {}) {
    return request.get('/manager/tasks', { params: { ...params, task_type: 'raster_mosaic_generation' } })
  },

  getRasterMosaicTask(id) {
    return request.get(`/manager/tasks/raster_mosaic_generation/${id}`)
  },

  updateRasterMosaicTask(id, payload) {
    return request.put(`/manager/tasks/raster_mosaic_generation/${id}`, payload)
  },

  deleteRasterMosaicTask(id) {
    return request.delete(`/manager/tasks/raster_mosaic_generation/${id}`)
  },

  executeRasterMosaicTask(id, payload = {}) {
    return request.post(`/manager/tasks/raster_mosaic_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  listRasterCOGs(params = {}) {
    return request.get('/manager/raster_cog', { params })
  },

  getRasterCOG(id) {
    return request.get(`/manager/raster_cog/${id}`)
  },

  deleteRasterCOG(id) {
    return request.delete(`/manager/raster_cog/${id}`)
  },

  createModel3DGLBTask(payload) {
    return request.post('/manager/tasks/model_3d_glb_generation', payload)
  },

  listModel3DGLBTasks(params = {}) {
    return request.get('/manager/tasks', { params: { ...params, task_type: 'model_3d_glb_generation' } })
  },

  getModel3DGLBTask(id) {
    return request.get(`/manager/tasks/model_3d_glb_generation/${id}`)
  },

  updateModel3DGLBTask(id, payload) {
    return request.put(`/manager/tasks/model_3d_glb_generation/${id}`, payload)
  },

  deleteModel3DGLBTask(id) {
    return request.delete(`/manager/tasks/model_3d_glb_generation/${id}`)
  },

  executeModel3DGLBTask(id, payload = {}) {
    return request.post(`/manager/tasks/model_3d_glb_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  listModel3DGLBs(params = {}) {
    return request.get('/manager/model_3d_glb', { params })
  },

  getModel3DGLB(id) {
    return request.get(`/manager/model_3d_glb/${id}`)
  },

  deleteModel3DGLB(id) {
    return request.delete(`/manager/model_3d_glb/${id}`)
  },

  createGaussianSplatKSplatTask(payload) {
    return request.post('/manager/tasks/gaussian_splat_ksplat_generation', payload)
  },

  listGaussianSplatKSplatTasks(params = {}) {
    return request.get('/manager/tasks', { params: { ...params, task_type: 'gaussian_splat_ksplat_generation' } })
  },

  getGaussianSplatKSplatTask(id) {
    return request.get(`/manager/tasks/gaussian_splat_ksplat_generation/${id}`)
  },

  updateGaussianSplatKSplatTask(id, payload) {
    return request.put(`/manager/tasks/gaussian_splat_ksplat_generation/${id}`, payload)
  },

  deleteGaussianSplatKSplatTask(id) {
    return request.delete(`/manager/tasks/gaussian_splat_ksplat_generation/${id}`)
  },

  executeGaussianSplatKSplatTask(id, payload = {}) {
    return request.post(`/manager/tasks/gaussian_splat_ksplat_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  listGaussianSplatKSplats(params = {}) {
    return request.get('/manager/gaussian_splat_ksplat', { params })
  },

  getGaussianSplatKSplat(id) {
    return request.get(`/manager/gaussian_splat_ksplat/${id}`)
  },

  inspectGaussianSplatKSplat(id) {
    return request.get(`/manager/gaussian_splat_ksplat/${id}/inspect`)
  },

  deleteGaussianSplatKSplat(id) {
    return request.delete(`/manager/gaussian_splat_ksplat/${id}`)
  },

  createPointCloudCOPCTask(payload) {
    return request.post('/manager/tasks/point_cloud_copc_generation', payload)
  },

  listPointCloudCOPCTasks(params = {}) {
    return request.get('/manager/tasks', { params: { ...params, task_type: 'point_cloud_copc_generation' } })
  },

  getPointCloudCOPCTask(id) {
    return request.get(`/manager/tasks/point_cloud_copc_generation/${id}`)
  },

  updatePointCloudCOPCTask(id, payload) {
    return request.put(`/manager/tasks/point_cloud_copc_generation/${id}`, payload)
  },

  deletePointCloudCOPCTask(id) {
    return request.delete(`/manager/tasks/point_cloud_copc_generation/${id}`)
  },

  executePointCloudCOPCTask(id, payload = {}) {
    return request.post(`/manager/tasks/point_cloud_copc_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  listPointCloudCOPCs(params = {}) {
    return request.get('/manager/point_cloud_copc', { params })
  },

  getPointCloudCOPC(id) {
    return request.get(`/manager/point_cloud_copc/${id}`)
  },

  deletePointCloudCOPC(id) {
    return request.delete(`/manager/point_cloud_copc/${id}`)
  },

  listModel3DTilesTasks(params = {}) {
    return request.get('/manager/tasks', { params: { ...params, task_type: 'model3d_tiles_generation' } })
  },

  listModel3DTilesResults(params = {}) {
    return request.get('/manager/model3d_tiles', { params })
  },

  deleteModel3DTilesResult(id) {
    return request.delete(`/manager/model3d_tiles/${id}`)
  },

  getModel3DTilesTask(id) {
    return request.get(`/manager/tasks/model3d_tiles_generation/${id}`)
  },

  deleteModel3DTilesTask(id) {
    return request.delete(`/manager/tasks/model3d_tiles_generation/${id}`)
  },

  executeModel3DTilesTask(id, payload = {}) {
    return request.post(`/manager/tasks/model3d_tiles_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  getExecutionStatus(executionID) {
    return request.get(`/manager/executions/${executionID}`)
	},

	listVectorTileSetTasks(params = {}) { return request.get('/manager/tasks', { params: { ...params, task_type: 'vector_tile_set_generation' } }) },
	getVectorTileSetTask(id) { return request.get(`/manager/tasks/vector_tile_set_generation/${id}`) },
	createVectorTileSetTask(payload) { return request.post('/manager/tasks/vector_tile_set_generation', payload) },
	updateVectorTileSetTask(id, payload) { return request.put(`/manager/tasks/vector_tile_set_generation/${id}`, payload) },
	deleteVectorTileSetTask(id) { return request.delete(`/manager/tasks/vector_tile_set_generation/${id}`) },
	executeVectorTileSetTask(id) {
		return request.post(`/manager/tasks/vector_tile_set_generation/${id}/execute`, { trigger_type: 'manual', source: 'manager' })
	}
}
