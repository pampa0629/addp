<template>
  <div class="dag-editor">
    <div class="toolbar">
      <div class="toolbar-left">
        <el-button-group>
          <el-button @click="addNode('transfer')" type="primary" size="small">
            <el-icon><Plus /></el-icon> Transfer
          </el-button>
          <el-button @click="addNode('meta')" type="success" size="small">
            <el-icon><Plus /></el-icon> Meta
          </el-button>
          <el-button @click="addNode('manager')" type="warning" size="small">
            <el-icon><Plus /></el-icon> Manager
          </el-button>
        </el-button-group>

        <el-divider direction="vertical" />

        <el-button type="danger" size="small" @click="deleteSelected" :disabled="!selectedNode">
          <el-icon><Delete /></el-icon> 删除
        </el-button>
        <el-button type="info" size="small" @click="clearGraph">
          <el-icon><DocumentDelete /></el-icon> 清空
        </el-button>
      </div>

      <div class="toolbar-tips">
        <el-alert type="info" :closable="false" show-icon>
          <template #title>
            <span class="tips-text">
              💡 从左侧拖拽任务 | 从节点拖出连线建立依赖 | 点击节点配置
            </span>
          </template>
        </el-alert>
      </div>
    </div>

    <div id="dag-container" ref="container" @dragover.prevent @drop="handleDrop"></div>

    <!-- 节点配置抽屉 -->
    <el-drawer v-model="drawerVisible" title="配置步骤" size="40%">
      <el-form :model="currentNode" label-width="100px">
        <el-form-item label="步骤名称">
          <el-input v-model="currentNode.name" placeholder="例如: 数据传输"></el-input>
        </el-form-item>

        <el-form-item label="模块">
          <el-input v-model="currentNode.module" disabled></el-input>
        </el-form-item>

        <el-form-item label="动作">
          <el-input v-model="currentNode.action" placeholder="例如: execute, scan, cache"></el-input>
        </el-form-item>

        <el-form-item label="API 端点">
          <el-input v-model="currentNode.endpoint" placeholder="例如: /api/tasks/:id/execute"></el-input>
        </el-form-item>

        <el-form-item label="HTTP 方法">
          <el-select v-model="currentNode.method">
            <el-option label="POST" value="POST"></el-option>
            <el-option label="GET" value="GET"></el-option>
            <el-option label="PUT" value="PUT"></el-option>
          </el-select>
        </el-form-item>

        <el-form-item label="超时(秒)">
          <el-input-number v-model="currentNode.timeout" :min="0" :max="3600"></el-input-number>
        </el-form-item>

        <el-form-item label="参数 (JSON)">
          <el-input
            type="textarea"
            v-model="parametersStr"
            :rows="8"
            placeholder='{"key": "value"}'
          ></el-input>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" @click="saveNodeConfig">保存</el-button>
          <el-button @click="drawerVisible = false">取消</el-button>
        </el-form-item>
      </el-form>
    </el-drawer>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import G6 from '@antv/g6'
import { ElMessage } from 'element-plus'
import { Plus, Delete, DocumentDelete } from '@element-plus/icons-vue'

const props = defineProps({
  initialSteps: {
    type: Array,
    default: () => []
  }
})

const emit = defineEmits(['update:steps'])

const container = ref(null)
const graph = ref(null)
const drawerVisible = ref(false)
const currentNode = ref({})
const parametersStr = ref('')
const selectedNode = ref(null)

onMounted(() => {
  initGraph()
  if (props.initialSteps.length > 0) {
    loadSteps(props.initialSteps)
  }
})

function initGraph() {
  if (!container.value) {
    console.error('容器未找到')
    return
  }

  const width = container.value.offsetWidth || 1200
  const height = container.value.offsetHeight || 600

  console.log('初始化 G6 画布:', { width, height })

  graph.value = new G6.Graph({
    container: container.value,
    width,
    height,
    modes: {
      default: [
        'drag-canvas',
        'drag-node',
        'click-select',
        {
          type: 'create-edge',  // 启用创建边模式
          trigger: 'drag',      // 拖拽触发
          key: undefined,       // 不需要按键
          shouldBegin(e) {
            // 只允许从节点开始拖拽
            return e.item && e.item.getType() === 'node'
          },
          shouldEnd(e) {
            // 只允许拖到节点上结束
            return e.item && e.item.getType() === 'node'
          }
        }
      ]
    },
    defaultNode: {
      type: 'rect',
      size: [150, 60],
      // 添加锚点配置
      anchorPoints: [
        [0.5, 0],    // 上
        [1, 0.5],    // 右
        [0.5, 1],    // 下
        [0, 0.5]     // 左
      ],
      style: {
        fill: '#5B8FF9',
        stroke: '#1890FF',
        lineWidth: 2,
        radius: 4
      },
      labelCfg: {
        style: {
          fill: '#fff',
          fontSize: 14
        }
      }
    },
    defaultEdge: {
      type: 'polyline',  // 折线,自动避障
      style: {
        stroke: '#A3B1BF',
        lineWidth: 2,
        endArrow: {
          path: 'M 0,0 L 8,4 L 8,-4 Z',
          fill: '#A3B1BF'
        }
      }
    },
    layout: {
      type: 'dagre',
      rankdir: 'LR',
      nodesep: 50,
      ranksep: 100
    }
  })

  // 节点点击事件
  graph.value.on('node:click', handleNodeClick)

  // 节点选中事件
  graph.value.on('nodeselectchange', (evt) => {
    const selectedItems = evt.selectedItems
    if (selectedItems.nodes && selectedItems.nodes.length > 0) {
      selectedNode.value = selectedItems.nodes[0]
    } else {
      selectedNode.value = null
    }
  })

  // 边创建后的处理
  graph.value.on('aftercreateedge', (e) => {
    const edge = e.edge
    const source = edge.getSource()
    const target = edge.getTarget()

    // 检查是否会形成环
    if (hasLoop(source.getID(), target.getID())) {
      graph.value.removeItem(edge)
      ElMessage.warning('不能创建环形依赖')
      return
    }

    ElMessage.success('已建立依赖关系')
    emitSteps()
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
      if (edge.getSource().getID() === current) {
        stack.push(edge.getTarget().getID())
      }
    })
  }

  return false
}

