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
 * 获取指定引擎的命名空间列表
 * @param {number} engineId - 引擎ID
 * @returns {Promise}
 */
export const listNamespaces = (engineId) => {
  return client.get(`/develop/engines/${engineId}/namespaces`)
}

/**
 * 获取指定引擎和命名空间下的数据项列表
 * @param {number} engineId - 引擎ID
 * @param {string} namespace - 命名空间名称
 * @returns {Promise}
 */
export const listCatalogItems = (engineId, namespace) => {
  return client.get(`/develop/engines/${engineId}/items`, {
    params: { namespace }
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
