import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { identity, json, login } from './transfer-browser-support.js'

test('栅格 Console 执行状态、持久成果血缘及分析 JSON 一致', async ({ page }) => {
  const evidence = JSON.parse(process.env.ADDP_ONLINE_RASTER_EVIDENCE || '{}')
  const analysis = evidence.result_kind === 'json'
  const required = ['run_id', 'principal_id', 'tenant_id', 'execution_id', 'source_item_id', 'source_locator', 'case_name', 'source_name']
  required.push(...(analysis ? ['expected_result'] : ['target_item_id', 'target_locator', 'target_name']))
  for (const field of required) {
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

    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const detail = frame.locator('.execution-detail-content')
    await expect(detail).toBeVisible()
    await expect(detail).toContainText(evidence.execution_id)
    await expect(detail.locator('.el-tag').filter({ hasText: /成功|Success/i }).first()).toBeVisible()
    if (analysis) {
      expect(execution.metadata?.lineage_facts).toBeUndefined()
      await expect(detail.locator('.execution-lineage')).toHaveCount(0)
      await detail.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `raster-workflow-${evidence.case_name}-monitor.png`) })
      const professional = await json(await api.get(`/api/v1/develop/executions/${evidence.execution_id}`), 'browser Develop analysis')
      expect(professional.status).toBe('success')
      expect(professional.outputs || {}).toEqual({})
      expect(professional.metadata.result.final_result).toEqual(evidence.expected_result)
      expect(professional.metadata.result.produced_targets || []).toEqual([])
      expect(professional.metadata.result.meta_scan_runs || []).toEqual([])
      await page.goto(`${process.env.CONSOLE_URL}/develop/executions/${encodeURIComponent(evidence.execution_id)}`)
      const resultPage = page.frameLocator('iframe[data-testid="module-iframe"]').locator('.execution-detail-page')
      await expect(resultPage).toBeVisible()
      await expect(resultPage).toContainText(evidence.execution_id)
      await expect(resultPage.locator('.toolbar .el-tag').filter({ hasText: /成功|Success/i })).toBeVisible()
      const preview = resultPage.locator('.workflow-final-result-json')
      await expect(preview).toBeVisible()
      await expect.poll(async () => JSON.parse(await preview.innerText())).toEqual(evidence.expected_result)
      await expect(resultPage.locator('.workflow-final-result-value')).toHaveCount(0)
      await preview.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `raster-workflow-${evidence.case_name}-json.png`) })
    } else {
      expect(execution.metadata.lineage_facts.inputs.map(input => input.locator)).toEqual([evidence.source_locator])
      expect(execution.metadata.lineage_facts.outputs.map(output => output.locator)).toEqual([evidence.target_locator])
      const lineage = frame.locator('.execution-lineage')
      await expect(lineage).toBeVisible()
      const groups = lineage.locator('.execution-lineage__group')
      await expect(groups).toHaveCount(2)
      await expect(groups.nth(0).locator('.execution-lineage__card')).toHaveCount(1)
      await expect(groups.nth(1).locator('.execution-lineage__card')).toHaveCount(1)
      await expect(groups.nth(0)).toContainText(evidence.source_name)
      await expect(groups.nth(1)).toContainText(evidence.target_name)
      await expect(groups.nth(0).locator('.execution-lineage__resource-action')).toHaveCount(1)
      await expect(groups.nth(1).locator('.execution-lineage__resource-action')).toHaveCount(0)

      const query = new URLSearchParams({ subject_kind: 'data_item', item_id: String(evidence.target_item_id), direction: 'upstream' })
      const graph = await json(await api.get(`/api/v1/meta/lineage/graph?${query}`), 'browser raster lineage graph')
      expect(graph.truncated).toBe(false)
      expect(graph.edges.some(edge => edge.source.item_id === evidence.source_item_id &&
        edge.target.item_id === evidence.target_item_id && edge.status === 'active' && edge.relation_kind === 'derive' &&
        edge.evidence?.execution_id === evidence.execution_id)).toBe(true)
      await detail.screenshot({ path: resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `raster-workflow-${evidence.case_name}-monitor.png`) })
    }
    writeFileSync(resolve(process.env.ADDP_ONLINE_ARTIFACT_DIR, `raster-workflow-${evidence.case_name}-browser.json`), JSON.stringify({
      ...evidence, schema_version: 'addp.raster-workflow-browser/v1', result: 'passed'
    }, null, 2))
  } finally {
    await api.dispose()
  }
})
