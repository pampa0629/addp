<template>
  <div class="ks-container">
    <!-- 左侧：图谱选择 -->
    <div class="ks-sidebar">
      <div class="ks-sidebar-title">知识图谱</div>
      <el-menu
        :default-active="selectedGraphId?.toString()"
        class="ks-menu"
        @select="handleGraphSelect"
      >
        <el-menu-item
          v-for="g in graphs"
          :key="g.id"
          :index="g.id.toString()"
        >
          {{ g.name }}
        </el-menu-item>
      </el-menu>
    </div>

    <!-- 右侧：配置面板 -->
    <div class="ks-main" v-if="selectedGraph">
      <div class="ks-header">
        <h2 class="ks-title">{{ selectedGraph.name }}</h2>
        <el-tag :type="selectedGraph.is_public ? 'success' : 'info'">
          {{ selectedGraph.is_public ? '公开' : '私有' }}
        </el-tag>
      </div>

      <el-tabs v-model="activeTab" class="ks-tabs">
        <!-- Tab 1: 服务配置 -->
        <el-tab-pane label="服务配置" name="config">
          <div class="config-panel">
            <el-form label-width="120px">
              <el-form-item label="公开访问">
                <el-switch
                  v-model="isPublic"
                  :loading="saving"
                  @change="handlePublicToggle"
                />
                <span class="config-hint">开启后，无需 JWT 即可访问该图谱的知识服务 API</span>
              </el-form-item>
              <el-form-item label="服务基础 URL">
                <el-input :model-value="serviceBaseUrl" readonly class="url-input">
                  <template #append>
                    <el-button @click="copyUrl">复制</el-button>
                  </template>
                </el-input>
              </el-form-item>
            </el-form>
          </div>
        </el-tab-pane>

        <!-- Tab 2: API 文档 -->
        <el-tab-pane label="API 文档" name="docs">
          <div class="api-docs">
            <div v-for="ep in endpoints" :key="ep.path" class="endpoint-card">
              <div class="endpoint-header">
                <el-tag :type="ep.method === 'GET' ? 'success' : 'primary'" size="small">
                  {{ ep.method }}
                </el-tag>
                <code class="endpoint-path">{{ serviceBaseUrl }}{{ ep.path }}</code>
              </div>
              <p class="endpoint-desc">{{ ep.desc }}</p>
              <div v-if="ep.params?.length" class="endpoint-params">
                <div v-for="p in ep.params" :key="p.name" class="param-row">
                  <code class="param-name">{{ p.name }}</code>
                  <span class="param-type">{{ p.type }}</span>
                  <span class="param-desc">{{ p.desc }}</span>
                </div>
              </div>
              <div class="endpoint-curl">
                <el-text size="small" type="info">示例</el-text>
                <pre class="curl-example">{{ buildCurlExample(ep) }}</pre>
              </div>
            </div>
          </div>
        </el-tab-pane>

        <!-- Tab 3: 接口测试 -->
        <el-tab-pane label="接口测试" name="test">
          <div class="test-panel">
            <el-form label-width="80px" class="test-form">
              <el-form-item label="关键词">
                <el-input
                  v-model="testQuery"
                  placeholder="输入搜索关键词"
                  @keyup.enter="handleTest"
                />
              </el-form-item>
              <el-form-item label="实体类型">
                <el-select
                  v-model="testEntityType"
                  placeholder="全部类型（可选）"
                  clearable
                >
                  <el-option
                    v-for="et in entityTypes"
                    :key="et.name"
                    :label="`${et.label || et.name} (${et.count})`"
                    :value="et.name"
                  />
                </el-select>
              </el-form-item>
              <el-form-item>
                <el-button type="primary" @click="handleTest" :loading="testing">搜索</el-button>
              </el-form-item>
            </el-form>
            <pre v-if="testResult" class="test-result">{{ testResult }}</pre>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>

    <!-- 未选择图谱 -->
    <div class="ks-empty" v-else>
      <el-empty description="请从左侧选择知识图谱" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { knowledgeServiceApi } from '../api/knowledgeService'

