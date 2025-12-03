import client from './client'

export default {
  /**
   * 获取 Transfer 任务列表
   * @param {Object} params - 查询参数 {page, page_size, type, status}
   * @returns {Promise<{items: Array, total: number}>}
   */
  async listTransferTasks(params = {}) {
    const response = await client.get('/tasks/list', {
      params: { module: 'transfer', page_size: 100, ...params }
    })
    return response.data
  },

  /**
   * 获取 Meta 扫描任务列表
   * @returns {Promise<Array>}
   */
  async listMetaTasks() {
    const response = await client.get('/tasks/list', {
      params: { module: 'meta' }
    })
    return response.data
  },

  /**
   * 获取 Manager 任务列表
   * @returns {Promise<Array>}
   */
  async listManagerTasks() {
    const response = await client.get('/tasks/list', {
      params: { module: 'manager' }
    })
    return response.data
  }
}
