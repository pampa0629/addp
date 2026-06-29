<template>
  <div class="three-preview tiles-preview">
    <div ref="viewportRef" class="three-viewport" />
    <div v-if="loading" class="three-status">{{ loadingText }}</div>
    <div v-else-if="errorMessage" class="three-status is-error">{{ errorMessage }}</div>
    <div v-if="summaryItems.length" class="three-summary">
      <span v-for="item in summaryItems" :key="item.label">{{ item.label }}: {{ item.value }}</span>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import { TilesRenderer } from '3d-tiles-renderer/three'
import { ReorientationPlugin } from '3d-tiles-renderer/three/plugins'
import { patchGLBMissingMaterialExtensions } from '@/utils/gltfCompatibility'
import { buildTilesetSource, resolveTileResourceURL, withAuthToken } from '@/utils/threeDTilesPreviewUrl'

const props = defineProps({
  data: {
    type: Object,
    required: true
  },
  loading: {
    type: Boolean,
    default: false
  }
})

const viewportRef = ref(null)
const loading = ref(false)
const loadingText = ref('正在加载 3D Tiles...')
const errorMessage = ref('')

let renderer = null
let scene = null
let camera = null
let controls = null
let animationFrame = 0
let resizeObserver = null
let tiles = null
let loadingTimer = 0
let loadSerial = 0
let cameraFitted = false
let tilesBoundingRadius = 1

const MIN_CAMERA_DISTANCE = 0.01
const MIN_NEAR_PLANE = 0.01

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const metadata = computed(() => content.value?.metadata || {})
const modelInfo = computed(() => metadata.value?.model_3d || {})
const formatInfo = computed(() => metadata.value?.format_info?.['3dtiles'] || metadata.value?.format_info?.['3DTiles'] || {})

const tilesetURL = computed(() => content.value?.url || objectData.value?.url || '')

const summaryItems = computed(() => {
  const model = modelInfo.value || {}
  const format = formatInfo.value || {}
  const items = []
  if (model.model_kind) items.push({ label: 'Kind', value: model.model_kind })
  if (model.lod_count) items.push({ label: 'LOD', value: Number(model.lod_count).toLocaleString() })
  if (format.tile_count) items.push({ label: 'Tiles', value: Number(format.tile_count).toLocaleString() })
  if (format.content_count) items.push({ label: 'Content', value: Number(format.content_count).toLocaleString() })
  return items
})

function ensureScene() {
  const el = viewportRef.value
  if (!el || renderer) return
  scene = new THREE.Scene()
  scene.background = null

  camera = new THREE.PerspectiveCamera(45, 1, 0.1, 100000000)
  camera.position.set(120, 90, 160)

  renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true, logarithmicDepthBuffer: true })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.outputColorSpace = THREE.SRGBColorSpace
  renderer.setClearColor(0x000000, 0)
  el.appendChild(renderer.domElement)

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.08
  controls.minDistance = MIN_CAMERA_DISTANCE
  controls.maxDistance = Infinity
  if ('zoomToCursor' in controls) {
    controls.zoomToCursor = true
  }

  scene.add(new THREE.HemisphereLight(0xffffff, 0x56616f, 1.4))
  const keyLight = new THREE.DirectionalLight(0xffffff, 1.5)
  keyLight.position.set(160, 220, 180)
  scene.add(keyLight)

  resizeObserver = new ResizeObserver(resize)
  resizeObserver.observe(el)
  resize()
  animate()
}

function animate() {
  if (!renderer || !scene || !camera) return
  controls?.update()
  updateCameraClipPlanes()
  camera.updateMatrixWorld()
  if (tiles) {
    tiles.setResolutionFromRenderer(camera, renderer)
    tiles.update()
  }
  renderer.render(scene, camera)
  animationFrame = window.requestAnimationFrame(animate)
}

function resize() {
  const el = viewportRef.value
  if (!el || !renderer || !camera) return
  const width = Math.max(1, el.clientWidth)
  const height = Math.max(1, el.clientHeight)
  renderer.setSize(width, height, false)
  camera.aspect = width / height
  camera.updateProjectionMatrix()
  if (tiles) tiles.setResolutionFromRenderer(camera, renderer)
}

function clearTiles() {
  if (!tiles) return
  if (scene && tiles.group) {
    scene.remove(tiles.group)
  }
  tiles.dispose()
  tiles = null
}

function fitCameraToTilesOnce() {
  if (cameraFitted) return
  if (fitCameraToTiles()) {
    cameraFitted = true
  }
}

function fitCameraToTiles() {
  if (!tiles || !camera || !controls) return
  const sphere = new THREE.Sphere()
  if (!tiles.getBoundingSphere(sphere) || !Number.isFinite(sphere.radius) || sphere.radius <= 0) return
  tiles.group.updateMatrixWorld(true)
  sphere.applyMatrix4(tiles.group.matrixWorld)
  const radius = Math.max(sphere.radius, 1)
  const center = sphere.center
  camera.near = Math.max(radius / 10000, 0.1)
  camera.far = Math.max(radius * 1000, 1000)
  tilesBoundingRadius = radius
  camera.up.set(0, 1, 0)
  camera.position.copy(center).add(new THREE.Vector3(radius * 1.35, radius * 1.05, radius * 1.45))
  camera.updateProjectionMatrix()
  controls.target.copy(center)
  controls.minDistance = MIN_CAMERA_DISTANCE
  controls.maxDistance = Infinity
  controls.update()
  updateCameraClipPlanes(true)
  return true
}

