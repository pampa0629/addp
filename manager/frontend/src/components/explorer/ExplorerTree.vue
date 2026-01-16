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
      title="存储引擎"
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
import { ResourceTree } from '@addp/common-frontend'
import { parseLocator } from '@addp/common-frontend'
import { useExplorerStore } from '@/stores/explorer'
import client from '@/api/client'

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
      label: '刷新',
      icon: 'Refresh',
      visible: () => true
    },
    // 向量化操作（仅 MinIO/S3 的单个对象）
    {
      id: 'embedding',
      name: 'embedding',
      label: '向量化',
      icon: 'MagicStick',
      visible: (node) => {
        return (node.engineType === 'minio' || node.engineType === 's3') && node.type === 'object'
      }
    },
    // 批量向量化操作（MinIO/S3 的目录或 Bucket）
    {
      id: 'embedding-batch',
      name: 'embedding-batch',
      label: '批量向量化',
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

    ElMessage.success('刷新成功')
  } catch (error) {
    ElMessage.error('刷新失败: ' + error.message)
  }
}

// 事件处理：节点点击
const handleNodeClick = async (node) => {
  const locator = node.locator || node.id

  console.log('[ExplorerTree] 节点点击:', {
    label: node.label,
    type: node.type,
    locator: locator,
    hasChildren: !!node.children,
    childrenCount: node.children?.length || 0
  })

  // 选择节点
  store.selectNode(locator)

  // 触发父组件的 node-select 事件（用于加载预览）
  emit('node-select', { node, locator })

  // 如果是引擎节点，点击时展开并加载内容
  if (node.type === 'engine' && node.engineId) {
    console.log('[ExplorerTree] 引擎节点点击')
    // 展开节点
    store.expandNode(locator)

    // 如果未加载过，懒加载内容
    if (!node.loaded) {
      try {
        await store.loadTree(node.engineId)
      } catch (error) {
        console.error('加载引擎内容失败:', error)
        ElMessage.error('加载引擎内容失败: ' + error.message)
      }
    }
    return
  }

  // 如果是容器类型节点（directory/bucket/prefix/schema/database），点击时切换展开状态
  const isDirLike = ['directory', 'bucket', 'prefix', 'schema', 'database'].includes(node.type)
  console.log('[ExplorerTree] 容器类型检查:', { isDirLike, nodeType: node.type })

  if (isDirLike) {
    // 检查节点是否需要加载子节点
    const needsLoading = (!node.children || node.children.length === 0 || !node.loaded) && node.hasChildren
    const isExpanded = store.expandedLocators.has(locator)

    console.log('[ExplorerTree] 节点状态:', {
      isExpanded,
      needsLoading,
      hasChildren: node.hasChildren,
      childrenCount: node.children?.length || 0,
      loaded: node.loaded
    })

    // 如果节点已展开但还没有加载子节点，先加载子节点
    if (isExpanded && needsLoading) {
      console.log('[ExplorerTree] 节点已展开但未加载子节点，触发加载:', node.label)
      try {
        const loc = parseLocator(locator)
        if (loc && loc.engineId) {
          // 强制刷新，绕过缓存，确保从后端加载数据
          await store.loadNodeChildren(locator, 1, true)
        }
      } catch (error) {
        console.error('加载子节点失败:', error)
        ElMessage.error('加载子节点失败: ' + error.message)
      }
      return
    }

    // 如果节点已展开且已加载子节点，折叠它
    if (isExpanded) {
      console.log('[ExplorerTree] 折叠节点:', node.label)
      store.collapseNode(locator)
      return
    }

    // 节点未展开，展开它
    console.log('[ExplorerTree] 展开节点:', node.label)
    store.expandNode(locator)

    // 如果需要加载子节点，触发加载
    if (needsLoading) {
      try {
        const loc = parseLocator(locator)
        if (loc && loc.engineId) {
          console.log('[ExplorerTree] 点击加载子节点:', node.label)
          // 强制刷新，绕过缓存，确保从后端加载数据
          await store.loadNodeChildren(locator, 1, true)
        }
      } catch (error) {
        console.error('加载子节点失败:', error)
        ElMessage.error('加载子节点失败: ' + error.message)
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
      ElMessage.success('刷新成功')
    } catch (error) {
      ElMessage.error('刷新失败: ' + error.message)
    }
    return
  }

  if (action === 'embedding' || action === 'embedding-batch') {
    // 只支持 MinIO/S3 对象存储
    if (node.engineType !== 'minio' && node.engineType !== 's3') {
      ElMessage.warning('向量化功能仅支持对象存储（MinIO/S3）')
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
          ElMessage.warning('该操作仅适用于单个文件')
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
          ElMessage.warning('批量向量化仅适用于目录或 Bucket')
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
        ElMessage.success(`向量化任务已提交（文件：${params.object_key}）`)
      } else {
        ElMessage.success(`批量向量化任务已提交（${node.type === 'bucket' ? 'Bucket' : '目录'}：${node.label}）`)
      }

      console.log('向量化任务响应:', response.data)
    } catch (error) {
      console.error('向量化失败:', error)
      ElMessage.error('向量化失败: ' + (error.response?.data?.error || error.message))
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
      ElMessage.error('加载引擎内容失败: ' + error.message)
    }
    return
  }

  // 🚀 优化：使用增量加载替代全量重载
  // 如果是容器节点且未加载子节点，使用增量加载
  const isDirLike = ['directory', 'bucket', 'prefix', 'schema', 'database'].includes(node.type)
  const needsLoading = isDirLike && (!node.children || node.children.length === 0 || !node.loaded)

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
      ElMessage.error('加载子节点失败: ' + error.message)
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
