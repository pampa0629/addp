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
          placeholder="搜索..."
          :prefix-icon="Search"
          clearable
          size="small"
        />
      </div>

      <!-- 空状态 -->
      <slot v-if="!hasTreeData && !loading" name="empty" :empty-text="emptyText">
        <el-empty :description="emptyText" class="empty-placeholder" />
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
        @node-click="handleNodeClick"
        @node-expand="handleNodeExpand"
        @node-collapse="handleNodeCollapse"
        @current-change="handleCurrentChange"
      >
        <template #default="{ data, node }">
          <slot name="node" :node="node" :data="data">
            <span
              class="tree-node-wrapper"
              @dblclick="enableDblclickToggle ? handleNodeDblclick($event, node, data) : null"
            >
              <span class="tree-node" :class="[data.type, { highlight: data.highlight }]">
                <span class="node-main">
                  <slot name="node-icon" :node="node" :data="data">
                    <el-icon v-if="data.icon">
                      <component :is="getIconComponent(data.icon)" />
                    </el-icon>
                    <el-icon v-else>
                      <component :is="getDefaultIcon(data.type)" />
                    </el-icon>
                  </slot>
                  <slot name="node-label" :node="node" :data="data">
                    <span class="label" :title="data.label">{{ data.label }}</span>
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
                        <component :is="getIconComponent(action.icon)" />
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
  Collection,
  Document
} from '@element-plus/icons-vue'
import { getAllNodeKeys, traverseTree, findNodePath } from '../types/tree'

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

  // 样式
  height: {
    type: String,
    default: '100%'
  },
  cardShadow: {
    type: String,
    default: 'never'
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
    // 如果节点有 hasChildren 字段，使用它来判断
    if (data.hasChildren !== undefined) {
      return !data.hasChildren
    }
    // 否则根据 children 数组判断
    return !data.children || data.children.length === 0
  }
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

// 监听搜索关键词，自动展开匹配路径
watch(
  () => props.filterText,
  (keyword) => {
    if (keyword) {
      const matchedPaths = findMatchedPaths(props.treeData, keyword)
      emit('update:expanded-keys', matchedPaths)
    }
  }
)

// 更新树状态
const updateTreeState = () => {
  const tree = treeRef.value
  if (!tree) {
    console.warn('[ResourceTree] updateTreeState: treeRef is null')
    return
  }

  console.log('[ResourceTree] updateTreeState called:', {
    expandedKeys: computedExpandedKeys.value,
    currentNodeKey: props.currentNodeKey,
    hasStore: !!tree.store
  })

  // 使用 toggleExpand 方法强制展开节点
  if (tree.store && computedExpandedKeys.value?.length > 0) {
    console.log('[ResourceTree] 开始强制展开节点...')
    let expandedCount = 0
    let notFoundCount = 0

    for (const key of computedExpandedKeys.value) {
      const node = tree.store.getNode(key)
      if (node) {
        // 检查节点是否已展开
        if (!node.expanded) {
          node.expanded = true
          expandedCount++
        }
      } else {
        // 节点未找到可能是因为还没加载（懒加载场景），只记录 debug 信息
        notFoundCount++
        if (import.meta.env.DEV) {
          console.debug('[ResourceTree] 节点未加载或不存在:', key)
        }
      }
    }

    console.log(`[ResourceTree] 已展开 ${expandedCount} 个节点${notFoundCount > 0 ? `, ${notFoundCount} 个节点未加载` : ''}`)
  }

  // 设置当前选中的节点
  if (typeof tree.setCurrentKey === 'function') {
    if (props.currentNodeKey) {
      tree.setCurrentKey(props.currentNodeKey)
      console.log('[ResourceTree] setCurrentKey 已调用:', props.currentNodeKey)

      // 滚动到当前节点
      nextTick(() => {
        const currentNode = tree.store.getNode(props.currentNodeKey)
        if (currentNode && currentNode.isCurrent) {
          // 找到对应的 DOM 元素并滚动到可见区域
          const treeElement = tree.$el
          if (treeElement) {
            const currentNodeElement = treeElement.querySelector('.is-current')
            if (currentNodeElement) {
              currentNodeElement.scrollIntoView({ behavior: 'smooth', block: 'center' })
              console.log('[ResourceTree] 已滚动到当前节点')
            }
          }
        }
      })
    }
  }
}

onMounted(() => {
  if (hasTreeData.value) {
    nextTick(updateTreeState)
  }
})

watch(
  () => props.expandedKeys,
  () => {
    if (hasTreeData.value) {
      nextTick(updateTreeState)
    }
  },
  { deep: true }
)

watch(
  () => props.treeData,
  () => {
    if (hasTreeData.value) {
      nextTick(updateTreeState)
    }
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
        }
      }
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

// 获取图标组件
const getIconComponent = (iconName) => {
  // 这里返回图标名称，由 Element Plus 自动解析
  // 如果需要支持自定义图标，可以维护一个图标映射表
  return iconName
}

// 获取默认图标
const getDefaultIcon = (nodeType) => {
  const iconMap = {
    resource: Collection,
    schema: Folder,
    bucket: Folder,
    directory: Folder,
    table: Document,
    object: Document,
    task: Document,
    folder: Folder,
    file: Document
  }
  return iconMap[nodeType] || Document
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
