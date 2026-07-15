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
            <el-button type="primary" :icon="Plus" @click="openCreateDialog">
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
                  <el-button size="small" @click="openEditDialog(row)">{{ t('manager.vectorMaterializedView.edit') }}</el-button>
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

    <el-dialog v-model="formDialogVisible" :title="formTitle" width="860px">
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
              <ResourceTree
                :tree-data="resourceTreeData"
                :loading="resourceLoading"
                :show-refresh-button="true"
                :show-count="false"
                :expanded-keys="resourceExpandedKeys"
                :current-node-key="resourceCurrentKey"
                :default-expand-root="false"
                :expand-on-click-node="true"
                :title="t('manager.vectorMaterializedView.resourceTreeTitle')"
                height="320px"
                @refresh="loadResourceTrees(true)"
                @node-click="handleResourceNodeClick"
                @node-expand="handleResourceNodeExpand"
                @update:expanded-keys="resourceExpandedKeys = $event"
                @update:current-node-key="resourceCurrentKey = $event"
              >
                <template #node-label="{ data }">
                  <span class="resource-node-label">
                    <el-icon v-if="isResourceRootNode(data)" class="resource-root-caret"><ArrowDown /></el-icon>
                    <span class="resource-node-text">{{ data.label }}</span>
                    <el-tag v-if="selectableResourceType(data)" size="small" type="success">
                      {{ selectableResourceType(data) }}
                    </el-tag>
                  </span>
                </template>
              </ResourceTree>
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
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowDown, InfoFilled, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { openMonitorExecution, parseLocatorSafe, ResourceTree } from '@addp/common-frontend'
import client from '@/api/client'
import { dataExplorerAPI } from '@/api/dataExplorer'
import { quickViewAPI } from '@/api/quickView'
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
import {
  createResourceRootNode,
  geometryColumnsFromNode,
  isResourceRootNode,
  locatorEngineID,
  mergeAncestorChainIntoResourceTree,
  normalizeResourceNode,
  replaceResourceNode,
  tableSelectionFromResourceNode,
  updateResourceNodeChildren
} from '@/utils/tileCacheResourceTree'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const activeTab = ref(route.query.tab === 'results' ? 'results' : 'tasks')
const tasks = ref([])
const tasksLoading = ref(false)
const tasksPage = ref(1)
const tasksPageSize = ref(20)
const tasksTotal = ref(0)
const executingId = ref(null)
const executingResultId = ref(null)
const engineOptions = ref([])
const resourceTreeData = ref([])
const resourceExpandedKeys = ref([])
const resourceCurrentKey = ref('')
const resourceLoading = ref(false)
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
  resourceCurrentKey.value = ''
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

const loadResourceTrees = async (force = false) => {
  if (resourceTreeData.value.length && !force) return
  resourceLoading.value = true
  try {
    const engines = await loadEngines(force)
    resourceTreeData.value = engines.map(createResourceRootNode)
    resourceExpandedKeys.value = []
  } catch (error) {
    console.error('加载矢量物化视图资源树失败:', error)
    ElMessage.error(t('manager.vectorMaterializedView.loadResourceTreeFailed'))
  } finally {
    resourceLoading.value = false
  }
}

const loadResourceTreeRoot = async (node) => {
  const engineID = Number(node?.engineId || locatorEngineID(node?.locator || node?.id, safeParseLocator))
  if (!engineID) return
  resourceLoading.value = true
  try {
    const tree = await dataExplorerAPI.getTree(engineID, 2)
    const engine = { id: node.engineId, engine_type: node.engineType, name: node.engineName }
    const normalized = normalizeResourceNode(tree, engine, { parseLocator: safeParseLocator, loaded: true })
    if (!normalized) return
    resourceTreeData.value = replaceResourceNode(resourceTreeData.value, node.locator || node.id, normalized)
    if (!resourceExpandedKeys.value.includes(normalized.id)) {
      resourceExpandedKeys.value = [...resourceExpandedKeys.value, normalized.id]
    }
  } catch (error) {
    console.error('加载矢量物化视图资源树失败:', error)
    ElMessage.error(t('manager.vectorMaterializedView.loadResourceTreeFailed'))
  } finally {
    resourceLoading.value = false
  }
}

