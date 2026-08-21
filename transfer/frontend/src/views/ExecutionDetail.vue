<template>
  <div class="execution-detail">
    <el-button @click="handleBack" style="margin-bottom: 20px;">{{ t('transfer.executionDetail.back') }}</el-button>
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

      <!-- ✅ 新增：后处理摘要卡片 -->
      <el-divider v-if="execution.status === 'success'">{{ t('transfer.executionDetail.postProcessSummary') }}</el-divider>
      <div v-if="execution.status === 'success'" class="post-process-summary">
        <el-space wrap :size="15">
          <!-- 主键创建 -->
          <el-statistic
            v-if="postProcessSummary.primary_key_created"
            :title="t('transfer.executionDetail.primaryKeyCreated')"
            :value="'✓'"
          >
            <template #prefix>
              <el-icon style="color: var(--el-color-success); font-size: 20px;">
                <span style="font-weight: bold;">🔑</span>
              </el-icon>
            </template>
            <template #suffix>
              <el-text size="small" type="success">
                {{ postProcessSummary.primary_key_columns.join(', ') }}
              </el-text>
            </template>
          </el-statistic>

          <!-- 空间索引 -->
          <el-statistic
            v-if="postProcessSummary.spatial_indexes_created > 0"
            :title="t('transfer.executionDetail.spatialIndexes')"
            :value="postProcessSummary.spatial_indexes_created"
          >
            <template #prefix>
              <el-icon style="color: var(--el-color-primary); font-size: 20px;">
                <span style="font-weight: bold;">🗺️</span>
              </el-icon>
            </template>
            <template #suffix>
              <el-text size="small" type="primary">{{ t('transfer.executionDetail.count', { count: '' }).trim() || '' }}</el-text>
            </template>
          </el-statistic>

          <!-- 统计更新 -->
          <el-statistic
            v-if="postProcessSummary.statistics_updated"
            :title="t('transfer.executionDetail.statisticsUpdated')"
            :value="'✓'"
          >
            <template #prefix>
              <el-icon style="color: var(--addp-text-tertiary); font-size: 20px;">
                <span style="font-weight: bold;">📊</span>
              </el-icon>
            </template>
          </el-statistic>
        </el-space>
      </div>

      <el-divider>{{ t('transfer.executionDetail.executionLogs') }}</el-divider>

      <!-- 日志控制栏 -->
      <div class="log-controls">
        <el-radio-group v-model="logLevel" size="small">
          <el-radio-button value="all">{{ t('transfer.executionDetail.filterAll') }}</el-radio-button>
          <el-radio-button value="info">INFO</el-radio-button>
          <el-radio-button value="post-process">{{ t('transfer.executionDetail.filterPostProcess') }}</el-radio-button>
          <el-radio-button value="error">ERROR</el-radio-button>
        </el-radio-group>

        <div class="log-actions">
          <el-button
            size="small"
            @click="refreshLogs"
            :loading="refreshing"
            :disabled="!execution.execution_id">
            {{ t('transfer.executionDetail.refreshLogs') }}
          </el-button>

          <el-button
            size="small"
            @click="downloadLogs"
            :disabled="!logs">
            {{ t('transfer.executionDetail.downloadLogs') }}
          </el-button>
        </div>
      </div>

      <!-- 日志查看器（高亮显示后处理日志） -->
      <div class="log-viewer">
        <div v-if="filteredLogs">
          <div
            v-for="(line, index) in filteredLogsArray"
            :key="index"
            :class="getLogLineClass(line)"
            class="log-line"
          >
            <span class="log-icon">{{ getLogIcon(line) }}</span>
            <span class="log-text">{{ line }}</span>
          </div>
        </div>
        <div v-else class="empty-logs">{{ t('transfer.executionDetail.noLogs') }}</div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useConsolePageDescriptor } from '@common-ui'
