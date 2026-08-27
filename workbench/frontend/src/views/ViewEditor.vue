<template>
  <div class="page" v-loading="loading" data-testid="workbench-view-editor">
    <div class="page-header">
      <h2>{{ isEdit ? t('workbench.editView') : t('workbench.createView') }}</h2>
      <div>
        <el-button @click="router.push('/views')">{{ t('workbench.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('workbench.save') }}</el-button>
      </div>
    </div>

    <el-alert
      v-if="contractChanged"
      type="warning"
      :closable="false"
      :title="t('workbench.contractChanged')"
      class="contract-alert"
      data-testid="contract-changed-alert"
    />

    <el-row :gutter="16">
      <el-col :xs="24" :lg="10">
        <el-card>
          <el-form label-position="top">
            <el-form-item :label="t('workbench.service')">
              <el-select v-model="serviceKey" :disabled="isEdit" filterable class="full" @change="selectService">
                <el-option v-for="item in services" :key="keyOf(item.ref)" :label="item.title" :value="keyOf(item.ref)" />
              </el-select>
            </el-form-item>

            <template v-if="descriptor">
              <el-form-item :label="t('workbench.name')">
                <el-input v-model="draft.name" maxlength="200" />
              </el-form-item>
              <el-form-item :label="t('workbench.description')">
                <el-input v-model="draft.description" type="textarea" maxlength="2000" />
              </el-form-item>
              <el-form-item :label="t('workbench.columns')">
              <el-checkbox-group v-model="draft.columns" @change="resetResult">
                  <el-checkbox v-for="field in selectableFields" :key="field.name" :value="field.name">
                    {{ outputField(field.name)?.comment || field.name }}
                  </el-checkbox>
                </el-checkbox-group>
              </el-form-item>
              <el-form-item :label="t('workbench.pageLimit')">
                <el-input-number v-model="draft.pageLimit" :min="1" :max="descriptor.input_contract.page.max_limit" @change="resetResult" />
              </el-form-item>
              <el-form-item :label="t('workbench.renderer')">
                <el-select v-model="draft.rendererType" class="full" data-testid="renderer-select" @change="initializeRenderer">
                  <el-option value="table" :label="t('workbench.renderers.table')" />
                  <el-option value="chart" :label="t('workbench.renderers.chart')" data-testid="renderer-option-chart" />
                  <el-option v-if="descriptor.output_contract.spatial" value="map" :label="t('workbench.renderers.map')" data-testid="renderer-option-map" />
                </el-select>
              </el-form-item>

              <template v-if="draft.rendererType === 'chart'">
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
                <el-form-item :label="t('workbench.geometryField')">
                  <el-input :model-value="draft.geometryField" disabled />
                </el-form-item>
                <el-form-item :label="t('workbench.tooltipFields')">
                  <el-select v-model="draft.tooltipFields" class="full" multiple filterable @change="syncRendererFields">
                    <el-option v-for="field in outputFields" :key="field.name" :value="field.name" :label="field.comment || field.name" />
                  </el-select>
                </el-form-item>
              </template>

              <div class="section-header">
                <strong>{{ t('workbench.parameters') }}</strong>
                <div class="parameter-actions">
                  <span v-if="filterableFields.length === 0" class="parameter-hint">{{ t('workbench.noFilterableFields') }}</span>
                  <el-button link type="primary" :disabled="filterableFields.length === 0" @click="addParameter">
                    {{ t('workbench.addParameter') }}
                  </el-button>
                </div>
              </div>
              <div v-for="(parameter, index) in draft.parameters" :key="index" class="parameter">
                <el-input v-model="parameter.key" :placeholder="t('workbench.parameterKey')" />
                <el-input v-model="parameter.label" :placeholder="t('workbench.parameterLabel')" />
                <el-select v-model="parameter.field" @change="syncParameter(parameter)">
                  <el-option v-for="field in filterableFields" :key="field.name" :label="field.name" :value="field.name" />
                </el-select>
                <el-select v-model="parameter.operator" @change="syncParameterControl(parameter)">
                  <el-option v-for="op in operatorsFor(parameter.field)" :key="op" :label="op" :value="op" />
                </el-select>
                <div v-if="parameter.controlType === 'bbox'" class="bbox-inputs">
                  <el-input-number v-for="index in 4" :key="index" v-model="parameter.value[index - 1]" :controls="false" @change="resetResult" />
                </div>
                <el-select
                  v-else-if="parameter.controlType === 'multiselect'"
                  v-model="parameter.value"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  @change="resetResult"
                />
                <el-switch v-else-if="parameter.controlType === 'checkbox'" v-model="parameter.value" @change="resetResult" />
                <el-select v-else-if="parameter.controlType === 'select'" v-model="parameter.value" clearable @change="resetResult">
                  <el-option :value="true" :label="t('workbench.booleanValues.true')" />
                  <el-option :value="false" :label="t('workbench.booleanValues.false')" />
                </el-select>
                <el-input-number
                  v-else-if="parameter.controlType === 'number'"
                  v-model="parameter.value"
                  :controls="false"
                  @change="resetResult"
                />
                <el-date-picker
                  v-else-if="parameter.controlType === 'date' || parameter.controlType === 'datetime'"
                  v-model="parameter.value"
                  :type="parameter.controlType === 'datetime' ? 'datetime' : 'date'"
                  :value-format="parameter.controlType === 'datetime' ? 'YYYY-MM-DDTHH:mm:ssZ' : 'YYYY-MM-DD'"
                  @change="resetResult"
                />
                <el-input v-else v-model="parameter.value" :placeholder="t('workbench.defaultValue')" @change="resetResult" />
                <el-button link type="danger" @click="removeParameter(index)">{{ t('workbench.delete') }}</el-button>
              </div>
            </template>
          </el-form>
        </el-card>
      </el-col>

      <el-col :xs="24" :lg="14">
        <el-card class="preview-card">
          <template #header>
            <div class="preview-header">
              <strong>{{ t('workbench.preview') }}</strong>
              <div>
                <el-button :disabled="!canQuery" :loading="exporting" @click="exportResult">{{ t('workbench.export') }}</el-button>
                <el-button type="primary" :disabled="!canQuery" :loading="querying" data-testid="query-action" @click="preview">{{ t('workbench.query') }}</el-button>
              </div>
            </div>
          </template>
          <WorkbenchRendererHost
            :rows="resultRows"
            :renderer-type="draft.rendererType"
            :config="rendererConfig"
            :descriptor="descriptor"
            :page="pageResult"
          />
          <div v-if="draft.rendererType === 'table' && (cursorIndex > 0 || pageResult.has_more)" class="cursor-actions">
            <el-button :disabled="cursorIndex === 0 || querying" @click="previousPage">{{ t('workbench.previousPage') }}</el-button>
            <span>{{ t('workbench.pageNumber', { page: cursorIndex + 1 }) }}</span>
            <el-button :disabled="!pageResult.has_more || querying" @click="nextPage">{{ t('workbench.nextPage') }}</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { listConsumerServices, getConsumerDescriptor, executeDescriptorOperation } from '../api/services'
import { createView, getView, updateView } from '../api/views'
import { buildQueryRequest, buildViewPayload, hasParameterValue } from '../utils/viewDraft.mjs'
import { navigateWorkbenchRoute } from '../utils/moduleNavigation'
import WorkbenchRendererHost from '../components/WorkbenchRendererHost.vue'

const numericTypes = new Set(['int', 'bigint', 'float', 'double', 'decimal'])
const { t } = useI18n()
const route = useRoute()
const rawRouter = useRouter()
const router = { push: (location) => navigateWorkbenchRoute(rawRouter, location) }
const isEdit = computed(() => typeof route.params.id === 'string')
const loading = ref(false)
const saving = ref(false)
const querying = ref(false)
const exporting = ref(false)
const services = ref([])
const descriptor = ref(null)
const serviceKey = ref('')
const version = ref(null)
const savedFingerprint = ref('')
const resultRows = ref([])
const pageResult = ref({ has_more: false, next_cursor: '' })
const cursors = ref([''])
const cursorIndex = ref(0)
const draft = reactive({
  name: '', description: '', columns: [], pageLimit: 50, parameters: [],
  rendererType: 'table', chartType: 'bar', dimension: '', measures: [],
  geometryField: '', tooltipFields: []
})

const selectableFields = computed(() => (descriptor.value?.input_contract?.fields || []).filter((field) => field.selectable))
const filterableFields = computed(() => (descriptor.value?.input_contract?.fields || []).filter((field) => field.filterable))
const outputFields = computed(() => descriptor.value?.output_contract?.fields || [])
const numericOutputFields = computed(() => outputFields.value.filter((field) => numericTypes.has(field.type)))
const dimensionFields = computed(() => {
  if (draft.chartType !== 'line') return outputFields.value
  const stableKeys = new Set(descriptor.value?.input_contract?.order?.stable_key || [])
  return outputFields.value.filter((field) => stableKeys.has(field.name))
})
const contractChanged = computed(() => Boolean(savedFingerprint.value && descriptor.value && savedFingerprint.value !== descriptor.value.contract_fingerprint))
const rendererConfig = computed(() => {
  if (draft.rendererType === 'chart') return { chart_type: draft.chartType, dimension: draft.dimension, measures: [...draft.measures] }
  if (draft.rendererType === 'map') return { geometry_field: draft.geometryField, tooltip_fields: [...draft.tooltipFields] }
  return { columns: [...draft.columns] }
})
const canQuery = computed(() => Boolean(descriptor.value && draft.columns.length > 0 && !contractChanged.value))
const keyOf = (ref) => `${ref.service_type}:${ref.service_id}`
const outputField = (name) => outputFields.value.find((field) => field.name === name)

async function loadServices() {
  const { data } = await listConsumerServices({ service_type: 'query', page: 1, page_size: 100 })
  services.value = data.data || []
}

async function selectService() {
  const summary = services.value.find((item) => keyOf(item.ref) === serviceKey.value)
  if (!summary) return
  try {
    const { data } = await getConsumerDescriptor(summary.ref)
    descriptor.value = data
    draft.name = data.title
    draft.description = data.description || ''
    draft.columns = [...(data.input_contract.default_selection || [])]
    draft.pageLimit = data.input_contract.page.default_limit
    draft.parameters = []
    draft.rendererType = 'table'
    draft.chartType = 'bar'
    draft.dimension = ''
    draft.measures = []
    draft.geometryField = ''
    draft.tooltipFields = []
    initializeRenderer()
  } catch (error) {
    descriptor.value = null
    resetResult()
    ElMessage.error(error?.response?.data?.error || t('workbench.loadFailed'))
  }
}

function initializeRenderer() {
  if (!descriptor.value) return
  if (draft.rendererType === 'chart') {
    draft.dimension ||= outputFields.value[0]?.name || ''
    if (draft.measures.length === 0) draft.measures = numericOutputFields.value.slice(0, 1).map((field) => field.name)
  } else if (draft.rendererType === 'map') {
    draft.geometryField = descriptor.value.output_contract.spatial?.primary_geometry_field || ''
  }
  ensureRendererFieldsSelected()
  resetResult()
}

function syncChartType() {
  if (draft.chartType === 'line' && !dimensionFields.value.some((field) => field.name === draft.dimension)) {
    draft.dimension = dimensionFields.value[0]?.name || ''
  }
  if (draft.chartType === 'pie' && draft.measures.length > 1) draft.measures = draft.measures.slice(0, 1)
  ensureRendererFieldsSelected()
  resetResult()
}

function syncRendererFields() {
  ensureRendererFieldsSelected()
  resetResult()
}

function ensureRendererFieldsSelected() {
  const required = draft.rendererType === 'chart'
    ? [draft.dimension, ...draft.measures]
    : draft.rendererType === 'map'
      ? [draft.geometryField, ...draft.tooltipFields]
      : []
  draft.columns = [...new Set([...draft.columns, ...required.filter(Boolean)])]
}

function addParameter() {
  const field = filterableFields.value[0]
  if (!field) return
  draft.parameters.push({
    key: `parameter_${draft.parameters.length + 1}`,
    label: field.name,
    controlType: controlTypeFor(field, field.operators[0]),
    required: false, field: field.name, operator: field.operators[0], fieldType: field.type,
    value: emptyControlValue(controlTypeFor(field, field.operators[0]))
  })
}

function operatorsFor(name) {
  return filterableFields.value.find((field) => field.name === name)?.operators || []
}

function syncParameter(item) {
  const field = filterableFields.value.find((candidate) => candidate.name === item.field)
  item.fieldType = field?.type || 'string'
  item.operator = field?.operators?.[0] || ''
  syncParameterControl(item)
}

function syncParameterControl(item) {
  const field = filterableFields.value.find((candidate) => candidate.name === item.field)
  const controlType = controlTypeFor(field, item.operator)
  item.controlType = controlType
  item.value = emptyControlValue(controlType)
  resetResult()
}

function controlTypeFor(field, operator) {
  if (operator === 'bbox_intersects') return 'bbox'
  if (operator === 'in') return 'multiselect'
  if (operator === 'is_null' || operator === 'is_not_null') return 'checkbox'
  if (field?.type === 'bool') return 'select'
  if (field?.type === 'date') return 'date'
  if (field?.type === 'timestamp') return 'datetime'
  if (numericTypes.has(field?.type)) return 'number'
  return 'text'
}

function emptyControlValue(controlType) {
  if (controlType === 'bbox') return ['', '', '', '']
  if (controlType === 'multiselect') return []
  if (controlType === 'checkbox') return false
  if (controlType === 'number') return null
  return ''
}

function removeParameter(index) {
  draft.parameters.splice(index, 1)
  resetResult()
}

function validateDraft() {
  ensureRendererFieldsSelected()
  if (!descriptor.value || !draft.name || draft.columns.length === 0) return false
  if (draft.parameters.some((parameter) => parameter.required && !hasParameterValue(parameter))) return false
  if (draft.rendererType === 'chart' && (!draft.dimension || draft.measures.length === 0 || (draft.chartType === 'pie' && draft.measures.length !== 1))) return false
  if (draft.rendererType === 'map' && !draft.geometryField) return false
  return true
}

async function executeAtCursor(cursor) {
  if (!validateDraft()) return ElMessage.warning(t('workbench.incomplete'))
  querying.value = true
  try {
    const operation = descriptor.value.operations.find((item) => item.key === 'query')
    const { data } = await executeDescriptorOperation(operation, buildQueryRequest(descriptor.value, draft, cursor))
    resultRows.value = data.data || []
    pageResult.value = data.page || { has_more: false, next_cursor: '' }
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.queryFailed'))
  } finally {
    querying.value = false
  }
}

async function preview() {
  if (contractChanged.value) return
  cursors.value = ['']
  cursorIndex.value = 0
  await executeAtCursor('')
}

async function nextPage() {
  if (!pageResult.value.next_cursor) return
  cursors.value = [...cursors.value.slice(0, cursorIndex.value + 1), pageResult.value.next_cursor]
  cursorIndex.value += 1
  await executeAtCursor(cursors.value[cursorIndex.value])
}

async function previousPage() {
  if (cursorIndex.value === 0) return
  cursorIndex.value -= 1
  await executeAtCursor(cursors.value[cursorIndex.value])
}

async function exportResult() {
  if (!validateDraft() || contractChanged.value) return ElMessage.warning(t('workbench.incomplete'))
  const format = draft.rendererType === 'map' ? 'geojson' : 'csv'
  if (!(descriptor.value.input_contract.formats || []).includes(format)) return ElMessage.warning(t('workbench.exportUnsupported'))
  exporting.value = true
  try {
    const operation = descriptor.value.operations.find((item) => item.key === 'query')
    const response = await executeDescriptorOperation(
      operation,
      buildQueryRequest(descriptor.value, draft, '', format),
      { intent: 'export', responseType: 'blob' }
    )
    if (String(response.headers['x-addp-has-more']).toLowerCase() === 'true') {
      return ElMessage.warning(t('workbench.exportIncomplete'))
    }
    const blob = response.data instanceof Blob ? response.data : new Blob([response.data])
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `workbench-${descriptor.value.ref.service_type}-${descriptor.value.ref.service_id}.${format === 'geojson' ? 'geojson' : 'csv'}`
    link.click()
    URL.revokeObjectURL(url)
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.exportFailed'))
  } finally {
    exporting.value = false
  }
}

async function save() {
  if (!validateDraft()) return ElMessage.warning(t('workbench.incomplete'))
  saving.value = true
  try {
    const payload = buildViewPayload(descriptor.value, draft, version.value)
    if (isEdit.value) await updateView(route.params.id, payload)
    else await createView(payload)
    ElMessage.success(t('workbench.saved'))
    await router.push('/views')
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function loadExisting() {
  const { data: view } = await getView(route.params.id)
  version.value = view.version
  savedFingerprint.value = view.contract_fingerprint
  draft.name = view.name
  draft.description = view.description
  draft.rendererType = view.renderer_type
  serviceKey.value = keyOf(view.service_ref)
  const { data } = await getConsumerDescriptor(view.service_ref)
  descriptor.value = data
  services.value = [{ ref: view.service_ref, title: data.title }]
  draft.columns = [...(view.query_template.select || [])]
  draft.pageLimit = view.query_template.page_limit
  const config = view.renderer_config || {}
  draft.chartType = config.chart_type || 'bar'
  draft.dimension = config.dimension || ''
  draft.measures = [...(config.measures || [])]
  draft.geometryField = config.geometry_field || data.output_contract.spatial?.primary_geometry_field || ''
  draft.tooltipFields = [...(config.tooltip_fields || [])]
  draft.parameters = (view.parameter_definitions || []).map((definition) => {
    const binding = (view.query_template.parameter_filters || []).find((item) => item.parameter_key === definition.key) || {}
    const field = (data.input_contract.fields || []).find((item) => item.name === binding.field)
    return {
      key: definition.key, label: definition.label, controlType: definition.control_type,
      required: definition.required, field: binding.field, operator: binding.operator,
      fieldType: field?.type || 'string',
      value: view.default_parameter_values?.[definition.key] ?? emptyControlValue(definition.control_type)
    }
  })
}

function resetResult() {
  resultRows.value = []
  pageResult.value = { has_more: false, next_cursor: '' }
  cursors.value = ['']
  cursorIndex.value = 0
}

onMounted(async () => {
  loading.value = true
  try {
    if (isEdit.value) await loadExisting()
    else await loadServices()
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.loadFailed'))
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.page { display: flex; flex-direction: column; gap: 16px; }
.page-header, .preview-header, .section-header, .cursor-actions { display: flex; align-items: center; justify-content: space-between; }
.parameter-actions { display: flex; align-items: center; gap: 8px; }
.parameter-hint { color: var(--addp-text-secondary); font-size: 12px; }
.page-header h2 { margin: 0; color: var(--addp-text-primary); }
.full { width: 100%; }
.contract-alert { margin-bottom: 0; }
.parameter { display: grid; grid-template-columns: 1fr 1fr 1fr 1fr 1fr auto; gap: 8px; margin-top: 10px; }
.bbox-inputs { display: grid; grid-template-columns: 1fr 1fr; gap: 4px; }
.preview-card { min-height: 520px; }
.cursor-actions { margin-top: 16px; }
@media (max-width: 900px) {
  .parameter { grid-template-columns: 1fr 1fr; }
  .preview-card { margin-top: 16px; }
}
</style>