const graphs = ref([])
const selectedGraphId = ref(null)
const activeTab = ref('config')
const isPublic = ref(false)
const saving = ref(false)
const testing = ref(false)
const testQuery = ref('')
const testEntityType = ref('')
const testResult = ref('')
const entityTypes = ref([])

const selectedGraph = computed(() =>
  graphs.value.find(g => g.id === selectedGraphId.value) || null
)

const serviceBaseUrl = computed(() => {
  if (!selectedGraphId.value) return ''
  const baseUrl = window.location.origin
  return `${baseUrl}/api/v1/graph/kg/${selectedGraphId.value}`
})

const endpoints = computed(() => {
  const base = `/entities`
  return [
    {
      method: 'GET',
      path: `/search`,
      desc: '全文搜索实体（支持类型过滤和分页）',
      params: [
        { name: 'q', type: 'string', desc: '搜索关键词（必填）' },
        { name: 'type', type: 'string', desc: '实体类型 name（可选）' },
        { name: 'page', type: 'int', desc: '页码，默认 1' },
        { name: 'page_size', type: 'int', desc: '每页大小，默认 20' },
      ],
    },
    {
      method: 'GET',
      path: `${base}/:type`,
      desc: '按本体类型列出实体（分页）',
      params: [
        { name: ':type', type: 'string', desc: '本体实体类型 name（路径参数）' },
        { name: 'page', type: 'int', desc: '页码，默认 1' },
        { name: 'page_size', type: 'int', desc: '每页大小，默认 20' },
      ],
    },
    {
      method: 'GET',
      path: `${base}/:type/:nodeId`,
      desc: '获取实体详情（全属性）',
      params: [
        { name: ':type', type: 'string', desc: '本体实体类型 name' },
        { name: ':nodeId', type: 'string', desc: 'Neo4j elementId' },
      ],
    },
    {
      method: 'GET',
      path: `/nodes/:nodeId/neighbors`,
      desc: '获取节点的所有直接邻居（含关系类型、方向）',
      params: [
        { name: ':nodeId', type: 'string', desc: 'Neo4j elementId' },
        { name: 'limit', type: 'int', desc: '最多返回数量，默认 100' },
      ],
    },
    {
      method: 'POST',
      path: `/subgraph`,
      desc: '获取实体中心子图（N 跳范围内的节点和关系）',
      params: [
        { name: 'node_id', type: 'string', desc: 'Neo4j elementId（必填）' },
        { name: 'depth', type: 'int', desc: '跳数（1-3），默认 2' },
        { name: 'limit', type: 'int', desc: '最多节点数，默认 50' },
      ],
    },
    {
      method: 'POST',
      path: `/paths`,
      desc: '查找两节点间的路径',
      params: [
        { name: 'source_node_id', type: 'string', desc: '起点 elementId（必填）' },
        { name: 'target_node_id', type: 'string', desc: '终点 elementId（必填）' },
      ],
    },
    {
      method: 'GET',
      path: `/ontology`,
      desc: '获取图谱本体描述（实体类型 + 关系类型 + 数量统计）',
      params: [],
    },
    {
      method: 'GET',
      path: `/stats`,
      desc: '获取图谱统计（节点数、关系数、按标签分组）',
      params: [],
    },
  ]
})

function buildCurlExample(ep) {
  const base = serviceBaseUrl.value
  if (ep.method === 'GET') {
    return `curl -H "Authorization: Bearer $TOKEN" \\\n  "${base}${ep.path}"`
  }
  return `curl -X POST -H "Authorization: Bearer $TOKEN" \\\n  -H "Content-Type: application/json" \\\n  "${base}${ep.path}"`
}

async function loadGraphs() {
  try {
    const res = await knowledgeServiceApi.listGraphs()
    graphs.value = Array.isArray(res) ? res : (res.data || [])
  } catch {
    graphs.value = []
  }
}

async function loadOntology() {
  if (!selectedGraphId.value) return
  try {
    const res = await knowledgeServiceApi.getOntology(selectedGraphId.value)
    entityTypes.value = res.entity_types || []
  } catch {
    entityTypes.value = []
  }
}

function handleGraphSelect(id) {
  selectedGraphId.value = parseInt(id)
  const g = graphs.value.find(g => g.id === selectedGraphId.value)
  isPublic.value = g?.is_public ?? false
  testResult.value = ''
  testEntityType.value = ''
  loadOntology()
}

