<template>
  <div class="query-result">
    <div v-if="result" class="result-summary">
      <el-tag :type="statusType" effect="plain" size="small">
        <el-icon><component :is="statusIcon" /></el-icon>
        {{ statusLabel }}
      </el-tag>

      <span v-if="result.rows_count !== undefined" class="summary-item">
        {{ t('develop.queryResult.rowsCount') }} <strong>{{ result.rows_count }}</strong>
      </span>
      <span v-if="result.rows_affected !== undefined" class="summary-item">
        {{ t('develop.queryResult.rowsAffected') }} <strong>{{ result.rows_affected }}</strong>
      </span>
      <span v-if="result.execution_time_ms !== undefined" class="summary-item">
        {{ t('develop.queryResult.executionTime') }} <strong>{{ result.execution_time_ms }}ms</strong>
      </span>

      <div class="summary-actions">
        <el-tooltip v-if="result.execution_id" :content="t('develop.queryResult.executionDetail')">
          <el-button circle size="small" :aria-label="t('develop.queryResult.executionDetail')" @click="openExecution">
            <el-icon><View /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip v-if="hasRows" :content="t('develop.queryResult.exportCsv')">
          <el-button circle size="small" type="primary" :aria-label="t('develop.queryResult.exportCsv')" @click="exportCSV">
            <el-icon><Download /></el-icon>
          </el-button>
        </el-tooltip>
      </div>
    </div>

    <el-progress
      v-if="isRunning"
      class="execution-progress"
      :percentage="result.progress || 0"
      :indeterminate="!result.progress"
      :duration="2"
    />

    <el-alert
      v-if="result?.truncated"
      class="result-alert"
      type="warning"
      :title="t('develop.queryResult.truncated', { limit: result.result_limit })"
      :closable="false"
      show-icon
    />

    <el-alert
      v-if="result && result.success === false && result.error"
      class="result-alert"
      type="error"
      :title="result.error"
      :closable="false"
      show-icon
    />

    <div v-if="customContent" class="custom-result-content">
      <slot />
    </div>

    <div v-else-if="hasRows" class="result-grid">
      <el-auto-resizer>
        <template #default="{ height, width }">
          <el-table-v2
            :columns="tableColumns"
            :data="result.rows"
            :width="width"
            :height="height"
            fixed
          />
        </template>
      </el-auto-resizer>
    </div>

    <el-empty
      v-else-if="!result"
      :description="t('develop.queryResult.emptyHint')"
      :image-size="96"
    />
    <el-empty
      v-else-if="isRunning"
      :description="t('develop.query.executing')"
      :image-size="72"
    />
    <el-empty
      v-else-if="result.success"
      :description="t('develop.queryResult.noData')"
      :image-size="72"
    />

    <el-dialog v-model="jsonVisible" title="JSON" width="min(680px, calc(100vw - 24px))" class="addp-dialog">
      <pre class="json-value">{{ jsonValue }}</pre>
      <template #footer>
        <el-button @click="copyText(jsonValue)">
          <el-icon><CopyDocument /></el-icon>
          {{ t('develop.queryResult.copy') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, h, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import {
  CircleCloseFilled,
  CopyDocument,
  Download,
  Loading,
  SuccessFilled,
  View
} from '@element-plus/icons-vue'
import { openMonitorExecution } from '@addp/common-frontend'
import { buildQueryResultCSV } from '@/utils/queryWorkbench.mjs'

const { t } = useI18n()
const props = defineProps({
  result: {
    type: Object,
    default: null
  },
  customContent: {
    type: Boolean,
    default: false
  }
})

const jsonVisible = ref(false)
const jsonValue = ref('')
const isRunning = computed(() => ['pending', 'running'].includes(props.result?.status))
const hasRows = computed(() => Array.isArray(props.result?.rows) && props.result.rows.length > 0)
const statusType = computed(() => {
  if (isRunning.value) return 'primary'
  return props.result?.success ? 'success' : 'danger'
})
const statusIcon = computed(() => {
  if (isRunning.value) return Loading
  return props.result?.success ? SuccessFilled : CircleCloseFilled
})
const statusLabel = computed(() => {
  if (isRunning.value) return t('develop.queryResult.running')
  return props.result?.success ? t('develop.queryResult.success') : t('develop.queryResult.failed')
})

const formatValue = (value) => {
  if (value === null) return 'NULL'
  if (value === undefined) return ''
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

const openJSON = (value) => {
  if (value === null || typeof value !== 'object') return
  jsonValue.value = JSON.stringify(value, null, 2)
  jsonVisible.value = true
}

const copyText = async (value) => {
  try {
    await navigator.clipboard.writeText(String(value ?? ''))
    ElMessage.success(t('develop.queryResult.copySuccess'))
  } catch (error) {
    ElMessage.error(t('develop.queryResult.copyFailed') + error.message)
  }
}

const tableColumns = computed(() => (props.result?.columns || []).map(column => ({
  key: column,
  dataKey: column,
  title: column,
  width: Math.max(140, Math.min(320, String(column).length * 12 + 72)),
  cellRenderer: ({ cellData }) => h('span', {
    class: ['result-cell', {
      'is-null': cellData === null,
      'is-number': typeof cellData === 'number',
      'is-object': cellData !== null && typeof cellData === 'object'
    }],
    title: formatValue(cellData),
    tabindex: 0,
    onClick: () => openJSON(cellData),
    onDblclick: () => copyText(formatValue(cellData)),
    onKeydown: event => {
      if (event.key === 'Enter') openJSON(cellData)
    }
  }, formatValue(cellData))
})))

const openExecution = () => openMonitorExecution(props.result.execution_id)

const exportCSV = () => {
  if (!hasRows.value) {
    ElMessage.warning(t('develop.queryResult.noExportData'))
    return
  }
  try {
    const csv = buildQueryResultCSV(props.result.columns, props.result.rows)
    const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8;' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = `query_result_${Date.now()}.csv`
    link.click()
    URL.revokeObjectURL(link.href)
    ElMessage.success(t('develop.queryResult.exportSuccess'))
  } catch (error) {
    ElMessage.error(t('develop.queryResult.exportFailed') + error.message)
  }
}
</script>

<style scoped>
.query-result {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-primary);
}

.result-summary {
  min-height: 44px;
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 6px 12px;
  border-bottom: 1px solid var(--addp-border-color);
  flex-wrap: wrap;
}

.summary-item {
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.summary-item strong {
  margin-left: 4px;
  color: var(--addp-text-primary);
}

.summary-actions {
  display: flex;
  gap: 6px;
  margin-left: auto;
}

.execution-progress {
  width: 100%;
}

.result-alert {
  margin: 8px 12px 0;
}

.result-grid {
  flex: 1;
  min-height: 160px;
  padding: 8px 12px 12px;
}

.custom-result-content {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

:deep(.result-cell) {
  display: block;
  width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--addp-text-primary);
  cursor: default;
}

:deep(.result-cell.is-null) {
  color: var(--addp-text-tertiary);
  font-style: italic;
}

:deep(.result-cell.is-number) {
  color: var(--el-color-primary);
}

:deep(.result-cell.is-object) {
  cursor: pointer;
  color: var(--el-color-success);
}

.json-value {
  max-height: 60vh;
  margin: 0;
  padding: 12px;
  overflow: auto;
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  white-space: pre-wrap;
  word-break: break-word;
}

.el-empty {
  flex: 1;
  min-height: 0;
}
</style>