function updateCameraClipPlanes(force = false) {
  if (!camera || !controls) return
  const distance = Math.max(camera.position.distanceTo(controls.target), MIN_CAMERA_DISTANCE)
  const radius = Math.max(tilesBoundingRadius, 1)
  const near = Math.max(Math.min(distance / 1000, radius / 1000000), MIN_NEAR_PLANE)
  const far = Math.max(distance + radius * 8, 1000)
  if (
    force ||
    Math.abs(camera.near - near) / Math.max(camera.near, 1) > 0.1 ||
    Math.abs(camera.far - far) / Math.max(camera.far, 1) > 0.1
  ) {
    camera.near = near
    camera.far = far
    camera.updateProjectionMatrix()
  }
}

async function loadTileset(url) {
  const currentLoad = ++loadSerial
  loading.value = true
  loadingText.value = '正在加载 3D Tiles...'
  errorMessage.value = ''
  window.clearTimeout(loadingTimer)
  await nextTick()
  ensureScene()
  clearTiles()
  cameraFitted = false
  tilesBoundingRadius = 1
  if (!url) {
    errorMessage.value = '缺少 3D Tiles 预览地址'
    loading.value = false
    return
  }

  const source = buildTilesetSource(url)
  tiles = new TilesRenderer(source.rootURL)
  installCompatibleGLTFLoader(tiles)
  let rootLoaded = false
  let modelLoaded = false
  const finishLoading = () => {
    if (currentLoad !== loadSerial) return
    window.clearTimeout(loadingTimer)
    loading.value = false
  }
  tiles.fetchOptions = { credentials: 'same-origin' }
  tiles.registerPlugin(new ReorientationPlugin({ recenter: true }))
  tiles.registerPlugin({
    name: 'addp-storage-stream-url',
    fetchData: (resourceURL, options = {}) => fetch(resolveTileResourceURL(resourceURL, source), withAuthOptions(options))
  })
  tiles.setCamera(camera)
  tiles.setResolutionFromRenderer(camera, renderer)
  tiles.addEventListener('load-root-tileset', () => {
    if (currentLoad !== loadSerial) return
    rootLoaded = true
    loadingText.value = '正在加载瓦片...'
    fitCameraToTilesOnce()
  })
  tiles.addEventListener('load-model', () => {
    if (currentLoad !== loadSerial) return
    modelLoaded = true
    fitCameraToTilesOnce()
    finishLoading()
  })
  tiles.addEventListener('tiles-load-end', () => {
    if (currentLoad !== loadSerial) return
    loading.value = false
  })
  tiles.addEventListener('load-error', (event) => {
    if (currentLoad !== loadSerial) return
    window.clearTimeout(loadingTimer)
    errorMessage.value = event?.error?.message || '3D Tiles 加载失败'
    loading.value = false
  })
  loadingTimer = window.setTimeout(() => {
    if (currentLoad !== loadSerial || !loading.value) return
    if (!rootLoaded) {
      errorMessage.value = '3D Tiles 入口加载超时'
      loading.value = false
      return
    }
    if (!modelLoaded) {
      loading.value = false
    }
  }, 15000)
  scene.add(tiles.group)
}

function installCompatibleGLTFLoader(targetTiles) {
  const manager = targetTiles?.manager
  if (!manager) return
  const loader = new GLTFLoader(manager)
  const parse = loader.parse.bind(loader)
  loader.parse = (data, path, onLoad, onError) => {
    parse(patchGLBMissingMaterialExtensions(data), path, onLoad, onError)
  }
  manager.addHandler(/\.(gltf|glb)$/i, loader)
}

function withAuthOptions(options) {
  const next = { ...options }
  if (next.headers instanceof Headers) {
    next.headers = Object.fromEntries(next.headers.entries())
  }
  return next
}

function disposeScene() {
  window.cancelAnimationFrame(animationFrame)
  window.clearTimeout(loadingTimer)
  resizeObserver?.disconnect()
  resizeObserver = null
  clearTiles()
  controls?.dispose()
  renderer?.dispose()
  renderer?.domElement?.remove()
  renderer = null
  scene = null
  camera = null
  controls = null
}

watch(tilesetURL, (url) => loadTileset(url), { immediate: true })
onBeforeUnmount(disposeScene)
</script>

<style scoped>
.three-preview {
  position: relative;
  min-height: 460px;
  height: min(68vh, 760px);
  overflow: hidden;
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--addp-bg-secondary) 82%, transparent), var(--addp-bg-primary));
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

.three-status.is-error {
  color: var(--el-color-danger);
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
