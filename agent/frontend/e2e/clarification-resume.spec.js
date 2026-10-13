import { expect, test } from '@playwright/test'

// T3: production UI and protocol client; HTTP/SSE responses are controlled.
// Same-business-Run persistence is checked by the backend Harness regression.
const sessionId = 41
const runId = 'a3db4a32-2c92-49eb-86e1-d3275b236198'
const questionId = '17e2125a-03b6-4495-aef5-bdb0f63bfa3a'
const reviewId = '803536f2-eb7c-43f1-bb6a-a1641a3cb416'
const goal = '把 MongoDB 的 outdoor 传到 PostgreSQL 的 outdoor，先建任务，不要运行。'
const answer = '一文档一行，只投影标量字段。'
const reviewOptions = [{ label: '知悉未核验项，仅创建任务', value: 'review-fingerprint' }, { label: '取消', value: 'cancel' }]
const unknownWarning = '预检结果为无法确定（unknown），并非通过。请核对未检查项和运行时核验要求；确认表示知情接受这些限制，仅保存未运行任务。'
const draftGoal = '把 MongoDB 的 outdoor 传到 PostgreSQL 的 outdoor，检查配置，只给草稿，不创建、不运行。'
const checkExplanation = '关键检查解释：尚有未核验项，不代表可以运行。'

function checkReport(status) {
  const report = { status, observed_at: '2026-10-12T00:00:00Z', checked: ['request_shape'], not_checked: ['business_rows'],
    runtime_checks_required: ['per_value_encoding'], diagnostics: [{ status, code: 'source_type_unknown', check: 'source_assignment',
      config_path: '/config/transforms/0/fields/0', source_field: 'members', target_field: 'members', physical_type: 'TEXT[]' }] }
  const surface = `surface:check-${status}`
  return { role: 'assistant', id: surface, parts: [{ type: 'presentation_ref', protocol: 'a2ui', catalog_id: 'addp.catalog/v1', surface_id: surface,
    content: { operations: [
      { version: 'v0.9', createSurface: { surfaceId: surface, catalogId: 'addp.catalog/v1' } },
      { version: 'v0.9', updateComponents: { surfaceId: surface, components: [{ id: 'root', component: 'TransferConfigCheckReport', report }] } }
    ] } }] }
}

function checkedDraftPart() {
  const surface = 'surface:checked-draft'
  return { type: 'presentation_ref', protocol: 'a2ui', catalog_id: 'addp.catalog/v1', surface_id: surface,
    content: { operations: [
      { version: 'v0.9', createSurface: { surfaceId: surface, catalogId: 'addp.catalog/v1' } },
      { version: 'v0.9', updateComponents: { surfaceId: surface, components: [{ id: 'root', component: 'TransferDraftSnapshot',
        reference: `draft:cccccccc-cccc-cccc-cccc-cccccccccccc:${'a'.repeat(64)}`, name: 'readonly_persons',
        parameters: JSON.stringify({ name: 'readonly_persons', config: {
          source: { locator: 'addp://engine/11/path/Outdoor/Persons?type=collection&item_id=106', data_type: 'table', representation: 'native' },
          target: { parent_locator: 'addp://engine/2/path/outdoor?type=schema&node_id=3', name: 'persons_check',
            data_type: 'table', representation: 'native', policy: { apply_mode: 'replace' } },
          runtime: { boundary: 'bounded' }, load: { mode: 'snapshot' }
        } }, null, 2),
        mappings: [{ source: 'members', target: 'members', target_type: 'json', nullable: null, precision: null, scale: null }]
      }] } }
    ] } }
}

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

