import { expect, test } from "@playwright/test";
const executionID = "bb1a324d-53a3-4f02-b666-d51385d258c8";
const ruleKey = "bfed8f3b-e3b4-8a8e-8140-860d0b0585ea";
const locator = "addp://engine/2/path/public/customers?type=table&item_id=23";
const rule = {
  id: 7,
  code: "required",
  name: "客户标识非空",
  type: "not_null",
  params: {},
  revision_no: 1,
  version: 1,
  plan_count: 1,
};
const plan = {
  id: 12,
  code: "customers_check",
  name: "客户质量检查",
  version: 2,
  table_bindings: [{ alias: "customers", locator }],
  check_items: [
    {
      rule_key: ruleKey,
      rule_id: 7,
      revision_no: 1,
      severity: "error",
      disabled: false,
      bindings: { table: "customers", column: "id" },
      rule: { ...rule, rule_id: 7, latest_revision_no: 1 },
    },
  ],
  last_execution_id: executionID,
  last_execution_status: "failed",
};
const execution = {
  execution_id: executionID,
  source_task_name: plan.name,
  source_task_id: "12",
  task_type: "quality_plan",
  status: "failed",
  execution_time_ms: 142,
  created_at: "2026-08-14T08:00:00Z",
  error_details: { code: "quality.plan.rule_failed" },
  metadata: {
    schema_version: "addp.quality.plan-result/v1",
    quality_score: 0,
    passed: false,
    rules: [
      {
        rule_key: ruleKey,
        name: "客户标识非空",
        type: "not_null",
        severity: "error",
        table: "customers",
        columns: ["id"],
        total_count: 30,
        failed_count: 4,
        passed: false,
        observed: {},
      },
    ],
  },
};

