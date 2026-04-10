<template>
  <div class="published-service-form" v-loading="loading">
    <div class="page-header">
      <h2>{{ isEdit ? t('service.published.formEditTitle') : t('service.published.formCreateTitle') }}</h2>
    </div>

    <!-- 步骤条（仅新建模式显示） -->
    <el-steps
      v-if="!isEdit"
      :active="currentStep"
      finish-status="success"
      align-center
      style="margin-bottom: 30px"
    >
      <el-step :title="t('service.published.stepSelectTable')" />
      <el-step :title="t('service.published.stepConfirmType')" />
      <el-step :title="t('service.published.stepConfigService')" />
    </el-steps>

    <!-- Step 0: 选择数据表（仅新建模式） -->
    <div v-if="!isEdit && currentStep === 0">
      <el-card>
        <template #header>
          <span>{{ t('service.published.selectTableTitle') }}</span>
        </template>

        <el-button type="primary" size="large" @click="showTableSelector = true">
          <el-icon style="margin-right: 8px"><FolderOpened /></el-icon>
          {{ t('service.published.selectTableBtn') }}
        </el-button>

        <el-alert
          v-if="selectedTable"
          type="success"
          :closable="false"
          style="margin-top: 20px"
        >
          <template #title>
            <div>
              {{ t('service.published.selectedTable') }}<strong>{{ selectedTable.fullName }}</strong>
            </div>
            <div v-if="selectedTable.hasGeometry" style="margin-top: 8px; font-size: 13px">
              <el-icon><Location /></el-icon>
              {{ t('service.published.geometryColumn') }}{{ selectedTable.geometryColumn }}
              (SRID: {{ selectedTable.srid }})
            </div>
            <div v-else style="margin-top: 8px; font-size: 13px; color: var(--addp-text-tertiary)">
              {{ t('service.published.noGeometry') }}
            </div>
          </template>
        </el-alert>
      </el-card>
    </div>

    <!-- Step 1: 确认服务类型（仅新建模式） -->
    <div v-if="!isEdit && currentStep === 1">
      <el-card>
        <template #header>
          <span>{{ t('service.published.selectServiceTypeTitle') }}</span>
        </template>

        <el-alert
          v-if="selectedTable && selectedTable.hasGeometry"
          type="info"
          :closable="false"
          style="margin-bottom: 20px"
        >
          {{ t('service.published.hasGeometryAlert', { table: selectedTable.fullName, column: selectedTable.geometryColumn }) }}
        </el-alert>

        <el-radio-group v-model="form.service_type" style="width: 100%">
          <el-card
            v-if="selectedTable && selectedTable.hasGeometry"
            shadow="hover"
            :class="{ 'selected-card': form.service_type === 'spatial' }"
            style="margin-bottom: 16px; cursor: pointer"
            @click="form.service_type = 'spatial'"
          >
            <el-radio value="spatial" style="width: 100%">
              <div class="service-type-card">
                <h3><el-icon><MapLocation /></el-icon> {{ t('service.published.spatialServiceTitle') }}</h3>
                <p>{{ t('service.published.spatialServiceDesc1') }}</p>
                <p>{{ t('service.published.spatialServiceDesc2') }}</p>
                <p>{{ t('service.published.spatialServiceDesc3') }}</p>
                <el-tag type="success" size="small">{{ t('service.published.spatialServiceTag') }}</el-tag>
              </div>
            </el-radio>
          </el-card>

          <el-card
            shadow="hover"
            :class="{ 'selected-card': form.service_type === 'table' }"
            style="cursor: pointer"
            @click="form.service_type = 'table'"
          >
            <el-radio value="table" style="width: 100%">
              <div class="service-type-card">
                <h3><el-icon><Document /></el-icon> {{ t('service.published.tableServiceTitle') }}</h3>
                <p>{{ t('service.published.tableServiceDesc1') }}</p>
                <p>{{ t('service.published.tableServiceDesc2') }}</p>
                <p>{{ t('service.published.tableServiceDesc3') }}</p>
                <el-tag type="primary" size="small">{{ t('service.published.tableServiceTag') }}</el-tag>
              </div>
            </el-radio>
          </el-card>
        </el-radio-group>
      </el-card>
    </div>

    <!-- Step 2: 配置服务信息 -->
    <div v-if="isEdit || currentStep === 2">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="140px">
        <!-- 基本信息 -->
        <el-card :header="t('service.published.basicInfoTitle')" style="margin-bottom: 20px">
          <el-form-item :label="t('service.published.serviceTypeLabel')" v-if="!isEdit">
            <el-tag :type="form.service_type === 'spatial' ? 'success' : 'primary'" size="large">
              {{ form.service_type === 'spatial' ? t('service.published.spatialServiceType') : t('service.published.tableServiceType') }}
            </el-tag>
            <span style="margin-left: 12px; color: var(--addp-text-tertiary); font-size: 13px">
              {{ t('service.published.serviceTypeNote') }}
            </span>
          </el-form-item>

          <el-form-item :label="t('service.published.serviceNameLabel')" prop="service_name" required>
            <el-input
              v-model="form.service_name"
              :placeholder="t('service.published.serviceNamePlaceholder')"
              :disabled="isEdit"
            />
            <div class="help-text">
              {{ t('service.published.serviceNameHelp') }}
            </div>
          </el-form-item>

          <el-form-item :label="t('service.published.titleLabel')" prop="title" required>
            <el-input v-model="form.title" :placeholder="t('service.published.titlePlaceholder')" />
          </el-form-item>

          <el-form-item :label="t('service.published.abstractLabel')" prop="abstract">
            <el-input
              type="textarea"
              v-model="form.abstract"
              :rows="3"
              :placeholder="t('service.published.abstractPlaceholder')"
            />
          </el-form-item>

          <el-form-item :label="t('service.published.keywordsLabel')">
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

        <!-- 空间服务专属配置 -->
        <el-card v-if="form.service_type === 'spatial'" :header="t('service.published.spatialConfigTitle')" style="margin-bottom: 20px">
          <el-form-item :label="t('service.published.defaultSridLabel')" prop="default_srid" required>
            <el-select v-model="form.default_srid" style="width: 300px">
              <el-option :value="4326" label="EPSG:4326 (WGS84)" />
              <el-option :value="3857" label="EPSG:3857 (Web Mercator)" />
              <el-option :value="4490" label="EPSG:4490 (CGCS2000)" />
            </el-select>
          </el-form-item>

          <el-form-item :label="t('service.published.enabledProtocolsLabel')" v-if="!isEdit">
            <el-checkbox-group v-model="enabledProtocols">
              <div style="display: flex; flex-direction: column; gap: 12px">
                <el-checkbox value="wfs">
                  <strong>WFS 2.0</strong>
                  <span style="color: var(--addp-text-tertiary); margin-left: 8px">
                    {{ t('service.published.wfsDesc') }}
                  </span>
                </el-checkbox>

                <el-checkbox value="wmts">
                  <strong>WMTS 1.0</strong>
                  <span style="color: var(--addp-text-tertiary); margin-left: 8px">
                    {{ t('service.published.wmtsDesc') }}
                  </span>
                </el-checkbox>

                <el-checkbox value="ogc_api">
                  <strong>OGC API Features</strong>
                  <span style="color: var(--addp-text-tertiary); margin-left: 8px">
                    {{ t('service.published.ogcApiDesc') }}
                  </span>
                </el-checkbox>

                <el-checkbox value="rest_query" checked disabled>
                  <strong>REST Query API</strong>
                  <span style="color: var(--addp-text-tertiary); margin-left: 8px">
                    {{ t('service.published.restQueryDesc') }}
                  </span>
                </el-checkbox>
              </div>
            </el-checkbox-group>
          </el-form-item>

          <el-form-item :label="t('service.published.maxFeaturesLabel')" prop="max_features">
            <el-input-number v-model="form.max_features" :min="1" :max="10000" />
            <div class="help-text">{{ t('service.published.maxFeaturesHelp') }}</div>
          </el-form-item>
        </el-card>

        <!-- 数据表服务专属配置 -->
        <el-card v-if="form.service_type === 'table'" :header="t('service.published.tableConfigTitle')" style="margin-bottom: 20px">
          <el-alert type="info" :closable="false" style="margin-bottom: 16px">
            {{ t('service.published.tableConfigAlert') }}
          </el-alert>

          <el-form-item :label="t('service.published.defaultPageSizeLabel')">
            <el-input-number v-model="form.default_page_size" :min="10" :max="100" />
          </el-form-item>

          <el-form-item :label="t('service.published.maxRecordsLabel')" prop="max_features">
            <el-input-number v-model="form.max_features" :min="1" :max="10000" />
            <div class="help-text">{{ t('service.published.maxRecordsHelp') }}</div>
          </el-form-item>
        </el-card>

        <!-- 通用配置 -->
        <el-card :header="t('service.published.accessControlTitle')" style="margin-bottom: 20px">
          <el-form-item :label="t('service.published.accessPermissionLabel')">
            <el-checkbox v-model="form.public_access">
              {{ t('service.published.publicAccessCheckbox') }}
            </el-checkbox>
            <div class="help-text">
              {{ t('service.published.publicAccessHelp') }}
            </div>
          </el-form-item>
        </el-card>

        <!-- 元数据（可选） -->
        <el-card :header="t('service.published.metadataTitle')" style="margin-bottom: 20px">
          <el-form-item :label="t('service.published.providerNameLabel')">
            <el-input v-model="form.provider_name" :placeholder="t('service.published.providerNamePlaceholder')" />
          </el-form-item>

          <el-form-item :label="t('service.published.providerSiteLabel')">
            <el-input v-model="form.provider_site" placeholder="https://example.com" />
          </el-form-item>

          <el-form-item :label="t('service.published.contactPersonLabel')">
            <el-input v-model="form.contact_person" :placeholder="t('service.published.contactPersonPlaceholder')" />
          </el-form-item>

          <el-form-item :label="t('service.published.contactEmailLabel')">
            <el-input
              v-model="form.contact_email"
              type="email"
              placeholder="example@example.com"
            />
          </el-form-item>
        </el-card>
      </el-form>
    </div>

    <!-- 操作按钮 -->
    <div class="button-group">
      <el-button v-if="!isEdit && currentStep > 0" @click="prevStep">
        {{ t('service.published.prevStep') }}
      </el-button>
      <el-button
        v-if="!isEdit && currentStep < 2"
        type="primary"
        :disabled="!canProceed"
        @click="nextStep"
      >
        {{ t('service.published.nextStep') }}
      </el-button>
      <el-button
        v-if="isEdit || currentStep === 2"
        type="primary"
        @click="handleSubmit"
        :loading="submitting"
      >
        {{ isEdit ? t('service.published.updateBtn') : t('service.published.createBtn') }}
      </el-button>
      <el-button @click="$router.back()">{{ t('service.common.cancel') }}</el-button>
    </div>

    <!-- 表选择器对话框 -->
    <table-selector
      v-model:visible="showTableSelector"
      @table-selected="handleTableSelected"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import {
  FolderOpened,
  Location,
  MapLocation,
  Document
} from '@element-plus/icons-vue'
import publishedServiceAPI from '../api/publishedService'
import TableSelector from '../components/TableSelector.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const formRef = ref(null)
const inputRef = ref(null)

