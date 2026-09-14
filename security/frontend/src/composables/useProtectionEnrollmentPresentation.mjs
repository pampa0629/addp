import { computed } from 'vue'
import {
  isZeroFindingDiscovery,
  normalizeDiscoverySummary
} from '../utils/protectionEnrollment.mjs'
import { createProtectionAccessRequestPresentation } from './protectionAccessRequestPresentation.mjs'
import { createProtectionEnrollmentGovernancePresentation } from './protectionEnrollmentGovernancePresentation.mjs'
import { createProtectionEnrollmentResourcePresentation } from './protectionEnrollmentResourcePresentation.mjs'

export function useProtectionEnrollmentPresentation({
  t,
  locale,
  activeWorkspace,
  reviewQueueTotal,
  detailRow,
  autoRefreshActive,
  lastRefreshedAt,
  engineNames,
  sensitiveTypes,
  securityClassifications,
  securityGrades,
  protectionBaselines,
  assessments,
  policies,
  canReadPolicies,
  canCreatePolicies,
  canUpdatePolicies
}) {
  const refreshFeedback = computed(() => {
    if (autoRefreshActive.value) return t('security.enrollment.autoRefreshing')
    if (!lastRefreshedAt.value) return ''
    const language = locale.value === 'en' ? 'en-US' : 'zh-CN'
    return t('security.enrollment.lastRefreshed', { time: lastRefreshedAt.value.toLocaleTimeString(language) })
  })
  const reviewRemainingLabel = computed(() => {
    if (activeWorkspace.value === 'review-queue') {
      return t('security.finding.reviewRemainingQueue', { count: reviewQueueTotal.value })
    }
    return t('security.finding.reviewRemainingResource', { count: normalizeDiscoverySummary(detailRow.value).pendingReviewCount })
  })

  const ownerLabel = owner => t(`security.enrollment.owners.${owner}`)
  const effectLabel = effect => t(`security.enrollment.effects.${effect}`)
  const actionLabel = action => {
    const normalized = String(action || '')
    const translated = t(`security.finding.actions.${normalized}`)
    return translated === `security.finding.actions.${normalized}` ? normalized : translated
  }

  function formatDateTime(value) {
    if (!value) return t('security.common.notAvailable')
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? t('security.common.notAvailable') : date.toLocaleString()
  }

  function releaseBasisLabel(basis) {
    const normalized = String(basis || '').trim()
    if (!['manual', 'no_supported_findings'].includes(normalized)) return t('security.common.notAvailable')
    return t(`security.enrollment.releaseBases.${normalized}`)
  }

  function releaseActorLabel(actorID) {
    const normalized = Number(actorID)
    return Number.isInteger(normalized) && normalized > 0
      ? t('security.enrollment.userId', { id: normalized })
      : t('security.common.notAvailable')
  }

  const {
    accessRequestDecisionDialogView,
    accessRequestReviewRowView,
    accessRequestReviewWorkspaceView
  } = createProtectionAccessRequestPresentation({ t, formatDateTime, releaseActorLabel })

  const {
    itemTypeLabel,
    resourceIdentityView,
    resourceRowView,
    resourceListView,
    enrollmentCreationSelectionView,
    enrollmentLifecycleActionDialogView,
    enrollmentReleaseDialogView,
    discoveryPresentation,
    resourceDetailView,
    presentationState,
    ownerPresentation,
    ownerEffectDescription
  } = createProtectionEnrollmentResourcePresentation({
    t,
    engineNames,
    formatDateTime,
    releaseBasisLabel,
    releaseActorLabel,
    ownerLabel,
    effectLabel,
    actionLabel
  })

  const {
    manualAssessments,
    governanceDetailView,
    findingReviewRowView,
    findingReviewQueueView,
    findingReviewDialogView,
    exemptionRowView,
    exemptionSectionView,
    assessmentRowView,
    assessmentChangeDialogView,
    findingRowView,
    assessmentHistoryView,
    baselineForAssessment,
    policyForAssessment,
    stricterPolicyEffects,
    canConfigurePolicy,
    protectionPolicyDialogView,
    activeGradesForType,
    manualAssessmentDialogView
  } = createProtectionEnrollmentGovernancePresentation({
    t,
    sensitiveTypes,
    securityClassifications,
    securityGrades,
    protectionBaselines,
    assessments,
    policies,
    canReadPolicies,
    canCreatePolicies,
    canUpdatePolicies,
    formatDateTime,
    ownerLabel,
    effectLabel,
    actionLabel,
    itemTypeLabel,
    resourceIdentityView
  })

  return {
    manualAssessments,
    refreshFeedback,
    reviewRemainingLabel,
    isZeroFindingDiscovery,
    resourceIdentityView,
    resourceRowView,
    resourceListView,
    enrollmentCreationSelectionView,
    accessRequestDecisionDialogView,
    accessRequestReviewRowView,
    accessRequestReviewWorkspaceView,
    enrollmentLifecycleActionDialogView,
    enrollmentReleaseDialogView,
    discoveryPresentation,
    governanceDetailView,
    resourceDetailView,
    presentationState,
    ownerPresentation,
    ownerEffectDescription,
    findingReviewRowView,
    findingReviewQueueView,
    findingReviewDialogView,
    exemptionRowView,
    exemptionSectionView,
    assessmentRowView,
    assessmentChangeDialogView,
    findingRowView,
    assessmentHistoryView,
    baselineForAssessment,
    policyForAssessment,
    stricterPolicyEffects,
    canConfigurePolicy,
    protectionPolicyDialogView,
    activeGradesForType,
    manualAssessmentDialogView
  }
}
