<template>
  <div class="relational-sql-builder">
    <div class="builder-toolbar">
      <el-radio-group :model-value="mode" @change="changeMode">
        <el-radio-button value="visual">{{ t('transfer.taskWizard.sqlBuilder.visualMode') }}</el-radio-button>
        <el-radio-button value="advanced">{{ t('transfer.taskWizard.sqlBuilder.advancedMode') }}</el-radio-button>
      </el-radio-group>
    </div>

    <el-alert
      v-if="unsupportedReason"
      type="warning"
      :closable="false"
      :title="t('transfer.taskWizard.sqlBuilder.unsupportedTitle')"
      :description="t('transfer.taskWizard.sqlBuilder.unsupportedDescription')"
    />

    <template v-if="mode === 'visual'">
      <section class="builder-section compact-section">
        <strong>{{ t('transfer.taskWizard.sqlBuilder.sourceTable') }}</strong>
        <code>{{ sourceLabel }}</code>
      </section>

      <section class="builder-section">
        <div class="section-heading">
          <div>
            <strong>{{ t('transfer.taskWizard.sqlBuilder.outputFields') }}</strong>
            <p>{{ t('transfer.taskWizard.sqlBuilder.outputFieldsHint') }}</p>
          </div>
          <div class="section-actions">
            <el-button size="small" @click="selectAllFields">
              {{ t('transfer.taskWizard.sqlBuilder.selectAll') }}
            </el-button>
            <el-button size="small" @click="clearSelectedFields">
              {{ t('transfer.taskWizard.sqlBuilder.clearSelection') }}
            </el-button>
          </div>
        </div>
        <el-input
          v-model="fieldSearch"
          clearable
          :placeholder="t('transfer.taskWizard.sqlBuilder.searchFields')"
        />
        <el-checkbox-group :model-value="draft.selectedFields" class="field-grid" @change="changeSelectedFields">
          <el-checkbox
            v-for="field in visibleFields"
            :key="field.name"
            :value="field.name"
            class="field-option"
          >
            <span class="field-name">{{ field.name }}</span>
            <el-tag size="small" type="info">{{ fieldTypeLabel(field) }}</el-tag>
          </el-checkbox>
        </el-checkbox-group>
      </section>

      <section class="builder-section">
        <div class="section-heading">
          <div>
            <strong>{{ t('transfer.taskWizard.sqlBuilder.filters') }}</strong>
            <p>{{ t('transfer.taskWizard.sqlBuilder.filtersHint') }}</p>
          </div>
          <el-select v-model="draft.matchMode" class="match-mode-select">
            <el-option :label="t('transfer.taskWizard.sqlBuilder.matchAll')" value="all" />
            <el-option :label="t('transfer.taskWizard.sqlBuilder.matchAny')" value="any" />
          </el-select>
        </div>

        <el-alert
          v-if="!parametersSupported"
          type="info"
          :closable="false"
          :title="t('transfer.taskWizard.sqlBuilder.parametersUnsupported')"
        />

        <div v-for="(filter, index) in draft.filters" :key="index" class="filter-row">
          <el-select
            :model-value="filter.field"
            filterable
            :placeholder="t('transfer.taskWizard.sqlBuilder.filterField')"
            @change="changeFilterField(index, $event)"
          >
            <el-option
              v-for="field in filterableFields"
              :key="field.name"
              :label="field.name"
              :value="field.name"
            />
          </el-select>
          <el-select
            :model-value="filter.operator"
            :placeholder="t('transfer.taskWizard.sqlBuilder.filterOperator')"
            @change="changeFilterOperator(index, $event)"
          >
            <el-option
              v-for="operator in operatorOptions(filter.field)"
              :key="operator"
              :label="t(`transfer.taskWizard.sqlBuilder.operators.${operator}`)"
              :value="operator"
            />
          </el-select>
          <span v-if="isNullOperator(filter.operator)" class="no-value-placeholder">—</span>
          <el-select
            v-else-if="filter.operator === 'in'"
            v-model="filter.value"
            multiple
            filterable
            allow-create
            default-first-option
            :placeholder="t('transfer.taskWizard.sqlBuilder.filterValues')"
          />
          <el-input-number
            v-else-if="fieldKind(filter.field) === 'number'"
            v-model="filter.value"
            controls-position="right"
            :placeholder="t('transfer.taskWizard.sqlBuilder.filterValue')"
          />
          <el-select
            v-else-if="fieldKind(filter.field) === 'boolean'"
            v-model="filter.value"
            :placeholder="t('transfer.taskWizard.sqlBuilder.filterValue')"
          >
            <el-option :label="t('transfer.taskWizard.sqlBuilder.booleanTrue')" :value="true" />
            <el-option :label="t('transfer.taskWizard.sqlBuilder.booleanFalse')" :value="false" />
          </el-select>
          <el-date-picker
            v-else-if="fieldKind(filter.field) === 'date'"
            v-model="filter.value"
            type="date"
            value-format="YYYY-MM-DD"
            :placeholder="t('transfer.taskWizard.sqlBuilder.filterValue')"
          />
          <el-time-picker
            v-else-if="fieldKind(filter.field) === 'time'"
            v-model="filter.value"
            value-format="HH:mm:ss"
            :placeholder="t('transfer.taskWizard.sqlBuilder.filterValue')"
          />
          <el-date-picker
            v-else-if="fieldKind(filter.field) === 'timestamp'"
            v-model="filter.value"
            type="datetime"
            value-format="YYYY-MM-DD HH:mm:ss"
            :placeholder="t('transfer.taskWizard.sqlBuilder.filterValue')"
          />
          <el-input
            v-else
            v-model="filter.value"
            :placeholder="t('transfer.taskWizard.sqlBuilder.filterValue')"
          />
          <el-button type="danger" plain @click="removeFilter(index)">
            {{ t('transfer.taskWizard.sqlBuilder.removeFilter') }}
          </el-button>
        </div>

        <el-button :disabled="!parametersSupported || filterableFields.length === 0" @click="addFilter">
          {{ t('transfer.taskWizard.sqlBuilder.addFilter') }}
        </el-button>
      </section>

      <el-alert
        v-if="validationMessages.length > 0"
        type="error"
        :closable="false"
        :title="t('transfer.taskWizard.sqlBuilder.incompleteTitle')"
        :description="validationMessages.join('；')"
      />

      <el-collapse v-else class="sql-preview">
        <el-collapse-item name="sql">
          <template #title>{{ t('transfer.taskWizard.sqlBuilder.viewGeneratedSql') }}</template>
          <el-input :model-value="compiled.statement" type="textarea" :rows="5" readonly />
          <el-input
            v-if="Object.keys(compiled.parameters).length > 0"
            :model-value="JSON.stringify(compiled.parameters, null, 2)"
            type="textarea"
            :rows="3"
            readonly
            class="parameter-preview"
          />
        </el-collapse-item>
      </el-collapse>
    </template>

    <template v-else>
      <el-alert
        type="info"
        :closable="false"
        :title="t('transfer.taskWizard.sqlBuilder.advancedHint')"
      />
      <el-input
        v-model="rawStatement"
        type="textarea"
        :rows="10"
        :placeholder="t('transfer.taskWizard.queryStatementPlaceholder')"
        @input="emitAdvancedQuery"
      />
      <el-input
        v-model="rawParametersText"
        type="textarea"
        :rows="3"
        :placeholder="t('transfer.taskWizard.queryParametersPlaceholder')"
        @input="emitAdvancedQuery"
      />
      <div v-if="advancedParameterError" class="query-error">{{ advancedParameterError }}</div>
    </template>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { normalizeFieldType } from '@addp/common-frontend'
