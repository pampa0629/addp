import { expect, test } from '@playwright/test'

test('retrieval explains field matches, expands definitions and escapes source markup', async ({ page }) => {
  const businessRequests = []
  const pageErrors = []
  page.on('pageerror', error => pageErrors.push(error.message))
  await page.addInitScript(() => {
    if (!localStorage.getItem('addp-lang')) localStorage.setItem('addp-lang', 'zh-cn')
  })
  await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url())
    if (url.pathname === '/api/v1/system/refresh') return json(route, { access_token: 'field-matches-fixture', expires_in: 3600 })
    if (url.pathname === '/api/v1/system/users/me') return json(route, { id: '34', display_name: 'retrieval-user', local_account: { username: 'retrieval-user' } })
    if (url.pathname === '/api/v1/system/auth/context') return json(route, {
      context: { type: 'tenant', tenant_id: '1' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '1' }, permissions: ['manager.search.execute', 'manager.data_item.read'] }] }
    })
    businessRequests.push(url.pathname)
    if (url.pathname === '/api/v1/manager/engines') return json(route, { data: [{ id: 11, name: 'Business MongoDB', engine_type: 'mongodb' }] })
    if (url.pathname === '/api/v1/manager/search/history') return json(route, { data: { items: [] } })
    if (url.pathname === '/api/v1/manager/search') {
      const semantic = url.searchParams.get('q') === 'semantic'
      return json(route, { data: { total: 1, page: 1, page_size: 10, results: [{
        document_id: 'collection', name: 'Outdoors', file_name: 'Outdoors', engine_id: 11,
        full_name: 'Outdoor.Outdoors', schema: 'Outdoor',
        locator: 'addp://engine/11/path/Outdoor/Outdoors?type=collection&item_id=111',
        match_methods: semantic ? ['vector'] : ['keyword'],
        field_matches: semantic ? [] : [
          { name: 'aaMembers', data_type: 'array', highlights: { name: '<mark>aaMember</mark>s' } },
          { name: 'leader.nickname', data_type: 'string', comment: 'Team participant', highlights: { comment: 'Team <mark>participant</mark>' } },
          ...['aaMembers.userInfo.nickName', 'aaMembers.userInfo.phone', 'aaMembers.entryInfo.status', 'aaMembers.<img src=x onerror="alert(1)">'].map(name => ({
            name, data_type: 'string', highlights: { name: name.replace('aaMember', '<mark>aaMember</mark>') }
          }))
        ]
      }] } })
    }
    return json(route, {}, 404)
  })

  await page.goto('/data-retrieval')
  await page.locator('.search-input input').fill('aamember')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(page.locator('.result-item')).toContainText('Outdoors')
  await expect(page.locator('.result-fields')).toContainText('命中字段（6）')
  await expect(page.locator('.field-match')).toHaveCount(5)
  await expect(page.locator('.field-name mark').first()).toHaveText('aaMember')
  await expect(page.locator('.field-comment mark')).toHaveText('participant')
  await expect(page.locator('.field-match').nth(1)).toContainText('leader.nickname')
  await page.getByRole('button', { name: '展开全部' }).click()
  await expect(page.locator('.field-match')).toHaveCount(6)
  await expect(page.locator('.field-match').last()).toContainText('<img src=x onerror="alert(1)">')
  await expect(page.locator('.result-fields img')).toHaveCount(0)
  await page.getByRole('button', { name: '收起', exact: true }).click()
  await expect(page.locator('.field-match')).toHaveCount(5)
  await page.setViewportSize({ width: 600, height: 800 })
  await expect(page.locator('.result-fields')).toBeVisible()
  expect(await page.locator('.result-fields').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)

  await page.locator('.search-input input').fill('semantic')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(page.locator('.result-item')).toContainText('向量匹配')
  await expect(page.locator('.result-fields')).toHaveCount(0)
  await page.evaluate(() => localStorage.setItem('addp-lang', 'en'))
  await page.reload()
  await page.locator('.search-input input').fill('aamember')
  await page.getByRole('button', { name: 'Search', exact: true }).click()
  await expect(page.locator('.result-fields')).toContainText('Matched fields (6)')
  await page.getByRole('button', { name: 'Show all', exact: true }).click()
  await expect(page.locator('.field-match')).toHaveCount(6)
  expect(businessRequests.every(path => ['/api/v1/manager/engines', '/api/v1/manager/search', '/api/v1/manager/search/history'].includes(path))).toBe(true)
  expect(pageErrors).toEqual([])
})

function json(route, body, status = 200) {
  return route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
