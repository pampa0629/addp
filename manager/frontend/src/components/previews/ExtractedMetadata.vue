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

const props = defineProps({
  metadata: {
    type: Object,
    default: () => ({})
  }
})

const sectionTitleMap = {
  basic_info: '基础信息',
  schema_info: '结构信息',
  stats: '统计信息',
  technical: '技术信息',
  quality: '质量信息',
  custom_attrs: '自定义属性'
}

const customAttrTitleMap = {
  video_metadata: { title: '视频信息', icon: 'el-icon-video-camera', className: 'section-video' },
  audio_metadata: { title: '音频信息', icon: 'el-icon-microphone', className: 'section-audio' },
  geo_metadata: { title: '空间信息', icon: 'el-icon-location', className: 'section-geo' },
  geojson_metadata: { title: '地理数据', icon: 'el-icon-map-location', className: 'section-geo' },
  image_metadata: { title: '图像信息', icon: 'el-icon-picture', className: 'section-image' },
  image_classification: { title: '图像分类', icon: 'el-icon-collection-tag', className: 'section-image' },
  pdf_metadata: { title: 'PDF 信息', icon: 'el-icon-document', className: 'section-document' },
  document_metadata: { title: '文档信息', icon: 'el-icon-reading', className: 'section-document' },
  text_metadata: { title: '文本信息', icon: 'el-icon-tickets', className: 'section-text' },
  table_metadata: { title: '表格信息', icon: 'el-icon-s-grid', className: 'section-table' },
  csv_metadata: { title: 'CSV 信息', icon: 'el-icon-s-grid', className: 'section-table' },
  sqlite_metadata: { title: 'SQLite 信息', icon: 'el-icon-coin', className: 'section-database' },
  excel_metadata: { title: 'Excel 信息', icon: 'el-icon-s-grid', className: 'section-table' }
}

// 字段名称本地化映射
const fieldLabelMap = {
  // 图像相关
  width: '宽度',
  height: '高度',
  format: '格式',
  color_space: '色彩空间',
  aspect_ratio: '宽高比',
  resolution: '分辨率',
  megapixels: '像素数(MP)',
  size_category: '尺寸分类',
  orientation: '方向',
  likely_icon: '可能是图标',
  likely_banner: '可能是横幅',

  // 视频相关
  duration: '时长',
  codec: '视频编码',
  bitrate: '比特率',
  frame_rate: '帧率',
  audio_codec: '音频编码',
  audio_channels: '声道数',
  has_subtitles: '字幕',
  container: '容器格式',

  // PDF/文档相关
  version: '版本',
  page_count: '页数',
  title: '标题',
  author: '作者',
  subject: '主题',
  keywords: '关键词',
  creator: '创建工具',
  producer: '生成器',
  is_encrypted: '加密',
  has_forms: '包含表单',

  // GeoJSON/空间相关
  type: '类型',
  feature_count: '要素数量',
  geometry_types: '几何类型',
  bbox: '边界框',
  crs: '坐标系统',
  properties_sample: '属性示例',

  // CSV/表格相关
  row_count: '行数',
  column_count: '列数',
  columns: '列名',
  has_header: '包含表头',
  delimiter: '分隔符',
  sheet_count: '工作表数量',
  sheets: '工作表',
  default_sheet: '默认工作表',
  column_types: '列类型',
  rows_truncated: '示例截断',

  // SQLite相关
  table_count: '表数量',
  tables: '表列表',
  total_rows: '总行数',
  file_size: '文件大小',
  file_size_human: '文件大小',

  // 通用
  file_extension: '文件扩展名',
  is_streaming_ready: '支持流式传输',
  extractor_available: '元数据提取器'
}

const normalizeKey = (key) => {
  // 优先使用预定义的标签
  if (fieldLabelMap[key]) {
    return fieldLabelMap[key]
  }

  if (!key) return ''
  return key
    .replace(/[_\-]+/g, ' ')
    .replace(/\b\w/g, (match) => match.toUpperCase())
}

const formatBoolean = (value) => (value ? '是' : '否')

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

  // 特殊字段格式化
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
    const channelMap = { 1: '单声道', 2: '立体声', 6: '5.1环绕声', 8: '7.1环绕声' }
    return channelMap[value] || `${value} 声道`
  }

  if (key === 'orientation') {
    const orientationMap = { landscape: '横向', portrait: '纵向', square: '正方形' }
    return orientationMap[value] || value
  }

  if (key === 'size_category') {
    const sizeMap = {
      thumbnail: '缩略图',
      small: '小图',
      medium: '中图',
      large: '大图',
      very_large: '超大图'
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

  const pushSection = (key, titleOrConfig, data) => {
    if (!data || typeof data !== 'object') return
    const entries = toEntries(data)
    if (!entries.length) return

    const config = typeof titleOrConfig === 'string'
      ? { title: titleOrConfig }
      : titleOrConfig

    sections.push({
      key,
      title: config.title || sectionTitleMap[key] || normalizeKey(key),
      icon: config.icon,
      className: config.className,
      entries
    })
    usedKeys.add(key)
  }

  pushSection('basic_info', sectionTitleMap.basic_info, metadata.basic_info)
  pushSection('technical', sectionTitleMap.technical, metadata.technical)
  pushSection('stats', sectionTitleMap.stats, metadata.stats)
  pushSection('schema_info', sectionTitleMap.schema_info, metadata.schema_info)
  pushSection('quality', sectionTitleMap.quality, metadata.quality)

  const customAttrs = metadata.custom_attrs
  if (customAttrs && typeof customAttrs === 'object') {
    Object.entries(customAttrs).forEach(([key, value]) => {
      const content = value && typeof value === 'object' && value.data ? value.data : value
      const entries = toEntries(content)
      if (!entries.length) {
        return
      }

      const config = customAttrTitleMap[key] || { title: normalizeKey(key) }
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

    const config = customAttrTitleMap[key] || sectionTitleMap[key]
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
.section-video h4 { color: #409eff; }
.section-video .section-icon { color: #409eff; }
.section-video { border-left: 3px solid #409eff; }

.section-audio h4 { color: #67c23a; }
.section-audio .section-icon { color: #67c23a; }
.section-audio { border-left: 3px solid #67c23a; }

.section-image h4 { color: #e6a23c; }
.section-image .section-icon { color: #e6a23c; }
.section-image { border-left: 3px solid #e6a23c; }

.section-document h4 { color: #f56c6c; }
.section-document .section-icon { color: #f56c6c; }
.section-document { border-left: 3px solid #f56c6c; }

.section-geo h4 { color: var(--addp-text-tertiary); }
.section-geo .section-icon { color: var(--addp-text-tertiary); }
.section-geo { border-left: 3px solid var(--addp-text-tertiary); }

.section-table h4 { color: var(--addp-text-secondary); }
.section-table .section-icon { color: var(--addp-text-secondary); }
.section-table { border-left: 3px solid var(--addp-text-secondary); }

.section-database h4 { color: var(--addp-text-primary); }
.section-database .section-icon { color: var(--addp-text-primary); }
.section-database { border-left: 3px solid var(--addp-text-primary); }

.section-text h4 { color: #79bbff; }
.section-text .section-icon { color: #79bbff; }
.section-text { border-left: 3px solid #79bbff; }

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
