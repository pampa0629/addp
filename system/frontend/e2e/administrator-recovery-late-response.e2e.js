import { expect, test } from '@playwright/test'
import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

test.use({ trace: 'off', screenshot: 'off', video: 'off' })

const token = 'synthetic-reviewer-session-A'
const proof = 'synthetic-recovery-proof-only'
const requestPath = '/api/v1/system/platform/administrator_changes'
const context = (id, role) => ({
  principal: { id, principal_type: 'user' },
  context: { type: 'platform' },
  authentication: { assurance_level: 'aal2' },
  authorization: { authorization_version: '1', role_assignments: [{
    role_key: `platform.${role}_administrator`,
    scope: { type: 'platform' },
    permissions: ['iam.platform_administrator_change.read', 'iam.platform_administrator_change.approve']
  }] }
})

for (const [transition, name] of [
  ['none', 'same authority displays its one-time recovery proof'],
  ['identity', 'old approval cannot display a recovery proof under a different identity'],
  ['token', 'old approval cannot display a recovery proof after access-token replacement'],
  ['version', 'old approval cannot display a recovery proof after authorization version changes']
]) {
  test(name, async ({ page }) => {
    await page.addInitScript(() => {
      const open = XMLHttpRequest.prototype.open
      XMLHttpRequest.prototype.open = function (method, url, ...args) {
        if (method === 'POST' && new URL(url, location.origin).pathname.endsWith('/administrator_changes/71/approve')) {
          this.addEventListener('loadend', () => {
            // Wait until the real API client and Vue have processed the delivered result.
            requestAnimationFrame(() => requestAnimationFrame(() => { window.approvalResponseHandled = this.status }))
          }, { once: true })
        }
        return open.call(this, method, url, ...args)
      }
    })
    const origin = browserTestOrigin('system')
    let authenticated = false, heldApproval, listReads = 0, approvalCalls = 0
    const pending = {
      id: '71', version: '1', status: 'pending',
      change_type: 'platform_credential_recovery',
      governance_role_key: 'platform.system_administrator',
      reviewer_role_key: 'platform.audit_administrator',
      requested_by_principal_id: '30', target_principal_id: '40',
      reason: 'Synthetic lost authenticator',
      expires_at: new Date(Date.now() + 600000).toISOString()
    }
    const initial = context('10', 'audit')
    const replacement = context('20', 'system')
    const unexpected = []
    await page.route('**/*', async route => {
      const request = route.request()
      const { pathname: path, origin: targetOrigin } = new URL(request.url())
      if (targetOrigin !== origin) {
        unexpected.push('outside isolated origin')
        return route.abort()
      }
      if (!path.startsWith('/api/')) return route.continue()
      const api = '/api/v1/system'
      if (path === api + '/refresh') return route.fulfill(authenticated
        ? { json: { access_token: token, expires_in: 300 } }
        : { status: 401, json: { error: 'authentication_required' } })
      if (path === api + '/login') {
        authenticated = true
        return route.fulfill({ json: { next_action: 'session_issued', session: { access_token: token, expires_in: 300 } } })
      }
      if (path === api + '/users/me') return route.fulfill({ json: { id: '10', username: 'reviewer-a', display_name: 'Reviewer A' } })
      if (path === api + '/auth/context') return route.fulfill({ json: initial })
      if (path === api + '/auth/context-options') return route.fulfill({ json: { contexts: [{ type: 'platform', current: true }] } })
      if (path === requestPath && request.method() === 'GET') {
        listReads++
        return route.fulfill({ json: { data: [pending], total: 1, total_pages: 1 } })
      }
      if (path === requestPath + '/71/approve' && request.method() === 'POST') {
        approvalCalls++
        expect(request.headers().authorization).toBe('Bearer ' + token)
        expect(request.postDataJSON()).toEqual({ version: '1', reason: 'Synthetic review' })
        heldApproval = route
        return
      }
      unexpected.push(request.method() + ' ' + path)
      return route.fulfill({ status: 403, json: { error: 'unexpected_fixture_request' } })
    })

    await page.goto('/login?redirect=%2Fiam%2Faccounts%3Ftab%3Dadministrator-changes')
    await page.locator('input[autocomplete="username"]').fill('reviewer-a')
    await page.locator('input[autocomplete="current-password"]').fill('synthetic-login-only')
    await page.locator('button.auth-login-primary').click()
    await expect(page.getByRole('heading', { name: '账号管理' })).toBeVisible()
    const table = page.getByRole('row').filter({ hasText: pending.reason })
    await expect(table).toBeVisible()
    await table.getByRole('button', { name: '批准', exact: true }).click()
    const review = page.getByRole('dialog', { name: '批准', exact: true })
    await review.getByRole('textbox').fill('Synthetic review')
    await review.getByRole('button', { name: '确认', exact: true }).click()
    await expect.poll(() => heldApproval !== undefined).toBe(true)

    const recovery = page.getByRole('dialog', { name: '一次性恢复凭据', exact: true })
    if (transition !== 'none') {
      // Drive the real store while keeping this page mounted. API responses are fixtures.
      await page.evaluate(async ({ transition, replacement }) => {
        const { useAuthStore } = await import('/src/store/auth.js')
        const auth = useAuthStore()
        if (transition === 'identity') auth.authContext = replacement
        if (transition === 'token') auth.token = 'synthetic-reviewer-session-B'
        if (transition === 'version') auth.authContext.authorization.authorization_version = '2'
      }, { transition, replacement })
      await expect(page.getByRole('tab', { name: '管理员变更治理', exact: true })).toBeVisible()
    }
    const readsBeforeResponse = listReads
    await heldApproval.fulfill({ json: { ...pending, status: 'approved', version: '2', recovery_token: proof } })
    await expect.poll(() => page.evaluate(() => window.approvalResponseHandled)).toBe(200)
    expect(unexpected).toEqual([])
    expect(approvalCalls).toBe(1)

    if (transition === 'none') {
      await expect(recovery.getByRole('textbox')).toHaveValue(proof)
      await expect.poll(() => listReads).toBe(readsBeforeResponse + 1)
    } else {
      expect(await recovery.isVisible(), 'a stale response must not reopen the recovery dialog').toBe(false)
      await expect(review).toBeHidden()
      await expect(page.getByText('更新成功', { exact: true })).toHaveCount(0)
      expect(listReads, 'a stale response must not reload the current list').toBe(readsBeforeResponse)
    }
  })
}
