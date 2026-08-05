<template>
  <div class="execution-parameter-form">
    <section v-for="group in groups" :key="group.name" class="parameter-group">
      <h4>{{ group.title }}</h4>
      <div v-for="field in group.fields" :key="field.name" class="parameter-field">
        <div class="field-header">
          <span>{{ field.title }}</span>
          <el-radio-group :model-value="parameterMode(field)" size="small" @change="mode => setParameterMode(field, mode)">
            <el-radio-button value="workflow">{{ t('common.executionParameters.workflowConfiguration') }}</el-radio-button>
            <el-radio-button value="override">{{ t('common.executionParameters.executionOverride') }}</el-radio-button>
            <el-radio-button v-if="allowUpstream" value="upstream">{{ t('common.executionParameters.upstream') }}</el-radio-button>
          </el-radio-group>
        </div>

        <div v-if="parameterMode(field) === 'workflow'" class="default-value">
          <template v-if="field.control === 'resource_tree_picker'">
            <template v-if="resourceSummary(field, field.defaultValue).engineName">
              <span class="resource-engine">{{ resourceSummary(field, field.defaultValue).engineName }}</span>
              <span class="resource-separator">·</span>
            </template>
            <span class="resource-name">{{ resourceSummary(field, field.defaultValue).name }}</span>
            <span v-if="resourceSummary(field, field.defaultValue).type" class="resource-type">
              {{ resourceSummary(field, field.defaultValue).type }}
            </span>
          </template>
          <template v-else>{{ formatDefault(field.defaultValue) }}</template>
        </div>

        <el-select
          v-else-if="parameterMode(field) === 'upstream'"
          :model-value="upstreamValue(field)"
          :placeholder="t('common.executionParameters.selectOutput')"
          @change="value => setUpstreamValue(field, value)"
        >
          <el-option
            v-for="option in compatibleOutputs(outputBindingSchema(field))"
            :key="option.template"
            :label="option.label"
            :value="option.template"
          />
        </el-select>

        <template v-else-if="field.control === 'resource_tree_picker'">
          <div class="resource-value">
            <el-input
              :model-value="resourceDisplayText(field, getPath(overrides, field.path))"
              readonly
              :placeholder="t('common.executionParameters.noResource')"
            />
            <el-button type="primary" plain @click="openResourcePicker(field)">
              {{ t('common.executionParameters.selectResource') }}
            </el-button>
          </div>
          <div
            v-for="child in resourceChildFields(field)"
            :key="child.name"
            class="resource-child"
          >
            <label>{{ child.title }}</label>
            <template v-if="isGeometryChild(field, child)">
              <el-select
                :model-value="getPath(overrides, child.path)"
                :disabled="geometryColumnOptions(field, getPath(overrides, field.path)).length <= 1"
                :placeholder="t('common.executionParameters.geometryNotDetected')"
                @update:model-value="value => updatePath(child.path, value)"
              >
                <el-option
                  v-for="column in geometryColumnOptions(field, getPath(overrides, field.path))"
                  :key="column"
                  :label="column"
                  :value="column"
                />
              </el-select>
              <span class="resource-child-hint">
                {{ geometryColumnOptions(field, getPath(overrides, field.path)).length > 1
                  ? t('common.executionParameters.geometryMultiple')
                  : geometryColumnOptions(field, getPath(overrides, field.path)).length === 1
                    ? t('common.executionParameters.geometryDetected')
                    : t('common.executionParameters.geometryNotDetected') }}
              </span>
            </template>
            <SchemaExecutionInput
              v-else
              :schema="child.schema"
              :model-value="getPath(overrides, child.path)"
              @update:model-value="value => updatePath(child.path, value)"
            />
          </div>
        </template>

        <SchemaExecutionInput
          v-else
          :schema="field.schema"
          :model-value="getPath(overrides, field.path)"
          @update:model-value="value => updatePath(field.path, value)"
        />
        <p v-if="field.schema.description" class="field-description">{{ field.schema.description }}</p>
      </div>
    </section>

    <el-empty v-if="groups.length === 0" :description="t('common.executionParameters.empty')" :image-size="48" />

    <el-dialog v-model="pickerVisible" :title="activePicker?.title || t('common.executionParameters.selectResource')" width="min(720px, calc(100vw - 24px))">
      <ResourceTreePicker
        v-if="activePicker"
        :api-base-url="activePicker.ui.api_base_url || '/api/v1/meta'"
        :engine-families="activePicker.ui.engine_families || []"
        :initial-locator="resourceLocator(activePicker)"
        :selectable-filter="resourceSelectableFilter"
        tree-height="360px"
        @update:model-value="selection => pickerSelection = selection"
      />
      <template #footer>
        <el-button @click="pickerVisible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :disabled="!pickerSelection" @click="confirmResourceSelection">{{ t('common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, defineComponent, h, ref, resolveComponent, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Delete, Plus } from '@element-plus/icons-vue'
