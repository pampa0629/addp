<template>
  <div class="shapefile-preview">
    <el-alert
      v-if="message"
      :title="message"
      type="warning"
      show-icon
      class="alert-message"
    />

    <div class="summary">
      <div class="summary-item">
        <span class="label">几何类型</span>
        <span class="value">{{ geometryType }}</span>
      </div>
      <div v-if="featureCount !== null" class="summary-item">
        <span class="label">要素总数</span>
        <span class="value">{{ featureCount }}</span>
      </div>
      <div v-if="previewCount !== null" class="summary-item">
        <span class="label">预览条目</span>
        <span class="value">{{ previewCount }}</span>
      </div>
      <div v-if="codePage" class="summary-item">
        <span class="label">编码</span>
        <span class="value">{{ codePage }}</span>
      </div>
    </div>

    <div v-if="bboxRows.length" class="bbox-panel">
      <div class="section-title">边界范围</div>
      <div class="bbox-grid">
        <div v-for="item in bboxRows" :key="item.label" class="bbox-item">
          <span class="label">{{ item.label }}</span>
          <span class="value">{{ item.value }}</span>
        </div>
      </div>
    </div>

    <div v-if="projection || truncated" class="meta-hints">
      <el-tag v-if="projection" size="small" effect="plain" type="info" class="proj-tag">
        {{ projection }}
      </el-tag>
      <el-tag v-if="truncated" size="small" type="warning" effect="plain">
        仅展示部分要素
      </el-tag>
    </div>

    <div v-if="fields.length" class="fields-table">
      <div class="section-title">字段信息</div>
      <el-table :data="fields" height="220" size="small" stripe class="table">
        <el-table-column prop="name" label="字段名" min-width="140" />
        <el-table-column prop="type" label="类型" width="120" />
        <el-table-column prop="size" label="长度" width="80" />
        <el-table-column prop="precision" label="精度" width="80" />
      </el-table>
    </div>

    <div v-if="hasGeoJSON" class="controls">
      <div class="toggle-wrapper">
        <span>地图预览</span>
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

    <MapContainer
      v-if="showMap && geoFeatures.length > 0"
      :features="geoFeatures"
      :base-map-type="baseMapType"
      height="360px"
    />

    <pre v-if="hasGeoJSON" class="json-content" :class="{ collapsed: showMap }">
{{ formattedJson }}
    </pre>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useMapConfig } from '../composables/useMapConfig'
import MapContainer from './map/MapContainer.vue'
import { safeStringify } from '../utils/formatters'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const content = computed(() => props.data?.object?.content || {})
const metadata = computed(() => content.value?.metadata || {})

const geometryType = computed(() => metadata.value?.geometry_type || '未知')
const featureCount = computed(() => {
  const value = metadata.value?.feature_count
  return Number.isFinite(value) ? value : null
})
const previewCount = computed(() => {
  const value = metadata.value?.preview_feature_count
  return Number.isFinite(value) ? value : null
})

const bboxRows = computed(() => {
  const raw = metadata.value?.bbox
  if (!Array.isArray(raw) || raw.length !== 4) return []
  const labels = ['Min X', 'Min Y', 'Max X', 'Max Y']
  return raw.map((value, index) => ({
    label: labels[index],
    value: formatNumber(value)
  }))
})

const projection = computed(() => metadata.value?.projection_wkt || '')
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

const hasGeoJSON = computed(() => Boolean(content.value?.geojson || content.value?.GeoJSON))
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
      return [
        {
          type: 'Feature',
          geometry: geojsonData.value,
          properties: {}
        }
      ]
    }
  } catch (error) {
    console.warn('Shapefile 预览: GeoJSON 解析失败', error)
  }

  return []
})

const formattedJson = computed(() => safeStringify(geojsonData.value))

const { baseMapOptions, defaultBaseMapType, loadMapConfig } = useMapConfig()
const showMap = ref(hasGeoJSON.value)
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

watch(
  hasGeoJSON,
  (value) => {
    if (!value) {
      showMap.value = false
    }
  },
  { immediate: true }
)

function formatNumber(value) {
  const num = Number(value)
  if (!Number.isFinite(num)) return '-'
  if (Math.abs(num) >= 1000) {
    return num.toFixed(2)
  }
  return num.toFixed(6)
}
</script>

<style scoped>
.shapefile-preview {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
}

.alert-message {
  margin-bottom: 4px;
}

.summary {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  padding: 12px 16px;
  background: var(--el-fill-color);
  border-radius: 6px;
}

.summary-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 120px;
}

.summary-item .label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.summary-item .value {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.bbox-panel {
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  padding: 12px 16px;
}

.section-title {
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 8px;
  color: var(--el-text-color-primary);
}

.bbox-grid {
  display: grid;
  gap: 8px;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
}

.bbox-item {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: 12px;
}

.bbox-item .label {
  color: var(--el-text-color-secondary);
}

.bbox-item .value {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, Courier, monospace;
  color: var(--el-text-color-primary);
}

.meta-hints {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.proj-tag {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fields-table {
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  padding: 12px 16px;
}

.table {
  --el-table-header-bg-color: var(--el-fill-color-dark);
}

.controls {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px;
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

.json-content {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, Courier, monospace;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-primary);
  padding: 12px;
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  overflow: auto;
  max-height: 360px;
}

.json-content.collapsed {
  max-height: 200px;
}
</style>
