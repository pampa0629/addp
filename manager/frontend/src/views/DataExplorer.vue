<template>
  <div class="data-explorer">
    <div class="split-container" :style="{ gridTemplateColumns: treeWidth + 'px 8px 1fr' }">
      <!-- 左侧资源树 -->
      <div class="tree-container">
        <!-- 加载状态：显示骨架屏 -->
        <el-skeleton
          v-if="store.loadingEngines"
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
          :tree-data="treeChildren"
          :loading="false"
          :refreshing-node-ids="refreshingNodeIds"
          v-model:expanded-keys="expandedKeys"
          :current-node-key="currentNodeKey"
          :node-actions="nodeActions"
          title="存储引擎"
          @refresh="handleRefresh"
          @node-click="handleNodeClick"
          @node-action="handleNodeAction"
          @node-expand="handleNodeExpand"
          @node-collapse="handleNodeCollapse"
        />
      </div>

      <!-- 可拖拽分隔器 -->
      <Splitter direction="horizontal" @resize="startTreeResize" />

      <!-- 右侧预览面板 -->
      <PreviewPanel
        :selected-node="selectedNodeLegacy"
        :preview-data="store.previewData"
        :loading="store.previewLoading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ResourceTree } from '@addp/common-frontend'
import { parseLocator, buildLocator } from '@addp/common-frontend'
import PreviewPanel from '@/components/explorer/PreviewPanel.vue'
import Splitter from '@/components/explorer/Splitter.vue'
import { useResizable } from '@/composables/useResizable'
import { useExplorerStore } from '@/stores/explorer'
import client from '@/api/client'

// 树形面板宽度
const { size: treeWidth, startResize: startTreeResize } = useResizable(320, 220, 600, 'horizontal')

const route = useRoute()
const router = useRouter()
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
const treeChildren = computed(() => {
  return store.engineNodes
})

// 计算属性：展开的节点 keys（转换为 ResourceTree 期望的格式）
const expandedKeys = computed({
  get: () => {
    return Array.from(store.expandedLocators)
  },
  set: (keys) => {
    store.expandedLocators = new Set(keys)
  }
})

// 计算属性：正在刷新的节点 IDs（转换为 locator）
const refreshingNodeIds = computed(() => {
  return Array.from(store.refreshingLocators)
})

// 计算属性：当前选中的节点 key
const currentNodeKey = computed(() => store.selectedLocator || '')

// 计算属性：兼容旧版 PreviewPanel 的 selectedNode 格式
const selectedNodeLegacy = computed(() => {
  if (!store.selectedNode) return null

  const loc = parseLocator(store.selectedLocator)

  return {
    id: store.selectedLocator,
    locator: store.selectedLocator,
    engineId: loc.engineId,
    schema: loc.path[0] || '',
    table: loc.path[1] || '',
    path: loc.path.join('/'),
    type: loc.type,
    label: store.selectedNode.label
  }
})

// 事件处理：刷新整个引擎列表
const handleRefresh = async () => {
  try {
    // 清空引擎树缓存
    store.engineTrees = {}
    // 重新加载引擎列表
    await store.loadEngines()
    ElMessage.success('刷新成功')
  } catch (error) {
    ElMessage.error('刷新失败: ' + error.message)
  }
}