test("single plan navigation and failed execution retain evidence", async ({
  page,
}) => {
  await installMockBackend(page);
  await page.goto("/plans");
  await expect(
    page.getByRole("heading", { name: "质量检查方案" }),
  ).toBeVisible();
  await expect(
    page.locator(".sidebar-menu").getByText("规则应用配置", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.locator(".sidebar-menu").getByText("执行记录", { exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "执行详情", exact: true }).click();
  await expect(page.getByText(ruleKey, { exact: true })).toBeVisible();
  await expect(page.getByText("客户标识非空", { exact: true })).toBeVisible();
  await expect(
    page.getByText("至少一条 error 级质量规则未通过", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "在统一监控中查看" }),
  ).toBeVisible();
});
test("edits versioned plan without Catalog or Model dependency", async ({
  page,
}) => {
  const state = await installMockBackend(page);
  await page.goto("/plans?task_id=12");
  const dialog = page.getByRole("dialog", { name: "编辑质量检查方案" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => state.writes.length).toBe(1);
  expect(state.writes[0]).toMatchObject({
    version: 2,
    table_bindings: plan.table_bindings,
    check_items: plan.check_items.map(({ rule, ...item }) => item),
  });
  expect(state.unexpected).toEqual([]);
});
test("creates with physical picker and starts a plan, preventing duplicate submit", async ({
  page,
}) => {
  const state = await installMockBackend(page);
  await page.goto("/plans?create=1");
  const dialog = page.getByRole("dialog", { name: "新建质量检查方案" });
  await dialog.getByRole("textbox").nth(0).fill("outdoor_check");
  await dialog.getByRole("textbox").nth(1).fill("户外数据质量检查");
  await dialog.getByRole("button", { name: "添加数据表", exact: true }).click();
  await dialog.locator(".resource-tree-picker .el-select").click();
  await page.getByRole("option", { name: "业务 PostgreSQL" }).click();
  const tree = dialog.locator(".resource-tree-picker");
  await tree
    .getByRole("treeitem", { name: "public", exact: true })
    .locator(".el-tree-node__expand-icon")
    .first()
    .click();
  await tree.getByText("customers", { exact: true }).click();
  await expect(
    dialog.locator(".binding-table").getByRole("textbox"),
  ).toHaveValue("customers");
  await dialog.locator(".rules-heading .el-select").click();
  await page.getByRole("option", { name: "客户标识非空 · R1" }).click();
  await dialog.getByRole("button", { name: "添加检查项", exact: true }).click();
  await dialog.locator(".rule-card .rule-grid .el-select").nth(1).click();
  await page.getByRole("option", { name: "id", exact: true }).click();
  await dialog
    .getByRole("button", { name: "保存", exact: true })
    .evaluate((button) => {
      button.click();
      button.click();
    });
  await expect.poll(() => state.writes.length).toBe(1);
  expect(state.writes[0].version).toBeUndefined();
  expect(state.writes[0].check_items[0]).toMatchObject({
    rule_id: 7,
    revision_no: 1,
    bindings: { table: "customers", column: "id" },
  });
  await page
    .getByRole("row", { name: /户外数据质量检查/ })
    .getByRole("button", { name: "执行", exact: true })
    .click();
  await page.getByRole('dialog', { name: '执行质量检查方案', exact: true }).getByRole('button', { name: '执行', exact: true }).click();
  await expect.poll(() => state.runs).toEqual([13]);
  await expect(page).toHaveURL(new RegExp("/executions/" + executionID));
  expect(state.unexpected).toEqual([]);
});
test('deferred input can be saved and supplied only for one execution', async ({ page }) => {
  const state = await installMockBackend(page);
  state.plans[0].table_bindings[0].locator = '';
  await page.goto('/plans?task_id=12');
  const editor = page.getByRole('dialog', { name: '编辑质量检查方案' });
  await expect(editor.getByText('执行时必填', { exact: true })).toBeVisible();
  await expect(editor.locator('.resource-tree-picker')).toHaveCount(0);
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  await expect.poll(() => state.writes.length).toBe(1);
  expect(state.writes[0].table_bindings).toEqual([{ alias: 'customers', locator: '' }]);
  await page.getByRole('button', { name: '执行', exact: true }).click();
  const runner = page.getByRole('dialog', { name: '执行质量检查方案', exact: true });
  await runner.getByRole('button', { name: '执行', exact: true }).click();
  await expect(page.getByText('请为以下表别名选择本次执行的数据表：customers', { exact: true })).toBeVisible();
  expect(state.runRequests).toEqual([]);
  await runner.getByText('执行时指定', { exact: true }).click();
  await runner.getByRole('button', { name: '选择资源', exact: true }).click();
  const picker = page.locator('.el-dialog').filter({ has: page.locator('.resource-tree-picker') });
  await picker.locator('.resource-tree-picker .el-select').click();
  await page.getByRole('option', { name: '业务 PostgreSQL' }).click();
  await picker.getByRole('treeitem', { name: 'public', exact: true }).locator('.el-tree-node__expand-icon').first().click();
  await picker.getByRole('treeitem', { name: 'customers', exact: true }).click();
  await picker.getByRole('button', { name: '确定', exact: true }).click();
  await runner.getByRole('button', { name: '执行', exact: true }).click();
  await expect.poll(() => state.runRequests.length).toBe(1);
  expect(state.runRequests[0]).toEqual({ table_bindings: { customers: locator } });
  expect(state.plans[0].table_bindings[0].locator).toBe('');
});

test('creates a target-independent plan using an alias and required field name', async ({ page }) => {
  const state = await installMockBackend(page);
  await page.goto('/plans?create=1');
  const editor = page.getByRole('dialog', { name: '新建质量检查方案', exact: true });
  await editor.getByRole('textbox').nth(0).fill('region_orders');
  await editor.getByRole('textbox').nth(1).fill('各地区订单检查');
  await editor.getByRole('button', { name: '添加执行时输入', exact: true }).click();
  await editor.locator('.binding-table').getByRole('textbox').fill('orders');
  await editor.locator('.rules-heading .el-select').click();
  await page.getByRole('option', { name: '客户标识非空 · R1' }).click();
  await editor.getByRole('button', { name: '添加检查项', exact: true }).click();
  const column = editor.locator('.rule-card .rule-grid .el-select').nth(1).getByRole('combobox');
  await column.fill('order_id');
  await page.getByRole('option', { name: 'order_id', exact: true }).click();
  await editor.getByRole('button', { name: '保存', exact: true }).click();
  await expect.poll(() => state.writes.length).toBe(1);
  expect(state.writes[0].table_bindings).toEqual([{ alias: 'orders', locator: '' }]);
  expect(state.writes[0].check_items[0].bindings).toEqual({ table: 'orders', column: 'order_id' });
  expect(state.unexpected).toEqual([]);
});

test("keeps edits on version conflict", async ({ page }) => {
  const state = await installMockBackend(page, {
    writeError: "方案版本已变化，请重新加载",
  });
  await page.goto("/plans?task_id=12");
  const dialog = page.getByRole("dialog", { name: "编辑质量检查方案" });
  await dialog.getByRole("textbox").nth(1).fill("保留本次编辑");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => state.writes.length).toBe(1);
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("textbox").nth(1)).toHaveValue("保留本次编辑");
  await expect(
    page.getByText("方案版本已变化，请重新加载", { exact: true }),
  ).toBeVisible();
});
test("imports a frozen Standard constraint in the independent rule library", async ({
  page,
}, testInfo) => {
  const state = await installMockBackend(page);
  await page.goto("/rules?create=1");
  const dialog = page.getByRole("dialog", {
    name: "新建质量规则",
    exact: true,
  });
  await dialog.getByRole("textbox").nth(0).fill("customer_identifier_length");
  await dialog
    .getByRole("button", { name: "从数据元导入", exact: true })
    .click();
  const source = page.getByRole("dialog", {
    name: "从数据元导入",
    exact: true,
  });
  await source.locator(".el-select").nth(0).getByRole("combobox").fill("标识");
  await page.getByRole("option", { name: "客户标识 · customer_id · R3" }).click();
  await source.locator(".el-select").nth(1).click();
  await page.getByRole("option", { name: "长度", exact: true }).click();
  await source
    .getByRole("button", { name: "从数据元导入", exact: true })
    .click();
  await expect(dialog.getByText("数据元 #3 · 修订 ID #31", { exact: true })).toBeVisible();
  await expect(source).toBeHidden();
  await page.screenshot({
    path: testInfo.outputPath("rule-editor.png"),
    fullPage: true,
  });
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => state.ruleWrites.length).toBe(1);
  await expect(page.getByText("质量规则已保存", { exact: true })).toBeVisible();
  expect(state.ruleWrites[0]).toMatchObject({
    type: "length",
    source: { element_id: 3, element_revision_id: 31, rule_key: ruleKey },
    params: { constraint: { min: 1, max: 64 } },
  });
  expect(state.writes).toEqual([]);
});
test("browses sources without typing, paginates and preserves selection", async ({ page }) => {
  const state = await installMockBackend(page, { pagedCandidates: true });
  await page.goto('/rules?create=1');
  const editor = page.getByRole('dialog', { name:'新建质量规则', exact:true });
  await expect(editor.getByText('规则编码', {exact:true})).toBeVisible();
  await editor.getByRole('button', {name:'从数据元导入',exact:true}).click();
  const source = page.getByRole('dialog',{name:'从数据元导入',exact:true});
  await expect.poll(() => state.candidateRequests.length).toBe(1);
  expect(state.candidateRequests[0]).toMatchObject({ keyword:'',page:'1',page_size:'30' });
  await source.locator('.el-select').nth(0).click();
  await page.getByRole('option',{name:'客户标识 · customer_id · R3'}).click();
  await source.locator('.el-select').nth(1).click();
  await page.getByRole('option',{name:'长度',exact:true}).click();
  await source.getByRole('button',{name:'下一页',exact:true}).click();
  await expect.poll(() => state.candidateRequests.at(-1).page).toBe('2');
  await source.locator('.el-select').nth(0).click();
  await expect(page.getByRole('option',{name:'第二页数据元 · second_page · R1'})).toBeVisible();
  await source.getByRole('combobox').nth(0).press('Escape');
  await source.getByRole('button',{name:'从数据元导入',exact:true}).click();
  await expect(editor.getByText('数据元 #3 · 修订 ID #31',{exact:true})).toBeVisible();
  expect(state.ruleWrites).toEqual([]);
});

