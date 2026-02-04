<template>
  <div class="data-source-cascader">
    <!-- 引擎选择 -->
    <div v-if="showEngineSelector" class="cascader-level">
      <el-form-item label="存储引擎">
        <el-select
          v-model="selections[0]"
          placeholder="请选择存储引擎"
          filterable
          :loading="loading && currentLoadingLevel === 0"
          style="width: 100%"
          @change="handleLevelChange(0)"
        >
          <el-option
            v-for="engine in levelOptions[0]"
            :key="engine.id"
            :label="`${engine.label} (${engine.metadata.engine_type})`"
            :value="engine.id"
          >
            <span class="option-with-icon">
              <el-icon><component :is="getIconComponent(engine.icon)" /></el-icon>
              <span style="margin-left: 8px">{{ engine.label }}</span>
              <el-tag size="small" style="margin-left: 8px">{{ engine.metadata.engine_type }}</el-tag>
            </span>
          </el-option>
        </el-select>
      </el-form-item>
    </div>

    <!-- 动态层级渲染（Schema/Bucket → Table/Object） -->
    <div
      v-for="(level, index) in visibleLevels"
      :key="level"
      class="cascader-level"
    >
      <el-form-item :label="getLevelLabel(level)">
        <el-select
          v-model="selections[index + 1]"
          :placeholder="`请选择${getLevelLabel(level)}`"
          filterable
          :loading="loading && currentLoadingLevel === index + 1"
          :multiple="isLastLevel(index + 1) && selectionMode === 'multiple'"
          :collapse-tags="isLastLevel(index + 1) && selectionMode === 'multiple'"
          :max-collapse-tags="2"
          style="width: 100%"
          @change="handleLevelChange(index + 1)"
        >
          <el-option
            v-for="option in levelOptions[index + 1]"
            :key="option.id"
            :label="option.label"
            :value="option.id"
          >
            <span class="option-with-icon">
              <el-icon><component :is="getIconComponent(option.icon)" /></el-icon>
              <span style="margin-left: 8px">{{ option.label }}</span>
              <!-- 显示几何标签 -->
              <el-tag
                v-if="showGeometryTag && option.type === 'table' && hasGeometryColumn(option)"
                size="small"
                type="success"
                style="margin-left: 8px"
              >
                空间
              </el-tag>
            </span>
          </el-option>
        </el-select>
      </el-form-item>
    </div>

    <!-- 选中信息展示 -->
    <div v-if="showSelectionInfo && currentSelection" class="selection-info">
      <el-alert
        type="success"
        :closable="false"
        style="margin-top: 16px"
      >
        <template #title>
          <div v-if="selectionMode === 'single'">
            已选择：<strong>{{ currentSelection.fullName }}</strong>
          </div>
          <div v-else>
            已选择 {{ currentSelection.length }} 项
          </div>
          <div v-if="selectionMode === 'single' && currentSelection.hasGeometry" style="margin-top: 8px; font-size: 13px">
            <el-icon><Location /></el-icon>
            几何列：{{ currentSelection.geometryColumn }}
            (SRID: {{ currentSelection.srid || 'unknown' }})
          </div>
        </template>
      </el-alert>
    </div>

    <!-- 几何列检测进度 -->
    <div v-if="detecting" class="detecting-tip">
      <el-icon class="is-loading"><Loading /></el-icon>
      <span style="margin-left: 8px">正在检测表结构...</span>
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
  Location,
  Loading
} from '@element-plus/icons-vue'
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
  selectableNodeTypes: {
    type: Array,
    default: () => ['table', 'object']
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

  // 初始状态
  initialEngineId: {
    type: Number,
    default: null
  }
})

const emit = defineEmits([
  'update:selection',
  'engine-change',
  'level-change',
  'geometry-detected',
  'loading-change',
  'error'
])

// 状态
const selections = ref([])  // 各层级的选择：[engineId, schemaId, tableId, ...]
const levelOptions = ref([[]])  // 各层级的选项：[[engines], [schemas], [tables], ...]
const nodeCache = ref(new Map())  // 节点缓存：nodeId → TreeNode
const geometryCache = ref(new Map())  // 几何信息缓存
const loading = ref(false)
const currentLoadingLevel = ref(-1)
const detecting = ref(false)
const currentSelection = ref(null)

// 层级标签映射
const levelLabels = {
  'schema': 'Schema',
  'database': 'Database',
  'bucket': 'Bucket',
  'directory': '目录',
  'prefix': '前缀',
  'table': '数据表',
  'collection': '集合',
  'object': '对象'
}

