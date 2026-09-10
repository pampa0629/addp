export function dataApplicationDeliveryContext(applicationID, revisionNumber) {
  return `delivery:${String(applicationID || '').trim()}:${String(revisionNumber ?? '').trim()}`
}

export function buildDataApplicationRuntimeRoute(applicationID, presetKey = '') {
  const id = String(applicationID || '').trim()
  if (!id) return ''
  const route = `/data-apps/${encodeURIComponent(id)}`
  if (typeof presetKey !== 'string' || presetKey === '') return route
  return `${route}?preset=${encodeURIComponent(presetKey)}`
}

export function dataApplicationDeliveryItems(runtime, defaultScenarioName) {
  if (!runtime) return []
  return [
    { presetKey: '', name: defaultScenarioName },
    ...runtime.snapshot.parameter_presets.map((preset) => ({
      presetKey: preset.key,
      name: preset.name,
    })),
  ]
}
