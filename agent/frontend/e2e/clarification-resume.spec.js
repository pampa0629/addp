import { expect, test } from '@playwright/test'

// T3: production UI and protocol client; HTTP/SSE responses are controlled.
// Same-business-Run persistence is checked by the backend Harness regression.
const sessionId = 41
const runId = 'a3db4a32-2c92-49eb-86e1-d3275b236198'
const questionId = '17e2125a-03b6-4495-aef5-bdb0f63bfa3a'
const reviewId = '803536f2-eb7c-43f1-bb6a-a1641a3cb416'
const goal = '把 MongoDB 的 outdoor 传到 PostgreSQL 的 outdoor，先建任务，不要运行。'
const answer = '一文档一行，只投影标量字段。'
const reviewOptions = [{ label: '确认创建', value: 'review-fingerprint' }, { label: '取消', value: 'cancel' }]

test.beforeEach(async ({ page }) => {
  page.on('pageerror', error => { throw error })
})

function clarification(id, prompt, options) {
  const operations = [
    { version: 'v0.9', createSurface: { surfaceId: `surface:${id}`, catalogId: 'addp.catalog/v1' } },
    { version: 'v0.9', updateComponents: { surfaceId: `surface:${id}`, components: [
      { id: 'root', component: 'ClarificationChoice', interactionId: id, prompt, options }
    ] } }
  ]
  return { role: 'assistant', id, parts: [
    { type: 'interaction_ref', interaction_id: id, kind: 'clarification', status: 'pending' },
    { type: 'presentation_ref', interaction_id: id, protocol: 'a2ui', surface_id: `surface:${id}`, content: { operations } }
  ] }
}

function completed(message) {
  message.parts.find(part => part.type === 'interaction_ref').status = 'completed'
}

async function installBackend(page, { multiple = false, rejectResume = false } = {}) {
  const question = clarification(questionId, '请选择行粒度', [{ label: '展开数组', value: 'unwind' }])
  const review = clarification(reviewId, '仅创建，不运行。请复核配置。\n```json\n{"name":"outdoor_copy","config":{"runtime":{"boundary":"bounded"},"load":{"mode":"snapshot"}}}\n```', reviewOptions)
  const messages = multiple ? [question, clarification(reviewId, '请选择目标', reviewOptions)] : []
  const requests = []
  const unexpected = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route(/^https?:\/\/[^/]+\/api\//, async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let body
    if (path === '/api/v1/system/refresh') body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path === '/api/v1/system/users/me') body = { id: 9, username: 'fixture-user' }
    else if (path === '/api/v1/system/auth/context') body = {
      context: { type: 'tenant', tenant_id: '7' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '7' },
        permissions: ['agent.session.read', 'agent.run.create', 'agent.run.execute'] }] }
    }
    else if (path === '/api/v1/agent/sessions' && request.method() === 'GET') body = [{ id: sessionId, title: goal }]
    else if (path === `/api/v1/agent/sessions/${sessionId}/messages`) body = messages
    else if (path === '/api/v1/agent/chat' && request.method() === 'POST') {
      const input = request.postDataJSON()
      requests.push(input)
      expect(input.threadId).toBe(String(sessionId))
      expect(request.headers().authorization).toBe('Bearer fixture-token')
      let current
      if (requests.length === 1) {
        expect(input.resume || []).toEqual([])
        expect(input.messages).toHaveLength(1)
        expect(input.messages[0]).toMatchObject({ role: 'user', content: goal })
        messages.push({ id: 'goal', role: 'user', parts: [{ type: 'text', text: goal }] }, question)
        current = question
      } else if (requests.length === 2) {
        expect(input.messages).toEqual([])
        expect(input.resume).toEqual([{ interruptId: questionId, status: 'resolved', payload: { text: answer } }])
        if (rejectResume) return route.fulfill({ status: 409, json: { error: '交互请求已处理' } })
        completed(question)
        messages.push({ id: 'answer', role: 'user', parts: [{ type: 'text', text: answer }] }, review)
        current = review
      } else {
        expect(requests).toHaveLength(3)
        expect(input.messages).toEqual([])
        expect(input.resume).toEqual([{ interruptId: reviewId, status: 'resolved', payload: reviewOptions[1] }])
        completed(review)
        messages.push({ id: 'cancelled', role: 'assistant', parts: [{ type: 'text', text: '已取消，不创建任务，不运行。' }] })
      }
      const events = [
        { type: 'RUN_STARTED', threadId: input.threadId, runId: input.runId },
        { type: 'STATE_SNAPSHOT', snapshot: { agentRunId: runId, sessionId, status: current ? 'waiting' : 'completed' } },
        ...(current ? [{ type: 'ACTIVITY_SNAPSHOT', messageId: `surface:${current.id}`, activityType: 'a2ui-surface', content: current.parts[1].content }] : []),
        { type: 'RUN_FINISHED', threadId: input.threadId, runId: input.runId,
          outcome: current ? { type: 'interrupt', interrupts: [{ id: current.id, reason: 'missing_input' }] } : { type: 'success' } }
      ]
      return route.fulfill({ contentType: 'text/event-stream', body: events.map(event => `data: ${JSON.stringify(event)}\n\n`).join('') })
    } else {
      // No fall-through to personal services, including Transfer writes/scans.
      unexpected.push(`${request.method()} ${path}`)
      return route.fulfill({ status: 500, json: { error: 'unexpected fixture request' } })
    }
    await route.fulfill({ json: body })
  })
  return { requests, unexpected }
}

