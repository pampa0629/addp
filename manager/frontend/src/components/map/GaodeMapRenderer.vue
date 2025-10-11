<template>
  <div ref="mapContainer" class="gaode-map-renderer"></div>
</template>

<script setup>
import { ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useGaodeMap } from '@/composables/useGaodeMap'

const props = defineProps({
  features: {
    type: Array,
    default: () => []
  },
  config: {
    type: Object,
    required: true
  },
  preserveView: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['feature-click'])

const mapContainer = ref(null)
const { initMap, renderFeatures, destroy } = useGaodeMap(props.config)

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

  renderFeatures(props.features, {
    preserveView: props.preserveView,
    onFeatureClick: (feature, position) => {
      emit('feature-click', { feature, position })
    }
  })
}

watch(
  () => props.features,
  () => {
    if (isInitialized) {
      renderFeatures(props.features, {
        preserveView: props.preserveView,
        onFeatureClick: (feature, position) => {
          emit('feature-click', { feature, position })
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
</style>
