<template>
  <div class="analysis-panel">
    <!-- 区域一：算法选择 -->
    <div class="panel-section">
      <div class="section-title">算法选择</div>
      <el-select
        v-model="selectedAlgo"
        placeholder="选择算法"
        style="width: 100%"
        @change="onAlgoChange"
      >
        <el-option-group label="Cypher 算法">
          <el-option
            v-for="algo in cypherAlgos"
            :key="algo.value"
            :label="algo.label"
            :value="algo.value"
          />
        </el-option-group>
        <el-option-group label="GDS 算法">
          <el-option
            v-for="algo in gdsAlgos"
            :key="algo.value"
            :label="algo.label"
            :value="algo.value"
            :disabled="!capabilities.gds_available"
          >
            <span>{{ algo.label }}</span>
            <el-tooltip
              v-if="!capabilities.gds_available"
              content="需要 Neo4j GDS 插件"
              placement="right"
            >
              <el-icon style="margin-left: 4px; color: var(--addp-text-tertiary)"><InfoFilled /></el-icon>
            </el-tooltip>
          </el-option>
        </el-option-group>
      </el-select>
      <div v-if="selectedAlgo" class="algo-desc">{{ algoDesc[selectedAlgo] }}</div>
    </div>

    <!-- 区域二：参数配置 -->
    <div class="panel-section" v-if="selectedAlgo">
      <div class="section-title">参数配置</div>

      <!-- degree_centrality -->
      <template v-if="selectedAlgo === 'degree_centrality'">
        <div class="param-item">
          <div class="param-label">节点类型过滤</div>
          <el-select v-model="params.node_labels" multiple collapse-tags style="width: 100%" placeholder="全部类型">
            <el-option v-for="l in schemaLabels" :key="l" :label="l" :value="l" />
          </el-select>
        </div>
        <div class="param-item">
          <div class="param-label">返回数量 Top-N</div>
          <el-input-number v-model="params.limit" :min="5" :max="200" :step="5" style="width: 100%" />
        </div>
      </template>

      <!-- khop_neighbors -->
      <template v-else-if="selectedAlgo === 'khop_neighbors'">
        <div class="param-item">
          <div class="param-label">起始节点 ID</div>
          <el-input v-model="params.node_id" placeholder="从画布点击节点自动填入" />
        </div>
        <div class="param-item">
          <div class="param-label">跳数 (1-4)</div>
          <el-input-number v-model="params.hops" :min="1" :max="4" style="width: 100%" />
        </div>
        <div class="param-item">
          <div class="param-label">返回数量</div>
          <el-input-number v-model="params.limit" :min="10" :max="200" :step="10" style="width: 100%" />
        </div>
      </template>

      <!-- multi_path -->
      <template v-else-if="selectedAlgo === 'multi_path'">
        <div class="param-item">
          <div class="param-label">路径对（最多5对）</div>
          <div v-for="(pair, idx) in params.pairs" :key="idx" class="path-pair">
            <el-input v-model="pair[0]" placeholder="源节点 ID" style="width: calc(50% - 20px)" />
            <span style="padding: 0 4px; color: var(--addp-text-secondary)">→</span>
            <el-input v-model="pair[1]" placeholder="目标节点 ID" style="width: calc(50% - 20px)" />
            <el-button
              v-if="params.pairs.length > 1"
              :icon="Delete"
              circle
              size="small"
              type="danger"
              plain
              @click="params.pairs.splice(idx, 1)"
            />
          </div>
          <el-button
            v-if="params.pairs.length < 5"
            size="small"
            plain
            @click="params.pairs.push(['', ''])"
            style="margin-top: 4px; width: 100%"
          >
            + 添加路径对
          </el-button>
        </div>
      </template>

      <!-- pagerank / betweenness -->
      <template v-else-if="selectedAlgo === 'pagerank' || selectedAlgo === 'betweenness'">
        <div class="param-item">
          <div class="param-label">节点类型过滤</div>
          <el-select v-model="params.node_labels" multiple collapse-tags style="width: 100%" placeholder="全部类型">
            <el-option v-for="l in schemaLabels" :key="l" :label="l" :value="l" />
          </el-select>
        </div>
        <div class="param-item">
          <div class="param-label">关系类型过滤</div>
          <el-select v-model="params.rel_types" multiple collapse-tags style="width: 100%" placeholder="全部类型">
            <el-option v-for="r in schemaRelTypes" :key="r" :label="r" :value="r" />
          </el-select>
        </div>
        <div class="param-item">
          <div class="param-label">返回数量 Top-N</div>
          <el-input-number v-model="params.limit" :min="5" :max="200" :step="5" style="width: 100%" />
        </div>
      </template>

      <!-- louvain / wcc -->
      <template v-else-if="selectedAlgo === 'louvain' || selectedAlgo === 'wcc'">
        <div class="param-item">
          <div class="param-label">节点类型过滤</div>
          <el-select v-model="params.node_labels" multiple collapse-tags style="width: 100%" placeholder="全部类型">
            <el-option v-for="l in schemaLabels" :key="l" :label="l" :value="l" />
          </el-select>
        </div>
        <div class="param-item">
          <div class="param-label">关系类型过滤</div>
          <el-select v-model="params.rel_types" multiple collapse-tags style="width: 100%" placeholder="全部类型">
            <el-option v-for="r in schemaRelTypes" :key="r" :label="r" :value="r" />
          </el-select>
        </div>
        <div class="param-item">
          <div class="param-label">返回数量</div>
          <el-input-number v-model="params.limit" :min="10" :max="200" :step="10" style="width: 100%" />
        </div>
      </template>

      <el-button
        type="primary"
        style="width: 100%; margin-top: 8px"
        :loading="running"
        @click="runAlgorithm"
      >
        执行
      </el-button>
    </div>

    <!-- 区域三：结果 -->
    <div class="panel-section result-section" v-if="result">
      <div class="result-meta">
        <span class="result-algo-name">{{ result.algorithm_name }}</span>
        <span class="result-meta-detail">
          耗时 {{ result.metadata?.elapsed_ms }}ms
          <template v-if="result.metadata?.community_count">
            · {{ result.metadata.community_count }} 个社区
          </template>
          <template v-if="result.node_scores?.length">
            · {{ result.node_scores.length }} 个节点
          </template>
          <template v-if="result.subgraph">
            · {{ result.subgraph.nodes?.length }} 节点 / {{ result.subgraph.edges?.length }} 关系
          </template>
        </span>
      </div>

      <div v-if="result.warning" class="result-warning">{{ result.warning }}</div>

      <!-- 着色操作（仅中心性/社区算法有 node_scores）-->
      <div v-if="result.node_scores?.length" class="color-actions">
        <el-button size="small" type="primary" @click="applyColors">在画布中着色</el-button>
        <el-button size="small" @click="clearColors">清除着色</el-button>
      </div>
      <!-- 路径/邻居算法结果加载到画布 -->
      <div v-else-if="result.subgraph" class="color-actions">
        <el-button size="small" type="primary" @click="$emit('load-subgraph', result.subgraph)">加载到画布</el-button>
      </div>

      <!-- 节点分数列表 -->
      <el-table
        v-if="result.node_scores?.length"
        :data="result.node_scores"
        size="small"
        height="300"
        @row-click="onRowClick"
        style="cursor: pointer"
      >
        <el-table-column prop="rank" label="#" width="36" />
        <el-table-column prop="display_name" label="节点名" min-width="80" show-overflow-tooltip />
        <el-table-column prop="entity_type" label="类型" width="70" show-overflow-tooltip />
        <el-table-column label="分数" width="70">
          <template #default="{ row }">
            <span :title="row.score">{{ formatScore(row.score) }}</span>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Delete, InfoFilled } from '@element-plus/icons-vue'
