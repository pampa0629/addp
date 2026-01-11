<template>
  <div class="table-preview">
    <!-- 地图预览控制和地图区域 -->
    <template v-if="hasGeometry">
      <div class="map-controls">
        <div class="toggle-wrapper">
          <span>地图预览</span>
          <el-switch v-model="showMap" size="small" />
        </div>
        <template v-if="showMap">
          <el-select v-model="baseMapType" size="small" class="base-map-select">
            <el-option
              v-for="item in baseMapOptions"
              :key="item.value"
              :label="item.label"
              :value="item.value"
            />
          </el-select>

          <!-- Pre-Cache Button -->
          <el-button
            type="primary"
            size="small"
            :loading="quickViewLoading"
            @click="handleQuickView"
          >
            {{ getQuickViewButtonText() }}
          </el-button>

          <!-- 模式指示器 -->
          <el-tag v-if="showMap" :type="useGeoJSONMode ? 'info' : 'success'" size="small">
            {{ useGeoJSONMode ? 'GeoJSON 模式' : 'MVT 瓦片模式' }}
          </el-tag>

          <!-- Progress Display -->
          <div v-if="quickViewStatus === 'generating' && quickViewProgress" class="progress-info">
            <span class="progress-text">
              {{ quickViewProgress.tiles_processed }} / {{ quickViewProgress.tiles_total_estimate }} 瓦片
            </span>
            <span class="progress-text">
              已用时 {{ formatTime(quickViewProgress.elapsed_seconds) }}
            </span>
            <span v-if="quickViewProgress.estimated_remaining_seconds > 0" class="progress-text">
              预计剩余 {{ formatTime(quickViewProgress.estimated_remaining_seconds) }}
            </span>
          </div>
        </template>
      </div>

      <!-- 动态切换地图渲染模式 -->
      <div v-if="showMap" class="map-container" :style="{ height: mapHeight + 'px' }">
        <!-- GeoJSON 模式（瓦片未就绪时使用） -->
        <GeoJSONPreview
          v-if="useGeoJSONMode"
          ref="mapRef"
          :engine-id="engineId"
          :schema="schema"
          :table="table"
          :geom="activeGeometryColumn"
        />

        <!-- MVT 瓦片模式（瓦片已就绪） -->
        <VectorTilePreview
          v-else
          ref="mapRef"
          :resource-id="engineId"
          :schema="schema"
          :table="table"
          :geom="activeGeometryColumn"
        />
      </div>

      <div v-if="showMap" class="map-splitter" @mousedown="startMapResize"></div>
    </template>

    <!-- 表头信息栏 -->
    <div class="table-info-bar">
      <div class="info-left">
        <!-- 空间数据表标识 -->
        <el-tag v-if="hasGeometry" type="danger" size="large" class="spatial-badge">
          <el-icon><Location /></el-icon>
          空间数据表
        </el-tag>
        <!-- 普通表标识 -->
        <el-tag v-else size="large" class="table-badge">
          <el-icon><Collection /></el-icon>
          数据表
        </el-tag>
      </div>

      <div class="info-right">
        <el-tag size="small" class="info-tag">
          <el-icon><Coin /></el-icon>
          {{ engineTypeName }}
        </el-tag>
        <el-tag v-if="hasGeometry" size="small" type="warning" class="geometry-tag">
          <el-icon><Location /></el-icon>
          几何列: {{ geometryColumns.join(', ') }}
        </el-tag>
        <el-tag size="small" type="info" class="info-tag">
          总计 {{ total.toLocaleString() }} 行
        </el-tag>
        <el-tag size="small" type="success" class="info-tag">
          {{ displayRange }}
        </el-tag>
        <el-tag size="small" class="info-tag">
          {{ displayColumns.length }} 列
        </el-tag>
      </div>
    </div>

    <!-- 表格区域（保持原有分页逻辑）-->
    <div class="table-wrapper">
      <el-table
        ref="tableRef"
        :data="tableData"
        v-loading="loading"
        height="100%"
        highlight-current-row
        :row-key="getRowKey"
        :current-row-key="currentRowKey"
        @row-click="handleRowClick"
      >
        <el-table-column
          v-for="col in displayColumns"
          :key="col"
          :prop="col"
          show-overflow-tooltip
        >
          <template #header>
            <div class="column-header">
              <span class="column-name">{{ col }}</span>
              <el-tooltip
                v-if="getColumnMetadata(col)"
                :content="getColumnTooltipContent(col)"
                placement="top"
                :show-after="300"
              >
                <el-icon class="column-info-icon"><InfoFilled /></el-icon>
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 分页 -->
    <div v-if="total > 0" class="pagination">
      <el-pagination
        background
        layout="prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="currentPage"
        @current-change="handlePageChange"
      />
      <div class="tip">最多展示前 50 行数据</div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { Collection, Coin, InfoFilled, Location } from '@element-plus/icons-vue'
