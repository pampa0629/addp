export function createMongoStructureQuery(collection = '') {
  return {
    collection: cleanText(collection),
    unwind: {
      enabled: false,
      path: '',
      includeIndex: false,
      indexOutput: ''
    },
    projections: []
  }
}

export function defaultMongoOutputName(sourcePath) {
  return cleanText(sourcePath)
    .replace(/^\$+/, '')
    .split('.')
    .filter(Boolean)
    .join('__')
}

export function defaultMongoIndexOutput(arrayPath) {
  const prefix = defaultMongoOutputName(arrayPath)
  return prefix ? `${prefix}__index` : ''
}

export function createMongoPathProjection(source, sourceFields = [], existingProjections = []) {
  const cleanSource = cleanText(source).replace(/^\$+/, '')
  const sourceField = findSourceField(sourceFields, cleanSource)
  return {
    source: cleanSource,
    output: uniqueOutputName(defaultMongoOutputName(cleanSource), existingProjections.map(item => item?.output)),
    nullable: sourceField?.primary_key === true ? false : sourceField?.nullable !== false
  }
}

export function compileMongoStructureQuery(model) {
  const normalized = normalizeModel(model)
  const issues = validateNormalizedModel(normalized)
  if (issues.length > 0) {
    const error = new Error('invalid MongoDB structure query')
    error.issues = issues
    throw error
  }

  const pipeline = []
  if (normalized.unwind.enabled) {
    const unwind = {
      path: `$${normalized.unwind.path}`,
      preserveNullAndEmptyArrays: false
    }
    if (normalized.unwind.includeIndex) {
      unwind.includeArrayIndex = normalized.unwind.indexOutput
    }
    pipeline.push({ $unwind: unwind })
  }

  const project = {}
  if (!normalized.projections.some(item => item.output === '_id')) {
    project._id = 0
  }
  normalized.projections.forEach(projection => {
    const reference = `$${projection.source}`
    project[projection.output] = projection.nullable
      ? { $ifNull: [reference, null] }
      : reference
  })
  if (normalized.unwind.includeIndex) {
    project[normalized.unwind.indexOutput] = 1
  }
  pipeline.push({ $project: project })

  return JSON.stringify({ aggregate: normalized.collection, pipeline })
}

export function parseMongoStructureQuery(statement) {
  let command
  try {
    command = JSON.parse(String(statement || '').trim())
  } catch {
    return unsupported('invalid_json')
  }
  if (!isPlainObject(command) || !hasExactKeys(command, ['aggregate', 'pipeline'])) {
    return unsupported('unsupported_command')
  }
  if (!cleanText(command.aggregate) || !Array.isArray(command.pipeline)) {
    return unsupported('unsupported_command')
  }

  const model = createMongoStructureQuery(command.aggregate)
  let stageIndex = 0

  if (stageHasOnlyOperator(command.pipeline[stageIndex], '$unwind')) {
    const unwind = command.pipeline[stageIndex].$unwind
    if (!isPlainObject(unwind) || !hasOnlyKeys(unwind, ['path', 'includeArrayIndex', 'preserveNullAndEmptyArrays'])) {
      return unsupported('unsupported_unwind')
    }
    if (typeof unwind.path !== 'string' || !unwind.path.startsWith('$') || !isFieldPath(unwind.path.slice(1))) {
      return unsupported('unsupported_unwind')
    }
    if (unwind.preserveNullAndEmptyArrays !== false) {
      return unsupported('unsupported_unwind')
    }
    const path = unwind.path.slice(1)
    const indexOutput = cleanText(unwind.includeArrayIndex)
    if (indexOutput && indexOutput !== defaultMongoIndexOutput(path)) {
      return unsupported('noncanonical_output')
    }
    model.unwind = {
      enabled: true,
      path,
      includeIndex: !!indexOutput,
      indexOutput
    }
    stageIndex += 1
  }

  if (!stageHasOnlyOperator(command.pipeline[stageIndex], '$project')) {
    return unsupported('project_required')
  }
  const project = command.pipeline[stageIndex].$project
  if (!isPlainObject(project)) return unsupported('unsupported_project')
  for (const [output, expression] of Object.entries(project)) {
    if (output === '_id' && expression === 0) continue
    if (model.unwind.includeIndex && output === model.unwind.indexOutput && expression === 1) continue
    const parsed = parsePathProjection(output, expression)
    if (!parsed) return unsupported('unsupported_project')
    const reservedOutputs = [model.unwind.includeIndex ? model.unwind.indexOutput : '', ...model.projections.map(item => item.output)]
    if (parsed.output !== uniqueOutputName(defaultMongoOutputName(parsed.source), reservedOutputs)) {
      return unsupported('noncanonical_output')
    }
    model.projections.push(parsed)
  }
  stageIndex += 1

  if (stageIndex !== command.pipeline.length) {
    return unsupported('unsupported_stage')
  }

  const issues = validateNormalizedModel(model)
  if (issues.length > 0) {
    return { supported: false, reason: 'invalid_structure', issues }
  }
  return { supported: true, model }
}

