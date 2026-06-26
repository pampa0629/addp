import client from './client'

/**
 * 获取存储引擎列表
 * @returns {Promise}
 */
export const listEngines = () => {
  return client.get('/develop/engines')
}

/**
 * 获取 Develop 内置查询模式列表
 * @returns {Promise}
 */
export const listQueryModes = () => {
  return client.get('/develop/query-modes')
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
