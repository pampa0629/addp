import { expect, test } from '@playwright/test'

const enrollmentID = '67b1460f-8102-4abc-9e8e-bb265a23206c'
const reEnrollmentID = '70fb90f7-a8bc-462b-8f6d-e8f49660d6f6'
const assessmentID = '8ca44894-dc69-4ce4-8e21-0f02e82bb93d'
const policyID = 'bca63b32-b670-4df9-bbcc-fe6867df6c0f'
const exemptionID = 'c5d985f0-82fb-4a87-9252-c77dc2462a52'
const accessApprovalRequestID = 'db5e38fd-e7e3-4e9c-bffb-87a0bffda005'
const accessRejectionRequestID = 'cf72b9b5-063d-4f22-aa07-5c7977580d2e'
const findingID = '8dfe6d44-dd40-4f63-b2bb-aaeb5d7f83f4'

test('marks and validates required classification and grade fields inline', async ({ page }) => {
  const backend = await installMockBackend(page)

  await page.goto('/classification-grading')
  await page.getByRole('button', { name: '新增分类目录' }).click()

  const classificationDialog = page.getByRole('dialog', { name: '新增分类目录' })
  const classificationCode = classificationDialog.locator('.el-form-item.is-required').filter({ hasText: '编码' })
  const classificationName = classificationDialog.locator('.el-form-item.is-required').filter({ hasText: '名称' })
  await expect(classificationCode).toBeVisible()
  await expect(classificationName).toBeVisible()
  await expect(classificationCode.getByRole('textbox')).toBeFocused()
  await classificationDialog.getByRole('button', { name: '保存' }).click()
  await expect(classificationCode.locator('.el-form-item__error')).toHaveText('请完整填写编码')
  await expect(classificationName.locator('.el-form-item__error')).toHaveText('请完整填写名称')
  expect(backend.classificationCreateRequests).toHaveLength(0)
  await classificationDialog.getByRole('button', { name: '取消' }).click()

  await page.getByRole('tab', { name: '保护等级' }).click()
  await page.getByRole('button', { name: '新增保护等级' }).click()

  const gradeDialog = page.getByRole('dialog', { name: '新增保护等级' })
  const gradeCode = gradeDialog.locator('.el-form-item.is-required').filter({ hasText: '编码' })
  const gradeName = gradeDialog.locator('.el-form-item.is-required').filter({ hasText: '名称' })
  const riskOrder = gradeDialog.locator('.el-form-item.is-required').filter({ hasText: '保护顺序' })
  await expect(gradeCode).toBeVisible()
  await expect(gradeName).toBeVisible()
  await expect(riskOrder).toBeVisible()
  await expect(riskOrder.getByRole('spinbutton')).toHaveValue('1')
  await riskOrder.getByRole('spinbutton').fill('')
  await gradeDialog.getByRole('button', { name: '保存' }).click()
  await expect(gradeCode.locator('.el-form-item__error')).toHaveText('请完整填写编码')
  await expect(gradeName.locator('.el-form-item__error')).toHaveText('请完整填写名称')
  await expect(riskOrder.locator('.el-form-item__error')).toHaveText('请完整填写保护顺序')
  expect(backend.gradeCreateRequests).toHaveLength(0)
  expect(backend.unhandledRequests).toEqual([])
})

