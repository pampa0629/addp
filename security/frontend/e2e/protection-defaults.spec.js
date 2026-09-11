import { expect, test } from '@playwright/test'

const enrollmentID = '67b1460f-8102-4abc-9e8e-bb265a23206c'
const assessmentID = '8ca44894-dc69-4ce4-8e21-0f02e82bb93d'
const policyID = 'bca63b32-b670-4df9-bbcc-fe6867df6c0f'

test('creates a sensitive definition, revises its protected-resource decision, and tightens protection', async ({ page }) => {
  const backend = await installMockBackend(page)
  const browserErrors = []
  page.on('pageerror', error => browserErrors.push(error.message))

  await page.goto('/sensitive-data-definitions')
  await page.getByRole('button', { name: '新增敏感类型' }).click()

  const createDialog = page.getByRole('dialog', { name: '新增敏感类型' })
  await formTextbox(createDialog, '编码').fill('customer_phone')
  await formTextbox(createDialog, '名称').fill('客户手机号')
  await formTextbox(createDialog, '说明').fill('客户联系号码')
  await expect(createDialog.getByText('初始默认保护', { exact: true })).toBeVisible()
  await expect(createDialog.getByRole('radio', { name: '遮盖' })).toBeChecked()
  await createDialog.getByRole('button', { name: '保存' }).click()

  await expect.poll(() => backend.typeCreateRequests.length).toBe(1)
  expect(backend.typeCreateRequests[0]).toMatchObject({
    code: 'customer_phone',
    name: '客户手机号',
    security_classification_id: 1,
    default_security_grade_id: 3,
    default_protection: {
      effect: 'mask',
      algorithm: 'addp.mask.keep_prefix_suffix/v2',
      keep_prefix: 3,
      keep_suffix: 4,
      invalid_value_effect: 'suppress'
    }
  })
  await expect(page.getByRole('row', { name: /客户手机号/ })).toContainText('遮盖')

  await page.getByRole('row', { name: /客户手机号/ }).getByRole('button', { name: '遮盖' }).click()
  const baselineDrawer = page.locator('.el-drawer').filter({ hasText: '管理默认保护' })
  await expect(baselineDrawer.getByText('初始规则', { exact: true })).toBeVisible()
  await expect(baselineDrawer.getByRole('row').filter({ hasText: '初始规则' }).getByRole('button', { name: '删除' })).toHaveCount(0)

  await page.goto('/protection-enrollments')
  await page.getByRole('button', { name: '查看详情' }).click()
  const detailDrawer = page.locator('.el-drawer').filter({ hasText: '资源保护详情' })
  await expect(detailDrawer.getByText('customer.phone', { exact: true })).toBeVisible()
  await expect(detailDrawer.getByText('数据预览执行默认保护：遮盖', { exact: true })).toBeVisible()

  await detailDrawer.getByRole('button', { name: '调整结论' }).click()
  const revisionDialog = page.getByRole('dialog', { name: '调整正式安全结论' })
  await expect(revisionDialog.getByText('客户手机号 · 个人信息 · 较高风险', { exact: true })).toBeVisible()
  await formCombobox(revisionDialog, '安全等级').click()
  await page.getByRole('option', { name: '高风险', exact: true }).click()
  await formTextbox(revisionDialog, '调整依据').fill('复核业务影响后提升保护等级')
  await revisionDialog.getByRole('button', { name: '保存调整' }).click()

  await expect.poll(() => backend.assessmentRevisionRequests.length).toBe(1)
  expect(backend.assessmentRevisionRequests[0]).toEqual({
    version: 1,
    sensitive_data_type_id: 20,
    security_grade_id: 4,
    rationale: '复核业务影响后提升保护等级'
  })
  await expect(detailDrawer.getByText('客户手机号 · 个人信息 · 高风险', { exact: true })).toBeVisible()
  await expect(detailDrawer.getByText('复核业务影响后提升保护等级', { exact: true })).toBeVisible()

  await detailDrawer.getByRole('button', { name: '收紧保护' }).click()

  const policyDialog = page.getByRole('dialog', { name: '资源级保护策略' })
  await expect(policyDialog.getByRole('textbox', { name: '生效出口' })).toHaveValue('数据预览')
  await expect(policyDialog.getByRole('radio', { name: '移除' })).toBeChecked()
  await formTextbox(policyDialog, '调整依据').fill('该客户表仅允许展示非联系方式字段')
  await policyDialog.getByRole('button', { name: '保存策略' }).click()

  await expect.poll(() => backend.policyCreateRequests.length).toBe(1)
  expect(backend.policyCreateRequests[0]).toEqual({
    assessment_id: assessmentID,
    consumer_owner: 'manager',
    action: 'preview',
    effect: 'suppress',
    rationale: '该客户表仅允许展示非联系方式字段'
  })
  await expect(detailDrawer.getByText('数据预览：默认 遮盖，资源策略收紧为 移除', { exact: true })).toBeVisible()

  await detailDrawer.getByRole('button', { name: '恢复默认' }).click()
  const restoreDialog = page.getByRole('dialog', { name: '恢复默认保护' })
  await restoreDialog.getByRole('textbox').fill('专项处理结束，恢复平台默认规则')
  await restoreDialog.getByRole('button', { name: '确认恢复' }).click()

  await expect.poll(() => backend.policyRevokeRequests.length).toBe(1)
  expect(backend.policyRevokeRequests[0]).toEqual({
    version: 1,
    rationale: '专项处理结束，恢复平台默认规则'
  })
  await expect(detailDrawer.getByText('数据预览执行默认保护：遮盖', { exact: true })).toBeVisible()
  expect(backend.unhandledRequests).toEqual([])
  expect(browserErrors).toEqual([])
})