async function installBackend(page, { multiple = false, rejectResume = false, draftReportStatus = null, checkedDraft = false } = {}) {
  const question = clarification(questionId, '请选择行粒度', [{ label: '展开数组', value: 'unwind' }])
  const review = clarification(reviewId, `仅创建，不运行。请复核配置。\n\n${unknownWarning}\n\n\`\`\`json\n{"name":"outdoor_copy","config":{"runtime":{"boundary":"bounded"},"load":{"mode":"snapshot"}}}\n\`\`\``, reviewOptions)
  review.parts.splice(1, 0, checkReport('unknown').parts[0])
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
        const expectedGoal = draftReportStatus ? draftGoal : goal
        expect(input.messages[0]).toMatchObject({ role: 'user', content: expectedGoal })
        current = draftReportStatus ? checkReport(draftReportStatus) : question
        if (checkedDraft) current.parts = [checkedDraftPart(), ...current.parts, { type: 'text', text: checkExplanation }]
        messages.push({ id: 'goal', role: 'user', parts: [{ type: 'text', text: expectedGoal }] }, current)
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
      const waiting = current?.parts.some(part => part.type === 'interaction_ref')
      const events = [
        { type: 'RUN_STARTED', threadId: input.threadId, runId: input.runId },
        { type: 'STATE_SNAPSHOT', snapshot: { agentRunId: runId, sessionId, status: waiting ? 'waiting' : 'completed' } },
        ...(current ? current.parts.flatMap((part, index) => {
          if (part.type === 'presentation_ref') return [
            { type: 'ACTIVITY_SNAPSHOT', messageId: `activity:${current.id}:${index}`, activityType: 'a2ui-surface', content: part.content }
          ]
          if (part.type === 'text') return [
            { type: 'TEXT_MESSAGE_START', messageId: `text:${current.id}:${index}`, role: 'assistant' },
            { type: 'TEXT_MESSAGE_CONTENT', messageId: `text:${current.id}:${index}`, delta: part.text },
            { type: 'TEXT_MESSAGE_END', messageId: `text:${current.id}:${index}` }
          ]
          return []
        }) : []),
        { type: 'RUN_FINISHED', threadId: input.threadId, runId: input.runId,
          outcome: waiting ? { type: 'interrupt', interrupts: [{ id: current.id, reason: 'missing_input' }] } : { type: 'success' } }
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
  await expect(page.getByRole('button', { name: reviewOptions[0].label, exact: true })).toBeVisible()
  await expect(page.getByText(unknownWarning)).toBeVisible()
  await expect(page.getByRole('button', { name: '展开数组', exact: true })).toHaveCount(0)
  await page.reload()
  const details = page.locator('.clarification-details')
  await expect(details).toHaveCount(1)
  for (const detail of await details.all()) await expect(detail).not.toHaveAttribute('open', '')
  await expect(page.getByText(unknownWarning)).toBeVisible()
  await expect(page.getByTestId('task-config-check-report')).toContainText('业务行数据')
  await page.getByText('查看配置详情', { exact: true }).click()
  await expect(details.locator('pre')).toContainText('outdoor_copy')
  await page.setViewportSize({ width: 620, height: 800 })
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByText('已取消，不创建任务，不运行。', { exact: true })).toBeVisible()
  await page.reload()
  await expect(page.getByText('已取消，不创建任务，不运行。', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: reviewOptions[0].label, exact: true })).toHaveCount(0)
  expect(backend.requests).toHaveLength(3)
  expect(new Set(backend.requests.map(input => input.runId)).size).toBe(3) // protocol IDs, not business Run IDs
  expect(backend.unexpected).toEqual([])
})

