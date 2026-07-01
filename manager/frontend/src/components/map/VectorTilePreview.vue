<template>
  <div class="vector-tile-preview">
    <!-- 加载状态指示器 -->
    <div v-if="isLoadingConfig" class="loading-overlay">
      <div class="loading-content">
        <div class="loading-spinner"></div>
        <p>{{ t('manager.vectorTile.loadingConfig') }}</p>
      </div>
    </div>

    <!-- 错误横幅 -->
    <div v-if="error && !isLoadingConfig" class="error-banner">
      <span class="error-icon">⚠️</span>
      <span class="error-text">{{ error }}</span>
      <button @click="retryLoadConfig" class="retry-btn">{{ t('manager.vectorTile.retry') }}</button>
    </div>

    <div ref="mapEl" class="vt-map"></div>

    <el-tooltip :content="renderStatusTipContent" placement="left" effect="dark">
      <div class="render-status-badge" :class="renderStatusClass" :aria-label="renderStatusLabel">
        <span class="render-status-dot"></span>
      </div>
    </el-tooltip>

    <!-- Popup 信息框 -->
    <div ref="popupEl" class="ol-popup">
      <div class="ol-popup-closer" @click="closePopup"></div>
      <div class="ol-popup-content" v-html="popupContent"></div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, onBeforeUnmount, ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import Map from 'ol/Map.js'
import View from 'ol/View.js'
import { toLonLat } from 'ol/proj.js'
import { defaults as defaultInteractions, MouseWheelZoom } from 'ol/interaction.js'
import { defaults as defaultControls, ZoomToExtent } from 'ol/control.js'
import { unByKey } from 'ol/Observable.js'
import client from '@/api/client'
import { useMvtGridDebug } from '@/composables/useMvtGridDebug'
import { useVectorTileRenderStatus } from '@/composables/useVectorTileRenderStatus'
import {
  shouldWarnVectorTileMaxZoom,
  vectorTileMaxZoomWarningKey,
  vectorTileSourceMaxZoom
} from '@/utils/vectorTileZoomWarning'

// 导入 common-frontend/map 的工具和composables
import {
  useMapPopup,
  useFeatureHighlight,
  useVectorTileLoader,
  fromLonLat,
  useMapConfig,
  createTiandituBaseLayers
} from '@common-ui-map'

const props = defineProps({
  locator: { type: String, default: '' },
  engineId: { type: [Number, String], default: 0 },
  schema: { type: String, default: '' },
  table: { type: String, default: '' },
  geom: { type: String, default: '' },
  tileUrlTemplate: { type: String, default: '' },
  tileRenderInfo: { type: Object, default: () => ({}) },
  renderSource: { type: String, default: '' },
  defaultTileCacheId: { type: [Number, String], default: '' },
  showMvtGrid: { type: Boolean, default: false },
  viewState: { type: Object, default: () => ({}) },
  center: { type: Array, default: () => [120.2, 30.3] },
  zoom: { type: Number, default: 10 }
})

const emit = defineEmits(['featureClick', 'tile-advisory', 'view-state-change'])

const { t } = useI18n()

const mapEl = ref(null)
let map
let mvtGridMoveKey = null
let mapMoveStartKey = null
let sizeUpdateFrame = null

const error = ref('')
const tileRenderInfo = ref(null)
const isLoadingConfig = ref(false)
let lastWarningZoom = null
let hasShownMinZoomWarning = false  // 是否已显示过最小zoom警告
let hasShownMaxZoomWarning = false  // 是否已显示过最大zoom警告

// 使用 composables
const { popupEl, popupContent, createPopup, showPopup, closePopup, extractFeatureId } = useMapPopup({ geomColumn: props.geom || undefined })
const { createHighlightLayer, focusFeatureById } = useFeatureHighlight()
const { createVectorTileLayer, cleanup: cleanupTileLoader } = useVectorTileLoader()
const { mapConfig, loadMapConfig } = useMapConfig()

const apiBase = computed(() => client.defaults.baseURL)
const token = () => localStorage.getItem('token') || ''
const {
  createGridLayer,
  updateGrid: updateMvtGrid,
  resetGrid: resetMvtGrid,
  clearTileStates: clearMvtGridTileStates,
  rememberTileState: rememberMvtGridTileState,
  disposeGrid: disposeMvtGrid
} = useMvtGridDebug({
  t,
  isVisible: () => props.showMvtGrid
})

