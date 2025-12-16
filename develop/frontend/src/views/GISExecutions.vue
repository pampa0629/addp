<template>
  <div class="gis-executions">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>GIS 执行历史</span>
          <el-button @click="goToTasks">返回任务列表</el-button>
        </div>
      </template>

      <!-- 筛选栏 -->
      <el-form :inline="true" class="filter-form">
        <el-form-item label="任务">
          <el-select v-model="filters.task_id" placeholder="全部" clearable style="width: 200px" @change="loadExecutions">
            <el-option label="全部" value="" />
            <el-option
              v-for="task in tasks"
              :key="task.id"
              :label="task.name"
              :value="task.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="filters.status" placeholder="全部" clearable style="width: 120px" @change="loadExecutions">
            <el-option label="全部" value="" />
            <el-option label="待执行" value="pending" />
            <el-option label="运行中" value="running" />
            <el-option label="成功" value="success" />
            <el-option label="失败" value="failed" />
            <el-option label="超时" value="timeout" />
          </el-select>
        </el-form-item>
        <el-form-item label="触发类型">
          <el-select v-model="filters.trigger_type" placeholder="全部" clearable style="width: 140px" @change="loadExecutions">
            <el-option label="全部" value="" />
            <el-option label="手动" value="manual" />
            <el-option label="调度" value="schedule" />
            <el-option label="编排" value="orchestrator" />
            <el-option label="API" value="api" />
          </el-select>
        </el-form-item>
        <el-form-item label="时间范围">
          <el-date-picker
            v-model="dateRange"
            type="daterange"
            range-separator="至"
            start-placeholder="开始日期"
            end-placeholder="结束日期"
            value-format="YYYY-MM-DD"
            @change="onDateRangeChange"
            style="width: 280px"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="loadExecutions">搜索</el-button>
          <el-button @click="resetFilters">重置</el-button>
        </el-form-item>
      </el-form>

      <!-- 执行列表 -->
      <el-table :data="executions" v-loading="loading" style="width: 100%">
        <el-table-column prop="id" label="执行ID" width="100">
          <template #default="{ row }">
            <el-link type="primary" @click="viewDetail(row.id)">{{ row.id }}</el-link>
          </template>
        </el-table-column>
        <el-table-column label="任务名称" min-width="180">
          <template #default="{ row }">
            {{ row.task_name || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="getStatusType(row.status)" size="small">
              {{ getStatusLabel(row.status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="trigger_type" label="触发" width="100">
          <template #default="{ row }">
            {{ getTriggerTypeLabel(row.trigger_type) }}
          </template>
        </el-table-column>
        <el-table-column label="结果数" width="100">
          <template #default="{ row }">
            <span v-if="row.result_count !== null && row.result_count !== undefined">
              {{ row.result_count.toLocaleString() }}
            </span>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="执行时间" width="120">
          <template #default="{ row }">
            <span v-if="row.execution_time_ms">
              {{ formatDuration(row.execution_time_ms) }}
            </span>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="started_at" label="开始时间" width="160">
          <template #default="{ row }">
            {{ formatTime(row.started_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="300" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="viewDetail(row.id)">查看详情</el-button>
            <el-button
              v-if="row.status === 'success' && row.result_table"
              size="small"
              @click="viewResult(row)"
            >
              查看结果
            </el-button>
            <el-button
              v-if="row.status === 'failed' || row.status === 'timeout'"
              size="small"
              type="warning"
              @click="retry(row)"
            >
              重试
            </el-button>
            <el-button
              v-if="row.status === 'running' || row.status === 'pending'"
              size="small"
              type="danger"
              @click="cancel(row)"
            >
              取消
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="loadExecutions"
        @current-change="loadExecutions"
        style="margin-top: 20px; justify-content: flex-end"
      />
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as spatialApi from '@/api/spatial'

const route = useRoute()
const router = useRouter()

const loading = ref(false)
const executions = ref([])
const tasks = ref([])
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const filters = ref({
  task_id: '',
  status: '',
  trigger_type: '',
  start_date: '',
  end_date: ''
})

const dateRange = ref([])

let refreshTimer = null

// 加载执行列表
const loadExecutions = async () => {
  loading.value = true
  try {
    const res = await spatialApi.listExecutions(
      currentPage.value,
      pageSize.value,
      filters.value
    )
    executions.value = res.executions || []
    total.value = res.total || 0

    // 自动刷新：如果有运行中的执行
    checkAutoRefresh()
  } catch (error) {
    ElMessage.error('加载执行列表失败: ' + error.message)
  } finally {
    loading.value = false
  }
}

// 加载任务列表（用于筛选）
const loadTasks = async () => {
  try {
    const res = await spatialApi.listTasks(1, 100)
    tasks.value = res.tasks || []
  } catch (error) {
    console.error('加载任务列表失败:', error)
  }
}

// 自动刷新检测
const checkAutoRefresh = () => {
  const hasRunning = executions.value.some(e => e.status === 'running' || e.status === 'pending')

  if (hasRunning && !refreshTimer) {
    // 启动定时器（每 5 秒刷新一次）
    refreshTimer = setInterval(() => {
      loadExecutions()
    }, 5000)
  } else if (!hasRunning && refreshTimer) {
    // 停止定时器
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

// 重置筛选
const resetFilters = () => {
  filters.value = {
    task_id: '',
    status: '',
    trigger_type: '',
    start_date: '',
    end_date: ''
  }
  dateRange.value = []
  currentPage.value = 1
  loadExecutions()
}

// 日期范围变化
const onDateRangeChange = (val) => {
  if (val && val.length === 2) {
    filters.value.start_date = val[0]
    filters.value.end_date = val[1]
  } else {
    filters.value.start_date = ''
    filters.value.end_date = ''
  }
  loadExecutions()
}

// 查看详情
const viewDetail = (id) => {
  router.push({
    name: 'GISExecutionDetail',
    params: { id }
  })
}

// 查看结果
const viewResult = (execution) => {
  const url = `/manager/data-explorer?resource=develop&table=${execution.result_table}`
  window.open(url, '_blank')
}

// 重试执行
const retry = async (execution) => {
  try {
    const res = await spatialApi.retryExecution(execution.id)
    ElMessage.success('重试已提交，执行ID: ' + res.execution_id)
    loadExecutions()
  } catch (error) {
    ElMessage.error('重试失败: ' + error.message)
  }
}

// 取消执行
const cancel = async (execution) => {
  ElMessageBox.confirm(
    `确定要取消执行 #${execution.id} 吗？`,
    '确认取消',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    }
  ).then(async () => {
    try {
      await spatialApi.cancelExecution(execution.id)
      ElMessage.success('执行已取消')
      loadExecutions()
    } catch (error) {
      ElMessage.error('取消失败: ' + error.message)
    }
  }).catch(() => {})
}

// 返回任务列表
const goToTasks = () => {
  router.push({ name: 'GISTasks' })
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
    api: 'API'
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
  // 从 URL 参数获取 task_id（如果有）
  if (route.query.task_id) {
    filters.value.task_id = parseInt(route.query.task_id)
  }

  loadTasks()
  loadExecutions()
})

onUnmounted(() => {
  // 清理定时器
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
})
</script>

<style scoped>
.gis-executions {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.filter-form {
  margin-bottom: 16px;
  padding-bottom: 16px;
  border-bottom: 1px solid #ebeef5;
}

.text-muted {
  color: #909399;
}
</style>
