<template>
  <div class="tabular-result-renderer">
    <el-table
      ref="tableRef"
      v-loading="loading"
      :data="rows"
      :border="border"
      :stripe="stripe"
      :height="height || undefined"
      :max-height="maxHeight || undefined"
      :size="size || undefined"
      :row-key="rowKey || undefined"
      :current-row-key="currentRowKey || undefined"
      :highlight-current-row="highlightCurrentRow"
      class="result-table"
      @row-click="selectRow"
    >
      <el-table-column
        v-for="column in visibleColumns"
        :key="column.key"
        :prop="column.key"
        :label="column.label"
        :width="column.width"
        :min-width="column.minWidth || columnMinWidth"
        :show-overflow-tooltip="!hasStructuredColumnValues(column)"
      >
        <template #default="scope">
          <button
            v-if="isStructuredResultValue(cellValue(scope.row, column))"
            type="button"
            class="structured-cell"
            @click.stop="openStructuredCell(scope.row, column)"
            @dblclick.stop="copyCellValue(cellValue(scope.row, column))"
          >
            <span class="structured-cell__kind">{{ structuredValueKind(cellValue(scope.row, column)) }}</span>
            <span class="structured-cell__summary">{{ structuredValueSummary(cellValue(scope.row, column)) }}</span>
          </button>
          <span
            v-else
            class="scalar-cell"
            :class="cellClass(cellValue(scope.row, column))"
            :title="cellTitle(cellValue(scope.row, column), column)"
            :tabindex="copyOnDblclick ? 0 : undefined"
            @dblclick="copyCellValue(cellValue(scope.row, column), column)"
            @keydown.enter="copyCellValue(cellValue(scope.row, column), column)"
          >
            <span>{{ presentCell(cellValue(scope.row, column), column).text }}</span>
            <span
              v-if="presentCell(cellValue(scope.row, column), column).state"
              class="state-indicator"
              :class="`state--${presentCell(cellValue(scope.row, column), column).state.tone}`"
            >{{ presentCell(cellValue(scope.row, column), column).state.label }}</span>
          </span>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog
      v-model="structuredDialogVisible"
      :title="structuredDialogTitle"
      width="min(720px, calc(100vw - 24px))"
      class="addp-dialog"
      destroy-on-close
    >
      <pre class="structured-value-json">{{ structuredDialogJSON }}</pre>
      <template #footer>
        <el-button @click="copyText(structuredDialogJSON)">{{ t('common.copy') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import {
  formatResultCell,
  isStructuredResultValue,
  normalizeTabularColumns,
  presentResultCell,
  resultSelectionFromRow,
  safeStructuredResultJSON,
  tabularCellValue
} from '../utils/tabularResult'

const props = defineProps({
  rows: { type: Array, default: () => [] },
  columns: { type: Array, default: () => [] },
  fields: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  height: { type: [String, Number], default: '' },
  maxHeight: { type: [String, Number], default: '' },
  size: { type: String, default: '' },
  border: { type: Boolean, default: false },
  stripe: { type: Boolean, default: true },
  rowKey: { type: [String, Function], default: '' },
  currentRowKey: { type: [String, Number], default: '' },
  highlightCurrentRow: { type: Boolean, default: false },
  columnMinWidth: { type: Number, default: 140 },
  nullText: { type: String, default: '—' },
  copyOnDblclick: { type: Boolean, default: false },
  presentations: { type: Array, default: () => [] }
})
const emit = defineEmits(['result-select', 'row-click'])
const { t, locale } = useI18n()
const tableRef = ref(null)
const structuredDialogVisible = ref(false)
const structuredDialogTitle = ref('')
const structuredDialogJSON = ref('')

function selectRow(row, column, event) {
  emit('row-click', row, column, event)
  const selection = resultSelectionFromRow(props.rows, row)
  if (selection) emit('result-select', selection)
}

const visibleColumns = computed(() => normalizeTabularColumns({
  columns: props.columns,
  fields: props.fields,
  rows: props.rows,
  presentations: props.presentations
}))

const cellValue = (row, column) => tabularCellValue(row, column)
const formatCell = (value, column) => formatResultCell(value, props.nullText, column?.presentation, locale.value)
const presentCell = (value, column) => presentResultCell(value, props.nullText, column?.presentation, locale.value)
const cellTitle = (value, column) => {
  const presented = presentCell(value, column)
  return presented.state ? `${presented.text} · ${presented.state.label}` : presented.text
}
const hasStructuredColumnValues = column => props.rows.some(row => isStructuredResultValue(cellValue(row, column)))
const cellClass = value => ({
  'is-null': value === null || value === undefined,
  'is-number': typeof value === 'number'
})

const structuredValueKind = value => Array.isArray(value)
  ? t('tabularResult.array')
  : t('tabularResult.object')

const structuredValueSummary = (value) => {
  if (Array.isArray(value)) {
    if (value.length === 0) return t('tabularResult.emptyArray')
    return t('tabularResult.arraySummary', { count: value.length })
  }
  const keys = Object.keys(value || {})
  if (keys.length === 0) return t('tabularResult.emptyObject')
  const previewKeys = keys.slice(0, 3).join(', ')
  return keys.length > 3
    ? t('tabularResult.objectSummaryMore', { count: keys.length, keys: previewKeys })
    : t('tabularResult.objectSummary', { count: keys.length, keys: previewKeys })
}

const openStructuredCell = (row, column) => {
  structuredDialogTitle.value = column.label
  structuredDialogJSON.value = safeStructuredResultJSON(cellValue(row, column))
  structuredDialogVisible.value = true
}

const copyText = async (value) => {
  try {
    await navigator.clipboard.writeText(String(value ?? ''))
    ElMessage.success(t('common.copySuccess'))
  } catch {
    ElMessage.error(t('common.copyFailed'))
  }
}

const copyCellValue = (value, column) => {
  if (!props.copyOnDblclick) return
  copyText(isStructuredResultValue(value) ? safeStructuredResultJSON(value) : formatCell(value, column))
}

defineExpose({
  setCurrentRow: row => tableRef.value?.setCurrentRow(row)
})
</script>

<style scoped>
.tabular-result-renderer {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
  width: 100%;
}

.result-table {
  flex: 1 1 auto;
  min-height: 0;
  width: 100%;
}

.scalar-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  vertical-align: bottom;
  white-space: nowrap;
  color: var(--addp-text-primary);
}

.state-indicator {
  flex: 0 0 auto;
  padding: 1px 6px;
  border: 1px solid currentColor;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 500;
  line-height: 18px;
}

.state--info { color: var(--el-color-info); background: var(--el-color-info-light-9); }
.state--success { color: var(--el-color-success); background: var(--el-color-success-light-9); }
.state--warning { color: var(--el-color-warning); background: var(--el-color-warning-light-9); }
.state--danger { color: var(--el-color-danger); background: var(--el-color-danger-light-9); }

.scalar-cell.is-null {
  color: var(--addp-text-tertiary);
  font-style: italic;
}

.scalar-cell.is-number {
  color: var(--el-color-primary);
}

.structured-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 100%;
  height: 24px;
  padding: 0 8px;
  border: 1px solid var(--addp-border-color-light);
  border-radius: 4px;
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  cursor: pointer;
  font: inherit;
  line-height: 1;
  vertical-align: middle;
}

.structured-cell:hover,
.structured-cell:focus-visible {
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
  outline: none;
}

.structured-cell__kind {
  flex: 0 0 auto;
  font-size: 11px;
  font-weight: 600;
  color: var(--el-color-primary);
}

.structured-cell__summary {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}

.structured-value-json {
  max-height: min(62vh, 620px);
  margin: 0;
  padding: 12px;
  overflow: auto;
  border: 1px solid var(--addp-border-color-light);
  border-radius: 4px;
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
