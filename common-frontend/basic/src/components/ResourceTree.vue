<template>
  <el-card :shadow="cardShadow" class="resource-tree" :style="{ height }">
    <template #header>
      <slot name="header" :title="title" :count="rootCount" :loading="loading">
        <div class="tree-header">
          <div class="header-info">
            <span class="header-title">{{ title }}</span>
            <span v-if="showCount && rootCount > 0" class="resource-count">
              {{ countText(rootCount) }}
            </span>
          </div>
          <div class="header-actions">
            <slot name="header-actions" :loading="loading" :on-refresh="emitRefresh">
              <el-button
                v-if="showRefreshButton"
                size="small"
                :loading="loading"
                :disabled="loading"
                @click="emitRefresh"
              >
                <el-icon><Refresh /></el-icon>
              </el-button>
            </slot>
          </div>
        </div>
      </slot>
    </template>

    <div class="tree-container" v-loading="loading">
      <!-- 搜索框 -->
      <div v-if="filterText !== undefined" class="tree-search">
        <el-input
          :model-value="filterText"
          @update:model-value="handleFilterChange"
          :placeholder="filterPlaceholder"
          :prefix-icon="Search"
          clearable
          size="small"
        />
      </div>

      <!-- 空状态 -->
      <slot v-if="!hasTreeData && !loading" name="empty" :empty-text="emptyText">
        <el-empty :description="emptyText" class="empty-placeholder" />
      </slot>

      <slot v-else-if="showFilterEmpty" name="filter-empty" :empty-text="filterEmptyText">
        <el-empty :description="filterEmptyText" class="empty-placeholder" />
      </slot>

      <!-- 树形结构 -->
      <el-tree
        v-else
        ref="treeRef"
        :data="filteredTreeData"
        :props="treeProps"
        node-key="id"
        :highlight-current="true"
        :expand-on-click-node="expandOnClickNode"
        :default-expanded-keys="computedExpandedKeys"
        :current-node-key="currentNodeKey"
        :filter-node-method="currentFilterMethod"
        :lazy="lazy"
        :load="load"
        :node-class-name="resolveNodeClassName"
        @node-click="handleNodeClick"
        @node-expand="handleNodeExpand"
        @node-collapse="handleNodeCollapse"
        @current-change="handleCurrentChange"
      >
        <template #default="{ data, node }">
          <slot name="node" :node="node" :data="data">
            <span
              class="tree-node-wrapper"
              :class="{ 'resource-tree-unloaded-children-inline': isUnloadedExpandable(data) }"
              @dblclick="enableDblclickToggle ? handleNodeDblclick($event, node, data) : null"
            >
              <span class="tree-node" :class="[data.type, { highlight: data.highlight }]">
                <span class="node-main">
                  <slot name="node-icon" :node="node" :data="data">
                    <el-icon v-if="data.type === 'engine'">
                      <component :is="resolveEngineIcon(data)" />
                    </el-icon>
                    <!-- 其他节点类型图标 -->
                    <el-icon v-else-if="data.type === 'schema' || data.type === 'database'">
                      <OfficeBuilding />
                    </el-icon>
                    <el-icon v-else-if="data.type === 'bucket'">
                      <Box />
                    </el-icon>
                    <el-icon v-else-if="data.type === 'directory'">
                      <FolderOpened />
                    </el-icon>
                    <el-icon v-else-if="data.type === 'root' || data.type === 'server' || data.type === 'service'">
                      <FolderOpened />
                    </el-icon>
                    <el-icon v-else-if="data.type === 'prefix'">
                      <Folder />
                    </el-icon>
                    <el-icon v-else-if="data.type === 'table'">
                      <Grid />
                    </el-icon>
                    <el-icon v-else-if="data.type === 'label'">
                      <Collection />
                    </el-icon>
                    <el-icon v-else-if="data.type === 'relationship'">
                      <Connection />
                    </el-icon>
                    <el-icon v-else-if="data.type === 'object'">
                      <Document />
                    </el-icon>
                    <el-icon v-else-if="data.type === 'task'">
                      <Files />
                    </el-icon>
                    <el-icon v-else-if="data.type === 'folder' || data.type === 'dir'">
                      <Folder />
                    </el-icon>
                    <el-icon v-else-if="data.type === 'file'">
                      <Document />
                    </el-icon>
                    <!-- 默认图标 -->
                    <el-icon v-else>
                      <Document />
                    </el-icon>
                  </slot>
                  <slot name="node-label" :node="node" :data="data">
                    <span class="label" :title="data.label">
                      {{ data.label }}
                    </span>
                  </slot>
                </span>
                <span v-if="visibleNodeActions(data).length" class="node-actions">
                  <slot name="node-actions" :node="node" :data="data" :actions="visibleNodeActions(data)">
                    <span
                      v-for="action in visibleNodeActions(data)"
                      :key="action.name"
                      class="node-action"
                      :class="{
                        disabled: isActionDisabled(action, data),
                        loading: isActionLoading(action, data)
                      }"
                      :title="action.tooltip"
                      @click.stop="handleNodeAction(action, data)"
                    >
                      <el-icon v-if="isActionLoading(action, data)">
                        <Loading />
                      </el-icon>
                      <el-icon v-else :style="{ color: action.color }">
                        <component :is="actionIconMap[action.icon]" />
                      </el-icon>
                    </span>
                  </slot>
                </span>
              </span>
            </span>
          </slot>
        </template>
      </el-tree>
    </div>
  </el-card>
