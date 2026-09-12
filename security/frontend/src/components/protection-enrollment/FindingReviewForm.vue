<template>
  <div class="review-target">
    <div class="review-target__header">
      <strong>{{ finding.component_key }}</strong>
      <el-tag size="small" type="warning" effect="plain">{{ remainingLabel }}</el-tag>
    </div>
    <span>{{ typeName(finding.sensitive_data_type_id) }} · {{ confidenceLabel(finding.confidence) }}</span>
  </div>
  <el-collapse :model-value="basisExpanded" class="review-basis" @update:model-value="emit('update:basisExpanded', $event)">
    <el-collapse-item name="basis">
      <template #title>
        <div class="review-basis__title">
          <QuestionFilled />
          <span>{{ t('security.finding.reviewBasis.title') }}</span>
          <small>{{ t('security.finding.reviewBasis.hint') }}</small>
        </div>
      </template>
      <dl class="review-basis__facts">
        <div>
          <dt>{{ t('security.finding.reviewBasis.recognitionMethod') }}</dt>
          <dd>{{ capabilityName(finding) }}</dd>
        </div>
        <div>
          <dt>{{ t('security.finding.reviewBasis.actualMatch') }}</dt>
          <dd>{{ evidenceAuditDescription(finding) }}</dd>
        </div>
        <div>
          <dt>{{ t('security.finding.reviewBasis.governanceDecision') }}</dt>
          <dd>
            <el-tag size="small" :type="decisionPresentation(finding).type">
              {{ decisionPresentation(finding).label }}
            </el-tag>
            <span>{{ effectiveDefinitionSummary(finding) }}</span>
            <span>{{ baselineDescription(finding) }}</span>
          </dd>
        </div>
        <div>
          <dt>{{ t('security.finding.reviewBasis.currentEnforcement') }}</dt>
          <dd v-if="finding.explanation?.outlets?.length" class="review-basis__outlets">
            <span v-for="outlet in finding.explanation.outlets" :key="outlet.consumer_owner">
              <strong>{{ ownerLabel(outlet.consumer_owner) }}</strong>
              {{ outletRuleDescription(finding, outlet.consumer_owner) }}
            </span>
          </dd>
          <dd v-else>{{ t('security.finding.outletUnavailable') }}</dd>
        </div>
      </dl>
    </el-collapse-item>
  </el-collapse>
  <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
    <el-form-item :label="t('security.finding.decision')" prop="decision" required>
      <el-radio-group v-model="form.decision" class="decision-group">
        <el-radio-button value="confirm">{{ t('security.finding.decisions.confirm') }}</el-radio-button>
        <el-radio-button value="adjust">{{ t('security.finding.decisions.adjust') }}</el-radio-button>
        <el-radio-button value="reject">{{ t('security.finding.decisions.reject') }}</el-radio-button>
      </el-radio-group>
    </el-form-item>
    <template v-if="form.decision === 'adjust'">
      <el-form-item :label="t('security.finding.sensitiveDataType')" prop="sensitiveDataTypeID" required>
        <el-select v-model="form.sensitiveDataTypeID" class="wide" :placeholder="t('security.finding.selectSensitiveDataType')" @change="emit('sensitiveTypeChange', $event)">
          <el-option v-for="item in sensitiveTypes" :key="item.id" :label="item.name" :value="String(item.id)" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('security.finding.securityGrade')" prop="securityGradeID" required>
        <el-select v-model="form.securityGradeID" class="wide" :placeholder="t('security.finding.selectSecurityGrade')">
          <el-option v-for="item in activeGradesForType(form.sensitiveDataTypeID)" :key="item.id" :label="item.name" :value="String(item.id)" />
        </el-select>
      </el-form-item>
    </template>
    <el-form-item :label="t('security.finding.rationale')" prop="rationale" required>
      <el-input ref="rationaleInput" v-model="form.rationale" type="textarea" :rows="4" maxlength="2000" show-word-limit :placeholder="rationalePlaceholder" />
    </el-form-item>
  </el-form>
</template>

<script setup>
import { nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { QuestionFilled } from '@element-plus/icons-vue'

defineProps({
  finding: { type: Object, required: true },
  remainingLabel: { type: String, required: true },
  basisExpanded: { type: Array, required: true },
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

const emit = defineEmits(['update:basisExpanded', 'sensitiveTypeChange'])
const { t } = useI18n()
const formRef = ref(null)
const rationaleInput = ref(null)

function validate() {
  return formRef.value?.validate() ?? Promise.resolve(false)
}

function focusRationale() {
  formRef.value?.clearValidate()
  rationaleInput.value?.focus?.()
}

onMounted(() => nextTick(focusRationale))

defineExpose({ validate, focusRationale })
</script>

<style scoped>
.review-target { display: flex; flex-direction: column; gap: 5px; margin-bottom: 18px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.review-target__header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: 12px; }
.review-target strong { overflow-wrap: anywhere; }
.review-target__header strong { min-width: 0; }
.review-target__header .el-tag { flex: 0 0 auto; }
.review-target span { color: var(--addp-text-secondary); font-size: 13px; }
.review-basis { margin: -6px 0 18px; border: 1px solid var(--addp-border-color); border-radius: 8px; }
.review-basis :deep(.el-collapse-item__header) { min-height: 44px; padding: 0 12px; border-bottom: 0; border-radius: 8px; background: var(--addp-bg-secondary); }
.review-basis :deep(.el-collapse-item__wrap) { border-bottom: 0; border-radius: 0 0 8px 8px; background: var(--addp-bg-primary); }
.review-basis :deep(.el-collapse-item__content) { padding: 0; }
.review-basis__title { display: flex; min-width: 0; align-items: center; gap: 7px; }
.review-basis__title svg { width: 16px; height: 16px; flex: 0 0 auto; color: var(--el-color-primary); }
.review-basis__title span { flex: 0 0 auto; color: var(--addp-text-primary); font-weight: 600; }
.review-basis__title small { overflow: hidden; color: var(--addp-text-tertiary); font-weight: 400; text-overflow: ellipsis; white-space: nowrap; }
.review-basis__facts { display: flex; flex-direction: column; gap: 0; margin: 0; padding: 4px 14px 12px; }
.review-basis__facts > div { display: grid; grid-template-columns: 96px minmax(0, 1fr); gap: 12px; padding: 9px 0; border-top: 1px solid var(--addp-border-color-light); font-size: 12px; line-height: 1.6; }
.review-basis__facts dt { color: var(--addp-text-tertiary); }
.review-basis__facts dd { display: flex; min-width: 0; flex-wrap: wrap; gap: 6px 10px; margin: 0; color: var(--addp-text-secondary); overflow-wrap: anywhere; }
.review-basis__outlets { flex-direction: column; }
.review-basis__outlets span { display: grid; grid-template-columns: minmax(82px, auto) minmax(0, 1fr); gap: 8px; }
.review-basis__outlets strong { color: var(--addp-text-primary); font-weight: 500; }
.decision-group { display: flex; width: 100%; }
.decision-group :deep(.el-radio-button) { flex: 1; }
.decision-group :deep(.el-radio-button__inner) { width: 100%; }
.wide { width: 100%; }
@media (max-width: 720px) {
  .review-basis__title small { display: none; }
  .review-basis__facts > div { grid-template-columns: 1fr; gap: 4px; }
}
</style>
