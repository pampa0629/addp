import { expect, test } from '@playwright/test'

const entryID = '00000000-0000-4000-8000-000000000021'
const collectionID = '00000000-0000-4000-8000-000000000031'

function mockCatalog(page, { groups = [], onCreate = () => {} } = {}) {
  return page.route('**/api/v1/**', route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (path === '/api/v1/system/refresh') return route.fulfill({ json: { access_token: 'catalog-collection-token', expires_in: 300 } })
    if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: 32, username: 'collection-user' } })
    if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
      context: { type: 'tenant', tenant_id: '3' },
      organization: { project_groups: groups.map(group => ({ project_group_id: group.project_group_id })) },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' }, permissions: ['catalog.collection.read', 'catalog.collection.update', 'catalog.entry.read'] }] }
    } })
    if (path === '/api/v1/catalog/me/project-groups') return route.fulfill({ json: { data: groups } })
    if (path === '/api/v1/catalog/collections' && request.method() === 'GET') {
      return route.fulfill({ json: { data: [], total: 0, page: 1, page_size: 20, total_pages: 0 } })
    }
    if (path === '/api/v1/catalog/entries') return route.fulfill({ json: {
      data: [{ id: entryID, display_name: '户外活动数据', entry_type: 'data_item' }], total: 1, page: 1, page_size: 50, total_pages: 1
    } })
    if (path === '/api/v1/catalog/collections' && request.method() === 'POST') {
      onCreate(request.postDataJSON())
      return route.fulfill({ json: { id: collectionID } })
    }
    if (path === `/api/v1/catalog/collections/${collectionID}`) return route.fulfill({ json: {
      id: collectionID, project_group_id: '9', name: '户外统计协作', description: '', version: 1,
      entries: [{ id: entryID, display_name: '户外活动数据' }]
    } })
    return route.fulfill({ status: 500, json: { error: `unexpected ${request.method()} ${path}` } })
  })
}

test('collection page explains an ineligible project group without claiming membership is absent', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await mockCatalog(page)
  await page.goto('/catalog/collections')
  const catalog = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(catalog.getByRole('heading', { name: '项目组协作集合' })).toBeVisible()
  await expect(catalog.getByText('当前没有同时满足有效成员资格和协作集合读取权限的项目组。')).toBeVisible()
  await expect(catalog.getByText('暂无可访问的项目组协作集合')).toBeVisible()
  await expect(catalog.getByRole('button', { name: '新建集合' })).toBeDisabled()
})

test('collection can include a visible catalog entry when it is created', async ({ page }) => {
  let createPayload
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await mockCatalog(page, { groups: [{ project_group_id: '9', name: '户外统计项目组', code: 'outdoor_stat', relation_role: 'member', can_read: true, can_update: true }], onCreate: payload => { createPayload = payload } })
  await page.goto('/catalog/collections')
  const catalog = page.frameLocator('iframe[data-testid="module-iframe"]')
  await catalog.getByRole('button', { name: '新建集合' }).click()
  const dialog = catalog.getByRole('dialog', { name: '新建集合' })
  await dialog.getByRole('textbox', { name: '集合名称' }).fill('户外统计协作')
  await dialog.getByRole('combobox', { name: '目录条目' }).fill('户外')
  await catalog.getByRole('option', { name: '户外活动数据' }).click()
  await dialog.getByRole('button', { name: '新建集合' }).click()
  await expect(page).toHaveURL(/\/catalog\/collections\/00000000-0000-4000-8000-000000000031$/)
  expect(createPayload).toMatchObject({ project_group_id: '9', name: '户外统计协作', entry_ids: [entryID] })
})
