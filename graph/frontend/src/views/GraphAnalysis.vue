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
        {{ capabilities.gds_available ? `GDS ${capabilities.gds_version}` : 'GDS 不可用（仅 Cypher 算法）' }}
      </el-tag>
    </div>

    <div v-if="!selectedGraphId" class="empty-hint">
      <el-empty description="请先选择知识图谱" />
    </div>

    <div v-else class="main-area">
      <!-- 左侧：算法配置 -->
      <div class="config-panel">
        <div class="section-title">选择算法</div>

        <!-- Cypher 算法组 -->
        <div class="algo-group">
          <div class="algo-group-label">基础算法</div>
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
        </div>

        <!-- GDS 算法组 -->
        <div class="algo-group">
          <div class="algo-group-label">
            GDS 算法
            <el-tag v-if="!capabilities?.gds_available" type="info" size="small" style="margin-left:4px">需插件</el-tag>
          </div>
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
        </div>

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
                filterable
                remote
                clearable
                placeholder="起点：输入名称搜索"
                :remote-method="(q) => fetchNodes(q, `pair_src_${idx}`)"
                :loading="nodeSearch[`pair_src_${idx}`]?.loading"
                style="width:100%; margin-bottom:4px"
              >
                <el-option
                  v-for="n in nodeSearch[`pair_src_${idx}`]?.options || []"
                  :key="n.id"
                  :label="n.display_name"
                  :value="n.id"
                >
                  <span>{{ n.display_name }}</span>
                  <span class="node-type-tag">{{ n.entity_type || n.labels?.[0] }}</span>
                </el-option>
              </el-select>
              <el-select
                v-model="pair.tgt"
                filterable
                remote
                clearable
                placeholder="终点：输入名称搜索"
                :remote-method="(q) => fetchNodes(q, `pair_tgt_${idx}`)"
                :loading="nodeSearch[`pair_tgt_${idx}`]?.loading"
                style="width:100%"
              >
                <el-option
                  v-for="n in nodeSearch[`pair_tgt_${idx}`]?.options || []"
                  :key="n.id"
                  :label="n.display_name"
                  :value="n.id"
                >
                  <span>{{ n.display_name }}</span>
                  <span class="node-type-tag">{{ n.entity_type || n.labels?.[0] }}</span>
                </el-option>
              </el-select>
              <el-button
                v-if="params.pairs.length > 1"
                link type="danger" size="small"
                style="margin-top:4px"
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
                <el-option v-for="r in schema.rel_types" :key="r" :label="r" :value="r" />
              </el-select>
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
            <el-table-column label="得分" width="120" align="right">
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
            <el-empty description="图谱中暂无数据" />
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { knowledgeGraphAPI } from '../api/ontology'
import { analysisAPI } from '../api/analysis'
import { browseAPI } from '../api/browse'
import GraphCanvas from '../components/GraphCanvas.vue'

const graphs = ref([])
const selectedGraphId = ref(null)
const capabilities = ref(null)
const schema = ref({ labels: [], rel_types: [] })
const selectedAlgo = ref('')
const running = ref(false)
const result = ref(null)
const canvasRef = ref(null)

// 节点搜索状态（每个选择器独立）
const nodeSearch = reactive({
  khop: { loading: false, options: [] },
})

const params = reactive({
  limit: 50,
  nodeLabels: [],
  relTypes: [],
  nodeId: '',      // K跳邻居：选中的节点 elementId
  hops: 2,
  pairs: [{ src: '', tgt: '' }],  // 多路最短路径：节点对
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

// 通用节点搜索（key 用于区分不同选择框）
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

const addPair = () => {
  params.pairs.push({ src: '', tgt: '' })
}

const removePair = (idx) => {
  params.pairs.splice(idx, 1)
}

const load = async () => {
  try {
    graphs.value = await knowledgeGraphAPI.list() || []
  } catch {
    ElMessage.error('加载图谱列表失败')
  }
}

const onGraphChange = async (id) => {
  capabilities.value = null
  schema.value = { labels: [], rel_types: [] }
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
    if (!params.nodeId) {
      ElMessage.warning('请搜索并选择起始节点')
      return
    }
    req.params = { node_id: params.nodeId, hops: params.hops }
  } else if (selectedAlgo.value === 'multi_path') {
    const validPairs = params.pairs.filter(p => p.src && p.tgt)
    if (validPairs.length === 0) {
      ElMessage.warning('请至少选择一对起点和终点节点')
      return
    }
    req.params = { pairs: validPairs.map(p => [p.src, p.tgt]) }
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
  result.value = null
  // 重置搜索状态
  nodeSearch.khop = { loading: false, options: [] }
})

const formatScore = (row) => {
  const algo = result.value?.algorithm
  if (algo === 'louvain' || algo === 'wcc') return `#${row.community_id}`
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

.algo-group {
  margin-bottom: 12px;
}

.algo-group-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
  display: flex;
  align-items: center;
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
