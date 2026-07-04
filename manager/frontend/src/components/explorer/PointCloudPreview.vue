<template>
  <div class="three-preview point-cloud-preview">
    <div ref="viewportRef" class="three-viewport" />
    <div v-if="showStatusOverlay" class="three-status">
      <div class="point-cloud-status">
        <div>{{ emptyText }}</div>
      </div>
    </div>
    <div v-if="summaryItems.length" class="three-summary">
      <span v-for="item in summaryItems" :key="item.label">{{ item.label }}: {{ item.value }}</span>
    </div>
  </div>
</template>

<script setup>
import { computed, markRaw, nextTick, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import GiroInstance from '@giro3d/giro3d/core/Instance.js'
import COPCSource from '@giro3d/giro3d/sources/COPCSource.js'
import GiroPointCloud from '@giro3d/giro3d/entities/PointCloud.js'
import { setLazPerfPath } from '@giro3d/giro3d/sources/las/config.js'

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

const wasmBaseURL = new URL(`${import.meta.env.BASE_URL}wasm`, window.location.origin).toString().replace(/\/$/, '')
setLazPerfPath(wasmBaseURL)

const viewportRef = ref(null)
const giroInstance = shallowRef(null)
const giroPointCloud = shallowRef(null)
const giroControls = shallowRef(null)
const giroLoading = ref(false)
const giroError = ref('')
const giroDisplayedPointCount = ref(0)
const giroTotalPointCount = ref(0)
const giroProgress = ref(0)
const giroDecimation = ref(1)
const giroRequestCount = ref(0)
const renderFPS = ref(0)

let sampleRenderer = null
let sampleScene = null
let sampleCamera = null
let sampleControls = null
let samplePointsObject = null
let sampleAnimationFrame = 0
let resizeObserver = null
let giroStatsTimer = 0
let giroRenderTimer = 0
let giroFrameCount = 0
let giroLoadToken = 0
let sampleFrameCount = 0
let sampleLastSampleAt = 0

const GIRO_POINT_BUDGET = 800000
const GIRO_SUBDIVISION_THRESHOLD = 1.8
const GIRO_POINT_SIZE = 2
const GIRO_CLEANUP_DELAY_MS = 30000
const MIN_CAMERA_DISTANCE = 0.01
const GIRO_UP = Object.freeze([0, 0, 1])

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const payload = computed(() => content.value?.json || content.value?.JSON || {})
const metadata = computed(() => content.value?.metadata || {})
const previewURL = computed(() => String(content.value?.url || objectData.value?.url || '').trim())
const contentFormat = computed(() => String(payload.value?.format || metadata.value?.format || metadata.value?.point_cloud?.format || '').toUpperCase())
const shouldLoadCOPC = computed(() => contentFormat.value === 'COPC' && Boolean(previewURL.value))
const isURLMaterial = computed(() => String(content.value?.preview_material || content.value?.previewMaterial || '').toLowerCase() === 'url' || Boolean(previewURL.value))

const points = computed(() => {
  const items = Array.isArray(payload.value?.points) ? payload.value.points : []
  return items
    .map((point) => ({
      x: Number(point?.x),
      y: Number(point?.y),
      z: Number(point?.z),
      intensity: Number(point?.intensity),
      r: Number(point?.r),
      g: Number(point?.g),
      b: Number(point?.b)
    }))
    .filter((point) => Number.isFinite(point.x) && Number.isFinite(point.y) && Number.isFinite(point.z))
})

const pointCount = computed(() => Number(payload.value?.point_count || metadata.value?.point_count || giroTotalPointCount.value || points.value.length || 0))
const sampleCount = computed(() => Number(payload.value?.sample_count || metadata.value?.sample_count || giroDisplayedPointCount.value || points.value.length || 0))
const hasVisibleContent = computed(() => shouldLoadCOPC.value ? giroDisplayedPointCount.value > 0 : points.value.length > 0)
const showStatusOverlay = computed(() => props.loading || giroLoading.value || Boolean(giroError.value) || !hasVisibleContent.value)

const emptyText = computed(() => {
  if (props.loading) return '正在加载点云...'
  if (giroLoading.value) return `正在加载 COPC 点云... ${giroProgress.value}%`
  if (giroError.value) return giroError.value
  if (shouldLoadCOPC.value || isURLMaterial.value) return 'COPC 快显文件已就绪'
  return '没有可展示的点云样本'
})

const summaryItems = computed(() => {
  const items = []
  if (pointCount.value) items.push({ label: 'Points', value: pointCount.value.toLocaleString() })
  if (sampleCount.value) items.push({ label: shouldLoadCOPC.value ? 'Displayed' : 'Sample', value: sampleCount.value.toLocaleString() })
  if (shouldLoadCOPC.value) {
    items.push({ label: 'Progress', value: `${giroProgress.value}%` })
    items.push({ label: 'Requests', value: giroRequestCount.value.toLocaleString() })
    items.push({ label: 'Decimation', value: String(giroDecimation.value) })
  }
  if (renderFPS.value) items.push({ label: 'FPS', value: renderFPS.value.toString() })
  if (contentFormat.value) items.push({ label: 'Format', value: contentFormat.value })
  return items
})

function authHeaders() {
  const token = localStorage.getItem('token') || ''
  return token ? { Authorization: token.toLowerCase().startsWith('bearer ') ? token : `Bearer ${token}` } : {}
}

function createRangeGetter(url, token) {
  return async (begin, end) => {
    if (token !== giroLoadToken) {
      throw new Error('aborted')
    }
    const response = await fetch(url, {
      headers: {
        ...authHeaders(),
        Range: `bytes=${begin}-${end - 1}`
      }
    })
    if (!response.ok) {
      throw new Error(`COPC range request failed: HTTP ${response.status}`)
    }
    const buffer = await response.arrayBuffer()
    giroRequestCount.value += 1
    return new Uint8Array(buffer)
  }
}

function safeRead(read, fallback) {
  try {
    return read()
  } catch (error) {
    if (error instanceof Error && (error.message.includes('entity is ready') || error.message.includes('not yet ready') || error.message === 'not initialized')) {
      return fallback
    }
    throw error
  }
}

async function waitEntityReady(entity, timeoutMs = 15000) {
  if (entity.ready) return
  await new Promise((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      entity.removeEventListener('initialized', onReady)
      reject(new Error('Giro3D entity 初始化超时'))
    }, timeoutMs)
    function onReady() {
      window.clearTimeout(timeout)
      entity.removeEventListener('initialized', onReady)
      resolve()
    }
    entity.addEventListener('initialized', onReady)
  })
}

