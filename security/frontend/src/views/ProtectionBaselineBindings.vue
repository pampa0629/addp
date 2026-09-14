<template>
  <section ref="workspaceRef" class="baseline-bindings" tabindex="-1">
    <div class="section-header">
      <div>
        <p class="section-description">{{ t('security.baseline.mappingHint') }}</p>
        <p class="section-context">{{ sensitiveType.name }} · {{ sensitiveType.code }}</p>
      </div>
      <el-button v-if="can('create') && unusedGrades.length" type="primary" :disabled="listBlocked" @click="openCreate">
        {{ t('security.baseline.create') }}
      </el-button>
    </div>

    <el-alert :title="t('security.baseline.initialRuleHint')" type="info" :closable="false" show-icon />
    <el-alert v-if="loadError" class="baseline-notice" :title="t(loadError)" type="warning" :closable="false" show-icon>
      <el-button ref="refreshButton" :loading="loading" :disabled="busy" @click="load()">
        {{ t('security.baselineRecovery.refresh') }}
      </el-button>
    </el-alert>

    <div v-if="!loadError || rows.length" class="table-panel">
      <el-table v-loading="loading" :data="rows" row-key="id">
        <el-table-column :label="t('security.fields.security_grade_id')" min-width="170">
          <template #default="{ row }">
            {{ referenceLabel(grades, row.security_grade_id) }}
            <el-tag v-if="isInitialBaseline(row)" class="initial-tag" size="small" type="primary">
              {{ t('security.baseline.initialRule') }}
            </el-tag>
          </template>
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
        <el-table-column :label="t('security.baseline.ruleEffective')" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? t('security.baseline.effective') : t('security.baseline.inactive') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('security.common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-if="can('update')" link :disabled="listBlocked" @click="openEdit(row, $event)">{{ t('security.common.edit') }}</el-button>
            <el-button v-if="can('delete') && !isInitialBaseline(row)" link type="danger" :disabled="listBlocked" @click="remove(row)">
              {{ t('security.common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && !loadError && rows.length === 0" :description="t('security.baseline.empty')" />
    </div>

    <el-dialog
      v-model="dialog"
      append-to-body
      class="addp-dialog"
      :title="editing ? t('security.baseline.edit') : t('security.baseline.create')"
      width="min(620px, calc(100vw - 24px))"
      :close-on-click-modal="!busy"
      :close-on-press-escape="!busy"
      :show-close="!busy"
      @closed="restoreTriggerFocus"
    >
      <el-alert :title="t('security.baseline.formHint')" type="info" :closable="false" show-icon />
      <el-alert v-if="conflict" class="baseline-notice" :title="t('security.baselineRecovery.versionConflict')" type="warning" :closable="false" show-icon>
        <el-button :loading="reloading" :disabled="saving" @click="reloadLatest">
          {{ t('security.baselineRecovery.reloadLatest') }}
        </el-button>
      </el-alert>
      <el-form ref="formRef" :model="form" :rules="formRules" :disabled="busy" label-position="top" class="baseline-form">
        <el-form-item :label="t('security.fields.security_grade_id')" prop="security_grade_id" required>
          <el-select v-model="form.security_grade_id" class="wide" filterable :disabled="Boolean(editing)">
            <el-option v-for="item in availableGrades" :key="item.id" :value="Number(item.id)" :label="definitionLabel(item)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('security.fields.effect')" prop="effect" required>
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
            <el-form-item :label="t('security.fields.keep_prefix')" prop="keep_prefix" required>
              <el-input-number v-model="form.keep_prefix" :min="0" controls-position="right" />
            </el-form-item>
            <el-form-item :label="t('security.fields.keep_suffix')" prop="keep_suffix" required>
              <el-input-number v-model="form.keep_suffix" :min="0" controls-position="right" />
            </el-form-item>
          </div>
          <el-form-item :label="t('security.fields.invalid_value_effect')" prop="invalid_value_effect" required>
            <el-select v-model="form.invalid_value_effect" class="wide">
              <el-option value="suppress" :label="effectLabel('suppress')" />
              <el-option value="deny" :label="effectLabel('deny')" />
            </el-select>
            <div class="field-help">{{ t('security.baseline.invalidValueHelp') }}</div>
          </el-form-item>
        </template>
        <el-form-item v-if="!isEditingInitial" :label="t('security.baseline.ruleEffective')">
          <el-switch v-model="form.enabled" :active-text="t('security.baseline.effective')" :inactive-text="t('security.baseline.inactive')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button :disabled="busy" @click="dialog = false">{{ t('security.common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="conflict || reloading" @click="save">{{ t('security.common.save') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { gradeAPI, protectionBaselineAPI } from '../api/security'
import { useAuthStore } from '../store/auth'
import { confirmDangerousAction } from '../utils/confirmation.mjs'
import { createNonNegativeIntegerRule, createRequiredRule, protectionEffectI18nKey } from '../utils/foundationForm.mjs'
import { isResourceVersionConflict } from '../utils/protectionEnrollment.mjs'

const props = defineProps({ sensitiveType: { type: Object, required: true } })
const emit = defineEmits(['changed'])
const { t } = useI18n()
const auth = useAuthStore()
const rows = ref([])
const grades = ref([])
const loading = ref(false)
const saving = ref(false)
const reloading = ref(false)
const conflict = ref(false)
const loadError = ref('')
const busy = computed(() => saving.value || reloading.value)
const listBlocked = computed(() => busy.value || loading.value || Boolean(loadError.value))
const dialog = ref(false)
const workspaceRef = ref(null)
const refreshButton = ref(null)
const formRef = ref(null)
const editing = ref(null)
let dialogTrigger = null
let disposed = false
const form = reactive({ security_grade_id: null, effect: 'mask', keep_prefix: 3, keep_suffix: 4, invalid_value_effect: 'suppress', enabled: true, version: 0 })
const orderedGrades = computed(() => [...grades.value].sort((left, right) => Number(left.risk_order) - Number(right.risk_order)))
const unusedGrades = computed(() => orderedGrades.value.filter(item => !rows.value.some(row => String(row.security_grade_id) === String(item.id))))
const availableGrades = computed(() => editing.value
  ? orderedGrades.value.filter(item => String(item.id) === String(editing.value.security_grade_id))
  : unusedGrades.value)
const isEditingInitial = computed(() => Boolean(editing.value && isInitialBaseline(editing.value)))
const formRules = computed(() => ({
  security_grade_id: [createRequiredRule(t('security.common.requiredField', { name: t('security.fields.security_grade_id') }))],
  effect: [createRequiredRule(t('security.common.requiredField', { name: t('security.fields.effect') }))],
  ...(form.effect === 'mask' ? {
    keep_prefix: [createNonNegativeIntegerRule(t('security.common.requiredField', { name: t('security.fields.keep_prefix') }))],
    keep_suffix: [createNonNegativeIntegerRule(t('security.common.requiredField', { name: t('security.fields.keep_suffix') }))],
    invalid_value_effect: [createRequiredRule(t('security.common.requiredField', { name: t('security.fields.invalid_value_effect') }))]
  } : {})
}))

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
function isInitialBaseline(row) {
  return String(row.security_grade_id) === String(props.sensitiveType.default_security_grade_id)
}

async function load({ afterWrite = false } = {}) {
  if (disposed || loading.value) return false
  const restoreWorkspaceFocus = Boolean(loadError.value) && !afterWrite
  loading.value = true
  try {
    const [allBaselines, allGrades] = await Promise.all([protectionBaselineAPI.list(), gradeAPI.list()])
    if (disposed) return false
    rows.value = (Array.isArray(allBaselines) ? allBaselines : []).filter(item => String(item.sensitive_data_type_id) === String(props.sensitiveType.id))
    grades.value = Array.isArray(allGrades) ? allGrades : []
    loadError.value = ''
    publishBaselines()
    if (restoreWorkspaceFocus) nextTick(() => workspaceRef.value?.focus({ preventScroll: true }))
    return true
  } catch {
    if (!disposed) loadError.value = afterWrite
      ? 'security.baselineRecovery.savedRefreshFailed'
      : loadError.value || 'security.baselineRecovery.loadFailed'
    return false
  } finally {
    loading.value = false
  }
}

function reset(row = {}) {
  form.security_grade_id = row.security_grade_id ? Number(row.security_grade_id) : (availableGrades.value[0] ? Number(availableGrades.value[0].id) : null)
  form.effect = row.effect || 'mask'
  form.keep_prefix = Number(row.keep_prefix ?? 3)
  form.keep_suffix = Number(row.keep_suffix ?? 4)
  form.invalid_value_effect = row.invalid_value_effect || 'suppress'
  form.enabled = row.enabled === undefined ? true : Boolean(row.enabled)
  form.version = Number(row.version || 0)
}
function openCreate(event) {
  if (disposed || listBlocked.value) return
  conflict.value = false
  dialogTrigger = event.currentTarget
  editing.value = null
  reset()
  dialog.value = true
  nextTick(() => formRef.value?.clearValidate())
}
function openEdit(row, event) {
  if (disposed || listBlocked.value) return
  conflict.value = false
  dialogTrigger = event.currentTarget
  editing.value = row
  reset(row)
  dialog.value = true
  nextTick(() => formRef.value?.clearValidate())
}

function restoreTriggerFocus() {
  // Pointer dismissal does not guarantee Element Plus restores the trigger.
  const target = dialogTrigger?.isConnected && !dialogTrigger.disabled
    ? dialogTrigger : refreshButton.value?.$el || workspaceRef.value
  target?.focus({ preventScroll: true })
  dialogTrigger = null
}

function applyBaseline(row) {
  const index = rows.value.findIndex(item => String(item.id) === String(row.id))
  if (index < 0) rows.value.push(row)
  else rows.value[index] = row
}

function publishBaselines() {
  emit('changed', { sensitiveTypeID: props.sensitiveType.id, baselines: rows.value })
}

function showOperationError(error, fallbackKey = 'security.common.failed') {
  const reason = error?.response?.data?.error
  ElMessage.error(typeof reason === 'string' && reason.trim() ? reason : t(fallbackKey))
}

async function reloadLatest() {
  if (disposed || busy.value || !conflict.value || !editing.value) return
  reloading.value = true
  try {
    const latest = await protectionBaselineAPI.get(editing.value.id)
    if (disposed) return
    applyBaseline(latest)
    publishBaselines()
    editing.value = latest
    reset(latest)
    conflict.value = false
    nextTick(() => {
      formRef.value?.clearValidate()
      formRef.value?.$el.querySelector('input:not(:disabled)')?.focus()
    })
  } catch (error) {
    if (!disposed) showOperationError(error, 'security.baselineRecovery.reloadFailed')
  } finally {
    reloading.value = false
  }
}

watch(() => form.effect, effect => {
  if (effect === 'deny') form.invalid_value_effect = 'deny'
  if (effect === 'suppress') form.invalid_value_effect = 'suppress'
})

async function save() {
  if (disposed || busy.value || conflict.value || loading.value || loadError.value) return
  saving.value = true
  try {
    const valid = await formRef.value?.validate().catch(() => false)
    if (disposed || !valid) return
    const mask = form.effect === 'mask'
    const payload = {
      sensitive_data_type_id: Number(props.sensitiveType.id),
      security_grade_id: Number(form.security_grade_id),
      effect: form.effect,
      algorithm: mask ? 'addp.mask.keep_prefix_suffix/v2' : '',
      keep_prefix: mask ? Number(form.keep_prefix) : 0,
      keep_suffix: mask ? Number(form.keep_suffix) : 0,
      invalid_value_effect: mask ? form.invalid_value_effect : form.effect,
      enabled: isEditingInitial.value ? true : Boolean(form.enabled)
    }
    let saved
    if (editing.value) {
      payload.version = form.version
      saved = await protectionBaselineAPI.update(editing.value.id, payload)
    } else {
      saved = await protectionBaselineAPI.create(payload)
    }
    if (disposed) return
    applyBaseline(saved)
    form.version = Number(saved.version)
    publishBaselines()
    const refreshed = await load({ afterWrite: true })
    if (disposed) return
    dialog.value = false
    if (refreshed) ElMessage.success(t('security.common.saved'))
  } catch (error) {
    if (disposed) return
    if (editing.value && isResourceVersionConflict(error)) conflict.value = true
    else showOperationError(error)
  } finally {
    saving.value = false
  }
}

async function remove(row) {
  if (disposed || listBlocked.value) return
  try {
    await confirmDangerousAction(ElMessageBox.confirm, {
      message: t('security.baseline.confirmDelete', { type: props.sensitiveType.name, grade: referenceLabel(grades.value, row.security_grade_id) }),
      title: t('security.common.hint'),
      confirmButtonText: t('security.common.delete'),
      cancelButtonText: t('security.common.cancel')
    })
  } catch (error) {
    if (!disposed && error !== 'cancel' && error !== 'close') showOperationError(error)
    return
  }
  if (disposed || listBlocked.value) return
  saving.value = true
  try {
    await protectionBaselineAPI.delete(row.id, { version: Number(row.version) })
    if (disposed) return
    rows.value = rows.value.filter(item => String(item.id) !== String(row.id))
    publishBaselines()
    await load({ afterWrite: true })
  } catch (error) {
    if (disposed) return
    if (isResourceVersionConflict(error)) loadError.value = 'security.baselineRecovery.listConflict'
    else showOperationError(error)
  } finally {
    saving.value = false
  }
}

onMounted(load)
onBeforeUnmount(() => { disposed = true })
</script>

<style scoped>
.baseline-bindings { min-height: 0; }
.baseline-notice { margin-top: 16px; }
.baseline-notice :deep(.el-alert__content) { min-width: 0; }
.baseline-notice :deep(.el-button) { max-width: 100%; height: auto; white-space: normal; }
.baseline-notice :deep(.el-button > span) { white-space: normal; overflow-wrap: anywhere; }
.section-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 16px; }
.section-description { margin: 0; color: var(--addp-text-secondary); line-height: 1.6; }
.section-context { margin: 6px 0 0; color: var(--addp-text-primary); font-weight: 600; }
.table-panel { margin-top: 16px; overflow: hidden; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-primary); }
.primary-text { color: var(--addp-text-primary); font-weight: 600; }
.secondary-text, .field-help { margin-top: 4px; color: var(--addp-text-tertiary); font-size: 12px; line-height: 1.5; }
.initial-tag { margin-left: 8px; }
.effect-impact { display: flex; width: 100%; gap: 8px; margin-top: 10px; padding: 10px 12px; box-sizing: border-box; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-secondary); color: var(--addp-text-secondary); font-size: 13px; line-height: 1.5; }
.effect-impact-title { flex: 0 0 auto; color: var(--addp-text-primary); font-weight: 600; }
.baseline-form { margin-top: 20px; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.wide { width: 100%; }
:deep(.el-table) { --el-table-bg-color: var(--addp-bg-primary); --el-table-tr-bg-color: var(--addp-bg-primary); }
@media (max-width: 720px) { .form-grid { grid-template-columns: 1fr; gap: 0; } }
</style>
