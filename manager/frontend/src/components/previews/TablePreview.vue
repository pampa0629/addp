<template>
  <div class="table-preview">
    <!-- 地图预览控制和地图区域 -->
    <template v-if="hasGeometry">
      <div class="map-controls">
        <div class="toggle-wrapper">
          <span class="toggle-label">地图预览</span>
          <el-switch v-model="showMap" size="small" />
        </div>
        <template v-if="showMap">
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
          :page="currentPage"
          :page-size="pageSize"
          @featureClick="handleFeatureClick"
        />

        <!-- MVT 瓦片模式（瓦片已就绪） -->
        <VectorTilePreview
          v-else
          ref="mapRef"
          :engine-id="engineId"
          :schema="schema"
          :table="table"
          :geom="activeGeometryColumn"
          @featureClick="handleFeatureClick"
        />
      </div>

      <div v-if="showMap" class="map-splitter" @mousedown="startMapResize"></div>
    </template>

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
          :show-overflow-tooltip="!isGeometryColumn(col)"
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
          <template #default="scope">
            <!-- 几何列：显示截断内容 + tooltip（支持选择复制）-->
            <el-tooltip
              v-if="isGeometryColumn(col)"
              placement="top"
              :show-after="300"
              raw-content
              popper-class="geometry-tooltip"
            >
              <template #content>
                <div class="geometry-tooltip-content" v-text="getFullGeometryValue(scope.row, col)"></div>
              </template>
              <span class="geometry-cell">{{ getCellValue(scope.row, col) }}</span>
            </el-tooltip>
            <!-- 普通列：直接显示 -->
            <span v-else>{{ scope.row[col] }}</span>
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
import { InfoFilled } from '@element-plus/icons-vue'
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

const { size: mapHeight, startResize: startMapResize } = useResizable(260, 140, 520, 'vertical')

const tableRef = ref(null)
const mapRef = ref(null)
const showMap = ref(false)  // 默认不显示地图,避免初始渲染延迟
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
const srid = computed(() => props.data?.srid || 0)
const extent = computed(() => props.data?.extent || [])

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

