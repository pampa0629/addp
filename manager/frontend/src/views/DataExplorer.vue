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
          :active-tab="activeTab"
          :selected-node="selectedPreviewNode"
          :preview-data="store.previewData"
          :profile-preview-data="store.activeChildPreviewData || store.previewData"
          :selected-child-name="store.selectedChildName"
          :selected-ref-path="store.selectedRefPath"
          :selected-nested-child-path="store.selectedNestedChildPath"
          :loading="store.previewLoading || store.childPreviewLoading"
          @page-change="handlePageChange"
          @child-change="handleChildChange"
          @tab-change="handleTabChange"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { engineRootLocator, parseLocator, syncConsoleRoute } from '@addp/common-frontend'
import ExplorerTree from '@/components/explorer/ExplorerTree.vue'
import ExplorerSearch from '@/components/explorer/ExplorerSearch.vue'
import EnginePanel from '@/components/explorer/EnginePanel.vue'
import NodePanel from '@/components/explorer/NodePanel.vue'
import ItemPanel from '@/components/explorer/ItemPanel.vue'
import Splitter from '@/components/explorer/Splitter.vue'
import { useResizable } from '@common-ui'
import { useExplorerStore } from '@/stores/explorer'
import {
  buildDataExplorerConsoleRoute,
  buildDataExplorerQuery,
  normalizeDataExplorerTab
} from '@/utils/dataExplorerRoute'

const { t } = useI18n()

// 树形面板宽度
const { size: treeWidth, startResize: startTreeResize } = useResizable(320, 220, 600, 'horizontal')

const route = useRoute()
const router = useRouter()
const store = useExplorerStore()
const activeTab = ref(normalizeDataExplorerTab(route.query.tab))

// 引用
const treeRef = ref(null)

// 控制搜索显示：资源树搜索只用于数据探查页内的资源定位
const showSearch = ref(true)

const itemTypes = new Set(['table', 'view', 'collection', 'graph', 'file', 'object'])
const nodeTypes = new Set(['schema', 'database', 'bucket', 'prefix', 'directory', 'root', 'dir', 'server', 'service'])
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
  if (nodeTypes.has(node.type)) return 'node'
  return 'item'
})

const currentNodeChildren = computed(() => {
  const node = store.selectedNode
  if (!node || panelType.value !== 'node') return []
  return node.children || []
})

const replaceDataExplorerRoute = async (locator, tab = activeTab.value) => {
  const normalizedTab = normalizeDataExplorerTab(tab)
  const query = buildDataExplorerQuery(locator, normalizedTab)
  const nextRoute = router.resolve({ name: 'DataExplorer', query })
  if (route.fullPath !== nextRoute.fullPath) {
    await router.replace({ name: 'DataExplorer', query })
  }
  try {
    await syncConsoleRoute(buildDataExplorerConsoleRoute(locator, normalizedTab), { history: 'replace' })
  } catch (error) {
    console.error('[DataExplorer] 同步 Console 路由失败:', error)
  }
}

// 事件处理：节点选择（从 ExplorerTree 组件触发）
const handleNodeSelect = async ({ node, locator }, options = {}) => {
  try {
    if (options.updateRoute !== false) {
      activeTab.value = 'preview'
      await replaceDataExplorerRoute(locator, activeTab.value)
    }

    if (nodeTypes.has(node.type) && node.hasChildren && !node.loaded) {
      await store.loadNodeChildren(locator)
    }

    if (itemTypes.has(node.type)) {
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
  if (!hasLocatorIdentity(loc)) {
    ElMessage.warning(t('manager.explorer.locateFailed'))
    return
  }

  try {
    const revealed = await store.revealLocator(locator)
    const node = revealed.node || resultNode
    await scrollToLocatedNode(revealed.path || [], revealed.locator || locator)
    await handleNodeSelect({ node, locator: revealed.locator || locator })
    ElMessage.success(t('manager.explorer.locateSuccess'))
  } catch (error) {
    console.error('定位资源失败:', error)
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
    if (activeTab.value !== 'preview') {
      activeTab.value = 'preview'
      await replaceDataExplorerRoute(store.selectedLocator, activeTab.value)
    }
    await store.loadPreview(store.selectedLocator, 1, childName, refPath, nestedChildPath, childKey)
  } catch (error) {
    ElMessage.error(t('manager.explorer.loadPreviewFailed', { error: error.message }))
  }
}

const handleTabChange = async (tab) => {
  const normalizedTab = normalizeDataExplorerTab(tab)
  activeTab.value = normalizedTab
  await replaceDataExplorerRoute(store.selectedLocator, normalizedTab)
}

const handleOpenNode = async (locator) => {
  if (!locator) return
  store.selectNodeContext(nodeContextFromLocator(locator), locator)
  const node = store.selectedNode
  if (!node) return
  await handleNodeSelect({ node, locator })
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
        store.engineTrees[engine.id]?.locator || engineRootLocator(engine)
      )

      // 设置展开状态
      store.expandedLocators = new Set(engineLocators)

    }
  } catch (error) {
    ElMessage.error(t('manager.explorer.initFailed', { error: error.message }))
  }
})

watch(() => route.query.tab, async (tab) => {
  const normalizedTab = normalizeDataExplorerTab(tab)
  activeTab.value = normalizedTab
  const rawTab = String(tab || '').trim().toLowerCase()
  if (rawTab && rawTab !== normalizedTab) {
    await replaceDataExplorerRoute(String(route.query.locator || '').trim(), normalizedTab)
  }
}, { immediate: true })

// 监听 locator 变化，根据标准资源身份定位和选中对象。
watch(() => route.query.locator, async (locator) => {
  const targetLocator = String(locator || '').trim()
  if (!targetLocator) {
    return
  }

  if (targetLocator === store.selectedLocator) {
    return
  }

  try {
    const loc = parseLocator(targetLocator)
    if (!loc?.engineId) {
      console.warn('[DataExplorer] 无效 locator:', targetLocator)
      return
    }
    if (!hasLocatorIdentity(loc)) {
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

    const revealed = await store.revealLocator(targetLocator)
    const revealedLocator = revealed.locator || targetLocator
    const targetNode = revealed.node || nodeContextFromLocator(revealedLocator, { type: loc.type })
    await scrollToLocatedNode(revealed.path || [], revealedLocator)
    await handleNodeSelect({ node: targetNode, locator: revealedLocator }, { updateRoute: false })

    if (revealedLocator !== targetLocator) {
      await replaceDataExplorerRoute(revealedLocator, activeTab.value)
    }

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
  min-height: 0;
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
  min-height: 0;
  overflow: auto;
  background: var(--addp-bg-primary) !important;
}

.preview-container {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
</style>
