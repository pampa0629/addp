<template>
  <div ref="fullscreenHost" class="office-preview">
    <el-alert
      v-if="isTruncated"
      class="office-alert"
      type="warning"
      :closable="false"
      :title="content.text || t('officePreview.tooLarge')"
    />

    <div v-else class="office-body">
      <div class="office-toolbar" role="toolbar" :aria-label="t('officePreview.toolbar')">
        <el-button-group size="small">
          <el-button
            :disabled="loading || zoom <= minimumZoom"
            :title="t('officePreview.zoomOut')"
            :aria-label="t('officePreview.zoomOut')"
            @click="zoomOut"
          >
            <el-icon><ZoomOut /></el-icon>
          </el-button>
          <el-button
            :disabled="loading"
            :title="t('officePreview.resetZoom')"
            :aria-label="t('officePreview.resetZoom')"
            @click="resetZoom"
          >
            {{ Math.round(zoom * 100) }}%
          </el-button>
          <el-button
            :disabled="loading || zoom >= maximumZoom"
            :title="t('officePreview.zoomIn')"
            :aria-label="t('officePreview.zoomIn')"
            @click="zoomIn"
          >
            <el-icon><ZoomIn /></el-icon>
          </el-button>
        </el-button-group>

        <el-button
          size="small"
          :disabled="loading || Boolean(error)"
          :title="t('officePreview.fullscreen')"
          :aria-label="t('officePreview.fullscreen')"
          @click="toggleFullscreen"
        >
          <el-icon><FullScreen /></el-icon>
        </el-button>
        <el-button
          size="small"
          :disabled="loading || Boolean(error)"
          :title="t('officePreview.print')"
          :aria-label="t('officePreview.print')"
          @click="printDocument"
        >
          <el-icon><Printer /></el-icon>
        </el-button>

        <div class="office-search">
          <el-input
            v-model="searchQuery"
            size="small"
            clearable
            :disabled="loading || Boolean(error)"
            :placeholder="t('officePreview.search')"
            :aria-label="t('officePreview.search')"
          >
            <template #prefix><el-icon><Search /></el-icon></template>
          </el-input>
          <span v-if="searchQuery" class="office-search-count">
            {{ t('officePreview.searchMatches', { count: searchMatchCount }) }}
          </span>
        </div>
      </div>

      <div
        ref="scrollHost"
        class="office-scroll"
        @touchstart="startPinchZoom"
        @touchmove="updatePinchZoom"
        @touchend="endPinchZoom"
        @touchcancel="endPinchZoom"
        @wheel="handleFullscreenWheelZoom"
      >
        <div v-if="loading" class="office-state">
          <el-icon class="is-loading"><Loading /></el-icon>
          <span>{{ t('officePreview.loading', { type: documentLabel }) }}</span>
        </div>

        <div v-else-if="error" class="office-state office-error">
          <el-alert type="error" :closable="false" :title="t('officePreview.loadFailed')">
            <template #default>
              <p>{{ error }}</p>
              <el-button size="small" @click="loadPreview">
                <el-icon><RefreshRight /></el-icon>
                {{ t('common.refresh') }}
              </el-button>
            </template>
          </el-alert>
        </div>

        <div
          ref="viewerHost"
          v-show="!loading && !error"
          class="office-viewer-host"
          :style="{ zoom }"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { FullScreen, Loading, Printer, RefreshRight, Search, ZoomIn, ZoomOut } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { getAccessToken } from '../../auth/authSession'
import { getTouchDistance, resolvePinchZoom, resolveWheelZoom } from '../../lib/office/pinchZoom'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const minimumZoom = 0.5
const maximumZoom = 2
const zoomStep = 0.1
const maximumSearchMatches = 1000

const { locale, t } = useI18n()
const fullscreenHost = ref(null)
const scrollHost = ref(null)
const viewerHost = ref(null)
const loading = ref(false)
const error = ref('')
const zoom = ref(1)
const searchQuery = ref('')
const searchMatchCount = ref(0)
let loadToken = 0
let cachedBytes = null
let cachedSourceKey = ''
let pinchGesture = null

class OfficePreviewSourceError extends Error {}

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const metadata = computed(() => content.value?.metadata || {})

const fileName = computed(() => {
  const explicitName = objectData.value?.name || objectData.value?.Name
  if (explicitName) return String(explicitName)
  const path = objectData.value?.path || objectData.value?.Path || ''
  return String(path).split('/').filter(Boolean).pop() || 'document.docx'
})

const fileExtension = computed(() => {
  const explicit = objectData.value?.extension || objectData.value?.Extension
  if (explicit) return String(explicit).replace(/^\./, '').toLowerCase()
  const match = fileName.value.toLowerCase().match(/\.([^.]+)$/)
  return match?.[1] || ''
})

