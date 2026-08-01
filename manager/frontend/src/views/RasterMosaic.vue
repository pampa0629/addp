<template>
  <div class="raster-mosaic">
    <el-card>
          <div class="tab-toolbar">
            <div class="toolbar-tip">
              <span class="toolbar-tip-text">{{ t('manager.rasterMosaic.subtitle') }}</span>
              <el-tooltip
                :content="t('manager.rasterMosaic.workflowDescription')"
                placement="bottom"
                :show-after="300"
              >
                <el-icon class="inline-tip-icon"><InfoFilled /></el-icon>
              </el-tooltip>
            </div>
            <el-button type="primary" @click="requestCreateDialog">{{ t('manager.rasterMosaic.create') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column :label="t('manager.rasterMosaic.name')" min-width="190" show-overflow-tooltip>
              <template #default="{ row }">{{ displayText(row.name) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterMosaic.mode')" width="120">
              <template #default="{ row }">{{ placementModeLabel(taskPlacementMode(row)) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterMosaic.source')" min-width="240" show-overflow-tooltip>
              <template #default="{ row }">{{ taskSourceText(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterMosaic.target')" min-width="240" show-overflow-tooltip>
              <template #default="{ row }">{{ taskTargetText(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterMosaic.datasetName')" min-width="150" show-overflow-tooltip>
              <template #default="{ row }">{{ taskDatasetName(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterMosaic.enabled')" width="100">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.rasterMosaic.enabledYes') : t('manager.rasterMosaic.enabledNo') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterMosaic.lastExecutionStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(lastExecutionStatus(row))">
                  {{ executionStatusLabel(lastExecutionStatus(row)) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterMosaic.lastRunAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.last_run_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.rasterMosaic.actions')" width="300" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" @click="requestEditTask(row)">{{ t('manager.rasterMosaic.edit') }}</el-button>
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.rasterMosaic.execute') }}
                  </el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                    {{ t('manager.rasterMosaic.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteTask(row)">{{ t('manager.rasterMosaic.delete') }}</el-button>
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
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="editingTask ? t('manager.rasterMosaic.editTitle') : t('manager.rasterMosaic.createTitle')"
      width="820px"
      destroy-on-close
      @closed="clearTaskDialogRoute"
    >
      <el-form label-position="top" :model="form">
        <div class="form-grid">
          <el-form-item :label="t('manager.rasterMosaic.name')">
            <el-input v-model="form.name" :placeholder="t('manager.rasterMosaic.namePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('manager.rasterMosaic.enabled')">
            <el-switch
              v-model="form.enabled"
              :active-text="t('manager.rasterMosaic.enabledYes')"
              :inactive-text="t('manager.rasterMosaic.enabledNo')"
            />
          </el-form-item>
        </div>

        <el-form-item :label="t('manager.rasterMosaic.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>

        <el-divider content-position="left">{{ t('manager.rasterMosaic.sourceScope') }}</el-divider>
        <ResourceTreePicker
          v-model="sourceSelection"
          mode="node"
          :initial-locator="sourceInitialLocator"
          :title="t('manager.rasterMosaic.sourceTreeTitle')"
          :engine-label="t('manager.rasterMosaic.engine')"
          :engine-placeholder="t('manager.rasterMosaic.enginePlaceholder')"
          :search-placeholder="t('manager.rasterMosaic.searchPlaceholder')"
          :search-all-engines-placeholder="t('manager.rasterMosaic.searchAllEnginesPlaceholder')"
          :search-empty-text="t('manager.rasterMosaic.searchEmptyText')"
          tree-height="300px"
        />

        <el-divider content-position="left">{{ t('manager.rasterMosaic.placement') }}</el-divider>
        <el-form-item :label="t('manager.rasterMosaic.mode')">
          <el-radio-group v-model="form.mode">
            <el-radio-button label="detached">{{ t('manager.rasterMosaic.modes.detached') }}</el-radio-button>
            <el-radio-button label="in_place">{{ t('manager.rasterMosaic.modes.inPlace') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-alert
          class="mode-alert"
          :title="modeHint"
          type="info"
          :closable="false"
          show-icon
        />

        <template v-if="form.mode === 'detached'">
          <el-form-item class="target-picker" :label="t('manager.rasterMosaic.targetScope')">
            <ResourceTreePicker
              v-model="targetSelection"
              mode="node"
              :initial-locator="targetInitialLocator"
              :title="t('manager.rasterMosaic.targetTreeTitle')"
              :engine-label="t('manager.rasterMosaic.engine')"
              :engine-placeholder="t('manager.rasterMosaic.enginePlaceholder')"
              :search-placeholder="t('manager.rasterMosaic.searchPlaceholder')"
              :search-all-engines-placeholder="t('manager.rasterMosaic.searchAllEnginesPlaceholder')"
              :search-empty-text="t('manager.rasterMosaic.searchEmptyText')"
              tree-height="300px"
            />
          </el-form-item>
        </template>

        <div class="form-grid">
          <el-form-item :label="t('manager.rasterMosaic.datasetName')">
            <el-input v-model="form.datasetName" :placeholder="t('manager.rasterMosaic.datasetNamePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('manager.rasterMosaic.recursive')">
            <el-switch
              v-model="form.recursive"
              :active-text="t('manager.rasterMosaic.enabledYes')"
              :inactive-text="t('manager.rasterMosaic.enabledNo')"
            />
          </el-form-item>
        </div>

        <el-collapse class="advanced-options">
          <el-collapse-item :title="t('manager.rasterMosaic.advancedOptions')" name="advanced">
            <div class="form-grid three-columns">
              <el-form-item :label="t('manager.rasterMosaic.compression')">
                <el-select v-model="form.compression">
                  <el-option label="DEFLATE" value="DEFLATE" />
                  <el-option label="LZW" value="LZW" />
                  <el-option label="ZSTD" value="ZSTD" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('manager.rasterMosaic.blockSize')">
                <el-input-number v-model="form.blocksize" :min="128" :max="1024" :step="128" />
              </el-form-item>
              <el-form-item :label="t('manager.rasterMosaic.overviewResampling')">
                <el-select v-model="form.overviewResampling">
                  <el-option label="NEAREST" value="NEAREST" />
                  <el-option label="AVERAGE" value="AVERAGE" />
                  <el-option label="BILINEAR" value="BILINEAR" />
                </el-select>
              </el-form-item>
            </div>
          </el-collapse-item>
        </el-collapse>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('manager.rasterMosaic.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTask">{{ t('manager.rasterMosaic.save') }}</el-button>
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
import { ResourceTreePicker, openMonitorExecution } from '@addp/common-frontend'
import { quickViewAPI } from '../api/quickView'
import { useQuickViewResourceDisplay } from '../composables/useQuickViewResourceDisplay'
import { formatDateTime } from '../utils/formatters'
import { rasterCOGExecutionStatusTagType, rasterCOGLastExecutionStatus } from '../utils/rasterCOGDisplay'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { displayText, loadQuickViewEngines, resourceLabel } = useQuickViewResourceDisplay(t)

const resolveRouteState = routeQuery => resolveManagerTaskWorkspaceRouteState({
  routeQuery,
  allowedQuery: ['create', 'task_id']
})
let routeDataReady = false
let workspaceRestoreSequence = 0

const tasks = ref([])
const tasksLoading = ref(false)
const tasksPage = ref(1)
const tasksPageSize = ref(20)
const tasksTotal = ref(0)
const executingId = ref(null)
const saving = ref(false)
const dialogVisible = ref(false)
const editingTask = ref(null)
const sourceSelection = ref(null)
const targetSelection = ref(null)
const sourceInitialLocator = ref('')
const targetInitialLocator = ref('')

const defaultForm = () => ({
  name: '',
  description: '',
  enabled: true,
  mode: 'detached',
  sourceLocator: '',
  sourceEngineId: 0,
  targetLocator: '',
  targetEngineId: 0,
  datasetName: '',
  recursive: true,
  compression: 'DEFLATE',
  blocksize: 512,
  overviewResampling: 'NEAREST'
})

const form = reactive(defaultForm())

const modeHint = computed(() => (
  form.mode === 'in_place'
    ? t('manager.rasterMosaic.inPlaceHint')
    : t('manager.rasterMosaic.detachedHint')
))

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

const resetForm = () => {
  Object.assign(form, defaultForm())
  sourceSelection.value = null
  targetSelection.value = null
  sourceInitialLocator.value = ''
  targetInitialLocator.value = ''
  editingTask.value = null
}

const taskPlacementMode = (task) => task?.placement?.mode || task?.config?.placement?.mode || ''
const taskSourceLocator = (task) => task?.source?.node_locator || task?.config?.source?.node_locator || ''
const taskTargetLocator = (task) => task?.target?.storage_locator || task?.config?.target?.storage_locator || ''
const taskSourceEngineID = (task) => task?.source?.source_engine_id || task?.config?.source?.source_engine_id || 0
const taskTargetEngineID = (task) => task?.target?.target_engine_id || task?.config?.target?.target_engine_id || 0
const taskDatasetName = (task) => task?.target?.dataset_name || task?.config?.target?.dataset_name || '-'
const taskSourceText = (task) => resourceLabel(taskSourceEngineID(task), taskSourceLocator(task))
const taskTargetText = (task) => resourceLabel(taskTargetEngineID(task), taskTargetLocator(task))
const lastExecutionStatus = (task) => rasterCOGLastExecutionStatus(task)
const executionStatusTagType = (status) => rasterCOGExecutionStatusTagType(status)

const selectionLocator = (selection, fallback) => String(selection?.identity?.locator || fallback || '').trim()
const selectionEngineID = (selection, fallback) => {
  const value = Number(selection?.identity?.engine_id || fallback || 0)
  return Number.isFinite(value) && value > 0 ? value : 0
}

const selectionEngineType = (selection) => String(
  selection?.display?.engine_type ||
  selection?.raw?.engine?.engine_type ||
  ''
).trim().toLowerCase()

const isObjectStoreSelection = (selection) => ['minio', 's3'].includes(selectionEngineType(selection))

const placementModeLabel = (mode) => {
  const value = String(mode || '').trim()
  if (value === 'in_place') return t('manager.rasterMosaic.modes.inPlace')
  if (value === 'detached') return t('manager.rasterMosaic.modes.detached')
  return '-'
}

const executionStatusLabel = (status) => {
  const key = String(status || '').trim().toLowerCase()
  if (!key) return t('manager.rasterMosaic.statusNeverRun')
  return t(`manager.rasterMosaic.status.${key}`, key)
}

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const response = await quickViewAPI.listRasterMosaicTasks({
      page: tasksPage.value,
      page_size: tasksPageSize.value
    })
    const list = unwrapList(response)
    tasks.value = list.items
    tasksTotal.value = list.total
  } catch (error) {
    console.error('加载 raster mosaic 任务失败:', error)
    ElMessage.error(errorMessage(error, t('manager.rasterMosaic.loadTasksFailed')))
  } finally {
    tasksLoading.value = false
  }
}

const openCreateDialog = () => {
  resetForm()
  dialogVisible.value = true
}

const requestCreateDialog = async () => {
  const routeState = resolveRouteState({ create: '1' })
  await navigateManagerRoute(router, {
    path: route.path,
    query: routeState.query
  }, { history: 'push' })
}

const clearTaskDialogRoute = async () => {
  const nextQuery = { ...route.query }
  delete nextQuery.create
  delete nextQuery.task_id
  const routeState = resolveRouteState(nextQuery)
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateManagerRoute(router, location, { history: 'replace' })
  }
}

const openEditDialog = (task) => {
  resetForm()
  editingTask.value = task
  const source = task?.source || task?.config?.source || {}
  const target = task?.target || task?.config?.target || {}
  const placement = task?.placement || task?.config?.placement || {}
  const cog = task?.config?.cog || {}
  Object.assign(form, {
    name: task?.name || '',
    description: task?.description || '',
    enabled: Boolean(task?.enabled),
    mode: placement.mode || 'detached',
    sourceLocator: source.node_locator || '',
    sourceEngineId: Number(source.source_engine_id || 0),
    targetLocator: target.storage_locator || '',
    targetEngineId: Number(target.target_engine_id || 0),
    datasetName: target.dataset_name || '',
    recursive: source.recursive !== false,
    compression: cog.compression || 'DEFLATE',
    blocksize: Number(cog.blocksize || 512),
    overviewResampling: cog.overview_resampling || 'NEAREST'
  })
  sourceInitialLocator.value = form.sourceLocator
  targetInitialLocator.value = form.targetLocator
  dialogVisible.value = true
}

const requestEditTask = async (task) => {
  const routeState = resolveRouteState({ task_id: task.id })
  await navigateManagerRoute(router, {
    path: route.path,
    query: routeState.query
  }, { history: 'push' })
}

const validateForm = () => {
  const sourceLocator = selectionLocator(sourceSelection.value, form.sourceLocator)
  const sourceEngineId = selectionEngineID(sourceSelection.value, form.sourceEngineId)
  const targetLocator = form.mode === 'in_place'
    ? sourceLocator
    : selectionLocator(targetSelection.value, form.targetLocator)
  const targetEngineId = form.mode === 'in_place'
    ? sourceEngineId
    : selectionEngineID(targetSelection.value, form.targetEngineId)

  if (!String(form.name || '').trim()) {
    ElMessage.warning(t('manager.rasterMosaic.nameRequired'))
    return null
  }
  if (!sourceLocator || !sourceEngineId) {
    ElMessage.warning(t('manager.rasterMosaic.sourceRequired'))
    return null
  }
  if (form.mode === 'detached' && (!targetLocator || !targetEngineId)) {
    ElMessage.warning(t('manager.rasterMosaic.targetRequired'))
    return null
  }
  if (form.mode === 'detached' && sourceLocator === targetLocator) {
    ElMessage.warning(t('manager.rasterMosaic.targetMustDiffer'))
    return null
  }
  if (form.mode === 'in_place' && isObjectStoreSelection(sourceSelection.value)) {
    ElMessage.warning(t('manager.rasterMosaic.inPlaceObjectStoreUnsupported'))
    return null
  }
  if (!String(form.datasetName || '').trim()) {
    ElMessage.warning(t('manager.rasterMosaic.datasetNameRequired'))
    return null
  }

  return { sourceLocator, sourceEngineId, targetLocator, targetEngineId }
}

const buildPayload = (validated) => ({
  name: String(form.name || '').trim(),
  description: String(form.description || '').trim(),
  enabled: Boolean(form.enabled),
  config: {
    source: {
      node_locator: validated.sourceLocator,
      source_engine_id: validated.sourceEngineId,
      recursive: Boolean(form.recursive),
      include_patterns: ['*.tif', '*.tiff'],
      exclude_patterns: []
    },
    placement: {
      mode: form.mode
    },
    target: {
      storage_locator: validated.targetLocator,
      target_engine_id: validated.targetEngineId,
      dataset_name: String(form.datasetName || '').trim()
    },
    cog: {
      compression: form.compression,
      blocksize: Number(form.blocksize || 512),
      overview_resampling: form.overviewResampling,
      validate_source_cog: true
    },
    overview: {
      enabled: true,
      max_pixels: 64000000,
      resampling: 'AVERAGE'
    },
    tiles: {
      enabled: false,
      min_zoom: 0,
      max_zoom: 0,
      format: 'webp'
    }
  }
})

const saveTask = async () => {
  const validated = validateForm()
  if (!validated) return
  saving.value = true
  try {
    const payload = buildPayload(validated)
    if (editingTask.value) {
      await quickViewAPI.updateRasterMosaicTask(editingTask.value.id, payload)
      ElMessage.success(t('manager.rasterMosaic.updateSuccess'))
    } else {
      await quickViewAPI.createRasterMosaicTask(payload)
      ElMessage.success(t('manager.rasterMosaic.createSuccess'))
    }
    dialogVisible.value = false
    await loadTasks()
  } catch (error) {
    console.error('保存 raster mosaic 任务失败:', error)
    ElMessage.error(errorMessage(error, t('manager.rasterMosaic.saveFailed')))
  } finally {
    saving.value = false
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await quickViewAPI.executeRasterMosaicTask(task.id)
    const executionID = response?.execution_id || response?.data?.execution_id
    ElMessage.success(t('manager.rasterMosaic.executeSubmitted'))
    await loadTasks()
    if (executionID) {
      await openMonitorExecution(executionID)
    }
  } catch (error) {
    console.error('执行 raster mosaic 任务失败:', error)
    ElMessage.error(errorMessage(error, t('manager.rasterMosaic.executeFailed')))
  } finally {
    executingId.value = null
  }
}

const deleteTask = async (task) => {
  await ElMessageBox.confirm(t('manager.rasterMosaic.deleteTaskConfirm'), t('manager.rasterMosaic.delete'), { type: 'warning' })
  await quickViewAPI.deleteRasterMosaicTask(task.id)
  ElMessage.success(t('manager.rasterMosaic.deleteSuccess'))
  await loadTasks()
}

const openTaskExecution = (task) => openMonitorExecution(task.last_execution_id)

const handleTasksSizeChange = () => {
  tasksPage.value = 1
  loadTasks()
}

async function restoreWorkspaceFromRoute() {
  const restoreSequence = ++workspaceRestoreSequence
  const routeState = resolveRouteState(route.query)
  if (routeState.changed) {
    await navigateManagerRoute(router, {
      path: route.path,
      query: routeState.query
    }, { history: 'replace' })
    return
  }
  if (!routeDataReady) return

  const taskID = Number(routeState.query.task_id || 0)
  if (taskID) {
    try {
      const response = await quickViewAPI.getRasterMosaicTask(taskID)
      if (restoreSequence !== workspaceRestoreSequence) return
      openEditDialog(response?.data?.data || response?.data || response)
    } catch (error) {
      if (restoreSequence !== workspaceRestoreSequence) return
      dialogVisible.value = false
      ElMessage.error(errorMessage(error, t('manager.rasterMosaic.loadTasksFailed')))
      await clearTaskDialogRoute()
      return
    }
  } else if (routeState.query.create === '1') {
    openCreateDialog()
  } else {
    dialogVisible.value = false
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
.raster-mosaic {
  height: 100%;
}

.tab-toolbar {
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

.inline-tip-icon {
  color: var(--el-color-info);
  cursor: help;
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

.form-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 180px;
  gap: 16px;
}

.form-grid.three-columns {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.mode-alert,
.target-picker,
.advanced-options {
  margin-bottom: 18px;
}

@media (max-width: 768px) {
  .form-grid,
  .form-grid.three-columns {
    grid-template-columns: 1fr;
  }
}
</style>
