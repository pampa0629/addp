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
  getPolicy,
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
  const policyScopes = ref([])
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
    policyScopes.value = []
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
    const row = enrollment.value
    const request = ++requestVersions.policies
    if (!unref(canReadPolicies) || !row?.id) {
      policies.value = []
      policyScopes.value = []
      policiesLoading.value = false
      return false
    }
    const enrollmentID = row.id
    policiesLoading.value = true
    try {
      const first = await listPolicies({ enrollment_id: enrollmentID, page: 1, page_size: 100 })
      if (!isCurrent('policies', request, enrollmentID)) return false
      const rows = Array.isArray(first?.data) ? [...first.data] : []
      const scopes = Array.isArray(first?.supported_scopes) ? first.supported_scopes : []
      for (let page = 2; page <= first.total_pages; page += 1) {
        const response = await listPolicies({ enrollment_id: enrollmentID, page, page_size: 100 })
        if (!isCurrent('policies', request, enrollmentID)) return false
        if (response.total !== first.total || JSON.stringify(response.supported_scopes) !== JSON.stringify(scopes)) throw Object.assign(new Error(), { code: 'policy_collection_changed' })
        rows.push(...response.data)
      }
      if (rows.length !== first.total || new Set(rows.map(item => item.id)).size !== rows.length) throw Object.assign(new Error(), { code: 'policy_collection_changed' })
      policies.value = rows
      policyScopes.value = scopes
      return true
    } catch (error) {
      if (isCurrent('policies', request, enrollmentID)) {
        policies.value = []
        policyScopes.value = []
        onLoadError('policies', error)
      }
      return false
    } finally {
      if (isCurrent('policies', request, enrollmentID)) policiesLoading.value = false
    }
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

  async function loadPolicyHistories(assessmentID) {
    if (!unref(canReadPolicies)) return []
    const enrollmentID = enrollment.value?.id
    if (!enrollmentID || !await loadPolicies()) throw new Error('policy_history_unavailable')
    const bound = policies.value.filter(policy => String(policy.assessment_id) === String(assessmentID))
    const histories = await Promise.all(bound.map(async policy => {
      const detail = await getPolicy(policy.id)
      if (String(detail?.id) !== String(policy.id) || String(detail?.assessment_id) !== String(assessmentID)
        || detail.consumer_owner !== policy.consumer_owner || detail.action !== policy.action
        || !Array.isArray(detail.history) || detail.history.length === 0) {
        throw new Error('policy_history_identity_mismatch')
      }
      return detail
    }))
    if (enrollment.value?.id !== enrollmentID) throw new Error('policy_history_resource_changed')
    return histories
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
    policyScopes,
    policiesLoading,
    exemptions,
    exemptionsLoading,
    governanceLoading,
    loadFindings,
    loadAssessments,
    loadPolicies,
    loadPolicyHistories,
    loadExemptions,
    loadGovernance,
    replaceAssessment,
    replacePolicy,
    replaceExemption,
    reset
  }
}
