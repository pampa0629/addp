<template>
  <div class="geojson-preview">
    <div class="controls">
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

    <pre class="json-content" :class="{ collapsed: showMap }">{{ formattedJson }}</pre>

    <div v-if="truncated" class="truncate-tip">内容较大，仅展示部分</div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useMapConfig } from '@/composables/useMapConfig'
import MapContainer from '@/components/map/MapContainer.vue'
import { safeStringify } from '@/utils/formatters'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const { baseMapOptions, defaultBaseMapType, loadMapConfig } = useMapConfig()

const showMap = ref(true)
const baseMapType = ref('')

const geojsonData = computed(() => {
  return props.data?.object?.content?.geojson || props.data?.object?.content?.GeoJSON || null
})

const truncated = computed(() => {
  return props.data?.object?.content?.truncated || props.data?.object?.truncated || false
})

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
    console.error('解析 GeoJSON 失败', error)
  }

  return []
})

const formattedJson = computed(() => {
  return safeStringify(geojsonData.value)
})

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
</style>
