<template>
  <div ref="containerRef" class="graph-canvas" v-loading="loading || layoutInProgress">
    <div v-if="!loading && !layoutInProgress && isEmpty" class="empty-hint">
      <el-empty description="暂无图数据，请尝试搜索或展开节点" />
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, watch, computed, nextTick } from 'vue'
import G6 from '@antv/g6'
import { getContrastingTextColor, readGraphTheme } from '../utils/graphVisualEncoding'
import { placeIncrementalNodes } from '../utils/incrementalGraphLayout'
import { createGraphLayoutConfig } from '../utils/graphLayoutPolicy'

const SEMANTIC_EDGE_TYPE = 'addp-semantic-quadratic'

G6.registerEdge(SEMANTIC_EDGE_TYPE, {
  afterDraw(config, group) {
    updateSemanticEdgeLine(config, group)
    updateDirectionMarker(config, group)
  },
  afterUpdate(config, item) {
    const group = item.getContainer()
    updateSemanticEdgeLine(config, group)
    updateDirectionMarker(config, group)
  }
}, 'quadratic')

function updateSemanticEdgeLine(config, group) {
  const existing = group.find(element => element.get('name') === 'semantic-edge-line')
  const path = semanticEdgePath(config)
  if (!path) {
    existing?.hide()
    return
  }
  const attrs = {
    path,
    fill: null,
    stroke: config._meta?.visual_color || config._meta?.color,
    lineDash: config._meta?.visual_line_dash || [],
    lineWidth: config.style?.lineWidth,
    opacity: config.style?.opacity
  }
  if (existing) {
    existing.show()
    existing.attr(attrs)
  } else {
    group.addShape('path', { name: 'semantic-edge-line', capture: false, attrs })
  }
}

function semanticEdgePath(config) {
  const start = config.startPoint
  const end = config.endPoint
  if (!start || !end) return null
  const control = config.controlPoints?.[0]
  if (control) return [['M', start.x, start.y], ['Q', control.x, control.y, end.x, end.y]]
  if (start.x === end.x && start.y === end.y) return null
  return [['M', start.x, start.y], ['L', end.x, end.y]]
}

function updateDirectionMarker(config, group) {
  const existing = group.find(element => element.get('name') === 'direction-marker')
  const path = config._meta?.directed === false ? null : directionMarkerPath(config)
  if (!path) {
    existing?.hide()
    return
  }
  const attrs = { path, fill: config._meta?.visual_color || config._meta?.color }
  if (existing) {
    existing.show()
    existing.attr(attrs)
  } else {
    group.addShape('path', { name: 'direction-marker', attrs })
  }
}

function directionMarkerPath(config) {
  const end = config.endPoint
  const previous = config.controlPoints?.[config.controlPoints.length - 1] || config.startPoint
  if (!end || !previous) return null
  const dx = end.x - previous.x
  const dy = end.y - previous.y
  const length = Math.hypot(dx, dy)
  if (!length) return null
  const unitX = dx / length
  const unitY = dy / length
  const baseX = end.x - unitX * 7
  const baseY = end.y - unitY * 7
  const perpendicularX = -unitY * 3.5
  const perpendicularY = unitX * 3.5
  return [
    ['M', end.x, end.y],
    ['L', baseX + perpendicularX, baseY + perpendicularY],
    ['L', baseX - perpendicularX, baseY - perpendicularY],
    ['Z']
  ]
}

const props = defineProps({
  nodes: { type: Array, default: () => [] },
  edges: { type: Array, default: () => [] },
  layout: { type: String, default: 'force' },
  showEdgeLabels: { type: Boolean, default: false },
  loading: { type: Boolean, default: false },
  centerNodeId: { type: String, default: '' },
  theme: { type: Object, default: () => ({ categoryColors: [] }) },
  selectedNodeId: { type: String, default: '' },
  selectedEdgeId: { type: String, default: '' },
  searchMatchNodeIds: { type: Array, default: () => [] },
  pathNodeIds: { type: Array, default: () => [] },
  pathEdgeIds: { type: Array, default: () => [] },
  expansionAnchorId: { type: String, default: '' }
})

const emit = defineEmits(['node-click', 'node-select', 'edge-select', 'canvas-click'])

const containerRef = ref(null)
const layoutInProgress = ref(false)
let graphInstance = null
let pendingFocusIds = null
let resizeObserver = null
let resizeFrame = 0
let layoutFrame = 0
let layoutCompletionReason = 'data'
let semanticZoomLevel = ''
const analysisVisuals = new Map()

