<template>
  <div class="docx-preview">
    <!-- 大文件警告提示 -->
    <div v-if="showLargeFileWarning && !loading && !error" class="large-file-warning">
      <el-alert
        title="大文件提示"
        type="warning"
        :closable="false"
        show-icon
      >
        <template #default>
          <p><strong>文档大小：{{ formatFileSize(fileSize) }}</strong></p>
          <p v-if="fileSize > 50 * 1024 * 1024">
            该文档超过 50MB，无法在线预览，请下载后使用本地应用查看。
          </p>
          <p v-else>
            该文档较大（{{formatFileSize(fileSize)}}），在线预览可能需要较长时间，建议下载后查看。
          </p>
          <p v-if="formattedLimit" class="limit-hint">
            当前预览限制：{{ formattedLimit }}
          </p>
          <div class="warning-actions">
            <el-button type="primary" size="small" :disabled="!docxData" @click="downloadDocx">
              <el-icon><Download /></el-icon>
              立即下载
            </el-button>
            <!-- 只有 30-50MB 的文件才提供"仍要预览"选项 -->
            <el-button v-if="fileSize <= 50 * 1024 * 1024" size="small" @click="forcePreview">
              仍要预览
            </el-button>
          </div>
        </template>
      </el-alert>
    </div>

    <!-- 加载中 -->
    <div v-if="loading" class="loading-container">
      <el-icon class="is-loading"><Loading /></el-icon>
      <div class="loading-info">
        <span>正在加载 DOCX 文档...</span>
        <div v-if="fileSize > 50 * 1024 * 1024" class="loading-hint">
          <p>文件较大（{{ formatFileSize(fileSize) }}），请耐心等待</p>
          <p class="loading-tips">提示：下载后使用本地应用查看会更快捷</p>
        </div>
      </div>
    </div>

    <!-- 错误提示 -->
    <div v-else-if="error" class="error-container">
      <el-icon><WarningFilled /></el-icon>
      <div class="error-info">
        <p class="error-message">{{ error }}</p>
        <div v-if="showLimitInfo" class="limit-info">
          <p>文件类型：{{ displayContentType }}</p>
          <p v-if="fileSize">文件大小：{{ formatFileSize(fileSize) }}</p>
          <p v-if="formattedLimit">预览限制：{{ formattedLimit }}</p>
        </div>
        <div class="error-actions">
          <el-button type="primary" size="small" :disabled="!docxData" @click="downloadDocx">
            <el-icon><Download /></el-icon>
            下载文档
          </el-button>
          <el-button size="small" @click="retryLoad">
            <el-icon><RefreshRight /></el-icon>
            重试
          </el-button>
        </div>
      </div>
    </div>

    <!-- 文档内容 -->
    <div v-else-if="!showLargeFileWarning" class="docx-container">
      <!-- 工具栏 -->
      <div class="docx-toolbar">
        <div class="toolbar-left">
          <el-icon><Document /></el-icon>
          <span class="docx-title">{{ fileName }}</span>
          <el-tag v-if="fileSize" size="small" type="info">
            {{ formatFileSize(fileSize) }}
          </el-tag>
        </div>
        <div class="toolbar-right">
          <el-button size="small" :disabled="!docxData" @click="downloadDocx">
            <el-icon><Download /></el-icon>
            下载
          </el-button>
        </div>
      </div>

      <!-- 文档内容 -->
      <div class="docx-content" v-html="htmlContent"></div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { Loading, WarningFilled, Document, Download, RefreshRight } from '@element-plus/icons-vue'
import mammoth from 'mammoth'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const loading = ref(false)
const error = ref('')
const htmlContent = ref('')
const showLargeFileWarning = ref(false)
let currentLoadToken = 0

const fileName = computed(() => {
  const path = props.data.object?.path || ''
  return path.split('/').pop() || 'document.docx'
})

const fileSize = computed(() => {
  return props.data.object?.size_bytes || 0
})

const docxData = computed(() => {
  const content = props.data.object?.content
  if (!content) return null
  return content.data || content.Data || null
})

const contentMetadata = computed(() => props.data.object?.content?.metadata || {})

const limitBytes = computed(() => contentMetadata.value?.limit_bytes ?? null)

const formattedLimit = computed(() => {
  if (!limitBytes.value) return ''
  return formatFileSize(limitBytes.value)
})

const displayContentType = computed(() => {
  return (
    contentMetadata.value?.content_type ||
    props.data.object?.content_type ||
    props.data.object?.contentType ||
    'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
  )
})

const showLimitInfo = computed(() => {
  return Boolean(limitBytes.value || displayContentType.value || fileSize.value)
})

const isTruncated = computed(() => {
  return props.data.object?.content?.truncated || props.data.object?.truncated || false
})

