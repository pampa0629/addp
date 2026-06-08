import client from './client'

export default {
  /**
   * 根据 TaskProvider 模块名获取任务列表
   * @param {string} moduleName - TaskProvider 模块名，如 "meta"
   * @param {Object} params - 查询参数 {page, page_size, type, status 等}
   * @returns {Promise<{items: Array, total: number}>}
   * @example
   * listTasksByModule('meta', {page: 1, page_size: 100})
   */
  async listTasksByModule(moduleName, params = {}) {
    const data = await client.get('/orchestrator/tasks', {
      params: { module_name: moduleName, ...params }
    })
    return data
  }
}
