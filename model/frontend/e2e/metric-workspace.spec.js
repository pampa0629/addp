import { test, expect } from '@playwright/test';

async function installBackend(page, options = {}) {
  const permissions = [
    'model.logical_model.read',
    ...(options.engineRead === false ? [] : ['meta.catalog.read']),
    'standard.metric.read',
    'service.definition.create',
    ...(options.serviceRead === false ? [] : ['service.definition.read']),
    'service.definition.update',
    ...['read', 'create', 'update', 'publish', 'offline', 'delete'].filter(action => action !== 'offline' || options.offline !== false).map(
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
      { id: 52, name: '昵称', column_name: 'nickname', data_type: 'string' },
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
  const engineReads = [];
  const referenceReads = [];
  const withdrawals = [];
  let conflict = false;
  let refreshCount = 0;
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), options.lang || 'zh-cn');
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
      return send({ access_token: `model-e2e-token-${++refreshCount}`, expires_in: 3600 });
    if (path === '/api/v1/system/users/me')
      return send({ id: 1, username: 'metric-author' });
    if (path === '/api/v1/system/auth/context')
      return send({
        context: { type: 'tenant' },
        authorization: { role_assignments: [{ permissions }] },
      });
    if (path === '/api/v1/meta/engines') {
      engineReads.push(path);
      return options.engineFailure
        ? send({ error: 'Engine names unavailable' }, 503)
        : send([{ id: 2, name: 'Business PostgreSQL', engine_type: 'postgresql' }, { id: 8, name: 'Archive PostgreSQL', engine_type: 'postgresql' }]);
    }
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
      item.revisions.find(r => r.status === 'draft').dependency_snapshot = sourceSnapshot();
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
    const withdrawMatch = path.match(/metric-implementations\/1\/revisions\/(\d+)\/withdraw$/);
    if (withdrawMatch) {
      const body = request.postDataJSON();
      writes.push(body);
      withdrawals.push({ revisionID: Number(withdrawMatch[1]), ...body });
      expect(body.version).toBe(item.version);
      if (options.withdrawConflict) return send({error:'版本冲突',error_code:'resource_version_conflict'},409);
      item.revisions.find(r => r.id === Number(withdrawMatch[1])).status = 'withdrawn';
      item.version++;
      return send(item);
    }
    if (path === '/api/v1/service/query' && request.method() === 'GET' && new URL(request.url()).searchParams.has('metric_revision_id')) {
      const query = Object.fromEntries(new URL(request.url()).searchParams);
      referenceReads.push(query);
      if (options.beforeReference) await options.beforeReference();
      if (options.referenceExpired) { options.referenceExpired = false; return send({error:'expired'},401); }
      if (options.referenceFailure) return send({ error: 'unavailable' }, options.referenceFailure);
      const all = query.metric_revision_id === '10' ? Array.from({length:options.referenceCount ?? 21}, (_, index) => ({ id:100+index, title:`Bound service ${index}`, service_name:`bound_${index}`, status:index===0?'inactive':'active' })) : [];
      const start = (Number(query.page)-1)*Number(query.limit);
      return send({data:all.slice(start,start+Number(query.limit)),total:all.length});
    }
    if (path === '/api/v1/service/query' && request.method() === 'GET')
      return send({
        data: [
          {
            id: 28,
            title: '人员重叠查询',
            service_name: 'person_overlap',
            config_type: 'analytical',
            status: 'inactive',
            service_version: '',
            version: options.missingVersion ? 0 : 1,
          },
        ],
        total: 1,
      });
    if (path === '/api/v1/service/query/28' && request.method() === 'GET')
      return send({id: 28, title: '人员重叠查询', service_name: 'person_overlap', config_type: 'analytical', version: 2});
    if (path === '/api/v1/service/query/28/metric-source') {
      writes.push(request.postDataJSON());
      if (options.serviceConflict && request.postDataJSON().version === 1)
        return send({error: 'changed', error_code: 'resource_version_conflict'}, 409);
      return send({ id: 28, version: request.postDataJSON().version + 1 });
    }
    if (path === '/api/v1/service/query') {
      writes.push(request.postDataJSON());
      return send({ id: 9 }, 201);
    }
    return send({ error: `Unexpected request: ${path}` }, 404);
  });
  return {
    writes,
    engineReads,
    referenceReads,
    withdrawals,
    get item() {
      return item;
    },
    tables,
    setConflict() {
      conflict = true;
    },
  };
}
function sourceSnapshot(engineId = 2, namespace = 'outdoor') {
  return {
    execution_plan: { engine_id: engineId },
    tables: Object.fromEntries([[3, 'dwd_outdoor_participation'], [4, 'dim_outdoor_activity'], [5, 'dim_outdoor_person']]
      .map(([id, name]) => [id, { id, name, locator: `addp://engine/${engineId}/path/${namespace}?type=schema&node_id=373` }])),
  };
}
function publishedRevision(id = 10, revisionNo = 1, engineId = 2, namespace = 'outdoor') {
  return {
    id, revision_no: revisionNo, status: 'published', metric_definition_revision_id: 62,
    contract: { operation: 'count_distinct', subject: { relation_id: 0, field_id: 31 }, subject_relation_id: 7,
      distinct: { relation_id: 0, field_id: 32 }, time: { relation_id: 8, field_id: 42 }, filters: [] },
    dependency_snapshot: sourceSnapshot(engineId, namespace),
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
  await expect(page.locator('.source-summary')).toContainText('保存并校验草稿后显示引擎和物理表来源。');
  await choose(page, '选择定义修订', '当前主领队活动次数 · R2');
  await choose(page, '主体字段', '活动参与事实 · 人员标识 (person_id)');
  await choose(page, '主体维度关系', '人员维度');
  await choose(page, '主体显示名称字段（可选）', '人员维度 · 昵称 (nickname)');
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
  await expect(page.locator('.source-summary')).toContainText('outdoor.dwd_outdoor_participation');
  await expect(page.locator('.source-summary')).toContainText('已保存来源 · R1');
  await page.getByRole('button', { name: '发布修订', exact: true }).click();
  await expect(
    page.getByRole('button', { name: '编辑新修订', exact: true }),
  ).toBeVisible();
  expect(backend.tables[0].version).toBe(17);
  await page.getByRole('button', { name: '编辑新修订', exact: true }).click();
  await expect(page.locator('.source-summary')).toContainText('配置尚未保存');
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
  const summary = page.getByRole('dialog').locator('.source-summary');
  await expect(summary).toContainText('当前主领队活动次数');
  await expect(summary).toContainText('已保存来源 · R1');
  await expect(summary).toContainText('Business PostgreSQL · #2');
  await expect(summary).toContainText('outdoor.dwd_outdoor_participation');
  await expect(summary.getByRole('button')).toHaveCount(0);
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
  expect(backend.item.revisions[1].contract.subject_label).toEqual({ relation_id: 7, field_id: 52 });
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
  await choose(page, '选择查询服务', '人员重叠查询 (person_overlap)');
  const summary = page.getByRole('dialog').locator('.source-summary');
  await expect(summary).toContainText('已保存来源 · R1');
  await expect(summary).toContainText('outdoor.dwd_outdoor_participation');
  await expect(summary).toContainText('outdoor.dim_outdoor_activity');
  await expect(summary).toContainText('outdoor.dim_outdoor_person');
  await page
    .getByRole('dialog')
    .getByRole('button', { name: '发布查询服务', exact: true })
    .click();
  await expect(page).toHaveURL(/service\/query-services\/28$/);
  expect(backend.writes.at(-1)).toEqual({
    metric_source: { implementation_id: 1, revision_id: 10 },
    version: 1,
  });
});


