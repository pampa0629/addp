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
import { getContrastingTextColor, readGraphTheme, graphNodeItemId as nodeItemId, graphEdgeItemId as edgeItemId } from '@addp/common-frontend/graph'
import { placeIncrementalNodes } from '../utils/incrementalGraphLayout'
import { createGraphLayoutConfig } from '../utils/graphLayoutPolicy'

const SEMANTIC_EDGE_TYPE = 'addp-semantic-quadratic'

for (const parent of ['quadratic', 'loop']) {
  G6.registerEdge(`addp-semantic-${parent}`, {
    getPathPoints(config) {
      if (parent !== 'loop') return config
      const center = config.sourceNode.getModel()
      return G6.Shape.getFactory('edge').getShape('loop').getPathPoints({
        ...config, startPoint: { x: center.x, y: center.y }, endPoint: { x: center.x, y: center.y }, controlPoints: undefined
      })
    },
    afterDraw(config, group) {
      renderSemanticEdge(this, config, group)
    },
    afterUpdate(config, item) {
      const group = item.getContainer()
      renderSemanticEdge(this, config, group)
      const matrix = group.getTotalMatrix()
      syncRenderedSize(item, matrix ? Math.hypot(matrix[0], matrix[1]) : 1)
    }
  }, parent)
}

function renderSemanticEdge(shape, config, group) {
  const points = shape.getPathPoints({ ...config })
  const controls = shape.getControlPoints(points) || []
  const path = shape.getPath([points.startPoint, ...controls, points.endPoint])
  updateSemanticEdgeLine(config, group, path)
  updateDirectionMarker(config, group, path)
}

function updateSemanticEdgeLine(config, group, path) {
  const existing = group.find(element => element.get('name') === 'semantic-edge-line')
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

function updateDirectionMarker(config, group, edgePath) {
  const existing = group.find(element => element.get('name') === 'direction-marker')
  const path = config._meta?.directed === false ? null : directionMarkerPath(edgePath)
  if (!path) {
    existing?.hide()
    return
  }
  const attrs = { path, fill: config._meta?.visual_color || config._meta?.color }
  if (existing) {
    existing.set('unscaledPath', path)
    existing.show()
    existing.attr(attrs)
  } else {
    group.addShape('path', { name: 'direction-marker', unscaledPath: path, attrs })
  }
}

function directionMarkerPath(edgePath) {
  const segment = edgePath?.at(-1)
  if (!segment || edgePath.length < 2) return null
  const [endX, endY] = segment.slice(-2)
  const [previousX, previousY] = segment[0] === 'L' ? edgePath.at(-2).slice(-2) : segment.slice(-4, -2)
  const end = { x: endX, y: endY }, previous = { x: previousX, y: previousY }
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
const analysisVisuals = new Map()

const isEmpty = computed(() => props.nodes.length === 0 && !props.loading)

function effectiveTheme() {
  const cssTheme = readGraphTheme(containerRef.value)
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
  const data = {
    nodes: nodes.map(node => {
      const isCenter = props.centerNodeId && node.id === props.centerNodeId
      const isAggregate = node.kind === 'aggregate'
      const aggregateRadius = Math.min(54, Math.max(34, 28 + Math.log2((node.member_count || 1) + 1) * 2.5))
      const radius = isAggregate ? aggregateRadius : isCenter ? density.centerRadius : density.radius
      const fill = node.visual_color || node.color || theme.categoryColors[0]
      const baseLabel = (node.display_name || node.id).toString().substring(0, density.labelMaxLength)
      const displayLabel = isAggregate ? `${baseLabel}\n${node.member_count || 0}` : baseLabel
      return {
        id: nodeItemId(node.id),
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
            fontFamily: theme.fontFamily,
            fontSize: density.labelFontSize,
            fontWeight: 500
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
        id: edgeItemId(edge.id || `generated-${index}`),
        source: nodeItemId(edge.source),
        target: nodeItemId(edge.target),
        curveOffset: 0,
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
            fontFamily: theme.fontFamily,
            fontSize: 11,
            fontWeight: 500,
            stroke: theme.edgeLabelStroke,
            lineWidth: 3
          },
          autoRotate: true
        }
      }
    })
  }
  G6.Util.processParallelEdges(data.edges, 20, SEMANTIC_EDGE_TYPE, SEMANTIC_EDGE_TYPE, 'addp-semantic-loop')
  return data
}

