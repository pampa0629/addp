<template>
  <div class="table-preview">
    <!-- 地图预览控制栏（有几何字段时始终显示，switch 在 v-if 块外部避免销毁时报错） -->
    <div v-if="hasGeometry" class="map-controls">
      <div class="toggle-wrapper">
        <span>{{ t('map.preview') }}</span>
        <el-switch v-model="showMap" size="small" />
      </div>
      <el-select v-if="showMap" v-model="baseMapType" size="small" class="base-map-select">
        <el-option
          v-for="item in baseMapOptions"
          :key="item.value"
          :label="item.label"
          :value="item.value"
        />
      </el-select>
    </div>

    <template v-if="hasGeometry && showMap">
      <MapContainer
        ref="mapRef"
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
          :label="col"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ formatCellValue(row[col]) }}
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
      <div class="tip">{{ t('map.maxRows') }}</div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMapConfig } from '../composables/useMapConfig'
import { useResizable } from '../composables/useResizable'
import MapContainer from './map/MapContainer.vue'
import WKT from 'ol/format/WKT'
import GeoJSON from 'ol/format/GeoJSON'

const { t } = useI18n()

const wktFormat = new WKT()
const geojsonFormat = new GeoJSON()

const WKT_TYPE_RE = /^(POINT|LINESTRING|POLYGON|MULTIPOINT|MULTILINESTRING|MULTIPOLYGON|GEOMETRYCOLLECTION)/i

const parseGeometry = (geometryStr) => {
  if (typeof geometryStr !== 'string') return geometryStr
  // 去除 EWKT 的 SRID 前缀，如 "SRID=4326;MULTIPOLYGON(...)"
  const wktStr = geometryStr.replace(/^SRID=\d+;/i, '').trim()
  if (WKT_TYPE_RE.test(wktStr)) {
    const olGeom = wktFormat.readGeometry(wktStr)
    return geojsonFormat.writeGeometryObject(olGeom)
  }
  return JSON.parse(geometryStr)
}

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

const columns = computed(() => props.data?.columns || [])
const rows = computed(() => props.data?.rows || [])
const total = computed(() => props.data?.total || 0)
const geometryColumns = computed(() => props.data?.geometry_columns || [])

const hasGeometry = computed(() => geometryColumns.value.length > 0)
const activeGeometryColumn = computed(() => geometryColumns.value[0] || '')

const escapeHtml = (value) => {
  if (value === null || value === undefined) return ''
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

const formatCellValue = (value) => {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') {
    try {
      return JSON.stringify(value)
    } catch (_error) {
      return '[object]'
    }
  }
  return String(value)
}

const buildPopupContent = (row) => {
  if (!row) {
    return `<div class="map-popup-content">${t('map.noData')}</div>`
  }
  const rowsHtml = (displayColumns.value || [])
    .map((col) => {
      const label = escapeHtml(col)
      const value = escapeHtml(formatCellValue(row[col]))
      return `<div class="map-popup-row"><span class="map-popup-label">${label}</span><span class="map-popup-value">${value}</span></div>`
    })
    .join('')
  return `<div class="map-popup-content">${rowsHtml || `<div class="map-popup-row">${t('map.noFieldData')}</div>`}</div>`
}

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
        return {
          type: 'Feature',
          geometry: parseGeometry(geometryStr),
          properties: row
        }
      } catch (error) {
        console.warn('解析几何数据失败', error)
        return null
      }
    })
    .filter(Boolean)
})

const focusRowOnMap = (row, options = {}) => {
  const rowKey = row?.__rowKey
  if (!rowKey || !mapRef.value || typeof mapRef.value.focusFeature !== 'function') return
  mapRef.value.focusFeature(rowKey, {
    fit: options.fit !== undefined ? options.fit : true,
    openPopup: !!options.openPopup,
    popupContent: options.popupContent,
    coordinate: options.coordinate,
    position: options.position,
    keepPopup: options.keepPopup,
    padding: options.padding
  })
}

const showPopupForRow = (row, payload = {}) => {
  if (!mapRef.value || typeof mapRef.value.showPopup !== 'function') return
  const content = `<div class="map-popup">${buildPopupContent(row)}</div>`
  mapRef.value.showPopup({
    content,
    coordinate: payload.coordinate,
    position: payload.position
  })
}

const getRowKey = (row) => {
  return row?.__rowKey || row?.id || row?.ID || row?._id || row?.uuid || String(Math.random())
}

const handleRowClick = (row) => {
  currentRowKey.value = row?.__rowKey || ''
  if (tableRef.value) {
    tableRef.value.setCurrentRow(row)
  }
  if (hasGeometry.value && showMap.value) {
    if (mapRef.value && typeof mapRef.value.hidePopup === 'function') {
      mapRef.value.hidePopup()
    }
    focusRowOnMap(row, { openPopup: false })
  }
}

const handleFeatureClick = ({ feature, coordinate, position }) => {
  const rowData = feature?.properties
  if (rowData && tableRef.value) {
    currentRowKey.value = rowData.__rowKey || ''
    tableRef.value.setCurrentRow(rowData)
  }
  if (rowData && hasGeometry.value) {
    showPopupForRow(rowData, { coordinate, position })
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

watch(
  showMap,
  (value) => {
    if (!value && mapRef.value && typeof mapRef.value.hidePopup === 'function') {
      mapRef.value.hidePopup()
    }
  }
)

watch(
  () => geoFeatures.value,
  () => {
    if (mapRef.value && typeof mapRef.value.hidePopup === 'function') {
      mapRef.value.hidePopup()
    }
  },
  { deep: true }
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
