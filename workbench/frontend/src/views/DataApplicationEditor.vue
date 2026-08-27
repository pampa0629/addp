<template>
  <div class="page" v-loading="loading" data-testid="data-application-editor">
    <div class="page-header">
      <div><h2>{{ application.name || t('workbench.dataApplication') }}</h2><p>{{ t('workbench.dataApplicationEditorSubtitle') }}</p></div>
      <div class="actions">
        <el-button @click="router.push('/applications')">{{ t('workbench.cancel') }}</el-button>
        <el-button :loading="saving" type="primary" @click="save">{{ t('workbench.saveDraft') }}</el-button>
        <el-button v-if="application.publication_status === 'unpublished'" :disabled="dirty" :loading="publishing" type="success" @click="publish">{{ t('workbench.publish') }}</el-button>
        <el-button v-else-if="application.publication_status === 'offline' || application.has_unpublished_changes" :disabled="dirty" :loading="publishing" type="success" @click="publish">{{ t('workbench.publishRevision') }}</el-button>
        <el-button v-if="application.publication_status === 'published'" :loading="offlining" @click="offline">{{ t('workbench.offline') }}</el-button>
        <el-button v-if="application.publication_status === 'published'" @click="openRuntime">{{ t('workbench.run') }}</el-button>
      </div>
    </div>

    <el-alert v-if="dirty" type="warning" :closable="false" :title="t('workbench.saveBeforePublish')" />

    <el-card>
      <el-form label-position="top">
        <el-row :gutter="16">
          <el-col :xs="24" :md="12"><el-form-item :label="t('workbench.name')"><el-input v-model="application.name" maxlength="200" /></el-form-item></el-col>
          <el-col :xs="24" :md="12"><el-form-item :label="t('workbench.pageTitle')"><el-input v-model="application.snapshot.page.title" maxlength="200" /></el-form-item></el-col>
        </el-row>
        <el-form-item :label="t('workbench.description')"><el-input v-model="application.description" type="textarea" maxlength="2000" /></el-form-item>
      </el-form>
    </el-card>

    <el-card>
      <template #header><div class="card-header"><strong>{{ t('workbench.applicationParameters') }}</strong><span>{{ t('workbench.applicationParametersHint') }}</span></div></template>
      <el-empty v-if="application.snapshot.parameters.length === 0" :description="t('workbench.noApplicationParameters')" />
      <el-table v-else :data="application.snapshot.parameters">
        <el-table-column prop="key" :label="t('workbench.parameterKey')" min-width="190" />
        <el-table-column :label="t('workbench.parameterLabel')" min-width="180"><template #default="scope"><el-input v-model="scope.row.label" /></template></el-table-column>
        <el-table-column prop="control_type" :label="t('workbench.controlType')" width="130" />
        <el-table-column :label="t('workbench.defaultValue')" min-width="220">
          <template #default="scope"><ApplicationParameterInput v-model="scope.row.default_value" :control-type="scope.row.control_type" /></template>
        </el-table-column>
        <el-table-column :label="t('workbench.required')" width="100"><template #default="scope"><el-switch v-model="scope.row.required" /></template></el-table-column>
      </el-table>
    </el-card>

    <el-card>
      <template #header><div class="card-header"><strong>{{ t('workbench.parameterBindings') }}</strong><span>{{ t('workbench.parameterBindingsHint') }}</span></div></template>
      <el-table :data="bindingRows">
        <el-table-column prop="componentTitle" :label="t('workbench.component')" min-width="180" />
        <el-table-column prop="componentParameterLabel" :label="t('workbench.componentParameter')" min-width="180" />
        <el-table-column :label="t('workbench.applicationParameter')" min-width="240">
          <template #default="scope">
            <el-select v-model="scope.row.binding.application_parameter_key" class="full">
              <el-option v-for="parameter in compatibleParameters(scope.row.controlType)" :key="parameter.key" :label="parameter.label" :value="parameter.key" />
            </el-select>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card>
      <template #header><div class="card-header"><strong>{{ t('workbench.desktopLayout') }}</strong><span>{{ t('workbench.desktopLayoutHint') }}</span></div></template>
      <div class="components">
        <div v-for="component in application.snapshot.components" :key="component.id" class="component-card">
          <div class="component-heading"><div><strong>{{ component.title }}</strong><span>{{ t(`workbench.renderers.${component.renderer_type}`) }}</span></div><small>{{ component.service_ref.service_type }} · {{ component.service_ref.service_id }}</small></div>
          <el-form :inline="true" size="small">
            <el-form-item label="X"><el-input-number v-model="placement(component.id).x" :min="0" :max="11" /></el-form-item>
            <el-form-item label="Y"><el-input-number v-model="placement(component.id).y" :min="0" /></el-form-item>
            <el-form-item :label="t('workbench.width')"><el-input-number v-model="placement(component.id).width" :min="1" :max="12" /></el-form-item>
            <el-form-item :label="t('workbench.height')"><el-input-number v-model="placement(component.id).height" :min="1" :max="24" /></el-form-item>
          </el-form>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { computed, defineComponent, h, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElDatePicker, ElInput, ElInputNumber, ElMessage, ElMessageBox, ElOption, ElSelect, ElSwitch } from 'element-plus'
import { getDataApplication, offlineDataApplication, publishDataApplication, updateDataApplication } from '../api/dataApplications'
import { confirmDataApplicationAction, normalizedApplicationSnapshot } from '../utils/dataApplicationDraft.mjs'
import { navigateWorkbenchRoute } from '../utils/moduleNavigation'