async function handlePublicToggle(val) {
  if (!selectedGraphId.value) return
  saving.value = true
  try {
    await knowledgeServiceApi.updateGraph(selectedGraphId.value, { is_public: val })
    const g = graphs.value.find(g => g.id === selectedGraphId.value)
    if (g) g.is_public = val
    ElMessage.success(val ? '已设为公开访问' : '已设为私有访问')
  } catch (e) {
    isPublic.value = !val
    ElMessage.error('设置失败')
  } finally {
    saving.value = false
  }
}

function copyUrl() {
  navigator.clipboard.writeText(serviceBaseUrl.value)
    .then(() => ElMessage.success('已复制'))
    .catch(() => ElMessage.error('复制失败'))
}

async function handleTest() {
  if (!testQuery.value.trim()) {
    ElMessage.warning('请输入搜索关键词')
    return
  }
  testing.value = true
  testResult.value = ''
  try {
    const res = await knowledgeServiceApi.searchEntities(
      selectedGraphId.value,
      testQuery.value,
      testEntityType.value
    )
    testResult.value = JSON.stringify(res, null, 2)
  } catch (e) {
    testResult.value = `错误: ${e.message || e}`
  } finally {
    testing.value = false
  }
}

onMounted(loadGraphs)
</script>

<style scoped>
.ks-container {
  display: flex;
  height: 100%;
  background: var(--addp-bg-secondary);
}

.ks-sidebar {
  width: 220px;
  flex-shrink: 0;
  background: var(--addp-bg-primary) !important;
  border-right: 1px solid var(--addp-border-color);
  overflow-y: auto;
}

.ks-sidebar-title {
  padding: 16px;
  font-size: 13px;
  font-weight: 600;
  color: var(--addp-text-secondary);
  border-bottom: 1px solid var(--addp-border-color-light);
}

.ks-menu {
  border-right: none;
  background: transparent;
}

.ks-main {
  flex: 1;
  padding: 24px;
  overflow: auto;
  background: var(--addp-bg-primary) !important;
}

.ks-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--addp-bg-primary) !important;
}

.ks-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 20px;
}

.ks-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--addp-text-primary);
  margin: 0;
}

.ks-tabs {
  --el-tabs-header-height: 36px;
}

.config-panel {
  max-width: 600px;
  padding: 24px;
  background: var(--addp-bg-secondary);
  border-radius: 8px;
  border: 1px solid var(--addp-border-color);
}

.config-hint {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-left: 8px;
}

.url-input {
  font-family: monospace;
}

.api-docs {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.endpoint-card {
  padding: 16px;
  background: var(--addp-bg-secondary);
  border-radius: 6px;
  border: 1px solid var(--addp-border-color);
}

.endpoint-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}

.endpoint-path {
  font-size: 13px;
  color: var(--addp-text-primary);
  font-family: monospace;
}

.endpoint-desc {
  font-size: 13px;
  color: var(--addp-text-secondary);
  margin: 4px 0 8px;
}

.endpoint-params {
  margin-bottom: 10px;
}

.param-row {
  display: flex;
  gap: 10px;
  font-size: 12px;
  padding: 3px 0;
  color: var(--addp-text-secondary);
}

.param-name {
  color: var(--addp-color-primary);
  font-family: monospace;
  min-width: 120px;
}

.param-type {
  color: var(--addp-text-tertiary);
  min-width: 60px;
}

.endpoint-curl {
  margin-top: 8px;
}

.curl-example {
  background: var(--addp-bg-tertiary, var(--addp-bg-secondary));
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  padding: 8px 12px;
  font-size: 12px;
  font-family: monospace;
  color: var(--addp-text-primary);
  overflow: auto;
  white-space: pre;
  margin: 4px 0 0;
}

.test-panel {
  max-width: 800px;
}

.test-form {
  margin-bottom: 16px;
}

.test-result {
  background: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  padding: 16px;
  font-size: 12px;
  font-family: monospace;
  color: var(--addp-text-primary);
  overflow: auto;
  max-height: 500px;
  white-space: pre-wrap;
}
</style>