for (const scenario of [
  { name: 'missing compiled constraints', detail: { nullable:true, length:11, format:'', value_domain_kind:'unrestricted' }, message:'配置了值约束，但缺少可导入规则' },
  { name: 'unconstrained source', detail: { nullable:true, length:null, format:'', value_domain_kind:'unrestricted' }, message:'未配置非空、长度、格式、范围或枚举约束' },
  { name: 'unavailable source', sourceFailure:true, message:'此已发布修订没有可导入规则' },
  { name: 'unreadable source', noStandardPermission:true, message:'此已发布修订没有可导入规则' },
]) {
  test(`allows inspecting ${scenario.name} without importing or guessing constraints`, async ({ page }) => {
    const state = await installMockBackend(page, { emptyRules:true, sourceDetail:scenario.detail, sourceFailure:scenario.sourceFailure, noStandardPermission:scenario.noStandardPermission });
    await page.goto('/rules?create=1');
    await page.getByRole('button',{name:'从数据元导入',exact:true}).click();
    const source = page.getByRole('dialog',{name:'从数据元导入',exact:true});
    await source.locator('.el-select').nth(0).click();
    const option = page.getByRole('option',{name:'客户标识 · customer_id · R3'});
    await expect(option).toBeEnabled();
    await option.click();
    await expect(source.getByText('来源预览',{exact:true})).toBeVisible();
    await expect(source.getByRole('alert').filter({hasText:scenario.message})).toBeVisible();
    await expect(source.getByRole('button',{name:'从数据元导入',exact:true})).toBeDisabled();
    if (scenario.sourceFailure || scenario.noStandardPermission) {
      await expect(source.getByText(scenario.sourceFailure ? /来源详情加载失败/ : /无权读取标准来源详情/)).toBeVisible();
      await expect(source.getByText(/未配置非空、长度、格式、范围或枚举约束/)).toHaveCount(0);
    }
    if (scenario.noStandardPermission) expect(state.sourceRequests).toEqual([]);
    expect(state.ruleWrites).toEqual([]);
  });
}

test("source failures are not empty results and can be retried", async ({ page }) => {
  const options = { candidateFailure:true };
  const state = await installMockBackend(page,options);
  await page.goto('/rules?create=1');
  await page.getByRole('button',{name:'从数据元导入',exact:true}).click();
  const source=page.getByRole('dialog',{name:'从数据元导入',exact:true});
  await expect(source.getByRole('alert')).toContainText('数据元加载失败');
  options.candidateFailure=false;
  await source.getByRole('button',{name:'重试',exact:true}).click();
  await expect.poll(() => state.candidateRequests.length).toBe(2);
  await expect(source.getByRole('alert')).toHaveCount(0);
});

