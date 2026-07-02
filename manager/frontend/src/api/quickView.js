import request from './client'

export const quickViewAPI = {
  getQuickViewCapabilityByLocator(locator) {
    return request.get('/manager/quick-view/capability', {
      params: { locator }
    })
  },

  executeQuickViewAction(locator, action) {
    return request.post('/manager/quick-view/actions', { locator, action })
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
    return request.get('/manager/vector_materialized_view_tasks', { params })
  },

  getOptimizationTask(id) {
    return request.get(`/manager/vector_materialized_view_tasks/${id}`)
  },

  createOptimizationTask(payload) {
    return request.post('/manager/vector_materialized_view_tasks', payload)
  },

  updateOptimizationTask(id, payload) {
    return request.put(`/manager/vector_materialized_view_tasks/${id}`, payload)
  },

  deleteOptimizationTask(id) {
    return request.delete(`/manager/vector_materialized_view_tasks/${id}`)
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

  createModel3DGLBTask(payload) {
    return request.post('/manager/model_3d_glb_tasks', payload)
  },

  listModel3DGLBTasks(params = {}) {
    return request.get('/manager/model_3d_glb_tasks', { params })
  },

  getModel3DGLBTask(id) {
    return request.get(`/manager/model_3d_glb_tasks/${id}`)
  },

  updateModel3DGLBTask(id, payload) {
    return request.put(`/manager/model_3d_glb_tasks/${id}`, payload)
  },

  deleteModel3DGLBTask(id) {
    return request.delete(`/manager/model_3d_glb_tasks/${id}`)
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
    return request.post('/manager/gaussian_splat_ksplat_tasks', payload)
  },

  listGaussianSplatKSplatTasks(params = {}) {
    return request.get('/manager/gaussian_splat_ksplat_tasks', { params })
  },

  getGaussianSplatKSplatTask(id) {
    return request.get(`/manager/gaussian_splat_ksplat_tasks/${id}`)
  },

  updateGaussianSplatKSplatTask(id, payload) {
    return request.put(`/manager/gaussian_splat_ksplat_tasks/${id}`, payload)
  },

  deleteGaussianSplatKSplatTask(id) {
    return request.delete(`/manager/gaussian_splat_ksplat_tasks/${id}`)
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
