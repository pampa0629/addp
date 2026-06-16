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
    return request.get('/manager/quick_view_optimization_tasks', { params })
  },

  getOptimizationTask(id) {
    return request.get(`/manager/quick_view_optimization_tasks/${id}`)
  },

  createOptimizationTask(payload) {
    return request.post('/manager/quick_view_optimization_tasks', payload)
  },

  updateOptimizationTask(id, payload) {
    return request.put(`/manager/quick_view_optimization_tasks/${id}`, payload)
  },

  deleteOptimizationTask(id) {
    return request.delete(`/manager/quick_view_optimization_tasks/${id}`)
  },

  executeOptimizationTask(id, payload = {}) {
    return request.post(`/manager/tasks/quick_view_optimization/${id}/execute`, {
      trigger_type: 'manual',
      source: 'manager',
      ...payload
    })
  },

  listOptimizations(params = {}) {
    return request.get('/manager/quick_view_optimization', { params })
  },

  getOptimization(id) {
    return request.get(`/manager/quick_view_optimization/${id}`)
  },

  deleteOptimization(id) {
    return request.delete(`/manager/quick_view_optimization/${id}`)
  }
}
