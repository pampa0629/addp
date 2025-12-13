<template>
  <div class="gis-workflow-editor">
    <el-card class="editor-card">
      <template #header>
        <div class="card-header">
          <span>GIS 工作流编辑器{{ taskId ? ` - 编辑任务 #${taskId}` : ' - 新建任务' }}</span>
          <div class="header-actions">
            <el-button @click="viewJSON">查看 JSON</el-button>
            <el-button type="primary" @click="saveTask">保存任务</el-button>
            <el-button type="success" @click="executeWorkflow">执行工作流</el-button>
          </div>
        </div>
      </template>

      <div class="editor-layout">
        <!-- 左侧：算子库 -->
        <div class="left-panel">
          <h3 class="panel-title">算子库</h3>
          <OperatorPalette
            @operator-click="handleOperatorClick"
            @operator-drag="handleOperatorDrag"
          />
        </div>

        <!-- 中间：DAG 画布 -->
        <div class="center-panel">
          <GISDAGCanvas
            ref="dagCanvas"
            :initial-workflow="workflow"
            @update:workflow="handleWorkflowUpdate"
            @node-click="handleNodeClick"
          />
        </div>

        <!-- 右侧：参数面板 -->
        <div class="right-panel">
          <h3 class="panel-title">参数配置</h3>
          <OperatorParamsPanel
            :node-id="selectedNode.id"
            :operator="selectedNode.operator"
            :param-definitions="selectedNode.paramDefs"
            :initial-params="selectedNode.params"
            @save="handleParamsSave"
          />
        </div>
      </div>
    </el-card>

    <!-- 保存任务对话框 -->
    <el-dialog
      v-model="saveDialogVisible"
      title="保存为 GIS 任务"
      width="40%"
    >
      <el-form :model="taskForm" label-width="100px">
        <el-form-item label="任务名称" required>
          <el-input v-model="taskForm.name" placeholder="例如: 缓冲区分析" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input
            type="textarea"
            v-model="taskForm.description"
            rows="3"
            placeholder="描述该任务的用途"
          />
        </el-form-item>
        <el-form-item label="调度计划">
          <el-input
            v-model="taskForm.schedule"
            placeholder="Cron 表达式（可选），例如: 0 0 * * * 表示每天零点"
          />
          <div class="form-tip">
            留空表示不启用自动调度
          </div>
        </el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="taskForm.status">
            <el-radio label="active">启用</el-radio>
            <el-radio label="inactive">禁用</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="saveDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmSaveTask" :loading="saving">
          保存
        </el-button>
      </template>
    </el-dialog>

    <!-- 执行工作流对话框 -->
    <el-dialog
      v-model="executeDialogVisible"
      title="执行工作流"
      width="40%"
    >
      <el-alert type="info" :closable="false" style="margin-bottom: 16px;">
        请提供工作流的输入参数（JSON 格式）
      </el-alert>
      <el-input
        type="textarea"
        v-model="executeInputsJSON"
        rows="10"
        placeholder='例如:
{
  "input_gdf": {
    "type": "postgis",
    "connection": "postgresql://user:pass@localhost:5432/db",
    "table": "public.poi"
  }
}'
      />
      <template #footer>
        <el-button @click="executeDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmExecute" :loading="executing">
          执行
        </el-button>
      </template>
    </el-dialog>

    <!-- JSON 查看对话框 -->
    <el-dialog
      v-model="jsonDialogVisible"
      title="工作流 JSON 定义"
      width="50%"
    >
      <pre class="json-viewer">{{ JSON.stringify(workflow, null, 2) }}</pre>
      <template #footer>
        <el-button @click="jsonDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="copyJSON">复制</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import OperatorPalette from '@/components/gis/OperatorPalette.vue'
import GISDAGCanvas from '@/components/gis/GISDAGCanvas.vue'
import OperatorParamsPanel from '@/components/gis/OperatorParamsPanel.vue'
import * as spatialApi from '@/api/spatial'

