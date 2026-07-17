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
            <el-button type="primary" size="small" :disabled="!canDownload" @click="downloadDocument">
              <el-icon><Download /></el-icon>
              立即下载
            </el-button>
            <el-button v-if="fileSize <= 50 * 1024 * 1024" size="small" @click="forcePreview">
              仍要预览
            </el-button>
            <span class="download-hint">如需获取原始文件，请使用右上角的下载按钮</span>
          </div>
        </template>
      </el-alert>
    </div>

    <!-- 加载中 -->
    <div v-if="loading" class="loading-container">
      <el-icon class="is-loading"><Loading /></el-icon>
      <div class="loading-info">
        <span>正在加载 {{ docLabel }} 文档...</span>
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
          <el-button type="primary" size="small" :disabled="!canDownload" @click="downloadDocument">
            <el-icon><Download /></el-icon>
            下载文档
          </el-button>
          <span class="download-hint">右上角下载按钮可保存原始文档</span>
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
          <el-button size="small" :disabled="!canDownload" @click="downloadDocument">
            <el-icon><Download /></el-icon>
            下载
          </el-button>
        </div>
      </div>

      <div v-if="infoMessage" class="docx-info">
        <el-alert
          type="info"
          :closable="false"
          show-icon
        >
          <template #default>
            <p>{{ infoMessage }}</p>
          </template>
        </el-alert>
      </div>

      <div v-if="unsupportedMessage" class="unsupported-preview">
        <el-icon><WarningFilled /></el-icon>
        <h3>{{ unsupportedMessage.title }}</h3>
        <p>{{ unsupportedMessage.description }}</p>
        <div class="unsupported-meta">
          <span>文件名：{{ fileName }}</span>
          <span v-if="displayContentType">文件类型：{{ displayContentType }}</span>
          <span v-if="fileSize">文件大小：{{ formatFileSize(fileSize) }}</span>
        </div>
        <el-button type="primary" size="small" :disabled="!canDownload" @click="downloadDocument">
          <el-icon><Download /></el-icon>
          下载后查看
        </el-button>
      </div>

      <!-- 文档内容 -->
      <div v-else class="docx-content" v-html="htmlContent"></div>
    </div>
  </div>
</template>

<script setup>
import { getAccessToken } from '../../auth/authSession'
import { ref, computed, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Loading, WarningFilled, Document, Download, RefreshRight } from '@element-plus/icons-vue'
import mammoth from 'mammoth/mammoth.browser'
import JSZip from 'jszip'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const loading = ref(false)
const error = ref('')
const htmlContent = ref('')
const infoMessage = ref('')
const unsupportedMessage = ref(null)
const showLargeFileWarning = ref(false)
const cachedDocumentBytes = ref(null)
let currentLoadToken = 0

const objectPath = computed(() => props.data.object?.path || '')

const fileExtension = computed(() => {
  const object = props.data.object || {}
  const explicit = (object.extension || object.Extension || '').toString().trim().toLowerCase()
  if (explicit) {
    return explicit.startsWith('.') ? explicit : `.${explicit}`
  }
  const path = (object.path || object.name || '').toString().toLowerCase()
  const dotIndex = path.lastIndexOf('.')
  if (dotIndex >= 0) {
    return path.slice(dotIndex)
  }
  return ''
})

