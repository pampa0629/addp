<template>
  <div class="spatial-tasks">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>空间计算任务</span>
          <el-button type="primary" @click="showCreateDialog">创建任务</el-button>
        </div>
      </template>

      <!-- 任务列表 -->
      <el-table :data="tasks" v-loading="loading" style="width: 100%">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="任务名称" min-width="150" />
        <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'info'">
              {{ row.status }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="schedule" label="调度" width="150" show-overflow-tooltip />
        <el-table-column prop="last_execution_status" label="最后执行" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.last_execution_status" :type="getExecutionStatusType(row.last_execution_status)">
              {{ row.last_execution_status }}
            </el-tag>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="executeTask(row)">执行</el-button>
            <el-button size="small" @click="editTask(row)">编辑</el-button>
            <el-button size="small" type="danger" @click="deleteTask(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="loadTasks"
        @current-change="loadTasks"
        style="margin-top: 20px; justify-content: flex-end"
      />
    </el-card>

    <!-- 创建/编辑任务对话框 -->
    <el-dialog
      :title="dialogTitle"
      v-model="dialogVisible"
      width="50%"
      @close="resetForm"
    >
      <el-form :model="taskForm" label-width="120px">
        <el-form-item label="任务名称" required>
          <el-input v-model="taskForm.name" placeholder="请输入任务名称" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="taskForm.description" type="textarea" rows="3" />
        </el-form-item>
        <el-form-item label="工作流定义" required>
          <el-input
            v-model="taskForm.workflowDefJson"
            type="textarea"
            rows="10"
            placeholder='{"tasks": [{"id": "t1", "operator": "buffer", "params": {...}, "depends_on": []}]}'
          />
          <div style="color: #909399; font-size: 12px; margin-top: 5px">
            JSON 格式的工作流定义
          </div>
        </el-form-item>
        <el-form-item label="调度表达式">
          <el-input v-model="taskForm.schedule" placeholder="Cron 表达式，如: 0 2 * * *" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveTask">保存</el-button>
      </template>
    </el-dialog>

    <!-- 执行任务对话框 -->
    <el-dialog
      title="执行任务"
      v-model="executeDialogVisible"
      width="40%"
    >
      <el-form label-width="120px">
        <el-form-item label="输入参数">
          <el-input
            v-model="executeInputs"
            type="textarea"
            rows="6"
            placeholder='{"poi_location": {...}, "buffer_distance": 0.001}'
          />
          <div style="color: #909399; font-size: 12px; margin-top: 5px">
            JSON 格式的输入参数
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="executeDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmExecute" :loading="executing">执行</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import * as spatialApi from '@/api/spatial'

const loading = ref(false)
const tasks = ref([])
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const dialogVisible = ref(false)
const dialogTitle = ref('创建任务')
const taskForm = ref({
  id: null,
  name: '',
  description: '',
  workflowDefJson: '',
  schedule: ''
})

const executeDialogVisible = ref(false)
const currentExecuteTask = ref(null)
const executeInputs = ref('{}')
const executing = ref(false)

// 加载任务列表
const loadTasks = async () => {
  loading.value = true
  try {
    const res = await spatialApi.listTasks(currentPage.value, pageSize.value)
    tasks.value = res.data.tasks || []
    total.value = res.data.total || 0
  } catch (error) {
    ElMessage.error('加载任务列表失败: ' + error.message)
  } finally {
    loading.value = false
  }
}

// 显示创建对话框
const showCreateDialog = () => {
  dialogTitle.value = '创建任务'
  taskForm.value = {
    id: null,
    name: '',
    description: '',
    workflowDefJson: JSON.stringify({
      tasks: [
        {
          id: 't1',
          operator: 'buffer',
          params: {
            input_gdf: {
              type: 'FeatureCollection',
              features: [{
                type: 'Feature',
                geometry: { type: 'Point', coordinates: [116.404, 39.915] },
                properties: { name: 'Beijing' }
              }]
            },
            distance: 0.01
          },
          depends_on: []
        }
      ]
    }, null, 2),
    schedule: ''
  }
  dialogVisible.value = true
}

// 编辑任务
const editTask = (task) => {
  dialogTitle.value = '编辑任务'
  taskForm.value = {
    id: task.id,
    name: task.name,
    description: task.description,
    workflowDefJson: JSON.stringify(task.workflow_def, null, 2),
    schedule: task.schedule || ''
  }
  dialogVisible.value = true
}

// 保存任务
const saveTask = async () => {
  try {
    // 验证 JSON 格式
    const workflowDef = JSON.parse(taskForm.value.workflowDefJson)

    const data = {
      name: taskForm.value.name,
      description: taskForm.value.description,
      workflow_def: workflowDef,
      schedule: taskForm.value.schedule
    }

    if (taskForm.value.id) {
      // 更新任务
      await spatialApi.updateTask(taskForm.value.id, data)
      ElMessage.success('任务更新成功')
    } else {
      // 创建任务
      await spatialApi.createTask(data)
      ElMessage.success('任务创建成功')
    }

    dialogVisible.value = false
    loadTasks()
  } catch (error) {
    ElMessage.error('保存任务失败: ' + error.message)
  }
}

// 删除任务
const deleteTask = (task) => {
  ElMessageBox.confirm(
    `确定要删除任务 "${task.name}" 吗？`,
    '确认删除',
    {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    }
  ).then(async () => {
    try {
      await spatialApi.deleteTask(task.id)
      ElMessage.success('任务删除成功')
      loadTasks()
    } catch (error) {
      ElMessage.error('删除任务失败: ' + error.message)
    }
  }).catch(() => {})
}

// 执行任务
const executeTask = (task) => {
  currentExecuteTask.value = task
  executeInputs.value = '{}'
  executeDialogVisible.value = true
}

// 确认执行
const confirmExecute = async () => {
  executing.value = true
  try {
    const inputs = JSON.parse(executeInputs.value)
    const res = await spatialApi.executeTask(currentExecuteTask.value.id, inputs)
    ElMessage.success('任务执行成功，执行ID: ' + res.data.execution_id)
    executeDialogVisible.value = false
    loadTasks()
  } catch (error) {
    ElMessage.error('执行任务失败: ' + error.message)
  } finally {
    executing.value = false
  }
}

// 重置表单
const resetForm = () => {
  taskForm.value = {
    id: null,
    name: '',
    description: '',
    workflowDefJson: '',
    schedule: ''
  }
}

// 获取执行状态类型
const getExecutionStatusType = (status) => {
  const statusMap = {
    success: 'success',
    failed: 'danger',
    running: 'warning',
    pending: 'info'
  }
  return statusMap[status] || 'info'
}

onMounted(() => {
  loadTasks()
})
</script>

<style scoped>
.spatial-tasks {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
