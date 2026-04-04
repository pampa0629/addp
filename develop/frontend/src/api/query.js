import client from './client'

/**
 * 获取引擎的样例查询（切换引擎时自动填充编辑器）
 * @param {number} engineId - 引擎ID
 * @returns {{ query: string, language: string }}
 */
export const getSampleQuery = (engineId) => {
  return client.get(`/develop/engines/${engineId}/sample-query`)
}

/**
 * 执行 SQL
 * @param {number} engineId - 数据源ID
 * @param {string} sql - SQL语句
 * @param {number} timeout - 超时时间（毫秒）
 */
export const executeQuery = (engineId, sql, timeout = 30000) => {
  return client.post('/develop/execute', {
    engine_id: engineId,
    query: sql,
    timeout: timeout
  })
}

/**
 * 测试数据库连接
 * @param {number} engineId - 数据源ID
 */
export const testConnection = (engineId) => {
  return client.get(`/develop/test/${engineId}`)
}

/**
 * 获取健康状态
 */
export const getHealth = () => {
  return client.get('/develop/health')
}

/**
 * 保存 SQL 为任务
 * @param {object} taskData - 任务数据
 */
export const saveQueryTask = (taskData) => {
  return client.post('/develop/query/tasks', taskData)
}

/**
 * 更新 SQL 任务
 * @param {number} id - 任务ID
 * @param {object} taskData - 任务数据
 */
export const updateQueryTask = (id, taskData) => {
  return client.put(`/develop/query/tasks/${id}`, taskData)
}

/**
 * 获取 SQL 任务列表
 * @param {object} params - 查询参数
 */
export const listQueryTasks = (params = {}) => {
  return client.get('/develop/query/tasks', { params })
}

/**
 * 获取 SQL 任务详情
 * @param {number} id - 任务ID
 */
export const getQueryTask = (id) => {
  return client.get(`/develop/query/tasks/${id}`)
}

/**
 * 删除 SQL 任务
 * @param {number} id - 任务ID
 */
export const deleteQueryTask = (id) => {
  return client.delete(`/develop/query/tasks/${id}`)
}
