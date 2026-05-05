<template>
  <div class="image-preview-container">
    <div class="image-preview">
      <div v-if="imageSrc" class="image-wrapper">
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
import { computed, ref } from 'vue'
import { formatBytes } from '../utils/formatters'
import ExtractedMetadata from './ExtractedMetadata.vue'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const imageLoadedDimensions = ref({ width: 0, height: 0 })

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const metadata = computed(() => content.value?.metadata || {})
const attributes = computed(() => objectData.value?.attributes || {})

const imageData = computed(() => content.value?.image_data || content.value?.imageData || null)

// 提取元数据
const extractedMetadata = computed(() => {
  return attributes.value?.extensions?.extraction?.extracted_metadata || null
})
const hasExtractedMetadata = computed(() => Boolean(extractedMetadata.value))

// 从标准 media 扩展获取图像元数据
const imageMetadata = computed(() => {
  const media = attributes.value?.extensions?.media
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

const imageSrc = computed(() => {
  if (!imageData.value) return ''
  return `data:${contentType.value};base64,${imageData.value}`
})

const contentMessage = computed(() => content.value?.text || '图片超出预览限制，无法在线展示')

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

.image-wrapper img {
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
