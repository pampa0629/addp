<template>
  <div class="dag-editor">
    <div class="toolbar">
      <div class="toolbar-left">
        <el-button
          :type="isAddEdgeMode ? 'primary' : 'default'"
          size="small"
          @click="handleToggleEdgeMode"
        >
          <el-icon><Connection /></el-icon> {{ isAddEdgeMode ? t('orchestrator.dagEditor.edgeModeOn') : t('orchestrator.dagEditor.edgeModeOff') }}
        </el-button>

        <el-divider direction="vertical" />

        <el-button type="danger" size="small" @click="handleDelete" :disabled="!selectedItem">
          <el-icon><Delete /></el-icon> {{ t('orchestrator.dagEditor.deleteBtn') }}{{ selectedItem ? (selectedItem.getType && selectedItem.getType() === 'edge' ? t('orchestrator.dagEditor.deleteEdge') : t('orchestrator.dagEditor.deleteNode')) : '' }}
        </el-button>
        <el-button type="info" size="small" @click="handleClear">
          <el-icon><DocumentDelete /></el-icon> {{ t('orchestrator.dagEditor.clearBtn') }}
        </el-button>
      </div>

      <div class="toolbar-tips">
        <el-alert type="info" :closable="false" show-icon>
          <template #title>
            <span class="tips-text">
              {{ isAddEdgeMode
                ? t('orchestrator.dagEditor.tipEdgeMode')
                : t('orchestrator.dagEditor.tipDefault')
              }}
            </span>
          </template>
        </el-alert>
      </div>
    </div>

    <div id="dag-container" ref="container" @dragover.prevent @drop="handleDrop"></div>

    <!-- 节点配置抽屉 -->
    <el-drawer v-model="drawerVisible" :title="t('orchestrator.dagEditor.drawerTitle')" size="40%">
      <el-form :model="currentNode" label-width="100px">
        <el-form-item :label="t('orchestrator.dagEditor.stepNameLabel')">
          <el-input v-model="currentNode.name" :placeholder="t('orchestrator.dagEditor.stepNamePlaceholder')"></el-input>
        </el-form-item>

        <el-form-item :label="t('orchestrator.dagEditor.executionModeLabel')">
          <el-tag v-if="currentNode.provider" type="success">{{ t('orchestrator.dagEditor.modeTaskRef') }}</el-tag>
          <el-tag v-else-if="currentNode.engineIdentifier" type="primary">{{ t('orchestrator.dagEditor.modeEngineCall') }}</el-tag>
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
        </template>

        <template v-else-if="currentNode.engineIdentifier !== undefined">
          <el-form-item :label="t('orchestrator.dagEditor.engineIdentifierLabel')">
            <el-input v-model="currentNode.engineIdentifier" :placeholder="t('orchestrator.dagEditor.engineIdentifierPlaceholder')"></el-input>
          </el-form-item>
        </template>

        <el-form-item :label="t('orchestrator.dagEditor.timeoutLabel')">
          <el-input-number v-model="currentNode.timeout" :min="0" :max="3600"></el-input-number>
        </el-form-item>

        <el-form-item :label="t('orchestrator.dagEditor.parametersLabel')">
          <el-input
            type="textarea"
            v-model="parametersStr"
            :rows="8"
            placeholder='{"key": "value"}'
          ></el-input>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" @click="saveNodeConfig">{{ t('orchestrator.dagEditor.saveConfigBtn') }}</el-button>
          <el-button @click="drawerVisible = false">{{ t('orchestrator.dagEditor.cancelBtn') }}</el-button>
        </el-form-item>
      </el-form>
    </el-drawer>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { Delete, DocumentDelete, Connection } from '@element-plus/icons-vue'
import { useDAGCore, useLoopDetection, useDAGSelection, useDAGEdgeMode, generateColor } from '@addp/common-frontend/dag'

const { t } = useI18n()

