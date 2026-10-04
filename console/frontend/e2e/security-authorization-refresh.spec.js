import { expect, test } from '@playwright/test'

const enrollmentID = '67b1460f-8102-4abc-9e8e-bb265a23206c'
const assessmentID = '8ca44894-dc69-4ce4-8e21-0f02e82bb93d'
const locator = 'addp://engine/11/path/Outdoor/Persons?type=collection&item_id=201'
const prefixAlgorithm = 'addp.mask.keep_prefix_suffix/v2'
const sm3Algorithm = 'addp.mask.sm3/v1'
const permissions = [
  'meta.catalog.read', 'security.enrollment.read', 'security.enrollment.create',
  'security.assessment.read', 'security.policy.read', 'security.policy.create',
  'security.sensitive_data_type.read', 'security.classification.read', 'security.grade.read', 'security.protection_baseline.read'
]

const collection = {
  id: locator, locator, label: 'Persons', type: 'collection', children: [],
  metadata: { item_id: 201, data_type: 'collection' }
}
const database = {
  id: 'addp://engine/11/path/Outdoor?type=database&node_id=20',
  locator: 'addp://engine/11/path/Outdoor?type=database&node_id=20',
  label: 'Outdoor', type: 'database', children: [collection]
}
const enrollment = {
  id: enrollmentID, state: 'active', version: '1',
  target: { owner_module: 'meta', resource_type: 'data_item', resource_identity: 'sha256:persons-fixture' },
  target_snapshot: { engine_id: 11, item_type: 'collection', full_name: 'Outdoor.Persons' },
  discovery_summary: { status: 'completed', finding_count: 1, pending_review_count: 0, reviewed_count: 1 },
  owner_progress: [['manager', 'preview'], ['transfer', 'export'], ['develop', 'query'], ['service', 'service_execute']].map(([consumer_owner, action]) => ({
    consumer_owner, projection_state: 'active', acknowledged: true, rules: [{ action, effect: 'mask' }]
  }))
}
const baseline = {
  id: '40', sensitive_data_type_id: '20', security_grade_id: '3', effect: 'mask',
  algorithm: prefixAlgorithm, parameters: { prefix_runes: 3, suffix_runes: 4, mask_rune: '*' },
  allowed_algorithms: [prefixAlgorithm, sm3Algorithm], invalid_value_effect: 'suppress', enabled: true, version: '1'
}
const paginate = data => ({ data, total: data.length, page: 1, page_size: 100, total_pages: data.length ? 1 : 0 })

async function setup(page, { protectedResource = false } = {}) {
  let generation = 0
  let hold = false
  const pendingContexts = []
  const unexpectedRequests = []
  const writes = []
  const browserErrors = []
  page.on('pageerror', error => browserErrors.push(error.message))
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const context = () => ({
    principal: { id: '17', type: 'user' },
    context: { type: 'tenant', tenant_id: '3', tenant_membership_id: '19' },
    authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' }, permissions }] }
  })
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    if (path === '/api/v1/system/refresh') return route.fulfill({ json: { access_token: `fixture-token-${++generation}`, expires_in: 900 } })
    if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: '17', display_name: 'Security fixture reader' } })
    if (path === '/api/v1/system/auth/context') {
      if (hold) { pendingContexts.push(route); return }
      return route.fulfill({ json: context() })
    }
    if (request.method() !== 'GET') writes.push({ path, body: request.postDataJSON() })
    const responses = {
      '/api/v1/system/engines': [{ id: 11, name: 'Business MongoDB', engine_type: 'mongodb', lifecycle_state: 'active', connection_status: 'online' }],
      '/api/v1/meta/engines': [{ id: 11, name: 'Business MongoDB', engine_type: 'mongodb', lifecycle_state: 'active', connection_status: 'online' }],
      '/api/v1/meta/resource-tree/11': {
        id: 'addp://engine/11/path/?type=database&node_id=19',
        locator: 'addp://engine/11/path/?type=database&node_id=19', label: 'Business MongoDB', type: 'database',
        children: [database]
      },
      '/api/v1/meta/resource-tree/11/node': { children: url.searchParams.get('locator') === database.locator ? [collection] : [database] },
      '/api/v1/meta/items/201': { id: 201, engine_id: 11, name: 'Persons', full_name: 'Outdoor.Persons', item_type: 'collection', fingerprint: 'sha256:persons-fixture', scanned_at: '2026-09-26T09:35:44Z' },
      '/api/v1/security/protection-enrollments': paginate(protectedResource ? [enrollment] : []),
      [`/api/v1/security/protection-enrollments/${enrollmentID}`]: enrollment,
      '/api/v1/security/protection-access-requests/review-queue': paginate([]),
      '/api/v1/security/assessments': paginate([{
        id: assessmentID, enrollment_id: enrollmentID, component_key: 'userInfo.phone', state: 'active', version: '1', current_revision: '1',
        current: { source_kind: 'manual', conclusion: 'sensitive', sensitive_data_type_id: '20', security_classification_id: '1', security_grade_id: '3', rationale: '手机号字段夹具', component: { key: 'userInfo.phone', value_type: 'string' } }
      }]),
      '/api/v1/security/protection-policies': paginate([]),
      '/api/v1/security/sensitive-data-types': [{ id: '20', code: 'phone', name: '手机号', security_classification_id: '1', default_security_grade_id: '3', version: '1' }],
      '/api/v1/security/classifications': [{ id: '1', code: 'personal_information', name: '个人信息', version: '1' }],
      '/api/v1/security/grades': [{ id: '3', code: 'l3', name: '较高风险', risk_order: 3, version: '1' }],
      '/api/v1/security/protection-baselines': [baseline],
      '/api/v1/security/protection-algorithms': [
        { key: prefixAlgorithm, name_i18n_key: 'security.algorithms.prefix.name', description_i18n_key: 'security.algorithms.prefix.description', supported_field_types: ['string'], parameters: ['prefix_runes', 'suffix_runes', 'mask_rune'], output_type: 'string' },
        { key: sm3Algorithm, name_i18n_key: 'security.algorithms.sm3.name', description_i18n_key: 'security.algorithms.sm3.description', supported_field_types: ['string'], parameters: [], output_type: 'string' }
      ]
    }
    if (request.method() === 'GET' && Object.hasOwn(responses, path)) return route.fulfill({ json: responses[path] })
    unexpectedRequests.push(`${request.method()} ${path}`)
    return route.fulfill({ status: 403, json: { error: 'unexpected_request' } })
  })
  await page.goto('/security/protection-enrollments')
  const iframe = page.locator('iframe.module-iframe')
  const frame = page.frameLocator('iframe.module-iframe')
  await expect(frame.getByRole('heading', { name: '受保护资源', exact: true })).toBeVisible()
  const original = page.frames().find(value => value.url().includes('/module-ui/security/'))
  return {
    iframe, frame, original, writes, unexpectedRequests, browserErrors,
    async refresh() {
      hold = true
      await page.evaluate(async () => {
        const { useAuthStore } = await import('/src/store/auth.js')
        await useAuthStore().refreshAccessToken({ force: true })
      })
      await expect(iframe).toHaveCount(1)
      await expect(iframe).toBeHidden()
      await expect.poll(() => pendingContexts.length).toBe(2)
      expect(await original.evaluate(async () => {
        const { useAuthStore } = await import('/module-ui/security/src/store/auth.js')
        return useAuthStore().permissions
      })).toEqual([])
      hold = false
      for (const route of pendingContexts.splice(0)) await route.fulfill({ json: context() })
      await expect(iframe).toBeVisible()
      expect(page.frames().find(value => value.url().includes('/module-ui/security/'))).toBe(original)
    },
    verifyNoWrites() {
      expect(writes).toEqual([])
      expect(unexpectedRequests).toEqual([])
      expect(browserErrors).toEqual([])
    }
  }
}

