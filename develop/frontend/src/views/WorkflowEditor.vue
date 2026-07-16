<template>
  <div class="workflow-editor-page">
    <!-- 工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <h2>{{ t('develop.workflow.title') }}</h2>

        <!-- 引擎选择区域 -->
        <div class="engine-selector">
          <!-- 工作流引擎选择（必选） -->
          <div class="engine-select-group">
            <label>{{ t('develop.workflow.workflowEngine') }}:</label>
            <el-select
              v-model="workflowEngineId"
              :placeholder="t('develop.workflow.selectEngine')"
              style="width: 280px"
              @change="handleEngineChange"
            >
              <el-option
                v-for="engine in workflowEngines"
                :key="engine.id"
                :label="engine.name"
                :value="engine.id"
              >
                <div style="display: flex; justify-content: space-between; align-items: center">
                  <span>{{ engine.name }}</span>
                  <el-tag size="small" type="success" style="margin-left: 8px">
                    {{ getEngineTag(engine) }}
                  </el-tag>
                </div>
              </el-option>
            </el-select>
          </div>

          <!-- Spark 通用引擎资源选择（仅 Spark Workflow 需要） -->
          <div v-if="needsSparkRuntime()" class="spark-runtime-select-group">
            <label>{{ t('develop.workflow.sparkRuntime') }}:</label>
            <el-select
              v-model="sparkRuntimeId"
              :placeholder="t('develop.workflow.selectSparkCluster')"
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
                  <div>{{ runtime.name }}</div>
                  <div style="font-size: 12px; color: #8492a6">
                    {{ runtime.connection_info?.spark_master || t('develop.workflow.notConfigured') }}
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
              {{ t('develop.workflow.noSparkRuntime') }}
            </el-alert>
          </div>
        </div>
      </div>

      <div class="toolbar-right">
        <el-button type="primary" @click="handleSave" :disabled="!canSave()">
          <el-icon><DocumentAdd /></el-icon>
          {{ t('develop.workflow.save') }}
        </el-button>
        <el-button type="success" @click="handleExecute" :disabled="!canExecute()">
          <el-icon><VideoPlay /></el-icon>
          {{ t('develop.workflow.execute') }}
        </el-button>
        <el-button @click="handleSaveAs">
          <el-icon><CopyDocument /></el-icon>
          {{ t('develop.workflow.saveAs') }}
        </el-button>
        <el-button type="info" @click="handleViewJSON">
          <el-icon><Document /></el-icon>
          {{ t('develop.workflow.viewJson') }}
        </el-button>
        <el-button type="warning" @click="handleClear">
          <el-icon><Delete /></el-icon>
          {{ t('develop.workflow.clear') }}
        </el-button>
        <el-button @click="handleExport">
          <el-icon><Download /></el-icon>
          {{ t('develop.workflow.export') }}
        </el-button>
        <el-button @click="handleImport">
          <el-icon><Upload /></el-icon>
          {{ t('develop.workflow.import') }}
        </el-button>
      </div>
    </div>

    <!-- 三栏布局 -->
    <div class="editor-content">
      <!-- 左侧: 算子面板 -->
      <div class="left-panel">
        <div class="panel-header">
          <span class="panel-title">{{ t('develop.workflow.operatorPanel') }}</span>
        </div>
        <div class="panel-body">
          <OperatorPalette
            :workflow-engine-id="workflowEngineId"
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
          <span class="panel-title">{{ t('develop.workflow.paramsPanel') }}</span>
        </div>
        <div class="panel-body">
          <div v-if="selectedNode" class="params-container">
            <OperatorParamsPanel
              :node-id="selectedNode.id"
              :operator="selectedNode.operator"
              :public-parameters="selectedNode.publicParameters"
              :initial-params="selectedNode.params"
              @save="handleParamsSave"
            />
          </div>
          <el-empty v-else :description="t('develop.workflow.selectNodeHint')" />
        </div>
      </div>
    </div>

    <!-- 保存对话框 -->
    <el-dialog
      v-model="saveDialogVisible"
      :title="t('develop.workflow.saveDialogTitle')"
      width="500px"
    >
      <el-form :model="saveForm" label-width="100px">
        <el-form-item :label="t('develop.workflow.workflowName')" required>
          <el-input v-model="saveForm.name" :placeholder="t('develop.workflow.workflowNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('develop.workflow.displayName')">
          <el-input v-model="saveForm.display_name" :placeholder="t('develop.workflow.optional')" />
        </el-form-item>
        <el-form-item :label="t('develop.workflow.description')">
          <el-input
            v-model="saveForm.description"
            type="textarea"
            :rows="3"
            :placeholder="t('develop.workflow.descriptionPlaceholder')"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="saveDialogVisible = false">{{ t('develop.workflow.cancel') }}</el-button>
        <el-button type="primary" @click="confirmSave">{{ t('develop.workflow.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 执行对话框 -->
    <el-dialog
      v-model="executeDialogVisible"
      :title="t('develop.workflow.executeDialogTitle')"
      width="500px"
    >
      <el-form label-width="100px">
        <el-form-item :label="t('develop.workflow.taskCount')">
          <el-input :value="workflowData?.tasks?.length || 0" disabled />
        </el-form-item>
        <el-form-item :label="t('develop.workflow.execParams')">
          <el-input
            v-model="executeInputs"
            type="textarea"
            :rows="5"
            placeholder='{"key": "value"}'
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="executeDialogVisible = false">{{ t('develop.workflow.cancel') }}</el-button>
        <el-button
          type="primary"
          @click="confirmExecute"
          :loading="executing"
        >
          {{ t('develop.workflow.execute') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- JSON 查看对话框 -->
    <el-dialog
      v-model="jsonDialogVisible"
      :title="t('develop.workflow.jsonDialogTitle')"
      width="60%"
    >
      <div class="json-viewer">
        <pre>{{ workflowJSON }}</pre>
      </div>
      <template #footer>
        <el-button @click="jsonDialogVisible = false">{{ t('develop.workflow.close') }}</el-button>
        <el-button type="primary" @click="copyJSON">{{ t('develop.workflow.copyToClipboard') }}</el-button>
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

    <!-- AI 工作流生成：魔法棒 + 向左滑出的输入面板 -->
    <div class="ai-fab-wrapper">
      <!-- 滑出的输入面板 -->
      <transition name="ai-slide">
        <div v-if="aiDialogOpen" class="ai-inline-panel">
          <div class="ai-panel-header">
            <span class="ai-panel-title">{{ t('develop.workflow.aiTitle') }}</span>
            <el-icon class="ai-panel-close" @click="aiDialogOpen = false"><Close /></el-icon>
          </div>
          <el-input
            ref="aiInputRef"
            v-model="aiQuery"
            type="textarea"
            :rows="4"
            :placeholder="t('develop.workflow.aiPlaceholder')"
            :disabled="generating"
            @keydown.ctrl.enter="generateWorkflow"
            class="ai-panel-input"
          />
          <div class="ai-panel-footer">
            <span class="ai-panel-hint">Ctrl+Enter {{ t('develop.workflow.generateWorkflow') }}</span>
            <el-button
              type="primary"
              :loading="generating"
              size="small"
              @click="generateWorkflow"
            >
              {{ t('develop.workflow.generateWorkflow') }}
            </el-button>
          </div>
        </div>
      </transition>

      <!-- 魔法棒 FAB 按钮 -->
      <div
        class="ai-fab"
        :class="{ 'ai-fab--active': aiDialogOpen }"
        @click="toggleAiPanel"
        :title="t('develop.workflow.aiTitle')"
      >
        <el-icon :size="20"><MagicStick /></el-icon>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  DocumentAdd,
  VideoPlay,
  CopyDocument,
  Delete,
  Download,
  Upload,
  Document,
  MagicStick,
  Close
} from '@element-plus/icons-vue'
import OperatorPalette from '@/components/workflow/OperatorPalette.vue'
import WorkflowDAGCanvas from '@/components/workflow/WorkflowDAGCanvas.vue'
import OperatorParamsPanel from '@/components/workflow/OperatorParamsPanel.vue'
import { getDevTask } from '@/api/devTask'
import { listOperatorsByWorkflowEngine } from '@/api/operator'
import { generateWorkflowFromNL } from '@/api/copilot'
import { getWorkflowEngines, getSparkRuntimes } from '@/api/engines'
import {
  createTemporaryWorkflowTask,
  executeWorkflowTask,
  saveWorkflowTask,
  updateWorkflowTask
} from '@/api/workflow'
import {
  buildWorkflowExportPayload,
  isSparkWorkflowEngine,
  isStandardWorkflowDefinition
} from '@/utils/workflowDevTaskPayload'
import { isStandardOperatorMetadata } from '@/utils/operatorMetadataContract'
import { resolveWorkflowGenerationResult } from '@/utils/workflowGenerationResult.mjs'
import { openMonitorExecution } from '@addp/common-frontend'

const route = useRoute()
const { t } = useI18n()

// 引擎选择状态
const workflowEngines = ref([])       // 工作流引擎列表
const workflowEngineId = ref(null)    // 选中的工作流引擎 ID
const selectedEngine = ref(null)      // 选中的工作流引擎对象

const sparkRuntimes = ref([])         // Spark 通用引擎资源列表
const sparkRuntimeId = ref(null)      // 选中的 Spark 通用引擎资源 ID

// AI 助手状态
const aiDialogOpen = ref(false)
const aiQuery = ref('')
const generating = ref(false)
const generatedWorkflow = ref(null)
const aiInputRef = ref(null)

const toggleAiPanel = () => {
  aiDialogOpen.value = !aiDialogOpen.value
  if (aiDialogOpen.value) {
    setTimeout(() => aiInputRef.value?.focus(), 300)
  }
}

// 画布引用
const canvasRef = ref(null)
const fileInputRef = ref(null)

// 当前任务信息
const currentTaskId = ref(null)
const currentTaskName = ref('')
const currentTask = ref(null)

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
  return isStandardWorkflowDefinition(workflowData.value)
})

// 工作流更新处理
const handleWorkflowUpdate = (workflow) => {
  workflowData.value = workflow
  console.log('工作流已更新:', workflow)
}

// 节点单击后加载参数配置。
const handleNodeClick = async (node) => {
  try {
    console.log('节点被点击:', node)

    if (!workflowEngineId.value) {
      ElMessage.warning(t('develop.workflow.selectEngineFirst'))
      return
    }

    // 从当前工作流运行时实例获取算子列表，然后查找匹配的算子
    const operatorList = await listOperatorsByWorkflowEngine(workflowEngineId.value)

    const operator = operatorList.operators?.find(op => op.name === node.operator)

    if (!operator) {
      ElMessage.error(t('develop.workflow.operatorNotInEngine', {
        operator: node.operator,
      }))
      return
    }
    if (!isStandardOperatorMetadata(operator)) {
      ElMessage.error(t('develop.workflow.loadOperatorFailed'))
      return
    }

    selectedNode.value = {
      id: node.id,
      operator: node.operator,
      params: node.params,
      publicParameters: operator.public_parameters
    }

    console.log('[WorkflowEditor] 加载节点参数:', node.id, node.params)
  } catch (error) {
    console.error('加载算子详情失败:', error)
    ElMessage.error(t('develop.workflow.loadOperatorFailed'))
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
    canvasRef.value.updateNodeParams(data.nodeId, data.params, selectedNode.value?.publicParameters)
    // 成功消息已经在 OperatorParamsPanel 中显示，这里不需要重复
  }
}

// ========== 引擎选择相关方法 ==========

// 加载工作流引擎列表
const loadWorkflowEngines = async () => {
  try {
    const response = await getWorkflowEngines()
    workflowEngines.value = response.data || response

    if (workflowEngines.value.length === 0) {
      ElMessage.warning(t('develop.workflow.noEngineAvailable'))
    }
  } catch (error) {
    console.error('加载工作流引擎失败:', error)
    ElMessage.error(t('develop.workflow.loadEngineFailed'))
  }
}

// 加载 Spark 通用引擎资源列表
const loadSparkRuntimes = async () => {
  try {
    const response = await getSparkRuntimes()
    sparkRuntimes.value = response.data || response
  } catch (error) {
    console.error('加载 Spark 通用引擎资源失败:', error)
    ElMessage.error(t('develop.workflow.loadSparkRuntimeFailed'))
  }
}

// 选择默认工作流引擎
const selectDefaultEngine = () => {
  if (workflowEngines.value.length === 0) return

  workflowEngineId.value = workflowEngines.value[0].id
  selectedEngine.value = workflowEngines.value[0]
}

// 引擎切换处理
const handleEngineChange = async (engineId) => {
  selectedEngine.value = workflowEngines.value.find(e => e.id === engineId)

  // 如果切换到 Spark 工作流引擎，加载 Spark 通用引擎资源列表
  if (needsSparkRuntime()) {
    await loadSparkRuntimes()

    // 自动选择第一个 Spark 通用引擎资源
    if (sparkRuntimes.value.length > 0) {
      sparkRuntimeId.value = sparkRuntimes.value[0].id
    }
  } else {
    // 切换到不需要 Spark 绑定的工作流运行时，清空 Spark 通用引擎资源选择
    sparkRuntimeId.value = null
  }
}

// 判断是否需要选择 Spark 通用引擎资源
const needsSparkRuntime = () => {
  return isSparkWorkflowEngine(selectedEngine.value)
}

// 获取引擎标签
const getEngineTag = (engine) => {
  return engine?.engine_type || '-'
}

// 格式化运行时标签
const formatRuntimeLabel = (runtime) => {
  // 根据资源类型显示不同的连接信息
  let connInfo = t('develop.workflow.notConfigured')

  if (runtime.connection_info) {
    if (runtime.engine_type === 'spark') {
      // Apache Spark 数据源：显示 host:port/database
      const { host, port, database } = runtime.connection_info
      if (host && port) {
        connInfo = `${host}:${port}${database ? '/' + database : ''}`
      }
    } else if (runtime.connection_info.spark_master) {
      // Spark 通用引擎资源：显示 spark_master
      connInfo = runtime.connection_info.spark_master
    }
  }

  return `${runtime.name} (${connInfo})`
}

// 是否可以保存
const canSave = () => {
  // 必须选择工作流引擎
  if (!workflowEngineId.value) return false

  // 如果是 Spark 工作流引擎，必须选择运行时
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
      ElMessage.warning(t('develop.workflow.selectEngineFirst'))
    } else if (needsSparkRuntime() && !sparkRuntimeId.value) {
      ElMessage.warning(t('develop.workflow.selectSparkRuntimeFirst'))
    } else if (!hasValidWorkflow.value) {
      ElMessage.warning(t('develop.workflow.emptyWorkflow'))
    }
    return
  }
  saveDialogVisible.value = true
}

