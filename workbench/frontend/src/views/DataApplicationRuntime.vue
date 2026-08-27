<template>
  <div class="runtime" v-loading="loading" data-testid="data-application-runtime">
    <header class="runtime-header">
      <div><h1>{{ application.name }}</h1><p>{{ application.description }}</p></div>
      <div class="runtime-actions"><span>{{ t('workbench.revisionLabel', { revision: application.revision_number }) }}</span><el-button type="primary" :loading="queryingAll" @click="queryAll">{{ t('workbench.queryAll') }}</el-button></div>
    </header>

    <el-card v-if="application.snapshot.parameters?.length" class="parameters-card">
      <template #header><strong>{{ t('workbench.queryParameters') }}</strong></template>
      <div class="parameter-grid">
        <label v-for="parameter in application.snapshot.parameters" :key="parameter.key" class="parameter-field">
          <span>{{ parameter.label }}<em v-if="parameter.required">*</em></span>
          <RuntimeParameterInput v-model="parameterValues[parameter.key]" :control-type="parameter.control_type" />
        </label>
      </div>
    </el-card>

    <el-alert v-if="pageError" type="error" :closable="false" :title="pageError" />
    <main v-if="application.snapshot.page" class="runtime-grid">
      <el-card v-for="placement in application.snapshot.page.placements" :key="placement.component_id" class="runtime-component" :style="runtimeLayoutStyle(placement)">
        <template #header>
          <div class="component-header"><strong>{{ component(placement.component_id)?.title }}</strong><el-button link type="primary" :loading="state(placement.component_id).querying" :disabled="!state(placement.component_id).descriptor || Boolean(state(placement.component_id).error)" @click="queryComponent(placement.component_id)">{{ t('workbench.query') }}</el-button></div>
        </template>
        <el-alert v-if="state(placement.component_id).error" type="warning" :closable="false" :title="state(placement.component_id).error" />
        <WorkbenchRendererHost
          v-else
          :rows="state(placement.component_id).rows"
          :renderer-type="component(placement.component_id).renderer_type"
          :config="component(placement.component_id).renderer_config"
          :descriptor="state(placement.component_id).descriptor"
          :page="state(placement.component_id).page"
        />
        <div v-if="component(placement.component_id)?.renderer_type === 'table'" class="component-pagination">
          <el-button
            size="small"
            :disabled="state(placement.component_id).querying || state(placement.component_id).cursor_index === 0"
            @click="previousPage(placement.component_id)"
          >{{ t('workbench.previousPage') }}</el-button>
          <span>{{ t('workbench.pageNumber', { page: state(placement.component_id).cursor_index + 1 }) }}</span>
          <el-button
            size="small"
            :disabled="state(placement.component_id).querying || !state(placement.component_id).page?.has_more || !state(placement.component_id).page?.next_cursor"
            @click="nextPage(placement.component_id)"
          >{{ t('workbench.nextPage') }}</el-button>
        </div>
      </el-card>
    </main>
  </div>
</template>

<script setup>
import { defineComponent, h, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { ElDatePicker, ElInput, ElInputNumber, ElOption, ElSelect, ElSwitch } from 'element-plus'
import { getDataApplicationRuntime } from '../api/dataApplications'
import { executeDescriptorOperation, getConsumerDescriptor } from '../api/services'
import { buildComponentQuery, initialApplicationParameterValues, runtimeLayoutStyle } from '../utils/dataApplicationRuntime.mjs'
import WorkbenchRendererHost from '../components/WorkbenchRendererHost.vue'

const RuntimeParameterInput = defineComponent({
  props: { modelValue: { default: '' }, controlType: { type: String, required: true } },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const { t } = useI18n()
    const update = (value) => emit('update:modelValue', value)
    return () => {
      if (props.controlType === 'number') return h(ElInputNumber, { modelValue: props.modelValue, 'onUpdate:modelValue': update, controls: false })
      if (props.controlType === 'checkbox') return h(ElSwitch, { modelValue: Boolean(props.modelValue), 'onUpdate:modelValue': update })
      if (props.controlType === 'select') return h(ElSelect, { modelValue: props.modelValue, 'onUpdate:modelValue': update, clearable: true }, () => [h(ElOption, { value: true, label: t('workbench.booleanValues.true') }), h(ElOption, { value: false, label: t('workbench.booleanValues.false') })])
      if (props.controlType === 'multiselect') return h(ElSelect, { modelValue: props.modelValue, 'onUpdate:modelValue': update, multiple: true, filterable: true, allowCreate: true, defaultFirstOption: true })
      if (props.controlType === 'date' || props.controlType === 'datetime') return h(ElDatePicker, { modelValue: props.modelValue, 'onUpdate:modelValue': update, type: props.controlType === 'datetime' ? 'datetime' : 'date', valueFormat: props.controlType === 'datetime' ? 'YYYY-MM-DDTHH:mm:ssZ' : 'YYYY-MM-DD' })
      return h(ElInput, { modelValue: props.modelValue, 'onUpdate:modelValue': update })
    }
  },
})

