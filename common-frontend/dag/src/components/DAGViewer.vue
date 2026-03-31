<template>
  <div class="dag-viewer" :style="{ height: height + 'px' }" ref="containerRef"></div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue'
import G6 from '@antv/g6'
import { registerMultiPortNode } from '../nodes/MultiPortNode.js'

const props = defineProps({
  dagData: {
    type: Object,
    required: true
  },
  height: {
    type: Number,
    default: 400
  }
})

const containerRef = ref(null)
const graph = ref(null)
let resizeObserver = null

onMounted(() => {
  // setTimeout 200ms 确保容器已完成布局，与 develop 模块保持一致
  setTimeout(() => {
    initGraph()
    if (props.dagData?.tasks?.length > 0) {
      loadWorkflow(props.dagData)
    }
    setupResizeObserver()
  }, 200)
})

onUnmounted(() => {
  if (resizeObserver) resizeObserver.disconnect()
  if (graph.value) graph.value.destroy()
})

watch(() => props.dagData, (newData) => {
  if (newData?.tasks?.length > 0 && graph.value) {
    loadWorkflow(newData)
  }
}, { deep: true })

function initGraph() {
  if (!containerRef.value) return

  registerMultiPortNode()

  const width = containerRef.value.offsetWidth || 800
  const height = containerRef.value.offsetHeight || props.height

  graph.value = new G6.Graph({
    container: containerRef.value,
    width,
    height,
    modes: {
      default: ['drag-canvas', 'zoom-canvas']
    },
    defaultNode: {
      type: 'workflow-node',
      size: [140, 60]
    },
    defaultEdge: {
      type: 'polyline',
      style: {
        stroke: '#A3B1BF',
        lineWidth: 2,
        radius: 10,
        endArrow: {
          path: G6.Arrow.triangle(10, 12, 0),
          fill: '#A3B1BF',
          d: 0
        }
      }
    }
  })
}

function loadWorkflow(workflow) {
  if (!graph.value || !workflow?.tasks) return

  graph.value.clear()

  const nodes = []
  const edges = []

  workflow.tasks.forEach((task, index) => {
    nodes.push({
      id: task.id,
      label: task.operator,
      x: 100 + (index % 3) * 200,
      y: 100 + Math.floor(index / 3) * 120,
      operator: task.operator,
      params: task.params || {},
      depends_on: task.depends_on || []
    })

    if (task.depends_on?.length > 0) {
      task.depends_on.forEach(sourceId => {
        edges.push({
          source: sourceId,
          target: task.id,
          type: 'polyline',
          style: {
            stroke: '#A3B1BF',
            lineWidth: 2,
            radius: 10,
            endArrow: { path: G6.Arrow.triangle(10, 12, 0), fill: '#A3B1BF', d: 0 }
          }
        })
      })
    }
  })

  graph.value.data({ nodes, edges })
  graph.value.render()
  graph.value.fitView(20)
}

function setupResizeObserver() {
  resizeObserver = new ResizeObserver(() => {
    if (graph.value && containerRef.value) {
      const w = containerRef.value.offsetWidth
      const h = containerRef.value.offsetHeight
      if (w > 0 && h > 0) {
        graph.value.changeSize(w, h)
      }
    }
  })
  if (containerRef.value) {
    resizeObserver.observe(containerRef.value)
  }
}
</script>

<style scoped>
.dag-viewer {
  width: 100%;
  background: var(--addp-bg-primary, #1a1a2e);
  border-radius: 4px;
  overflow: hidden;
  position: relative;
}
</style>
