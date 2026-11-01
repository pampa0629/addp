import { expect, test } from '@playwright/test'
import { assertFileGDBChild } from './online/manager-filegdb-preview.js'

const initial = [{ label: 'initial_points_0', value: 1 }, { label: 'initial_points_1', value: 2 }]
// Test the evidence reader against rendered DOM; this is not real Manager/T4 evidence.
async function render(page, rows, { child = 'initial_points', total = 2, columns = ['fid', 'value', 'label'] } = {}) {
  await page.setContent(`<div class="container-preview"><div class="child-select">${child}</div>
    <table class="child-meta"><tr><td>当前页行数</td><td>${rows.length}</td><td>总行数</td><td>${total}</td></tr></table>
    <div class="child-preview"><div class="tabular-result-renderer">
      <table class="el-table__header-wrapper"><tr>${columns.map(key => `<th><div class="cell">${key}</div></th>`).join('')}</tr></table>
      <table class="el-table__body-wrapper"><tbody>${rows.map(row => `<tr class="el-table__row">${columns.map(key => `<td><span class="scalar-cell">${row[key] ?? 1}</span></td>`).join('')}</tr>`).join('')}</tbody></table>
    </div></div></div>`)
  return page.locator('.container-preview')
}

test('preview evidence reads actual columns and the switched child without stale rows', async ({ page }) => {
  const container = await render(page, initial)
  expect(await assertFileGDBChild(container, 'initial_points', initial)).toEqual({ child: 'initial_points',
    current_rows: 2, total_rows: 2, rows: initial.map(row => ({ label: row.label, value: String(row.value) })), previous_labels_absent: [] })
  const added = [{ label: 'added_points_0', value: 1 }]
  await render(page, added, { child: 'added_points', total: 1 })
  expect((await assertFileGDBChild(container, 'added_points', added, initial.map(row => row.label))).rows)
    .toEqual([{ label: 'added_points_0', value: '1' }])
})

for (const failure of ['empty rows with index total two', 'wrong value beside correct label', 'stale initial rows after switch']) {
  test(`preview evidence rejects ${failure}`, async ({ page }) => {
    const rows = failure.startsWith('empty') ? [] : failure.startsWith('wrong')
      ? [{ ...initial[0], value: 2 }, initial[1]] : initial
    const switched = failure.startsWith('stale')
    const container = await render(page, rows, { child: switched ? 'added_points' : 'initial_points' })
    await expect(assertFileGDBChild(container, switched ? 'added_points' : 'initial_points',
      switched ? [{ label: 'added_points_0', value: 1 }] : initial)).rejects.toThrow()
  })
}
