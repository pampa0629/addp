<template>
  <div class="query-service-form" v-loading="loading" :inert="submitting || loading" :aria-busy="submitting || loading">
    <div class="page-header">
      <el-button @click="goBack" :icon="ArrowLeft" circle />
      <h2>{{ isEdit ? t('service.query.formEditTitle') : t('service.query.formCreateTitle') }}</h2>
    </div>

    <!-- 步骤条（仅新建模式显示） -->
    <el-alert v-if="versionConflict" type="warning" :closable="false" style="margin-bottom: 16px">
      {{ t('service.query.versionConflict') }}
      <el-button link type="primary" @click="reloadDefinition">{{ t('service.query.reloadDefinition') }}</el-button>
    </el-alert>
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
          <div class="config-card-wrapper" :class="{ selected: form.config_type === 'analytical' }" @click="form.config_type = 'analytical'">
            <el-radio value="analytical" class="config-radio">
              <div class="config-content">
                <h3><el-icon><Grid /></el-icon> {{ t('service.query.metricModeTitle') }}</h3>
                <p class="description">{{ t('service.query.metricModeDescription') }}</p>
                <el-tag size="small">{{ t('service.query.configTypeAnalytical') }}</el-tag>
              </div>
            </el-radio>
          </div>
        </el-radio-group>
      </el-card>
    </div>

    <!-- Step 1: 配置数据源 -->
    <div v-if="!isEdit && currentStep === 1">
      <el-card v-if="form.config_type === 'analytical'" :header="t('service.query.metricModeTitle')">
        <PublishedMetricSourcePicker v-model="metricSource" />
      </el-card>
      <!-- Table 模式 -->
      <div v-if="form.config_type === 'table'">
        <el-card>
          <template #header>
            <span>{{ t('service.query.selectTableTitle') }}</span>
          </template>

          <ResourceTreePicker
            ref="tablePickerRef"
            :api-base-url="metaApiBaseUrl"
            :engine-filter="queryTableEngineFilter"
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
            <el-form-item v-if="tableUsesRuntime" :label="t('service.query.runtimeEngineLabel')" required>
              <el-select
                v-model="form.runtime_engine_id"
                :placeholder="t('service.query.runtimeEnginePlaceholder')"
                :loading="loadingEngines"
                style="width: 400px"
                @visible-change="handleEngineDropdownVisible"
              >
                <el-option
                  v-for="engine in queryRuntimes"
                  :key="engine.id"
                  :label="engineOptionLabel(engine)"
                  :value="engine.id"
                  :disabled="!isEngineSelectable(engine)"
                />
              </el-select>
              <div class="help-text">{{ t('service.query.objectTableRuntimeHelp') }}</div>
            </el-form-item>

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
              <el-select
                v-model="form.execution_engine_id"
                :placeholder="t('service.query.enginePlaceholder')"
                style="width: 400px"
                :loading="loadingEngines || loadingSampleQuery"
                @change="handleSQLExecutionEngineChange"
                @visible-change="handleEngineDropdownVisible"
              >
                <el-option
                  v-for="engine in sqlSupportedEngines"
                  :key="engine.id"
                  :label="engineOptionLabel(engine)"
                  :value="engine.id"
                  :disabled="!isEngineSelectable(engine)"
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
                :placeholder="t('service.query.sqlPlaceholder')"
                :disabled="loadingSampleQuery"
				@input="resetSQLOutputContract"
                style="font-family: 'Courier New', monospace"
              />
              <div class="help-text">
                {{ t('service.query.sqlHelp') }}
              </div>
            </el-form-item>

            <el-form-item :label="t('service.query.namedParametersLabel')">
              <div class="named-parameters-editor">
                <div class="named-parameters-help">{{ t('service.query.namedParametersHelp') }}</div>
                <div v-for="(parameter, index) in sqlNamedParameters" :key="index" class="named-parameter-row">
                  <el-input
                    v-model="parameter.name"
                    :placeholder="t('service.query.namedParameterName')"
                    maxlength="64"
                    @input="resetSQLOutputContract"
                  />
                  <el-select v-model="parameter.type" @change="parameter.options = []; handleNamedParameterDefinitionChange(parameter)" class="named-parameter-type">
                    <el-option v-for="type in sqlNamedParameterTypes" :key="type" :label="type" :value="type" />
                  </el-select>
                  <el-checkbox v-model="parameter.required" @change="handleNamedParameterDefinitionChange(parameter)">
                    {{ t('service.query.namedParameterRequired') }}
                  </el-checkbox>
                  <ParameterValueInput v-model="parameter.value" :control-type="parameterControlType(parameter.type)" :options="parameter.options" @update:model-value="resetSQLOutputContract" />
                  <el-input
                    v-model="parameter.description"
                    :placeholder="t('service.query.namedParameterDescription')"
                    maxlength="500"
                  />
                  <el-button link type="danger" @click="removeSQLNamedParameter(index)">{{ t('service.common.delete') }}</el-button>
                  <div class="parameter-options-editor">
                    <div v-for="(option, optionIndex) in parameter.options" :key="optionIndex" class="parameter-option-row">
                      <ParameterValueInput v-model="option.value" :control-type="parameterControlType(parameter.type)" @update:model-value="resetSQLOutputContract" />
                      <el-input v-model="option.labels['zh-cn']" :placeholder="t('service.query.optionLabelZh')" maxlength="100" />
                      <el-input v-model="option.labels.en" :placeholder="t('service.query.optionLabelEn')" maxlength="100" />
                      <el-button link type="danger" @click="parameter.options.splice(optionIndex, 1); resetSQLOutputContract()">{{ t('service.common.delete') }}</el-button>
                    </div>
                    <el-button link type="primary" :disabled="parameter.options.length >= 100" @click="parameter.options.push({ value: null, labels: { 'zh-cn': '', en: '' } }); resetSQLOutputContract()">{{ t('service.query.addParameterOption') }}</el-button>
                  </div>
                </div>
                <el-button type="primary" plain @click="addSQLNamedParameter">
                  {{ t('service.query.addNamedParameter') }}
                </el-button>
              </div>
            </el-form-item>

			<el-form-item :label="t('service.query.stableKeyLabel')" required>
			  <el-select
				v-model="sqlStableKey"
				multiple
				filterable
				:filter-method="filterSQLStableKeyFields"
				:disabled="isEdit || !form.execution_engine_id || !form.sql_query.trim()"
				:loading="detectingSQLOutput"
				:loading-text="t('service.query.detectingOutputFields')"
				:placeholder="t('service.query.stableKeyPlaceholder')"
				@visible-change="handleSQLStableKeyVisibleChange"
				style="width: 100%"
			  >
				<el-option v-for="field in filteredSQLStableKeyFields" :key="field.name" :label="field.name" :value="field.name" />
			  </el-select>
			  <div class="help-text">{{ t('service.query.stableKeyHelp') }}</div>
			</el-form-item>

			<el-form-item :label="t('service.query.defaultFieldsLabel')">
			  <el-input v-model="defaultFieldsInput" :placeholder="t('service.query.defaultFieldsPlaceholder')" />
			  <div class="help-text">{{ t('service.query.defaultFieldsHelp') }}</div>
			</el-form-item>

			<el-form-item :label="t('service.query.filterableFieldsLabel')">
			  <el-input v-model="filterableFieldsInput" :placeholder="t('service.query.filterableFieldsPlaceholder')" />
			  <div class="help-text">{{ t('service.query.filterableFieldsHelp') }}</div>
			</el-form-item>

            <el-divider content-position="left">空间字段配置（可选）</el-divider>

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
              {{ form.config_type === 'table' ? t('service.query.configTypeTable') : form.config_type === 'analytical' ? t('service.query.configTypeAnalytical') : t('service.query.configTypeSql') }}
            </el-tag>
            <span style="margin-left: 12px; color: var(--addp-text-tertiary); font-size: 13px">
              {{ t('service.query.configTypeNote') }}
            </span>
          </el-form-item>

          <el-alert v-if="form.config_type === 'analytical'" :title="t('service.query.metricSourceHint')" type="info" :closable="false" style="margin-bottom: 16px" />
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
          <el-alert v-if="form.config_type !== 'analytical'" type="info" :closable="false" style="margin-bottom: 16px">
            {{ t('service.query.protocolAlert') }}
          </el-alert>

          <el-form-item :label="t('service.query.restApiLabel')">
            <el-checkbox v-model="enableRestApi" disabled>
              {{ t('service.query.restApiCheckbox') }}
            </el-checkbox>
            <div class="help-text">
              {{ form.config_type === 'analytical' ? t('service.query.metricRestApiHelp', { formats: metricRestFormats.join('/') }) : t('service.query.restApiHelp') }}
            </div>
          </el-form-item>

          <el-form-item v-if="form.config_type !== 'analytical'" :label="t('service.query.ogcFeaturesLabel')">
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
import PublishedMetricSourcePicker from '../components/PublishedMetricSourcePicker.vue'
import ParameterValueInput from '../../../../common-frontend/basic/src/components/ParameterValueInput.vue'
import { parameterControlType, parameterOptionsAllow, validParameterOptions } from '../../../../common-frontend/basic/src/utils/parameterInput.mjs'
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Grid, Document, Search } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import queryServiceAPI from '@/api/queryService'
import {
  ResourceTreePicker,
  createLatestRequestCoordinator,
  detectTableMetadata,
  engineSelectionState,
  isEngineSelectable,
  locatorPathFromSelection,
  withTransientRetry,
  useUnsavedChangesGuard
} from '@common-ui'
import {
  isQueryableTableEngine,
  isQueryableTableNode,
  isQueryableTableVisibleNode
} from '@/utils/resourceSelection'
import {
  applySQLExecutionEngine,
  federatedQueryRuntimes,
  queryServiceExecutionEngines,
  tableSelectionUsesRuntime
} from '@/utils/queryServiceEngines'
import { navigateServiceRoute } from '@/utils/moduleNavigation'
import { SERVICE_NAME_PATTERN } from '@/utils/serviceHelper'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()