// 可见的层级（动态计算）
const visibleLevels = computed(() => {
  const levels = []
  for (let i = 1; i < levelOptions.value.length; i++) {
    if (levelOptions.value[i]?.length > 0) {
      levels.push(i)
    }
  }
  return levels
})

// 获取层级标签
const getLevelLabel = (level) => {
  const options = levelOptions.value[level]
  if (!options || options.length === 0) return '节点'

  // 从第一个选项推断类型
  const nodeType = options[0].type
  return levelLabels[nodeType] || nodeType
}

// 判断是否为最后一层
const isLastLevel = (level) => {
  // 如果当前层的节点类型是可选类型，则为最后一层
  const options = levelOptions.value[level]
  if (!options || options.length === 0) return false

  const nodeType = options[0].type
  return props.selectableNodeTypes.includes(nodeType)
}

// 判断是否需要加载下一层
const shouldLoadNextLevel = (node) => {
  // 如果节点类型是可选类型，不再加载下一层
  if (props.selectableNodeTypes.includes(node.type)) {
    return false
  }

  // 如果节点有子节点，加载下一层
  return node.hasChildren
}

// 加载引擎列表
const loadEngines = async () => {
  loading.value = true
  currentLoadingLevel.value = 0
  emit('loading-change', true)

  try {
    const engines = await DataSourceAPI.getEngines(props.apiBaseUrl, {
      engineTypes: props.engineTypes,
      dataSourceTypes: props.dataSourceTypes
    })

    // 转换为 TreeNode 格式
    levelOptions.value[0] = engines.map(engine => ({
      id: `engine-${engine.id}`,
      label: engine.name,
      type: 'engine',
      icon: getEngineIconName(engine.engine_type),
      metadata: {
        engine_id: engine.id,
        engine_type: engine.engine_type
      },
      hasChildren: true
    }))

    // 缓存引擎节点
    levelOptions.value[0].forEach(node => {
      nodeCache.value.set(node.id, node)
    })

    // 如果有初始引擎，自动选择
    if (props.initialEngineId) {
      selections.value[0] = `engine-${props.initialEngineId}`
      await loadNextLevel(0)
    }
  } catch (error) {
    ElMessage.error(error.message)
    emit('error', error)
  } finally {
    loading.value = false
    currentLoadingLevel.value = -1
    emit('loading-change', false)
  }
}

// 加载下一层
const loadNextLevel = async (currentLevel) => {
  const selectedId = selections.value[currentLevel]
  if (!selectedId) return

  const node = nodeCache.value.get(selectedId)
  if (!node) {
    console.error('[DataSourceCascader] Node not found in cache:', selectedId)
    return
  }

  // 判断是否需要加载下一层
  if (!shouldLoadNextLevel(node)) {
    console.log('[DataSourceCascader] Reached selectable node, no more levels to load')
    await handleSelectionComplete(node)
    return
  }

  loading.value = true
  currentLoadingLevel.value = currentLevel + 1
  emit('loading-change', true)

  try {
    let children = []

    if (node.type === 'engine') {
      // 加载第一层（schemas/buckets）
      const engineId = node.metadata.engine_id
      const tree = await DataSourceAPI.getEngineTree(props.apiBaseUrl, engineId, {
        expandDepth: 1
      })
      children = tree.children || []
    } else {
      // 加载子节点
      const metaId = node.metadata.meta_id
      if (!metaId) {
        console.error('[DataSourceCascader] No meta_id in node:', node)
        return
      }
      children = await DataSourceAPI.getNodeChildren(props.apiBaseUrl, metaId)
    }

    // 缓存子节点
    children.forEach(child => {
      nodeCache.value.set(child.id, child)
    })

    // 设置下一层的选项
    levelOptions.value[currentLevel + 1] = children

    // 清空后续层级的选择
    selections.value.splice(currentLevel + 1)
    levelOptions.value.splice(currentLevel + 2)
  } catch (error) {
    ElMessage.error(`加载失败: ${error.message}`)
    emit('error', error)
  } finally {
    loading.value = false
    currentLoadingLevel.value = -1
    emit('loading-change', false)
  }
}

// 处理层级变化
const handleLevelChange = async (level) => {
  console.log('[DataSourceCascader] Level change:', level, selections.value[level])

  // 清空后续层级
  selections.value.splice(level + 1)
  levelOptions.value.splice(level + 1)

  // 发射层级变化事件
  if (level === 0) {
    const engineNode = nodeCache.value.get(selections.value[0])
    if (engineNode) {
      emit('engine-change', engineNode.metadata.engine_id)
    }
  }
  emit('level-change', level, selections.value[level])

  // 加载下一层
  await loadNextLevel(level)
}

