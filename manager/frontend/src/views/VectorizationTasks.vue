<template>
  <div class="vectorization">
    <el-card>
      <el-tabs v-model="activeTab" @tab-change="handleTabChange">
        <el-tab-pane :label="t('manager.vectorization.tasksTab')" name="tasks">
          <div class="tab-toolbar task-tab-toolbar">
            <el-button type="primary" :icon="Plus" @click="requestCreateDialog">
              {{ t('manager.vectorization.create') }}
            </el-button>
            <el-button :icon="Refresh" circle @click="loadTasks" />
          </div>

          <el-table class="vectorization-table" :data="tasks" v-loading="tasksLoading" stripe>
            <el-table-column prop="name" :label="t('manager.vectorization.name')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.vectorization.engine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">
                {{ engineName(row.target?.engine_id) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.targetType')" width="110">
              <template #default="{ row }">
                <el-tag size="small" :type="row.target?.scope === 'item' ? 'success' : 'primary'">
                  {{ targetScopeLabel(row.target?.scope) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.resource')" min-width="280" show-overflow-tooltip>
              <template #default="{ row }">
                {{ targetDisplay(row) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.recursive')" width="100">
              <template #default="{ row }">
                <el-tag v-if="row.target?.scope === 'node'" :type="row.target?.recursive ? 'success' : 'info'">
                  {{ row.target?.recursive ? t('common.yes') : t('common.no') }}
                </el-tag>
                <span v-else>-</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.schedule')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">
                <ScheduleDisplay :cron="row.schedule" :empty-text="t('manager.vectorization.manualOnly')" />
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.enabled')" width="100">
              <template #default="{ row }">
                <el-tag :type="row.enabled ? 'success' : 'info'">
                  {{ row.enabled ? t('manager.vectorization.enabledYes') : t('manager.vectorization.enabledNo') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.lastStatus')" width="140">
              <template #default="{ row }">
                <el-tag :type="executionStatusTagType(row.last_execution_status)">
                  {{ executionStatusLabel(row.last_execution_status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.lastRunAt')" width="170">
              <template #default="{ row }">
                {{ formatDateTime(row.last_run_at) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.actions')" width="400">
              <template #default="{ row }">
                <div class="task-actions">
                  <el-button type="primary" size="small" :loading="executingId === row.id" @click="executeTask(row)">
                    {{ t('manager.vectorization.execute') }}
                  </el-button>
                  <el-button size="small" @click="requestEditTask(row)">
                    {{ t('manager.vectorization.edit') }}
                  </el-button>
                  <el-button size="small" @click="viewTaskResults(row)">
                    {{ t('manager.vectorization.results') }}
                  </el-button>
                  <el-button size="small" :disabled="!row.last_execution_id" @click="openTaskExecution(row)">
                    {{ t('manager.vectorization.monitor') }}
                  </el-button>
                  <el-button size="small" @click="showTaskDetail(row)">
                    {{ t('manager.vectorization.detail') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteTask(row)">
                    {{ t('manager.vectorization.delete') }}
                  </el-button>
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

        <el-tab-pane :label="t('manager.vectorization.resultsTab')" name="results">
          <div class="filter-bar">
            <el-select
              v-model="resultFilters.engine_id"
              clearable
              filterable
              :placeholder="t('manager.vectorization.engine')"
              @change="handleResultEngineChange"
            >
              <el-option
                v-for="engine in engineOptions"
                :key="engine.id"
                :label="engine.name"
                :value="engine.id"
              />
            </el-select>
            <div class="node-filter-control">
              <el-input
                :model-value="resultNodeFilterLabel"
                readonly
                clearable
                :placeholder="t('manager.vectorization.selectNode')"
                @clear="clearResultNodeFilter()"
              />
              <el-button
                v-if="resultNodeFilterLabel"
                :icon="Close"
                circle
                :title="t('manager.vectorization.clear')"
                @click="clearResultNodeFilter()"
              />
              <el-button @click="openResultNodeDialog">{{ t('manager.vectorization.select') }}</el-button>
            </div>
            <el-input-number
              v-model="resultFilters.item_id"
              :min="0"
              :placeholder="t('manager.vectorization.item')"
              controls-position="right"
            />
            <el-select v-model="resultFilters.status" clearable :placeholder="t('manager.vectorization.resultStatus')">
              <el-option
                v-for="status in embeddingStatuses"
                :key="status"
                :label="embeddingStatusLabel(status)"
                :value="status"
              />
            </el-select>
            <el-input
              v-model="resultFilters.q"
              clearable
              :placeholder="t('manager.vectorization.keywordPlaceholder')"
              @keyup.enter="applyResultFilters"
            />
            <el-button type="primary" @click="applyResultFilters">{{ t('manager.vectorization.search') }}</el-button>
            <el-button @click="resetResultFilters">{{ t('manager.vectorization.reset') }}</el-button>
            <el-button :icon="Refresh" circle @click="loadResults" />
          </div>

          <el-table class="vectorization-table" :data="results" v-loading="resultsLoading" stripe>
            <el-table-column prop="item_id" :label="t('manager.vectorization.item')" width="100" />
            <el-table-column :label="t('manager.vectorization.engine')" min-width="140" show-overflow-tooltip>
              <template #default="{ row }">
                {{ engineName(row.engine_id) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.resourcePath')" min-width="260" show-overflow-tooltip>
              <template #default="{ row }">
                {{ resultResourcePath(row) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.resultStatus')" width="130">
              <template #default="{ row }">
                <el-tag :type="embeddingStatusTagType(row.status)">
                  {{ embeddingStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="model" :label="t('manager.vectorization.model')" min-width="150" show-overflow-tooltip />
            <el-table-column prop="dimension" :label="t('manager.vectorization.dimension')" width="110" />
            <el-table-column :label="t('manager.vectorization.vectorizedAt')" width="180">
              <template #default="{ row }">
                {{ formatDateTime(row.vectorized_at) }}
              </template>
            </el-table-column>
            <el-table-column :label="t('manager.vectorization.monitor')" width="100">
              <template #default="{ row }">
                <el-button size="small" :disabled="!row.last_execution_id" @click="openResultExecution(row)">
                  {{ t('manager.vectorization.monitor') }}
                </el-button>
              </template>
            </el-table-column>
            <el-table-column prop="error_message" :label="t('manager.vectorization.error')" min-width="180" show-overflow-tooltip />
            <el-table-column :label="t('manager.vectorization.actions')" width="290">
              <template #default="{ row }">
                <div class="task-actions">
                  <el-button size="small" :disabled="!row.locator" @click="locateResult(row)">
                    {{ t('manager.vectorization.locate') }}
                  </el-button>
                  <el-button size="small" :loading="revectorizingId === row.id" @click="revectorizeResult(row)">
                    {{ t('manager.vectorization.revectorize') }}
                  </el-button>
                  <el-button size="small" type="danger" @click="deleteResult(row)">
                    {{ t('manager.vectorization.delete') }}
                  </el-button>
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

    <el-dialog v-model="formDialogVisible" :title="formTitle" width="960px" class="vectorization-task-dialog" @closed="clearFormDialogRoute">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="120px">
        <div class="form-section-title">{{ t('manager.vectorization.basicInfo') }}</div>
        <el-form-item :label="t('manager.vectorization.name')" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>

        <div class="form-section-title">{{ t('manager.vectorization.resourceScope') }}</div>
        <el-form-item :label="t('manager.vectorization.resource')" prop="target.locator">
          <div class="resource-picker">
            <ResourceTree
              :tree-data="resourceTreeData"
              :loading="resourceLoading"
              :show-refresh-button="true"
              :show-count="false"
              :expanded-keys="resourceExpandedKeys"
              :current-node-key="resourceCurrentKey"
              :default-expand-root="false"
              :expand-on-click-node="true"
              :title="t('manager.vectorization.resourceTreeTitle')"
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
            <div v-if="form.target.locator" class="selected-resource">
              <div class="selected-resource__main">
                <el-tag size="small" :type="form.target.scope === 'item' ? 'success' : 'primary'">
                  {{ targetScopeLabel(form.target.scope) }}
                </el-tag>
                <span class="selected-resource__name">{{ form.target.label || targetNameFromLocator(form.target.locator) }}</span>
              </div>
              <div class="selected-resource__meta" :title="form.target.locator">
                {{ selectedResourceSummary }}
              </div>
            </div>
          </div>
        </el-form-item>
        <el-form-item v-if="form.target.scope === 'node'" :label="t('manager.vectorization.recursive')">
          <el-switch v-model="form.target.recursive" />
        </el-form-item>

        <div class="form-section-title">{{ t('manager.vectorization.scheduleSettings') }}</div>
        <el-form-item :label="t('manager.vectorization.enabled')">
          <el-switch v-model="form.enabled" />
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.schedule')">
          <ScheduleConfig v-model="form.schedule" :allow-custom-cron="false" />
        </el-form-item>

        <div class="form-section-title">{{ t('manager.vectorization.embeddingSettings') }}</div>
        <el-form-item :label="t('manager.vectorization.extensions')">
          <div class="extension-list">
            <el-tag v-for="extension in supportedExtensions" :key="extension" size="small" type="info">
              {{ extension }}
            </el-tag>
          </div>
        </el-form-item>
        <el-form-item :label="t('manager.vectorization.maxFileSize')">
          <el-input-number v-model="form.filters.max_file_size_mb" :min="1" :max="1024" />
          <span class="form-hint">{{ t('manager.vectorization.maxFileSizeHint') }}</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formDialogVisible = false">{{ t('manager.vectorization.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTask">{{ t('manager.vectorization.save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="resultNodeDialogVisible" :title="t('manager.vectorization.selectNode')" width="760px">
      <ResourceTree
        :tree-data="resultResourceTreeData"
        :loading="resultResourceLoading"
        :show-refresh-button="true"
        :show-count="false"
        :expanded-keys="resultResourceExpandedKeys"
        :current-node-key="resultResourceCurrentKey"
        :default-expand-root="false"
        :expand-on-click-node="true"
        :title="t('manager.vectorization.resourceTreeTitle')"
        height="420px"
        @refresh="loadResultResourceTrees(true)"
        @node-click="handleResultResourceNodeClick"
        @node-expand="handleResultResourceNodeExpand"
        @update:expanded-keys="resultResourceExpandedKeys = $event"
        @update:current-node-key="resultResourceCurrentKey = $event"
      >
        <template #node-label="{ data }">
          <span class="resource-node-label">
            <el-icon v-if="isResourceRootNode(data)" class="resource-root-caret"><ArrowDown /></el-icon>
            <span class="resource-node-text">{{ data.label }}</span>
            <el-tag v-if="isSelectableResultNode(data)" size="small" type="primary">
              {{ t('manager.vectorization.targetNode') }}
            </el-tag>
          </span>
        </template>
      </ResourceTree>
      <template #footer>
        <el-button @click="clearResultNodeFilterAndClose">{{ t('manager.vectorization.clear') }}</el-button>
        <el-button @click="resultNodeDialogVisible = false">{{ t('manager.vectorization.cancel') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailDialogVisible" :title="t('manager.vectorization.dialogTitle')" width="760px">
      <el-descriptions v-if="selectedTask" :column="2" border>
        <el-descriptions-item :label="t('manager.vectorization.id')">{{ selectedTask.id }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.name')">{{ selectedTask.name }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.description')" :span="2">
          {{ selectedTask.description || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.engine')">{{ engineName(selectedTask.target?.engine_id) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.targetType')">{{ targetScopeLabel(selectedTask.target?.scope) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.item')">{{ selectedTask.target?.item_id || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.node')">{{ selectedTask.target?.node_id || '-' }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.resourcePath')" :span="2">
          {{ targetResourcePath(selectedTask) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.model')">
          {{ selectedTask.config?.embedding?.model || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.dimension')">
          {{ selectedTask.config?.embedding?.dimension || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.schedule')">
          <ScheduleDisplay :cron="selectedTask.schedule" :empty-text="t('manager.vectorization.manualOnly')" />
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.nextRunAt')">{{ formatDateTime(selectedTask.next_run_at) }}</el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.monitor')" :span="2">
          <el-button size="small" :disabled="!selectedTask.last_execution_id" @click="openTaskExecution(selectedTask)">
            {{ t('manager.vectorization.monitor') }}
          </el-button>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.lastStatus')">
          <el-tag :type="executionStatusTagType(selectedTask.last_execution_status)">
            {{ executionStatusLabel(selectedTask.last_execution_status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.lastRunAt')">
          {{ formatDateTime(selectedTask.last_run_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.createdAt')">
          {{ formatDateTime(selectedTask.created_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('manager.vectorization.updatedAt')">
          {{ formatDateTime(selectedTask.updated_at) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { navigateManagerRoute } from '@/utils/moduleNavigation'
import { resolveManagerTaskWorkspaceRouteState } from '@/utils/taskWorkspaceRoute'
import { ArrowDown, Close, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { engineRootLocator, openMonitorExecution, parseLocatorSafe, ResourceTree, ScheduleConfig, ScheduleDisplay, catalogRootTypeForEngine } from '@addp/common-frontend'
import client from '../api/client'
import { dataExplorerAPI } from '../api/dataExplorer'
import { formatDateTime } from '../utils/formatters'
import {
  DEFAULT_VECTOR_MAX_FILE_SIZE_MB,
  SUPPORTED_VECTOR_EXTENSIONS,
  isVectorizableObjectNode,
  isVectorizableRangeNode
} from '../utils/vectorization'
import { mergeAncestorChainIntoResourceTree } from '../utils/tileCacheResourceTree'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const resolveRouteState = routeQuery => resolveManagerTaskWorkspaceRouteState({
  routeQuery,
  allowedQueryByTab: {
    tasks: ['create', 'task_id'],
    results: ['task_id']
  }
})
const activeTab = ref(resolveRouteState(route.query).tab)
let routeDataReady = false
let workspaceRestoreSequence = 0

const results = ref([])
const resultsLoading = ref(false)
const resultsPage = ref(1)
const resultsPageSize = ref(20)
const resultsTotal = ref(0)
const revectorizingId = ref(null)
const resultFilters = reactive(defaultResultFilters())
const engineOptions = ref([])
const resultNodeDialogVisible = ref(false)
const resultResourceTreeData = ref([])
const resultResourceExpandedKeys = ref([])
const resultResourceCurrentKey = ref('')
const resultResourceLoading = ref(false)
const selectedResultNode = ref(null)

const tasks = ref([])
const tasksLoading = ref(false)
const tasksPage = ref(1)
const tasksPageSize = ref(20)
const tasksTotal = ref(0)
const executingId = ref(null)
const resourceTreeData = ref([])
const resourceExpandedKeys = ref([])
const resourceCurrentKey = ref('')
const resourceLoading = ref(false)

const formRef = ref(null)
const formDialogVisible = ref(false)
const saving = ref(false)
const editingId = ref(null)
const form = reactive(defaultForm())

const detailDialogVisible = ref(false)
const selectedTask = ref(null)
const embeddingStatuses = ['ready', 'outdated', 'failed', 'unsupported', 'missing_source']
const storageEngineTypes = new Set(['minio', 's3', 'nfs', 'nas'])
const resourceRootTypes = new Set(['root', 'server', 'service'])
const supportedExtensions = SUPPORTED_VECTOR_EXTENSIONS

const formTitle = computed(() => editingId.value ? t('manager.vectorization.editTitle') : t('manager.vectorization.createTitle'))
const resultNodeFilterLabel = computed(() => {
  if (selectedResultNode.value?.label) {
    return selectedResultNode.value.label
  }
  return Number(resultFilters.node_id) > 0 ? t('manager.vectorization.nodeWithId', { id: resultFilters.node_id }) : ''
})
const selectedResourceSummary = computed(() => {
  if (!form.target.locator) return ''
  const parts = []
  if (form.target.engine_name) {
    parts.push(form.target.engine_name)
  }
  const path = targetPathFromLocator(form.target.locator)
  if (path) {
    parts.push(path)
  }
  return parts.join(' / ') || targetNameFromLocator(form.target.locator)
})
const rules = computed(() => ({
  name: [{ required: true, message: t('manager.vectorization.nameRequired'), trigger: 'blur' }],
  'target.locator': [{ required: true, message: t('manager.vectorization.resourceRequired'), trigger: 'change' }]
}))

function defaultForm() {
  return {
    name: '',
    description: '',
    enabled: true,
    schedule: '',
    target: {
      scope: '',
      engine_id: 0,
      item_id: 0,
      node_id: 0,
      locator: '',
      label: '',
      engine_name: '',
      recursive: true
    },
    filters: {
      max_file_size_mb: DEFAULT_VECTOR_MAX_FILE_SIZE_MB
    }
  }
}

function defaultResultFilters() {
  return {
    engine_id: null,
    node_id: null,
    item_id: null,
    status: '',
    q: ''
  }
}

const resetForm = (task = null) => {
  Object.assign(form, defaultForm())
  if (task) {
    const config = task.config || {}
    const target = task.target || config.target || {}
    const filters = config.filters || {}
    Object.assign(form, {
      name: task.name || '',
      description: task.description || '',
      enabled: task.enabled !== false,
      schedule: task.schedule || '',
      target: {
        scope: String(target.scope || 'node'),
        engine_id: Number(target.engine_id || 0),
        item_id: Number(target.item_id || 0),
        node_id: Number(target.node_id || 0),
        locator: target.locator || '',
        label: targetNameFromLocator(target.locator || ''),
        engine_name: '',
        recursive: target.recursive !== false
      },
      filters: {
        max_file_size_mb: Number(filters.max_file_size_mb || DEFAULT_VECTOR_MAX_FILE_SIZE_MB)
      }
    })
  }
  editingId.value = task?.id || null
  resourceCurrentKey.value = form.target.locator || ''
}

const taskPayload = () => {
  const scope = String(form.target.scope || '').trim()
  const target = {
    scope,
    engine_id: Number(form.target.engine_id),
    locator: String(form.target.locator || '').trim()
  }
  if (scope === 'item') {
    target.item_id = Number(form.target.item_id)
  } else {
    target.node_id = Number(form.target.node_id)
    target.recursive = form.target.recursive !== false
  }
  const config = {
    target,
    filters: {
      max_file_size_mb: Number(form.filters.max_file_size_mb || DEFAULT_VECTOR_MAX_FILE_SIZE_MB)
    }
  }
  return {
    name: String(form.name || '').trim(),
    description: String(form.description || '').trim(),
    enabled: form.enabled !== false,
    schedule: String(form.schedule || '').trim(),
    config
  }
}

const loadResults = async () => {
  resultsLoading.value = true
  try {
    const params = {
      page: resultsPage.value,
      page_size: resultsPageSize.value
    }
    if (Number(resultFilters.engine_id) > 0) {
      params.engine_id = Number(resultFilters.engine_id)
    }
    if (Number(resultFilters.item_id) > 0) {
      params.item_id = Number(resultFilters.item_id)
    }
    if (Number(resultFilters.node_id) > 0) {
      params.node_id = Number(resultFilters.node_id)
    }
    if (String(resultFilters.status || '').trim()) {
      params.status = String(resultFilters.status).trim()
    }
    if (String(resultFilters.q || '').trim()) {
      params.q = String(resultFilters.q).trim()
    }
    const response = await client.get('/manager/embeddings', {
      params
    })
    results.value = response.data || []
    resultsTotal.value = response.total || 0
  } catch (error) {
    console.error('加载向量化结果失败:', error)
    ElMessage.error(t('manager.vectorization.loadResultsFailed'))
  } finally {
    resultsLoading.value = false
  }
}

const loadTasks = async () => {
  tasksLoading.value = true
  try {
    const response = await client.get('/manager/embedding_tasks', {
      params: {
        page: tasksPage.value,
        page_size: tasksPageSize.value
      }
    })
    tasks.value = response.data || []
    tasksTotal.value = response.total || 0
  } catch (error) {
    console.error('加载向量化任务定义失败:', error)
    ElMessage.error(t('manager.vectorization.loadTasksFailed'))
  } finally {
    tasksLoading.value = false
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

const handleResultsSizeChange = () => {
  resultsPage.value = 1
  loadResults()
}

const applyResultFilters = () => {
  resultsPage.value = 1
  loadResults()
}

const resetResultFilters = () => {
  Object.assign(resultFilters, defaultResultFilters())
  selectedResultNode.value = null
  applyResultFilters()
}

const handleResultEngineChange = () => {
  if (selectedResultNode.value && Number(selectedResultNode.value.engine_id) !== Number(resultFilters.engine_id || 0)) {
    clearResultNodeFilter(false)
  }
  if (resultNodeDialogVisible.value) {
    loadResultResourceTrees(true)
  }
}

const clearResultNodeFilter = (apply = true) => {
  resultFilters.node_id = null
  selectedResultNode.value = null
  resultResourceCurrentKey.value = ''
  if (apply) {
    applyResultFilters()
  }
}

const clearResultNodeFilterAndClose = () => {
  clearResultNodeFilter()
  resultNodeDialogVisible.value = false
}

const handleTasksSizeChange = () => {
  tasksPage.value = 1
  loadTasks()
}

const openCreateDialog = async () => {
  resetForm()
  formDialogVisible.value = true
  await loadResourceTrees()
}

const requestCreateDialog = async () => {
  const routeState = resolveRouteState({ tab: 'tasks', create: '1' })
  await navigateManagerRoute(router, {
    path: route.path,
    query: routeState.query
  }, { history: 'push' })
}

const clearFormDialogRoute = async () => {
  if (resolveRouteState(route.query).tab !== 'tasks') return
  const nextQuery = { ...route.query }
  delete nextQuery.create
  delete nextQuery.task_id
  const routeState = resolveRouteState(nextQuery)
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateManagerRoute(router, location, { history: 'replace' })
  }
}

const openEditDialog = async (task) => {
  resetForm(task)
  formDialogVisible.value = true
  await loadResourceTrees()
  await revealSelectedResource()
}

const requestEditTask = async (task) => {
  const routeState = resolveRouteState({ tab: 'tasks', task_id: task.id })
  await navigateManagerRoute(router, {
    path: route.path,
    query: routeState.query
  }, { history: 'push' })
}

const saveTask = async () => {
  await formRef.value?.validate()
  if (!isValidTaskTarget()) {
    ElMessage.warning(t('manager.vectorization.resourceRequired'))
    return
  }
  saving.value = true
  try {
    if (editingId.value) {
      await client.put(`/manager/embedding_tasks/${editingId.value}`, taskPayload())
      ElMessage.success(t('manager.vectorization.updateSuccess'))
    } else {
      await client.post('/manager/embedding_tasks', taskPayload())
      ElMessage.success(t('manager.vectorization.createSuccess'))
    }
    formDialogVisible.value = false
    await loadTasks()
  } catch (error) {
    console.error('保存向量化任务失败:', error)
    ElMessage.error(error.response?.data?.error || t('manager.vectorization.saveFailed'))
  } finally {
    saving.value = false
  }
}

const showTaskDetail = (task) => {
  selectedTask.value = task
  detailDialogVisible.value = true
}

const openTaskFromQuery = async (taskId, restoreSequence) => {
  if (!taskId) return
  activeTab.value = 'tasks'
  try {
    const response = await client.get(`/manager/embedding_tasks/${taskId}`)
    if (restoreSequence !== workspaceRestoreSequence) return
    await openEditDialog(response.data || response)
  } catch (error) {
    if (restoreSequence !== workspaceRestoreSequence) return
    console.error('加载向量化任务详情失败:', error)
    ElMessage.error(t('manager.vectorization.loadTasksFailed'))
    formDialogVisible.value = false
    await clearFormDialogRoute()
  }
}

const executeTask = async (task) => {
  executingId.value = task.id
  try {
    const response = await client.post(`/manager/tasks/embedding/${task.id}/execute`, {
      trigger_type: 'manual',
      source: 'manager'
    })
    ElMessage.success(t('manager.vectorization.executeSubmitted', { id: response.execution_id || '-' }))
    await loadTasks()
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    console.error('执行向量化任务失败:', error)
    ElMessage.error(t('manager.vectorization.executeFailed'))
  } finally {
    executingId.value = null
  }
}

const viewTaskResults = async (task) => {
  Object.assign(resultFilters, defaultResultFilters(), {
    engine_id: Number(task.target?.engine_id || 0) || null,
    node_id: Number(task.target?.node_id || 0) || null,
    item_id: Number(task.target?.item_id || 0) || null
  })
  selectedResultNode.value = resultFilters.node_id
    ? {
        engine_id: Number(task.target?.engine_id || 0),
        node_id: Number(task.target?.node_id || 0),
        locator: task.target?.locator || '',
        label: targetNameFromLocator(task.target?.locator || '')
      }
    : null
  resultsPage.value = 1
  activeTab.value = 'results'
  const routeState = resolveRouteState({ tab: 'results', task_id: task.id })
  await navigateManagerRoute(router, {
    path: route.path,
    query: routeState.query
  }, { history: 'replace' })
}

const loadStorageEngines = async (force = false) => {
  if (engineOptions.value.length && !force) {
    return engineOptions.value
  }
  const response = await client.get('/manager/engines')
  engineOptions.value = (response.data || []).filter((engine) => storageEngineTypes.has(String(engine.engine_type || '').toLowerCase()))
  return engineOptions.value
}

const loadResourceTrees = async (force = false) => {
  if (resourceTreeData.value.length && !force) {
    return
  }
  resourceLoading.value = true
  try {
    const engines = await loadStorageEngines(force)
    resourceTreeData.value = engines.map(resourceRootNode)
    resourceExpandedKeys.value = []
  } catch (error) {
    console.error('加载向量化资源树失败:', error)
    ElMessage.error(t('manager.vectorization.loadResourceTreeFailed'))
  } finally {
    resourceLoading.value = false
  }
}

const loadResultResourceTrees = async (force = false) => {
  if (resultResourceTreeData.value.length && !force) {
    return
  }
  resultResourceLoading.value = true
  try {
    const engines = await loadStorageEngines(force)
    const engineID = Number(resultFilters.engine_id || 0)
    const filtered = engineID > 0 ? engines.filter((engine) => Number(engine.id) === engineID) : engines
    resultResourceTreeData.value = filtered.map(resourceRootNode)
    resultResourceExpandedKeys.value = []
  } catch (error) {
    console.error('加载向量化结果资源树失败:', error)
    ElMessage.error(t('manager.vectorization.loadResourceTreeFailed'))
  } finally {
    resultResourceLoading.value = false
  }
}

const resourceRootNode = (engine) => {
  const type = catalogRootTypeForEngine(engine)
  const locator = engineRootLocator(engine, type)
  return {
    id: locator,
    locator,
    label: engine.name,
    type,
    icon: 'Folder',
    engineId: engine.id,
    engineType: engine.engine_type,
    engineName: engine.name,
    children: [],
    hasChildren: true,
    loaded: false
  }
}

const normalizeResourceNode = (node, engine, loaded = true) => {
  if (!node) return null
  const locator = node.locator || node.id || ''
  return {
    ...node,
    id: locator || node.id,
    locator,
    label: node.label || node.name || engine?.name || locator,
    engineId: node.engineId || engine?.id || locatorEngineID(locator),
    engineType: node.engineType || engine?.engine_type || '',
    engineName: node.engineName || engine?.name || '',
    loaded,
    children: Array.isArray(node.children)
      ? node.children.map((child) => normalizeResourceNode(child, engine)).filter(Boolean)
      : []
  }
}

const normalizeResultResourceNode = (node, engine, loaded = true) => {
  const normalized = normalizeResourceNode(node, engine, loaded)
  if (!normalized) return null
  normalized.children = (normalized.children || [])
    .filter((child) => !isVectorizableObjectNode(child))
    .map((child) => normalizeResultResourceNode(child, engine))
    .filter(Boolean)
  return normalized
}

const revealSelectedResource = async () => {
  const locator = String(form.target.locator || '').trim()
  if (!locator) return
  const engineID = Number(form.target.engine_id || locatorEngineID(locator))
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
    console.error('定位向量化资源失败:', error)
    ElMessage.error(t('manager.vectorization.loadResourceTreeFailed'))
  } finally {
    resourceLoading.value = false
  }
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
  const selection = selectionFromResourceNode(node)
  if (!selection) {
    return
  }
  Object.assign(form.target, selection)
}

const openResultNodeDialog = async () => {
  resultNodeDialogVisible.value = true
  await loadResultResourceTrees(true)
}

const handleResultResourceNodeExpand = async (node) => {
  const locator = node?.locator || node?.id
  if (!locator || !node?.hasChildren || (node.children || []).length > 0) {
    return
  }
  if (isResourceRootNode(node) && !node.loaded) {
    await loadResultResourceTreeRoot(node)
    return
  }
  await loadResultResourceNodeChildren(node)
}

const handleResultResourceNodeClick = async (node) => {
  const locator = node?.locator || node?.id
  resultResourceCurrentKey.value = locator || ''
  if (isResourceRootNode(node) && !node.loaded) {
    await loadResultResourceTreeRoot(node)
    return
  }
  if (node?.hasChildren && (node.children || []).length === 0) {
    await loadResultResourceNodeChildren(node)
  }
  if (!isSelectableResultNode(node)) {
    return
  }
  const loc = safeParseLocator(locator)
  resultFilters.engine_id = Number(node.engineId || loc?.engineId || 0) || null
  resultFilters.node_id = Number(loc?.nodeId || 0) || null
  selectedResultNode.value = {
    engine_id: Number(node.engineId || loc?.engineId || 0),
    node_id: Number(loc?.nodeId || 0),
    locator,
    label: node.label || targetNameFromLocator(locator)
  }
  resultNodeDialogVisible.value = false
  applyResultFilters()
}

const loadResultResourceTreeRoot = async (node) => {
  const engineID = Number(node?.engineId || locatorEngineID(node?.locator || node?.id))
  if (!engineID) return
  resultResourceLoading.value = true
  try {
    const tree = await dataExplorerAPI.getTree(engineID, 2)
    const engine = {
      id: node.engineId,
      engine_type: node.engineType,
      name: node.engineName
    }
    const normalized = normalizeResultResourceNode(tree, engine, true)
    if (!normalized) return
    resultResourceTreeData.value = replaceResourceNode(resultResourceTreeData.value, node.locator || node.id, normalized)
    if (!resultResourceExpandedKeys.value.includes(normalized.id)) {
      resultResourceExpandedKeys.value = [...resultResourceExpandedKeys.value, normalized.id]
    }
  } catch (error) {
    console.error('加载向量化结果资源树失败:', error)
    ElMessage.error(t('manager.vectorization.loadResourceTreeFailed'))
  } finally {
    resultResourceLoading.value = false
  }
}

const loadResultResourceNodeChildren = async (node) => {
  const locator = node?.locator || node?.id
  const engineID = Number(node?.engineId || locatorEngineID(locator))
  if (!locator || !engineID) return
  resultResourceLoading.value = true
  try {
    const response = await dataExplorerAPI.getNodeChildren(engineID, locator)
    const engine = {
      id: node.engineId,
      engine_type: node.engineType,
      name: node.engineName
    }
    const children = (response.children || [])
      .map((child) => normalizeResultResourceNode(child, engine))
      .filter((child) => child && !isVectorizableObjectNode(child))
    resultResourceTreeData.value = updateResourceNodeChildren(resultResourceTreeData.value, locator, children)
  } catch (error) {
    console.error('加载向量化结果资源子节点失败:', error)
    ElMessage.error(t('manager.vectorization.loadResourceTreeFailed'))
  } finally {
    resultResourceLoading.value = false
  }
}

const loadResourceTreeRoot = async (node) => {
  const engineID = Number(node?.engineId || locatorEngineID(node?.locator || node?.id))
  if (!engineID) return
  resourceLoading.value = true
  try {
    const tree = await dataExplorerAPI.getTree(engineID, 2)
    const engine = {
      id: node.engineId,
      engine_type: node.engineType,
      name: node.engineName
    }
    const normalized = normalizeResourceNode(tree, engine, true)
    if (!normalized) return
    resourceTreeData.value = replaceResourceNode(resourceTreeData.value, node.locator || node.id, normalized)
    if (!resourceExpandedKeys.value.includes(normalized.id)) {
      resourceExpandedKeys.value = [...resourceExpandedKeys.value, normalized.id]
    }
  } catch (error) {
    console.error('加载向量化资源树失败:', error)
    ElMessage.error(t('manager.vectorization.loadResourceTreeFailed'))
  } finally {
    resourceLoading.value = false
  }
}

const loadResourceNodeChildren = async (node) => {
  const locator = node?.locator || node?.id
  const engineID = Number(node?.engineId || locatorEngineID(locator))
  if (!locator || !engineID) return
  resourceLoading.value = true
  try {
    const response = await dataExplorerAPI.getNodeChildren(engineID, locator)
    const engine = {
      id: node.engineId,
      engine_type: node.engineType,
      name: node.engineName
    }
    const children = (response.children || []).map((child) => normalizeResourceNode(child, engine)).filter(Boolean)
    resourceTreeData.value = updateResourceNodeChildren(resourceTreeData.value, locator, children)
  } catch (error) {
    console.error('加载向量化资源子节点失败:', error)
    ElMessage.error(t('manager.vectorization.loadResourceTreeFailed'))
  } finally {
    resourceLoading.value = false
  }
}

const replaceResourceNode = (nodes, locator, replacement) => {
  return nodes.map((node) => {
    const nodeLocator = node.locator || node.id
    if (nodeLocator === locator) {
      return replacement
    }
    if (node.children?.length) {
      return {
        ...node,
        children: replaceResourceNode(node.children, locator, replacement)
      }
    }
    return node
  })
}

const updateResourceNodeChildren = (nodes, locator, children) => {
  return nodes.map((node) => {
    const nodeLocator = node.locator || node.id
    if (nodeLocator === locator) {
      return {
        ...node,
        children,
        hasChildren: children.length > 0 || node.hasChildren
      }
    }
    if (node.children?.length) {
      return {
        ...node,
        children: updateResourceNodeChildren(node.children, locator, children)
      }
    }
    return node
  })
}

const isResourceRootNode = (node) => {
  const locator = String(node?.locator || node?.id || '')
  return resourceRootTypes.has(String(node?.type || '').toLowerCase()) && locator.includes('/path/?')
}

const selectionFromResourceNode = (node) => {
  if (!node) return null
  const locator = node.locator || node.id || ''
  const loc = safeParseLocator(locator)
  if (isVectorizableObjectNode(node) && loc?.itemId) {
    return {
      scope: 'item',
      engine_id: Number(node.engineId || loc.engineId),
      item_id: Number(loc.itemId),
      node_id: 0,
      locator,
      label: node.label || targetNameFromLocator(locator),
      engine_name: node.engineName || '',
      recursive: false
    }
  }
  if (isVectorizableRangeNode(node) && loc?.nodeId) {
    return {
      scope: 'node',
      engine_id: Number(node.engineId || loc.engineId),
      item_id: 0,
      node_id: Number(loc.nodeId),
      locator,
      label: node.label || targetNameFromLocator(locator),
      engine_name: node.engineName || '',
      recursive: form.target.recursive !== false
    }
  }
  return null
}

const selectableResourceType = (node) => {
  const selection = selectionFromResourceNode(node)
  if (!selection) return ''
  return targetScopeLabel(selection.scope)
}

const isSelectableResultNode = (node) => {
  const loc = safeParseLocator(node?.locator || node?.id || '')
  return isVectorizableRangeNode(node) && Number(loc?.nodeId || 0) > 0
}

const safeParseLocator = (locator) => {
  const parsed = parseLocatorSafe(locator)
  return parsed.engineId ? parsed : null
}

const locatorEngineID = (locator) => {
  return Number(safeParseLocator(locator)?.engineId || 0)
}

const targetNameFromLocator = (locator) => {
  const loc = safeParseLocator(locator)
  const last = loc?.path?.[loc.path.length - 1]
  return last ? decodeURIComponent(last) : locator
}

const targetPathFromLocator = (locator) => {
  const loc = safeParseLocator(locator)
  return loc?.path?.length ? loc.path.join('/') : ''
}

const targetScopeLabel = (scope) => {
  if (scope === 'item') return t('manager.vectorization.targetItem')
  if (scope === 'node') return t('manager.vectorization.targetNode')
  return '-'
}

const targetDisplay = (task) => {
  const target = task?.target || {}
  return target.locator ? targetNameFromLocator(target.locator) : '-'
}

const targetResourcePath = (task) => {
  const target = task?.target || {}
  if (!target.locator) return '-'
  return targetPathFromLocator(target.locator) || targetNameFromLocator(target.locator)
}

const engineName = (engineID) => {
  const engine = engineOptions.value.find((item) => Number(item.id) === Number(engineID))
  return engine?.name || (engineID ? t('manager.vectorization.engineWithId', { id: engineID }) : '-')
}

const resultResourcePath = (result) => {
  if (!result?.locator) return '-'
  return targetPathFromLocator(result.locator) || targetNameFromLocator(result.locator)
}

const isValidTaskTarget = () => {
  if (!form.target.locator || !form.target.engine_id || !form.target.scope) {
    return false
  }
  if (form.target.scope === 'item') {
    return Number(form.target.item_id) > 0
  }
  if (form.target.scope === 'node') {
    return Number(form.target.node_id) > 0
  }
  return false
}

const openTaskExecution = async (task) => {
  if (!task.last_execution_id) return
  await openMonitorExecution(task.last_execution_id)
}

const openResultExecution = async (result) => {
  if (!result.last_execution_id) return
  await openMonitorExecution(result.last_execution_id)
}

const locateResult = (result) => {
  if (!result?.locator) return
  navigateManagerRoute(router, {
    name: 'DataExplorer',
    query: {
      locator: result.locator
    }
  })
}

const deleteTask = async (task) => {
  await ElMessageBox.confirm(t('manager.vectorization.deleteTaskConfirm'), t('manager.vectorization.delete'), {
    type: 'warning'
  })
  await client.delete(`/manager/embedding_tasks/${task.id}`)
  ElMessage.success(t('manager.vectorization.deleteSuccess'))
  await loadTasks()
}

const revectorizeResult = async (result) => {
  revectorizingId.value = result.id
  try {
    const response = await client.post('/manager/embedding_executions', {
      scope: 'item',
      target: {
        engine_id: result.engine_id,
        item_id: result.item_id,
        locator: result.locator
      }
    })
    ElMessage.success(t('manager.vectorization.executeSubmitted', { id: response.execution_id || '-' }))
    await openMonitorExecution(response.execution_id)
  } catch (error) {
    console.error('重新向量化失败:', error)
    ElMessage.error(t('manager.vectorization.executeFailed'))
  } finally {
    revectorizingId.value = null
  }
}

const deleteResult = async (result) => {
  await ElMessageBox.confirm(t('manager.vectorization.deleteResultConfirm'), t('manager.vectorization.delete'), {
    type: 'warning'
  })
  await client.delete(`/manager/embeddings/${result.id}`)
  ElMessage.success(t('manager.vectorization.deleteSuccess'))
  await loadResults()
}

const executionStatusTagType = (status) => {
  switch (status) {
    case 'success':
      return 'success'
    case 'failed':
    case 'timeout':
      return 'danger'
    case 'running':
    case 'pending':
      return 'warning'
    case 'cancelled':
      return 'info'
    default:
      return 'info'
  }
}

const executionStatusLabel = (status) => {
  if (!status) return t('manager.vectorization.statusNeverRun')
  if (!['pending', 'running', 'success', 'failed', 'timeout', 'cancelled'].includes(status)) {
    return status
  }
  return t(`manager.vectorization.status.${status}`)
}

const embeddingStatusTagType = (status) => {
  switch (status) {
    case 'ready':
      return 'success'
    case 'outdated':
      return 'warning'
    case 'failed':
    case 'missing_source':
      return 'danger'
    case 'unsupported':
      return 'info'
    default:
      return 'info'
  }
}

const embeddingStatusLabel = (status) => {
  if (!status) return '-'
  if (!embeddingStatuses.includes(status)) {
    return status
  }
  return t(`manager.vectorization.embeddingStatus.${status}`)
}

async function restoreWorkspaceFromRoute() {
  const restoreSequence = ++workspaceRestoreSequence
  const routeState = resolveRouteState(route.query)
  activeTab.value = routeState.tab
  if (routeState.changed) {
    await navigateManagerRoute(router, {
      path: route.path,
      query: routeState.query
    }, { history: 'replace' })
    return
  }
  if (!routeDataReady) return

  const taskId = Number(routeState.query.task_id || 0)
  if (routeState.tab === 'results') {
    formDialogVisible.value = false
    detailDialogVisible.value = false
    if (taskId) {
      try {
        const response = await client.get(`/manager/embedding_tasks/${taskId}`)
        if (restoreSequence !== workspaceRestoreSequence) return
        const task = response.data || response
        Object.assign(resultFilters, defaultResultFilters(), {
          engine_id: Number(task.target?.engine_id || 0) || null,
          node_id: Number(task.target?.node_id || 0) || null,
          item_id: Number(task.target?.item_id || 0) || null
        })
      } catch (error) {
        if (restoreSequence !== workspaceRestoreSequence) return
        ElMessage.error(t('manager.vectorization.loadTasksFailed'))
      }
    }
    await loadResults()
    return
  }

  if (taskId) await openTaskFromQuery(taskId, restoreSequence)
  else if (routeState.query.create === '1') await openCreateDialog()
  else formDialogVisible.value = false
  await loadTasks()
}

watch(() => route.query, restoreWorkspaceFromRoute)

onMounted(async () => {
  await restoreWorkspaceFromRoute()
  await loadStorageEngines()
  await loadTasks()
  await loadResults()
  routeDataReady = true
  await restoreWorkspaceFromRoute()
})
</script>

<style scoped>
.vectorization {
  padding: 20px;
}

.tab-toolbar {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  margin-bottom: 12px;
}

.pagination {
  margin-top: 20px;
  justify-content: center;
}

.filter-bar {
  display: grid;
  grid-template-columns: 180px 320px 140px 170px minmax(220px, 1fr) auto auto auto;
  gap: 10px;
  margin-bottom: 14px;
}

.vectorization-table {
  width: 100%;
}

.node-filter-control {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 8px;
  min-width: 0;
}

.task-actions {
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 6px;
}

.task-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}

.form-section-title {
  margin: 18px 0 12px;
  padding-left: 10px;
  border-left: 3px solid var(--el-color-primary);
  color: var(--el-text-color-primary);
  font-size: 14px;
  font-weight: 600;
  line-height: 1.2;
}

.form-section-title:first-child {
  margin-top: 0;
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
  line-height: 1.4;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.extension-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  line-height: 1;
}

.form-hint {
  margin-left: 10px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

@media (max-width: 900px) {
  .filter-bar {
    grid-template-columns: 1fr 1fr;
  }

  .vectorization-task-dialog :deep(.el-dialog) {
    width: calc(100vw - 24px) !important;
  }

  .form-hint {
    display: block;
    margin: 6px 0 0;
  }
}
</style>