const loading = ref(false)
const submitting = ref(false)
const currentStep = ref(0)
const detectingSQLOutput = ref(false)
const loadingSampleQuery = ref(false)
const loadingEngines = ref(false)
const sampleRequests = createLatestRequestCoordinator()
const outputContractRequests = createLatestRequestCoordinator()
const tablePickerRef = ref(null)
const tableMetadataRequests = createLatestRequestCoordinator()
let editorVersion = 0

const isEdit = computed(() => !!route.params.id)

// 表单数据
const versionConflict = ref(false)
const reloadDefinition = async () => {
  try {
    await ElMessageBox.confirm(t('service.query.reloadDiscardConfirm'), t('service.query.reloadDefinition'), {
      confirmButtonText: t('service.common.confirm'), cancelButtonText: t('service.common.cancel'), type: 'warning'
    })
    markSaved()
    window.location.reload()
  } catch { /* Keep unsaved input when reload is cancelled. */ }
}
const createForm = () => ({
  version: null,
  config_type: 'table',
  engine_id: null,
  runtime_engine_id: null,
  execution_engine_id: null,
  schema_name: '',
  table_name: '',
  locator: '',
  sql_query: '',
  service_name: '',
  title: '',
  description: '',
  keywords: [],
  public_access: true, // 默认公开访问
  max_features: 1000
})

