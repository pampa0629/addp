<template>
  <div class="query-service-form" v-loading="loading">
    <div class="page-header">
      <el-button @click="goBack" :icon="ArrowLeft" circle />
      <h2>{{ isEdit ? '编辑查询服务' : '创建查询服务' }}</h2>
    </div>

    <!-- 步骤条（仅新建模式显示） -->
    <el-steps
      v-if="!isEdit"
      :active="currentStep"
      finish-status="success"
      align-center
      style="margin-bottom: 30px"
    >
      <el-step title="选择配置方式" />
      <el-step title="配置数据源" />
      <el-step title="配置服务信息" />
    </el-steps>

    <!-- Step 0: 选择配置方式（仅新建模式） -->
    <div v-if="!isEdit && currentStep === 0">
      <el-card class="config-step-card">
        <template #header>
          <span>选择数据源配置方式</span>
        </template>

        <el-radio-group v-model="form.config_type" class="config-radio-group">
          <div
            class="config-card-wrapper"
            :class="{ 'selected': form.config_type === 'table' }"
            @click="form.config_type = 'table'"
          >
            <el-radio value="table" class="config-radio">
              <div class="config-content">
                <h3><el-icon><Grid /></el-icon> 表配置模式（推荐新手）</h3>
                <p class="description">通过界面选择数据表，系统自动检测空间字段和元数据</p>
                <div class="features">
                  <p>✅ 支持动态查询参数（filter、fields、orderBy）</p>
                  <p>✅ 可选择默认返回字段和可过滤字段</p>
                  <p>✅ 自动检测空间字段并启用 OGC Features</p>
                </div>
                <el-tag type="success" size="small">适用场景：单表查询、快速发布数据服务</el-tag>
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
                <h3><el-icon><Document /></el-icon> SQL 配置模式（适合高级用户）</h3>
                <p class="description">编写自定义 SQL 查询语句，灵活控制数据输出</p>
                <div class="features">
                  <p>✅ 支持复杂查询（JOIN、子查询、聚合）</p>
                  <p>✅ 支持计算字段和自定义过滤逻辑</p>
                  <p>⚠️ 仅支持分页参数（page、page_size、format）</p>
                </div>
                <el-tag type="warning" size="small">适用场景：复杂业务查询、多表关联、数据计算</el-tag>
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
            <span>选择数据表</span>
          </template>

          <DataSourceCascader
            :api-base-url="metaApiBaseUrl"
            :engine-types="['postgresql', 'mysql', 'doris', 'clickhouse']"
            :selectable-node-types="['table']"
            :enable-geometry-detection="true"
            :require-geometry="false"
            :show-selection-info="true"
            @update:selection="handleTableSelection"
          />

          <el-form :model="form" label-width="120px" style="margin-top: 16px">

            <!-- 字段配置（可选） -->
            <el-divider content-position="left">字段配置（可选）</el-divider>

            <el-form-item label="默认返回字段">
              <el-input
                v-model="defaultFieldsInput"
                placeholder="留空返回所有字段，或输入字段列表（用逗号分隔）"
                style="width: 100%"
              />
              <div class="help-text">
                示例：id,name,category,geom（未配置则返回所有字段，API 可通过 fields 参数覆盖）
              </div>
            </el-form-item>

            <el-form-item label="可过滤字段">
              <el-input
                v-model="filterableFieldsInput"
                placeholder="留空允许过滤所有字段，或输入允许过滤的字段（用逗号分隔）"
                style="width: 100%"
              />
              <div class="help-text">
                示例：name,category（未配置则允许过滤所有字段）
              </div>
            </el-form-item>
          </el-form>
        </el-card>
      </div>

      <!-- SQL 模式 -->
      <div v-else-if="form.config_type === 'sql'">
        <el-card>
          <template #header>
            <span>编写 SQL 查询</span>
          </template>

          <el-form :model="form" label-width="120px">
            <el-form-item label="存储引擎" required>
              <el-select v-model="form.engine_id" placeholder="请选择存储引擎" style="width: 400px">
                <el-option
                  v-for="engine in sqlSupportedEngines"
                  :key="engine.id"
                  :label="`${engine.name} (${engine.engine_type})`"
                  :value="engine.id"
                />
              </el-select>
              <div class="help-text">
                仅显示支持 SQL 查询的存储引擎
              </div>
            </el-form-item>

            <el-form-item label="SQL 查询语句" required>
              <el-input
                v-model="form.sql_query"
                type="textarea"
                :rows="10"
                placeholder="示例：&#10;SELECT id, name, ST_AsGeoJSON(geom) as geometry&#10;FROM cities&#10;WHERE population > 1000000"
                style="font-family: 'Courier New', monospace"
              />
              <div class="help-text">
                ⚠️ SQL 语句固定，仅支持分页参数（page、page_size），不支持动态 filter/fields/orderBy
              </div>
            </el-form-item>

            <el-divider content-position="left">空间字段配置（可选）</el-divider>

            <el-form-item>
              <el-button
                type="primary"
                :loading="detectingSQLSpatial"
                :disabled="!form.engine_id || !form.sql_query"
                @click="detectSQLSpatialFields"
              >
                检测空间字段
              </el-button>
              <div class="help-text">
                自动检测 SQL 查询结果是否包含空间字段，并填充几何列配置
              </div>
            </el-form-item>

            <el-form-item label="包含空间字段">
              <el-checkbox v-model="sqlHasGeometry">
                SQL 查询结果包含空间字段
              </el-checkbox>
            </el-form-item>

            <div v-if="sqlHasGeometry">
              <el-form-item label="几何列名" required>
                <el-input v-model="sqlGeometryColumn" placeholder="例如: geometry" style="width: 300px" />
                <div class="help-text">
                  SQL 查询返回的几何列名称（必须在 SELECT 中）
                </div>
              </el-form-item>

              <el-form-item label="坐标系 (SRID)" required>
                <el-input-number v-model="sqlSrid" :min="0" :max="999999" placeholder="例如: 4326" />
                <div class="help-text">
                  空间坐标系统标识符（如 4326 = WGS84）
                </div>
              </el-form-item>

              <el-form-item label="几何类型" v-if="sqlGeometryType">
                <el-tag>{{ sqlGeometryType }}</el-tag>
                <div class="help-text">
                  检测到的几何类型
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
        <el-card header="基本信息" style="margin-bottom: 20px">
          <el-form-item label="配置方式" v-if="!isEdit">
            <el-tag :type="form.config_type === 'table' ? 'success' : 'warning'" size="large">
              {{ form.config_type === 'table' ? '表配置' : 'SQL配置' }}
            </el-tag>
            <span style="margin-left: 12px; color: var(--addp-text-tertiary); font-size: 13px">
              （配置方式创建后不可变更）
            </span>
          </el-form-item>

          <el-form-item label="服务名称" prop="service_name" required>
            <el-input
              v-model="form.service_name"
              placeholder="例如: beijing_poi"
              :disabled="isEdit"
              style="width: 400px"
            />
            <div class="help-text">
              仅支持英文、数字和下划线，用于服务 URL
            </div>
          </el-form-item>

          <el-form-item label="服务标题" prop="title" required>
            <el-input v-model="form.title" placeholder="例如: 北京POI数据查询" style="width: 400px" />
          </el-form-item>

          <el-form-item label="服务描述">
            <el-input
              type="textarea"
              v-model="form.description"
              :rows="3"
              placeholder="服务的简要描述"
            />
          </el-form-item>

          <el-form-item label="关键词">
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
                + 添加关键词
              </el-button>
            </div>
          </el-form-item>
        </el-card>

        <!-- 协议配置 -->
        <el-card header="协议配置" style="margin-bottom: 20px">
          <el-alert type="info" :closable="false" style="margin-bottom: 16px">
            REST API 默认启用。检测到空间字段时，OGC API Features 会自动启用。
          </el-alert>

          <el-form-item label="REST API">
            <el-checkbox v-model="enableRestApi" disabled>
              启用 REST API（默认启用）
            </el-checkbox>
            <div class="help-text">
              提供 JSON/CSV/GeoJSON 格式的查询接口
            </div>
          </el-form-item>

          <el-form-item label="OGC Features">
            <el-checkbox v-model="enableOgcFeatures" :disabled="!hasGeometryField">
              启用 OGC API Features
            </el-checkbox>
            <div class="help-text">
              {{ hasGeometryField ? '检测到空间字段，自动启用（可手动禁用）' : '未检测到空间字段，无法启用' }}
            </div>
          </el-form-item>
        </el-card>

        <!-- 访问控制 -->
        <el-card header="访问控制" style="margin-bottom: 20px">
          <el-form-item label="公开访问">
            <el-checkbox v-model="form.public_access">
              允许公开访问（无需 JWT 认证）
            </el-checkbox>
            <div class="help-text">
              启用后，任何人都可以访问查询端点
            </div>
          </el-form-item>

          <el-form-item label="最大返回数" prop="max_features">
            <el-input-number v-model="form.max_features" :min="1" :max="10000" />
            <div class="help-text">单次查询返回的最大记录数量</div>
          </el-form-item>
        </el-card>
      </el-form>
    </div>

    <!-- 操作按钮 -->
    <div class="button-group">
      <el-button v-if="!isEdit && currentStep > 0" @click="prevStep">
        上一步
      </el-button>
      <el-button
        v-if="!isEdit && currentStep < 2"
        type="primary"
        :disabled="!canProceed"
        @click="nextStep"
      >
        下一步
      </el-button>
      <el-button
        v-if="isEdit || currentStep === 2"
        type="primary"
        @click="handleSubmit"
        :loading="submitting"
      >
        {{ isEdit ? '更新' : '创建' }}
      </el-button>
      <el-button @click="goBack">
        取消
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Grid, Document, Search } from '@element-plus/icons-vue'
import queryServiceAPI from '@/api/queryService'
import { DataSourceCascader, detectTableMetadata } from '@common-ui'

