<template>
  <div class="lineage-viewer">
      <div class="lineage-toolbar">
        <div class="lineage-summary">
          <span>{{ t('lineage.summary', { nodes: nodes.length, edges: edges.length }) }}</span>
          <span class="lineage-legend">
            <span class="lineage-legend-dot lineage-legend-dot-current" />
            {{ t('lineage.currentItem') }}
          </span>
          <span class="lineage-legend">
            <span class="lineage-legend-dot" />
            {{ t('lineage.relatedItem') }}
          </span>
        </div>
        <div class="lineage-tools">
          <span class="lineage-depth-label">{{ t('lineage.depth') }}</span>
          <el-select :model-value="depth" :aria-label="t('lineage.depth')" class="lineage-depth" size="small" @update:model-value="emit('update:depth', $event)">
            <el-option v-for="value in [1, 2, 3, 5, 10, 20]" :key="value" :value="value" :label="t('lineage.layers', { count: value })" />
          </el-select>
          <el-tooltip :content="t('lineage.zoomOut')" placement="bottom">
            <el-button text circle size="small" :aria-label="t('lineage.zoomOut')" @click="zoomBy(0.8)">
              <el-icon><ZoomOut /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip :content="t('lineage.zoomIn')" placement="bottom">
            <el-button text circle size="small" :aria-label="t('lineage.zoomIn')" @click="zoomBy(1.25)">
              <el-icon><ZoomIn /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip :content="t('lineage.fitView')" placement="bottom">
            <el-button text circle size="small" :aria-label="t('lineage.fitView')" @click="fitView">
              <el-icon><FullScreen /></el-icon>
            </el-button>
          </el-tooltip>
        </div>
      </div>

      <div v-if="graph.truncated" class="lineage-truncated" role="status">{{ t('lineage.truncated') }}</div>
      <div class="lineage-stage">
        <el-empty v-if="!nodes.length" :description="t('lineage.noData')" :image-size="56" />
        <div
          v-else
          ref="canvasRef"
          class="lineage-canvas"
          role="img"
          :aria-label="t('lineage.graphLabel')"
        />
      </div>

      <div v-if="selectedNode" class="lineage-inspector" aria-live="polite">
        <div class="lineage-inspector-heading">
          <span class="lineage-inspector-kind">{{ nodeTypeLabel(selectedNode) }}</span>
          <strong :title="nodeDisplayName(selectedNode)">{{ nodeDisplayName(selectedNode) }}</strong>
          <span v-if="selectedNode.full_name" class="lineage-inspector-path" :title="selectedNode.full_name">{{ selectedNode.full_name }}</span>
        </div>
        <div class="lineage-expand-actions">
          <el-button v-for="direction in ['upstream', 'downstream']" v-show="selectedNode[`hidden_${direction}_count`] > 0" :key="direction" size="small" :disabled="graph.truncated" @click="expandNode(selectedNode, direction)">{{ t(`lineage.expand.${direction}`, { count: selectedNode[`hidden_${direction}_count`] }) }}</el-button>
        </div>
        <dl class="lineage-inspector-fields">
          <div v-if="selectedNode.engine_name">
            <dt>{{ t('lineage.engine') }}</dt>
            <dd>{{ selectedNode.engine_name }}</dd>
          </div>
          <div v-if="selectedNode.engine_id">
            <dt>{{ t('lineage.engineId') }}</dt>
            <dd>{{ selectedNode.engine_id }}</dd>
          </div>
          <div v-if="selectedNode.item_id">
            <dt>{{ t('lineage.itemId') }}</dt>
            <dd>{{ selectedNode.item_id }}</dd>
          </div>
          <div v-if="selectedNode.item_fingerprint" class="lineage-inspector-field-wide">
            <dt>{{ t('lineage.fingerprint') }}</dt>
            <dd class="lineage-mono">{{ selectedNode.item_fingerprint }}</dd>
          </div>
          <div v-if="selectedNode.service_id"><dt>{{ t('lineage.serviceId') }}</dt><dd>{{ selectedNode.service_id }}</dd></div>
          <div v-if="selectedNode.published_revision">
            <dt>{{ t('lineage.revision') }}</dt>
            <dd>{{ selectedNode.published_revision }}</dd>
          </div>
        </dl>
      </div>

      <div v-else-if="selectedEdge" class="lineage-inspector" aria-live="polite">
        <div class="lineage-inspector-heading">
          <span class="lineage-inspector-kind">{{ t('lineage.relationship') }}</span>
          <strong>{{ relationLabel(selectedEdge.relation_kind) }}</strong>
          <span class="lineage-inspector-path">
            {{ nodeQualifiedName(selectedEdge.source) }} → {{ nodeQualifiedName(selectedEdge.target) }}
          </span>
        </div>
        <dl class="lineage-inspector-fields">
          <div>
            <dt>{{ t('lineage.granularity') }}</dt>
            <dd>{{ granularityLabel(selectedEdge.granularity) }}</dd>
          </div>
          <div v-if="selectedEdge.last_observed_at">
            <dt>{{ t('lineage.lastObservedAt') }}</dt>
            <dd>{{ formatDateTime(selectedEdge.last_observed_at) }}</dd>
          </div>
          <div v-if="selectedEdge.evidence?.execution_id" class="lineage-inspector-field-wide">
            <dt>{{ t('lineage.executionId') }}</dt>
            <dd class="lineage-mono">{{ selectedEdge.evidence.execution_id }} <el-button link type="primary" @click="emit('view-execution', selectedEdge.evidence.execution_id)">{{ t('lineage.viewExecution') }}</el-button></dd>
          </div>
        </dl>
      </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { FullScreen, ZoomIn, ZoomOut } from '@element-plus/icons-vue'
