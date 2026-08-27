<template>
  <div class="explorer-tree">
    <!-- 加载状态：显示骨架屏 -->
    <el-skeleton
      v-if="loading"
      :rows="8"
      animated
      class="skeleton-loader"
    >
      <template #template>
        <el-skeleton-item variant="h3" style="width: 60%; margin-bottom: 20px;" />
        <div style="padding: 14px;">
          <el-skeleton-item variant="text" style="width: 40%; margin-bottom: 12px;" />
          <el-skeleton-item variant="text" style="width: 60%; margin-left: 20px; margin-bottom: 12px;" />
          <el-skeleton-item variant="text" style="width: 55%; margin-left: 20px; margin-bottom: 12px;" />
        </div>
        <div style="padding: 14px;">
          <el-skeleton-item variant="text" style="width: 45%; margin-bottom: 12px;" />
          <el-skeleton-item variant="text" style="width: 50%; margin-left: 20px; margin-bottom: 12px;" />
        </div>
        <div style="padding: 14px;">
          <el-skeleton-item variant="text" style="width: 35%; margin-bottom: 12px;" />
        </div>
      </template>
    </el-skeleton>

    <!-- 正常状态：显示树 -->
    <template v-else>
      <div
        v-if="activeScan.visible"
        class="scan-status"
      >
        <div class="scan-status__header">
          <span class="scan-status__title">{{ activeScan.title }}</span>
          <span class="scan-status__percent">{{ activeScan.percent }}%</span>
        </div>
        <el-progress
          :percentage="activeScan.percent"
          :status="activeScan.status"
          :stroke-width="6"
          :show-text="false"
        />
        <div class="scan-status__detail">{{ activeScan.detail }}</div>
      </div>

      <ResourceTree
        ref="resourceTreeRef"
        :tree-data="treeData"
        :loading="false"
        :refreshing-node-ids="refreshingNodeIds"
        v-model:expanded-keys="expandedKeys"
        :current-node-key="currentNodeKey"
        :node-actions="nodeActions"
        :node-class-name="resolveNodeClassName"
        :expand-on-click-node="true"
        :title="t('manager.explorer.storageEngines')"
        :count-text="(count) => t('manager.explorer.countText', { count })"
        @refresh="handleRefresh"
        @node-click="handleNodeClick"
        @node-action="handleNodeAction"
        @node-expand="handleNodeExpand"
        @node-collapse="handleNodeCollapse"
      >
        <template #node-label="{ data }">
          <span
            class="explorer-node-label"
            :title="data.label"
            :data-testid="isCatalogRootNode(data) ? 'engine-node' : undefined"
            :data-engine-id="isCatalogRootNode(data) ? String(data.engineId) : undefined"
            :data-engine-state="isCatalogRootNode(data) ? data.engineState : undefined"
            :data-connection-status="isCatalogRootNode(data) ? data.connectionStatus : undefined"
          >
            <span class="explorer-node-label__text">{{ data.label }}</span>
            <el-tag
              v-if="isCatalogRootNode(data) && !isNodeEngineAvailable(data)"
              size="small"
              type="warning"
              effect="plain"
            >
              {{ engineStatusLabel(data) }}
            </el-tag>
          </span>
        </template>
      </ResourceTree>
    </template>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { ResourceTree } from '@addp/common-frontend'
import { parseLocator } from '@addp/common-frontend'
import { useExplorerStore } from '@/stores/explorer'
import client from '@/api/client'
import {
  canShowVectorizeAction,
  isEmbeddingReady,
  isVectorizableObjectNode,
  isVectorizableRangeNode,
  isStorageEngineNode
} from '@/utils/vectorization'
import { resolveCanonicalNodeSelection } from '@/utils/dataExplorerSelection'

const { t } = useI18n()

// Props
const props = defineProps({
  loading: {
    type: Boolean,
    default: false
  }
})

