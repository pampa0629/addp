<template>
  <div v-if="sections.length" class="extracted-metadata">
    <div
      v-for="section in sections"
      :key="section.title"
      class="extracted-section"
      :class="section.className"
    >
      <h4>
        <i v-if="section.icon" :class="section.icon" class="section-icon"></i>
        {{ section.title }}
      </h4>
      <div
        v-for="entry in section.entries"
        :key="`${section.title}-${entry.key}`"
        class="extracted-entry"
      >
        <span class="entry-key">{{ entry.label }}</span>
        <span v-if="!entry.isBlock" class="entry-value" :class="entry.valueClass">
          {{ entry.value }}
        </span>
        <pre v-else class="entry-block">{{ entry.value }}</pre>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = defineProps({
  metadata: {
    type: Object,
    default: () => ({})
  }
})

// 节标题映射（响应式，语言切换后自动更新）
const sectionTitleMap = computed(() => ({
  basic_info: t('extractedMetadata.sections.basic_info'),
  schema_info: t('extractedMetadata.sections.schema_info'),
  stats: t('extractedMetadata.sections.stats'),
  technical: t('extractedMetadata.sections.technical'),
  quality: t('extractedMetadata.sections.quality'),
  custom_attrs: t('extractedMetadata.sections.custom_attrs')
}))

// 自定义属性节配置（响应式）
const customAttrTitleMap = computed(() => ({
  video_metadata: { title: t('extractedMetadata.sections.video_metadata'), icon: 'el-icon-video-camera', className: 'section-video' },
  audio_metadata: { title: t('extractedMetadata.sections.audio_metadata'), icon: 'el-icon-microphone', className: 'section-audio' },
  geo_metadata: { title: t('extractedMetadata.sections.geo_metadata'), icon: 'el-icon-location', className: 'section-geo' },
  geojson_metadata: { title: t('extractedMetadata.sections.geojson_metadata'), icon: 'el-icon-map-location', className: 'section-geo' },
  image_metadata: { title: t('extractedMetadata.sections.image_metadata'), icon: 'el-icon-picture', className: 'section-image' },
  image_classification: { title: t('extractedMetadata.sections.image_classification'), icon: 'el-icon-collection-tag', className: 'section-image' },
  pdf_metadata: { title: t('extractedMetadata.sections.pdf_metadata'), icon: 'el-icon-document', className: 'section-document' },
  document_metadata: { title: t('extractedMetadata.sections.document_metadata'), icon: 'el-icon-reading', className: 'section-document' },
  text_metadata: { title: t('extractedMetadata.sections.text_metadata'), icon: 'el-icon-tickets', className: 'section-text' },
  table_metadata: { title: t('extractedMetadata.sections.table_metadata'), icon: 'el-icon-s-grid', className: 'section-table' },
  csv_metadata: { title: t('extractedMetadata.sections.csv_metadata'), icon: 'el-icon-s-grid', className: 'section-table' },
  sqlite_metadata: { title: t('extractedMetadata.sections.sqlite_metadata'), icon: 'el-icon-coin', className: 'section-database' },
  excel_metadata: { title: t('extractedMetadata.sections.excel_metadata'), icon: 'el-icon-s-grid', className: 'section-table' }
}))

