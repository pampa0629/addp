<template>
  <div class="lineage-view">
    <el-card class="header-card">
      <h2>数据血缘关系可视化</h2>
      <el-row :gutter="20">
        <el-col :span="8">
          <el-input
            v-model="searchItemId"
            placeholder="输入节点ID查询血缘"
            clearable
          >
            <template #prepend>节点ID</template>
          </el-input>
        </el-col>
        <el-col :span="8">
          <el-input-number
            v-model="maxDepth"
            :min="1"
            :max="10"
            placeholder="最大深度"
          />
          <span style="margin-left: 10px">最大深度</span>
        </el-col>
        <el-col :span="8">
          <el-button type="primary" @click="fetchLineageGraph" :loading="loading">
            查询血缘图谱
          </el-button>
          <el-button @click="fetchAllLineage" :loading="loading">
            查看全部
          </el-button>
        </el-col>
      </el-row>
    </el-card>

    <el-row :gutter="20" style="margin-top: 20px; height: calc(100vh - 200px)">
      <!-- 左侧：图谱可视化 -->
      <el-col :span="18">
        <el-card class="graph-card" v-loading="loading">
          <template #header>
            <div class="card-header">
              <span>血缘图谱</span>
              <el-tag v-if="graphData.nodes" type="info">
                {{ graphData.nodes.length }} 个节点, {{ graphData.edges.length }} 条边
              </el-tag>
            </div>
          </template>
          <LineageGraph
            v-if="graphData.nodes && graphData.nodes.length > 0"
            :data="graphData"
            @node-click="handleNodeClick"
          />
          <el-empty
            v-else
            description="暂无数据，请查询血缘关系"
            :image-size="200"
          />
        </el-card>
      </el-col>

      <!-- 右侧：节点详情和列表 -->
      <el-col :span="6">
        <el-card class="detail-card">
          <template #header>
            <span>节点详情</span>
          </template>
          <div v-if="selectedNode">
            <el-descriptions :column="1" border>
              <el-descriptions-item label="节点ID">
                {{ selectedNode.id }}
              </el-descriptions-item>
              <el-descriptions-item label="名称">
                {{ selectedNode.label }}
              </el-descriptions-item>
              <el-descriptions-item label="描述">
                {{ selectedNode.description || '-' }}
              </el-descriptions-item>
            </el-descriptions>
            <div style="margin-top: 16px">
              <el-button
                type="primary"
                size="small"
                @click="fetchUpstream"
                style="width: 100%; margin-bottom: 8px"
              >
                查看上游
              </el-button>
              <el-button
                type="success"
                size="small"
                @click="fetchDownstream"
                style="width: 100%"
              >
                查看下游
              </el-button>
            </div>
          </div>
          <el-empty v-else description="点击节点查看详情" :image-size="100" />
        </el-card>

        <!-- 节点列表 -->
        <el-card class="list-card" style="margin-top: 16px">
          <template #header>
            <span>节点列表</span>
          </template>
          <el-scrollbar height="300px">
            <div
              v-for="node in graphData.nodes"
              :key="node.id"
              class="node-item"
              :class="{ active: selectedNode && selectedNode.id === node.id }"
              @click="selectNodeFromList(node)"
            >
              <div class="node-name">{{ node.label }}</div>
              <div class="node-id">{{ node.id }}</div>
            </div>
          </el-scrollbar>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { ElMessage } from 'element-plus'
import LineageGraph from '../components/LineageGraph.vue'
import {
  getAllLineage,
  getUpstream,
  getDownstream,
  getLineageGraph
} from '../api/lineage'

const loading = ref(false)
const searchItemId = ref('')
const maxDepth = ref(3)
const selectedNode = ref(null)

const graphData = reactive({
  nodes: [],
  edges: []
})

// 获取所有血缘关系
const fetchAllLineage = async () => {
  loading.value = true
  try {
    const result = await getAllLineage()
    // 将血缘关系数组转换为图谱格式
    const nodeMap = new Map()
    const edges = []

    result.forEach(rel => {
      // 添加源节点
      if (!nodeMap.has(rel.source_item_id)) {
        nodeMap.set(rel.source_item_id, {
          id: String(rel.source_item_id),
          name: rel.source_path.split('.').pop() || `节点${rel.source_item_id}`,
          type: rel.source_type
        })
      }

      // 添加目标节点
      if (!nodeMap.has(rel.target_item_id)) {
        nodeMap.set(rel.target_item_id, {
          id: String(rel.target_item_id),
          name: rel.target_path.split('.').pop() || `节点${rel.target_item_id}`,
          type: rel.target_type
        })
      }

      // 添加边
      edges.push({
        source: String(rel.source_item_id),
        target: String(rel.target_item_id),
        transform_type: rel.lineage_type
      })
    })

    graphData.nodes = Array.from(nodeMap.values())
    graphData.edges = edges
    ElMessage.success(`加载成功：${graphData.nodes.length} 个节点, ${graphData.edges.length} 条边`)
  } catch (error) {
    ElMessage.error('加载失败: ' + error.message)
  } finally {
    loading.value = false
  }
}

