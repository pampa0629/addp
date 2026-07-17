<template>
  <div class="s3m-preview">
    <div ref="viewportRef" class="s3m-viewport" />
    <div v-if="loading" class="s3m-status">{{ t('manager.explorer.s3mLoading') }}</div>
    <div v-else-if="errorMessage" class="s3m-status is-error">{{ errorMessage }}</div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAccessToken } from '@common-ui'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import { s3mCameraFitDistanceForBox } from '@/lib/supermap-s3m/three/S3MThreeCamera.js'
import S3MThreeLayer from '@/lib/supermap-s3m/three/S3MThreeLayer.js'
import {
  isThreeS3MViewState,
  S3M_THREE_RENDERER_RUNTIME
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
const content = computed(() => props.data?.object?.content || {})
const sourceURL = computed(() => content.value?.url || props.data?.object?.url || '')

const MIN_CAMERA_DISTANCE = 0.01
const MIN_NEAR_PLANE = 0.01

let renderer = null
let scene = null
let camera = null
let controls = null
let layer = null
let animationFrame = 0
let resizeObserver = null
let loadSerial = 0
let sceneBoundingRadius = 1

function ensureScene() {
  const viewport = viewportRef.value
  if (!viewport || renderer) return

  scene = new THREE.Scene()
  scene.background = null
  camera = new THREE.PerspectiveCamera(45, 1, 0.1, 100000000)
  camera.up.set(0, 0, 1)
  camera.position.set(120, -160, 120)

  renderer = new THREE.WebGLRenderer({
    antialias: true,
    alpha: true,
    logarithmicDepthBuffer: true
  })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.outputColorSpace = THREE.SRGBColorSpace
  renderer.setClearColor(0x000000, 0)
  viewport.appendChild(renderer.domElement)

  if (!renderer.extensions.has('WEBGL_compressed_texture_s3tc')) {
    throw new Error('WebGL S3TC extension is unavailable')
  }

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.08
  controls.minDistance = MIN_CAMERA_DISTANCE
  controls.maxDistance = Infinity
  if ('zoomToCursor' in controls) controls.zoomToCursor = true
  controls.addEventListener('end', emitCameraViewState)

  resizeObserver = new ResizeObserver(resize)
  resizeObserver.observe(viewport)
  resize()
  animate()
}

function animate() {
  if (!renderer || !scene || !camera) return
  controls?.update()
  updateCameraClipPlanes()
  layer?.update()
  renderer.render(scene, camera)
  animationFrame = window.requestAnimationFrame(animate)
}

function resize() {
  const viewport = viewportRef.value
  if (!viewport || !renderer || !camera) return
  const width = Math.max(1, viewport.clientWidth)
  const height = Math.max(1, viewport.clientHeight)
  renderer.setSize(width, height, false)
  camera.aspect = width / height
  camera.updateProjectionMatrix()
}

function finiteVector(values) {
  if (!Array.isArray(values) || values.length < 3) return null
  const vector = values.slice(0, 3).map(value => Number(value))
  return vector.every(Number.isFinite) ? vector : null
}

function applyCameraViewState(state) {
  if (!camera || !controls || !isThreeS3MViewState(state)) return false
  const position = finiteVector(state.position)
  const target = finiteVector(state.target)
  const up = finiteVector(state.up)
  if (!position || !target) return false
  camera.position.fromArray(position)
  controls.target.fromArray(target)
  if (up) camera.up.fromArray(up)
  controls.update()
  updateCameraClipPlanes(true)
  return true
}

function emitCameraViewState() {
  if (!camera || !controls) return
  emit('view-state-change', {
    renderer_runtime: S3M_THREE_RENDERER_RUNTIME,
    position: camera.position.toArray(),
    target: controls.target.toArray(),
    up: camera.up.toArray()
  })
}

function fitCameraToLayer(targetLayer) {
  if (!camera || !controls || !targetLayer) return false
  resize()
  const box = targetLayer.getBoundingBox()
  if (!box) return false
  const sphere = box.getBoundingSphere(new THREE.Sphere())
  const radius = Math.max(sphere.radius, 1)
  const viewDirection = new THREE.Vector3(1.15, -1.55, 1.25).normalize()
  const distance = s3mCameraFitDistanceForBox(
    box,
    viewDirection,
    new THREE.Vector3(0, 0, 1),
    camera.fov,
    camera.aspect
  )
  if (!distance) return false
  sceneBoundingRadius = radius
  camera.up.set(0, 0, 1)
  camera.position.copy(sphere.center).addScaledVector(viewDirection, distance)
  controls.target.copy(sphere.center)
  controls.minDistance = MIN_CAMERA_DISTANCE
  controls.maxDistance = Infinity
  controls.update()
  updateCameraClipPlanes(true)
  return true
}

function updateCameraClipPlanes(force = false) {
  if (!camera || !controls) return
  const distance = Math.max(camera.position.distanceTo(controls.target), MIN_CAMERA_DISTANCE)
  const radius = Math.max(sceneBoundingRadius, 1)
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

function authHeaders() {
  const token = getAccessToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

function disposeLayer(targetLayer = layer) {
  if (!targetLayer) return
  if (scene && targetLayer.group) scene.remove(targetLayer.group)
  targetLayer.dispose()
  if (layer === targetLayer) layer = null
}

async function loadS3M(url) {
  const currentLoad = ++loadSerial
  loading.value = true
  errorMessage.value = ''
  await nextTick()

  try {
    ensureScene()
  } catch (error) {
    if (currentLoad !== loadSerial) return
    console.error('S3M preview initialization failed:', error)
    errorMessage.value = t('manager.explorer.s3mLoadFailed', { error: error?.message || error })
    loading.value = false
    return
  }

  disposeLayer()
  sceneBoundingRadius = 1
  if (!url || !scene || !camera || !renderer) {
    loading.value = false
    errorMessage.value = t('manager.explorer.s3mMissingURL')
    return
  }

  const nextLayer = new S3MThreeLayer({
    url,
    camera,
    renderer,
    headers: authHeaders(),
    onTileError: error => console.warn('S3M child tile failed:', error)
  })
  layer = nextLayer
  scene.add(nextLayer.group)

  try {
    await nextLayer.load()
    if (currentLoad !== loadSerial || layer !== nextLayer) {
      disposeLayer(nextLayer)
      return
    }
    if (!applyCameraViewState(props.viewState) && !fitCameraToLayer(nextLayer)) {
      throw new Error('S3M scene has no renderable geometry')
    }
    loading.value = false
  } catch (error) {
    if (currentLoad !== loadSerial || layer !== nextLayer) {
      disposeLayer(nextLayer)
      return
    }
    console.error('S3M preview failed:', error)
    disposeLayer(nextLayer)
    errorMessage.value = t('manager.explorer.s3mLoadFailed', { error: error?.message || error })
    loading.value = false
  }
}

function disposeScene() {
  loadSerial += 1
  window.cancelAnimationFrame(animationFrame)
  resizeObserver?.disconnect()
  resizeObserver = null
  disposeLayer()
  controls?.removeEventListener?.('end', emitCameraViewState)
  controls?.dispose()
  renderer?.dispose()
  renderer?.domElement?.remove()
  renderer = null
  scene = null
  camera = null
  controls = null
}

watch(sourceURL, loadS3M, { immediate: true })
onBeforeUnmount(disposeScene)
</script>

<style scoped>
.s3m-preview {
  position: relative;
  min-height: 460px;
  height: min(68vh, 760px);
  overflow: hidden;
  background: linear-gradient(
    180deg,
    color-mix(in srgb, var(--addp-bg-secondary) 82%, transparent),
    var(--addp-bg-primary)
  );
  border: 1px solid var(--addp-border-color-light);
}

.s3m-viewport {
  width: 100%;
  height: 100%;
}

.s3m-viewport :deep(canvas) {
  display: block;
  width: 100%;
  height: 100%;
}

.s3m-status {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
  color: var(--addp-text-secondary);
  background: color-mix(in srgb, var(--addp-bg-primary) 70%, transparent);
}

.s3m-status.is-error {
  color: var(--el-color-danger);
}
</style>
