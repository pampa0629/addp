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
    policyScopes: ref([{ consumer_owner: 'manager', action: 'preview' }, { consumer_owner: 'develop', action: 'query' }, { consumer_owner: 'service', action: 'service_execute' }, { consumer_owner: 'transfer', action: 'export' }]),
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

describe('projection time validity presentation', () => {
  it('shows expiry independently from an acknowledged active projection and keeps rediscovery available', () => {
    const { presentation } = createPresentation()
    const enrollment = { state: 'active', owner_progress: [{ consumer_owner: 'manager', projection_state: 'active', acknowledged: true, validity: 'expired', valid_from: '2026-01-01T00:00:00Z', expires_at: '2027-01-01T00:00:00Z' }] }
    expect(presentation.presentationState(enrollment).type).toBe('danger')
    expect(presentation.ownerPresentation(enrollment, enrollment.owner_progress[0]).type).toBe('success')
    const detail = presentation.resourceDetailView(enrollment, { canUpdate: true })
    expect(detail.actions.canRediscover).toBe(true)
    expect(detail.owners[0].expiresAt).not.toBe('security.common.notAvailable')
    expect(detail.owners[0].validity.label).toBe('security.enrollment.validity.expired')
    enrollment.state = 'released'
    expect(presentation.presentationState(enrollment).type).toBe('info')
    expect(presentation.resourceDetailView(enrollment, { canUpdate: true }).actions.canRediscover).toBe(false)
  })
})

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

    expect(presentation.resourceIdentityView(resource)).toEqual({
      resource,
      name: 'customers',
      path: 'business.customers',
      itemType: 'Table',
      engine: 'Business MySQL'
    })
  })

  it('derives the selected enrollment resource through one creation presentation model', () => {
    const { presentation } = createPresentation()
    const item = {
      name: 'customers',
      full_name: 'business.customers',
      item_type: 'table',
      engine_id: 7,
      scanned_at: '2026-01-02T03:04:05Z'
    }

    const selection = presentation.enrollmentCreationSelectionView(item, { engineName: 'Selected Business Engine' })
    expect(selection).toEqual({
      name: 'customers',
      fullName: 'business.customers',
      itemType: 'Table',
      engine: 'Selected Business Engine',
      lastScannedAt: expect.not.stringContaining('security.common.notAvailable'),
      scope: 'security.enrollment.wholeResourceScope'
    })

    const fallback = presentation.enrollmentCreationSelectionView({ ...item, item_type: 'custom', engine_id: 99 }, {})
    expect(fallback.itemType).toBe('custom')
    expect(fallback.engine).toContain('"id":99')
  })

  it('derives lifecycle action warnings and resource details through one dialog model', () => {
    const { presentation } = createPresentation()
    const enrollment = {
      target_snapshot: { full_name: 'business.customers', item_type: 'table' },
      release_basis: 'manual',
      released_at: '2026-01-04T03:04:05Z',
      release_reason: 'retired',
      last_discovered_at: '2026-01-02T03:04:05Z'
    }

    const reEnrollment = presentation.enrollmentLifecycleActionDialogView(enrollment, 're-enroll')
    expect(reEnrollment.alert.type).toBe('warning')
    expect(reEnrollment.alert.title).toContain('"resource":"customers"')
    expect(reEnrollment.targetName).toBe('business.customers')
    expect(reEnrollment.details.map(detail => detail.key)).toEqual(['basis', 'releasedAt', 'reason'])
    expect(reEnrollment.details[0].value).toBe('security.enrollment.releaseBases.manual')
    expect(reEnrollment.details[2].value).toBe('retired')

    const rediscovery = presentation.enrollmentLifecycleActionDialogView(enrollment, 'rediscover')
    expect(rediscovery.alert).toEqual({ type: 'info', title: 'security.enrollment.rediscoverHint' })
    expect(rediscovery.details.map(detail => detail.key)).toEqual(['lastDiscoveredAt'])
    expect(rediscovery.details[0].value).not.toBe('security.common.notAvailable')
  })

  it('derives enrollment release content through one dialog presentation model', () => {
    const { presentation } = createPresentation()
    const enrollment = { target_snapshot: { full_name: 'business.customers' } }

    const manual = presentation.enrollmentReleaseDialogView(enrollment, 'manual')
    expect(manual).toEqual({
      title: 'security.enrollment.release',
      warning: 'security.enrollment.releaseWarning',
      targetName: 'business.customers',
      basisLabel: 'security.enrollment.releaseBases.manual',
      reasonPlaceholder: 'security.enrollment.releaseReason',
      confirmLabel: 'security.enrollment.confirmRelease'
    })

    const noSupportedFindings = presentation.enrollmentReleaseDialogView(enrollment, 'no_supported_findings')
    expect(noSupportedFindings.title).toBe('security.enrollment.confirmNoProtectionNeeded')
    expect(noSupportedFindings.warning).toBe('security.enrollment.noFindingsReleaseWarning')
    expect(noSupportedFindings.basisLabel).toBe('security.enrollment.releaseBases.no_supported_findings')
    expect(noSupportedFindings.reasonPlaceholder).toBe('security.enrollment.noFindingsReleaseReason')
    expect(noSupportedFindings.confirmLabel).toBe('security.enrollment.confirmNoProtectionNeededAction')

    expect(presentation.enrollmentReleaseDialogView(null, 'invalid').targetName).toBe('security.common.notAvailable')
    expect(presentation.enrollmentReleaseDialogView(null, 'invalid').basisLabel).toBe('security.common.notAvailable')
  })

  it('derives manual assessment choices through one dialog presentation model', () => {
    const { presentation } = createPresentation()
    const componentOptions = [{ component: { key: 'email', value_type: 'string' } }]

    const dialog = presentation.manualAssessmentDialogView({
      componentsLoading: true,
      componentOptions,
      sensitiveDataTypeID: '11'
    })

    expect(dialog).toEqual({
      componentsLoading: true,
      componentOptions: [{ value: 'email', label: 'email', valueType: 'string' }],
      sensitiveTypeOptions: [{ value: '11', label: 'Phone' }],
      gradeOptions: [{ value: '13', label: 'High' }]
    })
    expect(presentation.manualAssessmentDialogView().gradeOptions).toEqual([])
  })

  it('derives access request decision content through one dialog presentation model', () => {
    const { presentation } = createPresentation()
    const request = {
      target_full_name: 'business.customers',
      component: { key: 'phone' },
      requester: { id: 8, display_name: 'Alice' },
      requested_expires_at: '2026-01-04T03:04:05Z',
      rationale: 'support case'
    }

    const rejection = presentation.accessRequestDecisionDialogView(request, 'reject')
    expect(rejection.title).toBe('security.accessRequest.reject')
    expect(rejection.hint).toBe('security.accessRequest.rejectHint')
    expect(rejection.confirmLabel).toBe('security.accessRequest.confirmActions.reject')
    expect(rejection.confirmType).toBe('danger')
    expect(rejection.target).toBe('business.customers · phone')
    expect(rejection.details.map(detail => detail.key)).toEqual(['scope', 'requester', 'requestedUntil', 'rationale'])
    expect(rejection.details[1].value).toContain('Alice')
    expect(rejection.details[3].value).toBe('support case')

    const approval = presentation.accessRequestDecisionDialogView(request, 'approve')
    expect(approval.confirmType).toBe('primary')
    expect(approval.confirmLabel).toBe('security.accessRequest.confirmActions.approve')
  })

  it('derives pending and historical access requests through one row presentation model', () => {
    const { presentation } = createPresentation()
    const request = {
      id: 21,
      target_full_name: 'business.customers',
      component: { key: 'phone' },
      requester: { id: 8, display_name: 'Alice' },
      requested_expires_at: '2026-01-04T03:04:05Z',
      created_at: '2026-01-02T03:04:05Z',
      rationale: 'support case',
      state: 'approved',
      scopes: [{consumer_owner: 'manager', action: 'preview'}],
      authorizations: [{consumer_owner: 'manager', action: 'preview', authorization_state: 'active', authorized_until: '2026-01-04T03:04:05Z', exemption_id: 'exemption-1'}],
      enrollment_id: 'enrollment-1',
      reviewer: { id: 9, display_name: 'Bob' },
      decided_at: '2026-01-03T03:04:05Z',
      decision_rationale: 'approved',
      can_decide: false,
      decision_unavailable_reason: 'self_approval_forbidden'
    }

    const row = presentation.accessRequestReviewRowView(request, { canReadExemptions: true })
    expect(row.request).toBe(request)
    expect(row.target).toEqual({ fullName: 'business.customers', componentKey: 'phone' })
    expect(row.requester.displayName).toBe('Alice')
    expect(row.requester.idLabel).toContain('"id":8')
    expect(row.state).toEqual({ type: 'success', label: 'security.accessRequest.states.approved' })
    expect(row.authorizations[0]?.state.type).toBe('success')
    expect(row.authorizations[0]?.canOpen).toBe(true)
    expect(row.reviewer?.displayName).toBe('Bob')
    expect(row.decisionRationale).toBe('approved')
    expect(row.actions.canDecide).toBe(false)
    expect(row.actions.unavailable?.label).toBe('security.accessRequest.unavailableLabels.self_approval_forbidden')

    const workspace = presentation.accessRequestReviewWorkspaceView([request], { canReadExemptions: true })
    expect(workspace.rows).toHaveLength(1)
    expect(workspace.rows[0].request).toBe(request)
    expect(workspace.rows[0].authorizations[0]?.canOpen).toBe(true)

    const pending = presentation.accessRequestReviewRowView({ ...request, state: 'pending', authorizations: [], reviewer: null, can_decide: true })
    expect(pending.state.type).toBe('info')
    expect(pending.authorizations).toEqual([])
    expect(pending.reviewer).toBeNull()
    expect(pending.actions.canDecide).toBe(true)
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
      state: { type: 'info', label: 'security.enrollment.ownerStates.released' },
      expiresAt: 'security.common.notAvailable',
      validity: { type: 'info', label: 'security.common.notAvailable' }
    }])
    expect(row.discovery.type).toBe('warning')
    expect(row.discovery.observedAt).not.toBe('security.common.notAvailable')
    expect(row.releasedAt).not.toBe('security.common.notAvailable')
    expect(row.canReEnroll).toBe(true)

    const list = presentation.resourceListView([enrollment])
    expect(list.rows).toHaveLength(1)
    expect(list.rows[0].enrollment).toBe(enrollment)
    expect(list.rows[0].canReEnroll).toBe(true)
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

    const active = presentation.governanceDetailView({
      state: 'active',
      discovery_summary: { status: 'completed', finding_count: 2, pending_review_count: 2 }
    }, {
      loading: true,
      canReadFindings: true,
      canCreateAssessments: true
    })
    expect(active.visible).toBe(true)
    expect(active.loading).toBe(true)
    expect(active.section).toEqual({
      pendingReview: {
        count: 2,
        label: 'security.finding.pendingCount:{"count":2}'
      },
      canDesignate: true
    })
    expect(active.empty).toBe(true)

    const released = presentation.governanceDetailView({
      state: 'released',
      discovery_summary: { status: 'completed', finding_count: 0, pending_review_count: 0 }
    }, { canCreateAssessments: true })
    expect(released.section).toEqual({
      pendingReview: null,
      canDesignate: false
    })
    expect(released.visible).toBe(false)
    expect(released.loading).toBe(false)
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

    const list = presentation.governanceDetailView({}, {
      findings: [finding],
      findingsPage: 2,
      findingsTotal: 11,
      findingsPageSize: 10,
      canReadFindings: true,
      canReviewFindings: true,
      canUpdateAssessments: true,
      canRevokePolicies: false
    }).findings
    expect(list.rows).toHaveLength(1)
    expect(list.rows[0].finding).toBe(finding)
    expect(list.visible).toBe(true)
    expect(list.pagination).toEqual({ page: 2, total: 11, pageSize: 10 })
    expect(list.permissions).toEqual({
      canReview: true,
      canUpdateAssessments: true,
      canRevokePolicies: false
    })
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

    const queue = presentation.findingReviewQueueView([finding], {
      canReview: true,
      detectorCapabilities: [{ key: 'phone-metadata/v1', name_i18n_key: 'security.detectorCapabilities.phoneMetadata.name' }]
    })
    expect(queue.rows).toHaveLength(1)
    expect(queue.rows[0].actions.canReview).toBe(true)
    expect(queue.filters.sensitiveTypeOptions).toEqual([{ value: '11', label: 'Phone' }])
    expect(queue.filters.recognitionMethodOptions).toEqual([{
      value: 'phone-metadata/v1',
      label: 'Phone metadata（phone-metadata/v1）'
    }])
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
    expect(dialog.sensitiveTypeOptions).toEqual([{ value: '11', label: 'Phone' }])
    expect(dialog.gradeOptions).toEqual([{ value: '13', label: 'High' }])
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
    expect(presentation.policyForAssessment(assessment, { consumer_owner: 'manager', action: 'preview' })?.id).toBe('policy-1')
    expect(presentation.stricterPolicyEffects(assessment)).toEqual(['mask', 'suppress', 'deny'])
    expect(presentation.activeGradesForType(11).map(item => item.id)).toEqual([13])
    expect(presentation.canConfigurePolicy(assessment)).toBe(true)

    dependencies.canUpdatePolicies.value = false
    expect(presentation.canConfigurePolicy(assessment, { consumer_owner: 'manager', action: 'preview' })).toBe(false)
    expect(presentation.canConfigurePolicy(assessment)).toBe(true)
    dependencies.canCreatePolicies.value = false
    expect(presentation.canConfigurePolicy(assessment)).toBe(false)
  })

  it('derives policy target summaries and stricter effects through one dialog presentation model', () => {
    const { dependencies, presentation } = createPresentation()
    const assessment = dependencies.assessments.value[0]

    const dialog = presentation.protectionPolicyDialogView(assessment, { effects: ['suppress', 'deny'] })

    expect(dialog.componentKey).toBe('phone')
    expect(dialog.assessmentSummary).toContain('"type":"Phone"')
    expect(dialog.protectionSummary).toContain('security.policy.activeSummary')
    expect(dialog.effectOptions).toEqual([
      {
        value: 'suppress',
        label: 'security.enrollment.effects.suppress',
        impact: 'security.baseline.effectImpact.suppress'
      },
      {
        value: 'deny',
        label: 'security.enrollment.effects.deny',
        impact: 'security.baseline.effectImpact.deny'
      }
    ])
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
  })

  it('derives assessment change dialog content and grade choices through one presentation model', () => {
    const { dependencies, presentation } = createPresentation()
    const gradeOptions = [{ id: 13, name: 'High' }]

    const dialog = presentation.assessmentChangeDialogView(dependencies.assessments.value[0], { gradeOptions })
    expect(dialog.componentKey).toBe('phone')
    expect(dialog.conclusionLabel).toBe('security.assessment.conclusions.sensitive')
    expect(dialog.summary).toContain('"type":"Phone"')
    expect(dialog.sensitiveTypeOptions).toEqual([{ value: '11', label: 'Phone' }])
    expect(dialog.gradeOptions).toEqual([{ value: '13', label: 'High' }])

    const fallback = presentation.assessmentChangeDialogView(dependencies.assessments.value[1])
    expect(fallback.conclusionLabel).toBe('security.assessment.conclusions.not_sensitive')
    expect(fallback.sensitiveTypeOptions).toEqual([{ value: '11', label: 'Phone' }])
    expect(fallback.gradeOptions).toEqual([])
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

    const list = presentation.governanceDetailView({}, {
      manualAssessments: dependencies.assessments.value,
      canUpdateAssessments: true,
      canRevokePolicies: false
    }).assessments
    expect(list.rows).toHaveLength(2)
    expect(list.rows[0].assessment).toBe(dependencies.assessments.value[0])
    expect(list.visible).toBe(true)
    expect(list.permissions).toEqual({ canUpdate: true, canRevokePolicies: false })

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

  it('includes immutable outlet policy revisions and distinguishes revoked policies from effective protection', () => {
    const { presentation } = createPresentation()
    const scopes = [['manager', 'preview'], ['develop', 'query'], ['service', 'service_execute'], ['transfer', 'export']]
    const policies = scopes.map(([consumer_owner, action], index) => ({
      id: `policy-${index}`, consumer_owner, action, current_revision: 2,
      history: [
        { id: `p${index}-r1`, revision: 1, state: 'active', effect: 'mask', algorithm: 'addp.mask.keep_prefix_suffix/v2', parameters: { prefix_runes: 1, suffix_runes: 2, mask_rune: '#' }, invalid_value_effect: 'suppress', rationale: 'tightened', created_by: '8', created_at: '2026-01-03T03:04:05Z' },
        { id: `p${index}-r2`, revision: 2, state: 'revoked', effect: 'mask', algorithm: 'addp.mask.sm3/v1', parameters: {}, invalid_value_effect: 'deny', rationale: 'restore baseline', created_by: '9', created_at: '2026-01-04T03:04:05Z' }
      ]
    }))
    const view = presentation.assessmentHistoryView({ component_key: 'phone', history: [] }, policies)
    expect(view.items).toHaveLength(8)
    expect(new Set(view.items.map(item => item.id)).size).toBe(8)
    for (const [index, [owner, action]] of scopes.entries()) {
      const active = view.items.find(item => item.id === `p${index}-r1`)
      const revoked = view.items.find(item => item.id === `p${index}-r2`)
      expect(active.sourceLabel).toContain(`security.enrollment.owners.${owner}`)
      expect(active.sourceLabel).toContain(action === 'preview' ? 'Preview' : action)
      expect(active.summary).toContain('maskSummary')
      expect(active.summary).toContain('security.algorithms.prefix.name')
      expect(active.current).toBe(false)
      expect(revoked.current).toBe(true)
      expect(revoked.conclusion).toEqual({ type: 'info', label: 'security.policy.historyStates.revoked' })
      expect(revoked.summary).toContain('security.algorithms.sm3.name')
      expect(revoked.rationale).toBe('restore baseline')
      expect(revoked.metadata).toContain('userId')
    }
  })

  it('derives each temporary authorization through one row model', () => {
    const { presentation } = createPresentation()
    const exemption = {
      id: 'exemption-1',
      assessment_id: 'assessment-1',
      component_key: 'phone',
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

  it('derives the temporary authorization section through one presentation contract', () => {
    const { presentation } = createPresentation()
    const exemptions = [
      {
        id: 'exemption-1',
        assessment_id: 'assessment-1',
        component_key: 'phone',
        subject_id: 'user-8',
        consumer_owner: 'manager',
        action: 'preview',
        effective_state: 'active',
        current: { expires_at: '2026-01-05T03:04:05Z', rationale: 'approved research' }
      },
      {
        id: 'exemption-2',
        assessment_id: 'assessment-1',
        effective_state: 'expired',
        current: {}
      }
    ]

    const section = presentation.exemptionSectionView(exemptions, {
      canRead: true,
      loading: true,
      focusedId: 'exemption-2',
      canRevoke: true
    })

    expect(section.visible).toBe(true)
    expect(section.loading).toBe(true)
    expect(section.canRevoke).toBe(true)
    expect(section.rows).toHaveLength(2)
    expect(section.rows[0].focused).toBe(false)
    expect(section.rows[1].focused).toBe(true)
    expect(presentation.exemptionSectionView(null)).toEqual({
      visible: false,
      loading: false,
      rows: [],
      canRevoke: false
    })
  })

  it('identifies a grant field without permission to load the assessment list', () => {
    const { presentation } = createPresentation({ assessments: ref([]) })
    const grant = { id: 'grant-1', assessment_id: 'assessment-1', component_key: 'userInfo.phone', effective_state: 'active' }
    expect(presentation.exemptionRowView(grant).componentKey).toBe('userInfo.phone')
  })
})


it('does not expose structured-field Policy editing for document body components', () => {
  const { dependencies, presentation } = createPresentation()
  const assessment = { ...dependencies.assessments.value[0], component_key: '$document.text' }
  expect(presentation.canConfigurePolicy(assessment)).toBe(false)
})
