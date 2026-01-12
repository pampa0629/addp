<template>
  <!-- 加载状态指示器 -->
  <div v-if="isLoadingConfig" class="loading-overlay">
    <div class="loading-content">
      <div class="loading-spinner"></div>
      <p>正在加载瓦片配置...</p>
    </div>
  </div>

  <!-- 错误横幅 -->
  <div v-if="error && !isLoadingConfig" class="error-banner">
    <span class="error-icon">⚠️</span>
    <span class="error-text">{{ error }}</span>
    <button @click="retryLoadConfig" class="retry-btn">重试</button>
  </div>

  <div ref="mapEl" class="vt-map"></div>

  <!-- Popup 信息框 -->
  <div ref="popupEl" class="ol-popup">
    <div class="ol-popup-closer" @click="closePopup"></div>
    <div class="ol-popup-content" v-html="popupContent"></div>
  </div>
</template>

<script setup>
import { onMounted, onBeforeUnmount, ref, watch, computed } from 'vue'
import { ElMessage } from 'element-plus'
import Map from 'ol/Map.js'
import View from 'ol/View.js'
import { defaults as defaultInteractions, MouseWheelZoom } from 'ol/interaction.js'
import { defaults as defaultControls, ZoomToExtent } from 'ol/control.js'
import client from '@/api/client'

// 导入 common-frontend/map 的工具和composables
import {
  useMapPopup,
  useFeatureHighlight,
  useVectorTileLoader,
  fromLonLat,
  createGaodeBaseLayer
} from '@common-map'

const props = defineProps({
  engineId: { type: [Number, String], required: true },
  schema: { type: String, required: true },
  table: { type: String, required: true },
  geom: { type: String, default: 'geom' },
  center: { type: Array, default: () => [120.2, 30.3] },
  zoom: { type: Number, default: 10 }
})

const emit = defineEmits(['featureClick'])

const mapEl = ref(null)
let map

const error = ref('')
const tileConfig = ref(null)
const isLoadingConfig = ref(false)
let lastWarningZoom = null
let hasShownMinZoomWarning = false  // 是否已显示过最小zoom警告
let hasShownMaxZoomWarning = false  // 是否已显示过最大zoom警告

// 使用 composables
const { popupEl, popupContent, createPopup, showPopup, closePopup, extractFeatureId } = useMapPopup({ geomColumn: props.geom })
const { createHighlightLayer, focusFeatureById } = useFeatureHighlight()
const { createVectorTileLayer, cancelDifferentZoomRequests, cleanup: cleanupTileLoader } = useVectorTileLoader()

const apiBase = computed(() => client.defaults.baseURL)
const token = () => localStorage.getItem('token') || ''

const tilesURLTemplate = computed(() => {
  const base = apiBase.value.replace(/\/$/, '')
  let path = `${base}/manager/engines/${props.engineId}/spatial/tiles/${props.schema}/${props.table}/{z}/{x}/{y}`

  if (props.geom && props.geom !== 'geom') {
    path += `?geom=${encodeURIComponent(props.geom)}`
  }

  return path
})

// 获取瓦片配置
async function fetchTileConfig() {
  if (isLoadingConfig.value || tileConfig.value) {
    return
  }

  isLoadingConfig.value = true
  error.value = ''

  try {
    const url = `/manager/engines/${props.engineId}/spatial/${props.schema}/${props.table}/tile-config`
    console.log('Fetching tile config from:', url)
    const data = await client.get(url)
    console.log('Tile config data:', data)

    if (!data || typeof data !== 'object') {
      console.error('Invalid data:', data)
      throw new Error(`无效的配置数据: ${JSON.stringify(data)}`)
    }

    tileConfig.value = {
      min_zoom: data.min_zoom || 6,
      max_zoom: data.max_zoom || 18,
      extent: data.extent,
      srid: data.srid || 4326,
      ...data
    }

    console.log('Tile config loaded:', tileConfig.value)

    if (!tileConfig.value.extent || tileConfig.value.extent.length !== 4) {
      console.warn('未获取到有效的 extent，将使用默认视图')
    }
  } catch (err) {
    console.error('Failed to load tile config:', err)
    error.value = '无法加载瓦片配置，使用默认视图'

    tileConfig.value = {
      min_zoom: 6,
      max_zoom: 18,
      srid: 4326
    }
  } finally {
    isLoadingConfig.value = false
  }
}

