<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog"
    :title="presentation.title"
    width="min(560px, calc(100vw - 24px))"
    :close-on-click-modal="!saving"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="emit('update:modelValue', $event)"
    @open="clearValidate"
    @opened="focusPrimary"
    @closed="emit('closed')"
  >
    <el-alert type="warning" :closable="false" :title="presentation.warning" />
    <div v-if="conflict" class="version-conflict-notice" role="alert">
      <span>{{ t('security.enrollment.releaseVersionConflict') }}</span>
      <el-button link type="primary" :loading="reloading" @click="emit('reload')">
        {{ t('security.enrollment.reloadLatestForRelease') }}
      </el-button>
    </div>
    <el-form ref="formRef" :model="form" :rules="rules" label-position="top" class="release-form">
      <el-form-item :label="t('security.enrollment.resource')">
        <span>{{ presentation.targetName }}</span>
      </el-form-item>
      <el-form-item :label="t('security.enrollment.releaseBasisLabel')">
        <el-tag type="info" effect="plain">{{ presentation.basisLabel }}</el-tag>
      </el-form-item>
      <el-form-item :label="t('security.enrollment.releaseReasonLabel')" prop="reason" required>
        <el-input
          v-model="form.reason"
          type="textarea"
          :rows="3"
          :placeholder="presentation.reasonPlaceholder"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button ref="cancelButton" :disabled="saving" @click="emit('close')">
        {{ t('security.common.cancel') }}
      </el-button>
      <el-button type="danger" :loading="saving" :disabled="conflict" @click="emit('submit')">
        {{ presentation.confirmLabel }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

defineProps({
  modelValue: { type: Boolean, default: false },
  presentation: { type: Object, required: true },
  saving: { type: Boolean, default: false },
  reloading: { type: Boolean, default: false },
  conflict: { type: Boolean, default: false },
  form: { type: Object, required: true },
  rules: { type: Object, required: true }
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
  const button = cancelButton.value?.$el || cancelButton.value
  button?.focus?.()
}

defineExpose({ validate, clearValidate, focusPrimary })
</script>

<style scoped>
.release-form { margin-top: 16px; }
.version-conflict-notice { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; padding: 10px 12px; color: var(--el-color-warning); border: 1px solid var(--el-color-warning); border-radius: 8px; background: var(--addp-bg-secondary); font-size: 13px; line-height: 1.5; }
@media (max-width: 720px) {
  .version-conflict-notice { align-items: flex-start; flex-direction: column; }
}
</style>
