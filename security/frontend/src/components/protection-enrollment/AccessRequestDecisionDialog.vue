<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog"
    :title="presentation?.title || ''"
    width="min(600px, calc(100vw - 24px))"
    :close-on-click-modal="!saving"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="emit('update:modelValue', $event)"
    @open="clearValidate"
    @opened="focusPrimary"
    @closed="emit('closed')"
  >
    <AccessRequestDecisionForm
      v-if="presentation"
      ref="formRef"
      :presentation="presentation"
      :conflict="conflict"
      :reloading="reloading"
      :form="form"
      :rules="rules"
      @reload="emit('reload')"
    />
    <template #footer>
      <el-button ref="cancelButton" :disabled="saving" @click="emit('close')">
        {{ t('security.common.cancel') }}
      </el-button>
      <el-button
        :type="presentation?.confirmType || 'primary'"
        :loading="saving"
        :disabled="conflict"
        @click="emit('submit')"
      >
        {{ presentation?.confirmLabel || '' }}
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
  presentation: { type: Object, default: null },
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
