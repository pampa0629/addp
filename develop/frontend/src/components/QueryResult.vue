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
      <span
        v-if="result.effect !== 'read' && result.rows_affected !== undefined && result.rows_affected !== null"
        class="summary-item"
      >
        {{ t('develop.queryResult.rowsAffected') }} <strong>{{ result.rows_affected }}</strong>
      </span>
      <span v-if="result.execution_time_ms !== undefined" class="summary-item">
        {{ t('develop.queryResult.executionTime') }} <strong>{{ result.execution_time_ms }}ms</strong>
      </span>

      <el-tooltip
        v-if="result.truncated"
        :content="truncatedMessage"
        placement="top"
      >
        <el-tag class="truncated-status" type="warning" effect="plain" size="small">
          <el-icon><WarningFilled /></el-icon>
          <span class="truncated-status__text">{{ truncatedMessage }}</span>
        </el-tag>
      </el-tooltip>

      <div class="summary-actions">
        <el-tooltip v-if="result.execution_id" :content="t('develop.queryResult.executionDetail')">
          <el-button circle size="small" :aria-label="t('develop.queryResult.executionDetail')" @click="openExecution">
            <el-icon><View /></el-icon>
          </el-button>
        </el-tooltip>
        <el-button
          v-if="canExportFullResult"
          size="small"
          :aria-label="t('develop.queryResult.exportFull')"
          @click="emit('export-full')"
        >
          <el-icon><Download /></el-icon>
          {{ t('develop.queryResult.exportFull') }}
        </el-button>
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
      v-if="result && result.success && result.rows_count === 0"
      class="result-alert"
      type="warning"
      :title="noDataHint"
      :closable="false"
      show-icon
    />

    <el-alert
      v-if="result && result.success === false && errorMessage"
      class="result-alert"
      type="error"
      :title="errorMessage"
      :closable="false"
      show-icon
    />

    <div v-if="customContent" class="custom-result-content">
      <slot />
    </div>

    <div v-else-if="hasRows" class="result-grid">
      <div class="result-table">
        <TabularResultRenderer
          :rows="pagedRows"
          :columns="tableColumns"
          height="100%"
          null-text="NULL"
          copy-on-dblclick
        />
      </div>

      <DataPagination
        v-if="showPagination"
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        class="result-pagination"
        :page-sizes="pageSizeOptions"
        :total="loadedRowCount"
        :pager-count="5"
        size="small"
      />
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

  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CircleCloseFilled,
  Download,
  Loading,
  SuccessFilled,
  View,
  WarningFilled
} from '@element-plus/icons-vue'
import {
  DataPagination,
  openMonitorExecution,
  paginateRows,
  TabularResultRenderer
} from '@addp/common-frontend'
import { queryErrorMessage } from '@/utils/queryWorkbench.mjs'

const { t } = useI18n()
const emit = defineEmits(['export-full'])
const props = defineProps({
  result: {
    type: Object,
    default: null
  },
  customContent: {
    type: Boolean,
    default: false
  },
  fullExportSupported: {
    type: Boolean,
    default: false
  }
})

const pageSizeOptions = Object.freeze([10, 20, 50, 100])
const currentPage = ref(1)
const pageSize = ref(20)
const isRunning = computed(() => ['pending', 'running'].includes(props.result?.status))
const hasRows = computed(() => Array.isArray(props.result?.rows) && props.result.rows.length > 0)
const canExportFullResult = computed(() => Boolean(
  props.result?.success
  && props.result?.result_kind === 'table'
  && props.result?.execution_id
  && props.fullExportSupported
))
const loadedRowCount = computed(() => hasRows.value ? props.result.rows.length : 0)
const showPagination = computed(() => loadedRowCount.value > pageSizeOptions[0])
const pagedRows = computed(() => paginateRows(props.result?.rows, currentPage.value, pageSize.value))
const statusType = computed(() => {
  if (isRunning.value) return 'primary'
  if (props.result?.success && Number(props.result?.rows_count || 0) === 0) return 'warning'
  return props.result?.success ? 'success' : 'danger'
})
const statusIcon = computed(() => {
  if (isRunning.value) return Loading
  if (props.result?.success && Number(props.result?.rows_count || 0) === 0) return View
  return props.result?.success ? SuccessFilled : CircleCloseFilled
})
const statusLabel = computed(() => {
  if (isRunning.value) return t('develop.queryResult.running')
  if (props.result?.success && Number(props.result?.rows_count || 0) === 0) return t('develop.queryResult.successNoData')
  return props.result?.success ? t('develop.queryResult.success') : t('develop.queryResult.failed')
})
const errorMessage = computed(() => queryErrorMessage(props.result?.error_code, props.result?.error, t))
const noDataHint = computed(() => t('develop.queryResult.noDataHint'))
const truncatedMessage = computed(() => t('develop.queryResult.truncated', {
  limit: props.result?.result_limit
}))

watch(() => props.result?.rows, () => {
  currentPage.value = 1
})

const tableColumns = computed(() => (props.result?.columns || []).map(column => ({
  key: column,
  label: column,
  minWidth: Math.max(140, Math.min(320, String(column).length * 12 + 72))
})))

const openExecution = () => openMonitorExecution(props.result.execution_id)

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

.truncated-status {
  min-width: 0;
  max-width: min(360px, 45%);
  flex: 0 1 auto;
}

.truncated-status .el-icon {
  flex: 0 0 auto;
  margin-right: 4px;
}

.truncated-status__text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
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
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 8px 12px 12px;
}

.result-table {
  flex: 1;
  min-height: 120px;
  display: flex;
  overflow: hidden;
}

.result-pagination {
  flex: 0 0 auto;
  align-self: flex-end;
  max-width: 100%;
  overflow-x: auto;
}

.custom-result-content {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.el-empty {
  flex: 1;
  min-height: 0;
}
</style>
