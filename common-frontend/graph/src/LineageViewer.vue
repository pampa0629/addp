<template>
  <div ref="fullscreenHost" class="lineage-viewer">
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
          <el-radio-group v-if="supportsFields" :model-value="granularity" :aria-label="t('lineage.granularity')" size="small" @update:model-value="emit('update:granularity', $event)">
            <el-radio-button value="item">{{ t('lineage.granularities.item') }}</el-radio-button>
            <el-radio-button value="field">{{ t('lineage.granularities.field') }}</el-radio-button>
          </el-radio-group>
          <span class="lineage-depth-label">{{ t('lineage.depth') }}</span>
          <el-select :model-value="depth" :aria-label="t('lineage.depth')" class="lineage-depth" size="small" :teleported="false" @update:model-value="emit('update:depth', $event)">
            <el-option v-for="value in [1, 2, 3, 5, 10, 20]" :key="value" :value="value" :label="t('lineage.layers', { count: value })" />
          </el-select>
          <el-tooltip :teleported="false" :content="t('lineage.zoomOut')" placement="bottom">
            <el-button text circle size="small" :aria-label="t('lineage.zoomOut')" @click="zoomBy(0.8)">
              <el-icon><ZoomOut /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip :teleported="false" :content="t('lineage.zoomIn')" placement="bottom">
            <el-button text circle size="small" :aria-label="t('lineage.zoomIn')" @click="zoomBy(1.25)">
              <el-icon><ZoomIn /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip :teleported="false" :content="t('lineage.autoLayout')" placement="bottom">
            <el-button text circle size="small" :aria-label="t('lineage.autoLayout')" :disabled="layoutPending || fieldDisplayPending || !nodes.length" @click="autoLayout">
              <el-icon><Rank /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip :teleported="false" :content="t('lineage.fitView')" placement="bottom">
            <el-button text circle size="small" :aria-label="t('lineage.fitView')" @click="fitView">
              <el-icon><FullScreen /></el-icon>
            </el-button>
          </el-tooltip>
          <el-tooltip :teleported="false" :content="fullscreenLabel" placement="bottom">
            <el-button text circle size="small" :aria-label="fullscreenLabel" :aria-pressed="isFullscreen" :disabled="fullscreenPending" @click="toggleFullscreen">
              <el-icon><Close v-if="isFullscreen" /><Expand v-else /></el-icon>
            </el-button>
          </el-tooltip>
        </div>
      </div>

      <div v-if="isFieldGraph" class="lineage-fields" :aria-label="t('lineage.fields')">
        <el-input v-model="fieldSearch" class="lineage-field-search" size="small" clearable :prefix-icon="Search" :aria-label="t('lineage.searchFields')" :placeholder="t('lineage.searchFields')" />
        <div class="lineage-field-options">
          <el-button size="small" :type="!selectedNode ? 'primary' : ''" @click="clearSelection">{{ t('lineage.showAllFields') }}</el-button>
          <el-button v-for="node in visibleRootFields" :key="nodeId(node)" size="small" :disabled="layoutPending || fieldDisplayPending" :type="nodeId(selectedNode) === nodeId(node) ? 'primary' : ''" :title="node.field_name" @click="selectField(node, true)">{{ node.field_name }}</el-button>
          <span v-if="!visibleRootFields.length" class="lineage-field-empty" role="status">{{ t('lineage.noMatchingFields') }}</span>
        </div>
        <div class="lineage-field-display">
          <el-button size="small" :disabled="layoutPending || fieldDisplayPending || collapsedTables.size === fieldProjection.nodes.length" @click="setAllFieldsCollapsed(true)">{{ t('lineage.collapseFields') }}</el-button>
          <el-button size="small" :disabled="layoutPending || fieldDisplayPending || !collapsedTables.size" @click="setAllFieldsCollapsed(false)">{{ t('lineage.expandFields') }}</el-button>
        </div>
      </div>
      <div v-if="graph.truncated" class="lineage-truncated" role="status">{{ t('lineage.truncated') }}</div>
      <div v-if="!isFieldGraph && graph.field_lineage_status === 'unavailable'" class="lineage-truncated" role="status">{{ t('lineage.fieldUnavailable') }}</div>
      <div v-else-if="!isFieldGraph && graph.field_lineage_status === 'complete' && !edges.length" class="lineage-truncated" role="status">{{ t('lineage.fieldNoDependencies') }}</div>
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
        <div v-if="selectedNode.field_lineage_status === 'unavailable'" class="lineage-field-status" role="status">{{ t('lineage.fieldUnavailable') }}</div>
        <div v-else-if="!graph.truncated && selectedNode.field_lineage_status === 'complete' && !edges.some(edge => nodeId(edge.source) === nodeId(selectedNode) || nodeId(edge.target) === nodeId(selectedNode))" class="lineage-field-status" role="status">{{ t('lineage.fieldNoDependencies') }}</div>
        <dl class="lineage-inspector-fields">
          <div v-if="selectedNode.field_name"><dt>{{ t('lineage.field') }}</dt><dd>{{ selectedNode.field_name }}</dd></div>
          <div v-if="selectedNode.schema_snapshot_hash" class="lineage-inspector-field-wide"><dt>{{ t('lineage.schemaSnapshot') }}</dt><dd class="lineage-mono">{{ selectedNode.schema_snapshot_hash }}</dd></div>
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
          <div v-if="selectedEdge.transformation"><dt>{{ t('lineage.transformation') }}</dt><dd>{{ t(`lineage.transformations.${selectedEdge.transformation}`) }}</dd></div>
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
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Close, Expand, FullScreen, Rank, Search, ZoomIn, ZoomOut } from '@element-plus/icons-vue'
import G6 from '@antv/g6'
import { ElMessage } from 'element-plus'
import { useElementFullscreen } from '../../basic/src/composables/useElementFullscreen.js'
import { useDAGViewport } from '../../dag/src/composables/useDAGViewport.js'
import { createDAGDragNodeBehavior } from '../../dag/src/utils/directEdge.js'
import { focusDAGConnections } from '../../dag/src/utils/connections.js'
import { lineageNodeId as nodeId } from './lineageApi.js'
import { projectLineageFields, lineageFieldConnections, FIELD_HEADER_HEIGHT, FIELD_ROW_HEIGHT, FIELD_FONT_SIZE } from './lineageFields.js'

