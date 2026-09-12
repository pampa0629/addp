import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { useProtectionEnrollmentPresentation } from '../src/composables/useProtectionEnrollmentPresentation.mjs'

function createPresentation(overrides = {}) {
  const translations = new Map([
    ['security.enrollment.itemTypes.table', 'Table'],
    ['security.finding.actions.preview', 'Preview'],
    ['security.detectorCapabilities.phoneMetadata.name', 'Phone metadata']
  ])
  const t = vi.fn((key, params) => translations.get(key) || (params ? `${key}:${JSON.stringify(params)}` : key))
  const dependencies = {
    t,
    locale: ref('zh-cn'),
    activeWorkspace: ref('resources'),
    reviewQueueTotal: ref(4),
    detailRow: ref({ discovery_summary: { status: 'completed', finding_count: 3, pending_review_count: 2 } }),
    autoRefreshActive: ref(false),
    lastRefreshedAt: ref(null),
    engineNames: ref(new Map([[7, 'Business MySQL']])),
    sensitiveTypes: ref([{ id: 11, name: 'Phone' }]),
    securityClassifications: ref([{ id: 12, name: 'Personal' }]),
    securityGrades: ref([{ id: 13, name: 'High' }, { id: 14, name: 'Critical' }]),
    protectionBaselines: ref([
      { id: 15, enabled: true, sensitive_data_type_id: 11, security_grade_id: 13, effect: 'mask' }
    ]),
    assessments: ref([
      { id: 'assessment-1', current: { source_kind: 'manual', conclusion: 'sensitive', sensitive_data_type_id: 11, security_classification_id: 12, security_grade_id: 13 } },
      { id: 'assessment-2', current: { source_kind: 'finding', conclusion: 'not_sensitive' } }
    ]),
    policies: ref([
      { id: 'policy-1', assessment_id: 'assessment-1', consumer_owner: 'manager', action: 'preview', state: 'active', current: { effect: 'deny' } }
    ]),
    canReadPolicies: ref(true),
    canCreatePolicies: ref(true),
    canUpdatePolicies: ref(true),
    ...overrides
  }
  return {
    dependencies,
    presentation: useProtectionEnrollmentPresentation(dependencies)
  }
}

