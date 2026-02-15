<template>
  <div ref="mapEl" class="geojson-map"></div>
  <div v-if="error" class="geojson-error">{{ error }}</div>
  <div v-if="loading" class="geojson-loading">加载中...</div>

  <!-- Popup 信息框 -->
  <div ref="popupEl" class="ol-popup">
    <div class="ol-popup-closer" @click="closePopup"></div>
    <div class="ol-popup-content" v-html="popupContent"></div>
  </div>
</template>

<script setup>
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import Map from 'ol/Map.js'
import View from 'ol/View.js'
import VectorLayer from 'ol/layer/Vector.js'
import VectorSource from 'ol/source/Vector.js'
import GeoJSON from 'ol/format/GeoJSON.js'
import { defaults as defaultInteractions, MouseWheelZoom } from 'ol/interaction.js'
import { defaults as defaultControls } from 'ol/control.js'
import client from '@/api/client'

// 导入 common-frontend/map 的工具和composables
import {
  useMapPopup,
  useFeatureHighlight,
  fromLonLat,
  createGaodeBaseLayer,
  createDefaultStyleFunction
} from '@common-map'

const props = defineProps({
  engineId: { type: [Number, String], required: true },
  schema: { type: String, required: true },
  table: { type: String, required: true },
  geom: { type: String, default: 'geom' },
  center: { type: Array, default: () => [120.2, 30.3] },
  zoom: { type: Number, default: 10 },
  page: { type: Number, default: 1 },
  pageSize: { type: Number, default: 10 }
})

const emit = defineEmits(['featureClick'])

const mapEl = ref(null)
let map
const error = ref('')
const loading = ref(false)
const metadata = ref(null)
let vectorLayer = null
let vectorSource = null

// 使用 composables
const { popupEl, popupContent, createPopup, showPopup, closePopup, extractFeatureId } = useMapPopup({ geomColumn: props.geom })
const { createHighlightLayer, focusFeatureById } = useFeatureHighlight()

// 获取 GeoJSON 元数据
async function fetchMetadata() {
  try {
    const url = `/manager/engines/${props.engineId}/spatial/${props.schema}/${props.table}/geojson/metadata`
    const params = { geom_column: props.geom }
    const response = await client.get(url, { params })
    metadata.value = response.data
    console.log('GeoJSON metadata loaded:', metadata.value)
  } catch (err) {
    console.warn('Failed to load GeoJSON metadata:', err)
    metadata.value = null
  }
}

// 加载 GeoJSON 数据（支持分页）
async function loadGeoJSON(page = 1, pageSize = 1000) {
  if (loading.value) return

  loading.value = true
  error.value = ''

  try {
    const url = `/manager/engines/${props.engineId}/spatial/${props.schema}/${props.table}/geojson`
    const params = {
      page,
      page_size: pageSize,
      geom_column: props.geom
    }

    console.log('Loading GeoJSON from:', url, params)
    const response = await client.get(url, { params })

    console.log('Response received:', response)
    console.log('Response type:', typeof response)
    console.log('Response keys:', response ? Object.keys(response) : 'null')

    const format = new GeoJSON()
    const features = format.readFeatures(response, {
      dataProjection: 'EPSG:4326',
      featureProjection: 'EPSG:3857'
    })

    console.log(`Loaded ${features.length} features from GeoJSON`)

    if (vectorSource) {
      vectorSource.clear()
      vectorSource.addFeatures(features)
    }

    if (features.length > 0 && map) {
      const extent = vectorSource.getExtent()
      map.getView().fit(extent, {
        padding: [50, 50, 50, 50],
        maxZoom: 16,
        duration: 500
      })
    }

  } catch (err) {
    console.error('Failed to load GeoJSON:', err)
    error.value = '加载 GeoJSON 数据失败'
    ElMessage.error('加载数据失败: ' + (err.response?.data?.error || err.message))
  } finally {
    loading.value = false
  }
}