const documentKind = computed(() => {
  const ext = fileExtension.value
  if (ext === '.wps') {
    return 'wps'
  }
  if (ext === '.docx') {
    return 'docx'
  }

  const object = props.data.object || {}
  const content = object.content || {}
  const metadata = content.metadata || {}
  const candidates = [
    content.kind,
    content.Kind,
    metadata.kind,
    metadata.Kind,
    object.kind,
    object.Kind
  ]

  for (const candidate of candidates) {
    if (typeof candidate === 'string' && candidate) {
      const lower = candidate.toLowerCase()
      if (lower === 'wps' || lower === 'docx') {
        return lower
      }
    }
  }

  const contentTypeCandidates = [
    content.content_type,
    content.contentType,
    object.content_type,
    object.contentType,
    metadata.content_type,
    metadata.contentType
  ]

  for (const candidate of contentTypeCandidates) {
    if (typeof candidate === 'string' && candidate) {
      const lower = candidate.toLowerCase()
      if (lower.includes('ms-works') || lower.includes('wps')) {
        return 'wps'
      }
      if (lower.includes('wordprocessingml') || lower.includes('officedocument.wordprocessingml.document')) {
        return 'docx'
      }
    }
  }

  return 'docx'
})

const isWpsDocument = computed(() => documentKind.value === 'wps')

const docLabel = computed(() => (isWpsDocument.value ? 'WPS' : 'DOCX'))

const fileName = computed(() => {
  const path = objectPath.value
  if (!path) {
    return isWpsDocument.value ? 'document.wps' : 'document.docx'
  }
  return path.split('/').pop() || (isWpsDocument.value ? 'document.wps' : 'document.docx')
})

const fileSize = computed(() => {
  return props.data.object?.size_bytes || 0
})

const documentData = computed(() => {
  const content = props.data.object?.content
  if (!content) return null
  return content.data || content.Data || null
})

const engineId = computed(() => {
  const root = props.data || {}
  const object = root.object || {}
  return (
    root.engineId ||
    root.engine_id ||
    object.engine_id ||
    object.engineId ||
    null
  )
})

const downloadUrl = computed(() => {
  const root = props.data || {}
  const object = root.object || {}
  const content = object.content || {}

  if (root.download_url) return root.download_url
  if (root.downloadUrl) return root.downloadUrl
  if (content.download_url) return content.download_url
  if (content.downloadUrl) return content.downloadUrl
  if (object.download_url) return object.download_url
  if (object.downloadUrl) return object.downloadUrl
  if (root.preview_url) return root.preview_url
  if (root.previewUrl) return root.previewUrl
  if (content.preview_url) return content.preview_url
  if (content.previewUrl) return content.previewUrl
  if (object.preview_url) return object.preview_url
  if (object.previewUrl) return object.previewUrl
  if (content.url) return content.url
  if (object.url) return object.url
  if (content.signed_url) return content.signed_url
  if (content.signedUrl) return content.signedUrl
  if (object.signed_url) return object.signed_url
  if (object.signedUrl) return object.signedUrl

  if (object.path && engineId.value) {
    return `/api/preview/download?engine_id=${engineId.value}&path=${encodeURIComponent(object.path)}`
  }

  return ''
})

const contentMetadata = computed(() => props.data.object?.content?.metadata || {})

const canDownload = computed(() =>
  Boolean(documentData.value || downloadUrl.value)
)

const limitBytes = computed(() => contentMetadata.value?.limit_bytes ?? null)

const formattedLimit = computed(() => {
  if (!limitBytes.value) return ''
  return formatFileSize(limitBytes.value)
})

const rawContentType = computed(() => {
  const object = props.data.object || {}
  const content = object.content || {}
  const metadata = content.metadata || {}
  return (
    content.content_type ||
    content.contentType ||
    object.content_type ||
    object.contentType ||
    metadata.content_type ||
    metadata.contentType ||
    ''
  )
})

