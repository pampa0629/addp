import client from './client'

/**
 * Notebook 开发 API
 */
export const notebookAPI = {
  /**
   * 获取 Jupyter Lab URL
   * @param {Object} data - 请求参数
   * @param {string} data.notebook_path - Notebook 文件路径
   * @returns {Promise}
   */
  getJupyterURL(data) {
    return client.post('/develop/notebooks/jupyter-url', data)
  },

  /**
   * 执行 Notebook
   * @param {Object} data - 执行参数
   * @param {string} data.input_path - 输入 Notebook 路径
   * @param {string} data.output_path - 输出 Notebook 路径
   * @param {Object} data.parameters - 参数字典
   * @param {number} data.timeout - 超时时间（秒）
   * @returns {Promise}
   */
  executeNotebook(data) {
    return client.post('/develop/notebooks/execute', data, {
      timeout: (data.timeout || 300) * 1000 // 转换为毫秒
    })
  },

  /**
   * 列出可用的 Kernel
   * @returns {Promise}
   */
  listKernels() {
    return client.get('/develop/notebooks/kernels')
  },

  /**
   * 检查 Jupyter Engine 健康状态
   * @returns {Promise}
   */
  healthCheck() {
    return client.get('/develop/notebooks/health')
  }
}
