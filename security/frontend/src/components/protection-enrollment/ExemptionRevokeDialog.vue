<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog"
    :title="t('security.exemption.revokeTitle')"
    width="min(560px, calc(100vw - 24px))"
    :close-on-click-modal="!saving"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="emit('update:modelValue', $event)"
    @opened="focusPrimary"
    @closed="emit('closed')"
  >
    <template v-if="exemption">
      <el-alert type="warning" :closable="false" :title="t('security.exemption.revokeHint')" />
      <div v-if="conflict" class="version-conflict-notice" role="alert">
        <span>{{ t('security.exemption.revokeVersionConflict') }}</span>
        <el-button link type="primary" :loading="reloading" @click="emit('reload')">
          {{ t('security.exemption.reloadLatestForRevoke') }}
        </el-button>
      </div>
      <div class="policy-target">
        <strong>{{ assessmentComponent(exemption.assessment_id) }}</strong>
        <span>{{ t('security.exemption.subject') }}：{{ exemption.subject_id }}</span>
        <span>{{ ownerLabel(exemption.consumer_owner) }} · {{ actionLabel(exemption.action) }}</span>
        <span>{{ t('security.exemption.expiresAt') }}：{{ formatDateTime(exemption.current?.expires_at) }}</span>
      </div>
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item :label="t('security.exemption.revokeRationale')" prop="rationale" required>
          <el-input
            v-model="form.rationale"
            type="textarea"
            :rows="4"
            maxlength="2000"
            show-word-limit
            :placeholder="t('security.exemption.revokeRationalePlaceholder')"
          />
        </el-form-item>
      </el-form>
    </template>
    <template #footer>
      <el-button ref="cancelButton" :disabled="saving" @click="emit('close')">
        {{ t('security.common.cancel') }}
      </el-button>
      <el-button type="danger" :loading="saving" :disabled="conflict" @click="emit('submit')">
        {{ t('security.exemption.confirmRevoke') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

defineProps({
  modelValue: { type: Boolean, default: false },
  exemption: { type: Object, default: null },
  saving: { type: Boolean, default: false },
  reloading: { type: Boolean, default: false },
  conflict: { type: Boolean, default: false },
  form: { type: Object, required: true },
  rules: { type: Object, required: true },
  assessmentComponent: { type: Function, required: true },
  ownerLabel: { type: Function, required: true },
  actionLabel: { type: Function, required: true },
  formatDateTime: { type: Function, required: true }
})

const emit = defineEmits(['update:modelValue', 'reload', 'close', 'closed', 'submit'])
const { t } = useI18n()
const formRef = ref(null)
const cancelButton = ref(null)

function validate() {
  return formRef.value?.validate() ?? Promise.resolve(false)
}

function clearValidate() {
  formRef.value?.clearValidate()
}

function focusPrimary() {
  clearValidate()
  const button = cancelButton.value?.$el || cancelButton.value
  button?.focus?.()
}

defineExpose({ validate, clearValidate, focusPrimary })
</script>

<style scoped>
.policy-target { display: flex; flex-direction: column; gap: 4px; margin: 16px 0; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.policy-target span { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
.version-conflict-notice { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; padding: 10px 12px; color: var(--el-color-warning); border: 1px solid var(--el-color-warning); border-radius: 8px; background: var(--addp-bg-secondary); font-size: 13px; line-height: 1.5; }
@media (max-width: 720px) {
  .version-conflict-notice { align-items: flex-start; flex-direction: column; }
}
</style>
