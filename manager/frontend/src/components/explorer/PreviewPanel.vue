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
                {{ t('manager.explorer.spatial') }}
              </el-tag>
            </el-tooltip>
            <!-- 普通表标签 -->
            <el-tag v-else size="small" class="info-badge">
              <el-icon><Collection /></el-icon>
              {{ t('manager.explorer.dataTable') }}
            </el-tag>
            <!-- 总行数 / 预览行数 -->
            <el-tag v-if="tableTotal > 0" size="small" type="info">
              {{ tableTotalText }}
            </el-tag>
          </div>
          <!-- 对象存储元数据信息 -->
          <div v-if="showObjectInfo" class="object-info">
            <!-- 文件类型标签（带 tooltip 显示详细信息）-->
            <el-tooltip
              :content="objectMetadataTooltip"
              placement="bottom"
              :show-after="300"
              raw-content
            >
              <el-tag size="small" type="primary" class="info-badge clickable">
                {{ objectFileTypeLabel }}
              </el-tag>
            </el-tooltip>
          </div>
        </div>
        <div class="panel-actions">
          <!-- Markdown 渲染/原文切换按钮 -->
          <el-button
            v-if="isMarkdownContent"
            size="small"
            :type="markdownRawMode ? 'default' : 'primary'"
            @click="markdownRawMode = !markdownRawMode"
          >
            <el-icon><component :is="markdownRawMode ? View : Document" /></el-icon>
            {{ markdownRawMode ? t('manager.explorer.mdRendered') : t('manager.explorer.mdRaw') }}
          </el-button>

          <!-- 导入数据按钮（仅 PostgreSQL schema 节点） -->
          <el-button
            v-if="showImportButton"
            size="small"
            type="warning"
            @click="importDialogVisible = true"
          >
            <el-icon><Upload /></el-icon>
            {{ t('manager.explorer.importData') }}
          </el-button>

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
                {{ t('manager.explorer.downloadPage') }}
              </el-button>
            </span>
          </el-tooltip>
          <el-button
            v-if="showDownloadControl && !downloadDisabled"
            size="small"
            type="primary"
            :loading="downloading"
            @click="handleDownload"
          >
            <el-icon><Download /></el-icon>
            {{ t('manager.explorer.downloadPage') }}
          </el-button>
        </div>
      </div>
    </template>

    <!-- 无选择节点 -->
    <div v-if="!selectedNode" class="empty-state">
      <el-empty :description="t('manager.explorer.selectDataToPreview')" />
    </div>

    <!-- 无预览数据 -->
    <div v-else-if="!previewData" class="empty-state">
      <el-empty :description="emptyDescription" />
    </div>

    <!-- 无可用预览组件 -->
    <div v-else-if="!hasPreviewComponent" class="empty-state">
      <el-empty :description="t('manager.explorer.unsupportedFileType')">
        <template #description>
          <p>{{ t('manager.explorer.unsupportedFileTypeDetail', { ext: fileExtension || t('manager.explorer.thisType') }) }}</p>
          <p style="font-size: 12px; color: var(--addp-text-tertiary); margin-top: 8px;">
            {{ t('manager.explorer.supportedFormats') }}
          </p>
        </template>
      </el-empty>
    </div>

    <!-- 渲染预览组件 -->
    <div v-else class="preview-content">
      <div v-if="multiRefOptions.length" class="preview-ref-toolbar">
        <span class="preview-ref-label">{{ t('containerPreview.refs') }}</span>
        <el-select
          v-model="activeMultiRefPath"
          size="small"
          class="preview-ref-select"
          @change="handleMultiRefChange"
        >
          <el-option
            v-for="ref in multiRefOptions"
            :key="ref.key"
            :label="ref.label"
            :value="ref.path"
          />
        </el-select>
      </div>
      <component
        v-if="previewComponent"
        :is="previewComponent"
        :key="refKey"
        :data="previewData"
        v-bind="previewComponentProps"
        :loading="loading"
        @page-change="handlePageChange"
        @navigate="handleNavigate"
        @child-change="handleChildChange"
      />
    </div>

    <!-- 导入数据对话框 -->
    <ImportDialog
      v-model="importDialogVisible"
      :engine-id="selectedNode?.engineId"
      :engine-name="selectedNode?.engineName || ''"
      :schema-name="selectedNode?.schema || ''"
      @success="handleImportSuccess"
    />
  </el-card>
