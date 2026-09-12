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
    listPolicies: vi.fn().mockResolvedValue({ data: [{ id: 3 }] }),
    listExemptions: vi.fn().mockResolvedValue({ data: [{ id: 4 }] }),
    onLoadError: vi.fn(),
    ...overrides
  }
  return { enrollment, dependencies, governance: useProtectionEnrollmentGovernance(dependencies) }
}

describe('protected resource governance detail', () => {
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
