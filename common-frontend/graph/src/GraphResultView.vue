<template>
  <div class="graph-result-view">
    <div v-if="isEmpty" class="empty-state">
      <el-empty :description="t('graph.noData')" :image-size="60" />
    </div>
    <div v-else ref="wrapperRef" class="graph-wrapper">
      <div class="graph-toolbar">
        <span class="stat">{{ t('graph.nodes') }}: {{ graphData.nodes.length }}</span>
        <span class="stat">{{ t('graph.relationships') }}: {{ graphData.relationships.length }}</span>
        <el-button text size="small" @click="fitView">{{ t('graph.fitView') }}</el-button>
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
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import G6 from '@antv/g6'
import { readGraphTheme, getContrastingTextColor, graphLabelColor } from './graphTheme.js'
import { graphNodeItemId, graphEdgeItemId } from './graphIdentity.js'
import { fitGraphToViewport } from './graphViewport.js'

const { t } = useI18n()

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

function buildG6Data() {
  const theme = readGraphTheme(containerRef.value)
  const nodes = (props.graphData?.nodes || []).map(n => {
    const label = (n.labels && n.labels[0]) || 'Node'
    // 显示名称：优先显示 name/title/id 属性
    const displayName = n.properties?.name || n.properties?.title || n.properties?.id || label
    const fill = graphLabelColor(label, theme.categoryColors)
    return {
      id: graphNodeItemId(n.element_id),
      label: String(displayName).substring(0, 20),
      _meta: n,
      style: {
        fill,
        stroke: theme.nodeStroke,
        lineWidth: 2,
        r: 28
      },
      labelCfg: { style: { fill: getContrastingTextColor(fill, theme.labelLight, theme.labelDark), fontSize: 11, fontWeight: 'bold' } }
    }
  })

  const edges = (props.graphData?.relationships || []).map((rel, i) => ({
    id: graphEdgeItemId(rel.element_id || `generated-${i}`),
    source: graphNodeItemId(rel.start_node_id),
    target: graphNodeItemId(rel.end_node_id),
    label: rel.type,
    style: {
      stroke: theme.edgeDefault,
      lineWidth: 1.5,
      endArrow: { path: G6.Arrow.triangle(8, 6, 0), fill: theme.edgeDefault }
    },
    labelCfg: { style: { fill: theme.edgeLabel, fontSize: 11, fontWeight: '600', stroke: theme.edgeLabelStroke, lineWidth: 3 }, autoRotate: true }
  }))

  return { nodes, edges }
}

function updateTheme() {
  if (!graphInstance || !containerRef.value) return
  const { nodes, edges } = buildG6Data()
  for (const model of [...nodes, ...edges]) {
    graphInstance.updateItem(model.id, { style: model.style, labelCfg: model.labelCfg })
  }
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

  graphInstance.on('afterlayout', fitView)

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

watch([() => props.graphData, containerRef], () => {
  resetGraph()
  selectedItem.value = null
  if (!containerRef.value || isEmpty.value) return
  initGraph()
  resizeObserver = new ResizeObserver(resizeGraph)
  resizeObserver.observe(wrapperRef.value)
}, { deep: true, flush: 'post' })
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
  background: var(--addp-bg-secondary);
  border-bottom: 1px solid var(--addp-border-color);
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.stat { font-weight: 500; }

.graph-canvas {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  background: var(--addp-bg-primary);
}

/* 消除 canvas inline 元素底部间隙，避免 ResizeObserver 反馈循环 */
.graph-canvas :deep(canvas) {
  display: block;
}

.props-panel {
  position: absolute;
  bottom: 12px;
  right: 12px;
  background: var(--addp-bg-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  padding: 10px 14px;
  min-width: 200px;
  max-width: 300px;
  max-height: 260px;
  overflow-y: auto;
  box-shadow: var(--addp-shadow-card);
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
  border-bottom: 1px solid var(--addp-border-color-light);
  line-height: 1.5;
}

.prop-key {
  color: var(--addp-text-tertiary);
  flex-shrink: 0;
  width: 90px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.prop-val {
  color: var(--addp-text-primary);
  word-break: break-all;
}
</style>
