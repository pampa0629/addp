<template>
  <div class="gis-tasks">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>GIS 任务管理</span>
          <el-button type="primary" @click="showCreateDialog">新建任务</el-button>
        </div>
      </template>

      <!-- 筛选栏 -->
      <el-form :inline="true" class="filter-form">
        <el-form-item label="任务名称">
          <el-input
            v-model="filters.name"
            placeholder="搜索任务名称"
            clearable
            style="width: 200px"
            @clear="loadTasks"
          />
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="filters.status" placeholder="全部" clearable style="width: 120px" @change="loadTasks">
            <el-option label="全部" value="" />
            <el-option label="启用" value="active" />
            <el-option label="禁用" value="inactive" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="loadTasks">搜索</el-button>
          <el-button @click="resetFilters">重置</el-button>
        </el-form-item>
      </el-form>

      <!-- 任务列表 -->
      <el-table :data="tasks" v-loading="loading" style="width: 100%">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="任务名称" min-width="180">
          <template #default="{ row }">
            <el-link type="primary" @click="editTask(row)">{{ row.name }}</el-link>
          </template>
        </el-table-column>
        <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip />
        <el-table-column prop="schedule" label="调度配置" width="150">
          <template #default="{ row }">
            <el-tooltip v-if="row.schedule" :content="row.schedule" placement="top">
              <el-tag size="small">{{ row.schedule }}</el-tag>
            </el-tooltip>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="status" label="状态" width="100">
          <template #default="{ row }">
            <el-switch
              v-model="row.status"
              active-value="active"
              inactive-value="inactive"
              @change="toggleStatus(row)"
            />
          </template>
        </el-table-column>
        <el-table-column label="最近执行" width="150">
          <template #default="{ row }">
            <div v-if="row.last_execution_status">
              <el-tag :type="getExecutionStatusType(row.last_execution_status)" size="small">
                {{ row.last_execution_status }}
              </el-tag>
              <div class="text-muted" style="font-size: 12px; margin-top: 4px">
                {{ formatTime(row.last_execution_started_at) }}
              </div>
            </div>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="160">
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="280" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" @click="editWorkflow(row)">编辑</el-button>
            <el-button size="small" type="success" @click="executeTask(row)">执行</el-button>
            <el-button size="small" @click="viewExecutions(row)">历史</el-button>
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
      width="60%"
      @close="resetForm"
    >
      <el-form :model="taskForm" label-width="120px">
        <el-form-item label="任务名称" required>
          <el-input v-model="taskForm.name" placeholder="请输入任务名称（如：计算一个点周边500米商铺的个数）" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="taskForm.description" type="textarea" rows="3" placeholder="任务描述" />
        </el-form-item>
        <el-form-item label="工作流定义" required>
          <el-input
            v-model="taskForm.workflowDefJson"
            type="textarea"
            rows="12"
            placeholder='{"tasks": [{"id": "t1", "operator": "buffer", "params": {...}, "depends_on": []}]}'
          />
          <div style="color: #909399; font-size: 12px; margin-top: 5px">
            JSON 格式的工作流定义（DAG 结构）
          </div>
        </el-form-item>
        <el-form-item label="调度计划">
          <el-input v-model="taskForm.schedule" placeholder="Cron 表达式（可选），如: 0 0 * * *（每天零点）" />
          <div style="color: #909399; font-size: 12px; margin-top: 5px">
            留空表示不自动调度，仅手动执行或被 Orchestrator 调用
          </div>
        </el-form-item>
        <el-form-item label="是否启用">
          <el-switch v-model="taskForm.status" active-value="active" inactive-value="inactive" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveTask" :loading="saving">保存</el-button>
      </template>
    </el-dialog>

    <!-- 执行任务对话框 -->
    <el-dialog
      :title="`执行任务 - ${currentExecuteTask?.name || ''}`"
      v-model="executeDialogVisible"
      width="50%"
    >
      <el-alert type="info" :closable="false" style="margin-bottom: 16px">
        请提供工作流的输入参数（JSON 格式）
      </el-alert>
      <el-form label-width="120px">
        <el-form-item label="输入参数">
          <el-input
            v-model="executeInputs"
            type="textarea"
            rows="10"
            placeholder='{"input_gdf": {"type": "postgis", "table": "public.poi"}, "distance": 0.01}'
          />
          <div style="color: #909399; font-size: 12px; margin-top: 5px">
            JSON 格式的输入参数（根据任务定义的 input_schema）
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
import { useRouter } from 'vue-router'
import * as spatialApi from '@/api/spatial'

const router = useRouter()

const loading = ref(false)
const saving = ref(false)
const tasks = ref([])
const currentPage = ref(1)
const pageSize = ref(20)
const total = ref(0)

const filters = ref({
  name: '',
  status: ''
})

const dialogVisible = ref(false)
const dialogTitle = ref('创建任务')
const taskForm = ref({
  id: null,
  name: '',
  description: '',
  workflowDefJson: '',
  schedule: '',
  status: 'active'
})

