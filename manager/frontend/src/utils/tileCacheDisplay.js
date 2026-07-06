export function executionStatusFromExecution(execution) {
  return String(execution?.status || execution?.Status || '').trim()
}

export function executionRunAtFromExecution(execution) {
  return execution?.completed_at ||
    execution?.completedAt ||
    execution?.CompletedAt ||
    execution?.started_at ||
    execution?.startedAt ||
    execution?.StartedAt ||
    execution?.created_at ||
    execution?.createdAt ||
    execution?.CreatedAt ||
    ''
}

export function executionIDFromExecution(execution) {
  return String(execution?.execution_id || execution?.executionID || execution?.ExecutionID || '').trim()
}

export function lastExecutionStatus(task) {
  return String(
    task?.last_execution_status ||
      task?.lastExecutionStatus ||
      task?.execution_status ||
      task?.last_execution?.status ||
      ''
  ).trim()
}

export function parseTileCacheStorageRef(storageRef) {
  if (!storageRef || typeof storageRef !== 'string') return null
  try {
    const parsed = JSON.parse(storageRef)
    return parsed && typeof parsed === 'object' ? parsed : null
  } catch {
    return null
  }
}

export function resourceTextFromLocator(locator, parseLocator) {
  const parsed = parseLocator?.(String(locator || '').trim())
  if (!parsed?.path?.length) return ''
  const type = String(parsed.type || '').toLowerCase()
  const separator = ['object', 'file', 'directory', 'prefix', 'bucket'].includes(type) ? '/' : '.'
  return parsed.path.join(separator)
}

export function taskResource(task, parseLocator) {
  const target = task?.target || task?.config?.target || {}
  if (target.schema && target.table) return `${target.schema}.${target.table}`
  return resourceTextFromLocator(target.locator, parseLocator) || '-'
}

export function resultLocatorInfo(result, parseLocator) {
  const locator = String(result?.locator || '').trim()
  if (!locator) return null
  const parsedLocator = parseLocator?.(locator)
  if (!parsedLocator) return null
  return {
    engineId: Number(parsedLocator.engineId || 0),
    path: parsedLocator.path?.length ? parsedLocator.path.join('.') : ''
  }
}

export function storageLocationKey(storageRef) {
  const parsed = parseTileCacheStorageRef(storageRef)
  if (parsed?.provider === 'addp_object_storage' || parsed?.object_prefix) {
    return 'platformObjectStorage'
  }
  return 'externalStorage'
}

export function executionStatusTagType(status) {
  if (status === 'success') return 'success'
  if (status === 'failed' || status === 'timeout') return 'danger'
  if (status === 'running' || status === 'pending') return 'warning'
  return 'info'
}

export function resultStatusTagType(status) {
  if (status === 'ready') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'generating') return 'warning'
  return 'info'
}
