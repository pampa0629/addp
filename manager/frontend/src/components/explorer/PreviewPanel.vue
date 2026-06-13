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
              {{ tableInfoLabel }}
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
          <el-tag
            v-else-if="showVectorizedIndicator"
            size="small"
            type="success"
            class="vectorized-indicator"
          >
            <el-icon><Select /></el-icon>
            {{ t('manager.explorer.vectorized') }}
          </el-tag>

          <!-- 空间快显 / 瓦片缓存入口 -->
          <div v-if="showQuickViewActions" class="quick-view-actions">
            <el-tag
              v-if="quickViewStatus?.can_use_quick_view"
              size="small"
              type="success"
              class="quick-view-status"
            >
              {{ t('manager.spatialPreview.quickViewReady') }}
            </el-tag>
            <el-tag
              v-else-if="quickViewStatus && !quickViewStatus.can_generate_tile_cache"
              size="small"
              type="info"
              class="quick-view-status"
            >
              {{ quickViewStatus.unavailable_reason || t('manager.spatialPreview.quickViewUnavailable') }}
            </el-tag>
            <el-tag
              v-else-if="quickViewLoadError"
              size="small"
              type="danger"
              class="quick-view-status"
            >
              {{ quickViewLoadError }}
            </el-tag>
            <el-button
              v-if="isQuickViewActive"
              size="small"
              :loading="quickViewActionLoading"
              @click="handleBackToBasicPreview"
            >
              {{ t('manager.spatialPreview.backToBasicPreview') }}
            </el-button>
            <el-button
              v-else-if="quickViewStatus?.can_use_quick_view"
              size="small"
              type="primary"
              :loading="quickViewActionLoading"
              @click="handleSwitchQuickView"
            >
              {{ t('manager.spatialPreview.switchQuickView') }}
            </el-button>
            <el-button
              v-else-if="quickViewStatus?.can_generate_tile_cache"
              size="small"
              type="primary"
              @click="handleGenerateTileCache"
            >
              {{ t('manager.spatialPreview.generateTileCache') }}
            </el-button>
            <el-button
              v-if="showRealtimeTileCacheGeneration"
              size="small"
              @click="handleGenerateTileCache"
            >
              {{ t('manager.spatialPreview.generateTileCache') }}
            </el-button>
          </div>

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
      <el-alert
        v-if="previewRefreshAdvisory"
        class="preview-advisory"
        :title="previewRefreshAdvisoryTitle"
        type="info"
        :closable="false"
        show-icon
      >
        <template #default>
          <div class="preview-advisory-body">
            <span>{{ previewRefreshAdvisoryText }}</span>
            <el-button
              size="small"
              type="primary"
              :loading="refreshingPreviewItem"
              :disabled="!props.selectedNode?.locator"
              @click="handlePreviewAdvisoryRefresh"
            >
              <el-icon><Refresh /></el-icon>
              {{ t('manager.explorer.refreshItem') }}
            </el-button>
          </div>
        </template>
      </el-alert>
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
      <div v-if="isGraphOverview" class="graph-preview-layout">
        <div class="graph-overview-table">
          <div class="graph-overview-hint">{{ t('manager.explorer.graphOverviewHint') }}</div>
          <el-table
            :data="pagedGraphOverviewRows"
            v-loading="loading"
            height="100%"
            size="small"
            highlight-current-row
            :row-class-name="graphOverviewRowClassName"
            @row-click="handleGraphOverviewRowClick"
          >
            <el-table-column
              v-for="col in graphOverviewColumns"
              :key="col"
              :prop="col"
              :label="col"
              show-overflow-tooltip
            />
          </el-table>
          <div v-if="graphOverviewRows.length > 0" class="graph-overview-pagination">
            <el-pagination
              background
              small
              layout="total, sizes, prev, pager, next"
              :total="graphOverviewRows.length"
              :page-size="graphOverviewPageSize"
              :current-page="graphOverviewPage"
              :page-sizes="[10, 20, 50, 100]"
              @current-change="handleGraphOverviewPageChange"
              @size-change="handleGraphOverviewPageSizeChange"
            />
          </div>
        </div>
        <div v-if="showGraphSample" class="graph-sample-panel">
          <div class="graph-sample-header">
            <span class="graph-sample-title">{{ t('manager.explorer.graphSample') }}</span>
            <span class="graph-sample-count">
              {{ graphSampleStatsText }}
            </span>
          </div>
          <div class="graph-sample-grid">
            <div v-if="showGraphNodeSampleTable" class="graph-sample-table">
              <div class="graph-sample-subtitle">{{ t('manager.explorer.graphSampleNodes') }}</div>
              <el-table :data="graphSampleNodeRows" height="100%" size="small" empty-text="-">
                <el-table-column prop="name" :label="t('manager.explorer.name')" min-width="160" show-overflow-tooltip />
                <el-table-column prop="type" :label="t('manager.explorer.type')" min-width="140" show-overflow-tooltip />
                <el-table-column prop="properties" :label="t('manager.explorer.graphSampleProperties')" min-width="220" show-overflow-tooltip />
              </el-table>
            </div>
            <div v-if="showGraphRelationshipSampleTable" class="graph-sample-table">
              <div class="graph-sample-subtitle">{{ t('manager.explorer.graphSampleRelationships') }}</div>
              <el-table :data="graphSampleRelationshipRows" height="100%" size="small" empty-text="-">
                <el-table-column prop="type" :label="t('manager.explorer.type')" min-width="130" show-overflow-tooltip />
                <el-table-column prop="start" :label="t('manager.explorer.graphSampleStart')" min-width="140" show-overflow-tooltip />
                <el-table-column prop="end" :label="t('manager.explorer.graphSampleEnd')" min-width="140" show-overflow-tooltip />
                <el-table-column prop="properties" :label="t('manager.explorer.graphSampleProperties')" min-width="180" show-overflow-tooltip />
              </el-table>
            </div>
          </div>
        </div>
      </div>
      <component
        v-if="showQuickViewRenderer"
        :is="quickViewRenderer"
        :key="quickViewRenderKey"
        v-bind="quickViewRendererProps"
        class="quick-view-renderer"
      />
      <component
        v-else-if="previewComponent && !isGraphOverview"
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
import { useRouter } from 'vue-router'
import { MagicStick, Download, Location, Collection, Upload, Document, View, Refresh, Select } from '@element-plus/icons-vue'
import { getPreviewComponent } from '@/plugins/previews'
import { parseLocator } from '@addp/common-frontend'
import client from '@/api/client'
import { quickViewAPI } from '@/api/quickView'
import ImportDialog from '@/components/explorer/ImportDialog.vue'
import GeoJSONQuickView from '@/components/map/GeoJSONQuickView.vue'
import VectorTilePreview from '@/components/map/VectorTilePreview.vue'
import { useExplorerStore } from '@/stores/explorer'
import {
  canShowVectorizeAction,
  isVectorizableObjectNode,
  isVectorizableRangeNode,
  normalizedNodeType
} from '@/utils/vectorization'

