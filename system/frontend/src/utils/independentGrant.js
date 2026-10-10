import { serializeEngineCatalogTarget } from '@common-ui'
import { safePickerEngineID } from './engineApprovalCatalog'

function decimal(value) {
  if (typeof value === 'number' && !Number.isSafeInteger(value)) throw new Error('invalidGrant')
  const result = String(value)
  if (!/^[1-9]\d{0,18}$/.test(result) || BigInt(result) > 9223372036854775807n) throw new Error('invalidGrant')
  return result
}

export function captureIndependentGrant(engineID, requirement, form, candidates, now = Date.now()) {
  const id = String(safePickerEngineID(engineID)), path = requirement?.catalog_path
  if (requirement?.mode !== 'independent' || String(requirement.engine_id) !== id || String(path?.engine_id) !== id ||
    path?.segments?.at(-1)?.term !== 'table' || path.segments.at(-1).kind !== 'table') throw new Error('invalidGrant')
  const candidate = candidates.find(row => row.id === form.recipientID)
  const reason = form.reason?.trim()
  if (!['user', 'department', 'project_group'].includes(form.recipientType) || !candidate || !reason || Array.from(reason).length > 2000 ||
    !['at_time', 'until_revoked'].includes(form.expiryMode)) throw new Error('invalidGrant')
  let expiresAt = null
  if (form.expiryMode === 'at_time') {
    if (!(form.expiresAt instanceof Date) || !Number.isFinite(form.expiresAt.getTime()) || form.expiresAt.getTime() <= now) throw new Error('invalidGrant')
    expiresAt = form.expiresAt.toISOString()
  }
  if (requirement.initialize_approval && String(requirement.version) !== '1') throw new Error('invalidGrant')
  const fields = { request_id: crypto.randomUUID(), requirement_version: decimal(requirement.version), initialize_approval: requirement.initialize_approval === true, recipient_type: form.recipientType,
    recipient_id: decimal(candidate.id), action: 'read', expiry_mode: form.expiryMode, expires_at: expiresAt, reason }
  const encoded = JSON.stringify(fields)
  return Object.freeze({ requestID: fields.request_id, payload: `${encoded.slice(0, -1)},"catalog_path":${serializeEngineCatalogTarget({ ...path, engine_id: id })}}`,
    targetLabel: path.segments.filter(segment => segment.name).map(segment => segment.name).join(' / '), recipientLabel: candidate.name,
    recipientType: fields.recipient_type, recipientID: fields.recipient_id, expiresAt, expiryMode: form.expiryMode })
}
