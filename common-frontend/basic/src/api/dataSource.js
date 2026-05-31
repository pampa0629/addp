/**
 * 数据源选择器 API 模块
 *
 * 封装数据源相关的 API 调用，供 DataSourceSelector 组件使用
 * 支持多种引擎类型（PostgreSQL、MySQL、MinIO、S3 等）
 */

import axios from 'axios'
import { parseLocator as parseResourceLocator } from '../types/resourceLocator.js'
export { getEngineIconName } from '../utils/engineDisplay.js'

/**
 * 创建带有认证支持的 axios 实例
 * 自动从 localStorage 读取 token 并添加到请求头
 */
const createAuthenticatedAxios = () => {
  const instance = axios.create()

  // 请求拦截器 - 添加 Authorization header
  instance.interceptors.request.use(
    (config) => {
      const token = localStorage.getItem('token')
      if (token) {
        config.headers.Authorization = `Bearer ${token}`
      }
      return config
    },
    (error) => Promise.reject(error)
  )

  return instance
}

// 使用认证 axios 实例
const authenticatedAxios = createAuthenticatedAxios()

/**
 * 规范化树节点格式
 * 将标准 TreeNode 转换为组件期望的格式
 *
 * @param {Array|Object} nodes - 节点或节点数组
 * @returns {Array} 规范化的节点数组
 */
const normalizeTreeNodes = (nodes) => {
  if (!nodes) return []
  const nodeArray = Array.isArray(nodes) ? nodes : [nodes]

  return nodeArray.map(node => {
    const metadata = node.metadata || {}
    const normalized = {
      id: (typeof node.id === 'string' && node.id.startsWith('addp://')) ? node.id : `node-${node.id}`, // TreeNode 的 id 已经是 locator URI
      label: node.label || node.name || node.full_name || 'Unnamed',
      type: node.type || node.node_type || 'unknown',
      icon: getNodeIcon(node.type || node.node_type),
      metadata: {
        ...metadata,
        node_id: metadata.node_id || (typeof node.id === 'number' ? node.id : undefined),
        item_id: metadata.item_id || node.item_id,
        engine_id: metadata.engine_id || node.engine_id,
        tenant_id: metadata.tenant_id || node.tenant_id,
        full_name: metadata.full_name || node.full_name,
        scan_status: metadata.scan_status || node.scan_status,
        scanned_at: metadata.scanned_at || node.scanned_at,
        item_count: metadata.item_count || node.item_count,
        total_size_bytes: metadata.total_size_bytes || node.total_size_bytes
      },
      // 判断是否有子节点
      hasChildren: node.hasChildren || ['schema', 'database', 'bucket', 'directory'].includes(node.type || node.node_type),
      locator: node.locator // 保留 locator 字段（TreeNode 格式）
    }

    // 递归处理子节点
    if (node.children && node.children.length > 0) {
      normalized.children = normalizeTreeNodes(node.children)
    }

    return normalized
  })
}

/**
 * 根据节点类型获取图标名称
 */
const getNodeIcon = (nodeType) => {
  const iconMap = {
    'schema': 'Folder',
    'database': 'Coin',
    'bucket': 'Folder',
    'directory': 'Folder',
    'table': 'Document',
    'collection': 'Document',
    'object': 'Document',
    'lake_table': 'DataBoard'
  }
  return iconMap[nodeType] || 'Document'
}

/**
 * 获取存储引擎列表
 *
 * @param {string} apiBaseUrl - API 基础 URL，如 '/api/service'
 * @param {Object} options - 可选参数
 * @param {string[]} options.engineTypes - 过滤的引擎类型列表
 * @returns {Promise<Engine[]>} 引擎列表
 *
 * @typedef {Object} Engine
 * @property {number} id - 引擎 ID
 * @property {string} name - 引擎名称
 * @property {string} engine_type - 引擎类型（postgresql、mysql、minio、s3 等）
 * @property {number} tenant_id - 租户 ID
 * @property {Object} config - 引擎配置
 *
 * @example
 * const engines = await getEngines('/api/service')
 * // [{ id: 1, name: 'pg_default', engine_type: 'postgresql', ... }]
 */
export async function getEngines(apiBaseUrl, options = {}) {
  const { engineTypes } = options

  try {
    const url = `${apiBaseUrl}/engines`
    const response = await authenticatedAxios.get(url)

    // 按照 ADDP API 规范,响应格式为 { data: [...] }
    let engines = response.data.data || response.data

    // 兼容性处理：Meta 模块返回 resource_type，System 模块返回 engine_type
    // 统一转换为 engine_type
    engines = engines.map(engine => ({
      ...engine,
      engine_type: engine.engine_type || engine.resource_type
    }))

    // 如果指定了引擎类型过滤
    if (engineTypes && engineTypes.length > 0) {
      engines = engines.filter(engine =>
        engineTypes.includes(engine.engine_type)
      )
    }

    return engines
  } catch (error) {
    console.error('[DataSourceAPI] getEngines failed:', error)
    throw new Error(`获取引擎列表失败: ${error.response?.data?.error || error.message}`)
  }
}

