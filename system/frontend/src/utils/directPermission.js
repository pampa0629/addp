import { approvedBoundaryFields, readNativeApprovalFacts, serializeEngineCatalogTarget } from '@common-ui'

export function validateDirectPermission(row, engineID, fingerprint) {
  const validVersion = typeof row?.version === 'string' && /^(0|[1-9]\d{0,18})$/.test(row.version) && BigInt(row.version) <= 9223372036854775807n
  const validSource = row?.source === 'deployment_unknown' ? row.state === 'unknown' && row.version === '0'
    : ['explicit', 'deployment_default'].includes(row?.source) && ['allowed', 'denied'].includes(row.state) && row.version !== '0'
  if (!validVersion || !validSource || String(row.engine_id) !== String(engineID) || !/^[a-f0-9]{64}$/.test(row.item_fingerprint || '') ||
    (fingerprint && row.item_fingerprint !== fingerprint)) throw new Error('invalidDirectPermission')
  return row
}

// Reuse the canonical boundary validator, including exact file composition.
// The identity list is only navigation; it cannot supply a signing boundary.
export async function readPermissionTarget(engineID, fingerprint, readFacts) {
  const facts = await readFacts(engineID, fingerprint)
  const target = facts?.boundary?.native_target || facts?.boundary?.file_composition?.refs?.find(ref => ref.primary === true)?.target
  if (String(target?.engine_id) !== String(engineID) || facts?.item_fingerprint !== fingerprint) throw new Error('invalidDataItemBoundary')
  const observed = await readNativeApprovalFacts(target, async (id, fp) => {
    if (String(id) !== String(engineID) || fp !== fingerprint) throw new Error('invalidDataItemBoundary')
    return facts
  })
  if (observed.state !== 'complete') throw new Error('invalidDataItemBoundary')
  return { path: target, binding: observed.binding }
}

export function captureDirectPermission(row, target, allowed, reason) {
  validateDirectPermission(row, target.path.engine_id, target.binding.item_fingerprint)
  reason = typeof reason === 'string' ? reason.trim() : ''
  if (typeof allowed !== 'boolean' || !reason || Array.from(reason).length > 2000) throw new Error('invalidDirectPermission')
  const fields = approvedBoundaryFields(target.path, target.binding)
  return `{"catalog_path":${serializeEngineCatalogTarget({ ...target.path, engine_id: String(row.engine_id) })},"expected_boundary_hash":${JSON.stringify(fields.expected_boundary_hash)},"allowed":${allowed},"expected_source":${JSON.stringify(row.source)},"expected_version":${JSON.stringify(row.version)},"reason":${JSON.stringify(reason)}}`
}