function currentLayoutConfig() {
  return createGraphLayoutConfig(
    props.layout,
    props.nodes.length,
    props.edges.length,
    props.selectedNodeId ? nodeItemId(props.selectedNodeId) : '',
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

  graphInstance.on('beforelayout', () => {
    layoutInProgress.value = true
    suspendSemanticRendering()
  })

  graphInstance.on('afterlayout', () => {
    const manualLayout = layoutCompletionReason === 'layout'
    if (!manualLayout) {
      syncInteractionStates({ syncZoom: false })
      applyAnalysisVisuals()
    }
    layoutInProgress.value = false
    if (!focusPendingNodes()) fitView()
    syncSemanticZoom()
    layoutCompletionReason = ''
  })

  graphInstance.on('viewportchange', () => syncSemanticZoom())

  graphInstance.on('node:click', event => {
    const model = event.item.getModel()
    emit('node-click', model._meta.id)
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
    syncSemanticZoom()
    return
  }

  if (retainedCount > 0 && addedCount > 0 && applyIncrementalPositions(data, existingPositions)) {
    graphInstance.changeData(data)
    syncInteractionStates({ syncZoom: false })
    applyAnalysisVisuals()
    syncSemanticZoom()
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
  return placeIncrementalNodes(data, existingPositions, props.expansionAnchorId ? nodeItemId(props.expansionAnchorId) : '')
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
  const selectedNodeId = props.selectedNodeId ? nodeItemId(props.selectedNodeId) : ''
  const selectedEdgeId = props.selectedEdgeId ? edgeItemId(props.selectedEdgeId) : ''

  const relatedNodeIds = new Set()
  const relatedEdgeIds = new Set()
  if (selectedNodeId) {
    relatedNodeIds.add(selectedNodeId)
    edges.forEach(edge => {
      const model = edge.getModel()
      if (model.source === selectedNodeId || model.target === selectedNodeId) {
        relatedEdgeIds.add(model.id)
        relatedNodeIds.add(model.source)
        relatedNodeIds.add(model.target)
      }
    })
  } else if (selectedEdgeId) {
    const selectedEdge = graphInstance.findById(selectedEdgeId)
    if (selectedEdge) {
      const model = selectedEdge.getModel()
      relatedEdgeIds.add(model.id)
      relatedNodeIds.add(model.source)
      relatedNodeIds.add(model.target)
    }
  }

  relatedNodeIds.forEach(id => {
    if (id !== selectedNodeId && graphInstance.findById(id)) graphInstance.setItemState(id, 'related', true)
  })
  relatedEdgeIds.forEach(id => {
    if (id !== selectedEdgeId && graphInstance.findById(id)) graphInstance.setItemState(id, 'related', true)
  })

  props.pathNodeIds.map(nodeItemId).forEach(id => {
    if (graphInstance.findById(id)) graphInstance.setItemState(id, 'path', true)
  })
  props.pathEdgeIds.map(edgeItemId).forEach(id => {
    if (graphInstance.findById(id)) graphInstance.setItemState(id, 'path', true)
  })
  props.searchMatchNodeIds.map(nodeItemId).forEach(id => {
    if (graphInstance.findById(id)) graphInstance.setItemState(id, 'search-match', true)
  })
  if (selectedNodeId && graphInstance.findById(selectedNodeId)) {
    graphInstance.setItemState(selectedNodeId, 'selected', true)
  }
  if (selectedEdgeId && graphInstance.findById(selectedEdgeId)) {
    graphInstance.setItemState(selectedEdgeId, 'selected', true)
  }
  restoreSemanticChannels()
  if (syncZoom) syncSemanticZoom()
}

function focusPendingNodes() {
  if (!graphInstance || !pendingFocusIds?.length || !pendingFocusIds.every(id => graphInstance.findById(id))) return false
  const ids = pendingFocusIds
  pendingFocusIds = null
  if (ids.length === 1) {
    graphInstance.focusItem(graphInstance.findById(ids[0]), true, { duration: 300, easing: 'easeCubic' })
  } else {
    fitView()
  }
  return true
}

function fitView() {
  // Fit sparse graphs at natural size instead of requesting an oversized zoom.
  graphInstance?.fitView(undefined, { onlyOutOfViewPort: true })
}

function syncRenderedSize(item, zoom) {
  const scale = 1 / Math.max(1, zoom)
  const model = item.getModel()
  const group = item.getContainer()
  const state = ['selected', 'search-match', 'path', 'related'].find(name => item.hasState(name))
  const style = { ...model.style, ...(state ? item.getStateStyle(state) : {}) }
  item.getKeyShape()?.attr({
    lineWidth: style.lineWidth * scale,
    lineDash: (model.style?.lineDash || []).map(length => length * scale),
    lineAppendWidth: (model.style?.lineAppendWidth || 0) * scale,
    shadowBlur: (style.shadowBlur || 0) * scale
  })
  const label = group.find(element => element.get('type') === 'text')
  const fontSize = model.labelCfg.style.fontSize
  label?.attr({
    fontSize: Math.min(fontSize, 14 / zoom),
    lineWidth: (model.labelCfg.style.lineWidth || 0) * scale
  })
  group.find(element => element.get('name') === 'semantic-edge-line')?.attr({
    lineWidth: style.lineWidth * scale,
    lineDash: (model.style?.lineDash || []).map(length => length * scale)
  })
  const marker = group.find(element => element.get('name') === 'direction-marker')
  const path = marker?.get('unscaledPath')
  if (path) {
    const [, x, y] = path[0]
    marker.attr({ path: path.map(([command, px, py]) => command === 'Z' ? ['Z']
      : [command, x + (px - x) * scale, y + (py - y) * scale]) })
  }
}

function syncRenderedSizes() {
  if (!graphInstance) return
  const zoom = graphInstance.getZoom()
  graphInstance.getNodes().forEach(node => syncRenderedSize(node, zoom))
  graphInstance.getEdges().forEach(edge => syncRenderedSize(edge, zoom))
}

function syncSemanticZoom() {
  if (!graphInstance) return
  syncRenderedSizes()
  const zoom = graphInstance.getZoom()
  const level = zoom < 0.38 ? 'far' : zoom < 0.72 ? 'middle' : 'near'

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
  syncRenderedSizes()
}

defineExpose({
  focusNodes(nodeIds) {
    if (!graphInstance || !nodeIds?.length) return
    pendingFocusIds = nodeIds.map(nodeItemId)
    if (!layoutInProgress.value) focusPendingNodes()
  },
  fitView,
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
      analysisVisuals.set(nodeItemId(node.node_id), {
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