const sourcedRule = { type:'allowed_values',params:{values:['signup','leader']},source:{element_id:3,element_revision_id:31,rule_key:ruleKey} };
test("distinguishes an empty published list from an unmatched search", async ({page}) => {
  await installMockBackend(page,{emptyCandidates:true});
  await page.goto('/rules?create=1');
  await page.getByRole('button',{name:'从数据元导入',exact:true}).click();
  const source=page.getByRole('dialog',{name:'从数据元导入',exact:true});
  await expect(source.getByRole('status')).toHaveText('暂无已发布数据元');
  await source.getByRole('combobox').nth(0).fill('missing');
  await expect(source.getByRole('status')).toHaveText('没有匹配的数据元');
  await expect(source.getByRole('button',{name:'从数据元导入',exact:true})).toBeDisabled();
  await expect(source.getByRole('alert')).toHaveCount(0);
});

test("shows exact Standard provenance and value meanings without rewriting a rule", async ({page},testInfo) => {
  const state = await installMockBackend(page,{rule:sourcedRule});
  await page.goto('/rules?rule_id=7');
  const editor=page.getByRole('dialog',{name:'编辑质量规则',exact:true});
  await expect(editor.getByText('来源数据元：成员状态 · member_status · R3',{exact:true})).toBeVisible();
  await expect(editor.getByText('来源码值集：成员状态码值 · member_status_codes · R2',{exact:true})).toBeVisible();
  await expect(editor.getByText('signup · 报名',{exact:true})).toBeVisible();
  await editor.getByRole('button',{name:'查看来源详情',exact:true}).click();
  await expect(editor.getByRole('cell',{name:'活动负责人',exact:true})).toBeVisible();
  await page.screenshot({path:testInfo.outputPath('rule-source-details.png'),fullPage:true});
  expect(state.sourceRequests).toEqual(['/api/v1/standard/elements/3/revisions/31']);
  expect(state.ruleWrites).toEqual([]);
  await editor.getByRole('button',{name:'取消',exact:true}).click();
});

test("missing Standard permission keeps pinned provenance without fetching", async ({page}) => {
  const state=await installMockBackend(page,{rule:sourcedRule,noStandardPermission:true});
  await page.goto('/rules?rule_id=7');
  const editor=page.getByRole('dialog',{name:'编辑质量规则',exact:true});
  await expect(editor.getByText(/无权读取标准来源详情/)).toBeVisible();
  await expect(editor.getByText('标准导入',{exact:true})).toBeVisible();
  expect(state.sourceRequests).toEqual([]);
  expect(state.ruleWrites).toEqual([]);
});

test("source lookup failure retains constraints and detaching removes source labels", async ({page}) => {
  const options={rule:sourcedRule,sourceFailure:true};
  const state=await installMockBackend(page,options);
  await page.goto('/rules?rule_id=7');
  const editor=page.getByRole('dialog',{name:'编辑质量规则',exact:true});
  await expect(editor.getByRole('alert')).toContainText('来源详情加载失败');
  options.sourceFailure=false;
  await editor.getByRole('button',{name:'重试',exact:true}).click();
  await expect(editor.getByText('signup · 报名',{exact:true})).toBeVisible();
  await editor.getByRole('button',{name:'转为手工规则',exact:true}).click();
  await expect(editor.getByText('手工定义',{exact:true})).toBeVisible();
  await expect(editor.getByText('signup · 报名',{exact:true})).toHaveCount(0);
  expect(state.ruleWrites).toEqual([]);
});

