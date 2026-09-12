import { ref } from 'vue'
import { discoveryRefreshMarker, needsEnrollmentRefresh } from '../utils/protectionEnrollment.mjs'

const AUTO_REFRESH_FAST_INTERVAL_MS = 2000
const AUTO_REFRESH_SLOW_INTERVAL_MS = 5000
const AUTO_REFRESH_FAST_WINDOW_MS = 30000
const AUTO_REFRESH_TIMEOUT_MS = 120000

function browserRuntime() {
  return {
    now: () => Date.now(),
    setTimeout: (callback, delay) => globalThis.setTimeout(callback, delay),
    clearTimeout: timer => globalThis.clearTimeout(timer),
    isHidden: () => typeof document !== 'undefined' && document.hidden,
    addVisibilityListener: callback => {
      if (typeof document !== 'undefined') document.addEventListener('visibilitychange', callback)
    },
    removeVisibilityListener: callback => {
      if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', callback)
    }
  }
}

export function useProtectionEnrollmentCollection({
  listEnrollments,
  onRowsLoaded = async () => {},
  onLoadError = () => {},
  onAutoRefreshTimeout = () => {},
  runtime = browserRuntime()
}) {
  const rows = ref([])
  const total = ref(0)
  const currentPage = ref(1)
  const pageSize = ref(20)
  const listScope = ref('current')
  const loading = ref(false)
  const backgroundRefreshing = ref(false)
  const autoRefreshActive = ref(false)
  const lastRefreshedAt = ref(null)

  const discoveryRefreshWatches = new Map()
  let refreshTimer = null
  let autoRefreshStartedAt = 0
  let autoRefreshTimedOut = false
  let refreshHiddenAt = 0
  let visibilityTrackingStarted = false
  let disposed = false

  function markRefreshed(at = new Date(runtime.now())) {
    lastRefreshedAt.value = at
  }

  function replaceRow(latest) {
    const index = rows.value.findIndex(item => item.id === latest?.id)
    if (index >= 0) rows.value.splice(index, 1, latest)
  }

  function setCollection(nextRows, nextTotal) {
    rows.value = Array.isArray(nextRows) ? nextRows : []
    total.value = Number(nextTotal || 0)
  }

  function watchDiscovery(enrollmentID, baselineMarker) {
    if (enrollmentID === undefined || enrollmentID === null) return
    discoveryRefreshWatches.set(enrollmentID, baselineMarker)
  }

  function reconcileDiscoveryRefreshWatches() {
    for (const [enrollmentID, baselineMarker] of discoveryRefreshWatches) {
      const row = rows.value.find(item => item.id === enrollmentID)
      if (!row || row.state === 'released') {
        discoveryRefreshWatches.delete(enrollmentID)
        continue
      }
      if (row.last_discovered_at && discoveryRefreshMarker(row) !== baselineMarker) {
        discoveryRefreshWatches.delete(enrollmentID)
      }
    }
  }

  function hasPendingRefresh() {
    reconcileDiscoveryRefreshWatches()
    return discoveryRefreshWatches.size > 0 || rows.value.some(needsEnrollmentRefresh)
  }

  function clearRefreshTimer() {
    if (refreshTimer !== null) runtime.clearTimeout(refreshTimer)
    refreshTimer = null
  }

  function stopAutoRefresh() {
    clearRefreshTimer()
    autoRefreshActive.value = false
    autoRefreshStartedAt = 0
  }

  function scheduleAutoRefresh({ reset = false } = {}) {
    clearRefreshTimer()
    if (disposed) return
    if (!hasPendingRefresh()) {
      stopAutoRefresh()
      return
    }

    const now = runtime.now()
    if (reset || !autoRefreshStartedAt) {
      autoRefreshStartedAt = now
      autoRefreshTimedOut = false
    }
    autoRefreshActive.value = true
    if (runtime.isHidden()) return

    const elapsed = now - autoRefreshStartedAt
    if (elapsed >= AUTO_REFRESH_TIMEOUT_MS) {
      stopAutoRefresh()
      if (!autoRefreshTimedOut) {
        autoRefreshTimedOut = true
        onAutoRefreshTimeout()
      }
      return
    }

    const delay = elapsed < AUTO_REFRESH_FAST_WINDOW_MS
      ? AUTO_REFRESH_FAST_INTERVAL_MS
      : AUTO_REFRESH_SLOW_INTERVAL_MS
    refreshTimer = runtime.setTimeout(runAutoRefresh, delay)
  }

  async function load(options = {}) {
    const background = Boolean(options?.background)
    if (loading.value || backgroundRefreshing.value) return false
    if (background) backgroundRefreshing.value = true
    else loading.value = true

    try {
      const response = await listEnrollments({
        scope: listScope.value,
        page: currentPage.value,
        page_size: pageSize.value
      })
      setCollection(response?.data, response?.total)
      await onRowsLoaded({ rows: rows.value, options })
      markRefreshed()
      return true
    } catch (error) {
      if (!options?.silent) onLoadError(error)
      return false
    } finally {
      if (background) backgroundRefreshing.value = false
      else loading.value = false
    }
  }

  async function runAutoRefresh() {
    refreshTimer = null
    if (disposed || runtime.isHidden()) return
    await load({ background: true, silent: true, syncFindings: true })
    scheduleAutoRefresh()
  }

  async function refreshCurrentPage() {
    await load()
    scheduleAutoRefresh({ reset: true })
  }

  async function refreshChangedScope() {
    currentPage.value = 1
    await load()
    scheduleAutoRefresh({ reset: true })
  }

  function handleVisibilityChange() {
    if (runtime.isHidden()) {
      refreshHiddenAt = runtime.now()
      clearRefreshTimer()
      return
    }
    if (refreshHiddenAt && autoRefreshStartedAt) {
      autoRefreshStartedAt += runtime.now() - refreshHiddenAt
    }
    refreshHiddenAt = 0
    if (autoRefreshActive.value) void runAutoRefresh()
  }

  function startVisibilityTracking() {
    if (visibilityTrackingStarted) return
    disposed = false
    runtime.addVisibilityListener(handleVisibilityChange)
    visibilityTrackingStarted = true
  }

  function dispose() {
    disposed = true
    stopAutoRefresh()
    if (!visibilityTrackingStarted) return
    runtime.removeVisibilityListener(handleVisibilityChange)
    visibilityTrackingStarted = false
  }

  return {
    rows,
    total,
    currentPage,
    pageSize,
    listScope,
    loading,
    backgroundRefreshing,
    autoRefreshActive,
    lastRefreshedAt,
    load,
    markRefreshed,
    replaceRow,
    setCollection,
    watchDiscovery,
    scheduleAutoRefresh,
    stopAutoRefresh,
    refreshCurrentPage,
    refreshChangedScope,
    startVisibilityTracking,
    dispose
  }
}
