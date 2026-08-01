import { navigateConsoleModuleRoute } from '@addp/common-frontend'
import { buildDevelopTaskEditorLocation } from './developTaskRoute'

export function navigateDevelopRoute(router, location, options = {}) {
  return navigateConsoleModuleRoute(router, 'develop', location, options)
}

export function navigateDevelopTaskEditor(router, devType, taskID = '', options = {}) {
  return navigateDevelopRoute(
    router,
    buildDevelopTaskEditorLocation(devType, taskID),
    options
  )
}
