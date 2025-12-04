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