test('creates a sensitive definition and safely revises, tightens, restores, and revokes protection', async ({ page }) => {
  const backend = await installMockBackend(page)
  const browserErrors = []
  page.on('pageerror', error => browserErrors.push(error.message))

  await page.goto('/sensitive-data-definitions')
  await page.getByRole('button', { name: '新增敏感类型' }).click()

  const createDialog = page.getByRole('dialog', { name: '新增敏感类型' })
  const codeField = createDialog.locator('.el-form-item').filter({ hasText: '编码' })
  const nameField = createDialog.locator('.el-form-item').filter({ hasText: '名称' })
  await createDialog.getByRole('button', { name: '保存' }).click()
  await expect(codeField.locator('.el-form-item__error')).toHaveText('请完整填写编码')
  await expect(nameField.locator('.el-form-item__error')).toHaveText('请完整填写名称')
  expect(backend.typeCreateRequests).toHaveLength(0)
  await formTextbox(createDialog, '编码').fill('customer_phone')
  await formTextbox(createDialog, '编码').press('Tab')
  await formTextbox(createDialog, '名称').fill('客户手机号')
  await formTextbox(createDialog, '名称').press('Tab')
  await expect(codeField.locator('.el-form-item__error')).toHaveCount(0)
  await expect(nameField.locator('.el-form-item__error')).toHaveCount(0)
  await formTextbox(createDialog, '说明').fill('客户联系号码')
  await expect(createDialog.getByText('初始默认保护', { exact: true })).toBeVisible()
  await expect(createDialog.getByRole('radio', { name: '遮盖' })).toBeChecked()
  const createPrefixField = createDialog.locator('.el-form-item.is-required').filter({ hasText: '保留前部字符数' })
  await expect(createPrefixField).toBeVisible()
  await expect(createDialog.locator('.el-form-item.is-required').filter({ hasText: '保留后部字符数' })).toBeVisible()
  await createPrefixField.getByRole('spinbutton').fill('')
  await createDialog.getByRole('button', { name: '保存' }).click()
  await expect(createPrefixField.locator('.el-form-item__error')).toHaveText('请完整填写保留前部字符数')
  expect(backend.typeCreateRequests).toHaveLength(0)
  await createPrefixField.getByRole('spinbutton').fill('3')
  await createPrefixField.getByRole('spinbutton').press('Tab')
  await expect(createPrefixField.locator('.el-form-item__error')).toHaveCount(0)
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

  await page.getByRole('row', { name: /客户手机号/ }).getByRole('button', { name: /0 个参与/ }).click()
  const detectorDrawer = page.locator('.el-drawer').filter({ hasText: '管理识别方式' })
  await detectorDrawer.getByRole('button', { name: '添加识别方式' }).click()
  const detectorDialog = page.getByRole('dialog', { name: '添加识别方式' })
  await expect(detectorDialog.locator('.el-form-item.is-required').filter({ hasText: '识别能力' })).toBeVisible()
  const thresholdField = detectorDialog.locator('.el-form-item.is-required').filter({ hasText: '自动采用条件' })
  await expect(thresholdField).toBeVisible()
  await thresholdField.getByRole('spinbutton').fill('')
  await detectorDialog.getByRole('button', { name: '保存' }).click()
  await expect(thresholdField.locator('.el-form-item__error')).toHaveText('请完整填写自动采用条件')
  expect(backend.detectorCreateRequests).toHaveLength(0)
  await detectorDialog.getByRole('button', { name: '取消' }).click()
  await detectorDrawer.locator('.el-drawer__close-btn').click()

  await page.getByRole('row', { name: /客户手机号/ }).getByRole('button', { name: '遮盖' }).click()
  const baselineDrawer = page.locator('.el-drawer').filter({ hasText: '管理默认保护' })
  await expect(baselineDrawer.getByText('初始规则', { exact: true })).toBeVisible()
  await expect(baselineDrawer.getByRole('row').filter({ hasText: '初始规则' }).getByRole('button', { name: '删除' })).toHaveCount(0)
  await baselineDrawer.getByRole('row').filter({ hasText: '初始规则' }).getByRole('button', { name: '编辑' }).click()
  const baselineDialog = page.getByRole('dialog', { name: '编辑默认保护规则' })
  await expect(baselineDialog.locator('.el-form-item.is-required').filter({ hasText: '保留前部字符数' })).toBeVisible()
  const baselineSuffixField = baselineDialog.locator('.el-form-item.is-required').filter({ hasText: '保留后部字符数' })
  await expect(baselineSuffixField).toBeVisible()
  await baselineSuffixField.getByRole('spinbutton').fill('')
  await baselineDialog.getByRole('button', { name: '保存' }).click()
  await expect(baselineSuffixField.locator('.el-form-item__error')).toHaveText('请完整填写保留后部字符数')
  await baselineSuffixField.getByRole('spinbutton').fill('4')
  await baselineSuffixField.getByRole('spinbutton').press('Tab')
  await expect(baselineSuffixField.locator('.el-form-item__error')).toHaveCount(0)
  await baselineDialog.getByRole('button', { name: '取消' }).click()

  await page.goto('/protection-enrollments')
  const accessReviewCard = page.locator('.access-review-card')
  const approvalRow = accessReviewCard.getByRole('row').filter({ hasText: '申请用户甲' })
  await approvalRow.getByRole('button', { name: '批准' }).click()
  const approvalDialog = page.getByRole('dialog', { name: '批准' })
  await expect(approvalDialog.getByText('批准后将为该申请人生成仅限当前字段和数据预览出口的临时原值授权，并在申请期限到达时自动恢复保护。', { exact: true })).toBeVisible()
  await expect(approvalDialog.getByText('business.customers · customer.phone', { exact: true })).toBeVisible()
  await expect(approvalDialog.getByText(/申请用户甲/)).toBeVisible()
  await approvalDialog.getByRole('button', { name: '确认批准' }).click()
  await expect(approvalDialog.locator('.el-form-item__error')).toHaveText('请完整填写审批意见')
  expect(backend.accessDecisionRequests).toHaveLength(0)
  await formTextbox(approvalDialog, '审批意见').fill('核对工单后批准临时查看原值')
  await approvalDialog.getByRole('button', { name: '确认批准' }).click()

  await expect.poll(() => backend.accessDecisionRequests.length).toBe(1)
  expect(backend.accessDecisionRequests[0]).toEqual({
    id: accessApprovalRequestID,
    body: {
      version: 1,
      decision: 'approve',
      expires_at: '2026-09-25T10:00:00Z',
      rationale: '核对工单后批准临时查看原值'
    }
  })
  await expect(approvalDialog.getByText('该原值访问申请已被其他操作更新；当前审批动作和意见已保留，不会处理最新状态。', { exact: true })).toBeVisible()
  await expect(formTextbox(approvalDialog, '审批意见')).toHaveValue('核对工单后批准临时查看原值')
  await expect(approvalDialog.getByRole('button', { name: '确认批准' })).toBeDisabled()

  await approvalDialog.getByRole('button', { name: '放弃当前输入并加载最新申请' }).click()
  await expect.poll(() => backend.accessDetailRequests).toBe(1)
  await expect(approvalDialog).toHaveCount(0)
  await expect(accessReviewCard.getByRole('row').filter({ hasText: '申请用户甲' })).toHaveCount(0)

  const rejectionRow = accessReviewCard.getByRole('row').filter({ hasText: '申请用户乙' })
  await rejectionRow.getByRole('button', { name: '驳回' }).click()
  const rejectionDialog = page.getByRole('dialog', { name: '驳回' })
  await expect(rejectionDialog.getByText('驳回后不会生成临时原值授权，审批结论不可反转；如仍需访问，申请人必须重新发起申请。', { exact: true })).toBeVisible()
  await formTextbox(rejectionDialog, '审批意见').fill('当前业务依据不足，驳回申请')
  await rejectionDialog.getByRole('button', { name: '确认驳回' }).click()

  await expect.poll(() => backend.accessDecisionRequests.length).toBe(2)
  expect(backend.accessDecisionRequests[1]).toEqual({
    id: accessRejectionRequestID,
    body: {
      version: 1,
      decision: 'reject',
      rationale: '当前业务依据不足，驳回申请'
    }
  })
  await expect(accessReviewCard.getByText('暂无待审批的原值访问申请', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '查看详情' }).click()
  const detailDrawer = page.locator('.el-drawer').filter({ hasText: '资源保护详情' })
  await expect(detailDrawer.getByText('customer.phone', { exact: true }).first()).toBeVisible()
  await expect(detailDrawer.getByText('数据预览执行默认保护：遮盖', { exact: true })).toBeVisible()

  await detailDrawer.getByRole('button', { name: '调整结论' }).click()
  const revisionDialog = page.getByRole('dialog', { name: '调整正式安全结论' })
  await expect(revisionDialog.getByText('客户手机号 · 个人信息 · 较高风险', { exact: true })).toBeVisible()
  await revisionDialog.getByRole('button', { name: '保存调整' }).click()
  await expect(revisionDialog.locator('.el-form-item__error')).toHaveText('请完整填写调整依据')
  expect(backend.assessmentRevisionRequests).toHaveLength(0)
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
  await expect(revisionDialog.getByText('该正式安全结论已被其他操作更新；当前输入已保留，不会覆盖最新修订。', { exact: true })).toBeVisible()
  await expect(formTextbox(revisionDialog, '调整依据')).toHaveValue('复核业务影响后提升保护等级')
  await expect(revisionDialog.getByRole('button', { name: '保存调整' })).toBeDisabled()
  await revisionDialog.getByRole('button', { name: '放弃当前输入并加载最新结论' }).click()
  await expect(formTextbox(revisionDialog, '调整依据')).toHaveValue('')
  await expect(revisionDialog.getByText('该正式安全结论已被其他操作更新；当前输入已保留，不会覆盖最新修订。', { exact: true })).toHaveCount(0)

  await formCombobox(revisionDialog, '安全等级').click()
  await page.getByRole('option', { name: '高风险', exact: true }).click()
  await formTextbox(revisionDialog, '调整依据').fill('重新加载后确认提升保护等级')
  await revisionDialog.getByRole('button', { name: '保存调整' }).click()

  await expect.poll(() => backend.assessmentRevisionRequests.length).toBe(2)
  expect(backend.assessmentRevisionRequests[1]).toEqual({
    version: 2,
    sensitive_data_type_id: 20,
    security_grade_id: 4,
    rationale: '重新加载后确认提升保护等级'
  })
  await expect(detailDrawer.getByText('客户手机号 · 个人信息 · 高风险', { exact: true })).toBeVisible()
  await expect(detailDrawer.getByText('重新加载后确认提升保护等级', { exact: true })).toBeVisible()

  await detailDrawer.getByRole('button', { name: '修订记录' }).click()
  const historyDialog = page.getByRole('dialog', { name: '正式安全结论修订记录' })
  await expect(historyDialog.getByText('R3', { exact: true })).toBeVisible()
  await expect(historyDialog.getByText('当前修订', { exact: true })).toBeVisible()
  await expect(historyDialog.getByText('重新加载后确认提升保护等级', { exact: true })).toBeVisible()
  await expect(historyDialog.getByText('R2', { exact: true })).toBeVisible()
  await expect(historyDialog.getByText('其他治理人员已完成复核', { exact: true })).toBeVisible()
  await expect(historyDialog.getByText('R1', { exact: true })).toBeVisible()
  await expect(historyDialog.getByText('业务确认该字段是客户手机号', { exact: true })).toBeVisible()
  await expect(historyDialog.getByText('人工指定', { exact: true }).first()).toBeVisible()
  expect(backend.assessmentHistoryRequests).toBe(2)
  await historyDialog.getByRole('button', { name: '关闭', exact: true }).click()

  await detailDrawer.getByRole('button', { name: '收紧保护' }).click()

  const policyDialog = page.getByRole('dialog', { name: '资源级保护策略' })
  await expect(policyDialog.getByRole('textbox', { name: '生效出口' })).toHaveValue('数据预览')
  await expect(policyDialog.getByRole('radio', { name: '移除' })).toBeChecked()
  await policyDialog.getByRole('button', { name: '保存策略' }).click()
  await expect(policyDialog.locator('.el-form-item__error')).toHaveText('请完整填写调整依据')
  expect(backend.policyCreateRequests).toHaveLength(0)
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

  await detailDrawer.getByRole('button', { name: '调整收紧策略' }).click()
  await policyDialog.getByText('拒绝', { exact: true }).click()
  await formTextbox(policyDialog, '调整依据').fill('复核后将当前资源收紧为拒绝访问')
  await policyDialog.getByRole('button', { name: '保存策略' }).click()

  await expect.poll(() => backend.policyUpdateRequests.length).toBe(1)
  expect(backend.policyUpdateRequests[0]).toEqual({
    version: 1,
    effect: 'deny',
    rationale: '复核后将当前资源收紧为拒绝访问'
  })
  await expect(policyDialog.getByText('该资源级保护策略已被其他操作更新；当前选择和依据已保留，不会覆盖最新修订。', { exact: true })).toBeVisible()
  await expect(policyDialog.getByRole('radio', { name: '拒绝' })).toBeChecked()
  await expect(formTextbox(policyDialog, '调整依据')).toHaveValue('复核后将当前资源收紧为拒绝访问')
  await expect(policyDialog.getByRole('button', { name: '保存策略' })).toBeDisabled()

  await policyDialog.getByRole('button', { name: '放弃当前输入并加载最新策略' }).click()
  await expect.poll(() => backend.policyDetailRequests).toBe(1)
  await expect(policyDialog.getByRole('radio', { name: '移除' })).toBeChecked()
  await expect(formTextbox(policyDialog, '调整依据')).toHaveValue('其他治理人员已更新资源策略')
  await expect(policyDialog.getByText('该资源级保护策略已被其他操作更新；当前选择和依据已保留，不会覆盖最新修订。', { exact: true })).toHaveCount(0)

  await policyDialog.getByText('拒绝', { exact: true }).click()
  await formTextbox(policyDialog, '调整依据').fill('重新加载后确认收紧为拒绝访问')
  await policyDialog.getByRole('button', { name: '保存策略' }).click()

  await expect.poll(() => backend.policyUpdateRequests.length).toBe(2)
  expect(backend.policyUpdateRequests[1]).toEqual({
    version: 2,
    effect: 'deny',
    rationale: '重新加载后确认收紧为拒绝访问'
  })
  await expect(detailDrawer.getByText('数据预览：默认 遮盖，资源策略收紧为 拒绝', { exact: true })).toBeVisible()

  await detailDrawer.getByRole('button', { name: '恢复默认' }).click()
  const restoreDialog = page.getByRole('dialog', { name: '恢复默认保护' })
  await expect(restoreDialog.getByText('撤销资源级收紧策略后，该字段将回落到当前默认保护；资源仍保持纳管，也不会因此放行明文。', { exact: true })).toBeVisible()
  await restoreDialog.getByRole('button', { name: '确认恢复' }).click()
  await expect(restoreDialog.locator('.el-form-item__error')).toHaveText('请完整填写恢复依据')
  expect(backend.policyRevokeRequests).toHaveLength(0)
  await formTextbox(restoreDialog, '恢复依据').fill('专项处理结束，恢复平台默认规则')
  await restoreDialog.getByRole('button', { name: '确认恢复' }).click()

  await expect.poll(() => backend.policyRevokeRequests.length).toBe(1)
  expect(backend.policyRevokeRequests[0]).toEqual({
    version: 3,
    rationale: '专项处理结束，恢复平台默认规则'
  })
  await expect(restoreDialog.getByText('该资源级保护策略已被其他操作更新；当前恢复依据已保留，不会撤销最新修订。', { exact: true })).toBeVisible()
  await expect(formTextbox(restoreDialog, '恢复依据')).toHaveValue('专项处理结束，恢复平台默认规则')
  await expect(restoreDialog.getByRole('button', { name: '确认恢复' })).toBeDisabled()

  await restoreDialog.getByRole('button', { name: '放弃当前输入并加载最新策略' }).click()
  await expect.poll(() => backend.policyDetailRequests).toBe(2)
  await expect(formTextbox(restoreDialog, '恢复依据')).toHaveValue('')
  await expect(restoreDialog.getByText('数据预览：默认 遮盖，资源策略收紧为 移除', { exact: true })).toBeVisible()
  await expect(restoreDialog.getByText('该资源级保护策略已被其他操作更新；当前恢复依据已保留，不会撤销最新修订。', { exact: true })).toHaveCount(0)

  await formTextbox(restoreDialog, '恢复依据').fill('重新加载后确认恢复平台默认规则')
  await restoreDialog.getByRole('button', { name: '确认恢复' }).click()

  await expect.poll(() => backend.policyRevokeRequests.length).toBe(2)
  expect(backend.policyRevokeRequests[1]).toEqual({
    version: 4,
    rationale: '重新加载后确认恢复平台默认规则'
  })
  await expect(detailDrawer.getByText('数据预览执行默认保护：遮盖', { exact: true })).toBeVisible()

  await detailDrawer.getByRole('button', { name: '提前撤销' }).click()
  const exemptionRevokeDialog = page.getByRole('dialog', { name: '提前撤销临时授权' })
  await expect(exemptionRevokeDialog.getByText('提前撤销后，该用户将立即恢复执行当前字段保护规则；授权历史仍会保留。', { exact: true })).toBeVisible()
  await expect(exemptionRevokeDialog.getByText('requester-1', { exact: false })).toBeVisible()
  await exemptionRevokeDialog.getByRole('button', { name: '确认撤销' }).click()
  await expect(exemptionRevokeDialog.locator('.el-form-item__error')).toHaveText('请完整填写撤销依据')
  expect(backend.exemptionRevokeRequests).toHaveLength(0)
  await formTextbox(exemptionRevokeDialog, '撤销依据').fill('业务处理已结束，提前恢复字段保护')
  await exemptionRevokeDialog.getByRole('button', { name: '确认撤销' }).click()

  await expect.poll(() => backend.exemptionRevokeRequests.length).toBe(1)
  expect(backend.exemptionRevokeRequests[0]).toEqual({
    version: 1,
    rationale: '业务处理已结束，提前恢复字段保护'
  })
  await expect(exemptionRevokeDialog.getByText('该临时原值授权已被其他操作更新；当前撤销依据已保留，不会撤销最新修订。', { exact: true })).toBeVisible()
  await expect(formTextbox(exemptionRevokeDialog, '撤销依据')).toHaveValue('业务处理已结束，提前恢复字段保护')
  await expect(exemptionRevokeDialog.getByRole('button', { name: '确认撤销' })).toBeDisabled()

  await exemptionRevokeDialog.getByRole('button', { name: '放弃当前输入并加载最新授权' }).click()
  await expect.poll(() => backend.exemptionDetailRequests).toBe(1)
  await expect(formTextbox(exemptionRevokeDialog, '撤销依据')).toHaveValue('')
  await expect(exemptionRevokeDialog.getByText('该临时原值授权已被其他操作更新；当前撤销依据已保留，不会撤销最新修订。', { exact: true })).toHaveCount(0)

  await formTextbox(exemptionRevokeDialog, '撤销依据').fill('重新加载后确认提前恢复字段保护')
  await exemptionRevokeDialog.getByRole('button', { name: '确认撤销' }).click()

  await expect.poll(() => backend.exemptionRevokeRequests.length).toBe(2)
  expect(backend.exemptionRevokeRequests[1]).toEqual({
    version: 2,
    rationale: '重新加载后确认提前恢复字段保护'
  })
  await expect(detailDrawer.getByText('已撤销', { exact: true }).first()).toBeVisible()
  await expect(detailDrawer.getByText('重新加载后确认提前恢复字段保护', { exact: true })).toBeVisible()

  await detailDrawer.getByRole('button', { name: '撤销敏感结论' }).click()
  const assessmentRevokeDialog = page.getByRole('dialog', { name: '撤销正式敏感结论' })
  await expect(assessmentRevokeDialog.getByText('撤销后将追加不可变的非敏感结论，并通过统一投影链收回各数据出口的对应字段规则；历史评估和复核证据仍会保留。', { exact: true })).toBeVisible()
  await assessmentRevokeDialog.getByRole('button', { name: '确认撤销' }).click()
  await expect(assessmentRevokeDialog.locator('.el-form-item__error')).toHaveText('请完整填写撤销依据')
  expect(backend.assessmentRevokeRequests).toHaveLength(0)
  await formTextbox(assessmentRevokeDialog, '撤销依据').fill('核实后确认该字段不再属于敏感数据')
  await assessmentRevokeDialog.getByRole('button', { name: '确认撤销' }).click()

  await expect.poll(() => backend.assessmentRevokeRequests.length).toBe(1)
  expect(backend.assessmentRevokeRequests[0]).toEqual({
    version: 3,
    rationale: '核实后确认该字段不再属于敏感数据'
  })
  await expect(assessmentRevokeDialog.getByText('该正式安全结论已被其他操作更新；当前撤销依据已保留，不会撤销最新修订。', { exact: true })).toBeVisible()
  await expect(formTextbox(assessmentRevokeDialog, '撤销依据')).toHaveValue('核实后确认该字段不再属于敏感数据')
  await expect(assessmentRevokeDialog.getByRole('button', { name: '确认撤销' })).toBeDisabled()

  await assessmentRevokeDialog.getByRole('button', { name: '放弃当前输入并加载最新结论' }).click()
  await expect.poll(() => backend.assessmentHistoryRequests).toBe(5)
  await expect(formTextbox(assessmentRevokeDialog, '撤销依据')).toHaveValue('')
  await expect(assessmentRevokeDialog.getByText('该正式安全结论已被其他操作更新；当前撤销依据已保留，不会撤销最新修订。', { exact: true })).toHaveCount(0)

  await formTextbox(assessmentRevokeDialog, '撤销依据').fill('重新加载后确认撤销敏感结论')
  await assessmentRevokeDialog.getByRole('button', { name: '确认撤销' }).click()

  await expect.poll(() => backend.assessmentRevokeRequests.length).toBe(2)
  expect(backend.assessmentRevokeRequests[1]).toEqual({
    version: 4,
    rationale: '重新加载后确认撤销敏感结论'
  })
  await expect(detailDrawer.getByText('敏感结论已撤销', { exact: true }).first()).toBeVisible()
  await expect(detailDrawer.getByText('重新加载后确认撤销敏感结论', { exact: true })).toBeVisible()

  await detailDrawer.getByRole('button', { name: '确认当前无需保护并退出' }).click()
  const releaseDialog = page.getByRole('dialog', { name: '确认当前无需保护并退出' })
  await expect(releaseDialog.getByText('business.customers', { exact: true })).toBeVisible()
  await expect(releaseDialog.getByText('业务复核确认当前无需保护', { exact: true })).toBeVisible()
  await expect(releaseDialog.locator('.el-form-item.is-required').filter({ hasText: '退出原因' })).toBeVisible()
  await releaseDialog.getByRole('button', { name: '确认无需保护并退出' }).click()
  await expect(releaseDialog.locator('.el-form-item__error')).toHaveText('请完整填写退出原因')
  expect(backend.releaseRequests).toHaveLength(0)
  await formTextbox(releaseDialog, '退出原因').fill('业务复核确认当前已支持能力均无命中')
  await releaseDialog.getByRole('button', { name: '确认无需保护并退出' }).click()

  await expect.poll(() => backend.releaseRequests.length).toBe(1)
  expect(backend.releaseRequests[0]).toEqual({
    version: 4,
    basis: 'no_supported_findings',
    reason: '业务复核确认当前已支持能力均无命中'
  })
  await expect(releaseDialog.getByText('该受保护资源已被其他操作更新；当前退出依据和原因已保留，不会退出最新状态。', { exact: true })).toBeVisible()
  await expect(formTextbox(releaseDialog, '退出原因')).toHaveValue('业务复核确认当前已支持能力均无命中')
  await expect(releaseDialog.getByRole('button', { name: '确认无需保护并退出' })).toBeDisabled()

  await releaseDialog.getByRole('button', { name: '放弃当前输入并加载最新受保护资源' }).click()
  await expect.poll(() => backend.enrollmentDetailRequests).toBe(1)
  await expect(formTextbox(releaseDialog, '退出原因')).toHaveValue('')
  await expect(releaseDialog.getByText('该受保护资源已被其他操作更新；当前退出依据和原因已保留，不会退出最新状态。', { exact: true })).toHaveCount(0)

  await formTextbox(releaseDialog, '退出原因').fill('加载最新状态后重新确认退出保护')
  await releaseDialog.getByRole('button', { name: '确认无需保护并退出' }).click()

  await expect.poll(() => backend.releaseRequests.length).toBe(2)
  expect(backend.releaseRequests[1]).toEqual({
    version: 5,
    basis: 'no_supported_findings',
    reason: '加载最新状态后重新确认退出保护'
  })
  await expect(releaseDialog).toHaveCount(0)
  await expect(detailDrawer.getByText('正在停止保护', { exact: true }).first()).toBeVisible()
  expect(backend.unhandledRequests).toEqual([])
  expect(browserErrors).toEqual([])
})