const form = reactive(createForm())

// 存储引擎列表（SQL 模式下使用）
const engines = ref([])
const tableUsesRuntime = ref(false)
let engineLoadPromise = null

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
const sqlStableKey = ref([])
const sqlStableKeyFilter = ref('')
const sqlNamedParameters = ref([])
const sqlNamedParameterTypes = ['string', 'bool', 'int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp', 'uuid']
const numericSQLNamedParameterTypes = new Set(['int', 'bigint', 'float', 'double', 'decimal'])

// 字段配置输入
const metricSource = ref(null)
const defaultFieldsInput = ref('')
const filterableFieldsInput = ref('')

// 协议启用状态
const enableRestApi = ref(true) // REST API 默认启用，不可禁用
const metricRestFormats = ref(['json'])
const enableOgcFeatures = ref(false)

// 关键词输入
const inputVisible = ref(false)
const inputValue = ref('')
const inputRef = ref(null)

// 基线只包含编辑内容，步骤、候选列表、过滤词和服务端版本不属于草稿。
const captureDraft = () => {
  const { version, ...fields } = form
  return JSON.stringify({
    form: fields,
    sqlNamedParameters: sqlNamedParameters.value,
    sqlStableKey: sqlStableKey.value,
    sqlHasGeometry: sqlHasGeometry.value,
    sqlGeometryColumn: sqlGeometryColumn.value,
    sqlSrid: sqlSrid.value,
    sqlGeometryType: sqlGeometryType.value,
    defaultFields: defaultFieldsInput.value,
    filterableFields: filterableFieldsInput.value,
    metricSource: metricSource.value,
    enableOgcFeatures: enableOgcFeatures.value,
    keywordInput: inputValue.value
  })
}
const savedDraft = ref(captureDraft())
const markSaved = (snapshot = captureDraft()) => { savedDraft.value = snapshot }
useUnsavedChangesGuard({
  router,
  isDirty: () => captureDraft() !== savedDraft.value,
  shouldConfirmUpdate: (to, from) => to.name !== from.name || to.params.id !== from.params.id
})