import {
  compileRelationalSQLQuery,
  createRelationalSQLFilter,
  createRelationalSQLQuery,
  parseRelationalSQLQuery,
  relationalSQLFieldKind,
  relationalSQLFilterOperators,
  relationalSQLOutputFields,
  validateRelationalSQLQuery
} from './relationalSQLQuery.mjs'

const props = defineProps({
  modelValue: { type: String, default: '' },
  parameters: { type: Object, default: () => ({}) },
  sourcePath: { type: Array, default: () => [] },
  sourceFields: { type: Array, default: () => [] },
  identifierQuote: { type: String, default: '' },
  parametersSupported: { type: Boolean, default: false }
})

const emit = defineEmits(['update:query'])
const { t } = useI18n()
const mode = ref('visual')
const fieldSearch = ref('')
const rawStatement = ref('')
const rawParametersText = ref('{}')
const advancedParameterError = ref('')
const unsupportedReason = ref('')
const draft = reactive(createRelationalSQLQuery(props.sourcePath, props.sourceFields))
let lastEmittedProps = ''

const sourceLabel = computed(() => props.sourcePath.join('.'))
const sourceFieldMap = computed(() => new Map(props.sourceFields.map(field => [cleanText(field?.name).toLowerCase(), field])))
const visibleFields = computed(() => {
  const search = cleanText(fieldSearch.value).toLowerCase()
  return uniqueFields(props.sourceFields).filter(field => !search || field.name.toLowerCase().includes(search))
})
const filterableFields = computed(() => uniqueFields(props.sourceFields).filter(field => relationalSQLFilterOperators(field).length > 0))
const validationIssues = computed(() => validateRelationalSQLQuery(draft, props.sourceFields, props.parametersSupported))
const validationMessages = computed(() => [...new Set(validationIssues.value.map(item => {
  const key = `transfer.taskWizard.sqlBuilder.validation.${item.code}`
  const translated = t(key)
  return translated === key ? item.code : translated
}))])
const compiled = computed(() => {
  if (validationIssues.value.length > 0) return { statement: '', parameters: {} }
  return compileRelationalSQLQuery(draft, compilerOptions())
})

