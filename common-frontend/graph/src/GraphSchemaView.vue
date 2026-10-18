<template>
  <div class="graph-schema-view">
    <div v-if="isEmpty" class="empty-state">
      <el-empty :description="t('graph.noSchema')" :image-size="80" />
    </div>
    <div v-else ref="wrapperRef" class="graph-container-wrapper">
      <!-- 图例 -->
      <div class="legend">
        <span class="legend-item">
          <span class="legend-dot node-dot"></span>{{ t('graph.nodeLegend') }}
        </span>
        <span class="legend-item">
          <span class="legend-arrow"></span>{{ t('graph.relationLegend') }}
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
          <span class="detail-key">{{ t('graph.nodeCount') }}</span>
          <span class="detail-val">{{ selectedNode.count }}</span>
        </div>
        <div v-if="selectedNode.properties?.length" class="detail-row">
          <span class="detail-key">{{ t('graph.properties') }}</span>
          <span class="detail-val props">{{ selectedNode.properties.join(', ') }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import G6 from '@antv/g6'
import { graphEdgeType } from './graphGeometry.js'
import { readGraphTheme, getContrastingTextColor, graphLabelColor } from './graphTheme.js'
import { fitGraphToViewport } from './graphViewport.js'

const { t } = useI18n()

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

function buildGraphData() {
  const theme = readGraphTheme(containerRef.value)
  const nodes = (props.schema?.nodes || []).map(n => {
    const fill = graphLabelColor(n.label, theme.categoryColors)
    return {
      id: n.label,
      label: `${n.label}\n(${n.count})`,
      _meta: n,
      style: {
        fill,
        stroke: theme.nodeStroke,
        lineWidth: 2,
        r: Math.max(28, Math.min(50, 20 + Math.log10(n.count + 1) * 12))
      },
      labelCfg: { style: { fill: getContrastingTextColor(fill, theme.labelLight, theme.labelDark), fontSize: 12, fontWeight: 'bold' } }
    }
  })

  const edges = []
  const edgeCount = {}
  ;(props.schema?.relationships || []).forEach(rel => {
    ;(rel.from_labels || [rel.type]).forEach(from => {
      ;(rel.to_labels || [rel.type]).forEach(to => {
        const key = `${from}-${to}`
        edgeCount[key] = (edgeCount[key] || 0) + 1
        edges.push({
          id: `${rel.type}-${from}-${to}-${edgeCount[key]}`,
          type: graphEdgeType(from, to),
          source: from,
          target: to,
          label: rel.type,
          curveOffset: edgeCount[key] > 1 ? edgeCount[key] * 20 : 0,
          style: { stroke: theme.edgeDefault, lineWidth: 1.5, endArrow: { path: G6.Arrow.triangle(8, 6, 0), fill: theme.edgeDefault } },
          labelCfg: { style: { fill: theme.edgeLabel, fontSize: 12, fontWeight: '600', stroke: theme.edgeLabelStroke, lineWidth: 3 }, autoRotate: true }
        })
      })
    })
  })

  return { nodes, edges }
}

function updateTheme() {
  if (!graphInstance || !containerRef.value) return
  const { nodes, edges } = buildGraphData()
  for (const model of [...nodes, ...edges]) {
    graphInstance.updateItem(model.id, { style: model.style, labelCfg: model.labelCfg })
  }
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

  graphInstance.on('afterlayout', fitView)

  graphInstance.on('node:click', evt => {
    const node = evt.item.getModel()
    selectedNode.value = node._meta
    emit('node-click', node._meta?.label || node.id)
  })

  graphInstance.data(graphData)
  graphInstance.render()
}

function fitView() {
  fitGraphToViewport(graphInstance)
}

let resizeTimer = null

function resizeGraph() {
  if (!graphInstance || !containerRef.value) return
  clearTimeout(resizeTimer)
  resizeTimer = setTimeout(() => {
    if (!graphInstance || !containerRef.value) return
    graphInstance.changeSize(containerRef.value.offsetWidth, containerRef.value.offsetHeight)
    fitView()
  }, 300)
}

let resizeObserver = null
let themeObserver = null

function resetGraph() {
  clearTimeout(resizeTimer)
  resizeObserver?.disconnect()
  resizeObserver = null
  graphInstance?.destroy()
  graphInstance = null
}

onMounted(() => {
  themeObserver = new MutationObserver(updateTheme)
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style'] })
})

onUnmounted(() => {
  resetGraph()
  themeObserver?.disconnect()
})

watch([() => props.schema, containerRef], () => {
  resetGraph()
  selectedNode.value = null
  if (!containerRef.value || isEmpty.value) return
  initGraph()
  resizeObserver = new ResizeObserver(resizeGraph)
  resizeObserver.observe(wrapperRef.value)
}, { deep: true, flush: 'post' })
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
  background: var(--addp-bg-primary);
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
  background: var(--addp-bg-secondary);
  padding: 4px 10px;
  border-radius: 4px;
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.legend-item { display: flex; align-items: center; gap: 5px; }
.legend-dot { display: inline-block; width: 12px; height: 12px; border-radius: 50%; }
.node-dot { background: var(--addp-graph-category-1); }
.legend-arrow {
  display: inline-block;
  width: 20px;
  height: 2px;
  background: var(--addp-graph-edge-default);
  position: relative;
}
.legend-arrow::after {
  content: '';
  position: absolute;
  right: -1px;
  top: -3px;
  border: 4px solid transparent;
  border-left-color: var(--addp-graph-edge-default);
}

.detail-panel {
  position: absolute;
  bottom: 12px;
  right: 12px;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  padding: 10px 14px;
  min-width: 180px;
  max-width: 260px;
  box-shadow: var(--addp-shadow-card);
  font-size: 13px;
}

.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 8px;
  color: var(--addp-text-primary);
}

.detail-row {
  display: flex;
  gap: 8px;
  margin-bottom: 4px;
  line-height: 1.4;
}

.detail-key { color: var(--addp-text-tertiary); flex-shrink: 0; }
.detail-val { color: var(--addp-text-primary); word-break: break-all; }
.detail-val.props { font-size: 12px; color: var(--addp-text-secondary); }
</style>
