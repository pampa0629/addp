import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { useProtectionEnrollmentGovernance } from '../src/composables/useProtectionEnrollmentGovernance.mjs'

function createGovernance(overrides = {}) {
  const enrollment = overrides.enrollment || ref({
    id: 7,
    latest_source_snapshot_hash: 'snapshot-a',
    latest_discovery_execution_id: 11,
    discovery_summary: { status: 'completed', finding_count: 2 }
  })
  const dependencies = {
    enrollment,
    canReadFindings: true,
    canReadAssessments: true,
    canReadPolicies: true,
    canReadExemptions: true,
    listFindings: vi.fn().mockResolvedValue({ data: [{ id: 1 }], total: 2 }),
    listAssessments: vi.fn().mockResolvedValue({ data: [{ id: 2 }] }),
    listPolicies: vi.fn().mockResolvedValue({ data: [{ id: 3 }], total: 1, total_pages: 1, supported_scopes: [{ consumer_owner: 'manager', action: 'preview' }] }),
    getPolicy: vi.fn(),
    listExemptions: vi.fn().mockResolvedValue({ data: [{ id: 4 }] }),
    onLoadError: vi.fn(),
    ...overrides
  }
  return { enrollment, dependencies, governance: useProtectionEnrollmentGovernance(dependencies) }
}