const isEdit = computed(() => !!route.params.id)

const currentStep = ref(0)
const showTableSelector = ref(false)
const selectedTable = ref(null)
const enabledProtocols = ref(['wfs', 'wmts', 'ogc_api', 'rest_query'])

const form = ref({
  service_name: '',
  title: '',
  abstract: '',
  keywords: [],
  service_type: null,
  public_access: false,
  default_srid: 4326,
  max_features: 1000,
  default_page_size: 20,
  provider_name: '',
  provider_site: '',
  contact_person: '',
  contact_email: '',
  engine_id: null
})

const loading = ref(false)
const submitting = ref(false)
const inputVisible = ref(false)
const inputValue = ref('')

// 判断是否可以进入下一步
const canProceed = computed(() => {
  if (currentStep.value === 0) {
    return !!selectedTable.value
  }
  if (currentStep.value === 1) {
    return !!form.value.service_type
  }
  return true
})

// 表单验证规则
const rules = computed(() => ({
  service_name: [
    { required: true, message: t('service.published.serviceNameRequired'), trigger: 'blur' },
    {
      pattern: /^[a-zA-Z0-9_]+$/,
      message: t('service.published.serviceNameHelp'),
      trigger: 'blur'
    }
  ],
  title: [{ required: true, message: t('service.published.titleRequired'), trigger: 'blur' }],
  default_srid: [
    { required: true, message: t('service.published.defaultSridRequired'), trigger: 'change' }
  ],
  max_features: [
    { required: true, message: t('service.published.maxFeaturesRequired'), trigger: 'blur' },
    {
      type: 'number',
      min: 1,
      max: 10000,
      message: t('service.published.maxFeaturesRange'),
      trigger: 'blur'
    }
  ]
}))

