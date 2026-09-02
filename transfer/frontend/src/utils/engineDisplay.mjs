export function engineNameForID(engines, engineID) {
  const id = Number(engineID || 0)
  if (!id) return '-'
  const engine = (Array.isArray(engines) ? engines : []).find(candidate => Number(candidate?.id) === id)
  const name = String(engine?.name || '').trim()
  return name || '-'
}