const { t } = useI18n()
const router = useRouter()

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
const combinedMultiRefValue = '__combined__'
const activeGraphSampleKey = ref('')
const activeGraphSampleKind = ref('')
const activeGraphSampleTotal = ref(null)
const graphOverviewPage = ref(1)
const graphOverviewPageSize = ref(20)
const quickViewStatus = ref(null)
const quickViewLoadError = ref('')
const quickViewActionLoading = ref(false)
const activePreviewMode = ref('table_geojson')
let quickViewRequestSeq = 0

const DIRECT_GEOJSON_MAX_ROWS = 2000

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

const isStorageManagedDownloadUrl = (url) => {
  if (!url || typeof url !== 'string') return false
  try {
    const parsed = new URL(url, window.location.origin)
    return [
      '/api/v1/manager/storage-download',
      '/manager/storage-download'
    ].includes(parsed.pathname)
  } catch {
    return url.startsWith('/api/v1/manager/storage-download') ||
      url.startsWith('/manager/storage-download')
  }
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

const pickDownloadFileName = (target) => {
  if (!target || typeof target !== 'object') return ''
  const value = target.filename || target.fileName || target.name || ''
  if (typeof value !== 'string') return ''
  const trimmed = value.trim()
  if (!trimmed) return ''
  const parts = trimmed.split('/').filter(Boolean)
  return parts.pop() || trimmed
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
  const objectPath = data?.object?.path || data?.object?.storage_ref || data?.object?.storageRef || ''
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

const withAuthToken = (url) => {
  if (!url || typeof url !== 'string') return ''
  if (!url.startsWith('/api/') && !url.startsWith('/manager/')) return url
  const token = localStorage.getItem('token')
  if (!token) return url
  try {
    const parsed = new URL(url, window.location.origin)
    if (!parsed.searchParams.has('token')) {
      parsed.searchParams.set('token', token)
    }
    return parsed.origin === window.location.origin
      ? `${parsed.pathname}${parsed.search}${parsed.hash}`
      : parsed.toString()
  } catch {
    const separator = url.includes('?') ? '&' : '?'
    return `${url}${separator}token=${encodeURIComponent(token)}`
  }
}

const downloadFromUrl = (url, fileName) => {
  const link = document.createElement('a')
  link.href = withAuthToken(normalizeClientURL(url))
  link.download = fileName || 'download'
  link.rel = 'noopener'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

const normalizeClientURL = (url) => {
  if (!url || /^https?:\/\//i.test(url)) {
    return url
  }
  if (url.startsWith('/manager/')) {
    return `/api/v1${url}`
  }
  if (url.startsWith('/api/v1/')) {
    return url
  }
  return url
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
  if (!props.selectedNode) return t('manager.explorer.emptyPreview.noData')

  const nodeType = (props.selectedNode.type || '').toLowerCase()

  // MongoDB Database node
  if (nodeType === 'database') {
    return t('manager.explorer.emptyPreview.database')
  }

  // Relational database Schema node
  if (nodeType === 'schema') {
    return t('manager.explorer.emptyPreview.schema')
  }

  // Object storage Bucket/Prefix node
  if (nodeType === 'bucket' || nodeType === 'directory' || nodeType === 'prefix') {
    return t('manager.explorer.emptyPreview.directory')
  }

  return t('manager.explorer.emptyPreview.noData')
})

const rawMultiRefs = computed(() => {
  const contentMetadata = props.previewData?.object?.content?.metadata || {}
  return Array.isArray(contentMetadata.refs) ? contentMetadata.refs : []
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
      path: combinedMultiRefValue,
      label: t('containerPreview.combinedPreview')
    },
    ...refs
  ]
})

