<template>
  <div class="cad-preview-management">
    <el-card>
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.cadPreviewManagement.tasksTab')" name="tasks">
          <div class="toolbar">
            <span class="toolbar-tip">{{ t('manager.cadPreviewManagement.subtitle') }}</span>
            <el-button type="primary" @click="requestCreateDialog">{{ t('manager.cadPreviewManagement.create') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column :label="t('manager.cadPreviewManagement.name')" min-width="180" show-overflow-tooltip>
              <template #default="{ row }">{{ displayText(row.name) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.engine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(taskSource(row).source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ resourcePath(taskSource(row).item_locator) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.sourceFormat')" width="100">
              <template #default="{ row }">{{ formatLabel(taskSource(row).format) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.tileOptions')" width="150">
              <template #default="{ row }">{{ taskOptionsLabel(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.enabled')" width="90">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.cadPreviewManagement.yes') : t('manager.cadPreviewManagement.no') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.lastStatus')" width="120">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(row.last_execution_status)">{{ row.last_execution_status || '-' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.updatedAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.actions')" width="420" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.cadPreviewManagement.execute') }}
                  </el-button>
                  <el-button size="small" @click="viewTaskResults(row)">{{ t('manager.cadPreviewManagement.results') }}</el-button>
                  <el-button size="small" @click="requestEditTask(row)">{{ t('manager.cadPreviewManagement.edit') }}</el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                    {{ t('manager.cadPreviewManagement.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteTask(row)">{{ t('manager.cadPreviewManagement.delete') }}</el-button>
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

        <el-tab-pane :label="t('manager.cadPreviewManagement.resultsTab')" name="results">
          <div class="filter-bar">
            <el-tag v-if="selectedResultTask" type="primary" closable @close="clearTaskFilter">
              {{ selectedResultTask.name ? displayText(selectedResultTask.name) : `#${selectedResultTask.id}` }}
            </el-tag>
            <el-select v-model="resultFilters.status" clearable class="status-filter" :placeholder="t('manager.cadPreviewManagement.status')">
              <el-option v-for="status in resultStatuses" :key="status" :label="status" :value="status" />
            </el-select>
            <el-input
              v-model="resultFilters.q"
              clearable
              class="keyword-filter"
              :placeholder="t('manager.cadPreviewManagement.keywordPlaceholder')"
              @keyup.enter="applyResultFilters"
            />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.cadPreviewManagement.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.cadPreviewManagement.reset') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadResults" />
          </div>

          <el-table :data="results" v-loading="resultsLoading" stripe>
            <el-table-column :label="t('manager.cadPreviewManagement.engine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(row.source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ resourcePath(row.locator) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.sourceFormat')" width="100">
              <template #default="{ row }">{{ formatLabel(row.source_format) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.status')" width="110">
              <template #default="{ row }"><el-tag :type="resultStatusTagType(row.status)">{{ row.status || '-' }}</el-tag></template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.tiles')" width="120">
              <template #default="{ row }">{{ row.tile_count || 0 }} × {{ row.tile_size || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.zoomRange')" width="100">
              <template #default="{ row }">{{ zoomRange(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.bounds')" min-width="240" show-overflow-tooltip>
              <template #default="{ row }">{{ boundsLabel(row.bounds) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.sourceSize')" width="120">
              <template #default="{ row }">{{ formatBytes(row.source_size_bytes) }}</template>
            </el-table-column>
            <el-table-column prop="error_message" :label="t('manager.cadPreviewManagement.error')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.cadPreviewManagement.updatedAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.cadPreviewManagement.actions')" width="300" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" :disabled="!row.locator" @click="openSourcePreview(row)">
                    {{ t('manager.cadPreviewManagement.previewSource') }}
                  </el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openResultExecution(row)">
                    {{ t('manager.cadPreviewManagement.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteResult(row)">{{ t('manager.cadPreviewManagement.delete') }}</el-button>
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
      :title="editingTask ? t('manager.cadPreviewManagement.editTitle') : t('manager.cadPreviewManagement.createTitle')"
      width="820px"
      destroy-on-close
      @closed="clearTaskDialogRoute"
    >
      <el-form :model="form" label-position="top">
        <div class="form-grid">
          <el-form-item :label="t('manager.cadPreviewManagement.name')">
            <el-input v-model="form.name" :placeholder="t('manager.cadPreviewManagement.namePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('manager.cadPreviewManagement.enabled')">
            <el-switch v-model="form.enabled" :active-text="t('manager.cadPreviewManagement.yes')" :inactive-text="t('manager.cadPreviewManagement.no')" />
          </el-form-item>
        </div>
        <el-form-item :label="t('manager.cadPreviewManagement.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>

        <el-divider content-position="left">{{ t('manager.cadPreviewManagement.sourceScope') }}</el-divider>
        <ResourceTreePicker
          v-model="sourceSelection"
          mode="item"
          :initial-locator="sourceInitialLocator"
          :title="t('manager.cadPreviewManagement.sourceTreeTitle')"
          :engine-label="t('manager.cadPreviewManagement.engine')"
          :engine-placeholder="t('manager.cadPreviewManagement.enginePlaceholder')"
          :search-placeholder="t('manager.cadPreviewManagement.searchPlaceholder')"
          :search-all-engines-placeholder="t('manager.cadPreviewManagement.searchAllEnginesPlaceholder')"
          :search-empty-text="t('manager.cadPreviewManagement.searchEmptyText')"
          tree-height="300px"
        />
        <el-descriptions class="source-facts" :column="2" border size="small">
          <el-descriptions-item :label="t('manager.cadPreviewManagement.source')">{{ sourcePath(form.source.item_locator) }}</el-descriptions-item>
          <el-descriptions-item :label="t('manager.cadPreviewManagement.engine')">{{ engineName(form.source.source_engine_id) }}</el-descriptions-item>
          <el-descriptions-item :label="t('manager.cadPreviewManagement.itemFingerprint')">{{ form.source.item_fingerprint || '-' }}</el-descriptions-item>
          <el-descriptions-item :label="t('manager.cadPreviewManagement.sourceFormat')">{{ formatLabel(form.source.format) }}</el-descriptions-item>
          <el-descriptions-item :label="t('manager.cadPreviewManagement.sourceSize')">{{ formatBytes(form.source.source_size_bytes) }}</el-descriptions-item>
        </el-descriptions>

        <el-divider content-position="left">{{ t('manager.cadPreviewManagement.renderOptions') }}</el-divider>
        <div class="form-grid">
          <el-form-item :label="t('manager.cadPreviewManagement.tileSize')">
            <el-input-number v-model="form.options.tile_size" :min="128" :max="1024" :step="128" />
          </el-form-item>
          <el-form-item :label="t('manager.cadPreviewManagement.maxZoom')">
            <el-input-number v-model="form.options.max_zoom" :min="0" :max="7" />
          </el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="taskDialogVisible = false">{{ t('manager.cadPreviewManagement.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTask">{{ t('manager.cadPreviewManagement.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { navigateManagerRoute } from '@/utils/moduleNavigation'
import { resolveManagerTaskWorkspaceRouteState } from '@/utils/taskWorkspaceRoute'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Refresh } from '@element-plus/icons-vue'
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

const resolveRouteState = routeQuery => resolveManagerTaskWorkspaceRouteState({
  routeQuery,
  allowedQueryByTab: {
    tasks: ['create', 'task_id'],
    results: ['task_id']
  }
})
const activeTab = ref(resolveRouteState(route.query).tab)
let routeDataReady = false
let workspaceRestoreSequence = 0
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
const resultStatuses = ['building', 'ready', 'failed']
const resultFilters = reactive({ task_id: undefined, status: '', q: '' })
const taskDialogVisible = ref(false)
const editingTask = ref(null)
const saving = ref(false)
const sourceSelection = ref(null)
const sourceInitialLocator = ref('')
const form = reactive(defaultForm())

function defaultForm() {
  return {
    name: '',
    description: '',
    enabled: true,
    source: { item_locator: '', source_engine_id: 0, item_fingerprint: '', item_id: 0, format: '', source_size_bytes: 0 },
    options: { tile_size: 512, max_zoom: 4 }
  }
}

const unwrapPayload = (response) => response?.data?.data || response?.data || response || {}
const unwrapList = (response) => {
  const payload = unwrapPayload(response)
  const items = Array.isArray(payload?.data) ? payload.data : Array.isArray(payload?.items) ? payload.items : Array.isArray(payload) ? payload : []
  return { items, total: Number(payload?.total || items.length || 0) }
}
const errorMessage = (error, fallback) => error?.response?.data?.error || error?.message || fallback
const taskSource = (task) => task?.config?.source || {}
const taskOptions = (task) => task?.config?.options || {}
const formatLabel = (value) => String(value || '').trim().toUpperCase() || '-'
const cadFormat = (value) => ['dwg', 'dxf'].includes(String(value || '').trim().toLowerCase()) ? String(value).trim().toLowerCase() : ''
const taskOptionsLabel = (task) => `${taskOptions(task).tile_size || 512}px / z${taskOptions(task).max_zoom ?? 4}`

const executionStatusTagType = (status) => {
  const value = String(status || '').toLowerCase()
  if (value === 'success') return 'success'
  if (value === 'failed') return 'danger'
  if (['pending', 'running'].includes(value)) return 'warning'
  return 'info'
}
const resultStatusTagType = (status) => {
  if (status === 'ready') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'building') return 'warning'
  return 'info'
}
const zoomRange = (row) => `${row.min_zoom ?? 0}–${row.max_zoom ?? '-'}`
const boundsLabel = (bounds) => {
  if (!bounds || typeof bounds !== 'object') return '-'
  const values = [bounds.min_x, bounds.min_y, bounds.max_x, bounds.max_y]
  if (values.some(value => value === undefined || value === null)) return '-'
  return values.map(value => Number(value).toFixed(3)).join(', ')
}
const sourcePath = (locator) => {
  const text = String(locator || '').trim()
  const parsed = parseLocatorSafe(text)
  return parsed?.path?.length ? parsed.path.join(' / ') : text || '-'
}

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const { items, total } = unwrapList(await quickViewAPI.listCADPreviewTasks({ page: tasksPage.value, page_size: tasksPageSize.value }))
    tasks.value = items
    tasksTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.cadPreviewManagement.loadTasksFailed')))
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
    const { items, total } = unwrapList(await quickViewAPI.listCADPreviews(params))
    results.value = items
    resultsTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.cadPreviewManagement.loadResultsFailed')))
  } finally {
    resultsLoading.value = false
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
const resetResultFilters = () => { resultFilters.status = ''; resultFilters.q = ''; applyResultFilters() }

const resetForm = () => {
  const defaults = defaultForm()
  Object.assign(form, defaults)
  form.source = { ...defaults.source }
  form.options = { ...defaults.options }
  editingTask.value = null
  sourceSelection.value = null
  sourceInitialLocator.value = ''
}
const openCreateDialog = () => { resetForm(); taskDialogVisible.value = true }
const requestCreateDialog = async () => {
  const routeState = resolveRouteState({ tab: 'tasks', create: '1' })
  await navigateManagerRoute(router, {
    path: route.path,
    query: routeState.query
  }, { history: 'push' })
}

const clearTaskDialogRoute = async () => {
  if (resolveRouteState(route.query).tab !== 'tasks') return
  const nextQuery = { ...route.query }
  delete nextQuery.create
  delete nextQuery.task_id
  const routeState = resolveRouteState(nextQuery)
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateManagerRoute(router, location, { history: 'replace' })
  }
}

const editTask = (task) => {
  resetForm()
  editingTask.value = task
  form.name = task.name || ''
  form.description = task.description || ''
  form.enabled = Boolean(task.enabled)
  form.source = { ...defaultForm().source, ...taskSource(task) }
  form.options = { ...defaultForm().options, ...taskOptions(task) }
  sourceInitialLocator.value = form.source.item_locator
  taskDialogVisible.value = true
}

const requestEditTask = async (task) => {
  const routeState = resolveRouteState({ tab: 'tasks', task_id: task.id })
  await navigateManagerRoute(router, {
    path: route.path,
    query: routeState.query
  }, { history: 'push' })
}

const loadSourceFacts = async (locator) => {
  const normalized = String(locator || '').trim()
  if (!normalized) return
  const parsed = parseLocatorSafe(normalized)
  form.source.item_locator = normalized
  form.source.source_engine_id = Number(parsed?.engineId || form.source.source_engine_id || 0)
  form.source.item_id = Number(parsed?.itemId || form.source.item_id || 0)
  try {
    const capability = unwrapPayload(await quickViewAPI.getQuickViewCapabilityByLocator(normalized))
    const cad = capability?.cad || {}
    form.source.source_engine_id = Number(capability?.source_engine_id || form.source.source_engine_id || 0)
    form.source.item_fingerprint = String(capability?.item_fingerprint || '').trim()
    form.source.format = cadFormat(cad.format)
    form.source.source_size_bytes = Number(cad.size_bytes || 0)
    if (!form.source.format) ElMessage.warning(t('manager.cadPreviewManagement.sourceFormatRequired'))
  } catch (error) {
    ElMessage.warning(errorMessage(error, t('manager.cadPreviewManagement.loadSourceFactsFailed')))
  }
}

const validatedSource = () => {
  const locator = String(sourceSelection.value?.identity?.locator || form.source.item_locator || '').trim()
  const engineID = Number(sourceSelection.value?.identity?.engine_id || form.source.source_engine_id || 0)
  if (!String(form.name || '').trim()) {
    ElMessage.warning(t('manager.cadPreviewManagement.nameRequired'))
    return null
  }
  if (!locator || !engineID) {
    ElMessage.warning(t('manager.cadPreviewManagement.sourceRequired'))
    return null
  }
  if (!String(form.source.item_fingerprint || '').trim()) {
    ElMessage.warning(t('manager.cadPreviewManagement.itemFingerprintRequired'))
    return null
  }
  if (!cadFormat(form.source.format)) {
    ElMessage.warning(t('manager.cadPreviewManagement.sourceFormatRequired'))
    return null
  }
  return { locator, engineID }
}

const saveTask = async () => {
  const source = validatedSource()
  if (!source) return
  saving.value = true
  try {
    const payload = {
      name: String(form.name).trim(),
      description: String(form.description || '').trim(),
      enabled: Boolean(form.enabled),
      config: {
        source: {
          item_locator: source.locator,
          source_engine_id: source.engineID,
          item_fingerprint: String(form.source.item_fingerprint).trim(),
          item_id: Number(form.source.item_id || 0),
          format: cadFormat(form.source.format),
          source_size_bytes: Number(form.source.source_size_bytes || 0)
        },
        result: {},
        options: { tile_size: Number(form.options.tile_size), max_zoom: Number(form.options.max_zoom) }
      }
    }
    if (editingTask.value) await quickViewAPI.updateCADPreviewTask(editingTask.value.id, payload)
    else await quickViewAPI.createCADPreviewTask(payload)
    taskDialogVisible.value = false
    await loadTasks()
    ElMessage.success(t('manager.cadPreviewManagement.saveSuccess'))
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.cadPreviewManagement.saveFailed')))
  } finally {
    saving.value = false
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = unwrapPayload(await executeWithCurrentResultConfirmation(payload => quickViewAPI.executeCADPreviewTask(task.id, payload)))
    ElMessage.success(t('manager.cadPreviewManagement.executeSubmitted'))
    await loadTasks()
    if (response.execution_id) await openMonitorExecution(response.execution_id)
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(errorMessage(error, t('manager.cadPreviewManagement.executeFailed')))
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
const clearTaskFilter = async () => {
  selectedResultTask.value = null
  resultFilters.task_id = undefined
  const query = { ...route.query }
  delete query.task_id
  await navigateManagerRoute(router, { query }, { history: 'replace' })
}

const deleteTask = async (task) => {
  try {
    await ElMessageBox.confirm(t('manager.cadPreviewManagement.deleteTaskConfirm'), t('manager.cadPreviewManagement.delete'), { type: 'warning' })
    await quickViewAPI.deleteCADPreviewTask(task.id)
    await loadTasks()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(errorMessage(error, t('manager.cadPreviewManagement.deleteFailed')))
  }
}
const deleteResult = async (result) => {
  try {
    await ElMessageBox.confirm(t('manager.cadPreviewManagement.deleteResultConfirm'), t('manager.cadPreviewManagement.delete'), { type: 'warning' })
    await quickViewAPI.deleteCADPreview(result.id)
    await loadResults()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(errorMessage(error, t('manager.cadPreviewManagement.deleteFailed')))
  }
}
const openTaskExecution = (task) => openMonitorExecution(task.last_execution_id)
const openResultExecution = (result) => openMonitorExecution(result.last_execution_id)
const openSourcePreview = (result) => navigateManagerRoute(router, { name: 'DataExplorer', query: { locator: result.locator } })

watch(() => sourceSelection.value?.identity?.locator || '', (locator) => { if (locator) loadSourceFacts(locator) })

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
    taskDialogVisible.value = false
    const taskID = Number(routeState.query.task_id || 0)
    resultFilters.task_id = taskID || undefined
    selectedResultTask.value = null
    if (taskID) {
      try {
        const task = unwrapPayload(await quickViewAPI.getCADPreviewTask(taskID))
        if (restoreSequence !== workspaceRestoreSequence) return
        selectedResultTask.value = task
      } catch {
        if (restoreSequence !== workspaceRestoreSequence) return
        selectedResultTask.value = null
      }
    }
    await loadResults()
    return
  }

  const taskID = Number(routeState.query.task_id || 0)
  if (taskID) {
    try {
      const task = unwrapPayload(await quickViewAPI.getCADPreviewTask(taskID))
      if (restoreSequence !== workspaceRestoreSequence) return
      editTask(task)
    } catch (error) {
      if (restoreSequence !== workspaceRestoreSequence) return
      taskDialogVisible.value = false
      ElMessage.error(errorMessage(error, t('manager.cadPreviewManagement.loadTasksFailed')))
      await clearTaskDialogRoute()
      return
    }
  } else if (routeState.query.create === '1') {
    openCreateDialog()
  } else {
    taskDialogVisible.value = false
  }
  await loadTasks()
}

watch(() => route.query, restoreWorkspaceFromRoute)

onMounted(async () => {
  await restoreWorkspaceFromRoute()
  await loadQuickViewEngines()
  routeDataReady = true
  await restoreWorkspaceFromRoute()
})
</script>

<style scoped>
.cad-preview-management { height: 100%; }
.toolbar,
.filter-bar { display: flex; align-items: center; gap: 12px; margin-bottom: 16px; flex-wrap: wrap; }
.toolbar-tip { flex: 1; min-width: 260px; color: var(--el-text-color-secondary); font-size: 13px; }
.row-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: nowrap;
  white-space: nowrap;
}
.pagination { margin-top: 16px; justify-content: flex-end; }
.status-filter { width: 150px; }
.keyword-filter { width: 260px; }
.form-grid { display: grid; grid-template-columns: minmax(0, 1fr) 180px; gap: 16px; }
.source-facts { margin: 16px 0 18px; }
@media (max-width: 760px) {
  .form-grid { grid-template-columns: 1fr; }
  .status-filter,
  .keyword-filter { width: 100%; }
}
</style>