const displayContentType = computed(() => {
  if (rawContentType.value) {
    return rawContentType.value
  }
  return isWpsDocument.value
    ? 'application/vnd.ms-works'
    : 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
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

const sanitizeBase64 = (value) => {
  if (!value) return ''
  if (typeof value !== 'string') {
    return String(value)
  }
  return value.replace(/\s+/g, '')
}

const decodeBase64ToBytes = (base64) => {
  const clean = sanitizeBase64(base64)
  if (!clean) {
    throw new Error(`${docLabel.value} 数据为空`)
  }

  try {
    const binaryString = atob(clean)
    const bytes = new Uint8Array(binaryString.length)
    for (let i = 0; i < binaryString.length; i++) {
      bytes[i] = binaryString.charCodeAt(i)
    }
    return bytes
  } catch (err) {
    console.error(`${docLabel.value} base64 解码失败`, err)
    throw new Error(`${docLabel.value} 数据解码失败`)
  }
}

const fetchDocumentBytesFromUrl = async (url) => {
  try {
    const headers = {}
    const token = getAccessToken()
    if (token) {
      headers.Authorization = `Bearer ${token}`
    }
    const response = await fetch(url, { credentials: 'include', headers })
    if (!response.ok) {
      throw new Error(`请求失败（${response.status}）`)
    }
    const buffer = await response.arrayBuffer()
    return new Uint8Array(buffer)
  } catch (err) {
    console.error(`${docLabel.value} 下载失败`, err)
    throw new Error(err.message || `${docLabel.value} 下载失败`)
  }
}

const getDocumentBytes = async () => {
  if (cachedDocumentBytes.value) {
    return cachedDocumentBytes.value
  }

  if (documentData.value) {
    const bytes = decodeBase64ToBytes(documentData.value)
    cachedDocumentBytes.value = bytes
    return bytes
  }

  if (downloadUrl.value) {
    const bytes = await fetchDocumentBytesFromUrl(downloadUrl.value)
    cachedDocumentBytes.value = bytes
    return bytes
  }

  throw new Error(`未提供可用的 ${docLabel.value} 数据源`)
}

const toArrayBuffer = (bytes) => {
  if (!bytes) return null
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength)
}

const convertDocxArrayBufferToHtml = async (arrayBuffer) => {
  return mammoth.convertToHtml(
    { arrayBuffer },
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
        return image.read('base64').then((imageBuffer) => {
          return {
            src: `data:${image.contentType};base64,${imageBuffer}`
          }
        })
      })
    }
  )
}

const countMatches = (source, pattern) => {
  if (!source) return 0
  const matches = source.match(pattern)
  return matches ? matches.length : 0
}

const normalizeWhitespace = (value) => {
  if (!value) return ''
  return value
    .replace(/\r\n/g, '\n')
    .replace(/\r/g, '\n')
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .join('\n')
}

const extractParagraphsFromXml = (xml) => {
  try {
    const parser = new DOMParser()
    const doc = parser.parseFromString(xml, 'application/xml')
    const parserErrors = doc.getElementsByTagName('parsererror')
    if (parserErrors && parserErrors.length > 0) {
      return []
    }

    const selectors = [
      'w\\:p',
      'wx\\:p',
      'wps\\:p',
      'text\\:p',
      'p',
      'para',
      'paragraph',
      'office\\:text'
    ]
    const paragraphs = []
    const seen = new Set()

    for (const selector of selectors) {
      let nodes = []
      try {
        nodes = Array.from(doc.querySelectorAll(selector))
      } catch (err) {
        continue
      }
      nodes.forEach((node) => {
        const text = normalizeWhitespace(node.textContent || '')
        if (!text) return
        if (!seen.has(text)) {
          seen.add(text)
          paragraphs.push(text)
        }
      })
    }

    if (!paragraphs.length) {
      const fallback = normalizeWhitespace(doc.documentElement?.textContent || '')
      if (fallback) {
        paragraphs.push(fallback)
      }
    }

    return paragraphs
  } catch (err) {
    return []
  }
}

