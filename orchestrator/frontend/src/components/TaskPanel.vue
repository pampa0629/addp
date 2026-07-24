<template>
  <div class="task-panel">
    <ResourceTree
      :tree-data="treeData"
      :loading="initialLoading"
      :title="t('orchestrator.taskPanel.title')"
      v-model:filter-text="filterText"
      v-model:expanded-keys="expandedKeys"
      :filter-placeholder="t('orchestrator.taskPanel.searchPlaceholder')"
      :default-expand-root="false"
      :empty-text="t('orchestrator.taskPanel.emptyText')"
      :filter-empty-text="t('orchestrator.taskPanel.filterEmptyText')"
      :show-count="false"
      :show-refresh-button="false"
      card-shadow="never"
      height="100%"
    >
      <template #header-actions>
        <el-tooltip :content="t('orchestrator.taskPanel.refreshTooltip')" placement="bottom">
          <el-button
            size="small"
            :loading="loading"
            :disabled="loading"
            :aria-label="t('orchestrator.taskPanel.refreshTooltip')"
            @click="refreshAll"
          >
            <el-icon><Refresh /></el-icon>
          </el-button>
        </el-tooltip>
      </template>

      <!-- 自定义节点渲染 -->
      <template #node="{ data }">
        <div
          class="tree-node"
          :class="{
            'task-node': data.type === 'task',
            'module-node': data.type === 'module',
            'task-type-node': data.type === 'taskType'
          }"
          :draggable="data.type === 'task'"
          @dragstart="startDrag(data, $event)"
        >
          <!-- 模块节点 -->
          <template v-if="data.type === 'module'">
            <el-icon class="module-icon"><FolderOpened /></el-icon>
            <span class="module-name">{{ data.label }}</span>
            <el-icon v-if="data.metadata?.loading" class="task-loading module-loading is-loading">
              <Loading />
            </el-icon>
            <el-badge
              v-else-if="data.metadata?.taskCount"
              :value="data.metadata.taskCount"
              class="task-count-badge"
            />
          </template>

          <!-- 任务类型节点 -->
          <template v-else-if="data.type === 'taskType'">
            <el-icon class="task-type-icon"><FolderOpened /></el-icon>
            <span class="task-type-name">{{ data.label }}</span>
            <el-icon v-if="data.metadata?.loading" class="task-loading task-type-loading is-loading">
              <Loading />
            </el-icon>
            <el-tooltip
              v-else-if="data.metadata?.loadFailed"
              :content="t('orchestrator.taskPanel.taskTypeLoadFailed')"
              placement="right"
            >
              <el-icon class="task-load-error"><WarningFilled /></el-icon>
            </el-tooltip>
            <el-badge
              v-else
              :value="data.metadata?.taskCount || 0"
              class="task-count-badge"
            />
            <el-tooltip
              v-if="data.metadata?.createUrl"
              :content="t('orchestrator.taskPanel.createTaskTooltip')"
              placement="right"
            >
              <el-button
                class="node-icon-button"
                size="small"
                link
                type="primary"
                @click.stop="openCreateTask(data)"
              >
                <el-icon><Plus /></el-icon>
              </el-button>
            </el-tooltip>
          </template>

          <!-- 任务节点 -->
          <template v-else-if="data.type === 'task'">
            <el-icon class="drag-icon"><Rank /></el-icon>
            <span class="task-name">{{ data.label }}</span>
            <div class="task-actions" @click.stop>
              <el-tag
                v-if="data.metadata?.status"
                size="small"
                :type="getStatusColor(data.metadata.status)"
              >
                {{ data.metadata.status }}
              </el-tag>
              <el-tooltip
                :content="t('orchestrator.taskPanel.addToCanvas')"
                placement="right"
              >
                <el-button
                  class="node-icon-button"
                  size="small"
                  link
                  type="primary"
                  :aria-label="t('orchestrator.taskPanel.addToCanvas')"
                  @click.stop="addTaskToCanvas(data)"
                >
                  <el-icon><CirclePlus /></el-icon>
                </el-button>
              </el-tooltip>
              <el-tooltip
                v-if="data.metadata?.editUrl"
                :content="t('orchestrator.taskPanel.editTaskTooltip')"
                placement="right"
              >
                <el-button
                  class="node-icon-button"
                  size="small"
                  link
                  type="primary"
                  @click.stop="openEditTask(data)"
                >
                  <el-icon><Edit /></el-icon>
                </el-button>
              </el-tooltip>
            </div>
          </template>
        </div>
      </template>
    </ResourceTree>
  </div>