// Emits
const emit = defineEmits(['node-select'])

const store = useExplorerStore()
const resourceTreeRef = ref(null)
const embeddingStates = ref({})
const catalogRootLoadPromises = new Map()
let scanStatusTimer = 0
const activeScan = ref({
  visible: false,
  title: '',
  detail: '',
  percent: 0,
  status: ''
})

// 节点操作（根据节点类型动态生成）
const nodeActions = computed(() => {
  return [
    // 刷新操作（所有节点都支持）
    {
      id: 'refresh',
      name: 'refresh',
      label: t('manager.explorer.refresh'),
      tooltip: t('manager.explorer.refreshTooltip'),
      icon: 'Refresh',
      disabled: (node) => !isNodeEngineAvailable(node),
      visible: () => true
    },
    // 已向量化状态提示（仅支持向量化的单个对象）
    {
      id: 'embedding-ready',
      name: 'embedding-ready',
      label: t('manager.explorer.vectorized'),
      tooltip: t('manager.explorer.vectorized'),
      icon: 'Select',
      color: '#67c23a',
      disabled: () => true,
      visible: (node) => {
        const state = embeddingStates.value[node.locator || node.id]
        return isVectorizableObjectNode(node) && isEmbeddingReady(state)
      }
    },
    // 向量化操作（仅支持向量化且尚未 ready 的单个对象）
    {
      id: 'embedding',
      name: 'embedding',
      label: t('manager.explorer.vectorize'),
      icon: 'MagicStick',
      disabled: (node) => !isNodeEngineAvailable(node),
      visible: (node) => {
        const state = embeddingStates.value[node.locator || node.id]
        return isVectorizableObjectNode(node) && canShowVectorizeAction(node, state)
      }
    },
    // 批量向量化操作（MinIO/S3 的目录、前缀或 Bucket）
    {
      id: 'embedding-batch',
      name: 'embedding-batch',
      label: t('manager.explorer.batchVectorize'),
      icon: 'MagicStick',
      disabled: (node) => !isNodeEngineAvailable(node),
      visible: (node) => {
        return isVectorizableRangeNode(node)
      }
    }
  ]
})

// 计算属性：树的顶层节点（catalog root 列表）
const treeData = computed(() => {
  return store.catalogRootNodes
})

// 计算属性：展开的节点 keys
const expandedKeys = computed({
  get: () => {
    return Array.from(store.expandedLocators)
  },
  set: (keys) => {
    store.expandedLocators = new Set(keys)
  }
})

// 计算属性：正在刷新的节点 IDs
const refreshingNodeIds = computed(() => {
  return Array.from(store.refreshingLocators)
})

// 计算属性：当前选中的节点 key
const currentNodeKey = computed(() => store.selectedLocator || '')

onBeforeUnmount(() => {
  cancelScanStatusTimer()
})

// 事件处理：刷新整个引擎列表
const handleRefresh = async () => {
  try {
    // 清空引擎树缓存
    store.engineTrees = {}
    store.engineTreeDepths = {}
    store.engineTreeRequestSeq = {}
    store.loadingEngineIds = {}
    // 重新加载引擎列表
    await store.loadEngines()

    // 重新加载所有引擎的树结构（使用默认深度 2：bucket + directory）
    if (store.engines.length > 0) {
      await Promise.all(
        store.engines.map(engine => store.loadTree(engine.id))
      )
    }

    ElMessage.success(t('manager.explorer.refreshSuccess'))
  } catch (error) {
    ElMessage.error(t('manager.explorer.refreshFailed', { error: error.message }))
  }
}

