<template>
  <div class="analysis-page">
    <!-- 顶部：图谱选择 -->
    <div class="page-header">
      <h2>{{ t('graph.analysis.title') }}</h2>
      <el-select
        v-model="selectedGraphId"
        :placeholder="t('graph.analysis.selectGraph')"
        style="width: 280px"
        @change="onGraphChange"
      >
        <el-option
          v-for="g in graphs"
          :key="g.id"
          :label="g.name"
          :value="g.id"
        />
      </el-select>
      <el-tag v-if="capabilities" :type="capabilities.gds_available ? 'success' : 'info'" size="small">
        {{ capabilities.gds_available ? `GDS ${capabilities.gds_version}` : t('graph.analysis.gdsUnavailable') }}
      </el-tag>
      <el-tag v-if="capabilities" :type="capabilities.spatial_available ? 'success' : 'info'" size="small">
        {{ capabilities.spatial_available ? t('graph.analysis.spatialAvailable') : t('graph.analysis.spatialUnavailable') }}
      </el-tag>
      <el-button
        v-if="capabilities?.spatial_available && capabilities?.pending_layers?.length > 0"
        size="small"
        type="warning"
        :loading="syncing"
        @click="syncSpatialLayers"
      >
        {{ t('graph.analysis.syncSpatialLayers') }} ({{ capabilities.pending_layers.length }})
      </el-button>
    </div>

    <div v-if="!selectedGraphId" class="empty-hint">
      <el-empty :description="t('graph.analysis.selectGraphFirst')" />
    </div>

    <div v-else class="main-area">
      <!-- 左侧：算法配置 -->
      <div class="config-panel">
        <div class="section-title">{{ t('graph.analysis.selectAlgo') }}</div>

        <el-collapse v-model="openGroups" class="algo-collapse">
          <!-- 基础算法 -->
          <el-collapse-item name="cypher" :title="t('graph.analysis.basicAlgos')">
            <el-radio-group v-model="selectedAlgo" class="algo-list">
              <el-radio
                v-for="algo in cypherAlgos"
                :key="algo.value"
                :value="algo.value"
                class="algo-item"
              >
                <div class="algo-name">{{ algo.label }}</div>
                <div class="algo-desc">{{ algo.desc }}</div>
              </el-radio>
            </el-radio-group>
          </el-collapse-item>

          <!-- GDS 算法 -->
          <el-collapse-item name="gds">
            <template #title>
              <span>{{ t('graph.analysis.gdsAlgos') }}</span>
              <el-tag v-if="!capabilities?.gds_available" type="info" size="small" style="margin-left:6px">{{ t('graph.analysis.needPlugin') }}</el-tag>
            </template>
            <el-radio-group v-model="selectedAlgo" class="algo-list" :disabled="!capabilities?.gds_available">
              <el-radio
                v-for="algo in gdsAlgos"
                :key="algo.value"
                :value="algo.value"
                class="algo-item"
                :disabled="!capabilities?.gds_available"
              >
                <div class="algo-name">{{ algo.label }}</div>
                <div class="algo-desc">{{ algo.desc }}</div>
              </el-radio>
            </el-radio-group>
          </el-collapse-item>

          <!-- Spatial 算法 -->
          <el-collapse-item name="spatial">
            <template #title>
              <span>{{ t('graph.analysis.spatialAlgos') }}</span>
              <el-tag v-if="!capabilities?.spatial_available" type="info" size="small" style="margin-left:6px">{{ t('graph.analysis.needPlugin') }}</el-tag>
            </template>
            <el-radio-group v-model="selectedAlgo" class="algo-list" :disabled="!capabilities?.spatial_available">
              <el-radio
                v-for="algo in spatialAlgos"
                :key="algo.value"
                :value="algo.value"
                class="algo-item"
                :disabled="!capabilities?.spatial_available"
              >
                <div class="algo-name">{{ algo.label }}</div>
                <div class="algo-desc">{{ algo.desc }}</div>
              </el-radio>
            </el-radio-group>
          </el-collapse-item>
        </el-collapse>

        <div v-if="selectedAlgo" class="section-title" style="margin-top:16px">{{ t('graph.analysis.paramConfig') }}</div>

        <!-- 度中心性 -->
        <template v-if="selectedAlgo === 'degree_centrality'">
          <el-form label-width="80px" size="small">
            <el-form-item :label="t('graph.analysis.resultCount')">
              <el-input-number v-model="params.limit" :min="5" :max="200" style="width:100%" />
            </el-form-item>
            <el-form-item :label="t('graph.analysis.nodeShape')">
              <el-select v-model="params.nodeShapes" multiple :placeholder="t('graph.analysis.allNodeShapes')" style="width:100%">
                <el-option v-for="shape in nodeShapes" :key="shape.name" :label="shape.name" :value="shape.name" />
              </el-select>
            </el-form-item>
          </el-form>
        </template>

        <!-- K跳邻居 -->
        <template v-if="selectedAlgo === 'khop_neighbors'">
          <el-form label-width="80px" size="small">
            <el-form-item :label="t('graph.analysis.startNode')">
              <el-select
                v-model="params.nodeId"
                filterable
                remote
                clearable
                :placeholder="t('graph.analysis.searchNodePlaceholder')"
                :remote-method="(q) => fetchNodes(q, 'khop')"
                :loading="nodeSearch.khop.loading"
                style="width:100%"
                value-key="id"
              >
                <el-option
                  v-for="n in nodeSearch.khop.options"
                  :key="n.id"
                  :label="n.display_name"
                  :value="n.id"
                >
                  <span>{{ n.display_name }}</span>
                  <span class="node-type-tag">{{ n.entity_type || n.labels?.[0] }}</span>
                </el-option>
              </el-select>
            </el-form-item>
            <el-form-item :label="t('graph.analysis.hops')">
              <el-input-number v-model="params.hops" :min="1" :max="4" style="width:100%" />
            </el-form-item>
            <el-form-item :label="t('graph.analysis.resultLimit')">
              <el-input-number v-model="params.limit" :min="5" :max="200" style="width:100%" />
            </el-form-item>
          </el-form>
        </template>

        <!-- 多路最短路径 -->
        <template v-if="selectedAlgo === 'multi_path'">
          <el-form size="small">
            <div class="section-label">{{ t('graph.analysis.nodePairs') }}</div>
            <div v-for="(pair, idx) in params.pairs" :key="idx" class="pair-block">
              <div class="pair-index">{{ t('graph.analysis.pairIndex', { n: idx + 1 }) }}</div>
              <el-select
                v-model="pair.src"
                filterable remote clearable
                :placeholder="t('graph.analysis.srcPlaceholder')"
                :remote-method="(q) => fetchNodes(q, `pair_src_${idx}`)"
                :loading="nodeSearch[`pair_src_${idx}`]?.loading"
                style="width:100%; margin-bottom:4px"
              >
                <el-option
                  v-for="n in nodeSearch[`pair_src_${idx}`]?.options || []"
                  :key="n.id" :label="n.display_name" :value="n.id"
                >
                  <span>{{ n.display_name }}</span>
                  <span class="node-type-tag">{{ n.entity_type || n.labels?.[0] }}</span>
                </el-option>
              </el-select>
              <el-select
                v-model="pair.tgt"
                filterable remote clearable
                :placeholder="t('graph.analysis.tgtPlaceholder')"
                :remote-method="(q) => fetchNodes(q, `pair_tgt_${idx}`)"
                :loading="nodeSearch[`pair_tgt_${idx}`]?.loading"
                style="width:100%"
              >
                <el-option
                  v-for="n in nodeSearch[`pair_tgt_${idx}`]?.options || []"
                  :key="n.id" :label="n.display_name" :value="n.id"
                >
                  <span>{{ n.display_name }}</span>
                  <span class="node-type-tag">{{ n.entity_type || n.labels?.[0] }}</span>
                </el-option>
              </el-select>
              <el-button
                v-if="params.pairs.length > 1"
                link type="danger" size="small" style="margin-top:4px"
                @click="removePair(idx)"
              >{{ t('graph.analysis.remove') }}</el-button>
            </div>
            <el-button
              v-if="params.pairs.length < 5"
              link type="primary" size="small"
              @click="addPair"
            >+ {{ t('graph.analysis.addPair') }}</el-button>
          </el-form>
        </template>

        <!-- GDS 通用参数 -->
        <template v-if="['pagerank', 'louvain', 'wcc', 'betweenness'].includes(selectedAlgo)">
          <el-form label-width="80px" size="small">
            <el-form-item :label="t('graph.analysis.resultCount')">
              <el-input-number v-model="params.limit" :min="5" :max="200" style="width:100%" />
            </el-form-item>
            <el-form-item :label="t('graph.analysis.nodeShape')">
              <el-select v-model="params.nodeShapes" multiple :placeholder="t('graph.analysis.allNodeShapes')" style="width:100%">
                <el-option v-for="shape in nodeShapes" :key="shape.name" :label="shape.name" :value="shape.name" />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('graph.analysis.relationType')">
              <el-select v-model="params.relTypes" multiple :placeholder="t('graph.analysis.allRelations')" style="width:100%">
                <el-option v-for="r in availableRelTypes" :key="r" :label="r" :value="r" />
              </el-select>
              <div v-if="params.nodeShapes.length > 0 && availableRelTypes.length === 0" style="font-size:11px;color:var(--el-color-warning);margin-top:4px">
                {{ t('graph.analysis.noRelsBetweenTypes') }}
              </div>
              <div v-else-if="params.nodeShapes.length > 0" style="font-size:11px;color:var(--el-text-color-secondary);margin-top:4px">
                {{ t('graph.analysis.filteredByNodeType') }}
              </div>
            </el-form-item>
          </el-form>
        </template>

        <!-- 邻近节点参数 -->
        <template v-if="selectedAlgo === 'nearby_nodes'">
          <div v-if="!capabilities?.spatial_layers?.length" style="font-size:12px;color:var(--el-color-warning);padding:8px 0">
            {{ t('graph.analysis.noSpatialLayers') }}
          </div>
          <el-form v-else label-width="80px" size="small">
            <el-form-item :label="t('graph.analysis.spatialLayer')">
              <el-select v-model="params.layer" :placeholder="t('graph.analysis.selectSpatialLayer')" style="width:100%" @change="onLayerChange('nearby')">
                <el-option
                  v-for="l in capabilities.spatial_layers"
                  :key="l.name" :label="spatialLayerLabel(l)" :value="l.name"
                >
                  <span>{{ spatialLayerLabel(l) }}</span>
                  <span class="node-type-tag">{{ l.config?.geometry_type === 'wkt' ? t('graph.ontology.linePolygon') : t('graph.ontology.point') }}</span>
                </el-option>
              </el-select>
            </el-form-item>
            <el-form-item :label="t('graph.analysis.referenceNode')">
              <el-select
                v-model="params.nearbyNodeId"
                filterable remote clearable
                :placeholder="t('graph.analysis.searchNodePlaceholder')"
                :remote-method="(q) => fetchNodes(q, 'nearby')"
                :loading="nodeSearch.nearby?.loading"
                style="width:100%"
                @change="onNearbyNodeSelect"
              >
                <el-option
                  v-for="n in nodeSearch.nearby?.options || []"
                  :key="n.id" :label="n.display_name" :value="n.id"
                >
                  <span>{{ n.display_name }}</span>
                  <span class="node-type-tag">{{ n.entity_type || n.labels?.[0] }}</span>
                </el-option>
              </el-select>
            </el-form-item>
            <el-form-item v-if="params.nearbyNodeId" :label="t('graph.analysis.coordinates')">
              <span style="font-size:12px;color:var(--el-text-color-secondary)">
                lon: {{ params.lon !== undefined ? params.lon.toFixed(6) : '-' }},
                lat: {{ params.lat !== undefined ? params.lat.toFixed(6) : '-' }}
              </span>
            </el-form-item>
            <el-form-item :label="t('graph.analysis.radiusKm')">
              <el-input-number v-model="params.radiusKm" :min="0.1" :max="1000" :precision="1" style="width:100%" />
            </el-form-item>
            <el-form-item :label="t('graph.analysis.resultLimit')">
              <el-input-number v-model="params.limit" :min="5" :max="200" style="width:100%" />
            </el-form-item>
          </el-form>
        </template>

        <!-- 区域内节点参数 -->
        <template v-if="selectedAlgo === 'within_area'">
          <div v-if="!wktLayers.length || !pointLayers.length" style="font-size:12px;color:var(--el-color-warning);padding:8px 0">
            {{ t('graph.analysis.needWktAndPointLayers') }}
          </div>
          <el-form v-else label-width="80px" size="small">
            <el-form-item :label="t('graph.analysis.areaLayer')">
              <el-select v-model="params.areaLayer" :placeholder="t('graph.analysis.selectAreaLayer')" style="width:100%" @change="onAreaLayerChange">
                <el-option v-for="l in wktLayers" :key="l.name" :label="spatialLayerLabel(l)" :value="l.name" />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('graph.analysis.areaNode')">
              <el-select
                v-model="params.areaNodeId"
                filterable remote clearable
                :placeholder="t('graph.analysis.searchAreaNode')"
                :remote-method="(q) => fetchNodes(q, 'area')"
                :loading="nodeSearch.area?.loading"
                style="width:100%"
                :disabled="!params.areaLayer"
              >
                <el-option
                  v-for="n in (nodeSearch.area?.options || []).filter(n => nodeMatchesSpatialLayer(n, params.areaLayer))"
                  :key="n.id" :label="n.display_name" :value="n.id"
                >
                  <span>{{ n.display_name }}</span>
                  <span class="node-type-tag">{{ n.entity_type || n.labels?.[0] }}</span>
                </el-option>
              </el-select>
            </el-form-item>
            <el-form-item :label="t('graph.analysis.pointLayer')">
              <el-select v-model="params.pointLayer" :placeholder="t('graph.analysis.selectPointLayer')" style="width:100%">
                <el-option v-for="l in pointLayers" :key="l.name" :label="spatialLayerLabel(l)" :value="l.name" />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('graph.analysis.resultLimit')">
              <el-input-number v-model="params.limit" :min="5" :max="200" style="width:100%" />
            </el-form-item>
          </el-form>
        </template>

        <el-button
          type="primary"
          :loading="running"
          :disabled="!selectedAlgo"
          style="width:100%; margin-top:8px"
          @click="runAlgorithm"
        >
          {{ t('graph.analysis.runAnalysis') }}
        </el-button>
      </div>

      <!-- 右侧：结果展示 -->
      <div class="result-panel">
        <div v-if="!result && !running" class="result-empty">
          <el-empty :description="t('graph.analysis.resultEmpty')" />
        </div>

        <div v-if="running" class="result-empty" v-loading="true" :element-loading-text="t('graph.analysis.running')" style="min-height:300px" />

        <template v-if="result && !running">
          <div class="result-header">
            <span class="result-title">{{ result.algorithm_name }}</span>
            <div class="result-meta">
              <el-tag v-if="result.metadata?.elapsed_ms !== undefined" size="small" type="info">
                {{ t('graph.analysis.elapsed') }} {{ result.metadata.elapsed_ms }}ms
              </el-tag>
              <el-tag v-if="result.metadata?.node_count !== undefined" size="small" type="success">
                {{ result.metadata.node_count }} {{ t('graph.analysis.nodeCount') }}
              </el-tag>
              <el-tag v-if="result.metadata?.relationship_count !== undefined" size="small" type="success">
                {{ result.metadata.relationship_count }} {{ t('graph.analysis.edgeCount') }}
              </el-tag>
              <el-tag v-if="result.metadata?.community_count !== undefined" size="small" type="warning">
                {{ result.metadata.community_count }} {{ t('graph.analysis.communityCount') }}
              </el-tag>
              <el-tag v-if="result.metadata?.score_unit" size="small" type="info">
                {{ t('graph.analysis.distanceUnit') }}: {{ result.metadata.score_unit }}
              </el-tag>
            </div>
            <el-tag v-if="result.warning" type="warning" size="small" style="margin-top:4px">
              {{ result.warning }}
            </el-tag>
          </div>

          <el-table
            v-if="result.node_scores && result.node_scores.length > 0"
            :data="result.node_scores"
            border stripe size="small"
            style="width:100%"
            max-height="600"
          >
            <el-table-column prop="rank" :label="t('graph.analysis.rank')" width="60" align="center" />
            <el-table-column prop="display_name" :label="t('graph.analysis.nodeName')" min-width="160" show-overflow-tooltip />
            <el-table-column prop="entity_type" :label="t('graph.analysis.entityType')" width="120" />
            <el-table-column v-if="result.algorithm !== 'within_area'" :label="scoreColumnLabel" width="120" align="right">
              <template #default="{ row }">{{ formatScore(row) }}</template>
            </el-table-column>
            <el-table-column
              v-if="result.algorithm === 'louvain' || result.algorithm === 'wcc'"
              prop="community_id" :label="t('graph.analysis.communityId')" width="90" align="center"
            />
          </el-table>

          <div
            v-if="result.subgraph && (result.subgraph.nodes?.length > 0 || result.subgraph.edges?.length > 0)"
            class="subgraph-area"
          >
            <GraphCanvas
              ref="canvasRef"
              :nodes="result.subgraph.nodes || []"
              :edges="result.subgraph.edges || []"
              :center-node-id="result.metadata?.center_node_id || ''"
              layout="force"
            />
          </div>

          <div v-if="result.subgraph && result.subgraph.nodes?.length === 0" class="result-empty">
            <el-empty :description="t('graph.analysis.noPathOrNeighbors')" />
          </div>
          <div v-if="result.node_scores && result.node_scores.length === 0 && !(result.subgraph?.nodes?.length > 0)" class="result-empty">
            <el-empty :description="t('graph.analysis.noMatchingData')" />
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { knowledgeGraphAPI } from '../api/ontology'
import { analysisAPI } from '../api/analysis'
import { browseAPI } from '../api/browse'
import GraphCanvas from '../components/GraphCanvas.vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const graphs = ref([])
const selectedGraphId = ref(null)
const capabilities = ref(null)
const schema = ref({ node_shapes: [], relationship_shapes: [] })
const syncing = ref(false)

