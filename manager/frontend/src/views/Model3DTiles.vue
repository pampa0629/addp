<template>
  <div class="model3d-tiles-management">
    <el-card>
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.model3DTiles.tasksTab')" name="tasks">
          <div class="toolbar">
            <div class="toolbar-tip">
              <span>{{ t('manager.model3DTiles.subtitle') }}</span>
              <el-tooltip :content="t('manager.model3DTiles.workflowDescription')" placement="bottom" :show-after="300">
                <el-icon><InfoFilled /></el-icon>
              </el-tooltip>
            </div>
            <el-button type="primary" @click="openDataExplorer">{{ t('manager.model3DTiles.goGenerate') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column :label="t('manager.model3DTiles.name')" min-width="190" show-overflow-tooltip>
              <template #default="{ row }">{{ displayText(row.name) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.sourceEngine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(taskSource(row).source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ resourcePath(taskSource(row).item_locator) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.targetFormat')" width="120">
              <template #default="{ row }"><el-tag>{{ formatLabel(taskTargetFormat(row)) }}</el-tag></template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.sourceSize')" width="120">
              <template #default="{ row }">{{ formatBytes(taskSource(row).source_size_bytes) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.enabled')" width="90">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? t('manager.model3DTiles.yes') : t('manager.model3DTiles.no') }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.lastExecutionStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(row.last_execution_status)">{{ executionStatusLabel(row.last_execution_status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.lastRunAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.last_run_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.actions')" width="490" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button
                    type="primary"
                    size="small"
                    :loading="executingId === row.id"
                    :disabled="taskExecutionActive(row)"
                    @click="executeTask(row)"
                  >
                    {{ t('manager.model3DTiles.generate') }}
                  </el-button>
                  <el-button size="small" @click="requestTaskDetail(row)">{{ t('common.detail') }}</el-button>
                  <el-button size="small" @click="openSourcePreview(taskSource(row).item_locator)">{{ t('manager.model3DTiles.preview') }}</el-button>
                  <el-button size="small" @click="viewTaskResults(row)">{{ t('manager.model3DTiles.results') }}</el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openExecution(row.last_execution_id)">{{ t('manager.model3DTiles.monitor') }}</el-button>
                  <el-button size="small" type="danger" :disabled="taskDeleteDisabled(row)" @click="deleteTask(row)">{{ t('manager.model3DTiles.delete') }}</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>

          <el-pagination
            v-model:current-page="tasksPage"
            v-model:page-size="tasksPageSize"
            :total="tasksTotal"
            :page-sizes="[10, 20, 50, 100]"
            layout="total, sizes, prev, pager, next, jumper"
            class="pagination"
            @size-change="handleTasksSizeChange"
            @current-change="loadTasks"
          />
        </el-tab-pane>

        <el-tab-pane :label="t('manager.model3DTiles.resultsTab')" name="results">
          <div class="filter-bar">
            <el-tag v-if="selectedTask" type="primary" closable @close="clearTaskFilter">
              {{ selectedTask.name ? displayText(selectedTask.name) : t('manager.model3DTiles.taskWithId', { id: selectedTask.id }) }}
            </el-tag>
            <el-select v-model="resultFilters.target_format" clearable class="format-filter" :placeholder="t('manager.model3DTiles.targetFormat')">
              <el-option label="3D Tiles" value="3d_tiles" />
              <el-option label="S3M" value="s3m" />
            </el-select>
            <el-select v-model="resultFilters.status" clearable class="status-filter" :placeholder="t('manager.model3DTiles.resultStatus')">
              <el-option v-for="status in resultStatuses" :key="status" :label="resultStatusLabel(status)" :value="status" />
            </el-select>
            <el-input v-model="resultFilters.q" clearable class="keyword-filter" :placeholder="t('manager.model3DTiles.keywordPlaceholder')" @keyup.enter="applyResultFilters" />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.model3DTiles.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.model3DTiles.reset') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadResults" />
          </div>

          <el-table :data="results" v-loading="resultsLoading" stripe>
            <el-table-column :label="t('manager.model3DTiles.sourceEngine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(row.source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ resourcePath(row.locator) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.targetFormat')" width="120">
              <template #default="{ row }"><el-tag>{{ formatLabel(row.target_format) }}</el-tag></template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.resultStatus')" width="120">
              <template #default="{ row }"><el-tag :type="resultStatusTagType(row.status)">{{ resultStatusLabel(row.status) }}</el-tag></template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.size')" width="120">
              <template #default="{ row }">{{ formatBytes(row.size_bytes) }}</template>
            </el-table-column>
            <el-table-column prop="error_message" :label="t('manager.model3DTiles.error')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.model3DTiles.updatedAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.actions')" width="290" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" :disabled="row.status !== 'ready' || !row.locator" @click="openSourcePreview(row.locator)">{{ t('manager.model3DTiles.preview') }}</el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openExecution(row.last_execution_id)">{{ t('manager.model3DTiles.monitor') }}</el-button>
                  <el-button size="small" type="danger" :disabled="row.status === 'building'" @click="deleteResult(row)">{{ t('manager.model3DTiles.delete') }}</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>

          <el-pagination
            v-model:current-page="resultsPage"
            v-model:page-size="resultsPageSize"
            :total="resultsTotal"
            :page-sizes="[10, 20, 50, 100]"
            layout="total, sizes, prev, pager, next, jumper"
            class="pagination"
            @size-change="handleResultsSizeChange"
            @current-change="loadResults"
          />
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <el-dialog
      v-model="taskDetailVisible"
      :title="taskDetailTitle"
      width="720px"
      destroy-on-close
      @closed="clearTaskDetailRoute"
    >
      <el-descriptions v-if="taskDetail" :column="2" border>
        <el-descriptions-item :label="t('manager.model3DTiles.id')">{{ taskDetail.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.model3DTiles.sourceEngine')">{{ engineName(taskSource(taskDetail).source_engine_id) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.model3DTiles.source')" :span="2">{{ resourcePath(taskSource(taskDetail).item_locator) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.model3DTiles.targetFormat')">{{ formatLabel(taskTargetFormat(taskDetail)) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.model3DTiles.sourceSize')">{{ formatBytes(taskSource(taskDetail).source_size_bytes) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.model3DTiles.enabled')">
          <el-tag :type="taskDetail.enabled ? 'success' : 'info'">{{ taskDetail.enabled ? t('manager.model3DTiles.yes') : t('manager.model3DTiles.no') }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.model3DTiles.lastExecutionStatus')">
          <el-tag :type="executionStatusTagType(taskDetail.last_execution_status)">{{ executionStatusLabel(taskDetail.last_execution_status) }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.model3DTiles.lastRunAt')">{{ formatDateTime(taskDetail.last_run_at) }}</el-descriptions-item>
        <el-descriptions-item :label="t('common.createdAt')">{{ formatDateTime(taskDetail.created_at) }}</el-descriptions-item>
        <el-descriptions-item :label="t('common.updatedAt')">{{ formatDateTime(taskDetail.updated_at) }}</el-descriptions-item>
      </el-descriptions>
      <template #footer>
        <el-button @click="taskDetailVisible = false">{{ t('common.close') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { navigateManagerRoute } from '@/utils/moduleNavigation'
import { resolveManagerTaskWorkspaceRouteState } from '@/utils/taskWorkspaceRoute'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { InfoFilled, Refresh } from '@element-plus/icons-vue'
import { openMonitorExecution } from '@addp/common-frontend'
import { quickViewAPI } from '../api/quickView'
import { useQuickViewResourceDisplay } from '../composables/useQuickViewResourceDisplay'
import { useCurrentResultConfirmation } from '../composables/useCurrentResultConfirmation'
import { formatBytes, formatDateTime } from '../utils/formatters'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const executeWithCurrentResultConfirmation = useCurrentResultConfirmation()
const { displayText, engineName, loadQuickViewEngines, resourcePath } = useQuickViewResourceDisplay(t)
const resolveRouteState = routeQuery => resolveManagerTaskWorkspaceRouteState({
  routeQuery,
  allowedQueryByTab: {
    tasks: ['task_id'],
    results: ['task_id']
  }
})
const activeTab = ref(resolveRouteState(route.query).tab)
let routeDataReady = false
let workspaceRestoreSequence = 0
let resultsRequestSequence = 0
const tasks = ref([])
const tasksLoading = ref(false)
const tasksPage = ref(1)
const tasksPageSize = ref(20)
const tasksTotal = ref(0)
const results = ref([])
const resultsLoading = ref(false)
const resultsPage = ref(1)
const resultsPageSize = ref(20)
const resultsTotal = ref(0)
const executingId = ref(null)
const selectedTask = ref(null)
const taskDetail = ref(null)
const taskDetailVisible = ref(false)
const resultStatuses = ['building', 'ready', 'failed', 'stale']
const resultFilters = reactive({ task_id: undefined, target_format: '', status: '', q: '' })
const taskDetailTitle = computed(() => (
  taskDetail.value?.name
    ? displayText(taskDetail.value.name)
    : t('manager.model3DTiles.taskWithId', { id: taskDetail.value?.id || route.query.task_id })
))

const unwrapPayload = (response) => response?.data?.data || response?.data || response || {}
const unwrapList = (response) => {
  const payload = unwrapPayload(response)
  const items = Array.isArray(payload?.data) ? payload.data : Array.isArray(payload?.items) ? payload.items : Array.isArray(payload) ? payload : []
  return { items, total: Number(payload?.total || items.length || 0) }
}
const taskSource = (task) => task?.source || task?.config?.source || {}
const taskTargetFormat = (task) => String(task?.target_format || task?.config?.target_format || '').trim()
const formatLabel = (value) => String(value || '').trim().toLowerCase() === 's3m' ? 'S3M' : '3D Tiles'
const errorMessage = (error, fallback) => error?.response?.data?.error || error?.message || fallback
const executionStatusTagType = (status) => {
  const value = String(status || '').toLowerCase()
  if (value === 'success') return 'success'
  if (['failed', 'timeout', 'cancelled', 'canceled'].includes(value)) return 'danger'
  if (['pending', 'running'].includes(value)) return 'warning'
  return 'info'
}
const executionStatusLabel = (status) => {
  const value = String(status || '').trim().toLowerCase()
  return value ? t(`manager.model3DTiles.status.${value}`, value) : t('manager.model3DTiles.statusNeverRun')
}
const resultStatusTagType = (status) => status === 'ready' ? 'success' : status === 'failed' ? 'danger' : status === 'building' ? 'warning' : 'info'
const resultStatusLabel = (status) => t(`manager.model3DTiles.resultStatuses.${String(status || '').toLowerCase()}`, status || '-')

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const { items, total } = unwrapList(await quickViewAPI.listModel3DTilesTasks({ page: tasksPage.value, page_size: tasksPageSize.value }))
    tasks.value = items
    tasksTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DTiles.loadTasksFailed')))
  } finally {
    tasksLoading.value = false
  }
}

const loadResults = async () => {
  const requestSequence = ++resultsRequestSequence
  resultsLoading.value = true
  try {
    const params = { page: resultsPage.value, page_size: resultsPageSize.value }
    if (resultFilters.task_id) params.task_id = resultFilters.task_id
    if (resultFilters.target_format) params.target_format = resultFilters.target_format
    if (resultFilters.status) params.status = resultFilters.status
    if (resultFilters.q) params.q = resultFilters.q
    const { items, total } = unwrapList(await quickViewAPI.listModel3DTilesResults(params))
    if (requestSequence !== resultsRequestSequence) return
    results.value = items
    resultsTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DTiles.loadResultsFailed')))
  } finally {
    if (requestSequence === resultsRequestSequence) {
      resultsLoading.value = false
    }
  }
}

const handleTabChange = async (tab) => {
  const routeState = resolveRouteState({ ...route.query, tab })
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateManagerRoute(router, location, { history: 'replace' })
  }
}
const handleTasksSizeChange = () => { tasksPage.value = 1; loadTasks() }
const handleResultsSizeChange = () => { resultsPage.value = 1; loadResults() }
const applyResultFilters = () => { resultsPage.value = 1; loadResults() }
const resetResultFilters = () => { resultFilters.target_format = ''; resultFilters.status = ''; resultFilters.q = ''; applyResultFilters() }
const openDataExplorer = () => navigateManagerRoute(router, { name: 'DataExplorer' })
const openSourcePreview = (locator) => { if (locator) navigateManagerRoute(router, { name: 'DataExplorer', query: { locator } }) }
const openExecution = (executionID) => openMonitorExecution(executionID)
const taskExecutionActive = (task) => ['pending', 'running'].includes(String(task?.last_execution_status || '').toLowerCase())
const taskDeleteDisabled = (task) => taskExecutionActive(task)
const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = unwrapPayload(await executeWithCurrentResultConfirmation(payload => quickViewAPI.executeModel3DTilesTask(task.id, payload)))
    ElMessage.success(t('manager.model3DTiles.executeSubmitted'))
    await loadTasks()
    if (response?.execution_id) await openMonitorExecution(response.execution_id)
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(errorMessage(error, t('manager.model3DTiles.executeFailed')))
  } finally {
    executingId.value = null
  }
}
const deleteTask = async (task) => {
  try {
    await ElMessageBox.confirm(t('manager.model3DTiles.deleteTaskConfirm'), t('manager.model3DTiles.delete'), { type: 'warning' })
    await quickViewAPI.deleteModel3DTilesTask(task.id)
    if (selectedTask.value?.id === task.id) await clearTaskFilter()
    await loadTasks()
    ElMessage.success(t('manager.model3DTiles.deleteSuccess'))
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(errorMessage(error, t('manager.model3DTiles.deleteFailed')))
  }
}
const deleteResult = async (result) => {
  try {
    await ElMessageBox.confirm(t('manager.model3DTiles.deleteResultConfirm'), t('manager.model3DTiles.delete'), { type: 'warning' })
    await quickViewAPI.deleteModel3DTilesResult(result.id)
    await loadResults()
    ElMessage.success(t('manager.model3DTiles.deleteSuccess'))
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(errorMessage(error, t('manager.model3DTiles.deleteFailed')))
  }
}
const viewTaskResults = async (task) => {
  selectedTask.value = task
  resultFilters.task_id = task.id
  resultsPage.value = 1
  activeTab.value = 'results'
  await navigateManagerRoute(router, { query: { ...route.query, tab: 'results', task_id: task.id } }, { history: 'replace' })
}
const requestTaskDetail = async (task) => {
  const routeState = resolveRouteState({ task_id: task.id })
  await navigateManagerRoute(router, {
    path: route.path,
    query: routeState.query
  }, { history: 'push' })
}
const clearTaskDetailRoute = async () => {
  if (resolveRouteState(route.query).tab !== 'tasks') return
  const query = { ...route.query }
  delete query.task_id
  const routeState = resolveRouteState(query)
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateManagerRoute(router, location, { history: 'replace' })
  }
}
const clearTaskFilter = async () => {
  selectedTask.value = null
  resultFilters.task_id = undefined
  const query = { ...route.query }
  delete query.task_id
  await navigateManagerRoute(router, { query }, { history: 'replace' })
}

async function restoreWorkspaceFromRoute() {
  const restoreSequence = ++workspaceRestoreSequence
  const routeState = resolveRouteState(route.query)
  activeTab.value = routeState.tab
  if (routeState.changed) {
    await navigateManagerRoute(router, {
      path: route.path,
      query: routeState.query
    }, { history: 'replace' })
    return
  }
  if (!routeDataReady) return
  if (routeState.tab === 'results') {
    taskDetailVisible.value = false
    taskDetail.value = null
    const taskID = Number(routeState.query.task_id || 0)
    resultFilters.task_id = taskID || undefined
    selectedTask.value = null
    if (taskID) {
      try {
        const task = unwrapPayload(await quickViewAPI.getModel3DTilesTask(taskID))
        if (restoreSequence !== workspaceRestoreSequence) return
        selectedTask.value = task
      } catch {
        if (restoreSequence !== workspaceRestoreSequence) return
        selectedTask.value = null
      }
    }
    if (restoreSequence !== workspaceRestoreSequence) return
    await loadResults()
    return
  }
  await loadTasks()
  if (restoreSequence !== workspaceRestoreSequence) return
  selectedTask.value = null
  resultFilters.task_id = undefined
  const taskID = Number(routeState.query.task_id || 0)
  if (!taskID) {
    taskDetailVisible.value = false
    taskDetail.value = null
    return
  }
  try {
    const detail = unwrapPayload(await quickViewAPI.getModel3DTilesTask(taskID))
    if (restoreSequence !== workspaceRestoreSequence) return
    taskDetail.value = detail
    taskDetailVisible.value = true
  } catch (error) {
    if (restoreSequence !== workspaceRestoreSequence) return
    ElMessage.error(errorMessage(error, t('manager.model3DTiles.loadTasksFailed')))
    taskDetailVisible.value = false
    taskDetail.value = null
    await clearTaskDetailRoute()
  }
}

watch(() => route.query, restoreWorkspaceFromRoute)

onMounted(async () => {
  await restoreWorkspaceFromRoute()
  routeDataReady = true
  await Promise.all([loadQuickViewEngines(), restoreWorkspaceFromRoute()])
})
</script>

<style scoped>
.model3d-tiles-management { height: 100%; }
.toolbar,
.filter-bar { display: flex; align-items: center; gap: 12px; margin-bottom: 16px; flex-wrap: wrap; }
.toolbar-tip { display: flex; align-items: center; gap: 6px; flex: 1; min-width: 280px; color: var(--addp-text-secondary); font-size: 13px; }
.row-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.pagination { margin-top: 16px; justify-content: flex-end; }
.format-filter,
.status-filter { width: 150px; }
.keyword-filter { width: 280px; }
@media (max-width: 760px) {
  .format-filter,
  .status-filter,
  .keyword-filter { width: 100%; }
}
</style>
