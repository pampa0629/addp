<template>
  <div class="derived-tasks page-container">
    <div class="page-header">
      <div>
        <h2>{{ t('manager.derivedTasks.title') }}</h2>
        <p>{{ t('manager.derivedTasks.description') }}</p>
      </div>
      <div class="header-actions">
        <el-button @click="loadTasks"><el-icon><Refresh /></el-icon>{{ t('manager.derivedTasks.refresh') }}</el-button>
        <el-button v-if="category === 'managed_quick_view'" type="primary" @click="beginQuickViewCreate"><el-icon><Plus /></el-icon>{{ t('manager.derivedTasks.createQuickView') }}</el-button>
        <el-dropdown v-else split-button type="primary" @click="beginCreate(defaultSpatialTaskType)" @command="beginCreate">
          <el-icon><Plus /></el-icon>{{ t('manager.derivedTasks.createSpatial') }}
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item v-for="option in taskTypeOptions" :key="option.value" :command="option.value">{{ t(option.label) }}</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </div>

    <el-tabs v-model="category" @tab-change="changeCategory">
      <el-tab-pane :label="t('manager.derivedTasks.categories.managedQuickView')" name="managed_quick_view" />
      <el-tab-pane :label="t('manager.derivedTasks.categories.spatialBusiness')" name="spatial_business" />
    </el-tabs>

    <div class="toolbar">
      <el-select v-model="taskType" clearable :placeholder="t('manager.derivedTasks.allTypes')" @change="changeFilter">
        <el-option v-for="option in taskTypeOptions" :key="option.value" :label="t(option.label)" :value="option.value" />
      </el-select>
      <span class="summary">{{ t('manager.derivedTasks.total', { total }) }}</span>
    </div>

    <el-table v-loading="loading" :data="tasks" stripe>
      <el-table-column :label="t('manager.derivedTasks.columns.name')" min-width="220" show-overflow-tooltip>
        <template #default="{ row }"><el-button link type="primary" @click="showDetail(row)">{{ row.name }}</el-button></template>
      </el-table-column>
      <el-table-column :label="t('manager.derivedTasks.columns.type')" min-width="210">
        <template #default="{ row }">{{ taskTypeLabel(row.task_type) }}</template>
      </el-table-column>
      <el-table-column :label="t('manager.derivedTasks.columns.engine')" min-width="150">
        <template #default="{ row }">{{ sourceEngineLabel(row) }}</template>
      </el-table-column>
      <el-table-column :label="t('manager.derivedTasks.columns.enabled')" width="90">
        <template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? t('manager.derivedTasks.enabled') : t('manager.derivedTasks.disabled') }}</el-tag></template>
      </el-table-column>
      <el-table-column :label="t('manager.derivedTasks.columns.status')" width="130">
        <template #default="{ row }"><el-tag :type="statusType(row.last_execution_status)">{{ statusLabel(row.last_execution_status) }}</el-tag></template>
      </el-table-column>
      <el-table-column :label="t('manager.derivedTasks.columns.updatedAt')" min-width="170">
        <template #default="{ row }">{{ formatTime(row.updated_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('manager.derivedTasks.columns.actions')" width="420" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="showDetail(row)">{{ t('manager.derivedTasks.detail') }}</el-button>
          <el-button v-if="sourceLocator(row)" link @click="openSource(row)">{{ t('manager.derivedTasks.source') }}</el-button>
          <el-button v-if="isManagedQuickViewTask(row)" link type="primary" :loading="viewingTaskID === row.id" @click="viewTaskResult(row)">{{ t('manager.derivedTasks.result') }}</el-button>
          <el-button v-if="isSpatialBusinessTask(row)" link @click="beginEdit(row)">{{ t('manager.derivedTasks.edit') }}</el-button>
          <el-button link type="primary" :loading="executingTaskID === row.id" :disabled="!row.enabled || isExecuting(row)" @click="execute(row)">{{ t('manager.derivedTasks.execute') }}</el-button>
          <el-button v-if="row.last_execution_id" link @click="openMonitor(row)">{{ t('manager.derivedTasks.monitor') }}</el-button>
          <el-button link type="danger" @click="remove(row)">{{ t('manager.derivedTasks.delete') }}</el-button>
        </template>
      </el-table-column>
      <template #empty><el-empty :description="t('manager.derivedTasks.empty')" /></template>
    </el-table>

    <div class="pagination">
      <el-pagination v-model:current-page="page" v-model:page-size="pageSize" background layout="total, sizes, prev, pager, next" :total="total" :page-sizes="[20, 50, 100]" @current-change="loadTasks" @size-change="changePageSize" />
    </div>

    <el-drawer v-model="detailVisible" :title="selectedTask?.name || t('manager.derivedTasks.detail')" size="560px" @closed="clearTaskDetailRoute">
      <div v-if="selectedTask" class="detail-actions">
        <el-button v-if="sourceLocator(selectedTask)" type="primary" plain @click="openSource(selectedTask)">{{ t('manager.derivedTasks.viewSource') }}</el-button>
        <el-button v-if="isManagedQuickViewTask(selectedTask) && selectedTask.has_current_result" type="primary" :loading="viewingTaskID === selectedTask.id" @click="viewTaskResult(selectedTask)">{{ t('manager.derivedTasks.viewResult') }}</el-button>
        <el-button :loading="executingTaskID === selectedTask.id" :disabled="!selectedTask.enabled || isExecuting(selectedTask)" @click="execute(selectedTask)">{{ t('manager.derivedTasks.execute') }}</el-button>
        <el-button v-if="selectedTask.last_execution_id" @click="openMonitor(selectedTask)">{{ t('manager.derivedTasks.monitor') }}</el-button>
      </div>
      <el-descriptions v-if="selectedTask" :column="1" border>
        <el-descriptions-item :label="t('manager.derivedTasks.columns.type')">{{ taskTypeLabel(selectedTask.task_type) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.derivedTasks.columns.status')">{{ statusLabel(selectedTask.last_execution_status) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.derivedTasks.columns.enabled')">{{ selectedTask.enabled ? t('manager.derivedTasks.enabled') : t('manager.derivedTasks.disabled') }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.derivedTasks.columns.engine')">{{ sourceEngineLabel(selectedTask) }}</el-descriptions-item>
        <el-descriptions-item v-if="sourceLocator(selectedTask)" :label="t('manager.derivedTasks.sourceItem')">
          <el-button link type="primary" class="source-link" @click="openSource(selectedTask)">{{ sourcePath(selectedTask) }}</el-button>
        </el-descriptions-item>
        <el-descriptions-item v-if="sourceFormat(selectedTask)" :label="t('manager.derivedTasks.sourceFormat')">{{ sourceFormat(selectedTask) }}</el-descriptions-item>
        <el-descriptions-item v-if="sourceSize(selectedTask) !== null" :label="t('manager.derivedTasks.sourceSize')">{{ formatBytes(sourceSize(selectedTask)) }}</el-descriptions-item>
        <el-descriptions-item v-if="targetEngineLabel(selectedTask)" :label="t('manager.derivedTasks.targetEngine')">{{ targetEngineLabel(selectedTask) }}</el-descriptions-item>
        <el-descriptions-item v-if="resultName(selectedTask)" :label="t('manager.derivedTasks.resultName')">{{ resultName(selectedTask) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.derivedTasks.lastRunAt')">{{ formatTime(selectedTask.last_run_at) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.derivedTasks.columns.updatedAt')">{{ formatTime(selectedTask.updated_at) }}</el-descriptions-item>
        <el-descriptions-item v-if="selectedTask.description" :label="t('manager.derivedTasks.taskDescription')">{{ selectedTask.description }}</el-descriptions-item>
      </el-descriptions>
    </el-drawer>

    <QuickViewTaskCreator
      v-if="editorType === 'managed_quick_view'"
      v-model="editorVisible"
      :task-type="taskType"
      :locator="editorLocator"
      @created="handleQuickViewCreated"
      @closed="clearEditorRoute"
    />

    <VectorTileSetTaskEditor
      v-if="editorType === 'vector_tile_set_generation'"
      v-model="editorVisible"
      :task="editingTask"
      :locator="editorLocator"
      @saved="handleEditorSaved"
      @closed="clearEditorRoute"
    />
    <RasterMosaicTaskEditor
      v-if="editorType === 'raster_mosaic_generation'"
      v-model="editorVisible"
      :task="editingTask"
      :locator="editorLocator"
      @saved="handleEditorSaved"
      @closed="clearEditorRoute"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { formatBytes, openMonitorExecution } from '@addp/common-frontend'
import { deleteDerivedTask, executeDerivedTask, getDerivedTask, listDerivedTasks } from '../api/derivedTasks'
import { useCurrentResultConfirmation } from '../composables/useCurrentResultConfirmation'
import { useQuickViewResourceDisplay } from '../composables/useQuickViewResourceDisplay'
import {
  derivedTaskResultName,
  derivedTaskSourceEngineID,
  derivedTaskSourceFormat,
  derivedTaskSourceLocator,
  derivedTaskSourceSize,
  derivedTaskTargetEngineID
} from '../utils/derivedTaskPresentation'
import { navigateManagerRoute } from '../utils/moduleNavigation'
import { openQuickViewResult } from '../utils/quickViewResultNavigation'
import QuickViewTaskCreator from '../components/tasks/QuickViewTaskCreator.vue'
import VectorTileSetTaskEditor from '../components/tasks/VectorTileSetTaskEditor.vue'
import RasterMosaicTaskEditor from '../components/tasks/RasterMosaicTaskEditor.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const confirmCurrentResult = useCurrentResultConfirmation()
const { engineName, loadQuickViewEngines, resourcePath } = useQuickViewResourceDisplay(t)
const routeTaskType = typeof route.query.task_type === 'string' ? route.query.task_type : ''
const tasks = ref([])
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const detailVisible = ref(false)
const selectedTask = ref(null)
const editorVisible = ref(false)
const editorType = ref('')
const editorLocator = ref('')
const editingTask = ref(null)
const executingTaskID = ref(0)
const viewingTaskID = ref(0)

const taskTypes = {
  managed_quick_view: [
    ['pptx_pdf_generation', 'manager.derivedTasks.types.pptxPDF'],
    ['vector_materialized_view_generation', 'manager.derivedTasks.types.vectorMaterializedView'],
    ['vector_tile_cache_generation', 'manager.derivedTasks.types.vectorTileCache'],
    ['raster_cog_generation', 'manager.derivedTasks.types.rasterCOG'],
    ['model_3d_glb_generation', 'manager.derivedTasks.types.model3DGLB'],
    ['model3d_tiles_generation', 'manager.derivedTasks.types.model3DTiles'],
    ['gaussian_splat_ksplat_generation', 'manager.derivedTasks.types.gaussianSplat'],
    ['point_cloud_copc_generation', 'manager.derivedTasks.types.pointCloudCOPC']
  ],
  spatial_business: [
    ['vector_tile_set_generation', 'manager.derivedTasks.types.vectorTileSet'],
    ['raster_mosaic_generation', 'manager.derivedTasks.types.rasterMosaic']
  ]
}
function categoryForTaskType(value) {
  return Object.entries(taskTypes).find(([, types]) => types.some(([type]) => type === value))?.[0] || ''
}
const category = ref(categoryForTaskType(routeTaskType) || (route.query.category === 'spatial_business' ? 'spatial_business' : 'managed_quick_view'))
const taskType = ref(routeTaskType)
const taskTypeOptions = computed(() => (taskTypes[category.value] || []).map(([value, label]) => ({ value, label })))
const defaultSpatialTaskType = computed(() => taskType.value && categoryForTaskType(taskType.value) === 'spatial_business' ? taskType.value : 'vector_tile_set_generation')

function payload(response) { return response?.data ?? response }
function taskTypeLabel(value) {
  const found = Object.values(taskTypes).flat().find(([type]) => type === value)
  return found ? t(found[1]) : value
}
function statusLabel(value) { return value ? t(`manager.derivedTasks.status.${value}`, value) : t('manager.derivedTasks.status.never') }
function statusType(value) {
  if (value === 'success') return 'success'
  if (value === 'failed') return 'danger'
  if (value === 'pending' || value === 'running') return 'warning'
  return 'info'
}
function formatTime(value) { return value ? new Date(value).toLocaleString() : '-' }
function sourceLocator(task) { return derivedTaskSourceLocator(task) }
function sourceEngineLabel(task) { return engineName(derivedTaskSourceEngineID(task)) }
function targetEngineLabel(task) {
  const id = derivedTaskTargetEngineID(task)
  return id ? engineName(id) : ''
}
function sourcePath(task) { return resourcePath(sourceLocator(task)) }
function sourceFormat(task) { return derivedTaskSourceFormat(task).toUpperCase() }
function sourceSize(task) { return derivedTaskSourceSize(task) }
function resultName(task) { return derivedTaskResultName(task) }
function isExecuting(task) { return ['pending', 'running'].includes(task?.last_execution_status) }
function isManagedQuickViewTask(task) { return categoryForTaskType(task?.task_type) === 'managed_quick_view' }

async function syncRoute(extra = {}, history = 'replace') {
  const query = { category: category.value }
  if (taskType.value) query.task_type = taskType.value
  Object.assign(query, extra)
  await navigateManagerRoute(router, { path: '/derived-tasks', query }, { history })
}
async function loadTasks() {
  loading.value = true
  try {
    const response = payload(await listDerivedTasks({ category: category.value, task_type: taskType.value || undefined, page: page.value, page_size: pageSize.value }))
    tasks.value = response?.items || []
    total.value = Number(response?.total || 0)
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('manager.derivedTasks.loadFailed'))
  } finally { loading.value = false }
}
async function changeCategory() { taskType.value = ''; page.value = 1; detailVisible.value = false; editorVisible.value = false; await syncRoute(); await loadTasks() }
async function changeFilter() { page.value = 1; detailVisible.value = false; editorVisible.value = false; await syncRoute(); await loadTasks() }
async function changePageSize() { page.value = 1; await loadTasks() }
async function showDetail(row, updateRoute = true) {
  try {
    selectedTask.value = { ...row, ...payload(await getDerivedTask(row.task_type, row.id)) }
    detailVisible.value = true
    if (updateRoute) await syncRoute({ task_id: String(row.id) }, 'push')
  }
  catch (error) { ElMessage.error(error?.response?.data?.error || t('manager.derivedTasks.loadFailed')) }
}
async function openTaskFromRoute() {
  const id = Number(route.query.task_id)
  if (!taskType.value || !Number.isInteger(id) || id <= 0) return
  await showDetail({ task_type: taskType.value, id }, false)
}
async function execute(row) {
  executingTaskID.value = row.id
  try {
    const detail = payload(await getDerivedTask(row.task_type, row.id))
    await confirmCurrentResult(
      request => executeDerivedTask(row.task_type, row.id, request?.parameters || {}),
      { hasCurrentResult: Boolean(detail?.has_current_result) }
    )
    ElMessage.success(t('manager.derivedTasks.executeQueued'))
    await loadTasks()
  } catch (error) {
    if (error === 'cancel' || error === 'close') return
    ElMessage.error(error?.response?.data?.error || t('manager.derivedTasks.executeFailed'))
  } finally { executingTaskID.value = 0 }
}
async function remove(row) {
  await ElMessageBox.confirm(t('manager.derivedTasks.deleteConfirm'), t('manager.derivedTasks.delete'), { type: 'warning' })
  try { await deleteDerivedTask(row.task_type, row.id); ElMessage.success(t('manager.derivedTasks.deleteSuccess')); await loadTasks() }
  catch (error) { ElMessage.error(error?.response?.data?.error || t('manager.derivedTasks.deleteFailed')) }
}
function openSource(row) { navigateManagerRoute(router, { path: '/data-explorer', query: { locator: sourceLocator(row) } }, { history: 'push' }) }
async function viewTaskResult(row) {
  viewingTaskID.value = row.id
  try {
    const detail = row.has_current_result === true
      ? row
      : payload(await getDerivedTask(row.task_type, row.id))
    if (!detail?.has_current_result) {
      ElMessage.warning(t('manager.derivedTasks.resultUnavailable'))
      return
    }
    const locator = sourceLocator(detail)
    if (!locator) {
      ElMessage.warning(t('manager.derivedTasks.resultUnavailable'))
      return
    }
    await openQuickViewResult(router, locator)
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || error?.message || t('manager.derivedTasks.viewResultFailed'))
  } finally {
    viewingTaskID.value = 0
  }
}
async function openMonitor(row) { await openMonitorExecution(row.last_execution_id) }
function isSpatialBusinessTask(row) { return categoryForTaskType(row?.task_type) === 'spatial_business' }
async function beginQuickViewCreate() {
  category.value = 'managed_quick_view'
  editorType.value = 'managed_quick_view'
  editorLocator.value = String(route.query.locator || '')
  editingTask.value = null
  editorVisible.value = true
  await syncRoute({ create: '1', ...(editorLocator.value ? { locator: editorLocator.value } : {}) }, 'push')
}
async function beginCreate(type) {
  category.value = 'spatial_business'
  taskType.value = type
  editorType.value = type
  editorLocator.value = String(route.query.locator || '')
  editingTask.value = null
  editorVisible.value = true
  await syncRoute({ create: '1', ...(editorLocator.value ? { locator: editorLocator.value } : {}) }, 'push')
}
async function beginEdit(row) {
  try {
    editingTask.value = { ...row, ...payload(await getDerivedTask(row.task_type, row.id)) }
    editorType.value = row.task_type
    editorLocator.value = ''
    editorVisible.value = true
  } catch (error) { ElMessage.error(error?.response?.data?.error || t('manager.derivedTasks.loadFailed')) }
}
async function clearTaskDetailRoute() {
  if (!route.query.task_id) return
  await syncRoute()
}
async function clearEditorRoute() {
  editingTask.value = null
  editorLocator.value = ''
  editorType.value = ''
  if (!route.query.create && !route.query.locator) return
  await syncRoute()
}
async function handleEditorSaved() { await loadTasks() }
async function handleQuickViewCreated(created) {
  if (created?.taskType) taskType.value = created.taskType
  page.value = 1
  await loadTasks()
}
function openEditorFromRoute() {
  if (route.query.create !== '1') return
  if (category.value === 'managed_quick_view') editorType.value = 'managed_quick_view'
  else if (categoryForTaskType(taskType.value) === 'spatial_business') editorType.value = taskType.value
  else return
  editorLocator.value = typeof route.query.locator === 'string' ? route.query.locator : ''
  editingTask.value = null
  editorVisible.value = true
}