// 默认展开基础算法分组
const openGroups = ref(['cypher'])

const nodeShapes = computed(() => {
  return schema.value.node_shapes || []
})

const selectedNodeShapeFilters = computed(() => {
  if (!params.nodeShapes?.length) return []
  const selected = new Set(params.nodeShapes)
  return nodeShapes.value
    .filter(shape => selected.has(shape.name))
    .map(shape => ({
      name: shape.name,
      labels: shape.labels?.length ? shape.labels : [shape.name],
    }))
})

const relationshipShapes = computed(() => schema.value.relationship_shapes || [])

const relationshipTypes = computed(() => relationshipShapes.value.map(shape => shape.type).filter(Boolean))

// 根据已选节点形状动态过滤可用关系类型
const availableRelTypes = computed(() => {
  const selected = selectedNodeShapeFilters.value
  if (!selected || selected.length === 0) return relationshipTypes.value
  if (relationshipShapes.value.length === 0) return relationshipTypes.value
  const validRelTypes = new Set()
  for (const shape of relationshipShapes.value) {
    for (const pattern of shape.patterns || []) {
      const fromLabels = pattern.from?.labels || []
      const toLabels = pattern.to?.labels || []
      if (selected.some(item => sameLabelSet(fromLabels, item.labels)) && selected.some(item => sameLabelSet(toLabels, item.labels))) {
        validRelTypes.add(shape.type)
      }
    }
  }
  return relationshipTypes.value.filter(r => validRelTypes.has(r))
})

