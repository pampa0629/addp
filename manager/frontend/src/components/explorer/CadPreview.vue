<template>
  <div class="cad-preview">
    <div ref="viewportRef" class="cad-viewport" />
    <div v-if="loading" class="cad-status">{{ t('manager.explorer.cadLoading') }}</div>
    <div v-else-if="errorMessage" class="cad-status is-error">{{ errorMessage }}</div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Map from 'ol/Map.js'
import View from 'ol/View.js'
import TileLayer from 'ol/layer/Tile.js'
import XYZ from 'ol/source/XYZ.js'
import Projection from 'ol/proj/Projection.js'
import { resolveCADTileURL } from '@/utils/cadPreviewURL'

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
const manifestURL = computed(() => content.value?.url || content.value?.metadata?.manifest_url || '')
let map = null
let objectURLs = new Set()
let loadSerial = 0

function authHeaders() {
  const token = localStorage.getItem('token')
  return token ? { Authorization: `Bearer ${token}` } : {}
}

function disposeMap() {
  if (map) map.setTarget(undefined)
  map = null
  for (const url of objectURLs) URL.revokeObjectURL(url)
  objectURLs = new Set()
  if (viewportRef.value) viewportRef.value.replaceChildren()
}

async function loadManifest(url) {
  const serial = ++loadSerial
  loading.value = true
  errorMessage.value = ''
  await nextTick()
  disposeMap()
  if (!url || !viewportRef.value) {
    loading.value = false
    errorMessage.value = t('manager.explorer.cadMissingManifest')
    return
  }
  try {
    const response = await fetch(url, { headers: authHeaders() })
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`)
    const manifest = await response.json()
    if (serial !== loadSerial) return
    const bounds = manifest.bounds_2d || {}
    const extent = [bounds.min_x, bounds.min_y, bounds.max_x, bounds.max_y].map(Number)
    if (!extent.every(Number.isFinite) || extent[2] <= extent[0] || extent[3] <= extent[1]) {
      throw new Error('invalid bounds_2d')
    }
    const projection = new Projection({ code: `ADDP-CAD-${serial}`, units: 'm', extent })
    const template = manifest.tile_url_template || manifest.tile_template
    if (!template) throw new Error('missing tile template')
    const source = new XYZ({
      projection,
      minZoom: Number(manifest.min_zoom) || 0,
      maxZoom: Number(manifest.max_zoom) || 0,
      tileSize: Number(manifest.tile_size) || 512,
      tileUrlFunction: ([z, x, y]) => resolveCADTileURL(template, z, x, y, url),
      tileLoadFunction: async (imageTile, src) => {
        try {
          const tileResponse = await fetch(src, { headers: authHeaders() })
          if (!tileResponse.ok) throw new Error(String(tileResponse.status))
          const objectURL = URL.createObjectURL(await tileResponse.blob())
          objectURLs.add(objectURL)
          imageTile.getImage().src = objectURL
        } catch (error) {
          console.warn('CAD tile load failed', src, error)
        }
      }
    })
    const initialCenter = Array.isArray(props.viewState?.center) ? props.viewState.center.map(Number) : null
    const center = initialCenter?.length === 2 && initialCenter.every(Number.isFinite)
      ? initialCenter
      : [(extent[0] + extent[2]) / 2, (extent[1] + extent[3]) / 2]
    map = new Map({
      target: viewportRef.value,
      layers: [new TileLayer({ source })],
      view: new View({ projection, center, zoom: Number(props.viewState?.zoom) || 0, extent })
    })
    map.on('moveend', () => {
      const view = map?.getView()
      if (!view) return
      emit('view-state-change', { center: view.getCenter(), zoom: view.getZoom(), space_id: manifest.default_space || 'model-space' })
    })
    loading.value = false
  } catch (error) {
    if (serial !== loadSerial) return
    errorMessage.value = t('manager.explorer.cadLoadFailed', { error: error?.message || error })
    loading.value = false
  }
}

watch(manifestURL, loadManifest, { immediate: true })
onBeforeUnmount(() => {
  loadSerial++
  disposeMap()
})
</script>

<style scoped>
.cad-preview {
  position: relative;
  min-height: 460px;
  height: min(68vh, 760px);
  overflow: hidden;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color-light);
}
.cad-viewport { width: 100%; height: 100%; }
.cad-status {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
  color: var(--addp-text-secondary);
  background: color-mix(in srgb, var(--addp-bg-primary) 75%, transparent);
}
.cad-status.is-error { color: var(--el-color-danger); }
</style>