export function mongoStructureOutputFields(model, sourceFields = []) {
  const normalized = normalizeModel(model)
  const fields = normalized.projections.map(projection => {
    const source = findSourceField(sourceFields, projection.source) || {}
    return {
      ...source,
      name: projection.output,
      nullable: projection.nullable || source.nullable === true,
      source_path: projection.source,
      source_role: mongoProjectionSourceRole(projection.source, normalized)
    }
  })
  if (normalized.unwind.includeIndex) {
    const indexField = {
      name: normalized.unwind.indexOutput,
      type: 'bigint',
      native_type: 'int64',
      nullable: false,
      source_path: normalized.unwind.path,
      source_role: 'array_index'
    }
    const elementIndex = fields.findIndex(field => field.source_role === 'array_element_field')
    fields.splice(elementIndex >= 0 ? elementIndex : fields.length, 0, indexField)
  }
  return fields
}

export function isMongoProjectionLeafField(field) {
  const type = cleanText(field?.type || field?.native_type).toLowerCase()
  return !['array', 'json', 'object'].includes(type)
}

export function isMongoParentLeafField(field, sourceFields = []) {
  const name = cleanText(field?.name)
  if (!name || name === '_id' || !isMongoProjectionLeafField(field)) return false
  return !mongoArrayPaths(sourceFields).some(arrayPath => name.startsWith(`${arrayPath}.`))
}

export function isMongoArrayElementLeafField(field, arrayPath, sourceFields = []) {
  const name = cleanText(field?.name)
  const selectedArray = cleanText(arrayPath)
  if (!name || !selectedArray || !isMongoProjectionLeafField(field)) return false
  if (!name.startsWith(`${selectedArray}.`)) return false
  return !mongoArrayPaths(sourceFields).some(candidate => (
    candidate !== selectedArray && name.startsWith(`${candidate}.`)
  ))
}

export function validateMongoStructureQuery(model) {
  return validateNormalizedModel(normalizeModel(model))
}

function normalizeModel(model = {}) {
  const unwind = model.unwind || {}
  return {
    collection: cleanText(model.collection),
    unwind: {
      enabled: unwind.enabled === true,
      path: cleanText(unwind.path).replace(/^\$+/, ''),
      includeIndex: unwind.includeIndex === true,
      indexOutput: cleanText(unwind.indexOutput)
    },
    projections: (Array.isArray(model.projections) ? model.projections : []).map(projection => ({
      source: cleanText(projection?.source).replace(/^\$+/, ''),
      output: cleanText(projection?.output),
      nullable: projection?.nullable === true
    }))
  }
}