import { analysisAPI } from '../api/analysis'

const props = defineProps({
  graphId: { type: [Number, String], required: true },
  selectedNodeId: { type: String, default: '' },
  schemaLabels: { type: Array, default: () => [] },
  schemaRelTypes: { type: Array, default: () => [] },
  capabilities: {
    type: Object,
    default: () => ({ gds_available: false, cypher_algos: [], gds_algos: [] })
  }
})

const emit = defineEmits(['apply-scores', 'clear-scores', 'focus-node', 'load-subgraph'])

const cypherAlgos = [
  { value: 'degree_centrality', label: '度中心性' },
  { value: 'khop_neighbors', label: 'K跳邻居' },
  { value: 'multi_path', label: '多路最短路径' }
]
const gdsAlgos = [
  { value: 'pagerank', label: 'PageRank' },
  { value: 'louvain', label: 'Louvain 社区发现' },
  { value: 'wcc', label: '弱连通分量 (WCC)' },
  { value: 'betweenness', label: '介数中心性' }
]
const algoDesc = {
  degree_centrality: '统计每个节点的连接边数，度越高表示节点越重要。',
  khop_neighbors:    '从指定节点出发，找到 K 跳内的全部邻居节点。',
  multi_path:        '查找多对节点之间的所有最短路径并展示。',
  pagerank:          '基于链接结构评估节点重要性，分值越高越重要。',
  louvain:           '将图中节点按模块度划分为不同社区。',
  wcc:               '识别图中彼此不相连的弱连通分量（孤岛）。',
  betweenness:       '统计节点作为最短路径"桥梁"的频次，越高越是关键枢纽。'
}

