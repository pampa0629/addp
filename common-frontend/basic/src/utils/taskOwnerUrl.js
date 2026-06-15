import { requestConsoleBridge } from './consoleBridge'

export const CONSOLE_NAVIGATION_CHANNEL = 'console-navigation'

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
    const { protocol, hostname, port } = window.location
    if (port && port !== '5170' && /^51(7[3-9]|8[0-7])$/.test(port)) {
      return `${protocol}//${hostname}:5170`
    }
    return window.location.origin
  }
  return ''
}

function normalizeConsoleRoute(route) {
  if (!hasValue(route)) return ''
  const normalized = String(route).trim()
  return normalized.startsWith('/') ? normalized : `/${normalized}`
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

export function resolveConsoleRouteUrl(route, options = {}) {
  const normalizedRoute = normalizeConsoleRoute(route)
  if (!normalizedRoute) return ''
  return `${consoleOrigin(options)}${normalizedRoute}`
}

export function buildTaskOwnerUrl(rawUrl, replacements = {}, options = {}) {
  return resolveTaskOwnerUrl(fillTaskOwnerUrlTemplate(rawUrl, replacements), options)
}

export function buildMonitorExecutionRoute(executionId) {
  if (!hasValue(executionId)) return ''
  return `/monitor/executions?execution_id=${encodeURIComponent(String(executionId))}`
}

export function buildMonitorExecutionUrl(executionId, options = {}) {
  return resolveConsoleRouteUrl(buildMonitorExecutionRoute(executionId), options)
}

export async function openConsoleRoute(route, options = {}) {
  const normalizedRoute = normalizeConsoleRoute(route)
  if (!normalizedRoute) return false

  const {
    source = 'addp-module',
    timeout = 1500,
    target = '_self',
    windowFeatures = 'noopener,noreferrer'
  } = options

  if (typeof window !== 'undefined' && window.parent !== window) {
    try {
      await requestConsoleBridge(
        CONSOLE_NAVIGATION_CHANNEL,
        { route: normalizedRoute },
        { source, timeout }
      )
      return true
    } catch {
      // 独立模块或旧 Console 中没有该 bridge 时，回退到直接打开 Console 路由。
    }
  }

  const url = resolveConsoleRouteUrl(normalizedRoute, options)
  if (!url || typeof window === 'undefined') return false

  if (target && target !== '_self') {
    window.open(url, target, windowFeatures)
  } else {
    window.location.assign(url)
  }
  return true
}

export function openMonitorExecution(executionId, options = {}) {
  return openConsoleRoute(buildMonitorExecutionRoute(executionId), {
    target: '_blank',
    ...options
  })
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
