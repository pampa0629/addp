<template>
  <div class="lineage-viewer">
    <el-empty v-if="!nodes.length" :description="t('lineage.noData')" :image-size="56" />
    <template v-else>
      <div class="lineage-toolbar">
        <span>{{ t('lineage.nodes') }}: {{ nodes.length }}</span>
        <span>{{ t('lineage.edges') }}: {{ edges.length }}</span>
        <el-button text size="small" @click="fitView">{{ t('lineage.fitView') }}</el-button>
      </div>
      <div ref="canvasRef" class="lineage-canvas" :style="{ height: `${height}px` }" />
      <div v-if="selectedNode" class="lineage-details">
        <div class="lineage-details-title">{{ selectedNode.name || selectedNode.full_name || selectedNode.kind }}</div>
        <div v-if="selectedNode.item_fingerprint" class="lineage-details-row">{{ selectedNode.item_fingerprint }}</div>
        <div v-if="selectedNode.published_revision" class="lineage-details-row">{{ selectedNode.published_revision }}</div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import G6 from '@antv/g6'

const { t } = useI18n()
const props = defineProps({
  graph: { type: Object, default: () => ({ nodes: [], edges: [] }) },
  height: { type: Number, default: 420 }
})

const canvasRef = ref(null)
const selectedNode = ref(null)
let graphInstance
let resizeObserver

const nodes = computed(() => props.graph?.nodes || [])
const edges = computed(() => props.graph?.edges || [])

function nodeId(node) {
  if (node.kind === 'published_service') return `service:${node.service_id}:${node.published_revision}`
  return `item:${node.item_id}`
}

function themeColor(variableName) {
  if (typeof window === 'undefined') return ''
  const rootValue = getComputedStyle(document.documentElement).getPropertyValue(variableName).trim()
  if (rootValue) return rootValue
  return canvasRef.value ? getComputedStyle(canvasRef.value).getPropertyValue(variableName).trim() : ''
}

function graphData() {
  return {
    nodes: nodes.value.map(node => ({
      id: nodeId(node),
      label: String(node.name || node.full_name || node.published_revision || node.kind).slice(0, 28),
      _node: node,
      style: {
        fill: themeColor(node.kind === 'published_service' ? '--el-color-warning' : '--el-color-primary'),
        stroke: themeColor('--el-bg-color'),
        lineWidth: 2
      },
      labelCfg: { style: { fill: themeColor('--el-text-color-primary'), fontSize: 11 } }
    })),
    edges: edges.value.map((edge, index) => ({
      id: `lineage-edge:${index}`,
      source: nodeId(edge.source),
      target: nodeId(edge.target),
      label: edge.relation_kind,
      style: {
        stroke: themeColor('--el-border-color'),
        endArrow: { path: G6.Arrow.triangle(8, 6, 0), fill: themeColor('--el-border-color') }
      },
      labelCfg: { autoRotate: true, style: { fill: themeColor('--el-text-color-secondary'), fontSize: 10 } }
    }))
  }
}

function fitView() { graphInstance?.fitView(24) }

function destroyGraph() {
  graphInstance?.destroy()
  graphInstance = undefined
}

async function renderGraph() {
  destroyGraph()
  await nextTick()
  if (!canvasRef.value || !nodes.value.length) return
  graphInstance = new G6.Graph({
    container: canvasRef.value,
    width: canvasRef.value.clientWidth || 800,
    height: props.height,
    modes: { default: ['drag-canvas', 'zoom-canvas', 'drag-node'] },
    layout: { type: 'dagre', rankdir: 'LR', nodesep: 32, ranksep: 72 },
    defaultNode: { type: 'rect', size: [150, 42], style: { radius: 4 } },
    defaultEdge: { type: 'polyline' }
  })
  graphInstance.on('node:click', event => { selectedNode.value = event.item.getModel()._node })
  graphInstance.on('canvas:click', () => { selectedNode.value = null })
  graphInstance.on('afterlayout', fitView)
  graphInstance.data(graphData())
  graphInstance.render()
  if (typeof requestAnimationFrame === 'function') requestAnimationFrame(fitView)
  else fitView()
}

onMounted(() => {
  renderGraph()
  resizeObserver = new ResizeObserver(() => {
    if (!graphInstance || !canvasRef.value || canvasRef.value.clientWidth <= 0) return
    graphInstance.changeSize(canvasRef.value.clientWidth, props.height)
    fitView()
  })
  if (canvasRef.value) resizeObserver.observe(canvasRef.value)
})
onUnmounted(() => { resizeObserver?.disconnect(); destroyGraph() })
watch(() => props.graph, renderGraph, { deep: true })
</script>

<style scoped>
.lineage-viewer { position: relative; width: 100%; min-height: 300px; height: 100%; display: flex; flex-direction: column; background: var(--el-bg-color); }
.lineage-toolbar { display: flex; align-items: center; gap: 16px; min-height: 34px; padding: 0 10px; color: var(--el-text-color-secondary); border-bottom: 1px solid var(--el-border-color-lighter); font-size: 12px; }
.lineage-canvas { flex: 1; min-height: 260px; overflow: hidden; }
.lineage-canvas :deep(canvas) { display: block; }
.lineage-details { position: absolute; right: 12px; bottom: 12px; max-width: 280px; padding: 10px 12px; background: var(--el-bg-color-overlay); border: 1px solid var(--el-border-color); border-radius: 4px; box-shadow: var(--el-box-shadow-light); font-size: 12px; }
.lineage-details-title { color: var(--el-text-color-primary); font-weight: 600; margin-bottom: 6px; }
.lineage-details-row { color: var(--el-text-color-secondary); word-break: break-all; }
</style>