const stripXmlTags = (xml) => {
  if (!xml) return ''
  return normalizeWhitespace(
    xml
      .replace(/<!\[CDATA\[(.*?)]]>/gis, '$1')
      .replace(/<[^>]+>/g, '\n')
      .replace(/&nbsp;/g, ' ')
      .replace(/&lt;/g, '<')
      .replace(/&gt;/g, '>')
      .replace(/&amp;/g, '&')
      .replace(/&quot;/g, '"')
      .replace(/&#39;/g, "'")
  )
}

const escapeHtml = (value) => {
  if (!value) return ''
  const map = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;'
  }
  return value.replace(/[&<>"']/g, (char) => map[char] || char)
}

const isZipArchive = (bytes) => {
  if (!bytes || bytes.length < 4) return false
  return bytes[0] === 0x50 && bytes[1] === 0x4b && (bytes[2] === 0x03 || bytes[2] === 0x05) && (bytes[3] === 0x04 || bytes[3] === 0x06)
}

const convertLegacyWpsToHtml = () => {
  throw new Error('该 WPS 文件不是可在线解析的新式文档结构。请下载后使用 WPS 打开，或另存为 DOCX / 新版 WPS 后再预览。')
}

const convertWpsZipToHtml = async (bytes) => {
  const zip = await JSZip.loadAsync(bytes)
  const xmlEntries = []

  zip.forEach((relativePath, entry) => {
    if (entry.dir) return
    if (relativePath.toLowerCase().endsWith('.xml')) {
      xmlEntries.push({ path: relativePath, entry })
    }
  })

  if (!xmlEntries.length) {
    throw new Error('未找到可解析的 XML 主体')
  }

  const contents = []
  for (const item of xmlEntries) {
    const content = await item.entry.async('string')
    const score =
      countMatches(content, /<w:t[\s>]/gi) * 4 +
      countMatches(content, /<text:p[\s>]/gi) * 3 +
      countMatches(content, /<w:p[\s>]/gi) * 2 +
      countMatches(content, /<p[\s>]/gi) +
      stripXmlTags(content).length / 800
    contents.push({
      path: item.path,
      content,
      score
    })
  }

  contents.sort((a, b) => b.score - a.score)
  const primary = contents[0]

  if (!primary || primary.score <= 0) {
    throw new Error('未检测到正文内容')
  }

  const paragraphs = extractParagraphsFromXml(primary.content)
  const distinct = paragraphs.length
    ? paragraphs
    : [stripXmlTags(primary.content)].filter((text) => text.length > 0)

  if (!distinct.length) {
    throw new Error('无法解析 WPS 主体内容')
  }

  const html = distinct
    .map((text) => {
      const safe = escapeHtml(text).replace(/\n+/g, '<br />')
      return `<p>${safe}</p>`
    })
    .join('')

  return {
    html,
    infoMessage: '已使用 WPS 兼容解析，排版可能与原文略有差异。'
  }
}

const convertWpsBytesToHtml = async (bytes) => {
  if (!isZipArchive(bytes)) {
    return convertLegacyWpsToHtml(bytes)
  }

  try {
    return await convertWpsZipToHtml(bytes)
  } catch (err) {
    console.warn('WPS ZIP 解析失败，尝试旧版兼容模式。', err)
    return convertLegacyWpsToHtml(bytes)
  }
}

const unsupportedWpsResult = () => ({
  html: '',
  messages: [],
  usedFallback: true,
  fallbackMessage: '',
  unsupported: {
    title: '暂不支持在线预览该 WPS 文档',
    description: '当前浏览器预览组件暂不支持 WPS 专有文档格式。请下载后使用 WPS 打开；如需在线预览，可在 WPS 中另存为 DOCX 后重新上传。'
  }
})

const convertDocumentBytesToHtml = async (bytes, arrayBuffer) => {
  if (!isWpsDocument.value) {
    const result = await convertDocxArrayBufferToHtml(arrayBuffer)
    return {
      html: result.value || '',
      messages: result.messages || [],
      usedFallback: false,
      fallbackMessage: ''
    }
  }

  if (!isZipArchive(bytes)) {
    return unsupportedWpsResult()
  }

  try {
    const { html, infoMessage: fallbackMessage } = await convertWpsBytesToHtml(bytes)
    return {
      html,
      messages: [],
      usedFallback: true,
      fallbackMessage: fallbackMessage || '已使用 WPS 兼容解析，排版可能与原文略有差异。'
    }
  } catch (err) {
    console.warn('WPS ZIP 兼容解析失败。', err)
    return unsupportedWpsResult()
  }
}

const downloadDocument = async () => {
  if (!canDownload.value) {
    ElMessage.warning(`${docLabel.value} 暂无法下载`)
    return
  }

  try {
    const bytes = await getDocumentBytes()
    const arrayBuffer = toArrayBuffer(bytes)
    if (!arrayBuffer) {
      throw new Error(`${docLabel.value} 数据解析失败`)
    }

    const mimeType =
      displayContentType.value ||
      (isWpsDocument.value
        ? 'application/vnd.ms-works'
        : 'application/vnd.openxmlformats-officedocument.wordprocessingml.document')

    const blob = new Blob([arrayBuffer], { type: mimeType })
    const url = URL.createObjectURL(blob)

    const fallbackExt = isWpsDocument.value ? '.wps' : '.docx'
    const currentExt = fileExtension.value || fallbackExt
    const normalizedExt = currentExt.startsWith('.') ? currentExt : `.${currentExt}`
    let downloadName = fileName.value || `document${normalizedExt}`
    if (!downloadName.toLowerCase().endsWith(normalizedExt.toLowerCase())) {
      downloadName = `${downloadName}${normalizedExt}`
    }

    const link = document.createElement('a')
    link.href = url
    link.download = downloadName
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(url)

    console.log(`✅ ${docLabel.value} 下载完成`)
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err)
    console.error(`❌ ${docLabel.value} 下载失败:`, err)
    error.value = `下载失败: ${message}`
    ElMessage.error(`${docLabel.value} 下载失败: ${message}`)
  }
}

// 强制预览大文件
const forcePreview = () => {
  showLargeFileWarning.value = false
  cachedDocumentBytes.value = null
  loadDocument()
}

// 重试加载
const retryLoad = () => {
  error.value = ''
  cachedDocumentBytes.value = null
  loadDocument()
}

// 加载文档
const loadDocument = async () => {
  const token = ++currentLoadToken
  try {
    loading.value = true
    error.value = ''
    infoMessage.value = ''

    // 检查文件是否被截断
    if (isTruncated.value) {
      throw new Error(truncatedMessage.value)
    }

    if (!documentData.value && !downloadUrl.value) {
      throw new Error(`未找到 ${docLabel.value} 文档数据`)
    }

    console.log(`📄 开始加载 ${docLabel.value}: ${fileName.value} (${formatFileSize(fileSize.value)})`)

    if (!documentData.value && downloadUrl.value) {
      console.log(`🌐 通过下载链接加载 ${docLabel.value} 预览`, downloadUrl.value)
    }

    const bytes = await getDocumentBytes()

    if (token !== currentLoadToken) {
      return
    }

    if (!bytes || !bytes.byteLength) {
      throw new Error(`${docLabel.value} 数据为空`)
    }

    const arrayBuffer = toArrayBuffer(bytes)
    if (!arrayBuffer) {
      throw new Error(`${docLabel.value} 数据解析失败`)
    }

    console.log('🔄 转换中...')

    const { html, messages, usedFallback, fallbackMessage, unsupported } = await convertDocumentBytesToHtml(bytes, arrayBuffer)

    if (token !== currentLoadToken) {
      return
    }

    htmlContent.value = html || ''
    unsupportedMessage.value = unsupported || null

    const normalizedMessages = Array.isArray(messages) ? messages : []
    const errorMessages = normalizedMessages.filter((msg) => msg.type === 'error')

    if (normalizedMessages.length > 0) {
      console.warn(`⚠️  ${docLabel.value} 转换警告:`, normalizedMessages)
    }

    if (errorMessages.length > 0 && !html) {
      const firstError = errorMessages[0]?.message || '文档解析失败'
      throw new Error(firstError)
    }

    if (usedFallback && !unsupportedMessage.value) {
      infoMessage.value = fallbackMessage || '已使用 WPS 兼容解析，排版可能与原文略有差异。'
    }

    console.log(unsupportedMessage.value ? `ℹ️ ${docLabel.value} 不支持在线解析` : `✅ ${docLabel.value} 加载成功`)
  } catch (err) {
    if (token !== currentLoadToken) {
      return
    }
    console.error(`❌ ${docLabel.value} 加载失败:`, err)
    error.value = `加载失败: ${err.message}`
    htmlContent.value = ''
    infoMessage.value = ''
    unsupportedMessage.value = null
  } finally {
    if (token === currentLoadToken) {
      loading.value = false
    }
  }
}

// 初始化加载
const initLoad = () => {
  currentLoadToken++
  // 重置状态
  error.value = ''
  htmlContent.value = ''
  infoMessage.value = ''
  unsupportedMessage.value = null
  showLargeFileWarning.value = false
  loading.value = false
  cachedDocumentBytes.value = null

  // 检查是否需要显示大文件警告
  if (!checkLargeFile()) {
    loadDocument()
  }
}

// 监听 props.data 变化，自动重新加载
watch(
  () => props.data,
  (newData, oldData) => {
    if (!newData) return

    const newPath = newData?.object?.path
    const oldPath = oldData?.object?.path

    if (newPath && newPath !== oldPath) {
      console.log(`🔄 ${docLabel.value} 文件切换: ${oldPath} → ${newPath}`)
      initLoad()
      return
    }

    const newContent = newData?.object?.content || {}
    const oldContent = oldData?.object?.content || {}

    const contentChanged =
      newContent.data !== oldContent.data ||
      newContent.Data !== oldContent.Data ||
      newContent.download_url !== oldContent.download_url ||
      newContent.downloadUrl !== oldContent.downloadUrl ||
      newData?.object?.download_url !== oldData?.object?.download_url ||
      newData?.object?.downloadUrl !== oldData?.object?.downloadUrl ||
      newData?.download_url !== oldData?.download_url ||
      newData?.downloadUrl !== oldData?.downloadUrl ||
      newContent.preview_url !== oldContent.preview_url ||
      newContent.previewUrl !== oldContent.previewUrl ||
      newContent.text !== oldContent.text ||
      newContent.truncated !== oldContent.truncated

    if (contentChanged) {
      console.log(`🔄 ${docLabel.value} 内容更新，重新加载预览`)
      initLoad()
    }
  },
  { deep: true }
)

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
  background: var(--addp-bg-secondary);
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
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.limit-hint {
  margin-top: 8px;
  font-size: 13px;
  color: var(--addp-text-tertiary);
}

.loading-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  gap: 16px;
  color: var(--addp-text-secondary);
}

