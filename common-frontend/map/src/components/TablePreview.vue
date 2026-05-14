<template>
  <div ref="tablePreviewRef" class="table-preview">
    <!-- 地图预览控制栏（有几何字段时始终显示，switch 在 v-if 块外部避免销毁时报错） -->
    <div v-if="hasGeometry" ref="mapControlsRef" class="map-controls">
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
      <div ref="mapWrapperRef" class="map-wrapper" :style="{ height: mapHeight + 'px' }">
        <MapContainer
          v-if="geoFeatures.length > 0"
          ref="mapRef"
          :features="geoFeatures"
          :base-map-type="baseMapType"
          height="100%"
          @feature-click="handleFeatureClick"
        />
        <div v-else class="map-placeholder">
          <el-empty :description="suppressedMapMessage || t('map.noGeometryData')" :image-size="60" />
        </div>
      </div>

      <div ref="splitterRef" class="map-splitter" @mousedown="startMapResize"></div>
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
    <div v-if="total > 0" ref="paginationRef" class="pagination">
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

    <!-- 格式附加属性（可折叠） -->
    <div v-if="shapefileMetaItems.length > 0" ref="shapefileMetaRef" class="shapefile-meta">
      <div class="shapefile-meta-header" @click="shapefileMetaExpanded = !shapefileMetaExpanded">
        <span class="shapefile-meta-title">{{ t('map.formatAttributes') }}</span>
        <el-icon class="shapefile-meta-icon" :class="{ expanded: shapefileMetaExpanded }">
          <ArrowDown />
        </el-icon>
      </div>
      <el-descriptions
        v-show="shapefileMetaExpanded"
        :column="2"
        border
        size="small"
        class="shapefile-meta-desc"
      >
        <el-descriptions-item
          v-for="item in shapefileMetaItems"
          :key="item.key"
          :label="item.key"
        >{{ item.value }}</el-descriptions-item>
      </el-descriptions>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowDown } from '@element-plus/icons-vue'
import { useMapConfig } from '../composables/useMapConfig'
import { useResizable } from '../composables/useResizable'
import MapContainer from './map/MapContainer.vue'
import WKT from 'ol/format/WKT'
import GeoJSON from 'ol/format/GeoJSON'

const { t } = useI18n()

const wktFormat = new WKT()
const geojsonFormat = new GeoJSON()
const DEFAULT_MAP_HEIGHT = 520
const MIN_MAP_HEIGHT = 240
const MIN_TABLE_HEIGHT = 80
const CONTAINER_GAP = 12

const WKT_TYPE_RE = /^(POINT|LINESTRING|POLYGON|MULTIPOINT|MULTILINESTRING|MULTIPOLYGON|GEOMETRYCOLLECTION)/i

const isValidGeoJSONGeometry = (geom) => {
  if (!geom || typeof geom !== 'object') return false
  if (typeof geom.type !== 'string') return false
  const validTypes = new Set([
    'Point',
    'MultiPoint',
    'LineString',
    'MultiLineString',
    'Polygon',
    'MultiPolygon',
    'GeometryCollection'
  ])
  if (!validTypes.has(geom.type)) return false
  if (geom.type === 'GeometryCollection') {
    return Array.isArray(geom.geometries)
  }
  return geom.coordinates !== undefined
}

