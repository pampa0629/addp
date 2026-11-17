import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

// Exercise the exact Online screenshot boundary with real scrolling/clipping;
// importing the Online suite itself would register its real backend tests.
const source = readFileSync('e2e/online/vector-topology-persistence.spec.js', 'utf8')
const start = source.indexOf('async function assertInputDiagnosticViewport(')
const end = source.indexOf('async function verifyInputDiagnosticCase(', start)
const positionDiagnostic = new Function('expect', source.slice(start, end) + ';return assertInputDiagnosticViewport')(expect)

test('diagnostic screenshot boundary restores a scrolled reason and rejects clipped content', async ({ page }) => {
  await page.setContent(`<div style="height:500px;overflow:auto" id="modal">
    <div style="height:800px"></div><section class="failure" style="height:320px">
    <h4>原始失败原因</h4><dl><dd>执行操作</dd><dd>面邻检查</dd><dd>本次执行编号</dd></dl>
    <button style="margin-top:160px">复制诊断信息</button></section><div style="height:800px"></div></div>`)
  const panel = page.locator('.failure')
  await page.locator('#modal').evaluate(el => { el.scrollTop = 1050 })
  let clipped = false
  try { await expect(panel.locator('h4')).toBeInViewport({ ratio: 1, timeout: 100 }) } catch { clipped = true }
  expect(clipped).toBe(true)
  await expect(panel.locator('h4')).toHaveText('原始失败原因')
  await positionDiagnostic(panel)
  await expect(panel.locator('h4')).toBeInViewport({ ratio: 1 })
  await panel.evaluate(el => { el.style.height = '1400px' })
  let rejected = false
  try { await positionDiagnostic(panel) } catch { rejected = true }
  expect(rejected).toBe(true)
})
