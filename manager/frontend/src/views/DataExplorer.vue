<template>
  <div class="data-explorer">
    <div class="split-container" :style="{ gridTemplateColumns: treeWidth + 'px 8px 1fr' }">
      <!-- 左侧资源树区域 -->
      <div class="tree-container">
        <!-- 搜索组件 -->
        <ExplorerSearch
          v-if="showSearch"
          @select-result="handleSearchResultSelect"
        />

        <!-- 资源树组件 -->
        <ExplorerTree
          ref="treeRef"
          :loading="store.loadingEngines"
          @node-select="handleNodeSelect"
        />
      </div>

      <!-- 可拖拽分隔器 -->
      <Splitter direction="horizontal" @resize="startTreeResize" />

      <!-- 右侧预览面板（独立滚动容器） -->
      <div class="preview-container">
        <EnginePanel
          v-if="panelType === 'engine'"
          :engine="selectedEngine"
          :tree-root="selectedEngineTree"
        />

        <NodePanel
          v-else-if="panelType === 'node'"
          :selected-node="store.selectedNode"
          :children="currentNodeChildren"
          @open-node="handleOpenNode"
        />

        <ItemPanel
          v-else
          :selected-node="selectedPreviewNode"
          :preview-data="store.previewData"
          :loading="store.previewLoading || store.childPreviewLoading"
          @page-change="handlePageChange"
          @child-change="handleChildChange"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { parseLocator } from '@addp/common-frontend'
import ExplorerTree from '@/components/explorer/ExplorerTree.vue'
import ExplorerSearch from '@/components/explorer/ExplorerSearch.vue'
import EnginePanel from '@/components/explorer/EnginePanel.vue'
import NodePanel from '@/components/explorer/NodePanel.vue'
import ItemPanel from '@/components/explorer/ItemPanel.vue'
import Splitter from '@/components/explorer/Splitter.vue'
import { useResizable } from '@common-ui'
import { useExplorerStore } from '@/stores/explorer'

const { t } = useI18n()

// 树形面板宽度
const { size: treeWidth, startResize: startTreeResize } = useResizable(320, 220, 600, 'horizontal')

const route = useRoute()
const store = useExplorerStore()

// 引用
const treeRef = ref(null)

// 控制搜索显示：资源树搜索只用于数据探查页内的资源定位
const showSearch = ref(true)

const itemTypes = new Set(['table', 'view', 'collection', 'graph', 'file', 'object'])
const nodeTypes = new Set(['schema', 'database', 'bucket', 'prefix', 'directory', 'root', 'dir'])
const hasLocatorIdentity = (loc) => !!(loc?.itemId || loc?.nodeId)

const nodeContextFromLocator = (locator, baseNode = {}) => {
  const loc = parseLocator(locator)
  const engine = store.engines.find(e => e.id === loc.engineId)
  const label = baseNode.label || loc.path[loc.path.length - 1] || engine?.name || ''
  return {
    ...baseNode,
    id: baseNode.id || locator,
    locator,
    type: baseNode.type || loc.type,
    label,
    engineId: loc.engineId,
    engineType: baseNode.engineType || engine?.engine_type || '',
    engineName: baseNode.engineName || engine?.name || t('manager.explorer.engineNotFound', { engineId: loc.engineId }),
    path: loc.path.join('/'),
    schema: loc.path[0] || '',
    table: loc.path.slice(1).join('/')
  }
}

// 构造预览面板所需的节点上下文
const selectedPreviewNode = computed(() => {
  if (!store.selectedNode) return null

  const loc = parseLocator(store.selectedLocator)
  const engine = store.engines.find(e => e.id === loc.engineId)
  const engineType = store.selectedNode.engineType || engine?.engine_type || ''
  const path = loc.path.join('/')
  const schema = loc.path[0] || ''
  const table = loc.path.slice(1).join('/')

  return {
    id: store.selectedLocator,
    locator: store.selectedLocator,
    engineId: loc.engineId,
    engineType,
    engineName: engine?.name || t('manager.explorer.engineNotFound', { engineId: loc.engineId }),
    schema,
    table,
    path,
    type: loc.type,
    label: store.selectedNode.label
  }
})

