import { expect, test } from '@playwright/test'
import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

test.use({ timezoneId: 'Asia/Shanghai' })

const descriptor = (type, displayName, family, port = 5432) => ({
  type, display_name: displayName, origin: 'general', registration_group: family,
  capabilities: { schema_version: 'engine.capabilities/v1', engine_type: type, engine_family: family },
  connection_spec: { schema_version: 'engine.connection/v1', fields: [
    { key: 'host', label_key: 'storageEngine.host', input: 'text', default: 'localhost', required: true },
    { key: 'port', label_key: 'storageEngine.port', input: 'number', default: port }
  ] }
})
const descriptors = [
  descriptor('clickhouse', 'ClickHouse', 'tabular', 9000),
  descriptor('postgresql', 'PostgreSQL', 'tabular'),
  descriptor('oracle', 'Oracle', 'tabular', 1521),
  { ...descriptor('spark', 'Apache Spark', 'tabular', 10000), registration_group: 'compute' },
  { ...descriptor('vendor_compute', 'Another compute engine', 'query_runtime', 1235), registration_group: 'compute' },
  descriptor('futuredb', 'Newly compiled database', 'tabular', 1234),
  descriptor('mongodb', 'MongoDB', 'dynamic_schema', 27017),
  descriptor('elasticsearch', 'Elasticsearch', 'dynamic_schema', 9200),
  descriptor('redis', 'Redis', 'key_value', 6379),
  descriptor('neo4j', 'Neo4j', 'graph', 7687),
  descriptor('minio', 'MinIO', 'object', 9000),
  descriptor('s3', 'Amazon S3', 'object', 443),
  descriptor('hdfs', 'HDFS', 'file', 8020),
  descriptor('nfs', 'NFS', 'file', 2049),
  descriptor('kafka', 'Apache Kafka', 'event_stream', 9092)
]

async function fixture(page, language = 'zh-cn', typeDescriptors = descriptors, engineFacts = {}) {
  const writes = []
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || browserTestOrigin('system'),
      'access-control-allow-credentials': 'true', 'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,POST,OPTIONS' }
    const reply = json => route.fulfill({ json, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'engine-types-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Administrator' })
    if (path.endsWith('/auth/context')) return reply({ principal: { id: '1', principal_type: 'user' },
      context: { type: 'tenant', tenant_id: '2', tenant_membership_id: '4' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '2' },
        permissions: ['system.engine.read', 'system.engine.create', 'system.engine.update'] }] } })
    if (request.method() !== 'GET') writes.push({ path, body: request.postDataJSON() })
    if (path.endsWith('/engine-types')) return reply(typeDescriptors)
    if (path.endsWith('/engines/2')) return reply({ id: '2', name: 'Existing HighGo', engine_type: 'highgo', engine_origin: 'general', lifecycle_state: 'active', connection_info: {}, ...engineFacts })
    if (path.endsWith('/engines')) return reply([{ id: '2', name: 'Existing PostgreSQL',
      engine_type: 'postgresql', engine_origin: 'general', lifecycle_state: 'active',
      connection_status: 'online', is_builtin: false, capabilities: { schema_version: 'engine.capabilities/v1', storage: {} }, connection_info: { host: 'existing-host', port: 5432 } }])
    throw new Error(`Unexpected engine types fixture request: ${request.method()} ${path}`)
  })
  await page.goto('/engines')
  return writes
}

const sidebar = dialog => dialog.locator('.engine-type-sidebar')
const group = (dialog, title) => sidebar(dialog).locator('.el-collapse-item').filter({ hasText: title })