// 事件处理：节点点击
const handleNodeClick = async (node) => {
  const locator = node.locator || node.id

  // 选择节点
  store.selectNode(locator)

  // 如果是引擎节点，点击时展开并加载内容
  if (node.type === 'engine' && node.engineId) {
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

  // 非引擎节点，加载预览
  try {
    await store.loadPreview(locator, 1)
  } catch (error) {
    console.error('加载预览失败:', error)
    ElMessage.error('加载预览失败: ' + error.message)
  }
}

// 事件处理：节点操作（action 是字符串，如 'refresh'、'vectorize'）
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
      const response = await client.post('/operators/embedding/execute', {
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

// 事件处理：节点展开（懒加载引擎内容）
const handleNodeExpand = async (node) => {
  const locator = node.locator || node.id
  store.expandNode(locator)

  // 如果是引擎节点且未加载过，懒加载其内容
  if (node.type === 'engine' && node.engineId && !node.loaded) {
    try {
      await store.loadTree(node.engineId)
    } catch (error) {
      console.error('加载引擎内容失败:', error)
      ElMessage.error('加载引擎内容失败: ' + error.message)
    }
  }
}

// 事件处理：节点折叠
const handleNodeCollapse = (node) => {
  const locator = node.locator || node.id
  store.collapseNode(locator)
}

// 事件处理：分页变化
const handlePageChange = async (page) => {
  if (!store.selectedLocator) return

  try {
    await store.loadPreview(store.selectedLocator, page)
  } catch (error) {
    ElMessage.error('加载预览失败: ' + error.message)
  }
}

// 事件处理：导航（预留接口）
const handleNavigate = (params) => {
  console.log('导航:', params)
  // TODO: 实现导航逻辑
}

// 初始化
onMounted(async () => {
  try {
    // 1. 加载引擎列表
    await store.loadEngines()

    // 2. 并行加载所有引擎的树结构（但不影响展开状态）
    if (store.engines.length > 0) {
      // 并行加载所有引擎的树数据
      await Promise.all(
        store.engines.map(engine => store.loadTree(engine.id))
      )

      // 等待DOM更新后，再设置初始展开状态（只展开引擎层级）
      await nextTick()

      // 收集初始需要展开的引擎节点 locators
      const engineLocators = store.engines.map(engine =>
        `addp://engine/${engine.id}/path/?type=database`
      )

      // 通过expandedKeys的set方法设置，触发双向绑定
      expandedKeys.value = engineLocators
    }
  } catch (error) {
    ElMessage.error('初始化失败: ' + error.message)
  }
})

// 监听路由变化，根据参数自动定位和选中对象
watch(() => route.query, async (query) => {
  console.log('[DataExplorer] 路由参数变化:', query)

  if (!query.engineId || !query.bucket) {
    return
  }

  try {
    const engineId = parseInt(query.engineId)
    const bucket = query.bucket
    const objectKey = query.objectKey || ''

    // 1. 等待引擎列表加载完成
    // 如果还没有加载引擎，先等待加载
    if (store.engines.length === 0) {
      console.log('[DataExplorer] 等待引擎列表加载...')
      await store.loadEngines()
    }

    // 2. 确保引擎存在
    const engine = store.engines.find(e => e.id === engineId)
    if (!engine) {
      console.warn('[DataExplorer] 引擎未找到:', engineId, '可用引擎:', store.engines.map(e => ({ id: e.id, name: e.name })))
      ElMessage.warning(`引擎 ${engineId} 未找到`)
      return
    }

    console.log('[DataExplorer] 找到引擎:', engine.name)

    // 3. 构建目标节点的 locator
    let targetLocator
    if (objectKey) {
      // 定位到具体对象
      targetLocator = `addp://engine/${engineId}/path/${bucket}/${objectKey}?type=file`
    } else {
      // 只定位到 bucket
      targetLocator = `addp://engine/${engineId}/path/${bucket}?type=bucket`
    }

    console.log('[DataExplorer] 目标 locator:', targetLocator)

    // 4. 直接选中并加载预览（不依赖树展开）
    console.log('[DataExplorer] 选中节点并加载预览...')
    store.selectNode(targetLocator)
    await store.loadPreview(targetLocator, 1)

    // 5. 计算目标路径深度，加载足够深的树
    const pathParts = objectKey.split('/').filter(p => p)
    // 深度 = bucket (1层) + 中间目录层数 + 文件本身 (1层)
    const requiredDepth = 1 + pathParts.length
    console.log('[DataExplorer] 目标路径深度:', requiredDepth, '路径:', objectKey)

    // 加载引擎树（确保深度足够）
    await store.loadTree(engineId, requiredDepth)

    // 🔍 调试：检查实际加载的树节点 locator 和 id
    const engineTree = store.engineTrees[engineId]
    const expandKeys = []

    if (engineTree) {
      console.log('[DataExplorer] 树根节点:', {
        id: engineTree.id,
        locator: engineTree.locator,
        label: engineTree.label,
        childrenCount: engineTree.children?.length
      })

      // 添加引擎节点（使用实际的 id）
      expandKeys.push(engineTree.id)

      // 检查并添加 bucket 节点
      const bucketNode = engineTree.children?.find(c => c.label === bucket)
      if (bucketNode) {
        console.log('[DataExplorer] Bucket 节点:', {
          id: bucketNode.id,
          locator: bucketNode.locator,
          label: bucketNode.label,
          childrenCount: bucketNode.children?.length
        })

        // 添加 bucket 节点（使用实际的 id）
        expandKeys.push(bucketNode.id)

        // 检查并添加 image 目录节点
        if (pathParts.length > 1) {
          // 需要展开中间目录
          let currentNode = bucketNode
          for (let i = 0; i < pathParts.length - 1; i++) {
            const dirName = pathParts[i]
            const dirNode = currentNode.children?.find(c => c.label === dirName)
            if (dirNode) {
              console.log(`[DataExplorer] 目录节点 ${dirName}:`, {
                id: dirNode.id,
                locator: dirNode.locator,
                label: dirNode.label,
                childrenCount: dirNode.children?.length
              })
              expandKeys.push(dirNode.id)
              currentNode = dirNode
            } else {
              console.warn(`[DataExplorer] 未找到目录节点: ${dirName}`)
              break
            }
          }
        }
      } else {
        console.warn(`[DataExplorer] 未找到 bucket 节点: ${bucket}`)
      }
    }

    console.log('[DataExplorer] 需要展开的路径 (使用 id):', expandKeys)

    // 6. 展开路径上的所有节点（使用从树中提取的实际 locator）
    console.log('[DataExplorer] 当前已展开节点:', Array.from(store.expandedLocators))
    expandedKeys.value = [...new Set([...expandedKeys.value, ...expandKeys])]
    console.log('[DataExplorer] 更新后已展开节点:', Array.from(store.expandedLocators))
    console.log('[DataExplorer] expandedKeys 计算属性值:', expandedKeys.value)

    // 8. 等待 DOM 更新
    await nextTick()

    console.log('[DataExplorer] DOM 更新后，expandedKeys:', expandedKeys.value)

    // 详细检查 treeChildren 的结构
    const engineNode67 = treeChildren.value.find(n => n.engineId === 67)
    if (engineNode67) {
      console.log('[DataExplorer] 引擎 67 节点详情:', {
        label: engineNode67.label,
        locator: engineNode67.locator,
        loaded: engineNode67.loaded,
        childrenCount: engineNode67.children?.length,
        children: engineNode67.children?.map(c => ({
          label: c.label,
          locator: c.locator || c.id,
          type: c.type,
          childrenCount: c.children?.length
        }))
      })

      // 检查 addp bucket 节点
      const addpBucket = engineNode67.children?.find(c => c.label === 'addp')
      if (addpBucket) {
        console.log('[DataExplorer] addp bucket 详情:', {
          label: addpBucket.label,
          locator: addpBucket.locator || addpBucket.id,
          childrenCount: addpBucket.children?.length,
          children: addpBucket.children?.map(c => ({
            label: c.label,
            locator: c.locator || c.id
          }))
        })
      }
    }

    // 🔍 强制触发响应式更新
    console.log('[DataExplorer] 强制触发更新...')
    store.expandedLocators = new Set(expandedKeys.value)
    await nextTick()

    // 再次等待，确保 ResourceTree 组件完成渲染
    setTimeout(() => {
      console.log('[DataExplorer] 最终 expandedKeys:', expandedKeys.value)
      console.log('[DataExplorer] 最终 store.expandedLocators:', Array.from(store.expandedLocators))
    }, 100)

    console.log('[DataExplorer] 成功定位到对象')
    ElMessage.success('已定位到目标对象')
  } catch (error) {
    console.error('[DataExplorer] 定位失败:', error)
    ElMessage.error('定位失败: ' + error.message)
  }
}, { immediate: true })
</script>

<style scoped>
.data-explorer {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.split-container {
  flex: 1;
  display: grid;
  gap: 0;
  overflow: hidden;
}

.tree-container {
  height: 100%;
  overflow: auto;
}

.skeleton-loader {
  padding: 20px;
  height: 100%;
}
</style>
