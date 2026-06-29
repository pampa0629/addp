<template>
  <div class="model3d-tiles">
    <el-card>
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.model3DTiles.tasksTab')" name="tasks">
          <div class="tab-toolbar">
            <div class="toolbar-tip">
              <span class="toolbar-tip-text">{{ t('manager.model3DTiles.subtitle') }}</span>
              <el-tooltip
                :content="t('manager.model3DTiles.workflowDescription')"
                placement="bottom"
                :show-after="300"
              >
                <el-icon class="inline-tip-icon"><InfoFilled /></el-icon>
              </el-tooltip>
            </div>
            <el-button type="primary" @click="openCreateDialog">{{ t('manager.model3DTiles.create') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column prop="name" :label="t('manager.model3DTiles.name')" min-width="190" show-overflow-tooltip />
            <el-table-column :label="t('manager.model3DTiles.sourceEngine')" width="120">
              <template #default="{ row }">{{ source(row).source_engine_id || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.source')" min-width="260" show-overflow-tooltip>
              <template #default="{ row }">{{ locatorText(source(row).item_locator) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.target')" min-width="240" show-overflow-tooltip>
              <template #default="{ row }">{{ locatorText(target(row).storage_locator) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.datasetName')" min-width="160" show-overflow-tooltip>
              <template #default="{ row }">{{ target(row).dataset_name || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.enabled')" width="100">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.model3DTiles.enabledYes') : t('manager.model3DTiles.enabledNo') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.lastExecutionStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(lastExecutionStatus(row))">
                  {{ executionStatusLabel(lastExecutionStatus(row)) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.lastRunAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.last_run_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.actions')" width="360" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" @click="editTask(row)">{{ t('manager.model3DTiles.edit') }}</el-button>
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.model3DTiles.execute') }}
                  </el-button>
                  <el-button size="small" @click="viewTaskResults(row)">{{ t('manager.model3DTiles.results') }}</el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                    {{ t('manager.model3DTiles.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteTask(row)">{{ t('manager.model3DTiles.delete') }}</el-button>
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
            <el-tag v-if="resultTaskFilterLabel" type="primary" closable @close="clearResultTaskFilter">
              {{ resultTaskFilterLabel }}
            </el-tag>
            <el-select v-model="resultFilters.status" class="status-filter" clearable :placeholder="t('manager.model3DTiles.resultStatus')">
              <el-option v-for="status in resultStatuses" :key="status" :label="resultStatusLabel(status)" :value="status" />
            </el-select>
            <el-input
              v-model="resultFilters.q"
              class="keyword-filter"
              clearable
              :placeholder="t('manager.model3DTiles.keywordPlaceholder')"
              @keyup.enter="applyResultFilters"
            />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.model3DTiles.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.model3DTiles.reset') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadResults" />
          </div>

          <el-table :data="results" v-loading="resultsLoading" stripe>
            <el-table-column prop="task_name" :label="t('manager.model3DTiles.name')" min-width="190" show-overflow-tooltip />
            <el-table-column :label="t('manager.model3DTiles.source')" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">{{ locatorText(row.source_locator) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.target')" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">{{ row.result_path || '-' }}</template>
            </el-table-column>
            <el-table-column prop="dataset_name" :label="t('manager.model3DTiles.datasetName')" min-width="150" show-overflow-tooltip />
            <el-table-column :label="t('manager.model3DTiles.resultStatus')" width="120">
              <template #default="{ row }">
                <el-tag :type="resultStatusTagType(row.status)">
                  {{ resultStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.size')" width="120">
              <template #default="{ row }">{{ formatBytes(row.size_bytes) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.lastRunAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.updated_at || row.last_run_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DTiles.actions')" width="220" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" :disabled="!row.locator" @click="openResultPreview(row)">
                    {{ t('manager.model3DTiles.preview') }}
                  </el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openResultExecution(row)">
                    {{ t('manager.model3DTiles.monitor') }}
                  </el-button>
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
      :title="editingTask ? t('manager.model3DTiles.editTitle') : t('manager.model3DTiles.createTitle')"
      width="860px"
      destroy-on-close
    >
      <el-form label-position="top" :model="form">
        <div class="form-grid">
          <el-form-item :label="t('manager.model3DTiles.name')">
            <el-input v-model="form.name" :placeholder="t('manager.model3DTiles.namePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('manager.model3DTiles.enabled')">
            <el-switch
              v-model="form.enabled"
              :active-text="t('manager.model3DTiles.enabledYes')"
              :inactive-text="t('manager.model3DTiles.enabledNo')"
            />
          </el-form-item>
        </div>

        <el-form-item :label="t('manager.model3DTiles.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>

        <el-divider content-position="left">{{ t('manager.model3DTiles.sourceScope') }}</el-divider>
        <ResourceTreePicker
          v-model="sourceSelection"
          mode="item"
          :initial-locator="sourceInitialLocator"
          :title="t('manager.model3DTiles.sourceTreeTitle')"
          :engine-label="t('manager.model3DTiles.engine')"
          :engine-placeholder="t('manager.model3DTiles.enginePlaceholder')"
          :search-placeholder="t('manager.model3DTiles.searchPlaceholder')"
          :search-all-engines-placeholder="t('manager.model3DTiles.searchAllEnginesPlaceholder')"
          :search-empty-text="t('manager.model3DTiles.searchEmptyText')"
          tree-height="300px"
        />

        <el-divider content-position="left">{{ t('manager.model3DTiles.targetScope') }}</el-divider>
        <ResourceTreePicker
          v-model="targetSelection"
          mode="node"
          :initial-locator="targetInitialLocator"
          :title="t('manager.model3DTiles.targetTreeTitle')"
          :engine-label="t('manager.model3DTiles.engine')"
          :engine-placeholder="t('manager.model3DTiles.enginePlaceholder')"
          :search-placeholder="t('manager.model3DTiles.searchPlaceholder')"
          :search-all-engines-placeholder="t('manager.model3DTiles.searchAllEnginesPlaceholder')"
          :search-empty-text="t('manager.model3DTiles.searchEmptyText')"
          tree-height="300px"
        />

        <div class="form-grid result-grid">
          <el-form-item :label="t('manager.model3DTiles.datasetName')">
            <el-input v-model="form.target.dataset_name" :placeholder="t('manager.model3DTiles.datasetNamePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('manager.model3DTiles.tilesFormat')">
            <el-tag class="format-tag">3dtiles</el-tag>
          </el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="taskDialogVisible = false">{{ t('manager.model3DTiles.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTask">{{ t('manager.model3DTiles.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { InfoFilled, Refresh } from '@element-plus/icons-vue'
import { ResourceTreePicker, openMonitorExecution, parseLocatorSafe } from '@addp/common-frontend'
import { quickViewAPI } from '../api/quickView'
import { dataExplorerAPI } from '../api/dataExplorer'
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
const taskDialogVisible = ref(false)
const editingTask = ref(null)
const saving = ref(false)
const sourceSelection = ref(null)
const targetSelection = ref(null)
const sourceInitialLocator = ref('')
const targetInitialLocator = ref('')
const selectedResultTask = ref(null)
const resultStatuses = ['ready', 'missing', 'running', 'failed', 'never_run']
const resultFilters = reactive({ task_id: undefined, status: '', q: '' })

const form = reactive(defaultForm())

const resultTaskFilterLabel = computed(() => {
  if (!selectedResultTask.value) return ''
  return selectedResultTask.value.name || t('manager.model3DTiles.taskWithId', { id: selectedResultTask.value.id })
})

function defaultForm() {
  return {
    name: '',
    description: '',
    enabled: true,
    source: {
      item_locator: '',
      source_engine_id: 0,
      format: 'osgb_scene'
    },
    target: {
      storage_locator: '',
      target_engine_id: 0,
      dataset_name: ''
    },
    tiles: {
      format: '3dtiles'
    }
  }
}

function resetForm() {
  Object.assign(form, defaultForm())
  form.source = { ...defaultForm().source }
  form.target = { ...defaultForm().target }
  form.tiles = { ...defaultForm().tiles }
  editingTask.value = null
  sourceSelection.value = null
  targetSelection.value = null
  sourceInitialLocator.value = ''
  targetInitialLocator.value = ''
}

function assignForm(value) {
  Object.assign(form, defaultForm(), value)
  form.source = { ...defaultForm().source, ...(value?.source || value?.config?.source || {}) }
  form.target = { ...defaultForm().target, ...(value?.target || value?.config?.target || {}) }
  form.tiles = { ...defaultForm().tiles, ...(value?.tiles || value?.config?.tiles || {}) }
  sourceInitialLocator.value = form.source.item_locator
  targetInitialLocator.value = form.target.storage_locator
}

const unwrapPayload = (response) => response?.data?.data || response?.data || response

const unwrapList = (response) => {
  const payload = unwrapPayload(response)
  const items = Array.isArray(payload?.data)
    ? payload.data
    : Array.isArray(payload?.items)
      ? payload.items
      : Array.isArray(payload)
        ? payload
        : []
  return { items, total: Number(payload?.total || items.length || 0) }
}

const source = (task) => task?.source || task?.config?.source || {}
const target = (task) => task?.target || task?.config?.target || {}
const tiles = (task) => task?.tiles || task?.config?.tiles || {}

const lastExecutionStatus = (task) => String(task?.last_execution_status || task?.lastExecutionStatus || '').trim()

const executionStatusTagType = (status) => {
  const value = String(status || '').toLowerCase()
  if (value === 'success') return 'success'
  if (['failed', 'timeout', 'cancelled', 'canceled'].includes(value)) return 'danger'
  if (['pending', 'running'].includes(value)) return 'warning'
  return 'info'
}

const executionStatusLabel = (status) => {
  const key = String(status || '').trim().toLowerCase()
  if (!key) return t('manager.model3DTiles.statusNeverRun')
  return t(`manager.model3DTiles.status.${key}`, key)
}

const resultStatusTagType = (status) => {
  const value = String(status || '').toLowerCase()
  if (value === 'ready') return 'success'
  if (['failed', 'missing'].includes(value)) return 'danger'
  if (value === 'running') return 'warning'
  return 'info'
}

const resultStatusLabel = (status) => {
  const key = String(status || '').trim().toLowerCase()
  if (!key) return '-'
  return t(`manager.model3DTiles.resultStatuses.${key}`, key)
}

const errorMessage = (error, fallback) => (
  error?.response?.data?.error ||
  error?.response?.data?.message ||
  error?.message ||
  fallback
)

const locatorText = (locator) => {
  const parsed = parseLocatorSafe(locator)
  if (!parsed) return locator || '-'
  const parts = Array.isArray(parsed.path) ? parsed.path : []
  return parts.length ? parts.join(' / ') : (locator || '-')
}

const datasetName = (task) => String(target(task).dataset_name || 'model_3d_tiles').trim()

const resultPath = (task) => {
  const targetLocator = target(task).storage_locator
  const parsed = parseLocatorSafe(targetLocator)
  const parts = Array.isArray(parsed?.path) ? [...parsed.path] : []
  const name = datasetName(task)
  if (name) parts.push(name)
  return parts.length ? parts.join(' / ') : name
}

const selectionLocator = (selection, fallback) => String(selection?.identity?.locator || fallback || '').trim()
const selectionEngineID = (selection, fallback) => {
  const value = Number(selection?.identity?.engine_id || fallback || 0)
  return Number.isFinite(value) && value > 0 ? value : 0
}

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const response = await quickViewAPI.listModel3DTilesTasks({ page: tasksPage.value, page_size: tasksPageSize.value })
    const { items, total } = unwrapList(response)
    tasks.value = items
    tasksTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DTiles.loadTasksFailed')))
  } finally {
    tasksLoading.value = false
  }
}

const loadResultTaskSource = async () => {
  if (resultFilters.task_id) {
    const response = await quickViewAPI.getModel3DTilesTask(resultFilters.task_id)
    return [unwrapPayload(response)].filter(Boolean)
  }
  const response = await quickViewAPI.listModel3DTilesTasks({ page: 1, page_size: 500 })
  return unwrapList(response).items
}

const loadResults = async () => {
  resultsLoading.value = true
  try {
    const taskItems = await loadResultTaskSource()
    const rows = await Promise.all(taskItems.map(resolveTaskResult))
    const filtered = filterResultRows(rows)
    resultsTotal.value = filtered.length
    const start = (resultsPage.value - 1) * resultsPageSize.value
    results.value = filtered.slice(start, start + resultsPageSize.value)
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DTiles.loadResultsFailed')))
  } finally {
    resultsLoading.value = false
  }
}

const resolveTaskResult = async (task) => {
  const taskTarget = target(task)
  const targetLocator = String(taskTarget.storage_locator || '').trim()
  const targetEngineID = Number(taskTarget.target_engine_id || parseLocatorSafe(targetLocator)?.engineId || 0)
  const name = datasetName(task)
  let resultNode = null
  if (targetLocator && targetEngineID) {
    try {
      const response = await dataExplorerAPI.getNodeChildren(targetEngineID, targetLocator)
      const children = response?.children || response?.data?.children || []
      resultNode = children.find((child) => isTaskResultNode(child, name)) || null
    } catch {
      resultNode = null
    }
  }

  const metadata = resultNode?.metadata || {}
  const status = resultNode ? 'ready' : missingResultStatus(task)
  return {
    task_id: task.id,
    task_name: task.name,
    source_locator: source(task).item_locator,
    target_locator: targetLocator,
    target_engine_id: targetEngineID,
    dataset_name: name,
    result_path: resultPath(task),
    locator: resultNode?.locator || resultNode?.id || '',
    status,
    format: metadata.format || metadata.item?.format || '',
    size_bytes: Number(metadata.size_bytes || metadata.storage?.total_size || 0) || 0,
    updated_at: metadata.scanned_at || metadata.updated_at || '',
    last_run_at: task.last_run_at,
    last_execution_id: task.last_execution_id
  }
}

const isTaskResultNode = (node, name) => {
  if (!node || !name) return false
  const parsed = parseLocatorSafe(node.locator || node.id)
  const lastPath = Array.isArray(parsed?.path) ? parsed.path[parsed.path.length - 1] : ''
  const metadata = node.metadata || {}
  const format = String(metadata.format || metadata.item?.format || '').toLowerCase()
  return (node.label === name || lastPath === name) && format === '3dtiles'
}

const missingResultStatus = (task) => {
  const status = lastExecutionStatus(task).toLowerCase()
  if (!status) return 'never_run'
  if (['pending', 'running'].includes(status)) return 'running'
  if (['failed', 'timeout', 'cancelled', 'canceled'].includes(status)) return 'failed'
  return 'missing'
}

const filterResultRows = (rows) => {
  const status = String(resultFilters.status || '').trim().toLowerCase()
  const keyword = String(resultFilters.q || '').trim().toLowerCase()
  return rows.filter((row) => {
    if (status && row.status !== status) return false
    if (!keyword) return true
    return [
      row.task_name,
      row.source_locator,
      row.target_locator,
      row.dataset_name,
      row.result_path,
      row.locator
    ].some((value) => String(value || '').toLowerCase().includes(keyword))
  })
}

const handleTasksSizeChange = () => {
  tasksPage.value = 1
  loadTasks()
}

const handleResultsSizeChange = () => {
  resultsPage.value = 1
  loadResults()
}

const clearDialogQuery = async () => {
  const nextQuery = { ...route.query }
  delete nextQuery.task_id
  delete nextQuery.create
  delete nextQuery.source_locator
  delete nextQuery.source_engine_id
  delete nextQuery.item_id
  delete nextQuery.source_size_bytes
  delete nextQuery.name
  await router.replace({ query: nextQuery })
}

const applySourcePresetFromQuery = () => {
  const locator = String(route.query.source_locator || '').trim()
  if (!locator) return
  const engineID = Number(route.query.source_engine_id || parseLocatorSafe(locator)?.engineId || 0)
  form.source.item_locator = locator
  form.source.source_engine_id = engineID
  form.source.format = 'osgb_scene'
  sourceInitialLocator.value = locator
  const sourceName = String(route.query.name || '').replace(/\s*-\s*.*/, '').trim()
  form.name = t('manager.model3DTiles.createFromSourceName', { name: sourceName || locatorText(locator) })
  form.target.dataset_name = ''
}

const openCreateDialog = async () => {
  resetForm()
  applySourcePresetFromQuery()
  await clearDialogQuery()
  taskDialogVisible.value = true
}

const editTask = (task) => {
  editingTask.value = task
  assignForm({
    name: task.name,
    description: task.description || '',
    enabled: task.enabled,
    source: source(task),
    target: target(task),
    tiles: tiles(task)
  })
  taskDialogVisible.value = true
  router.replace({ query: { ...route.query, task_id: task.id } })
}

const validateForm = () => {
  const sourceLocator = selectionLocator(sourceSelection.value, form.source.item_locator)
  const sourceEngineID = selectionEngineID(sourceSelection.value, form.source.source_engine_id)
  const targetLocator = selectionLocator(targetSelection.value, form.target.storage_locator)
  const targetEngineID = selectionEngineID(targetSelection.value, form.target.target_engine_id)

  if (!String(form.name || '').trim()) {
    ElMessage.warning(t('manager.model3DTiles.nameRequired'))
    return null
  }
  if (!sourceLocator || !sourceEngineID) {
    ElMessage.warning(t('manager.model3DTiles.sourceRequired'))
    return null
  }
  if (!targetLocator || !targetEngineID) {
    ElMessage.warning(t('manager.model3DTiles.targetRequired'))
    return null
  }
  return { sourceLocator, sourceEngineID, targetLocator, targetEngineID }
}

const taskPayload = (validated) => ({
  name: String(form.name || '').trim(),
  description: String(form.description || '').trim(),
  enabled: Boolean(form.enabled),
  config: {
    source: {
      item_locator: validated.sourceLocator,
      source_engine_id: validated.sourceEngineID,
      format: 'osgb_scene'
    },
    target: {
      storage_locator: validated.targetLocator,
      target_engine_id: validated.targetEngineID,
      dataset_name: String(form.target.dataset_name || '').trim()
    },
    tiles: {
      format: '3dtiles'
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
      await quickViewAPI.updateModel3DTilesTask(editingTask.value.id, payload)
    } else {
      await quickViewAPI.createModel3DTilesTask(payload)
    }
    taskDialogVisible.value = false
    await loadTasks()
    ElMessage.success(t('manager.model3DTiles.saveSuccess'))
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DTiles.saveFailed')))
  } finally {
    saving.value = false
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await quickViewAPI.executeModel3DTilesTask(task.id)
    const executionID = response?.execution_id || response?.data?.execution_id
    ElMessage.success(t('manager.model3DTiles.executeSubmitted'))
    await loadTasks()
    if (activeTab.value === 'results') {
      await loadResults()
    }
    if (executionID) {
      await openMonitorExecution(executionID)
    }
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DTiles.executeFailed')))
  } finally {
    executingId.value = null
  }
}

const deleteTask = async (task) => {
  await ElMessageBox.confirm(t('manager.model3DTiles.deleteTaskConfirm'), t('manager.model3DTiles.delete'), { type: 'warning' })
  await quickViewAPI.deleteModel3DTilesTask(task.id)
  ElMessage.success(t('manager.model3DTiles.deleteSuccess'))
  await loadTasks()
  if (activeTab.value === 'results') {
    await loadResults()
  }
}

const openTaskExecution = (task) => openMonitorExecution(task.last_execution_id)
const openResultExecution = (result) => openMonitorExecution(result.last_execution_id)

const viewTaskResults = async (task) => {
  selectedResultTask.value = task
  resultFilters.task_id = task.id
  resultsPage.value = 1
  activeTab.value = 'results'
  await router.replace({ query: { ...route.query, tab: 'results', task_id: task.id } })
  await loadResults()
}

const clearResultTaskFilter = async () => {
  selectedResultTask.value = null
  resultFilters.task_id = undefined
  resultsPage.value = 1
  const nextQuery = { ...route.query }
  delete nextQuery.task_id
  await router.replace({ query: nextQuery })
  await loadResults()
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

const openResultPreview = (result) => {
  if (!result?.locator) return
  router.push({
    name: 'DataExplorer',
    query: { locator: result.locator }
  })
}

const openTaskFromQuery = async () => {
  const taskID = Number(route.query.task_id || 0)
  if (!taskID || activeTab.value === 'results') return
  try {
    const response = await quickViewAPI.getModel3DTilesTask(taskID)
    editTask(unwrapPayload(response))
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DTiles.loadTaskFailed')))
  }
}

const loadResultTaskFilterFromRoute = async () => {
  const taskID = Number(route.query.task_id || 0)
  if (!taskID) return
  resultFilters.task_id = taskID
  try {
    selectedResultTask.value = unwrapPayload(await quickViewAPI.getModel3DTilesTask(taskID))
  } catch {
    selectedResultTask.value = null
  }
}

const handleTabChange = async (tab) => {
  await router.replace({ query: { ...route.query, tab } })
  if (tab === 'results') {
    await loadResultTaskFilterFromRoute()
    await loadResults()
    return
  }
  await loadTasks()
}

onMounted(async () => {
  if (activeTab.value === 'results') {
    await loadResultTaskFilterFromRoute()
    await loadResults()
  } else {
    await loadTasks()
  }
  if (route.query.create === '1') {
    await openCreateDialog()
    return
  }
  await openTaskFromQuery()
})
</script>

<style scoped>
.model3d-tiles {
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

.filter-bar {
  justify-content: flex-start;
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

.inline-tip-icon {
  color: var(--el-color-info);
  cursor: help;
}

.row-actions {
  display: flex;
  align-items: center;
  gap: 8px;
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

.result-grid {
  margin-top: 18px;
}

.format-tag {
  width: fit-content;
}

@media (max-width: 768px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
