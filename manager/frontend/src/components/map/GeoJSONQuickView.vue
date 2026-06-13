<template>
  <div class="geojson-quick-view">
    <div class="quick-view-toolbar">
      <span class="quick-view-title">{{ t('manager.spatialPreview.directGeoJSONQuickView') }}</span>
      <el-select v-model="baseMapType" size="small" class="base-map-select">
        <el-option
          v-for="item in baseMapOptions"
          :key="item.value"
          :label="item.label"
          :value="item.value"
        />
      </el-select>
    </div>

    <div class="quick-view-map">
      <div v-if="loading" class="loading-overlay">
        <div class="loading-content">
          <div class="loading-spinner"></div>
          <p>{{ t('manager.spatialPreview.loadingQuickView') }}</p>
        </div>
      </div>

      <el-empty
        v-else-if="suppressedMapMessage"
        :description="suppressedMapMessage"
        :image-size="72"
        class="map-empty"
      />
      <el-empty
        v-else-if="error"
        :description="error"
        :image-size="72"
        class="map-empty"
      >
        <el-button type="primary" size="small" @click="loadGeoJSON">
          {{ t('manager.vectorTile.retry') }}
        </el-button>
      </el-empty>
      <el-empty
        v-else-if="geoFeatures.length === 0"
        :description="t('manager.spatialPreview.noQuickViewFeatures')"
        :image-size="72"
        class="map-empty"
      />
      <MapContainer
        v-else
        :features="geoFeatures"
        :base-map-type="baseMapType"
        height="100%"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  MapContainer,
  crsSuppressionStatus,
  getPreviewCRSTransform,
  transformGeoJSONGeometryToWGS84,
  useMapConfig
} from '@common-ui-map'
import client from '@/api/client'

const props = defineProps({
  status: {
    type: Object,
    required: true
  }
})

const { t } = useI18n()
const { baseMapOptions, loadMapConfig } = useMapConfig()

const baseMapType = ref('')
const response = ref(null)
const loading = ref(false)
const error = ref('')
let requestSeq = 0

const quickViewInfo = computed(() => props.status?.quick_view || props.status?.quickView || {})
const geoJSONURL = computed(() => {
  const raw = String(quickViewInfo.value.geojson_url || quickViewInfo.value.geoJSONURL || '').trim()
  return raw.replace(/^\/api\/v1(?=\/manager\/)/, '')
})
const crsPreview = computed(() => {
  const value = tableResponseData.value || {}
  return {
    ...value,
    source_srid: value.source_srid || quickViewInfo.value.source_srid || quickViewInfo.value.extent_srid,
    source_crs: value.source_crs,
    source_crs_definition: value.source_crs_definition,
    transform_status: value.transform_status
  }
})
const crsTransform = computed(() => getPreviewCRSTransform(crsPreview.value))
const suppressedMapMessage = computed(() => {
  const status = crsSuppressionStatus(crsTransform.value)
  if (status === 'unknown_crs') return t('map.mapSuppressedUnknownCRS')
  if (status === 'unsupported_crs') return t('map.mapSuppressedUnsupportedCRS')
  return ''
})

const parseGeometryValue = (value) => {
  if (!value) return null
  if (typeof value === 'object') return value
  if (typeof value !== 'string') return null
  try {
    return JSON.parse(value)
  } catch (_error) {
    return null
  }
}

const tableResponseData = computed(() => response.value?.data || response.value || {})

const geojsonData = computed(() => {
  if (response.value?.type === 'FeatureCollection') return response.value
  if (response.value?.type === 'Feature') return response.value
  if (response.value?.geojson) return response.value.geojson
  const data = tableResponseData.value
  const rows = Array.isArray(data?.rows) ? data.rows : []
  const geometryColumn = String(
    quickViewInfo.value.geometry_column ||
    data?.geometry_column ||
    (Array.isArray(data?.geometry_columns) ? data.geometry_columns[0] : '') ||
    ''
  ).trim()
  if (!rows.length || !geometryColumn) return null
  return {
    type: 'FeatureCollection',
    features: rows
      .map((row) => {
        const geometry = parseGeometryValue(row?.[geometryColumn])
        if (!geometry?.type) return null
        const properties = { ...row }
        delete properties[geometryColumn]
        return {
          type: 'Feature',
          geometry,
          properties
        }
      })
      .filter(Boolean)
  }
})

const geoFeatures = computed(() => {
  if (!geojsonData.value || crsSuppressionStatus(crsTransform.value)) return []
  try {
    if (geojsonData.value.type === 'FeatureCollection') {
      return (geojsonData.value.features || [])
        .map((feature) => ({
          ...feature,
          geometry: transformGeoJSONGeometryToWGS84(feature?.geometry, crsTransform.value)
        }))
        .filter((feature) => feature.geometry)
    }
    if (geojsonData.value.type === 'Feature') {
      const geometry = transformGeoJSONGeometryToWGS84(geojsonData.value.geometry, crsTransform.value)
      return geometry ? [{ ...geojsonData.value, geometry }] : []
    }
    if (geojsonData.value.type && geojsonData.value.coordinates) {
      const geometry = transformGeoJSONGeometryToWGS84(geojsonData.value, crsTransform.value)
      return geometry ? [{ type: 'Feature', geometry, properties: {} }] : []
    }
  } catch (err) {
    console.error('解析快显 GeoJSON 失败:', err)
  }
  return []
})

const loadGeoJSON = async () => {
  const url = geoJSONURL.value
  response.value = null
  error.value = ''
  requestSeq += 1
  const seq = requestSeq
  if (!url) {
    error.value = t('manager.spatialPreview.missingQuickViewURL')
    return
  }
  loading.value = true
  try {
    const data = await client.get(url)
    if (seq === requestSeq) {
      response.value = data
    }
  } catch (err) {
    if (seq === requestSeq) {
      error.value = t('manager.spatialPreview.loadQuickViewFailed')
    }
    console.error('加载快显 GeoJSON 失败:', err)
  } finally {
    if (seq === requestSeq) {
      loading.value = false
    }
  }
}

watch(
  baseMapOptions,
  (options) => {
    if (options.length > 0 && !baseMapType.value) {
      baseMapType.value = options[0].value
    }
  },
  { immediate: true }
)

watch(geoJSONURL, loadGeoJSON, { immediate: true })

onMounted(() => {
  loadMapConfig()
})
</script>

<style scoped>
.geojson-quick-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 320px;
}

.quick-view-toolbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--el-border-color-light);
  background: var(--addp-bg-primary);
}

.quick-view-title {
  font-size: 13px;
  color: var(--addp-text-secondary);
}

.base-map-select {
  width: 168px;
}

.quick-view-map {
  position: relative;
  flex: 1;
  min-height: 0;
}

.map-empty {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}

.loading-overlay {
  position: absolute;
  inset: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(255, 255, 255, 0.88);
}

.loading-content {
  text-align: center;
  color: var(--addp-text-secondary);
}

.loading-spinner {
  width: 32px;
  height: 32px;
  margin: 0 auto 10px;
  border: 3px solid var(--el-border-color-light);
  border-top-color: var(--el-color-primary);
  border-radius: 50%;
  animation: spin 0.9s linear infinite;
}

.loading-content p {
  margin: 0;
  font-size: 13px;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
