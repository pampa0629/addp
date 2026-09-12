<template>
  <section class="finding-section">
    <div class="finding-section__header">
      <div>
        <h4>{{ t('security.finding.governanceTitle') }}</h4>
        <p>{{ t('security.finding.governanceHint') }}</p>
      </div>
      <div class="finding-section__actions">
        <el-tag v-if="presentation.normalizeDiscoverySummary(enrollment).pendingReviewCount > 0" type="warning">
          {{ t('security.finding.pendingCount', { count: presentation.normalizeDiscoverySummary(enrollment).pendingReviewCount }) }}
        </el-tag>
        <el-button
          v-if="permissions.createAssessments && !['releasing', 'released'].includes(enrollment.state)"
          type="primary"
          plain
          @click="emit('designate')"
        >
          {{ t('security.assessment.designateField') }}
        </el-button>
      </div>
    </div>

    <el-skeleton v-if="governance.loading" :rows="3" animated />
    <template v-else>
      <EnrollmentFindingList
        v-if="permissions.readFindings && governance.findings.length > 0"
        :page="governance.findingsPage"
        :findings="governance.findings"
        :total="governance.findingsTotal"
        :page-size="governance.findingsPageSize"
        :can-review="permissions.reviewFindings"
        :can-update-assessments="permissions.updateAssessments"
        :can-revoke-policies="permissions.revokePolicies"
        :type-name="presentation.typeName"
        :finding-state-presentation="presentation.findingState"
        :capability-name="presentation.capabilityName"
        :evidence-description="presentation.evidenceDescription"
        :evidence-audit-description="presentation.evidenceAuditDescription"
        :capability-text="presentation.capabilityText"
        :capability-scope="presentation.capabilityScope"
        :confidence-label="presentation.confidenceLabel"
        :decision-presentation="presentation.decision"
        :effective-definition-summary="presentation.effectiveDefinitionSummary"
        :baseline-description="presentation.baselineDescription"
        :assessment-for-finding="presentation.assessmentForFinding"
        :active-assessment-for-finding="presentation.activeAssessmentForFinding"
        :assessment-protection-summary="presentation.assessmentProtectionSummary"
        :owner-label="presentation.ownerLabel"
        :outlet-rule-description="presentation.outletRuleDescription"
        :outlet-acknowledgement-presentation="presentation.outletAcknowledgement"
        :format-date-time="presentation.formatDateTime"
        :can-configure-policy="presentation.canConfigurePolicy"
        :policy-for-assessment="presentation.policyForAssessment"
        @update:page="emit('update:findingsPage', $event)"
        @review="forwardFindingReview"
        @history="emit('history', $event)"
        @revise="emit('revise', $event)"
        @configure-policy="emit('configurePolicy', $event)"
        @restore-policy="emit('restorePolicy', $event)"
        @revoke-assessment="emit('revokeAssessment', $event)"
        @page-change="emit('pageChange', $event)"
      />

      <EnrollmentAssessmentList
        v-if="governance.manualAssessments.length > 0"
        :assessments="governance.manualAssessments"
        :can-update="permissions.updateAssessments"
        :can-revoke-policies="permissions.revokePolicies"
        :assessment-summary="presentation.assessmentSummary"
        :assessment-protection-summary="presentation.assessmentProtectionSummary"
        :assessment-conclusion-label="presentation.assessmentConclusionLabel"
        :can-configure-policy="presentation.canConfigurePolicy"
        :policy-for-assessment="presentation.policyForAssessment"
        @history="emit('history', $event)"
        @revise="emit('revise', $event)"
        @configure-policy="emit('configurePolicy', $event)"
        @restore-policy="emit('restorePolicy', $event)"
        @revoke="emit('revokeAssessment', $event)"
      />

      <el-empty
        v-if="governance.findings.length === 0 && governance.manualAssessments.length === 0"
        :description="t('security.finding.noGovernanceConclusions')"
        :image-size="72"
      />
    </template>
  </section>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import EnrollmentAssessmentList from './EnrollmentAssessmentList.vue'
import EnrollmentFindingList from './EnrollmentFindingList.vue'

defineProps({
  enrollment: { type: Object, required: true },
  governance: { type: Object, required: true },
  permissions: { type: Object, required: true },
  presentation: { type: Object, required: true }
})

const emit = defineEmits([
  'update:findingsPage',
  'designate',
  'review',
  'history',
  'revise',
  'configurePolicy',
  'restorePolicy',
  'revokeAssessment',
  'pageChange'
])
const { t } = useI18n()

function forwardFindingReview(finding, decision) {
  emit('review', { finding, decision })
}
</script>

<style scoped>
.finding-section { margin-top: 24px; }
.finding-section__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.finding-section__header h4 { margin: 0; }
.finding-section__header p { margin: 6px 0 0; color: var(--addp-text-secondary); font-size: 13px; }
.finding-section__actions { display: flex; flex: 0 0 auto; align-items: center; gap: 8px; }
@media (max-width: 720px) {
  .finding-section__header { flex-direction: column; }
  .finding-section__actions { width: 100%; justify-content: space-between; }
}
</style>
