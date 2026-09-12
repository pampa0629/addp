import { computed, nextTick, reactive, ref, unref } from 'vue'
import { createRequiredRule } from '../utils/foundationForm.mjs'
import { isProtectionAccessRequestExpired, isResourceVersionConflict } from '../utils/protectionEnrollment.mjs'

function snapshotRequest(row) {
  if (!row) return null
  return {
    ...row,
    component: row.component ? { ...row.component } : null,
    requester: row.requester ? { ...row.requester } : null
  }
}

export function useProtectionAccessRequestReview({
  canReview,
  listRequests,
  getRequest,
  decideRequest,
  refreshCollection,
  t,
  onQueueLoadError = () => {},
  onDecisionReloaded = () => {},
  onAlreadyProcessed = () => {},
  onDecisionLoadError = () => {},
  onDecisionSucceeded = () => {},
  onDecisionConflict = () => {},
  onDecisionExpired = () => {},
  onDecisionError = () => {}
}) {
  const rows = ref([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(10)
  const loading = ref(false)
  const scope = ref('pending')
  const filters = reactive({ resourceSearch: '', requesterSearch: '', state: '', authorizationState: '' })
  const createdRange = ref([])

  const decisionDialog = ref(false)
  const decisionFormRef = ref(null)
  const decisionCancelButton = ref(null)
  const decisionSaving = ref(false)
  const decisionReloading = ref(false)
  const decisionConflict = ref(false)
  const decidingRequest = ref(null)
  const decision = ref('approve')
  const decisionForm = reactive({ rationale: '' })

  let queueRequest = 0
  let decisionReloadRequest = 0
  let decisionSubmitRequest = 0
  let disposed = false

  const decisionRules = computed(() => ({
    rationale: [createRequiredRule(t('security.common.requiredField', {
      name: t('security.accessRequest.decisionRationaleLabel')
    }), { trigger: 'blur', whitespace: true })]
  }))
  const decisionTitle = computed(() => t(`security.accessRequest.${decision.value}`))
  const decisionHint = computed(() => t(`security.accessRequest.${decision.value}Hint`))
  const decisionConfirmLabel = computed(() => t(`security.accessRequest.confirmActions.${decision.value}`))

  function queueParams(requestedPage) {
    const range = Array.isArray(createdRange.value) ? createdRange.value : []
    return {
      scope: scope.value,
      state: scope.value === 'history' ? filters.state || undefined : undefined,
      authorization_state: scope.value === 'history' ? filters.authorizationState || undefined : undefined,
      requester_search: filters.requesterSearch || undefined,
      resource_search: filters.resourceSearch || undefined,
      created_from: range[0] instanceof Date ? range[0].toISOString() : undefined,
      created_to: range[1] instanceof Date ? range[1].toISOString() : undefined,
      page: requestedPage,
      page_size: pageSize.value
    }
  }

  async function loadQueue(requestedPage = page.value) {
    if (disposed) return false
    const request = ++queueRequest
    page.value = Number(requestedPage) || 1
    if (!unref(canReview)) {
      rows.value = []
      total.value = 0
      loading.value = false
      return false
    }

    loading.value = true
    try {
      let response = await listRequests(queueParams(page.value))
      if (request !== queueRequest || disposed) return false
      const totalPages = Number(response?.total_pages || 0)
      if (totalPages > 0 && page.value > totalPages) {
        page.value = totalPages
        response = await listRequests(queueParams(page.value))
        if (request !== queueRequest || disposed) return false
      }
      rows.value = Array.isArray(response?.data) ? response.data : []
      total.value = Number(response?.total || 0)
      return true
    } catch (error) {
      if (request === queueRequest && !disposed) onQueueLoadError(error)
      return false
    } finally {
      if (request === queueRequest) loading.value = false
    }
  }

  async function changeScope() {
    page.value = 1
    filters.state = ''
    filters.authorizationState = ''
    return loadQueue(1)
  }

  function updateFilters(nextFilters) {
    Object.assign(filters, nextFilters)
  }

  async function applyFilters() {
    page.value = 1
    return loadQueue(1)
  }

  async function resetFilters() {
    filters.resourceSearch = ''
    filters.requesterSearch = ''
    filters.state = ''
    filters.authorizationState = ''
    createdRange.value = []
    page.value = 1
    return loadQueue(1)
  }

  function replaceRequest(latest) {
    const index = rows.value.findIndex(item => item.id === latest?.id)
    if (index >= 0) rows.value.splice(index, 1, latest)
  }

  function openDecision(row, nextDecision) {
    if (disposed || !row?.id || !row.can_decide || !['approve', 'reject'].includes(nextDecision)) return false
    decisionReloadRequest += 1
    decisionSubmitRequest += 1
    clearDecisionSession()
    decidingRequest.value = snapshotRequest(row)
    decision.value = nextDecision
    decisionDialog.value = true
    return true
  }

  function clearDecisionSession() {
    decisionDialog.value = false
    decidingRequest.value = null
    decision.value = 'approve'
    decisionForm.rationale = ''
    decisionSaving.value = false
    decisionConflict.value = false
    decisionReloading.value = false
  }

  function closeDecision() {
    const hasActiveSession = decisionDialog.value || Boolean(decidingRequest.value) || decisionSaving.value || decisionReloading.value
    if (!hasActiveSession) return
    decisionReloadRequest += 1
    decisionSubmitRequest += 1
    clearDecisionSession()
  }

  function focusDecisionCancel() {
    nextTick(() => {
      decisionFormRef.value?.clearValidate?.()
      const button = decisionCancelButton.value?.$el || decisionCancelButton.value
      button?.focus?.()
    })
  }

  async function reloadDecisionBaseline() {
    if (disposed || decisionReloading.value || decisionSaving.value || !decidingRequest.value?.id) return { status: 'unavailable' }
    const request = ++decisionReloadRequest
    const requestID = decidingRequest.value.id
    decisionReloading.value = true
    try {
      const latest = await getRequest(requestID)
      if (request !== decisionReloadRequest || disposed) return { status: 'stale' }
      replaceRequest(latest)
      if (latest?.state !== 'pending' || !latest.can_decide) {
        clearDecisionSession()
        await loadQueue(page.value)
        if (request !== decisionReloadRequest || disposed) return { status: 'stale' }
        onAlreadyProcessed()
        return { status: 'already_processed', request: latest }
      }
      decidingRequest.value = snapshotRequest(latest)
      decisionForm.rationale = ''
      decisionConflict.value = false
      onDecisionReloaded()
      focusDecisionCancel()
      return { status: 'reloaded', request: latest }
    } catch (error) {
      if (request !== decisionReloadRequest || disposed) return { status: 'stale' }
      onDecisionLoadError(error)
      return { status: 'failed', error }
    } finally {
      if (request === decisionReloadRequest) decisionReloading.value = false
    }
  }

  async function submitDecision() {
    const row = decidingRequest.value
    const submittedDecision = decision.value
    if (disposed || decisionSaving.value || decisionReloading.value || decisionConflict.value || !row?.id || !['approve', 'reject'].includes(submittedDecision)) {
      return { status: 'unavailable' }
    }
    const request = ++decisionSubmitRequest
    decisionSaving.value = true
    const validation = decisionFormRef.value?.validate?.()
    const valid = await Promise.resolve(validation).catch(() => false)
    if (request !== decisionSubmitRequest || disposed) return { status: 'stale' }
    if (!valid) {
      decisionSaving.value = false
      return { status: 'invalid' }
    }
    try {
      await decideRequest(row.id, {
        version: Number(row.version),
        decision: submittedDecision,
        expires_at: submittedDecision === 'approve' ? row.requested_expires_at : undefined,
        rationale: decisionForm.rationale.trim()
      })
      if (request !== decisionSubmitRequest || disposed) return { status: 'stale' }
      clearDecisionSession()
      page.value = 1
      await Promise.all([loadQueue(1), refreshCollection({ background: true })])
      if (request !== decisionSubmitRequest || disposed) return { status: 'stale' }
      onDecisionSucceeded(submittedDecision)
      return { status: 'succeeded', decision: submittedDecision }
    } catch (error) {
      if (request !== decisionSubmitRequest || disposed) return { status: 'stale' }
      if (isResourceVersionConflict(error)) {
        decisionConflict.value = true
        onDecisionConflict()
        return { status: 'version_conflict', error }
      }
      if (isProtectionAccessRequestExpired(error)) {
        clearDecisionSession()
        await loadQueue(page.value)
        if (request !== decisionSubmitRequest || disposed) return { status: 'stale' }
        onDecisionExpired()
        return { status: 'expired', error }
      }
      onDecisionError(error)
      return { status: 'failed', error }
    } finally {
      if (request === decisionSubmitRequest) decisionSaving.value = false
    }
  }

  function dispose() {
    disposed = true
    queueRequest += 1
    decisionReloadRequest += 1
    decisionSubmitRequest += 1
    loading.value = false
    decisionReloading.value = false
    decisionSaving.value = false
  }

  return {
    rows,
    total,
    page,
    pageSize,
    loading,
    scope,
    filters,
    createdRange,
    decisionDialog,
    decisionFormRef,
    decisionCancelButton,
    decisionSaving,
    decisionReloading,
    decisionConflict,
    decidingRequest,
    decision,
    decisionForm,
    decisionRules,
    decisionTitle,
    decisionHint,
    decisionConfirmLabel,
    loadQueue,
    changeScope,
    updateFilters,
    applyFilters,
    resetFilters,
    replaceRequest,
    openDecision,
    closeDecision,
    focusDecisionCancel,
    reloadDecisionBaseline,
    submitDecision,
    dispose
  }
}
