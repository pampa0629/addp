<template>
  <div class="graph-browser">
    <!-- 顶部工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <span class="graph-name">{{ graphName }}</span>
        <el-tag v-if="stats" size="small" type="info">
          {{ t('graph.browser.totalCount') }}:
          {{ stats.node_count }} {{ t('graph.browser.nodes') }} / {{ stats.relationship_count }} {{ t('graph.browser.relations') }}
        </el-tag>
        <el-tag size="small" type="success">
          {{ canvasSummary }}
        </el-tag>
        <!-- 着色状态标签 -->
        <el-tag v-if="analysisActive" size="small" type="warning" closable @close="handleClearScores">
          {{ t('graph.browser.colored') }}：{{ analysisAlgoName }}
        </el-tag>
      </div>
      <div class="toolbar-center">
        <el-input
          v-model="searchQuery"
          :placeholder="t('graph.browser.searchPlaceholder')"
          clearable
          class="search-input"
          @keyup.enter="handleSearch"
          @clear="clearSearch"
        >
          <template #append>
            <el-button :icon="Search" @click="handleSearch" :loading="searching" />
          </template>
        </el-input>
      </div>
      <div class="toolbar-right">
        <el-tooltip :content="t('graph.browser.expandDepth')">
          <el-segmented
            v-model="expandDepth"
            size="small"
            :options="expandDepthOptions"
            :disabled="!canExpandSelectedNode"
            :aria-label="t('graph.browser.expandDepth')"
            @change="handleExpandDepthChange"
          />
        </el-tooltip>
        <el-select v-model="currentLayout" class="layout-select">
          <el-option :label="t('graph.browser.layoutForce')" value="force" />
          <el-option :label="t('graph.browser.layoutDagre')" value="dagre" />
          <el-option :label="t('graph.browser.layoutCircular')" value="circular" />
          <el-option :label="t('graph.browser.layoutRadial')" value="radial" />
        </el-select>
        <el-tooltip :content="showEdgeLabels ? t('graph.browser.hideEdgeLabels') : t('graph.browser.showEdgeLabels')">
          <el-button
            :type="showEdgeLabels ? 'primary' : ''"
            size="small"
            @click="showEdgeLabels = !showEdgeLabels"
          >
            {{ t('graph.browser.edgeLabels') }}
          </el-button>
        </el-tooltip>
        <el-button :icon="Refresh" @click="loadBrowseSnapshot" :title="t('graph.browser.resetView')" />
        <!-- 图分析切换按钮 -->
        <el-button
          :type="activeRightPanel === 'analysis' ? 'primary' : ''"
          :icon="DataAnalysis"
          :title="t('graph.browser.graphAnalysis')"
          @click="toggleAnalysisPanel"
        />
        <el-button
          v-if="pathMode"
          type="warning"
          size="small"
          @click="cancelPathMode"
        >
          {{ t('graph.browser.cancelPathMode', { count: pathNodes.length }) }}
        </el-button>
      </div>
    </div>

    <!-- 主体区域 -->
    <div class="main-area">
      <!-- 左侧过滤面板 -->
      <div class="filter-panel">
        <div class="filter-section">
          <div class="filter-title">{{ t('graph.browser.nodeShapes') }}</div>
          <div v-if="nodeShapes.length === 0" class="filter-empty">—</div>
          <el-checkbox-group v-else v-model="visibleNodeShapes" @change="applyFilter">
            <div v-for="shape in nodeShapes" :key="shape.name" class="filter-item">
              <el-checkbox :label="shape.name" :value="shape.name">
                <span class="label-dot" :style="{ background: getNodeShapeColor(shape) }"></span>
                {{ shape.name }}
              </el-checkbox>
            </div>
          </el-checkbox-group>
        </div>
        <el-divider />
        <div class="filter-section">
          <div class="filter-title">{{ t('graph.browser.relationTypes') }}</div>
          <div v-if="relationshipTypes.length === 0" class="filter-empty">—</div>
          <el-checkbox-group v-else v-model="visibleRelTypes" @change="applyFilter">
            <div v-for="shape in relationshipShapes" :key="shape.type" class="filter-item">
              <el-checkbox :label="shape.type" :value="shape.type">
                <span
                  class="edge-swatch"
                  :class="[`dash-${getRelationshipVisual(shape).dashIndex}`, { 'is-directed': getRelationshipVisual(shape).directed }]"
                  :style="{ color: getRelationshipVisual(shape).color }"
                ></span>
                {{ shape.type }}
              </el-checkbox>
            </div>
          </el-checkbox-group>
        </div>
      </div>

      <!-- 画布区域 -->
      <div class="canvas-area">
        <GraphCanvas
          ref="canvasRef"
          :nodes="filteredNodes"
          :edges="filteredEdges"
          :layout="currentLayout"
          :show-edge-labels="showEdgeLabels"
          :loading="loading"
          :theme="graphTheme"
          :selected-node-id="selectedNodeId"
          :selected-edge-id="selectedEdgeId"
          :search-match-node-ids="searchMatchNodeIds"
          :path-node-ids="pathNodeIds"
          :path-edge-ids="pathEdgeIds"
          :expansion-anchor-id="expansionAnchorId"
          @node-click="handleNodeClick"
          @node-select="handleNodeSelect"
          @edge-select="handleEdgeSelect"
          @canvas-click="handleCanvasClick"
        />
      </div>

      <!-- 右侧面板 -->
      <div class="detail-panel" :class="{ 'is-open': selectedItem || activeRightPanel === 'analysis' }">
        <!-- Tab 切换：详情 / 分析 -->
        <div v-if="activeRightPanel === 'detail'" style="height: 100%; overflow: hidden;">
          <NodePanel
            :selected="selectedItem"
            @close="clearSelection"
            @expand="handleExpand"
            @set-path-node="handleSetPathNode"
          />
        </div>
        <div v-else style="height: 100%; overflow: hidden;">
          <AnalysisPanel
            :graph-id="Number(graphId)"
            :selected-node-id="selectedNode"
            :node-shapes="nodeShapes"
            :schema-rel-types="relationshipTypes"
            :capabilities="capabilities"
            @apply-scores="handleApplyScores"
            @clear-scores="handleClearScores"
            @focus-node="handleFocusNode"
            @load-subgraph="handleLoadSubgraph"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Search, Refresh, DataAnalysis } from '@element-plus/icons-vue'
