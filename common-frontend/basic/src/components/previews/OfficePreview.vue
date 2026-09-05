<template>
  <div class="office-preview">
    <el-alert
      v-if="isTruncated"
      class="office-alert"
      type="warning"
      :closable="false"
      :title="content.text || t('officePreview.tooLarge')"
    />

    <div v-else class="office-body">
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

      <div ref="viewerHost" v-show="!loading && !error" class="office-viewer-host"></div>
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Loading, RefreshRight } from '@element-plus/icons-vue'
import { getAccessToken } from '../../auth/authSession'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const { locale, t } = useI18n()
const viewerHost = ref(null)
const loading = ref(false)
const error = ref('')
let viewer = null
let loadToken = 0
let cachedBytes = null
let cachedSourceKey = ''

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

const mimeType = computed(() => {
  const explicit =
    content.value?.content_type ||
    content.value?.contentType ||
    objectData.value?.content_type ||
    objectData.value?.contentType ||
    metadata.value?.content_type ||
    metadata.value?.contentType
  if (explicit) return String(explicit)
  if (documentKind.value === 'doc') return 'application/msword'
  if (documentKind.value === 'rtf') return 'application/rtf'
  if (documentKind.value === 'wps') return 'application/vnd.ms-works'
  return 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
})

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
  const binary = atob(String(value || '').replace(/\s+/g, ''))
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
  const response = await fetch(url, { credentials: 'include', headers })
  if (!response.ok) throw new Error(t('officePreview.requestFailed', { status: response.status }))
  return new Uint8Array(await response.arrayBuffer())
}

async function getBytes() {
  const key = sourceKey.value
  if (cachedBytes && cachedSourceKey === key) return cachedBytes

  let bytes
  if (rawData.value) bytes = decodeBase64(rawData.value)
  else if (sourceURL.value) bytes = await fetchBytes(sourceURL.value)
  else throw new Error(t('officePreview.noSource'))
  if (!bytes.byteLength) throw new Error(t('officePreview.empty'))
  if (sourceKey.value === key) {
    cachedBytes = bytes
    cachedSourceKey = key
  }
  return bytes
}

function destroyViewer() {
  viewer?.destroy()
  viewer = null
}

function parserFileName() {
  if (documentKind.value !== 'wps') return fileName.value
  // WPS 文字文档使用 Word Binary File Format；0.1.44 通过 DOC 扩展名选择同一解析器。
  return fileName.value.replace(/\.wps$/i, '') + '.doc'
}

async function loadPreview() {
  const token = ++loadToken
  destroyViewer()
  cachedBytes = null
  cachedSourceKey = ''
  error.value = ''
  loading.value = false
  if (isTruncated.value || !viewerHost.value) return
  loading.value = true

  try {
    const [bytes, viewerModule] = await Promise.all([
      getBytes(),
      import('@open-file-viewer/core'),
      import('@open-file-viewer/core/style.css')
    ])
    if (token !== loadToken || !viewerHost.value) return

    const buffer = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength)
    viewer = viewerModule.createViewer({
      container: viewerHost.value,
      file: buffer,
      fileName: parserFileName(),
      mimeType: mimeType.value,
      locale: String(locale.value).toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-US',
      theme: document.documentElement.classList.contains('dark') ? 'dark' : 'light',
      fit: 'width',
      toolbar: {
        zoom: true,
        print: true,
        search: true,
        fullscreen: true,
        download: false
      },
      plugins: [viewerModule.officePlugin()],
      onLoad: () => {
        if (token === loadToken) loading.value = false
      },
      onError: (cause) => {
        if (token !== loadToken) return
        error.value = cause instanceof Error ? cause.message : String(cause)
        loading.value = false
      }
    })
  } catch (cause) {
    if (token !== loadToken) return
    error.value = cause instanceof Error ? cause.message : String(cause)
    loading.value = false
  }
}

watch(() => props.data, loadPreview, { deep: true })
watch(locale, loadPreview)
onMounted(loadPreview)
onBeforeUnmount(() => {
  loadToken += 1
  destroyViewer()
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

.office-body,
.office-viewer-host {
  flex: 1;
  min-height: 0;
}

.office-body {
  position: relative;
  overflow: hidden;
}

.office-viewer-host {
  width: 100%;
  height: 100%;
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
</style>
