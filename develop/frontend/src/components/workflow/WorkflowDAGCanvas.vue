<template>
  <div class="workflow-dag-canvas">
    <div class="toolbar">
      <div class="toolbar-left">
        <el-button
          :type="isAddEdgeMode ? 'primary' : 'default'"
          size="small"
          @click="handleToggleEdgeMode"
        >
          <el-icon><Connection /></el-icon> {{ isAddEdgeMode ? t('develop.workflowCanvas.exitEdgeMode') : t('develop.workflowCanvas.edgeMode') }}
        </el-button>

        <el-divider direction="vertical" />

        <el-button type="danger" size="small" @click="handleDelete" :disabled="!selectedItem">
          <el-icon><Delete /></el-icon> {{ selectedItem ? (selectedItem.getType && selectedItem.getType() === 'edge' ? t('develop.workflowCanvas.deleteEdge') : t('develop.workflowCanvas.deleteNode')) : t('develop.workflowCanvas.delete') }}
        </el-button>
        <el-button type="info" size="small" @click="handleClear">
          <el-icon><DocumentDelete /></el-icon> {{ t('develop.workflowCanvas.clear') }}
        </el-button>
      </div>

      <div class="toolbar-tips">
        <el-alert type="info" :closable="false" show-icon>
          <template #title>
            <span class="tips-text">
              {{ isAddEdgeMode
                ? t('develop.workflowCanvas.tipEdgeMode')
                : t('develop.workflowCanvas.tipNormal')
              }}
            </span>
          </template>
        </el-alert>
      </div>
    </div>

    <div id="workflow-dag-container" ref="container" @dragover.prevent @drop="handleDrop"></div>
  </div>
</template>

<script setup>
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Delete, DocumentDelete, Connection } from '@element-plus/icons-vue'
import { useDAGCore, useLoopDetection, useDAGSelection, useDAGEdgeMode, registerMultiPortNode } from '@addp/common-frontend/dag'
import { isStandardWorkflowDefinition } from '@/utils/workflowDevTaskPayload'
import { applyWorkflowInputRefs } from '@/utils/workflowInputBindings'

const { t } = useI18n()

const props = defineProps({
  initialWorkflow: {
    type: Object,
    default: () => ({ tasks: [] })
  }
})

const emit = defineEmits(['update:workflow', 'node-click'])

const container = ref(null)
const nodeCounter = ref(0)
const edgeSourceNode = ref(null)
const edgeSourcePort = ref(null)

const ARROW_PATH = 'M 0,0 L 12,5 L 12,-5 Z'

const { graph, initGraph } = useDAGCore(container, {
  modes: {
    default: ['drag-canvas', 'drag-node', 'click-select'],
    addEdge: ['drag-canvas']
  },
  defaultNode: {
    type: 'workflow-node',
    size: [140, 60]
  },
  defaultEdge: {
    type: 'polyline',
    style: {
      stroke: '#A3B1BF',
      lineWidth: 2,
      radius: 10,
      endArrow: { path: ARROW_PATH, fill: '#A3B1BF', d: 0 }
    },
    labelCfg: { autoRotate: true }
  }
})

const { hasLoop } = useLoopDetection(graph)
const { selectedItem, initSelectionListener, deleteSelected, clearGraph } = useDAGSelection(graph)
const { isAddEdgeMode, toggleAddEdgeMode } = useDAGEdgeMode(graph)

onMounted(() => {
  setTimeout(() => {
    registerMultiPortNode()
    initGraph()
    if (!graph.value) {
      console.error('[WorkflowDAGCanvas] initGraph 失败，容器可能尚未挂载')
      return
    }
    initSelectionListener()
    graph.value.on('node:click', handleNodeClick)
    graph.value.on('node:mouseenter', (e) => {
      const shape = e.shape
      if (shape && shape.cfg && shape.cfg.portType && shape.cfg.portName !== 'input') {
        console.log(`输出端口: ${shape.cfg.portName} - ${shape.cfg.portDescription || ''}`)
      }
    })
    graph.value.on('afterremoveitem', () => emitWorkflow())
    graph.value.on('afterupdateitem', () => emitWorkflow())
    if (props.initialWorkflow?.tasks?.length > 0) {
      loadWorkflow(props.initialWorkflow)
    }
  }, 200)
})

watch(() => props.initialWorkflow, (newWorkflow) => {
  if (newWorkflow?.tasks) {
    loadWorkflow(newWorkflow)
  }
}, { deep: true })