const tilesURLTemplate = computed(() => {
  const rawTemplate = String(props.tileUrlTemplate || '').trim()
  if (rawTemplate) {
    return rawTemplate
  }
  const values = new URLSearchParams()
  if (props.locator) values.set('locator', props.locator)
  if (props.geom) values.set('geometry_column', props.geom)
  const query = values.toString()
  if (query) {
    return `/api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?${query}`
  }
  const base = apiBase.value.replace(/\/$/, '')
  return `${base}/manager/quick-view/tiles/{z}/{x}/{y}.mvt`
})

const normalizedRenderSource = computed(() => String(
  props.renderSource || tileRenderInfo.value?.render_source || ''
).trim())

const maxZoomWarningMessage = (zoom, max) => {
  return t(vectorTileMaxZoomWarningKey(normalizedRenderSource.value), { zoom, max })
}

const sourceMaxZoom = computed(() => vectorTileSourceMaxZoom(
  normalizedRenderSource.value,
  tileRenderInfo.value?.max_zoom,
  22
))

const {
  renderStatusClass,
  renderStatusLabel,
  renderStatusTooltip,
  resetTileStatus,
  handleTileLoadEnd,
  handleTileLoadError
} = useVectorTileRenderStatus({
  t,
  getRenderSource: () => props.renderSource,
  getDefaultTileCacheId: () => props.defaultTileCacheId,
  rememberTileState: (meta, hasError) => {
    rememberMvtGridTileState(meta, hasError, map)
    const recommendation = String(meta?.recommendation || '').toLowerCase()
    const retryPolicy = String(meta?.retryPolicy || '').toLowerCase()
    if (
      recommendation === 'vector_quick_view_target_generation' ||
      recommendation === 'vector_tile_cache_generation' ||
      retryPolicy === 'suppress_tile' ||
      meta?.suppressed
    ) {
      emit('tile-advisory', {
        recommendation: recommendation || 'vector_quick_view_target_generation',
        retryPolicy,
        tileStatus: meta?.tileStatus || '',
        performanceMode: meta?.performanceMode || '',
        timeoutBudgetMS: meta?.timeoutBudgetMS || '',
        retryAfter: meta?.retryAfter || '',
        tileKey: meta?.tileKey || '',
        suppressed: !!meta?.suppressed
      })
    }
  }
})

const renderStatusTipContent = computed(() => {
  const label = renderStatusLabel.value || ''
  const detail = renderStatusTooltip.value || ''
  return [label, detail].filter(Boolean).join(': ')
})

function resetTileRenderState() {
  resetTileStatus()
  resetMvtGrid(map)
}

function scheduleMapSizeUpdate() {
  if (!map || typeof window === 'undefined') return
  if (sizeUpdateFrame) {
    window.cancelAnimationFrame(sizeUpdateFrame)
  }
  sizeUpdateFrame = window.requestAnimationFrame(() => {
    sizeUpdateFrame = null
    if (map) {
      map.updateSize()
    }
  })
}

const createMVTLayer = () => {
  const layer = createVectorTileLayer(tilesURLTemplate.value, token, {
    minZoom: tileRenderInfo.value?.min_zoom || 6,
    maxZoom: sourceMaxZoom.value,
    cacheSize: 64,
    maxDecodedTileBytes: 8 * 1024 * 1024,
    degradedRetryCooldownMs: 15000,
    onTileLoadEnd: handleTileLoadEnd,
    onTileLoadError: handleTileLoadError
  })
  layer.set('addpLayerRole', 'vector-tile')
  return layer
}

function applyTileRenderInfoFromProps() {
  const data = props.tileRenderInfo || {}
  tileRenderInfo.value = {
    min_zoom: data.min_zoom || 6,
    max_zoom: data.max_zoom || 18,
    extent: data.extent,
    extent_srid: data.extent_srid,
    geometry_column: data.geometry_column || props.geom,
    record_count: data.record_count,
    ...data
  }
  if (!tileRenderInfo.value.extent || tileRenderInfo.value.extent.length !== 4) {
    console.warn('未获取到有效的 extent，将使用默认视图')
  }
}

function renderExtentInWebMercator() {
  if (!tileRenderInfo.value?.extent || tileRenderInfo.value.extent.length !== 4) {
    return null
  }
  const [minX, minY, maxX, maxY] = tileRenderInfo.value.extent
  const minCorner = fromLonLat([minX, minY])
  const maxCorner = fromLonLat([maxX, maxY])
  return [...minCorner, ...maxCorner]
}