const resetDraft = () => {
  Object.assign(form, createForm())
  resetSQLOutputContract()
  sampleRequests.invalidate()
  tableMetadataRequests.invalidate()
  loadingSampleQuery.value = false
  submitting.value = false
  versionConflict.value = false
  currentStep.value = 0
  tableUsesRuntime.value = false
  spatialMetadata.value = null
  sqlNamedParameters.value = []
  metricSource.value = null
  defaultFieldsInput.value = ''
  filterableFieldsInput.value = ''
  enableOgcFeatures.value = false
  metricRestFormats.value = ['json']
  inputValue.value = ''
  inputVisible.value = false
  markSaved()
}

// 表单验证规则
const rules = computed(() => ({
  service_name: [
    { required: true, message: t('service.query.serviceNamePlaceholder'), trigger: 'blur' },
    { pattern: SERVICE_NAME_PATTERN, message: t('service.query.serviceNameHelp'), trigger: 'blur' }
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
  if (form.config_type === 'analytical') return false
  if (form.config_type === 'table') {
    return spatialMetadata.value?.hasGeometry === true
  } else {
    return sqlHasGeometry.value
  }
})

const sqlSupportedEngines = computed(() => queryServiceExecutionEngines(engines.value))
const queryRuntimes = computed(() => federatedQueryRuntimes(engines.value))
const queryTableEngineFilter = engine => isQueryableTableEngine(engine, queryRuntimes.value)
const engineOptionLabel = engine => (
  `${engine.name} (${engine.engine_type}) · ${t(`common.engineStatus.${engineSelectionState(engine)}`)}`
)
const selectedExecutionEngineAvailable = computed(() => {
  const selected = engines.value.find(engine => Number(engine.id) === Number(form.execution_engine_id))
  return isEngineSelectable(selected)
})
const selectedRuntimeAvailable = computed(() => {
  const selected = engines.value.find(engine => Number(engine.id) === Number(form.runtime_engine_id))
  return isEngineSelectable(selected)
})
const sqlOutputFields = computed(() => sqlOutputContract.value?.table?.fields || [])
const sqlStableKeyFields = computed(() => {
	const scalarTypes = new Set(['string', 'bool', 'int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp', 'uuid'])
	return sqlOutputFields.value.filter(field => scalarTypes.has(String(field?.type || '').toLowerCase()))
})
const filteredSQLStableKeyFields = computed(() => {
	const query = sqlStableKeyFilter.value.toLowerCase()
	if (!query) return sqlStableKeyFields.value
	return sqlStableKeyFields.value.filter(field => String(field.name || '').toLowerCase().includes(query))
})

// 计算属性：是否可以进入下一步
const canProceed = computed(() => {
  if (currentStep.value === 0) {
    return !!form.config_type
  } else if (currentStep.value === 1) {
    if (form.config_type === 'analytical') return !!metricSource.value
    if (form.config_type === 'table') {
      return !!form.locator && (!tableUsesRuntime.value || (!!form.runtime_engine_id && selectedRuntimeAvailable.value))
    } else {
		return !!form.execution_engine_id && selectedExecutionEngineAvailable.value && !!form.sql_query && validSQLNamedParameters.value && !!sqlOutputContract.value?.table && sqlStableKey.value.length > 0
    }
  }
  return true
})

const validSQLNamedParameters = computed(() => {
  if (sqlNamedParameters.value.length > 32) return false
  const names = new Set()
  return sqlNamedParameters.value.every(parameter => {
    const name = String(parameter.name || '').trim()
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name) || names.has(name) || !sqlNamedParameterTypes.includes(parameter.type)) return false
    names.add(name)
    if (!validParameterOptions(parameter.options, parameter.type) || !parameterOptionsAllow(parameter.options, parameter.value)) return false
	if (parameter.value === '' || parameter.value === null || parameter.value === undefined) return false
	if (numericSQLNamedParameterTypes.has(parameter.type)) {
	  const number = Number(parameter.value)
	  if (!Number.isFinite(number)) return false
	  if ((parameter.type === 'int' || parameter.type === 'bigint') && !Number.isInteger(number)) return false
	}
	return true
  })
})

