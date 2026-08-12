<template>
  <div class="page">
    <div class="toolbar">
      <div>
        <h2>{{ t('manager.vectorTileSet.title') }}</h2>
        <p>{{ t('manager.vectorTileSet.subtitle') }}</p>
      </div>
      <div class="commands">
        <el-button :icon="Refresh" @click="loadTasks">{{ t('common.refresh') }}</el-button>
        <el-button type="primary" :icon="Plus" @click="requestCreateDialog">{{ t('manager.vectorTileSet.create') }}</el-button>
      </div>
    </div>

    <el-table v-loading="loading" :data="tasks" row-key="id">
      <el-table-column prop="name" :label="t('manager.vectorTileSet.name')" min-width="180" />
      <el-table-column :label="t('manager.vectorTileSet.source')" min-width="260">
        <template #default="{ row }">{{ row.config?.source?.locator || '-' }}</template>
      </el-table-column>
      <el-table-column :label="t('manager.vectorTileSet.target')" min-width="220">
        <template #default="{ row }">{{ targetText(row) }}</template>
      </el-table-column>
      <el-table-column :label="t('manager.vectorTileSet.zoom')" width="100">
        <template #default="{ row }">{{ row.config?.tile?.min_zoom }}-{{ row.config?.tile?.max_zoom }}</template>
      </el-table-column>
      <el-table-column :label="t('manager.vectorTileSet.status')" width="120">
        <template #default="{ row }"><el-tag :type="statusType(row.last_execution_status)">{{ row.last_execution_status || t('manager.vectorTileSet.neverRun') }}</el-tag></template>
      </el-table-column>
      <el-table-column :label="t('manager.vectorTileSet.actions')" width="250" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" :icon="VideoPlay" :loading="executing === row.id" @click="execute(row)">{{ t('manager.vectorTileSet.execute') }}</el-button>
          <el-button link :icon="Edit" @click="requestEditTask(row)">{{ t('common.edit') }}</el-button>
          <el-button link type="danger" :icon="Delete" @click="remove(row)">{{ t('common.delete') }}</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" layout="total, prev, pager, next" @current-change="loadTasks" />

    <el-dialog v-model="dialog" :title="editing ? t('manager.vectorTileSet.edit') : t('manager.vectorTileSet.create')" width="860px" destroy-on-close @closed="clearTaskDialogRoute">
      <el-form label-position="top" :model="form">
        <div class="form-grid">
          <el-form-item :label="t('manager.vectorTileSet.name')"><el-input v-model="form.name" /></el-form-item>
          <el-form-item :label="t('manager.vectorTileSet.fileName')"><el-input v-model="form.fileName"><template #append>.pmtiles</template></el-input></el-form-item>
        </div>
        <el-form-item :label="t('manager.vectorTileSet.source')">
          <div class="picker-wrap" v-loading="sourceDetecting">
            <ResourceTreePicker v-model="sourceSelection" mode="item" :initial-locator="form.sourceLocator" :selectable-filter="isVectorTileSourceItem" tree-height="280px" />
          </div>
        </el-form-item>
        <el-descriptions v-if="sourceFacts.detected" class="source-facts" :column="4" size="small" border>
          <el-descriptions-item :label="t('manager.vectorTileSet.sourceFormat')">{{ sourceFacts.format }}</el-descriptions-item>
          <el-descriptions-item :label="t('manager.vectorTileSet.geometryColumn')">{{ form.geometryColumn }}</el-descriptions-item>
          <el-descriptions-item :label="t('manager.vectorTileSet.sourceSRID')">EPSG:{{ form.sourceSRID }}</el-descriptions-item>
          <el-descriptions-item :label="t('manager.vectorTileSet.recommendedZoom')">{{ sourceFacts.recommendedMinZoom }}-{{ sourceFacts.recommendedMaxZoom }}</el-descriptions-item>
        </el-descriptions>
        <el-form-item :label="t('manager.vectorTileSet.target')">
          <div class="picker-wrap target-picker">
            <ResourceTreePicker v-model="targetSelection" mode="node" :initial-locator="form.targetLocator" :engine-families="['object', 'file']" :engine-filter="isBusinessStorageEngine" :engine-label="''" tree-height="260px" />
          </div>
        </el-form-item>
        <div class="form-grid three">
          <el-form-item :label="t('manager.vectorTileSet.minZoom')"><el-input-number v-model="form.minZoom" :min="0" :max="form.maxZoom" /></el-form-item>
          <el-form-item :label="t('manager.vectorTileSet.maxZoom')"><el-input-number v-model="form.maxZoom" :min="form.minZoom" :max="24" /></el-form-item>
          <el-form-item :label="t('manager.vectorTileSet.layerName')"><el-input v-model="form.layerName" /></el-form-item>
        </div>
        <el-alert
          v-if="tileRangeEstimate.supported"
          :type="zoomAboveRecommendation ? 'warning' : 'info'"
          :closable="false"
          show-icon
          class="tile-cost-advice"
          :title="tileEstimateMessage"
        />
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
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
import { Delete, Edit, Plus, Refresh, VideoPlay } from '@element-plus/icons-vue'
import { ResourceTreePicker, openMonitorExecution } from '@addp/common-frontend'
import { quickViewAPI } from '../api/quickView'
import { calculateTileRangeEstimate, isZoomAboveRecommendation } from '../utils/vectorTileEstimate'
import { hasRequiredVectorTileSpatialFacts, isVectorTileSourceItem, resolveVectorTileZoomRecommendation } from '../utils/vectorTileSetResource'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const resolveRouteState = routeQuery => resolveManagerTaskWorkspaceRouteState({
  routeQuery,
  allowedQuery: ['create', 'locator', 'task_id']
})
let routeDataReady = false
let workspaceRestoreSequence = 0
const tasks = ref([]); const loading = ref(false); const page = ref(1); const pageSize = ref(20); const total = ref(0)
const dialog = ref(false); const editing = ref(null); const saving = ref(false); const executing = ref(null); const sourceDetecting = ref(false)
const sourceSelection = ref(null); const targetSelection = ref(null)
const sourceFacts = reactive({ detected: false, format: '', recommendedMinZoom: 0, recommendedMaxZoom: 0 })
const initial = () => ({ name: '', fileName: '', sourceLocator: '', sourceEngineID: 0, sourceItemID: 0, targetLocator: '', targetEngineID: 0, minZoom: 0, maxZoom: 12, layerName: '', sourceSRID: 0, extent: [], extentSRID: 0, geometryColumn: '' })
const form = reactive(initial())
let sourceDetectionSequence = 0