import { browseAPI } from '../api/browse'
import { analysisAPI } from '../api/analysis'
import { knowledgeGraphAPI } from '../api/ontology'
import GraphCanvas from '../components/GraphCanvas.vue'
import NodePanel from '../components/NodePanel.vue'
import AnalysisPanel from '../components/AnalysisPanel.vue'
import {
  createGraphVisualEncoding,
  getContrastingTextColor,
  graphNodeTypeKey,
  readGraphTheme
} from '../utils/graphVisualEncoding'
import { createLatestOperationController } from '../utils/graphOperationController'
import { useConsolePageDescriptor } from '@common-ui'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const graphId = computed(() => route.params.id)

// 图谱基本信息
const graphName = ref('')
useConsolePageDescriptor(router, 'graph', {
  title: computed(() => t('graph.browser.recentVisitTitle')),
  subject: graphName,
  ready: computed(() => Boolean(graphName.value))
})
const stats = ref(null)
const schema = ref({ node_shapes: [], relationship_shapes: [] })

// 图数据
const allNodes = ref([])
const allEdges = ref([])
const nodeMap = ref({})  // id → node，用于快速查找和去重
const edgeMap = ref({})  // id → edge

// 过滤状态
const visibleNodeShapes = ref([])
const visibleRelTypes = ref([])

// UI 状态
const loading = ref(false)
const searching = ref(false)
const searchQuery = ref('')
const currentLayout = ref('force')
const showEdgeLabels = ref(false)
const selectedItem = ref(null)
const canvasRef = ref(null)
const selectedNodeId = ref('')
const selectedEdgeId = ref('')
const searchMatchNodeIds = ref([])
const pathNodeIds = ref([])
const pathEdgeIds = ref([])
const graphTheme = ref({ categoryColors: [] })
const viewMode = ref('overview')
const expandDepth = ref(1)
const expansionAnchorId = ref('')
const browseOperationController = createLatestOperationController()