/**
 * 获取引擎的元数据树结构
 *
 * @param {string} apiBaseUrl - API 基础 URL
 * @param {number} engineId - 引擎 ID
 * @param {Object} options - 可选参数
 * @param {number} options.expandDepth - 展开深度，默认 2（schema + table）
 * @returns {Promise<TreeNode>} 树节点
 *
 * @typedef {Object} TreeNode
 * @property {string} id - 节点 ID
 * @property {string} label - 节点显示名称
 * @property {string} type - 节点类型（engine、schema、table、bucket、object 等）
 * @property {string} icon - 图标名称
 * @property {string} locator - 资源定位符（addp://engine/1/path/...）
 * @property {boolean} hasChildren - 是否有子节点
 * @property {TreeNode[]} children - 子节点列表
 * @property {Object} metadata - 节点元数据
 * @property {number} engineId - 所属引擎 ID
 * @property {string} engineType - 引擎类型
 *
 * @example
 * const tree = await getEngineTree('/api/service', 1, { expandDepth: 2 })
 * // { id: 'node_1', label: 'pg_default', type: 'engine', children: [...] }
 */
export async function getEngineTree(apiBaseUrl, engineId, options = {}) {
  const { expandDepth = 2 } = options

  try {
    const url = `${apiBaseUrl}/engines/${engineId}/tree`
    const response = await authenticatedAxios.get(url, {
      params: { expand_depth: expandDepth }
    })

    // 按照 ADDP API 规范,响应格式为 { data: {...} }
    const data = response.data.data || response.data

    if (data.children) {
      const normalizedChildren = normalizeTreeNodes(data.children)
      return {
        children: normalizedChildren
      }
    }

    // 如果是单个节点，也需要规范化
    return normalizeTreeNodes([data])[0]
  } catch (error) {
    console.error('[DataSourceAPI] getEngineTree failed:', error)
    throw new Error(`获取引擎树失败: ${error.response?.data?.error || error.message}`)
  }
}

/**
 * 懒加载节点的子节点
 *
 * @param {string} apiBaseUrl - API 基础 URL
 * @param {string} nodeId - 节点 ID
 * @returns {Promise<TreeNode[]>} 子节点列表
 *
 * @example
 * const children = await getNodeChildren('/api/service', 'node_123')
 * // [{ id: 'node_124', label: 'users', type: 'table', ... }]
 */
export async function getNodeChildren(apiBaseUrl, nodeId) {
  try {
    const url = `${apiBaseUrl}/nodes/${nodeId}/children`
    const response = await authenticatedAxios.get(url)

    // 按照 ADDP API 规范,响应格式为 { data: [...] }
    const children = response.data.data || response.data || []

    // 规范化节点格式
    return normalizeTreeNodes(children)
  } catch (error) {
    console.error('[DataSourceAPI] getNodeChildren failed:', error)
    throw new Error(`加载子节点失败: ${error.response?.data?.error || error.message}`)
  }
}

/**
 * 检测表的元数据（特别是几何列信息）
 *
 * @param {string} apiBaseUrl - API 基础 URL
 * @param {TableParams} params - 表参数
 * @returns {Promise<GeometryDetectionResult>} 检测结果
 *
 * @typedef {Object} TableParams
 * @property {number} engine_id - 引擎 ID
 * @property {string} schema - Schema 名称
 * @property {string} table - 表名称
 *
 * @typedef {Object} GeometryDetectionResult
 * @property {boolean} has_geometry - 是否有几何列
 * @property {string} geometry_column - 几何列名称
 * @property {number} srid - 空间参考系统标识符
 * @property {string} geometry_type - 几何类型（POINT、LINESTRING、POLYGON 等）
 * @property {Array<number>} extent - 数据范围 [minX, minY, maxX, maxY]
 * @property {Object[]} fields - 字段列表（可选）
 *
 * @example
 * const result = await detectTableMetadata('/api/service', {
 *   engine_id: 1,
 *   schema: 'public',
 *   table: 'users'
 * })
 * // { has_geometry: true, geometry_column: 'geom', srid: 4326, ... }
 */