// 处理表选择
const handleTableSelected = (table) => {
  selectedTable.value = table
  form.value.engine_id = table.engineId

  // 自动填充表单
  form.value.service_name = table.tableName
  form.value.title = table.tableName

  // 根据是否有几何列自动选择服务类型
  if (table.hasGeometry) {
    form.value.service_type = 'spatial'
    form.value.default_srid = table.srid || 4326
  } else {
    form.value.service_type = 'table'
  }
}

// 下一步
const nextStep = () => {
  if (!canProceed.value) {
    if (currentStep.value === 0) {
      ElMessage.warning(t('service.published.selectTableFirst'))
    } else if (currentStep.value === 1) {
      ElMessage.warning(t('service.published.selectTypeFirst'))
    }
    return
  }
  currentStep.value++
}

// 上一步
const prevStep = () => {
  if (currentStep.value > 0) {
    currentStep.value--
  }
}

// 关键词管理
const removeKeyword = (tag) => {
  form.value.keywords = form.value.keywords.filter((k) => k !== tag)
}

const showInput = () => {
  inputVisible.value = true
  nextTick(() => {
    inputRef.value?.focus()
  })
}

const handleInputConfirm = () => {
  const value = inputValue.value.trim()
  if (value && !form.value.keywords.includes(value)) {
    form.value.keywords.push(value)
  }
  inputValue.value = ''
  inputVisible.value = false
}

