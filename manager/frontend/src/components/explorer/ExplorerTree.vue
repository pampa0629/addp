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
    <ResourceTree
      v-else
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
  </div>
</template>

<script setup>
import { computed } from 'vue'
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

// 节点操作（根据节点类型动态生成）
const nodeActions = computed(() => {
  return [
    // 刷新操作（所有节点都支持）
    {
      id: 'refresh',
      name: 'refresh',
      label: t('manager.explorer.refresh'),
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

// 计算属性：树的根节点（引擎列表）
const treeData = computed(() => {
  return store.engineNodes
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

// 事件处理：刷新整个引擎列表
const handleRefresh = async () => {
  try {
    // 清空引擎树缓存
    store.engineTrees = {}
    store.engineTreeDepths = {}
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

  // 引擎节点：仅首次展开（未加载）时强制展开并加载数据
  if (node.type === 'engine' && node.engineId) {
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
  const isDirLike = ['directory', 'bucket', 'prefix', 'schema', 'database'].includes(node.type)
  if (isDirLike) {
    const realChildren = (node.children || []).filter(c => c.type !== '__sentinel__')
    const needsLoading = realChildren.length === 0 && node.hasChildren
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
      await store.refreshNode(locator)
      ElMessage.success(t('manager.explorer.refreshSuccess'))
    } catch (error) {
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
      const response = await client.post('/manager/embedding', {
        operator_name: 'embedding',
        params: params,
        execute_now: true
      })

      // 显示成功消息
      if (action === 'embedding') {
        ElMessage.success(t('manager.explorer.vectorizeSubmitted', { key: params.object_key }))
      } else {
        ElMessage.success(t('manager.explorer.batchVectorizeSubmitted', { label: node.label }))
      }

      console.log('向量化任务响应:', response.data)
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

  console.log('[ExplorerTree] 节点展开:', {
    label: node.label,
    type: node.type,
    locator: locator,
    hasChildren: !!node.children,
    childrenCount: node.children?.length || 0,
    loaded: node.loaded
  })

  // 如果是引擎节点且未加载过，懒加载其内容
  if (node.type === 'engine' && node.engineId && !node.loaded) {
    try {
      await store.loadTree(node.engineId)
    } catch (error) {
      console.error('加载引擎内容失败:', error)
      ElMessage.error(t('manager.explorer.loadEngineFailed', { error: error.message }))
    }
    return
  }

  // 🚀 优化：使用增量加载替代全量重载
  // 如果是容器节点且子节点为空，使用增量加载
  // 注意：过滤哨兵节点（__sentinel__）后再判断是否需要加载
  const isDirLike = ['directory', 'bucket', 'prefix', 'schema', 'database'].includes(node.type)
  const realChildren = (node.children || []).filter(c => c.type !== '__sentinel__')
  const needsLoading = isDirLike && realChildren.length === 0

  console.log('[ExplorerTree] 增量加载检查:', {
    isDirLike,
    needsLoading,
    shouldLoad: needsLoading && locator
  })

  if (needsLoading && locator) {
    try {
      // 从 locator 解析出 engineId
      const loc = parseLocator(locator)
      if (loc && loc.engineId) {
        console.log('[ExplorerTree] 增量加载子节点:', node.label, 'engine:', loc.engineId)

        // ⚡ 关键改进：只加载该节点的子节点，不重新加载整个树
        // 强制刷新，绕过缓存，确保从后端加载数据
        await store.loadNodeChildren(locator, 1, true)

        console.log('[ExplorerTree] 增量加载完成')
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

// 暴露方法供父组件调用
defineExpose({
  expandNode: (locator) => store.expandNode(locator),
  collapseNode: (locator) => store.collapseNode(locator),
  selectNode: (locator) => store.selectNode(locator)
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
</style>