import { useMapConfig } from '@/composables/useMapConfig'
import { useResizable } from '@/composables/useResizable'
import VectorTilePreview from '@/components/map/VectorTilePreview.vue'
import GeoJSONPreview from '@/components/map/GeoJSONPreview.vue'
import { dataExplorerAPI } from '@/api/dataExplorer'
import { quickViewAPI } from '@/api/quickView'
import { ElMessage, ElMessageBox } from 'element-plus'

const props = defineProps({
  data: {
    type: Object,
    required: true
  },
  loading: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['page-change'])

const { baseMapOptions, defaultBaseMapType, loadMapConfig } = useMapConfig()
const { size: mapHeight, startResize: startMapResize } = useResizable(260, 140, 520, 'vertical')

const tableRef = ref(null)
const mapRef = ref(null)
const showMap = ref(false)  // 默认不显示地图,避免初始渲染延迟
const baseMapType = ref('')
const currentRowKey = ref('')
const currentPage = ref(1)
const pageSize = ref(10)

// Pre-Cache state
const quickViewLoading = ref(false)
const quickViewStatus = ref('none') // 'none' | 'generating' | 'completed' | 'failed'
const quickViewProgress = ref(null) // 存储实时进度数据
let quickViewPollingTimer = null

const columns = computed(() => props.data?.columns || [])
const rows = computed(() => props.data?.rows || [])
const total = computed(() => props.data?.total || 0)
const geometryColumns = computed(() => props.data?.geometry_columns || [])

const hasGeometry = computed(() => geometryColumns.value.length > 0)
const activeGeometryColumn = computed(() => geometryColumns.value[0] || '')

// 地图渲染模式选择（自动切换）
// - quickViewStatus 为 'ready' 或 'completed' → 使用 MVT 瓦片模式（高性能）
// - quickViewStatus 为 'none' 或 'generating' → 使用 GeoJSON 模式（轻量级）
const useGeoJSONMode = computed(() => {
  const status = quickViewStatus.value
  // MVT 瓦片未就绪时使用 GeoJSON
  return status === 'none' || status === 'generating' || status === 'cancelled' || status === 'failed'
})

// 资源信息（用于 MVT 预览）
const engineId = computed(() => props.data?.engineId)
const schema = computed(() => props.data?.schema)
const table = computed(() => {
  const rawTable = props.data?.table || props.data?.path
  // 如果table包含schema前缀（如 "public.dltb"），去除前缀
  if (rawTable && rawTable.includes('.')) {
    return rawTable.split('.').pop()
  }
  return rawTable
})

// 过滤掉几何列后的显示列
const displayColumns = computed(() => {
  if (!columns.value || columns.value.length === 0) return []
  const geometrySet = new Set(geometryColumns.value || [])
  const filtered = columns.value.filter((col) => !geometrySet.has(col))
  return filtered.length > 0 ? filtered : columns.value
})

// 表头信息栏相关计算属性
const engineTypeName = computed(() => {
  const type = props.data?.engineType || ''
  const typeMap = {
    postgresql: 'PostgreSQL',
    mysql: 'MySQL',
    doris: 'Apache Doris',
    clickhouse: 'ClickHouse',
    mongodb: 'MongoDB',
    spark: 'Apache Spark'
  }
  return typeMap[type.toLowerCase()] || type || '未知引擎'
})

const displayRange = computed(() => {
  if (total.value === 0) return '无数据'
  const start = (currentPage.value - 1) * pageSize.value + 1
  const end = Math.min(currentPage.value * pageSize.value, total.value)
  return `显示 ${start.toLocaleString()}-${end.toLocaleString()} 行`
})

// 列元数据相关
const columnMetadata = computed(() => props.data?.column_metadata || [])

// 根据列名获取列元数据
const getColumnMetadata = (columnName) => {
  return columnMetadata.value.find(meta => meta.column_name === columnName)
}

// 生成列的 Tooltip 内容
const getColumnTooltipContent = (columnName) => {
  const meta = getColumnMetadata(columnName)
  if (!meta) return ''

  const parts = [
    `列名: ${meta.column_name}`,
    `类型: ${meta.data_type}`,
    `可空: ${meta.is_nullable ? '是' : '否'}`,
    `主键: ${meta.is_primary_key ? '是' : '否'}`
  ]

  if (meta.comment) {
    parts.push(`注释: ${meta.comment}`)
  }

  return parts.join('\n')
}

// 生成行键
const tableData = computed(() => {
  const baseKey = `${props.data?.engineId || 'res'}-${props.data?.schema || 'schema'}-${props.data?.table || 'table'}`
  return rows.value.map((row, index) => ({
    ...row,
    __rowKey: `${baseKey}-${(currentPage.value - 1) * pageSize.value + index}`
  }))
})

const getRowKey = (row) => {
  return row?.__rowKey || row?.id || row?.ID || row?._id || row?.uuid || String(Math.random())
}

const handleRowClick = async (row) => {
  currentRowKey.value = row?.__rowKey || ''
  if (tableRef.value) {
    tableRef.value.setCurrentRow(row)
  }

  // MVT 预览模式下，通过 API 查询要素中心点并定位
  if (hasGeometry.value && showMap.value && mapRef.value) {
    const featureId = row.id || row.ID || row._id || row.uuid
    if (!featureId) {
      console.warn('表格行缺少主键ID，无法定位到地图')
      return
    }

    try {
      // 调用后端 API 查询要素中心点
      const response = await dataExplorerAPI.getFeatureCentroid(
        engineId.value,
        schema.value,
        table.value,
        featureId,
        activeGeometryColumn.value,
        'id'  // 主键列名，可根据实际情况调整
      )

      const centroid = response.data
      // 调用地图组件的定位方法
      if (mapRef.value && typeof mapRef.value.focusFeatureById === 'function') {
        mapRef.value.focusFeatureById(featureId, centroid)
      }
    } catch (error) {
      console.error('查询要素中心点失败:', error)
      // 用户点击行时如果查询失败，只记录错误不弹窗，避免打断交互
    }
  }
}

// handleFeatureClick 已删除（MVT 模式下暂不支持地图→表格定位）

const handlePageChange = (page) => {
  currentPage.value = page
  emit('page-change', page)
}

// Pre-Cache 处理函数 (两步式：先预览配置，再确认生成)
const handleQuickView = async () => {
  if (!engineId.value || !schema.value || !table.value) {
    ElMessage.warning('缺少必要参数，无法启用预缓存')
    return
  }

  if (quickViewStatus.value === 'generating') {
    ElMessage.info('预缓存正在生成中，请稍后')
    return
  }

  try {
    quickViewLoading.value = true

    // 步骤1: 获取瓦片配置（计算 minZoom 和 maxZoom）
    const configResponse = await quickViewAPI.getTileConfig(engineId.value, schema.value, table.value)
    const tileConfig = configResponse.data

    const { min_zoom: calculatedMin, max_zoom: calculatedMax, extent, srid } = tileConfig

    // 步骤2: 使用 MessageBox 让用户确认或修改配置
    const { value } = await ElMessageBox.prompt(
      `检测到数据范围：\n` +
      `  - 记录数: ${extent ? '已计算' : '未知'}\n` +
      `  - 坐标系: SRID ${srid || 4326}\n\n` +
      `建议配置：\n` +
      `  - MinZoom: ${calculatedMin}\n` +
      `  - MaxZoom: ${calculatedMax}\n\n` +
      `请输入 MinZoom,MaxZoom (例如: 4,11)`,
      '确认预缓存配置',
      {
        confirmButtonText: '开始生成',
        cancelButtonText: '取消',
        inputValue: `${calculatedMin},${calculatedMax}`,
        inputPattern: /^\d+,\d+$/,
        inputErrorMessage: '请输入正确格式: MinZoom,MaxZoom (例如: 4,11)'
      }
    )

    // 解析用户输入
    const [minZoom, maxZoom] = value.split(',').map(v => parseInt(v.trim(), 10))

    if (minZoom >= maxZoom) {
      ElMessage.error('MinZoom 必须小于 MaxZoom')
      return
    }

    // 步骤3: 提交任务到后端
    await quickViewAPI.triggerQuickView(engineId.value, schema.value, table.value, {
      min_zoom: minZoom,
      max_zoom: maxZoom,
      concurrency: 10,
      priority: 'default'
    })

    quickViewStatus.value = 'generating'
    ElMessage.success(`预缓存任务已启动 (z${minZoom}-z${maxZoom})，正在后台生成`)

    // 开始轮询状态
    startQuickViewPolling()
  } catch (error) {
    // 用户取消操作
    if (error === 'cancel') {
      ElMessage.info('已取消预缓存')
      return
    }
    console.error('启用预缓存失败:', error)
    ElMessage.error(error.response?.data?.error || '启用预缓存失败')
  } finally {
    quickViewLoading.value = false
  }
}

// 获取预缓存状态
const fetchQuickViewStatus = async () => {
  if (!engineId.value || !schema.value || !table.value) {
    return
  }

  try {
    const response = await quickViewAPI.getQuickViewStatus(engineId.value, schema.value, table.value)
    const status = response.data?.status
    const progress = response.data?.progress // 提取进度数据

    if (status) {
      quickViewStatus.value = status

      // 保存进度数据（只在 generating 状态下有效）
      if (status === 'generating' && progress) {
        quickViewProgress.value = progress
      } else {
        quickViewProgress.value = null
      }

      // 如果状态变为完成或失败，停止轮询
      if (status === 'completed' || status === 'failed') {
        stopQuickViewPolling()

        if (status === 'completed') {
          ElMessage.success('预缓存生成完成')
        } else if (status === 'failed') {
          ElMessage.error('预缓存生成失败')
        }
      }
    }
  } catch (error) {
    // 如果返回404，说明还没有预缓存记录
    if (error.response?.status === 404) {
      quickViewStatus.value = 'none'
      quickViewProgress.value = null
    }
    // 其他错误不处理，避免频繁弹窗
  }
}

// 开始轮询预缓存状态
const startQuickViewPolling = () => {
  stopQuickViewPolling() // 先清除旧的定时器

  quickViewPollingTimer = setInterval(() => {
    fetchQuickViewStatus()
  }, 2000) // 每2秒查询一次（用户要求）
}

// 停止轮询
const stopQuickViewPolling = () => {
  if (quickViewPollingTimer) {
    clearInterval(quickViewPollingTimer)
    quickViewPollingTimer = null
  }
}

// 获取按钮文本（根据状态和进度）
const getQuickViewButtonText = () => {
  if (quickViewStatus.value === 'completed') {
    return '预缓存已启用'
  }
  if (quickViewStatus.value === 'generating') {
    if (quickViewProgress.value && quickViewProgress.value.progress_percent !== undefined) {
      return `生成中... ${quickViewProgress.value.progress_percent.toFixed(1)}%`
    }
    return '生成中...'
  }
  return '启用预缓存'
}

// 格式化时间（秒数转为可读格式）
const formatTime = (seconds) => {
  if (!seconds || seconds < 0) return '0秒'

  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = Math.floor(seconds % 60)

  if (hours > 0) {
    return `${hours}小时${minutes}分${secs}秒`
  } else if (minutes > 0) {
    return `${minutes}分${secs}秒`
  } else {
    return `${secs}秒`
  }
}

// 当 baseMapOptions 变化时，自动设置默认底图
watch(
  baseMapOptions,
  (newOptions) => {
    if (newOptions.length > 0 && !baseMapType.value) {
      baseMapType.value = newOptions[0].value
    }
  },
  { immediate: true }
)

// 监听表变化，重新获取预缓存状态
watch(
  () => [engineId.value, schema.value, table.value],
  () => {
    if (hasGeometry.value && engineId.value && schema.value && table.value) {
      fetchQuickViewStatus()
    }
  },
  { immediate: true }
)

onMounted(() => {
  loadMapConfig()

  // 初始化时获取预缓存状态
  if (hasGeometry.value && engineId.value && schema.value && table.value) {
    fetchQuickViewStatus()
  }
})

onUnmounted(() => {
  stopQuickViewPolling()
})
</script>

<style scoped>
.table-preview {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
}

.map-controls {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  background: var(--el-fill-color);
  border-radius: 4px;
}

.toggle-wrapper {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.base-map-select {
  min-width: 160px;
}

.map-container {
  position: relative;
  width: 100%;
  border-radius: 4px;
  overflow: hidden;
  background: #f5f5f5;
}

.map-splitter {
  height: 8px;
  cursor: row-resize;
  position: relative;
  margin: -4px 0 4px;
}

.map-splitter::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  height: 2px;
  background: var(--el-color-primary-light-8);
  border-radius: 2px;
}

.map-splitter:hover::after,
body.is-resizing .map-splitter::after {
  background: var(--el-color-primary);
}

.table-info-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background: linear-gradient(135deg, var(--el-fill-color) 0%, var(--el-fill-color-light) 100%);
  border-radius: 4px;
  border-left: 3px solid var(--el-color-primary);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
}

