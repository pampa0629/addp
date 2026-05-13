<template>
  <ContainerPreview
    :summary-items="summaryItems"
    :children="children"
    :default-child-key="defaultChildKey"
    :selector-label="t('excelPreview.sheetSelector')"
    :active-child-preview="resolvedActiveChildPreview"
    :active-child-loading="resolvedActiveChildLoading"
    :truncated="childrenTruncated"
    :empty-text="t('excelPreview.parseError')"
    @child-change="handleChildChange"
    @page-change="handlePageChange"
  />
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ContainerPreview from './ContainerPreview.vue'

const { t } = useI18n()

const props = defineProps({
  data: {
    type: Object,
    default: () => ({})
  },
  activeChildPreview: {
    type: Object,
    default: null
  },
  activeChildLoading: {
    type: Boolean,
    default: false
  },
})

const emit = defineEmits(['child-change', 'page-change'])

const content = computed(() => props.data?.object?.content ?? {})
const json = computed(() => content.value?.json ?? {})
const summary = computed(() => json.value?.summary ?? {})
const resolvedActiveChildPreview = computed(() => props.activeChildPreview)
const resolvedActiveChildLoading = computed(() => props.activeChildLoading)
const sheets = computed(() => {
  const list = json.value?.sheets
  return Array.isArray(list) ? list : []
})

const defaultSheetName = computed(() => json.value?.active_sheet || json.value?.default_sheet || '')

const defaultChildKey = computed(() => {
  const target = sheets.value.find(sheet => sheet.name === defaultSheetName.value) || sheets.value[0]
  return target ? sheetKey(target) : ''
})

const children = computed(() => {
  return sheets.value.map(sheet => ({
    key: sheetKey(sheet),
    name: sheet.name || `Sheet ${Number(sheet.index ?? 0) + 1}`,
    label: sheet.name || `Sheet ${Number(sheet.index ?? 0) + 1}`,
    kind: 'sheet',
    dataType: 'table',
    rowCount: numberOrUndefined(sheet.row_count),
    columnCount: numberOrUndefined(sheet.column_count),
    hasHeader: !!sheet.has_header,
    columns: Array.isArray(sheet.headers) ? sheet.headers : [],
    columnTypes: Array.isArray(sheet.column_types) ? sheet.column_types : [],
    rows: Array.isArray(sheet.rows) ? sheet.rows : []
  }))
})

const childrenTruncated = computed(() => Boolean(summary.value?.children_truncated || summary.value?.sheets_truncated))

const summaryItems = computed(() => {
  const meta = summary.value || {}
  const items = [
    { label: t('excelPreview.totalSheets'), value: formatNumber(numberOrDefault(meta.sheet_count, sheets.value.length)) },
    { label: t('excelPreview.loadedSheets'), value: formatNumber(numberOrDefault(meta.sampled_sheets, sheets.value.length)) }
  ]
  const sizeBytes = numberOrUndefined(meta.size_bytes)
  if (sizeBytes !== undefined) {
    items.push({ label: t('excelPreview.fileSize'), value: formatBytes(sizeBytes) })
  }
  return items
})

const sheetKey = (sheet) => {
  if (sheet?.name) return sheet.name
  return String(sheet?.index ?? '')
}

const numberOrUndefined = (value) => {
  const number = Number(value)
  return Number.isFinite(number) ? number : undefined
}

const numberOrDefault = (value, fallback) => {
  const number = numberOrUndefined(value)
  return number === undefined ? fallback : number
}

const formatNumber = (value) => {
  if (typeof value !== 'number' || Number.isNaN(value)) return '—'
  return new Intl.NumberFormat().format(value)
}

const formatBytes = (bytes) => {
  if (typeof bytes !== 'number' || Number.isNaN(bytes) || bytes < 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = bytes
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index++
  }
  const digits = index === 0 ? 0 : 2
  return `${size.toFixed(digits)} ${units[index]}`
}

const handleChildChange = (child) => {
  if (!child?.name) return
  emit('child-change', child.name)
}

const handlePageChange = (page) => {
  emit('page-change', page)
}
</script>
