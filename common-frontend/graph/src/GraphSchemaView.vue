<template>
  <div class="graph-schema-view">
    <div v-if="isEmpty" class="empty-state">
      <el-empty description="暂无图 Schema 数据，请先扫描元数据" :image-size="80" />
    </div>
    <div v-else ref="wrapperRef" class="graph-container-wrapper">
      <!-- 图例 -->
      <div class="legend">
        <span class="legend-item">
          <span class="legend-dot node-dot"></span>节点标签
        </span>
        <span class="legend-item">
          <span class="legend-arrow"></span>关系类型
        </span>
      </div>
      <div ref="containerRef" class="graph-container" />
      <!-- 选中节点详情面板 -->
      <div v-if="selectedNode" class="detail-panel">
        <div class="detail-header">
          <strong>{{ selectedNode.label }}</strong>
          <el-button text size="small" @click="selectedNode = null">×</el-button>
        </div>
        <div class="detail-row">
          <span class="detail-key">节点数量</span>
          <span class="detail-val">{{ selectedNode.count }}</span>
        </div>
        <div v-if="selectedNode.properties?.length" class="detail-row">
          <span class="detail-key">属性</span>
          <span class="detail-val props">{{ selectedNode.properties.join(', ') }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import G6 from '@antv/g6'

const emit = defineEmits(['node-click'])

const props = defineProps({
  /** { nodes: [{label, count, properties}], relationships: [{type, count, from_labels, to_labels}] } */
  schema: {
    type: Object,
    default: () => ({ nodes: [], relationships: [] })
  }
})

const containerRef = ref(null)
const wrapperRef = ref(null)
const selectedNode = ref(null)
let graphInstance = null

const isEmpty = computed(
  () => !props.schema?.nodes?.length && !props.schema?.relationships?.length
)

// 生成节点颜色（基于标签名 hash）
function labelColor(label) {
  const palette = [
    '#4E79A7', '#F28E2B', '#E15759', '#76B7B2',
    '#59A14F', '#EDC948', '#B07AA1', '#FF9DA7',
    '#9C755F', '#BAB0AC'
  ]
  let hash = 0
  for (let i = 0; i < label.length; i++) hash = (hash * 31 + label.charCodeAt(i)) & 0xffff
  return palette[hash % palette.length]
}

function buildGraphData() {
  const nodes = (props.schema?.nodes || []).map(n => ({
    id: n.label,
    label: `${n.label}\n(${n.count})`,
    _meta: n,
    style: {
      fill: labelColor(n.label),
      stroke: '#fff',
      lineWidth: 2,
      r: Math.max(28, Math.min(50, 20 + Math.log10(n.count + 1) * 12))
    },
    labelCfg: { style: { fill: '#fff', fontSize: 12, fontWeight: 'bold' } }
  }))

  const edges = []
  const edgeCount = {}
  ;(props.schema?.relationships || []).forEach(rel => {
    ;(rel.from_labels || [rel.type]).forEach(from => {
      ;(rel.to_labels || [rel.type]).forEach(to => {
        const key = `${from}-${to}`
        edgeCount[key] = (edgeCount[key] || 0) + 1
        edges.push({
          id: `${rel.type}-${from}-${to}-${edgeCount[key]}`,
          source: from,
          target: to,
          label: rel.type,
          curveOffset: edgeCount[key] > 1 ? edgeCount[key] * 20 : 0,
          style: { stroke: '#aab', lineWidth: 1.5, endArrow: { path: G6.Arrow.triangle(8, 6, 0), fill: '#aab' } },
          labelCfg: { style: { fill: '#333', fontSize: 12, fontWeight: '600', stroke: '#fff', lineWidth: 3 }, autoRotate: true }
        })
      })
    })
  })

  return { nodes, edges }
}

function initGraph() {
  if (!containerRef.value || isEmpty.value) return

  const width = containerRef.value.offsetWidth || 800
  const height = containerRef.value.offsetHeight || 500
  const graphData = buildGraphData()

  graphInstance = new G6.Graph({
    container: containerRef.value,
    width,
    height,
    fitView: false,
    fitViewPadding: 40,
    modes: { default: ['drag-canvas', 'zoom-canvas', 'drag-node'] },
    layout: {
      type: 'force',
      preventOverlap: true,
      nodeSpacing: 60,
      linkDistance: 160,
      nodeStrength: -120
    },
    defaultNode: { type: 'circle', size: 50 },
    defaultEdge: { type: 'quadratic' }
  })

  graphInstance.on('afterlayout', () => {
    graphInstance?.fitView()
  })

  graphInstance.on('node:click', evt => {
    const node = evt.item.getModel()
    selectedNode.value = node._meta
    emit('node-click', node._meta?.label || node.id)
  })

  graphInstance.data(graphData)
  graphInstance.render()
}

let resizeTimer = null

function resizeGraph() {
  if (!graphInstance || !containerRef.value) return
  clearTimeout(resizeTimer)
  resizeTimer = setTimeout(() => {
    if (!graphInstance || !containerRef.value) return
    graphInstance.changeSize(containerRef.value.offsetWidth, containerRef.value.offsetHeight)
    graphInstance.fitView()
  }, 300)
}

let resizeObserver = null

onMounted(async () => {
  await nextTick()
  if (!isEmpty.value) {
    initGraph()
    resizeObserver = new ResizeObserver(resizeGraph)
    resizeObserver.observe(wrapperRef.value)
  }
})

onUnmounted(() => {
  clearTimeout(resizeTimer)
  resizeObserver?.disconnect()
  graphInstance?.destroy()
  graphInstance = null
})

watch(() => props.schema, async () => {
  if (graphInstance) {
    graphInstance.destroy()
    graphInstance = null
  }
  await nextTick()
  if (!isEmpty.value) {
    initGraph()
  }
}, { deep: true })
</script>

<style scoped>
.graph-schema-view {
  width: 100%;
  height: 100%;
  min-height: 300px;
  position: relative;
  display: flex;
  flex-direction: column;
}

.empty-state {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.graph-container-wrapper {
  flex: 1;
  min-height: 0;
  position: relative;
  overflow: hidden;
}

.graph-container {
  width: 100%;
  height: 100%;
  overflow: hidden;
}

/* 消除 canvas inline 元素底部间隙，避免 ResizeObserver 反馈循环 */
.graph-container :deep(canvas) {
  display: block;
}

.legend {
  position: absolute;
  top: 8px;
  left: 8px;
  z-index: 10;
  display: flex;
  gap: 16px;
  background: rgba(255,255,255,0.85);
  padding: 4px 10px;
  border-radius: 4px;
  font-size: 12px;
  color: #555;
}

.legend-item { display: flex; align-items: center; gap: 5px; }
.legend-dot { display: inline-block; width: 12px; height: 12px; border-radius: 50%; }
.node-dot { background: #4E79A7; }
.legend-arrow {
  display: inline-block;
  width: 20px;
  height: 2px;
  background: #aab;
  position: relative;
}
.legend-arrow::after {
  content: '';
  position: absolute;
  right: -1px;
  top: -3px;
  border: 4px solid transparent;
  border-left-color: #aab;
}

.detail-panel {
  position: absolute;
  bottom: 12px;
  right: 12px;
  background: #fff;
  border: 1px solid #e0e0e0;
  border-radius: 6px;
  padding: 10px 14px;
  min-width: 180px;
  max-width: 260px;
  box-shadow: 0 2px 8px rgba(0,0,0,0.1);
  font-size: 13px;
}

.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
  color: #333;
}

.detail-row {
  display: flex;
  gap: 8px;
  margin-bottom: 4px;
  line-height: 1.4;
}

.detail-key { color: #888; flex-shrink: 0; }
.detail-val { color: #333; word-break: break-all; }
.detail-val.props { font-size: 12px; color: #555; }
</style>