</template>

<script setup>
import { computed, ref, watch, onMounted, nextTick } from 'vue'
import {
  Refresh,
  Loading,
  Search,
  Folder,
  FolderOpened,
  Collection,
  Connection,
  Document,
  Files,
  Grid,
  Coin,
  DataBoard,
  Box,
  MagicStick,
  Select,
  OfficeBuilding,
  Shop,
  Share,
  Upload
} from '@element-plus/icons-vue'
import { getAllNodeKeys, traverseTree, findNodePath } from '../types/tree'
import { getEngineIconName } from '../utils/engineDisplay.js'

// 动作图标映射（将字符串名称映射到图标组件）
const actionIconMap = {
  Refresh,
  Loading,
  Search,
  Files,
  MagicStick,
  Document,
  Folder,
  Grid,
  DataBoard,
  Coin,
  Box,
  OfficeBuilding,
  Select,
  Shop,
  Share,
  Upload
}

const resolveEngineIcon = (data) => {
  return actionIconMap[data.icon] || actionIconMap[getEngineIconName({
    engine_type: data.engineType || data.metadata?.engine_type,
    capabilities_view: data.capabilitiesView || data.capabilities_view || data.metadata?.capabilities_view,
    engine_family: data.engineFamily || data.metadata?.engine_family
  })] || Coin
}

const props = defineProps({
  // 数据源
  treeData: {
    type: Array,
    default: () => [],
    required: true
  },

  // 标题配置
  title: {
    type: String,
    default: '资源树'
  },
  showCount: {
    type: Boolean,
    default: true
  },
  countText: {
    type: Function,
    default: (count) => `共 ${count} 个`
  },

  // 刷新功能
  showRefreshButton: {
    type: Boolean,
    default: true
  },
  loading: {
    type: Boolean,
    default: false
  },
  refreshingNodeIds: {
    type: Array,
    default: () => []
  },

  // 节点操作
  nodeActions: {
    type: Array,
    default: () => []
  },

  // 展开/折叠状态
  expandedKeys: {
    type: Array,
    default: () => []
  },
  defaultExpandAll: {
    type: Boolean,
    default: false
  },
  defaultExpandRoot: {
    type: Boolean,
    default: true
  },

  // 选中状态
  currentNodeKey: {
    type: [String, Number],
    default: ''
  },

  // 搜索功能
  filterText: {
    type: String,
    default: undefined
  },
  filterPlaceholder: {
    type: String,
    default: '搜索...'
  },
  filterMethod: {
    type: Function,
    default: null
  },
  highlightMatch: {
    type: Boolean,
    default: true
  },

  // 节点渲染
  renderLabel: {
    type: Function,
    default: null
  },
  renderIcon: {
    type: Function,
    default: null
  },

  // 交互行为
  expandOnClickNode: {
    type: Boolean,
    default: false
  },
  enableDblclickToggle: {
    type: Boolean,
    default: true
  },

  // 懒加载
  lazy: {
    type: Boolean,
    default: false
  },
  load: {
    type: Function,
    default: null
  },

  // 空状态
  emptyText: {
    type: String,
    default: '暂无数据'
  },
  filterEmptyText: {
    type: String,
    default: '未找到匹配结果'
  },

  // 样式
  height: {
    type: String,
    default: '100%'
  },
  cardShadow: {
    type: String,
    default: 'never'
  },
  nodeClassName: {
    type: [String, Function],
    default: ''
  }
})

