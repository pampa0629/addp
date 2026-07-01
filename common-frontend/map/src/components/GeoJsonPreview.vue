<template>
  <div class="geojson-preview">
    <div class="controls">
      <div class="toggle-wrapper">
        <span>{{ t('map.preview') }}</span>
        <el-switch v-model="showMap" size="small" />
      </div>
      <div v-if="showMap" class="base-map-control">
        <el-select v-model="baseMapType" size="small" class="base-map-select">
          <el-option
            v-for="item in baseMapOptions"
            :key="item.value"
            :label="item.label"
            :value="item.value"
          />
        </el-select>
        <el-tooltip v-if="baseMapNotice" :content="baseMapNotice" placement="top">
          <el-tag size="small" type="warning" effect="plain" class="base-map-policy-tag">
            {{ t('map.businessBrowseMode') }}
          </el-tag>
        </el-tooltip>
      </div>
    </div>

    <MapContainer
      v-if="showMap && geoFeatures.length > 0"
      :features="geoFeatures"
      :base-map-type="baseMapType"
      :preserve-view="hasSavedViewState"
      :view-state="viewState"
      height="360px"
      @view-state-change="handleViewStateChange"
    />
    <div v-else-if="showMap" class="map-placeholder">
      <el-empty :description="suppressedMapMessage || t('map.noGeometryData')" :image-size="60" />
    </div>

    <pre class="json-content" :class="{ collapsed: showMap }">{{ formattedJson }}</pre>

    <div v-if="truncated" class="truncate-tip">{{ t('map.truncated') }}</div>

  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMapConfig } from '../composables/useMapConfig'
import MapContainer from './map/MapContainer.vue'
import { safeStringify } from '../utils/formatters'
import {
  crsSuppressionStatus,
  getPreviewCRSTransform,
  transformGeoJSONGeometryToWGS84
} from '../utils/crsRegistry'

const { t } = useI18n()

const props = defineProps({
  data: {
    type: Object,
    required: true
  },
  viewState: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['view-state-change'])

const { baseMapOptions, defaultBaseMapType, getBaseMapProfile, loadMapConfig } = useMapConfig()

const showMap = ref(true)
const baseMapType = ref('')
const selectedBaseMapProfile = computed(() => getBaseMapProfile(baseMapType.value))
const baseMapNotice = computed(() => {
  if (selectedBaseMapProfile.value?.coordinate_policy === 'gcj02') {
    return t('map.gcj02DisplayNotice')
  }
  return ''
})

const objectData = computed(() => props.data?.object || {})

const hasSavedViewState = computed(() => {
  const center = props.viewState?.center
  return Array.isArray(center) &&
    center.length >= 2 &&
    Number.isFinite(Number(center[0])) &&
    Number.isFinite(Number(center[1])) &&
    Number.isFinite(Number(props.viewState?.zoom))
})

const geojsonData = computed(() => {
  return objectData.value?.content?.geojson || objectData.value?.content?.GeoJSON || null
})

const crsTransform = computed(() => getPreviewCRSTransform(props.data))
const suppressedMapMessage = computed(() => {
  const status = crsSuppressionStatus(crsTransform.value)
  if (status === 'unknown_crs') return t('map.mapSuppressedUnknownCRS')
  if (status === 'unsupported_crs') return t('map.mapSuppressedUnsupportedCRS')
  return ''
})

const truncated = computed(() => {
  return objectData.value?.content?.truncated || objectData.value?.truncated || false
})

const geoFeatures = computed(() => {
  if (!geojsonData.value) return []
  if (crsSuppressionStatus(crsTransform.value)) return []

  try {
    if (geojsonData.value.type === 'FeatureCollection') {
      return (geojsonData.value.features || [])
        .map((feature) => ({
          ...feature,
          geometry: transformGeoJSONGeometryToWGS84(feature?.geometry, crsTransform.value)
        }))
        .filter((feature) => feature.geometry)
    } else if (geojsonData.value.type === 'Feature') {
      const geometry = transformGeoJSONGeometryToWGS84(geojsonData.value.geometry, crsTransform.value)
      return geometry ? [{ ...geojsonData.value, geometry }] : []
    } else if (geojsonData.value.type && geojsonData.value.coordinates) {
      const geometry = transformGeoJSONGeometryToWGS84(geojsonData.value, crsTransform.value)
      if (!geometry) return []
      return [
        {
          type: 'Feature',
          geometry,
          properties: {}
        }
      ]
    }
  } catch (error) {
    console.error('解析 GeoJSON 失败', error)
  }

  return []
})

const formattedJson = computed(() => {
  return safeStringify(geojsonData.value)
})

const handleViewStateChange = (state) => {
  emit('view-state-change', state)
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
.geojson-preview {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
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

.map-placeholder {
  height: 360px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  background: var(--addp-bg-primary);
}

.base-map-select {
  min-width: 160px;
}

.base-map-control {
  display: flex;
  align-items: center;
  gap: 8px;
}

.base-map-policy-tag {
  flex: 0 0 auto;
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
  max-height: 400px;
}

.json-content.collapsed {
  max-height: 200px;
}

.truncate-tip {
  font-size: 12px;
  color: var(--el-color-primary);
  text-align: center;
}

/* 元数据展示区域 */
.metadata-section {
  padding: 16px;
  background: var(--el-bg-color-page);
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
}
</style>