const tileRangeEstimate = computed(() => calculateTileRangeEstimate({
  extent: form.extent,
  extentSRID: form.extentSRID,
  minZoom: form.minZoom,
  maxZoom: form.maxZoom
}))
const zoomAboveRecommendation = computed(() => isZoomAboveRecommendation(form.maxZoom, sourceFacts.recommendedMaxZoom))
const tileEstimateMessage = computed(() => {
  const count = Number(tileRangeEstimate.value.tileCount || 0).toLocaleString()
  if (zoomAboveRecommendation.value) {
    return t('manager.vectorTileSet.tileEstimateAboveRecommendation', {
      count,
      current: form.maxZoom,
      recommended: sourceFacts.recommendedMaxZoom
    })
  }
  return t('manager.vectorTileSet.tileEstimate', { count })
})

const locator = (selection, fallback = '') => String(selection?.identity?.locator || fallback || '').trim()
const engineID = (selection, fallback = 0) => Number(selection?.identity?.engine_id || fallback || 0)
const itemID = (selection, fallback = 0) => Number(selection?.identity?.item_id || fallback || 0)
const selectedName = (selection) => String(selection?.display?.label || selection?.raw?.node?.label || '').replace(/\.[^.]+$/, '')
const isBusinessStorageEngine = (engine) => engine?.is_builtin !== true
const resetSourceFacts = () => {
  sourceFacts.detected = false
  sourceFacts.format = ''
  sourceFacts.recommendedMinZoom = 0
  sourceFacts.recommendedMaxZoom = 0
  form.sourceSRID = 0
  form.extent = []
  form.extentSRID = 0
  form.geometryColumn = ''
}
const applyCapability = (capability, selection) => {
  const quickView = capability?.quick_view || {}
  const renderFacts = capability?.render_facts || {}
  const zoom = renderFacts.zoom_recommendation || {}
  const geometryColumns = Array.isArray(quickView.geometry_columns) ? quickView.geometry_columns : []
  const geometryColumn = String(quickView.geometry_column || geometryColumns[0] || '').trim()
  const extent = Array.isArray(renderFacts.render_extent) ? renderFacts.render_extent : quickView.extent
  const sourceSRID = Number(renderFacts.source_srid || quickView.source_srid || 0)
  const extentSRID = Number(renderFacts.render_extent_srid || quickView.extent_srid || 0)
  const spatialFacts = { geometryColumn, sourceSRID, extent, extentSRID }
  if (!hasRequiredVectorTileSpatialFacts(spatialFacts, selection)) {
    throw new Error('incomplete spatial capability')
  }
  form.geometryColumn = geometryColumn
  form.sourceSRID = sourceSRID
  form.extentSRID = extentSRID
  form.extent = Array.isArray(extent) && extent.length === 4 ? extent.map(Number) : []
  const recommendation = resolveVectorTileZoomRecommendation(zoom, quickView, form.extent.length === 4)
  sourceFacts.recommendedMinZoom = recommendation.minZoom
  sourceFacts.recommendedMaxZoom = recommendation.maxZoom
  form.minZoom = sourceFacts.recommendedMinZoom
  form.maxZoom = sourceFacts.recommendedMaxZoom
  sourceFacts.format = String(selection?.resource?.format || selection?.resource?.data_type || selection?.display?.type || '-').toUpperCase()
  sourceFacts.detected = true
}
watch(sourceSelection, async (value) => {
  const sequence = ++sourceDetectionSequence
  resetSourceFacts()
  if (!value) return
  const name = selectedName(value)
  if (!form.name) form.name = name
  if (!form.fileName) form.fileName = name
  if (!form.layerName) form.layerName = name
  const selectedLocator = locator(value)
  sourceDetecting.value = true
  try {
    const capability = await quickViewAPI.getQuickViewCapabilityByLocator(selectedLocator)
    if (sequence !== sourceDetectionSequence) return
    applyCapability(capability, value)
  } catch (error) {
    if (sequence !== sourceDetectionSequence) return
    resetSourceFacts()
    ElMessage.error(t('manager.vectorTileSet.detectFailed'))
  } finally {
    if (sequence === sourceDetectionSequence) sourceDetecting.value = false
  }
})

