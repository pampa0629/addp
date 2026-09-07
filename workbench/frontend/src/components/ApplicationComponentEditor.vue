<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog component-dialog"
    :title="component ? t('workbench.editComponent') : t('workbench.addComponent')"
    width="min(1180px, calc(100vw - 24px))"
    destroy-on-close
    @open="initialize"
    @close="invalidateEditorRequests"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div v-loading="loading" class="component-editor" data-testid="application-component-editor">
      <el-alert v-if="contractChanged" data-testid="contract-changed-alert" type="warning" :closable="false" :title="t('workbench.contractChanged')" />
      <div class="editor-grid">
        <el-form label-position="top" class="configuration-form">
          <el-form-item :label="t('workbench.service')">
            <el-select v-model="serviceKey" filterable class="full" @change="selectService">
              <el-option v-for="item in services" :key="keyOf(item.ref)" :label="item.title" :value="keyOf(item.ref)" />
            </el-select>
          </el-form-item>
          <template v-if="descriptor">
            <el-form-item :label="t('workbench.componentTitle')"><el-input v-model="draft.name" maxlength="200" /></el-form-item>
            <el-form-item :label="t('workbench.description')"><el-input v-model="draft.description" type="textarea" maxlength="2000" /></el-form-item>
            <el-form-item :label="t('workbench.columns')">
              <el-checkbox-group v-model="draft.columns" @change="syncRendererFields">
                <el-checkbox v-for="field in selectableFields" :key="field.name" :value="field.name">
                  {{ outputField(field.name)?.comment || field.name }}
                </el-checkbox>
              </el-checkbox-group>
            </el-form-item>
            <el-form-item :label="t('workbench.pageLimit')">
              <el-input-number v-model="draft.pageLimit" :min="1" :max="descriptor.input_contract.page.max_limit" @change="resetResult" />
            </el-form-item>
            <el-form-item :label="t('workbench.renderer')">
              <el-select v-model="draft.rendererType" class="full" @change="initializeRenderer">
                <el-option value="table" :label="t('workbench.renderers.table')" />
                <el-option value="chart" :label="t('workbench.renderers.chart')" />
                <el-option v-if="descriptor.output_contract.spatial" value="map" :label="t('workbench.renderers.map')" />
                <el-option v-if="numericOutputFields.length > 0" value="value" :label="t('workbench.renderers.value')" />
              </el-select>
            </el-form-item>
            <template v-if="draft.rendererType === 'value'">
              <div class="section-header">
                <strong>{{ t('workbench.valueItems') }}</strong>
                <el-button link type="primary" :disabled="draft.valueItems.length >= 4" @click="addValueItem">{{ t('workbench.addValueItem') }}</el-button>
              </div>
              <div v-for="(item, index) in draft.valueItems" :key="index" class="value-item">
                <el-select v-model="item.field" filterable :placeholder="t('workbench.valueField')" @change="syncValueItem(item)">
                  <el-option v-for="field in numericOutputFields" :key="field.name" :value="field.name" :label="field.comment || field.name" :disabled="valueFieldUsed(field.name, index)" />
                </el-select>
                <el-input v-model="item.label" :placeholder="t('workbench.valueLabel')" maxlength="100" />
                <el-input v-model="item.unit" :placeholder="t('workbench.valueUnit')" maxlength="30" />
                <el-input-number v-model="item.precision" :min="0" :max="8" :placeholder="t('workbench.valuePrecision')" />
                <el-button link type="danger" :disabled="draft.valueItems.length === 1" @click="removeValueItem(index)">{{ t('workbench.delete') }}</el-button>
                <StateRuleEditor class="value-state-rules" :model-value="item.stateRules || []" :field-type="outputField(item.field)?.type || 'decimal'" @update:model-value="updateStateRules(item, $event)" />
              </div>
            </template>
            <template v-else-if="draft.rendererType === 'chart'">
              <el-form-item :label="t('workbench.chartType')">
                <el-select v-model="draft.chartType" class="full" @change="syncChartType">
                  <el-option value="bar" :label="t('workbench.chartTypes.bar')" />
                  <el-option value="line" :label="t('workbench.chartTypes.line')" />
                  <el-option value="pie" :label="t('workbench.chartTypes.pie')" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('workbench.dimension')">
                <el-select v-model="draft.dimension" class="full" filterable @change="syncRendererFields">
                  <el-option v-for="field in dimensionFields" :key="field.name" :value="field.name" :label="field.comment || field.name" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('workbench.measures')">
                <el-select v-model="draft.measures" class="full" multiple filterable :multiple-limit="draft.chartType === 'pie' ? 1 : 5" @change="syncRendererFields">
                  <el-option v-for="field in numericOutputFields" :key="field.name" :value="field.name" :label="field.comment || field.name" />
                </el-select>
              </el-form-item>
            </template>
            <template v-else-if="draft.rendererType === 'map'">
              <el-form-item :label="t('workbench.geometryField')"><el-input :model-value="draft.geometryField" disabled /></el-form-item>
              <el-form-item :label="t('workbench.mapLabelField')">
                <el-select v-model="draft.mapLabelField" class="full" clearable filterable @change="syncRendererFields">
                  <el-option v-for="field in thematicOutputFields" :key="field.name" :value="field.name" :label="field.comment || field.name" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('workbench.tooltipFields')">
                <el-select v-model="draft.tooltipFields" class="full" multiple filterable @change="syncRendererFields">
                  <el-option v-for="field in outputFields" :key="field.name" :value="field.name" :label="field.comment || field.name" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('workbench.mapStyleMode')">
                <el-select v-model="draft.mapStyleMode" class="full" @change="syncMapStyle">
                  <el-option value="uniform" :label="t('workbench.mapStyleModes.uniform')" />
                  <el-option value="categorical" :label="t('workbench.mapStyleModes.categorical')" />
                  <el-option value="continuous" :label="t('workbench.mapStyleModes.continuous')" />
                </el-select>
              </el-form-item>
              <template v-if="draft.mapStyleMode !== 'uniform'">
                <el-form-item :label="t('workbench.mapColorField')">
                  <el-select v-model="draft.mapColorField" class="full" filterable @change="syncMapStyleField">
                    <el-option v-for="field in mapStyleFields" :key="field.name" :value="field.name" :label="field.comment || field.name" />
                  </el-select>
                </el-form-item>
                <el-form-item :label="t('workbench.mapPalette')">
                  <el-select v-model="draft.mapPalette" class="full" @change="resetResult">
                    <el-option v-for="palette in mapPalettes" :key="palette" :value="palette" :label="t(`workbench.mapPalettes.${palette}`)" />
                  </el-select>
                </el-form-item>
                <el-form-item :label="t('workbench.mapLegendTitle')"><el-input v-model="draft.mapLegendTitle" maxlength="100" /></el-form-item>
              </template>
            </template>
            <template v-if="draft.rendererType !== 'value' && draft.fieldPresentations.length > 0">
              <div class="section-header field-presentation-header">
                <strong>{{ t('workbench.fieldPresentations') }}</strong>
                <span>{{ t('workbench.fieldPresentationsHint') }}</span>
              </div>
              <div v-for="item in draft.fieldPresentations" :key="item.field" class="field-presentation">
                <div class="field-presentation-fields">
                  <el-input :model-value="item.field" disabled :placeholder="t('workbench.presentationField')" />
                  <el-input v-model="item.label" maxlength="100" :placeholder="t('workbench.presentationLabel')" />
                  <el-input v-if="presentationIsNumeric(item)" v-model="item.unit" maxlength="30" :placeholder="t('workbench.presentationUnit')" />
                  <el-input-number v-if="presentationIsNumeric(item)" v-model="item.precision" :min="0" :max="8" :controls="false" :placeholder="t('workbench.presentationPrecision')" />
                  <el-select v-if="presentationIsTemporal(item)" v-model="item.temporalFormat" :placeholder="t('workbench.presentationTemporalFormat')">
                    <el-option v-for="format in temporalFormats(item)" :key="format" :value="format" :label="t(`workbench.temporalFormats.${format}`)" />
                  </el-select>
                  <el-input-number v-if="draft.rendererType === 'table'" v-model="item.width" :min="80" :max="600" :controls="false" :placeholder="t('workbench.presentationWidth')" />
                </div>
                <StateRuleEditor :model-value="item.stateRules || []" :field-type="item.fieldType" @update:model-value="updateStateRules(item, $event)" />
              </div>
            </template>
            <div class="section-header">
              <strong>{{ t('workbench.parameters') }}</strong>
              <div class="parameter-actions">
                <span v-if="parameterizableFields.length === 0 && draft.parameters.length === 0">{{ t('workbench.noParameters') }}</span>
                <el-button link type="primary" :disabled="parameterizableFields.length === 0" @click="addParameter">{{ t('workbench.addParameter') }}</el-button>
              </div>
            </div>
            <div v-for="(parameter, index) in draft.parameters" :key="index" class="parameter">
              <el-input v-model="parameter.key" :placeholder="t('workbench.parameterKey')" />
              <el-input v-model="parameter.label" :placeholder="t('workbench.parameterLabel')" />
              <el-input v-if="parameter.bindingKind === 'named'" :model-value="parameter.name" disabled />
              <el-select v-else v-model="parameter.field" @change="syncParameter(parameter)">
                <el-option v-for="field in parameterizableFields" :key="field.name" :label="field.comment || field.name" :value="field.name" />
              </el-select>
              <el-tag v-if="parameter.bindingKind === 'named'" type="warning">
                {{ parameter.required ? t('workbench.requiredServiceParameter') : t('workbench.optionalServiceParameter') }}
              </el-tag>
              <el-select v-else v-model="parameter.operator" @change="syncParameterControl(parameter)">
                <el-option v-for="operator in operatorsFor(parameter.field)" :key="operator" :label="operator" :value="operator" />
              </el-select>
              <el-input-number v-if="parameter.controlType === 'number'" v-model="parameter.value" :controls="false" @change="resetResult" />
              <el-switch v-else-if="parameter.controlType === 'checkbox'" v-model="parameter.value" @change="resetResult" />
              <el-select v-else-if="parameter.controlType === 'select'" v-model="parameter.value" clearable @change="resetResult">
                <el-option :value="true" :label="t('workbench.booleanValues.true')" />
                <el-option :value="false" :label="t('workbench.booleanValues.false')" />
              </el-select>
              <el-select v-else-if="parameter.controlType === 'multiselect'" v-model="parameter.value" multiple filterable allow-create default-first-option @change="resetResult" />
              <div v-else-if="parameter.controlType === 'bbox'" class="bbox-inputs">
                <el-input-number v-for="position in 4" :key="position" v-model="parameter.value[position - 1]" :controls="false" @change="resetResult" />
              </div>
              <el-date-picker v-else-if="parameter.controlType === 'date' || parameter.controlType === 'datetime'" v-model="parameter.value" :type="parameter.controlType === 'datetime' ? 'datetime' : 'date'" :value-format="parameter.controlType === 'datetime' ? 'YYYY-MM-DDTHH:mm:ssZ' : 'YYYY-MM-DD'" @change="resetResult" />
              <el-input v-else v-model="parameter.value" :placeholder="t('workbench.defaultValue')" @change="resetResult" />
              <el-button link type="danger" :disabled="parameter.bindingKind === 'named'" @click="removeParameter(index)">{{ t('workbench.delete') }}</el-button>
            </div>
          </template>
        </el-form>
        <section class="preview-panel">
          <div class="preview-header">
            <strong>{{ t('workbench.componentPreview') }}</strong>
            <div>
              <el-button data-testid="component-export-action" :disabled="!canQuery || querying" :loading="exporting" @click="exportResult">{{ t('workbench.export') }}</el-button>
              <el-button data-testid="component-query-action" type="primary" :disabled="!canQuery || exporting" :loading="querying" @click="preview">{{ t('workbench.query') }}</el-button>
            </div>
          </div>
          <WorkbenchRendererHost :rows="resultRows" :renderer-type="draft.rendererType" :config="rendererConfig" :descriptor="descriptor" :page="pageResult" :result-ready="queryCompleted" />
          <div v-if="draft.rendererType === 'table' && (cursorIndex > 0 || pageResult.has_more)" class="cursor-actions">
            <el-button :disabled="cursorIndex === 0 || querying" @click="previousPage">{{ t('workbench.previousPage') }}</el-button>
            <span>{{ t('workbench.pageNumber', { page: cursorIndex + 1 }) }}</span>
            <el-button :disabled="!pageResult.has_more || querying" @click="nextPage">{{ t('workbench.nextPage') }}</el-button>
          </div>
        </section>
      </div>
    </div>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">{{ t('workbench.cancel') }}</el-button>
      <el-button type="primary" :disabled="!validDraft" @click="submit">{{ t('workbench.confirmComponent') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { createLatestRequestCoordinator } from '@common-ui'
import { executeDescriptorOperation, getConsumerDescriptor, listConsumerServices } from '../api/services'
import { buildComponentConfiguration, buildQueryRequest, buildRendererConfig, controlTypeFor, createNamedParameterDraft, createParameterDraft, draftFromComponent, emptyControlValue, requiredParameterValuesPresent, synchronizeFieldPresentations } from '../utils/componentDraft.mjs'
import { boundedExportHasMore, descriptorSupportsExport, downloadBoundedExport, exportFormatForRenderer } from '../utils/boundedExport.mjs'
import WorkbenchRendererHost from './WorkbenchRendererHost.vue'
import StateRuleEditor from './StateRuleEditor.vue'

const props = defineProps({ modelValue: Boolean, component: { type: Object, default: null } })
const emit = defineEmits(['update:modelValue', 'save'])
const { t } = useI18n()
const numericTypes = new Set(['int', 'bigint', 'float', 'double', 'decimal'])
const thematicTypes = new Set(['string', 'bool', 'int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp', 'uuid'])
const mapPalettes = ['primary', 'success', 'warning', 'danger']
const services = ref([])
const descriptor = ref(null)
const serviceKey = ref('')
const loading = ref(false)
const querying = ref(false)
const exporting = ref(false)
const queryCompleted = ref(false)
const resultRows = ref([])
const pageResult = ref({ has_more: false, next_cursor: '' })
const cursors = ref([''])
const cursorIndex = ref(0)
const draft = reactive(emptyDraft())
const descriptorRequests = createLatestRequestCoordinator()
const operationRequests = createLatestRequestCoordinator()

const selectableFields = computed(() => (descriptor.value?.input_contract?.fields || []).filter((field) => field.selectable))
const filterableFields = computed(() => (descriptor.value?.input_contract?.fields || []).filter((field) => field.filterable))
const parameterizableFields = computed(() => filterableFields.value.filter((field) => Array.isArray(field.operators) && field.operators.some(Boolean)))
const outputFields = computed(() => descriptor.value?.output_contract?.fields || [])
const numericOutputFields = computed(() => outputFields.value.filter((field) => numericTypes.has(field.type)))
const thematicOutputFields = computed(() => outputFields.value.filter((field) => thematicTypes.has(field.type)))
const mapStyleFields = computed(() => draft.mapStyleMode === 'continuous' ? numericOutputFields.value : thematicOutputFields.value)
const dimensionFields = computed(() => {
  if (draft.chartType !== 'line') return outputFields.value
  const stableKeys = new Set(descriptor.value?.input_contract?.order?.stable_key || [])
  return outputFields.value.filter((field) => stableKeys.has(field.name))
})
const rendererConfig = computed(() => buildRendererConfig(draft))
const contractChanged = computed(() => Boolean(props.component?.contract_fingerprint && descriptor.value && props.component.contract_fingerprint !== descriptor.value.contract_fingerprint))
const validDraft = computed(() => {
  if (!descriptor.value || !draft.name.trim() || draft.columns.length === 0) return false
  const parameterKeys = new Set()
  const descriptorNamedParameters = new Map((descriptor.value.input_contract.named_parameters || []).map((parameter) => [parameter.name, parameter]))
  if (draft.parameters.some((parameter) => {
	if (!parameter.key.trim() || !parameter.label.trim() || parameterKeys.has(parameter.key)) return true
	parameterKeys.add(parameter.key)
	if (parameter.bindingKind !== 'named') return false
	const serviceParameter = descriptorNamedParameters.get(parameter.name)
	return !serviceParameter || serviceParameter.type !== parameter.fieldType || serviceParameter.required !== parameter.required
  })) return false
  if (draft.rendererType === 'value') {
    const fields = draft.valueItems.map((item) => item.field)
    return draft.pageLimit === 1 && fields.length > 0 && fields.length <= 4 && new Set(fields).size === fields.length && draft.valueItems.every((item) => item.field && String(item.label || '').trim() && Number.isInteger(item.precision) && item.precision >= 0 && item.precision <= 8 && stateRulesValid(item.stateRules, outputField(item.field)?.type))
  }
  if (!fieldPresentationsValid()) return false
  if (draft.rendererType === 'chart') return Boolean(draft.dimension && draft.measures.length > 0 && (draft.chartType !== 'pie' || draft.measures.length === 1))
  if (draft.rendererType === 'map') return Boolean(draft.geometryField && (draft.mapStyleMode === 'uniform' || draft.mapColorField))
  return true
})
const canQuery = computed(() => validDraft.value && requiredParameterValuesPresent(draft.parameters) && !contractChanged.value)
const keyOf = (refValue) => `${refValue.service_type}:${refValue.service_id}`
const outputField = (name) => outputFields.value.find((field) => field.name === name)

function componentContextKey(component = props.component) {
  if (!component) return 'create'
  return `edit:${component.id || ''}:${keyOf(component.service_ref)}:${component.contract_fingerprint || ''}`
}

function emptyDraft() {
  return {
    name: '', description: '', columns: [], pageLimit: 50, parameters: [], rendererType: 'table', chartType: 'bar', dimension: '', measures: [], valueItems: [], fieldPresentations: [],
    geometryField: '', mapLabelField: '', tooltipFields: [], mapStyleMode: 'uniform', mapColorField: '', mapPalette: 'primary', mapLegendTitle: '',
  }
}

function assignDraft(value) {
  Object.assign(draft, emptyDraft(), structuredClone(value))
}

async function initialize() {
  const sourceComponent = props.component
  const targetContext = componentContextKey(sourceComponent)
  const request = descriptorRequests.begin(targetContext)
  loading.value = true
  querying.value = false
  exporting.value = false
  serviceKey.value = sourceComponent ? keyOf(sourceComponent.service_ref) : ''
  descriptor.value = null
  assignDraft(emptyDraft())
  resetResult()
  try {
    const { data } = await listConsumerServices({ service_type: 'query', page: 1, page_size: 100 })
    if (!descriptorRequests.isCurrent(request, componentContextKey())) return
    services.value = data.data || []
    if (sourceComponent) {
      const { data: currentDescriptor } = await getConsumerDescriptor(sourceComponent.service_ref)
      if (!descriptorRequests.isCurrent(request, componentContextKey())) return
      descriptor.value = currentDescriptor
      assignDraft(draftFromComponent(sourceComponent, currentDescriptor))
    }
  } catch (error) {
    if (!descriptorRequests.isCurrent(request, componentContextKey())) return
    ElMessage.error(error?.response?.data?.error || t('workbench.loadFailed'))
  } finally {
    if (descriptorRequests.isCurrent(request, componentContextKey())) loading.value = false
  }
}

async function selectService(selectedServiceKey = serviceKey.value) {
  const targetServiceKey = String(selectedServiceKey || '')
  const summary = services.value.find((item) => keyOf(item.ref) === targetServiceKey)
  if (!summary) return
  const request = descriptorRequests.begin(targetServiceKey)
  loading.value = true
  querying.value = false
  exporting.value = false
  descriptor.value = null
  assignDraft(emptyDraft())
  resetResult()
  try {
    const { data } = await getConsumerDescriptor(summary.ref)
    if (!descriptorRequests.isCurrent(request, serviceKey.value)) return
    descriptor.value = data
    const namedParameters = (data.input_contract.named_parameters || [])
      .map((parameter, index) => createNamedParameterDraft(parameter, index))
      .filter(Boolean)
    assignDraft({
      ...emptyDraft(),
      name: data.title,
      description: data.description || '',
      columns: [...(data.input_contract.default_selection || [])],
      pageLimit: data.input_contract.page.default_limit,
      parameters: namedParameters,
    })
    initializeRenderer()
  } catch (error) {
    if (!descriptorRequests.isCurrent(request, serviceKey.value)) return
    descriptor.value = null
    ElMessage.error(error?.response?.data?.error || t('workbench.loadFailed'))
  } finally {
    if (descriptorRequests.isCurrent(request, serviceKey.value)) loading.value = false
  }
}

function initializeRenderer() {
  if (!descriptor.value) return
  if (draft.rendererType === 'chart') {
    draft.dimension ||= dimensionFields.value[0]?.name || ''
    if (draft.measures.length === 0) draft.measures = numericOutputFields.value.slice(0, 1).map((field) => field.name)
  } else if (draft.rendererType === 'map') {
    draft.geometryField = descriptor.value.output_contract.spatial?.primary_geometry_field || ''
    draft.mapStyleMode ||= 'uniform'
    draft.mapPalette ||= 'primary'
  } else if (draft.rendererType === 'value') {
    draft.pageLimit = 1
    if (draft.valueItems.length === 0) addValueItem()
  }
  syncRendererFields()
}

function syncChartType() {
  if (draft.chartType === 'line' && !dimensionFields.value.some((field) => field.name === draft.dimension)) draft.dimension = dimensionFields.value[0]?.name || ''
  if (draft.chartType === 'pie' && draft.measures.length > 1) draft.measures = draft.measures.slice(0, 1)
  syncRendererFields()
}

function syncRendererFields() {
  const required = draft.rendererType === 'chart'
    ? [draft.dimension, ...draft.measures]
    : draft.rendererType === 'map'
      ? [draft.geometryField, draft.mapLabelField, draft.mapStyleMode === 'uniform' ? '' : draft.mapColorField, ...draft.tooltipFields]
      : draft.rendererType === 'value'
        ? draft.valueItems.map((item) => item.field)
        : []
  draft.columns = [...new Set([...draft.columns, ...required.filter(Boolean)])]
  draft.fieldPresentations = synchronizeFieldPresentations(draft, outputFields.value)
  resetResult()
}

function presentationIsNumeric(item) {
  return numericTypes.has(item.fieldType)
}

function presentationIsTemporal(item) {
  return ['date', 'time', 'timestamp'].includes(item.fieldType)
}

function temporalFormats(item) {
  if (item.fieldType === 'date') return ['date']
  if (item.fieldType === 'time') return ['time']
  return ['date', 'datetime']
}

function fieldPresentationsValid() {
  if (draft.rendererType === 'value') return true
  const expected = synchronizeFieldPresentations(draft, outputFields.value).map((item) => item.field)
  if (expected.length !== draft.fieldPresentations.length || expected.some((field, index) => field !== draft.fieldPresentations[index]?.field)) return false
  return draft.fieldPresentations.every((item) => {
    if (!String(item.label || '').trim() || String(item.label).length > 100 || String(item.unit || '').length > 30) return false
    if (presentationIsNumeric(item) && (!Number.isInteger(item.precision) || item.precision < 0 || item.precision > 8)) return false
    if (presentationIsTemporal(item) && !temporalFormats(item).includes(item.temporalFormat)) return false
    if (draft.rendererType === 'table' && item.width !== null && item.width !== undefined && (!Number.isInteger(item.width) || item.width < 80 || item.width > 600)) return false
    if (!stateRulesValid(item.stateRules, item.fieldType)) return false
    return true
  })
}

function stateRulesValid(rules = [], fieldType = '') {
  if (!Array.isArray(rules) || rules.length > 8) return false
  const numeric = numericTypes.has(fieldType)
  const seen = new Set()
  return rules.every((rule) => {
    if (!['eq', 'lt', 'lte', 'gt', 'gte'].includes(rule.operator) || (rule.operator !== 'eq' && !numeric)) return false
    if (!String(rule.label || '').trim() || String(rule.label).length > 50 || !['info', 'success', 'warning', 'danger'].includes(rule.tone)) return false
    if (numeric && (!Number.isFinite(rule.operand) || (['int', 'bigint'].includes(fieldType) && !Number.isInteger(rule.operand)))) return false
    if (!numeric && fieldType === 'bool' && typeof rule.operand !== 'boolean') return false
    if (!numeric && fieldType !== 'bool' && typeof rule.operand !== 'string') return false
    const key = `${rule.operator}:${JSON.stringify(rule.operand)}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function syncMapStyle() {
  if (draft.mapStyleMode === 'uniform') {
    draft.mapColorField = ''
    draft.mapLegendTitle = ''
  } else if (!mapStyleFields.value.some((field) => field.name === draft.mapColorField)) {
    const field = mapStyleFields.value[0]
    draft.mapColorField = field?.name || ''
    draft.mapLegendTitle = field?.comment || field?.name || ''
  }
  syncRendererFields()
}

function syncMapStyleField() {
  const field = mapStyleFields.value.find((candidate) => candidate.name === draft.mapColorField)
  draft.mapLegendTitle = field?.comment || field?.name || ''
  syncRendererFields()
}

function addValueItem() {
  if (draft.valueItems.length >= 4) return
  const used = new Set(draft.valueItems.map((item) => item.field))
  const field = numericOutputFields.value.find((candidate) => !used.has(candidate.name))
  if (!field) return
  draft.valueItems.push({ field: field.name, label: field.comment || field.name, unit: '', precision: 0, stateRules: [] })
  syncRendererFields()
}

function removeValueItem(index) {
  if (draft.valueItems.length <= 1) return
  draft.valueItems.splice(index, 1)
  syncRendererFields()
}

function valueFieldUsed(field, currentIndex) {
  return draft.valueItems.some((item, index) => index !== currentIndex && item.field === field)
}

function syncValueItem(item) {
  const field = numericOutputFields.value.find((candidate) => candidate.name === item.field)
  item.label = field?.comment || item.field
  syncRendererFields()
}

function updateStateRules(item, rules) {
  item.stateRules = rules
}

function addParameter() {
  const parameter = createParameterDraft(parameterizableFields.value[0], draft.parameters.length)
  if (!parameter) return
  draft.parameters = [...draft.parameters, parameter]
}

function operatorsFor(name) {
  return filterableFields.value.find((field) => field.name === name)?.operators || []
}

function syncParameter(parameter) {
  const field = parameterizableFields.value.find((candidate) => candidate.name === parameter.field)
  parameter.fieldType = field?.type || 'string'
  parameter.operator = field?.operators?.[0] || ''
  syncParameterControl(parameter)
}

function syncParameterControl(parameter) {
  const field = filterableFields.value.find((candidate) => candidate.name === parameter.field)
  parameter.controlType = controlTypeFor(field, parameter.operator)
  parameter.value = emptyControlValue(parameter.controlType)
  resetResult()
}

function removeParameter(index) {
  draft.parameters.splice(index, 1)
  resetResult()
}

async function executeAtCursor(cursor, nextCursorIndex = cursorIndex.value, nextCursors = cursors.value) {
  if (!canQuery.value) return
  const targetServiceKey = serviceKey.value
  const request = operationRequests.begin(targetServiceKey)
  const currentDescriptor = descriptor.value
  const operation = currentDescriptor.operations.find((item) => item.key === 'query')
  const requestBody = buildQueryRequest(currentDescriptor, draft, cursor)
  querying.value = true
  try {
    const { data } = await executeDescriptorOperation(operation, requestBody)
    if (!operationRequests.isCurrent(request, serviceKey.value)) return
    resultRows.value = data.data || []
    pageResult.value = data.page || { has_more: false, next_cursor: '' }
    cursorIndex.value = nextCursorIndex
    cursors.value = nextCursors
    queryCompleted.value = true
  } catch (error) {
    if (!operationRequests.isCurrent(request, serviceKey.value)) return
    ElMessage.error(error?.response?.data?.error || t('workbench.queryFailed'))
  } finally {
    if (operationRequests.isCurrent(request, serviceKey.value)) querying.value = false
  }
}

async function preview() {
  resetResult()
  await executeAtCursor('')
}

async function nextPage() {
  const nextCursor = pageResult.value.next_cursor
  if (!nextCursor) return
  const nextIndex = cursorIndex.value + 1
  const nextCursors = cursors.value.slice(0, nextIndex)
  nextCursors[nextIndex] = nextCursor
  await executeAtCursor(nextCursor, nextIndex, nextCursors)
}

async function previousPage() {
  if (cursorIndex.value === 0) return
  const previousIndex = cursorIndex.value - 1
  await executeAtCursor(cursors.value[previousIndex], previousIndex, cursors.value)
}

async function exportResult() {
  if (!canQuery.value) return
  const format = exportFormatForRenderer(draft.rendererType)
  if (!descriptorSupportsExport(descriptor.value, draft.rendererType)) return ElMessage.warning(t('workbench.exportUnsupported'))
  const targetServiceKey = serviceKey.value
  const request = operationRequests.begin(targetServiceKey)
  const currentDescriptor = descriptor.value
  const operation = currentDescriptor.operations.find((item) => item.key === 'query')
  const requestBody = buildQueryRequest(currentDescriptor, draft, '', format)
  exporting.value = true
  try {
    const response = await executeDescriptorOperation(
      operation,
      requestBody,
      { intent: 'export', responseType: 'blob' },
    )
    if (!operationRequests.isCurrent(request, serviceKey.value)) return
    if (boundedExportHasMore(response.headers)) return ElMessage.warning(t('workbench.exportIncomplete'))
    downloadBoundedExport(response.data, `workbench-${currentDescriptor.ref.service_type}-${currentDescriptor.ref.service_id}.${format}`)
  } catch (error) {
    if (!operationRequests.isCurrent(request, serviceKey.value)) return
    ElMessage.error(error?.response?.data?.error || t('workbench.exportFailed'))
  } finally {
    if (operationRequests.isCurrent(request, serviceKey.value)) exporting.value = false
  }
}

function resetResult() {
  operationRequests.invalidate()
  querying.value = false
  exporting.value = false
  queryCompleted.value = false
  resultRows.value = []
  pageResult.value = { has_more: false, next_cursor: '' }
  cursors.value = ['']
  cursorIndex.value = 0
}

function invalidateEditorRequests() {
  descriptorRequests.invalidate()
  operationRequests.invalidate()
  loading.value = false
  querying.value = false
  exporting.value = false
}

onBeforeUnmount(invalidateEditorRequests)

function submit() {
  if (!validDraft.value) return
  emit('save', buildComponentConfiguration(descriptor.value, draft, props.component?.id || crypto.randomUUID()))
  emit('update:modelValue', false)
}
</script>

<style scoped>
.component-editor,.configuration-form,.preview-panel{display:flex;flex-direction:column;gap:16px}.editor-grid{display:grid;grid-template-columns:minmax(360px,5fr) minmax(480px,7fr);gap:16px}.full{width:100%}.preview-panel{min-height:520px;padding:16px;background:var(--addp-bg-primary);border:1px solid var(--addp-border-color);border-radius:8px}.preview-header,.section-header,.parameter-actions,.cursor-actions{display:flex;align-items:center;justify-content:space-between;gap:8px}.parameter-actions span,.field-presentation-header span{font-size:12px;color:var(--addp-text-secondary)}.parameter{display:grid;grid-template-columns:1fr 1fr 1fr 1fr 1fr auto;gap:8px;align-items:center;padding:12px;border:1px solid var(--addp-border-color);border-radius:8px}.value-item{display:grid;grid-template-columns:minmax(140px,1fr) minmax(120px,1fr) minmax(80px,.7fr) 110px auto;gap:8px;align-items:center;padding:10px;border:1px solid var(--addp-border-color);border-radius:8px}.value-state-rules{grid-column:1/-1}.field-presentation{display:flex;flex-direction:column;gap:8px;padding:10px;border:1px solid var(--addp-border-color);border-radius:8px}.field-presentation-fields{display:grid;grid-template-columns:minmax(120px,1fr) minmax(120px,1fr) repeat(3,minmax(90px,.7fr));gap:8px;align-items:center;width:100%}.bbox-inputs{display:grid;grid-template-columns:1fr 1fr;gap:4px}.cursor-actions{justify-content:center}@media(max-width:1000px){.editor-grid{grid-template-columns:1fr}.parameter,.value-item,.field-presentation-fields{grid-template-columns:1fr 1fr}.preview-panel{min-height:360px}}
</style>