import G6 from '@antv/g6'
import { focusDAGConnections } from '../../dag/src/utils/connections.js'

const LINEAGE_NODE_TYPE = 'addp-lineage-card'
const LINEAGE_EDGE_TYPE = 'addp-lineage-link'
const NODE_WIDTH = 280
const NODE_HEIGHT = 108
const FIT_PADDING = 48

const { t, locale } = useI18n()
const props = defineProps({
  graph: { type: Object, default: () => ({ nodes: [], edges: [] }) },
  depth: { type: Number, default: 2 }
})

const emit = defineEmits(['update:depth', 'expand', 'view-execution'])
const canvasRef = ref(null)
const selectedNode = ref(null)
const selectedEdge = ref(null)
let graphInstance
let resizeObserver
let themeObserver
let observedCanvas
let expansionAnchor
let renderSequence = 0

function nodeId(node) {
  if (!node) return ''
  if (node.kind === 'published_service') return `service:${node.service_id}:${node.published_revision}`
  return node.item_id ? `item:${node.item_id}` : ''
}

const nodes = computed(() => {
  const unique = new Map()
  for (const node of props.graph?.nodes || []) {
    const id = nodeId(node)
    if (id && !unique.has(id)) unique.set(id, node)
  }
  return [...unique.values()]
})

const edges = computed(() => props.graph?.edges || [])
const subjectId = computed(() => nodeId(props.graph?.subject))

function themeColor(variableName) {
  if (typeof window === 'undefined') return ''
  const rootValue = getComputedStyle(document.documentElement).getPropertyValue(variableName).trim()
  if (rootValue) return rootValue
  return canvasRef.value ? getComputedStyle(canvasRef.value).getPropertyValue(variableName).trim() : ''
}

function themePalette() {
  return {
    background: themeColor('--addp-bg-primary'),
    canvas: themeColor('--addp-bg-secondary'),
    border: themeColor('--addp-border-color'),
    borderLight: themeColor('--addp-border-color-light'),
    textPrimary: themeColor('--addp-text-primary'),
    textSecondary: themeColor('--addp-text-secondary'),
    textTertiary: themeColor('--addp-text-tertiary'),
    primary: themeColor('--el-color-primary'),
    primarySoft: themeColor('--el-color-primary-light-9'),
    primaryHover: themeColor('--el-color-primary-light-3'),
    warning: themeColor('--el-color-warning'),
    white: themeColor('--el-color-white')
  }
}