const ApplicationParameterInput = defineComponent({
  props: { modelValue: { default: '' }, controlType: { type: String, required: true } },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const { t } = useI18n()
    const update = (value) => emit('update:modelValue', value)
    return () => {
      if (props.controlType === 'number') return h(ElInputNumber, { modelValue: props.modelValue, 'onUpdate:modelValue': update, controls: false })
      if (props.controlType === 'checkbox') return h(ElSwitch, { modelValue: Boolean(props.modelValue), 'onUpdate:modelValue': update })
      if (props.controlType === 'date' || props.controlType === 'datetime') return h(ElDatePicker, { modelValue: props.modelValue, 'onUpdate:modelValue': update, type: props.controlType === 'datetime' ? 'datetime' : 'date', valueFormat: props.controlType === 'datetime' ? 'YYYY-MM-DDTHH:mm:ssZ' : 'YYYY-MM-DD' })
      if (props.controlType === 'select') return h(ElSelect, { modelValue: props.modelValue, 'onUpdate:modelValue': update, clearable: true }, () => [h(ElOption, { value: true, label: t('workbench.booleanValues.true') }), h(ElOption, { value: false, label: t('workbench.booleanValues.false') })])
      if (props.controlType === 'multiselect') return h(ElSelect, { modelValue: props.modelValue, 'onUpdate:modelValue': update, multiple: true, filterable: true, allowCreate: true, defaultFirstOption: true })
      return h(ElInput, { modelValue: props.modelValue, 'onUpdate:modelValue': update })
    }
  },
})

const { t } = useI18n()
const route = useRoute()
const rawRouter = useRouter()
const router = { push: (location) => navigateWorkbenchRoute(rawRouter, location) }
const loading = ref(false)
const saving = ref(false)
const publishing = ref(false)
const offlining = ref(false)
const baseline = ref('')
const application = reactive({ name: '', description: '', version: 0, publication_status: 'unpublished', has_unpublished_changes: false, snapshot: { page: { title: '', placements: [] }, components: [], parameters: [], parameter_bindings: [] } })
const dirty = computed(() => baseline.value !== serializeDraft())
const bindingRows = computed(() => application.snapshot.parameter_bindings.map((binding) => {
  const component = application.snapshot.components.find((item) => item.id === binding.component_id)
  const componentParameter = component?.parameter_definitions?.find((item) => item.key === binding.component_parameter_key)
  return { binding, componentTitle: component?.title || '', componentParameterLabel: componentParameter?.label || binding.component_parameter_key, controlType: componentParameter?.control_type || '' }
}))

function serializeDraft() {
  return JSON.stringify({ name: application.name, description: application.description, snapshot: application.snapshot })
}

function assignApplication(data) {
  application.id = data.id
  application.name = data.name
  application.description = data.description || ''
  application.version = data.version
  application.publication_status = data.publication_status
  application.has_unpublished_changes = data.has_unpublished_changes
  application.current_revision_number = data.current_revision_number
  application.snapshot = structuredClone(data.snapshot)
  baseline.value = serializeDraft()
}

function compatibleParameters(controlType) {
  return application.snapshot.parameters.filter((parameter) => parameter.control_type === controlType)
}

function placement(componentID) {
  return application.snapshot.page.placements.find((item) => item.component_id === componentID)
}

function normalizedSnapshot() {
  return normalizedApplicationSnapshot(application.snapshot)
}

async function load() {
  loading.value = true
  try {
    const { data } = await getDataApplication(route.params.id)
    assignApplication(data)
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    const { data } = await updateDataApplication(application.id, { name: application.name.trim(), description: application.description.trim(), snapshot: normalizedSnapshot(), version: application.version })
    assignApplication(data)
    ElMessage.success(t('workbench.dataApplicationSaved'))
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function publish() {
  if (dirty.value) return ElMessage.warning(t('workbench.saveBeforePublish'))
  if (!await confirmDataApplicationAction(ElMessageBox.confirm, t('workbench.publishConfirm'))) return
  publishing.value = true
  try {
    const { data } = await publishDataApplication(application.id, application.version)
    assignApplication(data)
    ElMessage.success(t('workbench.published'))
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.saveFailed'))
  } finally {
    publishing.value = false
  }
}

async function offline() {
  if (!await confirmDataApplicationAction(ElMessageBox.confirm, t('workbench.offlineConfirm'))) return
  offlining.value = true
  try {
    const { data } = await offlineDataApplication(application.id, application.version)
    assignApplication(data)
    ElMessage.success(t('workbench.offlined'))
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('workbench.saveFailed'))
  } finally {
    offlining.value = false
  }
}

function openRuntime() {
  window.open(`/data-apps/${application.id}`, '_blank', 'noopener')
}

onMounted(load)
</script>

<style scoped>
.page{display:flex;flex-direction:column;gap:16px}.page-header,.actions,.card-header,.component-heading{display:flex;align-items:center}.page-header,.card-header,.component-heading{justify-content:space-between}.actions{gap:8px;flex-wrap:wrap}.page-header h2{margin:0;color:var(--addp-text-primary)}.page-header p,.card-header span,.component-heading span,.component-heading small{color:var(--addp-text-secondary)}.page-header p{margin:6px 0 0}.card-header span{font-size:12px}.full{width:100%}.components{display:flex;flex-direction:column;gap:12px}.component-card{padding:16px;border:1px solid var(--addp-border-color);border-radius:var(--addp-border-radius-base)}.component-heading{margin-bottom:12px}.component-heading div{display:flex;gap:12px;align-items:center}
</style>