const sameLabelSet = (a, b) => {
  const left = [...new Set((a || []).filter(Boolean))].sort()
  const right = [...new Set((b || []).filter(Boolean))].sort()
  return left.length === right.length && left.every((value, index) => value === right[index])
}

// 面图层（WKT 几何类型）用于 within_area 的区域选择
const wktLayers = computed(() =>
  (capabilities.value?.spatial_layers || []).filter(l => l.config?.geometry_type === 'wkt')
)
// 点图层用于 within_area 的目标节点查询
const pointLayers = computed(() =>
  (capabilities.value?.spatial_layers || []).filter(l => l.config?.geometry_type !== 'wkt')
)

const spatialLayerLabel = (layer) => {
  if (!layer) return ''
  const parts = [layer.name]
  const typeLabel = layer.entity_type_label || layer.entity_type
  if (typeLabel) parts.push(typeLabel)
  return parts.join(' · ')
}

const nodeMatchesSpatialLayer = (node, layerName) => {
  const layerInfo = capabilities.value?.spatial_layers?.find(l => l.name === layerName)
  const labels = layerInfo?.node_labels || []
  return labels.length > 0 && (node.labels || []).some(label => labels.includes(label))
}

const selectedAlgo = ref('')
const running = ref(false)
const result = ref(null)
const canvasRef = ref(null)