function ensureGiroInstance() {
  if (giroInstance.value) return giroInstance.value
  const target = viewportRef.value
  if (!target) return null

  const camera = new THREE.PerspectiveCamera(60, 1, 0.1, 2000000000)
  camera.up.set(GIRO_UP[0], GIRO_UP[1], GIRO_UP[2])
  const instance = markRaw(new GiroInstance({
    target,
    crs: 'EPSG:3857',
    camera,
    backgroundColor: null,
    renderer: {
      antialias: false,
      alpha: true,
      powerPreference: 'high-performance'
    }
  }))
  instance.renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 1.5))
  instance.renderer.setClearColor(0x000000, 0)
  instance.mainLoop.automaticCameraPlaneComputation = false
  instance.renderingOptions.enableMSAA = false

  instance.scene.add(new THREE.AmbientLight(0xffffff, 0.55))
  const sun = new THREE.DirectionalLight(0xffffff, 0.9)
  sun.position.set(1, -1, 2)
  instance.scene.add(sun)

  const controls = markRaw(new OrbitControls(instance.view.camera, instance.domElement))
  controls.enableDamping = true
  controls.dampingFactor = 0.08
  controls.screenSpacePanning = true
  controls.minDistance = MIN_CAMERA_DISTANCE
  controls.maxDistance = Infinity
  if ('zoomToCursor' in controls) {
    controls.zoomToCursor = true
  }
  controls.addEventListener('change', () => instance.notifyChange(instance.view.camera))
  controls.addEventListener('end', emitGiroCameraViewState)
  instance.view.setControls(controls)
  instance.addEventListener('after-render', () => {
    giroFrameCount += 1
  })

  giroInstance.value = instance
  giroControls.value = controls
  startGiroStatsLoop()
  return instance
}

