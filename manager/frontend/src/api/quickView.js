import request from './client'

/**
 * Pre-Cache API - 预缓存管理
 *
 * Note: 路由已更新为 /pre-cache，旧路由 /quick-view 保留作为别名（向后兼容）
 */
export const quickViewAPI = {
  /**
   * 获取瓦片配置（计算 minZoom 和 maxZoom）
   */
  getTileConfig(engineId, schema, table) {
    return request.get(`/engines/${engineId}/spatial/${schema}/${table}/tile-config`)
  },

  /**
   * 触发预缓存生成
   */
  triggerQuickView(engineId, schema, table, params = {}) {
    return request.post(`/engines/${engineId}/spatial/${schema}/${table}/pre-cache`, params)
  },

  /**
   * 获取预缓存状态
   */
  getQuickViewStatus(engineId, schema, table) {
    return request.get(`/engines/${engineId}/spatial/${schema}/${table}/pre-cache/status`)
  },

  /**
   * 清除预缓存
   */
  clearQuickView(engineId, schema, table) {
    return request.delete(`/engines/${engineId}/spatial/${schema}/${table}/pre-cache`)
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
