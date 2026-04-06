<template>
  <div class="graph-service-form" v-loading="loading">
    <div class="page-header">
      <el-button @click="goBack" :icon="ArrowLeft" circle />
      <h2>{{ isEdit ? '编辑图查询服务' : '创建图查询服务' }}</h2>
    </div>

    <!-- 步骤条（仅新建） -->
    <el-steps v-if="!isEdit" :active="step" finish-status="success" align-center style="margin-bottom: 30px">
      <el-step title="选择配置类型" />
      <el-step title="配置数据源" />
      <el-step title="服务信息" />
    </el-steps>

    <!-- ===== Step 0: 选择配置类型 ===== -->
    <div v-if="!isEdit && step === 0">
      <el-card>
        <template #header><span>选择配置类型</span></template>
        <el-radio-group v-model="form.config_type" class="config-radio-group">
          <div class="config-card" :class="{ selected: form.config_type === 'label' }" @click="form.config_type = 'label'">
            <el-radio value="label">
              <div class="config-content">
                <h3>标签模式（推荐入门）</h3>
                <p>选择一个节点标签（如 Person、Company），系统自动生成查询 API，支持属性过滤和分页</p>
                <p class="tips">✅ 简单易用 &nbsp;✅ 支持过滤和分页 &nbsp;✅ 自动统计总数</p>
              </div>
            </el-radio>
          </div>
          <div class="config-card" :class="{ selected: form.config_type === 'cypher' }" @click="form.config_type = 'cypher'">
            <el-radio value="cypher">
              <div class="config-content">
                <h3>Cypher 模式（高级）</h3>
                <p>手写参数化 Cypher 查询，支持多标签联合、关系遍历，返回格式可选表格或图结构</p>
                <p class="tips">✅ 完全灵活 &nbsp;✅ 支持图结构返回 &nbsp;⚠️ 仅支持读操作</p>
              </div>
            </el-radio>
          </div>
        </el-radio-group>
      </el-card>
    </div>

    <!-- ===== Step 1: 配置数据源 ===== -->
    <div v-if="!isEdit && step === 1">
      <el-card>
        <template #header><span>配置 Neo4j 数据源</span></template>
        <el-form :model="form" label-width="130px">
          <!-- 公共：引擎选择 -->
          <el-form-item label="Neo4j 引擎" required>
            <el-select v-model="form.engine_id" placeholder="选择 Neo4j 引擎" style="width: 300px" :loading="enginesLoading">
              <el-option v-for="e in neo4jEngines" :key="e.id" :label="`${e.name} (${e.host || e.connection_info?.host || ''})`" :value="e.id" />
            </el-select>
            <el-button link @click="loadEngines" style="margin-left: 8px">刷新</el-button>
          </el-form-item>

          <el-form-item label="数据库名">
            <el-input v-model="form.database_name" placeholder="默认 neo4j" style="width: 200px" />
          </el-form-item>

          <!-- Label 模式 -->
          <template v-if="form.config_type === 'label'">
            <el-divider content-position="left">节点配置</el-divider>
            <el-form-item label="节点标签" required>
              <el-select
                v-model="form.node_label"
                placeholder="选择节点标签"
                style="width: 240px"
                :loading="labelsLoading"
                :disabled="!form.engine_id"
                filterable
                allow-create
              >
                <el-option v-for="label in nodeLabels" :key="label" :label="label" :value="label" />
              </el-select>
              <el-button link @click="loadNodeLabels" :disabled="!form.engine_id" style="margin-left: 8px">刷新</el-button>
              <span class="form-hint" style="display:block;margin-top:4px">区分大小写，与 Neo4j 中的标签名一致。可手动输入</span>
            </el-form-item>
            <el-form-item label="返回属性">
              <el-input v-model="labelPropertiesInput" placeholder="留空返回整个节点，或填逗号分隔的属性列表" style="width: 100%" />
              <span class="form-hint">示例：id,name,age,city（留空则 RETURN n）</span>
            </el-form-item>
            <el-form-item label="可过滤属性">
              <el-input v-model="filterablePropertiesInput" placeholder="允许客户端过滤的属性，逗号分隔" style="width: 100%" />
              <span class="form-hint">示例：name,city（留空则不支持过滤）</span>
            </el-form-item>
          </template>

          <!-- Cypher 模式 -->
          <template v-if="form.config_type === 'cypher'">
            <el-divider content-position="left">Cypher 查询</el-divider>
            <el-form-item label="Cypher 查询" required>
              <el-input
                v-model="form.cypher_query"
                type="textarea"
                :rows="6"
                placeholder="MATCH (n:Person) WHERE n.city = $city RETURN n SKIP $offset LIMIT $limit"
                style="font-family: monospace; width: 100%"
              />
              <div class="form-hint">
                使用 <code>$paramName</code> 声明参数，系统自动绑定。内置分页参数：<code>$offset</code>、<code>$limit</code>（无需手动声明）。
                <br>⚠️ 禁止写操作：CREATE、MERGE、DELETE、SET 等关键字将被拒绝。
              </div>
            </el-form-item>
            <el-form-item label="结果类型">
              <el-radio-group v-model="cypherResultType">
                <el-radio value="table">表格（Rows）</el-radio>
                <el-radio value="graph">图结构（Nodes + Relationships）</el-radio>
                <el-radio value="both">两者都要</el-radio>
              </el-radio-group>
            </el-form-item>
            <!-- 自动提取的参数预览 -->
            <el-form-item v-if="extractedParams.length > 0" label="自动提取参数">
              <el-tag v-for="p in extractedParams" :key="p" style="margin-right: 6px">${{ p }}</el-tag>
              <span class="form-hint" style="display:block;margin-top:4px">以上参数将作为必填参数发布到 API</span>
            </el-form-item>
          </template>
        </el-form>
      </el-card>
    </div>

    <!-- ===== Step 2 / 编辑模式：服务信息 ===== -->
    <div v-if="isEdit || step === 2">
      <el-card>
        <template #header><span>服务信息</span></template>
        <el-form :model="form" :rules="rules" ref="formRef" label-width="130px">
          <el-form-item v-if="!isEdit" label="服务名称" prop="service_name">
            <el-input v-model="form.service_name" placeholder="英文、数字、下划线，全局唯一" style="width: 300px" />
            <span class="form-hint">发布后不可修改，用于构成访问 URL</span>
          </el-form-item>
          <el-form-item v-else label="服务名称">
            <span class="readonly-text">{{ form.service_name }}</span>
          </el-form-item>

          <el-form-item label="标题" prop="title">
            <el-input v-model="form.title" placeholder="服务的显示标题" style="width: 400px" />
          </el-form-item>

          <el-form-item label="描述">
            <el-input v-model="form.description" type="textarea" :rows="3" placeholder="服务的详细描述" style="width: 100%" />
          </el-form-item>

          <el-form-item label="关键词">
            <el-input v-model="keywordsInput" placeholder="用逗号分隔，如：知识图谱,人员关系" style="width: 400px" />
          </el-form-item>

          <el-form-item label="最大返回记录数">
            <el-input-number v-model="form.max_records" :min="1" :max="5000" />
            <span class="form-hint" style="margin-left:8px">单次请求最多返回的记录数，默认 500</span>
          </el-form-item>

          <el-form-item label="公开访问">
            <el-switch v-model="form.public_access" />
            <span class="form-hint" style="margin-left:8px">开启后无需 JWT Token 即可访问</span>
          </el-form-item>

          <el-form-item v-if="isEdit" label="状态">
            <el-select v-model="form.status" style="width: 120px">
              <el-option value="active" label="运行中" />
              <el-option value="inactive" label="已停用" />
            </el-select>
          </el-form-item>
        </el-form>
      </el-card>
    </div>

    <!-- 按钮区域 -->
    <div class="form-actions">
      <el-button v-if="!isEdit && step > 0" @click="step--">上一步</el-button>
      <el-button v-if="!isEdit && step < 2" type="primary" @click="nextStep">下一步</el-button>
      <el-button v-if="isEdit || step === 2" type="primary" :loading="saving" @click="submit">
        {{ isEdit ? '保存' : '创建服务' }}
      </el-button>
      <el-button @click="goBack">取消</el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import graphApi from '../api/graphQueryService'