// 处理从任务面板拖放
function handleDrop(event) {
  event.preventDefault()

  try {
    const data = event.dataTransfer.getData('application/json')
    if (!data) return

    const nodeData = JSON.parse(data)

    // 获取画布坐标
    const point = graph.value.getPointByClient(event.clientX, event.clientY)

    // 创建节点
    const colors = {
      transfer: '#5B8FF9',
      meta: '#5AD8A6',
      manager: '#F6BD16'
    }

    const id = `${nodeData.module}-${Date.now()}`

    graph.value.addItem('node', {
      id,
      label: nodeData.name,
      module: nodeData.module,
      taskId: nodeData.taskId,
      name: nodeData.name,
      action: 'execute',
      endpoint: nodeData.endpoint || '',
      method: nodeData.method || 'POST',
      parameters: nodeData.parameters || {},
      timeout: 300,
      x: point.x,
      y: point.y,
      style: {
        fill: colors[nodeData.module]
      }
    })

    ElMessage.success(`已添加任务: ${nodeData.name}`)
    emitSteps()
  } catch (error) {
    console.error('拖放失败:', error)
  }
}

function addNode(module) {
  if (!graph.value) {
    ElMessage.error('画布未初始化')
    return
  }

  const id = `${module}-${Date.now()}`
  const colors = {
    transfer: '#5B8FF9',
    meta: '#5AD8A6',
    manager: '#F6BD16'
  }

  // 获取画布中心位置
  const width = container.value.offsetWidth || 1200
  const height = container.value.offsetHeight || 600
  const centerX = width / 2 + Math.random() * 100 - 50
  const centerY = height / 2 + Math.random() * 100 - 50

  const nodeModel = {
    id,
    label: module.toUpperCase(),
    module,
    name: '',
    action: '',
    endpoint: '',
    method: 'POST',
    parameters: {},
    timeout: 300,
    style: {
      fill: colors[module],
      stroke: '#1890FF',
      lineWidth: 2
    },
    x: centerX,
    y: centerY
  }

  // 添加节点到图中
  const node = graph.value.addItem('node', nodeModel)

  // 确保节点可见
  graph.value.paint()
  graph.value.setItemState(node, 'selected', true)

  ElMessage.success(`已添加 ${module} 节点,点击节点可配置`)

  // 延迟打开配置抽屉,确保节点已经渲染
  setTimeout(() => {
    currentNode.value = { ...nodeModel }
    parametersStr.value = '{}'
    drawerVisible.value = true
  }, 100)

  emitSteps()
}

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
      label: currentNode.value.name || currentNode.value.module.toUpperCase(),
      ...currentNode.value
    })

    drawerVisible.value = false
    ElMessage.success('配置已保存')
    emitSteps()
  } catch (error) {
    ElMessage.error('参数 JSON 格式错误')
  }
}

function deleteSelected() {
  if (selectedNode.value) {
    graph.value.removeItem(selectedNode.value)
    selectedNode.value = null
    ElMessage.success('节点已删除')
    emitSteps()
  }
}

function clearGraph() {
  graph.value.clear()
  ElMessage.info('画布已清空')
  emitSteps()
}

function emitSteps() {
  const data = graph.value.save()
  const steps = convertToSteps(data)
  emit('update:steps', steps)
}

function convertToSteps(graphData) {
  const steps = []
  const nodeMap = new Map()

  graphData.nodes.forEach(node => {
    nodeMap.set(node.id, {
      id: node.id,
      name: node.label || node.module,
      module: node.module,
      action: node.action || '',
      endpoint: node.endpoint || '',
      method: node.method || 'POST',
      parameters: node.parameters || {},
      depends_on: [],
      timeout: node.timeout || 300
    })
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
    const colors = {
      transfer: '#5B8FF9',
      meta: '#5AD8A6',
      manager: '#F6BD16'
    }

    nodes.push({
      id: step.id,
      label: step.name,
      module: step.module,
      action: step.action,
      endpoint: step.endpoint,
      method: step.method,
      parameters: step.parameters,
      timeout: step.timeout,
      style: {
        fill: colors[step.module] || '#5B8FF9'
      }
    })

    step.depends_on?.forEach(depId => {
      edges.push({
        source: depId,
        target: step.id
      })
    })
  })

  graph.value.data({ nodes, edges })
  graph.value.render()
  graph.value.fitView()
}

defineExpose({
  getSteps: () => {
    const data = graph.value.save()
    return convertToSteps(data)
  },
  loadSteps  // 暴露 loadSteps 方法供父组件调用
})
</script>

<style scoped>
.dag-editor {
  display: flex;
  flex-direction: column;
  height: 100%;
  background: #f5f7fa;
}

.toolbar {
  padding: 12px 16px;
  background: white;
  border-bottom: 1px solid #dcdfe6;
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
  color: #606266;
}

#dag-container {
  flex: 1;
  background: #fafbfc;
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
