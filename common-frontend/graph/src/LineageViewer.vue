<template>
  <div class="lineage-viewer">
    <el-empty v-if="!nodes.length" :description="t('lineage.noData')" :image-size="56" />
    <template v-else>
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

      <div class="lineage-stage">
        <div
          ref="canvasRef"
          class="lineage-canvas"
          :style="{ height: `${height}px` }"
          role="img"
          :aria-label="t('lineage.graphLabel')"
        />
      </div>

      <div v-if="selectedNode" class="lineage-inspector" aria-live="polite">
        <div class="lineage-inspector-heading">
          <span class="lineage-inspector-kind">{{ nodeTypeLabel(selectedNode) }}</span>
          <strong>{{ nodeDisplayName(selectedNode) }}</strong>
          <span v-if="selectedNode.full_name" class="lineage-inspector-path">{{ selectedNode.full_name }}</span>
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
            <dd class="lineage-mono">{{ selectedEdge.evidence.execution_id }}</dd>
          </div>
        </dl>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { FullScreen, ZoomIn, ZoomOut } from '@element-plus/icons-vue'
import G6 from '@antv/g6'

const LINEAGE_NODE_TYPE = 'addp-lineage-card'
const NODE_WIDTH = 280
const NODE_HEIGHT = 108
const FIT_PADDING = 48

const { t, locale } = useI18n()
const props = defineProps({
  graph: { type: Object, default: () => ({ nodes: [], edges: [] }) },
  height: { type: Number, default: 420 }
})

const canvasRef = ref(null)
const selectedNode = ref(null)
const selectedEdge = ref(null)
let graphInstance
let resizeObserver
let themeObserver
let observedCanvas

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
  return String(node?.name || node?.full_name || node?.published_revision || node?.kind || '')
}

function nodeQualifiedName(node) {
  return String(node?.full_name || nodeDisplayName(node))
}

function nodePath(node) {
  if (node?.full_name && node.full_name !== node.name) return String(node.full_name)
  if (node?.kind === 'published_service' && node.service_id) return `${t('lineage.serviceId')} ${node.service_id}`
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
      target: nodeId(edge.target),
      label: relationLabel(edge.relation_kind),
      _edge: edge,
      style: {
        stroke: palette.textTertiary,
        lineWidth: 1.5,
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
  const nextZoom = Math.min(2.5, Math.max(0.35, graphInstance.getZoom() * ratio))
  graphInstance.zoomTo(nextZoom)
}

function clearSelection() {
  graphInstance?.getNodes().forEach(item => graphInstance.setItemState(item, 'selected', false))
  graphInstance?.getEdges().forEach(item => graphInstance.setItemState(item, 'selected', false))
  selectedNode.value = null
  selectedEdge.value = null
}

function selectNode(item) {
  clearSelection()
  graphInstance.setItemState(item, 'selected', true)
  selectedNode.value = item.getModel()._node
}

function selectEdge(item) {
  clearSelection()
  graphInstance.setItemState(item, 'selected', true)
  selectedEdge.value = item.getModel()._edge
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

async function renderGraph() {
  destroyGraph()
  selectedNode.value = null
  selectedEdge.value = null
  await nextTick()
  if (!canvasRef.value || !nodes.value.length) return
  observeCanvasSize()

  const width = canvasRef.value.clientWidth
  if (width <= 0) return

  registerLineageNode()
  const palette = themePalette()
  graphInstance = new G6.Graph({
    container: canvasRef.value,
    width,
    height: props.height,
    minZoom: 0.35,
    maxZoom: 2.5,
    modes: { default: ['drag-canvas', 'zoom-canvas'] },
    layout: { type: 'dagre', rankdir: 'LR', nodesep: 56, ranksep: 112 },
    defaultNode: { type: LINEAGE_NODE_TYPE, size: [NODE_WIDTH, NODE_HEIGHT] },
    defaultEdge: { type: 'polyline' },
    edgeStateStyles: {
      selected: { stroke: palette.primary, lineWidth: 2.5 },
      hover: { stroke: palette.primaryHover, lineWidth: 2 }
    }
  })

  graphInstance.on('node:click', event => selectNode(event.item))
  graphInstance.on('edge:click', event => selectEdge(event.item))
  graphInstance.on('canvas:click', clearSelection)
  graphInstance.on('node:mouseenter', event => graphInstance.setItemState(event.item, 'hover', true))
  graphInstance.on('node:mouseleave', event => graphInstance.setItemState(event.item, 'hover', false))
  graphInstance.on('edge:mouseenter', event => graphInstance.setItemState(event.item, 'hover', true))
  graphInstance.on('edge:mouseleave', event => graphInstance.setItemState(event.item, 'hover', false))
  graphInstance.on('afterlayout', fitView)
  graphInstance.data(graphData())
  graphInstance.render()

  if (typeof requestAnimationFrame === 'function') requestAnimationFrame(fitView)
  else fitView()
}

onMounted(() => {
  resizeObserver = new ResizeObserver(entries => {
    const width = Math.floor(entries[0]?.contentRect?.width || canvasRef.value?.clientWidth || 0)
    if (width <= 0) return
    if (!graphInstance) {
      renderGraph()
      return
    }
    graphInstance.changeSize(width, props.height)
    if (typeof requestAnimationFrame === 'function') requestAnimationFrame(fitView)
    else fitView()
  })

  themeObserver = new MutationObserver(mutations => {
    if (mutations.some(mutation => mutation.attributeName === 'class')) renderGraph()
  })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  renderGraph()
})

onUnmounted(() => {
  resizeObserver?.disconnect()
  observedCanvas = undefined
  themeObserver?.disconnect()
  destroyGraph()
})

watch(() => props.graph, renderGraph, { deep: true })
watch(locale, renderGraph)
</script>

<style scoped>
.lineage-viewer {
  position: relative;
  width: 100%;
  min-height: 300px;
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
  gap: 16px;
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

.lineage-tools {
  flex: 0 0 auto;
  gap: 2px;
}

.lineage-stage {
  flex: 1;
  min-height: 260px;
  padding: 12px;
  background: var(--addp-bg-secondary);
}

.lineage-canvas {
  width: 100%;
  min-height: 260px;
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
  min-width: 0;
  margin: 0;
  overflow: hidden;
  color: var(--addp-text-secondary);
  text-overflow: ellipsis;
  white-space: nowrap;
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
