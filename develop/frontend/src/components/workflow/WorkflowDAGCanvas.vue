<template>
  <div class="workflow-dag-canvas">
    <div class="canvas-toolbar">
      <div class="canvas-toolbar-group">
        <el-tooltip :content="t('develop.workflowCanvas.undo')">
          <el-button circle size="small" :aria-label="t('develop.workflowCanvas.undo')" :disabled="!canUndo" @click="handleUndo">
            <el-icon><RefreshLeft /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.workflowCanvas.redo')">
          <el-button circle size="small" :aria-label="t('develop.workflowCanvas.redo')" :disabled="!canRedo" @click="handleRedo">
            <el-icon><RefreshRight /></el-icon>
          </el-button>
        </el-tooltip>
        <el-divider direction="vertical" />
        <el-tooltip :content="t('develop.workflowCanvas.copyNode')">
          <el-button circle size="small" :aria-label="t('develop.workflowCanvas.copyNode')" :disabled="!canCopyNode" @click="handleCopy">
            <el-icon><CopyDocument /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.workflowCanvas.pasteNode')">
          <el-button circle size="small" :aria-label="t('develop.workflowCanvas.pasteNode')" :disabled="!copiedNode" @click="handlePaste">
            <el-icon><DocumentAdd /></el-icon>
          </el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.workflowCanvas.duplicateNode')">
          <el-button circle size="small" :aria-label="t('develop.workflowCanvas.duplicateNode')" :disabled="!canCopyNode" @click="handleDuplicate">
            <el-icon><Plus /></el-icon>
          </el-button>
        </el-tooltip>
        <el-divider direction="vertical" />
        <el-tooltip :content="t('develop.workflowCanvas.deleteSelected')">
          <el-button circle size="small" type="danger" plain :aria-label="t('develop.workflowCanvas.deleteSelected')" :disabled="!selectedItem" @click="handleDelete">
            <el-icon><Delete /></el-icon>
          </el-button>
        </el-tooltip>
        <el-divider direction="vertical" />
        <el-tooltip :content="t('develop.workflowCanvas.zoomOut')">
          <el-button circle size="small" :aria-label="t('develop.workflowCanvas.zoomOut')" :disabled="!canZoomOut" @click="handleZoomOut"><el-icon><ZoomOut /></el-icon></el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.workflowCanvas.zoomIn')">
          <el-button circle size="small" :aria-label="t('develop.workflowCanvas.zoomIn')" :disabled="!canZoomIn" @click="handleZoomIn"><el-icon><ZoomIn /></el-icon></el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.workflowCanvas.fitView')">
          <el-button circle size="small" :aria-label="t('develop.workflowCanvas.fitView')" @click="handleFitView"><el-icon><FullScreen /></el-icon></el-button>
        </el-tooltip>
        <el-tooltip :content="t('develop.workflowCanvas.autoLayout')">
          <el-button circle size="small" :aria-label="t('develop.workflowCanvas.autoLayout')" @click="handleAutoLayout"><el-icon><Rank /></el-icon></el-button>
        </el-tooltip>
      </div>
      <span class="zoom-value">{{ Math.round(zoom * 100) }}%</span>
    </div>

    <div
      id="workflow-dag-container"
      ref="container"
      tabindex="0"
      @dragover.prevent
      @drop="handleDrop"
      @keydown="handleKeydown"
    ></div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import {
  CopyDocument,
  Delete,
  DocumentAdd,
  FullScreen,
  Plus,
  Rank,
  RefreshLeft,
  RefreshRight,
  ZoomIn,
  ZoomOut
} from '@element-plus/icons-vue'
import {
  createDAGDirectEdgeBehavior,
  createDAGDragNodeBehavior,
  isDAGPortEvent,
  useDAGClipboard,
  useDAGCore,
  useDAGHistory,
  useDAGLayout,
  useDAGSelection,
  useDAGViewport,
  useLoopDetection
} from '@addp/common-frontend/dag'
import { isStandardWorkflowDefinition } from '@/utils/workflowDevTaskPayload'
import {
  applyWorkflowInputRefs,
  areWorkflowTypesCompatible,
  isWorkflowInputParameter
} from '@/utils/workflowInputBindings'
import {
  isTargetResourceBinding,
  missingResourceBindingParams,
  resourceBindingNameParam,
  resourceBindingTargetExtension
} from '@/utils/workflowResourceBindings'
import {
  operatorInputPorts,
  operatorOutputPorts,
  registerWorkflowEditorNode
} from './workflowEditorNode'

