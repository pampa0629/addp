<template>
  <div class="object-catalog-preview" :style="{ gridTemplateRows: metaHeight + 'px 8px 1fr' }">
    <!-- 元数据区域 -->
    <div class="object-meta">
      <div class="meta-row">
        <span class="meta-label">{{ bucketLabel }}</span>
        <span class="meta-value">{{ rootDisplayValue }}</span>
      </div>
      <div class="meta-row">
        <span class="meta-label">{{ t('objectStorage.path') }}</span>
        <span class="meta-value">{{ objectData.path || '/' }}</span>
      </div>
      <div class="meta-row">
        <span class="meta-label">{{ t('objectStorage.type') }}</span>
        <span class="meta-value">{{ getObjectNodeTypeLabel(objectData.node_type, t) }}</span>
      </div>
      <div class="meta-row">
        <span class="meta-label">{{ t('objectStorage.size') }}</span>
        <span class="meta-value">{{ formatBytes(objectData.size_bytes ?? objectData.sizeBytes) }}</span>
      </div>
      <div
        v-if="objectCount !== null && objectCount !== undefined"
        class="meta-row"
      >
        <span class="meta-label">{{ t('objectStorage.objectCount') }}</span>
        <span class="meta-value">{{ objectCount }}</span>
      </div>
      <div class="meta-row">
        <span class="meta-label">{{ t('objectStorage.contentType') }}</span>
        <span class="meta-value">{{ objectData.content_type || objectData.contentType || '-' }}</span>
      </div>
      <div class="meta-row">
        <span class="meta-label">{{ t('objectStorage.lastModified') }}</span>
        <span class="meta-value">{{ formatDateTime(objectData.last_modified || objectData.lastModified) }}</span>
      </div>
      <div v-if="metadataEntries.length" class="meta-row meta-metadata">
        <span class="meta-label">{{ t('objectStorage.metadata') }}</span>
        <div class="meta-value metadata-list">
          <div
            v-for="([key, value]) in metadataEntries"
            :key="key"
            class="meta-kv"
          >
            <span class="meta-meta-key">{{ key }}</span>
            <span class="meta-meta-value">{{ value }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 可拖拽分隔器 -->
    <div class="meta-splitter" @mousedown="startResize"></div>

    <!-- 子对象列表或文件内容 -->
    <div v-if="isDirectory" class="object-children">
      <el-table
        :data="children"
        height="100%"
        @row-dblclick="handleRowDblclick"
      >
        <el-table-column prop="name" :label="t('objectStorage.colName')" show-overflow-tooltip />
        <el-table-column :label="t('objectStorage.colType')" width="120">
          <template #default="{ row }">
            {{ getObjectNodeTypeLabel(row.type, t) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('objectStorage.colSize')" width="160">
          <template #default="{ row }">
            <span v-if="row.type !== 'prefix'">{{ formatBytes(row.size_bytes) }}</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('objectStorage.colContentType')" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.content_type || '-' }}
          </template>
        </el-table-column>
        <el-table-column :label="t('objectStorage.colLastModified')" width="200">
          <template #default="{ row }">
            {{ formatDateTime(row.last_modified) }}
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 文件内容预览 -->
    <div v-else-if="isTableMaterial" class="object-table-material">
      <el-table :data="tableRows" height="100%">
        <el-table-column
          v-for="column in tableColumns"
          :key="column"
          :prop="column"
          :label="column"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            {{ formatTableCell(row[column]) }}
          </template>
        </el-table-column>
      </el-table>
    </div>
    <div v-else class="object-content">
      <component
        :is="contentPreview"
        v-if="contentPreview && objectData.content"
        :data="data"
        v-bind="contentComponentProps"
      />
      <div v-else class="placeholder">{{ t('objectStorage.noContent') }}</div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useResizable } from '../../composables/useResizable'
import { formatBytes, formatDateTime, getObjectNodeTypeLabel } from '../../utils/formatters'
import ImagePreview from '../ImagePreview.vue'
import JsonPreview from './JsonPreview.vue'
import TextPreview from './TextPreview.vue'
import MarkdownPreview from './MarkdownPreview.vue'
import ContainerPreview from './ContainerPreview.vue'
import UnsupportedPreview from './UnsupportedPreview.vue'

const props = defineProps({
  data: {
    type: Object,
    required: true
  },
  geojsonPreviewComponent: {
    type: Object,
    default: null
  }
})

const emit = defineEmits(['navigate'])

const { t } = useI18n()
const { size: metaHeight, startResize } = useResizable(140, 80, 300, 'vertical')

const objectData = computed(() => props.data?.object || {})

const bucketLabel = computed(() => {
  const type = (objectData.value.node_type || '').toLowerCase()
  if (type === 'bucket') return t('objectStorage.bucket')
  if (type === 'schema') return t('objectStorage.schema')
  if (type === 'database') return t('objectStorage.database')
  return t('objectStorage.rootDir')
})

const rootDisplayValue = computed(() => {
  const value = objectData.value.bucket
  if (value === null || value === undefined || value === '') return '/'
  return value
})

const isDirectory = computed(() => {
  const type = (objectData.value.node_type || '').toLowerCase()
  return type === 'directory' || type === 'prefix' || type === 'bucket' || type === 'schema' || type === 'database'
})

const objectCount = computed(() => {
  return objectData.value.object_count ?? objectData.value.objectCount
})

const children = computed(() => {
  return (objectData.value.children || []).map((child) => ({
    ...child,
    type: (child.type || '').toLowerCase(),
    size_bytes: child.size_bytes ?? child.sizeBytes ?? 0,
    content_type: child.content_type ?? child.contentType ?? '',
    last_modified: child.last_modified ?? child.lastModified ?? null
  }))
})

const metadataEntries = computed(() => {
  return Object.entries(objectData.value.metadata || {})
})

const contentJSON = computed(() => {
  const content = objectData.value.content || {}
  return content.json || content.JSON || null
})

const contentSemantic = computed(() => {
  const content = objectData.value.content || {}
  const metadata = content.metadata || {}
  return {
    kind: String(content.kind || '').toLowerCase(),
    material: String(
      content.preview_material ||
        content.previewMaterial ||
        metadata.preview_material ||
        metadata.previewMaterial ||
        ''
    ).toLowerCase(),
    renderer: String(
      content.frontend_renderer ||
        content.frontendRenderer ||
        metadata.frontend_renderer ||
        metadata.frontendRenderer ||
        ''
    ).toLowerCase()
  }
})

const isTableMaterial = computed(() => {
  const { kind, material, renderer } = contentSemantic.value
  return (renderer || material || kind) === 'table'
})

const isContainerMaterial = computed(() => {
  const { kind, material, renderer } = contentSemantic.value
  return (renderer || material || kind) === 'container'
})

const tableRows = computed(() => {
  const value = contentJSON.value
  return Array.isArray(value?.rows) ? value.rows : []
})

const tableColumns = computed(() => {
  const value = contentJSON.value
  const columns = Array.isArray(value?.columns) ? value.columns : []
  const names = columns
    .map((column) => {
      if (typeof column === 'string') return column
      return column?.name || column?.field || column?.key || ''
    })
    .filter(Boolean)
  if (names.length > 0) {
    return names
  }
  const firstRow = tableRows.value[0]
  return firstRow && typeof firstRow === 'object' ? Object.keys(firstRow) : []
})

const contentPreview = computed(() => {
  if (!objectData.value.content) return null

  const { kind, material, renderer } = contentSemantic.value

  switch (renderer || material || kind) {
    case 'container':
      return ContainerPreview
    case 'unsupported':
      return UnsupportedPreview
    case 'image':
      return ImagePreview
    case 'map':
    case 'geojson':
      return props.geojsonPreviewComponent || JsonPreview
    case 'json':
      return JsonPreview
    case 'markdown':
      return MarkdownPreview
    case 'table':
      return null
    default:
      return TextPreview
  }
})

const containerSummaryItems = computed(() => {
  const summary = contentJSON.value?.summary || {}
  const items = []
  const pushNumber = (label, value) => {
    const number = Number(value)
    if (Number.isFinite(number)) {
      items.push({ label, value: new Intl.NumberFormat().format(number) })
    }
  }
  pushNumber(t('containerPreview.rawEntries'), summary.raw_child_count ?? summary.child_count)
  pushNumber(t('containerPreview.previewableChildren'), summary.visible_child_count ?? summary.sampled_children)
  pushNumber(t('containerPreview.filtered'), summary.filtered_child_count ?? summary.ignored_child_count)
  const size = Number(summary.size_bytes ?? objectData.value.size_bytes ?? objectData.value.sizeBytes)
  if (Number.isFinite(size) && size >= 0) {
    items.push({ label: t('objectStorage.size'), value: formatBytes(size) })
  }
  return items
})

const containerChildren = computed(() => {
  const list = Array.isArray(contentJSON.value?.children) ? contentJSON.value.children : []
  return list.map((child, index) => {
    const columns = Array.isArray(child?.columns)
      ? child.columns
      : Array.isArray(child?.headers)
        ? child.headers
        : []
    return {
      key: child?.key || child?.name || child?.table || String(index),
      name: child?.name || child?.key || String(index),
      label: child?.label || child?.name || child?.table || child?.key || String(index),
      childKind: child?.child_kind || child?.childKind || 'child',
      dataType: child?.data_type || child?.dataType || 'unknown',
      format: child?.format || '',
      layout: child?.layout || '',
      rowCount: Number(child?.row_count ?? child?.rowCount) || undefined,
      columnCount: Number(child?.column_count ?? child?.columnCount ?? columns.length) || undefined,
      hasHeader: child?.has_header ?? child?.hasHeader,
      refs: Array.isArray(child?.refs) ? child.refs : [],
      columns,
      columnTypes: Array.isArray(child?.column_types) ? child.column_types : child?.columnTypes || [],
      rows: Array.isArray(child?.rows) ? child.rows : []
    }
  })
})

const containerDefaultChildKey = computed(() => {
  return contentJSON.value?.active_child || contentJSON.value?.default_child || containerChildren.value[0]?.key || ''
})

const containerTruncated = computed(() => {
  const summary = contentJSON.value?.summary || {}
  return Boolean(objectData.value.content?.truncated || summary.children_truncated)
})

const contentComponentProps = computed(() => {
  if (isContainerMaterial.value) {
    return {
      summaryItems: containerSummaryItems.value,
      children: containerChildren.value,
      defaultChildKey: containerDefaultChildKey.value,
      selectorLabel: t('containerPreview.childSelector'),
      truncated: containerTruncated.value,
      emptyText: t('containerPreview.empty')
    }
  }
  return {}
})

const formatTableCell = (value) => {
  if (value === null || value === undefined) return ''
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

const handleRowDblclick = (row) => {
  emit('navigate', row)
}
</script>

<style scoped>
.object-catalog-preview {
  display: grid;
  grid-template-rows: 140px 8px 1fr;
  gap: 0;
  flex: 1;
  overflow: hidden;
}

.object-meta {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px 16px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  padding: 12px;
  background: var(--el-fill-color-lighter);
  overflow-y: auto;
}

.meta-row {
  display: flex;
  gap: 12px;
  font-size: 13px;
  line-height: 1.4;
}

.meta-row.meta-metadata {
  grid-column: 1 / -1;
}

.meta-row.meta-extracted {
  grid-column: 1 / -1;
  flex-direction: column;
  align-items: stretch;
}

.meta-row.meta-extracted .meta-label {
  width: auto;
  font-weight: 600;
  margin-bottom: 4px;
}

.extracted-wrapper {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.meta-label {
  width: 96px;
  color: var(--el-text-color-secondary);
  flex-shrink: 0;
}

.meta-value {
  flex: 1;
  color: var(--el-text-color-primary);
  word-break: break-all;
}

.meta-splitter {
  height: 8px;
  cursor: row-resize;
  position: relative;
  margin: 0;
}

.meta-splitter::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  height: 2px;
  background: var(--el-color-primary-light-8);
  border-radius: 2px;
}

.meta-splitter:hover::after,
body.is-v-resizing .meta-splitter::after {
  background: var(--el-color-primary);
}

.metadata-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.meta-kv {
  background: var(--el-fill-color);
  border-radius: 4px;
  padding: 4px 8px;
  font-size: 12px;
  display: flex;
  gap: 4px;
  align-items: center;
}

.meta-meta-key {
  font-weight: 500;
  color: var(--el-text-color-regular);
}

.meta-meta-value {
  color: var(--el-text-color-secondary);
}

.object-children {
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  padding: 12px;
  background: var(--el-fill-color-lighter);
  flex: 1;
  min-height: 220px;
  overflow: hidden;
}

.object-children :deep(.el-table) {
  height: 100%;
}

.object-content {
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  padding: 12px;
  min-height: 220px;
  background: var(--el-fill-color-lighter);
  overflow: auto;
  position: relative;
}

.object-table-material {
  border: 1px solid var(--el-border-color-light);
  border-radius: 6px;
  min-height: 220px;
  overflow: hidden;
}

.placeholder {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  text-align: center;
  padding: 24px;
}
</style>
