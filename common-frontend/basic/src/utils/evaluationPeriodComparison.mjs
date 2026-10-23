// Compare evaluation-point means, not time-weighted integrals or scrape counts.
// Callers own metric validity, freshness and subject/launch identity validation.
export function compareEvaluationPeriods({ start, end, gridStart = start, periodSeconds, stepSeconds, points }) {
  const step = stepSeconds * 1000, width = periodSeconds * 1000
  const invalid = () => { throw new RangeError('invalid_period_comparison') }
  if (![start, end, gridStart, width, step].every(Number.isSafeInteger) || width <= 0 || step <= 0 || end - start !== 2 * width || gridStart < start || gridStart > end || !Array.isArray(points) || points.length !== Math.floor((end - gridStart) / step) + 1 || (end - start) / step > 1000) invalid()
  const values = new Map()
  for (const [index, point] of points.entries()) {
    if (point.at !== gridStart + index * step || (point.value !== null && (typeof point.value !== 'number' || !Number.isFinite(point.value)))) invalid()
    values.set(point.at, point.value)
  }
  const boundary = end - width
  const period = (from, to) => ({ start: new Date(from).toISOString(), end: new Date(to).toISOString(), valid: 0, total: 0, mean: null })
  const previous = period(start, boundary), current = period(boundary, end)
  let previousSum = 0, currentSum = 0
  // Extend a launch-clipped grid backwards to count unavailable history as missing.
  const phase = (gridStart - start) % step
  for (let at = start + (phase || step); at <= end; at += step) {
    const target = at <= boundary ? previous : current
    target.total++
    const value = values.get(at)
    if (value !== undefined && value !== null) {
      target.valid++
      if (target === previous) previousSum += value
      else currentSum += value
    }
  }
  if (![previousSum, currentSum].every(Number.isFinite)) invalid()
  const complete = gridStart === start && previous.total > 0 && current.total > 0 && previous.valid === previous.total && current.valid === current.total
  if (complete) { previous.mean = previousSum / previous.total; current.mean = currentSum / current.total }
  return { previous, current, complete, delta: complete ? current.mean - previous.mean : null, stepSeconds }
}
