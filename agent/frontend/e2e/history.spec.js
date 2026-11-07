import { expect, test } from '@playwright/test'
import { historyResponse } from './history-fixture'

async function installHistory(page, { pending = false } = {}) {
  const messages = Array.from({ length: 70 }, (_, index) => ({
    id: index + 1, protocol_message_id: `message-${index + 1}`, role: index ? 'assistant' : 'user',
    content: index ? `历史正文 ${index + 1}` : '最初用户目标',
    parts: [{ type: 'text', text: index ? `历史正文 ${index + 1}` : '最初用户目标' }]
  }))
  messages[4].parts.push({ type: 'presentation_ref', protocol: 'a2ui', surface_id: 'historical-table',
    content: { operations: [
      { version: 'v0.9', createSurface: { surfaceId: 'historical-table', catalogId: 'addp.catalog/v1' } },
      { version: 'v0.9', updateComponents: { surfaceId: 'historical-table', components: [
        { id: 'root', component: 'TablePreview', columns: ['name'], rows: [{ name: '早期成果' }], total: 1, truncated: false }
      ] } }
    ] } })
  if (pending) messages[6].parts.push({ type: 'interaction_ref', kind: 'clarification', interaction_id: 'old-question', status: 'pending' })
  const queries = [], submissions = [], unexpected = []
  let failNextPage = false
  let failWorkspace = false
  let nextPageWait = null
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route(/^https?:\/\/[^/]+\/api\//, async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    let body
    if (path === '/api/v1/system/refresh') body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path === '/api/v1/system/users/me') body = { id: 9, username: 'history-user' }
    else if (path === '/api/v1/system/auth/context') body = { context: { type: 'tenant', tenant_id: '7' }, authorization: {
      role_assignments: [{ scope: { type: 'tenant', tenant_id: '7' }, permissions: ['agent.session.read', 'agent.run.create', 'agent.run.execute'] }]
    } }
    else if (path === '/api/v1/agent/skills') body = []
    else if (path === '/api/v1/agent/sessions') body = { data: [{ id: 61, title: '长会话', version: 1 }], total: 1, page: 1, page_size: 20, total_pages: 1 }
    else if (path === '/api/v1/agent/sessions/61') body = { id: 61, title: '长会话', version: 1, archived: false }
    else if (path === '/api/v1/agent/sessions/61/messages' || path === '/api/v1/agent/sessions/61/message-index') {
      queries.push({ path, params: Object.fromEntries(url.searchParams) })
      if (url.searchParams.has('cursor') && nextPageWait) {
        const wait = nextPageWait
        nextPageWait = null
        await wait
      }
      if (failNextPage && url.searchParams.has('cursor')) {
        failNextPage = false
        return route.fulfill({ status: 503, json: { error: 'history unavailable' } })
      }
      const anchor = Number(url.searchParams.get('anchor_message_id'))
      const before = Number(url.searchParams.get('cursor') || messages.length + 1)
      const batch = anchor ? messages.slice(Math.max(0, anchor - 11), Math.max(0, anchor - 11) + 20) : messages.filter(message => message.id < before).slice(-20)
      const older = batch[0]?.id > 1 ? String(batch[0].id) : null
      if (path.endsWith('/messages')) body = { data: batch, page: { limit: 20, older_cursor: older, newer_cursor: anchor ? 'newer' : null } }
      else {
        body = historyResponse(path, 61, batch, url)
        body.page.older_cursor = older
      }
    } else if (path === '/api/v1/agent/sessions/61/workspace') {
      if (failWorkspace) { failWorkspace = false; return route.fulfill({ status: 503, json: { error: 'workspace unavailable' } }) }
      body = historyResponse(path, 61, messages, url)
    }
    else if (path === '/api/v1/agent/chat' && request.method() === 'POST') {
      const value = request.postDataJSON()
      submissions.push(value)
      expect(value.resume).toEqual([{ interruptId: 'old-question', status: 'resolved', payload: { text: '选择原数据源' } }])
      messages[6].parts[1].status = 'completed'
      const events = [{ type: 'RUN_STARTED', threadId: '61', runId: value.runId },
        { type: 'RUN_FINISHED', threadId: '61', runId: value.runId, outcome: { type: 'success' } }]
      return route.fulfill({ contentType: 'text/event-stream', body: events.map(event => `data: ${JSON.stringify(event)}\n\n`).join('') })
    } else {
      unexpected.push(path)
      return route.fulfill({ status: 500, json: { error: 'unexpected request' } })
    }
    return route.fulfill({ json: body })
  })
  return { queries, submissions, unexpected, failPage: () => { failNextPage = true }, failWorkspace: () => { failWorkspace = true }, holdPage: () => {
    let release
    nextPageWait = new Promise(resolve => { release = resolve })
    return release
  } }
}