.loading-container .el-icon {
  font-size: 48px;
  color: var(--el-color-primary);
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
  background: var(--addp-bg-secondary);
  border-radius: 4px;
  max-width: 400px;
}

.loading-hint p {
  margin: 4px 0;
  font-size: 14px;
  color: var(--addp-text-tertiary);
}

.loading-tips {
  color: var(--el-color-success) !important;
  font-size: 13px !important;
}

.error-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  gap: 16px;
  color: var(--addp-text-secondary);
  padding: 24px;
}

.error-container .el-icon {
  font-size: 48px;
  color: var(--el-color-danger);
}

.error-info {
  text-align: center;
  max-width: 500px;
}

.error-message {
  font-size: 16px;
  color: var(--addp-text-secondary);
  margin-bottom: 16px;
}

.limit-info {
  margin-bottom: 16px;
  font-size: 13px;
  color: var(--addp-text-tertiary);
  line-height: 1.6;
}

.limit-info p {
  margin: 4px 0;
}

.error-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  justify-content: center;
}

.download-hint {
  font-size: 12px;
  color: var(--addp-text-tertiary);
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
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
  flex-shrink: 0;
}

.docx-info {
  padding: 0 16px;
  margin: 0 16px 8px;
}

.docx-info p {
  margin: 0;
  line-height: 1.6;
  font-size: 13px;
  color: var(--addp-text-secondary);
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.toolbar-left .el-icon {
  font-size: 20px;
  color: var(--el-color-primary);
}

.docx-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--addp-text-primary);
  max-width: 400px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.docx-content {
  flex: 1;
  overflow-y: auto;
  padding: 32px;
  background: var(--addp-bg-primary);
  margin: 16px;
  border-radius: 4px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.1);
}

