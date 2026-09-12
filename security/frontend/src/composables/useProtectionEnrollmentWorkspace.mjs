import { ref, unref } from 'vue'
import { navigateConsoleModuleRoute, resolveCanonicalTabRouteState } from '@common-ui'
import { resolveFindingReviewQueueRouteState } from './useProtectionFindingReviewQueue.mjs'

const WORKSPACE_TABS = ['resources', 'review-queue']

export function resolveProtectionEnrollmentWorkspaceRouteState(routeQuery = {}) {
  const requested = resolveCanonicalTabRouteState({
    allowedTabs: WORKSPACE_TABS,
    defaultTab: 'resources',
    routeQuery
  })
  const reviewQueue = resolveFindingReviewQueueRouteState(routeQuery)
  const preservedQuery = requested.tab === 'review-queue'
    ? reviewQueue.query
    : (String(routeQuery.action || '') === 'enroll' ? { action: 'enroll', locator: routeQuery.locator } : {})
  return {
    ...resolveCanonicalTabRouteState({
      allowedTabs: WORKSPACE_TABS,
      defaultTab: 'resources',
      routeQuery,
      preservedQuery
    }),
    reviewQueue
  }
}

export function useProtectionEnrollmentWorkspace({
  route,
  router,
  activeWorkspace,
  reviewQueue,
  resources,
  references,
  creation
}) {
  const manualRefreshing = ref(false)

  let mounted = false
  let disposed = false
  let transitionRevision = 0
  let currentRouteState = null

  function isTransitionCurrent(request) {
    return !disposed && request === transitionRevision
  }

  function isCurrent(request, workspace) {
    return isTransitionCurrent(request) && activeWorkspace.value === workspace
  }

  function beginTransition() {
    transitionRevision += 1
    return transitionRevision
  }

  async function navigateWorkspace(workspace, history = 'replace', request = null) {
    if (disposed || (request !== null && !isCurrent(request, workspace))) return false
    const query = workspace === 'review-queue' ? reviewQueue.routeQuery() : {}
    const location = { path: '/protection-enrollments', query }
    if (router.resolve(location).fullPath === route.fullPath) return false
    const navigationRequest = request === null ? beginTransition() : request
    await navigateConsoleModuleRoute(router, 'security', location, { history })
    return request === null
      ? isTransitionCurrent(navigationRequest)
      : isCurrent(request, workspace)
  }

  async function loadReviewWorkspace(routeState, request) {
    const expectedPage = Number(routeState.reviewQueue.page) || 1
    resources.stopAutoRefresh()
    const [loaded] = await Promise.all([
      reviewQueue.load(expectedPage),
      references.loadDefinitions(),
      references.loadDetectorCapabilities()
    ])
    if (!isCurrent(request, 'review-queue')) return false
    if (loaded && Number(unref(reviewQueue.page)) !== expectedPage) {
      await navigateWorkspace('review-queue', 'replace', request)
    }
    return isCurrent(request, 'review-queue')
  }

  async function loadResourceWorkspace(request) {
    await Promise.all([resources.load(), resources.loadAccessRequests()])
    if (!isCurrent(request, 'resources')) return false
    resources.scheduleAutoRefresh({ reset: true })
    return true
  }

  function loadWorkspace(routeState, request) {
    if (routeState.tab === 'review-queue') return loadReviewWorkspace(routeState, request)
    return loadResourceWorkspace(request)
  }

  async function handleRouteChange(routeQuery) {
    if (disposed) return false
    const request = beginTransition()
    const routeState = resolveProtectionEnrollmentWorkspaceRouteState(routeQuery)
    currentRouteState = routeState
    if (routeState.changed) {
      const location = { path: '/protection-enrollments', query: routeState.query }
      await navigateConsoleModuleRoute(router, 'security', location, { history: 'replace' })
      return isTransitionCurrent(request)
    }

    activeWorkspace.value = routeState.tab
    reviewQueue.applyRouteState(routeState.reviewQueue)
    if (routeState.tab === 'resources' && String(routeQuery.action || '') === 'enroll' && unref(creation.canCreate)) {
      creation.open(routeQuery.locator)
    }
    if (!mounted) return true
    return loadWorkspace(routeState, request)
  }

  async function mount() {
    if (disposed || mounted) return false
    mounted = true
    resources.startVisibilityTracking()
    const request = beginTransition()
    const workspaceLoad = currentRouteState && !currentRouteState.changed
      ? loadWorkspace(currentRouteState, request)
      : Promise.resolve(false)
    const [loaded] = await Promise.all([workspaceLoad, references.loadEngines()])
    return loaded
  }

  async function manualRefresh() {
    if (disposed || manualRefreshing.value) return false
    const request = transitionRevision
    const workspace = activeWorkspace.value
    manualRefreshing.value = true
    try {
      if (workspace === 'review-queue') {
        const routeState = currentRouteState || resolveProtectionEnrollmentWorkspaceRouteState(route.query)
        return loadReviewWorkspace(routeState, request)
      }
      await Promise.all([resources.load({ background: true }), resources.loadAccessRequests()])
      if (!isCurrent(request, 'resources')) return false
      await resources.loadGovernance(unref(resources.findingsPage))
      if (!isCurrent(request, 'resources')) return false
      resources.scheduleAutoRefresh({ reset: true })
      return true
    } finally {
      if (!disposed) manualRefreshing.value = false
    }
  }

  async function handleWorkspaceChange(workspace) {
    resources.stopAutoRefresh()
    return navigateWorkspace(workspace)
  }

  async function handleReviewQueueFilterChange() {
    reviewQueue.prepareFilterChange()
    return navigateWorkspace('review-queue')
  }

  async function resetReviewQueueFilters() {
    reviewQueue.resetFilters()
    return navigateWorkspace('review-queue')
  }

  async function handleReviewQueuePageChange() {
    return navigateWorkspace('review-queue')
  }

  function dispose() {
    if (disposed) return
    beginTransition()
    disposed = true
    mounted = false
    manualRefreshing.value = false
    resources.stopAutoRefresh()
  }

  return {
    manualRefreshing,
    handleRouteChange,
    mount,
    manualRefresh,
    navigateWorkspace,
    handleWorkspaceChange,
    handleReviewQueueFilterChange,
    resetReviewQueueFilters,
    handleReviewQueuePageChange,
    dispose
  }
}