// 字段名称本地化映射（响应式）
const fieldLabelMap = computed(() => ({
  // 图像相关
  width: t('extractedMetadata.fields.width'),
  height: t('extractedMetadata.fields.height'),
  format: t('extractedMetadata.fields.format'),
  color_space: t('extractedMetadata.fields.color_space'),
  aspect_ratio: t('extractedMetadata.fields.aspect_ratio'),
  resolution: t('extractedMetadata.fields.resolution'),
  megapixels: t('extractedMetadata.fields.megapixels'),
  size_category: t('extractedMetadata.fields.size_category'),
  orientation: t('extractedMetadata.fields.orientation'),
  likely_icon: t('extractedMetadata.fields.likely_icon'),
  likely_banner: t('extractedMetadata.fields.likely_banner'),
  // 视频相关
  duration: t('extractedMetadata.fields.duration'),
  codec: t('extractedMetadata.fields.codec'),
  bitrate: t('extractedMetadata.fields.bitrate'),
  frame_rate: t('extractedMetadata.fields.frame_rate'),
  audio_codec: t('extractedMetadata.fields.audio_codec'),
  audio_channels: t('extractedMetadata.fields.audio_channels'),
  has_subtitles: t('extractedMetadata.fields.has_subtitles'),
  container: t('extractedMetadata.fields.container'),
  // PDF/文档相关
  version: t('extractedMetadata.fields.version'),
  page_count: t('extractedMetadata.fields.page_count'),
  title: t('extractedMetadata.fields.title'),
  author: t('extractedMetadata.fields.author'),
  subject: t('extractedMetadata.fields.subject'),
  keywords: t('extractedMetadata.fields.keywords'),
  creator: t('extractedMetadata.fields.creator'),
  producer: t('extractedMetadata.fields.producer'),
  is_encrypted: t('extractedMetadata.fields.is_encrypted'),
  has_forms: t('extractedMetadata.fields.has_forms'),
  // GeoJSON/空间相关
  type: t('extractedMetadata.fields.type'),
  feature_count: t('extractedMetadata.fields.feature_count'),
  geometry_types: t('extractedMetadata.fields.geometry_types'),
  bbox: t('extractedMetadata.fields.bbox'),
  crs: t('extractedMetadata.fields.crs'),
  properties_sample: t('extractedMetadata.fields.properties_sample'),
  // CSV/表格相关
  row_count: t('extractedMetadata.fields.row_count'),
  column_count: t('extractedMetadata.fields.column_count'),
  columns: t('extractedMetadata.fields.columns'),
  has_header: t('extractedMetadata.fields.has_header'),
  delimiter: t('extractedMetadata.fields.delimiter'),
  sheet_count: t('extractedMetadata.fields.sheet_count'),
  sheets: t('extractedMetadata.fields.sheets'),
  default_sheet: t('extractedMetadata.fields.default_sheet'),
  column_types: t('extractedMetadata.fields.column_types'),
  rows_truncated: t('extractedMetadata.fields.rows_truncated'),
  // SQLite相关
  table_count: t('extractedMetadata.fields.table_count'),
  tables: t('extractedMetadata.fields.tables'),
  total_rows: t('extractedMetadata.fields.total_rows'),
  file_size: t('extractedMetadata.fields.file_size'),
  file_size_human: t('extractedMetadata.fields.file_size_human'),
  // 通用
  file_extension: t('extractedMetadata.fields.file_extension'),
  is_streaming_ready: t('extractedMetadata.fields.is_streaming_ready'),
  extractor_available: t('extractedMetadata.fields.extractor_available')
}))

const normalizeKey = (key) => {
  const labels = fieldLabelMap.value
  if (labels[key]) return labels[key]
  if (!key) return ''
  return key
    .replace(/[_\-]+/g, ' ')
    .replace(/\b\w/g, (match) => match.toUpperCase())
}

const formatBoolean = (value) => (value ? t('extractedMetadata.values.yes') : t('extractedMetadata.values.no'))

// 格式化时长(秒 -> 时:分:秒)
const formatDuration = (seconds) => {
  if (typeof seconds !== 'number' || seconds < 0) return seconds

  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = seconds % 60

  if (hours > 0) {
    return `${hours}:${minutes.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`
  }
  return `${minutes}:${secs.toString().padStart(2, '0')}`
}

// 格式化比特率
const formatBitrate = (kbps) => {
  if (typeof kbps !== 'number') return kbps
  if (kbps >= 1000) {
    return `${(kbps / 1000).toFixed(1)} Mbps`
  }
  return `${kbps} Kbps`
}

// 格式化帧率
const formatFrameRate = (fps) => {
  if (typeof fps !== 'number') return fps
  return `${fps.toFixed(1)} fps`
}