const LINEAGE_NODE_TYPE = 'addp-lineage-card'
const LINEAGE_EDGE_TYPE = 'addp-lineage-link'
const NODE_WIDTH = 280
const NODE_HEIGHT = 108
const FIT_PADDING = 48

const { t, locale } = useI18n()
const props = defineProps({
  graph: { type: Object, default: () => ({ nodes: [], edges: [] }) },
  depth: { type: Number, default: 2 },
  supportsFields: { type: Boolean, default: false },
  granularity: { type: String, default: 'item' }
})

const emit = defineEmits(['update:depth', 'update:granularity', 'expand', 'view-execution'])
const fullscreenHost = ref(null)
const { isFullscreen, pending: fullscreenPending, toggleFullscreen } = useElementFullscreen(fullscreenHost, {
  onError: () => ElMessage.error(t('lineage.fullscreenFailed'))
})
const fullscreenLabel = computed(() => t(isFullscreen.value ? 'lineage.exitFullscreen' : 'lineage.fullscreen'))
const canvasRef = ref(null)
const selectedNode = ref(null)
const selectedEdge = ref(null)
const graphInstance = shallowRef(null)
const fieldSearch = ref('')
const collapsedTables = ref(new Set())
const fieldDisplayPending = ref(false)
const layoutPending = ref(false)
const { autoLayout: layoutDAG } = useDAGViewport(graphInstance)
let resizeObserver
let themeObserver
let observedCanvas
let expansionAnchor
let renderSequence = 0
let textMeasureContext


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
const isFieldGraph = computed(() => props.graph?.granularity === 'field')
const rootFields = computed(() => nodes.value.filter(node => node.kind === 'field_ref' && node.item_id === props.graph?.subject?.item_id && node.schema_snapshot_hash === props.graph?.subject?.schema_snapshot_hash))
const fieldProjection = computed(() => projectLineageFields(nodes.value, edges.value, collapsedTables.value))