// 右侧面板状态
const activeRightPanel = ref('detail')  // 'detail' | 'analysis'
const selectedNode = ref('')           // 当前画布选中节点ID，传给 AnalysisPanel

// 图分析状态
const analysisActive = ref(false)
const analysisAlgoName = ref('')
const capabilities = ref({ gds_available: false, cypher_algos: [], gds_algos: [] })

// 路径查找模式
const pathMode = ref(false)
const pathNodes = ref([])

const nodeShapes = computed(() => {
  return schema.value.node_shapes || []
})

const relationshipTypes = computed(() => {
  return relationshipShapes.value.map(shape => shape.type).filter(Boolean)
})

const relationshipShapes = computed(() => schema.value.relationship_shapes || [])

const expandDepthOptions = computed(() => [1, 2, 3].map(value => ({
  value,
  label: t('graph.browser.hopCount', { count: value })
})))

const canExpandSelectedNode = computed(() => {
  const node = nodeMap.value[selectedNodeId.value]
  return Boolean(node && node.kind !== 'aggregate')
})

const canvasSummary = computed(() => {
  const counts = {
    nodes: filteredNodes.value.length,
    relations: filteredEdges.value.length
  }
  if (viewMode.value === 'overview') {
    return t('graph.browser.overviewCount', counts)
  }
  return `${t('graph.browser.canvasCount')}: ${counts.nodes} ${t('graph.browser.nodes')} / ${counts.relations} ${t('graph.browser.relations')}`
})

const visualEncoding = computed(() => createGraphVisualEncoding({
  nodeShapes: nodeShapes.value,
  relationshipShapes: relationshipShapes.value,
  nodes: allNodes.value,
  edges: allEdges.value,
  palette: graphTheme.value.categoryColors
}))

const visuallyEncodedNodes = computed(() => allNodes.value.map(node => {
  const encoding = visualEncoding.value.nodeTypes.get(graphNodeTypeKey(node))
  const visualColor = encoding?.color || node.color || graphTheme.value.categoryColors[0]
  return {
    ...node,
    visual_color: visualColor,
    visual_label_color: getContrastingTextColor(
      visualColor,
      graphTheme.value.labelLight,
      graphTheme.value.labelDark
    )
  }
}))

const visuallyEncodedEdges = computed(() => allEdges.value.map(edge => {
  const relationType = edge.relation_type || edge.type
  const encoding = visualEncoding.value.relationshipTypes.get(relationType)
  return {
    ...edge,
    visual_color: encoding?.color || edge.color || graphTheme.value.edgeDefault,
    visual_line_dash: encoding?.lineDash || [],
    visual_dash_index: encoding?.dashIndex || 0,
    directed: typeof edge.directed === 'boolean' ? edge.directed : encoding?.directed !== false
  }
}))

const selectedNodeShapeFilters = computed(() => {
  if (visibleNodeShapes.value.length === 0) return []
  const selected = new Set(visibleNodeShapes.value)
  return nodeShapes.value
    .filter(shape => selected.has(shape.name))
    .map(shape => shape.labels?.length ? shape.labels : [shape.name])
})

// 过滤后的节点/边
const filteredNodes = computed(() => {
  if (visibleNodeShapes.value.length === 0) return visuallyEncodedNodes.value
  const filters = selectedNodeShapeFilters.value
  if (filters.length === 0) return visuallyEncodedNodes.value
  return visuallyEncodedNodes.value.filter(n => {
    if (!n.labels || n.labels.length === 0) return true
    return filters.some(labels => labels.every(label => n.labels.includes(label)))
  })
})

const filteredEdges = computed(() => {
  const nodeIds = new Set(filteredNodes.value.map(n => n.id))
  return visuallyEncodedEdges.value.filter(e => {
    if (visibleRelTypes.value.length > 0 && !visibleRelTypes.value.includes(e.type)) return false
    return nodeIds.has(e.source) && nodeIds.has(e.target)
  })
})

function getNodeShapeColor(shape) {
  return visualEncoding.value.nodeTypes.get(shape.name)?.color || graphTheme.value.categoryColors[0]
}

