export const runtimeInstancePeriods = [
  ['15m', 15, 'm15'], ['30m', 30, 'm30'], ['1h', 60, 'h1'],
  ['6h', 360, 'h6'], ['12h', 720, 'h12'], ['24h', 1440, 'h24'], ['7d', 10080, 'd7']
].map(([value, minutes, label]) => ({ value, minutes, label: `common.runtimeInstanceTime.periods.${label}` }))

export function normalizeQueryTimestamp(value) {
  const text = typeof value === 'string' ? value.trim() : ''
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(text)) return ''
  const time = new Date(text), calendar = new Date(`${text.slice(0, 10)}T00:00:00Z`)
  if (Number.isNaN(time.getTime()) || Number.isNaN(calendar.getTime()) ||
      calendar.toISOString().slice(0, 10) !== text.slice(0, 10) || Number(text.slice(11, 13)) > 23) return ''
  return time.toISOString()
}

export function resolveRuntimeInstanceTime(input = {}) {
  const selection = { timeBasis: typeof input.time_basis === 'string' && input.time_basis.trim() === 'offline' ? 'offline' : 'registered', period: 'all', from: '', to: '' }
  const period = typeof input.time_period === 'string' ? input.time_period.trim() : ''
  if (runtimeInstancePeriods.some(item => item.value === period)) selection.period = period
  else if (period === 'custom') {
    const from = normalizeQueryTimestamp(input.time_from), to = normalizeQueryTimestamp(input.time_to)
    if (from && to && from < to) Object.assign(selection, { period, from, to })
  }
  const query = {
    ...(selection.timeBasis === 'offline' ? { time_basis: 'offline' } : {}),
    ...(selection.period !== 'all' ? { time_period: selection.period } : {}),
    ...(selection.period === 'custom' ? { time_from: selection.from, time_to: selection.to } : {})
  }
  return { ...selection, query }
}

export function validRuntimeInstanceRange(range) {
  return Array.isArray(range) && range.length === 2 &&
    new Date(range[0]).getTime() < new Date(range[1]).getTime()
}

export function runtimeInstanceTimeParams(selection, now = Date.now()) {
  const params = selection.timeBasis === 'offline' ? { time_basis: 'offline' } : {}
  if (selection.period === 'custom') return { ...params, time_from: selection.from, time_to: selection.to }
  const preset = runtimeInstancePeriods.find(item => item.value === selection.period)
  return !preset ? params : { ...params, time_from: new Date(now - preset.minutes * 60000).toISOString(), time_to: new Date(now).toISOString() }
}
