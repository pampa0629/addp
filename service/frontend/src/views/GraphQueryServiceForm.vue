<template>
  <div class="graph-service-form" v-loading="loading">
    <div class="page-header">
      <el-button @click="goBack" :icon="ArrowLeft" circle />
      <h2>{{ isEdit ? t('service.graph.formEditTitle') : t('service.graph.formCreateTitle') }}</h2>
    </div>

    <!-- 步骤条（仅新建） -->
    <el-steps v-if="!isEdit" :active="step" finish-status="success" align-center style="margin-bottom: 30px">
      <el-step :title="t('service.graph.step1Title')" />
      <el-step :title="t('service.graph.step2Title')" />
      <el-step :title="t('service.graph.step3Title')" />
    </el-steps>

    <!-- ===== Step 0: 选择配置类型 ===== -->
    <div v-if="!isEdit && step === 0">
      <el-card>
        <template #header><span>{{ t('service.graph.selectConfigType') }}</span></template>
        <el-radio-group v-model="form.config_type" class="config-radio-group">
          <div class="config-card" :class="{ selected: form.config_type === 'shape' }" @click="form.config_type = 'shape'">
            <el-radio value="shape">
              <div class="config-content">
                <h3>{{ t('service.graph.shapeModeTitle') }}</h3>
                <p>{{ t('service.graph.shapeModeDesc') }}</p>
                <p class="tips">{{ t('service.graph.shapeModeTips') }}</p>
              </div>
            </el-radio>
          </div>
          <div class="config-card" :class="{ selected: form.config_type === 'cypher' }" @click="form.config_type = 'cypher'">
            <el-radio value="cypher">
              <div class="config-content">
                <h3>{{ t('service.graph.cypherModeTitle') }}</h3>
                <p>{{ t('service.graph.cypherModeDesc') }}</p>
                <p class="tips">{{ t('service.graph.cypherModeTips') }}</p>
              </div>
            </el-radio>
          </div>
        </el-radio-group>
      </el-card>
    </div>

    <!-- ===== Step 1: 配置数据源 ===== -->
    <div v-if="!isEdit && step === 1">
      <el-card>
        <template #header><span>{{ t('service.graph.configDataSource') }}</span></template>
        <el-form :model="form" label-width="130px">
          <!-- 公共：引擎选择 -->
          <el-form-item :label="t('service.graph.neo4jEngineLabel')" required>
            <el-select v-model="form.engine_id" :placeholder="t('service.graph.neo4jEnginePlaceholder')" style="width: 300px" :loading="enginesLoading">
              <el-option v-for="e in neo4jEngines" :key="e.id" :label="`${e.name} (${e.host || e.connection_info?.host || ''})`" :value="e.id" />
            </el-select>
            <el-button link @click="loadEngines" style="margin-left: 8px">{{ t('service.common.refresh') }}</el-button>
          </el-form-item>

          <el-form-item :label="t('service.graph.databaseNameLabel')">
            <el-input v-model="form.database_name" :placeholder="t('service.graph.databaseNamePlaceholder')" style="width: 200px" />
          </el-form-item>

          <!-- Node Shape 模式 -->
          <template v-if="form.config_type === 'shape'">
            <el-divider content-position="left">{{ t('service.graph.nodeConfigTitle') }}</el-divider>
            <el-form-item :label="t('service.graph.nodeShapeLabel')" required>
              <el-select
                v-model="form.node_shape"
                :placeholder="t('service.graph.nodeShapePlaceholder')"
                style="width: 240px"
                :loading="shapesLoading"
                :disabled="!form.engine_id"
                filterable
                @change="handleNodeShapeChange"
              >
                <el-option
                  v-for="shape in nodeShapes"
                  :key="shape.name"
                  :label="graphShapeLabel(shape)"
                  :value="shape.name"
                />
              </el-select>
              <el-button link @click="loadNodeShapes" :disabled="!form.engine_id" style="margin-left: 8px">{{ t('service.common.refresh') }}</el-button>
              <span class="form-hint" style="display:block;margin-top:4px">{{ t('service.graph.nodeShapeHelp') }}</span>
            </el-form-item>
            <el-form-item :label="t('service.graph.returnPropertiesLabel')">
              <el-input v-model="shapePropertiesInput" :placeholder="t('service.graph.returnPropertiesPlaceholder')" style="width: 100%" />
              <span class="form-hint">{{ t('service.graph.returnPropertiesHelp') }}</span>
            </el-form-item>
            <el-form-item :label="t('service.graph.filterablePropertiesLabel')">
              <el-input v-model="filterablePropertiesInput" :placeholder="t('service.graph.filterablePropertiesPlaceholder')" style="width: 100%" />
              <span class="form-hint">{{ t('service.graph.filterablePropertiesHelp') }}</span>
            </el-form-item>
          </template>

          <!-- Cypher 模式 -->
          <template v-if="form.config_type === 'cypher'">
            <el-divider content-position="left">{{ t('service.graph.cypherQueryTitle') }}</el-divider>
            <el-form-item :label="t('service.graph.cypherQueryLabel')" required>
              <el-input
                v-model="form.cypher_query"
                type="textarea"
                :rows="6"
                placeholder="MATCH (n:Person) WHERE n.city = $city RETURN n SKIP $offset LIMIT $limit"
                style="font-family: monospace; width: 100%"
              />
              <div class="form-hint">
                {{ t('service.graph.cypherQueryHelp1') }}
                <br>{{ t('service.graph.cypherQueryHelp2') }}
              </div>
            </el-form-item>
            <el-form-item :label="t('service.graph.resultTypeLabel')">
              <el-radio-group v-model="cypherResultType">
                <el-radio value="table">{{ t('service.graph.resultTypeTable') }}</el-radio>
                <el-radio value="graph">{{ t('service.graph.resultTypeGraph') }}</el-radio>
                <el-radio value="both">{{ t('service.graph.resultTypeBoth') }}</el-radio>
              </el-radio-group>
            </el-form-item>
            <!-- 自动提取的参数预览 -->
            <el-form-item v-if="extractedParams.length > 0" :label="t('service.graph.extractedParamsLabel')">
              <el-tag v-for="p in extractedParams" :key="p" style="margin-right: 6px">${{ p }}</el-tag>
              <span class="form-hint" style="display:block;margin-top:4px">{{ t('service.graph.extractedParamsHelp') }}</span>
            </el-form-item>
          </template>
        </el-form>
      </el-card>
    </div>

    <!-- ===== Step 2 / 编辑模式：服务信息 ===== -->
    <div v-if="isEdit || step === 2">
      <el-card>
        <template #header><span>{{ t('service.graph.serviceInfoTitle') }}</span></template>
        <el-form :model="form" :rules="rules" ref="formRef" label-width="130px">
          <el-form-item v-if="!isEdit" :label="t('service.graph.serviceNameLabel')" prop="service_name">
            <el-input v-model="form.service_name" :placeholder="t('service.graph.serviceNamePlaceholder')" style="width: 300px" />
            <span class="form-hint">{{ t('service.graph.serviceNameHelp') }}</span>
          </el-form-item>
          <el-form-item v-else :label="t('service.graph.serviceNameLabel')">
            <span class="readonly-text">{{ form.service_name }}</span>
          </el-form-item>

          <el-form-item :label="t('service.graph.titleLabel')" prop="title">
            <el-input v-model="form.title" :placeholder="t('service.graph.titlePlaceholder')" style="width: 400px" />
          </el-form-item>

          <el-form-item :label="t('service.graph.descriptionLabel')">
            <el-input v-model="form.description" type="textarea" :rows="3" :placeholder="t('service.graph.descriptionPlaceholder')" style="width: 100%" />
          </el-form-item>

          <el-form-item :label="t('service.graph.keywordsLabel')">
            <el-input v-model="keywordsInput" :placeholder="t('service.graph.keywordsPlaceholder')" style="width: 400px" />
          </el-form-item>

          <el-form-item :label="t('service.graph.maxRecordsLabel')">
            <el-input-number v-model="form.max_records" :min="1" :max="5000" />
            <span class="form-hint" style="margin-left:8px">{{ t('service.graph.maxRecordsHelp') }}</span>
          </el-form-item>

          <el-form-item :label="t('service.graph.publicAccessLabel')">
            <el-switch v-model="form.public_access" />
            <span class="form-hint" style="margin-left:8px">{{ t('service.graph.publicAccessHelp') }}</span>
          </el-form-item>

          <el-form-item v-if="isEdit" :label="t('service.graph.statusLabel')">
            <el-select v-model="form.status" style="width: 120px">
              <el-option value="active" :label="t('service.graph.statusRunning')" />
              <el-option value="inactive" :label="t('service.graph.statusInactive')" />
            </el-select>
          </el-form-item>
        </el-form>
      </el-card>
    </div>

    <!-- 按钮区域 -->
    <div class="form-actions">
      <el-button v-if="!isEdit && step > 0" @click="step--">{{ t('service.graph.prevStep') }}</el-button>
      <el-button v-if="!isEdit && step < 2" type="primary" @click="nextStep">{{ t('service.graph.nextStep') }}</el-button>
      <el-button v-if="isEdit || step === 2" type="primary" :loading="saving" @click="submit">
        {{ isEdit ? t('service.common.save') : t('service.graph.createBtn2') }}
      </el-button>
      <el-button @click="goBack">{{ t('service.common.cancel') }}</el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import graphApi from '../api/graphQueryService'
