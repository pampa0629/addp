import { ref, unref } from 'vue'
import { resolvePendingReviewContinuation, resolveReviewQueueFilters } from '../utils/protectionEnrollment.mjs'

export function resolveFindingReviewQueueRouteState(routeQuery = {}) {
  return resolveReviewQueueFilters(routeQuery)
}

export function useProtectionFindingReviewQueue({
  canRead,
  initialRouteState = resolveFindingReviewQueueRouteState(),
  listFindings,
  onRefreshed = () => {},
  onLoadError = () => {}
}) {
  const rows = ref([])
  const total = ref(0)
  const page = ref(initialRouteState.page)
  const pageSize = ref(initialRouteState.pageSize)
  const sensitiveDataTypeID = ref(initialRouteState.sensitiveDataTypeID)
  const detectorVersion = ref(initialRouteState.detectorVersion)
  const loading = ref(false)

  let queueRequest = 0
  let disposed = false

  function applyRouteState(routeState) {
    sensitiveDataTypeID.value = routeState.sensitiveDataTypeID
    detectorVersion.value = routeState.detectorVersion
    page.value = routeState.page
    pageSize.value = routeState.pageSize
  }

  function routeQuery() {
    const state = resolveFindingReviewQueueRouteState({
      sensitive_data_type_id: sensitiveDataTypeID.value,
      detector_version: detectorVersion.value,
      page: page.value,
      page_size: pageSize.value
    })
    return { tab: 'review-queue', ...state.query }
  }

  function queueParams(requestedPage) {
    return {
      snapshot_scope: 'current',
      review_state: 'pending',
      sensitive_data_type_id: sensitiveDataTypeID.value || undefined,
      detector_version: detectorVersion.value || undefined,
      page: requestedPage,
      page_size: pageSize.value
    }
  }

  async function loadQueue(requestedPage = page.value) {
    if (disposed) return false
    const request = ++queueRequest
    page.value = Number(requestedPage) || 1
    if (!unref(canRead)) {
      rows.value = []
      total.value = 0
      loading.value = false
      return false
    }

    loading.value = true
    try {
      let resolvedPage = page.value
      let response = await listFindings(queueParams(resolvedPage))
      if (request !== queueRequest || disposed) return false
      const totalPages = Number(response?.total_pages || 0)
      if (totalPages > 0 && resolvedPage > totalPages) {
        resolvedPage = totalPages
        response = await listFindings(queueParams(resolvedPage))
        if (request !== queueRequest || disposed) return false
      }
      page.value = resolvedPage
      rows.value = Array.isArray(response?.data) ? response.data : []
      total.value = Number(response?.total || 0)
      onRefreshed()
      return true
    } catch (error) {
      if (request === queueRequest && !disposed) onLoadError(error)
      return false
    } finally {
      if (request === queueRequest) loading.value = false
    }
  }

  function prepareFilterChange() {
    page.value = 1
  }

  function resetFilters() {
    sensitiveDataTypeID.value = ''
    detectorVersion.value = ''
    page.value = 1
  }

  async function nextFindingAfterReview(reviewedFinding) {
    const previousPage = page.value
    const reviewedIndex = rows.value.findIndex(item => item.id === reviewedFinding?.id)
    if (!await loadQueue(page.value)) return { finding: null, pageChanged: false }

    let continuation = resolvePendingReviewContinuation({
      rows: rows.value,
      total: total.value,
      page: page.value,
      pageSize: pageSize.value,
      reviewedIndex
    })
    if (continuation.reload) {
      page.value = continuation.page
      if (!await loadQueue(continuation.page)) return { finding: null, pageChanged: page.value !== previousPage }
      continuation = resolvePendingReviewContinuation({
        rows: rows.value,
        total: total.value,
        page: page.value,
        pageSize: pageSize.value,
        reviewedIndex
      })
    }
    return { finding: continuation.finding, pageChanged: page.value !== previousPage }
  }

  function dispose() {
    disposed = true
    queueRequest += 1
    loading.value = false
  }

  return {
    rows,
    total,
    page,
    pageSize,
    sensitiveDataTypeID,
    detectorVersion,
    loading,
    applyRouteState,
    routeQuery,
    loadQueue,
    prepareFilterChange,
    resetFilters,
    nextFindingAfterReview,
    dispose
  }
}
