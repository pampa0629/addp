<template>
  <div class="raster-tiff-quick-view">
    <div class="raster-toolbar">
      <div class="raster-heading">
        <span class="raster-title">{{ t('manager.spatialPreview.rasterTIFFQuickView') }}</span>
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
import GeoTIFFSource from 'ol/source/GeoTIFF'
import WebGLTileLayer from 'ol/layer/WebGLTile.js'
import { defaults as defaultControls, ZoomToExtent } from 'ol/control.js'
import { defaults as defaultInteractions, MouseWheelZoom } from 'ol/interaction.js'
import { fromLonLat, createGaodeBaseLayer, createTiandituBaseLayers, useMapConfig } from '@common-ui-map'
import { rasterGeoTIFFSourceOptions } from '@/utils/rasterGeoTIFFSourceOptions'
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

const props = defineProps({
  status: {
    type: Object,
    required: true
  }
})

const { t } = useI18n()
const { mapConfig, baseMapOptions, loadMapConfig } = useMapConfig()

const mapEl = ref(null)
const loading = ref(false)
const error = ref('')
const baseMapType = ref('')
const rasterOpacityPercent = ref(82)
let map = null
let rasterLayer = null
let rasterSource = null
let baseLayers = []
let sizeUpdateFrame = null
let sourceReadySeq = 0
let displayRangeSeq = 0

const quickViewInfo = computed(() => props.status?.quick_view || {})
const rasterInfo = computed(() => props.status?.raster || {})

const rasterURL = computed(() => {
  return String(quickViewInfo.value.preview_url || '').trim()
})
const renderSource = computed(() => String(
  props.status?.render_source || quickViewInfo.value.render_source || ''
).trim())
const rasterOpacity = computed(() => rasterOpacityPercent.value / 100)
const rasterBaseMapOptions = computed(() => {
  return rasterQuickViewBaseMapOptions(baseMapOptions.value)
})
const rasterBandCount = computed(() => Number(rasterInfo.value.band_count || 0))

const rasterSummary = computed(() => {
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

async function applyRasterSourceView(source, seq) {
  if (!source || !map) return false
  try {
    await source.getView()
    if (seq !== sourceReadySeq || !map) return false
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
  const source = new GeoTIFFSource({
    sources: [sourceInfo],
    sourceOptions,
    convertToRGB: 'auto',
    normalize: true,
    interpolate: true
  })
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
      center: fromLonLat(centerFromExtent()),
      zoom: 8,
      maxZoom: 19,
      minZoom: 1,
      enableRotation: false
    })
  })
  scheduleMapSizeUpdate()
  nextTick(async () => {
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
  if (map) {
    map.setTarget(null)
    map = null
  }
  rasterLayer = null
  rasterSource = null
  baseLayers = []
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

.opacity-label {
  flex: 0 0 auto;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.opacity-slider {
  flex: 1 1 auto;
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
