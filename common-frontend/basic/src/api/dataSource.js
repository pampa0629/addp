/**
 * 数据源选择器 API 模块
 *
 * 封装数据源相关的 API 调用，供 DataSourceSelector 组件使用
 * 支持多种引擎类型（PostgreSQL、MySQL、MinIO、S3 等）
 */

import axios from 'axios'
import { parseLocator as parseResourceLocator } from '../types/resourceLocator.js'

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

    let engines = response.data

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

    return response.data
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

    // 后端直接返回数组
    return response.data || []
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
    const url = `${apiBaseUrl}/tables/metadata`
    const response = await authenticatedAxios.get(url, { params })

    return {
      has_geometry: response.data.has_geometry || false,
      geometry_column: response.data.geometry_column || null,
      srid: response.data.srid || null,
      geometry_type: response.data.geometry_type || null,
      extent: response.data.extent || null,
      fields: response.data.fields || null
    }
  } catch (error) {
    console.error('[DataSourceAPI] detectTableMetadata failed:', error)
    // 如果检测失败，返回默认值（无几何列）
    return {
      has_geometry: false,
      geometry_column: null,
      srid: null,
      geometry_type: null,
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

  if (node.type === 'table') {
    // 数据库表: path = [schema, table]
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
