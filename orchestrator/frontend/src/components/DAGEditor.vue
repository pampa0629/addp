<template>
  <div class="dag-editor">
    <StatusAnnouncer
      :label="t('orchestrator.dagEditor.navigationStatusLabel')"
      :message="navigationAnnouncement"
    />
    <div class="toolbar">
      <div class="toolbar-left">
        <el-tooltip :content="t('orchestrator.dagEditor.undo')">
          <el-button circle size="small" :aria-label="t('orchestrator.dagEditor.undo')" :disabled="!canUndo" @click="handleUndo"><el-icon><RefreshLeft /></el-icon></el-button>
        </el-tooltip>
        <el-tooltip :content="t('orchestrator.dagEditor.redo')">
          <el-button circle size="small" :aria-label="t('orchestrator.dagEditor.redo')" :disabled="!canRedo" @click="handleRedo"><el-icon><RefreshRight /></el-icon></el-button>
        </el-tooltip>
        <el-divider direction="vertical" />
        <el-tooltip :content="t('orchestrator.dagEditor.copyNode')">
          <el-button circle size="small" :aria-label="t('orchestrator.dagEditor.copyNode')" :disabled="!canCopyNode" @click="handleCopy"><el-icon><CopyDocument /></el-icon></el-button>
        </el-tooltip>
        <el-tooltip :content="t('orchestrator.dagEditor.pasteNode')">
          <el-button circle size="small" :aria-label="t('orchestrator.dagEditor.pasteNode')" :disabled="!copiedNode" @click="handlePaste"><el-icon><DocumentAdd /></el-icon></el-button>
        </el-tooltip>
        <el-tooltip :content="t('orchestrator.dagEditor.duplicateNode')">
          <el-button circle size="small" :aria-label="t('orchestrator.dagEditor.duplicateNode')" :disabled="!canCopyNode" @click="handleDuplicate"><el-icon><Plus /></el-icon></el-button>
        </el-tooltip>
        <el-divider direction="vertical" />
        <el-tooltip :content="t('orchestrator.dagEditor.deleteSelected')">
          <el-button circle size="small" type="danger" plain :aria-label="t('orchestrator.dagEditor.deleteSelected')" @click="handleDelete" :disabled="!selectedItem">
            <el-icon><Delete /></el-icon>
          </el-button>
        </el-tooltip>
        <el-divider direction="vertical" />
        <el-tooltip :content="t('orchestrator.dagEditor.zoomOut')">
          <el-button circle size="small" :aria-label="t('orchestrator.dagEditor.zoomOut')" :disabled="!canZoomOut" @click="handleZoomOut"><el-icon><ZoomOut /></el-icon></el-button>
        </el-tooltip>
        <el-tooltip :content="t('orchestrator.dagEditor.zoomIn')">
          <el-button circle size="small" :aria-label="t('orchestrator.dagEditor.zoomIn')" :disabled="!canZoomIn" @click="handleZoomIn"><el-icon><ZoomIn /></el-icon></el-button>
        </el-tooltip>
        <el-tooltip :content="t('orchestrator.dagEditor.fitView')">
          <el-button circle size="small" :aria-label="t('orchestrator.dagEditor.fitView')" @click="handleFitView"><el-icon><FullScreen /></el-icon></el-button>
        </el-tooltip>
        <el-tooltip :content="t('orchestrator.dagEditor.autoLayout')">
          <el-button circle size="small" :aria-label="t('orchestrator.dagEditor.autoLayout')" @click="handleAutoLayout"><el-icon><Rank /></el-icon></el-button>
        </el-tooltip>
      </div>
      <el-tooltip :content="t('orchestrator.dagEditor.clearBtn')">
        <el-button circle size="small" :aria-label="t('orchestrator.dagEditor.clearBtn')" @click="handleClear"><el-icon><DocumentDelete /></el-icon></el-button>
      </el-tooltip>
    </div>

    <div
      id="dag-container"
      ref="container"
      class="addp-dag-focus-region"
      role="region"
      :aria-label="t('orchestrator.dagEditor.canvasAriaLabel')"
      aria-keyshortcuts="ArrowLeft ArrowRight ArrowUp ArrowDown Enter Delete Escape"
      tabindex="0"
      @dragover.prevent
      @drop="handleDrop"
      @keydown="handleKeydown"
    ></div>

    <!-- 节点配置抽屉 -->
    <el-drawer
      v-model="drawerVisible"
      :title="t('orchestrator.dagEditor.drawerTitle')"
      size="min(520px, calc(100vw - 12px))"
    >
      <el-form :model="currentNode" label-width="100px">
        <el-form-item :label="t('orchestrator.dagEditor.stepNameLabel')">
          <el-input v-model="currentNode.name" :placeholder="t('orchestrator.dagEditor.stepNamePlaceholder')"></el-input>
        </el-form-item>

        <el-form-item :label="t('orchestrator.dagEditor.executionModeLabel')">
          <el-tag v-if="currentNode.provider" type="success">{{ t('orchestrator.dagEditor.modeTaskRef') }}</el-tag>
          <el-tag v-else type="info">{{ t('orchestrator.dagEditor.modeNotConfigured') }}</el-tag>
        </el-form-item>

        <template v-if="currentNode.provider">
          <el-form-item :label="t('orchestrator.dagEditor.providerLabel')">
            <el-input v-model="currentNode.provider" disabled></el-input>
          </el-form-item>
          <el-form-item :label="t('orchestrator.dagEditor.taskTypeLabel')">
            <el-input v-model="currentNode.taskType" disabled></el-input>
          </el-form-item>
          <el-form-item :label="t('orchestrator.dagEditor.taskIdLabel')">
            <el-input :model-value="String(currentNode.taskId || '')" disabled></el-input>
          </el-form-item>
          <el-form-item :label="t('orchestrator.dagEditor.ownerTaskLabel')">
            <el-tooltip
              :content="ownerTaskButtonTooltip"
              placement="top"
            >
              <el-button
                type="primary"
                plain
                :disabled="!canOpenOwnerTask"
                @click="openCurrentOwnerTask"
              >
                <el-icon><Edit /></el-icon>
                {{ t('orchestrator.dagEditor.openOwnerTaskBtn') }}
              </el-button>
            </el-tooltip>
          </el-form-item>
        </template>

        <el-form-item :label="t('orchestrator.dagEditor.timeoutLabel')">
          <el-input-number v-model="currentNode.timeout" :min="0" :max="3600"></el-input-number>
        </el-form-item>

        <el-divider content-position="left">{{ t('orchestrator.dagEditor.dependenciesLabel') }}</el-divider>
        <el-form-item :label="t('orchestrator.dagEditor.predecessorsLabel')">
          <el-select
            v-model="currentDependencyIds"
            multiple
            clearable
            filterable
            :placeholder="t('orchestrator.dagEditor.predecessorsPlaceholder')"
            @change="applyCurrentDependencies"
          >
            <el-option
              v-for="candidate in currentDependencyCandidates"
              :key="candidate.node.id"
              :label="dependencyOptionLabel(candidate.node)"
              :value="candidate.node.id"
              :disabled="candidate.disabled"
            >
              <span class="dependency-option-name">{{ candidate.node.name || candidate.node.label || candidate.node.id }}</span>
              <span class="dependency-option-id">{{ candidate.node.id }}</span>
            </el-option>
          </el-select>
        </el-form-item>

        <el-divider content-position="left">{{ t('orchestrator.dagEditor.parametersLabel') }}</el-divider>
        <ExecutionParameterForm
          v-if="currentExecutionContract"
          :model-value="currentNode.parameters || {}"
          :contract="currentExecutionContract"
          :upstream-outputs="currentUpstreamOutputs"
          allow-upstream
          @update:model-value="applyCurrentNodeDraft"
        />
        <el-alert
          v-else
          type="warning"
          :closable="false"
          :title="t('orchestrator.dagEditor.executionContractUnavailable')"
          show-icon
        />
      </el-form>
    </el-drawer>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  CopyDocument,
  Delete,
  DocumentAdd,
  DocumentDelete,
  Edit,
  FullScreen,
  Plus,
  Rank,
  RefreshLeft,
  RefreshRight,
  ZoomIn,
  ZoomOut
} from '@element-plus/icons-vue'
import { buildTaskOwnerUrl, ExecutionParameterForm, StatusAnnouncer } from '@addp/common-frontend'
import {
  createDAGKeyboardHandler,
  createDAGDirectEdgeBehavior,
  createDAGDragNodeBehavior,
  generateColor,
  getDAGIncomingEdgeModels,
  getDAGUpstreamCandidates,
  useDAGClipboard,
  useDAGCore,
  useDAGHistory,
  useDAGLayout,
  useDAGSelection,
  useDAGViewport,
  useLoopDetection,
  validateDAGConnection
} from '@addp/common-frontend/dag'
import taskProvidersAPI from '../api/taskProviders'
import { activeTaskCapabilityMetadata } from '../utils/taskCapabilityMetadata'
import {
  arePortTypesCompatible,
  clearParameterBinding,
  executionInputPorts,
  executionOutputPorts,
  outputTemplate,
  parameterBindings,
  setParameterBinding
} from '../utils/orchestrationPorts'
import {
  ORCHESTRATION_NODE_TYPE,
  orchestrationAnchor,
  orchestrationPort,
  registerOrchestrationEditorNode
} from './orchestrationEditorNode'

