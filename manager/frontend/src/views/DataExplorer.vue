<template>
  <div class="data-explorer">
    <div class="split-container" :style="{ gridTemplateColumns: treeWidth + 'px 8px 1fr' }">
      <!-- 左侧资源树 -->
      <ResourceTree
        :resources="resources"
        :tree-data="treeData"
        :loading="loadingTree"
        :loading-resources="loadingResources"
        @refresh="loadTree"
        @node-click="handleNodeClick"
      />

      <!-- 可拖拽分隔器 -->
      <Splitter direction="horizontal" @resize="startTreeResize" />

      <!-- 右侧预览面板 -->
      <PreviewPanel
        :selected-node="selectedNode"
        :preview-data="previewData"
        :loading="loadingPreview"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import ResourceTree from '@/components/explorer/ResourceTree.vue'
import PreviewPanel from '@/components/explorer/PreviewPanel.vue'
import Splitter from '@/components/explorer/Splitter.vue'
import { useResizable } from '@/composables/useResizable'
import dataExplorerAPI from '@/api/dataExplorer'
import { transformResource, makeNodeId } from '@/utils/treeTransform'

// 树形面板宽度
const { size: treeWidth, startResize: startTreeResize } = useResizable(320, 220, 600, 'horizontal')

// 数据状态
const resources = ref([])
const loadingResources = ref(false)
const useLegacyEndpoint = ref(false)
const treeData = ref([])
const selectedNode = ref(null)
const previewData = ref(null)
const loadingTree = ref(false)
const loadingPreview = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
let previewRequestId = 0

const normalizeResourceList = (list = []) =>
  list.map((item) => ({
    ...item,
    resource_type: item.resource_type || item.resourceType,
    resourceType: item.resource_type || item.resourceType
  }))

const resolveResourceName = (id) => {
  const target = resources.value.find((item) => item.id === id)
  return target?.name ?? id
}

const resetSelection = () => {
  selectedNode.value = null
  previewData.value = null
  currentPage.value = 1
}

/**
 * 加载存储引擎列表
 */
