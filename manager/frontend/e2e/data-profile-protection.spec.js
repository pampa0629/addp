import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'
import zh from '../src/i18n/zh-cn.json' with { type: 'json' }
import en from '../src/i18n/en.json' with { type: 'json' }

for (const language of ['zh-cn', 'en']) {
  test(`profile read rejection stops stale progress and retries reading (${language})`, async ({ page }) => {
    const words = (language === 'en' ? en : zh).manager.explorer.profile
    const locator = 'addp://engine/2/path/public/profile_fixture?type=table&item_id=200'
    const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
    const item = { id: locator, locator, label: 'profile_fixture', type: 'table', hasChildren: false }
    const root = { id: rootLocator, locator: rootLocator, label: 'PostgreSQL', type: 'server', hasChildren: true, children: [item] }
    let created = 0
    let allowRead = false
    const auth = structuredClone(managerAuthContext)
    auth.authorization.role_assignments[0].permissions.push('manager.data_profile.execute')
    const reply = (route, body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
    await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
    await page.route('**/plugins/manifest.json', route => reply(route, { scripts: ['/plugins/table-preview.js'] }))
    await page.route('**/api/v1/**', route => {
      const path = new URL(route.request().url()).pathname
      if (path.endsWith('/system/refresh')) return reply(route, { access_token: 'profile-fixture-token', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return reply(route, { id: '1', display_name: 'fixture' })
      if (path.endsWith('/system/auth/context')) return reply(route, auth)
      if (path.endsWith('/manager/engines')) return reply(route, { data: [{ id: 2, name: root.label, engine_type: 'postgresql', lifecycle_state: 'active', connection_status: 'online' }] })
      if (path.endsWith('/ancestors')) return reply(route, { target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
      if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return reply(route, root)
      if (path.endsWith('/manager/preview')) return reply(route, { preview_type: 'table', data: { item_meta: { id: 200, fingerprint: 'profile-fixture', full_name: 'public.profile_fixture', attributes: [{key:'item',value:{data_type:'table'}}] }, mode: 'table', columns: ['name'], rows: [{ name: 'synthetic' }], total: 1, page: 1, page_size: 20 } })
      if (path.endsWith('/manager/data-profile-executions')) {
        created++
        return reply(route, { execution: { execution_id: 'profile-execution', status: 'pending', progress: 0 }, profile_config_hash: 'fixture-config', data_scope: { kind: 'all' } }, 202)
      }
      if (path.endsWith('/manager/data-profiles/current')) {
        if (!created) return reply(route, { supported: true })
        if (!allowRead) return reply(route, { error_code: 'security_protection_required', error: 'stale generic backend wording' }, 403)
        return reply(route, { supported: true, profile: { mode: 'sample', data_scope: { kind: 'all' }, field_count: 1, sample_size: 1, rows_scanned: 1, observations: [], fields: [{ name: 'name', type: 'string', status: 'computed', null_count: 0, non_null_count: 1, top_values: [] }] }, latest_execution: { execution_id: 'profile-execution', status: 'success' } })
      }
      return reply(route, {})
    })
    await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
    await expect(page.getByRole('cell', { name: 'synthetic', exact: true })).toBeVisible()
    await page.getByRole('tab', { name: language === 'en' ? 'Profile' : '剖析', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: words.protectionReadFailed })).toBeVisible()
    await expect(page.locator('.execution-strip')).toHaveCount(0)
    await expect(page.getByText(words.waitingForFirstResult, { exact: true })).toHaveCount(0)
    await expect(page.getByText(words.readFailed, { exact: true })).toBeVisible()
    expect(created).toBe(1)
    allowRead = true
    await page.getByRole('button', { name: words.retryRead, exact: true }).first().click()
    await expect(page.locator('.field-list .el-table')).toBeVisible()
    await expect(page.getByText(words.protectionReadFailed, { exact: true })).toHaveCount(0)
    expect(created).toBe(1)
  })
}

for (const language of ['zh-cn', 'en']) {
  for (const scenario of [
    { engine: 'mongodb', rejectAt: 'read' },
    { engine: 'mysql', rejectAt: 'read' },
    { engine: 'postgresql', rejectAt: 'create' },
    { engine: 'postgresql', rejectAt: 'poll' }
  ]) {
    test(`unsupported profile source is informational without retry or polling (${language}, ${scenario.engine}, ${scenario.rejectAt})`, async ({ page }) => {
      const words = (language === 'en' ? en : zh).manager.explorer.profile
      const locator = 'addp://engine/2/path/public/profile_fixture?type=table&item_id=200'
      const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
      const item = { id: locator, locator, label: 'profile_fixture', type: 'table', hasChildren: false }
      const root = { id: rootLocator, locator: rootLocator, label: scenario.engine, type: 'server', hasChildren: true, children: [item] }
      let created = 0
      let reads = 0
      const auth = structuredClone(managerAuthContext)
      auth.authorization.role_assignments[0].permissions.push('manager.data_profile.execute')
      const reply = (route, body, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
      const unsupported = route => reply(route, { error_code: 'data_profile_source_unsupported', error: 'generic backend wording' }, 422)
      await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
      await page.route('**/plugins/manifest.json', route => reply(route, { scripts: ['/plugins/table-preview.js'] }))
      await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/system/refresh')) return reply(route, { access_token: 'profile-fixture-token', expires_in: 3600 })
        if (path.endsWith('/system/users/me')) return reply(route, { id: '1', display_name: 'fixture' })
        if (path.endsWith('/system/auth/context')) return reply(route, auth)
        if (path.endsWith('/manager/engines')) return reply(route, { data: [{ id: 2, name: root.label, engine_type: scenario.engine, lifecycle_state: 'active', connection_status: 'online' }] })
        if (path.endsWith('/ancestors')) return reply(route, { target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
        if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return reply(route, root)
        if (path.endsWith('/manager/preview')) return reply(route, { preview_type: 'table', data: { item_meta: { id: 200, fingerprint: 'profile-fixture', full_name: 'public.profile_fixture', attributes: [{ key: 'item', value: { data_type: 'table' } }] }, mode: 'table', columns: ['name'], rows: [{ name: 'synthetic' }], total: 1, page: 1, page_size: 20 } })
        if (path.endsWith('/manager/data-profile-executions')) {
          created++
          if (scenario.rejectAt === 'poll') return reply(route, { execution: { execution_id: 'pending-profile', status: 'pending', progress: 0 }, profile_config_hash: 'fixture-config', data_scope: { kind: 'all' } }, 202)
          return unsupported(route)
        }
        if (path.endsWith('/manager/data-profiles/current')) {
          reads++
          return scenario.rejectAt === 'read' || created > 0 ? unsupported(route) : reply(route, { supported: true })
        }
        return reply(route, {})
      })
      await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
      await expect(page.getByRole('cell', { name: 'synthetic', exact: true })).toBeVisible()
      await page.getByRole('tab', { name: language === 'en' ? 'Profile' : '剖析', exact: true }).click()
      await expect(page.locator('.profile-alert.el-alert--info')).toHaveText(words.sourceUnsupported)
      await expect(page.locator('.execution-strip, .empty-state')).toHaveCount(0)
      for (const label of [words.retry, words.retryRead, words.refresh]) await expect(page.getByRole('button', { name: label, exact: true })).toHaveCount(0)
      // Observe beyond the production polling interval: neither GET nor POST repeats.
      await page.waitForTimeout(2300)
      expect(reads).toBe(scenario.rejectAt === 'poll' ? 2 : 1)
      expect(created).toBe(scenario.rejectAt === 'read' ? 0 : 1)
    })
  }
}