function nodeDisplayName(node) {
  return String(node?.name || node?.full_name || nodeTypeLabel(node))
}

function nodeQualifiedName(node) {
  return String(node?.full_name || nodeDisplayName(node))
}

function nodePath(node) {
  if (node?.full_name && node.full_name !== node.name) return String(node.full_name)
  return ''
}

function nodeTypeLabel(node) {
  if (node?.kind === 'published_service') return t('lineage.types.publishedService')
  const key = {
    table: 'table',
    object: 'object',
    topic: 'topic',
    collection: 'collection'
  }[String(node?.item_type || '').toLowerCase()]
  return key ? t(`lineage.types.${key}`) : t('lineage.types.dataItem')
}

function relationLabel(kind) {
  if (kind === 'derive') return t('lineage.relations.derive')
  if (kind === 'serve') return t('lineage.relations.serve')
  return kind || t('lineage.relationship')
}

function granularityLabel(granularity) {
  if (granularity === 'field') return t('lineage.granularities.field')
  if (granularity === 'item') return t('lineage.granularities.item')
  return granularity || t('lineage.granularities.item')
}

function truncate(value, length) {
  const text = String(value || '')
  return text.length > length ? `${text.slice(0, length - 1)}…` : text
}

function formatDateTime(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return String(value || '')
  return new Intl.DateTimeFormat(locale.value === 'en' ? 'en-US' : 'zh-CN', {
    dateStyle: 'medium',
    timeStyle: 'short'
  }).format(date)
}

