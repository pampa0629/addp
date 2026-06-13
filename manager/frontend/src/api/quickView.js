import request from './client'

export const quickViewAPI = {
  getQuickViewCapabilityByLocator(locator) {
    return request.get('/manager/quick-view/capability', {
      params: { locator }
    })
  },

  updatePreferredModeByLocator(locator, preferredMode) {
    return request.patch(
      '/manager/quick-view/preferred-mode',
      { locator, preferred_mode: preferredMode }
    )
  }
}
