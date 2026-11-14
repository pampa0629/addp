import { constants, openSync, closeSync, fstatSync, fsyncSync, writeFileSync, readFileSync, renameSync, unlinkSync } from 'node:fs'
import { dirname, basename, join, isAbsolute } from 'node:path'
import { createHash, randomBytes } from 'node:crypto'

export const navigationSQL = 'SELECT 42 AS fixture_value FROM pg_catalog.pg_sleep(20)'

const uuid = value => typeof value === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value)
const positive = value => Number.isSafeInteger(value) && value > 0
const keys = (value, expected) => value && typeof value === 'object' && !Array.isArray(value) && Object.keys(value).sort().join(',') === expected.split(',').sort().join(',')
const require = (value, code) => { if (!value) throw new Error(code) }
const digest = value => createHash('sha256').update(JSON.stringify(value)).digest('hex')
const sqlDigest = createHash('sha256').update(navigationSQL).digest('hex')
const prefix = input => [input.run_id, input.tenant_id, input.user_id, input.engine_id]
const definitionDigest = (input, slot) => digest([...prefix(input), 'definition', slot, sqlDigest])
const executionDigest = (input, slot, definition, id) => digest([...prefix(input), 'execution', slot, definition, id])
export const executionDefinitions = [0, 1, 2, 3, 4, 5, 0]

const failureCodes = new Set(["timeout", "assertion_failed", "filesystem_error", "operation_failed", "navigation_admission_identity_invalid", "navigation_budget_exhausted", "navigation_budget_insufficient", "navigation_command_count_invalid", "navigation_comparison_limit", "navigation_control_request_failed", "navigation_definition_binding_invalid", "navigation_definition_limit", "navigation_execution_binding_invalid", "navigation_execution_limit", "navigation_execution_time_invalid", "navigation_filtered_context_invalid", "navigation_filtered_list_failed", "navigation_input_invalid", "navigation_input_tab_not_persisted", "navigation_latest_identity_invalid", "navigation_ledger_already_started", "navigation_ledger_definition_invalid", "navigation_ledger_duplicate_identity", "navigation_ledger_execution_invalid", "navigation_ledger_file_invalid", "navigation_ledger_invalid", "navigation_ledger_path_invalid", "navigation_ledger_too_large", "navigation_monitor_identity_invalid", "navigation_monitor_read_failed", "navigation_old_terminal_changed", "navigation_owner_read_failed", "navigation_owner_snapshot_invalid", "navigation_pending_lifecycle_invalid", "navigation_poll_limit", "navigation_private_execution_field", "navigation_state_window_lost", "navigation_tabs_not_above_content"])
export function navigationFailure(error) {
  return failureCodes.has(error?.message) ? error.message
    : error?.name === 'TimeoutError' ? 'timeout' : error?.name === 'AssertionError' ? 'assertion_failed'
      : ['EACCES', 'EIO', 'ENOSPC', 'ENOENT'].includes(error?.code) ? 'filesystem_error' : 'operation_failed'
}

export function navigationInput(value) {
  require(keys(value, 'run_id,tenant_id,user_id,engine_id,query_concurrency,query_per_engine_concurrency,backend_instances') &&
    typeof value.run_id === 'string' && /^[A-Za-z0-9_-]{1,100}$/.test(value.run_id) && typeof value.tenant_id === 'string' && /^[1-9][0-9]*$/.test(value.tenant_id) && typeof value.user_id === 'string' && /^[1-9][0-9]*$/.test(value.user_id) &&
    positive(value.engine_id) && value.query_concurrency === 20 && value.query_per_engine_concurrency === 5 && value.backend_instances === 1,
  'navigation_input_invalid')
  return value
}

export function validateNavigationLedger(value, input) {
  require(keys(value, 'schema,run_id,tenant_id,user_id,engine_id,definitions,executions,reads') && value.schema === 'addp.execution-navigation-ownership/v1' &&
    ['run_id', 'tenant_id', 'user_id', 'engine_id'].every(key => value[key] === input[key]) &&
    Number.isInteger(value.reads) && value.reads >= 0 && value.reads <= 45 && Array.isArray(value.definitions) && value.definitions.length <= 6 && Array.isArray(value.executions) && value.executions.length <= 7,
  'navigation_ledger_invalid')
  for (const [index, row] of value.definitions.entries()) require(keys(row, 'slot,digest,id') && row.slot === index && row.digest === definitionDigest(input, index) &&
    (row.id === null || positive(row.id)), 'navigation_ledger_definition_invalid')
  for (const [index, row] of value.executions.entries()) {
    const id = value.definitions[row.definition]?.id
    require(keys(row, 'slot,definition,digest,id') && row.slot === index && row.definition === executionDefinitions[index] && positive(id) &&
      row.digest === executionDigest(input, index, row.definition, id) && (row.id === null || uuid(row.id)), 'navigation_ledger_execution_invalid')
  }
  for (const rows of [value.definitions, value.executions]) {
    const ids = rows.map(row => row.id).filter(id => id !== null)
    require(new Set(ids).size === ids.length, 'navigation_ledger_duplicate_identity')
  }
  return value
}

