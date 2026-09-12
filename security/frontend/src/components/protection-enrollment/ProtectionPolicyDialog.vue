<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog"
    :title="t(titleKey)"
    :width="mode === 'tighten' ? 'min(600px, calc(100vw - 24px))' : 'min(560px, calc(100vw - 24px))'"
    :close-on-click-modal="!saving"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="emit('update:modelValue', $event)"
    @open="clearValidate"
    @opened="focusPrimary"
    @closed="emit('closed')"
  >
    <ProtectionPolicyForm
      v-if="assessment"
      ref="formRef"
      :mode="mode"
      :assessment="assessment"
      :assessment-summary="assessmentSummary"
      :protection-summary="protectionSummary"
      :conflict="conflict"
      :reloading="reloading"
      :form="form"
      :rules="rules"
      :effects="effects"
      :effect-label="effectLabel"
      @reload="emit('reload')"
    />
    <template #footer>
      <el-button ref="cancelButton" :disabled="saving" @click="emit('close')">
        {{ t('security.common.cancel') }}
      </el-button>
      <el-button :type="mode === 'tighten' ? 'primary' : 'danger'" :loading="saving" :disabled="conflict" @click="emit('submit')">
        {{ t(confirmKey) }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ProtectionPolicyForm from './ProtectionPolicyForm.vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  mode: { type: String, required: true, validator: value => ['tighten', 'restore'].includes(value) },
  assessment: { type: Object, default: null },
  assessmentSummary: { type: String, default: '' },
  protectionSummary: { type: String, default: '' },
  saving: { type: Boolean, default: false },
  reloading: { type: Boolean, default: false },
  conflict: { type: Boolean, default: false },
  form: { type: Object, required: true },
  rules: { type: Object, required: true },
  effects: { type: Array, default: () => [] },
  effectLabel: { type: Function, required: true }
})

const emit = defineEmits(['update:modelValue', 'reload', 'close', 'closed', 'submit'])
const { t } = useI18n()
const formRef = ref(null)
const cancelButton = ref(null)
const titleKey = computed(() => props.mode === 'tighten' ? 'security.policy.title' : 'security.policy.restoreDefault')
const confirmKey = computed(() => props.mode === 'tighten' ? 'security.policy.confirm' : 'security.policy.confirmRestore')

function validate() {
  return formRef.value?.validate() ?? Promise.resolve(false)
}

function clearValidate() {
  formRef.value?.clearValidate()
}

function focusPrimary() {
  if (props.mode !== 'restore') return
  clearValidate()
  const button = cancelButton.value?.$el || cancelButton.value
  button?.focus?.()
}

defineExpose({ validate, clearValidate, focusPrimary })
</script>
