import { defineStore } from 'pinia'
import { parseLocator, buildLocator } from '@addp/common-frontend'
import client from '@/api/client'

/**
 * 引擎类型对应的图标
 */
const ENGINE_ICONS = {
  postgresql: 'Coin',
  mysql: 'Coin',
  mongodb: 'Collection',
  minio: 'FolderOpened',
  s3: 'FolderOpened',
  default: 'Coin'
}

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

    // 展开的节点（使用 locator URI 作为 key）
    expandedLocators: new Set(),

    // 正在刷新的节点
    refreshingLocators: new Set(),

    // 预览数据
    previewData: null,
    previewLoading: false,

    // 分页
    pagination: {
      page: 1,
      pageSize: 20
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
          icon: ENGINE_ICONS[engine.engine_type] || ENGINE_ICONS.default,
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
        const response = await client.get('/explorer/engines')
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
     * @param {number} expandDepth - 展开深度（默认 2，设置 -1 表示无限深度）
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
        const response = await client.get(`/explorer/tree/${engineId}`, {
          params: { expand_depth: expandDepth }
        })
        const tree = response.data.tree

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
        await client.post(`/explorer/tree/${loc.engineId}/refresh`, null, {
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
    async loadPreview(locator, page = 1) {
      this.selectedLocator = locator
      this.previewLoading = true
      this.pagination.page = page

      try {
        const response = await client.get('/explorer/preview', {
          params: {
            locator,
            page,
            page_size: this.pagination.pageSize
          }
        })
        // 后端返回 PreviewResult: {preview_type, data: TablePreview, metadata}
        // 前端预览插件期望直接收到 TablePreview 格式 (包含 mode, object, columns 等)
        // 所以这里提取内层的 data 字段
        this.previewData = response.data?.data || response.data
        return this.previewData
      } catch (error) {
        console.error('加载预览失败:', error)
        throw error
      } finally {
        this.previewLoading = false
      }
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
      this.expandedLocators.add(locator)
    },

    /**
     * 折叠节点
     */
    collapseNode(locator) {
      this.expandedLocators.delete(locator)
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
      this.previewData = null
      this.pagination.page = 1
    }
  }
})

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