function getRelationshipVisual(shape) {
  return visualEncoding.value.relationshipTypes.get(shape.type) || {
    color: graphTheme.value.edgeDefault,
    lineDash: [],
    dashIndex: 0,
    directed: shape.directed !== false
  }
}

// 合并新节点/边到画布（去重）
function mergeSubgraph(result) {
  if (!result) return
  result.nodes?.forEach(n => {
    if (!nodeMap.value[n.id]) {
      nodeMap.value[n.id] = n
      allNodes.value.push(n)
    }
  })
  result.edges?.forEach(e => {
    if (!edgeMap.value[e.id]) {
      edgeMap.value[e.id] = e
      allEdges.value.push(e)
    }
  })
}

function replaceSubgraph(result) {
  allNodes.value = []
  allEdges.value = []
  nodeMap.value = {}
  edgeMap.value = {}
  mergeSubgraph(result)
}

function runBrowseOperation(kind, request, onSuccess, errorMessageKey) {
  return browseOperationController.execute(kind, request, {
    onStart: () => {
      loading.value = true
      searching.value = kind === 'search'
    },
    onSuccess,
    onError: error => {
      ElMessage.error(t(errorMessageKey) + ': ' + error.message)
    },
    onFinish: () => {
      loading.value = false
      searching.value = false
    },
  })
}

// 初始化加载
function loadBrowseSnapshot() {
  return runBrowseOperation(
    'snapshot',
    signal => browseAPI.getBrowseSnapshot(graphId.value, signal),
    snapshot => {
      clearTransientState()
      viewMode.value = 'overview'
      expansionAnchorId.value = ''
      schema.value = snapshot?.schema || { node_shapes: [], relationship_shapes: [] }
      stats.value = snapshot?.stats || null
      replaceSubgraph(snapshot?.overview)
      visibleNodeShapes.value = nodeShapes.value.map(shape => shape.name)
      visibleRelTypes.value = [...relationshipTypes.value]
    },
    'graph.browser.loadOverviewFailed',
  )
}

async function loadGraphMeta() {
  try {
    const res = await knowledgeGraphAPI.get(graphId.value)
    graphName.value = res?.name || `${t('graph.browser.graphPrefix')} #${graphId.value}`
  } catch (e) {
    graphName.value = `${t('graph.browser.graphPrefix')} #${graphId.value}`
  }
}

async function loadCapabilities() {
  try {
    capabilities.value = await analysisAPI.getCapabilities(graphId.value)
  } catch (e) {
    // 能力探测失败不影响主流程
  }
}

// 搜索
function handleSearch() {
  if (!searchQuery.value.trim()) return
  const query = searchQuery.value.trim()
  searchMatchNodeIds.value = []
  return runBrowseOperation(
    'search',
    signal => browseAPI.searchNodes(graphId.value, query, 30, signal),
    async result => {
      if (!result?.nodes?.length) {
        ElMessage.info(t('graph.browser.noMatchingNodes'))
      } else {
        viewMode.value = 'entity'
        expansionAnchorId.value = ''
        replaceSubgraph(result)
        ElMessage.success(t('graph.browser.foundNodes', { count: result.nodes.length }))
        await nextTick()
        const foundIds = result.nodes.map(n => n.id)
        searchMatchNodeIds.value = foundIds
        canvasRef.value?.focusNodes(foundIds)
        if (result.nodes.length === 1) {
          const encodedNode = visuallyEncodedNodes.value.find(node => node.id === result.nodes[0].id)
          selectedItem.value = { ...(encodedNode || result.nodes[0]), type: 'node' }
        }
      }
    },
    'graph.browser.searchFailed',
  )
}

function clearSearch() {
  searchQuery.value = ''
  searchMatchNodeIds.value = []
}

// 节点点击
function handleNodeClick(nodeId) {
  if (nodeMap.value[nodeId]?.kind === 'aggregate') return
  selectedNode.value = nodeId
  if (pathMode.value) {
    handleSetPathNode(nodeId)
  }
}

function handleNodeSelect(node) {
  if (node.kind === 'aggregate') {
    handleExpand(node)
    return
  }
  selectedItem.value = { ...node, type: 'node' }
  selectedNode.value = node.id
  selectedNodeId.value = node.id
  selectedEdgeId.value = ''
  // 选中节点后自动切换到详情面板（分析面板打开时不切换）
  if (activeRightPanel.value !== 'analysis') {
    activeRightPanel.value = 'detail'
  }
}

