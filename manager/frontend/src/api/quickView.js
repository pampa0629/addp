import request from './client'

/**
 * Pre-Cache API - 预缓存管理（快显）
 */
export const quickViewAPI = {
  /**
   * 获取瓦片配置（计算 minZoom 和 maxZoom）
   */
  getTileConfig(engineId, schema, table) {
    return request.get(`/manager/engines/${engineId}/spatial/${schema}/${table}/tile-config`)
  },

  /**
   * 触发预缓存生成
   */
  triggerQuickView(engineId, schema, table, params = {}) {
    return request.post(`/manager/engines/${engineId}/spatial/${schema}/${table}/pre-cache`, params)
  },

  /**
   * 获取预缓存状态
   */
  getQuickViewStatus(engineId, schema, table) {
    return request.get(`/manager/engines/${engineId}/spatial/${schema}/${table}/pre-cache/status`)
  },

  /**
   * 清除预缓存
   */
  clearQuickView(engineId, schema, table) {
    return request.delete(`/manager/engines/${engineId}/spatial/${schema}/${table}/pre-cache`)
  },

  /**
   * 更新显示模式偏好
   */
  updatePreferredMode(engineId, schema, table, preferredMode) {
    return request.patch(
      `/manager/engines/${engineId}/spatial/${schema}/${table}/pre-cache/mode`,
      { preferred_mode: preferredMode }
    )
  },

  /**
   * 取消预缓存生成
   */
  cancelQuickView(engineId, schema, table) {
    return request.post(`/manager/engines/${engineId}/spatial/${schema}/${table}/pre-cache/cancel`)
  },

  /**
   * 恢复预缓存生成
   */
  resumeQuickView(engineId, schema, table) {
    return request.post(`/manager/engines/${engineId}/spatial/${schema}/${table}/pre-cache/resume`)
  },

  /**
   * 列出所有预缓存任务
   */
  listQuickViewTasks(params = {}) {
    return request.get('/manager/pre-cache/tasks', { params })
  },

  /**
   * 获取预缓存统计信息
   */
  getStatistics() {
    return request.get('/manager/pre-cache/statistics')
  }
}
