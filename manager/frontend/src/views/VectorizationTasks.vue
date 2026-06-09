<template>
  <div class="vectorization-tasks">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('manager.vectorization.title') }}</span>
          <el-button type="primary" :icon="Refresh" circle @click="refreshTasks" />
        </div>
      </template>

      <el-table :data="tasks" v-loading="loading" stripe>
        <el-table-column prop="name" :label="t('manager.vectorization.name')" min-width="180" show-overflow-tooltip />
        <el-table-column prop="engine_id" :label="t('manager.vectorization.engine')" width="100" />
        <el-table-column prop="bucket" :label="t('manager.vectorization.bucket')" min-width="150" show-overflow-tooltip />
        <el-table-column :label="t('manager.vectorization.path')" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            {{ row.prefix || '/' }}
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.vectorization.recursive')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.recursive ? 'success' : 'info'">
              {{ row.recursive ? t('common.yes') : t('common.no') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.vectorization.enabled')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? t('manager.vectorization.enabledYes') : t('manager.vectorization.enabledNo') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.vectorization.lastStatus')" width="120">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.last_execution_status)">
              {{ statusLabel(row.last_execution_status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.vectorization.lastRunAt')" width="180">
          <template #default="{ row }">
            {{ formatDateTime(row.last_run_at) }}
          </template>
        </el-table-column>
        <el-table-column prop="created_at" :label="t('manager.vectorization.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatDateTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.vectorization.actions')" width="170" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
              {{ t('manager.vectorization.execute') }}
            </el-button>
            <el-button size="small" @click="showTaskDetail(row)">
              {{ t('manager.vectorization.detail') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        class="pagination"
        @size-change="handleSizeChange"
        @current-change="handlePageChange"
      />
    </el-card>

    <el-dialog v-model="detailDialogVisible" :title="t('manager.vectorization.dialogTitle')" width="760px">
      <el-descriptions v-if="selectedTask" :column="2" border>
        <el-descriptions-item :label="t('manager.vectorization.id')">{{ selectedTask.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.name')">{{ selectedTask.name }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.description')" :span="2">
          {{ selectedTask.description || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.engine')">{{ selectedTask.engine_id }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.bucket')">{{ selectedTask.bucket }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.path')" :span="2">
          {{ selectedTask.prefix || '/' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.recursive')">
          {{ selectedTask.recursive ? t('common.yes') : t('common.no') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.enabled')">
          {{ selectedTask.enabled ? t('manager.vectorization.enabledYes') : t('manager.vectorization.enabledNo') }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.modality')">{{ selectedTask.modality || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.fileTypes')">{{ selectedTask.file_types || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.lastExecutionId')" :span="2">
          {{ selectedTask.last_execution_id || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.lastStatus')">
          <el-tag :type="statusTagType(selectedTask.last_execution_status)">
            {{ statusLabel(selectedTask.last_execution_status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.lastRunAt')">
          {{ formatDateTime(selectedTask.last_run_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.createdAt')">
          {{ formatDateTime(selectedTask.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.updatedAt')">
          {{ formatDateTime(selectedTask.updated_at) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { openMonitorExecution } from '@addp/common-frontend'
import client from '../api/client'
import { formatDateTime } from '../utils/formatters'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const tasks = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)
const executingId = ref(null)

const detailDialogVisible = ref(false)
const selectedTask = ref(null)

const loadTasks = async () => {
  loading.value = true
  try {
    const response = await client.get('/manager/embedding_tasks', {
      params: {
        task_type: 'embedding',
        page: currentPage.value,
        page_size: pageSize.value
      }
    })
    tasks.value = response.data || []
    total.value = response.total || 0
  } catch (error) {
    console.error('加载向量化任务定义失败:', error)
    ElMessage.error(t('manager.vectorization.loadFailed'))
  } finally {
    loading.value = false
  }
}

const refreshTasks = () => {
  loadTasks()
}

const handlePageChange = () => {
  loadTasks()
}

const handleSizeChange = () => {
  currentPage.value = 1
  loadTasks()
}

const showTaskDetail = (task) => {
  selectedTask.value = task
  detailDialogVisible.value = true
}

const openTaskFromQuery = async () => {
  const taskId = Number(route.query.task_id || 0)
  if (!taskId) return
  try {
    const response = await client.get(`/manager/embedding_tasks/${taskId}`)
    selectedTask.value = response.data || response
    detailDialogVisible.value = true
  } catch (error) {
    console.error('加载向量化任务详情失败:', error)
    ElMessage.error(t('manager.vectorization.loadFailed'))
  } finally {
    router.replace({ query: { ...route.query, task_id: undefined } })
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await client.post(`/manager/tasks/embedding/${task.id}/execute`, {
      trigger_type: 'manual',
      source: 'manager'
    })
    ElMessage.success(t('manager.vectorization.executeSubmitted', { id: response.execution_id || '-' }))
    await loadTasks()
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    console.error('执行向量化任务失败:', error)
    ElMessage.error(t('manager.vectorization.executeFailed'))
  } finally {
    executingId.value = null
  }
}

const statusTagType = (status) => {
  switch (status) {
    case 'success':
      return 'success'
    case 'failed':
    case 'timeout':
      return 'danger'
    case 'running':
    case 'pending':
      return 'warning'
    case 'cancelled':
      return 'info'
    default:
      return 'info'
  }
}

const statusLabel = (status) => {
  if (!status) return t('manager.vectorization.statusNeverRun')
  if (!['pending', 'running', 'success', 'failed', 'timeout', 'cancelled'].includes(status)) {
    return status
  }
  return t(`manager.vectorization.status.${status}`)
}

onMounted(async () => {
  await loadTasks()
  await openTaskFromQuery()
})
</script>

<style scoped>
.vectorization-tasks {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.pagination {
  margin-top: 20px;
  justify-content: center;
}
</style>
