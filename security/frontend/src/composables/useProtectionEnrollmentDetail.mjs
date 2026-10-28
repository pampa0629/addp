import { nextTick, ref } from 'vue'
import { parseLocator } from '@common-ui'
import { discoveryRefreshMarker } from '../utils/protectionEnrollment.mjs'

export function useProtectionEnrollmentDetail({
  enrollment,
  getEnrollment,
  getItem,
  listEnrollments,
  loadGovernance,
  resetGovernance,
  loadDefinitions,
  navigateToResources,
  listPendingFindings,
  onAuthorizationLoadError = () => {},
  onResourceLoadError = () => {},
  onDefinitionsLoadError = () => {},
  onLocatorLoadError = () => {}
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

  async function openLocator(locator, { isCurrent: routeCurrent = () => true } = {}) {
    if (disposed) return { status: 'unavailable' }
    const request = ++sessionRequest
    const current = () => request === sessionRequest && !disposed && routeCurrent()
    try {
      const parsed = parseLocator(locator)
      if (!Number.isSafeInteger(parsed.itemId) || parsed.itemId <= 0 || !Number.isSafeInteger(parsed.engineId) || parsed.engineId <= 0) {
        throw new Error('DataItem locator required')
      }
      const item = await getItem(parsed.itemId)
      if (!current()) return { status: 'stale' }
      if (Number(item?.id) !== parsed.itemId || Number(item?.engine_id) !== parsed.engineId || item?.item_type !== parsed.type || !item?.fingerprint) {
        throw new Error('Meta item identity differs from selected resource')
      }
      const matchesTarget = row => row?.target?.owner_module === 'meta'
        && row.target.resource_type === 'data_item' && row.target.resource_identity === item.fingerprint
        && Number(row.target_snapshot?.engine_id) === parsed.engineId && row.state !== 'released'
      let found = null
      let total = null
      for (let page = 1; ; page += 1) {
        const result = await listEnrollments({ scope: 'current', engine_id: parsed.engineId, page, page_size: 100 })
        if (!current()) return { status: 'stale' }
        if (!Array.isArray(result?.data) || !Number.isSafeInteger(result.total_pages) || result.total_pages < 0
            || !Number.isSafeInteger(result.total) || result.total < 0 || result.page !== page
            || result.page_size !== 100 || result.total_pages !== Math.ceil(result.total / 100)
            || result.data.length !== Math.min(100, Math.max(0, result.total - (page - 1) * 100))
            || (total !== null && result.total !== total)) {
          throw new Error('Incomplete or changing protection lookup pages')
        }
        total = result.total
        for (const row of result.data) {
          if (row?.target?.resource_identity !== item.fingerprint || row.state === 'released') continue
          if (!matchesTarget(row) || found || !row.id) throw new Error('Ambiguous current protection target')
          found = row
        }
        if (page >= result.total_pages) break
      }
      if (!found) return { status: 'not_enrolled' }
      const latest = await getEnrollment(found.id)
      if (!current()) return { status: 'stale' }
      if (latest?.id !== found.id || !matchesTarget(latest)) throw new Error('Protection target changed during lookup')
      const result = await activate(latest, '', request)
      if (!current()) {
        if (request === sessionRequest && !disposed) requestClose()
        return { status: 'stale' }
      }
      return result
    } catch (error) {
      if (!current()) return { status: 'stale' }
      onLocatorLoadError(error)
      return { status: 'failed', error }
    }
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
    openLocator,
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
