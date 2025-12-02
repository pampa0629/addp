<template>
  <div class="dag-editor">
    <div class="toolbar">
      <el-button-group>
        <el-button @click="addNode('transfer')" type="primary">Transfer</el-button>
        <el-button @click="addNode('meta')" type="success">Meta</el-button>
        <el-button @click="addNode('manager')" type="warning">Manager</el-button>
      </el-button-group>
      <el-button type="danger" @click="deleteSelected" :disabled="!selectedNode">删除节点</el-button>
      <el-button type="info" @click="clearGraph">清空</el-button>
    </div>

    <div id="dag-container" ref="container"></div>

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
import { onMounted, ref, watch } from 'vue'
import G6 from '@antv/g6'
import { ElMessage } from 'element-plus'

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
  const width = container.value.offsetWidth || 1200
  const height = 600

  graph.value = new G6.Graph({
    container: container.value,
    width,
    height,
    modes: {
      default: ['drag-canvas', 'drag-node', 'click-select']
    },
    defaultNode: {
      type: 'rect',
      size: [150, 60],
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
      type: 'polyline',
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

  // Shift + 拖拽建立依赖关系
  let sourceNode = null
  graph.value.on('node:mousedown', (evt) => {
    if (evt.originalEvent.shiftKey) {
      sourceNode = evt.item
    }
  })

  graph.value.on('node:mouseup', (evt) => {
    if (sourceNode && evt.originalEvent.shiftKey && sourceNode !== evt.item) {
      const source = sourceNode.getID()
      const target = evt.item.getID()

      // 检查是否已存在边
      const edges = graph.value.getEdges()
      const exists = edges.some(edge =>
        edge.getSource().getID() === source && edge.getTarget().getID() === target
      )

      if (!exists) {
        graph.value.addItem('edge', {
          source,
          target
        })
        ElMessage.success('已建立依赖关系')
      }
      sourceNode = null
    }
  })
}

function addNode(module) {
  const id = `${module}-${Date.now()}`
  const colors = {
    transfer: '#5B8FF9',
    meta: '#5AD8A6',
    manager: '#F6BD16'
  }

  graph.value.addItem('node', {
    id,
    label: module.toUpperCase(),
    module,
    action: '',
    endpoint: '',
    method: 'POST',
    parameters: {},
    timeout: 300,
    style: {
      fill: colors[module]
    },
    x: Math.random() * 800 + 200,
    y: Math.random() * 400 + 100
  })

  ElMessage.success(`已添加 ${module} 节点`)
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
  const nodes = []
  const edges = []

  steps.forEach((step, index) => {
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
  }
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
  padding: 16px;
  background: white;
  border-bottom: 1px solid #dcdfe6;
  display: flex;
  gap: 12px;
  align-items: center;
}

#dag-container {
  flex: 1;
  background: #fafbfc;
  position: relative;
}
</style>
