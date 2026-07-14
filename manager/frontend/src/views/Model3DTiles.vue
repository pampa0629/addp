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
            <el-table-column prop="name" :label="t('manager.model3DTiles.name')" min-width="190" show-overflow-tooltip />
            <el-table-column :label="t('manager.model3DTiles.sourceEngine')" width="110">
              <template #default="{ row }">{{ taskSource(row).source_engine_id || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ sourcePath(taskSource(row).item_locator) }}</template>
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
            <el-table-column :label="t('manager.model3DTiles.actions')" width="430" fixed="right">
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
              {{ selectedTask.name || t('manager.model3DTiles.taskWithId', { id: selectedTask.id }) }}
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
            <el-table-column :label="t('manager.model3DTiles.sourceEngine')" width="110">
              <template #default="{ row }">{{ row.source_engine_id || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ sourcePath(row.locator) }}</template>
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
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { InfoFilled, Refresh } from '@element-plus/icons-vue'
import { openMonitorExecution, parseLocatorSafe } from '@addp/common-frontend'
import { quickViewAPI } from '../api/quickView'
import { formatBytes, formatDateTime } from '../utils/formatters'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const activeTab = ref(route.query.tab === 'results' ? 'results' : 'tasks')
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
const resultStatuses = ['building', 'ready', 'failed', 'stale']
const resultFilters = reactive({ task_id: undefined, target_format: '', status: '', q: '' })

const unwrapPayload = (response) => response?.data?.data || response?.data || response || {}
const unwrapList = (response) => {
  const payload = unwrapPayload(response)
  const items = Array.isArray(payload?.data) ? payload.data : Array.isArray(payload?.items) ? payload.items : Array.isArray(payload) ? payload : []
  return { items, total: Number(payload?.total || items.length || 0) }
}
const taskSource = (task) => task?.source || task?.config?.source || {}
const taskTargetFormat = (task) => String(task?.target_format || task?.config?.target_format || '').trim()
const formatLabel = (value) => String(value || '').trim().toLowerCase() === 's3m' ? 'S3M' : '3D Tiles'
const sourcePath = (locator) => {
  const parsed = parseLocatorSafe(locator)
  return parsed?.path?.length ? parsed.path.join(' / ') : String(locator || '-').trim() || '-'
}
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
  resultsLoading.value = true
  try {
    const params = { page: resultsPage.value, page_size: resultsPageSize.value }
    if (resultFilters.task_id) params.task_id = resultFilters.task_id
    if (resultFilters.target_format) params.target_format = resultFilters.target_format
    if (resultFilters.status) params.status = resultFilters.status
    if (resultFilters.q) params.q = resultFilters.q
    const { items, total } = unwrapList(await quickViewAPI.listModel3DTilesResults(params))
    results.value = items
    resultsTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DTiles.loadResultsFailed')))
  } finally {
    resultsLoading.value = false
  }
}

const handleTabChange = async (tab) => {
  await router.replace({ query: { ...route.query, tab } })
  if (tab === 'tasks') await loadTasks()
  else await loadResults()
}
const handleTasksSizeChange = () => { tasksPage.value = 1; loadTasks() }
const handleResultsSizeChange = () => { resultsPage.value = 1; loadResults() }
const applyResultFilters = () => { resultsPage.value = 1; loadResults() }
const resetResultFilters = () => { resultFilters.target_format = ''; resultFilters.status = ''; resultFilters.q = ''; applyResultFilters() }
const openDataExplorer = () => router.push({ name: 'DataExplorer' })
const openSourcePreview = (locator) => { if (locator) router.push({ name: 'DataExplorer', query: { locator } }) }
const openExecution = (executionID) => openMonitorExecution(executionID)
const taskExecutionActive = (task) => ['pending', 'running'].includes(String(task?.last_execution_status || '').toLowerCase())
const taskDeleteDisabled = (task) => taskExecutionActive(task)
const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = unwrapPayload(await quickViewAPI.executeModel3DTilesTask(task.id))
    ElMessage.success(t('manager.model3DTiles.executeSubmitted'))
    await loadTasks()
    if (response?.execution_id) await openMonitorExecution(response.execution_id)
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DTiles.executeFailed')))
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
  await router.replace({ query: { ...route.query, tab: 'results', task_id: task.id } })
  await loadResults()
}
const clearTaskFilter = async () => {
  selectedTask.value = null
  resultFilters.task_id = undefined
  const query = { ...route.query }
  delete query.task_id
  await router.replace({ query })
  await loadResults()
}

onMounted(async () => {
  const taskID = Number(route.query.task_id || 0)
  if (taskID) {
    resultFilters.task_id = taskID
    try { selectedTask.value = unwrapPayload(await quickViewAPI.getModel3DTilesTask(taskID)) } catch { selectedTask.value = null }
  }
  await Promise.all([loadTasks(), loadResults()])
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
