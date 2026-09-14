import { computed } from 'vue'
import { isProtectionEffectStricter } from '../utils/protectionEnrollment.mjs'

export function createProtectionAssessmentPresentation({
  t,
  sensitiveTypes,
  securityGrades,
  protectionBaselines,
  assessments,
  policies,
  canReadPolicies,
  canCreatePolicies,
  canUpdatePolicies,
  formatDateTime,
  effectLabel,
  typeName,
  classificationName,
  gradeName,
  namedReferenceOptions
}) {
  const manualAssessments = computed(() => assessments.value.filter(item => item.current?.source_kind === 'manual'))

  function assessmentRevisionSummary(revision) {
    return t('security.assessment.summary', {
      type: typeName(revision?.sensitive_data_type_id),
      classification: classificationName(revision?.security_classification_id),
      grade: gradeName(revision?.security_grade_id)
    })
  }

  function assessmentSummary(assessment) {
    return assessmentRevisionSummary(assessment.current)
  }

  function assessmentConclusionLabel(conclusion) {
    const normalized = conclusion === 'sensitive' ? 'sensitive' : 'not_sensitive'
    return t(`security.assessment.conclusions.${normalized}`)
  }

  function assessmentChangeDialogView(assessment, context = {}) {
    return {
      componentKey: assessment?.component_key,
      conclusionLabel: assessmentConclusionLabel(assessment?.current?.conclusion),
      summary: assessmentSummary(assessment),
      sensitiveTypeOptions: namedReferenceOptions(sensitiveTypes.value),
      gradeOptions: namedReferenceOptions(context.gradeOptions)
    }
  }

  function assessmentRevisionSourceLabel(sourceKind) {
    const normalized = sourceKind === 'finding' ? 'finding' : 'manual'
    return t(`security.assessment.sources.${normalized}`)
  }

  function assessmentActorLabel(actorID) {
    const normalized = Number(actorID)
    return Number.isInteger(normalized) && normalized > 0
      ? t('security.enrollment.userId', { id: normalized })
      : t('security.common.notAvailable')
  }

  function assessmentHistoryView(assessment) {
    const currentRevision = Number(assessment?.current_revision)
    const history = Array.isArray(assessment?.history) ? assessment.history : []
    return {
      componentKey: assessment?.component_key,
      items: history.map(revision => {
        const timestamp = formatDateTime(revision?.created_at)
        const current = Number(revision?.revision) === currentRevision
        const conclusion = revision?.conclusion === 'sensitive' ? 'sensitive' : 'not_sensitive'
        return {
          id: revision?.id,
          timestamp,
          current,
          revisionLabel: t('security.assessment.revisionLabel', { revision: revision?.revision }),
          conclusion: {
            type: conclusion === 'sensitive' ? 'success' : 'info',
            label: assessmentConclusionLabel(conclusion)
          },
          sourceLabel: assessmentRevisionSourceLabel(revision?.source_kind),
          summary: assessmentRevisionSummary(revision),
          rationale: revision?.rationale || '',
          metadata: t('security.assessment.historyMeta', {
            actor: assessmentActorLabel(revision?.created_by),
            time: timestamp
          })
        }
      })
    }
  }

  function baselineForAssessment(assessment) {
    return protectionBaselines.value.find(item => item.enabled
      && String(item.sensitive_data_type_id) === String(assessment?.current?.sensitive_data_type_id)
      && String(item.security_grade_id) === String(assessment?.current?.security_grade_id)) || null
  }

  function policyForAssessment(assessment) {
    return policies.value.find(item => item.assessment_id === assessment?.id
      && item.consumer_owner === 'manager'
      && item.action === 'preview') || null
  }

  function stricterPolicyEffectsForBaseline(baseline) {
    return ['mask', 'suppress', 'deny'].filter(effect => isProtectionEffectStricter(effect, baseline?.effect))
  }

  function canConfigurePolicyFromContext(baseline, policy) {
    if (!canReadPolicies.value || stricterPolicyEffectsForBaseline(baseline).length === 0) return false
    return policy ? canUpdatePolicies.value : canCreatePolicies.value
  }

  function protectionSummaryFromContext(baseline, policy) {
    if (!baseline) return t('security.policy.baselineMissing')
    if (policy?.state === 'active' && isProtectionEffectStricter(policy.current?.effect, baseline.effect)) {
      return t('security.policy.activeSummary', { baseline: effectLabel(baseline.effect), policy: effectLabel(policy.current?.effect) })
    }
    return t('security.policy.defaultSummary', { baseline: effectLabel(baseline.effect) })
  }

  function stricterPolicyEffects(assessment) {
    return stricterPolicyEffectsForBaseline(baselineForAssessment(assessment))
  }

  function canConfigurePolicy(assessment) {
    return canConfigurePolicyFromContext(baselineForAssessment(assessment), policyForAssessment(assessment))
  }

  function assessmentProtectionSummary(assessment) {
    return protectionSummaryFromContext(baselineForAssessment(assessment), policyForAssessment(assessment))
  }

  function protectionPolicyDialogView(assessment, context = {}) {
    const effects = Array.isArray(context.effects) ? context.effects : []
    return {
      componentKey: assessment?.component_key,
      assessmentSummary: assessmentSummary(assessment),
      protectionSummary: assessmentProtectionSummary(assessment),
      effectOptions: effects.map(effect => ({
        value: effect,
        label: effectLabel(effect),
        impact: t(`security.baseline.effectImpact.${effect}`)
      }))
    }
  }

  function assessmentProtectionView(assessment) {
    const activeAssessment = assessment?.current?.conclusion === 'sensitive' ? assessment : null
    if (!activeAssessment) {
      return { current: assessment, active: null, policy: null, canConfigurePolicy: false, protectionSummary: '' }
    }
    const baseline = baselineForAssessment(activeAssessment)
    const policy = policyForAssessment(activeAssessment)
    return {
      current: assessment,
      active: activeAssessment,
      policy,
      canConfigurePolicy: canConfigurePolicyFromContext(baseline, policy),
      protectionSummary: protectionSummaryFromContext(baseline, policy)
    }
  }

  function assessmentRowView(assessment) {
    const protection = assessmentProtectionView(assessment)
    return {
      id: assessment?.id,
      assessment,
      componentKey: assessment?.component_key,
      summary: assessmentSummary(assessment),
      rationale: assessment?.current?.rationale || '',
      conclusion: {
        type: protection.active ? 'success' : 'info',
        label: assessmentConclusionLabel(assessment?.current?.conclusion)
      },
      protectionSummary: protection.protectionSummary,
      actions: {
        canConfigurePolicy: Boolean(protection.active && protection.canConfigurePolicy),
        configurePolicyLabel: protection.policy?.state === 'active'
          ? t('security.policy.adjust')
          : t('security.policy.tighten'),
        canRestorePolicy: protection.policy?.state === 'active',
        canRevokeAssessment: Boolean(protection.active)
      }
    }
  }

  function assessmentListView(items, context = {}) {
    const rows = Array.isArray(items) ? items : []
    return {
      rows: rows.map(assessmentRowView),
      permissions: {
        canUpdate: Boolean(context.canUpdate),
        canRevokePolicies: Boolean(context.canRevokePolicies)
      }
    }
  }

  function activeGradesForType(typeID) {
    const activeGradeIDs = new Set(protectionBaselines.value
      .filter(item => item.enabled && String(item.sensitive_data_type_id) === String(typeID))
      .map(item => String(item.security_grade_id)))
    return securityGrades.value.filter(item => activeGradeIDs.has(String(item.id)))
  }

  function manualAssessmentDialogView(context = {}) {
    const components = Array.isArray(context.componentOptions) ? context.componentOptions : []
    return {
      componentsLoading: Boolean(context.componentsLoading),
      componentOptions: components.map(item => ({
        value: item?.component?.key,
        label: item?.component?.key,
        valueType: item?.component?.value_type
      })),
      sensitiveTypeOptions: namedReferenceOptions(sensitiveTypes.value),
      gradeOptions: namedReferenceOptions(activeGradesForType(context.sensitiveDataTypeID))
    }
  }

  return {
    manualAssessments,
    assessmentRowView,
    assessmentChangeDialogView,
    assessmentHistoryView,
    baselineForAssessment,
    policyForAssessment,
    stricterPolicyEffects,
    canConfigurePolicy,
    protectionPolicyDialogView,
    activeGradesForType,
    manualAssessmentDialogView,
    assessmentListView,
    assessmentProtectionView
  }
}
