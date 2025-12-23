<template>
  <div class="workflow-editor-page">
    <!-- AI 助手面板 -->
    <div class="ai-assistant-panel">
      <div class="ai-header">
        <span class="ai-icon">🤖</span>
        <span class="ai-title">AI 工作流助手</span>
      </div>
      <div class="ai-input-group">
        <el-input
          v-model="aiQuery"
          type="textarea"
          :rows="2"
          placeholder="描述你的 GIS 工作流需求，例如：读取 Shapefile 后进行 100 米缓冲区分析，然后导出为 GeoJSON"
          :disabled="generating"
        />
        <el-button type="primary" @click="generateWorkflow" :loading="generating" size="small">
          生成工作流
        </el-button>
      </div>
    </div>

    <!-- 工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <h2>工作流编辑器</h2>

        <!-- 引擎选择区域 -->
        <div class="engine-selector">
          <!-- 1️⃣ 工作流引擎选择（必选） -->
          <div class="engine-select-group">
            <label>工作流引擎:</label>
            <el-select
              v-model="workflowEngineId"
              placeholder="选择工作流引擎"
              style="width: 280px"
              @change="handleEngineChange"
            >
              <el-option
                v-for="engine in workflowEngines"
                :key="engine.id"
                :label="engine.display_name"
                :value="engine.id"
              >
                <div style="display: flex; justify-content: space-between; align-items: center">
                  <span>{{ engine.display_name }}</span>
                  <el-tag size="small" type="success" style="margin-left: 8px">
                    {{ getEngineTag(engine) }}
                  </el-tag>
                </div>
              </el-option>
            </el-select>
          </div>

          <!-- 2️⃣ Spark 运行时选择（仅 Spark Sedona 引擎需要） -->
          <div v-if="needsSparkRuntime()" class="spark-runtime-select-group">
            <label>Spark 运行时:</label>
            <el-select
              v-model="sparkRuntimeId"
              placeholder="选择 Spark 集群"
              style="width: 280px"
              :disabled="sparkRuntimes.length === 0"
            >
              <el-option
                v-for="runtime in sparkRuntimes"
                :key="runtime.id"
                :label="formatRuntimeLabel(runtime)"
                :value="runtime.id"
              >
                <div>
                  <div>{{ runtime.display_name }}</div>
                  <div style="font-size: 12px; color: #8492a6">
                    {{ runtime.connection_info?.spark_master || '未配置' }}
                  </div>
                </div>
              </el-option>
            </el-select>

            <!-- 无可用运行时提示 -->
            <el-alert
              v-if="sparkRuntimes.length === 0"
              type="warning"
              :closable="false"
              style="margin-top: 8px"
            >
              未找到可用的 Spark 运行时，请先在系统模块注册 Spark 集群
            </el-alert>
          </div>
        </div>
      </div>

      <div class="toolbar-right">
        <el-button type="primary" @click="handleSave" :disabled="!canSave()">
          <el-icon><DocumentAdd /></el-icon>
          保存
        </el-button>
        <el-button type="success" @click="handleExecute" :disabled="!canExecute()">
          <el-icon><VideoPlay /></el-icon>
          执行
        </el-button>
        <el-button @click="handleSaveAs">
          <el-icon><CopyDocument /></el-icon>
          另存为
        </el-button>
        <el-button type="info" @click="handleViewJSON">
          <el-icon><Document /></el-icon>
          查看 JSON
        </el-button>
        <el-button type="warning" @click="handleClear">
          <el-icon><Delete /></el-icon>
          清空
        </el-button>
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
            :engine-type="null"
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
              :parameters="selectedNode.parameters"
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

    <!-- JSON 查看对话框 -->
    <el-dialog
      v-model="jsonDialogVisible"
      title="工作流 JSON"
      width="60%"
    >
      <div class="json-viewer">
        <pre>{{ workflowJSON }}</pre>
      </div>
      <template #footer>
        <el-button @click="jsonDialogVisible = false">关闭</el-button>
        <el-button type="primary" @click="copyJSON">复制到剪贴板</el-button>
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
import { ref, reactive, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  DocumentAdd,
  VideoPlay,
  CopyDocument,
  Delete,
  Download,
  Upload,
  Document
} from '@element-plus/icons-vue'
import OperatorPalette from '@/components/workflow/OperatorPalette.vue'
import WorkflowDAGCanvas from '@/components/workflow/WorkflowDAGCanvas.vue'
import OperatorParamsPanel from '@/components/workflow/OperatorParamsPanel.vue'
import { createDevItem, executeDevItem, getDevItem } from '@/api/devItem'
import { getOperatorDetail } from '@/api/operator'
import { generateWorkflowFromNL } from '@/api/copilot'
import { getWorkflowEngines, getSparkRuntimes } from '@/api/resources'

