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
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { parseLocator } from '@addp/common-frontend'
import ExplorerTree from '@/components/explorer/ExplorerTree.vue'
import ExplorerSearch from '@/components/explorer/ExplorerSearch.vue'
import PreviewPanel from '@/components/explorer/PreviewPanel.vue'
import Splitter from '@/components/explorer/Splitter.vue'
import { useResizable } from '@/composables/useResizable'
import { useExplorerStore } from '@/stores/explorer'

// 树形面板宽度
const { size: treeWidth, startResize: startTreeResize } = useResizable(320, 220, 600, 'horizontal')

const route = useRoute()
const store = useExplorerStore()

// 引用
const treeRef = ref(null)

// 控制搜索显示（可选功能，暂时隐藏）
const showSearch = ref(false)

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

// 事件处理：节点选择（从 ExplorerTree 组件触发）
const handleNodeSelect = async ({ node, locator }) => {
  console.log('[DataExplorer] 节点选择:', { label: node.label, locator })

  // 非引擎节点，加载预览
  if (node.type !== 'engine') {
    try {
      await store.loadPreview(locator, 1)
    } catch (error) {
      console.error('加载预览失败:', error)
      ElMessage.error('加载预览失败: ' + error.message)
    }
  }
}

// 事件处理：搜索结果选择
const handleSearchResultSelect = async (result) => {
  console.log('[DataExplorer] 搜索结果选择:', result)

  const locator = result.node.locator || result.node.id

  // 1. 展开路径上的所有节点
  for (const segment of result.path) {
    const segmentLocator = segment.locator
    if (segmentLocator) {
      treeRef.value?.expandNode(segmentLocator)
    }
  }

  // 2. 选中目标节点
  treeRef.value?.selectNode(locator)
  store.selectNode(locator)

  // 3. 加载预览
  try {
    await store.loadPreview(locator, 1)
    ElMessage.success('已定位到搜索结果')
  } catch (error) {
    console.error('加载预览失败:', error)
    ElMessage.error('加载预览失败: ' + error.message)
  }
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
    if (store.engines.length === 0) {
      console.log('[DataExplorer] 等待引擎列表加载...')
      await store.loadEngines()
    }

    // 2. 确保引擎存在
    const engine = store.engines.find(e => e.id === engineId)
    if (!engine) {
      console.warn('[DataExplorer] 引擎未找到:', engineId)
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

    // 4. 直接选中并加载预览
    console.log('[DataExplorer] 选中节点并加载预览...')
    store.selectNode(targetLocator)
    await store.loadPreview(targetLocator, 1)

    // 5. 计算目标路径深度，加载足够深的树
    const pathParts = objectKey.split('/').filter(p => p)
    const requiredDepth = 1 + pathParts.length
    console.log('[DataExplorer] 目标路径深度:', requiredDepth, '路径:', objectKey)

    // 加载引擎树（确保深度足够）
    await store.loadTree(engineId, requiredDepth)

    // 6. 展开路径上的所有节点
    const engineTree = store.engineTrees[engineId]
    const expandKeys = []

    if (engineTree) {
      // 添加引擎节点
      expandKeys.push(engineTree.id)

      // 查找并展开路径上的节点
      const bucketNode = engineTree.children?.find(c => c.label === bucket)
      if (bucketNode) {
        expandKeys.push(bucketNode.id)

        // 展开中间目录
        if (pathParts.length > 1) {
          let currentNode = bucketNode
          for (let i = 0; i < pathParts.length - 1; i++) {
            const dirName = pathParts[i]
            const dirNode = currentNode.children?.find(c => c.label === dirName)
            if (dirNode) {
              expandKeys.push(dirNode.id)
              currentNode = dirNode
            } else {
              break
            }
          }
        }
      }
    }

    // 7. 更新展开状态
    store.expandedLocators = new Set([...store.expandedLocators, ...expandKeys])

    // 8. 找到目标文件节点的实际 ID
    if (objectKey && pathParts.length > 0) {
      const bucketNode = engineTree?.children?.find(c => c.label === bucket)
      if (bucketNode) {
        let currentNode = bucketNode
        for (let i = 0; i < pathParts.length - 1; i++) {
          const dirName = pathParts[i]
          const dirNode = currentNode.children?.find(c => c.label === dirName)
          if (dirNode) {
            currentNode = dirNode
          } else {
            break
          }
        }
        const fileName = pathParts[pathParts.length - 1]
        const fileNode = currentNode.children?.find(c => c.label === fileName)
        if (fileNode) {
          store.selectNode(fileNode.id)
        }
      }
    }

    await nextTick()
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
</style>