// 事件处理：节点点击
const handleNodeClick = async (node) => {
  let selectedNode = node
  let locator = node.locator || node.id

  if (isCatalogRootNode(node) && node.engineId && !node.loaded) {
    store.expandNode(locator)
    try {
      const resolved = await resolveCanonicalNodeSelection({
        node,
        locator,
        loadTree: loadCatalogRoot
      })
      selectedNode = resolved.node
      const syntheticLocator = locator
      locator = resolved.locator
      if (locator !== syntheticLocator) {
        store.collapseNode(syntheticLocator)
        store.expandNode(locator)
      }
    } catch (error) {
      console.error('加载引擎内容失败:', error)
      ElMessage.error(t('manager.explorer.loadEngineFailed', { error: error.message }))
      return
    }
  }

  // 选择节点
  store.selectNode(locator)
  emit('node-select', { node: selectedNode, locator })
  if (isNodeEngineAvailable(selectedNode)) {
    await loadItemEmbeddingState(selectedNode, locator)
  }

  // 关键：@node-collapse / @node-expand 在 element-plus 中先于 @node-click 触发，
  // 因此这里读到的是"点击后"的 store 状态，而不是点击前的状态。
  // 只有当节点当前未展开 && 需要首次加载数据时，才主动介入（强制展开 + 加载）。
  // 对于已加载的节点，expand-on-click-node 和 handleNodeExpand/Collapse 已经处理好了。
  const isCurrentlyExpanded = store.expandedLocators.has(locator)

  // Catalog root 节点：仅首次展开（未加载）时强制展开并加载数据
  if (isCatalogRootNode(selectedNode) && selectedNode.engineId) {
    return
  }

  // 仅在需要首次加载子节点时介入，避免与 el-tree 的 expand-on-click-node 冲突。
  // 初始 expand_depth 树可能只带部分 children，必须以 loaded=false 作为懒加载边界。
  const isDirLike = isBranchNode(node)
  if (isDirLike) {
    const needsLoading = node.hasChildren && !node.loaded
    if (!isCurrentlyExpanded && needsLoading) {
      // 首次展开：el-tree 因暂无子节点可能不会自动展开，需要强制 store 记录展开状态
      store.expandNode(locator)
      try {
        const loc = parseLocator(locator)
        if (loc && loc.engineId) {
          await store.loadNodeChildren(locator, true)
        }
      } catch (error) {
        console.error('加载子节点失败:', error)
        ElMessage.error(t('manager.explorer.loadChildrenFailed', { error: error.message }))
      }
    }
  }
}

const loadItemEmbeddingState = async (node, locator) => {
  if (!node || node.type !== 'object') return
  try {
    const loc = parseLocator(locator)
    if (!loc.itemId) return
    const state = await client.get(`/manager/items/${loc.itemId}/embedding`)
    embeddingStates.value = {
      ...embeddingStates.value,
      [locator]: state
    }
  } catch (error) {
    console.warn('加载 item 向量化状态失败:', error)
  }
}

