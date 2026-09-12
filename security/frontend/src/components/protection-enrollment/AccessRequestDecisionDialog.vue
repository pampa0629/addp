<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog"
    :title="title"
    width="min(600px, calc(100vw - 24px))"
    :close-on-click-modal="!saving"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="emit('update:modelValue', $event)"
    @opened="focusPrimary"
    @closed="emit('closed')"
  >
    <AccessRequestDecisionForm
      v-if="request"
      ref="formRef"
      :request="request"
      :hint="hint"
      :conflict="conflict"
      :reloading="reloading"
      :form="form"
      :rules="rules"
      :release-actor-label="releaseActorLabel"
      :format-date-time="formatDateTime"
      @reload="emit('reload')"
    />
    <template #footer>
      <el-button ref="cancelButton" :disabled="saving" @click="emit('close')">
        {{ t('security.common.cancel') }}
      </el-button>
      <el-button
        :type="decision === 'reject' ? 'danger' : 'primary'"
        :loading="saving"
        :disabled="conflict"
        @click="emit('submit')"
      >
        {{ confirmLabel }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AccessRequestDecisionForm from './AccessRequestDecisionForm.vue'

defineProps({
  modelValue: { type: Boolean, default: false },
  decision: { type: String, required: true, validator: value => ['approve', 'reject'].includes(value) },
  request: { type: Object, default: null },
  title: { type: String, required: true },
  hint: { type: String, required: true },
  confirmLabel: { type: String, required: true },
  saving: { type: Boolean, default: false },
  reloading: { type: Boolean, default: false },
  conflict: { type: Boolean, default: false },
  form: { type: Object, required: true },
  rules: { type: Object, required: true },
  releaseActorLabel: { type: Function, required: true },
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
