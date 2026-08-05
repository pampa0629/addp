<template>
  <div class="workflow-dag-canvas">
    <StatusAnnouncer
      :label="t('develop.workflowCanvas.navigationStatusLabel')"
      :message="navigationAnnouncement"
    />
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
      class="addp-dag-focus-region"
      role="region"
      :aria-label="t('develop.workflowCanvas.canvasAriaLabel')"
      aria-keyshortcuts="ArrowLeft ArrowRight ArrowUp ArrowDown Enter Delete Escape"
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
import { StatusAnnouncer } from '@addp/common-frontend'
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
  createDAGKeyboardHandler,
  createDAGDirectEdgeBehavior,
  createDAGDragNodeBehavior,
  getDAGIncomingEdgeModels,
  getDAGUpstreamCandidates,
  isDAGPortEvent,
  useDAGClipboard,
  useDAGCore,
  useDAGHistory,
  useDAGLayout,
  useDAGSelection,
  useDAGViewport,
  useLoopDetection,
  validateDAGConnection
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

const emit = defineEmits(['update:workflow', 'update:layout', 'node-click', 'node-select'])
const container = ref(null)
const nodeCounter = ref(0)
const inspectedNodeId = ref('')
const lastEmittedSignature = ref('')
const edgeStyle = resolveEdgeStyle()