const router = useRouter()
const route = useRoute()

// 引擎选择状态
const workflowEngines = ref([])       // 工作流引擎列表
const workflowEngineId = ref(null)    // 选中的工作流引擎 ID
const selectedEngine = ref(null)      // 选中的工作流引擎对象

const sparkRuntimes = ref([])         // Spark 运行时列表
const sparkRuntimeId = ref(null)      // 选中的 Spark 运行时 ID

// AI 助手状态
const aiQuery = ref('')
const generating = ref(false)
const generatedWorkflow = ref(null)

// 画布引用
const canvasRef = ref(null)
const fileInputRef = ref(null)

// 当前任务信息
const currentTaskId = ref(null)
const currentTaskName = ref('')

// 工作流数据
const workflowData = ref({
  tasks: []
})

// 选中节点
const selectedNode = ref(null)

// 对话框状态
const saveDialogVisible = ref(false)
const jsonDialogVisible = ref(false)
const workflowJSON = ref('')
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

    // 转换参数定义为对象格式(用于向后兼容)
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
      paramDefs: paramDefs,
      parameters: operator.parameters || []  // 传递完整的参数数组
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

// ========== 引擎选择相关方法 ==========

// 加载工作流引擎列表
const loadWorkflowEngines = async () => {
  try {
    const response = await getWorkflowEngines()
    workflowEngines.value = response.data || response

    if (workflowEngines.value.length === 0) {
      ElMessage.warning('暂无可用的工作流引擎')
    }
  } catch (error) {
    console.error('加载工作流引擎失败:', error)
    ElMessage.error('加载工作流引擎失败')
  }
}

// 加载 Spark 运行时列表
const loadSparkRuntimes = async () => {
  try {
    const response = await getSparkRuntimes()
    sparkRuntimes.value = response.data || response
  } catch (error) {
    console.error('加载 Spark 运行时失败:', error)
    ElMessage.error('加载 Spark 运行时失败')
  }
}

// 选择默认引擎（优先 GeoPandas）
const selectDefaultEngine = () => {
  if (workflowEngines.value.length === 0) return

  const geopandas = workflowEngines.value.find(
    e => e.resource_type === 'api.geopandas'
  )

  if (geopandas) {
    workflowEngineId.value = geopandas.id
    selectedEngine.value = geopandas
  } else {
    workflowEngineId.value = workflowEngines.value[0].id
    selectedEngine.value = workflowEngines.value[0]
  }
}

// 引擎切换处理
const handleEngineChange = async (engineId) => {
  selectedEngine.value = workflowEngines.value.find(e => e.id === engineId)

  // 如果切换到 Spark Sedona 引擎，加载 Spark 运行时列表
  if (needsSparkRuntime()) {
    await loadSparkRuntimes()

    // 自动选择第一个运行时
    if (sparkRuntimes.value.length > 0) {
      sparkRuntimeId.value = sparkRuntimes.value[0].id
    }
  } else {
    // 切换到 GeoPandas，清空 Spark 运行时选择
    sparkRuntimeId.value = null
  }
}

// 判断是否需要选择 Spark 运行时
const needsSparkRuntime = () => {
  return selectedEngine.value?.resource_type === 'api.spark_sedona'
}

// 获取引擎标签
const getEngineTag = (engine) => {
  if (engine.resource_type === 'api.geopandas') {
    return 'GeoPandas'
  } else if (engine.resource_type === 'api.spark_sedona') {
    return 'Spark Sedona'
  }
  return engine.resource_type
}

// 格式化运行时标签
const formatRuntimeLabel = (runtime) => {
  // 根据资源类型显示不同的连接信息
  let connInfo = '未配置'

  if (runtime.connection_info) {
    if (runtime.resource_type === 'spark_sql') {
      // Spark SQL 数据源：显示 host:port/database
      const { host, port, database } = runtime.connection_info
      if (host && port) {
        connInfo = `${host}:${port}${database ? '/' + database : ''}`
      }
    } else if (runtime.connection_info.spark_master) {
      // Spark 运行时：显示 spark_master
      connInfo = runtime.connection_info.spark_master
    }
  }

  return `${runtime.display_name} (${connInfo})`
}

