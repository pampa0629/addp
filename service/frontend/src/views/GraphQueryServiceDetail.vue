<template>
  <div class="graph-service-detail" v-loading="loading">
    <div class="page-header">
      <el-button @click="$router.back()" :icon="ArrowLeft" circle />
      <h2>{{ service?.title || '图查询服务详情' }}</h2>
      <div class="header-actions">
        <el-tag :type="statusType(service?.status)" size="default">{{ statusText(service?.status) }}</el-tag>
        <el-button @click="$router.push(`/graph-services/${id}/edit`)">编辑</el-button>
      </div>
    </div>

    <div v-if="service" class="detail-content">
      <!-- 基本信息 -->
      <el-card class="info-card">
        <template #header><span>基本信息</span></template>
        <el-descriptions :column="2" border>
          <el-descriptions-item label="服务名称">{{ service.service_name }}</el-descriptions-item>
          <el-descriptions-item label="标题">{{ service.title }}</el-descriptions-item>
          <el-descriptions-item label="配置类型">
            <el-tag :type="service.config_type === 'label' ? 'success' : 'warning'">
              {{ service.config_type === 'label' ? '标签模式' : 'Cypher 模式' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="访问控制">
            <el-tag :type="service.public_access ? '' : 'info'">
              {{ service.public_access ? '公开访问' : '需认证' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="最大返回数">{{ service.max_records }}</el-descriptions-item>
          <el-descriptions-item label="数据库名">{{ service.database_name }}</el-descriptions-item>
          <el-descriptions-item v-if="service.description" label="描述" :span="2">{{ service.description }}</el-descriptions-item>
          <el-descriptions-item v-if="service.keywords?.length" label="关键词" :span="2">
            <el-tag v-for="k in service.keywords" :key="k" size="small" style="margin-right: 4px">{{ k }}</el-tag>
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- 配置详情 -->
      <el-card class="info-card">
        <template #header>
          <span>{{ service.config_type === 'label' ? '节点配置' : 'Cypher 配置' }}</span>
        </template>
        <el-descriptions v-if="service.config_type === 'label'" :column="1" border>
          <el-descriptions-item label="节点标签">
            <code>{{ service.node_label }}</code>
          </el-descriptions-item>
          <el-descriptions-item v-if="service.data_config?.properties?.length" label="返回属性">
            {{ service.data_config.properties.join(', ') }}
          </el-descriptions-item>
          <el-descriptions-item v-if="service.data_config?.filterable_properties?.length" label="可过滤属性">
            {{ service.data_config.filterable_properties.join(', ') }}
          </el-descriptions-item>
        </el-descriptions>
        <div v-else>
          <div class="cypher-block">{{ service.cypher_query }}</div>
          <el-descriptions :column="2" border style="margin-top:12px">
            <el-descriptions-item label="结果类型">
              {{ { table: '表格', graph: '图结构', both: '表格 + 图结构' }[service.data_config?.result_type] || '表格' }}
            </el-descriptions-item>
          </el-descriptions>
          <div v-if="service.parameters?.length" style="margin-top:12px">
            <strong>参数定义：</strong>
            <el-table :data="service.parameters" size="small" style="margin-top:8px">
              <el-table-column prop="name" label="参数名" width="150" />
              <el-table-column prop="type" label="类型" width="100" />
              <el-table-column prop="required" label="必填" width="80">
                <template #default="{ row }"><el-tag :type="row.required ? 'danger' : 'info'" size="small">{{ row.required ? '是' : '否' }}</el-tag></template>
              </el-table-column>
              <el-table-column prop="description" label="描述" />
            </el-table>
          </div>
        </div>
      </el-card>

      <!-- 访问端点 -->
      <el-card class="info-card">
        <template #header><span>访问端点</span></template>
        <el-descriptions :column="1" border>
          <el-descriptions-item label="执行查询">
            <code class="endpoint">POST {{ service.endpoints?.execute }}</code>
            <el-button link size="small" @click="copyText(service.endpoints?.execute)" style="margin-left:8px">复制</el-button>
          </el-descriptions-item>
        </el-descriptions>
        <div class="endpoint-help">
          <strong>调用示例：</strong>
          <pre class="code-block">{{ exampleRequest }}</pre>
        </div>
      </el-card>

      <!-- 在线测试 -->
      <el-card class="info-card">
        <template #header><span>在线测试</span></template>

        <!-- 参数输入 -->
        <div v-if="service.config_type === 'cypher' && service.parameters?.length">
          <el-form label-width="120px" style="margin-bottom:16px">
            <el-form-item v-for="p in service.parameters" :key="p.name" :label="p.name">
              <el-input v-model="testParams[p.name]" :placeholder="p.description || p.type" style="width:300px" />
              <el-tag v-if="p.required" type="danger" size="small" style="margin-left:6px">必填</el-tag>
            </el-form-item>
          </el-form>
        </div>
        <div v-if="service.config_type === 'label' && service.data_config?.filterable_properties?.length">
          <el-form label-width="120px" style="margin-bottom:16px">
            <el-form-item v-for="prop in service.data_config.filterable_properties" :key="prop" :label="prop">
              <el-input v-model="testParams[prop]" :placeholder="`过滤 ${prop}`" style="width:300px" />
            </el-form-item>
          </el-form>
        </div>

        <el-form label-width="120px" style="margin-bottom:16px">
          <el-form-item label="页码">
            <el-input-number v-model="testPage" :min="1" />
          </el-form-item>
          <el-form-item label="每页数量">
            <el-input-number v-model="testPageSize" :min="1" :max="service.max_records" />
          </el-form-item>
        </el-form>

        <el-button type="primary" :loading="testing" @click="runTest">执行查询</el-button>
        <el-button @click="clearTest">清除结果</el-button>

        <!-- 测试结果 -->
        <div v-if="testResult" class="test-result">
          <div class="result-meta">
            <span>返回 <strong>{{ testResult.rows_count }}</strong> 条记录</span>
            <span v-if="testResult.total_count != null">，共 <strong>{{ testResult.total_count }}</strong> 条</span>
          </div>

          <!-- 表格结果 -->
          <div v-if="testResult.columns?.length" style="margin-top:12px">
            <el-table :data="testResult.rows" size="small" max-height="400" border>
              <el-table-column v-for="col in testResult.columns" :key="col" :label="col" min-width="120">
                <template #default="{ row }">
                  <span v-if="typeof row[col] === 'object' && row[col] !== null">
                    <el-popover placement="bottom" :width="400" trigger="click">
                      <template #reference>
                        <el-button link size="small" type="primary">查看对象</el-button>
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
              图结构数据：{{ testResult.graph_data.nodes?.length || 0 }} 个节点，{{ testResult.graph_data.relationships?.length || 0 }} 条关系
            </el-alert>
            <el-collapse style="margin-top:8px">
              <el-collapse-item title="查看原始图数据（JSON）">
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
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import graphApi from '../api/graphQueryService'

const route = useRoute()
const id = route.params.id

const loading = ref(false)
const service = ref(null)
const testing = ref(false)
const testParams = ref({})
const testPage = ref(1)
const testPageSize = ref(20)
const testResult = ref(null)
const testError = ref('')

const statusType = (s) => ({ active: 'success', inactive: 'info', error: 'danger' }[s] || '')
const statusText = (s) => ({ active: '运行中', inactive: '已停用', error: '错误' }[s] || s)

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
  navigator.clipboard?.writeText(text).then(() => ElMessage.success('已复制'))
}

onMounted(async () => {
  loading.value = true
  try {
    service.value = await graphApi.getService(id)
  } catch (e) {
    ElMessage.error('加载失败：' + (e.response?.data?.error || e.message))
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.graph-service-detail {
  padding: 24px;
  min-height: 100%;
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