const { graph, initGraph } = useDAGCore(container, {
  modes: {
    default: [
      'drag-canvas',
      'zoom-canvas',
      createDAGDragNodeBehavior(),
      {
        type: 'click-select',
        selectEdge: true
      },
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
    hover: {
      stroke: cssColor('--el-color-primary'),
      lineWidth: 3.5
    },
    selected: {
      stroke: cssColor('--el-color-primary'),
      lineWidth: 3.5
    }
  }
})

const { hasLoop } = useLoopDetection(graph)
const {
  selectedItem,
  initSelectionListener,
  selectItem,
  selectPreviousNode,
  selectNextNode,
  deleteSelected,
  clearSelection
} = useDAGSelection(graph, {
  focusTarget: container
})
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
const navigationAnnouncement = computed(() => {
  if (selectedItem.value?.getType?.() !== 'node') return ''
  const model = selectedItem.value.getModel()
  const name = model.displayName || model.label || model.operator || model.id
  const issueCount = props.validationIssues.filter(issue => issue.nodeId === model.id).length
  return issueCount
    ? t('develop.workflowCanvas.nodeSelectedWithIssues', { name, count: issueCount })
    : t('develop.workflowCanvas.nodeSelected', { name })
})

onMounted(() => {
  registerWorkflowEditorNode()
  initGraph()
  if (!graph.value) return

  initSelectionListener()
  graph.value.on('node:click', handleNodeClick)
  graph.value.on('canvas:click', clearNodeSelection)
  graph.value.on('edge:click', handleEdgeClick)
  graph.value.on('edge:mouseenter', handleEdgeMouseEnter)
  graph.value.on('edge:mouseleave', handleEdgeMouseLeave)
  graph.value.on('aftercreateedge', ({ edge }) => {
    recordHistory()
    emitWorkflow()
    emitLayout()
    refreshInspectedNode(edge?.getModel?.()?.target)
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
  const baseResult = validateDAGConnection({
    graph: graph.value,
    sourceId,
    targetId,
    hasLoop,
    isDuplicate: model => (
      model.source === sourceId &&
      model.target === targetId &&
      (model.sourcePort || 'default') === (sourcePort?.name || 'default') &&
      model.targetParam === targetPort?.name
    )
  })
  if (baseResult !== true) return baseResult
  if (!areWorkflowTypesCompatible(sourcePort?.type, targetPort?.type)) {
    return 'type_mismatch'
  }

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
  inspectedNodeId.value = event.item.getModel().id
  emit('node-click', nodeViewModel(event.item.getModel()))
}

function clearNodeSelection() {
  inspectedNodeId.value = ''
  emit('node-click', null)
}

function handleEdgeClick(event) {
  event.item?.toFront?.()
  clearNodeSelection()
}

function handleEdgeMouseEnter(event) {
  if (!event.item) return
  event.item.toFront()
  graph.value?.setItemState?.(event.item, 'hover', true)
}

function handleEdgeMouseLeave(event) {
  if (!event.item) return
  graph.value?.setItemState?.(event.item, 'hover', false)
  if (!event.item.hasState?.('selected')) event.item.toBack?.()
}

function handleDelete() {
  const selected = selectedItem.value
  if (!selected) return
  const type = selected.getType?.()
  if (deleteSelected()) {
    if (type === 'node') {
      inspectedNodeId.value = ''
      emit('node-click', null)
    } else {
      refreshInspectedNode(selected.getModel?.()?.target)
    }
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
      const bindings = findWorkflowBindings(task.params, sourceId)
      const sourceNode = nodeById.get(sourceId)
      const targetNode = nodeById.get(task.id)
      if (bindings.length === 0) {
        edges.push(buildLoadedEdge(sourceId, task.id, sourceNode, targetNode, null))
      } else {
        bindings.forEach(binding => {
          edges.push(buildLoadedEdge(sourceId, task.id, sourceNode, targetNode, binding))
        })
      }
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

function updateInputConnection({ nodeId, targetParam, sourceId = '', sourcePort = 'default' }) {
  const targetItem = graph.value?.findById(nodeId)
  const targetModel = targetItem?.getModel?.()
  const targetPort = (targetModel?.inputPorts || []).find(port => port.name === targetParam)
  if (!targetItem || !targetPort) return false

  const currentEdges = graph.value.getEdges().filter(edge => {
    const model = edge.getModel()
    return model.target === nodeId && model.targetParam === targetParam
  })

  if (sourceId) {
    const sourceItem = graph.value.findById(sourceId)
    const outputPort = (sourceItem?.getModel?.()?.outputPorts || []).find(port => port.name === sourcePort)
    if (!sourceItem || !outputPort) return false

    const unchanged = currentEdges.length === 1 && currentEdges[0].getModel().source === sourceId &&
      (currentEdges[0].getModel().sourcePort || 'default') === sourcePort
    if (unchanged) return true

    const connectionResult = validateDAGConnection({
      graph: {
        getEdges: () => graph.value.getEdges().filter(edge => !currentEdges.includes(edge))
      },
      sourceId,
      targetId: nodeId,
      hasLoop,
      isDuplicate: model => (
        model.source === sourceId &&
        model.target === nodeId &&
        (model.sourcePort || 'default') === sourcePort &&
        model.targetParam === targetParam
      )
    })
    if (connectionResult !== true || !areWorkflowTypesCompatible(outputPort.type, targetPort.type)) {
      handleConnectionRejected({
        reason: connectionResult !== true ? connectionResult : 'type_mismatch'
      })
      return false
    }

    currentEdges.forEach(edge => graph.value.removeItem(edge, false))
    graph.value.addItem('edge', {
      source: sourceId,
      target: nodeId,
      ...buildEdgeConfig({
        sourceItem,
        targetItem,
        sourcePort: outputPort,
        targetPort
      })
    })
  } else {
    if (currentEdges.length === 0) return true
    currentEdges.forEach(edge => graph.value.removeItem(edge, false))
  }

  graph.value.paint()
  recordHistory()
  emitWorkflow()
  emitLayout()
  refreshInspectedNode(nodeId)
  ElMessage.success(t(sourceId ? 'develop.workflowCanvas.connected' : 'develop.workflowCanvas.disconnected'))
  return true
}

function clearGraph() {
  graph.value?.clear()
  nodeCounter.value = 0
  inspectedNodeId.value = ''
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

function handleCancelSelection() {
  if (clearSelection()) emit('node-click', null)
}

function handleKeyboardNodeSelection(selectNode) {
  const item = selectNode()
  if (!item) return
  const model = item.getModel()
  emit('node-select', nodeViewModel(model))
}

function activateKeyboardSelection() {
  if (selectedItem.value?.getType?.() === 'node') handleNodeClick({ item: selectedItem.value })
}

const handleKeydown = createDAGKeyboardHandler({
  undo: handleUndo,
  redo: handleRedo,
  copy: handleCopy,
  paste: handlePaste,
  duplicate: handleDuplicate,
  delete: handleDelete,
  cancelSelection: handleCancelSelection,
  selectPreviousNode: () => handleKeyboardNodeSelection(selectPreviousNode),
  selectNextNode: () => handleKeyboardNodeSelection(selectNextNode),
  activateSelection: activateKeyboardSelection
})

function handleZoomIn() {
  zoomIn()
}

function handleZoomOut() {
  zoomOut()
}

function handleFitView() {
  fitView()
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
  selectItem(item)
  handleNodeClick({ item })
}

function nodeViewModel(model) {
  return {
    id: model.id,
    operator: model.operator,
    params: model.params,
    label: model.label,
    displayName: model.displayName,
    publicParameters: model.publicParameters,
    inputConnections: inputConnectionsForNode(model.id),
    inputConnectionOptions: inputConnectionOptionsForNode(model)
  }
}

function inputConnectionsForNode(nodeId) {
  return getDAGIncomingEdgeModels(graph.value, nodeId).map(edge => {
    const source = graph.value.findById(edge.source)?.getModel?.() || {}
    return {
      targetParam: edge.targetParam,
      sourceId: edge.source,
      sourceLabel: source.displayName || source.label || source.operator || edge.source,
      sourcePort: edge.sourcePort || 'default'
    }
  })
}

function inputConnectionOptionsForNode(targetModel) {
  const candidates = getDAGUpstreamCandidates({
    graph: graph.value,
    targetId: targetModel.id,
    hasLoop
  })

  return (targetModel.inputPorts || []).flatMap(inputPort => candidates.flatMap(candidate => {
    const source = candidate.node
    return (source.outputPorts || [])
      .filter(outputPort => areWorkflowTypesCompatible(outputPort.type, inputPort.type))
      .map(outputPort => ({
        key: JSON.stringify([source.id, outputPort.name]),
        targetParam: inputPort.name,
        sourceId: source.id,
        sourceLabel: source.displayName || source.label || source.operator || source.id,
        sourcePort: outputPort.name,
        sourcePortLabel: outputPort.name,
        disabled: candidate.disabled
      }))
  }))
}

function refreshInspectedNode(changedNodeId) {
  if (!inspectedNodeId.value || inspectedNodeId.value !== changedNodeId) return
  const model = graph.value?.findById(inspectedNodeId.value)?.getModel?.()
  if (model) emit('node-click', nodeViewModel(model))
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
  inspectedNodeId.value = model.id
  emit('node-click', nodeViewModel(model))
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

function findWorkflowBindings(params, sourceId) {
  return Object.entries(params || {})
    .filter(([, value]) => isWorkflowReference(value) && value.$ref === sourceId)
    .map(([parameterName, ref]) => ({ parameterName, ref }))
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
    lineAppendWidth: 10,
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
  updateInputConnection,
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
  background-color: var(--addp-bg-secondary);
  background-image: radial-gradient(var(--addp-border-color-light) 1px, transparent 1px);
  background-size: 20px 20px;
}
</style>