.info-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.spatial-badge {
  font-weight: 600;
  font-size: 14px;
  padding: 8px 12px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.spatial-badge .el-icon {
  font-size: 16px;
}

.table-badge {
  font-weight: 600;
  font-size: 14px;
  padding: 8px 12px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.table-badge .el-icon {
  font-size: 16px;
}

.info-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.info-tag {
  display: flex;
  align-items: center;
  gap: 4px;
}

.info-tag .el-icon {
  font-size: 14px;
}

.geometry-tag {
  font-weight: 600;
  padding: 6px 10px;
}

.geometry-tag .el-icon {
  font-size: 14px;
}

.column-header {
  display: flex;
  align-items: center;
  gap: 6px;
  justify-content: center;
}

.column-name {
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.column-info-icon {
  font-size: 14px;
  color: var(--el-color-info);
  cursor: help;
  transition: color 0.2s;
}

.column-info-icon:hover {
  color: var(--el-color-primary);
}

.table-wrapper {
  flex: 1 1 auto;
  min-height: 220px;
  display: flex;
  flex-direction: column;
}

.table-wrapper :deep(.el-table) {
  flex: 1;
}

.pagination {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.pagination .tip {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.progress-info {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 12px;
  color: var(--el-text-color-regular);
}

.progress-text {
  padding: 4px 8px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  white-space: nowrap;
}
</style>
