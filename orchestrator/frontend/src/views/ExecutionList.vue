<template>
  <div class="execution-list">
    <div class="header">
      <h2>{{ t('orchestrator.executionList.title') }}</h2>
      <div class="header-actions">
        <MonitorExecutionsButton
          module="orchestrator"
          task-type="orchestration"
          :source-task-id="route.params.id"
          scope="task"
          type="primary"
          plain
        />
        <el-button @click="handleBack">{{ t('orchestrator.executionList.backBtn') }}</el-button>
      </div>
    </div>

    <el-table :data="executions" style="width: 100%" v-loading="loading">
      <el-table-column prop="execution_id" :label="t('orchestrator.executionList.colExecutionId')" width="260" show-overflow-tooltip></el-table-column>
      <el-table-column :label="t('orchestrator.executionList.colStatus')" width="120">
        <template #default="scope">
          <el-tag :type="executionStatusType(scope.row.status)">
            {{ getStatusText(scope.row.status, t) }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="current_step" :label="t('orchestrator.executionList.colCurrentStep')" width="150"></el-table-column>
      <el-table-column :label="t('orchestrator.executionList.colStartTime')" width="180">
        <template #default="scope">
          {{ formatTime(scope.row.started_at) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('orchestrator.executionList.colEndTime')" width="180">
        <template #default="scope">
          {{ formatTime(scope.row.completed_at) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('orchestrator.executionList.colError')" show-overflow-tooltip>
        <template #default="scope">
          {{ executionFailureMessage(scope.row, t) || '-' }}
        </template>
      </el-table-column>
      <el-table-column :label="t('orchestrator.executionList.colActions')" width="120">
        <template #default="scope">
          <el-button size="small" @click="handleViewDetail(scope.row)">{{ t('orchestrator.executionList.detailBtn') }}</el-button>
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
      :title="t('orchestrator.executionList.detailDialogTitle')"
      width="min(800px, calc(100vw - 24px))"
      @opened="focusDetailClose"
    >
      <div v-if="currentExecution" ref="detailContentRef" class="execution-detail">
        <div class="detail-actions">
          <el-button size="small" type="primary" @click="openExecutionInMonitor(currentExecution.execution_id)">
            {{ t('orchestrator.executionList.viewMonitorTreeBtn') }}
          </el-button>
        </div>
        <OrchestrationExecutionResult :current-execution="currentExecution" />
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onBeforeUnmount, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import orchestrationAPI from '../api/orchestration'
import OrchestrationExecutionResult from '../components/OrchestrationExecutionResult.vue'
import { executionFailureMessage, executionStatusType, formatTime, getStatusText } from '../utils/executionPresentation'
import { navigateOrchestratorRoute, orchestrationListLocation } from '@/utils/moduleNavigation'
import { MonitorExecutionsButton, openMonitorExecution, useConsolePageDescriptor, focusElement } from '@common-ui'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const executions = ref([])
const orchestration = ref(null)
useConsolePageDescriptor(router, 'orchestrator', {
  title: computed(() => t('orchestrator.executionList.recentVisitTitle')),
  subject: computed(() => orchestration.value?.name || ''),
  ready: computed(() => Boolean(orchestration.value?.name))
})
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const detailContentRef = ref(null)
function focusDetailClose() {
  focusElement(detailContentRef.value?.closest('.el-dialog')?.querySelector('.el-dialog__headerbtn'), { preserveDialogFocus: true })
}

const detailVisible = ref(false)
const currentExecution = ref(null)
let refreshTimer = null

onMounted(() => {
  orchestrationAPI.get(route.params.id).then(data => { orchestration.value = data }).catch(() => {})
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
    const orchId = route.params.id
    const result = await orchestrationAPI.listExecutions(orchId, {
      page: currentPage.value,
      page_size: pageSize.value
    })
    executions.value = result.data || []
    total.value = result.total
  } catch (error) {
    ElMessage.error(t('orchestrator.executionList.loadFailed'))
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
    ElMessage.error(t('orchestrator.executionList.detailLoadFailed'))
  }
}

function updateExecutionInList(execution) {
  const index = executions.value.findIndex(item => item.id === execution.id)
  if (index >= 0) {
    executions.value.splice(index, 1, execution)
  }
}

function handlePageChange() {
  loadExecutions()
}

function handleBack() {
  navigateOrchestratorRoute(router, orchestrationListLocation(route.query), { history: 'replace' })
}

async function openExecutionInMonitor(executionID) {
  if (!executionID) return
  await openMonitorExecution(executionID)
}
</script>

<style scoped>
.execution-list {
  padding: 20px;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.header-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.header-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}

h2 {
  margin: 0;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: center;
}

.detail-actions {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 12px;
}

</style>