const { t } = useI18n()

const props = defineProps({
  initialSteps: {
    type: Array,
    default: () => []
  },
  initialLayout: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['update:steps', 'update:layout'])

const container = ref(null)
const drawerVisible = ref(false)
const currentNode = ref({})
const currentDependencyIds = ref([])
const taskTypeEditUrlIndex = ref(new Map())
const taskContextIndex = ref(new Map())
const executionContractIndex = ref(new Map())
const lastStepsSignature = ref('')
let nodeCopyCounter = 0
let addedNodeCounter = 0
let syncingNodeDraft = false
let syncingDependencies = false
let stepsLoadGeneration = 0
const taskRuntimeMetadataRequests = new Map()
const canvasColors = resolveCanvasColors()

// 使用 composables
const { graph, initGraph, loadData } = useDAGCore(container, {
  modes: {
    default: [
      'drag-canvas',
      'zoom-canvas',
      createDAGDragNodeBehavior(),
      { type: 'click-select', selectEdge: true },
      createDAGDirectEdgeBehavior({
        resolveSource: event => orchestrationPort(event, 'output'),
        resolveTarget: event => orchestrationPort(event, 'input'),
        canConnect: canCreateConnection,
        buildEdgeConfig: buildConnectionEdgeConfig,
        onRejected: handleConnectionRejected
      })
    ]
  },
  defaultNode: {
    type: ORCHESTRATION_NODE_TYPE
  },
  defaultEdge: {
    type: 'polyline',
    style: parameterEdgeStyle()
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
  clearSelection,
  clearGraph
} = useDAGSelection(graph, {
  focusTarget: container
})
const { canZoomIn, canZoomOut, zoomIn, zoomOut, fitView, autoLayout } = useDAGViewport(graph)
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
  const name = model.name || model.label || model.provider || model.id
  return t('orchestrator.dagEditor.nodeSelected', { name })
})

const currentOwnerEditUrl = computed(() => resolveNodeEditUrl(currentNode.value))
const currentOwnerGraphId = computed(() => resolveNodeGraphId(currentNode.value))
const canOpenOwnerTask = computed(() => canBuildOwnerTaskUrl(currentOwnerEditUrl.value, currentNode.value, currentOwnerGraphId.value))
const ownerTaskButtonTooltip = computed(() => {
  if (canOpenOwnerTask.value) {
    return t('orchestrator.dagEditor.openOwnerTaskTooltip')
  }
  if (!hasValue(currentOwnerEditUrl.value)) {
    return t('orchestrator.dagEditor.ownerTaskUrlMissing')
  }
  return t('orchestrator.dagEditor.ownerTaskContextMissing')
})
const currentExecutionContract = computed(() => executionContractIndex.value.get(
  taskContextIndexKey(currentNode.value?.provider, currentNode.value?.taskType, currentNode.value?.taskId)
) || null)
const currentUpstreamOutputs = computed(() => {
  if (!graph.value || !currentNode.value?.id) return []
  return currentDependencyIds.value.flatMap(stepId => {
    const node = graph.value.findById(stepId)?.getModel?.()
    if (!node) return []
    return (node.outputPorts || []).map(output => ({
      ...output,
      label: `${node.name || node.label || node.id} / ${output.label}`,
      template: outputTemplate(node.id, output.path)
    }))
  })
})
const currentDependencyCandidates = computed(() => getDAGUpstreamCandidates({
  graph: graph.value,
  targetId: currentNode.value?.id,
  hasLoop
}))

watch(
  () => [currentNode.value?.name, currentNode.value?.timeout],
  () => applyCurrentNodeDraft(currentNode.value?.parameters || {}),
  { flush: 'sync' }
)

watch(() => props.initialSteps, async steps => {
  if (!graph.value || stepsSignature(steps) === lastStepsSignature.value) return
  await loadStepsWithRuntimeMetadata(steps)
}, { deep: true })

onMounted(async () => {
  registerOrchestrationEditorNode()
  initGraph()
  initSelectionListener()
  await loadTaskProviderRuntimeMetadata()

  // 双击节点事件
  graph.value.on('node:dblclick', handleNodeClick)

  // 边创建后的处理
  graph.value.on('aftercreateedge', ({ edge }) => {
    const model = edge?.getModel?.()
    if (model?.edgeKind === 'parameter') {
      removeControlEdges(model.source, model.target)
      applyParameterEdgeBinding(model)
    }
    recordHistory()
    ElMessage.success(t('orchestrator.dagEditor.edgeCreated'))
    emitSteps()
    emitLayout()
    syncCurrentDependencies(edge?.getModel?.()?.target)
  })
  graph.value.on('node:dragend', () => {
    recordHistory()
    emitLayout()
  })
  graph.value.on('canvas:dragend', emitLayout)
  graph.value.on('wheelzoom', emitLayout)

  await loadStepsWithRuntimeMetadata(props.initialSteps)
})

function handleNodeClick(evt) {
  const model = evt.item.getModel()
  withNodeDraftSync(() => {
    currentNode.value = {
      ...model,
      graphId: resolveNodeGraphId(model),
      editUrl: resolveNodeEditUrl(model)
    }
    syncCurrentDependencies(model.id, true)
    drawerVisible.value = true
  })
}

function applyCurrentNodeDraft(parameters = currentNode.value?.parameters || {}) {
  if (syncingNodeDraft || !drawerVisible.value || !graph.value || !currentNode.value?.id) return

  currentNode.value.parameters = parameters
  updateNodeParameters(currentNode.value.id, parameters, {
    ...currentNode.value,
    label: currentNode.value.name || currentNode.value.provider || currentNode.value.id
  })
  syncParameterEdges(currentNode.value.id, parameters)
  recordHistory({ mergeKey: `node-draft:${currentNode.value.id}` })
  emitSteps()
}

function withNodeDraftSync(callback) {
  const wasSyncing = syncingNodeDraft
  syncingNodeDraft = true
  try {
    callback()
  } finally {
    syncingNodeDraft = wasSyncing
  }
}

function dependencyOptionLabel(node) {
  const name = node?.name || node?.label || node?.id || ''
  return name === node?.id ? name : `${name} (${node.id})`
}

function syncCurrentDependencies(changedTargetId = currentNode.value?.id, force = false) {
  if (!force && changedTargetId !== currentNode.value?.id) return
  syncingDependencies = true
  currentDependencyIds.value = [...new Set(
    getDAGIncomingEdgeModels(graph.value, currentNode.value?.id).map(edge => edge.source)
  )]
  syncingDependencies = false
}

function applyCurrentDependencies(sourceIds) {
  if (syncingDependencies || !graph.value || !currentNode.value?.id) return
  const targetId = currentNode.value.id
  const nextSourceIds = [...new Set(sourceIds || [])]
  const currentEdges = getDAGIncomingEdgeModels(graph.value, targetId)
  const currentSourceIds = new Set(currentEdges.map(edge => edge.source))
  const additions = nextSourceIds.filter(sourceId => !currentSourceIds.has(sourceId))
  const removals = [...currentSourceIds].filter(sourceId => !nextSourceIds.includes(sourceId))

  if (removals.some(sourceId => currentEdges.some(edge => (
    edge.source === sourceId && edge.edgeKind === 'parameter'
  )))) {
    ElMessage.warning(t('orchestrator.dagEditor.removeParameterConnectionFirst'))
    syncCurrentDependencies(targetId, true)
    return
  }

  for (const sourceId of additions) {
    const result = canCreateControlDependency({ sourceId, targetId })
    if (result !== true) {
      handleConnectionRejected({ reason: result })
      syncCurrentDependencies(targetId, true)
      return
    }
  }

  const nextSourceIdSet = new Set(nextSourceIds)
  graph.value.getEdges().forEach(edge => {
    const model = edge.getModel()
    if (model.target === targetId && model.edgeKind === 'control' && !nextSourceIdSet.has(model.source)) {
      graph.value.removeItem(edge, false)
    }
  })
  additions.forEach(sourceId => {
    const source = graph.value.findById(sourceId)?.getModel?.()
    const target = graph.value.findById(targetId)?.getModel?.()
    if (source && target) graph.value.addItem('edge', controlEdgeConfig(source, target), false)
  })
  graph.value.paint()
  syncCurrentDependencies(targetId, true)
  recordHistory()
  emitSteps()
  emitLayout()
  ElMessage.success(t('orchestrator.dagEditor.dependenciesUpdated'))
}

function canCreateControlDependency({ sourceId, targetId }) {
  return validateDAGConnection({
    graph: graph.value,
    sourceId,
    targetId,
    hasLoop
  })
}

function canCreateConnection(context) {
  const { sourceId, targetId, sourcePort, targetPort } = context
  if (sourcePort?.kind !== targetPort?.kind) return 'incompatible'
  if (sourcePort.kind === 'control') return canCreateControlDependency({ sourceId, targetId })

  const sourceModel = context.sourceItem?.getModel?.()
  const targetModel = context.targetItem?.getModel?.()
  const output = sourceModel?.outputPorts?.find(port => port.name === sourcePort.name)
  const input = targetModel?.inputPorts?.find(port => port.name === targetPort.name)
  if (!arePortTypesCompatible(output, input)) return 'incompatible'

  return validateDAGConnection({
    graph: graph.value,
    sourceId,
    targetId,
    hasLoop,
    isDuplicate: model => (
      model.edgeKind === 'parameter' &&
      model.target === targetId &&
      model.targetInput === input.name
    )
  })
}

function buildConnectionEdgeConfig({ sourceItem, targetItem, sourcePort, targetPort }) {
  const source = sourceItem?.getModel?.() || {}
  const target = targetItem?.getModel?.() || {}
  if (sourcePort?.kind === 'control') {
    return {
      edgeKind: 'control',
      sourceAnchor: orchestrationAnchor(source, 'output', 'control'),
      targetAnchor: targetPort ? orchestrationAnchor(target, 'input', 'control') : undefined,
      style: controlEdgeStyle()
    }
  }
  return {
    edgeKind: 'parameter',
    sourceOutput: sourcePort?.name,
    targetInput: targetPort?.name,
    sourceAnchor: orchestrationAnchor(source, 'output', 'parameter', sourcePort?.name),
    targetAnchor: targetPort
      ? orchestrationAnchor(target, 'input', 'parameter', targetPort.name)
      : undefined,
    style: parameterEdgeStyle()
  }
}

function controlEdgeConfig(source, target) {
  return {
    source: source.id,
    target: target.id,
    edgeKind: 'control',
    sourceAnchor: orchestrationAnchor(source, 'output', 'control'),
    targetAnchor: orchestrationAnchor(target, 'input', 'control'),
    style: controlEdgeStyle()
  }
}

function parameterEdgeConfig(source, target, output, input) {
  return {
    source: source.id,
    target: target.id,
    edgeKind: 'parameter',
    sourceOutput: output.name,
    targetInput: input.name,
    sourceAnchor: orchestrationAnchor(source, 'output', 'parameter', output.name),
    targetAnchor: orchestrationAnchor(target, 'input', 'parameter', input.name),
    style: parameterEdgeStyle()
  }
}

function controlEdgeStyle() {
  return {
    stroke: canvasColors.warning,
    lineWidth: 1.5,
    lineDash: [6, 4],
    endArrow: edgeEndArrow(canvasColors.warning)
  }
}

function parameterEdgeStyle() {
  return {
    stroke: canvasColors.primary,
    lineWidth: 1.8,
    endArrow: edgeEndArrow(canvasColors.primary)
  }
}

function edgeEndArrow(color) {
  return {
    path: 'M 0,0 L 9,4 L 9,-4 Z',
    fill: color,
    stroke: color,
    d: -10
  }
}

function applyParameterEdgeBinding(edge) {
  const source = graph.value.findById(edge.source)?.getModel?.()
  const targetItem = graph.value.findById(edge.target)
  const target = targetItem?.getModel?.()
  const output = source?.outputPorts?.find(port => port.name === edge.sourceOutput)
  const input = target?.inputPorts?.find(port => port.name === edge.targetInput)
  if (!source || !target || !output || !input) return
  const parameters = setParameterBinding(
    target.parameters,
    input,
    outputTemplate(source.id, output.path)
  )
  updateNodeParameters(targetItem, parameters)
  syncCurrentNodeParameters(target.id, parameters)
}

function clearParameterEdgeBinding(edge) {
  const targetItem = graph.value.findById(edge.target)
  const target = targetItem?.getModel?.()
  const input = target?.inputPorts?.find(port => port.name === edge.targetInput)
  if (!target || !input) return
  const parameters = clearParameterBinding(target.parameters, input)
  updateNodeParameters(targetItem, parameters)
  syncCurrentNodeParameters(target.id, parameters)
}

function updateNodeParameters(itemOrId, parameters, config = {}) {
  const item = typeof itemOrId === 'string' ? graph.value?.findById(itemOrId) : itemOrId
  const model = item?.getModel?.()
  if (!model) return

  model.parameters = parameters
  graph.value.updateItem(item, { ...config, parameters })
}

function syncCurrentNodeParameters(nodeId, parameters) {
  if (currentNode.value?.id !== nodeId) return
  withNodeDraftSync(() => {
    currentNode.value = { ...currentNode.value, parameters }
  })
}

function removeControlEdges(sourceId, targetId) {
  graph.value.getEdges().forEach(edge => {
    const model = edge.getModel()
    if (model.source === sourceId && model.target === targetId && model.edgeKind === 'control') {
      graph.value.removeItem(edge, false)
    }
  })
}

function syncParameterEdges(targetId, parameters) {
  const target = graph.value.findById(targetId)?.getModel?.()
  if (!target) return
  const desired = parameterBindings(parameters, target.inputPorts).flatMap(binding => {
    const source = graph.value.findById(binding.stepId)?.getModel?.()
    const output = source?.outputPorts?.find(port => (
      port.path.join('.') === binding.outputPath.join('.')
    ))
    return source && output ? [{ source, output, input: binding.inputPort }] : []
  })
  const desiredKeys = new Set(desired.map(binding => parameterEdgeKey(
    binding.source.id,
    targetId,
    binding.output.name,
    binding.input.name
  )))

  graph.value.getEdges().forEach(edge => {
    const model = edge.getModel()
    if (model.target !== targetId || model.edgeKind !== 'parameter') return
    const key = parameterEdgeKey(model.source, model.target, model.sourceOutput, model.targetInput)
    if (!desiredKeys.has(key)) graph.value.removeItem(edge, false)
  })
  const existingKeys = new Set(graph.value.getEdges().map(edge => {
    const model = edge.getModel()
    return parameterEdgeKey(model.source, model.target, model.sourceOutput, model.targetInput)
  }))
  desired.forEach(binding => {
    const key = parameterEdgeKey(binding.source.id, targetId, binding.output.name, binding.input.name)
    if (existingKeys.has(key)) return
    removeControlEdges(binding.source.id, targetId)
    graph.value.addItem('edge', parameterEdgeConfig(binding.source, target, binding.output, binding.input), false)
  })
  graph.value.paint()
  syncCurrentDependencies(targetId)
}

function parameterEdgeKey(source, target, output, input) {
  return `${source || ''}:${target || ''}:${output || ''}:${input || ''}`
}

function handleConnectionRejected({ reason }) {
  if (reason === 'loop') {
    ElMessage.warning(t('orchestrator.dagEditor.loopDetected'))
  } else if (reason === 'duplicate') {
    ElMessage.warning(t('orchestrator.dagEditor.edgeAlreadyExists'))
  } else if (reason === 'incompatible') {
    ElMessage.warning(t('orchestrator.dagEditor.incompatiblePorts'))
  }
}

function handleDelete() {
  const selectedModel = selectedItem.value?.getModel?.()
  const itemType = selectedItem.value?.getType?.()
  if (itemType === 'edge' && selectedModel?.edgeKind === 'parameter') {
    clearParameterEdgeBinding(selectedModel)
  }
  if (itemType === 'node') {
    graph.value.getEdges()
      .map(edge => edge.getModel())
      .filter(edge => edge.source === selectedModel?.id && edge.edgeKind === 'parameter')
      .forEach(clearParameterEdgeBinding)
  }
  if (deleteSelected()) {
    ElMessage.success(itemType === 'edge' ? t('orchestrator.dagEditor.edgeDeleted') : t('orchestrator.dagEditor.nodeDeleted'))
    recordHistory()
    emitSteps()
    emitLayout()
    if (itemType === 'edge') syncCurrentDependencies(selectedModel?.target)
    if (itemType === 'node' && selectedModel?.id === currentNode.value?.id) {
      drawerVisible.value = false
      currentNode.value = {}
      currentDependencyIds.value = []
    }
  }
}

async function handleClear() {
  if (!graph.value?.getNodes().length) return
  try {
    await ElMessageBox.confirm(
      t('orchestrator.dagEditor.clearConfirm'),
      t('orchestrator.dagEditor.clearBtn'),
      {
        type: 'warning',
        customClass: 'addp-message-box',
        confirmButtonText: t('orchestrator.dagEditor.clearConfirmAction'),
        cancelButtonText: t('orchestrator.dagEditor.clearConfirmCancel'),
        confirmButtonClass: 'el-button--danger'
      }
    )
    clearGraph()
    recordHistory()
    ElMessage.info(t('orchestrator.dagEditor.canvasCleared'))
    emitSteps()
    emitLayout()
  } catch {
    // 用户取消
  }
}

async function handleDrop(event) {
  event.preventDefault()

  try {
    const data = event.dataTransfer.getData('application/json')
    if (!data) return

    const nodeData = JSON.parse(data)
    const point = graph.value.getPointByClient(event.clientX, event.clientY)
    await addTask(nodeData, point)
  } catch (error) {
    console.error('拖放失败:', error)
    ElMessage.error(t('orchestrator.dagEditor.addNodeFailed', { error: error.message }))
  }
}

async function addTask(nodeData, point = null) {
  try {
    if (!graph.value || !nodeData) return null
    await ensureTaskRuntimeMetadata(nodeData.provider, nodeData.taskType, nodeData.taskId)
    const targetPoint = point || viewportCenterPoint()
    const colorKey = nodeData.provider || 'unknown'
    const color = generateColor(colorKey)
    const ports = executionPortsForTask(nodeData.provider, nodeData.taskType, nodeData.taskId)
    addedNodeCounter += 1
    const id = `${colorKey}-${Date.now()}-${addedNodeCounter}`

    const nodeModel = {
      id,
      label: nodeData.name,
      name: nodeData.name,
      provider: nodeData.provider || null,
      taskType: nodeData.taskType || null,
      taskId: nodeData.taskId || null,
      graphId: nodeData.graphId || resolveTaskContext(nodeData.provider, nodeData.taskType, nodeData.taskId).graphId || null,
      editUrl: nodeData.editUrl || resolveTaskTypeEditUrl(nodeData.provider, nodeData.taskType),
      parameters: nodeData.parameters || {},
      timeout: 300,
      type: ORCHESTRATION_NODE_TYPE,
      inputPorts: ports.inputs,
      outputPorts: ports.outputs,
      providerColor: color,
      x: targetPoint.x,
      y: targetPoint.y
    }

    const item = graph.value.addItem('node', nodeModel)
    graph.value.paint()
    selectGraphItem(item)
    recordHistory()

    ElMessage.success(t('orchestrator.dagEditor.taskAdded', { name: nodeData.name }))
    emitSteps()
    emitLayout()
    return item
  } catch (error) {
    console.error('添加任务节点失败:', error)
    ElMessage.error(t('orchestrator.dagEditor.addNodeFailed', { error: error.message }))
    return null
  }
}

function viewportCenterPoint() {
  const rect = container.value.getBoundingClientRect()
  return graph.value.getPointByClient(
    rect.left + rect.width / 2,
    rect.top + rect.height / 2
  )
}

function emitSteps() {
  const data = graph.value.save()
  const steps = convertToSteps(data)
  lastStepsSignature.value = stepsSignature(steps)
  emit('update:steps', steps)
}

function convertToSteps(graphData) {
  const nodeMap = new Map()

  graphData.nodes.forEach(node => {
    const step = {
      id: node.id,
      name: node.label || node.provider || node.id,
      provider: node.provider || null,
      task_type: node.taskType || null,
      task_id: node.taskId || null,
      parameters: node.parameters || {},
      depends_on: [],
      timeout: node.timeout ?? 300
    }

    nodeMap.set(node.id, step)
  })

  graphData.edges.forEach(edge => {
    const step = nodeMap.get(edge.target)
    if (step && !step.depends_on.includes(edge.source)) {
      step.depends_on.push(edge.source)
    }
  })

  return Array.from(nodeMap.values())
}

function loadSteps(steps) {
  if (!graph.value) return

  const nodes = []
  const edges = []

  steps.forEach((step) => {
    const colorKey = step.provider || 'unknown'
    const color = generateColor(colorKey)
    const ports = executionPortsForTask(step.provider, step.task_type, step.task_id)

    nodes.push({
      id: step.id,
      label: step.name,
      name: step.name,
      provider: step.provider || null,
      taskType: step.task_type || null,
      taskId: step.task_id || null,
      graphId: resolveTaskContext(step.provider, step.task_type, step.task_id).graphId || null,
      editUrl: resolveTaskTypeEditUrl(step.provider, step.task_type),
      parameters: step.parameters,
      timeout: step.timeout,
      type: ORCHESTRATION_NODE_TYPE,
      inputPorts: ports.inputs,
      outputPorts: ports.outputs,
      providerColor: color
    })

  })

  const nodeMap = new Map(nodes.map(node => [node.id, node]))
  steps.forEach(step => {
    const target = nodeMap.get(step.id)
    if (!target) return
    const bindings = parameterBindings(step.parameters, target.inputPorts)
    const parameterSources = new Set()
    bindings.forEach(binding => {
      if (!step.depends_on?.includes(binding.stepId)) return
      const source = nodeMap.get(binding.stepId)
      const output = source?.outputPorts?.find(port => (
        port.path.join('.') === binding.outputPath.join('.')
      ))
      if (!source || !output) return
      parameterSources.add(source.id)
      edges.push(parameterEdgeConfig(source, target, output, binding.inputPort))
    })
    step.depends_on?.forEach(depId => {
      const source = nodeMap.get(depId)
      if (source && !parameterSources.has(depId)) edges.push(controlEdgeConfig(source, target))
    })
  })

  loadData(applyNodePositions(nodes, props.initialLayout), edges)
  lastStepsSignature.value = stepsSignature(steps)
  if (hasStoredLayout(props.initialLayout)) {
    restoreViewport(props.initialLayout)
  } else if (nodes.length > 0) {
    autoLayout()
  }
  resetHistory(graph.value.save())
  emitLayout()
}

function handleUndo() {
  if (undo()) ElMessage.success(t('orchestrator.dagEditor.undone'))
}

function handleRedo() {
  if (redo()) ElMessage.success(t('orchestrator.dagEditor.redone'))
}

function handleCopy() {
  if (!copy(selectedItem.value)) {
    ElMessage.warning(t('orchestrator.dagEditor.noNodeToCopy'))
    return false
  }
  ElMessage.success(t('orchestrator.dagEditor.nodeCopied'))
  return true
}

function handlePaste() {
  const item = paste()
  if (!item) {
    ElMessage.warning(t('orchestrator.dagEditor.noNodeToPaste'))
    return
  }
  clearCopiedParameterBindings(item)
  selectGraphItem(item)
  recordHistory()
  emitSteps()
  emitLayout()
  ElMessage.success(t('orchestrator.dagEditor.nodePasted'))
}

function clearCopiedParameterBindings(item) {
  const model = item?.getModel?.()
  if (!model) return
  let parameters = model.parameters || {}
  parameterBindings(parameters, model.inputPorts).forEach(binding => {
    parameters = clearParameterBinding(parameters, binding.inputPort)
  })
  updateNodeParameters(item, parameters)
}

function handleDuplicate() {
  if (handleCopy()) handlePaste()
}

function handleCancelSelection() {
  if (!clearSelection()) return
  drawerVisible.value = false
}

function handleKeyboardNodeSelection(selectNode) {
  const item = selectNode()
  if (!item) return
  drawerVisible.value = false
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
  drawerVisible.value = false
  emitSteps()
  emitLayout()
}

function emitLayout() {
  if (graph.value) emit('update:layout', captureLayout())
}

function createCopiedNodeId(node) {
  nodeCopyCounter += 1
  return `${node.provider || 'node'}-${Date.now()}-${nodeCopyCounter}`
}

function selectGraphItem(item) {
  selectItem(item)
}

function stepsSignature(steps) {
  return JSON.stringify(Array.isArray(steps) ? steps : [])
}

function hasStoredLayout(layout) {
  return Boolean(Object.keys(layout?.nodes || {}).length || layout?.viewport)
}

function hasValue(value) {
  return value !== null && value !== undefined && String(value).trim() !== ''
}

function resolveCanvasColors() {
  const style = getComputedStyle(document.documentElement)
  const color = name => style.getPropertyValue(name).trim()
  return {
    node: color('--addp-module-orchestrator'),
    primary: color('--el-color-primary'),
    warning: color('--el-color-warning'),
    danger: color('--el-color-danger'),
    onPrimary: color('--el-color-white'),
    background: color('--addp-bg-primary')
  }
}

function parseCapabilities(capabilities) {
  if (!capabilities) return {}
  if (typeof capabilities === 'object') return capabilities
  try {
    return JSON.parse(capabilities)
  } catch (error) {
    return {}
  }
}

function taskTypeIndexKey(provider, taskType) {
  return `${provider || ''}:${taskType || ''}`
}

function taskContextIndexKey(provider, taskType, taskId) {
  return `${provider || ''}:${taskType || ''}:${taskId || ''}`
}

async function loadTaskProviderRuntimeMetadata() {
  try {
    const providers = await taskProvidersAPI.list()
    const editUrlIndex = new Map()
    providers.forEach(provider => {
      const moduleName = provider.module_name
      const capabilities = parseCapabilities(provider.capabilities)
      const taskCapabilities = Array.isArray(capabilities.task_capabilities) ? capabilities.task_capabilities : []
      activeTaskCapabilityMetadata(taskCapabilities)
        .forEach(item => {
          if (item.editUrl) {
            editUrlIndex.set(taskTypeIndexKey(moduleName, item.type), item.editUrl)
          }
        })
    })
    taskTypeEditUrlIndex.value = editUrlIndex
  } catch (error) {
    console.error('加载任务提供者运行态元数据失败:', error)
  }
}

async function loadStepsWithRuntimeMetadata(steps) {
  const generation = ++stepsLoadGeneration
  await ensureTaskRuntimeMetadataForSteps(steps)
  if (generation !== stepsLoadGeneration) return
  loadSteps(steps)
}

async function ensureTaskRuntimeMetadataForSteps(steps) {
  const tasks = new Map()
  ;(steps || []).forEach(step => {
    const provider = step?.provider
    const taskType = step?.task_type
    const taskId = step?.task_id
    if (!hasValue(provider) || !hasValue(taskType) || !hasValue(taskId)) return
    tasks.set(taskContextIndexKey(provider, taskType, taskId), { provider, taskType, taskId })
  })
  const results = await Promise.allSettled(
    [...tasks.values()].map(task => ensureTaskRuntimeMetadata(task.provider, task.taskType, task.taskId))
  )
  const failures = results.filter(result => result.status === 'rejected')
  if (failures.length > 0) {
    failures.forEach(result => console.error('加载任务执行契约失败:', result.reason))
    ElMessage.error(t('orchestrator.dagEditor.executionContractUnavailable'))
  }
}

async function ensureTaskRuntimeMetadata(provider, taskType, taskId) {
  if (!hasValue(provider) || !hasValue(taskType) || !hasValue(taskId)) {
    throw new Error('task runtime metadata context is incomplete')
  }
  const key = taskContextIndexKey(provider, taskType, taskId)
  if (executionContractIndex.value.has(key)) return
  if (taskRuntimeMetadataRequests.has(key)) return taskRuntimeMetadataRequests.get(key)

  const request = taskProvidersAPI.getTaskDetail(provider, taskType, taskId)
    .then(detail => {
      if (!detail?.execution_contract) {
        throw new Error(`execution_contract is unavailable for ${provider}/${taskType}/${taskId}`)
      }
      taskContextIndex.value = new Map(taskContextIndex.value).set(key, {
        graphId: detail.graph_id || null
      })
      executionContractIndex.value = new Map(executionContractIndex.value).set(key, detail.execution_contract)
    })
    .finally(() => taskRuntimeMetadataRequests.delete(key))
  taskRuntimeMetadataRequests.set(key, request)
  return request
}

function executionPortsForTask(provider, taskType, taskId) {
  const contract = executionContractIndex.value.get(taskContextIndexKey(provider, taskType, taskId))
  return {
    inputs: executionInputPorts(contract),
    outputs: executionOutputPorts(contract)
  }
}

function resolveTaskTypeEditUrl(provider, taskType) {
  return taskTypeEditUrlIndex.value.get(taskTypeIndexKey(provider, taskType)) || ''
}

function resolveTaskContext(provider, taskType, taskId) {
  return taskContextIndex.value.get(taskContextIndexKey(provider, taskType, taskId)) || {}
}

function resolveNodeEditUrl(node) {
  return node?.editUrl || resolveTaskTypeEditUrl(node?.provider, node?.taskType)
}

function resolveNodeGraphId(node) {
  return node?.graphId || resolveTaskContext(node?.provider, node?.taskType, node?.taskId).graphId || null
}

function canBuildOwnerTaskUrl(rawUrl, node, graphId) {
  if (!hasValue(rawUrl) || !hasValue(node?.taskId)) {
    return false
  }
  if ((String(rawUrl).includes(':graph_id') || String(rawUrl).includes('{graph_id}')) && !hasValue(graphId)) {
    return false
  }
  return true
}

function openCurrentOwnerTask() {
  const rawUrl = currentOwnerEditUrl.value
  if (!hasValue(rawUrl)) {
    ElMessage.warning(t('orchestrator.dagEditor.ownerTaskUrlMissing'))
    return
  }
  if (!canBuildOwnerTaskUrl(rawUrl, currentNode.value, currentOwnerGraphId.value)) {
    ElMessage.warning(t('orchestrator.dagEditor.ownerTaskContextMissing'))
    return
  }

  const url = buildTaskOwnerUrl(rawUrl, {
    taskId: currentNode.value.taskId,
    graphId: currentOwnerGraphId.value
  })
  window.open(url, '_blank', 'noopener,noreferrer')
}

defineExpose({
  addTask,
  getSteps: () => {
    const data = graph.value.save()
    return convertToSteps(data)
  },
  getLayout: captureLayout,
  loadSteps
})
</script>

<style scoped>
.dag-editor {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--addp-bg-secondary) !important;
}

.toolbar {
  padding: 12px 16px;
  background: var(--addp-bg-primary) !important;
  border-bottom: 1px solid var(--addp-border-color);
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  flex-shrink: 0;
}

.toolbar-left {
  display: flex;
  gap: 12px;
  align-items: center;
}

.parameter-description {
  width: 100%;
  margin-top: 4px;
  color: var(--addp-text-secondary);
  font-size: 12px;
  line-height: 1.5;
}

.empty-parameters {
  padding: 16px 0;
}

:deep(.el-form-item .el-select) {
  width: 100%;
}

.dependency-option-name {
  float: left;
}

.dependency-option-id {
  float: right;
  max-width: 48%;
  overflow: hidden;
  color: var(--addp-text-tertiary);
  text-overflow: ellipsis;
  white-space: nowrap;
}

#dag-container {
  flex: 1;
  background: var(--addp-bg-secondary) !important;
  position: relative;
  overflow: hidden;
}

:deep(.el-alert--info) {
  padding: 8px 12px;
}

:deep(.el-alert__title) {
  font-size: 12px;
}
</style>
    recordHistory()
