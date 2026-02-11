<template>
  <div class="execution-detail-page">
    <!-- 顶部导航 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <el-button @click="handleBack">
          <el-icon><ArrowLeft /></el-icon>
          返回
        </el-button>
        <h2>执行详情</h2>
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
          v-if="execution?.status === 'running'"
          type="warning"
          @click="handleCancel"
        >
          <el-icon><Close /></el-icon>
          取消执行
        </el-button>
        <el-button
          v-if="['failed', 'timeout', 'cancelled'].includes(execution?.status)"
          type="success"
          @click="handleRetry"
        >
          <el-icon><Refresh /></el-icon>
          重新执行
        </el-button>
        <el-button @click="handleRefresh">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
      </div>
    </div>

    <!-- 基本信息卡片 -->
    <div class="info-section">
      <el-card>
        <template #header>
          <div class="card-header">
            <span>基本信息</span>
          </div>
        </template>
        <el-descriptions :column="3" border>
          <el-descriptions-item label="执行ID">
            {{ execution?.execution_id }}
          </el-descriptions-item>
          <el-descriptions-item label="任务名称">
            {{ execution?.dev_item?.name || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="任务类型">
            <el-tag :type="getTypeColor(execution?.dev_type)">
              {{ getTypeLabel(execution?.dev_type) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="触发方式">
            {{ getTriggerLabel(execution?.trigger_type) }}
          </el-descriptions-item>
          <el-descriptions-item label="开始时间">
            {{ formatTime(execution?.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item label="完成时间">
            {{ formatTime(execution?.completed_at) }}
          </el-descriptions-item>
          <el-descriptions-item label="执行时长">
            {{ formatDuration(execution?.execution_time_ms) }}
          </el-descriptions-item>
          <el-descriptions-item label="影响行数">
            {{ execution?.rows_affected || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="当前步骤">
            {{ execution?.current_step || '-' }}
          </el-descriptions-item>
        </el-descriptions>

        <!-- 进度条 -->
        <div style="margin-top: 16px;">
          <div style="margin-bottom: 8px; color: var(--addp-text-secondary);">执行进度</div>
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
        <!-- Tab 1: 执行结果 -->
        <el-tab-pane label="执行结果" name="result">
          <div class="tab-content">
            <div v-if="execution?.status === 'success' && execution?.result">
              <!-- 查询结果: 使用 QueryResult 组件 -->
              <div v-if="execution.dev_type === 'query'">
                <QueryResult :result="formatSQLResult(execution.result)" />
              </div>

              <!-- 工作流结果: 使用表格展示任务列表 -->
              <div v-else-if="execution.dev_type === 'workflow'">
                <div class="workflow-result">
                  <h3>工作流执行结果</h3>
                  <el-table
                    v-if="execution.result.tasks && execution.result.tasks.length > 0"
                    :data="execution.result.tasks"
                    stripe
                    border
                  >
                    <el-table-column prop="id" label="任务ID" width="150" />
                    <el-table-column prop="operator" label="算子" width="120" />
                    <el-table-column label="状态" width="100">
                      <template #default="{ row }">
                        <el-tag :type="getTaskStatusColor(row.status)">
                          {{ row.status }}
                        </el-tag>
                      </template>
                    </el-table-column>
                    <el-table-column prop="duration_ms" label="耗时" width="100">
                      <template #default="{ row }">
                        {{ formatDuration(row.duration_ms) }}
                      </template>
                    </el-table-column>
                    <el-table-column prop="output_path" label="输出路径" min-width="200" />
                  </el-table>
                  <el-empty v-else description="暂无任务数据" />
                </div>
              </div>

              <!-- 脚本结果: 使用 JSON 展示 -->
              <div v-else>
                <pre class="json-result">{{ JSON.stringify(execution.result, null, 2) }}</pre>
              </div>
            </div>

            <el-empty v-else-if="execution?.status === 'running'" description="执行中，结果尚未生成" />
            <el-empty v-else-if="!execution?.result" description="暂无执行结果" />
          </div>
        </el-tab-pane>

        <!-- Tab 2: 执行日志 -->
        <el-tab-pane label="执行日志" name="logs">
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
            <el-empty v-else description="暂无日志" />
          </div>
        </el-tab-pane>

        <!-- Tab 3: 输入参数 -->
        <el-tab-pane label="输入参数" name="inputs">
          <div class="tab-content">
            <pre class="json-result">{{ JSON.stringify(execution?.inputs || {}, null, 2) }}</pre>
          </div>
        </el-tab-pane>

        <!-- Tab 4: 错误信息 -->
        <el-tab-pane
          v-if="execution?.status === 'failed'"
          label="错误信息"
          name="error"
        >
          <div class="tab-content">
            <el-alert
              type="error"
              :title="execution?.error_message || '未知错误'"
              :closable="false"
              show-icon
            >
              <template v-if="execution?.result?.traceback">
                <div style="margin-top: 12px; margin-bottom: 8px; font-weight: bold;">详细堆栈信息:</div>
                <pre class="error-traceback">{{ execution.result.traceback }}</pre>
              </template>
              <pre v-else style="margin-top: 8px; white-space: pre-wrap;">{{ execution?.error_message }}</pre>
            </el-alert>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  ArrowLeft,
  Close,
  Refresh,
  Loading
} from '@element-plus/icons-vue'
import {
  getExecution,
  getExecutionLogs,
  cancelExecution,
  retryExecution
} from '@/api/devExecution'
import QueryResult from '@/components/QueryResult.vue'

const route = useRoute()
const router = useRouter()

// 状态管理
const execution = ref(null)
const logs = ref([])
const activeTab = ref('result')

let refreshTimer = null

// 加载执行详情
const loadExecution = async (silent = false) => {
  try {
    const id = route.params.id
    const data = await getExecution(id)
    execution.value = data

    // 从 execution.result 中读取日志
    if (data.result && data.result.logs) {
      logs.value = data.result.logs
    } else {
      logs.value = []
    }
  } catch (error) {
    console.error('加载执行详情失败:', error)
    if (!silent) {
      ElMessage.error('加载失败: ' + (error.response?.data?.error || error.message))
    }
  }
}

// 加载执行日志（保留向后兼容，但优先使用 result 中的日志）
const loadLogs = async () => {
  // 如果 result 中已有日志，则不再单独加载
  if (execution.value?.result?.logs) {
    logs.value = execution.value.result.logs
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
  const labels = { query: '查询', workflow: '工作流', script: '脚本' }
  return labels[type] || type
}

const getTypeColor = (type) => {
  const colors = { sql: 'primary', workflow: 'success', script: 'warning' }
  return colors[type] || 'info'
}

const getStatusLabel = (status) => {
  const labels = {
    pending: '等待中',
    running: '运行中',
    success: '成功',
    failed: '失败',
    timeout: '超时',
    cancelled: '已取消'
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
    manual: '手动',
    schedule: '定时',
    orchestrator: '编排',
    api: 'API'
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

const handleCancel = async () => {
  try {
    await ElMessageBox.confirm('确定要取消此执行吗？', '确认取消', {
      type: 'warning'
    })
    await cancelExecution(route.params.id)
    ElMessage.success('已取消')
    loadExecution()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('取消执行失败:', error)
      ElMessage.error('取消失败: ' + (error.response?.data?.error || error.message))
    }
  }
}

const handleRetry = async () => {
  try {
    await retryExecution(route.params.id)
    ElMessage.success('已提交重试')
    router.push('/executions')
  } catch (error) {
    console.error('重试执行失败:', error)
    ElMessage.error('重试失败: ' + (error.response?.data?.error || error.message))
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