export function openNavigationLedger(path, input) {
  require(isAbsolute(path || ''), 'navigation_ledger_path_invalid')
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW)
  let value
  try {
    const info = fstatSync(fd)
    require(info.isFile() && info.nlink === 1 && (info.mode & 0o077) === 0 && info.size <= 4096, 'navigation_ledger_file_invalid')
    value = validateNavigationLedger(JSON.parse(readFileSync(fd, 'utf8')), input)
    require(value.definitions.length === 0 && value.executions.length === 0 && value.reads === 0, 'navigation_ledger_already_started')
  } finally { closeSync(fd) }
  const save = () => {
    const encoded = JSON.stringify(value)
    require(Buffer.byteLength(encoded) <= 4096, 'navigation_ledger_too_large')
    const temporary = join(dirname(path), '.' + basename(path) + '-' + randomBytes(8).toString('hex'))
    let output
    try {
      output = openSync(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o600)
      writeFileSync(output, encoded); fsyncSync(output); closeSync(output); output = undefined
      renameSync(temporary, path)
      const directory = openSync(dirname(path), constants.O_RDONLY)
      try { fsyncSync(directory) } finally { closeSync(directory) }
    } finally {
      if (output !== undefined) closeSync(output)
      try { unlinkSync(temporary) } catch (error) { if (error.code !== 'ENOENT') throw error }
    }
  }
  return { value, save,
    definition(slot) {
      require(slot === value.definitions.length && slot < 6, 'navigation_definition_limit')
      const row = { slot, digest: definitionDigest(input, slot), id: null }
      value.definitions.push(row); save(); return row
    },
    execution(definition) {
      const slot = value.executions.length
      require(slot < 7 && definition === executionDefinitions[slot] && positive(value.definitions[definition]?.id), 'navigation_execution_limit')
      const row = { slot, definition, digest: executionDigest(input, slot, definition, value.definitions[definition].id), id: null }
      value.executions.push(row); save(); return row
    }
  }
}

export function navigationExecution(item, input, task, id) {
  const privateKeys = new Set(['observability', 'trace_id', 'span_id', 'lease_token', 'authorization_ref', 'access_token', 'password'])
  const safe = value => {
    if (value && typeof value === 'object') for (const [key, child] of Object.entries(value)) {
      require(!privateKeys.has(key), 'navigation_private_execution_field'); safe(child)
    }
  }
  safe(item)
  require(item?.execution_id === id && item.module === 'develop' && item.task_type === 'query' && item.source_task_id === String(task) &&
    String(item.triggered_by) === input.user_id && ['pending', 'running', 'success', 'failed', 'timeout', 'cancelled'].includes(item.status),
  'navigation_execution_binding_invalid')
  require(Number.isInteger(item.attempt) && (item.status !== 'pending' || item.attempt === 0 && item.started_at == null && item.completed_at == null) &&
    (item.status !== 'running' || item.attempt === 1 && typeof item.started_at === 'string' && item.completed_at == null), 'navigation_execution_time_invalid')
  return item
}

