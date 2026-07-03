<template>
  <div class="three-preview point-cloud-preview">
    <div ref="viewportRef" class="three-viewport" />
    <div v-if="showStatusOverlay" class="three-status">
      <div class="point-cloud-status">
        <div>{{ emptyText }}</div>
        <a
          v-if="previewURL"
          class="point-cloud-link"
          :href="previewURL"
          target="_blank"
          rel="noopener noreferrer"
        >
          {{ previewURLText }}
        </a>
      </div>
    </div>
    <div v-if="summaryItems.length" class="three-summary">
      <span v-for="item in summaryItems" :key="item.label">{{ item.label }}: {{ item.value }}</span>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import { createCOPCLazPerf } from '@/utils/pointCloudCOPCLazPerf'
import {
  DEFAULT_COPC_DETAIL_POINT_BUDGET,
  DEFAULT_COPC_HIERARCHY_PAGE_LOAD_LIMIT,
  DEFAULT_COPC_NODE_LIMIT,
  collectHierarchyPageEntries,
  collectHierarchyNodeEntries,
  enrichCOPCNodeEntries,
  hierarchyKeyAncestorOf,
  loadCOPCHierarchySubtrees,
  mergeCOPCNodeSelections,
  pointMaterialSize,
  selectCOPCCoverageNodes,
  selectCOPCDetailNodes,
  selectCOPCHierarchyPages,
  selectCOPCOverviewNodes
} from '@/utils/pointCloudCOPCPreview'

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
const copcPoints = ref([])
const copcLoading = ref(false)
const copcError = ref('')
const copcSummary = ref(null)
const copcVisiblePointCount = ref(0)
const copcLoadedNodeCount = ref(0)
const copcLoadedHierarchyPageCount = ref(0)
const copcRemainingHierarchyPageCount = ref(0)
const copcPendingHierarchyPageCount = ref(0)
const copcLoadedDepthLabel = ref('')
const copcActiveDepthLabel = ref('')

let renderer = null
let scene = null
let camera = null
let controls = null
let animationFrame = 0
let resizeObserver = null
let pointsObject = null
let copcLoadToken = 0
let copcAbortController = null
let lazPerfPromise = null
let sceneOrigin = new THREE.Vector3()
let copcRuntime = null
let copcLODTimer = 0
let copcLODVersion = 0
const copcNodePointCache = new Map()
const activeCOPCNodeObjects = new Map()

const COPC_PREVIEW_NODE_LIMIT = Math.max(DEFAULT_COPC_NODE_LIMIT, 160)
const COPC_OVERVIEW_NODE_LIMIT = 10
const COPC_COVERAGE_NODE_LIMIT = 56
const COPC_BASE_POINT_BUDGET = Math.max(DEFAULT_COPC_DETAIL_POINT_BUDGET, 2800000)
const COPC_MAX_POINT_BUDGET = 4800000
const COPC_OVERVIEW_NODE_POINT_LIMIT = 3000
const COPC_COVERAGE_NODE_POINT_LIMIT = 36000
const COPC_DETAIL_NODE_POINT_LIMIT = 180000
const COPC_NEAR_LEAF_NODE_POINT_LIMIT = 420000
const COPC_PARENT_NODE_POINT_LIMIT = 12000
const COPC_DETAIL_NODE_MIN_POINT_LIMIT = 24000
const COPC_NODE_LOAD_CONCURRENCY = 6
const COPC_NODE_CACHE_LIMIT = 260
const COPC_HIERARCHY_PAGE_LOAD_LIMIT = Math.max(DEFAULT_COPC_HIERARCHY_PAGE_LOAD_LIMIT, 32)
const COPC_HIERARCHY_PAGE_TOTAL_LIMIT = 4096
const COPC_HIERARCHY_PAGE_LOAD_CONCURRENCY = 6

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const payload = computed(() => content.value?.json || content.value?.JSON || {})
const metadata = computed(() => content.value?.metadata || {})
const previewURL = computed(() => String(content.value?.url || objectData.value?.url || '').trim())
const contentFormat = computed(() => String(payload.value?.format || metadata.value?.format || metadata.value?.point_cloud?.format || '').toUpperCase())
const isURLMaterial = computed(() => String(content.value?.preview_material || content.value?.previewMaterial || '').toLowerCase() === 'url' || Boolean(previewURL.value))
const shouldLoadCOPC = computed(() => contentFormat.value === 'COPC' && Boolean(previewURL.value))
const previewURLText = computed(() => contentFormat.value === 'COPC' ? '打开 COPC 快显文件' : '打开点云文件')
const emptyText = computed(() => {
  if (props.loading) return '正在加载点云...'
  if (copcLoading.value) return '正在加载 COPC 点云...'
  if (copcError.value) return copcError.value
  if (isURLMaterial.value) return 'COPC 快显文件已就绪'
  return '没有可展示的点云样本'
})

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

