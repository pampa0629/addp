<template>
  <div class="execution-list">
    <el-card shadow="hover">
      <template #header>
        <div class="card-header">
          <span>{{ t('monitor.execution.title') }}</span>
        </div>
      </template>

      <!-- 过滤器 -->
      <el-form :inline="true" :model="filters" class="filter-form">
        <el-form-item :label="t('monitor.execution.filter.preset')">
          <el-select
            v-model="selectedPreset"
            :placeholder="t('monitor.execution.filter.preset_placeholder')"
            clearable
            style="width: 180px;"
            @change="applyPreset"
          >
            <el-option
              v-for="option in presetOptions"
              :key="option.value || 'all'"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.execution.filter.module')">
          <el-select v-model="filters.module" :placeholder="t('monitor.execution.filter.module_placeholder')" clearable style="width: 150px;">
            <el-option
              v-for="option in moduleOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.execution.filter.task_type')">
          <el-select
            v-model="filters.task_type"
            :placeholder="t('monitor.execution.filter.task_type_placeholder')"
            clearable
            :disabled="filters.module && taskTypeOptions.length === 0"
            style="width: 180px;"
          >
            <el-option
              v-for="option in taskTypeOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.execution.filter.source_task_id')">
          <el-input
            v-model="filters.source_task_id"
            :placeholder="t('monitor.execution.filter.source_task_id_placeholder')"
            clearable
            style="width: 180px;"
          />
        </el-form-item>
        <el-form-item :label="t('monitor.execution.filter.source')">
          <el-select
            v-model="filters.source"
            :placeholder="t('monitor.execution.filter.source_placeholder')"
            clearable
            filterable
            allow-create
            default-first-option
            style="width: 150px;"
          >
            <el-option
              v-for="option in sourceOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.execution.filter.status')">
          <el-select v-model="filters.status" :placeholder="t('monitor.execution.filter.status_placeholder')" clearable style="width: 150px;">
            <el-option :label="t('monitor.execution.status.pending')" value="pending" />
            <el-option :label="t('monitor.execution.status.running')" value="running" />
            <el-option :label="t('monitor.execution.status.success')" value="success" />
            <el-option :label="t('monitor.execution.status.failed')" value="failed" />
            <el-option :label="t('monitor.execution.status.timeout')" value="timeout" />
            <el-option :label="t('monitor.execution.status.cancelled')" value="cancelled" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('monitor.execution.filter.trigger_type')">
          <el-select v-model="filters.trigger_type" :placeholder="t('monitor.execution.filter.trigger_placeholder')" clearable style="width: 150px;">
            <el-option :label="t('monitor.execution.trigger.manual')" value="manual" />
            <el-option :label="t('monitor.execution.trigger.scheduled')" value="scheduled" />
            <el-option :label="t('monitor.execution.trigger.event')" value="event" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="handleSearch">
            <el-icon><Search /></el-icon>
            {{ t('monitor.execution.filter.search') }}
          </el-button>
          <el-button @click="handleReset">
            <el-icon><RefreshLeft /></el-icon>
            {{ t('monitor.execution.filter.reset') }}
          </el-button>
        </el-form-item>
      </el-form>

      <!-- 执行记录表格 -->
      <execution-table
        :executions="executions"
        @view="handleViewExecution"
        v-loading="loading"
      />

      <!-- 分页 -->
      <div class="pagination">
        <el-pagination
          v-model:current-page="pagination.page"
          v-model:page-size="pagination.page_size"
          :page-sizes="[10, 20, 50, 100]"
          :total="pagination.total"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="handleSizeChange"
          @current-change="handlePageChange"
        />
      </div>
    </el-card>

    <!-- 执行详情对话框 -->
    <el-dialog
      v-model="detailDialogVisible"
      :title="t('monitor.execution.detail.title')"
      width="60%"
      :close-on-click-modal="false"
    >
      <div v-if="currentExecution">
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('monitor.execution.detail.id')">
            {{ currentExecution.id }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.uuid')">
            {{ currentExecution.execution_id }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.module')">
            <el-tag size="small">{{ currentExecution.module }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.source')">
            <el-tag size="small" type="info">{{ currentExecution.source || '-' }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.type')">
            {{ formatTaskType(currentExecution.task_type) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.source_task_id')">
            {{ currentExecution.source_task_id || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.source_task_name')">
            {{ currentExecution.source_task_name || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.status')">
            <el-tag :type="getStatusType(currentExecution.status)">
              {{ getStatusText(currentExecution.status) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.progress')">
            {{ currentExecution.progress }}%
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.trigger_type')">
            {{ getTriggerText(currentExecution.trigger_type) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.duration')">
            {{ formatDuration(currentExecution.execution_time_ms) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.created_at')">
            {{ formatDate(currentExecution.created_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.started_at')">
            {{ formatDate(currentExecution.started_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('monitor.execution.detail.completed_at')">
            {{ formatDate(currentExecution.completed_at) }}
          </el-descriptions-item>
        </el-descriptions>

        <div class="detail-actions">
          <el-button type="primary" @click="openOwnerTask(currentExecution)">
            <el-icon><Link /></el-icon>
            {{ t('monitor.execution.detail.open_task_definition') }}
          </el-button>
        </div>

        <div v-if="executionTreeData.length" class="execution-tree">
          <h4>{{ t('monitor.execution.detail.execution_tree') }}</h4>
          <el-tree
            :data="executionTreeData"
            :props="executionTreeProps"
            node-key="node_key"
            default-expand-all
            highlight-current
            @node-click="handleExecutionTreeNodeClick"
          >
            <template #default="{ data }">
              <div class="execution-tree-node">
                <div class="execution-tree-node-main">
                  <span class="execution-tree-title">
                    {{ executionDisplayName(data.execution) }}
                  </span>
                  <el-tag size="small" effect="plain">{{ data.execution.module }}</el-tag>
                  <el-tag size="small" type="info" effect="plain">{{ formatTaskType(data.execution.task_type) }}</el-tag>
                  <el-tag size="small" :type="getStatusType(data.execution.status)">
                    {{ getStatusText(data.execution.status) }}
                  </el-tag>
                </div>
                <div class="execution-tree-node-meta">
                  <span>{{ t('monitor.execution.detail.duration') }}: {{ formatDuration(data.execution.execution_time_ms) }}</span>
                  <span>{{ t('monitor.execution.detail.source') }}: {{ data.execution.source || '-' }}</span>
                  <span>{{ t('monitor.execution.detail.source_task_id') }}: {{ data.execution.source_task_id || '-' }}</span>
                  <span>{{ t('monitor.execution.detail.uuid') }}: {{ data.execution.execution_id }}</span>
                </div>
              </div>
            </template>
          </el-tree>
        </div>

        <!-- 执行元数据 -->
        <div v-if="hasExecutionMetadata" class="detail-section">
          <h4>{{ t('monitor.execution.detail.metadata') }}</h4>
          <el-descriptions
            v-if="metadataSummaryItems.length"
            :column="2"
            border
            class="metadata-summary"
          >
            <el-descriptions-item
              v-for="item in metadataSummaryItems"
              :key="item.summaryKey"
              :label="item.label"
            >
              <el-tag v-if="item.tagType" :type="item.tagType" size="small">
                {{ item.value }}
              </el-tag>
              <span v-else>{{ item.value }}</span>
            </el-descriptions-item>
          </el-descriptions>
          <el-input
            type="textarea"
            :value="executionMetadataText"
            :rows="10"
            readonly
            class="metadata-json"
          />
        </div>

        <!-- 执行结果 -->
        <div v-if="currentExecution.result" class="detail-section">
          <h4>{{ t('monitor.execution.detail.result') }}</h4>
          <el-input
            type="textarea"
            :value="JSON.stringify(currentExecution.result, null, 2)"
            :rows="10"
            readonly
          />
        </div>

        <!-- 错误详情 -->
        <div v-if="currentExecution.error_details" class="detail-section">
          <h4>{{ t('monitor.execution.detail.error') }}</h4>
          <el-alert
            type="error"
            :closable="false"
            :description="JSON.stringify(currentExecution.error_details, null, 2)"
          />
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Link } from '@element-plus/icons-vue'
import { buildTaskEditUrlFromProviders } from '@common-ui'
import { listExecutions, getExecutionTree, getExecutionTreeByExecutionID, listTaskProviders } from '@/api/monitor'
import ExecutionTable from '@/components/ExecutionTable.vue'

const { t } = useI18n()
const route = useRoute()

// 过滤条件
const filters = ref({
  module: '',
  task_type: '',
  source_task_id: '',
  source: '',
  status: '',
  trigger_type: ''
})

// 分页
const pagination = ref({
  page: 1,
  page_size: 20,
  total: 0
})

// 数据
const executions = ref([])
const taskProviders = ref([])
const selectedPreset = ref('')
const loading = ref(false)
const detailDialogVisible = ref(false)
const currentExecution = ref(null)
const executionTreeData = ref([])
const openedExecutionID = ref('')
let autoRefreshTimer = null
const executionTreeProps = {
  children: 'children'
}

const providerOptions = computed(() => taskProviders.value
  .filter(provider => hasValue(provider?.module_name))
  .map(provider => ({
    value: provider.module_name,
    label: provider.display_name || provider.module_name,
    provider
  }))
  .sort((a, b) => a.label.localeCompare(b.label)))

const moduleOptions = computed(() => {
  const options = [...providerOptions.value]
  if (!options.some(option => option.value === 'system')) {
    options.unshift({ value: 'system', label: t('monitor.execution.system_module') })
  }
  return options
})
const presetOptions = [
  { label: t('monitor.execution.preset.all'), value: '' },
  { label: t('monitor.execution.preset.cleanup_system'), value: 'cleanup_system' }
]

const sourceOptions = computed(() => {
  const options = new Map()
  for (const option of providerOptions.value) {
    options.set(option.value, option.label)
  }
  for (const execution of executions.value) {
    if (hasValue(execution?.source) && !options.has(execution.source)) {
      options.set(execution.source, execution.source)
    }
  }
  return Array.from(options, ([value, label]) => ({ value, label }))
})

const taskTypeOptions = computed(() => {
  const options = new Map()
  const selectedModule = filters.value.module

  for (const provider of taskProviders.value) {
    if (selectedModule && provider.module_name !== selectedModule) {
      continue
    }
    for (const taskType of parseTaskTypes(provider.capabilities)) {
      if (!options.has(taskType.value)) {
        options.set(taskType.value, taskType.label)
      }
    }
  }

  return Array.from(options, ([value, label]) => ({ value, label }))
})

const hasRunningExecution = computed(() => {
  return executions.value.some(execution => isRunningStatus(execution?.status)) ||
    (detailDialogVisible.value && isRunningStatus(currentExecution.value?.status))
})

const currentExecutionMetadata = computed(() => normalizeObject(currentExecution.value?.metadata))

const hasExecutionMetadata = computed(() => Object.keys(currentExecutionMetadata.value).length > 0)

const executionMetadataText = computed(() => JSON.stringify(currentExecutionMetadata.value, null, 2))

const metadataSummaryItems = computed(() => buildMetadataSummaryItems(currentExecutionMetadata.value))

function parseTaskTypes(capabilities) {
  const parsed = parseCapabilities(capabilities)
  const taskTypes = Array.isArray(parsed.task_types) ? parsed.task_types : []
  return taskTypes
    .filter(item => hasValue(item?.type) && !item.deprecated)
    .map(item => ({
      value: item.type,
      label: item.display_name || item.type
    }))
}

function parseCapabilities(capabilities) {
  if (!capabilities) return {}
  if (typeof capabilities === 'object') return capabilities
  try {
    return JSON.parse(capabilities)
  } catch {
    return {}
  }
}

function normalizeObject(value) {
  if (!value) return {}
  if (typeof value === 'object' && !Array.isArray(value)) return value
  if (typeof value !== 'string') return {}
  try {
    const parsed = JSON.parse(value)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {}
  } catch {
    return {}
  }
}

function buildMetadataSummaryItems(metadata) {
  const items = []
  appendTileGenerationTargetSummary(items, normalizeObject(metadata.tile_generation_target))
  appendQuickViewOptimizationSummary(items, metadata)
  appendMetadataField(items, 'source_srid', metadata.source_srid)
  appendMetadataField(items, 'target_srid', metadata.target_srid)
  appendMetadataField(items, 'extent_srid', metadata.extent_srid)
  appendMetadataField(items, 'min_zoom', metadata.min_zoom)
  appendMetadataField(items, 'max_zoom', metadata.max_zoom)
  appendMetadataField(items, 'total_tiles', metadata.total_tiles)
  appendMetadataField(items, 'generated_tiles', metadata.generated_tiles)
  appendMetadataField(items, 'cached_tiles', metadata.cached_tiles)
  appendMetadataField(items, 'failed_tiles', metadata.failed_tiles)
  appendMetadataField(items, 'stop_reason', metadata.stop_reason)
  return dedupeMetadataSummaryItems(items)
}

function appendTileGenerationTargetSummary(items, target) {
  if (Object.keys(target).length === 0) return

  const qualifiedName = [target.schema, target.table].filter(hasValue).join('.')
  appendMetadataField(items, 'tile_generation_target', qualifiedName)
  appendMetadataField(items, 'geometry_column', target.geom_column)
  appendMetadataField(items, 'target_srid', target.srid)
  appendMetadataField(items, 'target_kind', target.target_kind, {
    value: formatTargetKind(target.target_kind),
    tagType: targetKindTagType(target.target_kind)
  })
  appendMetadataField(items, 'optimization_recommended', target.optimization_recommended, {
    value: formatBoolean(target.optimization_recommended),
    tagType: target.optimization_recommended ? 'warning' : 'success'
  })
  appendMetadataField(items, 'optimization_recommendation', target.optimization_recommendation)
}

function appendQuickViewOptimizationSummary(items, metadata) {
  if (!hasValue(metadata.target_table) && !hasValue(metadata.target_kind)) return

  const qualifiedName = [metadata.target_schema, metadata.target_table].filter(hasValue).join('.')
  appendMetadataField(items, 'quick_view_target', qualifiedName)
  appendMetadataField(items, 'target_kind', metadata.target_kind, {
    value: formatTargetKind(metadata.target_kind),
    tagType: targetKindTagType(metadata.target_kind)
  })
  appendMetadataField(items, 'geometry_column', metadata.target_geometry_column)
  appendMetadataField(items, 'row_count_estimate', metadata.row_count_estimate)
  appendMetadataField(items, 'analyze_executed', metadata.analyze_executed, {
    value: formatBoolean(metadata.analyze_executed),
    tagType: metadata.analyze_executed ? 'success' : 'info'
  })
}

function appendMetadataField(items, key, rawValue, options = {}) {
  if (!hasValue(rawValue)) return
  items.push({
    summaryKey: `${key}:${items.length}`,
    key,
    label: t(`monitor.execution.detail.metadata_fields.${key}`),
    value: options.value || formatMetadataValue(rawValue),
    tagType: options.tagType || ''
  })
}

function dedupeMetadataSummaryItems(items) {
  const seen = new Set()
  return items.filter(item => {
    const dedupeKey = `${item.key}:${item.value}`
    if (seen.has(dedupeKey)) return false
    seen.add(dedupeKey)
    return true
  })
}

function formatMetadataValue(value) {
  if (typeof value === 'boolean') return formatBoolean(value)
  if (typeof value === 'number') return String(value)
  if (typeof value === 'string') return value
  return JSON.stringify(value)
}

function formatBoolean(value) {
  return value ? t('monitor.execution.detail.boolean.yes') : t('monitor.execution.detail.boolean.no')
}

function formatTargetKind(targetKind) {
  if (!hasValue(targetKind)) return '-'
  const key = `monitor.execution.detail.target_kind.${targetKind}`
  const translated = t(key)
  return translated === key ? targetKind : translated
}

function targetKindTagType(targetKind) {
  const tagTypeMap = {
    source_table: 'warning',
    source_schema_materialized_view: 'success',
    external_3857_materialized_view: 'success'
  }
  return tagTypeMap[targetKind] || 'info'
}

watch(
  () => filters.value.module,
  () => {
    filters.value.task_type = ''
  }
)

// 加载执行记录
async function loadExecutions(options = {}) {
  if (filters.value.source_task_id && (!filters.value.module || !filters.value.task_type)) {
    ElMessage.warning(t('monitor.execution.filter.source_task_id_requires_scope'))
    return
  }

  const silent = options.silent === true
  if (!silent) {
    loading.value = true
  }
  try {
    const params = {
      ...filters.value,
      page: pagination.value.page,
      page_size: pagination.value.page_size
    }
    const data = await listExecutions(params)
    executions.value = data.executions || []
    pagination.value.total = data.total || 0
  } catch (error) {
    if (!silent) {
      ElMessage.error(t('monitor.execution.load_failed'))
    }
    console.error(error)
  } finally {
    if (!silent) {
      loading.value = false
    }
  }
}

function applyQueryFilters(query) {
  const nextModule = firstQueryValue(query.module)
  const nextTaskType = firstQueryValue(query.task_type)
  const nextSource = firstQueryValue(query.source)
  const nextTriggerType = firstQueryValue(query.trigger_type)
  const nextStatus = firstQueryValue(query.status)
  if (!nextModule && !nextTaskType && !nextSource && !nextTriggerType && !nextStatus) {
    return false
  }
  filters.value.module = nextModule
  filters.value.task_type = nextTaskType
  filters.value.source = nextSource
  filters.value.trigger_type = nextTriggerType
  filters.value.status = nextStatus
  pagination.value.page = 1
  return true
}

async function loadTaskProviders() {
  try {
    taskProviders.value = await listTaskProviders()
  } catch (error) {
    taskProviders.value = []
    console.error(error)
  }
}

async function ensureTaskProviders() {
  if (taskProviders.value.length === 0) {
    await loadTaskProviders()
  }
}

// 查询
function handleSearch() {
  pagination.value.page = 1
  loadExecutions()
}

// 重置
function handleReset() {
  selectedPreset.value = ''
  filters.value = {
    module: '',
    task_type: '',
    source_task_id: '',
    source: '',
    status: '',
    trigger_type: ''
  }
  pagination.value.page = 1
  loadExecutions()
}

function applyPreset(preset) {
  selectedPreset.value = preset || ''
  if (preset === 'cleanup_system') {
    filters.value.module = 'system'
    filters.value.task_type = 'cleanup'
    filters.value.trigger_type = ''
    filters.value.source = 'system'
  } else {
    filters.value.module = ''
    filters.value.task_type = ''
    filters.value.source = ''
    filters.value.trigger_type = ''
  }
  pagination.value.page = 1
  loadExecutions()
}

// 分页变化
function handlePageChange(page) {
  pagination.value.page = page
  loadExecutions()
}

function handleSizeChange(size) {
  pagination.value.page_size = size
  pagination.value.page = 1
  loadExecutions()
}

// 查看执行详情
async function handleViewExecution(row) {
  try {
    const data = await getExecutionTree(row.id)
    openExecutionTree(data)
  } catch (error) {
    ElMessage.error(t('monitor.execution.detail_failed'))
    console.error(error)
  }
}

async function openExecutionByExecutionID(executionID) {
  executionID = firstQueryValue(executionID)
  if (!hasValue(executionID) || openedExecutionID.value === executionID) {
    return
  }
  try {
    const data = await getExecutionTreeByExecutionID(executionID)
    openExecutionTree(data)
  } catch (error) {
    ElMessage.error(t('monitor.execution.detail_failed'))
    console.error(error)
  }
}

function openExecutionTree(data) {
  const tree = normalizeExecutionTree(data)
  executionTreeData.value = tree ? [tree] : []
  currentExecution.value = tree?.execution || null
  openedExecutionID.value = tree?.execution?.execution_id || ''
  detailDialogVisible.value = true
}

function normalizeExecutionTree(node) {
  if (!node || !node.execution) {
    return null
  }
  return {
    ...node,
    node_key: node.execution.execution_id,
    children: (node.children || [])
      .map(normalizeExecutionTree)
      .filter(Boolean)
  }
}

function handleExecutionTreeNodeClick(node) {
  currentExecution.value = node.execution
}

function executionDisplayName(execution) {
  return execution?.source_task_name || execution?.source_task_id || execution?.execution_id || '-'
}

function formatTaskType(taskType) {
  if (!taskType) return '-'
  const key = `monitor.execution.task_type_names.${taskType}`
  const translated = t(key)
  return translated === key ? taskType : translated
}

// 辅助函数
function getStatusType(status) {
  const typeMap = {
    pending: 'info',
    running: 'warning',
    success: 'success',
    failed: 'danger',
    timeout: 'danger',
    cancelled: 'info'
  }
  return typeMap[status] || 'info'
}

function isRunningStatus(status) {
  return status === 'pending' || status === 'running'
}

function getStatusText(status) {
  const textMap = {
    pending: t('monitor.execution.status.pending'),
    running: t('monitor.execution.status.running'),
    success: t('monitor.execution.status.success'),
    failed: t('monitor.execution.status.failed'),
    timeout: t('monitor.execution.status.timeout'),
    cancelled: t('monitor.execution.status.cancelled'),
  }
  return textMap[status] || status
}

async function refreshOpenedExecution() {
  if (!detailDialogVisible.value || !hasValue(openedExecutionID.value)) {
    return
  }
  try {
    const data = await getExecutionTreeByExecutionID(openedExecutionID.value)
    openExecutionTree(data)
  } catch (error) {
    console.error(error)
  }
}

async function refreshRunningExecutions() {
  if (!hasRunningExecution.value) {
    return
  }
  await loadExecutions({ silent: true })
  await refreshOpenedExecution()
}

function startAutoRefresh() {
  stopAutoRefresh()
  autoRefreshTimer = window.setInterval(refreshRunningExecutions, 3000)
}

function stopAutoRefresh() {
  if (autoRefreshTimer) {
    window.clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
}

function getTriggerText(triggerType) {
  const textMap = {
    manual: t('monitor.execution.trigger.manual'),
    scheduled: t('monitor.execution.trigger.scheduled'),
    event: t('monitor.execution.trigger.event')
  }
  return textMap[triggerType] || triggerType || '-'
}

function hasValue(value) {
  return value !== null && value !== undefined && String(value).trim() !== ''
}

function firstQueryValue(value) {
  if (Array.isArray(value)) {
    return value[0] || ''
  }
  return value || ''
}

function buildOwnerTaskUrl(execution) {
  return buildTaskEditUrlFromProviders(taskProviders.value, execution)
}

async function openOwnerTask(execution) {
  await ensureTaskProviders()
  const url = buildOwnerTaskUrl(execution)
  if (!url) {
    ElMessage.warning(t('monitor.execution.detail.open_task_unavailable'))
    return
  }
  window.open(url, '_blank', 'noopener,noreferrer')
}

function formatDate(date) {
  if (!date) return '-'
  return new Date(date).toLocaleString('zh-CN')
}

function formatDuration(ms) {
  if (!ms) return '-'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(2)}s`
  return `${(ms / 60000).toFixed(2)}min`
}

// 初始化
onMounted(async () => {
  await loadTaskProviders()
  applyQueryFilters(route.query)
  await loadExecutions()
  await openExecutionByExecutionID(route.query.execution_id)
  if (hasRunningExecution.value) {
    startAutoRefresh()
  }
})

onBeforeUnmount(stopAutoRefresh)

watch(
  () => route.query.execution_id,
  executionID => {
    openExecutionByExecutionID(executionID)
  }
)

watch(
  () => route.query,
  query => {
    if (applyQueryFilters(query)) {
      loadExecutions()
    }
  }
)

watch(hasRunningExecution, running => {
  if (running) {
    startAutoRefresh()
  } else {
    stopAutoRefresh()
  }
})

watch(detailDialogVisible, visible => {
  if (!visible) {
    openedExecutionID.value = ''
  }
})
</script>

<style scoped>
.execution-list {
  padding: 20px;
  min-height: 100vh;
  background: var(--addp-bg-secondary);
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header span {
  color: var(--addp-text-primary);
  font-weight: 500;
  font-size: 16px;
}

.filter-form {
  margin-bottom: 20px;
}

.pagination {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}

.detail-actions {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
}

.execution-tree {
  margin-top: 20px;
}

.detail-section {
  margin-top: 20px;
}

.metadata-summary {
  margin-bottom: 12px;
}

.metadata-json :deep(.el-textarea__inner) {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 12px;
}

.execution-tree :deep(.el-tree-node__content) {
  height: auto;
  min-height: 44px;
  align-items: flex-start;
}

.execution-tree-node {
  min-width: 0;
  width: 100%;
  padding: 6px 0;
}

.execution-tree-node-main {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.execution-tree-title {
  font-weight: 500;
  color: var(--addp-text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.execution-tree-node-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
  margin-top: 4px;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.execution-tree-node-meta span {
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
