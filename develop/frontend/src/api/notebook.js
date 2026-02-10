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
  },

  /**
   * 上传 Notebook 文件并创建 dev_item
   * @param {File} file - Notebook 文件
   * @param {Object} options - 上传选项
   * @param {string} options.name - Notebook 名称
   * @param {string} options.description - 描述
   * @param {Array<number>} options.data_sources - 数据源 IDs
   * @param {Object} options.parameters - 参数
   * @param {string} options.kernel - Kernel 类型（默认 python3）
   * @returns {Promise}
   */
  uploadNotebook(file, options = {}) {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('name', options.name || file.name.replace('.ipynb', ''))

    if (options.description) {
      formData.append('description', options.description)
    }

    if (options.data_sources && options.data_sources.length > 0) {
      formData.append('data_sources', JSON.stringify(options.data_sources))
    }

    if (options.parameters && Object.keys(options.parameters).length > 0) {
      formData.append('parameters', JSON.stringify(options.parameters))
    }

    if (options.kernel) {
      formData.append('kernel', options.kernel)
    }

    return client.post('/develop/notebooks/upload', formData, {
      headers: {
        'Content-Type': 'multipart/form-data'
      }
    })
  },

  /**
   * 列出 Notebooks
   * @param {Object} params - 查询参数
   * @param {number} params.page - 页码
   * @param {number} params.page_size - 每页数量
   * @returns {Promise}
   */
  listNotebooks(params = {}) {
    return client.get('/develop/notebooks', { params })
  },

  /**
   * 下载 Notebook 文件
   * @param {number} id - DevItem ID
   * @returns {Promise}
   */
  downloadNotebook(id) {
    return client.get(`/develop/notebooks/${id}/download`, {
      responseType: 'blob'
    })
  },

  /**
   * 删除 Notebook
   * @param {number} id - DevItem ID
   * @returns {Promise}
   */
  deleteNotebook(id) {
    return client.delete(`/develop/notebooks/${id}`)
  }
}