const executeDialogVisible = ref(false)
const currentExecuteTask = ref(null)
const executeInputs = ref('{}')
const executing = ref(false)

// 加载任务列表
const loadTasks = async () => {
  loading.value = true
  try {
    const res = await spatialApi.listTasks(currentPage.value, pageSize.value, filters.value)
    tasks.value = res.data.tasks || []
    total.value = res.data.total || 0
  } catch (error) {
    ElMessage.error('加载任务列表失败: ' + error.message)
  } finally {
    loading.value = false
  }
}

// 重置筛选
const resetFilters = () => {
  filters.value = {
    name: '',
    status: ''
  }
  currentPage.value = 1
  loadTasks()
}

// 显示创建对话框
const showCreateDialog = () => {
  dialogTitle.value = '创建 GIS 任务'
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
            distance: 0.01,
            resolution: 16
          },
          depends_on: []
        }
      ]
    }, null, 2),
    schedule: '',
    status: 'active'
  }
  dialogVisible.value = true
}

// 编辑工作流（跳转到可视化编辑器）
const editWorkflow = (task) => {
  router.push({
    name: 'GISWorkflowEditor',
    query: { taskId: task.id }
  })
}

// 编辑任务基础信息
const editTaskInfo = (task) => {
  dialogTitle.value = '配置 GIS 任务'
  taskForm.value = {
    id: task.id,
    name: task.name,
    description: task.description,
    workflowDefJson: JSON.stringify(task.workflow_def, null, 2),
    schedule: task.schedule || '',
    status: task.status || 'active'
  }
  dialogVisible.value = true
}

// 保存任务
const saveTask = async () => {
  saving.value = true
  try {
    // 验证 JSON 格式
    let workflowDef
    try {
      workflowDef = JSON.parse(taskForm.value.workflowDefJson)
    } catch (e) {
      ElMessage.error('工作流定义 JSON 格式错误: ' + e.message)
      return
    }

    // 验证必需字段
    if (!taskForm.value.name.trim()) {
      ElMessage.error('请输入任务名称')
      return
    }

    const data = {
      name: taskForm.value.name,
      description: taskForm.value.description,
      workflow_def: workflowDef,
      schedule: taskForm.value.schedule,
      status: taskForm.value.status
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
  } finally {
    saving.value = false
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

// 切换任务状态
const toggleStatus = async (task) => {
  try {
    await spatialApi.updateTask(task.id, { status: task.status })
    ElMessage.success(`任务已${task.status === 'active' ? '启用' : '禁用'}`)
  } catch (error) {
    ElMessage.error('更新状态失败: ' + error.message)
    // 回滚状态
    task.status = task.status === 'active' ? 'inactive' : 'active'
  }
}

// 执行任务
const executeTask = (task) => {
  currentExecuteTask.value = task
  executeInputs.value = JSON.stringify({
    input_gdf: {
      type: 'postgis',
      connection: 'postgresql://user:pass@localhost:5432/db',
      table: 'public.example_table'
    }
  }, null, 2)
  executeDialogVisible.value = true
}

// 确认执行
const confirmExecute = async () => {
  executing.value = true
  try {
    let inputs
    try {
      inputs = JSON.parse(executeInputs.value)
    } catch (e) {
      ElMessage.error('输入参数 JSON 格式错误: ' + e.message)
      return
    }

    const res = await spatialApi.executeTask(currentExecuteTask.value.id, inputs)

    const executionId = res.data.execution_id
    ElMessage.success({
      message: '任务已提交执行',
      duration: 3000
    })

    executeDialogVisible.value = false

    // 跳转到执行详情页
    router.push({
      name: 'GISExecutionDetail',
      params: { id: executionId }
    })
  } catch (error) {
    ElMessage.error('执行任务失败: ' + error.message)
  } finally {
    executing.value = false
  }
}

// 查看执行历史
const viewExecutions = (task) => {
  router.push({
    name: 'GISExecutions',
    query: { task_id: task.id }
  })
}

// 重置表单
const resetForm = () => {
  taskForm.value = {
    id: null,
    name: '',
    description: '',
    workflowDefJson: '',
    schedule: '',
    status: 'active'
  }
}

// 获取执行状态类型
const getExecutionStatusType = (status) => {
  const statusMap = {
    success: 'success',
    failed: 'danger',
    running: 'warning',
    pending: 'info',
    timeout: ''
  }
  return statusMap[status] || 'info'
}

// 格式化时间
const formatTime = (time) => {
  if (!time) return '-'
  const date = new Date(time)
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}

onMounted(() => {
  loadTasks()
})
</script>

<style scoped>
.gis-tasks {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.filter-form {
  margin-bottom: 16px;
  padding-bottom: 16px;
  border-bottom: 1px solid #ebeef5;
}

.text-muted {
  color: #909399;
}
</style>
