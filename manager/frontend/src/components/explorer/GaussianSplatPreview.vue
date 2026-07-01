<template>
  <div class="three-preview gaussian-splat-preview">
    <div ref="viewportRef" class="three-viewport" />
    <div class="three-toolbar">
      <el-radio-group
        class="quality-mode"
        size="small"
        :model-value="qualityMode"
        :disabled="loading"
        :title="t('manager.spatialPreview.gaussianSplatQualityMode')"
        :aria-label="t('manager.spatialPreview.gaussianSplatQualityMode')"
        @change="setQualityMode"
      >
        <el-radio-button label="smooth">{{ t('manager.spatialPreview.gaussianSplatQualitySmooth') }}</el-radio-button>
        <el-radio-button label="standard">{{ t('manager.spatialPreview.gaussianSplatQualityStandard') }}</el-radio-button>
        <el-radio-button label="sharp">{{ t('manager.spatialPreview.gaussianSplatQualitySharp') }}</el-radio-button>
      </el-radio-group>
      <el-button
        circle
        size="small"
        :disabled="!previewReady"
        :title="t('manager.spatialPreview.gaussianSplatTopView')"
        :aria-label="t('manager.spatialPreview.gaussianSplatTopView')"
        @click="applyCameraFrame('top')"
      >
        <el-icon><Aim /></el-icon>
      </el-button>
      <el-button
        circle
        size="small"
        :disabled="!previewReady"
        :title="t('manager.spatialPreview.gaussianSplatLevelView')"
        :aria-label="t('manager.spatialPreview.gaussianSplatLevelView')"
        @click="applyCameraFrame('level')"
      >
        <el-icon><View /></el-icon>
      </el-button>
    </div>
    <div v-if="loading" class="three-status">{{ loadingText }}</div>
    <div v-else-if="errorMessage" class="three-status is-error">{{ errorMessage }}</div>
    <div v-if="summaryItems.length" class="three-summary">
      <span v-for="item in summaryItems" :key="item.label">{{ item.label }}: {{ item.value }}</span>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Aim, View } from '@element-plus/icons-vue'

