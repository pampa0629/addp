<template>
  <section class="sensitive-type-list">
    <div class="section-header">
      <p>{{ t('security.descriptions.sensitiveDataType') }}</p>
      <el-button v-if="canCreate" type="primary" @click="openCreate">
        {{ t('security.common.createResource', { name: t('security.resources.sensitiveDataType') }) }}
      </el-button>
    </div>

    <div class="table-panel">
      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column :label="t('security.sensitiveType.type')" min-width="190">
          <template #default="{ row }">
            <div class="primary-text">{{ row.name }}</div>
            <div class="secondary-text">{{ row.code }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.fields.security_classification_id')" min-width="170">
          <template #default="{ row }">{{ referenceLabel(classifications, row.security_classification_id) }}</template>
        </el-table-column>
        <el-table-column :label="t('security.fields.default_security_grade_id')" min-width="190">
          <template #default="{ row }">{{ referenceLabel(grades, row.default_security_grade_id) }}</template>
        </el-table-column>
        <el-table-column :label="t('security.sensitiveType.recognition')" min-width="180">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetectors(row)">
              {{ t('security.sensitiveType.recognitionCount', recognitionCount(row.id)) }}
            </el-button>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.sensitiveType.defaultProtection')" min-width="170">
          <template #default="{ row }">
            <el-button link type="primary" @click="openBaselines(row)">
              {{ baselineSummary(row) }}
            </el-button>
            <div class="secondary-text">{{ t('security.sensitiveType.baselineCount', { count: baselineCount(row.id) }) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-if="can('update')" link @click="openEdit(row)">{{ t('security.common.edit') }}</el-button>
            <el-button v-if="can('delete')" link type="danger" @click="remove(row)">{{ t('security.common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && rows.length === 0" :description="t('security.sensitiveType.empty')" />
    </div>

    <el-dialog
      v-model="dialog"
      class="addp-dialog"
      :title="editing ? t('security.common.editResource', { name: t('security.resources.sensitiveDataType') }) : t('security.common.createResource', { name: t('security.resources.sensitiveDataType') })"
      width="min(620px, calc(100vw - 24px))"
    >
      <el-form ref="formRef" :model="form" :rules="formRules" label-position="top">
        <div class="form-grid">
          <el-form-item :label="t('security.fields.code')" prop="code" required>
            <el-input v-model="form.code" :disabled="Boolean(editing)" />
          </el-form-item>
          <el-form-item :label="t('security.fields.name')" prop="name" required>
            <el-input v-model="form.name" />
          </el-form-item>
        </div>
        <el-form-item :label="t('security.fields.description')">
          <el-input v-model="form.description" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item :label="t('security.fields.security_classification_id')" prop="security_classification_id" required>
          <el-select v-model="form.security_classification_id" class="wide" filterable>
            <el-option v-for="item in classifications" :key="item.id" :value="Number(item.id)" :label="definitionLabel(item)" />
          </el-select>
          <div class="field-help">{{ t('security.sensitiveType.classificationHelp') }}</div>
        </el-form-item>
        <el-form-item :label="t('security.fields.default_security_grade_id')" prop="default_security_grade_id" required>
          <el-select v-model="form.default_security_grade_id" class="wide" filterable>
            <el-option v-for="item in selectableGrades" :key="item.id" :value="Number(item.id)" :label="definitionLabel(item)" />
          </el-select>
          <div class="field-help">{{ t('security.sensitiveType.initialGradeHelp') }}</div>
        </el-form-item>
        <template v-if="!editing">
          <el-divider content-position="left">{{ t('security.sensitiveType.initialProtection') }}</el-divider>
          <el-alert :title="t('security.sensitiveType.initialProtectionHelp')" type="info" :closable="false" show-icon />
          <el-form-item class="initial-protection-field" :label="t('security.fields.effect')" prop="default_effect" required>
            <el-radio-group v-model="form.default_effect">
              <el-radio-button value="mask">{{ effectLabel('mask') }}</el-radio-button>
              <el-radio-button value="suppress">{{ effectLabel('suppress') }}</el-radio-button>
              <el-radio-button value="deny">{{ effectLabel('deny') }}</el-radio-button>
            </el-radio-group>
            <div class="field-help">{{ t(`security.baseline.effectImpact.${form.default_effect}`) }}</div>
          </el-form-item>
          <template v-if="form.default_effect === 'mask'">
            <el-form-item :label="t('security.fields.algorithm')">
              <el-input :model-value="t('security.options.algorithms.keepPrefixSuffix')" disabled />
            </el-form-item>
            <div class="form-grid">
              <el-form-item :label="t('security.fields.keep_prefix')" prop="default_keep_prefix" required>
                <el-input-number v-model="form.default_keep_prefix" :min="0" controls-position="right" />
              </el-form-item>
              <el-form-item :label="t('security.fields.keep_suffix')" prop="default_keep_suffix" required>
                <el-input-number v-model="form.default_keep_suffix" :min="0" controls-position="right" />
              </el-form-item>
            </div>
            <el-form-item :label="t('security.fields.invalid_value_effect')" prop="default_invalid_value_effect" required>
              <el-select v-model="form.default_invalid_value_effect" class="wide">
                <el-option value="suppress" :label="effectLabel('suppress')" />
                <el-option value="deny" :label="effectLabel('deny')" />
              </el-select>
            </el-form-item>
          </template>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="dialog = false">{{ t('security.common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('security.common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-drawer v-model="detectorDrawer" :title="t('security.sensitiveType.manageRecognition')" size="min(820px, 92vw)">
      <div v-if="selectedType" class="drawer-context">
        <div class="primary-text">{{ selectedType.name }}</div>
        <div class="secondary-text">{{ selectedType.code }}</div>
      </div>
      <DetectorBindings
        v-if="selectedType"
        :key="`${selectedType.id}:${detectorDrawerRevision}`"
        :sensitive-type-id="Number(selectedType.id)"
        :sensitive-type-name="selectedType.name"
        @changed="load"
      />
    </el-drawer>

    <el-drawer v-model="baselineDrawer" :title="t('security.sensitiveType.manageProtection')" size="min(900px, 94vw)">
      <ProtectionBaselineBindings
        v-if="selectedBaselineType"
        :key="`${selectedBaselineType.id}:${baselineDrawerRevision}`"
        :sensitive-type="selectedBaselineType"
        @changed="load"
      />
    </el-drawer>
  </section>
</template>

<script setup>
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { classificationAPI, detectorAPI, gradeAPI, protectionBaselineAPI, sensitiveDataTypeAPI } from '../api/security'
import { useAuthStore } from '../store/auth'
import DetectorBindings from './DetectorBindings.vue'
import ProtectionBaselineBindings from './ProtectionBaselineBindings.vue'
import { createNonNegativeIntegerRule, createRequiredRule, protectionEffectI18nKey } from '../utils/foundationForm.mjs'

const { t } = useI18n()
const auth = useAuthStore()
const rows = ref([])
const classifications = ref([])
const grades = ref([])
const detectors = ref([])
const baselines = ref([])
const loading = ref(false)
const saving = ref(false)
const dialog = ref(false)
const formRef = ref(null)
const detectorDrawer = ref(false)
const detectorDrawerRevision = ref(0)
const baselineDrawer = ref(false)
const baselineDrawerRevision = ref(0)
const editing = ref(null)
const selectedType = ref(null)
const selectedBaselineType = ref(null)
const form = reactive({
  code: '', name: '', description: '', security_classification_id: null, default_security_grade_id: null, version: 0,
  default_effect: 'mask', default_keep_prefix: 3, default_keep_suffix: 4, default_invalid_value_effect: 'suppress'
})
const orderedGrades = computed(() => [...grades.value].sort((left, right) => Number(left.risk_order) - Number(right.risk_order)))
const selectableGrades = computed(() => {
  if (!editing.value) return orderedGrades.value
  return orderedGrades.value.filter(grade => baselines.value.some(item =>
    String(item.sensitive_data_type_id) === String(editing.value.id)
    && String(item.security_grade_id) === String(grade.id)
    && item.enabled
  ))
})
const canCreate = computed(() => can('create'))
const formRules = computed(() => ({
  code: [createRequiredRule(t('security.common.requiredField', { name: t('security.fields.code') }), { trigger: 'blur', whitespace: true })],
  name: [createRequiredRule(t('security.common.requiredField', { name: t('security.fields.name') }), { trigger: 'blur', whitespace: true })],
  security_classification_id: [createRequiredRule(t('security.common.requiredField', { name: t('security.fields.security_classification_id') }))],
  default_security_grade_id: [createRequiredRule(t('security.common.requiredField', { name: t('security.fields.default_security_grade_id') }))],
  ...(!editing.value ? {
    default_effect: [createRequiredRule(t('security.common.requiredField', { name: t('security.fields.effect') }))],
    ...(form.default_effect === 'mask' ? {
      default_keep_prefix: [createNonNegativeIntegerRule(t('security.common.requiredField', { name: t('security.fields.keep_prefix') }))],
      default_keep_suffix: [createNonNegativeIntegerRule(t('security.common.requiredField', { name: t('security.fields.keep_suffix') }))],
      default_invalid_value_effect: [createRequiredRule(t('security.common.requiredField', { name: t('security.fields.invalid_value_effect') }))]
    } : {})
  } : {})
}))

function can(action) { return auth.hasPermission(`security.sensitive_data_type.${action}`) }
function effectLabel(effect) {
  const key = protectionEffectI18nKey(effect)
  return key ? t(key) : t('security.common.notAvailable')
}
function definitionLabel(item) {
  return t('security.common.referenceOption', { name: item.name, code: item.code })
}
function referenceLabel(items, id) {
  const item = items.find(candidate => String(candidate.id) === String(id))
  return item ? definitionLabel(item) : t('security.common.unknownReference', { id })
}
function recognitionCount(id) {
  const all = detectors.value.filter(item => String(item.sensitive_data_type_id) === String(id))
  return { enabled: all.filter(item => item.enabled).length, total: all.length }
}
function baselineCount(id) {
  return baselines.value.filter(item => String(item.sensitive_data_type_id) === String(id) && item.enabled).length
}
function initialBaseline(row) {
  return baselines.value.find(item => String(item.sensitive_data_type_id) === String(row.id)
    && String(item.security_grade_id) === String(row.default_security_grade_id)
    && item.enabled)
}
function baselineSummary(row) {
  const baseline = initialBaseline(row)
  return baseline ? effectLabel(baseline.effect) : t('security.sensitiveType.missingInitialProtection')
}

async function load() {
  loading.value = true
  try {
    const result = await Promise.all([
      sensitiveDataTypeAPI.list(), classificationAPI.list(), gradeAPI.list(), detectorAPI.list(), protectionBaselineAPI.list()
    ])
    rows.value = Array.isArray(result[0]) ? result[0] : []
    classifications.value = Array.isArray(result[1]) ? result[1] : []
    grades.value = Array.isArray(result[2]) ? result[2] : []
    detectors.value = Array.isArray(result[3]) ? result[3] : []
    baselines.value = Array.isArray(result[4]) ? result[4] : []
    if (selectedType.value) selectedType.value = rows.value.find(item => String(item.id) === String(selectedType.value.id)) || null
    if (selectedBaselineType.value) selectedBaselineType.value = rows.value.find(item => String(item.id) === String(selectedBaselineType.value.id)) || null
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    loading.value = false
  }
}

function reset(row = {}) {
  form.code = row.code || ''
  form.name = row.name || ''
  form.description = row.description || ''
  form.security_classification_id = row.security_classification_id ? Number(row.security_classification_id) : (classifications.value[0] ? Number(classifications.value[0].id) : null)
  form.default_security_grade_id = row.default_security_grade_id ? Number(row.default_security_grade_id) : (orderedGrades.value[0] ? Number(orderedGrades.value[0].id) : null)
  form.version = Number(row.version || 0)
  form.default_effect = 'mask'
  form.default_keep_prefix = 3
  form.default_keep_suffix = 4
  form.default_invalid_value_effect = 'suppress'
}
function openCreate() {
  editing.value = null
  reset()
  dialog.value = true
  nextTick(() => formRef.value?.clearValidate())
}
function openEdit(row) {
  editing.value = row
  reset(row)
  dialog.value = true
  nextTick(() => formRef.value?.clearValidate())
}
function openDetectors(row) {
  selectedType.value = row
  detectorDrawerRevision.value += 1
  detectorDrawer.value = true
}
function openBaselines(row) {
  selectedBaselineType.value = row
  baselineDrawerRevision.value += 1
  baselineDrawer.value = true
}

watch(() => form.default_effect, effect => {
  if (effect === 'deny') form.default_invalid_value_effect = 'deny'
  if (effect === 'suppress') form.default_invalid_value_effect = 'suppress'
})

async function save() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (valid === false) return
  saving.value = true
  try {
    const payload = {
      code: form.code.trim(), name: form.name.trim(), description: form.description.trim(),
      security_classification_id: Number(form.security_classification_id),
      default_security_grade_id: Number(form.default_security_grade_id)
    }
    if (editing.value) {
      payload.version = form.version
      await sensitiveDataTypeAPI.update(editing.value.id, payload)
    } else {
      const mask = form.default_effect === 'mask'
      payload.default_protection = {
        effect: form.default_effect,
        algorithm: mask ? 'addp.mask.keep_prefix_suffix/v2' : '',
        keep_prefix: mask ? Number(form.default_keep_prefix) : 0,
        keep_suffix: mask ? Number(form.default_keep_suffix) : 0,
        invalid_value_effect: mask ? form.default_invalid_value_effect : form.default_effect
      }
      await sensitiveDataTypeAPI.create(payload)
    }
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
    await ElMessageBox.confirm(t('security.common.confirmDelete', { name: row.name }), t('security.common.hint'), { type: 'warning' })
    await sensitiveDataTypeAPI.delete(row.id)
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error.message || t('security.common.failed'))
  }
}

onMounted(load)
</script>

<style scoped>
.sensitive-type-list { min-height: 0; }
.section-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 16px; }
.section-header p { margin: 4px 0 0; color: var(--addp-text-secondary); }
.table-panel { overflow: hidden; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-primary); }
.primary-text { color: var(--addp-text-primary); font-weight: 600; }
.secondary-text { margin-top: 4px; color: var(--addp-text-tertiary); font-size: 12px; }
.wide { width: 100%; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.field-help { margin-top: 6px; color: var(--addp-text-tertiary); font-size: 12px; line-height: 1.5; }
.initial-protection-field { margin-top: 16px; }
.drawer-context { margin: -4px 0 16px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-secondary); }
:deep(.el-table) { --el-table-bg-color: var(--addp-bg-primary); --el-table-tr-bg-color: var(--addp-bg-primary); }
@media (max-width: 720px) { .form-grid { grid-template-columns: 1fr; gap: 0; } }
</style>
