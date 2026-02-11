<template>
  <div class="data-source-selector">
    <!-- 引擎选择器 -->
    <div v-if="showEngineSelector" class="engine-selector">
      <el-form-item label="存储引擎">
        <el-select
          v-model="selectedEngineId"
          placeholder="请选择存储引擎"
          style="width: 100%"
          :loading="loadingEngines"
          @change="handleEngineChange"
        >
          <el-option
            v-for="engine in filteredEngines"
            :key="engine.id"
            :label="`${engine.name} (${engine.engine_type})`"
            :value="engine.id"
          />
        </el-select>
      </el-form-item>
    </div>

    <!-- 树形选择器 -->
    <div v-if="selectedEngineId && treeData" class="tree-selector" :style="{ height: treeHeight }">
      <ResourceTree
        ref="treeRef"
        :tree-data="stableTreeData"
        :loading="loadingTree"
        :expand-on-click-node="true"
        :show-refresh-button="false"
        :current-node-key="currentNodeKey"
        :default-expand-root="true"
        :default-expand-all="false"
        :lazy="true"
        :load="loadNode"
        title=""
        @node-click="handleNodeClick"
      >
        <template #node="{ data }">
          <span class="tree-node">
            <el-icon :size="16" style="margin-right: 4px">
              <component :is="getIconComponent(data.icon)" />
            </el-icon>
            <span>{{ data.label }}</span>
            <!-- 几何列标签：从缓存中读取，避免依赖 metadata 的响应式更新 -->
            <el-tag
              v-if="showGeometryTag && data.type === 'table' && hasGeometryColumn(data)"
              size="small"
              type="success"
              style="margin-left: 8px"
            >
              空间
            </el-tag>
            <!-- 多选模式的选中标记 -->
            <el-icon
              v-if="selectionMode === 'multiple' && isNodeSelected(data.id)"
              :size="16"
              color="#409EFF"
              style="margin-left: 8px"
            >
              <Select />
            </el-icon>
          </span>
        </template>
      </ResourceTree>
    </div>

    <!-- 选中的数据源信息 -->
    <div v-if="showSelectionInfo && currentSelection" class="selection-info">
      <el-alert
        type="success"
        :closable="false"
        style="margin-top: 16px"
      >
        <template #title>
          <div>
            已选择：<strong>{{ currentSelection.fullName }}</strong>
          </div>
          <div v-if="currentSelection.hasGeometry" style="margin-top: 8px; font-size: 13px">
            <span>
              几何列：{{ currentSelection.geometryColumn }}
              (SRID: {{ currentSelection.srid || 'unknown' }})
            </span>
          </div>
          <div v-else style="margin-top: 8px; font-size: 13px; color: #909399">
            无几何列
          </div>
        </template>
      </el-alert>
    </div>

    <!-- 检测进度提示 -->
    <div v-if="detecting" class="detecting-tip">
      <el-icon class="is-loading"><Loading /></el-icon>
      <span style="margin-left: 8px">正在检测表结构...</span>
    </div>

    <!-- 多选模式的选中列表 -->
    <div v-if="selectionMode === 'multiple' && selectedItems.size > 0" class="selected-list">
      <div class="selected-list-header">
        已选择 {{ selectedItems.size }} 项
        <el-button type="text" size="small" @click="clearSelection">清空</el-button>
      </div>
      <div class="selected-list-items">
        <el-tag
          v-for="item in Array.from(selectedItems.values())"
          :key="item.locator"
          closable
          @close="removeSelection(item)"
          style="margin: 4px"
        >
          {{ item.fullName }}
        </el-tag>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Folder,
  Document,
  Coin,
  Loading,
  Select
} from '@element-plus/icons-vue'
import ResourceTree from './ResourceTree.vue'
import * as DataSourceAPI from '../api/dataSource'

