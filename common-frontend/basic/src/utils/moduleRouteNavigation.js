import { syncConsoleRoute } from './taskOwnerUrl'

const MODULE_NAME_PATTERN = /^[a-z][a-z0-9-]*$/
const HISTORY_MODES = new Set(['push', 'replace'])

function normalizeModuleName(moduleName) {
  const normalized = String(moduleName || '').trim()
  if (!MODULE_NAME_PATTERN.test(normalized)) {
    throw new Error('console module name must use lowercase letters, digits, and hyphens')
  }
  return normalized
}

function normalizeModuleFullPath(fullPath) {
  const normalized = String(fullPath || '').trim()
  if (!normalized.startsWith('/') || normalized.startsWith('//')) {
    throw new Error('module route must be an absolute module-local route')
  }
  return normalized
}

function normalizeHistory(history) {
  const normalized = history || 'push'
  if (!HISTORY_MODES.has(normalized)) {
    throw new Error('module navigation history must be push or replace')
  }
  return normalized
}

function isIframeRuntime() {
  return typeof window !== 'undefined' && window.parent !== window
}

export function buildConsoleModuleRoute(moduleName, moduleFullPath) {
  const module = normalizeModuleName(moduleName)
  const fullPath = normalizeModuleFullPath(moduleFullPath)
  const modulePrefix = `/${module}`

  if (fullPath === modulePrefix || fullPath.startsWith(`${modulePrefix}/`)) {
    throw new Error('module route must not include the Console module prefix')
  }
  if (fullPath === '/') return modulePrefix
  if (fullPath.startsWith('/?') || fullPath.startsWith('/#')) {
    return `${modulePrefix}${fullPath.slice(1)}`
  }
  return `${modulePrefix}${fullPath}`
}

export async function navigateConsoleModuleRoute(router, moduleName, location, options = {}) {
  if (!router?.resolve || !router?.push || !router?.replace) {
    throw new Error('Vue Router instance is required')
  }

  const history = normalizeHistory(options.history)
  const target = router.resolve(location)
  normalizeModuleFullPath(target?.fullPath)

  if (!isIframeRuntime()) {
    return router[history](location)
  }

  // iframe 内只替换模块自身历史，再由 Console 写入唯一公开历史项。
  const navigationFailure = await router.replace(location)
  if (navigationFailure) return navigationFailure

  const currentFullPath = router.currentRoute?.value?.fullPath || target.fullPath
  return syncConsoleRoute(buildConsoleModuleRoute(moduleName, currentFullPath), {
    history,
    source: options.source,
    timeout: options.timeout
  })
}