const router = useRouter()
const route = useRoute()

const loading = ref(false)
const submitting = ref(false)
const currentStep = ref(0)
const detecting = ref(false)
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

// Service API 基础 URL（用于 DataSourceCascader）
// 通过 Service 后端统一访问数据源信息
const metaApiBaseUrl = computed(() => {
  return '/api/service'
})

// 空间元数据
const spatialMetadata = ref(null)

// SQL 模式的空间字段配置
const sqlHasGeometry = ref(false)
const sqlGeometryColumn = ref('')
const sqlSrid = ref(4326)
const sqlGeometryType = ref('')

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
const rules = {
  service_name: [
    { required: true, message: '请输入服务名称', trigger: 'blur' },
    { pattern: /^[a-z0-9_]+$/, message: '仅支持小写英文、数字和下划线', trigger: 'blur' }
  ],
  title: [
    { required: true, message: '请输入服务标题', trigger: 'blur' }
  ],
  max_features: [
    { required: true, message: '请设置最大返回数', trigger: 'blur' },
    { type: 'number', min: 1, max: 10000, message: '范围: 1-10000', trigger: 'blur' }
  ]
}

// 计算属性：是否检测到空间字段
const hasGeometryField = computed(() => {
  if (form.config_type === 'table') {
    return spatialMetadata.value?.hasGeometry === true
  } else {
    return sqlHasGeometry.value
  }
})

