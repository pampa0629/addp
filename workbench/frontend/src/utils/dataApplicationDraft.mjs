import { initialApplicationParameterValue } from './dataApplicationParameters.mjs'
import { buildComponentQuery } from './dataApplicationRuntime.mjs'

const APPLICATION_PARAMETER_PRESET_KEY_PATTERN = /^[a-z][a-z0-9-]{0,63}$/

function cloneValue(value) {
  return structuredClone(value)
}

export function createApplicationParameterPreset(snapshot, key, name) {
  return {
    key,
    name,
    parameter_values: Object.fromEntries((snapshot?.parameters || []).map((parameter) => [
      parameter.key,
      initialApplicationParameterValue(parameter),
    ])),
  }
}

export function synchronizeApplicationParameterPresets(snapshot) {
  const parameters = snapshot?.parameters || []
  snapshot.parameter_presets = (snapshot?.parameter_presets || []).map((preset) => ({
    ...preset,
    parameter_values: Object.fromEntries(parameters.map((parameter) => [
      parameter.key,
      Object.prototype.hasOwnProperty.call(preset.parameter_values || {}, parameter.key)
        ? cloneValue(preset.parameter_values[parameter.key])
        : initialApplicationParameterValue(parameter),
    ])),
  }))
  return snapshot.parameter_presets
}

export function applicationParameterPresetsValid(snapshot) {
  const parameters = snapshot?.parameters || []
  const presets = snapshot?.parameter_presets || []
  if (presets.length > 20 || (presets.length > 0 && parameters.length === 0)) return false
  const parameterKeys = new Set(parameters.map((parameter) => parameter.key))
  const presetKeys = new Set()
  for (const preset of presets) {
    if (!APPLICATION_PARAMETER_PRESET_KEY_PATTERN.test(preset?.key || '') || !preset?.name?.trim() || preset.name.trim().length > 100 || presetKeys.has(preset.key)) return false
    presetKeys.add(preset.key)
    const values = preset.parameter_values || {}
    if (Object.keys(values).length !== parameterKeys.size || Object.keys(values).some((key) => !parameterKeys.has(key))) return false
    for (const parameter of parameters) {
      if (!Object.prototype.hasOwnProperty.call(values, parameter.key)) return false
      const value = values[parameter.key]
      if (!parameter.required) continue
      if (value === null || value === undefined || value === '' || (Array.isArray(value) && value.length === 0)) return false
    }
    try {
      for (const component of snapshot?.components || []) buildComponentQuery(snapshot, component, values)
    } catch {
      return false
    }
  }
  return true
}

export function normalizedApplicationSnapshot(snapshot) {
  const normalized = JSON.parse(JSON.stringify(snapshot))
  const used = new Set(normalized.parameter_bindings.map((binding) => binding.application_parameter_key))
  normalized.parameters = normalized.parameters.filter((parameter) => used.has(parameter.key))
  synchronizeApplicationParameterPresets(normalized)
  return normalized
}

export function buildDataApplicationPreview(application) {
  return {
    name: application.name.trim(),
    description: application.description.trim(),
    revision_number: 0,
    snapshot: normalizedApplicationSnapshot(application.snapshot),
  }
}

export function dataApplicationEditorRouteContext(routeName, applicationID = '') {
  if (routeName === 'DataApplicationCreate') return 'create'
  return `edit:${String(applicationID || '').trim()}`
}

export function dataApplicationEditorMutationContext(routeName, applicationID, action) {
  return `${dataApplicationEditorRouteContext(routeName, applicationID)}:${String(action || '').trim()}`
}

export function dataApplicationListPageContext(page) {
  const normalized = Number(page)
  return `page:${Number.isInteger(normalized) && normalized > 0 ? normalized : 1}`
}

export function dataApplicationDeletionContext(applicationID, version) {
  return `delete:${String(applicationID || '').trim()}:${String(version ?? '').trim()}`
}

export function commitLatestDataApplicationRequest(requests, request, currentContext, commit) {
  if (!requests.isCurrent(request, currentContext)) return false
  commit()
  return true
}

export async function confirmDataApplicationAction(confirm, message) {
  try {
    await confirm(message)
    return true
  } catch (error) {
    if (error === 'cancel' || error === 'close') return false
    throw error
  }
}
