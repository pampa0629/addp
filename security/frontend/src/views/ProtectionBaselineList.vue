<template>
  <section class="page">
    <header class="page-header">
      <div>
        <h2>{{ t('security.resources.protectionBaseline') }}</h2>
        <p>{{ t('security.descriptions.protectionBaseline') }}</p>
      </div>
      <el-button v-if="can('create')" type="primary" @click="openCreate">
        {{ t('security.baseline.create') }}
      </el-button>
    </header>

    <el-alert :title="t('security.baseline.mappingHint')" type="info" :closable="false" show-icon />

    <div class="table-panel">
      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column :label="t('security.fields.sensitive_data_type_id')" min-width="180">
          <template #default="{ row }">{{ referenceLabel(types, row.sensitive_data_type_id) }}</template>
        </el-table-column>
        <el-table-column :label="t('security.fields.security_grade_id')" min-width="170">
          <template #default="{ row }">{{ referenceLabel(grades, row.security_grade_id) }}</template>
        </el-table-column>
        <el-table-column :label="t('security.baseline.defaultProtection')" min-width="230">
          <template #default="{ row }">
            <div class="primary-text">{{ effectLabel(row.effect) }}</div>
            <div class="secondary-text">{{ t(`security.baseline.effectImpact.${row.effect}`) }}</div>
            <div v-if="row.effect === 'mask'" class="secondary-text">
              {{ t('security.baseline.maskSummary', { prefix: row.keep_prefix, suffix: row.keep_suffix }) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.fields.invalid_value_effect')" min-width="160">
          <template #default="{ row }">
            {{ row.effect === 'mask' ? effectLabel(row.invalid_value_effect) : t('security.common.notApplicable') }}
          </template>
        </el-table-column>
        <el-table-column :label="t('security.baseline.ruleEffective')" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? t('security.baseline.effective') : t('security.baseline.inactive') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-if="can('update')" link @click="openEdit(row)">{{ t('security.common.edit') }}</el-button>
            <el-button v-if="can('delete')" link type="danger" @click="remove(row)">{{ t('security.common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && rows.length === 0" :description="t('security.baseline.empty')" />
    </div>

    <el-dialog
      v-model="dialog"
      class="addp-dialog"
      :title="editing ? t('security.baseline.edit') : t('security.baseline.create')"
      width="min(620px, calc(100vw - 24px))"
    >
      <el-alert :title="t('security.baseline.formHint')" type="info" :closable="false" show-icon />
      <el-form label-position="top" class="baseline-form">
        <div class="form-grid">
          <el-form-item :label="t('security.fields.sensitive_data_type_id')" required>
            <el-select v-model="form.sensitive_data_type_id" class="wide" filterable>
              <el-option v-for="item in types" :key="item.id" :value="Number(item.id)" :label="definitionLabel(item)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('security.fields.security_grade_id')" required>
            <el-select v-model="form.security_grade_id" class="wide" filterable>
              <el-option v-for="item in orderedGrades" :key="item.id" :value="Number(item.id)" :label="definitionLabel(item)" />
            </el-select>
          </el-form-item>
        </div>
        <el-form-item :label="t('security.fields.effect')" required>
          <el-radio-group v-model="form.effect">
            <el-radio-button value="mask">{{ effectLabel('mask') }}</el-radio-button>
            <el-radio-button value="suppress">{{ effectLabel('suppress') }}</el-radio-button>
            <el-radio-button value="deny">{{ effectLabel('deny') }}</el-radio-button>
          </el-radio-group>
          <div class="field-help">{{ t(`security.baseline.effectHelp.${form.effect}`) }}</div>
          <div class="effect-impact" role="status">
            <span class="effect-impact-title">{{ t('security.baseline.effectImpactTitle') }}</span>
            <span>{{ t(`security.baseline.effectImpact.${form.effect}`) }}</span>
          </div>
        </el-form-item>
        <template v-if="form.effect === 'mask'">
          <el-form-item :label="t('security.fields.algorithm')">
            <el-input :model-value="t('security.options.algorithms.keepPrefixSuffix')" disabled />
          </el-form-item>
          <div class="form-grid">
            <el-form-item :label="t('security.fields.keep_prefix')">
              <el-input-number v-model="form.keep_prefix" :min="0" :max="10" controls-position="right" />
            </el-form-item>
            <el-form-item :label="t('security.fields.keep_suffix')">
              <el-input-number v-model="form.keep_suffix" :min="0" :max="10" controls-position="right" />
            </el-form-item>
          </div>
          <el-form-item :label="t('security.fields.invalid_value_effect')" required>
            <el-select v-model="form.invalid_value_effect" class="wide">
              <el-option value="suppress" :label="effectLabel('suppress')" />
              <el-option value="deny" :label="effectLabel('deny')" />
            </el-select>
            <div class="field-help">{{ t('security.baseline.invalidValueHelp') }}</div>
          </el-form-item>
        </template>
        <el-form-item :label="t('security.baseline.ruleEffective')">
          <el-switch v-model="form.enabled" :active-text="t('security.baseline.effective')" :inactive-text="t('security.baseline.inactive')" />
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
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { gradeAPI, protectionBaselineAPI, sensitiveDataTypeAPI } from '../api/security'
import { useAuthStore } from '../store/auth'
import { protectionEffectI18nKey } from '../utils/foundationForm.mjs'

const { t } = useI18n()
const auth = useAuthStore()
const rows = ref([])
const types = ref([])
const grades = ref([])
const loading = ref(false)
const saving = ref(false)
const dialog = ref(false)
const editing = ref(null)
const form = reactive({ sensitive_data_type_id: null, security_grade_id: null, effect: 'mask', keep_prefix: 3, keep_suffix: 4, invalid_value_effect: 'suppress', enabled: true, version: 0 })
const orderedGrades = computed(() => [...grades.value].sort((left, right) => Number(left.risk_order) - Number(right.risk_order)))

function can(action) { return auth.hasPermission(`security.protection_baseline.${action}`) }
function definitionLabel(item) { return t('security.common.referenceOption', { name: item.name, code: item.code }) }
function referenceLabel(items, id) {
  const item = items.find(candidate => String(candidate.id) === String(id))
  return item ? definitionLabel(item) : t('security.common.unknownReference', { id })
}
function effectLabel(effect) {
  const key = protectionEffectI18nKey(effect)
  return key ? t(key) : t('security.common.notAvailable')
}

async function load() {
  loading.value = true
  try {
    const result = await Promise.all([protectionBaselineAPI.list(), sensitiveDataTypeAPI.list(), gradeAPI.list()])
    rows.value = Array.isArray(result[0]) ? result[0] : []
    types.value = Array.isArray(result[1]) ? result[1] : []
    grades.value = Array.isArray(result[2]) ? result[2] : []
  } catch (error) {
    ElMessage.error(error.message || t('security.common.failed'))
  } finally {
    loading.value = false
  }
}

function reset(row = {}) {
  form.sensitive_data_type_id = row.sensitive_data_type_id ? Number(row.sensitive_data_type_id) : (types.value[0] ? Number(types.value[0].id) : null)
  form.security_grade_id = row.security_grade_id ? Number(row.security_grade_id) : (orderedGrades.value[0] ? Number(orderedGrades.value[0].id) : null)
  form.effect = row.effect || 'mask'
  form.keep_prefix = Number(row.keep_prefix ?? 3)
  form.keep_suffix = Number(row.keep_suffix ?? 4)
  form.invalid_value_effect = row.invalid_value_effect || 'suppress'
  form.enabled = row.enabled === undefined ? true : Boolean(row.enabled)
  form.version = Number(row.version || 0)
}
function openCreate() { editing.value = null; reset(); dialog.value = true }
function openEdit(row) { editing.value = row; reset(row); dialog.value = true }

watch(() => form.effect, effect => {
  if (effect === 'deny') form.invalid_value_effect = 'deny'
  if (effect === 'suppress') form.invalid_value_effect = 'suppress'
})

async function save() {
  if (!form.sensitive_data_type_id || !form.security_grade_id || (form.effect === 'mask' && form.keep_prefix + form.keep_suffix >= 11)) {
    ElMessage.warning(form.effect === 'mask' && form.keep_prefix + form.keep_suffix >= 11 ? t('security.baseline.invalidMaskLength') : t('security.baseline.required'))
    return
  }
  saving.value = true
  try {
    const mask = form.effect === 'mask'
    const payload = {
      sensitive_data_type_id: Number(form.sensitive_data_type_id),
      security_grade_id: Number(form.security_grade_id),
      effect: form.effect,
      algorithm: mask ? 'addp.mask.keep_prefix_suffix/v1' : '',
      keep_prefix: mask ? Number(form.keep_prefix) : 0,
      keep_suffix: mask ? Number(form.keep_suffix) : 0,
      invalid_value_effect: mask ? form.invalid_value_effect : form.effect,
      enabled: Boolean(form.enabled)
    }
    if (editing.value) {
      payload.version = form.version
      await protectionBaselineAPI.update(editing.value.id, payload)
    } else {
      await protectionBaselineAPI.create(payload)
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
    await ElMessageBox.confirm(t('security.baseline.confirmDelete', { type: referenceLabel(types.value, row.sensitive_data_type_id), grade: referenceLabel(grades.value, row.security_grade_id) }), t('security.common.hint'), { type: 'warning' })
    await protectionBaselineAPI.delete(row.id, { version: Number(row.version) })
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error.message || t('security.common.failed'))
  }
}

onMounted(load)
</script>

<style scoped>
.page { min-height: 100%; padding: 20px; background: var(--addp-bg-secondary); color: var(--addp-text-primary); }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 16px; }
.page-header h2 { margin: 0; }
.page-header p { margin: 8px 0 0; color: var(--addp-text-secondary); }
.table-panel { margin-top: 16px; overflow: hidden; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-primary); }
.primary-text { color: var(--addp-text-primary); font-weight: 600; }
.secondary-text, .field-help { margin-top: 4px; color: var(--addp-text-tertiary); font-size: 12px; line-height: 1.5; }
.effect-impact { display: flex; width: 100%; gap: 8px; margin-top: 10px; padding: 10px 12px; box-sizing: border-box; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-secondary); color: var(--addp-text-secondary); font-size: 13px; line-height: 1.5; }
.effect-impact-title { flex: 0 0 auto; color: var(--addp-text-primary); font-weight: 600; }
.baseline-form { margin-top: 20px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.wide { width: 100%; }
:deep(.el-table) { --el-table-bg-color: var(--addp-bg-primary); --el-table-tr-bg-color: var(--addp-bg-primary); }
@media (max-width: 720px) { .form-grid { grid-template-columns: 1fr; gap: 0; } }
</style>
