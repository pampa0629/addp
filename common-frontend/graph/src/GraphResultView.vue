<template>
  <div class="graph-result-view">
    <div v-if="isEmpty" class="empty-state">
      <el-empty description="无图形数据" :image-size="60" />
    </div>
    <div v-else ref="wrapperRef" class="graph-wrapper">
      <div class="graph-toolbar">
        <span class="stat">节点: {{ graphData.nodes.length }}</span>
        <span class="stat">关系: {{ graphData.relationships.length }}</span>
        <el-button text size="small" @click="fitView">适应窗口</el-button>
      </div>
      <div ref="containerRef" class="graph-canvas" />
      <!-- 点击节点展示属性 -->
      <div v-if="selectedItem" class="props-panel">
        <div class="props-header">
          <span>
            <el-tag v-for="label in selectedItem.labels" :key="label" size="small" style="margin-right:4px">{{ label }}</el-tag>
          </span>
          <el-button text size="small" @click="selectedItem = null">×</el-button>
        </div>
        <div
          v-for="(val, key) in selectedItem.properties"
          :key="key"
          class="prop-row"
        >
          <span class="prop-key">{{ key }}</span>
          <span class="prop-val">{{ val }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import G6 from '@antv/g6'

const props = defineProps({
  /** { nodes: [{element_id, labels, properties}], relationships: [{element_id, type, start_node_id, end_node_id, properties}] } */
  graphData: {
    type: Object,
    default: () => ({ nodes: [], relationships: [] })
  }
})

const containerRef = ref(null)
const wrapperRef = ref(null)
const selectedItem = ref(null)
let graphInstance = null

const isEmpty = computed(
  () => !props.graphData?.nodes?.length
)

// 基于标签生成颜色
function labelColor(labels) {
  const palette = [
    '#4E79A7', '#F28E2B', '#E15759', '#76B7B2',
    '#59A14F', '#EDC948', '#B07AA1', '#FF9DA7',
    '#9C755F', '#BAB0AC'
  ]
  const label = (labels && labels[0]) || 'Node'
  let hash = 0
  for (let i = 0; i < label.length; i++) hash = (hash * 31 + label.charCodeAt(i)) & 0xffff
  return palette[hash % palette.length]
}

function buildG6Data() {
  const nodes = (props.graphData?.nodes || []).map(n => {
    const label = (n.labels && n.labels[0]) || 'Node'
    // 显示名称：优先显示 name/title/id 属性
    const displayName = n.properties?.name || n.properties?.title || n.properties?.id || label
    return {
      id: n.element_id,
      label: String(displayName).substring(0, 20),
      _meta: n,
      style: {
        fill: labelColor(n.labels),
        stroke: '#fff',
        lineWidth: 2,
        r: 28
      },
      labelCfg: { style: { fill: '#fff', fontSize: 11, fontWeight: 'bold' } }
    }
  })

  const edges = (props.graphData?.relationships || []).map((rel, i) => ({
    id: rel.element_id || `edge-${i}`,
    source: rel.start_node_id,
    target: rel.end_node_id,
    label: rel.type,
    style: {
      stroke: '#c0c4cc',
      lineWidth: 1.5,
      endArrow: { path: G6.Arrow.triangle(8, 6, 0), fill: '#c0c4cc' }
    },
    labelCfg: { style: { fill: '#333', fontSize: 11, fontWeight: '600', stroke: '#fff', lineWidth: 3 }, autoRotate: true }
  }))

  return { nodes, edges }
}

function initGraph() {
  if (!containerRef.value || isEmpty.value) return

  const width = containerRef.value.offsetWidth || 800
  const height = containerRef.value.offsetHeight || 400

  graphInstance = new G6.Graph({
    container: containerRef.value,
    width,
    height,
    fitView: false,
    fitViewPadding: 30,
    modes: { default: ['drag-canvas', 'zoom-canvas', 'drag-node'] },
    layout: {
      type: 'force',
      preventOverlap: true,
      nodeSpacing: 50,
      linkDistance: 140,
      nodeStrength: -100
    },
    defaultNode: { type: 'circle', size: 56 },
    defaultEdge: { type: 'quadratic' }
  })

  graphInstance.on('afterlayout', () => {
    graphInstance?.fitView()
  })

  graphInstance.on('node:click', evt => {
    const model = evt.item.getModel()
    selectedItem.value = model._meta
  })

  graphInstance.on('canvas:click', () => {
    selectedItem.value = null
  })

  graphInstance.data(buildG6Data())
  graphInstance.render()
}

function fitView() {
  graphInstance?.fitView()
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

watch(() => props.graphData, async () => {
  if (graphInstance) {
    graphInstance.destroy()
    graphInstance = null
  }
  selectedItem.value = null
  await nextTick()
  if (!isEmpty.value) {
    initGraph()
  }
}, { deep: true })
</script>

<style scoped>
.graph-result-view {
  width: 100%;
  height: 100%;
  min-height: 300px;
  display: flex;
  flex-direction: column;
}

.empty-state {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.graph-wrapper {
  flex: 1;
  min-height: 0;
  position: relative;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.graph-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 4px 10px;
  background: #f5f7fa;
  border-bottom: 1px solid #e4e7ed;
  font-size: 12px;
  color: #606266;
}

.stat { font-weight: 500; }

.graph-canvas {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  background: #fafbfc;
}

/* 消除 canvas inline 元素底部间隙，避免 ResizeObserver 反馈循环 */
.graph-canvas :deep(canvas) {
  display: block;
}

.props-panel {
  position: absolute;
  bottom: 12px;
  right: 12px;
  background: #fff;
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  padding: 10px 14px;
  min-width: 200px;
  max-width: 300px;
  max-height: 260px;
  overflow-y: auto;
  box-shadow: 0 2px 8px rgba(0,0,0,0.1);
  font-size: 12px;
}

.props-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
}

.prop-row {
  display: flex;
  gap: 8px;
  padding: 2px 0;
  border-bottom: 1px solid #f0f0f0;
  line-height: 1.5;
}

.prop-key {
  color: #909399;
  flex-shrink: 0;
  width: 90px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.prop-val {
  color: #303133;
  word-break: break-all;
}
</style>
