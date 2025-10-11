<template>
  <div class="map-container" :style="{ height }">
    <component
      :is="mapRenderer"
      v-if="mapRenderer"
      :features="features"
      :config="mapConfig"
      :base-type="baseMapType"
      :preserve-view="preserveView"
      @feature-click="handleFeatureClick"
    />
    <div v-else class="map-placeholder">
      <el-empty description="未配置地图服务" />
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useMapConfig } from '@/composables/useMapConfig'
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
  }
})

const emit = defineEmits(['feature-click'])

const { mapConfig, GAODE_BASE_MAP_VALUE } = useMapConfig()

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
  background: rgba(255, 255, 255, 0.96);
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
  padding: 8px 12px;
  max-width: 280px;
  font-size: 12px;
  color: var(--el-text-color-primary);
}

:deep(.map-popup-content) {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

:deep(.map-popup-row) {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  line-height: 1.4;
}

:deep(.map-popup-label) {
  font-weight: 600;
  color: var(--el-text-color-secondary);
}

:deep(.map-popup-value) {
  flex: 1;
  text-align: right;
  color: var(--el-text-color-primary);
  word-break: break-all;
}
</style>
