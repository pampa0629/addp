<template>
  <div class="tile-cache">
    <el-card>
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.tileCache.tasksTab')" name="tasks">
          <div class="tab-toolbar">
            <el-button type="primary" :icon="Plus" @click="openCreateDialog">
              {{ t('manager.tileCache.create') }}
            </el-button>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column prop="name" :label="t('manager.tileCache.name')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.tileCache.engine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">{{ engineName(row.target?.source_engine_id) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.resource')" min-width="260" show-overflow-tooltip>
              <template #default="{ row }">{{ taskResource(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.format')" width="100">
              <template #default="{ row }">{{ row.tile?.format || 'mvt' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.zoom')" width="110">
              <template #default="{ row }">
                {{ row.tile?.min_zoom ?? 0 }} - {{ row.tile?.max_zoom ?? 18 }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.schedule')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">
                <ScheduleDisplay :cron="row.schedule" :empty-text="t('manager.tileCache.manualOnly')" />
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.enabled')" width="100">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.tileCache.enabledYes') : t('manager.tileCache.enabledNo') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.lastExecutionStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(lastExecutionStatus(row))">
                  {{ executionStatusLabel(lastExecutionStatus(row)) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.lastRunAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.last_run_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.actions')" width="420" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.tileCache.execute') }}
                  </el-button>
                  <el-button size="small" @click="openEditDialog(row)">{{ t('manager.tileCache.edit') }}</el-button>
                  <el-button size="small" @click="viewTaskResults(row)">{{ t('manager.tileCache.results') }}</el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                    {{ t('manager.tileCache.monitor') }}
                  </el-button>
                  <el-button size="small" @click="showTaskDetail(row)">{{ t('manager.tileCache.detail') }}</el-button>
                  <el-button size="small" type="danger" @click="deleteTask(row)">{{ t('manager.tileCache.delete') }}</el-button>
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

        <el-tab-pane :label="t('manager.tileCache.resultsTab')" name="results">
          <div class="filter-bar">
            <div v-if="resultTaskFilterLabel" class="task-filter-chip">
              <el-tag type="primary" closable @close="clearResultTaskFilter">
                {{ resultTaskFilterLabel }}
              </el-tag>
            </div>
            <el-select v-model="resultFilters.status" clearable :placeholder="t('manager.tileCache.resultStatus')">
              <el-option v-for="status in resultStatuses" :key="status" :label="resultStatusLabel(status)" :value="status" />
            </el-select>
            <el-input v-model="resultFilters.q" clearable :placeholder="t('manager.tileCache.keywordPlaceholder')" @keyup.enter="applyResultFilters" />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.tileCache.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.tileCache.reset') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadResults" />
          </div>

          <el-table :data="results" v-loading="resultsLoading" stripe>
            <el-table-column :label="t('manager.tileCache.engine')" min-width="160" show-overflow-tooltip>
              <template #default="{ row }">{{ resultEngineName(row) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.sourceDataPath')" min-width="220" show-overflow-tooltip>
              <template #default="{ row }">{{ resultSourceDataPath(row) }}</template>
            </el-table-column>
            <el-table-column prop="tile_format" :label="t('manager.tileCache.format')" width="100" />
            <el-table-column :label="t('manager.tileCache.resultStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="resultStatusTagType(row.status)">
                  {{ resultStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.zoom')" width="110">
              <template #default="{ row }">{{ row.min_zoom ?? '-' }} - {{ row.max_zoom ?? '-' }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.storageLocation')" min-width="260" show-overflow-tooltip>
              <template #default="{ row }">
                <span :title="storageLocationDetail(row.storage_ref)">{{ storageLocation(row.storage_ref) }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="error_message" :label="t('manager.tileCache.error')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.tileCache.updatedAt')" width="170">
              <template #default="{ row }">{{ formatDateTime(row.updated_at) }}</template>
            </el-table-column>
            <el-table-column :label="t('manager.tileCache.actions')" width="180" fixed="right">
              <template #default="{ row }">
                <div class="row-actions">
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openResultExecution(row)">
                    {{ t('manager.tileCache.monitor') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteResult(row)">{{ t('manager.tileCache.delete') }}</el-button>
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
      <el-form ref="formRef" :model="form" :rules="rules" label-width="120px" v-loading="capabilityLoading">
        <div class="form-section-title">{{ t('manager.tileCache.basicInfo') }}</div>
        <el-form-item :label="t('manager.tileCache.name')" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('manager.tileCache.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('manager.tileCache.enabled')">
          <el-switch v-model="form.enabled" />
        </el-form-item>

        <div class="form-section-title">{{ t('manager.tileCache.sourceData') }}</div>
        <el-descriptions v-if="!showResourcePicker" class="source-summary" :column="2" border>
          <el-descriptions-item :label="t('manager.tileCache.engine')">
            {{ engineName(form.config.target.source_engine_id) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.tileCache.resource')">
            {{ sourceResourceText }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.tileCache.sourceSrid')">
            {{ form.config.tile.source_srid || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('manager.tileCache.extentSrid')">
            {{ form.config.tile.extent_srid || '-' }}
          </el-descriptions-item>
        </el-descriptions>
        <template v-else>
          <el-form-item :label="t('manager.tileCache.sourceTable')" prop="config.target.locator">
            <div class="resource-picker">
              <el-alert
                v-if="sourceSelectionLocked"
                type="info"
                :title="t('manager.tileCache.sourceLockedHint')"
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
                :title="t('manager.tileCache.resourceTreeTitle')"
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
                  <el-tag size="small" type="success">{{ t('manager.tileCache.spatialTable') }}</el-tag>
                  <span class="selected-resource__name">{{ sourceResourceText }}</span>
                </div>
                <div class="selected-resource__meta">
                  {{ selectedResourceSummary }}
                </div>
              </div>
            </div>
          </el-form-item>
        </template>

        <div class="form-section-title">{{ t('manager.tileCache.tileSettings') }}</div>
        <el-alert
          v-if="tileCacheOptimizationAdvice.visible"
          type="warning"
          :closable="false"
          show-icon
          class="optimization-advice"
          :title="tileCacheOptimizationAdvice.message"
        >
          <template #default>
            <el-button size="small" type="warning" @click="openQuickViewOptimizationCreate">
              {{ t('manager.spatialPreview.optimizeQuickView') }}
            </el-button>
          </template>
        </el-alert>
        <el-form-item :label="t('manager.tileCache.format')">
          <el-tag type="info">mvt</el-tag>
        </el-form-item>
        <el-form-item :label="t('manager.tileCache.zoom')" required>
          <div class="zoom-row">
            <el-input-number v-model="form.config.tile.min_zoom" :min="0" :max="form.config.tile.max_zoom" controls-position="right" />
            <span>-</span>
            <el-input-number v-model="form.config.tile.max_zoom" :min="form.config.tile.min_zoom" :max="22" controls-position="right" />
          </div>
        </el-form-item>
        <el-form-item :label="t('manager.tileCache.targetSrid')">
          <el-tag type="info">{{ form.config.tile.target_srid }}</el-tag>
        </el-form-item>
        <el-form-item :label="t('manager.tileCache.geometryColumn')" prop="config.options.geometry_column">
          <el-select v-model="form.config.options.geometry_column" filterable :placeholder="t('manager.tileCache.geometryColumnRequired')">
            <el-option
              v-for="column in geometryColumnOptions"
              :key="column"
              :label="column"
              :value="column"
            />
          </el-select>
        </el-form-item>

        <div class="form-section-title">{{ t('manager.tileCache.scheduleSettings') }}</div>
        <el-form-item :label="t('manager.tileCache.schedule')">
          <ScheduleConfig v-model="form.schedule" :allow-custom-cron="false" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formDialogVisible = false">{{ t('manager.tileCache.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTask">{{ t('manager.tileCache.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailDialogVisible" :title="t('manager.tileCache.dialogTitle')" width="920px">
      <el-descriptions v-if="selectedTask" :column="2" border>
        <el-descriptions-item :label="t('manager.tileCache.id')">{{ selectedTask.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.name')">{{ selectedTask.name }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.description')" :span="2">
          {{ selectedTask.description || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.engine')">
          {{ engineName(selectedTask.target?.source_engine_id) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.resource')" :span="2">{{ taskResource(selectedTask) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.geometryColumn')">
          {{ selectedTask.tile?.geometry_column || selectedTask.config?.options?.geometry_column || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.format')">
          {{ selectedTask.tile?.format || selectedTask.config?.tile?.format || 'mvt' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.zoom')">
          {{ selectedTask.tile?.min_zoom ?? selectedTask.config?.tile?.min_zoom ?? '-' }} -
          {{ selectedTask.tile?.max_zoom ?? selectedTask.config?.tile?.max_zoom ?? '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.sourceSrid')">
          {{ selectedTask.config?.tile?.source_srid || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.targetSrid')">
          {{ selectedTask.tile?.target_srid || selectedTask.config?.tile?.target_srid || 3857 }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.enabled')">
          <el-tag :type="selectedTask.enabled ? 'success' : 'info'">
            {{ selectedTask.enabled ? t('manager.tileCache.enabledYes') : t('manager.tileCache.enabledNo') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.lastExecutionStatus')">
          <div class="status-with-source">
            <el-tag :type="executionStatusTagType(selectedTaskLastExecutionStatus)">
              {{ executionStatusLabel(selectedTaskLastExecutionStatus) }}
            </el-tag>
            <span class="status-source">{{ selectedTaskLastExecutionStatusSource }}</span>
          </div>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.lastRunAt')">
          {{ formatDateTime(selectedTaskLastRunAt) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.lastExecutionId')" :span="2">
          {{ selectedTask.last_execution_id || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.tileCache.schedule')">
          <ScheduleDisplay :cron="selectedTask.schedule" :empty-text="t('manager.tileCache.manualOnly')" />
        </el-descriptions-item>
      </el-descriptions>

      <div v-if="selectedTask" class="execution-stats" v-loading="detailExecutionLoading">
        <div class="form-section-title">{{ t('manager.tileCache.executionStats') }}</div>
        <el-empty
          v-if="!detailExecutionLoading && !executionStatsAvailable"
          :description="selectedTask.last_execution_id ? t('manager.tileCache.noExecutionStats') : t('manager.tileCache.noExecutionYet')"
        />
        <template v-else-if="executionStatsAvailable">
          <el-alert
            v-if="executionStatsCheck.visible"
            :type="executionStatsCheck.type"
            :closable="false"
            :title="executionStatsCheck.message"
            class="stats-check-alert"
            show-icon
          />
          <div class="stats-grid">
            <div v-for="item in executionStatItems" :key="item.key" class="stat-item">
              <span class="stat-label">{{ item.label }}</span>
              <span class="stat-value">{{ item.value }}</span>
            </div>
          </div>
          <el-table
            v-if="zoomLevelRows.length"
            :data="zoomLevelRows"
            size="small"
            stripe
            class="zoom-stats-table"
          >
            <el-table-column prop="zoom" :label="t('manager.tileCache.zoomLevel')" width="90" />
            <el-table-column prop="totalTilesText" :label="t('manager.tileCache.tilesTotalEstimate')" min-width="120" />
            <el-table-column prop="generatedTilesText" :label="t('manager.tileCache.generatedTiles')" min-width="110" />
            <el-table-column prop="emptyTilesText" :label="t('manager.tileCache.emptyTiles')" min-width="100" />
            <el-table-column prop="skippedTilesText" :label="t('manager.tileCache.skippedTiles')" min-width="100" />
            <el-table-column prop="oversizedTilesText" :label="t('manager.tileCache.oversizedSkippedTiles')" min-width="100" />
            <el-table-column prop="failedTilesText" :label="t('manager.tileCache.failedTiles')" min-width="100" />
            <el-table-column prop="avgSizeText" :label="t('manager.tileCache.avgTileSize')" min-width="110" />
            <el-table-column prop="maxSizeText" :label="t('manager.tileCache.maxTileSize')" min-width="110" />
          </el-table>
        </template>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowDown, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { openMonitorExecution, parseLocator, ResourceTree, ScheduleConfig, ScheduleDisplay } from '@addp/common-frontend'
import client from '../api/client'
import { quickViewAPI } from '../api/quickView'
import { useTileCacheExecutionStats } from '../composables/useTileCacheExecutionStats'
import { formatDateTime } from '../utils/formatters'
import {
  executionIDFromExecution,
  executionRunAtFromExecution,
  executionStatusFromExecution,
  executionStatusTagType,
  lastExecutionStatus,
  resultLocatorInfo as resultLocatorInfoValue,
  resultStatusTagType,
  resourceTextFromLocator as resourceTextFromLocatorValue,
  storageLocationKey,
  taskResource as taskResourceValue
} from '../utils/tileCacheDisplay'
import {
  createDefaultTileCacheTaskForm,
  createTileCacheTaskFormFromTask,
  createTileCacheTaskPayload
} from '../utils/tileCacheTaskForm'
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
} from '../utils/tileCacheResourceTree'

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

const results = ref([])
const resultsLoading = ref(false)
const resultsPage = ref(1)
const resultsPageSize = ref(20)
const resultsTotal = ref(0)
const resultFilters = reactive({ item_id: undefined, item_fingerprint: '', task_id: undefined, status: '', q: '' })
const resultStatuses = ['generating', 'ready', 'failed', 'cancelled', 'deleted']
const selectedResultTask = ref(null)

const formRef = ref(null)
const formDialogVisible = ref(false)
const saving = ref(false)
const editingId = ref(null)
const form = reactive(createDefaultTileCacheTaskForm())
const detailDialogVisible = ref(false)
const selectedTask = ref(null)
const selectedTaskExecution = ref(null)
const detailExecutionLoading = ref(false)
let detailExecutionRequestSeq = 0
let taskRefreshTimer = null
const capabilityLoading = ref(false)
const tileCacheOptimizationAdvice = reactive({ visible: false, message: '' })
const geometryColumnOptions = ref([])
const selectedSourceNode = ref(null)
const databaseEngineTypes = new Set(['postgresql', 'postgres', 'postgis'])
const routeSourceLocked = computed(() => {
  return !!route.query.engine_id && !!route.query.schema && !!route.query.table
})
const sourceSelectionLocked = computed(() => {
  return !!editingId.value || routeSourceLocked.value
})
const showResourcePicker = computed(() => {
  return !routeSourceLocked.value || !!editingId.value
})

const formTitle = computed(() => editingId.value ? t('manager.tileCache.editTitle') : t('manager.tileCache.createTitle'))
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
  return `${t('manager.tileCache.currentTask')}: ${selectedResultTask.value.name || taskResource(selectedResultTask.value)}`
})
const selectedExecutionMetadata = computed(() => {
  const metadata = selectedTaskExecution.value?.metadata || selectedTaskExecution.value?.Metadata || null
  return metadata && typeof metadata === 'object' ? metadata : {}
})
const selectedTaskLastExecutionStatus = computed(() => {
  return executionStatusFromExecution(selectedTaskExecution.value) || lastExecutionStatus(selectedTask.value)
})
const selectedTaskLastExecutionStatusSource = computed(() => {
  return executionStatusFromExecution(selectedTaskExecution.value)
    ? t('manager.tileCache.executionStatusSourceRealtime')
    : t('manager.tileCache.executionStatusSourceTaskSnapshot')
})
const selectedTaskLastRunAt = computed(() => {
  return executionRunAtFromExecution(selectedTaskExecution.value) || selectedTask.value?.last_run_at
})
const {
  executionStatsAvailable,
  executionStatsCheck,
  executionStatItems,
  zoomLevelRows
} = useTileCacheExecutionStats({
  t,
  metadata: selectedExecutionMetadata
})
const rules = computed(() => ({
  name: [{ required: true, message: t('manager.tileCache.nameRequired'), trigger: 'blur' }],
  'config.target.locator': [{ required: true, message: t('manager.tileCache.sourceTableRequired'), trigger: 'change' }],
  'config.options.geometry_column': [{ required: true, message: t('manager.tileCache.geometryColumnRequired'), trigger: 'change' }]
}))

const resetForm = (task = null) => {
  const next = createTileCacheTaskFormFromTask(task)
  geometryColumnOptions.value = []
  selectedSourceNode.value = null
  resourceCurrentKey.value = ''
  tileCacheOptimizationAdvice.visible = false
  tileCacheOptimizationAdvice.message = ''
  Object.assign(form, next)
  if (form.config.options.geometry_column) {
    geometryColumnOptions.value = [form.config.options.geometry_column]
  }
  editingId.value = task?.id || null
}

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const response = await client.get('/manager/tile_cache_tasks', {
      params: { page: tasksPage.value, page_size: tasksPageSize.value }
    })
    tasks.value = response.data || []
    tasksTotal.value = response.total || 0
    scheduleRunningTaskRefresh()
  } catch (error) {
    console.error('加载瓦片缓存任务失败:', error)
    ElMessage.error(t('manager.tileCache.loadTasksFailed'))
  } finally {
    tasksLoading.value = false
  }
}

const loadEngines = async (force = false) => {
  if (engineOptions.value.length && !force) {
    return engineOptions.value
  }
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
  if (resourceTreeData.value.length && !force) {
    return
  }
  resourceLoading.value = true
  try {
    const engines = await loadEngines(force)
    resourceTreeData.value = engines.map(createResourceRootNode)
    resourceExpandedKeys.value = []
  } catch (error) {
    console.error('加载瓦片缓存资源树失败:', error)
    ElMessage.error(t('manager.tileCache.loadResourceTreeFailed'))
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
    const engine = {
      id: node.engineId,
      engine_type: node.engineType,
      name: node.engineName
    }
    const normalized = normalizeResourceNode(tree, engine, { parseLocator: safeParseLocator, loaded: true })
    if (!normalized) return
    resourceTreeData.value = replaceResourceNode(resourceTreeData.value, node.locator || node.id, normalized)
    if (!resourceExpandedKeys.value.includes(normalized.id)) {
      resourceExpandedKeys.value = [...resourceExpandedKeys.value, normalized.id]
    }
  } catch (error) {
    console.error('加载瓦片缓存资源树失败:', error)
    ElMessage.error(t('manager.tileCache.loadResourceTreeFailed'))
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
    const engine = {
      id: node.engineId,
      engine_type: node.engineType,
      name: node.engineName
    }
    const children = (response.children || [])
      .map((child) => normalizeResourceNode(child, engine, { parseLocator: safeParseLocator }))
      .filter(Boolean)
    resourceTreeData.value = updateResourceNodeChildren(resourceTreeData.value, locator, children)
  } catch (error) {
    console.error('加载瓦片缓存资源子节点失败:', error)
    ElMessage.error(t('manager.tileCache.loadResourceTreeFailed'))
  } finally {
    resourceLoading.value = false
  }
}

const loadResults = async () => {
  resultsLoading.value = true
  try {
    const params = {
      page: resultsPage.value,
      page_size: resultsPageSize.value,
      status: resultFilters.status || undefined,
      q: resultFilters.q || undefined,
      task_id: resultFilters.task_id || undefined,
      item_fingerprint: resultFilters.item_fingerprint || undefined
    }
    const response = await client.get('/manager/tile_cache', { params })
    results.value = response.data || []
    resultsTotal.value = response.total || 0
  } catch (error) {
    console.error('加载瓦片缓存结果失败:', error)
    ElMessage.error(t('manager.tileCache.loadResultsFailed'))
  } finally {
    resultsLoading.value = false
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
    clearTaskRefreshTimer()
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

const parseQueryNumber = (value, defaultValue = 0) => {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : defaultValue
}

const parseQueryList = (value) => {
  if (!value) return []
  return String(value)
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

const parseExtent = (value) => {
  const values = parseQueryList(value).map(Number)
  return values.length === 4 && values.every((item) => Number.isFinite(item)) ? values : []
}

const setGeometryOptions = (columns, selected = '') => {
  const options = []
  for (const column of columns || []) {
    const normalized = String(column || '').trim()
    if (normalized && !options.includes(normalized)) {
      options.push(normalized)
    }
  }
  const current = String(selected || '').trim()
  if (current && !options.includes(current)) {
    options.unshift(current)
  }
  geometryColumnOptions.value = options
  if (!form.config.options.geometry_column && options.length === 1) {
    form.config.options.geometry_column = options[0]
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
  if (route.query.geom) form.config.options.geometry_column = String(route.query.geom)
  form.config.tile.source_srid = parseQueryNumber(route.query.source_srid, form.config.tile.source_srid)
  form.config.tile.extent_srid = parseQueryNumber(route.query.extent_srid, form.config.tile.extent_srid)
  const extent = parseExtent(route.query.extent)
  if (extent.length === 4) {
    form.config.tile.extent = extent
  }
  setGeometryOptions(parseQueryList(route.query.geometry_columns), form.config.options.geometry_column)
}

const applyRouteResultContext = () => {
  if (route.query.item_fingerprint) {
    resultFilters.item_fingerprint = String(route.query.item_fingerprint)
  }
  if (route.query.item_id) {
    resultFilters.item_id = Number(route.query.item_id)
  }
}

const applyQuickViewCapabilityToForm = (config, fallbackGeometryColumns = []) => {
  form.config.tile.min_zoom = Number(config.min_zoom ?? form.config.tile.min_zoom)
  form.config.tile.max_zoom = Number(config.max_zoom ?? form.config.tile.max_zoom)
  form.config.tile.target_srid = Number(config.target_srid || 3857)
  form.config.tile.source_srid = Number(config.source_srid || form.config.tile.source_srid || 0)
  form.config.tile.extent_srid = Number(config.extent_srid || config.srid || form.config.tile.extent_srid || 0)
  if (Array.isArray(config.extent) && config.extent.length === 4) {
    form.config.tile.extent = config.extent.map(Number)
  }
  const columns = [
    ...(config.geometry_columns || []),
    ...fallbackGeometryColumns
  ]
  setGeometryOptions(columns, form.config.options.geometry_column || config.geometry_column)
  if (!form.config.options.geometry_column && config.geometry_column) {
    form.config.options.geometry_column = config.geometry_column
  }
  tileCacheOptimizationAdvice.visible = !!config.optimization_recommended
  tileCacheOptimizationAdvice.message = config.optimization_message || t('manager.spatialPreview.optimizationRecommended')
}

const tileCacheFormConfigFromCapability = (capability) => {
  const quickView = capability?.quick_view || {}
  const renderFacts = capability?.render_facts || {}
  const realtime = capability?.realtime_tile || {}
  const optimization = capability?.optimization || {}
  const zoom = renderFacts.zoom_recommendation || {}
  return {
    ...quickView,
    min_zoom: zoom.min_zoom ?? quickView.min_zoom,
    max_zoom: zoom.max_zoom ?? quickView.max_zoom,
    source_srid: renderFacts.source_srid ?? quickView.source_srid,
    extent: Array.isArray(renderFacts.render_extent) ? renderFacts.render_extent : quickView.extent,
    extent_srid: renderFacts.render_extent_srid ?? quickView.extent_srid,
    geometry_column: quickView.geometry_column,
    geometry_columns: quickView.geometry_columns || [],
    optimization_recommended: realtime.optimization_recommended === true ||
      realtime.performance_mode === 'source_transform_path' ||
      (optimization.available !== true && Number(renderFacts.source_srid || quickView.source_srid || 0) !== 3857),
    optimization_message: realtime.optimization_recommendation || t('manager.spatialPreview.optimizationRecommended')
  }
}

const openQuickViewOptimizationCreate = () => {
  const target = form.config.target
  router.push({
    name: 'QuickViewOptimization',
    query: {
      tab: 'tasks',
      create: '1',
      engine_id: String(target.source_engine_id || ''),
      schema: target.schema || '',
      table: target.table || '',
      locator: target.locator || '',
      ...(target.item_id ? { item_id: String(target.item_id) } : {}),
      ...(target.item_fingerprint ? { item_fingerprint: target.item_fingerprint } : {}),
      ...(form.config.options.geometry_column ? { geom: form.config.options.geometry_column } : {}),
      ...(geometryColumnOptions.value.length ? { geometry_columns: geometryColumnOptions.value.join(',') } : {}),
      ...(form.config.tile.source_srid ? { source_srid: String(form.config.tile.source_srid) } : {})
    }
  })
}

const loadQuickViewCapabilityForForm = async (fallbackGeometryColumns = []) => {
  const locator = String(form.config.target.locator || '').trim()
  if (!locator) return null
  capabilityLoading.value = true
  try {
    const capability = await quickViewAPI.getQuickViewCapabilityByLocator(locator)
    const config = tileCacheFormConfigFromCapability(capability)
    applyQuickViewCapabilityToForm(config, fallbackGeometryColumns)
    return config
  } catch (error) {
    console.error('加载快显能力失败:', error)
    ElMessage.error(t('manager.tileCache.loadCapabilityFailed'))
    return null
  } finally {
    capabilityLoading.value = false
  }
}

const safeParseLocator = (locator) => {
  try {
    return parseLocator(locator)
  } catch {
    return null
  }
}

const selectableResourceType = (node) => {
  return tableSelectionFromResourceNode(node, safeParseLocator) ? t('manager.tileCache.spatialTable') : ''
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

const handleResourceNodeExpand = async (node) => {
  const locator = node?.locator || node?.id
  if (!locator || !node?.hasChildren || (node.children || []).length > 0) {
    return
  }
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
  if (!selection) {
    return
  }
  if (sourceSelectionLocked.value && selection.locator !== form.config.target.locator) {
    resourceCurrentKey.value = form.config.target.locator || ''
    ElMessage.info(t('manager.tileCache.sourceLockedHint'))
    return
  }
  const previousTarget = { ...form.config.target }
  const previousTile = JSON.parse(JSON.stringify(form.config.tile))
  const previousGeometryOptions = [...geometryColumnOptions.value]
  const previousGeometryColumn = form.config.options.geometry_column
  Object.assign(form.config.target, selection)
  form.config.options.geometry_column = ''
  const fallbackColumns = geometryColumnsFromNode(node)
  const config = await loadQuickViewCapabilityForForm(fallbackColumns)
  if (!config) {
    Object.assign(form.config.target, previousTarget)
    Object.assign(form.config.tile, previousTile)
    geometryColumnOptions.value = previousGeometryOptions
    form.config.options.geometry_column = previousGeometryColumn
    return
  }
  const columns = [
    ...(config?.geometry_columns || []),
    config?.geometry_column,
    ...fallbackColumns
  ].map((column) => String(column || '').trim()).filter(Boolean)
  if (!columns.length) {
    Object.assign(form.config.target, previousTarget)
    Object.assign(form.config.tile, previousTile)
    geometryColumnOptions.value = previousGeometryOptions
    form.config.options.geometry_column = previousGeometryColumn
    ElMessage.warning(t('manager.tileCache.spatialTableRequired'))
    return
  }
  selectedSourceNode.value = node
  resourceCurrentKey.value = selection.locator
  if (!form.name) {
    form.name = t('manager.tileCache.defaultTaskName', { resource: `${selection.schema}.${selection.table}` })
  }
  await formRef.value?.validateField('config.target.locator').catch(() => {})
}

const openCreateDialog = async () => {
  resetForm()
  formDialogVisible.value = true
  applyRouteSourceContext()
  if (showResourcePicker.value) {
    await loadResourceTrees()
  }
  await loadQuickViewCapabilityForForm()
  if (form.config.target.locator && showResourcePicker.value) {
    await revealSelectedResource()
  }
  if (!form.name && form.config.target.schema && form.config.target.table) {
    form.name = t('manager.tileCache.defaultTaskName', { resource: `${form.config.target.schema}.${form.config.target.table}` })
  }
}

const openEditDialog = async (task) => {
  resetForm(task)
  const columns = task?.config?.options?.geometry_column ? [task.config.options.geometry_column] : []
  setGeometryOptions(columns, task?.config?.options?.geometry_column || '')
  formDialogVisible.value = true
  await loadResourceTrees()
  await loadQuickViewCapabilityForForm(columns)
  await revealSelectedResource()
}

const saveTask = async () => {
  await formRef.value?.validate()
  saving.value = true
  try {
    const payload = createTileCacheTaskPayload(form)
    if (editingId.value) {
      await client.put(`/manager/tile_cache_tasks/${editingId.value}`, payload)
      ElMessage.success(t('manager.tileCache.updateSuccess'))
    } else {
      await client.post('/manager/tile_cache_tasks', payload)
      ElMessage.success(t('manager.tileCache.createSuccess'))
    }
    formDialogVisible.value = false
    await loadTasks()
  } catch (error) {
    console.error('保存瓦片缓存任务失败:', error)
    ElMessage.error(errorMessage(error, t('manager.tileCache.saveFailed')))
  } finally {
    saving.value = false
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await client.post(`/manager/tasks/tile_cache_generation/${task.id}/execute`, {
      trigger_type: 'manual',
      source: 'manager'
    })
    ElMessage.success(t('manager.tileCache.executeSubmitted'))
    await loadTasks()
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    console.error('执行瓦片缓存任务失败:', error)
    ElMessage.error(errorMessage(error, t('manager.tileCache.executeFailed')))
  } finally {
    executingId.value = null
  }
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
    selectedResultTask.value = await client.get(`/manager/tile_cache_tasks/${taskId}`)
  } catch (error) {
    console.error('加载瓦片缓存任务详情失败:', error)
    selectedResultTask.value = { id: taskId, name: t('manager.tileCache.taskWithId', { id: taskId }) }
  }
}

const deleteTask = async (task) => {
  await ElMessageBox.confirm(t('manager.tileCache.deleteTaskConfirm'), t('manager.tileCache.delete'), { type: 'warning' })
  await client.delete(`/manager/tile_cache_tasks/${task.id}`)
  ElMessage.success(t('manager.tileCache.deleteSuccess'))
  await loadTasks()
}

const deleteResult = async (result) => {
  await ElMessageBox.confirm(t('manager.tileCache.deleteResultConfirm'), t('manager.tileCache.delete'), { type: 'warning' })
  await client.delete(`/manager/tile_cache/${result.id}`)
  ElMessage.success(t('manager.tileCache.deleteSuccess'))
  await loadResults()
}

const loadTaskExecutionDetail = async (task) => {
  selectedTaskExecution.value = null
  detailExecutionRequestSeq += 1
  const seq = detailExecutionRequestSeq
  const executionId = String(task?.last_execution_id || '').trim()
  if (!executionId) {
    detailExecutionLoading.value = false
    return
  }
  detailExecutionLoading.value = true
  try {
    const response = await client.get(`/manager/executions/${executionId}`)
    const payload = response?.data || response
    if (seq === detailExecutionRequestSeq) {
      const execution = payload?.data || payload
      selectedTaskExecution.value = execution
      syncTaskLastExecutionFromExecution(task, execution)
    }
  } catch (error) {
    console.error('加载瓦片缓存执行详情失败:', error)
    if (seq === detailExecutionRequestSeq) {
      selectedTaskExecution.value = null
    }
  } finally {
    if (seq === detailExecutionRequestSeq) {
      detailExecutionLoading.value = false
    }
  }
}

const syncTaskLastExecutionFromExecution = (task, execution) => {
  const status = executionStatusFromExecution(execution)
  const executionID = executionIDFromExecution(execution)
  const taskExecutionID = String(task?.last_execution_id || task?.lastExecutionID || '').trim()
  if (!task || !status || !executionID || executionID !== taskExecutionID) {
    return
  }

  const patch = {
    last_execution_status: status,
    lastExecutionStatus: status,
    last_run_at: executionRunAtFromExecution(execution) || task.last_run_at
  }

  if (selectedTask.value?.id === task.id) {
    selectedTask.value = { ...selectedTask.value, ...patch }
  }
  tasks.value = tasks.value.map((row) => row.id === task.id ? { ...row, ...patch } : row)
  scheduleRunningTaskRefresh()
}

const showTaskDetail = (task) => {
  selectedTask.value = task
  detailDialogVisible.value = true
  loadTaskExecutionDetail(task)
}

const openTaskExecution = (task) => openMonitorExecution(task.last_execution_id)
const openResultExecution = (result) => openMonitorExecution(result.last_execution_id)

const taskResource = (task) => {
  return taskResourceValue(task, safeParseLocator)
}

const engineName = (engineId) => {
  const id = Number(engineId || 0)
  if (!id) return '-'
  const engine = engineOptions.value.find((item) => Number(item.id) === id)
  return engine?.name || t('manager.tileCache.engineWithId', { id })
}

const resultLocatorInfo = (result) => {
  return resultLocatorInfoValue(result, safeParseLocator)
}

const resultEngineName = (result) => {
  const info = resultLocatorInfo(result)
  return engineName(info?.engineId)
}

const resultSourceDataPath = (result) => {
  return resultLocatorInfo(result)?.path || '-'
}

const resourceTextFromLocator = (locator) => {
  return resourceTextFromLocatorValue(locator, safeParseLocator)
}

const storageLocation = (storageRef) => {
  if (!storageRef) return '-'
  return t(`manager.tileCache.${storageLocationKey(storageRef)}`)
}

const storageLocationDetail = (storageRef) => {
  if (!storageRef) return ''
  return t(`manager.tileCache.${storageLocationKey(storageRef)}`)
}

const errorMessage = (error, fallback) => {
  const data = error?.response?.data
  if (typeof data?.error === 'string' && data.error.trim()) return data.error
  if (typeof data?.message === 'string' && data.message.trim()) return data.message
  if (typeof error?.message === 'string' && error.message.trim()) return error.message
  return fallback
}

const executionStatusLabel = (status) => {
  if (!status) return t('manager.tileCache.statusNeverRun')
  if (!['pending', 'running', 'success', 'failed', 'timeout', 'cancelled'].includes(status)) return status
  return t(`manager.tileCache.status.${status}`)
}

const resultStatusLabel = (status) => {
  if (!status) return '-'
  return t(`manager.tileCache.resultStatuses.${status}`)
}

const hasRunningTileCacheTask = () => {
  return tasks.value.some((task) => ['pending', 'running'].includes(lastExecutionStatus(task)))
}

const clearTaskRefreshTimer = () => {
  if (taskRefreshTimer) {
    window.clearTimeout(taskRefreshTimer)
    taskRefreshTimer = null
  }
}

const scheduleRunningTaskRefresh = () => {
  clearTaskRefreshTimer()
  if (activeTab.value !== 'tasks' || document.visibilityState === 'hidden' || !hasRunningTileCacheTask()) {
    return
  }
  taskRefreshTimer = window.setTimeout(() => {
    taskRefreshTimer = null
    refreshTasksWhenVisible()
  }, 5000)
}

const refreshTasksWhenVisible = async () => {
  if (document.visibilityState === 'hidden' || activeTab.value !== 'tasks') {
    return
  }
  await loadTasks()
}

const handleVisibilityChange = () => {
  if (document.visibilityState === 'visible') {
    refreshTasksWhenVisible()
  }
}

onMounted(async () => {
  window.addEventListener('focus', refreshTasksWhenVisible)
  document.addEventListener('visibilitychange', handleVisibilityChange)
  await Promise.all([loadTasks(), loadEngines()])
  if (activeTab.value === 'results') {
    applyRouteResultContext()
    await loadResultTaskFilterFromRoute()
    await loadResults()
  }
  const taskId = Number(route.query.task_id || 0)
  if (taskId && activeTab.value !== 'results') {
    try {
      const response = await client.get(`/manager/tile_cache_tasks/${taskId}`)
      openEditDialog(response)
    } catch (error) {
      console.error('加载瓦片缓存任务详情失败:', error)
      ElMessage.error(t('manager.tileCache.loadTasksFailed'))
    }
  } else if (route.query.create === '1') {
    await openCreateDialog()
  }
})

onBeforeUnmount(() => {
  clearTaskRefreshTimer()
  window.removeEventListener('focus', refreshTasksWhenVisible)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>

<style scoped>
.tile-cache {
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

.filter-bar .el-input {
  width: 260px;
}

.filter-bar .el-select {
  width: 160px;
}

.task-filter-chip {
  max-width: 320px;
}

.task-filter-chip .el-tag {
  max-width: 100%;
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

.zoom-row {
  display: flex;
  gap: 12px;
  align-items: center;
}

.source-summary {
  margin-bottom: 16px;
}

.optimization-advice {
  margin-bottom: 14px;
}

.execution-stats {
  margin-top: 18px;
}

.status-with-source {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.status-source {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.stats-check-alert {
  margin-bottom: 12px;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 10px;
  margin-bottom: 14px;
}

.stat-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  background: var(--el-fill-color-lighter);
}

.stat-label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.2;
}

.stat-value {
  overflow: hidden;
  color: var(--el-text-color-primary);
  font-size: 16px;
  font-weight: 650;
  line-height: 1.3;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.zoom-stats-table {
  margin-top: 4px;
}

.resource-picker {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
}

.resource-node-label {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  max-width: 100%;
  min-width: 0;
}

.resource-root-caret {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.resource-node-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.selected-resource {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  background: var(--el-fill-color-light);
}

.selected-resource__main {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.selected-resource__name {
  overflow: hidden;
  color: var(--el-text-color-primary);
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.selected-resource__meta {
  overflow: hidden;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.form-section-title {
  font-weight: 600;
  margin: 14px 0 10px;
  color: var(--addp-text-primary);
}
</style>
