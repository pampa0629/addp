<template>
  <div class="execution-detail-page">
    <!-- 顶部导航 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <el-button @click="handleBack">
          <el-icon><ArrowLeft /></el-icon>
          {{ t('develop.executionDetail.back') }}
        </el-button>
        <h2>{{ t('develop.executionDetail.title') }}</h2>
        <el-tag
          :type="getStatusColor(execution?.status)"
          size="large"
          style="margin-left: 16px;"
        >
          <el-icon v-if="execution?.status === 'running'" class="rotating">
            <Loading />
          </el-icon>
          {{ getStatusLabel(execution?.status) }}
        </el-tag>
      </div>
      <div class="toolbar-right">
        <el-button
          v-if="['failed', 'timeout', 'cancelled'].includes(execution?.status)"
          type="success"
          @click="handleRetry"
        >
          <el-icon><Refresh /></el-icon>
          {{ t('develop.executionDetail.reExecute') }}
        </el-button>
        <el-button @click="handleRefresh">
          <el-icon><Refresh /></el-icon>
          {{ t('develop.executionDetail.refresh') }}
        </el-button>
      </div>
    </div>

    <!-- 基本信息卡片 -->
    <div class="info-section">
      <el-card>
        <template #header>
          <div class="card-header">
            <span>{{ t('develop.executionDetail.basicInfo') }}</span>
          </div>
        </template>
        <el-descriptions :column="3" border>
          <el-descriptions-item :label="t('develop.executionDetail.execId')">
            {{ execution?.execution_id }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('develop.executionDetail.taskName')">
            {{ execution?.dev_task?.name || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('develop.executionDetail.taskType')">
            <el-tag :type="getTypeColor(execution?.task_type)">
              {{ getTypeLabel(execution?.task_type) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('develop.executionDetail.triggerType')">
            {{ getTriggerLabel(execution?.trigger_type) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('develop.executionDetail.startTime')">
            {{ formatTime(execution?.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('develop.executionDetail.endTime')">
            {{ formatTime(execution?.completed_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('develop.executionDetail.duration')">
            {{ formatDuration(execution?.execution_time_ms) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('develop.executionDetail.rowsAffected')">
            {{ execution?.rows_affected || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('develop.executionDetail.currentStep')">
            {{ execution?.current_step || '-' }}
          </el-descriptions-item>
        </el-descriptions>

        <!-- 进度条 -->
        <div style="margin-top: 16px;">
          <div style="margin-bottom: 8px; color: var(--addp-text-secondary);">{{ t('develop.executionDetail.progress') }}</div>
          <el-progress
            :percentage="execution?.progress || 0"
            :status="getProgressStatus(execution?.status)"
          />
        </div>
      </el-card>
    </div>

    <!-- Tab 页签 -->
    <div class="content-area">
      <el-tabs v-model="activeTab" type="border-card" @tab-change="handleTabChange">
        <el-tab-pane :label="t('develop.executionDetail.tabResult')" name="result">
          <div class="tab-content">
            <div v-if="execution?.status === 'success' && executionResult">
              <!-- 查询结果: 使用 QueryResult 组件 -->
              <div v-if="execution.task_type === 'query'">
                <QueryResult :result="formatSQLResult()" />
              </div>

              <!-- 工作流结果 -->
              <div v-else-if="execution.task_type === 'workflow'">
                <div class="workflow-result">
                  <div class="workflow-result-heading">
                    <h3>{{ t('develop.executionDetail.workflowResult') }}</h3>
                    <el-tag :type="workflowOutputRows.length ? 'success' : 'info'" effect="plain">
                      {{ workflowOutputRows.length
                        ? t('develop.executionDetail.outputPersisted')
                        : t('develop.executionDetail.outputNotPersisted') }}
                    </el-tag>
                  </div>
                  <el-table
                    v-if="workflowOutputRows.length"
                    :data="workflowOutputRows"
                    stripe
                    border
                  >
                    <el-table-column prop="taskId" :label="t('develop.executionDetail.outputOperator')" min-width="150">
                      <template #default="{ row }">
                        <div class="output-operator">
                          <strong>{{ row.operatorLabel }}</strong>
                          <span>{{ row.taskId }}</span>
                        </div>
                      </template>
                    </el-table-column>
                    <el-table-column prop="engineName" :label="t('develop.executionDetail.outputEngine')" min-width="150" />
                    <el-table-column prop="path" :label="t('develop.executionDetail.outputResource')" min-width="240">
                      <template #default="{ row }">
                        <span class="output-resource" :title="row.path">{{ row.path }}</span>
                      </template>
                    </el-table-column>
                    <el-table-column prop="writeModeLabel" :label="t('develop.executionDetail.writeMode')" width="110" />
                  </el-table>
                  <el-alert
                    v-else
                    type="info"
                    :title="workflowHasTransientResult
                      ? t('develop.executionDetail.transientResultGenerated')
                      : t('develop.executionDetail.noPersistentOutput')"
                    :description="workflowHasTransientResult
                      ? t('develop.executionDetail.transientResultDescription', { size: formatBytes(workflowResultSize) })
                      : t('develop.executionDetail.noPersistentOutputDescription')"
                    :closable="false"
                    show-icon
                  />
                  <div v-if="hasWorkflowFinalResult" class="workflow-final-result">
                    <div class="workflow-final-result-label">{{ t('develop.executionDetail.computedResult') }}</div>
                    <div v-if="workflowFinalResultScalar" class="workflow-final-result-value">
                      {{ formatWorkflowResultValue(workflowFinalResult) }}
                    </div>
                    <pre v-else class="workflow-final-result-json">{{ workflowFinalResultText }}</pre>
                  </div>
                </div>
              </div>

              <!-- 脚本结果: 使用 JSON 展示 -->
              <div v-else>
                <pre class="json-result">{{ JSON.stringify(executionResult, null, 2) }}</pre>
              </div>
            </div>

            <el-empty v-else-if="execution?.status === 'running'" :description="t('develop.executionDetail.resultPending')" />
            <el-empty v-else-if="!executionResult" :description="t('develop.executionDetail.noResult')" />
          </div>
        </el-tab-pane>

        <!-- Tab 2: 执行日志 -->
        <el-tab-pane :label="t('develop.executionDetail.tabLogs')" name="logs">
          <div class="tab-content">
            <el-timeline v-if="logs.length > 0">
              <el-timeline-item
                v-for="(log, index) in logs"
                :key="index"
                :timestamp="formatTime(log.timestamp)"
                :type="getLogType(log.level)"
                placement="top"
              >
                <div class="log-item">
                  <el-tag :type="getLogType(log.level)" size="small">
                    {{ log.level }}
                  </el-tag>
                  <span class="log-message">{{ log.message }}</span>
                </div>
              </el-timeline-item>
            </el-timeline>
            <el-empty v-else :description="t('develop.executionDetail.noLogs')" />
          </div>
        </el-tab-pane>

        <!-- Tab 3: 输入参数 -->
        <el-tab-pane :label="t('develop.executionDetail.tabInputs')" name="inputs">
          <div class="tab-content">
            <pre class="json-result">{{ JSON.stringify(executionInputs, null, 2) }}</pre>
          </div>
        </el-tab-pane>

        <!-- Tab 4: 错误信息 -->
        <el-tab-pane
          v-if="execution?.status === 'failed'"
          :label="t('develop.executionDetail.tabError')"
          name="error"
        >
          <div class="tab-content">
            <el-alert
              type="error"
              :title="executionErrorMessage || t('develop.executionDetail.unknownError')"
              :closable="false"
              show-icon
            >
              <template v-if="executionResult?.traceback">
                <div style="margin-top: 12px; margin-bottom: 8px; font-weight: bold;">{{ t('develop.executionDetail.stackTrace') }}:</div>
                <pre class="error-traceback">{{ executionResult.traceback }}</pre>
              </template>
              <pre v-else style="margin-top: 8px; white-space: pre-wrap;">{{ executionErrorMessage }}</pre>
            </el-alert>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import {
  ArrowLeft,
  Refresh,
  Loading
} from '@element-plus/icons-vue'
import {
  getExecution,
  getExecutionLogs,
  retryExecution
} from '@/api/execution'
import QueryResult from '@/components/QueryResult.vue'
import { navigateDevelopRoute } from '@/utils/developNavigation'
import { resolveExecutionDetailRouteState } from '@/utils/executionDetailRouteState'
import { queryErrorMessage, queryResultFromExecution } from '@/utils/queryWorkbench.mjs'
import { formatLocatorDisplayPath, listResourceTreeEngines, parseLocatorSafe, useConsolePageDescriptor } from '@addp/common-frontend'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

// 状态管理
const execution = ref(null)
useConsolePageDescriptor(router, 'develop', {
  title: computed(() => t('develop.executionDetail.recentVisitTitle')),
  subject: computed(() => execution.value?.dev_task?.name || execution.value?.execution_id || ''),
  ready: computed(() => Boolean(execution.value?.execution_id))
})
const logs = ref([])
const resourceEngines = ref([])
const activeTab = ref(resolveExecutionDetailRouteState(route.query).tab)
let routeDataReady = false

const executionResult = computed(() => execution.value?.metadata?.result || null)
const executionInputs = computed(() => execution.value?.execution_config?.inputs || {})
const resourceEnginesById = computed(() => Object.fromEntries(
  resourceEngines.value.map(engine => [String(engine.id), engine])
))
const workflowTasksById = computed(() => {
  const definition = execution.value?.dev_task?.content?.workflow_definition
    || execution.value?.execution_config?.content?.workflow_definition
  const tasks = Array.isArray(definition?.tasks) ? definition.tasks : []
  return Object.fromEntries(tasks.map(task => [String(task.id), task]))
})
const workflowOutputRows = computed(() => {
  const result = executionResult.value || {}
  const producedTargets = Array.isArray(result.produced_targets) ? result.produced_targets : []
  const outputEntries = producedTargets.length
    ? producedTargets.map(target => [target.task_id || target.taskId, {
      resource: {
        locator: target.locator,
        type: target.type,
        write_mode: target.write_mode
      }
    }])
    : Object.entries(execution.value?.outputs || {})

  return outputEntries.map(([taskId, output]) => {
    const resource = output?.resource || output || {}
    const locator = String(resource.locator || '')
    let parsed = null
    try {
      parsed = locator ? parseLocatorSafe(locator) : null
    } catch {
      parsed = null
    }
    const engine = parsed ? resourceEnginesById.value[String(parsed.engineId)] : null
    const task = workflowTasksById.value[String(taskId)] || {}
    const path = parsed
      ? formatLocatorDisplayPath(locator, { engineType: engine?.engine_type, resourceType: resource.type || parsed.type })
      : String(resource.path || '')
    return {
      taskId: String(taskId || '-'),
      operatorLabel: task.operator || String(taskId || '-'),
      engineName: engine?.name || (parsed ? t('develop.executionDetail.engineFallback', { id: parsed.engineId }) : '-'),
      path: path || '-',
      writeModeLabel: formatWriteMode(resource.write_mode)
    }
  }).filter(row => row.path !== '-')
})
const workflowHasTransientResult = computed(() => Boolean(executionResult.value?.summary?.has_result))
const workflowResultSize = computed(() => Number(executionResult.value?.summary?.result_size_bytes || 0))
const workflowFinalResult = computed(() => executionResult.value?.final_result)
const hasWorkflowFinalResult = computed(() => workflowFinalResult.value !== undefined && workflowFinalResult.value !== null)
const workflowFinalResultScalar = computed(() => (
  workflowFinalResult.value === null || typeof workflowFinalResult.value !== 'object'
))
const workflowFinalResultText = computed(() => JSON.stringify(workflowFinalResult.value, null, 2))
const executionErrorMessage = computed(() => queryErrorMessage(
  execution.value?.error_details?.error_code,
  execution.value?.error_details?.message || execution.value?.error_details?.error || '',
  t
))

let refreshTimer = null
const executionId = computed(() => route.params.execution_id)

// 加载执行详情
const loadExecution = async (silent = false) => {
  try {
    const data = await getExecution(executionId.value)
    execution.value = data

    const result = data.metadata?.result
    if (result?.logs) {
      logs.value = result.logs
    } else {
      logs.value = []
    }
  } catch (error) {
    console.error('加载执行详情失败:', error)
    if (!silent) {
      ElMessage.error(t('develop.executionDetail.loadFailed') + (error.response?.data?.error || error.message))
    }
  }
}

// 加载执行日志
const loadLogs = async () => {
  if (executionResult.value?.logs) {
    logs.value = executionResult.value.logs
    return
  }

  try {
    const data = await getExecutionLogs(executionId.value)
    logs.value = data.logs || []
  } catch (error) {
    console.error('加载日志失败:', error)
  }
}

const loadResourceEngines = async () => {
  if (resourceEngines.value.length > 0) return
  try {
    resourceEngines.value = await listResourceTreeEngines('/api/v1/meta')
  } catch (error) {
    console.warn('加载资源引擎失败:', error)
    resourceEngines.value = []
  }
}

// 工具函数
const getTypeLabel = (type) => {
  const labels = {
    query: t('develop.execution.typeQuery'),
    workflow: t('develop.execution.typeWorkflow'),
    script: t('develop.execution.typeScript')
  }
  return labels[type] || type
}

const getTypeColor = (type) => {
  const colors = { query: 'primary', workflow: 'success', script: 'warning' }
  return colors[type] || 'info'
}

const getStatusLabel = (status) => {
  const labels = {
    pending: t('develop.execution.statusPending'),
    running: t('develop.execution.statusRunning'),
    success: t('develop.execution.statusSuccess'),
    failed: t('develop.execution.statusFailed'),
    timeout: t('develop.execution.statusTimeout'),
    cancelled: t('develop.execution.statusCancelled')
  }
  return labels[status] || status
}

const getStatusColor = (status) => {
  const colors = {
    pending: 'info',
    running: 'primary',
    success: 'success',
    failed: 'danger',
    timeout: 'warning',
    cancelled: 'info'
  }
  return colors[status] || 'info'
}

const getProgressStatus = (status) => {
  const statusMap = {
    success: 'success',
    failed: 'exception',
    timeout: 'warning',
    cancelled: 'warning'
  }
  return statusMap[status] || undefined
}

const getTriggerLabel = (trigger) => {
  const labels = {
    manual: t('develop.execution.triggerManual'),
    scheduled: t('develop.execution.triggerSchedule')
  }
  return labels[trigger] || trigger
}

const getTaskStatusColor = (status) => {
  const colors = {
    success: 'success',
    failed: 'danger',
    running: 'primary',
    pending: 'info'
  }
  return colors[status] || 'info'
}

const getLogType = (level) => {
  const types = {
    ERROR: 'danger',
    WARN: 'warning',
    INFO: 'info',
    DEBUG: 'info'
  }
  return types[level] || 'info'
}

const formatDuration = (ms) => {
  if (!ms) return '-'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60000).toFixed(1)}min`
}

const formatTime = (time) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

const formatSQLResult = () => queryResultFromExecution(execution.value)

const formatWriteMode = (mode) => {
  const labels = {
    replace: t('develop.executionDetail.writeModeReplace'),
    append: t('develop.executionDetail.writeModeAppend'),
    fail: t('develop.executionDetail.writeModeFail'),
    create: t('develop.executionDetail.writeModeCreate')
  }
  return labels[String(mode || '').toLowerCase()] || String(mode || '-')
}

const formatBytes = (bytes) => {
  if (!bytes) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

const formatWorkflowResultValue = (value) => {
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return String(value)
}

// 操作函数
const handleBack = () => {
  navigateDevelopRoute(router, '/executions', { history: 'replace' })
}

const handleRetry = async () => {
  try {
    await retryExecution(executionId.value)
    ElMessage.success(t('develop.execution.retrySubmitted'))
    await navigateDevelopRoute(router, '/executions', { history: 'replace' })
  } catch (error) {
    console.error('重试执行失败:', error)
    ElMessage.error(t('develop.execution.retryFailed') + (error.response?.data?.error || error.message))
  }
}

const handleRefresh = async () => {
  await loadExecution()
  await loadLogs()
}

const handleTabChange = async (tab) => {
  const routeState = resolveExecutionDetailRouteState({ tab }, execution.value?.status)
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateDevelopRoute(router, location, { history: 'replace' })
  }
}

async function restoreExecutionRoute() {
  const routeState = resolveExecutionDetailRouteState(route.query, execution.value?.status)
  activeTab.value = routeState.tab
  if (routeState.changed) {
    await navigateDevelopRoute(router, {
      path: route.path,
      query: routeState.query
    }, { history: 'replace' })
  }
}

// 自动刷新（仅 running 状态）
const startAutoRefresh = () => {
  if (refreshTimer) return
  if (execution.value?.status === 'running') {
    refreshTimer = setInterval(() => {
      if (execution.value?.status === 'running') {
        loadExecution(true) // 静默刷新
        loadLogs()
      } else {
        stopAutoRefresh()
      }
    }, 2000) // 每 2 秒刷新一次
  }
}

const stopAutoRefresh = () => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

// 监听执行状态变化
watch(() => execution.value?.status, (newStatus) => {
  if (newStatus !== 'running' && refreshTimer) {
    stopAutoRefresh()
  } else if (newStatus === 'running' && !refreshTimer) {
    startAutoRefresh()
  }
  if (routeDataReady) restoreExecutionRoute()
})

watch(() => route.query, () => {
  if (routeDataReady) restoreExecutionRoute()
})

watch(executionId, async () => {
  if (!routeDataReady) return
  stopAutoRefresh()
  execution.value = null
  logs.value = []
  await loadExecution()
  await loadLogs()
  await restoreExecutionRoute()
  startAutoRefresh()
})

// 生命周期
onMounted(async () => {
  await loadExecution()
  await loadLogs()
  if (execution.value?.task_type === 'workflow') await loadResourceEngines()
  routeDataReady = true
  await restoreExecutionRoute()
  startAutoRefresh()
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<style scoped>
.execution-detail-page {
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-secondary);
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 20px;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
}

.toolbar-left,
.toolbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.toolbar h2 {
  margin: 0;
  font-size: 18px;
  color: var(--addp-text-primary);
  font-weight: 500;
}

.info-section {
  padding: 16px;
  flex-shrink: 0;
}

.card-header {
  font-size: 16px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.content-area {
  overflow: visible;
  padding: 0 16px 16px 16px;
}

:deep(.el-tabs) {
  display: block;
}

:deep(.el-tabs__content) {
  overflow: visible;
}

.tab-content {
  padding: 16px;
  overflow: visible;
}

.json-result {
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  padding: 16px;
  border-radius: 4px;
  overflow: auto;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.5;
  border: 1px solid var(--addp-border-color);
}

.workflow-result h3 {
  margin: 0;
  color: var(--addp-text-primary);
}

.workflow-result-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
}

.output-operator {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.output-operator strong {
  color: var(--addp-text-primary);
  font-weight: 600;
}

.output-operator span,
.output-resource {
  color: var(--addp-text-secondary);
  overflow-wrap: anywhere;
}

.workflow-final-result {
  margin-top: 16px;
}

.workflow-final-result-label {
  margin-bottom: 6px;
  color: var(--addp-text-secondary);
  font-size: 13px;
  font-weight: 600;
}

.workflow-final-result-value {
  padding: 14px 16px;
  color: var(--addp-text-primary);
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  font-size: 22px;
  font-weight: 600;
}

.workflow-final-result-json {
  max-height: 320px;
  margin: 0;
  padding: 12px;
  overflow: auto;
  color: var(--addp-text-primary);
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.log-item {
  display: flex;
  align-items: center;
  gap: 12px;
}

.log-message {
  color: var(--addp-text-secondary);
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
}

.error-traceback {
  background: #fff5f5;
  color: #c45656;
  padding: 12px;
  border-radius: 4px;
  overflow: auto;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.4;
  border: 1px solid #fde2e2;
  max-height: 400px;
  white-space: pre-wrap;
  word-wrap: break-word;
}

@keyframes rotate {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

.rotating {
  animation: rotate 1s linear infinite;
}
</style>
