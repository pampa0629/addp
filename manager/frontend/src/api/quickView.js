import request from './client'

/**
 * Pre-Cache API - 预缓存管理
 *
 * Note: 路由已更新为 /pre-cache，旧路由 /quick-view 保留作为别名（向后兼容）
 */
export const quickViewAPI = {
  /**
   * 触发预缓存生成
   */
  triggerQuickView(resourceId, schema, table, params = {}) {
    return request.post(`/resources/${resourceId}/spatial/${schema}/${table}/pre-cache`, params)
  },

  /**
   * 获取预缓存状态
   */
  getQuickViewStatus(resourceId, schema, table) {
    return request.get(`/resources/${resourceId}/spatial/${schema}/${table}/pre-cache/status`)
  },

  /**
   * 清除预缓存
   */
  clearQuickView(resourceId, schema, table) {
    return request.delete(`/resources/${resourceId}/spatial/${schema}/${table}/pre-cache`)
  },

  /**
   * 列出所有预缓存任务
   */
  listQuickViewTasks(params = {}) {
    return request.get('/pre-cache/tasks', { params })
  },

  /**
   * 获取预缓存统计信息
   */
  getStatistics() {
    return request.get('/pre-cache/statistics')
  }
}