function handleEdgeSelect(edge) {
  selectedItem.value = { ...edge, type: 'edge' }
  selectedNode.value = ''
  selectedNodeId.value = ''
  selectedEdgeId.value = edge.id
}

function handleCanvasClick() {
  clearSelection()
}

function clearSelection() {
  selectedItem.value = null
  selectedNode.value = ''
  selectedNodeId.value = ''
  selectedEdgeId.value = ''
}

// 展开节点邻居
function handleExpand(targetValue) {
  const node = typeof targetValue === 'string' ? nodeMap.value[targetValue] : targetValue
  if (!node) return
  const target = node.kind === 'aggregate'
    ? { kind: 'aggregate', labels: node.labels || [] }
    : { kind: 'entity', id: node.id }
  return runBrowseOperation(
    'expand',
    signal => browseAPI.expandTarget(graphId.value, target, expandDepth.value, 200, 400, signal),
    async result => {
      viewMode.value = 'entity'
      expansionAnchorId.value = ''
      replaceSubgraph(result)
      if (node.kind === 'aggregate') {
        ElMessage.success(t('graph.browser.aggregateExpanded', { count: result?.nodes?.length || 0 }))
      }
      await nextTick()
      canvasRef.value?.fitView()
    },
    'graph.browser.expandFailed',
  )
}

function handleExpandDepthChange() {
  if (!canExpandSelectedNode.value) return
  handleExpand(selectedNodeId.value)
}

// 路径查找
function handleSetPathNode(nodeId) {
  pathMode.value = true
  if (!pathNodes.value.includes(nodeId)) {
    pathNodes.value.push(nodeId)
  }
  if (pathNodes.value.length >= 2) {
    findPath()
  }
}

function findPath() {
  const [src, dst] = pathNodes.value
  pathNodeIds.value = []
  pathEdgeIds.value = []
  expansionAnchorId.value = ''
  cancelPathMode()
  return runBrowseOperation(
    'path',
    signal => browseAPI.findPath(graphId.value, src, dst, signal),
    result => {
      mergeSubgraph(result)
      pathNodeIds.value = result?.nodes?.map(node => node.id) || []
      pathEdgeIds.value = result?.edges?.map(edge => edge.id).filter(Boolean) || []
      ElMessage.success(t('graph.browser.pathShown'))
    },
    'graph.browser.pathFailed',
  )
}

function cancelPathMode() {
  pathMode.value = false
  pathNodes.value = []
}

function applyFilter() {
  // filteredNodes/filteredEdges 是 computed，自动更新
}

// 分析面板相关
function toggleAnalysisPanel() {
  activeRightPanel.value = activeRightPanel.value === 'analysis' ? 'detail' : 'analysis'
}

function handleApplyScores(nodeScores, mode, algoName) {
  if (!nodeScores?.length) return
  canvasRef.value?.applyScoreColors(nodeScores, mode)
  analysisActive.value = true
  analysisAlgoName.value = algoName || t('graph.browser.algoAnalysis')
}

function handleClearScores() {
  canvasRef.value?.resetNodeColors()
  analysisActive.value = false
  analysisAlgoName.value = ''
}

function handleFocusNode(nodeId) {
  canvasRef.value?.focusNodes([nodeId])
}

function handleLoadSubgraph(subgraph) {
  viewMode.value = 'entity'
  expansionAnchorId.value = selectedNodeId.value
  mergeSubgraph(subgraph)
  ElMessage.success(t('graph.browser.subgraphLoaded', { count: subgraph.nodes?.length || 0 }))
}

function clearTransientState() {
  canvasRef.value?.resetNodeColors()
  clearSelection()
  searchQuery.value = ''
  searchMatchNodeIds.value = []
  pathNodeIds.value = []
  pathEdgeIds.value = []
  cancelPathMode()
  analysisActive.value = false
  analysisAlgoName.value = ''
}

let themeObserver = null

function syncGraphTheme() {
  graphTheme.value = readGraphTheme()
}