import { navigateServiceRoute } from '@/utils/moduleNavigation'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const id = route.params.id
const isEdit = computed(() => !!id)

const step = ref(0)
const loading = ref(false)
const saving = ref(false)
const enginesLoading = ref(false)
const shapesLoading = ref(false)
const neo4jEngines = ref([])
const nodeShapes = ref([])
const formRef = ref(null)

const form = ref({
  service_name: '',
  title: '',
  description: '',
  keywords: [],
  engine_id: null,
  database_name: '',
  config_type: 'shape',
  node_shape: '',
  node_labels: [],
  cypher_query: '',
  public_access: false,
  max_records: 500,
  status: 'active',
  data_config: {}
})

// 辅助输入字段
const shapePropertiesInput = ref('')
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
  service_name: [{ required: true, message: t('service.graph.serviceNameRequired'), trigger: 'blur' },
    { pattern: /^[a-zA-Z0-9_-]+$/, message: t('service.graph.serviceNamePattern'), trigger: 'blur' }],
  title: [{ required: true, message: t('service.graph.titleRequired'), trigger: 'blur' }]
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

const loadNodeShapes = async () => {
  if (!form.value.engine_id) return
  shapesLoading.value = true
  try {
    nodeShapes.value = await graphApi.getNodeShapes(form.value.engine_id, form.value.database_name || 'neo4j')
    if (form.value.node_shape) handleNodeShapeChange(form.value.node_shape, false)
  } catch {
    nodeShapes.value = []
  } finally {
    shapesLoading.value = false
  }
}