function fitToRenderExtent({ duration = 0 } = {}) {
  const extentForFit = renderExtentInWebMercator()
  if (!map || !extentForFit) return false
  map.getView().fit(extentForFit, {
    padding: [50, 50, 50, 50],
    duration
  })
  return true
}

function finiteMapViewState() {
  const state = props.viewState || {}
  const center = Array.isArray(state.center)
    ? state.center.slice(0, 2).map((value) => Number(value))
    : null
  const zoom = Number(state.zoom)
  if (!center || center.length < 2 || !center.every(Number.isFinite) || !Number.isFinite(zoom)) {
    return null
  }
  return {
    center,
    zoom,
    rotation: Number.isFinite(Number(state.rotation)) ? Number(state.rotation) : 0
  }
}

function applyMapViewState(state) {
  if (!map || !state) return false
  const view = map.getView()
  view.setCenter(fromLonLat(state.center))
  view.setZoom(state.zoom)
  view.setRotation(state.rotation || 0)
  return true
}

function emitMapViewState() {
  if (!map) return
  const view = map.getView()
  const center = toLonLat(view.getCenter() || [0, 0])
  emit('view-state-change', {
    center,
    zoom: view.getZoom(),
    rotation: view.getRotation()
  })
}

// 重试加载渲染信息
function retryLoadConfig() {
  error.value = ''
  applyTileRenderInfoFromProps()
  fitToRenderExtent()
}

