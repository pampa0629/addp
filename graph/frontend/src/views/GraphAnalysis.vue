<template>
  <div class="analysis-page">
    <!-- 顶部：图谱选择 -->
    <div class="page-header">
      <h2>图算法分析</h2>
      <el-select
        v-model="selectedGraphId"
        placeholder="请选择知识图谱"
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
        {{ capabilities.gds_available ? `GDS ${capabilities.gds_version}` : 'GDS 不可用' }}
      </el-tag>
      <el-tag v-if="capabilities" :type="capabilities.spatial_available ? 'success' : 'info'" size="small">
        {{ capabilities.spatial_available ? 'Spatial 可用' : 'Spatial 不可用' }}
      </el-tag>
      <el-button
        v-if="capabilities?.spatial_available && capabilities?.pending_layers?.length > 0"
        size="small"
        type="warning"
        :loading="syncing"
        @click="syncSpatialLayers"
      >
        同步空间图层 ({{ capabilities.pending_layers.length }})
      </el-button>
    </div>

    <div v-if="!selectedGraphId" class="empty-hint">
      <el-empty description="请先选择知识图谱" />
    </div>

    <div v-else class="main-area">
      <!-- 左侧：算法配置 -->
      <div class="config-panel">
        <div class="section-title">选择算法</div>

        <el-collapse v-model="openGroups" class="algo-collapse">
          <!-- 基础算法 -->
          <el-collapse-item name="cypher" title="基础算法">
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
              <span>GDS 算法</span>
              <el-tag v-if="!capabilities?.gds_available" type="info" size="small" style="margin-left:6px">需插件</el-tag>
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
              <span>Spatial 算法</span>
              <el-tag v-if="!capabilities?.spatial_available" type="info" size="small" style="margin-left:6px">需插件</el-tag>
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

        <!-- 参数配置 -->
        <div v-if="selectedAlgo" class="section-title" style="margin-top:16px">参数配置</div>

        <!-- 度中心性 -->
        <template v-if="selectedAlgo === 'degree_centrality'">
          <el-form label-width="80px" size="small">
            <el-form-item label="结果数量">
              <el-input-number v-model="params.limit" :min="5" :max="200" style="width:100%" />
            </el-form-item>
            <el-form-item label="实体类型">
              <el-select v-model="params.nodeLabels" multiple placeholder="不限（全部类型）" style="width:100%">
                <el-option v-for="l in schema.labels" :key="l" :label="l" :value="l" />
              </el-select>
            </el-form-item>
          </el-form>
        </template>

        <!-- K跳邻居 -->
        <template v-if="selectedAlgo === 'khop_neighbors'">
          <el-form label-width="80px" size="small">
            <el-form-item label="起始节点">
              <el-select
                v-model="params.nodeId"
                filterable
                remote
                clearable
                placeholder="输入名称搜索节点"
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
            <el-form-item label="跳数">
              <el-input-number v-model="params.hops" :min="1" :max="4" style="width:100%" />
            </el-form-item>
            <el-form-item label="结果上限">
              <el-input-number v-model="params.limit" :min="5" :max="200" style="width:100%" />
            </el-form-item>
          </el-form>
        </template>

        <!-- 多路最短路径 -->
        <template v-if="selectedAlgo === 'multi_path'">
          <el-form size="small">
            <div class="section-label">节点对（最多 5 对）</div>
            <div v-for="(pair, idx) in params.pairs" :key="idx" class="pair-block">
              <div class="pair-index">第 {{ idx + 1 }} 对</div>
              <el-select
                v-model="pair.src"
                filterable remote clearable
                placeholder="起点：输入名称搜索"
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
                placeholder="终点：输入名称搜索"
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
              >移除</el-button>
            </div>
            <el-button
              v-if="params.pairs.length < 5"
              link type="primary" size="small"
              @click="addPair"
            >+ 添加节点对</el-button>
          </el-form>
        </template>

        <!-- GDS 通用参数 -->
        <template v-if="['pagerank', 'louvain', 'wcc', 'betweenness'].includes(selectedAlgo)">
          <el-form label-width="80px" size="small">
            <el-form-item label="结果数量">
              <el-input-number v-model="params.limit" :min="5" :max="200" style="width:100%" />
            </el-form-item>
            <el-form-item label="实体类型">
              <el-select v-model="params.nodeLabels" multiple placeholder="不限（全部类型）" style="width:100%">
                <el-option v-for="l in schema.labels" :key="l" :label="l" :value="l" />
              </el-select>
            </el-form-item>
            <el-form-item label="关系类型">
              <el-select v-model="params.relTypes" multiple placeholder="不限（全部关系）" style="width:100%">
                <el-option v-for="r in availableRelTypes" :key="r" :label="r" :value="r" />
              </el-select>
              <div v-if="params.nodeLabels.length > 0 && availableRelTypes.length === 0" style="font-size:11px;color:var(--el-color-warning);margin-top:4px">
                所选节点类型之间无直接关系，GDS 投影将为空图
              </div>
              <div v-else-if="params.nodeLabels.length > 0" style="font-size:11px;color:var(--el-text-color-secondary);margin-top:4px">
                已按节点类型过滤（仅显示两端节点均在选中类型内的关系）
              </div>
            </el-form-item>
          </el-form>
        </template>

        <!-- 邻近节点参数 -->
        <template v-if="selectedAlgo === 'nearby_nodes'">
          <div v-if="!capabilities?.spatial_layers?.length" style="font-size:12px;color:var(--el-color-warning);padding:8px 0">
            请先在本体中定义空间图层类型并同步到该图谱
          </div>
          <el-form v-else label-width="80px" size="small">
            <el-form-item label="空间图层">
              <el-select v-model="params.layer" placeholder="选择空间图层" style="width:100%" @change="onLayerChange('nearby')">
                <el-option
                  v-for="l in capabilities.spatial_layers"
                  :key="l.name" :label="l.name" :value="l.name"
                >
                  <span>{{ l.name }}</span>
                  <span class="node-type-tag">{{ l.config?.geometry_type === 'wkt' ? '线面' : '点' }}</span>
                </el-option>
              </el-select>
            </el-form-item>
            <el-form-item label="参照节点">
              <el-select
                v-model="params.nearbyNodeId"
                filterable remote clearable
                placeholder="输入名称搜索节点"
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
            <el-form-item v-if="params.nearbyNodeId" label="坐标">
              <span style="font-size:12px;color:var(--el-text-color-secondary)">
                lon: {{ params.lon !== undefined ? params.lon.toFixed(6) : '-' }},
                lat: {{ params.lat !== undefined ? params.lat.toFixed(6) : '-' }}
              </span>
            </el-form-item>
            <el-form-item label="半径(km)">
              <el-input-number v-model="params.radiusKm" :min="0.1" :max="1000" :precision="1" style="width:100%" />
            </el-form-item>
            <el-form-item label="结果上限">
              <el-input-number v-model="params.limit" :min="5" :max="200" style="width:100%" />
            </el-form-item>
          </el-form>
        </template>

        <!-- 区域内节点参数 -->
        <template v-if="selectedAlgo === 'within_area'">
          <div v-if="!wktLayers.length || !pointLayers.length" style="font-size:12px;color:var(--el-color-warning);padding:8px 0">
            需要至少一个面图层（WKT）和一个点图层，请先在本体中定义并同步
          </div>
          <el-form v-else label-width="80px" size="small">
            <el-form-item label="面图层">
              <el-select v-model="params.areaLayer" placeholder="选择区域图层（如城市）" style="width:100%" @change="onAreaLayerChange">
                <el-option v-for="l in wktLayers" :key="l.name" :label="l.name" :value="l.name" />
              </el-select>
            </el-form-item>
            <el-form-item label="区域节点">
              <el-select
                v-model="params.areaNodeId"
                filterable remote clearable
                placeholder="输入名称搜索区域节点"
                :remote-method="(q) => fetchNodes(q, 'area')"
                :loading="nodeSearch.area?.loading"
                style="width:100%"
                :disabled="!params.areaLayer"
              >
                <el-option
                  v-for="n in (nodeSearch.area?.options || []).filter(n => (n.labels || []).includes(params.areaLayer) || n.entity_type === params.areaLayer)"
                  :key="n.id" :label="n.display_name" :value="n.id"
                >
                  <span>{{ n.display_name }}</span>
                  <span class="node-type-tag">{{ n.entity_type || n.labels?.[0] }}</span>
                </el-option>
              </el-select>
            </el-form-item>
            <el-form-item label="点图层">
              <el-select v-model="params.pointLayer" placeholder="选择点图层（如公司）" style="width:100%">
                <el-option v-for="l in pointLayers" :key="l.name" :label="l.name" :value="l.name" />
              </el-select>
            </el-form-item>
            <el-form-item label="结果上限">
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
          执行分析
        </el-button>
      </div>

      <!-- 右侧：结果展示 -->
      <div class="result-panel">
        <div v-if="!result && !running" class="result-empty">
          <el-empty description="选择算法并执行后，结果将在此显示" />
        </div>

        <div v-if="running" class="result-empty" v-loading="true" element-loading-text="算法执行中..." style="min-height:300px" />

        <template v-if="result && !running">
          <div class="result-header">
            <span class="result-title">{{ result.algorithm_name }}</span>
            <div class="result-meta">
              <el-tag v-if="result.metadata?.elapsed_ms !== undefined" size="small" type="info">
                耗时 {{ result.metadata.elapsed_ms }}ms
              </el-tag>
              <el-tag v-if="result.metadata?.node_count !== undefined" size="small" type="success">
                {{ result.metadata.node_count }} 个节点
              </el-tag>
              <el-tag v-if="result.metadata?.edge_count !== undefined" size="small" type="success">
                {{ result.metadata.edge_count }} 条关系
              </el-tag>
              <el-tag v-if="result.metadata?.community_count !== undefined" size="small" type="warning">
                {{ result.metadata.community_count }} 个社区
              </el-tag>
              <el-tag v-if="result.metadata?.score_unit" size="small" type="info">
                距离单位: {{ result.metadata.score_unit }}
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
            <el-table-column prop="rank" label="排名" width="60" align="center" />
            <el-table-column prop="display_name" label="节点名称" min-width="160" show-overflow-tooltip />
            <el-table-column prop="entity_type" label="实体类型" width="120" />
            <el-table-column v-if="result.algorithm !== 'within_area'" :label="scoreColumnLabel" width="120" align="right">
              <template #default="{ row }">{{ formatScore(row) }}</template>
            </el-table-column>
            <el-table-column
              v-if="result.algorithm === 'louvain' || result.algorithm === 'wcc'"
              prop="community_id" label="社区 ID" width="90" align="center"
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
            <el-empty description="未找到符合条件的路径或邻居" />
          </div>
          <div v-if="result.node_scores && result.node_scores.length === 0 && !(result.subgraph?.nodes?.length > 0)" class="result-empty">
            <el-empty description="图谱中暂无符合条件的数据" />
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