const displayPoints = computed(() => points.value.length ? points.value : copcPoints.value)
const pointCount = computed(() => Number(payload.value?.point_count || metadata.value?.point_count || copcSummary.value?.point_count || displayPoints.value.length || 0))
const sampleCount = computed(() => Number(payload.value?.sample_count || metadata.value?.sample_count || copcVisiblePointCount.value || copcSummary.value?.sample_count || displayPoints.value.length || 0))
const bounds3D = computed(() => payload.value?.bounds_3d || metadata.value?.bounds_3d || copcSummary.value?.bounds_3d || {})
const showStatusOverlay = computed(() => !(displayPoints.value.length || copcVisiblePointCount.value) || copcLoading.value || Boolean(copcError.value))

const summaryItems = computed(() => {
  const items = []
  if (pointCount.value) items.push({ label: 'Points', value: pointCount.value.toLocaleString() })
  if (sampleCount.value) items.push({ label: 'Sample', value: sampleCount.value.toLocaleString() })
  if (copcLoadedNodeCount.value) items.push({ label: 'Nodes', value: copcLoadedNodeCount.value.toLocaleString() })
  if (copcLoadedHierarchyPageCount.value) {
    const loaded = copcLoadedHierarchyPageCount.value.toLocaleString()
    const remaining = copcRemainingHierarchyPageCount.value
    const pending = copcPendingHierarchyPageCount.value
    const total = copcLoadedHierarchyPageCount.value + remaining + pending
    items.push({ label: 'Pages', value: remaining || pending ? `${loaded}/${total.toLocaleString()}` : loaded })
  }
  if (copcActiveDepthLabel.value || copcLoadedDepthLabel.value) {
    items.push({ label: 'Depth', value: copcActiveDepthLabel.value ? `${copcActiveDepthLabel.value}/${copcLoadedDepthLabel.value || '-'}` : copcLoadedDepthLabel.value })
  }
  const format = contentFormat.value
  if (format) items.push({ label: 'Format', value: format })
  return items
})

function ensureScene() {
  const el = viewportRef.value
  if (!el || renderer) return
  scene = new THREE.Scene()
  scene.background = null

  camera = new THREE.PerspectiveCamera(45, 1, 0.01, 100000000)
  camera.position.set(4, 3, 6)

  renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.outputColorSpace = THREE.SRGBColorSpace
  renderer.setClearColor(0x000000, 0)
  el.appendChild(renderer.domElement)

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.08
  controls.addEventListener('change', scheduleCOPCLodUpdate)

  scene.add(new THREE.AmbientLight(0xffffff, 1.4))

  resizeObserver = new ResizeObserver(resize)
  resizeObserver.observe(el)
  resize()
  animate()
}

function animate() {
  if (!renderer || !scene || !camera) return
  controls?.update()
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
}

function disposePoints() {
  if (!scene || !pointsObject) return
  scene.remove(pointsObject)
  pointsObject.traverse?.((object) => {
    object.geometry?.dispose?.()
    object.material?.dispose?.()
  })
  pointsObject.geometry?.dispose?.()
  pointsObject.material?.dispose?.()
  pointsObject = null
  activeCOPCNodeObjects.clear()
  copcVisiblePointCount.value = 0
  copcLoadedNodeCount.value = 0
  copcLoadedHierarchyPageCount.value = 0
  copcRemainingHierarchyPageCount.value = 0
  copcPendingHierarchyPageCount.value = 0
  copcLoadedDepthLabel.value = ''
  copcActiveDepthLabel.value = ''
}

