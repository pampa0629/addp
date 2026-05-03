/**
 * Meta 模块 API
 * Transfer 模块调用 Meta 模块获取元数据信息
 */
import axios from 'axios'

const API_BASE_URL = import.meta.env.DEV
  ? 'http://localhost:8000/api/v1'
  : '/api/v1'

// 创建带认证的请求函数
const apiRequest = (method, url, options = {}) => {
  const token = localStorage.getItem('token')
  return axios({
    method,
    url: `${API_BASE_URL}${url}`,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(options.headers || {})
    },
    ...options
  })
}

/**
 * 获取引擎的命名空间列表
 * @param {number} engineId - 引擎ID
 */
export const getSchemas = async (engineId) => {
  const response = await apiRequest('get', `/system/engines/${engineId}/namespaces`)
  const namespaces = response.data?.namespaces || response.data?.data?.namespaces || response.data || []
  return namespaces.map(item => ({
    ...item,
    schema_name: item.name || item.schema_name,
    name: item.name || item.schema_name
  }))
}

/**
 * 获取表列表
 * @param {number} engineId - 引擎ID
 * @param {string} schema - Schema名称（可选）
 */
export const getTables = async (engineId, schema = null) => {
  const params = {}
  if (schema) {
    params.namespace = schema
  }
  const response = await apiRequest('get', `/meta/engines/${engineId}/items`, { params })
  return response.data
}

/**
 * 获取表字段列表
 * @param {number} engineId - 引擎ID
 * @param {string} schema - Schema名称
 * @param {string} tableName - 表名
 */
export const getTableFields = async (engineId, schema, tableName) => {
  const response = await apiRequest('get', `/meta/engines/${engineId}/items/fields`, {
    params: {
      namespace: schema,
      name: tableName,
      include_details: true
    }
  })
  return response.data
}
