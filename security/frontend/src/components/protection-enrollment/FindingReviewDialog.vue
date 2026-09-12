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
      v-if="finding"
      ref="formRef"
      :finding="finding"
      :remaining-label="remainingLabel"
      :basis-expanded="basisExpanded"
      :form="form"
      :rules="rules"
      :sensitive-types="sensitiveTypes"
      :rationale-placeholder="rationalePlaceholder"
      :active-grades-for-type="activeGradesForType"
      :type-name="typeName"
      :confidence-label="confidenceLabel"
      :capability-name="capabilityName"
      :evidence-audit-description="evidenceAuditDescription"
      :decision-presentation="decisionPresentation"
      :effective-definition-summary="effectiveDefinitionSummary"
      :baseline-description="baselineDescription"
      :owner-label="ownerLabel"
      :outlet-rule-description="outletRuleDescription"
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
  finding: { type: Object, default: null },
  remainingLabel: { type: String, required: true },
  basisExpanded: { type: Array, required: true },
  saving: { type: Boolean, default: false },
  form: { type: Object, required: true },
  rules: { type: Object, required: true },
  sensitiveTypes: { type: Array, required: true },
  rationalePlaceholder: { type: String, required: true },
  activeGradesForType: { type: Function, required: true },
  typeName: { type: Function, required: true },
  confidenceLabel: { type: Function, required: true },
  capabilityName: { type: Function, required: true },
  evidenceAuditDescription: { type: Function, required: true },
  decisionPresentation: { type: Function, required: true },
  effectiveDefinitionSummary: { type: Function, required: true },
  baselineDescription: { type: Function, required: true },
  ownerLabel: { type: Function, required: true },
  outletRuleDescription: { type: Function, required: true }
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