const { t } = useI18n()
const route = useRoute()
const loading = ref(false)
const queryingAll = ref(false)
const pageError = ref('')
const application = reactive({ name: '', description: '', revision_number: 0, snapshot: { page: null, components: [], parameters: [], parameter_bindings: [] } })
const parameterValues = reactive({})
const componentStates = reactive({})

function component(id) {
  return application.snapshot.components.find((item) => item.id === id)
}

function state(id) {
  return componentStates[id] || { rows: [], page: {}, descriptor: null, error: '', querying: false, cursors: [''], cursor_index: 0 }
}

async function load() {
  loading.value = true
  pageError.value = ''
  try {
    const { data } = await getDataApplicationRuntime(route.params.id)
    Object.assign(application, data)
    Object.assign(parameterValues, initialApplicationParameterValues(data.snapshot))
    await Promise.all(data.snapshot.components.map(loadDescriptor))
  } catch (error) {
    pageError.value = error?.response?.data?.error || t('workbench.loadFailed')
  } finally {
    loading.value = false
  }
}

async function loadDescriptor(item) {
  componentStates[item.id] = { rows: [], page: { has_more: false, next_cursor: '' }, descriptor: null, error: '', querying: false, cursors: [''], cursor_index: 0 }
  try {
    const { data } = await getConsumerDescriptor(item.service_ref)
    if (data.contract_fingerprint !== item.contract_fingerprint) {
      componentStates[item.id].error = t('workbench.runtimeContractChanged')
      return
    }
    componentStates[item.id].descriptor = data
  } catch (error) {
    componentStates[item.id].error = error?.response?.data?.error || t('workbench.loadFailed')
  }
}

async function queryComponent(componentID, cursor = '', cursorIndex = 0, cursors = ['']) {
  const item = component(componentID)
  const current = componentStates[componentID]
  if (!item || !current?.descriptor) return
  current.querying = true
  current.error = ''
  try {
    const operation = current.descriptor.operations.find((candidate) => candidate.key === 'query')
    const { data } = await executeDescriptorOperation(operation, buildComponentQuery(application.snapshot, item, parameterValues, cursor))
    current.rows = data.data || []
    current.page = data.page || { has_more: false, next_cursor: '' }
    current.cursors = cursors
    current.cursor_index = cursorIndex
  } catch (error) {
    current.error = error?.response?.data?.error || (String(error?.message || '').startsWith('missing required') ? t('workbench.requiredParameterMissing') : t('workbench.queryFailed'))
  } finally {
    current.querying = false
  }
}

async function nextPage(componentID) {
  const current = componentStates[componentID]
  const nextCursor = current?.page?.next_cursor
  if (!nextCursor) return
  const nextIndex = current.cursor_index + 1
  const cursors = current.cursors.slice(0, nextIndex)
  cursors[nextIndex] = nextCursor
  await queryComponent(componentID, nextCursor, nextIndex, cursors)
}

async function previousPage(componentID) {
  const current = componentStates[componentID]
  if (!current || current.cursor_index === 0) return
  const previousIndex = current.cursor_index - 1
  await queryComponent(componentID, current.cursors[previousIndex], previousIndex, current.cursors)
}

async function queryAll() {
  queryingAll.value = true
  try {
    await Promise.all(application.snapshot.components.map((item) => queryComponent(item.id)))
  } finally {
    queryingAll.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.runtime{min-height:100vh;padding:24px;background:var(--addp-bg-secondary);box-sizing:border-box}.runtime-header,.runtime-actions,.component-header{display:flex;align-items:center;justify-content:space-between}.runtime-header{margin-bottom:16px;gap:24px}.runtime-header h1{margin:0;color:var(--addp-text-primary);font-size:28px}.runtime-header p{margin:6px 0 0;color:var(--addp-text-secondary)}.runtime-actions{gap:16px;color:var(--addp-text-secondary)}.parameters-card{margin-bottom:16px}.parameter-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:16px}.parameter-field{display:flex;flex-direction:column;gap:8px;color:var(--addp-text-primary)}.parameter-field em{color:var(--el-color-danger);font-style:normal}.runtime-grid{display:grid;grid-template-columns:repeat(12,minmax(0,1fr));grid-auto-rows:64px;gap:12px}.runtime-component{min-width:0;overflow:auto}.component-header{gap:12px}.component-pagination{display:flex;align-items:center;justify-content:flex-end;gap:12px;margin-top:12px;color:var(--addp-text-secondary)}@media(max-width:900px){.runtime{padding:16px}.runtime-header{align-items:flex-start;flex-direction:column}.runtime-grid{display:flex;flex-direction:column}.runtime-component{min-height:360px}}
</style>
