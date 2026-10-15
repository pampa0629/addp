// The backend accepts a numeric int64 Engine ID inside the structured path.
// Serialize its canonical decimal spelling directly, never through Number.
export function serializeEngineCatalogTarget(target) {
  const id = target?.engine_id
  if (typeof id !== 'string' || !/^[1-9]\d{0,18}$/.test(id) || BigInt(id) > 9223372036854775807n ||
      typeof target.version !== 'string' || !target.version || !Array.isArray(target.segments) || !target.segments.length) {
    throw new Error('invalidEngineCatalogTarget')
  }
  return `{"engine_id":${id},"version":${JSON.stringify(target.version)},"segments":${JSON.stringify(target.segments)}}`
}

export function serializeEngineApprovalInitialization(target, mode, reason, binding) {
  reason = typeof reason === 'string' ? reason.trim() : ''
  if (!['catalog', 'independent'].includes(mode) || !reason || Array.from(reason).length > 2000) {
    throw new Error('invalidApprovalInitialization')
  }
  const fields = approvedBoundaryFields(target, binding)
  return `{"catalog_path":${serializeEngineCatalogTarget(target)},"mode":${JSON.stringify(mode)},"reason":${JSON.stringify(reason)},"item_fingerprint":${JSON.stringify(fields.item_fingerprint)},"expected_boundary_hash":${JSON.stringify(fields.expected_boundary_hash)}}`
}

// Protocol validation only. Owner facts are always fetched by the caller;
// neither a browser candidate fingerprint nor a tree selection proves access.
export function approvedBoundaryFields(target, binding) {
  const path = binding?.native_target
  if (!/^[0-9a-f]{64}$/.test(binding?.item_fingerprint || '') || !/^[0-9a-f]{64}$/.test(binding?.boundary_hash || '') ||
      !path || String(path.engine_id) !== String(target?.engine_id) || path.version !== target?.version ||
      !Array.isArray(path.segments) || path.segments.length !== target?.segments?.length ||
      !path.segments.every((part, index) => ['term', 'kind', 'name'].every(key => part[key] === target.segments[index]?.[key]))) {
    throw new Error('invalidDataItemBoundary')
  }
  return { item_fingerprint: binding.item_fingerprint, expected_boundary_hash: binding.boundary_hash }
}

export async function observeNativeApprovalBinding(target, readFacts) {
  // Native paths use numeric Engine IDs. Normalize only exactly representable
  // numbers; never stringify an already rounded large identifier as identity.
  const id = typeof target?.engine_id === 'number' && Number.isSafeInteger(target.engine_id) ? String(target.engine_id) : target?.engine_id
  target = { ...target, engine_id: id }
  serializeEngineCatalogTarget(target)
  if (target.segments.length !== 3 || target.segments[0].term !== 'server' || target.segments[0].kind !== 'server' ||
      target.segments[1].term !== 'schema' || target.segments[1].kind !== 'namespace' ||
      target.segments[2].term !== 'table' || target.segments[2].kind !== 'table') throw new Error('invalidDataItemBoundary')
  const key = `${target.engine_id}:${target.segments[1].name}.${target.segments[2].name}`
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(key))
  const candidate = Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('')
  const facts = await readFacts(target.engine_id, candidate)
  if (facts?.schema_version !== 'meta.data_item_authorization_facts/v1' || facts.item_fingerprint !== candidate || facts.basis_state !== 'complete') throw new Error('invalidDataItemBoundary')
  const binding = { item_fingerprint: facts.item_fingerprint, boundary_hash: facts.boundary_hash, native_target: facts.boundary?.native_target }
  approvedBoundaryFields(target, binding)
  return Object.freeze(binding)
}
