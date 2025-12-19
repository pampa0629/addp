<template>
  <div class="workflow-editor-page">
    <!-- 工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <h2>工作流编辑器</h2>
        <el-button type="primary" @click="handleSave">
          <el-icon><DocumentAdd /></el-icon>
          保存
        </el-button>
        <el-button type="success" @click="handleExecute" :disabled="!hasValidWorkflow">
          <el-icon><VideoPlay /></el-icon>
          执行
        </el-button>
        <el-button @click="handleSaveAs">
          <el-icon><CopyDocument /></el-icon>
          另存为
        </el-button>
        <el-button type="warning" @click="handleClear">
          <el-icon><Delete /></el-icon>
          清空
        </el-button>
      </div>
      <div class="toolbar-right">
        <el-button @click="handleExport">
          <el-icon><Download /></el-icon>
          导出
        </el-button>
        <el-button @click="handleImport">
          <el-icon><Upload /></el-icon>
          导入
        </el-button>
      </div>
    </div>

    <!-- 三栏布局 -->
    <div class="editor-content">
      <!-- 左侧: 算子面板 -->
      <div class="left-panel">
        <div class="panel-header">
          <span class="panel-title">算子面板</span>
        </div>
        <div class="panel-body">
          <OperatorPalette
            @operator-drag="handleOperatorDrag"
            @operator-click="handleOperatorClick"
          />
        </div>
      </div>

      <!-- 中间: DAG 画布 -->
      <div class="canvas-panel">
        <WorkflowDAGCanvas
          ref="canvasRef"
          :initial-workflow="workflowData"
          @update:workflow="handleWorkflowUpdate"
          @node-click="handleNodeClick"
        />
      </div>

      <!-- 右侧: 参数配置面板 -->
      <div class="right-panel">
        <div class="panel-header">
          <span class="panel-title">参数配置</span>
        </div>
        <div class="panel-body">
          <div v-if="selectedNode" class="params-container">
            <OperatorParamsPanel
              :node-id="selectedNode.id"
              :operator="selectedNode.operator"
              :param-definitions="selectedNode.paramDefs"
              :initial-params="selectedNode.params"
              @save="handleParamsSave"
            />
          </div>
          <el-empty v-else description="选择节点以配置参数" />
        </div>
      </div>
    </div>

    <!-- 保存对话框 -->
    <el-dialog
      v-model="saveDialogVisible"
      title="保存工作流"
      width="500px"
    >
      <el-form :model="saveForm" label-width="100px">
        <el-form-item label="工作流名称" required>
          <el-input v-model="saveForm.name" placeholder="请输入工作流名称" />
        </el-form-item>
        <el-form-item label="显示名称">
          <el-input v-model="saveForm.display_name" placeholder="可选" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input
            v-model="saveForm.description"
            type="textarea"
            :rows="3"
            placeholder="工作流描述"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="saveDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmSave">保存</el-button>
      </template>
    </el-dialog>

    <!-- 执行对话框 -->
    <el-dialog
      v-model="executeDialogVisible"
      title="执行工作流"
      width="500px"
    >
      <el-form label-width="100px">
        <el-form-item label="工作流任务数">
          <el-input :value="workflowData?.tasks?.length || 0" disabled />
        </el-form-item>
        <el-form-item label="执行参数">
          <el-input
            v-model="executeInputs"
            type="textarea"
            :rows="5"
            placeholder='{"key": "value"}'
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="executeDialogVisible = false">取消</el-button>
        <el-button
          type="primary"
          @click="confirmExecute"
          :loading="executing"
        >
          执行
        </el-button>
      </template>
    </el-dialog>

    <!-- 导入文件输入 -->
    <input
      ref="fileInputRef"
      type="file"
      accept=".json"
      style="display: none;"
      @change="handleFileChange"
    />
  </div>
</template>

<script setup>
import { ref, reactive, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  DocumentAdd,
  VideoPlay,
  CopyDocument,
  Delete,
  Download,
  Upload
} from '@element-plus/icons-vue'
import OperatorPalette from '@/components/workflow/OperatorPalette.vue'
import WorkflowDAGCanvas from '@/components/workflow/WorkflowDAGCanvas.vue'
import OperatorParamsPanel from '@/components/workflow/OperatorParamsPanel.vue'
import { createDevItem, executeDevItem } from '@/api/devItem'
import { getOperatorDetail } from '@/api/operator'

const router = useRouter()

// 画布引用
const canvasRef = ref(null)
const fileInputRef = ref(null)

// 工作流数据
const workflowData = ref({
  tasks: []
})

// 选中节点
const selectedNode = ref(null)

// 对话框状态
const saveDialogVisible = ref(false)
const saveForm = reactive({
  name: '',
  display_name: '',
  description: ''
})

const executeDialogVisible = ref(false)
const executeInputs = ref('{}')
const executing = ref(false)

// 计算属性
const hasValidWorkflow = computed(() => {
  return workflowData.value?.tasks?.length > 0
})

// 工作流更新处理
const handleWorkflowUpdate = (workflow) => {
  workflowData.value = workflow
  console.log('工作流已更新:', workflow)
}

// 节点点击处理(双击节点配置参数)
const handleNodeClick = async (node) => {
  try {
    console.log('节点被点击:', node)

    // 获取算子的完整参数定义
    const response = await getOperatorDetail(node.operator)
    const operator = response.operator  // 从响应中提取 operator 对象

    // 转换参数定义为对象格式(用于 OperatorParamsPanel)
    const paramDefs = {}
    if (operator.parameters && Array.isArray(operator.parameters)) {
      operator.parameters.forEach(param => {
        paramDefs[param.name] = param.description || `${param.name}参数`
      })
    }

    selectedNode.value = {
      id: node.id,
      operator: node.operator,
      params: node.params || {},
      paramDefs: paramDefs
    }
  } catch (error) {
    console.error('加载算子详情失败:', error)
    ElMessage.error('加载算子参数定义失败')
  }
}

