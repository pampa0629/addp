<template>
  <div class="mvt-tasks">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('manager.mvtTasks.title') }}</span>
          <div class="header-actions">
            <el-button type="primary" :icon="Plus" @click="openCreateDialog">
              {{ t('manager.mvtTasks.create') }}
            </el-button>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>
        </div>
      </template>

      <el-table :data="tasks" v-loading="loading" stripe>
        <el-table-column prop="name" :label="t('manager.mvtTasks.name')" min-width="170" show-overflow-tooltip />
        <el-table-column prop="engine_id" :label="t('manager.mvtTasks.engine')" width="100" />
        <el-table-column prop="schema_name" :label="t('manager.mvtTasks.schema')" width="130" show-overflow-tooltip />
        <el-table-column prop="table_name" :label="t('manager.mvtTasks.table')" min-width="160" show-overflow-tooltip />
        <el-table-column :label="t('manager.mvtTasks.zoom')" width="110">
          <template #default="{ row }">
            {{ row.min_zoom }} - {{ row.max_zoom }}
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.mvtTasks.enabled')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? t('manager.mvtTasks.enabledYes') : t('manager.mvtTasks.enabledNo') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.mvtTasks.lastStatus')" width="120">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.last_execution_status)">
              {{ statusLabel(row.last_execution_status) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.mvtTasks.lastRunAt')" width="180">
          <template #default="{ row }">
            {{ formatDateTime(row.last_run_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('manager.mvtTasks.actions')" width="220" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
              {{ t('manager.mvtTasks.execute') }}
            </el-button>
            <el-button size="small" @click="openEditDialog(row)">
              {{ t('manager.mvtTasks.edit') }}
            </el-button>
            <el-button size="small" @click="showTaskDetail(row)">
              {{ t('manager.mvtTasks.detail') }}
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
        @current-change="loadTasks"
      />
    </el-card>

    <el-dialog v-model="formDialogVisible" :title="formTitle" width="640px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
        <el-form-item :label="t('manager.mvtTasks.name')" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('manager.mvtTasks.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('manager.mvtTasks.engine')" prop="engine_id">
          <el-input-number v-model="form.engine_id" :min="1" />
        </el-form-item>
        <el-form-item :label="t('manager.mvtTasks.schema')" prop="schema_name">
          <el-input v-model="form.schema_name" />
        </el-form-item>
        <el-form-item :label="t('manager.mvtTasks.table')" prop="table_name">
          <el-input v-model="form.table_name" />
        </el-form-item>
        <el-form-item :label="t('manager.mvtTasks.zoom')" required>
          <div class="zoom-row">
            <el-input-number v-model="form.min_zoom" :min="0" :max="form.max_zoom" />
            <span>-</span>
            <el-input-number v-model="form.max_zoom" :min="form.min_zoom" :max="22" />
          </div>
        </el-form-item>
        <el-form-item :label="t('manager.mvtTasks.enabled')">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formDialogVisible = false">{{ t('manager.mvtTasks.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTask">{{ t('manager.mvtTasks.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailDialogVisible" :title="t('manager.mvtTasks.dialogTitle')" width="760px">
      <el-descriptions v-if="selectedTask" :column="2" border>
        <el-descriptions-item :label="t('manager.mvtTasks.id')">{{ selectedTask.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.mvtTasks.name')">{{ selectedTask.name }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.mvtTasks.description')" :span="2">
          {{ selectedTask.description || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.mvtTasks.engine')">{{ selectedTask.engine_id }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.mvtTasks.schema')">{{ selectedTask.schema_name }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.mvtTasks.table')">{{ selectedTask.table_name }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.mvtTasks.zoom')">
          {{ selectedTask.min_zoom }} - {{ selectedTask.max_zoom }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.mvtTasks.lastExecutionId')" :span="2">
          {{ selectedTask.last_execution_id || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.mvtTasks.lastStatus')">
          <el-tag :type="statusTagType(selectedTask.last_execution_status)">
            {{ statusLabel(selectedTask.last_execution_status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.mvtTasks.lastRunAt')">
          {{ formatDateTime(selectedTask.last_run_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.mvtTasks.createdAt')">
          {{ formatDateTime(selectedTask.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.mvtTasks.updatedAt')">
          {{ formatDateTime(selectedTask.updated_at) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
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

const formRef = ref(null)
const formDialogVisible = ref(false)
const saving = ref(false)
const editingId = ref(null)
const form = reactive(defaultForm())

const detailDialogVisible = ref(false)
const selectedTask = ref(null)

const formTitle = computed(() => editingId.value ? t('manager.mvtTasks.editTitle') : t('manager.mvtTasks.createTitle'))
const rules = computed(() => ({
  name: [{ required: true, message: t('manager.mvtTasks.nameRequired'), trigger: 'blur' }],
  engine_id: [{ required: true, message: t('manager.mvtTasks.engineRequired'), trigger: 'change' }],
  schema_name: [{ required: true, message: t('manager.mvtTasks.schemaRequired'), trigger: 'blur' }],
  table_name: [{ required: true, message: t('manager.mvtTasks.tableRequired'), trigger: 'blur' }]
}))

function defaultForm() {
  return {
    name: '',
    description: '',
    enabled: true,
    engine_id: 1,
    schema_name: 'public',
    table_name: '',
    min_zoom: 0,
    max_zoom: 18
  }
}

const resetForm = (task = null) => {
  Object.assign(form, defaultForm(), task || {})
  editingId.value = task?.id || null
}

const loadTasks = async () => {
  loading.value = true
  try {
    const response = await client.get('/manager/mvt_tasks', {
      params: {
        task_type: 'mvt_generation',
        page: currentPage.value,
        page_size: pageSize.value
      }
    })
    tasks.value = response.data || []
    total.value = response.total || 0
  } catch (error) {
    console.error('加载 MVT 任务定义失败:', error)
    ElMessage.error(t('manager.mvtTasks.loadFailed'))
  } finally {
    loading.value = false
  }
}

const handleSizeChange = () => {
  currentPage.value = 1
  loadTasks()
}

const openCreateDialog = () => {
  resetForm()
  formDialogVisible.value = true
}

const openEditDialog = (task) => {
  resetForm(task)
  formDialogVisible.value = true
}

const saveTask = async () => {
  await formRef.value?.validate()
  saving.value = true
  try {
    if (editingId.value) {
      await client.put(`/manager/mvt_tasks/${editingId.value}`, form)
      ElMessage.success(t('manager.mvtTasks.updateSuccess'))
    } else {
      await client.post('/manager/mvt_tasks', form)
      ElMessage.success(t('manager.mvtTasks.createSuccess'))
    }
    formDialogVisible.value = false
    await loadTasks()
  } catch (error) {
    console.error('保存 MVT 任务失败:', error)
    ElMessage.error(t('manager.mvtTasks.saveFailed'))
  } finally {
    saving.value = false
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await client.post(`/manager/tasks/mvt_generation/${task.id}/execute`, {
      trigger_type: 'manual',
      source: 'manager'
    })
    ElMessage.success(t('manager.mvtTasks.executeSubmitted', { id: response.execution_id || '-' }))
    await loadTasks()
  } catch (error) {
    console.error('执行 MVT 任务失败:', error)
    ElMessage.error(t('manager.mvtTasks.executeFailed'))
  } finally {
    executingId.value = null
  }
}

const showTaskDetail = (task) => {
  selectedTask.value = task
  detailDialogVisible.value = true
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
  if (!status) return t('manager.mvtTasks.statusNeverRun')
  if (!['pending', 'running', 'success', 'failed', 'timeout', 'cancelled'].includes(status)) {
    return status
  }
  return t(`manager.mvtTasks.status.${status}`)
}

onMounted(async () => {
  await loadTasks()
  const taskId = Number(route.query.task_id || 0)
  if (taskId) {
    try {
      const response = await client.get(`/manager/mvt_tasks/${taskId}`)
      openEditDialog(response.data || response)
    } catch (error) {
      console.error('加载 MVT 任务详情失败:', error)
      ElMessage.error(t('manager.mvtTasks.loadFailed'))
    } finally {
      router.replace({ query: { ...route.query, task_id: undefined } })
    }
  }
})
</script>

<style scoped>
.mvt-tasks {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}

.pagination {
  margin-top: 20px;
  justify-content: center;
}

.zoom-row {
  display: flex;
  gap: 12px;
  align-items: center;
}
</style>