.wps-plain-text {
  font-family: 'Courier New', Courier, monospace;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 14px;
  line-height: 1.6;
  color: var(--addp-text-primary);
  background: var(--addp-bg-secondary);
  padding: 16px;
  border-radius: 4px;
}

.unsupported-preview {
  flex: 1;
  margin: 16px;
  padding: 48px 32px;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  text-align: center;
}

.unsupported-preview .el-icon {
  font-size: 48px;
  color: var(--el-color-warning);
}

.unsupported-preview h3 {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
}

.unsupported-preview p {
  margin: 0;
  max-width: 560px;
  line-height: 1.7;
  color: var(--addp-text-secondary);
}

.unsupported-meta {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 8px 16px;
  font-size: 13px;
  color: var(--addp-text-tertiary);
}

/* 文档内容样式 - 保持原有样式 */
.docx-content :deep(h1) {
  font-size: 28px;
  font-weight: 600;
  margin: 24px 0 16px;
  color: var(--addp-text-primary);
  line-height: 1.4;
}

.docx-content :deep(h2) {
  font-size: 24px;
  font-weight: 600;
  margin: 20px 0 12px;
  color: var(--addp-text-primary);
  line-height: 1.4;
}

.docx-content :deep(h3) {
  font-size: 20px;
  font-weight: 600;
  margin: 16px 0 12px;
  color: var(--addp-text-primary);
  line-height: 1.4;
}

