<template>
  <div class="execution-monitor-page">
    <!-- 工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <h2>{{ t('develop.execution.title') }}</h2>
        <el-button @click="handleRefresh">
          <el-icon><Refresh /></el-icon>
          {{ t('develop.execution.refresh') }}
        </el-button>
        <el-switch
          v-model="autoRefresh"
          :active-text="t('develop.execution.autoRefresh')"
          :inactive-text="t('develop.execution.manual')"
          style="margin-left: 12px;"
        />
      </div>
      <div class="toolbar-right">
        <!-- 筛选器 -->
        <el-select
          v-model="filters.dev_type"
          :placeholder="t('develop.execution.filterType')"
          clearable
          style="width: 120px; margin-right: 10px;"
        >
          <el-option :label="t('develop.execution.typeQuery')" value="query" />
          <el-option :label="t('develop.execution.typeWorkflow')" value="workflow" />
          <el-option :label="t('develop.execution.typeScript')" value="script" />
        </el-select>
        <el-select
          v-model="filters.status"
          :placeholder="t('develop.execution.filterStatus')"
          clearable
          style="width: 120px; margin-right: 10px;"
        >
          <el-option :label="t('develop.execution.statusPending')" value="pending" />
          <el-option :label="t('develop.execution.statusRunning')" value="running" />
          <el-option :label="t('develop.execution.statusSuccess')" value="success" />
          <el-option :label="t('develop.execution.statusFailed')" value="failed" />
          <el-option :label="t('develop.execution.statusTimeout')" value="timeout" />
          <el-option :label="t('develop.execution.statusCancelled')" value="cancelled" />
        </el-select>
        <el-select
          v-model="filters.trigger_type"
          :placeholder="t('develop.execution.filterTrigger')"
          clearable
          style="width: 120px; margin-right: 10px;"
        >
          <el-option :label="t('develop.execution.triggerManual')" value="manual" />
          <el-option :label="t('develop.execution.triggerSchedule')" value="scheduled" />
        </el-select>
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          :range-separator="t('develop.execution.dateSeparator')"
          :start-placeholder="t('develop.execution.startDate')"
          :end-placeholder="t('develop.execution.endDate')"
          style="width: 240px;"
        />
      </div>
    </div>

    <!-- 统计卡片 -->
    <div class="stats-cards">
      <el-card class="stat-card">
        <div class="stat-content">
          <div class="stat-icon" style="background: var(--el-color-primary);">
            <el-icon :size="24"><DataLine /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-label">{{ t('develop.execution.statTotal') }}</div>
            <div class="stat-value">{{ statistics.total_executions }}</div>
          </div>
        </div>
      </el-card>
      <el-card class="stat-card">
        <div class="stat-content">
          <div class="stat-icon" style="background: var(--el-color-success);">
            <el-icon :size="24"><SuccessFilled /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-label">{{ t('develop.execution.statSuccessRate') }}</div>
            <div class="stat-value">{{ statistics.success_rate.toFixed(1) }}%</div>
          </div>
        </div>
      </el-card>
      <el-card class="stat-card">
        <div class="stat-content">
          <div class="stat-icon" style="background: var(--el-color-warning);">
            <el-icon :size="24"><Timer /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-label">{{ t('develop.execution.statAvgDuration') }}</div>
            <div class="stat-value">{{ formatDuration(statistics.avg_execution_time_ms) }}</div>
          </div>
        </div>
      </el-card>
      <el-card class="stat-card">
        <div class="stat-content">
          <div class="stat-icon" style="background: var(--el-color-danger);">
            <el-icon :size="24"><Loading /></el-icon>
          </div>
          <div class="stat-info">
            <div class="stat-label">{{ t('develop.execution.statRunning') }}</div>
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
      >
        <el-table-column prop="execution_id" :label="t('develop.execution.colId')" width="200" show-overflow-tooltip />
        <el-table-column :label="t('develop.execution.colTaskName')" min-width="150">
          <template #default="{ row }">
            {{ row.dev_task?.name || '-' }}
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.execution.colType')" width="100">
          <template #default="{ row }">
            <el-tag :type="getTypeColor(row.task_type)">
              {{ getTypeLabel(row.task_type) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.execution.colStatus')" width="120">
          <template #default="{ row }">
            <el-tag :type="getStatusColor(row.status)">
              <el-icon v-if="row.status === 'running'" class="rotating">
                <Loading />
              </el-icon>
              {{ getStatusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.execution.colProgress')" width="150">
          <template #default="{ row }">
            <el-progress
              :percentage="row.progress || 0"
              :status="getProgressStatus(row.status)"
            />
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.execution.colTrigger')" width="100">
          <template #default="{ row }">
            {{ getTriggerLabel(row.trigger_type) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.execution.colDuration')" width="120">
          <template #default="{ row }">
            {{ formatDuration(row.execution_time_ms) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.execution.colStartTime')" width="160">
          <template #default="{ row }">
            {{ formatTime(row.started_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('develop.execution.colActions')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button
              type="primary"
              size="small"
              @click="handleViewDetail(row)"
            >
              <el-icon><View /></el-icon>
              {{ t('develop.execution.detail') }}
            </el-button>
            <el-button
              v-if="['failed', 'timeout', 'cancelled'].includes(row.status)"
              type="success"
              size="small"
              @click="handleRetry(row)"
            >
              <el-icon><RefreshRight /></el-icon>
              {{ t('develop.execution.retry') }}
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
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import {
  Refresh,
  DataLine,
  SuccessFilled,
  Timer,
  Loading,
  View,
  RefreshRight
} from '@element-plus/icons-vue'
import {
  listExecutions,
  retryExecution,
  getExecutionStatistics
} from '@/api/execution'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

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
  source_task_id: ''
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
      ElMessage.error(t('develop.execution.loadFailed') + (error.response?.data?.error || error.message))
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
    const params = {
      source_task_id: filters.source_task_id
    }
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
  const labels = {
    query: 'SQL',
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

const handleRetry = async (row) => {
  try {
    await retryExecution(row.execution_id)
    ElMessage.success(t('develop.execution.retrySubmitted'))
    loadExecutions()
    loadStatistics()
  } catch (error) {
    console.error('重试执行失败:', error)
    ElMessage.error(t('develop.execution.retryFailed') + (error.response?.data?.error || error.message))
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
  if (route.query.source_task_id) {
    filters.source_task_id = String(route.query.source_task_id)
  }
  if (route.query.dev_type) {
    filters.dev_type = String(route.query.dev_type)
  }
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
  overflow: visible;
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
