<template>
  <div class="three-preview point-cloud-preview">
    <div ref="viewportRef" class="three-viewport" />
    <div v-if="!points.length" class="three-status">{{ emptyText }}</div>
    <div v-if="summaryItems.length" class="three-summary">
      <span v-for="item in summaryItems" :key="item.label">{{ item.label }}: {{ item.value }}</span>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'

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
const emptyText = computed(() => props.loading ? '正在加载点云...' : '没有可展示的点云样本')

let renderer = null
let scene = null
let camera = null
let controls = null
let animationFrame = 0
let resizeObserver = null
let pointsObject = null

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const payload = computed(() => content.value?.json || content.value?.JSON || {})
const metadata = computed(() => content.value?.metadata || {})

const points = computed(() => {
  const items = Array.isArray(payload.value?.points) ? payload.value.points : []
  return items
    .map((point) => ({
      x: Number(point?.x),
      y: Number(point?.y),
      z: Number(point?.z),
      intensity: Number(point?.intensity)
    }))
    .filter((point) => Number.isFinite(point.x) && Number.isFinite(point.y) && Number.isFinite(point.z))
})

const pointCount = computed(() => Number(payload.value?.point_count || metadata.value?.point_count || points.value.length || 0))
const sampleCount = computed(() => Number(payload.value?.sample_count || metadata.value?.sample_count || points.value.length || 0))
const bounds3D = computed(() => payload.value?.bounds_3d || metadata.value?.bounds_3d || {})

const summaryItems = computed(() => {
  const items = []
  if (pointCount.value) items.push({ label: 'Points', value: pointCount.value.toLocaleString() })
  if (sampleCount.value) items.push({ label: 'Sample', value: sampleCount.value.toLocaleString() })
  const format = String(payload.value?.format || metadata.value?.format || '').toUpperCase()
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
  pointsObject.geometry?.dispose?.()
  pointsObject.material?.dispose?.()
  pointsObject = null
}

function pointBounds(items) {
  const bounds = new THREE.Box3()
  items.forEach((point) => bounds.expandByPoint(new THREE.Vector3(point.x, point.y, point.z)))
  return bounds
}

function normalizedBounds(items) {
  const attr = bounds3D.value || {}
  const minX = Number(attr.min_x)
  const minY = Number(attr.min_y)
  const minZ = Number(attr.min_z)
  const maxX = Number(attr.max_x)
  const maxY = Number(attr.max_y)
  const maxZ = Number(attr.max_z)
  if ([minX, minY, minZ, maxX, maxY, maxZ].every(Number.isFinite)) {
    return new THREE.Box3(
      new THREE.Vector3(minX, minY, minZ),
      new THREE.Vector3(maxX, maxY, maxZ)
    )
  }
  return pointBounds(items)
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
  controls.update()
}

function rebuildPointCloud(items) {
  ensureScene()
  disposePoints()
  if (!scene || !items.length) return

  const bounds = normalizedBounds(items)
  const center = bounds.getCenter(new THREE.Vector3())
  const size = bounds.getSize(new THREE.Vector3())
  const maxDim = Math.max(size.x, size.y, size.z, 1)
  const positions = new Float32Array(items.length * 3)
  const colors = new Float32Array(items.length * 3)
  const minZ = Number.isFinite(bounds.min.z) ? bounds.min.z : 0
  const maxZ = Number.isFinite(bounds.max.z) ? bounds.max.z : minZ + 1
  const zRange = Math.max(1, maxZ - minZ)

  items.forEach((point, index) => {
    const i = index * 3
    positions[i] = point.x - center.x
    positions[i + 1] = point.z - center.z
    positions[i + 2] = point.y - center.y
    const zRatio = Math.max(0, Math.min(1, (point.z - minZ) / zRange))
    const low = new THREE.Color(0x2f80ed)
    const high = new THREE.Color(0xf2c94c)
    const color = low.lerp(high, zRatio)
    colors[i] = color.r
    colors[i + 1] = color.g
    colors[i + 2] = color.b
  })

  const geometry = new THREE.BufferGeometry()
  geometry.setAttribute('position', new THREE.BufferAttribute(positions, 3))
  geometry.setAttribute('color', new THREE.BufferAttribute(colors, 3))

  const material = new THREE.PointsMaterial({
    size: Math.max(maxDim * 0.005, 0.8),
    sizeAttenuation: true,
    vertexColors: true,
    transparent: true,
    opacity: 0.92
  })
  pointsObject = new THREE.Points(geometry, material)
  scene.add(pointsObject)

  const localBounds = pointBounds(items)
  localBounds.min.sub(center)
  localBounds.max.sub(center)
  const helper = new THREE.Box3Helper(localBounds, 0x7c8aa0)
  pointsObject.add(helper)

  fitCamera(new THREE.Box3(
    new THREE.Vector3(-size.x / 2, -size.z / 2, -size.y / 2),
    new THREE.Vector3(size.x / 2, size.z / 2, size.y / 2)
  ))
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

watch(points, async (items) => {
  await nextTick()
  rebuildPointCloud(items)
}, { immediate: true })

onBeforeUnmount(disposeScene)
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
