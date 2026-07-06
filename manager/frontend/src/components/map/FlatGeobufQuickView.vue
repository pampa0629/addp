<template>
  <div class="flatgeobuf-quick-view">
    <div class="quick-view-toolbar">
      <span class="quick-view-title">{{ t('manager.spatialPreview.directFlatGeobufQuickView') }}</span>
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
        <el-button type="primary" size="small" @click="loadFlatGeobuf">
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
        :preserve-view="hasSavedViewState"
        :view-state="viewState"
        height="100%"
        @view-state-change="handleViewStateChange"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { geojson as flatgeobufGeoJSON } from 'flatgeobuf'
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
  },
  viewState: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['view-state-change'])

const { t } = useI18n()
const { baseMapOptions, loadMapConfig } = useMapConfig()

const baseMapType = ref('')
const featureCollection = ref(null)
const loading = ref(false)
const error = ref('')
let requestSeq = 0

const quickViewInfo = computed(() => props.status?.quick_view || props.status?.quickView || {})
const hasSavedViewState = computed(() => {
  const center = props.viewState?.center
  return Array.isArray(center) && center.length >= 2 && Number.isFinite(Number(center[0])) && Number.isFinite(Number(center[1])) && Number.isFinite(Number(props.viewState?.zoom))
})
const flatGeobufURL = computed(() => {
  const raw = String(quickViewInfo.value.flatgeobuf_url || quickViewInfo.value.flatGeobufURL || '').trim()
  return raw.replace(/^\/api\/v1(?=\/manager\/)/, '')
})
const crsPreview = computed(() => ({
  source_srid: quickViewInfo.value.source_srid || quickViewInfo.value.extent_srid,
  source_crs: quickViewInfo.value.source_crs,
  source_crs_definition: quickViewInfo.value.source_crs_definition,
  transform_status: quickViewInfo.value.transform_status
}))
const crsTransform = computed(() => getPreviewCRSTransform(crsPreview.value))
const suppressedMapMessage = computed(() => {
  const status = crsSuppressionStatus(crsTransform.value)
  if (status === 'unknown_crs') return t('map.mapSuppressedUnknownCRS')
  if (status === 'unsupported_crs') return t('map.mapSuppressedUnsupportedCRS')
  return ''
})

const geoFeatures = computed(() => {
  if (!featureCollection.value || crsSuppressionStatus(crsTransform.value)) return []
  try {
    return (featureCollection.value.features || [])
      .map((feature) => ({
        ...feature,
        geometry: transformGeoJSONGeometryToWGS84(feature?.geometry, crsTransform.value)
      }))
      .filter((feature) => feature.geometry)
  } catch (err) {
    console.error('解析快显 FlatGeobuf 失败:', err)
    return []
  }
})

const loadFlatGeobuf = async () => {
  const url = flatGeobufURL.value
  featureCollection.value = null
  error.value = ''
  requestSeq += 1
  const seq = requestSeq
  if (!url) {
    error.value = t('manager.spatialPreview.missingQuickViewURL')
    return
  }
  loading.value = true
  try {
    const data = await client.get(url, { responseType: 'arraybuffer' })
    const bytes = new Uint8Array(data)
    const features = []
    for await (const feature of flatgeobufGeoJSON.deserialize(bytes)) {
      features.push(feature)
    }
    if (seq === requestSeq) {
      featureCollection.value = {
        type: 'FeatureCollection',
        features
      }
    }
  } catch (err) {
    if (seq === requestSeq) {
      error.value = t('manager.spatialPreview.loadQuickViewFailed')
    }
    console.error('加载快显 FlatGeobuf 失败:', err)
  } finally {
    if (seq === requestSeq) {
      loading.value = false
    }
  }
}

const handleViewStateChange = (state) => {
  emit('view-state-change', state)
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

watch(flatGeobufURL, loadFlatGeobuf, { immediate: true })

onMounted(() => {
  loadMapConfig()
})
</script>

<style scoped>
.flatgeobuf-quick-view {
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
