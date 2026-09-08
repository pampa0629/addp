<template>
  <div class="derived-tasks page-container">
    <div class="page-header">
      <div>
        <h2>{{ t('manager.derivedTasks.title') }}</h2>
        <p>{{ t('manager.derivedTasks.description') }}</p>
      </div>
      <div class="header-actions">
        <el-button @click="loadTasks"><el-icon><Refresh /></el-icon>{{ t('manager.derivedTasks.refresh') }}</el-button>
        <el-button v-if="category === 'managed_quick_view'" type="primary" @click="openDataExplorer"><el-icon><Plus /></el-icon>{{ t('manager.derivedTasks.create') }}</el-button>
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
      <el-table-column prop="name" :label="t('manager.derivedTasks.columns.name')" min-width="220" show-overflow-tooltip />
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
      <el-table-column :label="t('manager.derivedTasks.columns.actions')" width="340" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="showDetail(row)">{{ t('manager.derivedTasks.detail') }}</el-button>
          <el-button v-if="isSpatialBusinessTask(row)" link @click="beginEdit(row)">{{ t('manager.derivedTasks.edit') }}</el-button>
          <el-button link type="primary" :disabled="!row.enabled" @click="execute(row)">{{ t('manager.derivedTasks.execute') }}</el-button>
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
      <el-descriptions v-if="selectedTask" :column="1" border>
        <el-descriptions-item :label="t('manager.derivedTasks.columns.type')">{{ taskTypeLabel(selectedTask.task_type) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.derivedTasks.version')">{{ selectedTask.version }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.derivedTasks.columns.status')">{{ statusLabel(selectedTask.last_execution_status) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.derivedTasks.semanticKey')">{{ selectedTask.semantic_key || '-' }}</el-descriptions-item>
      </el-descriptions>
      <h3>{{ t('manager.derivedTasks.config') }}</h3>
      <pre class="config-view">{{ JSON.stringify(selectedTask?.config || {}, null, 2) }}</pre>
    </el-drawer>

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
import { navigateConsoleModuleRoute } from '@common-ui'
import { deleteDerivedTask, executeDerivedTask, getDerivedTask, listDerivedTasks } from '../api/derivedTasks'
import { navigateManagerRoute } from '../utils/moduleNavigation'
import VectorTileSetTaskEditor from '../components/tasks/VectorTileSetTaskEditor.vue'
import RasterMosaicTaskEditor from '../components/tasks/RasterMosaicTaskEditor.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
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

const taskTypes = {
  managed_quick_view: [
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
function sourceSection(task) { return task?.config?.source || task?.config?.target || {} }
function sourceEngineLabel(task) {
  const engineID = sourceSection(task).source_engine_id || sourceSection(task).engine_id
  return engineID ? `#${engineID}` : '-'
}

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
async function execute(row, overwrite = false) {
  try {
    await executeDerivedTask(row.task_type, row.id, overwrite ? { existing_result_action: 'overwrite' } : {})
    ElMessage.success(t('manager.derivedTasks.executeQueued'))
    await loadTasks()
  } catch (error) {
    if (!overwrite && error?.response?.status === 409) {
      await ElMessageBox.confirm(t('manager.common.currentResultOverwriteConfirm'), t('manager.common.currentResultOverwriteTitle'), { type: 'warning' })
      return execute(row, true)
    }
    ElMessage.error(error?.response?.data?.error || t('manager.derivedTasks.executeFailed'))
  }
}
async function remove(row) {
  await ElMessageBox.confirm(t('manager.derivedTasks.deleteConfirm'), t('manager.derivedTasks.delete'), { type: 'warning' })
  try { await deleteDerivedTask(row.task_type, row.id); ElMessage.success(t('manager.derivedTasks.deleteSuccess')); await loadTasks() }
  catch (error) { ElMessage.error(error?.response?.data?.error || t('manager.derivedTasks.deleteFailed')) }
}
function openDataExplorer() { navigateManagerRoute(router, { path: '/data-explorer' }) }
function openMonitor(row) { navigateConsoleModuleRoute(router, 'monitor', { path: '/executions', query: { execution_id: row.last_execution_id } }) }
function isSpatialBusinessTask(row) { return categoryForTaskType(row?.task_type) === 'spatial_business' }
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
  if (!route.query.create && !route.query.locator) return
  await syncRoute()
}
async function handleEditorSaved() { await loadTasks() }
function openEditorFromRoute() {
  if (route.query.create !== '1' || categoryForTaskType(taskType.value) !== 'spatial_business') return
  editorType.value = taskType.value
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
  await loadTasks()
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
.config-view { padding: 16px; overflow: auto; border-radius: 6px; background: var(--el-fill-color-light); color: var(--el-text-color-primary); }
</style>