const confirmSave = async () => {
  if (!saveForm.name) {
    ElMessage.warning(t('develop.workflow.workflowNameRequired'))
    return
  }

  try {
    const workflow = canvasRef.value?.getWorkflow()

    const requiresSparkRuntime = needsSparkRuntime()
    const taskData = {
      name: saveForm.name,
      displayName: saveForm.display_name,
      description: saveForm.description,
      workflow,
      inputs: {},
      workflowEngineId: workflowEngineId.value,
      sparkRuntimeId: sparkRuntimeId.value,
      requiresSparkRuntime
    }

    console.log('[WorkflowEditor] 执行配置:', {
      workflowEngineId: workflowEngineId.value,
      selectedEngine: selectedEngine.value,
      requiresSparkRuntime
    })

    if (currentTaskId.value) {
      await updateWorkflowTask(currentTaskId.value, taskData)
    } else {
      const task = await saveWorkflowTask(taskData)
      currentTaskId.value = task.id
      currentTaskName.value = task.name
      currentTask.value = task
    }

    ElMessage.success(t('develop.workflow.saveSuccess'))
    saveDialogVisible.value = false

    // 重置表单
    Object.assign(saveForm, {
      name: '',
      display_name: '',
      description: ''
    })
  } catch (error) {
    console.error('保存工作流失败:', error)
    ElMessage.error(t('develop.workflow.saveFailed') + (error.response?.data?.error || error.message))
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
      ElMessage.warning(t('develop.workflow.selectEngineFirst'))
    } else if (needsSparkRuntime() && !sparkRuntimeId.value) {
      ElMessage.warning(t('develop.workflow.selectSparkRuntimeFirst'))
    } else if (!hasValidWorkflow.value) {
      ElMessage.warning(t('develop.workflow.emptyWorkflowExecute'))
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
      ElMessage.warning(t('develop.workflow.invalidJson'))
      return
    }

    const workflow = canvasRef.value?.getWorkflow()
    const requiresSparkRuntime = needsSparkRuntime()
    const taskData = {
      name: `${t('develop.workflow.tempWorkflowPrefix')}_${Date.now()}`,
      workflow,
      inputs,
      workflowEngineId: workflowEngineId.value,
      sparkRuntimeId: sparkRuntimeId.value,
      requiresSparkRuntime
    }

    console.log('[WorkflowEditor] 执行配置:', {
      workflowEngineId: workflowEngineId.value,
      selectedEngine: selectedEngine.value,
      requiresSparkRuntime
    })

    // 创建临时任务并执行
    const tempTask = await createTemporaryWorkflowTask(taskData)

    const execution = await executeWorkflowTask(tempTask.id, inputs)

    ElMessage.success(t('develop.workflow.executeSubmitted'))
    executeDialogVisible.value = false

    await openMonitorExecution(execution.execution_id)
  } catch (error) {
    console.error('执行工作流失败:', error)
    ElMessage.error(t('develop.workflow.executeFailed') + (error.response?.data?.error || error.message))
  } finally {
    executing.value = false
  }
}

