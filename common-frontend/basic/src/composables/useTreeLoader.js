/**
 * 树增量加载 Composable
 * 封装增量加载、缓存、错误处理逻辑
 *
 * @example
 * ```js
 * import { useTreeLoader } from '@addp/common-frontend'
 * import client from '@/api/client'
 *
 * const treeLoader = useTreeLoader(client, {
 *   enableCache: true,
 *   cacheOptions: { maxAge: 5 * 60 * 1000 }
 * })
 *
 * // 增量加载子节点
 * const children = await treeLoader.loadNodeChildren('addp://engine/1/path/public?type=schema', 1)
 *
 * // 搜索节点
 * const results = await treeLoader.searchNodes(1, 'users', { nodeTypes: ['table'], limit: 50 })
 *
 * // 清空缓存
 * treeLoader.clearCache()
 * ```
 */

import { ref } from 'vue'
import { useTreeCache } from './useTreeCache'
import { parseLocator } from '../types/resourceLocator'

/**
 * 创建树加载器
 * @param {Object} apiClient - API 客户端（axios 实例或类似对象）
 * @param {Object} options - 配置选项
 * @param {boolean} options.enableCache - 是否启用缓存，默认 true
 * @param {Object} options.cacheOptions - 缓存配置，传递给 useTreeCache
 * @param {string} options.apiBasePath - API 基础路径，默认为空（使用 `/tree`）
 * @returns {Object} 树加载器
 */
export function useTreeLoader(apiClient, options = {}) {
  const {
    enableCache = true,
    cacheOptions = {},
    apiBasePath = ''
  } = options

  const loading = ref(false)
  const error = ref(null)

  // 使用缓存
  const cache = enableCache ? useTreeCache(cacheOptions) : null

  /**
   * 增量加载节点子节点
   * @param {string} locator - 父节点的 ResourceLocator URI
   * @param {number} expandDepth - 展开深度（默认 1 = 只加载直接子节点）
   * @param {boolean} forceRefresh - 是否强制刷新（忽略缓存）
   * @returns {Promise<Array>} 子节点数组
   */
  const loadNodeChildren = async (locator, expandDepth = 1, forceRefresh = false) => {
    // 1. 检查缓存
    if (!forceRefresh && cache) {
      const cached = cache.getNodeChildrenCache(locator)
      if (cached) {
        console.log(`[useTreeLoader] 使用缓存: ${locator}`)
        return cached
      }
    }

    // 2. 解析 locator
    const loc = parseLocator(locator)
    if (!loc) {
      throw new Error(`Invalid locator: ${locator}`)
    }

    loading.value = true
    error.value = null

    try {
      // 3. 调用 API
      const url = apiBasePath ? `${apiBasePath}/tree/${loc.engineId}/node` : `/tree/${loc.engineId}/node`
      const response = await apiClient.get(url, {
        params: { locator, expand_depth: expandDepth }
      })

      const children = response.children || []

      // 4. 更新缓存
      if (cache) {
        cache.setNodeChildrenCache(locator, children)
      }

      console.log(`[useTreeLoader] 加载成功: ${children.length} 个子节点`)
      return children
    } catch (err) {
      error.value = err
      console.error('[useTreeLoader] 加载失败:', err)
      throw err
    } finally {
      loading.value = false
    }
  }

  /**
   * 搜索节点
   * @param {number} engineId - 引擎 ID
   * @param {string} keyword - 搜索关键词
   * @param {Object} searchOptions - 搜索选项
   * @param {string[]|null} searchOptions.nodeTypes - 节点类型过滤（可选）
   * @param {number} searchOptions.limit - 返回数量限制，默认 50
   * @param {boolean} searchOptions.forceRefresh - 是否强制刷新（忽略缓存）
   * @returns {Promise<Array>} 搜索结果数组
   */
  const searchNodes = async (engineId, keyword, searchOptions = {}) => {
    const { nodeTypes = null, limit = 50, forceRefresh = false } = searchOptions

    if (!keyword || keyword.length < 2) {
      return []
    }

    // 生成缓存 key
    const cacheKey = `${engineId}:${keyword}:${nodeTypes?.join(',') || ''}`

    // 检查缓存
    if (!forceRefresh && cache) {
      const cached = cache.getSearchResultsCache(cacheKey)
      if (cached) {
        console.log(`[useTreeLoader] 使用搜索缓存: ${cacheKey}`)
        return cached
      }
    }

    loading.value = true
    error.value = null

    try {
      const params = { q: keyword, limit }
      if (nodeTypes) {
        params.node_types = Array.isArray(nodeTypes) ? nodeTypes.join(',') : nodeTypes
      }

      const url = apiBasePath ? `${apiBasePath}/tree/${engineId}/search` : `/tree/${engineId}/search`
      const response = await apiClient.get(url, { params })
      const results = response.results || []

      // 更新缓存
      if (cache) {
        cache.setSearchResultsCache(cacheKey, results)
      }

      console.log(`[useTreeLoader] 搜索成功: 找到 ${results.length} 个结果`)
      return results
    } catch (err) {
      error.value = err
      console.error('[useTreeLoader] 搜索失败:', err)
      throw err
    } finally {
      loading.value = false
    }
  }

  return {
    // 状态
    loading,
    error,

    // 操作
    loadNodeChildren,
    searchNodes,
    clearCache: () => cache?.clearCache()
  }
}