</template>

<script setup>
import { computed, ref, watch, onUnmounted } from 'vue'
import { ElMessage, ElNotification } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { MagicStick, Download, Location, Collection, Upload, Document, View } from '@element-plus/icons-vue'
import { getPreviewComponent } from '@/plugins/previews'
import { parseLocator } from '@addp/common-frontend'
import client from '@/api/client'
import ImportDialog from '@/components/explorer/ImportDialog.vue'
import { useExplorerStore } from '@/stores/explorer'

const { t } = useI18n()

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

const emit = defineEmits(['page-change', 'navigate', 'child-change'])
const store = useExplorerStore()
const activeMultiRefPath = ref('')

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
    'text/plain': 'txt',
    'image/jpeg': 'jpg',
    'image/png': 'png',
    'image/gif': 'gif',
    'image/webp': 'webp'
  }
  return map[normalized] || ''
}

const contentPreviewMaterial = (content = {}) =>
  (
    content.preview_material ||
    content.previewMaterial ||
    content.metadata?.preview_material ||
    content.metadata?.previewMaterial ||
    ''
  ).toString().toLowerCase()

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
    throw new Error(t('manager.explorer.missingBase64Data'))
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
const previewComponentProps = computed(() => {
  if (isMarkdownContent.value) {
    return { rawMode: markdownRawMode.value }
  }
  if (previewPluginName.value === 'container-preview') {
    return {
      activeChildPreview: store.activeChildPreviewData,
      activeChildLoading: store.childPreviewLoading,
      selectedChildName: store.selectedChildName,
      selectedChildKey: store.selectedChildKey,
      selectedRefPath: store.selectedRefPath
    }
  }
  return {}
})

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

const rawMultiRefs = computed(() => {
  const attrs = props.previewData?.object?.attributes || {}
  const contentMetadata = props.previewData?.object?.content?.metadata || {}
  const candidates = [
    contentMetadata.refs,
    attrs.item?.refs
  ]
  for (const value of candidates) {
    if (Array.isArray(value) && value.length) {
      return value
    }
  }
  return []
})

const refDisplayName = (path) => {
  if (!path) return ''
  const parts = String(path).split(/[\\/]/).filter(Boolean)
  return parts.pop() || String(path)
}

const multiRefOptions = computed(() => {
  const refs = rawMultiRefs.value
    .filter(ref => ref && ref.path)
    .map((ref, index) => {
      const path = ref.path || ''
      const label = ref.label || ref.role || ref.key || refDisplayName(path) || String(index)
      const fileName = refDisplayName(path)
      return {
        key: ref.key || ref.role || path || String(index),
        path,
        label: fileName && !String(label).includes(fileName) ? `${label} · ${fileName}` : String(label)
      }
    })
  if (!refs.length) return []
  return [
    {
      key: '__combined__',
      path: '',
      label: t('containerPreview.combinedPreview')
    },
    ...refs
  ]
})

watch(
  () => props.previewData?.object?.path,
  () => {
    activeMultiRefPath.value = store.selectedRefPath || ''
  },
  { immediate: true }
)