watch(
  () => [
    props.previewData?.object?.path,
    store.selectedRefPath,
    multiRefOptions.value.length
  ],
  () => {
    activeMultiRefPath.value = multiRefOptions.value.length
      ? (store.selectedRefPath || combinedMultiRefValue)
      : ''
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
  const objectPath = props.previewData?.object?.path || props.previewData?.object?.storage_ref || ''
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
  const graphKey = isGraphOverview.value ? activeGraphSampleKey.value : ''

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
    childrenSignature,
    graphKey
  ].join('-')
})

const previewMode = computed(() => (props.previewData?.mode || '').toLowerCase())
const objectData = computed(() => props.previewData?.object || {})
const graphSample = computed(() => props.previewData?.graph || null)
const graphSampleNodes = computed(() => (Array.isArray(graphSample.value?.nodes) ? graphSample.value.nodes : []))
const graphSampleRelationships = computed(() => (Array.isArray(graphSample.value?.relationships) ? graphSample.value.relationships : []))
const showGraphSample = computed(() => isGraphOverview.value && (graphSampleNodes.value.length > 0 || graphSampleRelationships.value.length > 0))
const showGraphNodeSampleTable = computed(() => {
  return graphSampleNodeRows.value.length > 0 && activeGraphSampleKind.value !== 'relationship_shape'
})
const showGraphRelationshipSampleTable = computed(() => {
  return graphSampleRelationshipRows.value.length > 0 && activeGraphSampleKind.value !== 'node_shape'
})
const graphSampleNodeById = computed(() => {
  const nodes = Array.isArray(graphSample.value?.nodes) ? graphSample.value.nodes : []
  return new Map(nodes.map(node => [node.element_id, node]))
})
const graphSampleNodeRows = computed(() => graphSampleNodes.value.map((node, index) => ({
  name: graphNodeLabel(node, index),
  type: graphNodeShape(node),
  properties: graphPropertiesSummary(node?.properties)
})))
const graphSampleRelationshipRows = computed(() => graphSampleRelationships.value.map(rel => ({
  type: rel?.type || '-',
  start: graphEndpointLabel(rel?.start_node_id),
  end: graphEndpointLabel(rel?.end_node_id),
  properties: graphPropertiesSummary(rel?.properties)
})))
const graphOverviewColumns = computed(() => (props.previewData?.columns || []).filter(column => !String(column).startsWith('__')))
const graphOverviewRows = computed(() => Array.isArray(props.previewData?.rows) ? props.previewData.rows : [])
const pagedGraphOverviewRows = computed(() => {
  const start = (graphOverviewPage.value - 1) * graphOverviewPageSize.value
  return graphOverviewRows.value.slice(start, start + graphOverviewPageSize.value)
})
const graphSampleStatsText = computed(() => {
  const params = {
    nodes: graphSampleNodes.value.length,
    relationships: graphSampleRelationships.value.length,
    total: activeGraphSampleTotal.value || graphSampleNodes.value.length + graphSampleRelationships.value.length
  }
  return activeGraphSampleKey.value
    ? t('manager.explorer.graphFilteredSampleStats', params)
    : t('manager.explorer.graphSampleStats', params)
})

const graphNodeShape = (node) => {
  const labels = Array.isArray(node?.labels) ? node.labels.filter(Boolean) : []
  return labels.length ? labels.join('+') : '-'
}

const graphNodeLabel = (node, index = 0) => {
  const props = node?.properties || {}
  const readable = props.name || props.title || props.label || props.id
  if (readable !== null && readable !== undefined && String(readable).trim()) {
    return String(readable)
  }
  const type = graphNodeShape(node)
  return type && type !== '-' ? `${type} #${index + 1}` : `#${index + 1}`
}

const graphPropertiesSummary = (properties) => {
  if (!properties || typeof properties !== 'object') return '-'
  const hidden = new Set(['name', 'title', 'label', 'id'])
  const parts = Object.entries(properties)
    .filter(([key, value]) => {
      const normalizedKey = String(key).toLowerCase()
      if (hidden.has(normalizedKey)) return false
      if (normalizedKey.startsWith('_')) return false
      if (normalizedKey.endsWith('_at') || normalizedKey.endsWith('_time')) return false
      if (normalizedKey.includes('encoder') || normalizedKey.includes('geocoder')) return false
      if (normalizedKey.includes('config')) return false
      return value !== null && value !== undefined && typeof value !== 'object'
    })
    .slice(0, 4)
    .map(([key, value]) => `${key}: ${value}`)
  return parts.length ? parts.join(', ') : '-'
}

const graphEndpointLabel = (elementId) => {
  const node = graphSampleNodeById.value.get(elementId)
  if (!node) return shortGraphElementId(elementId)
  const props = node.properties || {}
  const readable = props.name || props.title || props.label || props.id
  if (readable !== null && readable !== undefined && String(readable).trim()) {
    return String(readable)
  }
  return graphNodeShape(node) || shortGraphElementId(elementId)
}

const graphRelationshipLabel = (rel) => {
  const start = graphEndpointLabel(rel?.start_node_id)
  const end = graphEndpointLabel(rel?.end_node_id)
  return `${start} -> ${end}`
}

const graphSampleKey = (filter) => {
  if (!filter) return ''
  return [
    filter.kind || '',
    (filter.labels || []).join('+'),
    filter.type || '',
    (filter.fromLabels || []).join('+'),
    (filter.toLabels || []).join('+')
  ].join('|')
}