const loadResources = async () => {
  loadingResources.value = true
  try {
    const response = await dataExplorerAPI.getResources()
    const list = normalizeResourceList(response.data?.data || [])

    useLegacyEndpoint.value = false
    resources.value = list

    if (list.length > 0) {
      await loadTree()
    } else {
      treeData.value = []
      resetSelection()
    }
  } catch (error) {
    if (error.response?.status === 404) {
      console.warn('[DataExplorer] 新接口不可用，回退旧版资源树', error)
      await loadLegacyTree({
        finalizeResourceLoading: true
      })
      return
    }

    console.error('[DataExplorer] 获取存储引擎列表失败', error)
    ElMessage.error('加载存储引擎失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loadingResources.value = false
  }
}

/**
 * 加载资源树（新接口）
 */
const loadModernTrees = async (resourceIds) => {
  const targetIds =
    Array.isArray(resourceIds) && resourceIds.length
      ? resourceIds
      : resources.value.map((item) => item.id)

  if (!targetIds.length) {
    treeData.value = []
    resetSelection()
    return
  }

  console.log('[DataExplorer] 刷新资源树...', { resourceIds: targetIds })
  loadingTree.value = true
  try {
    const results = await Promise.allSettled(
      targetIds.map((id) =>
        dataExplorerAPI.getTree(id).then((response) => response.data?.data ?? null)
      )
    )

    const treeNodes = []
    const failed = []

    results.forEach((result, index) => {
      if (result.status === 'fulfilled') {
        const resource = result.value
        if (resource && resource.id) {
          treeNodes.push(transformResource(resource))
        } else {
          failed.push(targetIds[index])
        }
      } else {
        failed.push(targetIds[index])
      }
    })

    treeData.value = treeNodes

    if (failed.length && treeNodes.length) {
      const names = failed.map((id) => resolveResourceName(id)).join(', ')
      ElMessage.warning(`部分存储引擎加载失败: ${names}`)
    } else if (failed.length && !treeNodes.length) {
      throw new Error('所有存储引擎加载失败')
    }

    resetSelection()
  } catch (error) {
    console.error('[DataExplorer] 加载资源树失败', error)
    ElMessage.error('加载资源树失败: ' + (error.response?.data?.error || error.message))
    treeData.value = []
    resetSelection()
  } finally {
    loadingTree.value = false
  }
}

/**
 * 使用旧版接口加载资源树（含资源列表）
 */
const loadLegacyTree = async (
  { finalizeResourceLoading = false, silent = false } = {}
) => {
  useLegacyEndpoint.value = true
  loadingTree.value = true
  try {
    const response = await dataExplorerAPI.getLegacyTree()
    const legacyResources = normalizeResourceList(response.data?.data || [])

    resources.value = legacyResources

    treeData.value = legacyResources.map((item) => transformResource(item))
    resetSelection()
  } catch (error) {
    console.error('[DataExplorer] 使用旧版资源树失败', error)
    if (!silent) {
      ElMessage.error('加载资源树失败: ' + (error.response?.data?.error || error.message))
    }
    treeData.value = []
  } finally {
    loadingTree.value = false
    if (finalizeResourceLoading) {
      loadingResources.value = false
    }
  }
}

/**
 * 根据当前模式加载资源树
 */
const loadTree = async (options = {}) => {
  if (useLegacyEndpoint.value) {
    await loadLegacyTree(options)
  } else {
    await loadModernTrees(options.resourceIds)
  }
}

/**
 * 加载数据预览
 */
const loadPreview = async () => {
  if (!selectedNode.value) return

  const requestId = ++previewRequestId
  loadingPreview.value = true
  try {
    const params = {
      resource_id: selectedNode.value.resourceId,
      schema: selectedNode.value.schema,
      table: selectedNode.value.path ?? selectedNode.value.table ?? '',
      page: currentPage.value,
      page_size: pageSize.value
    }

    const response = await dataExplorerAPI.getPreview(params)
    if (requestId !== previewRequestId) {
      return
    }

    previewData.value = response.data

    // 为表格模式添加额外的元数据
    if (response.data.mode === 'table') {
      previewData.value.resourceId = selectedNode.value.resourceId
      previewData.value.schema = selectedNode.value.schema
      previewData.value.table = selectedNode.value.table
    }
  } catch (error) {
    if (requestId !== previewRequestId) {
      return
    }
    ElMessage.error('加载数据预览失败: ' + (error.response?.data?.error || error.message))
    previewData.value = null
  } finally {
    if (requestId === previewRequestId) {
      loadingPreview.value = false
    }
  }
}

/**
 * 处理树节点点击
 */
const handleNodeClick = (node) => {
  selectedNode.value = node
  currentPage.value = 1
  loadPreview()
}

/**
 * 处理分页变化
 */
const handlePageChange = (page) => {
  currentPage.value = page
  loadPreview()
}

/**
 * 处理对象存储目录导航
 */
const handleNavigate = (child) => {
  if (!child || !selectedNode.value) return

  const nodeType = child.type === 'prefix' ? 'directory' : (child.type || '').toLowerCase()
  const schema = selectedNode.value.schema
  const resourceId = selectedNode.value.resourceId
  const resourceType = selectedNode.value.resourceType

  if (!schema || !resourceId) return

  const path = child.path || child.name || ''

  // 创建新的节点
  selectedNode.value = {
    id: makeNodeId(nodeType, resourceId, schema, path || Math.random()),
    type: nodeType,
    nodeType,
    label: child.name,
    resourceId,
    resourceType,
    schema,
    table: path,
    path
  }

  currentPage.value = 1
  loadPreview()
}

onMounted(() => {
  loadResources()
})
</script>

<style scoped>
.data-explorer {
  padding: 10px;
}

.split-container {
  display: grid;
  grid-template-columns: 320px 8px 1fr;
  min-height: 620px;
  align-items: stretch;
  width: 100%;
}
</style>
