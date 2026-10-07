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

export function serializeEngineApprovalInitialization(target, mode, reason) {
  reason = typeof reason === 'string' ? reason.trim() : ''
  if (!['catalog', 'independent'].includes(mode) || !reason || Array.from(reason).length > 2000) {
    throw new Error('invalidApprovalInitialization')
  }
  return `{"catalog_path":${serializeEngineCatalogTarget(target)},"mode":${JSON.stringify(mode)},"reason":${JSON.stringify(reason)}}`
}