const props = defineProps({
  data: {
    type: Object,
    required: true
  },
  loading: {
    type: Boolean,
    default: false
  },
  viewState: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['view-state-change'])

const { t } = useI18n()
const viewportRef = ref(null)
const loading = ref(false)
const errorMessage = ref('')
const lastDiagnostic = ref(null)
const loadProgress = ref(null)
const previewReady = ref(false)
const qualityMode = ref('smooth')
const loadingText = computed(() => {
  const percent = Number(loadProgress.value)
  const progressText = Number.isFinite(percent) && percent > 0
    ? ` ${Math.floor(percent)}%`
    : ''
  const largeNotice = isLargeDirectPLY.value
    ? `\n${t('manager.spatialPreview.largeGaussianSplatDirectPreviewNotice')}`
    : ''
  return `${t('manager.spatialPreview.loadingGaussianSplat')}${progressText}${largeNotice}`
})

let viewer = null
let loadToken = 0
let gaussianSplats3DModule = null
let interactionControls = null
let interactionRestoreTimer = null
let interactionTemporarilyReduced = false

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const metadata = computed(() => content.value?.metadata || {})
const gaussianInfo = computed(() => metadata.value?.gaussian_splat || {})
const formatInfo = computed(() => metadata.value?.format_info || {})
const storageInfo = computed(() => objectData.value?.attributes?.storage || {})

const splatURL = computed(() => content.value?.url || objectData.value?.url || '')
const sourceURL = computed(() => withAuthToken(splatURL.value))
const sourceFormat = computed(() => String(
  metadata.value?.format ||
  gaussianInfo.value?.format ||
  objectData.value?.attributes?.item?.format ||
  objectData.value?.format ||
  ''
).trim().toLowerCase())
const splatCount = computed(() => Number(gaussianInfo.value?.splat_count || 0))
const sourceSizeBytes = computed(() => Number(
  gaussianInfo.value?.size_bytes ||
  objectData.value?.size_bytes ||
  storageInfo.value?.total_size ||
  storageInfo.value?.size ||
  0
))
const isLargeDirectPLY = computed(() => (
  sourceFormat.value === 'ply' &&
  (splatCount.value >= 2_000_000 || sourceSizeBytes.value >= 150 * 1024 * 1024)
))

const summaryItems = computed(() => {
  const items = []
  if (splatCount.value > 0) items.push({ label: t('manager.spatialPreview.gaussianSplatCount'), value: splatCount.value.toLocaleString() })
  const format = sourceFormat.value.toUpperCase()
  if (format) items.push({ label: t('manager.spatialPreview.gaussianSplatFormat'), value: format })
  const shDegree = Number(gaussianInfo.value?.sh_degree)
  if (Number.isFinite(shDegree) && shDegree >= 0) items.push({ label: t('manager.spatialPreview.gaussianSplatSHDegree'), value: shDegree })
  if (sourceSizeBytes.value > 0) items.push({ label: t('manager.spatialPreview.gaussianSplatSourceSize'), value: formatByteCount(sourceSizeBytes.value) })
  return items
})

function formatByteCount(value) {
  const bytes = Number(value || 0)
  if (!Number.isFinite(bytes) || bytes <= 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = bytes
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex += 1
  }
  return `${size.toFixed(unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`
}

function withAuthToken(url) {
  if (!url || typeof url !== 'string') return ''
  if (!url.startsWith('/api/') && !url.startsWith('/manager/')) return url
  const token = localStorage.getItem('token')
  if (!token) return url
  try {
    const parsed = new URL(url, window.location.origin)
    if (!parsed.searchParams.has('token')) {
      parsed.searchParams.set('token', token)
    }
    return parsed.origin === window.location.origin
      ? `${parsed.pathname}${parsed.search}${parsed.hash}`
      : parsed.toString()
  } catch {
    const separator = url.includes('?') ? '&' : '?'
    return `${url}${separator}token=${encodeURIComponent(token)}`
  }
}

async function loadGaussianSplats3D() {
  if (!gaussianSplats3DModule) {
    gaussianSplats3DModule = await import('@mkkellogg/gaussian-splats-3d')
  }
  return gaussianSplats3DModule
}

function sceneFormat(GaussianSplats3D) {
  switch (sourceFormat.value) {
    case 'splat':
      return GaussianSplats3D.SceneFormat.Splat
    case 'ksplat':
      return GaussianSplats3D.SceneFormat.KSplat
    case 'ply':
    default:
      return GaussianSplats3D.SceneFormat.Ply
  }
}

function isSupportedSourceFormat() {
  return ['ply', 'splat', 'ksplat'].includes(sourceFormat.value)
}

function defaultQualityMode() {
  return isLargeDirectPLY.value ? 'smooth' : 'standard'
}

function initialQualityMode() {
  const mode = String(props.viewState?.quality_mode || '').trim()
  return ['smooth', 'standard', 'sharp'].includes(mode) ? mode : defaultQualityMode()
}

function qualityProfile() {
  return {
    ignoreDevicePixelRatio: isLargeDirectPLY.value,
    sortPrecision: isLargeDirectPLY.value ? 18 : 20,
    maxScreenSpaceSplatSize: sourceFormat.value === 'splat' ? 384 : (isLargeDirectPLY.value ? 384 : 512),
    maxSphericalHarmonicsDegree: isLargeDirectPLY.value ? 1 : 2
  }
}

function sphericalHarmonicsDegree(profile) {
  const value = Number(gaussianInfo.value?.sh_degree)
  if (!Number.isFinite(value) || value < 0) return 0
  return Math.min(Math.floor(value), profile.maxSphericalHarmonicsDegree)
}

function isCompressedPLY() {
  return sourceFormat.value === 'ply' && formatInfo.value?.ply?.is_compressed_splat === true
}

function shouldProgressivelyLoad() {
  if (isCompressedPLY()) return false
  if (sourceFormat.value === 'ksplat') {
    const order = String(gaussianInfo.value?.progressive_order || '').trim()
    return order === '' || order === 'center_first'
  }
  return true
}

function bounds3D() {
  const bounds = gaussianInfo.value?.bounds_3d || gaussianInfo.value?.sampled_bounds_3d || {}
  const minX = Number(bounds.min_x)
  const minY = Number(bounds.min_y)
  const minZ = Number(bounds.min_z)
  const maxX = Number(bounds.max_x)
  const maxY = Number(bounds.max_y)
  const maxZ = Number(bounds.max_z)
  if (![minX, minY, minZ, maxX, maxY, maxZ].every(Number.isFinite)) return null
  if (minX > maxX || minY > maxY || minZ > maxZ) return null
  return { minX, minY, minZ, maxX, maxY, maxZ }
}

function cameraFrame() {
  const bounds = bounds3D()
  if (!bounds) {
    return {
      lookAt: [0, 0, 0],
      position: [-1, -4, 6]
    }
  }
  return cameraFrameFromBounds(bounds)
}

function cameraFrameFromBounds(bounds) {
  return cameraFrameFromBoundsWithMode(bounds, 'oblique')
}

function cameraFrameFromBoundsWithMode(bounds, mode) {
  const center = [
    (bounds.minX + bounds.maxX) / 2,
    (bounds.minY + bounds.maxY) / 2,
    (bounds.minZ + bounds.maxZ) / 2
  ]
  const extent = [
    Math.max(0, bounds.maxX - bounds.minX),
    Math.max(0, bounds.maxY - bounds.minY),
    Math.max(0, bounds.maxZ - bounds.minZ)
  ]
  const radius = Math.max(extent[0], extent[1], extent[2], 1)
  const distance = radius * (mode === 'level' ? 2.2 : 1.9)
  if (mode === 'top') {
    return {
      lookAt: center,
      position: [
        center[0],
        center[1] - Math.max(radius * 0.02, 0.01),
        center[2] + distance
      ],
      up: [0, 1, 0]
    }
  }
  if (mode === 'level') {
    return {
      lookAt: center,
      position: [
        center[0] - distance * 0.25,
        center[1] - distance * 1.85,
        center[2] + Math.max(radius * 0.22, 0.2)
      ],
      up: [0, 0, 1]
    }
  }
  return {
    lookAt: center,
    position: [
      center[0] - distance * 0.55,
      center[1] - distance * 1.35,
      center[2] + distance * 0.9
    ],
    up: [0, 0, 1]
  }
}

function addSceneOptions(GaussianSplats3D) {
  return {
    format: sceneFormat(GaussianSplats3D),
    progressiveLoad: shouldProgressivelyLoad(),
    showLoadingUI: false,
    splatAlphaRemovalThreshold: sourceFormat.value === 'splat' ? 25 : 1,
    onProgress: (percentComplete) => {
      const percent = Number(percentComplete)
      loadProgress.value = Number.isFinite(percent)
        ? Math.max(0, Math.min(100, percent))
        : null
    }
  }
}

function viewerOptions(GaussianSplats3D, rootElement) {
  const frame = cameraFrame()
  const profile = qualityProfile()
  return {
    rootElement,
    cameraUp: [0, 0, 1],
    initialCameraPosition: frame.position,
    initialCameraLookAt: frame.lookAt,
    selfDrivenMode: true,
    useBuiltInControls: true,
    ignoreDevicePixelRatio: profile.ignoreDevicePixelRatio,
    sharedMemoryForWorkers: false,
    gpuAcceleratedSort: false,
    enableSIMDInSort: false,
    integerBasedSort: false,
    splatSortDistanceMapPrecision: profile.sortPrecision,
    maxScreenSpaceSplatSize: profile.maxScreenSpaceSplatSize,
    freeIntermediateSplatData: true,
    sphericalHarmonicsDegree: sphericalHarmonicsDegree(profile),
    splatRenderMode: sourceFormat.value === 'splat'
      ? GaussianSplats3D.SplatRenderMode.TwoD
      : GaussianSplats3D.SplatRenderMode.ThreeD,
    kernel2DSize: sourceFormat.value === 'splat' ? 0.18 : 0.3,
    sceneRevealMode: GaussianSplats3D.SceneRevealMode.Instant,
    renderMode: GaussianSplats3D.RenderMode.OnChange,
    logLevel: GaussianSplats3D.LogLevel.None
  }
}

function cameraFrameForMode(mode) {
  const bounds = bounds3D()
  if (bounds) return cameraFrameFromBoundsWithMode(bounds, mode)
  if (mode === 'top') {
    return {
      lookAt: [0, 0, 0],
      position: [0, -0.02, 6],
      up: [0, 1, 0]
    }
  }
  return {
    lookAt: [0, 0, 0],
    position: [-0.8, -5.5, 1.1],
    up: [0, 0, 1]
  }
}

function setVector3(target, values) {
  if (!target || !Array.isArray(values) || values.length < 3) return
  if (typeof target.set === 'function') {
    target.set(values[0], values[1], values[2])
    return
  }
  target.x = values[0]
  target.y = values[1]
  target.z = values[2]
}

function finiteVector(values) {
  if (!Array.isArray(values) || values.length < 3) return null
  const vector = values.slice(0, 3).map((value) => Number(value))
  return vector.every(Number.isFinite) ? vector : null
}

function requestViewerRender() {
  if (!viewer) return
  if (typeof viewer.forceRenderNextFrame === 'function') {
    viewer.forceRenderNextFrame()
    return
  }
  viewer.renderNextFrame = true
}

function applyCameraFrame(mode) {
  if (!viewer?.camera) return
  const frame = cameraFrameForMode(mode)
  setVector3(viewer.camera.position, frame.position)
  setVector3(viewer.camera.up, frame.up || [0, 0, 1])
  if (viewer.controls?.target) {
    setVector3(viewer.controls.target, frame.lookAt)
  }
  viewer.camera.lookAt?.(...frame.lookAt)
  viewer.controls?.update?.()
  requestViewerRender()
}

function applyCameraViewState(state) {
  if (!viewer?.camera || !state || typeof state !== 'object') return false
  const position = finiteVector(state.position)
  const target = finiteVector(state.target)
  const up = finiteVector(state.up)
  if (!position || !target) return false
  setVector3(viewer.camera.position, position)
  setVector3(viewer.camera.up, up || [0, 0, 1])
  if (viewer.controls?.target) {
    setVector3(viewer.controls.target, target)
  }
  viewer.camera.lookAt?.(...target)
  viewer.controls?.update?.()
  requestViewerRender()
  return true
}

function emitCameraViewState() {
  if (!viewer?.camera) return
  const target = viewer.controls?.target
  emit('view-state-change', {
    position: [
      Number(viewer.camera.position?.x || 0),
      Number(viewer.camera.position?.y || 0),
      Number(viewer.camera.position?.z || 0)
    ],
    target: [
      Number(target?.x || 0),
      Number(target?.y || 0),
      Number(target?.z || 0)
    ],
    up: [
      Number(viewer.camera.up?.x || 0),
      Number(viewer.camera.up?.y || 0),
      Number(viewer.camera.up?.z || 1)
    ],
    quality_mode: qualityMode.value
  })
}

function splatScaleForQualityMode(options = {}) {
  if (sourceFormat.value === 'splat') {
    if (options.interaction === true || interactionTemporarilyReduced) return 0.24
    if (qualityMode.value === 'sharp') return 0.28
    if (qualityMode.value === 'standard') return 0.34
    return 0.42
  }
  if (options.interaction === true || interactionTemporarilyReduced) {
    return isLargeDirectPLY.value ? 0.46 : 0.58
  }
  if (qualityMode.value === 'sharp') {
    return isLargeDirectPLY.value ? 0.48 : 0.65
  }
  if (qualityMode.value === 'standard') {
    return isLargeDirectPLY.value ? 0.58 : 0.72
  }
  return isLargeDirectPLY.value ? 0.72 : 0.9
}

function applyQualityModeToViewer(options = {}) {
  if (!viewer?.splatMesh) return
  const smooth = qualityMode.value === 'smooth'
  const interaction = options.interaction === true || interactionTemporarilyReduced
  viewer.splatMesh.setPointCloudModeEnabled?.(false)
  viewer.splatMesh.setSplatScale?.(splatScaleForQualityMode(options))
  if (typeof viewer.setActiveSphericalHarmonicsDegrees === 'function') {
    viewer.setActiveSphericalHarmonicsDegrees(
      (smooth || interaction) ? 0 : sphericalHarmonicsDegree(qualityProfile())
    )
  }
  requestViewerRender()
}

function setQualityMode(mode) {
  if (!['smooth', 'standard', 'sharp'].includes(mode) || qualityMode.value === mode) return
  qualityMode.value = mode
  applyQualityModeToViewer()
  emitCameraViewState()
}

function clearInteractionRestoreTimer() {
  if (!interactionRestoreTimer) return
  window.clearTimeout(interactionRestoreTimer)
  interactionRestoreTimer = null
}

function handleInteractionStart() {
  clearInteractionRestoreTimer()
  if (interactionTemporarilyReduced) return
  interactionTemporarilyReduced = true
  applyQualityModeToViewer({ interaction: true })
}

function handleInteractionEnd() {
  clearInteractionRestoreTimer()
  emitCameraViewState()
  interactionRestoreTimer = window.setTimeout(() => {
    interactionRestoreTimer = null
    interactionTemporarilyReduced = false
    applyQualityModeToViewer()
  }, 180)
}

function bindInteractionQualityControls() {
  detachInteractionQualityControls()
  const controls = viewer?.controls
  if (
    !controls ||
    typeof controls.addEventListener !== 'function' ||
    typeof controls.removeEventListener !== 'function'
  ) {
    return
  }
  controls.addEventListener('start', handleInteractionStart)
  controls.addEventListener('end', handleInteractionEnd)
  interactionControls = controls
}

function detachInteractionQualityControls() {
  clearInteractionRestoreTimer()
  interactionTemporarilyReduced = false
  if (!interactionControls) return
  interactionControls.removeEventListener?.('start', handleInteractionStart)
  interactionControls.removeEventListener?.('end', handleInteractionEnd)
  interactionControls = null
}

function withTimeout(promise, timeoutMs) {
  const nativePromise = promise?.promise && typeof promise.promise.then === 'function'
    ? promise.promise
    : promise
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => {
      promise?.abort?.('Load timeout')
      reject(new Error(t('manager.spatialPreview.loadGaussianSplatTimeout')))
    }, timeoutMs)
    nativePromise.then(
      (value) => {
        window.clearTimeout(timer)
        resolve(value)
      },
      (error) => {
        window.clearTimeout(timer)
        reject(error)
      }
    )
  })
}