const parseGeometry = (rawGeometry) => {
  if (rawGeometry === null || rawGeometry === undefined) return null

  // 已是对象时，兼容 Geometry / Feature / FeatureCollection
  if (typeof rawGeometry === 'object') {
    if (isValidGeoJSONGeometry(rawGeometry)) {
      return rawGeometry
    }
    if (rawGeometry.type === 'Feature' && isValidGeoJSONGeometry(rawGeometry.geometry)) {
      return rawGeometry.geometry
    }
    if (rawGeometry.type === 'FeatureCollection' && Array.isArray(rawGeometry.features)) {
      const firstGeometry = rawGeometry.features.find((f) => isValidGeoJSONGeometry(f?.geometry))?.geometry
      return firstGeometry || null
    }
    return null
  }

  if (typeof rawGeometry !== 'string') return null

  // 去除 EWKT 的 SRID 前缀，如 "SRID=4326;MULTIPOLYGON(...)"
  const wktStr = rawGeometry.replace(/^SRID=\d+;/i, '').trim()
  if (WKT_TYPE_RE.test(wktStr)) {
    const olGeom = wktFormat.readGeometry(wktStr)
    return geojsonFormat.writeGeometryObject(olGeom)
  }

  const parsed = JSON.parse(rawGeometry)
  if (isValidGeoJSONGeometry(parsed)) return parsed
  if (parsed?.type === 'Feature' && isValidGeoJSONGeometry(parsed.geometry)) return parsed.geometry
  return null
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

const tablePreviewRef = ref(null)
const mapControlsRef = ref(null)
const mapWrapperRef = ref(null)
const splitterRef = ref(null)
const paginationRef = ref(null)
const shapefileMetaRef = ref(null)

const getSectionHeight = (target, fallback = 0) => target?.value?.offsetHeight || fallback

const getVisibleSectionCount = () => {
  let count = 1
  if (hasGeometry.value) count += 1
  if (hasGeometry.value && showMap.value) count += 2
  if (total.value > 0) count += 1
  if (shapefileMetaItems.value.length > 0) count += 1
  return count
}

const getMaxMapHeight = () => {
  const containerH = tablePreviewRef.value?.clientHeight || 0
  const viewportH = typeof window !== 'undefined' ? window.innerHeight : DEFAULT_MAP_HEIGHT
  const base = containerH > 0 ? containerH : viewportH
  const controlsH = hasGeometry.value ? getSectionHeight(mapControlsRef, 44) : 0
  const splitterH = hasGeometry.value && showMap.value ? getSectionHeight(splitterRef, 14) : 0
  const paginationH = total.value > 0 ? getSectionHeight(paginationRef, 40) : 0
  const metaH = shapefileMetaItems.value.length > 0 ? getSectionHeight(shapefileMetaRef) : 0
  const gapTotal = Math.max(0, getVisibleSectionCount() - 1) * CONTAINER_GAP
  const available = base - controlsH - splitterH - paginationH - metaH - gapTotal - MIN_TABLE_HEIGHT
  return Math.max(160, available)
}
const { size: mapHeight, startResize: startMapResizeInternal } = useResizable(DEFAULT_MAP_HEIGHT, MIN_MAP_HEIGHT, getMaxMapHeight, 'vertical')

const tableRef = ref(null)
const mapRef = ref(null)
const showMap = ref(true)
const baseMapType = ref('')
const currentRowKey = ref('')
const currentPage = ref(1)
const pageSize = ref(10)
const shapefileMetaExpanded = ref(false)
const hasManualMapResize = ref(false)

let resizeObserver = null

const getDefaultMapHeight = () => {
  const containerH = tablePreviewRef.value?.clientHeight || 0
  const viewportH = typeof window !== 'undefined' ? window.innerHeight : DEFAULT_MAP_HEIGHT
  const base = containerH > 300 ? containerH : viewportH
  return Math.max(MIN_MAP_HEIGHT, Math.round(base * 0.58))
}

const syncMapHeight = (forceDefault = false) => {
  if (!hasGeometry.value || !showMap.value) return

  const maxHeight = getMaxMapHeight()
  const lowerBound = Math.min(MIN_MAP_HEIGHT, maxHeight)

  if (forceDefault || !hasManualMapResize.value) {
    mapHeight.value = Math.min(maxHeight, getDefaultMapHeight())
    return
  }

  if (mapHeight.value > maxHeight) {
    mapHeight.value = maxHeight
    return
  }

  if (mapHeight.value < lowerBound) {
    mapHeight.value = lowerBound
  }
}

const scheduleMapHeightSync = (forceDefault = false) => {
  nextTick(() => {
    syncMapHeight(forceDefault)
  })
}

const handleWindowResize = () => {
  scheduleMapHeightSync(false)
}

const startMapResize = (event) => {
  hasManualMapResize.value = true
  startMapResizeInternal(event)
}

const columns = computed(() => props.data?.columns || [])
const rows = computed(() => props.data?.rows || [])
const total = computed(() => props.data?.total || 0)
const geometryColumns = computed(() => props.data?.geometry_columns || [])
const renderGeometryColumns = computed(() => props.data?.render_geometry_columns || {})
const previewSRID = computed(() => Number(props.data?.srid || 0))

const hiddenMetadataKeys = new Set([
  'components',
  'component',
  'component_descriptors',
  'organization',
  'preview_material',
  'preview_renderer',
  'frontend_renderer',
  'required_parts',
  'optional_parts',
  'format',
  'data_type'
])

const formatMetadataValue = (value) => {
  if (value === null || value === undefined) return ''
  if (Array.isArray(value)) {
    const primitiveValues = value.filter(item => item === null || typeof item !== 'object')
    if (primitiveValues.length !== value.length) return ''
    return primitiveValues.map(item => String(item)).join(', ')
  }
  if (typeof value === 'object') {
    return ''
  }
  return String(value)
}

// 格式附加属性：只展示可读的标量元数据，结构化组织信息由外层通用控件承载。
const shapefileMetaItems = computed(() => {
  const meta = props.data?.object?.content?.metadata
  if (!meta || typeof meta !== 'object') return []
  return Object.entries(meta)
    .filter(([key]) => !hiddenMetadataKeys.has(String(key)))
    .map(([key, value]) => ({
      key,
      value: formatMetadataValue(value)
    }))
    .filter(item => item.value !== '')
})

const hasGeometry = computed(() => geometryColumns.value.length > 0)
const activeGeometryColumn = computed(() => geometryColumns.value[0] || '')
const activeRenderGeometryColumn = computed(() => {
  const column = activeGeometryColumn.value
  if (!column) return ''
  return renderGeometryColumns.value?.[column] || ''
})
const transformStatus = computed(() => {
  const value = props.data?.object?.content?.metadata?.transform_status
  return typeof value === 'string' ? value : ''
})
const transformMessage = computed(() => {
  const value = props.data?.object?.content?.metadata?.transform_message || props.data?.object?.content?.metadata?.transform_error
  return typeof value === 'string' ? value : ''
})
const shouldSuppressRawGeometryMap = computed(() => {
  if (activeRenderGeometryColumn.value) return false
  if (transformStatus.value === 'unknown_crs' || transformStatus.value === 'unsupported_crs') {
    return true
  }
  return previewSRID.value > 0 && previewSRID.value !== 4326
})
const suppressedMapMessage = computed(() => {
  if (!shouldSuppressRawGeometryMap.value) return ''
  if (transformMessage.value) return transformMessage.value
  if (transformStatus.value === 'unknown_crs') return t('map.mapSuppressedUnknownCRS')
  if (transformStatus.value === 'unsupported_crs') return t('map.mapSuppressedUnsupportedCRS')
  if (previewSRID.value > 0 && previewSRID.value !== 4326) return t('map.mapSuppressedNonWGS84')
  return ''
})

const buildFeatureProperties = (row) => {
  const properties = { ...row }
  geometryColumns.value.forEach((column) => {
    delete properties[column]
    const renderColumn = renderGeometryColumns.value?.[column]
    if (renderColumn) {
      delete properties[renderColumn]
    }
  })
  // OpenLayers 默认使用 "geometry" 作为几何属性名，这个键必须移除，否则会覆盖真实几何对象。
  delete properties.geometry
  return properties
}

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
  const renderGeometrySet = new Set(Object.values(renderGeometryColumns.value || {}))
  const filtered = columns.value.filter((col) => !geometrySet.has(col) && !renderGeometrySet.has(col))
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
  if (shouldSuppressRawGeometryMap.value) return []
  const column = activeGeometryColumn.value
  return tableData.value
    .map((row) => {
      const renderColumn = activeRenderGeometryColumn.value
      const rawGeometry = renderColumn ? row[renderColumn] : row[column]
      if (rawGeometry === null || rawGeometry === undefined) return null
      try {
        const geometry = parseGeometry(rawGeometry)
        if (!geometry) return null
        return {
          type: 'Feature',
          geometry,
          properties: buildFeatureProperties(row)
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
  if (!rowData) return
  const rowKey = rowData.__rowKey || ''
  currentRowKey.value = rowKey
  // 必须传入 tableData 中的对象引用，否则 el-table 内部访问 emitsOptions 会报错
  if (tableRef.value && rowKey) {
    const matched = tableData.value.find((r) => r.__rowKey === rowKey)
    if (matched) {
      tableRef.value.setCurrentRow(matched)
    }
  }
  if (hasGeometry.value) {
    showPopupForRow(rowData, { coordinate, position })
  }
}

const handlePageChange = (page) => {
  currentPage.value = page
  emit('page-change', page)
}

watch(
  () => props.data?.page,
  (page) => {
    currentPage.value = Number(page) > 0 ? Number(page) : 1
  },
  { immediate: true }
)

watch(
  () => props.data?.page_size,
  (size) => {
    const parsedSize = Number(size)
    pageSize.value = parsedSize > 0 ? parsedSize : Math.max(rows.value.length, 10)
  },
  { immediate: true }
)

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
    if (value) {
      scheduleMapHeightSync(false)
    }
  }
)

watch(
  [hasGeometry, total, shapefileMetaExpanded, () => shapefileMetaItems.value.length],
  () => {
    scheduleMapHeightSync(false)
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
  if (typeof window !== 'undefined') {
    window.addEventListener('resize', handleWindowResize)
  }

  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      syncMapHeight(false)
    })
    ;[
      tablePreviewRef.value,
      mapControlsRef.value,
      paginationRef.value,
      shapefileMetaRef.value
    ].forEach((element) => {
      if (element) {
        resizeObserver.observe(element)
      }
    })
  }

  scheduleMapHeightSync(true)
})