// 显示所有列（包括几何列）
const displayColumns = computed(() => {
  return columns.value || []
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

// 判断列是否为几何列
const isGeometryColumn = (columnName) => {
  return geometryColumns.value.includes(columnName)
}

// 截断几何列显示（只显示前几个字符）
const truncateGeometry = (value) => {
  if (value == null) return ''
  const str = String(value)
  const maxLength = 50
  if (str.length > maxLength) {
    return str.substring(0, maxLength) + '...'
  }
  return str
}

// 获取单元格显示值
const getCellValue = (row, columnName) => {
  const value = row[columnName]
  if (isGeometryColumn(columnName)) {
    return truncateGeometry(value)
  }
  return value
}

// 获取完整的几何值（用于 tooltip）
const getFullGeometryValue = (row, columnName) => {
  return row[columnName] || ''
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

  // MVT 预览模式下，通过 API 查询要素完整几何并高亮显示
  if (hasGeometry.value && showMap.value && mapRef.value) {
    const featureId = row.id || row.ID || row._id || row.uuid
    if (!featureId) {
      console.warn('表格行缺少主键ID，无法定位到地图')
      return
    }

    try {
      // 调用后端 API 查询要素完整几何
      const response = await dataExplorerAPI.getFeatureGeometry(
        engineId.value,
        schema.value,
        table.value,
        featureId,
        activeGeometryColumn.value,
        'id'  // 主键列名，可根据实际情况调整
      )

      // 注意：createAPIClient 默认 extractData=true，已自动提取了 response.data
      const { geojson, centroid, extent } = response
      // 调用地图组件的高亮方法，传入extent用于自适应缩放
      if (mapRef.value && typeof mapRef.value.focusFeatureById === 'function') {
        mapRef.value.focusFeatureById(featureId, geojson, centroid, extent)
      }
    } catch (error) {
      console.error('查询要素几何失败:', error)
      // 用户点击行时如果查询失败，只记录错误不弹窗，避免打断交互
    }
  }
}

// 处理地图要素点击事件（地图→表格选中）
const handleFeatureClick = (featureId) => {
  console.log('handleFeatureClick called with featureId:', featureId)
  console.log('Current tableData:', tableData.value)

  // 在当前页查找对应的行
  const targetRow = tableData.value.find(row => {
    const rowId = row.id || row.ID || row._id || row.uuid
    console.log('Comparing row id:', rowId, 'with featureId:', featureId)
    return String(rowId) === String(featureId)
  })

  console.log('Found target row:', targetRow)

  if (targetRow) {
    currentRowKey.value = targetRow.__rowKey
    if (tableRef.value) {
      tableRef.value.setCurrentRow(targetRow)
      // 滚动到对应行
      const tableBody = document.querySelector('.el-table__body-wrapper')
      if (tableBody) {
        const rowElement = tableBody.querySelector(`[data-row-key="${targetRow.__rowKey}"]`)
        if (rowElement) {
          rowElement.scrollIntoView({ behavior: 'smooth', block: 'center' })
        }
      }
    }
    console.log('Successfully set current row to:', targetRow)
  } else {
    console.warn('当前页未找到ID为', featureId, '的行')
    console.warn('当前页的所有ID:', tableData.value.map(row => row.id || row.ID || row._id || row.uuid))
  }
}

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
    // 注意: createAPIClient 默认 extractData=true，已自动提取 response.data
    const tileConfig = await quickViewAPI.getTileConfig(engineId.value, schema.value, table.value)

    // 步骤2: 使用 MessageBox 让用户确认或修改配置
    const {
      min_zoom: calculatedMin,
      max_zoom: calculatedMax,
      extent,
      srid,
      record_count: recordCount,
      estimated_tiles: estimatedTiles,
      avg_records_per_tile: avgRecordsPerTile
    } = tileConfig

    // 格式化空间范围
    const formatExtent = (ext) => {
      if (!ext || ext.length !== 4) return '未知'
      return `[${ext[0].toFixed(2)}, ${ext[1].toFixed(2)}, ${ext[2].toFixed(2)}, ${ext[3].toFixed(2)}]`
    }

    // 格式化数字
    const formatNumber = (num) => {
      if (!num) return '未知'
      return num.toLocaleString()
    }

    // 创建自定义输入对话框
    const result = await new Promise((resolve, reject) => {
      const messageBox = ElMessageBox({
        title: '确认预缓存配置',
        message: `
          <div style="text-align: left; line-height: 1.8;">
            <div style="margin-bottom: 16px;">
              <strong style="font-size: 14px;">数据信息：</strong><br>
              <span style="margin-left: 16px;">• 记录数: ${formatNumber(recordCount)} 条</span><br>
              <span style="margin-left: 16px;">• 空间范围: ${formatExtent(extent)}</span><br>
              <span style="margin-left: 16px;">• 坐标系: SRID ${srid || 4326}</span>
            </div>
            <div style="margin-bottom: 16px;">
              <strong style="font-size: 14px;">建议配置：</strong><br>
              <span style="margin-left: 16px;">• 预估瓦片数: ${formatNumber(estimatedTiles)} 个</span><br>
              ${avgRecordsPerTile ? `<span style="margin-left: 16px;">• MaxZoom 层级平均: ${avgRecordsPerTile.toFixed(0)} 条/瓦片</span><br>` : ''}
            </div>
            <div style="margin-bottom: 16px;">
              <strong style="font-size: 14px;">缩放层级配置：</strong>
            </div>
            <div style="display: flex; gap: 16px; align-items: center; margin-bottom: 12px;">
              <div style="flex: 1;">
                <label style="display: block; margin-bottom: 6px; color: #606266; font-size: 13px;">
                  最小缩放层级 (MinZoom)
                </label>
                <input
                  id="min-zoom-input"
                  type="number"
                  value="${calculatedMin}"
                  min="0"
                  max="22"
                  style="width: 100%; padding: 8px 12px; border: 1px solid #dcdfe6; border-radius: 4px; font-size: 14px;"
                />
              </div>
              <div style="flex: 1;">
                <label style="display: block; margin-bottom: 6px; color: #606266; font-size: 13px;">
                  最大缩放层级 (MaxZoom)
                </label>
                <input
                  id="max-zoom-input"
                  type="number"
                  value="${calculatedMax}"
                  min="0"
                  max="22"
                  style="width: 100%; padding: 8px 12px; border: 1px solid #dcdfe6; border-radius: 4px; font-size: 14px;"
                />
              </div>
            </div>
            <div style="color: #909399; font-size: 12px; margin-top: 8px;">
              提示：层级范围 0-22，可设置相同值以生成单一层级瓦片
            </div>
          </div>
        `,
        confirmButtonText: '开始生成',
        cancelButtonText: '取消',
        dangerouslyUseHTMLString: true,
        beforeClose: (action, instance, done) => {
          if (action === 'confirm') {
            const minInput = document.getElementById('min-zoom-input')
            const maxInput = document.getElementById('max-zoom-input')
            const minValue = parseInt(minInput.value, 10)
            const maxValue = parseInt(maxInput.value, 10)

            if (isNaN(minValue) || isNaN(maxValue)) {
              ElMessage.error('请输入有效的数字')
              return
            }

            if (minValue < 0 || maxValue < 0 || minValue > 22 || maxValue > 22) {
              ElMessage.error('层级范围必须在 0-22 之间')
              return
            }

            if (minValue > maxValue) {
              ElMessage.error('MinZoom 不能大于 MaxZoom')
              return
            }

            resolve({ minZoom: minValue, maxZoom: maxValue })
            done()
          } else {
            reject('cancel')
            done()
          }
        }
      })
    })

    const { minZoom, maxZoom } = result

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
    // 注意: createAPIClient 默认 extractData=true，已自动提取 response.data
    const response = await quickViewAPI.getQuickViewStatus(engineId.value, schema.value, table.value)
    const status = response?.status
    const progress = response?.progress // 提取进度数据

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

.toggle-label {
  white-space: nowrap;
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

/* 几何列单元格样式 */
.geometry-cell {
  display: inline-block;
  font-family: 'Courier New', monospace;
  font-size: 12px;
  color: var(--el-color-warning);
  cursor: help;
  padding: 2px 6px;
  background-color: var(--el-fill-color-light);
  border-radius: 3px;
  border: 1px solid var(--el-color-warning-light-5);
  transition: all 0.2s;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}

.geometry-cell:hover {
  background-color: var(--el-color-warning-light-9);
  border-color: var(--el-color-warning-light-3);
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

<style>
/* 几何 tooltip 全局样式（不带 scoped，因为 tooltip 挂载在 body 上）*/
.geometry-tooltip {
  max-width: 600px !important;
}

.geometry-tooltip .el-popper__arrow {
  display: none;
}

.geometry-tooltip-content {
  font-family: 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.6;
  color: #333;
  background-color: #fff;
  padding: 12px;
  border-radius: 4px;
  max-height: 400px;
  overflow: auto;
  word-break: break-all;
  white-space: pre-wrap;
  user-select: text;
  cursor: text;
}

/* 滚动条样式 */
.geometry-tooltip-content::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}

.geometry-tooltip-content::-webkit-scrollbar-thumb {
  background-color: rgba(0, 0, 0, 0.2);
  border-radius: 3px;
}

.geometry-tooltip-content::-webkit-scrollbar-track {
  background-color: rgba(0, 0, 0, 0.05);
}
</style>
