<template>
  <div class="vector-materialized-view">
    <el-card>
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.vectorMaterializedView.tasksTab')" name="tasks">
          <div class="tab-toolbar task-tab-toolbar">
            <div class="toolbar-tip">
              <span class="toolbar-tip-text">{{ t('manager.vectorMaterializedView.subtitle') }}</span>
              <el-tooltip
                :content="t('manager.vectorMaterializedView.workflowDescription')"
                placement="bottom"
                :show-after="300"
              >
                <el-icon class="inline-tip-icon"><InfoFilled /></el-icon>
              </el-tooltip>
            </div>
            <el-button type="primary" :icon="Plus" @click="requestCreateDialog">
              {{ t('manager.vectorMaterializedView.create') }}
            </el-button>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column :label="t('manager.vectorMaterializedView.name')" min-width="180" show-overflow-tooltip>
              <template #default="{ row }">{{ displayText(row.name) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.engine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(row.target?.source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.resource')" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">{{ taskResource(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.geometryColumn')" width="150" show-overflow-tooltip>
              <template #default="{ row }">{{ row.geometry?.geometry_column || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.srid')" width="120">
              <template #default="{ row }">
                {{ row.geometry?.source_srid || '-' }} -> {{ row.geometry?.target_srid || 3857 }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.enabled')" width="100">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.vectorMaterializedView.enabledYes') : t('manager.vectorMaterializedView.enabledNo') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.lastExecutionStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(lastExecutionStatus(row))">
                  {{ executionStatusLabel(lastExecutionStatus(row)) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.lastRunAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.last_run_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.actions')" width="430" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.vectorMaterializedView.execute') }}
                  </el-button>
                  <el-button size="small" @click="requestEditTask(row)">{{ t('manager.vectorMaterializedView.edit') }}</el-button>
                  <el-button size="small" @click="viewTaskResults(row)">{{ t('manager.vectorMaterializedView.results') }}</el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                    {{ t('manager.vectorMaterializedView.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteTask(row)">{{ t('manager.vectorMaterializedView.delete') }}</el-button>
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

        <el-tab-pane :label="t('manager.vectorMaterializedView.resultsTab')" name="results">
          <div class="filter-bar">
            <div v-if="resultTaskFilterLabel" class="task-filter-chip">
              <el-tag type="primary" closable @close="clearResultTaskFilter">
                {{ resultTaskFilterLabel }}
              </el-tag>
            </div>
            <el-select v-model="resultFilters.status" clearable :placeholder="t('manager.vectorMaterializedView.resultStatus')">
              <el-option v-for="status in resultStatuses" :key="status" :label="resultStatusLabel(status)" :value="status" />
            </el-select>
            <el-input v-model="resultFilters.q" clearable :placeholder="t('manager.vectorMaterializedView.keywordPlaceholder')" @keyup.enter="applyResultFilters" />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.vectorMaterializedView.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.vectorMaterializedView.reset') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadResults" />
          </div>

          <el-table :data="results" v-loading="resultsLoading" stripe>
            <el-table-column :label="t('manager.vectorMaterializedView.engine')" min-width="150" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(row.source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.sourceDataPath')" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">{{ sourceDataPath(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.target')" min-width="240" show-overflow-tooltip>
              <template #default="{ row }">{{ targetPath(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.resultStatus')" width="150">
              <template #default="{ row }">
                <div class="result-status-cell">
                  <el-tag :type="optimizationResultStatusTagType(row.status)">
                    {{ resultStatusLabel(row.status) }}
                  </el-tag>
                  <el-tooltip
                    v-if="row.status === 'stale'"
                    :content="t('manager.vectorMaterializedView.staleResultHint')"
                    placement="bottom"
                    :show-after="300"
                  >
                    <el-icon class="result-status-tip"><InfoFilled /></el-icon>
                  </el-tooltip>
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.rowCountEstimate')" width="130">
              <template #default="{ row }">{{ row.row_count_estimate ?? '-' }}</template>
            </el-table-column>
            <el-table-column prop="error_message" :label="t('manager.vectorMaterializedView.error')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.vectorMaterializedView.updatedAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorMaterializedView.actions')" width="380" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openResultExecution(row)">
                    {{ t('manager.vectorMaterializedView.monitor') }}
                  </el-button>
                  <el-button
                    v-if="resultAction(row).visible"
                    size="small"
                    type="warning"
                    :loading="executingResultId === row.id"
                    @click="refreshStaleResult(row)"
                  >
                    {{ t(resultAction(row).labelKey) }}
                  </el-button>
                  <el-button size="small" :disabled="row.status !== 'ready'" @click="openTileCacheCreate(row)">
                    {{ t('manager.vectorMaterializedView.generateTileCache') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteResult(row)">{{ t('manager.vectorMaterializedView.delete') }}</el-button>
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

    <el-dialog v-model="formDialogVisible" :title="formTitle" width="860px" @closed="clearFormDialogRoute">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="128px" v-loading="capabilityLoading">
        <div class="form-section-title">{{ t('manager.vectorMaterializedView.basicInfo') }}</div>
        <el-form-item :label="t('manager.vectorMaterializedView.name')" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorMaterializedView.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorMaterializedView.enabled')">
          <el-switch v-model="form.enabled" />
        </el-form-item>

        <div class="form-section-title">{{ t('manager.vectorMaterializedView.sourceData') }}</div>
        <el-descriptions v-if="!showResourcePicker" class="source-summary" :column="2" border>
          <el-descriptions-item :label="t('manager.vectorMaterializedView.engine')">
            {{ engineName(form.config.target.source_engine_id) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.vectorMaterializedView.resource')">
            {{ sourceResourceText }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.vectorMaterializedView.sourceSrid')">
            {{ form.config.geometry.source_srid || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.vectorMaterializedView.targetSrid')">
            {{ form.config.geometry.target_srid || 3857 }}
          </el-descriptions-item>
        </el-descriptions>
        <template v-else>
          <el-form-item :label="t('manager.vectorMaterializedView.sourceTable')" prop="config.target.locator">
            <div class="resource-picker">
              <el-alert
                v-if="sourceSelectionLocked"
                type="info"
                :title="t('manager.vectorMaterializedView.sourceLockedTitle')"
                :closable="false"
                show-icon
                class="compact-tip-alert"
              >
                <template #default>
                  <el-tooltip
                    :content="t('manager.vectorMaterializedView.sourceLockedHint')"
                    placement="bottom"
                    :show-after="300"
                  >
                    <el-icon class="inline-tip-icon"><InfoFilled /></el-icon>
                  </el-tooltip>
                </template>
              </el-alert>
              <ResourceTreePicker
                v-model="resourceSelection"
                mode="item"
                :initial-locator="form.config.target.locator"
                :engine-filter="isSupportedDatabaseEngine"
                :selectable-filter="isSelectableSpatialTable"
                :show-count="false"
                :show-selection-summary="false"
                :title="t('manager.vectorMaterializedView.resourceTreeTitle')"
                tree-height="320px"
                @select="handleResourceSelection"
                @node-click="handleResourceNodeClick"
              />
              <div v-if="form.config.target.locator" class="selected-resource">
                <div class="selected-resource__main">
                  <el-tag size="small" type="success">{{ t('manager.vectorMaterializedView.spatialTable') }}</el-tag>
                  <span class="selected-resource__name">{{ sourceResourceText }}</span>
                </div>
                <div class="selected-resource__meta">{{ selectedResourceSummary }}</div>
              </div>
            </div>
          </el-form-item>
        </template>

        <div class="form-section-title">{{ t('manager.vectorMaterializedView.optimizationSettings') }}</div>
        <el-alert
          v-if="sourceAlready3857"
          type="info"
          :closable="false"
          show-icon
          class="optimization-advice"
          :title="t('manager.vectorMaterializedView.sourceAlready3857Title')"
        >
          <template #default>
            <el-tooltip
              :content="t('manager.vectorMaterializedView.sourceAlready3857')"
              placement="bottom"
              :show-after="300"
            >
              <el-icon class="inline-tip-icon"><InfoFilled /></el-icon>
            </el-tooltip>
          </template>
        </el-alert>
        <el-form-item :label="t('manager.vectorMaterializedView.geometryColumn')" prop="config.geometry.geometry_column">
          <el-select v-model="form.config.geometry.geometry_column" filterable :placeholder="t('manager.vectorMaterializedView.geometryColumnRequired')">
            <el-option v-for="column in geometryColumnOptions" :key="column" :label="column" :value="column" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('manager.vectorMaterializedView.targetKind')">
          <el-tag type="info">source_schema_materialized_view</el-tag>
        </el-form-item>
        <el-form-item :label="t('manager.vectorMaterializedView.targetSrid')">
          <el-tag type="info">3857</el-tag>
        </el-form-item>
        <el-form-item :label="t('manager.vectorMaterializedView.analyzeAfterBuild')">
          <el-switch v-model="form.config.optimization.analyze_after_build" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formDialogVisible = false">{{ t('manager.vectorMaterializedView.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="sourceAlready3857" @click="saveTask">{{ t('manager.vectorMaterializedView.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { navigateManagerRoute } from '@/utils/moduleNavigation'
import { resolveManagerTaskWorkspaceRouteState } from '@/utils/taskWorkspaceRoute'
import { InfoFilled, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { openMonitorExecution, parseLocatorSafe, ResourceTreePicker } from '@addp/common-frontend'
import client from '@/api/client'
import { quickViewAPI } from '@/api/quickView'
import { useCurrentResultConfirmation } from '@/composables/useCurrentResultConfirmation'
import { formatDateTime } from '@/utils/formatters'
import {
  executionStatusTagType,
  lastExecutionStatus,
  resourceTextFromLocator as resourceTextFromLocatorValue,
  taskResource as taskResourceValue
} from '@/utils/tileCacheDisplay'
import {
  createDefaultVectorMaterializedViewTaskForm,
  createVectorMaterializedViewTaskFormFromTask,
  createVectorMaterializedViewTaskPayload
} from '@/utils/vectorMaterializedViewTaskForm'
import { vectorMaterializedViewResultAction } from '@/utils/vectorMaterializedViewResultAction'
import { quickViewDisplayText } from '@/utils/quickViewResourceDisplay'
import { buildTileCacheCreateQuery } from '@/utils/quickViewNavigationQuery'
import { tableSelectionFromResourceNode } from '@/utils/tileCacheResourceTree'

const { t } = useI18n()
const executeWithCurrentResultConfirmation = useCurrentResultConfirmation()
const route = useRoute()
const router = useRouter()

const routeQueryKeys = ['create', 'geom', 'geometry_columns', 'item_fingerprint', 'item_id', 'locator', 'source_srid', 'task_id']
const resolveRouteState = routeQuery => resolveManagerTaskWorkspaceRouteState({
  routeQuery,
  allowedQueryByTab: {
    tasks: routeQueryKeys,
    results: ['item_fingerprint', 'item_id', 'task_id']
  }
})
const activeTab = ref(resolveRouteState(route.query).tab)
let routeDataReady = false
const tasks = ref([])
const tasksLoading = ref(false)
const tasksPage = ref(1)
const tasksPageSize = ref(20)
const tasksTotal = ref(0)
const executingId = ref(null)
const executingResultId = ref(null)
const engineOptions = ref([])
const resourceSelection = ref(null)
const acceptedResourceSelection = ref(null)
const capabilityLoading = ref(false)

const results = ref([])
const resultsLoading = ref(false)
const resultsPage = ref(1)
const resultsPageSize = ref(20)
const resultsTotal = ref(0)
const resultFilters = reactive({ item_id: undefined, item_fingerprint: '', task_id: undefined, status: '', q: '' })
const resultStatuses = ['building', 'ready', 'stale', 'failed', 'deleted']
const selectedResultTask = ref(null)

const formRef = ref(null)
const formDialogVisible = ref(false)
const saving = ref(false)
const editingId = ref(null)
const form = reactive(createDefaultVectorMaterializedViewTaskForm())
const geometryColumnOptions = ref([])
const databaseEngineTypes = new Set(['postgresql', 'postgres', 'postgis'])

const routeSourceLocked = computed(() => !!route.query.locator)
const sourceSelectionLocked = computed(() => !!editingId.value || routeSourceLocked.value)
const showResourcePicker = computed(() => !routeSourceLocked.value || !!editingId.value)
const formTitle = computed(() => editingId.value ? t('manager.vectorMaterializedView.editTitle') : t('manager.vectorMaterializedView.createTitle'))
const sourceResourceText = computed(() => {
  const schema = String(form.config.target.schema || '').trim()
  const table = String(form.config.target.table || '').trim()
  return schema && table ? `${schema}.${table}` : '-'
})
const selectedResourceSummary = computed(() => {
  const parts = []
  const engine = engineName(form.config.target.source_engine_id)
  if (engine && engine !== '-') parts.push(engine)
  if (sourceResourceText.value && sourceResourceText.value !== '-') parts.push(sourceResourceText.value)
  return parts.join(' / ') || resourceTextFromLocator(form.config.target.locator) || ''
})
const resultTaskFilterLabel = computed(() => {
  if (!selectedResultTask.value) return ''
  return `${t('manager.vectorMaterializedView.currentTask')}: ${selectedResultTask.value.name ? displayText(selectedResultTask.value.name) : taskResource(selectedResultTask.value)}`
})
const sourceAlready3857 = computed(() => Number(form.config.geometry.source_srid || 0) === 3857)
const rules = computed(() => ({
  name: [{ required: true, message: t('manager.vectorMaterializedView.nameRequired'), trigger: 'blur' }],
  'config.target.locator': [{ required: true, message: t('manager.vectorMaterializedView.sourceTableRequired'), trigger: 'change' }],
  'config.geometry.geometry_column': [{ required: true, message: t('manager.vectorMaterializedView.geometryColumnRequired'), trigger: 'change' }]
}))

const resetForm = (task = null) => {
  const next = createVectorMaterializedViewTaskFormFromTask(task)
  geometryColumnOptions.value = []
  resourceSelection.value = null
  acceptedResourceSelection.value = null
  Object.assign(form, next)
  if (form.config.geometry.geometry_column) {
    geometryColumnOptions.value = [form.config.geometry.geometry_column]
  }
  editingId.value = task?.id || null
}

const safeParseLocator = (locator) => {
  const parsed = parseLocatorSafe(locator)
  return parsed.engineId ? parsed : null
}

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const response = await quickViewAPI.listOptimizationTasks({
      page: tasksPage.value,
      page_size: tasksPageSize.value
    })
    tasks.value = response.data || []
    tasksTotal.value = response.total || 0
  } catch (error) {
    console.error('加载矢量物化视图任务失败:', error)
    ElMessage.error(t('manager.vectorMaterializedView.loadTasksFailed'))
  } finally {
    tasksLoading.value = false
  }
}

const loadResults = async () => {
  resultsLoading.value = true
  try {
    const response = await quickViewAPI.listOptimizations({
      page: resultsPage.value,
      page_size: resultsPageSize.value,
      status: resultFilters.status || undefined,
      q: resultFilters.q || undefined,
      task_id: resultFilters.task_id || undefined,
      item_id: resultFilters.item_id || undefined,
      item_fingerprint: resultFilters.item_fingerprint || undefined
    })
    results.value = response.data || []
    resultsTotal.value = response.total || 0
  } catch (error) {
    console.error('加载矢量物化视图结果失败:', error)
    ElMessage.error(t('manager.vectorMaterializedView.loadResultsFailed'))
  } finally {
    resultsLoading.value = false
  }
}

const loadEngines = async (force = false) => {
  if (engineOptions.value.length && !force) return engineOptions.value
  try {
    const response = await client.get('/manager/engines')
    engineOptions.value = (response.data || []).filter((engine) => databaseEngineTypes.has(String(engine.engine_type || '').toLowerCase()))
    return engineOptions.value
  } catch (error) {
    console.error('加载引擎列表失败:', error)
    return []
  }
}

const applyRouteSourceContext = () => {
  if (route.query.item_id) form.config.target.item_id = Number(route.query.item_id)
  if (route.query.item_fingerprint) {
    form.config.target.item_fingerprint = String(route.query.item_fingerprint)
    resultFilters.item_fingerprint = String(route.query.item_fingerprint)
  }
  if (route.query.locator) form.config.target.locator = String(route.query.locator)
  if (route.query.geom) form.config.geometry.geometry_column = String(route.query.geom)
  form.config.geometry.source_srid = parseQueryNumber(route.query.source_srid, form.config.geometry.source_srid)
  form.config.storage.target_schema = form.config.target.schema
  setGeometryOptions(parseQueryList(route.query.geometry_columns), form.config.geometry.geometry_column)
}

const parseQueryNumber = (value, defaultValue = 0) => {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : defaultValue
}

const parseQueryList = (value) => {
  if (!value) return []
  return String(value).split(',').map((item) => item.trim()).filter(Boolean)
}

const setGeometryOptions = (columns, selected = '') => {
  const options = []
  for (const column of columns || []) {
    const normalized = String(column || '').trim()
    if (normalized && !options.includes(normalized)) options.push(normalized)
  }
  const current = String(selected || '').trim()
  if (current && !options.includes(current)) options.unshift(current)
  geometryColumnOptions.value = options
  if (!form.config.geometry.geometry_column && options.length === 1) {
    form.config.geometry.geometry_column = options[0]
  }
}

const loadQuickViewCapabilityForForm = async (fallbackGeometryColumns = []) => {
  const locator = String(form.config.target.locator || '').trim()
  if (!locator) return null
  capabilityLoading.value = true
  try {
    const capability = await quickViewAPI.getQuickViewCapabilityByLocator(locator)
    const quickView = capability?.quick_view || {}
    const renderFacts = capability?.render_facts || {}
    if (capability?.source_engine_id) form.config.target.source_engine_id = Number(capability.source_engine_id)
    if (capability?.source_schema) form.config.target.schema = String(capability.source_schema)
    if (capability?.source_table) form.config.target.table = String(capability.source_table)
    if (capability?.locator) form.config.target.locator = String(capability.locator)
    if (capability?.item_fingerprint) form.config.target.item_fingerprint = String(capability.item_fingerprint)
    const columns = [
      ...(quickView.geometry_columns || []),
      quickView.geometry_column,
      ...fallbackGeometryColumns
    ].map((column) => String(column || '').trim()).filter(Boolean)
    setGeometryOptions(columns, form.config.geometry.geometry_column || quickView.geometry_column)
    form.config.geometry.source_srid = Number(renderFacts.source_srid || quickView.source_srid || form.config.geometry.source_srid || 0)
    form.config.geometry.target_srid = 3857
    form.config.storage.target_schema = form.config.target.schema
    return capability
  } catch (error) {
    console.error('加载快显能力失败:', error)
    ElMessage.error(t('manager.vectorMaterializedView.loadCapabilityFailed'))
    return null
  } finally {
    capabilityLoading.value = false
  }
}

const isSupportedDatabaseEngine = (engine) => databaseEngineTypes.has(String(engine?.engine_type || '').toLowerCase())

const resourceTargetFromSelection = (selection) => {
  return tableSelectionFromResourceNode(selection?.raw?.node, safeParseLocator)
}

const isSelectableSpatialTable = (node) => {
  const target = tableSelectionFromResourceNode(node, safeParseLocator)
  if (!target) return false
  return !sourceSelectionLocked.value || target.locator === form.config.target.locator
}

const handleResourceNodeClick = (node) => {
  const target = tableSelectionFromResourceNode(node, safeParseLocator)
  if (target && sourceSelectionLocked.value && target.locator !== form.config.target.locator) {
    ElMessage.info(t('manager.vectorMaterializedView.sourceLockedHint'))
  }
}

const handleResourceSelection = async (resourceSelectionValue) => {
  const selection = resourceTargetFromSelection(resourceSelectionValue)
  if (!selection) return
  const previousTarget = { ...form.config.target }
  const previousGeometry = { ...form.config.geometry }
  const previousStorage = { ...form.config.storage }
  const previousOptions = [...geometryColumnOptions.value]
  const previousSelection = acceptedResourceSelection.value
  Object.assign(form.config.target, selection)
  form.config.geometry.geometry_column = ''
  form.config.storage.target_schema = selection.schema
  const fallbackColumns = resourceSelectionValue?.resource?.spatial?.geometry_columns || []
  const capability = await loadQuickViewCapabilityForForm(fallbackColumns)
  if (!capability || !geometryColumnOptions.value.length) {
    Object.assign(form.config.target, previousTarget)
    Object.assign(form.config.geometry, previousGeometry)
    Object.assign(form.config.storage, previousStorage)
    geometryColumnOptions.value = previousOptions
    resourceSelection.value = previousSelection
    ElMessage.warning(t('manager.vectorMaterializedView.spatialTableRequired'))
    return
  }
  resourceSelection.value = resourceSelectionValue
  acceptedResourceSelection.value = resourceSelectionValue
  if (!form.name) {
    form.name = t('manager.vectorMaterializedView.defaultTaskName', { resource: `${selection.schema}.${selection.table}` })
  }
  await formRef.value?.validateField('config.target.locator').catch(() => {})
}

const openCreateDialog = async () => {
  resetForm()
  formDialogVisible.value = true
  applyRouteSourceContext()
  await loadQuickViewCapabilityForForm()
  if (!form.name && form.config.target.schema && form.config.target.table) {
    form.name = t('manager.vectorMaterializedView.defaultTaskName', { resource: `${form.config.target.schema}.${form.config.target.table}` })
  }
}

const requestCreateDialog = async () => {
  const routeState = resolveRouteState({ tab: 'tasks', create: '1' })
  await navigateManagerRoute(router, { path: route.path, query: routeState.query }, { history: 'push' })
}

const clearFormDialogRoute = async () => {
  if (activeTab.value !== 'tasks') return
  const nextQuery = { ...route.query }
  for (const key of routeQueryKeys) delete nextQuery[key]
  const routeState = resolveRouteState(nextQuery)
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateManagerRoute(router, location, { history: 'replace' })
  }
}

const openEditDialog = async (task) => {
  resetForm(task)
  const columns = task?.config?.geometry?.geometry_column ? [task.config.geometry.geometry_column] : []
  setGeometryOptions(columns, task?.config?.geometry?.geometry_column || '')
  formDialogVisible.value = true
  await loadQuickViewCapabilityForForm(columns)
}

const requestEditTask = async (task) => {
  const routeState = resolveRouteState({ tab: 'tasks', task_id: task.id })
  await navigateManagerRoute(router, { path: route.path, query: routeState.query }, { history: 'push' })
}

const saveTask = async () => {
  if (sourceAlready3857.value) {
    ElMessage.info(t('manager.vectorMaterializedView.sourceAlready3857'))
    return
  }
  await formRef.value?.validate()
  saving.value = true
  try {
    const payload = createVectorMaterializedViewTaskPayload(form)
    if (editingId.value) {
      await quickViewAPI.updateOptimizationTask(editingId.value, payload)
      ElMessage.success(t('manager.vectorMaterializedView.updateSuccess'))
    } else {
      await quickViewAPI.createOptimizationTask(payload)
      ElMessage.success(t('manager.vectorMaterializedView.createSuccess'))
    }
    formDialogVisible.value = false
    await loadTasks()
  } catch (error) {
    console.error('保存矢量物化视图任务失败:', error)
    ElMessage.error(errorMessage(error, t('manager.vectorMaterializedView.saveFailed')))
  } finally {
    saving.value = false
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await executeWithCurrentResultConfirmation(payload => quickViewAPI.executeOptimizationTask(task.id, payload))
    ElMessage.success(t('manager.vectorMaterializedView.executeSubmitted'))
    await loadTasks()
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      console.error('执行矢量物化视图任务失败:', error)
      ElMessage.error(errorMessage(error, t('manager.vectorMaterializedView.executeFailed')))
    }
  } finally {
    executingId.value = null
  }
}

const refreshStaleResult = async (result) => {
  const action = resultAction(result)
  if (!action.canRerun) {
    await openCreateDialogFromResult(result, { preferCurrentCapability: true })
    return
  }
  executingResultId.value = result.id
  try {
    const response = await executeWithCurrentResultConfirmation(payload => quickViewAPI.executeOptimizationTask(result.task_id, payload))
    ElMessage.success(t('manager.vectorMaterializedView.refreshSubmitted'))
    await Promise.all([loadTasks(), loadResults()])
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') {
      console.error('重新执行矢量物化视图失败:', error)
      ElMessage.error(errorMessage(error, t('manager.vectorMaterializedView.executeFailed')))
    }
  } finally {
    executingResultId.value = null
  }
}

const deleteTask = async (task) => {
  await ElMessageBox.confirm(t('manager.vectorMaterializedView.deleteTaskConfirm'), t('manager.vectorMaterializedView.delete'), { type: 'warning' })
  await quickViewAPI.deleteOptimizationTask(task.id)
  ElMessage.success(t('manager.vectorMaterializedView.deleteSuccess'))
  await loadTasks()
}

const deleteResult = async (result) => {
  await ElMessageBox.confirm(t('manager.vectorMaterializedView.deleteResultConfirm'), t('manager.vectorMaterializedView.delete'), { type: 'warning' })
  await quickViewAPI.deleteOptimization(result.id)
  ElMessage.success(t('manager.vectorMaterializedView.deleteSuccess'))
  await loadResults()
}

const viewTaskResults = async (task) => {
  selectedResultTask.value = task
  resultFilters.task_id = task.id
  resultFilters.item_id = undefined
  resultFilters.item_fingerprint = ''
  resultFilters.status = ''
  resultFilters.q = ''
  resultsPage.value = 1
  activeTab.value = 'results'
  await navigateManagerRoute(router, {
    query: {
      ...route.query,
      tab: 'results',
      task_id: String(task.id),
      item_id: undefined,
      item_fingerprint: undefined,
      create: undefined
    }
  }, { history: 'replace' })
}

const loadResultTaskFilterFromRoute = async () => {
  const taskId = Number(route.query.task_id || 0)
  if (!taskId || activeTab.value !== 'results') return
  resultFilters.task_id = taskId
  try {
    selectedResultTask.value = await quickViewAPI.getOptimizationTask(taskId)
  } catch (error) {
    console.error('加载矢量物化视图任务详情失败:', error)
    selectedResultTask.value = { id: taskId, name: t('manager.vectorMaterializedView.taskWithId', { id: taskId }) }
  }
}

const handleTabChange = async (tab) => {
  const routeState = resolveRouteState({
    ...route.query,
    tab,
    task_id: tab === 'results' ? route.query.task_id : undefined
  })
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

const applyResultFilters = () => {
  resultsPage.value = 1
  loadResults()
}

const resetResultFilters = async () => {
  selectedResultTask.value = null
  Object.assign(resultFilters, { item_id: undefined, item_fingerprint: '', task_id: undefined, status: '', q: '' })
  await navigateManagerRoute(router, {
    query: {
      ...route.query,
      task_id: undefined,
      item_id: undefined,
      item_fingerprint: undefined
    }
  }, { history: 'replace' })
  applyResultFilters()
}

const clearResultTaskFilter = async () => {
  selectedResultTask.value = null
  resultFilters.task_id = undefined
  await navigateManagerRoute(router, { query: { ...route.query, task_id: undefined } }, { history: 'replace' })
  applyResultFilters()
}

const openTaskExecution = (task) => openMonitorExecution(task.last_execution_id)
const openResultExecution = (result) => openMonitorExecution(result.last_execution_id)

const resultAction = (result) => vectorMaterializedViewResultAction(result)

const openCreateDialogFromResult = async (result, options = {}) => {
  resetForm()
  form.config.target.source_engine_id = Number(result.source_engine_id || 0)
  form.config.target.schema = result.source_schema || ''
  form.config.target.table = result.source_table || ''
  form.config.target.locator = result.locator || ''
  if (result.item_id) form.config.target.item_id = Number(result.item_id)
  form.config.target.item_fingerprint = result.item_fingerprint || ''
  form.config.geometry.geometry_column = options.preferCurrentCapability ? '' : result.source_geometry_column || ''
  form.config.geometry.source_srid = options.preferCurrentCapability ? 0 : Number(result.source_srid || 0)
  form.config.geometry.target_srid = 3857
  form.config.storage.target_schema = result.source_schema || ''
  setGeometryOptions([result.source_geometry_column], form.config.geometry.geometry_column)
  if (result.source_schema && result.source_table) {
    form.name = t('manager.vectorMaterializedView.defaultTaskName', { resource: `${result.source_schema}.${result.source_table}` })
  }
  formDialogVisible.value = true
  await loadQuickViewCapabilityForForm([result.source_geometry_column])
}

const openTileCacheCreate = (result) => {
  navigateManagerRoute(router, {
    name: 'TileCache',
    query: {
      ...buildTileCacheCreateQuery({
        engineId: result.source_engine_id,
        schema: result.source_schema,
        table: result.source_table,
        locator: result.locator,
        geometryColumn: result.source_geometry_column,
        sourceSRID: result.source_srid,
        itemID: result.item_id,
        itemFingerprint: result.item_fingerprint
      }),
      vector_materialized_view_generation: 'ready',
      vector_materialized_view_id: result.id ? String(result.id) : undefined
    }
  })
}

const taskResource = (task) => taskResourceValue(task, safeParseLocator)

const resourceTextFromLocator = (locator) => resourceTextFromLocatorValue(locator, safeParseLocator)

const displayText = (value) => quickViewDisplayText(value, parseLocatorSafe) || '-'

const engineName = (engineId) => {
  const id = Number(engineId || 0)
  if (!id) return '-'
  const engine = engineOptions.value.find((item) => Number(item.id) === id)
  return engine?.name || t('manager.quickViewDisplay.unknownEngine')
}

const sourceDataPath = (result) => {
  if (result?.source_schema && result?.source_table) return `${result.source_schema}.${result.source_table}`
  return resourceTextFromLocator(result?.locator) || '-'
}

const targetPath = (result) => {
  if (result?.target_schema && result?.target_table) return `${result.target_schema}.${result.target_table}`
  return '-'
}

const executionStatusLabel = (status) => {
  if (!status) return t('manager.vectorMaterializedView.statusNeverRun')
  if (!['pending', 'running', 'success', 'failed', 'timeout', 'cancelled'].includes(status)) return status
  return t(`manager.vectorMaterializedView.status.${status}`)
}

const resultStatusLabel = (status) => {
  if (!status) return '-'
  return t(`manager.vectorMaterializedView.resultStatuses.${status}`)
}

const optimizationResultStatusTagType = (status) => {
  if (status === 'ready') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'building' || status === 'stale') return 'warning'
  return 'info'
}

const errorMessage = (error, fallback) => {
  const data = error?.response?.data
  if (typeof data?.error === 'string' && data.error.trim()) return data.error
  if (typeof data?.message === 'string' && data.message.trim()) return data.message
  if (typeof error?.message === 'string' && error.message.trim()) return error.message
  return fallback
}

async function restoreWorkspaceFromRoute() {
  const routeState = resolveRouteState(route.query)
  activeTab.value = routeState.tab
  if (routeState.changed) {
    await navigateManagerRoute(router, { path: route.path, query: routeState.query }, { history: 'replace' })
    return
  }
  if (!routeDataReady) return

  if (routeState.tab === 'results') {
    formDialogVisible.value = false
    Object.assign(resultFilters, {
      item_id: Number(routeState.query.item_id || 0) || undefined,
      item_fingerprint: String(routeState.query.item_fingerprint || ''),
      task_id: Number(routeState.query.task_id || 0) || undefined
    })
    selectedResultTask.value = null
    await loadResultTaskFilterFromRoute()
    await loadResults()
    return
  }

  const taskId = Number(routeState.query.task_id || 0)
  if (taskId) {
    try {
      const response = await quickViewAPI.getOptimizationTask(taskId)
      await openEditDialog(response)
    } catch (error) {
      ElMessage.error(t('manager.vectorMaterializedView.loadTasksFailed'))
    }
  } else if (routeState.query.create === '1') {
    await openCreateDialog()
  } else {
    formDialogVisible.value = false
  }
  await loadTasks()
}

watch(() => route.query, restoreWorkspaceFromRoute)

onMounted(async () => {
  await restoreWorkspaceFromRoute()
  await Promise.all([loadTasks(), loadEngines()])
  if (activeTab.value === 'results') {
    if (route.query.item_id) resultFilters.item_id = Number(route.query.item_id)
    if (route.query.item_fingerprint) resultFilters.item_fingerprint = String(route.query.item_fingerprint)
    await loadResultTaskFilterFromRoute()
    await loadResults()
  }
  const taskId = Number(route.query.task_id || 0)
  if (taskId && activeTab.value !== 'results') {
    try {
      const response = await quickViewAPI.getOptimizationTask(taskId)
      openEditDialog(response)
    } catch (error) {
      console.error('加载矢量物化视图任务详情失败:', error)
      ElMessage.error(t('manager.vectorMaterializedView.loadTasksFailed'))
    }
  } else if (route.query.create === '1') {
    await openCreateDialog()
  }
  routeDataReady = true
})
</script>

<style scoped>
.vector-materialized-view {
  padding: 20px;
}

.tab-toolbar,
.filter-bar {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 14px;
  flex-wrap: wrap;
}

.task-tab-toolbar {
  justify-content: flex-end;
}

.toolbar-tip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  margin-right: auto;
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.toolbar-tip-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.filter-bar .el-input {
  width: 260px;
}

.filter-bar .el-select {
  width: 160px;
}

.task-filter-chip {
  max-width: 320px;
}

.pagination {
  margin-top: 20px;
  justify-content: center;
}

.row-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.result-status-cell {
  display: inline-flex;
  gap: 4px;
  align-items: center;
}

.inline-tip-icon,
.result-status-tip {
  flex: 0 0 auto;
  color: var(--el-color-info);
  cursor: help;
}

.form-section-title {
  font-weight: 600;
  color: var(--addp-text-primary);
  margin: 16px 0 12px;
}

.source-summary {
  margin-bottom: 16px;
}

.optimization-advice {
  margin-bottom: 14px;
}

.optimization-advice :deep(.el-alert__description),
.compact-tip-alert :deep(.el-alert__description) {
  display: flex;
  align-items: center;
  justify-content: flex-end;
}

.resource-picker {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.selected-resource__main {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.selected-resource__name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.selected-resource {
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  padding: 10px 12px;
  background: var(--addp-bg-secondary);
}

.selected-resource__meta {
  margin-top: 6px;
  color: var(--addp-text-secondary);
  font-size: 12px;
}
</style>