function registerLineageNode() {
  if (G6.getNodeType?.(LINEAGE_NODE_TYPE)) return
  G6.registerNode(LINEAGE_NODE_TYPE, {
    draw(cfg, group) {
      const visual = cfg._visual
      const card = group.addShape('rect', {
        attrs: {
          x: -NODE_WIDTH / 2,
          y: -NODE_HEIGHT / 2,
          width: NODE_WIDTH,
          height: NODE_HEIGHT,
          radius: 6,
          fill: visual.fill,
          stroke: visual.stroke,
          lineWidth: visual.lineWidth,
          cursor: 'pointer'
        },
        name: 'lineage-card',
        draggable: false
      })

      group.addShape('rect', {
        attrs: {
          x: -NODE_WIDTH / 2,
          y: -NODE_HEIGHT / 2 + 6,
          width: 4,
          height: NODE_HEIGHT - 12,
          radius: 2,
          fill: visual.accent
        },
        name: 'lineage-accent',
        capture: false
      })

      group.addShape('text', {
        attrs: {
          text: cfg._title,
          x: -NODE_WIDTH / 2 + 18,
          y: -31,
          textAlign: 'left',
          textBaseline: 'middle',
          fill: visual.textPrimary,
          fontSize: 14,
          fontWeight: 600
        },
        name: 'lineage-title',
        capture: false
      })

      if (cfg._path) {
        group.addShape('text', {
          attrs: {
            text: cfg._path,
            x: -NODE_WIDTH / 2 + 18,
            y: -10,
            textAlign: 'left',
            textBaseline: 'middle',
            fill: visual.textSecondary,
            fontSize: 11
          },
          name: 'lineage-path',
          capture: false
        })
      }

      if (cfg._engineName || cfg._engineIdentifier) {
        group.addShape('circle', {
          attrs: {
            x: -NODE_WIDTH / 2 + 21,
            y: 14,
            r: 3,
            fill: visual.textSecondary
          },
          name: 'lineage-engine-dot',
          capture: false
        })

        group.addShape('text', {
          attrs: {
            text: cfg._engineName,
            x: -NODE_WIDTH / 2 + 30,
            y: 14,
            textAlign: 'left',
            textBaseline: 'middle',
            fill: visual.textSecondary,
            fontSize: 10,
            fontWeight: 500
          },
          name: 'lineage-engine',
          capture: false
        })

        group.addShape('text', {
          attrs: {
            text: cfg._engineIdentifier,
            x: NODE_WIDTH / 2 - 14,
            y: 14,
            textAlign: 'right',
            textBaseline: 'middle',
            fill: visual.textSecondary,
            fontSize: 10,
            fontWeight: 500
          },
          name: 'lineage-engine-identifier',
          capture: false
        })
      }

      group.addShape('circle', {
        attrs: {
          x: -NODE_WIDTH / 2 + 21,
          y: 38,
          r: 3,
          fill: visual.accent
        },
        name: 'lineage-kind-dot',
        capture: false
      })

      group.addShape('text', {
        attrs: {
          text: cfg._typeLabel,
          x: -NODE_WIDTH / 2 + 30,
          y: 38,
          textAlign: 'left',
          textBaseline: 'middle',
          fill: visual.textTertiary,
          fontSize: 10
        },
        name: 'lineage-kind',
        capture: false
      })

      if (cfg._identifier) {
        group.addShape('text', {
          attrs: {
            text: cfg._identifier,
            x: NODE_WIDTH / 2 - 14,
            y: 38,
            textAlign: 'right',
            textBaseline: 'middle',
            fill: visual.textTertiary,
            fontSize: 10
          },
          name: 'lineage-identifier',
          capture: false
        })
      }

      if (cfg._isSubject) {
        group.addShape('rect', {
          attrs: {
            x: NODE_WIDTH / 2 - 66,
            y: -NODE_HEIGHT / 2 + 10,
            width: 52,
            height: 20,
            radius: 4,
            fill: visual.accent
          },
          name: 'lineage-current-badge',
          capture: false
        })
        group.addShape('text', {
          attrs: {
            text: cfg._currentLabel,
            x: NODE_WIDTH / 2 - 40,
            y: -NODE_HEIGHT / 2 + 20,
            textAlign: 'center',
            textBaseline: 'middle',
            fill: visual.badgeText,
            fontSize: 10,
            fontWeight: 600
          },
          name: 'lineage-current-label',
          capture: false
        })
      }

      for (const direction of ['upstream', 'downstream']) {
        const count = cfg._node[`hidden_${direction}_count`]
        if (!count) continue
        const x = direction === 'upstream' ? -NODE_WIDTH / 2 - 38 : NODE_WIDTH / 2 + 38
        group.addShape('rect', { name: `lineage-expand-${direction}`, attrs: { x: x - 34, y: -12, width: 68, height: 24, radius: 12, fill: visual.fill, stroke: visual.accent, cursor: 'pointer' } })
        group.addShape('text', { name: `lineage-expand-${direction}`, attrs: { x, y: 0, text: cfg._expandLabels[direction], textAlign: 'center', textBaseline: 'middle', fill: visual.accent, fontSize: 11, cursor: 'pointer' } })
      }
      return card
    },

    setState(name, value, item) {
      const model = item.getModel()
      const visual = model._visual
      const card = item.getContainer().find(shape => shape.get('name') === 'lineage-card')
      if (!card || !visual) return
      if (name === 'selected') {
        card.attr({
          stroke: value ? visual.selectedStroke : visual.stroke,
          lineWidth: value ? 2.5 : visual.lineWidth
        })
      }
      if (name === 'hover' && !item.hasState('selected')) {
        card.attr({
          stroke: value ? visual.hoverStroke : visual.stroke,
          lineWidth: value ? 2 : visual.lineWidth
        })
      }
    },

    getAnchorPoints() {
      return [[0, 0.5], [1, 0.5]]
    }
  }, 'single-node')
}