const normalizeSQLNamedParameterValue = parameter => {
  const value = parameter.value
  if (['int', 'bigint', 'float', 'double', 'decimal'].includes(parameter.type)) return Number(value)
  if (parameter.type === 'bool') return value === true || String(value).toLowerCase() === 'true'
  return value
}

const sqlNamedParameterValues = () => Object.fromEntries(
  sqlNamedParameters.value.map(parameter => [String(parameter.name || '').trim(), normalizeSQLNamedParameterValue(parameter)])
)

const sqlOutputContractRequestKey = () => `${form.execution_engine_id}\n${form.sql_query}\n${JSON.stringify(sqlNamedParameterValues())}`

// 检测 SQL 输出契约，并从同一份事实中更新稳定排序键候选和空间字段。
const detectSQLOutputContract = async () => {
  if (!form.execution_engine_id || !form.sql_query) {
    return false
  }
  if (!validSQLNamedParameters.value) {
    ElMessage.warning(t('service.query.namedParametersInvalid'))
    return false
  }

  const requestKey = sqlOutputContractRequestKey()
  const request = outputContractRequests.begin(requestKey)
  detectingSQLOutput.value = true
  try {
    const response = await queryServiceAPI.detectSQLOutputContract({
		engine_id: form.execution_engine_id,
      sql: form.sql_query,
      parameters: sqlNamedParameterValues()
    })
	if (!outputContractRequests.isCurrent(request, sqlOutputContractRequestKey())) return false

		  sqlOutputContract.value = response
		  const outputNames = new Set((response?.table?.fields || []).map(field => field.name))
		  sqlStableKey.value = sqlStableKey.value.filter(field => outputNames.has(field))
		  if (sqlStableKey.value.length === 0 && Array.isArray(response?.table?.primary_key)) {
			sqlStableKey.value = response.table.primary_key.filter(field => outputNames.has(field))
		  }
	  const spatial = response?.spatial
	  const columns = spatial?.geometry_columns || []
	  const primary = columns.find(column => column.name === spatial?.primary_geometry_column) || (columns.length === 1 ? columns[0] : null)

    if (primary?.name) {
      // 自动填充空间字段信息
      sqlHasGeometry.value = true
      sqlGeometryColumn.value = primary.name
      sqlSrid.value = primary.srid || spatial.srid || 0
      sqlGeometryType.value = primary.geometry_type || ''

    } else {
      // 未检测到空间字段
      sqlHasGeometry.value = false
      sqlGeometryColumn.value = ''
	  sqlSrid.value = 0
      sqlGeometryType.value = ''
    }
    return true
  } catch (error) {
    if (!outputContractRequests.isCurrent(request, sqlOutputContractRequestKey())) return false
    console.error('[QueryServiceForm] SQL output detection failed:', error)
    ElMessage.warning(t('service.query.detectOutputFailed') + ': ' + (error.response?.data?.error || error.message || t('service.common.unknownError')))
    return false
  } finally {
    if (outputContractRequests.isCurrent(request, sqlOutputContractRequestKey())) {
	  detectingSQLOutput.value = false
    }
  }
}

const handleSQLStableKeyVisibleChange = visible => {
  if (visible) {
    sqlStableKeyFilter.value = ''
    if (!sqlOutputContract.value && !detectingSQLOutput.value) {
      detectSQLOutputContract()
    }
  }
}

const filterSQLStableKeyFields = query => {
  sqlStableKeyFilter.value = String(query || '').trim()
}

const resetSQLOutputContract = () => {
	  outputContractRequests.invalidate()
	  detectingSQLOutput.value = false
	  sqlOutputContract.value = null
	  sqlStableKey.value = []
	  sqlStableKeyFilter.value = ''
  sqlHasGeometry.value = false
  sqlGeometryColumn.value = ''
  sqlSrid.value = 0
  sqlGeometryType.value = ''
}