async function loadGiroCOPC(url) {
  const token = ++giroLoadToken
  disposeSampleScene()
  disposeGiroPointCloud()
  giroLoading.value = true
  giroError.value = ''
  giroDisplayedPointCount.value = 0
  giroTotalPointCount.value = 0
  giroProgress.value = 0
  giroRequestCount.value = 0

  try {
    await nextTick()
    const instance = ensureGiroInstance()
    if (!instance) return
    const source = new COPCSource({
      url: createRangeGetter(new URL(url, window.location.href).toString(), token),
      enableWorkers: false,
      compressColorsTo8Bit: true
    })
    const cloud = markRaw(new GiroPointCloud({
      source,
      cleanupDelay: GIRO_CLEANUP_DELAY_MS
    }))

    await instance.add(cloud)
    await waitEntityReady(cloud)
    if (token !== giroLoadToken) {
      instance.remove(cloud)
      return
    }
    giroPointCloud.value = cloud

    cloud.pointBudget = GIRO_POINT_BUDGET
    cloud.subdivisionThreshold = GIRO_SUBDIVISION_THRESHOLD
    cloud.pointSize = GIRO_POINT_SIZE
    cloud.depthTest = true

    if (!applyGiroCameraViewState(props.viewState)) {
      fitGiroCamera()
    }
    instance.notifyChange(cloud, { needsRedraw: true, immediate: true })
  } catch (error) {
    if (token === giroLoadToken && error?.message !== 'aborted') {
      giroError.value = error?.message || 'COPC 点云加载失败'
      console.error(error)
    }
  } finally {
    if (token === giroLoadToken) {
      giroLoading.value = false
    }
  }
}

function fitGiroCamera() {
  const instance = giroInstance.value
  const cloud = giroPointCloud.value
  const controls = giroControls.value
  if (!instance || !cloud || !controls) return
  const bounds = safeRead(() => cloud.getBoundingBox(), null)
  if (!bounds || bounds.isEmpty()) return

  const sphere = bounds.getBoundingSphere(new THREE.Sphere())
  const radius = Math.max(sphere.radius, 1)
  const camera = instance.view.camera
  const offset = new THREE.Vector3(radius * 0.9, -radius * 1.3, radius * 0.9)

  camera.up.set(GIRO_UP[0], GIRO_UP[1], GIRO_UP[2])
  camera.near = Math.max(radius / 10000, 0.1)
  camera.far = Math.max(radius * 100, 1000)
  camera.position.copy(sphere.center).add(offset)
  camera.lookAt(sphere.center)
  camera.updateProjectionMatrix()

  controls.target.copy(sphere.center)
  controls.minDistance = MIN_CAMERA_DISTANCE
  controls.maxDistance = Infinity
  controls.update()

  instance.view.near = camera.near
  instance.view.far = camera.far
  instance.notifyChange(camera, { needsRedraw: true, immediate: true })
}

function finiteVector(values) {
  if (!Array.isArray(values) || values.length < 3) return null
  const vector = values.slice(0, 3).map((value) => Number(value))
  return vector.every(Number.isFinite) ? vector : null
}

function applyGiroCameraViewState(state) {
  const instance = giroInstance.value
  const controls = giroControls.value
  if (!instance || !controls || !state || typeof state !== 'object') return false
  const position = finiteVector(state.position)
  const target = finiteVector(state.target)
  if (!position || !target) return false
  const camera = instance.view.camera
  camera.position.set(position[0], position[1], position[2])
  camera.up.set(GIRO_UP[0], GIRO_UP[1], GIRO_UP[2])
  controls.target.set(target[0], target[1], target[2])
  controls.update()
  camera.updateProjectionMatrix()
  instance.view.near = camera.near
  instance.view.far = camera.far
  instance.notifyChange(camera, { needsRedraw: true, immediate: true })
  return true
}

function emitGiroCameraViewState() {
  const instance = giroInstance.value
  const controls = giroControls.value
  if (!instance || !controls) return
  const camera = instance.view.camera
  emit('view-state-change', {
    position: camera.position.toArray(),
    target: controls.target.toArray(),
    up: [...GIRO_UP]
  })
}

function startGiroStatsLoop() {
  if (!giroStatsTimer) {
    giroStatsTimer = window.setInterval(() => {
      const cloud = giroPointCloud.value
      const instance = giroInstance.value
      renderFPS.value = giroFrameCount
      giroFrameCount = 0
      giroProgress.value = Math.round(((cloud?.progress ?? instance?.progress ?? 1) || 0) * 100)
      giroDisplayedPointCount.value = cloud?.ready ? safeRead(() => cloud.displayedPointCount, 0) : 0
      giroTotalPointCount.value = cloud?.ready ? safeRead(() => cloud.pointCount, 0) : 0
      giroDecimation.value = cloud?.ready ? safeRead(() => cloud.decimation, 1) : 1
    }, 1000)
  }
  if (!giroRenderTimer) {
    const tick = () => {
      giroControls.value?.update()
      if (giroPointCloud.value && giroInstance.value) {
        giroInstance.value.notifyChange(giroInstance.value.view.camera, { needsRedraw: true })
      }
      giroRenderTimer = window.setTimeout(() => window.requestAnimationFrame(tick), giroPointCloud.value ? 120 : 500)
    }
    window.requestAnimationFrame(tick)
  }
}