const props = defineProps({
  // API 配置
  apiBaseUrl: {
    type: String,
    required: true
  },

  // 数据源过滤
  engineTypes: {
    type: Array,
    default: () => []
  },
  dataSourceTypes: {
    type: Array,
    default: () => []
  },
  nodeTypes: {
    type: Array,
    default: () => []
  },

  // 几何列检测
  enableGeometryDetection: {
    type: Boolean,
    default: true
  },
  requireGeometry: {
    type: Boolean,
    default: false
  },

  // 选择模式
  selectionMode: {
    type: String,
    default: 'single',  // 'single' | 'multiple'
    validator: (value) => ['single', 'multiple'].includes(value)
  },
  selectableNodeTypes: {
    type: Array,
    default: () => ['table', 'object']
  },

  // UI 配置
  showEngineSelector: {
    type: Boolean,
    default: true
  },
  showGeometryTag: {
    type: Boolean,
    default: true
  },
  showSelectionInfo: {
    type: Boolean,
    default: true
  },
  treeHeight: {
    type: String,
    default: '400px'
  },

  // 初始状态
  initialEngineId: {
    type: Number,
    default: null
  }
})

const emit = defineEmits([
  'update:selection',
  'engine-change',
  'node-click',
  'geometry-detected',
  'loading-change',
  'error'
])

// 状态
const engines = ref([])
const selectedEngineId = ref(props.initialEngineId)
const treeData = ref(null)
const treeRef = ref(null)
const currentNodeKey = ref(null)
const currentSelection = ref(null)
const selectedItems = ref(new Map())  // 多选模式：Map<nodeId, selection>
const geometryCache = ref(new Map())  // 几何信息缓存：Map<nodeId, geometryInfo>，避免修改 treeData 触发重渲染
const loadingEngines = ref(false)
const loadingTree = ref(false)
const detecting = ref(false)

// 过滤后的引擎列表
const filteredEngines = computed(() => {
  let result = engines.value

  // 按引擎类型过滤
  if (props.engineTypes?.length > 0) {
    result = result.filter(engine =>
      props.engineTypes.includes(engine.engine_type)
    )
  }

  // 按数据源类型过滤
  if (props.dataSourceTypes?.length > 0) {
    const isDatabaseEngine = (engine) =>
      ['postgresql', 'mysql', 'doris', 'clickhouse', 'mongodb', 'spark'].includes(engine.engine_type)
    const isObjectStorageEngine = (engine) =>
      ['minio', 's3'].includes(engine.engine_type)

    result = result.filter(engine => {
      if (props.dataSourceTypes.includes('database') && isDatabaseEngine(engine)) return true
      if (props.dataSourceTypes.includes('object_storage') && isObjectStorageEngine(engine)) return true
      return false
    })
  }

  return result
})

// 稳定的树数据数组（避免每次渲染都创建新数组导致 el-tree 重新渲染）
const stableTreeData = computed(() => {
  return treeData.value ? [treeData.value] : []
})

// 加载引擎列表
const loadEngines = async () => {
  loadingEngines.value = true
  emit('loading-change', true)

  try {
    engines.value = await DataSourceAPI.getEngines(props.apiBaseUrl, {
      engineTypes: props.engineTypes
    })

    // 如果有初始引擎 ID，自动加载树
    if (props.initialEngineId && !treeData.value) {
      await loadEngineTree(props.initialEngineId)
    }
  } catch (error) {
    ElMessage.error(error.message)
    emit('error', error)
  } finally {
    loadingEngines.value = false
    emit('loading-change', false)
  }
}

// 加载引擎树
const loadEngineTree = async (engineId) => {
  if (!engineId) return

  loadingTree.value = true
  emit('loading-change', true)

  try {
    // 只加载第一层（schemas），tables 通过 lazy 模式按需加载
    const tree = await DataSourceAPI.getEngineTree(props.apiBaseUrl, engineId, {
      expandDepth: 1
    })

    treeData.value = tree
    console.log('[DataSourceSelector] 树数据已加载:', tree)
    console.log('[DataSourceSelector] 根节点 children 数量:', tree.children?.length)
    if (tree.children && tree.children.length > 0) {
      console.log('[DataSourceSelector] 第一个 schema 节点:', tree.children[0])
      console.log('[DataSourceSelector] 第一个 schema 的 hasChildren:', tree.children[0].hasChildren)
      console.log('[DataSourceSelector] 第一个 schema 的 metadata:', tree.children[0].metadata)
    }
  } catch (error) {
    ElMessage.error(error.message)
    emit('error', error)
  } finally {
    loadingTree.value = false
    emit('loading-change', false)
  }
}