import { executionAPI } from '@/api/tasks'
import { ElMessage, ElIcon } from 'element-plus'
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
import { navigateTransferRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()

const handleBack = () => navigateTransferRoute(router, '/executions', { history: 'replace' })
const loading = ref(false)
const refreshing = ref(false)
const execution = ref({})
useConsolePageDescriptor(router, 'transfer', {
  title: computed(() => t('transfer.executionDetail.recentVisitTitle')),
  subject: computed(() => execution.value?.task_name || execution.value?.execution_id || ''),
  ready: computed(() => Boolean(execution.value?.execution_id))
})
const logs = ref('')
const logLevel = ref('all')
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

const loadExecution = async () => {
  loading.value = true
  try {
    execution.value = await executionAPI.get(executionId.value)

    // 加载日志 - API 返回 {logs: "string"}
    const logData = await executionAPI.logs(executionId.value)

    // 处理响应格式
    if (typeof logData === 'object' && logData.logs !== undefined) {
      logs.value = logData.logs || ''
    } else if (typeof logData === 'string') {
      logs.value = logData
    } else if (Array.isArray(logData)) {
      logs.value = logData.join('\n')
    } else {
      logs.value = ''
    }

    // 如果任务正在运行，启动自动刷新
    if (['pending', 'running'].includes(execution.value.status) && !autoRefreshInterval.value) {
      autoRefreshInterval.value = setInterval(refreshLogs, 5000)
    }
  } catch (error) {
    ElMessage.error(t('transfer.executionDetail.loadFailed', { error: error.message || error }))
  } finally {
    loading.value = false
  }
}

const refreshLogs = async () => {
  if (refreshing.value) return

    refreshing.value = true
  try {
    const logData = await executionAPI.logs(executionId.value)

    // 处理响应格式
    if (typeof logData === 'object' && logData.logs !== undefined) {
      logs.value = logData.logs || ''
    } else if (typeof logData === 'string') {
      logs.value = logData
    } else if (Array.isArray(logData)) {
      logs.value = logData.join('\n')
    } else {
      logs.value = ''
    }

    // 同时刷新执行状态
    execution.value = await executionAPI.get(executionId.value)

    // 如果任务不再运行，停止自动刷新
    if (!['pending', 'running'].includes(execution.value.status) && autoRefreshInterval.value) {
      clearInterval(autoRefreshInterval.value)
      autoRefreshInterval.value = null
    }
  } catch (error) {
    ElMessage.error(t('transfer.executionDetail.refreshFailed', { error: error.message || error }))
  } finally {
    refreshing.value = false
  }
}

const downloadLogs = () => {
  if (!logs.value) {
    ElMessage.warning(t('transfer.executionDetail.noLogsToDownload'))
    return
  }

  const blob = new Blob([logs.value], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `execution-${executionId.value}-logs.txt`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)

  ElMessage.success(t('transfer.executionDetail.downloadSuccess'))
}

// ✅ 新增：后处理摘要信息提取
const postProcessSummary = computed(() => {
  const summary = {
    primary_key_created: false,
    primary_key_columns: [],
    spatial_indexes_created: 0,
    statistics_updated: false
  }

  if (!logs.value) return summary

  const logLines = logs.value.split('\n')

  logLines.forEach(line => {
    // 检测主键创建成功
    if (line.includes('✅ [后处理]') && line.includes('主键创建成功')) {
      summary.primary_key_created = true
      // 从日志中提取列名，格式: "columns"=["SmID"]
      const match = line.match(/"columns"=\[(.*?)\]/)
      if (match) {
        summary.primary_key_columns = match[1]
          .split(',')
          .map(s => s.trim().replace(/"/g, ''))
          .filter(Boolean)
      }
    }

    // 检测空间索引创建
    if (line.includes('✅ [后处理]') && line.includes('空间索引创建成功')) {
      summary.spatial_indexes_created++
    }

    // 检测统计信息更新
    if (line.includes('✅ [后处理]') && line.includes('统计信息更新成功')) {
      summary.statistics_updated = true
    }
  })

  return summary
})

// ✅ 新增：日志行分类
const getLogIcon = (line) => {
  if (line.includes('🔑')) return '🔑'
  if (line.includes('🗺️')) return '🗺️'
  if (line.includes('📊')) return '📊'
  if (line.includes('❌')) return '❌'
  if (line.includes('✅')) return '✅'
  if (line.includes('⚠️')) return '⚠️'
  if (line.includes('ℹ️')) return 'ℹ️'
  if (line.includes('⚙️')) return '⚙️'
  return ' '
}

// ✅ 新增：日志行样式分类
const getLogLineClass = (line) => {
  // 后处理相关日志
  if (line.includes('[后处理]')) {
    if (line.includes('✅')) return 'log-success'
    if (line.includes('🔑')) return 'log-primary-key'
    if (line.includes('🗺️')) return 'log-spatial-index'
    if (line.includes('📊')) return 'log-statistics'
    if (line.includes('❌')) return 'log-error'
    if (line.includes('⚠️')) return 'log-warning'
    return 'log-post-process'
  }

  // 普通日志
  if (line.includes('[ERROR]') || line.includes('❌')) return 'log-error'
  if (line.includes('[WARN]') || line.includes('⚠️')) return 'log-warning'
  if (line.includes('[INFO]') || line.includes('ℹ️')) return 'log-info'

  return 'log-default'
}

// ✅ 新增：日志行数组（用于逐行渲染）
const filteredLogsArray = computed(() => {
  if (!logs.value) return []

  const lines = logs.value.split('\n')

  if (logLevel.value === 'all') return lines

  if (logLevel.value === 'post-process') {
    return lines.filter(line => line.includes('[后处理]'))
  }

  return lines.filter(line => {
    const upperLevel = logLevel.value.toUpperCase()
    return line.includes(`[${upperLevel}]`)
  })
})

// 日志过滤（保留原有的 pre 方式作为备用）
const filteredLogs = computed(() => {
  if (!logs.value) return ''
  if (logLevel.value === 'all') return logs.value

  return logs.value
    .split('\n')
    .filter(line => {
      if (logLevel.value === 'post-process') {
        return line.includes('[后处理]')
      }
      const upperLevel = logLevel.value.toUpperCase()
      return line.includes(`[${upperLevel}]`)
    })
    .join('\n')
})

const getStatusType = (status) => {
  const types = {
    pending: 'info',
    running: 'primary',
    success: 'success',
    failed: 'danger'
  }
  return types[status] || 'info'
}

onMounted(() => {
  loadExecution()
})

onUnmounted(() => {
  // 清理自动刷新定时器
  if (autoRefreshInterval.value) {
    clearInterval(autoRefreshInterval.value)
    autoRefreshInterval.value = null
  }
})
</script>

<style scoped>
.execution-detail {
  padding: 20px;
}

.log-controls {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  padding: 10px;
  background: var(--addp-bg-secondary);
  border-radius: 4px;
}

.partition-table {
  margin-top: 12px;
}

.diagnostics-alert {
  margin-top: 12px;
}

.log-actions {
  display: flex;
  gap: 8px;
}

.log-viewer {
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 16px;
  border-radius: 4px;
  max-height: 600px;
  overflow-y: auto;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.5;
}

.log-viewer pre {
  margin: 0;
  white-space: pre-wrap;
  word-wrap: break-word;
}

/* ✅ 新增：日志行样式 */
.log-line {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 4px 0;
  border-left: 3px solid transparent;
  padding-left: 6px;
  white-space: pre-wrap;
  word-wrap: break-word;
}

.log-icon {
  min-width: 24px;
  font-weight: bold;
}

.log-text {
  flex: 1;
}

/* 主键创建日志 */
.log-primary-key {
  background-color: rgba(232, 219, 163, 0.1);
  border-left-color: var(--el-color-warning);
  color: #ffb94f;
}

/* 空间索引日志 */
.log-spatial-index {
  background-color: rgba(89, 184, 255, 0.1);
  border-left-color: var(--el-color-primary);
  color: #66b1ff;
}

/* 统计信息日志 */
.log-statistics {
  background-color: rgba(144, 147, 153, 0.1);
  border-left-color: var(--addp-text-tertiary);
  color: #a8abb2;
}

/* 成功日志 */
.log-success {
  background-color: rgba(103, 194, 58, 0.1);
  border-left-color: var(--el-color-success);
  color: #85ce61;
  font-weight: bold;
}

/* 错误日志 */
.log-error {
  background-color: rgba(245, 108, 108, 0.1);
  border-left-color: var(--el-color-danger);
  color: #f78989;
  font-weight: bold;
}

/* 警告日志 */
.log-warning {
  background-color: rgba(230, 162, 60, 0.1);
  border-left-color: var(--el-color-warning);
  color: #ffb94f;
}

/* 信息日志 */
.log-info {
  background-color: rgba(89, 184, 255, 0.1);
  border-left-color: var(--el-color-primary);
  color: #66b1ff;
}

/* 后处理日志 */
.log-post-process {
  background-color: rgba(103, 194, 58, 0.1);
  border-left-color: var(--el-color-success);
  color: #85ce61;
}

/* 默认日志 */
.log-default {
  color: #d4d4d4;
}

/* 后处理摘要样式 */
.post-process-summary {
  background: var(--addp-bg-secondary);
  padding: 16px;
  border-radius: 4px;
  margin-bottom: 16px;
  border-left: 4px solid var(--el-color-success);
}

.empty-logs {
  text-align: center;
  color: var(--addp-text-tertiary);
  padding: 40px 0;
  font-size: 14px;
}

/* 自定义滚动条样式 */
.log-viewer::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

.log-viewer::-webkit-scrollbar-track {
  background: #2d2d2d;
  border-radius: 4px;
}

.log-viewer::-webkit-scrollbar-thumb {
  background: #555;
  border-radius: 4px;
}

.log-viewer::-webkit-scrollbar-thumb:hover {
  background: var(--addp-text-secondary);
}
</style>