// 计算属性：支持 SQL 的存储引擎（过滤掉对象存储）
const sqlSupportedEngines = computed(() => {
  const supportedTypes = ['postgresql', 'mysql', 'doris', 'clickhouse', 'mongodb', 'spark']
  return engines.value.filter(engine => {
    const engineType = engine.engine_type?.toLowerCase() || ''
    return supportedTypes.includes(engineType)
  })
})

// 计算属性：是否可以进入下一步
const canProceed = computed(() => {
  if (currentStep.value === 0) {
    return !!form.config_type
  } else if (currentStep.value === 1) {
    if (form.config_type === 'table') {
      return form.engine_id && form.schema_name && form.table_name
    } else {
      return form.engine_id && form.sql_query
    }
  }
  return true
})

// 方法：检测空间字段
const detectSpatialFields = async () => {
  detecting.value = true
  try {
    // TODO: 调用 Meta 模块的 API 检测空间字段
    // const response = await metaAPI.getTableSpatialMetadata(form.engine_id, form.schema_name, form.table_name)

    // 临时模拟数据
    await new Promise(resolve => setTimeout(resolve, 1000))
    spatialMetadata.value = {
      hasGeometry: true,
      geometryColumn: 'geom',
      srid: 4326,
      geometryTypes: ['Point'],
      extent: { minX: 116.0, minY: 39.0, maxX: 117.0, maxY: 40.0 }
    }

    if (spatialMetadata.value.hasGeometry) {
      enableOgcFeatures.value = true
      ElMessage.success('检测到空间字段，已自动启用 OGC API Features')
    } else {
      ElMessage.info('未检测到空间字段')
    }
  } catch (error) {
    ElMessage.warning('空间字段检测失败: ' + (error.message || '未知错误'))
    spatialMetadata.value = { hasGeometry: false }
  } finally {
    detecting.value = false
  }
}

