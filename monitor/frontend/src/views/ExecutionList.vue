<template>
  <div class="execution-list">
    <el-card shadow="hover">
      <template #header>
        <div class="card-header">
          <span>{{ t('monitor.execution.title') }}</span>
        </div>
      </template>

      <!-- 过滤器 -->
      <el-form :inline="true" :model="filters" class="filter-form">
        <el-form-item :label="t('monitor.execution.filter.module')">
          <el-select v-model="filters.module" :placeholder="t('monitor.execution.filter.module_placeholder')" clearable style="width: 150px;">
            <el-option label="Transfer" value="transfer" />
            <el-option label="Develop" value="develop" />
            <el-option label="Orchestrator" value="orchestrator" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.execution.filter.status')">
          <el-select v-model="filters.status" :placeholder="t('monitor.execution.filter.status_placeholder')" clearable style="width: 150px;">
            <el-option :label="t('monitor.execution.status.pending')" value="pending" />
            <el-option :label="t('monitor.execution.status.running')" value="running" />
            <el-option :label="t('monitor.execution.status.success')" value="success" />
            <el-option :label="t('monitor.execution.status.failed')" value="failed" />
            <el-option :label="t('monitor.execution.status.timeout')" value="timeout" />
            <el-option :label="t('monitor.execution.status.cancelled')" value="cancelled" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.execution.filter.trigger_type')">
          <el-select v-model="filters.trigger_type" :placeholder="t('monitor.execution.filter.trigger_placeholder')" clearable style="width: 150px;">
            <el-option :label="t('monitor.execution.trigger.manual')" value="manual" />
            <el-option :label="t('monitor.execution.trigger.schedule')" value="schedule" />
            <el-option label="API" value="api" />
            <el-option :label="t('monitor.execution.trigger.orchestrator')" value="orchestrator" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">
            <el-icon><Search /></el-icon>
            {{ t('monitor.execution.filter.search') }}
          </el-button>
          <el-button @click="handleReset">
            <el-icon><RefreshLeft /></el-icon>
            {{ t('monitor.execution.filter.reset') }}
          </el-button>
        </el-form-item>
      </el-form>

      <!-- 执行记录表格 -->
      <execution-table
        :executions="executions"
        @view="handleViewExecution"
        v-loading="loading"
      />

      <!-- 分页 -->
      <div class="pagination">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.page_size"
          :page-sizes="[10, 20, 50, 100]"
          :total="pagination.total"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="handleSizeChange"
          @current-change="handlePageChange"
        />
      </div>
    </el-card>

    <!-- 执行详情对话框 -->
    <el-dialog
      v-model="detailDialogVisible"
      :title="t('monitor.execution.detail.title')"
      width="60%"
      :close-on-click-modal="false"
    >
      <div v-if="currentExecution">
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('monitor.execution.detail.id')">
            {{ currentExecution.id }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.uuid')">
            {{ currentExecution.execution_id }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.module')">
            <el-tag size="small">{{ currentExecution.module }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.type')">
            {{ currentExecution.execution_type }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.status')">
            <el-tag :type="getStatusType(currentExecution.status)">
              {{ getStatusText(currentExecution.status) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.progress')">
            {{ currentExecution.progress }}%
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.trigger_type')">
            {{ currentExecution.trigger_type }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.duration')">
            {{ formatDuration(currentExecution.execution_time_ms) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.created_at')">
            {{ formatDate(currentExecution.created_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.started_at')">
            {{ formatDate(currentExecution.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.completed_at')">
            {{ formatDate(currentExecution.completed_at) }}
          </el-descriptions-item>
        </el-descriptions>

        <!-- 执行结果 -->
        <div v-if="currentExecution.result" style="margin-top: 20px;">
          <h4>{{ t('monitor.execution.detail.result') }}</h4>
          <el-input
            type="textarea"
            :value="JSON.stringify(currentExecution.result, null, 2)"
            :rows="10"
            readonly
          />
        </div>

        <!-- 错误详情 -->
        <div v-if="currentExecution.error_details" style="margin-top: 20px;">
          <h4>{{ t('monitor.execution.detail.error') }}</h4>
          <el-alert
            type="error"
            :closable="false"
            :description="JSON.stringify(currentExecution.error_details, null, 2)"
          />
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { listExecutions, getExecution } from '@/api/monitor'
import ExecutionTable from '@/components/ExecutionTable.vue'

const { t } = useI18n()

// 过滤条件
const filters = ref({
  module: '',
  status: '',
  trigger_type: ''
})

// 分页
const pagination = ref({
  page: 1,
  page_size: 20,
  total: 0
})

// 数据
const executions = ref([])
const loading = ref(false)
const detailDialogVisible = ref(false)
const currentExecution = ref(null)

// 加载执行记录
async function loadExecutions() {
  loading.value = true
  try {
    const params = {
      ...filters.value,
      page: pagination.value.page,
      page_size: pagination.value.page_size
    }
    const data = await listExecutions(params)
    executions.value = data.executions || []
    pagination.value.total = data.total || 0
  } catch (error) {
    ElMessage.error(t('monitor.execution.load_failed'))
    console.error(error)
  } finally {
    loading.value = false
  }
}

// 查询
function handleSearch() {
  pagination.value.page = 1
  loadExecutions()
}

// 重置
function handleReset() {
  filters.value = {
    module: '',
    status: '',
    trigger_type: ''
  }
  pagination.value.page = 1
  loadExecutions()
}

// 分页变化
function handlePageChange(page) {
  pagination.value.page = page
  loadExecutions()
}

function handleSizeChange(size) {
  pagination.value.page_size = size
  pagination.value.page = 1
  loadExecutions()
}

// 查看执行详情
async function handleViewExecution(row) {
  try {
    const data = await getExecution(row.id)
    currentExecution.value = data
    detailDialogVisible.value = true
  } catch (error) {
    ElMessage.error(t('monitor.execution.detail_failed'))
    console.error(error)
  }
}

// 辅助函数
function getStatusType(status) {
  const typeMap = {
    pending: 'info',
    running: 'warning',
    success: 'success',
    failed: 'danger',
    timeout: 'danger',
    cancelled: 'info'
  }
  return typeMap[status] || 'info'
}

function getStatusText(status) {
  const textMap = {
    pending: t('monitor.execution.status.pending'),
    running: t('monitor.execution.status.running'),
    success: t('monitor.execution.status.success'),
    failed: t('monitor.execution.status.failed'),
    timeout: t('monitor.execution.status.timeout'),
    cancelled: t('monitor.execution.status.cancelled'),
  }
  return textMap[status] || status
}

function formatDate(date) {
  if (!date) return '-'
  return new Date(date).toLocaleString('zh-CN')
}

function formatDuration(ms) {
  if (!ms) return '-'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`
  return `${(ms / 60000).toFixed(2)}min`
}

// 初始化
onMounted(() => {
  loadExecutions()
})
</script>

<style scoped>
.execution-list {
  padding: 20px;
  min-height: 100vh;
  background: var(--addp-bg-secondary);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header span {
  color: var(--addp-text-primary);
  font-weight: 500;
  font-size: 16px;
}

.filter-form {
  margin-bottom: 20px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}
</style>
