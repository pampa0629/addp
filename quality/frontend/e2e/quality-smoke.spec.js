import { expect, test } from "@playwright/test";
const executionID = "bb1a324d-53a3-4f02-b666-d51385d258c8";
const ruleKey = "bfed8f3b-e3b4-8a8e-8140-860d0b0585ea";
const locator = "addp://engine/2/path/public/customers?type=table&node_id=23";
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
  await expect.poll(() => state.runs).toEqual([13]);
  await expect(page).toHaveURL(new RegExp("/executions/" + executionID));
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
  await page.getByRole("option", { name: "客户标识 · R3" }).click();
  await source.locator(".el-select").nth(1).click();
  await page.getByRole("option", { name: "长度", exact: true }).click();
  await source
    .getByRole("button", { name: "从数据元导入", exact: true })
    .click();
  await expect(dialog.getByText("标准修订 #31", { exact: true })).toBeVisible();
  await expect(source).toBeHidden();
  await page.screenshot({
    path: testInfo.outputPath("rule-editor.png"),
    fullPage: true,
  });
  await dialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => state.ruleWrites.length).toBe(1);
  expect(state.ruleWrites[0]).toMatchObject({
    type: "length",
    source: { element_id: 3, element_revision_id: 31, rule_key: ruleKey },
    params: { constraint: { min: 1, max: 64 } },
  });
  expect(state.writes).toEqual([]);
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

async function installMockBackend(page, options = {}) {
  const state = {
    plans: [structuredClone(plan)],
    writes: [],
    ruleWrites: [],
    runs: [],
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
              ],
            },
          ],
        },
      });
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
      metadata: { node_id: 23 },
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
    if (path === "/api/v1/quality/rules/element-candidates")
      return fulfillJSON(route, {
        data: [
          {
            id: 3,
            revision_id: 31,
            revision_no: 3,
            name: "客户标识",
            quality_rules: {
              schema_version: "addp.quality.rules/v1",
              rules: [
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
        total: 1,
      });
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
      const latest = { ...rule, revision_no: options.latestRevision || 1 };
      return fulfillJSON(
        route,
        path.endsWith("/7") ? latest : { data: [latest], total: 1 },
      );
    }
    if (path.startsWith("/api/v1/quality/plans")) {
      if (path.endsWith("/run")) {
        state.runs.push(Number(path.split("/").at(-2)));
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
      return fulfillJSON(
        route,
        path.endsWith("/12")
          ? state.plans[0]
          : { data: state.plans, total: state.plans.length },
      );
    }
    if (path === "/api/v1/quality/executions/" + executionID)
      return fulfillJSON(route, execution);
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