test('registration groups plugin types, searches across collapsed groups and preserves the connection draft', async ({ page }, testInfo) => {
  const writes = await fixture(page)
  await page.getByRole('button', { name: '注册通用引擎', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '新建通用引擎', exact: true })
  const groups = sidebar(dialog).locator('.engine-type-group-title')
  await expect(groups).toHaveText(['关系型数据库', '计算引擎', '文档与搜索', '键值存储', '图数据库', '对象存储', '文件存储', '事件流'])
  await expect(sidebar(dialog).locator('.engine-type-group-count')).toHaveCount(8)
  await group(dialog, '计算引擎').getByRole('button', { name: /计算引擎/ }).click()
  await expect(group(dialog, '关系型数据库').locator('.engine-type-group-count')).toHaveText('4')
  await expect(group(dialog, '关系型数据库').locator('.engine-type-name')).toHaveText(['PostgreSQL', 'Oracle', 'ClickHouse', 'Newly compiled database'])
  await expect(group(dialog, '计算引擎').locator('.engine-type-group-count')).toHaveText('2')
  await expect(group(dialog, '计算引擎').getByRole('button', { name: 'Apache Spark', exact: true })).toBeVisible()
  await expect(group(dialog, '关系型数据库').getByRole('button', { name: 'Apache Spark', exact: true })).toHaveCount(0)
  await expect(group(dialog, '关系型数据库').getByRole('button', { name: 'Oracle', exact: true })).toBeVisible()
  await expect(sidebar(dialog).getByRole('button', { name: 'Oracle Database', exact: true })).toHaveCount(0)
  await expect(sidebar(dialog).getByRole('button', { name: 'ClickHouse', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(sidebar(dialog).getByRole('button', { name: 'MongoDB', exact: true })).not.toBeVisible()
  await group(dialog, '文档与搜索').getByRole('button', { name: /文档与搜索/ }).click()
  await expect(sidebar(dialog).getByRole('button', { name: 'MongoDB', exact: true })).toBeVisible()
  await sidebar(dialog).getByRole('button', { name: 'Newly compiled database', exact: true }).click()
  await expect(dialog.getByRole('spinbutton')).toHaveValue('1234')
  const name = dialog.locator('.el-form-item').filter({ hasText: /^名称/ }).getByRole('textbox')
  await name.fill('Connection draft')
  const host = dialog.locator('.el-form-item').filter({ hasText: /^主机地址/ }).getByRole('textbox')
  await host.fill('draft-host')
  const search = dialog.getByRole('textbox', { name: '搜索引擎名称或类型' })
  await search.fill('  ReDiS  ')
  await expect(groups).toHaveText(['键值存储'])
  await expect(group(dialog, '键值存储').locator('.engine-type-group-count')).toHaveText('1')
  const redis = sidebar(dialog).getByRole('button', { name: 'Redis', exact: true })
  await expect(redis).toBeVisible()
  await expect(host).toHaveValue('draft-host')
  await redis.focus(); await page.keyboard.press('Enter')
  await expect(redis).toHaveAttribute('aria-pressed', 'true')
  await expect(dialog.getByRole('spinbutton')).toHaveValue('6379')
  await expect(name).toHaveValue('Connection draft')
  await search.fill('not-a-supported-engine')
  await expect(sidebar(dialog)).toContainText('没有匹配的引擎类型')
  await expect(name).toHaveValue('Connection draft')
  await search.fill('')
  await expect(groups).toHaveCount(8)
  await expect(redis).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('engine-type-groups.png') })
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await page.getByRole('button', { name: '注册通用引擎', exact: true }).click()
  await expect(search).toHaveValue('')
  await expect(sidebar(dialog).getByRole('button', { name: 'ClickHouse', exact: true })).toHaveAttribute('aria-pressed', 'true')
  expect(writes).toEqual([])
})

test('English groups and search fit a narrow dark dialog', async ({ page }, testInfo) => {
  await fixture(page, 'en')
  await page.setViewportSize({ width: 640, height: 800 })
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await page.getByRole('button', { name: 'Register General Engine', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'New General Engine', exact: true })
  await expect(sidebar(dialog).locator('.engine-type-group-title')).toHaveText([
    'Relational databases', 'Compute engines', 'Documents & search', 'Key-value stores', 'Graph databases', 'Object storage', 'File storage', 'Event streams'
  ])
  await dialog.getByRole('textbox', { name: 'Search engine name or type' }).fill('Amazon')
  await sidebar(dialog).getByRole('button', { name: 'Amazon S3', exact: true }).click()
  await expect(dialog.getByRole('spinbutton')).toHaveValue('443')
  await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeVisible()
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('engine-type-groups-narrow-dark.png') })
})

