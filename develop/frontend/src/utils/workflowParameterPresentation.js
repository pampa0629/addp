import { formatLocatorDisplayPath, parseLocatorSafe } from '@addp/common-frontend'
import { isWorkflowInputParameter } from './workflowInputBindings'
import {
  getResourceBinding,
  resourceBindingGeometryColumnParam,
  resourceBindingNameParam
} from './workflowResourceBindings'

export function workflowResourcePresentation(parameter, params = {}, enginesById = {}) {
  const binding = getResourceBinding(parameter)
  if (!binding) return emptyResourcePresentation()

  const locatorParam = binding.mode === 'target'
    ? binding.parent_locator_param
    : binding.locator_param
  const locator = String(params?.[locatorParam] || '')
  const parsed = parseLocatorSafe(locator)
  if (!parsed.engineId || !parsed.type) return emptyResourcePresentation(locator)

  const engine = resourceEngineById(enginesById, parsed.engineId)
  const targetName = binding.mode === 'target'
    ? String(params?.[resourceBindingNameParam(parameter)] || '').trim()
    : ''
  const resourceName = targetName || String(parsed.path?.at(-1) || '')
  const resourceType = binding.mode === 'target'
    ? binding.type_values?.[parsed.type] || parsed.type
    : parsed.type
  const path = formatLocatorDisplayPath(locator, {
    engineType: engine?.engine_type,
    resourceType,
    appendedPath: targetName ? [targetName] : []
  })
  const engineName = String(engine?.name || '')
  const fullLabel = [...new Set([path, engineName].filter(Boolean))].join(' · ')

  return {
    configured: true,
    locator,
    engineId: parsed.engineId,
    engineName,
    engineType: String(engine?.engine_type || ''),
    resourceName,
    path,
    fullLabel,
    type: resourceType
  }
}

export function workflowExternalParameterSummaries(
  parameters = [],
  params = {},
  enginesById = {},
  { emptyLabel = '-' } = {}
) {
  const resourceParameters = parameters.filter(parameter => parameter.ui_type === 'resource_tree_picker')
  const managedNames = new Set(resourceParameters.flatMap(resourceBindingFieldNames))
  const summaries = []

  for (const parameter of parameters) {
    if (!parameter || isWorkflowInputParameter(parameter)) continue

    if (parameter.ui_type === 'resource_tree_picker') {
      const resource = workflowResourcePresentation(parameter, params, enginesById)
      summaries.push({
        key: parameter.name,
        label: parameter.display_name || parameter.name,
        kind: 'resource',
        configured: resource.configured,
        engineName: resource.engineName,
        resourceName: resource.resourceName,
        path: resource.path,
        value: resource.fullLabel || emptyLabel
      })
      continue
    }

    if (
      parameter.param_type === 'resource' ||
      parameter.param_type === 'ui' ||
      parameter.type === 'ui' ||
      managedNames.has(parameter.name) ||
      !parameterIsVisible(parameter, params)
    ) {
      continue
    }

    const configuredValue = parameterValue(parameter, params)
    if (!configuredValue.configured && !parameter.required) continue
    summaries.push({
      key: parameter.name,
      label: parameter.display_name || parameter.name,
      kind: 'value',
      configured: configuredValue.configured,
      value: configuredValue.configured ? formatParameterValue(configuredValue.value) : emptyLabel
    })
  }

  return summaries
}

function resourceBindingFieldNames(parameter) {
  const binding = getResourceBinding(parameter) || {}
  return [
    binding.locator_param,
    binding.parent_locator_param,
    binding.type_param,
    resourceBindingNameParam(parameter),
    resourceBindingGeometryColumnParam(parameter)
  ].filter(Boolean)
}

function resourceEngineById(enginesById, engineId) {
  if (enginesById instanceof Map) return enginesById.get(engineId) || enginesById.get(String(engineId))
  return enginesById?.[engineId] || enginesById?.[String(engineId)] || null
}

function parameterIsVisible(parameter, params) {
  return Object.entries(parameter.show_when || {}).every(([name, expected]) => {
    const current = params?.[name]
    return Array.isArray(expected) ? expected.includes(current) : current === expected
  })
}

function parameterValue(parameter, params) {
  const value = params?.[parameter.name]
  if (!isEmptyValue(value)) return { configured: true, value }
  if (!isEmptyValue(parameter.default)) return { configured: true, value: parameter.default }
  return { configured: false, value: null }
}

function isEmptyValue(value) {
  return value === undefined || value === null || value === ''
}

function formatParameterValue(value) {
  if (Array.isArray(value)) return value.map(item => String(item)).join(', ')
  if (value && typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function emptyResourcePresentation(locator = '') {
  return {
    configured: false,
    locator,
    engineId: 0,
    engineName: '',
    engineType: '',
    resourceName: '',
    path: '',
    fullLabel: '',
    type: ''
  }
}