function graphData() {
  const palette = themePalette()
  return {
    nodes: nodes.value.map(node => {
      const isSubject = nodeId(node) === subjectId.value
      const accent = node.kind === 'published_service' ? palette.warning : palette.primary
      return {
        id: nodeId(node),
        type: LINEAGE_NODE_TYPE,
        size: [NODE_WIDTH, NODE_HEIGHT],
        _node: node,
        _expandLabels: Object.fromEntries(['upstream', 'downstream'].map(direction => [direction, t(`lineage.expand.${direction}`, { count: node[`hidden_${direction}_count`] })])),
        _title: truncate(nodeDisplayName(node), isSubject ? 23 : 30),
        _path: truncate(nodePath(node), 40),
        _engineName: truncate(node.engine_name, 29),
        _engineIdentifier: node.engine_id ? t('lineage.engineIdentifier', { id: node.engine_id }) : '',
        _typeLabel: nodeTypeLabel(node),
        _identifier: node.item_id ? t('lineage.itemIdentifier', { id: node.item_id }) : '',
        _isSubject: isSubject,
        _currentLabel: t('lineage.current'),
        _visual: {
          fill: isSubject ? palette.primarySoft : palette.background,
          stroke: isSubject ? palette.primary : palette.border,
          selectedStroke: palette.primary,
          hoverStroke: palette.primaryHover,
          lineWidth: isSubject ? 2 : 1,
          accent,
          textPrimary: palette.textPrimary,
          textSecondary: palette.textSecondary,
          textTertiary: palette.textTertiary,
          badgeText: palette.white
        }
      }
    }),
    edges: edges.value.map((edge, index) => ({
      id: `lineage-edge:${index}`,
      source: nodeId(edge.source),
      sourceAnchor: 1,
      targetAnchor: 0,
      target: nodeId(edge.target),
      label: relationLabel(edge.relation_kind),
      _edge: edge,
      style: {
        stroke: palette.textTertiary,
        lineWidth: 1.5,
        radius: 8,
        offset: 28,
        lineAppendWidth: 12,
        endArrow: {
          path: G6.Arrow.triangle(8, 6, 0),
          fill: palette.textTertiary
        }
      },
      labelCfg: {
        autoRotate: false,
        refY: -10,
        style: {
          fill: palette.textSecondary,
          stroke: palette.canvas,
          lineWidth: 4,
          fontSize: 11,
          fontWeight: 500
        }
      }
    }))
  }
}

// The halo belongs to each edge group: the upper edge masks the lower edge at
// crossings, without drawing a false junction. Hover brings the whole group up.
function registerLineageEdge() {
  G6.registerEdge(LINEAGE_EDGE_TYPE, {
    afterDraw(cfg, group) {
      const key = group.get('children')[0]
      const halo = group.addShape('path', {
        attrs: { path: key.attr('path'), stroke: themePalette().canvas, lineWidth: 7, lineJoin: 'round' },
        name: 'lineage-edge-halo', capture: false
      })
      halo.toBack()
    },
    afterUpdate(cfg, item) {
      const halo = item.getContainer().find(shape => shape.get('name') === 'lineage-edge-halo')
      halo?.attr('path', item.getKeyShape().attr('path'))
    }
  }, 'polyline')
}

function focusItem(item) {
  if (!graphInstance) return
  const focused = focusDAGConnections(graphInstance, item)
  for (const edge of graphInstance.getEdges()) {
    graphInstance.setItemState(edge, 'hover', focused.includes(edge))
  }
  for (const edge of focused) {
    edge.getSource().toFront()
    edge.getTarget().toFront()
  }
}

function restoreFocus() {
  const selected = [...(graphInstance?.getNodes() || []), ...(graphInstance?.getEdges() || [])].find(item => item.hasState('selected'))
  focusItem(selected)
}

function fitView() {
  if (!graphInstance) return
  graphInstance.fitView(FIT_PADDING)
  if (graphInstance.getZoom() > 1) {
    graphInstance.zoomTo(1)
    graphInstance.fitCenter()
  }
}

function zoomBy(ratio) {
  if (!graphInstance) return
  const nextZoom = Math.min(2.5, Math.max(0.1, graphInstance.getZoom() * ratio))
  graphInstance.zoomTo(nextZoom)
}

function clearSelection() {
  graphInstance?.getNodes().forEach(item => graphInstance.setItemState(item, 'selected', false))
  graphInstance?.getEdges().forEach(item => graphInstance.setItemState(item, 'selected', false))
  selectedNode.value = null
  selectedEdge.value = null
  focusItem(null)
}

function selectNode(item) {
  clearSelection()
  graphInstance.setItemState(item, 'selected', true)
  selectedNode.value = item.getModel()._node
  focusItem(item)
}

function selectEdge(item) {
  clearSelection()
  graphInstance.setItemState(item, 'selected', true)
  selectedEdge.value = item.getModel()._edge
  focusItem(item)
}

