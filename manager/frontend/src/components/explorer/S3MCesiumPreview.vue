<template>
  <div class="s3m-preview">
    <div ref="viewportRef" class="s3m-viewport" />
    <div v-if="!loading && !errorMessage" class="s3m-view-mode">
      <el-segmented
        v-model="viewMode"
        :options="viewModeOptions"
        size="small"
        :aria-label="t('manager.explorer.s3mViewMode')"
      />
    </div>
    <div v-if="loading" class="s3m-status">{{ t('manager.explorer.s3mLoading') }}</div>
    <div v-else-if="errorMessage" class="s3m-status is-error">{{ errorMessage }}</div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import 'cesium/Build/Cesium/Widgets/widgets.css'
import {
  cameraViewState,
  isS3MViewStateForMode,
  normalizeS3MViewMode,
  preserveDerivedResourceQuery,
  S3M_VIEW_MODE_GLOBE,
  S3M_VIEW_MODE_MODEL,
  s3mGlobeCameraRange,
  s3mRootBoundingSphere
} from '@/utils/s3mViewState'

const props = defineProps({
  data: { type: Object, required: true },
  viewState: { type: Object, default: () => ({}) }
})
const emit = defineEmits(['view-state-change'])
const { t } = useI18n()

const viewportRef = ref(null)
const loading = ref(false)
const errorMessage = ref('')
const viewMode = ref(normalizeS3MViewMode(props.viewState?.view_mode))
const content = computed(() => props.data?.object?.content || {})
const metadata = computed(() => content.value?.metadata || {})
const sourceURL = computed(() => content.value?.url || props.data?.object?.url || '')
const viewModeOptions = computed(() => [
  { label: t('manager.explorer.s3mModelView'), value: S3M_VIEW_MODE_MODEL },
  { label: t('manager.explorer.s3mGlobeView'), value: S3M_VIEW_MODE_GLOBE }
])

let viewer = null
let layer = null
let CesiumRuntime = null
let globeImageryLayer = null
let rootBounds = null
let loadSerial = 0

function cesiumBaseURL() {
  const base = import.meta.env.BASE_URL || '/'
  return new URL(`${base.endsWith('/') ? base : `${base}/`}cesium/`, window.location.origin).href
}

function authenticatedResource(Cesium, url) {
  const token = localStorage.getItem('token')
  const resource = new Cesium.Resource({
    url,
    headers: token ? { Authorization: `Bearer ${token}` } : {}
  })
  const version = new URL(url, window.location.origin).searchParams.get('version')
  if (version) resource.setQueryParameters({ version })
  return preserveDerivedResourceQuery(resource)
}

function installCesiumGlobal(Cesium) {
  const defaultValue = (value, fallback) => value === undefined || value === null ? fallback : value
  defaultValue.EMPTY_OBJECT = Object.freeze({})
  window.CESIUM_BASE_URL = cesiumBaseURL()
  window.Cesium = { ...Cesium, defaultValue }
}

function applyViewState(Cesium) {
  const state = props.viewState
  if (!viewer || !isS3MViewStateForMode(state, viewMode.value) || !Array.isArray(state.position)) return false
  const position = state.position.map(Number)
  if (position.length < 3 || !position.every(Number.isFinite)) return false
  viewer.camera.setView({
    destination: new Cesium.Cartesian3(position[0], position[1], position[2]),
    orientation: {
      heading: Number(state.heading) || 0,
      pitch: Number(state.pitch) || -Math.PI / 2,
      roll: Number(state.roll) || 0
    }
  })
  return true
}

function emitViewState() {
  if (!viewer) return
  const state = cameraViewState(viewer.camera)
  if (state) emit('view-state-change', { ...state, view_mode: viewMode.value })
}

async function installGlobeImagery(Cesium, currentLoad) {
  try {
    const provider = await Cesium.TileMapServiceImageryProvider.fromUrl(
      `${cesiumBaseURL()}Assets/Textures/NaturalEarthII`
    )
    if (currentLoad !== loadSerial || !viewer || viewer.isDestroyed()) return
    globeImageryLayer = viewer.imageryLayers.addImageryProvider(provider)
    globeImageryLayer.show = viewMode.value === S3M_VIEW_MODE_GLOBE
  } catch (error) {
    console.warn('S3M globe imagery failed to load:', error)
  }
}

function applySceneEnvironment() {
  if (!viewer || viewer.isDestroyed()) return
  const globeVisible = viewMode.value === S3M_VIEW_MODE_GLOBE
  viewer.scene.globe.show = globeVisible
  viewer.scene.globe.depthTestAgainstTerrain = false
  viewer.scene.skyAtmosphere.show = globeVisible
  viewer.scene.skyBox.show = globeVisible
  if (globeImageryLayer) globeImageryLayer.show = globeVisible
}

