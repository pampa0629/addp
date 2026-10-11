<template>
  <div class="execution-detail">
    <MonitorExecutionsButton
      v-if="authStore.hasPermission('monitor.execution.read')"
      module="transfer"
      task-type="sync"
      :source-task-id="execution.task_id"
      scope="task"
      style="margin-bottom: 20px;"
    />
    <el-card v-loading="loading">
      <template #header>{{ t('transfer.executionDetail.executionDetailTitle', { id: execution.execution_id }) }}</template>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="t('transfer.executionDetail.executionId')">{{ execution.execution_id }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.executionDetail.taskId')">{{ execution.task_id }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.executionDetail.status')">
          <el-tag :type="getStatusType(execution.status)">{{ execution.status }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('transfer.executionDetail.triggerType')">{{ execution.trigger_type }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.executionDetail.recordsRead')">{{ execution.records_read }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.executionDetail.recordsWritten')">{{ execution.records_written }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.executionDetail.startTime')">{{ execution.start_time }}</el-descriptions-item>
        <el-descriptions-item :label="t('transfer.executionDetail.endTime')">{{ execution.end_time || '-' }}</el-descriptions-item>
        <el-descriptions-item v-if="execution.error_msg" :label="t('transfer.executionDetail.errorMessage')" :span="2">
          <div class="execution-error-message" data-testid="execution-error-message">{{ execution.error_msg }}</div>
        </el-descriptions-item>
      </el-descriptions>

      <template v-if="isContinuousExecution">
        <el-divider>{{ t('transfer.executionDetail.continuousProgress') }}</el-divider>

        <el-alert
          v-if="recoveryAlertVisible"
          :title="recoveryStateText"
          :description="recoveryDescription"
          :type="recoveryAlertType"
          :closable="false"
          show-icon
          class="diagnostics-alert"
        />

        <el-descriptions :column="3" border>
          <el-descriptions-item :label="t('transfer.executionDetail.workerInstance')">
            {{ continuousMetadata.owner_instance_id || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.executionDetail.fencingToken')">
            {{ continuousMetadata.fencing_token ?? '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.executionDetail.heartbeatAt')">
            {{ formatTimestamp(continuousMetadata.heartbeat_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.executionDetail.leaseUntil')">
            {{ formatTimestamp(continuousMetadata.lease_until) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.executionDetail.partitionCount')">
            {{ continuousPartitions.length }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.executionDetail.retentionHealth')">
            <el-tag :type="continuousHealthTagType(continuousDiagnostics.health || 'unknown')">
              {{ continuousHealthText(continuousDiagnostics.health || 'unknown') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.executionDetail.diagnosticsSampledAt')">
            {{ formatTimestamp(continuousDiagnostics.sampled_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.executionDetail.lastCommittedAt')">
            {{ formatTimestamp(continuousMetadata.last_committed_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.executionDetail.lastEventAt')">
            {{ formatTimestamp(continuousMetadata.last_event_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.executionDetail.checkpointAge')">
            {{ checkpointAge }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('transfer.executionDetail.checkpointHealth')">
            <el-tag :type="continuousHealthTagType(continuousDiagnostics.checkpoint_health || 'unknown')">
              {{ continuousHealthText(continuousDiagnostics.checkpoint_health || 'unknown') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item v-if="execution.metadata?.stop_reason" :label="t('transfer.executionDetail.stopReason')">
            {{ execution.metadata.stop_reason }}
          </el-descriptions-item>
          <template v-if="continuousRecovery">
            <el-descriptions-item :label="t('transfer.recovery.state')">
              <el-tag :type="continuousRecoveryTagType(continuousRecovery.state)">
                {{ recoveryStateText }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item :label="t('transfer.recovery.reason')">
              {{ recoveryReasonText }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('transfer.recovery.attempt')">
              {{ continuousRecovery.attempt ?? '-' }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('transfer.recovery.consecutiveFailures')">
              {{ continuousRecovery.consecutiveFailures }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('transfer.recovery.backoff')">
              {{ formatRecoverySeconds(continuousRecovery.backoffSeconds) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('transfer.recovery.nextAttemptAt')">
              {{ formatTimestamp(continuousRecovery.notBefore) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('transfer.recovery.recoveredFrom')" :span="3">
              {{ continuousRecovery.recoveredFromExecutionId || '-' }}
            </el-descriptions-item>
          </template>
        </el-descriptions>

        <el-alert
          v-if="continuousDiagnostics.error"
          :title="t('transfer.executionDetail.diagnosticsError')"
          :description="continuousDiagnostics.error"
          type="warning"
          :closable="false"
          class="diagnostics-alert"
        />

				<el-alert
					v-if="continuousSchemaChange"
					:title="t('transfer.executionDetail.schemaChangeBlocked')"
					:description="schemaChangeDescription"
					type="error"
					:closable="false"
					class="diagnostics-alert"
				/>

        <el-table :data="continuousPartitions" border size="small" class="partition-table">
          <el-table-column prop="partition" :label="t('transfer.executionDetail.partition')" width="120" />
          <el-table-column :label="t('transfer.executionDetail.retentionHealth')" width="120">
            <template #default="{ row }">
              <el-tag :type="continuousHealthTagType(row.health)" size="small">
                {{ continuousHealthText(row.health) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('transfer.executionDetail.nextOffset')" min-width="130">
            <template #default="{ row }">{{ formatContinuousMetric(row.nextOffset) }}</template>
          </el-table-column>
          <el-table-column :label="t('transfer.executionDetail.earliestOffset')" min-width="130">
            <template #default="{ row }">{{ formatContinuousMetric(row.earliestOffset) }}</template>
          </el-table-column>
          <el-table-column :label="t('transfer.executionDetail.latestOffset')" min-width="130">
            <template #default="{ row }">{{ formatContinuousMetric(row.latestOffset) }}</template>
          </el-table-column>
          <el-table-column :label="t('transfer.executionDetail.lagRecords')" min-width="120">
            <template #default="{ row }">{{ formatContinuousMetric(row.lagRecords) }}</template>
          </el-table-column>
          <el-table-column :label="t('transfer.executionDetail.recoveryHeadroom')" min-width="150">
            <template #default="{ row }">{{ formatContinuousMetric(row.recoveryHeadroomRecords) }}</template>
          </el-table-column>
          <el-table-column :label="t('transfer.executionDetail.sourceRate')" min-width="140">
            <template #default="{ row }">{{ formatContinuousRate(row.sourceRateRecordsPerSecond) }}</template>
          </el-table-column>
          <el-table-column :label="t('transfer.executionDetail.retentionHorizon')" min-width="150">
            <template #default="{ row }">{{ formatContinuousDurationSeconds(row.retentionHorizonSeconds) }}</template>
          </el-table-column>
          <el-table-column :label="t('transfer.executionDetail.checkpointHealth')" min-width="130">
            <template #default="{ row }">
              <el-tag :type="continuousHealthTagType(row.checkpointHealth)" size="small">
                {{ continuousHealthText(row.checkpointHealth) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('transfer.executionDetail.checkpointAge')" min-width="130">
            <template #default="{ row }">{{ formatContinuousDurationSeconds(row.checkpointAgeSeconds) }}</template>
          </el-table-column>
          <el-table-column prop="positionType" :label="t('transfer.executionDetail.positionType')" min-width="150" />
        </el-table>
      </template>

      <ExecutionEvents
        v-if="execution.execution_id"
        :execution-id="execution.execution_id"
        :load-page="executionAPI.events"
        :running="['pending', 'running'].includes(execution.status)"
      />
    </el-card>
  </div>
</template>

<script setup>
import { useAuthStore } from '@/store/auth'
import { ref, computed, watch, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MonitorExecutionsButton, ExecutionEvents, useConsolePageDescriptor } from '@common-ui'
import { executionAPI } from '@/api/tasks'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import {
  buildContinuousPartitionRows,
  continuousHealthTagType,
  continuousRecoveryTagType,
  formatContinuousDurationSeconds,
  formatContinuousRate,
  formatRecoverySeconds,
  getContinuousDiagnostics,
  getContinuousRecovery
} from '@addp/common-frontend'

const { t } = useI18n()
const authStore = useAuthStore()

const route = useRoute()
const router = useRouter()

const loading = ref(false)
const refreshing = ref(false)
const execution = ref({})
useConsolePageDescriptor(router, 'transfer', {
  title: computed(() => t('transfer.executionDetail.recentVisitTitle')),
  subject: computed(() => execution.value?.task_name || execution.value?.execution_id || ''),
  ready: computed(() => Boolean(execution.value?.execution_id))
})
const autoRefreshInterval = ref(null)
const executionId = computed(() => route.params.execution_id)

const continuousMetadata = computed(() => execution.value?.metadata?.continuous || {})
const continuousDiagnostics = computed(() => getContinuousDiagnostics(execution.value?.metadata))
const continuousRecovery = computed(() => getContinuousRecovery(execution.value?.metadata, execution.value?.status))
const recoveryStateText = computed(() => continuousRecovery.value
  ? t(`transfer.recovery.states.${continuousRecovery.value.state}`)
  : '-')
const recoveryReasonText = computed(() => {
  const reason = continuousRecovery.value?.reason || 'unknown'
  const key = `transfer.recovery.reasons.${reason}`
  const translated = t(key)
  return translated === key ? reason : translated
})
const recoveryAlertVisible = computed(() => ['waiting', 'open', 'half_open', 'ready'].includes(continuousRecovery.value?.state))
const recoveryAlertType = computed(() => continuousRecovery.value?.state === 'open' ? 'error' : 'warning')
const recoveryDescription = computed(() => continuousRecovery.value
  ? t('transfer.recovery.noticeDescription', {
      reason: recoveryReasonText.value,
      failures: continuousRecovery.value.consecutiveFailures,
      nextAttempt: formatTimestamp(continuousRecovery.value.notBefore)
    })
  : '')
const continuousSchemaChange = computed(() => continuousMetadata.value?.schema_change || null)
const schemaChangeDescription = computed(() => {
	const change = continuousSchemaChange.value
	if (!change) return ''
	return t('transfer.executionDetail.schemaChangeDescription', {
		scope: change.scope || '-',
		missing: Array.isArray(change.missing_fields) && change.missing_fields.length > 0 ? change.missing_fields.join(', ') : '-',
		unexpected: Array.isArray(change.unexpected_fields) && change.unexpected_fields.length > 0 ? change.unexpected_fields.join(', ') : '-',
		incompatible: Array.isArray(change.incompatible_fields) && change.incompatible_fields.length > 0 ? change.incompatible_fields.join(', ') : '-'
	})
})
const isContinuousExecution = computed(() => {
  return Object.keys(continuousMetadata.value).length > 0 || !!execution.value?.metadata?.stop_reason || !!continuousRecovery.value
})
const continuousPartitions = computed(() => buildContinuousPartitionRows(execution.value?.metadata))
const checkpointAge = computed(() => {
  const committedAt = continuousMetadata.value?.last_committed_at
  if (!committedAt) return '-'
  const milliseconds = Date.now() - new Date(committedAt).getTime()
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return '-'
  const seconds = Math.floor(milliseconds / 1000)
  if (seconds < 60) return t('transfer.executionDetail.secondsAgo', { count: seconds })
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return t('transfer.executionDetail.minutesAgo', { count: minutes })
  return t('transfer.executionDetail.hoursAgo', { count: Math.floor(minutes / 60) })
})

function formatTimestamp(value) {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? String(value) : date.toLocaleString()
}

function formatContinuousMetric(value) {
  return value === null || value === undefined ? '-' : value
}

function continuousHealthText(health) {
  const key = `transfer.executionDetail.health.${health}`
  const translated = t(key)
  return translated === key ? health : translated
}

let generation = 0
const stopRefresh = () => {
  clearTimeout(autoRefreshInterval.value)
  autoRefreshInterval.value = null
}
const loadExecution = async () => {
  const current = ++generation
  const id = executionId.value
  stopRefresh()
  execution.value = {}
  loading.value = true
  try {
    const result = await executionAPI.get(id)
    if (current !== generation) return
    execution.value = result
    if (['pending', 'running'].includes(result.status)) autoRefreshInterval.value = setTimeout(refreshExecution, 5000)
  } catch (error) {
    if (current === generation) ElMessage.error(t('transfer.executionDetail.loadFailed', { error: error.message || error }))
  } finally {
    if (current === generation) loading.value = false
  }
}
const refreshExecution = async () => {
  const current = generation
  if (refreshing.value) return
  refreshing.value = true
  try {
    const result = await executionAPI.get(executionId.value)
    if (current !== generation) return
    execution.value = result
    if (['pending', 'running'].includes(result.status)) autoRefreshInterval.value = setTimeout(refreshExecution, 5000)
  } catch (error) {
    if (current !== generation) return
    execution.value = {}
    stopRefresh()
    ElMessage.error(t('transfer.executionDetail.refreshFailed', { error: error.message || error }))
  } finally {
    if (current === generation) refreshing.value = false
  }
}

const getStatusType = (status) => {
  const types = {
    pending: 'info',
    running: 'primary',
    success: 'success',
    failed: 'danger'
  }
  return types[status] || 'info'
}

watch(executionId, () => { refreshing.value = false; loadExecution() }, { immediate: true })
onUnmounted(() => { generation++; stopRefresh() })
</script>

<style scoped>
.execution-detail { padding: 20px; }
.execution-error-message { white-space: pre-wrap; overflow-wrap: anywhere; }
</style>
