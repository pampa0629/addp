const decimal = value => typeof value === 'string' && /^[1-9]\d{0,18}$/.test(value) && BigInt(value) <= 9223372036854775807n
const reason = value => typeof value === 'string' && value.trim() && Array.from(value.trim()).length <= 2000

// UI protocol checks only; System rereads Meta and owns the actual decision.
export function captureMetaCollectionFacts(facts, engineID, taskID) {
  if (!decimal(engineID) || !decimal(taskID) || facts?.schema_version !== 'meta.scan_task_facts/v1' ||
    facts.engine_id !== engineID || facts.task_id !== taskID || !/^[a-f0-9]{64}$/.test(facts.task_definition_digest || '') ||
    facts.scope_kind !== 'engine' || facts.scan_depth !== 'basic' || typeof facts.force !== 'boolean' ||
    !facts.read_options || typeof facts.read_options !== 'object' || Array.isArray(facts.read_options) ||
    Object.keys(facts.read_options).some(key => !['encoding', 'has_header'].includes(key)) ||
    (facts.read_options.encoding !== undefined && facts.read_options.encoding !== '') ||
    (facts.read_options.has_header !== undefined && facts.read_options.has_header !== null)) throw new Error('invalidMetaCollectionFacts')
  return JSON.parse(JSON.stringify(facts))
}

export function captureMetaCollectionApproval(facts, engineID, taskID, purpose, expiresAt, now = Date.now()) {
  const checked = captureMetaCollectionFacts(facts, engineID, taskID)
  const expiry = expiresAt instanceof Date ? expiresAt : new Date(expiresAt || NaN)
  if (!reason(purpose) || !Number.isFinite(expiry.getTime()) || expiry.getTime() <= now || expiry.getUTCFullYear() > 9999) throw new Error('invalidMetaCollectionApproval')
  return Object.freeze({ command_id: crypto.randomUUID(), task_id: checked.task_id,
    expected_task_definition_digest: checked.task_definition_digest, purpose: purpose.trim(), expires_at: expiry.toISOString() })
}

export function metaCollectionRecordState(row, now = Date.now()) {
  if (row?.status === 'revoked') return 'revoked'
  const expiry = new Date(row?.expires_at).getTime()
  if (row?.status !== 'active' || !Number.isFinite(expiry)) return 'unknown'
  return expiry <= now ? 'expired' : 'unexpired'
}

export function captureMetaCollectionRevocation(row, reasonText) {
  if (metaCollectionRecordState(row) !== 'unexpired' || !decimal(row?.version) || !reason(reasonText)) throw new Error('invalidMetaCollectionRevocation')
  return Object.freeze({ version: row.version, reason: reasonText.trim() })
}