import ResourceTreePicker from './ResourceTreePicker.vue'
import { listResourceTreeEngines } from '../api/resourceTree.js'
import { sortEntriesByOrder, summarizeExecutionResource } from '../utils/executionParameterPresentation'
import { geometryColumnFactsFromSelection } from '../utils/resourceSelection.js'

const props = defineProps({
  contract: { type: Object, required: true },
  modelValue: { type: Object, default: () => ({}) },
  allowUpstream: { type: Boolean, default: false },
  upstreamOutputs: { type: Array, default: () => [] }
})
const emit = defineEmits(['update:modelValue'])
const { t, te } = useI18n()
const pickerVisible = ref(false)
const activePicker = ref(null)
const pickerSelection = ref(null)
const enginesById = ref({})
const engineCatalogLoading = ref(false)
const geometryColumnsByField = ref({})
let engineCatalogRequestSeq = 0

const SchemaExecutionInput = defineComponent({
  name: 'SchemaExecutionInput',
  props: { schema: { type: Object, required: true }, modelValue: { default: null } },
  emits: ['update:modelValue'],
  setup(inputProps, { emit: inputEmit }) {
    return () => {
      const schema = inputProps.schema || {}
	  if (schema.type === 'object') {
		const properties = schema.properties || {}
		const current = inputProps.modelValue && typeof inputProps.modelValue === 'object' && !Array.isArray(inputProps.modelValue)
		  ? inputProps.modelValue
		  : {}
		return h('div', { class: 'structured-object' }, Object.entries(properties).map(([name, childSchema]) =>
		  h('div', { class: 'structured-field', key: name }, [
			h('label', childSchema.title || name),
			h(SchemaExecutionInput, {
			  schema: childSchema,
			  modelValue: current[name],
			  'onUpdate:modelValue': value => inputEmit('update:modelValue', { ...current, [name]: value })
			})
		  ])
		))
	  }
	  if (schema.type === 'array') {
		const current = Array.isArray(inputProps.modelValue) ? inputProps.modelValue : []
		const itemSchema = schema.items || { type: 'string' }
		const rows = current.map((value, index) => h('div', { class: 'array-row', key: index }, [
		  h(SchemaExecutionInput, {
			schema: itemSchema,
			modelValue: value,
			'onUpdate:modelValue': nextValue => {
			  const next = [...current]
			  next[index] = nextValue
			  inputEmit('update:modelValue', next)
			}
		  }),
		  h(resolveComponent('el-button'), {
			icon: Delete,
			circle: true,
			title: t('common.executionParameters.removeItem'),
			'onClick': () => inputEmit('update:modelValue', current.filter((_, itemIndex) => itemIndex !== index))
		  })
		]))
		rows.push(h(resolveComponent('el-button'), {
		  icon: Plus,
		  plain: true,
		  disabled: Number.isFinite(schema.maxItems) && current.length >= schema.maxItems,
		  'onClick': () => inputEmit('update:modelValue', [...current, emptyValue(itemSchema)])
		}, () => t('common.executionParameters.addItem')))
		return h('div', { class: 'array-input' }, rows)
	  }
      if (Array.isArray(schema.enum)) {
        return h(resolveComponent('el-select'), {
          modelValue: inputProps.modelValue,
          clearable: true,
          'onUpdate:modelValue': value => inputEmit('update:modelValue', value)
        }, () => schema.enum.map(value => h(resolveComponent('el-option'), { label: String(value), value })))
      }
      if (schema.type === 'boolean') {
        return h(resolveComponent('el-switch'), {
          modelValue: Boolean(inputProps.modelValue),
          'onUpdate:modelValue': value => inputEmit('update:modelValue', value)
        })
      }
      if (schema.type === 'integer' || schema.type === 'number') {
        return h(resolveComponent('el-input-number'), {
          modelValue: inputProps.modelValue,
          min: schema.minimum,
          max: schema.maximum,
          step: schema.type === 'integer' ? 1 : 0.1,
          precision: schema.type === 'integer' ? 0 : undefined,
          'onUpdate:modelValue': value => inputEmit('update:modelValue', value)
        })
      }
      return h(resolveComponent('el-input'), {
        modelValue: inputProps.modelValue == null ? '' : inputProps.modelValue,
        'onUpdate:modelValue': value => inputEmit('update:modelValue', value)
      })
    }
  }
})

