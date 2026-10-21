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
  if (binding.file_composition && mode !== 'independent') throw new Error('invalidApprovalInitialization')
  return `{"catalog_path":${serializeEngineCatalogTarget(target)},"mode":${JSON.stringify(mode)},"reason":${JSON.stringify(reason)},"item_fingerprint":${JSON.stringify(fields.item_fingerprint)},"expected_boundary_hash":${JSON.stringify(fields.expected_boundary_hash)}}`
}

// Protocol validation only. Owner facts are always fetched by the caller;
// neither a browser candidate fingerprint nor a tree selection proves access.
export function approvedBoundaryFields(target, binding) {
  const file = target?.segments?.at(-1)?.term === 'file'
  const path = file ? fileCompositionPrimary(binding?.file_composition) : binding?.native_target
  if (!/^[0-9a-f]{64}$/.test(binding?.item_fingerprint || '') || !/^[0-9a-f]{64}$/.test(binding?.boundary_hash || '') ||
      (file ? binding?.native_target !== undefined : binding?.file_composition !== undefined) || !samePath(path, target)) {
    throw new Error('invalidDataItemBoundary')
  }
  return { item_fingerprint: binding.item_fingerprint, expected_boundary_hash: binding.boundary_hash }
}

const boundaryError = () => { throw new Error('invalidDataItemBoundary') }
function samePath(left, right) {
  return !!left && !!right && String(left.engine_id) === String(right.engine_id) && left.version === right.version &&
    Array.isArray(left.segments) && left.segments.length === right.segments?.length &&
    left.segments.every((part, index) => ['term', 'kind', 'name'].every(key => part?.[key] === right.segments[index]?.[key]))
}
function filePath(path) {
  if (path?.version !== 'catalog.path/v1' || !Number.isSafeInteger(Number(path.engine_id)) || Number(path.engine_id) <= 0 ||
      !Array.isArray(path.segments) || path.segments.length < 2 || path.segments.length > 64) return false
  return path.segments.every((part, index) => {
    const term = index === 0 ? 'root' : index === path.segments.length - 1 ? 'file' : 'directory'
    return part?.term === term && part.kind === term && typeof part.name === 'string' &&
      (index === 0 ? part.name === '' : part.name && part.name !== '.' && part.name !== '..' &&
        part.name.isWellFormed() && !/[/\\\p{Cc}]/u.test(part.name) && new TextEncoder().encode(part.name).length <= 4096)
  }) && new TextEncoder().encode(path.segments.slice(1).map(part => part.name).join('/')).length <= 4096
}
// Only validate the owner response for display/command binding. The server
// independently rereads Meta and computes the approved boundary on submission.
function fileCompositionPrimary(composition) {
  if (!composition || Object.keys(composition).sort().join(',') !== 'format,refs' ||
      !['ifc', '3ds'].includes(composition.format) || !Array.isArray(composition.refs) ||
      !composition.refs.length || composition.refs.length > 200 || new TextEncoder().encode(JSON.stringify(composition)).length > 65536) return boundaryError()
  const seen = new Set(), primary = composition.refs.filter(ref => ref.primary === true)
  if (primary.length !== 1 || primary[0].required !== true || primary[0].role !== 'model') return boundaryError()
  const target = primary[0].target
  if (!filePath(target) || !target.segments.at(-1).name.toLowerCase().endsWith(`.${composition.format}`)) return boundaryError()
  const parent = target.segments.slice(0, -1)
  for (const ref of composition.refs) {
    if (!ref || Object.keys(ref).sort().join(',') !== 'primary,required,role,target' || typeof ref.required !== 'boolean' ||
        typeof ref.primary !== 'boolean' || !/^[a-z0-9_]{1,64}$/.test(ref.role) || !filePath(ref.target) ||
        String(ref.target.engine_id) !== String(target.engine_id) || ref.target.segments.length <= parent.length ||
        !parent.every((part, index) => ['term', 'kind', 'name'].every(key => part[key] === ref.target.segments[index]?.[key]))) return boundaryError()
    const key = JSON.stringify(ref.target.segments.map(({ term, kind, name }) => [term, kind, name]))
    if (seen.has(key)) return boundaryError()
    seen.add(key)
  }
  return target
}
function freeze(value) {
  if (value && typeof value === 'object') { Object.values(value).forEach(freeze); Object.freeze(value) }
  return value
}

export async function readNativeApprovalFacts(target, readFacts) {
  // Native paths use numeric Engine IDs. Normalize only exactly representable
  // numbers; never stringify an already rounded large identifier as identity.
  const id = typeof target?.engine_id === 'number' && Number.isSafeInteger(target.engine_id) ? String(target.engine_id) : target?.engine_id
  target = { ...target, engine_id: id }
  serializeEngineCatalogTarget(target)
  const file = target.segments.at(-1)?.term === 'file'
  if (file ? !filePath(target) : (target.segments.length !== 3 || target.segments[0].term !== 'server' || target.segments[0].kind !== 'server' ||
      target.segments[1].term !== 'schema' || target.segments[1].kind !== 'namespace' ||
      target.segments[2].term !== 'table' || target.segments[2].kind !== 'table')) throw new Error('invalidDataItemBoundary')
  const name = file ? target.segments.slice(1).map(part => part.name).join('/') : `${target.segments[1].name}.${target.segments[2].name}`
  const key = `${target.engine_id}:${name}`
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(key))
  const candidate = Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('')
  const facts = await readFacts(target.engine_id, candidate)
  if (facts?.schema_version !== 'meta.data_item_authorization_facts/v1' || facts.item_fingerprint !== candidate ||
      !['complete', 'incomplete', 'needs_refresh', 'unsupported'].includes(facts.basis_state)) throw new Error('invalidDataItemBoundary')
  if (facts.basis_state !== 'complete') {
    if (facts.boundary !== null || facts.boundary_hash !== null) throw new Error('invalidDataItemBoundary')
    return Object.freeze({ state: facts.basis_state, binding: null })
  }
  if (file && (String(facts.engine_id) !== target.engine_id || facts.item_type !== 'file' || facts.boundary?.layout !== 'single' ||
      facts.boundary?.native_target !== undefined)) throw new Error('invalidDataItemBoundary')
  const binding = { item_fingerprint: facts.item_fingerprint, boundary_hash: facts.boundary_hash,
    ...(file ? { file_composition: structuredClone(facts.boundary?.file_composition) } : { native_target: structuredClone(facts.boundary?.native_target) }) }
  approvedBoundaryFields(target, binding)
  return Object.freeze({ state: 'complete', binding: freeze(binding) })
}

export async function observeNativeApprovalBinding(target, readFacts) {
  const observation = await readNativeApprovalFacts(target, readFacts)
  if (observation.state !== 'complete') throw new Error('invalidDataItemBoundary')
  return observation.binding
}
