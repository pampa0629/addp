/**
 * ADDP 资源定位符 (Resource Locator) 工具
 *
 * 使用 addp:// 协议的 URI 系统来唯一标识平台中的任何资源
 *
 * URI 格式: addp://engine/{engine_id}/path/{resource_path}?type={type}&meta_id={meta_id}
 *
 * 示例:
 *   - PostgreSQL 表: addp://engine/1/path/public/users?type=table
 *   - MongoDB 集合: addp://engine/2/path/business/orders?type=collection
 *   - MinIO 对象: addp://engine/3/path/uploads/2024/geo/data.shp?type=object
 *   - MinIO 目录: addp://engine/3/path/uploads/2024?type=directory
 */

/**
 * 资源类型枚举
 */
export const ResourceType = {
  TABLE: 'table',
  COLLECTION: 'collection',
  OBJECT: 'object',
  DIRECTORY: 'directory',
  DATABASE: 'database',
  SCHEMA: 'schema',
  BUCKET: 'bucket',
  UNKNOWN: 'unknown'
}

/**
 * 资源定位符
 * @typedef {Object} ResourceLocator
 * @property {number} engineId - 引擎 ID
 * @property {string[]} path - 资源路径（如 ["public", "users"]）
 * @property {string} type - 资源类型（table/collection/object/directory等）
 * @property {number} [metaId] - 可选：Meta 模块的节点 ID
 */

/**
 * 解析 ResourceLocator URI
 *
 * @param {string} uri - ResourceLocator URI 字符串
 * @returns {ResourceLocator} 解析后的资源定位符
 * @throws {Error} URI 格式错误时抛出异常
 *
 * @example
 * const locator = parseLocator('addp://engine/1/path/public/users?type=table')
 * // { engineId: 1, path: ['public', 'users'], type: 'table' }
 */
export function parseLocator(uri) {
  try {
    const url = new URL(uri)

    // 验证协议
    if (url.protocol !== 'addp:') {
      throw new Error(`Invalid protocol: expected 'addp:', got '${url.protocol}'`)
    }

    // 解析路径: //engine/{id}/path/{path}
    // URL 对象会将 //engine 解析为 hostname，我们需要从 hostname 和 pathname 组合解析
    const pathStr = (url.hostname + url.pathname).replace(/^\/+|\/+$/g, '')
    const parts = pathStr.split('/')

    if (parts.length < 3 || parts[0] !== 'engine' || parts[2] !== 'path') {
      throw new Error(`Invalid path format: expected 'engine/{id}/path/{path}', got '${pathStr}'`)
    }

    // 解析 engine_id
    const engineId = parseInt(parts[1])
    if (isNaN(engineId)) {
      throw new Error(`Invalid engine_id: ${parts[1]}`)
    }

    // 解析资源路径（URL 解码）
    const path = []
    if (parts.length > 3) {
      for (let i = 3; i < parts.length; i++) {
        const decoded = decodeURIComponent(parts[i])
        if (decoded) {
          path.push(decoded)
        }
      }
    }

    // 解析查询参数
    const type = url.searchParams.get('type')
    if (!type) {
      throw new Error('Missing required parameter: type')
    }

    const metaIdStr = url.searchParams.get('meta_id')
    const metaId = metaIdStr ? parseInt(metaIdStr) : undefined

    return {
      engineId,
      path,
      type,
      metaId
    }
  } catch (error) {
    if (error instanceof TypeError && error.message.includes('Invalid URL')) {
      throw new Error(`Invalid URI: ${uri}`)
    }
    throw error
  }
}

/**
 * 构建 ResourceLocator URI
 *
 * @param {ResourceLocator} locator - 资源定位符对象
 * @returns {string} ResourceLocator URI
 *
 * @example
 * const uri = buildLocator({
 *   engineId: 1,
 *   path: ['public', 'users'],
 *   type: 'table',
 *   metaId: 100
 * })
 * // 'addp://engine/1/path/public/users?type=table&meta_id=100'
 */
export function buildLocator(locator) {
  // 编码路径
  const encodedPath = locator.path.map(encodeURIComponent).join('/')

  // 构建 URI
  let uri = `addp://engine/${locator.engineId}/path/${encodedPath}?type=${locator.type}`

  if (locator.metaId !== undefined && locator.metaId !== null) {
    uri += `&meta_id=${locator.metaId}`
  }

  return uri
}