const router = useRouter()
const route = useRoute()
const id = route.params.id
const isEdit = computed(() => !!id)

const step = ref(0)
const loading = ref(false)
const saving = ref(false)
const enginesLoading = ref(false)
const labelsLoading = ref(false)
const neo4jEngines = ref([])
const nodeLabels = ref([])
const formRef = ref(null)

const form = ref({
  service_name: '',
  title: '',
  description: '',
  keywords: [],
  engine_id: null,
  database_name: '',
  config_type: 'label',
  node_label: '',
  cypher_query: '',
  public_access: false,
  max_records: 500,
  status: 'active',
  data_config: {}
})

// 辅助输入字段
const labelPropertiesInput = ref('')
const filterablePropertiesInput = ref('')
const cypherResultType = ref('table')
const keywordsInput = ref('')

// 自动提取 Cypher 参数
const builtinParams = new Set(['offset', 'limit', 'skip', 'page_size'])
const extractedParams = computed(() => {
  if (form.value.config_type !== 'cypher' || !form.value.cypher_query) return []
  const matches = [...form.value.cypher_query.matchAll(/\$(\w+)/g)]
  const seen = new Set()
  return matches.map(m => m[1]).filter(name => {
    const lower = name.toLowerCase()
    if (builtinParams.has(lower) || seen.has(lower)) return false
    seen.add(lower)
    return true
  })
})

const rules = {
  service_name: [{ required: true, message: '请输入服务名称', trigger: 'blur' },
    { pattern: /^[a-zA-Z0-9_-]+$/, message: '只允许英文、数字、下划线、连字符', trigger: 'blur' }],
  title: [{ required: true, message: '请输入标题', trigger: 'blur' }]
}

