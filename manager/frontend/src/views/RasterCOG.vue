<template>
  <div class="cog-results">
    <el-card>
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.rasterCOG.tasksTab')" name="tasks">
          <div class="tab-toolbar">
            <div class="toolbar-tip">
              <span class="toolbar-tip-text">{{ t('manager.rasterCOG.subtitle') }}</span>
              <el-tooltip
                :content="t('manager.rasterCOG.workflowDescription')"
                placement="bottom"
                :show-after="300"
              >
                <el-icon class="inline-tip-icon"><InfoFilled /></el-icon>
              </el-tooltip>
            </div>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column :label="t('manager.rasterCOG.name')" min-width="190" show-overflow-tooltip>
              <template #default="{ row }">{{ displayText(row.name) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.engine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(row.target?.source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.resource')" min-width="260" show-overflow-tooltip>
              <template #default="{ row }">{{ taskResourceText(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.profile')" width="110">
              <template #default="{ row }">{{ row.raster?.source_profile || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.sourceSize')" width="120">
              <template #default="{ row }">{{ formatBytes(row.raster?.source_size_bytes) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.rasterSize')" width="130">
              <template #default="{ row }">{{ rasterSize(row.raster?.width, row.raster?.height) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.enabled')" width="100">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.rasterCOG.enabledYes') : t('manager.rasterCOG.enabledNo') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.lastExecutionStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(lastExecutionStatus(row))">
                  {{ executionStatusLabel(lastExecutionStatus(row)) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.lastRunAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.last_run_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.actions')" width="380" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.rasterCOG.execute') }}
                  </el-button>
                  <el-button size="small" @click="requestTaskDetail(row)">{{ t('common.detail') }}</el-button>
                  <el-button size="small" @click="viewTaskResults(row)">{{ t('manager.rasterCOG.results') }}</el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                    {{ t('manager.rasterCOG.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteTask(row)">{{ t('manager.rasterCOG.delete') }}</el-button>
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

        <el-tab-pane :label="t('manager.rasterCOG.resultsTab')" name="results">
          <div class="filter-bar result-filter-bar">
            <div v-if="resultTaskFilterLabel" class="task-filter-chip">
              <el-tag type="primary" closable @close="clearResultTaskFilter">
                {{ resultTaskFilterLabel }}
              </el-tag>
            </div>
            <el-select
              v-model="resultFilters.status"
              class="result-status-filter"
              clearable
              :placeholder="t('manager.rasterCOG.resultStatus')"
            >
              <el-option
                v-for="status in resultStatuses"
                :key="status"
                :label="resultStatusLabel(status)"
                :value="status"
              />
            </el-select>
            <el-input
              v-model="resultFilters.q"
              class="result-keyword-filter"
              clearable
              :placeholder="t('manager.rasterCOG.keywordPlaceholder')"
              @keyup.enter="applyResultFilters"
            />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.rasterCOG.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.rasterCOG.reset') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadResults" />
          </div>

          <el-table :data="results" v-loading="resultsLoading" stripe>
            <el-table-column :label="t('manager.rasterCOG.engine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(row.source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.sourceDataPath')" min-width="260" show-overflow-tooltip>
              <template #default="{ row }">{{ sourcePathText(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.resultStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="resultStatusTagType(row.status)">
                  {{ resultStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.size')" width="120">
              <template #default="{ row }">{{ formatBytes(row.size_bytes) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.rasterSize')" width="130">
              <template #default="{ row }">{{ rasterSize(row.width, row.height) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.sourceSrid')" width="110">
              <template #default="{ row }">{{ row.source_srid || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.extent')" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">{{ extentLabel(row.extent) }}</template>
            </el-table-column>
            <el-table-column prop="error_message" :label="t('manager.rasterCOG.error')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.rasterCOG.updatedAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterCOG.actions')" width="280" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" :disabled="!row.locator" @click="openSourcePreview(row)">
                    {{ t('manager.rasterCOG.previewSource') }}
                  </el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openResultExecution(row)">
                    {{ t('manager.rasterCOG.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteResult(row)">{{ t('manager.rasterCOG.delete') }}</el-button>
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
        <el-descriptions-item :label="t('manager.rasterCOG.id')">{{ taskDetail.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.rasterCOG.engine')">{{ engineName(taskDetail.target?.source_engine_id) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.rasterCOG.resource')" :span="2">{{ taskResourceText(taskDetail) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.rasterCOG.profile')">{{ taskDetail.raster?.source_profile || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.rasterCOG.sourceSize')">{{ formatBytes(taskDetail.raster?.source_size_bytes) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.rasterCOG.rasterSize')">{{ rasterSize(taskDetail.raster?.width, taskDetail.raster?.height) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.rasterCOG.enabled')">
          <el-tag :type="taskDetail.enabled ? 'success' : 'info'">
            {{ taskDetail.enabled ? t('manager.rasterCOG.enabledYes') : t('manager.rasterCOG.enabledNo') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.rasterCOG.lastExecutionStatus')">
          <el-tag :type="executionStatusTagType(lastExecutionStatus(taskDetail))">
            {{ executionStatusLabel(lastExecutionStatus(taskDetail)) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.rasterCOG.lastRunAt')">{{ formatDateTime(taskDetail.last_run_at) }}</el-descriptions-item>
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
import { openMonitorExecution, parseLocatorSafe } from '@addp/common-frontend'
import { quickViewAPI } from '../api/quickView'
import { useQuickViewResourceDisplay } from '../composables/useQuickViewResourceDisplay'
import { useCurrentResultConfirmation } from '../composables/useCurrentResultConfirmation'
import { formatBytes, formatDateTime } from '../utils/formatters'
import {
  rasterCOGExecutionStatusTagType,
  rasterCOGExtentLabel,
  rasterCOGLastExecutionStatus,
  rasterCOGResultStatusTagType,
  rasterCOGSourcePath,
  rasterCOGTaskResource,
  rasterCOGRasterSize
} from '../utils/rasterCOGDisplay'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const executeWithCurrentResultConfirmation = useCurrentResultConfirmation()
const { displayText, engineName, loadQuickViewEngines } = useQuickViewResourceDisplay(t)

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
const selectedResultTask = ref(null)
const taskDetail = ref(null)
const taskDetailVisible = ref(false)
const executingId = ref(null)
const resultStatuses = ['building', 'ready', 'stale', 'failed', 'deleted']
const resultFilters = reactive({
  task_id: undefined,
  status: '',
  q: ''
})

const resultTaskFilterLabel = computed(() => {
  if (!selectedResultTask.value) return ''
  return selectedResultTask.value.name ? displayText(selectedResultTask.value.name) : t('manager.rasterCOG.taskWithId', { id: selectedResultTask.value.id })
})
const taskDetailTitle = computed(() => (
  taskDetail.value?.name
    ? displayText(taskDetail.value.name)
    : t('manager.rasterCOG.taskWithId', { id: taskDetail.value?.id || route.query.task_id })
))

const unwrapList = (response) => {
  const payload = response?.data?.data
    ? response.data
    : response?.data && (Array.isArray(response.data) || response.data.total !== undefined)
      ? response
      : response
  const items = Array.isArray(payload?.data)
    ? payload.data
    : Array.isArray(payload?.items)
      ? payload.items
      : Array.isArray(payload)
        ? payload
        : []
  return {
    items,
    total: Number(payload?.total || items.length || 0)
  }
}

const errorMessage = (error, fallback) => (
  error?.response?.data?.error ||
  error?.response?.data?.message ||
  error?.message ||
  fallback
)

const taskResourceText = (task) => rasterCOGTaskResource(task, parseLocatorSafe)
const sourcePathText = (result) => rasterCOGSourcePath(result, parseLocatorSafe)
const rasterSize = (width, height) => rasterCOGRasterSize(width, height)
const extentLabel = (extent) => rasterCOGExtentLabel(extent)
const lastExecutionStatus = (task) => rasterCOGLastExecutionStatus(task)
const executionStatusTagType = (status) => rasterCOGExecutionStatusTagType(status)
const resultStatusTagType = (status) => rasterCOGResultStatusTagType(status)

const executionStatusLabel = (status) => {
  const key = String(status || '').trim().toLowerCase()
  if (!key) return t('manager.rasterCOG.statusNeverRun')
  return t(`manager.rasterCOG.status.${key}`, key)
}

const resultStatusLabel = (status) => {
  const key = String(status || '').trim().toLowerCase()
  if (!key) return '-'
  return t(`manager.rasterCOG.resultStatuses.${key}`, key)
}

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const response = await quickViewAPI.listRasterCOGTasks({
      page: tasksPage.value,
      page_size: tasksPageSize.value
    })
    const list = unwrapList(response)
    tasks.value = list.items
    tasksTotal.value = list.total
  } catch (error) {
    console.error('加载 COG 生成任务失败:', error)
    ElMessage.error(errorMessage(error, t('manager.rasterCOG.loadTasksFailed')))
  } finally {
    tasksLoading.value = false
  }
}

const loadResults = async () => {
  const requestSequence = ++resultsRequestSequence
  resultsLoading.value = true
  try {
    const params = {
      page: resultsPage.value,
      page_size: resultsPageSize.value
    }
    if (resultFilters.task_id) params.task_id = resultFilters.task_id
    if (resultFilters.status) params.status = resultFilters.status
    if (resultFilters.q) params.q = resultFilters.q
    const response = await quickViewAPI.listRasterCOGs(params)
    if (requestSequence !== resultsRequestSequence) return
    const list = unwrapList(response)
    results.value = list.items
    resultsTotal.value = list.total
  } catch (error) {
    console.error('加载 栅格快显 COG失败:', error)
    ElMessage.error(errorMessage(error, t('manager.rasterCOG.loadResultsFailed')))
  } finally {
    if (requestSequence === resultsRequestSequence) {
      resultsLoading.value = false
    }
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await executeWithCurrentResultConfirmation(payload => quickViewAPI.executeRasterCOGTask(task.id, payload))
    ElMessage.success(t('manager.rasterCOG.executeSubmitted'))
    await loadTasks()
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      console.error('执行 COG 生成任务失败:', error)
      ElMessage.error(errorMessage(error, t('manager.rasterCOG.executeFailed')))
    }
  } finally {
    executingId.value = null
  }
}

const deleteTask = async (task) => {
  await ElMessageBox.confirm(t('manager.rasterCOG.deleteTaskConfirm'), t('manager.rasterCOG.delete'), { type: 'warning' })
  await quickViewAPI.deleteRasterCOGTask(task.id)
  ElMessage.success(t('manager.rasterCOG.deleteSuccess'))
  await loadTasks()
}

const deleteResult = async (result) => {
  await ElMessageBox.confirm(t('manager.rasterCOG.deleteResultConfirm'), t('manager.rasterCOG.delete'), { type: 'warning' })
  await quickViewAPI.deleteRasterCOG(result.id)
  ElMessage.success(t('manager.rasterCOG.deleteSuccess'))
  await loadResults()
}

const viewTaskResults = async (task) => {
  selectedResultTask.value = task
  resultFilters.task_id = task.id
  resultFilters.status = ''
  resultFilters.q = ''
  resultsPage.value = 1
  activeTab.value = 'results'
  await navigateManagerRoute(router, {
    query: {
      ...route.query,
      tab: 'results',
      task_id: String(task.id)
    }
  }, { history: 'replace' })
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
  const nextQuery = { ...route.query }
  delete nextQuery.task_id
  const routeState = resolveRouteState(nextQuery)
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateManagerRoute(router, location, { history: 'replace' })
  }
}

const loadResultTaskFilterFromRoute = async (taskId, restoreSequence) => {
  if (!taskId || activeTab.value !== 'results') return
  resultFilters.task_id = taskId
  try {
    const task = await quickViewAPI.getRasterCOGTask(taskId)
    if (restoreSequence !== workspaceRestoreSequence) return
    selectedResultTask.value = task
  } catch (error) {
    if (restoreSequence !== workspaceRestoreSequence) return
    console.error('加载 COG 生成任务详情失败:', error)
    selectedResultTask.value = { id: taskId, name: t('manager.rasterCOG.taskWithId', { id: taskId }) }
  }
}

const handleTabChange = async (tab) => {
  const routeState = resolveRouteState({ ...route.query, tab })
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateManagerRoute(router, location, { history: 'replace' })
  }
}

const handleTasksSizeChange = () => {
  tasksPage.value = 1
  loadTasks()
}

const handleResultsSizeChange = () => {
  resultsPage.value = 1
  loadResults()
}

const applyResultFilters = () => {
  resultsPage.value = 1
  loadResults()
}

const resetResultFilters = async () => {
  selectedResultTask.value = null
  Object.assign(resultFilters, { task_id: undefined, status: '', q: '' })
  await navigateManagerRoute(router, { query: { ...route.query, task_id: undefined } }, { history: 'replace' })
  applyResultFilters()
}

const clearResultTaskFilter = async () => {
  selectedResultTask.value = null
  resultFilters.task_id = undefined
  await navigateManagerRoute(router, { query: { ...route.query, task_id: undefined } }, { history: 'replace' })
  applyResultFilters()
}

const openTaskExecution = (task) => openMonitorExecution(task.last_execution_id)
const openResultExecution = (result) => openMonitorExecution(result.last_execution_id)

const openSourcePreview = (result) => {
  if (!result?.locator) return
  navigateManagerRoute(router, {
    name: 'DataExplorer',
    query: { locator: result.locator }
  })
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
    selectedResultTask.value = null
    const taskId = Number(routeState.query.task_id || 0) || undefined
    resultFilters.task_id = taskId
    await loadResultTaskFilterFromRoute(taskId, restoreSequence)
    if (restoreSequence !== workspaceRestoreSequence) return
    await loadResults()
    return
  }
  selectedResultTask.value = null
  resultFilters.task_id = undefined
  await loadTasks()
  if (restoreSequence !== workspaceRestoreSequence) return
  const taskId = Number(routeState.query.task_id || 0)
  if (!taskId) {
    taskDetailVisible.value = false
    taskDetail.value = null
    return
  }
  try {
    const detail = await quickViewAPI.getRasterCOGTask(taskId)
    if (restoreSequence !== workspaceRestoreSequence) return
    taskDetail.value = detail
    taskDetailVisible.value = true
  } catch (error) {
    if (restoreSequence !== workspaceRestoreSequence) return
    console.error('加载 COG 生成任务详情失败:', error)
    ElMessage.error(errorMessage(error, t('manager.rasterCOG.loadTasksFailed')))
    taskDetailVisible.value = false
    taskDetail.value = null
    await clearTaskDetailRoute()
  }
}

watch(() => route.query, restoreWorkspaceFromRoute)

onMounted(async () => {
  await restoreWorkspaceFromRoute()
  routeDataReady = true
  await Promise.all([
    loadQuickViewEngines(),
    restoreWorkspaceFromRoute()
  ])
})
</script>

<style scoped>
.cog-results {
  height: 100%;
}

.tab-toolbar,
.filter-bar {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.toolbar-tip {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 260px;
}

.toolbar-tip-text {
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.result-filter-bar {
  flex-wrap: nowrap;
  width: 100%;
}

.result-status-filter {
  flex: 0 0 180px;
}

.result-keyword-filter {
  flex: 1 1 auto;
  min-width: 0;
}

.inline-tip-icon {
  color: var(--el-color-info);
  cursor: help;
}

.task-filter-chip {
  display: flex;
  align-items: center;
  flex: 0 0 auto;
}

.result-filter-bar :deep(.el-button) {
  flex: 0 0 auto;
}

.row-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}
</style>