const documentKind = computed(() => {
  const candidates = [
    content.value?.kind,
    content.value?.Kind,
    metadata.value?.format,
    objectData.value?.format,
    fileExtension.value
  ]
  for (const candidate of candidates) {
    const value = String(candidate || '').trim().toLowerCase()
    if (value === 'doc' || value === 'docx' || value === 'rtf' || value === 'wps') return value
  }
  return 'docx'
})

const documentLabel = computed(() => documentKind.value.toUpperCase())
const rawData = computed(() => content.value?.data || content.value?.Data || '')
const sourceURL = computed(() =>
  content.value?.url ||
  content.value?.URL ||
  content.value?.preview_url ||
  content.value?.previewUrl ||
  objectData.value?.preview_url ||
  objectData.value?.previewUrl ||
  props.data?.preview_url ||
  props.data?.previewUrl ||
  ''
)
const isTruncated = computed(() => Boolean(content.value?.truncated || objectData.value?.truncated))
const sourceKey = computed(() => `${fileName.value}\u0000${rawData.value}\u0000${sourceURL.value}`)

function decodeBase64(value) {
  let binary
  try {
    binary = atob(String(value || '').replace(/\s+/g, ''))
  } catch {
    throw new OfficePreviewSourceError(t('officePreview.invalidSource'))
  }
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index)
  }
  return bytes
}

async function fetchBytes(url) {
  const headers = {}
  const token = getAccessToken()
  if (token) headers.Authorization = `Bearer ${token}`
  let response
  try {
    response = await fetch(url, { credentials: 'include', headers })
  } catch {
    throw new OfficePreviewSourceError(t('officePreview.requestNetworkFailed'))
  }
  if (!response.ok) throw new OfficePreviewSourceError(t('officePreview.requestFailed', { status: response.status }))
  return new Uint8Array(await response.arrayBuffer())
}

async function getBytes() {
  const key = sourceKey.value
  if (cachedBytes && cachedSourceKey === key) return cachedBytes

  let bytes
  if (rawData.value) bytes = decodeBase64(rawData.value)
  else if (sourceURL.value) bytes = await fetchBytes(sourceURL.value)
  else throw new OfficePreviewSourceError(t('officePreview.noSource'))
  if (!bytes.byteLength) throw new OfficePreviewSourceError(t('officePreview.empty'))
  if (sourceKey.value === key) {
    cachedBytes = bytes
    cachedSourceKey = key
  }
  return bytes
}

function clearSearchHighlights() {
  if (!viewerHost.value) return
  const marks = viewerHost.value.querySelectorAll('mark[data-addp-office-search]')
  for (const mark of marks) {
    const parent = mark.parentNode
    mark.replaceWith(document.createTextNode(mark.textContent || ''))
    parent?.normalize()
  }
  searchMatchCount.value = 0
}

function applySearch() {
  clearSearchHighlights()
  const host = viewerHost.value
  const query = searchQuery.value.trim()
  if (!host || !query) return

  const normalizedQuery = query.toLocaleLowerCase(locale.value)
  const walker = document.createTreeWalker(host, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      if (!node.nodeValue?.trim()) return NodeFilter.FILTER_REJECT
      if (node.parentElement?.closest('script, style, mark[data-addp-office-search]')) {
        return NodeFilter.FILTER_REJECT
      }
      return NodeFilter.FILTER_ACCEPT
    }
  })
  const textNodes = []
  while (walker.nextNode()) textNodes.push(walker.currentNode)

  let matches = 0
  let firstMatch = null
  for (const node of textNodes) {
    if (matches >= maximumSearchMatches) break
    const text = node.nodeValue || ''
    const normalizedText = text.toLocaleLowerCase(locale.value)
    let cursor = 0
    let matchIndex = normalizedText.indexOf(normalizedQuery)
    if (matchIndex < 0) continue

    const fragment = document.createDocumentFragment()
    while (matchIndex >= 0 && matches < maximumSearchMatches) {
      fragment.append(document.createTextNode(text.slice(cursor, matchIndex)))
      const mark = document.createElement('mark')
      mark.dataset.addpOfficeSearch = 'true'
      mark.textContent = text.slice(matchIndex, matchIndex + query.length)
      fragment.append(mark)
      firstMatch ||= mark
      matches += 1
      cursor = matchIndex + query.length
      matchIndex = normalizedText.indexOf(normalizedQuery, cursor)
    }
    fragment.append(document.createTextNode(text.slice(cursor)))
    node.replaceWith(fragment)
  }
  searchMatchCount.value = matches
  firstMatch?.scrollIntoView({ block: 'center' })
}

