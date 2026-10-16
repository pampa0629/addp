import { expect, test } from '@playwright/test'
import { existsSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { isAnonymousRefreshResponse, login } from './transfer-browser-support.js'

// Login diagnostics may contain fill arguments; only explicitly verified screenshots are public.
test.use({ screenshot: 'off' })

async function sessionSnapshot(storeID) {
  const provides = document.querySelector('#app')?.__vue_app__?._context.provides || {}
  const pinia = Reflect.ownKeys(provides).map(key => provides[key])
    .find(value => value?._s instanceof Map && value._s.has(storeID))
  const store = pinia?._s.get(storeID)
  if (!store?.token || !store.authContext) return null
  const auth = store.authContext
  const fingerprint = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',
    new TextEncoder().encode(store.token)))).map(value => value.toString(16).padStart(2, '0')).join('')
  return {
    principal_id: auth.principal.id, tenant_id: auth.context.tenant_id,
    membership_id: auth.context.tenant_membership_id,
    authorization_version: auth.authorization.authorization_version,
    permissions: [...store.permissions].sort(), fingerprint,
    departments: auth.organization.departments.map(row => row.department_id),
    projects: auth.organization.project_groups.map(row => row.project_group_id),
    persisted_token: Boolean(localStorage.getItem('token') || sessionStorage.getItem('token'))
  }
}