function destroyGraph() {
  graphInstance?.destroy()
  graphInstance = undefined
}

function observeCanvasSize() {
  if (!resizeObserver || !canvasRef.value || observedCanvas === canvasRef.value) return
  resizeObserver.disconnect()
  observedCanvas = canvasRef.value
  resizeObserver.observe(observedCanvas)
}

function expandNode(node, direction) {
  if (props.graph.truncated || !node.item_id) return
  const item = graphInstance?.findById(nodeId(node))
  if (item) {
    const { x, y } = item.getModel()
    expansionAnchor = { id: nodeId(node), subject: subjectId.value, zoom: graphInstance.getZoom(), point: graphInstance.getCanvasByPoint(x, y) }
  }
  emit('expand', { item_id: node.item_id, direction })
}

async function renderGraph() {
  const sequence = ++renderSequence
  const anchor = expansionAnchor?.subject === subjectId.value ? expansionAnchor : null
  expansionAnchor = null
  destroyGraph()
  if (!anchor) { selectedNode.value = null; selectedEdge.value = null }
  else if (selectedNode.value) selectedNode.value = nodes.value.find(node => nodeId(node) === nodeId(selectedNode.value)) || null
  await nextTick()
  if (sequence !== renderSequence) return
  if (!canvasRef.value || !nodes.value.length) return
  observeCanvasSize()

  const width = canvasRef.value.clientWidth
  if (width <= 0) return

  registerLineageNode()
  registerLineageEdge()
  const palette = themePalette()
  graphInstance = new G6.Graph({
    container: canvasRef.value,
    width,
    height: canvasRef.value.clientHeight,
    minZoom: 0.1,
    maxZoom: 2.5,
    modes: { default: ['drag-canvas', 'zoom-canvas'] },
    plugins: [new G6.Tooltip({
      itemTypes: ['node'], offsetX: 12, offsetY: 12,
      getContent(event) {
        const content = document.createElement('div')
        content.textContent = nodeQualifiedName(event.item.getModel()._node)
        content.style.cssText = 'max-width: 480px; overflow-wrap: anywhere; padding: 8px 12px; background: var(--addp-bg-primary); color: var(--addp-text-primary); border: 1px solid var(--addp-border-color); border-radius: 4px;'
        return content
      }
    })],
    layout: { type: 'dagre', rankdir: 'LR', nodesep: 48, ranksep: 170, controlPoints: true },
    defaultNode: { type: LINEAGE_NODE_TYPE, size: [NODE_WIDTH, NODE_HEIGHT] },
    defaultEdge: { type: LINEAGE_EDGE_TYPE },
    edgeStateStyles: {
      selected: { stroke: palette.primary, lineWidth: 2.5 },
      hover: { stroke: palette.primaryHover, lineWidth: 2 }
    }
  })

  graphInstance.on('node:click', event => {
    const shape = event.target?.get('name')
    const direction = ['upstream', 'downstream'].find(value => shape === `lineage-expand-${value}`)
    if (direction) expandNode(event.item.getModel()._node, direction)
    else selectNode(event.item)
  })
  graphInstance.on('edge:click', event => selectEdge(event.item))
  graphInstance.on('canvas:click', clearSelection)
  graphInstance.on('node:mouseenter', event => focusItem(event.item))
  graphInstance.on('node:mouseleave', restoreFocus)
  graphInstance.on('edge:mouseenter', event => focusItem(event.item))
  graphInstance.on('edge:mouseleave', restoreFocus)
  const restoreViewport = () => {
    if (!graphInstance) return
    const item = anchor && graphInstance.findById(anchor.id)
    if (!item) { fitView(); return }
    graphInstance.zoomTo(anchor.zoom)
    const { x, y } = item.getModel()
    const point = graphInstance.getCanvasByPoint(x, y)
    graphInstance.translate(anchor.point.x - point.x, anchor.point.y - point.y)
  }
  graphInstance.once('afterrender', restoreViewport)
  graphInstance.data(graphData())
  graphInstance.render()

}

