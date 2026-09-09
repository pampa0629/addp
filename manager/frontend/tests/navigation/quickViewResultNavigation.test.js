import { beforeEach, describe, expect, it, vi } from 'vitest'

const { updatePreferredModeByLocator, navigateManagerRoute } = vi.hoisted(() => ({
  updatePreferredModeByLocator: vi.fn(),
  navigateManagerRoute: vi.fn()
}))

vi.mock('../../src/api/quickView.js', () => ({
  quickViewAPI: { updatePreferredModeByLocator }
}))

vi.mock('../../src/utils/moduleNavigation.js', () => ({ navigateManagerRoute }))

import { openQuickViewResult } from '../../src/utils/quickViewResultNavigation.js'

describe('quick-view result navigation', () => {
  beforeEach(() => {
    updatePreferredModeByLocator.mockReset()
    navigateManagerRoute.mockReset()
  })

  it('switches the source to quick-view mode before opening its canonical preview route', async () => {
    const router = {}
    const locator = 'addp://engine/3/path/model.fbx?type=file&item_id=8'

    await openQuickViewResult(router, locator)

    expect(updatePreferredModeByLocator).toHaveBeenCalledWith(locator, 'map_quick_view')
    expect(navigateManagerRoute).toHaveBeenCalledWith(router, {
      path: '/data-explorer',
      query: { locator }
    }, { history: 'push' })
    expect(updatePreferredModeByLocator.mock.invocationCallOrder[0])
      .toBeLessThan(navigateManagerRoute.mock.invocationCallOrder[0])
  })
})