function loadedSplatCount(targetViewer) {
  try {
    const count = Number(targetViewer?.splatMesh?.getSplatCount?.())
    return Number.isFinite(count) ? count : null
  } catch {
    return null
  }
}

function loadDiagnostic(error, phase) {
  const message = String(error?.message || error || '').trim()
  const lowered = message.toLowerCase()
  let reason = 'unknown'
  let userMessage = message || t('manager.spatialPreview.loadGaussianSplatFailed')

  if (message === t('manager.spatialPreview.loadGaussianSplatTimeout')) {
    reason = 'timeout'
    userMessage = t('manager.spatialPreview.loadGaussianSplatTimeout')
  } else if (lowered.includes('scene disposed') || lowered.includes('abort')) {
    reason = 'interrupted'
    userMessage = t('manager.spatialPreview.loadGaussianSplatInterrupted')
  } else if (
    lowered.includes('failed to fetch') ||
    lowered.includes('network') ||
    lowered.includes('404') ||
    lowered.includes('403') ||
    lowered.includes('500')
  ) {
    reason = 'network'
    userMessage = t('manager.spatialPreview.loadGaussianSplatNetworkFailed')
  } else if (
    lowered.includes('file type') ||
    lowered.includes('parse') ||
    lowered.includes('header') ||
    lowered.includes('invalid') ||
    lowered.includes('unexpected') ||
    lowered.includes('cannot convert undefined') ||
    lowered.includes('cannot read properties') ||
    lowered.includes('unsupported')
  ) {
    reason = 'format'
    userMessage = t('manager.spatialPreview.loadGaussianSplatFormatFailed')
  } else if (
    lowered.includes('webgl') ||
    lowered.includes('gpu') ||
    lowered.includes('texture') ||
    lowered.includes('shader') ||
    lowered.includes('memory') ||
    lowered.includes('allocation')
  ) {
    reason = 'render_resource'
    userMessage = t('manager.spatialPreview.loadGaussianSplatResourceFailed')
  } else if (lowered.includes('empty') || lowered.includes('no splat')) {
    reason = 'empty'
    userMessage = t('manager.spatialPreview.loadGaussianSplatEmpty')
  }

  return {
    reason,
    phase,
    message,
    format: sourceFormat.value,
    url: splatURL.value,
    sourceSizeBytes: sourceSizeBytes.value,
    splatCount: splatCount.value,
    progress: loadProgress.value,
    userMessage
  }
}