function disposeGiroPointCloud() {
  if (giroInstance.value && giroPointCloud.value) {
    giroInstance.value.remove(giroPointCloud.value)
  }
  giroPointCloud.value = null
  giroDisplayedPointCount.value = 0
  giroTotalPointCount.value = 0
  giroProgress.value = 0
  giroDecimation.value = 1
}

function disposeGiro() {
  disposeGiroPointCloud()
  if (giroStatsTimer) {
    window.clearInterval(giroStatsTimer)
    giroStatsTimer = 0
  }
  if (giroRenderTimer) {
    window.clearTimeout(giroRenderTimer)
    giroRenderTimer = 0
  }
  giroControls.value?.dispose()
  giroInstance.value?.dispose()
  giroControls.value = null
  giroInstance.value = null
  giroFrameCount = 0
}

function ensureSampleScene() {
  const el = viewportRef.value
  if (!el || sampleRenderer) return
  sampleScene = new THREE.Scene()
  sampleScene.background = null
  sampleCamera = new THREE.PerspectiveCamera(45, 1, 0.01, 100000000)
  sampleCamera.position.set(4, 3, 6)
  sampleRenderer = new THREE.WebGLRenderer({ antialias: false, alpha: true, powerPreference: 'high-performance' })
  sampleRenderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 1.5))
  sampleRenderer.outputColorSpace = THREE.SRGBColorSpace
  sampleRenderer.setClearColor(0x000000, 0)
  el.appendChild(sampleRenderer.domElement)

  sampleControls = new OrbitControls(sampleCamera, sampleRenderer.domElement)
  sampleControls.enableDamping = true
  sampleControls.dampingFactor = 0.08
  sampleControls.screenSpacePanning = true
  sampleControls.minDistance = MIN_CAMERA_DISTANCE
  sampleControls.maxDistance = Infinity
  if ('zoomToCursor' in sampleControls) {
    sampleControls.zoomToCursor = true
  }
  sampleScene.add(new THREE.AmbientLight(0xffffff, 1.4))

  resizeObserver = new ResizeObserver(resizeSampleScene)
  resizeObserver.observe(el)
  resizeSampleScene()
  animateSampleScene()
}

function animateSampleScene() {
  if (!sampleRenderer || !sampleScene || !sampleCamera) return
  sampleControls?.update()
  sampleRenderer.render(sampleScene, sampleCamera)
  updateSampleFPS()
  sampleAnimationFrame = window.requestAnimationFrame(animateSampleScene)
}

function updateSampleFPS() {
  const now = performance.now()
  sampleFrameCount += 1
  if (!sampleLastSampleAt) {
    sampleLastSampleAt = now
    return
  }
  const elapsed = now - sampleLastSampleAt
  if (elapsed < 500) return
  renderFPS.value = Math.round((sampleFrameCount * 1000) / elapsed)
  sampleFrameCount = 0
  sampleLastSampleAt = now
}

function resizeSampleScene() {
  const el = viewportRef.value
  if (!el || !sampleRenderer || !sampleCamera) return
  const width = Math.max(1, el.clientWidth)
  const height = Math.max(1, el.clientHeight)
  sampleRenderer.setSize(width, height, false)
  sampleCamera.aspect = width / height
  sampleCamera.updateProjectionMatrix()
}

function rebuildSamplePointCloud(items) {
  if (shouldLoadCOPC.value) return
  disposeGiro()
  ensureSampleScene()
  disposeSamplePoints()
  if (!sampleScene || !items.length) return

  const bounds = pointBounds(items)
  const center = bounds.getCenter(new THREE.Vector3())
  const size = bounds.getSize(new THREE.Vector3())
  const maxDim = Math.max(size.x, size.y, size.z, 1)
  const positions = new Float32Array(items.length * 3)
  const colors = new Float32Array(items.length * 3)
  const minZ = Number.isFinite(bounds.min.z) ? bounds.min.z : 0
  const maxZ = Number.isFinite(bounds.max.z) ? bounds.max.z : minZ + 1
  const zRange = Math.max(1, maxZ - minZ)

  items.forEach((point, index) => {
    const offset = index * 3
    positions[offset] = point.x - center.x
    positions[offset + 1] = point.z - center.z
    positions[offset + 2] = point.y - center.y
    if (pointHasRGB(point)) {
      colors[offset] = normalizedColorComponent(point.r)
      colors[offset + 1] = normalizedColorComponent(point.g)
      colors[offset + 2] = normalizedColorComponent(point.b)
    } else {
      writeElevationColor(colors, offset, point.z, minZ, zRange)
    }
  })

  const geometry = new THREE.BufferGeometry()
  geometry.setAttribute('position', new THREE.BufferAttribute(positions, 3))
  geometry.setAttribute('color', new THREE.BufferAttribute(colors, 3))
  const material = new THREE.PointsMaterial({
    size: Math.max(maxDim / 300, 0.01),
    sizeAttenuation: false,
    vertexColors: true
  })
  samplePointsObject = new THREE.Points(geometry, material)
  sampleScene.add(samplePointsObject)
  fitSampleCamera(bounds, center, maxDim)
}

