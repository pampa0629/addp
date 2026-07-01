<template>
  <div class="raster-tiff-quick-view">
    <div class="raster-toolbar">
      <div class="raster-heading">
        <span class="raster-title">{{ rasterTitle }}</span>
        <span v-if="rasterSummary" class="raster-summary">{{ rasterSummary }}</span>
      </div>
      <div class="raster-controls">
        <el-select v-model="baseMapType" size="small" class="base-map-select">
          <el-option
            v-for="item in rasterBaseMapOptions"
            :key="item.value"
            :label="item.label"
            :value="item.value"
          />
        </el-select>
        <div class="opacity-control">
          <span class="opacity-label">{{ t('manager.spatialPreview.rasterOpacity') }}</span>
          <el-slider
            v-model="rasterOpacityPercent"
            :min="20"
            :max="100"
            :step="5"
            size="small"
            class="opacity-slider"
          />
        </div>
        <div v-if="isMosaicQuickView" class="gamma-control">
          <span class="opacity-label">{{ t('manager.spatialPreview.rasterGamma') }}</span>
          <el-slider
            v-model="rasterMosaicGammaPercent"
            :min="30"
            :max="140"
            :step="5"
            size="small"
            class="gamma-slider"
          />
        </div>
        <el-checkbox
          v-if="isMosaicQuickView"
          v-model="rasterMosaicInvert"
          size="small"
          class="invert-checkbox"
        >
          {{ t('manager.spatialPreview.rasterInvert') }}
        </el-checkbox>
        <el-button size="small" @click="fitToExtent({ duration: 250 })">
          {{ t('manager.vectorTile.fitExtent') }}
        </el-button>
      </div>
    </div>

    <div class="raster-map-wrap">
      <div v-if="loading" class="loading-overlay">
        <div class="loading-content">
          <div class="loading-spinner"></div>
          <p>{{ t('manager.spatialPreview.loadingRasterQuickView') }}</p>
        </div>
      </div>

      <el-empty
        v-if="error"
        :description="error"
        :image-size="72"
        class="map-empty"
      >
        <el-button type="primary" size="small" @click="reload">
          {{ t('manager.vectorTile.retry') }}
        </el-button>
      </el-empty>

      <div ref="mapEl" class="raster-map"></div>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { fromUrl as tiffFromUrl } from 'geotiff'
import Map from 'ol/Map.js'
import View from 'ol/View.js'
import { toLonLat } from 'ol/proj.js'
import GeoTIFFSource from 'ol/source/GeoTIFF'
import XYZ from 'ol/source/XYZ.js'
import TileLayer from 'ol/layer/Tile.js'
import WebGLTileLayer from 'ol/layer/WebGLTile.js'
import TileState from 'ol/TileState.js'
import { defaults as defaultControls, ZoomToExtent } from 'ol/control.js'
import { defaults as defaultInteractions, MouseWheelZoom } from 'ol/interaction.js'
import { unByKey } from 'ol/Observable.js'
import { fromLonLat, createGaodeBaseLayer, createTiandituBaseLayers, useMapConfig } from '@common-ui-map'
import {
  rasterGeoTIFFProjectionFromQuickView,
  rasterGeoTIFFSourceOptions
} from '@/utils/rasterGeoTIFFSourceOptions'
import {
  RASTER_QUICK_VIEW_GAODE_BASE_MAP,
  defaultRasterQuickViewBaseMap,
  isGaodeRasterQuickViewBaseMap,
  isTiandituRasterQuickViewBaseMap,
  rasterQuickViewBaseMapOptions
} from '@/utils/rasterQuickViewBaseMap'
import {
  rasterDisplayRangeFromGDALMetadata,
  rasterDisplayRangeFromMeta,
  rasterDisplayRangeFromSamples,
  rasterSampleSize
} from '@/utils/rasterDisplayRange'
import {
  DEFAULT_RASTER_MOSAIC_GAMMA,
  rasterMosaicTileURLWithStyle
} from '@/utils/rasterMosaicStyle'

