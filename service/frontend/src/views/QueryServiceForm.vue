<template>
  <div class="query-service-form" v-loading="loading">
    <div class="page-header">
      <el-button @click="goBack" :icon="ArrowLeft" circle />
      <h2>{{ isEdit ? t('service.query.formEditTitle') : t('service.query.formCreateTitle') }}</h2>
    </div>

    <!-- 步骤条（仅新建模式显示） -->
    <el-steps
      v-if="!isEdit"
      :active="currentStep"
      finish-status="success"
      align-center
      style="margin-bottom: 30px"
    >
      <el-step :title="t('service.query.stepSelectMode')" />
      <el-step :title="t('service.query.stepConfigSource')" />
      <el-step :title="t('service.query.stepConfigService')" />
    </el-steps>

    <!-- Step 0: 选择配置方式（仅新建模式） -->
    <div v-if="!isEdit && currentStep === 0">
      <el-card class="config-step-card">
        <template #header>
          <span>{{ t('service.query.selectModeTitle') }}</span>
        </template>

        <el-radio-group v-model="form.config_type" class="config-radio-group">
          <div
            class="config-card-wrapper"
            :class="{ 'selected': form.config_type === 'table' }"
            @click="form.config_type = 'table'"
          >
            <el-radio value="table" class="config-radio">
              <div class="config-content">
                <h3><el-icon><Grid /></el-icon> {{ t('service.query.tableModeTitle') }}</h3>
                <p class="description">{{ t('service.query.tableModeDesc') }}</p>
                <div class="features">
                  <p>{{ t('service.query.tableModeFeature1') }}</p>
                  <p>{{ t('service.query.tableModeFeature2') }}</p>
                  <p>{{ t('service.query.tableModeFeature3') }}</p>
                </div>
                <el-tag type="success" size="small">{{ t('service.query.tableModeTag') }}</el-tag>
              </div>
            </el-radio>
          </div>

          <div
            class="config-card-wrapper"
            :class="{ 'selected': form.config_type === 'sql' }"
            @click="form.config_type = 'sql'"
          >
            <el-radio value="sql" class="config-radio">
              <div class="config-content">
                <h3><el-icon><Document /></el-icon> {{ t('service.query.sqlModeTitle') }}</h3>
                <p class="description">{{ t('service.query.sqlModeDesc') }}</p>
                <div class="features">
                  <p>{{ t('service.query.sqlModeFeature1') }}</p>
                  <p>{{ t('service.query.sqlModeFeature2') }}</p>
                  <p>{{ t('service.query.sqlModeFeature3') }}</p>
                </div>
                <el-tag type="warning" size="small">{{ t('service.query.sqlModeTag') }}</el-tag>
              </div>
            </el-radio>
          </div>
        </el-radio-group>
      </el-card>
    </div>

    <!-- Step 1: 配置数据源 -->
    <div v-if="!isEdit && currentStep === 1">
      <!-- Table 模式 -->
      <div v-if="form.config_type === 'table'">
        <el-card>
          <template #header>
            <span>{{ t('service.query.selectTableTitle') }}</span>
          </template>

          <ResourceTreePicker
            :api-base-url="metaApiBaseUrl"
            :engine-types="QUERY_TABLE_ENGINE_TYPES"
            mode="item"
            :node-filter="isQueryableTableVisibleNode"
            :selectable-filter="isQueryableTableNode"
            :show-selection-summary="true"
            :engine-multiple="true"
            :select-all-engines-by-default="true"
            :search-selectable-only="true"
            :show-disabled-label="false"
            :show-count="false"
            @update:model-value="handleTableSelection"
          />

          <el-form :model="form" label-width="120px" style="margin-top: 16px">

            <!-- 字段配置（可选） -->
            <el-divider content-position="left">字段配置（可选）</el-divider>

            <el-form-item :label="t('service.query.defaultFieldsLabel')">
              <el-input
                v-model="defaultFieldsInput"
                :placeholder="t('service.query.defaultFieldsPlaceholder')"
                style="width: 100%"
              />
              <div class="help-text">
                {{ t('service.query.defaultFieldsHelp') }}
              </div>
            </el-form-item>

            <el-form-item :label="t('service.query.filterableFieldsLabel')">
              <el-input
                v-model="filterableFieldsInput"
                :placeholder="t('service.query.filterableFieldsPlaceholder')"
                style="width: 100%"
              />
              <div class="help-text">
                {{ t('service.query.filterableFieldsHelp') }}
              </div>
            </el-form-item>
          </el-form>
        </el-card>
      </div>

      <!-- SQL 模式 -->
      <div v-else-if="form.config_type === 'sql'">
        <el-card>
          <template #header>
            <span>{{ t('service.query.writeSqlTitle') }}</span>
          </template>

          <el-form :model="form" label-width="120px">
            <el-form-item :label="t('service.query.engineLabel')" required>
              <el-select v-model="form.engine_id" :placeholder="t('service.query.enginePlaceholder')" style="width: 400px">
                <el-option
                  v-for="engine in sqlSupportedEngines"
                  :key="engine.id"
                  :label="`${engine.name} (${engine.engine_type})`"
                  :value="engine.id"
                />
              </el-select>
              <div class="help-text">
                {{ t('service.query.engineHelp') }}
              </div>
            </el-form-item>

            <el-form-item :label="t('service.query.sqlLabel')" required>
              <el-input
                v-model="form.sql_query"
                type="textarea"
                :rows="10"
                placeholder="示例：&#10;SELECT id, name, ST_AsGeoJSON(geom) as geometry&#10;FROM cities&#10;WHERE population > 1000000"
                style="font-family: 'Courier New', monospace"
              />
              <div class="help-text">
                {{ t('service.query.sqlHelp') }}
              </div>
            </el-form-item>

            <el-divider content-position="left">空间字段配置（可选）</el-divider>

            <el-form-item>
              <el-button
                type="primary"
                :loading="detectingSQLSpatial"
                :disabled="form.engine_id === null || !form.sql_query"
                @click="detectSQLSpatialFields"
              >
                {{ t('service.query.detectSpatialBtn') }}
              </el-button>
              <div class="help-text">
                {{ t('service.query.detectSpatialHelp') }}
              </div>
            </el-form-item>

            <el-form-item :label="t('service.query.hasSpatialLabel')">
              <el-checkbox v-model="sqlHasGeometry">
                {{ t('service.query.hasSpatialCheckbox') }}
              </el-checkbox>
            </el-form-item>

            <div v-if="sqlHasGeometry">
              <el-form-item :label="t('service.query.geometryColumnLabel')" required>
                <el-input v-model="sqlGeometryColumn" :placeholder="t('service.query.geometryColumnPlaceholder')" style="width: 300px" />
                <div class="help-text">
                  {{ t('service.query.geometryColumnHelp') }}
                </div>
              </el-form-item>

              <el-form-item :label="t('service.query.sridLabel')" required>
                <el-input-number v-model="sqlSrid" :min="0" :max="999999" placeholder="例如: 4326" />
                <div class="help-text">
                  {{ t('service.query.sridHelp') }}
                </div>
              </el-form-item>

              <el-form-item :label="t('service.query.geometryTypeLabel')" v-if="sqlGeometryType">
                <el-tag>{{ sqlGeometryType }}</el-tag>
                <div class="help-text">
                  {{ t('service.query.geometryTypeHelp') }}
                </div>
              </el-form-item>
            </div>
          </el-form>
        </el-card>
      </div>
    </div>

    <!-- Step 2: 配置服务信息 -->
    <div v-if="isEdit || currentStep === 2">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="140px">
        <!-- 基本信息 -->
        <el-card :header="t('service.query.basicInfoTitle')" style="margin-bottom: 20px">
          <el-form-item :label="t('service.query.configTypeLabel')" v-if="!isEdit">
            <el-tag :type="form.config_type === 'table' ? 'success' : 'warning'" size="large">
              {{ form.config_type === 'table' ? t('service.query.configTypeTable') : t('service.query.configTypeSql') }}
            </el-tag>
            <span style="margin-left: 12px; color: var(--addp-text-tertiary); font-size: 13px">
              {{ t('service.query.configTypeNote') }}
            </span>
          </el-form-item>

          <el-form-item :label="t('service.query.serviceNameLabel')" prop="service_name" required>
            <el-input
              v-model="form.service_name"
              :placeholder="t('service.query.serviceNamePlaceholder')"
              :disabled="isEdit"
              style="width: 400px"
            />
            <div class="help-text">
              {{ t('service.query.serviceNameHelp') }}
            </div>
          </el-form-item>

          <el-form-item :label="t('service.query.titleLabel')" prop="title" required>
            <el-input v-model="form.title" :placeholder="t('service.query.titlePlaceholder')" style="width: 400px" />
          </el-form-item>

          <el-form-item :label="t('service.query.descriptionLabel')">
            <el-input
              type="textarea"
              v-model="form.description"
              :rows="3"
              :placeholder="t('service.query.descriptionPlaceholder')"
            />
          </el-form-item>

          <el-form-item :label="t('service.query.keywordsLabel')">
            <div class="keyword-input">
              <el-tag
                v-for="tag in form.keywords"
                :key="tag"
                closable
                @close="removeKeyword(tag)"
                style="margin-right: 8px; margin-bottom: 8px"
              >
                {{ tag }}
              </el-tag>
              <el-input
                v-if="inputVisible"
                v-model="inputValue"
                ref="inputRef"
                size="small"
                style="width: 120px"
                @keyup.enter="handleInputConfirm"
                @blur="handleInputConfirm"
              />
              <el-button v-else size="small" @click="showInput">
                {{ t('service.common.addKeyword') }}
              </el-button>
            </div>
          </el-form-item>
        </el-card>

        <!-- 协议配置 -->
        <el-card :header="t('service.query.protocolConfigTitle')" style="margin-bottom: 20px">
          <el-alert type="info" :closable="false" style="margin-bottom: 16px">
            {{ t('service.query.protocolAlert') }}
          </el-alert>

          <el-form-item :label="t('service.query.restApiLabel')">
            <el-checkbox v-model="enableRestApi" disabled>
              {{ t('service.query.restApiCheckbox') }}
            </el-checkbox>
            <div class="help-text">
              {{ t('service.query.restApiHelp') }}
            </div>
          </el-form-item>

          <el-form-item :label="t('service.query.ogcFeaturesLabel')">
            <el-checkbox v-model="enableOgcFeatures" :disabled="!hasGeometryField">
              {{ t('service.query.ogcFeaturesCheckbox') }}
            </el-checkbox>
            <div class="help-text">
              {{ hasGeometryField ? t('service.query.ogcFeaturesHelpEnabled') : t('service.query.ogcFeaturesHelpDisabled') }}
            </div>
          </el-form-item>
        </el-card>

        <!-- 访问控制 -->
        <el-card :header="t('service.query.accessControlTitle')" style="margin-bottom: 20px">
          <el-form-item :label="t('service.query.publicAccessLabel')">
            <el-checkbox v-model="form.public_access">
              {{ t('service.query.publicAccessCheckbox') }}
            </el-checkbox>
            <div class="help-text">
              {{ t('service.query.publicAccessHelp') }}
            </div>
          </el-form-item>

          <el-form-item :label="t('service.query.maxFeaturesLabel')" prop="max_features">
            <el-input-number v-model="form.max_features" :min="1" :max="10000" />
            <div class="help-text">{{ t('service.query.maxFeaturesHelp') }}</div>
          </el-form-item>
        </el-card>
      </el-form>
    </div>

    <!-- 操作按钮 -->
    <div class="button-group">
      <el-button v-if="!isEdit && currentStep > 0" @click="prevStep">
        {{ t('service.query.prevStep') }}
      </el-button>
      <el-button
        v-if="!isEdit && currentStep < 2"
        type="primary"
        :disabled="!canProceed"
        @click="nextStep"
      >
        {{ t('service.query.nextStep') }}
      </el-button>
      <el-button
        v-if="isEdit || currentStep === 2"
        type="primary"
        @click="handleSubmit"
        :loading="submitting"
      >
        {{ isEdit ? t('service.query.updateBtn') : t('service.query.createBtn') }}
      </el-button>
      <el-button @click="goBack">
        {{ t('service.common.cancel') }}
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Grid, Document, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import queryServiceAPI from '@/api/queryService'
import { ResourceTreePicker, detectTableMetadata, locatorPathFromSelection } from '@common-ui'
import {
  QUERY_TABLE_ENGINE_TYPES,
  isQueryableTableNode,
  isQueryableTableVisibleNode
} from '@/utils/resourceSelection'
import { navigateServiceRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()

const loading = ref(false)
const submitting = ref(false)
const currentStep = ref(0)
const detectingSQLSpatial = ref(false)

const isEdit = computed(() => !!route.params.id)

// 表单数据
const form = reactive({
  config_type: 'table',
  engine_id: null,
  schema_name: '',
  table_name: '',
  sql_query: '',
  service_name: '',
  title: '',
  description: '',
  keywords: [],
  public_access: true, // 默认公开访问
  max_features: 1000
})

// 存储引擎列表（SQL 模式下使用）
const engines = ref([])

// Meta resource-tree API 基础 URL。资源选择使用 locator 主身份，业务层按需检测空间能力。
const metaApiBaseUrl = computed(() => {
  return '/api/v1/meta'
})

// 空间元数据
const spatialMetadata = ref(null)

// SQL 模式的空间字段配置
const sqlHasGeometry = ref(false)
const sqlGeometryColumn = ref('')
const sqlSrid = ref(0)
const sqlGeometryType = ref('')
const sqlOutputContract = ref(null)

// 字段配置输入
const defaultFieldsInput = ref('')
const filterableFieldsInput = ref('')

// 协议启用状态
const enableRestApi = ref(true) // REST API 默认启用，不可禁用
const enableOgcFeatures = ref(false)

// 关键词输入
const inputVisible = ref(false)
const inputValue = ref('')
const inputRef = ref(null)

// 表单验证规则
const rules = computed(() => ({
  service_name: [
    { required: true, message: t('service.query.serviceNamePlaceholder'), trigger: 'blur' },
    { pattern: /^[a-z0-9_]+$/, message: t('service.query.serviceNameHelp'), trigger: 'blur' }
  ],
  title: [
    { required: true, message: t('service.query.titlePlaceholder'), trigger: 'blur' }
  ],
  max_features: [
    { required: true, message: t('service.query.maxFeaturesHelp'), trigger: 'blur' },
    { type: 'number', min: 1, max: 10000, message: '范围: 1-10000', trigger: 'blur' }
  ]
}))

// 计算属性：是否检测到空间字段
const hasGeometryField = computed(() => {
  if (form.config_type === 'table') {
    return spatialMetadata.value?.hasGeometry === true
  } else {
    return sqlHasGeometry.value
  }
})

// 计算属性：支持 SQL 的存储引擎（含 DuckDB 虚拟引擎）
const sqlSupportedEngines = computed(() => {
  const supportedTypes = ['postgresql', 'mysql', 'doris', 'clickhouse', 'mongodb', 'spark', 'minio', 's3']
  const realEngines = engines.value.filter(engine => {
    const engineType = engine.engine_type?.toLowerCase() || ''
    return supportedTypes.includes(engineType)
  })
  // 追加 DuckDB 虚拟引擎（engine_id = null）
  return [
    ...realEngines,
    { id: null, name: 'DuckDB', engine_type: 'duckdb', _virtual: true }
  ]
})

// 计算属性：是否可以进入下一步
const canProceed = computed(() => {
  if (currentStep.value === 0) {
    return !!form.config_type
  } else if (currentStep.value === 1) {
    if (form.config_type === 'table') {
      return !!form.locator
    } else {
      // SQL 模式：DuckDB 虚拟引擎（engine_id=null）只需要 sql_query
      const isDuckDB = form.engine_id === null && form.config_type === 'sql'
      return (isDuckDB || form.engine_id) && form.sql_query
    }
  }
  return true
})

// 方法：检测 SQL 查询结果的空间字段
const detectSQLSpatialFields = async () => {
  if (!form.engine_id || !form.sql_query) {
    ElMessage.warning(t('service.query.detectSqlRequired'))
    return
  }

  console.log('[QueryServiceForm] Detecting SQL spatial fields...', {
    engine_id: form.engine_id,
    sql: form.sql_query
  })

  detectingSQLSpatial.value = true
  try {
    const response = await queryServiceAPI.detectSQLOutputContract({
      engine_id: form.engine_id,
      sql: form.sql_query
    })

    console.log('[QueryServiceForm] Detection response:', response)

	  sqlOutputContract.value = response
	  const spatial = response?.spatial
	  const columns = spatial?.geometry_columns || []
	  const primary = columns.find(column => column.name === spatial?.primary_geometry_column) || (columns.length === 1 ? columns[0] : null)

    if (primary?.name) {
      // 自动填充空间字段信息
      sqlHasGeometry.value = true
      sqlGeometryColumn.value = primary.name
      sqlSrid.value = primary.srid || spatial.srid || 0
      sqlGeometryType.value = primary.geometry_type || ''

	  ElMessage.success(t('service.query.detectSpatialSuccess', { column: primary.name, srid: sqlSrid.value || '-' }))
    } else {
      // 未检测到空间字段
      sqlHasGeometry.value = false
      sqlGeometryColumn.value = ''
	  sqlSrid.value = 0
      sqlGeometryType.value = ''

      ElMessage.info(t('service.query.detectSpatialNone'))
    }
  } catch (error) {
    console.error('[QueryServiceForm] SQL spatial detection failed:', error)
    ElMessage.warning(t('service.query.detectSpatialFailed') + ': ' + (error.message || error.response?.data?.error || t('service.common.unknownError')))
  } finally {
    detectingSQLSpatial.value = false
  }
}

// 方法：处理表选择（ResourceTreePicker 回调）
const handleTableSelection = async (selection) => {
  console.log('[QueryServiceForm] Table selection:', selection)

  if (!selection) {
    // 清空选择
    form.engine_id = null
    form.schema_name = ''
    form.table_name = ''
    form.locator = ''
    spatialMetadata.value = null
    return
  }

  const path = locatorPathFromSelection(selection)
  const schemaName = path[0] || ''
  const tableName = path[path.length - 1] || selection.display?.label || ''

  // 更新表单字段
  form.engine_id = selection.identity?.engine_id
  form.schema_name = schemaName
  form.table_name = tableName
  form.locator = selection.identity?.locator || ''

  const geometry = await detectTableMetadata('/api/v1/meta', {
    locator: form.locator,
    item_id: selection.identity?.item_id
  })

  // 如果检测到几何列，自动启用 OGC Features
  if (geometry.has_geometry) {
    spatialMetadata.value = {
      hasGeometry: true,
      geometryColumn: geometry.geometry_column,
      srid: geometry.srid,
      geometryTypes: geometry.geometry_types || [],
      extent: geometry.extent
    }
    enableOgcFeatures.value = true
    ElMessage.success(t('service.query.detectSpatialSuccess', { column: geometry.geometry_column, srid: geometry.srid }))
  } else {
    spatialMetadata.value = { hasGeometry: false }
    enableOgcFeatures.value = false
  }
}

const onEngineChange = () => {
  form.schema_name = ''
  form.table_name = ''
  spatialMetadata.value = null
}

// 方法：步骤控制
const nextStep = () => {
  if (canProceed.value) {
    currentStep.value++
  }
}

const prevStep = () => {
  if (currentStep.value > 0) {
    currentStep.value--
  }
}

// 方法：关键词管理
const removeKeyword = (tag) => {
  form.keywords = form.keywords.filter(k => k !== tag)
}

const showInput = () => {
  inputVisible.value = true
  nextTick(() => {
    inputRef.value?.focus()
  })
}

const handleInputConfirm = () => {
  if (inputValue.value && !form.keywords.includes(inputValue.value)) {
    form.keywords.push(inputValue.value)
  }
  inputVisible.value = false
  inputValue.value = ''
}

const buildSQLOutputContract = () => {
	const table = sqlOutputContract.value?.table || null
	if (!sqlHasGeometry.value || !sqlGeometryColumn.value.trim()) {
	  return table ? { table } : null
	}
	const srid = Number(sqlSrid.value) > 0 ? Number(sqlSrid.value) : 0
	const crsRef = srid > 0 ? `EPSG:${srid}` : ''
	const existingSpatial = sqlOutputContract.value?.spatial || {}
	const definitions = (existingSpatial.crs_definitions || []).filter(definition => definition?.id === crsRef)
	const geometryColumn = {
	  name: sqlGeometryColumn.value.trim(),
	  geometry_type: sqlGeometryType.value || 'Geometry'
	}
	if (srid > 0) {
	  geometryColumn.srid = srid
	  geometryColumn.crs_ref = crsRef
	}
	const spatial = {
	  geometry_columns: [geometryColumn],
	  primary_geometry_column: geometryColumn.name
	}
	if (definitions.length > 0) spatial.crs_definitions = definitions
	if (existingSpatial.extent) spatial.extent = existingSpatial.extent
	return { ...(table ? { table } : {}), spatial }
}

// 方法：提交表单
const handleSubmit = async () => {
  submitting.value = true
  try {
    // 构建请求数据
    const requestData = {
      service_name: form.service_name,
      title: form.title,
      description: form.description,
      keywords: form.keywords,
      config_type: form.config_type,
      // DuckDB 虚拟引擎时 engine_id 为 null
      engine_id: form.engine_id !== null ? form.engine_id : undefined,
      public_access: form.public_access,
      max_features: form.max_features
    }

    // Table 模式特有字段
    if (form.config_type === 'table') {
      requestData.schema_name = form.schema_name
      requestData.table_name = form.table_name

      // 构建 data_config
      const dataConfig = {}
	  if (!isEdit.value && form.locator) {
        dataConfig.locator = form.locator
      }

      // 默认字段
      if (defaultFieldsInput.value.trim()) {
        dataConfig.default_fields = defaultFieldsInput.value.split(',').map(f => f.trim())
      }

      // 可过滤字段
      if (filterableFieldsInput.value.trim()) {
        dataConfig.filterable_fields = filterableFieldsInput.value.split(',').map(f => f.trim())
      }

      if (Object.keys(dataConfig).length > 0) {
        requestData.data_config = dataConfig
      }
    }
    // SQL 模式特有字段
    else {
      requestData.sql_query = form.sql_query

	  if (!isEdit.value) {
		const outputContract = buildSQLOutputContract()
		if (outputContract) requestData.output_contract = outputContract
	  }
    }

    // 协议配置
    requestData.protocols = {
      rest_api: { enabled: true },
      ogc_features: { enabled: enableOgcFeatures.value }
    }

    // 提交
    if (isEdit.value) {
      await queryServiceAPI.updateService(route.params.id, requestData)
      ElMessage.success(t('service.query.updateSuccess'))
    } else {
      await queryServiceAPI.createService(requestData)
      ElMessage.success(t('service.query.createSuccess'))
    }

    await navigateServiceRoute(router, '/query-services', { history: 'replace' })
  } catch (error) {
    ElMessage.error(t('service.query.submitFailed') + ': ' + (error.message || t('service.common.unknownError')))
    console.error('Failed to submit:', error)
  } finally {
    submitting.value = false
  }
}

// 方法：返回列表
const goBack = () => {
  navigateServiceRoute(router, '/query-services', { history: 'replace' })
}

// 生命周期：加载编辑数据
onMounted(async () => {
  // 加载存储引擎列表（SQL 模式下使用）
  try {
    const response = await queryServiceAPI.getStorageEngines()
    engines.value = response
  } catch (error) {
    console.error('[QueryServiceForm] 加载存储引擎失败:', error)
    ElMessage.warning(t('service.query.loadEnginesFailed'))
  }

  if (isEdit.value) {
    loading.value = true
    try {
      const service = await queryServiceAPI.getService(route.params.id)
      console.log('[QueryServiceForm] 编辑模式：加载服务数据', service)

      // 填充表单（编辑模式只能修改服务信息，不能修改数据源）
      Object.assign(form, {
        service_name: service.service_name,
        title: service.title,
        description: service.description,
        keywords: service.keywords || [],
        public_access: service.public_access,
        max_features: service.max_features
      })

      // 填充数据源配置（用于显示和检测空间字段）
      // 注意：数据源配置字段直接在服务对象顶层，不是嵌套在 source_config 下
      form.config_type = service.config_type || 'table'
      form.engine_id = service.engine_id
      form.schema_name = service.schema_name || ''
      form.table_name = service.table_name || ''
      form.locator = service.data_config?.locator || ''
      form.sql_query = service.sql_query || ''

      console.log('[QueryServiceForm] 编辑模式：数据源配置', {
        config_type: form.config_type,
        engine_id: form.engine_id,
        schema_name: form.schema_name,
        table_name: form.table_name
      })

	  const snapshot = service.data_config?.source_snapshot
	  const spatial = snapshot?.spatial
	  const columns = spatial?.geometry_columns || []
	  const primary = columns.find(column => column.name === spatial?.primary_geometry_column) || (columns.length === 1 ? columns[0] : null)
	  if (form.config_type === 'table') {
		spatialMetadata.value = primary ? {
		  hasGeometry: true,
		  geometryColumn: primary.name,
		  srid: primary.srid || spatial.srid || 0,
		  geometryTypes: primary.geometry_type ? [primary.geometry_type] : [],
		  extent: spatial.extent || null
		} : { hasGeometry: false }
	  } else {
		sqlOutputContract.value = { table: snapshot?.table || null, spatial: spatial || null }
		sqlHasGeometry.value = !!primary
		sqlGeometryColumn.value = primary?.name || ''
		sqlSrid.value = primary?.srid || spatial?.srid || 0
		sqlGeometryType.value = primary?.geometry_type || ''
	  }

      // 协议配置
      if (service.protocols?.ogc_features?.enabled) {
        enableOgcFeatures.value = true
      }
    } catch (error) {
      ElMessage.error('加载服务失败: ' + (error.message || '未知错误'))
    } finally {
      loading.value = false
    }
  }
})
</script>

<style scoped>
.query-service-form {
  padding: 20px;
  max-width: 1200px;
  margin: 0 auto;
}

.page-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 24px;
  padding-bottom: 16px;
  border-bottom: 1px solid #ebeef5;
}