</template>

<script setup>
import { computed, ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { CirclePlus, Edit, FolderOpened, Loading, Plus, Rank, Refresh, WarningFilled } from '@element-plus/icons-vue'
import { buildTaskOwnerUrl, ResourceTree } from '@addp/common-frontend'
import taskProvidersAPI from '../api/taskProviders'
import modulesApi from '../api/modules'
import { createConcurrencyLimiter } from '../utils/concurrency'
import { ElMessage } from 'element-plus'

const TASK_REQUEST_CONCURRENCY = 4
const { t } = useI18n()
const emit = defineEmits(['add-task'])

const loading = ref(false)
const treeData = ref([])
const filterText = ref('')
const expandedKeys = ref([])
const initialLoading = computed(() => loading.value && treeData.value.length === 0)

onMounted(async () => {
  await loadAllTasks()
})

// 加载所有任务提供者及其任务
async function loadAllTasks() {
  loading.value = true
  try {
    // 1. 获取所有任务提供者
    const providers = await taskProvidersAPI.list()
    console.log('已加载任务提供者:', providers)

    const providerStates = providers.map(provider => ({
      provider,
      identifier: provider.module_name,
      taskCapabilities: parseTaskCapabilities(provider.capabilities)
    }))
    treeData.value = providerStates.map(buildProviderNode)
    if (!filterText.value) {
      expandedKeys.value = treeData.value.map(node => node.id)
    }

    // 所有 Provider 共用一个请求队列，每个任务类型完成后原位更新对应节点。
    const scheduleTaskRequest = createConcurrencyLimiter(TASK_REQUEST_CONCURRENCY)
    const failureCounts = await Promise.all(providerStates.map(async providerState => {
      try {
        return await loadProviderTasks(providerState, scheduleTaskRequest)
      } catch (error) {
        console.error(`加载任务提供者 ${providerState.identifier} 失败:`, error)
        markProviderLoadFailed(providerState.identifier)
        return providerState.taskCapabilities.length
      }
    }))
    console.log('树形数据已构建:', treeData.value)
    return failureCounts.reduce((sum, count) => sum + count, 0)
  } catch (error) {
    console.error('加载任务失败:', error)
    console.error('任务库树数据:', treeData.value)
    ElMessage.error(t('orchestrator.taskPanel.loadFailed'))
    return null
  } finally {
    loading.value = false
  }
}

// 加载单个任务提供者的任务
async function loadProviderTasks(providerState, scheduleTaskRequest) {
  const { identifier, taskCapabilities } = providerState
  console.log(`加载任务提供者任务: ${identifier}`)

  // 按 capabilities 中声明的 task_type 拉取任务，单个类型失败不丢弃其他已加载类型。
  const results = await Promise.all(taskCapabilities.map(async taskType => {
    try {
      const data = await scheduleTaskRequest(() => (
        modulesApi.listTasksByModule(identifier, { task_type: taskType.type })
      ))
      const tasks = Array.isArray(data.items) ? data.items : []
      updateTaskTypeNode(identifier, taskType, tasks, false)
      return true
    } catch (error) {
      console.error(`加载任务提供者 ${identifier} 的 ${taskType.type} 任务失败:`, error)
      updateTaskTypeNode(identifier, taskType, [], true)
      return false
    }
  }))
  return results.filter(success => !success).length
}

function hasValue(value) {
  return value !== null && value !== undefined && String(value).trim() !== ''
}

function parseTaskCapabilities(capabilities) {
  const parsed = parseCapabilities(capabilities)
  const taskCapabilities = Array.isArray(parsed.task_capabilities) ? parsed.task_capabilities : []
  return taskCapabilities
    .filter(item => hasValue(item?.type) && !item.deprecated)
    .map(item => ({
      type: item.type,
      displayName: item.display_name || item.type,
      createUrl: item.create_url || '',
      editUrl: item.edit_url || ''
    }))
}

function parseCapabilities(capabilities) {
  if (!capabilities) return {}
  if (typeof capabilities === 'object') return capabilities
  try {
    return JSON.parse(capabilities)
  } catch (error) {
    return {}
  }
}

function buildProviderNode({ provider, identifier, taskCapabilities }) {
  return {
    id: identifier,
    label: provider.display_name || provider.name || identifier,
    type: 'module',
    metadata: {
      uniqueIdentifier: identifier,
      taskCount: 0,
      loading: taskCapabilities.length > 0
    },
    children: taskCapabilities.map(taskType => buildTaskTypeNode(identifier, taskType, [], { loading: true }))
  }
}

function buildTaskTypeNode(identifier, taskType, tasks, { loading = false, loadFailed = false } = {}) {
  const children = tasks
    .filter(task => task.task_type === taskType.type)
    .map(task => buildTaskNode(identifier, task, taskType))
    .filter(Boolean)

  return {
    id: `${identifier}-type-${taskType.type}`,
    label: taskType.displayName,
    type: 'taskType',
    metadata: {
      provider: identifier,
      taskType: taskType.type,
      createUrl: taskType.createUrl,
      editUrl: taskType.editUrl,
      taskCount: children.length,
      loading,
      loadFailed
    },
    children
  }
}

function updateTaskTypeNode(identifier, taskType, tasks, loadFailed) {
  treeData.value = treeData.value.map(moduleNode => {
    if (moduleNode.id !== identifier) return moduleNode

    const children = moduleNode.children.map(node => (
      node.metadata?.taskType === taskType.type
        ? buildTaskTypeNode(identifier, taskType, tasks, { loadFailed })
        : node
    ))
    return {
      ...moduleNode,
      metadata: {
        ...moduleNode.metadata,
        taskCount: children.reduce((sum, child) => sum + (child.metadata?.taskCount || 0), 0),
        loading: children.some(child => child.metadata?.loading)
      },
      children
    }
  })
}

function markProviderLoadFailed(identifier) {
  treeData.value = treeData.value.map(moduleNode => {
    if (moduleNode.id !== identifier) return moduleNode
    const children = moduleNode.children.map(node => ({
      ...node,
      metadata: { ...node.metadata, loading: false, loadFailed: true }
    }))
    return {
      ...moduleNode,
      metadata: { ...moduleNode.metadata, loading: false },
      children
    }
  })
}

function buildTaskNode(identifier, task, taskTypeDef) {
  const taskType = task.task_type
  if (!hasValue(task.id) || !hasValue(taskType)) {
    return null
  }

  return {
    id: `${identifier}-${taskType}-task-${task.id}`,
    label: task.display_name || task.name || `Task ${task.id}`,
    type: 'task',
    metadata: {
      provider: identifier,
      taskType,
      taskId: task.id,
      graphId: task.graph_id,
      editUrl: taskTypeDef?.editUrl || '',
      status: task.last_execution_status || task.status || null,
      enabled: task.enabled,
      parameters: task.parameters || {}
    }
  }
}

function openCreateTask(data) {
  openOwnerUrl(data.metadata?.createUrl)
}

function openEditTask(data) {
  openOwnerUrl(data.metadata?.editUrl, {
    taskId: data.metadata?.taskId,
    graphId: data.metadata?.graphId
  })
}

function openOwnerUrl(rawUrl, replacements = {}) {
  if (!hasValue(rawUrl)) {
    ElMessage.warning(t('orchestrator.taskPanel.openUrlMissing'))
    return
  }

  const url = buildTaskOwnerUrl(rawUrl, replacements)
  window.open(url, '_blank', 'noopener,noreferrer')
}



// 刷新所有任务
async function refreshAll() {
  const failureCount = await loadAllTasks()
  if (failureCount === null) return
  if (failureCount > 0) {
    ElMessage.warning(t('orchestrator.taskPanel.refreshPartial'))
    return
  }
  ElMessage.success(t('orchestrator.taskPanel.refreshSuccess'))
}

function buildTaskNodeData(data) {
  if (data.type !== 'task' || !data.metadata) return

  return {
    provider: data.metadata.provider,
    taskType: data.metadata.taskType,
    taskId: data.metadata.taskId,
    graphId: data.metadata.graphId,
    editUrl: data.metadata.editUrl,
    name: data.label,
    parameters: data.metadata.parameters
  }
}

function addTaskToCanvas(data) {
  const nodeData = buildTaskNodeData(data)
  if (nodeData) emit('add-task', nodeData)
}

// 拖拽任务到画布
function startDrag(data, event) {
  const nodeData = buildTaskNodeData(data)
  if (!nodeData) return

  console.log('拖拽任务数据:', nodeData)
  event.dataTransfer.setData('application/json', JSON.stringify(nodeData))
  event.dataTransfer.effectAllowed = 'copy'
}

function getStatusColor(status) {
  const colors = {
    pending: 'info',
    running: 'warning',
    success: 'success',
    failed: 'danger',
    scheduled: 'primary'
  }
  return colors[status] || 'info'
}
</script>

<style scoped>
.task-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-primary) !important;
  border-right: 1px solid var(--addp-border-color);
}