// 方法：检测 SQL 查询结果的空间字段
const detectSQLSpatialFields = async () => {
  if (!form.engine_id || !form.sql_query) {
    ElMessage.warning('请先选择存储引擎并输入 SQL 查询')
    return
  }

  console.log('[QueryServiceForm] Detecting SQL spatial fields...', {
    engine_id: form.engine_id,
    sql: form.sql_query
  })

  detectingSQLSpatial.value = true
  try {
    const response = await queryServiceAPI.detectSQLSpatialMetadata({
      engine_id: form.engine_id,
      sql: form.sql_query
    })

    console.log('[QueryServiceForm] Detection response:', response)

    if (response.has_geometry) {
      // 自动填充空间字段信息
      sqlHasGeometry.value = true
      sqlGeometryColumn.value = response.geometry_column
      sqlSrid.value = response.srid || 4326
      sqlGeometryType.value = response.geometry_type || ''

      ElMessage.success(`检测到空间字段: ${response.geometry_column} (SRID: ${response.srid})`)
    } else {
      // 未检测到空间字段
      sqlHasGeometry.value = false
      sqlGeometryColumn.value = ''
      sqlSrid.value = 4326
      sqlGeometryType.value = ''

      ElMessage.info('未检测到空间字段')
    }
  } catch (error) {
    console.error('[QueryServiceForm] SQL spatial detection failed:', error)
    ElMessage.warning('空间字段检测失败: ' + (error.message || error.response?.data?.error || '未知错误'))
  } finally {
    detectingSQLSpatial.value = false
  }
}

// 方法：处理表选择（DataSourceCascader 回调）
const handleTableSelection = (selection) => {
  console.log('[QueryServiceForm] Table selection:', selection)

  if (!selection) {
    // 清空选择
    form.engine_id = null
    form.schema_name = ''
    form.table_name = ''
    spatialMetadata.value = null
    return
  }

  // 更新表单字段
  form.engine_id = selection.engineId
  form.schema_name = selection.schema
  form.table_name = selection.tableName

  // 如果检测到几何列,自动启用 OGC Features
  if (selection.hasGeometry) {
    spatialMetadata.value = {
      hasGeometry: true,
      geometryColumn: selection.geometryColumn,
      srid: selection.srid,
      geometryTypes: selection.geometryType ? [selection.geometryType] : [],
      extent: selection.extent
    }
    enableOgcFeatures.value = true
    ElMessage.success(`检测到空间字段: ${selection.geometryColumn}`)
  } else {
    spatialMetadata.value = { hasGeometry: false }
    enableOgcFeatures.value = false
  }
}