// 处理引擎变化
const handleEngineChange = async () => {
  // 重置状态
  treeData.value = null
  currentSelection.value = null
  currentNodeKey.value = null
  selectedItems.value.clear()
  geometryCache.value.clear()  // 清空几何信息缓存

  if (selectedEngineId.value) {
    await loadEngineTree(selectedEngineId.value)
    emit('engine-change', selectedEngineId.value)
  }
}

// 判断节点是否可选择
const isSelectableNode = (node) => {
  if (!node) return false

  // 如果指定了可选节点类型
  if (props.selectableNodeTypes?.length > 0) {
    return props.selectableNodeTypes.includes(node.type)
  }

  // 默认可选
  return true
}

// 判断节点是否已选中（多选模式）
const isNodeSelected = (nodeId) => {
  return selectedItems.value.has(nodeId)
}

// 处理节点点击
const handleNodeClick = async (node) => {
  console.log('[DataSourceSelector] ===== handleNodeClick 开始 =====', {
    nodeId: node?.id,
    nodeType: node?.type,
    label: node?.label,
    timestamp: Date.now()
  })

  if (!node) {
    console.log('[DataSourceSelector] 节点为空，退出')
    return
  }

  console.log('[DataSourceSelector] 发射 node-click 事件')
  emit('node-click', node)

  // 只允许选择特定类型的节点
  if (!isSelectableNode(node)) {
    console.log('[DataSourceSelector] 节点不可选，退出')
    return
  }

  // 多选模式：切换选中状态
  if (props.selectionMode === 'multiple') {
    console.log('[DataSourceSelector] 多选模式处理中...')
    if (selectedItems.value.has(node.id)) {
      selectedItems.value.delete(node.id)
      console.log('[DataSourceSelector] 取消选中:', node.id)
    } else {
      const selection = DataSourceAPI.extractDataSourceSelection(node)
      selectedItems.value.set(node.id, selection)
      console.log('[DataSourceSelector] 选中节点:', node.id)
    }

    // 发射多选更新事件
    const selections = Array.from(selectedItems.value.values())
    emit('update:selection', selections)
    console.log('[DataSourceSelector] 多选完成，已选择:', selections.length, '个')
    return
  }

  // 单选模式
  console.log('[DataSourceSelector] 单选模式，设置 currentNodeKey')
  console.log('[DataSourceSelector] currentNodeKey 之前:', currentNodeKey.value)
  currentNodeKey.value = node.id
  console.log('[DataSourceSelector] currentNodeKey 之后:', currentNodeKey.value)

  // 如果是表节点，检测几何列
  if (props.enableGeometryDetection && node.type === 'table') {
    console.log('[DataSourceSelector] 开始检测几何列...')
    await detectGeometry(node)
    console.log('[DataSourceSelector] 几何列检测完成')
  } else {
    console.log('[DataSourceSelector] 跳过几何检测 (enable:', props.enableGeometryDetection, 'type:', node.type, ')')
  }

  // 提取数据源选择信息
  console.log('[DataSourceSelector] 提取数据源选择信息...')
  const selection = DataSourceAPI.extractDataSourceSelection(node, {
    includeGeometry: true
  })

  // 从缓存中补充几何信息（如果已检测）
  const cachedGeometry = geometryCache.value.get(node.id)
  if (cachedGeometry) {
    console.log('[DataSourceSelector] 从缓存中读取几何信息:', cachedGeometry)
    selection.hasGeometry = cachedGeometry.has_geometry
    selection.geometryColumn = cachedGeometry.geometry_column
    selection.srid = cachedGeometry.srid
    selection.geometryType = cachedGeometry.geometry_type
    selection.extent = cachedGeometry.extent
  }

  console.log('[DataSourceSelector] 提取的 selection:', selection)

  // 如果要求必须有几何列，但检测结果无几何列，则不允许选择
  if (props.requireGeometry && !selection.hasGeometry) {
    console.log('[DataSourceSelector] 不满足 requireGeometry 要求')
    ElMessage.warning('请选择空间表（包含几何列的表）')
    return
  }

  console.log('[DataSourceSelector] 设置 currentSelection')
  console.log('[DataSourceSelector] currentSelection 之前:', currentSelection.value)
  currentSelection.value = selection
  console.log('[DataSourceSelector] currentSelection 之后:', currentSelection.value)

  console.log('[DataSourceSelector] 发射 update:selection 事件')
  emit('update:selection', selection)
  console.log('[DataSourceSelector] ===== handleNodeClick 结束 =====')
}

