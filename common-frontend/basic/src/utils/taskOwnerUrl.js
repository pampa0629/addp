import { requestConsoleBridge } from './consoleBridge'

export const CONSOLE_NAVIGATION_CHANNEL = 'console-navigation'
const CONSOLE_NAVIGATION_HISTORY = new Set(['push', 'replace'])

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

export function buildConsoleNavigationRequest(route, options = {}) {
  const normalizedRoute = normalizeConsoleRoute(route)
  if (!normalizedRoute || normalizedRoute.startsWith('//')) {
    throw new Error('console route must be an absolute application route')
  }

  const history = options.history || 'push'
  if (!CONSOLE_NAVIGATION_HISTORY.has(history)) {
    throw new Error('console navigation history must be push or replace')
  }

  return {
    route: normalizedRoute,
    history,
    synchronized: options.synchronized === true
  }
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

export function buildMonitorExecutionsRoute(params = {}) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (hasValue(value)) {
      query.set(key, String(value))
    }
  }
  const queryString = query.toString()
  return queryString ? `/monitor/executions?${queryString}` : '/monitor/executions'
}

export function buildMonitorExecutionUrl(executionId, options = {}) {
  return resolveConsoleRouteUrl(buildMonitorExecutionRoute(executionId), options)
}

export async function openConsoleRoute(route, options = {}) {
  const {
    source = 'addp-module',
    timeout = 1500,
    target = '_self',
    windowFeatures = 'noopener,noreferrer',
    history = 'push'
  } = options
  const request = buildConsoleNavigationRequest(route, { history })

  if (typeof window !== 'undefined' && window.parent !== window) {
    await requestConsoleBridge(
      CONSOLE_NAVIGATION_CHANNEL,
      request,
      { source, timeout }
    )
    return true
  }

  const url = resolveConsoleRouteUrl(request.route, options)
  if (!url || typeof window === 'undefined') return false

  if (target && target !== '_self') {
    window.open(url, target, windowFeatures)
  } else {
    window.location.assign(url)
  }
  return true
}

export async function syncConsoleRoute(route, options = {}) {
  if (typeof window === 'undefined' || window.parent === window) {
    return false
  }

  const {
    source = 'addp-module',
    timeout = 1500,
    history = 'replace'
  } = options
  await requestConsoleBridge(
    CONSOLE_NAVIGATION_CHANNEL,
    buildConsoleNavigationRequest(route, { history, synchronized: true }),
    { source, timeout }
  )
  return true
}

export function openMonitorExecution(executionId, options = {}) {
  return openConsoleRoute(buildMonitorExecutionRoute(executionId), {
    target: '_blank',
    ...options
  })
}

export function openMonitorExecutions(params = {}, options = {}) {
  return openConsoleRoute(buildMonitorExecutionsRoute(params), {
    target: '_blank',
    ...options
  })
}

export function findTaskTypeCapability(provider, taskType) {
  if (!provider || !hasValue(taskType)) return null

  const capabilities = parseCapabilities(provider.capabilities)
  const taskCapabilities = Array.isArray(capabilities.task_capabilities) ? capabilities.task_capabilities : []
  return taskCapabilities.find(item => item?.type === taskType && !item.deprecated) || null
}

export function findTaskProvider(providers, moduleName) {
  if (!Array.isArray(providers) || !hasValue(moduleName)) return null
  return providers.find(provider => provider?.module_name === moduleName) || null
}

export function resolveTaskTypeDisplayName(providers, moduleName, taskType) {
  if (!hasValue(taskType)) return ''

  const provider = findTaskProvider(providers, moduleName)
  const capability = findTaskTypeCapability(provider, taskType)
  return capability?.display_name || capability?.type || ''
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