// 生成组件唯一 key
const refKey = computed(() => {
  if (!props.selectedNode || !props.previewData) {
    return 'empty'
  }

  const nodeId = props.selectedNode.id || ''
  const nodePath = props.selectedNode.path || props.selectedNode.table || ''
  const objectPath = props.previewData?.object?.path || props.previewData?.object?.object_key || ''
  const contentType = props.previewData?.object?.content_type || ''
  const content = props.previewData?.object?.content || {}
  const contentKind = content.kind || ''
  const contentJson = content.json || content.JSON || {}
  const contentFormat = contentJson?.format || content.metadata?.format || ''
  const defaultChild = contentJson?.active_child || contentJson?.default_child || ''
  const childrenSignature = Array.isArray(contentJson?.children)
    ? contentJson.children.map((child) => child?.key || child?.name || child?.table || '').filter(Boolean).join('|')
    : ''
  const pluginName = previewPluginName.value || (props.previewData?.mode || 'unknown')

  return [
    'preview',
    pluginName,
    nodeId,
    nodePath,
    objectPath,
    store.selectedChildName,
    store.selectedChildKey,
    store.selectedRefPath,
    store.selectedNestedChildPath,
    contentType,
    contentKind,
    contentFormat,
    defaultChild,
    childrenSignature
  ].join('-')
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
    return { available: false, reason: t('manager.explorer.dirNodeNoDownload') }
  }

  const baseName = deriveBaseFileName(props.previewData, props.selectedNode)

  if (previewMode.value === 'table') {
    const columns = Array.isArray(props.previewData.columns) ? props.previewData.columns : []
    const rows = Array.isArray(props.previewData.rows) ? props.previewData.rows : []
    if (!columns.length) {
      return { available: false, reason: t('manager.explorer.noTableDataToExport') }
    }
    return {
      available: true,
      kind: 'csv',
      fileName: ensureExtension(baseName, '.csv'),
      columns,
      rows,
      note: t('manager.explorer.downloadNotePreviewOnly')
    }
  }

  const nodeType = (objectData.value?.node_type || objectData.value?.nodeType || '').toLowerCase()
  if (['directory', 'bucket', 'prefix'].includes(nodeType)) {
    return { available: false, reason: t('manager.explorer.dirNodeNoDownload') }
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
    const material = contentPreviewMaterial(content)
    let mime = 'text/plain;charset=utf-8'
    let extension = '.txt'
    if (material === 'geojson') {
      mime = 'application/geo+json;charset=utf-8'
      extension = '.geojson'
    } else if (kind === 'json') {
      mime = 'application/json;charset=utf-8'
      extension = '.json'
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
    const material = contentPreviewMaterial(content)
    let mime = 'application/json;charset=utf-8'
    let extension = '.json'
    if (material === 'geojson' || content.geojson || content.GeoJSON) {
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

  return { available: false, reason: t('manager.explorer.noDownloadSource') }
})

const downloading = ref(false)
const importDialogVisible = ref(false)

// 切换节点时重置预览局部状态
watch(
  () => props.selectedNode?.id,
  () => {
    markdownRawMode.value = false
  }
)

// 导入按钮：仅在 PostgreSQL schema 节点显示
const showImportButton = computed(() => {
  if (!props.selectedNode) return false
  const nodeType = (props.selectedNode.type || '').toLowerCase()
  const engineType = (props.selectedNode.engineType || '').toLowerCase()
  return nodeType === 'schema' && engineType === 'postgresql'
})

const handleImportSuccess = async () => {
  importDialogVisible.value = false
  // 刷新当前项，重新拉取 Meta 和预览数据
  if (props.selectedNode?.locator) {
    try {
      await store.refreshItem(props.selectedNode.locator)
      ElMessage.success(t('manager.explorer.importSuccessRefreshed'))
    } catch (error) {
      console.error('刷新节点失败:', error)
    }
  }
}

const showDownloadControl = computed(() => {
  if (!props.previewData || !props.selectedNode) return false
  return previewMode.value !== 'node'
})

const downloadDisabled = computed(() => !downloadInfo.value.available)
const downloadTip = computed(() => downloadInfo.value.reason || '')

const handleDownload = async () => {
  if (!downloadInfo.value.available) {
    ElMessage.warning(downloadInfo.value.reason || t('manager.explorer.downloadNotSupported'))
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
          throw new Error(t('manager.explorer.noTableDataToExport'))
        }
        const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
        downloadBlob(blob, info.fileName)
        break
      }
      default:
        throw new Error(t('manager.explorer.unknownDownloadType'))
    }
    if (info.note) {
      ElMessage.success(info.note)
    } else {
      ElMessage.success(t('manager.explorer.downloadStarted'))
    }
  } catch (error) {
    console.error('下载失败:', error)
    ElMessage.error(t('manager.explorer.downloadFailed', { error: error.message || error }))
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
  if (!props.selectedNode) return t('manager.explorer.dataPreview')

  const node = props.selectedNode
  const nodeType = node.nodeType || node.type
  const childName = store.selectedChildName || store.selectedChildKey

  // 对象/文件系统类型
  if (['object', 'file', 'directory', 'bucket', 'prefix'].includes(nodeType)) {
    const displayPath = node.path || node.table || node.label || node.schema || ''
    const childSuffix = childName ? ` / ${childName}` : ''
    return `${displayPath}${childSuffix} - ${t('manager.explorer.dataPreview')}`
  }

  // 表格类型
  if (node.schema && node.table) {
    return `${node.schema}/${node.table} - ${t('manager.explorer.dataPreview')}`
  }

  return `${node.label || ''} - ${t('manager.explorer.dataPreview')}`
})

