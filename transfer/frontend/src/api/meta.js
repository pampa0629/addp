/**
 * Meta 模块 API
 * Transfer 模块调用 Meta 模块获取元数据信息
 */
import axios from 'axios'

const API_BASE_URL = '/api/v1'

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

export const getItemByID = async (itemId) => {
  const response = await apiRequest('get', `/meta/items/${itemId}`)
  return response.data?.data || response.data
}

export const getItemFieldsByID = async (itemId) => {
  const response = await apiRequest('get', `/meta/items/${itemId}/fields`, {
    params: {
      include_details: true
    }
  })
  return response.data
}

export const getNodeByCatalogPath = async (engineId, catalogPath) => {
  const response = await apiRequest('get', '/meta/nodes/by-catalog-path', {
    params: {
      engine_id: engineId,
      catalog_path: catalogPath
    }
  })
  return response.data?.data || response.data
}