const graphs = ref([])
const selectedGraphId = ref(null)
const capabilities = ref(null)
const schema = ref({ labels: [], rel_types: [], connections: [] })
const syncing = ref(false)

// 默认展开基础算法分组
const openGroups = ref(['cypher'])

// 根据已选节点类型动态过滤可用关系类型
const availableRelTypes = computed(() => {
  const selected = params.nodeLabels
  if (!selected || selected.length === 0) return schema.value.rel_types || []
  const connections = schema.value.connections || []
  if (connections.length === 0) return schema.value.rel_types || []
  const selectedSet = new Set(selected)
  const validRelTypes = new Set()
  for (const conn of connections) {
    if (selectedSet.has(conn.source_label) && selectedSet.has(conn.target_label)) {
      validRelTypes.add(conn.rel_type)
    }
  }
  return (schema.value.rel_types || []).filter(r => validRelTypes.has(r))
})

// 面图层（WKT 几何类型）用于 within_area 的区域选择
const wktLayers = computed(() =>
  (capabilities.value?.spatial_layers || []).filter(l => l.config?.geometry_type === 'wkt')
)
// 点图层用于 within_area 的目标节点查询
const pointLayers = computed(() =>
  (capabilities.value?.spatial_layers || []).filter(l => l.config?.geometry_type !== 'wkt')
)

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
  nodeLabels: [],
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