/**
 * 获取路径字符串
 *
 * @param {ResourceLocator} locator - 资源定位符对象
 * @returns {string} 路径字符串（如 "public/users"）
 *
 * @example
 * const pathStr = getPathString({ path: ['public', 'users'] })
 * // 'public/users'
 */
export function getPathString(locator) {
  return locator.path.join('/')
}

/**
 * 获取完整名称
 * 根据资源类型返回格式化的名称
 *
 * @param {ResourceLocator} locator - 资源定位符对象
 * @returns {string} 完整名称
 *
 * @example
 * // 表：schema.table 格式
 * getFullName({ path: ['public', 'users'], type: 'table' })
 * // 'public.users'
 *
 * // 对象：bucket/path 格式
 * getFullName({ path: ['uploads', '2024', 'data.shp'], type: 'object' })
 * // 'uploads/2024/data.shp'
 */
export function getFullName(locator) {
  switch (locator.type) {
    case ResourceType.TABLE:
    case ResourceType.COLLECTION:
      // schema.table 或 database.collection 格式
      if (locator.path.length >= 2) {
        return locator.path.slice(-2).join('.')
      }
      return getPathString(locator)

    case ResourceType.OBJECT:
    case ResourceType.DIRECTORY:
      // bucket/path/to/file 格式
      return getPathString(locator)

    default:
      return getPathString(locator)
  }
}

/**
 * 获取路径的最后一段
 *
 * @param {ResourceLocator} locator - 资源定位符对象
 * @returns {string} 最后一段路径（如 "users"）
 *
 * @example
 * getLastSegment({ path: ['public', 'users'] })
 * // 'users'
 */
export function getLastSegment(locator) {
  if (!locator.path || locator.path.length === 0) {
    return ''
  }
  return locator.path[locator.path.length - 1]
}

/**
 * 获取父路径的 ResourceLocator
 *
 * @param {ResourceLocator} locator - 资源定位符对象
 * @returns {ResourceLocator|null} 父路径的 ResourceLocator（根节点返回 null）
 *
 * @example
 * const parent = getParentLocator({
 *   engineId: 1,
 *   path: ['public', 'users'],
 *   type: 'table'
 * })
 * // { engineId: 1, path: ['public'], type: 'schema' }
 */
export function getParentLocator(locator) {
  if (!locator.path || locator.path.length === 0) {
    return null
  }

  const parentPath = locator.path.slice(0, -1)

  // 推断父节点类型
  let parentType
  switch (locator.type) {
    case ResourceType.TABLE:
      parentType = ResourceType.SCHEMA
      break
    case ResourceType.COLLECTION:
      parentType = ResourceType.DATABASE
      break
    case ResourceType.OBJECT:
      parentType = ResourceType.DIRECTORY
      break
    default:
      parentType = ResourceType.DIRECTORY
  }

  return {
    engineId: locator.engineId,
    path: parentPath,
    type: parentType
    // 父节点的 metaId 需要单独查询
  }
}

/**
 * 从旧的 3 参数格式转换为 ResourceLocator
 * 用于兼容旧代码
 *
 * @param {number} engineId - 引擎 ID
 * @param {string} schema - Schema/Database 名称
 * @param {string} table - 表/集合名称
 * @param {string} type - 资源类型
 * @returns {ResourceLocator} 资源定位符对象
 *
 * @example
 * const locator = fromLegacyParams(1, 'public', 'users', 'table')
 * // { engineId: 1, path: ['public', 'users'], type: 'table' }
 */
export function fromLegacyParams(engineId, schema, table, type) {
  const path = [schema, table].filter(Boolean)
  return {
    engineId,
    path,
    type
  }
}

/**
 * 克隆 ResourceLocator（深拷贝）
 *
 * @param {ResourceLocator} locator - 资源定位符对象
 * @returns {ResourceLocator} 克隆的资源定位符对象
 */
export function cloneLocator(locator) {
  return {
    engineId: locator.engineId,
    path: [...locator.path],
    type: locator.type,
    metaId: locator.metaId
  }
}

/**
 * 比较两个 ResourceLocator 是否相等
 *
 * @param {ResourceLocator} a - 资源定位符 A
 * @param {ResourceLocator} b - 资源定位符 B
 * @returns {boolean} 是否相等
 */
export function isLocatorEqual(a, b) {
  if (!a || !b) return false
  if (a.engineId !== b.engineId || a.type !== b.type) return false
  if (a.path.length !== b.path.length) return false

  for (let i = 0; i < a.path.length; i++) {
    if (a.path[i] !== b.path[i]) return false
  }

  return true
}