export async function detectTableMetadata(apiBaseUrl, params) {
  try {
    // 根据 API 基础 URL 选择正确的端点
    // Meta 模块先按 catalog path 定位 item，再按 item_id 查询空间元数据。
    // Service 模块: /api/service/tables/spatial-metadata
    const isMeta = apiBaseUrl.includes('/meta')
    const catalogPath = params.schema ? `${params.schema}.${params.table}` : params.table
    let url = `${apiBaseUrl}/tables/spatial-metadata`
    let requestParams = params
    if (isMeta) {
      const itemResponse = await authenticatedAxios.get(`${apiBaseUrl}/items/by-catalog-path`, {
        params: {
          engine_id: params.engine_id,
          catalog_path: catalogPath
        }
      })
      const item = itemResponse.data.data || itemResponse.data
      url = `${apiBaseUrl}/items/${item.id}/spatial`
      requestParams = {}
    }

    const response = await authenticatedAxios.get(url, { params: requestParams })

    // 按照 ADDP API 规范,响应格式为 { data: {...} }
    const data = response.data.data || response.data

    // 兼容性处理：API 可能返回 geometry_types (数组) 或 geometry_type (字符串)
    const geometryTypes = data.geometry_types || (data.geometry_type ? [data.geometry_type] : [])
    const hasGeometry = !!data.geometry_column || (geometryTypes && geometryTypes.length > 0)

    return {
      has_geometry: hasGeometry,
      geometry_column: data.geometry_column || null,
      srid: data.srid || null,
      geometry_type: geometryTypes[0] || null,  // 取第一个几何类型
      geometry_types: geometryTypes,  // 保留完整的类型数组
      extent: data.extent || null,
      fields: data.fields || null
    }
  } catch (error) {
    console.error('[DataSourceAPI] detectTableMetadata failed:', error)
    // 如果检测失败，返回默认值（无几何列）
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
 * 解析 locator 字符串，提取引擎 ID 和路径信息
 * （使用 resourceLocator 模块的标准实现）
 *
 * @param {string} locator - 资源定位符（addp://engine/1/path/public/users?type=table）
 * @returns {Object|null} 解析结果
 *
 * @example
 * const parsed = parseLocator('addp://engine/1/path/public/users?type=table')
 * // { engineId: 1, path: ['public', 'users'], type: 'table' }
 */
export function parseLocator(locator) {
  if (!locator || typeof locator !== 'string') {
    return null
  }

  try {
    return parseResourceLocator(locator)
  } catch (error) {
    console.error('[DataSourceAPI] parseLocator failed:', error)
    return null
  }
}

/**
 * 从 TreeNode 提取数据源选择信息
 *
 * @param {TreeNode} node - 树节点
 * @param {Object} options - 可选参数
 * @param {boolean} options.includeGeometry - 是否包含几何信息（需要先调用 detectTableMetadata）
 * @returns {DataSourceSelection} 数据源选择信息
 *
 * @typedef {Object} DataSourceSelection
 * @property {number} engineId - 引擎 ID
 * @property {string} engineName - 引擎名称
 * @property {string} engineType - 引擎类型
 * @property {string} schema - Schema 名称（数据库）或 bucket 名称（对象存储）
 * @property {string} tableName - 表名称或对象路径
 * @property {string} fullName - 完整名称（schema.table 或 bucket/path）
 * @property {string} locator - 资源定位符
 * @property {string} nodeType - 节点类型
 * @property {boolean} hasGeometry - 是否有几何列
 * @property {string} geometryColumn - 几何列名称
 * @property {number} srid - 空间参考系统标识符
 * @property {string} geometryType - 几何类型
 * @property {Object} metadata - 节点元数据
 *
 * @example
 * const selection = extractDataSourceSelection(tableNode)
 * // { engineId: 1, schema: 'public', tableName: 'users', fullName: 'public.users', ... }
 */
export function extractDataSourceSelection(node, options = {}) {
  const { includeGeometry = false } = options

  if (!node) {
    return null
  }

  // 解析 locator 获取路径信息
  const parsed = parseLocator(node.locator)
  if (!parsed) {
    console.warn('[DataSourceAPI] Failed to parse locator:', node.locator)
    return null
  }

  const { engineId, path } = parsed

  // 根据节点类型提取信息
  let schema = ''
  let tableName = ''
  let fullName = ''

  if (node.type === 'table' || node.type === 'lake_table') {
    // 数据库表 / 湖表: path = [schema, table]
    schema = path[0] || ''
    tableName = path[path.length - 1] || ''
    fullName = `${schema}.${tableName}`
  } else if (node.type === 'object') {
    // 对象存储: path = [bucket, ...path]
    schema = path[0] || ''  // bucket 作为 schema
    tableName = path.slice(1).join('/') || ''  // 对象路径
    fullName = `${schema}/${tableName}`
  } else {
    // 其他类型（schema、bucket 等）
    schema = path[0] || ''
    tableName = ''
    fullName = path.join('/')
  }

  const selection = {
    engineId,
    engineName: node.metadata?.engineName || '',
    engineType: node.engineType || '',
    schema,
    tableName,
    fullName,
    locator: node.locator,
    nodeType: node.type,
    metadata: node.metadata || {}
  }

  // 如果需要包含几何信息
  if (includeGeometry && node.metadata) {
    selection.hasGeometry = node.metadata.has_geometry || false
    selection.geometryColumn = node.metadata.geometry_column || null
    selection.srid = node.metadata.srid || null
    selection.geometryType = node.metadata.geometry_type || null
  } else {
    selection.hasGeometry = false
    selection.geometryColumn = null
    selection.srid = null
    selection.geometryType = null
  }

  return selection
}
