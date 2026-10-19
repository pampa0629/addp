import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, identity, json, login, selectSearchResult } from './transfer-browser-support.js'

function required(name) {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

test('Read-only SQL SELECT-only source retains precise values and field facts in Console', async ({ page }) => {
  const expected = JSON.parse(required('ADDP_ONLINE_READONLY_SQL_EXPECTATIONS'))
  const api = await request.newContext({ baseURL: required('GATEWAY_URL'), extraHTTPHeaders: {
    Authorization: `Bearer ${required('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN')}`
  } })
  const current = identity(await json(await api.get('/api/v1/system/auth/context'), 'consumer identity'))
  await api.dispose()
  expect(current.principalType).toBe('user')
  expect(current.tenantID).toBe(String(expected.tenant_id))
  expect(current.principalID).toBe(expected.principal_id)
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const token = await login(page, required('ADDP_ONLINE_TEST_USER_USERNAME'), required('ADDP_ONLINE_TEST_USER_PASSWORD'),
    `/manager/data-explorer?locator=${encodeURIComponent(expected.locator)}`)
  const browserAPI = await request.newContext({ baseURL: required('GATEWAY_URL'), extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
  try {
    const browserIdentity = identity(await json(await browserAPI.get('/api/v1/system/auth/context'), 'browser identity'))
    expect(browserIdentity.principalID).toBe(current.principalID)
    expect(browserIdentity.tenantID).toBe(current.tenantID)
    expect([...browserIdentity.permissions].sort()).toEqual([...current.permissions].sort())
    const manager = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(manager.getByRole('treeitem', { name: 'source', exact: true })).toBeVisible()
    await manager.getByRole('treeitem', { name: 'source', exact: true }).click()
    await manager.getByRole('tab', { name: /^(预览|Preview)$/ }).click()
    await expect(manager.getByText(`${expected.namespace}/source - 数据预览`, { exact: true })).toBeVisible()
    await expect(manager.getByText('共 2 行', { exact: true })).toBeVisible()
    for (const value of ['9007199254740993', '12345678901234567890.123456789012345678', expected.sample_name]) {
      await expect(manager.getByRole('cell', { name: value, exact: true })).toBeVisible()
    }
    await expect(manager.getByText('数据预览失败', { exact: true })).toHaveCount(0)
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), `${expected.engine_type}-preview-console.png`) })
    await manager.getByRole('tab', { name: /^(属性|Attributes)$/ }).click()
    await manager.getByRole('button', { name: /^(查看 JSON|View JSON)$/ }).click()
    const dialog = manager.getByRole('dialog', { name: '完整属性 JSON', exact: true })
    await expect(dialog).toBeVisible()
    const attrs = JSON.parse(await dialog.locator('pre.attribute-json').innerText())
    expect(attrs.type_info.table.primary_key).toEqual(['name', 'id'])
    expect(attrs.type_info.table.fields.find(f => f.name === 'amount')).toMatchObject({ type: 'decimal', precision: 38, scale: 18 })
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), `${expected.engine_type}-facts-console.png`) })
    const engineResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/develop/engines')
    await page.goto('/develop/sql?action=create')
    const engines = await engineResponse
    const workbenchAPI = await browserAPIFromResponse(engines, required('GATEWAY_URL'), {
      principalID: current.principalID, tenantID: current.tenantID, permissions: current.permissions
    })
    await workbenchAPI.dispose()
    expect((await json(engines, 'Develop engines')).some(engine => engine.id === expected.engine_id && engine.engine_type === expected.engine_type)).toBe(true)
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(frame.locator('.engine-select')).toContainText(expected.engine_name)
    await expect(frame.locator('.toolbar-primary .el-tag')).toHaveText('SQL')
    await expect(frame.locator('.toolbar-actions').getByRole('button', { name: /保存查询|Save Query/ })).toHaveCount(0)
    await selectSearchResult(frame.locator('.catalog-panel .resource-tree-picker'), 'source', 'source')
    const queries = []
    const textValue = "中文' OR 1=1 --"
    async function execute(query, parameterized) {
      await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
      await page.evaluate(value => navigator.clipboard.writeText(value), query)
      await frame.locator('.monaco-editor').click({ position: { x: 70, y: 20 } })
      await page.keyboard.press('ControlOrMeta+A')
      await page.keyboard.press('ControlOrMeta+V')
      if (parameterized) {
        await frame.locator('.editor-heading-actions').getByRole('button', { name: /查询参数|Query Parameters/ }).click()
        for (const [index, name, type, value] of [[0, 'text', '文本', textValue], [1, 'minimum', '整数', '3']]) {
          await frame.locator('.parameter-toolbar').getByRole('button', { name: /添加参数|Add Parameter/ }).click()
          const parameter = frame.locator('.parameter-item').nth(index)
          await parameter.locator('.parameter-reference-field input').fill(name)
          await parameter.locator('.el-select').click()
          await frame.getByRole('option', { name: type, exact: true }).click()
          await parameter.locator('.parameter-default-scalar .el-checkbox').click()
          await expect(parameter.getByRole('checkbox')).toBeChecked()
          await parameter.locator('.parameter-default-scalar .el-input__inner').fill(value)
        }
        await frame.getByRole('button', { name: '关闭查询参数面板' }).click()
      }
      const analyzed = page.waitForResponse(response => {
        if (new URL(response.url()).pathname !== '/api/v1/develop/query-preflight' || response.request().method() !== 'POST') return false
        const body = response.request().postDataJSON()
        if (body.query !== query) return false
        return !parameterized || (body.query_parameters?.length === 2 && body.query_parameters[0].default === textValue && body.query_parameters[1].default === 3)
      })
      const created = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/develop/executions' && response.request().method() === 'POST')
      const finished = page.waitForResponse(async response => {
        if (!/^\/api\/v1\/develop\/executions\/[^/]+\/result$/.test(new URL(response.url()).pathname) || !response.ok()) return false
        return ['success', 'failed', 'timeout', 'cancelled'].includes((await response.json()).status)
      }, { timeout: 120_000 })
      await frame.locator('.toolbar-actions').getByRole('button', { name: /^(执行|Execute)$/ }).click()
      if (parameterized) await frame.locator('.el-dialog').filter({ hasText: /执行参数|Execution Parameters/ }).getByRole('button', { name: /^(执行|Execute)$/ }).click()
      const analysis = await json(await analyzed, 'read-only SQL read preflight')
      expect(analysis.allowed).toBe(true)
      expect(analysis.effect).toBe('read')
      expect(analysis.diagnostics.filter(item => item.severity === 'error')).toEqual([])
      const response = await created
      const body = response.request().postDataJSON()
      expect(body.content.query).toBe(query)
      expect(body.content.query_type).toBe('sql')
      expect(body.execution_config.engine_id).toBe(expected.engine_id)
      const locator = new URL(body.content.target_locator)
      expect(locator.hostname).toBe('engine')
      expect(decodeURIComponent(locator.pathname)).toBe(`/${expected.engine_id}/path/${expected.namespace}/source`)
      expect(locator.searchParams.get('type')).toBe('table')
      expect(body.content.query_parameters.map(({ name, type, default: value }) => ({ name, type, value }))).toEqual(parameterized ? [
        { name: 'text', type: 'string', value: textValue }, { name: 'minimum', type: 'integer', value: 3 }
      ] : [])
      const started = await json(response, 'read-only SQL workbench submission')
      expect(started.execution_id).toMatch(/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/)
      const terminal = await finished
      expect(new URL(terminal.url()).pathname).toBe(`/api/v1/develop/executions/${started.execution_id}/result`)
      const execution = await json(terminal, 'read-only SQL query result')
      expect(execution.execution_id).toBe(started.execution_id)
      expect(execution.status).toBe('success')
      expect(execution.metadata.result.result_kind).toBe('table')
      const rows = execution.metadata.result.summary.preview_rows
      const expectedRows = parameterized ? expected.rows.filter(row => BigInt(row.id) >= 3n) : expected.rows
      expect(rows).toHaveLength(expectedRows.length)
      const fields = execution.metadata.result.fields
      expect(fields.map(field => field.name)).toEqual(parameterized
        ? ['id', 'name', 'amount', 'optional_text', 'bound_text'] : ['id', 'name', 'amount', 'optional_text'])
      expect(fields.find(field => field.name === 'id')).toMatchObject({ type: 'bigint' })
      expect(fields.find(field => field.name === 'amount')).toMatchObject({ type: 'decimal', precision: 38, scale: 18 })
      const canonical = rows => rows.map(({ id, name, amount, optional_text }) => {
        expect(typeof id).toBe('string')
        expect(typeof amount).toBe('string')
        return { id, name, amount: amount.replace(/^(-?)0\./, '$1.'), optional_text }
      }).sort((a, b) => a.id.localeCompare(b.id))
      expect(canonical(rows)).toEqual(canonical(expectedRows))
      if (parameterized) expect(rows.every(row => row.bound_text === textValue)).toBe(true)
      const table = frame.locator('.query-result .result-table.el-table')
      await expect(table.locator('.el-table__body-wrapper tr')).toHaveCount(expectedRows.length)
      for (const value of ['9007199254740993', '12345678901234567890.123456789012345678', expected.sample_name]) {
        await expect(table.getByRole('cell', { name: value, exact: true })).toBeVisible()
      }
      if (parameterized) await expect(table).toContainText(textValue)
      await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), `${expected.engine_type}-query-${parameterized ? 'parameters' : 'source'}-console.png`) })
      queries.push({ execution_id: started.execution_id, status: execution.status, effect: analysis.effect, rows, fields })
    }
    await execute(`SELECT id,name,amount,optional_text FROM ${expected.namespace}.source ORDER BY id`, false)
    await execute(`SELECT id,name,amount,optional_text,CAST(:text AS ${expected.text_cast}) AS bound_text FROM ${expected.namespace}.source WHERE id >= :minimum ORDER BY id`, true)
    writeFileSync(required('ADDP_ONLINE_READONLY_SQL_BROWSER_REPORT'), JSON.stringify({ ...Object.fromEntries(
      ['run_id', 'tenant_id', 'principal_id', 'engine_id'].map(key => [key, expected[key]])), queries }))
  } finally { await browserAPI.dispose() }
})