// 处理选择完成
const handleSelectionComplete = async (node) => {
  console.log('[DataSourceCascader] Selection complete:', node)

  // 如果是单选模式
  if (props.selectionMode === 'single') {
    // 检测几何列
    if (props.enableGeometryDetection && node.type === 'table') {
      await detectGeometry(node)
    }

    // 提取选择信息
    const selection = DataSourceAPI.extractDataSourceSelection(node, {
      includeGeometry: true
    })

    // 从缓存中补充几何信息
    const cachedGeometry = geometryCache.value.get(node.id)
    if (cachedGeometry) {
      selection.hasGeometry = cachedGeometry.has_geometry
      selection.geometryColumn = cachedGeometry.geometry_column
      selection.srid = cachedGeometry.srid
      selection.geometryType = cachedGeometry.geometry_type
      selection.extent = cachedGeometry.extent
    }

    // 检查是否满足 requireGeometry
    if (props.requireGeometry && !selection.hasGeometry) {
      ElMessage.warning('请选择空间表（包含几何列的表）')
      return
    }

    currentSelection.value = selection
    emit('update:selection', selection)
  } else {
    // 多选模式：获取所有选中的节点
    const selectedIds = selections.value[selections.value.length - 1]
    if (!Array.isArray(selectedIds)) return

    const selections = selectedIds.map(id => {
      const node = nodeCache.value.get(id)
      return node ? DataSourceAPI.extractDataSourceSelection(node) : null
    }).filter(s => s !== null)

    currentSelection.value = selections
    emit('update:selection', selections)
  }
}

// 检测几何列
const detectGeometry = async (node) => {
  detecting.value = true

  try {
    const parsed = DataSourceAPI.parseLocator(node.locator)
    if (!parsed) return

    const { path } = parsed
    const schema = path[0]
    const tableName = path[path.length - 1]

    // 获取引擎ID
    const engineNode = nodeCache.value.get(selections.value[0])
    if (!engineNode) return

    const result = await DataSourceAPI.detectTableMetadata(props.apiBaseUrl, {
      engine_id: engineNode.metadata.engine_id,
      schema,
      table: tableName
    })

    // 缓存几何信息
    geometryCache.value.set(node.id, {
      has_geometry: result.has_geometry,
      geometry_column: result.geometry_column,
      srid: result.srid,
      geometry_type: result.geometry_type,
      extent: result.extent
    })

    emit('geometry-detected', result)
  } catch (error) {
    console.error('[DataSourceCascader] detectGeometry failed:', error)
  } finally {
    detecting.value = false
  }
}

// 检查节点是否有几何列
const hasGeometryColumn = (node) => {
  const cached = geometryCache.value.get(node.id)
  return cached?.has_geometry || false
}

// 获取图标组件
const getIconComponent = (iconName) => {
  const iconMap = {
    Database: Coin,
    Coin: Coin,
    Folder: Folder,
    FolderOpen: Folder,
    Document: Document,
    Table: Document,
    DocumentText: Document
  }
  return iconMap[iconName] || Document
}

// 获取引擎图标名称
const getEngineIconName = (engineType) => {
  const icons = {
    postgresql: 'Database',
    mysql: 'Database',
    doris: 'Database',
    clickhouse: 'Database',
    mongodb: 'DocumentText',
    minio: 'Folder',
    s3: 'Folder'
  }
  return icons[engineType.toLowerCase()] || 'Database'
}

// 获取当前选择（外部调用）
const getSelection = () => {
  return currentSelection.value
}

// 清空选择
const clearSelection = () => {
  selections.value = []
  levelOptions.value = [[]]
  currentSelection.value = null
  geometryCache.value.clear()
  loadEngines()
}

// 监听 initialEngineId 变化
watch(() => props.initialEngineId, (newValue) => {
  if (newValue && selections.value[0] !== `engine-${newValue}`) {
    selections.value[0] = `engine-${newValue}`
    loadNextLevel(0)
  }
})

// 挂载时加载引擎列表
onMounted(() => {
  loadEngines()
})

// 暴露方法
defineExpose({
  getSelection,
  clearSelection,
  loadEngines
})
</script>

<style scoped>
.data-source-cascader {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.cascader-level {
  width: 100%;
}

.option-with-icon {
  display: flex;
  align-items: center;
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

:deep(.el-form-item) {
  margin-bottom: 0;
}

:deep(.el-form-item__label) {
  font-weight: 500;
}
</style>
