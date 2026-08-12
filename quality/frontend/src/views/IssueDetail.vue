<template>
  <div v-loading="loading" class="issue-detail-page">
    <el-page-header @back="backToList" :content="detailTitle" />

    <el-result
      v-if="loadError"
      icon="error"
      :title="t('quality.issue.detailLoadFailed')"
      :sub-title="loadError"
    >
      <template #extra>
        <el-button @click="backToList">{{ t('quality.issue.backToList') }}</el-button>
      </template>
    </el-result>

    <template v-else-if="issue">
      <div class="detail-toolbar">
        <el-tag :type="statusTagType(issue.status)">{{ statusLabel(issue.status) }}</el-tag>
        <div v-if="issue.status === 'open'" class="detail-actions">
          <el-button type="success" @click="changeStatus('resolved')">{{ t('quality.issue.markResolved') }}</el-button>
          <el-button @click="changeStatus('ignored')">{{ t('quality.issue.ignore') }}</el-button>
        </div>
      </div>

      <section class="detail-section">
        <h3>{{ t('quality.issue.problemFacts') }}</h3>
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('quality.issue.ruleType')">{{ issue.type || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.severity')">
            <el-tag :type="severityTagType(issue.severity)">{{ issue.severity || '-' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.engineId')">{{ issue.engine_id ?? '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.ruleApplicationId')">{{ issue.rule_application_id ?? '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.schema')">{{ issue.schema_name || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.tableName')">{{ issue.table_name || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.column')">{{ issue.column_name || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.passRate')">{{ formatPassRate(issue.pass_rate) }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.failedCount')">{{ issue.failed_count ?? '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.totalCount')">{{ issue.total_count ?? '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.message')" :span="2">{{ issue.message || '-' }}</el-descriptions-item>
        </el-descriptions>
      </section>

      <section class="detail-section">
        <h3>{{ t('quality.issue.observationFacts') }}</h3>
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('quality.issue.firstExecution')" :span="2">
            <el-button v-if="issueExecutionRoute(issue.execution_id)" type="primary" link @click="openExecution(issue.execution_id)">
              {{ issue.execution_id }}
            </el-button>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.lastExecution')" :span="2">
            <el-button v-if="issueExecutionRoute(issue.last_execution_id)" type="primary" link @click="openExecution(issue.last_execution_id)">
              {{ issue.last_execution_id }}
            </el-button>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.firstObservedAt')">{{ formatDateTime(issue.created_at) }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.lastObservedAt')">{{ formatDateTime(issue.last_observed_at) }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.updatedAt')">{{ formatDateTime(issue.updated_at) }}</el-descriptions-item>
        </el-descriptions>
      </section>

      <section v-if="issue.status !== 'open'" class="detail-section">
        <h3>{{ t('quality.issue.resolutionAudit') }}</h3>
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('quality.issue.resolutionSource')">{{ resolutionSource }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.resolvedAt')">{{ formatDateTime(issue.resolved_at) }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.resolvedBy')">{{ issue.resolved_by ?? '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('quality.issue.resolutionNote')" :span="2">{{ issue.resolution_note || '-' }}</el-descriptions-item>
        </el-descriptions>
      </section>
    </template>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { issueAPI } from '../api/quality'
import { navigateQualityRoute } from '../utils/moduleNavigation'
import { issueExecutionRoute } from '../utils/issueNavigation'
import { resolveIssueListRouteState } from '../utils/issueListRouteState'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const issue = ref(null)
const loading = ref(false)
const loadError = ref('')
let loadSequence = 0

const detailTitle = computed(() => `${t('quality.issue.detailTitle')} - ${issue.value?.id || route.params.id || ''}`)
const resolutionSource = computed(() => issue.value?.resolved_by != null
  ? t('quality.issue.manualResolution')
  : t('quality.issue.automaticResolution'))

const statusTagType = (status) => ({ open: 'danger', resolved: 'success', ignored: 'info' }[status] || 'info')
const severityTagType = (severity) => ({ error: 'danger', warning: 'warning', info: 'info' }[severity] || 'info')
const statusLabel = (status) => ({
  open: t('quality.issue.open'),
  resolved: t('quality.issue.resolved'),
  ignored: t('quality.issue.ignored')
}[status] || status)
const formatPassRate = (value) => value == null ? '-' : `${Number(value).toFixed(1)}%`
const formatDateTime = (value) => value ? new Date(value).toLocaleString() : '-'

const backToList = () => navigateQualityRoute(router, {
  path: '/issues',
  query: resolveIssueListRouteState(route.query).query
}, { history: 'replace' })

const openExecution = (executionId) => {
  const location = issueExecutionRoute(executionId)
  if (location) navigateQualityRoute(router, location)
}

const loadIssue = async () => {
  const listState = resolveIssueListRouteState(route.query)
  if (listState.changed) {
    await navigateQualityRoute(router, {
      name: 'IssueDetail',
      params: { id: route.params.id },
      query: listState.query
    }, { history: 'replace' })
    return
  }
  const sequence = ++loadSequence
  loading.value = true
  loadError.value = ''
  try {
    const result = await issueAPI.get(route.params.id)
    if (sequence === loadSequence) issue.value = result
  } catch (error) {
    if (sequence !== loadSequence) return
    issue.value = null
    loadError.value = error.response?.data?.error || t('quality.issue.detailLoadFailed')
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

const changeStatus = async (status) => {
  try {
    const { value: note } = await ElMessageBox.prompt(t('quality.issue.notePrompt'), t('quality.issue.noteTitle'), {
      inputPattern: /\S+/,
      inputErrorMessage: t('quality.issue.noteRequired'),
      confirmButtonText: t('quality.issue.confirm'),
      cancelButtonText: t('quality.issue.cancel')
    })
    await issueAPI.updateStatus(issue.value.id, status, note)
    ElMessage.success(t('quality.issue.updateSuccess'))
    await loadIssue()
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error.response?.data?.error || t('quality.issue.updateFailed'))
  }
}

watch(() => route.fullPath, loadIssue, { immediate: true })
</script>

<style scoped>
.issue-detail-page {
  min-height: 240px;
}
.detail-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-top: 20px;
}
.detail-actions {
  display: flex;
  gap: 8px;
}
.detail-section {
  margin-top: 24px;
}
.detail-section h3 {
  margin: 0 0 12px;
  color: var(--addp-text-primary);
  font-size: 16px;
  font-weight: 600;
}
</style>
