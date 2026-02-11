<template>
  <div class="execution-monitor-page">
    <!-- 工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <h2>执行监控</h2>
        <el-button @click="handleRefresh">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
        <el-switch
          v-model="autoRefresh"
          active-text="自动刷新"
          inactive-text="手动"
          style="margin-left: 12px;"
        />
      </div>
      <div class="toolbar-right">
        <!-- 筛选器 -->
        <el-select
          v-model="filters.dev_type"
          placeholder="任务类型"
          clearable
          style="width: 120px; margin-right: 10px;"
        >
          <el-option label="查询" value="query" />
          <el-option label="工作流" value="workflow" />
          <el-option label="Notebook" value="notebook" />
          <el-option label="脚本" value="script" />
        </el-select>
        <el-select
          v-model="filters.status"
          placeholder="状态"
          clearable
          style="width: 120px; margin-right: 10px;"
        >
          <el-option label="等待中" value="pending" />
          <el-option label="运行中" value="running" />
          <el-option label="成功" value="success" />
          <el-option label="失败" value="failed" />
          <el-option label="超时" value="timeout" />
          <el-option label="已取消" value="cancelled" />
        </el-select>
        <el-select
          v-model="filters.trigger_type"
          placeholder="触发方式"
          clearable
          style="width: 120px; margin-right: 10px;"
        >
          <el-option label="手动" value="manual" />
          <el-option label="定时" value="schedule" />
          <el-option label="编排" value="orchestrator" />
          <el-option label="API" value="api" />
        </el-select>
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          range-separator="至"
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          style="width: 240px;"
        />
      </div>
    </div>

    <!-- 统计卡片 -->
    <div class="stats-cards">
      <el-card class="stat-card">
        <div class="stat-content">
          <div class="stat-icon" style="background: #409eff;">
            <el-icon :size="24"><DataLine /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-label">总执行次数</div>
            <div class="stat-value">{{ statistics.total_executions }}</div>
          </div>
        </div>
      </el-card>
      <el-card class="stat-card">
        <div class="stat-content">
          <div class="stat-icon" style="background: #67c23a;">
            <el-icon :size="24"><SuccessFilled /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-label">成功率</div>
            <div class="stat-value">{{ statistics.success_rate.toFixed(1) }}%</div>
          </div>
        </div>
      </el-card>
      <el-card class="stat-card">
        <div class="stat-content">
          <div class="stat-icon" style="background: #e6a23c;">
            <el-icon :size="24"><Timer /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-label">平均时长</div>
            <div class="stat-value">{{ formatDuration(statistics.avg_execution_time_ms) }}</div>
          </div>
        </div>
      </el-card>
      <el-card class="stat-card">
        <div class="stat-content">
          <div class="stat-icon" style="background: #f56c6c;">
            <el-icon :size="24"><Loading /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-label">运行中</div>
            <div class="stat-value">{{ statistics.running_count }}</div>
          </div>
        </div>
      </el-card>
    </div>

    <!-- 执行列表 -->
    <div class="content-area">
      <el-table
        v-loading="loading"
        :data="executions"
        stripe
        style="width: 100%"
        height="100%"
      >
        <el-table-column prop="execution_id" label="执行ID" width="200" show-overflow-tooltip />
        <el-table-column label="任务名称" min-width="150">
          <template #default="{ row }">
            {{ row.dev_item?.name || '-' }}
          </template>
        </el-table-column>
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag :type="getTypeColor(row.dev_type)">
              {{ getTypeLabel(row.dev_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <el-tag :type="getStatusColor(row.status)">
              <el-icon v-if="row.status === 'running'" class="rotating">
                <Loading />
              </el-icon>
              {{ getStatusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="进度" width="150">
          <template #default="{ row }">
            <el-progress
              :percentage="row.progress || 0"
              :status="getProgressStatus(row.status)"
            />
          </template>
        </el-table-column>
        <el-table-column label="触发方式" width="100">
          <template #default="{ row }">
            {{ getTriggerLabel(row.trigger_type) }}
          </template>
        </el-table-column>
        <el-table-column label="执行时长" width="120">
          <template #default="{ row }">
            {{ formatDuration(row.execution_time_ms) }}
          </template>
        </el-table-column>
        <el-table-column label="开始时间" width="160">
          <template #default="{ row }">
            {{ formatTime(row.started_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button
              type="primary"
              size="small"
              @click="handleViewDetail(row)"
            >
              <el-icon><View /></el-icon>
              详情
            </el-button>
            <el-button
              v-if="row.status === 'running'"
              type="warning"
              size="small"
              @click="handleCancel(row)"
            >
              <el-icon><Close /></el-icon>
              取消
            </el-button>
            <el-button
              v-if="['failed', 'timeout', 'cancelled'].includes(row.status)"
              type="success"
              size="small"
              @click="handleRetry(row)"
            >
              <el-icon><RefreshRight /></el-icon>
              重试
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="pagination">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.page_size"
          :total="pagination.total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="handlePageSizeChange"
          @current-change="handlePageChange"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Refresh,
  DataLine,
  SuccessFilled,
  Timer,
  Loading,
  View,
  Close,
  RefreshRight
} from '@element-plus/icons-vue'
import {
  listExecutions,
  cancelExecution,
  retryExecution,
  getExecutionStatistics
} from '@/api/devExecution'

const router = useRouter()

// 状态管理
const loading = ref(false)
const executions = ref([])
const statistics = ref({
  total_executions: 0,
  success_count: 0,
  failed_count: 0,
  running_count: 0,
  success_rate: 0,
  avg_execution_time_ms: 0,
  total_rows_affected: 0
})

// 自动刷新
const autoRefresh = ref(true)
let refreshTimer = null

// 筛选器
const filters = reactive({
  dev_type: '',
  status: '',
  trigger_type: '',
  dev_item_id: null
})

const dateRange = ref([])

// 分页
const pagination = reactive({
  page: 1,
  page_size: 20,
  total: 0
})

// 加载执行列表
const loadExecutions = async (silent = false) => {
  if (!silent) {
    loading.value = true
  }
  try {
    const params = {
      page: pagination.page,
      page_size: pagination.page_size,
      ...filters
    }

    // 处理日期范围
    if (dateRange.value && dateRange.value.length === 2) {
      params.start_date = dateRange.value[0].toISOString().split('T')[0]
      params.end_date = dateRange.value[1].toISOString().split('T')[0]
    }

    const data = await listExecutions(params)
    executions.value = data.executions || []
    pagination.total = data.total || 0
  } catch (error) {
    console.error('加载执行列表失败:', error)
    if (!silent) {
      ElMessage.error('加载失败: ' + (error.response?.data?.error || error.message))
    }
  } finally {
    if (!silent) {
      loading.value = false
    }
  }
}

// 加载统计数据
const loadStatistics = async () => {
  try {
    const params = {}
    if (dateRange.value && dateRange.value.length === 2) {
      params.start_date = dateRange.value[0].toISOString().split('T')[0]
      params.end_date = dateRange.value[1].toISOString().split('T')[0]
    }
    const data = await getExecutionStatistics(params)
    statistics.value = data
  } catch (error) {
    console.error('加载统计失败:', error)
  }
}

// 工具函数
const getTypeLabel = (type) => {
  const labels = { sql: 'SQL', workflow: '工作流', script: '脚本' }
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

// 操作函数
const handleViewDetail = (row) => {
  router.push(`/executions/${row.execution_id}`)
}

const handleCancel = async (row) => {
  try {
    await ElMessageBox.confirm('确定要取消此执行吗？', '确认取消', {
      type: 'warning'
    })
    await cancelExecution(row.execution_id)
    ElMessage.success('已取消')
    loadExecutions()
    loadStatistics()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('取消执行失败:', error)
      ElMessage.error('取消失败: ' + (error.response?.data?.error || error.message))
    }
  }
}

const handleRetry = async (row) => {
  try {
    await retryExecution(row.execution_id)
    ElMessage.success('已提交重试')
    loadExecutions()
    loadStatistics()
  } catch (error) {
    console.error('重试执行失败:', error)
    ElMessage.error('重试失败: ' + (error.response?.data?.error || error.message))
  }
}

const handleRefresh = () => {
  loadExecutions()
  loadStatistics()
}

const handlePageChange = () => {
  loadExecutions()
}

const handlePageSizeChange = () => {
  pagination.page = 1
  loadExecutions()
}

// 自动刷新逻辑
const startAutoRefresh = () => {
  if (refreshTimer) return
  refreshTimer = setInterval(() => {
    if (autoRefresh.value) {
      loadExecutions(true) // 静默刷新
      loadStatistics()
    }
  }, 3000) // 每 3 秒刷新一次
}

const stopAutoRefresh = () => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

// 监听筛选器变化
watch([filters, dateRange], () => {
  pagination.page = 1
  loadExecutions()
  loadStatistics()
}, { deep: true })

// 生命周期
onMounted(() => {
  loadExecutions()
  loadStatistics()
  startAutoRefresh()
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<style scoped>
.execution-monitor-page {
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
  margin-right: 20px;
}

.stats-cards {
  display: flex;
  gap: 16px;
  padding: 16px;
  flex-shrink: 0;
}

.stat-card {
  flex: 1;
}

:deep(.stat-card .el-card__body) {
  padding: 16px;
}

.stat-content {
  display: flex;
  align-items: center;
  gap: 16px;
}

.stat-icon {
  width: 50px;
  height: 50px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  flex-shrink: 0;
}

.stat-info {
  flex: 1;
}

.stat-label {
  font-size: 13px;
  color: var(--addp-text-tertiary);
  margin-bottom: 4px;
}

.stat-value {
  font-size: 24px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.content-area {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 0 16px 16px 16px;
}

.pagination {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
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
