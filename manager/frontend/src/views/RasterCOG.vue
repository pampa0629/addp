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
            <el-table-column :label="t('manager.rasterCOG.actions')" width="320" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.rasterCOG.execute') }}
                  </el-button>
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
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { InfoFilled, Refresh } from '@element-plus/icons-vue'
import { openMonitorExecution, parseLocatorSafe } from '@addp/common-frontend'
import { quickViewAPI } from '../api/quickView'
import { useQuickViewResourceDisplay } from '../composables/useQuickViewResourceDisplay'
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
const { displayText, engineName, loadQuickViewEngines } = useQuickViewResourceDisplay(t)

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
const selectedResultTask = ref(null)
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
    const list = unwrapList(response)
    results.value = list.items
    resultsTotal.value = list.total
  } catch (error) {
    console.error('加载 栅格快显 COG失败:', error)
    ElMessage.error(errorMessage(error, t('manager.rasterCOG.loadResultsFailed')))
  } finally {
    resultsLoading.value = false
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await quickViewAPI.executeRasterCOGTask(task.id)
    ElMessage.success(t('manager.rasterCOG.executeSubmitted'))
    await loadTasks()
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    console.error('执行 COG 生成任务失败:', error)
    ElMessage.error(errorMessage(error, t('manager.rasterCOG.executeFailed')))
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
  await router.replace({
    query: {
      ...route.query,
      tab: 'results',
      task_id: String(task.id)
    }
  })
  await loadResults()
}

const loadResultTaskFilterFromRoute = async () => {
  const taskId = Number(route.query.task_id || 0)
  if (!taskId || activeTab.value !== 'results') return
  resultFilters.task_id = taskId
  try {
    selectedResultTask.value = await quickViewAPI.getRasterCOGTask(taskId)
  } catch (error) {
    console.error('加载 COG 生成任务详情失败:', error)
    selectedResultTask.value = { id: taskId, name: t('manager.rasterCOG.taskWithId', { id: taskId }) }
  }
}

const handleTabChange = async () => {
  await router.replace({
    query: {
      ...route.query,
      tab: activeTab.value,
      task_id: activeTab.value === 'results' ? route.query.task_id : undefined
    }
  })
  if (activeTab.value === 'results') {
    await loadResults()
  } else {
    await loadTasks()
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
  await router.replace({ query: { ...route.query, task_id: undefined } })
  applyResultFilters()
}

const clearResultTaskFilter = async () => {
  selectedResultTask.value = null
  resultFilters.task_id = undefined
  await router.replace({ query: { ...route.query, task_id: undefined } })
  applyResultFilters()
}

const openTaskExecution = (task) => openMonitorExecution(task.last_execution_id)
const openResultExecution = (result) => openMonitorExecution(result.last_execution_id)

const openSourcePreview = (result) => {
  if (!result?.locator) return
  router.push({
    name: 'DataExplorer',
    query: { locator: result.locator }
  })
}

onMounted(async () => {
  await loadResultTaskFilterFromRoute()
  await Promise.all([
    loadQuickViewEngines(),
    activeTab.value === 'results' ? loadResults() : loadTasks()
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
