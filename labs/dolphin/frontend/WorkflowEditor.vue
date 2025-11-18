<template>
  <div class="workflow-editor">
    <h2>空间算子工作流编排</h2>

    <!-- 左侧：算子库 -->
    <div class="operator-palette">
      <h3>可用算子</h3>
      <div
        v-for="category in operatorsByCategory"
        :key="category.name"
        class="category"
      >
        <h4>{{ category.name }}</h4>
        <div
          v-for="op in category.operators"
          :key="op.code"
          class="operator-item"
          draggable="true"
          @dragstart="onDragStart(op)"
        >
          <strong>{{ op.name }}</strong>
          <p>{{ op.description }}</p>
        </div>
      </div>
    </div>

    <!-- 中间：画布 -->
    <div
      class="canvas"
      @drop="onDrop"
      @dragover.prevent
    >
      <h3>工作流画布</h3>
      <div
        v-for="node in nodes"
        :key="node.id"
        class="workflow-node"
        :style="{
          left: node.x + 'px',
          top: node.y + 'px'
        }"
        @click="selectNode(node)"
      >
        <div class="node-header">{{ node.operator.name }}</div>
        <div class="node-body">
          <div v-for="param in node.operator.params" :key="param.name">
            <label>{{ param.description }}:</label>
            <input
              v-model="node.params[param.name]"
              :type="getInputType(param.type)"
              :placeholder="param.default"
            />
          </div>
        </div>
      </div>

      <!-- 连接线绘制 -->
      <svg class="connections">
        <line
          v-for="(conn, idx) in connections"
          :key="idx"
          :x1="conn.x1"
          :y1="conn.y1"
          :x2="conn.x2"
          :y2="conn.y2"
          stroke="blue"
          stroke-width="2"
        />
      </svg>
    </div>

    <!-- 右侧：属性面板 -->
    <div class="properties-panel">
      <h3>节点属性</h3>
      <div v-if="selectedNode">
        <h4>{{ selectedNode.operator.name }}</h4>
        <div v-for="param in selectedNode.operator.params" :key="param.name">
          <label>{{ param.description }}:</label>
          <input
            v-model="selectedNode.params[param.name]"
            :type="getInputType(param.type)"
          />
        </div>
        <button @click="deleteNode">删除节点</button>
      </div>
    </div>

    <!-- 底部：操作按钮 -->
    <div class="actions">
      <el-button type="primary" @click="saveWorkflow">保存工作流</el-button>
      <el-button type="success" @click="executeWorkflow">执行工作流</el-button>
      <el-button @click="clearCanvas">清空画布</el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import axios from 'axios'

// 状态
const operators = ref([])
const nodes = ref([])
const selectedNode = ref(null)
const connections = ref([])
let nextNodeId = 1

// 按分类组织算子
const operatorsByCategory = computed(() => {
  const categories = {}
  operators.value.forEach(op => {
    if (!categories[op.category]) {
      categories[op.category] = []
    }
    categories[op.category].push(op)
  })

  return Object.keys(categories).map(name => ({
    name,
    operators: categories[name]
  }))
})

// 加载算子列表
onMounted(async () => {
  try {
    const response = await axios.get('http://localhost:8093/api/operators')
    operators.value = response.data.operators
  } catch (error) {
    console.error('Failed to load operators:', error)
  }
})

// 拖拽开始
function onDragStart(operator) {
  event.dataTransfer.setData('operator', JSON.stringify(operator))
}

// 放置到画布
function onDrop(event) {
  event.preventDefault()
  const operator = JSON.parse(event.dataTransfer.getData('operator'))

  // 计算相对画布的坐标
  const rect = event.currentTarget.getBoundingClientRect()
  const x = event.clientX - rect.left
  const y = event.clientY - rect.top

  // 创建新节点
  const newNode = {
    id: `node_${nextNodeId++}`,
    operator: operator,
    params: {},
    x: x,
    y: y,
    upstreamNodes: []
  }

  // 初始化参数默认值
  operator.params.forEach(param => {
    if (param.default !== null) {
      newNode.params[param.name] = param.default
    }
  })

  nodes.value.push(newNode)
}

// 选择节点
function selectNode(node) {
  selectedNode.value = node
}

// 删除节点
function deleteNode() {
  if (!selectedNode.value) return
  const index = nodes.value.findIndex(n => n.id === selectedNode.value.id)
  if (index > -1) {
    nodes.value.splice(index, 1)
  }
  selectedNode.value = null
}

// 清空画布
function clearCanvas() {
  nodes.value = []
  connections.value = []
  selectedNode.value = null
}

// 保存工作流
async function saveWorkflow() {
  const workflowDef = {
    project_name: 'spatial_analysis',
    workflow_name: `workflow_${Date.now()}`,
    nodes: nodes.value.map(node => ({
      node_id: node.id,
      operator_code: node.operator.code,
      params: node.params,
      upstream_nodes: node.upstreamNodes
    }))
  }

  try {
    const response = await axios.post('http://localhost:8093/api/workflows', workflowDef)
    alert('工作流保存成功！ID: ' + response.data.workflow_id)
  } catch (error) {
    alert('保存失败: ' + error.message)
  }
}

// 执行工作流
async function executeWorkflow() {
  // TODO: 调用 DolphinScheduler API 执行工作流
  alert('执行功能待实现')
}

// 根据参数类型返回输入框类型
function getInputType(paramType) {
  switch (paramType) {
    case 'float':
    case 'int':
      return 'number'
    default:
      return 'text'
  }
}
</script>

<style scoped>
.workflow-editor {
  display: flex;
  height: 100vh;
  position: relative;
}

.operator-palette {
  width: 250px;
  background: #f5f5f5;
  padding: 10px;
  overflow-y: auto;
}

.category {
  margin-bottom: 20px;
}

.operator-item {
  background: white;
  border: 1px solid #ddd;
  padding: 10px;
  margin: 5px 0;
  cursor: grab;
  border-radius: 4px;
}

.operator-item:hover {
  background: #e6f7ff;
}

.canvas {
  flex: 1;
  background: #fafafa;
  position: relative;
  overflow: auto;
}

.workflow-node {
  position: absolute;
  width: 200px;
  background: white;
  border: 2px solid #1890ff;
  border-radius: 8px;
  padding: 10px;
  cursor: move;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
}

.node-header {
  font-weight: bold;
  margin-bottom: 10px;
  padding-bottom: 5px;
  border-bottom: 1px solid #ddd;
}

.node-body label {
  display: block;
  font-size: 12px;
  margin-top: 5px;
}

.node-body input {
  width: 100%;
  font-size: 12px;
  padding: 3px;
}

.properties-panel {
  width: 300px;
  background: #f5f5f5;
  padding: 10px;
  overflow-y: auto;
}

.connections {
  position: absolute;
  top: 0;
  left: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
}

.actions {
  position: absolute;
  bottom: 20px;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  gap: 10px;
}
</style>