const loadEngines = async () => {
  enginesLoading.value = true
  try {
    neo4jEngines.value = await graphApi.getNeo4jEngines()
  } catch {
    neo4jEngines.value = []
  } finally {
    enginesLoading.value = false
  }
}

const loadNodeLabels = async () => {
  if (!form.value.engine_id) return
  labelsLoading.value = true
  try {
    nodeLabels.value = await graphApi.getNodeLabels(form.value.engine_id, form.value.database_name || 'neo4j')
  } catch {
    nodeLabels.value = []
  } finally {
    labelsLoading.value = false
  }
}

// 切换引擎时自动加载节点标签
watch(() => form.value.engine_id, (newId) => {
  if (newId && form.value.config_type === 'label') {
    loadNodeLabels()
  }
})

const nextStep = async () => {
  if (step.value === 0) {
    step.value = 1
    if (neo4jEngines.value.length === 0) loadEngines()
    return
  }
  if (step.value === 1) {
    if (!form.value.engine_id) { ElMessage.warning('请选择 Neo4j 引擎'); return }
    if (form.value.config_type === 'label' && !form.value.node_label) { ElMessage.warning('请填写节点标签'); return }
    if (form.value.config_type === 'cypher' && !form.value.cypher_query) { ElMessage.warning('请填写 Cypher 查询'); return }
    step.value = 2
  }
}

const buildPayload = () => {
  const data_config = {}
  if (form.value.config_type === 'label') {
    const props = labelPropertiesInput.value.split(',').map(s => s.trim()).filter(Boolean)
    const filterable = filterablePropertiesInput.value.split(',').map(s => s.trim()).filter(Boolean)
    if (props.length) data_config.properties = props
    if (filterable.length) data_config.filterable_properties = filterable
  } else {
    data_config.result_type = cypherResultType.value
  }

  return {
    ...form.value,
    keywords: keywordsInput.value.split(',').map(s => s.trim()).filter(Boolean),
    database_name: form.value.database_name || 'neo4j',
    data_config
  }
}

const submit = async () => {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid && !isEdit.value) return

  saving.value = true
  try {
    if (isEdit.value) {
      const payload = {
        title: form.value.title,
        description: form.value.description,
        keywords: keywordsInput.value.split(',').map(s => s.trim()).filter(Boolean),
        public_access: form.value.public_access,
        max_records: form.value.max_records,
        status: form.value.status
      }
      if (form.value.config_type === 'cypher' && form.value.cypher_query) {
        payload.cypher_query = form.value.cypher_query
      }
      await graphApi.updateService(id, payload)
      ElMessage.success('保存成功')
    } else {
      const payload = buildPayload()
      const result = await graphApi.createService(payload)
      ElMessage.success('创建成功')
      router.push(`/graph-services/${result.id}`)
      return
    }
    router.push(`/graph-services/${id}`)
  } catch (e) {
    ElMessage.error('操作失败：' + (e.response?.data?.error || e.message))
  } finally {
    saving.value = false
  }
}

const goBack = () => router.back()

onMounted(async () => {
  if (isEdit.value) {
    loading.value = true
    try {
      const data = await graphApi.getService(id)
      Object.assign(form.value, data)
      keywordsInput.value = (data.keywords || []).join(', ')
      if (data.config_type === 'label') {
        const dc = data.data_config || {}
        labelPropertiesInput.value = (dc.properties || []).join(', ')
        filterablePropertiesInput.value = (dc.filterable_properties || []).join(', ')
      } else {
        cypherResultType.value = data.data_config?.result_type || 'table'
      }
      await loadEngines()
    } catch (e) {
      ElMessage.error('加载失败：' + (e.response?.data?.error || e.message))
    } finally {
      loading.value = false
    }
  }
})
</script>

<style scoped>
.graph-service-form {
  padding: 24px;
  min-height: 100%;
  background: var(--addp-bg-secondary);
}
.page-header { display: flex; align-items: center; gap: 12px; margin-bottom: 24px; }
.config-radio-group { display: flex; gap: 20px; width: 100%; }
.config-card {
  flex: 1;
  border: 2px solid var(--addp-border-color);
  border-radius: 8px;
  padding: 16px;
  cursor: pointer;
  transition: all 0.2s;
}
.config-card.selected {
  border-color: var(--el-color-primary);
  background: var(--addp-bg-secondary);
}
.config-card:hover { border-color: var(--el-color-primary-light-5); }
.config-content h3 { margin: 0 0 8px; font-size: 15px; color: var(--addp-text-primary); }
.config-content p { margin: 6px 0; font-size: 13px; color: var(--addp-text-secondary); }
.config-content .tips { color: var(--el-color-success); font-size: 12px; }
.form-hint { color: var(--addp-text-tertiary); font-size: 12px; margin-top: 4px; }
code {
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  padding: 1px 4px;
  border-radius: 3px;
  font-family: monospace;
}
.readonly-text { color: var(--addp-text-primary); font-weight: 500; }
.form-actions { margin-top: 24px; display: flex; gap: 12px; }
</style>