function formTextbox(container, label) {
  return container.locator('.el-form-item').filter({ hasText: label }).getByRole('textbox')
}

function formCombobox(container, label) {
  return container.locator('.el-form-item').filter({ hasText: label }).locator('.el-select__wrapper')
}

async function installMockBackend(page) {
  const permissions = [
    'security.sensitive_data_type.read',
    'security.sensitive_data_type.create',
    'security.sensitive_data_type.update',
    'security.sensitive_data_type.delete',
    'security.detector.read',
    'security.protection_baseline.read',
    'security.protection_baseline.create',
    'security.protection_baseline.update',
    'security.protection_baseline.delete',
    'security.enrollment.read',
    'security.assessment.read',
    'security.assessment.update',
    'security.policy.read',
    'security.policy.create',
    'security.policy.update',
    'security.policy.delete'
  ]
  const state = {
    types: [],
    baselines: [],
    policies: [],
    typeCreateRequests: [],
    policyCreateRequests: [],
    policyRevokeRequests: [],
    assessmentRevisionRequests: [],
    unhandledRequests: []
  }
  const classifications = [{ id: '1', code: 'personal_information', name: '个人信息', version: '1' }]
  const grades = [
    { id: '3', code: 'l3', name: '较高风险', risk_order: 3, version: '1' },
    { id: '4', code: 'l4', name: '高风险', risk_order: 4, version: '1' }
  ]
  const enrollment = {
    id: enrollmentID,
    state: 'active',
    version: '4',
    target: { owner_module: 'meta', resource_type: 'data_item', resource_identity: 'sha256:customer-table' },
    target_snapshot: { engine_id: 2, item_type: 'table', full_name: 'business.customers' },
    latest_source_snapshot_hash: 'sha256:customer-table-v1',
    latest_discovery_execution_id: '9b2d216d-e880-4682-b359-f1d36394df4c',
    last_discovered_at: '2026-09-10T08:00:00Z',
    discovery_summary: { status: 'completed', finding_count: 0, pending_review_count: 0, reviewed_count: 0 },
    owner_progress: [
      { consumer_owner: 'manager', projection_state: 'active', acknowledged: true, rules: [{ action: 'preview', effect: 'mask' }] },
      { consumer_owner: 'transfer', projection_state: 'active', acknowledged: true, rules: [{ action: 'export', effect: 'mask' }] },
      { consumer_owner: 'develop', projection_state: 'active', acknowledged: true, rules: [{ action: 'query', effect: 'mask' }] },
      { consumer_owner: 'service', projection_state: 'active', acknowledged: true, rules: [{ action: 'service_execute', effect: 'mask' }] }
    ]
  }

  await page.addInitScript(() => {
    localStorage.setItem('addp-lang', 'zh-cn')
    localStorage.setItem('theme-mode', 'light')
  })

  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()

    if (path === '/api/v1/system/refresh') return fulfillJSON(route, { access_token: 'security-e2e-token', expires_in: 3600 })
    if (path === '/api/v1/system/users/me') return fulfillJSON(route, { id: 7, username: 'security-e2e' })
    if (path === '/api/v1/system/auth/context') {
      return fulfillJSON(route, { context: { type: 'tenant', tenant_id: '11' }, authorization: { role_assignments: [{ permissions }] } })
    }
    if (path === '/api/v1/system/engines') return fulfillJSON(route, [{ id: 2, name: '业务 PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active' }])
    if (path === '/api/v1/meta/engines') return fulfillJSON(route, [{ id: 2, name: '业务 PostgreSQL', engine_type: 'postgresql', lifecycle_state: 'active' }])

    if (method === 'GET' && path === '/api/v1/security/classifications') return fulfillJSON(route, classifications)
    if (method === 'GET' && path === '/api/v1/security/grades') return fulfillJSON(route, grades)
    if (method === 'GET' && path === '/api/v1/security/sensitive-data-types') return fulfillJSON(route, state.types)
    if (method === 'GET' && path === '/api/v1/security/detectors') return fulfillJSON(route, [])
    if (method === 'GET' && path === '/api/v1/security/protection-baselines') return fulfillJSON(route, state.baselines)

    if (method === 'POST' && path === '/api/v1/security/sensitive-data-types') {
      const body = request.postDataJSON()
      state.typeCreateRequests.push(body)
      const created = { id: '20', ...body, version: '1' }
      delete created.default_protection
      state.types.push(created)
      state.baselines.push({
        id: '40', sensitive_data_type_id: '20', security_grade_id: '3',
        effect: body.default_protection.effect, algorithm: body.default_protection.algorithm,
        keep_prefix: body.default_protection.keep_prefix, keep_suffix: body.default_protection.keep_suffix,
        invalid_value_effect: body.default_protection.invalid_value_effect, enabled: true, version: '1'
      })
      state.baselines.push({
        id: '41', sensitive_data_type_id: '20', security_grade_id: '4',
        effect: 'mask', algorithm: 'addp.mask.keep_prefix_suffix/v2',
        keep_prefix: 2, keep_suffix: 3, invalid_value_effect: 'suppress', enabled: true, version: '1'
      })
      return fulfillJSON(route, created, 201)
    }

    if (method === 'GET' && path === '/api/v1/security/protection-enrollments') {
      return fulfillJSON(route, { data: [enrollment], total: 1, page: 1, page_size: 20, total_pages: 1 })
    }
    if (method === 'GET' && path === '/api/v1/security/assessments') {
      state.assessment ||= {
        id: assessmentID,
        enrollment_id: enrollmentID,
        component_key: 'customer.phone',
        state: 'active',
        version: '1',
        current_revision: '1',
        current: {
          source_kind: 'manual',
          conclusion: 'sensitive',
          sensitive_data_type_id: '20',
          security_classification_id: '1',
          security_grade_id: '3',
          rationale: '业务确认该字段是客户手机号'
        }
      }
      return fulfillJSON(route, {
        data: [state.assessment],
        total: 1,
        page: 1,
        page_size: 100,
        total_pages: 1
      })
    }
    if (method === 'POST' && path === `/api/v1/security/assessments/${assessmentID}/revisions`) {
      const body = request.postDataJSON()
      state.assessmentRevisionRequests.push(body)
      state.assessment.version = String(Number(state.assessment.version) + 1)
      state.assessment.current_revision = String(Number(state.assessment.current_revision) + 1)
      state.assessment.current = {
        ...state.assessment.current,
        revision: state.assessment.current_revision,
        conclusion: 'sensitive',
        sensitive_data_type_id: String(body.sensitive_data_type_id),
        security_grade_id: String(body.security_grade_id),
        rationale: body.rationale
      }
      return fulfillJSON(route, state.assessment, 201)
    }
    if (method === 'GET' && path === '/api/v1/security/protection-policies') {
      return fulfillJSON(route, { data: state.policies, total: state.policies.length, page: 1, page_size: 100, total_pages: state.policies.length ? 1 : 0 })
    }
    if (method === 'POST' && path === '/api/v1/security/protection-policies') {
      const body = request.postDataJSON()
      state.policyCreateRequests.push(body)
      const created = {
        id: policyID,
        assessment_id: body.assessment_id,
        consumer_owner: body.consumer_owner,
        action: body.action,
        state: 'active',
        version: '1',
        current_revision: '1',
        current: { revision: '1', state: 'active', effect: body.effect, rationale: body.rationale }
      }
      state.policies.push(created)
      return fulfillJSON(route, created, 201)
    }
    if (method === 'DELETE' && path === `/api/v1/security/protection-policies/${policyID}`) {
      const body = request.postDataJSON()
      state.policyRevokeRequests.push(body)
      const policy = state.policies[0]
      policy.state = 'revoked'
      policy.version = '2'
      policy.current_revision = '2'
      policy.current = { revision: '2', state: 'revoked', effect: 'suppress', rationale: body.rationale }
      return fulfillJSON(route, policy)
    }

    state.unhandledRequests.push(`${method} ${path}`)
    return fulfillJSON(route, { error: `unhandled E2E request: ${method} ${path}` }, 500)
  })

  return state
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
