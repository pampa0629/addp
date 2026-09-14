<template>
  <el-dialog
    :model-value="modelValue"
    class="addp-dialog"
    :title="t('security.finding.reviewTitle')"
    width="min(640px, calc(100vw - 24px))"
    :close-on-click-modal="!saving"
    :close-on-press-escape="!saving"
    :show-close="!saving"
    @update:model-value="emit('update:modelValue', $event)"
    @opened="focusPrimary"
    @closed="emit('closed')"
  >
    <FindingReviewForm
      v-if="presentation"
      ref="formRef"
      :presentation="presentation"
      :basis-expanded="basisExpanded"
      :form="form"
      :rules="rules"
      @update:basis-expanded="emit('update:basisExpanded', $event)"
      @sensitive-type-change="emit('sensitiveTypeChange', $event)"
    />
    <template #footer>
      <el-button :disabled="saving" @click="emit('close')">
        {{ t('security.common.cancel') }}
      </el-button>
      <el-button type="primary" :loading="saving" @click="emit('submit')">
        {{ t('security.finding.submitReview') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import FindingReviewForm from './FindingReviewForm.vue'

defineProps({
  modelValue: { type: Boolean, default: false },
  presentation: { type: Object, default: null },
  basisExpanded: { type: Array, required: true },
  saving: { type: Boolean, default: false },
  form: { type: Object, required: true },
  rules: { type: Object, required: true }
})

const emit = defineEmits(['update:modelValue', 'update:basisExpanded', 'sensitiveTypeChange', 'close', 'closed', 'submit'])
const { t } = useI18n()
const formRef = ref(null)

function validate() {
  return formRef.value?.validate() ?? Promise.resolve(false)
}

function focusPrimary() {
  formRef.value?.focusRationale?.()
}

defineExpose({ validate, focusPrimary })
</script>