async function loadPreview() {
  const token = ++loadToken
  cachedBytes = null
  cachedSourceKey = ''
  error.value = ''
  loading.value = false
  searchQuery.value = ''
  searchMatchCount.value = 0
  zoom.value = 1
  pinchGesture = null
  viewerHost.value?.replaceChildren()
  if (isTruncated.value || !viewerHost.value) return
  loading.value = true

  try {
    const [bytes, renderer] = await Promise.all([
      getBytes(),
      import('../../lib/office/renderOfficeDocument')
    ])
    if (token !== loadToken || !viewerHost.value) return

    const buffer = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength)
    await renderer.renderOfficeDocument(viewerHost.value, buffer, documentKind.value)
    if (token !== loadToken) return
    if (!viewerHost.value.textContent?.trim() && !viewerHost.value.querySelector('img')) {
      throw new Error(t('officePreview.empty'))
    }
    loading.value = false
  } catch (cause) {
    if (token !== loadToken) return
    if (!(cause instanceof OfficePreviewSourceError)) {
      console.error('[OfficePreview] Failed to render document', cause)
    }
    error.value = cause instanceof OfficePreviewSourceError
      ? cause.message
      : t('officePreview.renderFailed')
    loading.value = false
  }
}

function zoomOut() {
  zoom.value = Math.max(minimumZoom, Number((zoom.value - zoomStep).toFixed(1)))
}

function zoomIn() {
  zoom.value = Math.min(maximumZoom, Number((zoom.value + zoomStep).toFixed(1)))
}

function resetZoom() {
  zoom.value = 1
}

function getPointInScroll(clientX, clientY, host) {
  const bounds = host.getBoundingClientRect()
  return {
    x: clientX - bounds.left,
    y: clientY - bounds.top
  }
}

function getTouchCenter(touches, host) {
  return getPointInScroll(
    (touches[0].clientX + touches[1].clientX) / 2,
    (touches[0].clientY + touches[1].clientY) / 2,
    host
  )
}

function applyZoomAtPoint(nextZoom, center, contentPoint = null) {
  const scroll = scrollHost.value
  const viewer = viewerHost.value
  if (!scroll || !viewer || nextZoom === zoom.value) return

  const anchor = contentPoint || {
    x: (scroll.scrollLeft + center.x - viewer.offsetLeft) / zoom.value,
    y: (scroll.scrollTop + center.y - viewer.offsetTop) / zoom.value
  }
  zoom.value = nextZoom
  viewer.style.zoom = String(nextZoom)
  scroll.scrollLeft = viewer.offsetLeft + anchor.x * nextZoom - center.x
  scroll.scrollTop = viewer.offsetTop + anchor.y * nextZoom - center.y
}

function startPinchZoom(event) {
  if (document.fullscreenElement !== fullscreenHost.value || event.touches.length !== 2) return
  const scroll = scrollHost.value
  const viewer = viewerHost.value
  const distance = getTouchDistance(event.touches)
  if (!scroll || !viewer || distance <= 0) return

  const center = getTouchCenter(event.touches, scroll)
  pinchGesture = {
    distance,
    zoom: zoom.value,
    contentX: (scroll.scrollLeft + center.x - viewer.offsetLeft) / zoom.value,
    contentY: (scroll.scrollTop + center.y - viewer.offsetTop) / zoom.value
  }
  if (event.cancelable) event.preventDefault()
}

function updatePinchZoom(event) {
  if (
    !pinchGesture ||
    document.fullscreenElement !== fullscreenHost.value ||
    event.touches.length !== 2
  ) {
    pinchGesture = null
    return
  }

  const scroll = scrollHost.value
  const viewer = viewerHost.value
  if (!scroll || !viewer) return
  if (event.cancelable) event.preventDefault()

  const center = getTouchCenter(event.touches, scroll)
  const nextZoom = resolvePinchZoom(
    pinchGesture.zoom,
    pinchGesture.distance,
    getTouchDistance(event.touches),
    minimumZoom,
    maximumZoom
  )
  applyZoomAtPoint(nextZoom, center, {
    x: pinchGesture.contentX,
    y: pinchGesture.contentY
  })
}

function endPinchZoom(event) {
  if (event.touches.length < 2) pinchGesture = null
}

function handleFullscreenWheelZoom(event) {
  if (
    document.fullscreenElement !== fullscreenHost.value ||
    (!event.ctrlKey && !event.metaKey) ||
    !event.cancelable
  ) return

  const scroll = scrollHost.value
  if (!scroll) return
  event.preventDefault()
  applyZoomAtPoint(
    resolveWheelZoom(zoom.value, event.deltaY, minimumZoom, maximumZoom),
    getPointInScroll(event.clientX, event.clientY, scroll)
  )
}