const graphShapeLabel = (shape) => {
  if (!shape) return ''
  const labels = Array.isArray(shape.labels) && shape.labels.length ? `:${shape.labels.join(':')}` : ''
  const count = shape.count != null ? ` (${shape.count})` : ''
  return `${shape.name || labels || '-'}${labels && labels !== `:${shape.name}` ? ` ${labels}` : ''}${count}`
}

const handleNodeShapeChange = (shapeName, fillProperties = true) => {
  const shape = nodeShapes.value.find(item => item.name === shapeName)
  form.value.node_shape = shapeName || ''
  form.value.node_labels = Array.isArray(shape?.labels) ? [...shape.labels] : []
  if (fillProperties && Array.isArray(shape?.properties) && shape.properties.length) {
    const props = shape.properties.map(prop => prop.name).filter(Boolean)
    shapePropertiesInput.value = props.join(', ')
    filterablePropertiesInput.value = props.join(', ')
  }
}

// 切换引擎时自动加载节点形状
watch(() => form.value.engine_id, (newId) => {
  if (newId && form.value.config_type === 'shape') {
    loadNodeShapes()
  }
})

const nextStep = async () => {
  if (step.value === 0) {
    step.value = 1
    if (neo4jEngines.value.length === 0) loadEngines()
    return
  }
  if (step.value === 1) {
    if (!form.value.engine_id) { ElMessage.warning(t('service.graph.selectEngineWarning')); return }
    if (form.value.config_type === 'shape' && !form.value.node_shape) { ElMessage.warning(t('service.graph.selectNodeShapeWarning')); return }
    if (form.value.config_type === 'cypher' && !form.value.cypher_query) { ElMessage.warning(t('service.graph.enterCypherWarning')); return }
    step.value = 2
  }
}

const buildPayload = () => {
  const data_config = {}
  if (form.value.config_type === 'shape') {
    handleNodeShapeChange(form.value.node_shape, false)
    const props = shapePropertiesInput.value.split(',').map(s => s.trim()).filter(Boolean)
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
      ElMessage.success(t('service.graph.saveSuccess'))
    } else {
      const payload = buildPayload()
      const result = await graphApi.createService(payload)
      ElMessage.success(t('service.graph.createSuccess'))
      await navigateServiceRoute(router, `/graph-services/${result.id}`, { history: 'replace' })
      return
    }
    await navigateServiceRoute(router, `/graph-services/${id}`, { history: 'replace' })
  } catch (e) {
    ElMessage.error(t('service.graph.operationFailed') + '：' + (e.response?.data?.error || e.message))
  } finally {
    saving.value = false
  }
}

const goBack = () => navigateServiceRoute(router, isEdit.value ? `/graph-services/${id}` : '/graph-services', { history: 'replace' })

onMounted(async () => {
  if (isEdit.value) {
    loading.value = true
    try {
      const data = await graphApi.getService(id)
      Object.assign(form.value, data)
      keywordsInput.value = (data.keywords || []).join(', ')
      if (data.config_type === 'shape') {
        const dc = data.data_config || {}
        shapePropertiesInput.value = (dc.properties || []).join(', ')
        filterablePropertiesInput.value = (dc.filterable_properties || []).join(', ')
      } else {
        cypherResultType.value = data.data_config?.result_type || 'table'
      }
      await loadEngines()
    } catch (e) {
      ElMessage.error(t('service.graph.loadFailed') + '：' + (e.response?.data?.error || e.message))
    } finally {
      loading.value = false
    }
  }
})
</script>

<style scoped>
.graph-service-form {
  padding: 24px;
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
