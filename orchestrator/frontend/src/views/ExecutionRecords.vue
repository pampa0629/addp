<template>
  <div class="execution-records">
    <div class="header">
      <h2>{{ t('orchestrator.executionRecords.title') }}</h2>
      <el-button @click="handleRefresh">{{ t('orchestrator.executionRecords.refreshBtn') }}</el-button>
    </div>

    <el-table :data="executions" style="width: 100%" v-loading="loading">
      <el-table-column prop="execution_id" :label="t('orchestrator.executionRecords.colExecutionId')" width="260" show-overflow-tooltip></el-table-column>
      <el-table-column prop="source_task_name" :label="t('orchestrator.executionRecords.colOrchestration')" width="200" show-overflow-tooltip></el-table-column>
      <el-table-column :label="t('orchestrator.executionRecords.colStatus')" width="120">
        <template #default="scope">
          <el-tag :type="getStatusType(scope.row.status)">
            {{ getStatusText(scope.row.status) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="current_step" :label="t('orchestrator.executionRecords.colCurrentStep')" width="150" show-overflow-tooltip></el-table-column>
      <el-table-column :label="t('orchestrator.executionRecords.colStartTime')" width="180">
        <template #default="scope">
          {{ formatTime(scope.row.started_at) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('orchestrator.executionRecords.colEndTime')" width="180">
        <template #default="scope">
          {{ formatTime(scope.row.completed_at) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('orchestrator.executionRecords.colError')" show-overflow-tooltip>
        <template #default="scope">
          {{ getErrorMessage(scope.row) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('orchestrator.executionRecords.colActions')" width="180" fixed="right">
        <template #default="scope">
          <el-button size="small" @click="handleViewDetail(scope.row)">{{ t('orchestrator.executionRecords.detailBtn') }}</el-button>
          <el-button size="small" @click="handleViewOrchestration(scope.row)">{{ t('orchestrator.executionRecords.viewOrchestrationBtn') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="pagination">
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        @current-change="handlePageChange"
        layout="total, prev, pager, next"
      />
    </div>

    <!-- 执行详情对话框 -->
    <el-dialog
      v-model="detailVisible"
      class="addp-dialog"
      :title="t('orchestrator.executionRecords.detailDialogTitle')"
      width="min(800px, calc(100vw - 24px))"
    >
      <div v-if="currentExecution" class="execution-detail">
        <div class="detail-actions">
          <el-button size="small" type="primary" @click="openExecutionInMonitor(currentExecution.execution_id)">
            {{ t('orchestrator.executionRecords.viewMonitorTreeBtn') }}
          </el-button>
        </div>
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('orchestrator.executionRecords.descExecutionId')">{{ currentExecution.execution_id }}</el-descriptions-item>
          <el-descriptions-item :label="t('orchestrator.executionRecords.descOrchestration')">{{ currentExecution.source_task_name || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('orchestrator.executionRecords.descStatus')">
            <el-tag :type="getStatusType(currentExecution.status)">
              {{ getStatusText(currentExecution.status) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('orchestrator.executionRecords.descStartTime')">
            {{ formatTime(currentExecution.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('orchestrator.executionRecords.descEndTime')">
            {{ formatTime(currentExecution.completed_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('orchestrator.executionRecords.descCurrentStep')" :span="2">
            {{ currentExecution.current_step || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('orchestrator.executionRecords.descError')" :span="2">
            {{ getErrorMessage(currentExecution) }}
          </el-descriptions-item>
        </el-descriptions>

        <h4 style="margin-top: 20px">{{ t('orchestrator.executionRecords.stepResults') }}</h4>
        <el-collapse v-if="getStepResults(currentExecution)">
          <el-collapse-item
            v-for="(result, stepId) in getStepResults(currentExecution)"
            :key="stepId"
          >
            <template #title>
              <span>{{ stepId }} - {{ result.status }}</span>
              <el-button
                v-if="getStepExecutionID(result)"
                size="small"
                text
                type="primary"
                class="step-monitor-link"
                @click.stop="openExecutionInMonitor(getStepExecutionID(result))"
              >
                {{ t('orchestrator.executionRecords.viewChildExecutionBtn') }}
              </el-button>
            </template>
            <pre>{{ JSON.stringify(result, null, 2) }}</pre>
          </el-collapse-item>
        </el-collapse>
        <p v-else>{{ t('orchestrator.executionRecords.noStepResults') }}</p>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onBeforeUnmount, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { openMonitorExecution } from '@addp/common-frontend'
import orchestrationAPI from '../api/orchestration'
import { navigateOrchestratorRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()
const router = useRouter()

const executions = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const detailVisible = ref(false)
const currentExecution = ref(null)
let refreshTimer = null

onMounted(() => {
  loadExecutions()
  refreshTimer = window.setInterval(() => loadExecutions(false), 5000)
})

onBeforeUnmount(() => {
  if (refreshTimer) {
    window.clearInterval(refreshTimer)
    refreshTimer = null
  }
})

async function loadExecutions(showLoading = true) {
  if (showLoading) {
    loading.value = true
  }
  try {
    const result = await orchestrationAPI.listAllExecutions({
      page: currentPage.value,
      page_size: pageSize.value
    })
    executions.value = result.data || []
    total.value = result.total
  } catch (error) {
    ElMessage.error(t('orchestrator.executionRecords.loadFailed'))
  } finally {
    if (showLoading) {
      loading.value = false
    }
  }
}

async function handleViewDetail(row) {
  try {
    const execution = await orchestrationAPI.getExecution(row.id)
    currentExecution.value = execution
    updateExecutionInList(execution)
    detailVisible.value = true
  } catch (error) {
    ElMessage.error(t('orchestrator.executionRecords.detailLoadFailed'))
  }
}

function updateExecutionInList(execution) {
  const index = executions.value.findIndex(item => item.id === execution.id)
  if (index >= 0) {
    executions.value.splice(index, 1, execution)
  }
}

function handleViewOrchestration(row) {
  if (row.source_task_id) {
    navigateOrchestratorRoute(router, `/orchestrations/${row.source_task_id}/executions`)
  }
}

function handleRefresh() {
  loadExecutions()
}

function handlePageChange() {
  loadExecutions()
}

function getStatusType(status) {
  const types = {
    pending: 'info',
    running: 'warning',
    success: 'success',
    failed: 'danger',
    timeout: 'danger',
    cancelled: 'info'
  }
  return types[status] || 'info'
}

function getStatusText(status) {
  const texts = {
    pending: t('orchestrator.executionRecords.statusPending'),
    running: t('orchestrator.executionRecords.statusRunning'),
    success: t('orchestrator.executionRecords.statusSuccess'),
    failed: t('orchestrator.executionRecords.statusFailed'),
    timeout: t('orchestrator.executionRecords.statusTimeout'),
    cancelled: t('orchestrator.executionRecords.statusCancelled')
  }
  return texts[status] || status
}

function formatTime(time) {
  if (!time) return '-'
  return new Date(time).toLocaleString()
}

function getErrorMessage(execution) {
  return execution?.error_details?.message || '-'
}

function getStepResults(execution) {
  return execution?.metadata?.step_results || null
}

function getStepExecutionID(result) {
  return result?.result?.execution_id || result?.execution_id || ''
}

async function openExecutionInMonitor(executionID) {
  if (!executionID) return
  await openMonitorExecution(executionID)
}
</script>

<style scoped>
.execution-records {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

h2 {
  margin: 0;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: center;
}

.execution-detail pre {
  background: var(--addp-bg-secondary);
  padding: 10px;
  border-radius: 4px;
  overflow-x: auto;
  font-size: 12px;
}

.detail-actions {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}

.step-monitor-link {
  margin-left: 12px;
}
</style>
