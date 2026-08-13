<template>
  <div>
    <div class="page-header">
      <h2>{{ t('quality.checkTask.title') }}</h2>
      <el-button type="primary" :icon="Plus" @click="requestCreateDialog">{{ t('quality.checkTask.createTask') }}</el-button>
    </div>

    <el-alert v-if="loadError" :title="loadError" type="error" show-icon :closable="false" class="load-error" />
    <el-table :data="tasks" v-loading="loading" :empty-text="t('quality.checkTask.empty')" border>
      <el-table-column prop="id" :label="t('quality.checkTask.id')" width="80" />
      <el-table-column prop="name" :label="t('quality.checkTask.taskName')" />
      <el-table-column :label="t('quality.checkTask.engine')" min-width="180">
        <template #default="{ row }">
          <div class="engine-cell">
            <span>{{ engineName(row.engine_id) }}</span>
            <span class="engine-id">{{ t('quality.checkTask.engineIdValue', { id: row.engine_id }) }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column prop="schema_name" :label="t('quality.checkTask.schema')" width="120" />
      <el-table-column prop="table_name" :label="t('quality.checkTask.table')" width="150" />
      <el-table-column :label="t('quality.checkTask.lastExecution')" min-width="190">
        <template #default="{ row }">
          <div v-if="row.last_execution_id" class="execution-cell">
            <el-tag :type="executionStatusType(row.last_execution_status)" size="small">
              {{ executionStatusLabel(row.last_execution_status) }}
            </el-tag>
            <el-button link type="primary" @click="openExecution(row.last_execution_id)">
              {{ t('quality.checkTask.executionDetail') }}
            </el-button>
            <span class="execution-time">{{ row.last_run_at ? new Date(row.last_run_at).toLocaleString() : '-' }}</span>
          </div>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('quality.checkTask.actions')" width="260">
        <template #default="{ row }">
          <el-button size="small" :disabled="isTaskActive(row) || deletingTaskIds.has(row.id)" @click="requestEditTask(row)">{{ t('quality.checkTask.edit') }}</el-button>
          <el-button
            size="small"
            type="primary"
            :loading="runningTaskIds.has(row.id)"
            :disabled="isTaskActive(row) || runningTaskIds.has(row.id) || deletingTaskIds.has(row.id)"
            @click="runTask(row.id)"
          >{{ t('quality.checkTask.run') }}</el-button>
          <el-button
            size="small"
            type="danger"
            :loading="deletingTaskIds.has(row.id)"
            :disabled="isTaskActive(row) || deletingTaskIds.has(row.id) || runningTaskIds.has(row.id)"
            @click="deleteTask(row.id)"
          >{{ t('quality.checkTask.delete') }}</el-button>
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
      @size-change="changePageSize"
      @current-change="changePage"
    />

    <el-dialog
      v-model="showCreateDialog"
      class="addp-dialog"
      :title="dialogTitle"
      width="min(500px, calc(100vw - 24px))"
      :show-close="!saving"
      :close-on-click-modal="!saving"
      :close-on-press-escape="!saving"
      @closed="clearTaskDialogRoute"
    >
      <el-form :model="form" label-position="top">
        <el-form-item :label="t('quality.checkTask.name')"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('quality.checkTask.description')"><el-input v-model="form.description" type="textarea" /></el-form-item>
        <el-form-item :label="t('quality.checkTask.engineIdLabel')">
          <el-select v-model="form.engine_id" :placeholder="t('quality.checkTask.enginePlaceholder')" style="width:100%" @change="onEngineChange">
            <el-option
              v-for="engine in postgresEngines"
              :key="engine.id"
              :label="engineOptionLabel(engine)"
              :value="engine.id"
              :disabled="engine.lifecycle_state !== 'active'"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('quality.checkTask.schemaLabel')">
          <el-select
            v-model="form.schema_name"
            :loading="catalogLoading"
            :disabled="!form.engine_id || !isActiveEngine(form.engine_id)"
            :placeholder="t('quality.checkTask.schemaPlaceholder')"
            filterable
            style="width:100%"
            @change="onSchemaChange"
          >
            <el-option v-for="schema in schemaOptions" :key="schema.pathKey" :label="schema.name" :value="schema.name" :disabled="schema.unavailable" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('quality.checkTask.tableLabel')">
          <el-select
            v-model="form.table_name"
            :loading="catalogLoading"
            :disabled="!form.schema_name || !isActiveEngine(form.engine_id)"
            :placeholder="t('quality.checkTask.tablePlaceholder')"
            filterable
            style="width:100%"
            @change="onTableChange"
          >
            <el-option v-for="table in tableOptions" :key="table.pathKey" :label="table.name" :value="table.name" :disabled="table.unavailable" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="saving" @click="closeDialog">{{ t('quality.checkTask.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="saving" @click="saveTask">{{ t('quality.checkTask.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, onBeforeUnmount, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { checkTaskAPI, systemCatalogAPI, systemEngineAPI } from '../api/quality'
import { navigateQualityRoute } from '../utils/moduleNavigation'
import { buildCheckTaskRouteQuery, resolveCheckTaskRouteState } from '../utils/checkTaskRouteState'
import { executionDetailRoute } from '../utils/executionNavigation'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const tasks = ref([])
const loading = ref(false)
const loadError = ref('')
const showCreateDialog = ref(false)
const saving = ref(false)
const editingTaskID = ref(null)
let routeDataReady = false
let routeRestoreSequence = 0
const defaultForm = () => ({ name: '', description: '', engine_id: null, schema_name: '', table_name: '' })
const form = ref(defaultForm())
const postgresEngines = ref([])
const schemaOptions = ref([])
const tableOptions = ref([])
const catalogLoading = ref(false)
const catalogTargetAvailable = ref(false)
const runningTaskIds = ref(new Set())
const deletingTaskIds = ref(new Set())
const pagination = ref({ page: 1, page_size: 20, total: 0 })
const isEditing = computed(() => editingTaskID.value !== null)
const dialogTitle = computed(() => isEditing.value ? t('quality.checkTask.editTitle') : t('quality.checkTask.createTitle'))
let listRequestSequence = 0
let catalogRequestSequence = 0
let taskPollTimer = null
let taskPollStopped = false
const engineByID = computed(() => new Map(postgresEngines.value.map(engine => [engine.id, engine])))

const engineName = (id) => engineByID.value.get(id)?.name || t('quality.checkTask.engineUnknown')
const engineOptionLabel = (engine) => engine.lifecycle_state === 'active'
  ? engine.name
  : t('quality.checkTask.engineDisabledOption', { name: engine.name })
const isActiveEngine = (id) => engineByID.value.get(id)?.lifecycle_state === 'active'
const isTaskActive = (task) => task?.last_execution_status === 'pending' || task?.last_execution_status === 'running'
const executionStatusType = (status) => ({ success: 'success', failed: 'danger', running: 'warning', pending: 'info' }[status] || 'info')
const executionStatusLabel = (status) => ({
  pending: t('quality.execution.pending'),
  running: t('quality.execution.running'),
  success: t('quality.execution.success'),
  failed: t('quality.execution.failed'),
  timeout: t('quality.execution.timeout'),
  cancelled: t('quality.execution.cancelled')
}[status] || status || '-')
const openExecution = (executionID) => {
  const location = executionDetailRoute(executionID)
  if (location) return navigateQualityRoute(router, location, { history: 'push' })
}

const scheduleTaskRefresh = () => {
  if (taskPollTimer) window.clearTimeout(taskPollTimer)
  taskPollTimer = null
  if (!taskPollStopped && tasks.value.some(isTaskActive)) {
    taskPollTimer = window.setTimeout(fetchTasks, 2000)
  }
}

const fetchTasks = async () => {
  const requestSequence = ++listRequestSequence
  loading.value = true
  loadError.value = ''
  try {
    const res = await checkTaskAPI.list({ page: pagination.value.page, page_size: pagination.value.page_size })
    if (requestSequence !== listRequestSequence) return
    tasks.value = res?.data || []
    pagination.value.total = res?.total || 0
    scheduleTaskRefresh()
    const lastPage = Math.max(1, Math.ceil(pagination.value.total / pagination.value.page_size))
    if (pagination.value.page > lastPage) {
      pagination.value.page = lastPage
      await syncTaskRoute()
      return
    }
  } catch (e) {
    if (requestSequence !== listRequestSequence) return
    tasks.value = []
    pagination.value.total = 0
    scheduleTaskRefresh()
    loadError.value = e.response?.data?.error || t('quality.checkTask.loadFailed')
    ElMessage.error(loadError.value)
  } finally {
    if (requestSequence === listRequestSequence) loading.value = false
  }
}

const syncTaskRoute = () => {
  const currentState = resolveCheckTaskRouteState(route.query)
  return navigateQualityRoute(router, {
    path: route.path,
    query: buildCheckTaskRouteQuery({
      mode: currentState.mode,
      taskID: currentState.taskID,
      page: pagination.value.page,
      pageSize: pagination.value.page_size
    })
  }, { history: 'replace' })
}

const changePage = async (page) => {
  pagination.value.page = page
  await syncTaskRoute()
}

const changePageSize = async (pageSize) => {
  pagination.value.page_size = pageSize
  pagination.value.page = 1
  await syncTaskRoute()
}

const closeDialog = () => {
  catalogRequestSequence++
  showCreateDialog.value = false
  editingTaskID.value = null
  form.value = defaultForm()
  schemaOptions.value = []
  tableOptions.value = []
  catalogTargetAvailable.value = false
}

const catalogNodes = (response) => response?.nodes || response?.data?.nodes || []

const unavailableCatalogOption = (name, kind) => ({
  name,
  pathKey: `unavailable:${kind}:${name}`,
  unavailable: true
})

const loadCatalogSelection = async (selectedSchema = '', selectedTable = '') => {
  const engineID = form.value.engine_id
  const requestSequence = ++catalogRequestSequence
  schemaOptions.value = []
  tableOptions.value = []
  form.value.schema_name = ''
  form.value.table_name = ''
  catalogTargetAvailable.value = false
  if (!engineID || !isActiveEngine(engineID)) {
    if (selectedSchema) {
      schemaOptions.value = [unavailableCatalogOption(selectedSchema, 'schema')]
      form.value.schema_name = selectedSchema
    }
    if (selectedTable) {
      tableOptions.value = [unavailableCatalogOption(selectedTable, 'table')]
      form.value.table_name = selectedTable
    }
    return
  }
  catalogLoading.value = true
  try {
    const rootResponse = await systemCatalogAPI.listChildren(engineID, { segments: [] }, { limit: 100 })
    if (requestSequence !== catalogRequestSequence) return
    const root = catalogNodes(rootResponse).find(node => node.role === 'branch' && (node.path?.segments || []).length === 1)
    if (!root) throw new Error('PostgreSQL catalog root is unavailable')
    const schemaResponse = await systemCatalogAPI.listChildren(engineID, root.path, { limit: 1000 })
    if (requestSequence !== catalogRequestSequence) return
    schemaOptions.value = catalogNodes(schemaResponse)
      .filter(node => node.role === 'branch')
      .map(node => ({ ...node, pathKey: JSON.stringify(node.path) }))
    if (!selectedSchema) return
    const schema = schemaOptions.value.find(item => item.name === selectedSchema)
    if (!schema) {
      schemaOptions.value.push(unavailableCatalogOption(selectedSchema, 'schema'))
      form.value.schema_name = selectedSchema
      if (selectedTable) {
        tableOptions.value = [unavailableCatalogOption(selectedTable, 'table')]
        form.value.table_name = selectedTable
      }
      return
    }
    form.value.schema_name = selectedSchema
    const tableResponse = await systemCatalogAPI.listChildren(engineID, schema.path, { limit: 1000 })
    if (requestSequence !== catalogRequestSequence) return
    tableOptions.value = catalogNodes(tableResponse)
      .filter(node => node.role === 'leaf')
      .map(node => ({ ...node, pathKey: JSON.stringify(node.path) }))
    if (!selectedTable) return
    const table = tableOptions.value.find(item => item.name === selectedTable)
    if (!table) {
      tableOptions.value.push(unavailableCatalogOption(selectedTable, 'table'))
      form.value.table_name = selectedTable
      return
    }
    form.value.table_name = selectedTable
    catalogTargetAvailable.value = true
  } catch (error) {
    if (requestSequence === catalogRequestSequence) {
      if (selectedSchema) {
        schemaOptions.value = [unavailableCatalogOption(selectedSchema, 'schema')]
        form.value.schema_name = selectedSchema
      }
      if (selectedTable) {
        tableOptions.value = [unavailableCatalogOption(selectedTable, 'table')]
        form.value.table_name = selectedTable
      }
      ElMessage.error(error.response?.data?.error || t('quality.checkTask.catalogLoadFailed'))
    }
  } finally {
    if (requestSequence === catalogRequestSequence) catalogLoading.value = false
  }
}

const onEngineChange = () => loadCatalogSelection()

const onSchemaChange = async () => {
  const schema = schemaOptions.value.find(item => item.name === form.value.schema_name && !item.unavailable)
  const engineID = form.value.engine_id
  const requestSequence = ++catalogRequestSequence
  tableOptions.value = []
  form.value.table_name = ''
  catalogTargetAvailable.value = false
  if (!schema || !engineID) return
  catalogLoading.value = true
  try {
    const response = await systemCatalogAPI.listChildren(engineID, schema.path, { limit: 1000 })
    if (requestSequence !== catalogRequestSequence) return
    tableOptions.value = catalogNodes(response)
      .filter(node => node.role === 'leaf')
      .map(node => ({ ...node, pathKey: JSON.stringify(node.path) }))
  } catch (error) {
    if (requestSequence === catalogRequestSequence) {
      ElMessage.error(error.response?.data?.error || t('quality.checkTask.catalogLoadFailed'))
    }
  } finally {
    if (requestSequence === catalogRequestSequence) catalogLoading.value = false
  }
}

const onTableChange = () => {
  catalogTargetAvailable.value = tableOptions.value.some(item => item.name === form.value.table_name && !item.unavailable)
}

const editTask = async (task) => {
  editingTaskID.value = task.id
  form.value = {
    name: task.name || '',
    description: task.description || '',
    engine_id: task.engine_id || null,
    schema_name: task.schema_name || '',
    table_name: task.table_name || '',
  }
  showCreateDialog.value = true
  await loadCatalogSelection(task.schema_name || '', task.table_name || '')
}

const requestCreateDialog = async () => {
  await navigateQualityRoute(router, {
    path: route.path,
    query: buildCheckTaskRouteQuery({
      mode: 'create',
      taskID: '',
      page: pagination.value.page,
      pageSize: pagination.value.page_size
    })
  }, { history: 'push' })
}

const requestEditTask = async (task) => {
  await navigateQualityRoute(router, {
    path: route.path,
    query: buildCheckTaskRouteQuery({
      mode: 'edit',
      taskID: task.id,
      page: pagination.value.page,
      pageSize: pagination.value.page_size
    })
  }, { history: 'push' })
}

const clearTaskDialogRoute = async () => {
  if (resolveCheckTaskRouteState(route.query).mode === 'list') return
  const location = {
    path: route.path,
    query: buildCheckTaskRouteQuery({
      mode: 'list',
      taskID: '',
      page: pagination.value.page,
      pageSize: pagination.value.page_size
    })
  }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateQualityRoute(router, location, { history: 'replace' })
  }
}

const saveTask = async () => {
  if (saving.value) return
  if (!form.value.name.trim()) return ElMessage.warning(t('quality.checkTask.nameRequired'))
  if (!form.value.engine_id) return ElMessage.warning(t('quality.checkTask.engineRequired'))
  if (!isActiveEngine(form.value.engine_id)) return ElMessage.warning(t('quality.checkTask.engineUnavailable'))
  if (!form.value.schema_name.trim()) return ElMessage.warning(t('quality.checkTask.schemaRequired'))
  if (!form.value.table_name.trim()) return ElMessage.warning(t('quality.checkTask.tableRequired'))
  if (!catalogTargetAvailable.value) return ElMessage.warning(t('quality.checkTask.targetUnavailable'))
  saving.value = true
  try {
    if (isEditing.value) {
      await checkTaskAPI.update(editingTaskID.value, {
        name: form.value.name,
        description: form.value.description,
        engine_id: form.value.engine_id,
        schema_name: form.value.schema_name,
        table_name: form.value.table_name
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
  } finally {
    saving.value = false
  }
}

const runTask = async (id) => {
  if (runningTaskIds.value.has(id)) return
  runningTaskIds.value.add(id)
  try {
    const res = await checkTaskAPI.run(id)
    ElMessage.success(t('quality.checkTask.runSuccess', { id: res.execution_id }))
    await fetchTasks()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('quality.checkTask.runFailed'))
  } finally {
    runningTaskIds.value.delete(id)
  }
}

const deleteTask = async (id) => {
  if (deletingTaskIds.value.has(id)) return
  deletingTaskIds.value.add(id)
  try {
    await ElMessageBox.confirm(t('quality.checkTask.deleteConfirm'), t('quality.checkTask.deleteTitle'), {
      type: 'warning',
      customClass: 'addp-message-box',
      confirmButtonText: t('quality.checkTask.confirm'),
      cancelButtonText: t('quality.checkTask.cancel'),
      confirmButtonClass: 'el-button--danger'
    })
    await checkTaskAPI.delete(id)
    ElMessage.success(t('quality.checkTask.deleteSuccess'))
    await fetchTasks()
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    ElMessage.error(e.response?.data?.error || t('quality.checkTask.deleteFailed'))
  } finally {
    deletingTaskIds.value.delete(id)
  }
}

async function restoreTaskFromRoute() {
  const restoreSequence = ++routeRestoreSequence
  const routeState = resolveCheckTaskRouteState(route.query)
  const pageChanged = pagination.value.page !== routeState.page || pagination.value.page_size !== routeState.pageSize
  pagination.value.page = routeState.page
  pagination.value.page_size = routeState.pageSize
  if (routeState.changed) {
    await navigateQualityRoute(router, {
      path: route.path,
      query: routeState.query
    }, { history: 'replace' })
    return
  }
  if (!routeDataReady) return
  if (pageChanged) await fetchTasks()

  if (routeState.mode === 'edit') {
    try {
      const task = await checkTaskAPI.get(routeState.taskID)
      if (restoreSequence !== routeRestoreSequence) return
      await editTask(task)
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
    schemaOptions.value = []
    tableOptions.value = []
    catalogTargetAvailable.value = false
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

onBeforeUnmount(() => {
  taskPollStopped = true
  if (taskPollTimer) window.clearTimeout(taskPollTimer)
})

const fetchEngines = async () => {
  try {
    const res = await systemEngineAPI.list({
      engine_type: 'postgresql',
      lifecycle_states: 'active,disabled'
    })
    postgresEngines.value = (res || []).filter(engine => engine.engine_type === 'postgresql')
  } catch {
    postgresEngines.value = []
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
.load-error {
  margin-bottom: 16px;
}
.engine-cell {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.engine-cell > span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.engine-id {
  color: var(--addp-text-secondary);
  font-size: 12px;
}
.execution-cell {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
}
.execution-time {
  flex-basis: 100%;
  color: var(--addp-text-secondary);
  font-size: 12px;
}
</style>
