<template>
  <div class="map-container" :style="{ height }">
    <component
      :is="mapRenderer"
      v-if="mapRenderer"
      :features="features"
      :config="mapConfig"
      :base-type="baseMapType"
      :base-map-profile="baseMapProfile"
      :preserve-view="preserveView"
      :popup-options="mapPopupOptions"
      ref="rendererRef"
      @feature-click="handleFeatureClick"
    />
    <div v-else class="map-placeholder">
      <el-empty :description="t('map.mapServiceNotConfigured')" />
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMapConfig } from '../../composables/useMapConfig'
import GaodeMapRenderer from './GaodeMapRenderer.vue'
import OpenLayersRenderer from './OpenLayersRenderer.vue'

const props = defineProps({
  features: {
    type: Array,
    default: () => []
  },
  baseMapType: {
    type: String,
    default: ''
  },
  height: {
    type: String,
    default: '360px'
  },
  preserveView: {
    type: Boolean,
    default: false
  },
  popupOptions: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['feature-click'])

const rendererRef = ref(null)

const { t } = useI18n()
const { mapConfig, GAODE_BASE_MAP_VALUE, getBaseMapProfile } = useMapConfig()

const baseMapProfile = computed(() => getBaseMapProfile(props.baseMapType))
const mapPopupOptions = computed(() => ({
  ...props.popupOptions,
  labels: {
    id: t('map.featureId'),
    unknown: t('map.unknown'),
    unknownGeometry: t('map.unknownGeometry'),
    nullValue: t('map.nullValue'),
    noAttributes: t('map.noFieldData'),
    ...(props.popupOptions.labels || {})
  },
  geometryTypeLabels: {
    Point: t('map.geometryPoint'),
    MultiPoint: t('map.geometryMultiPoint'),
    LineString: t('map.geometryLineString'),
    MultiLineString: t('map.geometryMultiLineString'),
    Polygon: t('map.geometryPolygon'),
    MultiPolygon: t('map.geometryMultiPolygon'),
    ...(props.popupOptions.geometryTypeLabels || {})
  }
}))

const mapRenderer = computed(() => {
  if (props.baseMapType === GAODE_BASE_MAP_VALUE) {
    return GaodeMapRenderer
  }
  if (['tiandituVector', 'tiandituImage'].includes(props.baseMapType)) {
    return OpenLayersRenderer
  }
  return null
})

const handleFeatureClick = (event) => {
  emit('feature-click', event)
}

const focusFeature = (rowKey, options = {}) => {
  if (!rendererRef.value || typeof rendererRef.value.focusFeature !== 'function') {
    return false
  }
  return rendererRef.value.focusFeature(rowKey, options)
}

const showPopup = (payload) => {
  if (!payload || !payload.content) return
  if (!rendererRef.value || typeof rendererRef.value.showPopup !== 'function') return
  rendererRef.value.showPopup(payload)
}

const hidePopup = () => {
  if (!rendererRef.value || typeof rendererRef.value.hidePopup !== 'function') return
  rendererRef.value.hidePopup()
}

const resize = () => {
  if (!rendererRef.value || typeof rendererRef.value.resize !== 'function') return
  rendererRef.value.resize()
}

defineExpose({ focusFeature, showPopup, hidePopup, resize })
</script>

<style scoped>
.map-container {
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  overflow: hidden;
  position: relative;
}

.map-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-fill-color-lighter);
}

:deep(.map-popup) {
  background: var(--addp-bg-primary);
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
  min-width: 280px;
  max-width: 400px;
  max-height: 320px;
  overflow: auto;
  color: var(--addp-text-primary);
}

:deep(.map-popup-content) {
  display: flex;
  flex-direction: column;
  gap: 4px;
  color: #1f2937;
}

:deep(.map-popup-row) {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  line-height: 1.4;
  color: #1f2937;
}

:deep(.map-popup-label) {
  font-weight: 600;
  color: #4b5563;
}

:deep(.map-popup-value) {
  flex: 1;
  text-align: right;
  color: #111827;
  word-break: break-all;
}

:deep(.feature-card) {
  font-size: 12px;
}

:deep(.feature-card-header) {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--el-border-color-light);
  background: var(--addp-bg-secondary);
}

:deep(.feature-id) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 600;
  color: var(--addp-text-primary);
}

:deep(.feature-geom-type) {
  flex: 0 0 auto;
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
}

:deep(.feature-primary-field) {
  padding: 12px;
  border-bottom: 1px solid var(--el-border-color-light);
}

:deep(.primary-value) {
  font-size: 16px;
  font-weight: 600;
  line-height: 1.4;
  color: var(--addp-text-primary);
  word-break: break-word;
}

:deep(.primary-label) {
  margin-top: 4px;
  color: var(--addp-text-tertiary);
}

:deep(.feature-attributes) {
  padding: 8px 12px 10px;
}

:deep(.attribute-item) {
  display: grid;
  grid-template-columns: minmax(72px, 0.42fr) minmax(0, 1fr);
  gap: 8px;
  padding: 5px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
  line-height: 1.45;
}

:deep(.attribute-item:last-child) {
  border-bottom: 0;
}

:deep(.attr-key) {
  color: var(--addp-text-secondary);
  font-weight: 600;
  word-break: break-word;
}

:deep(.attr-value) {
  color: var(--addp-text-primary);
  word-break: break-word;
  user-select: text;
}

:deep(.attribute-empty),
:deep(.null-value) {
  color: var(--addp-text-tertiary);
}
</style>