test("upgrades a pinned rule only by explicit action in the plan editor", async ({
  page,
}, testInfo) => {
  const state = await installMockBackend(page, { latestRevision: 2 });
  await page.goto("/plans?task_id=12");
  const dialog = page.getByRole("dialog", { name: "编辑质量检查方案" });
  await expect(dialog.locator(".rule-header")).toContainText("R1");
  await dialog.getByRole("button", { name: "检查新修订", exact: true }).click();
  await expect(dialog.locator(".rule-header")).toContainText("R1");
  await dialog.getByRole("button", { name: "升级至 R2", exact: true }).click();
  await expect(dialog.locator(".rule-header")).toContainText("R2");
  await page.screenshot({
    path: testInfo.outputPath("plan-editor.png"),
    fullPage: true,
  });
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => state.writes.length).toBe(1);
  expect(state.writes[0].check_items[0]).toMatchObject({
    rule_id: 7,
    revision_no: 2,
    rule_key: ruleKey,
  });
});
test("rule update conflict preserves edits and offers explicit reload", async ({
  page,
}) => {
  const state = await installMockBackend(page, { ruleConflict: true });
  await page.goto("/rules?rule_id=7");
  const dialog = page.getByRole("dialog", { name: "编辑质量规则" });
  await dialog.getByRole("textbox").nth(1).fill("保留规则编辑");
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => state.ruleWrites.length).toBe(1);
  await expect(dialog.getByRole("textbox").nth(1)).toHaveValue("保留规则编辑");
  await expect(
    dialog.getByRole("button", { name: "重新加载", exact: true }),
  ).toBeVisible();
});
test("unavailable physical target remains editable for rebinding", async ({
  page,
}) => {
  await installMockBackend(page, { missingTarget: true });
  await page.goto("/plans?task_id=12");
  const dialog = page.getByRole("dialog", { name: "编辑质量检查方案" });
  await expect(dialog).toBeVisible();
  await expect(dialog.locator(".binding-table")).toContainText("customers");
});
test("requires and persists a note when resolving an issue", async ({
  page,
}) => {
  const backend = await installMockBackend(page, { issueStatus: "open" });
  await page.goto("/issues");

  await page.getByRole("button", { name: "标记解决" }).click();
  const prompt = page.getByRole("dialog", { name: "填写处理说明" });
  await prompt.getByRole("textbox").fill("已修复手机号格式校验");
  await prompt.getByRole("button", { name: "确定" }).click();

  await expect.poll(() => backend.issueStatusRequests.length).toBe(1);
  expect(backend.issueStatusRequests[0]).toEqual({
    id: 17,
    status: "resolved",
    note: "已修复手机号格式校验",
  });
  await expect(page.getByText("已解决", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "标记解决" })).toHaveCount(0);
});

test("requires and persists a note when ignoring an issue", async ({
  page,
}) => {
  const backend = await installMockBackend(page, { issueStatus: "open" });
  await page.goto("/issues");

  await page.getByRole("button", { name: "忽略" }).click();
  const prompt = page.getByRole("dialog", { name: "填写处理说明" });
  await prompt.getByRole("textbox").fill("业务确认该异常可忽略");
  await prompt.getByRole("button", { name: "确定" }).click();

  await expect.poll(() => backend.issueStatusRequests.length).toBe(1);
  expect(backend.issueStatusRequests[0]).toEqual({
    id: 17,
    status: "ignored",
    note: "业务确认该异常可忽略",
  });
  await expect(page.getByText("已忽略", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "忽略" })).toHaveCount(0);
});

test("keeps an issue open when its status update is rejected", async ({
  page,
}) => {
  const backend = await installMockBackend(page, {
    issueStatus: "open",
    issueStatusError: "问题工单已被处理",
  });
  await page.goto("/issues");

  await page.getByRole("button", { name: "标记解决" }).click();
  const prompt = page.getByRole("dialog", { name: "填写处理说明" });
  await prompt.getByRole("textbox").fill("尝试解决");
  await prompt.getByRole("button", { name: "确定" }).click();

  await expect.poll(() => backend.issueStatusRequests.length).toBe(1);
  await expect(
    page.getByText("问题工单已被处理", { exact: true }),
  ).toBeVisible();
  await expect(page.getByText("待处理", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "标记解决" })).toBeVisible();
});

for (const noPermission of [false, true]) {
  test(`domain ${noPermission ? 'permission denial' : 'load failure'} preserves rule ownership`, async ({ page }) => {
    const state = await installMockBackend(page, { rule: { owner_domain_id: 42 }, noDomainPermission: noPermission, domainFailure: !noPermission });
    await page.goto('/rules?rule_id=7');
    const dialog = page.getByRole('dialog', { name: '编辑质量规则' });
    await expect(dialog.getByText(noPermission ? '无业务域查看权限，保留原有归属。' : '业务域加载失败，保留原有归属；重试成功后可修改。')).toBeVisible();
    await expect(dialog.locator('.domain-ownership-select .el-select__wrapper')).toHaveClass(/is-disabled/);
    await dialog.getByRole('button', { name: '保存', exact: true }).click();
    await expect.poll(() => state.ruleWrites.length).toBe(1);
    expect(state.ruleWrites[0].owner_domain_id).toBe(42);
    expect(state.domainRequests.length).toBe(noPermission ? 0 : 2);
  });
}

test('rule domain can be selected and explicitly cleared', async ({ page }) => {
  const state = await installMockBackend(page);
  await page.goto('/rules?rule_id=7');
  const dialog = page.getByRole('dialog', { name: '编辑质量规则' });
  const selector = dialog.locator('.domain-ownership-select .el-select');
  await selector.click();
  await page.getByRole('option', { name: '户外 · outdoor' }).click();
  await dialog.getByRole('button', { name: '保存', exact: true }).click();
  await expect.poll(() => state.ruleWrites.length).toBe(1);
  expect(state.ruleWrites[0].owner_domain_id).toBe(42);
  await page.getByRole('button', { name: '编辑', exact: true }).click();
  await expect(selector).toContainText('不指定业务域');
  await dialog.getByRole('button', { name: '保存', exact: true }).click();
  await expect.poll(() => state.ruleWrites.length).toBe(2);
  expect(state.ruleWrites[1].owner_domain_id).toBeNull();
});