test('PG member exits silently renew the same Console and Manager session without retaining denied rows', async ({ page, context }) => {
  if (process.env.ADDP_ONLINE_HOSTED !== '1' || process.env.ADDP_ONLINE_TEST !== '1') throw new Error('dedicated Hosted entry required')
  const binding = JSON.parse(process.env.ADDP_PG_BROWSER_BINDING)
  const control = process.env.ADDP_PG_BROWSER_CONTROL
  const artifacts = process.env.ADDP_ONLINE_ARTIFACT_DIR
  if (!control || !artifacts || !binding.run_id || !binding.locator) throw new Error('incomplete PG browser binding')
  const destination = resolve(artifacts, 'pg-authorization-browser.json')
  const route = `/manager/data-explorer?locator=${encodeURIComponent(binding.locator)}`
  const expectedPermissions = ['manager.data_item.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read']
  const observations = [], checks = [], failedResponses = [], pageErrors = []
  let phase = 'login', businessStarted = false, loginRequests = 0, anonymousRefreshes = 0
  const refreshes = [], staleResponses = []
  const matchesPreview = response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/manager/preview' && url.searchParams.get('locator') === binding.locator
  }
  context.on('request', request => {
    const path = new URL(request.url()).pathname
    if (path === '/api/v1/system/login' && request.method() === 'POST') loginRequests++
    if (path === '/api/v1/system/auth/context' && request.headers().authorization) businessStarted = true
  })
  context.on('response', response => {
    const path = new URL(response.url()).pathname
    if (path === '/api/v1/system/refresh') {
      if (isAnonymousRefreshResponse(response, businessStarted)) anonymousRefreshes++
      else refreshes.push({ phase, status: response.status() })
    }
    if (response.status() === 401 && phase !== 'login' && (
      path.startsWith('/api/v1/meta/') || path.startsWith('/api/v1/manager/') ||
      ['/api/v1/system/auth/context', '/api/v1/system/users/me'].includes(path))) {
      staleResponses.push(phase)
      return
    }
    if (matchesPreview(response) && response.status() === 403 && phase === 'project_group') return
    if (response.status() >= 400 && (matchesPreview(response) || path === '/api/v1/system/auth/context' ||
      (path === '/api/v1/system/refresh' && !isAnonymousRefreshResponse(response, businessStarted)))) {
      failedResponses.push({ path, status: response.status() })
    }
  })
  page.on('pageerror', () => pageErrors.push('uncaught_browser_error'))
  const report = result => ({ ...binding, schema_version: 'addp.pg-source-authorization-browser/v1',
    result, checks, observations, login_requests: loginRequests, anonymous_refreshes: anonymousRefreshes })
  try {
    await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
    const initialResponse = page.waitForResponse(response => matchesPreview(response) && response.status() === 200)
    await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME, process.env.ADDP_ONLINE_TEST_USER_PASSWORD, route)
    const initialData = (await (await initialResponse).json()).data
    expect(initialData.columns).toEqual(['person_id'])
    expect(initialData.rows.map(row => row.person_id).sort()).toEqual(['A', 'B'])
    const iframe = page.locator('iframe[data-testid="module-iframe"]')
    const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
    const moduleFrame = () => page.frames().find(frame => frame.url().includes('/module-ui/manager/'))
    await expect(manager.locator('.preview-panel .el-table__body-wrapper tbody tr')).toHaveCount(2)
    const originalFrame = moduleFrame()
    await originalFrame.evaluate(() => { window.__pgSessionObservation = true })
    const original = await page.evaluate(sessionSnapshot, 'console-auth')
    expect(original).toMatchObject({ principal_id: binding.principal_id, tenant_id: binding.tenant_id,
      permissions: expectedPermissions, persisted_token: false })
    let previous = original
    for (const kind of ['department', 'project_group']) {
      phase = kind
      const refreshCount = refreshes.length
      const beforeCookie = (await context.cookies()).find(cookie => cookie.name === 'addp_refresh_token')
      const marker = { ...binding, phase: kind }
      const ready = resolve(control, `ready-${kind}.tmp`)
      writeFileSync(ready, JSON.stringify(marker))
      renameSync(ready, resolve(control, `ready-${kind}.json`))
      await expect.poll(() => {
        const file = resolve(control, `release-${kind}.json`)
        return existsSync(file) ? JSON.parse(readFileSync(file, 'utf8')) : null
      }, { timeout: 120_000, intervals: [100] }).toEqual(marker)

      const status = kind === 'department' ? 200 : 403
      const retried = page.waitForResponse(response => matchesPreview(response) && response.status() === status)
      // Reselect the same real tree item: this performs a fresh read, never a scan or a page reload.
      await manager.locator('.explorer-tree').getByText('metric_people', { exact: true }).click()
      const payload = await (await retried).json()
      await expect(iframe).toBeVisible()
      await expect(page).toHaveURL(url => url.pathname === '/manager/data-explorer' &&
        url.searchParams.get('locator') === binding.locator)
      await expect.poll(async () => {
        const parent = await page.evaluate(sessionSnapshot, 'console-auth')
        const child = await moduleFrame()?.evaluate(sessionSnapshot, 'manager-auth')
        return Boolean(parent && child && parent.fingerprint !== previous.fingerprint &&
          child.fingerprint === parent.fingerprint && parent.authorization_version === child.authorization_version &&
          BigInt(parent.authorization_version) > BigInt(previous.authorization_version))
      }).toBe(true)
      const current = await page.evaluate(sessionSnapshot, 'console-auth')
      const child = await moduleFrame().evaluate(sessionSnapshot, 'manager-auth')
      for (const snapshot of [current, child]) {
        expect(snapshot).toMatchObject({ principal_id: original.principal_id, tenant_id: original.tenant_id,
          membership_id: original.membership_id, permissions: expectedPermissions, persisted_token: false,
          departments: [] })
        expect(snapshot.projects.length).toBe(kind === 'department' ? 1 : 0)
      }
      expect(moduleFrame()).toBe(originalFrame)
      expect(await originalFrame.evaluate(() => window.__pgSessionObservation)).toBe(true)
      expect(refreshes.slice(refreshCount)).toEqual([{ phase: kind, status: 200 }])
      expect(staleResponses.includes(kind)).toBe(true)
      const afterCookie = (await context.cookies()).find(cookie => cookie.name === 'addp_refresh_token')
      expect(Boolean(beforeCookie?.httpOnly && afterCookie?.httpOnly && beforeCookie.value !== afterCookie.value)).toBe(true)
      if (status === 200) {
        expect(payload.data.rows.map(row => row.person_id).sort()).toEqual(['A', 'B'])
        await expect(manager.locator('.preview-panel .el-table__body-wrapper tbody tr')).toHaveCount(2)
        await expect(manager.getByTestId('preview-failure')).toHaveCount(0)
        checks.push('department_exit_silent_refresh')
      } else {
        expect(Object.keys(payload).every(key => ['error', 'error_code'].includes(key))).toBe(true)
        expect(payload.error_code === 'security_protection_required').toBe(false)
        await expect(manager.getByTestId('preview-failure')).toContainText('无权读取此数据项')
        await expect(manager.locator('.preview-panel .el-table__body-wrapper tbody tr')).toHaveCount(0)
        await expect(manager.locator('.preview-panel')).not.toContainText('person_id')
        checks.push('project_exit_silent_refresh')
      }
      observations.push({ closed_type: kind, refreshes: 1, preview_status: status })
      expect(loginRequests).toBe(1)
      expect(anonymousRefreshes).toBeLessThanOrEqual(1)
      expect(failedResponses).toEqual([])
      expect(pageErrors).toEqual([])
      await page.screenshot({ path: resolve(artifacts, `pg-membership-${kind}.png`) })
      previous = current
    }
    checks.push('same_iframe_without_relogin', 'denied_preview_clears_old_rows')
    writeFileSync(destination, JSON.stringify(report('passed')))
  } catch {
    writeFileSync(destination, JSON.stringify({ ...report('failed'), failed_phase: phase }))
    throw new Error(`PG browser member-exit acceptance failed at ${phase}`)
  }
})
