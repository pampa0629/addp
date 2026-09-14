<template>
  <section class="finding-section">
    <div class="finding-section__header">
      <div>
        <h4>{{ t('security.finding.governanceTitle') }}</h4>
        <p>{{ t('security.finding.governanceHint') }}</p>
      </div>
      <div class="finding-section__actions">
        <el-tag v-if="presentation.section.pendingReview" type="warning">
          {{ presentation.section.pendingReview.label }}
        </el-tag>
        <el-button
          v-if="presentation.section.canDesignate"
          type="primary"
          plain
          @click="emit('designate')"
        >
          {{ t('security.assessment.designateField') }}
        </el-button>
      </div>
    </div>

    <el-skeleton v-if="presentation.loading" :rows="3" animated />
    <template v-else>
      <EnrollmentFindingList
        v-if="presentation.findings.visible"
        :presentation="presentation.findings"
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
        v-if="presentation.assessments.visible"
        :presentation="presentation.assessments"
        @history="emit('history', $event)"
        @revise="emit('revise', $event)"
        @configure-policy="emit('configurePolicy', $event)"
        @restore-policy="emit('restorePolicy', $event)"
        @revoke="emit('revokeAssessment', $event)"
      />

      <el-empty
        v-if="presentation.empty"
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