for (const path of ['/rules', '/plans', '/issues']) {
  test(`${path} filters public and exact domains and restores on refresh`, async ({ page }) => {
    const state = await installMockBackend(page);
    await page.goto(path);
    const selector = page.locator('.domain-ownership-select').first();
    await selector.locator('.el-select__wrapper').click();
    await page.getByRole('option', { name: '不指定业务域（租户公共）', exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`${path}\\?owner_domain_id=0$`));
    await expect.poll(() => state.listRequests.at(-1)?.query.owner_domain_id).toBe('0');
    await selector.locator('.el-select__wrapper').click();
    await page.getByRole('option', { name: /户外/ }).click();
    await expect.poll(() => state.listRequests.at(-1)?.query.owner_domain_id).toBe('42');
    await page.reload();
    await expect(selector).toContainText('户外');
    await expect.poll(() => state.listRequests.at(-1)?.query.owner_domain_id).toBe('42');
    expect(state.unexpected).toEqual([]);
  });
}

for (const [config, label] of [[undefined, '未记录'], [{ owner_domain_id: null }, '不指定业务域（租户公共）'], [{ owner_domain_id: 42 }, '业务域 #42']]) {
  test(`execution domain snapshot displays ${label}`, async ({ page }) => {
    await installMockBackend(page, { execution: { execution_config: config } });
    await page.goto(`/executions/${executionID}`);
    await expect(page.getByText('执行时归属业务域（快照）', { exact: true })).toBeVisible();
    await expect(page.getByText('执行时归属业务域（快照）', { exact: true }).locator('xpath=following-sibling::td[1]')).toHaveText(label);
  });
}