// 表格信息相关计算属性
const showTableInfo = computed(() => {
  return previewMode.value === 'table' && props.previewData
})

const tableTotal = computed(() => {
  return props.previewData?.total || 0
})

const tableTotalText = computed(() => {
  const total = tableTotal.value
  const rows = props.previewData?.rows || []
  const rowCount = rows.length

  // 如果预览行数小于总行数，说明只是预览了部分数据
  if (rowCount < total) {
    return t('manager.explorer.previewRows', { rowCount: rowCount.toLocaleString(), total: total.toLocaleString() })
  }

  // 否则显示总计
  return t('manager.explorer.totalRows', { total: total.toLocaleString() })
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
    parts.push(`${t('manager.explorer.geometryColumns')}: ${geometryColumns.join(', ')}`)
  }

  // SRID
  if (srid > 0) {
    parts.push(`SRID: ${srid}`)
  }

  // 空间范围
  if (extent && extent.length === 4) {
    const [minX, minY, maxX, maxY] = extent
    parts.push(`${t('manager.explorer.spatialExtent')}:\n  minX: ${minX}\n  minY: ${minY}\n  maxX: ${maxX}\n  maxY: ${maxY}`)
  }

  return parts.join('\n')
})

// 对象存储元数据相关计算属性
const showObjectInfo = computed(() => {
  return (previewMode.value === 'object' || previewMode.value === 'node') &&
         props.previewData?.object &&
         props.previewData.object.node_type === 'object'
})

// Markdown 切换
const markdownRawMode = ref(false)
const isMarkdownContent = computed(() => {
  return (props.previewData?.object?.content?.kind || '').toLowerCase() === 'markdown'
})

const objectSizeBytes = computed(() => {
  return objectData.value?.size_bytes || 0
})

const objectContentType = computed(() => {
  return objectData.value?.content_type || ''
})

function attributeSection(attributes = {}, section) {
  if (!attributes || !section) return {}

  const current = section.split('.').reduce((target, key) => {
    if (!target || typeof target !== 'object') return undefined
    return target[key]
  }, attributes)

  return current && typeof current === 'object' ? current : {}
}

function attributeValue(attributes = {}, section, key, ...fallbackKeys) {
  const standard = attributeSection(attributes, section)
  const keys = [key, ...fallbackKeys]
  for (const currentKey of keys) {
    const value = standard?.[currentKey]
    if (value !== undefined && value !== null && value !== '') return value
  }
  return undefined
}

function extractedMetadataValue(attributes = {}) {
  return objectData.value?.extracted_metadata ||
    objectData.value?.extractedMetadata ||
    attributeSection(attributes, 'capabilities.extraction').extracted_metadata
}

function mediaDurationSeconds(attributes = {}) {
  const durationMS = attributeValue(attributes, 'type_info.media', 'duration_ms')
  if (durationMS === undefined || durationMS === null || durationMS === '') return undefined
  const value = Number(durationMS)
  return Number.isFinite(value) ? value / 1000 : undefined
}

const objectFileTypeLabel = computed(() => {
  const contentType = objectContentType.value
  const path = objectData.value?.path || ''

  // 从 content_type 推断文件类型
  if (contentType.startsWith('image/')) {
    const format = contentType.split('/')[1].toUpperCase()
    return t('manager.explorer.fileTypeImage', { format })
  } else if (contentType.includes('pdf')) {
    return t('manager.explorer.fileTypePdf')
  } else if (contentType.includes('json')) {
    return t('manager.explorer.fileTypeJson')
  } else if (contentType.includes('text')) {
    return t('manager.explorer.fileTypeText')
  } else if (contentType.includes('video')) {
    return t('manager.explorer.fileTypeVideo')
  } else if (contentType.includes('audio')) {
    return t('manager.explorer.fileTypeAudio')
  }

  // 从文件扩展名推断
  const ext = path.split('.').pop()?.toUpperCase()
  if (ext) {
    return t('manager.explorer.fileTypeExt', { ext })
  }

  return t('manager.explorer.fileTypeGeneric')
})

// 图片尺寸信息（优先从标准 attributes 分区读取）
const objectImageDimensions = computed(() => {
  if (!objectContentType.value.startsWith('image/')) {
    return null
  }

  const attributes = objectData.value?.attributes || {}
  const extracted = extractedMetadataValue(attributes) || {}

  const width = attributeValue(attributes, 'type_info.media', 'width') || extracted.width || extracted.Width
  const height = attributeValue(attributes, 'type_info.media', 'height') || extracted.height || extracted.Height

  if (width && height) {
    return `${width} × ${height}`
  }

  return null
})