const selectedEngine = computed(() => {
  if (!store.selectedLocator) return null
  const loc = parseLocator(store.selectedLocator)
  return store.engines.find(e => e.id === loc.engineId) || null
})

const selectedEngineTree = computed(() => {
  const engine = selectedEngine.value
  if (!engine) return null
  return store.engineTrees[engine.id] || null
})

const panelType = computed(() => {
  const node = store.selectedNode
  if (!node) return 'item'
  if (node.type === 'engine') return 'engine'
  if (nodeTypes.has(node.type)) return 'node'
  return 'item'
})

const currentNodeChildren = computed(() => {
  const node = store.selectedNode
  if (!node || panelType.value !== 'node') return []
  return node.children || []
})

// 事件处理：节点选择（从 ExplorerTree 组件触发）
const handleNodeSelect = async ({ node, locator }) => {
  try {
    if (node.type !== 'engine' && nodeTypes.has(node.type)) {
      await store.loadNodeChildren(locator, 1)
    }

    if (node.type !== 'engine' && itemTypes.has(node.type)) {
      await store.loadPreview(locator, 1)
    }
  } catch (error) {
    console.error('加载节点数据失败:', error)
    const errorMessage = error.response?.data?.error || error.message
    ElMessage.error(errorMessage)
  }
}

// 事件处理：搜索结果选择
const handleSearchResultSelect = async (result) => {
  const resultNode = result?.node || result
  const locator = resultNode?.locator || resultNode?.id
  if (!locator) {
    return
  }

  const loc = parseLocator(locator)
  if (!hasLocatorIdentity(loc) && loc?.type !== 'database') {
    ElMessage.warning(t('manager.explorer.locateFailed'))
    return
  }
  if (loc?.engineId) {
    await store.loadTree(loc.engineId, -1)
  }

  const tree = loc?.engineId ? store.engineTrees[loc.engineId] : null
  const path = findNodePathByLocator(tree, locator)
  for (const segment of path.slice(0, -1)) {
    const segmentLocator = segment.locator || segment.id
    if (segmentLocator) {
      treeRef.value?.expandNode(segmentLocator)
    }
  }

  const node = path[path.length - 1] || resultNode

  store.selectNodeContext(nodeContextFromLocator(locator, node), locator)
  await scrollToLocatedNode(path, locator)

  try {
    await handleNodeSelect({ node, locator })
    ElMessage.success(t('manager.explorer.locateSuccess'))
  } catch (error) {
    console.error('加载预览失败:', error)
    ElMessage.error(t('manager.explorer.loadPreviewFailed', { error: error.message }))
  }
}

// 事件处理：分页变化
const handlePageChange = async (payload) => {
  if (!store.selectedLocator) return
  const page = typeof payload === 'object' ? payload?.page || 1 : payload
  const pageSize = typeof payload === 'object' ? Number(payload?.pageSize || 0) : 0
  if (pageSize > 0) {
    store.pagination.pageSize = pageSize
  }

  try {
    await store.loadPreview(store.selectedLocator, page, store.selectedChildName, store.selectedRefPath, store.selectedNestedChildPath, store.selectedChildKey)
  } catch (error) {
    ElMessage.error(t('manager.explorer.loadPreviewFailed', { error: error.message }))
  }
}

const handleChildChange = async (payload) => {
  const childName = typeof payload === 'string' ? payload : payload?.childName
  const childKey = typeof payload === 'object' ? payload?.childKey || '' : ''
  const refPath = typeof payload === 'object' ? payload?.refPath || '' : ''
  const nestedChildPath = typeof payload === 'object' ? payload?.nestedChildPath || '' : ''
  const refSwitch = typeof payload === 'object' && payload?.refSwitch
  if (!store.selectedLocator || (!childName && !refPath && !nestedChildPath && !refSwitch)) return
  try {
    await store.loadPreview(store.selectedLocator, 1, childName, refPath, nestedChildPath, childKey)
  } catch (error) {
    ElMessage.error(t('manager.explorer.loadPreviewFailed', { error: error.message }))
  }
}

const handleOpenNode = async (locator) => {
  if (!locator) return
  store.selectNodeContext(nodeContextFromLocator(locator), locator)
  const node = store.selectedNode
  if (!node) return
  await handleNodeSelect({ node, locator })
}

