<template>
  <div>
    <div class="page-header">
      <h2>{{ t('quality.checkTask.title') }}</h2>
      <el-button type="primary" :icon="Plus" @click="showCreateDialog = true">{{ t('quality.checkTask.createTask') }}</el-button>
    </div>

    <el-table :data="tasks" v-loading="loading" border>
      <el-table-column prop="id" :label="t('quality.checkTask.id')" width="80" />
      <el-table-column prop="name" :label="t('quality.checkTask.taskName')" />
      <el-table-column prop="engine_id" :label="t('quality.checkTask.engineId')" width="100" />
      <el-table-column prop="schema_name" :label="t('quality.checkTask.schema')" width="120" />
      <el-table-column prop="table_name" :label="t('quality.checkTask.table')" width="150" />
      <el-table-column prop="enabled" :label="t('quality.checkTask.enabled')" width="80">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? t('quality.checkTask.yes') : t('quality.checkTask.no') }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="last_run_at" :label="t('quality.checkTask.lastRun')" width="180">
        <template #default="{ row }">{{ row.last_run_at ? new Date(row.last_run_at).toLocaleString() : '-' }}</template>
      </el-table-column>
      <el-table-column :label="t('quality.checkTask.actions')" width="260">
        <template #default="{ row }">
          <el-button size="small" @click="editTask(row)">{{ t('quality.checkTask.edit') }}</el-button>
          <el-button size="small" type="primary" @click="runTask(row.id)">{{ t('quality.checkTask.run') }}</el-button>
          <el-button size="small" @click="deleteTask(row.id)" type="danger">{{ t('quality.checkTask.delete') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="showCreateDialog" :title="dialogTitle" width="500px">
      <el-form :model="form" label-width="100px">
        <el-form-item :label="t('quality.checkTask.name')"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('quality.checkTask.description')"><el-input v-model="form.description" type="textarea" /></el-form-item>
        <el-form-item :label="t('quality.checkTask.engineIdLabel')"><el-input-number v-model="form.engine_id" :min="1" :disabled="isEditing" /></el-form-item>
        <el-form-item :label="t('quality.checkTask.schemaLabel')"><el-input v-model="form.schema_name" :placeholder="t('quality.checkTask.schemaPlaceholder')" :disabled="isEditing" /></el-form-item>
        <el-form-item :label="t('quality.checkTask.tableLabel')"><el-input v-model="form.table_name" :placeholder="t('quality.checkTask.tablePlaceholder')" :disabled="isEditing" /></el-form-item>
        <el-form-item v-if="isEditing" :label="t('quality.checkTask.enabled')">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="closeDialog">{{ t('quality.checkTask.cancel') }}</el-button>
        <el-button type="primary" @click="saveTask">{{ t('quality.checkTask.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { checkTaskAPI } from '../api/quality'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute()

const tasks = ref([])
const loading = ref(false)
const showCreateDialog = ref(false)
const editingTaskID = ref(null)
const openedRouteTaskID = ref('')
const defaultForm = () => ({ name: '', description: '', engine_id: 1, schema_name: '', table_name: '', enabled: true })
const form = ref(defaultForm())
const isEditing = computed(() => editingTaskID.value !== null)
const dialogTitle = computed(() => isEditing.value ? t('quality.checkTask.editTitle') : t('quality.checkTask.createTitle'))

const fetchTasks = async () => {
  loading.value = true
  try {
    const res = await checkTaskAPI.list()
    tasks.value = res || []
    openTaskFromRoute()
  } finally {
    loading.value = false
  }
}

const closeDialog = () => {
  showCreateDialog.value = false
  editingTaskID.value = null
  form.value = defaultForm()
}

const editTask = (task) => {
  editingTaskID.value = task.id
  form.value = {
    name: task.name || '',
    description: task.description || '',
    engine_id: task.engine_id || 1,
    schema_name: task.schema_name || '',
    table_name: task.table_name || '',
    enabled: task.enabled !== false
  }
  showCreateDialog.value = true
}

const openTaskFromRoute = () => {
  const taskID = String(route.query.task_id || '').trim()
  if (!taskID || openedRouteTaskID.value === taskID) return
  const task = tasks.value.find(item => String(item.id) === taskID)
  if (!task) return
  openedRouteTaskID.value = taskID
  editTask(task)
}

const saveTask = async () => {
  try {
    if (isEditing.value) {
      await checkTaskAPI.update(editingTaskID.value, {
        name: form.value.name,
        description: form.value.description,
        enabled: form.value.enabled
      })
      ElMessage.success(t('quality.checkTask.updateSuccess'))
    } else {
      await checkTaskAPI.create(form.value)
      ElMessage.success(t('quality.checkTask.createSuccess'))
    }
    closeDialog()
    await fetchTasks()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || (isEditing.value ? t('quality.checkTask.updateFailed') : t('quality.checkTask.createFailed')))
  }
}

const runTask = async (id) => {
  try {
    const res = await checkTaskAPI.run(id)
    ElMessage.success(t('quality.checkTask.runSuccess', { id: res.execution_id }))
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('quality.checkTask.runFailed'))
  }
}

const deleteTask = async (id) => {
  await ElMessageBox.confirm(t('quality.checkTask.deleteConfirm'), t('quality.checkTask.deleteTitle'), { type: 'warning' })
  await checkTaskAPI.delete(id)
  ElMessage.success(t('quality.checkTask.deleteSuccess'))
  await fetchTasks()
}

onMounted(fetchTasks)

watch(() => route.query.task_id, () => {
  openTaskFromRoute()
})
</script>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
</style>
