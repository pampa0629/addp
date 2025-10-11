<template>
  <div ref="mapContainer" class="openlayers-map-renderer"></div>
</template>

<script setup>
import { ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useOpenLayersMap } from '@/composables/useOpenLayersMap'

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
  preserveView: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['feature-click'])

const mapContainer = ref(null)
const { initMap, renderFeatures, destroy } = useOpenLayersMap(props.config)

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

onMounted(() => {
  setupMap()
})

onBeforeUnmount(() => {
  destroy()
})
</script>

<style scoped>
.openlayers-map-renderer {
  width: 100%;
  height: 100%;
}
</style>