onMounted(async () => {
  syncGraphTheme()
  themeObserver = new MutationObserver(syncGraphTheme)
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  await Promise.all([loadGraphMeta(), loadBrowseSnapshot(), loadCapabilities()])
})

onBeforeUnmount(() => {
  browseOperationController.cancel()
  themeObserver?.disconnect()
})
</script>

<style scoped>
.graph-browser {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  background: var(--addp-bg-primary);
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  padding: 8px 16px;
  border-bottom: 1px solid var(--addp-border-color);
  background: var(--addp-bg-primary);
  gap: 12px;
  flex-shrink: 0;
}

.toolbar-left {
  display: flex;
  align-items: center;
  flex: 1 1 320px;
  flex-wrap: wrap;
  min-width: 0;
  gap: 8px;
}

.toolbar-center {
  flex: 1 1 240px;
  display: flex;
  justify-content: center;
}

.search-input {
  width: 280px;
}

.toolbar-right {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  justify-content: flex-end;
  margin-left: auto;
  gap: 8px;
}

.layout-select {
  width: 132px;
}

.graph-name {
  font-weight: 600;
  font-size: 15px;
  white-space: nowrap;
}

.toolbar :deep(.el-tag) {
  white-space: nowrap;
}

.main-area {
  display: flex;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.filter-panel {
  width: 180px;
  flex-shrink: 0;
  min-height: 0;
  border-right: 1px solid var(--addp-border-color);
  padding: 12px;
  overflow-y: auto;
  background: var(--addp-bg-secondary);
}

.filter-section {
  margin-bottom: 8px;
}

.filter-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--addp-text-secondary);
  margin-bottom: 8px;
}

.filter-empty {
  font-size: 12px;
  color: var(--addp-text-tertiary);
}

.filter-item {
  margin-bottom: 4px;
}

.filter-item .el-checkbox {
  display: flex;
  align-items: center;
}

.label-dot {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  margin-right: 4px;
  flex-shrink: 0;
}

.edge-swatch {
  position: relative;
  display: inline-block;
  width: 24px;
  height: 8px;
  margin-right: 5px;
  flex-shrink: 0;
}

.edge-swatch::before {
  content: '';
  position: absolute;
  inset: 3px 0 auto;
  height: 2px;
  background: currentColor;
}

.edge-swatch.dash-1::before {
  background: repeating-linear-gradient(to right, currentColor 0 8px, transparent 8px 12px);
}

.edge-swatch.dash-2::before {
  background: repeating-linear-gradient(to right, currentColor 0 2px, transparent 2px 6px);
}

.edge-swatch.dash-3::before {
  background: linear-gradient(to right, currentColor 0 10px, transparent 10px 13px, currentColor 13px 15px, transparent 15px 18px);
  background-size: 18px 2px;
}

.edge-swatch.is-directed::after {
  content: '>';
  position: absolute;
  right: -2px;
  top: -9px;
  color: currentColor;
  font-size: 12px;
  font-weight: 700;
}

.canvas-area {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  position: relative;
}

.detail-panel {
  width: 240px;
  flex-shrink: 0;
  min-height: 0;
  border-left: 1px solid var(--addp-border-color);
  overflow: hidden;
  background: var(--addp-bg-primary);
}

@media (max-width: 720px) {
  .toolbar {
    flex-wrap: wrap;
    padding: 8px;
  }

  .toolbar-left {
    min-width: 0;
  }

  .toolbar-center {
    order: 3;
    flex-basis: 100%;
  }

  .toolbar-right {
    width: 100%;
  }

  .search-input {
    width: 100%;
  }

  .main-area {
    position: relative;
  }

  .filter-panel {
    width: 124px;
    padding: 8px;
  }

  .filter-item :deep(.el-checkbox__label) {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .detail-panel {
    position: absolute;
    z-index: 2;
    top: 0;
    right: 0;
    bottom: 0;
    width: 0;
    border-left: 0;
    box-shadow: none;
    transition: width 0.2s ease;
  }

  .detail-panel.is-open {
    width: min(240px, 72vw);
    border-left: 1px solid var(--addp-border-color);
    box-shadow: var(--addp-shadow-card);
  }
}
</style>