// 格式化像素数
const formatMegapixels = (mp) => {
  if (typeof mp !== 'number') return mp
  return `${mp.toFixed(1)} MP`
}

// 智能格式化值
const formatValue = (key, value) => {
  if (value === null || value === undefined || value === '') {
    return '—'
  }

  if (typeof value === 'boolean') {
    return formatBoolean(value)
  }

  if (key === 'duration' && typeof value === 'number') {
    return formatDuration(value)
  }

  if (key === 'bitrate' && typeof value === 'number') {
    return formatBitrate(value)
  }

  if (key === 'frame_rate' && typeof value === 'number') {
    return formatFrameRate(value)
  }

  if (key === 'megapixels' && typeof value === 'number') {
    return formatMegapixels(value)
  }

  if (key === 'audio_channels' && typeof value === 'number') {
    const channelMap = {
      1: t('extractedMetadata.values.mono'),
      2: t('extractedMetadata.values.stereo'),
      6: t('extractedMetadata.values.surround51'),
      8: t('extractedMetadata.values.surround71')
    }
    return channelMap[value] || t('extractedMetadata.values.channelN', { n: value })
  }

  if (key === 'orientation') {
    const orientationMap = {
      landscape: t('extractedMetadata.values.landscape'),
      portrait: t('extractedMetadata.values.portrait'),
      square: t('extractedMetadata.values.square')
    }
    return orientationMap[value] || value
  }

  if (key === 'size_category') {
    const sizeMap = {
      thumbnail: t('extractedMetadata.values.thumbnail'),
      small: t('extractedMetadata.values.small'),
      medium: t('extractedMetadata.values.medium'),
      large: t('extractedMetadata.values.large'),
      very_large: t('extractedMetadata.values.very_large')
    }
    return sizeMap[value] || value
  }

  if (typeof value === 'number') {
    return value.toLocaleString()
  }

  if (typeof value === 'string') {
    return value
  }

  return value
}

const toEntries = (data) => {
  if (!data || typeof data !== 'object' || Array.isArray(data)) {
    return []
  }
  return Object.entries(data).map(([key, value]) => {
    const formattedValue = formatValue(key, value)
    const isSimple = typeof formattedValue === 'string' || typeof formattedValue === 'number'

    return {
      key,
      label: normalizeKey(key),
      value: isSimple ? formattedValue : JSON.stringify(value, null, 2),
      isBlock: !isSimple,
      valueClass: typeof value === 'boolean' ? 'value-boolean' : ''
    }
  })
}

const buildSections = (metadata) => {
  if (!metadata || typeof metadata !== 'object') {
    return []
  }

  const sections = []
  const usedKeys = new Set()
  const sectionTitles = sectionTitleMap.value
  const customAttrTitles = customAttrTitleMap.value

  const pushSection = (key, titleOrConfig, data) => {
    if (!data || typeof data !== 'object') return
    const entries = toEntries(data)
    if (!entries.length) return

    const config = typeof titleOrConfig === 'string'
      ? { title: titleOrConfig }
      : titleOrConfig

    sections.push({
      key,
      title: config.title || sectionTitles[key] || normalizeKey(key),
      icon: config.icon,
      className: config.className,
      entries
    })
    usedKeys.add(key)
  }

  pushSection('basic_info', sectionTitles.basic_info, metadata.basic_info)
  pushSection('technical', sectionTitles.technical, metadata.technical)
  pushSection('stats', sectionTitles.stats, metadata.stats)
  pushSection('schema_info', sectionTitles.schema_info, metadata.schema_info)
  pushSection('quality', sectionTitles.quality, metadata.quality)

  const customAttrs = metadata.custom_attrs
  if (customAttrs && typeof customAttrs === 'object') {
    Object.entries(customAttrs).forEach(([key, value]) => {
      const content = value && typeof value === 'object' && value.data ? value.data : value
      const entries = toEntries(content)
      if (!entries.length) {
        return
      }

      const config = customAttrTitles[key] || { title: normalizeKey(key) }
      sections.push({
        key: `custom_${key}`,
        title: config.title || normalizeKey(key),
        icon: config.icon,
        className: config.className,
        entries
      })
    })
    usedKeys.add('custom_attrs')
  }

  Object.entries(metadata).forEach(([key, value]) => {
    if (usedKeys.has(key)) return
    if (!value || typeof value !== 'object') return
    const entries = toEntries(value)
    if (!entries.length) return

    const config = customAttrTitles[key] || sectionTitles[key]
    if (typeof config === 'object') {
      sections.push({
        key,
        title: config.title || normalizeKey(key),
        icon: config.icon,
        className: config.className,
        entries
      })
    } else {
      sections.push({
        key,
        title: config || normalizeKey(key),
        entries
      })
    }
  })

  return sections
}

