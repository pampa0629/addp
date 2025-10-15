<template>
  <div class="image-preview">
    <div v-if="imageSrc" class="image-wrapper">
      <img :src="imageSrc" :alt="fileName" />
    </div>
    <div v-else class="placeholder">
      <p class="message">{{ contentMessage }}</p>
      <div v-if="showDownloadButton" class="actions">
        <el-button type="primary" size="small" @click="downloadImage">
          <el-icon><Download /></el-icon>
          下载文件
        </el-button>
      </div>
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
</template>

<script setup>
import { computed } from 'vue'
import { ElMessage } from 'element-plus'
import { Download } from '@element-plus/icons-vue'
import { formatBytes } from '@/utils/formatters'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const metadata = computed(() => content.value?.metadata || {})

const imageData = computed(() => content.value?.image_data || content.value?.imageData || null)

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

const resourceId = computed(() => {
  return (
    props.data?.resourceId ||
    props.data?.resource_id ||
    objectData.value?.resource_id ||
    objectData.value?.resourceId ||
    null
  )
})

const downloadUrl = computed(() => {
  const object = objectData.value || {}
  const contentValue = content.value || {}

  if (contentValue.download_url) return contentValue.download_url
  if (contentValue.downloadUrl) return contentValue.downloadUrl
  if (object.download_url) return object.download_url
  if (object.downloadUrl) return object.downloadUrl
  if (object.preview_url) return object.preview_url
  if (object.previewUrl) return object.previewUrl
  if (object.url) return object.url
  if (contentValue.preview_url) return contentValue.preview_url
  if (contentValue.previewUrl) return contentValue.previewUrl
  if (contentValue.url) return contentValue.url
  if (object.signed_url) return object.signed_url
  if (object.signedUrl) return object.signedUrl
  if (contentValue.signed_url) return contentValue.signed_url
  if (contentValue.signedUrl) return contentValue.signedUrl

  if (object.path && resourceId.value) {
    return `/api/preview/download?resource_id=${resourceId.value}&path=${encodeURIComponent(object.path)}`
  }

  return ''
})

const showDownloadButton = computed(() => !imageSrc.value && Boolean(downloadUrl.value))

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

const downloadImage = () => {
  try {
    if (imageSrc.value) {
      const link = document.createElement('a')
      link.href = imageSrc.value
      link.download = fileName.value
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      return
    }

    if (downloadUrl.value) {
      const link = document.createElement('a')
      link.href = downloadUrl.value
      link.download = fileName.value
      link.rel = 'noopener'
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      return
    }

    ElMessage.warning('暂无可用的下载链接')
  } catch (err) {
    console.error('图片下载失败:', err)
    ElMessage.error('图片下载失败')
  }
}
</script>

<style scoped>
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
  max-height: 360px;
  border-radius: 4px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
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

.actions {
  margin: 0 0 12px;
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