// 是否可以保存
const canSave = () => {
  // 必须选择工作流引擎
  if (!workflowEngineId.value) return false

  // 如果是 Spark Sedona，必须选择运行时
  if (needsSparkRuntime() && !sparkRuntimeId.value) return false

  // 必须有工作流内容
  if (!hasValidWorkflow.value) return false

  return true
}

// 是否可以执行
const canExecute = () => {
  return canSave()
}

// 保存工作流
const handleSave = () => {
  if (!canSave()) {
    if (!workflowEngineId.value) {
      ElMessage.warning('请选择工作流引擎')
    } else if (needsSparkRuntime() && !sparkRuntimeId.value) {
      ElMessage.warning('请选择 Spark 运行时')
    } else if (!hasValidWorkflow.value) {
      ElMessage.warning('工作流为空，请先添加任务节点')
    }
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

    // 构造执行配置（JSONB格式）
    const executionConfig = {
      type: 'workflow',
      engine_id: workflowEngineId.value,
      engine_type: selectedEngine.value.resource_type,
    }

    // 如果是 Spark Sedona，添加 engine_specific 配置
    if (needsSparkRuntime()) {
      executionConfig.engine_specific = {
        spark_cluster_id: sparkRuntimeId.value
      }
    }

    await createDevItem({
      name: saveForm.name,
      display_name: saveForm.display_name,
      dev_type: 'workflow',
      description: saveForm.description,
      execution_config: JSON.stringify(executionConfig),  // 序列化为 JSON 字符串
      content: {
        workflow_definition: workflow,
        inputs: {}
      }
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
  if (!canExecute()) {
    if (!workflowEngineId.value) {
      ElMessage.warning('请选择工作流引擎')
    } else if (needsSparkRuntime() && !sparkRuntimeId.value) {
      ElMessage.warning('请选择 Spark 运行时')
    } else if (!hasValidWorkflow.value) {
      ElMessage.warning('工作流为空，无法执行')
    }
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

    // 构造执行配置（JSONB格式）
    const executionConfig = {
      type: 'workflow',
      engine_id: workflowEngineId.value,
      engine_type: selectedEngine.value.resource_type,
    }

    // 如果是 Spark Sedona，添加 engine_specific 配置
    if (needsSparkRuntime()) {
      executionConfig.engine_specific = {
        spark_cluster_id: sparkRuntimeId.value
      }
    }

    // 创建临时任务并执行
    const tempTask = await createDevItem({
      name: `临时工作流_${Date.now()}`,
      dev_type: 'workflow',
      execution_config: JSON.stringify(executionConfig),
      content: {
        workflow_definition: workflow,
        inputs: inputs
      }
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

// 查看 JSON
const handleViewJSON = () => {
  if (!hasValidWorkflow.value) {
    ElMessage.warning('工作流为空')
    return
  }

  const workflow = canvasRef.value?.getWorkflow()
  workflowJSON.value = JSON.stringify(workflow, null, 2)
  jsonDialogVisible.value = true
}

// 复制 JSON 到剪贴板
const copyJSON = async () => {
  try {
    await navigator.clipboard.writeText(workflowJSON.value)
    ElMessage.success('已复制到剪贴板')
  } catch (error) {
    ElMessage.error('复制失败，请手动复制')
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

// AI 工作流生成
const generateWorkflow = async () => {
  if (!aiQuery.value.trim()) {
    ElMessage.warning('请描述你的工作流需求')
    return
  }

  generating.value = true
  try {
    const result = await generateWorkflowFromNL({
      query: aiQuery.value,
      tenant_id: 1, // TODO: 从 store 获取
      user_id: 1
    })

    // 直接加载到画布，不显示预览
    workflowData.value = result.workflow
    ElMessage.success(`工作流生成成功，包含 ${result.workflow.tasks.length} 个步骤`)
  } catch (error) {
    console.error('工作流生成失败:', error)

    // 提取后端返回的错误消息
    let errorMsg = '未知错误'
    if (error.response?.data?.detail) {
      errorMsg = error.response.data.detail
    } else if (error.message) {
      errorMsg = error.message
    }

    ElMessage.error({
      message: errorMsg,
      duration: 5000,
      showClose: true
    })
  } finally {
    generating.value = false
  }
}

const loadToCanvas = () => {
  if (generatedWorkflow.value) {
    workflowData.value = generatedWorkflow.value
    generatedWorkflow.value = null
    ElMessage.success('已加载到画布')
  }
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

// 加载已有任务
const loadTask = async (taskId) => {
  try {
    const task = await getDevItem(taskId)

    // 设置当前任务信息
    currentTaskId.value = task.id
    currentTaskName.value = task.name

    // 解析执行配置
    if (task.execution_config) {
      try {
        const config = JSON.parse(task.execution_config)

        // 恢复工作流引擎选择
        workflowEngineId.value = config.engine_id
        selectedEngine.value = workflowEngines.value.find(
          e => e.id === config.engine_id
        )

        // 如果需要 Spark 运行时，加载列表并恢复选择
        if (needsSparkRuntime() && config.engine_specific) {
          await loadSparkRuntimes()
          sparkRuntimeId.value = config.engine_specific.spark_cluster_id
        }
      } catch (error) {
        console.error('解析执行配置失败:', error)
        ElMessage.warning('工作流配置已损坏，请重新配置')
      }
    }

    // 加载工作流内容（支持新旧字段名）
    if (task.content) {
      if (task.content.workflow_definition) {
        // 新格式
        workflowData.value = task.content.workflow_definition
      } else if (task.content.tasks) {
        // 旧格式（向后兼容）
        workflowData.value = task.content
      } else {
        ElMessage.warning('该任务没有工作流内容')
        return
      }
      ElMessage.success(`已加载工作流: ${task.name}`)
    } else {
      ElMessage.warning('该任务没有工作流内容')
    }
  } catch (error) {
    console.error('加载任务失败:', error)
    ElMessage.error('加载任务失败: ' + (error.response?.data?.error || error.message))
  }
}

// 组件挂载时检查是否有任务 ID
onMounted(async () => {
  // 加载工作流引擎列表
  await loadWorkflowEngines()

  const taskId = route.query.taskId
  if (taskId) {
    // 如果是编辑模式，加载开发项
    await loadTask(taskId)
  } else {
    // 新建模式：选择默认引擎
    selectDefaultEngine()
  }
})
</script>

<style scoped>
.workflow-editor-page {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: #f5f7fa;
}

.ai-assistant-panel {
  padding: 12px 16px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  color: white;
  flex-shrink: 0;
}

.ai-header {
  display: flex;
  align-items: center;
  margin-bottom: 8px;
  font-weight: 600;
}

.ai-icon {
  font-size: 18px;
  margin-right: 8px;
}

.ai-input-group {
  display: flex;
  gap: 8px;
}

.ai-input-group :deep(.el-textarea__inner) {
  background: rgba(255, 255, 255, 0.95);
}

.workflow-preview {
  margin-top: 12px;
  padding: 12px;
  background: rgba(255, 255, 255, 0.1);
  border-radius: 4px;
}

.preview-header {
  font-weight: 600;
  margin-bottom: 8px;
}

.preview-tasks {
  max-height: 100px;
  overflow-y: auto;
  margin-bottom: 8px;
}

.task-item {
  padding: 4px 0;
  font-size: 13px;
  opacity: 0.9;
}

.preview-actions {
  display: flex;
  gap: 8px;
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  padding: 12px 20px;
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
  flex-shrink: 0;
}

.toolbar-left,
.toolbar-right {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.toolbar-left {
  flex-direction: column;
}

.toolbar-left h2 {
  margin: 0;
  font-size: 18px;
  color: #303133;
  font-weight: 500;
}

.engine-selector {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 8px;
}

.engine-select-group,
.spark-runtime-select-group {
  display: flex;
  align-items: center;
  gap: 8px;
}

.engine-select-group label,
.spark-runtime-select-group label {
  font-size: 14px;
  font-weight: 500;
  color: #606266;
  min-width: 100px;
  white-space: nowrap;
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

.json-viewer {
  max-height: 500px;
  overflow: auto;
  background-color: #f5f7fa;
  border-radius: 4px;
  padding: 12px;
}

.json-viewer pre {
  margin: 0;
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 13px;
  line-height: 1.5;
  color: #303133;
}
</style>
