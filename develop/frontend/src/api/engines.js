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
 * 获取指定引擎的 schema 列表
 * @param {number} engineId - 引擎ID
 * @returns {Promise}
 */
export const listSchemas = (engineId) => {
  return client.get(`/develop/engines/${engineId}/schemas`)
}

/**
 * 获取指定引擎和 schema 下的表列表
 * @param {number} engineId - 引擎ID
 * @param {string} schema - Schema名称
 * @returns {Promise}
 */
export const listTables = (engineId, schema) => {
  return client.get(`/develop/engines/${engineId}/tables`, {
    params: { schema }
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
