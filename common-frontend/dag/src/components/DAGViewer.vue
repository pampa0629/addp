<template>
  <div class="dag-viewer" :style="{ height: height + 'px' }" ref="containerRef"></div>
</template>

<script setup>
import { ref, shallowRef, onMounted, onUnmounted, watch } from 'vue'
import G6 from '@antv/g6'
import { useDAGTheme } from '../composables/useDAGTheme.js'
import { registerMultiPortNode } from '../nodes/MultiPortNode.js'
import { readGraphTheme, getContrastingTextColor } from '../../../graph/src/graphTheme.js'
import { fitGraphToViewport } from '../../../graph/src/graphViewport.js'

const props = defineProps({
  dagData: { type: Object, required: true },
  height: { type: Number, default: 400 }
})

const containerRef = ref(null)
const graph = shallowRef(null)
let resizeObserver = null
useDAGTheme(updateTheme)

function nodeAppearance(theme) {
  return {
    style: { fill: theme.primary, stroke: theme.border, lineWidth: 2 },
    labelCfg: { style: { fill: getContrastingTextColor(theme.primary, theme.labelLight, theme.labelDark) } },
    portStyle: { fill: theme.surface, input: theme.success, output: theme.warning, defaultOutput: theme.primary, label: theme.edgeLabel }
  }
}

function edgeAppearance(theme) {
  return { style: { stroke: theme.edgeDefault, lineWidth: 2, radius: 10,
    endArrow: { path: G6.Arrow.triangle(10, 12, 0), fill: theme.edgeDefault, d: 0 } } }
}

function updateTheme() {
  if (!graph.value) return
  const theme = readGraphTheme(containerRef.value)
  graph.value.getNodes().forEach(node => graph.value.updateItem(node, nodeAppearance(theme)))
  graph.value.getEdges().forEach(edge => graph.value.updateItem(edge, edgeAppearance(theme)))
}

function loadWorkflow() {
  if (!graph.value) return
  const theme = readGraphTheme(containerRef.value)
  const nodes = [], edges = []
  for (const [index, task] of (props.dagData?.tasks || []).entries()) {
    nodes.push({
      id: task.id, label: task.operator,
      x: 100 + (index % 3) * 200, y: 100 + Math.floor(index / 3) * 120,
      operator: task.operator, params: task.params || {}, depends_on: task.depends_on || [],
      ...nodeAppearance(theme)
    })
    for (const sourceId of task.depends_on || []) {
      edges.push({ source: sourceId, target: task.id, ...edgeAppearance(theme) })
    }
  }
  graph.value.clear()
  graph.value.data({ nodes, edges })
  graph.value.render()
  if (nodes.length) fitGraphToViewport(graph.value)
}

function resizeGraph() {
  const container = containerRef.value
  if (!container || container.offsetWidth <= 0 || container.offsetHeight <= 0) return
  const width = container.offsetWidth, height = container.offsetHeight
  if (graph.value) {
    if (width !== graph.value.get('width') || height !== graph.value.get('height')) graph.value.changeSize(width, height)
    return
  }
  registerMultiPortNode()
  graph.value = new G6.Graph({
    container, width, height, fitViewPadding: 20,
    modes: { default: ['drag-canvas', 'zoom-canvas'] },
    defaultNode: { type: 'workflow-node', size: [140, 60] },
    defaultEdge: { type: 'polyline' }
  })
  loadWorkflow()
}

onMounted(() => {
  resizeGraph()
  resizeObserver = new ResizeObserver(resizeGraph)
  resizeObserver.observe(containerRef.value)
})
onUnmounted(() => {
  resizeObserver?.disconnect()
  graph.value?.destroy()
  graph.value = null
})
watch(() => props.dagData, loadWorkflow, { deep: true })
</script>

<style src="./dag-viewer.css"></style>
