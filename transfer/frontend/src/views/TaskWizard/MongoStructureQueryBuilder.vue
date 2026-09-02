<template>
  <div class="mongo-structure-builder">
    <div class="builder-toolbar">
      <el-radio-group :model-value="mode" @change="changeMode">
        <el-radio-button value="visual">{{ t('transfer.taskWizard.mongoBuilder.visualMode') }}</el-radio-button>
        <el-radio-button value="advanced">{{ t('transfer.taskWizard.mongoBuilder.advancedMode') }}</el-radio-button>
      </el-radio-group>
    </div>

    <el-alert
      v-if="unsupportedReason"
      type="warning"
      :closable="false"
      :title="t('transfer.taskWizard.mongoBuilder.unsupportedTitle')"
      :description="t('transfer.taskWizard.mongoBuilder.unsupportedDescription')"
    />

    <template v-if="mode === 'visual'">
      <section class="builder-section compact-section">
        <strong>{{ t('transfer.taskWizard.mongoBuilder.collection') }}</strong>
        <el-input :model-value="draft.collection" disabled />
      </section>

      <section class="builder-section">
        <div class="section-heading">
          <div>
            <strong>{{ t('transfer.taskWizard.mongoBuilder.rowShape') }}</strong>
            <p>{{ t('transfer.taskWizard.mongoBuilder.rowShapeHint') }}</p>
          </div>
        </div>
        <el-radio-group :model-value="rowShape" @change="changeRowShape">
          <el-radio value="document">{{ t('transfer.taskWizard.mongoBuilder.documentRow') }}</el-radio>
          <el-radio value="array">{{ t('transfer.taskWizard.mongoBuilder.arrayRow') }}</el-radio>
        </el-radio-group>

        <label v-if="draft.unwind.enabled" class="field-control">
          <span>{{ t('transfer.taskWizard.mongoBuilder.arrayPath') }}</span>
          <el-select
            :model-value="draft.unwind.path"
            filterable
            :placeholder="t('transfer.taskWizard.mongoBuilder.arrayPathPlaceholder')"
            @change="changeArrayPath"
          >
            <el-option
              v-for="field in arrayFieldOptions"
              :key="field.name"
              :label="field.name"
              :value="field.name"
            />
          </el-select>
        </label>
      </section>

      <section class="builder-section">
        <div class="section-heading">
          <div>
            <strong>{{ t('transfer.taskWizard.mongoBuilder.sourceFields') }}</strong>
            <p>{{ t('transfer.taskWizard.mongoBuilder.sourceFieldsHint') }}</p>
          </div>
        </div>

        <div class="automatic-field-row">
          <el-tag size="small" type="info">
            {{ draft.unwind.enabled
              ? t('transfer.taskWizard.mongoBuilder.parentIdentifier')
              : t('transfer.taskWizard.mongoBuilder.recordIdentifier') }}
          </el-tag>
          <span class="source-path">_id</span>
          <span class="automatic-label">{{ t('transfer.taskWizard.mongoBuilder.automaticallyIncluded') }}</span>
        </div>

        <label v-if="draft.unwind.enabled" class="field-control">
          <span>{{ t('transfer.taskWizard.mongoBuilder.parentDocumentFields') }}</span>
          <small>{{ t('transfer.taskWizard.mongoBuilder.parentDocumentFieldsHint') }}</small>
          <el-select
            v-model="selectedParentSources"
            multiple
            filterable
            collapse-tags
            collapse-tags-tooltip
            :placeholder="t('transfer.taskWizard.mongoBuilder.selectSourceFields')"
          >
            <el-option
              v-for="field in parentFieldOptions"
              :key="field.name"
              :label="field.name"
              :value="field.name"
            />
          </el-select>
        </label>

        <label class="field-control">
          <span>{{ draft.unwind.enabled
            ? t('transfer.taskWizard.mongoBuilder.arrayElementFields')
            : t('transfer.taskWizard.mongoBuilder.documentFields') }}</span>
          <el-select
            v-if="draft.unwind.enabled"
            v-model="selectedArraySources"
            multiple
            filterable
            collapse-tags
            collapse-tags-tooltip
            :disabled="!draft.unwind.path"
            :placeholder="t('transfer.taskWizard.mongoBuilder.selectSourceFields')"
          >
            <el-option
              v-for="field in arrayElementFieldOptions"
              :key="field.name"
              :label="field.name"
              :value="field.name"
            />
          </el-select>
          <el-select
            v-else
            v-model="selectedDocumentSources"
            multiple
            filterable
            collapse-tags
            collapse-tags-tooltip
            :placeholder="t('transfer.taskWizard.mongoBuilder.selectSourceFields')"
          >
            <el-option
              v-for="field in documentFieldOptions"
              :key="field.name"
              :label="field.name"
              :value="field.name"
            />
          </el-select>
        </label>

        <el-checkbox
          v-if="draft.unwind.enabled"
          :model-value="draft.unwind.includeIndex"
          @change="changeIncludeIndex"
        >
          {{ t('transfer.taskWizard.mongoBuilder.includeArrayIndex') }}
        </el-checkbox>

        <div class="selected-field-list">
          <div v-for="field in outputFields" :key="field.name" class="selected-field-row">
            <el-tag v-if="field.source_role === 'array_index'" size="small" type="warning">
              {{ t('transfer.taskWizard.mongoBuilder.arrayIndex') }}
            </el-tag>
            <el-tag v-else-if="field.source_role.includes('identifier')" size="small" type="info">
              {{ field.source_role === 'parent_identifier'
                ? t('transfer.taskWizard.mongoBuilder.parentIdentifier')
                : t('transfer.taskWizard.mongoBuilder.recordIdentifier') }}
            </el-tag>
            <el-tag v-else-if="field.source_role === 'parent_field'" size="small">
              {{ t('transfer.taskWizard.mongoBuilder.parentDocumentField') }}
            </el-tag>
            <el-tag v-else size="small" type="success">
              {{ field.source_role === 'array_element_field'
                ? t('transfer.taskWizard.mongoBuilder.arrayElement')
                : t('transfer.taskWizard.mongoBuilder.documentField') }}
            </el-tag>
            <span class="source-path">{{ field.source_role === 'array_index'
              ? t('transfer.taskWizard.mongoBuilder.arrayIndexSource', { path: field.source_path })
              : field.source_path }}</span>
          </div>
        </div>
      </section>

      <el-alert
        v-if="validationMessages.length > 0"
        type="error"
        :closable="false"
        :title="t('transfer.taskWizard.mongoBuilder.incompleteTitle')"
        :description="validationMessages.join('；')"
      />

      <el-collapse v-else class="mql-preview">
        <el-collapse-item name="mql">
          <template #title>{{ t('transfer.taskWizard.mongoBuilder.viewGeneratedMql') }}</template>
          <el-input :model-value="compiledStatement" type="textarea" :rows="5" readonly />
        </el-collapse-item>
      </el-collapse>
    </template>

    <template v-else>
      <el-alert
        type="info"
        :closable="false"
        :title="t('transfer.taskWizard.mongoBuilder.advancedHint')"
      />
      <el-input
        v-model="rawStatement"
        type="textarea"
        :rows="10"
        :placeholder="t('transfer.taskWizard.queryStatementPlaceholder')"
        @input="emitRawStatement"
      />
    </template>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  compileMongoStructureQuery,
  createMongoPathProjection,
  createMongoStructureQuery,
  defaultMongoIndexOutput,
  isMongoArrayElementLeafField,
  isMongoParentLeafField,
  isMongoProjectionLeafField,
  mongoStructureOutputFields,
  parseMongoStructureQuery,
  validateMongoStructureQuery
} from './mongoStructureQuery.mjs'

