<template>
  <div
    class="data-explorer"
    data-testid="data-explorer"
    :data-engine-load-state="engineLoadState"
  >
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
        <div v-if="selectedEngineUnavailable" class="engine-unavailable-panel">
          <el-alert
            :title="t('manager.explorer.engineUnavailableTitle')"
            type="warning"
            :closable="false"
            show-icon
          >
            <template #default>
              <p class="engine-unavailable-panel__description">
                {{ t('manager.explorer.engineUnavailableDescription', { status: selectedEngineStatusLabel }) }}
              </p>
              <dl class="engine-unavailable-panel__facts">
                <div>
                  <dt>{{ t('manager.explorer.engine') }}</dt>
                  <dd>{{ selectedEngine?.name }}</dd>
                </div>
                <div>
                  <dt>{{ t('manager.explorer.engineStatus') }}</dt>
                  <dd>{{ selectedEngineStatusLabel }}</dd>
                </div>
                <div v-if="store.selectedNode?.label">
                  <dt>{{ t('manager.explorer.selectedResource') }}</dt>
                  <dd>{{ store.selectedNode.label }}</dd>
                </div>
              </dl>
            </template>
          </el-alert>
        </div>

        <EnginePanel
          v-else-if="panelType === 'engine'"
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
          @open-catalog="handleOpenCatalog"
          @refresh-preview="handlePreviewRefresh"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onBeforeUnmount, onMounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { engineSelectionState, isEngineSelectable, parseLocator } from '@addp/common-frontend'