watch(
  () => [props.modelValue, props.parameters, props.sourcePath, props.sourceFields, props.identifierQuote, props.parametersSupported],
  () => hydrateFromProps(),
  { immediate: true, deep: true }
)

watch(
  draft,
  () => {
    if (mode.value !== 'visual') return
    emitVisualQuery()
  },
  { deep: true, immediate: true }
)

function hydrateFromProps() {
  const signature = propsSignature(props.modelValue, props.parameters)
  if (signature === lastEmittedProps) return
  const statement = cleanText(props.modelValue)
  if (!statement) {
    replaceDraft(createRelationalSQLQuery(props.sourcePath, props.sourceFields))
    rawStatement.value = ''
    rawParametersText.value = '{}'
    mode.value = 'visual'
    unsupportedReason.value = ''
    return
  }
  const parsed = parseRelationalSQLQuery(statement, props.parameters, compilerOptions())
  if (parsed.supported) {
    replaceDraft(parsed.model)
    rawStatement.value = statement
    rawParametersText.value = JSON.stringify(props.parameters || {}, null, 2)
    mode.value = 'visual'
    unsupportedReason.value = ''
    return
  }
  rawStatement.value = props.modelValue
  rawParametersText.value = JSON.stringify(props.parameters || {}, null, 2)
  mode.value = 'advanced'
  unsupportedReason.value = parsed.reason
}

function changeMode(nextMode) {
  if (nextMode === mode.value) return
  if (nextMode === 'advanced') {
    rawStatement.value = compiled.value.statement || cleanText(props.modelValue)
    rawParametersText.value = JSON.stringify(compiled.value.parameters || props.parameters || {}, null, 2)
    mode.value = 'advanced'
    unsupportedReason.value = ''
    emitAdvancedQuery()
    return
  }
  const parameters = parseAdvancedParameters()
  if (!parameters) return
  const parsed = parseRelationalSQLQuery(rawStatement.value, parameters, compilerOptions())
  if (!parsed.supported) {
    unsupportedReason.value = parsed.reason
    return
  }
  replaceDraft(parsed.model)
  mode.value = 'visual'
  unsupportedReason.value = ''
  emitVisualQuery()
}

function selectAllFields() {
  draft.selectedFields = uniqueFields(props.sourceFields).map(field => field.name)
}

function clearSelectedFields() {
  draft.selectedFields = []
}

function changeSelectedFields(values) {
  const selected = new Set((Array.isArray(values) ? values : []).map(value => cleanText(value).toLowerCase()))
  draft.selectedFields = uniqueFields(props.sourceFields).filter(field => selected.has(field.name.toLowerCase())).map(field => field.name)
}

function addFilter() {
  const field = filterableFields.value[0]
  if (!field) return
  draft.filters.push(createRelationalSQLFilter(field.name, props.sourceFields))
}

function removeFilter(index) {
  draft.filters.splice(index, 1)
}

function changeFilterField(index, fieldName) {
  const filter = draft.filters[index]
  if (!filter) return
  const field = sourceField(fieldName)
  const operators = relationalSQLFilterOperators(field)
  filter.field = field?.name || ''
  filter.operator = operators[0] || ''
  filter.value = undefined
}

function changeFilterOperator(index, operator) {
  const filter = draft.filters[index]
  if (!filter) return
  filter.operator = operator
  filter.value = operator === 'in' ? [] : undefined
}

function operatorOptions(fieldName) {
  return relationalSQLFilterOperators(sourceField(fieldName))
}

function fieldKind(fieldName) {
  return relationalSQLFieldKind(sourceField(fieldName))
}

function fieldTypeLabel(field) {
  const type = normalizeFieldType(field, 'string')
  const key = `transfer.taskWizard.fieldType.${type}`
  const translated = t(key)
  return translated === key ? type : translated
}

function sourceField(name) {
  return sourceFieldMap.value.get(cleanText(name).toLowerCase()) || null
}

function isNullOperator(operator) {
  return operator === 'is_null' || operator === 'is_not_null'
}

