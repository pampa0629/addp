import { sortEntriesByOrder } from '../../../../common-frontend/basic/src/utils/executionParameterPresentation.js'

const OUTPUT_TEMPLATE = /^\s*\{\{([^{}]+)\.outputs\.([^{}]+)\}\}\s*$/

export function executionInputPorts(contract) {
  const properties = contract?.input_schema?.properties || {}
  const defaults = contract?.input_defaults || {}
  const uiSchema = contract?.input_ui_schema || {}
  const ports = []

  sortEntriesByOrder(properties, uiSchema).forEach(([name, schema]) => {
    const ui = uiSchema[name] || {}
    if (schema?.type === 'object' && schema.properties && ui.control === 'group') {
      sortEntriesByOrder(schema.properties, ui.fields).forEach(([fieldName, fieldSchema]) => {
        const fieldUI = ui.fields?.[fieldName] || {}
        ports.push(inputPort({
          name: fieldName,
          schema: fieldSchema,
          ui: fieldUI,
          path: [name, fieldName],
          defaultValue: defaults?.[name]?.[fieldName],
          groupTitle: ui.title || schema.title || name
        }))
      })
      return
    }
    ports.push(inputPort({
      name,
      schema,
      ui,
      path: [name],
      defaultValue: defaults?.[name]
    }))
  })

  return ports
}

export function executionOutputPorts(contract) {
  return flattenOutputs(contract?.output_schema)
}

export function outputTemplate(stepId, outputPath) {
  return `{{${stepId}.outputs.${outputPath.join('.')}}}`
}

export function parseOutputTemplate(value) {
  if (typeof value !== 'string') return null
  const match = value.match(OUTPUT_TEMPLATE)
  if (!match) return null
  return { stepId: match[1], outputPath: match[2].split('.') }
}

export function parameterBindings(parameters, inputPorts) {
  return (inputPorts || []).flatMap(port => {
    const parsed = parseOutputTemplate(getPath(parameters, port.bindingPath))
    return parsed ? [{ ...parsed, inputPort: port }] : []
  })
}

export function setParameterBinding(parameters, inputPort, template) {
  const next = cloneValue(parameters) || {}
  if (inputPort.resource) {
    const current = getPath(next, inputPort.path)
    setPath(next, inputPort.path, {
      ...(cloneValue(inputPort.defaultValue) || {}),
      ...(current && typeof current === 'object' && !Array.isArray(current) ? current : {}),
      [inputPort.bindingPath.at(-1)]: template
    })
    return next
  }
  setPath(next, inputPort.path, template)
  return next
}

export function clearParameterBinding(parameters, inputPort) {
  const next = cloneValue(parameters) || {}
  deletePath(next, inputPort.path)
  return next
}

export function arePortTypesCompatible(outputPort, inputPort) {
  return Boolean(outputPort?.type && inputPort?.type && outputPort.type === inputPort.type)
}

function inputPort({ name, schema, ui, path, defaultValue, groupTitle = '' }) {
  const resource = ui?.control === 'resource_tree_picker'
  const locatorName = ui?.resource_binding?.mode === 'target' ? 'parent_locator' : 'locator'
  const title = ui?.display_name || schema?.title || name
  return {
    name: path.join('.'),
    label: groupTitle ? `${groupTitle} / ${title}` : title,
    type: resource ? 'string' : schema?.type,
    path,
    bindingPath: resource ? [...path, locatorName] : path,
    defaultValue,
    resource
  }
}

function flattenOutputs(schema, path = [], labels = []) {
  if (!schema || typeof schema !== 'object') return []
  const properties = schema.properties && typeof schema.properties === 'object'
    ? schema.properties
    : null
  if (!properties || Object.keys(properties).length === 0) {
    return path.length > 0
      ? [{ name: path.join('.'), path, type: schema.type, label: labels.join(' / ') || path.join('.') }]
      : []
  }

  const locator = properties.locator
  if (locator?.type === 'string') {
    return [{
      name: [...path, 'locator'].join('.'),
      path: [...path, 'locator'],
      type: 'string',
      label: labels.join(' / ') || schema.title || path.at(-1) || 'resource',
      resource: true
    }]
  }

  return Object.entries(properties).flatMap(([name, child]) => (
    flattenOutputs(child, [...path, name], [...labels, child.title || name])
  ))
}

function getPath(value, path) {
  return path.reduce((current, name) => current?.[name], value)
}

function setPath(value, path, nextValue) {
  let current = value
  path.slice(0, -1).forEach(name => {
    if (!current[name] || typeof current[name] !== 'object' || Array.isArray(current[name])) current[name] = {}
    current = current[name]
  })
  current[path.at(-1)] = nextValue
}

function deletePath(value, path) {
  const parents = []
  let current = value
  for (const name of path.slice(0, -1)) {
    if (!current[name] || typeof current[name] !== 'object') return
    parents.push([current, name])
    current = current[name]
  }
  delete current[path.at(-1)]
  for (let index = parents.length - 1; index >= 0; index -= 1) {
    const [parent, name] = parents[index]
    if (Object.keys(parent[name]).length === 0) delete parent[name]
  }
}

function cloneValue(value) {
  return value === undefined ? undefined : JSON.parse(JSON.stringify(value))
}
