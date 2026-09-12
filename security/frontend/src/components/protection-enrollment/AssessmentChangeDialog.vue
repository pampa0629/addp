<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog"
    :title="t(titleKey)"
    :width="mode === 'revise' ? 'min(640px, calc(100vw - 24px))' : 'min(560px, calc(100vw - 24px))'"
    :close-on-click-modal="!saving"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="emit('update:modelValue', $event)"
    @opened="focusPrimary"
    @closed="emit('closed')"
  >
    <template v-if="assessment">
      <el-alert :type="mode === 'revise' ? 'info' : 'warning'" :closable="false" :title="t(hintKey)" />
      <div v-if="conflict" class="version-conflict-notice" role="alert">
        <span>{{ t(conflictKey) }}</span>
        <el-button link type="primary" :loading="reloading" @click="emit('reload')">
          {{ t(reloadKey) }}
        </el-button>
      </div>
      <div class="policy-target">
        <strong>{{ assessment.component_key }}</strong>
        <span>{{ assessmentConclusionLabel(assessment.current?.conclusion) }}</span>
        <span>{{ assessmentSummary(assessment) }}</span>
      </div>
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <template v-if="mode === 'revise'">
          <el-form-item :label="t('security.finding.sensitiveDataType')" prop="sensitiveDataTypeID" required>
            <el-select
              v-model="form.sensitiveDataTypeID"
              class="wide"
              :placeholder="t('security.finding.selectSensitiveDataType')"
              @change="emit('sensitiveTypeChange', $event)"
            >
              <el-option v-for="item in sensitiveTypes" :key="item.id" :label="item.name" :value="String(item.id)" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('security.finding.securityGrade')" prop="securityGradeID" required>
            <el-select v-model="form.securityGradeID" class="wide" :placeholder="t('security.finding.selectSecurityGrade')">
              <el-option v-for="item in activeGradesForType(form.sensitiveDataTypeID)" :key="item.id" :label="item.name" :value="String(item.id)" />
            </el-select>
          </el-form-item>
        </template>
        <el-form-item :label="t(rationaleLabelKey)" prop="rationale" required>
          <el-input
            ref="rationaleInput"
            v-model="form.rationale"
            type="textarea"
            :rows="4"
            maxlength="2000"
            show-word-limit
            :placeholder="t(rationalePlaceholderKey)"
          />
        </el-form-item>
      </el-form>
    </template>
    <template #footer>
      <el-button ref="cancelButton" :disabled="saving" @click="emit('close')">
        {{ t('security.common.cancel') }}
      </el-button>
      <el-button :type="mode === 'revise' ? 'primary' : 'danger'" :loading="saving" :disabled="conflict" @click="emit('submit')">
        {{ t(confirmKey) }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  mode: { type: String, required: true, validator: value => ['revise', 'revoke'].includes(value) },
  assessment: { type: Object, default: null },
  saving: { type: Boolean, default: false },
  reloading: { type: Boolean, default: false },
  conflict: { type: Boolean, default: false },
  form: { type: Object, required: true },
  rules: { type: Object, required: true },
  sensitiveTypes: { type: Array, default: () => [] },
  activeGradesForType: { type: Function, required: true },
  assessmentSummary: { type: Function, required: true },
  assessmentConclusionLabel: { type: Function, required: true }
})

const emit = defineEmits(['update:modelValue', 'reload', 'sensitiveTypeChange', 'close', 'closed', 'submit'])
const { t } = useI18n()
const formRef = ref(null)
const rationaleInput = ref(null)
const cancelButton = ref(null)

const titleKey = computed(() => props.mode === 'revise' ? 'security.assessment.reviseTitle' : 'security.assessment.revokeTitle')
const hintKey = computed(() => props.mode === 'revise' ? 'security.assessment.reviseHint' : 'security.assessment.revokeHint')
const conflictKey = computed(() => props.mode === 'revise' ? 'security.assessment.versionConflict' : 'security.assessment.revokeVersionConflict')
const reloadKey = computed(() => props.mode === 'revise' ? 'security.assessment.reloadLatest' : 'security.assessment.reloadLatestForRevoke')
const rationaleLabelKey = computed(() => props.mode === 'revise' ? 'security.assessment.revisionRationale' : 'security.assessment.revokeRationale')
const rationalePlaceholderKey = computed(() => props.mode === 'revise' ? 'security.assessment.revisionRationalePlaceholder' : 'security.assessment.revokeRationalePlaceholder')
const confirmKey = computed(() => props.mode === 'revise' ? 'security.assessment.confirmRevision' : 'security.assessment.confirmRevoke')

function validate() {
  return formRef.value?.validate() ?? Promise.resolve(false)
}

function clearValidate() {
  formRef.value?.clearValidate()
}

function focusPrimary() {
  clearValidate()
  const target = props.mode === 'revise'
    ? rationaleInput.value
    : (cancelButton.value?.$el || cancelButton.value)
  target?.focus?.()
}

defineExpose({ validate, clearValidate, focusPrimary })
</script>

<style scoped>
.policy-target { display: flex; flex-direction: column; gap: 4px; margin: 16px 0; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.policy-target span { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
.version-conflict-notice { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; padding: 10px 12px; color: var(--el-color-warning); border: 1px solid var(--el-color-warning); border-radius: 8px; background: var(--addp-bg-secondary); font-size: 13px; line-height: 1.5; }
.wide { width: 100%; }
@media (max-width: 720px) {
  .version-conflict-notice { align-items: flex-start; flex-direction: column; }
}
</style>