import ExplorerTree from '@/components/explorer/ExplorerTree.vue'
import ExplorerSearch from '@/components/explorer/ExplorerSearch.vue'
import EnginePanel from '@/components/explorer/EnginePanel.vue'
import NodePanel from '@/components/explorer/NodePanel.vue'
import ItemPanel from '@/components/explorer/ItemPanel.vue'
import Splitter from '@/components/explorer/Splitter.vue'
import { openConsoleRoute, useResizable } from '@common-ui'
import { useExplorerStore } from '@/stores/explorer'
import {
  buildCatalogEntryConsoleRoute,
  buildDataExplorerQuery,
  normalizeDataExplorerTab,
  resolveDataExplorerRouteState
} from '@/utils/dataExplorerRoute'
import { navigateManagerRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()

// 树形面板宽度
const { size: treeWidth, startResize: startTreeResize } = useResizable(320, 220, 600, 'horizontal')

const route = useRoute()
const router = useRouter()
const store = useExplorerStore()
const activeTab = ref(resolveDataExplorerRouteState(route.query).tab)
const ENGINE_STATUS_REFRESH_INTERVAL_MS = 15000
let engineStatusTimer = 0
let engineInitializationPromise = null
const engineInitializationErrorShown = ref(false)
const engineLoadState = computed(() => store.loadingEngines
  ? 'loading'
  : (engineInitializationErrorShown.value ? 'error' : 'loaded'))

// 引用
const treeRef = ref(null)

// 控制搜索显示：资源树搜索只用于数据探查页内的资源定位
const showSearch = ref(true)

const itemTypes = new Set(['table', 'view', 'collection', 'graph', 'topic', 'file', 'object'])
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

const selectedEngineUnavailable = computed(() => (
  !!selectedEngine.value && !isEngineSelectable(selectedEngine.value)
))

const selectedEngineStatusLabel = computed(() => (
  t(`common.engineStatus.${engineSelectionState(selectedEngine.value)}`)
))

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
  const location = { name: 'DataExplorer', query }
  if (route.fullPath === router.resolve(location).fullPath) return
  try {
    await navigateManagerRoute(router, location, { history: 'replace' })
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
      const loc = parseLocator(locator)
      if (!store.isEngineAvailable(loc.engineId)) {
        store.selectNodeContext(node, locator)
        store.clearPreview()
        return
      }
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
  if (!store.selectedLocator || selectedEngineUnavailable.value) return
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

const handlePreviewRefresh = () => handlePageChange({ page: store.pagination.page, pageSize: store.pagination.pageSize })

const handleChildChange = async (payload) => {
  const childName = typeof payload === 'string' ? payload : payload?.childName
  const childKey = typeof payload === 'object' ? payload?.childKey || '' : ''
  const refPath = typeof payload === 'object' ? payload?.refPath || '' : ''
  const nestedChildPath = typeof payload === 'object' ? payload?.nestedChildPath || '' : ''
  const refSwitch = typeof payload === 'object' && payload?.refSwitch
  if (!store.selectedLocator || selectedEngineUnavailable.value || (!childName && !refPath && !nestedChildPath && !refSwitch)) return
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

const handleOpenCatalog = async (entryId) => {
  if (!entryId) return
  await openConsoleRoute(buildCatalogEntryConsoleRoute(entryId))
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

const ensureEnginesLoaded = () => {
  if (store.engines.length > 0) {
    return Promise.resolve(store.engines)
  }
  if (engineInitializationPromise) {
    return engineInitializationPromise
  }
  const task = store.loadEngines()
    .then(engines => {
      engineInitializationErrorShown.value = false
      return engines
    })
    .catch(error => {
      if (!engineInitializationErrorShown.value) {
        engineInitializationErrorShown.value = true
        ElMessage.error(t('manager.explorer.initFailed', { error: error.message }))
      }
      throw error
    })
    .finally(() => {
      if (engineInitializationPromise === task) {
        engineInitializationPromise = null
      }
    })
  engineInitializationPromise = task
  return task
}

const locateRouteLocator = async (targetLocator, options = {}) => {
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
  const engine = store.engines.find(e => e.id === loc.engineId)
  if (!engine) {
    console.warn('[DataExplorer] 引擎未找到:', loc.engineId)
    ElMessage.warning(t('manager.explorer.engineNotFound', { engineId: loc.engineId }))
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
  if (options.notifySuccess !== false) {
    ElMessage.success(t('manager.explorer.locateSuccess'))
  }
}

const refreshEngineStatuses = async () => {
  const targetLocator = String(store.selectedLocator || route.query.locator || '').trim()
  const targetEngineID = targetLocator ? parseLocator(targetLocator)?.engineId : null
  const wasAvailable = targetEngineID ? store.isEngineAvailable(targetEngineID) : false
  try {
    await store.loadEngines({ silent: true })
    if (!targetEngineID) return

    const isAvailable = store.isEngineAvailable(targetEngineID)
    if (!isAvailable) {
      store.clearPreview()
      return
    }
    if (!wasAvailable || !store.selectedNode) {
      await locateRouteLocator(targetLocator, { notifySuccess: false })
    }
  } catch (error) {
    console.warn('[DataExplorer] 刷新引擎状态失败:', error)
  }
}

// 初始化
onMounted(async () => {
  engineStatusTimer = window.setInterval(refreshEngineStatuses, ENGINE_STATUS_REFRESH_INTERVAL_MS)
  try {
    await ensureEnginesLoaded()
  } catch {
    // ensureEnginesLoaded 统一展示一次初始化错误；状态轮询会继续恢复。
  }
})

onBeforeUnmount(() => {
  if (engineStatusTimer) {
    window.clearInterval(engineStatusTimer)
    engineStatusTimer = 0
  }
})

watch(() => route.query, async (query) => {
  const routeState = resolveDataExplorerRouteState(query)
  activeTab.value = routeState.tab
  if (routeState.changed) {
    const location = { name: 'DataExplorer', query: routeState.query }
    if (route.fullPath !== router.resolve(location).fullPath) {
      await navigateManagerRoute(router, location, { history: 'replace' })
    }
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
    await ensureEnginesLoaded()
  } catch {
    return
  }

  try {
    await locateRouteLocator(targetLocator)
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

.engine-unavailable-panel {
  padding: 24px;
}

.engine-unavailable-panel__description {
  margin: 8px 0 16px;
  line-height: 1.6;
}

.engine-unavailable-panel__facts {
  display: grid;
  gap: 10px;
  margin: 0;
}

.engine-unavailable-panel__facts > div {
  display: grid;
  grid-template-columns: 96px minmax(0, 1fr);
  gap: 12px;
}

.engine-unavailable-panel__facts dt {
  color: var(--el-text-color-secondary);
}

.engine-unavailable-panel__facts dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
