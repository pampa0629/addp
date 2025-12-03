<template>
  <div class="task-panel">
    <div class="panel-header">
      <h3>任务库</h3>
      <el-button @click="refreshAll" size="small" :loading="loading" circle>
        <el-icon><Refresh /></el-icon>
      </el-button>
    </div>

    <el-tabs v-model="activeTab" class="task-tabs">
      <!-- Transfer 任务 -->
      <el-tab-pane label="Transfer" name="transfer">
        <div v-loading="loading" class="task-list">
          <el-empty v-if="transferTasks.length === 0" description="暂无任务" :image-size="80" />
          <div
            v-for="task in transferTasks"
            :key="task.id"
            class="task-item"
            draggable="true"
            @dragstart="startDrag('transfer', task, $event)"
          >
            <div class="task-header">
              <el-icon class="drag-icon"><Rank /></el-icon>
              <span class="task-name">{{ task.name }}</span>
            </div>
            <div class="task-meta">
              <el-tag size="small" :type="getTaskTypeColor(task.type)">
                {{ task.type }}
              </el-tag>
              <el-tag size="small" :type="getStatusColor(task.status)">
                {{ task.status }}
              </el-tag>
            </div>
          </div>
        </div>
      </el-tab-pane>

      <!-- Meta 任务 -->
      <el-tab-pane label="Meta" name="meta">
        <div v-loading="loading" class="task-list">
          <el-empty v-if="metaTasks.length === 0" description="暂无任务" :image-size="80" />
          <div
            v-for="task in metaTasks"
            :key="task.id"
            class="task-item"
            draggable="true"
            @dragstart="startDrag('meta', task, $event)"
          >
            <div class="task-header">
              <el-icon class="drag-icon"><Rank /></el-icon>
              <span class="task-name">{{ task.name }}</span>
            </div>
            <div class="task-meta">
              <el-tag size="small" type="info">{{ task.schedule_type }}</el-tag>
              <el-tag size="small" :type="task.enabled ? 'success' : 'info'">
                {{ task.enabled ? '已启用' : '未启用' }}
              </el-tag>
            </div>
          </div>
        </div>
      </el-tab-pane>

      <!-- Manager 任务 -->
      <el-tab-pane label="Manager" name="manager">
        <div class="task-list">
          <el-empty description="暂无任务" :image-size="80" />
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { Refresh, Rank } from '@element-plus/icons-vue'
import modulesApi from '@/api/modules'
import { ElMessage } from 'element-plus'

const activeTab = ref('transfer')
const loading = ref(false)
const transferTasks = ref([])
const metaTasks = ref([])

onMounted(() => {
  loadTransferTasks()
  loadMetaTasks()
})

const loadTransferTasks = async () => {
  loading.value = true
  try {
    const data = await modulesApi.listTransferTasks()
    transferTasks.value = data.items || []
  } catch (error) {
    console.error('加载 Transfer 任务失败:', error)
    ElMessage.error('加载 Transfer 任务失败')
  } finally {
    loading.value = false
  }
}

const loadMetaTasks = async () => {
  try {
    const data = await modulesApi.listMetaTasks()
    metaTasks.value = Array.isArray(data) ? data : (data.items || [])
  } catch (error) {
    console.error('加载 Meta 任务失败:', error)
    ElMessage.error('加载 Meta 任务失败')
  }
}

const refreshAll = () => {
  loadTransferTasks()
  loadMetaTasks()
}

const startDrag = (module, task, event) => {
  const nodeData = {
    module,
    taskId: task.id,
    name: task.name,
    type: task.type || 'scan',
    endpoint: buildEndpoint(module, task),
    method: 'POST',
    parameters: {}
  }

  event.dataTransfer.setData('application/json', JSON.stringify(nodeData))
  event.dataTransfer.effectAllowed = 'copy'
}

const buildEndpoint = (module, task) => {
  switch (module) {
    case 'transfer':
      return `/api/tasks/${task.id}/execute`
    case 'meta':
      return `/api/scan/tasks/${task.id}/run`
    case 'manager':
      return `/api/cache/tasks/${task.id}/execute`
    default:
      return ''
  }
}

const getTaskTypeColor = (type) => {
  const colors = {
    import: 'success',
    export: 'warning',
    sync: 'info'
  }
  return colors[type] || 'info'
}

const getStatusColor = (status) => {
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
}

.panel-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 500;
}

.task-tabs {
  flex: 1;
  display: flex;
  flex-direction: column;
}

:deep(.el-tabs__content) {
  flex: 1;
  overflow: hidden;
}

:deep(.el-tab-pane) {
  height: 100%;
}

.task-list {
  height: 100%;
  overflow-y: auto;
  padding: 8px;
}

.task-item {
  padding: 12px;
  margin-bottom: 8px;
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  cursor: move;
  background: white;
  transition: all 0.3s;
}

.task-item:hover {
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
  border-color: #409eff;
  transform: translateX(2px);
}

.task-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.drag-icon {
  color: #909399;
  flex-shrink: 0;
}

.task-name {
  flex: 1;
  font-weight: 500;
  font-size: 14px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-meta {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
</style>
