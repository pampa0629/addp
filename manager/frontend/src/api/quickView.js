import request from './client'

/**
 * Quick View API - 快显缓存管理
 */
export const quickViewAPI = {
  /**
   * 触发快显缓存生成
   */
  triggerQuickView(resourceId, schema, table, params = {}) {
    return request.post(`/resources/${resourceId}/spatial/${schema}/${table}/quick-view`, params)
  },

  /**
   * 获取快显状态
   */
  getQuickViewStatus(resourceId, schema, table) {
    return request.get(`/resources/${resourceId}/spatial/${schema}/${table}/quick-view/status`)
  },

  /**
   * 清除快显缓存
   */
  clearQuickView(resourceId, schema, table) {
    return request.delete(`/resources/${resourceId}/spatial/${schema}/${table}/quick-view`)
  },

  /**
   * 列出所有快显任务
   */
  listQuickViewTasks(params = {}) {
    return request.get('/quick-view/tasks', { params })
  },

  /**
   * 获取快显统计信息
   */
  getStatistics() {
    return request.get('/quick-view/statistics')
  }
}