const isEmpty = computed(() => props.nodes.length === 0 && !props.loading)

function effectiveTheme() {
  const cssTheme = readGraphTheme()
  return { ...cssTheme, ...props.theme, categoryColors: props.theme.categoryColors?.length ? props.theme.categoryColors : cssTheme.categoryColors }
}

function graphDensity(nodes, edges) {
  const dense = nodes.length > 35 || edges.length > 60
  const veryDense = nodes.length > 60 || edges.length > 120
  const radius = veryDense ? 18 : dense ? 22 : 28
  return {
    dense,
    radius,
    centerRadius: Math.max(radius + 6, 28),
    labelMaxLength: veryDense ? 10 : dense ? 14 : 20,
    labelFontSize: veryDense ? 9 : dense ? 10 : 11
  }
}

function buildG6Data(nodes, edges) {
  const theme = effectiveTheme()
  const density = graphDensity(nodes, edges)
  return {
    nodes: nodes.map(node => {
      const isCenter = props.centerNodeId && node.id === props.centerNodeId
      const isAggregate = node.kind === 'aggregate'
      const aggregateRadius = Math.min(54, Math.max(34, 28 + Math.log2((node.member_count || 1) + 1) * 2.5))
      const radius = isAggregate ? aggregateRadius : isCenter ? density.centerRadius : density.radius
      const fill = node.visual_color || node.color || theme.categoryColors[0]
      const baseLabel = (node.display_name || node.id).toString().substring(0, density.labelMaxLength)
      const displayLabel = isAggregate ? `${baseLabel}\n${node.member_count || 0}` : baseLabel
      return {
        id: node.id,
        label: displayLabel,
        _meta: node,
        _displayLabel: displayLabel,
        _baseRadius: radius,
        style: {
          fill,
          stroke: isCenter ? theme.path : theme.nodeStroke,
          lineWidth: isAggregate || isCenter ? 4 : 2,
          r: radius
        },
        size: radius * 2,
        labelCfg: {
          style: {
            fill: node.visual_label_color || getContrastingTextColor(fill, theme.labelLight, theme.labelDark),
            fontSize: density.labelFontSize,
            fontWeight: 'bold'
          }
        }
      }
    }),
    edges: edges.map((edge, index) => {
      const color = edge.visual_color || edge.color || theme.edgeDefault
      const isAggregate = edge.kind === 'aggregate'
      const displayLabel = isAggregate
        ? `${edge.type || ''} (${edge.count || 0})`
        : props.showEdgeLabels ? edge.type || '' : ''
      return {
        id: edge.id || `edge-${index}`,
        source: edge.source,
        target: edge.target,
        label: displayLabel,
        _meta: edge,
        _displayLabel: displayLabel,
        style: {
          stroke: color,
          lineDash: edge.visual_line_dash || [],
          lineWidth: isAggregate ? Math.min(6, 2 + Math.log2((edge.count || 1) + 1) * 0.35) : density.dense ? 2 : 2.5,
          lineAppendWidth: 8,
          opacity: density.dense ? 0.78 : 0.9
        },
        labelCfg: {
          style: {
            fill: theme.edgeLabel,
            fontSize: 11,
            fontWeight: '600',
            stroke: theme.edgeLabelStroke,
            lineWidth: 3
          },
          autoRotate: true
        }
      }
    })
  }
}

function currentLayoutConfig() {
  return createGraphLayoutConfig(
    props.layout,
    props.nodes.length,
    props.edges.length,
    props.selectedNodeId,
  )
}

function nodeStateStyles() {
  const theme = effectiveTheme()
  return {
    related: { opacity: 1, stroke: theme.related, lineWidth: 3 },
    path: { opacity: 1, stroke: theme.path, lineWidth: 4 },
    'search-match': { opacity: 1, stroke: theme.searchMatch, lineWidth: 4, shadowColor: theme.searchMatch, shadowBlur: 12 },
    selected: { opacity: 1, stroke: theme.selection, lineWidth: 5, shadowColor: theme.selection, shadowBlur: 18 }
  }
}

function edgeStateStyles() {
  const theme = effectiveTheme()
  return {
    related: { opacity: 1, lineWidth: 3 },
    path: { opacity: 1, lineWidth: 4 },
    selected: { opacity: 1, lineWidth: 4, shadowColor: theme.selection, shadowBlur: 12 }
  }
}

