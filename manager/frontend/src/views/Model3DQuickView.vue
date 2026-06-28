<template>
  <div class="model3d-quick-view">
    <el-card>
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.model3DQuickView.tasksTab')" name="tasks">
          <div class="tab-toolbar">
            <div class="toolbar-tip">
              <span class="toolbar-tip-text">{{ t('manager.model3DQuickView.subtitle') }}</span>
              <el-tooltip
                :content="t('manager.model3DQuickView.workflowDescription')"
                placement="bottom"
                :show-after="300"
              >
                <el-icon class="inline-tip-icon"><InfoFilled /></el-icon>
              </el-tooltip>
            </div>
            <el-button type="primary" @click="openCreateDialog">{{ t('manager.model3DQuickView.create') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column prop="name" :label="t('manager.model3DQuickView.name')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.model3DQuickView.engine')" width="120">
              <template #default="{ row }">{{ source(row).source_engine_id || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DQuickView.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ source(row).item_locator || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DQuickView.sourceSize')" width="120">
              <template #default="{ row }">{{ formatBytes(source(row).source_size_bytes) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DQuickView.enabled')" width="90">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.model3DQuickView.enabledYes') : t('manager.model3DQuickView.enabledNo') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DQuickView.lastExecutionStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(lastExecutionStatus(row))">
                  {{ executionStatusLabel(lastExecutionStatus(row)) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DQuickView.lastRunAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.last_run_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DQuickView.actions')" width="360" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.model3DQuickView.execute') }}
                  </el-button>
                  <el-button size="small" @click="viewTaskResults(row)">{{ t('manager.model3DQuickView.results') }}</el-button>
                  <el-button size="small" @click="editTask(row)">{{ t('manager.model3DQuickView.edit') }}</el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                    {{ t('manager.model3DQuickView.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteTask(row)">{{ t('manager.model3DQuickView.delete') }}</el-button>
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

        <el-tab-pane :label="t('manager.model3DQuickView.resultsTab')" name="results">
          <div class="filter-bar">
            <el-tag v-if="resultTaskFilterLabel" type="primary" closable @close="clearResultTaskFilter">
              {{ resultTaskFilterLabel }}
            </el-tag>
            <el-select v-model="resultFilters.status" class="status-filter" clearable :placeholder="t('manager.model3DQuickView.resultStatus')">
              <el-option v-for="status in resultStatuses" :key="status" :label="resultStatusLabel(status)" :value="status" />
            </el-select>
            <el-input
              v-model="resultFilters.q"
              class="keyword-filter"
              clearable
              :placeholder="t('manager.model3DQuickView.keywordPlaceholder')"
              @keyup.enter="applyResultFilters"
            />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.model3DQuickView.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.model3DQuickView.reset') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadResults" />
          </div>

          <el-table :data="results" v-loading="resultsLoading" stripe>
            <el-table-column :label="t('manager.model3DQuickView.engine')" width="120">
              <template #default="{ row }">{{ row.source_engine_id || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DQuickView.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ row.locator || '-' }}</template>
            </el-table-column>
            <el-table-column prop="file_name" :label="t('manager.model3DQuickView.fileName')" min-width="150" show-overflow-tooltip />
            <el-table-column :label="t('manager.model3DQuickView.resultStatus')" width="120">
              <template #default="{ row }">
                <el-tag :type="resultStatusTagType(row.status)">
                  {{ resultStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DQuickView.size')" width="120">
              <template #default="{ row }">{{ formatBytes(row.size_bytes) }}</template>
            </el-table-column>
            <el-table-column prop="error_message" :label="t('manager.model3DQuickView.error')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.model3DQuickView.updatedAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.model3DQuickView.actions')" width="280" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" :disabled="row.status !== 'ready' || !row.content_url" @click="openGLB(row)">
                    {{ t('manager.model3DQuickView.previewGLB') }}
                  </el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openResultExecution(row)">
                    {{ t('manager.model3DQuickView.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteResult(row)">{{ t('manager.model3DQuickView.delete') }}</el-button>
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
      :title="editingTask ? t('manager.model3DQuickView.editTitle') : t('manager.model3DQuickView.createTitle')"
      width="820px"
      destroy-on-close
    >
      <el-form label-position="top" :model="form">
        <div class="form-grid">
          <el-form-item :label="t('manager.model3DQuickView.name')">
            <el-input v-model="form.name" :placeholder="t('manager.model3DQuickView.namePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('manager.model3DQuickView.enabled')">
            <el-switch
              v-model="form.enabled"
              :active-text="t('manager.model3DQuickView.enabledYes')"
              :inactive-text="t('manager.model3DQuickView.enabledNo')"
            />
          </el-form-item>
        </div>

        <el-divider content-position="left">{{ t('manager.model3DQuickView.sourceScope') }}</el-divider>
        <ResourceTreePicker
          v-model="sourceSelection"
          mode="item"
          :initial-locator="sourceInitialLocator"
          :title="t('manager.model3DQuickView.sourceTreeTitle')"
          :engine-label="t('manager.model3DQuickView.engine')"
          :engine-placeholder="t('manager.model3DQuickView.enginePlaceholder')"
          :search-placeholder="t('manager.model3DQuickView.searchPlaceholder')"
          :search-all-engines-placeholder="t('manager.model3DQuickView.searchAllEnginesPlaceholder')"
          :search-empty-text="t('manager.model3DQuickView.searchEmptyText')"
          tree-height="300px"
        />

        <el-descriptions class="source-facts" :column="2" border size="small">
          <el-descriptions-item :label="t('manager.model3DQuickView.sourceLocator')">
            {{ sourceFactText(form.source.item_locator) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.model3DQuickView.sourceEngineID')">
            {{ form.source.source_engine_id || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.model3DQuickView.itemFingerprint')">
            {{ form.source.item_fingerprint || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.model3DQuickView.sourceSize')">
            {{ formatBytes(form.source.source_size_bytes) }}
          </el-descriptions-item>
        </el-descriptions>

        <el-form-item :label="t('manager.model3DQuickView.fileName')">
          <el-input v-model="form.result.file_name" :placeholder="t('manager.model3DQuickView.fileNamePlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="taskDialogVisible = false">{{ t('manager.model3DQuickView.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTask">{{ t('manager.model3DQuickView.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { InfoFilled, Refresh } from '@element-plus/icons-vue'
import { ResourceTreePicker, openMonitorExecution, parseLocatorSafe } from '@addp/common-frontend'
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
const selectedResultTask = ref(null)
const taskDialogVisible = ref(false)
const editingTask = ref(null)
const saving = ref(false)
const sourceSelection = ref(null)
const sourceInitialLocator = ref('')
const resultStatuses = ['building', 'ready', 'failed', 'deleted']
const resultFilters = reactive({ task_id: undefined, status: '', q: '' })

const form = reactive(defaultForm())

const resultTaskFilterLabel = computed(() => {
  if (!selectedResultTask.value) return ''
  return selectedResultTask.value.name || t('manager.model3DQuickView.taskWithId', { id: selectedResultTask.value.id })
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
      format: 'osgb',
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

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const response = await quickViewAPI.listModel3DQuickViewTasks({ page: tasksPage.value, page_size: tasksPageSize.value })
    const { items, total } = unwrapList(response)
    tasks.value = items
    tasksTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DQuickView.loadTasksFailed')))
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
    const response = await quickViewAPI.listModel3DQuickViews(params)
    const { items, total } = unwrapList(response)
    results.value = items
    resultsTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DQuickView.loadResultsFailed')))
  } finally {
    resultsLoading.value = false
  }
}

const handleTabChange = async (tab) => {
  await router.replace({ query: { ...route.query, tab } })
  if (tab === 'tasks') await loadTasks()
  if (tab === 'results') await loadResults()
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
  await router.replace({ query: nextQuery })
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
  sourceInitialLocator.value = locator
  const sourceName = String(route.query.name || '').replace(/\s*-\s*.*/, '').trim()
  form.name = t('manager.model3DQuickView.createFromSourceName', { name: sourceName || sourceFactText(locator) })
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
    enabled: task.enabled,
    source: source(task),
    result: task.result || task.config?.result || {}
  })
  taskDialogVisible.value = true
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
    const model3D = capability?.model_3d || {}
    form.source.source_engine_id = Number(capability?.source_engine_id || form.source.source_engine_id || 0)
    form.source.item_fingerprint = String(capability?.item_fingerprint || form.source.item_fingerprint || '').trim()
    form.source.source_size_bytes = Number(model3D.size_bytes || form.source.source_size_bytes || 0)
    form.source.format = String(model3D.format || form.source.format || 'osgb').toLowerCase()
  } catch (error) {
    ElMessage.warning(errorMessage(error, t('manager.model3DQuickView.loadSourceFactsFailed')))
  }
}

const validateForm = () => {
  const sourceLocator = selectionLocator(sourceSelection.value, form.source.item_locator)
  const sourceEngineID = selectionEngineID(sourceSelection.value, form.source.source_engine_id)
  if (!String(form.name || '').trim()) {
    ElMessage.warning(t('manager.model3DQuickView.nameRequired'))
    return null
  }
  if (!sourceLocator || !sourceEngineID) {
    ElMessage.warning(t('manager.model3DQuickView.sourceRequired'))
    return null
  }
  if (!String(form.source.item_fingerprint || '').trim()) {
    ElMessage.warning(t('manager.model3DQuickView.itemFingerprintRequired'))
    return null
  }
  return { sourceLocator, sourceEngineID }
}

const taskPayload = (validated) => ({
  name: String(form.name || '').trim() || t('manager.model3DQuickView.defaultTaskName'),
  enabled: Boolean(form.enabled),
  config: {
    source: {
      item_locator: validated.sourceLocator,
      source_engine_id: validated.sourceEngineID,
      item_fingerprint: String(form.source.item_fingerprint || '').trim(),
      item_id: Number(form.source.item_id || 0),
      format: 'osgb',
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
      await quickViewAPI.updateModel3DQuickViewTask(editingTask.value.id, payload)
    } else {
      await quickViewAPI.createModel3DQuickViewTask(payload)
    }
    taskDialogVisible.value = false
    await loadTasks()
    ElMessage.success(t('manager.model3DQuickView.saveSuccess'))
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DQuickView.saveFailed')))
  } finally {
    saving.value = false
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await quickViewAPI.executeModel3DQuickViewTask(task.id)
    const executionID = response?.execution_id || response?.data?.execution_id
    ElMessage.success(t('manager.model3DQuickView.executeSubmitted'))
    await loadTasks()
    if (executionID) {
      await openMonitorExecution(executionID)
    }
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.model3DQuickView.executeFailed')))
  } finally {
    executingId.value = null
  }
}

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

const deleteTask = async (task) => {
  await ElMessageBox.confirm(t('manager.model3DQuickView.deleteTaskConfirm'), t('manager.model3DQuickView.delete'), { type: 'warning' })
  await quickViewAPI.deleteModel3DQuickViewTask(task.id)
  ElMessage.success(t('manager.model3DQuickView.deleteSuccess'))
  await loadTasks()
}

const deleteResult = async (result) => {
  await ElMessageBox.confirm(t('manager.model3DQuickView.deleteResultConfirm'), t('manager.model3DQuickView.delete'), { type: 'warning' })
  await quickViewAPI.deleteModel3DQuickView(result.id)
  await loadResults()
}

const openTaskExecution = (task) => openMonitorExecution(task.last_execution_id)
const openResultExecution = (result) => openMonitorExecution(result.last_execution_id)

const openGLB = (result) => {
  if (result?.content_url) {
    window.open(result.content_url, '_blank', 'noopener')
  }
}

watch(
  () => sourceSelection.value?.identity?.locator || '',
  (locator) => {
    if (locator) {
      loadSourceFacts(locator)
    }
  }
)

onMounted(async () => {
  if (route.query.task_id) {
    resultFilters.task_id = Number(route.query.task_id)
  }
  await Promise.all([loadTasks(), loadResults()])
  if (route.query.create === '1') {
    await openCreateDialog()
  }
})
</script>

<style scoped>
.model3d-quick-view {
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

@media (max-width: 768px) {
  .form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
