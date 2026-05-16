import client from './client'

/**
 * 获取存储引擎列表
 * @returns {Promise}
 */
export const listEngines = () => {
  return client.get('/develop/engines')
}

/**
 * 获取 NFS 存储引擎列表
 * @returns {Promise}
 */
export const listNfsEngines = () => {
  return client.get('/develop/engines/nfs')
}

/**
 * 获取指定引擎的实时 catalog 子节点
 * @param {number} engineId - 引擎ID
 * @param {object} path - Catalog 路径
 * @param {object} options - 列表选项
 * @returns {Promise}
 */
export const listCatalogChildren = (engineId, path = { segments: [] }, options = {}) => {
  return client.post(`/develop/engines/${engineId}/catalog/children`, {
    path,
    options
  })
}

/**
 * 获取工作流引擎列表
 * @returns {Promise}
 */
export const getWorkflowEngines = () => {
  return client.get('/develop/workflow-engines')
}

/**
 * 获取 Spark 运行时列表
 * @returns {Promise}
 */
export const getSparkRuntimes = () => {
  return client.get('/develop/spark-runtimes')
}