function reportLoadError(error, phase) {
  const diagnostic = loadDiagnostic(error, phase)
  lastDiagnostic.value = diagnostic
  errorMessage.value = diagnostic.userMessage
  const log = diagnostic.reason === 'interrupted' ? console.debug : console.error
  log('Gaussian splat preview failed', diagnostic, error)
}

async function disposeViewer() {
  const current = viewer
  viewer = null
  previewReady.value = false
  detachInteractionQualityControls()
  if (!current) return
  try {
    current.stop?.()
    await current.dispose?.()
  } catch (error) {
    console.warn('Failed to dispose gaussian splat preview resources', error)
  }
}

async function loadSplat(url) {
  const token = ++loadToken
  await disposeViewer()
  errorMessage.value = ''
  lastDiagnostic.value = null
  loadProgress.value = null
  if (!url) {
    loading.value = false
    errorMessage.value = t('manager.spatialPreview.missingGaussianSplatURL')
    return
  }
  if (!isSupportedSourceFormat()) {
    loading.value = false
    errorMessage.value = t('manager.spatialPreview.unsupportedGaussianSplatFormat')
    return
  }
  await nextTick()
  const rootElement = viewportRef.value
  if (!rootElement || token !== loadToken) return

  loading.value = true
  previewReady.value = false
  rootElement.innerHTML = ''

  try {
    const GaussianSplats3D = await loadGaussianSplats3D()
    if (token !== loadToken) return
    const nextViewer = new GaussianSplats3D.Viewer(viewerOptions(GaussianSplats3D, rootElement))
    viewer = nextViewer
    await withTimeout(nextViewer.addSplatScene(url, addSceneOptions(GaussianSplats3D)), isLargeDirectPLY.value ? 300000 : 120000)
    if (token !== loadToken) {
      await nextViewer.dispose?.()
      return
    }
    const renderedSplatCount = loadedSplatCount(nextViewer)
    if (renderedSplatCount !== null && renderedSplatCount <= 0) {
      throw new Error('no splats loaded')
    }
    nextViewer.start()
    bindInteractionQualityControls()
    previewReady.value = true
    applyQualityModeToViewer()
    if (!applyCameraViewState(props.viewState)) {
      applyCameraFrame('level')
    }
  } catch (error) {
    if (token === loadToken) {
      reportLoadError(error, 'load')
      await disposeViewer()
    }
  } finally {
    if (token === loadToken) {
      loading.value = false
      loadProgress.value = null
    }
  }
}

