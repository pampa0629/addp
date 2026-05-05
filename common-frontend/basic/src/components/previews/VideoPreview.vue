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

    <div v-if="hasAdditionalMetadata" class="extra-metadata">
      <el-divider content-position="left">
        <el-icon><Collection /></el-icon>
        <span style="margin-left: 8px;">提取元数据</span>
      </el-divider>
      <ExtractedMetadata :metadata="metadataForDetails" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { VideoPlay, Loading, Document, Collection } from '@element-plus/icons-vue'
import { formatBytes } from '../../utils/formatters'
import ExtractedMetadata from '../ExtractedMetadata.vue'

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

const parseMaybeJSON = (value) => {
  if (typeof value === 'string') {
    try {
      return JSON.parse(value)
    } catch (error) {
      console.warn('视频预览: JSON 解析失败', error)
      return null
    }
  }
  return value
}

const normalizedAttributes = computed(() => {
  const attrs = objectData.value?.attributes
  const parsed = parseMaybeJSON(attrs)
  if (parsed && typeof parsed === 'object') {
    return parsed
  }
  return attrs && typeof attrs === 'object' ? attrs : {}
})

const extractedMetadataRoot = computed(() => {
  const attrs = normalizedAttributes.value
  const fromAttrs = parseMaybeJSON(attrs?.extensions?.extraction?.extracted_metadata)
  if (fromAttrs && typeof fromAttrs === 'object') {
    return fromAttrs
  }
  if (attrs?.extensions?.extraction?.extracted_metadata && typeof attrs.extensions.extraction.extracted_metadata === 'object') {
    return attrs.extensions.extraction.extracted_metadata
  }
  return null
})

const rawVideoMetadata = computed(() => {
  const fromRoot = extractedMetadataRoot.value?.custom_attrs?.video_metadata
  if (fromRoot && typeof fromRoot === 'object') {
    if (fromRoot.data && typeof fromRoot.data === 'object') {
      return fromRoot.data
    }
    return fromRoot
  }
  return {}
})

const pickValue = (source, keys) => {
  if (!source) return undefined
  for (const key of keys) {
    if (source[key] !== undefined && source[key] !== null && source[key] !== '') {
      return source[key]
    }
  }
  return undefined
}

const videoMetadata = computed(() => {
  const raw = rawVideoMetadata.value || {}
  const width = pickValue(raw, ['width', 'video_width'])
  const height = pickValue(raw, ['height', 'video_height'])
  const resolution =
    pickValue(raw, ['resolution', 'video_resolution']) ||
    (width && height ? `${width}x${height}` : undefined)

  return {
    duration: pickValue(raw, ['duration', 'duration_seconds', 'total_duration']),
    resolution,
    codec: pickValue(raw, ['codec', 'video_codec']),
    bitrate: pickValue(raw, ['bitrate', 'bit_rate']),
    frame_rate: pickValue(raw, ['frame_rate', 'fps']),
    audio_codec: pickValue(raw, ['audio_codec', 'audio_format']),
    audio_channels: pickValue(raw, ['audio_channels', 'channels']),
    has_subtitles: pickValue(raw, ['has_subtitles', 'subtitle']),
    container: pickValue(raw, ['container', 'format', 'file_format'])
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

const engineId = computed(() => {
  return (
    objectData.value?.engine_id ??
    objectData.value?.engineId ??
    props.data?.engineId ??
    props.data?.engine_id ??
    null
  )
})

const objectKey = computed(() => {
  if (objectData.value?.object_key) {
    return objectData.value.object_key
  }
  const bucket = objectData.value?.bucket
  const path = objectData.value?.path || ''
  if (!bucket || !path) return ''
  const cleanedPath = String(path).replace(/^\/+/, '')
  if (!cleanedPath) return bucket
  return `${bucket}/${cleanedPath}`
})

// 构建视频URL
const videoSrc = computed(() => {
  if (videoError.value) return ''
  if (!engineId.value || !objectKey.value) return ''

  const params = new URLSearchParams()
  params.set('engine_id', String(engineId.value))
  params.set('object_key', objectKey.value)

  const token = localStorage.getItem('token')
  if (token) {
    params.set('token', token)
  }
  return `/api/v1/manager/video-stream?${params.toString()}`
})

const metadataForDetails = computed(() => {
  const root = extractedMetadataRoot.value
  if (!root) return null
  try {
    const cloned = JSON.parse(JSON.stringify(root))
    if (cloned.custom_attrs && cloned.custom_attrs.video_metadata) {
      delete cloned.custom_attrs.video_metadata
      if (!Object.keys(cloned.custom_attrs).length) {
        delete cloned.custom_attrs
      }
    }
    return Object.keys(cloned).length ? cloned : null
  } catch (error) {
    console.warn('提取元数据克隆失败', error)
    return null
  }
})

const hasAdditionalMetadata = computed(() => {
  if (!metadataForDetails.value) return false
  return Object.keys(metadataForDetails.value || {}).length > 0
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

// 格式化比特率
const formatBitrate = (bitrate) => {
  if (bitrate === undefined || bitrate === null || bitrate === '') return '未知'
  const value = Number(bitrate)
  if (!Number.isFinite(value) || value <= 0) return '未知'
  if (value >= 1000000) {
    return `${(value / 1000000).toFixed(2)} Mbps`
  }
  return `${(value / 1000).toFixed(0)} Kbps`
}

// 格式化声道数
const formatChannels = (channels) => {
  const channelNames = {
    1: '单声道',
    2: '立体声',
    6: '5.1 环绕声',
    8: '7.1 环绕声'
  }
  if (channels === undefined || channels === null || channels === '') {
    return '未知'
  }
  const value = Number(channels)
  if (Number.isFinite(value)) {
    return channelNames[value] || `${value} 声道`
  }
  return String(channels)
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
