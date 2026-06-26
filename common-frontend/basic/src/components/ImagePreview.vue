<template>
  <div class="image-preview-container">
    <div class="image-preview">
      <div v-if="isTiffImage" class="image-wrapper">
        <div v-if="!imageSrc || tiffLoading || tiffError" class="placeholder">
          <p class="message">{{ tiffLoading ? '正在解析 TIFF 图片...' : contentMessage }}</p>
          <p v-if="tiffError" class="download-hint">如需下载原始文件，请使用右上角的下载按钮</p>
        </div>
        <canvas
          v-show="imageSrc && !tiffLoading && !tiffError"
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
        <p class="download-hint">如需下载原始文件，请使用右上角的下载按钮</p>
        <div v-if="showMetadata" class="meta-info">
          <div class="meta-row" v-if="displayContentType">
            <span class="meta-label">文件类型</span>
            <span class="meta-value">{{ displayContentType }}</span>
          </div>
          <div class="meta-row" v-if="formattedSize">
            <span class="meta-label">文件大小</span>
            <span class="meta-value">{{ formattedSize }}</span>
          </div>
          <div class="meta-row" v-if="formattedLimit">
            <span class="meta-label">预览限制</span>
            <span class="meta-value">{{ formattedLimit }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, ref, watch } from 'vue'
import { fromArrayBuffer } from 'geotiff'
import { formatBytes } from '../utils/formatters'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const tiffCanvasRef = ref(null)
const tiffLoading = ref(false)
const tiffError = ref('')

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const metadata = computed(() => content.value?.metadata || {})

const imageURL = computed(() => {
  return (
    content.value?.url ||
    content.value?.preview_url ||
    content.value?.previewUrl ||
    content.value?.download_url ||
    content.value?.downloadUrl ||
    content.value?.signed_url ||
    content.value?.signedUrl ||
    objectData.value?.url ||
    objectData.value?.preview_url ||
    objectData.value?.previewUrl ||
    objectData.value?.download_url ||
    objectData.value?.downloadUrl ||
    objectData.value?.signed_url ||
    objectData.value?.signedUrl ||
    ''
  )
})

const sizeBytes = computed(() => {
  const objectSize = objectData.value?.size_bytes ?? objectData.value?.sizeBytes
  return objectSize ?? metadata.value?.size_bytes ?? null
})

const limitBytes = computed(() => metadata.value?.limit_bytes ?? null)

const contentType = computed(() => {
  return (
    metadata.value?.content_type ||
    objectData.value?.content_type ||
    objectData.value?.contentType ||
    'image/png'
  )
})

const fileName = computed(() => objectData.value?.path || objectData.value?.name || 'image')

const contentFormat = computed(() => {
  return String(
    metadata.value?.format ||
      objectData.value?.format ||
      ''
  ).toLowerCase()
})

const fileExtension = computed(() => {
  const name = fileName.value || ''
  const dot = name.lastIndexOf('.')
  return dot >= 0 ? name.slice(dot).toLowerCase() : ''
})

const isTiffImage = computed(() => {
  const type = String(contentType.value || '').toLowerCase()
  return (
    contentFormat.value === 'tiff' ||
    type.includes('image/tiff') ||
    ['.tif', '.tiff'].includes(fileExtension.value)
  )
})

const withAuthToken = (url) => {
  if (!url || typeof url !== 'string') return ''
  if (!url.startsWith('/api/v1/manager/storage-stream')) return url
  const token = localStorage.getItem('token')
  if (!token) return url
  const separator = url.includes('?') ? '&' : '?'
  return `${url}${separator}token=${encodeURIComponent(token)}`
}

const imageSrc = computed(() => {
  const rawData = content.value?.data || content.value?.Data || ''
  const encoding = String(content.value?.encoding || content.value?.Encoding || '').toLowerCase()
  if (rawData && encoding === 'base64') {
    return `data:${contentType.value || 'application/octet-stream'};base64,${rawData}`
  }
  return withAuthToken(imageURL.value)
})

const contentMessage = computed(() => content.value?.text || tiffError.value || '图片超出预览限制，无法在线展示')

const formattedSize = computed(() => {
  if (!sizeBytes.value) return ''
  return formatBytes(sizeBytes.value)
})

const formattedLimit = computed(() => {
  if (!limitBytes.value) return ''
  return formatBytes(limitBytes.value)
})

const displayContentType = computed(() => contentType.value || '')

