import { test, expect } from '@playwright/test'
import { installBackend, allPermissions, root } from './fixture.js'

const path = `${root}/beijing_outdoor/trial?class_id=activity&rule_id=valid_date`
const binding = {
  ontology_id: 'beijing_outdoor',
  revision: 1,
  generation: 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee',
  activation_version: 2,
  digest: 'a'.repeat(64),
  knowledge_kind: 'native_definition'
}
const rule = {
  id: 'valid_date',
  class_id: 'activity',
  expression: 'has_date',
  basis: '测试夹具的日期存在性规则',
  inputs: [{ variable: 'has_date', property_id: 'date', on_absent: 'false' }]
}
async function fixture(context, options = {}) {
  const backend = await installBackend(context, {
    permissions: [...allPermissions, 'ontology.semantic.read'],
    ...options
  })
  const state = {
    backend,
    expireOnce: false,
    outcome: 'matched',
    code: 'predicate_true',
    requests: [],
    fail: false,
    hold: null,
    empty: false,
    mismatch: false,
    property: { id: 'date', name: '有日期', kind: 'bool', enum: [] }
  }
  await context.route('**/api/v1/ontology/**/semantic/**', async (route) => {
    const req = route.request(),
      url = new URL(req.url())
    const send = (body, status = 200) =>
      route.fulfill({
        status,
        contentType: 'application/json',
        body: JSON.stringify(body)
      })
    if (url.pathname.endsWith('/classes'))
      return send({
        ...binding,
        classes: [{ id: 'activity', name: '活动', parents: [] }]
      })
    if (url.pathname.endsWith('/classes/activity')) {
      expect(Object.fromEntries(url.searchParams)).toEqual({
        revision: '1',
        generation: binding.generation,
        activation_version: '2'
      })
      return send({
        ...binding,
        class: { id: 'activity', name: '活动', parents: [] },
        ancestors: [],
        relations: [],
        properties: [state.property],
        rules: state.empty ? [] : [rule]
      })
    }
    expect(url.pathname.endsWith('/rules/valid_date/trial')).toBe(true)
    const body = req.postDataJSON()
    state.requests.push(body)
    if (state.expireOnce) {
      state.expireOnce = false
      return send({ error: 'expired' }, 401)
    }
    expect(body.revision).toBe(1)
    expect(body.generation).toBe(binding.generation)
    expect(body.activation_version).toBe(2)
    if (state.hold) await state.hold
    if (state.fail)
      return send(
        { error: '激活版本已变化', error_code: 'ontology_activation_changed' },
        409
      )
    return send({
      ...binding,
      revision: state.mismatch ? 2 : 1,
      decision: {
        mode: 'hypothetical',
        outcome: state.outcome,
        code: state.code,
        rule_id: rule.id,
        digest: binding.digest,
        inputs: [
          {
            variable: 'has_date',
            property_id: 'date',
            state: body.inputs.has_date.state,
            treatment:
              body.inputs.has_date.state === 'known' ? 'value' : 'unresolved'
          }
        ],
        unresolved: state.outcome === 'unknown' ? ['has_date'] : []
      }
    })
  })
  return state
}
async function choose(page, label, name) {
  const select = page.getByRole('combobox', { name: label, exact: true })
  await select.focus()
  await select.press('Enter')
  const id = await select.getAttribute('aria-controls')
  await page
    .locator(`#${id}`)
    .getByRole('option', { name, exact: true })
    .click()
}
const chooseState = (page, name) => choose(page, '输入状态', name)
test('token rotation during a trial preserves inputs for the same authenticated owner', async ({
  page,
  context
}) => {
  const state = await fixture(context)
  await page.goto(path)
  await chooseState(page, '已知值')
  await choose(page, '假设值', '是')
  let releaseAuth
  state.backend.authHold = new Promise((resolve) => {
    releaseAuth = resolve
  })
  state.expireOnce = true
  await page.getByRole('button', { name: '开始试算', exact: true }).click()
  await expect.poll(() => state.backend.refreshCount).toBe(2)
  await expect(page.getByTestId('trial-input')).toHaveCount(0)
  await expect(page.getByTestId('trial-result')).toHaveCount(0)
  releaseAuth()
  state.backend.authHold = null
  await expect(page.getByTestId('trial-input')).toContainText('已知值')
  await expect(
    page.getByRole('combobox', { name: '假设值', exact: true })
  ).toBeVisible()
  await expect(page.getByTestId('trial-input')).toContainText('是')
  expect(state.requests).toHaveLength(1)
  await page.getByRole('button', { name: '开始试算', exact: true }).click()
  await expect(page.getByTestId('trial-result')).toContainText('匹配')
  expect(state.requests.at(-1).inputs.has_date).toEqual({
    state: 'known',
    value: true
  })
})
for (const change of [
  'principal',
  'tenant',
  'membership',
  'permission',
  'auth_failure',
  'logout'
]) {
  test(`reauthorization clears old trial data on ${change}`, async ({
    page,
    context
  }) => {
    const state = await fixture(context)
    await page.goto(path)
    await chooseState(page, '已知值')
    await choose(page, '假设值', '是')
    const auth = state.backend.authContext
    if (change === 'principal') auth.principal.id = '8'
    if (change === 'tenant') auth.context.tenant_id = '8'
    if (change === 'membership') auth.context.tenant_membership_id = '80'
    if (change === 'permission') auth.authorization.role_assignments = []
    if (change === 'auth_failure') state.backend.authFailure = true
    if (change === 'logout') state.backend.sessionExpired = true
    state.expireOnce = true
    await page.getByRole('button', { name: '开始试算', exact: true }).click()
    if (['principal', 'tenant', 'membership'].includes(change)) {
      await expect.poll(() => state.backend.refreshCount).toBe(2)
      await expect(page.getByTestId('trial-input')).toContainText(
        '未知（未读取）'
      )
      await expect(
        page.getByRole('combobox', { name: '假设值', exact: true })
      ).toHaveCount(0)
    } else {
      await expect(page.getByTestId('trial-input')).toHaveCount(0)
      await expect(
        page.getByRole('button', { name: '开始试算', exact: true })
      ).toHaveCount(0)
      if (change === 'logout')
        await expect(page).toHaveURL(/\/ontology\/login\?redirect=/)
      else
        await expect(
          page.getByText('需要租户会话及本体语义读取权限。', { exact: true })
        ).toBeVisible()
    }
    await expect(page.getByTestId('trial-result')).toHaveCount(0)
    expect(state.requests).toHaveLength(1)
  })
}
test('a version conflict remains blocked after a same-owner token refresh', async ({
  page,
  context
}) => {
  const state = await fixture(context)
  await page.goto(path)
  await chooseState(page, '已确认缺失')
  state.fail = true
  await page.getByRole('button', { name: '开始试算', exact: true }).click()
  await expect(
    page.getByRole('button', { name: '开始试算', exact: true })
  ).toBeDisabled()
  // Exercise the real shared auth store with only intercepted test credentials.
  await page.evaluate(async () => {
    const { useAuthStore } = await import('/ontology/src/store/auth.js')
    await useAuthStore().refreshAuthorization()
  })
  await expect(page.getByTestId('trial-input')).toContainText('已确认缺失')
  await expect(
    page.getByRole('button', { name: '开始试算', exact: true })
  ).toBeDisabled()
  expect(state.requests).toHaveLength(1)
})
test('token refresh invalidates a late result without losing the current hypothesis', async ({
  page,
  context
}) => {
  const state = await fixture(context)
  await page.goto(path)
  await chooseState(page, '已确认缺失')
  let release
  state.hold = new Promise((resolve) => {
    release = resolve
  })
  await page.getByRole('button', { name: '开始试算', exact: true }).click()
  await expect.poll(() => state.requests.length).toBe(1)
  await page.evaluate(async () => {
    const { useAuthStore } = await import('/ontology/src/store/auth.js')
    await useAuthStore().refreshAuthorization()
  })
  release()
  state.hold = null
  await expect(page.getByTestId('trial-input')).toContainText('已确认缺失')
  await expect(page.getByTestId('trial-result')).toHaveCount(0)
  state.outcome = 'not_matched'
  state.code = 'predicate_false'
  await page.getByRole('button', { name: '开始试算', exact: true }).click()
  await expect(
    page.getByTestId('trial-result').getByText('不匹配', { exact: true })
  ).toBeVisible()
  expect(state.requests).toHaveLength(2)
})
test('string inputs preserve enum values and unrestricted text', async ({
  page,
  context
}) => {
  const state = await fixture(context)
  state.property = {
    id: 'date',
    name: '状态',
    kind: 'string',
    enum: ['报名中', '浏览中']
  }
  await page.goto(path)
  await chooseState(page, '已知值')
  await choose(page, '假设值', '报名中')
  await page.getByRole('button', { name: '开始试算', exact: true }).click()
  await expect(page.getByTestId('trial-result')).toBeVisible()
  expect(state.requests[0].inputs.has_date).toEqual({
    state: 'known',
    value: '报名中'
  })

  state.property.enum = []
  await page.reload()
  await chooseState(page, '已知值')
  await page
    .getByRole('textbox', { name: '假设值', exact: true })
    .fill(' 保留原始输入 ')
  await page.getByRole('button', { name: '开始试算', exact: true }).click()
  await expect(page.getByTestId('trial-result')).toBeVisible()
  expect(state.requests[1].inputs.has_date).toEqual({
    state: 'known',
    value: ' 保留原始输入 '
  })
})
test('four trial outcomes, explicit inputs, and stale-result clearing', async ({
  page,
  context
}) => {
  const state = await fixture(context)
  await page.goto(path)
  await expect(page.getByText('测试夹具的日期存在性规则')).toBeVisible()
  const result = page.getByTestId('trial-result')
  for (const [input, outcome, code, label] of [
    ['未知（未读取）', 'unknown', 'insufficient_evidence', '未知'],
    ['已确认缺失', 'not_matched', 'predicate_false', '不匹配'],
    ['无效（证据不可用）', 'error', 'invalid_fact_state', '试算错误'],
    ['已知值', 'matched', 'predicate_true', '匹配']
  ]) {
    await chooseState(page, input)
    await expect(result).toHaveCount(0)
    if (input === '已知值') {
      await choose(page, '假设值', '是')
    }
    state.outcome = outcome
    state.code = code
    await page.getByRole('button', { name: '开始试算', exact: true }).click()
    await expect(result.getByText(label, { exact: true })).toBeVisible()
    await expect(result).toContainText('不是已核实业务事实')
  }
  expect(state.requests.map((r) => r.inputs.has_date)).toEqual([
    { state: 'unknown' },
    { state: 'absent' },
    { state: 'invalid' },
    { state: 'known', value: true }
  ])
  await chooseState(page, '未知（未读取）')
  await expect(result).toHaveCount(0)
})
test('conflict preserves inputs, blocks repeated trials and requires explicit reload', async ({
  page,
  context
}) => {
  const state = await fixture(context)
  await page.goto(path)
  await chooseState(page, '已确认缺失')
  state.fail = true
  await page.getByRole('button', { name: '开始试算', exact: true }).click()
  await expect(page.getByText('激活版本已变化', { exact: true })).toBeVisible()
  await expect(
    page.getByRole('button', { name: '开始试算', exact: true })
  ).toBeDisabled()
  await expect(page.getByTestId('trial-input')).toContainText('已确认缺失')
  expect(state.requests).toHaveLength(1)
  state.fail = false
  await page.getByRole('button', { name: '重新加载', exact: true }).click()
  await expect(
    page.getByRole('button', { name: '开始试算', exact: true })
  ).toBeEnabled()
  await expect(page.getByTestId('trial-input')).toContainText('未知（未读取）')
})
test('input changes discard late responses and allow a fresh trial', async ({
  page,
  context
}) => {
  const state = await fixture(context)
  let release
  state.hold = new Promise((resolve) => {
    release = resolve
  })
  await page.goto(path)
  await page.getByRole('button', { name: '开始试算', exact: true }).click()
  await expect.poll(() => state.requests.length).toBe(1)
  await chooseState(page, '已确认缺失')
  release()
  state.hold = null
  await expect(page.getByTestId('trial-result')).toHaveCount(0)
  state.outcome = 'not_matched'
  state.code = 'predicate_false'
  await page.getByRole('button', { name: '开始试算', exact: true }).click()
  await expect(
    page.getByTestId('trial-result').getByText('不匹配', { exact: true })
  ).toBeVisible()
})
test('denies mismatched result binding; empty and unauthorized pages cannot execute', async ({
  page,
  context
}) => {
  const state = await fixture(context)
  state.mismatch = true
  await page.goto(path)
  await page.getByRole('button', { name: '开始试算', exact: true }).click()
  await expect(
    page.getByRole('button', { name: '开始试算', exact: true })
  ).toBeDisabled()
  await expect(page.getByTestId('trial-result')).toHaveCount(0)
  state.empty = true
  await page.goto(`${root}/beijing_outdoor/trial?class_id=activity`)
  await expect(page.getByText('该类型没有直接绑定的规则')).toBeVisible()
  await expect(
    page.getByRole('button', { name: '开始试算', exact: true })
  ).toHaveCount(0)
})
test('semantic permission required; English and narrow layout supported', async ({
  page,
  context
}) => {
  await fixture(context, { permissions: allPermissions, locale: 'en' })
  await page.goto(path)
  await expect(
    page.getByText(
      'A tenant session and ontology semantic read permission are required.'
    )
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Run trial', exact: true })
  ).toHaveCount(0)
})
test('entry navigation, reload restoration, English labels and narrow width', async ({
  page,
  context
}) => {
  const state = await installBackend(context, {
    status: 'published',
    permissions: [...allPermissions, 'ontology.semantic.read'],
    locale: 'en'
  })
  state.activate()
  await page.goto(`${root}/beijing_outdoor`)
  await expect(
    page.getByRole('button', { name: 'Ontology rule trial', exact: true })
  ).toBeEnabled()
  // Keep all semantic calls intercepted by the shared fixture, never personal services.
  await fixture(context, { locale: 'en' })
  await page
    .getByRole('button', { name: 'Ontology rule trial', exact: true })
    .click()
  await choose(page, 'Class', '活动 (activity)')
  await choose(page, 'Trial rule', 'valid_date')
  await expect(page).toHaveURL(
    new RegExp('class_id=activity&rule_id=valid_date')
  )
  await page.reload()
  await expect(page.getByText('测试夹具的日期存在性规则')).toBeVisible()
  await page.setViewportSize({ width: 620, height: 860 })
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth
    )
  ).toBe(true)
})
