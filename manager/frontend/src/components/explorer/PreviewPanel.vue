<template>
  <el-card shadow="never" class="preview-panel">
    <template #header>
      <div class="panel-header">
        <div class="header-left">
          <span class="header-title">{{ title }}</span>
          <!-- 表格空间信息 -->
          <div v-if="showTableInfo" class="table-info">
            <!-- 空间标签（带 tooltip）-->
            <el-tooltip
              v-if="hasGeometry"
              :content="spatialInfoTooltip"
              placement="bottom"
              :show-after="300"
              raw-content
            >
              <el-tag type="danger" size="small" class="info-badge">
                <el-icon><Location /></el-icon>
                空间
              </el-tag>
            </el-tooltip>
            <!-- 普通表标签 -->
            <el-tag v-else size="small" class="info-badge">
              <el-icon><Collection /></el-icon>
              数据表
            </el-tag>
            <!-- 总行数 -->
            <el-tag v-if="tableTotal > 0" size="small" type="info">
              总计 {{ tableTotal.toLocaleString() }} 行
            </el-tag>
          </div>
        </div>
        <div class="panel-actions">
          <!-- 向量化按钮 -->
          <el-button
            v-if="showVectorizeButton"
            size="small"
            type="success"
            @click="handleVectorize"
          >
            <el-icon><MagicStick /></el-icon>
            {{ vectorizeButtonText }}
          </el-button>

          <!-- 下载按钮 -->
          <el-tooltip
            v-if="showDownloadControl && downloadDisabled && downloadTip"
            :content="downloadTip"
            placement="bottom"
          >
            <span>
              <el-button
                size="small"
                type="primary"
                :loading="downloading"
                :disabled="true"
              >
                <el-icon><Download /></el-icon>
                下载本页
              </el-button>
            </span>
          </el-tooltip>
          <el-button
            v-else-if="showDownloadControl"
            size="small"
            type="primary"
            :loading="downloading"
            @click="handleDownload"
          >
            <el-icon><Download /></el-icon>
            下载本页
          </el-button>
        </div>
      </div>
    </template>

    <!-- 无选择节点 -->
    <div v-if="!selectedNode" class="empty-state">
      <el-empty description="从左侧选择数据查看预览" />
    </div>

    <!-- 无预览数据 -->
    <div v-else-if="!previewData" class="empty-state">
      <el-empty :description="emptyDescription" />
    </div>

    <!-- 无可用预览组件 -->
    <div v-else-if="!hasPreviewComponent" class="empty-state">
      <el-empty description="暂不支持该文件类型的预览">
        <template #description>
          <p>不支持 {{ fileExtension || '该类型' }} 文件的在线预览</p>
          <p style="font-size: 12px; color: #909399; margin-top: 8px;">
            支持的格式：PDF、DOCX、WPS、PPTX、图片、JSON、GeoJSON、CSV、SQLite、文本
          </p>
        </template>
      </el-empty>
    </div>

    <!-- 渲染预览组件 -->
    <div v-else class="preview-content">
      <component
        v-if="previewComponent"
        :is="previewComponent"
        :key="componentKey"
        :data="previewData"
        :loading="loading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
      />
    </div>
  </el-card>
</template>

<script setup>
import { computed, ref, watch, onUnmounted } from 'vue'
import { ElMessage, ElNotification } from 'element-plus'
import { MagicStick, Download, Location, Collection } from '@element-plus/icons-vue'
import { getPreviewComponent } from '@/plugins/previews'
import { parseLocator } from '@addp/common-frontend'
import client from '@/api/client'