// 清空画布
const handleClear = async () => {
  try {
    await ElMessageBox.confirm(t('develop.workflow.clearConfirmMsg'), t('develop.workflow.clearConfirmTitle'), {
      type: 'warning'
    })

    if (canvasRef.value) {
      canvasRef.value.clearGraph()
    }

    workflowData.value = { tasks: [] }
    selectedNode.value = null
    ElMessage.success(t('develop.workflow.clearSuccess'))
  } catch (error) {
    // 用户取消
  }
}

// 查看 JSON
const handleViewJSON = () => {
  if (!hasValidWorkflow.value) {
    ElMessage.warning(t('develop.workflow.emptyWorkflow'))
    return
  }

  const workflow = canvasRef.value?.getWorkflow()
  const exportData = buildWorkflowExportPayload({
    workflow,
    workflowEngineId: workflowEngineId.value,
    sparkRuntimeId: sparkRuntimeId.value,
    requiresSparkRuntime: needsSparkRuntime()
  })

  workflowJSON.value = JSON.stringify(exportData, null, 2)
  jsonDialogVisible.value = true
}

// 复制 JSON 到剪贴板
const copyJSON = async () => {
  try {
    await navigator.clipboard.writeText(workflowJSON.value)
    ElMessage.success(t('develop.workflow.copiedToClipboard'))
  } catch (error) {
    ElMessage.error(t('develop.workflow.copyFailed'))
  }
}

