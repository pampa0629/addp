import { defineStore } from 'pinia'
import { parseLocator } from '@addp/common-frontend'
import client from '@/api/client'

/**
 * explorerStore - 数据探查状态管理
 * 基于 ResourceLocator URI 系统
 */
export const useExplorerStore = defineStore('explorer', {
  state: () => ({
    // 引擎列表
    engines: [],
    loadingEngines: false,

    // 每个引擎的资源树（按 engineId 索引）
    engineTrees: {},
    // 记录每个引擎已加载的深度
    engineTreeDepths: {},
    loadingEngineId: null,

    // 节点选择
    selectedLocator: null,
    selectedChildName: '',

    // 展开的节点（使用 locator URI 作为 key）
    expandedLocators: new Set(),

    // 正在刷新的节点
    refreshingLocators: new Set(),

    // 预览数据
    previewData: null,
    activeChildPreviewData: null,
    previewLoading: false,
    childPreviewLoading: false,

    // 分页
    pagination: {
      page: 1,
      pageSize: 20
    },

    // 新增：节点级缓存（增量加载）
    nodeChildrenCache: {}, // { 'locator': { children: [...], timestamp: 123 } }
    searchResultsCache: {}, // { 'engineId:keyword': { results: [...], timestamp: 123 } }
    cacheConfig: {
      maxAge: 5 * 60 * 1000, // 5分钟过期
      maxSize: 100
    }
  }),

  getters: {
    /**
     * 将引擎列表转换为树节点格式
     * 这是资源树的根节点列表
     */
    engineNodes: (state) => {
      return state.engines.map(engine => {
        const locator = `addp://engine/${engine.id}/path/?type=database`
        const tree = state.engineTrees[engine.id]

        return {
          id: locator,
          locator: locator,
          label: engine.name,
          type: 'engine',
          engineId: engine.id,
          engineType: engine.engine_type,
          // 如果已加载树数据，使用树的子节点；否则返回空数组（懒加载）
          children: tree ? tree.children : [],
          // 标记是否已加载
          loaded: !!tree,
          // 标记是否正在加载
          loading: state.loadingEngineId === engine.id
        }
      })
    },

    /**
     * 获取选中的节点
     */
    selectedNode: (state) => {
      if (!state.selectedLocator) return null

      // 在所有引擎树中查找
      for (const engineId in state.engineTrees) {
        const tree = state.engineTrees[engineId]
        const found = findNodeByLocator(tree, state.selectedLocator)
        if (found) return found
      }

      // 检查是否选中的是引擎节点本身
      const engine = state.engines.find(e =>
        state.selectedLocator === `addp://engine/${e.id}/path/?type=database`
      )
      if (engine) {
        return {
          id: state.selectedLocator,
          locator: state.selectedLocator,
          label: engine.name,
          type: 'engine'
        }
      }

      return null
    },

    /**
     * 检查节点是否正在刷新
     */
    isRefreshing: (state) => {
      return (locator) => state.refreshingLocators.has(locator)
    },

    /**
     * 检查节点是否已展开
     */
    isExpanded: (state) => {
      return (locator) => state.expandedLocators.has(locator)
    },

    /**
     * 检查是否正在加载某个引擎
     */
    isLoadingEngine: (state) => {
      return (engineId) => state.loadingEngineId === engineId
    }
  },

  actions: {
    /**
     * 加载引擎列表
     */
    async loadEngines() {
      this.loadingEngines = true
      try {
        const response = await client.get('/manager/engines')
        // API 客户端已经通过 extractData 提取了 response.data
        // 后端返回 { data: engines }，这里的 response 就是 { data: engines }
        this.engines = response.data || []
      } catch (error) {
        console.error('加载引擎列表失败:', error)
        throw error
      } finally {
        this.loadingEngines = false
      }
    },

    /**
     * 加载资源树（懒加载某个引擎的内容）
     * @param {number} engineId - 引擎 ID
     * @param {number} expandDepth - 展开深度
     *   - 2: 只加载到目录层（bucket + directory），适合初始加载
     *   - -1: 加载所有深度（包括对象），用于完整视图
     * @param {boolean} forceRefresh - 是否强制刷新（忽略缓存）
     */
    async loadTree(engineId, expandDepth = 2, forceRefresh = false) {
      const cachedDepth = this.engineTreeDepths[engineId] || 0

      // 如果已经加载过，且当前缓存深度 >= 需要的深度，直接返回缓存
      if (!forceRefresh && this.engineTrees[engineId] && cachedDepth >= expandDepth) {
        console.log(`[ExplorerStore] 使用缓存（引擎 ${engineId}，已缓存深度 ${cachedDepth}）`)
        return this.engineTrees[engineId]
      }

      this.loadingEngineId = engineId

      try {
        console.log(`[ExplorerStore] 加载引擎树（引擎 ${engineId}，深度 ${expandDepth}）`)
        const response = await client.get(`/manager/tree/${engineId}`, {
          params: { expand_depth: expandDepth }
        })
        console.log('[ExplorerStore] API 响应:', response)
        // 后端直接返回 tree 对象（符合 API 规范）
        // API 客户端的 extractData 已提取了 response.data
        const tree = response

        // 获取引擎类型并为所有节点添加 engineType
        const engine = this.engines.find(e => e.id === engineId)
        if (engine && tree) {
          this.addEngineTypeToTree(tree, engine.engine_type)
        }

        // 存储到引擎树缓存中
        this.engineTrees[engineId] = tree
        this.engineTreeDepths[engineId] = expandDepth

        return tree
      } catch (error) {
        console.error('加载资源树失败:', error)
        throw error
      } finally {
        this.loadingEngineId = null
      }
    },

    /**
     * 刷新节点（强制重新加载某个引擎的树）
     */
    async refreshNode(locator) {
      this.refreshingLocators.add(locator)

      try {
        const loc = parseLocator(locator)
        await client.post(`/manager/tree/${loc.engineId}/refresh`, null, {
          params: { locator }
        })

        // 清除缓存并重新加载
        delete this.engineTrees[loc.engineId]
        await this.loadTree(loc.engineId)
      } catch (error) {
        console.error('刷新节点失败:', error)
        throw error
      } finally {
        this.refreshingLocators.delete(locator)
      }
    },

    /**
     * 加载预览数据
     */
    async loadPreview(locator, page = 1, childName = '') {
      this.selectedLocator = locator
      this.pagination.page = page
      const normalizedChildName = childName || ''
      this.selectedChildName = normalizedChildName
      if (normalizedChildName) {
        this.activeChildPreviewData = null
        this.childPreviewLoading = true
      } else {
        this.previewLoading = true
        this.activeChildPreviewData = null
      }

      try {
        const params = {
          locator,
          page,
          page_size: this.pagination.pageSize
        }
        if (normalizedChildName) {
          params.child_name = normalizedChildName
        }
        const response = await client.get('/manager/preview', {
          params
        })
        // 后端直接返回 PreviewResult 对象（符合 API 规范）
        // API 客户端的 extractData 已提取了 response.data
        // PreviewResult: {preview_type, data: TablePreview, metadata}
        // 前端预览插件期望直接收到 TablePreview 格式，所以需要提取 response.data
        const preview = response.data
        if (normalizedChildName) {
          this.activeChildPreviewData = preview
          return preview
        }
        this.previewData = preview
        const defaultSheet = this.defaultExcelSheetName(preview)
        if (defaultSheet) {
          await this.loadPreview(locator, 1, defaultSheet)
        }
        return this.previewData
      } catch (error) {
        console.error('加载预览失败:', error)
        throw error
      } finally {
        if (normalizedChildName) {
          this.childPreviewLoading = false
        } else {
          this.previewLoading = false
        }
      }
    },

    defaultExcelSheetName(preview) {
      const content = preview?.object?.content
      const kind = String(content?.kind || '').toLowerCase()
      const json = content?.json || content?.JSON || {}
      if (kind !== 'excel' || !json || typeof json !== 'object') {
        return ''
      }
      const explicit = json.active_sheet || json.default_sheet
      if (explicit) return explicit
      const sheets = Array.isArray(json.sheets) ? json.sheets : []
      return sheets.find(sheet => sheet?.name)?.name || ''
    },

    /**
     * 选择节点
     */
    selectNode(locator) {
      this.selectedLocator = locator
    },

    /**
     * 展开节点
     */
    expandNode(locator) {
      // 使用替换 Set 的方式确保响应式更新
      this.expandedLocators = new Set([...this.expandedLocators, locator])
    },

    /**
     * 折叠节点
     */
    collapseNode(locator) {
      // 使用替换 Set 的方式确保响应式更新
      // 同时移除当前节点及其所有子节点的展开状态
      const normalizeLocatorPath = (value) => {
        if (!value || typeof value !== 'string') return ''
        return value.split('?')[0] || ''
      }

      const targetPath = normalizeLocatorPath(locator)
      if (!targetPath) {
        this.expandedLocators = new Set(this.expandedLocators)
        return
      }

      const newSet = new Set()
      for (const key of this.expandedLocators) {
        if (!key || typeof key !== 'string') continue

        const currentPath = normalizeLocatorPath(key)
        // 仅保留不在目标节点子树内的展开键
        if (!currentPath.startsWith(targetPath)) {
          newSet.add(key)
        }
      }
      this.expandedLocators = newSet
    },

    /**
     * 切换节点展开状态
     */
    toggleExpand(locator) {
      if (this.expandedLocators.has(locator)) {
        this.collapseNode(locator)
      } else {
        this.expandNode(locator)
      }
    },

    /**
     * 重置状态
     */
    reset() {
      this.selectedLocator = null
      this.selectedChildName = ''
      this.previewData = null
      this.activeChildPreviewData = null
      this.pagination.page = 1
    },

    /**
     * 增量加载节点子节点（新增）
     * @param {string} locator - 父节点的 ResourceLocator URI
     * @param {number} expandDepth - 展开深度（默认 1 = 只加载直接子节点）
     * @param {boolean} forceRefresh - 是否强制刷新（忽略缓存）
     */
    async loadNodeChildren(locator, expandDepth = 1, forceRefresh = false) {
      // 1. 检查缓存
      if (!forceRefresh && this.nodeChildrenCache[locator]) {
        const cache = this.nodeChildrenCache[locator]
        if (Date.now() - cache.timestamp < this.cacheConfig.maxAge) {
          console.log(`[ExplorerStore] 使用节点缓存: ${locator}`)
          return cache.children
        }
      }

      // 2. 解析 locator
      const loc = parseLocator(locator)

      try {
        console.log(`[ExplorerStore] 增量加载子节点: ${locator}, 深度: ${expandDepth}`)

        // 3. 调用后端 API
        const response = await client.get(`/manager/tree/${loc.engineId}/node`, {
          params: { locator, expand_depth: expandDepth }
        })
        // API 客户端已经通过 extractData 提取了 response.data
        // 后端返回 { parent_locator: ..., children: [...] }
        const children = response.children || []

        // 获取引擎类型并为子节点添加 engineType
        const engine = this.engines.find(e => e.id === loc.engineId)
        if (engine && children.length > 0) {
          children.forEach(child => {
            this.addEngineTypeToTree(child, engine.engine_type)
          })
        }

        // 4. 更新缓存
        this.nodeChildrenCache[locator] = {
          children,
          timestamp: Date.now()
        }

        // 5. 更新引擎树（增量更新）
        if (this.engineTrees[loc.engineId]) {
          const updated = this.updateTreeNode(this.engineTrees[loc.engineId], locator, {
            children,
            loaded: true,
            hasChildren: children.length > 0
          })

          if (updated) {
            // 深度克隆引擎树，确保 Vue 3 响应式系统检测到所有层级的变化
            this.engineTrees = {
              ...this.engineTrees,
              [loc.engineId]: deepCloneTree(this.engineTrees[loc.engineId])
            }
            console.log(`[ExplorerStore] 已触发响应式更新`)
          }
        }

        console.log(`[ExplorerStore] 增量加载成功: ${children.length} 个子节点`)
        return children
      } catch (error) {
        console.error('增量加载子节点失败:', error)
        throw error
      }
    },

    /**
     * 搜索资源树节点（新增）
     * @param {number} engineId - 引擎 ID
     * @param {string} keyword - 搜索关键词
     * @param {string[]|null} nodeTypes - 节点类型过滤（可选）
     * @param {number} limit - 返回数量限制
     */
    async searchNodes(engineId, keyword, nodeTypes = null, limit = 50) {
      if (!keyword || keyword.length < 2) {
        return []
      }

      // 生成缓存 key
      const cacheKey = `${engineId}:${keyword}:${nodeTypes?.join(',') || ''}`

      // 检查缓存
      if (this.searchResultsCache[cacheKey]) {
        const cache = this.searchResultsCache[cacheKey]
        if (Date.now() - cache.timestamp < this.cacheConfig.maxAge) {
          console.log(`[ExplorerStore] 使用搜索缓存: ${cacheKey}`)
          return cache.results
        }
      }

      try {
        console.log(`[ExplorerStore] 搜索节点: 引擎 ${engineId}, 关键词 "${keyword}"`)

        const params = { q: keyword, limit }
        if (nodeTypes) {
          params.node_types = Array.isArray(nodeTypes) ? nodeTypes.join(',') : nodeTypes
        }

        const response = await client.get(`/manager/tree/${engineId}/search`, { params })
        // API 客户端已经通过 extractData 提取了 response.data
        // 后端返回 { keyword: ..., total: ..., results: [...] }
        const results = response.results || []
        const total = response.total || 0

        // 更新缓存
        this.searchResultsCache[cacheKey] = {
          results,
          total,
          timestamp: Date.now()
        }

        console.log(`[ExplorerStore] 搜索成功: 找到 ${results.length}/${total} 个结果`)
        return results
      } catch (error) {
        console.error('搜索节点失败:', error)
        throw error
      }
    },

    /**
     * 更新树节点（辅助方法）
     * 在树中查找指定 locator 的节点并更新其属性
     * @param {Object} tree - 树根节点
     * @param {string} locator - 要更新的节点 locator
     * @param {Object} updates - 要更新的属性
     */
    updateTreeNode(tree, locator, updates) {
      if (!tree || !locator) return false

      if (tree.locator === locator) {
        // 找到目标节点，用新对象替换属性，触发 Vue 3 响应式
        Object.assign(tree, updates)
        return true
      }

      // 递归查找子节点，找到后替换父节点的 children 数组（触发响应式）
      if (tree.children && tree.children.length > 0) {
        for (let i = 0; i < tree.children.length; i++) {
          const child = tree.children[i]
          if (child.locator === locator) {
            // 直接替换数组元素为新对象，触发 Vue 3 响应式
            tree.children[i] = { ...child, ...updates }
            return true
          }
          if (this.updateTreeNode(child, locator, updates)) {
            return true
          }
        }
      }

      return false
    },

    /**
     * 为树的所有节点添加 engineType 字段（辅助方法）
     * @param {Object} tree - 树根节点
     * @param {string} engineType - 引擎类型
     */
    addEngineTypeToTree(tree, engineType) {
      if (!tree) return

      // 为当前节点添加 engineType
      tree.engineType = engineType

      // 递归处理子节点
      if (tree.children && tree.children.length > 0) {
        for (const child of tree.children) {
          this.addEngineTypeToTree(child, engineType)
        }
      }
    }
  }
})

/**
 * 深度克隆树节点（用于触发 Vue 3 响应式更新）
 */
function deepCloneTree(node) {
  if (!node) return node
  return {
    ...node,
    children: node.children ? node.children.map(deepCloneTree) : []
  }
}

/**
 * 在树中查找节点（通过 locator）
 */
function findNodeByLocator(tree, locator) {
  if (!tree || !locator) return null

  if (tree.locator === locator) {
    return tree
  }

  if (tree.children && tree.children.length > 0) {
    for (const child of tree.children) {
      const found = findNodeByLocator(child, locator)
      if (found) return found
    }
  }

  return null
}
 
