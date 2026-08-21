<template>
  <el-table
    :data="executions"
    stripe
    style="width: 100%"
    @row-click="handleRowClick"
  >
    <el-table-column prop="id" :label="t('monitor.table.id')" width="80" />
    <el-table-column prop="module" :label="t('monitor.table.module')" width="120">
      <template #default="{ row }">
        <el-tag size="small">{{ row.module }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="source" :label="t('monitor.table.source')" width="120">
      <template #default="{ row }">
        <el-tag size="small" type="info">{{ row.source || '-' }}</el-tag>
      </template>
    </el-table-column>
    <el-table-column prop="task_type" :label="t('monitor.table.task_type')" width="140">
      <template #default="{ row }">
        {{ formatTaskType(row) }}
      </template>
    </el-table-column>
    <el-table-column prop="source_task_id" :label="t('monitor.table.source_task_id')" width="130">
      <template #default="{ row }">
        {{ row.source_task_id || '-' }}
      </template>
    </el-table-column>
    <el-table-column prop="status" :label="t('monitor.table.status')" width="100">
      <template #default="{ row }">
        <el-tag :type="getStatusType(row.status)" size="small">
          {{ getStatusText(row.status) }}
        </el-tag>
      </template>
    </el-table-column>
    <el-table-column :label="t('monitor.table.observation_signals')" width="150">
      <template #default="{ row }">
        <el-tag v-if="continuousPrimarySignal(row)" :type="continuousSignalTagType(continuousPrimarySignal(row).severity)" size="small">
          {{ continuousSignalText(continuousPrimarySignal(row).code) }}
          <template v-if="continuousSignals(row).length > 1"> +{{ continuousSignals(row).length - 1 }}</template>
        </el-tag>
        <span v-else>-</span>
      </template>
    </el-table-column>
    <el-table-column prop="trigger_type" :label="t('monitor.table.trigger_type')" width="110">
      <template #default="{ row }">
        {{ getTriggerText(row.trigger_type) }}
      </template>
    </el-table-column>
    <el-table-column prop="progress" :label="t('monitor.table.progress')" width="120">
      <template #default="{ row }">
        <el-tag v-if="hasContinuousExecutionMetadata(row.metadata)" size="small" type="info">
          {{ t('monitor.table.continuous') }}
        </el-tag>
        <el-progress v-else :percentage="row.progress || 0" :status="getProgressStatus(row.status)" />
      </template>
    </el-table-column>
    <el-table-column prop="created_at" :label="t('monitor.table.created_at')" width="180">
      <template #default="{ row }">
        {{ formatDate(row.created_at) }}
      </template>
    </el-table-column>
    <el-table-column prop="queue_duration_ms" :label="t('monitor.table.queue_duration')" width="120">
      <template #default="{ row }">
        {{ formatDuration(row.queue_duration_ms) }}
      </template>
    </el-table-column>
    <el-table-column prop="run_duration_ms" :label="t('monitor.table.duration')" width="120">
      <template #default="{ row }">
        {{ formatDuration(row.run_duration_ms) }}
      </template>
    </el-table-column>
    <el-table-column prop="attempt" :label="t('monitor.table.attempt')" width="100">
      <template #default="{ row }">{{ row.attempt || 0 }} / {{ row.max_attempts || '-' }}</template>
    </el-table-column>
    <el-table-column :label="t('monitor.table.actions')" width="100" fixed="right">
      <template #default="{ row }">
        <el-button text size="small" @click.stop="handleView(row)">
          {{ t('monitor.table.view_detail') }}
        </el-button>
      </template>
    </el-table-column>
  </el-table>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import { buildContinuousSignals, continuousSignalTagType, hasContinuousExecutionMetadata, resolveTaskTypeDisplayName } from '@common-ui'

const { t, te } = useI18n()

const props = defineProps({
  executions: {
    type: Array,
    default: () => []
  },
  taskProviders: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['view'])

function getStatusType(status) {
  const typeMap = {
    pending: 'info',
    running: 'warning',
    success: 'success',
    failed: 'danger',
    timeout: 'danger',
    cancelled: 'info'
  }
  return typeMap[status] || 'info'
}

function getStatusText(status) {
  const textMap = {
    pending: t('monitor.execution.status.pending'),
    running: t('monitor.execution.status.running'),
    success: t('monitor.execution.status.success'),
    failed: t('monitor.execution.status.failed'),
    timeout: t('monitor.execution.status.timeout'),
    cancelled: t('monitor.execution.status.cancelled'),
  }
  return textMap[status] || status
}

function continuousSignals(row) {
  return buildContinuousSignals(row?.metadata, row?.status)
}

function continuousPrimarySignal(row) {
  return continuousSignals(row)[0] || null
}

function continuousSignalText(code) {
  if (!code) return '-'
  const key = `monitor.execution.detail.continuous.signals.${code}.title`
  return te(key) ? t(key) : code
}

function getTriggerText(triggerType) {
  const textMap = {
    manual: t('monitor.execution.trigger.manual'),
    scheduled: t('monitor.execution.trigger.scheduled'),
    event: t('monitor.execution.trigger.event')
  }
  return textMap[triggerType] || triggerType || '-'
}

function formatTaskType(row) {
  const taskType = row?.task_type
  if (!taskType) return '-'
  const capabilityName = resolveTaskTypeDisplayName(props.taskProviders, row?.module, taskType)
  if (capabilityName) return capabilityName
  const key = `monitor.execution.task_type_names.${taskType}`
  return te(key) ? t(key) : taskType
}

function getProgressStatus(status) {
  if (status === 'success') return 'success'
  if (status === 'failed' || status === 'timeout') return 'exception'
  return undefined
}

function formatDate(date) {
  if (!date) return '-'
  return new Date(date).toLocaleString('zh-CN')
}

function formatDuration(ms) {
  if (ms === null || ms === undefined) return '-'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`
  return `${(ms / 60000).toFixed(2)}min`
}

function handleRowClick(row) {
  // 可选：点击行也触发查看详情
}

function handleView(row) {
  emit('view', row)
}
</script>

<style scoped>
.el-table {
  cursor: pointer;
}
</style>
