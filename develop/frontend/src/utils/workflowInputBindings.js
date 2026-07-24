const DATA_TYPES = new Set(['object', 'geodataframe', 'dataframe'])
const NUMERIC_TYPES = new Set(['integer', 'int', 'float', 'double', 'number'])
const WORKFLOW_INPUT_NAMES = new Set([
  'data',
  'input',
  'input_gdf',
  'input_df',
  'left_df',
  'right_df',
  'gdf_b',
  'df_b',
  'mask_gdf',
  'gdf_list',
  'df_list'
])

const normalizeType = (value) => String(value || '').trim().toLowerCase()
export function isPublicWorkflowParameter(parameter) {
  if (!parameter || typeof parameter.name !== 'string') return false
  return parameter.type !== 'ui' && parameter.param_type !== 'ui'
}

const isReferenceValue = (value) => (
  value &&
  typeof value === 'object' &&
  !Array.isArray(value) &&
  typeof value.$ref === 'string' &&
  value.$ref.trim() !== ''
)

export function isWorkflowInputParameter(parameter) {
  if (!parameter || typeof parameter.name !== 'string') return false

  const name = parameter.name.trim()
  const type = normalizeType(parameter.type)
  if (parameter.param_type === 'input') return true
  if (WORKFLOW_INPUT_NAMES.has(name)) return true
  return DATA_TYPES.has(type) && name.startsWith('input_')
}

export function areWorkflowTypesCompatible(sourceType, parameterType) {
  const source = normalizeType(sourceType)
  const target = normalizeType(parameterType)
  if (!source || !target) return false
  if (source === target) return true
  if (DATA_TYPES.has(source) && DATA_TYPES.has(target)) return true
  if (NUMERIC_TYPES.has(source) && NUMERIC_TYPES.has(target)) return true
  return false
}

function findInputParameter(parameters, sourceType, usedNames, currentParams) {
  const candidates = parameters.filter(param => (
    isPublicWorkflowParameter(param) &&
    param &&
    typeof param.name === 'string' &&
    !usedNames.has(param.name) &&
    areWorkflowTypesCompatible(sourceType, param.type)
  ))

  const workflowInputs = candidates.filter(isWorkflowInputParameter)
  const ordered = workflowInputs.length > 0 ? workflowInputs : candidates

  return (
    ordered.find(param => currentParams[param.name] === undefined || isReferenceValue(currentParams[param.name])) ||
    ordered[0] ||
    null
  )
}

function findExplicitInputParameter(parameters, edge, usedNames) {
  if (!edge.targetParam) return null
  const parameter = parameters.find(param => param?.name === edge.targetParam)
  if (
    !parameter ||
    !isWorkflowInputParameter(parameter) ||
    usedNames.has(parameter.name) ||
    !areWorkflowTypesCompatible(edge.sourceType, parameter.type)
  ) {
    throw new Error(`input parameter ${edge.targetParam} is not compatible with source ${edge.sourceId}`)
  }
  return parameter
}

export function applyWorkflowInputRefs({ params = {}, parameters = [], inputEdges = [] }) {
  const nextParams = { ...params }
  if (!Array.isArray(inputEdges) || inputEdges.length === 0) {
    return nextParams
  }
  if (!Array.isArray(parameters)) {
    throw new Error('operator parameters must be an array')
  }

  const usedNames = new Set()

  inputEdges.forEach(edge => {
    const parameter = findExplicitInputParameter(parameters, edge, usedNames) ||
      findInputParameter(parameters, edge.sourceType, usedNames, nextParams)
    if (!parameter) {
      throw new Error(`no compatible input parameter for source ${edge.sourceId}`)
    }

    const ref = { $ref: edge.sourceId }
    if (edge.sourcePort && edge.sourcePort !== 'default') {
      ref.port = edge.sourcePort
    }

    nextParams[parameter.name] = ref
    usedNames.add(parameter.name)
  })

  return nextParams
}