function cancelCOPCLoad() {
  copcLoadToken += 1
  copcLODVersion += 1
  window.clearTimeout(copcLODTimer)
  copcAbortController?.abort()
  copcAbortController = null
  copcLoading.value = false
  copcRuntime = null
  copcNodePointCache.clear()
  disposePoints()
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

function fitCamera(bounds) {
  if (!camera || !controls || bounds.isEmpty()) return
  const center = bounds.getCenter(new THREE.Vector3())
  const size = bounds.getSize(new THREE.Vector3())
  const maxDim = Math.max(size.x, size.y, size.z, 1)
  const distance = maxDim / (2 * Math.tan((camera.fov * Math.PI) / 360))
  camera.near = Math.max(maxDim / 100000, 0.01)
  camera.far = Math.max(maxDim * 100, distance * 10)
  camera.position.copy(center).add(new THREE.Vector3(distance * 0.8, distance * 0.65, distance * 1.2))
  camera.updateProjectionMatrix()
  controls.target.copy(center)
  controls.minDistance = Math.max(maxDim / 10000, 0.001)
  controls.maxDistance = camera.far * 0.5
  controls.update()
}

function rebuildPointCloud(items) {
  if (shouldLoadCOPC.value) return
  ensureScene()
  disposePoints()
  if (!scene || !items.length) return

  const bounds = pointBounds(items)
  const center = bounds.getCenter(new THREE.Vector3())
  sceneOrigin = center.clone()
  const renderBounds = renderBoundsFromWorldBox(bounds)
  const size = renderBounds.getSize(new THREE.Vector3())
  const positions = new Float32Array(items.length * 3)
  const colors = new Float32Array(items.length * 3)
  const minZ = Number.isFinite(bounds.min.z) ? bounds.min.z : 0
  const maxZ = Number.isFinite(bounds.max.z) ? bounds.max.z : minZ + 1
  const zRange = Math.max(1, maxZ - minZ)

  items.forEach((point, index) => {
    const i = index * 3
    const position = renderPointFromWorld(point)
    positions[i] = position.x
    positions[i + 1] = position.y
    positions[i + 2] = position.z
    let color
    if (pointHasRGB(point)) {
      color = new THREE.Color(
        normalizedColorComponent(point.r),
        normalizedColorComponent(point.g),
        normalizedColorComponent(point.b)
      )
    } else {
      const zRatio = Math.max(0, Math.min(1, (point.z - minZ) / zRange))
      const low = new THREE.Color(0x2f80ed)
      const high = new THREE.Color(0xf2c94c)
      color = low.lerp(high, zRatio)
    }
    colors[i] = color.r
    colors[i + 1] = color.g
    colors[i + 2] = color.b
  })

  const geometry = new THREE.BufferGeometry()
  geometry.setAttribute('position', new THREE.BufferAttribute(positions, 3))
  geometry.setAttribute('color', new THREE.BufferAttribute(colors, 3))

  const material = new THREE.PointsMaterial({
    size: pointMaterialSize(size, items.length),
    sizeAttenuation: false,
    vertexColors: true,
    transparent: true,
    opacity: 0.92
  })
  pointsObject = new THREE.Points(geometry, material)
  scene.add(pointsObject)

  fitCamera(renderBounds)
}

function renderPointFromWorld(point) {
  return new THREE.Vector3(
    Number(point.x) - sceneOrigin.x,
    Number(point.z) - sceneOrigin.z,
    Number(point.y) - sceneOrigin.y
  )
}

function worldPointFromRender(point) {
  return {
    x: Number(point?.x || 0) + sceneOrigin.x,
    y: Number(point?.z || 0) + sceneOrigin.y,
    z: Number(point?.y || 0) + sceneOrigin.z
  }
}

function renderBoundsFromWorldArray(bounds) {
  return new THREE.Box3(
    new THREE.Vector3(bounds[0] - sceneOrigin.x, bounds[2] - sceneOrigin.z, bounds[1] - sceneOrigin.y),
    new THREE.Vector3(bounds[3] - sceneOrigin.x, bounds[5] - sceneOrigin.z, bounds[4] - sceneOrigin.y)
  )
}

function renderBoundsFromWorldBox(bounds) {
  return renderBoundsFromWorldArray([
    bounds.min.x,
    bounds.min.y,
    bounds.min.z,
    bounds.max.x,
    bounds.max.y,
    bounds.max.z
  ])
}

function visibleCOPCEntries(entries) {
  if (!camera || !Array.isArray(entries) || !entries.length) return entries || []
  camera.updateMatrixWorld()
  camera.updateProjectionMatrix()
  const matrix = new THREE.Matrix4().multiplyMatrices(camera.projectionMatrix, camera.matrixWorldInverse)
  const frustum = new THREE.Frustum().setFromProjectionMatrix(matrix)
  return entries.filter((entry) => {
    if (!entry?.bounds) return false
    return frustum.intersectsBox(renderBoundsFromWorldArray(entry.bounds))
  })
}

function currentCOPCView() {
  return {
    camera: worldPointFromRender(camera.position),
    target: worldPointFromRender(controls.target),
    viewportHeight: renderer?.domElement?.clientHeight || viewportRef.value?.clientHeight || 1,
    fov: camera.fov
  }
}

function currentCOPCPointBudget() {
  const width = renderer?.domElement?.clientWidth || viewportRef.value?.clientWidth || 1
  const height = renderer?.domElement?.clientHeight || viewportRef.value?.clientHeight || 1
  const pixelRatio = Math.min(window.devicePixelRatio || 1, 2)
  const viewportPixels = Math.max(width * height * pixelRatio, 1)
  return Math.max(COPC_BASE_POINT_BUDGET, Math.min(COPC_MAX_POINT_BUDGET, Math.round(viewportPixels * 1.65)))
}

function worldBoundsArrayFromCOPC(copc) {
  const min = Array.isArray(copc?.header?.min) ? copc.header.min : []
  const max = Array.isArray(copc?.header?.max) ? copc.header.max : []
  if (min.length >= 3 && max.length >= 3) {
    return [Number(min[0]), Number(min[1]), Number(min[2]), Number(max[0]), Number(max[1]), Number(max[2])]
  }
  const cube = Array.isArray(copc?.info?.cube) ? copc.info.cube : []
  return cube.length >= 6 ? cube.slice(0, 6).map((value) => Number(value)) : null
}

function authorizationHeaders() {
  const token = localStorage.getItem('token') || ''
  return token ? { Authorization: `Bearer ${token}` } : {}
}

function createRangeGetter(url, signal) {
  return async (begin, end) => {
    const headers = {
      ...authorizationHeaders(),
      Range: `bytes=${begin}-${end - 1}`
    }
    const response = await fetch(url, { headers, signal })
    if (!response.ok) {
      throw new Error(`COPC range request failed: HTTP ${response.status}`)
    }
    return new Uint8Array(await response.arrayBuffer())
  }
}

function getterOrNull(view, name) {
  try {
    if (!view?.dimensions?.[name]) return null
    return view.getter(name)
  } catch {
    return null
  }
}

function pointBuffersFromCOPCView(view, remaining, worldBounds) {
  const count = Math.min(Number(view?.pointCount || 0), Math.max(remaining, 0))
  if (!count) return { positions: new Float32Array(), colors: new Float32Array(), count: 0 }
  const step = Math.max(1, Math.ceil(Number(view.pointCount || 0) / count))
  const getX = view.getter('X')
  const getY = view.getter('Y')
  const getZ = view.getter('Z')
  const getRed = getterOrNull(view, 'Red')
  const getGreen = getterOrNull(view, 'Green')
  const getBlue = getterOrNull(view, 'Blue')
  const positions = new Float32Array(count * 3)
  const colors = new Float32Array(count * 3)
  const minZ = Number(worldBounds[2])
  const maxZ = Number(worldBounds[5])
  const zRange = Math.max(1, maxZ - minZ)
  let actualCount = 0
  for (let index = 0; index < view.pointCount && actualCount < count; index += step) {
    const x = Number(getX(index))
    const y = Number(getY(index))
    const z = Number(getZ(index))
    const r = getRed ? Number(getRed(index)) : NaN
    const g = getGreen ? Number(getGreen(index)) : NaN
    const b = getBlue ? Number(getBlue(index)) : NaN
    const i = actualCount * 3
    const position = renderPointFromWorld({ x, y, z })
    positions[i] = position.x
    positions[i + 1] = position.y
    positions[i + 2] = position.z
    let color
    if (Number.isFinite(r) && Number.isFinite(g) && Number.isFinite(b)) {
      color = new THREE.Color(
        normalizedColorComponent(r),
        normalizedColorComponent(g),
        normalizedColorComponent(b)
      )
    } else {
      const zRatio = Math.max(0, Math.min(1, (z - minZ) / zRange))
      color = new THREE.Color(0x2f80ed).lerp(new THREE.Color(0xf2c94c), zRatio)
    }
    colors[i] = color.r
    colors[i + 1] = color.g
    colors[i + 2] = color.b
    actualCount += 1
  }
  return {
    positions: actualCount === count ? positions : positions.subarray(0, actualCount * 3),
    colors: actualCount === count ? colors : colors.subarray(0, actualCount * 3),
    count: actualCount
  }
}

function buildNodePointsObject(sample, worldBounds, sourcePointCount) {
  const renderBounds = renderBoundsFromWorldArray(worldBounds)
  const size = renderBounds.getSize(new THREE.Vector3())
  const geometry = new THREE.BufferGeometry()
  geometry.setAttribute('position', new THREE.BufferAttribute(sample.positions, 3))
  geometry.setAttribute('color', new THREE.BufferAttribute(sample.colors, 3))
  const material = new THREE.PointsMaterial({
    size: pointMaterialSize(size, Math.max(sourcePointCount || sample.count, sample.count)),
    sizeAttenuation: false,
    vertexColors: true,
    transparent: true,
    opacity: 0.92
  })
  return new THREE.Points(geometry, material)
}

function copcBounds3D(copc) {
  const min = Array.isArray(copc?.header?.min) ? copc.header.min : []
  const max = Array.isArray(copc?.header?.max) ? copc.header.max : []
  if (min.length < 3 || max.length < 3) return {}
  return {
    min_x: Number(min[0]),
    min_y: Number(min[1]),
    min_z: Number(min[2]),
    max_x: Number(max[0]),
    max_y: Number(max[1]),
    max_z: Number(max[2])
  }
}

function getLazPerf() {
  if (!lazPerfPromise) {
    lazPerfPromise = createCOPCLazPerf()
  }
  return lazPerfPromise
}

async function loadCOPCPreview(url) {
  cancelCOPCLoad()
  const token = copcLoadToken
  const controller = new AbortController()
  copcAbortController = controller
  copcLoading.value = true
  copcError.value = ''
  copcPoints.value = []
  copcSummary.value = null
  copcVisiblePointCount.value = 0
  copcLoadedNodeCount.value = 0
  try {
    const [{ Copc }, lazPerf] = await Promise.all([
      import('copc'),
      getLazPerf()
    ])
    if (token !== copcLoadToken) return
    const getter = createRangeGetter(url, controller.signal)
    const copc = await Copc.create(getter)
    if (token !== copcLoadToken) return
    const hierarchySubtrees = await loadCOPCHierarchySubtrees(Copc, getter, copc.info.rootHierarchyPage)
    const globalBounds = worldBoundsArrayFromCOPC(copc)
    const initialEntries = enrichCOPCNodeEntries(collectHierarchyNodeEntries(hierarchySubtrees), copc?.info?.cube)
    if (!globalBounds || !initialEntries.length) {
      throw new Error('COPC 文件未包含可渲染层级')
    }
    if (token !== copcLoadToken) return
    const globalCenter = {
      x: globalBounds[0] + (globalBounds[3] - globalBounds[0]) / 2,
      y: globalBounds[1] + (globalBounds[4] - globalBounds[1]) / 2,
      z: globalBounds[2] + (globalBounds[5] - globalBounds[2]) / 2
    }
    sceneOrigin = new THREE.Vector3(globalCenter.x, globalCenter.y, globalCenter.z)
    ensureScene()
    disposePoints()
    pointsObject = new THREE.Group()
    scene.add(pointsObject)
    fitCamera(renderBoundsFromWorldArray(globalBounds))
    copcRuntime = {
      Copc,
      copc,
      getter,
      lazPerf,
      entries: [],
      pageEntries: [],
      nodeEntriesByKey: new Map(),
      pageEntriesByKey: new Map(),
      loadedHierarchyPageKeys: new Set(),
      pendingHierarchyPageKeys: new Set(),
      globalBounds,
      token
    }
    registerCOPCHierarchySubtrees(hierarchySubtrees)
    if (!copcRuntime.entries.length && initialEntries.length) {
      copcRuntime.entries = initialEntries
    }
    copcSummary.value = {
      point_count: Number(copc?.header?.pointCount || 0),
      sample_count: 0,
      bounds_3d: copcBounds3D(copc)
    }
    copcLoadedHierarchyPageCount.value = copcRuntime.loadedHierarchyPageKeys.size
    await updateCOPCLod(true)
  } catch (error) {
    if (token === copcLoadToken && error?.name !== 'AbortError') {
      copcError.value = error?.message || 'COPC 点云加载失败'
      copcPoints.value = []
      copcSummary.value = null
    }
  } finally {
    if (token === copcLoadToken) {
      copcLoading.value = false
      copcAbortController = null
    }
  }
}

function registerCOPCHierarchySubtrees(subtrees) {
  if (!copcRuntime) return
  subtrees.forEach((subtree) => {
    if (subtree?.pageKey) {
      copcRuntime.loadedHierarchyPageKeys.add(subtree.pageKey)
      copcRuntime.pendingHierarchyPageKeys.delete(subtree.pageKey)
      copcRuntime.pageEntriesByKey.delete(subtree.pageKey)
    }
  })
  const nodes = enrichCOPCNodeEntries(collectHierarchyNodeEntries(subtrees), copcRuntime.copc?.info?.cube)
  nodes.forEach((entry) => {
    copcRuntime.nodeEntriesByKey.set(entry.key, entry)
  })
  const pages = enrichCOPCNodeEntries(collectHierarchyPageEntries(subtrees), copcRuntime.copc?.info?.cube)
  pages.forEach((entry) => {
    if (!copcRuntime.loadedHierarchyPageKeys.has(entry.key)) {
      copcRuntime.pageEntriesByKey.set(entry.key, entry)
    }
  })
  copcRuntime.entries = [...copcRuntime.nodeEntriesByKey.values()]
  copcRuntime.pageEntries = [...copcRuntime.pageEntriesByKey.values()]
  updateCOPCRuntimeStats()
}

function updateCOPCRuntimeStats() {
  if (!copcRuntime) return
  copcLoadedHierarchyPageCount.value = copcRuntime.loadedHierarchyPageKeys.size
  copcRemainingHierarchyPageCount.value = copcRuntime.pageEntriesByKey.size
  copcPendingHierarchyPageCount.value = copcRuntime.pendingHierarchyPageKeys.size
  copcLoadedDepthLabel.value = depthLabel(copcRuntime.entries)
}

function depthLabel(entries) {
  const depths = (entries || [])
    .map((entry) => Number(entry?.depth))
    .filter(Number.isFinite)
  if (!depths.length) return ''
  const min = Math.min(...depths)
  const max = Math.max(...depths)
  return min === max ? String(max) : `${min}-${max}`
}

function scheduleCOPCLodUpdate() {
  if (!copcRuntime || !camera || !controls) return
  window.clearTimeout(copcLODTimer)
  copcLODTimer = window.setTimeout(() => {
    updateCOPCLod(false)
  }, 220)
}

async function updateCOPCLod(force) {
  if (!copcRuntime || !scene || !pointsObject || !camera || !controls) return
  const version = ++copcLODVersion
  await loadCOPCHierarchyPagesForView(version)
  if (version !== copcLODVersion || copcRuntime.token !== copcLoadToken) return
  const selectedEntries = currentCOPCNodeSelection()
  if (!selectedEntries.length) return
  const jobs = []
  let plannedPointCount = 0
  const pointBudget = currentCOPCPointBudget()
  for (const entry of selectedEntries) {
    if (version !== copcLODVersion || copcRuntime.token !== copcLoadToken) return
    if (entry.role === 'detail' && plannedPointCount >= pointBudget) continue
    const remaining = entry.role === 'overview'
      ? COPC_OVERVIEW_NODE_POINT_LIMIT
      : entry.role === 'coverage'
        ? COPC_COVERAGE_NODE_POINT_LIMIT
      : Math.max(pointBudget - plannedPointCount, 0)
    if (remaining <= 0) continue
    const nodePointLimit = entry.role === 'overview'
      ? Math.min(COPC_OVERVIEW_NODE_POINT_LIMIT, remaining)
      : entry.role === 'coverage'
        ? Math.min(COPC_COVERAGE_NODE_POINT_LIMIT, remaining)
        : Math.min(Number(entry.renderPointLimit || COPC_DETAIL_NODE_POINT_LIMIT), remaining)
    jobs.push({ entry, limit: nodePointLimit })
    plannedPointCount += nodePointLimit
  }
  const loaded = await loadCOPCNodeJobs(jobs, version)
  if (version !== copcLODVersion || copcRuntime.token !== copcLoadToken) return
  applyCOPCNodeObjects(loaded)
}

async function loadCOPCHierarchyPagesForView(version) {
  if (!copcRuntime || !camera || !controls || !renderer) return
  if (copcRuntime.loadedHierarchyPageKeys.size >= COPC_HIERARCHY_PAGE_TOTAL_LIMIT) return
  const visiblePages = visibleCOPCEntries(copcRuntime.pageEntries)
  const selectablePages = visiblePages.length ? visiblePages : copcRuntime.pageEntries
  const remaining = Math.max(COPC_HIERARCHY_PAGE_TOTAL_LIMIT - copcRuntime.loadedHierarchyPageKeys.size, 0)
  const pageEntries = selectCOPCHierarchyPages(selectablePages, currentCOPCView(), {
    pageLimit: Math.min(COPC_HIERARCHY_PAGE_LOAD_LIMIT, remaining)
  }).filter((entry) => (
    !copcRuntime.loadedHierarchyPageKeys.has(entry.key) &&
    !copcRuntime.pendingHierarchyPageKeys.has(entry.key)
  ))
  if (!pageEntries.length) return
  pageEntries.forEach((entry) => {
    copcRuntime.pendingHierarchyPageKeys.add(entry.key)
  })
  updateCOPCRuntimeStats()
  const subtrees = await loadCOPCHierarchyPageJobs(pageEntries, version)
  if (!subtrees.length || version !== copcLODVersion || copcRuntime.token !== copcLoadToken) return
  registerCOPCHierarchySubtrees(subtrees)
}

async function loadCOPCHierarchyPageJobs(entries, version) {
  const results = []
  let cursor = 0
  async function worker() {
    while (cursor < entries.length) {
      if (!copcRuntime || version !== copcLODVersion || copcRuntime.token !== copcLoadToken) return
      const index = cursor
      cursor += 1
      const entry = entries[index]
      try {
        const subtree = await copcRuntime.Copc.loadHierarchyPage(copcRuntime.getter, entry.page)
        if (!copcRuntime || version !== copcLODVersion || copcRuntime.token !== copcLoadToken) return
        subtree.pageKey = entry.key
        results.push(subtree)
      } catch (error) {
        if (error?.name === 'AbortError') return
        console.warn('COPC hierarchy page load failed', entry.key, error)
      } finally {
        copcRuntime?.pendingHierarchyPageKeys?.delete(entry.key)
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(COPC_HIERARCHY_PAGE_LOAD_CONCURRENCY, entries.length) }, () => worker()))
  return results
}

function currentCOPCNodeSelection() {
  if (!copcRuntime || !camera || !controls || !renderer) return []
  const visibleEntries = visibleCOPCEntries(copcRuntime.entries)
  const selectableEntries = visibleEntries.length ? visibleEntries : copcRuntime.entries
  const overview = selectCOPCOverviewNodes(copcRuntime.entries, COPC_OVERVIEW_NODE_LIMIT)
    .map((entry) => ({ ...entry, role: 'overview', renderPointLimit: COPC_OVERVIEW_NODE_POINT_LIMIT }))
  const coverage = selectCOPCCoverageNodes(selectableEntries, {
    nodeLimit: COPC_COVERAGE_NODE_LIMIT,
    minDepth: Math.max(4, maxDepth(copcRuntime.entries) - 4)
  }).map((entry) => ({ ...entry, role: 'coverage', renderPointLimit: COPC_COVERAGE_NODE_POINT_LIMIT }))
  const detail = selectCOPCDetailNodes(selectableEntries, {
    ...currentCOPCView()
  }, {
    nodeLimit: Math.max(COPC_PREVIEW_NODE_LIMIT - overview.length - coverage.length, 1),
    pointBudget: currentCOPCPointBudget(),
    nodePointLimit: COPC_DETAIL_NODE_POINT_LIMIT,
    minNodePointLimit: COPC_DETAIL_NODE_MIN_POINT_LIMIT
  }).map((entry) => enrichCOPCDetailEntry({ ...entry, role: 'detail' }))
  return mergeCOPCNodeSelections(detail, coverage, overview).slice(0, COPC_PREVIEW_NODE_LIMIT)
}

function maxDepth(entries) {
  const depths = (entries || []).map((entry) => Number(entry?.depth)).filter(Number.isFinite)
  return depths.length ? Math.max(...depths) : 0
}

function enrichCOPCDetailEntry(entry) {
  if (!copcRuntime) return entry
  const hasKnownDescendant = [...copcRuntime.nodeEntriesByKey.keys()].some((key) => hierarchyKeyAncestorOf(entry.key, key)) ||
    [...copcRuntime.pageEntriesByKey.keys()].some((key) => hierarchyKeyAncestorOf(entry.key, key))
  if (hasKnownDescendant) {
    return {
      ...entry,
      renderPointLimit: Math.min(
        Number(entry.node?.pointCount || COPC_PARENT_NODE_POINT_LIMIT),
        COPC_PARENT_NODE_POINT_LIMIT
      )
    }
  }
  if (Number(entry.projectedPixels || 0) < 60) return entry
  return {
    ...entry,
    renderPointLimit: Math.min(
      Number(entry.node?.pointCount || COPC_NEAR_LEAF_NODE_POINT_LIMIT),
      Math.max(Number(entry.renderPointLimit || 0), COPC_NEAR_LEAF_NODE_POINT_LIMIT)
    )
  }
}

async function loadCOPCNodePoints(entry, remaining) {
  const cached = copcNodePointCache.get(entry.key)
  if (cached && cached.sample.count >= remaining) {
    cached.lastUsedAt = Date.now()
    return sliceCOPCPointSample(cached.sample, remaining)
  }
  const view = await copcRuntime.Copc.loadPointDataView(copcRuntime.getter, copcRuntime.copc, entry.node, {
    lazPerf: copcRuntime.lazPerf,
    include: ['X', 'Y', 'Z', 'Intensity', 'Red', 'Green', 'Blue']
  })
  const sample = pointBuffersFromCOPCView(view, remaining, entry.bounds)
  if (!cached || sample.count > cached.sample.count) {
    copcNodePointCache.set(entry.key, { sample, pointCount: Number(entry.node?.pointCount || sample.count), lastUsedAt: Date.now() })
    trimCOPCNodePointCache()
  } else if (cached) {
    cached.lastUsedAt = Date.now()
  }
  return sample
}

function sliceCOPCPointSample(sample, count) {
  const nextCount = Math.min(Number(count || 0), Number(sample?.count || 0))
  if (nextCount >= sample.count) return sample
  return {
    positions: sample.positions.subarray(0, nextCount * 3),
    colors: sample.colors.subarray(0, nextCount * 3),
    count: nextCount
  }
}

function trimCOPCNodePointCache() {
  if (copcNodePointCache.size <= COPC_NODE_CACHE_LIMIT) return
  const activeKeys = new Set(activeCOPCNodeObjects.keys())
  const removable = [...copcNodePointCache.entries()]
    .filter(([key]) => !activeKeys.has(key))
    .sort((left, right) => Number(left[1]?.lastUsedAt || 0) - Number(right[1]?.lastUsedAt || 0))
  while (copcNodePointCache.size > COPC_NODE_CACHE_LIMIT && removable.length) {
    const [key] = removable.shift()
    copcNodePointCache.delete(key)
  }
}

async function loadCOPCNodeJobs(jobs, version) {
  const results = new Array(jobs.length)
  let cursor = 0
  async function worker() {
    while (cursor < jobs.length) {
      if (!copcRuntime || version !== copcLODVersion || copcRuntime.token !== copcLoadToken) return
      const index = cursor
      cursor += 1
      const job = jobs[index]
      const sample = await loadCOPCNodePoints(job.entry, job.limit)
      if (!copcRuntime || version !== copcLODVersion || copcRuntime.token !== copcLoadToken) return
      if (sample.count) {
        results[index] = { entry: job.entry, sample }
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(COPC_NODE_LOAD_CONCURRENCY, jobs.length) }, () => worker()))
  return results.filter(Boolean)
}

function applyCOPCNodeObjects(loadedEntries) {
  const selectedKeys = new Set(loadedEntries.map(({ entry }) => entry.key))
  activeCOPCNodeObjects.forEach((object, key) => {
    if (!selectedKeys.has(key)) {
      pointsObject.remove(object)
      object.geometry?.dispose?.()
      object.material?.dispose?.()
      activeCOPCNodeObjects.delete(key)
    }
  })
  let visiblePoints = 0
  copcActiveDepthLabel.value = depthLabel(loadedEntries.map(({ entry }) => entry))
  loadedEntries.forEach(({ entry, sample }) => {
    visiblePoints += sample.count
    const existing = activeCOPCNodeObjects.get(entry.key)
    if (existing && existing.userData?.pointSampleCount === sample.count) return
    if (existing) {
      pointsObject.remove(existing)
      existing.geometry?.dispose?.()
      existing.material?.dispose?.()
      activeCOPCNodeObjects.delete(entry.key)
    }
    const object = buildNodePointsObject(sample, entry.bounds, Number(entry.node?.pointCount || sample.count))
    object.userData.pointSampleCount = sample.count
    activeCOPCNodeObjects.set(entry.key, object)
    pointsObject.add(object)
  })
  copcVisiblePointCount.value = visiblePoints
  copcLoadedNodeCount.value = activeCOPCNodeObjects.size
  if (copcSummary.value) {
    copcSummary.value = {
      ...copcSummary.value,
      sample_count: visiblePoints,
      loaded_nodes: activeCOPCNodeObjects.size
    }
  }
}

function disposeScene() {
  window.cancelAnimationFrame(animationFrame)
  resizeObserver?.disconnect()
  resizeObserver = null
  disposePoints()
  controls?.dispose()
  renderer?.dispose()
  renderer?.domElement?.remove()
  renderer = null
  scene = null
  camera = null
  controls = null
}

watch(displayPoints, async (items) => {
  await nextTick()
  rebuildPointCloud(items)
}, { immediate: true })

watch([previewURL, shouldLoadCOPC], ([url, enabled]) => {
  if (!enabled) {
    cancelCOPCLoad()
    copcPoints.value = []
    copcSummary.value = null
    copcError.value = ''
    return
  }
  loadCOPCPreview(url)
}, { immediate: true })

onBeforeUnmount(() => {
  cancelCOPCLoad()
  disposeScene()
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

.point-cloud-link {
  color: var(--addp-color-primary);
  text-decoration: none;
}

.point-cloud-link:hover {
  text-decoration: underline;
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