const visibleRootFields = computed(() => {
  const query = fieldSearch.value.trim().toLocaleLowerCase()
  return rootFields.value.filter(node => !query || node.field_name.toLocaleLowerCase().includes(query))
})

function lineageLayout() {
  const tableCount = isFieldGraph.value ? fieldProjection.value.nodes.length : 0
  // Small chains stay readable in a narrow pane; larger graphs need routing space.
  const ranksep = isFieldGraph.value ? (tableCount <= 4 ? 12 : 48) : 170
  return { type: 'dagre', rankdir: 'LR', nodesep: 48, ranksep, controlPoints: true }
}

async function autoLayout() {
  if (!graphInstance.value || layoutPending.value) return
  const instance = graphInstance.value
  layoutPending.value = true
  try {
    await layoutDAG({ layout: lineageLayout(), fit: () => {
      if (graphInstance.value !== instance) return
      fitView()
      restoreFocus()
    } })
  } finally {
    layoutPending.value = false
  }
}

async function applyCollapsedTables(next) {
  if (!graphInstance.value || fieldDisplayPending.value || layoutPending.value) return
  fieldDisplayPending.value = true
  try {
    collapsedTables.value = next
    await renderGraph(true)
  } finally {
    fieldDisplayPending.value = false
  }
}

function setAllFieldsCollapsed(collapsed) {
  return applyCollapsedTables(new Set(collapsed ? fieldProjection.value.nodes.map(table => table.id) : []))
}

function toggleFields(tableId) {
  const next = new Set(collapsedTables.value)
  if (next.has(tableId)) next.delete(tableId)
  else next.add(tableId)
  return applyCollapsedTables(next)
}

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
  return String(node?.field_name || node?.name || node?.full_name || nodeTypeLabel(node))
}

function nodeQualifiedName(node) {
  return node?.kind === 'field_ref' ? `${node.full_name} · ${node.field_name}` : String(node?.full_name || nodeDisplayName(node))
}

function nodePath(node) {
  if (node?.full_name && node.full_name !== node.name) return String(node.full_name)
  return ''
}

function nodeTypeLabel(node) {
  if (node?.kind === 'field_ref') return t('lineage.types.field')
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
      if (cfg._fields) return drawFieldCard(cfg, group)
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

    getAnchorPoints(cfg) {
      return cfg._anchors || [[0, 0.5], [1, 0.5]]
    }
  }, 'single-node')
}

