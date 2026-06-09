<template>
  <div class="task-panel">
    <ResourceTree
      :tree-data="treeData"
      :loading="loading"
      :title="t('orchestrator.taskPanel.title')"
      default-expand-all
      :empty-text="t('orchestrator.taskPanel.emptyText')"
      :show-count="false"
      card-shadow="never"
      height="100%"
      @refresh="refreshAll"
    >
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
            <el-badge
              v-if="data.metadata?.taskCount"
              :value="data.metadata.taskCount"
              class="task-count-badge"
            />
          </template>

          <!-- 任务类型节点 -->
          <template v-else-if="data.type === 'taskType'">
            <el-icon class="task-type-icon"><FolderOpened /></el-icon>
            <span class="task-type-name">{{ data.label }}</span>
            <el-tag size="small" :type="getTaskTypeColor(data.metadata?.taskType)">
              {{ data.metadata?.taskType }}
            </el-tag>
            <el-badge
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
              <el-tag
                v-if="data.metadata?.taskType"
                size="small"
                :type="getTaskTypeColor(data.metadata.taskType)"
              >
                {{ data.metadata.taskType }}
              </el-tag>
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
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Edit, FolderOpened, Plus, Rank } from '@element-plus/icons-vue'
import { buildTaskOwnerUrl, ResourceTree } from '@addp/common-frontend'
import taskProvidersAPI from '../api/taskProviders'
import modulesApi from '../api/modules'
import { ElMessage } from 'element-plus'

const { t } = useI18n()

const loading = ref(false)
const treeData = ref([])

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

    // 2. 为每个任务提供者加载任务
    const treeNodes = []
    for (const provider of providers) {
      const moduleNode = await loadProviderTasks(provider)
      if (moduleNode) {
        treeNodes.push(moduleNode)
      }
    }

    treeData.value = treeNodes
    console.log('树形数据已构建:', treeData.value)
  } catch (error) {
    console.error('加载任务失败:', error)
    ElMessage.error(t('orchestrator.taskPanel.loadFailed'))
  } finally {
    loading.value = false
  }
}

// 加载单个任务提供者的任务
async function loadProviderTasks(provider) {
  const identifier = provider.module_name
  const taskTypes = parseTaskTypes(provider.capabilities)
  let tasks = []

  try {
    console.log(`加载任务提供者任务: ${identifier}`)

    // 按 capabilities 中声明的 task_type 拉取任务，避免跨类型任务由前端猜测过滤。
    if (taskTypes.length > 0) {
      const results = await Promise.all(taskTypes.map(async taskType => {
        const data = await modulesApi.listTasksByModule(identifier, { task_type: taskType.type })
        return data.items || []
      }))
      tasks = results.flat()
    } else {
      const data = await modulesApi.listTasksByModule(identifier)
      tasks = data.items || []
    }

    console.log(`任务提供者 ${identifier} 的任务:`, tasks)
  } catch (error) {
    console.error(`加载任务提供者 ${identifier} 任务失败:`, error)
  }

  const children = buildTaskTypeNodes(identifier, taskTypes, tasks)

  return {
    id: identifier,
    label: provider.display_name || provider.name || identifier,
    type: 'module',
    metadata: {
      uniqueIdentifier: identifier,
      taskCount: children.reduce((sum, child) => sum + (child.metadata?.taskCount || 0), 0)
    },
    children
  }
}

function hasValue(value) {
  return value !== null && value !== undefined && String(value).trim() !== ''
}

function parseTaskTypes(capabilities) {
  const parsed = parseCapabilities(capabilities)
  const taskTypes = Array.isArray(parsed.task_types) ? parsed.task_types : []
  return taskTypes
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

function buildTaskTypeNodes(identifier, taskTypes, tasks) {
  if (taskTypes.length === 0) {
    return tasks
      .map(task => buildTaskNode(identifier, task, null))
      .filter(Boolean)
  }

  return taskTypes.map(taskType => {
    const children = tasks
      .filter(task => (task.task_type || task.type) === taskType.type)
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
        taskCount: children.length
      },
      children
    }
  })
}

function buildTaskNode(identifier, task, taskTypeDef) {
  const taskType = task.task_type || task.type
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
  await loadAllTasks()
  ElMessage.success(t('orchestrator.taskPanel.refreshSuccess'))
}

// 拖拽任务到画布
function startDrag(data, event) {
  if (data.type !== 'task' || !data.metadata) return

  const nodeData = {
    // 任务引用模式字段
    provider: data.metadata.provider,
    taskType: data.metadata.taskType,
    taskId: data.metadata.taskId,
    graphId: data.metadata.graphId,
    editUrl: data.metadata.editUrl,
    name: data.label,
    parameters: data.metadata.parameters
  }

  console.log('拖拽任务数据:', nodeData)
  event.dataTransfer.setData('application/json', JSON.stringify(nodeData))
  event.dataTransfer.effectAllowed = 'copy'
}

// 任务类型颜色
function getTaskTypeColor(type) {
  const colors = {
    scan: 'primary',
    import: 'success',
    query: 'warning',
    workflow: 'success',
    script: 'info',
    mvt_generation: 'warning',
    embedding: 'success',
    check: 'danger',
    kg_build: 'primary',
    orchestration: 'primary'
  }
  return colors[type] || 'info'
}

// 任务状态颜色
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
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  border-radius: 4px;
  transition: all 0.3s;
  width: 100%;
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
  font-size: 14px;
}

.task-count-badge {
  margin-left: auto;
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
