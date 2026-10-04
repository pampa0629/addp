<template>
  <div class="image-preview-container">
    <div
      v-if="isTiffImage && pages.length"
      class="tiff-pages"
      role="group"
      :aria-label="t('common.imagePreview.pages')"
    >
      <el-button :disabled="pageIndex === 0" @click="pageIndex--">{{ t('common.imagePreview.previous') }}</el-button>
      <span aria-live="polite">{{ t('common.imagePreview.pagePosition', { page: pageIndex + 1, count: pages.length }) }}</span>
      <el-button :disabled="pageIndex === pages.length - 1" @click="pageIndex++">{{ t('common.imagePreview.next') }}</el-button>
      <span>{{ selectedPage.width }} × {{ selectedPage.height }}</span>
    </div>
    <div class="image-preview">
      <div v-if="isTiffImage" class="image-wrapper">
        <div v-if="tiffLoading || tiffError" class="placeholder" role="status">
          <p class="message">{{ tiffLoading ? t('common.imagePreview.loading') : t(tiffError) }}</p>
          <p v-if="tiffError" class="download-hint">{{ t('common.imagePreview.downloadHint') }}</p>
        </div>
        <canvas
          v-show="!tiffLoading && !tiffError"
          ref="tiffCanvasRef"
          class="tiff-canvas"
          :aria-label="fileName"
        />
      </div>
      <div v-else-if="imageSrc" class="image-wrapper">
        <img :src="imageSrc" :alt="fileName" />
      </div>
      <div v-else class="placeholder">
        <p class="message">{{ contentMessage }}</p>
        <p class="download-hint">{{ t('common.imagePreview.downloadHint') }}</p>
        <div v-if="showMetadata" class="meta-info">
          <div v-if="contentType" class="meta-row">
            <span class="meta-label">{{ t('common.imagePreview.contentType') }}</span>
            <span class="meta-value">{{ contentType }}</span>
          </div>
          <div v-if="formattedSize" class="meta-row">
            <span class="meta-label">{{ t('common.imagePreview.size') }}</span>
            <span class="meta-value">{{ formattedSize }}</span>
          </div>
          <div v-if="formattedLimit" class="meta-row">
            <span class="meta-label">{{ t('common.imagePreview.limit') }}</span>
            <span class="meta-value">{{ formattedLimit }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { fromCustomClient } from 'geotiff'
import { getAccessToken } from '../auth/authSession'
import { formatBytes } from '../utils/formatters'
import { tiffPages, tiffPreviewSize, readTIFFRGBA } from '../utils/tiffPreview'

const props = defineProps({ data: { type: Object, required: true } })
const { t } = useI18n()
const tiffCanvasRef = ref(null)
const tiffLoading = ref(false)
const tiffError = ref('')
const pageIndex = ref(0)
const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value.content || {})
const metadata = computed(() => content.value.metadata || {})
const pageSummary = computed(() => objectData.value.attributes?.format_info?.tiff)
const pages = computed(() => tiffPages(pageSummary.value))
const selectedPage = computed(() => pages.value[pageIndex.value])
const imageURL = computed(() => content.value.url || '')
const contentType = computed(() => metadata.value.content_type || objectData.value.content_type || '')
const fileName = computed(() => objectData.value.path || 'image')
const isTiffImage = computed(() => {
  const format = metadata.value.format || objectData.value.attributes?.item?.format
  return format === 'tiff' || contentType.value === 'image/tiff' || /\.tiff?$/i.test(fileName.value)
})
const imageSrc = computed(() => content.value.encoding === 'base64' && content.value.data
  ? `data:${contentType.value};base64,${content.value.data}` : imageURL.value)
const contentMessage = computed(() => content.value.text || t('common.imagePreview.unavailable'))
const formattedSize = computed(() => objectData.value.size_bytes ? formatBytes(objectData.value.size_bytes) : '')
const formattedLimit = computed(() => metadata.value.limit_bytes ? formatBytes(metadata.value.limit_bytes) : '')
const showMetadata = computed(() => Boolean(formattedSize.value || formattedLimit.value || contentType.value))
let controller
let revision = 0