// 事件处理：节点操作
const handleNodeAction = async ({ node, action }) => {
  const locator = node.locator || node.id

  if (!isNodeEngineAvailable(node)) {
    ElMessage.warning(t('manager.explorer.engineUnavailableAction'))
    return
  }

  if (action === 'refresh') {
    try {
      startScanStatus(t('manager.explorer.scanSubmitting'), t('manager.explorer.scanSubmitting'), 5)
      if (isBranchNode(node)) {
        await store.refreshNode(locator, {
          onSubmitted: (run) => updateScanStatusFromRun(run, t('manager.explorer.scanSubmitted')),
          onProgress: updateScanStatusFromRun,
          onScanCompleted: (run) => updateScanStatusFromRun(run, t('manager.explorer.treeRefreshing'), 95)
        })
      } else {
        await store.refreshItem(locator)
      }
      completeScanStatus()
      ElMessage.success(t('manager.explorer.scanCompleted'))
    } catch (error) {
      failScanStatus(error)
      ElMessage.error(t('manager.explorer.refreshFailed', { error: error.message }))
    }
    return
  }

  if (action === 'embedding' || action === 'embedding-batch') {
    // 只支持 MinIO/S3 对象存储
    if (!isStorageEngineNode(node)) {
      ElMessage.warning(t('manager.explorer.vectorizeOnlyStorage'))
      return
    }

    try {
      // 解析 locator 提取参数
      const loc = parseLocator(locator)
      let request
      if (action === 'embedding') {
        if (!isVectorizableObjectNode(node)) {
          ElMessage.warning(t('manager.explorer.vectorizeSingleFileOnly'))
          return
        }
        if (!loc.itemId) {
          ElMessage.warning(t('manager.explorer.vectorizeSingleFileOnly'))
          return
        }
        request = {
          scope: 'item',
          target: {
            engine_id: loc.engineId,
            item_id: loc.itemId,
            locator
          }
        }
      } else {
        if (!isVectorizableRangeNode(node) || !loc.nodeId) {
          ElMessage.warning(t('manager.explorer.batchVectorizeDirOnly'))
          return
        }
        request = {
          scope: 'node',
          target: {
            engine_id: loc.engineId,
            node_id: loc.nodeId,
            locator,
            recursive: true
          }
        }
      }

      await client.post('/manager/embedding_executions', request)

      if (action === 'embedding') {
        await loadItemEmbeddingState(node, locator)
        ElMessage.success(t('manager.explorer.vectorizeSubmitted', { key: node.label }))
      } else {
        ElMessage.success(t('manager.explorer.batchVectorizeSubmitted', { label: node.label }))
      }
    } catch (error) {
      console.error('向量化失败:', error)
      ElMessage.error(t('manager.explorer.vectorizeFailed', { error: error.response?.data?.error || error.message }))
    }
  }
}

// 事件处理：节点展开（优化版 - 使用增量加载）
const handleNodeExpand = async (node) => {
  const locator = node.locator || node.id
  store.expandNode(locator)

  // 如果是 catalog root 节点且未加载过，懒加载其内容
  if (isCatalogRootNode(node) && node.engineId && !node.loaded) {
    try {
      await loadCatalogRoot(node.engineId)
    } catch (error) {
      console.error('加载引擎内容失败:', error)
      ElMessage.error(t('manager.explorer.loadEngineFailed', { error: error.message }))
    }
    return
  }

  // 使用增量加载替代全量重载：容器节点首次展开时，只加载该节点的直接子节点。
  const needsLoading = isBranchNode(node) && node.hasChildren && !node.loaded

  if (needsLoading && locator) {
    try {
      // 从 locator 解析出 engineId
      const loc = parseLocator(locator)
      if (loc && loc.engineId) {
        // 只加载该节点的子节点，不重新加载整个树。
        // 强制刷新，绕过缓存，确保从后端加载数据
        await store.loadNodeChildren(locator, true)
      }
    } catch (error) {
      console.error('增量加载子节点失败:', error)
      ElMessage.error(t('manager.explorer.loadChildrenFailed', { error: error.message }))
    }
  }
}

// 事件处理：节点折叠
const handleNodeCollapse = (node) => {
  const locator = node.locator || node.id
  store.collapseNode(locator)
}

const rootTypes = new Set(['root', 'server', 'service'])
const branchTypes = new Set(['directory', 'bucket', 'prefix', 'schema', 'database', 'dir', 'root', 'server', 'service'])

const isCatalogRootNode = (node) => {
  const fullName = node?.metadata?.full_name
  return !!node && rootTypes.has(node.type) && (fullName === '' || (node.locator || node.id || '').includes('/path/?'))
}

const loadCatalogRoot = (engineId) => {
  const key = Number(engineId)
  const existing = catalogRootLoadPromises.get(key)
  if (existing) return existing

  const request = store.loadTree(key).finally(() => {
    if (catalogRootLoadPromises.get(key) === request) {
      catalogRootLoadPromises.delete(key)
    }
  })
  catalogRootLoadPromises.set(key, request)
  return request
}