// 加载服务详情（编辑模式）
const loadService = async () => {
  if (!isEdit.value) return

  loading.value = true
  try {
    const service_data = await publishedServiceAPI.getService(route.params.id)
    Object.assign(form.value, service_data)

    // 确保 keywords 是数组
    if (!Array.isArray(form.value.keywords)) {
      form.value.keywords = []
    }
  } catch (error) {
    ElMessage.error(t('service.published.loadFailed') + ': ' + (error.message || t('service.common.unknownError')))
  } finally {
    loading.value = false
  }
}

// 提交表单
const handleSubmit = async () => {
  if (!formRef.value) return

  try {
    // 验证表单
    await formRef.value.validate()

    submitting.value = true

    if (isEdit.value) {
      // 编辑模式：不允许修改服务类型和协议配置
      const updateData = {
        title: form.value.title,
        abstract: form.value.abstract,
        keywords: form.value.keywords,
        public_access: form.value.public_access,
        max_features: form.value.max_features,
        provider_name: form.value.provider_name,
        provider_site: form.value.provider_site,
        contact_person: form.value.contact_person,
        contact_email: form.value.contact_email
      }

      if (form.value.service_type === 'spatial') {
        updateData.default_srid = form.value.default_srid
      }

      await publishedServiceAPI.updateService(route.params.id, updateData)
      ElMessage.success(t('service.published.updateSuccess'))
    } else {
      // 新建模式 - 适配新的 QueryService API
      const createData = {
        service_name: form.value.service_name,
        title: form.value.title,
        description: form.value.abstract || '',
        keywords: form.value.keywords || [],

        // 配置方式：当前只支持表模式
        config_type: 'table',

        // 存储引擎和表信息
        engine_id: form.value.engine_id,
        schema_name: selectedTable.value.schema,
        table_name: selectedTable.value.tableName,

        // 数据配置
        data_config: {
          geometry: selectedTable.value.hasGeometry ? {
            has_geometry: true,
            column: selectedTable.value.geometryColumn,
            srid: selectedTable.value.srid || 4326,
            types: selectedTable.value.geometryType
              ? [selectedTable.value.geometryType]
              : []
          } : {
            has_geometry: false
          }
        },

        // 协议配置
        protocols: {
          rest_api: {
            enabled: true,
            formats: ['json', 'csv', 'geojson']
          },
          ogc_features: {
            enabled: form.value.service_type === 'spatial',
            version: '1.0'
          }
        },

        // 访问控制
        public_access: form.value.public_access || false,
        max_features: form.value.max_features || 1000
      }

      const response = await publishedServiceAPI.createService(createData)
      ElMessage.success(t('service.published.createSuccess'))
      router.push(`/published-services/${response.id}`)
    }
  } catch (error) {
    if (error.message) {
      ElMessage.error(t('service.common.saveFailed') + ': ' + error.message)
    }
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  if (isEdit.value) {
    loadService()
  }
})
</script>

<style scoped>
.published-service-form {
  padding: 20px;
}

.page-header {
  margin-bottom: 20px;
}

.page-header h2 {
  margin: 0;
  font-size: 24px;
  color: var(--addp-text-primary);
}

.help-text {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-top: 4px;
  line-height: 1.5;
}

.keyword-input {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
}

.button-group {
  margin-top: 20px;
  text-align: center;
}

.service-type-card {
  padding: 12px 0;
}

.service-type-card h3 {
  margin: 0 0 12px 0;
  font-size: 16px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.service-type-card p {
  margin: 4px 0;
  font-size: 13px;
  color: var(--addp-text-secondary);
}

.selected-card {
  border-color: var(--el-color-primary);
  box-shadow: 0 2px 12px 0 rgba(64, 158, 255, 0.3);
}

:deep(.el-card__header) {
  font-weight: 600;
  font-size: 16px;
}

:deep(.el-radio) {
  width: 100%;
  margin-right: 0;
}
</style>