const showMetadata = computed(
  () => Boolean(formattedSize.value || formattedLimit.value || displayContentType.value)
)

const dataURLToArrayBuffer = (dataURL) => {
  const base64 = dataURL.split(',', 2)[1] || ''
  const binary = atob(base64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i)
  }
  return bytes.buffer
}

const fetchImageArrayBuffer = async (src) => {
  if (!src) {
    throw new Error('缺少 TIFF 图片地址')
  }
  if (src.startsWith('data:')) {
    return dataURLToArrayBuffer(src)
  }
  const response = await fetch(src)
  if (!response.ok) {
    throw new Error(`读取 TIFF 图片失败: ${response.status}`)
  }
  return response.arrayBuffer()
}

const normalizeSample = (value, min, max) => {
  if (!Number.isFinite(value)) return 0
  if (max <= min) return 0
  return Math.max(0, Math.min(255, Math.round(((value - min) / (max - min)) * 255)))
}

const isNoDataSample = (value, noDataValue) => {
  if (!Number.isFinite(value)) return true
  return Number.isFinite(noDataValue) && value === noDataValue
}

const percentile = (values, ratio) => {
  if (!values.length) return 0
  const index = Math.max(0, Math.min(values.length - 1, Math.floor((values.length - 1) * ratio)))
  return values[index]
}

const rasterStats = (raster, noDataValue) => {
  const samples = []
  const step = Math.max(1, Math.floor(raster.length / 100000))
  for (let i = 0; i < raster.length; i += step) {
    const value = Number(raster[i])
    if (isNoDataSample(value, noDataValue)) continue
    samples.push(value)
  }
  if (!samples.length) {
    return { min: 0, max: 0 }
  }
  samples.sort((a, b) => a - b)
  let min = percentile(samples, 0.02)
  let max = percentile(samples, 0.98)
  if (max <= min) {
    min = samples[0]
    max = samples[samples.length - 1]
  }
  return { min, max }
}

const rgbaFromRaster = (raster, width, height, samplesPerPixel, noDataValue) => {
  const output = new Uint8ClampedArray(width * height * 4)
  const hasRGB = samplesPerPixel >= 3
  const stats = hasRGB ? null : rasterStats(raster, noDataValue)
  for (let pixel = 0; pixel < width * height; pixel += 1) {
    const src = pixel * samplesPerPixel
    const dest = pixel * 4
    if (hasRGB) {
      output[dest] = Number(raster[src]) || 0
      output[dest + 1] = Number(raster[src + 1]) || 0
      output[dest + 2] = Number(raster[src + 2]) || 0
      output[dest + 3] = samplesPerPixel >= 4 ? Number(raster[src + 3]) || 255 : 255
    } else {
      const value = Number(raster[src])
      const gray = isNoDataSample(value, noDataValue) ? 0 : normalizeSample(value, stats.min, stats.max)
      output[dest] = gray
      output[dest + 1] = gray
      output[dest + 2] = gray
      output[dest + 3] = isNoDataSample(value, noDataValue) ? 0 : 255
    }
  }
  return output
}

const renderTiff = async () => {
  if (!isTiffImage.value || !imageSrc.value) return
  tiffLoading.value = true
  tiffError.value = ''
  await nextTick()
  try {
    const arrayBuffer = await fetchImageArrayBuffer(imageSrc.value)
    const tiff = await fromArrayBuffer(arrayBuffer)
    const image = await tiff.getImage()
    const width = image.getWidth()
    const height = image.getHeight()
    const samplesPerPixel = image.getSamplesPerPixel()
    const noDataValue = Number(image.getGDALNoData?.())
    const raster = await image.readRasters({ interleave: true })
    const canvas = tiffCanvasRef.value
    if (!canvas) return
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    const imageData = new ImageData(rgbaFromRaster(raster, width, height, samplesPerPixel, noDataValue), width, height)
    ctx.putImageData(imageData, 0, 0)
  } catch (error) {
    tiffError.value = error?.message || 'TIFF 图片解析失败，请下载后查看'
  } finally {
    tiffLoading.value = false
  }
}

watch(
  () => [isTiffImage.value, imageSrc.value],
  () => {
    if (isTiffImage.value) {
      renderTiff()
    } else {
      tiffError.value = ''
      tiffLoading.value = false
    }
  },
  { immediate: true }
)
</script>

<style scoped>
.image-preview-container {
  display: flex;
  flex-direction: column;
  gap: 16px;
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
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.1);
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
