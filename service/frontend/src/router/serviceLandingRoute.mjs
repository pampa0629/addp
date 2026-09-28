import { resolveModuleLandingRoute } from '../../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'

export function resolveServiceLandingRoute(contextType, permissions) {
  return resolveModuleLandingRoute('/service', ['/query-services', '/services'], contextType, permissions)
}
