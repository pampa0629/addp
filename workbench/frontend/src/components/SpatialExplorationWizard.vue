<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog"
    :title="t('workbench.spatialWizard.title')"
    width="min(980px, calc(100vw - 24px))"
    destroy-on-close
    @open="initialize"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div v-loading="loading" class="wizard" data-testid="spatial-exploration-wizard">
      <el-alert type="info" :closable="false" :title="t('workbench.spatialWizard.boundary')" />
      <el-form label-position="top">
        <el-row :gutter="16">
          <el-col :xs="24" :md="12">
            <el-form-item :label="t('workbench.spatialWizard.applicationName')">
              <el-input v-model="draft.applicationName" maxlength="200" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :md="12">
            <el-form-item :label="t('workbench.spatialWizard.parameterLabel')">
              <el-input v-model="draft.parameterLabel" maxlength="100" />
            </el-form-item>
          </el-col>
        </el-row>

        <section class="wizard-section">
          <div class="section-heading">
            <strong>{{ t('workbench.spatialWizard.aggregateSection') }}</strong>
            <span>{{ t('workbench.spatialWizard.aggregateHint') }}</span>
          </div>
          <el-row :gutter="16">
            <el-col :xs="24" :md="12">
              <el-form-item :label="t('workbench.spatialWizard.aggregateService')">
                <el-select v-model="aggregateKey" class="full" filterable @change="selectAggregateService">
                  <el-option v-for="service in services" :key="keyOf(service.ref)" :label="service.title" :value="keyOf(service.ref)" />
                </el-select>
              </el-form-item>
            </el-col>
            <template v-if="aggregateDescriptor">
              <el-col :xs="24" :md="12">
                <el-form-item :label="t('workbench.spatialWizard.aggregateFilterField')">
                  <el-select v-model="draft.aggregateFilterField" class="full" filterable @change="aggregateFilterChanged">
                    <el-option v-for="field in aggregateFilterFields" :key="field.name" :label="fieldLabel(field)" :value="field.name" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :md="12">
                <el-form-item :label="t('workbench.spatialWizard.dimensionField')">
                  <el-select v-model="draft.dimensionField" class="full" filterable>
                    <el-option v-for="field in dimensionFields" :key="field.name" :label="fieldLabel(field)" :value="field.name" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :md="12">
                <el-form-item :label="t('workbench.spatialWizard.chartMeasure')">
                  <el-select v-model="draft.chartMeasureField" class="full" filterable>
                    <el-option v-for="field in aggregateNumericFields" :key="field.name" :label="fieldLabel(field)" :value="field.name" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="24">
                <el-form-item :label="t('workbench.spatialWizard.valueFields')">
                  <el-select v-model="selectedValueFields" class="full" multiple filterable :multiple-limit="4" @change="syncValueItems">
                    <el-option v-for="field in aggregateNumericFields" :key="field.name" :label="fieldLabel(field)" :value="field.name" />
                  </el-select>
                </el-form-item>
                <div v-for="item in draft.valueItems" :key="item.field" class="value-item">
                  <span>{{ fieldLabel(aggregateOutputField(item.field)) }}</span>
                  <el-input v-model="item.label" :placeholder="t('workbench.valueLabel')" maxlength="100" />
                  <el-input v-model="item.unit" :placeholder="t('workbench.valueUnit')" maxlength="30" />
                  <el-input-number v-model="item.precision" :min="0" :max="8" />
                </div>
              </el-col>
            </template>
          </el-row>
        </section>

        <section class="wizard-section">
          <div class="section-heading">
            <strong>{{ t('workbench.spatialWizard.spatialSection') }}</strong>
            <span>{{ t('workbench.spatialWizard.spatialHint') }}</span>
          </div>
          <el-row :gutter="16">
            <el-col :xs="24" :md="12">
              <el-form-item :label="t('workbench.spatialWizard.spatialService')">
                <el-select v-model="spatialKey" class="full" filterable @change="selectSpatialService">
                  <el-option v-for="service in spatialServices" :key="keyOf(service.ref)" :label="service.title" :value="keyOf(service.ref)" />
                </el-select>
              </el-form-item>
            </el-col>
            <template v-if="spatialDescriptor">
              <el-col :xs="24" :md="12">
                <el-form-item :label="t('workbench.spatialWizard.detailFilterField')">
                  <el-select v-model="draft.detailFilterField" class="full" filterable>
                    <el-option v-for="field in detailFilterFields" :key="field.name" :label="fieldLabel(field)" :value="field.name" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :md="12">
                <el-form-item :label="t('workbench.mapLabelField')">
                  <el-select v-model="draft.mapLabelField" class="full" clearable filterable>
                    <el-option v-for="field in detailThematicFields" :key="field.name" :label="fieldLabel(field)" :value="field.name" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :md="12">
                <el-form-item :label="t('workbench.mapStyleMode')">
                  <el-select v-model="draft.mapStyleMode" class="full" @change="mapStyleChanged">
                    <el-option value="uniform" :label="t('workbench.mapStyleModes.uniform')" />
                    <el-option value="categorical" :label="t('workbench.mapStyleModes.categorical')" />
                    <el-option value="continuous" :label="t('workbench.mapStyleModes.continuous')" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col v-if="draft.mapStyleMode !== 'uniform'" :xs="24" :md="12">
                <el-form-item :label="t('workbench.mapColorField')">
                  <el-select v-model="draft.mapStyleField" class="full" filterable @change="mapStyleFieldChanged">
                    <el-option v-for="field in mapStyleFields" :key="field.name" :label="fieldLabel(field)" :value="field.name" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :md="12">
                <el-form-item :label="t('workbench.mapPalette')">
                  <el-select v-model="draft.mapPalette" class="full">
                    <el-option v-for="palette in palettes" :key="palette" :label="t(`workbench.mapPalettes.${palette}`)" :value="palette" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col v-if="draft.mapStyleMode !== 'uniform'" :xs="24" :md="12">
                <el-form-item :label="t('workbench.mapLegendTitle')"><el-input v-model="draft.mapLegendTitle" maxlength="100" /></el-form-item>
              </el-col>
              <el-col :span="24">
                <el-form-item :label="t('workbench.tooltipFields')">
                  <el-select v-model="draft.mapTooltipFields" class="full" multiple filterable>
                    <el-option v-for="field in detailSelectableFields" :key="field.name" :label="fieldLabel(field)" :value="field.name" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="24">
                <el-form-item :label="t('workbench.spatialWizard.tableFields')">
                  <el-select v-model="draft.tableColumns" class="full" multiple filterable>
                    <el-option v-for="field in detailSelectableFields" :key="field.name" :label="fieldLabel(field)" :value="field.name" />
                  </el-select>
                </el-form-item>
              </el-col>
            </template>
          </el-row>
        </section>

        <section v-if="aggregateDescriptor && spatialDescriptor" class="wizard-section">
          <div class="section-heading">
            <strong>{{ t('workbench.spatialWizard.sharedParameter') }}</strong>
            <span>{{ t('workbench.spatialWizard.sharedParameterHint') }}</span>
          </div>
          <el-form-item :label="t('workbench.defaultValue')">
            <ApplicationParameterValueInput v-model="draft.defaultValue" :control-type="parameterControlType" />
          </el-form-item>
        </section>
      </el-form>
    </div>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">{{ t('workbench.cancel') }}</el-button>
      <el-button type="primary" :disabled="!canApply" @click="apply">{{ t('workbench.spatialWizard.apply') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { getConsumerDescriptor, listConsumerServices } from '../api/services'
import { controlTypeFor, emptyControlValue } from '../utils/componentDraft.mjs'
import { buildSpatialExplorationDraft } from '../utils/spatialExplorationDraft.mjs'
import ApplicationParameterValueInput from './ApplicationParameterValueInput.vue'

defineProps({ modelValue: Boolean })
const emit = defineEmits(['update:modelValue', 'apply'])
const { t } = useI18n()
const numericTypes = new Set(['int', 'bigint', 'float', 'double', 'decimal'])
const thematicTypes = new Set(['string', 'bool', 'int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp', 'uuid'])
const palettes = ['primary', 'success', 'warning', 'danger']
const services = ref([])
const aggregateDescriptor = ref(null)
const spatialDescriptor = ref(null)
const aggregateKey = ref('')
const spatialKey = ref('')
const loading = ref(false)
const selectedValueFields = ref([])
const draft = reactive(emptyDraft())

const spatialServices = computed(() => services.value.filter((service) => service.output_kind === 'spatial_tabular'))
const aggregateOutputFields = computed(() => selectableOutputFields(aggregateDescriptor.value))
const aggregateNumericFields = computed(() => aggregateOutputFields.value.filter((field) => numericTypes.has(field.type)))
const aggregateFilterFields = computed(() => equalFilterFields(aggregateDescriptor.value))
const aggregateFilterField = computed(() => aggregateFilterFields.value.find((field) => field.name === draft.aggregateFilterField))
const dimensionFields = computed(() => aggregateOutputFields.value.filter((field) => field.type === aggregateFilterField.value?.type && !field.nullable && thematicTypes.has(field.type)))
const detailSelectableFields = computed(() => selectableOutputFields(spatialDescriptor.value).filter((field) => field.type !== 'geometry'))
const detailThematicFields = computed(() => detailSelectableFields.value.filter((field) => thematicTypes.has(field.type)))
const detailNumericFields = computed(() => detailSelectableFields.value.filter((field) => numericTypes.has(field.type)))
const detailFilterFields = computed(() => equalFilterFields(spatialDescriptor.value).filter((field) => field.type === aggregateFilterField.value?.type))
const mapStyleFields = computed(() => draft.mapStyleMode === 'continuous' ? detailNumericFields.value : detailThematicFields.value)
const parameterControlType = computed(() => aggregateFilterField.value ? controlTypeFor(aggregateFilterField.value, 'eq') : 'text')
const canApply = computed(() => {
  try {
    buildSpatialExplorationDraft(configuration(), (role) => `template-${role}`)
    return true
  } catch {
    return false
  }
})

function emptyDraft() {
  return {
    applicationName: '', parameterLabel: '', defaultValue: '', aggregateFilterField: '', dimensionField: '',
    chartMeasureField: '', valueItems: [], detailFilterField: '', mapLabelField: '', mapTooltipFields: [],
    mapStyleMode: 'continuous', mapStyleField: '', mapPalette: 'primary', mapLegendTitle: '', tableColumns: [],
  }
}

async function initialize() {
  Object.assign(draft, emptyDraft())
  aggregateKey.value = ''
  spatialKey.value = ''
  aggregateDescriptor.value = null
  spatialDescriptor.value = null
  selectedValueFields.value = []
  loading.value = true
  try {
    const { data } = await listConsumerServices({ service_type: 'query', page: 1, page_size: 100 })
    services.value = data.data || []
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function selectAggregateService() {
  aggregateDescriptor.value = await loadDescriptor(aggregateKey.value)
  if (!aggregateDescriptor.value) return
  draft.aggregateFilterField = aggregateFilterFields.value.find((field) => aggregateOutputField(field.name) && !aggregateOutputField(field.name).nullable)?.name || aggregateFilterFields.value[0]?.name || ''
  aggregateFilterChanged()
  selectedValueFields.value = aggregateNumericFields.value.slice(0, 2).map((field) => field.name)
  syncValueItems()
  draft.chartMeasureField = aggregateNumericFields.value[0]?.name || ''
}

function aggregateFilterChanged() {
  draft.dimensionField = dimensionFields.value.find((field) => field.name === draft.aggregateFilterField)?.name || dimensionFields.value[0]?.name || ''
  draft.defaultValue = emptyControlValue(parameterControlType.value)
  if (!detailFilterFields.value.some((field) => field.name === draft.detailFilterField)) draft.detailFilterField = detailFilterFields.value[0]?.name || ''
}

async function selectSpatialService() {
  spatialDescriptor.value = await loadDescriptor(spatialKey.value)
  if (!spatialDescriptor.value) return
  draft.detailFilterField = detailFilterFields.value.find((field) => field.name === draft.aggregateFilterField)?.name || detailFilterFields.value[0]?.name || ''
  draft.mapLabelField = detailThematicFields.value.find((field) => field.type === 'string')?.name || detailThematicFields.value[0]?.name || ''
  draft.mapStyleMode = detailNumericFields.value.length > 0 ? 'continuous' : detailThematicFields.value.length > 0 ? 'categorical' : 'uniform'
  mapStyleChanged()
  const defaults = spatialDescriptor.value.input_contract.default_selection || []
  draft.tableColumns = defaults.filter((name) => detailSelectableFields.value.some((field) => field.name === name))
  if (draft.tableColumns.length === 0) draft.tableColumns = detailSelectableFields.value.slice(0, 6).map((field) => field.name)
  draft.mapTooltipFields = [...new Set([draft.mapLabelField, draft.mapStyleField].filter(Boolean))]
}

async function loadDescriptor(key) {
  const service = services.value.find((candidate) => keyOf(candidate.ref) === key)
  if (!service) return null
  loading.value = true
  try {
    const { data } = await getConsumerDescriptor(service.ref)
    return data
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.loadFailed'))
    return null
  } finally {
    loading.value = false
  }
}

function syncValueItems() {
  const previous = new Map(draft.valueItems.map((item) => [item.field, item]))
  draft.valueItems = selectedValueFields.value.map((name) => previous.get(name) || {
    field: name, label: fieldLabel(aggregateOutputField(name)), unit: '', precision: null,
  })
}

function mapStyleChanged() {
  if (draft.mapStyleMode === 'uniform') {
    draft.mapStyleField = ''
    draft.mapLegendTitle = ''
    return
  }
  if (!mapStyleFields.value.some((field) => field.name === draft.mapStyleField)) draft.mapStyleField = mapStyleFields.value[0]?.name || ''
  mapStyleFieldChanged()
}

function mapStyleFieldChanged() {
  draft.mapLegendTitle = fieldLabel(detailSelectableFields.value.find((field) => field.name === draft.mapStyleField))
}

function equalFilterFields(descriptor) {
  return (descriptor?.input_contract?.fields || []).filter((field) => field.filterable && field.operators?.includes('eq'))
}

function selectableOutputFields(descriptor) {
  const selectable = new Set((descriptor?.input_contract?.fields || []).filter((field) => field.selectable).map((field) => field.name))
  return (descriptor?.output_contract?.fields || []).filter((field) => selectable.has(field.name))
}

function aggregateOutputField(name) {
  return aggregateOutputFields.value.find((field) => field.name === name)
}

function fieldLabel(field) {
  return field?.comment || field?.name || ''
}

function keyOf(refValue) {
  return `${refValue.service_type}:${refValue.service_id}`
}

function configuration() {
  return {
    ...draft,
    aggregateDescriptor: aggregateDescriptor.value,
    spatialDescriptor: spatialDescriptor.value,
    chartType: 'bar',
    titles: {
      value: t('workbench.spatialWizard.componentTitles.value'),
      chart: t('workbench.spatialWizard.componentTitles.chart'),
      map: t('workbench.spatialWizard.componentTitles.map'),
      table: t('workbench.spatialWizard.componentTitles.table'),
    },
  }
}

function apply() {
  if (!canApply.value) return
  emit('apply', {
    generated: buildSpatialExplorationDraft(configuration()),
    descriptors: { aggregate: aggregateDescriptor.value, spatial: spatialDescriptor.value },
  })
  emit('update:modelValue', false)
}
</script>

<style scoped>
.wizard,.wizard-section,.section-heading{display:flex;flex-direction:column}.wizard{gap:16px}.wizard-section{gap:12px;padding:16px;border:1px solid var(--addp-border-color);border-radius:8px}.section-heading{gap:4px;margin-bottom:4px}.section-heading span{font-size:12px;color:var(--addp-text-secondary)}.full{width:100%}.value-item{display:grid;grid-template-columns:minmax(140px,1fr) minmax(140px,1fr) minmax(100px,.7fr) 120px;gap:8px;align-items:center;margin-bottom:8px}.value-item>span{color:var(--addp-text-secondary)}@media(max-width:760px){.value-item{grid-template-columns:1fr}}
</style>