const unwrap = (response) => response?.data?.items ? response.data : response
const loadTasks = async () => {
  loading.value = true
  try { const data = unwrap(await quickViewAPI.listVectorTileSetTasks({ page: page.value, page_size: pageSize.value })); tasks.value = data?.items || []; total.value = Number(data?.total || 0) }
  catch (error) { ElMessage.error(error?.response?.data?.error || t('manager.vectorTileSet.loadFailed')) }
  finally { loading.value = false }
}
const reset = () => { sourceDetectionSequence += 1; Object.assign(form, initial()); Object.assign(sourceFacts, { detected: false, format: '', recommendedMinZoom: 0, recommendedMaxZoom: 0 }); sourceSelection.value = null; targetSelection.value = null; editing.value = null; sourceDetecting.value = false }
const openCreate = (sourceLocator = '') => {
  reset()
  form.sourceLocator = String(sourceLocator || '').trim()
  dialog.value = true
}
const requestCreateDialog = async () => {
  const routeState = resolveRouteState({ create: '1' })
  await navigateManagerRoute(router, { path: route.path, query: routeState.query }, { history: 'push' })
}
const clearTaskDialogRoute = async () => {
  const nextQuery = { ...route.query }
  delete nextQuery.create
  delete nextQuery.locator
  delete nextQuery.task_id
  const routeState = resolveRouteState(nextQuery)
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateManagerRoute(router, location, { history: 'replace' })
  }
}
const openEdit = (task) => {
  reset(); editing.value = task
  const c = task.config || {}; const s = c.source || {}; const target = c.target || {}; const tile = c.tile || {}; const options = c.options || {}
  Object.assign(form, { name: task.name, fileName: String(target.name || '').replace(/\.pmtiles$/i, ''), sourceLocator: s.locator, sourceEngineID: s.source_engine_id, sourceItemID: s.item_id, targetLocator: target.storage_locator, targetEngineID: target.engine_id, minZoom: tile.min_zoom, maxZoom: tile.max_zoom, layerName: options.layer_name, sourceSRID: tile.source_srid || 0, extent: tile.extent || [], extentSRID: tile.extent_srid || 0, geometryColumn: options.geometry_column || '' })
  dialog.value = true
}
const requestEditTask = async (task) => {
  const routeState = resolveRouteState({ task_id: task.id })
  await navigateManagerRoute(router, { path: route.path, query: routeState.query }, { history: 'push' })
}
const payload = () => {
  const tile = { archive_format: 'pmtiles', tile_type: 'mvt', tile_matrix_set: 'WebMercatorQuad', min_zoom: form.minZoom, max_zoom: form.maxZoom, source_srid: form.sourceSRID, target_srid: 3857 }
  if (form.extent.length === 4 && form.extentSRID > 0) {
    tile.extent = form.extent
    tile.extent_srid = form.extentSRID
  }
  return { name: form.name.trim(), enabled: true, config: {
    source: { source_engine_id: engineID(sourceSelection.value, form.sourceEngineID), locator: locator(sourceSelection.value, form.sourceLocator), item_id: itemID(sourceSelection.value, form.sourceItemID) },
    target: { engine_id: engineID(targetSelection.value, form.targetEngineID), storage_locator: locator(targetSelection.value, form.targetLocator), name: `${form.fileName.trim()}.pmtiles` },
    tile,
    options: { geometry_column: form.geometryColumn, layer_name: form.layerName.trim() }
  } }
}
const save = async () => {
  const body = payload()
  const spatialFacts = { geometryColumn: form.geometryColumn, sourceSRID: form.sourceSRID, extent: form.extent, extentSRID: form.extentSRID }
  if (!body.name || !body.config.source.locator || !body.config.source.item_id || !body.config.target.storage_locator || !form.fileName.trim() || !form.layerName.trim() || !hasRequiredVectorTileSpatialFacts(spatialFacts, sourceSelection.value)) { ElMessage.warning(t('manager.vectorTileSet.required')); return }
  saving.value = true
  try { editing.value ? await quickViewAPI.updateVectorTileSetTask(editing.value.id, body) : await quickViewAPI.createVectorTileSetTask(body); dialog.value = false; ElMessage.success(t('manager.vectorTileSet.saved')); await loadTasks() }
  catch (error) { ElMessage.error(error?.response?.data?.error || t('manager.vectorTileSet.saveFailed')) }
  finally { saving.value = false }
}
const execute = async (task) => { executing.value = task.id; try { const data = await quickViewAPI.executeVectorTileSetTask(task.id); ElMessage.success(t('manager.vectorTileSet.submitted')); if (data?.execution_id) await openMonitorExecution(data.execution_id); await loadTasks() } catch (error) { ElMessage.error(error?.response?.data?.error || t('manager.vectorTileSet.executeFailed')) } finally { executing.value = null } }
const remove = async (task) => { await ElMessageBox.confirm(t('manager.vectorTileSet.deleteConfirm'), t('common.delete'), { type: 'warning' }); await quickViewAPI.deleteVectorTileSetTask(task.id); await loadTasks() }
const targetText = (task) => `${task.config?.target?.storage_locator || ''}/${task.config?.target?.name || ''}`
const statusType = (status) => ({ success: 'success', failed: 'danger', running: 'warning', pending: 'info' }[status] || 'info')
async function restoreWorkspaceFromRoute() {
  const restoreSequence = ++workspaceRestoreSequence
  const routeState = resolveRouteState(route.query)
  if (routeState.changed) {
    await navigateManagerRoute(router, { path: route.path, query: routeState.query }, { history: 'replace' })
    return
  }
  if (!routeDataReady) return

  const taskID = Number(routeState.query.task_id || 0)
  if (taskID) {
    try {
      const task = await quickViewAPI.getVectorTileSetTask(taskID)
      if (restoreSequence !== workspaceRestoreSequence) return
      openEdit(task?.data?.data || task?.data || task)
    } catch (error) {
      if (restoreSequence !== workspaceRestoreSequence) return
      dialog.value = false
      ElMessage.error(error?.response?.data?.error || t('manager.vectorTileSet.loadFailed'))
      await clearTaskDialogRoute()
      return
    }
  } else if (routeState.query.create === '1') {
    openCreate(routeState.query.locator)
  } else {
    dialog.value = false
  }
  await loadTasks()
}

watch(() => route.query, restoreWorkspaceFromRoute)

onMounted(async () => {
  await restoreWorkspaceFromRoute()
  routeDataReady = true
  await restoreWorkspaceFromRoute()
})
</script>

<style scoped>
.page { min-height: 100%; padding: 20px; color: var(--addp-text-primary); background: var(--addp-bg-primary); }
.toolbar { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 18px; }
h2 { margin: 0 0 6px; font-size: 22px; letter-spacing: 0; } p { margin: 0; color: var(--addp-text-secondary); }
.commands { display: flex; gap: 8px; }.el-pagination { justify-content: flex-end; margin-top: 16px; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }.form-grid.three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.picker-wrap { width: 100%; min-width: 0; }.source-facts { margin: -4px 0 18px; }
.tile-cost-advice { margin-top: 4px; }
@media (max-width: 760px) { .toolbar { flex-direction: column; }.form-grid,.form-grid.three { grid-template-columns: 1fr; } }
</style>