// 算子拖拽/点击处理
const handleOperatorDrag = (operator) => {
  console.log('算子拖拽:', operator)
}

const handleOperatorClick = (operator) => {
  console.log('算子点击:', operator)
}

// 参数保存处理
const handleParamsSave = (data) => {
  if (canvasRef.value) {
    canvasRef.value.updateNodeParams(data.nodeId, data.params)
    ElMessage.success('参数已保存')
  }
}

// 保存工作流
const handleSave = () => {
  if (!hasValidWorkflow.value) {
    ElMessage.warning('工作流为空，请先添加任务节点')
    return
  }
  saveDialogVisible.value = true
}

const confirmSave = async () => {
  if (!saveForm.name) {
    ElMessage.warning('请输入工作流名称')
    return
  }

  try {
    const workflow = canvasRef.value?.getWorkflow()

    await createDevItem({
      name: saveForm.name,
      display_name: saveForm.display_name,
      dev_type: 'workflow',
      description: saveForm.description,
      content: workflow
    })

    ElMessage.success('保存成功')
    saveDialogVisible.value = false

    // 重置表单
    Object.assign(saveForm, {
      name: '',
      display_name: '',
      description: ''
    })
  } catch (error) {
    console.error('保存工作流失败:', error)
    ElMessage.error('保存失败: ' + (error.response?.data?.error || error.message))
  }
}

// 另存为
const handleSaveAs = () => {
  handleSave()
}

// 执行工作流
const handleExecute = () => {
  if (!hasValidWorkflow.value) {
    ElMessage.warning('工作流为空，无法执行')
    return
  }
  executeInputs.value = '{}'
  executeDialogVisible.value = true
}

const confirmExecute = async () => {
  try {
    executing.value = true

    let inputs = {}
    try {
      inputs = JSON.parse(executeInputs.value)
    } catch {
      ElMessage.warning('执行参数必须是有效的 JSON')
      return
    }

    const workflow = canvasRef.value?.getWorkflow()

    // 创建临时任务并执行
    const tempTask = await createDevItem({
      name: `临时工作流_${Date.now()}`,
      dev_type: 'workflow',
      content: workflow
    })

    await executeDevItem(tempTask.id, inputs)

    ElMessage.success('工作流已提交执行')
    executeDialogVisible.value = false

    // 跳转到执行监控页面
    router.push('/executions')
  } catch (error) {
    console.error('执行工作流失败:', error)
    ElMessage.error('执行失败: ' + (error.response?.data?.error || error.message))
  } finally {
    executing.value = false
  }
}

// 清空画布
const handleClear = async () => {
  try {
    await ElMessageBox.confirm('确定要清空画布吗？', '确认清空', {
      type: 'warning'
    })

    if (canvasRef.value) {
      canvasRef.value.clearGraph()
    }

    workflowData.value = { tasks: [] }
    selectedNode.value = null
    ElMessage.success('画布已清空')
  } catch (error) {
    // 用户取消
  }
}

// 导出工作流
const handleExport = () => {
  if (!hasValidWorkflow.value) {
    ElMessage.warning('工作流为空，无法导出')
    return
  }

  try {
    const workflow = canvasRef.value?.getWorkflow()
    const json = JSON.stringify(workflow, null, 2)

    const blob = new Blob([json], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `workflow_${Date.now()}.json`
    link.click()
    URL.revokeObjectURL(url)

    ElMessage.success('导出成功')
  } catch (error) {
    console.error('导出工作流失败:', error)
    ElMessage.error('导出失败: ' + error.message)
  }
}

// 导入工作流
const handleImport = () => {
  fileInputRef.value?.click()
}

const handleFileChange = async (event) => {
  const file = event.target.files[0]
  if (!file) return

  try {
    const text = await file.text()
    const workflow = JSON.parse(text)

    // 验证格式
    if (!workflow.tasks || !Array.isArray(workflow.tasks)) {
      throw new Error('工作流格式不正确')
    }

    workflowData.value = workflow
    ElMessage.success('导入成功')
  } catch (error) {
    console.error('导入工作流失败:', error)
    ElMessage.error('导入失败: ' + error.message)
  } finally {
    // 清空文件输入
    event.target.value = ''
  }
}
</script>

<style scoped>
.workflow-editor-page {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: #f5f7fa;
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 20px;
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
  flex-shrink: 0;
}

.toolbar-left,
.toolbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.toolbar h2 {
  margin: 0;
  font-size: 18px;
  color: #303133;
  font-weight: 500;
  margin-right: 20px;
}

.editor-content {
  flex: 1;
  display: flex;
  overflow: hidden;
}

.left-panel,
.right-panel {
  width: 300px;
  display: flex;
  flex-direction: column;
  background: #fff;
  border-right: 1px solid #e4e7ed;
  flex-shrink: 0;
}

.right-panel {
  border-right: none;
  border-left: 1px solid #e4e7ed;
}

.canvas-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: #f5f7fa;
}

.panel-header {
  padding: 12px 16px;
  background: #fafafa;
  border-bottom: 1px solid #e4e7ed;
  flex-shrink: 0;
}

.panel-title {
  font-size: 14px;
  font-weight: 600;
  color: #303133;
}

.panel-body {
  flex: 1;
  overflow: auto;
  padding: 16px;
}

.params-container {
  height: 100%;
}
</style>