describe('protected resource governance detail', () => {
  it('loads histories only for the selected assessment, including revoked outlet policies', async () => {
    const rows = [
      { id: 'manager-policy', assessment_id: 'a', consumer_owner: 'manager', action: 'preview', state: 'active' },
      { id: 'develop-policy', assessment_id: 'a', consumer_owner: 'develop', action: 'query', state: 'revoked' },
      { id: 'other-policy', assessment_id: 'b' }
    ]
    const { governance, dependencies } = createGovernance({
      listPolicies: vi.fn().mockResolvedValue({ data: rows, total: 3, total_pages: 1, supported_scopes: [] }),
      getPolicy: vi.fn(id => Promise.resolve({ ...rows.find(row => row.id === id), history: [{ revision: 1 }] }))
    })
    const histories = await governance.loadPolicyHistories('a')
    expect(histories.map(row => row.id)).toEqual(['manager-policy', 'develop-policy'])
    expect(dependencies.getPolicy.mock.calls).toEqual([['manager-policy'], ['develop-policy']])
  })

  it('does not request policy history without policy read permission', async () => {
    const { governance, dependencies } = createGovernance({ canReadPolicies: false })
    await expect(governance.loadPolicyHistories('a')).resolves.toEqual([])
    expect(dependencies.listPolicies).not.toHaveBeenCalled()
    expect(dependencies.getPolicy).not.toHaveBeenCalled()
  })

  it.each(['identity', 'assessment', 'outlet', 'missing-history'])('rejects inconsistent %s history instead of showing a false empty result', async mismatch => {
    const row = { id: 'p', assessment_id: 'a', consumer_owner: 'manager', action: 'preview' }
    const detail = { ...row, history: [{ revision: 1 }] }
    if (mismatch === 'identity') detail.id = 'other'
    if (mismatch === 'assessment') detail.assessment_id = 'other'
    if (mismatch === 'outlet') detail.action = 'export'
    if (mismatch === 'missing-history') delete detail.history
    const { governance } = createGovernance({
      listPolicies: vi.fn().mockResolvedValue({ data: [row], total: 1, total_pages: 1, supported_scopes: [] }),
      getPolicy: vi.fn().mockResolvedValue(detail)
    })
    await expect(governance.loadPolicyHistories('a')).rejects.toThrow('policy_history_identity_mismatch')
  })

  it('rejects policy history when switching resources while details load', async () => {
    let complete
    const row = { id: 'p', assessment_id: 'a' }
    const { governance, enrollment, dependencies } = createGovernance({
      listPolicies: vi.fn().mockResolvedValue({ data: [row], total: 1, total_pages: 1, supported_scopes: [] }),
      getPolicy: vi.fn(() => new Promise(resolve => { complete = resolve }))
    })
    const pending = governance.loadPolicyHistories('a')
    await vi.waitFor(() => expect(dependencies.getPolicy).toHaveBeenCalled())
    enrollment.value = { id: 8 }
    governance.reset()
    complete({ ...row, history: [{ revision: 1 }] })
    await expect(pending).rejects.toThrow('policy_history_resource_changed')
    expect(governance.policies.value).toEqual([])
  })

  it('rejects a failed or incomplete policy list before requesting details', async () => {
    const { governance, dependencies } = createGovernance({
      listPolicies: vi.fn().mockResolvedValue({ data: [], total: 1, total_pages: 1, supported_scopes: [] })
    })
    await expect(governance.loadPolicyHistories('a')).rejects.toThrow('policy_history_unavailable')
    expect(dependencies.getPolicy).not.toHaveBeenCalled()
  })

  it('loads the four read models with enrollment-scoped canonical parameters', async () => {
    const { dependencies, governance } = createGovernance()

    await governance.loadGovernance(3)

    expect(dependencies.listFindings).toHaveBeenCalledWith({
      enrollment_id: 7,
      source_snapshot_hash: 'snapshot-a',
      discovery_execution_id: 11,
      page: 3,
      page_size: 20
    })
    expect(dependencies.listAssessments).toHaveBeenCalledWith({ enrollment_id: 7, page: 1, page_size: 100 })
    expect(dependencies.listPolicies).toHaveBeenCalledWith({ enrollment_id: 7, page: 1, page_size: 100 })
    expect(dependencies.listExemptions).toHaveBeenCalledWith({ enrollment_id: 7, page: 1, page_size: 100 })
    expect(governance.findings.value).toEqual([{ id: 1 }])
    expect(governance.findingsTotal.value).toBe(2)
    expect(governance.assessments.value).toEqual([{ id: 2 }])
    expect(governance.policies.value).toEqual([{ id: 3 }])
    expect(governance.exemptions.value).toEqual([{ id: 4 }])
    expect(governance.governanceLoading.value).toBe(false)
  })

  it('does not query unavailable read models and clears their previous state', async () => {
    const { dependencies, governance } = createGovernance({
      canReadFindings: false,
      canReadAssessments: false,
      canReadPolicies: false,
      canReadExemptions: false
    })
    governance.findings.value = [{ id: 1 }]
    governance.assessments.value = [{ id: 2 }]
    governance.policies.value = [{ id: 3 }]
    governance.exemptions.value = [{ id: 4 }]

    await governance.loadGovernance()

    expect(dependencies.listFindings).not.toHaveBeenCalled()
    expect(dependencies.listAssessments).not.toHaveBeenCalled()
    expect(dependencies.listPolicies).not.toHaveBeenCalled()
    expect(dependencies.listExemptions).not.toHaveBeenCalled()
    expect(governance.findings.value).toEqual([])
    expect(governance.assessments.value).toEqual([])
    expect(governance.policies.value).toEqual([])
    expect(governance.exemptions.value).toEqual([])
  })

  it('rejects a slower assessment response from the previously opened resource', async () => {
    const enrollment = ref({ id: 7 })
    const pending = []
    const listAssessments = vi.fn(() => new Promise(resolve => pending.push(resolve)))
    const { governance } = createGovernance({ enrollment, listAssessments })

    const firstLoad = governance.loadAssessments()
    enrollment.value = { id: 8 }
    governance.reset()
    const secondLoad = governance.loadAssessments()
    pending[1]({ data: [{ id: 82 }] })
    await secondLoad
    pending[0]({ data: [{ id: 71 }] })
    await firstLoad

    expect(governance.assessments.value).toEqual([{ id: 82 }])
    expect(governance.assessmentsLoading.value).toBe(false)
  })

  it('rejects findings when the current discovery snapshot changes in flight', async () => {
    const enrollment = ref({
      id: 7,
      latest_source_snapshot_hash: 'snapshot-a',
      latest_discovery_execution_id: 11,
      discovery_summary: { status: 'completed', finding_count: 1 }
    })
    let resolveFindings
    const listFindings = vi.fn(() => new Promise(resolve => { resolveFindings = resolve }))
    const { governance } = createGovernance({ enrollment, listFindings })

    const load = governance.loadFindings()
    enrollment.value = { ...enrollment.value, latest_source_snapshot_hash: 'snapshot-b', latest_discovery_execution_id: 12 }
    resolveFindings({ data: [{ id: 1 }], total: 1 })

    await expect(load).resolves.toBe(false)
    expect(governance.findings.value).toEqual([])
    expect(governance.findingsLoading.value).toBe(false)
  })

  it('provides collection-owned replacement and reset operations for page workflows', () => {
    const { governance } = createGovernance()
    governance.assessments.value = [{ id: 1, version: 1 }]
    governance.policies.value = [{ id: 2, version: 1 }]
    governance.exemptions.value = [{ id: 3, version: 1 }]

    governance.replaceAssessment({ id: 1, version: 2 })
    governance.replacePolicy({ id: 2, version: 2 })
    governance.replaceExemption({ id: 3, version: 2 })

    expect(governance.assessments.value[0].version).toBe(2)
    expect(governance.policies.value[0].version).toBe(2)
    expect(governance.exemptions.value[0].version).toBe(2)
    governance.reset()
    expect(governance.assessments.value).toEqual([])
    expect(governance.policies.value).toEqual([])
    expect(governance.exemptions.value).toEqual([])
  })
})


it('loads every policy page before publishing the supported outlets and refuses incomplete collections', async () => {
  const scopes = [{ consumer_owner: 'develop', action: 'query' }]
  const listPolicies = vi.fn().mockResolvedValueOnce({ data: [{ id: 'a' }], total: 2, total_pages: 2, supported_scopes: scopes }).mockResolvedValueOnce({ data: [{ id: 'b' }], total: 2, total_pages: 2, supported_scopes: scopes })
  const { governance } = createGovernance({ listPolicies })
  expect(await governance.loadPolicies()).toBe(true)
  expect(governance.policies.value.map(item => item.id)).toEqual(['a', 'b'])
  expect(governance.policyScopes.value).toEqual(scopes)
  listPolicies.mockResolvedValueOnce({ data: [{ id: 'a' }], total: 2, total_pages: 1, supported_scopes: scopes })
  expect(await governance.loadPolicies()).toBe(false)
  expect(governance.policyScopes.value).toEqual([])
  expect(governance.policies.value).toEqual([])
})
