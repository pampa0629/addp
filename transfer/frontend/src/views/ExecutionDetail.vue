<template>
  <div class="execution-detail">
    <el-button @click="$router.back()" style="margin-bottom: 20px;">返回</el-button>
    <el-card v-loading="loading">
      <template #header>执行详情 #{{ execution.id }}</template>
      <el-descriptions :column="2" border>
        <el-descriptions-item label="执行ID">{{ execution.id }}</el-descriptions-item>
        <el-descriptions-item label="任务ID">{{ execution.task_id }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="getStatusType(execution.status)">{{ execution.status }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="触发方式">{{ execution.trigger_type }}</el-descriptions-item>
        <el-descriptions-item label="已读取记录">{{ execution.records_read }}</el-descriptions-item>
        <el-descriptions-item label="已写入记录">{{ execution.records_written }}</el-descriptions-item>
        <el-descriptions-item label="开始时间">{{ execution.start_time }}</el-descriptions-item>
        <el-descriptions-item label="结束时间">{{ execution.end_time || '-' }}</el-descriptions-item>
      </el-descriptions>

      <el-divider>执行日志</el-divider>

      <!-- 日志控制栏 -->
      <div class="log-controls">
        <el-radio-group v-model="logLevel" size="small">
          <el-radio-button label="all">全部</el-radio-button>
          <el-radio-button label="info">INFO</el-radio-button>
          <el-radio-button label="error">ERROR</el-radio-button>
        </el-radio-group>

        <div class="log-actions">
          <el-button
            size="small"
            @click="refreshLogs"
            :loading="refreshing"
            :disabled="!execution.id">
            刷新日志
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
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { executionAPI } from '@/api/tasks'
import { ElMessage } from 'element-plus'

const route = useRoute()
const loading = ref(false)
const refreshing = ref(false)
const execution = ref({})
const logs = ref('')
const logLevel = ref('all')
const autoRefreshInterval = ref(null)

const loadExecution = async () => {
  loading.value = true
  try {
    execution.value = await executionAPI.get(route.params.id)

    // 加载日志 - API 返回 {logs: "string"}
    const logData = await executionAPI.logs(route.params.id)

    // 处理响应格式
    if (typeof logData === 'object' && logData.logs !== undefined) {
      logs.value = logData.logs || ''
    } else if (typeof logData === 'string') {
      logs.value = logData
    } else if (Array.isArray(logData)) {
      logs.value = logData.join('\n')
    } else {
      logs.value = ''
    }

    // 如果任务正在运行，启动自动刷新
    if (execution.value.status === 'running' && !autoRefreshInterval.value) {
      autoRefreshInterval.value = setInterval(refreshLogs, 5000)
    }
  } catch (error) {
    ElMessage.error('加载执行详情失败: ' + (error.message || error))
  } finally {
    loading.value = false
  }
}

const refreshLogs = async () => {
  if (refreshing.value) return

  refreshing.value = true
  try {
    const logData = await executionAPI.logs(route.params.id)

    // 处理响应格式
    if (typeof logData === 'object' && logData.logs !== undefined) {
      logs.value = logData.logs || ''
    } else if (typeof logData === 'string') {
      logs.value = logData
    } else if (Array.isArray(logData)) {
      logs.value = logData.join('\n')
    } else {
      logs.value = ''
    }

    // 同时刷新执行状态
    execution.value = await executionAPI.get(route.params.id)

    // 如果任务不再运行，停止自动刷新
    if (execution.value.status !== 'running' && autoRefreshInterval.value) {
      clearInterval(autoRefreshInterval.value)
      autoRefreshInterval.value = null
    }
  } catch (error) {
    ElMessage.error('刷新日志失败: ' + (error.message || error))
  } finally {
    refreshing.value = false
  }
}

const downloadLogs = () => {
  if (!logs.value) {
    ElMessage.warning('没有日志可下载')
    return
  }

  const blob = new Blob([logs.value], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `execution-${route.params.id}-logs.txt`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)

  ElMessage.success('日志下载成功')
}

const filteredLogs = computed(() => {
  if (!logs.value) return ''
  if (logLevel.value === 'all') return logs.value

  return logs.value
    .split('\n')
    .filter(line => {
      const upperLevel = logLevel.value.toUpperCase()
      return line.includes(`[${upperLevel}]`)
    })
    .join('\n')
})

const getStatusType = (status) => {
  const types = {
    pending: 'info',
    running: 'primary',
    success: 'success',
    failed: 'danger'
  }
  return types[status] || ''
}

onMounted(() => {
  loadExecution()
})

onUnmounted(() => {
  // 清理自动刷新定时器
  if (autoRefreshInterval.value) {
    clearInterval(autoRefreshInterval.value)
    autoRefreshInterval.value = null
  }
})
</script>

<style scoped>
.execution-detail {
  padding: 20px;
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
