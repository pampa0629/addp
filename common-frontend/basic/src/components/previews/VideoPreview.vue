<template>
  <div class="video-preview">
    <!-- 视频播放器 -->
    <div v-if="videoSrc" class="video-wrapper">
      <video
        ref="videoPlayer"
        :src="videoSrc"
        controls
        preload="metadata"
        @loadedmetadata="onVideoLoaded"
        @error="onVideoError"
      >
        您的浏览器不支持视频播放
      </video>

      <!-- 视频控制信息提示 -->
      <div class="video-hint">
        <el-icon><VideoPlay /></el-icon>
        <span>点击播放按钮开始观看视频</span>
      </div>
    </div>

    <!-- 加载状态或错误提示 -->
    <div v-else class="placeholder">
      <el-icon v-if="loading" class="loading-icon"><Loading /></el-icon>
      <p class="message">{{ contentMessage }}</p>
      <p v-if="!loading" class="download-hint">如需下载原始文件，请使用右上角的下载按钮</p>
    </div>

    <div v-if="hasMetadata" class="metadata-section">
      <el-divider content-position="left">
        <el-icon><Collection /></el-icon>
        <span style="margin-left: 8px;">媒体信息</span>
      </el-divider>
      <div class="metadata-grid">
        <div class="metadata-group">
          <h4>
            <el-icon><VideoPlay /></el-icon>
            视频信息
          </h4>
          <div class="meta-items">
            <div v-if="formattedSize" class="meta-row">
              <span class="meta-label">文件大小</span>
              <span class="meta-value">{{ formattedSize }}</span>
            </div>
            <div v-if="videoMetadata.resolution" class="meta-row">
              <span class="meta-label">分辨率</span>
              <span class="meta-value">{{ videoMetadata.resolution }}</span>
            </div>
            <div v-if="videoMetadata.duration" class="meta-row">
              <span class="meta-label">时长</span>
              <span class="meta-value">{{ formatDuration(videoMetadata.duration) }}</span>
            </div>
            <div v-if="videoMetadata.encoding" class="meta-row">
              <span class="meta-label">编码</span>
              <span class="meta-value">{{ videoMetadata.encoding }}</span>
            </div>
            <div v-if="videoMetadata.container" class="meta-row">
              <span class="meta-label">容器</span>
              <span class="meta-value">{{ videoMetadata.container }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { VideoPlay, Loading, Collection } from '@element-plus/icons-vue'
import { formatBytes } from '../../utils/formatters'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const videoPlayer = ref(null)
const loading = ref(true)
const videoError = ref(false)

const objectData = computed(() => props.data?.object || {})
const content = computed(() => objectData.value?.content || {})
const metadata = computed(() => content.value?.metadata || {})

const videoMetadata = computed(() => {
  const width = metadata.value?.width
  const height = metadata.value?.height
  const durationMS = Number(metadata.value?.duration_ms)

  return {
    duration: Number.isFinite(durationMS) && durationMS > 0 ? durationMS / 1000 : undefined,
    resolution: width && height ? `${width}x${height}` : undefined,
    encoding: metadata.value?.encoding,
    container: metadata.value?.content_type || metadata.value?.format || objectData.value?.format
  }
})

const hasMetadata = computed(() => {
  const valueList = Object.values(videoMetadata.value || {})
  const hasVideoInfo = valueList.some((v) => v !== undefined && v !== null && v !== '')
  return hasVideoInfo || Boolean(formattedSize.value)
})

const sizeBytes = computed(() => {
  const objectSize = objectData.value?.size_bytes ?? objectData.value?.sizeBytes
  return objectSize ?? metadata.value?.size_bytes ?? null
})

const formattedSize = computed(() => {
  if (!sizeBytes.value) return ''
  return formatBytes(sizeBytes.value)
})

const videoSrc = computed(() => {
  if (videoError.value) return ''
  return String(content.value?.url || objectData.value?.url || '').trim()
})

const contentMessage = computed(() => {
  if (videoError.value) return '视频加载失败，请稍后重试'
  if (loading.value) return '正在加载视频...'
  return content.value?.text || '视频预览不可用'
})

// 格式化时长（秒转换为 HH:MM:SS）
const formatDuration = (seconds) => {
  if (seconds === undefined || seconds === null || seconds === '') return '未知'
  if (typeof seconds === 'string') {
    const normalized = seconds.trim()
    if (!normalized) return '未知'
    if (normalized.includes(':')) return normalized
    const numeric = Number(normalized)
    if (!Number.isFinite(numeric) || numeric <= 0) return '未知'
    seconds = numeric
  }

  const value = Number(seconds)
  if (!Number.isFinite(value) || value <= 0) return '未知'

  const hours = Math.floor(value / 3600)
  const minutes = Math.floor((value % 3600) / 60)
  const secs = Math.floor(value % 60)

  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
  }
  return `${minutes}:${String(secs).padStart(2, '0')}`
}

const onVideoLoaded = () => {
  loading.value = false
}

const onVideoError = () => {
  loading.value = false
  videoError.value = true
}

onMounted(() => {
  if (!videoSrc.value) {
    loading.value = false
  }
})
</script>

<style scoped>
.video-preview {
  display: flex;
  flex-direction: column;
  gap: 20px;
  padding: 20px;
  background: var(--el-bg-color);
}

.video-wrapper {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 20px;
  background: var(--el-fill-color);
  border: 1px dashed var(--el-border-color);
  border-radius: 6px;
}

video {
  width: 100%;
  max-width: 960px;
  max-height: 540px;
  border-radius: 4px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.1);
  background: #000;
}

.video-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 300px;
  padding: 40px;
  background: var(--el-fill-color);
  border: 1px dashed var(--el-border-color);
  border-radius: 6px;
  text-align: center;
}

.loading-icon {
  font-size: 32px;
  color: var(--el-color-primary);
  animation: rotate 1s linear infinite;
}

@keyframes rotate {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.message {
  margin: 12px 0;
  font-size: 14px;
  color: var(--el-text-color-regular);
}

.download-hint {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.metadata-section {
  padding: 20px;
  background: var(--el-bg-color-page);
  border-radius: 6px;
}

.extra-metadata {
  padding: 0 20px 20px;
  background: var(--el-bg-color);
  border-radius: 6px;
  border: 1px solid var(--el-border-color-light);
}

.metadata-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 24px;
}

.metadata-group {
  background: var(--el-fill-color);
  padding: 16px;
  border-radius: 6px;
  border: 1px solid var(--el-border-color-light);
}

.metadata-group h4 {
  margin: 0 0 12px;
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  display: flex;
  align-items: center;
  gap: 6px;
}

.meta-items {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.meta-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  font-size: 13px;
}

.meta-label {
  min-width: 80px;
  flex-shrink: 0;
  color: var(--el-text-color-secondary);
}

.meta-value {
  color: var(--el-text-color-primary);
  text-align: right;
}
</style>
