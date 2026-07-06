<template>
  <div class="graph-browser">
    <!-- 顶部工具栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <span class="graph-name">{{ graphName }}</span>
        <el-tag v-if="stats" size="small" type="info">
          {{ stats.node_count }} {{ t('graph.browser.nodes') }} / {{ stats.relationship_count }} {{ t('graph.browser.relations') }}
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
          style="width: 280px"
          @keyup.enter="handleSearch"
          @clear="clearSearch"
        >
          <template #append>
            <el-button :icon="Search" @click="handleSearch" :loading="searching" />
          </template>
        </el-input>
      </div>
      <div class="toolbar-right">
        <el-select v-model="currentLayout" class="layout-select" @change="onLayoutChange">
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
        <el-button :icon="Refresh" @click="loadOverview" :title="t('graph.browser.resetView')" />
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
            <div v-for="rt in relationshipTypes" :key="rt" class="filter-item">
              <el-checkbox :label="rt" :value="rt">{{ rt }}</el-checkbox>
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
          @node-click="handleNodeClick"
          @node-select="handleNodeSelect"
          @edge-select="handleEdgeSelect"
          @canvas-click="handleCanvasClick"
        />
      </div>

      <!-- 右侧面板 -->
      <div class="detail-panel">
        <!-- Tab 切换：详情 / 分析 -->
        <div v-if="activeRightPanel === 'detail'" style="height: 100%; overflow: hidden;">
          <NodePanel
            :selected="selectedItem"
            @close="selectedItem = null"
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
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Search, Refresh, DataAnalysis } from '@element-plus/icons-vue'
import { browseAPI } from '../api/browse'
import { analysisAPI } from '../api/analysis'
import { knowledgeGraphAPI } from '../api/ontology'
import GraphCanvas from '../components/GraphCanvas.vue'
import NodePanel from '../components/NodePanel.vue'
import AnalysisPanel from '../components/AnalysisPanel.vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const route = useRoute()
const graphId = computed(() => route.params.id)

// 图谱基本信息
const graphName = ref('')
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
  return (schema.value.relationship_shapes || []).map(shape => shape.type).filter(Boolean)
})

const selectedNodeShapeFilters = computed(() => {
  if (visibleNodeShapes.value.length === 0) return []
  const selected = new Set(visibleNodeShapes.value)
  return nodeShapes.value
    .filter(shape => selected.has(shape.name))
    .map(shape => shape.labels?.length ? shape.labels : [shape.name])
})

// 过滤后的节点/边
const filteredNodes = computed(() => {
  if (visibleNodeShapes.value.length === 0) return allNodes.value
  const filters = selectedNodeShapeFilters.value
  if (filters.length === 0) return allNodes.value
  return allNodes.value.filter(n => {
    if (!n.labels || n.labels.length === 0) return true
    return filters.some(labels => labels.every(label => n.labels.includes(label)))
  })
})

const filteredEdges = computed(() => {
  const nodeIds = new Set(filteredNodes.value.map(n => n.id))
  return allEdges.value.filter(e => {
    if (visibleRelTypes.value.length > 0 && !visibleRelTypes.value.includes(e.type)) return false
    return nodeIds.has(e.source) && nodeIds.has(e.target)
  })
})

// Neo4j label → color 映射（仅用于给 node shape 图例取色）
const labelColorMap = computed(() => {
  const map = {}
  allNodes.value.forEach(n => {
    if (n.labels && n.color) {
      n.labels.forEach(l => { if (!map[l]) map[l] = n.color })
    }
  })
  return map
})