const loadResourceNodeChildren = async (node) => {
  const locator = node?.locator || node?.id
  const engineID = Number(node?.engineId || locatorEngineID(locator, safeParseLocator))
  if (!locator || !engineID) return
  resourceLoading.value = true
  try {
    const response = await dataExplorerAPI.getNodeChildren(engineID, locator)
    const engine = { id: node.engineId, engine_type: node.engineType, name: node.engineName }
    const children = (response.children || [])
      .map((child) => normalizeResourceNode(child, engine, { parseLocator: safeParseLocator }))
      .filter(Boolean)
    resourceTreeData.value = updateResourceNodeChildren(resourceTreeData.value, locator, children)
  } catch (error) {
    console.error('加载矢量物化视图资源子节点失败:', error)
    ElMessage.error(t('manager.vectorMaterializedView.loadResourceTreeFailed'))
  } finally {
    resourceLoading.value = false
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

const handleResourceNodeExpand = async (node) => {
  const locator = node?.locator || node?.id
  if (!locator || !node?.hasChildren || (node.children || []).length > 0) return
  if (isResourceRootNode(node) && !node.loaded) {
    await loadResourceTreeRoot(node)
    return
  }
  await loadResourceNodeChildren(node)
}

const handleResourceNodeClick = async (node) => {
  const locator = node?.locator || node?.id
  resourceCurrentKey.value = locator || ''
  if (isResourceRootNode(node) && !node.loaded) {
    await loadResourceTreeRoot(node)
    return
  }
  if (node?.hasChildren && (node.children || []).length === 0) {
    await loadResourceNodeChildren(node)
  }
  const selection = tableSelectionFromResourceNode(node, safeParseLocator)
  if (!selection) return
  if (sourceSelectionLocked.value && selection.locator !== form.config.target.locator) {
    resourceCurrentKey.value = form.config.target.locator || ''
    ElMessage.info(t('manager.vectorMaterializedView.sourceLockedHint'))
    return
  }
  const previousTarget = { ...form.config.target }
  const previousGeometry = { ...form.config.geometry }
  const previousStorage = { ...form.config.storage }
  const previousOptions = [...geometryColumnOptions.value]
  Object.assign(form.config.target, selection)
  form.config.geometry.geometry_column = ''
  form.config.storage.target_schema = selection.schema
  const capability = await loadQuickViewCapabilityForForm(geometryColumnsFromNode(node))
  if (!capability || !geometryColumnOptions.value.length) {
    Object.assign(form.config.target, previousTarget)
    Object.assign(form.config.geometry, previousGeometry)
    Object.assign(form.config.storage, previousStorage)
    geometryColumnOptions.value = previousOptions
    ElMessage.warning(t('manager.vectorMaterializedView.spatialTableRequired'))
    return
  }
  resourceCurrentKey.value = selection.locator
  if (!form.name) {
    form.name = t('manager.vectorMaterializedView.defaultTaskName', { resource: `${selection.schema}.${selection.table}` })
  }
  await formRef.value?.validateField('config.target.locator').catch(() => {})
}

const revealSelectedResource = async () => {
  const locator = String(form.config.target.locator || '').trim()
  if (!locator) return
  const engineID = Number(form.config.target.source_engine_id || locatorEngineID(locator, safeParseLocator))
  if (!engineID) return
  resourceLoading.value = true
  try {
    const response = await dataExplorerAPI.getTreeAncestors(engineID, locator)
    const chain = Array.isArray(response?.ancestors) ? response.ancestors : []
    if (!chain.length) return
    const engine = engineOptions.value.find((item) => Number(item.id) === engineID) || null
    const merged = mergeAncestorChainIntoResourceTree(resourceTreeData.value, chain, {
      engine,
      parseLocator: safeParseLocator
    })
    resourceTreeData.value = merged.nodes
    const expanded = new Set(resourceExpandedKeys.value)
    for (const key of merged.expandedKeys) {
      expanded.add(key)
    }
    resourceExpandedKeys.value = Array.from(expanded)
    resourceCurrentKey.value = response?.target_locator || merged.target?.locator || merged.target?.id || locator
  } catch (error) {
    console.error('定位矢量物化视图资源失败:', error)
    ElMessage.error(t('manager.vectorMaterializedView.loadResourceTreeFailed'))
  } finally {
    resourceLoading.value = false
  }
}

const openCreateDialog = async () => {
  resetForm()
  formDialogVisible.value = true
  applyRouteSourceContext()
  if (showResourcePicker.value) await loadResourceTrees()
  await loadQuickViewCapabilityForForm()
  if (form.config.target.locator && showResourcePicker.value) {
    await revealSelectedResource()
  }
  if (!form.name && form.config.target.schema && form.config.target.table) {
    form.name = t('manager.vectorMaterializedView.defaultTaskName', { resource: `${form.config.target.schema}.${form.config.target.table}` })
  }
}

const openEditDialog = async (task) => {
  resetForm(task)
  const columns = task?.config?.geometry?.geometry_column ? [task.config.geometry.geometry_column] : []
  setGeometryOptions(columns, task?.config?.geometry?.geometry_column || '')
  formDialogVisible.value = true
  await loadResourceTrees()
  await loadQuickViewCapabilityForForm(columns)
  await revealSelectedResource()
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
    const response = await quickViewAPI.executeOptimizationTask(task.id)
    ElMessage.success(t('manager.vectorMaterializedView.executeSubmitted'))
    await loadTasks()
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    console.error('执行矢量物化视图任务失败:', error)
    ElMessage.error(errorMessage(error, t('manager.vectorMaterializedView.executeFailed')))
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
    const response = await quickViewAPI.executeOptimizationTask(result.task_id)
    ElMessage.success(t('manager.vectorMaterializedView.refreshSubmitted'))
    await Promise.all([loadTasks(), loadResults()])
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    console.error('重新执行矢量物化视图失败:', error)
    ElMessage.error(errorMessage(error, t('manager.vectorMaterializedView.executeFailed')))
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
  await router.replace({
    query: {
      ...route.query,
      tab: 'results',
      task_id: String(task.id),
      item_id: undefined,
      item_fingerprint: undefined,
      create: undefined
    }
  })
  await loadResults()
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

const handleTabChange = async () => {
  await router.replace({
    query: {
      ...route.query,
      tab: activeTab.value,
      task_id: activeTab.value === 'results' ? route.query.task_id : undefined
    }
  })
  if (activeTab.value === 'results') {
    await loadResults()
  } else {
    await loadTasks()
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
  await router.replace({
    query: {
      ...route.query,
      task_id: undefined,
      item_id: undefined,
      item_fingerprint: undefined
    }
  })
  applyResultFilters()
}

const clearResultTaskFilter = async () => {
  selectedResultTask.value = null
  resultFilters.task_id = undefined
  await router.replace({ query: { ...route.query, task_id: undefined } })
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
  if (showResourcePicker.value) await loadResourceTrees()
  await loadQuickViewCapabilityForForm([result.source_geometry_column])
  if (showResourcePicker.value) await revealSelectedResource()
}

const openTileCacheCreate = (result) => {
  router.push({
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

const selectableResourceType = (node) => tableSelectionFromResourceNode(node, safeParseLocator)
  ? t('manager.vectorMaterializedView.spatialTable')
  : ''

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

onMounted(async () => {
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

.resource-node-label,
.selected-resource__main {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.resource-node-text,
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