const overrides = computed(() => props.modelValue || {})
const groups = computed(() => {
  const schemaGroups = props.contract?.input_schema?.properties || {}
  const defaults = props.contract?.input_defaults || {}
  const uiGroups = props.contract?.input_ui_schema || {}
  const grouped = []
  const directFields = []
  sortEntriesByOrder(schemaGroups, uiGroups).forEach(([name, schema]) => {
    const ui = uiGroups[name] || {}
	if (schema.type !== 'object' || !schema.properties || ui.control !== 'group') {
	  directFields.push(executionField(name, schema, ui, [name], defaults?.[name]))
	  return
	}
	grouped.push({
	  name,
	  title: ui.title || schema.title || name,
	  fields: sortEntriesByOrder(schema.properties, ui.fields).map(([fieldName, fieldSchema]) => {
		const fieldUI = ui.fields?.[fieldName] || {}
		return executionField(fieldName, fieldSchema, fieldUI, [name, fieldName], defaults?.[name]?.[fieldName])
	  })
	})
  })
  if (directFields.length > 0) {
	grouped.unshift({
	  name: '$root',
	  title: props.contract?.input_schema?.title || t('common.executionParameters.title'),
	  fields: directFields
	})
  }
  return grouped
})

watch(groups, async currentGroups => {
  const requestSeq = ++engineCatalogRequestSeq
  const apiBaseUrls = Array.from(new Set(
    currentGroups.flatMap(group => group.fields)
      .filter(field => field.control === 'resource_tree_picker')
      .map(field => field.ui.api_base_url || '/api/v1/meta')
  ))
  if (apiBaseUrls.length === 0) {
    enginesById.value = {}
    engineCatalogLoading.value = false
    return
  }
  engineCatalogLoading.value = true
  const results = await Promise.allSettled(apiBaseUrls.map(apiBaseUrl => listResourceTreeEngines(apiBaseUrl)))
  if (requestSeq !== engineCatalogRequestSeq) return
  const next = {}
  for (const result of results) {
    if (result.status !== 'fulfilled') continue
    for (const engine of result.value || []) {
      if (engine?.id) next[engine.id] = engine
    }
  }
  enginesById.value = next
  engineCatalogLoading.value = false
}, { immediate: true })

function executionField(name, schema, ui, path, defaultValue) {
  return {
	name,
	title: ui.display_name || schema.title || name,
	schema,
	ui,
	control: ui.control || '',
	path,
	defaultValue
  }
}

