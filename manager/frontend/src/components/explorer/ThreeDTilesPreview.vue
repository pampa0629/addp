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
import { TilesRenderer } from '3d-tiles-renderer/three'

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
const errorMessage = ref('')
const loadingText = '正在加载 3D Tiles...'

let renderer = null
let scene = null
let camera = null
let controls = null
let animationFrame = 0
let resizeObserver = null
let tiles = null

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

  renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.outputColorSpace = THREE.SRGBColorSpace
  renderer.setClearColor(0x000000, 0)
  el.appendChild(renderer.domElement)

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.08

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
  tiles.dispose()
  tiles = null
}

function fitCameraToTiles() {
  if (!tiles || !camera || !controls) return
  const sphere = new THREE.Sphere()
  if (!tiles.getBoundingSphere(sphere) || !Number.isFinite(sphere.radius) || sphere.radius <= 0) return
  const radius = Math.max(sphere.radius, 1)
  const center = sphere.center
  camera.near = Math.max(radius / 10000, 0.1)
  camera.far = Math.max(radius * 1000, 1000)
  camera.position.copy(center).add(new THREE.Vector3(radius * 1.45, radius * 1.1, radius * 1.65))
  camera.updateProjectionMatrix()
  controls.target.copy(center)
  controls.update()
}

async function loadTileset(url) {
  loading.value = true
  errorMessage.value = ''
  await nextTick()
  ensureScene()
  clearTiles()
  if (!url) {
    errorMessage.value = '缺少 3D Tiles 预览地址'
    loading.value = false
    return
  }

  const source = buildTilesetSource(url)
  tiles = new TilesRenderer(source.rootURL)
  tiles.fetchOptions = { credentials: 'same-origin' }
  tiles.registerPlugin({
    name: 'addp-storage-stream-url',
    fetchData: (resourceURL, options = {}) => fetch(resolveTileResourceURL(resourceURL, source), withAuthOptions(options))
  })
  tiles.setCamera(camera)
  tiles.setResolutionFromRenderer(camera, renderer)
  tiles.addEventListener('load-tileset', () => {
    fitCameraToTiles()
    loading.value = false
  })
  tiles.addEventListener('load-error', (event) => {
    errorMessage.value = event?.error?.message || '3D Tiles 加载失败'
    loading.value = false
  })
  scene.add(tiles.group)
}

function buildTilesetSource(url) {
  const parsed = parseStorageStreamURL(url)
  if (!parsed) {
    return { rootURL: withAuthToken(url), engineID: '', storageRef: '', virtual: false }
  }
  return {
    rootURL: virtualTileURL(parsed.storageRef),
    engineID: parsed.engineID,
    storageRef: parsed.storageRef,
    virtual: true
  }
}

function parseStorageStreamURL(url) {
  if (!url || typeof url !== 'string') return null
  let parsed
  try {
    parsed = new URL(url, window.location.origin)
  } catch {
    return null
  }
  if (!parsed.pathname.endsWith('/api/v1/manager/storage-stream')) return null
  const engineID = parsed.searchParams.get('engine_id') || ''
  const storageRef = parsed.searchParams.get('storage_ref') || ''
  if (!engineID || !storageRef) return null
  return { engineID, storageRef }
}

function virtualTileURL(storageRef) {
  const encoded = String(storageRef || '')
    .split('/')
    .filter(Boolean)
    .map((part) => encodeURIComponent(part))
    .join('/')
  return `${window.location.origin}/__addp_3dtiles__/${encoded}`
}

function resolveTileResourceURL(resourceURL, source) {
  if (!source?.virtual) return withAuthToken(resourceURL)
  let parsed
  try {
    parsed = new URL(resourceURL, window.location.origin)
  } catch {
    return resourceURL
  }
  const prefix = '/__addp_3dtiles__/'
  if (!parsed.pathname.startsWith(prefix)) return withAuthToken(resourceURL)
  const encodedPath = parsed.pathname.slice(prefix.length)
  const storageRef = encodedPath
    .split('/')
    .filter(Boolean)
    .map((part) => decodeURIComponent(part))
    .join('/')
  const params = new URLSearchParams()
  params.set('engine_id', source.engineID)
  params.set('storage_ref', storageRef || source.storageRef)
  appendAuthToken(params)
  return `/api/v1/manager/storage-stream?${params.toString()}`
}

function withAuthOptions(options) {
  const next = { ...options }
  if (next.headers instanceof Headers) {
    next.headers = Object.fromEntries(next.headers.entries())
  }
  return next
}

function withAuthToken(url) {
  if (!url || typeof url !== 'string') return ''
  if (!url.startsWith('/api/') && !url.startsWith('/manager/')) return url
  const parsed = new URL(url, window.location.origin)
  appendAuthToken(parsed.searchParams)
  return `${parsed.pathname}?${parsed.searchParams.toString()}`
}

function appendAuthToken(params) {
  const token = localStorage.getItem('token')
  if (token && !params.has('token')) {
    params.set('token', token)
  }
}

function disposeScene() {
  window.cancelAnimationFrame(animationFrame)
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
