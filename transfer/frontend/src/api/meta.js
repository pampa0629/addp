/**
 * Meta 模块 API
 * Transfer 模块调用 Meta 模块获取元数据信息
 */
import axios from 'axios'
import { listCatalogChildren as listSystemCatalogChildren } from '@addp/common-frontend'
import client from './client'

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

/**
 * 获取引擎的顶层 catalog 节点列表
 * @param {number} engineId - 引擎ID
 */
export const getSchemas = async (engineId) => {
  const response = await apiRequest('post', `/system/engines/${engineId}/catalog/children`, {
    data: {
      path: { segments: [] },
      options: {}
    }
  })
  const nodes = response.data?.nodes || response.data?.data?.nodes || []
  return nodes.filter(item => item.role === 'branch').map(item => ({
    ...item,
    schema_name: item.name || item.schema_name,
    name: item.name || item.schema_name
  }))
}

export const listCatalogChildren = async (engineId, path = { segments: [] }, options = {}) => {
  return listSystemCatalogChildren(client, engineId, path, options)
}

export const getTransferEngineTree = async (engineId, expandDepth = 1) => {
  return client.get(`/transfer/engines/${engineId}/tree`, {
    params: {
      expand_depth: expandDepth
    }
  })
}

export const getTransferNodeChildren = async (nodeId) => {
  return client.get(`/transfer/nodes/${nodeId}/children`)
}

export const getItemByCatalogPath = async (engineId, catalogPath) => {
  const response = await apiRequest('get', '/meta/items/by-catalog-path', {
    params: {
      engine_id: engineId,
      catalog_path: catalogPath
    }
  })
  return response.data?.data || response.data
}

export const getItemFieldsByCatalogPath = async (engineId, catalogPath) => {
  const item = await getItemByCatalogPath(engineId, catalogPath)
  const response = await apiRequest('get', `/meta/items/${item.id}/fields`, {
    params: {
      include_details: true
    }
  })
  return response.data
}

/**
 * 获取表列表
 * @param {number} engineId - 引擎ID
 * @param {string} schema - Schema名称（可选）
 */
export const getTables = async (engineId, schema = null) => {
  const params = {}
  if (schema) {
    params.branch = schema
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
  const catalogPath = schema ? `${schema}.${tableName}` : tableName
  return getItemFieldsByCatalogPath(engineId, catalogPath)
}