async function initMap() {
  // 1. 获取元数据
  await fetchMetadata()

  // 2. 计算初始中心点和缩放级别
  let initialCenter = props.center
  let initialZoom = props.zoom

  if (metadata.value?.extent && metadata.value.extent.length === 4) {
    const [minX, minY, maxX, maxY] = metadata.value.extent
    initialCenter = [(minX + maxX) / 2, (minY + maxY) / 2]

    const lonRange = maxX - minX
    const latRange = maxY - minY
    const maxRange = Math.max(lonRange, latRange)

    if (maxRange > 10) initialZoom = 6
    else if (maxRange > 5) initialZoom = 8
    else if (maxRange > 1) initialZoom = 10
    else if (maxRange > 0.1) initialZoom = 12
    else initialZoom = 14

    console.log('Using metadata extent:', initialCenter, 'zoom:', initialZoom)
  }

  // 3. 创建图层
  const baseLayer = createGaodeBaseLayer()

  vectorSource = new VectorSource()
  vectorLayer = new VectorLayer({
    source: vectorSource,
    style: createDefaultStyleFunction(),
    zIndex: 10
  })

  const highlightLayer = createHighlightLayer()

  // 4. 创建地图
  map = new Map({
    target: mapEl.value,
    layers: [baseLayer, vectorLayer, highlightLayer],
    interactions: defaultInteractions({ mouseWheelZoom: false }).extend([
      new MouseWheelZoom({
        duration: 100,
        timeout: 100,
        useAnchor: true
      })
    ]),
    controls: defaultControls({
      zoom: true,
      zoomOptions: {
        zoomInTipLabel: '放大',
        zoomOutTipLabel: '缩小'
      }
    }),
    view: new View({
      center: fromLonLat(initialCenter),
      zoom: initialZoom,
      maxZoom: 20,
      minZoom: 1,
      smoothResolutionConstraint: true,
      constrainResolution: false,
      enableRotation: false
    })
  })

  // 5. 创建 Popup
  createPopup(map)

  // 6. 加载 GeoJSON 数据
  await loadGeoJSON(props.page, props.pageSize)

  // 7. 添加地图点击事件监听
  map.on('singleclick', (evt) => {
    const features = map.getFeaturesAtPixel(evt.pixel)
    console.log('Map clicked, features found:', features ? features.length : 0)

    if (features && features.length > 0) {
      const feature = features[0]
      const properties = feature.getProperties()
      console.log('Feature properties:', properties)

      showPopup(feature, evt.coordinate)

      const featureId = extractFeatureId(properties)
      console.log('Extracted featureId:', featureId)

      if (featureId) {
        console.log('Emitting featureClick event with id:', featureId)
        emit('featureClick', featureId)
      } else {
        console.warn('Feature has no id field. Available properties:', Object.keys(properties))
      }
    } else {
      closePopup()
      console.log('No features found at clicked position')
    }
  })
}

function focusFeatureByIdWrapper(featureId, geojsonString, centroid, extent) {
  focusFeatureById(map, featureId, geojsonString, centroid, extent)
}

defineExpose({ focusFeatureById: focusFeatureByIdWrapper, loadGeoJSON })

onMounted(() => {
  initMap()
})

onBeforeUnmount(() => {
  if (map) {
    map.setTarget(null)
    map = null
  }
})

watch(() => [props.engineId, props.schema, props.table, props.geom], async () => {
  if (!map) return

  metadata.value = null
  await fetchMetadata()

  if (metadata.value?.extent && metadata.value.extent.length === 4) {
    const [minX, minY, maxX, maxY] = metadata.value.extent
    const newCenter = [(minX + maxX) / 2, (minY + maxY) / 2]
    map.getView().setCenter(fromLonLat(newCenter))
  }

  await loadGeoJSON(props.page, props.pageSize)
})