// 对象元数据 Tooltip 内容（统一显示所有元数据信息）
const objectMetadataTooltip = computed(() => {
  const parts = []
  const contentType = objectContentType.value
  const path = objectData.value?.path || ''
  const attributes = objectData.value?.attributes || {}
  const extracted = extractedMetadataValue(attributes) || {}

  // 图片特有信息
  if (contentType.startsWith('image/')) {
    // 图片尺寸（宽 高）
    const width = attributeValue(attributes, 'type_info.media', 'width') || extracted.width || extracted.Width
    const height = attributeValue(attributes, 'type_info.media', 'height') || extracted.height || extracted.Height
    if (width && height) {
      parts.push(`${t('manager.explorer.metaWidth')} ${width} ${t('manager.explorer.metaHeight')} ${height}`)
    }

    // 文件大小
    if (objectSizeBytes.value > 0) {
      parts.push(`${t('manager.explorer.metaFileSize')}: ${formatFileSize(objectSizeBytes.value)}`)
    }

    // 图片格式
    const format = attributeValue(attributes, 'type_info.media', 'encoding') || extracted.format || extracted.Format
    if (format && format !== contentType.split('/')[1]) {
      parts.push(`${t('manager.explorer.metaFormat')}: ${format}`)
    }

    // 颜色模式
    const colorMode = attributeValue(attributes, 'type_info.media', 'color_space') || extracted.color_space || extracted.mode || extracted.Mode
    if (colorMode) {
      parts.push(`${t('manager.explorer.metaColorMode')}: ${colorMode}`)
    }

    // DPI
    const dpi = extracted.dpi || extracted.DPI
    if (dpi) {
      parts.push(`DPI: ${dpi}`)
    }
  }

  // 视频特有信息
  if (contentType.includes('video')) {
    // 兼容提取 payload 内部结构，但入口只来自标准 extraction 扩展。
    const videoMeta = extracted.custom_attrs?.video_metadata?.data ||
                      extracted.custom_attrs?.video_metadata ||
                      {}

    // 文件大小
    if (objectSizeBytes.value > 0) {
      parts.push(`${t('manager.explorer.metaFileSize')}: ${formatFileSize(objectSizeBytes.value)}`)
    }

    // 视频尺寸（宽 × 高）
    const width = attributeValue(attributes, 'type_info.media', 'width') ||
                  videoMeta.width || videoMeta.video_width ||
                  extracted.width || extracted.Width || extracted.video_width
    const height = attributeValue(attributes, 'type_info.media', 'height') ||
                   videoMeta.height || videoMeta.video_height ||
                   extracted.height || extracted.Height || extracted.video_height
    const resolution = videoMeta.resolution || videoMeta.video_resolution ||
                       extracted.resolution || extracted.Resolution

    if (width && height) {
      parts.push(`${t('manager.explorer.metaResolution')}: ${width} × ${height}`)
    } else if (resolution && typeof resolution === 'string') {
      parts.push(`${t('manager.explorer.metaResolution')}: ${resolution}`)
    }

    // 时长
    const duration = mediaDurationSeconds(attributes) ||
                     videoMeta.duration || videoMeta.duration_seconds || videoMeta.total_duration ||
                     extracted.duration || extracted.Duration
    if (duration) {
      const durationStr = formatDuration(duration)
      parts.push(`${t('manager.explorer.metaDuration')}: ${durationStr}`)
    }

    // 视频编码（codec）
    const videoCodec = attributeValue(attributes, 'type_info.media', 'encoding') ||
                       videoMeta.codec || videoMeta.video_codec ||
                       extracted.video_codec || extracted.VideoCodec ||
                       extracted.codec || extracted.Codec
    if (videoCodec) {
      parts.push(`${t('manager.explorer.metaVideoCodec')}: ${videoCodec}`)
    }

    // 音频编码
    const audioCodec = videoMeta.audio_codec || videoMeta.audio_format ||
                       extracted.audio_codec || extracted.AudioCodec
    if (audioCodec) {
      parts.push(`${t('manager.explorer.metaAudioCodec')}: ${audioCodec}`)
    }

    // 帧率
    const fps = videoMeta.frame_rate || videoMeta.fps ||
                extracted.fps || extracted.FPS || extracted.frame_rate
    if (fps) {
      parts.push(`${t('manager.explorer.metaFrameRate')}: ${fps} fps`)
    }

    // 比特率
    const bitrate = videoMeta.bitrate || videoMeta.bit_rate ||
                    extracted.bitrate || extracted.Bitrate
    if (bitrate) {
      const bitrateStr = formatBitrate(bitrate)
      parts.push(`${t('manager.explorer.metaBitrate')}: ${bitrateStr}`)
    }
  }

  // 音频特有信息
  if (contentType.includes('audio')) {
    // 时长
    const duration = mediaDurationSeconds(attributes) || extracted.duration || extracted.Duration
    if (duration) {
      const durationStr = formatDuration(duration)
      parts.push(`${t('manager.explorer.metaDuration')}: ${durationStr}`)
    }

    // 音频编码
    const audioCodec = attributeValue(attributes, 'type_info.media', 'encoding') ||
                       extracted.audio_codec || extracted.AudioCodec ||
                       extracted.codec || extracted.Codec
    if (audioCodec) {
      parts.push(`${t('manager.explorer.metaAudioCodec')}: ${audioCodec}`)
    }

    // 采样率
    const sampleRate = extracted.sample_rate || extracted.SampleRate
    if (sampleRate) {
      parts.push(`${t('manager.explorer.metaSampleRate')}: ${sampleRate} Hz`)
    }

    // 比特率
    const bitrate = extracted.bitrate || extracted.Bitrate
    if (bitrate) {
      const bitrateStr = formatBitrate(bitrate)
      parts.push(`${t('manager.explorer.metaBitrate')}: ${bitrateStr}`)
    }
  }

  // PDF 特有信息
  if (contentType.includes('pdf')) {
    const pages = attributeValue(attributes, 'type_info.document', 'page_count') || extracted.pages || extracted.Pages
    if (pages) {
      parts.push(t('manager.explorer.metaPdfPages', { value: pages }))
    }

    const author = attributeValue(attributes, 'format_info.pdf', 'author') || extracted.author || extracted.Author
    if (author) {
      parts.push(t('manager.explorer.metaPdfAuthor', { value: author }))
    }

    const title = attributeValue(attributes, 'type_info.document', 'title') || extracted.title || extracted.Title
    if (title) {
      parts.push(t('manager.explorer.metaPdfTitle', { value: title }))
    }

    const creator = attributeValue(attributes, 'format_info.pdf', 'creator') || extracted.creator || extracted.Creator
    if (creator) {
      parts.push(t('manager.explorer.metaPdfCreator', { value: creator }))
    }
  }

  // 最后修改时间
  if (objectLastModified.value) {
    parts.push(t('manager.explorer.metaLastModified', { value: objectLastModified.value }))
  }

  return parts.join('\n')
})

