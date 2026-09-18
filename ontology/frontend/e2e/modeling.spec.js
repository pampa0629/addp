import { test, expect } from '@playwright/test'
import { installBackend, root, editor, allPermissions } from './fixture.js'

test('committed 502 publication is reconciled as published and can only rebuild the failed generation', async ({
  page,
  context
}) => {
  const state = await installBackend(context, { status: 'in_review' })
  await page.goto(editor)
  state.failWrite = 'committed-admission'
  await page.getByRole('button', { name: '发布修订', exact: true }).click()
  await confirm(page)
  await expect(
    page.getByText('admission failed', { exact: true })
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: '发布修订', exact: true })
  ).toBeDisabled()
  expect(state.writes).toHaveLength(1)
  await page.getByRole('button', { name: '重新加载', exact: true }).click()
  await expect(page.getByText('已发布', { exact: true })).toBeVisible()
  await expect(page.getByText('构建失败', { exact: true })).toBeVisible()
  await expect(
    page.getByRole('button', { name: '发布修订', exact: true })
  ).toHaveCount(0)
  await page.getByRole('button', { name: '重建失败投影', exact: true }).click()
  await confirm(page)
  await expect(page.getByText('等待构建', { exact: true })).toBeVisible()
  expect(state.writes[1].body).toEqual({
    version: 2,
    failed_generation: 'g1',
    activation_version: 0
  })
})

test('standalone session expiry stays on the module login route', async ({
  page,
  context
}) => {
  const state = await installBackend(context)
  await page.goto(editor)
  await expect(page.getByTestId('classes-member')).toBeVisible()
  state.sessionExpired = true
  await page.getByRole('button', { name: '重新加载', exact: true }).click()
  await expect(page).toHaveURL(/\/ontology\/login\?redirect=/)
  await expect(
    page.getByRole('heading', { name: '领域本体', exact: true })
  ).toBeVisible()
  expect(state.writes).toHaveLength(0)
})

test('cancelling logout preserves both the session and dirty editor', async ({
  page,
  context
}) => {
  const state = await installBackend(context)
  await page.goto(editor)
  await field(page.getByTestId('classes-member'), '名称').fill('未保存')
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await page
    .getByRole('dialog')
    .getByRole('button', { name: '继续编辑', exact: true })
    .click()
  await expect(page).toHaveURL(editor)
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect(
    page.getByRole('alert').getByText('已保存', { exact: true })
  ).toBeVisible()
  expect(state.unexpected).toEqual([])
})

const confirm = (page) =>
  page
    .getByRole('dialog')
    .getByRole('button', { name: '确认', exact: true })
    .click()
const field = (parent, label) =>
  parent.getByRole('textbox', {
    name: new RegExp(`^\\*?${label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`)
  })
async function choose(page, parent, label, value) {
  const select = parent.getByRole('combobox', {
    name: new RegExp(`^\\*?${label}$`)
  })
  await select.focus()
  await select.press('Enter')
  const id = await select.getAttribute('aria-controls')
  await page
    .locator(`#${id}`)
    .getByRole('option', { name: value, exact: true })
    .click()
}