// 监听分页参数变化，重新加载当前页数据
watch(() => [props.page, props.pageSize], async () => {
  if (!map || !vectorSource) return

  console.log(`Page changed to ${props.page}, loading GeoJSON...`)
  await loadGeoJSON(props.page, props.pageSize)
})
</script>

<style scoped>
.geojson-map {
  width: 100%;
  height: 100%;
  position: absolute;
  inset: 0;
}
.geojson-error {
  position: absolute;
  top: 8px;
  right: 8px;
  background: rgba(220, 38, 38, 0.9);
  color: #fff;
  padding: 6px 12px;
  border-radius: 4px;
  font-size: 12px;
  z-index: 1000;
}
.geojson-loading {
  position: absolute;
  top: 8px;
  left: 8px;
  background: rgba(59, 130, 246, 0.9);
  color: #fff;
  padding: 6px 12px;
  border-radius: 4px;
  font-size: 12px;
  z-index: 1000;
}

/* Popup 样式 */
.ol-popup {
  position: absolute;
  background-color: white;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  padding: 0;
  border-radius: 8px;
  border: 1px solid #ccc;
  bottom: 12px;
  left: -50px;
  min-width: 280px;
  max-width: 400px;
  max-height: 300px;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.ol-popup:after,
.ol-popup:before {
  top: 100%;
  border: solid transparent;
  content: " ";
  height: 0;
  width: 0;
  position: absolute;
  pointer-events: none;
}

.ol-popup:after {
  border-top-color: white;
  border-width: 10px;
  left: 48px;
  margin-left: -10px;
}

.ol-popup:before {
  border-top-color: #ccc;
  border-width: 11px;
  left: 48px;
  margin-left: -11px;
}

.ol-popup-closer {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 20px;
  height: 20px;
  cursor: pointer;
  z-index: 1;
}

.ol-popup-closer:after {
  content: "✕";
  font-size: 16px;
  color: var(--addp-text-secondary);
  display: block;
  text-align: center;
  line-height: 20px;
}

.ol-popup-closer:hover:after {
  color: var(--addp-text-primary);
}

.ol-popup-content {
  overflow-y: auto;
  max-height: 320px;
  padding: 0;
}

.ol-popup-content :deep(.feature-card) {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
}

.ol-popup-content :deep(.feature-card-header) {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 12px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  font-size: 13px;
}

.ol-popup-content :deep(.feature-id) {
  display: flex;
  align-items: center;
  gap: 4px;
  font-weight: 600;
}

.ol-popup-content :deep(.id-icon) {
  font-size: 14px;
}

.ol-popup-content :deep(.feature-geom-type) {
  font-size: 11px;
  background: rgba(255, 255, 255, 0.2);
  padding: 2px 8px;
  border-radius: 10px;
}

.ol-popup-content :deep(.feature-primary-field) {
  padding: 16px 12px 12px;
  border-bottom: 2px solid #f0f0f0;
}

.ol-popup-content :deep(.primary-value) {
  font-size: 20px;
  font-weight: 700;
  color: var(--addp-text-primary);
  margin-bottom: 4px;
  line-height: 1.3;
}

.ol-popup-content :deep(.primary-label) {
  font-size: 11px;
  color: var(--addp-text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.ol-popup-content :deep(.feature-attributes) {
  padding: 12px;
}

.ol-popup-content :deep(.attribute-item) {
  padding: 6px 0;
  font-size: 13px;
  line-height: 1.5;
  border-bottom: 1px solid #f5f5f5;
}

.ol-popup-content :deep(.attribute-item:last-child) {
  border-bottom: none;
}

.ol-popup-content :deep(.attr-key) {
  font-weight: 600;
  color: var(--addp-text-secondary);
  margin-right: 4px;
}

.ol-popup-content :deep(.attr-value) {
  color: #000;
  word-break: break-word;
  user-select: text;
  cursor: text;
}

.ol-popup-content :deep(.null-value) {
  color: var(--addp-text-tertiary);
  font-style: italic;
}
</style>
