<template>
  <div v-loading="loading" class="execution-detail-page">
    <el-page-header @back="backToList" :content="`${t('quality.execution.detailTitle')} - ${execution?.execution_id || ''}`" />

    <el-result
      v-if="loadError"
      icon="error"
      :title="t('quality.execution.detailLoadFailed')"
      :sub-title="loadError"
    >
      <template #extra>
        <el-button @click="backToList">{{ t('quality.execution.backToList') }}</el-button>
      </template>
    </el-result>

    <el-descriptions v-else-if="execution" :column="2" border style="margin-top:20px">
      <el-descriptions-item :label="t('quality.execution.status')">
        <el-tag :type="statusType(execution.status)">{{ statusLabel(execution.status) }}</el-tag>
      </el-descriptions-item>
      <el-descriptions-item :label="t('quality.execution.qualityScore')">
        <span v-if="result?.quality_score != null" style="font-size:18px;font-weight:bold">
          {{ Number(result.quality_score).toFixed(1) }}%
        </span>
        <span v-else>-</span>
      </el-descriptions-item>
    <el-descriptions-item :label="t('quality.execution.totalRules')">{{ result?.total_rules ?? '-' }}</el-descriptions-item>
      <el-descriptions-item :label="t('quality.execution.passedFailed')">
        {{ result?.passed_rules ?? '-' }} / {{ result?.failed_rules ?? '-' }}
      </el-descriptions-item>
      <el-descriptions-item :label="t('quality.execution.executionTime')">{{ execution.execution_time_ms ? execution.execution_time_ms + ' ms' : '-' }}</el-descriptions-item>
      <el-descriptions-item :label="t('quality.execution.createdAt')">{{ execution.created_at ? new Date(execution.created_at).toLocaleString() : '-' }}</el-descriptions-item>
    </el-descriptions>

    <el-alert
      v-if="execution?.status === 'failed' && failureReason"
      :title="t('quality.execution.failureReason')"
      :description="failureReason"
      type="error"
      show-icon
      :closable="false"
      style="margin-top:20px"
    />

    <template v-if="result?.field_scores?.length">
      <h3 style="margin-top:24px">{{ t('quality.execution.fieldScores') }}</h3>
      <el-table :data="result.field_scores" border size="small">
        <el-table-column prop="column" :label="t('quality.execution.field')" />
        <el-table-column :label="t('quality.execution.score')" width="120">
          <template #default="{ row }">{{ Number(row.score).toFixed(1) }}%</template>
        </el-table-column>
        <el-table-column prop="rule_count" :label="t('quality.execution.totalRules')" width="100" />
      </el-table>
    </template>

    <template v-if="result?.rule_details?.length">
      <h3 style="margin-top:24px">{{ t('quality.execution.ruleDetails') }}</h3>
      <el-table :data="result.rule_details" border size="small">
        <el-table-column prop="type" :label="t('quality.execution.ruleType')" width="120" />
        <el-table-column prop="severity" :label="t('quality.execution.severity')" width="100" />
        <el-table-column prop="column" :label="t('quality.execution.column')" width="150" />
        <el-table-column prop="table" :label="t('quality.execution.table')" width="150" />
        <el-table-column :label="t('quality.execution.passRate')" width="100">
          <template #default="{ row }">{{ row.pass_rate == null ? '-' : Number(row.pass_rate).toFixed(1) }}%</template>
        </el-table-column>
        <el-table-column :label="t('quality.execution.result')" width="80">
          <template #default="{ row }">
            <el-tag :type="row.passed ? 'success' : 'danger'">{{ row.passed ? t('quality.execution.passed') : t('quality.execution.failed') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="failed_count" :label="t('quality.execution.failedCount')" width="100" />
        <el-table-column prop="total_count" :label="t('quality.execution.totalCount')" width="100" />
      </el-table>
    </template>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { executionAPI } from '../api/quality'
import { useI18n } from 'vue-i18n'
import { navigateQualityRoute } from '../utils/moduleNavigation'
import { resolveExecutionListRouteState } from '../utils/executionListRouteState'
import { executionFailureLabel } from '../utils/executionFailure'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const execution = ref(null)
const loading = ref(false)
const loadError = ref('')
let pollTimer = null
let pollStopped = false
let initialLoad = true
let loadSequence = 0
const result = computed(() => {
  const metadata = execution.value?.metadata
  return metadata?.schema_version === 'addp.quality.execution-result/v1' ? metadata : null
})
const failureReason = computed(() => executionFailureLabel(execution.value, t))

const statusType = (status) => {
  const map = { success: 'success', failed: 'danger', running: 'warning', pending: 'info' }
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

const backToList = () => navigateQualityRoute(router, {
  path: '/executions',
  query: resolveExecutionListRouteState(route.query).query
}, { history: 'replace' })

const loadExecution = async () => {
  const listState = resolveExecutionListRouteState(route.query)
  if (listState.changed) {
    await navigateQualityRoute(router, {
      name: 'ExecutionDetail',
      params: { execution_id: route.params.execution_id },
      query: listState.query
    }, { history: 'replace' })
    return
  }
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
.execution-detail-page {
  min-height: 240px;
}
</style>
