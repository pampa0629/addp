<template>
  <div class="data-explorer">
    <div class="split-container" :style="{ gridTemplateColumns: treeWidth + 'px 8px 1fr' }">
      <!-- 左侧资源树 -->
      <ResourceTree
        :tree-data="treeData"
        :loading="loadingTree"
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
const treeData = ref([])
const selectedNode = ref(null)
const previewData = ref(null)
const loadingTree = ref(false)
const loadingPreview = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)

/**
 * 加载资源树
 */
const loadTree = async () => {
  loadingTree.value = true
  try {
    const response = await dataExplorerAPI.getTree()
    const resources = response.data?.data || []
    treeData.value = resources.map((res) => transformResource(res))

    // 重置预览状态
    selectedNode.value = null
    previewData.value = null
  } catch (error) {
    ElMessage.error('加载资源树失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loadingTree.value = false
  }
}

/**
 * 加载数据预览
 */
const loadPreview = async () => {
  if (!selectedNode.value) return

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
    previewData.value = response.data

    // 为表格模式添加额外的元数据
    if (response.data.mode === 'table') {
      previewData.value.resourceId = selectedNode.value.resourceId
      previewData.value.schema = selectedNode.value.schema
      previewData.value.table = selectedNode.value.table
    }
  } catch (error) {
    ElMessage.error('加载数据预览失败: ' + (error.response?.data?.error || error.message))
    previewData.value = null
  } finally {
    loadingPreview.value = false
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
  loadTree()
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
