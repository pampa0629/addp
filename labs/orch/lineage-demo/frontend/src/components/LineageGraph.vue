<template>
  <div class="lineage-graph">
    <div class="toolbar">
      <el-button @click="zoomIn" :icon="ZoomIn" circle />
      <el-button @click="zoomOut" :icon="ZoomOut" circle />
      <el-button @click="fitView" :icon="FullScreen" circle />
      <el-button @click="resetGraph" :icon="Refresh" circle />
    </div>
    <div ref="container" class="graph-container"></div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue'
import G6 from '@antv/g6'
import { ZoomIn, ZoomOut, FullScreen, Refresh } from '@element-plus/icons-vue'

const props = defineProps({
  data: {
    type: Object,
    required: true
  }
})

const emit = defineEmits(['nodeClick'])

const container = ref(null)
let graph = null

// 节点样式配置
const nodeStateStyles = {
  hover: {
    fill: '#d3f8ff',
    stroke: '#1890ff',
    lineWidth: 3
  },
  selected: {
    fill: '#ffd8b8',
    stroke: '#ff7a00',
    lineWidth: 3
  }
}

// 初始化图谱
const initGraph = () => {
  if (!container.value) return

  // 节点类型颜色映射
  const nodeTypeColors = {
    'source_table': '#5B8FF9',    // 源表 - 蓝色
    'staging_table': '#61DDAA',   // 中间表 - 绿色
    'target_table': '#F6BD16',    // 目标表 - 黄色
    'view': '#7262FD',            // 视图 - 紫色
    'api': '#78D3F8',             // API - 浅蓝
    'file': '#9661BC'             // 文件 - 深紫
  }

  graph = new G6.Graph({
    container: container.value,
    width: container.value.offsetWidth,
    height: container.value.offsetHeight,
    modes: {
      default: ['drag-canvas', 'zoom-canvas', 'drag-node']
    },
    layout: {
      type: 'dagre',
      rankdir: 'LR',  // 从左到右布局
      align: 'UL',
      nodesep: 80,
      ranksep: 120
    },
    defaultNode: {
      type: 'rect',
      size: [180, 60],
      style: {
        radius: 8,
        stroke: '#5B8FF9',
        lineWidth: 2,
        fill: '#C6E5FF'
      },
      labelCfg: {
        style: {
          fill: '#000',
          fontSize: 14,
          fontWeight: 'bold'
        }
      }
    },
    defaultEdge: {
      type: 'polyline',
      style: {
        radius: 10,
        offset: 20,
        endArrow: {
          path: G6.Arrow.triangle(10, 12, 5),
          d: 5,
          fill: '#999'
        },
        stroke: '#999',
        lineWidth: 2
      },
      labelCfg: {
        autoRotate: true,
        style: {
          fill: '#666',
          fontSize: 12,
          background: {
            fill: '#ffffff',
            padding: [2, 4, 2, 4],
            radius: 4
          }
        }
      }
    },
    nodeStateStyles
  })

  // 处理节点数据，添加颜色
  const processedData = {
    nodes: props.data.nodes.map(node => ({
      id: node.id,
      label: node.name,
      style: {
        fill: nodeTypeColors[node.type] || '#C6E5FF',
        stroke: nodeTypeColors[node.type] || '#5B8FF9'
      },
      description: `${node.type}\n${node.schema || ''}`
    })),
    edges: props.data.edges.map(edge => ({
      source: edge.source,
      target: edge.target,
      label: edge.transform_type || ''
    }))
  }

  graph.data(processedData)
  graph.render()

  // 节点点击事件
  graph.on('node:click', (e) => {
    const node = e.item
    const model = node.getModel()

    // 清除其他节点的选中状态
    graph.getNodes().forEach(n => {
      graph.clearItemStates(n)
    })

    // 设置当前节点为选中状态
    graph.setItemState(node, 'selected', true)

    emit('nodeClick', model)
  })

  // 节点hover事件
  graph.on('node:mouseenter', (e) => {
    const node = e.item
    graph.setItemState(node, 'hover', true)
  })

  graph.on('node:mouseleave', (e) => {
    const node = e.item
    graph.setItemState(node, 'hover', false)
  })

  // 自适应画布
  graph.fitView(20)
}

// 工具栏功能
const zoomIn = () => {
  const currentZoom = graph.getZoom()
  graph.zoomTo(currentZoom * 1.2)
}

const zoomOut = () => {
  const currentZoom = graph.getZoom()
  graph.zoomTo(currentZoom * 0.8)
}

const fitView = () => {
  graph.fitView(20)
}

const resetGraph = () => {
  graph.clear()
  initGraph()
}

// 监听数据变化
watch(() => props.data, () => {
  if (graph) {
    resetGraph()
  }
}, { deep: true })

onMounted(() => {
  initGraph()

  // 窗口大小变化时重新渲染
  window.addEventListener('resize', () => {
    if (graph && container.value) {
      graph.changeSize(container.value.offsetWidth, container.value.offsetHeight)
      graph.fitView(20)
    }
  })
})

onUnmounted(() => {
  if (graph) {
    graph.destroy()
  }
})
</script>

<style scoped>
.lineage-graph {
  position: relative;
  width: 100%;
  height: 100%;
  background: #f5f5f5;
  border-radius: 8px;
}

.toolbar {
  position: absolute;
  top: 20px;
  right: 20px;
  z-index: 10;
  display: flex;
  gap: 8px;
  background: white;
  padding: 8px;
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0,0,0,0.1);
}

.graph-container {
  width: 100%;
  height: 100%;
  border-radius: 8px;
}
</style>