// 导出工作流
const handleExport = () => {
  if (!hasValidWorkflow.value) {
    ElMessage.warning(t('develop.workflow.emptyWorkflowExport'))
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

    ElMessage.success(t('develop.workflow.exportSuccess'))
  } catch (error) {
    console.error('导出工作流失败:', error)
    ElMessage.error(t('develop.workflow.exportFailed') + error.message)
  }
}

// 导入工作流
const handleImport = () => {
  fileInputRef.value?.click()
}

// AI 工作流生成
const generateWorkflow = async () => {
  if (!aiQuery.value.trim()) {
    ElMessage.warning(t('develop.workflow.describeWorkflow'))
    return
  }

  // 检查是否选择了工作流引擎
  if (!workflowEngineId.value) {
    ElMessage.warning(t('develop.workflow.selectEngineFirst'))
    return
  }

  generating.value = true
  try {
    const currentWorkflow = canvasRef.value?.getWorkflow()
    const resources = (currentWorkflow?.tasks || [])
      .filter(task => task.operator === 'load' && task.params?.locator)
      .map(task => ({ role: task.id, locator: task.params.locator }))
    const result = await generateWorkflowFromNL({
      query: aiQuery.value,
      workflow_engine_id: workflowEngineId.value,
      resources
    })
    const resolved = resolveWorkflowGenerationResult(result)
    if (resolved.clarificationKey) {
      ElMessage.warning({
        message: t(resolved.clarificationKey),
        duration: 5000,
        showClose: true
      })
      return
    }

    // 直接加载到画布
    workflowData.value = resolved.workflow
    aiDialogOpen.value = false
    ElMessage.success(t('develop.workflow.generateSuccess', { count: resolved.workflow.tasks.length }))
  } catch (error) {
    console.error('工作流生成失败:', error)

    // 提取后端返回的错误消息
    let errorMsg = t('develop.workflow.generateFailed')
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
    ElMessage.success(t('develop.workflow.loadedToCanvas'))
  }
}