// 检测几何列
const detectGeometry = async (node) => {
  console.log('[DataSourceSelector] >>> detectGeometry 开始 >>>', {
    nodeId: node.id,
    locator: node.locator,
    metadata_before: node.metadata
  })

  console.log('[DataSourceSelector] 设置 detecting = true')
  detecting.value = true

  try {
    // 解析 locator 获取 schema 和 table
    console.log('[DataSourceSelector] 解析 locator...')
    const parsed = DataSourceAPI.parseLocator(node.locator)
    if (!parsed) {
      console.warn('[DataSourceSelector] Failed to parse locator:', node.locator)
      return
    }

    const { path } = parsed
    const schema = path[0]
    const tableName = path[path.length - 1]
    console.log('[DataSourceSelector] 解析结果:', { schema, tableName, path })

    // 调用检测 API
    console.log('[DataSourceSelector] 调用 detectTableMetadata API...', {
      apiBaseUrl: props.apiBaseUrl,
      engine_id: selectedEngineId.value,
      schema,
      table: tableName
    })
    const result = await DataSourceAPI.detectTableMetadata(props.apiBaseUrl, {
      engine_id: selectedEngineId.value,
      schema,
      table: tableName
    })
    console.log('[DataSourceSelector] API 返回结果:', result)

    // 将几何信息缓存到 geometryCache，避免修改 treeData 触发重渲染
    console.log('[DataSourceSelector] 缓存几何信息到 geometryCache...')
    geometryCache.value.set(node.id, {
      has_geometry: result.has_geometry,
      geometry_column: result.geometry_column,
      srid: result.srid,
      geometry_type: result.geometry_type,
      extent: result.extent
    })
    console.log('[DataSourceSelector] 缓存完成:', geometryCache.value.get(node.id))

    console.log('[DataSourceSelector] 发射 geometry-detected 事件')
    emit('geometry-detected', result)
  } catch (error) {
    console.error('[DataSourceSelector] detectGeometry 失败:', error)
    // 不抛出错误，让用户可以继续选择
  } finally {
    console.log('[DataSourceSelector] 设置 detecting = false')
    detecting.value = false
    console.log('[DataSourceSelector] <<< detectGeometry 结束 <<<')
  }
}