const graphFilterFromOverviewRow = (row) => {
  const kind = row?.__graph_sample_kind || ''
  if (kind === 'node_shape') {
    return {
      kind,
      labels: Array.isArray(row.__graph_node_labels) ? row.__graph_node_labels.filter(Boolean) : []
    }
  }
  if (kind === 'relationship_shape') {
    return {
      kind,
      type: row.__graph_relationship_type || '',
      fromLabels: Array.isArray(row.__graph_from_labels) ? row.__graph_from_labels.filter(Boolean) : [],
      toLabels: Array.isArray(row.__graph_to_labels) ? row.__graph_to_labels.filter(Boolean) : []
    }
  }
  return null
}

const handleGraphOverviewRowClick = async (row) => {
  const filter = graphFilterFromOverviewRow(row)
  if (!filter || !props.selectedNode?.locator) return
  activeGraphSampleKey.value = graphSampleKey(filter)
  activeGraphSampleKind.value = filter.kind || ''
  activeGraphSampleTotal.value = Number(row?.数量) > 0 ? Number(row.数量) : null
  try {
    await store.loadPreview(
      props.selectedNode.locator,
      1,
      store.selectedChildName,
      store.selectedRefPath,
      store.selectedNestedChildPath,
      store.selectedChildKey,
      filter
    )
  } catch (error) {
    activeGraphSampleKey.value = ''
    activeGraphSampleKind.value = ''
    activeGraphSampleTotal.value = null
    ElMessage.error(t('manager.explorer.loadPreviewFailed', { error: error.message || error }))
  }
}

const graphOverviewRowClassName = ({ row }) => {
  const key = graphSampleKey(graphFilterFromOverviewRow(row))
  return key && key === activeGraphSampleKey.value ? 'active-graph-overview-row' : ''
}

const handleGraphOverviewPageChange = (page) => {
  graphOverviewPage.value = page
}

const handleGraphOverviewPageSizeChange = (size) => {
  graphOverviewPageSize.value = size
  graphOverviewPage.value = 1
}

const shortGraphElementId = (value) => {
  const text = String(value || '')
  if (text.length <= 18) return text
  return `${text.slice(0, 8)}...${text.slice(-6)}`
}

const engineId = computed(() => {
  return (
    props.previewData?.engineId ||
    props.previewData?.engine_id ||
    props.selectedNode?.engineId ||
    props.selectedNode?.engine_id ||
    null
  )
})

const storageDownloadUrl = computed(() => {
  const storageRef =
    objectData.value?.storage_ref ||
    objectData.value?.storageRef ||
    ''
  if (!storageRef || !engineId.value) {
    return ''
  }
  return `/manager/storage-download?engine_id=${encodeURIComponent(engineId.value)}&storage_ref=${encodeURIComponent(storageRef)}`
})