test('missing service definition version is explained before attempting rebind', async ({ page }) => {
  const backend = await installBackend(page, { missingVersion: true });
  backend.item.revisions.push({ id: 10, revision_no: 2, status: 'published', metric_definition_revision_id: 62,
    contract: { operation: 'count_distinct', subject: { relation_id: 0, field_id: 31 }, subject_relation_id: 7,
      distinct: { relation_id: 0, field_id: 32 }, time: { relation_id: 8, field_id: 42 }, filters: [] } });
  await page.goto('/metric-implementations/1?revision_id=10');
  await page.getByRole('button', { name: '发布查询服务', exact: true }).click();
  await page.getByText('替换现有服务来源', { exact: true }).click();
  await choose(page, '选择查询服务', '人员重叠查询 (person_overlap)');
  await page.getByRole('dialog').getByRole('button', { name: '发布查询服务', exact: true }).click();
  await expect(page.getByRole('alert').filter({ hasText: '未获取到服务定义版本' })).toBeVisible();
  await expect(page.getByRole('dialog')).toBeVisible();
  expect(backend.writes).toEqual([]);
});

 test('rebind conflict preserves selection and reloads only on explicit request', async ({ page }) => {
  const backend = await installBackend(page, { serviceConflict: true });
  backend.item.revisions.push({ id: 10, revision_no: 2, status: 'published', metric_definition_revision_id: 62,
    contract: { operation: 'count_distinct', subject: { relation_id: 7, field_id: 31 }, distinct: { relation_id: 0, field_id: 32 }, time: { relation_id: 8, field_id: 42 }, filters: [] } });
  await page.goto('/metric-implementations/1?revision_id=10');
  await page.getByRole('button', { name: '发布查询服务', exact: true }).click();
  await page.getByText('替换现有服务来源', { exact: true }).click();
  await choose(page, '选择查询服务', '人员重叠查询 (person_overlap)');
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('button', { name: '发布查询服务', exact: true }).click();
  await expect(dialog.getByText('服务已变化，已保留当前选择。请重新加载服务后再发布。')).toBeVisible();
  await expect(dialog.getByRole('button', { name: '发布查询服务', exact: true })).toBeDisabled();
  expect(backend.writes).toHaveLength(1);
  await dialog.getByRole('button', { name: '重新加载服务', exact: true }).click();
  await dialog.getByRole('button', { name: '发布查询服务', exact: true }).click();
  await expect(page).toHaveURL(/service\/query-services\/28$/);
  expect(backend.writes.map(body => body.version)).toEqual([1, 2]);
 });