const emit = defineEmits([
  'refresh',
  'node-action',
  'node-click',
  'node-dblclick',
  'node-expand',
  'node-collapse',
  'current-change',
  'update:expanded-keys',
  'update:current-node-key',
  'update:filter-text'
])

const treeRef = ref(null)

// 根节点数量
const rootCount = computed(() => props.treeData?.length || 0)

// 是否有树数据
const hasTreeData = computed(() => {
  if (!Array.isArray(props.treeData)) return false
  return props.treeData.length > 0
})

// 树属性配置
const treeProps = {
  label: 'label',
  children: 'children',
  isLeaf: (data, node) => {
    if (hasExpandableChildren(data)) {
      return false
    }
    if (data?.hasChildren === false || data?.metadata?.has_children === false) {
      return true
    }
    return !data.children || data.children.length === 0
  }
}

const hasExpandableChildren = (data) => {
  if (!data) return false
  if (data.hasChildren === true) return true
  if (data.metadata?.has_children === true) return true
  return Number(data.metadata?.item_count || 0) > 0
}

const isUnloadedExpandable = (data) => {
  return hasExpandableChildren(data) && (!data.children || data.children.length === 0)
}

// 计算展开的节点
const computedExpandedKeys = computed(() => {
  if (props.expandedKeys?.length) {
    return props.expandedKeys
  }
  if (props.defaultExpandAll) {
    return getAllNodeKeys(props.treeData)
  }
  if (props.defaultExpandRoot) {
    return props.treeData.map((node) => node.id)
  }
  return []
})

// 默认过滤方法
const defaultFilterMethod = (node, keyword) => {
  if (!keyword) return true
  const lowerKeyword = keyword.toLowerCase()

  // 搜索 label
  if (node.label?.toLowerCase().includes(lowerKeyword)) {
    return true
  }

  // 搜索 metadata 中的关键字段
  if (node.metadata) {
    const fields = ['name', 'schema', 'table', 'path', 'description']
    for (const field of fields) {
      const value = node.metadata[field]
      if (value && String(value).toLowerCase().includes(lowerKeyword)) {
        return true
      }
    }
  }

  return false
}

// 当前使用的过滤方法
const currentFilterMethod = computed(() => {
  return props.filterMethod || defaultFilterMethod
})

const resolveNodeClassName = (data, node) => {
  const classes = []
  if (typeof props.nodeClassName === 'function') {
    const customClass = props.nodeClassName(data, node)
    if (customClass) classes.push(customClass)
  } else if (props.nodeClassName) {
    classes.push(props.nodeClassName)
  }

  if (isUnloadedExpandable(data)) {
    classes.push('resource-tree-unloaded-children')
  }

  return classes.join(' ')
}

// 过滤后的树数据
const filteredTreeData = computed(() => {
  if (!props.filterText) {
    return props.treeData
  }

  if (!props.highlightMatch) {
    return props.treeData
  }

  // 深拷贝并添加高亮标记
  const filterTree = (nodes) => {
    const filtered = []
    for (const node of nodes) {
      const match = currentFilterMethod.value(node, props.filterText)
      let filteredChildren = []

      if (node.children?.length) {
        filteredChildren = filterTree(node.children)
      }

      if (match || filteredChildren.length) {
        filtered.push({
          ...node,
          highlight: match,
          children: filteredChildren
        })
      }
    }
    return filtered
  }

  return filterTree(props.treeData)
})

const showFilterEmpty = computed(() => {
  return !props.loading && Boolean(props.filterText) && hasTreeData.value && filteredTreeData.value.length === 0
})