test('latest refresh restores unknown pending facts without losing the input draft', async ({ page }) => {
  const fixture = await installHistory(page)
  await page.goto('/module-ui/agent/sessions/61')
  await page.locator('textarea').fill('保留这段未发送草稿')
  fixture.failPage()
  await page.getByRole('button', { name: '加载更早消息', exact: true }).click()
  await expect(page.getByText('历史内容加载失败，已加载内容仍保留')).toBeVisible()
  fixture.failWorkspace()
  await page.locator('.history-controls').getByRole('button', { name: '刷新列表', exact: true }).click()
  await expect(page.locator('textarea')).toBeDisabled()
  await expect(page.locator('.message-row')).toHaveCount(20)
  await page.locator('.conversation-actions button').nth(1).click()
  await expect(page.locator('textarea')).toBeEnabled()
  await expect(page.locator('textarea')).toHaveValue('保留这段未发送草稿')
  await expect(page.getByText('历史内容加载失败，已加载内容仍保留')).toHaveCount(0)
  expect(fixture.submissions).toEqual([])
  expect(fixture.unexpected).toEqual([])
})

test('returning latest cancels an older-page loading state without locking future reads', async ({ page }) => {
  const fixture = await installHistory(page)
  await page.goto('/module-ui/agent/sessions/61')
  const older = page.getByRole('button', { name: '加载更早消息', exact: true })
  await expect(older).toBeVisible()
  const release = fixture.holdPage()
  try {
    await older.click()
    await expect(older).toHaveClass(/is-loading/)
    await page.locator('.conversation-actions button').nth(1).click()
    await expect(older).not.toHaveClass(/is-loading/)
    await expect(page.locator('.message-row')).toHaveCount(20)
  } finally { release() }
  await older.click()
  await expect(page.locator('.message-row')).toHaveCount(40)
  expect(fixture.submissions).toEqual([])
  expect(fixture.unexpected).toEqual([])
})

test('history keeps loaded messages on failure and locates an unloaded old result across refresh', async ({ page }) => {
  const fixture = await installHistory(page)
  await page.goto('/module-ui/agent/sessions/61')
  await expect(page.locator('.message-row')).toHaveCount(20)
  await expect(page.locator('[data-message-id="message-51"]')).toBeVisible()
  fixture.failPage()
  await page.getByRole('button', { name: '加载更早消息', exact: true }).click()
  await expect(page.getByText('历史内容加载失败，已加载内容仍保留')).toBeVisible()
  await expect(page.locator('.message-row')).toHaveCount(20)
  await page.getByRole('button', { name: '加载更早消息', exact: true }).click()
  await expect(page.locator('.message-row')).toHaveCount(40)
  await expect(page.locator('[data-message-id="message-51"]')).toHaveCount(1)
  await page.getByRole('button', { name: '过程与成果', exact: true }).click()
  await page.getByRole('tab', { name: /^成果/ }).click()
  for (let batch = 0; batch < 3; batch++) {
    await page.getByRole('button', { name: '加载更多索引', exact: true }).click()
  }
  const result = page.locator('.result-link').filter({ hasText: '表格' })
  await expect(result).toHaveCount(1)
  await result.click()
  await expect(page).toHaveURL(/message_id=5/)
  await expect(page.locator('[data-message-id="message-5"]')).toContainText('早期成果')
  await expect(page.getByText('正在查看历史片段', { exact: true })).toBeVisible()
  await page.reload()
  await expect(page.locator('[data-message-id="message-5"]')).toContainText('早期成果')
  await page.getByRole('button', { name: '返回最新消息', exact: true }).click()
  await expect(page.locator('[data-message-id="message-70"]')).toBeVisible()
  await expect(page.locator('[data-message-id="message-5"]')).toHaveCount(0)
  expect(fixture.submissions).toEqual([])
  expect(fixture.unexpected).toEqual([])
})

test('the sole pending clarification outside the latest window resumes the original interaction', async ({ page }) => {
  const fixture = await installHistory(page, { pending: true })
  await page.goto('/module-ui/agent/sessions/61')
  await expect(page.locator('[data-message-id="message-7"]')).toHaveCount(0)
  await page.locator('textarea').fill('选择原数据源')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect.poll(() => fixture.submissions.length).toBe(1)
  expect(fixture.unexpected).toEqual([])
})