const selectedAlgo = ref('')
const running = ref(false)
const result = ref(null)

// 算法参数（按算法类型复用）
const params = ref({
  node_labels: [],
  rel_types: [],
  limit: 50,
  node_id: '',
  hops: 2,
  pairs: [['', '']]
})

// 当画布选中节点变化时，自动填入 khop_neighbors 的 node_id
watch(() => props.selectedNodeId, (val) => {
  if (val && selectedAlgo.value === 'khop_neighbors') {
    params.value.node_id = val
  }
})

function onAlgoChange() {
  result.value = null
  params.value = {
    node_labels: [],
    rel_types: [],
    limit: 50,
    node_id: props.selectedNodeId || '',
    hops: 2,
    pairs: [['', '']]
  }
}

async function runAlgorithm() {
  if (!selectedAlgo.value) return
  running.value = true
  try {
    const body = {
      algorithm: selectedAlgo.value,
      node_labels: params.value.node_labels,
      rel_types: params.value.rel_types,
      limit: params.value.limit,
      params: {}
    }
    if (selectedAlgo.value === 'khop_neighbors') {
      body.params = { node_id: params.value.node_id, hops: params.value.hops }
    } else if (selectedAlgo.value === 'multi_path') {
      body.params = { pairs: params.value.pairs.filter(p => p[0] && p[1]) }
    }
    result.value = await analysisAPI.runAlgorithm(props.graphId, body)
  } catch (e) {
    ElMessage.error('算法执行失败: ' + (e.response?.data?.error || e.message))
  } finally {
    running.value = false
  }
}

function applyColors() {
  if (!result.value?.node_scores?.length) return
  const isCommunity = ['louvain', 'wcc'].includes(result.value.algorithm)
  emit('apply-scores', result.value.node_scores, isCommunity ? 'community' : 'gradient', result.value.algorithm_name)
}

function clearColors() {
  emit('clear-scores')
}

function onRowClick(row) {
  if (row.node_id) emit('focus-node', row.node_id)
}

function formatScore(score) {
  if (Number.isInteger(score)) return score
  return Number(score).toFixed(4)
}
</script>

<style scoped>
.analysis-panel {
  height: 100%;
  overflow-y: auto;
  background: var(--addp-bg-primary) !important;
  border-left: 1px solid var(--addp-border-color);
  display: flex;
  flex-direction: column;
  gap: 0;
}

.panel-section {
  padding: 12px;
  border-bottom: 1px solid var(--addp-border-color);
}

.section-title {
  color: var(--addp-text-secondary);
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 8px;
}

.algo-desc {
  font-size: 11px;
  color: var(--addp-text-tertiary);
  margin-top: 6px;
  line-height: 1.5;
}

.param-item {
  margin-bottom: 8px;
}

.param-label {
  font-size: 11px;
  color: var(--addp-text-secondary);
  margin-bottom: 4px;
}

.path-pair {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-bottom: 4px;
}

.result-section {
  flex: 1;
}

.result-meta {
  background: var(--addp-bg-secondary) !important;
  border-radius: 4px;
  padding: 6px 8px;
  margin-bottom: 8px;
}

.result-algo-name {
  font-weight: 600;
  font-size: 13px;
  color: var(--addp-text-primary);
  display: block;
}

.result-meta-detail {
  font-size: 11px;
  color: var(--addp-text-secondary);
}

.result-warning {
  font-size: 11px;
  color: #e6a23c;
  margin-bottom: 6px;
}

.color-actions {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
}
</style>