const handleFileChange = async (event) => {
  const file = event.target.files[0]
  if (!file) return

  try {
    const text = await file.text()
    const workflow = JSON.parse(text)

    // 验证格式
    if (!isStandardWorkflowDefinition(workflow)) {
      throw new Error(t('develop.workflow.invalidWorkflowFormat'))
    }

    workflowData.value = workflow
    ElMessage.success(t('develop.workflow.importSuccess'))
  } catch (error) {
    console.error('导入工作流失败:', error)
    ElMessage.error(t('develop.workflow.importFailed') + error.message)
  } finally {
    // 清空文件输入
    event.target.value = ''
  }
}

// 加载已有任务
const loadTask = async (taskId) => {
  try {
    const task = await getDevTask(taskId)

    // 设置当前任务信息
    currentTaskId.value = task.id
    currentTaskName.value = task.name
    currentTask.value = task
    Object.assign(saveForm, {
      name: task.name || '',
      display_name: task.display_name || '',
      description: task.description || ''
    })

    // 解析执行配置
    if (task.execution_config) {
      try {
        if (typeof task.execution_config !== 'object') {
          throw new Error('execution_config must be an object')
        }
        const config = task.execution_config

        // 恢复工作流引擎选择
        workflowEngineId.value = config.engine_id
        selectedEngine.value = workflowEngines.value.find(
          e => e.id === config.engine_id
        )

        // 如果需要 Spark 通用引擎资源，加载列表并恢复选择
        if (needsSparkRuntime() && config.engine_specific) {
          await loadSparkRuntimes()
          sparkRuntimeId.value = config.engine_specific.spark_cluster_id
        }
      } catch (error) {
        console.error('解析执行配置失败:', error)
        ElMessage.warning(t('develop.workflow.corruptedConfig'))
      }
    }

    // 加载工作流内容
    if (task.content) {
      if (task.content.workflow_definition) {
        workflowData.value = task.content.workflow_definition
      } else {
        ElMessage.warning(t('develop.workflow.noWorkflowContent'))
        return
      }
      ElMessage.success(t('develop.workflow.workflowLoaded', { name: task.name }))
    } else {
      ElMessage.warning(t('develop.workflow.noWorkflowContent'))
    }
  } catch (error) {
    console.error('加载任务失败:', error)
    ElMessage.error(t('develop.workflow.loadTaskFailed') + (error.response?.data?.error || error.message))
  }
}

