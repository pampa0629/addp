import client from './client'

/**
 * Notebook 开发 API
 */
export const notebookAPI = {
  /**
   * 列出可用的 Notebook 引擎
   * @returns {Promise}
   */
  listNotebookEngines() {
    return client.get('/develop/notebook-engines')
  },

  /**
   * 列出指定 Notebook 引擎的 Kernel
   * @param {number} engineId - Notebook 引擎实例 ID
   * @returns {Promise}
   */
  listKernels(engineId) {
    return client.get(`/develop/notebook-engines/${engineId}/kernels`)
  },

  /**
   * 上传 Notebook 文件并创建 dev_task
   * @param {File} file - Notebook 文件
   * @param {Object} options - 上传选项
   * @param {string} options.name - Notebook 名称
   * @param {string} options.description - 描述
   * @param {Object} options.parameters - 参数
   * @param {number} options.engine_id - Notebook 引擎实例 ID
   * @param {string} options.kernel - Kernel 名称
   * @returns {Promise}
   */
  uploadNotebook(file, options = {}) {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('name', options.name || file.name.replace('.ipynb', ''))
    formData.append('engine_id', String(options.engine_id))
    formData.append('kernel', options.kernel)

    if (options.description) {
      formData.append('description', options.description)
    }

    if (options.parameters && Object.keys(options.parameters).length > 0) {
      formData.append('parameters', JSON.stringify(options.parameters))
    }

    return client.post('/develop/notebooks/upload', formData, {
      headers: {
        'Content-Type': 'multipart/form-data'
      }
    })
  },

  createNotebook(options) {
    return client.post('/develop/notebooks', options)
  },

  createSession(id) {
    return client.post(`/develop/notebooks/${id}/sessions`)
  },

  closeSession(id, sessionId) {
    return client.delete(`/develop/notebooks/${id}/sessions/${sessionId}`)
  },

  generateSessionCell(sessionId, payload) {
    return client.post(`/develop/notebook-copilot-sessions/${sessionId}/generate`, payload)
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
   * 完整替换 Notebook 当前任务的运行时绑定
   * @param {number} id - DevTask ID
   * @param {Object} binding - 运行时绑定
   * @param {number} binding.engine_id - Notebook 引擎实例 ID
   * @param {string} binding.kernel - Kernel 名称
   * @returns {Promise}
   */
  updateRuntimeBinding(id, binding) {
    return client.put(`/develop/notebooks/${id}/runtime-binding`, binding)
  },

  /**
   * 下载 Notebook 文件
   * @param {number} id - DevTask ID
   * @returns {Promise}
   */
  downloadNotebook(id) {
    return client.get(`/develop/notebooks/${id}/download`, {
      responseType: 'blob'
    })
  },

  /**
   * 删除 Notebook
   * @param {number} id - DevTask ID
   * @returns {Promise}
   */
  deleteNotebook(id) {
    return client.delete(`/develop/notebooks/${id}`)
  }
}
