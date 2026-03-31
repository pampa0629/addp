/**
 * DAG JSON Schema 定义
 */

export const DAG_SCHEMA_URL = 'https://addp.io/schemas/dag/v1'

/**
 * DAG 节点状态枚举
 */
export const NodeStatus = {
  PENDING: 'pending',
  RUNNING: 'running',
  SUCCESS: 'success',
  FAILED: 'failed',
  SKIPPED: 'skipped'
}

/**
 * 验证 DAG JSON 格式
 */
export function validateDAGSchema(data) {
  if (!data || typeof data !== 'object') {
    return { valid: false, error: 'Invalid data format' }
  }

  if (data.$schema !== DAG_SCHEMA_URL) {
    return { valid: false, error: 'Invalid schema URL' }
  }

  if (!Array.isArray(data.nodes) || !Array.isArray(data.edges)) {
    return { valid: false, error: 'Missing nodes or edges array' }
  }

  return { valid: true }
}
