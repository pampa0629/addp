<template>
  <div class="page-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <div class="page-title">
            <span>{{ t('system.cleanup.title') }}</span>
            <small>{{ t('system.cleanup.subtitle') }}</small>
          </div>
          <div class="header-actions">
            <el-button
              type="primary"
              :icon="Search"
              :loading="scanLoading"
              :disabled="isTenantScopeRequired"
              @click="startScan"
            >
              {{ t('system.cleanup.scan') }}
            </el-button>
            <el-button
              :icon="Refresh"
              @click="loadHistory"
            >
              {{ t('system.cleanup.refreshHistory') }}
            </el-button>
            <el-button
              :icon="Monitor"
              @click="openCleanupMonitor"
            >
              {{ t('system.cleanup.actions.viewAllMonitor') }}
            </el-button>
          </div>
        </div>
      </template>

      <!-- 当前扫描/执行任务 -->
      <el-alert
        v-if="isTenantScopeRequired"
        :title="t('system.cleanup.tenantScopeRequired.title')"
        type="warning"
        :closable="false"
        class="tenant-scope-alert"
      >
        <div>{{ t('system.cleanup.tenantScopeRequired.desc') }}</div>
      </el-alert>

      <el-alert
        v-if="currentTask"
        :title="t('system.cleanup.taskInProgress', { action: currentTask.action === 'scan' ? t('system.cleanup.history.actionScan') : t('system.cleanup.history.actionCleanup') })"
        type="info"
        :closable="false"
        style="margin-bottom: 20px"
      >
        <div>{{ t('system.cleanup.taskId', { id: currentTask.task_id }) }}</div>
        <div>{{ t('system.cleanup.taskStatus', { status: getStatusText(currentTask.status) }) }}</div>
        <el-button
          v-if="currentTask.task?.execution_id"
          type="primary"
          link
          @click="openMonitor(currentTask.task.execution_id)"
        >
          {{ t('system.cleanup.actions.viewMonitor') }}
        </el-button>

      </el-alert>

      <el-empty
        v-if="!latestResult"
        class="cleanup-empty"
        :description="t('system.cleanup.empty.description')"
      >
        <el-button
          type="primary"
          :icon="Search"
          :loading="scanLoading"
          :disabled="isTenantScopeRequired"
          @click="startScan"
        >
          {{ t('system.cleanup.empty.action') }}
        </el-button>
      </el-empty>

      <!-- 扫描结果 -->
      <div v-if="latestResult" class="scan-result">
        <el-alert
          :title="`${resultBannerTitle} · ${getStatusText(latestResult.status)}`"
          :type="latestResult.status === 'success' ? 'success' : 'warning'"
          :closable="false"
          class="result-banner"
        >
          <div>{{ resultBannerDescription }}</div>
        </el-alert>
        <div class="summary-grid">
          <div class="summary-tile">
            <span class="summary-label">{{ latestResult.action === 'execute' ? t('system.cleanup.overview.affectedRecords') : t('system.cleanup.overview.items') }}</span>
            <strong>{{ latestResultOverview.primaryCount }}</strong>
          </div>
          <div class="summary-tile">
            <span class="summary-label">{{ t('system.cleanup.overview.risk') }}</span>
            <el-tag :type="getRiskLevelType(latestResultOverview.risk)">
              {{ getRiskLevelText(latestResultOverview.risk) }}
            </el-tag>
          </div>
          <div class="summary-tile">
            <span class="summary-label">{{ t('system.cleanup.overview.freedBytes') }}</span>
            <strong>{{ formatBytes(latestResultOverview.freedBytes) }}</strong>
          </div>
          <div class="summary-tile">
            <span class="summary-label">{{ t('system.cleanup.overview.modules') }}</span>
            <strong>{{ latestResultOverview.modules }}</strong>
          </div>
        </div>

        <el-descriptions :title="latestResult.action === 'execute' ? t('system.cleanup.executeResult.title') : t('system.cleanup.scanResult.title')" :column="2" border class="result-descriptions">
          <el-descriptions-item :label="t('system.cleanup.scanResult.scanTime')">
            {{ formatTime(latestResult.task.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.scanScope')">
            {{ formatModules(latestResult.task.expected_modules) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.triggerType')">
            <el-tag :type="getTriggerTypeTag(latestResult.task.trigger_type)">
              {{ getTriggerTypeText(latestResult.task.trigger_type) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.causeEvent')">
            {{ getCauseEventText(latestResult.task.cause_event) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.context')">
            {{ formatContext(latestResult.task.context) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.riskLevel')">
            <el-tag :type="getRiskLevelType(latestResult.summary?.risk_level || 'unknown')">
              {{ getRiskLevelText(latestResult.summary?.risk_level || 'unknown') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="latestResult.action === 'execute' ? t('system.cleanup.executeResult.affectedRecords') : t('system.cleanup.scanResult.itemsToClean')">
            {{ t('system.cleanup.scanResult.items', { count: latestResultOverview.primaryCount }) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.monitor')" v-if="latestResult.task.execution_id">
            <el-button type="primary" link @click="openMonitor(latestResult.task.execution_id)">
              {{ t('system.cleanup.actions.viewMonitor') }}
            </el-button>
          </el-descriptions-item>
        </el-descriptions>

        <CleanupModuleResults :executions="latestResult.module_executions" :request-id="latestResult.task_id" :identity-generation="identityGeneration" :can-read-result="canReadResult" :read-result="cleanupApi.readProfessionalResult" />

        <!-- 清理操作 -->
        <div class="cleanup-actions">
          <el-alert
            :title="t('system.cleanup.cleanupNote.title')"
            type="warning"
            :closable="false"
            style="margin-bottom: 15px"
          >
            <ul style="margin: 0; padding-left: 20px">
              <li><strong>{{ t('system.cleanup.cleanupNote.logicalCleanup') }}</strong>: {{ t('system.cleanup.cleanupNote.logicalCleanupDesc') }}</li>
              <li><strong>{{ t('system.cleanup.cleanupNote.physicalCleanup') }}</strong>: {{ t('system.cleanup.cleanupNote.physicalCleanupDesc') }}</li>
            </ul>
          </el-alert>

          <el-space>
            <el-button
              v-if="latestResult.action === 'execute'"
              type="primary"
              :icon="Search"
              :loading="scanLoading"
              :disabled="isTenantScopeRequired"
              @click="startScan"
            >
              {{ t('system.cleanup.actions.newAssessment') }}
            </el-button>
            <el-button
              v-if="scanResult"
              type="warning"
              :icon="WarningFilled"
              :loading="executeLoading"
              :disabled="isTenantScopeRequired"
              @click="openExecuteConfirm('logical_cleanup')"
            >
              {{ t('system.cleanup.actions.logicalCleanup') }}
            </el-button>
            <el-button
              v-if="scanResult"
              type="danger"
              :icon="Delete"
              :loading="executeLoading"
              :disabled="isTenantScopeRequired"
              @click="openExecuteConfirm('physical_cleanup')"
            >
              {{ t('system.cleanup.actions.physicalCleanup') }}
            </el-button>
          </el-space>
        </div>
      </div>

      <!-- 任务历史 -->
      <div class="task-history">
        <h3>{{ t('system.cleanup.history.title') }}</h3>
        <el-table
          :data="taskHistory"
          v-loading="historyLoading"
          border
          style="width: 100%"
        >
          <el-table-column :label="t('system.cleanup.history.columns.taskId')" width="270">
            <template #default="{ row }">
              <span>{{ row.task_id }}</span>
              <el-tag
                v-if="row.task_id === latestScanTaskId"
                size="small"
                type="info"
                effect="plain"
                class="history-tag"
              >
                {{ t('system.cleanup.history.latestScan') }}
              </el-tag>
              <el-tag
                v-if="row.task_id === latestExecuteTaskId"
                size="small"
                type="success"
                effect="plain"
                class="history-tag"
              >
                {{ t('system.cleanup.history.latestExecute') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.actionType')" width="100">
            <template #default="{ row }">
              <el-tag :type="row.action === 'scan' ? 'info' : 'warning'">
                {{ row.action === 'scan' ? t('system.cleanup.history.actionScan') : t('system.cleanup.history.actionCleanup') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.cleanupMode')" width="120">
            <template #default="{ row }">
              <el-tag v-if="row.cleanup_mode === 'logical_cleanup'" type="warning">{{ t('system.cleanup.history.logicalCleanup') }}</el-tag>
              <el-tag v-else-if="row.cleanup_mode === 'physical_cleanup'" type="danger">{{ t('system.cleanup.history.physicalCleanup') }}</el-tag>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.triggerType')" width="110">
            <template #default="{ row }">
              <el-tag :type="getTriggerTypeTag(row.trigger_type)">
                {{ getTriggerTypeText(row.trigger_type) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.causeEvent')" width="150">
            <template #default="{ row }">
              {{ getCauseEventText(row.cause_event) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.basedOnScan')" width="220">
            <template #default="{ row }">
              {{ row.based_on_scan || '-' }}
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.context')" min-width="180">
            <template #default="{ row }">
              <span class="context-cell">{{ formatContext(row.context) }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.status')" width="100">
            <template #default="{ row }">
              {{ getStatusText(row.status) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.scope')" min-width="180" show-overflow-tooltip>
            <template #default="{ row }">
              {{ formatModules(row.expected_modules) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.startTime')" width="180">
            <template #default="{ row }">
              {{ formatTime(row.started_at) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.actions')" fixed="right" width="240">
            <template #default="{ row }">
              <el-button
                type="primary"
                link
                @click="viewTaskDetail(row.task_id)"
              >
                {{ t('system.cleanup.history.viewDetail') }}
              </el-button>
              <el-button
                type="primary"
                link
                @click="openAuditLogs(row.task_id)"
              >
                {{ t('system.cleanup.actions.viewAudit') }}
              </el-button>
              <el-button
                v-if="row.execution_id"
                type="primary"
                link
                @click="openMonitor(row.execution_id)"
              >
                {{ t('system.cleanup.actions.viewMonitor') }}
              </el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-pagination v-model:current-page="historyPage" :disabled="historyLoading" :page-size="10" :total="historyTotal" layout="prev, pager, next, total" @current-change="loadHistory" />
      </div>
    </el-card>

    <!-- 任务详情对话框 -->
    <el-dialog
      v-model="detailDialogVisible"
      :title="t('system.cleanup.detail.title')"
      width="70%"
    >
      <div v-if="taskDetail" class="detail-view">
        <el-descriptions :title="t('system.cleanup.detail.summaryTitle')" :column="2" border>
          <el-descriptions-item :label="t('system.cleanup.detail.taskId')">
            {{ taskDetail.task_id }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.action')">
            {{ getActionText(taskDetail.action) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.status')">
            <el-tag :type="getTaskStatusType(taskDetail.status)">
              {{ getStatusText(taskDetail.status) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.cleanupMode')">
            {{ getCleanupModeText(taskDetail.task?.cleanup_mode) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.basedOnScan')">
            {{ taskDetail.task?.based_on_scan || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.scope')">
            {{ formatModules(taskDetail.task?.expected_modules) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.triggerType')">
            {{ getTriggerTypeText(taskDetail.task?.trigger_type) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.causeEvent')">
            {{ getCauseEventText(taskDetail.task?.cause_event) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.riskLevel')">
            <el-tag :type="getRiskLevelType(taskDetailSummary.risk_level)">
              {{ getRiskLevelText(taskDetailSummary.risk_level || 'unknown') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.releasedStorage')">
            {{ formatBytes(taskDetailSummary.freed_bytes) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.startedAt')">
            {{ formatTime(taskDetail.task?.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.completedAt')">
            {{ formatTime(taskDetail.task?.completed_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.monitor')" v-if="taskDetail.task?.execution_id">
            <el-button type="primary" link @click="openMonitor(taskDetail.task.execution_id)">
              {{ t('system.cleanup.actions.viewMonitor') }}
            </el-button>
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.detail.audit')">
            <el-button type="primary" link @click="openAuditLogs(taskDetail.task_id)">
              {{ t('system.cleanup.actions.viewAudit') }}
            </el-button>
          </el-descriptions-item>
        </el-descriptions>

        <CleanupModuleResults :executions="taskDetail.module_executions" :request-id="taskDetail.task_id" :identity-generation="identityGeneration" :can-read-result="canReadResult" :read-result="cleanupApi.readProfessionalResult" />


      </div>
      <el-alert v-else-if="detailFailed" :title="t('common.cleanupObservation.errors.unavailable')" type="error" :closable="false" />
      <el-skeleton v-else :rows="10" animated />
    </el-dialog>

    <el-dialog
      v-model="executeConfirmVisible"
      :title="executeConfirmTitle"
      width="560px"
      :close-on-click-modal="false"
    >
      <el-alert
        :title="executeConfirmAlertTitle"
        :type="executeConfirmMode === 'physical_cleanup' ? 'error' : 'warning'"
        :closable="false"
        class="confirm-alert"
      />
      <el-descriptions :column="1" border class="confirm-summary">
        <el-descriptions-item :label="t('system.cleanup.confirm.basedOnScan')">
          {{ scanResult?.task_id || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('system.cleanup.confirm.riskLevel')">
          <el-tag :type="getRiskLevelType(reclaimOverview.risk)">
            {{ getRiskLevelText(reclaimOverview.risk) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('system.cleanup.confirm.scope')">
          {{ formatModules(scanResult?.task?.expected_modules) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('system.cleanup.confirm.impact')">
          {{ t('system.cleanup.confirm.impactValue', {
            items: reclaimOverview.items,
            records: reclaimOverview.affectedRecords,
            space: formatBytes(reclaimOverview.freedBytes)
          }) }}
        </el-descriptions-item>
      </el-descriptions>
      <el-form label-position="top">
        <el-form-item
          v-if="executeConfirmMode === 'physical_cleanup'"
          :label="t('system.cleanup.confirm.externalArtifactPolicy')"
        >
          <el-radio-group v-model="executeExternalArtifactPolicy">
            <el-radio-button value="delete">
              {{ t('system.cleanup.confirm.deleteExternalArtifacts') }}
            </el-radio-button>
            <el-radio-button value="abandon">
              {{ t('system.cleanup.confirm.abandonExternalArtifacts') }}
            </el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-alert
          v-if="executeConfirmMode === 'physical_cleanup' && executeExternalArtifactPolicy === 'abandon'"
          :title="t('system.cleanup.confirm.abandonExternalWarning')"
          type="error"
          :closable="false"
          show-icon
          class="confirm-policy-alert"
        />
        <el-form-item>
          <el-checkbox v-model="executeConfirmChecked">
            {{ t('system.cleanup.confirm.confirmChecked') }}
          </el-checkbox>
        </el-form-item>
        <el-form-item
          v-if="requiresConfirmationToken"
          :label="t('system.cleanup.confirm.tokenLabel', { token: confirmationTokenText })"
        >
          <el-input
            v-model="executeConfirmToken"
            :placeholder="confirmationTokenText"
            autocomplete="off"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="executeConfirmVisible = false">
          {{ t('system.cleanup.actions.cancel') }}
        </el-button>
        <el-button
          :type="executeConfirmMode === 'physical_cleanup' ? 'danger' : 'warning'"
          :disabled="!canSubmitExecuteConfirm"
          :loading="executeLoading"
          @click="submitExecuteConfirm"
        >
          {{ executeConfirmButtonText }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, watch, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter, onBeforeRouteLeave } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Search, Refresh, Delete, WarningFilled, Monitor } from '@element-plus/icons-vue'
import { openMonitorExecution, openMonitorExecutions, CleanupModuleResults, parseCleanupExecutions, createLatestRequestScope } from '@common-ui'
import { cleanupApi } from '../api/cleanup'
import { useAuthStore } from '../store/auth'
import {
  formatCleanupStateChanges,
  selectCleanupHistoryResults
} from '../utils/cleanupPresentation'
import { navigateSystemRoute } from '../utils/moduleNavigation'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()

const scanLoading = ref(false)
const executeLoading = ref(false)
const historyLoading = ref(false)
const currentTask = ref(null)
const scanResult = ref(null)
const latestResult = ref(null)
const taskHistory = ref([])
const detailDialogVisible = ref(false)
const taskDetail = ref(null)
const detailFailed = ref(false)
const executeConfirmVisible = ref(false)
const executeConfirmMode = ref('')
const executeConfirmChecked = ref(false)
const executeConfirmToken = ref('')
const executeExternalArtifactPolicy = ref('delete')
const latestScanTaskId = ref('')
const latestExecuteTaskId = ref('')

const confirmationTokenText = 'CONFIRM'

const isTenantScopeRequired = computed(() => {
  return authStore.contextType !== 'tenant'
})

const identityGeneration = ref(0)
const historyPage = ref(1), historyTotal = ref(0)
const historyRequests = createLatestRequestScope(), detailRequests = createLatestRequestScope(), polling = createLatestRequestScope()
let pollTimer, scanRequestId = '', executeAcceptance = null
const canReadResult = owner => authStore.hasPermission('system.cleanup.read') && authStore.hasPermission(`${owner}.cleanup_result.read`)
function checkedResponse(response) { parseCleanupExecutions(response); return response }
const overview = result => {
  const summary = result?.summary
  return { items: summary ? (summary.scanned_items ?? 0) : null, affectedRecords: summary ? (summary.affected_records ?? 0) : null,
    primaryCount: summary ? (result.action === 'execute' ? summary.affected_records ?? 0 : summary.scanned_items ?? 0) : '—',
    risk: summary?.risk_level || 'unknown', freedBytes: summary ? (summary.freed_bytes ?? 0) : null,
    modules: result?.module_executions?.length ?? '—' }
}
const reclaimOverview = computed(() => overview(scanResult.value))
const latestResultOverview = computed(() => overview(latestResult.value))
const taskDetailSummary = computed(() => taskDetail.value?.summary || {})

const resultBannerTitle = computed(() => {
  if (!latestResult.value) return ''
  return latestResult.value.action === 'execute'
    ? t('system.cleanup.executeResult.bannerTitle')
    : t('system.cleanup.scanResult.bannerTitle')
})

const resultBannerDescription = computed(() => {
  if (!latestResult.value) return ''
  if (latestResult.value.action === 'execute') {
    return t('system.cleanup.executeResult.bannerDesc', {
      records: latestResultOverview.value.affectedRecords ?? '—',
      space: formatBytes(latestResultOverview.value.freedBytes)
    })
  }
  return t('system.cleanup.scanResult.bannerDesc', {
    items: latestResultOverview.value.items ?? '—',
    modules: latestResultOverview.value.modules
  })
})

const requiresConfirmationToken = computed(() => {
  return executeConfirmMode.value === 'physical_cleanup' || reclaimOverview.value.risk === 'high'
})

const canSubmitExecuteConfirm = computed(() => {
  const hasPhysicalPolicy = executeConfirmMode.value !== 'physical_cleanup' || ['delete', 'abandon'].includes(executeExternalArtifactPolicy.value)
  return hasPhysicalPolicy && executeConfirmChecked.value && (!requiresConfirmationToken.value || executeConfirmToken.value === confirmationTokenText)
})

const executeConfirmTitle = computed(() => {
  return executeConfirmMode.value === 'physical_cleanup'
    ? t('system.cleanup.confirm.physicalTitle')
    : t('system.cleanup.confirm.logicalTitle')
})

const executeConfirmAlertTitle = computed(() => {
  return executeConfirmMode.value === 'physical_cleanup'
    ? t('system.cleanup.confirm.physicalAlert')
    : t('system.cleanup.confirm.logicalAlert')
})

const executeConfirmButtonText = computed(() => {
  if (executeConfirmMode.value === 'physical_cleanup' && executeExternalArtifactPolicy.value === 'abandon') {
    return t('system.cleanup.actions.abandonExternalArtifacts')
  }
  return executeConfirmMode.value === 'physical_cleanup'
    ? t('system.cleanup.actions.physicalCleanup')
    : t('system.cleanup.actions.logicalCleanup')
})

const warnTenantScopeRequired = () => {
  ElMessage.warning(t('system.cleanup.msg.tenantScopeRequired'))
}

// 开始评估
const startScan = async () => {
  if (isTenantScopeRequired.value) {
    warnTenantScopeRequired()
    return
  }

  const generation = identityGeneration.value
  try {
    scanLoading.value = true
    scanRequestId ||= crypto.randomUUID()
    const response = await cleanupApi.createScanTask({ request_id: scanRequestId })
    if (generation !== identityGeneration.value) return
    scanRequestId = ''
    const taskId = response.task_id
    latestResult.value = null

    ElMessage.success(t('system.cleanup.msg.scanCreated'))

    // 轮询任务状态
    await pollTaskStatus(taskId)
  } catch (error) {
    if (generation === identityGeneration.value) ElMessage.error(t('system.cleanup.msg.scanFailed', { error: '' }))
  } finally {
    if (generation === identityGeneration.value) scanLoading.value = false
  }
}

const openExecuteConfirm = (cleanupMode) => {
  if (isTenantScopeRequired.value) {
    warnTenantScopeRequired()
    return
  }
  if (!scanResult.value || scanResult.value.task?.cause_event || scanResult.value.status !== 'success') {
    ElMessage.warning(t('system.cleanup.msg.noScanFirst'))
    return
  }
  executeConfirmMode.value = cleanupMode
  executeConfirmChecked.value = false
  executeConfirmToken.value = ''
  executeExternalArtifactPolicy.value = 'delete'
  executeConfirmVisible.value = true
}

const submitExecuteConfirm = async () => {
  if (isTenantScopeRequired.value) {
    warnTenantScopeRequired()
    return
  }
  if (!canSubmitExecuteConfirm.value) {
    ElMessage.warning(t('system.cleanup.msg.confirmRequired'))
    return
  }
  await executeCleanup(executeConfirmMode.value, {
    confirmed: executeConfirmChecked.value,
    confirmation_token: executeConfirmToken.value,
    external_artifact_policy: executeConfirmMode.value === 'physical_cleanup'
      ? executeExternalArtifactPolicy.value
      : undefined
  })
}

// 执行资源回收
const executeCleanup = async (cleanupMode, confirmation) => {
  if (isTenantScopeRequired.value) {
    warnTenantScopeRequired()
    return
  }

  if (!scanResult.value || scanResult.value.task?.cause_event || scanResult.value.status !== 'success') {
    ElMessage.warning(t('system.cleanup.msg.noScanFirst'))
    return
  }

  const generation = identityGeneration.value
  try {
    executeLoading.value = true
    const payload = {
      based_on_scan: scanResult.value.task_id,
      cleanup_mode: cleanupMode,
      confirmed: confirmation?.confirmed === true,
      confirmation_token: confirmation?.confirmation_token || '',
      external_artifact_policy: confirmation?.external_artifact_policy
    }
    const intent = JSON.stringify(payload)
    if (executeAcceptance?.intent !== intent) executeAcceptance = { intent, request_id: crypto.randomUUID() }
    const response = await cleanupApi.createExecuteTask({ ...payload, request_id: executeAcceptance.request_id })
    if (generation !== identityGeneration.value) return
    executeAcceptance = null

    ElMessage.success(t('system.cleanup.msg.cleanupCreated'))
    executeConfirmVisible.value = false

    // 轮询任务状态
    await pollTaskStatus(response.task_id)
  } catch (error) {
    if (generation === identityGeneration.value) ElMessage.error(error?.response?.data?.error_code === 'cleanup_manager_scope_required' ? t('system.cleanup.msg.managerScopeRequired') : t('system.cleanup.msg.cleanupFailed', { error: '' }))
  } finally {
    if (generation === identityGeneration.value) executeLoading.value = false
  }
}

// 每次观察都重新读取已授权事实，失败即清除本次视图。
const pollTaskStatus = async taskId => {
  clearTimeout(pollTimer)
  const ticket = polling.invalidate()
  currentTask.value = null
  let attempts = 0
  const poll = async () => {
    try {
      const status = checkedResponse(await polling.request(options => cleanupApi.getTaskStatus(taskId, options)))
      if (!polling.current(ticket)) return
      currentTask.value = status
      if (['success', 'failed', 'timeout', 'cancelled'].includes(status.status)) {
        latestResult.value = status
        if (status.action === 'scan' && status.status === 'success' && !status.task.cause_event) scanResult.value = status
        await loadHistory()
        return
      }
      if (++attempts < 160) pollTimer = setTimeout(poll, 2000)
    } catch {
      if (!polling.current(ticket)) return
      currentTask.value = null; latestResult.value = null; scanResult.value = null
      ElMessage.error(t('common.cleanupObservation.errors.unavailable'))
    }
  }
  await poll()
}

const loadHistory = async () => {
  const ticket = historyRequests.invalidate()
  taskHistory.value = []; latestResult.value = null; scanResult.value = null
  latestScanTaskId.value = ''; latestExecuteTaskId.value = ''
  if (isTenantScopeRequired.value) return
  historyLoading.value = true
  try {
    const response = await historyRequests.request(options => cleanupApi.getTaskHistory({ page: historyPage.value, page_size: 10 }, options))
    if (!historyRequests.current(ticket)) return
    if (!Array.isArray(response.tasks) || !Number.isSafeInteger(response.total) || response.total < 0) throw new Error('cleanup_history_invalid')
    taskHistory.value = response.tasks; historyTotal.value = response.total
    if (historyPage.value !== 1) return
    const selected = selectCleanupHistoryResults(response.tasks)
    latestScanTaskId.value = selected.latestScanTaskId; latestExecuteTaskId.value = selected.latestExecuteTaskId
    const ids = [...new Set([selected.latestCompletedScanTaskId, selected.latestCompletedResultTaskId].filter(Boolean))]
    const entries = await Promise.all(ids.map(async id => [id, checkedResponse(await historyRequests.request(options => cleanupApi.getTaskStatus(id, options)))]))
    if (!historyRequests.current(ticket)) return
    const results = new Map(entries)
    scanResult.value = results.get(selected.latestCompletedScanTaskId) || null
    latestResult.value = results.get(selected.latestCompletedResultTaskId) || null
  } catch {
    if (!historyRequests.current(ticket)) return
    taskHistory.value = []; latestResult.value = null; scanResult.value = null; historyTotal.value = 0
    ElMessage.error(t('common.cleanupObservation.errors.unavailable'))
  } finally { if (historyRequests.current(ticket)) historyLoading.value = false }
}
const viewTaskDetail = async taskId => {
  const ticket = detailRequests.invalidate()
  detailDialogVisible.value = true; taskDetail.value = null; detailFailed.value = false
  try {
    const detail = checkedResponse(await detailRequests.request(options => cleanupApi.getTaskStatus(taskId, options)))
    if (detailRequests.current(ticket)) taskDetail.value = detail
  } catch {
    if (detailRequests.current(ticket)) { taskDetail.value = null; detailFailed.value = true; ElMessage.error(t('system.cleanup.msg.detailFailed')) }
  }
}
const invalidateViews = () => {
  identityGeneration.value++
  historyRequests.invalidate(); detailRequests.invalidate(); polling.invalidate(); clearTimeout(pollTimer)
  currentTask.value = null; scanResult.value = null; latestResult.value = null; taskDetail.value = null; taskHistory.value = []
  detailDialogVisible.value = false; executeConfirmVisible.value = false
  historyTotal.value = 0; historyPage.value = 1
  scanRequestId = ''; executeAcceptance = null
  scanLoading.value = false; executeLoading.value = false; historyLoading.value = false
}
watch(() => [authStore.token, authStore.authContext, authStore.permissions.join('|')], invalidateViews, { flush: 'sync' })
watch(detailDialogVisible, open => { if (!open) { detailRequests.invalidate(); taskDetail.value = null } })
onBeforeRouteLeave(invalidateViews)
onBeforeUnmount(() => { invalidateViews(); historyRequests.dispose(); detailRequests.dispose(); polling.dispose() })

const openMonitor = async (executionId) => {
  if (!executionId) return
  await openMonitorExecution(executionId)
}

const openCleanupMonitor = async () => {
  await openMonitorExecutions({
    module: 'system',
    task_type: 'cleanup'
  })
}

const openAuditLogs = async (taskId) => {
  if (!taskId) return
  await navigateSystemRoute(router, {
    name: 'IAMSecurity',
    query: {
      tab: 'audit',
      module_name: 'system',
      entity_type: 'cleanup',
      entity_id: taskId
    }
  })
}

// 格式化时间
const formatTime = (time) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

// 获取状态文本
const getStatusText = (status) => {
  const statusMap = {
    pending: t('system.cleanup.status.pending'),
    running: t('system.cleanup.status.running'),
    success: t('system.cleanup.status.completed'),
    timeout: t('common.cleanupObservation.status.timeout'),
    cancelled: t('common.cleanupObservation.status.cancelled'),
    failed: t('system.cleanup.status.failed')
  }
  return statusMap[status] || status
}

const getTaskStatusType = (status) => {
  const typeMap = {
    pending: 'info',
    running: 'warning',
    success: 'success',
    timeout: 'danger',
    cancelled: 'info',
    failed: 'danger'
  }
  return typeMap[status] || 'info'
}

const getActionText = (action) => {
  if (action === 'scan') return t('system.cleanup.history.actionScan')
  if (action === 'execute') return t('system.cleanup.history.actionCleanup')
  return action || '-'
}

const getCleanupModeText = (cleanupMode) => {
  if (cleanupMode === 'logical_cleanup') return t('system.cleanup.history.logicalCleanup')
  if (cleanupMode === 'physical_cleanup') return t('system.cleanup.history.physicalCleanup')
  return '-'
}

const getRiskLevelText = (level) => {
  const levelMap = {
    unknown: '—',
    low: t('system.cleanup.risk.low'),
    medium: t('system.cleanup.risk.medium'),
    high: t('system.cleanup.risk.high')
  }
  return levelMap[level] || level
}

// 获取风险等级标签类型
const getRiskLevelType = (level) => {
  const typeMap = {
    low: 'success',
    medium: 'warning',
    high: 'danger'
  }
  return typeMap[level] || 'info'
}

const getTriggerTypeText = (triggerType) => {
  const textMap = {
    manual: t('system.cleanup.trigger.manual'),
    scheduled: t('system.cleanup.trigger.scheduled'),
    event: t('system.cleanup.trigger.event')
  }
  return textMap[triggerType] || triggerType || '-'
}

const getTriggerTypeTag = (triggerType) => {
  const typeMap = {
    manual: 'info',
    scheduled: 'success',
    event: 'warning'
  }
  return typeMap[triggerType] || 'info'
}

const getCauseEventText = (causeEvent) => {
  if (!causeEvent) return '-'
  const key = `system.cleanup.causeEvent.${causeEvent.replaceAll('.', '_')}`
  const translated = t(key)
  return translated === key ? causeEvent : translated
}

const getModuleStatusText = (status) => {
  const statusMap = {
    success: t('system.cleanup.moduleStatus.success'),
    failed: t('system.cleanup.moduleStatus.failed'),
    partial_success: t('system.cleanup.moduleStatus.partialSuccess'),
    skipped: t('system.cleanup.moduleStatus.skipped'),
    timeout: t('system.cleanup.moduleStatus.timeout')
  }
  return statusMap[status] || status
}

const getModuleStatusType = (status) => {
  const typeMap = {
    success: 'success',
    failed: 'danger',
    partial_success: 'warning',
    skipped: 'info',
    timeout: 'danger'
  }
  return typeMap[status] || 'info'
}

const getModuleName = (module) => {
  if (!module) return '-'
  const key = `system.cleanup.modules.names.${module}`
  const translated = t(key)
  return translated === key ? module : translated
}

const formatModules = (modules) => {
  if (!Array.isArray(modules) || modules.length === 0) return '-'
  return modules.map((module) => getModuleName(module)).join(', ')
}

const formatBytes = (value) => {
  if (value == null) return '—'
  const bytes = Number(value)
  if (bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, index)).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

const formatStateChanges = (summary) => formatCleanupStateChanges(summary, t)

const formatContext = (context) => {
  if (!context || Object.keys(context).length === 0) return '-'
  return Object.entries(context)
    .filter(([, value]) => value !== undefined && value !== null && value !== '')
    .map(([key, value]) => `${key}=${value}`)
    .join(', ') || '-'
}

onMounted(async () => {
  if (!authStore.user) {
    try {
      await authStore.fetchUser()
    } catch (error) {
      console.error('Failed to load user:', error)
    }
  }
  await loadHistory()
})
</script>

<style scoped>
.page-container {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}

.page-title {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.page-title span {
  font-size: 16px;
  font-weight: 600;
}

.page-title small {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.header-actions {
  display: flex;
  gap: 10px;
}

.scan-result {
  margin-top: 20px;
}

.tenant-scope-alert {
  margin-bottom: 20px;
}

.cleanup-empty {
  padding: 44px 0 32px;
}

.result-banner {
  margin-bottom: 16px;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 18px;
}

.summary-tile {
  min-height: 72px;
  padding: 14px 16px;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  background: var(--addp-bg-secondary);
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 8px;
}

.summary-tile strong {
  color: var(--el-text-color-primary);
  font-size: 20px;
  line-height: 1;
}

.summary-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.result-descriptions {
  margin-top: 4px;
}

.module-result {
  margin-top: 30px;
  padding: 20px;
  background: var(--addp-bg-secondary);
  border-radius: 4px;
}

.module-result h3 {
  margin-top: 0;
  margin-bottom: 20px;
}

.cleanup-actions {
  margin-top: 30px;
  padding: 20px;
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
}

.confirm-alert {
  margin-bottom: 16px;
}

.confirm-summary {
  margin-bottom: 16px;
}

.confirm-policy-alert {
  margin-bottom: 16px;
}

.detail-view {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.detail-section h3 {
  margin: 0 0 12px;
}

.raw-json {
  max-height: 360px;
  overflow: auto;
  margin: 0;
  padding: 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}

.task-history {
  margin-top: 40px;
}

.task-history h3 {
  margin-bottom: 15px;
}

.context-cell {
  word-break: break-all;
}

.history-tag {
  margin-left: 8px;
}

@media (max-width: 960px) {
  .card-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .summary-grid {
    grid-template-columns: 1fr;
  }
}
</style>
