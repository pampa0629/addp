import { nextTick, ref } from 'vue'
import { discoveryRefreshMarker } from '../utils/protectionEnrollment.mjs'

export function useProtectionEnrollmentDetail({
  enrollment,
  getEnrollment,
  loadGovernance,
  resetGovernance,
  loadDefinitions,
  navigateToResources,
  listPendingFindings,
  onAuthorizationLoadError = () => {},
  onResourceLoadError = () => {},
  onDefinitionsLoadError = () => {}
}) {
  const drawer = ref(false)
  const focusedExemptionID = ref('')
  const drawerRef = ref(null)

  let sessionRequest = 0
  let disposed = false

  function isCurrent(request, enrollmentID) {
    return request === sessionRequest && !disposed && enrollment.value?.id === enrollmentID
  }

  async function load(page = 1, request = sessionRequest) {
    const enrollmentID = enrollment.value?.id
    if (disposed || !drawer.value || !enrollmentID) return false
    const definitions = Promise.resolve()
      .then(() => loadDefinitions())
      .catch(error => {
        if (isCurrent(request, enrollmentID)) onDefinitionsLoadError(error)
        return false
      })
    await Promise.all([loadGovernance(page), definitions])
    return isCurrent(request, enrollmentID)
  }

  async function focusExemption(request = sessionRequest) {
    const enrollmentID = enrollment.value?.id
    if (!focusedExemptionID.value || !enrollmentID) return false
    await nextTick()
    if (!isCurrent(request, enrollmentID)) return false
    await drawerRef.value?.focusExemption?.()
    return isCurrent(request, enrollmentID)
  }

  async function activate(row, exemptionID, request) {
    if (!row?.id || request !== sessionRequest || disposed) return { status: 'stale' }
    enrollment.value = row
    resetGovernance()
    focusedExemptionID.value = exemptionID || ''
    drawer.value = true
    await load(1, request)
    if (!isCurrent(request, row.id)) return { status: 'stale' }
    await focusExemption(request)
    if (!isCurrent(request, row.id)) return { status: 'stale' }
    return { status: 'opened', enrollment: row }
  }

  function open(row, exemptionID = '') {
    if (disposed || !row?.id) return false
    const request = ++sessionRequest
    return activate(row, exemptionID, request)
  }

  async function openAuthorization(accessRequest) {
    const enrollmentID = accessRequest?.enrollment_id
    if (disposed || !enrollmentID) return { status: 'unavailable' }
    const request = ++sessionRequest
    try {
      const row = await getEnrollment(enrollmentID)
      if (request !== sessionRequest || disposed) return { status: 'stale' }
      return await activate(row, accessRequest?.exemption_id, request)
    } catch (error) {
      if (request !== sessionRequest || disposed) return { status: 'stale' }
      onAuthorizationLoadError(error)
      return { status: 'failed', error }
    }
  }

  async function openFinding(finding) {
    const enrollmentID = finding?.enrollment_id
    if (disposed || !enrollmentID) return { status: 'unavailable' }
    const request = ++sessionRequest
    try {
      const row = await getEnrollment(enrollmentID)
      if (request !== sessionRequest || disposed) return { status: 'stale' }
      await navigateToResources()
      if (request !== sessionRequest || disposed) return { status: 'stale' }
      return await activate(row, '', request)
    } catch (error) {
      if (request !== sessionRequest || disposed) return { status: 'stale' }
      onResourceLoadError(error)
      return { status: 'failed', error }
    }
  }

  function requestClose() {
    if (disposed) return false
    sessionRequest += 1
    drawer.value = false
    return true
  }

  function clearSession() {
    drawer.value = false
    focusedExemptionID.value = ''
    enrollment.value = null
    resetGovernance()
  }

  function closed() {
    if (disposed) return false
    sessionRequest += 1
    clearSession()
    return true
  }

  function replace(latest) {
    if (!latest?.id || enrollment.value?.id !== latest.id) return false
    enrollment.value = latest
    return true
  }

  async function syncLoadedRows({ rows, options }) {
    const current = enrollment.value
    if (disposed || !drawer.value || !current?.id) return false
    const request = sessionRequest
    const enrollmentID = current.id
    const previousMarker = discoveryRefreshMarker(current)
    const visible = Array.isArray(rows) ? rows.find(row => row.id === enrollmentID) : null
    const latest = visible || await getEnrollment(enrollmentID)
    if (!isCurrent(request, enrollmentID)) return false
    enrollment.value = latest
    if (options?.syncFindings && discoveryRefreshMarker(latest) !== previousMarker) {
      return load(1, request)
    }
    return true
  }

  async function nextPendingFinding() {
    const row = enrollment.value
    if (disposed || !drawer.value || !row?.id || !row.latest_source_snapshot_hash || !row.latest_discovery_execution_id) return null
    const request = sessionRequest
    const response = await listPendingFindings({
      enrollment_id: row.id,
      source_snapshot_hash: row.latest_source_snapshot_hash,
      discovery_execution_id: row.latest_discovery_execution_id,
      review_state: 'pending',
      page: 1,
      page_size: 1
    })
    if (!isCurrent(request, row.id)) return null
    if (enrollment.value?.latest_source_snapshot_hash !== row.latest_source_snapshot_hash || enrollment.value?.latest_discovery_execution_id !== row.latest_discovery_execution_id) return null
    return Array.isArray(response?.data) ? response.data[0] || null : null
  }

  function dispose() {
    if (disposed) return
    sessionRequest += 1
    clearSession()
    disposed = true
  }

  return {
    enrollment,
    drawer,
    focusedExemptionID,
    drawerRef,
    load,
    open,
    openAuthorization,
    openFinding,
    requestClose,
    closed,
    replace,
    syncLoadedRows,
    nextPendingFinding,
    dispose
  }
}