// 格式化视频时长（秒转为 HH:MM:SS）
const formatDuration = (seconds) => {
  if (typeof seconds === 'string') {
    seconds = parseFloat(seconds)
  }
  if (isNaN(seconds) || seconds < 0) {
    return t('manager.explorer.metaUnknown')
  }

  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = Math.floor(seconds % 60)

  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
  } else {
    return `${minutes}:${String(secs).padStart(2, '0')}`
  }
}

// 格式化比特率（转为 Kbps 或 Mbps）
const formatBitrate = (bitrate) => {
  if (typeof bitrate === 'string') {
    bitrate = parseFloat(bitrate)
  }
  if (isNaN(bitrate) || bitrate < 0) {
    return t('manager.explorer.metaUnknown')
  }

  const k = 1000 // 使用 1000 而不是 1024
  if (bitrate < k) {
    return `${bitrate.toFixed(0)} bps`
  } else if (bitrate < k * k) {
    return `${(bitrate / k).toFixed(2)} Kbps`
  } else {
    return `${(bitrate / (k * k)).toFixed(2)} Mbps`
  }
}

const objectLastModified = computed(() => {
  const lastModified = objectData.value?.last_modified
  if (!lastModified) return null

  try {
    const date = new Date(lastModified)
    return new Intl.DateTimeFormat(undefined, {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false
    }).format(date)
  } catch (e) {
    return null
  }
})

