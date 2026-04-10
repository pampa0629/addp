<template>
  <div class="execution-records">
    <div class="header">
      <h2>{{ t('orchestrator.executionRecords.title') }}</h2>
      <el-button @click="handleRefresh">{{ t('orchestrator.executionRecords.refreshBtn') }}</el-button>
    </div>

    <el-table :data="executions" style="width: 100%" v-loading="loading">
      <el-table-column prop="id" :label="t('orchestrator.executionRecords.colExecutionId')" width="100"></el-table-column>
      <el-table-column prop="orchestration.name" :label="t('orchestrator.executionRecords.colOrchestration')" width="200" show-overflow-tooltip></el-table-column>
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
      <el-table-column prop="error_message" :label="t('orchestrator.executionRecords.colError')" show-overflow-tooltip></el-table-column>
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
    <el-dialog v-model="detailVisible" :title="t('orchestrator.executionRecords.detailDialogTitle')" width="800px">
      <div v-if="currentExecution" class="execution-detail">
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('orchestrator.executionRecords.descExecutionId')">{{ currentExecution.id }}</el-descriptions-item>
          <el-descriptions-item :label="t('orchestrator.executionRecords.descOrchestration')">{{ currentExecution.orchestration?.name || '-' }}</el-descriptions-item>
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
            {{ currentExecution.error_message || '-' }}
          </el-descriptions-item>
        </el-descriptions>

        <h4 style="margin-top: 20px">{{ t('orchestrator.executionRecords.stepResults') }}</h4>
        <el-collapse v-if="currentExecution.step_results">
          <el-collapse-item
            v-for="(result, stepId) in currentExecution.step_results"
            :key="stepId"
            :title="`${stepId} - ${result.status}`"
          >
            <pre>{{ JSON.stringify(result, null, 2) }}</pre>
          </el-collapse-item>
        </el-collapse>
        <p v-else>{{ t('orchestrator.executionRecords.noStepResults') }}</p>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import orchestrationAPI from '../api/orchestration'

const { t } = useI18n()
const router = useRouter()

const executions = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const detailVisible = ref(false)
const currentExecution = ref(null)

onMounted(() => {
  loadExecutions()
})

async function loadExecutions() {
  loading.value = true
  try {
    const result = await orchestrationAPI.listAllExecutions({
      limit: pageSize.value,
      offset: (currentPage.value - 1) * pageSize.value
    })
    executions.value = result.items
    total.value = result.total
  } catch (error) {
    ElMessage.error(t('orchestrator.executionRecords.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function handleViewDetail(row) {
  try {
    currentExecution.value = await orchestrationAPI.getExecution(row.id)
    detailVisible.value = true
  } catch (error) {
    ElMessage.error(t('orchestrator.executionRecords.detailLoadFailed'))
  }
}

function handleViewOrchestration(row) {
  if (row.orchestration_id) {
    router.push(`/orchestrations/${row.orchestration_id}/executions`)
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
    completed: 'success',
    failed: 'danger'
  }
  return types[status] || 'info'
}

function getStatusText(status) {
  const texts = {
    pending: t('orchestrator.executionRecords.statusPending'),
    running: t('orchestrator.executionRecords.statusRunning'),
    completed: t('orchestrator.executionRecords.statusCompleted'),
    failed: t('orchestrator.executionRecords.statusFailed')
  }
  return texts[status] || status
}

function formatTime(time) {
  if (!time) return '-'
  return new Date(time).toLocaleString()
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
</style>