function parameterMode(field) {
  if (!hasPath(overrides.value, field.path)) return 'workflow'
  return isOutputTemplate(upstreamValue(field)) ? 'upstream' : 'override'
}

function setParameterMode(field, mode) {
  if (mode === 'workflow') {
    clearGeometryColumnOptions(field)
    deletePath(field.path)
    return
  }
  if (mode === 'upstream') {
	const option = compatibleOutputs(outputBindingSchema(field))[0]
	setUpstreamValue(field, option?.template || '')
    return
  }
  clearGeometryColumnOptions(field)
  updatePath(field.path, cloneValue(field.defaultValue) ?? emptyValue(field.schema))
}

function clearGeometryColumnOptions(field) {
  if (field.control !== 'resource_tree_picker') return
  const key = field.path.join('.')
  const next = { ...geometryColumnsByField.value }
  delete next[key]
  geometryColumnsByField.value = next
}

function outputBindingSchema(field) {
  return field.control === 'resource_tree_picker' ? { type: 'string' } : field.schema
}

function upstreamValue(field) {
  if (field.control === 'resource_tree_picker') return resourceLocator(field)
  return getPath(overrides.value, field.path)
}

function setUpstreamValue(field, value) {
  if (field.control !== 'resource_tree_picker') {
	updatePath(field.path, value)
	return
  }
  const resourceValue = cloneValue(getPath(overrides.value, field.path) || field.defaultValue) || {}
  resourceValue[resourceLocatorName(field)] = value
  updatePath(field.path, resourceValue)
}

function updatePath(path, value) {
  const next = cloneValue(overrides.value) || {}
  let current = next
  path.slice(0, -1).forEach(name => {
    if (!current[name] || typeof current[name] !== 'object' || Array.isArray(current[name])) current[name] = {}
    current = current[name]
  })
  current[path[path.length - 1]] = value
  emit('update:modelValue', next)
}

function deletePath(path) {
  const next = cloneValue(overrides.value) || {}
  const parents = []
  let current = next
  for (const name of path.slice(0, -1)) {
    if (!current[name] || typeof current[name] !== 'object') return
    parents.push([current, name])
    current = current[name]
  }
  delete current[path[path.length - 1]]
  for (let index = parents.length - 1; index >= 0; index -= 1) {
    const [parent, name] = parents[index]
    if (Object.keys(parent[name]).length === 0) delete parent[name]
  }
  emit('update:modelValue', next)
}

function resourceChildFields(field) {
  const locatorName = resourceLocatorName(field)
  return Object.entries(field.schema.properties || {})
    .filter(([name]) => name !== locatorName)
    .map(([name, schema]) => ({ name, schema, title: schema.title || name, path: [...field.path, name] }))
}

function isGeometryChild(field, child) {
  return Boolean(field.ui?.resource_binding?.geometry_column_param) && child.name === 'geometry_column'
}

function geometryColumnOptions(field, value) {
  const detected = geometryColumnsByField.value[field.path.join('.')] || []
  if (detected.length > 0) return detected
  const saved = String(value?.geometry_column || '').trim()
  return saved ? [saved] : []
}

function resourceLocatorName(field) {
  return field.ui?.resource_binding?.mode === 'target' ? 'parent_locator' : 'locator'
}

function resourceLocator(field) {
  return getPath(overrides.value, [...field.path, resourceLocatorName(field)]) || ''
}

function resourceSummary(field, value) {
  const summary = summarizeExecutionResource(field, value, enginesById.value)
  if (summary.status === 'empty') {
    return { engineName: '', name: t('common.executionParameters.noResource'), type: '' }
  }
  if (summary.status === 'configured') {
    return { engineName: '', name: t('common.executionParameters.configuredResource'), type: '' }
  }
  const typeKey = `common.executionParameters.resourceTypes.${summary.type}`
  return {
    engineName: summary.engineName || (engineCatalogLoading.value
      ? t('common.executionParameters.engineLoading')
      : t('common.executionParameters.engineUnavailable')),
    name: summary.name || t('common.executionParameters.configuredResource'),
    type: summary.type && te(typeKey) ? t(typeKey) : ''
  }
}