// 格式化文件大小
const formatFileSize = (bytes) => {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

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
  if (!props.selectedNode) return t('manager.explorer.vectorize')

  const nodeType = props.selectedNode.nodeType || props.selectedNode.type

  if (nodeType === 'object') {
    return t('manager.explorer.vectorize')
  } else if (nodeType === 'directory' || nodeType === 'bucket') {
    return t('manager.explorer.batchVectorize')
  }

  return t('manager.explorer.vectorize')
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

    // 调用后端 API（直接发送参数，不嵌套）
    const response = await client.post('/manager/embedding', params)

    // 获取响应数据
    const responseData = response?.data || response
    console.log('向量化任务响应:', responseData)

    const taskId = responseData?.task_id
    if (!taskId) {
      ElMessage.success(t('manager.explorer.vectorizeTaskSubmitted'))
      return
    }

    // 显示进度通知
    const targetName = nodeType === 'object'
      ? params.object_key
      : (nodeType === 'bucket' ? bucket : node.label)

    notification = ElNotification({
      title: t('manager.explorer.vectorizeInProgress'),
      message: t('manager.explorer.vectorizingTarget', { name: targetName }),
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
    ElMessage.error(t('manager.explorer.vectorizeTaskFailed', { error: error.response?.data?.error || error.message }))
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
              title: t('manager.explorer.vectorizeSkipped'),
              message: t('manager.explorer.vectorizeSkippedMsg', { name: targetName, reason: result.skip_reason || t('manager.explorer.vectorizeSkipReasonDefault') }),
              type: 'info',
              duration: 5000,
              position: 'bottom-right'
            })
            return
          }
          // 正常成功
          ElNotification({
            title: t('manager.explorer.vectorizeDone'),
            message: t('manager.explorer.vectorizeSuccess', { name: targetName }),
            type: 'success',
            duration: 5000,
            position: 'bottom-right'
          })
          return
        }

        // 批量向量化显示统计信息
        const { total = 0, vectorized = 0, skipped = 0, failed = 0, errors = [] } = result
        let successMessage = t('manager.explorer.vectorizeBatchStats', { name: targetName, total, vectorized, skipped, failed })

        // 如果有失败，显示错误详情
        if (failed > 0 && errors.length > 0) {
          successMessage += t('manager.explorer.vectorizeBatchErrors') + errors.slice(0, 5).join('\n')
          if (errors.length > 5) {
            successMessage += t('manager.explorer.vectorizeBatchMoreErrors', { count: errors.length - 5 })
          }
        }

        ElNotification({
          title: failed > 0 ? t('manager.explorer.vectorizeDoneWithFailed') : t('manager.explorer.vectorizeDone'),
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
          title: t('manager.explorer.vectorizeFailed2'),
          message: data.error || data.message || t('manager.explorer.vectorizeUnknownError'),
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
            title: t('manager.explorer.vectorizeTimeout'),
            message: t('manager.explorer.vectorizeTimeoutMsg'),
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
          title: t('manager.explorer.vectorizeTaskDone'),
          message: t('manager.explorer.vectorizeTaskDoneMsg'),
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
          title: t('manager.explorer.vectorizeStatusFailed'),
          message: t('manager.explorer.vectorizeStatusFailedMsg'),
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

const handleChildChange = (payload) => {
  emit('child-change', payload)
}

const handleMultiRefChange = (path) => {
  activeMultiRefPath.value = path || ''
  emit('child-change', {
    childName: '',
    refPath: path || '',
    refSwitch: true
  })
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
  background: var(--addp-bg-primary) !important;
}

.preview-panel :deep(.el-card) {
  background: var(--addp-bg-primary) !important;
  border-color: var(--addp-border-color) !important;
}

.preview-panel :deep(.el-card__body) {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-primary) !important;
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

.object-info {
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

.info-badge.clickable {
  cursor: help;
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
  background: var(--addp-bg-primary) !important;
}

.preview-content {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: var(--addp-bg-primary) !important;
}

.preview-ref-toolbar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  background: var(--el-fill-color-blank);
}

.preview-ref-label {
  color: var(--addp-text-secondary);
  font-size: 13px;
  white-space: nowrap;
}

.preview-ref-select {
  width: min(420px, 100%);
}

/* 强制覆盖 Element Plus Empty 组件的背景 */
.preview-panel :deep(.el-empty) {
  background: transparent !important;
}

.preview-panel :deep(.el-empty__description) {
  color: var(--addp-text-secondary) !important;
}
</style>
