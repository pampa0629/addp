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

      <!-- ✅ 新增：后处理摘要卡片 -->
      <el-divider v-if="execution.status === 'success'">后处理执行摘要</el-divider>
      <div v-if="execution.status === 'success'" class="post-process-summary">
        <el-space wrap :size="15">
          <!-- 主键创建 -->
          <el-statistic
            v-if="postProcessSummary.primary_key_created"
            title="主键创建"
            :value="'✓'"
          >
            <template #prefix>
              <el-icon style="color: var(--el-color-success); font-size: 20px;">
                <span style="font-weight: bold;">🔑</span>
              </el-icon>
            </template>
            <template #suffix>
              <el-text size="small" type="success">
                {{ postProcessSummary.primary_key_columns.join(', ') }}
              </el-text>
            </template>
          </el-statistic>

          <!-- 空间索引 -->
          <el-statistic
            v-if="postProcessSummary.spatial_indexes_created > 0"
            title="空间索引"
            :value="postProcessSummary.spatial_indexes_created"
          >
            <template #prefix>
              <el-icon style="color: var(--el-color-primary); font-size: 20px;">
                <span style="font-weight: bold;">🗺️</span>
              </el-icon>
            </template>
            <template #suffix>
              <el-text size="small" type="primary">个</el-text>
            </template>
          </el-statistic>

          <!-- 统计更新 -->
          <el-statistic
            v-if="postProcessSummary.statistics_updated"
            title="统计更新"
            :value="'✓'"
          >
            <template #prefix>
              <el-icon style="color: var(--addp-text-tertiary); font-size: 20px;">
                <span style="font-weight: bold;">📊</span>
              </el-icon>
            </template>
          </el-statistic>
        </el-space>
      </div>

      <el-divider>执行日志</el-divider>

      <!-- 日志控制栏 -->
      <div class="log-controls">
        <el-radio-group v-model="logLevel" size="small">
          <el-radio-button value="all">全部</el-radio-button>
          <el-radio-button value="info">INFO</el-radio-button>
          <el-radio-button value="post-process">后处理</el-radio-button>
          <el-radio-button value="error">ERROR</el-radio-button>
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

      <!-- 日志查看器（高亮显示后处理日志） -->
      <div class="log-viewer">
        <div v-if="filteredLogs">
          <div
            v-for="(line, index) in filteredLogsArray"
            :key="index"
            :class="getLogLineClass(line)"
            class="log-line"
          >
            <span class="log-icon">{{ getLogIcon(line) }}</span>
            <span class="log-text">{{ line }}</span>
          </div>
        </div>
        <div v-else class="empty-logs">暂无日志</div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { executionAPI } from '@/api/tasks'
import { ElMessage, ElIcon } from 'element-plus'

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

