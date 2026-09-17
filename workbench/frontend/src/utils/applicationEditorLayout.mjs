// Placement is still the only persisted layout. Editing controls compile into it.
export function orderedPlacements(placements) {
  return [...placements].sort((a, b) => a.y - b.y || a.x - b.x)
}

export function arrangePlacements(placements, { componentID, width, height, beforeID, direction, columns } = {}) {
  const ordered = orderedPlacements(placements).map(item => ({ ...item }))
  const index = ordered.findIndex(item => item.component_id === componentID)
  if (index >= 0) {
    if (Number.isInteger(width)) ordered[index].width = Math.min(12, Math.max(1, width))
    if (Number.isInteger(height)) ordered[index].height = Math.min(24, Math.max(1, height))
    const destination = beforeID ? ordered.findIndex(item => item.component_id === beforeID) : index + (direction || 0)
    if (destination >= 0 && destination < ordered.length && destination !== index) {
      const [item] = ordered.splice(index, 1)
      ordered.splice(beforeID && index < destination ? destination - 1 : destination, 0, item)
    }
  }
  let x = 0, y = 0, rowHeight = 0
  return ordered.map(item => {
    if (columns === 1 || columns === 2) item.width = 12 / columns
    if (x + item.width > 12) { y += rowHeight; x = 0; rowHeight = 0 }
    const next = { ...item, x, y }
    x += item.width
    rowHeight = Math.max(rowHeight, item.height)
    return next
  })
}

// Excludes presentation and placement: these can reuse the last query result.
export function applicationQueryContext(snapshot) {
  return JSON.stringify({
    components: snapshot.components.map(({ id, service_ref, contract_fingerprint, query_template, parameter_definitions, default_parameter_values }) =>
      ({ id, service_ref, contract_fingerprint, query_template, parameter_definitions, default_parameter_values })),
    parameters: snapshot.parameters.map(({ key, required, default_value }) => ({ key, required, default_value })),
    bindings: snapshot.parameter_bindings,
  }, (_, value) => value && typeof value === 'object' && !Array.isArray(value)
    ? Object.fromEntries(Object.keys(value).sort().map(key => [key, value[key]]))
    : value)
}
