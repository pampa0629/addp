import client from './client'

export default {
  /**
   * 【新】根据 unique_identifier 获取任务列表（动态调用）
   * @param {string} uniqueIdentifier - 资源的唯一标识符，如 "meta.scanner.default"
   * @param {Object} params - 查询参数 {page, page_size, type, status 等}
   * @returns {Promise<{items: Array, total: number}>}
   * @example
   * listTasksByIdentifier('meta.scanner.default', {page: 1, page_size: 100})
   */
  async listTasksByIdentifier(uniqueIdentifier, params = {}) {
    const data = await client.get('/tasks/list', {
      params: { unique_identifier: uniqueIdentifier, ...params }
    })
    return data
  },

  /**
   * 【旧】获取 Transfer 任务列表（向后兼容，推荐使用 listTasksByIdentifier）
   * @param {Object} params - 查询参数 {page, page_size, type, status}
   * @returns {Promise<{items: Array, total: number}>}
   * @deprecated 请使用 listTasksByIdentifier('transfer.importer.default', params)
   */
  async listTransferTasks(params = {}) {
    // 注意: client 的响应拦截器已经返回 response.data，所以这里直接返回
    const data = await client.get('/tasks/list', {
      params: { module: 'transfer', page_size: 100, ...params }
    })
    return data
  },

  /**
   * 【旧】获取 Meta 扫描任务列表（向后兼容，推荐使用 listTasksByIdentifier）
   * @returns {Promise<Array>}
   * @deprecated 请使用 listTasksByIdentifier('meta.scanner.default')
   */
  async listMetaTasks() {
    // 注意: client 的响应拦截器已经返回 response.data，所以这里直接返回
    const data = await client.get('/tasks/list', {
      params: { module: 'meta' }
    })
    return data
  },

  /**
   * 【旧】获取 Manager 任务列表（向后兼容，推荐使用 listTasksByIdentifier）
   * @returns {Promise<Array>}
   * @deprecated 请使用 listTasksByIdentifier('manager.tile_cache.default')
   */
  async listManagerTasks() {
    // 注意: client 的响应拦截器已经返回 response.data，所以这里直接返回
    const data = await client.get('/tasks/list', {
      params: { module: 'manager' }
    })
    return data
  }
}
