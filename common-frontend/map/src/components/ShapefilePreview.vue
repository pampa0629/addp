<template>
  <div class="shapefile-preview">
    <!-- 警告信息（大文件跳过等） -->
    <el-alert
      v-if="message"
      :title="message"
      type="warning"
      show-icon
      class="alert-message"
    />

    <!-- 顶部紧凑信息栏 -->
    <div class="info-bar">
      <div class="info-tags">
        <el-tag size="small" type="success" effect="plain">{{ geometryType }}</el-tag>
        <el-tooltip v-if="featureCount !== null" :content="t('map.featureCount')" placement="top">
          <el-tag size="small" effect="plain">{{ featureCount }} {{ t('map.features') }}</el-tag>
        </el-tooltip>
        <el-tooltip v-if="truncated" :content="t('map.shapefileTruncated')" placement="top">
          <el-tag size="small" type="warning" effect="plain">{{ t('map.truncated') }}</el-tag>
        </el-tooltip>
        <el-tooltip v-if="codePage" :content="t('map.encoding')" placement="top">
          <el-tag size="small" type="info" effect="plain">{{ codePage }}</el-tag>
        </el-tooltip>
        <el-tooltip v-if="transformEngine" :content="t('map.transformEngine')" placement="top">
          <el-tag size="small" type="info" effect="plain">{{ transformEngine }}</el-tag>
        </el-tooltip>
      </div>
      <div class="info-actions">
        <!-- Bbox tooltip -->
        <el-tooltip v-if="bboxText" :content="bboxText" placement="bottom" raw-content>
          <el-button size="small" text type="info">BBox</el-button>
        </el-tooltip>
        <!-- 投影 tooltip -->
        <el-tooltip v-if="projection" :content="projection" placement="bottom">
          <el-button size="small" text type="info">CRS</el-button>
        </el-tooltip>
        <!-- 底图选择 -->
        <el-select v-if="hasGeoJSON" v-model="baseMapType" size="small" class="base-map-select">
          <el-option
            v-for="item in baseMapOptions"
            :key="item.value"
            :label="item.label"
            :value="item.value"
          />
        </el-select>
      </div>
    </div>

    <!-- 地图区域（优先展示） -->
    <div class="map-area">
      <MapContainer
        v-if="hasGeoJSON && geoFeatures.length > 0"
        :features="geoFeatures"
        :base-map-type="baseMapType"
        height="100%"
      />
      <div v-else-if="!message" class="map-placeholder">
        <el-empty :description="mapStatusMessage || t('map.noGeometryData')" :image-size="60" />
      </div>
    </div>

    <!-- 字段信息（可折叠） -->
    <div v-if="fields.length" class="fields-section">
      <div class="section-header" @click="fieldsExpanded = !fieldsExpanded">
        <span class="section-title">{{ t('map.fieldsTitle') }} ({{ fields.length }})</span>
        <el-icon class="toggle-icon" :class="{ rotated: fieldsExpanded }"><ArrowDown /></el-icon>
      </div>
      <el-table v-if="fieldsExpanded" :data="fields" height="160" size="small" stripe class="fields-table">
        <el-table-column prop="name" :label="t('map.fieldName')" min-width="120" />
        <el-table-column prop="type" :label="t('map.fieldType')" width="100" />
        <el-table-column prop="size" :label="t('map.fieldLength')" width="70" />
        <el-table-column prop="precision" :label="t('map.fieldPrecision')" width="70" />
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowDown } from '@element-plus/icons-vue'
import { useMapConfig } from '../composables/useMapConfig'
import MapContainer from './map/MapContainer.vue'

const { t } = useI18n()

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const fieldsExpanded = ref(false)

const content = computed(() => props.data?.object?.content || {})
const metadata = computed(() => content.value?.metadata || {})
const transformStatus = computed(() => {
  const value = metadata.value?.transform_status
  return typeof value === 'string' ? value : ''
})
const transformEngine = computed(() => {
  const value = metadata.value?.transform_engine
  return typeof value === 'string' ? value : ''
})
const transformMessage = computed(() => {
  const value = metadata.value?.transform_message || metadata.value?.transform_error
  return typeof value === 'string' ? value : ''
})
const sourceSRID = computed(() => Number(metadata.value?.source_srid || 0))

const geometryType = computed(() => metadata.value?.geometry_type || t('map.unknown'))
const featureCount = computed(() => {
  const value = metadata.value?.feature_count
  return Number.isFinite(value) ? value : null
})

