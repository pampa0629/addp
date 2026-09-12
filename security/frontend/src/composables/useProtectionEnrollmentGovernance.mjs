import { computed, ref, unref } from 'vue'
import { normalizeDiscoverySummary } from '../utils/protectionEnrollment.mjs'

const GOVERNANCE_KINDS = ['findings', 'assessments', 'policies', 'exemptions']

export function useProtectionEnrollmentGovernance({
  enrollment,
  canReadFindings,
  canReadAssessments,
  canReadPolicies,
  canReadExemptions,
  listFindings,
  listAssessments,
  listPolicies,
  listExemptions,
  onLoadError = () => {}
}) {
  const findings = ref([])
  const findingsTotal = ref(0)
  const findingsPage = ref(1)
  const findingsPageSize = 20
  const findingsLoading = ref(false)
  const assessments = ref([])
  const assessmentsLoading = ref(false)
  const policies = ref([])
  const policiesLoading = ref(false)
  const exemptions = ref([])
  const exemptionsLoading = ref(false)
  const requestVersions = Object.fromEntries(GOVERNANCE_KINDS.map(kind => [kind, 0]))

  const governanceLoading = computed(() => findingsLoading.value || assessmentsLoading.value || policiesLoading.value)

  function invalidate(kind) {
    requestVersions[kind] += 1
  }

  function isCurrent(kind, request, enrollmentID) {
    return request === requestVersions[kind] && enrollment.value?.id === enrollmentID
  }

  function replaceItem(collection, latest) {
    const index = collection.value.findIndex(item => item.id === latest?.id)
    if (index >= 0) collection.value.splice(index, 1, latest)
  }

  function replaceAssessment(latest) {
    replaceItem(assessments, latest)
  }

  function replacePolicy(latest) {
    replaceItem(policies, latest)
  }

  function replaceExemption(latest) {
    replaceItem(exemptions, latest)
  }

  function reset() {
    for (const kind of GOVERNANCE_KINDS) invalidate(kind)
    findings.value = []
    findingsTotal.value = 0
    findingsPage.value = 1
    assessments.value = []
    policies.value = []
    exemptions.value = []
    findingsLoading.value = false
    assessmentsLoading.value = false
    policiesLoading.value = false
    exemptionsLoading.value = false
  }

  async function loadFindings(page = findingsPage.value) {
    const row = enrollment.value
    const request = ++requestVersions.findings
    findingsPage.value = Number(page) || 1
    if (!unref(canReadFindings) || !row?.id || !row.latest_source_snapshot_hash || normalizeDiscoverySummary(row).findingCount === 0) {
      findings.value = []
      findingsTotal.value = 0
      findingsLoading.value = false
      return false
    }

    const enrollmentID = row.id
    const sourceSnapshotHash = row.latest_source_snapshot_hash
    const discoveryExecutionID = row.latest_discovery_execution_id
    findingsLoading.value = true
    try {
      const response = await listFindings({
        enrollment_id: enrollmentID,
        source_snapshot_hash: sourceSnapshotHash,
        discovery_execution_id: discoveryExecutionID,
        page: findingsPage.value,
        page_size: findingsPageSize
      })
      if (!isCurrent('findings', request, enrollmentID)) return false
      if (enrollment.value?.latest_source_snapshot_hash !== sourceSnapshotHash || enrollment.value?.latest_discovery_execution_id !== discoveryExecutionID) return false
      findings.value = Array.isArray(response?.data) ? response.data : []
      findingsTotal.value = Number(response?.total || 0)
      return true
    } catch (error) {
      if (isCurrent('findings', request, enrollmentID)) onLoadError('findings', error)
      return false
    } finally {
      if (isCurrent('findings', request, enrollmentID)) findingsLoading.value = false
    }
  }

  async function loadAssessments() {
    return loadCollection({
      kind: 'assessments',
      allowed: canReadAssessments,
      collection: assessments,
      loading: assessmentsLoading,
      fetch: listAssessments
    })
  }

  async function loadPolicies() {
    return loadCollection({
      kind: 'policies',
      allowed: canReadPolicies,
      collection: policies,
      loading: policiesLoading,
      fetch: listPolicies
    })
  }

  async function loadExemptions() {
    return loadCollection({
      kind: 'exemptions',
      allowed: canReadExemptions,
      collection: exemptions,
      loading: exemptionsLoading,
      fetch: listExemptions
    })
  }

  async function loadCollection({ kind, allowed, collection, loading, fetch }) {
    const row = enrollment.value
    const request = ++requestVersions[kind]
    if (!unref(allowed) || !row?.id) {
      collection.value = []
      loading.value = false
      return false
    }

    const enrollmentID = row.id
    loading.value = true
    try {
      const response = await fetch({ enrollment_id: enrollmentID, page: 1, page_size: 100 })
      if (!isCurrent(kind, request, enrollmentID)) return false
      collection.value = Array.isArray(response?.data) ? response.data : []
      return true
    } catch (error) {
      if (isCurrent(kind, request, enrollmentID)) onLoadError(kind, error)
      return false
    } finally {
      if (isCurrent(kind, request, enrollmentID)) loading.value = false
    }
  }

  async function loadGovernance(page = findingsPage.value) {
    await Promise.all([
      loadFindings(page),
      loadAssessments(),
      loadPolicies(),
      loadExemptions()
    ])
  }

  return {
    findings,
    findingsTotal,
    findingsPage,
    findingsPageSize,
    findingsLoading,
    assessments,
    assessmentsLoading,
    policies,
    policiesLoading,
    exemptions,
    exemptionsLoading,
    governanceLoading,
    loadFindings,
    loadAssessments,
    loadPolicies,
    loadExemptions,
    loadGovernance,
    replaceAssessment,
    replacePolicy,
    replaceExemption,
    reset
  }
}