.docx-content :deep(p) {
  margin: 8px 0;
  line-height: 1.8;
  color: var(--addp-text-secondary);
  font-size: 14px;
}

.docx-content :deep(blockquote) {
  margin: 16px 0;
  padding: 12px 16px;
  border-left: 4px solid var(--el-color-primary);
  background: var(--addp-bg-secondary);
  color: var(--addp-text-secondary);
  font-style: italic;
}

.docx-content :deep(strong) {
  font-weight: 600;
  color: var(--addp-text-primary);
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
  color: var(--addp-text-secondary);
}

.docx-content :deep(table) {
  width: 100%;
  border-collapse: collapse;
  margin: 16px 0;
}

.docx-content :deep(th),
.docx-content :deep(td) {
  border: 1px solid var(--addp-border-color);
  padding: 8px 12px;
  text-align: left;
}

.docx-content :deep(th) {
  background: var(--addp-bg-secondary);
  font-weight: 600;
  color: var(--addp-text-primary);
}

.docx-content :deep(td) {
  color: var(--addp-text-secondary);
}

.docx-content :deep(img) {
  max-width: 100%;
  height: auto;
  margin: 16px 0;
  border-radius: 4px;
}

.docx-content :deep(a) {
  color: var(--el-color-primary);
  text-decoration: none;
}

.docx-content :deep(a:hover) {
  text-decoration: underline;
}

.docx-content :deep(code) {
  background: var(--addp-bg-secondary);
  padding: 2px 6px;
  border-radius: 3px;
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: var(--el-color-warning);
}

.docx-content :deep(pre) {
  background: var(--addp-bg-secondary);
  padding: 12px;
  border-radius: 4px;
  overflow-x: auto;
  margin: 12px 0;
}

.docx-content :deep(pre code) {
  background: none;
  padding: 0;
  color: var(--addp-text-primary);
}
</style>
