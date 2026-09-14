import { createProtectionAssessmentPresentation } from './protectionAssessmentPresentation.mjs'
import {
  findingDecisionState,
  findingOutletRules,
  findingReviewState,
  normalizeDiscoverySummary
} from '../utils/protectionEnrollment.mjs'

export function createProtectionEnrollmentGovernancePresentation({
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
}) {
  const namedReferenceOptions = items => (Array.isArray(items) ? items : []).map(item => ({
    value: String(item.id),
    label: item.name
  }))

  const {
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
  } = createProtectionAssessmentPresentation({
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
  })

  function governanceSectionView(enrollment, governance) {
    const { pendingReviewCount } = normalizeDiscoverySummary(enrollment)
    return {
      pendingReview: pendingReviewCount > 0
        ? {
            count: pendingReviewCount,
            label: t('security.finding.pendingCount', { count: pendingReviewCount })
          }
        : null,
      canDesignate: Boolean(governance?.createAssessments
        && !['releasing', 'released'].includes(enrollment?.state))
    }
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

  function findingReviewRowView(finding, permissions) {
    return {
      id: finding?.id,
      finding,
      identity: resourceIdentityView(finding),
      candidate: {
        componentKey: finding?.component_key,
        sensitiveTypeName: typeName(finding?.sensitive_data_type_id)
      },
      recognition: {
        name: capabilityName(finding),
        version: finding?.detector_version
      },
      evidence: {
        description: evidenceDescription(finding),
        metadata: `${t('security.finding.confidenceValue', { value: confidenceLabel(finding?.confidence) })} · ${formatDateTime(finding?.observed_at)}`
      },
      actions: {
        canReview: Boolean(permissions?.canReview)
      }
    }
  }

  function recognitionMethodOption(capability) {
    const value = String(capability?.key || '')
    const i18nKey = String(capability?.name_i18n_key || '')
    const translated = i18nKey ? t(i18nKey) : ''
    const name = translated && translated !== i18nKey ? translated : value
    return {
      value,
      label: name === value ? value : `${name}（${value}）`
    }
  }

  function findingReviewQueueView(findings, context = {}) {
    const rows = Array.isArray(findings) ? findings : []
    const capabilities = new Map((Array.isArray(context.detectorCapabilities) ? context.detectorCapabilities : [])
      .map(item => [String(item?.key || ''), item]))
    for (const finding of rows) {
      const capability = finding?.explanation?.capability
      const key = String(finding?.detector_version || '')
      if (key && !capabilities.has(key)) capabilities.set(key, capability?.key ? capability : { key })
    }
    return {
      rows: rows.map(finding => findingReviewRowView(finding, { canReview: context.canReview })),
      filters: {
        sensitiveTypeOptions: namedReferenceOptions(sensitiveTypes.value),
        recognitionMethodOptions: [...capabilities.values()]
          .filter(item => item?.key)
          .sort((left, right) => String(left.key).localeCompare(String(right.key)))
          .map(recognitionMethodOption)
      }
    }
  }

  function findingReviewDialogView(finding, context) {
    const outlets = Array.isArray(finding?.explanation?.outlets) ? finding.explanation.outlets : []
    return {
      target: {
        componentKey: finding?.component_key,
        summary: `${typeName(finding?.sensitive_data_type_id)} · ${confidenceLabel(finding?.confidence)}`
      },
      remainingLabel: context?.remainingLabel || '',
      rationalePlaceholder: context?.rationalePlaceholder || '',
      sensitiveTypeOptions: namedReferenceOptions(sensitiveTypes.value),
      gradeOptions: namedReferenceOptions(context?.gradeOptions),
      basis: {
        recognitionMethod: capabilityName(finding),
        actualMatch: evidenceAuditDescription(finding),
        governance: {
          decision: decisionPresentation(finding),
          definition: effectiveDefinitionSummary(finding),
          baseline: baselineDescription(finding)
        },
        outlets: outlets.map(outlet => ({
          key: outlet.consumer_owner,
          owner: ownerLabel(outlet.consumer_owner),
          rule: outletRuleDescription(finding, outlet.consumer_owner)
        }))
      }
    }
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

  function assessmentComponent(assessmentID) {
    return assessments.value.find(item => item.id === assessmentID)?.component_key || t('security.exemption.unknownField')
  }

  function exemptionStatePresentation(exemption) {
    const state = String(exemption?.effective_state || 'expired')
    const types = { active: 'warning', expired: 'info', revoked: 'info' }
    return { type: types[state] || 'info', label: t(`security.exemption.states.${state}`) }
  }

  function exemptionRowView(exemption) {
    return {
      id: exemption?.id,
      exemption,
      componentKey: assessmentComponent(exemption?.assessment_id),
      state: exemptionStatePresentation(exemption),
      subjectId: exemption?.subject_id,
      outlet: `${ownerLabel(exemption?.consumer_owner)} · ${actionLabel(exemption?.action)}`,
      expiresAt: formatDateTime(exemption?.current?.expires_at),
      rationale: exemption?.current?.rationale || '',
      canRevoke: exemption?.effective_state === 'active'
    }
  }

  function exemptionSectionView(exemptions, context = {}) {
    const focusedID = context.focusedId == null ? '' : String(context.focusedId)
    return {
      visible: Boolean(context.canRead),
      loading: Boolean(context.loading),
      rows: (Array.isArray(exemptions) ? exemptions : []).map(exemption => ({
        ...exemptionRowView(exemption),
        focused: focusedID !== '' && String(exemption?.id) === focusedID
      })),
      canRevoke: Boolean(context.canRevoke)
    }
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

  function findingAssessmentView(finding) {
    return assessmentProtectionView(assessmentForFinding(finding))
  }

  function findingRowView(finding) {
    const explanation = finding?.explanation || {}
    const assessment = findingAssessmentView(finding)
    const threshold = explanation.automatic_adoption_threshold
    const outlets = Array.isArray(explanation.outlets) ? explanation.outlets : []
    return {
      id: finding?.id,
      finding,
      componentKey: finding?.component_key,
      sensitiveTypeName: typeName(finding?.sensitive_data_type_id),
      state: findingStatePresentation(finding),
      detection: {
        name: capabilityName(finding),
        evidence: evidenceDescription(finding),
        evidenceAudit: evidenceAuditDescription(finding),
        method: capabilityText(finding, 'method_i18n_key'),
        scope: capabilityScope(finding),
        privacy: capabilityText(finding, 'privacy_i18n_key'),
        limitations: capabilityText(finding, 'limitations_i18n_key'),
        version: explanation.capability?.key || finding?.detector_version,
        confidence: confidenceLabel(finding?.confidence),
        threshold: threshold == null
          ? null
          : {
              value: confidenceLabel(threshold),
              type: explanation.meets_automatic_threshold ? 'success' : 'warning'
            }
      },
      governance: {
        decision: decisionPresentation(finding),
        definition: effectiveDefinitionSummary(finding),
        baseline: baselineDescription(finding),
        protectionSummary: assessment.active ? assessment.protectionSummary : ''
      },
      outlets: outlets.map(outlet => ({
        key: outlet.consumer_owner,
        owner: ownerLabel(outlet.consumer_owner),
        rule: outletRuleDescription(finding, outlet.consumer_owner),
        acknowledgement: outletAcknowledgementPresentation(outlet)
      })),
      observedAt: formatDateTime(finding?.observed_at),
      review: finding?.review || null,
      assessment
    }
  }

  function findingListView(findings, context = {}) {
    const rows = Array.isArray(findings) ? findings : []
    return {
      rows: rows.map(findingRowView),
      permissions: {
        canReview: Boolean(context.canReview),
        canUpdateAssessments: Boolean(context.canUpdateAssessments),
        canRevokePolicies: Boolean(context.canRevokePolicies)
      }
    }
  }

  function governanceDetailView(enrollment, context = {}) {
    const findings = Array.isArray(context.findings) ? context.findings : []
    const assessments = Array.isArray(context.manualAssessments) ? context.manualAssessments : []
    return {
      visible: Boolean(context.canReadFindings || context.canReadAssessments),
      loading: Boolean(context.loading),
      section: governanceSectionView(enrollment, {
        createAssessments: context.canCreateAssessments
      }),
      findings: {
        ...findingListView(findings, {
          canReview: context.canReviewFindings,
          canUpdateAssessments: context.canUpdateAssessments,
          canRevokePolicies: context.canRevokePolicies
        }),
        visible: Boolean(context.canReadFindings && findings.length > 0),
        pagination: {
          page: context.findingsPage,
          total: context.findingsTotal,
          pageSize: context.findingsPageSize
        }
      },
      assessments: {
        ...assessmentListView(assessments, {
          canUpdate: context.canUpdateAssessments,
          canRevokePolicies: context.canRevokePolicies
        }),
        visible: assessments.length > 0
      },
      empty: findings.length === 0 && assessments.length === 0
    }
  }

  return {
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
  }
}