// 组件挂载时检查是否有任务 ID
onMounted(async () => {
  // 加载工作流引擎列表
  await loadWorkflowEngines()

  const taskId = firstQueryValue(route.query.id || route.query.taskId)
  if (taskId) {
    // 如果是编辑模式，加载开发任务
    await loadTask(taskId)
  } else {
    // 新建模式：选择默认引擎
    selectDefaultEngine()
  }
})

function firstQueryValue(value) {
  if (Array.isArray(value)) {
    return value[0] || ''
  }
  return value || ''
}
</script>

<style scoped>
.workflow-editor-page {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--addp-bg-secondary);
}

/* AI 助手顶部面板已移除 */

.ai-fab-wrapper {
  position: fixed;
  bottom: 28px;
  right: 28px;
  display: flex;
  align-items: flex-end;
  gap: 10px;
  z-index: 1000;
}

.ai-fab {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: var(--el-color-primary);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
  flex-shrink: 0;
  transition: background 0.2s, box-shadow 0.2s, transform 0.2s;
}

.ai-fab:hover {
  background: var(--el-color-primary-dark-2);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.3);
}

.ai-fab--active {
  background: var(--el-color-primary-dark-2);
  transform: rotate(15deg);
}

.ai-inline-panel {
  width: 320px;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.25);
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  transform-origin: right bottom;
}

