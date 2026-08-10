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

/**
 * 获取数据项字段，用于查询编辑器等需要字段级补全的场景。
 * 只接受 Meta 资源身份，不根据引擎、Schema、表名自行拼接查询。
 *
 * @param {string} apiBaseUrl API 基础 URL
 * @param {{ locator?: string, item_id?: number, itemId?: number }} params 资源身份参数
 * @returns {Promise<Array<{name: string, type?: object|string, native_type?: string, comment?: string}>>}
 */
export async function getResourceFields(apiBaseUrl, params = {}) {
  if (!apiBaseUrl.includes('/meta')) {
    throw new Error('getResourceFields only supports Meta API')
  }
  const parsed = params.locator ? parseLocator(params.locator) : null
  const itemId = params.item_id || params.itemId || parsed?.itemId
  if (!itemId) {
    throw new Error('item_id or locator with item_id is required for Meta field lookup')
  }
  const response = await authenticatedAxios.get(`${apiBaseUrl}/items/${itemId}/fields`)
  const data = response.data?.data || response.data
  return Array.isArray(data) ? data : []
}

/**
 * 按引擎和 catalog path 查询 Meta 数据项。
 *
 * @param {string} apiBaseUrl API 基础 URL
 * @param {{ engine_id: number|string, catalog_path: string }} params 查询参数
 * @returns {Promise<Object>}
 */
export async function getResourceItemByCatalogPath(apiBaseUrl, params = {}) {
  if (!apiBaseUrl.includes('/meta')) {
    throw new Error('getResourceItemByCatalogPath only supports Meta API')
  }
  const engineId = params.engine_id || params.engineId
  const catalogPath = String(params.catalog_path || params.catalogPath || '').trim()
  if (!engineId || !catalogPath) {
    throw new Error('engine_id and catalog_path are required for Meta item lookup')
  }
  const response = await authenticatedAxios.get(`${apiBaseUrl}/items/by-catalog-path`, {
    params: { engine_id: engineId, catalog_path: catalogPath }
  })
  return response.data?.data || response.data
}
