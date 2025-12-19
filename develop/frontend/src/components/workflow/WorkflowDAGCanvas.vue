<template>
  <div class="workflow-dag-canvas">
    <div class="toolbar">
      <div class="toolbar-left">
        <el-button
          :type="isAddEdgeMode ? 'primary' : 'default'"
          size="small"
          @click="toggleAddEdgeMode"
        >
          <el-icon><Connection /></el-icon> {{ isAddEdgeMode ? '退出连线' : '连线模式' }}
        </el-button>

        <el-divider direction="vertical" />

        <el-button type="danger" size="small" @click="deleteSelected" :disabled="!selectedNode">
          <el-icon><Delete /></el-icon> 删除{{ selectedNode ? (selectedNode.getType && selectedNode.getType() === 'edge' ? '连线' : '节点') : '' }}
        </el-button>
        <el-button type="info" size="small" @click="clearGraph">
          <el-icon><DocumentDelete /></el-icon> 清空
        </el-button>
      </div>

      <div class="toolbar-tips">
        <el-alert type="info" :closable="false" show-icon>
          <template #title>
            <span class="tips-text">
              {{ isAddEdgeMode
                ? '🔗 连线模式：依次点击两个节点建立连线'
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
import G6 from '@antv/g6'
import { ElMessage } from 'element-plus'
import { Delete, DocumentDelete, Connection } from '@element-plus/icons-vue'

const props = defineProps({
  initialWorkflow: {
    type: Object,
    default: () => ({ tasks: [] })
  }
})

const emit = defineEmits(['update:workflow', 'node-click'])

const container = ref(null)
const graph = ref(null)
const selectedNode = ref(null)
const isAddEdgeMode = ref(false)
const nodeCounter = ref(0) // 节点计数器，用于生成唯一ID
const edgeSourceNode = ref(null) // 连线模式下的源节点

onMounted(() => {
  initGraph()
  if (props.initialWorkflow?.tasks?.length > 0) {
    loadWorkflow(props.initialWorkflow)
  }
})

// 监听 initialWorkflow 变化
watch(() => props.initialWorkflow, (newWorkflow) => {
  if (newWorkflow?.tasks) {
    loadWorkflow(newWorkflow)
  }
}, { deep: true })

function initGraph() {
  if (!container.value) {
    console.error('容器未找到')
    return
  }

  const width = container.value.offsetWidth || 1200
  const height = container.value.offsetHeight || 600

  graph.value = new G6.Graph({
    container: container.value,
    width,
    height,
    modes: {
      default: ['drag-canvas', 'drag-node', 'click-select'],
      addEdge: ['drag-canvas']
    },
    nodeStateStyles: {
      selected: {
        stroke: '#f00',
        lineWidth: 3
      }
    },
    edgeStateStyles: {
      selected: {
        stroke: '#f00',
        lineWidth: 3
      },
      hover: {
        stroke: '#1890ff',
        lineWidth: 3
      }
    },
    defaultNode: {
      type: 'rect',
      size: [120, 50],
      anchorPoints: [
        [0.5, 0],    // 上
        [1, 0.5],    // 右
        [0.5, 1],    // 下
        [0, 0.5]     // 左
      ],
      style: {
        fill: '#6DC8EC',  // 工作流专属浅蓝色
        stroke: '#5DADE2',
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
        stroke: '#5DADE2'
      }
    },
    defaultEdge: {
      type: 'polyline',
      style: {
        stroke: '#A3B1BF',
        lineWidth: 2,
        radius: 10,
        endArrow: {
          path: G6.Arrow.triangle(10, 12, 0),
          fill: '#A3B1BF',
          d: 0
        }
      },
      labelCfg: {
        autoRotate: true
      }
    }
  })

  // 节点双击事件（配置参数）
  graph.value.on('node:dblclick', handleNodeDoubleClick)

  // 节点选中事件
  graph.value.on('nodeselectchange', (evt) => {
    const selectedItems = evt.selectedItems
    if (selectedItems.nodes && selectedItems.nodes.length > 0) {
      selectedNode.value = selectedItems.nodes[0]
    } else if (selectedItems.edges && selectedItems.edges.length > 0) {
      selectedNode.value = selectedItems.edges[0]
    } else {
      selectedNode.value = null
    }
  })

  // 节点点击事件 - 用于连线模式
  graph.value.on('node:click', (e) => {
    if (!isAddEdgeMode.value) return

    const clickedNode = e.item
    const model = clickedNode.getModel()

    if (!edgeSourceNode.value) {
      // 第一次点击，选择源节点
      edgeSourceNode.value = model.id
      // 高亮源节点
      graph.value.setItemState(clickedNode, 'selected', true)
      ElMessage.info(`已选择源节点: ${model.label}，请点击目标节点`)
    } else {
      // 第二次点击，创建边
      const sourceId = edgeSourceNode.value
      const targetId = model.id

      // 不能连接自己
      if (sourceId === targetId) {
        ElMessage.warning('不能连接到自己')
        // 清除源节点高亮
        const sourceNode = graph.value.findById(sourceId)
        if (sourceNode) {
          graph.value.setItemState(sourceNode, 'selected', false)
        }
        edgeSourceNode.value = null
        return
      }

      // 检查是否已存在该边
      const edges = graph.value.getEdges()
      const edgeExists = edges.some(edge => {
        const edgeModel = edge.getModel()
        return edgeModel.source === sourceId && edgeModel.target === targetId
      })

      if (edgeExists) {
        ElMessage.warning('该连接已存在')
        // 清除源节点高亮
        const sourceNode = graph.value.findById(sourceId)
        if (sourceNode) {
          graph.value.setItemState(sourceNode, 'selected', false)
        }
        edgeSourceNode.value = null
        return
      }

      // 检查是否会形成环
      if (hasLoop(sourceId, targetId)) {
        ElMessage.warning('不能创建环形依赖')
        // 清除源节点高亮
        const sourceNode = graph.value.findById(sourceId)
        if (sourceNode) {
          graph.value.setItemState(sourceNode, 'selected', false)
        }
        edgeSourceNode.value = null
        return
      }

      // 创建边
      graph.value.addItem('edge', {
        source: sourceId,
        target: targetId,
        type: 'polyline',
        style: {
          stroke: '#A3B1BF',
          lineWidth: 2,
          radius: 10,
          endArrow: {
            path: G6.Arrow.triangle(10, 12, 0),
            fill: '#A3B1BF',
            d: 0
          }
        }
      })

      // 清除源节点高亮
      const sourceNode = graph.value.findById(sourceId)
      if (sourceNode) {
        graph.value.setItemState(sourceNode, 'selected', false)
      }

      edgeSourceNode.value = null
      ElMessage.success('已建立依赖关系')
      emitWorkflow()
    }
  })

  // 节点/边删除后
  graph.value.on('afterremoveitem', () => {
    emitWorkflow()
  })

  // 节点位置变化后
  graph.value.on('afterupdateitem', () => {
    emitWorkflow()
  })
}

// 检测环的辅助函数
function hasLoop(sourceId, targetId) {
  const edges = graph.value.getEdges()
  const visited = new Set()
  const stack = [targetId]

  while (stack.length > 0) {
    const current = stack.pop()
    if (current === sourceId) return true
    if (visited.has(current)) continue

    visited.add(current)

    edges.forEach(edge => {
      // 获取边的模型数据，其中包含 source 和 target 的 ID
      const model = edge.getModel()
      const edgeSourceId = model.source
      const edgeTargetId = model.target

      if (edgeSourceId === current) {
        stack.push(edgeTargetId)
      }
    })
  }

  return false
}

// 处理拖放
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

  // 计算画布坐标（考虑画布的缩放和平移）
  const point = graph.value.getPointByClient(event.clientX, event.clientY)

  // 生成唯一节点ID
  nodeCounter.value++
  const nodeId = `${operatorData.name}_${nodeCounter.value}`

  // 添加节点
  graph.value.addItem('node', {
    id: nodeId,
    label: operatorData.name,
    x: point.x,
    y: point.y,
    operator: operatorData.name,
    params: {},
    depends_on: []
  })

  emitWorkflow()
  ElMessage.success(`已添加算子: ${operatorData.name}`)
}

// 节点双击事件
function handleNodeDoubleClick(e) {
  const node = e.item
  const model = node.getModel()

  // 触发父组件的节点点击事件（打开参数面板）
  emit('node-click', {
    id: model.id,
    operator: model.operator,
    params: model.params || {},
    label: model.label
  })
}

// 切换连线模式
function toggleAddEdgeMode() {
  isAddEdgeMode.value = !isAddEdgeMode.value

  if (isAddEdgeMode.value) {
    graph.value.setMode('addEdge')
    ElMessage.info('已进入连线模式，请依次点击两个节点')
  } else {
    graph.value.setMode('default')

    // 退出连线模式时，清除源节点选择和高亮
    if (edgeSourceNode.value) {
      const sourceNode = graph.value.findById(edgeSourceNode.value)
      if (sourceNode) {
        graph.value.setItemState(sourceNode, 'selected', false)
      }
      edgeSourceNode.value = null
    }
  }
}

// 删除选中的节点/边
function deleteSelected() {
  if (!selectedNode.value) return

  graph.value.removeItem(selectedNode.value)
  selectedNode.value = null
  emitWorkflow()
  ElMessage.success('已删除')
}

// 清空画布
function clearGraph() {
  graph.value.clear()
  nodeCounter.value = 0
  emitWorkflow()
  ElMessage.success('已清空画布')
}

// 加载工作流
function loadWorkflow(workflow) {
  if (!graph.value || !workflow.tasks) return

  graph.value.clear()
  nodeCounter.value = 0

  const nodes = []
  const edges = []

  // 创建节点
  workflow.tasks.forEach((task, index) => {
    const nodeId = task.id || `${task.operator}_${index + 1}`
    nodes.push({
      id: nodeId,
      label: task.operator,
      x: 100 + (index % 3) * 200, // 简单布局
      y: 100 + Math.floor(index / 3) * 120,
      operator: task.operator,
      params: task.params || {},
      depends_on: task.depends_on || []
    })

    // 更新计数器
    const match = nodeId.match(/_(\d+)$/)
    if (match) {
      const num = parseInt(match[1])
      if (num > nodeCounter.value) {
        nodeCounter.value = num
      }
    }
  })

  // 创建边
  workflow.tasks.forEach(task => {
    const targetId = task.id
    if (task.depends_on && task.depends_on.length > 0) {
      task.depends_on.forEach(sourceId => {
        edges.push({
          source: sourceId,
          target: targetId,
          type: 'polyline',
          style: {
            stroke: '#A3B1BF',
            lineWidth: 2,
            radius: 10,
            endArrow: {
              path: G6.Arrow.triangle(10, 12, 0),
              fill: '#A3B1BF',
              d: 0
            }
          }
        })
      })
    }
  })

  graph.value.data({ nodes, edges })
  graph.value.render()
}

// 导出工作流
function emitWorkflow() {
  if (!graph.value) return

  const nodes = graph.value.getNodes()
  const edges = graph.value.getEdges()

  // 构建 depends_on 关系
  const dependsMap = {}
  edges.forEach(edge => {
    // 获取边的模型数据，其中包含 source 和 target 的 ID
    const model = edge.getModel()
    const sourceId = model.source
    const targetId = model.target

    if (!dependsMap[targetId]) {
      dependsMap[targetId] = []
    }
    dependsMap[targetId].push(sourceId)
  })

  // 构建 tasks 数组
  const tasks = nodes.map(node => {
    const model = node.getModel()
    return {
      id: model.id,
      operator: model.operator,
      params: model.params || {},
      depends_on: dependsMap[model.id] || []
    }
  })

  emit('update:workflow', { tasks })
}

// 更新节点参数（由父组件调用）
function updateNodeParams(nodeId, params) {
  const node = graph.value.findById(nodeId)
  if (node) {
    const model = node.getModel()
    model.params = params
    graph.value.updateItem(node, model)
    emitWorkflow()
  }
}

// 暴露方法给父组件
defineExpose({
  updateNodeParams,
  getWorkflow: () => {
    // 返回当前工作流
    const nodes = graph.value.getNodes()
    const edges = graph.value.getEdges()

    const dependsMap = {}
    edges.forEach(edge => {
      // 获取边的模型数据，其中包含 source 和 target 的 ID
      const model = edge.getModel()
      const sourceId = model.source
      const targetId = model.target

      if (!dependsMap[targetId]) {
        dependsMap[targetId] = []
      }
      dependsMap[targetId].push(sourceId)
    })

    const tasks = nodes.map(node => {
      const model = node.getModel()
      return {
        id: model.id,
        operator: model.operator,
        params: model.params || {},
        depends_on: dependsMap[model.id] || []
      }
    })

    return { tasks }
  }
})
</script>

<style scoped>
.workflow-dag-canvas {
  height: 100%;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.toolbar {
  padding: 12px 16px;
  background: #f5f7fa;
  border-bottom: 1px solid #e4e7ed;
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
  background: #fff;
  overflow: hidden;
  position: relative;
}

:deep(.g6-tooltip) {
  background: rgba(0, 0, 0, 0.75);
  color: #fff;
  padding: 8px 12px;
  border-radius: 4px;
  font-size: 12px;
}
</style>
