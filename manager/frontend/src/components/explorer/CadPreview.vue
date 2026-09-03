<template>
  <div class="cad-preview">
    <div ref="viewportRef" class="cad-viewport" />
    <div v-if="loading" class="cad-status" role="status">
      {{ t('manager.explorer.cadLoading') }}
    </div>
    <div v-else-if="errorMessage" class="cad-status is-error" role="alert">
      {{ errorMessage }}
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAccessToken } from '@common-ui'

const props = defineProps({
  data: { type: Object, required: true }
})

const { t } = useI18n()
const viewportRef = ref(null)
const loading = ref(false)
const errorMessage = ref('')
const content = computed(() => props.data?.object?.content || {})
const sourceURL = computed(() => content.value?.url || content.value?.metadata?.source_url || '')

let activeManager = null
let abortController = null
let loadSerial = 0
let runtimePromise = null
let converterPromise = null
let pendingDestroy = Promise.resolve()

function runtimeAssetURL(fileName) {
  const base = String(import.meta.env.BASE_URL || '/').replace(/\/+$/, '')
  return new URL(`${base}/cad-engine/${fileName}`, window.location.origin).href
}

function authHeaders() {
  const token = getAccessToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

function drawingName() {
  const object = props.data?.object || {}
  const candidate = String(object.path || object.name || object.storage_ref || 'drawing.dwg')
  return candidate.split('/').filter(Boolean).pop() || 'drawing.dwg'
}

async function loadRuntime() {
  if (!runtimePromise) {
    runtimePromise = Promise.all([
      import('@mlightcad/cad-simple-viewer'),
      import('@mlightcad/data-model'),
      import('@mlightcad/libredwg-converter')
    ]).then(([engine, dataModel, converter]) => ({ engine, dataModel, converter }))
  }
  const runtime = await runtimePromise
  if (!converterPromise) {
    converterPromise = Promise.resolve().then(() => {
      const dwgConverter = new runtime.converter.AcDbLibreDwgConverter({
        convertByEntityType: false,
        parserWorkerUrl: runtimeAssetURL('libredwg-parser-worker.js'),
        useWorker: true
      })
      runtime.dataModel.AcDbDatabaseConverterManager.instance.register(
        runtime.dataModel.AcDbFileType.DWG,
        dwgConverter
      )
    })
  }
  await converterPromise
  return runtime.engine
}

async function disposeManager() {
  abortController?.abort()
  abortController = null
  const manager = activeManager
  activeManager = null
  if (!manager) return pendingDestroy
  pendingDestroy = manager.destroy().catch((error) => {
    console.warn('释放 CAD WebGL 预览失败', error)
  })
  return pendingDestroy
}

async function loadDrawing(url) {
  const serial = ++loadSerial
  loading.value = true
  errorMessage.value = ''
  await nextTick()
  await disposeManager()

  if (!url || !viewportRef.value || serial !== loadSerial) {
    if (serial === loadSerial) {
      loading.value = false
      errorMessage.value = t('manager.explorer.cadMissingSource')
    }
    return
  }

  const controller = new AbortController()
  abortController = controller
  let manager = null
  try {
    const [engine, response] = await Promise.all([
      loadRuntime(),
      fetch(url, { headers: authHeaders(), signal: controller.signal })
    ])
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`)
    const arrayBuffer = await response.arrayBuffer()
    if (serial !== loadSerial) return

    manager = engine.AcApDocManager.createInstance({
      container: viewportRef.value,
      autoResize: true,
      busyIndicatorHost: viewportRef.value,
      builtinOpenFileDialog: false,
      preloadDefaultFonts: false,
      useMainThreadDraw: false,
      webworkerFileUrls: {
        dwgParser: runtimeAssetURL('libredwg-parser-worker.js'),
        mtextRender: runtimeAssetURL('mtext-renderer-worker.js')
      }
    })
    if (!manager) throw new Error('CAD rendering engine could not be initialized')
    if (!(await manager.areWorkersReady())) throw new Error('CAD worker resources are unavailable')

    const opened = await manager.openDocument(drawingName(), arrayBuffer, {
      mode: engine.AcEdOpenMode.Read,
      openViewMode: engine.AcApOpenViewMode.Extents,
      progressiveRendering: true
    })
    if (!opened) throw new Error('CAD document could not be opened')
    if (serial !== loadSerial) {
      await manager.destroy()
      return
    }

    activeManager = manager
    manager = null
    activeManager.curView.zoomToFitDrawing()
    loading.value = false
  } catch (error) {
    if (manager) await manager.destroy().catch(() => undefined)
    if (serial !== loadSerial || error?.name === 'AbortError') return
    errorMessage.value = t('manager.explorer.cadLoadFailed', { error: error?.message || error })
    loading.value = false
  } finally {
    if (abortController === controller) abortController = null
  }
}

watch(sourceURL, loadDrawing, { immediate: true })

onBeforeUnmount(() => {
  loadSerial++
  void disposeManager()
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

.cad-viewport {
  width: 100%;
  height: 100%;
}

.cad-status {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
  color: var(--addp-text-secondary);
  background: color-mix(in srgb, var(--addp-bg-primary) 75%, transparent);
}

.cad-status.is-error {
  color: var(--el-color-danger);
}
</style>