const props = defineProps({
  status: {
    type: Object,
    required: true
  },
  viewState: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['view-state-change'])

const { t } = useI18n()
const { mapConfig, baseMapOptions, loadMapConfig } = useMapConfig()

const mapEl = ref(null)
const loading = ref(false)
const error = ref('')
const baseMapType = ref('')
const rasterOpacityPercent = ref(82)
const rasterMosaicGammaPercent = ref(Math.round(DEFAULT_RASTER_MOSAIC_GAMMA * 100))
const rasterMosaicInvert = ref(false)
let map = null
let rasterLayer = null
let rasterSource = null
let baseLayers = []
let sizeUpdateFrame = null
let sourceReadySeq = 0
let displayRangeSeq = 0
let rasterMosaicTileBaseURL = ''
let mapMoveEndKey = null

const quickViewInfo = computed(() => props.status?.quick_view || {})
const rasterInfo = computed(() => props.status?.raster || {})
const rasterMosaicInfo = computed(() => props.status?.raster_mosaic || {})

const rasterURL = computed(() => {
  return String(quickViewInfo.value.preview_url || '').trim()
})
const tileURLTemplate = computed(() => {
  return String(quickViewInfo.value.tile_url_template || '').trim()
})
const styledTileURLTemplate = computed(() => {
  return rasterMosaicTileURLWithStyle(tileURLTemplate.value, {
    gamma: rasterMosaicGammaPercent.value / 100,
    invert: rasterMosaicInvert.value
  })
})
const renderSource = computed(() => String(
  props.status?.render_source || quickViewInfo.value.render_source || ''
).trim())
const isMosaicQuickView = computed(() => renderSource.value === 'raster_mosaic_tile')
const rasterOpacity = computed(() => rasterOpacityPercent.value / 100)
const rasterBaseMapOptions = computed(() => {
  return rasterQuickViewBaseMapOptions(baseMapOptions.value)
})
const rasterBandCount = computed(() => Number(rasterInfo.value.band_count || 0))
const rasterTitle = computed(() => isMosaicQuickView.value
  ? t('manager.spatialPreview.rasterMosaicQuickView')
  : t('manager.spatialPreview.rasterTIFFQuickView')
)

const rasterSummary = computed(() => {
  if (isMosaicQuickView.value) {
    const leafCount = Number(rasterMosaicInfo.value.leaf_count || 0)
    const sourceCount = Number(rasterMosaicInfo.value.source_count || 0)
    const overviewWidth = Number(rasterMosaicInfo.value.overview_width || 0)
    const overviewHeight = Number(rasterMosaicInfo.value.overview_height || 0)
    const parts = ['raster_mosaic']
    if (leafCount > 0) parts.push(`${leafCount} COG`)
    if (sourceCount > 0 && sourceCount !== leafCount) parts.push(`${sourceCount} sources`)
    if (overviewWidth > 0 && overviewHeight > 0) parts.push(`${overviewWidth} x ${overviewHeight}`)
    return parts.join(' · ')
  }
  const profile = String(rasterInfo.value.profile || '').trim()
  const width = Number(rasterInfo.value.width || 0)
  const height = Number(rasterInfo.value.height || 0)
  const size = Number(rasterInfo.value.size_bytes || 0)
  const parts = []
  if (profile) parts.push(profile)
  if (width > 0 && height > 0) parts.push(`${width} x ${height}`)
  if (size > 0) parts.push(formatBytes(size))
  return parts.join(' · ')
})

function formatBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return ''
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`
}

function renderExtentInWebMercator() {
  const extent = quickViewInfo.value.extent
  const srid = Number(quickViewInfo.value.extent_srid || 0)
  if (!Array.isArray(extent) || extent.length !== 4 || srid !== 4326) return null
  const [minX, minY, maxX, maxY] = extent.map(Number)
  if (![minX, minY, maxX, maxY].every(Number.isFinite)) return null
  const minCorner = fromLonLat([minX, minY])
  const maxCorner = fromLonLat([maxX, maxY])
  return [...minCorner, ...maxCorner]
}

function fitToExtent({ duration = 0 } = {}) {
  const extent = renderExtentInWebMercator()
  if (!map || !extent) return false
  map.getView().fit(extent, {
    padding: [48, 48, 48, 48],
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

async function applyRasterSourceView(source, seq) {
  if (!source || !map) return false
  try {
    await source.getView()
    if (seq !== sourceReadySeq || !map) return false
    const savedMapViewState = finiteMapViewState()
    if (savedMapViewState) {
      applyMapViewState(savedMapViewState)
      scheduleMapSizeUpdate()
      return true
    }
    fitToExtent()
    scheduleMapSizeUpdate()
    return true
  } catch (err) {
    console.warn('GeoTIFF source view is unavailable:', err)
    return false
  }
}

function scheduleMapSizeUpdate() {
  if (!map || typeof window === 'undefined') return
  if (sizeUpdateFrame) window.cancelAnimationFrame(sizeUpdateFrame)
  sizeUpdateFrame = window.requestAnimationFrame(() => {
    sizeUpdateFrame = null
    if (map) map.updateSize()
  })
}

function ensureBaseMapSelection() {
  if (baseMapType.value) return
  baseMapType.value = defaultRasterQuickViewBaseMap(baseMapOptions.value)
}

function createSelectedBaseLayers() {
  const selected = String(baseMapType.value || RASTER_QUICK_VIEW_GAODE_BASE_MAP)
  if (isTiandituRasterQuickViewBaseMap(selected)) {
    const layers = createTiandituBaseLayers({
      key: mapConfig.value?.tdtKey,
      type: selected === 'tiandituImage' ? 'image' : 'vector',
      zIndex: 0,
      labelZIndex: 20,
      maxZoom: 18
    })
    if (layers.length) return layers
  }
  if (isGaodeRasterQuickViewBaseMap(selected)) {
    return [createGaodeBaseLayer({ zIndex: 0, maxZoom: 18 })]
  }
  return [createGaodeBaseLayer({ zIndex: 0, maxZoom: 18 })]
}

function createRasterSourceOptions() {
  return rasterGeoTIFFSourceOptions(renderSource.value, localStorage.getItem('token') || '')
}

async function selectSmallestImage(tiff) {
  const count = typeof tiff?.getImageCount === 'function' ? await tiff.getImageCount() : 1
  let selected = null
  let selectedPixels = Number.POSITIVE_INFINITY
  for (let index = 0; index < Math.max(1, count); index += 1) {
    const image = await tiff.getImage(index)
    const width = Number(image.getWidth?.() || 0)
    const height = Number(image.getHeight?.() || 0)
    const pixels = width > 0 && height > 0 ? width * height : Number.POSITIVE_INFINITY
    if (!selected || pixels < selectedPixels) {
      selected = image
      selectedPixels = pixels
    }
  }
  return selected
}

async function loadRasterDisplayRange(url, sourceOptions, seq) {
  if (!url || seq !== displayRangeSeq) return null
  try {
    const tiff = await tiffFromUrl(url, sourceOptions)
    if (seq !== displayRangeSeq) return null
    const image = await selectSmallestImage(tiff)
    if (!image || seq !== displayRangeSeq) return null
    const samplesPerPixel = Number(image.getSamplesPerPixel?.() || rasterBandCount.value || 0)
    if (samplesPerPixel >= 3) return null
    const noDataValue = Number(image.getGDALNoData?.())
    const metadataRange = rasterDisplayRangeFromGDALMetadata(
      image.getGDALMetadata?.(0) || image.getGDALMetadata?.(),
      noDataValue
    )
    if (metadataRange) return metadataRange
    const sampleSize = rasterSampleSize(image.getWidth?.(), image.getHeight?.())
    const samples = await image.readRasters({
      samples: [0],
      width: sampleSize.width,
      height: sampleSize.height,
      interleave: true
    })
    if (seq !== displayRangeSeq) return null
    return rasterDisplayRangeFromSamples(samples, noDataValue)
  } catch (err) {
    console.warn('GeoTIFF display range probe failed:', err)
    return null
  }
}

function replaceBaseLayers() {
  if (!map) return
  baseLayers.forEach((layer) => map.removeLayer(layer))
  baseLayers = createSelectedBaseLayers()
  baseLayers.forEach((layer, index) => {
    map.getLayers().insertAt(index, layer)
  })
  scheduleMapSizeUpdate()
}

async function createRasterLayer() {
  if (isMosaicQuickView.value) {
    return createRasterMosaicLayer()
  }
  const url = rasterURL.value
  if (!url) return null
  const seq = sourceReadySeq
  displayRangeSeq += 1
  const rangeSeq = displayRangeSeq
  const sourceOptions = createRasterSourceOptions()
  const displayRange = rasterDisplayRangeFromMeta(rasterInfo.value) ||
    await loadRasterDisplayRange(url, sourceOptions, rangeSeq)
  if (seq !== sourceReadySeq) return null
  const sourceInfo = { url }
  if (displayRange) {
    sourceInfo.min = displayRange.min
    sourceInfo.max = displayRange.max
    if (displayRange.nodata !== undefined) sourceInfo.nodata = displayRange.nodata
  }
  const projection = rasterGeoTIFFProjectionFromQuickView(quickViewInfo.value)
  const sourceConfig = {
    sources: [sourceInfo],
    sourceOptions,
    convertToRGB: 'auto',
    normalize: true,
    interpolate: true
  }
  if (projection) {
    sourceConfig.projection = projection
  }
  const source = new GeoTIFFSource(sourceConfig)
  source.on('change', () => {
    if (seq !== sourceReadySeq) return
    const state = typeof source.getState === 'function' ? source.getState() : ''
    if (state === 'ready') {
      loading.value = false
      nextTick(async () => {
        scheduleMapSizeUpdate()
        if (!(await applyRasterSourceView(source, seq))) {
          fitToExtent()
        }
      })
    } else if (state === 'error') {
      loading.value = false
      const sourceError = typeof source.getError === 'function' ? source.getError() : null
      if (sourceError) {
        console.error('GeoTIFF source failed:', sourceError)
      }
      error.value = t('manager.spatialPreview.loadRasterQuickViewFailed')
    }
  })
  source.on('error', () => {
    loading.value = false
    error.value = t('manager.spatialPreview.loadRasterQuickViewFailed')
  })
  rasterSource = source
  return new WebGLTileLayer({
    source,
    opacity: rasterOpacity.value,
    style: {
      contrast: 0.18,
      exposure: 0.08,
      gamma: 0.9
    },
    zIndex: 10
  })
}

function createRasterMosaicLayer() {
  const url = styledTileURLTemplate.value
  if (!url) return null
  rasterMosaicTileBaseURL = tileURLTemplate.value
  const source = new XYZ({
    url,
    minZoom: Number(quickViewInfo.value.min_zoom || 0),
    maxZoom: Number(quickViewInfo.value.max_zoom || 18),
    tileSize: 256,
    crossOrigin: 'anonymous',
    transition: 120
  })
  source.setTileLoadFunction(createRasterMosaicTileLoadFunction())
  source.on('tileloadend', () => {
    loading.value = false
    error.value = ''
  })
  source.on('tileloaderror', () => {
    loading.value = false
    error.value = t('manager.spatialPreview.loadRasterQuickViewFailed')
  })
  rasterSource = source
  loading.value = false
  return new TileLayer({
    source,
    opacity: rasterOpacity.value,
    zIndex: 10
  })
}

function updateRasterMosaicStyleURL(url) {
  if (!isMosaicQuickView.value) {
    reload()
    return
  }
  if (!map || !rasterSource || typeof rasterSource.setUrl !== 'function') {
    reload()
    return
  }
  if (rasterMosaicTileBaseURL !== tileURLTemplate.value) {
    reload()
    return
  }
  error.value = ''
  rasterSource.setUrl(url)
  if (typeof rasterSource.refresh === 'function') {
    rasterSource.refresh()
  }
  scheduleMapSizeUpdate()
}

function createRasterMosaicTileLoadFunction() {
  return (tile, src) => {
    const image = tile.getImage()
    const token = localStorage.getItem('token') || ''
    fetch(src, {
      headers: token ? { Authorization: `Bearer ${token}` } : {}
    })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        return res.blob()
      })
      .then((blob) => {
        const objectURL = URL.createObjectURL(blob)
        image.onload = () => URL.revokeObjectURL(objectURL)
        image.onerror = () => {
          URL.revokeObjectURL(objectURL)
          tile.setState(TileState.ERROR)
        }
        image.src = objectURL
      })
      .catch((err) => {
        console.error('Raster mosaic tile load failed:', err)
        tile.setState(TileState.ERROR)
      })
  }
}

async function initMap() {
  error.value = ''
  loading.value = true
  sourceReadySeq += 1
  const seq = sourceReadySeq
  await loadMapConfig()
  if (sourceReadySeq !== seq) return
  ensureBaseMapSelection()
  if (!mapEl.value) return

  baseLayers = createSelectedBaseLayers()
  rasterLayer = await createRasterLayer()
  if (sourceReadySeq !== seq) return
  if (!rasterLayer) {
    loading.value = false
    error.value = t('manager.spatialPreview.missingQuickViewURL')
    return
  }

  const controls = defaultControls({ zoom: true })
  const extentForControl = renderExtentInWebMercator()
  if (extentForControl) {
    controls.push(new ZoomToExtent({
      extent: extentForControl,
      tipLabel: t('manager.vectorTile.fitExtent'),
      label: '⛶'
    }))
  }

  map = new Map({
    target: mapEl.value,
    layers: [...baseLayers, rasterLayer],
    interactions: defaultInteractions({ mouseWheelZoom: false }).extend([
      new MouseWheelZoom({
        duration: 100,
        timeout: 100,
        useAnchor: true
      })
    ]),
    controls,
    view: new View({
      center: fromLonLat(finiteMapViewState()?.center || centerFromExtent()),
      zoom: finiteMapViewState()?.zoom || 8,
      maxZoom: 19,
      minZoom: 1,
      enableRotation: false
    })
  })
  mapMoveEndKey = map.on('moveend', emitMapViewState)
  scheduleMapSizeUpdate()
  nextTick(async () => {
    const savedMapViewState = finiteMapViewState()
    if (isMosaicQuickView.value) {
      if (!applyMapViewState(savedMapViewState)) {
        fitToExtent()
      }
      return
    }
    if (!(await applyRasterSourceView(rasterSource, seq))) {
      fitToExtent()
    }
  })
}

function centerFromExtent() {
  const extent = quickViewInfo.value.extent
  if (Array.isArray(extent) && extent.length === 4 && Number(quickViewInfo.value.extent_srid || 0) === 4326) {
    const [minX, minY, maxX, maxY] = extent.map(Number)
    if ([minX, minY, maxX, maxY].every(Number.isFinite)) {
      return [(minX + maxX) / 2, (minY + maxY) / 2]
    }
  }
  return [105, 35]
}

function disposeMap() {
  if (mapMoveEndKey) {
    unByKey(mapMoveEndKey)
    mapMoveEndKey = null
  }
  if (map) {
    map.setTarget(null)
    map = null
  }
  rasterLayer = null
  rasterSource = null
  baseLayers = []
  rasterMosaicTileBaseURL = ''
  sourceReadySeq += 1
  displayRangeSeq += 1
  if (sizeUpdateFrame && typeof window !== 'undefined') {
    window.cancelAnimationFrame(sizeUpdateFrame)
    sizeUpdateFrame = null
  }
}

async function reload() {
  disposeMap()
  await nextTick()
  await initMap()
}

onMounted(initMap)
onBeforeUnmount(disposeMap)

watch(rasterURL, reload)
watch(styledTileURLTemplate, updateRasterMosaicStyleURL)
watch(baseMapOptions, ensureBaseMapSelection, { immediate: true })
watch(baseMapType, () => {
  replaceBaseLayers()
})
watch(rasterOpacity, (opacity) => {
  if (rasterLayer) {
    rasterLayer.setOpacity(opacity)
  }
})
</script>

<style scoped>
.raster-tiff-quick-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 320px;
}

.raster-toolbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--el-border-color-light);
  background: var(--addp-bg-primary);
}

.raster-heading {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 10px;
}

.raster-title {
  flex: 0 0 auto;
  font-size: 13px;
  color: var(--addp-text-secondary);
}

.raster-summary {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.raster-controls {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 10px;
}

.base-map-select {
  width: 150px;
}

.opacity-control {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 180px;
}

.gamma-control {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 150px;
}

.opacity-label {
  flex: 0 0 auto;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.opacity-slider {
  flex: 1 1 auto;
}

.gamma-slider {
  flex: 1 1 auto;
}

.invert-checkbox {
  flex: 0 0 auto;
}

.raster-map-wrap {
  position: relative;
  flex: 1;
  min-height: 0;
  background: var(--addp-bg-secondary);
}

.raster-map {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}

.map-empty {
  position: absolute;
  inset: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--addp-bg-primary);
}

.loading-overlay {
  position: absolute;
  inset: 0;
  z-index: 30;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--addp-bg-primary);
}

.loading-content {
  text-align: center;
  color: var(--addp-text-secondary);
}

.loading-spinner {
  width: 32px;
  height: 32px;
  margin: 0 auto 10px;
  border: 3px solid var(--addp-border-color-light);
  border-top-color: var(--el-color-primary);
  border-radius: 50%;
  animation: spin 1s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>
