<template>
  <div class="step3-field-mapping">
    <h3>{{ t('transfer.taskWizard.fieldMappingPage') }}</h3>
    <p class="step-description">{{ t('transfer.taskWizard.fieldMappingPageDesc') }}</p>

    <el-alert
      v-if="wizardState.isRawCopyTask.value"
      type="info"
      :closable="false"
      :title="t('transfer.taskWizard.rawCopyNoMappingTitle')"
      :description="t('transfer.taskWizard.rawCopyNoMappingDesc')"
    />

    <template v-else>
    <el-alert
      v-if="isStructuredMongoQuery"
      type="info"
      :closable="false"
      :title="t('transfer.taskWizard.structuredMongoMappingTitle')"
      :description="t('transfer.taskWizard.structuredMongoMappingDesc')"
      class="structured-mapping-alert"
    />
    <el-alert
      v-if="wizardState.isContinuousTask.value"
      type="info"
      :closable="false"
      :title="t('transfer.taskWizard.continuousMappingTitle')"
      :description="t('transfer.taskWizard.continuousMappingDesc')"
      class="continuous-mapping-alert"
    />
    <el-alert
      v-if="mysqlDecimalIssues.length > 0"
      type="error"
      :closable="false"
      :title="t('transfer.taskWizard.mysqlDecimalValidationTitle')"
      :description="t('transfer.taskWizard.mysqlDecimalValidationDesc', { fields: invalidDecimalFieldNames })"
      class="decimal-validation-alert"
    />
    <div v-if="!isStructuredMongoQuery" class="mapping-controls">
      <el-button v-if="!wizardState.isContinuousTask.value" type="primary" @click="autoMap">{{ t('transfer.taskWizard.autoMap') }}</el-button>
      <el-button
        v-if="canRecommendDecimalDefinitions"
        :icon="MagicStick"
        :loading="decimalRecommendationLoading"
        @click="recommendDecimalDefinitions"
      >
        {{ t('transfer.taskWizard.recommendDecimalDefinitions') }}
      </el-button>
      <el-button
        v-if="wizardState.isKafkaContinuousTask.value"
        type="primary"
        :icon="MagicStick"
        :loading="topicSampleLoading"
        @click="loadTopicFieldRecommendations"
      >
        {{ t('transfer.taskWizard.topicSampleSuggest') }}
      </el-button>
      <el-button @click="addMapping">{{ t('transfer.taskWizard.addMapping') }}</el-button>
      <el-button @click="clearMappings">{{ t('transfer.taskWizard.clearAll') }}</el-button>
    </div>

    <el-table
      :data="wizardState.fieldMappings.value"
      :row-class-name="mappingRowClassName"
      border
      class="mapping-table"
    >
      <el-table-column :label="t('transfer.taskWizard.sourceFieldCol')" :width="isStructuredMongoQuery ? 300 : 200">
        <template #default="{ row, $index }">
          <div v-if="isStructuredMongoQuery" class="structured-source-field">
            <span>{{ structuredSourcePath(row.source_field) }}</span>
            <el-tag size="small" :type="structuredSourceRoleType(row.source_field)">
              {{ structuredSourceRoleLabel(row.source_field) }}
            </el-tag>
          </div>
          <el-select
            v-else
            v-model="row.source_field"
            :placeholder="t('transfer.taskWizard.selectSourceField')"
            filterable
            :allow-create="wizardState.isContinuousTask.value || wizardState.sourceQueryEnabled.value"
            :default-first-option="wizardState.isContinuousTask.value || wizardState.sourceQueryEnabled.value"
            @change="handleMappingChange($index)"
          >
            <el-option
              v-for="field in wizardState.sourceFields.value"
              :key="field.name"
              :label="fieldOptionLabel(field)"
              :value="field.name"
            />
          </el-select>
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.sourceTypeCol')" width="120">
        <template #default="{ row }">
          <el-tag v-if="sourceFieldType(row.source_field)" size="small">
            {{ sourceFieldType(row.source_field) }}
          </el-tag>
          <span v-else>-</span>
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.targetFieldCol')" width="200">
        <template #default="{ row, $index }">
          <el-select
            v-model="row.target_field"
            :placeholder="t('transfer.taskWizard.selectTargetField')"
            filterable
            allow-create
            default-first-option
            @change="handleTargetFieldChange($index)"
          >
            <el-option
              v-for="field in wizardState.targetFields.value"
              :key="field.name"
              :label="fieldOptionLabel(field)"
              :value="field.name"
            />
          </el-select>
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.dataTypeCol')" width="140">
        <template #default="{ row, $index }">
          <el-select
            v-model="row.target_type"
            :placeholder="t('transfer.taskWizard.selectTargetType')"
            @change="handleMappingChange($index)"
          >
            <el-option
              v-for="option in targetTypeOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
          </el-select>
        </template>
      </el-table-column>

      <el-table-column :width="isMySQLTarget ? 190 : 150">
        <template #header>
          <div class="decimal-column-header">
            <span>{{ t('transfer.taskWizard.precisionCol') }}</span>
            <el-tooltip :content="t('transfer.taskWizard.precisionHelp')" placement="top">
              <el-icon class="decimal-help-icon" tabindex="0"><QuestionFilled /></el-icon>
            </el-tooltip>
          </div>
        </template>
        <template #default="{ row, $index }">
          <div v-if="row.target_type === 'decimal'" class="decimal-value-cell">
            <el-input-number
              v-model="row.precision"
              class="decimal-number-input"
              :class="{ 'is-error': !!precisionIssue($index) }"
              :placeholder="t('transfer.taskWizard.decimalPrecisionPlaceholder')"
              :min="1"
              :max="decimalPrecisionMax"
              controls-position="right"
              @change="handleMappingChange($index)"
            />
            <span v-if="precisionIssue($index)" class="decimal-error-text">
              {{ decimalIssueMessage(precisionIssue($index)) }}
            </span>
          </div>
          <span v-else>-</span>
        </template>
      </el-table-column>

      <el-table-column :width="isMySQLTarget ? 170 : 150">
        <template #header>
          <div class="decimal-column-header">
            <span>{{ t('transfer.taskWizard.scaleCol') }}</span>
            <el-tooltip :content="t('transfer.taskWizard.scaleHelp')" placement="top">
              <el-icon class="decimal-help-icon" tabindex="0"><QuestionFilled /></el-icon>
            </el-tooltip>
          </div>
        </template>
        <template #default="{ row, $index }">
          <div v-if="row.target_type === 'decimal'" class="decimal-value-cell">
            <el-input-number
              v-model="row.scale"
              class="decimal-number-input"
              :class="{ 'is-error': !!scaleIssue($index) }"
              :placeholder="t('transfer.taskWizard.decimalScalePlaceholder')"
              :min="0"
              :max="decimalScaleMax(row)"
              controls-position="right"
              @change="handleMappingChange($index)"
            />
            <span v-if="scaleIssue($index)" class="decimal-error-text">
              {{ decimalIssueMessage(scaleIssue($index)) }}
            </span>
          </div>
          <span v-else>-</span>
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.defaultValueCol')" width="150">
        <template #default="{ row, $index }">
          <el-input
            v-model="row.default_value"
            :placeholder="t('transfer.taskWizard.defaultValuePlaceholder')"
            @input="handleMappingChange($index)"
          />
        </template>
      </el-table-column>

      <el-table-column v-if="!wizardState.isContinuousTask.value" :label="t('transfer.taskWizard.formatCol')" width="140">
        <template #default="{ row, $index }">
          <el-input
            v-model="row.format"
            :placeholder="t('transfer.taskWizard.formatPlaceholder')"
            @input="handleMappingChange($index)"
          />
        </template>
      </el-table-column>

      <el-table-column :label="t('transfer.taskWizard.nullableCol')" width="90" align="center">
        <template #default="{ row, $index }">
          <el-switch
            v-model="row.nullable"
            :disabled="wizardState.isDatabaseCDCTask.value"
            @change="handleMappingChange($index)"
          />
        </template>
      </el-table-column>

      <el-table-column v-if="!isStructuredMongoQuery" :label="t('transfer.taskWizard.actionsCol')" width="100" fixed="right">
        <template #default="{ $index }">
          <el-button
            type="danger"
            size="small"
            @click="removeMapping($index)"
          >
            {{ t('transfer.taskWizard.deleteMappingBtn') }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <div v-if="wizardState.fieldMappings.value.length === 0" class="empty-hint">
      <el-empty :description="t('transfer.taskWizard.emptyMappingHint')" />
    </div>

    <el-dialog
      v-model="topicSampleDialogVisible"
      :title="t('transfer.taskWizard.topicSampleDialogTitle')"
      width="760px"
      destroy-on-close
    >
      <el-alert
        type="warning"
        :closable="false"
        :title="t('transfer.taskWizard.topicSampleNotice')"
        class="topic-sample-notice"
      />
      <el-table :data="topicRecommendations" border max-height="420">
        <el-table-column prop="name" :label="t('transfer.taskWizard.topicSampleField')" min-width="150" />
        <el-table-column :label="t('transfer.taskWizard.topicSampleTarget')" min-width="170">
          <template #default="{ row }">
            <el-input v-model="row.target_name" />
          </template>
        </el-table-column>
        <el-table-column :label="t('transfer.taskWizard.topicSampleType')" width="150">
          <template #default="{ row }">
            <el-select v-model="row.type">
              <el-option
                v-for="option in continuousTargetTypeOptions"
                :key="option.value"
                :label="option.label"
                :value="option.value"
              />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column :label="t('transfer.taskWizard.topicSampleCoverage')" width="110" align="center">
          <template #default="{ row }">
            {{ row.present_count }}/{{ row.sample_count }}
          </template>
        </el-table-column>
        <el-table-column :label="t('transfer.taskWizard.topicSampleNullable')" width="90" align="center">
          <template #default="{ row }">
            <el-switch v-model="row.nullable" />
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="topicSampleDialogVisible = false">{{ t('transfer.taskWizard.cancel') }}</el-button>
        <el-button type="primary" :disabled="!topicRecommendationsValid" @click="confirmTopicFieldRecommendations">
          {{ t('transfer.taskWizard.topicSampleConfirm') }}
        </el-button>
      </template>
    </el-dialog>
    </template>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { MagicStick, QuestionFilled } from '@element-plus/icons-vue'
import { getManagerPreview } from '@/api/managerPreview'
import { fieldDefinitionRecommendationAPI } from '@/api/tasks'
import { CONTINUOUS_FIELD_TYPES, databaseCDCFieldTypes } from './continuousTask.mjs'
import { mysqlDecimalMappingIssues } from './decimalMapping.mjs'
import { inferTopicFieldRecommendations } from './topicFieldRecommendations.mjs'
import { parseMongoStructureQuery } from './mongoStructureQuery.mjs'
import { normalizeFieldType } from '@addp/common-frontend'

const { t } = useI18n()

const props = defineProps({
  wizardState: {
    type: Object,
    required: true
  }
})

const topicSampleLoading = ref(false)
const decimalRecommendationLoading = ref(false)
const topicSampleDialogVisible = ref(false)
const topicRecommendations = ref([])

const structuredMongoModel = computed(() => {
  if (!props.wizardState.sourceQueryEnabled.value) return null
  if (String(props.wizardState.sourceQueryLanguage.value || '').trim().toLowerCase() !== 'mql') return null
  if (!String(props.wizardState.sourceEngineType.value || '').trim().toLowerCase().includes('mongodb')) return null
  const parsed = parseMongoStructureQuery(props.wizardState.sourceQueryStatement.value)
  return parsed.supported ? parsed.model : null
})
const isStructuredMongoQuery = computed(() => structuredMongoModel.value !== null)

const targetTypeOptions = computed(() => {
  const types = props.wizardState.isDatabaseCDCTask.value
    ? databaseCDCFieldTypes(props.wizardState.sourceEngineType.value)
    : props.wizardState.isContinuousTask.value
    ? CONTINUOUS_FIELD_TYPES
    : ['string', 'bool', 'int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp', 'json', 'uuid', 'geometry']
  return types.map(value => ({
    value,
    label: t(`transfer.taskWizard.fieldType.${value}`)
  }))
})
const continuousTargetTypeOptions = computed(() => CONTINUOUS_FIELD_TYPES.map(value => ({
  value,
  label: t(`transfer.taskWizard.fieldType.${value}`)
})))
const topicRecommendationsValid = computed(() => {
  const existingSources = new Set(props.wizardState.fieldMappings.value
    .map(mapping => String(mapping?.source_field || '').trim().toLowerCase())
    .filter(Boolean))
  const targetNames = new Set()
  props.wizardState.fieldMappings.value.forEach(mapping => {
    const targetName = String(mapping?.target_field || '').trim().toLowerCase()
    if (targetName) targetNames.add(targetName)
  })
  for (const recommendation of topicRecommendations.value) {
    const sourceName = String(recommendation?.name || '').trim().toLowerCase()
    const targetName = String(recommendation?.target_name || '').trim().toLowerCase()
    if (!targetName || !CONTINUOUS_FIELD_TYPES.includes(String(recommendation?.type || '').trim().toLowerCase())) {
      return false
    }
    if (existingSources.has(sourceName)) continue
    if (targetNames.has(targetName)) return false
    targetNames.add(targetName)
  }
  return topicRecommendations.value.length > 0
})

const isMySQLTarget = computed(() => String(props.wizardState.targetEngineType.value || '').toLowerCase().includes('mysql'))
const decimalPrecisionMax = computed(() => isMySQLTarget.value ? 65 : 1000)
const mysqlDecimalIssues = computed(() => mysqlDecimalMappingIssues(
  props.wizardState.fieldMappings.value,
  props.wizardState.sourceFields.value,
  props.wizardState.targetFields.value,
  props.wizardState.targetEngineType.value,
  props.wizardState.targetRepresentation.value
))
const decimalIssueByIndex = computed(() => new Map(mysqlDecimalIssues.value.map(issue => [issue.index, issue])))
const invalidDecimalFieldNames = computed(() => mysqlDecimalIssues.value
  .map(issue => issue.targetField || issue.sourceField || t('transfer.taskWizard.unnamedField'))
  .join(', '))
const recommendableDecimalSourceFields = computed(() => {
  const sourceTypes = new Map(props.wizardState.sourceFields.value.map(field => [
    String(field?.name || '').trim().toLowerCase(),
    normalizeFieldType(field)
  ]))
  const names = mysqlDecimalIssues.value
    .map(issue => props.wizardState.fieldMappings.value[issue.index]?.source_field)
    .map(name => String(name || '').trim())
    .filter(name => name && sourceTypes.get(name.toLowerCase()) === 'decimal')
  return [...new Set(names)]
})
const canRecommendDecimalDefinitions = computed(() => {
  return isMySQLTarget.value &&
    String(props.wizardState.targetRepresentation.value || '').toLowerCase() === 'native' &&
    props.wizardState.targetFields.value.length === 0 &&
    recommendableDecimalSourceFields.value.length > 0
})

function decimalScaleMax(row) {
  const precision = Number(row?.precision)
  const engineMaximum = isMySQLTarget.value ? 30 : 1000
  return Number.isInteger(precision) && precision > 0 ? Math.min(engineMaximum, precision) : engineMaximum
}

function autoMap() {
  props.wizardState.autoGenerateFieldMappings()
  ElMessage.success(t('transfer.taskWizard.autoMapSuccess'))
}

async function recommendDecimalDefinitions() {
  decimalRecommendationLoading.value = true
  try {
    const response = await fieldDefinitionRecommendationAPI.create({
      source_locator: props.wizardState.sourceLocator.value,
      source_fields: recommendableDecimalSourceFields.value,
      target_engine_type: 'mysql'
    })
    const result = response?.data || response
    const fields = Array.isArray(result?.fields) ? result.fields : []
    const applicable = fields.filter(field => field?.fits_target === true)
    props.wizardState.applyRecommendedDecimalDefinitions(applicable)
    const unsupported = fields.filter(field => field?.fits_target !== true)
    if (unsupported.length > 0) {
      ElMessage.error(t('transfer.taskWizard.decimalRecommendationExceedsTarget', {
        fields: unsupported.map(field => field.source_field).join(', ')
      }))
      return
    }
    ElMessage.success(t('transfer.taskWizard.decimalRecommendationApplied', {
      rows: Number(result?.rows_scanned || 0).toLocaleString()
    }))
  } catch (error) {
    const detail = error.response?.data?.error || error.message
    ElMessage.error(t('transfer.taskWizard.decimalRecommendationFailed', { error: detail }))
  } finally {
    decimalRecommendationLoading.value = false
  }
}

async function loadTopicFieldRecommendations() {
  topicSampleLoading.value = true
  try {
    const response = await getManagerPreview(props.wizardState.sourceLocator.value, 50)
    const preview = response?.preview_type && response?.data
      ? response.data
      : (response?.data || response)
    const recommendations = inferTopicFieldRecommendations(preview?.rows)
    if (recommendations.length === 0) {
      ElMessage.warning(t('transfer.taskWizard.topicSampleEmpty'))
      return
    }
    topicRecommendations.value = recommendations.map(recommendation => ({
      ...recommendation,
      target_name: recommendation.name
    }))
    topicSampleDialogVisible.value = true
  } catch (error) {
    const detail = error.response?.data?.error || error.response?.data?.message || error.message
    ElMessage.error(t('transfer.taskWizard.topicSampleFailed', { error: detail }))
  } finally {
    topicSampleLoading.value = false
  }
}

function confirmTopicFieldRecommendations() {
  if (!topicRecommendationsValid.value) {
    ElMessage.warning(t('transfer.taskWizard.topicSampleInvalid'))
    return
  }
  const { addedCount } = props.wizardState.applyTopicFieldRecommendations(topicRecommendations.value)
  topicSampleDialogVisible.value = false
  ElMessage.success(t('transfer.taskWizard.topicSampleApplied', { count: addedCount }))
}

function addMapping() {
  props.wizardState.addFieldMapping()
}

function removeMapping(index) {
  props.wizardState.removeFieldMapping(index)
}

async function clearMappings() {
  try {
    await ElMessageBox.confirm(
      t('transfer.taskWizard.clearMappingsConfirm'),
      t('transfer.taskWizard.clearMappingsConfirmTitle'),
      {
        confirmButtonText: t('transfer.taskWizard.confirmOk'),
        cancelButtonText: t('transfer.taskWizard.cancel'),
        type: 'warning'
      }
    )

    while (props.wizardState.fieldMappings.value.length > 0) {
      props.wizardState.removeFieldMapping(0)
    }
    ElMessage.success(t('transfer.taskWizard.clearMappingsSuccess'))
  } catch (error) {
    // 用户取消
  }
}

function handleMappingChange(index) {
  const mapping = props.wizardState.fieldMappings.value[index]
  props.wizardState.updateFieldMapping(index, mapping)
}

function handleTargetFieldChange(index) {
  props.wizardState.applyTargetFieldMapping(index)
}

function decimalMappingIssue(index) {
  return decimalIssueByIndex.value.get(index) || null
}

function precisionIssue(index) {
  const issue = decimalMappingIssue(index)
  return issue && ['precision_required', 'precision_out_of_range', 'target_definition_missing'].includes(issue.code)
    ? issue
    : null
}

function scaleIssue(index) {
  const issue = decimalMappingIssue(index)
  return issue && ['scale_required', 'scale_out_of_range', 'scale_exceeds_precision'].includes(issue.code)
    ? issue
    : null
}

function decimalIssueMessage(issue) {
  return issue ? t(`transfer.taskWizard.mysqlDecimalIssue.${issue.code}`) : ''
}

function mappingRowClassName({ rowIndex }) {
  return decimalIssueByIndex.value.has(rowIndex) ? 'decimal-invalid-row' : ''
}

function fieldOptionLabel(field) {
  const name = String(field?.name || '').trim()
  const type = standardFieldType(field)
  return type ? `${name} (${type})` : name
}

function structuredSourceField(fieldName) {
  return props.wizardState.sourceFields.value.find(field => field?.name === fieldName) || null
}

function structuredSourcePath(fieldName) {
  const field = structuredSourceField(fieldName)
  if (!field) return fieldName
  if (field.source_role === 'array_index') {
    return t('transfer.taskWizard.mongoBuilder.arrayIndexSource', { path: field.source_path })
  }
  return field.source_path || field.name
}

function structuredSourceRoleLabel(fieldName) {
  const role = structuredSourceField(fieldName)?.source_role
  const key = {
    record_identifier: 'recordIdentifier',
    parent_identifier: 'parentIdentifier',
    parent_field: 'parentField',
    array_element_field: 'arrayElementField',
    array_index: 'arrayIndex',
    selected_field: 'selectedField'
  }[role] || 'selectedField'
  return t(`transfer.taskWizard.structuredSourceRole.${key}`)
}

function structuredSourceRoleType(fieldName) {
  const role = structuredSourceField(fieldName)?.source_role
  if (role === 'array_index') return 'warning'
  if (role === 'record_identifier' || role === 'parent_identifier') return 'info'
  if (role === 'parent_field') return undefined
  return 'success'
}

function standardFieldType(field) {
  return String(field?.type || '').trim()
}

function sourceFieldType(fieldName) {
  const name = String(fieldName || '').trim()
  if (!name) return ''
  const field = props.wizardState.sourceFields.value.find(item => item?.name === name)
  return standardFieldType(field)
}
</script>

<style scoped>
.step3-field-mapping {
  max-width: 1440px;
  margin: 0 auto;
}

.step-description {
  color: var(--addp-text-secondary);
  margin-bottom: 20px;
}

.mapping-controls {
  display: flex;
  gap: 12px;
}

.continuous-mapping-alert {
  margin-bottom: 16px;
}

.structured-mapping-alert {
  margin-bottom: 16px;
}

.structured-source-field {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.structured-source-field > span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.decimal-validation-alert {
  margin-bottom: 16px;
}

.topic-sample-notice {
  margin-bottom: 16px;
}

.mapping-table {
  margin-top: 20px;
}

.empty-hint {
  margin-top: 40px;
}

.decimal-number-input {
  width: 100%;
}

.decimal-value-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.decimal-column-header {
  display: flex;
  align-items: center;
  gap: 6px;
}

.decimal-help-icon {
  color: var(--addp-text-secondary);
  cursor: help;
}

.decimal-error-text {
  color: var(--el-color-danger);
  font-size: 12px;
  line-height: 1.5;
  white-space: normal;
}

.decimal-number-input.is-error :deep(.el-input__wrapper) {
  box-shadow: 0 0 0 1px var(--el-color-danger) inset;
}

.mapping-table :deep(.decimal-invalid-row > .el-table__cell) {
  background: var(--el-color-danger-light-9);
}
</style>
