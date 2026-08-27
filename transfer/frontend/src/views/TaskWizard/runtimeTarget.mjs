export function queryStatementValid(language, statement) {
  const normalizedStatement = String(statement || '').trim()
  if (!normalizedStatement) return false
  if (String(language || '').trim().toLowerCase() !== 'mql') return true
  try {
    const command = JSON.parse(normalizedStatement)
    return command !== null && typeof command === 'object' && !Array.isArray(command)
  } catch {
    return false
  }
}

export function querySourceValid({ enabled, boundary, dataType, representation, language, statement, parametersValid }) {
  if (!enabled) return true
  return boundary === 'bounded' &&
    dataType === 'table' &&
    representation === 'native' &&
    String(language || '').trim() !== '' &&
    queryStatementValid(language, statement) &&
    parametersValid === true
}

export function withQuerySource(endpoint, { enabled, language, statement, parameters }) {
  if (!enabled) return endpoint
  const query = {
    language: String(language || '').trim().toLowerCase(),
    statement: String(statement || '').trim()
  }
  if (parameters && typeof parameters === 'object' && !Array.isArray(parameters) && Object.keys(parameters).length > 0) {
    query.parameters = parameters
  }
  return { ...endpoint, query }
}

export function buildRuntimeTableTarget() {
  return {
    binding: 'runtime',
    data_type: 'table',
    representation: 'native',
    policy: { apply_mode: 'append' }
  }
}