const route = useRoute()
const router = useRouter()

const taskId = ref(route.query.taskId)
const dagCanvas = ref(null)
const workflow = ref({ tasks: [] })
const selectedNode = ref({
  id: '',
  operator: '',
  params: {},
  paramDefs: {}
})
const operatorsMap = ref({}) // 算子定义映射

const saveDialogVisible = ref(false)
const executeDialogVisible = ref(false)
const jsonDialogVisible = ref(false)
const saving = ref(false)
const executing = ref(false)

const taskForm = ref({
  name: '',
  description: '',
  schedule: '',
  status: 'active'
})

const executeInputsJSON = ref('{}')

// 加载任务（编辑模式）
const loadTask = async () => {
  if (!taskId.value) return

  const loadingMessage = ElMessage({
    message: '正在加载任务数据...',
    type: 'info',
    duration: 0
  })

  try {
    const res = await spatialApi.getTask(taskId.value)
    const task = res.data

    // 验证必需字段
    if (!task || !task.name) {
      throw new Error('任务数据格式错误')
    }

    taskForm.value.name = task.name
    taskForm.value.description = task.description || ''
    taskForm.value.schedule = task.schedule || ''
    taskForm.value.status = task.status || 'active'

    workflow.value = task.workflow_def || { tasks: [] }

    // 验证工作流定义
    if (!workflow.value.tasks || workflow.value.tasks.length === 0) {
      ElMessage.warning('该任务的工作流为空，请添加算子到画布中')
    }

    loadingMessage.close()
    ElMessage.success(`任务 "${task.name}" 加载成功`)
  } catch (error) {
    loadingMessage.close()
    ElMessage.error('加载任务失败: ' + error.message)
    // 回退到列表页
    router.push({ name: 'GISTasks' })
  }
}

// 加载算子列表
const loadOperators = async () => {
  try {
    const res = await spatialApi.listOperators()
    operatorsMap.value = res.data.operators || {}
  } catch (error) {
    console.error('加载算子列表失败:', error)
  }
}

// 处理工作流更新
const handleWorkflowUpdate = (newWorkflow) => {
  workflow.value = newWorkflow
}

// 处理节点点击（从 DAG 画布）
const handleNodeClick = (node) => {
  selectedNode.value = {
    id: node.id,
    operator: node.operator,
    params: node.params || {},
    paramDefs: operatorsMap.value[node.operator]?.params || {}
  }
}

// 处理算子点击（从算子库）
const handleOperatorClick = (operator) => {
  ElMessage.info(`点击了算子: ${operator.name}，请拖拽到画布中添加`)
}

// 处理算子拖拽
const handleOperatorDrag = (operator) => {
  // 可以在这里添加拖拽提示
}

// 处理参数保存
const handleParamsSave = ({ nodeId, params }) => {
  if (dagCanvas.value) {
    dagCanvas.value.updateNodeParams(nodeId, params)
  }
  ElMessage.success('参数已保存')
}

// 查看 JSON
const viewJSON = () => {
  // 确保获取最新的工作流定义
  if (dagCanvas.value) {
    workflow.value = dagCanvas.value.getWorkflow()
  }
  jsonDialogVisible.value = true
}

// 复制 JSON
const copyJSON = () => {
  const jsonStr = JSON.stringify(workflow.value, null, 2)
  navigator.clipboard.writeText(jsonStr).then(() => {
    ElMessage.success('已复制到剪贴板')
  }).catch(() => {
    ElMessage.error('复制失败，请手动复制')
  })
}

// 保存任务
const saveTask = () => {
  // 确保获取最新的工作流定义
  if (dagCanvas.value) {
    workflow.value = dagCanvas.value.getWorkflow()
  }

  // 验证工作流
  if (!workflow.value.tasks || workflow.value.tasks.length === 0) {
    ElMessage.warning('请先添加算子到画布中')
    return
  }

  saveDialogVisible.value = true
}