function resourceDisplayText(field, value) {
  const summary = resourceSummary(field, value)
  return [summary.engineName, summary.name, summary.type].filter(Boolean).join(' · ')
}

function openResourcePicker(field) {
  activePicker.value = field
  pickerSelection.value = null
  pickerVisible.value = true
}

function confirmResourceSelection() {
  const locator = pickerSelection.value?.identity?.locator
  if (!locator || !activePicker.value) return
  const field = activePicker.value
  const resourceValue = cloneValue(getPath(overrides.value, field.path) || field.defaultValue) || {}
  resourceValue[resourceLocatorName(field)] = locator
  if (field.ui?.resource_binding?.geometry_column_param) {
    const facts = geometryColumnFactsFromSelection(pickerSelection.value)
    geometryColumnsByField.value = {
      ...geometryColumnsByField.value,
      [field.path.join('.')]: facts.columns
    }
    resourceValue.geometry_column = facts.selected || null
  }
  updatePath(field.path, resourceValue)
  pickerVisible.value = false
}

function resourceSelectableFilter(node) {
  const config = activePicker.value?.ui || {}
  const allowed = config.resource_binding?.mode === 'target'
    ? config.selectable_parent_node_types
    : config.selectable_node_types
  return !Array.isArray(allowed) || allowed.length === 0 || allowed.includes(node?.type)
}

function compatibleOutputs(schema) {
  return props.upstreamOutputs.filter(option => option.type === schema?.type)
}

function getPath(value, path) {
  return path.reduce((current, name) => current?.[name], value)
}

function hasPath(value, path) {
  let current = value
  for (const name of path) {
    if (!current || !Object.prototype.hasOwnProperty.call(current, name)) return false
    current = current[name]
  }
  return true
}

function isOutputTemplate(value) {
  return typeof value === 'string' && /^\s*\{\{[^{}]+\.outputs\.[^{}]+\}\}\s*$/.test(value)
}

function emptyValue(schema) {
  if (schema?.type === 'boolean') return false
  if (schema?.type === 'integer' || schema?.type === 'number') return 0
  if (schema?.type === 'array') return []
  if (schema?.type === 'object') return {}
  return ''
}

function formatDefault(value) {
  if (value === undefined) return t('common.executionParameters.noDefault')
  if (value && typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function cloneValue(value) {
  return value === undefined ? undefined : JSON.parse(JSON.stringify(value))
}
</script>

<style scoped>
.execution-parameter-form { display: grid; gap: 16px; }
.parameter-group { border-top: 1px solid var(--el-border-color-lighter); padding-top: 12px; }
.parameter-group h4 { margin: 0 0 12px; font-size: 14px; }
.parameter-field { display: grid; gap: 8px; margin-bottom: 16px; }
.field-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.default-value { min-height: 32px; padding: 7px 10px; color: var(--el-text-color-secondary); background: var(--el-fill-color-light); border-radius: 4px; overflow-wrap: anywhere; }
.resource-engine, .resource-name { color: var(--el-text-color-primary); }
.resource-engine { font-weight: 600; }
.resource-separator { margin: 0 6px; color: var(--el-text-color-placeholder); }
.resource-type { margin-left: 8px; color: var(--el-text-color-secondary); font-size: 12px; }
.resource-value { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; }
.resource-child { display: grid; gap: 6px; }
.resource-child label, .structured-field label, .field-description { color: var(--el-text-color-secondary); font-size: 12px; }
.resource-child-hint { color: var(--el-text-color-secondary); font-size: 12px; }
.structured-object, .array-input { display: grid; gap: 8px; }
.structured-field { display: grid; gap: 6px; }
.array-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; align-items: start; }
.field-description { margin: 0; }
@media (max-width: 640px) { .field-header { align-items: flex-start; flex-direction: column; } }
</style>