// All mutations are confined to six definitions and seven admissions. The UI callback only navigates.
export async function runExecutionNavigation({ input, ledger, connect, navigate, remaining, stage, receipt,
  now = () => performance.now(), wait = ms => new Promise(resolve => setTimeout(resolve, ms)) }) {
  require(remaining() >= 130000, 'navigation_budget_insufficient')
  const deadline = now() + 120000
  let api, polls = 0, comparisons = 0, lastPoll = -Infinity, activeDeadline = Infinity
  receipt.states = {}; receipt.polls = 0; receipt.comparisons = 0
  const budget = () => { const left = Math.min(deadline - now(), activeDeadline - now(), remaining() - 10000); require(left > 0, 'navigation_budget_exhausted'); return Math.min(5000, left) }
  const request = async (method, path, data) => {
    const timeout = budget()
    try { return await api(method, path, data, timeout) } catch { throw new Error('navigation_control_request_failed') }
  }
  const compare = async path => { require(++comparisons <= 12, 'navigation_comparison_limit'); receipt.comparisons = comparisons; return request('GET', path) }
  const read = async (definition, id) => {
    require(ledger.value.reads < 45, 'navigation_poll_limit'); polls++; receipt.polls = polls
    ledger.value.reads++; ledger.save()
    const pause = 2000 - (now() - lastPoll)
    if (pause > 0) { budget(); await wait(pause) }
    budget(); lastPoll = now()
    return navigationExecution(await request('GET', '/api/v1/develop/executions/' + id), input, definition, id)
  }
  const until = async (definition, id, state) => {
    while (true) {
      const item = await read(definition, id)
      if (item.status === state) return item
      require(item.status === 'pending' || state === 'success' && item.status === 'running', 'navigation_state_window_lost')
      budget()
    }
  }
  const reconnect = async () => {
    if (api) await api.dispose()
    require(++comparisons <= 12, 'navigation_comparison_limit'); receipt.comparisons = comparisons
    api = await connect(budget()) // The existing helper revalidates the exact current browser identity.
  }
  const create = async slot => {
    const intent = ledger.definition(slot)
    const task = await request('POST', '/api/v1/develop/task-definitions', {
      name: input.run_id + '-navigation-' + slot, dev_type: 'query', content: { query: navigationSQL, query_type: 'sql' },
      execution_config: { engine_id: input.engine_id }, timeout: 30, editor_layout: {}
    })
    require(positive(task.id) && String(task.tenant_id) === input.tenant_id && String(task.created_by) === input.user_id &&
      task.name === input.run_id + '-navigation-' + slot && task.dev_type === 'query' && task.execution_config?.engine_id === input.engine_id,
    'navigation_definition_binding_invalid')
    intent.id = task.id; validateNavigationLedger(ledger.value, input); ledger.save(); return task.id
  }
  const submit = async definition => {
    const intent = ledger.execution(definition)
    const value = await request('POST', '/api/v1/develop/task-definitions/' + ledger.value.definitions[definition].id + '/execute', { parameters: {} })
    require(uuid(value.execution_id), 'navigation_admission_identity_invalid')
    intent.id = value.execution_id; validateNavigationLedger(ledger.value, input); ledger.save(); return value.execution_id
  }
  try {
    stage('navigation-prepare'); await reconnect()
    const task = await create(0), old = await submit(0)
    receipt.task_id = task; receipt.previous_execution_id = old
    const previous = await until(task, old, 'success')
    const blockers = []
    for (let slot = 1; slot <= 5; slot++) {
      const definition = await create(slot), id = await submit(slot)
      blockers.push({ definition, id })
    }
    for (const blocker of blockers) await until(blocker.definition, blocker.id, 'running')
    const id = await submit(0)
    receipt.execution_id = id
    for (const state of ['pending', 'running', 'success']) {
      stage('navigation-' + state)
      const item = state === 'pending' ? await read(task, id) : await until(task, id, state)
      require(item.status === state, 'navigation_state_window_lost')
      const definition = await compare('/api/v1/develop/task-definitions/' + task)
      require(definition.last_execution_id === id && definition.last_execution_status === state, 'navigation_latest_identity_invalid')
      await api.dispose(); api = null
      const stateDeadline = Math.min(deadline, now() + (state === 'success' ? 30000 : 15000))
      activeDeadline = stateDeadline
      await navigate({ task, id, old, state, engine: input.engine_id, deadline: stateDeadline })
      require(now() <= stateDeadline, 'navigation_state_window_lost')
      await reconnect()
      const after = navigationExecution(await compare('/api/v1/develop/executions/' + id), input, task, id)
      require(after.status === state, 'navigation_state_window_lost')
      require(now() <= stateDeadline, 'navigation_state_window_lost')
      receipt.states[state] = true; activeDeadline = Infinity
    }
    for (const blocker of blockers) await until(blocker.definition, blocker.id, 'success')
    const original = navigationExecution(await compare('/api/v1/develop/executions/' + old), input, task, old)
    require(original.status === 'success' && ['created_at', 'started_at', 'completed_at', 'attempt'].every(key => original[key] === previous[key]) &&
      digest(original.metadata?.result ?? null) === digest(previous.metadata?.result ?? null) &&
      digest(original.execution_config?.content ?? null) === digest(previous.execution_config?.content ?? null), 'navigation_old_terminal_changed')
    require(ledger.value.definitions.length === 6 && ledger.value.executions.length === 7, 'navigation_command_count_invalid')
    receipt.result = 'passed'
  } finally { if (api) await api.dispose() }
}

