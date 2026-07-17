/**
 * Meta 模块 API
 * Transfer 模块调用 Meta 模块获取元数据信息
 */
import { createAPIClient } from '@common-ui'
import { useAuthStore } from '../store/auth'

const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Transfer',
  baseURL: '/api/v1',
  extractData: false
})

export const getItemByID = async (itemId) => {
  const response = await client.get(`/meta/items/${itemId}`)
  return response.data?.data || response.data
}

export const getItemFieldsByID = async (itemId) => {
  const response = await client.get(`/meta/items/${itemId}/fields`, {
    params: {
      include_details: true
    }
  })
  return response.data
}

export const getNodeByCatalogPath = async (engineId, catalogPath) => {
  const response = await client.get('/meta/nodes/by-catalog-path', {
    params: {
      engine_id: engineId,
      catalog_path: catalogPath
    }
  })
  return response.data?.data || response.data
}