test('source summary follows the selected frozen revision rather than current table bindings', async ({ page }) => {
  const backend = await installBackend(page);
  backend.item.revisions.push(publishedRevision(), publishedRevision(11, 2, 8, 'archive'));
  backend.tables[0].materialization = { target_parent_locator: 'addp://engine/99/path/changed?type=schema&node_id=4', target_name: 'changed_table' };
  await page.goto('/metric-implementations/1?revision_id=10');
  const summary = page.locator('.source-summary');
  await expect(summary).toContainText('Business PostgreSQL · #2');
  await expect(summary).toContainText('outdoor.dwd_outdoor_participation');
  await expect(summary).toContainText('outdoor.dim_outdoor_activity');
  await expect(summary).toContainText('outdoor.dim_outdoor_person');
  await expect(summary).not.toContainText('changed_table');
  await expect(summary.locator('.el-table__body tbody tr')).toHaveCount(3);
  await page.locator('.el-card').first().locator('.el-select__wrapper').click();
  await page.getByRole('option', { name: 'R2 · 已发布', exact: true }).click();
  await expect(summary).toContainText('已保存来源 · R2');
  await expect(summary).toContainText('Archive PostgreSQL · #8');
  await expect(summary).toContainText('archive.dwd_outdoor_participation');
  await expect(summary).not.toContainText('outdoor.dwd_outdoor_participation');
  expect(backend.writes).toEqual([]);
  await summary.getByRole('button', { name: '活动参与事实', exact: true }).click();
  await expect(page).toHaveURL(/logical-tables\/3\?tab=physical-target$/);
});