const renderTiff = async () => {
  const current = ++revision
  controller?.abort()
  tiffLoading.value = false
  tiffError.value = ''
  if (!isTiffImage.value) return
  const page = selectedPage.value
  if (!page) {
    tiffError.value = pageSummary.value?.page_summary_status === 'unsupported'
      ? 'common.imagePreview.unsupportedDirectory' : 'common.imagePreview.refreshDirectory'
    return
  }
  let url
  try { url = new URL(imageURL.value, window.location.href) } catch { /* Report a missing range source below. */ }
  // Credentials are only attached to the platform-owned stream on this origin.
  if (!url || url.origin !== window.location.origin || url.pathname !== '/api/v1/manager/storage-stream') {
    tiffError.value = 'common.imagePreview.rangeRequired'
    return
  }
  const output = tiffPreviewSize(page.width, page.height)
  if (!output) {
    tiffError.value = 'common.imagePreview.pixelLimit'
    return
  }
  controller = new AbortController()
  const signal = controller.signal
  tiffLoading.value = true
  try {
    const token = getAccessToken()
    // geotiff.js 2.x does not forward the fromUrl signal when parsing later
    // IFDs. Its public client adapter binds every request to this page lifecycle.
    const client = {
      url: url.href,
      async request({ headers }) {
        const response = await fetch(url.href, { headers, credentials: 'include', signal })
        if (response.status !== 206) {
          await response.body?.cancel()
          throw new Error('rangeRequired')
        }
        return {
          ok: response.ok, status: response.status,
          getHeader: name => response.headers.get(name),
          getData: () => response.arrayBuffer()
        }
      }
    }
    const tiff = await fromCustomClient(client, {
      allowFullFile: false, blockSize: 65536, cacheSize: 8,
      headers: token ? { Authorization: `Bearer ${token}` } : {}
    }, signal)
    const image = await tiff.getImage(page.ifd_index)
    if (image.getWidth() !== page.width || image.getHeight() !== page.height) {
      throw new Error('directoryChanged')
    }
    const rgba = await readTIFFRGBA(image, output.width, output.height, signal)
    if (current !== revision || signal.aborted) return
    await nextTick()
    if (current !== revision || signal.aborted) return
    const canvas = tiffCanvasRef.value
    if (!canvas) return
    canvas.width = output.width
    canvas.height = output.height
    canvas.getContext('2d').putImageData(new ImageData(rgba, output.width, output.height), 0, 0)
  } catch (error) {
    if (current !== revision || signal.aborted) return
    const key = error?.message === 'directoryChanged' ? 'refreshDirectory'
      : error?.message === 'unsupportedSamples' ? 'unsupportedSamples' : 'decodeFailed'
    tiffError.value = `common.imagePreview.${key}`
  } finally {
    if (current === revision) tiffLoading.value = false
  }
}

watch(() => [props.data, imageURL.value, pageSummary.value], () => {
  pageIndex.value = 0
  renderTiff()
}, { immediate: true })
watch(pageIndex, renderTiff)
onBeforeUnmount(() => { revision++; controller?.abort() })
</script>

<style scoped>
.image-preview-container {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.tiff-pages {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
}

.image-preview {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
  padding: 20px;
  background: var(--el-fill-color);
  border: 1px dashed var(--el-border-color);
  border-radius: 6px;
}

.image-wrapper {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
}

.image-wrapper img,
.tiff-canvas {
  max-width: 100%;
  max-height: 500px;
  border-radius: 4px;
  box-shadow: var(--el-box-shadow-light);
  cursor: zoom-in;
  transition: transform 0.3s ease;
}

.image-wrapper img:hover {
  transform: scale(1.02);
}

.placeholder {
  color: var(--el-text-color-regular);
  font-size: 13px;
  text-align: left;
  padding: 24px;
  line-height: 1.6;
}

.message {
  margin: 0 0 12px;
}

.download-hint {
  margin: 0 0 12px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.meta-info {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.meta-row {
  display: flex;
  gap: 8px;
  font-size: 12px;
}

.meta-label {
  width: 72px;
  flex-shrink: 0;
  color: var(--el-text-color-secondary);
}

.meta-value {
  color: var(--el-text-color-primary);
}

</style>