const { t } = useI18n()

const props = defineProps({
  initialWorkflow: {
    type: Object,
    default: () => ({ tasks: [] })
  },
  operators: {
    type: Array,
    default: () => []
  },
  validationIssues: {
    type: Array,
    default: () => []
  },
  initialLayout: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['update:workflow', 'update:layout', 'node-click'])
const container = ref(null)
const nodeCounter = ref(0)
const lastEmittedSignature = ref('')
const edgeStyle = resolveEdgeStyle()

const { graph, initGraph } = useDAGCore(container, {
  modes: {
    default: [
      'drag-canvas',
      'zoom-canvas',
      createDAGDragNodeBehavior(),
      'click-select',
      createDAGDirectEdgeBehavior({
        resolveSource: resolveOutputPort,
        resolveTarget: resolveInputPort,
        canConnect: canConnectPorts,
        buildEdgeConfig,
        onRejected: handleConnectionRejected
      })
    ]
  },
  defaultNode: {
    type: 'develop-workflow-node'
  },
  defaultEdge: {
    type: 'polyline',
    style: edgeStyle
  },
  nodeStateStyles: {
    selected: {
      stroke: cssColor('--el-color-primary'),
      lineWidth: 2.5
    },
    invalid: {
      stroke: cssColor('--el-color-danger'),
      lineWidth: 2.5
    }
  },
  edgeStateStyles: {
    selected: {
      stroke: cssColor('--el-color-primary'),
      lineWidth: 2.5
    }
  }
})

const { hasLoop } = useLoopDetection(graph)
const { selectedItem, initSelectionListener, deleteSelected } = useDAGSelection(graph)
const {
  zoom,
  canZoomIn,
  canZoomOut,
  zoomIn,
  zoomOut,
  fitView,
  autoLayout,
  syncZoom
} = useDAGViewport(graph)
const { captureLayout, applyNodePositions, restoreViewport } = useDAGLayout(graph)
const { copiedNode, copy, paste } = useDAGClipboard(graph, { createNodeId: createCopiedNodeId })
const {
  canUndo,
  canRedo,
  reset: resetHistory,
  record: recordHistory,
  undo,
  redo
} = useDAGHistory({
  capture: () => graph.value?.save?.() || { nodes: [], edges: [] },
  restore: restoreGraphSnapshot
})
const canCopyNode = computed(() => selectedItem.value?.getType?.() === 'node')

onMounted(() => {
  registerWorkflowEditorNode()
  initGraph()
  if (!graph.value) return

  initSelectionListener()
  graph.value.on('node:click', handleNodeClick)
  graph.value.on('canvas:click', clearNodeSelection)
  graph.value.on('edge:click', () => emit('node-click', null))
  graph.value.on('aftercreateedge', () => {
    recordHistory()
    emitWorkflow()
    emitLayout()
    ElMessage.success(t('develop.workflowCanvas.connected'))
  })
  graph.value.on('node:dragend', () => {
    recordHistory()
    emitLayout()
  })
  graph.value.on('canvas:dragend', emitLayout)
  graph.value.on('wheelzoom', () => {
    syncZoom()
    emitLayout()
  })

  loadWorkflow(props.initialWorkflow)
})

watch(() => props.initialWorkflow, newWorkflow => {
  if (!graph.value || !newWorkflow?.tasks) return
  const signature = workflowSignature(newWorkflow)
  if (signature !== lastEmittedSignature.value) {
    loadWorkflow(newWorkflow)
  }
}, { deep: true })

watch(() => props.operators, () => {
  if (graph.value && props.initialWorkflow?.tasks?.length > 0) {
    loadWorkflow(props.initialWorkflow)
  }
}, { deep: true })

watch(() => props.validationIssues, applyValidationStates, { deep: true })

function resolveOutputPort(event) {
  if (event?.shape?.cfg?.portType !== 'output') return null
  return {
    name: event.shape.cfg.portName,
    type: event.shape.cfg.portDataType
  }
}

function resolveInputPort(event) {
  if (event?.shape?.cfg?.portType !== 'input') return null
  return {
    name: event.shape.cfg.portName,
    type: event.shape.cfg.portDataType
  }
}

function canConnectPorts({ sourceId, targetId, sourcePort, targetPort }) {
  if (!sourceId || !targetId || sourceId === targetId || hasLoop(sourceId, targetId)) {
    return 'loop'
  }
  if (!areWorkflowTypesCompatible(sourcePort?.type, targetPort?.type)) {
    return 'type_mismatch'
  }

  const duplicated = graph.value.getEdges().some(edge => {
    const model = edge.getModel()
    return model.source === sourceId && model.target === targetId
  })
  if (duplicated) return 'duplicate'

  const occupied = graph.value.getEdges().some(edge => {
    const model = edge.getModel()
    return model.target === targetId && model.targetParam === targetPort.name
  })
  return occupied ? 'target_occupied' : true
}

function buildEdgeConfig({ sourceItem, targetItem, sourcePort, targetPort }) {
  const sourceModel = sourceItem?.getModel?.() || {}
  const targetModel = targetItem?.getModel?.() || {}
  const sourceIndex = (sourceModel.outputPorts || []).findIndex(port => port.name === sourcePort?.name)
  const targetIndex = (targetModel.inputPorts || []).findIndex(port => port.name === targetPort?.name)
  return {
    sourcePort: sourcePort?.name || 'default',
    targetParam: targetPort?.name,
    sourceAnchor: sourceIndex >= 0 ? (sourceModel.inputPorts?.length || 0) + sourceIndex : undefined,
    targetAnchor: targetIndex >= 0 ? targetIndex : undefined,
    type: 'polyline',
    label: sourcePort?.name && sourcePort.name !== 'default' ? sourcePort.name : '',
    style: edgeStyle
  }
}

function handleConnectionRejected({ reason }) {
  const messageKey = {
    loop: 'cannotCreateLoop',
    duplicate: 'edgeAlreadyExists',
    type_mismatch: 'portTypeMismatch',
    target_occupied: 'inputAlreadyConnected'
  }[reason]
  if (messageKey) ElMessage.warning(t(`develop.workflowCanvas.${messageKey}`))
}

function handleNodeClick(event) {
  if (isDAGPortEvent(event)) return
  const model = event.item.getModel()
  emit('node-click', {
    id: model.id,
    operator: model.operator,
    params: model.params,
    label: model.label,
    displayName: model.displayName,
    publicParameters: model.publicParameters
  })
}

function clearNodeSelection() {
  emit('node-click', null)
}

function handleDelete() {
  const selected = selectedItem.value
  if (!selected) return
  const type = selected.getType?.()
  if (deleteSelected()) {
    if (type === 'node') emit('node-click', null)
    recordHistory()
    emitWorkflow()
    emitLayout()
    ElMessage.success(t('develop.workflowCanvas.deleted'))
  }
}

function handleDrop(event) {
  event.preventDefault()
  const raw = event.dataTransfer.getData('application/json')
  if (!raw) return

  try {
    const operator = JSON.parse(raw)
    if (operator.type !== 'operator') return
    const point = graph.value.getPointByClient(event.clientX, event.clientY)
    addOperator(operator, point)
  } catch (error) {
    ElMessage.error(t('develop.workflowCanvas.invalidOperatorMetadata', { name: '-' }))
  }
}

function addOperator(operator, point = null) {
  if (!graph.value || !Array.isArray(operator.output_ports || operator.outputPorts)) {
    ElMessage.error(t('develop.workflowCanvas.invalidOperatorMetadata', { name: operator.name || '-' }))
    return
  }

  nodeCounter.value += 1
  const position = point || viewportCenterPoint()
  const metadata = normalizeOperator(operator)
  graph.value.addItem('node', buildNodeModel(metadata, `${metadata.name}_${nodeCounter.value}`, position))
  container.value?.focus()
  recordHistory()
  emitWorkflow()
  emitLayout()
  ElMessage.success(t('develop.workflowCanvas.operatorAdded', { name: metadata.display_name || metadata.name }))
}

function viewportCenterPoint() {
  const rect = container.value.getBoundingClientRect()
  return graph.value.getPointByClient(rect.left + rect.width / 2, rect.top + rect.height / 2)
}

function loadWorkflow(workflow) {
  if (!graph.value || !Array.isArray(workflow?.tasks)) return
  graph.value.clear()
  nodeCounter.value = 0

  if (workflow.tasks.length === 0) {
    lastEmittedSignature.value = workflowSignature(workflow)
    resetHistory({ nodes: [], edges: [] })
    emitLayout()
    return
  }
  if (!isStandardWorkflowDefinition(workflow)) {
    ElMessage.error(t('develop.workflow.invalidWorkflowFormat'))
    return
  }

  const nodes = applyNodePositions(workflow.tasks.map(task => {
    const operator = operatorByName(task.operator)
    updateNodeCounter(task.id)
    return buildNodeModel(operator || fallbackOperator(task), task.id, null, task.params)
  }), props.initialLayout)
  const nodeById = new Map(nodes.map(node => [node.id, node]))
  const edges = []

  workflow.tasks.forEach(task => {
    task.depends_on.forEach(sourceId => {
      const binding = findWorkflowBinding(task.params, sourceId)
      const sourceNode = nodeById.get(sourceId)
      const targetNode = nodeById.get(task.id)
      edges.push(buildLoadedEdge(sourceId, task.id, sourceNode, targetNode, binding))
    })
  })

  graph.value.data({ nodes, edges })
  graph.value.render()
  lastEmittedSignature.value = workflowSignature(workflow)
  applyValidationStates()
  if (hasStoredLayout(props.initialLayout)) {
    restoreViewport(props.initialLayout)
    syncZoom()
  } else {
    autoLayout()
  }
  resetHistory(graph.value.save())
  emitLayout()
}

function buildNodeModel(operator, id, point = null, params = null) {
  const inputPorts = operatorInputPorts(operator)
  const outputPorts = operatorOutputPorts(operator)
  return {
    id,
    type: 'develop-workflow-node',
    label: operator.display_name || operator.name,
    displayName: operator.display_name || operator.name,
    operator: operator.name,
    params: params ? { ...params } : defaultOperatorParams(operator),
    publicParameters: operator.public_parameters || [],
    inputPorts,
    outputPorts,
    ...(point ? { x: point.x, y: point.y } : {})
  }
}

function buildLoadedEdge(sourceId, targetId, sourceNode, targetNode, binding) {
  const sourcePort = binding?.ref?.port || 'default'
  const sourceIndex = (sourceNode?.outputPorts || []).findIndex(port => port.name === sourcePort)
  const targetIndex = (targetNode?.inputPorts || []).findIndex(port => port.name === binding?.parameterName)
  return {
    source: sourceId,
    target: targetId,
    sourcePort,
    targetParam: binding?.parameterName,
    sourceAnchor: sourceIndex >= 0 ? (sourceNode?.inputPorts?.length || 0) + sourceIndex : undefined,
    targetAnchor: targetIndex >= 0 ? targetIndex : undefined,
    type: 'polyline',
    label: sourcePort !== 'default' ? sourcePort : '',
    style: edgeStyle
  }
}

function buildWorkflowFromGraph() {
  if (!graph.value) return { tasks: [] }
  const edgesByTarget = new Map()

  graph.value.getEdges().forEach(edge => {
    const model = edge.getModel()
    const sourceModel = graph.value.findById(model.source)?.getModel?.() || {}
    const output = (sourceModel.outputPorts || []).find(port => port.name === (model.sourcePort || 'default'))
    if (!edgesByTarget.has(model.target)) edgesByTarget.set(model.target, [])
    edgesByTarget.get(model.target).push({
      sourceId: model.source,
      sourcePort: model.sourcePort || 'default',
      sourceType: output?.type,
      targetParam: model.targetParam
    })
  })

  const tasks = graph.value.getNodes().map(node => {
    const model = node.getModel()
    const inputEdges = edgesByTarget.get(model.id) || []
    const params = removeStaleInputRefs(model.params, model.publicParameters)
    return {
      id: model.id,
      operator: model.operator,
      params: applyWorkflowInputRefs({
        params,
        parameters: model.publicParameters,
        inputEdges
      }),
      depends_on: [...new Set(inputEdges.map(edge => edge.sourceId))]
    }
  })
  return { tasks }
}

function getClientValidationIssues() {
  const workflow = buildWorkflowFromGraph()
  const taskById = new Map(workflow.tasks.map(task => [task.id, task]))
  const issues = []

  graph.value.getNodes().forEach((node, index) => {
    const model = node.getModel()
    const task = taskById.get(model.id)
    const parameters = visibleParameters(model.publicParameters || [], task?.params || {})

    parameters.forEach(parameter => {
      if (parameter.param_type === 'ui' || parameter.type === 'ui' || parameter.param_type === 'resource') return
      if (parameter.required && isMissingValue(task?.params?.[parameter.name])) {
        issues.push(clientValidationIssue(index, parameter.name, 'required_parameter_missing'))
      }
    })

    const resourcePickers = parameters.filter(parameter => parameter.ui_type === 'resource_tree_picker')
    missingResourceBindingParams(resourcePickers, task?.params || {}).forEach(name => {
      issues.push(clientValidationIssue(index, name, 'required_resource_missing'))
    })
    resourcePickers.filter(isTargetResourceBinding).forEach(parameter => {
      const extension = resourceBindingTargetExtension(parameter)
      const name = String(task?.params?.[resourceBindingNameParam(parameter)] || '')
      if (extension && name && !name.toLowerCase().endsWith(extension.toLowerCase())) {
        issues.push({
          code: 'target_extension_invalid',
          path: `workflow_definition.tasks[${index}].params.${resourceBindingNameParam(parameter)}`,
          message: t('develop.operatorParams.targetExtensionRequired', { extension })
        })
      }
    })
  })
  return issues
}

function visibleParameters(parameters, params) {
  return parameters.filter(parameter => Object.entries(parameter.show_when || {}).every(([name, expected]) => {
    const current = params[name]
    return Array.isArray(expected) ? expected.includes(current) : current === expected
  }))
}

function clientValidationIssue(taskIndex, parameterName, code) {
  return {
    code,
    path: `workflow_definition.tasks[${taskIndex}].params.${parameterName}`,
    message: t('develop.operatorParams.requiredParam', { name: parameterName })
  }
}

function isMissingValue(value) {
  return value === undefined || value === null || value === ''
}

function removeStaleInputRefs(params = {}, parameters = []) {
  const next = { ...params }
  parameters.filter(isWorkflowInputParameter).forEach(parameter => {
    if (isWorkflowReference(next[parameter.name])) delete next[parameter.name]
  })
  return next
}

function emitWorkflow() {
  try {
    const workflow = buildWorkflowFromGraph()
    lastEmittedSignature.value = workflowSignature(workflow)
    emit('update:workflow', workflow)
  } catch (error) {
    ElMessage.error(t('develop.workflowCanvas.inputBindingFailed') + error.message)
  }
}

function updateNodeParams(nodeId, params, publicParameters = null) {
  const node = graph.value?.findById(nodeId)
  if (!node) return
  const model = { ...node.getModel(), params: { ...params } }
  if (Array.isArray(publicParameters)) model.publicParameters = publicParameters
  graph.value.updateItem(node, model)
  recordHistory({ mergeKey: `params:${nodeId}` })
  emitWorkflow()
}

function clearGraph() {
  graph.value?.clear()
  nodeCounter.value = 0
  emit('node-click', null)
  recordHistory()
  emitWorkflow()
  emitLayout()
}

function handleUndo() {
  if (undo()) ElMessage.success(t('develop.workflowCanvas.undone'))
}

function handleRedo() {
  if (redo()) ElMessage.success(t('develop.workflowCanvas.redone'))
}

function handleCopy() {
  if (!copy(selectedItem.value)) {
    ElMessage.warning(t('develop.workflowCanvas.noNodeToCopy'))
    return false
  }
  ElMessage.success(t('develop.workflowCanvas.nodeCopied'))
  return true
}

function handlePaste() {
  const item = paste()
  if (!item) {
    ElMessage.warning(t('develop.workflowCanvas.noNodeToPaste'))
    return
  }
  selectGraphItem(item)
  recordHistory()
  emitWorkflow()
  emitLayout()
  ElMessage.success(t('develop.workflowCanvas.nodePasted'))
}

function handleDuplicate() {
  if (handleCopy()) handlePaste()
}

function handleKeydown(event) {
  const modifier = event.metaKey || event.ctrlKey
  const key = event.key.toLowerCase()
  if (modifier && key === 'z') {
    event.preventDefault()
    event.shiftKey ? handleRedo() : handleUndo()
  } else if (modifier && key === 'y') {
    event.preventDefault()
    handleRedo()
  } else if (modifier && key === 'c') {
    event.preventDefault()
    handleCopy()
  } else if (modifier && key === 'v') {
    event.preventDefault()
    handlePaste()
  } else if (modifier && key === 'd') {
    event.preventDefault()
    handleDuplicate()
  } else if (key === 'delete' || key === 'backspace') {
    event.preventDefault()
    handleDelete()
  }
}

function handleZoomIn() {
  zoomIn()
  emitLayout()
}

function handleZoomOut() {
  zoomOut()
  emitLayout()
}

function handleFitView() {
  fitView()
  emitLayout()
}

function handleAutoLayout() {
  autoLayout()
  recordHistory()
  emitLayout()
}

function restoreGraphSnapshot(snapshot) {
  graph.value?.clear()
  graph.value?.data(snapshot)
  graph.value?.render()
  selectedItem.value = null
  emit('node-click', null)
  emitWorkflow()
  emitLayout()
  applyValidationStates()
}

function emitLayout() {
  if (graph.value) emit('update:layout', captureLayout())
}

function createCopiedNodeId(node) {
  const base = node.operator || 'node'
  let id
  do {
    nodeCounter.value += 1
    id = `${base}_${nodeCounter.value}`
  } while (graph.value?.findById?.(id))
  return id
}

function selectGraphItem(item) {
  graph.value.getNodes().forEach(node => graph.value.setItemState(node, 'selected', false))
  graph.value.setItemState(item, 'selected', true)
  selectedItem.value = item
  handleNodeClick({ item })
  container.value?.focus()
}

function hasStoredLayout(layout) {
  return Boolean(
    Object.keys(layout?.nodes || {}).length ||
    layout?.viewport
  )
}

function selectNode(nodeId) {
  const node = graph.value?.findById(nodeId)
  if (!node) return
  graph.value.getNodes().forEach(item => graph.value.setItemState(item, 'selected', false))
  graph.value.setItemState(node, 'selected', true)
  graph.value.focusItem(node, true, { duration: 200, easing: 'easeCubic' })
  const model = node.getModel()
  emit('node-click', {
    id: model.id,
    operator: model.operator,
    params: model.params,
    label: model.label,
    displayName: model.displayName,
    publicParameters: model.publicParameters
  })
}

function applyValidationStates() {
  if (!graph.value) return
  const invalidIds = new Set(props.validationIssues.map(issue => issue.nodeId).filter(Boolean))
  graph.value.getNodes().forEach(node => {
    graph.value.setItemState(node, 'invalid', invalidIds.has(node.getID()))
  })
}

function normalizeOperator(operator) {
  return {
    ...operator,
    display_name: operator.display_name || operator.displayName || operator.name,
    public_parameters: operator.public_parameters || operator.publicParameters || [],
    output_ports: operator.output_ports || operator.outputPorts || []
  }
}

function operatorByName(name) {
  const operator = props.operators.find(item => item.name === name || item.id === name)
  return operator ? normalizeOperator(operator) : null
}

function fallbackOperator(task) {
  return {
    name: task.operator,
    display_name: task.operator,
    public_parameters: [],
    output_ports: []
  }
}

function defaultOperatorParams(operator) {
  const params = {}
  ;(operator.public_parameters || []).forEach(parameter => {
    if (!isWorkflowInputParameter(parameter) && parameter.default !== undefined && parameter.default !== null) {
      params[parameter.name] = parameter.default
    }
  })
  return params
}

function findWorkflowBinding(params, sourceId) {
  for (const [parameterName, value] of Object.entries(params || {})) {
    if (isWorkflowReference(value) && value.$ref === sourceId) {
      return { parameterName, ref: value }
    }
  }
  return null
}

function isWorkflowReference(value) {
  return value && typeof value === 'object' && !Array.isArray(value) && typeof value.$ref === 'string'
}

function updateNodeCounter(nodeId) {
  const match = String(nodeId).match(/_(\d+)$/)
  if (match) nodeCounter.value = Math.max(nodeCounter.value, Number(match[1]))
}

function workflowSignature(workflow) {
  return JSON.stringify(workflow || { tasks: [] })
}

function resolveEdgeStyle() {
  const stroke = cssColor('--addp-text-tertiary')
  return {
    stroke,
    lineWidth: 1.5,
    radius: 8,
    endArrow: {
      path: 'M 0,0 L 10,4 L 10,-4 Z',
      fill: stroke,
      d: 0
    }
  }
}

function cssColor(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

defineExpose({
  addOperator,
  updateNodeParams,
  clearGraph,
  getWorkflow: buildWorkflowFromGraph,
  getClientValidationIssues,
  selectNode,
  fitView,
  autoLayout
})
</script>

<style scoped>
.workflow-dag-canvas {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.canvas-toolbar {
  min-height: 44px;
  padding: 6px 12px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
}

.canvas-toolbar-group {
  display: flex;
  align-items: center;
  gap: 6px;
}

.zoom-value {
  min-width: 42px;
  color: var(--addp-text-tertiary);
  font-size: 12px;
  text-align: right;
}

#workflow-dag-container {
  position: relative;
  z-index: 1;
  flex: 1;
  overflow: hidden;
  outline: none;
  background-color: var(--addp-bg-secondary);
  background-image: radial-gradient(var(--addp-border-color-light) 1px, transparent 1px);
  background-size: 20px 20px;
}
</style>