for (const mode of ['no permission', 'directory unavailable']) {
  test(`source bindings remain visible with ${mode}`, async ({ page }) => {
    const backend = await installBackend(page, { engineRead: mode !== 'no permission', engineFailure: true });
    backend.item.revisions.push(publishedRevision());
    await page.goto('/metric-implementations/1');
    const summary = page.locator('.source-summary');
    await expect(summary).toContainText('引擎 #2');
    await expect(summary).toContainText('outdoor.dwd_outdoor_participation');
    await expect(page.getByRole('button', { name: '发布查询服务', exact: true })).toBeEnabled();
    if (mode === 'no permission') expect(backend.engineReads).toEqual([]);
    else await expect(summary).toContainText('引擎名称暂不可用');
    expect(backend.writes).toEqual([]);
  });
}

test('a missing snapshot never substitutes the current logical table target', async ({ page }) => {
  const backend = await installBackend(page);
  const revision = publishedRevision();
  delete revision.dependency_snapshot;
  backend.item.revisions.push(revision);
  backend.tables[0].materialization = { target_parent_locator: 'addp://engine/99/path/changed?type=schema&node_id=4', target_name: 'changed_table' };
  await page.goto('/metric-implementations/1');
  const summary = page.locator('.source-summary');
  await expect(summary).toContainText('此修订缺少可展示的来源快照');
  await expect(summary).not.toContainText('changed_table');
  await expect(summary.locator('.el-table')).toHaveCount(0);
});

