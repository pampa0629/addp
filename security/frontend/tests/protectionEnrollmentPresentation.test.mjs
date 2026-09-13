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
      { id: 'assessment-1', component_key: 'phone', current: { source_kind: 'manual', conclusion: 'sensitive', sensitive_data_type_id: 11, security_classification_id: 12, security_grade_id: 13, rationale: 'confirmed' } },
      { id: 'assessment-2', component_key: 'nickname', current: { source_kind: 'finding', conclusion: 'not_sensitive', rationale: 'not sensitive' } }
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

    const resource = { target_snapshot: { full_name: 'business.customers', item_type: 'table', engine_id: 7 } }

    expect(presentation.resourceName(resource)).toBe('customers')
    expect(presentation.resourcePath({ target_snapshot: {} })).toBe('security.enrollment.snapshotUnavailable')
    expect(presentation.engineLabel(7)).toBe('Business MySQL')
    expect(presentation.engineLabel(99)).toContain('"id":99')
    expect(presentation.itemTypeLabel('TABLE')).toBe('Table')
    expect(presentation.itemTypeLabel('custom')).toBe('custom')
    expect(presentation.resourceIdentityView(resource)).toEqual({
      resource,
      name: 'customers',
      path: 'business.customers',
      itemType: 'Table',
      engine: 'Business MySQL'
    })
    expect(presentation.releaseBasisLabel('invalid')).toBe('security.common.notAvailable')
    expect(presentation.releaseActorLabel(8)).toContain('"id":8')
  })

  it('derives each protected resource list row through one presentation model', () => {
    const { presentation } = createPresentation()
    const enrollment = {
      id: 'enrollment-1',
      state: 'released',
      target_snapshot: { full_name: 'business.customers', item_type: 'table', engine_id: 7 },
      owner_progress: [{ consumer_owner: 'manager', acknowledged: true, projection_state: 'active' }],
      discovery_summary: { status: 'completed', finding_count: 2, pending_review_count: 1 },
      last_discovered_at: '2026-01-02T03:04:05Z',
      released_at: '2026-01-03T03:04:05Z'
    }

    const row = presentation.resourceRowView(enrollment)

    expect(row.id).toBe('enrollment-1')
    expect(row.enrollment).toBe(enrollment)
    expect(row.identity.resource).toBe(enrollment)
    expect(row.identity.name).toBe('customers')
    expect(row.state.type).toBe('info')
    expect(row.owners).toEqual([{
      key: 'manager',
      label: 'security.enrollment.owners.manager',
      state: { type: 'info', label: 'security.enrollment.ownerStates.released' }
    }])
    expect(row.discovery.type).toBe('warning')
    expect(row.discovery.observedAt).not.toBe('security.common.notAvailable')
    expect(row.releasedAt).not.toBe('security.common.notAvailable')
    expect(row.canReEnroll).toBe(true)
  })

  it('derives resource identity, owners, audit, and lifecycle actions through one detail view', () => {
    const { presentation } = createPresentation()
    const enrollment = {
      id: 'enrollment-1',
      state: 'active',
      target: { resource_identity: 'resource-fingerprint' },
      target_snapshot: { full_name: 'business.customers', item_type: 'table' },
      discovery_summary: { status: 'completed', finding_count: 0 },
      owner_progress: [{
        consumer_owner: 'manager',
        acknowledged: true,
        projection_state: 'active',
        rules: [{ action: 'preview', effect: 'mask' }]
      }],
      last_discovered_at: '2026-01-02T03:04:05Z',
      created_at: '2026-01-01T03:04:05Z'
    }
    const active = presentation.resourceDetailView(enrollment, { canCreate: true, canUpdate: true })

    expect(active.enrollment).toBe(enrollment)
    expect(active.identity).toEqual({ name: 'customers', path: 'business.customers' })
    expect(active.state.type).toBe('success')
    expect(active.discovery.label).toBe('security.enrollment.discoveryZeroFindings')
    expect(active.releaseAudit).toBeNull()
    expect(active.owners[0].state.type).toBe('success')
    expect(active.technical).toEqual({
      resourceIdentity: 'resource-fingerprint',
      enrollmentId: 'enrollment-1',
      releaseSourceSnapshotHash: null
    })
    expect(active.actions).toEqual({
      canReEnroll: false,
      canRediscover: true,
      release: {
        basis: 'no_supported_findings',
        label: 'security.enrollment.confirmNoProtectionNeeded'
      }
    })

    const released = presentation.resourceDetailView({
      ...enrollment,
      state: 'released',
      release_basis: 'manual',
      release_requested_by: 8,
      release_requested_at: '2026-01-03T03:04:05Z',
      released_at: '2026-01-04T03:04:05Z',
      release_reason: 'retired',
      release_source_snapshot_hash: 'snapshot-hash'
    }, { canCreate: true, canUpdate: true })
    expect(released.releaseAudit?.basis).toBe('security.enrollment.releaseBases.manual')
    expect(released.releaseAudit?.reason).toBe('retired')
    expect(released.technical.releaseSourceSnapshotHash).toBe('snapshot-hash')
    expect(released.actions).toEqual({
      canReEnroll: true,
      canRediscover: false,
      release: null
    })
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

  it('derives the governance header through one section view', () => {
    const { presentation } = createPresentation()

    expect(presentation.governanceSectionView({
      state: 'active',
      discovery_summary: { status: 'completed', finding_count: 2, pending_review_count: 2 }
    }, { createAssessments: true })).toEqual({
      pendingReview: {
        count: 2,
        label: 'security.finding.pendingCount:{"count":2}'
      },
      canDesignate: true
    })
    expect(presentation.governanceSectionView({
      state: 'released',
      discovery_summary: { status: 'completed', finding_count: 0, pending_review_count: 0 }
    }, { createAssessments: true })).toEqual({
      pendingReview: null,
      canDesignate: false
    })
  })

  it('presents owner rules and finding outlet rules from the same action and effect labels', () => {
    const { presentation } = createPresentation()
    const rules = [{ action: 'preview', effect: 'mask' }]

    expect(presentation.ownerEffectDescription({ acknowledged: true, projection_state: 'active', rules })).toContain('Preview')
    const row = presentation.findingRowView({
      explanation: { outlets: [{ consumer_owner: 'manager', projection_state: 'active', acknowledged: true, rules }] }
    })
    expect(row.outlets[0].rule).toContain('Preview')
  })

  it('resolves definition names, evidence, capabilities, and review states', () => {
    const { presentation } = createPresentation()
    const finding = {
      id: 'finding-1',
      component_key: 'phone',
      detector_version: 'phone-metadata/v1',
      confidence: 0.876,
      observed_at: '2026-01-02T03:04:05Z',
      review: { decision: 'adjust', rationale: 'confirmed' },
      evidence: { matched_rule: 'terminal_field_name', field_type: 'string' },
      explanation: {
        assessment_id: 'assessment-1',
        automatic_adoption_threshold: 0.8,
        meets_automatic_threshold: true,
        decision_state: 'baseline_missing',
        effective_sensitive_data_type_id: 11,
        effective_security_classification_id: 12,
        effective_security_grade_id: 13,
        capability: { key: 'phone-metadata/v1', name_i18n_key: 'security.detectorCapabilities.phoneMetadata.name' },
        outlets: [{
          consumer_owner: 'manager',
          acknowledged: true,
          projection_state: 'active',
          rules: [{ action: 'preview', effect: 'mask' }]
        }]
      }
    }
    const row = presentation.findingRowView(finding)

    expect(row.finding).toBe(finding)
    expect(row.state.type).toBe('primary')
    expect(row.detection.name).toBe('Phone metadata')
    expect(row.detection.evidence).toContain('"type":"string"')
    expect(row.detection.confidence).toBe('88%')
    expect(row.detection.threshold).toEqual({ value: '80%', type: 'success' })
    expect(row.governance.decision.type).toBe('danger')
    expect(row.governance.definition).toContain('"type":"Phone"')
    expect(row.governance.definition).toContain('"classification":"Personal"')
    expect(row.governance.definition).toContain('"grade":"High"')
    expect(row.governance.protectionSummary).toContain('security.policy.activeSummary')
    expect(row.outlets[0].acknowledgement.type).toBe('success')
    expect(row.assessment.active?.id).toBe('assessment-1')
  })

  it('derives each pending review candidate through one row model', () => {
    const { presentation } = createPresentation()
    const finding = {
      id: 'finding-1',
      component_key: 'phone',
      sensitive_data_type_id: 11,
      detector_version: 'phone-metadata/v1',
      confidence: 0.876,
      observed_at: '2026-01-02T03:04:05Z',
      target_snapshot: { full_name: 'business.customers', item_type: 'table', engine_id: 7 },
      explanation: {
        capability: { name_i18n_key: 'security.detectorCapabilities.phoneMetadata.name' }
      },
      evidence: { matched_rule: 'terminal_field_name', field_type: 'string' }
    }

    const row = presentation.findingReviewRowView(finding, { canReview: true })

    expect(row.id).toBe('finding-1')
    expect(row.finding).toBe(finding)
    expect(row.identity.resource).toBe(finding)
    expect(row.identity.name).toBe('customers')
    expect(row.candidate).toEqual({ componentKey: 'phone', sensitiveTypeName: 'Phone' })
    expect(row.recognition).toEqual({ name: 'Phone metadata', version: 'phone-metadata/v1' })
    expect(row.evidence.description).toContain('"type":"string"')
    expect(row.evidence.metadata).toContain('88%')
    expect(row.actions.canReview).toBe(true)
    expect(presentation.findingReviewRowView(finding).actions.canReview).toBe(false)
  })

  it('derives the finding review dialog through one presentation model', () => {
    const { presentation } = createPresentation()
    const finding = {
      component_key: 'phone',
      sensitive_data_type_id: 11,
      detector_version: 'phone-metadata/v1',
      confidence: 0.876,
      evidence: { matched_rule: 'terminal_field_name', field_type: 'string' },
      explanation: {
        decision_state: 'baseline_missing',
        effective_sensitive_data_type_id: 11,
        effective_security_classification_id: 12,
        effective_security_grade_id: 13,
        capability: { name_i18n_key: 'security.detectorCapabilities.phoneMetadata.name' },
        outlets: [{
          consumer_owner: 'manager',
          acknowledged: true,
          projection_state: 'active',
          rules: [{ action: 'preview', effect: 'mask' }]
        }]
      }
    }
    const gradeOptions = [{ id: 13, name: 'High' }]

    const dialog = presentation.findingReviewDialogView(finding, {
      remainingLabel: '3 remaining',
      rationalePlaceholder: 'Explain the decision',
      gradeOptions
    })

    expect(dialog.target).toEqual({ componentKey: 'phone', summary: 'Phone · 88%' })
    expect(dialog.remainingLabel).toBe('3 remaining')
    expect(dialog.rationalePlaceholder).toBe('Explain the decision')
    expect(dialog.gradeOptions).toBe(gradeOptions)
    expect(dialog.basis.recognitionMethod).toBe('Phone metadata')
    expect(dialog.basis.actualMatch).toContain('security.finding.ruleAudit.metadataEvidence')
    expect(dialog.basis.governance.decision.type).toBe('danger')
    expect(dialog.basis.governance.definition).toContain('"type":"Phone"')
    expect(dialog.basis.outlets[0]).toEqual({
      key: 'manager',
      owner: 'security.enrollment.owners.manager',
      rule: 'security.finding.outletRule:{"action":"Preview","effect":"security.enrollment.effects.mask"}'
    })
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

  it('derives each finding assessment and policy view through one row model', () => {
    const { presentation } = createPresentation()

    const sensitive = presentation.findingRowView({ explanation: { assessment_id: 'assessment-1', outlets: [] } }).assessment
    expect(sensitive.current?.id).toBe('assessment-1')
    expect(sensitive.active?.id).toBe('assessment-1')
    expect(sensitive.policy?.id).toBe('policy-1')
    expect(sensitive.canConfigurePolicy).toBe(true)
    expect(sensitive.protectionSummary).toContain('security.policy.activeSummary')

    const notSensitive = presentation.findingRowView({ explanation: { assessment_id: 'assessment-2', outlets: [] } }).assessment
    expect(notSensitive.current?.id).toBe('assessment-2')
    expect(notSensitive.active).toBeNull()
    expect(notSensitive.policy).toBeNull()
    expect(notSensitive.canConfigurePolicy).toBe(false)
    expect(notSensitive.protectionSummary).toBe('')
    expect(presentation.assessmentConclusionLabel('other')).toBe('security.assessment.conclusions.not_sensitive')
  })

  it('derives each assessment card through one row model', () => {
    const { dependencies, presentation } = createPresentation()

    const sensitive = presentation.assessmentRowView(dependencies.assessments.value[0])
    expect(sensitive.id).toBe('assessment-1')
    expect(sensitive.componentKey).toBe('phone')
    expect(sensitive.rationale).toBe('confirmed')
    expect(sensitive.summary).toContain('"type":"Phone"')
    expect(sensitive.conclusion).toEqual({ type: 'success', label: 'security.assessment.conclusions.sensitive' })
    expect(sensitive.protectionSummary).toContain('security.policy.activeSummary')
    expect(sensitive.actions).toEqual({
      canConfigurePolicy: true,
      configurePolicyLabel: 'security.policy.adjust',
      canRestorePolicy: true,
      canRevokeAssessment: true
    })

    const notSensitive = presentation.assessmentRowView(dependencies.assessments.value[1])
    expect(notSensitive.conclusion).toEqual({ type: 'info', label: 'security.assessment.conclusions.not_sensitive' })
    expect(notSensitive.protectionSummary).toBe('')
    expect(notSensitive.actions).toEqual({
      canConfigurePolicy: false,
      configurePolicyLabel: 'security.policy.tighten',
      canRestorePolicy: false,
      canRevokeAssessment: false
    })
  })

  it('derives assessment revisions through one history presentation model', () => {
    const { presentation } = createPresentation()
    const assessment = {
      component_key: 'phone',
      current_revision: 2,
      history: [{
        id: 'revision-1',
        revision: 1,
        conclusion: 'not_sensitive',
        source_kind: 'finding',
        sensitive_data_type_id: 11,
        security_classification_id: 12,
        security_grade_id: 13,
        rationale: 'initial review',
        created_by: 8,
        created_at: '2026-01-02T03:04:05Z'
      }, {
        id: 'revision-2',
        revision: 2,
        conclusion: 'sensitive',
        source_kind: 'manual',
        sensitive_data_type_id: 11,
        security_classification_id: 12,
        security_grade_id: 13,
        rationale: 'confirmed',
        created_by: 9,
        created_at: '2026-01-03T03:04:05Z'
      }]
    }

    const history = presentation.assessmentHistoryView(assessment)

    expect(history.componentKey).toBe('phone')
    expect(history.items).toHaveLength(2)
    expect(history.items[0].current).toBe(false)
    expect(history.items[0].conclusion).toEqual({
      type: 'info',
      label: 'security.assessment.conclusions.not_sensitive'
    })
    expect(history.items[0].sourceLabel).toBe('security.assessment.sources.finding')
    expect(history.items[0].metadata).toContain('"actor":"security.enrollment.userId:{\\"id\\":8}"')
    expect(history.items[1].current).toBe(true)
    expect(history.items[1].conclusion).toEqual({
      type: 'success',
      label: 'security.assessment.conclusions.sensitive'
    })
    expect(history.items[1].sourceLabel).toBe('security.assessment.sources.manual')
    expect(history.items[1].summary).toContain('"type":"Phone"')
    expect(history.items[1].rationale).toBe('confirmed')
  })

  it('derives each temporary authorization through one row model', () => {
    const { presentation } = createPresentation()
    const exemption = {
      id: 'exemption-1',
      assessment_id: 'assessment-1',
      subject_id: 'user-8',
      consumer_owner: 'manager',
      action: 'preview',
      effective_state: 'active',
      current: { expires_at: '2026-01-05T03:04:05Z', rationale: 'approved research' }
    }
    const active = presentation.exemptionRowView(exemption)

    expect(active.exemption).toBe(exemption)
    expect(active.componentKey).toBe('phone')
    expect(active.state).toEqual({ type: 'warning', label: 'security.exemption.states.active' })
    expect(active.subjectId).toBe('user-8')
    expect(active.outlet).toContain('Preview')
    expect(active.expiresAt).not.toBe('security.common.notAvailable')
    expect(active.rationale).toBe('approved research')
    expect(active.canRevoke).toBe(true)

    const expired = presentation.exemptionRowView({ ...exemption, effective_state: 'expired' })
    expect(expired.state).toEqual({ type: 'info', label: 'security.exemption.states.expired' })
    expect(expired.canRevoke).toBe(false)
  })
})