const truncatedMessage = computed(() => {
  return props.data.object?.content?.text || '文件太大，无法完整预览'
})

// 格式化文件大小
const formatFileSize = (bytes) => {
  if (!bytes) return '未知'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / 1024 / 1024).toFixed(2) + ' MB'
  return (bytes / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}

// 检查是否需要显示大文件警告
const checkLargeFile = () => {
  const size = fileSize.value
  // 30MB 以上显示警告（给用户选择）
  // 50MB 以上后端会拒绝
  if (size > 30 * 1024 * 1024) {
    showLargeFileWarning.value = true
    return true
  }
  return false
}

// 强制预览大文件
const forcePreview = () => {
  showLargeFileWarning.value = false
  loadDocx()
}

// 重试加载
const retryLoad = () => {
  error.value = ''
  loadDocx()
}

// 加载 DOCX 文档
const loadDocx = async () => {
  const token = ++currentLoadToken
  try {
    loading.value = true
    error.value = ''

    // 检查文件是否被截断
    if (isTruncated.value) {
      throw new Error(truncatedMessage.value)
    }

    if (!docxData.value) {
      throw new Error('未找到 DOCX 文档数据')
    }

    console.log(`📄 开始加载 DOCX: ${fileName.value} (${formatFileSize(fileSize.value)})`)

    // 将 base64 转换为 ArrayBuffer
    const base64Data = docxData.value
    const binaryString = atob(base64Data)
    const bytes = new Uint8Array(binaryString.length)
    for (let i = 0; i < binaryString.length; i++) {
      bytes[i] = binaryString.charCodeAt(i)
    }

    console.log('🔄 转换中...')

    // 使用 mammoth.js 转换 DOCX 为 HTML
    const result = await mammoth.convertToHtml(
      { arrayBuffer: bytes.buffer },
      {
        styleMap: [
          "p[style-name='Heading 1'] => h1:fresh",
          "p[style-name='Heading 2'] => h2:fresh",
          "p[style-name='Heading 3'] => h3:fresh",
          "p[style-name='Title'] => h1.title:fresh",
          "p[style-name='Subtitle'] => h2.subtitle:fresh",
          "p[style-name='Quote'] => blockquote:fresh",
          "r[style-name='Strong'] => strong",
          "r[style-name='Emphasis'] => em"
        ],
        convertImage: mammoth.images.imgElement((image) => {
          return image.read("base64").then((imageBuffer) => {
            return {
              src: `data:${image.contentType};base64,${imageBuffer}`
            }
          })
        })
      }
    )

    if (token !== currentLoadToken) {
      return
    }

    htmlContent.value = result.value || ''

    const messages = Array.isArray(result.messages) ? result.messages : []
    const errorMessages = messages.filter((msg) => msg.type === 'error')

    if (messages.length > 0) {
      console.warn('⚠️  DOCX 转换警告:', messages)
    }

    if (errorMessages.length > 0 && !result.value) {
      const firstError = errorMessages[0]?.message || '文档解析失败'
      throw new Error(firstError)
    }

    console.log('✅ DOCX 加载成功')
  } catch (err) {
    if (token !== currentLoadToken) {
      return
    }
    console.error('❌ DOCX 加载失败:', err)
    error.value = `加载失败: ${err.message}`
    htmlContent.value = ''
  } finally {
    if (token === currentLoadToken) {
      loading.value = false
    }
  }
}

// 下载 DOCX 文件
const downloadDocx = () => {
  try {
    if (!docxData.value) {
      throw new Error('未找到文档数据')
    }

    const base64Data = docxData.value
    const binaryString = atob(base64Data)
    const bytes = new Uint8Array(binaryString.length)
    for (let i = 0; i < binaryString.length; i++) {
      bytes[i] = binaryString.charCodeAt(i)
    }

    const blob = new Blob([bytes], {
      type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
    })

    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = fileName.value
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)

    console.log('✅ DOCX 下载完成')
  } catch (err) {
    console.error('❌ DOCX 下载失败:', err)
    error.value = `下载失败: ${err.message}`
  }
}

// 初始化加载
const initLoad = () => {
  currentLoadToken++
  // 重置状态
  error.value = ''
  htmlContent.value = ''
  showLargeFileWarning.value = false
  loading.value = false

  // 检查是否需要显示大文件警告
  if (!checkLargeFile()) {
    loadDocx()
  }
}

// 监听 props.data 变化，自动重新加载
watch(() => props.data, (newData, oldData) => {
  // 检查文件路径是否变化
  const newPath = newData?.object?.path
  const oldPath = oldData?.object?.path

  if (newPath && newPath !== oldPath) {
    console.log(`🔄 DOCX 文件切换: ${oldPath} → ${newPath}`)
    initLoad()
  }
}, { deep: true })

onMounted(() => {
  initLoad()
})
</script>

