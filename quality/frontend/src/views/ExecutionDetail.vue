<template>
  <div v-loading="loading" class="execution-detail-page">
    <div class="page-header">
      <h2>{{ `${t('quality.execution.detailTitle')} - ${execution?.execution_id || ''}` }}</h2>
      <MonitorExecutionsButton
        module="quality"
        :task-type="execution?.task_type"
        :source-task-id="execution?.source_task_id"
        scope="task"
      />
    </div>

    <el-result
      v-if="loadError"
      icon="error"
      :title="t('quality.execution.detailLoadFailed')"
      :sub-title="loadError"
    >
      <template #extra>
        <MonitorExecutionsButton module="quality" />
      </template>
    </el-result>

    <el-descriptions v-else-if="execution" :column="2" border style="margin-top:20px">
      <el-descriptions-item :label="t('quality.domain.snapshotOwner')" :span="2">{{ executionDomainLabel(execution.execution_config, t) }}</el-descriptions-item>
      <el-descriptions-item :label="t('quality.targets.scope')" :span="2">{{ execution.execution_config?.target_key || t('quality.domain.unrecorded') }}</el-descriptions-item>
      <el-descriptions-item :label="t('quality.targets.actual')" :span="2">
        <div v-for="binding in execution.execution_config?.table_bindings || []" :key="binding.alias">{{ binding.alias }}: {{ binding.locator }}</div>
      </el-descriptions-item>
      <el-descriptions-item :label="t('quality.execution.status')">
        <el-tag :type="statusType(execution.status)">{{ statusLabel(execution.status) }}</el-tag>
      </el-descriptions-item>
      <el-descriptions-item v-if="planResult" :label="t('quality.execution.planResult')">
        <el-tag :type="planResult.passed ? 'success' : 'danger'">
          {{ planResult.passed ? t('quality.execution.gatePassed') : t('quality.execution.gateBlocked') }}
        </el-tag>
      </el-descriptions-item>
      <el-descriptions-item v-if="planResult" :label="t('quality.execution.qualityScore')">{{ planResult.quality_score == null ? '-' : Number(planResult.quality_score).toFixed(1)+'%' }}</el-descriptions-item>
      <el-descriptions-item :label="t('quality.execution.executionTime')">{{ execution.execution_time_ms ? execution.execution_time_ms + ' ms' : '-' }}</el-descriptions-item>
      <el-descriptions-item :label="t('quality.execution.createdAt')">{{ execution.created_at ? new Date(execution.created_at).toLocaleString() : '-' }}</el-descriptions-item>
    </el-descriptions>

    <el-alert
      v-if="failureReason"
      :title="t('quality.execution.failureReason')"
      :description="failureReason"
      type="error"
      show-icon
      :closable="false"
      style="margin-top:20px"
    />

    <template v-if="planResult?.rules?.length">
      <h3 style="margin-top:24px">{{ t('quality.execution.planRules') }}</h3>
      <el-table :data="planResult.rules" border size="small">
        <el-table-column prop="name" :label="t('quality.execution.ruleName')" min-width="180" />
        <el-table-column :label="t('quality.rule.revision')" width="110"><template #default="{row}">{{ row.revision_no ? 'R' + row.revision_no : '-' }}</template></el-table-column>
        <el-table-column prop="table" :label="t('quality.execution.tableAlias')" min-width="120" />
        <el-table-column :label="t('quality.execution.column')" min-width="150"><template #default="{row}">{{ row.columns?.join(', ') || '-' }}</template></el-table-column>
        <el-table-column prop="rule_key" :label="t('quality.execution.ruleKey')" min-width="280" show-overflow-tooltip />
        <el-table-column prop="type" :label="t('quality.execution.ruleType')" width="180" />
        <el-table-column prop="severity" :label="t('quality.execution.severity')" width="100" />
        <el-table-column :label="t('quality.execution.result')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.passed ? 'success' : 'danger'">{{ row.passed ? t('quality.execution.passed') : t('quality.execution.failed') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('quality.execution.failedCount')" width="120"><template #default="{row}">{{ row.type==='row_count' ? '-' : row.failed_count }}</template></el-table-column>
        <el-table-column prop="total_count" :label="t('quality.execution.totalCount')" width="100" />
        <el-table-column :label="t('quality.execution.observed')" min-width="220">
          <template #default="{ row }"><code>{{ JSON.stringify(row.observed || {}) }}</code></template>
        </el-table-column>
      </el-table>

    </template>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MonitorExecutionsButton, useConsolePageDescriptor } from '@common-ui'
import { executionAPI } from '../api/quality'
import { useI18n } from 'vue-i18n'
import { executionFailureLabel } from '../utils/executionFailure'
import { executionDomainLabel } from '../utils/domainOwnership'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const execution = ref(null)
useConsolePageDescriptor(router, 'quality', {
  title: computed(() => t('quality.execution.recentVisitTitle')),
  subject: computed(() => execution.value?.task_name || execution.value?.execution_id || ''),
  ready: computed(() => Boolean(execution.value?.execution_id))
})
const loading = ref(false)
const loadError = ref('')
let pollTimer = null
let pollStopped = false
let initialLoad = true
let loadSequence = 0
const planResult = computed(() => {
  const metadata = execution.value?.metadata
  return metadata?.schema_version === 'addp.quality.plan-result/v1' ? metadata : null
})
const failureReason = computed(() => executionFailureLabel(execution.value, t))

const statusType = (status) => {
  const map = { success: 'success', failed: 'danger', timeout: 'danger', running: 'warning', pending: 'info' }
  return map[status] || 'info'
}
const statusLabel = (status) => ({
  pending: t('quality.execution.pending'),
  running: t('quality.execution.running'),
  success: t('quality.execution.success'),
  failed: t('quality.execution.failed'),
  timeout: t('quality.execution.timeout'),
  cancelled: t('quality.execution.cancelled')
}[status] || status)

const loadExecution = async () => {
  const executionID = route.params.execution_id
  const requestSequence = ++loadSequence
  if (pollTimer) {
    window.clearTimeout(pollTimer)
    pollTimer = null
  }
  loading.value = initialLoad
  loadError.value = ''
  try {
    const res = await executionAPI.get(executionID)
    if (requestSequence !== loadSequence) return
    execution.value = res
    if (!pollStopped && (res?.status === 'pending' || res?.status === 'running')) {
      pollTimer = window.setTimeout(loadExecution, 2000)
    }
  } catch (error) {
    if (requestSequence !== loadSequence) return
    execution.value = null
    loadError.value = error.response?.data?.error || t('quality.execution.detailLoadFailed')
  } finally {
    if (requestSequence !== loadSequence) return
    loading.value = false
    initialLoad = false
  }
}

watch(() => route.fullPath, () => {
  loadSequence += 1
  if (pollTimer) {
    window.clearTimeout(pollTimer)
    pollTimer = null
  }
  execution.value = null
  loadError.value = ''
  initialLoad = true
  pollStopped = false
  loadExecution()
}, { immediate: true })
onBeforeUnmount(() => {
  loadSequence += 1
  if (pollTimer) window.clearTimeout(pollTimer)
  pollStopped = true
})
</script>

<style scoped>
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.page-header h2 {
  margin: 0;
  color: var(--addp-text-primary);
  font-size: 20px;
}

.execution-detail-page {
  min-height: 240px;
}
</style>
