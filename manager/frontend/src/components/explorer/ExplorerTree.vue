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
        :expand-on-click-node="true"
        :title="t('manager.explorer.storageEngines')"
        :count-text="(count) => t('manager.explorer.countText', { count })"
        @refresh="handleRefresh"
        @node-click="handleNodeClick"
        @node-action="handleNodeAction"
        @node-expand="handleNodeExpand"
        @node-collapse="handleNodeCollapse"
      />
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
      visible: () => true
    },
    // 向量化操作（仅 MinIO/S3 的单个对象）
    {
      id: 'embedding',
      name: 'embedding',
      label: t('manager.explorer.vectorize'),
      icon: 'MagicStick',
      visible: (node) => {
        return (node.engineType === 'minio' || node.engineType === 's3') && node.type === 'object'
      }
    },
    // 批量向量化操作（MinIO/S3 的目录或 Bucket）
    {
      id: 'embedding-batch',
      name: 'embedding-batch',
      label: t('manager.explorer.batchVectorize'),
      icon: 'Files',
      visible: (node) => {
        return (node.engineType === 'minio' || node.engineType === 's3') && (node.type === 'directory' || node.type === 'bucket')
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
  const locator = node.locator || node.id

  // 选择节点
  store.selectNode(locator)
  emit('node-select', { node, locator })

  // 关键：@node-collapse / @node-expand 在 element-plus 中先于 @node-click 触发，
  // 因此这里读到的是"点击后"的 store 状态，而不是点击前的状态。
  // 只有当节点当前未展开 && 需要首次加载数据时，才主动介入（强制展开 + 加载）。
  // 对于已加载的节点，expand-on-click-node 和 handleNodeExpand/Collapse 已经处理好了。
  const isCurrentlyExpanded = store.expandedLocators.has(locator)

  // Catalog root 节点：仅首次展开（未加载）时强制展开并加载数据
  if (isCatalogRootNode(node) && node.engineId) {
    if (!isCurrentlyExpanded && !node.loaded) {
      store.expandNode(locator)
      try {
        await store.loadTree(node.engineId)
      } catch (error) {
        console.error('加载引擎内容失败:', error)
        ElMessage.error(t('manager.explorer.loadEngineFailed', { error: error.message }))
      }
    }
    return
  }

  // 仅在需要首次加载子节点时介入，避免与 el-tree 的 expand-on-click-node 冲突
  // 注意：不使用 !node.loaded 判断，避免已有子节点的节点在折叠后点击时被重新展开
  const isDirLike = isBranchNode(node)
  if (isDirLike) {
    const needsLoading = (node.children || []).length === 0 && node.hasChildren
    if (!isCurrentlyExpanded && needsLoading) {
      // 首次展开：el-tree 因暂无子节点可能不会自动展开，需要强制 store 记录展开状态
      store.expandNode(locator)
      try {
        const loc = parseLocator(locator)
        if (loc && loc.engineId) {
          await store.loadNodeChildren(locator, 1, true)
        }
      } catch (error) {
        console.error('加载子节点失败:', error)
        ElMessage.error(t('manager.explorer.loadChildrenFailed', { error: error.message }))
      }
    }
  }
}

// 事件处理：节点操作
const handleNodeAction = async ({ node, action }) => {
  const locator = node.locator || node.id

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
    if (node.engineType !== 'minio' && node.engineType !== 's3') {
      ElMessage.warning(t('manager.explorer.vectorizeOnlyStorage'))
      return
    }

    try {
      // 解析 locator 提取参数
      const loc = parseLocator(locator)
      const engineId = loc.engineId
      const bucket = loc.path[0] // 第一个路径段是 bucket

      // 构建请求参数
      const params = {
        engine_id: engineId,
        bucket: bucket
      }

      // 根据节点类型设置不同的参数
      if (action === 'embedding') {
        // 单个对象向量化
        if (node.type !== 'object') {
          ElMessage.warning(t('manager.explorer.vectorizeSingleFileOnly'))
          return
        }
        params.scope = 'object'
        params.object_key = loc.path.slice(1).join('/') // bucket 后面的所有路径段
      } else {
        // 批量向量化
        if (node.type === 'directory') {
          params.scope = 'directory'
          params.prefix = loc.path.slice(1).join('/') + '/' // bucket 后面的路径作为前缀
          params.recursive = true
        } else if (node.type === 'bucket') {
          params.scope = 'bucket'
        } else {
          ElMessage.warning(t('manager.explorer.batchVectorizeDirOnly'))
          return
        }
      }

      // 调用后端 API
      await client.post('/manager/embedding', {
        operator_name: 'embedding',
        params: params,
        execute_now: true
      })

      if (action === 'embedding') {
        ElMessage.success(t('manager.explorer.vectorizeSubmitted', { key: params.object_key }))
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
      await store.loadTree(node.engineId)
    } catch (error) {
      console.error('加载引擎内容失败:', error)
      ElMessage.error(t('manager.explorer.loadEngineFailed', { error: error.message }))
    }
    return
  }

  // 使用增量加载替代全量重载：容器节点展开且子节点为空时，只加载该节点的直接子节点。
  const needsLoading = isBranchNode(node) && (node.children || []).length === 0

  if (needsLoading && locator) {
    try {
      // 从 locator 解析出 engineId
      const loc = parseLocator(locator)
      if (loc && loc.engineId) {
        // 只加载该节点的子节点，不重新加载整个树。
        // 强制刷新，绕过缓存，确保从后端加载数据
        await store.loadNodeChildren(locator, 1, true)
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

const isBranchNode = (node) => !!node && branchTypes.has(node.type)

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
  height: 100%;
  overflow: auto;
}

.skeleton-loader {
  padding: 20px;
  height: 100%;
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
</style>
