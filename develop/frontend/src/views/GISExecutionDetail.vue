<template>
  <div class="gis-execution-detail">
    <el-card v-loading="loading">
      <template #header>
        <div class="card-header">
          <span>执行详情 #{{ execution.id || '-' }}</span>
          <div class="header-actions">
            <el-button @click="goBack">返回列表</el-button>
            <el-button
              v-if="execution.status === 'failed' || execution.status === 'timeout'"
              type="warning"
              @click="retry">
              重试
            </el-button>
          </div>
        </div>
      </template>

      <!-- 基本信息 -->
      <section class="info-section">
        <h3>📋 基本信息</h3>
        <el-descriptions :column="2" border>
          <el-descriptions-item label="任务名称">
            {{ execution.task_name || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="任务ID">
            {{ execution.task_id || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="状态">
            <el-tag :type="getStatusType(execution.status)" size="large">
              {{ getStatusLabel(execution.status) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="触发方式">
            {{ getTriggerTypeLabel(execution.trigger_type) }}
          </el-descriptions-item>
          <el-descriptions-item label="开始时间">
            {{ formatTime(execution.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item label="完成时间">
            {{ formatTime(execution.completed_at) }}
          </el-descriptions-item>
          <el-descriptions-item label="执行时间">
            <span v-if="execution.execution_time_ms">
              {{ formatDuration(execution.execution_time_ms) }}
            </span>
            <span v-else class="text-muted">-</span>
          </el-descriptions-item>
          <el-descriptions-item label="结果记录数">
            <span v-if="execution.result_count !== null && execution.result_count !== undefined">
              {{ execution.result_count.toLocaleString() }}
            </span>
            <span v-else class="text-muted">-</span>
          </el-descriptions-item>
        </el-descriptions>
      </section>

      <!-- 输入参数 -->
      <section class="info-section">
        <h3>📥 输入参数</h3>
        <div class="json-viewer">
          <pre v-if="execution.inputs">{{ JSON.stringify(execution.inputs, null, 2) }}</pre>
          <div v-else class="empty-content">无输入参数</div>
        </div>
      </section>

      <!-- 执行结果 -->
      <section class="info-section" v-if="execution.status === 'success' && execution.result_table">
        <h3>📊 执行结果</h3>
        <el-descriptions :column="1" border>
          <el-descriptions-item label="结果表">
            {{ execution.result_table }}
          </el-descriptions-item>
        </el-descriptions>
        <div style="margin-top: 12px;">
          <el-button type="primary" @click="viewResult">
            在 Manager 中预览
          </el-button>
        </div>
      </section>

      <!-- 错误信息 -->
      <section class="info-section" v-if="execution.error_message">
        <h3>❌ 错误信息</h3>
        <el-alert
          type="error"
          :title="execution.error_message"
          :closable="false"
          show-icon />
      </section>

      <!-- 执行日志 -->
      <section class="info-section">
        <h3>📝 执行日志</h3>

        <!-- 日志控制栏 -->
        <div class="log-controls">
          <el-radio-group v-model="logLevel" size="small">
            <el-radio-button label="all">全部</el-radio-button>
            <el-radio-button label="INFO">INFO</el-radio-button>
            <el-radio-button label="ERROR">ERROR</el-radio-button>
          </el-radio-group>

          <div class="log-actions">
            <el-button
              size="small"
              @click="refreshLogs"
              :loading="refreshing">
              刷新
            </el-button>
            <el-button
              size="small"
              @click="downloadLogs"
              :disabled="!logs">
              下载日志
            </el-button>
          </div>
        </div>

        <!-- 日志查看器 -->
        <div class="log-viewer">
          <pre v-if="filteredLogs">{{ filteredLogs }}</pre>
          <div v-else class="empty-logs">暂无日志</div>
        </div>
      </section>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as spatialApi from '@/api/spatial'

const route = useRoute()
const router = useRouter()

const loading = ref(false)
const refreshing = ref(false)
const execution = ref({})
const logs = ref('')
const logLevel = ref('all')
const autoRefreshInterval = ref(null)

// 加载执行详情
const loadExecution = async () => {
  loading.value = true
  try {
    const res = await spatialApi.getExecution(route.params.id)
    execution.value = res.data || {}

    // 加载日志
    await loadLogs()

    // 如果任务正在运行，启动自动刷新
    checkAutoRefresh()
  } catch (error) {
    ElMessage.error('加载执行详情失败: ' + error.message)
  } finally {
    loading.value = false
  }
}

// 加载日志
const loadLogs = async () => {
  try {
    const res = await spatialApi.getExecutionLogs(route.params.id)
    logs.value = res.data?.logs || ''
  } catch (error) {
    console.error('加载日志失败:', error)
    logs.value = ''
  }
}

// 刷新日志
const refreshLogs = async () => {
  if (refreshing.value) return

  refreshing.value = true
  try {
    // 同时刷新执行状态和日志
    const [execRes, logRes] = await Promise.all([
      spatialApi.getExecution(route.params.id),
      spatialApi.getExecutionLogs(route.params.id)
    ])

    execution.value = execRes.data || {}
    logs.value = logRes.data?.logs || ''

    // 检查是否需要继续自动刷新
    checkAutoRefresh()
  } catch (error) {
    ElMessage.error('刷新失败: ' + error.message)
  } finally {
    refreshing.value = false
  }
}

// 检查自动刷新
const checkAutoRefresh = () => {
  const isRunning = execution.value.status === 'running' || execution.value.status === 'pending'

  if (isRunning && !autoRefreshInterval.value) {
    // 启动定时器（每 5 秒刷新一次）
    autoRefreshInterval.value = setInterval(refreshLogs, 5000)
  } else if (!isRunning && autoRefreshInterval.value) {
    // 停止定时器
    clearInterval(autoRefreshInterval.value)
    autoRefreshInterval.value = null
  }
}

// 下载日志
const downloadLogs = () => {
  if (!logs.value) {
    ElMessage.warning('没有日志可下载')
    return
  }

  const blob = new Blob([logs.value], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `gis-execution-${route.params.id}-logs.txt`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)

  ElMessage.success('日志下载成功')
}

// 过滤日志
const filteredLogs = computed(() => {
  if (!logs.value) return ''
  if (logLevel.value === 'all') return logs.value

  return logs.value
    .split('\n')
    .filter(line => line.includes(`[${logLevel.value}]`))
    .join('\n')
})

// 查看结果
const viewResult = () => {
  const url = `/manager/data-explorer?resource=develop&table=${execution.value.result_table}`
  window.open(url, '_blank')
}

// 重试执行
const retry = async () => {
  try {
    const res = await spatialApi.retryExecution(route.params.id)
    ElMessage.success('重试已提交，执行ID: ' + res.execution_id)
    // 跳转到新的执行详情页
    router.push({ name: 'GISExecutionDetail', params: { id: res.execution_id } })
  } catch (error) {
    ElMessage.error('重试失败: ' + error.message)
  }
}

// 返回列表
const goBack = () => {
  router.push({ name: 'GISExecutions' })
}

// 获取状态类型
const getStatusType = (status) => {
  const map = {
    pending: 'info',
    running: 'warning',
    success: 'success',
    failed: 'danger',
    timeout: ''
  }
  return map[status] || 'info'
}

// 获取状态标签
const getStatusLabel = (status) => {
  const map = {
    pending: '待执行',
    running: '运行中',
    success: '成功',
    failed: '失败',
    timeout: '超时'
  }
  return map[status] || status
}

// 获取触发类型标签
const getTriggerTypeLabel = (type) => {
  const map = {
    manual: '手动',
    schedule: '调度',
    orchestrator: '编排',
    api: 'API',
    retry: '重试'
  }
  return map[type] || type
}

// 格式化时间
const formatTime = (time) => {
  if (!time) return '-'
  const date = new Date(time)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  })
}

// 格式化时长
const formatDuration = (ms) => {
  if (!ms) return '-'
  if (ms < 1000) return `${ms} ms`
  const seconds = (ms / 1000).toFixed(1)
  if (seconds < 60) return `${seconds} 秒`
  const minutes = (seconds / 60).toFixed(1)
  return `${minutes} 分钟`
}

onMounted(() => {
  loadExecution()
})

onUnmounted(() => {
  // 清理定时器
  if (autoRefreshInterval.value) {
    clearInterval(autoRefreshInterval.value)
    autoRefreshInterval.value = null
  }
})
</script>

<style scoped>
.gis-execution-detail {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-actions {
  display: flex;
  gap: 8px;
}

.info-section {
  margin-bottom: 32px;
}

.info-section h3 {
  font-size: 16px;
  font-weight: 600;
  color: #303133;
  margin-bottom: 16px;
  display: flex;
  align-items: center;
}

.json-viewer {
  background: #f5f7fa;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 16px;
  overflow-x: auto;
}

.json-viewer pre {
  margin: 0;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.5;
  color: #303133;
}

.empty-content {
  text-align: center;
  color: #909399;
  padding: 20px 0;
  font-size: 14px;
}

.text-muted {
  color: #909399;
}

.log-controls {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  padding: 10px;
  background: #f5f7fa;
  border-radius: 4px;
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

.empty-logs {
  text-align: center;
  color: #909399;
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
  background: #666;
}
</style>
