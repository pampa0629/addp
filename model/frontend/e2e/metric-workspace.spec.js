import { test, expect } from '@playwright/test';

async function installBackend(page) {
  const permissions = [
    'model.logical_model.read',
    'standard.metric.read',
    'service.definition.create',
    'service.definition.read',
    'service.definition.update',
    ...['read', 'create', 'update', 'publish', 'offline', 'delete'].map(
      (action) => `model.metric_implementation.${action}`,
    ),
  ];
  const tables = [
    {
      id: 3,
      name: '活动参与事实',
      code: 'participation',
      table_type: 'fact',
      status: 'approved',
      version: 17,
    },
    {
      id: 4,
      name: '活动维度',
      code: 'activity',
      table_type: 'dimension',
      status: 'approved',
    },
    {
      id: 5,
      name: '人员维度',
      code: 'person',
      table_type: 'dimension',
      status: 'approved',
    },
  ];
  const fields = {
    3: [
      {
        id: 31,
        name: '人员标识',
        column_name: 'person_id',
        data_type: 'string',
      },
      {
        id: 32,
        name: '活动标识',
        column_name: 'activity_id',
        data_type: 'string',
      },
      {
        id: 33,
        name: '当前主领队',
        column_name: 'is_current_leader',
        data_type: 'bool',
      },
    ],
    4: [
      {
        id: 41,
        name: '活动标识',
        column_name: 'activity_id',
        data_type: 'string',
        is_pk: true,
      },
      {
        id: 42,
        name: '活动日期',
        column_name: 'activity_date',
        data_type: 'date',
      },
    ],
    5: [
      {
        id: 51,
        name: '人员标识',
        column_name: 'person_id',
        data_type: 'string',
        is_pk: true,
      },
    ],
  };
  let item = {
    id: 1,
    fact_table_id: 3,
    metric_definition_id: 6,
    name: '当前主领队活动次数',
    version: 1,
    revisions: [],
  };
  const writes = [];
  let conflict = false;
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'));
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request(),
      path = new URL(request.url()).pathname;
    const send = (data, status = 200) =>
      route.fulfill({
        status,
        contentType: 'application/json',
        body: JSON.stringify(data),
      });
    if (path === '/api/v1/system/refresh')
      return send({ access_token: 'model-e2e-token', expires_in: 3600 });
    if (path === '/api/v1/system/users/me')
      return send({ id: 1, username: 'metric-author' });
    if (path === '/api/v1/system/auth/context')
      return send({
        context: { type: 'tenant' },
        authorization: { role_assignments: [{ permissions }] },
      });
    if (path === '/api/v1/standard/metrics')
      return send({
        data: [
          {
            id: 6,
            code: 'leader_count',
            current_revision: { id: 62, name: item.name, status: 'published' },
          },
        ],
        total: 1,
      });
    if (path === '/api/v1/standard/metrics/6/revisions')
      return send([
        { id: 62, name: item.name, status: 'published', revision_no: 2 },
      ]);
    if (path === '/api/v1/model/logical-tables')
      return send({ data: tables, total: 3 });
    const fieldMatch = path.match(/logical-tables\/(\d+)\/fields$/);
    if (fieldMatch) return send(fields[fieldMatch[1]]);
    if (path === '/api/v1/model/logical-tables/3/dimension-relations')
      return send([
        {
          id: 7,
          source_table: 3,
          source_field: 31,
          target_table: 5,
          target_field: 51,
          target_table_name: '人员维度',
        },
        {
          id: 8,
          source_table: 3,
          source_field: 32,
          target_table: 4,
          target_field: 41,
          target_table_name: '活动维度',
        },
      ]);
    if (
      path === '/api/v1/model/metric-implementations' &&
      request.method() === 'GET'
    )
      return send([item]);
    if (
      path === '/api/v1/model/metric-implementations' &&
      request.method() === 'POST'
    ) {
      writes.push(request.postDataJSON());
      return send(item, 201);
    }
    if (
      path === '/api/v1/model/metric-implementations/1' &&
      request.method() === 'GET'
    )
      return send(item);
    if (path === '/api/v1/model/metric-implementations/1/draft') {
      const body = request.postDataJSON();
      writes.push(body);
      if (conflict)
        return send(
          { error: '版本冲突', error_code: 'resource_version_conflict' },
          409,
        );
      expect(body.version).toBe(item.version);
      const draft = item.revisions.find((r) => r.status === 'draft');
      if (draft) Object.assign(draft, body);
      else
        item.revisions.unshift({
          ...body,
          id: 10 + item.revisions.length,
          revision_no: item.revisions.length + 1,
          status: 'draft',
        });
      item.version++;
      return send(item);
    }
    if (path.endsWith('/publish')) {
      const body = request.postDataJSON();
      writes.push(body);
      expect(body.version).toBe(item.version);
      item.revisions[0].status = 'published';
      item.version++;
      return send(item);
    }
    if (path === '/api/v1/service/query' && request.method() === 'GET')
      return send({
        data: [
          {
            id: 28,
            title: '人员重叠查询',
            service_name: 'person_overlap',
            config_type: 'sql',
            service_version: 'publication-28',
          },
        ],
        total: 1,
      });
    if (path === '/api/v1/service/query/28/metric-source') {
      writes.push(request.postDataJSON());
      return send({ id: 28 });
    }
    if (path === '/api/v1/service/query') {
      writes.push(request.postDataJSON());
      return send({ id: 9 }, 201);
    }
    return send({ error: `Unexpected request: ${path}` }, 404);
  });
  return {
    writes,
    get item() {
      return item;
    },
    tables,
    setConflict() {
      conflict = true;
    },
  };
}
async function choose(page, label, option) {
  const field = page.locator('.el-form-item').filter({
    has: page.locator('.el-form-item__label').filter({ hasText: label }),
  });
  await field.locator('.el-select__wrapper').click();
  const list = await field.getByRole('combobox').getAttribute('aria-controls');
  await page
    .locator(`#${list}`)
    .getByRole('option', { name: option, exact: true })
    .click();
}