async function toggleFullscreen() {
  try {
    if (document.fullscreenElement) await document.exitFullscreen()
    else await fullscreenHost.value?.requestFullscreen()
  } catch {
    ElMessage.error(t('officePreview.fullscreenFailed'))
  }
}

function printDocument() {
  const printWindow = window.open('', '_blank')
  if (!printWindow || !viewerHost.value) {
    ElMessage.error(t('officePreview.printFailed'))
    return
  }
  printWindow.opener = null
  printWindow.document.title = fileName.value
  const style = printWindow.document.createElement('style')
  style.textContent = `
    body { margin: 0; font: 14px/1.7 system-ui, sans-serif; }
    article, section { box-sizing: border-box; max-width: 100%; margin: 0 auto; padding: 24px; }
    img { max-width: 100%; }
    table { width: 100%; border-collapse: collapse; }
    th, td { border: 1px solid currentColor; padding: 6px 8px; }
    pre { white-space: pre-wrap; word-break: break-word; font: inherit; }
  `
  printWindow.document.head.append(style)
  printWindow.document.body.append(viewerHost.value.cloneNode(true))
  printWindow.focus()
  printWindow.print()
  printWindow.close()
}

watch(() => props.data, loadPreview, { deep: true })
watch(locale, loadPreview)
watch(searchQuery, () => nextTick(applySearch))
onMounted(loadPreview)
onBeforeUnmount(() => {
  loadToken += 1
  pinchGesture = null
  clearSearchHighlights()
})
</script>

<style scoped>
.office-preview {
  width: 100%;
  height: 100%;
  min-height: 360px;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-secondary);
}

.office-alert {
  margin: 24px;
}

.office-body {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.office-toolbar {
  flex: none;
  min-height: 48px;
  padding: 8px 12px;
  display: flex;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid var(--addp-border-color);
  background: var(--addp-bg-primary);
}

.office-search {
  margin-left: auto;
  min-width: 220px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.office-search-count {
  flex: none;
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.office-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 24px;
}

.office-preview:fullscreen .office-scroll {
  touch-action: pan-x pan-y;
}

.office-viewer-host {
  width: 100%;
  min-height: 100%;
  transform-origin: top center;
}

.office-state {
  height: 100%;
  min-height: 240px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--addp-text-secondary);
}

.office-error {
  padding: 24px;
}

.office-error .el-alert {
  max-width: 680px;
}

.office-error p {
  margin: 0 0 12px;
  word-break: break-word;
}

:deep(.addp-office-document),
:deep(.ofv-msdoc-page) {
  box-sizing: border-box;
  width: min(100%, 920px);
  margin: 0 auto;
  padding: 40px 48px;
  color: var(--addp-text-primary);
  background: var(--addp-bg-primary);
  box-shadow: var(--addp-shadow-card);
  font-size: 15px;
  line-height: 1.75;
}

:deep(.addp-office-document img),
:deep(.ofv-msdoc-document img) {
  max-width: 100%;
  height: auto;
}

:deep(.addp-office-document table),
:deep(.ofv-msdoc-table) {
  width: 100%;
  border-collapse: collapse;
}

:deep(.addp-office-document th),
:deep(.addp-office-document td),
:deep(.ofv-msdoc-table th),
:deep(.ofv-msdoc-table td) {
  padding: 6px 8px;
  border: 1px solid var(--addp-border-color);
  vertical-align: top;
}

:deep(.addp-office-plain-text) {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font: inherit;
}

:deep(.ofv-msdoc-document) {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

:deep(.ofv-msdoc-page) {
  min-height: 600px;
}

:deep(.ofv-msdoc-page-header),
:deep(.ofv-msdoc-page-footer),
:deep(.ofv-msdoc-meta) {
  color: var(--addp-text-secondary);
  font-size: 12px;
}

:deep(.ofv-msdoc-title) {
  margin: 16px 0 24px;
  font-size: 26px;
  font-weight: 700;
  text-align: center;
}

:deep(.ofv-msdoc-heading-level-1) {
  font-size: 22px;
  font-weight: 700;
}

:deep(.ofv-msdoc-heading-level-2) {
  font-size: 19px;
  font-weight: 650;
}

:deep(.ofv-msdoc-heading-level-3) {
  font-size: 17px;
  font-weight: 600;
}

:deep(mark[data-addp-office-search]) {
  color: inherit;
  background: var(--el-color-warning-light-5);
}

@media (max-width: 720px) {
  .office-toolbar {
    flex-wrap: wrap;
  }

  .office-search {
    width: 100%;
    min-width: 0;
  }

  .office-scroll {
    padding: 12px;
  }

  :deep(.addp-office-document),
  :deep(.ofv-msdoc-page) {
    padding: 24px 20px;
  }
}
</style>
