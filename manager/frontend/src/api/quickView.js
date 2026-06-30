import request from './client'

export const quickViewAPI = {
  getQuickViewCapabilityByLocator(locator) {
    return request.get('/manager/quick-view/capability', {
      params: { locator }
    })
  },

  updatePreferredModeByLocator(locator, preferredMode) {
    return request.patch(
      '/manager/quick-view/preferred-mode',
      { locator, preferred_mode: preferredMode }
    )
  },

  listOptimizationTasks(params = {}) {
    return request.get('/manager/vector_quick_view_target_tasks', { params })
  },

  getOptimizationTask(id) {
    return request.get(`/manager/vector_quick_view_target_tasks/${id}`)
  },

  createOptimizationTask(payload) {
    return request.post('/manager/vector_quick_view_target_tasks', payload)
  },

  updateOptimizationTask(id, payload) {
    return request.put(`/manager/vector_quick_view_target_tasks/${id}`, payload)
  },

  deleteOptimizationTask(id) {
    return request.delete(`/manager/vector_quick_view_target_tasks/${id}`)
  },

  executeOptimizationTask(id, payload = {}) {
    return request.post(`/manager/tasks/vector_quick_view_target_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  listOptimizations(params = {}) {
    return request.get('/manager/vector_quick_view_targets', { params })
  },

  getOptimization(id) {
    return request.get(`/manager/vector_quick_view_targets/${id}`)
  },

  deleteOptimization(id) {
    return request.delete(`/manager/vector_quick_view_targets/${id}`)
  },

  createRasterCOGTask(payload) {
    return request.post('/manager/raster_cog_tasks', payload)
  },

  listRasterCOGTasks(params = {}) {
    return request.get('/manager/raster_cog_tasks', { params })
  },

  getRasterCOGTask(id) {
    return request.get(`/manager/raster_cog_tasks/${id}`)
  },

  updateRasterCOGTask(id, payload) {
    return request.put(`/manager/raster_cog_tasks/${id}`, payload)
  },

  deleteRasterCOGTask(id) {
    return request.delete(`/manager/raster_cog_tasks/${id}`)
  },

  executeRasterCOGTask(id, payload = {}) {
    return request.post(`/manager/tasks/raster_cog_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  createRasterMosaicTask(payload) {
    return request.post('/manager/raster_mosaic_tasks', payload)
  },

  listRasterMosaicTasks(params = {}) {
    return request.get('/manager/raster_mosaic_tasks', { params })
  },

  getRasterMosaicTask(id) {
    return request.get(`/manager/raster_mosaic_tasks/${id}`)
  },

  updateRasterMosaicTask(id, payload) {
    return request.put(`/manager/raster_mosaic_tasks/${id}`, payload)
  },

  deleteRasterMosaicTask(id) {
    return request.delete(`/manager/raster_mosaic_tasks/${id}`)
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

  createModel3DQuickViewTask(payload) {
    return request.post('/manager/model_3d_quick_view_tasks', payload)
  },

  listModel3DQuickViewTasks(params = {}) {
    return request.get('/manager/model_3d_quick_view_tasks', { params })
  },

  getModel3DQuickViewTask(id) {
    return request.get(`/manager/model_3d_quick_view_tasks/${id}`)
  },

  updateModel3DQuickViewTask(id, payload) {
    return request.put(`/manager/model_3d_quick_view_tasks/${id}`, payload)
  },

  deleteModel3DQuickViewTask(id) {
    return request.delete(`/manager/model_3d_quick_view_tasks/${id}`)
  },

  executeModel3DQuickViewTask(id, payload = {}) {
    return request.post(`/manager/tasks/model_3d_quick_view_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  listModel3DQuickViews(params = {}) {
    return request.get('/manager/model_3d_quick_view', { params })
  },

  getModel3DQuickView(id) {
    return request.get(`/manager/model_3d_quick_view/${id}`)
  },

  deleteModel3DQuickView(id) {
    return request.delete(`/manager/model_3d_quick_view/${id}`)
  },

  createGaussianSplatQuickViewTask(payload) {
    return request.post('/manager/gaussian_splat_quick_view_tasks', payload)
  },

  listGaussianSplatQuickViewTasks(params = {}) {
    return request.get('/manager/gaussian_splat_quick_view_tasks', { params })
  },

  getGaussianSplatQuickViewTask(id) {
    return request.get(`/manager/gaussian_splat_quick_view_tasks/${id}`)
  },

  updateGaussianSplatQuickViewTask(id, payload) {
    return request.put(`/manager/gaussian_splat_quick_view_tasks/${id}`, payload)
  },

  deleteGaussianSplatQuickViewTask(id) {
    return request.delete(`/manager/gaussian_splat_quick_view_tasks/${id}`)
  },

  executeGaussianSplatQuickViewTask(id, payload = {}) {
    return request.post(`/manager/tasks/gaussian_splat_quick_view_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  listGaussianSplatQuickViews(params = {}) {
    return request.get('/manager/gaussian_splat_quick_view', { params })
  },

  getGaussianSplatQuickView(id) {
    return request.get(`/manager/gaussian_splat_quick_view/${id}`)
  },

  deleteGaussianSplatQuickView(id) {
    return request.delete(`/manager/gaussian_splat_quick_view/${id}`)
  },

  createModel3DTilesTask(payload) {
    return request.post('/manager/model_3d_tiles_tasks', payload)
  },

  listModel3DTilesTasks(params = {}) {
    return request.get('/manager/model_3d_tiles_tasks', { params })
  },

  getModel3DTilesTask(id) {
    return request.get(`/manager/model_3d_tiles_tasks/${id}`)
  },

  updateModel3DTilesTask(id, payload) {
    return request.put(`/manager/model_3d_tiles_tasks/${id}`, payload)
  },

  deleteModel3DTilesTask(id) {
    return request.delete(`/manager/model_3d_tiles_tasks/${id}`)
  },

  executeModel3DTilesTask(id, payload = {}) {
    return request.post(`/manager/tasks/model_3d_tiles_generation/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  getExecutionStatus(executionID) {
    return request.get(`/manager/executions/${executionID}`)
  }
}
