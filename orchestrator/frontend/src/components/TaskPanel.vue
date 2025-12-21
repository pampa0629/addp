<template>
  <div class="task-panel">
    <div class="panel-header">
      <h3>任务库</h3>
      <el-button @click="refreshAll" size="small" :loading="loading" circle>
        <el-icon><Refresh /></el-icon>
      </el-button>
    </div>

    <div class="task-tree-container">
      <el-tree
        :data="treeData"
        :props="treeProps"
        node-key="id"
        default-expand-all
        :expand-on-click-node="false"
        v-loading="loading"
      >
        <template #default="{ node, data }">
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
              <el-badge :value="data.taskCount" :hidden="!data.taskCount" class="task-count-badge" />
            </template>

            <!-- 任务节点 -->
            <template v-else-if="data.type === 'task'">
              <el-icon class="drag-icon"><Rank /></el-icon>
              <span class="task-name">{{ data.label }}</span>
              <div class="task-tags" @click.stop>
                <el-tag v-if="data.status" size="small" :type="getStatusColor(data.status)">
                  {{ data.status }}
                </el-tag>
                <el-tag v-if="data.taskType" size="small" :type="getTaskTypeColor(data.taskType)">
                  {{ data.taskType }}
                </el-tag>
              </div>
            </template>
          </div>
        </template>
      </el-tree>

      <el-empty v-if="treeData.length === 0 && !loading" description="暂无任务" :image-size="80" />
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { Refresh, Rank, FolderOpened } from '@element-plus/icons-vue'
import computeResourcesAPI from '../api/computeResources'
import modulesApi from '../api/modules'
import { ElMessage } from 'element-plus'

const loading = ref(false)
const treeData = ref([])
const treeProps = {
  children: 'children',
  label: 'label'
}

onMounted(async () => {
  await loadAllTasks()
})

// 加载所有计算资源及其任务
async function loadAllTasks() {
  loading.value = true
  try {
    // 1. 获取所有计算资源
    const resources = await computeResourcesAPI.list()
    console.log('已加载计算资源:', resources)

    // 2. 为每个资源加载任务
    const treeNodes = []
    for (const resource of resources) {
      const moduleNode = await loadResourceTasks(resource)
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

// 加载单个资源的任务
async function loadResourceTasks(resource) {
  try {
    // 兼容新旧格式: unique_identifier 或 module_name
    const identifier = resource.unique_identifier || resource.module_name
    console.log(`加载资源任务: ${identifier}`)

    // 调用后端 API 获取任务列表 (使用 module_name 参数)
    const data = await modulesApi.listTasksByIdentifier(identifier)
    const tasks = data.items || []

    console.log(`资源 ${identifier} 的任务:`, tasks)

    // 构建树节点
    const children = tasks.map(task => ({
      id: `${identifier}-task-${task.id}`,
      label: task.display_name || task.name || `任务 ${task.id}`,  // 优先使用中文显示名称
      type: 'task',
      uniqueIdentifier: identifier,
      taskId: task.id,
      taskType: task.type || null,
      status: task.status || null,
      enabled: task.enabled,
      endpoint: task.endpoint || buildEndpointFallback(identifier, task),
      method: 'POST',
      parameters: task.parameters || {}
    }))

    return {
      id: identifier,
      label: resource.display_name || resource.name,  // 优先使用中文显示名称
      type: 'module',
      uniqueIdentifier: identifier,
      taskCount: children.length,
      children
    }
  } catch (error) {
    const identifier = resource.unique_identifier || resource.module_name
    console.error(`加载资源 ${identifier} 任务失败:`, error)
    // 返回空任务的模块节点
    return {
      id: identifier,
      label: resource.display_name || resource.name,  // 优先使用中文显示名称
      type: 'module',
      uniqueIdentifier: identifier,
      taskCount: 0,
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
  if (data.type !== 'task') return

  const nodeData = {
    uniqueIdentifier: data.uniqueIdentifier,  // 新增：unique_identifier
    module: data.uniqueIdentifier.split('.')[0],  // 兼容旧字段
    taskId: data.taskId,
    name: data.label,
    type: data.taskType,
    endpoint: data.endpoint,
    method: data.method,
    parameters: data.parameters
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
  background: white;
  border-right: 1px solid #dcdfe6;
}

.panel-header {
  padding: 16px;
  border-bottom: 1px solid #dcdfe6;
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-shrink: 0;
}

.panel-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 500;
}

.task-tree-container {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
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
}

/* 模块节点 */
.module-node {
  font-weight: 500;
  color: #303133;
}

.module-node:hover {
  background-color: #f5f7fa;
}

.module-icon {
  color: #409eff;
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
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  background: white;
}

.task-node:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  border-color: #409eff;
  transform: translateX(2px);
}

.drag-icon {
  color: #909399;
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

/* Element Plus Tree 样式覆盖 */
:deep(.el-tree-node__content) {
  height: auto !important;
  padding: 4px 0;
}

:deep(.el-tree-node__children) {
  overflow: visible;
}

:deep(.el-tree-node) {
  white-space: normal;
}

:deep(.el-tree-node__expand-icon) {
  color: #909399;
}
</style>
