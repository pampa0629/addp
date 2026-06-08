function hasValue(value) {
  return value !== null && value !== undefined && String(value).trim() !== ''
}

function parseCapabilities(capabilities) {
  if (!capabilities) return {}
  if (typeof capabilities === 'object') return capabilities
  try {
    return JSON.parse(capabilities)
  } catch {
    return {}
  }
}

function consoleOrigin(options = {}) {
  if (hasValue(options.consoleOrigin)) {
    return String(options.consoleOrigin).replace(/\/$/, '')
  }
  if (typeof window !== 'undefined' && window.location?.origin) {
    return window.location.origin
  }
  return ''
}

export function fillTaskOwnerUrlTemplate(rawUrl, replacements = {}) {
  if (!hasValue(rawUrl)) return ''

  const taskID = hasValue(replacements.taskId)
    ? encodeURIComponent(String(replacements.taskId))
    : ''
  const graphID = hasValue(replacements.graphId)
    ? encodeURIComponent(String(replacements.graphId))
    : ''

  return String(rawUrl)
    .replaceAll(':id', taskID)
    .replaceAll('{id}', taskID)
    .replaceAll(':task_id', taskID)
    .replaceAll('{task_id}', taskID)
    .replaceAll(':graph_id', graphID)
    .replaceAll('{graph_id}', graphID)
}

export function resolveTaskOwnerUrl(rawUrl, options = {}) {
  if (!hasValue(rawUrl)) return ''

  const url = String(rawUrl).trim()
  if (/^https?:\/\//i.test(url)) {
    return url
  }
  if (url.startsWith('/')) {
    return `${consoleOrigin(options)}${url}`
  }
  return url
}

export function buildTaskOwnerUrl(rawUrl, replacements = {}, options = {}) {
  return resolveTaskOwnerUrl(fillTaskOwnerUrlTemplate(rawUrl, replacements), options)
}

export function findTaskTypeCapability(provider, taskType) {
  if (!provider || !hasValue(taskType)) return null

  const capabilities = parseCapabilities(provider.capabilities)
  const taskTypes = Array.isArray(capabilities.task_types) ? capabilities.task_types : []
  return taskTypes.find(item => item?.type === taskType && !item.deprecated) || null
}

export function findTaskProvider(providers, moduleName) {
  if (!Array.isArray(providers) || !hasValue(moduleName)) return null
  return providers.find(provider => provider?.module_name === moduleName) || null
}

export function buildTaskEditUrlFromProviders(providers, execution, options = {}) {
  if (!execution || !hasValue(execution.source_task_id)) return ''

  const provider = findTaskProvider(providers, execution.module)
  const taskType = findTaskTypeCapability(provider, execution.task_type)
  if (!taskType?.edit_url) return ''

  return buildTaskOwnerUrl(
    taskType.edit_url,
    {
      taskId: execution.source_task_id,
      graphId: execution.execution_config?.graph_id || execution.metadata?.graph_id
    },
    options
  )
}
