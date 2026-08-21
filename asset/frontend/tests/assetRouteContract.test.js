import { readFileSync } from 'node:fs'
import { describe, expect, it, vi } from 'vitest'

import { navigateAssetRoute } from '@/utils/moduleNavigation'

const readSource = path => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8')

describe('Asset public route contract', () => {
  it('delegates standalone navigation to the requested Vue Router history mode', async () => {
    const router = {
      resolve: vi.fn(() => ({ fullPath: '/assets?catalog_id=12' })),
      push: vi.fn(),
      replace: vi.fn()
    }

    await navigateAssetRoute(
      router,
      { path: '/assets', query: { catalog_id: '12' } },
      { history: 'replace' }
    )

    expect(router.replace).toHaveBeenCalledWith({
      path: '/assets',
      query: { catalog_id: '12' }
    })
    expect(router.push).not.toHaveBeenCalled()
  })

  it('keeps module routes local and exposes only supported asset workflows', () => {
    const router = readSource('router/index.js')

    expect(router).toContain("path: 'assets/:id'")
    expect(router).toContain("path: 'assets/:id/edit'")
    expect(router).not.toContain("path: 'assets/create'")
    expect(router).not.toMatch(/path: ['"]\/?asset\//)
  })

  it('persists the selected catalog in the canonical asset-list query', () => {
    const assetManager = readSource('views/AssetManager.vue')

    expect(assetManager).toContain('route.query.catalog_id')
    expect(assetManager).toContain('{ catalog_id: String(id) }')
    expect(assetManager).toContain("{ history: 'replace' }")
  })
})
