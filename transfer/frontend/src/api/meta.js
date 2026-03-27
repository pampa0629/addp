/**
 * Meta 模块 API
 * Transfer 模块调用 Meta 模块获取元数据信息
 */
import axios from 'axios'

// Meta 模块的基础URL（开发环境）
const META_BASE_URL = import.meta.env.DEV
  ? 'http://localhost:8000/api/v1/meta'
  : '/api/v1/meta'

// 创建带认证的请求函数
const metaRequest = (method, url, options = {}) => {
  const token = localStorage.getItem('token')
  return axios({
    method,
    url: `${META_BASE_URL}${url}`,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(options.headers || {})
    },
    ...options
  })
}

/**
 * 获取引擎的 schemas 列表
 * @param {number} engineId - 引擎ID
 */
export const getSchemas = async (engineId) => {
  const response = await metaRequest('get', `/engines/${engineId}/schemas`)
  // 后端返回格式: { data: [...] }
  // 直接返回，不要再包装一层
  return response.data
}

/**
 * 获取表列表
 * @param {number} engineId - 引擎ID
 * @param {string} schema - Schema名称（可选）
 */
export const getTables = async (engineId, schema = null) => {
  const params = { engine_id: engineId }
  if (schema) {
    params.schema = schema
  }
  const response = await metaRequest('get', '/metadata/tables', { params })
  // 后端返回格式: { data: [...] }
  // 直接返回，不要再包装一层
  return response.data
}

/**
 * 获取表字段列表
 * @param {number} engineId - 引擎ID
 * @param {string} schema - Schema名称
 * @param {string} tableName - 表名
 */
export const getTableFields = async (engineId, schema, tableName) => {
  const response = await metaRequest('get', '/metadata/fields', {
    params: {
      engine_id: engineId,
      schema,
      table_name: tableName,
      include_details: true
    }
  })
  // 后端返回格式: { data: [...] }
  // 直接返回，不要再包装一层
  return response.data
}
