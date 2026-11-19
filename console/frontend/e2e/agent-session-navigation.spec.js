import { expect, test } from '@playwright/test'
import { historyResponse } from '../../../agent/frontend/e2e/history-fixture'

async function installAgentReader(page, identity = () => ({ userID: '42', tenantID: '7' })) {
  const unexpected = []
  const sessions = [50, 36].map(id => ({ id, title: `只读会话 ${id}`, version: 1,
    pinned: false, archived: false, created_at: '2026-11-03T00:00:00Z', updated_at: '2026-11-03T00:00:00Z' }))
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    if (path === '/api/v1/system/refresh' && request.method() === 'POST') {
      return route.fulfill({ json: { access_token: 'agent-reader-fixture', expires_in: 3600 } })
    }
    if (request.method() === 'GET') {
      const { userID, tenantID } = identity()
      if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: userID, display_name: `Reader ${userID}` } })
      if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
        principal: { type: 'user', id: userID }, context: { type: 'tenant', tenant_id: tenantID },
        authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: tenantID },
          permissions: ['agent.session.read', 'agent.run.read'] }] }
      } })
      if (path === '/api/v1/agent/skills') return route.fulfill({ json: [] })
      if (path === '/api/v1/agent/sessions') return route.fulfill({ json: {
        data: sessions, total: 2, page: 1, page_size: 20, total_pages: 1
      } })
      const session = sessions.find(value => path === `/api/v1/agent/sessions/${value.id}`)
      if (session) return route.fulfill({ json: session })
      for (const value of sessions) {
        const history = [{ id: value.id * 10 + 1, protocol_message_id: `reader-${value.id}`,
          role: 'assistant', content: `历史正文 ${value.id}`, parts: [{ type: 'text', text: `历史正文 ${value.id}` }],
          is_complete: true, created_at: value.created_at }]
        const response = historyResponse(path, value.id, history, url)
        if (response) return route.fulfill({ json: response })
      }
    }
    unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({ status: 500, json: { error: 'unexpected_fixture_request' } })
  })
  return unexpected
}

test('Agent session switch preserves one public history entry through reload, back and forward', async ({ page }) => {
  const unexpected = await installAgentReader(page), errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('/agent/sessions/50')
  const agent = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(agent.getByText('历史正文 50', { exact: true })).toBeVisible()
  const frame = page.frames().find(frame => frame.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  const historyLength = await page.evaluate(() => history.length)
  await agent.getByRole('button', { name: '只读会话 36', exact: true }).click()
  await expect(page).toHaveURL('/agent/sessions/36')
  await expect(agent.getByText('历史正文 36', { exact: true })).toBeVisible()
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  expect(await page.evaluate(() => history.length)).toBe(historyLength + 1)
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('addp_recent_visits_v3:user:42:tenant:7') || '[]')
    .find(entry => entry.route === '/agent/sessions/36')?.subject)).toBe('只读会话 36')
  await page.reload()
  await expect(agent.getByText('历史正文 36', { exact: true })).toBeVisible()
  await page.goBack()
  await expect(page).toHaveURL('/agent/sessions/50')
  await expect(agent.getByText('历史正文 50', { exact: true })).toBeVisible()
  await page.goForward()
  await expect(page).toHaveURL('/agent/sessions/36')
  await expect(agent.getByText('历史正文 36', { exact: true })).toBeVisible()
  expect(unexpected).toEqual([])
  expect(errors).toEqual([])
})

