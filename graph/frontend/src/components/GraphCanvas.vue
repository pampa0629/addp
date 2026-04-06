<template>
  <div ref="containerRef" class="graph-canvas" v-loading="loading">
    <div v-if="!loading && isEmpty" class="empty-hint">
      <el-empty description="暂无图数据，请尝试搜索或展开节点" />
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, watch, computed, nextTick } from 'vue'
import G6 from '@antv/g6'

const props = defineProps({
  nodes: { type: Array, default: () => [] },
  edges: { type: Array, default: () => [] },
  layout: { type: String, default: 'force' },
  loading: { type: Boolean, default: false },
  centerNodeId: { type: String, default: '' }
})

const emit = defineEmits(['node-click', 'node-select', 'edge-select', 'canvas-click'])

const containerRef = ref(null)
let graphInstance = null
let pendingFocusIds = null  // 搜索后待定位的节点 ID 列表

// 社区算法12色高对比度色板
const COMMUNITY_COLORS = [
  '#5B8FF9', '#61DDAA', '#F6BD16', '#E8684A',
  '#9270CA', '#FF99C3', '#6DC8EC', '#7CEFA0',
  '#F6903D', '#B9C0CA', '#D81E00', '#1DB446'
]

const isEmpty = computed(() => props.nodes.length === 0 && !props.loading)

function isDarkTheme() {
  const cl = document.documentElement.classList
  return cl.contains('dark') || cl.contains('blue') || cl.contains('purple')
}

function buildG6Data(nodes, edges) {
  const dark = isDarkTheme()
  const edgeColor = dark ? '#6b7280' : '#c0c4cc'
  const edgeLabelColor = dark ? '#d1d5db' : '#333'
  const edgeLabelStroke = dark ? '#1D1E1F' : '#fff'
  return {
    nodes: nodes.map(n => {
      const isCenter = props.centerNodeId && n.id === props.centerNodeId
      return {
        id: n.id,
        label: (n.display_name || n.id).toString().substring(0, 20),
        _meta: n,
        style: {
          fill: n.color || '#5B8FF9',
          stroke: isCenter ? '#f59e0b' : (dark ? '#374151' : '#fff'),
          lineWidth: isCenter ? 4 : 2,
          r: isCenter ? 34 : 28
        },
        size: isCenter ? 68 : 56,
        labelCfg: { style: { fill: '#fff', fontSize: 11, fontWeight: 'bold' } }
      }
    }),
    edges: edges.map((e, i) => ({
      id: e.id || `edge-${i}`,
      source: e.source,
      target: e.target,
      label: e.type || '',
      style: {
        stroke: e.color || edgeColor,
        lineWidth: 1.5,
        endArrow: { path: G6.Arrow.triangle(8, 6, 0), fill: e.color || edgeColor }
      },
      labelCfg: { style: { fill: edgeLabelColor, fontSize: 11, fontWeight: '600', stroke: edgeLabelStroke, lineWidth: 3 }, autoRotate: true }
    }))
  }
}

function getLayoutConfig(layoutType) {
  switch (layoutType) {
    case 'dagre':
      return { type: 'dagre', rankdir: 'LR', nodesep: 50, ranksep: 80 }
    case 'circular':
      return { type: 'circular', radius: 200 }
    case 'radial':
      return { type: 'radial', unitRadius: 100 }
    default:
      return { type: 'force', preventOverlap: true, nodeSize: 56, linkDistance: 140, nodeStrength: -100 }
  }
}

function initGraph() {
  if (!containerRef.value || graphInstance) return

  const width = containerRef.value.offsetWidth || 800
  const height = containerRef.value.offsetHeight || 600

  graphInstance = new G6.Graph({
    container: containerRef.value,
    width,
    height,
    fitView: false,
    fitViewPadding: 30,
    minZoom: 0.1,
    maxZoom: 5,
    modes: { default: ['drag-canvas', 'zoom-canvas', 'drag-node'] },
    layout: getLayoutConfig(props.layout),
    defaultNode: { type: 'circle', size: 56 },
    defaultEdge: { type: 'quadratic' },
    nodeStateStyles: {
      selected: {
        fill: '#60a5fa',
        stroke: '#fbbf24',
        lineWidth: 5
      }
    }
  })

  graphInstance.on('afterlayout', () => {
    if (pendingFocusIds) {
      const ids = pendingFocusIds
      pendingFocusIds = null
      // 先清除所有高亮，再设置新高亮
      graphInstance?.getNodes().forEach(node => graphInstance.clearItemStates(node, ['selected']))
      ids.forEach(id => {
        if (graphInstance?.findById(id)) graphInstance.setItemState(id, 'selected', true)
      })
      // 定位到第一个命中节点
      const first = graphInstance?.findById(ids[0])
      if (first) graphInstance.focusItem(first, true, { duration: 300, easing: 'easeCubic' })
    } else {
      graphInstance?.fitView()
    }
  })

  graphInstance.on('node:click', evt => {
    const model = evt.item.getModel()
    emit('node-click', model.id)
    const node = props.nodes.find(n => n.id === model.id)
    if (node) emit('node-select', node)
  })

  graphInstance.on('edge:click', evt => {
    const model = evt.item.getModel()
    const edge = props.edges.find(e => e.id === model.id)
    if (edge) emit('edge-select', edge)
  })

  graphInstance.on('canvas:click', () => {
    emit('canvas-click')
  })

  graphInstance.data(buildG6Data(props.nodes, props.edges))
  graphInstance.render()
}