// 方法：处理几何检测结果（DataSourceSelector 回调）
const handleGeometryDetected = (result) => {
  console.log('[QueryServiceForm] Geometry detected:', result)
}

// 方法：引擎变更（已被 DataSourceSelector 替代，不再需要）
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
      engine_id: form.engine_id,
      public_access: form.public_access,
      max_features: form.max_features
    }

    // Table 模式特有字段
    if (form.config_type === 'table') {
      requestData.schema_name = form.schema_name
      requestData.table_name = form.table_name

      // 构建 data_config
      const dataConfig = {}

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

      // 如果有空间字段配置
      if (sqlHasGeometry.value && sqlGeometryColumn.value) {
        requestData.data_config = {
          geometry: {
            has_geometry: true,
            column: sqlGeometryColumn.value,
            srid: sqlSrid.value
          }
        }
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
      ElMessage.success('查询服务已更新')
    } else {
      await queryServiceAPI.createService(requestData)
      ElMessage.success('查询服务已创建')
    }

    router.push('/query-services')
  } catch (error) {
    ElMessage.error((isEdit.value ? '更新' : '创建') + '失败: ' + (error.message || '未知错误'))
    console.error('Failed to submit:', error)
  } finally {
    submitting.value = false
  }
}

// 方法：返回列表
const goBack = () => {
  router.push('/query-services')
}

// 生命周期：加载编辑数据
onMounted(async () => {
  // 加载存储引擎列表（SQL 模式下使用）
  try {
    const response = await queryServiceAPI.getStorageEngines()
    engines.value = response?.data || []
  } catch (error) {
    console.error('[QueryServiceForm] 加载存储引擎失败:', error)
    ElMessage.warning('加载存储引擎失败，SQL 模式可能无法使用')
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
      form.sql_query = service.sql_query || ''

      console.log('[QueryServiceForm] 编辑模式：数据源配置', {
        config_type: form.config_type,
        engine_id: form.engine_id,
        schema_name: form.schema_name,
        table_name: form.table_name
      })

      // 如果是 table 模式且有完整的数据源信息，重新检测空间字段
      if (form.config_type === 'table' && form.engine_id && form.schema_name && form.table_name) {
        try {
          console.log('[QueryServiceForm] 编辑模式：开始检测空间字段...')
          const geometryInfo = await detectTableMetadata(metaApiBaseUrl.value, {
            engine_id: form.engine_id,
            schema: form.schema_name,
            table: form.table_name
          })

          console.log('[QueryServiceForm] 编辑模式：检测结果', geometryInfo)

          if (geometryInfo.has_geometry) {
            spatialMetadata.value = {
              hasGeometry: true,
              geometryColumn: geometryInfo.geometry_column,
              srid: geometryInfo.srid,
              geometryTypes: geometryInfo.geometry_types || [],
              extent: geometryInfo.extent
            }
            console.log('[QueryServiceForm] 编辑模式：检测到空间字段', geometryInfo.geometry_column)
          } else {
            spatialMetadata.value = { hasGeometry: false }
            console.log('[QueryServiceForm] 编辑模式：未检测到空间字段')
          }
        } catch (error) {
          console.error('[QueryServiceForm] 编辑模式：空间字段检测失败:', error)
          spatialMetadata.value = { hasGeometry: false }
        }
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
  border: 2px solid #dcdfe6;
  border-radius: 8px;
  padding: 20px;
  cursor: pointer;
  transition: all 0.3s;
  background: var(--addp-bg-primary);
  display: flex;
  flex-direction: column;
  min-height: 100%;
}

.config-card-wrapper:hover {
  border-color: #409eff;
  box-shadow: 0 2px 12px 0 rgba(64, 158, 255, 0.2);
}

.config-card-wrapper.selected {
  border-color: #409eff;
  background-color: #f0f7ff;
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
