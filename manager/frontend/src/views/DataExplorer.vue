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
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ResourceTree } from '@addp/common-frontend'
import { parseLocator, buildLocator } from '@addp/common-frontend'
import PreviewPanel from '@/components/explorer/PreviewPanel.vue'
import Splitter from '@/components/explorer/Splitter.vue'
import { useResizable } from '@/composables/useResizable'
import { useExplorerStore } from '@/stores/explorer'

// 树形面板宽度
const { size: treeWidth, startResize: startTreeResize } = useResizable(320, 220, 600, 'horizontal')

const route = useRoute()
const router = useRouter()
const store = useExplorerStore()

// 节点操作
const nodeActions = ref([
  { id: 'refresh', label: '刷新', icon: 'Refresh' }
])

// 计算属性：树的根节点（引擎列表）
const treeChildren = computed(() => {
  return store.engineNodes
})

// 计算属性：展开的节点 keys（转换为 ResourceTree 期望的格式）
const expandedKeys = computed({
  get: () => Array.from(store.expandedLocators),
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

// 事件处理：节点操作（action 是字符串，如 'refresh'）
const handleNodeAction = async ({ node, action }) => {
  if (action === 'refresh') {
    const locator = node.locator || node.id

    try {
      await store.refreshNode(locator)
      ElMessage.success('刷新成功')
    } catch (error) {
      ElMessage.error('刷新失败: ' + error.message)
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
      // 收集初始需要展开的引擎节点 locators
      const engineLocators = store.engines.map(engine =>
        `addp://engine/${engine.id}/path/?type=database`
      )

      // 并行加载所有引擎的树数据
      await Promise.all(
        store.engines.map(engine => store.loadTree(engine.id))
      )

      // 一次性设置初始展开状态（只展开引擎层级）
      store.expandedLocators = new Set(engineLocators)
    }
  } catch (error) {
    ElMessage.error('初始化失败: ' + error.message)
  }
})

// 监听路由变化
watch(() => route.query, (query) => {
  // TODO: 根据路由参数选择节点
  console.log('路由参数变化:', query)
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