const bboxText = computed(() => {
  const raw = metadata.value?.bbox
  if (!Array.isArray(raw) || raw.length !== 4) return ''
  const [minX, minY, maxX, maxY] = raw
  return `Min X: ${formatNumber(minX)}<br>Min Y: ${formatNumber(minY)}<br>Max X: ${formatNumber(maxX)}<br>Max Y: ${formatNumber(maxY)}`
})

const projection = computed(() => {
  const wkt = metadata.value?.projection_wkt || ''
  if (!wkt) return ''
  // 截断过长的投影字符串
  return wkt.length > 200 ? wkt.slice(0, 200) + '...' : wkt
})
const codePage = computed(() => metadata.value?.code_page || '')
const fields = computed(() => {
  const raw = metadata.value?.fields
  if (!Array.isArray(raw)) return []
  return raw.map((item) => ({
    name: item?.name || '',
    type: item?.type || item?.raw_type || '-',
    size: item?.size ?? '',
    precision: item?.precision ?? ''
  }))
})

const message = computed(() => {
  if (typeof content.value?.text === 'string' && content.value.text.trim().length > 0) {
    return content.value.text.trim()
  }
  return ''
})

const shouldSuppressMap = computed(() => {
  if (transformStatus.value === 'unknown_crs' || transformStatus.value === 'unsupported_crs') {
    return true
  }
  return sourceSRID.value > 0 && sourceSRID.value !== 4326 && !metadata.value?.render_bbox
})

const mapStatusMessage = computed(() => {
  if (!shouldSuppressMap.value) return ''
  if (transformMessage.value) return transformMessage.value
  if (transformStatus.value === 'unknown_crs') {
    return t('map.mapSuppressedUnknownCRS')
  }
  if (transformStatus.value === 'unsupported_crs') {
    return t('map.mapSuppressedUnsupportedCRS')
  }
  if (sourceSRID.value > 0 && sourceSRID.value !== 4326) {
    return t('map.mapSuppressedNonWGS84')
  }
  return ''
})

const hasGeoJSON = computed(() => {
  if (shouldSuppressMap.value) return false
  return Boolean(content.value?.geojson || content.value?.GeoJSON)
})
const truncated = computed(() => {
  if (typeof content.value?.truncated === 'boolean') return content.value.truncated
  return Boolean(props.data?.object?.truncated)
})

const geojsonData = computed(() => content.value?.geojson || content.value?.GeoJSON || null)

const geoFeatures = computed(() => {
  if (!geojsonData.value) return []
  try {
    if (geojsonData.value.type === 'FeatureCollection') {
      return geojsonData.value.features || []
    } else if (geojsonData.value.type === 'Feature') {
      return [geojsonData.value]
    } else if (geojsonData.value.type && geojsonData.value.coordinates) {
      return [{ type: 'Feature', geometry: geojsonData.value, properties: {} }]
    }
  } catch (error) {
    console.warn('Shapefile 预览: GeoJSON 解析失败', error)
  }
  return []
})

const { baseMapOptions, defaultBaseMapType, loadMapConfig } = useMapConfig()
const baseMapType = ref('')

watch(
  baseMapOptions,
  (options) => {
    if (options.length > 0 && !baseMapType.value) {
      baseMapType.value = defaultBaseMapType.value || options[0].value
    }
  },
  { immediate: true }
)

onMounted(() => {
  loadMapConfig()
})

function formatNumber(value) {
  const num = Number(value)
  if (!Number.isFinite(num)) return '-'
  return Math.abs(num) >= 1000 ? num.toFixed(2) : num.toFixed(6)
}
</script>

<style scoped>
.shapefile-preview {
  display: flex;
  flex-direction: column;
  gap: 6px;
  height: 100%;
  overflow: hidden;
}

.alert-message {
  flex-shrink: 0;
}

.info-bar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 4px 0;
}

.info-tags {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.info-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.base-map-select {
  width: 130px;
}

.map-area {
  flex: 1;
  min-height: 0;
  border-radius: 6px;
  overflow: hidden;
  border: 1px solid var(--el-border-color-light);
}

.map-placeholder {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-fill-color-lighter);
}

.fields-section {
  flex-shrink: 0;
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  overflow: hidden;
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  cursor: pointer;
  background: var(--el-fill-color);
  user-select: none;
}

.section-header:hover {
  background: var(--el-fill-color-dark);
}

.section-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.toggle-icon {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  transition: transform 0.2s;
}

.toggle-icon.rotated {
  transform: rotate(180deg);
}

.fields-table {
  --el-table-header-bg-color: var(--el-fill-color-dark);
}
</style>