test('validates finding review and manual designation fields inline before submission', async ({ page }) => {
  const backend = await installMockBackend(page, { governanceValidation: true })
  const browserErrors = []
  page.on('pageerror', error => browserErrors.push(error.message))

  await page.goto('/protection-enrollments')
  await page.getByRole('button', { name: '查看详情' }).click()
  const detailDrawer = page.locator('.el-drawer').filter({ hasText: '资源保护详情' })

  const findingCard = detailDrawer.locator('.finding-card').filter({ hasText: 'customer.email' })
  await findingCard.getByRole('button', { name: '确认或调整' }).click()
  const reviewDialog = page.getByRole('dialog', { name: '复核敏感字段候选' })
  await expect(reviewDialog.locator('.el-form-item.is-required').filter({ hasText: '复核说明' })).toBeVisible()
  await reviewDialog.getByRole('button', { name: '提交复核' }).click()
  await expect(reviewDialog.locator('.el-form-item__error')).toHaveText('请完整填写复核说明')
  expect(backend.findingReviewRequests).toHaveLength(0)

  await reviewDialog.getByText('调整分类分级', { exact: true }).click()
  await expect(reviewDialog.locator('.el-form-item.is-required').filter({ hasText: '敏感数据类型' })).toBeVisible()
  await expect(reviewDialog.locator('.el-form-item.is-required').filter({ hasText: '安全等级' })).toBeVisible()
  await reviewDialog.getByRole('button', { name: '取消' }).click()

  await detailDrawer.getByRole('button', { name: '指定敏感字段' }).click()
  const designationDialog = page.getByRole('dialog', { name: '人工指定敏感字段' })
  await designationDialog.getByRole('button', { name: '确认指定并应用保护' }).click()
  await expect(designationDialog.locator('.el-form-item__error')).toHaveText([
    '请完整填写数据字段',
    '请完整填写敏感数据类型',
    '请完整填写安全等级',
    '请完整填写指定依据'
  ])
  expect(backend.assessmentCreateRequests).toHaveLength(0)
  expect(backend.unhandledRequests).toEqual([])
  expect(browserErrors).toEqual([])
})