function handleNodeClick(e) {
  if (!isAddEdgeMode.value) {
    emitNodeSelection(e)
    return
  }

  const clickedNode = e.item
  const model = clickedNode.getModel()
  const shape = e.shape

  let clickedPort = null
  let portType = null

  if (shape && shape.cfg) {
    if (shape.cfg.portType) {
      clickedPort = shape.cfg.portName
      portType = shape.cfg.portType
    }
  }

  if (!edgeSourceNode.value) {
    if (portType === 'output') {
      edgeSourceNode.value = model.id
      edgeSourcePort.value = clickedPort
      graph.value.setItemState(clickedNode, 'selected', true)
      ElMessage.info(t('develop.workflowCanvas.selectedOutputPort', { label: model.label, port: clickedPort }))
    } else {
      ElMessage.warning(t('develop.workflowCanvas.clickOutputPortFirst'))
    }
  } else {
    const sourceId = edgeSourceNode.value
    const sourcePort = edgeSourcePort.value
    const targetId = model.id

    const clearSource = () => {
      const sourceNode = graph.value.findById(sourceId)
      if (sourceNode) graph.value.setItemState(sourceNode, 'selected', false)
      edgeSourceNode.value = null
      edgeSourcePort.value = null
    }

    if (sourceId === targetId) {
      ElMessage.warning(t('develop.workflowCanvas.cannotSelfConnect'))
      clearSource()
      return
    }

    if (portType === 'input' || !portType) {
      const edgeExists = graph.value.getEdges().some(edge => {
        const em = edge.getModel()
        return em.source === sourceId && em.target === targetId
      })

      if (edgeExists) {
        ElMessage.warning(t('develop.workflowCanvas.edgeAlreadyExists'))
        clearSource()
        return
      }

      if (hasLoop(sourceId, targetId)) {
        ElMessage.warning(t('develop.workflowCanvas.cannotCreateLoop'))
        clearSource()
        return
      }

      const edgeLabel = sourcePort !== 'default' ? sourcePort : ''
      graph.value.addItem('edge', {
        source: sourceId,
        target: targetId,
        sourcePort,
        targetPort: 'input',
        type: 'polyline',
        label: edgeLabel,
        style: {
          stroke: '#A3B1BF',
          lineWidth: 2,
          radius: 10,
          endArrow: { path: ARROW_PATH, fill: '#A3B1BF', d: 0 }
        }
      })

      const sourceNode = graph.value.findById(sourceId)
      if (sourceNode) {
        ElMessage.success(t('develop.workflowCanvas.connected', { source: `${sourceNode.getModel().label}.${sourcePort}`, target: model.label }))
      }
      clearSource()
      emitWorkflow()
    } else {
      ElMessage.warning(t('develop.workflowCanvas.clickInputPort'))
    }
  }
}

function emitNodeSelection(e) {
  const model = e.item.getModel()
  emit('node-click', {
    id: model.id,
    operator: model.operator,
    params: model.params,
    label: model.label
  })
}

function handleToggleEdgeMode() {
  toggleAddEdgeMode()
  if (isAddEdgeMode.value) {
    ElMessage.info(t('develop.workflowCanvas.enteredEdgeMode'))
  } else {
    // 退出时清除源节点高亮
    if (edgeSourceNode.value) {
      const sourceNode = graph.value.findById(edgeSourceNode.value)
      if (sourceNode) graph.value.setItemState(sourceNode, 'selected', false)
      edgeSourceNode.value = null
      edgeSourcePort.value = null
    }
  }
}

function handleDelete() {
  if (deleteSelected()) {
    ElMessage.success(t('develop.workflowCanvas.deleted'))
    emitWorkflow()
  }
}

function handleClear() {
  clearGraph()
  nodeCounter.value = 0
  emitWorkflow()
  ElMessage.success(t('develop.workflowCanvas.canvasCleared'))
}

function handleDrop(event) {
  event.preventDefault()

  const data = event.dataTransfer.getData('application/json')
  if (!data) return

  let operatorData
  try {
    operatorData = JSON.parse(data)
  } catch (error) {
    console.error('解析拖放数据失败:', error)
    return
  }

  if (operatorData.type !== 'operator') return

  const point = graph.value.getPointByClient(event.clientX, event.clientY)

  nodeCounter.value++
  const nodeId = `${operatorData.name}_${nodeCounter.value}`

  if (!Array.isArray(operatorData.output_ports) || operatorData.output_ports.length === 0) {
    ElMessage.error(t('develop.workflowCanvas.invalidOperatorMetadata', { name: operatorData.name }))
    return
  }
  if (!Array.isArray(operatorData.publicParameters)) {
    ElMessage.error(t('develop.workflowCanvas.invalidOperatorMetadata', { name: operatorData.name }))
    return
  }

  const outputPorts = operatorData.output_ports

  graph.value.addItem('node', {
    id: nodeId,
    label: operatorData.name,
    x: point.x,
    y: point.y,
    operator: operatorData.name,
    params: {},
    depends_on: [],
    publicParameters: operatorData.publicParameters,
    outputPorts
  })

  emitWorkflow()
  ElMessage.success(t('develop.workflowCanvas.operatorAdded', { name: operatorData.name }))
}