function validateNormalizedModel(model) {
  const issues = []
  if (!model.collection) issues.push(issue('collection', 'collection_required'))
  if (model.unwind.enabled && !isFieldPath(model.unwind.path)) {
    issues.push(issue('unwind.path', 'unwind_path_required'))
  }
  if (!model.unwind.enabled && model.unwind.includeIndex) {
    issues.push(issue('unwind.includeIndex', 'array_index_requires_unwind'))
  }
  if (model.unwind.includeIndex && model.unwind.indexOutput !== defaultMongoIndexOutput(model.unwind.path)) {
    issues.push(issue('unwind.indexOutput', 'array_index_output_invalid'))
  }
  if (model.projections.length === 0) issues.push(issue('projections', 'projection_required'))

  const sources = new Set()
  const outputs = new Set(model.unwind.includeIndex ? [model.unwind.indexOutput.toLowerCase()] : [])
  model.projections.forEach((projection, index) => {
    if (!isFieldPath(projection.source)) issues.push(issue(`projections.${index}.source`, 'field_path_required'))
    const sourceKey = projection.source.toLowerCase()
    if (sourceKey && sources.has(sourceKey)) issues.push(issue(`projections.${index}.source`, 'source_field_duplicate'))
    sources.add(sourceKey)
    const expectedOutput = uniqueOutputName(defaultMongoOutputName(projection.source), [...outputs])
    if (!isOutputField(projection.output) || projection.output !== expectedOutput) {
      issues.push(issue(`projections.${index}.output`, 'output_field_invalid'))
    }
    const outputKey = projection.output.toLowerCase()
    if (outputKey && outputs.has(outputKey)) issues.push(issue(`projections.${index}.output`, 'output_field_duplicate'))
    outputs.add(outputKey)
  })
  if (!sources.has('_id')) issues.push(issue('projections', 'identifier_required'))
  return issues
}

function parsePathProjection(output, expression) {
  if (!isOutputField(output)) return null
  if (typeof expression === 'string' && expression.startsWith('$') && isFieldPath(expression.slice(1))) {
    return { output, source: expression.slice(1), nullable: false }
  }
  if (!isNullablePathExpression(expression)) return null
  return { output, source: expression.$ifNull[0].slice(1), nullable: true }
}

function isNullablePathExpression(value) {
  return isPlainObject(value) &&
    hasExactKeys(value, ['$ifNull']) &&
    Array.isArray(value.$ifNull) &&
    value.$ifNull.length === 2 &&
    typeof value.$ifNull[0] === 'string' &&
    value.$ifNull[0].startsWith('$') &&
    isFieldPath(value.$ifNull[0].slice(1)) &&
    value.$ifNull[1] === null
}

function stageHasOnlyOperator(stage, operator) {
  return isPlainObject(stage) && hasExactKeys(stage, [operator])
}

function hasExactKeys(value, expected) {
  const keys = Object.keys(value)
  return keys.length === expected.length && keys.every(key => expected.includes(key))
}

function hasOnlyKeys(value, allowed) {
  return Object.keys(value).every(key => allowed.includes(key))
}

function findSourceField(sourceFields, name) {
  const key = cleanText(name).toLowerCase()
  return (Array.isArray(sourceFields) ? sourceFields : []).find(field => cleanText(field?.name).toLowerCase() === key)
}

function mongoProjectionSourceRole(source, model) {
  if (source === '_id') return model.unwind.enabled ? 'parent_identifier' : 'record_identifier'
  if (!model.unwind.enabled) return 'selected_field'
  return source.startsWith(`${model.unwind.path}.`) ? 'array_element_field' : 'parent_field'
}

function mongoArrayPaths(sourceFields) {
  return (Array.isArray(sourceFields) ? sourceFields : [])
    .filter(field => cleanText(field?.type || field?.native_type).toLowerCase() === 'array')
    .map(field => cleanText(field?.name))
    .filter(Boolean)
}

function uniqueOutputName(base, existingOutputs = []) {
  const used = new Set(existingOutputs.map(value => cleanText(value).toLowerCase()).filter(Boolean))
  if (!used.has(base.toLowerCase())) return base
  let suffix = 2
  while (used.has(`${base}__${suffix}`.toLowerCase())) suffix += 1
  return `${base}__${suffix}`
}

function isPlainObject(value) {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function isFieldPath(value) {
  const text = cleanText(value)
  return text !== '' && text.split('.').every(segment => segment !== '' && !segment.startsWith('$'))
}

function isOutputField(value) {
  const text = cleanText(value)
  return text !== '' && !text.includes('.') && !text.startsWith('$')
}

function cleanText(value) {
  return typeof value === 'string' ? value.trim() : ''
}

function issue(path, code) {
  return { path, code }
}

function unsupported(reason) {
  return { supported: false, reason }
}