.page-header h2 {
  margin: 0;
  font-size: 24px;
  color: var(--addp-text-primary);
}

.config-step-card {
  overflow: visible;
}

.config-step-card :deep(.el-card__body) {
  overflow: visible;
  height: auto;
  max-height: none;
}

.config-radio-group {
  display: flex;
  flex-direction: row;
  gap: 20px;
  width: 100%;
}

.config-card-wrapper {
  flex: 1;
  border: 2px solid var(--addp-border-color);
  border-radius: 8px;
  padding: 20px;
  cursor: pointer;
  transition: all 0.3s;
  background: var(--addp-bg-primary);
  display: flex;
  flex-direction: column;
}

.config-card-wrapper:hover {
  border-color: var(--el-color-primary);
  box-shadow: 0 2px 12px 0 rgba(64, 158, 255, 0.2);
}

.config-card-wrapper.selected {
  border-color: var(--el-color-primary);
  background-color: var(--el-color-primary-light-9);
  box-shadow: 0 2px 12px 0 rgba(64, 158, 255, 0.3);
}

.config-radio {
  width: 100%;
  display: flex;
  align-items: flex-start;
  margin: 0 !important;
  height: auto;
}

.config-radio :deep(.el-radio__input) {
  margin-top: 2px;
  flex-shrink: 0;
}

.config-radio :deep(.el-radio__label) {
  width: 100%;
  padding-left: 12px;
  white-space: normal;
  overflow: visible;
  text-overflow: clip;
}

.config-content {
  width: 100%;
  overflow: visible;
}

.config-content h3 {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 12px 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.config-content .description {
  margin: 0 0 12px 0;
  font-size: 14px;
  color: var(--addp-text-secondary);
  line-height: 1.6;
}

.config-content .features {
  margin-bottom: 12px;
}

.config-content .features p {
  margin: 6px 0;
  font-size: 13px;
  color: var(--addp-text-secondary);
  line-height: 1.5;
}

.help-text {
  margin-top: 4px;
  font-size: 13px;
  color: var(--addp-text-tertiary);
}

.keyword-input {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
}

.button-group {
  display: flex;
  gap: 12px;
  justify-content: center;
  margin-top: 30px;
  padding-top: 20px;
  border-top: 1px solid #ebeef5;
}
</style>