const props = defineProps({
  modelValue: { type: String, default: '' },
  collection: { type: String, default: '' },
  sourceFields: { type: Array, default: () => [] }
})

const emit = defineEmits(['update:modelValue'])
const { t } = useI18n()
const mode = ref('visual')
const rawStatement = ref('')
const unsupportedReason = ref('')
const draft = reactive(createMongoStructureQuery(props.collection))
let lastEmittedStatement = ''

const rowShape = computed(() => draft.unwind.enabled ? 'array' : 'document')
const arrayFieldOptions = computed(() => uniqueFields(props.sourceFields.filter(isArrayField)))
const documentFieldOptions = computed(() => uniqueFields(props.sourceFields.filter(field => {
  const name = cleanText(field?.name)
  if (!name || name === '_id' || !isMongoProjectionLeafField(field)) return false
  return !belongsToAnyArray(name)
})))
const parentFieldOptions = computed(() => uniqueFields(props.sourceFields.filter(field => (
  isMongoParentLeafField(field, props.sourceFields)
))))
const arrayElementFieldOptions = computed(() => uniqueFields(props.sourceFields.filter(field => (
  isMongoArrayElementLeafField(field, draft.unwind.path, props.sourceFields)
))))
const selectedDocumentSources = computed({
  get: () => draft.projections.filter(item => item.source !== '_id').map(item => item.source),
  set: values => replaceSelectedSources(values)
})
const selectedParentSources = computed({
  get: () => draft.projections
    .filter(item => item.source !== '_id' && !belongsToAnyArray(item.source))
    .map(item => item.source),
  set: values => replaceSelectedSources([...values, ...selectedArraySources.value])
})
const selectedArraySources = computed({
  get: () => draft.projections
    .filter(item => item.source.startsWith(`${draft.unwind.path}.`))
    .map(item => item.source),
  set: values => replaceSelectedSources([...selectedParentSources.value, ...values])
})
const validationIssues = computed(() => validateMongoStructureQuery(draft))
const validationMessages = computed(() => [...new Set(validationIssues.value.map(item => {
  const key = `transfer.taskWizard.mongoBuilder.validation.${item.code}`
  const translated = t(key)
  return translated === key ? item.code : translated
}))])
const compiledStatement = computed(() => {
  if (validationIssues.value.length > 0) return ''
  return compileMongoStructureQuery(draft)
})
const outputFields = computed(() => mongoStructureOutputFields(draft, props.sourceFields))