const downloadInfo = computed(() => {
  if (!props.previewData || !props.selectedNode) {
    return { available: false, reason: '' }
  }

  if (previewMode.value === 'node') {
    return { available: false, reason: t('manager.explorer.dirNodeNoDownload') }
  }

  const baseName = deriveBaseFileName(props.previewData, props.selectedNode)

  const content = objectData.value?.content || {}
  const metadata = content.metadata || {}
  const material = contentPreviewMaterial(content)
  const contentType =
    metadata.content_type ||
    metadata.contentType ||
    objectData.value?.content_type ||
    objectData.value?.contentType ||
    props.previewData?.content_type ||
    ''
  const ext = extractExtension(baseName)
  const inferredExt = ext || guessExtensionFromMime(contentType) || guessExtensionFromKind(content.kind)
  const fileName = inferredExt ? ensureExtension(baseName, `.${inferredExt}`) : baseName

  const objectDownloadUrl = pickUrl(objectData.value?.download)
  if (objectDownloadUrl) {
    return {
      available: true,
      kind: 'url',
      fileName: pickDownloadFileName(objectData.value?.download) || fileName,
      url: objectDownloadUrl
    }
  }

  if (storageDownloadUrl.value) {
    return {
      available: true,
      kind: 'url',
      fileName,
      url: storageDownloadUrl.value
    }
  }

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

  const managedDownloadUrlCandidate = urlCandidates.find(isStorageManagedDownloadUrl)
  if (managedDownloadUrlCandidate) {
    return {
      available: true,
      kind: 'url',
      fileName,
      url: managedDownloadUrlCandidate
    }
  }

  const renderer = (
    content.frontend_renderer ||
    content.frontendRenderer ||
    metadata.frontend_renderer ||
    metadata.frontendRenderer ||
    ''
  ).toString().toLowerCase()
  const kind = (content.kind || '').toLowerCase()
  if (renderer === 'unsupported' || material === 'unsupported' || kind === 'unsupported') {
    return { available: false, reason: t('manager.explorer.noDownloadSource') }
  }

  if (urlCandidates.length > 0) {
    return {
      available: true,
      kind: 'url',
      fileName,
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
const refreshingPreviewItem = ref(false)
const selectedEmbeddingState = ref(null)

const loadSelectedItemEmbeddingState = async () => {
  const node = props.selectedNode
  if (!node || !isVectorizableObjectNode(node, props.previewData)) {
    selectedEmbeddingState.value = null
    return
  }
  const locator = node.locator || node.id
  if (!locator) {
    selectedEmbeddingState.value = null
    return
  }
  try {
    const loc = parseLocator(locator)
    if (!loc.itemId) {
      selectedEmbeddingState.value = null
      return
    }
    selectedEmbeddingState.value = await client.get(`/manager/items/${loc.itemId}/embedding`)
  } catch (error) {
    selectedEmbeddingState.value = null
    console.warn('加载 item 向量化状态失败:', error)
  }
}

// 切换节点时重置预览局部状态
watch(
  () => props.selectedNode?.id,
  () => {
    markdownRawMode.value = false
    activePreviewMode.value = 'table_geojson'
    activeGraphSampleKey.value = ''
    activeGraphSampleKind.value = ''
    activeGraphSampleTotal.value = null
    graphOverviewPage.value = 1
  }
)

watch(
  () => props.previewData?.rows,
  () => {
    graphOverviewPage.value = 1
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

const previewRefreshAdvisory = computed(() => {
  const advisories = Array.isArray(props.previewData?.preview_advisories)
    ? props.previewData.preview_advisories
    : []
  return advisories.find(advisory =>
    advisory?.action === 'item_refresh' &&
    ['item_refresh_recommended', 'access_index_refresh_recommended'].includes(advisory?.code)
  ) || null
})

const previewRefreshAdvisoryTitle = computed(() => {
  if (previewRefreshAdvisory.value?.code === 'access_index_refresh_recommended') {
    return t('manager.explorer.accessIndexRefreshTitle')
  }
  return t('manager.explorer.itemRefreshRecommendedTitle')
})

const previewRefreshAdvisoryText = computed(() => {
  if (previewRefreshAdvisory.value?.code === 'access_index_refresh_recommended') {
    return t('manager.explorer.accessIndexRefreshText')
  }
  return t('manager.explorer.itemRefreshRecommendedText')
})

const handlePreviewAdvisoryRefresh = async () => {
  if (!props.selectedNode?.locator || refreshingPreviewItem.value) {
    return
  }
  refreshingPreviewItem.value = true
  try {
    await store.refreshItem(props.selectedNode.locator)
    ElMessage.success(t('manager.explorer.refreshSuccess'))
  } catch (error) {
    ElMessage.error(t('manager.explorer.refreshFailed', { error: error?.message || error }))
  } finally {
    refreshingPreviewItem.value = false
  }
}

const showDownloadControl = computed(() => {
  if (!props.previewData || !props.selectedNode) return false
  if (isGraphOverview.value) return false
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
        await downloadFromUrl(info.url, info.fileName)
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

watch(
  () => props.previewData,
  () => {
    downloading.value = false
    loadSelectedItemEmbeddingState()
  }
)

watch(
  () => props.selectedNode?.locator || props.selectedNode?.id || '',
  () => {
    selectedEmbeddingState.value = null
    loadSelectedItemEmbeddingState()
  },
  { immediate: true }
)

// 组件卸载时清理状态（防止 race condition 导致的错误）
onUnmounted(() => {
  downloading.value = false
  quickViewRequestSeq += 1
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

const isGraphOverview = computed(() => {
  return props.previewData?.preview_kind === 'graph_overview'
})

const tableInfoLabel = computed(() => {
  return isGraphOverview.value ? t('manager.explorer.graphOverview') : t('manager.explorer.dataTable')
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
  const geometryColumn = String(props.previewData?.geometry_column || '').trim()
  return geometryColumns.length > 0 || geometryColumn !== ''
})

const spatialInfoTooltip = computed(() => {
  if (!hasGeometry.value) return ''

  const parts = []
  const geometryColumns = props.previewData?.geometry_columns || []
  const srid = props.previewData?.source_srid || 0
  const sourceCRS = String(props.previewData?.source_crs || '').trim()
  const extent = props.previewData?.extent || []

  // 几何列
  if (geometryColumns.length > 0) {
    parts.push(`${t('manager.explorer.geometryColumns')}: ${geometryColumns.join(', ')}`)
  }

  // SRID
  if (srid > 0) {
    parts.push(`SRID: ${srid}`)
  }
  if (sourceCRS) {
    parts.push(`${t('manager.explorer.sourceCRS')}: ${sourceCRS}`)
  }

  // 空间范围
  if (extent && extent.length === 4) {
    const [minX, minY, maxX, maxY] = extent
    parts.push(`${t('manager.explorer.spatialExtent')}:\n  minX: ${minX}\n  minY: ${minY}\n  maxX: ${maxX}\n  maxY: ${maxY}`)
  }

  return parts.join('\n')
})

const spatialPreviewTarget = computed(() => {
  if (!hasGeometry.value || !props.selectedNode) return null
  const node = props.selectedNode
  const locator = String(node.locator || node.id || '').trim()
  let parsedLocator = null
  if (locator) {
    try {
      parsedLocator = parseLocator(locator)
    } catch (_error) {
      parsedLocator = null
    }
  }
  const engine = Number(
    node.engineId ||
    node.engine_id ||
    parsedLocator?.engineId ||
    props.previewData?.engineId ||
    props.previewData?.engine_id ||
    engineId.value ||
    0
  )
  if (!engine) return null
  const locatorType = String(parsedLocator?.type || node.nodeType || node.type || '').toLowerCase()
  const schema = String(node.schema || '').trim()
  const table = String(node.table || '').trim()
  const geometryColumns = Array.isArray(props.previewData?.geometry_columns)
    ? props.previewData.geometry_columns
    : []
  const extent = Array.isArray(props.previewData?.extent) ? props.previewData.extent : []
  return {
    engineId: engine,
    schema,
    table,
    locator,
    locatorType,
    geometryColumn: String(props.previewData?.geometry_column || geometryColumns[0] || '').trim(),
    geometryColumns: geometryColumns.map((column) => String(column || '').trim()).filter(Boolean),
    sourceSRID: Number(props.previewData?.source_srid || props.previewData?.srid || 0),
    extentSRID: Number(props.previewData?.extent_srid || props.previewData?.srid || 0),
    extent,
    recordCount: Number(props.previewData?.total || props.previewData?.rows?.length || 0)
  }
})

const showQuickViewActions = computed(() => {
  return !!spatialPreviewTarget.value && (!!quickViewStatus.value || !!quickViewLoadError.value)
})

const isQuickViewActive = computed(() => {
  return activePreviewMode.value === 'quick_view' && !!quickViewStatus.value?.can_use_quick_view
})

const quickViewRenderSource = computed(() => String(
  quickViewStatus.value?.render_source || quickViewStatus.value?.quick_view?.render_source || ''
).trim())

const quickViewRenderer = computed(() => {
  if (!isQuickViewActive.value) return null
  if (quickViewRenderSource.value === 'direct_geojson') return GeoJSONQuickView
  if (['cached_tile', 'realtime_tile'].includes(quickViewRenderSource.value)) return VectorTilePreview
  return null
})

const showRealtimeTileCacheGeneration = computed(() => {
  return quickViewRenderSource.value === 'realtime_tile' && !!quickViewStatus.value?.can_generate_tile_cache
})

const quickViewRendererProps = computed(() => {
  const target = spatialPreviewTarget.value
  if (!target || !quickViewStatus.value) return {}
  if (quickViewRenderSource.value === 'direct_geojson') {
    return { status: quickViewStatus.value }
  }
  return {
    locator: target.locator,
    engineId: target.engineId,
    schema: target.schema,
    table: target.table,
    geom: quickViewStatus.value?.quick_view?.geometry_column || target.geometryColumn,
    tileUrlTemplate: quickViewStatus.value?.quick_view?.tile_url_template || '',
    tileRenderInfo: quickViewStatus.value?.quick_view || {},
    renderSource: quickViewRenderSource.value,
    defaultTileCacheId: quickViewStatus.value?.default_tile_cache_id || ''
  }
})

const showQuickViewRenderer = computed(() => Boolean(quickViewRenderer.value))

const quickViewRenderKey = computed(() => {
  const target = spatialPreviewTarget.value
  if (!target) return 'quick-view-empty'
  return [
    'quick-view',
    target.engineId,
    target.schema || target.locator,
    target.table || target.locatorType,
    quickViewRenderSource.value,
    quickViewStatus.value?.default_tile_cache_id || '',
    quickViewStatus.value?.quick_view?.geojson_url || ''
  ].join('-')
})

const loadQuickViewStatus = async () => {
  const target = spatialPreviewTarget.value
  quickViewStatus.value = null
  quickViewLoadError.value = ''
  quickViewRequestSeq += 1
  const seq = quickViewRequestSeq
  if (!target) return
  try {
    const status = await quickViewAPI.getQuickViewCapabilityByLocator(target.locator)
    if (seq === quickViewRequestSeq) {
      quickViewStatus.value = status
      if (status?.preferred_mode === 'quick_view' && status?.can_use_quick_view) {
        activePreviewMode.value = 'quick_view'
      } else if (!status?.can_use_quick_view) {
        activePreviewMode.value = 'table_geojson'
      }
    }
  } catch (error) {
    if (seq === quickViewRequestSeq) {
      quickViewStatus.value = null
      quickViewLoadError.value = t('manager.spatialPreview.quickViewLoadFailed')
    }
    console.error('加载快显状态失败:', error)
  }
}

const handleSwitchQuickView = async () => {
  const target = spatialPreviewTarget.value
  if (!target) return
  quickViewActionLoading.value = true
  try {
    await quickViewAPI.updatePreferredModeByLocator(target.locator, 'quick_view')
    ElMessage.success(t('manager.spatialPreview.switchQuickViewSuccess'))
    await loadQuickViewStatus()
    if (quickViewStatus.value?.can_use_quick_view) {
      activePreviewMode.value = 'quick_view'
    }
  } catch (error) {
    console.error('切换快显失败:', error)
    ElMessage.error(t('manager.spatialPreview.switchQuickViewFailed'))
  } finally {
    quickViewActionLoading.value = false
  }
}

const handleBackToBasicPreview = async () => {
  const target = spatialPreviewTarget.value
  if (!target) return
  quickViewActionLoading.value = true
  try {
    await quickViewAPI.updatePreferredModeByLocator(target.locator, 'table_geojson')
    activePreviewMode.value = 'table_geojson'
    await loadQuickViewStatus()
  } catch (error) {
    console.error('返回基础预览失败:', error)
    activePreviewMode.value = 'table_geojson'
  } finally {
    quickViewActionLoading.value = false
  }
}

const handleGenerateTileCache = () => {
  const target = spatialPreviewTarget.value
  if (!target) return
  router.push({
    name: 'TileCache',
    query: {
      tab: 'tasks',
      create: '1',
      engine_id: String(target.engineId),
      schema: target.schema,
      table: target.table,
      ...(target.locator ? { locator: target.locator } : {}),
      ...(target.geometryColumn ? { geom: target.geometryColumn } : {}),
      ...(target.geometryColumns.length ? { geometry_columns: target.geometryColumns.join(',') } : {}),
      ...(target.sourceSRID > 0 ? { source_srid: String(target.sourceSRID) } : {}),
      ...(target.extentSRID > 0 ? { extent_srid: String(target.extentSRID) } : {}),
      ...(target.extent.length === 4 ? { extent: target.extent.join(',') } : {})
    }
  })
}

watch(
  () => spatialPreviewTarget.value
    ? `${spatialPreviewTarget.value.engineId}:${spatialPreviewTarget.value.schema}:${spatialPreviewTarget.value.table}:${spatialPreviewTarget.value.locator}:${spatialPreviewTarget.value.recordCount}`
    : '',
  loadQuickViewStatus,
  { immediate: true }
)

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

const objectContentMetadata = computed(() => {
  return objectData.value?.content?.metadata || {}
})

function metadataValue(key) {
  const value = objectContentMetadata.value?.[key]
  return value !== undefined && value !== null && value !== '' ? value : undefined
}

function mediaDurationSeconds() {
  const durationMS = metadataValue('duration_ms')
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

// 图片尺寸信息
const objectImageDimensions = computed(() => {
  if (!objectContentType.value.startsWith('image/')) {
    return null
  }

  const width = metadataValue('width')
  const height = metadataValue('height')

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

  // 图片特有信息
  if (contentType.startsWith('image/')) {
    // 图片尺寸（宽 高）
    const width = metadataValue('width')
    const height = metadataValue('height')
    if (width && height) {
      parts.push(`${t('manager.explorer.metaWidth')} ${width} ${t('manager.explorer.metaHeight')} ${height}`)
    }

    // 文件大小
    if (objectSizeBytes.value > 0) {
      parts.push(`${t('manager.explorer.metaFileSize')}: ${formatFileSize(objectSizeBytes.value)}`)
    }

    // 图片格式
    const format = metadataValue('encoding')
    if (format && format !== contentType.split('/')[1]) {
      parts.push(`${t('manager.explorer.metaFormat')}: ${format}`)
    }

    // 颜色模式
    const colorMode = metadataValue('color_space')
    if (colorMode) {
      parts.push(`${t('manager.explorer.metaColorMode')}: ${colorMode}`)
    }
  }

  // 视频特有信息
  if (contentType.includes('video')) {
    // 文件大小
    if (objectSizeBytes.value > 0) {
      parts.push(`${t('manager.explorer.metaFileSize')}: ${formatFileSize(objectSizeBytes.value)}`)
    }

    // 视频尺寸（宽 × 高）
    const width = metadataValue('width')
    const height = metadataValue('height')

    if (width && height) {
      parts.push(`${t('manager.explorer.metaResolution')}: ${width} × ${height}`)
    }

    // 时长
    const duration = mediaDurationSeconds()
    if (duration) {
      const durationStr = formatDuration(duration)
      parts.push(`${t('manager.explorer.metaDuration')}: ${durationStr}`)
    }

    const encoding = metadataValue('encoding')
    if (encoding) {
      parts.push(`${t('manager.explorer.metaEncoding')}: ${encoding}`)
    }
  }

  // 音频特有信息
  if (contentType.includes('audio')) {
    // 时长
    const duration = mediaDurationSeconds()
    if (duration) {
      const durationStr = formatDuration(duration)
      parts.push(`${t('manager.explorer.metaDuration')}: ${durationStr}`)
    }

    const encoding = metadataValue('encoding')
    if (encoding) {
      parts.push(`${t('manager.explorer.metaEncoding')}: ${encoding}`)
    }
  }

  // PDF 特有信息
  if (contentType.includes('pdf')) {
    const pages = metadataValue('page_count')
    if (pages) {
      parts.push(t('manager.explorer.metaPdfPages', { value: pages }))
    }

    const author = metadataValue('author')
    if (author) {
      parts.push(t('manager.explorer.metaPdfAuthor', { value: author }))
    }

    const title = metadataValue('title')
    if (title) {
      parts.push(t('manager.explorer.metaPdfTitle', { value: title }))
    }

    const creator = metadataValue('creator')
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

// 判断是否显示向量化按钮（支持向量化的对象、目录、前缀、Bucket）
const showVectorizeButton = computed(() => {
  if (!props.selectedNode) return false
  if (!props.previewData) return false

  const node = props.selectedNode

  // 检查预览模式（对象存储的节点预览模式是 'object' 或 'node'）
  const previewMode = props.previewData.mode || ''
  if (previewMode !== 'object' && previewMode !== 'node') {
    return false
  }

  return canShowVectorizeAction(node, selectedEmbeddingState.value, props.previewData)
})

const showVectorizedIndicator = computed(() => {
  if (!props.selectedNode) return false
  if (!props.previewData) return false
  return isVectorizableObjectNode(props.selectedNode, props.previewData) &&
    selectedEmbeddingState.value?.embedding?.status === 'ready'
})

// 向量化按钮文本
const vectorizeButtonText = computed(() => {
  if (!props.selectedNode) return t('manager.explorer.vectorize')

  const nodeType = normalizedNodeType(props.selectedNode)

  if (nodeType === 'object') {
    return t('manager.explorer.vectorize')
  } else if (isVectorizableRangeNode(props.selectedNode)) {
    return t('manager.explorer.batchVectorize')
  }

  return t('manager.explorer.vectorize')
})

// 处理向量化按钮点击
const handleVectorize = async () => {
  if (!props.selectedNode) return

  const node = props.selectedNode
  const nodeType = normalizedNodeType(node)

  // 创建通知实例
  let notification = null

  try {
    // 解析 locator 提取参数
    const locator = node.locator || node.id
    const loc = parseLocator(locator)
    let request
    if (nodeType === 'object') {
      if (!isVectorizableObjectNode(node, props.previewData) || !loc.itemId) {
        ElMessage.warning(t('manager.explorer.vectorizeSingleFileOnly'))
        return
      }
      request = {
        scope: 'item',
        target: {
          engine_id: loc.engineId,
          item_id: loc.itemId,
          locator
        }
      }
    } else if (isVectorizableRangeNode(node)) {
      if (!loc.nodeId) {
        ElMessage.warning(t('manager.explorer.batchVectorizeDirOnly'))
        return
      }
      request = {
        scope: 'node',
        target: {
          engine_id: loc.engineId,
          node_id: loc.nodeId,
          locator,
          recursive: true
        }
      }
    } else {
      ElMessage.warning(t('manager.explorer.batchVectorizeDirOnly'))
      return
    }

    const response = await client.post('/manager/embedding_executions', request)

    // 获取响应数据
    const responseData = response?.data || response
    console.log('向量化任务响应:', responseData)

    const executionId = responseData?.execution_id
    if (!executionId) {
      ElMessage.success(t('manager.explorer.vectorizeTaskSubmitted'))
      return
    }

    // 显示进度通知
    const targetName = node.label

    notification = ElNotification({
      title: t('manager.explorer.vectorizeInProgress'),
      message: t('manager.explorer.vectorizingTarget', { name: targetName }),
      type: 'info',
      duration: 0, // 不自动关闭
      position: 'bottom-right'
    })

    pollTaskStatus(executionId, notification, targetName)

  } catch (error) {
    console.error('向量化失败:', error)
    if (notification) {
      notification.close()
    }
    ElMessage.error(t('manager.explorer.vectorizeTaskFailed', { error: error.response?.data?.error || error.message }))
  }
}

// 轮询任务状态
const pollTaskStatus = async (executionId, notification, targetName, maxAttempts = 30) => {
  let attempts = 0
  const pollInterval = 2000 // 2秒

  const poll = async () => {
    try {
      attempts++

      const response = await client.get(`/manager/executions/${executionId}`)
      const payload = response?.data || response
      const data = payload?.data || payload

      console.log(`[轮询 ${attempts}/${maxAttempts}] execution 状态:`, data.status)

      if (data.status === 'success') {
        // 任务成功完成
        notification.close()
        await loadSelectedItemEmbeddingState()

        const metadata = data.metadata || {}
        const total = metadata.total || 0
        const generated = metadata.generated || 0
        const rebuilt = metadata.rebuilt || 0
        const skipped = metadata.ready_skipped || 0
        const failed = metadata.failed || 0
        const successMessage = total > 0
          ? t('manager.explorer.vectorizeBatchStats', {
              name: targetName,
              total,
              vectorized: generated + rebuilt,
              skipped,
              failed
            })
          : t('manager.explorer.vectorizeSuccess', { name: targetName })

        ElNotification({
          title: failed > 0 ? t('manager.explorer.vectorizeDoneWithFailed') : t('manager.explorer.vectorizeDone'),
          message: successMessage,
          type: failed > 0 ? 'warning' : 'success',
          duration: failed > 0 ? 10000 : 5000,
          position: 'bottom-right'
        })
        return
      }

      if (['failed', 'timeout', 'cancelled', 'canceled'].includes(data.status)) {
        // 任务失败
        notification.close()
        ElNotification({
          title: t('manager.explorer.vectorizeFailed2'),
          message: data.error_details?.message || data.error || data.message || t('manager.explorer.vectorizeUnknownError'),
          type: 'error',
          duration: 8000,
          position: 'bottom-right'
        })
        return
      }

      // 任务仍在运行
      if (data.status === 'running' || data.status === 'pending') {
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
  const refPath = path === combinedMultiRefValue ? '' : (path || '')
  activeMultiRefPath.value = path || combinedMultiRefValue
  emit('child-change', {
    childName: '',
    refPath,
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
  flex-wrap: wrap;
  justify-content: flex-end;
}

.quick-view-actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.quick-view-status {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
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

.quick-view-renderer {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
}

.preview-advisory {
  flex: 0 0 auto;
}

.preview-advisory-body {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
}

.preview-advisory-body span {
  min-width: 0;
  line-height: 1.5;
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

.graph-preview-layout {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-rows: minmax(360px, 46%) minmax(260px, 1fr);
  gap: 12px;
  overflow: hidden;
}

.graph-sample-panel {
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  background: var(--addp-bg-secondary);
  padding: 10px 12px;
}

.graph-sample-header {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 8px;
}

.graph-sample-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.graph-sample-count {
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.graph-sample-grid {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(360px, 1fr));
  gap: 12px;
}

.graph-sample-table {
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.graph-sample-subtitle {
  flex: 0 0 auto;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.graph-sample-table :deep(.el-table) {
  flex: 1;
  min-height: 0;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
}

.graph-overview-table {
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.graph-overview-hint {
  flex: 0 0 auto;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.graph-overview-table :deep(.el-table) {
  flex: 1;
  min-height: 0;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
}

.graph-overview-pagination {
  flex: 0 0 auto;
  display: flex;
  justify-content: flex-end;
}

.graph-overview-table :deep(.el-table__row) {
  cursor: pointer;
}

.graph-overview-table :deep(.active-graph-overview-row > td.el-table__cell) {
  background: var(--el-color-primary-light-9);
}

/* 强制覆盖 Element Plus Empty 组件的背景 */
.preview-panel :deep(.el-empty) {
  background: transparent !important;
}

.preview-panel :deep(.el-empty__description) {
  color: var(--addp-text-secondary) !important;
}
</style>
