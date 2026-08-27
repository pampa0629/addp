const QUERY_OPERATION_PATH = /^\/api\/query\/[^/?#]+\/query$/

export function assertQueryOperation(operation) {
  if (!operation || operation.key !== 'query' || operation.method !== 'POST' ||
      operation.input_kind !== 'structured_query' || !QUERY_OPERATION_PATH.test(operation.path || '')) {
    throw new Error('invalid Service query operation')
  }
  return operation
}