const isBranchNode = (node) => !!node && branchTypes.has(node.type)

const nodeEngineID = (node) => {
  if (node?.engineId) return node.engineId
  const locator = node?.locator || node?.id
  return locator ? parseLocator(locator)?.engineId : null
}

const isNodeEngineAvailable = (node) => store.isEngineAvailable(nodeEngineID(node))

const engineStatusLabel = (node) => t(`common.engineStatus.${store.engineState(nodeEngineID(node))}`)

const resolveNodeClassName = (node) => isNodeEngineAvailable(node) ? '' : 'engine-resource-offline'

function startScanStatus(title, detail, percent = 5) {
  cancelScanStatusTimer()
  activeScan.value = {
    visible: true,
    title,
    detail,
    percent: clampScanPercent(percent),
    status: ''
  }
}

function updateScanStatusFromRun(run, title = '', minPercent = 10) {
  cancelScanStatusTimer()
  const progress = Number(run?.progress)
  const percent = Number.isFinite(progress)
    ? clampScanPercent(Math.max(progress, minPercent))
    : clampScanPercent(minPercent)
  activeScan.value = {
    visible: true,
    title: title || scanTitleFromRun(run),
    detail: scanDetailFromRun(run),
    percent,
    status: ''
  }
}

function failScanStatus(error) {
  cancelScanStatusTimer()
  activeScan.value = {
    visible: true,
    title: t('manager.explorer.scanFailed'),
    detail: error?.message || t('manager.explorer.scanFailed'),
    percent: 100,
    status: 'exception'
  }
}

function completeScanStatus() {
  cancelScanStatusTimer()
  activeScan.value = {
    visible: true,
    title: t('manager.explorer.scanCompleted'),
    detail: t('manager.explorer.scanCompleted'),
    percent: 100,
    status: 'success'
  }
  scanStatusTimer = window.setTimeout(() => {
    clearScanStatus()
  }, 5000)
}

function clearScanStatus() {
  cancelScanStatusTimer()
  activeScan.value = {
    visible: false,
    title: '',
    detail: '',
    percent: 0,
    status: ''
  }
}

function cancelScanStatusTimer() {
  if (scanStatusTimer) {
    window.clearTimeout(scanStatusTimer)
    scanStatusTimer = 0
  }
}

function scanTitleFromRun(run) {
  const status = String(run?.status || '').toLowerCase()
  if (status === 'pending') {
    return t('manager.explorer.scanSubmitted')
  }
  if (status === 'running') {
    return t('manager.explorer.scanRunning')
  }
  return t('manager.explorer.scanSubmitted')
}

function scanDetailFromRun(run) {
  return run?.current_step || run?.progress_message || run?.message || t('manager.explorer.scanWaiting')
}

function clampScanPercent(value) {
  return Math.max(0, Math.min(100, Math.round(value)))
}

// 暴露方法供父组件调用
defineExpose({
  expandNode: (locator) => store.expandNode(locator),
  collapseNode: (locator) => store.collapseNode(locator),
  scrollToNode: (locator, options) => resourceTreeRef.value?.scrollToNode(locator, options)
})
</script>

<style scoped>
.explorer-tree {
  overflow: visible;
}

.skeleton-loader {
  padding: 20px;
}

.scan-status {
  position: sticky;
  top: 0;
  z-index: 3;
  padding: 10px 12px;
  border-bottom: 1px solid var(--el-border-color);
  background: var(--el-bg-color);
}

.scan-status__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 6px;
  font-size: 12px;
}

.scan-status__title {
  min-width: 0;
  overflow: hidden;
  color: var(--el-text-color-primary);
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.scan-status__percent {
  flex: 0 0 auto;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
}

.scan-status__detail {
  margin-top: 6px;
  overflow: hidden;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.explorer-node-label {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.explorer-node-label__text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:deep(.engine-resource-offline .tree-node) {
  opacity: 0.68;
}
</style>