// 查找所有匹配节点的路径（优化：复用 tree.js 的工具函数）
const findMatchedPaths = (nodes, keyword) => {
  const matchedPaths = []

  // 1. 使用 traverseTree 遍历所有节点
  traverseTree(nodes, (node) => {
    // 2. 检查是否匹配
    if (currentFilterMethod.value(node, keyword)) {
      // 3. 使用 findNodePath 获取路径
      const path = findNodePath(nodes, node.id)
      matchedPaths.push(...path)
    }
  })

  // 4. 去重
  return [...new Set(matchedPaths)]
}

const expandedKeysBeforeFilter = ref(null)

// 搜索期间展开匹配路径，清空后恢复搜索前的展开状态。
watch(
  [() => props.filterText, () => props.treeData],
  ([keyword], [previousKeyword]) => {
    if (keyword) {
      if (!previousKeyword) {
        expandedKeysBeforeFilter.value = [...computedExpandedKeys.value]
      }
      const matchedPaths = findMatchedPaths(props.treeData, keyword)
      emit('update:expanded-keys', matchedPaths)
      return
    }

    if (previousKeyword) {
      const restoredKeys = expandedKeysBeforeFilter.value || []
      expandedKeysBeforeFilter.value = null
      emit('update:expanded-keys', restoredKeys)
    }
  },
  { deep: true }
)

let appliedExpandedKeys = new Set()

// 同步受控展开状态，不触发选中或滚动。
const updateExpandState = () => {
  const tree = treeRef.value
  if (!tree?.store) return

  const nextExpandedKeys = new Set(computedExpandedKeys.value)
  for (const key of appliedExpandedKeys) {
    if (nextExpandedKeys.has(key)) continue
    const node = tree.store.getNode(key)
    if (node?.expanded) {
      node.expanded = false
    }
  }

  for (const key of nextExpandedKeys) {
    const node = tree.store.getNode(key)
    if (node && !node.expanded) {
      node.expanded = true
    }
  }
  appliedExpandedKeys = nextExpandedKeys
}

// 全量同步：展开状态 + 选中节点（仅在挂载和数据加载后调用）
const updateTreeState = () => {
  updateExpandState()
  const tree = treeRef.value
  if (!tree) return
  if (typeof tree.setCurrentKey === 'function' && props.currentNodeKey) {
    tree.setCurrentKey(props.currentNodeKey)
  }
}

onMounted(() => {
  if (hasTreeData.value) {
    nextTick(() => {
      updateTreeState()
    })
  }
})

// treeData 变化（数据加载）时全量同步，恢复展开 + 选中状态
watch(
  () => props.treeData,
  () => {
    if (hasTreeData.value) {
      nextTick(() => {
        updateTreeState()
      })
    }
  },
  { deep: true }
)

// expandedKeys 变化时只更新展开状态，不触发 setCurrentKey / scrollIntoView，避免跳动
watch(
  () => props.expandedKeys,
  () => {
    if (hasTreeData.value) {
      nextTick(() => {
        updateExpandState()
      })
    }
  },
  { deep: true }
)

const scrollToCurrentNode = (block = 'center', behavior = 'smooth') => {
  const tree = treeRef.value
  const el = tree?.$el?.querySelector('.is-current')
  if (el) {
    el.scrollIntoView({ behavior, block })
    return true
  }
  return false
}

const selectAndScrollToNode = async (key, options = {}) => {
  if (!key) return false
  const tree = treeRef.value
  if (!tree || typeof tree.setCurrentKey !== 'function') return false
  tree.setCurrentKey(key)
  await nextTick()
  return scrollToCurrentNode(options.block || 'center', options.behavior || 'smooth')
}

// 选中节点变化时单独处理：更新高亮，滚动到可见区域
watch(
  () => props.currentNodeKey,
  (key) => {
    nextTick(() => {
      selectAndScrollToNode(key, { block: 'nearest' })
    })
  }
)

// 节点点击
const handleNodeClick = (nodeData) => {
  if (!nodeData) return
  emit('node-click', nodeData)
}

// 当前节点变化
const handleCurrentChange = (data, node) => {
  if (data) {
    emit('update:current-node-key', data.id)
    emit('current-change', data, node)
  }
}