// 确认保存任务
const confirmSaveTask = async () => {
  if (!taskForm.value.name.trim()) {
    ElMessage.warning('请输入任务名称')
    return
  }

  // 确保获取最新的工作流定义
  if (dagCanvas.value) {
    workflow.value = dagCanvas.value.getWorkflow()
  }

  saving.value = true
  try {
    const taskData = {
      name: taskForm.value.name,
      description: taskForm.value.description,
      workflow_def: workflow.value,
      schedule: taskForm.value.schedule || '',
      status: taskForm.value.status
    }

    if (taskId.value) {
      // 更新已有任务
      await spatialApi.updateTask(taskId.value, taskData)
      ElMessage.success('任务已更新')
    } else {
      // 创建新任务
      const res = await spatialApi.createTask(taskData)
      const createdTask = res.data
      taskId.value = createdTask.id
      ElMessage.success('任务已保存，任务ID: ' + createdTask.id)
      // 更新 URL 为编辑模式
      router.replace({ query: { taskId: createdTask.id } })
    }

    saveDialogVisible.value = false
  } catch (error) {
    ElMessage.error('保存失败: ' + error.message)
  } finally {
    saving.value = false
  }
}

// 执行工作流
const executeWorkflow = () => {
  // 确保获取最新的工作流定义
  if (dagCanvas.value) {
    workflow.value = dagCanvas.value.getWorkflow()
  }

  if (!workflow.value.tasks || workflow.value.tasks.length === 0) {
    ElMessage.warning('请先添加算子到画布中')
    return
  }

  executeDialogVisible.value = true
}

// 确认执行
const confirmExecute = async () => {
  let inputs
  try {
    inputs = JSON.parse(executeInputsJSON.value || '{}')
  } catch (error) {
    ElMessage.error('输入参数 JSON 格式错误')
    return
  }

  executing.value = true
  try {
    // 确保获取最新的工作流定义
    if (dagCanvas.value) {
      workflow.value = dagCanvas.value.getWorkflow()
    }

    const res = await spatialApi.executeWorkflow(workflow.value, inputs)

    ElMessage.success('工作流已提交执行，执行ID: ' + res.data.execution_id)
    executeDialogVisible.value = false

    // 跳转到执行详情页
    router.push({ name: 'GISExecutionDetail', params: { id: res.data.execution_id } })
  } catch (error) {
    ElMessage.error('执行失败: ' + error.message)
  } finally {
    executing.value = false
  }
}

onMounted(async () => {
  await loadOperators()
  await loadTask()
})
</script>

<style scoped>
.gis-workflow-editor {
  padding: 20px;
  height: calc(100vh - 40px);
}

.editor-card {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.editor-card :deep(.el-card__body) {
  flex: 1;
  overflow: hidden;
  padding: 0;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-actions {
  display: flex;
  gap: 8px;
}

.editor-layout {
  height: 100%;
  display: flex;
  gap: 0;
}

.left-panel,
.right-panel {
  width: 300px;
  background: #f5f7fa;
  border: 1px solid #e4e7ed;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.left-panel {
  border-right: none;
}

.right-panel {
  border-left: none;
}

.center-panel {
  flex: 1;
  overflow: hidden;
  background: #fff;
  border-top: 1px solid #e4e7ed;
  border-bottom: 1px solid #e4e7ed;
}

.panel-title {
  font-size: 14px;
  font-weight: 600;
  color: #303133;
  padding: 12px 16px;
  margin: 0;
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
  flex-shrink: 0;
}

.left-panel > :deep(.operator-palette),
.right-panel > :deep(.operator-params-panel) {
  flex: 1;
  overflow: hidden;
  padding: 16px;
}

.json-viewer {
  background: #f5f7fa;
  padding: 16px;
  border-radius: 4px;
  overflow-x: auto;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.5;
  color: #303133;
  max-height: 500px;
}

.form-tip {
  font-size: 12px;
  color: #909399;
  margin-top: 4px;
}
</style>
