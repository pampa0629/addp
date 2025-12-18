import client from './client'

/**
 * 执行 SQL
 * @param {number} resourceId - 数据源ID
 * @param {string} sql - SQL语句
 * @param {number} timeout - 超时时间（毫秒）
 */
export const executeSQL = (resourceId, sql, timeout = 30000) => {
  return client.post('/develop/execute', {
    resource_id: resourceId,
    sql: sql,
    timeout: timeout
  })
}

/**
 * 测试数据库连接
 * @param {number} resourceId - 数据源ID
 */
export const testConnection = (resourceId) => {
  return client.get(`/develop/test/${resourceId}`)
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
export const saveSQLTask = (taskData) => {
  return client.post('/develop/sql/tasks', taskData)
}

/**
 * 更新 SQL 任务
 * @param {number} id - 任务ID
 * @param {object} taskData - 任务数据
 */
export const updateSQLTask = (id, taskData) => {
  return client.put(`/develop/sql/tasks/${id}`, taskData)
}

/**
 * 获取 SQL 任务列表
 * @param {object} params - 查询参数
 */
export const listSQLTasks = (params = {}) => {
  return client.get('/develop/sql/tasks', { params })
}

/**
 * 获取 SQL 任务详情
 * @param {number} id - 任务ID
 */
export const getSQLTask = (id) => {
  return client.get(`/develop/sql/tasks/${id}`)
}

/**
 * 删除 SQL 任务
 * @param {number} id - 任务ID
 */
export const deleteSQLTask = (id) => {
  return client.delete(`/develop/sql/tasks/${id}`)
}