test('create, save, review, publish, activate and withdraw use precise versions', async ({
  page,
  context
}) => {
  const state = await installBackend(context, { empty: true })
  await page.goto(root)
  await page.getByRole('button', { name: '新建本体', exact: true }).click()
  await field(page, '本体标识').fill('beijing_outdoor')
  await page.getByRole('button', { name: '添加类型', exact: true }).click()
  const member = page.getByTestId('classes-member')
  await field(member, '成员标识').fill('activity')
  await field(member, '名称').fill('北京徒步活动')
  await page.getByRole('button', { name: '创建草稿', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(editor + '$'))
  expect(state.writes[0].body).toEqual({
    revision: 1,
    definition: {
      classes: [{ id: 'activity', name: '北京徒步活动', parents: [] }],
      properties: [],
      relations: [],
      rules: []
    }
  })
  await field(member, '名称').fill('北京户外活动')
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  expect(state.writes[1].body.version).toBe(1)
  await page.getByRole('button', { name: '提交审核', exact: true }).click()
  await confirm(page)
  await expect(
    page.getByRole('button', { name: '发布修订', exact: true })
  ).toBeVisible()
  expect(state.writes[2].body.version).toBe(2)
  await page.getByRole('button', { name: '发布修订', exact: true }).click()
  await confirm(page)
  await expect(page.getByText('等待构建', { exact: true })).toBeVisible()
  await expect(page.getByText('当前生效', { exact: true })).toHaveCount(0)
  expect(state.writes[3].body.version).toBe(3)
  state.activate()
  await page.getByRole('button', { name: '重新加载', exact: true }).click()
  await expect(page.getByText('当前生效', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '撤回修订', exact: true }).click()
  await confirm(page)
  await expect(page.getByText('已撤回', { exact: true })).toBeVisible()
  await expect(page.getByText('未生效', { exact: true })).toBeVisible()
  expect(state.head.active_revision).toBeNull()
  expect(state.unexpected).toEqual([])
  expect(state.headers.every((lang) => lang === 'zh-cn')).toBe(true)
})

test('all native member forms edit one draft; tab refresh preserves canonical route', async ({
  page,
  context
}) => {
  const state = await installBackend(context)
  await page.goto(editor)
  for (const [section, title] of [
    ['properties', '属性'],
    ['relations', '关系'],
    ['rules', '规则']
  ]) {
    await page.getByRole('tab', { name: new RegExp(`^${title}`) }).click()
    await page
      .getByRole('button', { name: `添加${title}`, exact: true })
      .click()
    await field(page.getByTestId(`${section}-member`), '成员标识').fill(
      `${section}_one`
    )
    const member = page.getByTestId(`${section}-member`)
    if (section !== 'rules')
      await field(member, '名称').fill(
        section === 'properties' ? '城市' : '关联活动'
      )
    if (section === 'properties' || section === 'rules')
      await choose(page, member, '所属类型', '北京户外活动 (activity)')
    if (section === 'properties') await field(member, '属性键').fill('city')
    if (section === 'relations') {
      await choose(page, member, '起点类型', '北京户外活动 (activity)')
      await choose(page, member, '终点类型', '北京户外活动 (activity)')
    }
  }
  const rule = page.getByTestId('rules-member')
  await field(rule, '分类表达式（CEL）').fill('city == "北京市"')
  await field(rule, '规则依据').fill('明确城市条件')
  await rule.getByRole('button', { name: '添加输入', exact: true }).click()
  await field(rule, '变量名').fill('city')
  await choose(page, rule, '绑定属性', '城市 (properties_one)')
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  const definition = state.writes[0].body.definition
  expect(definition.properties).toHaveLength(1)
  expect(definition.relations).toHaveLength(1)
  expect(definition.rules[0].inputs[0].on_absent).toBe('unknown')
  await page.reload()
  await expect(page.getByRole('tab', { name: /^规则/ })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  await expect(field(rule, '分类表达式（CEL）')).toHaveValue('city == "北京市"')
  expect(state.unexpected).toEqual([])
})

test('conflict preserves local draft and blocks repeat write until explicit reload', async ({
  page,
  context
}) => {
  const state = await installBackend(context)
  await page.goto(editor)
  const name = field(page.getByTestId('classes-member'), '名称')
  await name.fill('未保存的修改')
  state.failWrite = 409
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect(
    page.getByText('version conflict', { exact: true })
  ).toBeVisible()
  await expect(name).toHaveValue('未保存的修改')
  await expect(
    page.getByRole('button', { name: '保存草稿', exact: true })
  ).toBeDisabled()
  expect(state.writes).toHaveLength(1)
  await page.getByRole('button', { name: '重新加载', exact: true }).click()
  await page
    .getByRole('dialog')
    .getByRole('button', { name: '取消', exact: true })
    .click()
  await expect(name).toHaveValue('未保存的修改')
  await page.getByRole('button', { name: '重新加载', exact: true }).click()
  await confirm(page)
  await expect(name).toHaveValue('北京户外活动')
  expect(state.writes).toHaveLength(1)
})

test('failed admission is never replayed; failed projection rebuild has explicit baseline', async ({
  page,
  context
}) => {
  const state = await installBackend(context, {
    status: 'published',
    projection: 'failed'
  })
  await page.goto(editor)
  state.failWrite = 502
  await page.getByRole('button', { name: '重建失败投影', exact: true }).click()
  await confirm(page)
  await expect(
    page.getByText('admission failed', { exact: true })
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: '重建失败投影', exact: true })
  ).toBeDisabled()
  expect(state.writes[0].body).toEqual({
    version: 1,
    failed_generation: 'g1',
    activation_version: 0
  })
  await page.getByRole('button', { name: '重新加载', exact: true }).click()
  await page.getByRole('button', { name: '重建失败投影', exact: true }).click()
  await confirm(page)
  await expect(page.getByText('等待构建', { exact: true })).toBeVisible()
  expect(state.writes).toHaveLength(2)
  expect(state.unexpected).toEqual([])
})

test('transport failure locks save without replaying the write', async ({
  page,
  context
}) => {
  const state = await installBackend(context)
  await page.goto(editor)
  await field(page.getByTestId('classes-member'), '名称').fill('修改')
  state.failWrite = 'network'
  await page.getByRole('button', { name: '保存草稿', exact: true }).click()
  await expect(
    page.getByRole('button', { name: '保存草稿', exact: true })
  ).toBeDisabled()
  await expect(
    page.getByText('写入结果可能已变更。', { exact: false })
  ).toBeVisible()
  expect(state.writes).toHaveLength(1)
})

