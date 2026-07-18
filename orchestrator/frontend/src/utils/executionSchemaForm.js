const STRUCTURED_TYPES = new Set(['string', 'number', 'integer', 'boolean'])
const TEMPLATE_PATTERN = /^\s*\{\{[^{}]+\}\}\s*$/

function isObject(value) {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function schemaProperties(schema) {
  return isObject(schema?.properties) ? schema.properties : {}
}

function stringLength(value) {
  return Array.from(value).length
}

export function activeTaskCapabilityMetadata(taskCapabilities) {
  if (!Array.isArray(taskCapabilities)) {
    return []
  }
  return taskCapabilities
    .filter(item => typeof item?.type === 'string' && item.type.trim() !== '' && !item.deprecated)
    .map(item => ({
      type: item.type,
      editUrl: typeof item.edit_url === 'string' ? item.edit_url.trim() : '',
      executionSchema: item.execution_schema
    }))
}

export function executionParameterMode(schema, parameters = {}) {
  if (!isObject(schema) || schema.type !== 'object') {
    return 'json'
  }

  const properties = schemaProperties(schema)
  const fields = Object.entries(properties)
  if (fields.length === 0 && schema.additionalProperties === false) {
    return 'empty'
  }

  const isClosedScalarObject = schema.additionalProperties === false && fields.every(([, fieldSchema]) => {
    return isObject(fieldSchema) && STRUCTURED_TYPES.has(fieldSchema.type)
  })
  if (!isClosedScalarObject) {
    return 'json'
  }

  const declaredNames = new Set(fields.map(([name]) => name))
  if (Object.keys(parameters || {}).some(name => !declaredNames.has(name))) {
    return 'json'
  }

  const containsNonStringTemplate = fields.some(([name, fieldSchema]) => {
    const value = parameters?.[name]
    if (typeof value !== 'string' || !TEMPLATE_PATTERN.test(value)) {
      return false
    }
    return fieldSchema.type !== 'string' || Array.isArray(fieldSchema.enum)
  })

  return containsNonStringTemplate ? 'json' : 'structured'
}

export function executionSchemaFields(schema) {
  const required = new Set(Array.isArray(schema?.required) ? schema.required : [])
  return Object.entries(schemaProperties(schema)).map(([name, fieldSchema]) => ({
    name,
    schema: fieldSchema,
    required: required.has(name)
  }))
}

export function createParameterDraft(schema, parameters = {}) {
  const draft = {}
  for (const field of executionSchemaFields(schema)) {
    if (Object.prototype.hasOwnProperty.call(parameters, field.name)) {
      draft[field.name] = parameters[field.name]
    } else if (Object.prototype.hasOwnProperty.call(field.schema, 'default')) {
      draft[field.name] = field.schema.default
    } else {
      draft[field.name] = null
    }
  }
  return draft
}

export function serializeParameterDraft(schema, draft = {}) {
  const parameters = {}
  for (const field of executionSchemaFields(schema)) {
    const value = draft[field.name]
    if (value === null || value === undefined || (field.schema.type === 'string' && value === '' && !field.required)) {
      continue
    }
    parameters[field.name] = value
  }
  return parameters
}

export function validateParameterDraft(schema, draft = {}) {
  for (const field of executionSchemaFields(schema)) {
    const value = draft[field.name]
    const missing = value === null || value === undefined || (field.schema.type === 'string' && value === '')
    if (field.required && missing) {
      return { field: field.name, reason: 'required' }
    }
    if (missing) {
      continue
    }
    if (Array.isArray(field.schema.enum) && !field.schema.enum.includes(value)) {
      return { field: field.name, reason: 'enum' }
    }
    if (field.schema.type === 'string') {
      const length = stringLength(value)
      if (field.schema.minLength !== undefined && length < field.schema.minLength) {
        return { field: field.name, reason: 'minLength', limit: field.schema.minLength }
      }
      if (field.schema.maxLength !== undefined && length > field.schema.maxLength) {
        return { field: field.name, reason: 'maxLength', limit: field.schema.maxLength }
      }
    }
    if (field.schema.type === 'integer' && !Number.isInteger(value)) {
      return { field: field.name, reason: 'integer' }
    }
    if (['integer', 'number'].includes(field.schema.type)) {
      if (typeof value !== 'number' || !Number.isFinite(value)) {
        return { field: field.name, reason: 'number' }
      }
      if (field.schema.minimum !== undefined && value < field.schema.minimum) {
        return { field: field.name, reason: 'minimum', limit: field.schema.minimum }
      }
      if (field.schema.maximum !== undefined && value > field.schema.maximum) {
        return { field: field.name, reason: 'maximum', limit: field.schema.maximum }
      }
    }
    if (field.schema.type === 'boolean' && typeof value !== 'boolean') {
      return { field: field.name, reason: 'boolean' }
    }
  }
  return null
}