const cypherAlgos = [
  { value: 'degree_centrality', label: '度中心性',    desc: '按节点的关联关系数量排名' },
  { value: 'khop_neighbors',    label: 'K跳邻居',    desc: '展示指定节点的 K 跳邻居子图' },
  { value: 'multi_path',        label: '多路最短路径', desc: '计算多对节点间的所有最短路径' },
]

const gdsAlgos = [
  { value: 'pagerank',    label: 'PageRank',        desc: '衡量节点在图中的综合影响力' },
  { value: 'betweenness', label: '介数中心性',       desc: '衡量节点作为其他节点间路径桥梁的程度' },
  { value: 'louvain',     label: 'Louvain 社区发现', desc: '将图划分为高度内聚的社区' },
  { value: 'wcc',         label: '弱连通分量',        desc: '识别图中互相不可达的独立连通子图' },
]

const spatialAlgos = [
  { value: 'nearby_nodes', label: '邻近节点',   desc: '查找指定坐标半径范围内的节点，按距离排序' },
  { value: 'within_area',  label: '区域内节点', desc: '查找面要素（如城市）范围内的所有点节点（如公司）' },
]

// 得分列标题（Spatial 算法显示距离单位）
const scoreColumnLabel = computed(() => {
  if (result.value?.algorithm === 'nearby_nodes') return '距离(km)'
  if (result.value?.algorithm === 'louvain' || result.value?.algorithm === 'wcc') return '社区'
  return '得分'
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
    ElMessage.error('加载图谱列表失败')
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
    ElMessage.error(e.response?.data?.error || '空间图层同步失败')
  } finally {
    syncing.value = false
  }
}