.task-panel :deep(.el-card) {
  border: none;
  border-right: 1px solid var(--addp-border-color);
}

.task-panel :deep(.el-card__header) {
  padding: 16px;
  border-bottom: 1px solid var(--addp-border-color);
}

/* 树节点样式 */
.tree-node {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  border-radius: 4px;
  transition: all 0.3s;
  width: 100%;
  box-sizing: border-box;
}

/* 模块节点 */
.module-node {
  font-weight: 500;
  color: var(--addp-text-primary);
}

.module-node:hover {
  background-color: var(--addp-bg-secondary);
}

.module-icon {
  color: var(--el-color-primary);
  flex-shrink: 0;
}

.module-name {
  flex: 1;
  min-width: 0;
  font-size: 14px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-count-badge {
  margin-left: auto;
}

.task-loading {
  color: var(--addp-text-tertiary);
  flex-shrink: 0;
}

.task-load-error {
  color: var(--el-color-danger);
  flex-shrink: 0;
}

/* 任务类型节点 */
.task-type-node {
  font-weight: 500;
  color: var(--addp-text-primary);
}

.task-type-node:hover {
  background-color: var(--addp-bg-secondary);
}

.task-type-icon {
  color: var(--el-color-info);
  flex-shrink: 0;
}

.task-type-name {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.node-icon-button {
  width: 24px;
  height: 24px;
  padding: 0;
  flex-shrink: 0;
}

/* 任务节点 */
.task-node {
  cursor: move;
  padding: 8px;
  margin: 2px 0;
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  background: var(--addp-bg-primary) !important;
}

.task-node:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  border-color: var(--el-color-primary);
  transform: translateX(2px);
}

.drag-icon {
  color: var(--addp-text-tertiary);
  flex-shrink: 0;
}

.task-name {
  flex: 1;
  min-width: 0;
  font-size: 14px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  margin-left: auto;
}

/* 覆盖 ResourceTree 的样式 */
.task-panel :deep(.tree-container) {
  padding: 8px;
}

.task-panel :deep(.el-tree-node__content) {
  height: auto !important;
  padding: 4px 0;
}

.task-panel :deep(.el-tree-node__children) {
  overflow: visible;
}

.task-panel :deep(.el-tree-node) {
  white-space: normal;
}

.task-panel :deep(.el-tree-node__expand-icon) {
  color: var(--addp-text-tertiary);
}
</style>