const findNodePathByLocator = (node, locator, parents = []) => {
  if (!node || !locator) return []
  const current = [...parents, node]
  if ((node.locator || node.id) === locator) {
    return current
  }
  for (const child of node.children || []) {
    const found = findNodePathByLocator(child, locator, current)
    if (found.length > 0) {
      return found
    }
  }
  return []
}

const scrollToLocatedNode = async (path, locator) => {
  await nextTick()
  if (await treeRef.value?.scrollToNode(locator, { block: 'center' })) {
    return true
  }
  for (let i = path.length - 1; i >= 0; i -= 1) {
    const fallbackLocator = path[i]?.locator || path[i]?.id
    if (fallbackLocator && await treeRef.value?.scrollToNode(fallbackLocator, { block: 'center' })) {
      return true
    }
  }
  return false
}

// 初始化
onMounted(async () => {
  try {
    // 1. 加载引擎列表
    await store.loadEngines()

    // 2. 并行加载所有引擎的树结构（使用默认深度 2）
    if (store.engines.length > 0) {
      await Promise.all(
        store.engines.map(engine => store.loadTree(engine.id))
      )

      // 等待 DOM 更新后，设置初始展开状态（只展开引擎层级）
      await nextTick()

      // 收集初始需要展开的引擎节点 locators
      const engineLocators = store.engines.map(engine =>
        `addp://engine/${engine.id}/path/?type=database`
      )

      // 设置展开状态
      store.expandedLocators = new Set(engineLocators)

    }
  } catch (error) {
    ElMessage.error(t('manager.explorer.initFailed', { error: error.message }))
  }
})

// 监听路由变化，根据参数自动定位和选中对象
watch(() => route.query, async (query) => {
  const targetLocator = String(query.locator || '').trim()
  if (!targetLocator) {
    return
  }

  try {
    const loc = parseLocator(targetLocator)
    if (!loc?.engineId) {
      console.warn('[DataExplorer] 无效 locator:', targetLocator)
      return
    }
    if (!hasLocatorIdentity(loc) && loc.type !== 'database') {
      console.warn('[DataExplorer] locator 缺少 node_id/item_id，拒绝定位:', targetLocator)
      ElMessage.warning(t('manager.explorer.locateFailed'))
      return
    }
    const engineId = loc.engineId

    if (store.engines.length === 0) {
      await store.loadEngines()
    }

    const engine = store.engines.find(e => e.id === engineId)
    if (!engine) {
      console.warn('[DataExplorer] 引擎未找到:', engineId)
      ElMessage.warning(t('manager.explorer.engineNotFound', { engineId }))
      return
    }

    await store.loadTree(engineId, -1)

    const engineTree = store.engineTrees[engineId]
    const path = findNodePathByLocator(engineTree, targetLocator)
    if (path.length === 0) {
      console.warn('[DataExplorer] 资源树中未找到 locator:', targetLocator)
    }
    for (const segment of path.slice(0, -1)) {
      const segmentLocator = segment.locator || segment.id
      if (segmentLocator) {
        treeRef.value?.expandNode(segmentLocator)
      }
    }

    const targetNode = path[path.length - 1] || nodeContextFromLocator(targetLocator, { type: loc.type })
    store.selectNodeContext(nodeContextFromLocator(targetLocator, targetNode), targetLocator)
    await scrollToLocatedNode(path, targetLocator)
    await handleNodeSelect({ node: targetNode, locator: targetLocator })

    ElMessage.success(t('manager.explorer.locateSuccess'))
  } catch (error) {
    console.error('[DataExplorer] 定位失败:', error)
    ElMessage.error(t('manager.explorer.loadPreviewFailed', { error: error.message }))
  }
}, { immediate: true })
</script>

<style scoped>
.data-explorer {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-secondary) !important;
}

.split-container {
  flex: 1;
  display: grid;
  gap: 0;
  overflow: hidden;
  grid-template-rows: 1fr;
  min-height: 0;
  background: var(--addp-bg-secondary) !important;
}

.tree-container {
  height: 100%;
  overflow: auto;
  background: var(--addp-bg-primary) !important;
}

.preview-container {
  height: 100%;
  min-height: 0;
  overflow: hidden;
}
</style>