// ✅ 新增：后处理摘要信息提取
const postProcessSummary = computed(() => {
  const summary = {
    primary_key_created: false,
    primary_key_columns: [],
    spatial_indexes_created: 0,
    statistics_updated: false
  }

  if (!logs.value) return summary

  const logLines = logs.value.split('\n')

  logLines.forEach(line => {
    // 检测主键创建成功
    if (line.includes('✅ [后处理]') && line.includes('主键创建成功')) {
      summary.primary_key_created = true
      // 从日志中提取列名，格式: "columns"=["SmID"]
      const match = line.match(/"columns"=\[(.*?)\]/)
      if (match) {
        summary.primary_key_columns = match[1]
          .split(',')
          .map(s => s.trim().replace(/"/g, ''))
          .filter(Boolean)
      }
    }

    // 检测空间索引创建
    if (line.includes('✅ [后处理]') && line.includes('空间索引创建成功')) {
      summary.spatial_indexes_created++
    }

    // 检测统计信息更新
    if (line.includes('✅ [后处理]') && line.includes('统计信息更新成功')) {
      summary.statistics_updated = true
    }
  })

  return summary
})

// ✅ 新增：日志行分类
const getLogIcon = (line) => {
  if (line.includes('🔑')) return '🔑'
  if (line.includes('🗺️')) return '🗺️'
  if (line.includes('📊')) return '📊'
  if (line.includes('❌')) return '❌'
  if (line.includes('✅')) return '✅'
  if (line.includes('⚠️')) return '⚠️'
  if (line.includes('ℹ️')) return 'ℹ️'
  if (line.includes('⚙️')) return '⚙️'
  return ' '
}

// ✅ 新增：日志行样式分类
const getLogLineClass = (line) => {
  // 后处理相关日志
  if (line.includes('[后处理]')) {
    if (line.includes('✅')) return 'log-success'
    if (line.includes('🔑')) return 'log-primary-key'
    if (line.includes('🗺️')) return 'log-spatial-index'
    if (line.includes('📊')) return 'log-statistics'
    if (line.includes('❌')) return 'log-error'
    if (line.includes('⚠️')) return 'log-warning'
    return 'log-post-process'
  }

  // 普通日志
  if (line.includes('[ERROR]') || line.includes('❌')) return 'log-error'
  if (line.includes('[WARN]') || line.includes('⚠️')) return 'log-warning'
  if (line.includes('[INFO]') || line.includes('ℹ️')) return 'log-info'

  return 'log-default'
}

// ✅ 新增：日志行数组（用于逐行渲染）
const filteredLogsArray = computed(() => {
  if (!logs.value) return []

  const lines = logs.value.split('\n')

  if (logLevel.value === 'all') return lines

  if (logLevel.value === 'post-process') {
    return lines.filter(line => line.includes('[后处理]'))
  }

  return lines.filter(line => {
    const upperLevel = logLevel.value.toUpperCase()
    return line.includes(`[${upperLevel}]`)
  })
})

// 日志过滤（保留原有的 pre 方式作为备用）
const filteredLogs = computed(() => {
  if (!logs.value) return ''
  if (logLevel.value === 'all') return logs.value

  return logs.value
    .split('\n')
    .filter(line => {
      if (logLevel.value === 'post-process') {
        return line.includes('[后处理]')
      }
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
  return types[status] || 'info'
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
  background: var(--addp-bg-secondary);
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

/* ✅ 新增：日志行样式 */
.log-line {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 4px 0;
  border-left: 3px solid transparent;
  padding-left: 6px;
  white-space: pre-wrap;
  word-wrap: break-word;
}

.log-icon {
  min-width: 24px;
  font-weight: bold;
}

.log-text {
  flex: 1;
}

/* 主键创建日志 */
.log-primary-key {
  background-color: rgba(232, 219, 163, 0.1);
  border-left-color: var(--el-color-warning);
  color: #ffb94f;
}

/* 空间索引日志 */
.log-spatial-index {
  background-color: rgba(89, 184, 255, 0.1);
  border-left-color: var(--el-color-primary);
  color: #66b1ff;
}

/* 统计信息日志 */
.log-statistics {
  background-color: rgba(144, 147, 153, 0.1);
  border-left-color: var(--addp-text-tertiary);
  color: #a8abb2;
}

/* 成功日志 */
.log-success {
  background-color: rgba(103, 194, 58, 0.1);
  border-left-color: var(--el-color-success);
  color: #85ce61;
  font-weight: bold;
}

/* 错误日志 */
.log-error {
  background-color: rgba(245, 108, 108, 0.1);
  border-left-color: var(--el-color-danger);
  color: #f78989;
  font-weight: bold;
}

/* 警告日志 */
.log-warning {
  background-color: rgba(230, 162, 60, 0.1);
  border-left-color: var(--el-color-warning);
  color: #ffb94f;
}

/* 信息日志 */
.log-info {
  background-color: rgba(89, 184, 255, 0.1);
  border-left-color: var(--el-color-primary);
  color: #66b1ff;
}

/* 后处理日志 */
.log-post-process {
  background-color: rgba(103, 194, 58, 0.1);
  border-left-color: var(--el-color-success);
  color: #85ce61;
}

/* 默认日志 */
.log-default {
  color: #d4d4d4;
}

/* 后处理摘要样式 */
.post-process-summary {
  background: var(--addp-bg-secondary);
  padding: 16px;
  border-radius: 4px;
  margin-bottom: 16px;
  border-left: 4px solid var(--el-color-success);
}

.empty-logs {
  text-align: center;
  color: var(--addp-text-tertiary);
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
  background: var(--addp-text-secondary);
}
</style>
