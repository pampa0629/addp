<template>
  <div class="workflow-dag-canvas">
    <div class="toolbar">
      <div class="toolbar-left">
        <el-button
          :type="isAddEdgeMode ? 'primary' : 'default'"
          size="small"
          @click="handleToggleEdgeMode"
        >
          <el-icon><Connection /></el-icon> {{ isAddEdgeMode ? '退出连线' : '连线模式' }}
        </el-button>

        <el-divider direction="vertical" />

        <el-button type="danger" size="small" @click="handleDelete" :disabled="!selectedItem">
          <el-icon><Delete /></el-icon> 删除{{ selectedItem ? (selectedItem.getType && selectedItem.getType() === 'edge' ? '连线' : '节点') : '' }}
        </el-button>
        <el-button type="info" size="small" @click="handleClear">
          <el-icon><DocumentDelete /></el-icon> 清空
        </el-button>
      </div>

      <div class="toolbar-tips">
        <el-alert type="info" :closable="false" show-icon>
          <template #title>
            <span class="tips-text">
              {{ isAddEdgeMode
                ? '🔗 连线模式：点击源节点的输出端口（底部圆点），再点击目标节点'
                : '💡 从左侧拖拽算子到画布 | 点击"连线模式"建立连线 | 双击节点配置参数'
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
import { ElMessage } from 'element-plus'
import { Delete, DocumentDelete, Connection } from '@element-plus/icons-vue'
import { useDAGCore, useLoopDetection, useDAGSelection, useDAGEdgeMode, registerMultiPortNode } from '@addp/common-frontend/dag'

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
    initSelectionListener()
    graph.value.on('node:dblclick', handleNodeDoubleClick)
    graph.value.on('node:click', handleNodeClickForEdge)
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

function handleNodeClickForEdge(e) {
  if (!isAddEdgeMode.value) return

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
      ElMessage.info(`已选择输出端口: ${model.label}.${clickedPort}，请点击目标节点`)
    } else {
      ElMessage.warning('请先点击源节点的输出端口（底部圆点）')
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
      ElMessage.warning('不能连接到自己')
      clearSource()
      return
    }

    if (portType === 'input' || !portType) {
      const edgeExists = graph.value.getEdges().some(edge => {
        const em = edge.getModel()
        return em.source === sourceId && em.target === targetId
      })

      if (edgeExists) {
        ElMessage.warning('该连接已存在')
        clearSource()
        return
      }

      if (hasLoop(sourceId, targetId)) {
        ElMessage.warning('不能创建环形依赖')
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
        ElMessage.success(`已连接: ${sourceNode.getModel().label}.${sourcePort} → ${model.label}`)
      }
      clearSource()
      emitWorkflow()
    } else {
      ElMessage.warning('请点击目标节点的输入端口（顶部圆点）或节点本身')
    }
  }
}

function handleNodeDoubleClick(e) {
  const model = e.item.getModel()
  emit('node-click', {
    id: model.id,
    operator: model.operator,
    params: model.params || {},
    label: model.label
  })
}

function handleToggleEdgeMode() {
  toggleAddEdgeMode()
  if (isAddEdgeMode.value) {
    ElMessage.info('已进入连线模式，请点击源节点的输出端口')
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
    ElMessage.success('已删除')
    emitWorkflow()
  }
}

function handleClear() {
  clearGraph()
  nodeCounter.value = 0
  emitWorkflow()
  ElMessage.success('已清空画布')
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

  const outputPorts = operatorData.output_ports || [{
    name: 'default',
    type: 'geodataframe',
    description: '默认输出',
    is_default: true
  }]

  graph.value.addItem('node', {
    id: nodeId,
    label: operatorData.name,
    x: point.x,
    y: point.y,
    operator: operatorData.name,
    params: {},
    depends_on: [],
    outputPorts
  })

  emitWorkflow()
  ElMessage.success(`已添加算子: ${operatorData.name}`)
}

function loadWorkflow(workflow) {
  if (!graph.value || !workflow.tasks) return

  graph.value.clear()
  nodeCounter.value = 0

  const nodes = []
  const edges = []

  workflow.tasks.forEach((task, index) => {
    const nodeId = task.id || `${task.operator}_${index + 1}`
    nodes.push({
      id: nodeId,
      label: task.operator,
      x: 100 + (index % 3) * 200,
      y: 100 + Math.floor(index / 3) * 120,
      operator: task.operator,
      params: task.params || {},
      depends_on: task.depends_on || []
    })

    const match = nodeId.match(/_(\d+)$/)
    if (match) {
      const num = parseInt(match[1])
      if (num > nodeCounter.value) nodeCounter.value = num
    }
  })

  workflow.tasks.forEach(task => {
    if (task.depends_on && task.depends_on.length > 0) {
      task.depends_on.forEach(sourceId => {
        edges.push({
          source: sourceId,
          target: task.id,
          type: 'polyline',
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

function emitWorkflow() {
  if (!graph.value) return

  const nodes = graph.value.getNodes()
  const edges = graph.value.getEdges()

  // 构建端口引用映射
  const edgePortMap = {}
  edges.forEach(edge => {
    const model = edge.getModel()
    const { source: sourceId, target: targetId, sourcePort = 'default', targetPort = 'input' } = model

    if (!edgePortMap[targetId]) edgePortMap[targetId] = []
    edgePortMap[targetId].push({ sourceId, sourcePort, targetPort })
  })

  const tasks = nodes.map(node => {
    const model = node.getModel()
    const params = { ...(model.params || {}) }

    const inEdges = edgePortMap[model.id] || []
    inEdges.forEach(edgeInfo => {
      const ref = { "$ref": edgeInfo.sourceId }
      if (edgeInfo.sourcePort !== 'default') ref.port = edgeInfo.sourcePort
      params.input_gdf = ref
    })

    return {
      id: model.id,
      operator: model.operator,
      params,
      depends_on: inEdges.map(e => e.sourceId)
    }
  })

  emit('update:workflow', { tasks })
}

function updateNodeParams(nodeId, params) {
  const node = graph.value.findById(nodeId)
  if (node) {
    graph.value.updateItem(node, { ...node.getModel(), params: { ...params } })
    emitWorkflow()
  }
}

defineExpose({
  updateNodeParams,
  clearGraph: handleClear,
  getWorkflow: () => {
    const dependsMap = {}
    graph.value.getEdges().forEach(edge => {
      const model = edge.getModel()
      if (!dependsMap[model.target]) dependsMap[model.target] = []
      dependsMap[model.target].push(model.source)
    })

    return {
      tasks: graph.value.getNodes().map(node => {
        const model = node.getModel()
        return {
          id: model.id,
          operator: model.operator,
          params: model.params || {},
          depends_on: dependsMap[model.id] || []
        }
      })
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
