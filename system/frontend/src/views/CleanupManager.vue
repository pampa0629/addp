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
        <el-progress
          v-if="currentTask.progress"
          :percentage="Math.round((currentTask.progress.completed / currentTask.progress.total) * 100)"
          :status="currentTask.status === 'completed' ? 'success' : undefined"
        />
      </el-alert>

      <!-- 扫描结果 -->
      <div v-if="scanResult" class="scan-result">
        <div class="summary-grid">
          <div class="summary-tile">
            <span class="summary-label">{{ t('system.cleanup.overview.items') }}</span>
            <strong>{{ reclaimOverview.items }}</strong>
          </div>
          <div class="summary-tile">
            <span class="summary-label">{{ t('system.cleanup.overview.risk') }}</span>
            <el-tag :type="getRiskLevelType(reclaimOverview.risk)">
              {{ getRiskLevelText(reclaimOverview.risk) }}
            </el-tag>
          </div>
          <div class="summary-tile">
            <span class="summary-label">{{ t('system.cleanup.overview.freedBytes') }}</span>
            <strong>{{ formatBytes(reclaimOverview.freedBytes) }}</strong>
          </div>
          <div class="summary-tile">
            <span class="summary-label">{{ t('system.cleanup.overview.modules') }}</span>
            <strong>{{ reclaimOverview.modules }}</strong>
          </div>
        </div>

        <el-descriptions :title="t('system.cleanup.scanResult.title')" :column="2" border class="result-descriptions">
          <el-descriptions-item :label="t('system.cleanup.scanResult.scanTime')">
            {{ formatTime(scanResult.task.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.scanScope')">
            {{ formatModules(scanResult.task.expected_modules) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.triggerType')">
            <el-tag :type="getTriggerTypeTag(scanResult.task.trigger_type)">
              {{ getTriggerTypeText(scanResult.task.trigger_type) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.causeEvent')">
            {{ getCauseEventText(scanResult.task.cause_event) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.context')">
            {{ formatContext(scanResult.task.context) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.riskLevel')">
            <el-tag :type="getRiskLevelType(scanResult.summary.risk_level)">
              {{ getRiskLevelText(scanResult.summary.risk_level) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.itemsToClean')">
            {{ t('system.cleanup.scanResult.items', { count: reclaimOverview.items }) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('system.cleanup.scanResult.monitor')" v-if="scanResult.task.execution_id">
            <el-button type="primary" link @click="openMonitor(scanResult.task.execution_id)">
              {{ t('system.cleanup.actions.viewMonitor') }}
            </el-button>
          </el-descriptions-item>
        </el-descriptions>

        <!-- 模块扫描详情 -->
        <div v-if="moduleResults.length > 0" class="module-result">
          <h3>{{ t('system.cleanup.modules.title') }}</h3>
          <el-table :data="moduleResults" border>
            <el-table-column :label="t('system.cleanup.modules.columns.module')" width="130">
              <template #default="{ row }">
                {{ getModuleName(row.module) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('system.cleanup.modules.columns.status')" width="120">
              <template #default="{ row }">
                <el-tag :type="getModuleStatusType(row.status)">
                  {{ getModuleStatusText(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="summary.scanned_items" :label="t('system.cleanup.modules.columns.scannedItems')" width="120" />
            <el-table-column prop="summary.affected_records" :label="t('system.cleanup.modules.columns.affectedRecords')" width="130" />
            <el-table-column :label="t('system.cleanup.modules.columns.stateChanges')" min-width="180">
              <template #default="{ row }">
                {{ formatStateChanges(row.summary) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('system.cleanup.modules.columns.releasedStorage')" min-width="160">
              <template #default="{ row }">
                {{ t('system.cleanup.modules.releasedStorageValue', {
                  artifacts: row.summary.deleted_physical_artifacts || 0,
                  bytes: formatBytes(row.summary.freed_bytes)
                }) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('system.cleanup.modules.columns.exceptions')" min-width="150">
              <template #default="{ row }">
                {{ t('system.cleanup.modules.exceptionsValue', {
                  skipped: row.summary.skipped_items || 0,
                  errors: row.summary.error_count || 0
                }) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('system.cleanup.modules.columns.riskLevel')" width="100">
              <template #default="{ row }">
                <el-tag :type="getRiskLevelType(row.summary.risk_level)">
                  {{ getRiskLevelText(row.summary.risk_level || 'low') }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </div>

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
              type="warning"
              :icon="WarningFilled"
              :loading="executeLoading"
              @click="executeCleanup('logical_cleanup')"
            >
              {{ t('system.cleanup.actions.logicalCleanup') }}
            </el-button>
            <el-popconfirm
              :title="t('system.cleanup.actions.physicalCleanupConfirm')"
              :confirm-button-text="t('system.cleanup.actions.confirm')"
              :cancel-button-text="t('system.cleanup.actions.cancel')"
              @confirm="executeCleanup('physical_cleanup')"
            >
              <template #reference>
                <el-button
                  type="danger"
                  :icon="Delete"
                  :loading="executeLoading"
                >
                  {{ t('system.cleanup.actions.physicalCleanup') }}
                </el-button>
              </template>
            </el-popconfirm>
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
          <el-table-column prop="task_id" :label="t('system.cleanup.history.columns.taskId')" width="250" />
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
          <el-table-column :label="t('system.cleanup.history.columns.scope')" width="100">
            <template #default="{ row }">
              {{ formatModules(row.expected_modules) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.startTime')" width="180">
            <template #default="{ row }">
              {{ formatTime(row.started_at) }}
            </template>
          </el-table-column>
          <el-table-column :label="t('system.cleanup.history.columns.actions')" fixed="right" width="180">
            <template #default="{ row }">
              <el-button
                type="primary"
                link
                @click="viewTaskDetail(row.task_id)"
              >
                {{ t('system.cleanup.history.viewDetail') }}
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
      </div>
    </el-card>

    <!-- 任务详情对话框 -->
    <el-dialog
      v-model="detailDialogVisible"
      :title="t('system.cleanup.detail.title')"
      width="70%"
    >
      <pre v-if="taskDetail" style="max-height: 500px; overflow: auto">{{ JSON.stringify(taskDetail, null, 2) }}</pre>
      <el-skeleton v-else :rows="10" animated />
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Search, Refresh, Delete, WarningFilled, Monitor } from '@element-plus/icons-vue'
import { openMonitorExecution, openMonitorExecutions } from '@common-ui'
import { cleanupApi } from '../api/cleanup'

const { t } = useI18n()

const scanLoading = ref(false)
const executeLoading = ref(false)
const historyLoading = ref(false)
const currentTask = ref(null)
const scanResult = ref(null)
const taskHistory = ref([])
const detailDialogVisible = ref(false)
const taskDetail = ref(null)

const emptySummary = {
  scanned_items: 0,
  affected_records: 0,
  deleted_physical_artifacts: 0,
  freed_bytes: 0,
  marked_missing_source: 0,
  marked_outdated: 0,
  disabled_task_definitions: 0,
  skipped_items: 0,
  error_count: 0,
  risk_level: 'low'
}

const moduleResults = computed(() => {
  const results = scanResult.value?.results || {}
  return Object.entries(results)
    .map(([module, result]) => ({
      module,
      status: result?.status || 'unknown',
      summary: {
        ...emptySummary,
        ...(result?.summary || {})
      }
    }))
    .sort((left, right) => left.module.localeCompare(right.module))
})

const reclaimOverview = computed(() => {
  const summary = scanResult.value?.summary || {}
  return {
    items: Number(summary.scanned_items ?? summary.total_items_to_clean ?? 0),
    risk: summary.risk_level || 'low',
    freedBytes: Number(summary.freed_bytes || 0),
    modules: scanResult.value?.task?.expected_modules?.length || moduleResults.value.length || 0
  }
})

// 开始评估
const startScan = async () => {
  try {
    scanLoading.value = true
    const response = await cleanupApi.createScanTask({})
    const taskId = response.task_id

    ElMessage.success(t('system.cleanup.msg.scanCreated'))

    // 轮询任务状态
    await pollTaskStatus(taskId)
  } catch (error) {
    ElMessage.error(t('system.cleanup.msg.scanFailed', { error: error.message || '' }))
  } finally {
    scanLoading.value = false
  }
}

// 执行资源回收
const executeCleanup = async (cleanupMode) => {
  if (!scanResult.value) {
    ElMessage.warning(t('system.cleanup.msg.noScanFirst'))
    return
  }

  try {
    executeLoading.value = true
    const response = await cleanupApi.createExecuteTask({
      based_on_scan: scanResult.value.task_id,
      cleanup_mode: cleanupMode
    })

    ElMessage.success(t('system.cleanup.msg.cleanupCreated'))

    // 轮询任务状态
    await pollTaskStatus(response.task_id)
  } catch (error) {
    ElMessage.error(t('system.cleanup.msg.cleanupFailed', { error: error.message || '' }))
  } finally {
    executeLoading.value = false
  }
}

// 轮询任务状态
const pollTaskStatus = async (taskId) => {
  const maxAttempts = 20
  let attempts = 0

  const poll = async () => {
    try {
      const status = await cleanupApi.getTaskStatus(taskId)
      currentTask.value = status

      if (status.status === 'completed' || status.status === 'completed_with_errors') {
        // 评估完成，显示结果
        if (status.action === 'scan') {
          scanResult.value = status
        }
        // 刷新历史
        await loadHistory()
        return
      }

      if (status.status === 'failed') {
        ElMessage.error(t('system.cleanup.msg.taskFailed'))
        return
      }

      // 继续轮询
      if (attempts < maxAttempts) {
        attempts++
        setTimeout(poll, 2000)
      }
    } catch (error) {
      console.error('查询任务状态失败:', error)
    }
  }

  poll()
}

// 加载任务历史
const loadHistory = async () => {
  try {
    historyLoading.value = true
    const response = await cleanupApi.getTaskHistory({ page: 1, page_size: 10 })
    taskHistory.value = response.tasks || []
  } catch (error) {
    console.error('加载任务历史失败:', error)
  } finally {
    historyLoading.value = false
  }
}

// 查看任务详情
const viewTaskDetail = async (taskId) => {
  try {
    detailDialogVisible.value = true
    taskDetail.value = null
    const detail = await cleanupApi.getTaskStatus(taskId)
    taskDetail.value = detail
  } catch (error) {
    ElMessage.error(t('system.cleanup.msg.detailFailed'))
  }
}

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
    completed: t('system.cleanup.status.completed'),
    completed_with_errors: t('system.cleanup.status.completedWithErrors'),
    failed: t('system.cleanup.status.failed')
  }
  return statusMap[status] || status
}

const getRiskLevelText = (level) => {
  const levelMap = {
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
  const bytes = Number(value || 0)
  if (bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, index)).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

const formatStateChanges = (summary) => {
  const parts = []
  const missing = Number(summary?.marked_missing_source || 0)
  const outdated = Number(summary?.marked_outdated || 0)
  const disabled = Number(summary?.disabled_task_definitions || 0)
  if (missing > 0) parts.push(t('system.cleanup.modules.stateChanges.missing', { count: missing }))
  if (outdated > 0) parts.push(t('system.cleanup.modules.stateChanges.outdated', { count: outdated }))
  if (disabled > 0) parts.push(t('system.cleanup.modules.stateChanges.disabled', { count: disabled }))
  return parts.length > 0 ? parts.join(' / ') : '-'
}

const formatContext = (context) => {
  if (!context || Object.keys(context).length === 0) return '-'
  return Object.entries(context)
    .filter(([, value]) => value !== undefined && value !== null && value !== '')
    .map(([key, value]) => `${key}=${value}`)
    .join(', ') || '-'
}

onMounted(() => {
  loadHistory()
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

.task-history {
  margin-top: 40px;
}

.task-history h3 {
  margin-bottom: 15px;
}

.context-cell {
  word-break: break-all;
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
