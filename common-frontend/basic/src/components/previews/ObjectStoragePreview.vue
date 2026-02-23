<template>
  <div class="object-storage-preview" :style="{ gridTemplateRows: metaHeight + 'px 8px 1fr' }">
    <!-- 元数据区域 -->
    <div class="object-meta">
      <div class="meta-row">
        <span class="meta-label">Bucket</span>
        <span class="meta-value">{{ objectData.bucket || '-' }}</span>
      </div>
      <div class="meta-row">
        <span class="meta-label">路径</span>
        <span class="meta-value">{{ objectData.path || '/' }}</span>
      </div>
      <div class="meta-row">
        <span class="meta-label">类型</span>
        <span class="meta-value">{{ getObjectNodeTypeLabel(objectData.node_type) }}</span>
      </div>
      <div class="meta-row">
        <span class="meta-label">大小</span>
        <span class="meta-value">{{ formatBytes(objectData.size_bytes ?? objectData.sizeBytes) }}</span>
      </div>
      <div
        v-if="objectCount !== null && objectCount !== undefined"
        class="meta-row"
      >
        <span class="meta-label">对象数量</span>
        <span class="meta-value">{{ objectCount }}</span>
      </div>
      <div class="meta-row">
        <span class="meta-label">Content-Type</span>
        <span class="meta-value">{{ objectData.content_type || objectData.contentType || '-' }}</span>
      </div>
      <div class="meta-row">
        <span class="meta-label">更新时间</span>
        <span class="meta-value">{{ formatDateTime(objectData.last_modified || objectData.lastModified) }}</span>
      </div>
      <div v-if="metadataEntries.length" class="meta-row meta-metadata">
        <span class="meta-label">元数据</span>
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
      <div v-if="hasExtractedMetadata" class="meta-row meta-extracted">
        <span class="meta-label">提取元数据</span>
        <div class="meta-value extracted-wrapper">
          <ExtractedMetadata :metadata="extractedMetadata" />
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
        <el-table-column prop="name" label="名称" show-overflow-tooltip />
        <el-table-column label="类型" width="120">
          <template #default="{ row }">
            {{ getObjectNodeTypeLabel(row.type) }}
          </template>
        </el-table-column>
        <el-table-column label="大小" width="160">
          <template #default="{ row }">
            <span v-if="row.type !== 'prefix'">{{ formatBytes(row.size_bytes) }}</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="内容类型" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.content_type || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="更新时间" width="200">
          <template #default="{ row }">
            {{ formatDateTime(row.last_modified) }}
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 文件内容预览 -->
    <div v-else class="object-content">
      <component
        :is="contentPreview"
        v-if="contentPreview && objectData.content"
        :data="data"
      />
      <div v-else class="placeholder">暂无可用内容</div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useResizable } from '../../composables/useResizable'
import { formatBytes, formatDateTime, getObjectNodeTypeLabel } from '../../utils/formatters'
import ImagePreview from '../ImagePreview.vue'
import JsonPreview from './JsonPreview.vue'
import TextPreview from './TextPreview.vue'
import ExtractedMetadata from '../ExtractedMetadata.vue'

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

const { size: metaHeight, startResize } = useResizable(140, 80, 300, 'vertical')

const parseMaybeJSON = (value) => {
  if (typeof value === 'string') {
    try {
      return JSON.parse(value)
    } catch (error) {
      console.warn('对象存储预览: JSON 解析失败', error)
      return null
    }
  }
  return value
}

const objectData = computed(() => props.data?.object || {})

const isDirectory = computed(() => {
  const type = (objectData.value.node_type || '').toLowerCase()
  return type === 'directory' || type === 'prefix' || type === 'bucket'
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

const extractedMetadata = computed(() => {
  // Debug: 打印原始数据
  console.log('[DEBUG] objectData.value:', objectData.value)
  console.log('[DEBUG] objectData.value.attributes:', objectData.value?.attributes)
  console.log('[DEBUG] objectData.value.extracted_metadata:', objectData.value?.extracted_metadata)

  // 1. 优先使用 extracted_metadata 字段（如果存在）
  const direct = parseMaybeJSON(objectData.value?.extracted_metadata)
  if (direct && typeof direct === 'object') {
    console.log('[DEBUG] Found direct extracted_metadata:', direct)
    return direct
  }

  // 2. 从 attributes 中提取所有 *_metadata 字段（如 video_metadata, audio_metadata 等）
  const attrs = parseMaybeJSON(objectData.value?.attributes) || objectData.value?.attributes
  console.log('[DEBUG] Parsed attributes:', attrs)

  if (!attrs || typeof attrs !== 'object') {
    console.log('[DEBUG] No valid attributes found')
    return null
  }

  // 检查是否有 extracted_metadata 字段
  const nested = parseMaybeJSON(attrs.extracted_metadata)
  if (nested && typeof nested === 'object') {
    console.log('[DEBUG] Found nested extracted_metadata:', nested)
    return nested
  }

  // 3. 自动识别所有以 _metadata 结尾的字段，构造为 custom_attrs 格式
  const customAttrs = {}
  let hasMetadata = false

  for (const [key, value] of Object.entries(attrs)) {
    if (key.endsWith('_metadata')) {
      console.log(`[DEBUG] Found metadata field: ${key}`, value)
      const parsed = parseMaybeJSON(value)
      if (parsed && typeof parsed === 'object') {
        // 如果元数据有 data 字段（Meta的标准格式），提取data
        customAttrs[key] = parsed.data || parsed
        hasMetadata = true
        console.log(`[DEBUG] Added to customAttrs:`, customAttrs[key])
      }
    }
  }

  // 如果找到了元数据字段，返回 custom_attrs 包装格式（供 ExtractedMetadata 组件使用）
  const result = hasMetadata ? { custom_attrs: customAttrs } : null
  console.log('[DEBUG] Final extractedMetadata result:', result)
  return result
})

const hasExtractedMetadata = computed(() => {
  if (!extractedMetadata.value) return false
  return Object.keys(extractedMetadata.value || {}).length > 0
})

const contentPreview = computed(() => {
  if (!objectData.value.content) return null

  const kind = (objectData.value.content.kind || '').toLowerCase()

  switch (kind) {
    case 'image':
      return ImagePreview
    case 'geojson':
      return props.geojsonPreviewComponent || JsonPreview
    case 'json':
      return JsonPreview
    default:
      return TextPreview
  }
})

const handleRowDblclick = (row) => {
  emit('navigate', row)
}
</script>

<style scoped>
.object-storage-preview {
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
body.is-resizing .meta-splitter::after {
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

.placeholder {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  text-align: center;
  padding: 24px;
}
</style>