test('editing displays only the locked current engine type', async ({ page }) => {
  const writes = await fixture(page)
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '编辑引擎', exact: true })
  await expect(sidebar(dialog).locator('.engine-type-group-title')).toHaveText(['关系型数据库'])
  await expect(sidebar(dialog).locator('.engine-type-item')).toHaveCount(1)
  await expect(sidebar(dialog).getByRole('button', { name: 'PostgreSQL', exact: true })).toBeDisabled()
  await expect(dialog.getByRole('textbox', { name: '搜索引擎名称或类型' })).toHaveCount(0)
  expect(writes).toEqual([])
})

test('compute registration uses the plugin group and retains the declared connection form', async ({ page }) => {
  const writes = await fixture(page)
  await page.getByRole('button', { name: '注册通用引擎', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '新建通用引擎', exact: true })
  const search = dialog.getByRole('textbox', { name: '搜索引擎名称或类型' })
  await search.fill('spark')
  await expect(sidebar(dialog).locator('.engine-type-group-title')).toHaveText(['计算引擎'])
  await sidebar(dialog).getByRole('button', { name: 'Apache Spark', exact: true }).click()
  await expect(dialog.getByRole('spinbutton')).toHaveValue('10000')
  await search.fill('vendor_compute')
  await expect(sidebar(dialog).locator('.engine-type-group-title')).toHaveText(['计算引擎'])
  await sidebar(dialog).getByRole('button', { name: 'Another compute engine', exact: true }).click()
  await expect(dialog.getByRole('spinbutton')).toHaveValue('1235')
  expect(writes).toEqual([])
})

test('incomplete registration metadata renders neither a banner nor an unnamed group', async ({ page }) => {
  await fixture(page, 'zh-cn', descriptors.map(({ registration_group, ...rest }) => rest))
  await expect(page.getByRole('button', { name: '注册通用引擎', exact: true })).toBeDisabled()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.locator('.engine-type-group-title')).toHaveCount(0)
})

test('engine icons match product types on the same row while group headings and counts stay visible', async ({ page }, testInfo) => {
  const writes = await fixture(page, 'zh-cn', descriptors.filter(item => !['futuredb', 'vendor_compute'].includes(item.type)))
  await page.getByRole('button', { name: '注册通用引擎', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '新建通用引擎', exact: true })
  await expect(sidebar(dialog).locator('.engine-type-group-title')).toHaveText(['关系型数据库', '计算引擎', '文档与搜索', '键值存储', '图数据库', '对象存储', '文件存储', '事件流'])
  await expect(sidebar(dialog).locator('.engine-type-group-count')).toHaveCount(8)
  const postgres = sidebar(dialog).getByRole('button', { name: 'PostgreSQL', exact: true })
  const clickhouse = sidebar(dialog).getByRole('button', { name: 'ClickHouse', exact: true })
  await expect(postgres.locator('svg path')).toHaveCount(1)
  expect(await postgres.locator('svg path').getAttribute('d')).not.toBe(await clickhouse.locator('svg path').getAttribute('d'))
  const layout = await postgres.evaluate(el => {
    const icon = el.querySelector('.engine-type-icon').getBoundingClientRect()
    const text = el.querySelector('.engine-type-name').getBoundingClientRect()
    return { width: icon.width, beforeText: icon.right <= text.left, centers: Math.abs((icon.top + icon.bottom - text.top - text.bottom) / 2) }
  })
  expect(layout.width).toBe(18)
  expect(layout.beforeText).toBe(true)
  expect(layout.centers).toBeLessThan(2)
  await group(dialog, '计算引擎').getByRole('button', { name: /^计算引擎/ }).click()
  await expect(group(dialog, '计算引擎').getByRole('button', { name: 'Apache Spark', exact: true }).locator('.engine-type-icon')).toHaveAttribute('data-engine-type', 'spark')
  await dialog.getByRole('textbox', { name: '搜索引擎名称或类型' }).fill('oracle')
  await expect(sidebar(dialog).getByRole('button', { name: 'Oracle', exact: true }).locator('.engine-type-icon')).toHaveText('OR')
  await dialog.getByRole('textbox', { name: '搜索引擎名称或类型' }).fill('')
  const bounds = await dialog.evaluate(el => ({
    sidebarBottom: el.querySelector('.engine-type-sidebar').getBoundingClientRect().bottom,
    bodyBottom: el.querySelector('.el-dialog__body').getBoundingClientRect().bottom,
    listBottom: el.querySelector('.engine-type-list').getBoundingClientRect().bottom,
    lastHeadingBottom: [...el.querySelectorAll('.engine-type-group-title')].at(-1).getBoundingClientRect().bottom
  }))
  expect(bounds.sidebarBottom).toBeLessThanOrEqual(bounds.bodyBottom)
  expect(bounds.lastHeadingBottom).toBeLessThanOrEqual(bounds.listBottom)
  const screenshot = testInfo.outputPath('engine-type-icons-grouped.png')
  await page.screenshot({ path: screenshot, animations: 'disabled' })
  await testInfo.attach('engine-type-icons-grouped', { path: screenshot, contentType: 'image/png' })
  expect(writes).toEqual([])
})

