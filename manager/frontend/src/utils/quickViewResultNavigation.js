import { quickViewAPI } from '@/api/quickView'
import { navigateManagerRoute } from '@/utils/moduleNavigation'

export async function openQuickViewResult(router, locator) {
  const normalizedLocator = String(locator || '').trim()
  if (!normalizedLocator) return false
  await quickViewAPI.updatePreferredModeByLocator(normalizedLocator, 'map_quick_view')
  await navigateManagerRoute(router, {
    path: '/data-explorer',
    query: { locator: normalizedLocator }
  }, { history: 'push' })
  return true
}