watch(
  () => props.modelValue,
  statement => {
    const text = cleanText(statement)
    if (text === lastEmittedStatement) return
    rawStatement.value = text
    if (!text) {
      replaceDraft(createMongoStructureQuery(props.collection))
      ensureIdentifierProjection()
      mode.value = 'visual'
      unsupportedReason.value = ''
      return
    }
    const parsed = parseMongoStructureQuery(text)
    if (parsed.supported) {
      replaceDraft(parsed.model)
      mode.value = 'visual'
      unsupportedReason.value = ''
      return
    }
    mode.value = 'advanced'
    unsupportedReason.value = parsed.reason
  },
  { immediate: true }
)

watch(
  () => props.collection,
  collection => {
    if (mode.value !== 'visual') return
    const nextCollection = cleanText(collection)
    if (nextCollection && nextCollection !== draft.collection) draft.collection = nextCollection
  }
)

watch(
  () => props.sourceFields,
  () => {
    if (mode.value !== 'visual') return
    ensureIdentifierProjection()
  },
  { deep: true }
)

watch(
  draft,
  () => {
    if (mode.value !== 'visual') return
    const statement = compiledStatement.value
    lastEmittedStatement = statement
    rawStatement.value = statement
    emit('update:modelValue', statement)
  },
  { deep: true }
)

function changeMode(nextMode) {
  if (nextMode === mode.value) return
  if (nextMode === 'advanced') {
    rawStatement.value = compiledStatement.value || cleanText(props.modelValue)
    mode.value = 'advanced'
    unsupportedReason.value = ''
    return
  }
  const parsed = parseMongoStructureQuery(rawStatement.value)
  if (!parsed.supported) {
    unsupportedReason.value = parsed.reason
    return
  }
  replaceDraft(parsed.model)
  mode.value = 'visual'
  unsupportedReason.value = ''
}