function initGraph() {
  if (!containerRef.value || graphInstance) return
  graphInstance = new G6.Graph({
    container: containerRef.value,
    width: containerRef.value.offsetWidth || 800,
    height: containerRef.value.offsetHeight || 600,
    fitView: false,
    fitViewPadding: 30,
    minZoom: 0.01,
    maxZoom: 5,
    modes: { default: ['drag-canvas', 'zoom-canvas', 'drag-node'] },
    layout: currentLayoutConfig(),
    defaultNode: { type: 'circle', size: 56 },
    defaultEdge: { type: SEMANTIC_EDGE_TYPE },
    nodeStateStyles: nodeStateStyles(),
    edgeStateStyles: edgeStateStyles()
  })

  graphInstance.on('beforelayout', suspendSemanticRendering)

  graphInstance.on('afterlayout', () => {
    const manualLayout = layoutCompletionReason === 'layout'
    if (!manualLayout) {
      syncInteractionStates({ syncZoom: false })
      applyAnalysisVisuals()
    }
    if (pendingFocusIds?.length) {
      const first = graphInstance?.findById(pendingFocusIds[0])
      pendingFocusIds = null
      if (first) graphInstance.focusItem(first, true, { duration: 300, easing: 'easeCubic' })
    } else {
      graphInstance?.fitView()
    }
    syncSemanticZoom(true)
    layoutCompletionReason = ''
    layoutInProgress.value = false
  })

  graphInstance.on('viewportchange', () => syncSemanticZoom())

  graphInstance.on('node:click', event => {
    const model = event.item.getModel()
    emit('node-click', model.id)
    if (model._meta) emit('node-select', model._meta)
  })

  graphInstance.on('edge:click', event => {
    const model = event.item.getModel()
    if (model._meta) emit('edge-select', model._meta)
  })

  graphInstance.on('canvas:click', () => emit('canvas-click'))
  graphInstance.data(buildG6Data(props.nodes, props.edges))
  graphInstance.render()
}

function updateGraph() {
  if (!graphInstance) {
    initGraph()
    return
  }
  const data = buildG6Data(props.nodes, props.edges)
  const existingPositions = new Map(graphInstance.getNodes().map(item => {
    const model = item.getModel()
    return [model.id, { x: model.x, y: model.y }]
  }))
  const retainedCount = data.nodes.filter(node => existingPositions.has(node.id)).length
  const addedCount = data.nodes.length - retainedCount

  if (retainedCount > 0 && addedCount === 0) {
    applyKnownPositions(data, existingPositions)
    graphInstance.changeData(data)
    syncInteractionStates({ syncZoom: false })
    applyAnalysisVisuals()
    syncSemanticZoom(true)
    return
  }

  if (retainedCount > 0 && addedCount > 0 && applyIncrementalPositions(data, existingPositions)) {
    graphInstance.changeData(data)
    syncInteractionStates({ syncZoom: false })
    applyAnalysisVisuals()
    syncSemanticZoom(true)
    return
  }

  layoutCompletionReason = 'data'
  graphInstance.changeData(data)
  graphInstance.updateLayout(currentLayoutConfig())
}

function applyKnownPositions(data, existingPositions) {
  data.nodes.forEach(node => {
    const position = existingPositions.get(node.id)
    if (!position) return
    node.x = position.x
    node.y = position.y
  })
}

function applyIncrementalPositions(data, existingPositions) {
  return placeIncrementalNodes(data, existingPositions, props.expansionAnchorId)
}

function updateTheme() {
  if (!graphInstance) return
  graphInstance.set('nodeStateStyles', nodeStateStyles())
  graphInstance.set('edgeStateStyles', edgeStateStyles())
  updateGraph()
}

function scheduleLayoutUpdate() {
  if (!graphInstance || props.nodes.length === 0) return
  layoutInProgress.value = true
  cancelAnimationFrame(layoutFrame)
  layoutFrame = requestAnimationFrame(runScheduledLayout)
}

function runScheduledLayout() {
  layoutFrame = 0
  if (!graphInstance || props.nodes.length === 0) {
    layoutInProgress.value = false
    return
  }
  layoutCompletionReason = 'layout'
  graphInstance.stopLayout?.()
  graphInstance.updateLayout(currentLayoutConfig())
}

function suspendSemanticRendering() {
  if (!graphInstance) return
  graphInstance.getNodes().forEach(node => {
    node.getContainer().find(element => element.get('type') === 'text')?.hide()
  })
  graphInstance.getEdges().forEach(edge => edge.getContainer().hide())
}