describe('protected-resource presentation model', () => {
  it('derives reactive refresh, review, and manual-assessment summaries', () => {
    const { dependencies, presentation } = createPresentation()

    expect(presentation.manualAssessments.value.map(item => item.id)).toEqual(['assessment-1'])
    expect(presentation.reviewRemainingLabel.value).toContain('security.finding.reviewRemainingResource')
    dependencies.activeWorkspace.value = 'review-queue'
    expect(presentation.reviewRemainingLabel.value).toContain('"count":4')
    dependencies.autoRefreshActive.value = true
    expect(presentation.refreshFeedback.value).toBe('security.enrollment.autoRefreshing')
  })

  it('presents resource identity and safe fallbacks without exposing raw missing values', () => {
    const { presentation } = createPresentation()

    expect(presentation.resourceName({ target_snapshot: { full_name: 'business.customers', item_type: 'table' } })).toBe('customers')
    expect(presentation.resourcePath({ target_snapshot: {} })).toBe('security.enrollment.snapshotUnavailable')
    expect(presentation.engineLabel(7)).toBe('Business MySQL')
    expect(presentation.engineLabel(99)).toContain('"id":99')
    expect(presentation.itemTypeLabel('TABLE')).toBe('Table')
    expect(presentation.itemTypeLabel('custom')).toBe('custom')
    expect(presentation.releaseBasisLabel('invalid')).toBe('security.common.notAvailable')
    expect(presentation.releaseActorLabel(8)).toContain('"id":8')
  })

  it('maps discovery and enrollment progress to stable presentation states', () => {
    const { presentation } = createPresentation()

    expect(presentation.discoveryPresentation({}).label).toBe('security.enrollment.discoveryNotCompleted')
    expect(presentation.discoveryPresentation({ discovery_summary: { status: 'completed', finding_count: 0 } }).label).toBe('security.enrollment.discoveryZeroFindings')
    expect(presentation.discoveryPresentation({ discovery_summary: { status: 'completed', finding_count: 2, pending_review_count: 0 } }).alertType).toBe('success')
    expect(presentation.discoveryPresentation({ discovery_summary: { status: 'completed', finding_count: 2, pending_review_count: 1 } }).type).toBe('warning')
    expect(presentation.presentationState({ state: 'enrolling', owner_progress: [{ acknowledged: true, projection_state: 'active' }] }).type).toBe('success')
    expect(presentation.presentationState({ state: 'enrolling', owner_progress: [{ acknowledged: true, projection_state: 'active' }, { acknowledged: false }] }).type).toBe('primary')
    expect(presentation.ownerPresentation({ state: 'released' }, {}).type).toBe('info')
  })

  it('presents owner rules and finding outlet rules from the same action and effect labels', () => {
    const { presentation } = createPresentation()
    const rules = [{ action: 'preview', effect: 'mask' }]

    expect(presentation.ownerEffectDescription({ acknowledged: true, projection_state: 'active', rules })).toContain('Preview')
    expect(presentation.outletRuleDescription({
      explanation: { outlets: [{ consumer_owner: 'manager', projection_state: 'active', acknowledged: true, rules }] }
    }, 'manager')).toContain('Preview')
    expect(presentation.outletRuleDescription({ explanation: { outlets: [] } }, 'manager')).toBe('security.finding.outletUnavailable')
  })

  it('resolves definition names, evidence, capabilities, and review states', () => {
    const { presentation } = createPresentation()
    const finding = {
      component_key: 'phone',
      detector_version: 'phone-metadata/v1',
      evidence: { matched_rule: 'terminal_field_name', field_type: 'string' },
      explanation: {
        decision_state: 'baseline_missing',
        effective_sensitive_data_type_id: 11,
        effective_security_classification_id: 12,
        effective_security_grade_id: 13,
        capability: { name_i18n_key: 'security.detectorCapabilities.phoneMetadata.name' }
      }
    }

    expect(presentation.typeName(11)).toBe('Phone')
    expect(presentation.classificationName(12)).toBe('Personal')
    expect(presentation.gradeName(13)).toBe('High')
    expect(presentation.confidenceLabel(0.876)).toBe('88%')
    expect(presentation.evidenceDescription(finding)).toContain('"type":"string"')
    expect(presentation.capabilityName(finding)).toBe('Phone metadata')
    expect(presentation.decisionPresentation(finding)).toEqual({ type: 'danger', label: 'security.finding.decisionStates.baseline_missing' })
    expect(presentation.findingStatePresentation({ review: { decision: 'adjust' } }).type).toBe('primary')
    expect(presentation.effectiveDefinitionSummary(finding)).toContain('"type":"Phone"')
  })

  it('selects only the effective baseline, policy, and stricter choices for an assessment', () => {
    const { dependencies, presentation } = createPresentation()
    const assessment = dependencies.assessments.value[0]

    expect(presentation.baselineForAssessment(assessment)?.id).toBe(15)
    expect(presentation.policyForAssessment(assessment)?.id).toBe('policy-1')
    expect(presentation.stricterPolicyEffects(assessment)).toEqual(['suppress', 'deny'])
    expect(presentation.activeGradesForType(11).map(item => item.id)).toEqual([13])
    expect(presentation.canConfigurePolicy(assessment)).toBe(true)
    expect(presentation.assessmentProtectionSummary(assessment)).toContain('security.policy.activeSummary')

    dependencies.canUpdatePolicies.value = false
    expect(presentation.canConfigurePolicy(assessment)).toBe(false)
  })

  it('links findings to their current formal assessment only when it remains sensitive', () => {
    const { presentation } = createPresentation()

    expect(presentation.activeAssessmentForFinding({ explanation: { assessment_id: 'assessment-1' } })?.id).toBe('assessment-1')
    expect(presentation.activeAssessmentForFinding({ explanation: { assessment_id: 'assessment-2' } })).toBeNull()
    expect(presentation.assessmentComponent('assessment-1')).toBe('security.exemption.unknownField')
    expect(presentation.assessmentConclusionLabel('other')).toBe('security.assessment.conclusions.not_sensitive')
  })
})