test('metric draft publishes independently and Service binds the requested immutable revision', async ({
  page,
}) => {
  const backend = await installBackend(page);
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/metric-implementations/1');
  await choose(page, '选择定义修订', '当前主领队活动次数 · R2');
  await choose(page, '主体字段', '活动参与事实 · 人员标识 (person_id)');
  await choose(page, '主体维度关系', '人员维度');
  await choose(page, '去重字段', '活动参与事实 · 活动标识 (activity_id)');
  await choose(page, '日期字段', '活动维度 · 活动日期 (activity_date)');
  await page.getByRole('button', { name: '增加条件', exact: true }).click();
  await page.locator('.filter-row .el-select__wrapper').first().click();
  await page
    .getByRole('option', {
      name: '活动参与事实 · 当前主领队 (is_current_leader)',
      exact: true,
    })
    .click();
  await page
    .getByRole('button', { name: '保存并校验草稿', exact: true })
    .click();
  await expect(
    page.getByRole('button', { name: '发布修订', exact: true }),
  ).toBeEnabled();
  await page.getByRole('button', { name: '发布修订', exact: true }).click();
  await expect(
    page.getByRole('button', { name: '编辑新修订', exact: true }),
  ).toBeVisible();
  expect(backend.tables[0].version).toBe(17);
  await page.getByRole('button', { name: '编辑新修订', exact: true }).click();
  await page
    .getByRole('button', { name: '保存并校验草稿', exact: true })
    .click();
  await expect.poll(() => backend.item.revisions.length).toBe(2);
  await page.goto('/metric-implementations/1?revision_id=10');
  await expect(
    page.getByRole('button', { name: '发布查询服务', exact: true }),
  ).toBeVisible();
  await page.context().route('**/service/query-services/9', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: '<p>Metric query service</p>',
    }),
  );
  await page.getByRole('button', { name: '发布查询服务', exact: true }).click();
  await page
    .getByRole('dialog')
    .getByRole('button', { name: '发布查询服务', exact: true })
    .click();
  await expect(page).toHaveURL(/\/service\/query-services\/9$/);
  expect(backend.writes.at(-1).metric_source).toEqual({
    implementation_id: 1,
    revision_id: 10,
  });
  expect(backend.writes.at(-1)).not.toHaveProperty('sql_query');
  expect(backend.item.revisions[1].contract.filters[0].value).toBe(true);
  expect(errors).toEqual([]);
});

test('creating a metric identity uses the approved fact without a fact version', async ({
  page,
}) => {
  const backend = await installBackend(page);
  await page.goto('/metric-implementations/new?fact_table_id=3');
  await choose(page, '指标定义', '当前主领队活动次数');
  await page.getByRole('button', { name: '新建实现', exact: true }).click();
  await expect(page).toHaveURL(/metric-implementations\/1$/);
  expect(backend.writes[0]).toEqual({
    fact_table_id: 3,
    metric_definition_id: 6,
    name: '当前主领队活动次数',
    note: '',
  });
});

test('overlap uses an explicit calculation and replaces a selected service publication', async ({
  page,
}) => {
  const backend = await installBackend(page);
  await page.goto('/metric-implementations/1');
  await choose(page, '计算方式', '定向集合重叠率');
  await expect(
    page.getByRole('button', { name: '增加条件', exact: true }),
  ).toHaveCount(0);
  await choose(page, '选择定义修订', '当前主领队活动次数 · R2');
  await choose(page, '主体字段', '活动参与事实 · 人员标识 (person_id)');
  await choose(page, '主体维度关系', '人员维度');
  await choose(page, '去重字段', '活动参与事实 · 活动标识 (activity_id)');
  await choose(page, '日期字段', '活动维度 · 活动日期 (activity_date)');
  await page
    .getByRole('button', { name: '保存并校验草稿', exact: true })
    .click();
  await page.getByRole('button', { name: '发布修订', exact: true }).click();
  expect(backend.item.revisions[0].contract.operation).toBe(
    'directional_overlap',
  );
  expect(backend.item.revisions[0].contract.filters).toEqual([]);
  await page.context().route('**/service/query-services/28', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: '<p>Rebound service</p>',
    }),
  );
  await page.getByRole('button', { name: '发布查询服务', exact: true }).click();
  await page.getByText('替换现有服务来源', { exact: true }).click();
  await expect(page.getByRole('radio', { name: '替换现有服务来源', exact: true })).toBeChecked();
  await choose(page, '选择 SQL 查询服务', '人员重叠查询 (person_overlap)');
  await page
    .getByRole('dialog')
    .getByRole('button', { name: '发布查询服务', exact: true })
    .click();
  await expect(page).toHaveURL(/service\/query-services\/28$/);
  expect(backend.writes.at(-1)).toEqual({
    metric_source: { implementation_id: 1, revision_id: 10 },
    service_version: 'publication-28',
  });
});