watch(() => route.query, async (query) => {
  const nextType = typeof query.task_type === 'string' ? query.task_type : ''
  const nextCategory = categoryForTaskType(nextType) || (query.category === 'spatial_business' ? 'spatial_business' : 'managed_quick_view')
  if (nextCategory !== category.value || nextType !== taskType.value) {
    category.value = nextCategory; taskType.value = nextType; page.value = 1; await loadTasks()
  }
  if (query.task_id) await openTaskFromRoute()
  else if (query.create === '1') openEditorFromRoute()
})
onMounted(async () => {
  const extra = {}
  if (route.query.task_id) extra.task_id = route.query.task_id
  if (route.query.create === '1') extra.create = '1'
  if (typeof route.query.locator === 'string' && route.query.locator) extra.locator = route.query.locator
  await syncRoute(extra)
  await Promise.all([loadQuickViewEngines(), loadTasks()])
  if (extra.task_id) await openTaskFromRoute()
  else if (extra.create) openEditorFromRoute()
})
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 24px; margin-bottom: 8px; }
.page-header h2 { margin: 0 0 8px; }
.page-header p { margin: 0; color: var(--el-text-color-secondary); }
.header-actions, .toolbar { display: flex; align-items: center; gap: 12px; }
.toolbar { margin: 12px 0 16px; }
.toolbar .el-select { width: 280px; }
.summary { color: var(--el-text-color-secondary); }
.pagination { display: flex; justify-content: flex-end; margin-top: 20px; }
.detail-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 16px; }
.source-link { height: auto; padding: 0; white-space: normal; text-align: left; }
</style>