const sections = computed(() => buildSections(props.metadata))
</script>

<style scoped>
.extracted-metadata {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.extracted-section {
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  padding: 12px 16px;
  background: var(--el-fill-color);
  transition: all 0.3s ease;
}

.extracted-section:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
}

.extracted-section h4 {
  margin: 0 0 12px;
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  display: flex;
  align-items: center;
  gap: 8px;
}

.section-icon {
  font-size: 16px;
}

/* 不同类型的section样式 */
.section-video h4 { color: var(--el-color-primary); }
.section-video .section-icon { color: var(--el-color-primary); }
.section-video { border-left: 3px solid var(--el-color-primary); }

.section-audio h4 { color: var(--el-color-success); }
.section-audio .section-icon { color: var(--el-color-success); }
.section-audio { border-left: 3px solid var(--el-color-success); }

.section-image h4 { color: var(--el-color-warning); }
.section-image .section-icon { color: var(--el-color-warning); }
.section-image { border-left: 3px solid var(--el-color-warning); }

.section-document h4 { color: var(--el-color-danger); }
.section-document .section-icon { color: var(--el-color-danger); }
.section-document { border-left: 3px solid var(--el-color-danger); }

.section-geo h4 { color: var(--addp-text-tertiary); }
.section-geo .section-icon { color: var(--addp-text-tertiary); }
.section-geo { border-left: 3px solid var(--addp-text-tertiary); }

.section-table h4 { color: var(--addp-text-secondary); }
.section-table .section-icon { color: var(--addp-text-secondary); }
.section-table { border-left: 3px solid var(--addp-text-secondary); }

.section-database h4 { color: var(--addp-text-primary); }
.section-database .section-icon { color: var(--addp-text-primary); }
.section-database { border-left: 3px solid var(--addp-text-primary); }

.section-text h4 { color: var(--el-color-primary-light-3); }
.section-text .section-icon { color: var(--el-color-primary-light-3); }
.section-text { border-left: 3px solid var(--el-color-primary-light-3); }

.extracted-entry {
  display: grid;
  grid-template-columns: 120px 1fr;
  gap: 12px;
  align-items: start;
  font-size: 13px;
  padding: 8px 0;
  border-top: 1px dashed var(--el-border-color-extra-light);
}

.extracted-entry:first-of-type {
  border-top: none;
  padding-top: 0;
}

.entry-key {
  font-weight: 500;
  color: var(--el-text-color-secondary);
  text-align: right;
  padding-right: 12px;
  word-break: keep-all;
}

.entry-value {
  color: var(--el-text-color-primary);
  font-weight: 500;
}

.value-boolean {
  font-weight: 600;
}

.entry-block {
  grid-column: 1 / -1;
  margin: 0;
  padding: 12px;
  background: var(--el-fill-color-lighter);
  border-radius: 4px;
  border: 1px solid var(--el-border-color-extra-light);
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--el-text-color-regular);
  max-height: 300px;
  overflow: auto;
}

@media (max-width: 768px) {
  .extracted-entry {
    grid-template-columns: 1fr;
    gap: 4px;
  }

  .entry-key {
    text-align: left;
    padding-right: 0;
  }
}
</style>
