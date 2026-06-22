<template>
  <div ref="mapContainer" class="openlayers-map-renderer"></div>
</template>

<script setup>
import { ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useOpenLayersMap } from '../../composables/useOpenLayersMap'

const props = defineProps({
  features: {
    type: Array,
    default: () => []
  },
  config: {
    type: Object,
    required: true
  },
  baseType: {
    type: String,
    default: 'tiandituVector'
  },
  baseMapProfile: {
    type: Object,
    default: () => ({})
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

const mapContainer = ref(null)
const { mapInstance, initMap, renderFeatures, focusFeature, showPopup, hidePopup, destroy } = useOpenLayersMap(props.config)

let isInitialized = false

const setupMap = async () => {
  if (!mapContainer.value) return

  await nextTick()

  if (!isInitialized) {
    const result = initMap(mapContainer.value, props.baseType)
    if (result) {
      isInitialized = true
    }
  }

  renderFeatures(props.features, {
    preserveView: props.preserveView,
    popupOptions: props.popupOptions,
    onFeatureClick: (feature, coordinate) => {
      emit('feature-click', { feature, coordinate })
    }
  })
}

watch(
  () => props.features,
  () => {
    if (isInitialized) {
      renderFeatures(props.features, {
        preserveView: props.preserveView,
        popupOptions: props.popupOptions,
        onFeatureClick: (feature, coordinate) => {
          emit('feature-click', { feature, coordinate })
        }
      })
    }
  },
  { deep: true }
)

watch(
  () => props.baseType,
  () => {
    // 底图类型变化时重新初始化
    if (mapContainer.value) {
      destroy()
      isInitialized = false
      setupMap()
    }
  }
)

watch(
  () => props.popupOptions,
  () => {
    if (isInitialized) {
      renderFeatures(props.features, {
        preserveView: true,
        popupOptions: props.popupOptions,
        onFeatureClick: (feature, coordinate) => {
          emit('feature-click', { feature, coordinate })
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

const showPopupContent = ({ content, coordinate }) => {
  if (!isInitialized || !content) return
  showPopup(content, coordinate)
}

const hidePopupContent = () => {
  hidePopup()
}

const resize = () => {
  if (mapInstance.value && typeof mapInstance.value.updateSize === 'function') {
    mapInstance.value.updateSize()
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
.openlayers-map-renderer {
  width: 100%;
  height: 100%;
}
</style>
