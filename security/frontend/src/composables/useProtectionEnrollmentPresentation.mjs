import { computed } from 'vue'
import {
  findingDecisionState,
  findingOutletRules,
  findingReviewState,
  isProtectionEffectStricter,
  isZeroFindingDiscovery,
  normalizeDiscoverySummary
} from '../utils/protectionEnrollment.mjs'

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
  const manualAssessments = computed(() => assessments.value.filter(item => item.current?.source_kind === 'manual'))
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

  function resourceName(row) {
    const fullName = String(row.target_snapshot?.full_name || '').trim()
    if (!fullName) return t('security.enrollment.unknownResource')
    const separator = ['object', 'file', 'directory'].includes(String(row.target_snapshot?.item_type || '').toLowerCase()) ? '/' : '.'
    return fullName.split(separator).filter(Boolean).at(-1) || fullName
  }

  function resourcePath(row) {
    return String(row.target_snapshot?.full_name || '').trim() || t('security.enrollment.snapshotUnavailable')
  }

  function engineLabel(engineID) {
    const id = Number(engineID || 0)
    return engineNames.value.get(id) || t('security.enrollment.engineId', { id: id || '-' })
  }

  function itemTypeLabel(itemType) {
    const key = String(itemType || 'unknown').toLowerCase()
    const translated = t(`security.enrollment.itemTypes.${key}`)
    return translated === `security.enrollment.itemTypes.${key}` ? key : translated
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

  function discoveryPresentation(row) {
    const summary = normalizeDiscoverySummary(row)
    if (summary.status !== 'completed') {
      return {
        type: 'info', alertType: 'info', label: t('security.enrollment.discoveryNotCompleted'),
        detailTitle: t('security.enrollment.discoveryNotCompletedTitle'),
        detailDescription: t('security.enrollment.discoveryNotCompletedDescription')
      }
    }
    if (summary.findingCount === 0) {
      return {
        type: 'success', alertType: 'info', label: t('security.enrollment.discoveryZeroFindings'),
        detailTitle: t('security.enrollment.discoveryZeroFindingsTitle'),
        detailDescription: t('security.enrollment.discoveryZeroFindingsDescription')
      }
    }
    if (summary.pendingReviewCount === 0) {
      return {
        type: 'success', alertType: 'success', label: t('security.enrollment.discoveryReviewCompleted'),
        detailTitle: t('security.enrollment.discoveryReviewCompletedTitle', { count: summary.findingCount }),
        detailDescription: t('security.enrollment.discoveryReviewCompletedDescription')
      }
    }
    return {
      type: 'warning', alertType: 'warning', label: t('security.enrollment.discoveryPendingCount', { count: summary.pendingReviewCount }),
      detailTitle: t('security.enrollment.discoveryFindingCountTitle', { count: summary.findingCount }),
      detailDescription: t('security.enrollment.discoveryFindingCountDescription', { count: summary.pendingReviewCount })
    }
  }

  function presentationState(row) {
    if (row.state === 'released') return { type: 'info', label: t('security.enrollment.states.released'), description: t('security.enrollment.stateDescriptions.released') }
    if (row.state === 'releasing') return { type: 'warning', label: t('security.enrollment.states.releasing'), description: t('security.enrollment.stateDescriptions.releasing') }
    const owners = Array.isArray(row.owner_progress) ? row.owner_progress : []
    if (owners.length > 0 && owners.every(owner => owner.acknowledged && owner.projection_state === 'active')) {
      return { type: 'success', label: t('security.enrollment.states.active'), description: t('security.enrollment.stateDescriptions.active') }
    }
    if (row.state === 'active') return { type: 'success', label: t('security.enrollment.states.active'), description: t('security.enrollment.stateDescriptions.active') }
    const activeOwners = owners.filter(owner => owner.acknowledged && owner.projection_state === 'active').length
    if (activeOwners > 0) return { type: 'primary', label: t('security.enrollment.states.partiallyActive'), description: t('security.enrollment.stateDescriptions.partiallyActive', { count: activeOwners }) }
    if (row.state === 'activating') return { type: 'warning', label: t('security.enrollment.states.activating'), description: t('security.enrollment.stateDescriptions.activating') }
    return { type: 'primary', label: t('security.enrollment.states.enrolling'), description: t('security.enrollment.stateDescriptions.enrolling') }
  }

  function ownerPresentation(row, owner) {
    if (row.state === 'released') return { type: 'info', label: t('security.enrollment.ownerStates.released') }
    if (!owner.acknowledged) return { type: 'warning', label: t('security.enrollment.ownerStates.waiting') }
    if (owner.projection_state === 'active') return { type: 'success', label: t('security.enrollment.ownerStates.active') }
    return { type: 'info', label: t('security.enrollment.ownerStates.denied') }
  }

  function ownerEffectDescription(owner) {
    const rules = Array.isArray(owner.rules) ? owner.rules : []
    if (!owner.acknowledged) return t('security.enrollment.ownerEffectWaiting')
    if (owner.projection_state === 'enrolling') {
      return owner.consumer_owner === 'manager'
        ? t('security.enrollment.ownerEffectDeniedPendingRule')
        : t('security.enrollment.ownerEffectDeniedUnsupported')
    }
    return rules.length
      ? t('security.enrollment.ownerEffectRequirements', {
          rules: rules.map(rule => t('security.finding.outletRule', {
            action: actionLabel(rule.action), effect: effectLabel(rule.effect)
          })).join('；')
        })
      : t('security.enrollment.ownerEffectActive')
  }

  function typeName(id) {
    const match = sensitiveTypes.value.find(item => String(item.id) === String(id))
    return match?.name || t('security.finding.typeId', { id })
  }

  function classificationName(id) {
    const match = securityClassifications.value.find(item => String(item.id) === String(id))
    return match?.name || t('security.finding.classificationId', { id })
  }

  function gradeName(id) {
    const match = securityGrades.value.find(item => String(item.id) === String(id))
    return match?.name || t('security.finding.gradeId', { id })
  }

  function confidenceLabel(value) {
    const confidence = Number(value)
    return Number.isFinite(confidence) ? `${Math.round(confidence * 100)}%` : t('security.common.notAvailable')
  }

  function evidenceDescription(finding) {
    const rule = String(finding?.evidence?.matched_rule || '')
    if (rule === 'terminal_field_name') return t('security.finding.evidenceRules.terminalFieldName', { type: finding?.evidence?.field_type || '-' })
    if (rule === 'exact_ascii_digit_run') return t('security.finding.evidenceRules.exactDigitRun', { count: Number(finding?.evidence?.match_count || 0) })
    return t('security.finding.evidenceRules.detectorMatch')
  }

  function capabilityText(finding, key) {
    const i18nKey = String(finding?.explanation?.capability?.[key] || '')
    if (!i18nKey) return t('security.common.notAvailable')
    const translated = t(i18nKey)
    return translated === i18nKey ? i18nKey : translated
  }

  function capabilityScope(finding) {
    const capability = finding?.explanation?.capability || {}
    const itemTypes = Array.isArray(capability.supported_item_types)
      ? capability.supported_item_types.map(itemTypeLabel).join('、')
      : t('security.common.notAvailable')
    const fieldTypes = Array.isArray(capability.supported_field_types) && capability.supported_field_types.length
      ? capability.supported_field_types.map(type => t(`security.detector.fieldTypes.${type}`)).join('、')
      : t('security.finding.ruleAudit.notApplicable')
    return t('security.finding.ruleAudit.scopeValue', {
      target: t(`security.detector.targets.${capability.target_kind}`),
      evidence: t(`security.detector.evidenceSources.${capability.evidence_source}`),
      itemTypes,
      fieldTypes
    })
  }

  function evidenceAuditDescription(finding) {
    const evidence = finding?.evidence || {}
    if (evidence.matched_rule === 'terminal_field_name') {
      return t('security.finding.ruleAudit.metadataEvidence', {
        component: evidence.component_key || finding.component_key,
        terminal: evidence.semantic_terminal || '-',
        normalized: evidence.normalized_terminal || '-',
        alias: evidence.matched_alias || '-',
        fieldType: evidence.field_type || '-'
      })
    }
    if (evidence.matched_rule === 'exact_ascii_digit_run') {
      return t('security.finding.ruleAudit.documentEvidence', {
        count: Number(evidence.match_count || 0),
        rule: evidence.matched_rule
      })
    }
    return JSON.stringify(evidence)
  }

  function capabilityName(finding) {
    const key = String(finding?.explanation?.capability?.name_i18n_key || '')
    if (key) {
      const translated = t(key)
      if (translated !== key) return translated
    }
    return String(finding?.detector_version || t('security.common.notAvailable'))
  }

  function decisionPresentation(finding) {
    const state = findingDecisionState(finding)
    const types = {
      automatic: 'success', formal: 'success', awaiting_review: 'warning', detector_inactive: 'info',
      baseline_missing: 'danger', rejected: 'info', revoked: 'info', superseded: 'info'
    }
    return { type: types[state], label: t(`security.finding.decisionStates.${state}`) }
  }

  function effectiveDefinitionSummary(finding) {
    const explanation = finding?.explanation || {}
    if (!explanation.effective_sensitive_data_type_id || !explanation.effective_security_classification_id || !explanation.effective_security_grade_id) {
      return t('security.finding.noEffectiveDefinition')
    }
    return t('security.finding.effectiveDefinition', {
      type: typeName(explanation.effective_sensitive_data_type_id),
      classification: classificationName(explanation.effective_security_classification_id),
      grade: gradeName(explanation.effective_security_grade_id)
    })
  }

  function baselineDescription(finding) {
    const baseline = finding?.explanation?.baseline
    if (!baseline) return t('security.finding.noEffectiveBaseline')
    if (baseline.effect === 'mask') {
      return t('security.finding.baselineMask', { prefix: baseline.keep_prefix, suffix: baseline.keep_suffix })
    }
    return t('security.finding.baselineEffect', { effect: effectLabel(baseline.effect) })
  }

  function actionLabel(action) {
    const normalized = String(action || '')
    const translated = t(`security.finding.actions.${normalized}`)
    return translated === `security.finding.actions.${normalized}` ? normalized : translated
  }

  function assessmentComponent(assessmentID) {
    return assessments.value.find(item => item.id === assessmentID)?.component_key || t('security.exemption.unknownField')
  }

  function exemptionStatePresentation(exemption) {
    const state = String(exemption?.effective_state || 'expired')
    const types = { active: 'warning', expired: 'info', revoked: 'info' }
    return { type: types[state] || 'info', label: t(`security.exemption.states.${state}`) }
  }

  function outletRuleDescription(finding, owner) {
    const outlet = findingOutletRules(finding, owner)
    if (!outlet) return t('security.finding.outletUnavailable')
    if (!outlet.rules.length) {
      return outlet.projectionState === 'enrolling'
        ? t('security.finding.outletConservativeDeny')
        : t('security.finding.outletNoFieldRule')
    }
    return outlet.rules.map(rule => t('security.finding.outletRule', {
      action: actionLabel(rule.action), effect: effectLabel(rule.effect)
    })).join('；')
  }

  function outletAcknowledgementPresentation(outlet) {
    return outlet?.acknowledged
      ? { type: 'success', label: t('security.finding.outletAcknowledged') }
      : { type: 'warning', label: t('security.finding.outletWaiting') }
  }

  function findingStatePresentation(finding) {
    const state = findingReviewState(finding)
    const types = { pending: 'warning', confirm: 'success', adjust: 'primary', reject: 'info' }
    return { type: types[state], label: t(`security.finding.states.${state}`) }
  }

  function assessmentForFinding(finding) {
    const id = String(finding?.explanation?.assessment_id || '')
    if (!id) return null
    return assessments.value.find(item => item.id === id) || null
  }

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

  function findingAssessmentView(finding) {
    const assessment = assessmentForFinding(finding)
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

  function activeGradesForType(typeID) {
    const activeGradeIDs = new Set(protectionBaselines.value
      .filter(item => item.enabled && String(item.sensitive_data_type_id) === String(typeID))
      .map(item => String(item.security_grade_id)))
    return securityGrades.value.filter(item => activeGradeIDs.has(String(item.id)))
  }

  return {
    manualAssessments,
    refreshFeedback,
    reviewRemainingLabel,
    normalizeDiscoverySummary,
    isZeroFindingDiscovery,
    ownerLabel,
    effectLabel,
    resourceName,
    resourcePath,
    engineLabel,
    itemTypeLabel,
    formatDateTime,
    releaseBasisLabel,
    releaseActorLabel,
    discoveryPresentation,
    presentationState,
    ownerPresentation,
    ownerEffectDescription,
    typeName,
    classificationName,
    gradeName,
    confidenceLabel,
    evidenceDescription,
    capabilityText,
    capabilityScope,
    evidenceAuditDescription,
    capabilityName,
    decisionPresentation,
    effectiveDefinitionSummary,
    baselineDescription,
    actionLabel,
    assessmentComponent,
    exemptionStatePresentation,
    outletRuleDescription,
    outletAcknowledgementPresentation,
    findingStatePresentation,
    findingAssessmentView,
    assessmentSummary,
    assessmentRevisionSummary,
    assessmentConclusionLabel,
    assessmentRevisionSourceLabel,
    assessmentActorLabel,
    baselineForAssessment,
    policyForAssessment,
    stricterPolicyEffects,
    canConfigurePolicy,
    assessmentProtectionSummary,
    activeGradesForType
  }
}