// 节点展开
const handleNodeExpand = (data) => {
  if (!data) return
  const keys = new Set(props.expandedKeys)
  keys.add(data.id)
  emit('update:expanded-keys', Array.from(keys))
  emit('node-expand', data)
}

// 节点折叠
const handleNodeCollapse = (data) => {
  if (!data) return
  const keys = props.expandedKeys.filter((id) => id !== data.id)
  emit('update:expanded-keys', keys)
  emit('node-collapse', data)
}

// 双击节点
const handleNodeDblclick = (event, node, data) => {
  event.preventDefault()

  const selection = window.getSelection?.()
  if (selection && selection.removeAllRanges) {
    selection.removeAllRanges()
  }

  // 切换节点展开/折叠状态
  if (node) {
    const willExpand = !node.expanded
    node.expanded = willExpand

    // 触发对应的展开/折叠事件，让父组件可以处理（例如懒加载数据）
    if (willExpand) {
      handleNodeExpand(data)
    } else {
      handleNodeCollapse(data)
    }
  }

  emit('node-dblclick', data)
}

// 搜索输入变化
const handleFilterChange = (value) => {
  emit('update:filter-text', value)
}

// 全局刷新
const emitRefresh = () => {
  emit('refresh')
}

// 节点操作相关
const refreshingSet = computed(() => {
  const ids = Array.isArray(props.refreshingNodeIds) ? props.refreshingNodeIds : []
  return new Set(ids)
})

// 获取可见的节点操作
const visibleNodeActions = (node) => {
  return props.nodeActions.filter((action) => {
    if (typeof action.visible === 'function') {
      return action.visible(node)
    }
    return true
  })
}

// 判断操作是否禁用
const isActionDisabled = (action, node) => {
  if (props.loading) return true
  if (typeof action.disabled === 'function') {
    return action.disabled(node)
  }
  return false
}

// 判断操作是否在加载中
const isActionLoading = (action, node) => {
  if (typeof action.loading === 'function') {
    return action.loading(node)
  }
  return false
}

// 触发节点操作
const handleNodeAction = (action, node) => {
  if (isActionDisabled(action, node)) return
  emit('node-action', { action: action.name, node })
}

defineExpose({
  scrollToNode: selectAndScrollToNode
})
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
  overflow: hidden;
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
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.tree-search {
  padding: 12px 16px 8px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.tree-container :deep(.el-tree) {
  flex: 1;
  overflow: auto;
  padding: 0 16px 16px;
}

.tree-container :deep(.el-tree-node__content) {
  border-radius: 4px;
  transition: background-color 0.18s ease, box-shadow 0.18s ease, color 0.18s ease;
}

.tree-container :deep(.el-tree-node.resource-tree-unloaded-children > .el-tree-node__content .el-tree-node__expand-icon.is-leaf) {
  visibility: visible;
  color: var(--el-text-color-secondary);
  pointer-events: none;
}

.tree-container :deep(.el-tree-node__content:has(.tree-node-wrapper.resource-tree-unloaded-children-inline) .el-tree-node__expand-icon.is-leaf) {
  visibility: visible;
  color: var(--el-text-color-secondary);
  pointer-events: none;
}

.tree-container :deep(.el-tree-node.is-current > .el-tree-node__content) {
  background: color-mix(in srgb, var(--el-color-primary) 18%, transparent);
  box-shadow:
    inset 3px 0 0 var(--el-color-primary),
    inset 0 0 0 1px color-mix(in srgb, var(--el-color-primary) 42%, transparent);
}

.tree-container :deep(.el-tree-node.is-current > .el-tree-node__content .tree-node .label),
.tree-container :deep(.el-tree-node.is-current > .el-tree-node__content .tree-node .el-icon) {
  color: var(--el-color-primary);
}

.tree-container :deep(.el-tree-node.is-current > .el-tree-node__content .tree-node .label) {
  font-weight: 600;
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

.tree-node.highlight .label {
  font-weight: 600;
  color: var(--el-color-primary);
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

.node-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.node-action {
  display: flex;
  align-items: center;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  opacity: 0.75;
  transition: color 0.2s, opacity 0.2s;
  padding: 2px;
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

.node-action.loading {
  cursor: wait;
  opacity: 1;
  color: var(--el-color-primary);
}
</style>
