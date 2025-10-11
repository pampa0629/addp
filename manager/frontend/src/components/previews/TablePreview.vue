<template>
  <div class="table-preview">
    <!-- 地图预览 -->
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
      </div>

      <MapContainer
        :features="geoFeatures"
        :base-map-type="baseMapType"
        :height="mapHeight + 'px'"
        @feature-click="handleFeatureClick"
      />

      <div class="map-splitter" @mousedown="startMapResize"></div>
    </template>

    <!-- 表格区域 -->
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
import { ref, computed, watch, onMounted } from 'vue'
import { useMapConfig } from '@/composables/useMapConfig'
import { useResizable } from '@/composables/useResizable'
import MapContainer from '@/components/map/MapContainer.vue'

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
const showMap = ref(true)
const baseMapType = ref('')
const currentRowKey = ref('')
const currentPage = ref(1)
const pageSize = ref(10)

const columns = computed(() => props.data?.columns || [])
const rows = computed(() => props.data?.rows || [])
const total = computed(() => props.data?.total || 0)
const geometryColumns = computed(() => props.data?.geometry_columns || [])

const hasGeometry = computed(() => geometryColumns.value.length > 0)
const activeGeometryColumn = computed(() => geometryColumns.value[0] || '')

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

// 转换为 GeoJSON Features
const geoFeatures = computed(() => {
  if (!hasGeometry.value || !activeGeometryColumn.value) return []
  const column = activeGeometryColumn.value
  return tableData.value
    .map((row) => {
      const geometryStr = row[column]
      if (!geometryStr) return null
      try {
        const geometry = typeof geometryStr === 'string' ? JSON.parse(geometryStr) : geometryStr
        return {
          type: 'Feature',
          geometry,
          properties: row
        }
      } catch (error) {
        console.warn('解析 GeoJSON 失败', error)
        return null
      }
    })
    .filter(Boolean)
})

const getRowKey = (row) => {
  return row?.__rowKey || row?.id || row?.ID || row?._id || row?.uuid || String(Math.random())
}

const handleRowClick = (row) => {
  currentRowKey.value = row?.__rowKey || ''
  if (tableRef.value) {
    tableRef.value.setCurrentRow(row)
  }
}

const handleFeatureClick = ({ feature }) => {
  const rowData = feature?.properties
  if (rowData && tableRef.value) {
    currentRowKey.value = rowData.__rowKey || ''
    tableRef.value.setCurrentRow(rowData)
  }
}

const handlePageChange = (page) => {
  currentPage.value = page
  emit('page-change', page)
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

onMounted(() => {
  loadMapConfig()
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