const props = defineProps({
  initialSteps: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['update:steps'])

const container = ref(null)
const drawerVisible = ref(false)
const currentNode = ref({})
const parametersStr = ref('')

// 使用 composables
const { graph, initGraph, loadData } = useDAGCore(container, {
  modes: {
    default: ['drag-canvas', 'drag-node', 'click-select'],
    addEdge: ['drag-canvas', 'click-select', 'create-edge']
  },
  defaultNode: {
    type: 'rect',
    size: [120, 50],
    anchorPoints: [[0.5, 0], [1, 0.5], [0.5, 1], [0, 0.5]],
    style: {
      fill: '#5B8FF9',
      stroke: '#1890FF',
      lineWidth: 2,
      radius: 4
    },
    labelCfg: {
      style: {
        fill: '#fff',
        fontSize: 13
      }
    },
    linkPoints: {
      top: true,
      right: true,
      bottom: true,
      left: true,
      size: 10,
      lineWidth: 2,
      fill: '#fff',
      stroke: '#1890FF'
    }
  }
})

const { hasLoop } = useLoopDetection(graph)
const { selectedItem, initSelectionListener, deleteSelected, clearGraph } = useDAGSelection(graph)
const { isAddEdgeMode, toggleAddEdgeMode } = useDAGEdgeMode(graph)

onMounted(async () => {
  initGraph()
  initSelectionListener()

  // 双击节点事件
  graph.value.on('node:dblclick', handleNodeClick)

  // 边创建后的处理
  graph.value.on('aftercreateedge', (e) => {
    const edge = e.edge
    const source = edge.getSource()
    const target = edge.getTarget()

    if (hasLoop(source.getID(), target.getID())) {
      graph.value.removeItem(edge)
      ElMessage.warning(t('orchestrator.dagEditor.loopDetected'))
      return
    }

    ElMessage.success(t('orchestrator.dagEditor.edgeCreated'))
    emitSteps()
  })

  if (props.initialSteps.length > 0) {
    loadSteps(props.initialSteps)
  }
})

function handleNodeClick(evt) {
  const model = evt.item.getModel()
  currentNode.value = { ...model }
  parametersStr.value = JSON.stringify(model.parameters || {}, null, 2)
  drawerVisible.value = true
}

function saveNodeConfig() {
  try {
    const params = JSON.parse(parametersStr.value || '{}')
    currentNode.value.parameters = params

    graph.value.updateItem(currentNode.value.id, {
      label: currentNode.value.name || currentNode.value.provider || currentNode.value.engineIdentifier,
      ...currentNode.value
    })

    drawerVisible.value = false
    ElMessage.success(t('orchestrator.dagEditor.configSaved'))
    emitSteps()
  } catch (error) {
    ElMessage.error(t('orchestrator.dagEditor.jsonError'))
  }
}

function handleToggleEdgeMode() {
  toggleAddEdgeMode()
  if (isAddEdgeMode.value) {
    ElMessage.info(t('orchestrator.dagEditor.enterEdgeMode'))
  } else {
    ElMessage.info(t('orchestrator.dagEditor.exitEdgeMode'))
  }
}

function handleDelete() {
  if (deleteSelected()) {
    const itemType = selectedItem.value?.getType ? selectedItem.value.getType() : 'edge'
    ElMessage.success(itemType === 'edge' ? t('orchestrator.dagEditor.edgeDeleted') : t('orchestrator.dagEditor.nodeDeleted'))
    emitSteps()
  }
}

function handleClear() {
  clearGraph()
  ElMessage.info(t('orchestrator.dagEditor.canvasCleared'))
  emitSteps()
}

function handleDrop(event) {
  event.preventDefault()

  try {
    const data = event.dataTransfer.getData('application/json')
    if (!data) return

    const nodeData = JSON.parse(data)
    const point = graph.value.getPointByClient(event.clientX, event.clientY)

    const width = container.value.offsetWidth || 1200
    const height = container.value.offsetHeight || 600

    let x = point.x
    let y = point.y

    if (x < 0 || x > width || y < 0 || y > height) {
      x = width / 2 + Math.random() * 200 - 100
      y = height / 2 + Math.random() * 200 - 100
    }

    const colorKey = nodeData.provider || nodeData.engineIdentifier || 'unknown'
    const color = generateColor(colorKey)
    const id = `${colorKey}-${Date.now()}`

    const nodeModel = {
      id,
      label: nodeData.name,
      name: nodeData.name,
      provider: nodeData.provider || null,
      taskType: nodeData.taskType || null,
      taskId: nodeData.taskId || null,
      engineIdentifier: nodeData.engineIdentifier || null,
      parameters: nodeData.parameters || {},
      timeout: 300,
      x,
      y,
      style: {
        fill: color,
        stroke: '#1890FF',
        lineWidth: 2
      }
    }

    graph.value.addItem('node', nodeModel)
    graph.value.paint()

    ElMessage.success(t('orchestrator.dagEditor.taskAdded', { name: nodeData.name }))
    emitSteps()
  } catch (error) {
    console.error('拖放失败:', error)
    ElMessage.error(t('orchestrator.dagEditor.addNodeFailed', { error: error.message }))
  }
}

function emitSteps() {
  const data = graph.value.save()
  const steps = convertToSteps(data)
  emit('update:steps', steps)
}

function convertToSteps(graphData) {
  const nodeMap = new Map()

  graphData.nodes.forEach(node => {
    const step = {
      id: node.id,
      name: node.label || node.provider || node.engineIdentifier || node.id,
      parameters: node.parameters || {},
      depends_on: [],
      timeout: node.timeout || 300
    }

    if (node.provider) {
      step.provider = node.provider
      step.task_type = node.taskType
      step.task_id = node.taskId
    } else if (node.engineIdentifier) {
      step.engine_identifier = node.engineIdentifier
    }

    nodeMap.set(node.id, step)
  })

  graphData.edges.forEach(edge => {
    const step = nodeMap.get(edge.target)
    if (step) {
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
    const colorKey = step.provider || step.engine_identifier || 'unknown'
    const color = generateColor(colorKey)

    nodes.push({
      id: step.id,
      label: step.name,
      name: step.name,
      provider: step.provider || null,
      taskType: step.task_type || null,
      taskId: step.task_id || null,
      engineIdentifier: step.engine_identifier || null,
      parameters: step.parameters,
      timeout: step.timeout,
      style: {
        fill: color
      }
    })

    step.depends_on?.forEach(depId => {
      edges.push({
        source: depId,
        target: step.id
      })
    })
  })

  loadData(nodes, edges)
}

defineExpose({
  getSteps: () => {
    const data = graph.value.save()
    return convertToSteps(data)
  },
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

.toolbar-tips {
  flex: 1;
  max-width: 600px;
}

.tips-text {
  font-size: 12px;
  color: var(--addp-text-secondary);
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
