<template>
  <section class="manual-assessment-list">
    <h5>{{ t('security.assessment.manualConclusions') }}</h5>
    <article v-for="assessment in assessments" :key="assessment.id" class="manual-assessment-card">
      <div>
        <strong>{{ assessment.component_key }}</strong>
        <span>{{ presentation.summary(assessment) }}</span>
        <p>{{ assessment.current?.rationale }}</p>
        <p v-if="assessment.current?.conclusion === 'sensitive'" class="resource-policy-summary">
          {{ presentation.protectionSummary(assessment) }}
        </p>
      </div>
      <div class="manual-assessment-card__actions">
        <el-tag :type="assessment.current?.conclusion === 'sensitive' ? 'success' : 'info'">
          {{ presentation.conclusionLabel(assessment.current?.conclusion) }}
        </el-tag>
        <el-button link @click="emit('history', assessment)">{{ t('security.assessment.history') }}</el-button>
        <el-button v-if="canUpdate" link @click="emit('revise', assessment)">
          {{ t('security.assessment.reviseConclusion') }}
        </el-button>
        <el-button
          v-if="assessment.current?.conclusion === 'sensitive' && presentation.canConfigurePolicy(assessment)"
          link
          type="primary"
          @click="emit('configurePolicy', assessment)"
        >
          {{ presentation.policyForAssessment(assessment)?.state === 'active' ? t('security.policy.adjust') : t('security.policy.tighten') }}
        </el-button>
        <el-button
          v-if="assessment.current?.conclusion === 'sensitive' && presentation.policyForAssessment(assessment)?.state === 'active' && canRevokePolicies"
          link
          @click="emit('restorePolicy', assessment)"
        >
          {{ t('security.policy.restoreDefault') }}
        </el-button>
        <el-button
          v-if="assessment.current?.conclusion === 'sensitive' && canUpdate"
          link
          type="danger"
          @click="emit('revoke', assessment)"
        >
          {{ t('security.assessment.revokeConclusion') }}
        </el-button>
      </div>
    </article>
  </section>
</template>

<script setup>
import { useI18n } from 'vue-i18n'

defineProps({
  assessments: { type: Array, required: true },
  canUpdate: { type: Boolean, default: false },
  canRevokePolicies: { type: Boolean, default: false },
  presentation: { type: Object, required: true }
})

const emit = defineEmits(['history', 'revise', 'configurePolicy', 'restorePolicy', 'revoke'])
const { t } = useI18n()
</script>

<style scoped>
.manual-assessment-list { margin-top: 16px; padding-top: 16px; border-top: 1px solid var(--addp-border-color); }
.manual-assessment-list h5 { margin: 0 0 10px; font-size: 14px; }
.manual-assessment-card { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); }
.manual-assessment-card + .manual-assessment-card { margin-top: 8px; }
.manual-assessment-card > div:first-child { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.manual-assessment-card strong { overflow-wrap: anywhere; }
.manual-assessment-card span, .manual-assessment-card p { margin: 0; color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
.manual-assessment-card .resource-policy-summary { color: var(--addp-text-primary); font-weight: 600; }
.manual-assessment-card__actions { display: flex; flex: 0 0 auto; align-items: center; gap: 8px; }
@media (max-width: 720px) {
  .manual-assessment-card { flex-direction: column; }
}
</style>