const onGraphChange = async (id) => {
  capabilities.value = null
  schema.value = { labels: [], rel_types: [], connections: [] }
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
    ElMessage.error('加载图谱信息失败')
  }
}

const runAlgorithm = async () => {
  if (!selectedAlgo.value) return

  const req = {
    algorithm: selectedAlgo.value,
    limit: params.limit,
    node_labels: params.nodeLabels,
    rel_types: params.relTypes,
    params: {},
  }

  if (selectedAlgo.value === 'khop_neighbors') {
    if (!params.nodeId) { ElMessage.warning('请搜索并选择起始节点'); return }
    req.params = { node_id: params.nodeId, hops: params.hops }
  } else if (selectedAlgo.value === 'multi_path') {
    const validPairs = params.pairs.filter(p => p.src && p.tgt)
    if (validPairs.length === 0) { ElMessage.warning('请至少选择一对起点和终点节点'); return }
    req.params = { pairs: validPairs.map(p => [p.src, p.tgt]) }
  } else if (selectedAlgo.value === 'nearby_nodes') {
    if (!params.layer) { ElMessage.warning('请选择空间图层'); return }
    if (!params.nearbyNodeId) { ElMessage.warning('请搜索并选择参照节点'); return }
    if (params.lon === undefined || params.lat === undefined) { ElMessage.warning('所选节点无坐标信息'); return }
    req.params = {
      lon: params.lon, lat: params.lat,
      radius_km: params.radiusKm,
      layer: params.layer,
    }
  } else if (selectedAlgo.value === 'within_area') {
    if (!params.areaLayer) { ElMessage.warning('请选择面图层'); return }
    if (!params.areaNodeId) { ElMessage.warning('请搜索并选择区域节点'); return }
    if (!params.pointLayer) { ElMessage.warning('请选择点图层'); return }
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
    ElMessage.error(e.response?.data?.error || '算法执行失败')
  } finally {
    running.value = false
  }
}

watch(selectedAlgo, () => {
  params.limit = 50
  params.nodeLabels = []
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

// 节点类型变化时，自动清除不再有效的关系类型选择
watch(() => params.nodeLabels, () => {
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
