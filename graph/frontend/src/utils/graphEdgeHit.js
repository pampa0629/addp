// Rank native G6 hit candidates by their painted path, rather than by group order.
export function nearestGraphEdge(edges, point, preferred) {
  if (![point.x, point.y].every(Number.isFinite)) return null
  let selected = null, distance = Infinity
  for (const edge of edges) {
    const shape = edge.getKeyShape()
    if (!edge.getContainer().get('visible') || !shape.get('visible') || !shape.get('capture')) continue
    const m = shape.getTotalMatrix() || [1, 0, 0, 0, 1, 0, 0, 0, 1]
    const determinant = m[0] * m[4] - m[1] * m[3]
    if (!m.every(Number.isFinite) || !determinant) continue
    const dx = point.x - m[6], dy = point.y - m[7]
    if (!shape.isInShape((m[4] * dx - m[3] * dy) / determinant, (m[0] * dy - m[1] * dx) / determinant)) continue
    const squaredDistance = t => {
      const p = shape.getPoint(t)
      if (!p || ![p.x, p.y].every(Number.isFinite)) return Infinity
      const x = m[0] * p.x + m[3] * p.y + m[6]
      const y = m[1] * p.x + m[4] * p.y + m[7]
      return (x - point.x) ** 2 + (y - point.y) ** 2
    }
    let best = Infinity, index = 0
    for (let sample = 0; sample <= 32; sample++) {
      const d = squaredDistance(sample / 32)
      if (d < best) { best = d; index = sample }
    }
    let low = Math.max(0, (index - 1) / 32), high = Math.min(1, (index + 1) / 32)
    for (let step = 0; step < 24; step++) {
      const left = low + (high - low) / 3, right = high - (high - low) / 3
      if (squaredDistance(left) < squaredDistance(right)) high = right
      else low = left
    }
    const d = Math.sqrt(Math.min(best, squaredDistance((low + high) / 2)))
    if (Number.isFinite(d) && (d < distance - .01 || (Math.abs(d-distance) <= .01 && edge === preferred))) {
      selected = edge; distance = d
    }
  }
  return selected
}
