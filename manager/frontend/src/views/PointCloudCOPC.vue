<template>
  <div class="point-cloud-copc">
    <el-card>
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.pointCloudCOPC.tasksTab')" name="tasks">
          <div class="tab-toolbar">
            <div class="toolbar-tip">
              <span class="toolbar-tip-text">{{ t('manager.pointCloudCOPC.subtitle') }}</span>
              <el-tooltip :content="t('manager.pointCloudCOPC.workflowDescription')" placement="bottom" :show-after="300">
                <el-icon class="inline-tip-icon"><InfoFilled /></el-icon>
              </el-tooltip>
            </div>
            <el-button type="primary" @click="requestCreateDialog">{{ t('manager.pointCloudCOPC.create') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column :label="t('manager.pointCloudCOPC.name')" min-width="180" show-overflow-tooltip>
              <template #default="{ row }">{{ displayText(row.name) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.engine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(source(row).source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ resourcePath(source(row).item_locator) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.sourceFormat')" width="110">
              <template #default="{ row }">{{ formatLabel(source(row).format) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.sourceSize')" width="120">
              <template #default="{ row }">{{ formatBytes(source(row).source_size_bytes) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.enabled')" width="90">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.pointCloudCOPC.enabledYes') : t('manager.pointCloudCOPC.enabledNo') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.lastExecutionStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(lastExecutionStatus(row))">
                  {{ executionStatusLabel(lastExecutionStatus(row)) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.lastRunAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.last_run_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.actions')" width="360" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.pointCloudCOPC.execute') }}
                  </el-button>
                  <el-button size="small" @click="viewTaskResults(row)">{{ t('manager.pointCloudCOPC.results') }}</el-button>
                  <el-button size="small" @click="requestEditTask(row)">{{ t('manager.pointCloudCOPC.edit') }}</el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                    {{ t('manager.pointCloudCOPC.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteTask(row)">{{ t('manager.pointCloudCOPC.delete') }}</el-button>
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

        <el-tab-pane :label="t('manager.pointCloudCOPC.resultsTab')" name="results">
          <div class="filter-bar">
            <el-tag v-if="resultTaskFilterLabel" type="primary" closable @close="clearResultTaskFilter">
              {{ resultTaskFilterLabel }}
            </el-tag>
            <el-select v-model="resultFilters.status" class="status-filter" clearable :placeholder="t('manager.pointCloudCOPC.resultStatus')">
              <el-option v-for="status in resultStatuses" :key="status" :label="resultStatusLabel(status)" :value="status" />
            </el-select>
            <el-input
              v-model="resultFilters.q"
              class="keyword-filter"
              clearable
              :placeholder="t('manager.pointCloudCOPC.keywordPlaceholder')"
              @keyup.enter="applyResultFilters"
            />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.pointCloudCOPC.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.pointCloudCOPC.reset') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadResults" />
          </div>

          <el-table :data="results" v-loading="resultsLoading" stripe>
            <el-table-column :label="t('manager.pointCloudCOPC.engine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(row.source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ resourcePath(row.locator) }}</template>
            </el-table-column>
            <el-table-column prop="file_name" :label="t('manager.pointCloudCOPC.fileName')" min-width="170" show-overflow-tooltip />
            <el-table-column :label="t('manager.pointCloudCOPC.resultStatus')" width="120">
              <template #default="{ row }">
                <el-tag :type="resultStatusTagType(row.status)">
                  {{ resultStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.workflowOperator')" width="150">
              <template #default="{ row }">{{ diagnosticValue(workflowRuntime(row).operator) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.size')" width="120">
              <template #default="{ row }">{{ formatBytes(row.size_bytes) }}</template>
            </el-table-column>
            <el-table-column prop="error_message" :label="t('manager.pointCloudCOPC.error')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.pointCloudCOPC.updatedAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.pointCloudCOPC.actions')" width="330" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" :disabled="row.status !== 'ready' || !row.locator" @click="openSourcePreview(row)">
                    {{ t('manager.pointCloudCOPC.previewCOPC') }}
                  </el-button>
                  <el-button size="small" :disabled="!hasDiagnostics(row)" @click="openDiagnostics(row)">
                    {{ t('manager.pointCloudCOPC.diagnostics') }}
                  </el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openResultExecution(row)">
                    {{ t('manager.pointCloudCOPC.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteResult(row)">{{ t('manager.pointCloudCOPC.delete') }}</el-button>
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
      v-model="taskDialogVisible"
      :title="editingTask ? t('manager.pointCloudCOPC.editTitle') : t('manager.pointCloudCOPC.createTitle')"
      width="820px"
      destroy-on-close
      @closed="clearDialogQuery"
    >
      <el-form label-position="top" :model="form">
        <div class="form-grid">
          <el-form-item :label="t('manager.pointCloudCOPC.name')">
            <el-input v-model="form.name" :placeholder="t('manager.pointCloudCOPC.namePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('manager.pointCloudCOPC.enabled')">
            <el-switch
              v-model="form.enabled"
              :active-text="t('manager.pointCloudCOPC.enabledYes')"
              :inactive-text="t('manager.pointCloudCOPC.enabledNo')"
            />
          </el-form-item>
        </div>

        <el-divider content-position="left">{{ t('manager.pointCloudCOPC.sourceScope') }}</el-divider>
        <ResourceTreePicker
          v-model="sourceSelection"
          mode="item"
          :initial-locator="sourceInitialLocator"
          :title="t('manager.pointCloudCOPC.sourceTreeTitle')"
          :engine-label="t('manager.pointCloudCOPC.engine')"
          :engine-placeholder="t('manager.pointCloudCOPC.enginePlaceholder')"
          :search-placeholder="t('manager.pointCloudCOPC.searchPlaceholder')"
          :search-all-engines-placeholder="t('manager.pointCloudCOPC.searchAllEnginesPlaceholder')"
          :search-empty-text="t('manager.pointCloudCOPC.searchEmptyText')"
          tree-height="300px"
        />

        <el-descriptions class="source-facts" :column="2" border size="small">
          <el-descriptions-item :label="t('manager.pointCloudCOPC.source')">
            {{ sourceFactText(form.source.item_locator) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.pointCloudCOPC.engine')">
            {{ engineName(form.source.source_engine_id) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.pointCloudCOPC.itemFingerprint')">
            {{ form.source.item_fingerprint || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.pointCloudCOPC.sourceFormat')">
            {{ formatLabel(form.source.format) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.pointCloudCOPC.sourceSize')">
            {{ formatBytes(form.source.source_size_bytes) }}
          </el-descriptions-item>
        </el-descriptions>

        <el-form-item :label="t('manager.pointCloudCOPC.fileName')">
          <el-input v-model="form.result.file_name" :placeholder="t('manager.pointCloudCOPC.fileNamePlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="taskDialogVisible = false">{{ t('manager.pointCloudCOPC.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTask">{{ t('manager.pointCloudCOPC.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="diagnosticsDialogVisible" :title="t('manager.pointCloudCOPC.diagnosticsTitle')" width="760px" destroy-on-close>
      <template v-if="selectedDiagnosticResult">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item :label="t('manager.pointCloudCOPC.sourceFormat')">
            {{ diagnosticValue(selectedDiagnosticResult.source_format || sourceFacts(selectedDiagnosticResult).format) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.pointCloudCOPC.workflowOperator')">
            {{ diagnosticValue(workflowRuntime(selectedDiagnosticResult).operator) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.pointCloudCOPC.workflowExecutionTime')">
            {{ diagnosticExecutionTime(selectedDiagnosticResult) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.pointCloudCOPC.copcRef')">
            {{ diagnosticValue(artifactFacts(selectedDiagnosticResult).object || selectedDiagnosticResult.storage_ref) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.pointCloudCOPC.copcSize')">
            {{ formatBytes(selectedDiagnosticResult.size_bytes) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.pointCloudCOPC.converter')">
            {{ diagnosticValue(workflowRuntime(selectedDiagnosticResult).converter) }}
          </el-descriptions-item>
        </el-descriptions>
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
import { ResourceTreePicker, openMonitorExecution, parseLocatorSafe } from '@addp/common-frontend'
import { quickViewAPI } from '../api/quickView'
import { useQuickViewResourceDisplay } from '../composables/useQuickViewResourceDisplay'
import { useCurrentResultConfirmation } from '../composables/useCurrentResultConfirmation'
import { formatBytes, formatDateTime } from '../utils/formatters'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const executeWithCurrentResultConfirmation = useCurrentResultConfirmation()
const { displayText, engineName, loadQuickViewEngines, resourcePath } = useQuickViewResourceDisplay(t)

const routeQueryKeys = ['create', 'source_locator', 'source_engine_id', 'item_id', 'item_fingerprint', 'source_size_bytes', 'name', 'format', 'task_id']
const resolveRouteState = routeQuery => resolveManagerTaskWorkspaceRouteState({
  routeQuery,
  allowedQueryByTab: { tasks: routeQueryKeys, results: ['task_id'] }
})
const activeTab = ref(resolveRouteState(route.query).tab)
let routeDataReady = false
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
const selectedResultTask = ref(null)
const taskDialogVisible = ref(false)
const editingTask = ref(null)
const saving = ref(false)
const sourceSelection = ref(null)
const sourceInitialLocator = ref('')
const diagnosticsDialogVisible = ref(false)
const selectedDiagnosticResult = ref(null)
const resultStatuses = ['building', 'ready', 'failed', 'deleted']
const resultFilters = reactive({ task_id: undefined, status: '', q: '' })
const form = reactive(defaultForm())

const resultTaskFilterLabel = computed(() => {
  if (!selectedResultTask.value) return ''
  return selectedResultTask.value.name ? displayText(selectedResultTask.value.name) : t('manager.pointCloudCOPC.taskWithId', { id: selectedResultTask.value.id })
})

function defaultForm() {
  return {
    name: '',
    enabled: true,
    source: {
      item_locator: '',
      source_engine_id: 0,
      item_fingerprint: '',
      item_id: 0,
      format: 'laz',
      source_size_bytes: 0
    },
    result: {
      file_name: ''
    }
  }
}

function assignForm(value) {
  Object.assign(form, defaultForm(), value)
  form.source = { ...defaultForm().source, ...(value?.source || value?.config?.source || {}) }
  form.result = { ...defaultForm().result, ...(value?.result || value?.config?.result || {}) }
  sourceInitialLocator.value = form.source.item_locator
}

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
  return { items, total: Number(payload?.total || items.length || 0) }
}

const unwrapPayload = (response) => response?.data?.data || response?.data || response || {}
const source = (task) => task?.source || task?.config?.source || {}
const lastExecutionStatus = (task) => String(task?.last_execution_status || task?.lastExecutionStatus || '').trim()

const executionStatusTagType = (status) => {
  const value = String(status || '').toLowerCase()
  if (value === 'success') return 'success'
  if (['failed', 'timeout', 'cancelled', 'canceled'].includes(value)) return 'danger'
  if (['pending', 'running'].includes(value)) return 'warning'
  return 'info'
}

const executionStatusLabel = (status) => status || '-'

const resultStatusTagType = (status) => {
  const value = String(status || '').toLowerCase()
  if (value === 'ready') return 'success'
  if (value === 'failed') return 'danger'
  if (value === 'building') return 'warning'
  return 'info'
}

const resultStatusLabel = (status) => status || '-'
const objectValue = (value) => value && typeof value === 'object' && !Array.isArray(value) ? value : {}
const resultMetadata = (result) => objectValue(result?.metadata)
const workflowRuntime = (result) => objectValue(resultMetadata(result).workflow_runtime)
const sourceFacts = (result) => objectValue(resultMetadata(result).source)
const artifactFacts = (result) => objectValue(resultMetadata(result).artifact)
const hasDiagnostics = (result) => result?.status === 'ready' || Object.keys(workflowRuntime(result)).length > 0 || Object.keys(artifactFacts(result)).length > 0
const diagnosticValue = (value) => value === undefined || value === null || value === '' ? '-' : String(value)

const diagnosticExecutionTime = (result) => {
  const numberValue = Number(workflowRuntime(result).execution_time_ms)
  if (!Number.isFinite(numberValue) || numberValue <= 0) return '-'
  return t('manager.pointCloudCOPC.executionTimeMs', { ms: numberValue })
}

const formatLabel = (value) => {
  const text = String(value || '').trim()
  return text ? text.toUpperCase() : '-'
}

const sourceFactText = (value) => {
  const text = String(value || '').trim()
  if (!text) return '-'
  const parsed = parseLocatorSafe(text)
  if (!parsed || !Array.isArray(parsed.path) || parsed.path.length === 0) return text
  return parsed.path.join(' / ')
}

const selectionLocator = (selection, fallback) => String(selection?.identity?.locator || fallback || '').trim()
const selectionEngineID = (selection, fallback) => {
  const value = Number(selection?.identity?.engine_id || fallback || 0)
  return Number.isFinite(value) && value > 0 ? value : 0
}

const errorMessage = (error, fallback) => error?.response?.data?.error || error?.message || fallback

const pointCloudCOPCSourceFormat = (value) => {
  const sourceFormat = String(value || '').trim().toLowerCase()
  return ['las', 'laz', 'e57', 'pcd', 'xyz'].includes(sourceFormat) ? sourceFormat : ''
}

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const response = await quickViewAPI.listPointCloudCOPCTasks({ page: tasksPage.value, page_size: tasksPageSize.value })
    const { items, total } = unwrapList(response)
    tasks.value = items
    tasksTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.pointCloudCOPC.loadTasksFailed')))
  } finally {
    tasksLoading.value = false
  }
}

const loadResults = async () => {
  resultsLoading.value = true
  try {
    const params = { page: resultsPage.value, page_size: resultsPageSize.value }
    if (resultFilters.task_id) params.task_id = resultFilters.task_id
    if (resultFilters.status) params.status = resultFilters.status
    if (resultFilters.q) params.q = resultFilters.q
    const response = await quickViewAPI.listPointCloudCOPCs(params)
    const { items, total } = unwrapList(response)
    results.value = items
    resultsTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.pointCloudCOPC.loadResultsFailed')))
  } finally {
    resultsLoading.value = false
  }
}

const handleTabChange = async (tab) => {
  const routeState = resolveRouteState({ ...route.query, tab, task_id: tab === 'tasks' ? undefined : route.query.task_id })
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

const resetForm = () => {
  Object.assign(form, defaultForm())
  form.source = { ...defaultForm().source }
  form.result = { ...defaultForm().result }
  editingTask.value = null
  sourceSelection.value = null
  sourceInitialLocator.value = ''
}

const clearDialogQuery = async () => {
  const nextQuery = { ...route.query }
  delete nextQuery.create
  delete nextQuery.source_locator
  delete nextQuery.source_engine_id
  delete nextQuery.item_id
  delete nextQuery.item_fingerprint
  delete nextQuery.source_size_bytes
  delete nextQuery.name
  delete nextQuery.format
  if (activeTab.value === 'tasks') delete nextQuery.task_id
  const routeState = resolveRouteState(nextQuery)
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateManagerRoute(router, location, { history: 'replace' })
  }
}

const applySourcePresetFromQuery = () => {
  const locator = String(route.query.source_locator || '').trim()
  if (!locator) return
  const parsed = parseLocatorSafe(locator)
  form.source.item_locator = locator
  form.source.source_engine_id = Number(route.query.source_engine_id || parsed?.engineId || 0)
  form.source.item_id = Number(route.query.item_id || parsed?.itemId || 0)
  form.source.item_fingerprint = String(route.query.item_fingerprint || '').trim()
  form.source.source_size_bytes = Number(route.query.source_size_bytes || 0)
  form.source.format = pointCloudCOPCSourceFormat(route.query.format || form.source.format)
  sourceInitialLocator.value = locator
  const sourceName = String(route.query.name || '').replace(/\s*-\s*.*/, '').trim()
  form.name = t('manager.pointCloudCOPC.createFromSourceName', { name: sourceName || sourceFactText(locator) })
}

const openCreateDialog = async () => {
  resetForm()
  applySourcePresetFromQuery()
  taskDialogVisible.value = true
}

const requestCreateDialog = async () => {
  const routeState = resolveRouteState({ tab: 'tasks', create: '1' })
  await navigateManagerRoute(router, { path: route.path, query: routeState.query }, { history: 'push' })
}

const editTask = (task) => {
  editingTask.value = task
  assignForm({
    name: task.name,
    enabled: task.enabled,
    source: source(task),
    result: task.result || task.config?.result || {}
  })
  taskDialogVisible.value = true
}

const requestEditTask = async (task) => {
  const routeState = resolveRouteState({ tab: 'tasks', task_id: task.id })
  await navigateManagerRoute(router, { path: route.path, query: routeState.query }, { history: 'push' })
}

const loadSourceFacts = async (locator) => {
  const normalized = String(locator || '').trim()
  if (!normalized) return
  const parsed = parseLocatorSafe(normalized)
  form.source.item_locator = normalized
  form.source.source_engine_id = Number(parsed?.engineId || form.source.source_engine_id || 0)
  form.source.item_id = Number(parsed?.itemId || form.source.item_id || 0)
  try {
    const capability = await quickViewAPI.getQuickViewCapabilityByLocator(normalized)
    const pointCloud = capability?.point_cloud || {}
    form.source.source_engine_id = Number(capability?.source_engine_id || form.source.source_engine_id || 0)
    form.source.item_fingerprint = String(capability?.item_fingerprint || form.source.item_fingerprint || '').trim()
    form.source.source_size_bytes = Number(pointCloud.size_bytes || form.source.source_size_bytes || 0)
    form.source.format = pointCloudCOPCSourceFormat(pointCloud.format || form.source.format)
    if (!pointCloudCOPCSourceFormat(form.source.format)) {
      ElMessage.warning(t('manager.pointCloudCOPC.sourceFormatRequired'))
    }
  } catch (error) {
    ElMessage.warning(errorMessage(error, t('manager.pointCloudCOPC.loadSourceFactsFailed')))
  }
}

const validateForm = () => {
  const sourceLocator = selectionLocator(sourceSelection.value, form.source.item_locator)
  const sourceEngineID = selectionEngineID(sourceSelection.value, form.source.source_engine_id)
  if (!String(form.name || '').trim()) {
    ElMessage.warning(t('manager.pointCloudCOPC.nameRequired'))
    return null
  }
  if (!sourceLocator || !sourceEngineID) {
    ElMessage.warning(t('manager.pointCloudCOPC.sourceRequired'))
    return null
  }
  if (!String(form.source.item_fingerprint || '').trim()) {
    ElMessage.warning(t('manager.pointCloudCOPC.itemFingerprintRequired'))
    return null
  }
  if (!pointCloudCOPCSourceFormat(form.source.format)) {
    ElMessage.warning(t('manager.pointCloudCOPC.sourceFormatRequired'))
    return null
  }
  return { sourceLocator, sourceEngineID }
}

const taskPayload = (validated) => ({
  name: String(form.name || '').trim() || t('manager.pointCloudCOPC.defaultTaskName'),
  enabled: Boolean(form.enabled),
  config: {
    source: {
      item_locator: validated.sourceLocator,
      source_engine_id: validated.sourceEngineID,
      item_fingerprint: String(form.source.item_fingerprint || '').trim(),
      item_id: Number(form.source.item_id || 0),
      format: pointCloudCOPCSourceFormat(form.source.format),
      source_size_bytes: Number(form.source.source_size_bytes || 0)
    },
    result: {
      file_name: String(form.result.file_name || '').trim()
    }
  }
})

const saveTask = async () => {
  const validated = validateForm()
  if (!validated) return
  saving.value = true
  try {
    const payload = taskPayload(validated)
    if (editingTask.value) {
      await quickViewAPI.updatePointCloudCOPCTask(editingTask.value.id, payload)
    } else {
      await quickViewAPI.createPointCloudCOPCTask(payload)
    }
    taskDialogVisible.value = false
    await loadTasks()
    ElMessage.success(t('manager.pointCloudCOPC.saveSuccess'))
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.pointCloudCOPC.saveFailed')))
  } finally {
    saving.value = false
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await executeWithCurrentResultConfirmation(payload => quickViewAPI.executePointCloudCOPCTask(task.id, payload))
    const executionID = response?.execution_id || response?.data?.execution_id
    ElMessage.success(t('manager.pointCloudCOPC.executeSubmitted'))
    await loadTasks()
    if (executionID) await openMonitorExecution(executionID)
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(errorMessage(error, t('manager.pointCloudCOPC.executeFailed')))
  } finally {
    executingId.value = null
  }
}

const viewTaskResults = async (task) => {
  selectedResultTask.value = task
  resultFilters.task_id = task.id
  resultsPage.value = 1
  activeTab.value = 'results'
  await navigateManagerRoute(router, { query: { ...route.query, tab: 'results', task_id: task.id } }, { history: 'replace' })
}

const clearResultTaskFilter = async () => {
  selectedResultTask.value = null
  resultFilters.task_id = undefined
  resultsPage.value = 1
  const nextQuery = { ...route.query }
  delete nextQuery.task_id
  await navigateManagerRoute(router, { query: nextQuery }, { history: 'replace' })
}

const applyResultFilters = () => {
  resultsPage.value = 1
  loadResults()
}

const resetResultFilters = () => {
  resultFilters.status = ''
  resultFilters.q = ''
  applyResultFilters()
}

const deleteTask = async (task) => {
  await ElMessageBox.confirm(t('manager.pointCloudCOPC.deleteTaskConfirm'), t('manager.pointCloudCOPC.delete'), { type: 'warning' })
  await quickViewAPI.deletePointCloudCOPCTask(task.id)
  ElMessage.success(t('manager.pointCloudCOPC.deleteSuccess'))
  await loadTasks()
}

const deleteResult = async (result) => {
  await ElMessageBox.confirm(t('manager.pointCloudCOPC.deleteResultConfirm'), t('manager.pointCloudCOPC.delete'), { type: 'warning' })
  await quickViewAPI.deletePointCloudCOPC(result.id)
  await loadResults()
}

const openTaskExecution = (task) => openMonitorExecution(task.last_execution_id)
const openResultExecution = (result) => openMonitorExecution(result.last_execution_id)

const openDiagnostics = (result) => {
  selectedDiagnosticResult.value = result
  diagnosticsDialogVisible.value = true
}

const openSourcePreview = (result) => {
  if (!result?.locator) return
  navigateManagerRoute(router, { name: 'DataExplorer', query: { locator: result.locator } })
}

const openTaskFromQuery = async () => {
  const taskID = Number(route.query.task_id || 0)
  if (!taskID || activeTab.value === 'results') return
  try {
    const response = await quickViewAPI.getPointCloudCOPCTask(taskID)
    editTask(unwrapPayload(response))
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.pointCloudCOPC.loadTaskFailed')))
  }
}

const loadResultTaskFilterFromRoute = async () => {
  const taskID = Number(route.query.task_id || 0)
  if (!taskID) return
  resultFilters.task_id = taskID
  try {
    selectedResultTask.value = unwrapPayload(await quickViewAPI.getPointCloudCOPCTask(taskID))
  } catch {
    selectedResultTask.value = null
  }
}

watch(
  () => sourceSelection.value?.identity?.locator || '',
  (locator) => {
    if (locator) loadSourceFacts(locator)
  }
)

async function restoreWorkspaceFromRoute() {
  const routeState = resolveRouteState(route.query)
  activeTab.value = routeState.tab
  if (routeState.changed) {
    await navigateManagerRoute(router, { path: route.path, query: routeState.query }, { history: 'replace' })
    return
  }
  if (!routeDataReady) return
  if (routeState.tab === 'results') {
    taskDialogVisible.value = false
    selectedResultTask.value = null
    resultFilters.task_id = Number(routeState.query.task_id || 0) || undefined
    await loadResultTaskFilterFromRoute()
    await loadResults()
    return
  }
  if (routeState.query.create === '1') await openCreateDialog()
  else if (routeState.query.task_id) await openTaskFromQuery()
  else taskDialogVisible.value = false
  await loadTasks()
}

watch(() => route.query, restoreWorkspaceFromRoute)

onMounted(async () => {
  await restoreWorkspaceFromRoute()
  if (activeTab.value === 'results') {
    await loadResultTaskFilterFromRoute()
  }
  await Promise.all([loadQuickViewEngines(), loadTasks(), loadResults()])
  routeDataReady = true
  if (route.query.create === '1') {
    await openCreateDialog()
    return
  }
  await openTaskFromQuery()
})
</script>

<style scoped>
.point-cloud-copc {
  height: 100%;
}

.tab-toolbar,
.filter-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
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
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

.inline-tip-icon {
  color: var(--el-color-info);
  cursor: help;
}

.row-actions,
.filter-bar {
  display: flex;
  align-items: center;
  gap: 8px;
}

.row-actions {
  flex-wrap: wrap;
}

.pagination {
  margin-top: 16px;
  justify-content: flex-end;
}

.status-filter {
  width: 150px;
}

.keyword-filter {
  width: 260px;
}

.form-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 180px;
  gap: 16px;
}

.source-facts {
  margin: 16px 0 18px;
}

@media (max-width: 760px) {
  .form-grid {
    grid-template-columns: 1fr;
  }

  .keyword-filter,
  .status-filter {
    width: 100%;
  }
}
</style>
