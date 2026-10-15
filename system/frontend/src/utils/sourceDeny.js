import { serializeEngineCatalogTarget } from '@common-ui'
import { safePickerEngineID } from './engineApprovalCatalog'

// Capture a single immutable restriction command. A retry must never extend
// its validity or replace its ID after an unknown outcome.
export function captureSourceDeny(engineID, row, form, now = Date.now()) {
  const id = String(safePickerEngineID(engineID)), path = row?.catalog_path
  const recipient = row?.recipient_id, reason = form.reason?.trim()
  if (String(path?.engine_id) !== id || row?.recipient_type !== 'user' ||
    typeof recipient !== 'string' || !/^[1-9]\d{0,18}$/.test(recipient) || BigInt(recipient) > 9223372036854775807n ||
    !/^[a-f0-9]{64}$/.test(row.item_fingerprint) || !reason || Array.from(reason).length > 2000 ||
    !['at_time', 'until_revoked'].includes(form.expiryMode)) throw new Error('invalidDeny')
  let expiresAt = null
  if (form.expiryMode === 'at_time') {
    if (!(form.expiresAt instanceof Date) || !Number.isFinite(form.expiresAt.getTime()) || form.expiresAt.getTime() <= now) throw new Error('invalidDeny')
    expiresAt = form.expiresAt.toISOString()
  }
  const denyID = crypto.randomUUID()
  const fields = JSON.stringify({ deny_id: denyID, item_fingerprint: row.item_fingerprint, recipient_type: 'user', recipient_id: recipient,
    action: 'read', expiry_mode: form.expiryMode, expires_at: expiresAt, reason })
  return Object.freeze({ denyID, payload: `${fields.slice(0, -1)},"catalog_path":${serializeEngineCatalogTarget({ ...path, engine_id: id })}}` })
}
