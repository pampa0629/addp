import { computed } from 'vue'
import { protectionPolicyScopeKey } from '../utils/protectionPolicy.mjs'
import { isProtectionEffectStricter } from '../utils/protectionEnrollment.mjs'
import { algorithmNameKey, prefixAlgorithm, constantAlgorithm } from '../utils/protectionAlgorithm.mjs'

export function createProtectionAssessmentPresentation({
  t,
  sensitiveTypes,
  securityGrades,
  protectionBaselines,
  assessments,
  policies,
  policyScopes,
  canReadPolicies,
  canCreatePolicies,
  canUpdatePolicies,
  formatDateTime,
  ownerLabel,
  actionLabel,
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

  function policyRevisionSummary(revision) {
    if (revision.effect !== 'mask') return effectLabel(revision.effect)
    const algorithm = t(algorithmNameKey(revision.algorithm))
    const invalid = effectLabel(revision.invalid_value_effect)
    if (revision.algorithm !== prefixAlgorithm && revision.algorithm !== constantAlgorithm) {
      return t('security.policy.historyAlgorithm', { algorithm, invalid })
    }
    const parameters = revision.parameters || {}
    const details = revision.algorithm === prefixAlgorithm
      ? t('security.baseline.maskSummary', { prefix: parameters.prefix_runes, suffix: parameters.suffix_runes, mask: parameters.mask_rune })
      : t('security.policy.historyConstant', { value: JSON.stringify(parameters.value) })
    return t('security.policy.historyConfiguration', {
      algorithm,
      parameters: details,
      invalid
    })
  }

  function assessmentHistoryView(assessment, policyHistories = []) {
    const currentRevision = Number(assessment?.current_revision)
    const history = Array.isArray(assessment?.history) ? assessment.history : []
    const items = history.map(revision => {
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
    for (const policy of policyHistories) {
      for (const revision of policy.history || []) {
        const timestamp = formatDateTime(revision.created_at)
        items.push({
          id: revision.id,
          timestamp,
          current: Number(revision.revision) === Number(policy.current_revision),
          revisionLabel: t('security.policy.historyRevision', { revision: revision.revision }),
          conclusion: { type: revision.state === 'active' ? 'success' : 'info', label: t(`security.policy.historyStates.${revision.state}`) },
          sourceLabel: t('security.policy.historySource', { owner: ownerLabel(policy.consumer_owner), action: actionLabel(policy.action) }),
          summary: policyRevisionSummary(revision),
          roleOptions: (revision.role_options || []).map(option => ({ id: option.id, text: t('security.roleOptions.history', { name: option.name, roles: option.role_keys.join(', '), method: policyRevisionSummary(option.decision) }) })),
          rationale: revision.rationale || '',
          metadata: t('security.assessment.historyMeta', { actor: assessmentActorLabel(revision.created_by), time: timestamp })
        })
      }
    }
    return { componentKey: assessment?.component_key, items }
  }

  function baselineForAssessment(assessment) {
    return protectionBaselines.value.find(item => item.enabled
      && String(item.sensitive_data_type_id) === String(assessment?.current?.sensitive_data_type_id)
      && String(item.security_grade_id) === String(assessment?.current?.security_grade_id)) || null
  }

  function policyForAssessment(assessment, scope) {
    return policies.value.find(item => item.assessment_id === assessment?.id
      && item.consumer_owner === scope?.consumer_owner
      && item.action === scope?.action) || null
  }

  function stricterPolicyEffectsForBaseline(baseline) {
    return ['mask', 'suppress', 'deny'].filter(effect => isProtectionEffectStricter(effect, baseline?.effect) || (effect === 'mask' && baseline?.effect === 'mask'))
  }

  function canConfigurePolicyFromContext(baseline, policy) {
    if (!canReadPolicies.value || stricterPolicyEffectsForBaseline(baseline).length === 0) return false
    return policy ? canUpdatePolicies.value : canCreatePolicies.value
  }

  function protectionSummaryFromContext(baseline, policy) {
    if (!baseline) return t('security.policy.baselineMissing')
    if (policy?.state === 'active' && (isProtectionEffectStricter(policy.current?.effect, baseline.effect) || policy.current?.effect === 'mask')) {
      return t('security.policy.activeSummary', { baseline: effectLabel(baseline.effect), policy: effectLabel(policy.current?.effect) })
    }
    return t('security.policy.defaultSummary', { baseline: effectLabel(baseline.effect) })
  }

  function stricterPolicyEffects(assessment) {
    return stricterPolicyEffectsForBaseline(baselineForAssessment(assessment))
  }

  function canConfigurePolicy(assessment, scope) {
    if (assessment?.component_key === '$document.text') return false
    if (!scope) return policyScopes.value.some(item => canConfigurePolicy(assessment, item))
    return canConfigurePolicyFromContext(baselineForAssessment(assessment), policyForAssessment(assessment, scope))
  }

  function assessmentProtectionSummary(assessment, scope) {
    const baseline = baselineForAssessment(assessment)
    if (scope) return t('security.policy.scopeSummary', { scope: t(`security.enrollment.owners.${scope.consumer_owner}`), summary: protectionSummaryFromContext(baseline, policyForAssessment(assessment, scope)) })
    const active = policyScopes.value.filter(item => policyForAssessment(assessment, item)?.state === 'active')
    if (!active.length) return protectionSummaryFromContext(baseline, null)
    return active.map(item => t('security.policy.scopeSummary', {
      scope: t(`security.enrollment.owners.${item.consumer_owner}`),
      summary: protectionSummaryFromContext(baseline, policyForAssessment(assessment, item))
    })).join(t('security.policy.scopeSeparator'))
  }

  function protectionPolicyDialogView(assessment, context = {}) {
    const effects = Array.isArray(context.effects) ? context.effects : []
    return {
      baseline: baselineForAssessment(assessment),
      fieldType: assessment?.current?.component?.value_type,
      componentKey: assessment?.component_key,
      assessmentSummary: assessmentSummary(assessment),
      protectionSummary: assessmentProtectionSummary(assessment, context.scope),
      scopeValue: protectionPolicyScopeKey(context.scope),
      scopeOptions: policyScopes.value
        .filter(scope => !context.restore || policyForAssessment(assessment, scope)?.state === 'active')
        .map(scope => ({ value: protectionPolicyScopeKey(scope), label: t(`security.enrollment.owners.${scope.consumer_owner}`), disabled: !context.restore && !canConfigurePolicy(assessment, scope) })),
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
    const fieldPolicies = policyScopes.value.map(scope => policyForAssessment(activeAssessment, scope)).filter(Boolean)
    const policy = fieldPolicies.find(item => item.state === 'active') || fieldPolicies[0] || null
    return {
      current: assessment,
      active: activeAssessment,
      policy,
      canConfigurePolicy: canConfigurePolicy(activeAssessment),
      protectionSummary: assessmentProtectionSummary(activeAssessment)
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