async function initMap() {
  // 1. 应用快显渲染信息并加载底图配置
  isLoadingConfig.value = true
  error.value = ''
  applyTileRenderInfoFromProps()
  await loadMapConfig()
  isLoadingConfig.value = false

  // 2. 准备初始视图参数（稍后如果有extent会调用fit）
  const savedMapViewState = finiteMapViewState()
  let initialCenter = props.center
  let initialZoom = props.zoom

  if (savedMapViewState) {
    initialCenter = savedMapViewState.center
    initialZoom = savedMapViewState.zoom
  } else if (tileRenderInfo.value?.extent?.length === 4) {
    const [minX, minY, maxX, maxY] = tileRenderInfo.value.extent
    initialCenter = [(minX + maxX) / 2, (minY + maxY) / 2]
    initialZoom = tileRenderInfo.value.min_zoom || props.zoom
  }

  // 3. 创建图层
  const baseLayers = createTiandituBaseLayers({
    key: mapConfig.value?.tdtKey,
    type: 'vector',
    zIndex: 0,
    labelZIndex: 1,
    maxZoom: 18
  })
  if (baseLayers.length === 0) {
    console.warn('未配置天地图 Key，快显使用无底图模式')
  }
  const vtLayer = createMVTLayer()
  const highlightLayer = createHighlightLayer()
  const mvtGridLayer = createGridLayer()

  // 4. 准备控件
  const controls = defaultControls({
    zoom: true,
    zoomOptions: {
      zoomInTipLabel: t('manager.vectorTile.zoomIn'),
      zoomOutTipLabel: t('manager.vectorTile.zoomOut')
    }
  })

  // 如果有有效的 extent，添加全幅显示控件
  const extentForControl = renderExtentInWebMercator()
  if (extentForControl) {
    controls.push(new ZoomToExtent({
      extent: extentForControl,
      tipLabel: t('manager.vectorTile.fitExtent'),
      label: '⛶'  // 四个角的方框图标，更能表达"适应到边界"的含义
    }))
  }

  // 5. 创建地图
  map = new Map({
    target: mapEl.value,
    layers: [...baseLayers, vtLayer, highlightLayer, mvtGridLayer],
    maxTilesLoading: 8,
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
      maxZoom: 19,
      minZoom: 1,
      constrainResolution: true,  // 强制zoom level对齐到整数，确保请求正确的切片层级
      enableRotation: false
    })
  })

  scheduleMapSizeUpdate()

  // 6. 如果有 extent，自动全幅显示
  if (savedMapViewState) {
    applyMapViewState(savedMapViewState)
  } else {
    fitToRenderExtent()
  }
  updateMvtGrid(map)

  // 7. 创建 Popup
  createPopup(map)

  mvtGridMoveKey = map.on('moveend', () => {
    updateMvtGrid(map)
    emitMapViewState()
  })
  mapMoveStartKey = map.on('movestart', resetTileRenderState)

  // 8. 监听 zoom 变化
  map.getView().on('change:resolution', () => {
    const currentZoom = Math.round(map.getView().getZoom())

    if (currentZoom === lastWarningZoom) return

    const minZoom = tileRenderInfo.value?.min_zoom || 0
    const maxZoom = tileRenderInfo.value?.max_zoom || 20

    if (currentZoom < minZoom) {
      if (!hasShownMinZoomWarning) {
        ElMessage.warning({
          message: t('manager.vectorTile.zoomTooLow', { zoom: currentZoom, min: minZoom }),
          duration: 3000,
          showClose: true
        })
        hasShownMinZoomWarning = true
      }
      lastWarningZoom = currentZoom
    } else if (shouldWarnVectorTileMaxZoom(normalizedRenderSource.value) && currentZoom > maxZoom) {
      if (!hasShownMaxZoomWarning) {
        ElMessage.info({
          message: maxZoomWarningMessage(currentZoom, maxZoom),
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
})

onBeforeUnmount(() => {
  cleanupTileLoader()
  if (mvtGridMoveKey) {
    unByKey(mvtGridMoveKey)
    mvtGridMoveKey = null
  }
  if (mapMoveStartKey) {
    unByKey(mapMoveStartKey)
    mapMoveStartKey = null
  }

  if (map) {
    map.setTarget(null)
    map = null
  }
  if (sizeUpdateFrame && typeof window !== 'undefined') {
    window.cancelAnimationFrame(sizeUpdateFrame)
    sizeUpdateFrame = null
  }
  disposeMvtGrid()
})

watch(() => [props.locator, props.geom, props.tileUrlTemplate, props.tileRenderInfo], async () => {
  if (!map) return

  applyTileRenderInfoFromProps()
  lastWarningZoom = null
  resetTileRenderState()

  if (tileRenderInfo.value?.extent?.length === 4) {
    fitToRenderExtent()
  }

  const layers = map.getLayers()
  const newVt = createMVTLayer()
  const vtIdx = layers.getArray().findIndex((layer) => layer.get('addpLayerRole') === 'vector-tile')
  if (vtIdx >= 0) {
    layers.setAt(vtIdx, newVt)
  } else {
    layers.push(newVt)
  }
  updateMvtGrid(map)
})

watch(() => props.showMvtGrid, () => {
  if (!props.showMvtGrid) {
    clearMvtGridTileStates()
  }
  updateMvtGrid(map)
})
</script>

<style scoped>
.vector-tile-preview {
  position: relative;
  width: 100%;
  height: 100%;
  min-height: 320px;
  overflow: hidden;
}

.vt-map { width: 100%; height: 100%; position: absolute; inset: 0; }

.vt-map :deep(.ol-viewport) {
  background: #eef2f7;
}

.render-status-badge {
  position: absolute;
  top: 12px;
  right: 12px;
  z-index: 20;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  border-radius: 50%;
  border: 1px solid rgba(255, 255, 255, 0.18);
  background: rgba(24, 30, 38, 0.88);
  color: #f8fafc;
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.32);
  cursor: help;
  backdrop-filter: blur(8px);
}

.render-status-dot {
  width: 8px;
  height: 8px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: var(--el-color-info);
  box-shadow: 0 0 0 2px rgba(255, 255, 255, 0.18);
}

.render-status-badge.is-cache {
  color: #bff7c7;
}

.render-status-badge.is-cache .render-status-dot {
  background: var(--el-color-success);
}

.render-status-badge.is-cache-priority {
  color: #c7ddff;
}

.render-status-badge.is-cache-priority .render-status-dot {
  background: var(--el-color-primary);
}

.render-status-badge.is-dynamic {
  color: #ffe0a3;
}

.render-status-badge.is-dynamic .render-status-dot {
  background: var(--el-color-warning);
}

.render-status-badge.is-error {
  color: #ffc4c4;
}

.render-status-badge.is-error .render-status-dot {
  background: var(--el-color-danger);
}

.render-status-badge.is-warning {
  color: #ffd9a8;
}

.render-status-badge.is-warning .render-status-dot {
  background: var(--el-color-warning);
}

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
  color: var(--addp-text-secondary);
}

.loading-spinner {
  width: 40px;
  height: 40px;
  border: 4px solid #f3f3f3;
  border-top: 4px solid var(--el-color-primary);
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
  color: var(--el-color-danger);
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
  background: var(--el-color-danger);
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
