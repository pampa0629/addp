import { expect, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromNavigation, browserAPIFromResponse, json, login } from './transfer-browser-support.js'

// This is invoked only by the HDFS owner runner, never against a personal cluster.
test('同一个 Runtime 经真实查询和工作流页面路由两套集群', async ({ page, context }) => {
  test.setTimeout(420_000)
  const expected = JSON.parse(process.env.ADDP_ONLINE_HDFS_EXPECTATIONS)
  const clusters = expected.multicluster.clusters
  await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME,
    process.env.ADDP_ONLINE_TEST_USER_PASSWORD, '/develop/workflow?action=create')
  const expectedIdentity = { principalID: expected.principal_id, tenantID: String(expected.tenant_id),
    permissions: new Set(expected.multicluster.permissions) }
  const apiContexts = new Set()
  let api
  const peer = await context.newPage()
  let managerNoSpark = false
  const observations = []
  const savedTasks = {}
  const screenshot = (target, name) => target.screenshot({
    path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `spark-dual-${name}.png`)
  })
  const frame = target => target.frameLocator('iframe[data-testid="module-iframe"]')
  const pathIs = (response, path, method) => new URL(response.url()).pathname === path && response.request().method() === method
  const readExecution = async id => json(await api.get(`/api/v1/develop/executions/${id}`), 'page execution')
  const masters = async () => Object.fromEntries(await Promise.all(Object.entries(clusters).map(async ([key, cluster]) => {
    const response = await fetch(`${cluster.master_url}/json/`, { signal: AbortSignal.timeout(5000) })
    expect(response.ok).toBe(true)
    const state = await response.json()
    expect(state.status).toBe('ALIVE')
    expect(state.aliveworkers).toBe(1)
    return [key, { active: state.activeapps || [], completed: state.completedapps || [] }]
  })))
  const ids = state => [...state.active, ...state.completed].map(app => app.id).sort()
  async function select(target, selector, name) {
    await selector.click()
    let option = frame(target).getByRole('option').filter({ hasText: name })
    // The A name is a prefix of B. Exclude other fixture identities without
    // depending on whitespace between Element Plus labels and status badges.
    for (const other of [...Object.values(clusters).map(cluster => cluster.name), expected.multicluster.runtime_name]) {
      if (other !== name && other.startsWith(name)) option = option.filter({ hasNotText: other })
    }
    await expect(option).toHaveCount(1)
    await option.click()
    await expect(selector).toContainText(name)
  }
  async function bindResponseAPI(response) {
    const current = await browserAPIFromResponse(response, process.env.GATEWAY_URL, expectedIdentity)
    apiContexts.add(current)
    api = current
  }
  async function assertIdentity(target, navigate) {
    const current = await browserAPIFromNavigation(target, process.env.GATEWAY_URL, expectedIdentity,
      navigate, response => pathIs(response, '/api/v1/system/auth/context', 'GET') && response.ok())
    apiContexts.add(current)
    api = current
  }
  async function queryPage(target, cluster) {
    await assertIdentity(target, () => target.goto('/develop/sql?action=create'))
    await select(target, frame(target).locator('.workbench-toolbar .engine-select'), clusters[cluster].name)
  }
  async function editQuery(target, sql) {
    const editor = frame(target).locator('.monaco-editor')
    await editor.click({ position: { x: 70, y: 20 } })
    await target.keyboard.press('ControlOrMeta+A')
    await target.keyboard.insertText(sql)
  }
  async function startQuery(target, cluster, failing = false) {
    const sql = failing ? expected.multicluster.failure_query : expected.multicluster.query
    await editQuery(target, sql)
    const preflight = target.waitForResponse(response => pathIs(response, '/api/v1/develop/query-preflight', 'POST'))
    const submitted = target.waitForResponse(response => pathIs(response, '/api/v1/develop/executions', 'POST'))
    await frame(target).locator('.toolbar-actions').getByRole('button', { name: /^(执行|Execute)$/ }).click()
    const response = await preflight
    expect(response.request().postDataJSON()).toMatchObject({ engine_id: clusters[cluster].id, query: sql, query_type: 'sql' })
    const preflightValue = await json(response, 'query preflight')
    expect(preflightValue.allowed).toBe(true)
    expect(preflightValue.effect).toBe('read')
    expect(preflightValue.requires_confirmation).toBe(false)
    const accepted = await submitted
    expect(accepted.request().postDataJSON()).toMatchObject({ dev_type: 'query', execution_config: { engine_id: clusters[cluster].id }, content: { query: sql } })
    const started = await json(accepted, 'page query submission')
    await bindResponseAPI(accepted)
    await expect.poll(async () => (await readExecution(started.execution_id)).status,
      { timeout: 120_000 }).toBe(failing ? 'failed' : 'success')
    const execution = await readExecution(started.execution_id)
    if (failing) {
      expect(execution.error_details.diagnostic).toEqual({
        schema_version: 'addp.execution-failure/v3', stage: 'execution', reason: 'column_not_found'
      })
      await expect(frame(target).locator('.query-result .execution-failure')).toContainText(/字段不存在|column.*(does not exist|could not be resolved)/i)
    } else {
      expect(execution.metadata.result.summary.preview_rows).toEqual(expected.multicluster.query_rows)
      await expect(frame(target).locator('.query-result .el-table__body-wrapper tr')).toHaveCount(2)
      for (const region of ['east', 'west']) {
        await expect(frame(target).locator('.query-result').getByRole('cell', { name: region, exact: true })).toBeVisible()
      }
    }
    await screenshot(target, `query-${cluster}-${started.execution_id}`)
    return { cluster, status: failing ? 'failed' : 'success', execution_id: started.execution_id, query: sql,
      ...(failing ? { diagnostic: execution.error_details.diagnostic } : {}) }
  }

  async function workflowPage(target, key, id) {
    await assertIdentity(target, () => target.goto(id ? `/develop/workflow?action=edit&id=${id}` : '/develop/workflow'))
    const selectors = frame(target).locator('.editor-header .engine-select')
    if (!id) {
      await select(target, selectors.nth(0), expected.multicluster.runtime_name)
      await select(target, selectors.nth(1), clusters[key].name)
    }
    await expect(selectors.nth(0)).toContainText(expected.multicluster.runtime_name)
    await expect(selectors.nth(1)).toContainText(clusters[key].name)
  }
  async function editWorkflow(target, sql, create) {
    const editor = frame(target)
    if (create) {
      const category = editor.locator('.el-collapse-item__header').filter({ hasText: /SQL/ })
      if (await category.getAttribute('aria-expanded') !== 'true') await category.click()
      await editor.locator('.operator-item').filter({ hasText: /SQL查询|SQL Query/i }).click()
    }
    const canvas = editor.getByRole('region', { name: /工作流 DAG 画布|Workflow DAG canvas/ })
    await canvas.press('ArrowRight')
    await canvas.press('Enter')
    await editor.locator('[data-param-name="query"]').getByRole('textbox').fill(sql)

  }
  async function saveWorkflow(target, key, create) {
    const editor = frame(target)
    const method = create ? 'POST' : 'PUT'
    const path = create ? '/api/v1/develop/task-definitions' : `/api/v1/develop/task-definitions/${savedTasks[key]}`
    const received = target.waitForResponse(response => pathIs(response, path, method))
    await editor.locator('.primary-actions').getByRole('button', { name: /^(保存|Save)$/ }).click()
    if (create) {
      const dialog = editor.getByRole('dialog', { name: /^(保存工作流|Save Workflow)$/i })
      await dialog.getByRole('textbox').first().fill(`dual-${process.env.ADDP_ONLINE_TEST_RUN_ID}-${key}`)
      await dialog.getByRole('button', { name: /^(保存|Save)$/ }).click()
    }
    const response = await received
    const payload = response.request().postDataJSON()
    expect(payload.execution_config).toEqual({ type: 'workflow', engine_id: expected.runtime_id, engine_specific: { spark_cluster_id: clusters[key].id } })
    expect(payload.content.workflow_definition.tasks).toHaveLength(1)
    const task = await json(response, 'workflow page save')
    const id = create ? task.id : savedTasks[key]
    expect(Number.isInteger(id) && id > 0).toBe(true)
    savedTasks[key] = id
    await bindResponseAPI(response)
    const stored = await json(await api.get(`/api/v1/develop/task-definitions/${id}`), 'stored workflow')
    expect(stored.execution_config).toEqual(payload.execution_config)
    expect(stored.content.workflow_definition).toEqual(payload.content.workflow_definition)
    await screenshot(target, `workflow-save-${key}`)
    return stored
  }
  async function startWorkflow(target, key, failing = false) {
    const id = savedTasks[key]
    const editor = frame(target)
    await editor.locator('.primary-actions').getByRole('button', { name: /^(执行|Execute)$/ }).click()
    const dialog = editor.getByRole('dialog', { name: /^(执行工作流|Execute Workflow)$/i })
    const received = target.waitForResponse(response => pathIs(response, `/api/v1/develop/task-definitions/${id}/execute`, 'POST'))
    await dialog.getByRole('button', { name: /^(执行|Execute)$/ }).click()
    const response = await received
    expect(response.request().postDataJSON()).toEqual({ parameters: {} })
    const started = await json(response, 'workflow page execute')
    await bindResponseAPI(response)
    await expect.poll(async () => (await readExecution(started.execution_id)).status,
      { timeout: 120_000 }).toBe(failing ? 'failed' : 'success')
    const execution = await readExecution(started.execution_id)
    expect(execution.source_task_id).toBe(String(id))
    expect(execution.execution_config.engine_id).toBe(expected.runtime_id)
    expect(execution.execution_config.engine_specific.spark_cluster_id).toBe(clusters[key].id)
    return { cluster: key, status: failing ? 'failed' : 'success', execution_id: started.execution_id, task_id: id }
  }
  async function inspectWorkflowResult(target, operation) {
    await assertIdentity(target, () => target.goto(`/develop/executions/${operation.execution_id}`))
    const execution = await readExecution(operation.execution_id)
    expect(execution.status).toBe(operation.status)
    const detail = frame(target).locator('.execution-detail-page')
    await expect(detail).toContainText(operation.execution_id)
    if (operation.status === 'success') {
      const result = execution.metadata.result.final_result
      expect(result.preview_rows).toEqual([{ marker: 'dual-workflow', rows: 4 }])
      await expect.poll(async () => JSON.parse(await detail.locator('.workflow-final-result-json').innerText())).toEqual(result)
    } else {
      expect(execution.error_details.diagnostic).toEqual({
        schema_version: 'addp.execution-failure/v3', stage: 'execution', reason: 'table_not_found'
      })
      await expect(detail.locator('.execution-failure')).toContainText(/表或视图不存在|table or view does not exist/i)
      operation.diagnostic = execution.error_details.diagnostic
    }
    await screenshot(target, `workflow-${operation.cluster}-${operation.execution_id}`)
  }
  async function batch(kind, phase, actions, concurrent = false) {
    const before = await masters()
    expect(before.a.active).toHaveLength(0)
    expect(before.b.active).toHaveLength(0)
    let done = false
    let overlap = false
    const watch = (async () => {
      while (!done) {
        const state = await masters()
        if (state.a.active.length && state.b.active.length) overlap = true
        await new Promise(resolve => setTimeout(resolve, 100))
      }
    })()
    watch.catch(() => {})
    let operations
    try { operations = await Promise.all(actions.map(action => action())) } finally { done = true; await watch }
    if (concurrent) expect(overlap, `${kind} ${phase} must run on both Masters concurrently`).toBe(true)
    await expect.poll(async () => {
      const state = await masters()
      return state.a.active.length + state.b.active.length
    }, { timeout: 30_000 }).toBe(0)
    const after = await masters()
    const applications = {}
    for (const key of ['a', 'b']) {
      const added = ids(after[key]).filter(id => !ids(before[key]).includes(id))
      const selected = operations.filter(operation => operation.cluster === key)
      expect(added).toHaveLength(selected.length)
      applications[key] = after[key].completed.filter(app => added.includes(app.id))
        .map(({ id, name, state }) => ({ id, name, state }))
      expect(applications[key]).toHaveLength(selected.length)
      for (const app of applications[key]) {
        expect(app.state).toBe('FINISHED')
        expect(app.name).toBe(kind === 'workflow' ? `ADDP-Workflow-Engine-${clusters[key].id}` : 'ADDP-federated-query')
      }
    }
    // Finish both concurrent API reads before any full-page detail navigation
    // can rotate the shared browser session. Each detail then rebinds its identity.
    if (kind === 'workflow') {
      for (const operation of operations) await inspectWorkflowResult(page, operation)
    }
    observations.push({ kind, phase, operations, before: { a: ids(before.a), b: ids(before.b) }, applications, overlap })
    // Keep partial, non-secret receipts even if a later assertion fails.
    writeFileSync(process.env.ADDP_ONLINE_SPARK_DUAL_BROWSER_REPORT, JSON.stringify({
      run_id: process.env.ADDP_ONLINE_TEST_RUN_ID, principal_id: expected.principal_id,
      tenant_id: expected.tenant_id, runtime_id: expected.runtime_id, manager_no_spark: managerNoSpark, tasks: savedTasks, observations
    }))
  }
  try {
    await assertIdentity(page, () => page.goto('/manager/data-explorer'))
    await expect(frame(page).getByText('Hosted HDFS', { exact: true })).toBeVisible()
    for (const cluster of Object.values(clusters)) await expect(frame(page).getByText(cluster.name, { exact: true })).toHaveCount(0)
    await expect(frame(page).getByText(expected.multicluster.runtime_name, { exact: true })).toHaveCount(0)
    await screenshot(page, 'manager-no-spark')
    managerNoSpark = true
    for (const [key, target] of [['a', page], ['b', peer]]) {
      await workflowPage(target, key)
      await editWorkflow(target, expected.multicluster.workflow_query, true)
      await saveWorkflow(target, key, true)
    }
    for (const key of ['a', 'b', 'a']) {
      await workflowPage(page, key, savedTasks[key])
      await batch('workflow', `sequence-${observations.length}`, [() => startWorkflow(page, key)])
    }
    await workflowPage(page, 'a', savedTasks.a)
    await workflowPage(peer, 'b', savedTasks.b)
    await batch('workflow', 'concurrent', [() => startWorkflow(page, 'a'), () => startWorkflow(peer, 'b')], true)
    await workflowPage(page, 'a', savedTasks.a)
    await editWorkflow(page, 'SELECT * FROM missing_private_invocation_view', false)
    await saveWorkflow(page, 'a', false)
    await workflowPage(peer, 'b', savedTasks.b)
    await batch('workflow', 'failure-isolation', [() => startWorkflow(page, 'a', true), () => startWorkflow(peer, 'b')], true)
    await workflowPage(page, 'a', savedTasks.a)
    await editWorkflow(page, expected.multicluster.workflow_query, false)
    await saveWorkflow(page, 'a', false)
    await batch('workflow', 'recovery', [() => startWorkflow(page, 'a')])
    for (const key of ['a', 'b', 'a']) {
      await queryPage(page, key)
      await batch('query', `sequence-${observations.length}`, [() => startQuery(page, key)])
    }
    await queryPage(page, 'a')
    await queryPage(peer, 'b')
    await batch('query', 'concurrent', [() => startQuery(page, 'a'), () => startQuery(peer, 'b')], true)
    await batch('query', 'failure-isolation', [() => startQuery(page, 'a', true), () => startQuery(peer, 'b')], true)
    await batch('query', 'recovery', [() => startQuery(page, 'a')])
  } finally {
    await Promise.all([peer.close(), ...[...apiContexts].map(current => current.dispose())])
  }
})