function loadWorkflow(workflow) {
  if (!graph.value || !Array.isArray(workflow.tasks)) return

  if (workflow.tasks.length === 0) {
    graph.value.clear()
    nodeCounter.value = 0
    return
  }

  if (!isStandardWorkflowDefinition(workflow)) {
    ElMessage.error(t('develop.workflow.invalidWorkflowFormat'))
    return
  }

  graph.value.clear()
  nodeCounter.value = 0

  const nodes = []
  const edges = []

  workflow.tasks.forEach((task, index) => {
    nodes.push({
      id: task.id,
      label: task.operator,
      x: 100 + (index % 3) * 200,
      y: 100 + Math.floor(index / 3) * 120,
      operator: task.operator,
      params: task.params,
      depends_on: task.depends_on
    })

    const match = task.id.match(/_(\d+)$/)
    if (match) {
      const num = parseInt(match[1])
      if (num > nodeCounter.value) nodeCounter.value = num
    }
  })

  workflow.tasks.forEach(task => {
    if (task.depends_on.length > 0) {
      task.depends_on.forEach(sourceId => {
        const ref = findWorkflowRefForSource(task.params, sourceId)
        const sourcePort = ref?.port || 'default'
        edges.push({
          source: sourceId,
          target: task.id,
          sourcePort,
          targetPort: 'input',
          type: 'polyline',
          label: sourcePort !== 'default' ? sourcePort : '',
          style: {
            stroke: '#A3B1BF',
            lineWidth: 2,
            radius: 10,
            endArrow: { path: ARROW_PATH, fill: '#A3B1BF', d: 0 }
          }
        })
      })
    }
  })

  graph.value.data({ nodes, edges })
  graph.value.render()
}

function findWorkflowRefForSource(params, sourceId) {
  if (!params || typeof params !== 'object') return null
  for (const value of Object.values(params)) {
    if (value && typeof value === 'object' && !Array.isArray(value) && value.$ref === sourceId) {
      return value
    }
  }
  return null
}

function buildWorkflowFromGraph() {
  if (!graph.value) return

  const nodes = graph.value.getNodes()
  const edges = graph.value.getEdges()

  // 构建端口引用映射
  const edgePortMap = {}
  edges.forEach(edge => {
    const model = edge.getModel()
    const { source: sourceId, target: targetId, sourcePort = 'default', targetPort = 'input' } = model
    const sourceNode = graph.value.findById(sourceId)
    const sourceModel = sourceNode?.getModel?.() || {}
    const sourceOutput = (sourceModel.outputPorts || []).find(port => port.name === sourcePort)

    if (!edgePortMap[targetId]) edgePortMap[targetId] = []
    edgePortMap[targetId].push({
      sourceId,
      sourcePort,
      sourceType: sourceOutput?.type,
      targetPort
    })
  })

  const tasks = nodes.map(node => {
    const model = node.getModel()
    const inEdges = edgePortMap[model.id] || []
    let params
    if (!Array.isArray(model.publicParameters) && inEdges.length > 0) {
      if (!paramsContainRefsForEdges(model.params, inEdges)) {
        throw new Error(`operator ${model.operator} is missing parameter metadata`)
      }
      params = { ...model.params }
    } else {
      params = applyWorkflowInputRefs({
        params: model.params,
        parameters: model.publicParameters,
        inputEdges: inEdges
      })
    }

    return {
      id: model.id,
      operator: model.operator,
      params,
      depends_on: inEdges.map(e => e.sourceId)
    }
  })

  return { tasks }
}

function emitWorkflow() {
  if (!graph.value) return

  try {
    emit('update:workflow', buildWorkflowFromGraph())
  } catch (error) {
    ElMessage.error(t('develop.workflowCanvas.inputBindingFailed') + error.message)
  }
}

function paramsContainRefsForEdges(params, inEdges) {
  return inEdges.every(edge => {
    const ref = findWorkflowRefForSource(params, edge.sourceId)
    if (!ref) return false
    return (ref.port || 'default') === (edge.sourcePort || 'default')
  })
}

function updateNodeParams(nodeId, params, publicParameters = null) {
  const node = graph.value.findById(nodeId)
  if (node) {
    const nextModel = { ...node.getModel(), params: { ...params } }
    if (Array.isArray(publicParameters)) {
      nextModel.publicParameters = publicParameters
    }
    graph.value.updateItem(node, nextModel)
    emitWorkflow()
  }
}

defineExpose({
  updateNodeParams,
  clearGraph: handleClear,
  getWorkflow: () => {
    try {
      return buildWorkflowFromGraph()
    } catch (error) {
      ElMessage.error(t('develop.workflowCanvas.inputBindingFailed') + error.message)
      return { tasks: [] }
    }
  }
})
</script>

<style scoped>
.workflow-dag-canvas {
  height: 100%;
  width: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.toolbar {
  padding: 12px 16px;
  background: var(--addp-bg-secondary);
  border-bottom: 1px solid var(--addp-border-color);
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-shrink: 0;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.toolbar-tips {
  flex: 1;
  margin-left: 16px;
}

.tips-text {
  font-size: 13px;
}

#workflow-dag-container {
  flex: 1;
  background: var(--addp-bg-primary);
  overflow: hidden;
  position: relative;
  z-index: 1;
}

:deep(.g6-tooltip) {
  background: rgba(0, 0, 0, 0.75);
  color: #fff;
  padding: 8px 12px;
  border-radius: 4px;
  font-size: 12px;
}
</style>
