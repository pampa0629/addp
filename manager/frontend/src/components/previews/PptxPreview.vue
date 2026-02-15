<template>
  <div class="pptx-preview">
    <!-- 加载中 -->
    <div v-if="loading" class="loading-container">
      <el-icon class="is-loading"><Loading /></el-icon>
      <div class="loading-info">
        <span>正在解析文件信息...</span>
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
          <span class="download-hint">右上角下载按钮可获取原始文件</span>
        </div>
      </div>
    </div>

    <!-- PPTX 文件信息展示 -->
    <div v-else class="pptx-info-container">
      <!-- 文件基本信息 -->
      <div class="file-card">
        <div class="file-info">
          <div class="file-header">
            <el-icon :size="24" color="#d04726" class="file-type-icon">
              <Document />
            </el-icon>
            <h2 class="filename">{{ fileName }}</h2>
          </div>
          <div class="file-meta">
            <el-tag type="info" size="large">{{ formatFileSize(fileSize) }}</el-tag>
            <el-tag type="success" size="large" v-if="slideCount > 0">{{ slideCount }} 张幻灯片</el-tag>
            <el-tag type="warning" size="large">PowerPoint 演示文稿</el-tag>
          </div>
        </div>
      </div>

      <!-- 说明信息 -->
      <el-alert
        title="无法在线预览 PowerPoint 文件"
        type="warning"
        :closable="false"
        show-icon
      >
        <template #default>
          <div class="notice-content">
            <p><strong>为什么无法预览？</strong></p>
            <ul>
              <li>PowerPoint 文件包含复杂的布局、动画、样式和嵌入对象</li>
              <li>浏览器无法完整还原 PowerPoint 的显示效果</li>
              <li>在线预览可能导致内容丢失或显示错误</li>
            </ul>
            <p><strong>如何查看？</strong></p>
            <p>请使用右上角的下载按钮获取文件，并使用以下软件打开：</p>
            <ul>
              <li>Microsoft PowerPoint（推荐）</li>
              <li>WPS 演示</li>
              <li>LibreOffice Impress</li>
              <li>macOS Keynote</li>
            </ul>
          </div>
        </template>
      </el-alert>

      <!-- 文件详细信息 -->
      <div class="detail-card" v-if="pptxMetadata">
        <h3>文件详细信息</h3>
        <div class="detail-grid">
          <div class="detail-item">
            <span class="label">文件名称</span>
            <span class="value">{{ fileName }}</span>
          </div>
          <div class="detail-item">
            <span class="label">文件大小</span>
            <span class="value">{{ formatFileSize(fileSize) }}</span>
          </div>
          <div class="detail-item" v-if="slideCount > 0">
            <span class="label">幻灯片数量</span>
            <span class="value">{{ slideCount }} 张</span>
          </div>
          <div class="detail-item" v-if="pptxMetadata.creator">
            <span class="label">创建者</span>
            <span class="value">{{ pptxMetadata.creator }}</span>
          </div>
          <div class="detail-item" v-if="pptxMetadata.lastModifiedBy">
            <span class="label">最后修改者</span>
            <span class="value">{{ pptxMetadata.lastModifiedBy }}</span>
          </div>
          <div class="detail-item" v-if="pptxMetadata.created">
            <span class="label">创建时间</span>
            <span class="value">{{ formatDate(pptxMetadata.created) }}</span>
          </div>
          <div class="detail-item" v-if="pptxMetadata.modified">
            <span class="label">修改时间</span>
            <span class="value">{{ formatDate(pptxMetadata.modified) }}</span>
          </div>
          <div class="detail-item" v-if="pptxTitle">
            <span class="label">演示标题</span>
            <span class="value">{{ pptxTitle }}</span>
          </div>
        </div>
      </div>

      <!-- 下载引导 -->
      <div class="download-section">
        <span class="download-hint">如需下载 PowerPoint，请点击右上角的下载按钮</span>
        <p v-if="formattedLimit" class="limit-hint">预览限制：{{ formattedLimit }}</p>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { Loading, WarningFilled, Document } from '@element-plus/icons-vue'
import JSZip from 'jszip'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const loading = ref(false)
const error = ref('')
const slideCount = ref(0)
const pptxTitle = ref('')
const pptxMetadata = ref(null)

const fileName = computed(() => {
  const path = props.data.object?.path || ''
  return path.split('/').pop() || 'presentation.pptx'
})

const fileSize = computed(() => {
  return props.data.object?.size_bytes || 0
})

const pptxData = computed(() => {
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
    'application/vnd.openxmlformats-officedocument.presentationml.presentation'
  )
})

const showLimitInfo = computed(() => {
  return Boolean(limitBytes.value || displayContentType.value || fileSize.value)
})

const isTruncated = computed(() => {
  return props.data.object?.content?.truncated || props.data.object?.truncated || false
})

const truncatedMessage = computed(() => {
  return props.data.object?.content?.text || '文件太大，无法加载'
})

// 格式化文件大小
const formatFileSize = (bytes) => {
  if (!bytes) return '未知'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / 1024 / 1024).toFixed(2) + ' MB'
  return (bytes / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}

// 格式化日期
const formatDate = (dateStr) => {
  if (!dateStr) return '未知'
  try {
    const date = new Date(dateStr)
    return date.toLocaleString('zh-CN')
  } catch {
    return dateStr
  }
}

