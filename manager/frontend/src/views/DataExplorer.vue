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
          :selected-node="selectedNodeLegacy"
          :preview-data="store.previewData"
          :loading="store.previewLoading || store.childPreviewLoading"
          @page-change="handlePageChange"
          @navigate="handleNavigate"
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

// 控制搜索显示（可选功能，暂时隐藏）
const showSearch = ref(false)

const itemTypes = new Set(['table', 'view', 'collection', 'label', 'relationship', 'file', 'object'])
const nodeTypes = new Set(['schema', 'database', 'bucket', 'prefix', 'directory', 'root', 'dir'])

// 计算属性：兼容旧版 PreviewPanel 的 selectedNode 格式
const selectedNodeLegacy = computed(() => {
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
  return (node.children || []).filter(child => child.type !== '__sentinel__')
})

// 事件处理：节点选择（从 ExplorerTree 组件触发）
const handleNodeSelect = async ({ node, locator }) => {
  console.log('[DataExplorer] 节点选择:', { label: node.label, locator })

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
    await store.loadPreview(store.selectedLocator, page, store.selectedChildName, store.selectedComponentPath, store.selectedNestedChildPath)
  } catch (error) {
    ElMessage.error(t('manager.explorer.loadPreviewFailed', { error: error.message }))
  }
}

const handleChildChange = async (payload) => {
  const childName = typeof payload === 'string' ? payload : payload?.childName
  const componentPath = typeof payload === 'object' ? payload?.componentPath || '' : ''
  const nestedChildPath = typeof payload === 'object' ? payload?.nestedChildPath || '' : ''
  const componentSwitch = typeof payload === 'object' && payload?.componentSwitch
  if (!store.selectedLocator || (!childName && !componentPath && !nestedChildPath && !componentSwitch)) return
  try {
    await store.loadPreview(store.selectedLocator, 1, childName, componentPath, nestedChildPath)
  } catch (error) {
    ElMessage.error(t('manager.explorer.loadPreviewFailed', { error: error.message }))
  }
}

// 事件处理：导航（预留接口）
const handleNavigate = (params) => {
  console.log('导航:', params)
  // TODO: 实现导航逻辑
}

const handleOpenNode = async (locator) => {
  if (!locator) return
  store.selectNode(locator)
  const node = store.selectedNode
  if (!node) return
  await handleNodeSelect({ node, locator })
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

      // 暴露一个临时检查函数到 window，便于在浏览器控制台对所有引擎执行折叠校验
      // 用法（在浏览器控制台执行）：window.__checkExplorerCollapse()
      try {
        // eslint-disable-next-line no-undef
        window.__checkExplorerCollapse = async () => {
          const results = []
          // 遍历每个已加载的引擎
          for (const engine of store.engines) {
            const engineLocator = `addp://engine/${engine.id}/path/?type=database`

            // 收集该引擎树所有节点的 locator（用于模拟深度展开）
            const tree = store.engineTrees[engine.id]
            const collect = (node, acc, depth = 0) => {
              if (!node) return
              if (node.locator) acc.push({ locator: node.locator, depth })
              if (node.children && node.children.length > 0) {
                for (const c of node.children) collect(c, acc, depth + 1)
              }
            }

            const allNodes = []
            collect(tree, allNodes)

            // 人为设置：将 engine 本身和其深度>=2 的节点设为展开
            const toExpand = new Set([engineLocator])
            for (const n of allNodes) {
              if (n.depth >= 2) toExpand.add(n.locator)
            }

            store.expandedLocators = new Set([...store.expandedLocators, ...toExpand])

            // 调用 collapseNode 折叠引擎节点
            store.collapseNode(engineLocator)

            // 检查是否仍有以 engineLocator 为前缀的展开键残留
            const leftover = Array.from(store.expandedLocators).filter(k => typeof k === 'string' && k.startsWith(engineLocator))

            results.push({ engineId: engine.id, engineName: engine.name, leftOverCount: leftover.length, leftover })
          }

          // 输出并返回结果
          console.table(results.map(r => ({ engineId: r.engineId, engineName: r.engineName, leftOverCount: r.leftOverCount })))
          return results
        }
      } catch (e) {
        // ignore
      }
    }
  } catch (error) {
    ElMessage.error(t('manager.explorer.initFailed', { error: error.message }))
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
      ElMessage.warning(t('manager.explorer.engineNotFound', { engineId }))
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
