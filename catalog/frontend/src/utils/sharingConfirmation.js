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
    confirm,
    create: confirm && owner && entry?.entry_status === 'active' && entry?.governance_status !== 'deprecated' &&
      entry?.entry_type === 'data_item' && entry?.source?.source_module === 'meta' && entry?.source?.source_status === 'active',
    history: read && auth.hasPermission('system.engine_access_fulfillment.create')
  }
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

// The existing System configuration API expects a numeric engine_id. Emit the
// validated decimal token directly; Number() would round large int64 IDs.
export function serializeApprovalInitialization(target, reason) {
  reason = typeof reason === 'string' ? reason.trim() : ''
  if (!isSharingInt64(target?.engine_id) || !target?.version || !Array.isArray(target?.segments) || !target.segments.length ||
      !reason || Array.from(reason).length > 2000) throw new Error('invalidInitialization')
  return `{"catalog_path":{"engine_id":${target.engine_id},"version":${JSON.stringify(target.version)},"segments":${JSON.stringify(target.segments)}},"reason":${JSON.stringify(reason)}}`
}
