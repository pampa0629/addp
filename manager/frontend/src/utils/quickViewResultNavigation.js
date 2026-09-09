import { quickViewAPI } from '@/api/quickView'
import { navigateManagerRoute } from '@/utils/moduleNavigation'

export function quickViewResultPreferredMode(taskType) {
  return taskType === 'pptx_pdf_generation' ? 'basic_preview' : 'map_quick_view'
}

export async function openQuickViewResult(router, locator, taskType) {
  const normalizedLocator = String(locator || '').trim()
  if (!normalizedLocator) return false
  await quickViewAPI.updatePreferredModeByLocator(normalizedLocator, quickViewResultPreferredMode(taskType))
  await navigateManagerRoute(router, {
    path: '/data-explorer',
    query: { locator: normalizedLocator }
  }, { history: 'push' })
  return true
}