const addSQLNamedParameter = () => {
  if (sqlNamedParameters.value.length >= 32) return
  sqlNamedParameters.value.push({ name: '', type: 'string', required: true, description: '', value: '', options: [] })
  resetSQLOutputContract()
}

const removeSQLNamedParameter = index => {
  sqlNamedParameters.value.splice(index, 1)
  resetSQLOutputContract()
}

const handleNamedParameterDefinitionChange = parameter => {
  parameter.value = ''
  resetSQLOutputContract()
}

const handleSQLExecutionEngineChange = async (engineID) => {
  applySQLExecutionEngine(form, engineID, engines.value)
  resetSQLOutputContract()
  form.sql_query = ''
  if (!form.execution_engine_id) {
    sampleRequests.invalidate()
    loadingSampleQuery.value = false
    return
  }

  const request = sampleRequests.begin(form.execution_engine_id)
  loadingSampleQuery.value = true
  try {
    const sample = await queryServiceAPI.getQueryEngineSample(form.execution_engine_id)
    if (!sampleRequests.isCurrent(request, form.execution_engine_id)) return
    if (String(sample?.language || '').toLowerCase() !== 'sql' || !String(sample?.query || '').trim()) {
      throw new Error(t('service.query.sampleUnavailable'))
    }
    form.sql_query = sample.query
  } catch (error) {
    if (!sampleRequests.isCurrent(request, form.execution_engine_id)) return
    form.sql_query = ''
    ElMessage.warning(error.response?.data?.error || error.message || t('service.query.sampleUnavailable'))
  } finally {
    if (sampleRequests.isCurrent(request, form.execution_engine_id)) {
      loadingSampleQuery.value = false
    }
  }
}

const loadStorageEngines = () => {
  if (engineLoadPromise) {
    return engineLoadPromise
  }
  loadingEngines.value = true
  const task = withTransientRetry(() => queryServiceAPI.getStorageEngines())
    .then(response => {
      engines.value = response || []
    })
    .catch(error => {
      console.error('[QueryServiceForm] 加载存储引擎失败:', error)
      ElMessage.warning(t('service.query.loadEnginesFailed'))
    })
    .finally(() => {
      loadingEngines.value = false
      if (engineLoadPromise === task) {
        engineLoadPromise = null
      }
    })
  engineLoadPromise = task
  return task
}

const handleEngineDropdownVisible = visible => {
  if (visible) loadStorageEngines()
}

