<template>
  <div v-loading="loading">
    <el-page-header @back="backToList" :content="`${t('quality.execution.detailTitle')} - ${execution?.execution_id || ''}`" />

    <el-descriptions v-if="execution" :column="2" border style="margin-top:20px">
      <el-descriptions-item :label="t('quality.execution.status')">
        <el-tag :type="statusType(execution.status)">{{ execution.status }}</el-tag>
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
import { computed, onBeforeUnmount, ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { executionAPI } from '../api/quality'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { navigateQualityRoute } from '../utils/moduleNavigation'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const execution = ref(null)
const loading = ref(false)
let pollTimer = null
const result = computed(() => {
  const metadata = execution.value?.metadata
  return metadata?.schema_version === 'addp.quality.execution-result/v1' ? metadata : null
})

const statusType = (status) => {
  const map = { success: 'success', failed: 'danger', running: 'warning', pending: 'info' }
  return map[status] || 'info'
}

const backToList = () => navigateQualityRoute(router, '/executions', { history: 'replace' })

const loadExecution = async () => {
  loading.value = true
  try {
    const res = await executionAPI.get(route.params.execution_id)
    execution.value = res
    if (res?.status === 'pending' || res?.status === 'running') {
      pollTimer = window.setTimeout(loadExecution, 2000)
    }
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('quality.execution.loadFailed'))
  } finally {
    loading.value = false
  }
}

onMounted(loadExecution)
onBeforeUnmount(() => {
  if (pollTimer) window.clearTimeout(pollTimer)
})
</script>