function focusCurrentMode() {
  if (!viewer || viewer.isDestroyed() || !CesiumRuntime || !rootBounds) return
  if (viewMode.value === S3M_VIEW_MODE_GLOBE) {
    viewer.camera.flyToBoundingSphere(rootBounds, {
      duration: 0.6,
      offset: new CesiumRuntime.HeadingPitchRange(
        0,
        -CesiumRuntime.Math.PI_OVER_FOUR,
        s3mGlobeCameraRange(rootBounds.radius)
      )
    })
    return
  }
  viewer.camera.flyToBoundingSphere(rootBounds, { duration: 0.6 })
}

function handleViewModeChange() {
  applySceneEnvironment()
  focusCurrentMode()
}

async function loadS3M(url) {
  const currentLoad = ++loadSerial
  loading.value = true
  errorMessage.value = ''
  viewMode.value = normalizeS3MViewMode(props.viewState?.view_mode)
  await nextTick()
  disposeViewer()
  if (!url || !viewportRef.value) {
    loading.value = false
    errorMessage.value = t('manager.explorer.s3mMissingURL')
    return
  }
  try {
    const Cesium = await import('cesium')
    CesiumRuntime = Cesium
    installCesiumGlobal(Cesium)
    const { default: S3MTilesLayer } = await import('@/lib/supermap-s3m/S3MTiles/S3MTilesLayer.js?renderer=webp-mips-v2')
    if (currentLoad !== loadSerial) return
    viewer = new Cesium.Viewer(viewportRef.value, {
      animation: false,
      baseLayer: false,
      baseLayerPicker: false,
      fullscreenButton: false,
      geocoder: false,
      homeButton: false,
      infoBox: false,
      navigationHelpButton: false,
      sceneModePicker: false,
      selectionIndicator: false,
      timeline: false
    })
    viewer.scene.globe.show = false
    viewer.scene.skyAtmosphere.show = false
    viewer.scene.skyBox.show = false
    void installGlobeImagery(Cesium, currentLoad)
    const encoding = String(metadata.value.manifest_encoding || '').toLowerCase()
    const extension = String(metadata.value.tile_extension || '').toLowerCase()
    layer = new S3MTilesLayer({
      context: viewer.scene._context,
      url: authenticatedResource(Cesium, url),
      isS3MB: encoding === 'json' || extension === '.s3mb',
      selectEnabled: false
    })
    viewer.scene.primitives.add(layer)
    await layer.readyPromise
    if (currentLoad !== loadSerial) return
    rootBounds = s3mRootBoundingSphere(Cesium, layer._rootTiles)
    applySceneEnvironment()
    if (!applyViewState(Cesium) && rootBounds) {
      focusCurrentMode()
    }
    viewer.camera.moveEnd.addEventListener(emitViewState)
    loading.value = false
  } catch (error) {
    if (currentLoad !== loadSerial) return
    console.error('S3M preview failed:', error)
    errorMessage.value = t('manager.explorer.s3mLoadFailed', { error: error?.message || error })
    loading.value = false
  }
}

function disposeViewer() {
  const currentViewer = viewer
  const currentLayer = layer
  viewer = null
  layer = null
  CesiumRuntime = null
  globeImageryLayer = null
  rootBounds = null
  if (currentViewer && !currentViewer.isDestroyed()) {
    currentViewer.useDefaultRenderLoop = false
    currentViewer.camera?.moveEnd?.removeEventListener(emitViewState)
    if (currentLayer && !currentLayer.isDestroyed?.()) {
      currentViewer.scene.primitives.remove(currentLayer)
    }
    currentViewer.destroy()
  }
}

watch(sourceURL, loadS3M, { immediate: true })
watch(viewMode, handleViewModeChange)
onBeforeUnmount(() => {
  loadSerial++
  disposeViewer()
})
</script>

<style scoped>
.s3m-preview {
  position: relative;
  min-height: 460px;
  height: min(68vh, 760px);
  overflow: hidden;
  background: linear-gradient(180deg, color-mix(in srgb, var(--addp-bg-secondary) 82%, transparent), var(--addp-bg-primary));
  border: 1px solid var(--addp-border-color-light);
}
.s3m-viewport { width: 100%; height: 100%; }
.s3m-viewport :deep(.cesium-viewer),
.s3m-viewport :deep(.cesium-viewer-cesiumWidgetContainer),
.s3m-viewport :deep(.cesium-widget),
.s3m-viewport :deep(canvas) { width: 100%; height: 100%; }
.s3m-view-mode {
  position: absolute;
  top: 12px;
  right: 12px;
  z-index: 2;
  padding: 4px;
  background: color-mix(in srgb, var(--addp-bg-primary) 88%, transparent);
  border: 1px solid var(--addp-border-color-light);
  border-radius: 6px;
  box-shadow: var(--addp-shadow-card);
}
.s3m-view-mode :deep(.el-segmented) { min-width: 112px; }
.s3m-status {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
  color: var(--addp-text-secondary);
  background: color-mix(in srgb, var(--addp-bg-primary) 70%, transparent);
}
.s3m-status.is-error { color: var(--el-color-danger); }
</style>
