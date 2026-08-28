import { readFileSync } from 'node:fs'
import { describe, expect, it, vi } from 'vitest'

import { navigateAssetRoute } from '@/utils/moduleNavigation'

const readSource = path => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8')

describe('Asset public route contract', () => {
  it('delegates standalone navigation to the requested Vue Router history mode', async () => {
    const router = {
      resolve: vi.fn(() => ({ fullPath: '/assets?category_id=12' })),
      push: vi.fn(),
      replace: vi.fn()
    }

    await navigateAssetRoute(
      router,
      { path: '/assets', query: { category_id: '12' } },
      { history: 'replace' }
    )

    expect(router.replace).toHaveBeenCalledWith({
      path: '/assets',
      query: { category_id: '12' }
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

  it('persists the selected asset category in the canonical asset-list query', () => {
    const assetManager = readSource('views/AssetManager.vue')

    expect(assetManager).toContain('route.query.category_id')
    expect(assetManager).toContain('{ category_id: String(id) }')
    expect(assetManager).toContain("{ history: 'replace' }")
  })

  it('uses the single AssetCategory API and optimistic concurrency contract', () => {
    const api = readSource('api/asset.js')
    const categoryManagement = readSource('views/CategoryManagement.vue')

    expect(api).toContain("client.get('/asset/categories/tree')")
    expect(api).toContain("client.post('/asset/assets/batch-category'")
    expect(api).not.toContain('/asset/catalogs')
    expect(api).not.toContain('batch-catalog')
    expect(categoryManagement).toContain('version: form.value.version')
    expect(categoryManagement).toContain('parent_id: form.value.parent_value === ROOT_CATEGORY_PARENT ? null : form.value.parent_value')
    expect(categoryManagement).toContain("error_code === 'asset_category_version_conflict'")
    expect(categoryManagement).toContain('reloadEditBaseline')
    expect(categoryManagement).toContain('categoryAPI.delete(data.id, data.version)')
    expect(api).not.toContain('/move')
  })

  it('keeps the selected category synchronized with the tree after mutations', () => {
    const categoryManagement = readSource('views/CategoryManagement.vue')

    expect(categoryManagement).not.toContain('dialogVisible.value = false\n    selected.value = null')
    expect(categoryManagement).toContain('await selectCategory(saved.id)')
    expect(categoryManagement).toContain('treeRef.value?.setCurrentKey')
  })

  it('keeps AssetManager rename on the full AssetCategory update contract', () => {
    const assetManager = readSource('views/AssetManager.vue')

    expect(assetManager).not.toContain('{ version: categoryForm.version, name: categoryForm.name }')
    expect(assetManager).toContain('parent_id: categoryForm.parentId')
    expect(assetManager).toContain('description: categoryForm.description')
    expect(assetManager).toContain('sort_order: categoryForm.sortOrder')
  })

  it('presents the category tree as the Asset Directory without reusing Catalog', () => {
    const zhCn = JSON.parse(readSource('i18n/zh-cn.json')).asset
    const en = JSON.parse(readSource('i18n/en.json')).asset

    expect(zhCn.category.title).toBe('资产目录管理')
    expect(zhCn.category.detailTitle).toBe('目录节点详情')
    expect(en.layout.assetCatalog).toBe('Asset Directory')
    expect(en.category.title).toBe('Asset Directory Management')
    expect(JSON.stringify(en)).not.toContain('Asset Catalog')
  })

  it('loads selectable CatalogEntry candidates from the inventory view', () => {
    const picker = readSource('components/CatalogEntryPicker.vue')

    expect(picker).toContain("view: 'inventory'")
    expect(picker).toContain("source_status: 'active'")
  })

  it('exposes deletion for both draft and offline assets', () => {
    const assetDetail = readSource('views/AssetDetail.vue')

    expect(assetDetail).toContain("['draft', 'offline'].includes(asset.status)")
    expect(assetDetail).toContain(":confirm-button-text=\"t('asset.assetDetail.confirm')\"")
    expect(assetDetail).toContain(":cancel-button-text=\"t('asset.assetDetail.cancel')\"")
  })

  it('uses the single filtered Asset dashboard contract for application asset operations', () => {
    const api = readSource('api/asset.js')
    const dashboard = readSource('views/Dashboard.vue')
    const zhCn = JSON.parse(readSource('i18n/zh-cn.json')).asset.dashboard
    const en = JSON.parse(readSource('i18n/en.json')).asset.dashboard

    expect(api).toContain("dashboard: (params = {}) => client.get('/asset/assets/stats/dashboard', { params })")
    expect(dashboard).toContain('dashboardStatsParams(selectedScope.value)')
    expect(dashboard).toContain('requestSequence !== statsRequestSequence')
    expect(dashboard).toContain("const loadAnnouncementKey = ref('')")
    expect(dashboard).toContain('t(`asset.dashboard.${loadAnnouncementKey.value}`)')
    expect(dashboard).not.toContain('loadAnnouncement.value = t(')
    expect(dashboard).toContain('stats.effective_authorized_users')
    expect(dashboard).not.toContain('authorization_active')
    expect(dashboard).not.toMatch(/\sstyle=\"/)
    expect(zhCn.scopeApplications).toBe('全部数据应用资产')
    expect(en.scopeApplications).toBe('All Data Application Assets')
  })
})