// 节点搜索状态（每个选择器独立）
const nodeSearch = reactive({
  khop: { loading: false, options: [] },
  nearby: { loading: false, options: [] },
})

const params = reactive({
  limit: 50,
  nodeShapes: [],
  relTypes: [],
  nodeId: '',
  hops: 2,
  pairs: [{ src: '', tgt: '' }],
  // Spatial 参数
  layer: '',
  nearbyNodeId: '',
  lon: undefined,
  lat: undefined,
  radiusKm: 10,
  // within_area 参数
  areaLayer: '',
  areaNodeId: '',
  pointLayer: '',
})

const cypherAlgos = computed(() => [
  { value: 'degree_centrality', label: t('graph.analysis.degreeCentrality'), desc: t('graph.analysis.degreeCentralityDesc') },
  { value: 'khop_neighbors', label: t('graph.analysis.khopNeighbors'), desc: t('graph.analysis.khopNeighborsDesc') },
  { value: 'multi_path', label: t('graph.analysis.multiPath'), desc: t('graph.analysis.multiPathDesc') },
])

const gdsAlgos = computed(() => [
  { value: 'pagerank', label: 'PageRank', desc: t('graph.analysis.pagerankDesc') },
  { value: 'betweenness', label: t('graph.analysis.betweenness'), desc: t('graph.analysis.betweennessDesc') },
  { value: 'louvain', label: t('graph.analysis.louvain'), desc: t('graph.analysis.louvainDesc') },
  { value: 'wcc', label: t('graph.analysis.wcc'), desc: t('graph.analysis.wccDesc') },
])

