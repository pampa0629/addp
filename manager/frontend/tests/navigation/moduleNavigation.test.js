import { describe, expect, it, vi } from 'vitest'

import { navigateManagerRoute } from '@/utils/moduleNavigation'

describe('Manager module navigation', () => {
  it('uses the requested Vue Router history mode in standalone mode', async () => {
    const router = {
      resolve: vi.fn(() => ({ fullPath: '/data-explorer?locator=resource' })),
      push: vi.fn(),
      replace: vi.fn()
    }

    await navigateManagerRoute(router, { name: 'DataExplorer' }, { history: 'replace' })

    expect(router.replace).toHaveBeenCalledWith({ name: 'DataExplorer' })
    expect(router.push).not.toHaveBeenCalled()
  })
})