function syncInteractionStates({ syncZoom = true } = {}) {
  if (!graphInstance) return
  const nodes = graphInstance.getNodes()
  const edges = graphInstance.getEdges()
  nodes.forEach(node => graphInstance.clearItemStates(node))
  edges.forEach(edge => graphInstance.clearItemStates(edge))

  const relatedNodeIds = new Set()
  const relatedEdgeIds = new Set()
  if (props.selectedNodeId) {
    relatedNodeIds.add(props.selectedNodeId)
    edges.forEach(edge => {
      const model = edge.getModel()
      if (model.source === props.selectedNodeId || model.target === props.selectedNodeId) {
        relatedEdgeIds.add(model.id)
        relatedNodeIds.add(model.source)
        relatedNodeIds.add(model.target)
      }
    })
  } else if (props.selectedEdgeId) {
    const selectedEdge = graphInstance.findById(props.selectedEdgeId)
    if (selectedEdge) {
      const model = selectedEdge.getModel()
      relatedEdgeIds.add(model.id)
      relatedNodeIds.add(model.source)
      relatedNodeIds.add(model.target)
    }
  }

  relatedNodeIds.forEach(id => {
    if (id !== props.selectedNodeId && graphInstance.findById(id)) graphInstance.setItemState(id, 'related', true)
  })
  relatedEdgeIds.forEach(id => {
    if (id !== props.selectedEdgeId && graphInstance.findById(id)) graphInstance.setItemState(id, 'related', true)
  })

  props.pathNodeIds.forEach(id => {
    if (graphInstance.findById(id)) graphInstance.setItemState(id, 'path', true)
  })
  props.pathEdgeIds.forEach(id => {
    if (graphInstance.findById(id)) graphInstance.setItemState(id, 'path', true)
  })
  props.searchMatchNodeIds.forEach(id => {
    if (graphInstance.findById(id)) graphInstance.setItemState(id, 'search-match', true)
  })
  if (props.selectedNodeId && graphInstance.findById(props.selectedNodeId)) {
    graphInstance.setItemState(props.selectedNodeId, 'selected', true)
  }
  if (props.selectedEdgeId && graphInstance.findById(props.selectedEdgeId)) {
    graphInstance.setItemState(props.selectedEdgeId, 'selected', true)
  }
  restoreSemanticChannels()
  if (syncZoom) syncSemanticZoom(true)
}

function syncSemanticZoom(force = false) {
  if (!graphInstance) return
  const zoom = graphInstance.getZoom()
  const level = zoom < 0.38 ? 'far' : zoom < 0.72 ? 'middle' : 'near'
  if (!force && level === semanticZoomLevel) return
  semanticZoomLevel = level

  graphInstance.getNodes().forEach(node => {
    const model = node.getModel()
    const emphasized = node.hasState('selected') || node.hasState('related') || node.hasState('path') || node.hasState('search-match')
    const showLabel = model._meta?.kind === 'aggregate' || level === 'near' || emphasized
    const label = node.getContainer().find(element => element.get('type') === 'text')
    if (showLabel) label?.show()
    else label?.hide()
  })

  graphInstance.getEdges().forEach(edge => {
    const model = edge.getModel()
    const emphasized = edge.hasState('selected') || edge.hasState('related') || edge.hasState('path')
    const showEdge = model._meta?.kind === 'aggregate' || level !== 'far' || emphasized
    if (showEdge) edge.getContainer().show()
    else edge.getContainer().hide()
    const label = edge.getContainer().find(element => element.get('type') === 'text')
    const showLabel = showEdge && model._displayLabel && (model._meta?.kind === 'aggregate' || level === 'near')
    if (showLabel) label?.show()
    else label?.hide()
  })
}

function restoreSemanticChannels() {
  if (!graphInstance) return
  const theme = effectiveTheme()
  graphInstance.getNodes().forEach(node => {
    const model = node.getModel()
    const fill = analysisVisuals.get(model.id)?.fill || model._meta?.visual_color || model._meta?.color || theme.categoryColors[0]
    node.getKeyShape()?.attr({ fill })
  })
  graphInstance.getEdges().forEach(edge => {
    const model = edge.getModel()
    const color = model._meta?.visual_color || model._meta?.color || theme.edgeDefault
    edge.getKeyShape()?.attr({ stroke: color, lineDash: model._meta?.visual_line_dash || [] })
    const semanticLine = edge.getContainer().find(element => element.get('name') === 'semantic-edge-line')
    const emphasized = edge.hasState('selected') || edge.hasState('path')
    semanticLine?.attr({
      stroke: color,
      lineDash: model._meta?.visual_line_dash || [],
      lineWidth: emphasized ? 4 : edge.hasState('related') ? 3 : model.style?.lineWidth,
      opacity: model.style?.opacity
    })
    const marker = edge.getContainer().find(element => element.get('name') === 'direction-marker')
    marker?.attr({
      fill: color,
      opacity: 1
    })
  })
}