const spatialAlgos = computed(() => [
  { value: 'nearby_nodes', label: t('graph.analysis.nearbyNodes'), desc: t('graph.analysis.nearbyNodesDesc') },
  { value: 'within_area', label: t('graph.analysis.withinArea'), desc: t('graph.analysis.withinAreaDesc') },
])

// 得分列标题（Spatial 算法显示距离单位）
const scoreColumnLabel = computed(() => {
  if (result.value?.algorithm === 'nearby_nodes') return t('graph.analysis.distanceKm')
  if (result.value?.algorithm === 'louvain' || result.value?.algorithm === 'wcc') return t('graph.analysis.community')
  return t('graph.analysis.score')
})

// 通用节点搜索
let searchTimers = {}
const fetchNodes = (query, key) => {
  if (!nodeSearch[key]) {
    nodeSearch[key] = { loading: false, options: [] }
  }
  if (!query || query.length < 1) {
    nodeSearch[key].options = []
    return
  }
  clearTimeout(searchTimers[key])
  searchTimers[key] = setTimeout(async () => {
    nodeSearch[key].loading = true
    try {
      const res = await browseAPI.searchNodes(selectedGraphId.value, query, 20)
      const data = res.data || res
      nodeSearch[key].options = data.nodes || []
    } catch {
      nodeSearch[key].options = []
    } finally {
      nodeSearch[key].loading = false
    }
  }, 300)
}

