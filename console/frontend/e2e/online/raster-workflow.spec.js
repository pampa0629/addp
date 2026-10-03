import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

test('栅格业务成果的 Console 执行状态、身份和源目标血缘一致', async ({ page }) => {
  const evidence = JSON.parse(process.env.ADDP_ONLINE_RASTER_EVIDENCE || '{}')
  for (const field of ['run_id', 'principal_id', 'tenant_id', 'execution_id', 'source_item_id', 'target_item_id', 'source_locator', 'target_locator']) {
    expect(evidence[field], `missing raster evidence ${field}`).toBeTruthy()
  }
  const path = `/monitor/executions?execution_id=${encodeURIComponent(evidence.execution_id)}`
  const token = await login(page, process.env.ADDP_ONLINE_TEST_USER_USERNAME, process.env.ADDP_ONLINE_TEST_USER_PASSWORD, path)
  const api = await request.newContext({
    baseURL: process.env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${token}` }
  })
  try {
    const auth = identity(await json(await api.get('/api/v1/system/auth/context'), 'browser AuthContext'))
    expect(auth.principalID).toBe(evidence.principal_id)
    expect(auth.principalType).toBe('user')
    expect(auth.tenantID).toBe(evidence.tenant_id)
    expect(auth.contextType).toBe('tenant')
    const execution = await json(await api.get(`/api/v1/monitor/executions/by-execution-id/${evidence.execution_id}`), 'browser Monitor execution')
    expect(execution.status).toBe('success')
    expect(execution.module).toBe('develop')
    expect(execution.metadata.lineage_facts.inputs.map(input => input.locator)).toEqual([evidence.source_locator])
    expect(execution.metadata.lineage_facts.outputs.map(output => output.locator)).toEqual([evidence.target_locator])

    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const detail = frame.locator('.execution-detail-content')
    await expect(detail).toBeVisible()
    await expect(detail).toContainText(evidence.execution_id)
    await expect(detail.locator('.el-tag').filter({ hasText: /成功|Success/i }).first()).toBeVisible()
    const lineage = frame.locator('.execution-lineage')
    await expect(lineage).toBeVisible()
    const groups = lineage.locator('.execution-lineage__group')
    await expect(groups).toHaveCount(2)
    await expect(groups.nth(0).locator('.execution-lineage__card')).toHaveCount(1)
    await expect(groups.nth(1).locator('.execution-lineage__card')).toHaveCount(1)
    await expect(groups.nth(0)).toContainText('source.tif')
    await expect(groups.nth(1)).toContainText('result.cog.tif')
    await expect(groups.nth(0).locator('.execution-lineage__resource-action')).toHaveCount(1)
    await expect(groups.nth(1).locator('.execution-lineage__resource-action')).toHaveCount(0)

    const query = new URLSearchParams({ subject_kind: 'data_item', item_id: String(evidence.target_item_id), direction: 'upstream' })
    const graph = await json(await api.get(`/api/v1/meta/lineage/graph?${query}`), 'browser raster lineage graph')
    expect(graph.truncated).toBe(false)
    expect(graph.edges.some(edge => edge.source.item_id === evidence.source_item_id &&
      edge.target.item_id === evidence.target_item_id && edge.status === 'active' &&
      edge.evidence?.execution_id === evidence.execution_id)).toBe(true)
    await detail.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'raster-workflow-monitor.png') })
    writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, 'raster-workflow-browser.json'), JSON.stringify({
      ...evidence, schema_version: 'addp.raster-workflow-browser/v1', result: 'passed'
    }, null, 2))
  } finally {
    await api.dispose()
  }
})