export async function assertExecutionNavigationPage({ page, expect, task, id, old, state, engine, deadline, now = () => performance.now() }) {
  const options = () => {
    const timeout = Math.min(5000, deadline - now())
    require(timeout > 0, 'navigation_state_window_lost'); return { timeout }
  }
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  const tree = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/monitor/executions/by-execution-id/' + id + '/tree', options())
  tree.catch(() => {})
  await page.goto('/develop/tasks', options())
  const row = frame.locator('.el-table__row').filter({ has: frame.getByRole('cell', { name: String(task), exact: true }) })
  await expect(row).toHaveCount(1, options())
  await row.getByRole('button', { name: '最近执行', exact: true }).click(options())
  await expect(page).toHaveURL('/monitor/executions?execution_id=' + id, options())
  const response = await tree
  require(response.status() === 200, 'navigation_monitor_read_failed')
  const snapshot = (await response.json()).execution
  require(snapshot?.execution_id === id && id !== old && snapshot.status === state && snapshot.source_task_id === String(task), 'navigation_monitor_identity_invalid')
  if (state === 'pending') require(snapshot.started_at == null && snapshot.attempt === 0 && snapshot.lease_state === 'none' &&
    snapshot.lease_owner == null && snapshot.lease_expires_at == null, 'navigation_pending_lifecycle_invalid')
  await expect(frame.getByRole('dialog')).toBeVisible(options())
  const monitorURL = '/monitor/executions?module=develop&task_type=query&source_task_id=' + task + '&page=1&page_size=20&execution_id=' + id
  const listing = page.waitForResponse(response => {
    const url = new URL(response.url())
    return url.pathname === '/api/v1/monitor/executions' && url.searchParams.get('source_task_id') === String(task)
  }, options())
  listing.catch(() => {})
  await page.goto(monitorURL, options())
  const listed = await listing
  require(listed.status() === 200, 'navigation_filtered_list_failed')
  const payload = await listed.json()
  require(payload.page === 1 && payload.page_size === 20 && payload.total === 2 && payload.executions?.length === 2 &&
    payload.executions.every(item => item.module === 'develop' && item.task_type === 'query' && item.source_task_id === String(task)) &&
    payload.executions.some(item => item.execution_id === id) && payload.executions.some(item => item.execution_id === old), 'navigation_filtered_context_invalid')
  const owner = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/develop/executions/' + id, options())
  owner.catch(() => {})
  await frame.getByRole('button', { name: '查看执行结果', exact: true }).click(options())
  const ownerResponse = await owner
  require(ownerResponse.status() === 200, 'navigation_owner_read_failed')
  const professional = await ownerResponse.json()
  require(professional.execution_id === id && professional.status === state && professional.source_task_id === String(task) &&
    professional.execution_config?.content?.query === navigationSQL && professional.execution_config.engine_id === engine &&
    professional.execution_config.timeout === 30, 'navigation_owner_snapshot_invalid')
  await expect(page).toHaveURL(url => url.pathname === '/develop/executions/' + id && url.searchParams.get('return_to') === monitorURL, options())
  await expect(frame.getByRole('heading', { name: '执行结果', exact: true })).toBeVisible(options())
  await expect(frame.locator('.execution-result-identity')).toContainText(id, options())
  if (state === 'success') {
    await expect(frame.locator('.query-result .el-table__body .el-table__row')).toHaveCount(1, options())
    await expect(frame.locator('.query-result .el-table__body .cell')).toHaveText(['42'], options())
  }
  else await expect(frame.locator('.query-result')).toHaveCount(0, options())
  const tabs = frame.locator('.content-area > .el-tabs')
  for (const name of ['执行结果', '执行日志', '输入参数']) {
    await frame.getByRole('tab', { name, exact: true }).click(options())
    if (name === '输入参数') await expect(frame.locator('#pane-inputs .json-result')).toHaveText(JSON.stringify(professional.execution_config.inputs || {}, null, 2), options())
    const header = await tabs.locator(':scope > .el-tabs__header').boundingBox()
    const content = await tabs.locator(':scope > .el-tabs__content').boundingBox()
    require(header && content && header.y + header.height <= content.y + 1, 'navigation_tabs_not_above_content')
  }
  const resultURL = page.url()
  require(new URL(resultURL).searchParams.get('tab') === 'inputs', 'navigation_input_tab_not_persisted')
  await page.reload(options())
  await expect(frame.getByRole('tab', { name: '输入参数', exact: true })).toHaveAttribute('aria-selected', 'true', options())
  await expect(page).toHaveURL(resultURL, options())
  await frame.getByRole('button', { name: '返回本次执行', exact: true }).click(options())
  await expect(page).toHaveURL(monitorURL, options())
  await expect(frame.getByRole('dialog')).toBeVisible(options())
  await page.goBack(options()); await expect(page).toHaveURL(resultURL, options())
  await expect(frame.locator('.execution-result-identity')).toContainText(id, options())
  await page.goForward(options()); await expect(page).toHaveURL(monitorURL, options())
  await expect(frame.getByRole('dialog')).toBeVisible(options())
}