test('short goal → reloaded text resume → reloaded review → cancel, with the real protocol client', async ({ page }) => {
  const backend = await installBackend(page)
  await page.goto(`/module-ui/agent/sessions/${sessionId}`)
  await page.getByRole('textbox').fill(goal)
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('button', { name: '展开数组', exact: true })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('textbox')).toHaveAttribute('maxlength', '2000')
  await page.getByRole('textbox').fill(answer)
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('button', { name: '确认创建', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '展开数组', exact: true })).toHaveCount(0)
  await page.reload()
  const detail = page.locator('details')
  await expect(detail).not.toHaveAttribute('open', '')
  await page.getByText('查看配置详情', { exact: true }).click()
  await expect(detail.locator('pre')).toContainText('outdoor_copy')
  await page.setViewportSize({ width: 620, height: 800 })
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByText('已取消，不创建任务，不运行。', { exact: true })).toBeVisible()
  await page.reload()
  await expect(page.getByText('已取消，不创建任务，不运行。', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '确认创建', exact: true })).toHaveCount(0)
  expect(backend.requests).toHaveLength(3)
  expect(new Set(backend.requests.map(input => input.runId)).size).toBe(3) // protocol IDs, not business Run IDs
  expect(backend.unexpected).toEqual([])
})

test('multiple pending questions do not guess a resume target or lose text', async ({ page }) => {
  const backend = await installBackend(page, { multiple: true })
  await page.goto(`/module-ui/agent/sessions/${sessionId}`)
  await page.getByRole('textbox').fill(answer)
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByText('当前有多个待回答的问题，请在对应卡片中选择选项', { exact: true })).toBeVisible()
  await expect(page.getByRole('textbox')).toHaveValue(answer)
  expect(backend.requests).toEqual([])
  expect(backend.unexpected).toEqual([])
})

test('a rejected text resume keeps the answer and does not retry as a new goal', async ({ page }) => {
  const backend = await installBackend(page, { rejectResume: true })
  await page.goto(`/module-ui/agent/sessions/${sessionId}`)
  await page.getByRole('textbox').fill(goal)
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('button', { name: '展开数组', exact: true })).toBeVisible()
  await page.getByRole('textbox').fill(answer)
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('textbox')).toHaveValue(answer)
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeVisible()
  expect(backend.requests).toHaveLength(2)
  expect(backend.unexpected).toEqual([])
})
