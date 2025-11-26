<template>
  <div class="table-preview">
    <!-- 地图预览 (统一使用 MVT 瓦片) -->
    <template v-if="hasGeometry && showMap">
      <div class="map-controls">
        <div class="toggle-wrapper">
          <span>地图预览</span>
          <el-switch v-model="showMap" size="small" />
        </div>
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
          {{ quickViewStatus === 'completed' ? '预缓存已启用' : quickViewStatus === 'generating' ? '生成中...' : '启用预缓存' }}
        </el-button>
      </div>

      <!-- 统一使用 MVT 瓦片预览（所有空间表）-->
      <div class="map-container" :style="{ height: mapHeight + 'px' }">
        <VectorTilePreview
          ref="mapRef"
          :resource-id="resourceId"
          :schema="schema"
          :table="table"
          :geom="activeGeometryColumn"
        />
      </div>

      <div class="map-splitter" @mousedown="startMapResize"></div>
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
          :label="col"
          show-overflow-tooltip
        />
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
import { useMapConfig } from '@/composables/useMapConfig'
import { useResizable } from '@/composables/useResizable'
import VectorTilePreview from '@/components/map/VectorTilePreview.vue'
import { dataExplorerAPI } from '@/api/dataExplorer'
import { quickViewAPI } from '@/api/quickView'
import { ElMessage } from 'element-plus'

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
const showMap = ref(true)
const baseMapType = ref('')
const currentRowKey = ref('')
const currentPage = ref(1)
const pageSize = ref(10)

// Pre-Cache state
const quickViewLoading = ref(false)
const quickViewStatus = ref('none') // 'none' | 'generating' | 'completed' | 'failed'
let quickViewPollingTimer = null

const columns = computed(() => props.data?.columns || [])
const rows = computed(() => props.data?.rows || [])
const total = computed(() => props.data?.total || 0)
const geometryColumns = computed(() => props.data?.geometry_columns || [])

const hasGeometry = computed(() => geometryColumns.value.length > 0)
const activeGeometryColumn = computed(() => geometryColumns.value[0] || '')

// 资源信息（用于 MVT 预览）
const resourceId = computed(() => props.data?.resourceId)
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

// 生成行键
const tableData = computed(() => {
  const baseKey = `${props.data?.resourceId || 'res'}-${props.data?.schema || 'schema'}-${props.data?.table || 'table'}`
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
        resourceId.value,
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

// Pre-Cache 处理函数
const handleQuickView = async () => {
  if (!resourceId.value || !schema.value || !table.value) {
    ElMessage.warning('缺少必要参数，无法启用预缓存')
    return
  }

  if (quickViewStatus.value === 'generating') {
    ElMessage.info('预缓存正在生成中，请稍后')
    return
  }

  try {
    quickViewLoading.value = true
    await quickViewAPI.triggerQuickView(resourceId.value, schema.value, table.value, {
      max_zoom: 18,
      concurrency: 10,
      priority: 'default'
    })
    quickViewStatus.value = 'generating'
    ElMessage.success('预缓存任务已启动，正在后台生成缓存')

    // 开始轮询状态
    startQuickViewPolling()
  } catch (error) {
    console.error('启用预缓存失败:', error)
    ElMessage.error(error.response?.data?.error || '启用预缓存失败')
  } finally {
    quickViewLoading.value = false
  }
}

// 获取预缓存状态
const fetchQuickViewStatus = async () => {
  if (!resourceId.value || !schema.value || !table.value) {
    return
  }

  try {
    const response = await quickViewAPI.getQuickViewStatus(resourceId.value, schema.value, table.value)
    const status = response.data?.status

    if (status) {
      quickViewStatus.value = status

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
    }
    // 其他错误不处理，避免频繁弹窗
  }
}

// 开始轮询预缓存状态
const startQuickViewPolling = () => {
  stopQuickViewPolling() // 先清除旧的定时器

  quickViewPollingTimer = setInterval(() => {
    fetchQuickViewStatus()
  }, 3000) // 每3秒查询一次
}

// 停止轮询
const stopQuickViewPolling = () => {
  if (quickViewPollingTimer) {
    clearInterval(quickViewPollingTimer)
    quickViewPollingTimer = null
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
  () => [resourceId.value, schema.value, table.value],
  () => {
    if (hasGeometry.value && resourceId.value && schema.value && table.value) {
      fetchQuickViewStatus()
    }
  },
  { immediate: true }
)

onMounted(() => {
  loadMapConfig()

  // 初始化时获取预缓存状态
  if (hasGeometry.value && resourceId.value && schema.value && table.value) {
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
</style>