function changeRowShape(shape) {
  const portableProjections = draft.projections.filter(item => item.source === '_id' || !belongsToAnyArray(item.source))
  draft.projections = portableProjections
  if (shape === 'array') {
    draft.unwind = { enabled: true, path: '', includeIndex: false, indexOutput: '' }
    return
  }
  draft.unwind = { enabled: false, path: '', includeIndex: false, indexOutput: '' }
}

function changeArrayPath(path) {
  const parentSources = selectedParentSources.value
  draft.unwind.path = cleanText(path)
  draft.unwind.indexOutput = draft.unwind.includeIndex ? defaultMongoIndexOutput(path) : ''
  replaceSelectedSources(parentSources)
}

function changeIncludeIndex(value) {
  draft.unwind.includeIndex = value === true
  draft.unwind.indexOutput = value === true ? defaultMongoIndexOutput(draft.unwind.path) : ''
  if (value === true) replaceSelectedSources([...selectedParentSources.value, ...selectedArraySources.value])
}

function replaceSelectedSources(values) {
  const paths = [...new Set((Array.isArray(values) ? values : []).map(cleanText).filter(Boolean))]
  const previous = new Map(draft.projections.map(item => [item.source, item]))
  const projections = []
  const projectionPaths = ['_id', ...paths]
  projectionPaths.forEach(path => {
    const reserved = draft.unwind.includeIndex
      ? [...projections, { output: draft.unwind.indexOutput }]
      : projections
    const projection = createMongoPathProjection(path, props.sourceFields, reserved)
    if (previous.has(path)) projection.nullable = previous.get(path).nullable
    projections.push(projection)
  })
  draft.projections = projections
}

function ensureIdentifierProjection() {
  if (draft.projections.some(item => item.source === '_id')) return
  draft.projections = [createMongoPathProjection('_id', props.sourceFields), ...draft.projections]
}

function emitRawStatement() {
  lastEmittedStatement = cleanText(rawStatement.value)
  emit('update:modelValue', rawStatement.value)
}

function replaceDraft(model) {
  draft.collection = model.collection
  draft.unwind = { ...model.unwind }
  draft.projections = model.projections.map(item => ({ ...item }))
}

function belongsToAnyArray(name) {
  return arrayFieldOptions.value.some(field => name.startsWith(`${field.name}.`))
}

function isArrayField(field) {
  const type = cleanText(field?.type || field?.native_type).toLowerCase()
  return type === 'array'
}

function uniqueFields(fields) {
  const seen = new Set()
  return (Array.isArray(fields) ? fields : []).filter(field => {
    const name = cleanText(field?.name)
    const key = name.toLowerCase()
    if (!name || seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function cleanText(value) {
  return typeof value === 'string' ? value.trim() : ''
}
</script>

<style scoped>
.mongo-structure-builder {
  display: grid;
  gap: 12px;
}

.builder-toolbar,
.section-heading,
.automatic-field-row,
.selected-field-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.section-heading {
  justify-content: space-between;
}

.section-heading p,
.automatic-label {
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.section-heading p {
  margin: 4px 0 0;
}

.builder-section {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  background: var(--addp-bg-secondary);
}

.compact-section {
  grid-template-columns: 150px minmax(0, 1fr);
  align-items: center;
}

.field-control {
  display: grid;
  gap: 6px;
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.field-control small {
  color: var(--addp-text-tertiary);
  font-size: 12px;
  line-height: 1.5;
}

.source-path {
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--addp-text-primary);
}

.selected-field-list {
  display: grid;
  gap: 8px;
}

.selected-field-row {
  min-height: 32px;
  padding: 6px 10px;
  border-radius: 4px;
  background: var(--addp-bg-primary);
}

.selected-field-row :deep(.el-tag) {
  flex: 0 0 auto;
}

.mql-preview {
  border-top: none;
}

@media (max-width: 720px) {
  .compact-section {
    grid-template-columns: 1fr;
  }
}
</style>