const addPair = () => { params.pairs.push({ src: '', tgt: '' }) }
const removePair = (idx) => { params.pairs.splice(idx, 1) }

// 切换空间图层时重置节点选择
const onLayerChange = () => {
  params.nearbyNodeId = ''
  params.lon = undefined
  params.lat = undefined
}

// 切换面图层时重置区域节点选择
const onAreaLayerChange = () => {
  params.areaNodeId = ''
  nodeSearch.area = { loading: false, options: [] }
}

// 选中参照节点后自动提取坐标
const onNearbyNodeSelect = (nodeId) => {
  if (!nodeId) {
    params.lon = undefined
    params.lat = undefined
    return
  }
  const layerInfo = capabilities.value?.spatial_layers?.find(l => l.name === params.layer)
  const selectedNode = nodeSearch.nearby?.options?.find(n => n.id === nodeId)
  if (!selectedNode || !layerInfo?.config) return

  const cfg = layerInfo.config
  if (cfg.geometry_type === 'point') {
    const lonField = cfg.lon_field || 'lon'
    const latField = cfg.lat_field || 'lat'
    params.lon = selectedNode.properties?.[lonField]
    params.lat = selectedNode.properties?.[latField]
  } else if (cfg.geometry_type === 'wkt') {
    // 从 WKT POINT(lon lat) 中提取坐标
    const geomField = cfg.geom_field || 'wkt'
    const wkt = selectedNode.properties?.[geomField]
    if (wkt) {
      const match = wkt.match(/POINT\s*\(\s*([-\d.]+)\s+([-\d.]+)\s*\)/i)
      if (match) {
        params.lon = parseFloat(match[1])
        params.lat = parseFloat(match[2])
      }
    }
  }
}

