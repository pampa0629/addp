<template>
  <div class="graph-service-detail" v-loading="loading">
    <div class="page-header">
      <el-button @click="goBack" :icon="ArrowLeft" circle />
      <h2>{{ service?.title || t('service.graph.detailTitle') }}</h2>
      <div class="header-actions">
        <el-tag :type="statusType(service?.status)" size="default">{{ statusText(service?.status) }}</el-tag>
        <el-button @click="goToEdit">{{ t('service.common.edit') }}</el-button>
      </div>
    </div>

    <div v-if="service" class="detail-content">
      <!-- 基本信息 -->
      <el-card class="info-card">
        <template #header><span>{{ t('service.graph.basicInfoTitle') }}</span></template>
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('service.graph.serviceNameLabel')">{{ service.service_name }}</el-descriptions-item>
          <el-descriptions-item :label="t('service.graph.titleLabel')">{{ service.title }}</el-descriptions-item>
          <el-descriptions-item :label="t('service.graph.colConfigType')">
            <el-tag :type="service.config_type === 'shape' ? 'success' : 'warning'">
              {{ service.config_type === 'shape' ? t('service.graph.shapeMode') : t('service.graph.cypherMode') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('service.graph.colAccess')">
            <el-tag :type="service.public_access ? '' : 'info'">
              {{ service.public_access ? t('service.graph.publicAccess') : t('service.graph.authRequired') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('service.graph.maxRecordsLabel')">{{ service.max_records }}</el-descriptions-item>
          <el-descriptions-item :label="t('service.graph.databaseNameLabel')">{{ service.database_name }}</el-descriptions-item>
          <el-descriptions-item v-if="service.description" :label="t('service.graph.descriptionLabel')" :span="2">{{ service.description }}</el-descriptions-item>
          <el-descriptions-item v-if="service.keywords?.length" :label="t('service.graph.keywordsLabel')" :span="2">
            <el-tag v-for="k in service.keywords" :key="k" size="small" style="margin-right: 4px">{{ k }}</el-tag>
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- 配置详情 -->
      <el-card class="info-card">
        <template #header>
          <span>{{ service.config_type === 'shape' ? t('service.graph.nodeConfigTitle') : t('service.graph.cypherConfigTitle') }}</span>
        </template>
        <el-descriptions v-if="service.config_type === 'shape'" :column="1" border>
          <el-descriptions-item :label="t('service.graph.nodeShapeLabel')">
            <code>{{ service.node_shape }}</code>
          </el-descriptions-item>
          <el-descriptions-item v-if="service.node_labels?.length" :label="t('service.graph.nodeLabelsLabel')">
            {{ service.node_labels.join(', ') }}
          </el-descriptions-item>
          <el-descriptions-item v-if="service.data_config?.properties?.length" :label="t('service.graph.returnPropertiesLabel')">
            {{ service.data_config.properties.join(', ') }}
          </el-descriptions-item>
          <el-descriptions-item v-if="service.data_config?.filterable_properties?.length" :label="t('service.graph.filterablePropertiesLabel')">
            {{ service.data_config.filterable_properties.join(', ') }}
          </el-descriptions-item>
        </el-descriptions>
        <div v-else>
          <div class="cypher-block">{{ service.cypher_query }}</div>
          <el-descriptions :column="2" border style="margin-top:12px">
            <el-descriptions-item :label="t('service.graph.resultTypeLabel')">
              {{ { table: t('service.graph.resultTypeTable'), graph: t('service.graph.resultTypeGraph'), both: t('service.graph.resultTypeBothShort') }[service.data_config?.result_type] || t('service.graph.resultTypeTable') }}
            </el-descriptions-item>
          </el-descriptions>
          <div v-if="service.parameters?.length" style="margin-top:12px">
            <strong>{{ t('service.graph.paramDefsLabel') }}</strong>
            <el-table :data="service.parameters" size="small" style="margin-top:8px">
              <el-table-column prop="name" :label="t('service.graph.paramName')" width="150" />
              <el-table-column prop="type" :label="t('service.graph.paramType')" width="100" />
              <el-table-column prop="required" :label="t('service.graph.paramRequired')" width="80">
                <template #default="{ row }"><el-tag :type="row.required ? 'danger' : 'info'" size="small">{{ row.required ? t('service.common.yes') : t('service.common.no') }}</el-tag></template>
              </el-table-column>
              <el-table-column prop="description" :label="t('service.graph.paramDescription')" />
            </el-table>
          </div>
        </div>
      </el-card>

      <!-- 访问端点 -->
      <el-card class="info-card">
        <template #header><span>{{ t('service.graph.endpointsTitle') }}</span></template>
        <el-descriptions :column="1" border>
          <el-descriptions-item :label="t('service.graph.executeQueryLabel')">
            <code class="endpoint">POST {{ service.endpoints?.execute }}</code>
            <el-button link size="small" @click="copyText(service.endpoints?.execute)" style="margin-left:8px">{{ t('service.common.copy') }}</el-button>
          </el-descriptions-item>
        </el-descriptions>
        <div class="endpoint-help">
          <strong>{{ t('service.graph.callExampleLabel') }}</strong>
          <pre class="code-block">{{ exampleRequest }}</pre>
        </div>
      </el-card>

      <!-- 在线测试 -->
      <el-card class="info-card">
        <template #header><span>{{ t('service.graph.onlineTestTitle') }}</span></template>

        <!-- 参数输入 -->
        <div v-if="service.config_type === 'cypher' && service.parameters?.length">
          <el-form label-width="120px" style="margin-bottom:16px">
            <el-form-item v-for="p in service.parameters" :key="p.name" :label="p.name">
              <el-input v-model="testParams[p.name]" :placeholder="p.description || p.type" style="width:300px" />
              <el-tag v-if="p.required" type="danger" size="small" style="margin-left:6px">{{ t('service.graph.required') }}</el-tag>
            </el-form-item>
          </el-form>
        </div>
        <div v-if="service.config_type === 'shape' && service.data_config?.filterable_properties?.length">
          <el-form label-width="120px" style="margin-bottom:16px">
            <el-form-item v-for="prop in service.data_config.filterable_properties" :key="prop" :label="prop">
              <el-input v-model="testParams[prop]" :placeholder="t('service.graph.filterPlaceholder', { prop })" style="width:300px" />
            </el-form-item>
          </el-form>
        </div>

        <el-form label-width="120px" style="margin-bottom:16px">
          <el-form-item :label="t('service.graph.pageLabel')">
            <el-input-number v-model="testPage" :min="1" />
          </el-form-item>
          <el-form-item :label="t('service.graph.pageSizeLabel')">
            <el-input-number v-model="testPageSize" :min="1" :max="service.max_records" />
          </el-form-item>
        </el-form>

        <el-button type="primary" :loading="testing" @click="runTest">{{ t('service.graph.runQueryBtn') }}</el-button>
        <el-button @click="clearTest">{{ t('service.graph.clearResultBtn') }}</el-button>

        <!-- 测试结果 -->
        <div v-if="testResult" class="test-result">
          <div class="result-meta">
            <span>{{ t('service.graph.returnedCount', { count: testResult.rows_count }) }}</span>
            <span v-if="testResult.total_count != null">{{ t('service.graph.totalCount', { total: testResult.total_count }) }}</span>
          </div>

          <!-- 表格结果 -->
          <div v-if="testResult.columns?.length" style="margin-top:12px">
            <el-table :data="testResult.rows" size="small" max-height="400" border>
              <el-table-column v-for="col in testResult.columns" :key="col" :label="col" min-width="120">
                <template #default="{ row }">
                  <span v-if="typeof row[col] === 'object' && row[col] !== null">
                    <el-popover placement="bottom" :width="400" trigger="click">
                      <template #reference>
                        <el-button link size="small" type="primary">{{ t('service.graph.viewObject') }}</el-button>
                      </template>
                      <pre class="json-block">{{ JSON.stringify(row[col], null, 2) }}</pre>
                    </el-popover>
                  </span>
                  <span v-else>{{ row[col] ?? '' }}</span>
                </template>
              </el-table-column>
            </el-table>
          </div>

          <!-- 图结构结果 -->
          <div v-if="testResult.graph_data" style="margin-top:12px">
            <el-alert type="info" :closable="false">
              {{ t('service.graph.graphDataSummary', { nodes: testResult.graph_data.nodes?.length || 0, rels: testResult.graph_data.relationships?.length || 0 }) }}
            </el-alert>
            <el-collapse style="margin-top:8px">
              <el-collapse-item :title="t('service.graph.viewRawGraphData')">
                <pre class="json-block">{{ JSON.stringify(testResult.graph_data, null, 2) }}</pre>
              </el-collapse-item>
            </el-collapse>
          </div>
        </div>

        <div v-if="testError" class="test-error">
          <el-alert type="error" :title="testError" :closable="false" />
        </div>
      </el-card>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import graphApi from '../api/graphQueryService'
import { navigateServiceRoute } from '@/utils/moduleNavigation'
import { useConsolePageDescriptor } from '@common-ui'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const id = route.params.id
const goBack = () => navigateServiceRoute(router, '/graph-services', { history: 'replace' })
const goToEdit = () => navigateServiceRoute(router, `/graph-services/${id}/edit`)

const loading = ref(false)
const service = ref(null)
useConsolePageDescriptor(router, 'service', {
  title: computed(() => t('service.graph.recentVisitTitle')),
  subject: computed(() => service.value?.title || service.value?.name || ''),
  ready: computed(() => Boolean(service.value?.title || service.value?.name))
})
const testing = ref(false)
const testParams = ref({})
const testPage = ref(1)
const testPageSize = ref(20)
const testResult = ref(null)
const testError = ref('')

const statusType = (s) => ({ active: 'success', inactive: 'info', error: 'danger' }[s] || '')
const statusText = (s) => ({ active: t('service.graph.statusRunning'), inactive: t('service.graph.statusInactive'), error: t('service.graph.statusError') }[s] || s)

const exampleRequest = computed(() => {
  if (!service.value) return ''
  const url = service.value.endpoints?.execute || ''
  const body = { parameters: {}, page: 1, page_size: 20 }
  if (service.value.parameters?.length) {
    service.value.parameters.forEach(p => { body.parameters[p.name] = `<${p.type}>` })
  }
  return `curl -X POST "${url}" \\\n  -H "Content-Type: application/json" \\\n  -H "Authorization: Bearer <token>" \\\n  -d '${JSON.stringify(body, null, 2)}'`
})

const runTest = async () => {
  testing.value = true
  testResult.value = null
  testError.value = ''
  try {
    const result = await graphApi.executeQuery(service.value.service_name, {
      parameters: { ...testParams.value },
      page: testPage.value,
      page_size: testPageSize.value
    })
    testResult.value = result
  } catch (e) {
    testError.value = e.response?.data?.error || e.message
  } finally {
    testing.value = false
  }
}

const clearTest = () => {
  testResult.value = null
  testError.value = ''
  testParams.value = {}
}

const copyText = (text) => {
  navigator.clipboard?.writeText(text).then(() => ElMessage.success(t('service.common.copied')))
}

onMounted(async () => {
  loading.value = true
  try {
    service.value = await graphApi.getService(id)
  } catch (e) {
    ElMessage.error(t('service.graph.loadFailed') + '：' + (e.response?.data?.error || e.message))
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.graph-service-detail {
  padding: 24px;
  background: var(--addp-bg-secondary);
}
.page-header { display: flex; align-items: center; gap: 12px; margin-bottom: 24px; }
.header-actions { margin-left: auto; display: flex; align-items: center; gap: 10px; }
.detail-content { display: flex; flex-direction: column; gap: 16px; }
.info-card { }
.cypher-block {
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  border: 1px solid var(--addp-border-color);
  padding: 12px 16px;
  border-radius: 6px;
  font-family: monospace;
  font-size: 13px;
  white-space: pre-wrap;
  word-break: break-word;
}
code {
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  padding: 2px 6px;
  border-radius: 3px;
  font-family: monospace;
  font-size: 13px;
}
.endpoint { font-size: 13px; }
.endpoint-help { margin-top: 16px; }
.code-block {
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  border: 1px solid var(--addp-border-color);
  padding: 12px 16px;
  border-radius: 6px;
  font-family: monospace;
  font-size: 12px;
  white-space: pre;
  overflow-x: auto;
  margin-top: 8px;
}
.test-result { margin-top: 16px; }
.result-meta { font-size: 14px; color: var(--addp-text-secondary); }
.json-block {
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  border: 1px solid var(--addp-border-color);
  padding: 12px;
  border-radius: 6px;
  font-family: monospace;
  font-size: 12px;
  overflow: auto;
  max-height: 400px;
  margin: 0;
}
.test-error { margin-top: 16px; }
</style>