// 重试加载配置
function retryLoadConfig() {
  tileConfig.value = null
  error.value = ''
  fetchTileConfig()
}

async function initMap() {
  // 1. 获取瓦片配置
  await fetchTileConfig()

  // 2. 准备初始视图参数（稍后如果有extent会调用fit）
  let initialCenter = props.center
  let initialZoom = props.zoom

  if (tileConfig.value?.extent?.length === 4) {
    const [minX, minY, maxX, maxY] = tileConfig.value.extent
    initialCenter = [(minX + maxX) / 2, (minY + maxY) / 2]
    initialZoom = tileConfig.value.min_zoom || props.zoom
    console.log('Extent available, will fit to extent after map creation')
  } else {
    console.log('No extent available, using default center:', initialCenter)
  }

  // 3. 创建图层
  const baseLayer = createGaodeBaseLayer()
  const vtLayer = createVectorTileLayer(tilesURLTemplate.value, token, {
    minZoom: tileConfig.value?.min_zoom || 6,
    maxZoom: tileConfig.value?.max_zoom || 18
  })
  const highlightLayer = createHighlightLayer()

  // 4. 准备控件
  const controls = defaultControls({
    zoom: true,
    zoomOptions: {
      zoomInTipLabel: '放大',
      zoomOutTipLabel: '缩小'
    }
  })

  // 如果有有效的 extent，添加全幅显示控件
  let extentForControl = null
  if (tileConfig.value?.extent?.length === 4) {
    const [minX, minY, maxX, maxY] = tileConfig.value.extent
    const minCorner = fromLonLat([minX, minY])
    const maxCorner = fromLonLat([maxX, maxY])
    extentForControl = [...minCorner, ...maxCorner]

    controls.push(new ZoomToExtent({
      extent: extentForControl,
      tipLabel: '全幅显示',
      label: '⛶'  // 四个角的方框图标，更能表达"适应到边界"的含义
    }))
  }

  // 5. 创建地图
  map = new Map({
    target: mapEl.value,
    layers: [baseLayer, vtLayer, highlightLayer],
    maxTilesLoading: 16,
    interactions: defaultInteractions({ mouseWheelZoom: false }).extend([
      new MouseWheelZoom({
        duration: 100,
        timeout: 100,
        useAnchor: true
      })
    ]),
    controls: controls,
    view: new View({
      center: fromLonLat(initialCenter),
      zoom: initialZoom,
      maxZoom: 20,
      minZoom: 1,
      constrainResolution: true,  // 强制zoom level对齐到整数，确保请求正确的切片层级
      enableRotation: false
    })
  })

  // 6. 如果有 extent，自动全幅显示
  if (extentForControl) {
    map.getView().fit(extentForControl, {
      padding: [50, 50, 50, 50],  // 四周留白
      duration: 0  // 不需要动画，立即显示
    })
    console.log('Fitted map view to data extent')
  }

  // 7. 创建 Popup
  createPopup(map)

  // 8. 监听 zoom 变化
  map.getView().on('change:resolution', () => {
    const currentZoom = Math.round(map.getView().getZoom())

    if (currentZoom === lastWarningZoom) return

    const minZoom = tileConfig.value?.min_zoom || 0
    const maxZoom = tileConfig.value?.max_zoom || 20

    if (currentZoom < minZoom) {
      if (!hasShownMinZoomWarning) {
        ElMessage.warning({
          message: `当前层级 ${currentZoom} 低于建议范围，数据可能不可见。建议放大到 ${minZoom} 层级`,
          duration: 3000,
          showClose: true
        })
        hasShownMinZoomWarning = true
      }
      lastWarningZoom = currentZoom
    } else if (currentZoom > maxZoom) {
      if (!hasShownMaxZoomWarning) {
        ElMessage.info({
          message: `当前层级 ${currentZoom} 超出预缓存范围 (${maxZoom})，加载可能较慢`,
          duration: 3000,
          showClose: true
        })
        hasShownMaxZoomWarning = true
      }
      lastWarningZoom = currentZoom
    } else {
      // 回到正常范围，重置警告标志
      hasShownMinZoomWarning = false
      hasShownMaxZoomWarning = false
      lastWarningZoom = null
    }
  })

  console.log(`Map zoom range (suggested): ${tileConfig.value?.min_zoom || 6} - ${tileConfig.value?.max_zoom || 18}, actual: 1 - 20`)

  // 7. 添加点击事件
  map.on('singleclick', (evt) => {
    const features = map.getFeaturesAtPixel(evt.pixel)
    if (features && features.length > 0) {
      const feature = features[0]
      const properties = feature.getProperties()

      showPopup(feature, evt.coordinate)

      const featureId = extractFeatureId(properties)
      if (featureId) {
        emit('featureClick', featureId)
      }
    } else {
      closePopup()
    }
  })
}