test('reloads the latest enrollment before retrying rediscovery and handles an in-progress task', async ({ page }) => {
  const backend = await installMockBackend(page)
  const browserErrors = []
  page.on('pageerror', error => browserErrors.push(error.message))

  await page.goto('/protection-enrollments')
  await page.getByRole('button', { name: '查看详情' }).click()
  const detailDrawer = page.locator('.el-drawer').filter({ hasText: '资源保护详情' })
  await detailDrawer.getByRole('button', { name: '重新发现' }).click()

  const rediscoveryDialog = page.getByRole('dialog', { name: '重新发现敏感数据' })
  await expect(rediscoveryDialog.getByText('business.customers', { exact: true })).toBeVisible()
  await rediscoveryDialog.getByRole('button', { name: '确认重新发现' }).click()

  await expect.poll(() => backend.rediscoveryRequests.length).toBe(1)
  expect(backend.rediscoveryRequests[0]).toEqual({ version: 4 })
  await expect(rediscoveryDialog.getByText('该受保护资源已被其他操作更新；重新发现意图已保留，不会使用旧版本创建任务。', { exact: true })).toBeVisible()
  await expect(rediscoveryDialog.getByRole('button', { name: '确认重新发现' })).toBeDisabled()

  await rediscoveryDialog.getByRole('button', { name: '加载最新受保护资源' }).click()
  await expect.poll(() => backend.enrollmentDetailRequests).toBe(1)
  await expect(rediscoveryDialog.getByText('该受保护资源已被其他操作更新；重新发现意图已保留，不会使用旧版本创建任务。', { exact: true })).toHaveCount(0)
  await rediscoveryDialog.getByRole('button', { name: '确认重新发现' }).click()

  await expect.poll(() => backend.rediscoveryRequests.length).toBe(2)
  expect(backend.rediscoveryRequests[1]).toEqual({ version: 5 })
  await expect(rediscoveryDialog).toHaveCount(0)

  await detailDrawer.getByRole('button', { name: '重新发现' }).click()
  await page.getByRole('dialog', { name: '重新发现敏感数据' }).getByRole('button', { name: '确认重新发现' }).click()
  await expect.poll(() => backend.rediscoveryRequests.length).toBe(3)
  expect(backend.rediscoveryRequests[2]).toEqual({ version: 6 })
  await expect(page.getByRole('dialog', { name: '重新发现敏感数据' })).toHaveCount(0)
  await expect(page.getByText('当前已有敏感发现任务正在等待或执行，请完成后再试', { exact: true })).toBeVisible()
  await expect(detailDrawer.getByText('正在自动更新状态', { exact: true })).toBeVisible()
  expect(backend.unhandledRequests).toEqual([])
  expect(browserErrors).toEqual([])
})