// 方法：处理表选择（ResourceTreePicker 回调）
const handleTableSelection = async (selection) => {
  tableMetadataRequests.invalidate()

  if (!selection) {
    // 清空选择
    form.engine_id = null
    form.schema_name = ''
    form.table_name = ''
    form.locator = ''
    form.runtime_engine_id = null
    tableUsesRuntime.value = false
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
  tableUsesRuntime.value = tableSelectionUsesRuntime(selection)
  if (!tableUsesRuntime.value) {
    form.runtime_engine_id = null
  }

  const request = tableMetadataRequests.begin(form.locator)
  const geometry = await detectTableMetadata('/api/v1/meta', {
    locator: form.locator,
    item_id: selection.identity?.item_id
  })

  if (!tableMetadataRequests.isCurrent(request, form.locator)) return

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

const parseFieldInput = value => String(value || '').split(',').map(field => field.trim()).filter(Boolean)

// 方法：提交表单
const handleSubmit = async () => {
  if (versionConflict.value || submitting.value || loading.value) return
  if (!isEdit.value && form.config_type === 'analytical' && !metricSource.value) {
    ElMessage.warning(t('service.query.metricSourceRequired'))
    return
  }
  const version = editorVersion
  const submittedDraft = captureDraft()
  submitting.value = true
  try {
    // 构建请求数据
    const requestData = {
      service_name: form.service_name,
      title: form.title,
      description: form.description,
      keywords: form.keywords,
      config_type: form.config_type,
      public_access: form.public_access,
      max_features: form.max_features
    }

    // Table 模式特有字段
    if (form.config_type === 'table') {
      requestData.engine_id = form.engine_id
      if (tableUsesRuntime.value) {
        requestData.runtime_engine_id = form.runtime_engine_id
      }
      requestData.schema_name = form.schema_name
      requestData.table_name = form.table_name

      // 构建 data_config
      const dataConfig = {}
	  if (!isEdit.value && form.locator) {
        dataConfig.locator = form.locator
      }

		  dataConfig.default_fields = parseFieldInput(defaultFieldsInput.value)
		  dataConfig.filterable_fields = parseFieldInput(filterableFieldsInput.value)

      if (Object.keys(dataConfig).length > 0) {
        requestData.data_config = dataConfig
      }
    }
    // SQL 模式特有字段
    else if (form.config_type === 'sql') {
      if (form.runtime_engine_id) {
        requestData.runtime_engine_id = form.runtime_engine_id
      } else {
        requestData.engine_id = form.engine_id
      }
		requestData.sql_query = form.sql_query
		requestData.named_parameters = sqlNamedParameters.value.map(parameter => ({
		  name: String(parameter.name || '').trim(),
		  type: parameter.type,
		  required: parameter.required,
          options: (parameter.options || []).map(option => ({ value: option.value, labels: { ...option.labels } })),
		  description: String(parameter.description || '').trim(),
		  ...(!parameter.required ? { default: normalizeSQLNamedParameterValue(parameter) } : {})
		}))
		const dataConfig = {}
		dataConfig.default_fields = parseFieldInput(defaultFieldsInput.value)
		dataConfig.filterable_fields = parseFieldInput(filterableFieldsInput.value)

		  if (!isEdit.value) {
			const outputContract = buildSQLOutputContract()
			requestData.output_contract = outputContract
			dataConfig.stable_key = [...sqlStableKey.value]
		  }
		if (Object.keys(dataConfig).length > 0) requestData.data_config = dataConfig
    }

    if (!isEdit.value && form.config_type === 'analytical') {
      requestData.metric_source = { ...metricSource.value }
    }

    // 协议配置
    requestData.protocols = form.config_type === 'analytical' ? { rest_api: { enabled: true, formats: ['json'] } } : {
      rest_api: { enabled: true },
      ogc_features: { enabled: enableOgcFeatures.value }
    }

    // 提交
    if (isEdit.value) {
      const updated = await queryServiceAPI.updateService(route.params.id, {
        version: form.version,
        title: requestData.title,
        description: requestData.description,
        keywords: requestData.keywords,
        public_access: requestData.public_access,
        max_features: requestData.max_features,
        ...(form.config_type !== 'analytical' ? { protocols: requestData.protocols } : {}),
        ...(requestData.data_config ? { data_config: requestData.data_config } : {})
      })
      if (version !== editorVersion) return
      form.version = updated.version
      markSaved(submittedDraft)
      ElMessage.success(t('service.query.updateSuccess'))
    } else {
      const created = await queryServiceAPI.createService(requestData)
      if (version !== editorVersion) return
      markSaved(submittedDraft)
      ElMessage.success(t('service.query.createSuccess'))
      await navigateServiceRoute(router, `/query-services/${created.id}`, { history: 'replace' })
      return
    }

    await navigateServiceRoute(router, '/query-services', { history: 'replace' })
  } catch (error) {
    if (version !== editorVersion) return
    if (error.response?.data?.error_code === 'resource_version_conflict') {
      versionConflict.value = true
      ElMessage.warning(t('service.query.versionConflict'))
      return
    }
    ElMessage.error(t('service.query.submitFailed') + ': ' + (error.response?.data?.error || error.message || t('service.common.unknownError')))
    console.error('Failed to submit:', error)
  } finally {
    if (version === editorVersion) submitting.value = false
  }
}

// 方法：返回列表
const goBack = () => {
  return navigateServiceRoute(router, '/query-services', { history: 'replace' })
}

// 引擎发现不修改草稿基线，避免把加载期间的用户输入标记为已保存。
onMounted(async () => {
  // 加载存储引擎列表（SQL 模式下使用）
  await loadStorageEngines()
  await tablePickerRef.value?.loadEngines()
})

// 同组件切换服务身份时重建草稿；过期响应不得覆盖当前对象。
watch(() => `${route.name}:${route.params.id || ''}`, async () => {
  const version = ++editorVersion
  resetDraft()
  loading.value = false
  if (isEdit.value) {
    loading.value = true
    try {
      const service = await queryServiceAPI.getService(route.params.id)
      if (version !== editorVersion) return

      // 填充表单（编辑模式只能修改服务信息，不能修改数据源）
      Object.assign(form, {
        service_name: service.service_name,
        title: service.title,
        version: service.version,
        description: service.description,
        keywords: service.keywords || [],
        public_access: service.public_access,
        max_features: service.max_features
      })

      // 填充数据源配置（用于显示和检测空间字段）
      // 注意：数据源配置字段直接在服务对象顶层，不是嵌套在 source_config 下
      form.config_type = service.config_type || 'table'
      form.engine_id = service.engine_id
      form.runtime_engine_id = service.runtime_engine_id
      form.execution_engine_id = service.runtime_engine_id || service.engine_id
      form.schema_name = service.schema_name || ''
      form.table_name = service.table_name || ''
      form.locator = service.data_config?.locator || ''
      form.sql_query = service.sql_query || ''
	  sqlNamedParameters.value = (service.named_parameters || []).map(parameter => ({
		name: parameter.name,
		type: parameter.type,
		required: parameter.required,
          options: (parameter.options || []).map(option => ({ value: option.value, labels: { ...option.labels } })),
		description: parameter.description || '',
		value: parameter.required ? '' : parameter.default
	  }))

      console.log('[QueryServiceForm] 编辑模式：数据源配置', {
        config_type: form.config_type,
        engine_id: form.engine_id,
        schema_name: form.schema_name,
        table_name: form.table_name
      })

	  const snapshot = service.data_config?.source_snapshot
	  defaultFieldsInput.value = (service.data_config?.default_fields || []).join(',')
	  filterableFieldsInput.value = (service.data_config?.filterable_fields || []).join(',')
	  const spatial = snapshot?.spatial
	  const columns = spatial?.geometry_columns || []
	  const primary = columns.find(column => column.name === spatial?.primary_geometry_column) || (columns.length === 1 ? columns[0] : null)
	  if (form.config_type === 'table') {
		tableUsesRuntime.value = !!snapshot?.object_table
		spatialMetadata.value = primary ? {
		  hasGeometry: true,
		  geometryColumn: primary.name,
		  srid: primary.srid || spatial.srid || 0,
		  geometryTypes: primary.geometry_type ? [primary.geometry_type] : [],
		  extent: spatial.extent || null
		} : { hasGeometry: false }
	  } else {
		sqlOutputContract.value = service.output_contract
		sqlStableKey.value = service.stable_key || []
		sqlHasGeometry.value = !!primary
		sqlGeometryColumn.value = primary?.name || ''
		sqlSrid.value = primary?.srid || spatial?.srid || 0
		sqlGeometryType.value = primary?.geometry_type || ''
	  }

      // 协议配置
      metricRestFormats.value = service.protocols?.rest_api?.formats || []
      enableOgcFeatures.value = Boolean(service.protocols?.ogc_features?.enabled)
      markSaved()
    } catch (error) {
      if (version !== editorVersion) return
      ElMessage.error('加载服务失败: ' + (error.message || '未知错误'))
    } finally {
      if (version === editorVersion) loading.value = false
    }
  }
}, { immediate: true })

onBeforeUnmount(() => {
  editorVersion++
  sampleRequests.invalidate()
  outputContractRequests.invalidate()
  tableMetadataRequests.invalidate()
})
</script>

<style scoped>
.query-service-form {
  padding: 20px;
  max-width: 1200px;
  margin: 0 auto;
}

.named-parameters-editor {
  display: flex;
  width: 100%;
  flex-direction: column;
  gap: 10px;
}

.named-parameters-help {
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.named-parameter-row {
  display: grid;
  grid-template-columns: minmax(130px, 1fr) 120px auto minmax(150px, 1fr) minmax(180px, 1.2fr) auto;
  align-items: center;
  gap: 8px;
}

@media (max-width: 960px) {
  .named-parameter-row {
    grid-template-columns: 1fr 1fr;
  }
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
  flex-wrap: wrap;
  align-items: stretch;
  gap: 20px;
  width: 100%;
}

.config-card-wrapper {
  flex: 1 1 240px;
  min-width: 0;
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

<style scoped>
.parameter-options-editor { grid-column: 1 / -1; width: 100%; }
.parameter-option-row { display: grid; grid-template-columns: 1fr 1fr 1fr auto; gap: 8px; margin: 8px 0; }
</style>