test('group counts and popularity order follow the current search results', async ({ page }) => {
  const writes = await fixture(page)
  await page.getByRole('button', { name: '注册通用引擎', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '新建通用引擎', exact: true })
  const objects = group(dialog, '对象存储')
  await objects.getByRole('button', { name: /^对象存储/ }).click()
  await expect(objects.locator('.engine-type-group-count')).toHaveText('2')
  await expect(objects.locator('.engine-type-name')).toHaveText(['Amazon S3', 'MinIO'])
  const documents = group(dialog, '文档与搜索')
  await documents.getByRole('button', { name: /^文档与搜索/ }).click()
  await expect(documents.locator('.engine-type-name')).toHaveText(['MongoDB', 'Elasticsearch'])
  const search = dialog.getByRole('textbox', { name: '搜索引擎名称或类型' })
  await search.fill('post')
  await expect(sidebar(dialog).locator('.engine-type-group-count')).toHaveText(['1'])
  await expect(sidebar(dialog).locator('.engine-type-name')).toHaveText(['PostgreSQL'])
  await search.fill('')
  await expect(group(dialog, '关系型数据库').locator('.engine-type-group-count')).toHaveText('4')
  await expect(group(dialog, '关系型数据库').locator('.engine-type-name')).toHaveText(['PostgreSQL', 'Oracle', 'ClickHouse', 'Newly compiled database'])
  expect(writes).toEqual([])
})

for (const [language, typeName, versionLabel, timeLabel] of [
  ['zh-cn', '瀚高（HighGo）', '实例版本', '版本探测时间'],
  ['en', 'HighGo', 'Server version', 'Version observed at']
]) {
  test(`native product version is a read-only instance fact in ${language}`, async ({ page }) => {
    const version = 'HighGo Database Management System 4.5 on aarch64,build on 20250227'
    const writes = await fixture(page, language, [descriptor('highgo', 'HighGo', 'tabular', 5866)], {
      server_version: version, server_version_checked_at: '2026-10-16T08:00:00Z',
      last_check_at: '2026-10-16T09:00:00Z', connection_status: 'offline'
    })
    await page.goto('/engines/2')
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByText(typeName, { exact: true })).toBeVisible()
    await expect(dialog.getByText(versionLabel, { exact: true })).toBeVisible()
    await expect(dialog.getByText(version, { exact: true })).toBeVisible()
    await expect(dialog.getByText(timeLabel, { exact: true })).toBeVisible()
    // The version timestamp is independent of the later failed connectivity check.
    const versionTime = dialog.locator('tr').filter({ hasText: timeLabel })
    await expect(versionTime).toContainText('16:00:00')
    await expect(dialog.locator('input')).toHaveCount(0)
    expect(writes).toEqual([])
  })
}

test('an engine without a successful version probe does not infer a version from its type', async ({ page }) => {
  const writes = await fixture(page, 'zh-cn', [descriptor('highgo', 'HighGo', 'tabular', 5866)])
  await page.goto('/engines/2')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('瀚高（HighGo）', { exact: true })).toBeVisible()
  await expect(dialog.getByText('实例版本', { exact: true })).toHaveCount(0)
  await expect(dialog.getByText('版本探测时间', { exact: true })).toHaveCount(0)
  expect(writes).toEqual([])
})
