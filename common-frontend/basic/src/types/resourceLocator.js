/**
 * ADDP 资源定位符 (Resource Locator) 工具
 *
 * 使用 addp:// 协议的 URI 系统来唯一标识平台中的任何资源
 *
 * URI 格式: addp://engine/{engine_id}/path/{resource_path}?type={type}&node_id={node_id}&item_id={item_id}
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
  GRAPH: 'graph',
  OBJECT: 'object',
  FILE: 'file',
  DIRECTORY: 'directory',
  PREFIX: 'prefix',
  DATABASE: 'database',
  SCHEMA: 'schema',
  BUCKET: 'bucket',
  ROOT: 'root',
  SERVER: 'server',
  SERVICE: 'service',
  DIR: 'dir',
  UNKNOWN: 'unknown'
}

/**
 * 资源定位符
 * @typedef {Object} ResourceLocator
 * @property {number} engineId - 引擎 ID
 * @property {string[]} path - 资源路径（如 ["public", "users"]）
 * @property {string} type - catalog 术语（table/collection/object/file/directory/prefix/root/server/service等）
 * @property {number} [nodeId] - 可选：MetaNode ID
 * @property {number} [itemId] - 可选：MetaItem ID
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

    const nodeIdStr = url.searchParams.get('node_id')
    const itemIdStr = url.searchParams.get('item_id')
    const nodeId = nodeIdStr ? parseInt(nodeIdStr) : undefined
    const itemId = itemIdStr ? parseInt(itemIdStr) : undefined
    if (nodeIdStr && (isNaN(nodeId) || nodeId <= 0)) {
      throw new Error(`Invalid node_id: ${nodeIdStr}`)
    }
    if (itemIdStr && (isNaN(itemId) || itemId <= 0)) {
      throw new Error(`Invalid item_id: ${itemIdStr}`)
    }
    if (nodeId !== undefined && itemId !== undefined) {
      throw new Error('node_id and item_id are mutually exclusive')
    }

    return {
      engineId,
      path,
      type,
      nodeId,
      itemId
    }
  } catch (error) {
    if (error instanceof TypeError && error.message.includes('Invalid URL')) {
      throw new Error(`Invalid URI: ${uri}`)
    }
    throw error
  }
}

/**
 * 容错解析 ResourceLocator URI。
 *
 * 适用于 UI 展示、编辑回填等不应因无效 locator 中断页面渲染的场景。
 *
 * @param {string} uri - ResourceLocator URI 字符串
 * @returns {ResourceLocator} 解析后的资源定位符，解析失败时返回空定位符
 */
export function parseLocatorSafe(uri) {
  try {
    return parseLocator(uri)
  } catch {
    return {
      engineId: 0,
      path: [],
      type: '',
      nodeId: undefined,
      itemId: undefined
    }
  }
}

/**
 * 从 ResourceTreePicker selection 中读取 locator path。
 *
 * @param {Object} selection - ResourceTreePicker 选择结果
 * @returns {string[]} locator path，解析失败时返回空数组
 */
export function locatorPathFromSelection(selection) {
  const locator = selection?.identity?.locator
  return parseLocatorSafe(locator).path || []
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
 *   itemId: 100
 * })
 * // 'addp://engine/1/path/public/users?type=table&item_id=100'
 */
export function buildLocator(locator) {
  // 编码路径
  const encodedPath = locator.path.map(encodeURIComponent).join('/')

  // 构建 URI
  let uri = `addp://engine/${locator.engineId}/path/${encodedPath}?type=${locator.type}`

  if (locator.nodeId !== undefined && locator.nodeId !== null && locator.itemId !== undefined && locator.itemId !== null) {
    throw new Error('nodeId and itemId are mutually exclusive')
  }

  if (locator.nodeId !== undefined && locator.nodeId !== null) {
    uri += `&node_id=${locator.nodeId}`
  }
  if (locator.itemId !== undefined && locator.itemId !== null) {
    uri += `&item_id=${locator.itemId}`
  }

  return uri
}

/**
 * 构建引擎 catalog root ResourceLocator URI。
 *
 * @param {number|Object} engineOrID - 引擎 ID 或 engine 对象
 * @param {string} [type] - root 类型；未传入时从 Engine Catalog Model 读取
 * @returns {string} catalog root locator
 */
export function engineRootLocator(engineOrID, type = '') {
  const engine = typeof engineOrID === 'object' && engineOrID !== null ? engineOrID : null
  const engineId = Number(engine?.id || engine?.engine_id || engineOrID || 0)
  const rootType = String(type || catalogRootTypeForEngine(engine) || 'root').trim()
  return buildLocator({
    engineId,
    path: [],
    type: rootType
  })
}

export function catalogRootTypeForEngine(engine) {
  let capabilities = engine?.capabilities
  if (typeof capabilities === 'string') {
    try {
      capabilities = JSON.parse(capabilities)
    } catch {
      capabilities = null
    }
  }
  return String(
    engine?.catalog_model?.root_term ||
    capabilities?.storage?.catalog_model?.root_term ||
    'root'
  ).trim().toLowerCase()
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
    case ResourceType.FILE:
    case ResourceType.DIRECTORY:
    case ResourceType.PREFIX:
    case ResourceType.BUCKET:
    case ResourceType.DIR:
    case ResourceType.ROOT:
    case ResourceType.SERVER:
    case ResourceType.SERVICE:
      // 文件、对象存储和结构根按 slash 路径语义展示
      return getPathString(locator)

    default:
      return getPathString(locator)
  }
}

/**
 * 格式化 ResourceLocator 的 UI 展示路径。
 *
 * 按资源语义展示路径；表、集合和图使用点号，其余层级使用斜杠。
 *
 * @param {string} uri - ResourceLocator URI 字符串
 * @param {Object} options - 引擎和目标资源展示事实
 * @returns {string} 展示路径
 */
export function formatLocatorDisplayPath(uri, options = {}) {
  const locator = parseLocatorSafe(uri)
  const path = [...(locator.path || []), ...(options.appendedPath || [])].filter(segment => String(segment).trim() !== '')
  if (path.length === 0) return ''
  const resourceType = String(options.resourceType || locator.type || '').trim().toLowerCase()
  const separator = [ResourceType.TABLE, ResourceType.COLLECTION, ResourceType.GRAPH].includes(resourceType) ? '.' : '/'
  return path.join(separator)
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
    case ResourceType.FILE:
      parentType = ResourceType.DIRECTORY
      break
    case ResourceType.PREFIX:
      parentType = ResourceType.DIRECTORY
      break
    default:
      parentType = ResourceType.DIRECTORY
  }

  return {
    engineId: locator.engineId,
    path: parentPath,
    type: parentType
    // 父节点的 nodeId 需要单独查询
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
    nodeId: locator.nodeId,
    itemId: locator.itemId
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