function focusFeatureByIdWrapper(featureId, geojsonString, centroid, extent) {
  focusFeatureById(map, featureId, geojsonString, centroid, extent)
}

defineExpose({ focusFeatureById: focusFeatureByIdWrapper })

onMounted(() => {
  initMap()

  setTimeout(() => {
    if (map) {
      map.on('movestart', () => {
        const currentZoom = Math.round(map.getView().getZoom())
        cancelDifferentZoomRequests(currentZoom)
      })
    }
  }, 500)
})

onBeforeUnmount(() => {
  cleanupTileLoader()

  if (map) {
    map.setTarget(null)
    map = null
  }
})

watch(() => [props.engineId, props.schema, props.table, props.geom], async () => {
  if (!map) return

  tileConfig.value = null
  lastWarningZoom = null

  await fetchTileConfig()

  console.log(`Updated zoom range (suggested): ${tileConfig.value?.min_zoom || 6} - ${tileConfig.value?.max_zoom || 18}`)

  if (tileConfig.value?.extent?.length === 4) {
    const [minX, minY, maxX, maxY] = tileConfig.value.extent
    const newCenter = [(minX + maxX) / 2, (minY + maxY) / 2]

    const view = map.getView()
    const newZoom = tileConfig.value.min_zoom || view.getZoom()

    view.setCenter(fromLonLat(newCenter))
    view.setZoom(newZoom)
    console.log('Updated map center to extent:', newCenter, 'zoom:', newZoom)
  }

  const layers = map.getLayers()
  const vtIdx = 1
  const newVt = createVectorTileLayer(tilesURLTemplate.value, token, {
    minZoom: tileConfig.value?.min_zoom || 6,
    maxZoom: tileConfig.value?.max_zoom || 18
  })
  layers.setAt(vtIdx, newVt)
})
</script>

<style scoped>
.vt-map { width: 100%; height: 100%; position: absolute; inset: 0; }

.loading-overlay {
  position: absolute;
  inset: 0;
  background: rgba(255, 255, 255, 0.9);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.loading-content {
  text-align: center;
  color: #666;
}

.loading-spinner {
  width: 40px;
  height: 40px;
  border: 4px solid #f3f3f3;
  border-top: 4px solid #409eff;
  border-radius: 50%;
  animation: spin 1s linear infinite;
  margin: 0 auto 12px;
}

@keyframes spin {
  0% { transform: rotate(0deg); }
  100% { transform: rotate(360deg); }
}

.loading-content p {
  margin: 0;
  font-size: 14px;
}

.error-banner {
  position: absolute;
  top: 16px;
  left: 50%;
  transform: translateX(-50%);
  background: #fef0f0;
  border: 1px solid #fde2e2;
  color: #f56c6c;
  padding: 12px 16px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  gap: 10px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  z-index: 999;
  max-width: 90%;
}

.error-icon {
  font-size: 18px;
  flex-shrink: 0;
}

.error-text {
  font-size: 14px;
  flex: 1;
}

.retry-btn {
  background: #f56c6c;
  color: white;
  border: none;
  padding: 6px 12px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
  transition: background 0.3s;
  flex-shrink: 0;
}

.retry-btn:hover {
  background: #f78989;
}

.retry-btn:active {
  background: #dd6161;
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
  color: #666;
  display: block;
  text-align: center;
  line-height: 20px;
}

.ol-popup-closer:hover:after {
  color: #333;
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
  color: #333;
  margin-bottom: 4px;
  line-height: 1.3;
}

.ol-popup-content :deep(.primary-label) {
  font-size: 11px;
  color: #999;
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
  color: #666;
  margin-right: 4px;
}

.ol-popup-content :deep(.attr-value) {
  color: #000;
  word-break: break-word;
  user-select: text;
  cursor: text;
}

.ol-popup-content :deep(.null-value) {
  color: #999;
  font-style: italic;
}
</style>
