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
      <el-tabs v-model="activeTab" type="border-card" style="height: 100%;">
        <el-tab-pane :label="t('develop.executionDetail.tabResult')" name="result">
          <div class="tab-content">
            <div v-if="execution?.status === 'success' && executionResult">
              <!-- 查询结果: 使用 QueryResult 组件 -->
              <div v-if="execution.task_type === 'query'">
                <QueryResult :result="formatSQLResult(executionResult)" />
              </div>

              <!-- 工作流结果: 使用表格展示任务列表 -->
              <div v-else-if="execution.task_type === 'workflow'">
                <div class="workflow-result">
                  <h3>{{ t('develop.executionDetail.workflowResult') }}</h3>
                  <el-table
                    v-if="executionResult.tasks && executionResult.tasks.length > 0"
                    :data="executionResult.tasks"
                    stripe
                    border
                  >
                    <el-table-column prop="id" :label="t('develop.executionDetail.taskId')" width="150" />
                    <el-table-column prop="operator" :label="t('develop.executionDetail.operator')" width="120" />
                    <el-table-column :label="t('develop.executionDetail.taskStatus')" width="100">
                      <template #default="{ row }">
                        <el-tag :type="getTaskStatusColor(row.status)">
                          {{ row.status }}
                        </el-tag>
                      </template>
                    </el-table-column>
                    <el-table-column prop="duration_ms" :label="t('develop.executionDetail.taskDuration')" width="100">
                      <template #default="{ row }">
                        {{ formatDuration(row.duration_ms) }}
                      </template>
                    </el-table-column>
                    <el-table-column prop="output_path" :label="t('develop.executionDetail.outputPath')" min-width="200" />
                  </el-table>
                  <el-empty v-else :description="t('develop.executionDetail.noTaskData')" />
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
} from '@/api/devExecution'
import QueryResult from '@/components/QueryResult.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

// 状态管理
const execution = ref(null)
const logs = ref([])
const activeTab = ref('result')

const executionResult = computed(() => execution.value?.metadata?.result || null)
const executionInputs = computed(() => execution.value?.execution_config?.inputs || {})
const executionErrorMessage = computed(() => (
  execution.value?.error_details?.message ||
  execution.value?.error_details?.error ||
  ''
))

let refreshTimer = null

// 加载执行详情
const loadExecution = async (silent = false) => {
  try {
    const id = route.params.id
    const data = await getExecution(id)
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
    const id = route.params.id
    const data = await getExecutionLogs(id)
    logs.value = data.logs || []
  } catch (error) {
    console.error('加载日志失败:', error)
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

const formatSQLResult = (result) => {
  // 将后端结果格式转换为 QueryResult 组件需要的格式
  return {
    success: true,
    columns: result.columns || [],
    rows: result.rows || [],
    rows_count: result.rows_count,
    rows_affected: result.rows_affected,
    execution_time_ms: result.execution_time_ms
  }
}

// 操作函数
const handleBack = () => {
  router.push('/executions')
}

const handleRetry = async () => {
  try {
    await retryExecution(route.params.id)
    ElMessage.success(t('develop.execution.retrySubmitted'))
    router.push('/executions')
  } catch (error) {
    console.error('重试执行失败:', error)
    ElMessage.error(t('develop.execution.retryFailed') + (error.response?.data?.error || error.message))
  }
}

const handleRefresh = () => {
  loadExecution()
  loadLogs()
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
})

// 生命周期
onMounted(() => {
  loadExecution()
  loadLogs()
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
  height: 100vh;
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
  flex: 1;
  overflow: hidden;
  padding: 0 16px 16px 16px;
}

:deep(.el-tabs) {
  display: flex;
  flex-direction: column;
  height: 100%;
}

:deep(.el-tabs__content) {
  flex: 1;
  overflow: auto;
}

.tab-content {
  padding: 16px;
  height: 100%;
  overflow: auto;
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
  margin: 0 0 16px 0;
  color: var(--addp-text-primary);
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
