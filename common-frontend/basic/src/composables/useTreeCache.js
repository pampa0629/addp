/**
 * 树节点缓存 Composable
 * 提供节点级缓存、搜索结果缓存、TTL 管理
 *
 * @example
 * ```js
 * import { useTreeCache } from '@addp/common-frontend'
 *
 * const cache = useTreeCache({
 *   maxAge: 5 * 60 * 1000,  // 5分钟过期
 *   maxSize: 100             // 最多100个缓存项
 * })
 *
 * // 设置缓存
 * cache.setNodeChildrenCache('node_id', children)
 *
 * // 获取缓存
 * const cached = cache.getNodeChildrenCache('node_id')
 *
 * // 清空所有缓存
 * cache.clearCache()
 * ```
 */

import { ref } from 'vue'

/**
 * 创建树缓存管理器
 * @param {Object} options - 配置选项
 * @param {number} options.maxAge - 缓存过期时间（毫秒），默认 5 分钟
 * @param {number} options.maxSize - 最大缓存项数量，默认 100
 * @returns {Object} 缓存管理器
 */
export function useTreeCache(options = {}) {
  const {
    maxAge = 5 * 60 * 1000,  // 默认 5 分钟
    maxSize = 100             // 默认 100 个缓存项
  } = options

  // 节点子节点缓存
  const nodeChildrenCache = ref({})

  // 搜索结果缓存
  const searchResultsCache = ref({})

  /**
   * 获取缓存
   * @param {string} key - 缓存键
   * @param {Object} cacheStore - 缓存存储对象
   * @returns {any|null} 缓存数据或 null
   */
  const getCache = (key, cacheStore) => {
    const cache = cacheStore[key]
    if (!cache) return null

    // 检查是否过期
    if (Date.now() - cache.timestamp > maxAge) {
      delete cacheStore[key]
      return null
    }

    return cache.data
  }

  /**
   * 设置缓存
   * @param {string} key - 缓存键
   * @param {any} data - 缓存数据
   * @param {Object} cacheStore - 缓存存储对象
   */
  const setCache = (key, data, cacheStore) => {
    cacheStore[key] = {
      data,
      timestamp: Date.now()
    }

    // 清理过期缓存
    if (Object.keys(cacheStore).length > maxSize) {
      cleanExpiredCache(cacheStore)
    }
  }

  /**
   * 清理过期缓存
   * @param {Object} cacheStore - 缓存存储对象
   */
  const cleanExpiredCache = (cacheStore) => {
    const now = Date.now()
    for (const key in cacheStore) {
      if (now - cacheStore[key].timestamp > maxAge) {
        delete cacheStore[key]
      }
    }
  }

  /**
   * 清空所有缓存
   */
  const clearCache = () => {
    nodeChildrenCache.value = {}
    searchResultsCache.value = {}
  }

  /**
   * 获取节点子节点缓存
   * @param {string} key - 缓存键（通常是 locator）
   * @returns {any|null} 缓存的子节点数据或 null
   */
  const getNodeChildrenCache = (key) => {
    return getCache(key, nodeChildrenCache.value)
  }

  /**
   * 设置节点子节点缓存
   * @param {string} key - 缓存键（通常是 locator）
   * @param {any} data - 子节点数据
   */
  const setNodeChildrenCache = (key, data) => {
    setCache(key, data, nodeChildrenCache.value)
  }

  /**
   * 获取搜索结果缓存
   * @param {string} key - 缓存键（通常是 `engineId:keyword:nodeTypes`）
   * @returns {any|null} 缓存的搜索结果或 null
   */
  const getSearchResultsCache = (key) => {
    return getCache(key, searchResultsCache.value)
  }

  /**
   * 设置搜索结果缓存
   * @param {string} key - 缓存键（通常是 `engineId:keyword:nodeTypes`）
   * @param {any} data - 搜索结果数据
   */
  const setSearchResultsCache = (key, data) => {
    setCache(key, data, searchResultsCache.value)
  }

  return {
    // 原始缓存对象（响应式）
    nodeChildrenCache,
    searchResultsCache,

    // 节点子节点缓存操作
    getNodeChildrenCache,
    setNodeChildrenCache,

    // 搜索结果缓存操作
    getSearchResultsCache,
    setSearchResultsCache,

    // 通用操作
    clearCache
  }
}