test('read permission permits browse only and ready projection is not necessarily active', async ({
  page,
  context
}) => {
  const state = await installBackend(context, {
    permissions: ['ontology.revision.read'],
    status: 'published',
    projection: 'ready'
  })
  await page.goto(editor)
  await expect(
    page.getByText('已就绪（非当前生效）', { exact: true })
  ).toBeVisible()
  await expect(field(page.getByTestId('classes-member'), '名称')).toBeDisabled()
  await expect(
    page.getByRole('button', { name: '撤回修订', exact: true })
  ).toHaveCount(0)
  await page.getByRole('button', { name: '返回', exact: true }).click()
  await expect(page).toHaveURL(root + '/beijing_outdoor')
  await expect(
    page.getByRole('button', { name: '创建下一修订', exact: true })
  ).toHaveCount(0)
  expect(state.writes).toEqual([])
})

test('publication requires execution authorization in addition to publish permission', async ({
  page,
  context
}) => {
  await installBackend(context, {
    permissions: allPermissions.filter(
      (p) => p !== 'system.execution_authorization.create'
    ),
    status: 'in_review'
  })
  await page.goto(editor)
  await expect(page.getByText('审核中', { exact: true })).toBeVisible()
  await expect(
    page.getByRole('button', { name: '发布修订', exact: true })
  ).toHaveCount(0)
  await page.getByRole('button', { name: '退回草稿', exact: true }).click()
  await confirm(page)
  await expect(page.getByText('草稿', { exact: true })).toBeVisible()
})

test('copy next revision remains in ontology owner and history restores identity', async ({
  page,
  context
}) => {
  const state = await installBackend(context, { status: 'published' })
  await page.goto(root + '/beijing_outdoor')
  await page.getByRole('button', { name: '创建下一修订', exact: true }).click()
  await confirm(page)
  await expect(page).toHaveURL(root + '/beijing_outdoor/revisions/2')
  expect(state.writes[0].body.revision).toBe(2)
  expect(state.writes[0].body.definition.scope).toBeUndefined()
  await page.goBack()
  await expect(page).toHaveURL(root + '/beijing_outdoor')
  await page.goForward()
  await expect(page).toHaveURL(root + '/beijing_outdoor/revisions/2')
  await expect(page.getByTestId('classes-member')).toContainText('名称')
})

test('unsaved guard protects navigation; narrow layout and English remain usable', async ({
  page,
  context
}) => {
  await installBackend(context, { locale: 'en' })
  await page.setViewportSize({ width: 680, height: 900 })
  await page.goto(editor)
  await field(page.getByTestId('classes-member'), 'Name').fill('Changed')
  await page.getByRole('button', { name: 'Back', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await expect(page).toHaveURL(editor)
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth + 1
    )
  ).toBe(true)
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Keep editing', exact: true })
    .click()
  await page.screenshot({
    path: '/tmp/addp-ontology-editor-narrow.png',
    fullPage: true,
    animations: 'disabled'
  })
})

test('non-tenant contexts fail closed without ontology requests', async ({
  page,
  context
}) => {
  const state = await installBackend(context, { contextType: 'platform' })
  await page.goto(editor)
  await expect(
    page.getByText('需要租户会话及本体读取权限。', { exact: true })
  ).toBeVisible()
  expect(state.headers).toEqual([])
  expect(state.writes).toEqual([])
})

test('invalid revision identity fails closed and unknown tabs canonicalize without data loss', async ({
  page,
  context
}) => {
  const state = await installBackend(context)
  await page.goto(root + '/beijing_outdoor/revisions/01')
  await expect(
    page.getByText('本体或修订标识无效。', { exact: true })
  ).toBeVisible()
  expect(state.headers).toEqual([])
  await page.goto(editor + '?tab=unknown&token=bad')
  await expect(page).toHaveURL(editor)
  await expect(page.getByTestId('classes-member')).toBeVisible()
})

test('runtime language and theme changes preserve the draft and language header', async ({
  page,
  context
}) => {
  const state = await installBackend(context)
  await page.goto(editor)
  await field(page.getByTestId('classes-member'), '名称').fill('北京登山')
  await page.getByRole('button', { name: 'CN', exact: true }).click()
  await page.getByRole('menuitem', { name: 'English', exact: true }).click()
  await expect(
    page.getByRole('button', { name: 'Save draft', exact: true })
  ).toBeVisible()
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await page.getByRole('button', { name: 'Save draft', exact: true }).click()
  await expect(
    page.getByRole('alert').getByText('Saved', { exact: true })
  ).toBeVisible()
  expect(state.headers.at(-1)).toBe('en')
  await expect(
    page.getByRole('button', { name: 'Save draft', exact: true })
  ).toBeDisabled()
  await page.screenshot({
    path: '/tmp/addp-ontology-editor-dark.png',
    fullPage: true,
    animations: 'disabled'
  })
})