function emitVisualQuery() {
  const query = compiled.value
  const fields = relationalSQLOutputFields(draft, props.sourceFields)
  lastEmittedProps = propsSignature(query.statement, query.parameters)
  rawStatement.value = query.statement
  rawParametersText.value = JSON.stringify(query.parameters, null, 2)
  emit('update:query', {
    statement: query.statement,
    parameters: query.parameters,
    fields,
    valid: validationIssues.value.length === 0
  })
}

function emitAdvancedQuery() {
  const parameters = parseAdvancedParameters()
  emit('update:query', {
    statement: rawStatement.value,
    parameters: parameters || {},
    fields: null,
    valid: !!cleanText(rawStatement.value) && !!parameters
  })
  if (parameters) lastEmittedProps = propsSignature(rawStatement.value, parameters)
}

function parseAdvancedParameters() {
  advancedParameterError.value = ''
  try {
    const parsed = JSON.parse(rawParametersText.value.trim() || '{}')
    if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
      throw new Error(t('transfer.taskWizard.queryParametersObjectRequired'))
    }
    return parsed
  } catch (error) {
    advancedParameterError.value = error.message || t('transfer.taskWizard.queryParametersInvalid')
    return null
  }
}

function replaceDraft(model) {
  draft.sourcePath = [...model.sourcePath]
  draft.selectedFields = [...model.selectedFields]
  draft.matchMode = model.matchMode
  draft.filters = model.filters.map(filter => ({ ...filter, value: Array.isArray(filter.value) ? [...filter.value] : filter.value }))
}

function compilerOptions() {
  return {
    sourcePath: props.sourcePath,
    sourceFields: props.sourceFields,
    identifierQuote: props.identifierQuote,
    parametersSupported: props.parametersSupported
  }
}

function uniqueFields(fields) {
  const seen = new Set()
  return (Array.isArray(fields) ? fields : []).flatMap(field => {
    const name = cleanText(field?.name)
    const key = name.toLowerCase()
    if (!name || seen.has(key)) return []
    seen.add(key)
    return [{ ...field, name }]
  })
}

function querySignature(statement, parameters) {
  return `${String(statement || '')}\n${JSON.stringify(parameters || {})}`
}

function propsSignature(statement, parameters) {
  const fields = uniqueFields(props.sourceFields).map(field => ({
    name: field.name,
    type: normalizeFieldType(field, '')
  }))
  return `${querySignature(statement, parameters)}\n${JSON.stringify({
    sourcePath: props.sourcePath,
    fields,
    identifierQuote: props.identifierQuote,
    parametersSupported: props.parametersSupported
  })}`
}

function cleanText(value) {
  return typeof value === 'string' ? value.trim() : ''
}
</script>

<style scoped>
.relational-sql-builder {
  display: grid;
  gap: 12px;
}

.builder-toolbar,
.section-heading,
.section-actions,
.filter-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.section-heading {
  justify-content: space-between;
}

.section-heading p {
  margin: 4px 0 0;
  color: var(--addp-text-secondary);
  font-size: 12px;
}

.builder-section {
  display: grid;
  gap: 12px;
  padding: 16px;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-secondary);
}

.compact-section {
  grid-template-columns: 150px minmax(0, 1fr);
  align-items: center;
}

.compact-section code {
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--addp-text-primary);
}

.field-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 8px;
  max-height: 240px;
  overflow: auto;
}

.field-option {
  min-width: 0;
  margin-right: 0;
  padding: 8px 10px;
  border-radius: 4px;
  background: var(--addp-bg-primary);
}

.field-option :deep(.el-checkbox__label) {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.field-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.match-mode-select {
  width: 180px;
}

.filter-row > :deep(.el-select),
.filter-row > :deep(.el-input),
.filter-row > :deep(.el-input-number),
.filter-row > :deep(.el-date-editor) {
  flex: 1 1 0;
  min-width: 0;
  width: auto;
}

.no-value-placeholder {
  flex: 1 1 0;
  text-align: center;
  color: var(--addp-text-tertiary);
}

.parameter-preview {
  margin-top: 8px;
}

.query-error {
  color: var(--el-color-danger);
  font-size: 12px;
}

@media (max-width: 900px) {
  .section-heading,
  .filter-row {
    align-items: stretch;
    flex-direction: column;
  }

  .section-actions,
  .match-mode-select,
  .filter-row > :deep(.el-select),
  .filter-row > :deep(.el-input),
  .filter-row > :deep(.el-input-number),
  .filter-row > :deep(.el-date-editor) {
    width: 100%;
  }
}

@media (max-width: 720px) {
  .compact-section {
    grid-template-columns: 1fr;
  }
}
</style>