.ai-panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ai-panel-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.ai-panel-close {
  cursor: pointer;
  color: var(--addp-text-secondary);
  font-size: 14px;
  transition: color 0.15s;
}

.ai-panel-close:hover {
  color: var(--addp-text-primary);
}

.ai-panel-input :deep(.el-textarea__inner) {
  font-size: 13px;
  resize: none;
}

.ai-panel-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ai-panel-hint {
  font-size: 11px;
  color: var(--addp-text-secondary);
}

/* 滑动动画 */
.ai-slide-enter-active {
  transition: all 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.ai-slide-leave-active {
  transition: all 0.18s ease-in;
}

.ai-slide-enter-from {
  opacity: 0;
  transform: translateX(20px) scale(0.95);
}

.ai-slide-leave-to {
  opacity: 0;
  transform: translateX(20px) scale(0.95);
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  padding: 12px 20px;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
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
  color: var(--addp-text-primary);
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
  color: var(--addp-text-secondary);
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
  background: var(--addp-bg-primary);
  border-right: 1px solid var(--addp-border-color);
  flex-shrink: 0;
  position: relative;
  z-index: 10;
}

.right-panel {
  border-right: none;
  border-left: 1px solid var(--addp-border-color);
  position: relative;
  z-index: 10;
}

.canvas-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--addp-bg-secondary);
  position: relative;
  z-index: 1;
}

.panel-header {
  padding: 12px 16px;
  background: var(--addp-bg-secondary);
  border-bottom: 1px solid var(--addp-border-color);
  flex-shrink: 0;
}

.panel-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
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
  background-color: var(--addp-bg-secondary);
  border-radius: 4px;
  padding: 12px;
}

.json-viewer pre {
  margin: 0;
  font-family: 'Monaco', 'Menlo', 'Consolas', monospace;
  font-size: 13px;
  line-height: 1.5;
  color: var(--addp-text-primary);
}
</style>