function fitSampleCamera(bounds, center, maxDim) {
  if (!sampleCamera || !sampleControls || bounds.isEmpty()) return
  const distance = maxDim / (2 * Math.tan((sampleCamera.fov * Math.PI) / 360))
  sampleCamera.near = Math.max(maxDim / 100000, 0.01)
  sampleCamera.far = Math.max(maxDim * 100, distance * 10)
  sampleCamera.position.set(distance * 0.8, distance * 0.65, distance * 1.2)
  sampleCamera.updateProjectionMatrix()
  sampleControls.target.set(0, 0, 0)
  sampleControls.minDistance = MIN_CAMERA_DISTANCE
  sampleControls.maxDistance = Infinity
  sampleControls.update()
}

function pointBounds(items) {
  const bounds = new THREE.Box3()
  items.forEach((point) => bounds.expandByPoint(new THREE.Vector3(point.x, point.y, point.z)))
  return bounds
}

function pointHasRGB(point) {
  return Number.isFinite(point.r) && Number.isFinite(point.g) && Number.isFinite(point.b)
}

function normalizedColorComponent(value) {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.min(1, value > 255 ? value / 65535 : value / 255))
}

function writeElevationColor(colors, offset, z, minZ, zRange) {
  const ratio = Math.max(0, Math.min(1, (z - minZ) / zRange))
  colors[offset] = 0.1843137254901961 + (0.9490196078431372 - 0.1843137254901961) * ratio
  colors[offset + 1] = 0.5019607843137255 + (0.788235294117647 - 0.5019607843137255) * ratio
  colors[offset + 2] = 0.9294117647058824 + (0.2980392156862745 - 0.9294117647058824) * ratio
}

function disposeSamplePoints() {
  if (!sampleScene || !samplePointsObject) return
  sampleScene.remove(samplePointsObject)
  samplePointsObject.geometry?.dispose?.()
  samplePointsObject.material?.dispose?.()
  samplePointsObject = null
}

function disposeSampleScene() {
  window.cancelAnimationFrame(sampleAnimationFrame)
  resizeObserver?.disconnect()
  resizeObserver = null
  disposeSamplePoints()
  sampleControls?.dispose()
  sampleRenderer?.dispose()
  sampleRenderer?.domElement?.remove()
  sampleRenderer = null
  sampleScene = null
  sampleCamera = null
  sampleControls = null
  sampleFrameCount = 0
  sampleLastSampleAt = 0
  if (!shouldLoadCOPC.value) renderFPS.value = 0
}

watch(points, async (items) => {
  if (shouldLoadCOPC.value) return
  await nextTick()
  rebuildSamplePointCloud(items)
}, { immediate: true })

watch([previewURL, shouldLoadCOPC], ([url, enabled]) => {
  if (!enabled) {
    giroLoadToken += 1
    giroLoading.value = false
    giroError.value = ''
    disposeGiro()
    nextTick(() => rebuildSamplePointCloud(points.value))
    return
  }
  loadGiroCOPC(url)
}, { immediate: true })

onBeforeUnmount(() => {
  giroLoadToken += 1
  disposeGiro()
  disposeSampleScene()
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
  width: 100%;
  height: 100%;
}

.three-viewport :deep(canvas) {
  display: block;
  width: 100%;
  height: 100%;
}

.three-status {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
  color: var(--addp-text-secondary);
  background: color-mix(in srgb, var(--addp-bg-primary) 70%, transparent);
}

.point-cloud-status {
  display: grid;
  gap: 10px;
  justify-items: center;
  text-align: center;
}

.three-summary {
  position: absolute;
  left: 12px;
  bottom: 12px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  max-width: calc(100% - 24px);
}

.three-summary span {
  padding: 4px 8px;
  border-radius: 4px;
  color: var(--addp-text-secondary);
  background: color-mix(in srgb, var(--addp-bg-primary) 84%, transparent);
  border: 1px solid var(--addp-border-color-light);
  font-size: 12px;
}
</style>