const load = async () => {
  try {
    graphs.value = await knowledgeGraphAPI.list() || []
  } catch {
    ElMessage.error(t('graph.common.loadFailed'))
  }
}

const syncSpatialLayers = async () => {
  if (!selectedGraphId.value) return
  syncing.value = true
  try {
    const res = await analysisAPI.syncSpatialLayers(selectedGraphId.value)
    const data = res.data || res
    ElMessage.success(`${data.message}（${data.count} 个图层）`)
    // 刷新 capabilities
    const caps = await analysisAPI.getCapabilities(selectedGraphId.value)
    capabilities.value = caps.data || caps
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.analysis.syncFailed'))
  } finally {
    syncing.value = false
  }
}

const onGraphChange = async (id) => {
  capabilities.value = null
  schema.value = { node_shapes: [], relationship_shapes: [] }
  result.value = null
  selectedAlgo.value = ''
  if (!id) return
  try {
    const [caps, sc] = await Promise.all([
      analysisAPI.getCapabilities(id),
      browseAPI.getSchema(id),
    ])
    capabilities.value = caps.data || caps
    schema.value = sc.data || sc
    // 根据能力自动展开对应分组
    openGroups.value = ['cypher']
    if (capabilities.value?.gds_available) openGroups.value.push('gds')
    if (capabilities.value?.spatial_available) openGroups.value.push('spatial')
  } catch {
    ElMessage.error(t('graph.analysis.loadGraphFailed'))
  }
}

const runAlgorithm = async () => {
  if (!selectedAlgo.value) return

  const req = {
    algorithm: selectedAlgo.value,
    limit: params.limit,
    node_shapes: selectedNodeShapeFilters.value,
    rel_types: params.relTypes,
    params: {},
  }

  if (selectedAlgo.value === 'khop_neighbors') {
    if (!params.nodeId) { ElMessage.warning(t('graph.analysis.selectStartNode')); return }
    req.params = { node_id: params.nodeId, hops: params.hops }
  } else if (selectedAlgo.value === 'multi_path') {
    const validPairs = params.pairs.filter(p => p.src && p.tgt)
    if (validPairs.length === 0) { ElMessage.warning(t('graph.analysis.selectAtLeastOnePair')); return }
    req.params = { pairs: validPairs.map(p => [p.src, p.tgt]) }
  } else if (selectedAlgo.value === 'nearby_nodes') {
    if (!params.layer) { ElMessage.warning(t('graph.analysis.selectSpatialLayer')); return }
    if (!params.nearbyNodeId) { ElMessage.warning(t('graph.analysis.selectReferenceNode')); return }
    if (params.lon === undefined || params.lat === undefined) { ElMessage.warning(t('graph.analysis.noCoordinates')); return }
    req.params = {
      lon: params.lon, lat: params.lat,
      radius_km: params.radiusKm,
      layer: params.layer,
    }
  } else if (selectedAlgo.value === 'within_area') {
    if (!params.areaLayer) { ElMessage.warning(t('graph.analysis.selectAreaLayer')); return }
    if (!params.areaNodeId) { ElMessage.warning(t('graph.analysis.selectAreaNode')); return }
    if (!params.pointLayer) { ElMessage.warning(t('graph.analysis.selectPointLayer')); return }
    const areaLayerInfo = capabilities.value?.spatial_layers?.find(l => l.name === params.areaLayer)
    const geomField = areaLayerInfo?.config?.geom_field || 'wkt'
    req.params = {
      area_layer: params.areaLayer,
      area_node_id: params.areaNodeId,
      area_geom_field: geomField,
      point_layer: params.pointLayer,
    }
  }

  running.value = true
  result.value = null
  try {
    const res = await analysisAPI.runAlgorithm(selectedGraphId.value, req)
    result.value = res.data || res
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.analysis.algoFailed'))
  } finally {
    running.value = false
  }
}

