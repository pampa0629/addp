import request from './client'

/**
 * Quick View API - 快显服务（准备 → 预缓存 → MVT启用）
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
    return request.post(`/manager/engines/${engineId}/spatial/${schema}/${table}/quick-view/pre-cache`, params)
  },

  /**
   * 获取预缓存状态
   */
  getQuickViewStatus(engineId, schema, table) {
    return request.get(`/manager/engines/${engineId}/spatial/${schema}/${table}/quick-view/status`)
  },

  /**
   * 清除预缓存
   */
  clearQuickView(engineId, schema, table) {
    return request.delete(`/manager/engines/${engineId}/spatial/${schema}/${table}/quick-view`)
  },

  /**
   * 更新显示模式偏好
   */
  updatePreferredMode(engineId, schema, table, preferredMode) {
    return request.patch(
      `/manager/engines/${engineId}/spatial/${schema}/${table}/quick-view/preferred-mode`,
      { preferred_mode: preferredMode }
    )
  },

  /**
   * 取消预缓存生成
   */
  cancelQuickView(engineId, schema, table) {
    return request.post(`/manager/engines/${engineId}/spatial/${schema}/${table}/quick-view/cancel`)
  },

  /**
   * 恢复预缓存生成
   */
  resumeQuickView(engineId, schema, table) {
    return request.post(`/manager/engines/${engineId}/spatial/${schema}/${table}/quick-view/resume`)
  },

  /**
   * 列出所有快显任务
   */
  listQuickViewTasks(params = {}) {
    return request.get('/manager/quick-view/tasks', { params })
  },

  /**
   * 获取快显统计信息
   */
  getStatistics() {
    return request.get('/manager/quick-view/statistics')
  },

  /**
   * 检查准备状态（诊断，不修改）
   */
  checkPreparation(engineId, schema, table) {
    return request.get(`/manager/engines/${engineId}/spatial/${schema}/${table}/quick-view/check-preparation`)
  },

  /**
   * 启动准备工作（创建物化视图、索引、ANALYZE）
   */
  prepareForCreateMVT(engineId, schema, table) {
    return request.post(`/manager/engines/${engineId}/spatial/${schema}/${table}/quick-view/prepare`)
  }
}
