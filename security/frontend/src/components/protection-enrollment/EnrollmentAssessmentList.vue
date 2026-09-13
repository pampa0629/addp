<template>
  <section class="manual-assessment-list">
    <h5>{{ t('security.assessment.manualConclusions') }}</h5>
    <article v-for="row in rows" :key="row.id" class="manual-assessment-card">
      <div>
        <strong>{{ row.componentKey }}</strong>
        <span>{{ row.summary }}</span>
        <p>{{ row.rationale }}</p>
        <p v-if="row.protectionSummary" class="resource-policy-summary">
          {{ row.protectionSummary }}
        </p>
      </div>
      <div class="manual-assessment-card__actions">
        <el-tag :type="row.conclusion.type">
          {{ row.conclusion.label }}
        </el-tag>
        <el-button link @click="emit('history', row.assessment)">{{ t('security.assessment.history') }}</el-button>
        <el-button v-if="canUpdate" link @click="emit('revise', row.assessment)">
          {{ t('security.assessment.reviseConclusion') }}
        </el-button>
        <el-button
          v-if="row.actions.canConfigurePolicy"
          link
          type="primary"
          @click="emit('configurePolicy', row.assessment)"
        >
          {{ row.actions.configurePolicyLabel }}
        </el-button>
        <el-button
          v-if="row.actions.canRestorePolicy && canRevokePolicies"
          link
          @click="emit('restorePolicy', row.assessment)"
        >
          {{ t('security.policy.restoreDefault') }}
        </el-button>
        <el-button
          v-if="row.actions.canRevokeAssessment && canUpdate"
          link
          type="danger"
          @click="emit('revoke', row.assessment)"
        >
          {{ t('security.assessment.revokeConclusion') }}
        </el-button>
      </div>
    </article>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps({
  assessments: { type: Array, required: true },
  canUpdate: { type: Boolean, default: false },
  canRevokePolicies: { type: Boolean, default: false },
  presentation: { type: Object, required: true }
})

const emit = defineEmits(['history', 'revise', 'configurePolicy', 'restorePolicy', 'revoke'])
const { t } = useI18n()
const rows = computed(() => props.assessments.map(assessment => props.presentation.row(assessment)))
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
