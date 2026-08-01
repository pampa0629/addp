const DEVELOP_TASK_EDITOR_PATHS = Object.freeze({
  query: '/sql',
  workflow: '/workflow',
  script: '/notebook'
})

function normalizeDevType(devType) {
  const normalized = String(devType || '').trim().toLowerCase()
  if (!DEVELOP_TASK_EDITOR_PATHS[normalized]) {
    throw new Error(`unsupported Develop task type: ${normalized || '-'}`)
  }
  return normalized
}

function firstQueryValue(value) {
  return Array.isArray(value) ? value[0] || '' : value || ''
}

export function normalizeDevelopTaskID(taskID) {
  const normalized = String(firstQueryValue(taskID)).trim()
  if (!normalized) return ''
  if (!/^[1-9]\d*$/.test(normalized)) {
    throw new Error('Develop task id must be a positive integer')
  }
  return normalized
}

export function developTaskIDFromRoute(route) {
  return normalizeDevelopTaskID(route?.query?.id)
}

export function buildDevelopTaskPageLocation(devType) {
  const normalizedType = normalizeDevType(devType)
  return { path: DEVELOP_TASK_EDITOR_PATHS[normalizedType] }
}

export function buildDevelopTaskEditorLocation(devType, taskID = '') {
  const normalizedType = normalizeDevType(devType)
  const normalizedID = normalizeDevelopTaskID(taskID)
  return {
    ...buildDevelopTaskPageLocation(normalizedType),
    query: normalizedID
      ? { action: 'edit', id: normalizedID }
      : { action: 'create' }
  }
}