test('source summary uses English labels and fits a narrow page', async ({ page }) => {
  await page.setViewportSize({ width: 720, height: 900 });
  const backend = await installBackend(page, { lang: 'en' });
  backend.item.revisions.push(publishedRevision());
  await page.goto('/metric-implementations/1');
  const summary = page.locator('.source-summary');
  await expect(summary).toContainText('Data sources');
  await expect(summary).toContainText('Saved sources · R1');
  await expect(summary).toContainText('Execution engine');
  await expect(summary).toContainText('Physical table');
  await expect(summary).toContainText('Business PostgreSQL · #2');
  expect(await summary.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
});


for (const lang of ['zh-cn', 'en']) {
  test(`publication summary follows the selected revision in ${lang} at narrow width`, async ({ page }) => {
    await page.setViewportSize({ width: 720, height: 900 });
    const backend = await installBackend(page, { lang });
    backend.item.revisions.push(publishedRevision(11, 2, 8, 'archive'), publishedRevision());
    const english = lang === 'en';
    const publish = english ? 'Publish query service' : '发布查询服务';
    await page.goto('/metric-implementations/1?revision_id=10');
    await page.getByRole('button', { name: publish, exact: true }).click();
    const dialog = page.getByRole('dialog');
    const summary = dialog.locator('.source-summary');
    await expect(summary).toContainText(english ? 'Saved sources · R1' : '已保存来源 · R1');
    await expect(summary).toContainText('Business PostgreSQL · #2');
    await expect(summary).not.toContainText('archive.');
    await expect(summary).toContainText(english ? 'Physical table' : '物理表');
    await expect(summary.getByRole('button')).toHaveCount(0);
    expect(await dialog.evaluate(el => {
      const rect = el.getBoundingClientRect();
      return rect.left >= 0 && rect.right <= window.innerWidth && el.scrollWidth <= el.clientWidth;
    })).toBe(true);
    await dialog.locator('.el-dialog__headerbtn').click();
    await page.locator('.el-card').first().locator('.el-select__wrapper').click();
    await page.getByRole('option', { name: english ? 'R2 · Published' : 'R2 · 已发布', exact: true }).click();
    await page.getByRole('button', { name: publish, exact: true }).click();
    await expect(summary).toContainText(english ? 'Saved sources · R2' : '已保存来源 · R2');
    await expect(summary).toContainText('Archive PostgreSQL · #8');
    await expect(summary).toContainText('archive.dwd_outdoor_participation');
    await expect(summary).not.toContainText('outdoor.');
    const screenshotPath = test.info().outputPath('publication-source-summary.png');
    await dialog.screenshot({ path: screenshotPath });
    await test.info().attach('publication-source-summary', { path: screenshotPath, contentType: 'image/png' });
    expect(backend.writes).toEqual([]);
  });
}

for (const lang of ['zh-cn', 'en']) {
  test(`revision service references are lazy, paginated and revision-specific (${lang})`, async ({page}) => {
    await page.setViewportSize({width:720,height:900});
    const backend = await installBackend(page, {lang});
    backend.item.revisions.push(publishedRevision(), publishedRevision(11,2));
    await page.goto('/metric-implementations/1?revision_id=10');
    const label = lang === 'en' ? 'Services referencing this revision' : '引用此修订的服务';
    await expect(page.getByRole('button',{name:label,exact:true})).toBeVisible();
    expect(backend.referenceReads).toHaveLength(0);
    await page.getByRole('button',{name:label,exact:true}).click();
    const dialog=page.getByRole('dialog');
    await expect(dialog.getByRole('button',{name:'Bound service 0',exact:true})).toBeVisible();
    await expect(dialog.getByText(lang==='en'?'Inactive':'已停用',{exact:true})).toBeVisible();
    expect(backend.referenceReads[0]).toEqual({metric_implementation_id:'1',metric_revision_id:'10',page:'1',limit:'20'});
    await dialog.getByRole('button',{name:lang==='en'?'Go to next page':'下一页'}).click();
    await expect(dialog.getByRole('button',{name:'Bound service 20',exact:true})).toBeVisible();
    await dialog.getByRole('button',{name:lang==='en'?'Close this dialog':'关闭此对话框'}).click();
    await page.goto('/metric-implementations/1?revision_id=11');
    await page.getByRole('button',{name:label,exact:true}).click();
    await expect(page.getByRole('dialog').getByText(lang==='en'?'No services reference this revision':'暂无服务引用此修订')).toBeVisible();
    expect(backend.referenceReads.at(-1).metric_revision_id).toBe('11');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
    expect(backend.writes).toHaveLength(0);
  });
}
test('reference query errors remain distinct from empty results and can be retried', async ({page}) => {
  const options={referenceFailure:503};
  const backend=await installBackend(page,options);
  backend.item.revisions.push(publishedRevision());
  await page.goto('/metric-implementations/1?revision_id=10');
  await page.getByRole('button',{name:'引用此修订的服务',exact:true}).click();
  const dialog=page.getByRole('dialog');
  await expect(dialog.getByText('暂时无法查询引用服务，请刷新重试')).toBeVisible();
  await expect(dialog.getByText('暂无服务引用此修订')).toHaveCount(0);
  options.referenceFailure=403;
  await dialog.getByRole('button',{name:'刷新',exact:true}).click();
  await expect(dialog.getByText('没有读取服务定义的权限')).toBeVisible();
  options.referenceFailure=0;
  await dialog.getByRole('button',{name:'刷新',exact:true}).click();
  await expect(dialog.getByRole('button',{name:'Bound service 0',exact:true})).toBeVisible();
  await page.context().route('**/service/query-services/100', route => route.fulfill({contentType:'text/html',body:'<p>Referenced service</p>'}));
  await dialog.getByRole('button',{name:'Bound service 0',exact:true}).click();
  await expect(page).toHaveURL(/service\/query-services\/100$/);
  expect(backend.writes).toHaveLength(0);
});
test('no Service read permission means no reference entry or request', async ({page}) => {
  const backend=await installBackend(page,{serviceRead:false});
  backend.item.revisions.push(publishedRevision());
  await page.goto('/metric-implementations/1?revision_id=10');
  await expect(page.getByRole('heading',{name:'当前主领队活动次数'})).toBeVisible();
  await expect(page.getByRole('button',{name:'引用此修订的服务',exact:true})).toHaveCount(0);
  expect(backend.referenceReads).toHaveLength(0);
});

test('refresh after rebind reduces pages without falsely reporting no references', async ({page}) => {
  const options={referenceCount:21};
  const backend=await installBackend(page,options);
  backend.item.revisions.push(publishedRevision());
  await page.goto('/metric-implementations/1?revision_id=10');
  await page.getByRole('button',{name:'引用此修订的服务',exact:true}).click();
  const dialog=page.getByRole('dialog');
  await expect(dialog.getByRole('button',{name:'Bound service 0',exact:true})).toBeVisible();
  await dialog.getByRole('button',{name:'下一页',exact:true}).click();
  await expect(dialog.getByRole('button',{name:'Bound service 20',exact:true})).toBeVisible();
  options.referenceCount=1;
  await dialog.getByRole('button',{name:'刷新',exact:true}).click();
  await expect(dialog.getByRole('button',{name:'Bound service 0',exact:true})).toBeVisible();
  await expect(dialog.getByText('暂无服务引用此修订')).toHaveCount(0);
  expect(backend.referenceReads.at(-1).page).toBe('1');
});

test('reference dialog survives token renewal and authorization reload', async ({page}) => {
  const backend=await installBackend(page,{referenceExpired:true});
  backend.item.revisions.push(publishedRevision());
  await page.goto('/metric-implementations/1?revision_id=10');
  await page.getByRole('button',{name:'引用此修订的服务',exact:true}).click();
  const dialog=page.getByRole('dialog');
  await expect(dialog.getByRole('button',{name:'Bound service 0',exact:true})).toBeVisible();
  expect(backend.referenceReads.length).toBeGreaterThan(1);
  await expect(dialog.getByText('没有读取服务定义的权限')).toHaveCount(0);
  expect(backend.writes).toHaveLength(0);
});

for (const lang of ['zh-cn', 'en']) {
  test(`withdrawal confirms the total for the exact revision and supports cancel (${lang})`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    const options = { lang, referenceCount: 21 };
    const backend = await installBackend(page, options);
    backend.item.revisions.push(publishedRevision(11, 2), publishedRevision());
    backend.item.version = 7;
    await page.goto('/metric-implementations/1?revision_id=10');
    const entry = page.getByRole('button', { name: lang === 'en' ? 'Withdraw revision' : '撤回修订', exact: true });
    await entry.click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText(lang === 'en' ? '21 services reference this revision' : '21 个服务引用此修订');
    await expect(dialog).toContainText(lang === 'en' ? 'including inactive services' : '含已停用服务');
    await expect(dialog).toContainText(lang === 'en' ? 'will reject new queries' : '将拒绝新的查询');
    expect(backend.referenceReads.at(-1)).toEqual({ metric_implementation_id: '1', metric_revision_id: '10', page: '1', limit: '1' });
    expect(backend.writes).toHaveLength(0);
    await dialog.getByRole('button', { name: lang === 'en' ? 'Cancel' : '取消', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(backend.writes).toHaveLength(0);
    options.referenceCount = 0;
    await entry.click();
    await expect(dialog).toContainText(lang === 'en' ? '0 services reference' : '0 个服务引用');
    options.referenceCount = 2;
    await dialog.getByRole('button', { name: lang === 'en' ? 'Refresh' : '刷新', exact: true }).click();
    await expect(dialog).toContainText(lang === 'en' ? '2 services reference' : '2 个服务引用');
    const box = await dialog.boundingBox();
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(390);
    await dialog.getByRole('button', { name: lang === 'en' ? 'Confirm withdrawal' : '确认撤回', exact: true }).click();
    await expect(dialog).not.toBeVisible();
    expect(backend.withdrawals).toEqual([{ revisionID: 10, version: 7 }]);
    expect(backend.item.revisions.find(r => r.id === 11).status).toBe('published');
    expect(backend.item.revisions.find(r => r.id === 10).status).toBe('withdrawn');
  });
}

test('withdrawal reports failed and forbidden counts as unknown, and refresh recovers', async ({ page }) => {
  const options = { referenceFailure: 503 };
  const backend = await installBackend(page, options);
  backend.item.revisions.push(publishedRevision());
  await page.goto('/metric-implementations/1?revision_id=10');
  await page.getByRole('button', { name: '撤回修订', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('暂时无法确认引用服务数量');
  await expect(dialog).not.toContainText('0 个服务');
  options.referenceFailure = 403;
  await dialog.getByRole('button', { name: '刷新', exact: true }).click();
  await expect(dialog).toContainText('没有读取服务定义的权限，无法确认引用服务数量');
  options.referenceFailure = 0;
  await dialog.getByRole('button', { name: '刷新', exact: true }).click();
  await expect(dialog).toContainText('21 个服务引用');
  expect(backend.writes).toHaveLength(0);
});

test('withdraw permission is independent of service read permission', async ({ page }) => {
  const backend = await installBackend(page, { serviceRead: false });
  backend.item.revisions.push(publishedRevision());
  await page.goto('/metric-implementations/1?revision_id=10');
  await page.getByRole('button', { name: '撤回修订', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('没有读取服务定义的权限，无法确认引用服务数量');
  expect(backend.referenceReads).toHaveLength(0);
  await dialog.getByRole('button', { name: '确认撤回', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(backend.withdrawals).toEqual([{ revisionID: 10, version: 1 }]);
});

test('users without offline permission cannot open a withdrawal confirmation', async ({ page }) => {
  const backend = await installBackend(page, { offline: false });
  backend.item.revisions.push(publishedRevision());
  await page.goto('/metric-implementations/1?revision_id=10');
  await expect(page.getByRole('heading', { name: '当前主领队活动次数' })).toBeVisible();
  await expect(page.getByRole('button', { name: '撤回修订', exact: true })).toHaveCount(0);
  expect(backend.referenceReads).toHaveLength(0);
  expect(backend.writes).toHaveLength(0);
});

test('withdrawal waits for the count, ignores cancelled responses and survives token renewal', async ({ page }) => {
  let release;
  const held = new Promise(resolve => { release = resolve; });
  const options = { beforeReference: () => held };
  const backend = await installBackend(page, options);
  backend.item.revisions.push(publishedRevision());
  await page.goto('/metric-implementations/1?revision_id=10');
  const entry = page.getByRole('button', { name: '撤回修订', exact: true });
  await entry.click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('正在查询引用服务数量');
  await expect(dialog.getByRole('button', { name: '确认撤回', exact: true })).toBeDisabled();
  await expect.poll(() => backend.referenceReads.length).toBe(1);
  await dialog.getByRole('button', { name: '取消', exact: true }).click();
  options.beforeReference = null;
  options.referenceCount = 2;
  options.referenceExpired = true;
  await entry.click();
  await expect(dialog).toContainText('2 个服务引用');
  options.referenceCount = 99;
  const late = page.waitForResponse(response => response.url().includes('/service/query?') && response.status() === 200);
  release();
  await late;
  await expect(dialog).toContainText('2 个服务引用');
  await expect(dialog).not.toContainText('99 个服务');
  expect(backend.writes).toHaveLength(0);
});

test('withdrawal conflicts keep confirmation open and never retry the write automatically', async ({ page }) => {
  const backend = await installBackend(page, { withdrawConflict: true });
  backend.item.revisions.push(publishedRevision());
  await page.goto('/metric-implementations/1?revision_id=10');
  await page.getByRole('button', { name: '撤回修订', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('21 个服务引用');
  await dialog.getByRole('button', { name: '确认撤回', exact: true }).click();
  await expect(page.getByText(/资源已被其他用户修改/)).toBeVisible();
  await expect(dialog).toBeVisible();
  expect(backend.withdrawals).toEqual([{ revisionID: 10, version: 1 }]);
  expect(backend.item.revisions[0].status).toBe('published');
});