test('exact checked draft streams and reloads without rebuilding parameters or sending owner requests', async ({ page }) => {
  await page.addInitScript(() => {
    // Observe only rendered DOM, including the transient streaming message before history replaces it.
    window.checkedDraftDomOrders = []
    new MutationObserver(() => {
      const parts = document.querySelector('.live-message .message-parts')
      if (!parts) return
      const order = Array.from(parts.children, child => {
        if (child.matches('.message-content')) return 'text'
        if (child.querySelector('[data-testid="transfer-draft-snapshot"]')) return 'draft'
        if (child.querySelector('[data-testid="task-config-check-report"]')) return 'report'
        return 'other'
      })
      window.checkedDraftDomOrders.push(order)
    }).observe(document, { childList: true, subtree: true })
  })
  const backend = await installBackend(page, { draftReportStatus: 'unknown', checkedDraft: true })
  await page.setViewportSize({ width: 620, height: 900 })
  await page.goto(`/module-ui/agent/sessions/${sessionId}`)
  await page.getByRole('textbox').fill(draftGoal)
  await page.getByRole('button', { name: '发送', exact: true }).click()
  const panel = page.getByTestId('transfer-draft-snapshot')
  await expect(panel).toContainText('readonly_persons')
  await expect(panel).toContainText('不代表已保存任务、已创建目标表或当前运行授权')
  await expect(panel.locator('button')).toHaveCount(0)
  await expect(panel.getByTestId('draft-sourceLocation')).toHaveText('引擎 11 · Outdoor.Persons')
  await expect(panel.getByTestId('draft-targetLocation')).toHaveText('引擎 2 · outdoor.persons_check')
  await expect(panel.getByTestId('draft-applyPolicy')).toHaveText('replace · 运行时先清理再写入')
  await expect(page.getByText(checkExplanation, { exact: true })).toBeVisible()
  const orderedContent = () => panel.evaluate(element => {
    const message = element.closest('.message-row')
    const text = message?.querySelector('.message-content')
    return !!text && !!(element.compareDocumentPosition(text) & Node.DOCUMENT_POSITION_FOLLOWING)
  })
  expect(await orderedContent()).toBe(true)
  expect(await page.evaluate(() => window.checkedDraftDomOrders)).toContainEqual(['draft', 'report', 'text'])
  await panel.locator('summary').filter({ hasText: '精确字段映射' }).click()
  await expect(panel.getByText('json', { exact: true })).toBeVisible()
  await panel.locator('summary').filter({ hasText: '完整预检参数' }).click()
  await expect(panel.locator('pre')).toContainText('"apply_mode": "replace"')
  const parameters = await panel.locator('pre').textContent()
  const reference = await panel.locator('.draft-reference').textContent()
  expect(await panel.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  await page.reload()
  await expect(panel.locator('.draft-reference')).toHaveText(reference)
  await expect(page.getByText(checkExplanation, { exact: true })).toBeVisible()
  expect(await orderedContent()).toBe(true)
  await panel.locator('summary').filter({ hasText: '完整预检参数' }).click()
  await expect(panel.locator('pre')).toHaveText(parameters)
  await expect(panel.locator('button')).toHaveCount(0)
  expect(backend.requests).toHaveLength(1)
  expect(backend.unexpected).toEqual([])
})

for (const [status, title] of [['supported', '已检查定义受支持'], ['unsupported', '发现不支持的定义'], ['unknown', '尚无法确定（不是通过）']]) {
  test(`readonly ${status} report streams and reloads without review, owner request or write`, async ({ page }) => {
    const backend = await installBackend(page, { draftReportStatus: status })
    await page.setViewportSize({ width: 620, height: 900 })
    await page.goto(`/module-ui/agent/sessions/${sessionId}`)
    await page.getByRole('textbox').fill(draftGoal)
    await page.getByRole('button', { name: '发送', exact: true }).click()
    const panel = page.getByTestId('task-config-check-report')
    await expect(panel).toContainText(title)
    await expect(panel).toContainText('不代表执行成功或当前运行授权')
    await panel.locator('.diagnostics > summary').click()
    await expect(panel).toContainText('/config/transforms/0/fields/0')
    await expect(panel.getByText('members', { exact: true }).first()).toBeVisible()
    expect(await panel.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
    await expect(panel.locator('button')).toHaveCount(0)
    await page.reload()
    await expect(panel).toContainText(title)
    await expect(panel.locator('button')).toHaveCount(0)
    expect(backend.requests).toHaveLength(1)
    expect(backend.unexpected).toEqual([])
  })
}

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
