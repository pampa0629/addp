import client from './client'

export default {
  /**
   * 获取 Transfer 任务列表
   * @param {Object} params - 查询参数 {page, page_size, type, status}
   * @returns {Promise<{items: Array, total: number}>}
   */
  async listTransferTasks(params = {}) {
    // 注意: client 的响应拦截器已经返回 response.data，所以这里直接返回
    const data = await client.get('/tasks/list', {
      params: { module: 'transfer', page_size: 100, ...params }
    })
    return data
  },

  /**
   * 获取 Meta 扫描任务列表
   * @returns {Promise<Array>}
   */
  async listMetaTasks() {
    // 注意: client 的响应拦截器已经返回 response.data，所以这里直接返回
    const data = await client.get('/tasks/list', {
      params: { module: 'meta' }
    })
    return data
  },

  /**
   * 获取 Manager 任务列表
   * @returns {Promise<Array>}
   */
  async listManagerTasks() {
    // 注意: client 的响应拦截器已经返回 response.data，所以这里直接返回
    const data = await client.get('/tasks/list', {
      params: { module: 'manager' }
    })
    return data
  }
}