function getNodeShapeColor(shape) {
  const labels = shape.labels?.length ? shape.labels : [shape.name]
  return labels.map(label => labelColorMap.value[label]).find(Boolean) || '#5B8FF9'
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

// 初始化加载
async function loadOverview() {
  loading.value = true
  allNodes.value = []
  allEdges.value = []
  nodeMap.value = {}
  edgeMap.value = {}
  try {
    const res = await browseAPI.getOverview(graphId.value)
    mergeSubgraph(res)
    // 初始化过滤器为全选
    visibleNodeShapes.value = nodeShapes.value.map(shape => shape.name)
    visibleRelTypes.value = [...relationshipTypes.value]
  } catch (e) {
    ElMessage.error(t('graph.browser.loadOverviewFailed') + ': ' + e.message)
  } finally {
    loading.value = false
  }
}

async function loadSchema() {
  try {
    const res = await browseAPI.getSchema(graphId.value)
    schema.value = res || { node_shapes: [], relationship_shapes: [] }
    visibleNodeShapes.value = nodeShapes.value.map(shape => shape.name)
    visibleRelTypes.value = [...relationshipTypes.value]
  } catch (e) {
    // schema 加载失败不影响主流程
  }
}

async function loadStats() {
  try {
    const res = await browseAPI.getStats(graphId.value)
    stats.value = res
  } catch (e) {
    // 统计加载失败不影响主流程
  }
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
async function handleSearch() {
  if (!searchQuery.value.trim()) return
  searching.value = true
  try {
    const res = await browseAPI.searchNodes(graphId.value, searchQuery.value.trim())
    const result = res
    mergeSubgraph(result)
    if (!result?.nodes?.length) {
      ElMessage.info(t('graph.browser.noMatchingNodes'))
    } else {
      ElMessage.success(t('graph.browser.foundNodes', { count: result.nodes.length }))
      // 等待 Vue 响应式更新后触发定位（afterlayout 中执行高亮+居中）
      await nextTick()
      const foundIds = result.nodes.map(n => n.id)
      canvasRef.value?.focusNodes(foundIds)
      // 单个结果自动展示详情
      if (result.nodes.length === 1) {
        selectedItem.value = { ...result.nodes[0], type: 'node' }
      }
    }
  } catch (e) {
    ElMessage.error(t('graph.browser.searchFailed') + ': ' + e.message)
  } finally {
    searching.value = false
  }
}

function clearSearch() {
  searchQuery.value = ''
  canvasRef.value?.clearHighlight()
}

// 节点点击
function handleNodeClick(nodeId) {
  selectedNode.value = nodeId
  if (pathMode.value) {
    handleSetPathNode(nodeId)
  }
}

function handleNodeSelect(node) {
  selectedItem.value = { ...node, type: 'node' }
  selectedNode.value = node.id
  // 选中节点后自动切换到详情面板（分析面板打开时不切换）
  if (activeRightPanel.value !== 'analysis') {
    activeRightPanel.value = 'detail'
  }
}

function handleEdgeSelect(edge) {
  selectedItem.value = { ...edge, type: 'edge' }
}

function handleCanvasClick() {
  selectedItem.value = null
}

// 展开节点邻居
async function handleExpand(nodeId) {
  loading.value = true
  try {
    const res = await browseAPI.expandNode(graphId.value, nodeId)
    mergeSubgraph(res)
  } catch (e) {
    ElMessage.error(t('graph.browser.expandFailed') + ': ' + e.message)
  } finally {
    loading.value = false
  }
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

async function findPath() {
  const [src, dst] = pathNodes.value
  loading.value = true
  try {
    const res = await browseAPI.findPath(graphId.value, src, dst)
    mergeSubgraph(res)
    ElMessage.success(t('graph.browser.pathShown'))
  } catch (e) {
    ElMessage.error(t('graph.browser.pathFailed') + ': ' + e.message)
  } finally {
    loading.value = false
    cancelPathMode()
  }
}

function cancelPathMode() {
  pathMode.value = false
  pathNodes.value = []
}

function applyFilter() {
  // filteredNodes/filteredEdges 是 computed，自动更新
}

function onLayoutChange() {
  // currentLayout 变化后 GraphCanvas 内部 watch 会自动更新布局
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
  mergeSubgraph(subgraph)
  ElMessage.success(t('graph.browser.subgraphLoaded', { count: subgraph.nodes?.length || 0 }))
}

onMounted(async () => {
  await Promise.all([loadGraphMeta(), loadSchema(), loadStats()])
  await Promise.all([loadOverview(), loadCapabilities()])
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
  padding: 8px 16px;
  border-bottom: 1px solid var(--addp-border-color);
  background: var(--addp-bg-primary);
  gap: 12px;
  flex-shrink: 0;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.toolbar-center {
  flex: 1;
  display: flex;
  justify-content: center;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.layout-select {
  width: 132px;
}

.graph-name {
  font-weight: 600;
  font-size: 15px;
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
</style>