test('preserves the real MongoDB enrollment selection through authorization refresh', async ({ page }) => {
  const fixture = await setup(page)
  await fixture.frame.getByRole('button', { name: '纳入数据保护', exact: true }).first().click()
  const drawer = fixture.frame.getByRole('dialog', { name: '纳入数据保护', exact: true })
  await drawer.locator('.resource-tree-picker .el-select__wrapper').click()
  await fixture.frame.getByRole('option', { name: 'Business MongoDB (mongodb) · 在线', exact: true }).click()
  await drawer.getByRole('treeitem', { name: 'Outdoor', exact: true }).locator('.el-tree-node__expand-icon').click()
  await drawer.getByRole('treeitem', { name: 'Persons', exact: true }).click()
  await expect(drawer.locator('.selection-card')).toContainText('Outdoor.Persons')
  await fixture.refresh()
  await expect(drawer).toBeVisible()
  await expect(drawer.locator('.selection-card')).toContainText('Outdoor.Persons')
  await expect(drawer.getByRole('button', { name: '确认纳入保护', exact: true })).toBeEnabled()
  fixture.verifyNoWrites()
})

for (const algorithm of ['prefix', 'sm3']) {
  test(`preserves the real userInfo.phone ${algorithm} policy draft through authorization refresh`, async ({ page }) => {
    const fixture = await setup(page, { protectedResource: true })
    await fixture.frame.getByRole('button', { name: '查看详情', exact: true }).click()
    await fixture.frame.getByRole('button', { name: '收紧保护', exact: true }).click()
    const dialog = fixture.frame.getByRole('dialog', { name: '资源级保护策略', exact: true })
    await expect(dialog).toContainText('userInfo.phone')
    await dialog.getByText('脱敏', { exact: true }).click()
    const field = label => dialog.locator('.el-form-item').filter({ hasText: label })
    if (algorithm === 'sm3') {
      await field('脱敏算法').locator('.el-select__wrapper').click()
      await fixture.frame.getByRole('option', { name: 'SM3 哈希', exact: true }).click()
    } else {
      await field('保留前部字符数').getByRole('spinbutton').fill('1')
      await field('保留后部字符数').getByRole('spinbutton').fill('2')
      await field('掩码字符').getByRole('textbox').fill('#')
    }
    await field('调整依据').getByRole('textbox').fill('保留未提交的手机号字段保护草稿')
    await fixture.refresh()
    await expect(dialog).toBeVisible()
    await expect(field('调整依据').getByRole('textbox')).toHaveValue('保留未提交的手机号字段保护草稿')
    if (algorithm === 'sm3') await expect(field('脱敏算法')).toContainText('SM3 哈希')
    else {
      await expect(field('保留前部字符数').getByRole('spinbutton')).toHaveValue('1')
      await expect(field('保留后部字符数').getByRole('spinbutton')).toHaveValue('2')
      await expect(field('掩码字符').getByRole('textbox')).toHaveValue('#')
    }
    await expect(dialog.getByRole('button', { name: '保存策略', exact: true })).toBeEnabled()
    fixture.verifyNoWrites()
  })
}
