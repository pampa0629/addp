<template>
  <div class="image-preview-container">
    <div class="image-preview">
      <div v-if="isTiffImage" class="image-wrapper">
        <div v-if="tiffLoading || tiffError" class="placeholder">
          <p class="message">{{ tiffLoading ? '正在解析 TIFF 图片...' : contentMessage }}</p>
          <p v-if="tiffError" class="download-hint">如需下载原始文件，请使用右上角的下载按钮</p>
        </div>
        <canvas
          v-show="!tiffLoading && !tiffError"
          ref="tiffCanvasRef"
          class="tiff-canvas"
          :aria-label="fileName"
        />
      </div>
      <div v-else-if="imageSrc" class="image-wrapper">
        <img :src="imageSrc" :alt="fileName" @load="onImageLoad" />
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

    <!-- 元数据展示区域 -->
    <div v-if="hasExtractedMetadata" class="metadata-section">
      <ExtractedMetadata :metadata="extractedMetadata" />
    </div>

    <!-- 图像标准扩展信息 -->
    <div v-else-if="hasImageMetadata" class="quick-metadata">
      <h4><i class="el-icon-picture"></i> 图像信息</h4>
      <div class="quick-meta-grid">
        <div v-if="imageMetadata.resolution" class="quick-meta-item">
          <span class="label">分辨率</span>
          <span class="value">{{ imageMetadata.resolution }}</span>
        </div>
        <div v-if="imageMetadata.format" class="quick-meta-item">
          <span class="label">格式</span>
          <span class="value">{{ imageMetadata.format.toUpperCase() }}</span>
        </div>
        <div v-if="imageMetadata.megapixels" class="quick-meta-item">
          <span class="label">像素数</span>
          <span class="value">{{ imageMetadata.megapixels.toFixed(1) }} MP</span>
        </div>
        <div v-if="imageMetadata.aspect_ratio" class="quick-meta-item">
          <span class="label">宽高比</span>
          <span class="value">{{ imageMetadata.aspect_ratio }}</span>
        </div>
        <div v-if="imageMetadata.color_space" class="quick-meta-item">
          <span class="label">色彩空间</span>
          <span class="value">{{ imageMetadata.color_space }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, ref, watch } from 'vue'
import { formatBytes } from '../utils/formatters'
import ExtractedMetadata from './ExtractedMetadata.vue'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const imageLoadedDimensions = ref({ width: 0, height: 0 })
const tiffCanvasRef = ref(null)
const tiffLoading = ref(false)
const tiffError = ref('')

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const metadata = computed(() => content.value?.metadata || {})

const parseMaybeJSON = (value) => {
  if (!value || typeof value !== 'string') return value
  try {
    return JSON.parse(value)
  } catch (error) {
    return value
  }
}

const attributes = computed(() => {
  const parsed = parseMaybeJSON(objectData.value?.attributes)
  if (parsed && typeof parsed === 'object') {
    return parsed
  }
  const attrs = objectData.value?.attributes
  return attrs && typeof attrs === 'object' ? attrs : {}
})

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

// 提取元数据
const extractedMetadata = computed(() => {
  const raw = attributes.value?.capabilities?.extraction?.extracted_metadata
  const parsed = parseMaybeJSON(raw)
  return parsed && typeof parsed === 'object' ? parsed : null
})
const hasExtractedMetadata = computed(() => Boolean(extractedMetadata.value))

// 从标准 media 信息获取图像元数据
const imageMetadata = computed(() => {
  const media = attributes.value?.type_info?.media
  if (!media || typeof media !== 'object') {
    return null
  }
  const width = Number(media.width)
  const height = Number(media.height)
  return {
    ...media,
    resolution: media.resolution || (width && height ? `${width} × ${height}` : ''),
    megapixels: media.megapixels || (width && height ? (width * height) / 1000000 : 0),
    aspect_ratio: media.aspect_ratio || (width && height ? (width / height).toFixed(2) : ''),
    color_space: media.color_space || media.color_mode || media.mode || ''
  }
})

const hasImageMetadata = computed(() => Boolean(imageMetadata.value))

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
      attributes.value?.item?.format ||
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
  if (!url.startsWith('/api/v1/manager/object-stream')) return url
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

const contentMessage = computed(() => tiffError.value || content.value?.text || '图片超出预览限制，无法在线展示')

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

const onImageLoad = (event) => {
  const img = event.target
  imageLoadedDimensions.value = {
    width: img.naturalWidth,
    height: img.naturalHeight
  }
}

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

const rasterStats = (raster) => {
  let min = Infinity
  let max = -Infinity
  for (let i = 0; i < raster.length; i += 1) {
    const value = Number(raster[i])
    if (!Number.isFinite(value)) continue
    if (value < min) min = value
    if (value > max) max = value
  }
  if (!Number.isFinite(min) || !Number.isFinite(max)) {
    return { min: 0, max: 0 }
  }
  return { min, max }
}

const rgbaFromRaster = (raster, width, height, samplesPerPixel) => {
  const output = new Uint8ClampedArray(width * height * 4)
  const hasRGB = samplesPerPixel >= 3
  const stats = hasRGB ? null : rasterStats(raster)
  for (let pixel = 0; pixel < width * height; pixel += 1) {
    const src = pixel * samplesPerPixel
    const dest = pixel * 4
    if (hasRGB) {
      output[dest] = Number(raster[src]) || 0
      output[dest + 1] = Number(raster[src + 1]) || 0
      output[dest + 2] = Number(raster[src + 2]) || 0
      output[dest + 3] = samplesPerPixel >= 4 ? Number(raster[src + 3]) || 255 : 255
    } else {
      const gray = normalizeSample(Number(raster[src]), stats.min, stats.max)
      output[dest] = gray
      output[dest + 1] = gray
      output[dest + 2] = gray
      output[dest + 3] = 255
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
    const [{ fromArrayBuffer }, arrayBuffer] = await Promise.all([
      import('geotiff'),
      fetchImageArrayBuffer(imageSrc.value)
    ])
    const tiff = await fromArrayBuffer(arrayBuffer)
    const image = await tiff.getImage()
    const width = image.getWidth()
    const height = image.getHeight()
    const samplesPerPixel = image.getSamplesPerPixel()
    const raster = await image.readRasters({ interleave: true })
    const canvas = tiffCanvasRef.value
    if (!canvas) return
    canvas.width = width
    canvas.height = height
    const ctx = canvas.getContext('2d')
    const imageData = new ImageData(rgbaFromRaster(raster, width, height, samplesPerPixel), width, height)
    ctx.putImageData(imageData, 0, 0)
    imageLoadedDimensions.value = { width, height }
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

/* 元数据展示区域 */
.metadata-section {
  background: var(--el-bg-color);
  padding: 16px;
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
}

/* 快速元数据展示 */
.quick-metadata {
  background: var(--el-bg-color);
  padding: 16px;
  border-radius: 8px;
  border: 1px solid var(--el-border-color-lighter);
  border-left: 3px solid var(--el-color-warning);
}

.quick-metadata h4 {
  margin: 0 0 12px;
  font-size: 14px;
  font-weight: 600;
  color: var(--el-color-warning);
  display: flex;
  align-items: center;
  gap: 8px;
}

.quick-meta-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 12px;
}

.quick-meta-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.quick-meta-item .label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-weight: 500;
}

.quick-meta-item .value {
  font-size: 14px;
  color: var(--el-text-color-primary);
  font-weight: 600;
}
</style>
