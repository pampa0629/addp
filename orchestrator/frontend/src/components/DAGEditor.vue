<template>
  <div class="dag-editor">
    <div class="toolbar">
      <div class="toolbar-left">
        <!-- 引擎按钮已移除，仅保留任务库拖拽方式 -->
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
                : '💡 从左侧任务库拖拽任务到画布 | 使用连线模式建立依赖关系 | 双击节点可配置参数'
              }}
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
import { Delete, DocumentDelete, Connection } from '@element-plus/icons-vue'

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
const isAddEdgeMode = ref(false)

onMounted(async () => {
  initGraph()
  if (props.initialSteps.length > 0) {
    loadSteps(props.initialSteps)
  }
})

// Hash 字符串生成一致的颜色
function hashColor(str) {
  let hash = 0
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash)
  }

  const h = hash % 360
  const s = 70
  const l = 60

  return `hsl(${h}, ${s}%, ${l}%)`
}

// 根据 unique_identifier 生成节点颜色
function generateColor(uniqueIdentifier) {
  if (!uniqueIdentifier) return '#5B8FF9'

  // 从 unique_identifier 提取模块名（如 "meta.scanner.default" → "meta"）
  const moduleName = uniqueIdentifier.split('.')[0]

  // 为常见模块使用预定义颜色
  const predefinedColors = {
    'meta': '#5AD8A6',
    'transfer': '#5B8FF9',
    'manager': '#F6BD16',
    'develop': '#6DC8EC',
    'orchestrator': '#9270CA'
  }

  // 如果是预定义模块，使用预定义颜色
  if (predefinedColors[moduleName]) {
    return predefinedColors[moduleName]
  }

  // 否则，使用 hash 生成一致的颜色
  return hashColor(uniqueIdentifier)
}

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
      default: ['drag-canvas', 'drag-node', 'click-select'],
      // 连线模式：点击两次节点创建连线
      addEdge: ['drag-canvas', 'click-select', 'create-edge']
    },
    // 启用边的选中状态
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
      size: [120, 50],  // 调整节点大小：宽120px，高50px
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
          fontSize: 13  // 调整字体大小
        }
      },
      // 显示锚点
      linkPoints: {
        top: true,
        right: true,
        bottom: true,
        left: true,
        size: 10,  // 增大锚点尺寸，更容易点击
        lineWidth: 2,
        fill: '#fff',
        stroke: '#1890FF'
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
    }
    // 移除自动布局配置，改为手动放置节点
    // layout: {
    //   type: 'dagre',
    //   rankdir: 'LR',
    //   nodesep: 50,
    //   ranksep: 100
    // }
  })

  // 节点双击事件（配置节点）
  graph.value.on('node:dblclick', handleNodeClick)

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
    if (!data) {
      console.warn('未获取到拖放数据')
      return
    }

    const nodeData = JSON.parse(data)
    console.log('拖放节点数据:', nodeData)

    // 获取画布坐标
    const point = graph.value.getPointByClient(event.clientX, event.clientY)
    console.log('原始画布坐标:', point)

    // 确保坐标在可见范围内（修复负坐标问题）
    const width = container.value.offsetWidth || 1200
    const height = container.value.offsetHeight || 600

    // 如果坐标超出合理范围，使用随机位置在画布中心附近
    let x = point.x
    let y = point.y

    if (x < 0 || x > width || y < 0 || y > height) {
      x = width / 2 + Math.random() * 200 - 100
      y = height / 2 + Math.random() * 200 - 100
      console.log('坐标超出范围，使用中心位置:', { x, y })
    }

    // 使用 unique_identifier 生成颜色（而非硬编码 module）
    const uniqueIdentifier = nodeData.uniqueIdentifier || nodeData.module || 'unknown'
    const color = generateColor(uniqueIdentifier)

    const id = `${uniqueIdentifier}-${Date.now()}`

    const nodeModel = {
      id,
      label: nodeData.name,
      uniqueIdentifier,  // 新增：存储 unique_identifier
      module: nodeData.module,  // 保留向后兼容
      taskId: nodeData.taskId,
      name: nodeData.name,
      action: 'execute',
      endpoint: nodeData.endpoint || '',
      method: nodeData.method || 'POST',
      parameters: nodeData.parameters || {},
      timeout: 300,
      x,
      y,
      style: {
        fill: color,  // 使用动态生成的颜色
        stroke: '#1890FF',
        lineWidth: 2
      }
    }

    console.log('添加节点模型:', nodeModel)

    // 添加节点
    const node = graph.value.addItem('node', nodeModel)
    console.log('节点已添加，node ID:', node.getID())

    // 重绘画布
    graph.value.paint()
    console.log('画布已重绘')

    ElMessage.success(`已添加任务: ${nodeData.name}`)
    emitSteps()
  } catch (error) {
    console.error('拖放失败:', error)
    ElMessage.error('添加节点失败: ' + error.message)
  }
}

function addNode(resource) {
  if (!graph.value) {
    ElMessage.error('画布未初始化')
    return
  }

  const uniqueIdentifier = resource.module_name || resource.engine_type || 'unknown'
  const id = `${uniqueIdentifier}-${Date.now()}`
  const color = generateColor(uniqueIdentifier)

  // 获取画布中心位置
  const width = container.value.offsetWidth || 1200
  const height = container.value.offsetHeight || 600
  const centerX = width / 2 + Math.random() * 100 - 50
  const centerY = height / 2 + Math.random() * 100 - 50

  const nodeModel = {
    id,
    label: resource.name,
    uniqueIdentifier,  // 新增：存储 unique_identifier
    module: resource.module_name || resource.engine_type?.split('.')[0] || 'unknown',  // 兼容旧字段
    name: '',
    action: '',
    endpoint: '',
    method: 'POST',
    parameters: {},
    timeout: 300,
    style: {
      fill: color,
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

  ElMessage.success(`已添加 ${resource.name} 节点,点击节点可配置`)

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
    const itemType = selectedNode.value.getType ? selectedNode.value.getType() : 'edge'

    graph.value.removeItem(selectedNode.value)
    selectedNode.value = null

    if (itemType === 'edge') {
      ElMessage.success('连线已删除')
    } else {
      ElMessage.success('节点已删除')
    }

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
    // 使用 uniqueIdentifier 或回退到 module 生成颜色
    const uniqueIdentifier = step.uniqueIdentifier || step.module || 'unknown'
    const color = generateColor(uniqueIdentifier)

    nodes.push({
      id: step.id,
      label: step.name,
      uniqueIdentifier: step.uniqueIdentifier,  // 新增字段
      module: step.module,
      action: step.action,
      endpoint: step.endpoint,
      method: step.method,
      parameters: step.parameters,
      timeout: step.timeout,
      style: {
        fill: color  // 使用动态生成的颜色
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
}

function toggleAddEdgeMode() {
  isAddEdgeMode.value = !isAddEdgeMode.value

  if (isAddEdgeMode.value) {
    // 切换到连线模式
    graph.value.setMode('addEdge')
    ElMessage.info('已进入连线模式，依次点击两个节点建立连线')
  } else {
    // 切换回默认模式
    graph.value.setMode('default')
    ElMessage.info('已退出连线模式')
  }
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
