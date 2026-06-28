<template>
  <div class="three-preview model-preview">
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
const loadingText = '正在加载三维模型...'

let renderer = null
let scene = null
let camera = null
let controls = null
let animationFrame = 0
let resizeObserver = null
let activeObject = null

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const metadata = computed(() => content.value?.metadata || {})
const modelInfo = computed(() => metadata.value?.model_3d || {})

const modelURL = computed(() => {
  return content.value?.url || objectData.value?.url || ''
})

const sourceURL = computed(() => withAuthToken(modelURL.value))

const summaryItems = computed(() => {
  const info = modelInfo.value || {}
  const items = []
  if (info.model_kind) items.push({ label: 'Kind', value: info.model_kind })
  if (info.vertex_count) items.push({ label: 'Vertices', value: Number(info.vertex_count).toLocaleString() })
  if (info.triangle_count) items.push({ label: 'Triangles', value: Number(info.triangle_count).toLocaleString() })
  return items
})

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

function ensureScene() {
  const el = viewportRef.value
  if (!el || renderer) return
  scene = new THREE.Scene()
  scene.background = null

  camera = new THREE.PerspectiveCamera(45, 1, 0.01, 100000)
  camera.position.set(4, 3, 6)

  renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  renderer.outputColorSpace = THREE.SRGBColorSpace
  renderer.setClearColor(0x000000, 0)
  el.appendChild(renderer.domElement)

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.08

  scene.add(new THREE.HemisphereLight(0xffffff, 0x4b5563, 1.7))
  const keyLight = new THREE.DirectionalLight(0xffffff, 1.8)
  keyLight.position.set(5, 8, 6)
  scene.add(keyLight)
  const fillLight = new THREE.DirectionalLight(0x8fd3ff, 0.6)
  fillLight.position.set(-6, 3, -4)
  scene.add(fillLight)

  const grid = new THREE.GridHelper(10, 20, 0x7c8aa0, 0xd0d7e2)
  grid.material.opacity = 0.22
  grid.material.transparent = true
  scene.add(grid)

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

function clearModel() {
  if (!scene || !activeObject) return
  scene.remove(activeObject)
  activeObject.traverse?.((node) => {
    node.geometry?.dispose?.()
    if (Array.isArray(node.material)) {
      node.material.forEach((material) => material?.dispose?.())
    } else {
      node.material?.dispose?.()
    }
  })
  activeObject = null
}

function fitCamera(object) {
  if (!object || !camera || !controls) return
  const box = new THREE.Box3().setFromObject(object)
  if (box.isEmpty()) return
  const center = box.getCenter(new THREE.Vector3())
  const size = box.getSize(new THREE.Vector3())
  const maxDim = Math.max(size.x, size.y, size.z, 1)
  const distance = maxDim / (2 * Math.tan((camera.fov * Math.PI) / 360))
  camera.near = Math.max(maxDim / 10000, 0.01)
  camera.far = Math.max(maxDim * 100, distance * 10)
  camera.position.copy(center).add(new THREE.Vector3(distance * 0.75, distance * 0.55, distance * 1.15))
  camera.updateProjectionMatrix()
  controls.target.copy(center)
  controls.update()
}

async function loadModel(url) {
  loading.value = true
  errorMessage.value = ''
  await nextTick()
  ensureScene()
  clearModel()
  if (!url) {
    errorMessage.value = '缺少三维模型预览地址'
    loading.value = false
    return
  }
  const loader = new GLTFLoader()
  loader.load(
    url,
    (gltf) => {
      clearModel()
      activeObject = gltf.scene || gltf.scenes?.[0]
      if (!activeObject) {
        errorMessage.value = '模型内容为空'
      } else {
        scene.add(activeObject)
        fitCamera(activeObject)
      }
      loading.value = false
    },
    undefined,
    (error) => {
      errorMessage.value = error?.message || '三维模型加载失败'
      loading.value = false
    }
  )
}

function disposeScene() {
  window.cancelAnimationFrame(animationFrame)
  resizeObserver?.disconnect()
  resizeObserver = null
  clearModel()
  controls?.dispose()
  renderer?.dispose()
  renderer?.domElement?.remove()
  renderer = null
  scene = null
  camera = null
  controls = null
}

watch(sourceURL, (url) => loadModel(url), { immediate: true })
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