watch(() => [props.nodes, props.edges, props.showEdgeLabels, props.centerNodeId], updateGraph, { deep: true })
watch(() => props.layout, scheduleLayoutUpdate)
watch(
  () => [props.selectedNodeId, props.selectedEdgeId, props.searchMatchNodeIds, props.pathNodeIds, props.pathEdgeIds],
  syncInteractionStates,
  { deep: true }
)
watch(() => props.theme, updateTheme, { deep: true })

onMounted(async () => {
  await nextTick()
  initGraph()
  resizeObserver = new ResizeObserver(() => {
    cancelAnimationFrame(resizeFrame)
    resizeFrame = requestAnimationFrame(() => {
      if (graphInstance && containerRef.value) {
        graphInstance.changeSize(containerRef.value.offsetWidth, containerRef.value.offsetHeight)
      }
    })
  })
  if (containerRef.value) resizeObserver.observe(containerRef.value)
})

onBeforeUnmount(() => {
  cancelAnimationFrame(resizeFrame)
  cancelAnimationFrame(layoutFrame)
  resizeObserver?.disconnect()
  graphInstance?.destroy()
  graphInstance = null
})

function interpolateColor(hex1, hex2, ratio) {
  const parse = color => [
    Number.parseInt(color.slice(1, 3), 16),
    Number.parseInt(color.slice(3, 5), 16),
    Number.parseInt(color.slice(5, 7), 16)
  ]
  const start = parse(hex1)
  const end = parse(hex2)
  return `#${start.map((value, index) => Math.round(value + (end[index] - value) * ratio).toString(16).padStart(2, '0')).join('')}`
}

function applyAnalysisVisuals() {
  if (!graphInstance) return
  const theme = effectiveTheme()
  analysisVisuals.forEach((visual, id) => {
    const item = graphInstance.findById(id)
    if (!item) return
    graphInstance.updateItem(item, {
      style: { fill: visual.fill, ...(visual.radius ? { r: visual.radius } : {}) },
      ...(visual.radius ? { size: visual.radius * 2 } : {}),
      labelCfg: { style: { fill: getContrastingTextColor(visual.fill, theme.labelLight, theme.labelDark) } }
    })
  })
}

defineExpose({
  focusNodes(nodeIds) {
    if (!graphInstance || !nodeIds?.length) return
    const first = graphInstance.findById(nodeIds[0])
    if (first && nodeIds.every(id => graphInstance.findById(id))) {
      graphInstance.focusItem(first, true, { duration: 300, easing: 'easeCubic' })
      pendingFocusIds = null
    } else {
      pendingFocusIds = [...nodeIds]
    }
  },
  fitView() {
    graphInstance?.fitView()
  },
  applyScoreColors(nodeScores, mode = 'gradient', sizeMapping = false) {
    if (!graphInstance || !nodeScores?.length) return
    const theme = effectiveTheme()
    const scores = nodeScores.map(node => node.score)
    const minScore = Math.min(...scores)
    const maxScore = Math.max(...scores)
    const range = maxScore - minScore || 1
    nodeScores.forEach(node => {
      const ratio = (node.score - minScore) / range
      const communityIndex = theme.categoryColors.length
        ? Math.abs(Number(node.community_id) % theme.categoryColors.length)
        : 0
      const fill = mode === 'community'
        ? theme.categoryColors[communityIndex] || theme.analysisLow
        : interpolateColor(theme.analysisLow, theme.analysisHigh, ratio)
      analysisVisuals.set(node.node_id, {
        fill,
        radius: sizeMapping ? 20 + Math.round(ratio * 20) : null
      })
    })
    applyAnalysisVisuals()
  },
  resetNodeColors() {
    if (!graphInstance) return
    analysisVisuals.clear()
    const theme = effectiveTheme()
    graphInstance.getNodes().forEach(node => {
      const model = node.getModel()
      const fill = model._meta?.visual_color || model._meta?.color || theme.categoryColors[0]
      const radius = model._baseRadius
      graphInstance.updateItem(node, {
        style: { fill, r: radius },
        size: radius * 2,
        labelCfg: { style: { fill: getContrastingTextColor(fill, theme.labelLight, theme.labelDark) } }
      })
    })
    syncInteractionStates()
  }
})
</script>

<style scoped>
.graph-canvas {
  width: 100%;
  height: 100%;
  position: relative;
  background: var(--addp-bg-primary);
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
