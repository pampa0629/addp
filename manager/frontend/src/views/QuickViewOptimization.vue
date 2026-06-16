<template>
  <div class="quick-view-optimization">
    <el-card>
      <template #header>
        <div class="page-header">
          <div>
            <div class="page-title">{{ t('manager.quickViewOptimization.title') }}</div>
            <div class="page-subtitle">{{ t('manager.quickViewOptimization.subtitle') }}</div>
          </div>
          <el-button type="primary" :icon="Plus" @click="openCreateDialog">
            {{ t('manager.quickViewOptimization.create') }}
          </el-button>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        class="workflow-alert"
        :title="t('manager.quickViewOptimization.workflowTitle')"
        :description="t('manager.quickViewOptimization.workflowDescription')"
      />

      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.quickViewOptimization.tasksTab')" name="tasks">
          <div class="tab-toolbar">
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column prop="name" :label="t('manager.quickViewOptimization.name')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.quickViewOptimization.engine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(row.target?.source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.resource')" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">{{ taskResource(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.geometryColumn')" width="150" show-overflow-tooltip>
              <template #default="{ row }">{{ row.geometry?.geometry_column || '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.srid')" width="120">
              <template #default="{ row }">
                {{ row.geometry?.source_srid || '-' }} -> {{ row.geometry?.target_srid || 3857 }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.enabled')" width="100">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.quickViewOptimization.enabledYes') : t('manager.quickViewOptimization.enabledNo') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.lastExecutionStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(lastExecutionStatus(row))">
                  {{ executionStatusLabel(lastExecutionStatus(row)) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.lastRunAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.last_run_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.actions')" width="430" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.quickViewOptimization.execute') }}
                  </el-button>
                  <el-button size="small" @click="openEditDialog(row)">{{ t('manager.quickViewOptimization.edit') }}</el-button>
                  <el-button size="small" @click="viewTaskResults(row)">{{ t('manager.quickViewOptimization.results') }}</el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                    {{ t('manager.quickViewOptimization.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteTask(row)">{{ t('manager.quickViewOptimization.delete') }}</el-button>
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

        <el-tab-pane :label="t('manager.quickViewOptimization.resultsTab')" name="results">
          <div class="filter-bar">
            <div v-if="resultTaskFilterLabel" class="task-filter-chip">
              <el-tag type="primary" closable @close="clearResultTaskFilter">
                {{ resultTaskFilterLabel }}
              </el-tag>
            </div>
            <el-select v-model="resultFilters.status" clearable :placeholder="t('manager.quickViewOptimization.resultStatus')">
              <el-option v-for="status in resultStatuses" :key="status" :label="resultStatusLabel(status)" :value="status" />
            </el-select>
            <el-input v-model="resultFilters.q" clearable :placeholder="t('manager.quickViewOptimization.keywordPlaceholder')" @keyup.enter="applyResultFilters" />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.quickViewOptimization.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.quickViewOptimization.reset') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadResults" />
          </div>

          <el-table :data="results" v-loading="resultsLoading" stripe>
            <el-table-column :label="t('manager.quickViewOptimization.engine')" min-width="150" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(row.source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.sourceDataPath')" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">{{ sourceDataPath(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.target')" min-width="240" show-overflow-tooltip>
              <template #default="{ row }">{{ targetPath(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.resultStatus')" width="120">
              <template #default="{ row }">
                <el-tag :type="optimizationResultStatusTagType(row.status)">
                  {{ resultStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.rowCountEstimate')" width="130">
              <template #default="{ row }">{{ row.row_count_estimate ?? '-' }}</template>
            </el-table-column>
            <el-table-column prop="error_message" :label="t('manager.quickViewOptimization.error')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.quickViewOptimization.updatedAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.quickViewOptimization.actions')" width="280" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openResultExecution(row)">
                    {{ t('manager.quickViewOptimization.monitor') }}
                  </el-button>
                  <el-button size="small" :disabled="row.status !== 'ready'" @click="openTileCacheCreate(row)">
                    {{ t('manager.quickViewOptimization.generateTileCache') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteResult(row)">{{ t('manager.quickViewOptimization.delete') }}</el-button>
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
        <div class="form-section-title">{{ t('manager.quickViewOptimization.basicInfo') }}</div>
        <el-form-item :label="t('manager.quickViewOptimization.name')" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('manager.quickViewOptimization.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('manager.quickViewOptimization.enabled')">
          <el-switch v-model="form.enabled" />
        </el-form-item>

        <div class="form-section-title">{{ t('manager.quickViewOptimization.sourceData') }}</div>
        <el-descriptions v-if="!showResourcePicker" class="source-summary" :column="2" border>
          <el-descriptions-item :label="t('manager.quickViewOptimization.engine')">
            {{ engineName(form.config.target.source_engine_id) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.quickViewOptimization.resource')">
            {{ sourceResourceText }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.quickViewOptimization.sourceSrid')">
            {{ form.config.geometry.source_srid || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.quickViewOptimization.targetSrid')">
            {{ form.config.geometry.target_srid || 3857 }}
          </el-descriptions-item>
        </el-descriptions>
        <template v-else>
          <el-form-item :label="t('manager.quickViewOptimization.sourceTable')" prop="config.target.locator">
            <div class="resource-picker">
              <el-alert
                v-if="sourceSelectionLocked"
                type="info"
                :title="t('manager.quickViewOptimization.sourceLockedHint')"
                :closable="false"
                show-icon
              />
              <ResourceTree
                :tree-data="resourceTreeData"
                :loading="resourceLoading"
                :show-refresh-button="true"
                :show-count="false"
                :expanded-keys="resourceExpandedKeys"
                :current-node-key="resourceCurrentKey"
                :default-expand-root="false"
                :expand-on-click-node="true"
                :title="t('manager.quickViewOptimization.resourceTreeTitle')"
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
                  <el-tag size="small" type="success">{{ t('manager.quickViewOptimization.spatialTable') }}</el-tag>
                  <span class="selected-resource__name">{{ sourceResourceText }}</span>
                </div>
                <div class="selected-resource__meta">{{ selectedResourceSummary }}</div>
              </div>
            </div>
          </el-form-item>
        </template>

        <div class="form-section-title">{{ t('manager.quickViewOptimization.optimizationSettings') }}</div>
        <el-alert
          v-if="sourceAlready3857"
          type="info"
          :closable="false"
          show-icon
          class="optimization-advice"
          :title="t('manager.quickViewOptimization.sourceAlready3857')"
        />
        <el-form-item :label="t('manager.quickViewOptimization.geometryColumn')" prop="config.geometry.geometry_column">
          <el-select v-model="form.config.geometry.geometry_column" filterable :placeholder="t('manager.quickViewOptimization.geometryColumnRequired')">
            <el-option v-for="column in geometryColumnOptions" :key="column" :label="column" :value="column" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('manager.quickViewOptimization.targetKind')">
          <el-tag type="info">source_schema_materialized_view</el-tag>
        </el-form-item>
        <el-form-item :label="t('manager.quickViewOptimization.targetSrid')">
          <el-tag type="info">3857</el-tag>
        </el-form-item>
        <el-form-item :label="t('manager.quickViewOptimization.analyzeAfterBuild')">
          <el-switch v-model="form.config.optimization.analyze_after_build" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formDialogVisible = false">{{ t('manager.quickViewOptimization.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="sourceAlready3857" @click="saveTask">{{ t('manager.quickViewOptimization.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowDown, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { openMonitorExecution, parseLocator, ResourceTree } from '@addp/common-frontend'
import client from '@/api/client'
import { quickViewAPI } from '@/api/quickView'
import { formatDateTime } from '@/utils/formatters'
import {
  executionStatusTagType,
  lastExecutionStatus,
  resourceTextFromLocator as resourceTextFromLocatorValue,
  taskResource as taskResourceValue
} from '@/utils/tileCacheDisplay'
import {
  createDefaultQuickViewOptimizationTaskForm,
  createQuickViewOptimizationTaskFormFromTask,
  createQuickViewOptimizationTaskPayload
} from '@/utils/quickViewOptimizationTaskForm'
import {
  createResourceRootNode,
  findResourceNodePath,
  geometryColumnsFromNode,
  isResourceRootNode,
  locatorEngineID,
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
const form = reactive(createDefaultQuickViewOptimizationTaskForm())
const geometryColumnOptions = ref([])
const databaseEngineTypes = new Set(['postgresql', 'postgres', 'postgis'])

const routeSourceLocked = computed(() => !!route.query.engine_id && !!route.query.schema && !!route.query.table)
const sourceSelectionLocked = computed(() => !!editingId.value || routeSourceLocked.value)
const showResourcePicker = computed(() => !routeSourceLocked.value || !!editingId.value)
const formTitle = computed(() => editingId.value ? t('manager.quickViewOptimization.editTitle') : t('manager.quickViewOptimization.createTitle'))
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
  return `${t('manager.quickViewOptimization.currentTask')}: ${selectedResultTask.value.name || taskResource(selectedResultTask.value)}`
})
const sourceAlready3857 = computed(() => Number(form.config.geometry.source_srid || 0) === 3857)
const rules = computed(() => ({
  name: [{ required: true, message: t('manager.quickViewOptimization.nameRequired'), trigger: 'blur' }],
  'config.target.locator': [{ required: true, message: t('manager.quickViewOptimization.sourceTableRequired'), trigger: 'change' }],
  'config.geometry.geometry_column': [{ required: true, message: t('manager.quickViewOptimization.geometryColumnRequired'), trigger: 'change' }]
}))

const resetForm = (task = null) => {
  const next = createQuickViewOptimizationTaskFormFromTask(task)
  geometryColumnOptions.value = []
  resourceCurrentKey.value = ''
  Object.assign(form, next)
  if (form.config.geometry.geometry_column) {
    geometryColumnOptions.value = [form.config.geometry.geometry_column]
  }
  editingId.value = task?.id || null
}

const safeParseLocator = (locator) => {
  try {
    return parseLocator(locator)
  } catch {
    return null
  }
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
    console.error('加载快显性能优化任务失败:', error)
    ElMessage.error(t('manager.quickViewOptimization.loadTasksFailed'))
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
    console.error('加载快显性能优化结果失败:', error)
    ElMessage.error(t('manager.quickViewOptimization.loadResultsFailed'))
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
    console.error('加载快显性能优化资源树失败:', error)
    ElMessage.error(t('manager.quickViewOptimization.loadResourceTreeFailed'))
  } finally {
    resourceLoading.value = false
  }
}

const loadResourceTreeRoot = async (node) => {
  const engineID = Number(node?.engineId || locatorEngineID(node?.locator || node?.id, safeParseLocator))
  if (!engineID) return
  resourceLoading.value = true
  try {
    const tree = await client.get(`/manager/tree/${engineID}`, {
      params: { expand_depth: 2 }
    })
    const engine = { id: node.engineId, engine_type: node.engineType, name: node.engineName }
    const normalized = normalizeResourceNode(tree, engine, { parseLocator: safeParseLocator, loaded: true })
    if (!normalized) return
    resourceTreeData.value = replaceResourceNode(resourceTreeData.value, node.locator || node.id, normalized)
    if (!resourceExpandedKeys.value.includes(normalized.id)) {
      resourceExpandedKeys.value = [...resourceExpandedKeys.value, normalized.id]
    }
  } catch (error) {
    console.error('加载快显性能优化资源树失败:', error)
    ElMessage.error(t('manager.quickViewOptimization.loadResourceTreeFailed'))
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
    const response = await client.get(`/manager/tree/${engineID}/node`, {
      params: { locator, expand_depth: 1 }
    })
    const engine = { id: node.engineId, engine_type: node.engineType, name: node.engineName }
    const children = (response.children || [])
      .map((child) => normalizeResourceNode(child, engine, { parseLocator: safeParseLocator }))
      .filter(Boolean)
    resourceTreeData.value = updateResourceNodeChildren(resourceTreeData.value, locator, children)
  } catch (error) {
    console.error('加载快显性能优化资源子节点失败:', error)
    ElMessage.error(t('manager.quickViewOptimization.loadResourceTreeFailed'))
  } finally {
    resourceLoading.value = false
  }
}

const applyRouteSourceContext = () => {
  if (route.query.engine_id) form.config.target.source_engine_id = Number(route.query.engine_id)
  if (route.query.schema) form.config.target.schema = String(route.query.schema)
  if (route.query.table) form.config.target.table = String(route.query.table)
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
    ElMessage.error(t('manager.quickViewOptimization.loadCapabilityFailed'))
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
    ElMessage.info(t('manager.quickViewOptimization.sourceLockedHint'))
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
    ElMessage.warning(t('manager.quickViewOptimization.spatialTableRequired'))
    return
  }
  resourceCurrentKey.value = selection.locator
  if (!form.name) {
    form.name = t('manager.quickViewOptimization.defaultTaskName', { resource: `${selection.schema}.${selection.table}` })
  }
  await formRef.value?.validateField('config.target.locator').catch(() => {})
}

const revealSelectedResource = async () => {
  const locator = String(form.config.target.locator || '').trim()
  if (!locator) return
  const engineID = Number(form.config.target.source_engine_id || locatorEngineID(locator, safeParseLocator))
  if (!engineID) return
  let root = resourceTreeData.value.find((node) => Number(node.engineId) === engineID)
  if (!root) return
  if (isResourceRootNode(root) && !root.loaded) {
    await loadResourceTreeRoot(root)
  }
  const path = findResourceNodePath(resourceTreeData.value, locator)
  const expanded = new Set(resourceExpandedKeys.value)
  if (path.length > 1) {
    for (const node of path.slice(0, -1)) {
      expanded.add(node.locator || node.id)
    }
  } else {
    root = resourceTreeData.value.find((node) => Number(node.engineId) === engineID) || root
    expanded.add(root.locator || root.id)
  }
  resourceExpandedKeys.value = Array.from(expanded)
  resourceCurrentKey.value = ''
  await nextTick()
  resourceCurrentKey.value = locator
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
    form.name = t('manager.quickViewOptimization.defaultTaskName', { resource: `${form.config.target.schema}.${form.config.target.table}` })
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
    ElMessage.info(t('manager.quickViewOptimization.sourceAlready3857'))
    return
  }
  await formRef.value?.validate()
  saving.value = true
  try {
    const payload = createQuickViewOptimizationTaskPayload(form)
    if (editingId.value) {
      await quickViewAPI.updateOptimizationTask(editingId.value, payload)
      ElMessage.success(t('manager.quickViewOptimization.updateSuccess'))
    } else {
      await quickViewAPI.createOptimizationTask(payload)
      ElMessage.success(t('manager.quickViewOptimization.createSuccess'))
    }
    formDialogVisible.value = false
    await loadTasks()
  } catch (error) {
    console.error('保存快显性能优化任务失败:', error)
    ElMessage.error(errorMessage(error, t('manager.quickViewOptimization.saveFailed')))
  } finally {
    saving.value = false
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await quickViewAPI.executeOptimizationTask(task.id)
    ElMessage.success(t('manager.quickViewOptimization.executeSubmitted'))
    await loadTasks()
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    console.error('执行快显性能优化任务失败:', error)
    ElMessage.error(errorMessage(error, t('manager.quickViewOptimization.executeFailed')))
  } finally {
    executingId.value = null
  }
}

const deleteTask = async (task) => {
  await ElMessageBox.confirm(t('manager.quickViewOptimization.deleteTaskConfirm'), t('manager.quickViewOptimization.delete'), { type: 'warning' })
  await quickViewAPI.deleteOptimizationTask(task.id)
  ElMessage.success(t('manager.quickViewOptimization.deleteSuccess'))
  await loadTasks()
}

const deleteResult = async (result) => {
  await ElMessageBox.confirm(t('manager.quickViewOptimization.deleteResultConfirm'), t('manager.quickViewOptimization.delete'), { type: 'warning' })
  await quickViewAPI.deleteOptimization(result.id)
  ElMessage.success(t('manager.quickViewOptimization.deleteSuccess'))
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
  await router.replace({ query: { ...route.query, tab: 'results', task_id: String(task.id) } })
  await loadResults()
}

const loadResultTaskFilterFromRoute = async () => {
  const taskId = Number(route.query.task_id || 0)
  if (!taskId || activeTab.value !== 'results') return
  resultFilters.task_id = taskId
  try {
    selectedResultTask.value = await quickViewAPI.getOptimizationTask(taskId)
  } catch (error) {
    console.error('加载快显性能优化任务详情失败:', error)
    selectedResultTask.value = { id: taskId, name: t('manager.quickViewOptimization.taskWithId', { id: taskId }) }
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

const resetResultFilters = () => {
  selectedResultTask.value = null
  Object.assign(resultFilters, { item_id: undefined, item_fingerprint: '', task_id: undefined, status: '', q: '' })
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

const openTileCacheCreate = (result) => {
  router.push({
    name: 'TileCache',
    query: {
      tab: 'tasks',
      create: '1',
      engine_id: String(result.source_engine_id || ''),
      schema: result.source_schema || '',
      table: result.source_table || '',
      locator: result.locator || '',
      geom: result.source_geometry_column || '',
      source_srid: String(result.source_srid || ''),
      item_id: result.item_id ? String(result.item_id) : undefined,
      item_fingerprint: result.item_fingerprint || undefined
    }
  })
}

const selectableResourceType = (node) => tableSelectionFromResourceNode(node, safeParseLocator)
  ? t('manager.quickViewOptimization.spatialTable')
  : ''

const taskResource = (task) => taskResourceValue(task, safeParseLocator)

const resourceTextFromLocator = (locator) => resourceTextFromLocatorValue(locator, safeParseLocator)

const engineName = (engineId) => {
  const id = Number(engineId || 0)
  if (!id) return '-'
  const engine = engineOptions.value.find((item) => Number(item.id) === id)
  return engine?.name || t('manager.quickViewOptimization.engineWithId', { id })
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
  if (!status) return t('manager.quickViewOptimization.statusNeverRun')
  if (!['pending', 'running', 'success', 'failed', 'timeout', 'cancelled'].includes(status)) return status
  return t(`manager.quickViewOptimization.status.${status}`)
}

const resultStatusLabel = (status) => {
  if (!status) return '-'
  return t(`manager.quickViewOptimization.resultStatuses.${status}`)
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
      console.error('加载快显性能优化任务详情失败:', error)
      ElMessage.error(t('manager.quickViewOptimization.loadTasksFailed'))
    }
  } else if (route.query.create === '1') {
    await openCreateDialog()
  }
})
</script>

<style scoped>
.quick-view-optimization {
  padding: 20px;
}

.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.page-title {
  color: var(--addp-text-primary);
  font-size: 18px;
  font-weight: 600;
}

.page-subtitle {
  color: var(--addp-text-secondary);
  font-size: 13px;
  margin-top: 4px;
}

.workflow-alert {
  margin-bottom: 14px;
}

.tab-toolbar,
.filter-bar {
  display: flex;
  gap: 10px;
  align-items: center;
  margin-bottom: 14px;
  flex-wrap: wrap;
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
