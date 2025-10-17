<template>
  <el-card shadow="never" class="resource-tree">
    <template #header>
      <div class="tree-header">
        <div class="header-info">
          <span class="header-title">存储引擎</span>
          <span class="resource-count" v-if="resources.length">
            共 {{ resources.length }} 个
          </span>
        </div>
        <div class="header-actions">
          <el-button
            size="small"
            :loading="loading || loadingResources"
            :disabled="loadingResources"
            @click="emitRefresh"
          >
            <el-icon><Refresh /></el-icon>
          </el-button>
        </div>
      </div>
    </template>

    <div class="tree-container" v-loading="loading">
      <el-empty
        v-if="!resources.length && !loadingResources"
        description="暂无存储引擎"
        class="empty-placeholder"
      />
      <el-empty
        v-else-if="!loading && !hasTreeData"
        description="无可用元数据，请先执行扫描"
        class="empty-placeholder"
      />
      <el-tree
        ref="treeRef"
        v-else
        :key="treeKey"
        :data="treeData"
        :props="treeProps"
        node-key="id"
        :highlight-current="true"
        :expand-on-click-node="false"
        :default-expanded-keys="appliedExpandedKeys"
        @node-click="handleNodeClick"
        @dblclick="handleTreeDblclick"
        @node-expand="handleNodeExpand"
        @node-collapse="handleNodeCollapse"
      >
        <template #default="{ data, node }">
          <span class="tree-node-wrapper">
            <span class="tree-node" :class="data.type">
              <span class="node-main">
                <el-icon v-if="data.type === 'resource'"><Collection /></el-icon>
                <el-icon v-else-if="['schema', 'bucket', 'directory'].includes(data.type)"><Folder /></el-icon>
                <el-icon v-else><Document /></el-icon>
                <span class="label" :title="data.label">{{ data.label }}</span>
              </span>
              <span
                v-if="shouldShowNodeRefresh(data)"
                class="node-action"
                :class="{ disabled: isNodeRefreshDisabled(data) }"
                @click.stop="handleNodeRefresh(data)"
              >
                <el-icon v-if="isNodeRefreshing(data)"><Loading /></el-icon>
                <el-icon v-else><Refresh /></el-icon>
              </span>
            </span>
          </span>
        </template>
      </el-tree>
    </div>
  </el-card>
</template>

<script setup>
import { computed, ref, watch, onMounted, nextTick } from 'vue'
import { Refresh, Folder, Collection, Document, Loading } from '@element-plus/icons-vue'