// 查询血缘图谱
const fetchLineageGraph = async () => {
  if (!searchItemId.value) {
    ElMessage.warning('请输入节点ID')
    return
  }

  loading.value = true
  try {
    const result = await getLineageGraph(searchItemId.value, maxDepth.value)
    graphData.nodes = result.nodes || []
    graphData.edges = result.edges || []
    ElMessage.success('查询成功')
  } catch (error) {
    ElMessage.error('查询失败: ' + error.message)
  } finally {
    loading.value = false
  }
}

// 查看上游
const fetchUpstream = async () => {
  if (!selectedNode.value) return

  loading.value = true
  try {
    const result = await getUpstream(selectedNode.value.id)
    // 转换数据格式
    const nodeMap = new Map()
    const edges = []

    result.forEach(rel => {
      if (!nodeMap.has(rel.source_item_id)) {
        nodeMap.set(rel.source_item_id, {
          id: String(rel.source_item_id),
          name: rel.source_path.split('.').pop() || `节点${rel.source_item_id}`,
          type: rel.source_type
        })
      }
      if (!nodeMap.has(rel.target_item_id)) {
        nodeMap.set(rel.target_item_id, {
          id: String(rel.target_item_id),
          name: rel.target_path.split('.').pop() || `节点${rel.target_item_id}`,
          type: rel.target_type
        })
      }
      edges.push({
        source: String(rel.source_item_id),
        target: String(rel.target_item_id),
        transform_type: rel.lineage_type
      })
    })

    graphData.nodes = Array.from(nodeMap.values())
    graphData.edges = edges
    ElMessage.success('已显示上游节点')
  } catch (error) {
    ElMessage.error('查询失败: ' + error.message)
  } finally {
    loading.value = false
  }
}

// 查看下游
const fetchDownstream = async () => {
  if (!selectedNode.value) return

  loading.value = true
  try {
    const result = await getDownstream(selectedNode.value.id)
    // 转换数据格式
    const nodeMap = new Map()
    const edges = []

    result.forEach(rel => {
      if (!nodeMap.has(rel.source_item_id)) {
        nodeMap.set(rel.source_item_id, {
          id: String(rel.source_item_id),
          name: rel.source_path.split('.').pop() || `节点${rel.source_item_id}`,
          type: rel.source_type
        })
      }
      if (!nodeMap.has(rel.target_item_id)) {
        nodeMap.set(rel.target_item_id, {
          id: String(rel.target_item_id),
          name: rel.target_path.split('.').pop() || `节点${rel.target_item_id}`,
          type: rel.target_type
        })
      }
      edges.push({
        source: String(rel.source_item_id),
        target: String(rel.target_item_id),
        transform_type: rel.lineage_type
      })
    })

    graphData.nodes = Array.from(nodeMap.values())
    graphData.edges = edges
    ElMessage.success('已显示下游节点')
  } catch (error) {
    ElMessage.error('查询失败: ' + error.message)
  } finally {
    loading.value = false
  }
}

// 节点点击处理
const handleNodeClick = (node) => {
  selectedNode.value = node
}

// 从列表选择节点
const selectNodeFromList = (node) => {
  selectedNode.value = node
}
</script>

<style scoped>
.lineage-view {
  padding: 20px;
  background: #f0f2f5;
  min-height: 100vh;
}

.header-card {
  margin-bottom: 20px;
}

.header-card h2 {
  margin: 0 0 20px 0;
  color: #333;
}

.graph-card, .detail-card, .list-card {
  height: 100%;
}

.graph-card :deep(.el-card__body) {
  height: calc(100vh - 280px);
  padding: 0;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.node-item {
  padding: 12px;
  margin-bottom: 8px;
  background: #f5f5f5;
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.3s;
}

.node-item:hover {
  background: #e6f7ff;
  transform: translateX(4px);
}

.node-item.active {
  background: #bae7ff;
  border-left: 3px solid #1890ff;
}

.node-name {
  font-weight: bold;
  color: #333;
  margin-bottom: 4px;
}

.node-id {
  font-size: 12px;
  color: #999;
}
</style>