// 懒加载子节点
const loadNode = async (node, resolve) => {
  console.log('[DataSourceSelector] loadNode called:', {
    level: node.level,
    nodeId: node.id,
    label: node.data?.label,
    type: node.data?.type,
    hasChildren: node.data?.hasChildren,
    childrenCount: node.data?.children?.length || 0,
    metadata: node.data?.metadata
  })

  // 如果是引擎根节点，返回已加载的 schemas
  if (node.level === 0) {
    console.log('[DataSourceSelector] Root node, returning initial children')
    resolve(treeData.value?.children || [])
    return
  }

  // 如果是 engine 类型节点，返回已有的 children（engine 节点的 children 已在初始加载时获取）
  if (node.data?.type === 'engine') {
    console.log('[DataSourceSelector] Engine node, returning existing children:', node.data?.children?.length || 0)
    resolve(node.data?.children || [])
    return
  }

  // 特殊处理：schema/bucket 类型节点总是尝试加载子节点
  // 原因：Meta 数据的 ItemCount 可能为 0（未扫描或扫描不准确），但实际可能有表
  const isContainerNode = ['schema', 'bucket', 'database', 'directory'].includes(node.data?.type)

  // 如果不是容器节点，且没有 hasChildren 标志，说明它是叶子节点
  if (!isContainerNode && !node.data?.hasChildren) {
    console.log('[DataSourceSelector] Leaf node, no children')
    resolve([])
    return
  }

  // 从节点 metadata 中获取 meta_id
  const metaId = node.data?.metadata?.meta_id
  if (!metaId) {
    console.warn('[DataSourceSelector] No meta_id in node:', {
      nodeId: node.id,
      level: node.level,
      type: node.data?.type,
      label: node.data?.label,
      locator: node.data?.locator,
      metadata: node.data?.metadata
    })
    resolve([])
    return
  }

  try {
    // 调用 API 获取子节点
    console.log('[DataSourceSelector] Loading children for meta_id:', metaId)
    const children = await DataSourceAPI.getNodeChildren(props.apiBaseUrl, metaId)
    console.log('[DataSourceSelector] Loaded children:', children?.length || 0, 'items')

    // 返回子节点
    resolve(children || [])
  } catch (error) {
    console.error('[DataSourceSelector] loadNode failed:', error)
    ElMessage.error(`加载子节点失败: ${error.message}`)
    resolve([])
  }
}

// 获取图标组件
const getIconComponent = (iconName) => {
  const iconMap = {
    database: Coin,
    schema: Folder,
    table: Document,
    bucket: Folder,
    directory: Folder,
    object: Document,
    folder: Folder,
    file: Document
  }
  return iconMap[iconName] || Document
}

// 检查节点是否有几何列（从缓存或 metadata 中读取）
const hasGeometryColumn = (node) => {
  // 优先从缓存中读取
  const cached = geometryCache.value.get(node.id)
  if (cached) {
    return cached.has_geometry
  }
  // 降级到 metadata（兼容旧数据）
  return node.metadata?.has_geometry || false
}

// 清空选择（多选模式）
const clearSelection = () => {
  selectedItems.value.clear()
  emit('update:selection', [])
}

// 移除单个选择（多选模式）
const removeSelection = (item) => {
  // 找到对应的 node ID
  for (const [nodeId, selection] of selectedItems.value.entries()) {
    if (selection.locator === item.locator) {
      selectedItems.value.delete(nodeId)
      break
    }
  }

  const selections = Array.from(selectedItems.value.values())
  emit('update:selection', selections)
}

// 获取当前选择（外部调用）
const getSelection = () => {
  if (props.selectionMode === 'multiple') {
    return Array.from(selectedItems.value.values())
  }
  return currentSelection.value
}

// 监听初始引擎 ID 变化
watch(() => props.initialEngineId, (newValue) => {
  if (newValue && newValue !== selectedEngineId.value) {
    selectedEngineId.value = newValue
    loadEngineTree(newValue)
  }
})

// 挂载时加载引擎列表
onMounted(() => {
  loadEngines()
})

// 暴露方法供父组件调用
defineExpose({
  getSelection,
  loadEngines,
  clearSelection
})
</script>

<style scoped>
.data-source-selector {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.engine-selector {
  width: 100%;
}

.tree-selector {
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 12px;
  overflow-y: auto;
}

.tree-node {
  display: flex;
  align-items: center;
  width: 100%;
}

.selection-info {
  width: 100%;
}

.detecting-tip {
  display: flex;
  align-items: center;
  color: #409eff;
  font-size: 14px;
}

.selected-list {
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 12px;
}

.selected-list-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
  font-weight: 500;
}

.selected-list-items {
  display: flex;
  flex-wrap: wrap;
}

:deep(.el-tree-node__content) {
  height: 32px;
}

:deep(.el-tree-node:focus > .el-tree-node__content) {
  background-color: var(--addp-bg-secondary);
}
</style>
