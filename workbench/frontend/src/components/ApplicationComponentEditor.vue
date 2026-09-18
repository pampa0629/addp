<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog component-dialog"
    :title="selectionTarget ? t('workbench.selectionWizard.title') : duplicate ? t('workbench.studio.duplicateComponent') : component ? t('workbench.editComponent') : t('workbench.addComponent')"
    width="min(1180px, calc(100vw - 24px))"
    destroy-on-close
    @open="initialize"
    @close="invalidateEditorRequests"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div v-loading="loading" class="component-editor" data-testid="application-component-editor">
      <el-alert v-if="contractChanged" data-testid="contract-changed-alert" type="warning" :closable="false" :title="t('workbench.contractChanged')" />
      <el-button v-if="contractChanged" :disabled="loading" @click="selectService()">{{ t('workbench.reconfigureContract') }}</el-button>
      <p v-if="contractChanged">{{ t('workbench.reconfigureContractHint') }}</p>
      <el-tabs v-model="activeStep" class="component-steps">
        <el-tab-pane v-for="(step, index) in ['data', 'display', 'conditions']" :key="step" :name="step" :disabled="step !== 'data' && !descriptor" :label="`${index + 1}. ${t(`workbench.studio.steps.${step}`)}`" />
      </el-tabs>
      <div class="editor-grid">
        <el-form label-position="top" class="configuration-form">
          <div v-show="activeStep === 'data'" class="configuration-section">
          <el-form-item :label="t('workbench.service')">
            <el-select v-model="serviceKey" filterable class="full" @change="selectService">
              <el-option v-for="item in services" :key="keyOf(item.ref)" :label="item.title" :value="keyOf(item.ref)" />
            </el-select>
          </el-form-item>
            <p class="configuration-hint">{{ t('workbench.studio.serviceHint') }}</p>
            <el-alert v-if="!loading && services.length === 0" type="info" :closable="false" :title="t('workbench.studio.noServices')" />
            <el-alert v-if="descriptor && draft.rendererType === 'map' && !descriptor.output_contract.spatial" type="warning" :closable="false" :title="t('workbench.studio.mapServiceRequired')" />
            <section v-if="!component && !selectionTarget && displaySuggestions.length" class="display-suggestions" data-testid="display-suggestions">
              <strong>{{ t('workbench.studio.suggestionsTitle') }}</strong>
              <p class="reuse-hint">{{ t('workbench.studio.suggestionsHint') }}</p>
              <button v-for="suggestion in displaySuggestions" :key="suggestion.key" type="button" class="display-suggestion" :data-testid="`suggestion-${suggestion.key}`" @click="applyDisplaySuggestion(suggestion)">
                <strong>{{ t(`workbench.studio.suggestions.${suggestion.key}`) }}</strong>
                <span>{{ suggestionFields(suggestion) }}</span>
                <span class="suggestion-action">{{ t('workbench.studio.trySuggestion') }} →</span>
              </button>
            </section>
          </div>
          <template v-if="descriptor">
            <div v-show="activeStep === 'display'" class="configuration-section">

            <el-form-item v-if="!component" :label="t('workbench.componentTitle')"><el-input v-model="draft.name" maxlength="200" /></el-form-item>
            <el-form-item v-if="!component" :label="t('workbench.description')"><el-input v-model="draft.description" type="textarea" maxlength="2000" /></el-form-item>
            <section v-if="selectionTarget" data-testid="selection-list-fields">
              <p class="configuration-hint">{{ t('workbench.selectionWizard.target', { label: selectionTarget.label }) }}</p>
              <el-form-item v-for="kind in ['search', 'label', 'value']" :key="kind" :label="t(`workbench.selectionWizard.${kind}Field`)" :data-testid="`selection-list-${kind}`">
                <el-select v-model="selectionFields[kind]" class="full" @change="configureSelectionList">
                  <el-option v-for="field in selectionListFields[kind]" :key="field.name" :value="field.name" :label="field.comment ? `${field.comment} (${field.name})` : field.name" />
                </el-select>
              </el-form-item>
              <p v-if="!selectionListFields.search.length || !selectionListFields.value.length" class="configuration-warning">{{ t('workbench.selectionWizard.unsupported') }}</p>
            </section>
            <template v-else>
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
              <el-form-item :label="t('workbench.resultNameField')">
                <el-select v-model="draft.resultNameField" class="full" clearable filterable @change="syncRendererFields">
                  <el-option v-for="field in outputFields.filter(field => field.type === 'string' && selectableFields.some(input => input.name === field.name))" :key="field.name" :value="field.name" :label="field.comment || field.name" />
                </el-select>
              </el-form-item>
              <el-checkbox v-model="draft.totalAsValue">{{ t('workbench.totalAsValue') }}</el-checkbox>
              <p v-if="draft.totalAsValue" class="help-text">{{ t('workbench.totalAsValueHint') }}</p>
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
            <el-form-item :label="t('workbench.columns')">
              <el-checkbox-group v-model="draft.columns" @change="syncRendererFields">
                <el-checkbox v-for="field in selectableFields" :key="field.name" :value="field.name">
                  {{ draft.fieldPresentations.find(item => item.field === field.name)?.label || outputField(field.name)?.comment || field.name }}
                </el-checkbox>
              </el-checkbox-group>
            </el-form-item>
            </template>
            <el-form-item :label="t('workbench.pageLimit')">
              <el-input-number v-model="draft.pageLimit" :min="1" :max="descriptor.input_contract.page.max_limit" @change="resetResult" />
            </el-form-item>
              <details class="presentation-details" :open="Boolean(component)"><summary>{{ t('workbench.studio.fieldFormatting') }}</summary>
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
                <div v-if="item.temporalFormat === 'period'" class="field-presentation-fields">
                  <el-select v-for="kind in ['grain', 'start', 'end']" :key="kind" v-model="item.period[`${kind}_parameter`]" :aria-label="t(`workbench.periodParameters.${kind}`)" :placeholder="t(`workbench.periodParameters.${kind}`)" @change="resetResult">
                    <el-option v-for="parameter in periodParameterCandidates(kind)" :key="parameter.name" :value="parameter.name" :label="parameter.description || parameter.name" />
                  </el-select>
                </div>
                <StateRuleEditor :model-value="item.stateRules || []" :field-type="item.fieldType" @update:model-value="updateStateRules(item, $event)" />
                <ValueLabelEditor v-if="['string', 'bool'].includes(item.fieldType)" :model-value="item.valueLabels || []" :field-type="item.fieldType" @update:model-value="item.valueLabels = $event; resetResult()" />
              </div>
            </template>
              </details>
            </div>
            <div v-show="activeStep === 'conditions'" class="configuration-section">
              <template v-if="duplicate">
                <el-form-item :label="t('workbench.componentTitle')"><el-input v-model="draft.name" maxlength="200" /></el-form-item>
                <p class="reuse-hint">{{ t('workbench.studio.duplicateHint') }}</p>
              </template>
              <p v-else class="configuration-hint">{{ t(isNewComponent ? 'workbench.studio.conditionsHint' : 'workbench.studio.existingConditionsHint') }}</p>
              <p v-if="!component && !selectionTarget && !snapshot.components.length && draft.parameters.some(parameter => parameter.required)" class="configuration-hint">{{ t('workbench.creationGuide.missingIdentity') }}</p>
            <div class="section-header">
              <strong>{{ t('workbench.parameters') }}</strong>
              <div class="parameter-actions">
                <span v-if="parameterizableFields.length === 0 && draft.parameters.length === 0">{{ t('workbench.noParameters') }}</span>
                <el-button link type="primary" :disabled="parameterizableFields.length === 0" v-if="!selectionTarget" @click="addParameter">{{ t('workbench.addParameter') }}</el-button>
              </div>
            </div>
            <p v-if="!selectionTarget && !component && snapshot.parameters.length && draft.parameters.length" class="reuse-hint">{{ t('workbench.studio.reuseFilterHint') }}</p>
            <div v-for="(parameter, index) in draft.parameters" :key="index" class="parameter">
              <label class="parameter-name">{{ parameter.label }}<span v-if="parameter.required"> *</span></label>
              <ParameterCaption v-if="parameter.bindingKind === 'named'" :parameter="parameter" />
              <el-select v-else-if="!selectionTarget" v-model="parameter.field" @change="syncParameter(parameter)">
                <el-option v-for="field in parameterizableFields" :key="field.name" :label="field.comment || field.name" :value="field.name" />
              </el-select>
              <el-tag v-if="parameter.bindingKind === 'named'" type="warning">
                {{ parameter.required ? t('workbench.requiredServiceParameter') : t('workbench.optionalServiceParameter') }}
              </el-tag>
              <el-select v-else-if="!selectionTarget" v-model="parameter.operator" @change="syncParameterControl(parameter)">
                <el-option v-for="operator in operatorsFor(parameter.field)" :key="operator" :label="operator" :value="operator" />
              </el-select>
              <div v-if="isNewComponent && snapshot.parameters.length" class="parameter-reuse" data-testid="parameter-reuse">
                <template v-if="!selectionTarget || parameter.bindingKind === 'named'">
                <label>{{ t('workbench.studio.filterSource') }}</label>
                <el-select :model-value="parameter.applicationParameterKey || ''" :empty-values="[null, undefined]" :fit-input-width="true" :aria-label="t('workbench.studio.filterSource')" class="full" @update:model-value="chooseExistingParameter(parameter, $event)">
                  <el-option value="" :label="t('workbench.studio.independentFilter')" />
                  <el-option v-for="option in reuseOptions(parameter)" :key="option.parameter.key" :value="option.parameter.key" :label="reuseLabel(option.parameter)" :title="reuseLabel(option.parameter)" :disabled="!option.compatible" />
                </el-select>
                <span v-if="parameter.applicationParameterKey" class="reuse-hint">{{ t('workbench.studio.reusedFilterHint') }}</span>
                </template>
                <label v-if="!parameter.applicationParameterKey" class="independent-filter-name">
                  {{ t('workbench.studio.filterName') }}
                  <el-input v-model="parameter.label" :aria-label="t('workbench.studio.filterName')" />
                </label>
              </div>
              <span v-if="isNewComponent && snapshot.parameters.length && !parameter.applicationParameterKey" class="reuse-hint">{{ t('workbench.studio.initialValue') }}</span>
              <ParameterValueInput v-model="parameter.value" :aria-label="t('workbench.studio.initialValue')" :control-type="parameter.controlType" :options="parameter.options || []" :disabled="Boolean(parameter.applicationParameterKey)" @update:model-value="resetResult" />
              <details v-if="!selectionTarget || parameter.bindingKind === 'named'" class="parameter-details"><summary>{{ t('workbench.studio.parameterSettings') }}</summary><el-input v-if="!(isNewComponent && snapshot.parameters.length && !parameter.applicationParameterKey)" v-model="parameter.label" :placeholder="t('workbench.parameterLabel')" /><el-input v-model="parameter.key" :placeholder="t('workbench.parameterKey')" /><el-button link type="danger" :disabled="parameter.bindingKind === 'named'" @click="removeParameter(index)">{{ t('workbench.delete') }}</el-button></details>
            </div>
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
          <div v-if="!queryCompleted && !querying" class="preview-placeholder"><p>{{ t('workbench.studio.componentPreviewHint') }}</p><el-button v-if="descriptor && !requiredParameterValuesPresent(draft.parameters)" link type="primary" @click="activeStep = 'conditions'">{{ t('workbench.studio.fillConditions') }}</el-button></div>
          <WorkbenchRendererHost v-else :rows="resultRows" :renderer-type="draft.rendererType" :config="rendererConfig" :descriptor="descriptor" :page="pageResult" :result-ready="queryCompleted" :query-parameters="resultParameters" />
          <div v-if="draft.rendererType === 'table' && (cursorIndex > 0 || pageResult.has_more)" class="cursor-actions">
            <el-button :disabled="cursorIndex === 0 || querying" @click="previousPage">{{ t('workbench.previousPage') }}</el-button>
            <span>{{ t('workbench.pageNumber', { page: cursorIndex + 1 }) }}</span>
            <el-button :disabled="!pageResult.has_more || querying" @click="nextPage">{{ t('workbench.nextPage') }}</el-button>
          </div>
        </section>
      </div>
    </div>
    <template #footer>
      <span v-if="descriptor && !validDraft" class="configuration-error">{{ t('workbench.studio.incompleteComponent') }}</span>
      <el-button @click="emit('update:modelValue', false)">{{ t('workbench.cancel') }}</el-button>
      <el-button v-if="!component && activeStep !== 'data'" @click="previousStep">{{ t('workbench.studio.previousStep') }}</el-button>
      <el-button v-if="!component && activeStep !== 'conditions'" type="primary" :disabled="!descriptor" @click="nextStep">{{ t('workbench.studio.nextStep') }}</el-button>
      <el-button v-else type="primary" :disabled="!validDraft" @click="submit">{{ t('workbench.confirmComponent') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import ParameterCaption from '../../../../common-frontend/basic/src/components/ParameterCaption.vue'
import ParameterValueInput from '../../../../common-frontend/basic/src/components/ParameterValueInput.vue'
import { computed, onBeforeUnmount, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { createLatestRequestCoordinator } from '@common-ui'
import { executeDescriptorOperation, getConsumerDescriptor, listConsumerServices } from '../api/services'
import { configureSelectionListDraft, buildComponentConfiguration, buildQueryRequest, buildRendererConfig, componentDisplaySuggestions, controlTypeFor, createNamedParameterDraft, createParameterDraft, draftFromComponent, emptyControlValue, requiredParameterValuesPresent, synchronizeFieldPresentations } from '../utils/componentDraft.mjs'
import { boundedExportHasMore, descriptorSupportsExport, downloadBoundedExport, exportFormatForRenderer } from '../utils/boundedExport.mjs'
import WorkbenchRendererHost from './WorkbenchRendererHost.vue'
import StateRuleEditor from './StateRuleEditor.vue'
import ValueLabelEditor from './ValueLabelEditor.vue'
import { valueLabelsValid } from '@common-ui/utils/fieldPresentation.mjs'
import { canBindApplicationParameter, newComponentParameterContext } from '../utils/applicationParameterOptions.mjs'
import { initialApplicationParameterValue } from '../utils/dataApplicationParameters.mjs'
import { compatibleSelectionParameters, affectedSelectionComponentIDs } from '../utils/dataApplicationSelection.mjs'

const props = defineProps({ modelValue: Boolean, selectionTarget: { type: Object, default: null }, duplicate: Boolean, initialRenderer: { type: String, default: 'table' }, component: { type: Object, default: null }, snapshot: { type: Object, required: true }, descriptors: { type: Object, required: true } })
const emit = defineEmits(['update:modelValue', 'save'])
const { t, locale } = useI18n()
const numericTypes = new Set(['int', 'bigint', 'float', 'double', 'decimal'])
const thematicTypes = new Set(['string', 'bool', 'int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp', 'uuid'])
const mapPalettes = ['primary', 'success', 'warning', 'danger']
const activeStep = ref('data')
function nextStep() { activeStep.value = activeStep.value === 'data' ? 'display' : 'conditions' }
function previousStep() { activeStep.value = activeStep.value === 'conditions' ? 'display' : 'data' }
const services = ref([])
const descriptor = ref(null)
const serviceKey = ref('')
const loading = ref(false)
const querying = ref(false)
const exporting = ref(false)
const queryCompleted = ref(false)
const resultRows = ref([])
const resultParameters = ref(null)
const pageResult = ref({ has_more: false, next_cursor: '' })
const cursors = ref([''])
const cursorIndex = ref(0)
const draft = reactive(emptyDraft())
const componentID = ref('')
const configurationFingerprint = ref('')
const isNewComponent = computed(() => !props.component || props.duplicate)
const selectionFields = reactive({ search: '', label: '', value: '' })
const selectionListFields = computed(() => {
  const fields = (descriptor.value?.output_contract.fields || []).filter(field => descriptor.value.input_contract.fields.some(input => input.name === field.name && input.selectable))
  return {
    search: (descriptor.value?.input_contract.fields || []).filter(field => field.type === 'string' && field.filterable && field.operators?.includes('contains')),
    label: fields.filter(field => ['string', 'uuid', 'bool', 'int', 'bigint', 'float', 'double', 'decimal', 'date', 'time', 'timestamp'].includes(field.type)),
    value: fields.filter(field => compatibleSelectionParameters(props.snapshot, props.descriptors, field).some(parameter => parameter.key === props.selectionTarget?.key)),
  }
})
function configureSelectionList() {
  configureSelectionListDraft(draft, descriptor.value, {
    searchField: selectionFields.search, labelField: selectionFields.label, valueField: selectionFields.value,
    searchLabel: t('workbench.selectionWizard.searchLabel', { label: props.selectionTarget.label }),
  })
  resetResult()
}
const reusedParameters = computed(() => Object.fromEntries(draft.parameters.filter(p => p.applicationParameterKey).map(p => [p.key, p.applicationParameterKey])))
const reuseContext = computed(() => descriptor.value && isNewComponent.value ? newComponentParameterContext(props.snapshot, props.descriptors, buildComponentConfiguration(descriptor.value, draft, componentID.value), descriptor.value, reusedParameters.value) : null)
function reuseOptions(parameter) {
  if (!reuseContext.value) return []
  const { snapshot, descriptors } = reuseContext.value
  const binding = snapshot.parameter_bindings.find(b => b.component_id === componentID.value && b.component_parameter_key === parameter.key)
  return props.snapshot.parameters.map(candidate => ({ parameter: candidate, compatible: Boolean(binding && canBindApplicationParameter(snapshot, descriptors, binding, candidate)) }))
}
function reuseLabel(parameter) {
  const ids = affectedSelectionComponentIDs(props.snapshot, [{ application_parameter_key: parameter.key }])
  return `${parameter.label} · ${ids.map(id => props.snapshot.components.find(c => c.id === id)?.title || id).join(t('workbench.listSeparator')) || t('workbench.none')}`
}
function chooseExistingParameter(parameter, key) {
  parameter.applicationParameterKey = key
  const existing = props.snapshot.parameters.find(p => p.key === key)
  parameter.value = existing ? initialApplicationParameterValue(JSON.parse(JSON.stringify(existing))) : emptyControlValue(parameter.controlType)
  resetResult()
}
const descriptorRequests = createLatestRequestCoordinator()
const operationRequests = createLatestRequestCoordinator()

const selectableFields = computed(() => (descriptor.value?.input_contract?.fields || []).filter((field) => field.selectable))
const filterableFields = computed(() => (descriptor.value?.input_contract?.fields || []).filter((field) => field.filterable))
const parameterizableFields = computed(() => filterableFields.value.filter((field) => Array.isArray(field.operators) && field.operators.some(Boolean)))
const outputFields = computed(() => descriptor.value?.output_contract?.fields || [])
const displaySuggestions = computed(() => componentDisplaySuggestions(descriptor.value).sort((a, b) => Number(a.rendererType !== props.initialRenderer) - Number(b.rendererType !== props.initialRenderer)))
function suggestionFields(suggestion) {
  const labels = suggestion.columns.slice(0, 3).map(name => {
    const field = outputFields.value.find(candidate => candidate.name === name)
    return field?.comment ? `${field.comment} (${name})` : name
  })
  const fields = labels.join(suggestion.rendererType === 'chart' ? ' → ' : t('workbench.listSeparator'))
  return suggestion.columns.length > 3 ? t('workbench.studio.suggestionMoreFields', { fields, count: suggestion.columns.length }) : fields
}
async function applyDisplaySuggestion(suggestion) {
  Object.assign(draft, {
    rendererType: suggestion.rendererType, chartType: suggestion.chartType || 'bar',
    columns: [...suggestion.columns], dimension: suggestion.dimension || '', measures: [...(suggestion.measures || [])],
    geometryField: suggestion.geometryField || '', mapLabelField: '', tooltipFields: [], mapStyleMode: 'uniform', mapColorField: '', mapPalette: 'primary', mapLegendTitle: '',
    fieldPresentations: [], totalAsValue: false, resultNameField: '', valueItems: [], pageLimit: descriptor.value.input_contract.page.default_limit,
  })
  syncRendererFields()
  activeStep.value = requiredParameterValuesPresent(draft.parameters) ? 'display' : 'conditions'
  if (canQuery.value) await preview()
}
const numericOutputFields = computed(() => outputFields.value.filter((field) => numericTypes.has(field.type)))
const thematicOutputFields = computed(() => outputFields.value.filter((field) => thematicTypes.has(field.type)))
const mapStyleFields = computed(() => draft.mapStyleMode === 'continuous' ? numericOutputFields.value : thematicOutputFields.value)
const dimensionFields = computed(() => {
  if (draft.chartType !== 'line') return outputFields.value
  const stableKeys = new Set(descriptor.value?.input_contract?.order?.stable_key || [])
  return outputFields.value.filter((field) => stableKeys.has(field.name))
})
const rendererConfig = computed(() => buildRendererConfig(draft))
const contractChanged = computed(() => Boolean(configurationFingerprint.value && descriptor.value && configurationFingerprint.value !== descriptor.value.contract_fingerprint))
const validDraft = computed(() => {
  if (!descriptor.value || !draft.name.trim() || draft.columns.length === 0) return false
  if (props.selectionTarget && (!['search', 'label', 'value'].every(kind => selectionListFields.value[kind].some(field => field.name === selectionFields[kind]))
    || !draft.parameters.some(parameter => parameter.bindingKind !== 'named' && parameter.field === selectionFields.search && parameter.operator === 'contains' && !parameter.applicationParameterKey))) return false
  if (props.duplicate && contractChanged.value) return false
  if (isNewComponent.value && draft.parameters.some(p => p.applicationParameterKey && !reuseOptions(p).some(option => option.parameter.key === p.applicationParameterKey && option.compatible))) return false
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
  if (draft.rendererType === 'chart' && draft.resultNameField && (!draft.columns.includes(draft.resultNameField) || !outputFields.value.some(field => field.name === draft.resultNameField && field.type === 'string'))) return false
  if (draft.rendererType === 'chart' && draft.totalAsValue && (draft.measures.length > 4 || !draft.fieldPresentations.some(item => item.field === draft.dimension && item.temporalFormat === 'period'))) return false
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
    name: '', description: '', columns: [], fixedFilter: null, orderBy: null, pageLimit: 50, parameters: [], rendererType: 'table', chartType: 'bar', totalAsValue: false, resultNameField: '', dimension: '', measures: [], valueItems: [], fieldPresentations: [],
    geometryField: '', mapLabelField: '', tooltipFields: [], mapStyleMode: 'uniform', mapColorField: '', mapPalette: 'primary', mapLegendTitle: '',
  }
}

function assignDraft(value) {
  Object.assign(draft, emptyDraft(), structuredClone(value))
}

async function initialize() {
  componentID.value = isNewComponent.value ? crypto.randomUUID() : props.component.id
  configurationFingerprint.value = props.component?.contract_fingerprint || ''
  activeStep.value = props.duplicate ? 'conditions' : props.component ? 'display' : 'data'
  const sourceComponent = props.component
  const targetContext = componentContextKey(sourceComponent)
  const request = descriptorRequests.begin(targetContext)
  loading.value = true
  querying.value = false
  exporting.value = false
  serviceKey.value = sourceComponent ? keyOf(sourceComponent.service_ref) : ''
  descriptor.value = null
  Object.assign(selectionFields, { search: '', label: '', value: '' })
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
      if (props.duplicate) {
        draft.name = t('workbench.studio.duplicateTitle', { title: sourceComponent.title }).slice(0, 200)
        for (const parameter of draft.parameters) {
          const binding = props.snapshot.parameter_bindings.find(b => b.component_id === sourceComponent.id && b.component_parameter_key === parameter.key)
          if (binding) chooseExistingParameter(parameter, binding.application_parameter_key)
        }
      }
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
  Object.assign(selectionFields, { search: '', label: '', value: '' })
  assignDraft(emptyDraft())
  resetResult()
  try {
    const { data } = await getConsumerDescriptor(summary.ref)
    if (!descriptorRequests.isCurrent(request, serviceKey.value)) return
    descriptor.value = data
    configurationFingerprint.value = data.contract_fingerprint
    const namedParameters = (data.input_contract.named_parameters || [])
      .map((parameter, index) => createNamedParameterDraft(parameter, index, locale.value))
      .filter(Boolean)
    assignDraft({
      ...emptyDraft(),
      rendererType: props.component ? 'table' : props.initialRenderer,
      name: data.title,
      description: data.description || '',
      columns: [...(data.input_contract.default_selection || [])],
      pageLimit: data.input_contract.page.default_limit,
      parameters: namedParameters,
    })
    initializeRenderer()
    if (props.selectionTarget) {
      draft.name = t('workbench.selectionWizard.componentName', { label: props.selectionTarget.label }).slice(0, 200)
      draft.description = ''
      configureSelectionList()
    }
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
    ? [draft.dimension, ...draft.measures, draft.resultNameField]
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
  if (item.fieldType === 'date') return ['date', 'month', ...(['table', 'chart'].includes(draft.rendererType) ? ['period'] : [])]
  if (item.fieldType === 'time') return ['time']
  return ['date', 'datetime']
}

function periodParameterCandidates(kind) {
  return (descriptor.value?.input_contract?.named_parameters || []).filter(parameter => {
    if (!parameter.required) return false
    if (kind !== 'grain') return parameter.type === 'date'
    const values = (parameter.options || []).map(option => option.value)
    return parameter.type === 'string' && values.length === 2 && values.includes('total') && values.includes('month')
  })
}

function fieldPresentationsValid() {
  if (draft.rendererType === 'value') return true
  const expected = synchronizeFieldPresentations(draft, outputFields.value).map((item) => item.field)
  if (expected.length !== draft.fieldPresentations.length || expected.some((field, index) => field !== draft.fieldPresentations[index]?.field)) return false
  return draft.fieldPresentations.every((item) => {
    if (!String(item.label || '').trim() || String(item.label).length > 100 || String(item.unit || '').length > 30) return false
    if (presentationIsNumeric(item) && (!Number.isInteger(item.precision) || item.precision < 0 || item.precision > 8)) return false
    if (presentationIsTemporal(item) && !temporalFormats(item).includes(item.temporalFormat)) return false
    if (item.temporalFormat === 'period' && (!['grain', 'start', 'end'].every(kind => periodParameterCandidates(kind).some(parameter => parameter.name === item.period?.[`${kind}_parameter`])) || item.period.start_parameter === item.period.end_parameter)) return false
    if (draft.rendererType === 'table' && item.width !== null && item.width !== undefined && (!Number.isInteger(item.width) || item.width < 80 || item.width > 600)) return false
    if (!stateRulesValid(item.stateRules, item.fieldType)) return false
    if (!valueLabelsValid(item.valueLabels, item.fieldType)) return false
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
  parameter.applicationParameterKey = ''
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
    resultParameters.value = structuredClone(requestBody.parameters || {})
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
  resultParameters.value = null
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
  emit('save', buildComponentConfiguration(descriptor.value, draft, componentID.value), reusedParameters.value, descriptor.value, props.selectionTarget ? { source_field: selectionFields.value, application_parameter_key: props.selectionTarget.key } : null)
  emit('update:modelValue', false)
}
</script>

<style scoped>
.component-editor,.configuration-form,.preview-panel{display:flex;flex-direction:column;gap:16px}.editor-grid{display:grid;grid-template-columns:minmax(360px,5fr) minmax(480px,7fr);gap:16px}.full{width:100%}.preview-panel{min-height:520px;padding:16px;background:var(--addp-bg-primary);border:1px solid var(--addp-border-color);border-radius:8px}.preview-header,.section-header,.parameter-actions,.cursor-actions{display:flex;align-items:center;justify-content:space-between;gap:8px}.parameter-actions span,.field-presentation-header span{font-size:12px;color:var(--addp-text-secondary)}.parameter{display:grid;grid-template-columns:1fr 1fr 1fr 1fr 1fr auto;gap:8px;align-items:center;padding:12px;border:1px solid var(--addp-border-color);border-radius:8px}.value-item{display:grid;grid-template-columns:minmax(140px,1fr) minmax(120px,1fr) minmax(80px,.7fr) 110px auto;gap:8px;align-items:center;padding:10px;border:1px solid var(--addp-border-color);border-radius:8px}.value-state-rules{grid-column:1/-1}.field-presentation{display:flex;flex-direction:column;gap:8px;padding:10px;border:1px solid var(--addp-border-color);border-radius:8px}.field-presentation-fields{display:grid;grid-template-columns:minmax(120px,1fr) minmax(120px,1fr) repeat(3,minmax(90px,.7fr));gap:8px;align-items:center;width:100%}.cursor-actions{justify-content:center}@media(max-width:1000px){.editor-grid{grid-template-columns:1fr}.parameter,.value-item,.field-presentation-fields{grid-template-columns:1fr 1fr}.preview-panel{min-height:360px}}

.configuration-section { display: flex; flex-direction: column; gap: 12px; min-width: 0; }
.configuration-hint, .preview-placeholder { color: var(--addp-text-secondary); font-size: 13px; line-height: 1.7; }
.preview-placeholder { margin: auto; text-align: center; max-width: 320px; }
.component-steps :deep(.el-tabs__header) { margin-bottom: 0; }
.configuration-form { min-width: 0; max-height: 62vh; overflow: auto; padding-right: 8px; }
.preview-panel { min-height: 360px; height: 58vh; position: sticky; top: 0; overflow: auto; }
.presentation-details > summary, .parameter-details > summary { cursor: pointer; color: var(--el-color-primary); margin: 8px 0 12px; }
.parameter { grid-template-columns: minmax(0, 1fr); gap: 12px; }
.parameter-name { font-size: 14px; font-weight: 600; }.parameter-name span { color: var(--el-color-danger); }
.parameter-details .el-input { margin-bottom: 8px; }
.parameter-reuse { display: flex; flex-direction: column; gap: 8px; }.parameter-reuse label, .reuse-hint { font-size: 12px; color: var(--addp-text-secondary); line-height: 1.6; }
.field-presentation-fields { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.value-item { grid-template-columns: repeat(2, minmax(0, 1fr)); }
@media(max-width:1000px) { .configuration-form { max-height: none; }.preview-panel { position: static; height: 360px; } }
.configuration-error { display: block; margin-bottom: 12px; color: var(--el-color-warning); text-align: left; font-size: 13px; }
.display-suggestions { display: grid; gap: 10px; }
.display-suggestions > p { margin: 0; }
.display-suggestion { display: grid; gap: 8px; width: 100%; padding: 14px; text-align: left; font: inherit; color: var(--addp-text-primary); background: var(--addp-bg-primary); border: 1px solid var(--addp-border-color); border-radius: 8px; cursor: pointer; }
.display-suggestion:hover, .display-suggestion:focus-visible { border-color: var(--el-color-primary); background: var(--el-color-primary-light-9); }
.display-suggestion span { font-size: 12px; line-height: 1.6; overflow-wrap: anywhere; color: var(--addp-text-secondary); }
.display-suggestion .suggestion-action { color: var(--el-color-primary); }
</style>
