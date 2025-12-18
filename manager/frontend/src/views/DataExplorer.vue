<template>
  <div class="data-explorer">
    <div class="split-container" :style="{ gridTemplateColumns: treeWidth + 'px 8px 1fr' }">
      <!-- 左侧资源树 -->
      <ResourceTree
        :resources="resources"
        :tree-data="treeData"
        :loading="loadingTree"
        :loading-resources="loadingResources"
        :refreshing-node-ids="refreshingNodeIds"
        :expanded-keys="expandedNodeIds"
        :current-node-key="selectedNode?.id || ''"
        @refresh="loadTree"
        @node-click="handleNodeClick"
        @refresh-node="handleNodeRefresh"
        @node-expand="handleNodeExpandEvent"
        @node-collapse="handleNodeCollapseEvent"
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
import { ref, onMounted, watch, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import ResourceTree from '@/components/explorer/ResourceTree.vue'
import PreviewPanel from '@/components/explorer/PreviewPanel.vue'
import Splitter from '@/components/explorer/Splitter.vue'
import { useResizable } from '@/composables/useResizable'
import dataExplorerAPI from '@/api/dataExplorer'
import { transformResource, makeNodeId } from '@/utils/treeTransform'

// 树形面板宽度
const { size: treeWidth, startResize: startTreeResize } = useResizable(320, 220, 600, 'horizontal')

const route = useRoute()

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
const refreshingNodeIds = ref([])
const expandedNodeIds = ref([])
let previewRequestId = 0
const pendingRouteSelection = ref(null)

const buildSnapshotFromQuery = (query = {}) => {
  const resourceKey = query.resourceId ?? query.resource_id ?? query.resource
  const schema = query.schema ?? query.bucket ?? ''
  const objectPath = query.objectPath ?? query.path ?? query.table ?? ''
  if (!resourceKey) return null

  // Try to parse as numeric ID first
  const numericId = Number(resourceKey)
  if (!Number.isNaN(numericId) && numericId > 0) {
    const nodeType = query.nodeType || (objectPath ? 'object' : '')
    return {
      resourceId: numericId,
      schema,
      path: objectPath,
      table: objectPath,
      type: nodeType
    }
  }

  // If not numeric, treat as resource name/identifier
  // Find matching resource by name or unique_identifier
  const matchingResource = resources.value.find(r =>
    r.name === resourceKey ||
    r.unique_identifier === resourceKey ||
    r.unique_identifier?.endsWith(`.${resourceKey}`)
  )

  if (!matchingResource) {
    console.warn(`Resource not found: ${resourceKey}`)
    return null
  }

  const nodeType = query.nodeType || (objectPath ? 'object' : '')
  return {
    resourceId: matchingResource.id,
    schema,
    path: objectPath,
    table: objectPath,
    type: nodeType
  }
}

const attemptRouteSelection = async () => {
  if (!pendingRouteSelection.value) return
  await nextTick()
  const snapshot = pendingRouteSelection.value
  const restored = restoreSelectionFromSnapshot(snapshot)
  if (restored) {
    pendingRouteSelection.value = null
    currentPage.value = 1
    await loadPreview()
  }
}

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

const snapshotNode = (node) => {
  if (!node) return null
  return {
    id: node.id,
    type: node.type,
    resourceId: node.resourceId,
    schema: node.schema || '',
    path: node.path || '',
    table: node.table || ''
  }
}

const extractParentPath = (path = '') => {
  if (!path) return ''
  const segments = path.split('/').filter(Boolean)
  if (segments.length <= 1) return ''
  segments.pop()
  return segments.join('/')
}

const nodesMatch = (candidate, snapshot) => {
  if (!candidate || !snapshot) return false
  if (snapshot.id && candidate.id === snapshot.id) {
    return true
  }
  const normalize = (value) => (value ?? '').toString()
  return (
    Number(candidate.resourceId) === Number(snapshot.resourceId) &&
    normalize(candidate.type) === normalize(snapshot.type) &&
    normalize(candidate.schema) === normalize(snapshot.schema) &&
    normalize(candidate.path) === normalize(snapshot.path) &&
    normalize(candidate.table) === normalize(snapshot.table)
  )
}

const findNodeInTree = (nodes, snapshot) => {
  if (!Array.isArray(nodes) || !snapshot) return null
  for (const node of nodes) {
    if (nodesMatch(node, snapshot)) {
      return node
    }
    if (Array.isArray(node.children) && node.children.length) {
      const found = findNodeInTree(node.children, snapshot)
      if (found) return found
    }
  }
  return null
}

const restoreSelectionFromSnapshot = (snapshot) => {
  if (!snapshot) return false
  const found = findNodeInTree(treeData.value, snapshot)
  if (found) {
    selectedNode.value = found
    ensureNodeExpanded(found.id)
    ensureNodePathExpanded(found.id)
    return true
  }
  return false
}

const normalizePreviewPayload = (data, node) => {
  if (!data || !node) return data
  const normalized = { ...data }
  const resourceId = node.resourceId ?? node.resource_id ?? null

  if (resourceId != null) {
    if (normalized.resourceId == null) normalized.resourceId = resourceId
    if (normalized.resource_id == null) normalized.resource_id = resourceId
  }

  if (!normalized.schema && node.schema) {
    normalized.schema = node.schema
  }
  if (!normalized.table) {
    normalized.table = node.table ?? node.path ?? ''
  }

  if (normalized.object && typeof normalized.object === 'object') {
    const object = { ...normalized.object }
    if (resourceId != null) {
      if (object.resource_id == null) object.resource_id = resourceId
      if (object.resourceId == null) object.resourceId = resourceId
    }
    if (!object.bucket && node.schema) {
      object.bucket = node.schema
    }
    const rawPath = object.path ?? node.path ?? node.table ?? ''
    if (!object.path && rawPath) {
      object.path = rawPath
    }
    if (!object.object_key) {
      const bucket = object.bucket ?? node.schema ?? ''
      const cleaned = String(rawPath || '').replace(/^\/+/, '')
      if (bucket && cleaned) {
        object.object_key = `${bucket}/${cleaned}`
      }
    }
    normalized.object = object
  }

  return normalized
}

const addRefreshingNode = (id) => {
  if (!id) return
  if (!refreshingNodeIds.value.includes(id)) {
    refreshingNodeIds.value = [...refreshingNodeIds.value, id]
  }
}

const removeRefreshingNode = (id) => {
  if (!id) {
    refreshingNodeIds.value = []
    return
  }
  refreshingNodeIds.value = refreshingNodeIds.value.filter((item) => item !== id)
}

const collectAllNodeIds = (nodes, set) => {
  if (!Array.isArray(nodes)) return
  for (const node of nodes) {
    if (!node || !node.id) continue
    set.add(node.id)
    if (Array.isArray(node.children) && node.children.length) {
      collectAllNodeIds(node.children, set)
    }
  }
}

const syncExpandedKeysWithTree = () => {
  const available = new Set()
  collectAllNodeIds(treeData.value, available)
  let nextKeys = expandedNodeIds.value.filter((id) => available.has(id))
  if (!nextKeys.length && treeData.value.length) {
    nextKeys = treeData.value.map((node) => node.id).filter(Boolean)
  }
  const normalized = [...new Set(nextKeys)]
  const current = expandedNodeIds.value
  const unchanged =
    normalized.length === current.length && normalized.every((id, index) => id === current[index])
  if (!unchanged) {
    expandedNodeIds.value = normalized
  }
}

const findNodePathById = (nodes, targetId) => {
  if (!Array.isArray(nodes)) return null
  for (const node of nodes) {
    if (!node || !node.id) continue
    if (node.id === targetId) {
      return [node.id]
    }
    if (Array.isArray(node.children) && node.children.length) {
      const childPath = findNodePathById(node.children, targetId)
      if (childPath) {
        return [node.id, ...childPath]
      }
    }
  }
  return null
}

const ensureNodePathExpanded = (nodeId) => {
  if (!nodeId) return
  const path = findNodePathById(treeData.value, nodeId)
  if (!path || path.length <= 1) return
  const ancestors = path.slice(0, -1)
  const nextSet = new Set(expandedNodeIds.value)
  ancestors.forEach((id) => nextSet.add(id))
  expandedNodeIds.value = Array.from(nextSet)
}

const ensureNodeExpanded = (nodeId) => {
  if (!nodeId) return
  const nextSet = new Set(expandedNodeIds.value)
  const sizeBefore = nextSet.size
  nextSet.add(nodeId)
  if (nextSet.size !== sizeBefore) {
    expandedNodeIds.value = Array.from(nextSet)
  }
}

const handleNodeExpandEvent = (node) => {
  if (!node?.id) return
  const nextSet = new Set(expandedNodeIds.value)
  const sizeBefore = nextSet.size
  nextSet.add(node.id)
  if (nextSet.size !== sizeBefore) {
    expandedNodeIds.value = Array.from(nextSet)
  }
}

const handleNodeCollapseEvent = (node) => {
  if (!node?.id) return
  const nextSet = expandedNodeIds.value.filter((id) => id !== node.id)
  if (nextSet.length !== expandedNodeIds.value.length) {
    expandedNodeIds.value = nextSet
  }
}

watch(
  treeData,
  () => {
    nextTick(async () => {
      syncExpandedKeysWithTree()
      if (selectedNode.value?.id) {
        ensureNodeExpanded(selectedNode.value.id)
        ensureNodePathExpanded(selectedNode.value.id)
      }
      await attemptRouteSelection()
    })
  },
  { deep: true }
)

watch(
  () => route.fullPath,
  () => {
    const snapshot = buildSnapshotFromQuery(route.query || {})
    if (snapshot) {
      pendingRouteSelection.value = snapshot
      attemptRouteSelection()
    } else {
      pendingRouteSelection.value = null
    }
  },
  { immediate: true }
)

/**
 * 加载存储引擎列表
 */
const loadResources = async () => {
  loadingResources.value = true
  try {
    const response = await dataExplorerAPI.getResources()
    const list = normalizeResourceList(response || [])

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
    await attemptRouteSelection()
    return
  }

  console.log('[DataExplorer] 刷新资源树...', { resourceIds: targetIds })
  loadingTree.value = true
  try {
    const results = await Promise.allSettled(
      targetIds.map((id) =>
        dataExplorerAPI.getTree(id).then((response) => response.data ?? null)
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
    syncExpandedKeysWithTree()

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
    expandedNodeIds.value = []
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
    const legacyResources = normalizeResourceList(response || [])

    resources.value = legacyResources

    treeData.value = legacyResources.map((item) => transformResource(item))
    syncExpandedKeysWithTree()
    resetSelection()
    await attemptRouteSelection()
  } catch (error) {
    console.error('[DataExplorer] 使用旧版资源树失败', error)
    if (!silent) {
      ElMessage.error('加载资源树失败: ' + (error.response?.data?.error || error.message))
    }
    treeData.value = []
    expandedNodeIds.value = []
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

    previewData.value = normalizePreviewPayload(response, selectedNode.value)
  } catch (error) {
    // 忽略请求取消错误（用户快速点击时的正常行为）
    if (error.name === 'CanceledError' || error.code === 'ERR_CANCELED') {
      return
    }

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
  ensureNodeExpanded(selectedNode.value.id)
  ensureNodePathExpanded(selectedNode.value.id)
  loadPreview()
}

const handleNodeRefresh = async (node) => {
  if (!node || !node.resourceId) return

  if (refreshingNodeIds.value.includes(node.id)) return

  ensureNodeExpanded(node.id)
  ensureNodePathExpanded(node.id)
  const shouldRestoreSelection = selectedNode.value?.id === node.id
  const selectionSnapshot = shouldRestoreSelection ? snapshotNode(selectedNode.value) : null
  const previousPage = shouldRestoreSelection ? currentPage.value : 1

  addRefreshingNode(node.id)
  try {
    const nodeType = (node.type || '').toLowerCase()
    let refreshPath = node.path || ''
    let refreshFullPath = node.fullName || node.full_name || ''
    let refreshTable = node.table || ''

    if (nodeType === 'object') {
      const parentPath = node.parentPath || extractParentPath(node.path || '')
      refreshPath = parentPath
      refreshFullPath = parentPath
      refreshTable = parentPath
    }

    const payload = {
      node_type: node.type,
      schema: node.schema,
      path: refreshPath,
      table: refreshTable,
      full_path: refreshFullPath,
      full_name: refreshFullPath,
      resource_type: node.resourceType
    }

    await dataExplorerAPI.refreshNode(node.resourceId, payload)
    ElMessage.success('刷新完成')

    await loadTree()

    if (shouldRestoreSelection && selectionSnapshot) {
      const restored = restoreSelectionFromSnapshot(selectionSnapshot)
      if (restored) {
        currentPage.value = previousPage
        await loadPreview()
      }
    }
  } catch (error) {
    console.error('[DataExplorer] 节点刷新失败', error)
    ElMessage.error('刷新失败: ' + (error.response?.data?.error || error.message))
    if (shouldRestoreSelection && selectionSnapshot) {
      restoreSelectionFromSnapshot(selectionSnapshot)
      currentPage.value = previousPage
    }
  } finally {
    removeRefreshingNode(node.id)
  }
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
