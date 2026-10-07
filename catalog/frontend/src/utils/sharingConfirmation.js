import { isCanonicalPositiveID } from './entryEdit'

// Sharing commands require lossless decimal strings within the backend int64
// contract, rather than accepting normalized or already-rounded numbers.
function isSharingInt64(value) {
  return typeof value === 'string' && value === value.trim() && value.length <= 19 &&
    isCanonicalPositiveID(value) && BigInt(value) <= 9223372036854775807n
}

export function canonicalSharingUUID(value) {
  return typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value) && value !== '00000000-0000-0000-0000-000000000000' ? value : ''
}

export function hasInvalidSharingRouteIdentity(query) {
  return ['sharing_decision_id', 'sharing_request_id'].some(key => query[key] !== undefined && !canonicalSharingUUID(query[key]))
}

export function sharingEligibility(entry, auth) {
  const read = auth.hasPermission('catalog.entry.read')
  const confirm = read && auth.hasPermission('catalog.sharing_decision.create')
  const principal = auth.authContext?.principal
  const owner = principal?.type === 'user' && (entry?.responsibilities || []).some(item => (
    item.role === 'business_owner' && item.subject_type === 'user' && item.status === 'active' && item.subject_id === principal.id
  ))
  return {
    owner,
    confirm,
    create: confirm && owner && entry?.entry_status === 'active' && entry?.governance_status !== 'deprecated' &&
      entry?.entry_type === 'data_item' && entry?.source?.source_module === 'meta' && entry?.source?.source_status === 'active',
    history: read && auth.hasPermission('system.engine_access_fulfillment.create')
  }
}

// Explain only the selected record. These labels are not access decisions.
export function sharingProgress({ decisionID = '', requestID = '', confirmation, request, candidate,
  loading = false, error = false, results = [], resultsPage = 1, canCreate = false, canHandle = false }) {
  const state = (status, step, next = status) => ({ status, step, next })
  if (loading) return state('checking', -1)
  if (error) return state('unavailable', -1)
  let outcome
  if (requestID) {
    outcome = request || results.find(row => row.request_id === requestID)
    if (!outcome) return state('unavailable', -1)
    if (outcome.request_id !== requestID || (decisionID && outcome.decision_id !== decisionID)) return state('mismatch', -1)
  } else if (decisionID) {
    if (confirmation?.id !== decisionID && candidate?.id !== decisionID) return state('unavailable', -1)
    if (resultsPage !== 1) return state('history', -1)
    if (results.some(row => row.decision_id !== decisionID)) return state('mismatch', -1)
    outcome = results[0]
    if (!outcome) return state('awaitingHandling', 1, canHandle ? 'handle' : 'askHandler')
  } else {
    if (candidate) return state('awaitingHandling', 1, 'handle')
    return canCreate ? state('awaitingConfirmation', 0, 'confirm') : state('chooseConfirmation', 0)
  }
  if (outcome.state === 'accepted' && outcome.granted_at) return state('issued', 3)
  if (outcome.state === 'accepted') return state('issuing', 2)
  if (outcome.state === 'pending') return state('processing', 1)
  if (outcome.state === 'closed') return state('closed', 1)
  return state('unavailable', -1)
}

// Capture one explicit confirmation. Never regenerate its ID/parameters on a
// transport retry; never accept a rounded numeric version or manual recipient.
export function captureSharingConfirmation(entry, form, recipientOptions, decisionID, now = Date.now()) {
  const version = typeof entry?.version === 'string' ? entry.version : Number.isSafeInteger(entry?.version) ? String(entry.version) : ''
  const recipient = recipientOptions.find(item => item.id === form.recipientID && item.recipient_type === form.recipientType && item.status === 'active')
  if (!canonicalSharingUUID(decisionID) || !isSharingInt64(version) || !isSharingInt64(form.recipientID) || !recipient) throw new Error('invalidConfirmation')
  const reason = typeof form.reason === 'string' ? form.reason.trim() : ''
  if (!reason || Array.from(reason).length > 2000 || !['user', 'project_group'].includes(form.recipientType)) throw new Error('invalidConfirmation')
  let expiresAt = null
  if (form.expiryMode === 'at_time') {
    const date = form.expiresAt instanceof Date ? form.expiresAt : null
    if (!date || !Number.isFinite(date.getTime()) || date.getTime() <= now) throw new Error('invalidConfirmation')
    expiresAt = date.toISOString()
  } else if (form.expiryMode !== 'until_revoked' || form.expiresAt != null) throw new Error('invalidConfirmation')
  return Object.freeze({ decision_id: decisionID, version, recipient_type: form.recipientType,
    recipient_id: form.recipientID, expiry_mode: form.expiryMode, expires_at: expiresAt, reason })
}

export function validateHandlingRequirement(candidate, requirement) {
  if (!canonicalSharingUUID(candidate?.id) ||
      !isSharingInt64(candidate?.target?.engine_id) || !candidate?.target?.version || !Array.isArray(candidate?.target?.segments) ||
      requirement?.mode !== 'catalog' || typeof requirement?.requirement_version !== 'string' ||
      !isSharingInt64(requirement.requirement_version)) throw new Error('invalidHandlingRequirement')
}

export function captureSharingRequest(candidate, requirement, requestID) {
  validateHandlingRequirement(candidate, requirement)
  if (!canonicalSharingUUID(requestID)) throw new Error('invalidHandlingRequirement')
  return Object.freeze({ request_id: requestID, decision_id: candidate.id, requirement_version: requirement.requirement_version })
}