test('Agent history anchor survives public reload and returns latest without a new iframe or history entry', async ({ page }) => {
  const unexpected = await installAgentReader(page)
  const anchorRequest = () => page.waitForRequest(request => {
    const url = new URL(request.url())
    return url.pathname === '/api/v1/agent/sessions/50/messages' && url.searchParams.get('anchor_message_id') === '501'
  })
  const first = anchorRequest()
  await page.goto('/agent/sessions/50?message_id=501')
  await first
  const agent = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(agent.getByText('历史正文 50', { exact: true })).toBeVisible()
  await expect(agent.getByText('正在查看历史片段', { exact: true })).toBeVisible()
  const restored = anchorRequest()
  await page.reload()
  await restored
  await expect(page).toHaveURL('/agent/sessions/50?message_id=501')
  await expect(agent.getByText('历史正文 50', { exact: true })).toBeVisible()
  const frame = page.frames().find(value => value.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  const historyLength = await page.evaluate(() => history.length)
  await agent.getByRole('button', { name: '返回最新消息', exact: true }).click()
  await expect(page).toHaveURL('/agent/sessions/50')
  await expect(agent.getByText('正在查看历史片段', { exact: true })).toHaveCount(0)
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  expect(await page.evaluate(() => history.length)).toBe(historyLength)
  expect(unexpected).toEqual([])
})

// Inject descriptor delivery independently of navigation to cover both message
// orders. The receiver is the real Console and the source is its active Agent.
async function describe(frame, route, subject, extra = {}) {
  return frame.evaluate(({ route, subject, extra }) => new Promise(resolve => {
    const requestId = crypto.randomUUID()
    const receive = event => {
      const message = event.data
      if (event.source !== parent || message?.type !== 'addp:console-bridge:response'
        || message.requestId !== requestId || message.pending) return
      window.removeEventListener('message', receive)
      resolve(message)
    }
    window.addEventListener('message', receive)
    parent.postMessage({ type: 'addp:console-bridge:request', source: 'addp-module',
      channel: 'console-navigation', requestId, payload: {
        route, history: 'replace', synchronized: true,
        pageDescriptor: { title: '智能体对话', subject, recent: true }, ...extra
      }
    }, location.origin)
  }), { route, subject, extra })
}

test('early and stale page descriptors cannot navigate or replace Agent history', async ({ page }) => {
  const unexpected = await installAgentReader(page)
  await page.goto('/agent/sessions/50')
  const agent = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(agent.getByText('历史正文 50', { exact: true })).toBeVisible()
  const frame = page.frames().find(frame => frame.parentFrame())
  const historyLength = await page.evaluate(() => history.length)
  expect((await describe(frame, '/agent/sessions/36', '尚未提交的会话')).data).toEqual({ cancelled: true })
  await expect(page).toHaveURL('/agent/sessions/50')
  expect(await page.evaluate(() => history.length)).toBe(historyLength)
  expect((await describe(frame, '/agent/sessions/50', '当前会话的描述')).ok).toBe(true)
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('addp_recent_visits_v3:user:42:tenant:7') || '[]')
    .find(entry => entry.route === '/agent/sessions/50')?.subject)).toBe('当前会话的描述')
  expect((await describe(frame, '/graph/ontologies/1', '其他模块')).ok).toBe(false)
  expect((await describe(frame, '/agent/sessions/50', '非同步描述', { synchronized: false })).ok).toBe(false)
  await agent.getByRole('button', { name: '只读会话 36', exact: true }).click()
  await expect(agent.getByText('历史正文 36', { exact: true })).toBeVisible()
  await expect(page).toHaveURL('/agent/sessions/36')
  expect((await describe(frame, '/agent/sessions/50', '迟到的会话')).data).toEqual({ cancelled: true })
  await expect(page).toHaveURL('/agent/sessions/36')
  expect(await page.evaluate(() => history.length)).toBe(historyLength + 1)
  await page.goBack()
  await expect(page).toHaveURL('/agent/sessions/50')
  await expect(agent.getByText('历史正文 50', { exact: true })).toBeVisible()
  expect(unexpected).toEqual([])
})

test('recent visits on the same browser separate accounts and tenants', async ({ page }) => {
  let userID = '42'
  let tenantID = '7'
  const unexpected = await installAgentReader(page, () => ({ userID, tenantID }))
  await page.addInitScript(() => {
    if (sessionStorage.getItem('legacy-recent-visits-seeded')) return
    sessionStorage.setItem('legacy-recent-visits-seeded', 'yes')
    localStorage.setItem('addp_recent_visits_v2', JSON.stringify([{
      key: '/agent/sessions/10', route: '/agent/sessions/10', module: 'agent',
      title: '旧会话', subject: '无账号归属', contextKey: 'tenant:7'
    }]))
  })
  await page.goto('/agent/sessions/50')
  await expect(page.frameLocator('iframe[data-testid="module-iframe"]').getByText('历史正文 50', { exact: true })).toBeVisible()
  await expect.poll(() => page.evaluate(() => localStorage.getItem('addp_recent_visits_v2'))).toBeNull()
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('addp_recent_visits_v3:user:42:tenant:7') || '[]')
    .find(entry => entry.route === '/agent/sessions/50')?.subject)).toBe('只读会话 50')

  const recentA = page.getByText(/· 只读会话 50$/)
  const recentB = page.getByText(/· 只读会话 36$/)
  userID = '43'
  await page.goto('/')
  await expect(page.getByRole('heading', { name: /Reader 43/ })).toBeVisible()
  await expect(recentA).toHaveCount(0)
  await page.goto('/agent/sessions/36')
  await expect(page.frameLocator('iframe[data-testid="module-iframe"]').getByText('历史正文 36', { exact: true })).toBeVisible()
  await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem('addp_recent_visits_v3:user:43:tenant:7') || '[]')
    .find(entry => entry.route === '/agent/sessions/36')?.subject)).toBe('只读会话 36')
  await page.goto('/')
  await expect(recentB).toBeVisible()
  await expect(recentA).toHaveCount(0)

  userID = '42'
  tenantID = '8'
  await page.goto('/')
  await expect(page.getByRole('heading', { name: /Reader 42/ })).toBeVisible()
  await expect(recentA).toHaveCount(0)
  await expect(recentB).toHaveCount(0)
  tenantID = '7'
  await page.goto('/')
  await expect(recentA).toBeVisible()
  await expect(recentB).toHaveCount(0)
  await expect(page.getByText(/无账号归属/)).toHaveCount(0)
  expect(unexpected).toEqual([])
})
