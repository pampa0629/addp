export function resourceCandidateKey(candidate) {
  return JSON.stringify([
    candidate?.role || '',
    Number(candidate?.engine_id) || 0,
    candidate?.locator || ''
  ])
}

export function groupResourceCandidates(candidates) {
  const groups = new Map()
  for (const candidate of Array.isArray(candidates) ? candidates : []) {
    if (!candidate?.role || !candidate?.locator) continue
    if (!groups.has(candidate.role)) groups.set(candidate.role, [])
    groups.get(candidate.role).push(candidate)
  }
  return [...groups.entries()].map(([role, roleCandidates]) => ({
    role,
    candidates: roleCandidates
  }))
}

export function defaultResourceCandidatesByRole(candidates) {
  return Object.fromEntries(
    groupResourceCandidates(candidates)
      .filter(group => group.candidates.length === 1)
      .map(group => [group.role, resourceCandidateKey(group.candidates[0])])
  )
}

export function hasSelectedResourceForEveryRole(candidates, selectedCandidatesByRole) {
  const groups = groupResourceCandidates(candidates)
  if (!groups.length) return false
  return groups.every(group => group.candidates.some(candidate => (
    selectedCandidatesByRole?.[group.role] === resourceCandidateKey(candidate)
  )))
}

export function confirmedResources(candidates, selectedCandidatesByRole) {
  if (!hasSelectedResourceForEveryRole(candidates, selectedCandidatesByRole)) return []
  const seen = new Set()
  return (Array.isArray(candidates) ? candidates : [])
    .filter(candidate => {
      const key = resourceCandidateKey(candidate)
      if (selectedCandidatesByRole?.[candidate.role] !== key || seen.has(key)) return false
      seen.add(key)
      return true
    })
    .map(resourceFact)
}

export function resourceFact(candidate) {
  return {
    role: candidate?.role || candidate?.name || candidate?.locator,
    ...(Number(candidate?.engine_id) > 0 ? { engine_id: Number(candidate.engine_id) } : {}),
    locator: candidate?.locator || '',
    ...(candidate?.source_engine_type ? { source_engine_type: candidate.source_engine_type } : {}),
    ...(candidate?.full_name ? { full_name: candidate.full_name } : {}),
    ...(candidate?.query_names && typeof candidate.query_names === 'object'
      ? { query_names: candidate.query_names } : {}),
    ...(candidate?.schema_coverage ? { schema_coverage: candidate.schema_coverage } : {}),
    ...(candidate?.data_type ? { data_type: candidate.data_type } : {}),
    ...(candidate?.geometry_column ? { geometry_column: candidate.geometry_column } : {}),
    ...(candidate?.geometry_type ? { geometry_type: candidate.geometry_type } : {}),
    ...(candidate?.crs ? { crs: candidate.crs } : {}),
    ...(Array.isArray(candidate?.fields) ? { fields: candidate.fields } : {})
  }
}
