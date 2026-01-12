/**
 * 默认配置 API（占位实现）
 *
 * 注意：这是 common-frontend/map 的默认实现，仅返回空配置。
 * 各业务模块应该使用自己的 useMapConfig 实现来调用实际的后端 API。
 *
 * 例如：
 * - manager/frontend/src/composables/useMapConfig.js
 * - develop/frontend/src/composables/useMapConfig.js
 */

export const configAPI = {
  /**
   * 获取地图配置（默认实现）
   * @returns {Promise} 返回空配置
   */
  getMapConfig() {
    // 返回空配置，依赖环境变量作为后备
    return Promise.resolve({
      data: {
        amap_key: '',
        amap_security_js_code: '',
        tdt_key: ''
      }
    })
  }
}

export default configAPI
