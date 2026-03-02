<template>
  <div class="task-panel">
    <ResourceTree
      :tree-data="treeData"
      :loading="loading"
      title="任务库"
      default-expand-all
      empty-text="暂无任务"
      :show-count="false"
      card-shadow="never"
      height="100%"
      @refresh="refreshAll"
    >
      <!-- 自定义节点渲染 -->
      <template #node="{ data }">
        <div
          class="tree-node"
          :class="{'task-node': data.type === 'task', 'module-node': data.type === 'module'}"
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

          <!-- 任务节点 -->
          <template v-else-if="data.type === 'task'">
            <el-icon class="drag-icon"><Rank /></el-icon>
            <span class="task-name">{{ data.label }}</span>
            <div class="task-tags" @click.stop>
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
            </div>
          </template>
        </div>
      </template>
    </ResourceTree>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { Rank, FolderOpened } from '@element-plus/icons-vue'
import { ResourceTree } from '@addp/common-frontend'
import computeEnginesAPI from '../api/computeEngines'
import modulesApi from '../api/modules'
import { ElMessage } from 'element-plus'

const loading = ref(false)
const treeData = ref([])

onMounted(async () => {
  await loadAllTasks()
})

// 加载所有计算引擎及其任务
async function loadAllTasks() {
  loading.value = true
  try {
    // 1. 获取所有计算引擎
    const engines = await computeEnginesAPI.list()
    console.log('已加载计算引擎:', engines)

    // 2. 为每个引擎加载任务
    const treeNodes = []
    for (const engine of engines) {
      const moduleNode = await loadEngineTasks(engine)
      if (moduleNode) {
        treeNodes.push(moduleNode)
      }
    }

    treeData.value = treeNodes
    console.log('树形数据已构建:', treeData.value)
  } catch (error) {
    console.error('加载任务失败:', error)
    ElMessage.error('加载任务失败')
  } finally {
    loading.value = false
  }
}

// 加载单个引擎的任务
async function loadEngineTasks(engine) {
  try {
    // 使用 engine_type 作为标识符
    const identifier = engine.module_name || engine.engine_type
    console.log(`加载引擎任务: ${identifier}`)

    // 调用后端 API 获取任务列表 (使用 module_name 参数)
    const data = await modulesApi.listTasksByIdentifier(identifier)
    const tasks = data.items || []

    console.log(`引擎 ${identifier} 的任务:`, tasks)

    // 构建子节点（任务列表）
    const children = tasks.map(task => ({
      id: `${identifier}-task-${task.id}`,
      label: task.display_name || task.name || `任务 ${task.id}`,
      type: 'task',
      metadata: {
        uniqueIdentifier: identifier,
        taskId: task.id,
        taskType: task.type || null,
        status: task.status || null,
        enabled: task.enabled,
        endpoint: task.endpoint || buildEndpointFallback(identifier, task),
        method: 'POST',
        parameters: task.parameters || {}
      }
    }))

    // 返回模块节点
    return {
      id: identifier,
      label: engine.display_name || engine.name,
      type: 'module',
      metadata: {
        uniqueIdentifier: identifier,
        taskCount: children.length
      },
      children
    }
  } catch (error) {
    const identifier = engine.module_name
    console.error(`加载引擎 ${identifier} 任务失败:`, error)
    // 返回空任务的模块节点
    return {
      id: identifier,
      label: engine.display_name || engine.name,
      type: 'module',
      metadata: {
        uniqueIdentifier: identifier,
        taskCount: 0
      },
      children: []
    }
  }
}

// 回退的 endpoint 构建逻辑（兼容旧格式）
function buildEndpointFallback(uniqueIdentifier, task) {
  if (uniqueIdentifier.startsWith('transfer.')) {
    return `/api/tasks/${task.id}/execute`
  } else if (uniqueIdentifier.startsWith('meta.')) {
    return `/api/scan/tasks/${task.id}/run`
  } else if (uniqueIdentifier.startsWith('manager.')) {
    return `/api/quick-view/${task.id}/execute`
  }
  return ''
}

// 刷新所有任务
async function refreshAll() {
  await loadAllTasks()
  ElMessage.success('任务列表已刷新')
}

// 拖拽任务到画布
function startDrag(data, event) {
  if (data.type !== 'task' || !data.metadata) return

  const nodeData = {
    uniqueIdentifier: data.metadata.uniqueIdentifier,
    module: data.metadata.uniqueIdentifier.split('.')[0],
    taskId: data.metadata.taskId,
    name: data.label,
    type: data.metadata.taskType,
    endpoint: data.metadata.endpoint,
    method: data.metadata.method,
    parameters: data.metadata.parameters
  }

  console.log('拖拽任务数据:', nodeData)
  event.dataTransfer.setData('application/json', JSON.stringify(nodeData))
  event.dataTransfer.effectAllowed = 'copy'
}

// 任务类型颜色
function getTaskTypeColor(type) {
  const colors = {
    import: 'success',
    export: 'warning',
    sync: 'info',
    scan: 'primary'
  }
  return colors[type] || 'info'
}

// 任务状态颜色
function getStatusColor(status) {
  const colors = {
    pending: 'info',
    running: 'warning',
    completed: 'success',
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

.task-tags {
  display: flex;
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