test('reloads the released lifecycle before protecting again and switches to an existing active lifecycle', async ({ page }) => {
  const backend = await installMockBackend(page, { enrollmentState: 'released' })
  const browserErrors = []
  page.on('pageerror', error => browserErrors.push(error.message))

  await page.goto('/protection-enrollments')
  await page.locator('.el-radio-button').filter({ hasText: '已退出' }).click()
  const enrollmentCard = page.locator('.enrollment-card')
  const releasedRow = enrollmentCard.getByRole('row').filter({ hasText: 'business.customers' })
  await releasedRow.getByRole('button', { name: '重新纳入' }).click()

  const dialog = page.getByRole('dialog', { name: '重新纳入数据保护' })
  await expect(dialog.getByText('业务复核确认当前无需保护', { exact: false })).toBeVisible()
  await expect(dialog.getByText('原保护生命周期已完成退出', { exact: false })).toBeVisible()
  await dialog.getByRole('button', { name: '确认重新纳入' }).click()

  await expect.poll(() => backend.reEnrollmentRequests.length).toBe(1)
  expect(backend.reEnrollmentRequests[0]).toEqual({ version: 7 })
  await expect(dialog.getByText('该退出记录已被其他操作更新；重新纳入意图已保留，不会使用旧版本创建保护生命周期。', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('button', { name: '确认重新纳入' })).toBeDisabled()

  await dialog.getByRole('button', { name: '加载最新退出记录' }).click()
  await expect.poll(() => backend.enrollmentDetailRequests).toBe(1)
  await expect(dialog.getByText('该退出记录已被其他操作更新；重新纳入意图已保留，不会使用旧版本创建保护生命周期。', { exact: true })).toHaveCount(0)
  await dialog.getByRole('button', { name: '确认重新纳入' }).click()

  await expect.poll(() => backend.reEnrollmentRequests.length).toBe(2)
  expect(backend.reEnrollmentRequests[1]).toEqual({ version: 8 })
  await expect(dialog).toHaveCount(0)
  await expect(enrollmentCard.getByRole('row').filter({ hasText: 'business.customers' })).toContainText('保护规则同步中')

  await page.locator('.el-radio-button').filter({ hasText: '已退出' }).click()
  await enrollmentCard.getByRole('row').filter({ hasText: 'business.customers' }).getByRole('button', { name: '重新纳入' }).click()
  await page.getByRole('dialog', { name: '重新纳入数据保护' }).getByRole('button', { name: '确认重新纳入' }).click()

  await expect.poll(() => backend.reEnrollmentRequests.length).toBe(3)
  expect(backend.reEnrollmentRequests[2]).toEqual({ version: 9 })
  await expect(page.getByRole('dialog', { name: '重新纳入数据保护' })).toHaveCount(0)
  await expect(page.getByText('该资源已存在保护中的生命周期，已切换到当前受保护资源', { exact: true })).toBeVisible()
  await expect(page.getByRole('radio', { name: '保护中' })).toBeChecked()
  expect(backend.unhandledRequests).toEqual([])
  expect(browserErrors).toEqual([])
})

function formTextbox(container, label) {
  return container.locator('.el-form-item').filter({ hasText: label }).getByRole('textbox')
}

function formCombobox(container, label) {
  return container.locator('.el-form-item').filter({ hasText: label }).locator('.el-select__wrapper')
}

async function installMockBackend(page, options = {}) {
  const permissions = [
    'security.classification.read',
    'security.classification.create',
    'security.grade.read',
    'security.grade.create',
    'security.sensitive_data_type.read',
    'security.sensitive_data_type.create',
    'security.sensitive_data_type.update',
    'security.sensitive_data_type.delete',
    'security.detector.read',
    'security.detector.create',
    'security.protection_baseline.read',
    'security.protection_baseline.create',
    'security.protection_baseline.update',
    'security.protection_baseline.delete',
    'security.enrollment.read',
    'security.enrollment.create',
    'security.enrollment.update',
    'security.assessment.read',
    'security.assessment.update',
    'security.policy.read',
    'security.policy.create',
    'security.policy.update',
    'security.policy.delete',
    'security.protection_exemption.read',
    'security.protection_exemption.delete',
    'security.protection_access_request.update'
  ]
  if (options.governanceValidation) {
    permissions.push(
      'security.finding.read',
      'security.finding.update',
      'security.assessment.create'
    )
  }
  const state = {
    classificationCreateRequests: [],
    gradeCreateRequests: [],
    types: [],
    baselines: [],
    policies: [],
    exemptions: [{
      id: exemptionID,
      assessment_id: assessmentID,
      consumer_owner: 'manager',
      action: 'preview',
      subject_type: 'user',
      subject_id: 'requester-1',
      state: 'active',
      version: '1',
      current_revision: '1',
      effective_state: 'active',
      current: {
        revision: '1',
        state: 'active',
        expires_at: '2026-09-30T10:00:00Z',
        rationale: '客户核验期间临时查看原值'
      }
    }],
    accessRequests: [
      {
        id: accessApprovalRequestID,
        assessment_id: assessmentID,
        assessment_revision: '1',
        consumer_owner: 'manager',
        action: 'preview',
        subject_type: 'user',
        subject_id: '51',
        requested_expires_at: '2026-09-25T10:00:00Z',
        rationale: '工单 SEC-001 核验客户联系方式',
        state: 'pending',
        version: '1',
        created_at: '2026-09-11T08:00:00Z',
        requester: { type: 'user', id: '51', display_name: '申请用户甲' },
        component: { key: 'customer.phone', value_type: 'string' },
        enrollment_id: enrollmentID,
        target_full_name: 'business.customers',
        can_decide: true
      },
      {
        id: accessRejectionRequestID,
        assessment_id: assessmentID,
        assessment_revision: '1',
        consumer_owner: 'manager',
        action: 'preview',
        subject_type: 'user',
        subject_id: '52',
        requested_expires_at: '2026-09-26T10:00:00Z',
        rationale: '缺少关联工单的临时访问申请',
        state: 'pending',
        version: '1',
        created_at: '2026-09-11T08:05:00Z',
        requester: { type: 'user', id: '52', display_name: '申请用户乙' },
        component: { key: 'customer.phone', value_type: 'string' },
        enrollment_id: enrollmentID,
        target_full_name: 'business.customers',
        can_decide: true
      }
    ],
    typeCreateRequests: [],
    detectorCreateRequests: [],
    policyCreateRequests: [],
    policyUpdateRequests: [],
    policyRevokeRequests: [],
    policyDetailRequests: 0,
    exemptionRevokeRequests: [],
    exemptionDetailRequests: 0,
    accessDecisionRequests: [],
    accessDetailRequests: 0,
    findingReviewRequests: [],
    assessmentCreateRequests: [],
    assessmentRevisionRequests: [],
    assessmentRevokeRequests: [],
    assessmentHistoryRequests: 0,
    releaseRequests: [],
    enrollmentDetailRequests: 0,
    rediscoveryRequests: [],
    reEnrollmentRequests: [],
    currentEnrollment: null,
    conflictNextAssessmentRevision: true,
    conflictNextAssessmentRevoke: true,
    conflictNextPolicyUpdate: true,
    conflictNextPolicyRevoke: true,
    conflictNextExemptionRevoke: true,
    conflictNextAccessApproval: true,
    conflictNextRelease: true,
    conflictNextRediscovery: true,
    conflictNextReEnrollment: true,
    unhandledRequests: []
  }
  const classifications = [{ id: '1', code: 'personal_information', name: '个人信息', version: '1' }]
  const grades = [
    { id: '3', code: 'l3', name: '较高风险', risk_order: 3, version: '1' },
    { id: '4', code: 'l4', name: '高风险', risk_order: 4, version: '1' }
  ]
  const detectorCapability = {
    key: 'addp.detector.email_metadata/v1',
    version: 'v1',
    name_i18n_key: 'security.detectorCapabilities.emailMetadata.name',
    description_i18n_key: 'security.detectorCapabilities.emailMetadata.description',
    method_i18n_key: 'security.detectorCapabilities.emailMetadata.method',
    privacy_i18n_key: 'security.detectorCapabilities.emailMetadata.privacy',
    limitations_i18n_key: 'security.detectorCapabilities.emailMetadata.limitations',
    target_kind: 'field_metadata',
    evidence_source: 'metadata',
    supported_item_types: ['table'],
    supported_field_types: ['string'],
    recommended_threshold: 0.9
  }
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
  if (options.enrollmentState === 'released') {
    Object.assign(enrollment, {
      state: 'released',
      version: '7',
      release_basis: 'no_supported_findings',
      release_reason: '原保护生命周期已完成退出',
      release_requested_by: '7',
      release_requested_at: '2026-09-10T09:00:00Z',
      released_at: '2026-09-10T09:05:00Z',
      release_source_snapshot_hash: 'sha256:customer-table-v1',
      owner_progress: []
    })
  }
  const governanceFinding = {
    id: findingID,
    enrollment_id: enrollmentID,
    component_key: 'customer.email',
    sensitive_data_type_id: '20',
    detector_version: 'addp.detector.email_metadata/v1',
    confidence: 0.96,
    observed_at: '2026-09-10T08:05:00Z',
    review: null,
    evidence: {
      matched_rule: 'terminal_field_name',
      component_key: 'customer.email',
      semantic_terminal: 'email',
      normalized_terminal: 'email',
      matched_alias: 'email',
      field_type: 'string'
    },
    explanation: {
      decision_state: 'awaiting_review',
      automatic_adoption_threshold: 0.98,
      meets_automatic_threshold: false,
      capability: {
        key: 'addp.detector.email_metadata/v1',
        name_i18n_key: 'security.detectorCapabilities.emailMetadata.name',
        method_i18n_key: 'security.detectorCapabilities.emailMetadata.method',
        privacy_i18n_key: 'security.detectorCapabilities.emailMetadata.privacy',
        limitations_i18n_key: 'security.detectorCapabilities.emailMetadata.limitations',
        supported_item_types: ['table'],
        evidence_source: 'metadata'
      },
      outlets: []
    }
  }
  if (options.governanceValidation) {
    state.types.push({
      id: '20',
      code: 'email',
      name: '电子邮箱',
      security_classification_id: '1',
      default_security_grade_id: '3',
      version: '1'
    })
    state.baselines.push({
      id: '40',
      sensitive_data_type_id: '20',
      security_grade_id: '3',
      effect: 'mask',
      algorithm: 'addp.mask.keep_prefix_suffix/v2',
      keep_prefix: 2,
      keep_suffix: 3,
      invalid_value_effect: 'suppress',
      enabled: true,
      version: '1'
    })
    enrollment.discovery_summary = { status: 'completed', finding_count: 1, pending_review_count: 1, reviewed_count: 0 }
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
    if (method === 'GET' && path === '/api/v1/security/definition-profiles') return fulfillJSON(route, [])
    if (method === 'POST' && path === '/api/v1/security/classifications') {
      state.classificationCreateRequests.push(request.postDataJSON())
      return fulfillJSON(route, { id: '2', ...request.postDataJSON(), version: '1' }, 201)
    }
    if (method === 'POST' && path === '/api/v1/security/grades') {
      state.gradeCreateRequests.push(request.postDataJSON())
      return fulfillJSON(route, { id: '5', ...request.postDataJSON(), version: '1' }, 201)
    }
    if (method === 'GET' && path === '/api/v1/security/sensitive-data-types') return fulfillJSON(route, state.types)
    if (method === 'GET' && path === '/api/v1/security/detectors') return fulfillJSON(route, [])
    if (method === 'GET' && path === '/api/v1/security/detector-capabilities') return fulfillJSON(route, [detectorCapability])
    if (method === 'GET' && path === '/api/v1/security/discovery-quality') {
      return fulfillJSON(route, {
        current_finding_count: 0,
        awaiting_review_count: 0,
        reviewed_sample_count: 0,
        sensitive_confirmation_rate: null,
        active_manual_assessment_count: 0,
        revoked_manual_assessment_count: 0,
        capabilities: []
      })
    }
    if (method === 'POST' && path === '/api/v1/security/detectors') {
      state.detectorCreateRequests.push(request.postDataJSON())
      return fulfillJSON(route, { id: '50', ...request.postDataJSON(), version: '1' }, 201)
    }
    if (method === 'GET' && path === '/api/v1/security/protection-baselines') return fulfillJSON(route, state.baselines)

    if (method === 'GET' && path === '/api/v1/security/protection-access-requests/review-queue') {
      const scope = new URL(request.url()).searchParams.get('scope')
      const rows = state.accessRequests.filter(item => scope === 'history' ? item.state !== 'pending' : item.state === 'pending')
      return fulfillJSON(route, { data: rows, total: rows.length, page: 1, page_size: 10, total_pages: rows.length ? 1 : 0 })
    }
    if (method === 'GET' && path.startsWith('/api/v1/security/protection-access-requests/')) {
      const accessRequest = state.accessRequests.find(item => path.endsWith(`/${item.id}`))
      if (accessRequest) {
        state.accessDetailRequests += 1
        return fulfillJSON(route, accessRequest)
      }
    }
    if (method === 'POST' && path.endsWith('/decisions')) {
      const accessRequest = state.accessRequests.find(item => path === `/api/v1/security/protection-access-requests/${item.id}/decisions`)
      if (accessRequest) {
        const body = request.postDataJSON()
        state.accessDecisionRequests.push({ id: accessRequest.id, body })
        if (accessRequest.id === accessApprovalRequestID && state.conflictNextAccessApproval) {
          state.conflictNextAccessApproval = false
          accessRequest.state = 'approved'
          accessRequest.version = '2'
          accessRequest.can_decide = false
          accessRequest.reviewer = { type: 'user', id: '61', display_name: '其他审批人' }
          accessRequest.decided_at = '2026-09-11T08:10:00Z'
          accessRequest.decision_rationale = '其他审批人已先行批准'
          return fulfillJSON(route, {
            error: '资源已被其他操作更新，请刷新后重试',
            error_code: 'resource_version_conflict'
          }, 409)
        }
        accessRequest.state = body.decision === 'approve' ? 'approved' : 'rejected'
        accessRequest.version = String(Number(accessRequest.version) + 1)
        accessRequest.can_decide = false
        accessRequest.reviewer = { type: 'user', id: '7', display_name: 'security-e2e' }
        accessRequest.decided_at = '2026-09-11T08:15:00Z'
        accessRequest.decision_rationale = body.rationale
        return fulfillJSON(route, accessRequest)
      }
    }

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
      const scope = new URL(request.url()).searchParams.get('scope')
      const rows = []
      if (scope !== 'current' && enrollment.state === 'released') rows.push(enrollment)
      if (scope !== 'released' && enrollment.state !== 'released') rows.push(enrollment)
      if (scope !== 'released' && state.currentEnrollment) rows.push(state.currentEnrollment)
      return fulfillJSON(route, { data: rows, total: rows.length, page: 1, page_size: 20, total_pages: rows.length ? 1 : 0 })
    }
    if (method === 'GET' && path === `/api/v1/security/protection-enrollments/${enrollmentID}`) {
      state.enrollmentDetailRequests += 1
      return fulfillJSON(route, enrollment)
    }
    if (method === 'GET' && path === `/api/v1/security/protection-enrollments/${reEnrollmentID}` && state.currentEnrollment) {
      state.enrollmentDetailRequests += 1
      return fulfillJSON(route, state.currentEnrollment)
    }
    if (method === 'POST' && path === `/api/v1/security/protection-enrollments/${enrollmentID}/re-enrollments`) {
      const body = request.postDataJSON()
      state.reEnrollmentRequests.push(body)
      if (state.conflictNextReEnrollment) {
        state.conflictNextReEnrollment = false
        enrollment.version = '8'
        return fulfillJSON(route, {
          error: '资源已被其他操作更新，请刷新后重试',
          error_code: 'resource_version_conflict'
        }, 409)
      }
      if (state.currentEnrollment) {
        return fulfillJSON(route, {
          error: '该资源已经存在保护中的纳管生命周期',
          error_code: 'protection_enrollment_already_active'
        }, 409)
      }
      enrollment.version = String(Number(enrollment.version) + 1)
      state.currentEnrollment = {
        ...enrollment,
        id: reEnrollmentID,
        state: 'activating',
        version: '1',
        release_basis: '',
        release_reason: '',
        release_requested_by: '',
        release_requested_at: null,
        released_at: null,
        release_source_snapshot_hash: '',
        owner_progress: []
      }
      return fulfillJSON(route, {
        source_enrollment_version: Number(enrollment.version),
        enrollment: state.currentEnrollment
      }, 201)
    }
    if (method === 'POST' && path === `/api/v1/security/protection-enrollments/${enrollmentID}/releases`) {
      const body = request.postDataJSON()
      state.releaseRequests.push(body)
      if (state.conflictNextRelease) {
        state.conflictNextRelease = false
        enrollment.version = '5'
        return fulfillJSON(route, {
          error: '资源已被其他操作更新，请刷新后重试',
          error_code: 'resource_version_conflict'
        }, 409)
      }
      enrollment.version = String(Number(enrollment.version) + 1)
      enrollment.state = 'releasing'
      enrollment.release_basis = body.basis
      enrollment.release_reason = body.reason
      enrollment.release_requested_by = '7'
      enrollment.release_requested_at = '2026-09-11T09:00:00Z'
      enrollment.release_source_snapshot_hash = enrollment.latest_source_snapshot_hash
      return fulfillJSON(route, enrollment)
    }
    if (method === 'POST' && path === `/api/v1/security/protection-enrollments/${enrollmentID}/discovery-executions`) {
      const body = request.postDataJSON()
      state.rediscoveryRequests.push(body)
      if (state.conflictNextRediscovery) {
        state.conflictNextRediscovery = false
        enrollment.version = '5'
        return fulfillJSON(route, {
          error: '资源已被其他操作更新，请刷新后重试',
          error_code: 'resource_version_conflict'
        }, 409)
      }
      if (state.rediscoveryRequests.length > 2) {
        return fulfillJSON(route, {
          error: '当前资源已有敏感发现任务正在等待或执行，请完成后重试',
          error_code: 'protection_discovery_execution_in_progress'
        }, 409)
      }
      enrollment.version = String(Number(enrollment.version) + 1)
      return fulfillJSON(route, {
        execution_id: '',
        enrollment_id: enrollmentID,
        enrollment_version: Number(enrollment.version),
        status: 'pending',
        created_at: '2026-09-11T09:10:00Z'
      }, 201)
    }
    if (method === 'GET' && path === '/api/v1/security/findings') {
      const rows = options.governanceValidation ? [governanceFinding] : []
      return fulfillJSON(route, { data: rows, total: rows.length, page: 1, page_size: 20, total_pages: rows.length ? 1 : 0 })
    }
    if (method === 'POST' && path === `/api/v1/security/findings/${findingID}/reviews`) {
      state.findingReviewRequests.push(request.postDataJSON())
      return fulfillJSON(route, governanceFinding, 201)
    }
    if (method === 'GET' && path === `/api/v1/security/protection-enrollments/${enrollmentID}/components`) {
      return fulfillJSON(route, {
        data: [{ component: { key: 'customer.email', value_type: 'string' } }],
        total: 1,
        page: 1,
        page_size: 100,
        total_pages: 1
      })
    }
    if (method === 'POST' && path === '/api/v1/security/assessments') {
      state.assessmentCreateRequests.push(request.postDataJSON())
      return fulfillJSON(route, state.assessment, 201)
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
      state.assessmentHistory ||= [{
        id: 'assessment-revision-1',
        assessment_id: assessmentID,
        revision: '1',
        source_kind: 'manual',
        conclusion: 'sensitive',
        sensitive_data_type_id: '20',
        security_classification_id: '1',
        security_grade_id: '3',
        rationale: '业务确认该字段是客户手机号',
        created_by: '7',
        created_at: '2026-09-10T08:10:00Z'
      }]
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
      if (state.conflictNextAssessmentRevision) {
        state.conflictNextAssessmentRevision = false
        state.assessment.version = '2'
        state.assessment.current_revision = '2'
        state.assessment.current = {
          ...state.assessment.current,
          revision: '2',
          conclusion: 'sensitive',
          sensitive_data_type_id: '20',
          security_grade_id: '3',
          rationale: '其他治理人员已完成复核'
        }
        state.assessmentHistory.unshift({
          id: 'assessment-revision-2',
          assessment_id: assessmentID,
          revision: '2',
          source_kind: 'manual',
          conclusion: 'sensitive',
          sensitive_data_type_id: '20',
          security_classification_id: '1',
          security_grade_id: '3',
          rationale: '其他治理人员已完成复核',
          created_by: '8',
          created_at: '2026-09-10T08:15:00Z'
        })
        return fulfillJSON(route, {
          error: '资源已被其他操作更新，请刷新后重试',
          error_code: 'resource_version_conflict'
        }, 409)
      }
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
      state.assessmentHistory.unshift({
        id: 'assessment-revision-3',
        assessment_id: assessmentID,
        revision: state.assessment.current_revision,
        source_kind: 'manual',
        conclusion: 'sensitive',
        sensitive_data_type_id: String(body.sensitive_data_type_id),
        security_classification_id: '1',
        security_grade_id: String(body.security_grade_id),
        rationale: body.rationale,
        created_by: '7',
        created_at: '2026-09-10T08:20:00Z'
      })
      return fulfillJSON(route, { ...state.assessment, history: state.assessmentHistory }, 201)
    }
    if (method === 'GET' && path === `/api/v1/security/assessments/${assessmentID}`) {
      state.assessmentHistoryRequests += 1
      return fulfillJSON(route, { ...state.assessment, history: state.assessmentHistory })
    }
    if (method === 'DELETE' && path === `/api/v1/security/assessments/${assessmentID}`) {
      const body = request.postDataJSON()
      state.assessmentRevokeRequests.push(body)
      if (state.conflictNextAssessmentRevoke) {
        state.conflictNextAssessmentRevoke = false
        state.assessment.version = '4'
        state.assessment.current_revision = '4'
        state.assessment.current = {
          ...state.assessment.current,
          revision: '4',
          conclusion: 'sensitive',
          rationale: '其他治理人员确认该字段仍需保护'
        }
        state.assessmentHistory.unshift({
          id: 'assessment-revision-4',
          assessment_id: assessmentID,
          revision: '4',
          source_kind: 'manual',
          conclusion: 'sensitive',
          sensitive_data_type_id: '20',
          security_classification_id: '1',
          security_grade_id: '4',
          rationale: '其他治理人员确认该字段仍需保护',
          created_by: '8',
          created_at: '2026-09-10T08:25:00Z'
        })
        return fulfillJSON(route, {
          error: '资源已被其他操作更新，请刷新后重试',
          error_code: 'resource_version_conflict'
        }, 409)
      }
      state.assessment.version = String(Number(state.assessment.version) + 1)
      state.assessment.current_revision = String(Number(state.assessment.current_revision) + 1)
      state.assessment.current = {
        ...state.assessment.current,
        revision: state.assessment.current_revision,
        conclusion: 'not_sensitive',
        rationale: body.rationale
      }
      state.assessmentHistory.unshift({
        id: 'assessment-revision-5',
        assessment_id: assessmentID,
        revision: state.assessment.current_revision,
        source_kind: 'manual',
        conclusion: 'not_sensitive',
        sensitive_data_type_id: '20',
        security_classification_id: '1',
        security_grade_id: '4',
        rationale: body.rationale,
        created_by: '7',
        created_at: '2026-09-10T08:30:00Z'
      })
      return fulfillJSON(route, { ...state.assessment, history: state.assessmentHistory })
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
    if (method === 'GET' && path === `/api/v1/security/protection-policies/${policyID}`) {
      state.policyDetailRequests += 1
      return fulfillJSON(route, state.policies[0])
    }
    if (method === 'PUT' && path === `/api/v1/security/protection-policies/${policyID}`) {
      const body = request.postDataJSON()
      state.policyUpdateRequests.push(body)
      const policy = state.policies[0]
      if (state.conflictNextPolicyUpdate) {
        state.conflictNextPolicyUpdate = false
        policy.version = '2'
        policy.current_revision = '2'
        policy.current = {
          revision: '2',
          state: 'active',
          effect: 'suppress',
          rationale: '其他治理人员已更新资源策略'
        }
        return fulfillJSON(route, {
          error: '资源已被其他操作更新，请刷新后重试',
          error_code: 'resource_version_conflict'
        }, 409)
      }
      policy.version = String(Number(policy.version) + 1)
      policy.current_revision = String(Number(policy.current_revision) + 1)
      policy.current = {
        revision: policy.current_revision,
        state: 'active',
        effect: body.effect,
        rationale: body.rationale
      }
      return fulfillJSON(route, policy)
    }
    if (method === 'DELETE' && path === `/api/v1/security/protection-policies/${policyID}`) {
      const body = request.postDataJSON()
      state.policyRevokeRequests.push(body)
      const policy = state.policies[0]
      if (state.conflictNextPolicyRevoke) {
        state.conflictNextPolicyRevoke = false
        policy.version = '4'
        policy.current_revision = '4'
        policy.current = {
          revision: '4',
          state: 'active',
          effect: 'suppress',
          rationale: '其他治理人员已再次调整资源策略'
        }
        return fulfillJSON(route, {
          error: '资源已被其他操作更新，请刷新后重试',
          error_code: 'resource_version_conflict'
        }, 409)
      }
      policy.state = 'revoked'
      policy.version = String(Number(policy.version) + 1)
      policy.current_revision = String(Number(policy.current_revision) + 1)
      policy.current = { revision: policy.current_revision, state: 'revoked', effect: 'deny', rationale: body.rationale }
      return fulfillJSON(route, policy)
    }
    if (method === 'GET' && path === '/api/v1/security/protection-exemptions') {
      return fulfillJSON(route, { data: state.exemptions, total: state.exemptions.length, page: 1, page_size: 100, total_pages: state.exemptions.length ? 1 : 0 })
    }
    if (method === 'GET' && path === `/api/v1/security/protection-exemptions/${exemptionID}`) {
      state.exemptionDetailRequests += 1
      return fulfillJSON(route, state.exemptions[0])
    }
    if (method === 'DELETE' && path === `/api/v1/security/protection-exemptions/${exemptionID}`) {
      const body = request.postDataJSON()
      state.exemptionRevokeRequests.push(body)
      const exemption = state.exemptions[0]
      if (state.conflictNextExemptionRevoke) {
        state.conflictNextExemptionRevoke = false
        exemption.version = '2'
        exemption.current_revision = '2'
        exemption.current = {
          revision: '2',
          state: 'active',
          expires_at: '2026-10-15T10:00:00Z',
          rationale: '其他治理人员延长临时授权'
        }
        return fulfillJSON(route, {
          error: '资源已被其他操作更新，请刷新后重试',
          error_code: 'resource_version_conflict'
        }, 409)
      }
      exemption.state = 'revoked'
      exemption.effective_state = 'revoked'
      exemption.version = String(Number(exemption.version) + 1)
      exemption.current_revision = String(Number(exemption.current_revision) + 1)
      exemption.current = {
        revision: exemption.current_revision,
        state: 'revoked',
        expires_at: '2026-10-15T10:00:00Z',
        rationale: body.rationale
      }
      return fulfillJSON(route, exemption)
    }

    state.unhandledRequests.push(`${method} ${path}`)
    return fulfillJSON(route, { error: `unhandled E2E request: ${method} ${path}` }, 500)
  })

  return state
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) })
}