watch(selectedAlgo, () => {
  params.limit = 50
  params.nodeShapes = []
  params.relTypes = []
  params.nodeId = ''
  params.hops = 2
  params.pairs = [{ src: '', tgt: '' }]
  params.layer = ''
  params.nearbyNodeId = ''
  params.lon = undefined
  params.lat = undefined
  params.areaLayer = ''
  params.areaNodeId = ''
  params.pointLayer = ''
  result.value = null
  nodeSearch.khop = { loading: false, options: [] }
  nodeSearch.nearby = { loading: false, options: [] }
  nodeSearch.area = { loading: false, options: [] }
})

// 节点形状变化时，自动清除不再有效的关系类型选择
watch(() => params.nodeShapes, () => {
  const valid = new Set(availableRelTypes.value)
  params.relTypes = params.relTypes.filter(r => valid.has(r))
})

const formatScore = (row) => {
  const algo = result.value?.algorithm
  if (algo === 'louvain' || algo === 'wcc') return `#${row.community_id}`
  if (algo === 'nearby_nodes') return typeof row.score === 'number' ? row.score.toFixed(3) : row.score
  if (typeof row.score === 'number') {
    return row.score > 10 ? Math.round(row.score) : row.score.toFixed(4)
  }
  return row.score
}

onMounted(load)
</script>

<style scoped>
.analysis-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: 16px;
  box-sizing: border-box;
  overflow: hidden;
}

.page-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
  flex-shrink: 0;
}

.page-header h2 {
  margin: 0;
  font-size: 18px;
  white-space: nowrap;
}

.empty-hint {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.main-area {
  flex: 1;
  display: flex;
  gap: 16px;
  overflow: hidden;
  min-height: 0;
}

.config-panel {
  width: 280px;
  flex-shrink: 0;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  padding: 16px;
  overflow-y: auto;
}

.section-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 10px;
}

.section-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 8px;
}

/* 折叠面板样式 */
.algo-collapse {
  border: none;
}

.algo-collapse :deep(.el-collapse-item__header) {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary);
  background: transparent;
  border-bottom: 1px solid var(--el-border-color-lighter);
  padding: 0 0 0 4px;
  height: 34px;
  line-height: 34px;
}

.algo-collapse :deep(.el-collapse-item__wrap) {
  border-bottom: none;
  background: transparent;
}

.algo-collapse :deep(.el-collapse-item__content) {
  padding: 8px 0 4px;
}

.algo-list {
  display: flex !important;
  flex-direction: column !important;
  gap: 4px;
  width: 100%;
}

.algo-item {
  width: 100%;
  display: flex !important;
  align-items: flex-start;
  margin-right: 0 !important;
  padding: 6px 8px;
  border-radius: 4px;
  border: 1px solid transparent;
  transition: all 0.15s;
  height: auto !important;
}

.algo-item:hover {
  border-color: var(--el-color-primary-light-5);
  background: var(--el-color-primary-light-9);
}

.algo-name {
  font-size: 13px;
  font-weight: 500;
  line-height: 1.4;
}

.algo-desc {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  line-height: 1.4;
  margin-top: 2px;
}

.node-type-tag {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  margin-left: 6px;
}

.pair-block {
  background: var(--el-fill-color-light);
  border-radius: 4px;
  padding: 8px;
  margin-bottom: 8px;
}

.pair-index {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
}

.result-panel {
  flex: 1;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  padding: 16px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.result-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.result-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  flex-wrap: wrap;
  flex-shrink: 0;
}

.result-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.result-meta {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.subgraph-area {
  flex: 1;
  min-height: 400px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 4px;
  overflow: hidden;
}
</style>