test('overview separates last attempt from observation and restores domain and window filters', async ({ page }) => {
  const state = await installMockBackend(page);
  await page.goto('/overview?owner_domain_id=42&days=7');
  await expect(page.getByRole('heading', { name: '质量概览', exact: true })).toBeVisible();
  await expect.poll(() => state.overviewRequests.at(-1)).toEqual({ owner_domain_id: '42', days: '7', page: '1', page_size: '20' });
  const observed = page.getByRole('row').filter({ hasText: '客户质量检查' });
  await expect(observed.getByRole('button', { name: '失败', exact: true })).toBeVisible();
  await expect(observed.getByRole('button', { name: '50.0% (1/2)', exact: true })).toBeVisible();
  await expect(observed.getByText('来自较早的方案版本', { exact: true })).toBeVisible();
  await expect(page.getByText('尚无完整检查结果', { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByText('尚无完整检查结果', { exact: true })).toBeVisible();
  expect(state.overviewRequests.at(-1)).toMatchObject({ owner_domain_id: '42', days: '7' });
  await page.locator('.window-select').click();
  await page.getByRole('option', { name: '近 90 天', exact: true }).click();
  await expect.poll(() => state.overviewRequests.at(-1)?.days).toBe('90');
  await observed.getByRole('button', { name: '50.0% (1/2)', exact: true }).click();
  await expect(page).toHaveURL(new RegExp('/executions/' + executionID));
  expect(state.unexpected).toEqual([]);
});

test('overview fetch failure is not displayed as zero quality', async ({ page }) => {
  await installMockBackend(page, { overviewFailure: true });
  await page.goto('/overview');
  await expect(page.getByText('统计服务暂时不可用', { exact: true })).toBeVisible();
  await expect(page.locator('.el-statistic')).toHaveCount(0);
  await expect(page.locator('.el-table')).toHaveCount(0);
});

async function installMockBackend(page, options = {}) {
  const state = {
    plans: [structuredClone(plan)],
    writes: [],
    ruleWrites: [],
    candidateRequests: [],
    sourceRequests: [],
    domainRequests: [],
    listRequests: [],
    runs: [],
    runRequests: [],
    overviewRequests: [],
    unexpected: [],
    issueStatusRequests: [],
    issues: [
      {
        id: 17,
        type: "format",
        rule_key: ruleKey,
        plan_id: 12,
        table_name: "customers",
        column_name: "mobile_phone",
        pass_rate: 86.67,
        failed_count: 4,
        execution_id: executionID,
        last_execution_id: executionID,
        engine_id: 2,
        status: options.issueStatus || "open",
        created_at: "2026-08-14T08:00:00Z",
      },
    ],
  };
  await page.addInitScript(() => {
    localStorage.setItem("addp-lang", "zh-cn");
    localStorage.setItem("theme-mode", "light");
  });
  await page.route("**/api/v1/**", async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname;
    if (req.method() === 'GET' && ['/api/v1/quality/rules', '/api/v1/quality/plans', '/api/v1/quality/issues'].includes(path)) {
      state.listRequests.push({ path, query: Object.fromEntries(url.searchParams) });
    }
    if (path === "/api/v1/system/refresh")
      return fulfillJSON(route, {
        access_token: "quality-e2e-token",
        expires_in: 3600,
      });
    if (path === "/api/v1/system/users/me")
      return fulfillJSON(route, { id: 1, username: "quality-e2e" });
    if (path === "/api/v1/system/auth/context")
      return fulfillJSON(route, {
        context: { type: "tenant" },
        authorization: {
          role_assignments: [
            {
              permissions: [
                ...["read", "create", "update", "delete", "execute"].map(
                  (a) => "quality.plan." + a,
                ),
                ...["read", "create", "update", "delete"].map(
                  (a) => "quality.rule." + a,
                ),
                "quality.issue.read",
                "quality.issue.update",
                "monitor.execution.read",
                "system.engine.read",
                "meta.catalog.read",
                ...(options.noStandardPermission ? [] : ["standard.element.read"]),
                ...(options.noDomainPermission ? [] : ["standard.domain.read"]),
              ],
            },
          ],
        },
      });
    if (path === "/api/v1/standard/domains") {
      state.domainRequests.push(path);
      return options.domainFailure
        ? fulfillJSON(route, { error: "unavailable" }, 503)
        : fulfillJSON(route, [{ id: 42, name: "户外", code: "outdoor" }]);
    }
    if (path === '/api/v1/quality/overview') {
      state.overviewRequests.push(Object.fromEntries(url.searchParams));
      if (options.overviewFailure) return fulfillJSON(route, { error: '统计服务暂时不可用' }, 503);
      return fulfillJSON(route, {
        plan_count: 2, never_run_plans: 1, open_issues: 3, unscoped_issues: 1, unscoped_executions: 2,
        total: 2, page: 1, page_size: 20, total_pages: 1,
        data: [
          { plan_id: 12, plan_name: plan.name, plan_version: 2, owner_domain_id: 42, target_key: 'a'.repeat(64), table_bindings: plan.table_bindings, execution_id: executionID, status: 'failed', observed_execution_id: executionID, observed_version: 1, observed_at: '2026-09-17T08:00:00Z', passed_rules: 1, total_rules: 2, pass_rate: 50 },
          { plan_id: 13, plan_name: '尚未执行的方案', plan_version: 1, owner_domain_id: null, target_key: null, observed_execution_id: null, pass_rate: null },
        ],
        trend: [{ day: '2026-09-17', executions: 2, runtime_errors: 1, passed_rules: 1, total_rules: 2, pass_rate: 50 }],
      });
    }
    const engines = [
      {
        id: 2,
        name: "业务 PostgreSQL",
        engine_type: "postgresql",
        engine_family: "tabular",
        lifecycle_state: "active",
        connection_status: "online",
      },
    ];
    if (["/api/v1/system/engines", "/api/v1/meta/engines"].includes(path))
      return fulfillJSON(route, engines);
    const table = {
      id: "table-23",
      label: "customers",
      type: "table",
      locator,
      children: [],
      has_children: false,
      metadata: { item_id: 23 },
    };
    const schema = {
      id: "schema-22",
      label: "public",
      type: "schema",
      locator: "addp://engine/2/path/public?type=schema&node_id=22",
      children: [table],
      has_children: true,
    };
    if (path === "/api/v1/meta/resource-tree/2")
      return fulfillJSON(route, {
        id: "db-21",
        label: "业务 PostgreSQL",
        type: "database",
        locator: "addp://engine/2/path/?type=database&node_id=21",
        children: [schema],
        has_children: true,
      });
    if (path === "/api/v1/meta/resource-tree/2/node")
      return fulfillJSON(route, {
        children: url.searchParams.get("locator").includes("type=database")
          ? [schema]
          : [table],
      });
    if (path === "/api/v1/system/engines/2/catalog/children") {
      const segments = req.postDataJSON()?.path?.segments || [],
        next = ["database", "public", "customers"][segments.length];
      return fulfillJSON(route, {
        nodes: options.missingTarget
          ? []
          : [
              {
                name: next,
                role: segments.length === 2 ? "leaf" : "branch",
                path: { segments: [...segments, next] },
              },
            ],
      });
    }
    if (path === "/api/v1/system/engines/2/catalog/facts")
      return fulfillJSON(route, {
        table: {
          fields: [
            { name: "id", type: "integer" },
            { name: "mobile_phone", type: "text" },
          ],
        },
      });
    if (path === "/api/v1/standard/elements/3/revisions/31") {
      state.sourceRequests.push(path);
      if (options.sourceFailure) return fulfillJSON(route, { error: "unavailable" }, 503);
      return fulfillJSON(route, { id:31, element_id:3, element_code:"member_status", name:"成员状态", revision_no:3, definition:"历史成员关系状态", code_set_revision: { code:"member_status_codes", name:"成员状态码值", revision_no:2, revision_id:"21", code_set_id:"2", description:"历史允许状态", items:[{ code:"signup",label:"报名",definition:"已报名" },{code:"leader",label:"领队",definition:"活动负责人"}] }, ...options.sourceDetail });
    }
    if (path === "/api/v1/quality/rules/element-candidates") {
      state.candidateRequests.push(Object.fromEntries(url.searchParams));
      if (options.candidateFailure) return fulfillJSON(route, { error:"unavailable" },503);
      if (options.emptyCandidates) return fulfillJSON(route,{data:[],total:0});
      if (options.pagedCandidates && url.searchParams.get('page') === '2') return fulfillJSON(route,{data:[{id:4,revision_id:41,revision_no:1,name:'第二页数据元',code:'second_page',quality_rules:{schema_version:'addp.quality.rules/v1',rules:[]}}],total:31});
      return fulfillJSON(route, {
        data: [
          {
            id: 3,
            revision_id: 31,
            revision_no: 3,
            name: "客户标识",
            code: "customer_id",
            quality_rules: {
              schema_version: "addp.quality.rules/v1",
              rules: options.emptyRules ? [] : [
                {
                  rule_key: ruleKey,
                  type: "length",
                  severity: "error",
                  enabled: true,
                  params: { min: 1, max: 64 },
                },
              ],
            },
          },
        ],
        total: options.pagedCandidates ? 31 : 1,
      });
    }
    if (path.startsWith("/api/v1/quality/rules")) {
      if (["POST", "PUT"].includes(req.method())) {
        const body = req.postDataJSON();
        state.ruleWrites.push(body);
        if (options.ruleConflict)
          return fulfillJSON(
            route,
            {
              error: "规则版本已变化",
              error_code: "resource_version_conflict",
            },
            409,
          );
        return fulfillJSON(
          route,
          { ...body, id: 7, revision_no: 2, version: 2 },
          req.method() === "POST" ? 201 : 200,
        );
      }
      if (path.endsWith("/plans"))
        return fulfillJSON(route, {
          data: state.plans,
          total: state.plans.length,
        });
      const latest = { ...rule, ...options.rule, revision_no: options.latestRevision || 1 };
      return fulfillJSON(
        route,
        path.endsWith("/7") ? latest : { data: [latest], total: 1 },
      );
    }
    if (path.startsWith("/api/v1/quality/plans")) {
      if (path.endsWith("/run")) {
        state.runs.push(Number(path.split("/").at(-2)));
        state.runRequests.push(req.postDataJSON());
        return fulfillJSON(route, { execution_id: executionID });
      }
      if (["PUT", "POST"].includes(req.method())) {
        const body = req.postDataJSON();
        state.writes.push(body);
        if (options.writeError)
          return fulfillJSON(
            route,
            {
              error: options.writeError,
              error_code: "resource_version_conflict",
            },
            409,
          );
        const saved = {
          ...body,
          id: req.method() === "POST" ? 13 : 12,
          version: 3,
        };
        if (req.method() === "POST") state.plans.push(saved);
        else state.plans[0] = saved;
        return fulfillJSON(route, saved, req.method() === "POST" ? 201 : 200);
      }
      const detail = state.plans.find(plan => path.endsWith(`/${plan.id}`));
      return fulfillJSON(route, detail ? { ...detail, execution_contract: mockPlanContract(detail) } : { data: state.plans, total: state.plans.length });
    }
    if (path === "/api/v1/quality/executions/" + executionID)
      return fulfillJSON(route, { ...execution, ...options.execution });
    if (req.method() === "PUT" && path === "/api/v1/quality/issues/17/status") {
      const body = req.postDataJSON();
      state.issueStatusRequests.push({
        id: 17,
        status: body.status,
        note: body.note,
      });
      if (options.issueStatusError)
        return fulfillJSON(route, { error: options.issueStatusError }, 409);
      state.issues[0].status = body.status;
      return fulfillJSON(route, state.issues[0]);
    }
    if (path === "/api/v1/quality/issues") {
      const status = url.searchParams.get("status"),
        data = state.issues.filter((i) => !status || i.status === status);
      return fulfillJSON(route, { data, total: data.length });
    }
    state.unexpected.push(req.method() + " " + path);
    return fulfillJSON(route, { error: "Unmocked request" }, 500);
  });
  return state;
}
async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

function mockPlanContract(plan) {
  const properties = {}, defaults = {}, fields = {}, required = [];
  for (const binding of plan.table_bindings) {
    properties[binding.alias] = { type: 'string', format: 'resource-locator', minLength: 1 };
    fields[binding.alias] = { control: 'resource_tree_picker', selectable_node_types: ['table'], engine_families: ['tabular'] };
    if (binding.locator) defaults[binding.alias] = binding.locator;
    else required.push(binding.alias);
  }
  return {
    input_schema: { type: 'object', properties: { table_bindings: { type: 'object', properties, required, additionalProperties: false } }, additionalProperties: false },
    input_defaults: { table_bindings: defaults },
    input_ui_schema: { table_bindings: { control: 'group', fields } },
    output_schema: { type: 'object', properties: { passed: { type: 'boolean' } }, additionalProperties: false },
  };
}