const props = defineProps({
  resources: {
    type: Array,
    default: () => []
  },
  treeData: {
    type: Array,
    default: () => []
  },
  loading: {
    type: Boolean,
    default: false
  },
  loadingResources: {
    type: Boolean,
    default: false
  },
  refreshingNodeIds: {
    type: Array,
    default: () => []
  },
  expandedKeys: {
    type: Array,
    default: () => []
  },
  currentNodeKey: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['refresh', 'node-click', 'refresh-node', 'node-expand', 'node-collapse'])

const treeRef = ref(null)

const hasTreeData = computed(() => {
  if (!Array.isArray(props.treeData)) return false
  return props.treeData.some((node) => Array.isArray(node?.children) && node.children.length > 0)
})

const treeProps = {
  label: 'label',
  children: 'children'
}

const appliedExpandedKeys = computed(() => {
  if (Array.isArray(props.expandedKeys) && props.expandedKeys.length) {
    return props.expandedKeys
  }
  return props.treeData.map((item) => item.id)
})
const treeKey = computed(() => appliedExpandedKeys.value.join('|') || 'resource-tree')

const updateTreeState = () => {
  const tree = treeRef.value
  if (!tree) return

  if (typeof tree.setExpandedKeys === 'function') {
    tree.setExpandedKeys(appliedExpandedKeys.value)
  }

  if (typeof tree.setCurrentKey === 'function') {
    if (props.currentNodeKey) {
      tree.setCurrentKey(props.currentNodeKey)
    } else {
      tree.setCurrentKey(undefined)
    }
  }
}

onMounted(() => {
  nextTick(updateTreeState)
})

watch(
  () => props.expandedKeys,
  () => {
    nextTick(updateTreeState)
  },
  { deep: true }
)

watch(
  () => props.treeData,
  () => {
    nextTick(updateTreeState)
  },
  { deep: true }
)

watch(
  () => props.currentNodeKey,
  () => {
    nextTick(() => {
      const tree = treeRef.value
      if (tree && typeof tree.setCurrentKey === 'function') {
        if (props.currentNodeKey) {
          tree.setCurrentKey(props.currentNodeKey)
        } else {
          tree.setCurrentKey(undefined)
        }
      }
    })
  }
)

const handleNodeClick = (nodeData) => {
  if (!nodeData || nodeData.type === 'resource') return
  emit('node-click', nodeData)
}

const isFolderNode = (data) => {
  if (!data) return false
  if (['resource', 'schema', 'bucket', 'directory'].includes(data.type)) return true
  return Array.isArray(data.children) && data.children.length > 0
}

const toggleTreeNodeExpand = (treeNode, data) => {
  if (!treeNode || !isFolderNode(data)) return

  const tree = treeRef.value
  if (tree && typeof tree.toggleExpand === 'function') {
    tree.toggleExpand(treeNode, !treeNode.expanded)
    return
  }

  if (tree && tree.store && typeof tree.store.toggleExpand === 'function') {
    tree.store.toggleExpand(treeNode)
    return
  }

  treeNode.expanded = !treeNode.expanded
}

const handleTreeDblclick = (event) => {
  event.preventDefault()

  const selection = window.getSelection?.()
  if (selection && selection.removeAllRanges) {
    selection.removeAllRanges()
  }

  const tree = treeRef.value
  if (!tree) return

  const contentEl = event.target?.closest('.el-tree-node__content')
  if (!contentEl) return

  const nodeEl = contentEl.closest('.el-tree-node')
  const nodeKey = nodeEl?.dataset?.key
  if (!nodeKey) return

  const treeNode = tree.getNode ? tree.getNode(nodeKey) : null
  if (!treeNode) return

  toggleTreeNodeExpand(treeNode, treeNode.data)
}

const refreshSet = computed(() => {
  const ids = Array.isArray(props.refreshingNodeIds) ? props.refreshingNodeIds : []
  return new Set(ids)
})

const shouldShowNodeRefresh = (data) => {
  if (!data) return false
  return data.type !== 'resource'
}

const isNodeRefreshing = (data) => refreshSet.value.has(data?.id)

const isNodeRefreshDisabled = (data) => {
  if (!data) return true
  return props.loading || props.loadingResources || isNodeRefreshing(data)
}

const handleNodeExpand = (data) => {
  if (!data) return
  emit('node-expand', data)
}

const handleNodeCollapse = (data) => {
  if (!data) return
  emit('node-collapse', data)
}

const handleNodeRefresh = (data) => {
  if (!data || !shouldShowNodeRefresh(data) || isNodeRefreshDisabled(data)) return
  emit('refresh-node', data)
}

const emitRefresh = () => {
  emit('refresh')
}
</script>

<style scoped>
.resource-tree {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.resource-tree :deep(.el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
  padding: 0;
}

.tree-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.header-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.header-title {
  font-weight: 500;
}

.resource-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tree-container {
  flex: 1;
  overflow: auto;
  padding: 0 16px 16px;
}

.tree-node-wrapper {
  display: flex;
  align-items: center;
  width: 100%;
}

.empty-placeholder {
  margin-top: 60px;
}

.tree-node {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  width: 100%;
}

.node-main {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
}

.tree-node .label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-action {
  display: flex;
  align-items: center;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  opacity: 0.75;
  transition: color 0.2s, opacity 0.2s;
}

.node-action:hover {
  color: var(--el-color-primary);
  opacity: 1;
}

.tree-node-wrapper:hover .node-action {
  opacity: 1;
}

.node-action.disabled {
  cursor: not-allowed;
  opacity: 0.35;
  pointer-events: none;
}
</style>