<style scoped>
.docx-preview {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: #f5f5f5;
}

.large-file-warning {
  padding: 24px;
  max-width: 600px;
  margin: 40px auto;
}

.large-file-warning :deep(.el-alert__content) {
  width: 100%;
}

.large-file-warning p {
  margin: 8px 0;
  line-height: 1.6;
}

.warning-actions {
  margin-top: 16px;
  display: flex;
  gap: 12px;
}

.limit-hint {
  margin-top: 8px;
  font-size: 13px;
  color: #909399;
}

.loading-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  gap: 16px;
  color: #666;
}

.loading-container .el-icon {
  font-size: 48px;
  color: #409eff;
}

.loading-info {
  text-align: center;
}

.loading-info > span {
  font-size: 16px;
  font-weight: 500;
}

.loading-hint {
  margin-top: 12px;
  padding: 12px;
  background: #f4f4f5;
  border-radius: 4px;
  max-width: 400px;
}

.loading-hint p {
  margin: 4px 0;
  font-size: 14px;
  color: #909399;
}

.loading-tips {
  color: #67c23a !important;
  font-size: 13px !important;
}

.error-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  gap: 16px;
  color: #666;
  padding: 24px;
}

.error-container .el-icon {
  font-size: 48px;
  color: #f56c6c;
}

.error-info {
  text-align: center;
  max-width: 500px;
}

.error-message {
  font-size: 16px;
  color: #606266;
  margin-bottom: 16px;
}

.limit-info {
  margin-bottom: 16px;
  font-size: 13px;
  color: #909399;
  line-height: 1.6;
}

.limit-info p {
  margin: 4px 0;
}

.error-actions {
  display: flex;
  gap: 12px;
  justify-content: center;
}

.docx-container {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.docx-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  background: white;
  border-bottom: 1px solid #e4e7ed;
  flex-shrink: 0;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.toolbar-left .el-icon {
  font-size: 20px;
  color: #409eff;
}

.docx-title {
  font-size: 14px;
  font-weight: 500;
  color: #303133;
  max-width: 400px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.toolbar-right {
  display: flex;
  gap: 8px;
}

.docx-content {
  flex: 1;
  overflow-y: auto;
  padding: 32px;
  background: white;
  margin: 16px;
  border-radius: 4px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.1);
}

/* DOCX 内容样式 - 保持原有样式 */
.docx-content :deep(h1) {
  font-size: 28px;
  font-weight: 600;
  margin: 24px 0 16px;
  color: #303133;
  line-height: 1.4;
}

.docx-content :deep(h2) {
  font-size: 24px;
  font-weight: 600;
  margin: 20px 0 12px;
  color: #303133;
  line-height: 1.4;
}

.docx-content :deep(h3) {
  font-size: 20px;
  font-weight: 600;
  margin: 16px 0 12px;
  color: #303133;
  line-height: 1.4;
}

.docx-content :deep(p) {
  margin: 8px 0;
  line-height: 1.8;
  color: #606266;
  font-size: 14px;
}

.docx-content :deep(blockquote) {
  margin: 16px 0;
  padding: 12px 16px;
  border-left: 4px solid #409eff;
  background: #f4f4f5;
  color: #606266;
  font-style: italic;
}

.docx-content :deep(strong) {
  font-weight: 600;
  color: #303133;
}

.docx-content :deep(em) {
  font-style: italic;
}

.docx-content :deep(ul),
.docx-content :deep(ol) {
  margin: 12px 0;
  padding-left: 24px;
}

.docx-content :deep(li) {
  margin: 4px 0;
  line-height: 1.8;
  color: #606266;
}

.docx-content :deep(table) {
  width: 100%;
  border-collapse: collapse;
  margin: 16px 0;
}

.docx-content :deep(th),
.docx-content :deep(td) {
  border: 1px solid #dcdfe6;
  padding: 8px 12px;
  text-align: left;
}

.docx-content :deep(th) {
  background: #f5f7fa;
  font-weight: 600;
  color: #303133;
}

.docx-content :deep(td) {
  color: #606266;
}

.docx-content :deep(img) {
  max-width: 100%;
  height: auto;
  margin: 16px 0;
  border-radius: 4px;
}

.docx-content :deep(a) {
  color: #409eff;
  text-decoration: none;
}

.docx-content :deep(a:hover) {
  text-decoration: underline;
}

.docx-content :deep(code) {
  background: #f5f7fa;
  padding: 2px 6px;
  border-radius: 3px;
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: #e6a23c;
}

.docx-content :deep(pre) {
  background: #f5f7fa;
  padding: 12px;
  border-radius: 4px;
  overflow-x: auto;
  margin: 12px 0;
}

.docx-content :deep(pre code) {
  background: none;
  padding: 0;
  color: #303133;
}
</style>