onMounted(() => {
  resizeObserver = new ResizeObserver(entries => {
    const width = Math.floor(entries[0]?.contentRect?.width || canvasRef.value?.clientWidth || 0)
    if (width <= 0) return
    if (!graphInstance) {
      renderGraph()
      return
    }
    const height = Math.floor(entries[0]?.contentRect?.height || canvasRef.value?.clientHeight || 0)
    if (height <= 0) return
    graphInstance.changeSize(width, height)
    // Resizing the inspector/canvas must not reset user zoom or local expansion.
  })

  themeObserver = new MutationObserver(mutations => {
    if (mutations.some(mutation => mutation.attributeName === 'class')) renderGraph()
  })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  renderGraph()
})

onUnmounted(() => {
  renderSequence++
  resizeObserver?.disconnect()
  observedCanvas = undefined
  themeObserver?.disconnect()
  destroyGraph()
})

watch(() => props.graph, renderGraph, { deep: true })
watch(locale, renderGraph)
watch(() => props.depth, () => { expansionAnchor = null })
</script>

<style scoped>
.lineage-viewer {
  position: relative;
  width: 100%;
  min-height: 0;
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
}

.lineage-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
  flex-shrink: 0;
  min-height: 42px;
  padding: 0 12px;
  border-bottom: 1px solid var(--addp-border-color-light);
  font-size: 12px;
}

.lineage-summary,
.lineage-tools,
.lineage-legend {
  display: flex;
  align-items: center;
}

.lineage-summary {
  flex-wrap: wrap;
  gap: 16px;
  color: var(--addp-text-secondary);
}

.lineage-legend {
  gap: 6px;
  color: var(--addp-text-tertiary);
}

.lineage-legend-dot {
  width: 8px;
  height: 8px;
  border: 1px solid var(--addp-border-color);
  border-radius: 50%;
  background: var(--addp-bg-primary);
}

.lineage-legend-dot-current {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary);
}

.lineage-depth { width: 94px; margin-right: 8px; }
.lineage-depth-label { margin-right: 6px; white-space: nowrap; }
.lineage-truncated { padding: 6px 12px; color: var(--el-color-warning); font-size: 12px; }

.lineage-tools {
  flex: 0 0 auto;
  gap: 2px;
}

.lineage-stage {
  position: relative;
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  padding: 12px;
  background: var(--addp-bg-secondary);
}

.lineage-canvas {
  flex: 1;
  width: 100%;
  min-height: 0;
  overflow: hidden;
  background: var(--addp-bg-secondary);
}

.lineage-canvas :deep(canvas) {
  display: block;
}

.lineage-inspector {
  display: grid;
  grid-template-columns: minmax(180px, 0.8fr) minmax(0, 2fr);
  gap: 24px;
  padding: 12px 16px;
  border-top: 1px solid var(--addp-border-color-light);
  background: var(--addp-bg-primary);
  font-size: 12px;
}

.lineage-inspector-heading {
  min-width: 0;
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.lineage-inspector-heading strong {
  flex: 0 1 auto;
  min-width: 0;
  overflow: hidden;
  color: var(--addp-text-primary);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lineage-inspector-kind {
  flex: 0 0 auto;
  color: var(--el-color-primary);
  font-weight: 600;
}

.lineage-inspector-path {
  min-width: 0;
  overflow: hidden;
  color: var(--addp-text-secondary);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lineage-inspector-fields {
  min-width: 0;
  display: grid;
  grid-template-columns: repeat(2, minmax(120px, 1fr));
  gap: 8px 24px;
  margin: 0;
}

.lineage-inspector-fields > div {
  min-width: 0;
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: 8px;
}

.lineage-inspector-field-wide {
  grid-column: 1 / -1;
}

.lineage-inspector-fields dt {
  color: var(--addp-text-tertiary);
}

.lineage-inspector-fields dd {
  overflow-wrap: anywhere;
  min-width: 0;
  margin: 0;
  overflow: hidden;
  color: var(--addp-text-secondary);
  text-overflow: ellipsis;
  white-space: normal;
}

.lineage-mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

@media (max-width: 900px) {
  .lineage-legend {
    display: none;
  }

  .lineage-inspector {
    grid-template-columns: 1fr;
    gap: 10px;
  }
}
</style>
