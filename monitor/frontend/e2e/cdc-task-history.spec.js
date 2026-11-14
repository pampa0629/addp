import { test, expect } from '@playwright/test'
import { cdcTaskBackend } from './cdc-tasks.fixture'

test('Task history follows URL windows and clears failed or revoked samples', async ({ page }) => {
 const state=await cdcTaskBackend(page);state.setMode('history-values')
 await page.goto('/cdc-resources?view=tasks&task_id=7&refresh=off')
 await expect(page.getByTestId('cdc-task-history-chart')).toBeVisible()
 await expect(page.getByTestId('cdc-task-history-empty')).toHaveCount(0)
 expect(state.calls.find(v=>v.path.endsWith('/cdc-metric-history')).params.generation).toBe('1')
 state.hold();await page.getByRole('button',{name:'刷新',exact:true}).click()
 await expect(page.getByTestId('cdc-task-chain')).toHaveAttribute('aria-busy','true')
 await expect(page.getByTestId('cdc-task-history-chart')).toBeVisible()
 state.release();await expect(page.getByTestId('cdc-task-chain')).toHaveAttribute('aria-busy','false')

 await page.getByTestId('cdc-task-history').locator('.el-select').filter({has:page.getByRole('combobox',{name:'趋势时段'})}).click()
 await page.getByRole('option',{name:'近 7 天',exact:true}).click()
 await expect(page).toHaveURL(/task_range=7d/)
 await expect(page.getByTestId('cdc-task-history-chart')).toBeVisible()
 state.setMode('history-large');await page.getByRole('button',{name:'刷新',exact:true}).click()
 await expect(page.getByTestId('cdc-task-history-precision')).toBeVisible()
 state.setMode('history-failed');await page.getByRole('button',{name:'刷新',exact:true}).click()
 await expect(page.getByTestId('cdc-task-history-error')).toBeVisible()
 await expect(page.getByTestId('cdc-task-history-chart')).toHaveCount(0)
 state.setMode('history-values');await page.getByRole('button',{name:'刷新',exact:true}).click()
 await expect(page.getByTestId('cdc-task-history-chart')).toBeVisible()
 state.setMode('history-denied');await page.getByRole('button',{name:'刷新',exact:true}).click()
 await expect(page.getByTestId('cdc-task-history')).toHaveCount(0)
 await expect(page.getByTestId('cdc-task-source')).toHaveCount(0)
})

test('English history keeps gaps and metric selection on a narrow task page', async ({ page }) => {
 await page.setViewportSize({width:390,height:844})
 const state=await cdcTaskBackend(page,{locale:'en'});state.setMode('history-values')
 await page.goto('/cdc-resources?view=tasks&task_id=7&refresh=off&task_metric=delay_seconds&task_range=24h')
 await expect(page.getByRole('heading',{name:'Task metric history'})).toBeVisible()
 await expect(page.getByTestId('cdc-task-history-empty')).toBeVisible()
 await expect(page.getByTestId('cdc-task-history-chart')).toBeVisible()
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+1)).toBe(true)
})
