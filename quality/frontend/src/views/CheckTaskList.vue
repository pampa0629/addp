<template>
  <div>
    <div class="page-header">
      <h2>{{ t('quality.checkTask.title') }}</h2>
      <el-button type="primary" :icon="Plus" @click="requestCreateDialog">{{ t('quality.checkTask.createTask') }}</el-button>
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
          <el-button size="small" @click="requestEditTask(row)">{{ t('quality.checkTask.edit') }}</el-button>
          <el-button size="small" type="primary" @click="runTask(row.id)">{{ t('quality.checkTask.run') }}</el-button>
          <el-button size="small" @click="deleteTask(row.id)" type="danger">{{ t('quality.checkTask.delete') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.page_size"
      :page-sizes="[20, 50, 100]"
      layout="total, sizes, prev, pager, next"
      :total="pagination.total"
      class="pagination"
      @size-change="fetchTasks"
      @current-change="fetchTasks"
    />

    <el-dialog v-model="showCreateDialog" :title="dialogTitle" width="500px" @closed="clearTaskDialogRoute">
      <el-form :model="form" label-width="100px">
        <el-form-item :label="t('quality.checkTask.name')"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('quality.checkTask.description')"><el-input v-model="form.description" type="textarea" /></el-form-item>
        <el-form-item :label="t('quality.checkTask.engineIdLabel')">
          <el-select v-model="form.engine_id" :placeholder="t('quality.checkTask.enginePlaceholder')" style="width:100%">
            <el-option v-for="engine in engines" :key="engine.id" :label="`${engine.name}（${engine.engine_type}）`" :value="engine.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('quality.checkTask.schemaLabel')"><el-input v-model="form.schema_name" :placeholder="t('quality.checkTask.schemaPlaceholder')" /></el-form-item>
        <el-form-item :label="t('quality.checkTask.tableLabel')"><el-input v-model="form.table_name" :placeholder="t('quality.checkTask.tablePlaceholder')" /></el-form-item>
        <el-form-item :label="t('quality.checkTask.enabled')">
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
import { useRoute, useRouter } from 'vue-router'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { checkTaskAPI, systemEngineAPI } from '../api/quality'
import { navigateQualityRoute } from '../utils/moduleNavigation'
import { resolveCheckTaskRouteState } from '../utils/checkTaskRouteState'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const tasks = ref([])
const loading = ref(false)
const showCreateDialog = ref(false)
const editingTaskID = ref(null)
let routeDataReady = false
let routeRestoreSequence = 0
const defaultForm = () => ({ name: '', description: '', engine_id: null, schema_name: '', table_name: '', enabled: true })
const form = ref(defaultForm())
const engines = ref([])
const pagination = ref({ page: 1, page_size: 20, total: 0 })
const isEditing = computed(() => editingTaskID.value !== null)
const dialogTitle = computed(() => isEditing.value ? t('quality.checkTask.editTitle') : t('quality.checkTask.createTitle'))

const fetchTasks = async () => {
  loading.value = true
  try {
    const res = await checkTaskAPI.list({ page: pagination.value.page, page_size: pagination.value.page_size })
    tasks.value = res?.data || []
    pagination.value.total = res?.total || 0
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('quality.checkTask.loadFailed'))
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
    engine_id: task.engine_id || null,
    schema_name: task.schema_name || '',
    table_name: task.table_name || '',
    enabled: task.enabled !== false
  }
  showCreateDialog.value = true
}

const requestCreateDialog = async () => {
  const routeState = resolveCheckTaskRouteState({ create: '1' })
  await navigateQualityRoute(router, {
    path: route.path,
    query: routeState.query
  }, { history: 'push' })
}

const requestEditTask = async (task) => {
  const routeState = resolveCheckTaskRouteState({ task_id: task.id })
  await navigateQualityRoute(router, {
    path: route.path,
    query: routeState.query
  }, { history: 'push' })
}

const clearTaskDialogRoute = async () => {
  if (resolveCheckTaskRouteState(route.query).mode === 'list') return
  const routeState = resolveCheckTaskRouteState({})
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateQualityRoute(router, location, { history: 'replace' })
  }
}

const saveTask = async () => {
  if (!form.value.name.trim()) return ElMessage.warning(t('quality.checkTask.nameRequired'))
  if (!form.value.engine_id) return ElMessage.warning(t('quality.checkTask.engineRequired'))
  if (!form.value.schema_name.trim()) return ElMessage.warning(t('quality.checkTask.schemaRequired'))
  if (!form.value.table_name.trim()) return ElMessage.warning(t('quality.checkTask.tableRequired'))
  try {
    if (isEditing.value) {
      await checkTaskAPI.update(editingTaskID.value, {
        name: form.value.name,
        description: form.value.description,
        engine_id: form.value.engine_id,
        schema_name: form.value.schema_name,
        table_name: form.value.table_name,
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
  try {
    await ElMessageBox.confirm(t('quality.checkTask.deleteConfirm'), t('quality.checkTask.deleteTitle'), { type: 'warning' })
    await checkTaskAPI.delete(id)
    ElMessage.success(t('quality.checkTask.deleteSuccess'))
    await fetchTasks()
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    ElMessage.error(e.response?.data?.error || t('quality.checkTask.deleteFailed'))
  }
}

async function restoreTaskFromRoute() {
  const restoreSequence = ++routeRestoreSequence
  const routeState = resolveCheckTaskRouteState(route.query)
  if (routeState.changed) {
    await navigateQualityRoute(router, {
      path: route.path,
      query: routeState.query
    }, { history: 'replace' })
    return
  }
  if (!routeDataReady) return

  if (routeState.mode === 'edit') {
    try {
      const task = await checkTaskAPI.get(routeState.taskID)
      if (restoreSequence !== routeRestoreSequence) return
      editTask(task)
    } catch (error) {
      if (restoreSequence !== routeRestoreSequence) return
      showCreateDialog.value = false
      ElMessage.error(error.response?.data?.error || t('quality.checkTask.updateFailed'))
      await clearTaskDialogRoute()
    }
    return
  }
  if (routeState.mode === 'create') {
    editingTaskID.value = null
    form.value = defaultForm()
    showCreateDialog.value = true
    return
  }
  showCreateDialog.value = false
}

watch(() => route.query, restoreTaskFromRoute)

onMounted(async () => {
  await fetchEngines()
  await restoreTaskFromRoute()
  await fetchTasks()
  routeDataReady = true
  await restoreTaskFromRoute()
})

const fetchEngines = async () => {
  try {
    const res = await systemEngineAPI.list()
    engines.value = (res || []).filter(engine => engine.engine_type === 'postgresql' && engine.lifecycle_state === 'active')
  } catch {
    engines.value = []
  }
}
</script>

<style scoped>
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>
