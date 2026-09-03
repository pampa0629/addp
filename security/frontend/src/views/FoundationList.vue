<template>
  <section class="foundation-list" :class="{ embedded }">
    <div class="page-header">
      <div v-if="!embedded">
        <h2>{{ title }}</h2>
        <p>{{ description }}</p>
      </div>
      <p v-else class="section-description">{{ description }}</p>
      <el-button v-if="can('create')" type="primary" @click="openCreate">
        {{ t('security.common.createResource', { name: title }) }}
      </el-button>
    </div>

    <div class="table-panel">
      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column
          v-for="column in columns"
          :key="column.prop"
          :prop="column.prop"
          :label="t(column.label)"
          :min-width="column.width || 120"
          show-overflow-tooltip
        >
          <template #default="{ row }">
            <el-tag v-if="column.prop === 'enabled'" size="small" :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? t('security.common.enabled') : t('security.common.disabled') }}
            </el-tag>
            <span v-else>{{ displayValue(column, row[column.prop]) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-if="can('update')" link @click="openEdit(row)">{{ t('security.common.edit') }}</el-button>
            <el-button v-if="can('delete')" link type="danger" @click="remove(row)">{{ t('security.common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && rows.length === 0" :description="t('security.common.empty')" />
    </div>

    <el-dialog
      v-model="dialog"
      :title="editing ? t('security.common.editResource', { name: title }) : t('security.common.createResource', { name: title })"
      width="600px"
    >
      <el-form label-width="170px">
        <el-form-item
          v-for="field in visibleFields"
          :key="field.key"
          :label="t(field.label)"
          :required="field.required"
        >
          <el-select
            v-if="field.reference"
            v-model="form[field.key]"
            class="wide"
            filterable
            :clearable="field.nullable"
            :placeholder="t('security.common.selectPlaceholder', { name: t(field.label) })"
          >
            <el-option
              v-for="option in referenceOptions(field)"
              :key="option.id"
              :value="Number(option.id)"
              :label="referenceOptionLabel(option)"
            />
          </el-select>
          <el-switch v-else-if="field.type === 'boolean'" v-model="form[field.key]" />
          <el-input-number
            v-else-if="field.type === 'number'"
            v-model="form[field.key]"
            :min="field.min ?? 0"
            :max="field.max"
            class="wide"
          />
          <el-input v-else v-model="form[field.key]" :type="field.type === 'textarea' ? 'textarea' : 'text'" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">{{ t('security.common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('security.common.save') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { classificationAPI, gradeAPI } from '../api/security'
import { useAuthStore } from '../store/auth'
import { buildFoundationPayload, initialFoundationFieldValue, sortFoundationRows } from '../utils/foundationForm.mjs'

const props = defineProps({
  resourceKey: { type: String, default: '' },
  embedded: { type: Boolean, default: false }
})
const { t } = useI18n()
const route = useRoute()
const auth = useAuthStore()
const resource = computed(() => props.resourceKey || route.meta.resource)
const references = reactive({ classification: [] })
const referenceAPIs = { classification: classificationAPI }

function label(key) { return `security.fields.${key}` }
function text(key, required = false, type = 'text') { return { key, label: label(key), required, type } }
function number(key, required = false, min = 0, max) { return { key, label: label(key), required, type: 'number', min, max } }
function reference(key, target, required = false, nullable = false) { return { key, label: label(key), required, nullable, reference: target } }
function column(prop, options = {}) { return { prop, label: label(prop), ...options } }

const specs = {
  classification: {
    api: classificationAPI,
    permission: 'classification',
    fields: [text('code', true), text('name', true), text('description', false, 'textarea'), reference('parent_id', 'classification', false, true), number('sort_order')],
    columns: [column('code'), column('name'), column('parent_id', { reference: 'classification' }), column('sort_order'), column('description', { width: 220 })]
  },
  grade: {
    api: gradeAPI,
    permission: 'grade',
    fields: [text('code', true), text('name', true), text('description', false, 'textarea'), number('risk_order', true, 1)],
    columns: [column('code'), column('name'), column('risk_order'), column('description', { width: 260 })]
  }
}

const spec = computed(() => specs[resource.value])
const title = computed(() => t(`security.resources.${resource.value}`))
const description = computed(() => t(`security.descriptions.${resource.value}`))
const fields = computed(() => spec.value.fields)
const columns = computed(() => spec.value.columns)
const rows = ref([])
const loading = ref(false)
const saving = ref(false)
const dialog = ref(false)
const editing = ref(null)
const form = reactive({})
const visibleFields = computed(() => fields.value)

function can(action) {
  return auth.hasPermission(`security.${spec.value.permission}.${action}`)
}

function referenceOptionLabel(option) {
  const name = String(option?.name || '').trim()
  const code = String(option?.code || '').trim()
  return name && code ? t('security.common.referenceOption', { name, code }) : name || code || String(option?.id || '')
}

function referenceOptions(field) {
  const options = references[field.reference] || []
  if (field.key !== 'parent_id' || !editing.value) return options
  return options.filter(option => String(option.id) !== String(editing.value.id))
}

function referenceLabel(target, value) {
  if (value === null || value === undefined || value === '') return t('security.common.notAvailable')
  const option = (references[target] || []).find(item => String(item.id) === String(value))
  return option ? referenceOptionLabel(option) : t('security.common.unknownReference', { id: value })
}

function displayValue(column, value) {
  if (column.reference) return referenceLabel(column.reference, value)
  return value === null || value === undefined || value === '' ? t('security.common.notAvailable') : value
}

function reset(values = {}) {
  Object.keys(form).forEach(key => delete form[key])
  fields.value.forEach(field => {
    const value = initialFoundationFieldValue(field, values)
    form[field.key] = field.reference && value !== null && value !== '' ? Number(value) : value
  })
  if (values.version) form.version = Number(values.version)
}

function openCreate() {
  editing.value = null
  reset()
  dialog.value = true
}

function openEdit(row) {
  editing.value = row
  reset(row)
  dialog.value = true
}

async function load() {
  loading.value = true
  try {
    const referenceTargets = [...new Set(fields.value.filter(field => field.reference).map(field => field.reference))]
    const dataPromise = spec.value.api.list()
    const [data, ...referenceRows] = await Promise.all([
      dataPromise,
      ...referenceTargets.map(target => target === resource.value ? dataPromise : referenceAPIs[target].list())
    ])
    rows.value = sortFoundationRows(resource.value, Array.isArray(data) ? data : [])
    referenceTargets.forEach((target, index) => {
      references[target] = sortFoundationRows(target, Array.isArray(referenceRows[index]) ? referenceRows[index] : [])
    })
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  const missingField = visibleFields.value.find(field => {
    if (!field.required) return false
    const value = form[field.key]
    if (field.reference) return !Number.isFinite(Number(value)) || Number(value) <= 0
    if (field.type === 'number') return !Number.isFinite(Number(value)) || Number(value) < (field.min ?? 0)
    return String(value ?? '').trim() === ''
  })
  if (missingField) {
    ElMessage.warning(t('security.common.requiredField', { name: t(missingField.label) }))
    return
  }
  saving.value = true
  try {
    const payload = buildFoundationPayload(fields.value, form)
    if (editing.value) await spec.value.api.update(editing.value.id, payload)
    else await spec.value.api.create(payload)
    dialog.value = false
    await load()
    ElMessage.success(t('security.common.saved'))
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  try {
    await ElMessageBox.confirm(t('security.common.confirmDelete', { name: row.name || row.code || row.id }), t('security.common.hint'), { type: 'warning' })
    await spec.value.api.delete(row.id)
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error.message || t('security.common.failed'))
  }
}

watch(resource, load, { immediate: true })
</script>

<style scoped>
.foundation-list { min-height: 100%; padding: 20px; background: var(--addp-bg-secondary); color: var(--addp-text-primary); }
.foundation-list.embedded { min-height: 0; padding: 0; background: transparent; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 16px; }
.page-header h2 { margin: 0; }
.page-header p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.section-description { flex: 1; margin: 4px 0 0 !important; }
.table-panel { overflow: hidden; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-primary); }
.wide { width: 100%; }
:deep(.el-table) { --el-table-bg-color: var(--addp-bg-primary); --el-table-tr-bg-color: var(--addp-bg-primary); }
</style>
