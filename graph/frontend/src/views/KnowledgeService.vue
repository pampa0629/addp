<template>
  <div class="ks-container">
    <!-- 左侧：图谱选择 -->
    <div class="ks-sidebar">
      <div class="ks-sidebar-title">{{ t('graph.service.knowledgeGraph') }}</div>
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
          {{ selectedGraph.is_public ? t('graph.service.public') : t('graph.service.private') }}
        </el-tag>
      </div>

      <el-tabs v-model="activeTab" class="ks-tabs">
        <el-tab-pane :label="t('graph.service.configTab')" name="config">
          <div class="config-panel">
            <el-form label-width="120px">
              <el-form-item :label="t('graph.service.publicAccess')">
                <el-switch
                  v-model="isPublic"
                  :loading="saving"
                  @change="handlePublicToggle"
                />
                <span class="config-hint">{{ t('graph.service.publicAccessHint') }}</span>
              </el-form-item>
              <el-form-item :label="t('graph.service.serviceBaseUrl')">
                <el-input :model-value="serviceBaseUrl" readonly class="url-input">
                  <template #append>
                    <el-button @click="copyUrl">{{ t('graph.service.copy') }}</el-button>
                  </template>
                </el-input>
              </el-form-item>
            </el-form>
          </div>
        </el-tab-pane>

        <!-- Tab 2: API 文档 -->
        <el-tab-pane :label="t('graph.service.apiDocsTab')" name="docs">
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
                <el-text size="small" type="info">{{ t('graph.service.example') }}</el-text>
                <pre class="curl-example">{{ buildCurlExample(ep) }}</pre>
              </div>
            </div>
          </div>
        </el-tab-pane>

        <!-- Tab 3: 接口测试 -->
        <el-tab-pane :label="t('graph.service.testTab')" name="test">
          <div class="test-panel">
            <el-form label-width="80px" class="test-form">
              <el-form-item :label="t('graph.service.keyword')">
                <el-input
                  v-model="testQuery"
                  :placeholder="t('graph.service.keywordPlaceholder')"
                  @keyup.enter="handleTest"
                />
              </el-form-item>
              <el-form-item :label="t('graph.service.entityTypeFilter')">
                <el-select
                  v-model="testEntityType"
                  :placeholder="t('graph.service.allTypes')"
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
                <el-button type="primary" @click="handleTest" :loading="testing">{{ t('graph.service.search') }}</el-button>
              </el-form-item>
            </el-form>
            <pre v-if="testResult" class="test-result">{{ testResult }}</pre>
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>

    <!-- 未选择图谱 -->
    <div class="ks-empty" v-else>
      <el-empty :description="t('graph.service.selectGraph')" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { knowledgeServiceApi } from '../api/knowledgeService'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

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
      desc: t('graph.service.endpointSearch'),
      params: [
        { name: 'q', type: 'string', desc: t('graph.service.paramQ') },
        { name: 'type', type: 'string', desc: t('graph.service.paramType') },
        { name: 'page', type: 'int', desc: t('graph.service.paramPage') },
        { name: 'page_size', type: 'int', desc: t('graph.service.paramPageSize') },
      ],
    },
    {
      method: 'GET',
      path: `${base}/:type`,
      desc: t('graph.service.endpointListByType'),
      params: [
        { name: ':type', type: 'string', desc: t('graph.service.paramTypePath') },
        { name: 'page', type: 'int', desc: t('graph.service.paramPage') },
        { name: 'page_size', type: 'int', desc: t('graph.service.paramPageSize') },
      ],
    },
    {
      method: 'GET',
      path: `${base}/:type/:nodeId`,
      desc: t('graph.service.endpointEntityDetail'),
      params: [
        { name: ':type', type: 'string', desc: t('graph.service.paramTypeEntity') },
        { name: ':nodeId', type: 'string', desc: t('graph.service.paramNodeId') },
      ],
    },
    {
      method: 'GET',
      path: `/nodes/:nodeId/neighbors`,
      desc: t('graph.service.endpointNeighbors'),
      params: [
        { name: ':nodeId', type: 'string', desc: t('graph.service.paramNodeId') },
        { name: 'limit', type: 'int', desc: t('graph.service.paramLimit') },
      ],
    },
    {
      method: 'POST',
      path: `/subgraph`,
      desc: t('graph.service.endpointSubgraph'),
      params: [
        { name: 'node_id', type: 'string', desc: t('graph.service.paramNodeIdRequired') },
        { name: 'depth', type: 'int', desc: t('graph.service.paramDepth') },
        { name: 'limit', type: 'int', desc: t('graph.service.paramLimitDefault50') },
      ],
    },
    {
      method: 'POST',
      path: `/paths`,
      desc: t('graph.service.endpointPaths'),
      params: [
        { name: 'source_node_id', type: 'string', desc: t('graph.service.paramSourceNodeId') },
        { name: 'target_node_id', type: 'string', desc: t('graph.service.paramTargetNodeId') },
      ],
    },
    {
      method: 'GET',
      path: `/ontology`,
      desc: t('graph.service.endpointOntology'),
      params: [],
    },
    {
      method: 'GET',
      path: `/stats`,
      desc: t('graph.service.endpointStats'),
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
    ElMessage.success(val ? t('graph.service.setPublic') : t('graph.service.setPrivate'))
  } catch (e) {
    isPublic.value = !val
    ElMessage.error(t('graph.service.setFailed'))
  } finally {
    saving.value = false
  }
}

function copyUrl() {
  navigator.clipboard.writeText(serviceBaseUrl.value)
    .then(() => ElMessage.success(t('graph.service.copied')))
    .catch(() => ElMessage.error(t('graph.service.copyFailed')))
}

async function handleTest() {
  if (!testQuery.value.trim()) {
    ElMessage.warning(t('graph.service.keywordRequired'))
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
    testResult.value = `${t('graph.service.error')}: ${e.message || e}`
  } finally {
    testing.value = false
  }
}

onMounted(loadGraphs)
</script>

<style scoped>
.ks-container {
  display: flex;
  background: var(--addp-bg-secondary);
}

.ks-sidebar {
  width: 220px;
  flex-shrink: 0;
  background: var(--addp-bg-primary) !important;
  border-right: 1px solid var(--addp-border-color);
  overflow: visible;
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
  overflow: visible;
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
