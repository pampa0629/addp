import axios from 'axios'
import { getAccessToken } from '../auth/authSession.js'
import { parseLocator } from '../types/resourceLocator.js'

const createAuthenticatedAxios = () => {
  const instance = axios.create()
  instance.interceptors.request.use(
    (config) => {
      const token = getAccessToken()
      if (token) {
        config.headers.Authorization = `Bearer ${token}`
      }
      return config
    },
    (error) => Promise.reject(error)
  )
  return instance
}

const authenticatedAxios = createAuthenticatedAxios()

/**
 * 检测表资源的空间元数据。
 *
 * Meta API 调用必须传入 item_id，或传入带 item_id 的 ResourceLocator。
 * 业务模块不应再通过 engine_id/schema/table 拼 catalog_path 查找资源。
 *
 * @param {string} apiBaseUrl API 基础 URL
 * @param {{ locator?: string, item_id?: number, itemId?: number }} params 资源身份参数
 * @returns {Promise<{
 *   has_geometry: boolean,
 *   geometry_column: string|null,
 *   srid: number|null,
 *   geometry_type: string|null,
 *   geometry_types: string[],
 *   extent: Array<number>|null,
 *   fields: Object[]|null
 * }>}
 */
export async function detectTableMetadata(apiBaseUrl, params = {}) {
  try {
    if (!apiBaseUrl.includes('/meta')) {
      throw new Error('detectTableMetadata only supports Meta API')
    }

    const parsed = params.locator ? parseLocator(params.locator) : null
    const itemId = params.item_id || params.itemId || parsed?.itemId
    if (!itemId) {
      throw new Error('item_id or locator with item_id is required for Meta spatial metadata detection')
    }

    const response = await authenticatedAxios.get(`${apiBaseUrl}/items/${itemId}/spatial`)
    const data = response.data.data || response.data
    const geometryTypes = data.geometry_types || (data.geometry_type ? [data.geometry_type] : [])
    const hasGeometry = !!data.geometry_column || geometryTypes.length > 0

    return {
      has_geometry: hasGeometry,
      geometry_column: data.geometry_column || null,
      srid: data.srid || null,
      geometry_type: geometryTypes[0] || null,
      geometry_types: geometryTypes,
      extent: data.extent || null,
      fields: data.fields || null
    }
  } catch (error) {
    console.error('[ResourceCapabilityAPI] detectTableMetadata failed:', error)
    return {
      has_geometry: false,
      geometry_column: null,
      srid: null,
      geometry_type: null,
      geometry_types: [],
      extent: null,
      fields: null
    }
  }
}