// 解析 PPTX 元数据（仅提取基本信息）
const parsePptxMetadata = async () => {
  try {
    loading.value = true
    error.value = ''

    if (isTruncated.value) {
      error.value = truncatedMessage.value
      return
    }

    if (!pptxData.value) {
      error.value = '未找到文件数据'
      return
    }

    console.log(`📊 开始解析 PPTX 元数据: ${fileName.value}`)

    // 将 base64 转换为 ArrayBuffer
    const base64Data = pptxData.value
    const binaryString = atob(base64Data)
    const bytes = new Uint8Array(binaryString.length)
    for (let i = 0; i < binaryString.length; i++) {
      bytes[i] = binaryString.charCodeAt(i)
    }

    // 使用 JSZip 解压 PPTX 文件
    const zip = await JSZip.loadAsync(bytes.buffer)

    // 读取幻灯片数量
    const slideFiles = Object.keys(zip.files).filter(name =>
      name.startsWith('ppt/slides/slide') && name.endsWith('.xml')
    )
    slideCount.value = slideFiles.length

    // 读取核心属性
    const corePropsFile = zip.file('docProps/core.xml')
    if (corePropsFile) {
      const corePropsXml = await corePropsFile.async('text')
      const metadata = {}

      // 提取各项元数据
      const creatorMatch = corePropsXml.match(/<dc:creator[^>]*>([^<]+)<\/dc:creator>/)
      if (creatorMatch) metadata.creator = creatorMatch[1]

      const lastModifiedMatch = corePropsXml.match(/<cp:lastModifiedBy[^>]*>([^<]+)<\/cp:lastModifiedBy>/)
      if (lastModifiedMatch) metadata.lastModifiedBy = lastModifiedMatch[1]

      const createdMatch = corePropsXml.match(/<dcterms:created[^>]*>([^<]+)<\/dcterms:created>/)
      if (createdMatch) metadata.created = createdMatch[1]

      const modifiedMatch = corePropsXml.match(/<dcterms:modified[^>]*>([^<]+)<\/dcterms:modified>/)
      if (modifiedMatch) metadata.modified = modifiedMatch[1]

      const titleMatch = corePropsXml.match(/<dc:title[^>]*>([^<]+)<\/dc:title>/)
      if (titleMatch) {
        metadata.title = titleMatch[1]
        pptxTitle.value = titleMatch[1]
      }

      pptxMetadata.value = metadata
    }

    console.log(`✅ PPTX 元数据解析完成: ${slideCount.value} 张幻灯片`)
  } catch (err) {
    console.error('❌ PPTX 元数据解析失败:', err)
    error.value = `解析失败: ${err.message}`
  } finally {
    loading.value = false
  }
}

// 初始化加载
const initLoad = () => {
  error.value = ''
  slideCount.value = 0
  pptxTitle.value = ''
  pptxMetadata.value = null
  parsePptxMetadata()
}

// 监听 props.data 变化
watch(() => props.data, (newData, oldData) => {
  const newPath = newData?.object?.path
  const oldPath = oldData?.object?.path

  if (newPath && newPath !== oldPath) {
    console.log(`🔄 PPTX 文件切换: ${oldPath} → ${newPath}`)
    initLoad()
  }
}, { deep: true })

onMounted(() => {
  initLoad()
})
</script>

<style scoped>
.pptx-preview {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-secondary);
  overflow: auto;
}

/* 加载状态 */
.loading-container {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 20px;
  color: var(--el-color-primary);
}

.loading-container .el-icon {
  font-size: 48px;
}

.loading-info {
  font-size: 16px;
}

/* 错误状态 */
.error-container {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 20px;
  color: var(--el-color-danger);
  padding: 40px;
}

.error-container .el-icon {
  font-size: 64px;
}

.error-info {
  text-align: center;
}

.error-message {
  font-size: 16px;
  margin-bottom: 20px;
  color: var(--addp-text-secondary);
}

.limit-info {
  font-size: 13px;
  color: var(--addp-text-tertiary);
  line-height: 1.6;
  margin-bottom: 16px;
}

.limit-info p {
  margin: 4px 0;
}

.error-actions {
  display: flex;
  gap: 10px;
  justify-content: center;
}

/* PPTX 信息容器 */
.pptx-info-container {
  max-width: 900px;
  margin: 0 auto;
  padding: 40px 20px;
  width: 100%;
}

/* 文件卡片 */
.file-card {
  padding: 30px;
  background: white;
  border-radius: 12px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
  margin-bottom: 30px;
}

.file-info {
  width: 100%;
}

.file-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.file-type-icon {
  flex-shrink: 0;
}

.filename {
  font-size: 20px;
  font-weight: 600;
  color: var(--addp-text-primary);
  margin: 0;
  word-break: break-all;
  flex: 1;
}

.file-meta {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

/* 说明信息 */
.notice-content {
  line-height: 1.8;
}

.notice-content p {
  margin: 12px 0;
}

.notice-content strong {
  font-size: 16px;
  color: var(--addp-text-primary);
}

.notice-content ul {
  margin: 10px 0;
  padding-left: 25px;
}

.notice-content li {
  margin: 6px 0;
  color: var(--addp-text-secondary);
}

/* 详细信息卡片 */
.detail-card {
  background: white;
  padding: 30px;
  border-radius: 12px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.08);
  margin: 30px 0;
}

.detail-card h3 {
  font-size: 18px;
  font-weight: 600;
  color: var(--addp-text-primary);
  margin: 0 0 20px 0;
  padding-bottom: 15px;
  border-bottom: 2px solid #f0f0f0;
}

.detail-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
  gap: 20px;
}

.detail-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.detail-item .label {
  font-size: 13px;
  font-weight: 600;
  color: var(--addp-text-tertiary);
  text-transform: uppercase;
}

.detail-item .value {
  font-size: 15px;
  color: var(--addp-text-primary);
  word-break: break-all;
}

/* 下载区域 */
.download-section {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 20px 0;
}

.download-hint {
  font-size: 13px;
  color: var(--addp-text-tertiary);
}

.limit-hint {
  font-size: 13px;
  color: var(--addp-text-tertiary);
}
</style>