watch(sourceURL, (url) => {
  qualityMode.value = initialQualityMode()
  loadSplat(url)
}, { immediate: true })

onBeforeUnmount(async () => {
  loadToken += 1
  await disposeViewer()
})
</script>

<style scoped>
.three-preview {
  position: relative;
  min-height: 460px;
  height: min(68vh, 760px);
  overflow: hidden;
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--addp-bg-secondary) 78%, transparent), var(--addp-bg-primary));
  border: 1px solid var(--addp-border-color-light);
}

.three-viewport {
  position: relative;
  width: 100%;
  height: 100%;
}

.three-viewport :deep(canvas) {
  display: block;
  width: 100%;
  height: 100%;
}

.three-toolbar {
  position: absolute;
  top: 12px;
  right: 12px;
  z-index: 2;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px;
  max-width: calc(100% - 24px);
  background: color-mix(in srgb, var(--addp-bg-primary) 88%, transparent);
  border: 1px solid var(--addp-border-color-light);
}

.three-toolbar :deep(.el-button + .el-button) {
  margin-left: 0;
}

.quality-mode {
  flex: 0 1 auto;
  min-width: 0;
}

.three-status {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
  color: var(--addp-text-secondary);
  background: color-mix(in srgb, var(--addp-bg-primary) 72%, transparent);
  text-align: center;
  white-space: pre-line;
}

.three-status.is-error {
  color: var(--addp-danger-color);
}

.three-summary {
  position: absolute;
  left: 12px;
  bottom: 12px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 8px 10px;
  max-width: calc(100% - 24px);
  background: color-mix(in srgb, var(--addp-bg-primary) 88%, transparent);
  border: 1px solid var(--addp-border-color-light);
  color: var(--addp-text-secondary);
  font-size: 12px;
}
</style>
