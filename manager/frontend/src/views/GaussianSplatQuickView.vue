<template>
  <div class="gaussian-splat-quick-view">
    <el-card>
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.gaussianSplatQuickView.tasksTab')" name="tasks">
          <div class="tab-toolbar">
            <div class="toolbar-tip">
              <span class="toolbar-tip-text">{{ t('manager.gaussianSplatQuickView.subtitle') }}</span>
              <el-tooltip
                :content="t('manager.gaussianSplatQuickView.workflowDescription')"
                placement="bottom"
                :show-after="300"
              >
                <el-icon class="inline-tip-icon"><InfoFilled /></el-icon>
              </el-tooltip>
            </div>
            <el-button type="primary" @click="openCreateDialog">{{ t('manager.gaussianSplatQuickView.create') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column prop="name" :label="t('manager.gaussianSplatQuickView.name')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.gaussianSplatQuickView.engine')" width="120">
              <template #default="{ row }">{{ source(row).source_engine_id || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.gaussianSplatQuickView.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ source(row).item_locator || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.gaussianSplatQuickView.sourceFormat')" width="110">
              <template #default="{ row }">{{ formatLabel(source(row).format) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.gaussianSplatQuickView.sourceSize')" width="120">
              <template #default="{ row }">{{ formatBytes(source(row).source_size_bytes) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.gaussianSplatQuickView.enabled')" width="90">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.gaussianSplatQuickView.enabledYes') : t('manager.gaussianSplatQuickView.enabledNo') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.gaussianSplatQuickView.lastExecutionStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(lastExecutionStatus(row))">
                  {{ executionStatusLabel(lastExecutionStatus(row)) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.gaussianSplatQuickView.lastRunAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.last_run_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.gaussianSplatQuickView.actions')" width="360" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.gaussianSplatQuickView.execute') }}
                  </el-button>
                  <el-button size="small" @click="viewTaskResults(row)">{{ t('manager.gaussianSplatQuickView.results') }}</el-button>
                  <el-button size="small" @click="editTask(row)">{{ t('manager.gaussianSplatQuickView.edit') }}</el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                    {{ t('manager.gaussianSplatQuickView.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteTask(row)">{{ t('manager.gaussianSplatQuickView.delete') }}</el-button>
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

        <el-tab-pane :label="t('manager.gaussianSplatQuickView.resultsTab')" name="results">
          <div class="filter-bar">
            <el-tag v-if="resultTaskFilterLabel" type="primary" closable @close="clearResultTaskFilter">
              {{ resultTaskFilterLabel }}
            </el-tag>
            <el-select v-model="resultFilters.status" class="status-filter" clearable :placeholder="t('manager.gaussianSplatQuickView.resultStatus')">
              <el-option v-for="status in resultStatuses" :key="status" :label="resultStatusLabel(status)" :value="status" />
            </el-select>
            <el-input
              v-model="resultFilters.q"
              class="keyword-filter"
              clearable
              :placeholder="t('manager.gaussianSplatQuickView.keywordPlaceholder')"
              @keyup.enter="applyResultFilters"
            />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.gaussianSplatQuickView.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.gaussianSplatQuickView.reset') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadResults" />
          </div>

          <el-table :data="results" v-loading="resultsLoading" stripe>
            <el-table-column :label="t('manager.gaussianSplatQuickView.engine')" width="120">
              <template #default="{ row }">{{ row.source_engine_id || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.gaussianSplatQuickView.source')" min-width="300" show-overflow-tooltip>
              <template #default="{ row }">{{ row.locator || '-' }}</template>
            </el-table-column>
            <el-table-column prop="file_name" :label="t('manager.gaussianSplatQuickView.fileName')" min-width="150" show-overflow-tooltip />
            <el-table-column :label="t('manager.gaussianSplatQuickView.resultStatus')" width="120">
              <template #default="{ row }">
                <el-tag :type="resultStatusTagType(row.status)">
                  {{ resultStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.gaussianSplatQuickView.diagnostics')" width="170">
              <template #default="{ row }">
                <el-tag v-if="diagnosticSummary(row)" type="info" effect="plain">
                  {{ diagnosticSummary(row) }}
                </el-tag>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.gaussianSplatQuickView.size')" width="120">
              <template #default="{ row }">{{ formatBytes(row.size_bytes) }}</template>
            </el-table-column>
            <el-table-column prop="error_message" :label="t('manager.gaussianSplatQuickView.error')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.gaussianSplatQuickView.updatedAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.gaussianSplatQuickView.actions')" width="360" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" :disabled="row.status !== 'ready' || !row.locator" @click="openSourcePreview(row)">
                    {{ t('manager.gaussianSplatQuickView.previewKSplat') }}
                  </el-button>
                  <el-button size="small" :disabled="!hasDiagnostics(row)" @click="openDiagnostics(row)">
                    {{ t('manager.gaussianSplatQuickView.diagnostics') }}
                  </el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openResultExecution(row)">
                    {{ t('manager.gaussianSplatQuickView.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteResult(row)">{{ t('manager.gaussianSplatQuickView.delete') }}</el-button>
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
      :title="editingTask ? t('manager.gaussianSplatQuickView.editTitle') : t('manager.gaussianSplatQuickView.createTitle')"
      width="820px"
      destroy-on-close
    >
      <el-form label-position="top" :model="form">
        <div class="form-grid">
          <el-form-item :label="t('manager.gaussianSplatQuickView.name')">
            <el-input v-model="form.name" :placeholder="t('manager.gaussianSplatQuickView.namePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('manager.gaussianSplatQuickView.enabled')">
            <el-switch
              v-model="form.enabled"
              :active-text="t('manager.gaussianSplatQuickView.enabledYes')"
              :inactive-text="t('manager.gaussianSplatQuickView.enabledNo')"
            />
          </el-form-item>
        </div>

        <el-divider content-position="left">{{ t('manager.gaussianSplatQuickView.sourceScope') }}</el-divider>
        <ResourceTreePicker
          v-model="sourceSelection"
          mode="item"
          :initial-locator="sourceInitialLocator"
          :title="t('manager.gaussianSplatQuickView.sourceTreeTitle')"
          :engine-label="t('manager.gaussianSplatQuickView.engine')"
          :engine-placeholder="t('manager.gaussianSplatQuickView.enginePlaceholder')"
          :search-placeholder="t('manager.gaussianSplatQuickView.searchPlaceholder')"
          :search-all-engines-placeholder="t('manager.gaussianSplatQuickView.searchAllEnginesPlaceholder')"
          :search-empty-text="t('manager.gaussianSplatQuickView.searchEmptyText')"
          tree-height="300px"
        />

        <el-descriptions class="source-facts" :column="2" border size="small">
          <el-descriptions-item :label="t('manager.gaussianSplatQuickView.sourceLocator')">
            {{ sourceFactText(form.source.item_locator) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.gaussianSplatQuickView.sourceEngineID')">
            {{ form.source.source_engine_id || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.gaussianSplatQuickView.itemFingerprint')">
            {{ form.source.item_fingerprint || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.gaussianSplatQuickView.sourceFormat')">
            {{ formatLabel(form.source.format) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.gaussianSplatQuickView.sourceSize')">
            {{ formatBytes(form.source.source_size_bytes) }}
          </el-descriptions-item>
        </el-descriptions>

        <el-form-item :label="t('manager.gaussianSplatQuickView.fileName')">
          <el-input v-model="form.result.file_name" :placeholder="t('manager.gaussianSplatQuickView.fileNamePlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="taskDialogVisible = false">{{ t('manager.gaussianSplatQuickView.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTask">{{ t('manager.gaussianSplatQuickView.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="diagnosticsDialogVisible"
      :title="t('manager.gaussianSplatQuickView.diagnosticsTitle')"
      width="760px"
      destroy-on-close
    >
      <template v-if="selectedDiagnosticResult">
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item :label="t('manager.gaussianSplatQuickView.sourceFormat')">
            {{ diagnosticValue(diagnosticSourceFormat(selectedDiagnosticResult)) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.gaussianSplatQuickView.workflowOperator')">
            {{ diagnosticValue(workflowRuntime(selectedDiagnosticResult).operator) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.gaussianSplatQuickView.workflowExecutionTime')">
            {{ diagnosticExecutionTime(selectedDiagnosticResult) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.gaussianSplatQuickView.ksplatRef')">
            {{ diagnosticValue(artifactFacts(selectedDiagnosticResult).object || selectedDiagnosticResult.storage_ref) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.gaussianSplatQuickView.ksplatSize')">
            {{ formatBytes(ksplatFacts(selectedDiagnosticResult).size_bytes || selectedDiagnosticResult.size_bytes) }}
          </el-descriptions-item>
        </el-descriptions>
        <el-divider content-position="left">{{ t('manager.gaussianSplatQuickView.contentInspection') }}</el-divider>
        <div v-if="inspectionLoading" class="inspection-loading">
          {{ t('manager.gaussianSplatQuickView.inspectLoading') }}
        </div>
        <el-alert
          v-else-if="inspectionError"
          type="error"
          :closable="false"
          :title="inspectionError"
          show-icon
        />
        <template v-else-if="selectedInspection">
          <el-descriptions :column="2" border size="small">
            <el-descriptions-item :label="t('manager.gaussianSplatQuickView.inspectStatus')">
              <el-tag :type="inspectionStatusTagType(selectedInspection.summary?.status)">
                {{ diagnosticValue(selectedInspection.summary?.message) }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item :label="t('manager.gaussianSplatQuickView.splatCount')">
              {{ numberText(selectedInspection.header?.splat_count) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('manager.gaussianSplatQuickView.kplatVersion')">
              {{ kplatVersionText(selectedInspection.header) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('manager.gaussianSplatQuickView.compressionLevel')">
              {{ diagnosticValue(selectedInspection.header?.compression_level) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('manager.gaussianSplatQuickView.objectSize')">
              {{ formatBytes(selectedInspection.object_size_bytes) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('manager.gaussianSplatQuickView.bytesInspected')">
              {{ numberText(selectedInspection.bytes_inspected) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('manager.gaussianSplatQuickView.objectContentType')">
              {{ diagnosticValue(selectedInspection.object_content_type || selectedInspection.expected_content_type) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('manager.gaussianSplatQuickView.headerSignature')">
              {{ diagnosticValue(selectedInspection.header_signature_hex) }}
            </el-descriptions-item>
            <el-descriptions-item :label="t('manager.gaussianSplatQuickView.sceneCenter')" :span="2">
              {{ sceneCenterText(selectedInspection.header?.scene_center) }}
            </el-descriptions-item>
          </el-descriptions>
          <el-table class="inspection-checks" :data="selectedInspection.checks || []" size="small" border>
            <el-table-column prop="name" :label="t('manager.gaussianSplatQuickView.inspectCheck')" width="170" />
            <el-table-column :label="t('manager.gaussianSplatQuickView.inspectCheckStatus')" width="100">
              <template #default="{ row }">
                <el-tag :type="inspectionStatusTagType(row.status)" size="small">{{ row.status }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="message" :label="t('manager.gaussianSplatQuickView.inspectCheckMessage')" min-width="260" show-overflow-tooltip />
          </el-table>
        </template>
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
const diagnosticsDialogVisible = ref(false)
const selectedDiagnosticResult = ref(null)
const selectedInspection = ref(null)
const inspectionLoading = ref(false)
const inspectionError = ref('')
const resultStatuses = ['building', 'ready', 'failed', 'deleted']
const resultFilters = reactive({ task_id: undefined, status: '', q: '' })

const form = reactive(defaultForm())

const resultTaskFilterLabel = computed(() => {
  if (!selectedResultTask.value) return ''
  return selectedResultTask.value.name || t('manager.gaussianSplatQuickView.taskWithId', { id: selectedResultTask.value.id })
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
      format: 'ksplat',
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
const ksplatFacts = (result) => objectValue(resultMetadata(result).ksplat_facts)
const workflowRuntime = (result) => objectValue(resultMetadata(result).workflow_runtime)
const sourceFacts = (result) => objectValue(resultMetadata(result).source)
const artifactFacts = (result) => objectValue(resultMetadata(result).artifact)

const hasDiagnostics = (result) => {
  return result?.status === 'ready' ||
    Object.keys(ksplatFacts(result)).length > 0 ||
    Object.keys(workflowRuntime(result)).length > 0 ||
    Object.keys(artifactFacts(result)).length > 0
}

const diagnosticSummary = (result) => {
  if (!hasDiagnostics(result)) return ''
  if (workflowRuntime(result).operator) return String(workflowRuntime(result).operator)
  if (result?.status === 'ready') return t('manager.gaussianSplatQuickView.contentInspection')
  return t('manager.gaussianSplatQuickView.diagnosticsAvailable')
}

const diagnosticValue = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  return String(value)
}

const diagnosticSourceFormat = (result) => sourceFacts(result).format || result?.source_format || ''

const diagnosticExecutionTime = (result) => {
  const value = workflowRuntime(result).execution_time_ms
  const numberValue = Number(value)
  if (!Number.isFinite(numberValue) || numberValue <= 0) return '-'
  return t('manager.gaussianSplatQuickView.executionTimeMs', { ms: numberValue })
}

const numberText = (value) => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue.toLocaleString() : '-'
}

const kplatVersionText = (header) => {
  if (!header) return '-'
  return `${diagnosticValue(header.version_major)}.${diagnosticValue(header.version_minor)}`
}

const sceneCenterText = (value) => {
  if (!Array.isArray(value) || value.length < 3) return '-'
  return value.slice(0, 3).map((part) => {
    const numberValue = Number(part)
    return Number.isFinite(numberValue) ? numberValue.toFixed(4) : '-'
  }).join(', ')
}

const inspectionStatusTagType = (status) => {
  const value = String(status || '').toLowerCase()
  if (value === 'ok') return 'success'
  if (value === 'failed') return 'danger'
  return 'warning'
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

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const response = await quickViewAPI.listGaussianSplatQuickViewTasks({ page: tasksPage.value, page_size: tasksPageSize.value })
    const { items, total } = unwrapList(response)
    tasks.value = items
    tasksTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.gaussianSplatQuickView.loadTasksFailed')))
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
    const response = await quickViewAPI.listGaussianSplatQuickViews(params)
    const { items, total } = unwrapList(response)
    results.value = items
    resultsTotal.value = total
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.gaussianSplatQuickView.loadResultsFailed')))
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
  form.source.format = gaussianSplatQuickViewSourceFormat(route.query.format || form.source.format)
  sourceInitialLocator.value = locator
  const sourceName = String(route.query.name || '').replace(/\s*-\s*.*/, '').trim()
  form.name = t('manager.gaussianSplatQuickView.createFromSourceName', { name: sourceName || sourceFactText(locator) })
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
    const gaussianSplat = capability?.gaussian_splat || {}
    form.source.source_engine_id = Number(capability?.source_engine_id || form.source.source_engine_id || 0)
    form.source.item_fingerprint = String(capability?.item_fingerprint || form.source.item_fingerprint || '').trim()
    form.source.source_size_bytes = Number(gaussianSplat.size_bytes || form.source.source_size_bytes || 0)
    form.source.format = gaussianSplatQuickViewSourceFormat(gaussianSplat.format || form.source.format)
    if (!gaussianSplatQuickViewSourceFormat(form.source.format)) {
      ElMessage.warning(t('manager.gaussianSplatQuickView.sourceFormatRequired'))
    }
  } catch (error) {
    ElMessage.warning(errorMessage(error, t('manager.gaussianSplatQuickView.loadSourceFactsFailed')))
  }
}

const validateForm = () => {
  const sourceLocator = selectionLocator(sourceSelection.value, form.source.item_locator)
  const sourceEngineID = selectionEngineID(sourceSelection.value, form.source.source_engine_id)
  if (!String(form.name || '').trim()) {
    ElMessage.warning(t('manager.gaussianSplatQuickView.nameRequired'))
    return null
  }
  if (!sourceLocator || !sourceEngineID) {
    ElMessage.warning(t('manager.gaussianSplatQuickView.sourceRequired'))
    return null
  }
  if (!String(form.source.item_fingerprint || '').trim()) {
    ElMessage.warning(t('manager.gaussianSplatQuickView.itemFingerprintRequired'))
    return null
  }
  if (!gaussianSplatQuickViewSourceFormat(form.source.format)) {
    ElMessage.warning(t('manager.gaussianSplatQuickView.sourceFormatRequired'))
    return null
  }
  return { sourceLocator, sourceEngineID }
}

const taskPayload = (validated) => ({
  name: String(form.name || '').trim() || t('manager.gaussianSplatQuickView.defaultTaskName'),
  enabled: Boolean(form.enabled),
  config: {
    source: {
      item_locator: validated.sourceLocator,
      source_engine_id: validated.sourceEngineID,
      item_fingerprint: String(form.source.item_fingerprint || '').trim(),
      item_id: Number(form.source.item_id || 0),
      format: gaussianSplatQuickViewSourceFormat(form.source.format),
      source_size_bytes: Number(form.source.source_size_bytes || 0)
    },
    result: {
      file_name: String(form.result.file_name || '').trim()
    }
  }
})

const gaussianSplatQuickViewSourceFormat = (value) => {
  const sourceFormat = String(value || '').trim().toLowerCase()
  return ['ply', 'splat', 'ksplat'].includes(sourceFormat) ? sourceFormat : ''
}

const saveTask = async () => {
  const validated = validateForm()
  if (!validated) return
  saving.value = true
  try {
    const payload = taskPayload(validated)
    if (editingTask.value) {
      await quickViewAPI.updateGaussianSplatQuickViewTask(editingTask.value.id, payload)
    } else {
      await quickViewAPI.createGaussianSplatQuickViewTask(payload)
    }
    taskDialogVisible.value = false
    await loadTasks()
    ElMessage.success(t('manager.gaussianSplatQuickView.saveSuccess'))
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.gaussianSplatQuickView.saveFailed')))
  } finally {
    saving.value = false
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await quickViewAPI.executeGaussianSplatQuickViewTask(task.id)
    const executionID = response?.execution_id || response?.data?.execution_id
    ElMessage.success(t('manager.gaussianSplatQuickView.executeSubmitted'))
    await loadTasks()
    if (executionID) {
      await openMonitorExecution(executionID)
    }
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.gaussianSplatQuickView.executeFailed')))
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

const deleteTask = async (task) => {
  await ElMessageBox.confirm(t('manager.gaussianSplatQuickView.deleteTaskConfirm'), t('manager.gaussianSplatQuickView.delete'), { type: 'warning' })
  await quickViewAPI.deleteGaussianSplatQuickViewTask(task.id)
  ElMessage.success(t('manager.gaussianSplatQuickView.deleteSuccess'))
  await loadTasks()
}

const deleteResult = async (result) => {
  await ElMessageBox.confirm(t('manager.gaussianSplatQuickView.deleteResultConfirm'), t('manager.gaussianSplatQuickView.delete'), { type: 'warning' })
  await quickViewAPI.deleteGaussianSplatQuickView(result.id)
  await loadResults()
}

const openTaskExecution = (task) => openMonitorExecution(task.last_execution_id)
const openResultExecution = (result) => openMonitorExecution(result.last_execution_id)

const openDiagnostics = async (result) => {
  selectedDiagnosticResult.value = result
  selectedInspection.value = null
  inspectionError.value = ''
  diagnosticsDialogVisible.value = true
  if (!result?.id || result.status !== 'ready') return
  inspectionLoading.value = true
  try {
    selectedInspection.value = unwrapPayload(await quickViewAPI.inspectGaussianSplatQuickView(result.id))
  } catch (error) {
    inspectionError.value = errorMessage(error, t('manager.gaussianSplatQuickView.inspectFailed'))
  } finally {
    inspectionLoading.value = false
  }
}

const openSourcePreview = (result) => {
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
    const response = await quickViewAPI.getGaussianSplatQuickViewTask(taskID)
    editTask(unwrapPayload(response))
  } catch (error) {
    ElMessage.error(errorMessage(error, t('manager.gaussianSplatQuickView.loadTaskFailed')))
  }
}

const loadResultTaskFilterFromRoute = async () => {
  const taskID = Number(route.query.task_id || 0)
  if (!taskID) return
  resultFilters.task_id = taskID
  try {
    selectedResultTask.value = unwrapPayload(await quickViewAPI.getGaussianSplatQuickViewTask(taskID))
  } catch {
    selectedResultTask.value = null
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
  if (activeTab.value === 'results') {
    await loadResultTaskFilterFromRoute()
  }
  await Promise.all([loadTasks(), loadResults()])
  if (route.query.create === '1') {
    await openCreateDialog()
    return
  }
  await openTaskFromQuery()
})
</script>

<style scoped>
.gaussian-splat-quick-view {
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

.inspection-loading {
  padding: 16px 0;
  color: var(--el-text-color-secondary);
}

.inspection-checks {
  margin-top: 12px;
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