onBeforeUnmount(() => {
  if (resizeObserver) {
    resizeObserver.disconnect()
    resizeObserver = null
  }
  if (typeof window !== 'undefined') {
    window.removeEventListener('resize', handleWindowResize)
  }
})
</script>

<style scoped>
.table-preview {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
  min-height: 0;
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

.map-wrapper {
  flex: 0 0 auto;
  min-height: 160px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 4px;
  overflow: hidden;
}

.map-placeholder {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-fill-color-lighter);
}

.map-splitter {
  height: 14px;
  cursor: row-resize;
  position: relative;
  margin: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.map-splitter::before {
  content: '⋯';
  font-size: 16px;
  line-height: 1;
  color: var(--el-color-primary-light-5);
  letter-spacing: 2px;
}

.map-splitter::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  height: 3px;
  background: var(--el-color-primary-light-7);
  border-radius: 2px;
}

.map-splitter:hover::after,
body.is-v-resizing .map-splitter::after {
  background: var(--el-color-primary);
}

.map-splitter:hover::before,
body.is-v-resizing .map-splitter::before {
  color: var(--el-color-primary);
}

.table-wrapper {
  flex: 1 1 auto;
  min-height: 80px;
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

.shapefile-meta {
  flex-shrink: 0;
  border: 1px solid var(--el-border-color-light);
  border-radius: 4px;
  overflow: hidden;
}

.shapefile-meta-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  cursor: pointer;
  background: var(--el-fill-color-light);
  user-select: none;
}

.shapefile-meta-title {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.shapefile-meta-icon {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  transition: transform 0.2s;
}

.shapefile-meta-icon.expanded {
  transform: rotate(180deg);
}

.shapefile-meta-desc {
  padding: 8px;
}
</style>