const props = defineProps({
  selectedNode: {
    type: Object,
    default: null
  },
  previewData: {
    type: Object,
    default: null
  },
  loading: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['page-change', 'navigate'])

const sanitizeBase64 = (value) => {
  if (typeof value !== 'string') return ''
  return value.replace(/\s+/g, '')
}

const pickUrl = (target) => {
  if (!target || typeof target !== 'object') return ''
  const keys = [
    'download_url',
    'downloadUrl',
    'preview_url',
    'previewUrl',
    'url',
    'signed_url',
    'signedUrl'
  ]
  for (const key of keys) {
    const val = target[key]
    if (typeof val === 'string' && val.trim()) {
      return val
    }
  }
  return ''
}

const guessExtensionFromMime = (mime) => {
  if (!mime || typeof mime !== 'string') return ''
  const normalized = mime.toLowerCase()
  const map = {
    'application/pdf': 'pdf',
    'application/json': 'json',
    'application/geo+json': 'geojson',
    'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'docx',
    'application/vnd.ms-works': 'wps',
    'application/wps-office.doc': 'wps',
    'application/x-wps': 'wps',
    'application/kswps': 'wps',
    'application/vnd.openxmlformats-officedocument.presentationml.presentation': 'pptx',
    'application/vnd.ms-excel': 'xls',
    'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': 'xlsx',
    'application/vnd.sqlite3': 'sqlite',
    'text/csv': 'csv',
    'text/plain': 'txt',
    'image/jpeg': 'jpg',
    'image/png': 'png',
    'image/gif': 'gif',
    'image/webp': 'webp'
  }
  return map[normalized] || ''
}

const guessExtensionFromKind = (kind) => {
  const normalized = (kind || '').toLowerCase()
  switch (normalized) {
    case 'pdf':
      return 'pdf'
    case 'docx':
      return 'docx'
    case 'wps':
      return 'wps'
    case 'pptx':
      return 'pptx'
    case 'json':
      return 'json'
    case 'geojson':
      return 'geojson'
    case 'csv':
      return 'csv'
    case 'text':
      return 'txt'
    case 'sqlite':
      return 'sqlite'
    case 'parquet':
      return 'parquet'
    case 'image':
      return ''
    default:
      return ''
  }
}

const ensureExtension = (name, extension) => {
  if (!name) return `download${extension}`
  if (!extension) return name
  const normalizedExt = extension.startsWith('.') ? extension.toLowerCase() : `.${extension.toLowerCase()}`
  if (name.toLowerCase().endsWith(normalizedExt)) {
    return name
  }
  return `${name}${normalizedExt}`
}

const extractExtension = (name) => {
  if (!name) return ''
  const match = String(name).match(/\.([^.]+)$/)
  return match ? match[1].toLowerCase() : ''
}

const guessMimeFromKind = (kind, fallbackMime = '') => {
  const normalized = (kind || '').toLowerCase()
  switch (normalized) {
    case 'pdf':
      return 'application/pdf'
    case 'docx':
      return 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
    case 'wps':
      return 'application/vnd.ms-works'
    case 'pptx':
      return 'application/vnd.openxmlformats-officedocument.presentationml.presentation'
    case 'json':
      return 'application/json'
    case 'geojson':
      return 'application/geo+json'
    case 'csv':
      return 'text/csv'
    case 'text':
      return 'text/plain'
    case 'sqlite':
      return 'application/vnd.sqlite3'
    case 'image':
      return 'image/png'
    default:
      return fallbackMime || 'application/octet-stream'
  }
}

const deriveBaseFileName = (data, node) => {
  const objectPath = data?.object?.path || ''
  if (objectPath) {
    const parts = objectPath.split('/').filter(Boolean)
    const last = parts.pop()
    if (last) return last
  }

  if (node?.path) {
    const parts = String(node.path).split('/').filter(Boolean)
    const last = parts.pop()
    if (last) return last
  }

  if (node?.table && node?.schema) {
    return `${node.schema}.${node.table}`
  }

  return node?.label || 'download'
}

const toBlobFromBase64 = (base64, mime) => {
  const clean = sanitizeBase64(base64)
  if (!clean) {
    throw new Error('缺少可用的 base64 数据')
  }
  const binary = atob(clean)
  const length = binary.length
  const bytes = new Uint8Array(length)
  for (let i = 0; i < length; i++) {
    bytes[i] = binary.charCodeAt(i)
  }
  return new Blob([bytes], { type: mime || 'application/octet-stream' })
}

const downloadBlob = (blob, fileName) => {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = fileName || 'download'
  link.rel = 'noopener'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

const triggerUrlDownload = (url, fileName) => {
  const link = document.createElement('a')
  link.href = url
  if (fileName) {
    link.download = fileName
  }
  link.rel = 'noopener'
  link.target = '_blank'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

const stringifyJson = (value) => {
  if (typeof value === 'string') {
    return value
  }
  try {
    return JSON.stringify(value, null, 2)
  } catch (error) {
    console.warn('JSON 序列化失败', error)
    return String(value)
  }
}

const buildCsv = (columns, rows) => {
  if (!Array.isArray(columns) || columns.length === 0) {
    return ''
  }

  const escapeCell = (value) => {
    if (value === null || value === undefined) return ''
    let str = ''
    if (typeof value === 'object') {
      str = stringifyJson(value)
    } else {
      str = String(value)
    }
    if (str.includes('"') || str.includes(',') || str.includes('\n') || str.includes('\r')) {
      return `"${str.replace(/"/g, '""')}"`
    }
    return str
  }

  const lines = []
  lines.push(columns.map(escapeCell).join(','))
  rows.forEach((row) => {
    const values = columns.map((col) => escapeCell(row?.[col]))
    lines.push(values.join(','))
  })
  return `\uFEFF${lines.join('\r\n')}`
}

// 获取预览插件信息
const activePlugin = computed(() => {
  if (!props.previewData) {
    return null
  }

  try {
    return getPreviewComponent(props.previewData)
  } catch (error) {
    console.error('❌ 获取预览插件失败:', error)
    return null
  }
})

const previewComponent = computed(() => activePlugin.value?.component ?? null)
const previewPluginName = computed(() => activePlugin.value?.name ?? '')

// 检查是否有可用的预览组件
const hasPreviewComponent = computed(() => Boolean(previewComponent.value))

// 获取文件扩展名（用于错误提示）
const fileExtension = computed(() => {
  if (!props.selectedNode) return ''
  const path = props.selectedNode.path || props.selectedNode.label || ''
  const match = path.match(/\.([^.]+)$/)
  return match ? match[1].toUpperCase() : ''
})

// Generate appropriate empty data message based on node type
const emptyDescription = computed(() => {
  if (!props.selectedNode) return 'No data available'

  const nodeType = (props.selectedNode.type || '').toLowerCase()

  // MongoDB Database node
  if (nodeType === 'database') {
    return 'Expand database and select a collection to view data'
  }

  // Relational database Schema node
  if (nodeType === 'schema') {
    return 'Expand schema and select a table to view data'
  }

  // Object storage Bucket/Prefix node
  if (nodeType === 'bucket' || nodeType === 'directory' || nodeType === 'prefix') {
    return 'Expand directory and select an object to view data'
  }

  return 'No data available'
})

// 生成组件唯一 key
const componentKey = computed(() => {
  if (!props.selectedNode || !props.previewData) {
    return 'empty'
  }

  const nodeId = props.selectedNode.id || ''
  const nodePath = props.selectedNode.path || props.selectedNode.table || ''
  const contentType = props.previewData?.object?.content_type || ''
  const contentKind = props.previewData?.object?.content?.kind || ''
  const pluginName = previewPluginName.value || (props.previewData?.mode || 'unknown')

  return `preview-${pluginName}-${nodeId}-${nodePath}-${contentType}-${contentKind}`
})

const previewMode = computed(() => (props.previewData?.mode || '').toLowerCase())
const objectData = computed(() => props.previewData?.object || {})

const engineId = computed(() => {
  return (
    props.previewData?.engineId ||
    props.previewData?.engine_id ||
    props.selectedNode?.engineId ||
    props.selectedNode?.engine_id ||
    null
  )
})

const fallbackDownloadUrl = computed(() => {
  const path = objectData.value?.path || props.selectedNode?.path || props.selectedNode?.table || ''
  if (!path || !engineId.value) {
    return ''
  }
  return `/api/preview/download?engine_id=${engineId.value}&path=${encodeURIComponent(path)}`
})

const downloadInfo = computed(() => {
  if (!props.previewData || !props.selectedNode) {
    return { available: false, reason: '' }
  }

  if (previewMode.value === 'node') {
    return { available: false, reason: '目录节点不支持下载' }
  }

  const baseName = deriveBaseFileName(props.previewData, props.selectedNode)

  if (previewMode.value === 'table') {
    const columns = Array.isArray(props.previewData.columns) ? props.previewData.columns : []
    const rows = Array.isArray(props.previewData.rows) ? props.previewData.rows : []
    if (!columns.length) {
      return { available: false, reason: '暂无可导出的表格数据' }
    }
    return {
      available: true,
      kind: 'csv',
      fileName: ensureExtension(baseName, '.csv'),
      columns,
      rows,
      note: '当前仅导出预览中的行数据'
    }
  }

  const nodeType = (objectData.value?.node_type || objectData.value?.nodeType || '').toLowerCase()
  if (['directory', 'bucket', 'prefix'].includes(nodeType)) {
    return { available: false, reason: '目录节点不支持下载' }
  }

  const content = objectData.value?.content || {}
  const metadata = content.metadata || {}
  const contentType =
    metadata.content_type ||
    metadata.contentType ||
    objectData.value?.content_type ||
    objectData.value?.contentType ||
    props.previewData?.content_type ||
    ''

  const urlCandidates = []
  const collectUrl = (target) => {
    const url = pickUrl(target)
    if (url) {
      urlCandidates.push(url)
    }
  }

  collectUrl(props.previewData)
  collectUrl(props.previewData?.download)
  collectUrl(objectData.value)
  collectUrl(objectData.value?.download)
  collectUrl(content)
  collectUrl(content?.download)
  collectUrl(metadata)

  if (fallbackDownloadUrl.value) {
    urlCandidates.push(fallbackDownloadUrl.value)
  }

  if (urlCandidates.length > 0) {
    const ext = extractExtension(baseName)
    const inferredExt = ext || guessExtensionFromMime(contentType) || guessExtensionFromKind(content.kind)
    const filename = inferredExt ? ensureExtension(baseName, `.${inferredExt}`) : baseName
    return {
      available: true,
      kind: 'url',
      fileName: filename,
      url: urlCandidates[0]
    }
  }

  const base64Data =
    sanitizeBase64(
      content.data ||
        content.Data ||
        content.pdf_data ||
        content.pdfData ||
        content.image_data ||
        content.imageData ||
        ''
    )

  if (base64Data) {
    const inferredExt = extractExtension(baseName) || guessExtensionFromMime(contentType) || guessExtensionFromKind(content.kind)
    const fileName = inferredExt ? ensureExtension(baseName, `.${inferredExt}`) : baseName
    const mime = contentType || guessMimeFromKind(content.kind, 'application/octet-stream')
    return {
      available: true,
      kind: 'base64',
      fileName,
      base64: base64Data,
      mime
    }
  }

  if (content.text) {
    const kind = (content.kind || '').toLowerCase()
    let mime = 'text/plain;charset=utf-8'
    let extension = '.txt'
    if (kind === 'json') {
      mime = 'application/json;charset=utf-8'
      extension = '.json'
    } else if (kind === 'geojson') {
      mime = 'application/geo+json;charset=utf-8'
      extension = '.geojson'
    } else if (kind === 'csv') {
      mime = 'text/csv;charset=utf-8'
      extension = '.csv'
    }
    return {
      available: true,
      kind: 'text',
      fileName: ensureExtension(baseName, extension),
      text: content.text,
      mime
    }
  }

  if (content.json || content.JSON || content.geojson || content.GeoJSON) {
    const jsonValue = content.json || content.JSON || content.geojson || content.GeoJSON
    const kind = (content.kind || '').toLowerCase()
    let mime = 'application/json;charset=utf-8'
    let extension = '.json'
    if (kind === 'geojson' || content.geojson || content.GeoJSON) {
      mime = 'application/geo+json;charset=utf-8'
      extension = '.geojson'
    }
    return {
      available: true,
      kind: 'json',
      fileName: ensureExtension(baseName, extension),
      json: jsonValue,
      mime
    }
  }

  return { available: false, reason: '未找到可下载的数据源' }
})

const downloading = ref(false)

const showDownloadControl = computed(() => {
  if (!props.previewData || !props.selectedNode) return false
  return previewMode.value !== 'node'
})

const downloadDisabled = computed(() => !downloadInfo.value.available)
const downloadTip = computed(() => downloadInfo.value.reason || '')

const handleDownload = async () => {
  if (!downloadInfo.value.available) {
    ElMessage.warning(downloadInfo.value.reason || '当前预览不支持下载')
    return
  }

  downloading.value = true
  try {
    const info = downloadInfo.value
    switch (info.kind) {
      case 'url':
        triggerUrlDownload(info.url, info.fileName)
        break
      case 'base64': {
        const blob = toBlobFromBase64(info.base64, info.mime)
        downloadBlob(blob, info.fileName)
        break
      }
      case 'text': {
        const blob = new Blob([info.text], { type: info.mime || 'text/plain;charset=utf-8' })
        downloadBlob(blob, info.fileName)
        break
      }
      case 'json': {
        const jsonText = stringifyJson(info.json)
        const blob = new Blob([jsonText], { type: info.mime || 'application/json;charset=utf-8' })
        downloadBlob(blob, info.fileName)
        break
      }
      case 'csv': {
        const csv = buildCsv(info.columns, info.rows)
        if (!csv) {
          throw new Error('暂无可导出的表格数据')
        }
        const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
        downloadBlob(blob, info.fileName)
        break
      }
      default:
        throw new Error('未知的下载类型')
    }
    if (info.note) {
      ElMessage.success(info.note)
    } else {
      ElMessage.success('下载任务已开始')
    }
  } catch (error) {
    console.error('下载失败:', error)
    ElMessage.error(`下载失败: ${error.message || error}`)
  } finally {
    downloading.value = false
  }
}

// 监听数据变化，输出调试信息
watch(
  () => props.previewData,
  (newData) => {
    if (newData) {
      console.log('📦 PreviewPanel 收到新数据:', {
        mode: newData.mode,
        contentKind: newData.object?.content?.kind,
        contentType: newData.object?.content_type,
        plugin: previewPluginName.value,
        hasComponent: hasPreviewComponent.value
      })
    }
  },
  { immediate: true, deep: true }
)

watch(
  () => props.previewData,
  () => {
    downloading.value = false
  }
)

// 组件卸载时清理状态（防止 race condition 导致的错误）
onUnmounted(() => {
  downloading.value = false
})

const title = computed(() => {
  if (!props.selectedNode) return '数据预览'

  const node = props.selectedNode
  const nodeType = node.nodeType || node.type

  // 对象存储类型
  if (['object', 'directory', 'bucket'].includes(nodeType)) {
    const path = node.path || node.table || ''
    if (path) {
      return `${node.schema}/${path} - 数据预览`
    }
    return `${node.schema || node.label || ''} - 数据预览`
  }

  // 表格类型
  if (node.schema && node.table) {
    return `${node.schema}.${node.table} - 数据预览`
  }

  return `${node.label || ''} - 数据预览`
})

// 表格信息相关计算属性
const showTableInfo = computed(() => {
  return previewMode.value === 'table' && props.previewData
})

const tableTotal = computed(() => {
  return props.previewData?.total || 0
})

const hasGeometry = computed(() => {
  const geometryColumns = props.previewData?.geometry_columns || []
  return geometryColumns.length > 0
})

const spatialInfoTooltip = computed(() => {
  if (!hasGeometry.value) return ''

  const parts = []
  const geometryColumns = props.previewData?.geometry_columns || []
  const srid = props.previewData?.srid || 0
  const extent = props.previewData?.extent || []

  // 几何列
  if (geometryColumns.length > 0) {
    parts.push(`几何列: ${geometryColumns.join(', ')}`)
  }

  // SRID
  if (srid > 0) {
    parts.push(`SRID: ${srid}`)
  }

  // 空间范围
  if (extent && extent.length === 4) {
    const [minX, minY, maxX, maxY] = extent
    parts.push(`空间范围:\n  minX: ${minX}\n  minY: ${minY}\n  maxX: ${maxX}\n  maxY: ${maxY}`)
  }

  return parts.join('\n')
})

// 判断是否显示向量化按钮（仅 MinIO/S3 的对象、目录、Bucket）
const showVectorizeButton = computed(() => {
  if (!props.selectedNode) return false
  if (!props.previewData) return false

  const node = props.selectedNode
  const nodeType = node.nodeType || node.type

  // 检查预览模式（对象存储的节点预览模式是 'object' 或 'node'）
  const previewMode = props.previewData.mode || ''
  if (previewMode !== 'object' && previewMode !== 'node') {
    return false
  }

  // 支持 object、directory、bucket 类型
  return ['object', 'directory', 'bucket'].includes(nodeType)
})

// 向量化按钮文本
const vectorizeButtonText = computed(() => {
  if (!props.selectedNode) return '向量化'

  const nodeType = props.selectedNode.nodeType || props.selectedNode.type

  if (nodeType === 'object') {
    return '向量化'
  } else if (nodeType === 'directory' || nodeType === 'bucket') {
    return '批量向量化'
  }

  return '向量化'
})

// 处理向量化按钮点击
const handleVectorize = async () => {
  if (!props.selectedNode) return

  const node = props.selectedNode
  const nodeType = node.nodeType || node.type

  // 创建通知实例
  let notification = null

  try {
    // 解析 locator 提取参数
    const locator = node.locator || node.id
    const loc = parseLocator(locator)
    const engineId = loc.engineId
    const bucket = loc.path[0]

    // 构建请求参数
    const params = {
      engine_id: engineId,
      bucket: bucket
    }

    // 根据节点类型设置不同的参数
    if (nodeType === 'object') {
      params.scope = 'object'
      params.object_key = loc.path.slice(1).join('/')
    } else if (nodeType === 'directory') {
      params.scope = 'directory'
      params.prefix = loc.path.slice(1).join('/') + '/'
      params.recursive = true
    } else if (nodeType === 'bucket') {
      params.scope = 'bucket'
    }

    // 调用后端 API
    const response = await client.post('/manager/embedding', {
      operator_name: 'embedding',
      params: params,
      execute_now: true
    })

    // 获取响应数据
    const responseData = response?.data || response
    console.log('向量化任务响应:', responseData)

    const taskId = responseData?.task_id
    if (!taskId) {
      ElMessage.success('向量化任务已提交')
      return
    }

    // 显示进度通知
    const targetName = nodeType === 'object'
      ? params.object_key
      : (nodeType === 'bucket' ? bucket : node.label)

    notification = ElNotification({
      title: '向量化进行中',
      message: `正在向量化：${targetName}`,
      type: 'info',
      duration: 0, // 不自动关闭
      position: 'bottom-right'
    })

    // 开始轮询任务状态
    pollTaskStatus(taskId, notification, targetName, nodeType)

  } catch (error) {
    console.error('向量化失败:', error)
    if (notification) {
      notification.close()
    }
    ElMessage.error('向量化失败: ' + (error.response?.data?.error || error.message))
  }
}

// 轮询任务状态
const pollTaskStatus = async (taskId, notification, targetName, nodeType, maxAttempts = 30) => {
  let attempts = 0
  const pollInterval = 2000 // 2秒

  const poll = async () => {
    try {
      attempts++

      const response = await client.get(`/manager/embedding/tasks/${taskId}`)
      const data = response?.data || response

      console.log(`[轮询 ${attempts}/${maxAttempts}] 任务状态:`, data.task_status, data.message)

      if (data.task_status === 'completed') {
        // 任务成功完成
        notification.close()

        // 根据 result 显示详细信息
        const result = data.result

        // 单文件向量化
        if (nodeType === 'object' || !result.total) {
          // 检查是否跳过
          if (result.skipped) {
            ElNotification({
              title: '向量化跳过',
              message: `文件已经向量化，跳过：${targetName}\n原因：${result.skip_reason || '未修改'}`,
              type: 'info',
              duration: 5000,
              position: 'bottom-right'
            })
            return
          }
          // 正常成功
          ElNotification({
            title: '向量化完成',
            message: `向量化成功：${targetName}`,
            type: 'success',
            duration: 5000,
            position: 'bottom-right'
          })
          return
        }

        // 批量向量化显示统计信息
        const { total = 0, vectorized = 0, skipped = 0, failed = 0, errors = [] } = result
        let successMessage = `向量化完成：${targetName}\n总计: ${total}, 成功: ${vectorized}, 跳过: ${skipped}, 失败: ${failed}`

        // 如果有失败，显示错误详情
        if (failed > 0 && errors.length > 0) {
          successMessage += '\n\n失败详情：\n' + errors.slice(0, 5).join('\n')
          if (errors.length > 5) {
            successMessage += `\n... 还有 ${errors.length - 5} 个错误`
          }
        }

        ElNotification({
          title: failed > 0 ? '向量化完成（有失败）' : '向量化完成',
          message: successMessage,
          type: failed > 0 ? 'warning' : 'success',
          duration: failed > 0 ? 10000 : 5000,
          position: 'bottom-right'
        })
        return
      }

      if (data.task_status === 'failed') {
        // 任务失败
        notification.close()
        ElNotification({
          title: '向量化失败',
          message: data.error || data.message || '未知错误',
          type: 'error',
          duration: 8000,
          position: 'bottom-right'
        })
        return
      }

      // 任务仍在运行
      if (data.task_status === 'running' || data.task_status === 'pending') {
        if (attempts >= maxAttempts) {
          // 达到最大轮询次数
          notification.close()
          ElNotification({
            title: '向量化超时',
            message: `任务执行时间过长，请稍后在数据库中查看结果`,
            type: 'warning',
            duration: 6000,
            position: 'bottom-right'
          })
          return
        }

        // 继续轮询
        setTimeout(poll, pollInterval)
        return
      }

    } catch (error) {
      console.error('轮询任务状态失败:', error)

      if (error.response?.status === 404) {
        // 任务不存在（可能已被清理）
        notification.close()
        ElNotification({
          title: '任务已完成',
          message: '任务可能已完成，请在数据库中查看结果',
          type: 'info',
          duration: 5000,
          position: 'bottom-right'
        })
        return
      }

      // 其他错误，继续轮询
      if (attempts < maxAttempts) {
        setTimeout(poll, pollInterval)
      } else {
        notification.close()
        ElNotification({
          title: '状态查询失败',
          message: '无法获取任务状态，请稍后查看结果',
          type: 'warning',
          duration: 5000,
          position: 'bottom-right'
        })
      }
    }
  }

  // 开始第一次轮询
  setTimeout(poll, pollInterval)
}

const handlePageChange = (page) => {
  emit('page-change', page)
}

const handleNavigate = (path) => {
  emit('navigate', path)
}
</script>

<style scoped>
.preview-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.preview-panel :deep(.el-card__body) {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-weight: 600;
  gap: 12px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 16px;
  flex: 1;
}

.header-title {
  white-space: nowrap;
}

.table-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.info-badge {
  display: flex;
  align-items: center;
  gap: 4px;
  font-weight: 600;
}

.info-badge .el-icon {
  font-size: 14px;
}

.panel-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.empty-state {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.preview-content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
</style>
