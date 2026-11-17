<template>
  <div ref="mapContainer" class="gaode-map-renderer"></div>
</template>

<script setup>
import { computed, ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { getPreviewCRSTransform, transformGeoJSONGeometryToWGS84 } from '../../utils/crsRegistry'
import { useGaodeMap } from '../../composables/useGaodeMap'

const props = defineProps({
  dataProjection: {
    type: String,
    default: 'EPSG:4326'
  },
  features: {
    type: Array,
    default: () => []
  },
  config: {
    type: Object,
    required: true
  },
  baseMapProfile: {
    type: Object,
    default: () => ({})
  },
  preserveView: {
    type: Boolean,
    default: false
  },
  viewState: {
    type: Object,
    default: () => ({})
  },
  popupOptions: {
    type: Object,
    default: () => ({})
  }
})

const displayFeatures = computed(() => {
  const transform = getPreviewCRSTransform({ source_crs: props.dataProjection })
  return props.features.map(feature => ({ ...feature,
    geometry: transformGeoJSONGeometryToWGS84(feature.geometry, transform)
  })).filter(feature => feature.geometry)
})

const emit = defineEmits(['feature-click', 'view-state-change'])

const mapContainer = ref(null)
const { mapInstance, initMap, renderFeatures, focusFeature, showPopup, hidePopup, destroy } = useGaodeMap(props.config, props.baseMapProfile, {
  initialViewState: props.viewState,
  onViewStateChange: (state) => emit('view-state-change', state)
})

let isInitialized = false

const setupMap = async () => {
  if (!mapContainer.value) return

  await nextTick()

  if (!isInitialized) {
    const result = await initMap(mapContainer.value)
    if (result) {
      isInitialized = true
    }
  }

  renderFeatures(displayFeatures.value, {
    preserveView: props.preserveView,
    popupOptions: props.popupOptions,
    onFeatureClick: (feature, coordinate, position) => {
      emit('feature-click', { feature, coordinate, position })
    }
  })
}

watch(
  () => displayFeatures.value,
  () => {
    if (isInitialized) {
      renderFeatures(displayFeatures.value, {
        preserveView: props.preserveView,
        popupOptions: props.popupOptions,
        onFeatureClick: (feature, coordinate, position) => {
          emit('feature-click', { feature, coordinate, position })
        }
      })
    }
  },
  { deep: true }
)

watch(
  () => props.popupOptions,
  () => {
    if (isInitialized) {
      renderFeatures(displayFeatures.value, {
        preserveView: true,
        popupOptions: props.popupOptions,
        onFeatureClick: (feature, coordinate, position) => {
          emit('feature-click', { feature, coordinate, position })
        }
      })
    }
  },
  { deep: true }
)

onMounted(() => {
  setupMap()
})

onBeforeUnmount(() => {
  destroy()
})

const focusFeatureByKey = (rowKey, options = {}) => {
  if (!isInitialized) return false
  return focusFeature(rowKey, options)
}

const showPopupContent = ({ content, coordinate, position }) => {
  if (!isInitialized) return
  const targetPosition = position || coordinate
  if (!content) return
  showPopup(content, targetPosition)
}

const hidePopupContent = () => {
  hidePopup()
}

const resize = () => {
  if (mapInstance.value && typeof mapInstance.value.resize === 'function') {
    mapInstance.value.resize()
  }
}

defineExpose({
  focusFeature: focusFeatureByKey,
  showPopup: showPopupContent,
  hidePopup: hidePopupContent,
  resize
})
</script>

<style scoped>
.gaode-map-renderer {
  width: 100%;
  height: 100%;
}

:deep(.gaode-point-marker) {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background-color: #409eff;
  border: 2px solid #ffffff;
  box-shadow: 0 0 6px rgba(64, 158, 255, 0.4);
}

:deep(.gaode-point-marker.is-highlighted) {
  width: 18px;
  height: 18px;
  background-color: #ffd700;
  border-color: #fff7bf;
  box-shadow: 0 0 0 3px rgba(255, 215, 0, 0.35), 0 0 14px rgba(255, 215, 0, 0.7);
}
</style>