function updateGraph() {
  if (!graphInstance) {
    initGraph()
    return
  }
  graphInstance.changeData(buildG6Data(props.nodes, props.edges))
  graphInstance.layout()
}

function updateLayout() {
  if (!graphInstance) return
  graphInstance.updateLayout(getLayoutConfig(props.layout))
}

watch(() => [props.nodes, props.edges], updateGraph, { deep: true })
watch(() => props.layout, updateLayout)

let resizeObserver = null

onMounted(async () => {
  await nextTick()
  initGraph()
  resizeObserver = new ResizeObserver(() => {
    if (graphInstance && containerRef.value) {
      graphInstance.changeSize(containerRef.value.offsetWidth, containerRef.value.offsetHeight)
      graphInstance.fitView()
    }
  })
  if (containerRef.value) resizeObserver.observe(containerRef.value)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  if (graphInstance) {
    graphInstance.destroy()
    graphInstance = null
  }
})

// 将两个十六进制颜色按 t（0-1）线性插值
function interpolateColor(hex1, hex2, t) {
  const parse = h => [
    parseInt(h.slice(1, 3), 16),
    parseInt(h.slice(3, 5), 16),
    parseInt(h.slice(5, 7), 16)
  ]
  const [r1, g1, b1] = parse(hex1)
  const [r2, g2, b2] = parse(hex2)
  const r = Math.round(r1 + (r2 - r1) * t)
  const g = Math.round(g1 + (g2 - g1) * t)
  const b = Math.round(b1 + (b2 - b1) * t)
  return `#${r.toString(16).padStart(2, '0')}${g.toString(16).padStart(2, '0')}${b.toString(16).padStart(2, '0')}`
}

defineExpose({
  focusNodes(nodeIds) {
    if (!graphInstance || !nodeIds?.length) return
    // 若节点已全部在画布上，立即高亮+定位，无需等 afterlayout
    const allExist = nodeIds.every(id => graphInstance.findById(id))
    if (allExist) {
      graphInstance.getNodes().forEach(node => graphInstance.clearItemStates(node, ['selected']))
      nodeIds.forEach(id => graphInstance.setItemState(id, 'selected', true))
      const first = graphInstance.findById(nodeIds[0])
      if (first) graphInstance.focusItem(first, true, { duration: 300, easing: 'easeCubic' })
      pendingFocusIds = null
    } else {
      // 有新节点，等布局完成后在 afterlayout 里执行
      pendingFocusIds = [...nodeIds]
    }
  },
  clearHighlight() {
    if (!graphInstance) return
    pendingFocusIds = null
    graphInstance.getNodes().forEach(node => graphInstance.clearItemStates(node, ['selected']))
  },
  highlightNodes(nodeIds) {
    if (!graphInstance) return
    nodeIds.forEach(id => graphInstance.setItemState(id, 'selected', true))
  },
  fitView() {
    if (graphInstance) graphInstance.fitView()
  },
  // 对节点按算法分数着色
  // mode='gradient': 蓝→红渐变（中心性算法）
  // mode='community': 按 communityId % 12 从色板取色（社区算法）
  // sizeMapping: 同步调整节点大小
  applyScoreColors(nodeScores, mode = 'gradient', sizeMapping = false) {
    if (!graphInstance || !nodeScores?.length) return
    const scores = nodeScores.map(ns => ns.score)
    const minScore = Math.min(...scores)
    const maxScore = Math.max(...scores)
    const range = maxScore - minScore || 1

    nodeScores.forEach(ns => {
      const item = graphInstance.findById(ns.node_id)
      if (!item) return

      let fill
      if (mode === 'community') {
        const idx = Math.abs(Number(ns.community_id) % COMMUNITY_COLORS.length)
        fill = COMMUNITY_COLORS[idx]
      } else {
        // gradient: t=0 蓝 → t=1 红
        const t = (ns.score - minScore) / range
        fill = interpolateColor('#3b82f6', '#ef4444', t)
      }

      const updateCfg = { style: { fill } }
      if (sizeMapping) {
        const r = 20 + Math.round((ns.score - minScore) / range * 20)
        updateCfg.style.r = r
        updateCfg.size = r * 2
      }
      graphInstance.updateItem(item, updateCfg)
    })
  },
  // 恢复节点原始本体颜色和默认大小
  resetNodeColors() {
    if (!graphInstance) return
    graphInstance.getNodes().forEach(node => {
      const model = node.getModel()
      const originalColor = model._meta?.color || '#5B8FF9'
      graphInstance.updateItem(node, {
        style: { fill: originalColor, r: 28 },
        size: 56
      })
    })
  }
})
</script>

<style scoped>
.graph-canvas {
  width: 100%;
  height: 100%;
  position: relative;
  background: var(--addp-bg-primary, #fafafa);
  border-radius: 4px;
  overflow: hidden;
}

.graph-canvas :deep(canvas) {
  display: block;
}

.empty-hint {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
}
</style>