function graphData() {
  const palette = themePalette()
  const projection = isFieldGraph.value ? fieldProjection.value : null
  const data = {
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
  if (projection) {
    const visuals = new Map(data.nodes.map(node => [node.id, node]))
    data.nodes = projection.nodes.map(group => {
      const visual = visuals.get(nodeId(group.node))
      const isSubject = group.node.item_id === props.graph.subject?.item_id && group.node.schema_snapshot_hash === props.graph.subject?.schema_snapshot_hash
      return { ...visual, id: group.id, size: group.size, _fields: group.fields, _anchors: group.anchors,
        _collapsed: group.collapsed, _collapsedLabel: t('lineage.collapsedFieldCount', { count: group.fields.length }),
        _title: truncate(group.node.name || group.node.full_name, isSubject ? 23 : 30), _path: truncate(group.node.full_name, 40), _isSubject: isSubject,
        _visual: { ...visual._visual, fill: isSubject ? palette.primarySoft : palette.background, stroke: isSubject ? palette.primary : palette.border, lineWidth: isSubject ? 2 : 1 } }
    })
    data.edges = projection.edges.map(edge => ({ ...data.edges.find(model => model.id === edge.id), ...edge, label: '' }))
  }
  return data
}

function fieldLabel(value, width, fontSize, fontWeight = 400) {
  textMeasureContext ||= document.createElement('canvas').getContext('2d')
  textMeasureContext.font = `${fontWeight} ${fontSize}px sans-serif`
  const text = String(value || '')
  if (textMeasureContext.measureText(text).width <= width) return text
  const characters = Array.from(text)
  let low = 0, high = characters.length
  while (low < high) {
    const middle = Math.ceil((low + high) / 2)
    if (textMeasureContext.measureText(`${characters.slice(0, middle).join('')}…`).width <= width) low = middle
    else high = middle - 1
  }
  return `${characters.slice(0, low).join('')}…`
}

function drawFieldCard(cfg, group) {
  const visual = cfg._visual
  const [width, height] = cfg.size
  const left = -width / 2
  const top = -height / 2
  const card = group.addShape('rect', { name: 'lineage-card', attrs: { x: left, y: top, width, height, radius: 6, cursor: 'move', fill: visual.fill, stroke: visual.stroke, lineWidth: visual.lineWidth } })
  const text = (name, value, y, fontSize, fill, available = width - 24, fontWeight = 400) => group.addShape('text', { name, capture: false, attrs: { x: left + 12, y: top + y, text: fieldLabel(value, available, fontSize, fontWeight), fill, fontSize, fontWeight, fontFamily: 'sans-serif' } })
  text('lineage-title', cfg._node.name || cfg._node.full_name, 22, 15, visual.textPrimary, width - (cfg._isSubject ? 92 : 54), 600)
  text('lineage-path', cfg._node.full_name, 40, 11, visual.textSecondary)
  text('lineage-engine', cfg._node.engine_name, 56, 11, visual.textTertiary)
  if (cfg._isSubject) group.addShape('text', { name: 'lineage-current', capture: false, attrs: { x: -left - 42, y: top + 22, text: cfg._currentLabel, textAlign: 'right', fill: visual.accent, fontSize: 11 } })
  const toggleName = 'lineage-toggle-fields'
  group.addShape('rect', { name: toggleName, attrs: { x: width / 2 - 34, y: top + 7, width: 28, height: 28, radius: 4, fill: visual.fill, stroke: visual.stroke, cursor: 'pointer' } })
  group.addShape('text', { name: toggleName, attrs: { x: width / 2 - 20, y: top + 21, text: cfg._collapsed ? '+' : '−', textAlign: 'center', textBaseline: 'middle', fill: visual.accent, fontSize: 20, cursor: 'pointer' } })
  if (cfg._collapsed) {
    text('lineage-collapsed-summary', cfg._collapsedLabel, FIELD_HEADER_HEIGHT + FIELD_ROW_HEIGHT / 2, FIELD_FONT_SIZE, visual.textSecondary).attr('textBaseline', 'middle')
    return card
  }
  cfg._fields.forEach((field, index) => {
    const y = top + FIELD_HEADER_HEIGHT + index * FIELD_ROW_HEIGHT
    const name = `lineage-field:${index}`
    group.addShape('rect', { name, attrs: { x: left + 1, y, width: width - 2, height: FIELD_ROW_HEIGHT, fill: visual.fill, cursor: 'pointer' } })
    group.addShape('text', { name, attrs: { x: left + 12, y: y + FIELD_ROW_HEIGHT / 2, text: fieldLabel(field.field_name, width - 24, FIELD_FONT_SIZE), textBaseline: 'middle', fill: field.field_lineage_status === 'unavailable' ? visual.textTertiary : visual.textPrimary, fontSize: FIELD_FONT_SIZE, fontFamily: 'sans-serif', cursor: 'pointer' } })
  })
  return card
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
  if (!graphInstance.value) return
  if (isFieldGraph.value && selectedNode.value?.kind === 'field_ref') { focusField(selectedNode.value); return }
  const focused = focusDAGConnections(graphInstance.value, item)
  for (const edge of graphInstance.value.getEdges()) {
    graphInstance.value.setItemState(edge, 'hover', focused.includes(edge))
  }
  for (const edge of focused) {
    edge.getSource().toFront()
    edge.getTarget().toFront()
  }
}

function focusField(node) {
  if (!graphInstance.value) return
  const focus = node && lineageFieldConnections(edges.value, nodeId(node))
  for (const item of graphInstance.value.getEdges()) {
    const active = !focus || focus.connections.has(item.getID())
    graphInstance.value.setItemState(item, 'hover', !!focus && active)
    item.getContainer().attr('opacity', active ? 1 : 0.15)
  }
  for (const item of graphInstance.value.getNodes()) {
    const model = item.getModel()
    const active = !focus || model._fields.some(field => focus.fields.has(nodeId(field)))
    item.getContainer().attr('opacity', active ? 1 : 0.35)
    model._fields.forEach((field, index) => {
      const selected = nodeId(field) === nodeId(node)
      for (const shape of item.getContainer().get('children').filter(shape => shape.get('name') === `lineage-field:${index}`)) {
        if (shape.get('type') === 'rect') shape.attr({ fill: selected ? themePalette().primarySoft : model._visual.fill, stroke: selected ? model._visual.accent : model._visual.fill })
        shape.attr('opacity', !focus || focus.fields.has(nodeId(field)) ? 1 : 0.35)
      }
    })
  }
}

async function selectField(node, locate = false) {
  if (locate && collapsedTables.value.size) {
    const focus = lineageFieldConnections(edges.value, nodeId(node))
    const next = new Set(collapsedTables.value)
    for (const table of fieldProjection.value.nodes) {
      if (table.fields.some(field => focus.fields.has(nodeId(field)))) next.delete(table.id)
    }
    if (next.size !== collapsedTables.value.size) await applyCollapsedTables(next)
  }
  clearSelection()
  selectedNode.value = node
  focusField(node)
  if (!locate) return
  const instance = graphInstance.value
  await nextTick()
  if (!instance || graphInstance.value !== instance) return
  const table = instance.getNodes().find(item => item.getModel()._fields.some(field => nodeId(field) === nodeId(node)))
  if (!table) return
  const model = table.getModel()
  const index = model._fields.findIndex(field => nodeId(field) === nodeId(node))
  const point = instance.getCanvasByPoint(model.x, model.y - model.size[1] / 2 + FIELD_HEADER_HEIGHT + (index + 0.5) * FIELD_ROW_HEIGHT)
  instance.translate(canvasRef.value.clientWidth / 2 - point.x, canvasRef.value.clientHeight / 2 - point.y)
}

function restoreFocus() {
  const selected = [...(graphInstance.value?.getNodes() || []), ...(graphInstance.value?.getEdges() || [])].find(item => item.hasState('selected'))
  focusItem(selected)
}

function fitView() {
  if (!graphInstance.value) return
  graphInstance.value.fitView(isFieldGraph.value ? 20 : FIT_PADDING)
  if (graphInstance.value.getZoom() > 1) {
    graphInstance.value.zoomTo(1)
    graphInstance.value.fitCenter()
  }
}

function zoomBy(ratio) {
  if (!graphInstance.value) return
  const nextZoom = Math.min(2.5, Math.max(0.1, graphInstance.value.getZoom() * ratio))
  graphInstance.value.zoomTo(nextZoom)
}

function clearSelection() {
  graphInstance.value?.getNodes().forEach(item => graphInstance.value.setItemState(item, 'selected', false))
  graphInstance.value?.getEdges().forEach(item => graphInstance.value.setItemState(item, 'selected', false))
  selectedNode.value = null
  selectedEdge.value = null
  if (isFieldGraph.value) focusField(null)
  else focusItem(null)
}

function selectNode(item) {
  clearSelection()
  graphInstance.value.setItemState(item, 'selected', true)
  selectedNode.value = item.getModel()._node
  focusItem(item)
}

function selectEdge(item) {
  clearSelection()
  graphInstance.value.setItemState(item, 'selected', true)
  selectedEdge.value = item.getModel()._edge
  focusItem(item)
}

function destroyGraph() {
  graphInstance.value?.destroy()
  graphInstance.value = null
}

function observeCanvasSize() {
  if (!resizeObserver || !canvasRef.value || observedCanvas === canvasRef.value) return
  resizeObserver.disconnect()
  observedCanvas = canvasRef.value
  resizeObserver.observe(observedCanvas)
}

function expandNode(node, direction) {
  if (props.graph.truncated || !node.item_id) return
  const item = graphInstance.value?.findById(nodeId(node))
  if (item) {
    const { x, y } = item.getModel()
    expansionAnchor = { id: nodeId(node), subject: subjectId.value, zoom: graphInstance.value.getZoom(), point: graphInstance.value.getCanvasByPoint(x, y) }
  }
  emit('expand', { item_id: node.item_id, direction })
}

async function renderGraph(preserveView = false) {
  const preserved = preserveView && graphInstance.value ? {
    data: graphInstance.value.save(),
    matrix: graphInstance.value.getGroup().getMatrix()?.slice() || null
  } : null
  const sequence = ++renderSequence
  if (!preserved) { fieldSearch.value = ''; collapsedTables.value = new Set() }
  const anchor = expansionAnchor?.subject === subjectId.value ? expansionAnchor : null
  expansionAnchor = null
  destroyGraph()
  if (!anchor && !preserved) { selectedNode.value = null; selectedEdge.value = null }
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
  const dragBehavior = createDAGDragNodeBehavior()
  graphInstance.value = new G6.Graph({
    container: canvasRef.value,
    width,
    height: canvasRef.value.clientHeight,
    minZoom: 0.1,
    maxZoom: 2.5,
    modes: { default: ['drag-canvas', 'zoom-canvas', { ...dragBehavior, shouldBegin: event => event.target?.get('name') !== 'lineage-toggle-fields' && dragBehavior.shouldBegin(event) }] },
    plugins: [new G6.Tooltip({
      className: 'lineage-tooltip',
      itemTypes: ['node'], offsetX: 12, offsetY: 12,
      getContent(event) {
        const content = document.createElement('div')
        const model = event.item.getModel()
        const shape = event.target?.get('name')
        if (shape === 'lineage-toggle-fields') {
          content.textContent = t(model._collapsed ? 'lineage.expandFields' : 'lineage.collapseFields')
          return content
        }
        const index = shape?.startsWith('lineage-field:') ? Number(shape.slice('lineage-field:'.length)) : -1
        content.textContent = index >= 0 ? nodeQualifiedName(model._fields[index]) : model._fields ? model._node.full_name : nodeQualifiedName(model._node)
        return content
      }
    })],
    layout: preserved ? undefined : lineageLayout(),
    defaultNode: { type: LINEAGE_NODE_TYPE, size: [NODE_WIDTH, NODE_HEIGHT] },
    defaultEdge: { type: LINEAGE_EDGE_TYPE },
    edgeStateStyles: {
      selected: { stroke: palette.primary, lineWidth: 2.5 },
      hover: { stroke: palette.primaryHover, lineWidth: 2 }
    }
  })

  graphInstance.value.on('node:dragstart', event => {
    // Dagre points are absolute: discard them before moving the endpoints.
    for (const edge of event.item.getEdges()) graphInstance.value.updateItem(edge, { controlPoints: [] })
  })
  graphInstance.value.on('node:click', event => {
    const shape = event.target?.get('name')
    const direction = ['upstream', 'downstream'].find(value => shape === `lineage-expand-${value}`)
    if (direction) expandNode(event.item.getModel()._node, direction)
    else if (isFieldGraph.value) {
      if (shape === 'lineage-toggle-fields') { toggleFields(event.item.getID()); return }
      const index = shape?.startsWith('lineage-field:') ? Number(shape.slice('lineage-field:'.length)) : -1
      if (index >= 0) selectField(event.item.getModel()._fields[index])
      else clearSelection()
    } else selectNode(event.item)
  })
  graphInstance.value.on('edge:click', event => selectEdge(event.item))
  graphInstance.value.on('canvas:click', clearSelection)
  graphInstance.value.on('node:mouseenter', event => { if (!isFieldGraph.value) focusItem(event.item) })
  graphInstance.value.on('node:mouseleave', restoreFocus)
  graphInstance.value.on('edge:mouseenter', event => focusItem(event.item))
  graphInstance.value.on('edge:mouseleave', restoreFocus)
  const restoreViewport = () => {
    if (!graphInstance.value) return
    if (preserved) {
      if (preserved.matrix) graphInstance.value.getGroup().setMatrix(preserved.matrix)
      else graphInstance.value.getGroup().resetMatrix()
      const selected = selectedEdge.value
        ? graphInstance.value.getEdges().find(item => item.getModel()._edge === selectedEdge.value)
        : graphInstance.value.findById(nodeId(selectedNode.value))
      if (selected) graphInstance.value.setItemState(selected, 'selected', true)
      restoreFocus()
      graphInstance.value.paint()
      return
    }
    const item = anchor && graphInstance.value.findById(anchor.id)
    if (!item) {
      fitView()
      // Keep field text readable on entry; fit-view remains an explicit overview.
      if (isFieldGraph.value && graphInstance.value.getZoom() < 11 / FIELD_FONT_SIZE) {
        graphInstance.value.zoomTo(11 / FIELD_FONT_SIZE)
        const root = graphInstance.value.getNodes().find(node => node.getModel()._isSubject)
        if (root) graphInstance.value.focusItem(root, false)
      }
      return
    }
    graphInstance.value.zoomTo(anchor.zoom)
    const { x, y } = item.getModel()
    const point = graphInstance.value.getCanvasByPoint(x, y)
    graphInstance.value.translate(anchor.point.x - point.x, anchor.point.y - point.y)
  }
  graphInstance.value.once('afterrender', restoreViewport)
  const data = graphData()
  if (preserved) {
    const positions = new Map(preserved.data.nodes.map(node => [node.id, node]))
    const routes = new Map(preserved.data.edges.map(edge => [edge.id, edge.controlPoints]))
    const resized = new Set()
    for (const node of data.nodes) {
      const previous = positions.get(node.id)
      if (previous) {
        const heightChange = node.size[1] - previous.size[1]
        Object.assign(node, { x: previous.x, y: previous.y + heightChange / 2 })
        if (heightChange) resized.add(node.id)
      }
    }
    for (const edge of data.edges) edge.controlPoints = resized.has(edge.source) || resized.has(edge.target) ? [] : routes.get(edge.id)
  }
  graphInstance.value.data(data)
  graphInstance.value.render()

}

onMounted(() => {
  resizeObserver = new ResizeObserver(entries => {
    const width = Math.floor(entries[0]?.contentRect?.width || canvasRef.value?.clientWidth || 0)
    if (width <= 0) return
    if (!graphInstance.value) {
      renderGraph()
      return
    }
    const height = Math.floor(entries[0]?.contentRect?.height || canvasRef.value?.clientHeight || 0)
    if (height <= 0) return
    graphInstance.value.changeSize(width, height)
    // Resizing the inspector/canvas must not reset user zoom or local expansion.
  })

  themeObserver = new MutationObserver(mutations => {
    if (mutations.some(mutation => mutation.attributeName === 'class')) renderGraph(true)
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

watch(() => props.graph, () => renderGraph(), { deep: true })
watch(locale, () => renderGraph(true))
watch(() => props.depth, () => { expansionAnchor = null })
</script>

<style scoped>
.lineage-canvas :deep(.lineage-tooltip) {
  max-width: 480px;
  overflow-wrap: anywhere;
  padding: 8px 12px;
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
}

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

.lineage-viewer:fullscreen {
  width: 100vw;
  height: 100vh;
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

.lineage-fields { display: flex; align-items: flex-start; flex-wrap: wrap; gap: 6px; padding: 6px 12px; flex-shrink: 0; }
.lineage-field-display { display: flex; gap: 6px; flex-shrink: 0; }
.lineage-field-search { width: 180px; }
.lineage-field-options { display: flex; flex: 1; min-width: min(240px, 100%); flex-wrap: wrap; gap: 6px; max-height: 90px; overflow-y: auto; }
.lineage-field-empty { color: var(--addp-text-secondary); font-size: 12px; padding: 4px; }
.lineage-fields .el-button { margin-left: 0; max-width: min(240px, 100%); }
.lineage-fields .el-button :deep(span) { min-width: 0; display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.lineage-field-status { color: var(--addp-text-secondary); font-size: 12px; margin-top: 8px; }
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
  grid-column: 1;
  grid-row: 1;
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
  grid-column: 2;
  grid-row: 1 / span 3;
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

.lineage-expand-actions,
.lineage-field-status {
  grid-column: 1;
}

@media (max-width: 900px) {
  .lineage-legend {
    display: none;
  }

  .lineage-inspector {
    grid-template-columns: 1fr;
    gap: 10px;
  }

  .lineage-inspector-heading,
  .lineage-inspector-fields {
    grid-column: 1;
    grid-row: auto;
  }
}
</style>
